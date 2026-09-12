package diag

import (
	"fmt"
	"strings"
)

// A DYNT_ATOM item (APPL4 id 0x09 sid 0x02) is the screen: a chain of atoms,
// one per element part, each starting with its own length. The layout here
// was read off two captures — a logon, SE38, the screen painter, a
// selection screen, a probe screen pushed 67 times with one value changing,
// and SE16 — and every fact carries how well it held: confirmed means it
// was checked on at least two screens, inferred means it is the plausible
// reading of one. Names of the etypes and attribute bits follow pysap's
// where the capture agrees with them.
//
// Every atom begins with a 12-byte header:
//
//	0-1   length, big-endian, counting these two bytes      confirmed
//	2-3   two flag bytes                                     raw
//	4     etype                                              confirmed
//	5     area: 1 inside a step loop, else 0                 inferred
//	6     line within the loop, 1-based                      inferred
//	7     group: 1 on radio buttons, else 0                  inferred
//	8-9   row, 0-based                                       confirmed
//	10-11 column, 0-based                                    confirmed
//
// Rows and columns never exceeded 19 and 78 on the captures, so their high
// byte has only been seen as zero; the two-byte width is inferred. What
// follows the header depends on the etype and is described at each parser.

// Atom etypes seen on the wire, named by what the element did on screen.
// The value names are pysap's for the same numbers.
const (
	AtomFieldName   = 0x72 // FNAME_1: the ABAP name of the element before it
	AtomPushbutton  = 0x73 // PUSHBUTTON_2
	AtomXMLProperty = 0x78 // XMLPROP: a <Propertybag> for the element before it
	AtomFrame       = 0x7f // FRAME_1: a box with a title
	AtomCheckbox    = 0x80 // CHECKBUTTON_1
	AtomRadioButton = 0x81 // RADIOBUTTON_1
	AtomInputField  = 0x82 // EFIELD_2
	AtomOutputField = 0x83 // OFIELD_2
	AtomLabel       = 0x84 // KEYWORD_2
)

// Attribute bits of the byte that follows the header on most atoms. The
// first six were each seen where the screen element called for them:
// protected on labels and output fields, invisible on the password field,
// right-justified on integer fields, matchcode on a field with F4 help,
// proportional font on labels, 3D on input fields. Invisible and matchcode
// rest on one element each. Bit 0x80 was set on the program-name field of
// SE38 and on most of SE16's input fields and is not explained; pysap
// calls it COMBOSTYLE.
const (
	AttrProtected  = 0x01
	AttrInvisible  = 0x02
	AttrIntensify  = 0x04
	AttrJustRight  = 0x08
	AttrMatchcode  = 0x10
	AttrPropFont   = 0x20
	AttrYes3D      = 0x40
	AttrComboStyle = 0x80
)

var atomNames = map[byte]string{
	AtomFieldName: "name", AtomPushbutton: "button", AtomXMLProperty: "xmlprop", AtomFrame: "frame",
	AtomCheckbox: "checkbox", AtomRadioButton: "radio", AtomInputField: "input", AtomOutputField: "output",
	AtomLabel: "label",
}

// AtomHeaderLen is the fixed part of every atom, the length field included.
const AtomHeaderLen = 12

