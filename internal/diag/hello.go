package diag

import (
	"os"
)

// Building the opening hello from scratch — the capture-free handshake.
//
// A SAP GUI's first frame to the dispatcher is: a 200-byte DP (dispatcher)
// header, an 8-byte DIAG header with the INI com-flag, and a short ST_USER
// "connect" item block. This file constructs that frame in code rather than
// replaying a captured one, so cmd/tui needs no --hello template to reach the
// logon screen.
//
// What each part is, read off the wire:
//   - The DP header is mostly unset routing (0xff, "dispatcher, assign me")
//     and space-padded fields. The one field that carries meaning for us is
//     the frontend terminal name (the hostname), NUL-terminated at offset
//     dpTerminalOffset. A trailing block (~offset 126..199) holds frontend
//     runtime values (timestamps/handles); the dispatcher does not validate
//     them for a connect, so we carry a fixed, documented skeleton there.
//   - The DIAG header is mode 0, com INI (0x10), everything else 0, compress 0.
//   - The connect block is seven ST_USER items: CONNECT (three u32 window/
//     timeout params), RFC_PARENT_UUID (a frontend id the dispatcher VALIDATES,
//     carried as a documented constant — see rfcParentUUID), GUI_SESSION_UUID
//     (a structured handle pair), SUPPORTDATA (capability flags), two small
//     fixed flags (0x17, 0x16), and the logon language (0x27). The server
//     assigns none of these; the terminal name and language are ours to set.

// dpHeaderSkeleton is the 200-byte DP header a GUI sends on connect, as a
// documented constant. dpTerminalOffset is overwritten with our hostname and
// the language item is set per call; the rest is the connect skeleton.
var dpHeaderSkeleton = [200]byte{
	0xff, 0xff, 0xff, 0xff, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x75, 0x00, 0x00, 0x00, 0x00, 0xff,
	0xff, 0xff, 0xff, 0x00, 0x01, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
	0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
	0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
	0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x69, 0x6d, 0x71,
	0x74, 0x32, 0x36, 0x00, 0x76, 0x23, 0x23, 0x75, 0x66, 0x23, 0x32, 0x23,
	0x76, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x20,
	0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
	0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0xe7, 0x22, 0x70, 0x22, 0xe7, 0x22,
	0x90, 0xaa, 0xff, 0xff, 0xff, 0xff, 0xe7, 0x22, 0x80, 0x50, 0x01, 0x80,
	0xf4, 0x03, 0xb3, 0x65, 0x60, 0xe7, 0x64, 0x1c, 0x00, 0x00, 0x00, 0x00,
	0x8c, 0xac, 0x60, 0x67, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x44, 0xbb, 0xb2, 0x65, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xb3, 0x50, 0xf2, 0xd5, 0x00, 0x15, 0x00, 0x80,
}

// dpTerminalOffset is where the NUL-terminated frontend hostname sits in the
// DP header (a space-padded field whose width the skeleton's own NUL marks).
const dpTerminalOffset = 81

// connectParams is the ST_USER.CONNECT payload: three u32 window/timeout
// parameters a GUI sends unchanged.
var connectParams = []byte{0x00, 0x01, 0x87, 0x68, 0x00, 0x00, 0x04, 0x4c, 0x00, 0x00, 0x0b, 0xb8}

// guiSessionUUID is the ST_USER.GUI_SESSION_UUID handle block (structured, not
// random; carried as observed).
var guiSessionUUID = []byte{0x00, 0x00, 0x00, 0x37, 0x00, 0x00, 0x00, 0xd1, 0x00, 0x00, 0x00, 0x37, 0x00, 0x00, 0x00, 0xd1}

// supportData is the ST_USER.SUPPORTDATA capability flags.
var supportData = []byte{0x00, 0x16, 0x00, 0x08, 0x00, 0x10, 0x00, 0x08}

// rfcParentUUID is the ST_USER.RFC_PARENT_UUID the frontend sends on connect.
// It is NOT free-form: the dispatcher validates its content and drops the
// connection on a random value (tested live — a minted UUID gets an immediate
// EOF, the observed one reaches the logon screen). Its 32-byte structure is not
// yet understood, so it is carried as a documented constant until reversed.
// (Minting it correctly is the remaining piece of a fully synthetic hello.)
var rfcParentUUID = []byte{
	0xff, 0x7f, 0xfa, 0x0d, 0x78, 0xb7, 0x37, 0xde, 0xf6, 0x19, 0x6e, 0x93, 0x25, 0xbf, 0x15, 0x97,
	0xef, 0x73, 0xfe, 0xeb, 0xdb, 0x51, 0xfd, 0x91, 0xce, 0x24, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00,
}

// BuildHello constructs the opening hello frame for the given terminal name
// (empty = this host's name) and logon language. The result is a complete NI
// payload: DP header, DIAG INI header, and the connect item block, ready to be
// NI-framed and sent. It reads no capture.
func BuildHello(terminal, lang string) []byte {
	dp := dpHeaderSkeleton // copy
	if terminal == "" {
		terminal, _ = os.Hostname()
	}
	setDPTerminal(dp[:], terminal)

	if lang == "" {
		lang = "EN"
	}
	langb := []byte(lang)
	if len(langb) < 2 {
		langb = append(langb, ' ')
	}
	langb = langb[:2]

	h := Header{ComFlag: FlagTermINI} // mode 0, INI, info 0, compress 0
	items := []Item{
		{Type: ItemAPPL, ID: 0x04, SID: 0x02, Value: connectParams},
		{Type: ItemAPPL, ID: 0x04, SID: 0x0b, Value: rfcParentUUID},
		{Type: ItemAPPL, ID: 0x04, SID: 0x0d, Value: guiSessionUUID},
		{Type: ItemAPPL, ID: 0x04, SID: 0x04, Value: supportData},
		{Type: ItemAPPL, ID: 0x04, SID: 0x17, Value: []byte{0x00, 0x22}},
		{Type: ItemAPPL, ID: 0x04, SID: 0x16, Value: []byte{0x00, 0x11}},
		{Type: ItemAPPL, ID: 0x04, SID: 0x27, Value: langb},
	}
	out := make([]byte, 0, 200+HeaderLen+64)
	out = append(out, dp[:]...)
	out = append(out, h.Bytes()...)
	out = append(out, EncodeItems(items)...)
	return out
}

// setDPTerminal writes the terminal name into the DP header's terminal field.
// The field runs from dpTerminalOffset up to the NUL the skeleton already
// carries there; keeping that width leaves the following field untouched.
func setDPTerminal(dp []byte, name string) {
	end := dpTerminalOffset
	for end < len(dp) && dp[end] != 0x00 {
		end++
	}
	width := end - dpTerminalOffset
	if width <= 0 {
		return
	}
	b := []byte(name)
	if len(b) > width {
		b = b[:width]
	}
	for i := 0; i < width; i++ {
		dp[dpTerminalOffset+i] = ' '
	}
	copy(dp[dpTerminalOffset:], b)
}
