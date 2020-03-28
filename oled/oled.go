package oled

import (
	"image"
	"time"

	"github.com/aluedtke7/piradio/display"
	"github.com/antigloss/go/logger"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"periph.io/x/periph/conn/i2c"
	"periph.io/x/periph/conn/i2c/i2creg"
	"periph.io/x/periph/devices/ssd1306"
	"periph.io/x/periph/devices/ssd1306/image1bit"
	"periph.io/x/periph/host"
)

const (
	numChars = 18
	numLines = 4
	cmdClear = iota
	cmdClearline
	cmdPrintline
)

type oled struct {
	dev          *ssd1306.Dev
	img          *image1bit.VerticalLSB
	bus          i2c.BusCloser
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

func (o *oled) printLine(ofs int, text string) {
	f := basicfont.Face7x13
	lineOfs := 50 - ofs*16
	drawer := font.Drawer{
		Dst:  o.img,
		Src:  &image.Uniform{image1bit.On},
		Face: f,
		Dot:  fixed.P(0, o.img.Bounds().Dy()-lineOfs),
	}
	drawer.DrawString(text)
	if err := o.dev.Draw(o.dev.Bounds(), o.img, image.Point{}); err != nil {
		logger.Error(err.Error())
	}
}

func (o *oled) clearLine(ofs int) {
	if ofs < 0 || ofs >= numLines {
		return
	}
	lineOfs := 256 * ofs
	for i := 0; i < 256; i++ {
		o.img.Pix[i+lineOfs] = 0
	}
}

func (o *oled) runTicker(line int, text string) {
	o.ticker[line] = time.NewTicker(time.Duration(o.scrollSpeed) * time.Millisecond)
	s := text + "     "
	for range o.ticker[line].C {
		o.cmdChan <- command{
			cmd:      cmdPrintline,
			lineNum:  line,
			lineText: s,
		}
		s = s[1:] + s[:1]
	}
}

func (o *oled) printAndScrollLine(line int, text string) {
	line = line % numLines
	if o.ticker[line] != nil {
		o.ticker[line].Stop()
		o.ticker[line] = nil
	}
	if len(text) <= numChars {
		o.cmdChan <- command{
			cmd:      cmdPrintline,
			lineNum:  line,
			lineText: text,
		}
	} else {
		go o.runTicker(line, text)
	}
}

func (o *oled) commandHandler() {
	for {
		c := <-o.cmdChan
		switch c.cmd {
		case cmdClear:
			for i := 0; i < len(o.img.Pix); i++ {
				o.img.Pix[i] = 0
			}
			_ = o.dev.Draw(o.dev.Bounds(), o.img, image.Point{})
		case cmdClearline:
			o.clearLine(c.lineNum)
		case cmdPrintline:
			o.clearLine(c.lineNum)
			o.printLine(c.lineNum, c.lineText)
		}
	}
}

func (o *oled) Backlight(on bool) {
	// nothing to do here: OLEDs don't have a backlight
}

func (o *oled) ClearLine(ofs int) {
	o.cmdChan <- command{
		cmd:     cmdClearline,
		lineNum: ofs,
	}
}

func (o *oled) Clear() {
	o.cmdChan <- command{
		cmd: cmdClear,
	}
}

func (o *oled) Close() {
	if o.bus != nil {
		for i := 0; i < numLines; i++ {
			if o.ticker[i] != nil {
				o.ticker[i].Stop()
				o.ticker[i] = nil
			}
		}
		_ = o.bus.Close()
	}
}

func (o *oled) PrintLine(line int, text string, scroll bool) {
	if scroll {
		o.printAndScrollLine(line, text)
	} else {
		if o.ticker[line] != nil {
			o.ticker[line].Stop()
			o.ticker[line] = nil
		}
		o.cmdChan <- command{
			cmd:      cmdPrintline,
			lineNum:  line,
			lineText: text,
		}
	}
}

func (o *oled) GetCharsPerLine() int {
	return o.charsPerLine
}

/**
Initializes the OLED Display and returns the maximum char count per line
*/
func New(speed int) (disp display.Display, err error) {
	logger.Trace("OLED initializing...")
	o := oled{scrollSpeed: speed, charsPerLine: numChars, cmdChan: make(chan command)}
	err = nil

	// Make sure periph is initialized.
	if _, err = host.Init(); err != nil {
		logger.Error(err.Error())
		return &o, err
	}

	// Use i2creg I²C bus registry to find the first available I²C bus.
	o.bus, err = i2creg.Open("")
	if err != nil {
		logger.Error(err.Error())
		return &o, err
	}

	// Open a handle to a ssd1306 connected on the I²C bus:
	o.dev, err = ssd1306.NewI2C(o.bus, &ssd1306.DefaultOpts)
	if err != nil {
		logger.Error(err.Error())
		return &o, err
	}

	o.img = image1bit.NewVerticalLSB(o.dev.Bounds())

	go o.commandHandler()

	o.Clear()
	return &o, err
}
