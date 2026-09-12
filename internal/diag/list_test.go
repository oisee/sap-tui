package diag

import "testing"

// sba, sfe, slc and text build the four items of one list run. They mirror the
// wire grouping without any captured bytes: the values are made up here.
func sba(row, col byte) Item { return Item{Type: ItemSBA, Value: []byte{row, col}} }
func sfe(b0, color, b2 byte) Item {
	return Item{Type: ItemSFE, Value: []byte{b0, color, b2}}
}
func slc(n int) Item     { return Item{Type: ItemSLC, Value: []byte{byte(n >> 8), byte(n)}} }
func text(s string) Item { return Item{Type: ItemAPPL, ID: 0x0c, SID: 0x0b, Value: []byte(s)} }

// A frame's SBA/SFE/SLC/VARINFO.0b stream parses into positioned, coloured
// segments: each run lands at the row and column its SBA gave, carries the
// colour its SFE gave, and its declared length matches its text.
func TestParseListItems(t *testing.T) {
	items := []Item{
		// An item that is not part of the list stream, to be ignored.
		{Type: ItemAPPL, ID: 0x06, SID: 0x07, Value: []byte("SE38")},
		// A heading cell at row 4, col 0, coloured COL_HEADING.
		sba(4, 0), sfe(0x0a, ColHeading, 0x00), slc(3), text("idx"),
		// A ruled ULINE run at row 5, col 0.
		sba(5, 0), sfe(sfeRuled, ColOff, 0x08), slc(4), text("----"),
		// A data key cell at row 6, col 0, coloured COL_KEY.
		sba(6, 0), sfe(0x0a, ColKey, 0x00), slc(3), text("  1"),
		// The value cell of the same row, plain body text.
		sba(6, 29), sfe(0x0a, ColOff, 0x00), slc(1), text("1"),
	}
	segs := ParseListItems(items)
	if len(segs) != 4 {
		t.Fatalf("got %d segments, want 4: %+v", len(segs), segs)
	}

	head := segs[0]
	if head.Row != 4 || head.Col != 0 {
		t.Errorf("heading at (%d,%d), want (4,0)", head.Row, head.Col)
	}
	if head.Color != ColHeading {
		t.Errorf("heading colour = %#x, want %#x", head.Color, ColHeading)
	}
	if head.Text != "idx" || head.Length != 3 || head.Status != Confirmed {
		t.Errorf("heading = %+v", head)
	}
	if head.Ruled() {
		t.Errorf("heading should not be ruled")
	}

	rule := segs[1]
	if !rule.Ruled() {
		t.Errorf("ULINE run not reported as ruled: %+v", rule)
	}

	key := segs[2]
	if key.Row != 6 || key.Col != 0 || key.Color != ColKey {
		t.Errorf("key cell = %+v, want row 6 col 0 colour %#x", key, ColKey)
	}

	val := segs[3]
	if val.Row != 6 || val.Col != 29 || val.Color != ColOff || val.Text != "1" {
		t.Errorf("value cell = %+v, want row 6 col 29 colour off text \"1\"", val)
	}
}

// A run whose SBA/SFE/SLC are missing is still returned, marked Inferred, so a
// malformed stream shows what text went by rather than dropping it.
func TestParseListItemsOrphanRun(t *testing.T) {
	segs := ParseListItems([]Item{text("stray")})
	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1", len(segs))
	}
	if segs[0].Status != Inferred {
		t.Errorf("orphan run status = %s, want inferred", segs[0].Status)
	}
	if segs[0].Text != "stray" || segs[0].Length != 5 {
		t.Errorf("orphan run = %+v, want text \"stray\" length 5", segs[0])
	}
}

// HasListSegments tells a list frame from a plain screen, and GroupListLines
// gathers a frame's segments into rows in the order the rows first appear.
func TestGroupListLines(t *testing.T) {
	items := []Item{
		sba(6, 0), sfe(0x0a, ColKey, 0x00), slc(3), text("  1"),
		sba(6, 29), sfe(0x0a, ColOff, 0x00), slc(1), text("1"),
		sba(7, 0), sfe(0x0a, ColKey, 0x00), slc(3), text("  2"),
	}
	if !HasListSegments(items) {
		t.Fatal("HasListSegments = false on a list frame")
	}
	if HasListSegments([]Item{{Type: ItemOKC, Value: []byte{0x01}}}) {
		t.Error("HasListSegments = true on a frame with no list run")
	}
	lines := GroupListLines(ParseListItems(items))
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].Row != 6 || len(lines[0].Segments) != 2 {
		t.Errorf("line 0 = %+v, want row 6 with 2 segments", lines[0])
	}
	if lines[1].Row != 7 || len(lines[1].Segments) != 1 {
		t.Errorf("line 1 = %+v, want row 7 with 1 segment", lines[1])
	}
}

func TestEncodeListItemsRoundTrip(t *testing.T) {
	segs := []ListSegment{
		ListText(0, 2, ColHeading, "Report"),
		ListText(2, 2, ColKey, "key-1"),
		ListText(2, 12, ColPositive, "+100"),
	}
	back := ParseListItems(EncodeListItems(segs))
	if len(back) != 3 {
		t.Fatalf("got %d", len(back))
	}
	if back[0].Row != 0 || back[0].Col != 2 || back[0].Color != ColHeading || back[0].Text != "Report" {
		t.Errorf("heading: %+v", back[0])
	}
	if back[2].Color != ColPositive || back[2].Col != 12 || back[2].Text != "+100" {
		t.Errorf("positive: %+v", back[2])
	}
}
