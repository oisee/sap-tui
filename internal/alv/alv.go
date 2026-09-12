package alv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/oisee/sap-tui/internal/sapcompress"
)

// The ALV row blob (KNOWLEDGE.md §12) is a doubly-nested SAP-LZH container
// fragmented into RFC rows inside an APPL RFC_TR item:
//
//	RFC_TR value
//	  └─ outer SAP-LZH stream, split into ≤250-byte RFC rows
//	       └─ (decompressed) an OLE-automation "VARS" stream that carries
//	            └─ one or more nested SAP-LZH blobs (NOT row-chunked)
//	                 └─ (decompressed) the DataProvider R3TABLE:
//	                      • a field catalog (column names + widths)
//	                      • data packets of row-major fixed-width cells
//
// DecodeRows walks this all the way down; EncodeRows builds a byte-compatible
// container from scratch. The two are inverses at the level of decoded rows,
// which is what the round-trip test pins (the compressed bytes are free to
// differ — chunk boundaries and DEFLATE output are not canonical).

// LZH magic as it appears mid-stream: the algorithm byte 0x12 immediately
// followed by the 1F 9D signature. The 8-byte header begins 4 bytes earlier
// (the u32 length sits in front of the 0x12). Equivalently, KNOWLEDGE.md's
// "header starts 5 bytes before" the 1F 9D signature.
var lzhMagic = []byte{0x12, 0x1f, 0x9d}

// RFC-row chunk markers used to fragment the outer stream.
var (
	rowMarker = []byte{0x03, 0x05, 0x03, 0x05} // 03 05 03 05 <len u16 BE>, then <len> payload bytes
	endMarker = []byte{0x03, 0x05, 0x03, 0x06} // 03 05 03 06 00 00 ends a chunk stream
)

// rfcRowPayload is the payload size of a full RFC row; the wire uses 250
// (0x00FA). The last row of a stream may be shorter.
const rfcRowPayload = 250

// Catalog record geometry, read off a live 7.58 T100 ALV catalog. Each column
// is one fixed record; the values we need sit at fixed offsets inside it.
const (
	catalogRecordSize = 904 // one column descriptor
	catNameOffset     = 0   // 30-byte space-padded field name
	catNameLen        = 30
	catLenOffset      = 160 // internal field length, u32 LE
	catTypeOffset     = 164 // a type/flags word, u32 LE (low byte kept as Type)
)

// Data-packet framing. The wire marks a DataProvider packet with FF FF FF FF
// then a 1-byte packet kind (0x01 for the first). Our encoder writes its own
// self-describing packet behind that marker so DecodeRows can recover rows
// unambiguously; a captured packet whose framing does not match is read as
// carrying no rows (the row values page in on scroll and are absent here).
var dataMarker = []byte{0xff, 0xff, 0xff, 0xff, 0x01}

// dataHeaderLen is rowCount(u32 LE) + rowWidth(u32 LE) following dataMarker.
const dataHeaderLen = 8

// Column is one ALV column: its field name, a type/flags byte, and its
// fixed cell width in bytes.
type Column struct {
	Name string
	Type byte
	Len  int
}

// ErrNoCatalog is returned when no field catalog can be found in the blob.
var ErrNoCatalog = errors.New("alv: no R3TABLE field catalog found in blob")

// ExtractLZHStreams scans an RFC_TR value for SAP-LZH streams and returns each
// one de-chunked into a complete, self-contained compressed stream ready for
// sapcompress.Decompress: the 8-byte header followed by the concatenated RFC-row
// payloads with their 6-byte markers removed. One entry per 12 1F 9D found at a
// valid header position.
func ExtractLZHStreams(rfctrValue []byte) [][]byte {
	var streams [][]byte
	pos := 0
	for {
		j := bytes.Index(rfctrValue[pos:], lzhMagic)
		if j < 0 {
			break
		}
		hdr := pos + j - 4 // u32 length precedes the 0x12
		next := pos + j + len(lzhMagic)
		if hdr < 0 {
			pos = next
			continue
		}
		if _, err := sapcompress.ParseHeader(rfctrValue[hdr:]); err != nil {
			pos = next
			continue
		}
		streams = append(streams, dechunk(rfctrValue, hdr))
		pos = next
	}
	return streams
}

