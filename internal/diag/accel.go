package diag

import (
	"sort"
	"strconv"
	"strings"
)

// The keyboard accelerator table travels as a run of ST_R3INFO.13 items (APPL
// id 0x06 sid 0x13, mislabelled "TRANSACTION" — see KNOWLEDGE.md §8). It binds
// a keystroke to a function NUMBER: the same number MNUENTRY carries as its
// Code and the client puts into UI_EVENT_SOURCE[5] to fire the function. One
// item is one binding; a function may have several (a primary and secondaries).
//
// The value is:
//
//	[1 slot][2 ASCII digits = function#][NUL] <keystroke tokens> [NUL NUL]
//
// The tokens are '&'-led two-byte codes (the same alphabet as ACCEL_LEGEND):
// &C Ctrl, &S Shift, &A Alt, &F a function key (its number follows as ASCII),
// &0 Enter, &L/&R/&U/&D the arrows; a bare printable byte is a literal letter
// (Ctrl+S is `&C` then `S`). Examples off one SE38 status:
//
//	01 "08" 00 &F "8 "        -> F8        -> function 8
//	01 "03" 00 &F "3 "        -> F3        -> function 3
//	02 "03" 00 &A &L          -> Alt+Left  -> function 3 (secondary)
//	01 "11" 00 &C "S "        -> Ctrl+S    -> function 11
//	01 "20" 00 &S &F "8 "     -> Shift+F8  -> function 20

// AccelBinding is one keystroke-to-function binding from the accelerator table.
type AccelBinding struct {
	Func int    // the function number (== MNUENTRY Code, == UI_EVENT_SOURCE[5])
	Mods string // modifier letters, sorted: "" plain, "C" Ctrl, "S" Shift, "CS", …
	Key  string // "F8", "Enter", "Left", or a literal letter like "S"
}

// FKey is the function-key number when the binding is a plain, unmodified
// F-key (F8 -> 8), or 0 when it is anything else.
func (b AccelBinding) FKey() int {
	if b.Mods != "" || !strings.HasPrefix(b.Key, "F") {
		return 0
	}
	n, err := strconv.Atoi(b.Key[1:])
	if err != nil {
		return 0
	}
	return n
}

// ParseAccelTable reads every ST_R3INFO.13 item in a frame's items into its
// keystroke-to-function bindings, in wire order.
func ParseAccelTable(items []Item) []AccelBinding {
	var out []AccelBinding
	for _, it := range items {
		if it.Type == ItemAPPL && it.ID == 0x06 && it.SID == 0x13 {
			if b, ok := parseAccelRow(it.Value); ok {
				out = append(out, b)
			}
		}
	}
	return out
}

// FKeyFuncs is the plain F-key bindings of a frame as F-key number -> function
// number (F8 -> 8), for a client that fires F-keys through UI_EVENT_SOURCE.
func FKeyFuncs(items []Item) map[int]int {
	m := map[int]int{}
	for _, b := range ParseAccelTable(items) {
		if fk := b.FKey(); fk != 0 {
			if _, dup := m[fk]; !dup {
				m[fk] = b.Func
			}
		}
	}
	return m
}

// AccelLabels maps a function number to a human keystroke for display, e.g.
// 8 -> "F8", 11 -> "Ctrl+S". When a function has several bindings the first
// plain F-key wins, else the first binding of any kind, so the label is the
// one a user is most likely to reach for.
func AccelLabels(items []Item) map[int]string {
	m := map[int]string{}
	fkey := map[int]bool{} // the stored label is a plain F-key
	for _, b := range ParseAccelTable(items) {
		lbl := b.Label()
		if lbl == "" {
			continue
		}
		isF := b.FKey() != 0
		if _, ok := m[b.Func]; !ok || (isF && !fkey[b.Func]) {
			m[b.Func] = lbl
			fkey[b.Func] = isF
		}
	}
	return m
}

// Label is the binding as a human keystroke, e.g. "F8", "Shift+F8", "Ctrl+S",
// "Enter"; empty when the binding named no key.
func (b AccelBinding) Label() string {
	if b.Key == "" {
		return ""
	}
	var parts []string
	for _, m := range b.Mods {
		switch m {
		case 'C':
			parts = append(parts, "Ctrl")
		case 'S':
			parts = append(parts, "Shift")
		case 'A':
			parts = append(parts, "Alt")
		}
	}
	parts = append(parts, b.Key)
	return strings.Join(parts, "+")
}

// parseAccelRow decodes one ST_R3INFO.13 value. A value too short to hold the
// slot, the two-digit function number and its NUL yields ok=false.
func parseAccelRow(v []byte) (AccelBinding, bool) {
	if len(v) < 4 || v[3] != 0 {
		return AccelBinding{}, false
	}
	fn, err := strconv.Atoi(string(v[1:3]))
	if err != nil {
		return AccelBinding{}, false
	}
	b := AccelBinding{Func: fn}
	var mods []string
	var isFKey bool
	var lit strings.Builder
	for i := 4; i < len(v); {
		c := v[i]
		switch {
		case c == '&' && i+1 < len(v):
			switch t := v[i+1]; t {
			case 'C':
				mods = append(mods, "C")
			case 'S':
				mods = append(mods, "S")
			case 'A':
				mods = append(mods, "A")
			case 'F':
				isFKey = true
			case '0':
				b.Key = "Enter"
			case 'L':
				b.Key = "Left"
			case 'R':
				b.Key = "Right"
			case 'U':
				b.Key = "Up"
			case 'D':
				b.Key = "Down"
			}
			i += 2
		case c >= ' ' && c < 0x7f:
			lit.WriteByte(c)
			i++
		default: // NUL and other separators
			i++
		}
	}
	sort.Strings(mods)
	b.Mods = strings.Join(mods, "")
	s := strings.TrimSpace(lit.String())
	switch {
	case isFKey:
		b.Key = "F" + s
	case b.Key == "" && s != "":
		b.Key = s
	}
	return b, true
}
