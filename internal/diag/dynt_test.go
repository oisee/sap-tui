package diag

import (
	"encoding/hex"
	"strings"
	"testing"
)

// screen0100 is the 96-byte DYNT_ATOM the probe's screen 0100 arrived as:
// the label "Ticks" at line 2 column 2, the INT4 output field GV_TICKS at
// line 2 column 10 with length 10, and each element's name atom. The
// counter reads 0 here; the server pushed the same bytes 67 times with only
// that digit changing.
const screen0100 = "" +
	"0018" + "0000" + "84" + "000100" + "0001" + "0001" + "21" + "0000" + "05" + "05" + "0005" + "5469636b73" +
	"0016" + "0000" + "72" + "000100" + "0001" + "0001" + "21" + "5458545f5449434b53" +
	"001d" + "0000" + "83" + "000100" + "0001" + "0009" + "29" + "0000" + "0a" + "0a" + "000a" + "20202020202020203020" +
	"0015" + "0000" + "72" + "000100" + "0001" + "0009" + "29" + "47565f5449434b53"

func fixture(t *testing.T, h string) []byte {
	t.Helper()
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseDyntAtomsScreen0100(t *testing.T) {
	value := fixture(t, screen0100)
	if len(value) != 96 {
		t.Fatalf("fixture is %d bytes, want 96", len(value))
	}
	atoms, err := ParseDyntAtoms(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 4 {
		t.Fatalf("%d atoms, want 4: %+v", len(atoms), atoms)
	}
	// The screen painter counts lines and columns from one; the wire from zero.
	want := []struct {
		etype     byte
		line, col int
		length    int
		text      string
		attr      byte
		status    Status
	}{
		{AtomLabel, 2, 2, 5, "Ticks", AttrProtected | AttrPropFont, Confirmed},
		{AtomFieldName, 2, 2, 9, "TXT_TICKS", AttrProtected | AttrPropFont, Confirmed},
		{AtomOutputField, 2, 10, 10, "0", AttrProtected | AttrJustRight | AttrPropFont, Confirmed},
		{AtomFieldName, 2, 10, 8, "GV_TICKS", AttrProtected | AttrJustRight | AttrPropFont, Confirmed},
	}
	for i, w := range want {
		a := atoms[i]
		if a.EType != w.etype || a.Row+1 != w.line || a.Col+1 != w.col || a.Length != w.length || a.Value() != w.text || a.Attr != w.attr || a.Status != w.status {
			t.Errorf("atom %d: got type=%02x line=%d col=%d len=%d text=%q attr=%02x %s, want %+v", i, a.EType, a.Row+1, a.Col+1, a.Length, a.Value(), a.Attr, a.Status, w)
		}
		if a.Area != 0 || a.Block != 1 || a.Group != 0 || a.Flags != [2]byte{} {
			t.Errorf("atom %d: area/block/group=%d/%d/%d flags=%x", i, a.Area, a.Block, a.Group, a.Flags)
		}
	}
	if f := atoms[2]; f.VisibleLength != 10 || f.MaxChars != 10 || f.Text != "        0 " {
		t.Errorf("output field: visible=%d max=%d text=%q", f.VisibleLength, f.MaxChars, f.Text)
	}
	if atoms[0].Offset != 0 || atoms[0].Size != 24 || atoms[3].Offset != 75 || atoms[3].Size != 21 {
		t.Errorf("offsets: %+v %+v", atoms[0], atoms[3])
	}
}

func TestParseDyntAtomsCounterChanges(t *testing.T) {
	// A later push differs from the first only in the counter's digits,
	// which is what animating a screen amounts to.
	later := strings.Replace(screen0100, "20202020202020203020", "20202020202020313320", 1)
	atoms, err := ParseDyntAtoms(fixture(t, later))
	if err != nil {
		t.Fatal(err)
	}
	if v := atoms[2].Value(); v != "13" {
		t.Errorf("counter: %q", v)
	}
	if atoms[2].Length != 10 || atoms[2].Status != Confirmed {
		t.Errorf("counter field: %+v", atoms[2])
	}
}

func TestParseDyntAtomsPushbutton(t *testing.T) {
	// Built by hand to the layout: attr, width 4, height 1, then the two
	// offsets from the atom's start, caption "Go" and function code "=GO",
	// each ending in NUL.
	caption := "Go\x00"
	fcode := "=GO\x00"
	body := []byte{0x20, 4, 1, 0, byte(19 + len(caption)), 0, 19}
	body = append(body, caption...)
	body = append(body, fcode...)
	hdr := []byte{0, byte(12 + len(body)), 0, 0, AtomPushbutton, 0, 1, 0, 0, 3, 0, 40}
	atoms, err := ParseDyntAtoms(append(hdr, body...))
	if err != nil {
		t.Fatal(err)
	}
	a := atoms[0]
	if a.Text != "Go" || a.Function != "=GO" || a.Length != 4 || a.Height != 1 || a.Row != 3 || a.Col != 40 || a.Status != Confirmed {
		t.Errorf("pushbutton: %+v", a)
	}
}

func TestParseDyntAtomsErrors(t *testing.T) {
	value := fixture(t, screen0100)
	atoms, err := ParseDyntAtoms(value[:40])
	if err == nil || len(atoms) != 1 {
		t.Errorf("truncated: %d atoms, err %v", len(atoms), err)
	}
	if _, err := ParseDyntAtoms([]byte{0, 5, 0, 0, 0}); err == nil {
		t.Error("a length shorter than the header passed")
	}
	unknown := []byte{0, 13, 0, 0, 0x99, 0, 1, 0, 0, 0, 0, 0, 0xff}
	atoms, err = ParseDyntAtoms(unknown)
	if err != nil || len(atoms) != 1 || atoms[0].Status != Raw || atoms[0].TypeName() != "0x99" || len(atoms[0].Rest) != 1 {
		t.Errorf("unknown etype: %+v %v", atoms, err)
	}
}
