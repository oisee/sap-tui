package main

import (
	"encoding/binary"
	"sort"
	"strings"

	"github.com/oisee/sap-tui/internal/diag"
	"github.com/oisee/sap-tui/internal/tui"
)

// The interactive screen: the atoms the server sent, a focus ring over every
// control the user can act on — input fields, checkboxes, radio buttons and
// pushbuttons — the values and toggle states as the user changes them locally,
// and a caret for text. Keys move the focus and change the focused control;
// Enter (or a pushbutton, or an OK-code) turns the changes into one PAI.
// Nothing goes to the server until then — the DIAG input model is one
// round-trip per action, and typed letters and toggles travel with it.

// the kinds of control the focus ring stops on
const (
	fInput  = iota // an editable text field
	fCheck         // a checkbox (Space toggles)
	fRadio         // a radio button (Space selects, clearing its group)
	fButton        // a pushbutton (Enter/Space fires its function code)
)

type field struct {
	idx              int // index into screenState.atoms
	kind             int
	row, col         int
	width            int // cells on screen (for the focus highlight)
	max              int // characters an input field holds
	hidden           bool
	area, block, grp byte   // radio-group identity
	fcode            string // a pushbutton's function code
}

type screenState struct {
	atoms     []diag.Atom
	fields    []field
	tabs      []diag.Tab     // a tabstrip's tabs, if the screen has one
	tabHits   []tui.TabSpan  // where each tab was last drawn, for click hit-testing
	orig      map[int]string // input fields: the value the server sent
	values    map[int]string // input fields: the value now
	origState map[int]byte   // check/radio: the state the server sent
	states    map[int]byte   // check/radio: the state now ('X' or ' ')
	focus     int            // index into fields, -1 when the screen has none
	caret     int            // rune offset in the focused input value
	cmd       string
	inCmd     bool
}

// tabBarRow is the canvas row the tab bar is drawn on.
const tabBarRow = 0

// newScreenState reads the screen a frame carries: every DYNT_ATOM's atoms,
// the controls the user can act on among them, and the server's cursor
// (DYNT.0b) as the initial focus.
func newScreenState(items []diag.Item) *screenState {
	s := &screenState{
		orig: map[int]string{}, values: map[int]string{},
		origState: map[int]byte{}, states: map[int]byte{}, focus: -1,
	}
	curRow, curCol := -1, -1
	for _, it := range items {
		switch {
		case it.Type == diag.ItemAPPL4 && it.ID == 0x09 && it.SID == 0x02:
			a, _ := diag.ParseDyntAtoms(it.Value)
			s.atoms = append(s.atoms, a...)
		case it.Type == diag.ItemAPPL && it.ID == 0x09 && it.SID == 0x0b && len(it.Value) >= 5:
			curRow = int(binary.BigEndian.Uint16(it.Value[1:]))
			curCol = int(binary.BigEndian.Uint16(it.Value[3:]))
		case it.Type == diag.ItemAPPL4 && it.ID == 0x09 && it.SID == 0x10:
			s.tabs = append(s.tabs, diag.ParseTabstrip(it.Value)...)
		}
	}
	// Place each subscreen's atoms at its area origin (the logon Information box
	// renders inside its frame, not at the top-left), before the fields read
	// their positions and the cursor is matched.
	s.atoms = diag.OffsetAtomsByArea(s.atoms, diag.AreaOrigins(items))
	for i, a := range s.atoms {
		if a.Attr&diag.AttrInvisible != 0 && a.EType != diag.AtomInputField {
			continue // an invisible label/frame is not a control
		}
		switch a.EType {
		case diag.AtomInputField:
			if a.Attr&diag.AttrProtected != 0 {
				continue // a protected field is output, not editable
			}
			w := a.VisibleLength
			if w <= 0 {
				w = a.Length
			}
			if w <= 0 {
				continue
			}
			max := a.MaxChars
			if max <= 0 {
				max = w
			}
			hidden := a.Attr&diag.AttrInvisible != 0
			s.fields = append(s.fields, field{idx: i, kind: fInput, row: a.Row, col: a.Col, width: w, max: max, hidden: hidden})
			v := a.Value()
			if hidden {
				v = "" // the server never sends a password back
			}
			s.orig[i], s.values[i] = v, v
		case diag.AtomCheckbox:
			s.fields = append(s.fields, field{idx: i, kind: fCheck, row: a.Row, col: a.Col, width: 4 + len([]rune(a.Value()))})
			s.origState[i], s.states[i] = a.State, a.State
		case diag.AtomRadioButton:
			s.fields = append(s.fields, field{idx: i, kind: fRadio, row: a.Row, col: a.Col, width: 4 + len([]rune(a.Value())),
				area: a.Area, block: a.Block, grp: a.Group})
			s.origState[i], s.states[i] = a.State, a.State
		case diag.AtomPushbutton:
			w := a.Length
			if w <= 0 {
				w = len([]rune(a.Value())) + 2
			}
			s.fields = append(s.fields, field{idx: i, kind: fButton, row: a.Row, col: a.Col, width: w, fcode: a.Function})
		}
	}
	sort.SliceStable(s.fields, func(i, j int) bool {
		if s.fields[i].row != s.fields[j].row {
			return s.fields[i].row < s.fields[j].row
		}
		return s.fields[i].col < s.fields[j].col
	})
	if len(s.fields) > 0 {
		s.focus = 0
		for i, f := range s.fields {
			if f.row == curRow && f.col == curCol {
				s.focus = i
			}
		}
		s.syncCaret()
	}
	return s
}

