package diag

import "testing"

func TestAreaOrigins(t *testing.T) {
	// CONTAINER.04 from the logon capture: area 1 -> row 1, col 36.
	items := []Item{{Type: ItemAPPL, ID: 0x0a, SID: 0x04, Value: []byte{0x01, 0x00, 0x01, 0x00, 0x24, 0x00, 0x36, 0x00, 0x11}}}
	o := AreaOrigins(items)
	if got := o[1]; got != [2]int{1, 36} {
		t.Errorf("area 1 origin = %v, want {1,36}", got)
	}

	// An atom in area 1 is translated by that origin; area 0 is unchanged.
	atoms := []Atom{
		{EType: AtomLabel, Area: 1, Row: 2, Col: 0, Text: "info"},
		{EType: AtomFrame, Area: 0, Row: 0, Col: 35},
	}
	out := OffsetAtomsByArea(atoms, o)
	if out[0].Row != 3 || out[0].Col != 36 {
		t.Errorf("area-1 atom at (%d,%d), want (3,36)", out[0].Row, out[0].Col)
	}
	if out[1].Row != 0 || out[1].Col != 35 {
		t.Errorf("area-0 atom moved: (%d,%d), want (0,35)", out[1].Row, out[1].Col)
	}
	// The input slice is not mutated.
	if atoms[0].Row != 2 {
		t.Error("OffsetAtomsByArea mutated its input")
	}
}
