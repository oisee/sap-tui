package main

import (
	"fmt"

	"github.com/oisee/sap-tui/internal/diag"
	"github.com/oisee/sap-tui/internal/tui"
)

// The command palette: the whole command set a screen offers, read straight
// from its GUI status — the application toolbar (MNUENTRY.03), the function
// keys (.04) and the dropdown menus (.02). It is a reference overlay: it shows
// what the screen can do, how each command is labelled, and the keystroke that
// fires it (joined from the screen's accelerator table, ST_R3INFO.13). A
// function key listed here works directly; the client resolves it to its
// function number and fires it (see session.fkeyFuncs, UI_EVENT_SOURCE).

// commandList builds the palette lines from a screen's MNUENTRY items. Each
// entry is annotated with the keystroke that fires it, joined from the screen's
// accelerator table by function number (so a toolbar or menu command shows its
// F-key or Ctrl-shortcut, and F-keys are discoverable without a manual binding).
func commandList(items []diag.Item, keys map[int]string) []string {
	var out []string
	add := func(sid byte, label string) {
		for _, it := range items {
			if it.Type != diag.ItemAPPL4 || it.ID != 0x0b || it.SID != sid {
				continue
			}
			for _, e := range diag.ParseMenuEntries(it.Value) {
				if e.Separator() || e.Text == "" {
					continue
				}
				line := fmt.Sprintf("%-9s %s", label, e.Text)
				if k := keys[int(e.Code)]; k != "" {
					line += "  [" + k + "]"
				} else if e.Accel != 0 {
					line += fmt.Sprintf("  (Alt+%c)", e.Accel)
				}
				if e.Tooltip != "" && e.Tooltip != e.Text {
					line += "  — " + e.Tooltip
				}
				out = append(out, line)
			}
		}
	}
	add(0x03, "[toolbar]")
	add(0x04, "[key]")
	add(0x02, "[menu]")
	if len(out) == 0 {
		out = []string{"(this screen carries no GUI status)"}
	}
	return out
}

// drawPalette overlays the command list on the composed grid g, scrolled by
// s.palScroll. The box is centred and sized to the terminal.
func (s *session) drawPalette(g *tui.Grid) {
	lines := s.palLines
	w := 72
	if w > g.Cols-2 {
		w = g.Cols - 2
	}
	h := len(lines) + 2
	if h > g.Rows-4 {
		h = g.Rows - 4
	}
	if h < 3 {
		h = 3
	}
	visible := h - 2
	if s.palScroll > len(lines)-visible {
		s.palScroll = len(lines) - visible
	}
	if s.palScroll < 0 {
		s.palScroll = 0
	}
	end := s.palScroll + visible
	if end > len(lines) {
		end = len(lines)
	}
	shown := lines[s.palScroll:end]
	title := fmt.Sprintf("Commands (%d) — Ctrl+P/Esc close, up/down scroll", len(lines))
	row := (g.Rows - h) / 2
	col := (g.Cols - w) / 2
	if row < 0 {
		row = 0
	}
	if col < 0 {
		col = 0
	}
	g.Overlay(row, col, w, h, title, shown, tui.StyleMenu)
}
