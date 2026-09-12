// Package cfw decodes the SAP DIAG Control Framework's OLE-automation
// exchange and models the state a client must keep to ANSWER it, instead of
// replaying captured answers.
//
// Each server RFC_TR.00 (an OLE_FLUSH_CALL into program SAPLOLEA) carries
// three fixed-width ASCII streams, SAP-LZH-compressed, inside its
// XML_DATA_STREAM (recover them with Streams):
//
//   - VERBS: the script. 53-byte records, one per automation verb —
//     objId right-aligned in [0:9], verb name in [10:52], a flag byte at [52]
//     (C = call a method, S = set a property, G = get a property).
//   - SVARS descriptor: 44-byte records with columns [group, type, name,
//     pointer] — group is the 1-based verb index, name is "#N" (a positional
//     input the server fills) or "_RESULT" (an output slot the FRONTEND fills),
//     pointer the value-pool record the slot maps to.
//   - SVARS value pool: 337-byte records holding the values, output slots
//     carrying the frontend-minted object handles as "000000000O<n>".
//
// The crux (see the cfw-ole-automation-model memory / KNOWLEDGE §8): OLE
// object handles "O<n>" are minted by the frontend and returned in the
// _RESULT slots; the server stores them and feeds them back as inputs in
// later calls. Replaying a fixed answer hands back a foreign session's
// handles, so the first call whose layout diverges dereferences a handle this
// session never minted and SAPLOLEA dumps (MESSAGE X373 '-1'). The Engine here
// mints this session's own handles as it walks the verbs.
//
// This file is the DECODER, the state model, and FillResults — the core of
// emission: it substitutes this session's handles into a value pool's _RESULT
// slots in place. Assembling a whole RFC_TR.01 frame (recompressing the
// modified streams and reproducing the inter-string envelope) is the remaining
// step, then one live A4H run decides whether client-minted handles are
// accepted.
package cfw

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/oisee/sap-tui/internal/alv"
	"github.com/oisee/sap-tui/internal/sapcompress"
)

// stream record widths, pinned on captures/probe.jsonl frame S->C #6.
const (
	verbRecLen  = 53
	descRecLen  = 44
	valueRecLen = 337
	verbNameOff = 10 // verb name column start within a 53-byte record
	verbFlagOff = 52 // the C/S/G flag byte
)

// Verb is one automation instruction from the VERBS stream.
type Verb struct {
	ObjID string // the OLE object the verb runs against
	Name  string // CreateObject, CreateControl, SetProperty, GetContainer, …
	Flag  byte   // 'C' call method, 'S' set property, 'G' get property
}

// Creates reports whether the verb makes a new OLE object, and so has a
// _RESULT slot the frontend must fill with a freshly minted handle.
func (v Verb) Creates() bool {
	switch v.Name {
	case "CreateObject", "CreateControl", "CreateControl2":
		return true
	}
	return false
}

// SvarsRec is one record of the SVARS descriptor table: the columns are
// [group, type, name, pointer] — group is the 1-based index of the verb this
// slot belongs to, type 12=string/3=int, name "#N" (a server-filled input) or
// "_RESULT" (a frontend output), pointer the 1-based value-pool record index.
type SvarsRec struct {
	Fields  []string // the record's whitespace-separated columns
	Group   string   // the verb index this slot belongs to
	Name    string   // "#N" (server-filled input) or "_RESULT" (frontend output)
	Pointer string   // the value-pool record index this slot maps to
}

// IsResult reports whether this descriptor slot is a frontend output slot.
func (r SvarsRec) IsResult() bool { return r.Name == "_RESULT" }

// Streams recovers and decompresses the inner streams of an RFC_TR OLE call
// (the raw APPL 0x08 item value) and returns them by role. A call usually
// carries three (VERBS, SVARS descriptor, value pool) but some carry only one
// or two (a data-only leg, or a call with no results), so each is classified
// by content rather than by position. A missing role comes back nil.
func Streams(rfctrValue []byte) (verbs, desc, values []byte, err error) {
	raw := alv.ExtractLZHStreams(rfctrValue)
	if len(raw) == 0 {
		return nil, nil, nil, fmt.Errorf("cfw: no inner streams")
	}
	for _, r := range raw {
		dec, e := sapcompress.Decompress(r)
		if e != nil {
			continue
		}
		switch classifyStream(dec) {
		case streamVerbs:
			verbs = dec
		case streamValues:
			values = dec
		default: // descriptor
			desc = dec
		}
	}
	return verbs, desc, values, nil
}