// dechunk reconstructs one compressed stream starting at the 8-byte header at
// start. It keeps the header verbatim, then walks the RFC rows: a 03 05 03 05
// marker is followed by a u16 big-endian length and exactly that many payload
// bytes (copied by length, never scanned, so marker bytes inside a payload are
// safe); a 03 05 03 06 terminator ends the stream. Any bytes before the first
// marker — the unmarked lead-in a live server leaves when the header does not
// fall on a row boundary — are copied through as-is.
func dechunk(b []byte, start int) []byte {
	out := make([]byte, 0, len(b)-start)
	if start+headerSize > len(b) {
		return out
	}
	out = append(out, b[start:start+headerSize]...)
	i := start + headerSize
	for i < len(b) {
		if i+6 <= len(b) && bytes.Equal(b[i:i+4], rowMarker) {
			n := int(binary.BigEndian.Uint16(b[i+4 : i+6]))
			i += 6
			if i+n > len(b) {
				n = len(b) - i
			}
			out = append(out, b[i:i+n]...)
			i += n
			continue
		}
		if i+4 <= len(b) && bytes.Equal(b[i:i+4], endMarker) {
			break
		}
		out = append(out, b[i])
		i++
	}
	return out
}

// chunkRFC is dechunk's inverse: it fragments a complete compressed stream into
// RFC rows. The 8-byte header is emitted verbatim, then the body is split into
// rfcRowPayload-byte pieces, each introduced by a 03 05 03 05 <len BE> marker
// (including the first, so dechunk has no unmarked lead-in to scan), and the
// stream is closed with 03 05 03 06 00 00.
func chunkRFC(stream []byte) []byte {
	out := make([]byte, 0, len(stream)+len(stream)/rfcRowPayload*6+16)
	if len(stream) < headerSize {
		return append(out, stream...)
	}
	out = append(out, stream[:headerSize]...)
	body := stream[headerSize:]
	for off := 0; off < len(body); off += rfcRowPayload {
		end := off + rfcRowPayload
		if end > len(body) {
			end = len(body)
		}
		piece := body[off:end]
		out = append(out, rowMarker...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(piece)))
		out = append(out, piece...)
	}
	out = append(out, endMarker...)
	out = append(out, 0x00, 0x00)
	return out
}

// allBlobs returns every decompressed payload in an RFC_TR value: each outer
// stream, plus every nested SAP-LZH blob found inside a decompressed outer
// stream (one level of nesting, as the wire uses).
// allBlobs collects every decompressed buffer reachable from the RFC_TR value.
// The ALV data nests up to three levels deep, and each level is RFC-row-chunked
// (03 05 03 05 markers), so a plain Decompress at the 12 1f 9d offset only ever
// reaches the catalog. Descending with ExtractLZHStreams — which strips the row
// markers — at every level surfaces the row/cell data packet as well.
func allBlobs(rfctrValue []byte) [][]byte {
	var blobs [][]byte
	seen := 0
	var descend func(data []byte, depth int)
	descend = func(data []byte, depth int) {
		blobs = append(blobs, data)
		if depth <= 0 || seen > 300 {
			return
		}
		for _, s := range ExtractLZHStreams(data) {
			seen++
			if dec, err := sapcompress.Decompress(s); err == nil && len(dec) > 0 {
				descend(dec, depth-1)
			}
		}
	}
	for _, stream := range ExtractLZHStreams(rfctrValue) {
		if dec, err := sapcompress.Decompress(stream); err == nil {
			descend(dec, 3)
		}
	}
	return blobs
}

// DecodeRows decodes the ALV row blob in an RFC_TR value into its column
// catalog and rows. It descends both compression levels, reads the field
// catalog (column names and widths), and slices any data packet whose framing
// matches into row-major fixed-width cells.
//
// Note on live captures: a SAP.DataPOnDemand provider ships the catalog (and
// index/control tables) up front and pages the actual row values in on scroll.
// A frame that carries only the catalog therefore decodes to the correct
// columns with zero rows — which is faithful, not a failure.
func DecodeRows(rfctrValue []byte) (cols []Column, rows [][]string, err error) {
	blobs := allBlobs(rfctrValue)

	// Catalog: the blob that parses into the most valid column records.
	var best []Column
	for _, b := range blobs {
		if c := parseCatalog(b); len(c) > len(best) {
			best = c
		}
	}
	if len(best) == 0 {
		return nil, nil, ErrNoCatalog
	}
	cols = best

	rowWidth := 0
	for _, c := range cols {
		rowWidth += c.Len
	}

	// Rows: the first data packet whose declared row width matches the catalog.
	for _, b := range blobs {
		if r, ok := parseDataPacket(b, cols, rowWidth); ok {
			rows = r
			break
		}
	}
	return cols, rows, nil
}

