package display

// Interface definition for LCD and OLED
type Display interface {
	Clear()
	ClearLine(ofs int)
	Close()
	GetCharsPerLine() int
	PrintLine(line int, text string, scroll bool)
}
