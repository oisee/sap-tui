package diag

import "encoding/binary"

// The inverse of ParseDyntAtoms: build a DYNT_ATOM item value from atoms we
// describe, so a screen of our own making draws on a real GUI. Only the
// etypes a plain screen needs are encoded — label, output and input field,
// the field name that follows each, the pushbutton and the checkbox. Each
// encoder is the exact mirror of the parser above it, so an atom encoded
// and parsed back is the atom it started as; the round-trip test holds that.

func putU16(b []byte, v int) { binary.BigEndian.PutUint16(b, uint16(v)) }

// Encode writes one atom as it travels on the wire, the 12-byte header and
// the body its etype calls for. An etype the encoder does not know yields
// its Rest after the header, so an atom read from the wire re-encodes byte
// for byte whether or not this code names its body.
func (a Atom) Encode() []byte {
	var body []byte
	switch a.EType {
	case AtomInputField, AtomOutputField, AtomLabel:
		text := []byte(a.Text)
		body = make([]byte, 7+len(text))
		body[0] = a.Attr
		body[3] = byte(len(text))
		body[4] = byte(a.VisibleLength)
		putU16(body[5:], a.MaxChars)
		copy(body[7:], text)
	case AtomFieldName, AtomXMLProperty:
		body = append([]byte{a.Attr}, []byte(a.Text)...)
	case AtomPushbutton:
		caption := append([]byte(a.Text), 0)
		fcode := append([]byte(a.Function), 0)
		// The caption sits right after the 7-byte body head; the function
		// code follows it. Offsets are counted from the atom's first byte.
		captionOff := AtomHeaderLen + 7
		fcodeOff := captionOff + len(caption)
		body = make([]byte, 7)
		body[0] = a.Attr
		body[1] = byte(a.Length)
		body[2] = byte(a.Height)
		putU16(body[3:], fcodeOff)
		putU16(body[5:], captionOff)
		body = append(body, caption...)
		body = append(body, fcode...)
	case AtomCheckbox, AtomRadioButton:
		text := []byte(a.Text)
		body = make([]byte, 11+len(text))
		body[0] = a.Attr
		body[1] = a.State
		putU16(body[2:], len(text))
		body[10] = byte(len(text))
		copy(body[11:], text)
	case AtomFrame:
		// attr, height and width as two-byte numbers, then the title — the
		// mirror of the parser. Without this a synthesized group box (scr.Frame)
		// encoded to an empty body and the real GUI drew no box outline.
		title := []byte(a.Text)
		body = make([]byte, 5+len(title))
		body[0] = a.Attr
		putU16(body[1:], a.Height)
		putU16(body[3:], a.Length)
		copy(body[5:], title)
	default:
		body = a.Rest
	}
	n := AtomHeaderLen + len(body)
	out := make([]byte, n)
	putU16(out, n)
	out[2], out[3] = a.Flags[0], a.Flags[1]
	out[4] = a.EType
	out[5], out[6], out[7] = a.Area, a.Block, a.Group
	putU16(out[8:], a.Row)
	putU16(out[10:], a.Col)
	copy(out[AtomHeaderLen:], body)
	return out
}

// EncodeDyntAtoms lays a chain of atoms into one DYNT_ATOM item value.
func EncodeDyntAtoms(atoms []Atom) []byte {
	var out []byte
	for _, a := range atoms {
		out = append(out, a.Encode()...)
	}
	return out
}

// Label is a static caption at row, col (both 0-based).
func Label(row, col int, text string) Atom {
	return Atom{EType: AtomLabel, Row: row, Col: col, Attr: AttrProtected,
		Length: len(text), VisibleLength: len(text), MaxChars: len(text), Text: text, Status: Confirmed}
}

// OutputField is a protected value of the given on-screen width. A numeric
// field is right-justified, the way the counter's INT4 field was.
func OutputField(row, col, width int, value string, numeric bool) Atom {
	attr := byte(AttrProtected)
	if numeric {
		attr |= AttrJustRight
	}
	return Atom{EType: AtomOutputField, Row: row, Col: col, Attr: attr,
		Length: len(value), VisibleLength: width, MaxChars: width, Text: value, Status: Confirmed}
}

// InputField is an editable field of the given width holding value.
func InputField(row, col, width int, value string) Atom {
	return Atom{EType: AtomInputField, Row: row, Col: col, Attr: AttrYes3D,
		Length: len(value), VisibleLength: width, MaxChars: width, Text: value, Status: Confirmed}
}

// FieldName is the ABAP name atom that follows an element and ties the
// value the client sends back to a field.
func FieldName(row, col int, name string, attr byte) Atom {
	return Atom{EType: AtomFieldName, Row: row, Col: col, Attr: attr,
		Length: len(name), Text: name, Status: Confirmed}
}
