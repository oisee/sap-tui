package diag

// A classic ABAP list — the output of WRITE statements in a report — does not
// travel over DIAG as a DYNT_ATOM screen. It is painted as a stream of
// positioned text runs. Each run is a short group of items in this order:
//
//	SBA  (item 0x0b, two bytes)   the cursor position: byte 0 row, byte 1 col
//	SFE  (item 0x0a, three bytes) the display attribute: byte 1 is the SAP
//	                              list colour, byte 0 tells a text run from a
//	                              ruled ULINE run
//	SLC  (item 0x13, two bytes)   the run's length, big-endian
//	VARINFO.0b (APPL id 0x0c sid 0x0b)  the run's characters, plain text
//
// The three attribute items always precede the VARINFO.0b they describe, so a
// segment binds to the most recent SBA, SFE and SLC seen before it. This was
// read off a captured report list whose content was known in full: a header
// line, ULINE separators and twenty numbered rows. Rows and columns lined up
// one to one with the known layout, and SLC equalled the following run's byte
// count on every one of its segments, so the positions and lengths are
// confirmed. The colour reading is confirmed too (see the colour constants).
// The remaining SFE bytes distinguish a ruled line from text but their exact
// bit meaning is inferred.

// SAP list colours. The value is SFE byte 1, the same numbering an ABAP report
// gives FORMAT COLOR. Off, Heading and Key were seen in the capture — Off on
// the body text, Heading on a column header, Key on the first (idx) column of
// every data row — and are confirmed. The rest are the standard SAP list
// palette, kept here for completeness and marked inferred by absence.
const (
	ColOff      = 0x00 // normal body text, no FORMAT COLOR
	ColHeading  = 0x01 // COL_HEADING
	ColNormal   = 0x02 // COL_NORMAL, inferred
	ColTotal    = 0x03 // COL_TOTAL, inferred
	ColKey      = 0x04 // COL_KEY
	ColPositive = 0x05 // COL_POSITIVE, inferred
	ColNegative = 0x06 // COL_NEGATIVE, inferred
	ColGroup    = 0x07 // COL_GROUP, inferred
)

// An icon in a classic list is not a special item: it is the four ASCII bytes
// "@XX@" sitting inside an ordinary VARINFO.0b text run, and the GUI swaps the
// bitmap in on its own. This was read off ZODGP_ICON, whose list carried the
// same icons three ways — WRITE ... AS ICON, the raw icon constant, and a
// hand-typed '@0A@' string — and all three arrived as the identical bytes
// (40 30 41 40) with the plain text SFE (0a 00 00); AS ICON changes nothing on
// the wire. A coloured run can hold an icon and text at once, so icons place by
// (row, col) like any other run — a colour stream of little pictures.
//
// Where "@XX@" becomes a picture depends on the element, not the token. A
// dynpro *label* (KEYWORD_2) prints it literally — a probe showed the raw
// text. A dynpro *output field* (OFIELD_2) does substitute it: the real logon
// screen's welcome lines are output fields beginning with @0S@, and the GUI
// draws the info icon there. So icons reach a dynpro screen too, through an
// output field, not only through the list channel.
//
// The codes below are the ones ZODGP_ICON confirmed on the wire; the SAP icon
// set has hundreds more, each a "@XX@" of the same shape.
const (
	IconGreenLight  = "@08@" // a traffic light lit green
	IconYellowLight = "@09@" // lit yellow
	IconRedLight    = "@0A@" // lit red
	IconLEDGreen    = "@5B@" // a small green block
	IconChecked     = "@01@" // a check mark
	IconOkay        = "@0V@" // a green check
	IconCancel      = "@0W@" // a red cross
)

// Icon wraps a two-character SAP icon id in the "@XX@" the list channel
// substitutes, so Icon("0A") is the red light. The confirmed ids have named
// constants above; this is for reaching any of the rest.
func Icon(id string) string { return "@" + id + "@" }

// ListIcon is a run holding a single icon at a row and column — a picture
// placed on the list grid the way ListText places text.
func ListIcon(row, col int, icon string) ListSegment {
	return ListText(row, col, ColOff, icon)
}

// sfeRuled is SFE byte 0 on a ruled run — the horizontal line a ULINE draws,
// which arrives as a run of the character the SAP font renders as a line.
// A text run carries 0x0a there instead. Inferred.
const sfeRuled = 0x08

// ListSegment is one positioned, coloured run of text from a classic list.
type ListSegment struct {
	// Row and Col are 0-based, taken from the SBA item.
	Row, Col int
	// Length is the run length the SLC item declared; it equals len(Text) on
	// every segment of the capture.
	Length int
	// Color is the SAP list colour, SFE byte 1 (ColOff, ColKey, ...).
	Color byte
	// Attr is the three raw SFE bytes, for the display state Color does not
	// carry (a ruled run versus a text run).
	Attr [3]byte
	// Text is the run's characters, exactly as they arrived.
	Text string
	// Status says how well the segment's position and length are known: a run
	// with its own SBA is Confirmed, one that had to inherit an earlier
	// position is Inferred.
	Status Status
}

