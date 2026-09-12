package main

import (
	"encoding/binary"
	"fmt"

	"github.com/oisee/sap-tui/internal/diag"
)

// A general PAI: the frame a GUI sends when the user acts on a screen. Its
// shape, read off the captures, is the client environment block a real GUI
// repeats on every frame, then what changed: the OK-code (VARINFO.04) when a
// command or function was fired, the input atoms the user edited (an APPL
// DYNT_ATOM, 2-byte length) or an empty DYNT.0a when none, the cursor field
// (DYNT.0b), the window metrics XML, EOM. The environment values are taken
// from a captured client frame so ours are byte-for-byte the shape a real
// GUI's were; only the session id, the counter, the dynpro descriptor and the
// user's own changes are ours.

// envTemplate holds the client items a captured PAI carried, by key.
type envTemplate struct {
	header diag.Header
	items  map[[3]byte]diag.Item
}

func itemKey(t, id, sid byte) [3]byte { return [3]byte{t, id, sid} }

// newEnvTemplate indexes the items of a captured client frame (the logon PAI
// works: it carries the full environment block).
func newEnvTemplate(frame []byte) (*envTemplate, error) {
	m, err := diag.ParseMessage(frame, false)
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	e := &envTemplate{header: m.Header, items: map[[3]byte]diag.Item{}}
	for _, it := range diag.ParseItems(m.Body) {
		k := itemKey(it.Type, it.ID, it.SID)
		if it.Type == diag.ItemXML || it.Type == diag.ItemSES {
			k = itemKey(it.Type, 0, 0)
		}
		if _, dup := e.items[k]; !dup {
			e.items[k] = it
		}
	}
	return e, nil
}

// take copies one template item; ok is false when the capture had none.
func (e *envTemplate) take(t, id, sid byte) (diag.Item, bool) {
	it, ok := e.items[itemKey(t, id, sid)]
	if !ok {
		return diag.Item{}, false
	}
	it.Value = append([]byte{}, it.Value...)
	return it, true
}

// paiInput is what a PAI carries of the user's action.
type paiInput struct {
	OKCode  string      // the OK-code field: "/nse38", "=FCODE", "" for Enter
	Changed []diag.Atom // the input atoms the user edited, values set
	Cursor  *diag.Atom  // the field the cursor is on, nil when none
	UIEvent int         // a fired function number (UI_EVENT_SOURCE), -1 for none
	SES     []byte      // the session id the server sent last
	DYNN    []byte      // the server's last DYNN.01 (dynpro descriptor)
	Counter uint32      // ST_USER.26
	Stat    byte        // header mode-stat to echo (0xF0 after pushed frames)
}

// uiEventSource builds the APPL UI_EVENT.UI_EVENT_SOURCE (0x0f/0x01) value that
// fires function number n, as a real GUI sends it on a function key: a fixed
// event descriptor with the number at byte 5 and the cursor's row and column
// (little-endian) at bytes 10..13. Read off captures/f8sniff.jsonl (F8 -> 8,
// F3 -> 3), where those two 16-bit fields equalled the frame's DYNT.0b cursor.
func uiEventSource(n int, cursor *diag.Atom) []byte {
	v := []byte{0x0a, 0x00, 0x07, 0x00, 0x01, byte(n), 0, 0, 0, 0, 0, 0, 0, 0, 0x01, 0x00}
	if cursor != nil {
		binary.LittleEndian.PutUint16(v[10:], uint16(cursor.Row))
		binary.LittleEndian.PutUint16(v[12:], uint16(cursor.Col))
	}
	return v
}

