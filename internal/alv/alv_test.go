package alv

import (
	"bytes"
	"math/rand"
	"os"
	"reflect"
	"testing"

	"github.com/oisee/sap-tui/internal/diag"
	"github.com/oisee/sap-tui/internal/replay"
	"github.com/oisee/sap-tui/internal/sapcompress"
)

// TestLZHRoundTrip is the foundational gate: our Compress must be an exact
// inverse of sapcompress.Decompress for every input.
func TestLZHRoundTrip(t *testing.T) {
	big := make([]byte, 64*1024)
	rand.New(rand.NewSource(42)).Read(big)

	cases := map[string][]byte{
		"empty":       {},
		"one byte":    {0x41},
		"small ascii": []byte("SPRSL ARBGB MSGNR TEXT"),
		"repetitive":  bytes.Repeat([]byte("Enter an existing info structure. "), 200),
		"big random":  big,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			comp, err := Compress(in)
			if err != nil {
				t.Fatalf("Compress: %v", err)
			}
			out, err := sapcompress.Decompress(comp)
			if err != nil {
				t.Fatalf("Decompress: %v", err)
			}
			if !bytes.Equal(in, out) {
				t.Fatalf("round-trip mismatch: in %d bytes, out %d bytes", len(in), len(out))
			}
		})
	}
}

// sampleCatalog is the T100 message-text column set (as read off a live 7.58
// ALV run), used to drive the synthetic round-trip.
func sampleCatalog() []Column {
	return []Column{
		{Name: "SPRSL", Type: 32, Len: 2},
		{Name: "ARBGB", Type: 32, Len: 20},
		{Name: "MSGNR", Type: 32, Len: 3},
		{Name: "TEXT", Type: 32, Len: 73},
	}
}

// TestRowRoundTrip encodes a catalog and rows and decodes them back unchanged.
func TestRowRoundTrip(t *testing.T) {
	cols := sampleCatalog()
	rows := [][]string{
		{"E", "Q6", "001", "Enter an existing info structure"},
		{"E", "00", "042", "The quick brown fox"},
		{"D", "Z1", "999", ""},
		{"E", "SABAPDOCU", "123", "A cell with  double  spaces kept internally"},
	}

	blob, err := EncodeRows(cols, rows)
	if err != nil {
		t.Fatalf("EncodeRows: %v", err)
	}

	gotCols, gotRows, err := DecodeRows(blob)
	if err != nil {
		t.Fatalf("DecodeRows: %v", err)
	}
	if !reflect.DeepEqual(cols, gotCols) {
		t.Fatalf("columns differ:\n want %+v\n  got %+v", cols, gotCols)
	}
	if !reflect.DeepEqual(rows, gotRows) {
		t.Fatalf("rows differ:\n want %v\n  got %v", rows, gotRows)
	}
}

// TestEncodeIsDechunkable proves the encoded blob is a real chunked SAP-LZH
// container: ExtractLZHStreams recovers a stream sapcompress can decode.
func TestEncodeIsDechunkable(t *testing.T) {
	blob, err := EncodeRows(sampleCatalog(), [][]string{{"E", "Q6", "001", "hi"}})
	if err != nil {
		t.Fatalf("EncodeRows: %v", err)
	}
	streams := ExtractLZHStreams(blob)
	if len(streams) == 0 {
		t.Fatal("ExtractLZHStreams found no stream")
	}
	if _, err := sapcompress.Decompress(streams[0]); err != nil {
		t.Fatalf("outer stream did not decompress: %v", err)
	}
}

// TestEmptyRows covers a catalog-only blob (the DataPOnDemand case): no rows.
func TestEmptyRows(t *testing.T) {
	cols := sampleCatalog()
	blob, err := EncodeRows(cols, nil)
	if err != nil {
		t.Fatalf("EncodeRows: %v", err)
	}
	gotCols, gotRows, err := DecodeRows(blob)
	if err != nil {
		t.Fatalf("DecodeRows: %v", err)
	}
	if !reflect.DeepEqual(cols, gotCols) {
		t.Fatalf("columns differ: want %+v got %+v", cols, gotCols)
	}
	if len(gotRows) != 0 {
		t.Fatalf("want 0 rows, got %d", len(gotRows))
	}
}

