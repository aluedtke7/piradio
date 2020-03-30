package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aluedtke7/piradio/debouncer"
	"github.com/aluedtke7/piradio/display"
	"github.com/aluedtke7/piradio/lcd"
	"github.com/aluedtke7/piradio/oled"

	"github.com/antigloss/go/logger"
	"periph.io/x/periph/conn/gpio"
	"periph.io/x/periph/conn/gpio/gpioreg"
	"periph.io/x/periph/host"
)

const (
	debounceTime            = 100
	debounceWriteToFileTime = 15
	defVolumeAnalog         = "55"
	defVolumeBluetooth      = "35"
)

var (
	disp                display.Display
	readyForMplayer     bool
	bluetoothConnected  bool
	debug               *bool
	camelCasePtr        *bool
	noisePtr            *bool
	oledPtr             *bool
	noBluetoothPtr      *bool
	backlightOffPtr     *bool
	backlightOffTimePtr *int
	scrollStationPtr    *bool
	lcdDelayPtr         *int
	scrollSpeedPtr      *int
	stations            []radioStation
	stationIdx          = -1
	btDevices           []string
	bitrate             string
	volume              string
	volumeAnalog        string
	volumeBluetooth     string
	muted               bool
	charsPerLine        int
	command             *exec.Cmd
	inPipe              io.WriteCloser
	outPipe             io.ReadCloser
	pipeChan            = make(chan io.ReadCloser)
	ipAddress           string
	homePath            string
	currentStation      string
	debounceWrite       func(f func())
	debounceBacklight   func(f func())
	stationMutex        = &sync.Mutex{}
	charMap             = map[string]string{"'": "'", "´": "'", "á": "a", "é": "e", "ê": "e", "è": "e", "í": "i", "à": "a",
		"ä": "ae", "Ä": "Ae", "ö": "oe", "Ö": "Oe", "ü": "ue", "Ü": "Ue", "ß": "ss", "…": "...", "Ó": "O", "ó": "o",
		"õ": "o", "ñ": "n", "ó": "o", "ø": "o", "É": "E"}
)

// holds a Radio Station name and url
type radioStation struct {
	name string
	url  string
}

// helper for error checking
func check(err error) {
	if err != nil {
		logger.Error(err.Error())
		logger.Error(errors.Unwrap(fmt.Errorf("Wrapped error: %w", err)).Error())
	}
}

// logs the ipv4 addresses found and stores the first non localhost address in variable 'ipAddress'
func logNetworkInterfaces() {
	interfaces, err := net.Interfaces()
	if err != nil {
		logger.Error(err.Error())
		return
	}
	reg := regexp.MustCompile("^((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\\.){3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])")
	for _, i := range interfaces {
		byName, err := net.InterfaceByName(i.Name)
		if err != nil {
			logger.Warn(err.Error())
		}
		err = nil
		addresses, err := byName.Addrs()
		for _, v := range addresses {
			ipv4 := v.String()
			if reg.MatchString(ipv4) {
				logger.Trace(ipv4)
				if strings.Index(ipv4, "127.0.") != 0 {
					idx := strings.Index(ipv4, "/")
					if idx > 0 {
						ipAddress = ipv4[0:idx]
					} else {
						ipAddress = ipv4
					}
				}
			}
		}
	}
}

// checks if a given string contains only lowercase or special characters. Is used for the conversion to camel case.
// Lowercase strings will not be 'camel-cased'.
func isOnlyLowerCase(text string) bool {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyz.+-*/%&!# _,;:()[]{}"
	for _, c := range text {
		if !strings.Contains(chars, string(c)) {
			return false
		}
	}
	return true
}

