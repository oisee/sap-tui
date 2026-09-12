package diag

import (
	"encoding/binary"
	"testing"
)

// lp frames a string the way an RFC_TR payload does: a two-byte big-endian
// length, then the bytes, padded with NULs to width.
func lp(s string, width int) []byte {
	body := make([]byte, width)
	copy(body, s)
	out := make([]byte, 2+width)
	binary.BigEndian.PutUint16(out, uint16(width))
	copy(out[2:], body)
	return out
}

// TestSplitRFCTR builds a synthetic payload, never capture bytes. The header
// bytes are all below 0x20 so none reads as a length-prefixed string, and the
// low byte of every real length is below 0x20 too, so a scan cannot mistake a
// length prefix for text a byte early.
func TestSplitRFCTR(t *testing.T) {
	header := []byte{0x01, 0x01, 0x00, 0x08, 0x01, 0x01}
	payload := append([]byte(nil), header...)
	payload = append(payload, lp("1-2-3-DEST-SYNTHETIC", 24)...) // the destination, padded
	payload = append(payload, 0x01, 0x27, 0x00, 0x07)            // a tag run between strings
	payload = append(payload, lp("IMPORT_XML", 10)...)
	payload = append(payload, 0x02, 0x03, 0x02)
	payload = append(payload, lp("XML_DATA_STREAM", 15)...)
	payload = append(payload, lp("STREAM", 6)...)

	got := SplitRFCTR(payload)

	if string(got.Header) != string(header) {
		t.Errorf("header %x, want %x", got.Header, header)
	}
	if got.Destination != "1-2-3-DEST-SYNTHETIC" {
		t.Errorf("destination %q", got.Destination)
	}
	want := []string{"1-2-3-DEST-SYNTHETIC", "IMPORT_XML", "XML_DATA_STREAM", "STREAM"}
	if len(got.Strings) != len(want) {
		t.Fatalf("%d strings, want %d: %q", len(got.Strings), len(want), got.Strings)
	}
	for i, w := range want {
		if got.Strings[i] != w {
			t.Errorf("string %d = %q, want %q", i, got.Strings[i], w)
		}
	}
}

// TestSplitRFCTREmpty keeps the best-effort promise: no string, no panic.
func TestSplitRFCTREmpty(t *testing.T) {
	got := SplitRFCTR([]byte{0x01, 0x02, 0x03, 0x04})
	if got.Destination != "" || len(got.Strings) != 0 || got.Header != nil {
		t.Errorf("want a zero split, got %+v", got)
	}
}
