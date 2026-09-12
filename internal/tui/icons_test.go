package tui

import (
	"strings"
	"testing"

	"github.com/oisee/sap-tui/internal/diag"
)

// cellsToString drops the styling and returns the glyphs, for asserting layout.
func cellsToString(cs []Cell) string {
	r := make([]rune, len(cs))
	for i, c := range cs {
		r[i] = c.Ch
	}
	return string(r)
}

func TestExpandIconsBare(t *testing.T) {
	// A bare @XX@ becomes a glyph and a space; surrounding text is kept.
	got := cellsToString(expandIcons("a@0Y@b", Style{}))
	if got != "a＋ b" {
		t.Errorf("bare icon = %q, want %q", got, "a＋ b")
	}
}

func TestExpandIconsTooltip(t *testing.T) {
	// A pushbutton caption @XX\Qtooltip@caption: the tooltip is dropped, the
	// icon drawn, the visible caption kept. This is the SE38 "Display" button.
	got := cellsToString(expandIcons("@10\\QDisplay@ Display", Style{}))
	if got != "▤  Display" {
		t.Errorf("tooltip icon = %q, want %q", got, "▤  Display")
	}
	// Create from the same screen (the caption keeps its own leading space, so
	// the glyph's trailing space and it read as two).
	if s := cellsToString(expandIcons("@0Y\\QCreate@ Create", Style{})); s != "＋  Create" {
		t.Errorf("create = %q, want %q", s, "＋  Create")
	}
	// An unknown code still resolves (neutral glyph) and drops its tooltip.
	if s := cellsToString(expandIcons("@ZZ\\QWhatever@ X", Style{})); s != "▣  X" {
		t.Errorf("unknown tooltip icon = %q, want %q", s, "▣  X")
	}
}

// A malformed token (no closing @) is left as literal text, not swallowed.
func TestExpandIconsMalformed(t *testing.T) {
	got := cellsToString(expandIcons("@10\\Qno close", Style{}))
	if got != "@10\\Qno close" {
		t.Errorf("malformed = %q, want it left literal", got)
	}
}

func TestDrawTabBar(t *testing.T) {
	tabs := []diag.Tab{
		{Caption: "Attributes", Fcode: "=HD", Active: true},
		{Caption: "Element list", Fcode: "=FL"},
		{Caption: "Flow logic", Fcode: "=LS"},
	}
	g := NewGrid(3, 80)
	spans := DrawTabBar(g, tabs, 0)
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
	line := g.Line(0)
	for _, cap := range []string{"Attributes", "Element list", "Flow logic"} {
		if !strings.Contains(line, cap) {
			t.Errorf("tab bar %q missing %q", line, cap)
		}
	}
	// spans are contiguous and carry the fcodes for hit-testing.
	if spans[0].Col != 0 || !spans[0].Active || spans[0].Fcode != "=HD" {
		t.Errorf("span[0] = %+v, want col 0, active, =HD", spans[0])
	}
	if spans[1].Col != spans[0].Col+spans[0].Width {
		t.Errorf("spans not contiguous: %+v %+v", spans[0], spans[1])
	}
	if spans[2].Fcode != "=LS" {
		t.Errorf("span[2].Fcode = %q, want =LS", spans[2].Fcode)
	}
}

func TestExpandIconsQuickinfoNoIcon(t *testing.T) {
	// A plain label with a quickinfo but no icon: @\Qtooltip@caption. The
	// tooltip is dropped, the caption kept, no glyph drawn.
	got := cellsToString(expandIcons("@\\QUp to 40 characters@Password", Style{}))
	if got != "Password" {
		t.Errorf("quickinfo-only = %q, want %q", got, "Password")
	}
}
