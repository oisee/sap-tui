package cfw

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/oisee/sap-tui/internal/diag"
)

// rfctr00 returns the value of the RFC_TR.00 item in the nth S->C frame of a
// capture, or skips the test when the (gitignored) capture is absent.
func rfctr00(t *testing.T, path string, nth int) []byte {
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
		m, err := diag.ParseMessage(d, false)
		if err != nil {
			t.Fatalf("parse S->C #%d: %v", nth, err)
		}
		for _, it := range diag.ParseItems(m.Body) {
			if it.ID == 0x08 && it.SID == 0x00 {
				return it.Value
			}
		}
	}
	t.Fatalf("no RFC_TR.00 in S->C #%d", nth)
	return nil
}

// The Easy Access CFW-build call (S->C #6) decodes to a known verb sequence
// and its object-creating verbs each mint a handle.
func TestDecodeEasyAccessCall(t *testing.T) {
	val := rfctr00(t, "../../captures/probe.jsonl", 6)

	verbs, desc, values, err := Streams(val)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) == 0 {
		t.Fatal("empty value pool")
	}

	vs := ParseVerbs(verbs)
	if len(vs) != 71 {
		t.Errorf("verb count = %d, want 71", len(vs))
	}
	if len(vs) < 3 || vs[0].Name != "CreateObject" || vs[0].Flag != 'C' {
		t.Errorf("verb[0] = %+v, want CreateObject/C", vs[0])
	}
	if vs[1].Name != "CreateControl" {
		t.Errorf("verb[1] = %q, want CreateControl", vs[1].Name)
	}
	// The call creates the Easy Access container/tree controls, so several
	// verbs are object-creating.
	creates := 0
	for _, v := range vs {
		if v.Creates() {
			creates++
		}
	}
	if creates == 0 {
		t.Error("no object-creating verbs found")
	}

	ds := ParseSvarsDesc(desc)
	results := 0
	for _, r := range ds {
		if r.IsResult() {
			results++
		}
	}
	if results == 0 {
		t.Error("no _RESULT descriptor slots found")
	}

	vals := ParseSvarsValues(values)
	if len(vals) != 145 {
		t.Errorf("value-pool records = %d, want 145", len(vals))
	}
	poolResults := 0
	for _, v := range vals {
		if v.IsResult {
			poolResults++
		}
	}
	if poolResults == 0 {
		t.Error("no _RESULT slots in the value pool")
	}
	// The first descriptor _RESULT pointer (4) resolves to value-pool record 3,
	// which is a _RESULT slot.
	for _, r := range ds {
		if r.IsResult() {
			slot := ResultSlot(r.Pointer)
			if slot < 0 || slot >= len(vals) {
				t.Errorf("_RESULT pointer %q -> slot %d out of range", r.Pointer, slot)
			} else if !vals[slot].IsResult {
				t.Errorf("_RESULT pointer %q -> value rec %d, which is not a result slot", r.Pointer, slot)
			}
			break
		}
	}

	e := NewEngine()
	minted := e.Run(vs)
	if len(minted) != creates {
		t.Errorf("minted %d handles, want %d (one per creating verb)", len(minted), creates)
	}
	if len(minted) > 0 && minted[0] != "O1" {
		t.Errorf("first handle = %q, want O1", minted[0])
	}

	// FillResults writes our handles into the _RESULT slots in place; every
	// other byte of the value pool is preserved, and the filled slots read
	// back as our handles.
	e2 := NewEngine()
	filled, written := e2.FillResults(vs, ds, values)
	if len(filled) != len(values) {
		t.Fatalf("FillResults changed the pool length: %d != %d", len(filled), len(values))
	}
	if len(written) == 0 {
		t.Fatal("FillResults wrote no handles")
	}
	// each written handle appears in the pool at the record its slot named
	for _, h := range written {
		if !strings.Contains(string(filled), "000000000"+h) {
			t.Errorf("handle %q not written into the value pool", h)
		}
	}
	// bytes outside the touched value records are unchanged
	touched := map[int]bool{}
	for _, d := range ds {
		if d.IsResult() {
			if sl := ResultSlot(d.Pointer); sl >= 0 {
				touched[sl] = true
			}
		}
	}
	for rec := 0; rec*337 < len(values); rec++ {
		if touched[rec] {
			continue
		}
		a := values[rec*337 : (rec+1)*337]
		b := filled[rec*337 : (rec+1)*337]
		if string(a) != string(b) {
			t.Errorf("untouched value record %d changed", rec)
			break
		}
	}
}

// SpliceValuePool round-trips: after filling handles and splicing the pool
// back, the payload's value-pool stream decodes to the filled pool and the
// other streams are unchanged.
func TestSpliceValuePool(t *testing.T) {
	val := rfctr00(t, "../../captures/probe.jsonl", 6)
	verbs, desc, values, err := Streams(val)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine()
	filled, written := e.FillResults(ParseVerbs(verbs), ParseSvarsDesc(desc), values)
	if len(written) == 0 {
		t.Fatal("no handles written")
	}
	spliced, err := SpliceValuePool(val, filled)
	if err != nil {
		t.Fatal(err)
	}
	v2, d2, vals2, err := Streams(spliced)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vals2, filled) {
		t.Errorf("spliced value pool != filled pool (%d vs %d bytes)", len(vals2), len(filled))
	}
	if !bytes.Equal(v2, verbs) {
		t.Error("verbs stream changed by the splice")
	}
	if !bytes.Equal(d2, desc) {
		t.Error("descriptor stream changed by the splice")
	}
}
