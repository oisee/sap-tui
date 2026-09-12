package diag

import "testing"

func TestFieldIndexAndSet(t *testing.T) {
	atoms := []Atom{
		InputField(0, 34, 10, "300"),
		FieldName(0, 34, "P_MS", AttrYes3D),
		OutputField(2, 34, 10, "0", true),
		FieldName(2, 34, "P_TICKS", AttrProtected|AttrJustRight),
	}
	val := EncodeDyntAtoms(atoms)
	got, byName := FieldIndex(val)
	if byName["P_MS"] != 0 || byName["P_TICKS"] != 2 {
		t.Fatalf("index: %v", byName)
	}
	SetField(got, byName, "P_TICKS", "49")
	SetField(got, byName, "GHOST", "x") // ignored
	back, _ := ParseDyntAtoms(EncodeDyntAtoms(got))
	if back[2].Value() != "49" {
		t.Errorf("set: %q", back[2].Value())
	}
}
