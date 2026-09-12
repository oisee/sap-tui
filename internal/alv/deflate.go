package alv

// Exact-length DEFLATE assembly for recolouring an ALV blob in place.
//
// SAP's LZH is RFC-1951 DEFLATE behind a two-bit prefix, and its decompressor
// reads the stream at that two-bit offset with a continuous bit reader. Stored
// blocks are the one DEFLATE construct that must be byte-aligned, so at the
// prefix offset they desync SAP's reader and the GUI hangs (sapcompress warns of
// exactly this). Huffman blocks are alignment-free and are accepted — but an ALV
// blob is read positionally by a compressed-length prefix, so a recoloured blob
// must occupy the original's exact byte length or the sibling blobs after it move
// and the grid vanishes.
//
// deflateExactHuffman produces a valid, terminated, Huffman-only DEFLATE stream
// of an exact target length: it compresses with compress/flate, un-finalises the
// last block, and pads with empty fixed-Huffman blocks (and, for byte-accurate
// landing, a final block that carries a reserved tail of literals) until the
// stream is exactly target bytes with its final end-of-block in the last byte.

import (
	"bytes"
	"compress/flate"
)

// DEFLATE length- and distance-code extra-bit counts (RFC 1951 §3.2.5). Only the
// counts matter here: the walker tracks bit position, not decoded values.
var lenExtraBits = [29]int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
var distExtraBits = [30]int{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}

// The order HCLEN code lengths arrive in (RFC 1951 §3.2.7).
var clOrder = [19]int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

// bitReaderLSB reads DEFLATE's least-significant-bit-first stream, tracking the
// absolute bit position so the walker can report where blocks begin and end.
type bitReaderLSB struct {
	data []byte
	pos  int
}

func (r *bitReaderLSB) bit() (int, bool) {
	if r.pos>>3 >= len(r.data) {
		return 0, false
	}
	b := int(r.data[r.pos>>3]>>(uint(r.pos)&7)) & 1
	r.pos++
	return b, true
}

func (r *bitReaderLSB) bits(n int) (int, bool) {
	v := 0
	for i := 0; i < n; i++ {
		b, ok := r.bit()
		if !ok {
			return 0, false
		}
		v |= b << i
	}
	return v, true
}

// huffTable is a canonical Huffman decoder built from code lengths, decoding the
// puff.c way: read one bit at a time, comparing the running code against the
// first code of each length.
type huffTable struct {
	counts  [16]int
	symbols []int
}

func buildHuff(lengths []int) huffTable {
	var h huffTable
	for _, l := range lengths {
		if l > 0 {
			h.counts[l]++
		}
	}
	var offs [16]int
	for l := 1; l < 15; l++ {
		offs[l+1] = offs[l] + h.counts[l]
	}
	h.symbols = make([]int, len(lengths))
	for sym, l := range lengths {
		if l > 0 {
			h.symbols[offs[l]] = sym
			offs[l]++
		}
	}
	return h
}

func (h *huffTable) decode(r *bitReaderLSB) (int, bool) {
	code, first, index := 0, 0, 0
	for l := 1; l <= 15; l++ {
		b, ok := r.bit()
		if !ok {
			return 0, false
		}
		code |= b
		count := h.counts[l]
		if code-first < count {
			return h.symbols[index+(code-first)], true
		}
		index += count
		first += count
		first <<= 1
		code <<= 1
	}
	return 0, false
}

func fixedLitLengths() []int {
	l := make([]int, 288)
	for i := 0; i < 144; i++ {
		l[i] = 8
	}
	for i := 144; i < 256; i++ {
		l[i] = 9
	}
	for i := 256; i < 280; i++ {
		l[i] = 7
	}
	for i := 280; i < 288; i++ {
		l[i] = 8
	}
	return l
}

func fixedDistLengths() []int {
	l := make([]int, 30)
	for i := range l {
		l[i] = 5
	}
	return l
}

// inflateSymbols advances r past one block's symbols, stopping just after the
// end-of-block symbol (256). It decodes only enough to track position.
func inflateSymbols(r *bitReaderLSB, lit, dist *huffTable) bool {
	for {
		sym, ok := lit.decode(r)
		if !ok {
			return false
		}
		if sym == 256 {
			return true
		}
		if sym < 256 {
			continue
		}
		sym -= 257
		if sym >= len(lenExtraBits) {
			return false
		}
		if _, ok := r.bits(lenExtraBits[sym]); !ok {
			return false
		}
		dsym, ok := dist.decode(r)
		if !ok {
			return false
		}
		if dsym >= len(distExtraBits) {
			return false
		}
		if _, ok := r.bits(distExtraBits[dsym]); !ok {
			return false
		}
	}
}

