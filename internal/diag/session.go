package diag

import (
	"encoding/binary"
	"fmt"
)

// This file describes the minimal server responses a rogue server sends to
// bring a real SAP GUI to a drawn screen, and the encoder that turns items
// and a header back into wire bytes (the inverse of ParseItems /
// ParseMessage). It is built from captures/probe.jsonl but copies no value
// that identifies a system, a host, a user, or a session: every such field
// below is a zero-filled placeholder of the length the capture showed.
// Only structural facts — item type/id/sid, fixed lengths, and the constant
// XML data-manager document — are real.

// APPL sub-ids used by the frames described here. The names are the ones in
// names.go; a few are mislabelled relative to their payload (the item called
// SYSID carries a codepage number, the one called CODEPAGE carries the
// system id), but the ids and lengths are what the wire needs.
const (
	sidCodepageApp  = 0x11
	sidSysname      = 0x23
	sidSysid        = 0x24
	sidDBName       = 0x21
	sidCodepage     = 0x02
	sidFloatFormat  = 0x03
	sidUsername     = 0x0a
	sidGUILabel     = 0x1f
	sidCPUName      = 0x22
	sidProfile      = 0x2d
	sidRelease      = 0x29
	sidSessionIcon  = 0x16
	sidClient       = 0x0c
	sidStatusFlag   = 0x15 // ST_R3INFO.15, the 0-length flag on info=02 frames
	sidUserCounter  = 0x26 // ST_USER.26, the internal-mode / roundtrip counter
	sidUserBlob     = 0x18 // ST_USER.18
	sidVarWindow    = 0x07 // VARINFO.07
	sidVarLayout    = 0x06 // VARINFO.06
	sidVarProduct   = 0x09 // VARINFO.09 ("SAP")
	sidVarTitle     = 0x0a // VARINFO.0a, the window title
	sidDynn         = 0x01 // DYNN.01, the dynpro descriptor
	sidDyntAtom     = 0x02 // DYNT.DYNT_ATOM, the screen fields
	sidDyntTrailer  = 0x0b // DYNT.0b
	sidMnuGUIStatus = 0x01 // MNUENTRY.01
)

// ServerFrame is one response: a header and its ordered items. The items'
// values are placeholders; sizes match the capture.
type ServerFrame struct {
	Header Header
	Items  []Item
}

// appl builds a type-0x10 item with a placeholder value of n bytes.
func appl(id, sid byte, n int) Item {
	return Item{Type: ItemAPPL, ID: id, SID: sid, Value: make([]byte, n), Status: Inferred}
}

// appl4 builds a type-0x12 item with a placeholder value of n bytes.
func appl4(id, sid byte, n int) Item {
	return Item{Type: ItemAPPL4, ID: id, SID: sid, Value: make([]byte, n), Status: Inferred}
}

// applVal builds a type-0x10 item with a given (non-identifying) value.
func applVal(id, sid byte, v []byte) Item {
	return Item{Type: ItemAPPL, ID: id, SID: sid, Value: v, Status: Inferred}
}

// XMLDataManager is the data-manager document the server sends in every
// drawn screen. It is a constant of the protocol, not capture data.
var XMLDataManager = []byte(`<?xml version="1.0" encoding="utf-16"?><DATAMANAGER/>`)

// envBlock is the run of items every server frame opens with: the ST_R3INFO
// environment, the mode counter, the client, and the session token. Every
// identifying member is a zero placeholder of the observed length.
func envBlock() []Item {
	return []Item{
		appl(0x06, sidCodepageApp, 32),                             // app-server identity blob (identifying)
		appl(0x06, sidSysname, 16),                                 // identifying
		appl(0x06, sidSysid, 5),                                    // codepage number, NUL-padded
		appl(0x06, sidDBName, 32),                                  // DBNAME GUID (identifying)
		appl(0x06, sidCodepage, 3),                                 // 3-letter system id (identifying)
		appl(0x06, sidFloatFormat, 10),                             // app host name (identifying)
		appl(0x06, 0x19, 2),                                        // flags
		appl(0x06, 0x01, 2),                                        // flags
		appl(0x06, sidUsername, 2),                                 // short code, not the user name
		appl(0x06, sidGUILabel, 18),                                // GUID/label blob (identifying)
		appl(0x06, 0x18, 2),                                        // flags
		appl(0x06, sidCPUName, 4),                                  // number
		appl(0x06, 0x25, 10),                                       // instance/host token (identifying)
		appl(0x06, sidProfile, 8),                                  // profile name
		appl(0x06, sidRelease, 13),                                 // release / kernel numbers
		appl(0x06, sidSessionIcon, 4),                              // number
		appl(0x04, sidUserCounter, 4),                              // internal-mode / roundtrip counter
		appl(0x06, sidClient, 3),                                   // logon client
		{Type: ItemSES, Value: make([]byte, 16), Status: Inferred}, // session/mode state token
	}
}