// buildPAI lays the client frame for one action.
func buildPAI(e *envTemplate, in paiInput, compress bool) ([]byte, error) {
	var items []diag.Item
	add := func(t, id, sid byte, must bool) error {
		it, ok := e.take(t, id, sid)
		if !ok {
			if must {
				return fmt.Errorf("template has no item %02x/%02x/%02x", t, id, sid)
			}
			return nil
		}
		items = append(items, it)
		return nil
	}

	// ST_USER.26: the counter.
	ctr := make([]byte, 4)
	binary.BigEndian.PutUint32(ctr, in.Counter)
	items = append(items, diag.Item{Type: diag.ItemAPPL, ID: 0x04, SID: 0x26, Value: ctr})

	// SES: the server's session id, at the template's length.
	ses, ok := e.take(diag.ItemSES, 0, 0)
	if !ok {
		return nil, fmt.Errorf("template has no SES item")
	}
	if len(in.SES) == len(ses.Value) {
		ses.Value = append([]byte{}, in.SES...)
	}
	items = append(items, ses)

	if err := add(diag.ItemAPPL, 0x06, 0x23, true); err != nil { // ST_R3INFO.SYSNAME
		return nil, err
	}
	if in.OKCode != "" && in.UIEvent < 0 {
		// A fired function key carries its number in UI_EVENT_SOURCE below, not
		// as an OK-code string, so the two are mutually exclusive.
		items = append(items, diag.Item{Type: diag.ItemAPPL, ID: 0x0c, SID: 0x04, Value: []byte(in.OKCode)})
	}
	for _, sid := range []byte{0x09, 0x1d, 0x0f, 0x19} { // ST_USER block
		if err := add(diag.ItemAPPL, 0x04, sid, false); err != nil {
			return nil, err
		}
	}
	// DYNN.01: the dynpro descriptor of the screen being answered.
	if dynn, ok := e.take(diag.ItemAPPL, 0x05, 0x01); ok {
		if len(in.DYNN) == len(dynn.Value) {
			dynn.Value = append([]byte{}, in.DYNN...)
		}
		items = append(items, dynn)
	}
	for _, sid := range []byte{0x08, 0x06} { // VARINFO.08, VARINFO.06: window metrics
		if err := add(diag.ItemAPPL, 0x0c, sid, false); err != nil {
			return nil, err
		}
	}
	if err := add(diag.ItemAPPL, 0x0a, 0x01, false); err != nil { // CONTAINER.01
		return nil, err
	}
	switch {
	case len(in.Changed) > 0:
		items = append(items, diag.Item{Type: diag.ItemAPPL, ID: 0x09, SID: 0x02, Value: diag.EncodeDyntAtoms(in.Changed)})
	case in.UIEvent >= 0 && in.Cursor != nil:
		// A function key carries the focused field's current state as a
		// DYNT_ATOM, the way a real GUI does even when nothing changed. An empty
		// DYNT.0a here makes the server treat the frame as a passive refresh and
		// ignore the function. Send the cursor field's atom verbatim (server
		// flags, current value) — NOT flagged "changed", which the real GUI
		// reserves for edits and which the server otherwise ignores.
		cur := *in.Cursor
		cur.Rest = nil
		items = append(items, diag.Item{Type: diag.ItemAPPL, ID: 0x09, SID: 0x02, Value: diag.EncodeDyntAtoms([]diag.Atom{cur})})
	default:
		items = append(items, diag.Item{Type: diag.ItemAPPL, ID: 0x09, SID: 0x0a})
	}
	// A fired function key rides as UI_EVENT_SOURCE after the changed fields
	// and before the cursor, the way a real GUI orders them.
	if in.UIEvent >= 0 {
		items = append(items, diag.Item{Type: diag.ItemAPPL, ID: 0x0f, SID: 0x01, Value: uiEventSource(in.UIEvent, in.Cursor)})
	}
	if in.Cursor != nil {
		c := make([]byte, 10)
		c[0] = 1
		binary.BigEndian.PutUint16(c[1:], uint16(in.Cursor.Row))
		binary.BigEndian.PutUint16(c[3:], uint16(in.Cursor.Col))
		binary.BigEndian.PutUint16(c[7:], uint16(len(in.Cursor.Text)))
		items = append(items, diag.Item{Type: diag.ItemAPPL, ID: 0x09, SID: 0x0b, Value: c})
	}
	// A real GUI omits the window-state XML on a function-key frame (it carries
	// it on Enter/OK-code frames). With UI_EVENT present, a frame that also
	// carries the XML is taken as a passive state refresh and the function is
	// ignored, so drop the XML when firing a function key.
	if in.UIEvent < 0 {
		if err := add(diag.ItemXML, 0, 0, false); err != nil {
			return nil, err
		}
	}
	items = append(items, diag.Item{Type: diag.ItemEOM})

	h := e.header
	h.ComFlag = 0
	h.ModeStat = in.Stat
	return encodeClient(h, items, compress)
}

// changedAtom is the client's copy of a server input atom with the user's
// value: the text unpadded, the length its byte count, the "changed by the
// user" flag on, the rest of the layout as the server sent it.
func changedAtom(a diag.Atom, value string) diag.Atom {
	a.Text = value
	a.Length = len(value)
	a.Flags[1] |= 0x01
	a.Rest = nil
	return a
}