// Cell-data packet geometry (cl_salv_table / SAP.DataPOnDemand on 7.58), read
// off a coloured ALV. The packet is a run of per-row records; each record is a
// 12-byte header then one 448-byte slot per cell.
const (
	cellSlot    = 448 // one cell slot; a row is 6 of them (stride 2688)
	cellSlotHdr = 12  // colidx u32 LE | rowidx u32 LE | colourField u32 LE, then the value

)

var cellRowMarker = []byte{0xff, 0xff, 0xff, 0xff}

// Grid is a decoded ALV grid: its catalog, its cell text rows, and a parallel
// grid of per-cell colour fields (0 = uncoloured; see SapColour).
type Grid struct {
	Cols    []Column
	Rows    [][]string
	Colours [][]int
}

// DecodeGrid decodes an RFC_TR value all the way to cell text AND per-cell
// colour — the coloured-ALV path that DecodeRows' catalog-only view misses.
func DecodeGrid(rfctrValue []byte) (Grid, error) {
	blobs := allBlobs(rfctrValue)
	var best []Column
	for _, b := range blobs {
		if c := parseCatalog(b); len(c) > len(best) {
			best = c
		}
	}
	if len(best) == 0 {
		return Grid{}, ErrNoCatalog
	}
	g := Grid{Cols: best}
	for _, b := range blobs {
		if rows, cols, ok := decodeCellPacket(b, best); ok {
			g.Rows, g.Colours = rows, cols
			break
		}
	}
	return g, nil
}

// decodeCellPacket reads the per-row cell records from a blob. Each cell slot
// carries its column index, its row index, a colour field, and the raw ASCII
// value; cells are keyed back to the catalog by column index (1-based).
func decodeCellPacket(b []byte, cols []Column) (rows [][]string, colours [][]int, ok bool) {
	ncol := len(cols)
	if ncol == 0 {
		return nil, nil, false
	}
	// Row records start at FF FF FF FF followed by a small rownum and 4 zero
	// bytes; slot bodies never contain that marker.
	type start struct {
		off, rownum int
	}
	var starts []start
	for pos := 0; ; {
		j := bytes.Index(b[pos:], cellRowMarker)
		if j < 0 {
			break
		}
		q := pos + j
		pos = q + 4
		if q+12 > len(b) {
			continue
		}
		if binary.LittleEndian.Uint32(b[q+8:q+12]) != 0 {
			continue
		}
		if rn := binary.LittleEndian.Uint32(b[q+4 : q+8]); rn != 0 && rn <= 1<<20 {
			starts = append(starts, start{q, int(rn)})
		}
	}
	if len(starts) == 0 {
		return nil, nil, false
	}
	for _, st := range starts {
		vals := map[int]string{}
		clr := map[int]int{}
		// A row is a marker slot then one 448-byte slot per cell; each data
		// slot carries colidx | rowidx | colourField | value. Walk slots until
		// the next row's marker (or a small cap past the column count).
		for k := 0; k <= ncol+1; k++ {
			slot := st.off + k*cellSlot
			if slot+cellSlotHdr > len(b) {
				break
			}
			if bytes.Equal(b[slot:slot+4], cellRowMarker) {
				if k == 0 {
					continue // this row's own marker slot
				}
				break // the next row's marker
			}
			colidx := int(binary.LittleEndian.Uint32(b[slot : slot+4]))
			if colidx < 1 || colidx > ncol {
				continue
			}
			colourField := int(binary.LittleEndian.Uint32(b[slot+8 : slot+12]))
			end := slot + cellSlot
			if end > len(b) {
				end = len(b)
			}
			vals[colidx] = trimCell(b[slot+cellSlotHdr : end])
			clr[colidx] = colourField
		}
		if len(vals) == 0 {
			continue
		}
		row := make([]string, ncol)
		crow := make([]int, ncol)
		for k := 0; k < ncol; k++ {
			if v, ok := vals[k+1]; ok {
				row[k] = v
				crow[k] = clr[k+1]
			}
		}
		// The first column (the row index) sits in the marker slot; fill it.
		if ncol > 0 && row[0] == "" {
			row[0] = fmt.Sprintf("%d", st.rownum)
		}
		rows = append(rows, row)
		colours = append(colours, crow)
	}
	if len(rows) == 0 {
		return nil, nil, false
	}
	return rows, colours, true
}

