package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/oisee/sap-tui/internal/diag"
)

func capFrames(t *testing.T, dir string, n int) [][]byte {
	f, err := os.Open("../../captures/probe.jsonl")
	if err != nil {
		t.Skip("no capture")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var out [][]byte
	for sc.Scan() && len(out) < n {
		var l struct{ Dir, Hex string }
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Dir != dir {
			continue
		}
		d, _ := hex.DecodeString(l.Hex)
		if len(d) == 0 {
			continue
		}
		if _, ok := diag.NIControl(d); ok {
			continue
		}
		out = append(out, d)
	}
	return out
}

func TestLogonPasswordSubstituted(t *testing.T) {
	cs := capFrames(t, "C->S", 2)
	sc := capFrames(t, "S->C", 30)
	if len(cs) < 2 || len(sc) == 0 {
		t.Skip("capture too short")
	}
	var screen []diag.Item
	for _, d := range sc {
		m, err := diag.ParseMessage(d, false)
		if err != nil {
			continue
		}
		its := diag.ParseItems(m.Body)
		if isLogonScreen(its) {
			screen = its
			break
		}
	}
	if screen == nil {
		t.Skip("no logon screen in capture")
	}
	const dummy = "ZZDUMMYPW42"
	out, err := buildLogonPAI(cs[1], screen, credentials{Client: "001", User: "TESTER", Password: dummy, Lang: "EN"}, 5, false)
	if err != nil {
		t.Fatalf("buildLogonPAI: %v", err)
	}
	h := hex.EncodeToString(out)
	if !strings.Contains(h, hex.EncodeToString([]byte(dummy))) {
		t.Errorf("dummy password NOT in built frame: password field not substituted")
	}
	if !strings.Contains(h, hex.EncodeToString([]byte("TESTER"))) {
		t.Errorf("user NOT in built frame")
	}
	if strings.Contains(h, hex.EncodeToString([]byte("(7ujm"))) {
		t.Errorf("template password '(7ujm...' leaked into built frame")
	}
}
