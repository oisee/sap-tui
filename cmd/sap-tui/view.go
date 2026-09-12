package main

import (
	"fmt"
	"strings"

	"github.com/oisee/sap-tui/internal/diag"
	"github.com/oisee/sap-tui/internal/tui"
)

// chrome is what the GUI keeps showing between screens: the window title,
// the menu bar and the toolbar arrive with a screen and stay until the next
// screen replaces them, so a frame that carries only a status message or a
// pushed animation frame is drawn under the chrome of the screen before it.
type chrome struct {
	title   string
	menus   []string
	toolbar []string
	sysid   string
	program string
	dynpro  string
	rows    int // dynpro size from VARINFO.06, 0 when unknown
	cols    int
	command       string // the standard-toolbar command field text
	commandActive bool   // it is being typed into
}

// update takes the chrome items out of a frame's items, keeping what was
// there for anything the frame does not carry, and returns the status message
// the frame carried (type 0 when none).
func (c *chrome) update(items []diag.Item) (msgType byte, msg string) {
	var menus, toolbar []string
	sawMenu, sawToolbar := false, false
	for _, it := range items {
		switch {
		case it.Type == diag.ItemAPPL && it.ID == 0x0c && it.SID == 0x0a: // VARINFO.0a window title
			c.title = trimField(it.Value)
		case it.Type == diag.ItemAPPL && it.ID == 0x0c && it.SID == 0x03: // VARINFO.03 status message
			msgType, msg = diag.ParseStatusMessage(it.Value)
		case it.Type == diag.ItemAPPL && it.ID == 0x0c && it.SID == 0x06: // VARINFO.06 layout
			if r, cc := diag.ParseLayout(it.Value); r > 0 && cc > 0 && r < 400 && cc < 1000 {
				c.rows, c.cols = r, cc
			}
		case it.Type == diag.ItemAPPL4 && it.ID == 0x0b && it.SID == 0x01: // MNUENTRY.01 menu bar
			sawMenu = true
			for _, e := range diag.ParseMenuEntries(it.Value) {
				if e.Text != "" {
					menus = append(menus, e.Text)
				}
			}
		case it.Type == diag.ItemAPPL4 && it.ID == 0x0b && it.SID == 0x03: // MNUENTRY.03 toolbar
			sawToolbar = true
			for _, e := range diag.ParseMenuEntries(it.Value) {
				if e.Text != "" {
					toolbar = append(toolbar, e.Text)
				}
			}
		case it.Type == diag.ItemAPPL && it.ID == 0x06:
			switch it.SID {
			case 0x24: // SYSID
				c.sysid = trimField(it.Value)
			case 0x0d: // program
				c.program = trimField(it.Value)
			case 0x0e: // dynpro
				c.dynpro = trimField(it.Value)
			}
		}
	}
	if sawMenu {
		c.menus = menus
	}
	if sawToolbar {
		c.toolbar = toolbar
	}
	return msgType, msg
}

// info is the right-hand end of the status bar: system, program and dynpro
// when known, then whatever note the caller adds.
func (c *chrome) info(extra string) string {
	var parts []string
	if c.sysid != "" {
		parts = append(parts, c.sysid)
	}
	if c.program != "" {
		p := c.program
		if c.dynpro != "" {
			p += " " + c.dynpro
		}
		parts = append(parts, p)
	}
	if extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, "  ")
}

// compose lays the canvas under the current chrome into a grid that fits
// the terminal (rows/cols 0 = as big as the canvas needs).
func (c *chrome) compose(canvas *tui.Grid, msgType byte, msg, extra string, rows, cols int) *tui.Grid {
	v := tui.View{
		Title:         c.title,
		Menus:         c.menus,
		Toolbar:       c.toolbar,
		Canvas:        canvas,
		MsgType:       msgType,
		Message:       msg,
		Info:          c.info(extra),
		Command:       c.command,
		CommandActive: c.commandActive,
	}
	return tui.Compose(v, rows, cols)
}

// canvasSize is the dynpro size to start the canvas at: the layout item's
// when the frame told us, else the classic 24x80. Render grows past it when
// the atoms need more.
func (c *chrome) canvasSize() (int, int) {
	if c.rows > 0 && c.cols > 0 {
		return c.rows, c.cols
	}
	return tui.DefaultRows, tui.DefaultCols
}

// frameCanvas renders the screen a frame carries: the classic list runs when
// present, else every DYNT_ATOM. A screen with sub-areas (the logon screen's
// field area and its Information box are two chains) ships more than one
// DYNT_ATOM, each placing its atoms in absolute screen cells, so they all
// composite onto one canvas the way the GUI layers them. ok is false when the
// frame has no screen.
func (c *chrome) frameCanvas(items []diag.Item) (canvas *tui.Grid, note string, ok bool) {
	rows, cols := c.canvasSize()
	if diag.HasListSegments(items) {
		segs := diag.ParseListItems(items)
		return tui.RenderList(segs, rows, cols), fmt.Sprintf("%d list runs", len(segs)), true
	}
	var atoms []diag.Atom
	found, partial := false, false
	for i := range items {
		it := items[i]
		if it.Type == diag.ItemAPPL4 && it.ID == 0x09 && it.SID == 0x02 {
			found = true
			a, aerr := diag.ParseDyntAtoms(it.Value)
			atoms = append(atoms, a...)
			if aerr != nil {
				partial = true
			}
		}
	}
	if !found {
		return nil, "", false
	}
	// Place each subscreen's atoms at its area origin, so a subscreen (the logon
	// Information box) renders inside its frame instead of at the top-left.
	atoms = diag.OffsetAtomsByArea(atoms, diag.AreaOrigins(items))
	note = fmt.Sprintf("%d atoms", len(atoms))
	if partial {
		note += " (partial)"
	}
	return tui.Render(atoms, rows, cols), note, true
}

// trimField reads a NUL/space-padded field value as a string.
func trimField(b []byte) string {
	return strings.Trim(string(b), " \x00")
}
