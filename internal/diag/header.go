// Package diag holds what is known of DIAG's wire format — the header, the
// compression it wraps, the items — with every fact marked by how it is
// known. Nothing here is taken from a captured system's data: the layout is
// protocol knowledge, the captures are the sandbox's.
package diag

import (
	"encoding/binary"
	"fmt"

	"github.com/oisee/sap-tui/internal/sapcompress"
)

// Status is how a fact about the wire is known.
type Status string

const (
	// Confirmed was read off a capture and checked against a second one.
	Confirmed Status = "confirmed"
	// Inferred is protocol lore not yet checked here.
	Inferred Status = "inferred"
	// Raw is bytes nobody has explained.
	Raw Status = "raw"
)

// HeaderLen is the DIAG header's length in bytes.
const HeaderLen = 8

// DPHeaderLen is the dispatcher header a client's first message carries
// before the DIAG header. Inferred.
const DPHeaderLen = 200

// Header is DIAG's eight-byte message header. Inferred until Phase 0 reads
// it off the wire.
type Header struct {
	Mode     byte
	ComFlag  byte
	ModeStat byte
	ErrNo    byte
	MsgType  byte
	MsgInfo  byte
	MsgRC    byte
	Compress byte // 0 none, 1 LZC, 2 LZH
}

// ComFlag bits.
const (
	FlagTermEOS = 0x01
	FlagTermEOC = 0x02
	FlagTermNOP = 0x04
	FlagTermEOP = 0x08
	FlagTermINI = 0x10
	FlagTermCAS = 0x20
	FlagTermNNM = 0x40
	FlagTermGRA = 0x80
)

// ParseHeader reads a DIAG header.
func ParseHeader(b []byte) (Header, error) {
	if len(b) < HeaderLen {
		return Header{}, fmt.Errorf("diag header: %d bytes, want %d", len(b), HeaderLen)
	}
	return Header{Mode: b[0], ComFlag: b[1], ModeStat: b[2], ErrNo: b[3], MsgType: b[4], MsgInfo: b[5], MsgRC: b[6], Compress: b[7]}, nil
}

// Bytes is the header on the wire.
func (h Header) Bytes() []byte {
	return []byte{h.Mode, h.ComFlag, h.ModeStat, h.ErrNo, h.MsgType, h.MsgInfo, h.MsgRC, h.Compress}
}

func (h Header) String() string {
	return fmt.Sprintf("mode=%02x com=%02x stat=%02x err=%02x type=%02x info=%02x rc=%02x compress=%d", h.Mode, h.ComFlag, h.ModeStat, h.ErrNo, h.MsgType, h.MsgInfo, h.MsgRC, h.Compress)
}

// Message is one NI payload taken apart: an optional DP header, the DIAG
// header, and the item bytes — decompressed when they were compressed.
type Message struct {
	DP         []byte
	Header     Header
	Compressed bool
	// Body is the item bytes after decompression.
	Body []byte
	// Note says what was assumed.
	Note string
}

// ParseMessage takes an NI payload apart. firstFromClient says this is the
// first frame a client sent on its connection, which is where the DP
// header is expected.
func ParseMessage(payload []byte, firstFromClient bool) (*Message, error) {
	m := &Message{}
	rest := payload
	if firstFromClient && len(rest) >= DPHeaderLen+HeaderLen {
		m.DP = rest[:DPHeaderLen]
		rest = rest[DPHeaderLen:]
		m.Note = "dp header assumed on the client's first frame (inferred)"
	}
	h, err := ParseHeader(rest)
	if err != nil {
		return nil, err
	}
	m.Header = h
	rest = rest[HeaderLen:]
	if h.Compress != 0 {
		m.Compressed = true
		body, err := sapcompress.Decompress(rest)
		if err != nil {
			return m, fmt.Errorf("decompressing (%d bytes, compress=%d): %w", len(rest), h.Compress, err)
		}
		m.Body = body
		return m, nil
	}
	m.Body = rest
	return m, nil
}

// beUint16 and friends read what the items need.
func beUint16(b []byte) int { return int(binary.BigEndian.Uint16(b)) }
func beUint32(b []byte) int { return int(binary.BigEndian.Uint32(b)) }

// NIControl recognises the NI layer's own keepalive payloads, which carry
// no DIAG header: "NI_PING" and "NI_PONG", eight bytes with a trailing
// NUL. Confirmed on the Phase 0 capture.
func NIControl(payload []byte) (string, bool) {
	if len(payload) != 8 {
		return "", false
	}
	switch string(payload[:7]) {
	case "NI_PING", "NI_PONG":
		return string(payload[:7]), true
	}
	return "", false
}
