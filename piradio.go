package main

import (
	"bufio"
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
	debounceTime = 100
)

var (
	disp             display.Display
	debug            *bool
	camelCasePtr     *bool
	noisePtr         *bool
	oledPtr          *bool
	scrollStationPtr *bool
	lcdDelayPtr      *int
	scrollSpeedPtr   *int
	stations         []radioStation
	stationIdx       = -1
	bitrate          string
	volume           string
	charsPerLine     int
	command          *exec.Cmd
	inPipe           io.WriteCloser
	outPipe          io.ReadCloser
	pipeChan         = make(chan io.ReadCloser)
	ipAddress        string
	homePath         string
	currentStation   string
	debounceWrite    func(f func())
	charMap          = map[string]string{"'": "'", "´": "'", "á": "a", "é": "e", "ê": "e", "è": "e", "í": "i", "à": "a",
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

// returns the index of the last used station
func getLaststationIdx() (idx int) {
	fileName := filepath.Join(homePath, "last_station")
	index, err := ioutil.ReadFile(fileName)
	if err != nil {
		idx = 0
	} else {
		idx, err = strconv.Atoi(string(index))
	}
	logger.Trace("getLaststationIdx: %d", idx)
	return idx - 1
}

// saves the index of the actual station index
func saveLaststationIdx() {
	fileName := filepath.Join(homePath, "last_station")
	err := ioutil.WriteFile(fileName, []byte(strconv.Itoa(stationIdx)), 0644)
	if err != nil {
		logger.Warn("Error writing file %s : %s", fileName, err)
	}
	logger.Trace("saveLaststationIdx: %d", stationIdx)
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

func printBitrateVolume(lineNum int, bitrate string, volume string) {
	var s string
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

// does everything needed to stop the running mplayer and start a new instance
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
	command = exec.Command("mplayer", "-quiet", stations[stationIdx].url)
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

	go func() {
		debounceWrite(saveLaststationIdx)
	}()
}

// removes unneeded/unwanted strings from the title like " (CDM EDIT)" etc.
func removeNoise(title string) string {
	opening := strings.Index(title, "(")
	closing := strings.Index(title, ")")
	// text must be enclosed by round brackets
	if opening >= 0 && closing >= 0 && closing > opening {
		var remove bool = false
		// fmt.Println("removing noise...")
		noise := strings.ToLower(title[opening+1 : closing])
		if len(noise) > 0 {
			// fmt.Println("noise:", noise)
			if strings.Contains(noise, "edit") ||
				strings.Contains(noise, "mix") ||
				strings.Contains(noise, "cdm") ||
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
	scrollSpeedPtr = flag.Int("scrollSpeed", 500, "scroll speed in ms (100ms...10000ms)")
	scrollStationPtr = flag.Bool("scrollStation", false, "set to scroll station names")
	flag.Parse()
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
	p17 := gpioreg.ByName("GPIO17")
	if p17 == nil {
		logger.Error("Failed to find GPIO17")
	}
	if err := p17.In(gpio.PullUp, gpio.NoEdge); err != nil {
		check(err)
	}

	p22 := gpioreg.ByName("GPIO22")
	if p22 == nil {
		logger.Error("Failed to find GPIO22")
	}
	if err := p22.In(gpio.PullUp, gpio.NoEdge); err != nil {
		check(err)
	}

	p23 := gpioreg.ByName("GPIO23")
	if p23 == nil {
		logger.Error("Failed to find GPIO23")
	}
	if err := p23.In(gpio.PullUp, gpio.NoEdge); err != nil {
		check(err)
	}

	p27 := gpioreg.ByName("GPIO27")
	if p27 == nil {
		logger.Error("Failed to find GPIO27")
	}
	if err := p27.In(gpio.PullUp, gpio.NoEdge); err != nil {
		check(err)
	}

	var statusChan = make(chan string)
	var ctrlChan = make(chan os.Signal)
	var stationMutex = &sync.Mutex{}
	var volumeMutex = &sync.Mutex{}

	debounceBtn := debouncer.New(debounceTime * time.Millisecond)
	debounceWrite = debouncer.New(15 * time.Second)

	// the following 4 functions handle the pressed buttons
	fp17 := func() {
		stationMutex.Lock()
		stationIdx-- // previous station
		if stationIdx < 0 {
			stationIdx = len(stations) - 1
		}
		newStation()
		stationMutex.Unlock()
	}
	fp22 := func() {
		volumeMutex.Lock()
		_, err = inPipe.Write([]byte("*")) // increase volume
		volumeMutex.Unlock()
		check(err)
	}
	fp23 := func() {
		volumeMutex.Lock()
		_, err = inPipe.Write([]byte("/")) // decrease volume
		volumeMutex.Unlock()
		check(err)
	}
	fp27 := func() {
		stationMutex.Lock()
		stationIdx++ // next station
		stationIdx = stationIdx % len(stations)
		newStation()
		stationMutex.Unlock()
	}

	signal.Notify(ctrlChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL)

	stations = loadStations(filepath.Join(homePath, "stations"))
	stationIdx = getLaststationIdx()

	// this function is polling the GPIO Levels and calls the debouncer when a Low-Level is found (pull up resistor)
	go func() {
		for {
			switch {
			case p27.Read() == false:
				debounceBtn(fp27) // next station
			case p17.Read() == false:
				debounceBtn(fp17) // previous station
			case p22.Read() == false:
				debounceBtn(fp22) // increase volume
			case p23.Read() == false:
				debounceBtn(fp23) // decrease volume
			}
			time.Sleep(70 * time.Millisecond)
		}
	}()

	// this goroutine is waiting for someone to stop piradio
	go func() {
		<-ctrlChan
		logger.Trace("Ctrl+C received... Exiting")
		close(statusChan)
		close(pipeChan)
		os.Exit(1)
	}()

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
	fp27()

	go func() {
		// In order to show the volume level, it's neccessary to 'press' once 'decrease volume'
		// and 'increase volume'. We're waiting 5 seconds to give mplayer enough time to
		// initialize and get ready to receive commands.
		time.Sleep(5 * time.Second)
		_, _ = inPipe.Write([]byte("/"))
		_, _ = inPipe.Write([]byte("*"))
	}()

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
					printBitrateVolume(3, bitrate, volume)
				}
			}
			if strings.Index(line, "Volume:") >= 0 {
				volumeArr := strings.Split(line, ":")
				if len(volumeArr) > 1 {
					var format string
					if charsPerLine < 20 {
						format = "V %s%%"
					} else {
						format = "Vol %s%%"
					}
					volume = fmt.Sprintf(format, strings.Split(strings.Trim(volumeArr[1], " \n"), " ")[0])
					printBitrateVolume(3, bitrate, volume)
				}
			}
		}
	}
}
