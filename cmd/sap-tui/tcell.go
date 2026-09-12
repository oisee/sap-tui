package main

import (
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/oisee/sap-tui/internal/diag"
	"github.com/oisee/sap-tui/internal/tui"
)

// The tcell backend for interactive mode. tcell owns the real terminal — raw
// mode, a double-buffered cell screen, resize events, mouse, and proper key
// decoding — while pkg/tui.Grid stays the render buffer we compose screens
// into. blit copies a composed Grid onto the tcell screen; tcellKey maps a key
// event onto our key model; the run loop turns resize and mouse into redraws
// and focus.

// tcellStyle maps a pkg/tui cell style (256-colour indices, 0 = default) to a
// tcell style.
func tcellStyle(st tui.Style) tcell.Style {
	s := tcell.StyleDefault
	if st.Fg != 0 {
		s = s.Foreground(tcell.PaletteColor(int(st.Fg)))
	}
	if st.Bg != 0 {
		s = s.Background(tcell.PaletteColor(int(st.Bg)))
	}
	return s.Bold(st.Bold).Underline(st.Underline).Reverse(st.Reverse)
}

// blit draws grid g onto the screen, clipped to the screen size.
func blit(scr tcell.Screen, g *tui.Grid) {
	w, h := scr.Size()
	for r := 0; r < g.Rows && r < h; r++ {
		for c := 0; c < g.Cols && c < w; c++ {
			cell := g.CellAt(r, c)
			ch := cell.Ch
			if ch == 0 {
				ch = ' '
			}
			scr.SetContent(c, r, ch, nil, tcellStyle(cell.Style))
		}
	}
}

// tcellKey maps a tcell key event to our key model. ok is false for a key we
// do not handle.
func tcellKey(e *tcell.EventKey) (key, bool) {
	switch e.Key() {
	case tcell.KeyRune:
		return key{kind: keyRune, r: e.Rune()}, true
	case tcell.KeyEnter:
		return key{kind: keyEnter}, true
	case tcell.KeyTab:
		return key{kind: keyTab}, true
	case tcell.KeyBacktab:
		return key{kind: keyBackTab}, true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return key{kind: keyBackspace}, true
	case tcell.KeyDelete:
		return key{kind: keyDelete}, true
	case tcell.KeyLeft:
		return key{kind: keyLeft}, true
	case tcell.KeyRight:
		return key{kind: keyRight}, true
	case tcell.KeyUp:
		return key{kind: keyUp}, true
	case tcell.KeyDown:
		return key{kind: keyDown}, true
	case tcell.KeyHome:
		if e.Modifiers()&tcell.ModCtrl != 0 {
			return key{kind: keyCtrlHome}, true
		}
		return key{kind: keyHome}, true
	case tcell.KeyEnd:
		if e.Modifiers()&tcell.ModCtrl != 0 {
			return key{kind: keyCtrlEnd}, true
		}
		return key{kind: keyEnd}, true
	case tcell.KeyPgUp:
		return key{kind: keyPgUp}, true
	case tcell.KeyPgDn:
		return key{kind: keyPgDn}, true
	case tcell.KeyEscape:
		return key{kind: keyEsc}, true
	case tcell.KeyCtrlC:
		return key{kind: keyCtrlC}, true
	case tcell.KeyCtrlO:
		return key{kind: keyCtrlO}, true
	case tcell.KeyCtrlP:
		return key{kind: keyCtrlP}, true
	}
	if e.Key() >= tcell.KeyF1 && e.Key() <= tcell.KeyF24 {
		return key{kind: keyFunc, n: int(e.Key()-tcell.KeyF1) + 1}, true
	}
	return key{}, false
}

// handleTcellEvent dispatches one tcell event: a key through the key model, a
// resize into a repaint, a mouse click into focus or activation.
func (s *session) handleTcellEvent(ev tcell.Event, cancel func()) error {
	switch e := ev.(type) {
	case *tcell.EventResize:
		s.screen.Sync()
		s.redraw()
	case *tcell.EventKey:
		if k, ok := tcellKey(e); ok {
			return s.handleKey(k, cancel)
		}
	case *tcell.EventMouse:
		s.handleMouse(e)
	}
	return nil
}

