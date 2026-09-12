package diag

import (
	"encoding/hex"
	"testing"
)

// The bytes are verbatim from captures/probe.jsonl (SE80 screen painter,
// S->C #58): three tabs of BS_TABSTR_CONTROL, "Attributes" active.
func TestParseTabstrip(t *testing.T) {
	h := "002e400074010100000000002010010200270016002b41747472696275746573202020202020" +
		"003d4844004844000021400072010100000000002042535f5441425354525f434f4e54524f4c5f4844" +
		"002b00007401010000000001200d0102002400160028456c656d656e74206c69737420003d464c00464c00" +
		"0021000072010100000000012042535f5441425354525f434f4e54524f4c5f464c" +
		"002a00007401010000000002200c0102002300160027466c6f77206c6f6769632020003d4c53004c5300" +
		"0021000072010100000000022042535f5441425354525f434f4e54524f4c5f4c53"
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	tabs := ParseTabstrip(b)
	want := []Tab{
		{Caption: "Attributes", Fcode: "=HD", Active: true},
		{Caption: "Element list", Fcode: "=FL", Active: false},
		{Caption: "Flow logic", Fcode: "=LS", Active: false},
	}
	if len(tabs) != len(want) {
		t.Fatalf("parsed %d tabs, want %d: %+v", len(tabs), len(want), tabs)
	}
	for i, w := range want {
		if tabs[i] != w {
			t.Errorf("tab %d = %+v, want %+v", i, tabs[i], w)
		}
	}

	// The field-name atoms (0x72) between the tabs are skipped, not mistaken
	// for tabs, so exactly the three tab buttons come back.
	active := 0
	for _, tb := range tabs {
		if tb.Active {
			active++
		}
	}
	if active != 1 {
		t.Errorf("active tabs = %d, want exactly 1", active)
	}
}
