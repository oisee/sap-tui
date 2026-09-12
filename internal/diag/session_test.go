package diag

import (
	"bytes"
	"testing"
)

// sameItems compares two item lists on what the wire carries — type, id,
// sid and value — ignoring the offset and status the parser adds.
func sameItems(t *testing.T, got, want []Item) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("item count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Type != w.Type || g.ID != w.ID || g.SID != w.SID || !bytes.Equal(g.Value, w.Value) {
			t.Errorf("item %d: got %s id=%02x sid=%02x len=%d, want %s id=%02x sid=%02x len=%d",
				i, g.TypeName(), g.ID, g.SID, len(g.Value),
				w.TypeName(), w.ID, w.SID, len(w.Value))
		}
	}
}

func TestEncodeItemsRoundTrip(t *testing.T) {
	items := []Item{
		appl(0x06, sidDBName, 32),
		applVal(0x0c, sidVarProduct, []byte("SAP")),
		appl4(0x09, sidDyntAtom, 48),
		{Type: ItemSES, Value: make([]byte, 16)},
		{Type: ItemXML, Value: XMLDataManager},
		appl(0x06, sidStatusFlag, 0), // zero-length APPL value
		{Type: ItemEOM},
	}
	body := EncodeItems(items)
	sameItems(t, ParseItems(body), items)
}

func TestEncodeMessageRoundTrip(t *testing.T) {
	frame := LogonScreen()
	payload, err := EncodeMessage(frame.Header, frame.Items, false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMessage(payload, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Header.MsgInfo != 0x01 || m.Header.Compress != 0 {
		t.Errorf("header: %s", m.Header)
	}
	if m.DP != nil {
		t.Errorf("server frame carried a DP header of %d bytes", len(m.DP))
	}
	sameItems(t, ParseItems(m.Body), frame.Items)
}

func TestStatusFrameRoundTrip(t *testing.T) {
	frame := StatusFrame()
	payload, err := EncodeMessage(frame.Header, frame.Items, false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMessage(payload, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Header.MsgInfo != 0x02 {
		t.Errorf("status frame info: got %02x, want 02", m.Header.MsgInfo)
	}
	sameItems(t, ParseItems(m.Body), frame.Items)
	// The last item is the terminator; the one before it is the info=02 flag.
	last := frame.Items[len(frame.Items)-1]
	if last.Type != ItemEOM {
		t.Errorf("status frame does not end in EOM: %s", last.TypeName())
	}
}

func TestEncodeMessageCompressRefused(t *testing.T) {
	frame := LogonScreen()
	if _, err := EncodeMessage(frame.Header, frame.Items, true); err == nil {
		t.Error("compressed encoding was accepted; it is not implemented")
	}
}
