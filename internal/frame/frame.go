// Package frame is the screen described the way you would draw it, not the
// way DIAG carries it: a grid with elements placed on it — a label, a field,
// a checkbox, a button, or a run of text lines for an old list screen — that
// turns into the DYNT_ATOM a real SAP GUI draws. It is the near side of the
// serializer; diag is the far side.
//
// The API reads like a small TUI builder: New a screen, place elements by
// row and column (0-based), and Encode it. Every field carries a name, the
// ABAP name the GUI sends back with the value; a label does not.
package frame

import (
	"fmt"
	"strings"

	"github.com/oisee/sap-tui/internal/diag"
)

// Screen is a grid of the given size with elements placed on it.
type Screen struct {
	Rows, Cols int
	atoms      []diag.Atom
}

// New is a screen of rows by cols character cells.
func New(rows, cols int) *Screen { return &Screen{Rows: rows, Cols: cols} }

// clip keeps a placement on the grid and trims a text that runs off the
// right edge, so a description can never produce an atom off-screen.
func (s *Screen) clip(row, col, width int, text string) (int, int, int, string) {
	if row < 0 {
		row = 0
	}
	if col < 0 {
		col = 0
	}
	if s.Cols > 0 && col+width > s.Cols {
		width = s.Cols - col
	}
	if width < 0 {
		width = 0
	}
	if len([]rune(text)) > width {
		text = string([]rune(text)[:width])
	}
	return row, col, width, text
}

// Text places a static caption.
func (s *Screen) Text(row, col int, text string) *Screen {
	row, col, _, text = s.clip(row, col, len(text), text)
	s.atoms = append(s.atoms, diag.Label(row, col, text))
	return s
}

// Output places a protected field of width columns holding value, with the
// ABAP name the client will echo. numeric right-justifies it.
func (s *Screen) Output(row, col, width int, name, value string, numeric bool) *Screen {
	row, col, width, value = s.clip(row, col, width, padValue(value, width, numeric))
	attr := byte(diag.AttrProtected)
	if numeric {
		attr |= diag.AttrJustRight
	}
	s.atoms = append(s.atoms,
		diag.OutputField(row, col, width, value, numeric),
		diag.FieldName(row, col, upper(name), attr))
	return s
}

// Number is an Output holding an integer.
func (s *Screen) Number(row, col, width int, name string, value int) *Screen {
	return s.Output(row, col, width, name, fmt.Sprintf("%d", value), true)
}

// Icon places a display-only icon: an @XX@ token in a protected output field,
// which a dynpro GUI substitutes for the bitmap (a plain label does not). No
// field name, since it is never read back — one atom, so a grid of them stays
// light.
func (s *Screen) Icon(row, col int, code string) *Screen {
	row, col, _, code = s.clip(row, col, len(code), code)
	s.atoms = append(s.atoms, diag.OutputField(row, col, len(code), code, false))
	return s
}

// Input places an editable field of width columns.
func (s *Screen) Input(row, col, width int, name, value string) *Screen {
	row, col, width, value = s.clip(row, col, width, padValue(value, width, false))
	s.atoms = append(s.atoms,
		diag.InputField(row, col, width, value),
		diag.FieldName(row, col, upper(name), diag.AttrYes3D))
	return s
}

// The state of an input field is its attribute bits: active is editable,
// protected is shown but not editable, hidden is invisible, and F4 adds the
// value-help (matchcode) the user opens with F4.

// InputProtected is an inactive input — the value shows, the user cannot
// change it.
func (s *Screen) InputProtected(row, col, width int, name, value string) *Screen {
	row, col, width, value = s.clip(row, col, width, padValue(value, width, false))
	s.atoms = append(s.atoms,
		diag.Atom{EType: diag.AtomInputField, Row: row, Col: col, Attr: diag.AttrProtected,
			Length: len(value), VisibleLength: width, MaxChars: width, Text: value, Status: diag.Confirmed},
		diag.FieldName(row, col, upper(name), diag.AttrProtected))
	return s
}

// InputHidden is an invisible input — present on the screen, its value not
// shown (a password field is the same, INVISIBLE set).
func (s *Screen) InputHidden(row, col, width int, name, value string) *Screen {
	row, col, width, value = s.clip(row, col, width, padValue(value, width, false))
	attr := byte(diag.AttrYes3D | diag.AttrInvisible)
	s.atoms = append(s.atoms,
		diag.Atom{EType: diag.AtomInputField, Row: row, Col: col, Attr: attr,
			Length: len(value), VisibleLength: width, MaxChars: width, Text: value, Status: diag.Inferred},
		diag.FieldName(row, col, upper(name), attr))
	return s
}

// InputF4 is an active input that offers value help — the matchcode button
// the user opens with F4.
func (s *Screen) InputF4(row, col, width int, name, value string) *Screen {
	row, col, width, value = s.clip(row, col, width, padValue(value, width, false))
	attr := byte(diag.AttrYes3D | diag.AttrMatchcode)
	s.atoms = append(s.atoms,
		diag.Atom{EType: diag.AtomInputField, Row: row, Col: col, Attr: attr,
			Length: len(value), VisibleLength: width, MaxChars: width, Text: value, Status: diag.Inferred},
		diag.FieldName(row, col, upper(name), attr))
	return s
}

