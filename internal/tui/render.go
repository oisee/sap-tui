// Package tui renders a DIAG screen onto a character grid with styles, the
// way SAP GUI lays it out cell for cell. A dynpro screen is a chain of
// diag.Atom read from a DYNT_ATOM item (Render); a classic ABAP list is a
// stream of positioned coloured text runs read from the SBA/SFE/SLC/VARINFO.0b
// items (RenderList). Compose puts the GUI chrome round a canvas: title bar,
// menu bar, toolbar, status bar. It is read-only: it turns items into cells
// and never encodes anything back. The network side lives in cmd/tui; the
// drawing lives here so it can be tested against synthetic atoms and segments
// with no capture and no connection.
package tui

import (
	"strings"

	"github.com/oisee/sap-tui/internal/diag"
)

// DefaultRows and DefaultCols are the classic dynpro size used when the
// screen's own size is not known.
const (
	DefaultRows = 24
	DefaultCols = 80
)

// Grid is a rectangle of styled character cells, addressed by 0-based row
// and column, the atoms already placed on it.
type Grid struct {
	Rows, Cols int
	cells      [][]Cell
}

// newGrid is a rows-by-cols grid filled with blanks in the given style.
func newGrid(rows, cols int, fill Style) *Grid {
	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}
	cells := make([][]Cell, rows)
	for i := range cells {
		row := make([]Cell, cols)
		for j := range row {
			row[j] = Cell{' ', fill}
		}
		cells[i] = row
	}
	return &Grid{Rows: rows, Cols: cols, cells: cells}
}

// NewGrid is a blank grid of the given size in the plain style.
func NewGrid(rows, cols int) *Grid { return newGrid(rows, cols, Style{}) }

// set writes one cell, dropping it when it falls off the grid. Placement is
// clipping only; it never grows the grid.
func (g *Grid) set(row, col int, c Cell) {
	if row < 0 || row >= g.Rows || col < 0 || col >= g.Cols {
		return
	}
	g.cells[row][col] = c
}

// put writes s starting at row, col in one style.
func (g *Grid) put(row, col int, s string, st Style) {
	for i, r := range []rune(s) {
		g.set(row, col+i, Cell{r, st})
	}
}

// putCells writes a run of cells starting at row, col.
func (g *Grid) putCells(row, col int, cs []Cell) {
	for i, c := range cs {
		g.set(row, col+i, c)
	}
}

// fill paints a width-by-height block with one character and style.
func (g *Grid) fill(row, col, width, height int, ch rune, st Style) {
	for r := row; r < row+height; r++ {
		for c := col; c < col+width; c++ {
			g.set(r, c, Cell{ch, st})
		}
	}
}

// box draws the outline of a width-by-height frame with box-drawing lines
// and its title inset on the top edge, as the GUI draws a group box.
func (g *Grid) box(row, col, width, height int, title string, st Style) {
	if width < 2 || height < 2 {
		// Too small for an outline: the title alone marks it.
		g.put(row, col, title, st)
		return
	}
	g.set(row, col, Cell{'┌', st})
	g.set(row, col+width-1, Cell{'┐', st})
	g.set(row+height-1, col, Cell{'└', st})
	g.set(row+height-1, col+width-1, Cell{'┘', st})
	for c := col + 1; c < col+width-1; c++ {
		g.set(row, c, Cell{'─', st})
		g.set(row+height-1, c, Cell{'─', st})
	}
	for r := row + 1; r < row+height-1; r++ {
		g.set(r, col, Cell{'│', st})
		g.set(r, col+width-1, Cell{'│', st})
	}
	if t := []rune(strings.TrimSpace(title)); len(t) > 0 {
		if len(t) > width-4 && width-4 > 0 {
			t = t[:width-4]
		}
		g.put(row, col+2, " "+string(t)+" ", Style{Fg: 16})
	}
}

// extent is the rows and columns an atom occupies, so the grid can grow to
// hold it: a frame or button its width and height, anything else its text.
func extent(a diag.Atom) (rows, cols int) {
	switch a.EType {
	case diag.AtomFrame, diag.AtomPushbutton:
		if a.Length > 0 {
			h := a.Height
			if h < 1 {
				h = 1
			}
			return a.Row + h, a.Col + a.Length
		}
	}
	s := atomText(a)
	if s == "" {
		return 0, 0
	}
	return a.Row + 1, a.Col + len([]rune(s))
}

