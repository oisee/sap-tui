package tui

import (
	"strings"
	"testing"

	"github.com/oisee/sap-tui/internal/diag"
)

// at reads the string of length n starting at row, col off the grid.
func at(g *Grid, row, col, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteRune(g.At(row, col+i))
	}
	return b.String()
}

// A label lands on the row and column it was given, one-to-one, with nothing
// spilling into the cells before it.
func TestRenderLabelPlacement(t *testing.T) {
	atoms := []diag.Atom{diag.Label(2, 5, "Hello")}
	g := Render(atoms, DefaultRows, DefaultCols)
	if g.Rows != DefaultRows || g.Cols != DefaultCols {
		t.Fatalf("grid size = %dx%d, want %dx%d", g.Rows, g.Cols, DefaultRows, DefaultCols)
	}
	if got := at(g, 2, 5, 5); got != "Hello" {
		t.Errorf("label at (2,5) = %q, want %q", got, "Hello")
	}
	if got := g.At(2, 4); got != ' ' {
		t.Errorf("cell before the label = %q, want blank", string(got))
	}
	if line := g.Line(0); strings.TrimSpace(line) != "" {
		t.Errorf("row 0 should be blank, got %q", line)
	}
}

// Several elements each sit where they were placed and do not disturb each
// other: a label, an output value, an input field, a checkbox and a button.
func TestRenderMixedElements(t *testing.T) {
	atoms := []diag.Atom{
		diag.Label(0, 0, "User"),
		diag.OutputField(0, 10, 8, "42", true),
		diag.InputField(1, 0, 6, "AB"),
		{EType: diag.AtomCheckbox, Row: 2, Col: 0, State: 'X', Text: "On"},
		{EType: diag.AtomRadioButton, Row: 3, Col: 0, State: ' ', Text: "Off"},
		{EType: diag.AtomPushbutton, Row: 4, Col: 0, Text: "Save", Function: "=SAVE"},
	}
	g := Render(atoms, DefaultRows, DefaultCols)

	if got := at(g, 0, 0, 4); got != "User" {
		t.Errorf("label = %q, want %q", got, "User")
	}
	if got := at(g, 0, 10, 2); got != "42" {
		t.Errorf("output value = %q, want %q", got, "42")
	}
	// The input shows its value then underscores out to its width of six.
	if got := at(g, 1, 0, 6); got != "AB____" {
		t.Errorf("input field = %q, want %q", got, "AB____")
	}
	if got := at(g, 2, 0, 6); got != "[X] On" {
		t.Errorf("checkbox = %q, want %q", got, "[X] On")
	}
	if got := at(g, 3, 0, 7); got != "( ) Off" {
		t.Errorf("radio = %q, want %q", got, "( ) Off")
	}
	if got := at(g, 4, 0, 6); got != "[Save]" {
		t.Errorf("button = %q, want %q", got, "[Save]")
	}
}

// A field-name atom is metadata, not something on screen, so it draws nothing.
func TestRenderSkipsFieldName(t *testing.T) {
	atoms := []diag.Atom{diag.FieldName(0, 0, "GV_X", 0)}
	g := Render(atoms, DefaultRows, DefaultCols)
	if strings.TrimSpace(g.String()) != "" {
		t.Errorf("field-name atom drew something: %q", g.String())
	}
}

// The grid grows past the requested minimum to hold an atom that would fall
// off the bottom-right, so nothing placed is ever lost.
func TestRenderGrowsToFitAtom(t *testing.T) {
	atoms := []diag.Atom{diag.Label(30, 100, "Edge")}
	g := Render(atoms, DefaultRows, DefaultCols)
	if g.Rows < 31 {
		t.Errorf("rows = %d, want at least 31", g.Rows)
	}
	if g.Cols < 104 {
		t.Errorf("cols = %d, want at least 104", g.Cols)
	}
	if got := at(g, 30, 100, 4); got != "Edge" {
		t.Errorf("grown-into label = %q, want %q", got, "Edge")
	}
}

// A classic list renders its runs at the row and column the list stream gave
// them: a coloured header cell, a ruled line, and a data row whose key and
// value columns land where they were placed and overprint in arrival order.
func TestRenderListPlacement(t *testing.T) {
	segs := []diag.ListSegment{
		{Row: 4, Col: 0, Color: diag.ColHeading, Text: "idx"},
		{Row: 5, Col: 0, Attr: [3]byte{0x08, 0x00, 0x08}, Text: "4444"}, // SAP box code '4' = ─
		{Row: 6, Col: 0, Color: diag.ColKey, Text: "  1"},
		{Row: 6, Col: 29, Color: diag.ColOff, Text: "1"},
	}
	g := RenderList(segs, DefaultRows, DefaultCols)
	if got := at(g, 4, 0, 3); got != "idx" {
		t.Errorf("header = %q, want %q", got, "idx")
	}
	if got := at(g, 5, 0, 4); got != "────" {
		t.Errorf("ruled line = %q, want %q", got, "────")
	}
	if got := at(g, 6, 0, 3); got != "  1" {
		t.Errorf("key column = %q, want %q", got, "  1")
	}
	if got := g.At(6, 29); got != '1' {
		t.Errorf("value column = %q, want %q", string(got), "1")
	}
	// Rows above the first run stay blank.
	if line := g.Line(0); strings.TrimSpace(line) != "" {
		t.Errorf("row 0 should be blank, got %q", line)
	}
}

