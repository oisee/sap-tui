package diag

import "strings"

// A tabstrip travels as a TABSTRIP_TAB item (APPL4 id 0x09 sid 0x10): a chain
// of atoms two per tab — a tab-button atom (etype 0x74) then a field-name atom
// (etype 0x72, the tab's ABAP name, skipped here). The tab-button atom reuses
// the 12-byte atom header, then a body like a pushbutton's but with an extra
// byte and a third offset:
//
//	[header:12] [attr][width][height][?] [2 fcode-off][2 caption-off][2 short-off]
//	[caption padded to width][NUL] [=FCODE NUL] [SHORT NUL]
//
// The offsets count from the atom's first byte. Header byte 2 carries 0x40 on
// the active tab (cross-confirmed by TABSTRIP_DEF's current-tab code). Decoded
// off captures/probe.jsonl (SE80 screen painter, control BS_TABSTR_CONTROL):
// "Attributes" =HD, "Element list" =FL, "Flow logic" =LS.
const atomTabstripTab = 0x74

// Tab is one tab of a tabstrip: its caption, the function code it fires when
// selected (a pushbutton-style "=HD"), and whether it is the active tab.
type Tab struct {
	Caption string
	Fcode   string
	Active  bool
}

// ParseTabstrip decodes a TABSTRIP_TAB item value into its tabs, in order. It
// walks the length-prefixed atom records, decoding the tab-button atoms (0x74)
// and skipping the field-name atoms between them.
func ParseTabstrip(value []byte) []Tab {
	var out []Tab
	for i := 0; i+AtomHeaderLen <= len(value); {
		n := beUint16(value[i:])
		if n < AtomHeaderLen || i+n > len(value) {
			break
		}
		rec := value[i : i+n]
		if rec[4] == atomTabstripTab {
			if t, ok := parseTab(rec); ok {
				out = append(out, t)
			}
		}
		i += n
	}
	return out
}

// parseTab decodes one tab-button atom (etype 0x74).
func parseTab(rec []byte) (Tab, bool) {
	// body: attr[12], width[13], height[14], ?[15], fcode-off[16:18],
	// caption-off[18:20], short-off[20:22] — offsets from rec[0].
	if len(rec) < 22 {
		return Tab{}, false
	}
	t := Tab{Active: rec[2]&0x40 != 0}
	width := int(rec[13])
	fcodeOff := beUint16(rec[16:])
	capOff := beUint16(rec[18:])
	if capOff > 0 && capOff < len(rec) {
		end := capOff + width
		if end > len(rec) {
			end = len(rec)
		}
		t.Caption = strings.TrimRight(nulCut(rec[capOff:end]), " \x00")
	}
	if fcodeOff > 0 && fcodeOff < len(rec) {
		t.Fcode = nulCut(rec[fcodeOff:])
	}
	return t, t.Caption != "" || t.Fcode != ""
}

// nulCut returns b up to the first NUL, as a string.
func nulCut(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
