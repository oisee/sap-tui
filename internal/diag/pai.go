package diag

import "regexp"

// The client's answer to a screen — its PAI — carries two things a server
// needs: the values the user left in the fields, and the event that fired
// (a function code, or a control event). For a plain dynpro the values come
// back in the DYNT_ATOM as input atoms, each at the row and column the
// server placed it, so a server that remembers where it put a field reads
// the value straight back by position. Control events (an editor click, an
// ALV action) come in the client's XML DataManager instead.

// FieldValue is one value the client returned, at the cell the field sat on.
type FieldValue struct {
	Row, Col int
	Value    string
}

// ClientFields reads the values a client echoed in its DYNT_ATOM: every
// input or output atom, by the cell it sits on. A server matches these to
// the fields it sent by position.
func ClientFields(items []Item) []FieldValue {
	var out []FieldValue
	for _, it := range items {
		// The server sends the screen as an APPL4 DYNT_ATOM (a 4-byte
		// length); the client echoes the fields it changed as an APPL
		// DYNT_ATOM (a 2-byte length). Read either.
		if (it.Type != ItemAPPL4 && it.Type != ItemAPPL) || it.ID != 0x09 || it.SID != 0x02 {
			continue
		}
		atoms, _ := ParseDyntAtoms(it.Value)
		for _, a := range atoms {
			if a.EType == AtomInputField || a.EType == AtomOutputField {
				out = append(out, FieldValue{Row: a.Row, Col: a.Col, Value: a.Value()})
			}
		}
	}
	return out
}

// Event is one entry of the client's XML DataManager EVENTS block: which
// control (shell) fired which event, with its parameter. For a control-less
// dynpro there are none, and the function code rides the fields instead.
type Event struct {
	ShellID, EventID, Value string
}

var (
	xmlEvent = regexp.MustCompile(`<EVENT SHELLID ="([^"]*)" EVENTID ="([^"]*)"[^>]*>`)
	xmlParam = regexp.MustCompile(`<PARAM PID ="0" VALUE ="([^"]*)"`)
)

// Events reads the EVENTS block of the client's XML DataManager. The first
// PARAM of each event is taken as its value, which is enough to see which
// toolbar command or line the user acted on.
func Events(items []Item) []Event {
	var out []Event
	for _, it := range items {
		if it.Type != ItemXML {
			continue
		}
		body := string(it.Value)
		locs := xmlEvent.FindAllStringSubmatchIndex(body, -1)
		for i, m := range locs {
			ev := Event{ShellID: body[m[2]:m[3]], EventID: body[m[4]:m[5]]}
			end := len(body)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			if p := xmlParam.FindStringSubmatch(body[m[1]:end]); p != nil {
				ev.Value = p[1]
			}
			out = append(out, ev)
		}
	}
	return out
}

// StatusMessage builds the item that shows a message in the status bar and
// makes the GUI play the sound of its type — VARINFO.03 (APPL 0x0c/0x03).
// Its first byte is the message type (S, W, E, I), which is what the sound
// hangs off, followed by a message id and number and the text. The layout
// was read off the wire: type, a NUL, a two-character class padded with
// spaces, a NUL, the number "001", a NUL, a space, a NUL, then the text.
func StatusMessage(msgType byte, text string) Item {
	v := []byte{msgType, 0x00}
	v = append(v, '0', '0')                        // message class
	v = append(v, []byte("                  ")...) // padded to the width the wire used
	v = append(v, 0x00)
	v = append(v, '0', '0', '1') // message number
	v = append(v, 0x00, 0x20, 0x00)
	v = append(v, []byte(text)...)
	return Item{Type: ItemAPPL, ID: 0x0c, SID: 0x03, Value: v}
}

// Message types for StatusMessage. Each maps to a sound in the GUI's sound
// scheme: Success-Msg, Warning-Msg, Error-Msg.
const (
	MsgSuccess = 'S'
	MsgWarning = 'W'
	MsgError   = 'E'
	MsgInfo    = 'I'
)
