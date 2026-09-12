package diag

import (
	"fmt"
)

// Item types. The names are pysap's and SAP's; the fixed lengths of the
// short ones are inferred from protocol lore and stand until a capture
// confirms them.
const (
	ItemSES     = 0x01
	ItemICO     = 0x02
	ItemTIT     = 0x03
	ItemMessage = 0x07
	ItemOKC     = 0x08
	ItemCHL     = 0x09
	ItemSFE     = 0x0a
	ItemSBA     = 0x0b
	ItemEOM     = 0x0c
	ItemAPPL    = 0x10
	ItemXML     = 0x11
	ItemAPPL4   = 0x12
	ItemSLC     = 0x13
	ItemSBA2    = 0x15
)

// fixedLen is the value length of the item types that carry no length
// field. Inferred, except CHL: it was 3 here until the ALV capture showed
// the next item's type byte 22 bytes after the CHL byte, in both
// directions and on every CHL of the capture, which is also pysap's
// length. With 3 the rest of every frame with a CHL came out raw.
var fixedLen = map[byte]int{
	ItemSES: 16, ItemICO: 20, ItemTIT: 3, ItemMessage: 76, ItemOKC: 1, ItemCHL: 22,
	ItemSFE: 3, ItemSBA: 2, ItemEOM: 0, ItemSLC: 2, ItemSBA2: 9,
}

var typeNames = map[byte]string{
	ItemSES: "SES", ItemICO: "ICO", ItemTIT: "TIT", ItemMessage: "MSG", ItemOKC: "OKC", ItemCHL: "CHL",
	ItemSFE: "SFE", ItemSBA: "SBA", ItemEOM: "EOM", ItemAPPL: "APPL", ItemXML: "XML", ItemAPPL4: "APPL4",
	ItemSLC: "SLC", ItemSBA2: "SBA2",
}

// Item is one item of a message.
type Item struct {
	Offset int
	Type   byte
	// ID and SID are set for APPL and APPL4 items.
	ID, SID byte
	Value   []byte
	Status  Status
}

// TypeName is the item type in letters.
func (it Item) TypeName() string {
	if n, ok := typeNames[it.Type]; ok {
		return n
	}
	return fmt.Sprintf("0x%02x", it.Type)
}

// Key is what an item is, without its value: the thing to diff on.
func (it Item) Key() string {
	if it.Type == ItemAPPL || it.Type == ItemAPPL4 {
		return fmt.Sprintf("%s %s", it.TypeName(), applName(it.ID, it.SID))
	}
	return it.TypeName()
}

// ParseItems splits a decompressed body into items. It stops at the first
// item type it does not know and returns the rest as one Raw item, so a
// capture never fails to show what went by.
func ParseItems(body []byte) []Item {
	var items []Item
	i := 0
	for i < len(body) {
		t := body[i]
		switch t {
		case ItemAPPL, ItemAPPL4:
			lenBytes := 2
			if t == ItemAPPL4 {
				lenBytes = 4
			}
			if i+3+lenBytes > len(body) {
				return append(items, Item{Offset: i, Type: t, Value: body[i:], Status: Raw})
			}
			id, sid := body[i+1], body[i+2]
			n := beUint16(body[i+3:])
			if lenBytes == 4 {
				n = beUint32(body[i+3:])
			}
			start := i + 3 + lenBytes
			if start+n > len(body) {
				return append(items, Item{Offset: i, Type: t, ID: id, SID: sid, Value: body[start:], Status: Raw})
			}
			items = append(items, Item{Offset: i, Type: t, ID: id, SID: sid, Value: body[start : start+n], Status: Inferred})
			i = start + n
		case ItemXML:
			// Type, a four-byte big-endian length, the document. Confirmed on
			// the Phase 0 capture: 11 00000035 <?xml …><DATAMANAGER/>.
			if i+5 > len(body) {
				return append(items, Item{Offset: i, Type: t, Value: body[i:], Status: Raw})
			}
			n := beUint32(body[i+1:])
			if i+5+n > len(body) {
				return append(items, Item{Offset: i, Type: t, Value: body[i+5:], Status: Raw})
			}
			items = append(items, Item{Offset: i, Type: t, Value: body[i+5 : i+5+n], Status: Confirmed})
			i += 5 + n
		default:
			n, ok := fixedLen[t]
			if !ok || i+1+n > len(body) {
				return append(items, Item{Offset: i, Type: t, Value: body[i:], Status: Raw})
			}
			items = append(items, Item{Offset: i, Type: t, Value: body[i+1 : i+1+n], Status: Inferred})
			i += 1 + n
		}
	}
	return items
}