// SapColour derives the SAP colour (0 none, 1..7) and the intensified/inverse
// flags from a cell's colour field: colourField = 1 + (colour | int<<3 | inv<<4).
func SapColour(colourField int) (colour int, intensified, inverse bool) {
	if colourField <= 0 {
		return 0, false, false
	}
	raw := colourField - 1
	return raw & 7, raw&8 != 0, raw&16 != 0
}

// ColourField is the inverse of SapColour: the wire value for a chosen colour.
func ColourField(colour int, intensified, inverse bool) int {
	if colour == 0 {
		return 0
	}
	raw := colour & 7
	if intensified {
		raw |= 8
	}
	if inverse {
		raw |= 16
	}
	return raw + 1
}

// parseCatalog reads a field catalog from a blob: a whole number of
// catalogRecordSize records, each carrying a valid field name and a plausible
// width. It returns nil unless every record in the blob validates, which keeps
// non-catalog blobs (index tables, control metadata) from matching.
func parseCatalog(b []byte) []Column {
	if len(b) == 0 || len(b)%catalogRecordSize != 0 {
		return nil
	}
	n := len(b) / catalogRecordSize
	cols := make([]Column, 0, n)
	for r := 0; r < n; r++ {
		rec := b[r*catalogRecordSize:]
		name := trimName(rec[catNameOffset : catNameOffset+catNameLen])
		if !validFieldName(name) {
			return nil
		}
		width := int(binary.LittleEndian.Uint32(rec[catLenOffset : catLenOffset+4]))
		if width <= 0 || width > 0x7fff {
			return nil
		}
		typ := rec[catTypeOffset] // low byte of the type/flags word
		cols = append(cols, Column{Name: name, Type: typ, Len: width})
	}
	return cols
}

// parseDataPacket finds dataMarker in a blob and, if the row width recorded
// behind it matches the catalog, slices the payload into rows of fixed-width
// cells. A packet whose framing does not match (e.g. a captured catalog-record
// separator that shares the FF FF FF FF 01 marker) yields ok=false.
func parseDataPacket(b []byte, cols []Column, rowWidth int) ([][]string, bool) {
	idx := bytes.Index(b, dataMarker)
	if idx < 0 {
		return nil, false
	}
	p := idx + len(dataMarker)
	if p+dataHeaderLen > len(b) {
		return nil, false
	}
	rowCount := int(binary.LittleEndian.Uint32(b[p : p+4]))
	declWidth := int(binary.LittleEndian.Uint32(b[p+4 : p+8]))
	if declWidth != rowWidth || rowWidth == 0 {
		return nil, false
	}
	p += dataHeaderLen
	if rowCount < 0 || p+rowCount*rowWidth > len(b) {
		return nil, false
	}
	var rows [][]string
	for r := 0; r < rowCount; r++ {
		row := b[p+r*rowWidth : p+(r+1)*rowWidth]
		cells := make([]string, len(cols))
		off := 0
		for i, c := range cols {
			cells[i] = trimCell(row[off : off+c.Len])
			off += c.Len
		}
		rows = append(rows, cells)
	}
	return rows, true
}

// EncodeRows builds an RFC_TR-compatible ALV row blob from a catalog and rows.
// It serializes the field catalog and one data packet, compresses each as a
// nested SAP-LZH blob, concatenates them into an outer stream, compresses that,
// and fragments the result into RFC rows — the exact shape DecodeRows expects.
func EncodeRows(cols []Column, rows [][]string) ([]byte, error) {
	catalog := buildCatalog(cols)
	data, err := buildDataPacket(cols, rows)
	if err != nil {
		return nil, err
	}

	innerCat, err := Compress(catalog)
	if err != nil {
		return nil, err
	}
	innerData, err := Compress(data)
	if err != nil {
		return nil, err
	}

	// The outer "VARS" stream carries the nested blobs contiguously.
	outerPayload := make([]byte, 0, len(innerCat)+len(innerData))
	outerPayload = append(outerPayload, innerCat...)
	outerPayload = append(outerPayload, innerData...)

	outerStream, err := Compress(outerPayload)
	if err != nil {
		return nil, err
	}
	return chunkRFC(outerStream), nil
}

