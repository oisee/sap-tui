package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/oisee/sap-tui/internal/cfw"
	"github.com/oisee/sap-tui/internal/diag"
)

// The Control Framework: after logon the server calls into the GUI over
// RFC_TR.00 items and waits for the GUI's RFC_TR.01 answers before it goes on.
// Two ways to answer:
//
//   - Replay (default): send the next captured RFC_TR.01. Works only while the
//     server's call sequence matches the capture; a novel action produces a
//     call whose layout no longer lines up, so SAPLOLEA dereferences a handle
//     this session never minted and dumps (X373 '-1').
//   - Generate (--cfw, s.engine set): patch the captured answer's value pool
//     with THIS session's own minted OLE handles, from the live call's verbs.

// controlAnswers reads every C->S frame of a capture that carries an
// RFC_TR.01, in order.
func controlAnswers(path string) ([][]byte, error) {
	frames, err := clientFrames(path, 1<<20)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, fr := range frames {
		m, err := diag.ParseMessage(fr, false)
		if err != nil {
			continue
		}
		if hasRFCTR(diag.ParseItems(m.Body), 0x01) {
			out = append(out, fr)
		}
	}
	return out, nil
}

// answerControl answers one server RFC_TR.00. With an engine it generates the
// answer (minting this session's handles); otherwise it replays the next
// captured RFC_TR.01. It returns whether one was sent.
func (s *session) answerControl(liveItems []diag.Item, counter uint32) (bool, error) {
	if s.controlNext >= len(s.controls) {
		fmt.Fprintf(os.Stderr, "tui: RFC_TR.00 from the server, no captured answer left (%d used)\n", s.controlNext)
		return false, nil
	}
	capFrame := s.controls[s.controlNext]
	var out []byte
	var err error
	if s.engine != nil {
		if out, err = s.genControlAnswer(liveItems, capFrame, counter); err != nil {
			fmt.Fprintf(os.Stderr, "tui: cfw generate failed (%v); replaying instead\n", err)
			out, err = recount(capFrame, counter, s.compress)
		} else {
			fmt.Fprintf(os.Stderr, "tui: RFC_TR.01 GENERATED %d/%d (handles minted: %d)\n", s.controlNext+1, len(s.controls), len(s.engine.Minted))
		}
	} else {
		out, err = recount(capFrame, counter, s.compress)
	}
	if err != nil {
		return false, err
	}
	s.controlNext++
	if err := s.send(out); err != nil {
		return false, err
	}
	if s.engine == nil {
		fmt.Fprintf(os.Stderr, "tui: RFC_TR.01 answer %d/%d sent\n", s.controlNext, len(s.controls))
	}
	return true, nil
}

// genControlAnswer builds an RFC_TR.01 for the live RFC_TR.00: it takes the
// captured answer, fills its value-pool _RESULT slots with handles this session
// mints for the live call's object-creating verbs, splices the pool back, and
// rewrites the counter and framing.
func (s *session) genControlAnswer(liveItems []diag.Item, capFrame []byte, counter uint32) ([]byte, error) {
	var verbs []cfw.Verb
	for _, it := range liveItems {
		if it.ID == 0x08 && it.SID == 0x00 {
			if v, _, _, e := cfw.Streams(it.Value); e == nil {
				verbs = cfw.ParseVerbs(v)
			}
			break
		}
	}
	m, err := diag.ParseMessage(capFrame, false)
	if err != nil {
		return nil, err
	}
	items := diag.ParseItems(m.Body)
	patched := false
	for i := range items {
		it := &items[i]
		switch {
		case it.ID == 0x08 && it.SID == 0x01:
			_, desc, values, e := cfw.Streams(it.Value)
			if e != nil || values == nil {
				return nil, fmt.Errorf("captured answer has no value pool: %v", e)
			}
			filled, _ := s.engine.FillResults(verbs, cfw.ParseSvarsDesc(desc), values)
			// Rewrite input handle references to earlier objects into our own
			// handles, so the whole session speaks the handles we minted.
			s.engine.RemapInputs(filled)
			spliced, e := cfw.SpliceValuePool(it.Value, filled)
			if e != nil {
				return nil, e
			}
			it.Value = spliced
			patched = true
		case it.Type == diag.ItemAPPL && it.ID == 0x04 && it.SID == 0x26 && len(it.Value) == 4:
			it.Value = append([]byte{}, it.Value...)
			binary.BigEndian.PutUint32(it.Value, counter)
		}
	}
	if !patched {
		return nil, fmt.Errorf("no RFC_TR.01 item in the captured answer")
	}
	return encodeClient(m.Header, items, s.compress)
}
