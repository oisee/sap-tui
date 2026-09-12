// Package replay turns a tap capture into what a rogue server says: the
// server frames grouped by the client frame they answered, and a way to
// hand a real GUI those groups in turn. Nothing here understands the
// screens; it repeats what a real server said, which is the fastest way
// to learn what a GUI checks and what it lets pass.
package replay

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/oisee/sap-tui/internal/diag"
)

// Frame is one captured NI payload.
type Frame struct {
	At    time.Time
	Dir   string
	Conn  int
	Index int
	Data  []byte
}

// Capture is one connection's frames, and the server's answers grouped by
// the client frame they followed: Replies[k] is what the server sent
// after client frame k and before client frame k+1.
type Capture struct {
	Client  []Frame
	Server  []Frame
	Replies [][]Frame
}

// Load reads a tap capture and keeps one connection.
func Load(path string, conn int) (*Capture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c := &Capture{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	group := -1
	for sc.Scan() {
		var l struct {
			At    time.Time `json:"at"`
			Dir   string    `json:"dir"`
			Conn  int       `json:"conn"`
			Index int       `json:"index"`
			Hex   string    `json:"hex"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Conn != conn {
			continue
		}
		data, err := hex.DecodeString(l.Hex)
		if err != nil {
			continue
		}
		fr := Frame{At: l.At, Dir: l.Dir, Conn: l.Conn, Index: l.Index, Data: data}
		switch l.Dir {
		case "C->S":
			c.Client = append(c.Client, fr)
			group++
			c.Replies = append(c.Replies, nil)
		case "S->C":
			c.Server = append(c.Server, fr)
			if group >= 0 {
				c.Replies[group] = append(c.Replies[group], fr)
			}
		}
	}
	if len(c.Client) == 0 {
		return nil, fmt.Errorf("%s: no client frames on connection %d", path, conn)
	}
	return c, nil
}

// Plain is a captured server frame with its compression undone and the
// header saying so, which a GUI accepts as it accepts the compressed one.
// Frames that are not DIAG (NI keepalives) come back unchanged.
func Plain(data []byte) ([]byte, error) {
	if _, ok := diag.NIControl(data); ok {
		return data, nil
	}
	m, err := diag.ParseMessage(data, false)
	if err != nil {
		return nil, err
	}
	h := m.Header
	h.Compress = 0
	out := append([]byte{}, m.DP...)
	out = append(out, h.Bytes()...)
	out = append(out, m.Body...)
	return out, nil
}

// ServerFrame finds a server frame by its capture index.
func (c *Capture) ServerFrame(index int) (Frame, bool) {
	for _, f := range c.Server {
		if f.Index == index {
			return f, true
		}
	}
	return Frame{}, false
}

// FindPopup scans a whole capture, every connection, for the modal log-off
// popup SAP sends when a window is closed — a server frame whose DYNT_ATOM
// names "log off". Its bytes are the template a rogue server reuses to show
// a modal of its own: the frame is already a modal dialog, so only its text
// and buttons need swapping. Empty when the capture has none.
func FindPopup(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Dir string `json:"dir"`
			Hex string `json:"hex"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Dir != "S->C" || l.Hex == "" {
			continue
		}
		data, err := hex.DecodeString(l.Hex)
		if err != nil {
			continue
		}
		m, err := diag.ParseMessage(data, false)
		if err != nil {
			continue
		}
		for _, it := range diag.ParseItems(m.Body) {
			if it.Type == diag.ItemAPPL4 && it.ID == 0x09 && it.SID == 0x02 &&
				strings.Contains(strings.ToLower(string(it.Value)), "log off") {
				return data
			}
		}
	}
	return nil
}

// FindListWrap scans a whole capture for a plain classic-list frame — a
// server frame whose list segments include "end of list", the marker of a
// WRITE report rather than a data-browser list with controls. Its bytes are
// the wrapper a colourful list of ours reuses. Empty when none is found.
func FindListWrap(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Dir string `json:"dir"`
			Hex string `json:"hex"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Dir != "S->C" || l.Hex == "" {
			continue
		}
		data, err := hex.DecodeString(l.Hex)
		if err != nil {
			continue
		}
		m, err := diag.ParseMessage(data, false)
		if err != nil {
			continue
		}
		items := diag.ParseItems(m.Body)
		if !diag.HasListSegments(items) {
			continue
		}
		for _, seg := range diag.ParseListItems(items) {
			if strings.Contains(strings.ToLower(seg.Text), "end of list") {
				return data
			}
		}
	}
	return nil
}