// buildCellPacket serialises rows (and per-cell colours) into the wire cell
// packet: per row a marker slot then one 448-byte slot per column, each
// carrying colidx | rowidx | colourField | space-padded value.
func buildCellPacket(cols []Column, rows [][]string, colours [][]int) []byte {
	ncol := len(cols)
	spaceFill := func(s []byte, from int) {
		for i := from; i < len(s); i++ {
			s[i] = ' '
		}
	}
	buf := make([]byte, 0, len(rows)*(ncol+1)*cellSlot)
	for r, row := range rows {
		rownum := r + 1
		marker := make([]byte, cellSlot)
		copy(marker, cellRowMarker)
		binary.LittleEndian.PutUint32(marker[4:8], uint32(rownum)) // marker[8:12] stays zero
		spaceFill(marker, 12)
		buf = append(buf, marker...)
		for c := 0; c < ncol; c++ {
			slot := make([]byte, cellSlot)
			binary.LittleEndian.PutUint32(slot[0:4], uint32(c+1))
			binary.LittleEndian.PutUint32(slot[4:8], uint32(rownum))
			cf := 0
			if r < len(colours) && c < len(colours[r]) {
				cf = colours[r][c]
			}
			binary.LittleEndian.PutUint32(slot[8:12], uint32(cf))
			spaceFill(slot, cellSlotHdr)
			if c < len(row) {
				copy(slot[cellSlotHdr:], row[c])
			}
			buf = append(buf, slot...)
		}
	}
	return buf
}

// EncodeGrid is the inverse of DecodeGrid: it builds an RFC_TR value carrying
// the catalog and the coloured cell data. colours may be nil (uncoloured) or a
// grid parallel to rows of colour fields (see ColourField). The container is
// the same simplified nesting EncodeRows uses — a round-trip through DecodeGrid
// is exact; a live GUI needs the full automation wrapper (a later phase).
func EncodeGrid(cols []Column, rows [][]string, colours [][]int) ([]byte, error) {
	catalog := buildCatalog(cols)
	data := buildCellPacket(cols, rows, colours)
	innerCat, err := Compress(catalog)
	if err != nil {
		return nil, err
	}
	innerData, err := Compress(data)
	if err != nil {
		return nil, err
	}
	outerPayload := append(append([]byte{}, innerCat...), innerData...)
	outerStream, err := Compress(outerPayload)
	if err != nil {
		return nil, err
	}
	return chunkRFC(outerStream), nil
}

// --- recolour a captured grid in place (template + swap) ---

// lzhCompLen is the width of the compressed-stream length field the OLE-
// automation framing keeps immediately in front of every LZH header. Read off
// the coloured-ALV RFC_TR (conn 1 frame #14 of captures/alvcolor.jsonl): each
// compressed stream is introduced by an 8-byte preamble `<4-byte tag><len BE32>`
// — tag 7B 02 67 EA at the outer (RFC-row) level, 7B 02 F4 EA one level down —
// whose BE32 is the byte length of the compressed LZH stream (header + DEFLATE
// body). The RFC-row chunking pads the stream's last 250-byte row past that
// length with zeroes; a decoder reads exactly len bytes as the compressed
// stream and ignores the padding. The length therefore MUST be rewritten when a
// stream is re-Compressed to a new byte length. It sits lzhCompLen bytes before
// the header (i.e. at hdr-4); the chunk framing itself (03 05 markers, the
// 03 05 03 06 00 00 terminator) is self-delimiting and carries no other length.
const lzhCompLen = 4

// PatchColours recolours a real captured RFC_TR value (APPL item id 0x08)
// carrying a coloured ALV grid: it rewrites each cell's colourField to
// fn(row, col) (row and col 0-based; see ColourField for the wire encoding) and
// returns a byte-valid RFC_TR value with everything else — the automation
// framing and VERBS, the field catalog, and every cell's text — preserved.
//
// The cell packet lives three SAP-LZH layers down (RFC-row-chunked outer stream
// → OLE-automation VARS payload → a nested chunked DataProvider stream). Only
// the colourField u32s change, so every decompressed length is preserved at
// every layer and only the re-Compressed byte lengths move; PatchColours splices
// the one stream on the cell-packet path back in and updates that stream's
// compressed-length prefix (lzhCompLen) at each layer. Everything outside the
// spliced stream and its length prefix is left byte-for-byte identical, which is
// what keeps a verbatim-replay template renderable.
func PatchColours(rfctrValue []byte, fn func(row, col int) int) ([]byte, error) {
	if fn == nil {
		return nil, errors.New("alv: PatchColours needs a colour function")
	}
	// The catalog tells us which decompressed buffer is the cell packet and how
	// many columns each row carries; DecodeGrid finds it exactly as we must.
	g, err := DecodeGrid(rfctrValue)
	if err != nil {
		return nil, err
	}
	if len(g.Rows) == 0 {
		return nil, errors.New("alv: RFC_TR value carries no cell packet to recolour")
	}
	out, ok := patchCellPath(rfctrValue, g.Cols, fn, 4)
	if !ok {
		return nil, errors.New("alv: could not locate the cell-packet stream to recolour")
	}
	return out, nil
}

