package diag

import "testing"

// entry builds one MNUENTRY entry the way the capture laid it out.
func entry(menu, item, flag, code byte, text string, accel byte) []byte {
	body := append([]byte(text), 0, accel, 0, 0)
	n := menuEntryHead + len(body)
	b := make([]byte, menuEntryHead, n)
	b[0], b[1] = byte(n>>8), byte(n)
	b[2], b[3] = menu, item
	b[6], b[7] = flag, code
	b[8], b[9] = menu, item
	return append(b, body...)
}

// The three menu-bar titles of the logon screen parse back with their
// positions, flags and accelerators.
func TestParseMenuBar(t *testing.T) {
	v := append(entry(1, 0, 0x16, 0x01, "User", 'U'), entry(2, 0, 0x16, 0x02, "System", 'y')...)
	v = append(v, entry(3, 0, 0x16, 0x03, "Help", 'H')...)
	got := ParseMenuEntries(v)
	if len(got) != 3 {
		t.Fatalf("entries = %d, want 3", len(got))
	}
	want := []string{"User", "System", "Help"}
	for i, e := range got {
		if e.Text != want[i] || e.Menu != i+1 || e.Flag != 0x16 {
			t.Errorf("entry %d = %+v, want text %q menu %d flag 0x16", i, e, want[i], i+1)
		}
	}
	if got[1].Accel != 'y' {
		t.Errorf("System accelerator = %q, want y", got[1].Accel)
	}
}

// A toolbar entry carries caption and tooltip separated by an empty field,
// exactly as MNUENTRY.03 did on the capture.
func TestParseToolbarEntry(t *testing.T) {
	body := append([]byte("New password"), 0, 0)
	body = append(body, []byte("Change it")...)
	body = append(body, 0)
	n := menuEntryHead + len(body)
	b := make([]byte, menuEntryHead, n)
	b[0], b[1] = byte(n>>8), byte(n)
	b[6], b[7] = 0x02, 0x05
	b = append(b, body...)
	got := ParseMenuEntries(b)
	if len(got) != 1 || got[0].Text != "New password" || got[0].Tooltip != "Change it" || got[0].Accel != 0 {
		t.Fatalf("toolbar entry = %+v", got)
	}
}

// A length that runs past the value ends the parse without a panic.
func TestParseMenuTruncated(t *testing.T) {
	v := entry(1, 0, 0x16, 0x01, "User", 'U')
	v[1] += 40
	if got := ParseMenuEntries(v); len(got) != 0 {
		t.Errorf("truncated entry parsed as %+v", got)
	}
}

// StatusMessage and ParseStatusMessage are inverses.
func TestParseStatusMessage(t *testing.T) {
	it := StatusMessage(MsgWarning, "odgp: now playing")
	typ, text := ParseStatusMessage(it.Value)
	if typ != 'W' || text != "odgp: now playing" {
		t.Errorf("parsed (%q, %q)", typ, text)
	}
	if typ, text := ParseStatusMessage([]byte{'E', 0, '0', '0'}); typ != 'E' || text != "" {
		t.Errorf("short value parsed (%q, %q)", typ, text)
	}
}

// The layout item's first two words are the dynpro size.
func TestParseLayout(t *testing.T) {
	v := []byte{0, 0, 0, 0x13, 0, 0, 0, 0x5b, 0, 0, 0, 0x13, 0, 0, 0, 0x5b}
	if r, c := ParseLayout(v); r != 19 || c != 91 {
		t.Errorf("layout = %dx%d, want 19x91", r, c)
	}
}
