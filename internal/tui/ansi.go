package tui

import (
	"fmt"
	"strings"
)

// ANSI output: one escape sequence per run of equally styled cells, so a
// row of 120 plain cells costs 120 bytes and a coloured LED row a few dozen
// more. Colours are the 256-colour SGR (38;5;n / 48;5;n) every modern
// terminal speaks; 0 means the terminal's own default.

// sgr is the escape sequence that switches to st from plain.
func sgr(st Style) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	if st.Bold {
		b.WriteString(";1")
	}
	if st.Underline {
		b.WriteString(";4")
	}
	if st.Reverse {
		b.WriteString(";7")
	}
	if st.Fg != 0 {
		fmt.Fprintf(&b, ";38;5;%d", st.Fg)
	}
	if st.Bg != 0 {
		fmt.Fprintf(&b, ";48;5;%d", st.Bg)
	}
	b.WriteString("m")
	return b.String()
}

// ANSILine is row r of the grid as styled terminal text, padded or clipped
// to width cells (width <= 0 means the grid's own width), ending in a reset.
func (g *Grid) ANSILine(r, width int) string {
	if width <= 0 {
		width = g.Cols
	}
	var b strings.Builder
	cur := Style{}
	b.WriteString("\x1b[0m")
	for c := 0; c < width; c++ {
		cell := g.CellAt(r, c)
		if cell.Style != cur {
			b.WriteString(sgr(cell.Style))
			cur = cell.Style
		}
		b.WriteRune(cell.Ch)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// ANSI is the whole grid as terminal text, one line per row.
func (g *Grid) ANSI() string {
	lines := make([]string, g.Rows)
	for r := range lines {
		lines[r] = g.ANSILine(r, 0)
	}
	return strings.Join(lines, "\n")
}
