package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/oisee/sap-tui/internal/demo"
	"github.com/oisee/sap-tui/internal/frame"
	"github.com/oisee/sap-tui/internal/tui"
)

// runDemo animates, non-interactively, entirely locally: no socket, no SAP, no
// handshake. It runs the very scenes cmd/server pushes to a real GUI — the same
// pkg/demo engine — and draws them straight through the styled TUI (chrome +
// colour) on a wall-clock timer. Dynpro scenes render as widgets; the LED
// scenes render in the classic-list channel, in colour, exactly the channel the
// server pushes them on. It is the offline counterpart to pointing the tui at
// the demo server, without that server's live-handshake fragility. Ctrl-C or q
// quits; it is read-only and sends nothing anywhere.
func runDemo(sceneMS int, plain bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go watchQuit(ctx, stop)

	ch := chrome{
		title:   "SAP GUI as a display — odgp demo",
		menus:   []string{"Menu", "Edit", "Goto", "System", "Help"},
		toolbar: []string{"@0V@ Play", "@0W@ Stop"},
		sysid:   "ODGP",
		program: "ZODGP_DEMO",
		dynpro:  "0100",
	}
	scenes := demo.Scenes()
	defDur := time.Duration(sceneMS) * time.Millisecond
	const fps = 20
	tick := time.NewTicker(time.Second / fps)
	defer tick.Stop()

	sceneStart := time.Now()
	idx := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Print("\x1b[2J\x1b[H")
			return nil
		case now := <-tick.C:
			sc := scenes[idx]
			dur := sc.Dur
			if dur == 0 {
				dur = defDur
			}
			if now.Sub(sceneStart) >= dur {
				idx = (idx + 1) % len(scenes)
				sceneStart = now
				sc = scenes[idx]
			}
			ts := now.Sub(sceneStart).Seconds()
			rows, cols := terminalSize()
			note := fmt.Sprintf("scene %d/%d  %s", idx+1, len(scenes), sc.Name)

			var canvas *tui.Grid
			switch {
			case sc.List != nil:
				canvas = tui.RenderList(sc.List(ts), 22, 100)
			case sc.Dynpro != nil:
				scr := frame.New(24, 118)
				sc.Dynpro(ts, scr)
				canvas = tui.Render(scr.Atoms(), 22, 100)
			default:
				canvas = tui.NewGrid(22, 100)
			}

			if plain {
				if rows > 1 {
					canvas = canvas.Clip(rows-1, cols)
				}
				printClear(canvas.String() + "\n\x1b[7m " + note + "  |  q quits \x1b[0m")
			} else {
				g := ch.compose(canvas, byte('S'), sc.Approach, note+"  q quits", rows, cols)
				printClear(g.ANSI())
			}
		}
	}
}
