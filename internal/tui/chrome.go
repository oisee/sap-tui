package tui

import (
	"strings"
)

// View is one SAP GUI window as the terminal shows it: the chrome the GUI
// draws round every dynpro — title bar, menu bar, application toolbar and
// status bar — and the canvas (the dynpro or list) inside it. Compose lays
// them out top to bottom in one grid, the way the GUI does, so a screen
// drawn here has the same rows and columns as the real one below the same
// chrome.
type View struct {
	// Title is the window title (VARINFO.0a), shown on the title bar.
	Title string
	// Menus are the menu bar titles (MNUENTRY.01) in order.
	Menus []string
	// Toolbar are the application toolbar captions (MNUENTRY.03), icons
	// included as @XX@ tokens.
	Toolbar []string
	// Canvas is the rendered dynpro or list.
	Canvas *Grid
	// MsgType and Message are the status-bar message (VARINFO.03): the type
	// S/W/E/I and the text. An empty Message leaves the bar's left side blank.
	MsgType byte
	Message string
	// Info is the right-hand end of the status bar: system, client, program.
	Info string
	// Command is the text in the standard-toolbar command field (the OK-code /
	// transaction entry). CommandActive highlights it while it is being typed.
	Command       string
	CommandActive bool
}

// ChromeRows is how many rows Compose adds round the canvas: title, menu, the
// standard toolbar (with the command field) and the application toolbar above;
// the status bar below.
const ChromeRows = 5

// Standard-toolbar layout: the Enter button, then the command field.
const (
	stdToolbarRow = 2
	stdEnterCol   = 1
	stdCmdCol     = 4
	stdCmdWidth   = 20
)

// CommandFieldSpan is the column range of the standard-toolbar command field,
// so a click there can focus it. EnterButtonCol is where the Enter (✓) button
// sits.
func CommandFieldSpan() (col, width, row int) { return stdCmdCol, stdCmdWidth, stdToolbarRow }
func EnterButtonCol() (col, row int)          { return stdEnterCol, stdToolbarRow }

// Compose lays the view into one grid of exactly rows by cols cells (a
// non-positive value takes what the canvas needs), clipping a canvas that
// does not fit and padding one that is smaller. The canvas starts at row 3.
func Compose(v View, rows, cols int) *Grid {
	canvas := v.Canvas
	if canvas == nil {
		canvas = NewGrid(DefaultRows, DefaultCols)
	}
	if cols <= 0 {
		cols = canvas.Cols
	}
	if rows <= 0 {
		rows = canvas.Rows + ChromeRows
	}
	g := newGrid(rows, cols, Style{})

	// Title bar: the window title left, the way the GUI's frame shows it.
	g.fill(0, 0, cols, 1, ' ', StyleTitle)
	g.put(0, 1, v.Title, StyleTitle)

	// Menu bar: titles separated by two blanks.
	g.fill(1, 0, cols, 1, ' ', StyleMenu)
	col := 1
	for _, m := range v.Menus {
		g.put(1, col, m, StyleMenu)
		col += len([]rune(m)) + 2
	}

	// Standard toolbar: the Enter button, the command field, then the fixed GUI
	// buttons (Save, Back, Exit, Cancel, Print, Find). The command field is an
	// input box; the rest are glyphs.
	g.fill(stdToolbarRow, 0, cols, 1, ' ', StyleToolbar)
	g.put(stdToolbarRow, stdEnterCol, "✓", Style{Fg: 34, Bold: true})
	cmd := []rune(v.Command)
	if len(cmd) > stdCmdWidth {
		cmd = cmd[len(cmd)-stdCmdWidth:]
	}
	fieldSt := StyleInput
	if v.CommandActive {
		fieldSt.Reverse = true
	}
	for i := 0; i < stdCmdWidth; i++ {
		ch := '_'
		if i < len(cmd) {
			ch = cmd[i]
		}
		g.set(stdToolbarRow, stdCmdCol+i, Cell{ch, fieldSt})
	}
	col = stdCmdCol + stdCmdWidth + 2
	for _, b := range []struct {
		glyph rune
		fg    uint8
	}{{'▤', 24}, {'←', 34}, {'↑', 220}, {'✕', 160}, {'⎙', 240}, {'⌕', 240}} {
		g.set(stdToolbarRow, col, Cell{b.glyph, Style{Fg: b.fg}})
		col += 2
	}

	// Application toolbar: captions in button faces, a blank between.
	g.fill(3, 0, cols, 1, ' ', StyleToolbar)
	col = 1
	for _, t := range v.Toolbar {
		cells := expandIcons(" "+t+" ", StyleButton)
		g.putCells(3, col, cells)
		col += len(cells) + 1
	}

	// Canvas from row ChromeRows-1 down to the status bar.
	canvasRows := rows - ChromeRows
	for r := 0; r < canvasRows && r < canvas.Rows; r++ {
		for c := 0; c < cols && c < canvas.Cols; c++ {
			g.set(ChromeRows-1+r, c, canvas.cells[r][c])
		}
	}

	// Status bar: the message with its glyph on the left, the system info
	// on the right.
	sr := rows - 1
	g.fill(sr, 0, cols, 1, ' ', StyleStatus)
	if v.Message != "" {
		st := MessageStyle(v.MsgType)
		g.put(sr, 1, MessageGlyph(v.MsgType)+" "+v.Message, st)
	}
	if info := strings.TrimSpace(v.Info); info != "" {
		start := cols - len([]rune(info)) - 1
		if start < 1 {
			start = 1
		}
		g.put(sr, start, info, StyleInfo)
	}
	return g
}