// Later runs overprint earlier ones at a shared cell, the way SAP's idx column
// and its "row" label overlap on the wire.
func TestRenderListOverprint(t *testing.T) {
	segs := []diag.ListSegment{
		{Row: 6, Col: 0, Text: "        1"}, // nine-wide idx cell, "10" clipped to one digit here
		{Row: 6, Col: 9, Text: "row"},
	}
	g := RenderList(segs, DefaultRows, DefaultCols)
	if got := at(g, 6, 9, 3); got != "row" {
		t.Errorf("overprinted cell = %q, want %q", got, "row")
	}
}

// A list grows the grid past the requested minimum so a run near the bottom
// right is not lost.
func TestRenderListGrows(t *testing.T) {
	segs := []diag.ListSegment{{Row: 40, Col: 100, Text: "end of list"}}
	g := RenderList(segs, DefaultRows, DefaultCols)
	if g.Rows < 41 || g.Cols < 111 {
		t.Fatalf("grid = %dx%d, want at least 41x111", g.Rows, g.Cols)
	}
	if got := at(g, 40, 100, 11); got != "end of list" {
		t.Errorf("grown-into run = %q, want %q", got, "end of list")
	}
}

// Clip drops the overflow of a screen larger than the terminal instead of
// wrapping it.
func TestClip(t *testing.T) {
	atoms := []diag.Atom{diag.Label(0, 0, "Left edge and more")}
	g := Render(atoms, DefaultRows, DefaultCols).Clip(10, 4)
	if g.Rows != 10 || g.Cols != 4 {
		t.Fatalf("clipped size = %dx%d, want 10x4", g.Rows, g.Cols)
	}
	if got := g.Line(0); got != "Left" {
		t.Errorf("clipped row 0 = %q, want %q", got, "Left")
	}
}

func TestRenderInputStates(t *testing.T) {
	atoms := []diag.Atom{
		{EType: diag.AtomInputField, Row: 0, Col: 0, Attr: diag.AttrYes3D, VisibleLength: 6, Text: "ab"},
		{EType: diag.AtomInputField, Row: 1, Col: 0, Attr: diag.AttrProtected, VisibleLength: 6, Text: "cd"},
		{EType: diag.AtomInputField, Row: 2, Col: 0, Attr: diag.AttrYes3D | diag.AttrInvisible, VisibleLength: 6, Text: "secret"},
		{EType: diag.AtomInputField, Row: 3, Col: 0, Attr: diag.AttrYes3D | diag.AttrMatchcode, VisibleLength: 4, Text: "x"},
	}
	g := Render(atoms, 5, 20)
	rows := g.String()
	if want := "ab____"; !containsRow(rows, want) {
		t.Errorf("active row missing %q in\n%s", want, rows)
	}
	if containsRow(rows, "secret") {
		t.Error("invisible field was drawn")
	}
	if !containsRow(rows, "x") || !containsAny(rows, "▾") {
		t.Error("F4 marker missing")
	}
}