// StatusFrame is an info=02 frame: the environment block, the 0-length
// status flag, and EOM. Small, uncompressed, no dynpro. The server flushes
// these while it prepares a real screen.
func StatusFrame() ServerFrame {
	items := envBlock()
	items = append(items,
		appl(0x06, sidStatusFlag, 0), // ST_R3INFO.15: presence marks an info=02 frame
		Item{Type: ItemEOM, Status: Inferred},
	)
	return ServerFrame{
		Header: Header{MsgInfo: 0x02, Compress: 0},
		Items:  items,
	}
}

// LogonScreen is the minimal info=01 frame that makes a real GUI draw a
// screen: the environment block, window/menu/layout information, one dynpro
// with one field atom, the data-manager document, and EOM. Menu and layout
// items are informational; the dynpro, the atom, the XML and EOM are what a
// screen needs. Sizes match the capture; every value is a placeholder except
// the product string and the XML document.
func LogonScreen() ServerFrame {
	items := envBlock()
	items = append(items,
		appl(0x0c, sidVarWindow, 16),                                  // VARINFO.07: window/variant info
		applVal(0x0c, sidVarProduct, []byte("SAP")),                   // VARINFO.09: product string (not identifying)
		appl(0x0c, sidVarTitle, 20),                                   // VARINFO.0a: window title text (identifying)
		appl(0x0c, sidVarLayout, 33),                                  // VARINFO.06: layout
		appl4(0x0b, sidMnuGUIStatus, 32),                              // MNUENTRY.01: GUI status / menu (informational)
		appl(0x05, sidDynn, 22),                                       // DYNN.01: dynpro descriptor
		appl4(0x09, sidDyntAtom, 48),                                  // DYNT.DYNT_ATOM: the screen fields
		appl(0x09, sidDyntTrailer, 10),                                // DYNT.0b: dynpro trailer
		Item{Type: ItemXML, Value: XMLDataManager, Status: Confirmed}, // data manager
		appl4(0x04, sidUserBlob, 8),                                   // ST_USER.18: session blob
		Item{Type: ItemEOM, Status: Inferred},                         // terminator
	)
	return ServerFrame{
		Header: Header{MsgInfo: 0x01, Compress: 0},
		Items:  items,
	}
}

// EncodeItems turns items back into a message body, the inverse of
// ParseItems. APPL carries a 2-byte length, APPL4 a 4-byte length, XML a
// 4-byte length; every other type is written as its type byte followed by
// its value, which for the fixed-length types (SES, EOM, ...) must already
// be the right length.
func EncodeItems(items []Item) []byte {
	var out []byte
	var n2 [2]byte
	var n4 [4]byte
	for _, it := range items {
		switch it.Type {
		case ItemAPPL:
			out = append(out, it.Type, it.ID, it.SID)
			binary.BigEndian.PutUint16(n2[:], uint16(len(it.Value)))
			out = append(out, n2[:]...)
			out = append(out, it.Value...)
		case ItemAPPL4:
			out = append(out, it.Type, it.ID, it.SID)
			binary.BigEndian.PutUint32(n4[:], uint32(len(it.Value)))
			out = append(out, n4[:]...)
			out = append(out, it.Value...)
		case ItemXML:
			out = append(out, it.Type)
			binary.BigEndian.PutUint32(n4[:], uint32(len(it.Value)))
			out = append(out, n4[:]...)
			out = append(out, it.Value...)
		default:
			out = append(out, it.Type)
			out = append(out, it.Value...)
		}
	}
	return out
}

// EncodeMessage turns a header and items into an NI payload, the inverse of
// ParseMessage for a server frame (no DP header). Compression is not
// implemented: DIAG accepts compress=0 and the capture shows uncompressed
// responses, so a server can send everything uncompressed. Asking for
// compression is an error rather than a silent lie.
func EncodeMessage(h Header, items []Item, compress bool) ([]byte, error) {
	if compress {
		return nil, fmt.Errorf("diag: compressed encoding not implemented; send compress=0")
	}
	h.Compress = 0
	body := EncodeItems(items)
	out := make([]byte, 0, HeaderLen+len(body))
	out = append(out, h.Bytes()...)
	out = append(out, body...)
	return out, nil
}