type streamKind int

const (
	streamDesc streamKind = iota
	streamVerbs
	streamValues
)

// classifyStream identifies an inner stream by content: a verb name marks the
// VERBS script; the 9-zero value prefix marks the value pool (which also holds
// _RESULT, so check it first); otherwise it is the descriptor.
func classifyStream(b []byte) streamKind {
	s := string(b)
	if strings.Contains(s, "CreateObject") || strings.Contains(s, "CreateControl") ||
		strings.Contains(s, "SetProperty") || strings.Contains(s, "FreeObject") ||
		strings.Contains(s, "GetContainer") {
		return streamVerbs
	}
	if strings.Contains(s, "000000000") {
		return streamValues
	}
	return streamDesc
}

// ParseVerbs splits the VERBS stream into its 53-byte records.
func ParseVerbs(b []byte) []Verb {
	var out []Verb
	for off := 0; off+verbRecLen <= len(b); off += verbRecLen {
		rec := b[off : off+verbRecLen]
		name := strings.TrimSpace(string(rec[verbNameOff:verbFlagOff]))
		if name == "" {
			continue
		}
		out = append(out, Verb{
			ObjID: strings.TrimSpace(string(rec[0:verbNameOff])),
			Name:  name,
			Flag:  rec[verbFlagOff],
		})
	}
	return out
}

// ParseSvarsDesc splits the SVARS descriptor stream into its 44-byte records.
func ParseSvarsDesc(b []byte) []SvarsRec {
	var out []SvarsRec
	for off := 0; off+descRecLen <= len(b); off += descRecLen {
		fields := strings.Fields(string(b[off : off+descRecLen]))
		if len(fields) == 0 {
			continue
		}
		r := SvarsRec{Fields: fields, Group: fields[0]}
		// The name is the "#N" or "_RESULT" column, and the pointer is the
		// value-pool index that follows it.
		for i, f := range fields {
			if f == "_RESULT" || (len(f) > 1 && f[0] == '#') {
				r.Name = f
				if i+1 < len(fields) {
					r.Pointer = fields[i+1]
				}
				break
			}
		}
		out = append(out, r)
	}
	return out
}

// SvarsVal is one record of the SVARS value pool: the value carried in a slot,
// or a _RESULT output slot the frontend fills with a handle.
type SvarsVal struct {
	IsResult bool   // the slot the frontend writes a minted handle into
	Value    string // the value, with the 9-zero prefix stripped ("101", "O2", …)
}

// ParseSvarsValues splits the SVARS value pool into its 337-byte records. Each
// carries a value at offset 32 as "000000000<value>" (a handle like O2 has no
// separating space; a plain value like 101 does); a _RESULT record marks its
// name at offset 0.
func ParseSvarsValues(b []byte) []SvarsVal {
	var out []SvarsVal
	for off := 0; off+valueRecLen <= len(b); off += valueRecLen {
		rec := b[off : off+valueRecLen]
		name := strings.TrimSpace(string(rec[0:valNameCol]))
		val := strings.TrimSpace(string(rec[valNameCol:]))
		val = strings.TrimSpace(strings.TrimPrefix(val, "000000000"))
		out = append(out, SvarsVal{IsResult: name == "_RESULT", Value: val})
	}
	return out
}

// ResultSlot resolves a descriptor _RESULT pointer (1-based, as ParseSvarsDesc
// returns it) to its index in the value-pool records, or -1.
func ResultSlot(pointer string) int {
	var n int
	if _, err := fmt.Sscanf(pointer, "%d", &n); err != nil || n < 1 {
		return -1
	}
	return n - 1
}

// valNameCol is where a value-pool record's value begins; [0:valNameCol] holds
// the _RESULT marker when present.
const valNameCol = 32

// Engine holds one session's OLE automation state: the monotonic handle
// counter and the objId->handle map. It is what replaces blind answer replay.
type Engine struct {
	next    int
	Handles map[string]string // objId -> minted "O<n>"
	Minted  []string          // handles minted this session, in order
	remap   map[string]string // captured handle -> our handle, session-wide
}

// NewEngine starts a fresh session engine.
func NewEngine() *Engine {
	return &Engine{next: 1, Handles: map[string]string{}, remap: map[string]string{}}
}