func containsRow(s, sub string) bool { return containsAny(s, sub) }
func containsAny(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// A frame draws as an outlined box of its width and height with the title
// on the top edge, and a field placed inside it overprints the interior.
func TestRenderFrameBox(t *testing.T) {
	atoms := []diag.Atom{
		diag.Label(2, 3, "in"),
		{EType: diag.AtomFrame, Row: 1, Col: 1, Length: 10, Height: 4, Text: "Box"},
	}
	g := Render(atoms, 6, 12)
	if g.At(1, 1) != '┌' || g.At(1, 10) != '┐' || g.At(4, 1) != '└' || g.At(4, 10) != '┘' {
		t.Errorf("corners wrong:\n%s", g.String())
	}
	if got := at(g, 1, 3, 5); got != " Box " {
		t.Errorf("title on top edge = %q", got)
	}
	if got := at(g, 2, 3, 2); got != "in" {
		t.Errorf("label inside the box = %q", got)
	}
	if g.At(2, 1) != '│' {
		t.Errorf("left edge = %q, want │", g.At(2, 1))
	}
}

// A sized pushbutton fills its width and height with the button face, the
// caption centred — so an equalizer bar made of an empty tall button shows
// as a solid block, not a bracketed nothing.
func TestRenderButtonFace(t *testing.T) {
	atoms := []diag.Atom{
		{EType: diag.AtomPushbutton, Row: 0, Col: 2, Length: 4, Height: 3, Text: ""},
		{EType: diag.AtomPushbutton, Row: 4, Col: 0, Length: 8, Height: 1, Text: "OK"},
	}
	g := Render(atoms, 6, 12)
	for r := 0; r < 3; r++ {
		for c := 2; c < 6; c++ {
			if cell := g.CellAt(r, c); cell.Style != StyleButton {
				t.Fatalf("cell (%d,%d) not a button face: %+v", r, c, cell)
			}
		}
	}
	if cell := g.CellAt(0, 6); cell.Style == StyleButton {
		t.Error("button face spills past its width")
	}
	if got := strings.TrimSpace(at(g, 4, 0, 8)); got != "OK" {
		t.Errorf("caption = %q, want OK", got)
	}
	if g.CellAt(4, 3).Ch != 'O' {
		t.Errorf("caption not centred: %q", at(g, 4, 0, 8))
	}
}

// List runs carry their SAP colour as the cell style, icons become a glyph
// and a space with the rest of the run shifted two cells left, a box-drawing
// run (SFE byte 2 = 0x08) maps its SAP codes to box glyphs, and a text run is
// text even when SFE byte 0 is 0x08.
func TestRenderListColourAndIcons(t *testing.T) {
	segs := []diag.ListSegment{
		diag.ListText(0, 0, diag.ColNegative, "bad"),
		diag.ListText(1, 0, diag.ColOff, "@0A@ red"),
		{Row: 2, Col: 0, Length: 3, Attr: [3]byte{0x08, 0, 0x08}, Text: "454"}, // box: ─│─
		{Row: 3, Col: 0, Length: 3, Attr: [3]byte{0x08, 0, 0x00}, Text: "001"}, // text, not ruled
	}
	g := RenderList(segs, 4, 10)
	if st := g.CellAt(0, 0).Style; st != ListStyle(diag.ColNegative) || st.Bg == 0 {
		t.Errorf("negative colour not applied: %+v", st)
	}
	if got := at(g, 1, 0, 6); got != "●  red" {
		t.Errorf("icon run = %q, want %q", got, "●  red")
	}
	if g.CellAt(1, 0).Style.Fg != 196 {
		t.Errorf("red light glyph colour = %d", g.CellAt(1, 0).Style.Fg)
	}
	if got := at(g, 2, 0, 3); got != "─│─" {
		t.Errorf("box run = %q, want %q", got, "─│─")
	}
	if got := at(g, 3, 0, 3); got != "001" {
		t.Errorf("text run with SFE byte0=0x08 = %q, want 001 (not a ruled line)", got)
	}
}

// A password field shows one asterisk per character over its underscores.
func TestRenderPasswordMask(t *testing.T) {
	atoms := []diag.Atom{
		{EType: diag.AtomInputField, Row: 0, Col: 0, Attr: diag.AttrYes3D | diag.AttrInvisible, VisibleLength: 8, Text: "abc"},
	}
	g := Render(atoms, 1, 10)
	if got := at(g, 0, 0, 8); got != "***_____" {
		t.Errorf("password field = %q", got)
	}
}

// Compose puts title, menus, the standard toolbar (with the command field) and
// the application toolbar above the canvas, and the status bar below; the
// canvas lands at row ChromeRows-1.
func TestCompose(t *testing.T) {
	canvas := Render([]diag.Atom{diag.Label(0, 0, "Client")}, 2, 20)
	v := View{Title: "SAP R/3 (1) A4H", Menus: []string{"User", "System", "Help"},
		Toolbar: []string{"New password"}, Canvas: canvas, MsgType: 'E', Message: "Name or password is incorrect", Info: "A4H (1) 001",
		Command: "se38"}
	g := Compose(v, 0, 60)
	if g.Rows != 2+ChromeRows || g.Cols != 60 {
		t.Fatalf("composed size %dx%d", g.Rows, g.Cols)
	}
	if got := at(g, 0, 1, 15); got != "SAP R/3 (1) A4H" {
		t.Errorf("title = %q", got)
	}
	if got := at(g, 1, 1, 18); got != "User  System  Help" {
		t.Errorf("menu bar = %q", got)
	}
	// Standard toolbar: the Enter button and the command field showing "se38".
	if got := at(g, stdToolbarRow, stdEnterCol, 1); got != "✓" {
		t.Errorf("Enter button = %q", got)
	}
	if got := at(g, stdToolbarRow, stdCmdCol, 6); got != "se38__" {
		t.Errorf("command field = %q", got)
	}
	// Application toolbar is now the row below the standard toolbar.
	if got := at(g, 3, 1, 14); got != " New password " {
		t.Errorf("app toolbar = %q", got)
	}
	if got := at(g, ChromeRows-1, 0, 6); got != "Client" {
		t.Errorf("canvas row 0 = %q", got)
	}
	sr := g.Rows - 1
	if got := at(g, sr, 1, 31); got != "✖ Name or password is incorrect" {
		t.Errorf("status message = %q", got)
	}
	if got := at(g, sr, 60-12, 11); got != "A4H (1) 001" {
		t.Errorf("status info = %q", got)
	}
	if !strings.Contains(g.ANSILine(0, 60), "48;5;24") {
		t.Error("title bar ANSI lacks its background")
	}
}
