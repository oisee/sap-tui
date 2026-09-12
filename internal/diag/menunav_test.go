package diag

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// The SE38 menu bar decodes to its known top-level structure. Scans the capture
// for the frame with the richest menu (the SE38 status).
func TestParseMenusOnCapture(t *testing.T) {
	f, err := os.Open("../../captures/f8sniff.jsonl")
	if err != nil {
		t.Skipf("capture absent: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var best []Menu
	for sc.Scan() {
		var l struct{ Dir, Hex string }
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Dir != "S->C" {
			continue
		}
		d, _ := hex.DecodeString(l.Hex)
		m, err := ParseMessage(d, false)
		if err != nil {
			continue
		}
		if mn := ParseMenus(ParseItems(m.Body)); len(mn) > len(best) {
			best = mn
		}
	}
	if len(best) == 0 {
		t.Skip("no menus in capture")
	}
	if best[0].Title != "Program" {
		t.Errorf("first menu = %q, want Program", best[0].Title)
	}
	if len(best) != 7 {
		t.Errorf("menu count = %d, want 7 (Program..Help)", len(best))
	}
	prog := best[0]
	got := map[string]int{}
	subs := 0
	for _, it := range prog.Items {
		if it.Separator {
			continue
		}
		got[it.Text] = it.Code
		if it.HasSub {
			subs++
		}
	}
	for txt, code := range map[string]int{"Create": 5, "Change": 6, "Display": 7} {
		if got[txt] != code {
			t.Errorf("Program/%s code = %d, want %d", txt, got[txt], code)
		}
	}
	if subs == 0 {
		t.Error("no submenu-parent items found in the Program menu")
	}
}
