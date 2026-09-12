package diag

import "testing"

func TestParseItems(t *testing.T) {
	body := []byte{
		ItemAPPL, 0x06, 0x0c, 0x00, 0x03, 'a', 'b', 'c', // APPL 06/0c, 3 bytes
		ItemOKC, 0x01,
		ItemAPPL4, 0x12, 0x09, 0x00, 0x00, 0x00, 0x02, 'x', 'y',
		ItemEOM,
		0x7f, 0xde, 0xad, // unknown: the rest is raw
	}
	items := ParseItems(body)
	if len(items) != 5 {
		t.Fatalf("%d items: %+v", len(items), items)
	}
	if items[0].Key() != "APPL ST_R3INFO.0c" || string(items[0].Value) != "abc" {
		t.Errorf("appl: %+v", items[0])
	}
	if items[2].Key() != "APPL4 ACC_LIST.09" || string(items[2].Value) != "xy" {
		t.Errorf("appl4: %+v", items[2])
	}
	if items[3].TypeName() != "EOM" || len(items[3].Value) != 0 {
		t.Errorf("eom: %+v", items[3])
	}
	if items[4].Status != Raw || len(items[4].Value) != 3 {
		t.Errorf("raw tail: %+v", items[4])
	}
	h, _ := ParseHeader([]byte{0, 0x11, 0, 0, 0, 0, 0, 2})
	if h.Compress != 2 || h.ComFlag&FlagTermINI == 0 {
		t.Errorf("header: %+v", h)
	}
}

// TestParseItemsCHL guards the CHL length. CHL carries no length field and
// its value is 22 bytes, not 3. When it was read as 3 the parser landed
// three bytes into the CHL body, took the next 0x00 there for a new item
// type 0x00, and dumped the rest of the frame as one raw item. This body
// puts a 0x00 inside the CHL value and a known item after it, so a wrong
// length shows up as a raw tail.
func TestParseItemsCHL(t *testing.T) {
	chlBody := []byte{0x00, 0x09, 0x00, 0x00, 0x00, 0x00, 0x45, 0x00, 0x00, 0x72, 0x00, 0x00, 0xc9, 0x00, 0x00, 0x72, 0x00, 0x02, 0x01, 0xee, 0x37, 0x00}
	if len(chlBody) != 22 {
		t.Fatalf("chl body is %d bytes, the test wants 22", len(chlBody))
	}
	body := []byte{ItemCHL}
	body = append(body, chlBody...)
	body = append(body, ItemAPPL, 0x06, 0x0c, 0x00, 0x03, 'a', 'b', 'c') // APPL 06/0c after the CHL
	body = append(body, ItemEOM)

	items := ParseItems(body)
	for _, it := range items {
		if it.Status == Raw {
			t.Fatalf("raw item at offset %d, type 0x%02x: CHL length is wrong", it.Offset, it.Type)
		}
	}
	if len(items) != 3 {
		t.Fatalf("%d items, want 3: %+v", len(items), items)
	}
	if items[0].TypeName() != "CHL" || len(items[0].Value) != 22 {
		t.Errorf("chl: %+v", items[0])
	}
	if items[1].Key() != "APPL ST_R3INFO.0c" || string(items[1].Value) != "abc" {
		t.Errorf("item after chl: %+v", items[1])
	}
	if items[2].TypeName() != "EOM" {
		t.Errorf("eom: %+v", items[2])
	}
}

func TestNIControl(t *testing.T) {
	if n, ok := NIControl([]byte("NI_PONG\x00")); !ok || n != "NI_PONG" {
		t.Errorf("pong: %q %v", n, ok)
	}
	if _, ok := NIControl([]byte{0, 0, 0, 0, 0, 0, 0, 1}); ok {
		t.Error("a header taken for a ping")
	}
}