// readDynamicTables reads a dynamic block's Huffman tables (RFC 1951 §3.2.7).
func readDynamicTables(r *bitReaderLSB) (lit, dist huffTable, ok bool) {
	hlit, ok1 := r.bits(5)
	hdist, ok2 := r.bits(5)
	hclen, ok3 := r.bits(4)
	if !ok1 || !ok2 || !ok3 {
		return lit, dist, false
	}
	hlit += 257
	hdist += 1
	hclen += 4
	clLengths := make([]int, 19)
	for i := 0; i < hclen; i++ {
		v, ok := r.bits(3)
		if !ok {
			return lit, dist, false
		}
		clLengths[clOrder[i]] = v
	}
	clHuff := buildHuff(clLengths)
	all := make([]int, 0, hlit+hdist)
	for len(all) < hlit+hdist {
		sym, ok := clHuff.decode(r)
		if !ok {
			return lit, dist, false
		}
		switch {
		case sym < 16:
			all = append(all, sym)
		case sym == 16:
			n, ok := r.bits(2)
			if !ok || len(all) == 0 {
				return lit, dist, false
			}
			prev := all[len(all)-1]
			for i := 0; i < n+3; i++ {
				all = append(all, prev)
			}
		case sym == 17:
			n, ok := r.bits(3)
			if !ok {
				return lit, dist, false
			}
			for i := 0; i < n+3; i++ {
				all = append(all, 0)
			}
		case sym == 18:
			n, ok := r.bits(7)
			if !ok {
				return lit, dist, false
			}
			for i := 0; i < n+11; i++ {
				all = append(all, 0)
			}
		default:
			return lit, dist, false
		}
	}
	if len(all) != hlit+hdist {
		return lit, dist, false
	}
	return buildHuff(all[:hlit]), buildHuff(all[hlit:]), true
}

// deflateEnds walks the DEFLATE stream compress/flate produced and reports how
// to reuse its Huffman-coded data as a non-final prefix. compress/flate writes
// the data as one or more Huffman blocks and terminates the stream with a final
// empty *stored* block; that terminator is dropped here, since the caller writes
// its own finalisation. prefixEnd is the bit index just past the last Huffman
// block's end-of-block symbol; clearBit is the bit index of that block's BFINAL
// flag when it was set (so the caller clears it to un-finalise the block), or -1
// when the block was already non-final. A stored block that is not the empty
// terminator means the data would not stay Huffman-only, so ok is false.
func deflateEnds(data []byte) (prefixEnd, clearBit int, ok bool) {
	r := &bitReaderLSB{data: data}
	fixedLit := buildHuff(fixedLitLengths())
	fixedDist := buildHuff(fixedDistLengths())
	prefixEnd, clearBit = 0, -1
	sawHuffman := false
	for {
		start := r.pos
		bfinal, ok1 := r.bit()
		btype, ok2 := r.bits(2)
		if !ok1 || !ok2 {
			return 0, 0, false
		}
		switch btype {
		case 0: // stored
			if r.pos&7 != 0 {
				r.pos += 8 - (r.pos & 7)
			}
			ln, okl := r.bits(16)
			_, okn := r.bits(16)
			if !okl || !okn {
				return 0, 0, false
			}
			if bfinal == 1 && ln == 0 && sawHuffman {
				return prefixEnd, clearBit, true // flate's empty final terminator
			}
			return 0, 0, false // a real stored block: cannot stay Huffman-only
		case 1:
			if !inflateSymbols(r, &fixedLit, &fixedDist) {
				return 0, 0, false
			}
		case 2:
			lit, dist, okd := readDynamicTables(r)
			if !okd || !inflateSymbols(r, &lit, &dist) {
				return 0, 0, false
			}
		default:
			return 0, 0, false
		}
		sawHuffman = true
		prefixEnd = r.pos
		if bfinal == 1 {
			return prefixEnd, start, true // last Huffman block is final: clear its flag
		}
		clearBit = -1
		if r.pos>>3 >= len(data) {
			return 0, 0, false
		}
	}
}

