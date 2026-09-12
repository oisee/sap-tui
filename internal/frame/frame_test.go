package frame

import (
	"testing"

	"github.com/oisee/sap-tui/internal/diag"
)

func TestScreenCounter(t *testing.T) {
	s := New(27, 120).
		Text(1, 1, "Ticks").
		Number(1, 9, 10, "GV_TICKS", 13)
	atoms, err := diag.ParseDyntAtoms(s.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 3 {
		t.Fatalf("got %d atoms: %+v", len(atoms), atoms)
	}
	if atoms[0].Value() != "Ticks" || atoms[1].Value() != "13" || atoms[1].Attr&diag.AttrJustRight == 0 {
		t.Errorf("atoms: %+v", atoms)
	}
	if atoms[2].TypeName() != "name" || atoms[2].Value() != "GV_TICKS" {
		t.Errorf("name: %+v", atoms[2])
	}
}

func TestScreenLinesAndClip(t *testing.T) {
	s := New(3, 10).Lines(0, []string{"this line is far too wide", "two", "three", "four never shows"})
	atoms, err := diag.ParseDyntAtoms(s.Encode())
	if err != nil {
		t.Fatal(err)
	}
	// Only three rows fit, and the first is trimmed to ten columns.
	if len(atoms) != 3 {
		t.Fatalf("rows: %d", len(atoms))
	}
	if atoms[0].Value() != "this line" && atoms[0].Text != "this line " {
		t.Errorf("clip: %q", atoms[0].Text)
	}
}
