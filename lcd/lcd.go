package lcd

import (
	"time"

	"github.com/aluedtke7/piradio/display"
	log "github.com/antigloss/go/logger"
	device "github.com/d2r2/go-hd44780"
	"github.com/d2r2/go-i2c"
	"github.com/d2r2/go-logger"
)

const (
	numChars = 20
	numLines = 4
	cmdClear = iota
	cmdBacklightOn
	cmdBacklightOff
	cmdPrintline
)

type lcd struct {
	i2cbus       *i2c.I2C
	dev          *device.Lcd
	lines        [numLines]device.ShowOptions
	ticker       [numLines]*time.Ticker
	cmdChan      chan command
	scrollSpeed  int
	charsPerLine int
}

type command struct {
	cmd      int
	lineNum  int
	lineText string
}

func (l *lcd) printLine(line int, text string) {
	if line >= 0 && line < numLines {
		if len(text) == 0 {
			text = " " // panic vermeiden, da die Bibliothek nicht mit leeren Strings umgehen kann
		}
		err := l.dev.ShowMessage(text, l.lines[line])
		if err != nil {
			log.Error(err.Error())
		}
	}
}

func (l *lcd) runTicker(line int, text string) {
	l.ticker[line] = time.NewTicker(time.Duration(l.scrollSpeed) * time.Millisecond)
	s := text + "     "
	for range l.ticker[line].C {
		l.cmdChan <- command{
			cmd:      cmdPrintline,
			lineNum:  line,
			lineText: s,
		}
		s = s[1:] + s[:1]
	}
}

func (l *lcd) printAndScrollLine(line int, text string) {
	line = line % numLines
	if l.ticker[line] != nil {
		l.ticker[line].Stop()
		l.ticker[line] = nil
	}
	if len(text) <= numChars {
		l.cmdChan <- command{
			cmd:      cmdPrintline,
			lineNum:  line,
			lineText: text,
		}
	} else {
		go l.runTicker(line, text)
	}
}

func (l *lcd) commandHandler() {
	for {
		c := <-l.cmdChan
		switch c.cmd {
		case cmdClear:
			err := l.dev.Clear()
			if err != nil {
				log.Error(err.Error())
			}
			time.Sleep(100 * time.Millisecond)
		case cmdBacklightOn:
			err := l.dev.BacklightOn()
			if err != nil {
				log.Error(err.Error())
			}
		case cmdBacklightOff:
			err := l.dev.BacklightOff()
			if err != nil {
				log.Error(err.Error())
			}
		case cmdPrintline:
			l.printLine(c.lineNum, c.lineText)
		}
	}
}

func (l *lcd) Backlight(on bool) {
	if on {
		l.cmdChan <- command{
			cmd: cmdBacklightOn,
		}
	} else {
		l.cmdChan <- command{
			cmd: cmdBacklightOff,
		}
	}
}

func (l *lcd) ClearLine(ofs int) {
	// dummy function, not needed for lcd
}

func (l *lcd) Clear() {
	l.cmdChan <- command{
		cmd: cmdClear,
	}
}

func (l *lcd) Close() {
	if l.i2cbus != nil {
		for i := 0; i < numLines; i++ {
			if l.ticker[i] != nil {
				l.ticker[i].Stop()
				l.ticker[i] = nil
			}
		}
		time.Sleep(2 * time.Second)
		_ = l.i2cbus.Close()
	}
}

func (l *lcd) PrintLine(line int, text string, scroll bool) {
	if scroll {
		l.printAndScrollLine(line, text)
	} else {
		if l.ticker[line] != nil {
			l.ticker[line].Stop()
			l.ticker[line] = nil
		}
		l.cmdChan <- command{
			cmd:      cmdPrintline,
			lineNum:  line,
			lineText: text,
		}
	}
}

func (l *lcd) GetCharsPerLine() int {
	return l.charsPerLine
}

/**
Initializes the LC-Display and returns the maximum char count per line
*/
func New(scrollHeader bool, speed int, initDelay int) (disp display.Display, err error) {
	log.Trace("LCD initializing...")
	_ = logger.ChangePackageLogLevel("i2c", logger.WarnLevel)
	l := lcd{scrollSpeed: speed, charsPerLine: numChars, cmdChan: make(chan command)}
	err = nil

	l.lines[0] = device.SHOW_LINE_1 | device.SHOW_BLANK_PADDING
	if !scrollHeader {
		l.lines[0] |= device.SHOW_ELIPSE_IF_NOT_FIT
	}
	l.lines[1] = device.SHOW_LINE_2 | device.SHOW_BLANK_PADDING
	l.lines[2] = device.SHOW_LINE_3 | device.SHOW_BLANK_PADDING
	l.lines[3] = device.SHOW_LINE_4 | device.SHOW_BLANK_PADDING

	l.i2cbus, err = i2c.NewI2C(0x27, 1)
	if err != nil {
		log.Error(err.Error())
		return &l, err
	}
	time.Sleep(3 * time.Second)

	l.dev, err = device.NewLcd(l.i2cbus, device.LCD_20x4)
	if err != nil {
		log.Error(err.Error())
		return &l, err
	}
	time.Sleep(time.Duration(initDelay) * time.Second)

	go l.commandHandler()

	l.Clear()
	l.Backlight(true)
	return &l, err
}