// bitWriterLSB writes DEFLATE's least-significant-bit-first stream.
type bitWriterLSB struct {
	buf  []byte
	cur  byte
	nbit int
}

func (w *bitWriterLSB) writeBit(b int) {
	w.cur |= byte(b&1) << uint(w.nbit)
	w.nbit++
	if w.nbit == 8 {
		w.buf = append(w.buf, w.cur)
		w.cur = 0
		w.nbit = 0
	}
}

// bitLen is the number of bits written so far.
func (w *bitWriterLSB) bitLen() int { return len(w.buf)*8 + w.nbit }

// bytesPadded flushes the partial byte (zero-filled) and returns the buffer.
func (w *bitWriterLSB) bytesPadded() []byte {
	out := append([]byte(nil), w.buf...)
	if w.nbit > 0 {
		out = append(out, w.cur)
	}
	return out
}

// writeFixedLiteral writes one byte as a fixed-Huffman literal code, most
// significant bit first (RFC 1951 §3.2.6): 0–143 as 8-bit codes 0x30–0xBF,
// 144–255 as 9-bit codes 0x190–0x1FF.
func (w *bitWriterLSB) writeFixedLiteral(b byte) {
	var code, n int
	if b <= 143 {
		code, n = 0x30+int(b), 8
	} else {
		code, n = 0x190+int(b)-144, 9
	}
	for i := n - 1; i >= 0; i-- {
		w.writeBit((code >> uint(i)) & 1)
	}
}

// writeFixedBlock writes one fixed-Huffman block: the BFINAL flag, BTYPE=01, the
// tail bytes as literals, and the 7-bit end-of-block code (value 0).
func (w *bitWriterLSB) writeFixedBlock(final int, tail []byte) {
	w.writeBit(final)
	w.writeBit(1) // BTYPE low bit
	w.writeBit(0) // BTYPE high bit → 01 = fixed Huffman
	for _, b := range tail {
		w.writeFixedLiteral(b)
	}
	for i := 0; i < 7; i++ {
		w.writeBit(0) // end-of-block symbol 256: fixed code is 7 zero bits
	}
}

func fixedBlockBits(tail []byte) int {
	n := 3 + 7
	for _, b := range tail {
		if b <= 143 {
			n += 8
		} else {
			n += 9
		}
	}
	return n
}

// deflateExactHuffman returns a valid, terminated, Huffman-only DEFLATE stream of
// exactly target bytes that inflates to data, or false if it cannot land on
// target (the caller then falls back to a non-exact splice).
func deflateExactHuffman(data []byte, target int) ([]byte, bool) {
	// Reserve a short tail to emit as literals in the final block: varying its
	// length gives byte-granular control to complement the ten-bit empty blocks,
	// so any reachable target can be hit exactly.
	maxTail := 8
	if maxTail > len(data) {
		maxTail = len(data)
	}
	for m := 0; m <= maxTail; m++ {
		head := data[:len(data)-m]
		tail := data[len(data)-m:]

		var buf bytes.Buffer
		fw, err := flate.NewWriter(&buf, CompressLevel)
		if err != nil {
			return nil, false
		}
		if _, err := fw.Write(head); err != nil {
			return nil, false
		}
		if err := fw.Close(); err != nil {
			return nil, false
		}
		s := buf.Bytes()
		prefixEnd, clearBit, ok := deflateEnds(s)
		if !ok {
			continue
		}

		finalBits := fixedBlockBits(tail)
		emptyBits := fixedBlockBits(nil) // 10
		// Total bits = prefixEnd (head, last block un-finalised) + 10*k (empty
		// blocks) + finalBits (the terminating block with the tail). Land it in
		// the last byte: (target-1)*8 < total ≤ target*8.
		for k := 0; ; k++ {
			total := prefixEnd + emptyBits*k + finalBits
			if total > target*8 {
				break
			}
			if total <= (target-1)*8 {
				continue
			}
			// Build it.
			w := &bitWriterLSB{}
			for i := 0; i < prefixEnd; i++ {
				b := int(s[i>>3]>>(uint(i)&7)) & 1
				if i == clearBit {
					b = 0 // clear BFINAL: the head's last block is no longer final
				}
				w.writeBit(b)
			}
			for j := 0; j < k; j++ {
				w.writeFixedBlock(0, nil)
			}
			w.writeFixedBlock(1, tail)
			out := w.bytesPadded()
			if len(out) == target {
				return out, true
			}
			break
		}
	}
	return nil, false
}