func (s *screenState) focused() *field {
	if s == nil || s.focus < 0 || s.focus >= len(s.fields) {
		return nil
	}
	return &s.fields[s.focus]
}

// syncCaret puts the caret at the end of the focused input's value.
func (s *screenState) syncCaret() {
	if f := s.focused(); f != nil && f.kind == fInput {
		s.caret = len([]rune(s.values[f.idx]))
	} else {
		s.caret = 0
	}
}

func (s *screenState) moveFocus(d int) {
	if len(s.fields) == 0 {
		return
	}
	s.focus = ((s.focus+d)%len(s.fields) + len(s.fields)) % len(s.fields)
	s.syncCaret()
}

// selectRadio selects the focused radio button and clears the others in its
// group (same area/block/group), the way a real GUI does.
func (s *screenState) selectRadio(f *field) {
	for i := range s.fields {
		g := &s.fields[i]
		if g.kind == fRadio && g.area == f.area && g.block == f.block && g.grp == f.grp {
			if g.idx == f.idx {
				s.states[g.idx] = 'X'
			} else {
				s.states[g.idx] = ' '
			}
		}
	}
}

// action is what a key asks the session to do.
type action int

const (
	actNone action = iota
	actRedraw
	actSend // send a PAI with the returned okcode
	actQuit
)

// handleKey applies one key to the screen: focus, editing and toggles locally,
// Enter / a pushbutton / the OK-code prompt as a send. It returns the action
// and, for a send, the OK-code to carry (a pushbutton's function code, or "").
func (s *screenState) handleKey(k key) (action, string) {
	if k.kind == keyCtrlC {
		return actQuit, ""
	}
	if s.inCmd {
		switch k.kind {
		case keyEnter:
			s.inCmd = false
			cmd := strings.TrimSpace(s.cmd)
			s.cmd = ""
			return actSend, cmd
		case keyEsc, keyCtrlO:
			s.inCmd, s.cmd = false, ""
			return actRedraw, ""
		case keyBackspace:
			if r := []rune(s.cmd); len(r) > 0 {
				s.cmd = string(r[:len(r)-1])
			}
			return actRedraw, ""
		case keyRune:
			s.cmd += string(k.r)
			return actRedraw, ""
		}
		return actNone, ""
	}
	switch k.kind {
	case keyCtrlO:
		s.inCmd, s.cmd = true, ""
		return actRedraw, ""
	case keyTab, keyDown:
		s.moveFocus(1)
		return actRedraw, ""
	case keyBackTab, keyUp:
		s.moveFocus(-1)
		return actRedraw, ""
	}
	f := s.focused()
	// Enter on a pushbutton fires it; Enter anywhere else submits the screen.
	if k.kind == keyEnter {
		if f != nil && f.kind == fButton {
			return actSend, f.fcode
		}
		return actSend, ""
	}
	if f == nil {
		return actNone, ""
	}
	// Space acts on the focused control: toggle a checkbox, select a radio,
	// fire a button. In a text field it is an ordinary character.
	if k.kind == keyRune && k.r == ' ' && f.kind != fInput {
		switch f.kind {
		case fCheck:
			if s.states[f.idx] == 'X' {
				s.states[f.idx] = ' '
			} else {
				s.states[f.idx] = 'X'
			}
			return actRedraw, ""
		case fRadio:
			s.selectRadio(f)
			return actRedraw, ""
		case fButton:
			return actSend, f.fcode
		}
	}
	if f.kind != fInput {
		return actNone, "" // editing keys do nothing on a non-text control
	}
	v := []rune(s.values[f.idx])
	if s.caret > len(v) {
		s.caret = len(v)
	}
	switch k.kind {
	case keyLeft:
		if s.caret > 0 {
			s.caret--
		}
	case keyRight:
		if s.caret < len(v) {
			s.caret++
		}
	case keyHome:
		s.caret = 0
	case keyEnd:
		s.caret = len(v)
	case keyBackspace:
		if s.caret > 0 {
			v = append(v[:s.caret-1], v[s.caret:]...)
			s.caret--
		}
	case keyDelete:
		if s.caret < len(v) {
			v = append(v[:s.caret], v[s.caret+1:]...)
		}
	case keyRune:
		if len(v) >= f.max {
			return actNone, ""
		}
		v = append(v[:s.caret], append([]rune{k.r}, v[s.caret:]...)...)
		s.caret++
	default:
		return actNone, ""
	}
	s.values[f.idx] = string(v)
	return actRedraw, ""
}