// Atom is one element part of a screen.
type Atom struct {
	// Offset is where the atom starts in the item value; Size is its length
	// on the wire, the length field included.
	Offset, Size int
	// Flags are header bytes 2 and 3, kept raw. On the captures byte 2 was
	// 0x20 on numeric fields and 0x04 on fields with a value help, byte 3
	// carried 0x01 on fields the client sent back changed, 0x02 on the text
	// of selection-screen parameters and 0x80 on the selected radio button.
	// None of that is more than an association.
	Flags [2]byte
	EType byte
	// Area, Block and Group are header bytes 5 to 7: 1/n/0 for the n-th line
	// of a step loop, 0/1/1 for a radio button, 0/1/0 otherwise.
	Area, Block, Group byte
	// Row and Col are 0-based; the screen painter shows them plus one.
	Row, Col int
	// Attr is the attribute byte (AttrProtected and the rest). A name or
	// xmlprop atom repeats the attribute byte of the element it belongs to.
	Attr byte
	// Length is the element's extent in characters: the text's length for
	// fields, labels, checkboxes and radio buttons, the width for frames
	// and pushbuttons.
	Length int
	// VisibleLength and MaxChars are set on fields: how much of the field
	// is on screen and how many characters it holds. Both are inferred from
	// one field whose three lengths differed (40 characters, 30 shown).
	VisibleLength, MaxChars int
	// Height is set on frames and pushbuttons, in rows. Inferred.
	Height int
	// Text is the label, the field's value, the element name, the XML, the
	// button caption or the frame title, in the session's codepage, padding
	// included. Value trims it.
	Text string
	// Function is a pushbutton's function code, with its leading "=".
	Function string
	// State is ' ' or 'X' on checkboxes and radio buttons.
	State byte
	// Rest is everything after the 12-byte header, for what the parser did
	// not name.
	Rest []byte
	// Status says how well this atom's layout past the header is known.
	Status Status
}

// TypeName is the etype in a word, or its number.
func (a Atom) TypeName() string {
	if n, ok := atomNames[a.EType]; ok {
		return n
	}
	return fmt.Sprintf("0x%02x", a.EType)
}

// Value is Text without the blanks SAP pads the element with, on the right
// for most elements and on the left for right-justified fields.
func (a Atom) Value() string {
	return strings.Trim(a.Text, " \x00")
}

// ParseDyntAtoms splits a DYNT_ATOM item value into atoms. An atom whose
// length is shorter than its header or runs past the value ends the parse
// with an error; the atoms before it are returned with it.
func ParseDyntAtoms(value []byte) ([]Atom, error) {
	var atoms []Atom
	i := 0
	for i < len(value) {
		if i+2 > len(value) {
			return atoms, fmt.Errorf("dynt atom at %d: one trailing byte", i)
		}
		n := beUint16(value[i:])
		if n < AtomHeaderLen {
			return atoms, fmt.Errorf("dynt atom at %d: length %d is shorter than the %d-byte header", i, n, AtomHeaderLen)
		}
		if i+n > len(value) {
			return atoms, fmt.Errorf("dynt atom at %d: length %d runs past the item's %d bytes", i, n, len(value))
		}
		b := value[i : i+n]
		a := Atom{
			Offset: i, Size: n,
			Flags: [2]byte{b[2], b[3]},
			EType: b[4],
			Area:  b[5], Block: b[6], Group: b[7],
			Row: beUint16(b[8:]), Col: beUint16(b[10:]),
			Rest: b[AtomHeaderLen:],
		}
		parseAtomBody(&a)
		atoms = append(atoms, a)
		i += n
	}
	return atoms, nil
}

// AreaOrigins reads subscreen area origins from CONTAINER.04 items (APPL
// 0x0a/0x04): each is [1 area id][2 row][2 col][2 width][2 height] big-endian.
// A dynpro with a subscreen (the logon screen's Information box) places that
// area's atoms in coordinates relative to the area, and the area's screen
// origin lives here; area 0 (the main screen) is the implicit (0,0). Returns
// area id -> {row, col}. Read off the logon capture (area 1 -> row 1, col 36,
// the inside of the FRAME box).
func AreaOrigins(items []Item) map[byte][2]int {
	m := map[byte][2]int{}
	for _, it := range items {
		if it.Type == ItemAPPL && it.ID == 0x0a && it.SID == 0x04 && len(it.Value) >= 5 {
			m[it.Value[0]] = [2]int{beUint16(it.Value[1:]), beUint16(it.Value[3:])}
		}
	}
	return m
}

