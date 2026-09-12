package diag

import "encoding/binary"

// An APPL RFC_TR item (id 0x08) carries a GUI-RFC call. The server drives it:
// it calls into the SAP GUI, which acts as an RFC server for the control
// framework, and the call carries OLE-automation methods for the controls the
// GUI hosts (an ALV grid, a toolbar, a container). The sub-id says which leg
// of the call this is: 0x04 the request that sets a control up (its class,
// geometry, menus), 0x00 and 0x01 the IMPORT_XML / EXPORT_XML legs that move a
// control's data as an XML_DATA_STREAM, 0x06 a session/context blob.
//
// The payload is not classic APPC: the open-rfc-go APPC decoder rejects its
// first byte 0x01 as an "APPC protocol version 0x1". It is a tag stream of the
// control framework's own, and only its coarse shape is read here. What is
// reliable across every RFC_TR.04 on the ALV capture is a fixed lead-in
// followed by length-prefixed strings: a two-byte big-endian length and then
// that many bytes. The first such string is the RFC destination the GUI is to
// call back on; "GUICORE_BLOB_DIAG_PARSER", "STREAM", "IMPORT_XML",
// "EXPORT_XML", "XML_DATA_STREAM" and the menu texts follow the same way.
//
// SplitRFCTR is best-effort and never fails: it returns the lead-in bytes
// before the first string as Header, that first string as Destination, and
// every length-prefixed string it can walk out as Strings. It reads structure
// only; the callers must keep a real destination (it holds a host and address)
// out of anything tracked.

// RFCTR is a best-effort split of an RFC_TR payload.
type RFCTR struct {
	// Header is the bytes before the first length-prefixed string.
	Header []byte
	// Destination is the first length-prefixed string, the RFC callback target.
	Destination string
	// Strings is every length-prefixed string, the destination included.
	Strings []string
}

// SplitRFCTR walks an RFC_TR payload into its lead-in, its destination string,
// and its readable strings. It assumes strings are framed as a two-byte
// big-endian length and then that many bytes, which is what the ALV capture
// shows. It returns a zero RFCTR for a payload with no string in it.
func SplitRFCTR(payload []byte) RFCTR {
	var out RFCTR
	headerEnd := -1
	for i := 0; i+2 <= len(payload); {
		s, n, ok := rfcTRStringAt(payload, i)
		if !ok {
			i++
			continue
		}
		if headerEnd < 0 {
			headerEnd = i
			out.Header = payload[:i]
			out.Destination = s
		}
		out.Strings = append(out.Strings, s)
		i += n
	}
	return out
}

// rfcTRStringAt reads a length-prefixed string at offset i. It returns the
// trimmed string, the whole field's byte width (length prefix included), and
// whether the bytes read as text. The length is two bytes big-endian; the
// value must be printable ASCII apart from padding NULs, at least four fifths
// printable, and hold a letter or digit, which is enough to skip the tag and
// binary runs between strings without walking off a real one.
func rfcTRStringAt(b []byte, i int) (string, int, bool) {
	if i+2 > len(b) {
		return "", 0, false
	}
	n := int(binary.BigEndian.Uint16(b[i:]))
	if n < 2 || n > 8192 || i+2+n > len(b) {
		return "", 0, false
	}
	v := b[i+2 : i+2+n]
	printable, alnum := 0, 0
	for _, c := range v {
		switch {
		case c >= 0x20 && c < 0x7f:
			printable++
			if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				alnum++
			}
		case c == 0x00:
			// padding, allowed but not counted as printable
		default:
			return "", 0, false
		}
	}
	if alnum == 0 || printable*5 < len(v)*4 {
		return "", 0, false
	}
	return trimRFCTR(v), 2 + n, true
}

// trimRFCTR drops trailing NULs and spaces, which pad the fixed-width fields.
func trimRFCTR(v []byte) string {
	end := len(v)
	for end > 0 && (v[end-1] == 0x00 || v[end-1] == 0x20) {
		end--
	}
	return string(v[:end])
}
