package tui

import "github.com/oisee/sap-tui/internal/diag"

// Style is how one cell is painted: 256-colour ANSI indices for foreground
// and background (0 = the terminal's default) and the bold / underline /
// reverse attributes. The zero Style is plain text.
type Style struct {
	Fg, Bg    uint8
	Bold      bool
	Underline bool
	Reverse   bool
}

// Cell is one character cell of a grid with its style.
type Cell struct {
	Ch    rune
	Style Style
}

// The palette is the SAP GUI "Signature"/Blue Crystal look mapped onto the
// xterm 256-colour cube: dark text on tinted backgrounds, the way the GUI
// paints list colours and controls. The numbers are chosen, not measured;
// change them here and every element follows.
var (
	// canvas chrome
	StyleTitle   = Style{Fg: 231, Bg: 24, Bold: true} // window title bar: white on SAP blue
	StyleMenu    = Style{Fg: 16, Bg: 254}             // menu bar: black on light grey
	StyleToolbar = Style{Fg: 16, Bg: 252}             // application toolbar
	StyleStatus  = Style{Fg: 16, Bg: 254}             // status bar
	StyleInfo    = Style{Fg: 240, Bg: 254}            // the right-hand system info on the status bar

	// dynpro elements
	StyleFrame  = Style{Fg: 246}                          // group box lines
	StyleButton = Style{Fg: 16, Bg: 250}                  // pushbutton face
	StyleInput  = Style{Fg: 16, Bg: 231, Underline: true} // editable field
	StyleProt   = Style{Fg: 16}                           // protected / output text
	StyleLabel  = Style{}                                 // plain label

	// list colours, indexed by the SAP list colour (SFE byte 1)
	listStyles = [8]Style{
		diag.ColOff:      {},
		diag.ColHeading:  {Fg: 16, Bg: 110},
		diag.ColNormal:   {Fg: 16, Bg: 189},
		diag.ColTotal:    {Fg: 16, Bg: 229},
		diag.ColKey:      {Fg: 16, Bg: 117},
		diag.ColPositive: {Fg: 16, Bg: 157},
		diag.ColNegative: {Fg: 16, Bg: 217},
		diag.ColGroup:    {Fg: 16, Bg: 183},
	}

	// status message types: the S/W/E/I of a VARINFO.03
	msgStyles = map[byte]Style{
		'S': {Fg: 28, Bg: 254, Bold: true},
		'W': {Fg: 130, Bg: 254, Bold: true},
		'E': {Fg: 231, Bg: 160, Bold: true},
		'I': {Fg: 24, Bg: 254, Bold: true},
		'A': {Fg: 231, Bg: 160, Bold: true},
		'X': {Fg: 231, Bg: 160, Bold: true},
	}
)

// ListStyle is the cell style for a SAP list colour; colours past the eight
// the GUI knows fall back to plain.
func ListStyle(color byte) Style {
	if int(color) < len(listStyles) {
		return listStyles[color]
	}
	return Style{}
}

// MessageStyle is the style of a status-bar message of the given type; an
// unknown type is shown like the rest of the status bar.
func MessageStyle(msgType byte) Style {
	if s, ok := msgStyles[msgType]; ok {
		return s
	}
	return StyleStatus
}

// MessageGlyph is the small picture the GUI puts before a status message:
// a tick, a warning triangle, a stop sign, an information i.
func MessageGlyph(msgType byte) string {
	switch msgType {
	case 'S':
		return "✔"
	case 'W':
		return "⚠"
	case 'E', 'A', 'X':
		return "✖"
	case 'I':
		return "ℹ"
	}
	return " "
}
