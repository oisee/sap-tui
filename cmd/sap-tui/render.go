package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/oisee/sap-tui/internal/diag"
)

// renderCapture draws, offline, every screen a tap capture holds: it walks the
// S->C DIAG frames in order, carries the GUI chrome forward the way a live
// session does, and composes each frame that carries a screen. With once it
// draws the first screen and stops; otherwise it steps on Enter, so a capture
// can be paged through in a terminal with no SAP system behind it. Client
// frames and NI keepalives are skipped. Screens the tui cannot fully parse are
// still drawn as far as they go.
func renderCapture(path string, plain, once bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	s := &session{plain: plain}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	stdin := bufio.NewReader(os.Stdin)
	drawn := 0
	for sc.Scan() {
		var l struct{ Dir, Hex string }
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Dir != "S->C" {
			continue
		}
		data, err := hex.DecodeString(l.Hex)
		if err != nil || len(data) == 0 {
			continue
		}
		if _, ok := diag.NIControl(data); ok {
			continue
		}
		m, err := diag.ParseMessage(data, false)
		if err != nil {
			continue
		}
		items := diag.ParseItems(m.Body)
		msgType, msg := s.chrome.update(items)
		canvas, note, ok := s.chrome.frameCanvas(items)
		if !ok {
			continue // a handshake/status-only frame, no screen to draw
		}
		s.draw(canvas, msgType, msg, note)
		drawn++
		if once {
			return nil
		}
		// Step: Enter for the next screen, q to stop.
		fmt.Print("\x1b[7m -- screen ", drawn, " -- Enter=next  q=quit -- \x1b[0m")
		line, _ := stdin.ReadString('\n')
		if len(line) > 0 && (line[0] == 'q' || line[0] == 'Q') {
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if drawn == 0 {
		return fmt.Errorf("%s: no server screen frames found", path)
	}
	fmt.Fprintf(os.Stderr, "tui: %d screens drawn\n", drawn)
	return nil
}
