package diag

import (
	"bytes"
	"testing"
)

// The screen-0100 DYNT_ATOM as the server pushed it: a label "Ticks", its
// name atom, the counter's output field, its name atom. Encoding these and
// parsing them back must return the same atoms, and the bytes must match
// the fixture the capture carried.
func TestEncodeDyntRoundTrip(t *testing.T) {
	atoms := []Atom{
		Label(1, 1, "Ticks"),
		FieldName(1, 1, "TXT_TICKS", AttrProtected),
		OutputField(1, 9, 10, "       13 ", true),
		FieldName(1, 9, "GV_TICKS", AttrProtected|AttrJustRight),
	}
	enc := EncodeDyntAtoms(atoms)
	back, err := ParseDyntAtoms(enc)
	if err != nil {
		t.Fatalf("parse back: %v", err)
	}
	if len(back) != 4 {
		t.Fatalf("got %d atoms", len(back))
	}
	if back[0].TypeName() != "label" || back[0].Value() != "Ticks" || back[0].Row != 1 || back[0].Col != 1 {
		t.Errorf("label: %+v", back[0])
	}
	if back[2].TypeName() != "output" || back[2].Value() != "13" || back[2].Col != 9 || back[2].Attr&AttrJustRight == 0 {
		t.Errorf("output: %+v", back[2])
	}
	if back[3].TypeName() != "name" || back[3].Value() != "GV_TICKS" {
		t.Errorf("name: %+v", back[3])
	}
	// Re-encoding the parsed atoms is byte-identical: a stable round trip.
	if !bytes.Equal(enc, EncodeDyntAtoms(back)) {
		t.Error("re-encode differs")
	}
}

// A button and a checkbox survive the round trip too.
func TestEncodeDyntButtonCheckbox(t *testing.T) {
	b := Atom{EType: AtomPushbutton, Row: 2, Col: 0, Attr: AttrYes3D, Length: 16, Height: 1, Text: "Save", Function: "=SAVE", Status: Confirmed}
	c := Atom{EType: AtomCheckbox, Row: 3, Col: 0, Attr: AttrYes3D, State: 'X', Text: "Run", Status: Inferred}
	back, err := ParseDyntAtoms(EncodeDyntAtoms([]Atom{b, c}))
	if err != nil {
		t.Fatal(err)
	}
	if back[0].Value() != "Save" || back[0].Function != "=SAVE" || back[0].Length != 16 {
		t.Errorf("button: %+v", back[0])
	}
	if back[1].State != 'X' || back[1].Value() != "Run" {
		t.Errorf("checkbox: %+v", back[1])
	}
}

// A synthesized FRAME (no Rest from the wire) must encode its body — attr,
// height, width, title — so a real GUI draws the group box. Regression: it
// used to fall through to an empty body and no box appeared.
func TestEncodeFrameBody(t *testing.T) {
	f := Atom{EType: AtomFrame, Row: 0, Col: 35, Height: 19, Length: 56, Text: "Information"}
	enc := f.Encode()
	if len(enc) <= AtomHeaderLen {
		t.Fatalf("frame encoded to %d bytes (header only = empty body bug)", len(enc))
	}
	got, err := ParseDyntAtoms(enc)
	if err != nil || len(got) != 1 {
		t.Fatalf("parse back: %v (%d atoms)", err, len(got))
	}
	a := got[0]
	if a.EType != AtomFrame || a.Height != 19 || a.Length != 56 || a.Value() != "Information" {
		t.Errorf("round-trip = etype 0x%02x h=%d w=%d title=%q", a.EType, a.Height, a.Length, a.Value())
	}
}