// Render places every drawable atom onto a grid at least minRows by minCols.
// The grid is grown, never shrunk, so that no atom is clipped off the bottom
// or the right; a caller with a smaller terminal clips at draw time with Clip.
// A minRows or minCols below one falls back to the default dynpro size.
//
// Buttons draw first, then the fields and labels, then FRAMES LAST — a group
// box's outline stays a clean rectangle even when a subscreen's text is wider
// than the box and spills past its right edge (the SAP GUI keeps the box outline
// and lets the text stick out; drawing frames first let that text erase the
// border). Content genuinely inside a box is inset and never touches the border.
func Render(atoms []diag.Atom, minRows, minCols int) *Grid {
	rows, cols := minRows, minCols
	if rows < 1 {
		rows = DefaultRows
	}
	if cols < 1 {
		cols = DefaultCols
	}
	for _, a := range atoms {
		r, c := extent(a)
		if r > rows {
			rows = r
		}
		if c > cols {
			cols = c
		}
	}
	g := newGrid(rows, cols, Style{})
	for _, a := range atoms {
		if a.EType == diag.AtomPushbutton {
			g.drawAtom(a)
		}
	}
	for _, a := range atoms {
		if a.EType != diag.AtomFrame && a.EType != diag.AtomPushbutton {
			g.drawAtom(a)
		}
	}
	for _, a := range atoms {
		if a.EType == diag.AtomFrame {
			g.drawAtom(a)
		}
	}
	return g
}

// drawAtom paints one atom: the frame as an outlined box, the button as a
// filled face of its width and height with the caption centred, the rest as
// their text in the style of their kind.
func (g *Grid) drawAtom(a diag.Atom) {
	switch a.EType {
	case diag.AtomFrame:
		if a.Attr&diag.AttrInvisible != 0 {
			return
		}
		if a.Length > 0 && a.Height > 0 {
			g.box(a.Row, a.Col, a.Length, a.Height, a.Value(), StyleFrame)
		} else {
			g.put(a.Row, a.Col, a.Value(), Style{Fg: 16})
		}
	case diag.AtomPushbutton:
		if a.Attr&diag.AttrInvisible != 0 {
			return
		}
		if a.Length <= 0 {
			g.put(a.Row, a.Col, "["+a.Value()+"]", StyleButton)
			return
		}
		h := a.Height
		if h < 1 {
			h = 1
		}
		g.fill(a.Row, a.Col, a.Length, h, ' ', StyleButton)
		cap := expandIcons(a.Value(), StyleButton)
		if len(cap) > a.Length {
			cap = cap[:a.Length]
		}
		g.putCells(a.Row+(h-1)/2, a.Col+(a.Length-len(cap))/2, cap)
	case diag.AtomInputField:
		if a.Attr&diag.AttrInvisible != 0 && a.Attr&diag.AttrProtected != 0 {
			return
		}
		if a.Attr&diag.AttrProtected != 0 {
			g.put(a.Row, a.Col, a.Value(), StyleProt)
			return
		}
		g.put(a.Row, a.Col, inputText(a), StyleInput)
		if a.Attr&diag.AttrMatchcode != 0 {
			g.put(a.Row, a.Col+inputWidth(a), "▾", Style{Fg: 240})
		}
	default:
		s := atomText(a)
		if s == "" {
			return
		}
		st := StyleLabel
		if a.EType == diag.AtomOutputField {
			st = StyleProt
		}
		if a.Attr&diag.AttrIntensify != 0 {
			st.Bold = true
		}
		g.putCells(a.Row, a.Col, expandIcons(s, st))
	}
}

