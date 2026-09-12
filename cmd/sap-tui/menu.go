package main

import (
	"fmt"

	"github.com/oisee/sap-tui/internal/diag"
	"github.com/oisee/sap-tui/internal/tui"
)

// Menu-bar navigation. F10 (or a click on a title) opens the menu bar; the
// arrow keys walk the titles and the dropdown, Enter fires the highlighted
// item. It draws the open dropdown as an overlay on the composed grid, the way
// the command palette does — tcell has no menu widget, so the menu is our own
// cells. Firing an item goes through the same OK-code path as an F-key: an item
// with a matching on-screen pushbutton fires its code; a menu-only item (code
// 100) or one whose code is not on the wire can only be fired once the control
// framework is live (see fireFKey / KNOWLEDGE §8).

// openMenu enters menu-bar navigation, reading the menus from the persisted GUI
// status. It is a no-op when the screen carries no menus.
func (s *session) openMenu() {
	s.menus = diag.ParseMenus(s.statusItems)
	if len(s.menus) == 0 {
		s.msgType, s.msg = 'W', "this screen carries no menu"
		return
	}
	s.menuOpen = true
	s.menuIdx = 0
	s.menuItem = s.firstItem(0, +1)
}

func (s *session) closeMenu() { s.menuOpen = false }

// firstItem returns the first selectable item index of menu m searching in
// direction d (+1 from the top, -1 from the bottom), skipping separators; -1
// when the menu has none.
func (s *session) firstItem(m, d int) int {
	items := s.menus[m].Items
	if d > 0 {
		for i := range items {
			if !items[i].Separator {
				return i
			}
		}
	} else {
		for i := len(items) - 1; i >= 0; i-- {
			if !items[i].Separator {
				return i
			}
		}
	}
	return -1
}

// menuKey handles one key while the menu bar is open. It returns whether the
// menu consumed it.
func (s *session) menuKey(k key, cancel func()) bool {
	switch k.kind {
	case keyCtrlC:
		cancel()
	case keyEsc, keyFunc: // Esc or F10 again closes
		s.closeMenu()
	case keyLeft:
		s.menuIdx = (s.menuIdx - 1 + len(s.menus)) % len(s.menus)
		s.menuItem = s.firstItem(s.menuIdx, +1)
	case keyRight:
		s.menuIdx = (s.menuIdx + 1) % len(s.menus)
		s.menuItem = s.firstItem(s.menuIdx, +1)
	case keyDown:
		s.moveItem(+1)
	case keyUp:
		s.moveItem(-1)
	case keyEnter, keyRune: // Enter or Space fires
		if k.kind == keyRune && k.r != ' ' {
			return true
		}
		s.fireMenuItem()
	default:
		return true
	}
	s.redraw()
	return true
}

// moveItem moves the dropdown highlight by d, skipping separators and wrapping.
func (s *session) moveItem(d int) {
	items := s.menus[s.menuIdx].Items
	if len(items) == 0 {
		return
	}
	i := s.menuItem
	for n := 0; n < len(items); n++ {
		i = (i + d + len(items)) % len(items)
		if !items[i].Separator {
			s.menuItem = i
			return
		}
	}
}

// fireMenuItem fires the highlighted item: resolve its label to an on-screen
// pushbutton's OK-code and send it. A menu-only or unresolvable item explains
// why it cannot fire (the control framework is not live).
func (s *session) fireMenuItem() {
	if s.menuIdx >= len(s.menus) || s.menuItem < 0 || s.menuItem >= len(s.menus[s.menuIdx].Items) {
		return
	}
	it := s.menus[s.menuIdx].Items[s.menuItem]
	if it.HasSub {
		s.msgType, s.msg = 'W', fmt.Sprintf("%q opens a submenu (not yet navigable)", it.Text)
		return
	}
	if code, ok := s.fcodeForLabel(it.Text); ok {
		s.closeMenu()
		if err := s.sendPAI(code, -1); err != nil {
			s.msgType, s.msg = 'E', "send: "+err.Error()
		}
		return
	}
	s.closeMenu()
	s.msgType, s.msg = 'W', fmt.Sprintf("%q (fn#%d) has no code on this screen; UI_EVENT firing needs a live control", it.Text, it.Code)
}

