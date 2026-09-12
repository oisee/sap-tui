package tui

import "strings"

// An icon on the wire is the four ASCII bytes @XX@ inside a text; the GUI
// substitutes the bitmap, which takes about two character cells, and shifts
// the rest of the text left to close the gap. This does the same on the
// terminal: each token becomes a coloured glyph and a space, and the text
// after it moves two cells left. Codes not in the table draw as a neutral
// square so the layout stays the same whatever the picture was.

type icon struct {
	glyph rune
	style Style
}

// The @XX@ codes are SAP's compact icon ids; there are ~1000 in the full SAP
// icon set and the two-character code is not self-describing. This table maps
// the ones our captures actually carry — most confirmed by the tooltip in their
// own `@XX\Qtooltip@` form — to a fitting Unicode glyph; codes not here draw as
// a neutral square (iconDefault) so the layout is preserved whatever the
// picture was. Colours are 256-palette indices.
var icons = map[string]icon{
	// status lights / LEDs
	"08": {'●', Style{Fg: 34}},  // green light
	"09": {'●', Style{Fg: 220}}, // yellow light
	"0A": {'●', Style{Fg: 196}}, // red light
	"5B": {'■', Style{Fg: 40}},  // LED green
	// confirmations
	"01": {'✔', Style{Fg: 28}},  // checked
	"0V": {'✔', Style{Fg: 28}},  // okay
	"0W": {'✖', Style{Fg: 160}}, // cancel
	// messages
	"0S": {'ℹ', Style{Fg: 24}},  // information
	"19": {'ℹ', Style{Fg: 33}},  // information message (tooltip-confirmed)
	// object actions (tooltip-confirmed on SE38)
	"0Y": {'＋', Style{Fg: 24}}, // create
	"0Z": {'✎', Style{Fg: 24}}, // change
	"10": {'▤', Style{Fg: 24}}, // display
	"15": {'▶', Style{Fg: 34}}, // execute (toolbar F8)
	// list / grid tools (tooltip-confirmed on ALV)
	"0D": {'⊕', Style{Fg: 24}},  // add sort/filter criterion (F7)
	"0E": {'⊖', Style{Fg: 24}},  // remove sort/filter criterion (F6)
	"1F": {'⧉', Style{Fg: 24}},  // multiple selection
	"4G": {'▽', Style{Fg: 24}},  // define filter values
	"9T": {'⚙', Style{Fg: 240}}, // properties of dynpro element
	// menus and navigation triangles
	"6C": {'☰', Style{Fg: 240}}, // menu
	"6A": {'☰', Style{Fg: 240}}, // menu
	"2L": {'▲', Style{Fg: 24}},
	"2M": {'▼', Style{Fg: 24}},
}

var iconDefault = icon{'▣', Style{Fg: 240}}

// iconAt reports whether s[i:] starts with an icon token and which one, plus
// the token's length in runes. Two forms occur: the bare `@XX@` (four runes),
// and the icon-with-tooltip `@XX\Qtooltip@` a pushbutton or toolbar caption
// uses — the tooltip is display-only and dropped, the visible caption follows
// the closing `@`. XX is two alphanumeric code characters.
func iconAt(s []rune, i int) (icon, int, bool) {
	if i+3 > len(s) || s[i] != '@' {
		return icon{}, 0, false
	}
	// An icon-less quickinfo `@\Qtooltip@caption`: a tooltip on a plain label
	// with no icon before it. Drop the whole `@\Q…@` (zero glyph, no cell) and
	// keep the caption after it.
	if s[i+1] == '\\' && s[i+2] == 'Q' {
		for j := i + 3; j < len(s); j++ {
			if s[j] == '@' {
				return icon{}, j - i + 1, true
			}
		}
		return icon{}, 0, false
	}
	if i+4 > len(s) || !isIconChar(s[i+1]) || !isIconChar(s[i+2]) {
		return icon{}, 0, false
	}
	code := strings.ToUpper(string(s[i+1 : i+3]))
	ic := iconDefault
	if x, ok := icons[code]; ok {
		ic = x
	}
	switch {
	case s[i+3] == '@': // bare @XX@
		return ic, 4, true
	case s[i+3] == '\\' && i+4 < len(s) && s[i+4] == 'Q': // @XX\Qtooltip@
		for j := i + 5; j < len(s); j++ {
			if s[j] == '@' {
				return ic, j - i + 1, true
			}
		}
	}
	return icon{}, 0, false
}

func isIconChar(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

// expandIcons turns a text into cells, substituting each @XX@ with its
// glyph and a space in the given base style. Text without icons is returned
// unchanged except for the styling.
func expandIcons(text string, base Style) []Cell {
	rs := []rune(text)
	out := make([]Cell, 0, len(rs))
	for i := 0; i < len(rs); {
		if ic, n, ok := iconAt(rs, i); ok {
			if ic.glyph != 0 { // a zero glyph is an icon-less quickinfo: drop it
				st := base
				st.Fg = ic.style.Fg
				out = append(out, Cell{ic.glyph, st}, Cell{' ', base})
			}
			i += n
			continue
		}
		out = append(out, Cell{rs[i], base})
		i++
	}
	return out
}

// hasIcon reports whether the text carries an icon token.
func hasIcon(text string) bool {
	rs := []rune(text)
	for i := range rs {
		if _, _, ok := iconAt(rs, i); ok {
			return true
		}
	}
	return false
}