// Date is an input field holding a date. On the wire it is an ordinary
// input; a native calendar needs the field's DDIC type, which a screen
// built here does not carry, so the value and an F4 marker are what a rogue
// screen can offer. width defaults to the value's length.
func (s *Screen) Date(row, col int, name, value string) *Screen {
	return s.InputF4(row, col, len(value), name, value)
}

// Time is an input field holding a time, like Date.
func (s *Screen) Time(row, col int, name, value string) *Screen {
	return s.InputF4(row, col, len(value), name, value)
}

// Checkbox places a checkbox with its label.
func (s *Screen) Checkbox(row, col int, name, label string, on bool) *Screen {
	state := byte(' ')
	if on {
		state = 'X'
	}
	s.atoms = append(s.atoms,
		diag.Atom{EType: diag.AtomCheckbox, Row: row, Col: col, Attr: diag.AttrYes3D, State: state, Text: label, Status: diag.Inferred},
		diag.FieldName(row, col, upper(name), diag.AttrYes3D))
	return s
}

// Button places a pushbutton of the given width with a function code.
func (s *Screen) Button(row, col, width int, caption, fcode string) *Screen {
	return s.ButtonH(row, col, width, 1, caption, fcode)
}

// ButtonH places a pushbutton height cells tall — a button can span more
// than one row.
func (s *Screen) ButtonH(row, col, width, height int, caption, fcode string) *Screen {
	if !strings.HasPrefix(fcode, "=") {
		fcode = "=" + fcode
	}
	s.atoms = append(s.atoms, diag.Atom{EType: diag.AtomPushbutton, Row: row, Col: col,
		Attr: diag.AttrYes3D, Length: width, Height: height, Text: caption, Function: upper(fcode), Status: diag.Confirmed})
	return s
}

// Lines lays a run of text down the screen from startRow, one label a row,
// each trimmed to the width — an old text list shown as protected text.
func (s *Screen) Lines(startRow int, lines []string) *Screen {
	for i, line := range lines {
		row := startRow + i
		if s.Rows > 0 && row >= s.Rows {
			break
		}
		s.Text(row, 0, line)
	}
	return s
}

// Frame draws a box of width by height with a title on its top edge. The box
// is kept inside the screen: a height or width that would run off the grid is
// trimmed, so a frame can never stretch the GUI's canvas into a tall scrollable
// page.
func (s *Screen) Frame(row, col, width, height int, title string) *Screen {
	if row < 0 {
		row = 0
	}
	if col < 0 {
		col = 0
	}
	if s.Rows > 0 && row+height > s.Rows {
		height = s.Rows - row
	}
	if s.Cols > 0 && col+width > s.Cols {
		width = s.Cols - col
	}
	if height < 1 || width < 1 {
		return s
	}
	s.atoms = append(s.atoms, diag.Atom{EType: diag.AtomFrame, Row: row, Col: col,
		Attr: diag.AttrProtected, Length: width, Height: height, Text: title, Status: diag.Confirmed})
	return s
}

// Radio places a radio button with its label. Buttons of one group share a
// name; on marks the selected one.
func (s *Screen) Radio(row, col int, name, label string, on bool) *Screen {
	state := byte(' ')
	if on {
		state = 'X'
	}
	s.atoms = append(s.atoms,
		diag.Atom{EType: diag.AtomRadioButton, Row: row, Col: col, Attr: diag.AttrYes3D, Group: 1, State: state, Text: label, Status: diag.Inferred},
		diag.FieldName(row, col, upper(name), diag.AttrYes3D))
	return s
}

// Overlay puts values the client returned back into the screen's fields,
// matched by the cell they sit on, so a re-render keeps what the user
// typed. It is how a static screen answers a PAI without losing input.
func (s *Screen) Overlay(vals []diag.FieldValue) *Screen {
	for i := range s.atoms {
		a := &s.atoms[i]
		if a.EType != diag.AtomInputField && a.EType != diag.AtomOutputField {
			continue
		}
		for _, v := range vals {
			if v.Row == a.Row && v.Col == a.Col {
				w := a.VisibleLength
				if w <= 0 {
					w = len(v.Value)
				}
				a.Text = padValue(v.Value, w, a.Attr&diag.AttrJustRight != 0)
				a.Length = len(a.Text)
			}
		}
	}
	return s
}

// Atoms is the screen as the atoms diag encodes.
func (s *Screen) Atoms() []diag.Atom { return s.atoms }

// Encode is the DYNT_ATOM item value for this screen.
func (s *Screen) Encode() []byte { return diag.EncodeDyntAtoms(s.atoms) }

// padValue pads a value to the field width the way SAP holds it: on the
// left for a right-justified number, on the right otherwise.
func padValue(v string, width int, numeric bool) string {
	if len(v) >= width {
		return v
	}
	pad := strings.Repeat(" ", width-len(v))
	if numeric {
		return pad + v
	}
	return v + pad
}

func upper(s string) string { return strings.ToUpper(s) }