// handleMouse turns a left click into focus (or a pushbutton press) on the
// canvas, and the wheel into palette scrolling.
func (s *session) handleMouse(e *tcell.EventMouse) {
	btn := e.Buttons()
	if s.palette {
		switch {
		case btn&tcell.WheelUp != 0:
			s.palScroll--
			s.redraw()
		case btn&tcell.WheelDown != 0:
			s.palScroll++
			s.redraw()
		}
		return
	}
	click := btn&tcell.Button1 != 0 && s.prevBtn&tcell.Button1 == 0
	s.prevBtn = btn
	if !click || s.scr == nil {
		return
	}
	x, y := e.Position()
	// The menu bar (composed-grid row 1) and its open dropdown take clicks
	// first, so a click there opens/navigates the menu instead of the canvas.
	if len(diag.ParseMenus(s.statusItems)) > 0 && s.menuClick(y, x) {
		return
	}
	// The standard toolbar: a click on the command field focuses it, a click on
	// the Enter button submits it (the way the SAP GUI command bar works).
	if cmdCol, cmdW, cmdRow := tui.CommandFieldSpan(); y == cmdRow && x >= cmdCol && x < cmdCol+cmdW {
		s.scr.inCmd = true
		s.redraw()
		return
	}
	if ec, er := tui.EnterButtonCol(); y == er && x == ec {
		cmd := strings.TrimSpace(s.scr.cmd)
		s.scr.inCmd, s.scr.cmd = false, ""
		if err := s.sendPAI(cmd, -1); err != nil {
			s.msgType, s.msg = 'E', "send: "+err.Error()
		}
		s.redraw()
		return
	}
	if s.hasList {
		return
	}
	cr, cc := y-tui.ChromeRows+1, x // canvas is drawn from screen row 3
	// A click on the tab bar switches tabs: fire the tab's function code (an
	// OK-code, which the server acts on).
	if cr == tabBarRow {
		for _, t := range s.scr.tabHits {
			if cc >= t.Col && cc < t.Col+t.Width && t.Fcode != "" && !t.Active {
				if err := s.sendPAI(t.Fcode, -1); err != nil {
					s.msgType, s.msg = 'E', "send: "+err.Error()
				}
				s.redraw()
				return
			}
		}
	}
	for i := range s.scr.fields {
		f := s.scr.fields[i]
		if f.row == cr && cc >= f.col && cc < f.col+f.width {
			s.scr.focus = i
			s.scr.syncCaret()
			if f.kind == fButton {
				if err := s.sendPAI(f.fcode, -1); err != nil {
					s.msgType, s.msg = 'E', "send: "+err.Error()
				}
			}
			s.redraw()
			return
		}
	}
}

// drawTcell composes the screen to the tcell size, draws the focus highlight
// and any palette overlay, blits it, and places or hides the cursor.
func (s *session) drawTcell(canvas *tui.Grid, msgType byte, msg, note string) {
	w, h := s.screen.Size()
	g := s.chrome.compose(canvas, msgType, msg, note, h, w)
	cr, cc := 0, 0
	if s.scr != nil && s.scr.inCmd {
		// The caret sits in the standard-toolbar command field.
		cmdCol, cmdW, cmdRow := tui.CommandFieldSpan()
		n := len([]rune(s.scr.cmd))
		if n > cmdW-1 {
			n = cmdW - 1
		}
		cr, cc = cmdRow+1, cmdCol+n+1
	} else if s.scr != nil && !s.hasList {
		cr, cc = s.scr.markFocus(g, tui.ChromeRows-1)
	}
	if s.menuOpen {
		s.drawMenu(g)
		cr, cc = 0, 0
	}
	if s.palette {
		s.drawPalette(g)
		cr, cc = 0, 0
	}
	s.screen.Clear()
	blit(s.screen, g)
	if cr > 0 {
		s.screen.ShowCursor(cc-1, cr-1)
	} else {
		s.screen.HideCursor()
	}
	s.screen.Show()
}