// RenderList places the text runs of a classic ABAP list onto a grid at least
// minRows by minCols. Each segment is drawn at its own row and column, the way
// the list stream positioned it, in the SAP list colour it carried; a ruled
// run is a horizontal line. The grid grows so no run is clipped off the bottom
// or the right. Runs are drawn in arrival order, so where the list overprints
// a cell the later run wins, as it does in the GUI.
func RenderList(segs []diag.ListSegment, minRows, minCols int) *Grid {
	rows, cols := minRows, minCols
	if rows < 1 {
		rows = DefaultRows
	}
	if cols < 1 {
		cols = DefaultCols
	}
	for _, s := range segs {
		if s.Text == "" {
			continue
		}
		if s.Row+1 > rows {
			rows = s.Row + 1
		}
		if end := s.Col + len([]rune(s.Text)); end > cols {
			cols = end
		}
	}
	g := newGrid(rows, cols, Style{})
	for _, s := range segs {
		if s.Text == "" {
			continue
		}
		st := ListStyle(s.Color)
		if s.Box() {
			// A box-drawing run: each byte is a SAP box code, one per cell.
			cells := make([]Cell, 0, len(s.Text))
			for i := 0; i < len(s.Text); i++ {
				cells = append(cells, Cell{diag.BoxGlyph(s.Text[i]), StyleFrame})
			}
			g.putCells(s.Row, s.Col, cells)
			continue
		}
		g.putCells(s.Row, s.Col, expandIcons(s.Text, st))
	}
	return g
}

// atomText is how one atom shows on the grid as plain text. Labels, output
// fields and the title of a frame are their trimmed value. An input field
// shows its value over a run of underscores that mark how wide the field is;
// a password field shows asterisks. A checkbox or radio button shows its
// state in a box before its label, a pushbutton its caption in brackets.
// Field-name and XML-property atoms carry no visible text and draw nothing.
func atomText(a diag.Atom) string {
	switch a.EType {
	case diag.AtomLabel, diag.AtomOutputField, diag.AtomFrame:
		if a.Attr&diag.AttrInvisible != 0 {
			return ""
		}
		return a.Value()
	case diag.AtomInputField:
		// The attribute bits change how a field looks: a protected one
		// shows its value without the editable underscores, an invisible
		// one masks its value, and a value-help field gets an F4 marker.
		if a.Attr&diag.AttrProtected != 0 {
			if a.Attr&diag.AttrInvisible != 0 {
				return ""
			}
			return a.Value()
		}
		t := inputText(a)
		if a.Attr&diag.AttrMatchcode != 0 {
			t += "▾"
		}
		return t
	case diag.AtomCheckbox:
		return checkBox(a.State) + " " + a.Value()
	case diag.AtomRadioButton:
		return radioBox(a.State) + " " + a.Value()
	case diag.AtomPushbutton:
		return "[" + a.Value() + "]"
	default:
		return ""
	}
}

// inputWidth is the on-screen width of an input field.
func inputWidth(a diag.Atom) int {
	w := a.VisibleLength
	if w <= 0 {
		w = a.MaxChars
	}
	if w <= 0 {
		w = a.Length
	}
	if w <= 0 {
		w = len([]rune(a.Value()))
	}
	return w
}

// inputText draws an input field as its value left-justified over underscores
// that show the field's on-screen width, so an empty field is still visible.
// An invisible (password) field shows one asterisk per character instead.
func inputText(a diag.Atom) string {
	w := inputWidth(a)
	v := []rune(a.Value())
	if len(v) > w {
		v = v[:w]
	}
	out := make([]rune, 0, w)
	if a.Attr&diag.AttrInvisible != 0 {
		for range v {
			out = append(out, '*')
		}
	} else {
		out = append(out, v...)
	}
	for len(out) < w {
		out = append(out, '_')
	}
	return string(out)
}

// checkBox is [X] when the state byte is 'X', else [ ].
func checkBox(state byte) string {
	if state == 'X' || state == 'x' {
		return "[X]"
	}
	return "[ ]"
}

// radioBox is (X) when the state byte is 'X', else ( ).
func radioBox(state byte) string {
	if state == 'X' || state == 'x' {
		return "(X)"
	}
	return "( )"
}

// TabSpan is where one tab of a tabstrip was drawn: its column range on the
// tab-bar row and the function code it fires when clicked.
type TabSpan struct {
	Col, Width int
	Fcode      string
	Active     bool
}