// menuTitleCols returns the starting column of each menu-bar title, matching the
// chrome's layout (col 1, titles two blanks apart).
func (s *session) menuTitleCols() []int {
	cols := make([]int, len(s.menus))
	col := 1
	for i, m := range s.menus {
		cols[i] = col
		col += len([]rune(m.Title)) + 2
	}
	return cols
}

// drawMenu highlights the open title on the menu bar and draws its dropdown as
// an overlay below it, the highlighted item reversed.
func (s *session) drawMenu(g *tui.Grid) {
	if !s.menuOpen || s.menuIdx >= len(s.menus) {
		return
	}
	cols := s.menuTitleCols()
	title := s.menus[s.menuIdx].Title
	tc := cols[s.menuIdx]
	g.Restyle(1, tc, len([]rune(title)), func(st tui.Style) tui.Style {
		st.Reverse = true
		return st
	})
	// Build the dropdown lines and its width.
	items := s.menus[s.menuIdx].Items
	width := len([]rune(title)) + 2
	for _, it := range items {
		if w := len([]rune(it.Text)) + 4; w > width {
			width = w
		}
	}
	lines := make([]string, len(items))
	for i, it := range items {
		switch {
		case it.Separator:
			lines[i] = ""
		case it.HasSub:
			lines[i] = " " + it.Text + " ▸"
		default:
			lines[i] = " " + it.Text
		}
	}
	row := 2 // just below the menu bar
	col := tc - 1
	if col < 0 {
		col = 0
	}
	s.drawDropdown(g, row, col, width, lines, items)
}

// drawDropdown paints the bordered dropdown box and its items, the highlighted
// one reversed and separators as rules.
func (s *session) drawDropdown(g *tui.Grid, row, col, width int, lines []string, items []diag.MenuItem) {
	height := len(lines) + 2
	g.Overlay(row, col, width, height, "", nil, tui.StyleMenu)
	for i, ln := range lines {
		r := row + 1 + i
		if items[i].Separator {
			g.PutText(r, col+1, repeat('─', width-2), tui.StyleFrame)
			continue
		}
		g.PutText(r, col+1, ln, tui.StyleMenu)
		if i == s.menuItem {
			g.Restyle(r, col+1, width-2, func(st tui.Style) tui.Style {
				st.Reverse = true
				return st
			})
		}
	}
}

func repeat(r rune, n int) string {
	if n < 0 {
		n = 0
	}
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}

// menuClick handles a mouse click at composed-grid (row, col) while navigating
// or opening the menu bar. It returns whether the click was on the menu.
func (s *session) menuClick(row, col int) bool {
	// Row 1 is the menu bar: a click on a title opens (or switches to) it.
	if row == 1 {
		cols := s.menuTitleCols()
		for i, m := range s.menus {
			start := cols[i]
			if col >= start && col < start+len([]rune(m.Title)) {
				if !s.menuOpen {
					s.menus = diag.ParseMenus(s.statusItems)
				}
				s.menuOpen = true
				s.menuIdx = i
				s.menuItem = s.firstItem(i, +1)
				s.redraw()
				return true
			}
		}
	}
	if !s.menuOpen {
		return false
	}
	// A click inside the open dropdown selects that item.
	items := s.menus[s.menuIdx].Items
	if row >= 3 && row < 3+len(items) {
		i := row - 3
		if i >= 0 && i < len(items) && !items[i].Separator {
			s.menuItem = i
			s.fireMenuItem()
			s.redraw()
			return true
		}
	}
	// A click anywhere else closes the menu.
	s.closeMenu()
	s.redraw()
	return true
}