// OffsetAtomsByArea translates each atom by its subscreen area's origin, so a
// subscreen's atoms land where the area sits on screen instead of at (0,0). It
// returns a new slice; atoms in an area with no origin (area 0, or one not in
// the map) are copied unchanged.
func OffsetAtomsByArea(atoms []Atom, origins map[byte][2]int) []Atom {
	out := make([]Atom, len(atoms))
	copy(out, atoms)
	for i := range out {
		if o, ok := origins[out[i].Area]; ok {
			out[i].Row += o[0]
			out[i].Col += o[1]
		}
	}
	return out
}

// parseAtomBody reads what follows the header for the etypes seen so far.
func parseAtomBody(a *Atom) {
	r := a.Rest
	switch a.EType {
	case AtomInputField, AtomOutputField, AtomLabel:
		// attr, two bytes always zero on 500 atoms, the text's length, the
		// visible length, a two-byte maximum, the text. The first length
		// equalled the remaining byte count on every field of both captures.
		if len(r) < 7 {
			a.Status = Raw
			return
		}
		a.Attr = r[0]
		a.Length = int(r[3])
		a.VisibleLength = int(r[4])
		a.MaxChars = beUint16(r[5:])
		a.Text = string(r[7:])
		a.Status = Confirmed
		if a.Length != len(r)-7 {
			a.Status = Inferred
		}
	case AtomFieldName, AtomXMLProperty:
		// attr, then the name or the XML to the end of the atom. No
		// terminator, no length: 616 name atoms and 206 xmlprops.
		if len(r) < 1 {
			a.Status = Raw
			return
		}
		a.Attr = r[0]
		a.Text = string(r[1:])
		a.Length = len(a.Text)
		a.Status = Confirmed
	case AtomPushbutton:
		// attr, width and height in characters, then two offsets counted
		// from the atom's first byte: where the function code starts and
		// where the caption starts. Both strings end in NUL. The caption
		// offset was 19 on all 43 buttons, the width matched the button
		// drawn (16 for a captioned one, 2 or 3 for an icon).
		if len(r) < 7 {
			a.Status = Raw
			return
		}
		a.Attr = r[0]
		a.Length = int(r[1])
		a.Height = int(r[2])
		fcode := beUint16(r[3:]) - AtomHeaderLen
		text := beUint16(r[5:]) - AtomHeaderLen
		if text < 7 || text > fcode || fcode > len(r) {
			a.Status = Raw
			return
		}
		a.Text = strings.TrimRight(string(r[text:fcode]), "\x00")
		a.Function = strings.TrimRight(string(r[fcode:]), "\x00")
		a.Status = Confirmed
	case AtomFrame:
		// attr, height and width as two-byte numbers, the title padded to
		// the width. The width equalled the remaining bytes on all 22
		// frames; the height is inferred from a box whose contents sat on
		// the rows it claimed.
		if len(r) < 5 {
			a.Status = Raw
			return
		}
		a.Attr = r[0]
		a.Height = beUint16(r[1:])
		a.Length = beUint16(r[3:])
		a.Text = string(r[5:])
		a.Status = Confirmed
		if a.Length != len(r)-5 {
			a.Status = Inferred
		}
	case AtomCheckbox, AtomRadioButton:
		// attr, the state as ' ' or 'X', the text's length as two bytes,
		// six bytes, the text's length again as one byte, the text. The six
		// bytes were zero except on five checkboxes of the screen painter,
		// where they read 00 00 03 00 03 00 and the text began "=HD"; what
		// that means is not known. A client sending a radio group back
		// writes attr, state, the element's length, six zero bytes, a zero
		// count and one more zero byte, with no text; the extra byte is not
		// explained.
		if len(r) < 11 {
			a.Status = Raw
			return
		}
		a.Attr = r[0]
		a.State = r[1]
		a.Length = beUint16(r[2:])
		a.Status = Inferred
		if a.Length == len(r)-11 && int(r[10]) == a.Length {
			a.Text = string(r[11:])
			return
		}
		a.Text = strings.TrimRight(string(r[11:]), "\x00")
	default:
		a.Status = Raw
	}
}