// removes characters/runes that cannot be displayed on the LCD/OLED. These displays can only display ascii characters.
// Via the 'charMap' the best possible translation is made. When the flag 'camelCase' is set to true, all non only
// lowercase strings will be converted to camel case format.
func beautify(text string) string {
	var b strings.Builder
	for _, runeValue := range text {
		s := charMap[string(runeValue)]
		if s == "" {
			if runeValue < 32 || runeValue > 126 {
				logger.Trace("Illegal rune:", runeValue, string(runeValue))
			} else {
				b.WriteRune(runeValue)
			}
		} else {
			b.WriteString(s)
		}
	}
	text = b.String()
	if *camelCasePtr {
		if !isOnlyLowerCase(text) {
			cct := strings.Title(strings.ToLower(text))
			idx := strings.Index(cct, "'")
			if idx > 0 && idx < len(cct)-1 {
				cct = cct[:idx+1] + strings.ToLower(string(cct[idx+1])) + cct[idx+2:]
			}
			return cct
		}
	}
	return text
}

func printLine(line int, text string, scroll bool, doNotBeautify ...bool) {
	t := strings.TrimSpace(text)
	if len(doNotBeautify) < 1 {
		t = beautify(t)
	}
	if line == 2 && *noisePtr {
		t = removeNoise(t)
	}
	disp.PrintLine(line, t, scroll)
}

func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func getHomeDir() string {
	usr, err := user.Current()
	if err != nil {
		return "~/"
	}
	return usr.HomeDir
}

// returns the index of the last used station and the volume levels
func getStationAndVolumes() (idx int, volAnalog string, volBt string) {
	fileName := filepath.Join(homePath, "last_values")
	content, err := ioutil.ReadFile(fileName)
	if err != nil {
		idx = 0
		volAnalog = defVolumeAnalog
		volBt = defVolumeBluetooth
	} else {
		contentArr := strings.Split(strings.Trim(string(content), " "), "\n")
		if len(contentArr) > 0 {
			idx, err = strconv.Atoi(contentArr[0])
		}
		if len(contentArr) > 1 && len(contentArr[1]) > 0 {
			volAnalog = contentArr[1]
		} else {
			volAnalog = defVolumeAnalog
		}
		if len(contentArr) > 2 && len(contentArr[2]) > 0 {
			volBt = contentArr[2]
		} else {
			volBt = defVolumeBluetooth
		}
	}
	logger.Trace("getStationAndVolumes: %d %s %s", idx, volAnalog, volBt)
	return idx - 1, volAnalog, volBt
}

// saves the index of the actual station index and the volumes levels
func saveStationAndVolumes() {
	fileName := filepath.Join(homePath, "last_values")
	var s = strconv.Itoa(stationIdx) + "\n" + volumeAnalog + "\n" + volumeBluetooth
	err := ioutil.WriteFile(fileName, []byte(s), 0644)
	if err != nil {
		logger.Warn("Error writing file %s : %s", fileName, err)
	}
	logger.Trace("saveStationAndVolumes: %d %s %s", stationIdx, volumeAnalog, volumeBluetooth)
}

// loads the list with radio stations or creates a default list
func loadStations(fileName string) []radioStation {
	var stations []radioStation

	if fileExists(fileName) {
		f, err := os.Open(fileName)
		check(err)
		//noinspection GoUnhandledErrorResult
		defer f.Close()

		scanner := bufio.NewScanner(f)
		nr := 1
		for scanner.Scan() {
			line := strings.Trim(scanner.Text(), "\n\r")
			items := strings.Split(line, ",")
			if len(items) == 2 {
				stations = append(stations,
					radioStation{strconv.Itoa(nr) + " " + strings.TrimSpace(items[0]), strings.TrimSpace(items[1])})
				nr++
			}
		}
		check(scanner.Err())
	}
	if len(stations) == 0 {
		stations = append(stations,
			radioStation{"RadioHH", "http://stream.radiohamburg.de/rhh-live/mp3-192/linkradiohamburgde"})
		stations = append(stations,
			radioStation{"Jazz Radio", "http://jazzradio.ice.infomaniak.ch/jazzradio-high.mp3"})
		stations = append(stations,
			radioStation{"M1.FM Chillout", "http://tuner.m1.fm/chillout.mp3"})
	}
	return stations
}