// capturedT100Blob returns the RFC_TR value from the capture frame whose ALV
// blob decodes to the most columns, or skips the test when the (gitignored)
// capture is absent. The capture holds a live session, so this test reads only
// the public T100 column catalog from it and embeds nothing.
func capturedT100Blob(t *testing.T) []byte {
	t.Helper()
	const path = "../../captures/probe.jsonl"
	if _, err := os.Stat(path); err != nil {
		t.Skip("capture not present (gitignored); skipping live-blob test")
	}
	cap, err := replay.Load(path, 1)
	if err != nil {
		t.Skipf("capture load: %v", err)
	}
	var best []byte
	bestCols := 0
	for _, f := range cap.Server {
		m, err := diag.ParseMessage(f.Data, false)
		if err != nil {
			continue
		}
		for _, it := range diag.ParseItems(m.Body) {
			if it.Type != diag.ItemAPPL || it.ID != 0x08 {
				continue
			}
			cols, _, err := DecodeRows(it.Value)
			if err == nil && len(cols) > bestCols {
				bestCols = len(cols)
				best = append([]byte(nil), it.Value...)
			}
		}
	}
	if best == nil {
		t.Skip("no decodable ALV catalog in capture")
	}
	return best
}

// TestCapturedCatalog decodes the real T100 catalog off the wire and checks the
// four expected columns and their widths.
func TestCapturedCatalog(t *testing.T) {
	blob := capturedT100Blob(t)
	cols, _, err := DecodeRows(blob)
	if err != nil {
		t.Fatalf("DecodeRows(captured): %v", err)
	}
	want := sampleCatalog()
	if !reflect.DeepEqual(cols, want) {
		t.Fatalf("captured catalog:\n want %+v\n  got %+v", want, cols)
	}
	t.Logf("decoded real T100 catalog: %+v", cols)
}

// TestCapturedStability proves DecodeRows → EncodeRows → DecodeRows is stable on
// the captured blob (byte-identical re-encoding is not expected; the decoded
// form is).
func TestCapturedStability(t *testing.T) {
	blob := capturedT100Blob(t)
	cols1, rows1, err := DecodeRows(blob)
	if err != nil {
		t.Fatalf("first DecodeRows: %v", err)
	}
	reblob, err := EncodeRows(cols1, rows1)
	if err != nil {
		t.Fatalf("EncodeRows: %v", err)
	}
	cols2, rows2, err := DecodeRows(reblob)
	if err != nil {
		t.Fatalf("second DecodeRows: %v", err)
	}
	if !reflect.DeepEqual(cols1, cols2) {
		t.Fatalf("columns not stable:\n first %+v\n second %+v", cols1, cols2)
	}
	if !reflect.DeepEqual(rows1, rows2) {
		t.Fatalf("rows not stable:\n first %v\n second %v", rows1, rows2)
	}
}

// TestGridRoundTrip proves EncodeGrid → DecodeGrid recovers the exact cell
// values AND per-cell colours — the coloured-ALV path.
func TestGridRoundTrip(t *testing.T) {
	cols := []Column{{"R", 73, 10}, {"V1", 67, 6}, {"V2", 67, 6}, {"V3", 67, 6}, {"V4", 67, 6}}
	var rows [][]string
	var colours [][]int
	for r := 1; r <= 12; r++ {
		rows = append(rows, []string{
			// R is right-justified by SapColour-independent formatting.
			padLeft(r), "AAAAAA", "BBBBBB", "CCCCCC", "DDDDDD"})
		cf := []int{0}
		for c := 1; c <= 4; c++ {
			cf = append(cf, ColourField((r+c)%7+1, true, false))
		}
		colours = append(colours, cf)
	}
	blob, err := EncodeGrid(cols, rows, colours)
	if err != nil {
		t.Fatalf("EncodeGrid: %v", err)
	}
	g, err := DecodeGrid(blob)
	if err != nil {
		t.Fatalf("DecodeGrid: %v", err)
	}
	if len(g.Rows) != len(rows) {
		t.Fatalf("row count: got %d want %d", len(g.Rows), len(rows))
	}
	for r := range rows {
		for c := range cols {
			if g.Rows[r][c] != rows[r][c] {
				t.Errorf("cell[%d][%d] value: got %q want %q", r, c, g.Rows[r][c], rows[r][c])
			}
			if g.Colours[r][c] != colours[r][c] {
				t.Errorf("cell[%d][%d] colour: got %d want %d", r, c, g.Colours[r][c], colours[r][c])
			}
		}
	}
}

