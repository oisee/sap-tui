package diag

import (
	"bytes"
	"encoding/binary"
	"strings"
)

// The GUI status travels as MNUENTRY items (APPL4 id 0x0b): sid 0x01 the menu
// bar titles, 0x02 the dropdown tree, 0x03 the application toolbar, 0x04 the
// bound function keys. Every entry has the same 20-byte head, read off the
// logon capture (KNOWLEDGE.md §8):
//
//	[2 len BE][2 pos: menu#, item#][2 flags][2 type/code][2 pos-repeat]
//	[2 flags][8 zero][text NUL][1 accelerator][pad to len]
//
// where len counts the whole entry. On the toolbar (.03) the text is
// `caption NUL NUL tooltip NUL`, the caption possibly led by an `@XX@` icon.
// Confirmed on .01/.02/.03/.04 of one capture; the flag bits are inferred.

// MenuEntry is one entry of a MNUENTRY item.
type MenuEntry struct {
	// Menu and Item are the position bytes: the menu number and the item
	// number within it (the menu bar's titles have Item 0).
	Menu, Item int
	// Flag is the high byte of type/code (0x16 a menu-bar title, 0x08 a
	// separator, 0x02 an enabled leaf, 0x12 a leaf bound to a key/toolbar
	// slot) and Code its low byte, the function number that joins .02/.03/.04
	// (0x64 = 100 for a menu-only item).
	Flag, Code byte
	// Text is the caption; on the toolbar Tooltip is the second string.
	Text, Tooltip string
	// Accel is the Alt-accelerator letter, or 0.
	Accel byte
}

// Separator reports whether the entry is a menu separator line.
func (e MenuEntry) Separator() bool { return e.Flag&0x08 != 0 }

// menuEntryHead is the fixed part of an entry before its text.
const menuEntryHead = 20

// ParseMenuEntries splits a MNUENTRY item value into its entries. A truncated
// or zero-length entry ends the parse; the entries before it are returned.
func ParseMenuEntries(value []byte) []MenuEntry {
	var out []MenuEntry
	for len(value) >= menuEntryHead {
		n := int(binary.BigEndian.Uint16(value))
		if n < menuEntryHead || n > len(value) {
			break
		}
		e := MenuEntry{
			Menu: int(value[2]), Item: int(value[3]),
			Flag: value[6], Code: value[7],
		}
		body := value[menuEntryHead:n]
		parts := bytes.Split(body, []byte{0})
		if len(parts) > 0 {
			e.Text = string(parts[0])
		}
		// The toolbar form: caption, an empty field, the tooltip. A menu
		// entry instead has the accelerator letter right after the caption's
		// NUL, which is a single printable byte, not an empty field.
		if len(parts) >= 3 && len(parts[1]) == 0 {
			e.Tooltip = string(parts[2])
		} else if len(parts) >= 2 && len(parts[1]) >= 1 && parts[1][0] > ' ' {
			e.Accel = parts[1][0]
		}
		out = append(out, e)
		value = value[n:]
	}
	return out
}

// Menu is one dropdown of the menu bar: its title and its top-level items.
type Menu struct {
	Title string
	Num   int
	Items []MenuItem
}

// MenuItem is one entry of a dropdown: its caption, the function number it
// fires (Code; 100 = a menu-only node with no direct function), whether it is
// a separator, and whether it opens a submenu.
type MenuItem struct {
	Text      string
	Code      int
	Separator bool
	HasSub    bool
}

// ParseMenus builds the menu bar from a frame's GUI status: the titles
// (MNUENTRY.01) and the dropdown tree (MNUENTRY.02). It returns each menu with
// its TOP-LEVEL items — the first run of increasing item numbers under that
// menu; submenu children (which repeat their parent's item number) are marked
// on the parent via HasSub but not yet expanded.
func ParseMenus(items []Item) []Menu {
	var titles, tree []MenuEntry
	for _, it := range items {
		if it.Type != ItemAPPL4 || it.ID != 0x0b {
			continue
		}
		switch it.SID {
		case 0x01:
			if es := ParseMenuEntries(it.Value); len(es) > len(titles) {
				titles = es
			}
		case 0x02:
			if es := ParseMenuEntries(it.Value); len(es) > len(tree) {
				tree = es
			}
		}
	}
	var menus []Menu
	for _, t := range titles {
		if t.Text == "" {
			continue
		}
		m := Menu{Title: t.Text, Num: t.Menu}
		maxItem, top := 0, true
		for _, e := range tree {
			if e.Menu != t.Menu {
				continue
			}
			if top && e.Item > maxItem {
				maxItem = e.Item
				m.Items = append(m.Items, MenuItem{
					Text:      e.Text,
					Code:      int(e.Code),
					Separator: e.Separator(),
					HasSub:    e.Flag&0x04 != 0 && !e.Separator(),
				})
			} else if !e.Separator() {
				top = false // an item number that did not advance starts the submenus
			}
		}
		menus = append(menus, m)
	}
	return menus
}

// FunctionLabels maps a function number to its caption, read from the GUI
// status: the function-key list (MNUENTRY.04), the toolbar (.03) and the menus
// (.02) all carry the same numbers as their Code. It lets a client name a fired
// function (F8 -> "Execute") for the user.
func FunctionLabels(items []Item) map[int]string {
	m := map[int]string{}
	// Scan the function-key list first, then the toolbar, then the menus: a
	// function's action name (F8 = "Execute") lives in .04/.03, while the menu
	// (.02) may repeat the same code under a wordier synonym ("Direct
	// Processing"). The first caption found for a code wins.
	for _, sid := range []byte{0x04, 0x03, 0x02} {
		for _, it := range items {
			if it.Type != ItemAPPL4 || it.ID != 0x0b || it.SID != sid {
				continue
			}
			for _, e := range ParseMenuEntries(it.Value) {
				if e.Separator() || e.Text == "" || e.Code == 0 {
					continue
				}
				if _, ok := m[int(e.Code)]; !ok {
					m[int(e.Code)] = e.Text
				}
			}
		}
	}
	return m
}

// ParseStatusMessage reads a VARINFO.03 status message (the inverse of
// StatusMessage): the message type (S, W, E, I) and the text. The wire form
// is type, NUL, class padded with spaces, NUL, number, NUL, a space, NUL,
// text; a value too short to hold that yields type 0 and an empty text.
func ParseStatusMessage(value []byte) (msgType byte, text string) {
	if len(value) == 0 {
		return 0, ""
	}
	msgType = value[0]
	parts := bytes.SplitN(value, []byte{0}, 5)
	if len(parts) < 5 {
		return msgType, ""
	}
	return msgType, strings.TrimRight(string(parts[4]), " \x00")
}

// ParseLayout reads the VARINFO.06 layout item: its first two big-endian
// u32 are the dynpro's rows and columns (19 x 91 on the captured logon
// screen), the next two repeat them, and a third pair is the GUI window in
// cells. Inferred from one capture; a short value yields zeros.
func ParseLayout(value []byte) (rows, cols int) {
	if len(value) < 8 {
		return 0, 0
	}
	return int(binary.BigEndian.Uint32(value)), int(binary.BigEndian.Uint32(value[4:]))
}