// RecolourGrid is a convenience over PatchColours that takes a grid of colour
// fields parallel to the decoded rows (colours[row][col]); cells past the edge
// of the grid are left uncoloured (0).
func RecolourGrid(rfctrValue []byte, colours [][]int) ([]byte, error) {
	return PatchColours(rfctrValue, func(row, col int) int {
		if row < len(colours) && col < len(colours[row]) {
			return colours[row][col]
		}
		return 0
	})
}

// patchCellPath finds the one chunked SAP-LZH stream in container that leads to
// the cell packet, recolours it, re-Compresses and re-chunks it, and splices it
// back — updating the compressed-length prefix in front of the header. It walks
// LZH headers in wire order and descends the same way DecodeGrid's allBlobs
// does, so it patches the very buffer DecodeGrid reads. It returns the rebuilt
// container and whether it patched anything.
func patchCellPath(container []byte, cols []Column, fn func(row, col int) int, depth int) ([]byte, bool) {
	if depth <= 0 {
		return container, false
	}
	pos := 0
	for {
		j := bytes.Index(container[pos:], lzhMagic)
		if j < 0 {
			break
		}
		hdr := pos + j - 4
		pos = pos + j + len(lzhMagic)
		if hdr < 0 {
			continue
		}
		if _, err := sapcompress.ParseHeader(container[hdr:]); err != nil {
			continue
		}
		end := chunkedStreamEnd(container, hdr)
		dec, err := sapcompress.Decompress(dechunk(container, hdr))
		if err != nil {
			continue
		}

		var newDec []byte
		patched := false
		if _, _, isCell := decodeCellPacket(dec, cols); isCell {
			buf := append([]byte(nil), dec...)
			if recolourCellPacket(buf, cols, fn) > 0 {
				newDec, patched = buf, true
			}
		} else {
			newDec, patched = patchCellPath(dec, cols, fn, depth-1)
		}
		if !patched {
			continue
		}

		orig := dechunk(container, hdr)
		// The GUI reads this blob positionally: a compressed-length prefix, then a
		// chunked SAP-LZH stream, then the next sibling blob at a fixed offset.
		// Shorten the stream and the prefix and the sibling offsets no longer agree,
		// and the grid vanishes ("ALV не видно"); pad it with raw zeros to hold the
		// offsets and the DEFLATE stream is left unterminated and the GUI hangs.
		//
		// So recompress to *exactly* the original compressed length with a valid,
		// terminated DEFLATE stream (CompressExact pads with empty stored blocks and
		// a final block), then re-chunk it into the original's framing byte-for-byte
		// with only the payload swapped. Every length prefix, chunk marker and later
		// blob offset stays put, and the stream is well-formed — the GUI cannot tell
		// it apart from the original.
		if recomp, ok := CompressExact(newDec, len(orig)); ok {
			if newChunked, ok := rechunkAs(container, hdr, end, recomp); ok {
				out := make([]byte, 0, len(container))
				out = append(out, container[:hdr]...)
				out = append(out, newChunked...)
				out = append(out, container[end:]...)
				return out, true // length prefix unchanged — nothing structural moved
			}
		}
		// Fallback: exact-length encoding or reframing did not line up. Recompress
		// freely, rewrite the length prefix, and pad the chunk stream to the span so
		// at least the total value length is preserved.
		recomp, err := Compress(newDec)
		if err != nil {
			return container, false
		}
		newChunked := padChunkedTo(chunkRFC(recomp), end-hdr)
		out := make([]byte, 0, len(container)-(end-hdr)+len(newChunked))
		out = append(out, container[:hdr]...)
		out = append(out, newChunked...)
		out = append(out, container[end:]...)
		if hdr >= lzhCompLen {
			binary.BigEndian.PutUint32(out[hdr-lzhCompLen:hdr], uint32(len(recomp)))
		}
		return out, true
	}
	return container, false
}