func printBitrateVolume(lineNum int, bitrate string, volume string, muted bool) {
	var s string
	if muted {
		volume = "-mute-"
	}
	if charsPerLine < 20 {
		s = fmt.Sprintf("%-10v%8v", bitrate, volume)
	} else {
		s = fmt.Sprintf("%-10v%10v", bitrate, volume)
	}
	printLine(lineNum, s, false, true)
}

func isConnceted(url string) bool {
	_, err := http.Get(url)
	if err != nil {
		return false
	}
	return true
}

// does everything needed to stop the running mplayer and start a new instance with the actual station url
func newStation() {
	disp.Clear()
	logger.Trace("New station: %s", stations[stationIdx].name)
	printLine(0, "-> "+stations[stationIdx].name, false)
	printLine(1, "", false)
	printLine(2, "", false)
	if stationIdx == 0 {
		printLine(3, ipAddress, false)
	} else {
		printLine(3, time.Now().Format("15:04:05  02.01.06"), false)
	}
	if inPipe != nil {
		_, _ = inPipe.Write([]byte("q"))
		_ = inPipe.Close()
		_ = outPipe.Close()
		_ = command.Wait()
	}
	for {
		if readyForMplayer {
			break
		}
		logger.Trace("Waiting for 'readyForMplayer'...")
		time.Sleep(time.Second)
	}
	if bluetoothConnected {
		logger.Trace("Using BT volume " + volumeBluetooth)
		volume = vol2VolString(volumeBluetooth)
		command = exec.Command("mplayer", "-quiet", "-volume", volumeBluetooth, stations[stationIdx].url)
	} else {
		logger.Trace("Using Analog volume " + volumeAnalog)
		volume = vol2VolString(volumeAnalog)
		command = exec.Command("mplayer", "-quiet", "-volume", volumeAnalog, stations[stationIdx].url)
	}
	var err error
	inPipe, err = command.StdinPipe()
	check(err)
	outPipe, err = command.StdoutPipe()
	check(err)
	err = command.Start()
	check(err)
	go func() {
		pipeChan <- outPipe
	}()

	debounceWrite(saveStationAndVolumes)
}

func switchBacklightOn() {
	disp.Backlight(true)
	if *backlightOffPtr {
		debounceBacklight(switchBacklightOff)
	}
}

func switchBacklightOff() {
	disp.Backlight(false)
}

// reads the paired bt devices into an array and signals via 'readyForMplayer' to start the mplayer
func checkBluetooth() {
	// init part: get the list of paired bluetooth devices
	result, err := exec.Command("bluetoothctl", "devices").Output()
	if err != nil {
		logger.Error(err.Error())
	} else {
		arr := strings.Split(string(result), "\n")
		logger.Info("BT Devices paired:")
		for _, s := range arr {
			parts := strings.Split(s, " ")
			if len(parts) > 1 {
				info, err2 := exec.Command("bluetoothctl", "info", parts[1]).Output()
				if err2 == nil {
					if strings.Contains(string(info), "Audio Sink") {
						btDevices = append(btDevices, parts[1])
						logger.Info(parts[1])
						if strings.Contains(string(info), "Connected: yes") {
							logger.Info("BT connected to " + parts[1])
							bluetoothConnected = true
						}
					}
				}
			}
		}
	}
	readyForMplayer = true
}