// DrawTabBar draws a tabstrip's tabs as "│ Caption " segments on the given row
// of the grid — the active tab bold and reversed — and returns each tab's
// column span so a caller can hit-test a click. A screen's exact tabstrip
// geometry is not reliably on the wire, so the bar is laid left to right from
// column 0; it is a faithful list of the tabs and which is active, not a
// pixel-match of the GUI's placement.
func DrawTabBar(g *Grid, tabs []diag.Tab, row int) []TabSpan {
	if len(tabs) == 0 || row < 0 || row >= g.Rows {
		return nil
	}
	var spans []TabSpan
	col := 0
	for _, t := range tabs {
		label := t.Caption
		seg := "│ " + label + " "
		st := StyleFrame
		if t.Active {
			st = Style{Bold: true, Reverse: true}
		}
		g.put(row, col, seg, st)
		w := len([]rune(seg))
		spans = append(spans, TabSpan{Col: col, Width: w, Fcode: t.Fcode, Active: t.Active})
		col += w
	}
	g.set(row, col, Cell{'│', StyleFrame})
	return spans
}

// Line is one row of the grid as a string, blanks included.
func (g *Grid) Line(row int) string {
	if row < 0 || row >= g.Rows {
		return ""
	}
	var b strings.Builder
	for _, c := range g.cells[row] {
		b.WriteRune(c.Ch)
	}
	return b.String()
}

// At is the rune at row, col, or a blank when out of range.
func (g *Grid) At(row, col int) rune {
	return g.CellAt(row, col).Ch
}

// CellAt is the cell at row, col, or a blank plain cell when out of range.
func (g *Grid) CellAt(row, col int) Cell {
	if row < 0 || row >= g.Rows || col < 0 || col >= g.Cols {
		return Cell{' ', Style{}}
	}
	return g.cells[row][col]
}

// Clip returns a copy no larger than maxRows by maxCols, so a screen wider or
// taller than the terminal loses its overflow rather than wrapping. A maximum
// of zero or less on either axis leaves that axis unclipped.
func (g *Grid) Clip(maxRows, maxCols int) *Grid {
	rows, cols := g.Rows, g.Cols
	if maxRows > 0 && maxRows < rows {
		rows = maxRows
	}
	if maxCols > 0 && maxCols < cols {
		cols = maxCols
	}
	out := newGrid(rows, cols, Style{})
	for r := 0; r < rows; r++ {
		copy(out.cells[r], g.cells[r][:cols])
	}
	return out
}

// String is the whole grid, one row per line, each row's trailing blanks
// trimmed. It is what a plain terminal prints; styles are dropped.
func (g *Grid) String() string {
	var b strings.Builder
	for i := 0; i < g.Rows; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(g.Line(i), " "))
	}
	return b.String()
}

// PutText writes s at (row, col) in one style, clipped to the grid. It is the
// exported way for a caller in another package to draw onto the grid (an
// overlay such as the menu dropdown).
func (g *Grid) PutText(row, col int, s string, st Style) { g.put(row, col, s, st) }

// Restyle applies f to the style of width cells from (row, col), clipped to
// the grid. A caller marks keyboard focus with it without redrawing the atom.
func (g *Grid) Restyle(row, col, width int, f func(Style) Style) {
	if row < 0 || row >= g.Rows {
		return
	}
	for c := col; c < col+width && c < g.Cols; c++ {
		if c < 0 {
			continue
		}
		g.cells[row][c].Style = f(g.cells[row][c].Style)
	}
}

// Overlay draws a bordered box of text lines at (row, col) over the grid, in
// the given style, clipping lines to the box. It is for transient UI such as a
// command list drawn on top of a screen.
func (g *Grid) Overlay(row, col, width, height int, title string, lines []string, st Style) {
	if width < 2 || height < 2 {
		return
	}
	for r := row; r < row+height && r < g.Rows; r++ {
		for c := col; c < col+width && c < g.Cols; c++ {
			if r >= 0 && c >= 0 {
				g.cells[r][c] = Cell{' ', st}
			}
		}
	}
	g.box(row, col, width, height, title, st)
	for i, ln := range lines {
		if i >= height-2 {
			break
		}
		r := []rune(ln)
		if len(r) > width-2 {
			r = r[:width-2]
		}
		g.put(row+1+i, col+1, string(r), st)
	}
}