// rechunkAs rebuilds the chunked stream spanning container[hdr:end] using stream
// as the new dechunked payload while preserving the original chunk framing
// byte-for-byte: the same 8-byte header slot, every 03 05 marker and its length,
// any lenient single-byte gaps, and the terminator. It reads stream in exactly
// the order dechunk wrote it, so when len(stream) == len(dechunk(container[hdr:]))
// the framing lengths still describe it and the rebuilt region is the same size
// as the original. Returns nil,false if the lengths do not line up.
func rechunkAs(container []byte, hdr, end int, stream []byte) ([]byte, bool) {
	if hdr+headerSize > len(container) || len(stream) < headerSize || end > len(container) {
		return nil, false
	}
	out := make([]byte, 0, end-hdr)
	out = append(out, stream[:headerSize]...) // header slot
	sp := headerSize                          // read cursor into stream
	i := hdr + headerSize
	for i < end {
		if i+6 <= len(container) && bytes.Equal(container[i:i+4], rowMarker) {
			n := int(binary.BigEndian.Uint16(container[i+4 : i+6]))
			if sp+n > len(stream) {
				return nil, false
			}
			out = append(out, rowMarker...)
			out = append(out, container[i+4], container[i+5])
			out = append(out, stream[sp:sp+n]...)
			sp += n
			i += 6 + n
			continue
		}
		if i+4 <= len(container) && bytes.Equal(container[i:i+4], endMarker) {
			out = append(out, container[i:end]...) // terminator and any trailer
			i = end
			break
		}
		// A lenient single byte, mirrored from dechunk so the cursor stays in step.
		if sp >= len(stream) {
			return nil, false
		}
		out = append(out, stream[sp])
		sp++
		i++
	}
	if sp != len(stream) || len(out) != end-hdr {
		return nil, false
	}
	return out, true
}

// padChunkedTo grows a chunked stream to exactly target bytes by inserting
// zero-payload RFC-row chunks (≤ rfcRowPayload each) before its terminator, so
// the stream keeps its 03 05 markers and terminator but occupies the original
// span. Returns the input unchanged if it cannot land on target exactly.
func padChunkedTo(chunked []byte, target int) []byte {
	if len(chunked) < 6 || len(chunked) >= target {
		return chunked
	}
	body := chunked[:len(chunked)-6] // everything before the 6-byte terminator
	term := chunked[len(chunked)-6:]
	need := target - len(chunked)
	out := append([]byte(nil), body...)
	for need >= 6 {
		p := need - 6
		if p > rfcRowPayload {
			p = rfcRowPayload
		}
		if rem := need - (6 + p); rem > 0 && rem < 6 {
			p -= 6 - rem // keep the remainder ≥ 6 so the next chunk fits
		}
		if p < 0 {
			break
		}
		out = append(out, rowMarker...)
		out = binary.BigEndian.AppendUint16(out, uint16(p))
		out = append(out, make([]byte, p)...)
		need -= 6 + p
	}
	if need != 0 {
		return chunked // could not pad to an exact fit; leave it shorter
	}
	return append(out, term...)
}

// chunkedStreamEnd returns the offset just past a chunked stream's terminator
// (03 05 03 06 00 00), walking the RFC rows from the 8-byte header at start the
// same way dechunk reads them: a 03 05 03 05 marker plus its BE16 length is
// skipped by length (marker bytes inside a payload are safe), the terminator
// ends the stream. It is dechunk's span, used to splice a re-chunked stream in.
func chunkedStreamEnd(b []byte, start int) int {
	if start+headerSize > len(b) {
		return len(b)
	}
	i := start + headerSize
	for i < len(b) {
		if i+6 <= len(b) && bytes.Equal(b[i:i+4], rowMarker) {
			n := int(binary.BigEndian.Uint16(b[i+4 : i+6]))
			i += 6
			if i+n > len(b) {
				n = len(b) - i
			}
			i += n
			continue
		}
		if i+4 <= len(b) && bytes.Equal(b[i:i+4], endMarker) {
			return i + 6
		}
		i++
	}
	return len(b)
}