// listens for BT events and restarts the mplayer if event detected
func listenForBtChanges() {
	lastExitCode := 999
	for {
		cmd := exec.Command("ls", "/dev/input/event0")
		_ = cmd.Run()
		exitCode := cmd.ProcessState.ExitCode()
		if exitCode == 2 {
			// not connected
			if lastExitCode == 0 {
				logger.Info("Re-run mplayer (2)... ")
				bluetoothConnected = false
				stationMutex.Lock()
				newStation()
				stationMutex.Unlock()
			}
			for _, btDevice := range btDevices {
				// logger.Info(fmt.Sprintf("Trying to connect device #%d %s", idx, btDevice))
				cmd = exec.Command("bluetoothctl", "connect", btDevice)
				_ = cmd.Run()
				connectExitCode := cmd.ProcessState.ExitCode()
				if connectExitCode == 0 {
					logger.Info("Success with device " + btDevice)
					break
				}
			}
		} else if exitCode == 0 {
			// connected
			if lastExitCode == 2 {
				logger.Info("Re-run mplayer (0)... ")
				bluetoothConnected = true
				stationMutex.Lock()
				newStation()
				stationMutex.Unlock()
			}
		}
		lastExitCode = exitCode
		time.Sleep(3 * time.Second)
	}
}

// removes unneeded/unwanted strings from the title like " (CDM EDIT)" etc.
func removeNoise(title string) string {
	opening := strings.Index(title, "(")
	closing := strings.Index(title, ")")
	// text must be enclosed by round brackets
	if opening >= 0 && closing >= 0 && closing > opening {
		remove := false
		// fmt.Println("removing noise...")
		noise := strings.ToLower(title[opening+1 : closing])
		if len(noise) > 0 {
			// fmt.Println("noise:", noise)
			if strings.Contains(noise, "edit") ||
				strings.Contains(noise, "mix") ||
				strings.Contains(noise, "cdm") ||
				strings.Contains(noise, "cut") ||
				strings.Contains(noise, "rmx") ||
				strings.Contains(noise, "cover") {
				remove = true
			}
		}
		if remove {
			title = strings.ReplaceAll(title[:opening]+title[closing+1:], "  ", " ")
			title = strings.TrimSpace(strings.ReplaceAll(title, " .", ""))
			if *debug {
				logger.Info("removeNoise: %s", title)
			}
		}
	}
	return title
}

func vol2VolString(vol string) string {
	var format string
	if charsPerLine < 20 {
		format = "V %s%%"
	} else {
		format = "Vol %s%%"
	}
	return fmt.Sprintf(format, vol)
}

