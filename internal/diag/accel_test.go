package diag

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// scFrameItems returns the items of the nth S->C DIAG frame of a capture, or
// skips the test when the (gitignored) capture is absent.
func scFrameItems(t *testing.T, path string, nth int) []Item {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("capture %s absent: %v", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	si := -1
	for sc.Scan() {
		var l struct{ Dir, Hex string }
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Dir != "S->C" {
			continue
		}
		si++
		if si != nth {
			continue
		}
		d, _ := hex.DecodeString(l.Hex)
		m, err := ParseMessage(d, false)
		if err != nil {
			t.Fatalf("parse S->C #%d: %v", nth, err)
		}
		return ParseItems(m.Body)
	}
	t.Fatalf("no S->C #%d in %s", nth, path)
	return nil
}

// The whole F-key chain against the real wire: the opening status (#0) carries
// the accelerator table binding F8 -> function 8, and the SE38 status (#13)
// carries the MNUENTRY that labels function 8 "Execute". A client presses F8,
// resolves it to function 8, and can name it — exactly the join the TUI makes.
func TestFKeyJoinOnCapture(t *testing.T) {
	const cap = "../../captures/f8sniff.jsonl"
	accel := scFrameItems(t, cap, 0)
	fk := FKeyFuncs(accel)
	if fk[8] != 8 || fk[3] != 3 {
		t.Errorf("accel table: F8->%d F3->%d, want 8 and 3", fk[8], fk[3])
	}
	if lbl := AccelLabels(accel)[8]; lbl != "F8" {
		t.Errorf("function 8 keystroke label = %q, want \"F8\"", lbl)
	}
	se38 := scFrameItems(t, cap, 13)
	labels := FunctionLabels(se38)
	if labels[8] != "Execute" {
		t.Errorf("function 8 label on SE38 = %q, want \"Execute\"", labels[8])
	}
	if labels[3] != "Back" {
		t.Errorf("function 3 label on SE38 = %q, want \"Back\"", labels[3])
	}
}

// The rows are taken verbatim from an SE38 status frame's ST_R3INFO.13 items
// (captures/f8sniff.jsonl, S->C #0); decoding them needs no capture.
func accelItem(t *testing.T, h string) Item {
	t.Helper()
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	return Item{Type: ItemAPPL, ID: 0x06, SID: 0x13, Value: b}
}

func TestParseAccelTable(t *testing.T) {
	items := []Item{
		accelItem(t, "0130300026300000"),         // &0            Enter    -> 0
		accelItem(t, "0130330026460033200000"),   // &F "3"        F3       -> 3
		accelItem(t, "02303300264100264c0000"),   // &A &L         Alt+Left -> 3
		accelItem(t, "0130380026460038200000"),   // &F "8"        F8       -> 8
		accelItem(t, "0131300026460031300000"),   // &F "10"       F10      -> 10
		accelItem(t, "0131310026430053200000"),   // &C "S"        Ctrl+S   -> 11
		accelItem(t, "0231310026460031310000"),   // &F "11"       F11      -> 11
		accelItem(t, "0132300026530026460038200000"), // &S &F "8" Shift+F8 -> 20
	}
	got := ParseAccelTable(items)
	if len(got) != len(items) {
		t.Fatalf("parsed %d rows, want %d", len(got), len(items))
	}

	want := []AccelBinding{
		{Func: 0, Mods: "", Key: "Enter"},
		{Func: 3, Mods: "", Key: "F3"},
		{Func: 3, Mods: "A", Key: "Left"},
		{Func: 8, Mods: "", Key: "F8"},
		{Func: 10, Mods: "", Key: "F10"},
		{Func: 11, Mods: "C", Key: "S"},
		{Func: 11, Mods: "", Key: "F11"},
		{Func: 20, Mods: "S", Key: "F8"},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("row %d = %+v, want %+v", i, got[i], w)
		}
	}

	// The plain F-key map is what the client fires F-keys through.
	fk := FKeyFuncs(items)
	for key, wantFunc := range map[int]int{3: 3, 8: 8, 10: 10, 11: 11} {
		if fk[key] != wantFunc {
			t.Errorf("F%d -> function %d, want %d", key, fk[key], wantFunc)
		}
	}
	// Shift+F8 is not a plain F-key, so F8 keeps its unmodified binding (8),
	// not the Shift+F8 function (20).
	if fk[8] != 8 {
		t.Errorf("F8 -> function %d, want 8 (the plain binding, not Shift+F8=20)", fk[8])
	}
	// The modified rows do not leak into the plain-F-key map.
	if len(fk) != 4 {
		t.Errorf("plain F-key map has %d entries, want 4 (F3,F8,F10,F11)", len(fk))
	}
}