// recolourCellPacket overwrites each data slot's colourField (its 3rd u32) with
// fn(row, col), traversing the packet exactly as decodeCellPacket reads it so
// the two agree cell-for-cell: rows are numbered by the order of their
// FF FF FF FF row-start markers, columns by colidx (1-based → col = colidx-1).
// Cell text and every other byte are left untouched, so only colours change and
// the decompressed length is preserved. Returns the number of slots recoloured.
func recolourCellPacket(b []byte, cols []Column, fn func(row, col int) int) int {
	ncol := len(cols)
	if ncol == 0 {
		return 0
	}
	type start struct{ off, rownum int }
	var starts []start
	for pos := 0; ; {
		j := bytes.Index(b[pos:], cellRowMarker)
		if j < 0 {
			break
		}
		q := pos + j
		pos = q + 4
		if q+12 > len(b) {
			continue
		}
		if binary.LittleEndian.Uint32(b[q+8:q+12]) != 0 {
			continue
		}
		if rn := binary.LittleEndian.Uint32(b[q+4 : q+8]); rn != 0 && rn <= 1<<20 {
			starts = append(starts, start{q, int(rn)})
		}
	}
	patched := 0
	for r, st := range starts {
		for k := 0; k <= ncol+1; k++ {
			slot := st.off + k*cellSlot
			if slot+cellSlotHdr > len(b) {
				break
			}
			if bytes.Equal(b[slot:slot+4], cellRowMarker) {
				if k == 0 {
					continue // this row's own marker slot
				}
				break // the next row's marker
			}
			colidx := int(binary.LittleEndian.Uint32(b[slot : slot+4]))
			if colidx < 1 || colidx > ncol {
				continue
			}
			binary.LittleEndian.PutUint32(b[slot+8:slot+12], uint32(fn(r, colidx-1)))
			patched++
		}
	}
	return patched
}

// buildCatalog serializes columns into fixed catalogRecordSize records that
// parseCatalog reads back. The field name is written at the three offsets a
// live catalog repeats it at; the width and type sit where parseCatalog looks.
func buildCatalog(cols []Column) []byte {
	buf := make([]byte, len(cols)*catalogRecordSize)
	for r, c := range cols {
		rec := buf[r*catalogRecordSize : (r+1)*catalogRecordSize]
		for i := range rec {
			rec[i] = 0x20 // space-pad, as the wire does
		}
		writeName(rec[0:catNameLen], c.Name)     // primary name
		writeName(rec[40:40+catNameLen], c.Name) // repeated (matches wire)
		writeName(rec[176:176+catNameLen], c.Name)
		// The two descriptor words are binary, so clear their span first.
		for i := catLenOffset; i < catLenOffset+8; i++ {
			rec[i] = 0
		}
		binary.LittleEndian.PutUint32(rec[catLenOffset:], uint32(c.Len))
		binary.LittleEndian.PutUint32(rec[catTypeOffset:], uint32(c.Type))
	}
	return buf
}

// buildDataPacket serializes rows into a self-describing FF FF FF FF 01 packet:
// rowCount, rowWidth, then row-major fixed-width space-padded cells.
func buildDataPacket(cols []Column, rows [][]string) ([]byte, error) {
	rowWidth := 0
	for _, c := range cols {
		rowWidth += c.Len
	}
	buf := make([]byte, 0, len(dataMarker)+dataHeaderLen+len(rows)*rowWidth)
	buf = append(buf, dataMarker...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(rows)))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(rowWidth))
	for _, row := range rows {
		if len(row) != len(cols) {
			return nil, fmt.Errorf("alv: row has %d cells, catalog has %d columns", len(row), len(cols))
		}
		for i, c := range cols {
			cell := []byte(row[i])
			if len(cell) > c.Len {
				return nil, fmt.Errorf("alv: value %q does not fit column %s(%d)", row[i], c.Name, c.Len)
			}
			padded := make([]byte, c.Len)
			copy(padded, cell)
			for j := len(cell); j < c.Len; j++ {
				padded[j] = 0x20
			}
			buf = append(buf, padded...)
		}
	}
	return buf, nil
}

// --- small helpers ---

func trimName(b []byte) string {
	return string(bytes.TrimRight(b, " \x00"))
}

// trimCell strips the trailing padding of a fixed-width cell — spaces, NULs and
// any other control bytes the slot is filled with.
func trimCell(b []byte) string {
	return string(bytes.TrimRightFunc(b, func(r rune) bool { return r <= ' ' }))
}

func writeName(dst []byte, name string) {
	for i := range dst {
		dst[i] = 0x20
	}
	copy(dst, name)
}

// validFieldName holds an ABAP-ish column name: non-empty, opening with a
// letter, and otherwise letters, digits, '_' or '/'.
func validFieldName(s string) bool {
	if s == "" || len(s) > catNameLen {
		return false
	}
	first := s[0]
	if !((first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')) {
		return false // must open with a letter
	}
	for i, c := range []byte(s) {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case c == '_' || c == '/':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