// changed lists the controls whose value or state differs from what the server
// sent, as client atoms: edited text fields, toggled checkboxes, and the
// radio buttons of a group whose selection moved.
func (s *screenState) changed() []diag.Atom {
	var out []diag.Atom
	for _, f := range s.fields {
		switch f.kind {
		case fInput:
			if s.values[f.idx] != s.orig[f.idx] {
				out = append(out, changedAtom(s.atoms[f.idx], s.values[f.idx]))
			}
		case fCheck, fRadio:
			if s.states[f.idx] != s.origState[f.idx] {
				a := s.atoms[f.idx]
				a.State = s.states[f.idx]
				a.Flags[1] |= 0x01 // changed by the user
				if f.kind == fRadio {
					// Byte-3 0x80 marks the selected radio. Set it on the one
					// now chosen and CLEAR it on a deselected one — the server
					// preset it on the option it shipped selected, and that
					// atom is copied here, so leaving 0x80 would send a
					// deselected radio still flagged selected.
					if s.states[f.idx] == 'X' {
						a.Flags[1] |= 0x80
					} else {
						a.Flags[1] &^= 0x80
					}
				}
				a.Rest = nil
				out = append(out, a)
			}
		}
	}
	return out
}

// cursor is the focused field as the cursor atom for DYNT.0b, nil when none.
func (s *screenState) cursor() *diag.Atom {
	f := s.focused()
	if f == nil {
		return nil
	}
	a := s.atoms[f.idx]
	if f.kind == fInput {
		a.Text = s.values[f.idx]
	}
	return &a
}

// overlay is the screen's atoms with the local edits and toggles in place, for
// drawing.
func (s *screenState) overlay() []diag.Atom {
	out := append([]diag.Atom{}, s.atoms...)
	for _, f := range s.fields {
		switch f.kind {
		case fInput:
			out[f.idx].Text = s.values[f.idx]
		case fCheck, fRadio:
			out[f.idx].State = s.states[f.idx]
		}
	}
	return out
}

// markFocus inverts the focused control on a composed grid whose canvas starts
// at canvasRow, and returns the terminal cursor cell (1-based) for a text
// field, or 0,0 for anything else (the highlight alone shows the focus).
func (s *screenState) markFocus(g *tui.Grid, canvasRow int) (row, col int) {
	f := s.focused()
	if f == nil {
		return 0, 0
	}
	g.Restyle(canvasRow+f.row, f.col, f.width, func(st tui.Style) tui.Style {
		st.Reverse = true
		return st
	})
	if f.kind != fInput {
		return 0, 0
	}
	c := s.caret
	if c > f.width-1 {
		c = f.width - 1
	}
	return canvasRow + f.row + 1, f.col + c + 1
}

// note is the status-bar hint for the interactive mode.
func (s *screenState) note() string {
	if s.inCmd {
		return "Enter sends  (type F8, F3, … to fire that key)  Esc cancels"
	}
	f := s.focused()
	if f != nil {
		switch f.kind {
		case fCheck:
			return "Space toggles  Tab moves  Enter send  ^O OK-code  ^C quit"
		case fRadio:
			return "Space selects  Tab moves  Enter send  ^O OK-code  ^C quit"
		case fButton:
			return "Enter/Space presses  Tab moves  ^O OK-code  ^C quit"
		}
	}
	if len(s.tabs) > 0 {
		return "click a tab to switch  Tab/Shift-Tab fields  Enter send  ^O OK-code  ^C quit"
	}
	return "Tab/Shift-Tab fields  Enter send  ^O OK-code  ^C quit"
}

// valueAt returns the current (typed) value of the input field at (row, col).
func (s *screenState) valueAt(row, col int) string {
	for _, f := range s.fields {
		if f.kind == fInput && f.row == row && f.col == col {
			return s.values[f.idx]
		}
	}
	return ""
}
