package cfw

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/oisee/sap-tui/internal/alv"
	"github.com/oisee/sap-tui/internal/sapcompress"
)

// Splicing a modified inner stream back into an RFC_TR payload. To emit an
// answer we take a captured RFC_TR.01, rewrite the value pool's _RESULT
// handles (FillResults), and put the re-compressed pool back where it sat —
// reusing the payload's string envelope and every other stream verbatim.
//
// A stream sits in the payload as an 8-byte SAP-LZH header (found by the
// 12 1f 9d magic) followed by RFC rows: 03 05 03 05 <len BE> <payload>, closed
// by 03 05 03 06 00 00. ExtractLZHStreams (pkg/alv) de-chunks; the mirror below
// re-chunks, so a modified stream re-frames exactly as the wire expects.

var (
	lzhMagic  = []byte{0x12, 0x1f, 0x9d}
	rowMarker = []byte{0x03, 0x05, 0x03, 0x05}
	endMarker = []byte{0x03, 0x05, 0x03, 0x06}
)

const (
	lzhHeaderSize = 8
	rfcRowPayload = 250
)

// rechunk fragments a complete compressed stream into RFC rows, the inverse of
// pkg/alv's de-chunking: the 8-byte header verbatim, then 250-byte pieces each
// behind a 03 05 03 05 <len BE> marker, closed with 03 05 03 06 00 00.
func rechunk(stream []byte) []byte {
	if len(stream) < lzhHeaderSize {
		return append([]byte(nil), stream...)
	}
	out := make([]byte, 0, len(stream)+len(stream)/rfcRowPayload*6+16)
	out = append(out, stream[:lzhHeaderSize]...)
	body := stream[lzhHeaderSize:]
	for off := 0; off < len(body); off += rfcRowPayload {
		end := off + rfcRowPayload
		if end > len(body) {
			end = len(body)
		}
		piece := body[off:end]
		out = append(out, rowMarker...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(piece)))
		out = append(out, piece...)
	}
	out = append(out, endMarker...)
	out = append(out, 0x00, 0x00)
	return out
}

// streamSpan returns the byte range [hdr:end) that one chunked SAP-LZH stream
// occupies in the payload, starting from its 8-byte header at hdr — through the
// rows to just past the 03 05 03 06 00 00 terminator.
func streamSpan(b []byte, hdr int) (end int, ok bool) {
	i := hdr + lzhHeaderSize
	if i > len(b) {
		return 0, false
	}
	for i < len(b) {
		if i+6 <= len(b) && bytes.Equal(b[i:i+4], rowMarker) {
			n := int(binary.BigEndian.Uint16(b[i+4 : i+6]))
			i += 6 + n
			continue
		}
		if i+4 <= len(b) && bytes.Equal(b[i:i+4], endMarker) {
			e := i + 4
			if e+2 <= len(b) {
				e += 2 // the trailing 00 00
			}
			return e, true
		}
		i++
	}
	return len(b), true
}

// compressPool re-compresses a modified value pool into a SAP-LZH stream the
// kernel accepts: Huffman-only via alv.CompressExact, at a length close to the
// natural compressed size (NOT padded up — a padded stream is dozens of times
// too large and the kernel resets the connection). alv.Compress gives the
// natural-size estimate; CompressExact is then probed up from there to the
// smallest reachable exact length.
func compressPool(pool []byte) ([]byte, error) {
	est := len(pool)/4 + 64
	if z0, err := alv.Compress(pool); err == nil {
		est = len(z0)
	}
	for extra := 0; extra < 8192; extra++ {
		if z, ok := alv.CompressExact(pool, est+extra); ok {
			return z, nil
		}
	}
	return nil, fmt.Errorf("cfw: could not compress a %d-byte value pool near %d bytes", len(pool), est)
}

// SpliceValuePool replaces the value-pool stream inside an RFC_TR payload with
// newPool (decompressed), leaving the string envelope and the other streams
// exactly as they were. It locates each SAP-LZH stream, decompresses to
// classify it, re-compresses and re-chunks newPool, and splices it in place.
func SpliceValuePool(rfctrValue, newPool []byte) ([]byte, error) {
	pos := 0
	for {
		j := bytes.Index(rfctrValue[pos:], lzhMagic)
		if j < 0 {
			break
		}
		hdr := pos + j - 4
		next := pos + j + len(lzhMagic)
		if hdr < 0 {
			pos = next
			continue
		}
		raw := alv.ExtractLZHStreams(rfctrValue[hdr:])
		if len(raw) == 0 {
			pos = next
			continue
		}
		dec, derr := sapcompress.Decompress(raw[0])
		if derr != nil {
			pos = next
			continue
		}
		if classifyStream(dec) != streamValues {
			pos = next
			continue
		}
		end, ok := streamSpan(rfctrValue, hdr)
		if !ok {
			return nil, fmt.Errorf("cfw: value-pool stream span not found")
		}
		comp, cerr := compressPool(newPool)
		if cerr != nil {
			return nil, cerr
		}
		chunked := rechunk(comp)
		out := make([]byte, 0, hdr+len(chunked)+(len(rfctrValue)-end))
		out = append(out, rfctrValue[:hdr]...)
		out = append(out, chunked...)
		out = append(out, rfctrValue[end:]...)
		return out, nil
	}
	return nil, fmt.Errorf("cfw: no value-pool stream in the RFC_TR payload")
}