// mint returns the next frontend-owned OLE handle ("O1", "O2", …) and records
// it. Whether the kernel accepts a plain counter at face value, or validates
// the numeric handle, is the open go/no-go a live A4H run must answer.
func (e *Engine) mint() string {
	h := fmt.Sprintf("O%d", e.next)
	e.next++
	e.Minted = append(e.Minted, h)
	return h
}

// FillResults writes this session's handles into the _RESULT slots of a value
// pool, in place, leaving every other byte untouched. It pairs the object-
// creating verbs (in order) with the _RESULT descriptor slots (in order): the
// nth create verb's minted handle goes into the nth _RESULT slot, at the value
// record the slot's pointer names. It returns the modified value pool and the
// handles written. This is the core of emitting an RFC_TR.01: a captured
// answer's bytes with our own handles substituted for the foreign session's.
func (e *Engine) FillResults(verbs []Verb, desc []SvarsRec, values []byte) ([]byte, []string) {
	// Map each verb group (1-based verb index) to its _RESULT value-pool slot.
	slotOf := map[string]int{}
	for _, d := range desc {
		if d.IsResult() {
			slotOf[d.Group] = ResultSlot(d.Pointer)
		}
	}
	out := append([]byte(nil), values...)
	written := []string{}
	for i, v := range verbs {
		if !v.Creates() {
			continue
		}
		h := e.mint()
		if v.ObjID != "" {
			e.Handles[v.ObjID] = h
		}
		group := strconv.Itoa(i + 1) // the descriptor group is the 1-based verb index
		slot, ok := slotOf[group]
		if !ok || slot < 0 {
			continue // creating verb with no _RESULT slot found (handle still minted)
		}
		off := slot * valueRecLen
		if off+valueRecLen > len(out) {
			continue
		}
		// Record the captured handle this slot held so later calls that feed
		// it back as an input can be remapped to our handle.
		if old := handleIn(out[off : off+valueRecLen]); old != "" {
			e.remap[old] = h
		}
		writeHandle(out[off:off+valueRecLen], h)
		written = append(written, h)
	}
	return out, written
}

// handleIn returns the "O<n>" handle a value-pool record carries, or "".
func handleIn(rec []byte) string {
	v := strings.TrimSpace(string(rec[valNameCol:]))
	v = strings.TrimSpace(strings.TrimPrefix(v, "000000000"))
	if len(v) >= 2 && v[0] == 'O' {
		ok := true
		for _, c := range v[1:] {
			if c < '0' || c > '9' {
				ok = false
				break
			}
		}
		if ok {
			return v
		}
	}
	return ""
}

// RemapInputs rewrites, in place, every value-pool record whose handle input
// refers to an object created earlier this session, replacing the captured
// session's handle with ours (from the bijection FillResults built). This is
// what keeps a whole session consistent: the server stored our handles from
// earlier answers and feeds them back, so a later answer must speak our
// handles, not the captured GUI's.
func (e *Engine) RemapInputs(values []byte) {
	for off := 0; off+valueRecLen <= len(values); off += valueRecLen {
		rec := values[off : off+valueRecLen]
		h := handleIn(rec)
		if h == "" {
			continue
		}
		if mapped, ok := e.remap[h]; ok && mapped != h {
			writeHandle(rec, mapped)
		}
	}
}

// writeHandle overwrites one value-pool record's value field with a minted
// handle: the 9-zero prefix, the handle, then spaces to the record's end. The
// name column [0:valNameCol] (the _RESULT marker) is left as it was.
func writeHandle(rec []byte, handle string) {
	v := rec[valNameCol:]
	for i := range v {
		v[i] = ' '
	}
	copy(v, "000000000"+handle)
}

// Run walks a call's verbs and mints a handle for each object-creating verb,
// binding it under the verb's objId so later calls that feed the handle back
// resolve. It returns the handles minted, in verb order. (This is the state
// half; wiring these into the _RESULT descriptor slots of an emitted RFC_TR.01
// is the next phase, once the value-pool layout is pinned.)
func (e *Engine) Run(verbs []Verb) []string {
	var minted []string
	for _, v := range verbs {
		if !v.Creates() {
			continue
		}
		h := e.mint()
		if v.ObjID != "" {
			e.Handles[v.ObjID] = h
		}
		minted = append(minted, h)
	}
	return minted
}