// capturedColourGrid returns the real coloured-ALV RFC_TR value (APPL id 0x08)
// from conn 1 frame #14 of the alvcolor capture — the 13429-byte grid of
// AAAAAA/BBBBBB… demo cells — or skips when the (gitignored) capture is absent.
// Only the public demo cells are read; nothing is embedded.
func capturedColourGrid(t *testing.T) []byte {
	t.Helper()
	const path = "../../captures/alvcolor.jsonl"
	if _, err := os.Stat(path); err != nil {
		t.Skip("capture not present (gitignored); skipping coloured-grid test")
	}
	cap, err := replay.Load(path, 1)
	if err != nil {
		t.Skipf("capture load: %v", err)
	}
	for _, f := range cap.Server {
		if f.Index != 14 {
			continue
		}
		m, err := diag.ParseMessage(f.Data, false)
		if err != nil {
			continue
		}
		for _, it := range diag.ParseItems(m.Body) {
			if it.Type != diag.ItemAPPL || it.ID != 0x08 {
				continue
			}
			if g, err := DecodeGrid(it.Value); err == nil && len(g.Rows) > 0 {
				return append([]byte(nil), it.Value...)
			}
		}
	}
	t.Skip("no coloured ALV grid at conn 1 frame #14")
	return nil
}

// TestPatchColoursCaptured recolours a REAL captured coloured-ALV RFC_TR value
// and pins the two guarantees of the template+swap approach: DecodeGrid reads
// back the same cell text with our colours, and every byte outside the single
// re-Compressed stream (and its 4-byte length prefix) is untouched.
func TestPatchColoursCaptured(t *testing.T) {
	orig := capturedColourGrid(t)

	before, err := DecodeGrid(orig)
	if err != nil {
		t.Fatalf("DecodeGrid(orig): %v", err)
	}

	// A colour that varies per cell and exercises the flags, distinct from the
	// captured (row+col) pattern so a stale packet would be caught.
	fn := func(row, col int) int { return ColourField((row*2+col)%7+1, false, true) }

	patched, err := PatchColours(orig, fn)
	if err != nil {
		t.Fatalf("PatchColours: %v", err)
	}

	after, err := DecodeGrid(patched)
	if err != nil {
		t.Fatalf("DecodeGrid(patched): %v", err)
	}

	if len(after.Rows) != len(before.Rows) {
		t.Fatalf("row count changed: %d -> %d", len(before.Rows), len(after.Rows))
	}
	for r := range before.Rows {
		for c := range before.Rows[r] {
			if after.Rows[r][c] != before.Rows[r][c] {
				t.Errorf("cell[%d][%d] text changed: %q -> %q", r, c, before.Rows[r][c], after.Rows[r][c])
			}
			if want := fn(r, c); after.Colours[r][c] != want {
				t.Errorf("cell[%d][%d] colour: got %d want %d", r, c, after.Colours[r][c], want)
			}
		}
	}

	// Byte preservation: the only change is one LZH stream's span plus the
	// 4-byte compressed-length prefix in front of its header.
	p := commonPrefix(orig, patched)
	s := commonSuffix(orig, patched)
	loMid, hiMid := p, len(orig)-s // the changed middle of the original
	if loMid >= hiMid {
		t.Fatalf("no change detected (prefix=%d suffix=%d) — patch did nothing", p, s)
	}
	covered := false
	for pos := 0; ; {
		j := bytes.Index(orig[pos:], lzhMagic)
		if j < 0 {
			break
		}
		hdr := pos + j - 4
		pos = pos + j + len(lzhMagic)
		if hdr < lzhCompLen {
			continue
		}
		if hdr-lzhCompLen <= loMid && hiMid <= chunkedStreamEnd(orig, hdr) {
			covered = true
			break
		}
	}
	if !covered {
		t.Errorf("changed region [%d..%d) is not confined to one stream + its length prefix", loMid, hiMid)
	}
	t.Logf("orig=%d bytes, patched=%d bytes; changed only [%d..%d) (one stream + length prefix)",
		len(orig), len(patched), loMid, hiMid)

	// RecolourGrid is the convenience form and must agree with PatchColours.
	grid := make([][]int, len(before.Rows))
	for r := range before.Rows {
		grid[r] = make([]int, len(before.Rows[r]))
		for c := range before.Rows[r] {
			grid[r][c] = fn(r, c)
		}
	}
	viaGrid, err := RecolourGrid(orig, grid)
	if err != nil {
		t.Fatalf("RecolourGrid: %v", err)
	}
	if !bytes.Equal(viaGrid, patched) {
		t.Errorf("RecolourGrid and PatchColours disagree (%d vs %d bytes)", len(viaGrid), len(patched))
	}
}

func commonPrefix(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func commonSuffix(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[len(a)-1-i] != b[len(b)-1-i] {
			return i
		}
	}
	return n
}

func padLeft(n int) string {
	s := ""
	for i := 0; i < 10; i++ {
		s = "0" + s
	}
	d := []byte(s)
	x := n
	for i := 9; i >= 0 && x > 0; i-- {
		d[i] = byte('0' + x%10)
		x /= 10
	}
	return string(d)
}
