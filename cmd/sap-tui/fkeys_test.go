package main

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/oisee/sap-tui/internal/diag"
)

func TestParseFKeys(t *testing.T) {
	m := parseFKeys("8=STRT,3==BACK,12=/n, 5 = ONLI ,bad,99=X,0=Y,7=")
	cases := map[int]string{
		8:  "=STRT", // bare code gets a leading =
		3:  "=BACK", // already has =, kept
		12: "/n",    // a system command is left as-is
		5:  "=ONLI", // spaces around the pair and code are trimmed
	}
	for k, want := range cases {
		if m[k] != want {
			t.Errorf("F%d = %q, want %q", k, m[k], want)
		}
	}
	if _, ok := m[99]; ok {
		t.Errorf("F99 should be rejected (out of 1..24)")
	}
	if _, ok := m[0]; ok {
		t.Errorf("F0 should be rejected")
	}
	if _, ok := m[7]; ok {
		t.Errorf("F7 with an empty code should be left unbound")
	}
	if len(m) != 4 {
		t.Errorf("got %d bindings, want 4: %v", len(m), m)
	}
}

// The UI_EVENT_SOURCE bytes match a real GUI's function-key frames
// (captures/f8sniff.jsonl): the function number at byte 5 and the cursor's
// row/col little-endian at 10..13. F8 on RS38M-PROGRAMM (row 2, col 14) gave
// 0a00070001 08 0000000002000e000100.
func TestUIEventSource(t *testing.T) {
	cur := &diag.Atom{Row: 2, Col: 14}
	got := uiEventSource(8, cur)
	want, _ := hex.DecodeString("0a00070001080000000002000e000100")
	if !bytes.Equal(got, want) {
		t.Errorf("F8 event = %x, want %x", got, want)
	}
	// F3 from the same field: only byte 5 changes.
	got3 := uiEventSource(3, cur)
	want3, _ := hex.DecodeString("0a00070001030000000002000e000100")
	if !bytes.Equal(got3, want3) {
		t.Errorf("F3 event = %x, want %x", got3, want3)
	}
	// No cursor: the position fields stay zero, the number still lands.
	g0 := uiEventSource(3, nil)
	if g0[5] != 3 || g0[10] != 0 || g0[12] != 0 {
		t.Errorf("no-cursor event = %x, want number at [5], zero position", g0)
	}
}

func TestSplitSteps(t *testing.T) {
	got := splitSteps(" /nse38 ; F8;; =BACK ;")
	want := []string{"/nse38", "F8", "=BACK"}
	if len(got) != len(want) {
		t.Fatalf("splitSteps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFKeyStep(t *testing.T) {
	for step, want := range map[string]int{"F8": 8, "f12": 12, "F1": 1, "F24": 24} {
		if n, ok := fkeyStep(step); !ok || n != want {
			t.Errorf("fkeyStep(%q) = %d,%v, want %d,true", step, n, ok, want)
		}
	}
	for _, step := range []string{"/nse38", "=BACK", "F0", "F25", "F", "FX", ""} {
		if n, ok := fkeyStep(step); ok {
			t.Errorf("fkeyStep(%q) = %d,true, want not-an-fkey", step, n)
		}
	}
}

func TestStripMarkup(t *testing.T) {
	cases := map[string]string{
		"@10\\QDisplay@ Display": "Display", // icon+tooltip form, trailing caption
		"@0Y@ Create":           "Create",   // bare icon
		"Change":                "Change",    // no markup
		"  Display  ":           "Display",   // trimmed
		"@6C@":                  "",          // icon only
	}
	for in, want := range cases {
		if got := stripMarkup(in); got != want {
			t.Errorf("stripMarkup(%q) = %q, want %q", in, got, want)
		}
	}
}