func main() {
	homePath = filepath.Join(getHomeDir(), ".piradio")
	_ = os.MkdirAll(homePath, os.ModePerm)
	_ = logger.Init(filepath.Join(homePath, "log"), 30, 2, 10, true)

	logger.Trace("Starting piradio...")
	logNetworkInterfaces()

	// Commandline parameters
	camelCasePtr = flag.Bool("camelCase", false, "set to format title")
	debug = flag.Bool("debug", false, "set to output mplayer info on stdout")
	lcdDelayPtr = flag.Int("lcdDelay", 3, "initial delay for LCD in s (1s...10s)")
	noisePtr = flag.Bool("noise", false, "set to remove noise from title")
	oledPtr = flag.Bool("oled", false, "set to use OLED Display")
	noBluetoothPtr = flag.Bool("noBluetooth", false, "set to only use analog output")
	backlightOffPtr = flag.Bool("backlightOff", false, "set to switch off backlight after some time")
	backlightOffTimePtr = flag.Int("backlightOffTime", 15, "backlight switch off time in s (3s...3600s)")
	scrollSpeedPtr = flag.Int("scrollSpeed", 500, "scroll speed in ms (100ms...10000ms)")
	scrollStationPtr = flag.Bool("scrollStation", false, "set to scroll station names")
	flag.Parse()
	if *backlightOffTimePtr < 3 {
		*backlightOffTimePtr = 3
	}
	if *backlightOffTimePtr > 3600 {
		*backlightOffTimePtr = 3600
	}
	if *scrollSpeedPtr < 100 {
		*scrollSpeedPtr = 100
	}
	if *scrollSpeedPtr > 10000 {
		*scrollSpeedPtr = 10000
	}
	if *lcdDelayPtr < 1 {
		*lcdDelayPtr = 1
	}
	if *lcdDelayPtr > 10 {
		*lcdDelayPtr = 10
	}

	var err error
	if *oledPtr {
		disp, err = oled.New(*scrollSpeedPtr)
	} else {
		disp, err = lcd.New(*scrollStationPtr, *scrollSpeedPtr, *lcdDelayPtr)
	}
	charsPerLine = disp.GetCharsPerLine()
	if err != nil {
		logger.Error("Couldn't initialize display: %s", err)
	}

	// Load gpio drivers:
	if _, err = host.Init(); err != nil {
		check(err)
	}

	// Lookup pins by their names and set them as input pins with an internal pull up resistor:
	pNextStation := gpioreg.ByName("GPIO5")
	if pNextStation == nil {
		logger.Error("Failed to find GPIO5")
	}
	if err := pNextStation.In(gpio.PullUp, gpio.NoEdge); err != nil {
		check(err)
	}

	pPrevStation := gpioreg.ByName("GPIO6")
	if pPrevStation == nil {
		logger.Error("Failed to find GPIO6")
	}
	if err := pPrevStation.In(gpio.PullUp, gpio.NoEdge); err != nil {
		check(err)
	}

	pVolUp := gpioreg.ByName("GPIO19")
	if pVolUp == nil {
		logger.Error("Failed to find GPIO19")
	}
	if err := pVolUp.In(gpio.PullUp, gpio.NoEdge); err != nil {
		check(err)
	}

	pVolDown := gpioreg.ByName("GPIO26")
	if pVolDown == nil {
		logger.Error("Failed to find GPIO26")
	}
	if err := pVolDown.In(gpio.PullUp, gpio.NoEdge); err != nil {
		check(err)
	}

	pMuteAudio := gpioreg.ByName("GPIO16")
	if pMuteAudio == nil {
		logger.Error("Failed to find GPIO16")
	}
	if err := pMuteAudio.In(gpio.PullUp, gpio.NoEdge); err != nil {
		check(err)
	}

	var statusChan = make(chan string)
	var ctrlChan = make(chan os.Signal)
	var volumeMutex = &sync.Mutex{}

	debounceBtn := debouncer.New(debounceTime * time.Millisecond)
	debounceWrite = debouncer.New(debounceWriteToFileTime * time.Second)
	debounceBacklight = debouncer.New(time.Duration(*backlightOffTimePtr) * time.Second)

	// the following 4 functions handle the pressed buttons
	fpPrev := func() {
		stationMutex.Lock()
		stationIdx-- // previous station
		if stationIdx < 0 {
			stationIdx = len(stations) - 1
		}
		newStation()
		stationMutex.Unlock()
	}
	fpNext := func() {
		stationMutex.Lock()
		stationIdx++ // next station
		stationIdx = stationIdx % len(stations)
		newStation()
		stationMutex.Unlock()
	}
	fpUp := func() {
		volumeMutex.Lock()
		_, err = inPipe.Write([]byte("*")) // increase volume
		volumeMutex.Unlock()
		check(err)
		debounceWrite(saveStationAndVolumes)
	}
	fpDown := func() {
		volumeMutex.Lock()
		_, err = inPipe.Write([]byte("/")) // decrease volume
		volumeMutex.Unlock()
		check(err)
		debounceWrite(saveStationAndVolumes)
	}
	fpMute := func() {
		volumeMutex.Lock()
		_, err = inPipe.Write([]byte("m")) // toggle mute
		volumeMutex.Unlock()
		check(err)
	}

	signal.Notify(ctrlChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL)

	stations = loadStations(filepath.Join(homePath, "stations"))
	stationIdx, volumeAnalog, volumeBluetooth = getStationAndVolumes()
	go checkBluetooth()

	// this function is polling the GPIO Levels and calls the debouncer when a Low-Level is found (pull up resistor)
	go func() {
		for {
			switch {
			case pNextStation.Read() == false:
				debounceBtn(fpNext) // next station
				switchBacklightOn()
			case pPrevStation.Read() == false:
				debounceBtn(fpPrev) // previous station
				switchBacklightOn()
			case pVolUp.Read() == false:
				if !muted {
					debounceBtn(fpUp) // increase volume
				}
				switchBacklightOn()
			case pVolDown.Read() == false:
				if !muted {
					debounceBtn(fpDown) // decrease volume
				}
				switchBacklightOn()
			case pMuteAudio.Read() == false:
				debounceBtn(fpMute) // toggle mute
				switchBacklightOn()
			}
			time.Sleep(70 * time.Millisecond)
		}
	}()

	// this goroutine is waiting for piradio being stopped
	go func() {
		<-ctrlChan
		logger.Trace("Ctrl+C received... Exiting")
		close(statusChan)
		close(pipeChan)
		os.Exit(1)
	}()

	switchBacklightOn()
	// this goroutine is reading the output from mplayer and feeds the strings into the statusChan
	go func() {
		for {
			outPipe := <-pipeChan
			reader := bufio.NewReader(outPipe)
			for {
				data, err := reader.ReadString('\n')
				if err != nil {
					statusChan <- "Playing stopped"
					break
				} else {
					statusChan <- data
				}
			}
		}
	}()

	// Is used for testing if the url is available on startup. This is important, when started via rc.local
	// on boot, because the internet connection might not be available yet.
	for !isConnceted(stations[0].url) {
		logger.Trace("URL %s is NOT available", stations[0].url)
		time.Sleep(300 * time.Millisecond)
	}
	fpNext()

	if !*noBluetoothPtr {
		go listenForBtChanges()
	}

	// loop for processing the output of mplayer
	for {
		select {
		case line := <-statusChan:
			if *debug && len(strings.TrimSpace(line)) > 0 {
				fmt.Print("Process output: " + line)
			}
			if strings.Index(line, "Name") == 0 {
				name := strings.Split(line, ":")
				if len(name) > 1 {
					s := strings.Trim(name[1], " \n")
					// logger.Trace("Station: " + s)
					printLine(0, s, *scrollStationPtr)
					logger.Info("Station: " + s)
					currentStation = s
				}
			}
			if strings.Index(line, "ICY Info:") == 0 {
				icy2 := line[10:]
				st := strings.Split(icy2, ";")
				for _, value := range st {
					if strings.Index(value, "StreamTitle=") == 0 {
						title := value[13 : len(value)-1]
						trenner := strings.Index(title, " - ")
						if trenner > 0 {
							printLine(1, title[:trenner], true)
							printLine(2, title[trenner+3:], true)
							if strings.TrimSpace(title) != "-" && title != currentStation {
								logger.Info("Title:   " + title)
							}
						} else {
							printLine(1, title, true)
							printLine(2, "", false)
						}
					}
				}
			}
			if strings.Index(line, "Bitrate") == 0 {
				bitrateArr := strings.Split(line, ":")
				if len(bitrateArr) > 1 {
					bitrate = strings.Trim(bitrateArr[1], " \n")
					logger.Trace("Bitrate: " + bitrate)
					printBitrateVolume(3, bitrate, volume, muted)
				}
			}
			if strings.Index(line, "Volume:") >= 0 {
				volumeArr := strings.Split(line, ":")
				if len(volumeArr) > 1 {
					v := strings.Split(strings.Trim(volumeArr[1], " \n"), " ")[0]
					volume = vol2VolString(v)
					logger.Trace("Volume: " + v)
					printBitrateVolume(3, bitrate, volume, muted)
					if bluetoothConnected {
						volumeBluetooth = v
					} else {
						volumeAnalog = v
					}
				}
			}
			if strings.Index(line, "Mute:") >= 0 {
				muteArr := strings.Split(line, ":")
				if len(muteArr) > 1 {
					muted = strings.Contains(muteArr[1], "enabled")
					printBitrateVolume(3, bitrate, volume, muted)
				}
			}
		}
	}
}