// Ruled reports whether the run is a ULINE ruled line rather than text.
// Inferred: SFE byte 0 was 0x08 on every ULINE run and 0x0a on every text run.
//
// Deprecated: SFE byte 0 is 0x08 on box-drawing runs AND on some text runs
// ("001", a message text), so byte 0 alone mislabels text as ruled. Use Box.
func (s ListSegment) Ruled() bool {
	return s.Attr[0] == sfeRuled
}

// Box reports whether the run draws the list's grid — the column separators,
// the horizontal borders, the corners and tees — rather than text. SFE byte 2
// is 0x08 on every box-drawing run and 0x00 on text, confirmed across a full
// SE16 list frame (byte 0 alone is not enough). A box run's characters are SAP
// box-drawing codes, one per cell; map them with BoxGlyph.
func (s ListSegment) Box() bool {
	return s.Attr[2] == sfeBox
}

// sfeBox is SFE byte 2 on a box-drawing run.
const sfeBox = 0x08

// BoxGlyph maps one SAP list box-drawing code (the bytes '0'..':' a Box run
// carries, one per cell) to its Unicode box character. Confirmed from a full
// SE16 grid: '0'┌ '1'└ '2'┐ '3'┘ '4'─ '5'│ '6'├ '7'┤ '8'┴ '9'┬ ':'┼ — a clean
// run over codes 0x30..0x3a. An unmapped byte returns a space.
func BoxGlyph(code byte) rune {
	const glyphs = "┌└┐┘─│├┤┴┬┼"
	i := int(code) - '0'
	rs := []rune(glyphs)
	if i < 0 || i >= len(rs) {
		return ' '
	}
	return rs[i]
}

// ListLine is the segments that share one row, in the order they arrived.
type ListLine struct {
	Row      int
	Segments []ListSegment
}

// isListText reports whether an item is a VARINFO.0b list text run.
func isListText(it Item) bool {
	return it.Type == ItemAPPL && it.ID == 0x0c && it.SID == 0x0b
}

// HasListSegments reports whether a frame carries at least one list text run,
// which is what tells a classic list apart from a plain dynpro screen.
func HasListSegments(items []Item) bool {
	for _, it := range items {
		if isListText(it) {
			return true
		}
	}
	return false
}

// ParseListItems turns the SBA/SFE/SLC/VARINFO.0b stream of a frame into
// positioned, coloured text segments, one per VARINFO.0b run. Items that are
// not part of the list stream are ignored, so it is safe to hand it a whole
// frame's items.
func ParseListItems(items []Item) []ListSegment {
	var segs []ListSegment
	var sba, sfe, slc []byte
	for _, it := range items {
		switch {
		case it.Type == ItemSBA:
			sba = it.Value
		case it.Type == ItemSFE:
			sfe = it.Value
		case it.Type == ItemSLC:
			slc = it.Value
		case isListText(it):
			seg := ListSegment{Text: string(it.Value), Status: Confirmed}
			if len(sba) == 2 {
				seg.Row = int(sba[0])
				seg.Col = int(sba[1])
			} else {
				seg.Status = Inferred
			}
			if len(sfe) == 3 {
				seg.Attr = [3]byte{sfe[0], sfe[1], sfe[2]}
				seg.Color = sfe[1]
			}
			if len(slc) == 2 {
				seg.Length = int(slc[0])<<8 | int(slc[1])
			} else {
				seg.Length = len(it.Value)
			}
			segs = append(segs, seg)
			// Each run carries its own SBA/SFE/SLC, so consume them: a later run
			// that arrives without a fresh position is then marked Inferred
			// rather than silently reusing this one's.
			sba, sfe, slc = nil, nil, nil
		}
	}
	return segs
}

// GroupListLines gathers segments into lines by row, keeping the row order in
// which the rows first appear and each line's segments in arrival order.
func GroupListLines(segs []ListSegment) []ListLine {
	var lines []ListLine
	index := map[int]int{}
	for _, s := range segs {
		i, ok := index[s.Row]
		if !ok {
			index[s.Row] = len(lines)
			lines = append(lines, ListLine{Row: s.Row})
			i = len(lines) - 1
		}
		lines[i].Segments = append(lines[i].Segments, s)
	}
	return lines
}

// EncodeListItems is the inverse of ParseListItems: it lays each segment out
// as the trio the wire wants — SBA (row, col), SFE (a text-run marker, the
// colour, a zero), SLC (the length, big-endian) — then the run's text in a
// VARINFO.0b item. A server sends these in place of a captured list's own
// stream to draw a list of its own, in colour.
func EncodeListItems(segs []ListSegment) []Item {
	var out []Item
	for _, s := range segs {
		attr := s.Attr
		if attr == ([3]byte{}) {
			attr = [3]byte{0x0a, s.Color, 0x00} // a text run in the given colour
		}
		n := len(s.Text)
		out = append(out,
			Item{Type: ItemSBA, Value: []byte{byte(s.Row), byte(s.Col)}},
			Item{Type: ItemSFE, Value: []byte{attr[0], attr[1], attr[2]}},
			Item{Type: ItemSLC, Value: []byte{byte(n >> 8), byte(n)}},
			Item{Type: ItemAPPL, ID: 0x0c, SID: 0x0b, Value: []byte(s.Text)},
		)
	}
	return out
}

// ListText is a coloured run at a row and column.
func ListText(row, col int, color byte, text string) ListSegment {
	return ListSegment{Row: row, Col: col, Color: color, Length: len(text), Text: text, Status: Confirmed}
}
