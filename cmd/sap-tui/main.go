// tui is a SAP GUI-protocol terminal: it draws the screens a dispatcher sends
// the way SAP GUI lays them out — the window chrome (title bar, menu bar,
// application toolbar, status bar) around a canvas of dynpro elements or a
// classic list, in colour, with icons and framed group boxes. Two modes:
//
//	# read-only: connect, send a captured hello, draw every screen the server
//	# sends and nothing back but NI_PONG keepalives.
//	tui --addr host:port --hello captures/probe.jsonl
//
//	# live logon: same, but answer the logon screen once with credentials from
//	# a .mcp.json server, then keep drawing each screen the server sends.
//	tui --addr host:32NN --hello captures/probe.jsonl --logon --mcp .mcp.json --server a4h
//
// It never sends a keystroke or a function code of its own, and it makes at
// most one logon attempt per run: a second wrong password would count towards
// locking the user, so a logon screen that comes back is drawn, not answered.
// The password is read from the .mcp.json the user points at (or ODGP_PASSWORD
// when --user is given on the command line) and is never logged or drawn.
// Point this only at your own system.
package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
	"github.com/oisee/sap-tui/internal/ni"

	"github.com/oisee/sap-tui/internal/cfw"
	"github.com/oisee/sap-tui/internal/diag"
	"github.com/oisee/sap-tui/internal/tui"
)

func main() {
	addr := flag.String("addr", "", "dispatcher to connect to, host:port (required)")
	hello := flag.String("hello", "", "capture whose first C->S frame is sent as the opening hello (required)")
	once := flag.Bool("once", false, "draw the first screen received, then quit")
	plain := flag.Bool("plain", false, "draw without colour or chrome (the old bare grid)")
	logon := flag.Bool("logon", false, "answer the logon screen once, then keep drawing the session")
	mcpPath := flag.String("mcp", "", "a .mcp.json to read credentials from, for --logon")
	server := flag.String("server", "", "which server in --mcp to use")
	user := flag.String("user", "", "logon user (password from ODGP_PASSWORD); overrides --mcp")
	client := flag.String("client", "", "logon client; with --user")
	lang := flag.String("lang", "EN", "logon language; with --user")
	compress := flag.Bool("compress", false, "LZH-compress the frames we send (default off: DIAG accepts plain)")
	render := flag.String("render", "", "offline: read this tap capture and draw every S->C screen it holds, no connection")
	demo := flag.Bool("demo", false, "offline: animate a self-contained demo through the styled TUI, no connection")
	sceneMS := flag.Int("scene-ms", 4000, "milliseconds each demo scene runs (with --demo)")
	interactive := flag.Bool("interactive", false, "read the keyboard: edit fields, Enter/OK-code send a PAI (one logon per run)")
	dump := flag.String("dump", "", "record every frame both ways to this JSONL file (tap format, for cmd/lens)")
	run := flag.String("run", "", "an OK-code to send on the first screen after logon, e.g. /nse38 (with --logon)")
	script := flag.String("script", "", "headless: after logon, play a ';'-separated sequence of steps, one per server response — an OK-code (/nse38, =BACK) or a function key (F8, F3); builds screen state without a terminal (with --logon, no --interactive)")
	cfwGen := flag.Bool("cfw", false, "generate RFC_TR.01 control answers with this session's own OLE handles instead of replaying captured ones (the SAPLOLEA experiment)")
	fkeys := flag.String("fkeys", "", `bind function keys to OK-codes, comma-separated N=CODE, e.g. "8=STRT,3==BACK,12=/n" (F8 fires STRT, F3 fires =BACK, F12 fires /n); a bare CODE gets a leading = as a function code, the screen's GUI status decides what each F-key means`)
	flag.Parse()

	// Offline demo: no socket, no SAP. Animate locally through the renderer.
	if *demo {
		if err := runDemo(*sceneMS, *plain); err != nil {
			fmt.Fprintln(os.Stderr, "tui: demo:", err)
			os.Exit(1)
		}
		return
	}

	// Offline render: no socket, no SAP. Walk a capture's server frames and
	// draw each screen, stepping on Enter. The way to eyeball the renderer.
	if *render != "" {
		if err := renderCapture(*render, *plain, *once); err != nil {
			fmt.Fprintln(os.Stderr, "tui: render:", err)
			os.Exit(1)
		}
		return
	}

	if *logon && *hello == "" {
		fmt.Fprintln(os.Stderr, "tui: --logon still needs --hello for the PAI env block (capture-free logon is the next step); synthesized hello is read-only for now")
		os.Exit(2)
	}
	var creds credentials
	if *logon {
		var err error
		var derived string
		creds, derived, err = resolveCreds(*mcpPath, *server, *user, *client, *lang)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tui: logon:", err)
			os.Exit(1)
		}
		if *addr == "" {
			*addr = derived
		}
	}
	// Accept the address as a bare positional argument too: `sap-tui host:port`.
	if *addr == "" && flag.NArg() > 0 {
		*addr = flag.Arg(0)
	}
	if *addr == "" {
		fmt.Fprintln(os.Stderr, "usage: sap-tui HOST:PORT   (or --addr HOST:PORT)")
		os.Exit(2)
	}

	var helloBytes []byte
	var err error
	if *hello != "" {
		helloBytes, err = helloFromCapture(*hello)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tui: hello:", err)
			os.Exit(1)
		}
	} else {
		// Capture-free handshake: construct the opening hello from scratch.
		helloBytes = diag.BuildHello("", *lang)
		fmt.Fprintln(os.Stderr, "tui: synthesized hello (no --hello capture)")
	}
	var template []byte
	if *hello != "" {
		template, _ = logonTemplate(*hello) // the captured client logon PAI, for --logon
	}
	var env *envTemplate
	if template != nil {
		env, _ = newEnvTemplate(template)
	}
	var controls [][]byte
	if *logon || *interactive {
		controls, _ = controlAnswers(*hello)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	s := &session{
		plain:       *plain,
		once:        *once,
		logon:       *logon,
		interactive: *interactive,
		creds:       creds,
		template:    template,
		env:         env,
		controls:    controls,
		compress:    *compress,
		runOnce:     *run,
		steps:       splitSteps(*script),
		scripted:    *script != "" && !*interactive,
		fkeys:       parseFKeys(*fkeys),
	}
	if *cfwGen {
		s.engine = cfw.NewEngine()
	}
	if *dump != "" {
		d, err := newDumper(*dump)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tui: dump:", err)
			os.Exit(1)
		}
		defer d.close()
		s.dump = d
	}
	if err := s.run(ctx, *addr, helloBytes); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}

// parseFKeys reads the --fkeys binding string ("8=STRT,3==BACK,12=/n") into a
// map of F-key number -> OK-code. A bare code (no leading = or /) is taken as a
// function code and gets a leading =, the form the OK-code field wants.
func parseFKeys(spec string) map[int]string {
	m := map[int]string{}
	for _, pair := range strings.Split(spec, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(pair[:eq]), "%d", &n); err != nil || n < 1 || n > 24 {
			continue
		}
		code := strings.TrimSpace(pair[eq+1:])
		if code == "" {
			continue // "N=" with no code: not a binding, leave F-N unbound
		}
		if !strings.HasPrefix(code, "=") && !strings.HasPrefix(code, "/") {
			code = "=" + code
		}
		m[n] = code
	}
	return m
}

// statusItems returns the MNUENTRY items of a frame (the GUI status: menu bar,
// menus, toolbar, function keys), or nil when the frame carries none.
func statusItems(items []diag.Item) []diag.Item {
	var out []diag.Item
	for _, it := range items {
		if it.Type == diag.ItemAPPL4 && it.ID == 0x0b {
			out = append(out, it)
		}
	}
	return out
}

// stripMarkup removes SAP icon tokens (@XX@ and @XX\Qtooltip@) from a caption
// and trims it, so a pushbutton's visible label can be matched by name.
func stripMarkup(s string) string {
	for {
		i := strings.IndexByte(s, '@')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i+1:], '@')
		if j < 0 {
			break
		}
		s = s[:i] + s[i+1+j+1:]
	}
	return strings.TrimSpace(s)
}

// okCodeForFKey resolves function key n to an OK-code string by joining the
// screen's accelerator table (key -> function number) and the GUI status
// (number -> label) with an on-screen pushbutton of that label (label ->
// function code). Coverage is partial by nature: only functions that have a
// labelled pushbutton on the current screen carry their code on the wire (the
// GUI status has numbers and labels but no code strings), so a toolbar-only
// function like Execute is not resolvable this way and needs --fkeys.
func (s *session) okCodeForFKey(n int) (string, bool) {
	fn, ok := s.fkeyFuncs[n]
	if !ok || s.scr == nil {
		return "", false
	}
	label := diag.FunctionLabels(s.statusItems)[fn]
	if label == "" {
		return "", false
	}
	return s.fcodeForLabel(label)
}

// fcodeForLabel returns the function code of an on-screen pushbutton whose
// visible caption matches label (icon markup stripped), the only place a
// function's code string is on the wire.
func (s *session) fcodeForLabel(label string) (string, bool) {
	if s.scr == nil {
		return "", false
	}
	for _, a := range s.scr.atoms {
		if a.EType == diag.AtomPushbutton && a.Function != "" && stripMarkup(a.Value()) == label {
			return a.Function, true
		}
	}
	return "", false
}

// splitSteps parses a --script string ("/nse38;F8") into trimmed steps,
// dropping empties.
func splitSteps(spec string) []string {
	var out []string
	for _, p := range strings.Split(spec, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// sendStep plays one script step: "F<n>" fires the function key (resolved to
// its number through the screen's accelerator table, the way a real GUI does);
// anything else is sent as an OK-code.
func (s *session) sendStep(step string) error {
	if n, ok := fkeyStep(step); ok {
		code, bound := s.fkeys[n]
		if !bound {
			code, bound = s.okCodeForFKey(n)
		}
		if !bound {
			return fmt.Errorf("F%d has no resolvable code on this screen (bind it with --fkeys %d=CODE)", n, n)
		}
		fmt.Fprintf(os.Stderr, "tui: script F%d -> OK-code %q\n", n, code)
		return s.sendPAI(code, -1)
	}
	fmt.Fprintf(os.Stderr, "tui: script OK-code %q\n", step)
	return s.sendPAI(step, -1)
}

// fkeyStep parses a "F8"/"f12" step into its function-key number.
func fkeyStep(step string) (int, bool) {
	if len(step) < 2 || (step[0] != 'F' && step[0] != 'f') {
		return 0, false
	}
	n, err := strconv.Atoi(step[1:])
	if err != nil || n < 1 || n > 24 {
		return 0, false
	}
	return n, true
}

// resolveCreds gathers logon credentials: from a .mcp.json server when --mcp
// is given, else from --user with the password in ODGP_PASSWORD.
func resolveCreds(mcpPath, server, user, client, lang string) (credentials, string, error) {
	if mcpPath != "" {
		if server == "" {
			return credentials{}, "", fmt.Errorf("--mcp needs --server")
		}
		return fromMCP(mcpPath, server)
	}
	if user == "" {
		return credentials{}, "", fmt.Errorf("give --mcp/--server or --user")
	}
	pw := os.Getenv("ODGP_PASSWORD")
	if pw == "" {
		return credentials{}, "", fmt.Errorf("set ODGP_PASSWORD for --user %s", user)
	}
	return credentials{Client: client, User: user, Password: pw, Lang: lang}, "", nil
}

// session is one connection's state: the flags, the chrome carried between
// screens, the interactive screen, and whether the one logon attempt has been
// made.
type session struct {
	plain       bool
	once        bool
	logon       bool
	interactive bool
	compress    bool
	creds       credentials
	template    []byte
	env         *envTemplate
	controls    [][]byte // captured RFC_TR.01 answers, replayed in order
	controlNext int
	chrome      chrome
	loggedOn    bool // the one logon PAI of this run has been sent
	drewOnce    bool
	conn        net.Conn
	dump        *dumper

	// the live screen, for the interactive mode
	scr       *screenState
	ses       []byte // the server's last session id
	dynn      []byte // the server's last DYNN.01
	counter   uint32 // the server's last ST_USER.26
	stat      byte   // the server's last header mode-stat, echoed
	msgType   byte   // last status message, redrawn under local edits
	msg       string
	hasList   bool // the current screen is a classic list, not a dynpro
	items     []diag.Item
	pending   bool             // a PAI is out, its answer not yet drawn
	logonSeen bool             // the screen on show is the logon screen
	runOnce   string           // an OK-code to send on the first screen after logon
	steps       []string       // headless script: OK-codes / F-keys, one per response (--script)
	scripted    bool           // play steps without a terminal
	statusItems []diag.Item    // the last GUI status (MNUENTRY), persisted across frames
	menuOpen    bool           // the menu bar is being navigated
	menus       []diag.Menu    // the parsed menu tree, while navigating
	menuIdx     int            // the open menu (index into menus)
	menuItem    int            // the highlighted item in the open menu
	fkeys      map[int]string   // function-key -> OK-code bindings (--fkeys)
	fkeyFuncs  map[int]int      // function-key -> function number, from the accelerator table
	accelKeys  map[int]string   // function number -> keystroke label, from the accelerator table
	engine    *cfw.Engine      // when set, generate control answers instead of replaying
	palette   bool             // the command palette overlay is open
	palLines  []string         // its lines
	palScroll int              // its scroll offset
	screen    tcell.Screen     // the interactive terminal backend (tcell)
	prevBtn   tcell.ButtonMask // last mouse button mask, to detect a click
}

// redirectStderr routes stderr to a temp log file so the "tui: …" diagnostics
// do not scroll the drawn screen, and returns a cleanup that restores it and
// prints the log's path. A failure to open the log leaves stderr as it was.
func redirectStderr(label string) func() {
	logf, err := os.CreateTemp("", "odgp-tui-*.log")
	if err != nil {
		return func() {}
	}
	fmt.Fprintf(os.Stderr, "tui: %s; diagnostics -> %s\n", label, logf.Name())
	orig := os.Stderr
	os.Stderr = logf
	return func() {
		os.Stderr = orig
		logf.Close()
		fmt.Fprintf(os.Stderr, "tui: session log at %s\n", logf.Name())
	}
}

// run connects, sends the hello once, and loops rendering screens until the
// connection closes, the context is cancelled, or (with once) the first
// screen is drawn. Interactive, it also reads the keyboard and answers
// screens with PAIs.
func (s *session) run(parent context.Context, addr string, helloBytes []byte) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	s.conn = conn
	go func() { <-ctx.Done(); conn.Close() }()

	if s.dump != nil {
		s.dump.write("C->S", helloBytes)
	}
	frame, err := ni.EncodeFrame(helloBytes)
	if err != nil {
		return fmt.Errorf("encoding hello: %w", err)
	}
	if _, err := conn.Write(frame); err != nil {
		return fmt.Errorf("sending hello: %w", err)
	}
	mode := "read-only"
	if s.logon {
		mode = "live logon as " + s.creds.User
	}
	if s.interactive {
		mode += ", interactive"
	}
	fmt.Fprintf(os.Stderr, "tui: connected to %s, hello sent (%d bytes); %s\n", addr, len(helloBytes), mode)

	var events chan tcell.Event
	if s.interactive {
		// tcell owns the terminal now. Our fmt.Fprintf(os.Stderr, "tui: …")
		// diagnostics share that terminal (fd 2) and would corrupt the drawn
		// screen, so route stderr to a log file for the session.
		defer redirectStderr("interactive")()
		scr, err := tcell.NewScreen()
		if err != nil {
			return fmt.Errorf("tcell: %w", err)
		}
		if err := scr.Init(); err != nil {
			return fmt.Errorf("tcell init: %w", err)
		}
		scr.EnableMouse()
		scr.Clear()
		s.screen = scr
		defer scr.Fini()
		events = make(chan tcell.Event, 32)
		go func() {
			for {
				ev := scr.PollEvent()
				if ev == nil {
					return
				}
				select {
				case events <- ev:
				case <-ctx.Done():
					return
				}
			}
		}()
	} else {
		// Read-only chrome mode also draws over the whole terminal, so the same
		// stderr diagnostics (the accelerator-table line and the rest) would
		// scroll the drawn screen up — route them to a log too. Plain mode is a
		// scrolling text view where that does not matter.
		if !s.plain && !s.once {
			defer redirectStderr("read-only")()
		}
		if !s.once {
			go watchQuit(ctx, cancel)
		}
	}

	// The socket is read on its own goroutine; frames and keys meet here.
	type inbound struct {
		payload []byte
		err     error
	}
	frames := make(chan inbound, 16)
	go func() {
		dec, err := ni.NewFrameDecoder(64 << 20)
		if err != nil {
			frames <- inbound{err: err}
			return
		}
		buf := make([]byte, 64<<10)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				frames <- inbound{err: err}
				return
			}
			fs, err := dec.Push(buf[:n])
			if err != nil {
				frames <- inbound{err: fmt.Errorf("ni framing: %w", err)}
				return
			}
			for _, p := range fs {
				frames <- inbound{payload: p}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-events:
			if err := s.handleTcellEvent(ev, cancel); err != nil {
				return err
			}
		case in := <-frames:
			if in.err != nil {
				if ctx.Err() != nil {
					return nil
				}
				fmt.Fprintf(os.Stderr, "tui: connection closed: %v\n", in.err)
				return nil
			}
			if s.dump != nil {
				s.dump.write("S->C", in.payload)
			}
			if name, ok := diag.NIControl(in.payload); ok {
				if name == "NI_PING" {
					if err := s.send([]byte("NI_PONG\x00")); err != nil {
						return fmt.Errorf("sending NI_PONG: %w", err)
					}
				}
				continue
			}
			drawn, err := s.handleFrame(in.payload)
			if err != nil {
				fmt.Fprintf(os.Stderr, "tui: frame skipped: %v\n", err)
				continue
			}
			if drawn && s.once {
				return nil
			}
		}
	}
}

// send NI-frames one payload, records it, and writes it to the server.
func (s *session) send(payload []byte) error {
	if s.dump != nil {
		s.dump.write("C->S", payload)
	}
	return writeFrame(s.conn, payload)
}

// handleFrame decodes one server frame, updates the chrome and the session
// state from it, answers it when it asks for something we answer on our own
// (the logon screen with --logon, a Control Framework call with a captured
// answer), and draws whatever screen it carries. It returns whether a screen
// was drawn.
func (s *session) handleFrame(payload []byte) (bool, error) {
	m, err := diag.ParseMessage(payload, false)
	if err != nil {
		return false, err
	}
	items := diag.ParseItems(m.Body)
	msgType, msg := s.chrome.update(items)
	s.stat = m.Header.ModeStat
	for _, it := range items {
		switch {
		case it.Type == diag.ItemSES:
			s.ses = append([]byte{}, it.Value...)
		case it.Type == diag.ItemAPPL && it.ID == 0x05 && it.SID == 0x01:
			s.dynn = append([]byte{}, it.Value...)
		case it.Type == diag.ItemAPPL && it.ID == 0x04 && it.SID == 0x26 && len(it.Value) == 4:
			s.counter = binary.BigEndian.Uint32(it.Value)
		}
	}
	// Keep the F-key resolution current from ANY frame that carries the
	// accelerator table (ST_R3INFO.13), not only a drawn screen — it is sent
	// once at session start (often on a status-only frame) and persists, so
	// parsing it here, before the screen/early-return logic below, is what makes
	// F-keys resolve on every later screen.
	if fk := diag.FKeyFuncs(items); len(fk) > 0 {
		s.fkeyFuncs = fk
		s.accelKeys = diag.AccelLabels(items)
		fmt.Fprintf(os.Stderr, "tui: accelerator table: %d F-keys bound (F8->fn#%d)\n", len(fk), fk[8])
	}
	// The GUI status (MNUENTRY) arrives on the frame that sets it and persists
	// across later frames that omit it, so keep the last one for the command
	// palette and the F-key-to-label join.
	if st := statusItems(items); len(st) > 0 {
		s.statusItems = st
	}

	if s.logon {
		if err := s.answerLogon(items); err != nil {
			fmt.Fprintf(os.Stderr, "tui: logon step: %v\n", err)
		}
	}
	if s.loggedOn && len(s.controls) > 0 && hasRFCTR(items, 0x00) {
		if _, err := s.answerControl(items, s.counter); err != nil {
			fmt.Fprintf(os.Stderr, "tui: control answer: %v\n", err)
		}
	}

	canvas, note, ok := s.chrome.frameCanvas(items)
	if !ok && isScreenFrame(items) {
		// A screen with no dynpro atoms — SAP Easy Access draws its tree in
		// a control we do not render — still has chrome, a title and an
		// OK-code field to type into, so it is drawn as an empty canvas.
		r, c := s.chrome.canvasSize()
		canvas, note, ok = tui.NewGrid(r, c), "no dynpro atoms (control screen)", true
	}
	if !ok {
		// A handshake or status-only frame: repaint the status bar so a
		// "saving…" or an error message still shows under the last chrome.
		if s.drewOnce && msg != "" {
			// A message-bearing frame with no new screen is the answer to our
			// PAI (an error/info that keeps the current screen), so the round
			// trip is over — clear pending, or the next Enter/key would be
			// refused as "previous answer still pending" forever.
			s.pending = false
			s.msgType, s.msg = msgType, msg
			if s.interactive && s.scr != nil {
				s.redraw()
			} else {
				s.drawStatusOnly(msgType, msg)
			}
		}
		return false, nil
	}
	s.items = items
	s.msgType, s.msg = msgType, msg
	s.pending = false
	s.logonSeen = isLogonScreen(items)
	if s.interactive {
		s.hasList = diag.HasListSegments(items)
		s.scr = newScreenState(items)
		s.redraw()
	} else {
		// A headless script still needs the screen state to shape a PAI with a
		// cursor and changed fields (an F-key rides the focused field's cursor).
		if s.scripted {
			s.hasList = diag.HasListSegments(items)
			s.scr = newScreenState(items)
		}
		s.draw(canvas, msgType, msg, note)
	}
	s.drewOnce = true
	if s.loggedOn && !s.logonSeen && !s.pending {
		if s.runOnce != "" {
			cmd := s.runOnce
			s.runOnce = ""
			if err := s.sendOKCode(cmd); err != nil {
				fmt.Fprintf(os.Stderr, "tui: --run %q: %v\n", cmd, err)
			}
		} else if s.scripted && len(s.steps) > 0 {
			step := s.steps[0]
			s.steps = s.steps[1:]
			if err := s.sendStep(step); err != nil {
				fmt.Fprintf(os.Stderr, "tui: script step %q: %v\n", step, err)
			}
		}
	}
	return true, nil
}

// isScreenFrame reports whether a frame that carries no dynpro atoms still
// presents a screen: a new window title, GUI status or dynpro descriptor.
func isScreenFrame(items []diag.Item) bool {
	for _, it := range items {
		switch {
		case it.Type == diag.ItemAPPL && it.ID == 0x0c && it.SID == 0x0a, // VARINFO.0a title
			it.Type == diag.ItemAPPL4 && it.ID == 0x0b && it.SID == 0x01, // MNUENTRY.01
			it.Type == diag.ItemAPPL && it.ID == 0x05 && it.SID == 0x01:  // DYNN.01
			return true
		}
	}
	return false
}

// handleKey applies one key to the screen, sending a PAI when it asks for one.
func (s *session) handleKey(k key, cancel context.CancelFunc) error {
	if s.scr == nil {
		if k.kind == keyCtrlC {
			cancel()
		}
		return nil
	}
	// A function key sends the OK-code it is bound to (--fkeys). The code a
	// given F-key fires is defined by the screen's GUI status and is not in
	// the data we can read, so the binding is the user's to state. The screen's
	// changed fields ride along, as they do for a pushbutton.
	// The menu bar: F10 opens it, then the arrows walk it. It is client-side
	// navigation and consumes keys until Enter fires an item or Esc closes.
	if s.menuOpen {
		s.menuKey(k, func() { cancel() })
		return nil
	}
	if k.kind == keyFunc && k.n == 10 {
		s.openMenu()
		s.redraw()
		return nil
	}
	if k.kind == keyCtrlP {
		s.palette = !s.palette
		if s.palette {
			s.palLines, s.palScroll = commandList(s.statusItems, s.accelKeys), 0
		}
		s.redraw()
		return nil
	}
	if s.palette {
		switch k.kind {
		case keyCtrlC:
			cancel()
		case keyEsc, keyCtrlP:
			s.palette = false
		case keyUp:
			s.palScroll--
		case keyDown:
			s.palScroll++
		case keyPgUp:
			s.palScroll -= 10
		case keyPgDn:
			s.palScroll += 10
		}
		s.redraw()
		return nil
	}
	// On a classic-list screen the paging keys scroll via the standard ABAP
	// list commands (P+ next, P- previous, P++ last, P-- first).
	if s.hasList {
		scroll := map[keyKind]string{keyPgDn: "P+", keyPgUp: "P-", keyCtrlEnd: "P++", keyCtrlHome: "P--"}
		if code := scroll[k.kind]; code != "" {
			if err := s.sendPAI(code, -1); err != nil {
				s.msgType, s.msg = 'E', "send: "+err.Error()
			}
			s.redraw()
			return nil
		}
	}
	if k.kind == keyFunc {
		s.fireFKey(k.n)
		s.redraw()
		return nil
	}
	act, okcode := s.scr.handleKey(k)
	switch act {
	case actQuit:
		cancel()
	case actRedraw:
		s.redraw()
	case actSend:
		// "F8" typed at the OK-code prompt fires that function key, so it works
		// in a terminal that steals the real F-keys (macOS media keys, Ctrl+P
		// for Print, …). Anything else is a genuine OK-code.
		if n, ok := fkeyStep(okcode); ok {
			s.fireFKey(n)
		} else if err := s.sendPAI(okcode, -1); err != nil {
			s.msgType, s.msg = 'E', "send: "+err.Error()
		}
		s.redraw()
	}
	return nil
}

// fireFKey fires function key n through the OK-code path, which the server
// acts on. In priority: an explicit --fkeys binding; else the code of an
// on-screen pushbutton this key maps to (okCodeForFKey). A function with no
// resolvable code (e.g. Execute, a toolbar-only function whose code is not on
// the wire) cannot be auto-fired — the accelerator table's UI_EVENT path needs
// a live control framework, which our replay-based session does not have — so
// the user is told to bind it with --fkeys.
func (s *session) fireFKey(n int) {
	code, ok := s.fkeys[n]
	if !ok {
		code, ok = s.okCodeForFKey(n)
	}
	if !ok {
		label := ""
		if fn, has := s.fkeyFuncs[n]; has {
			label = diag.FunctionLabels(s.statusItems)[fn]
		}
		if label != "" {
			s.msgType, s.msg = 'W', fmt.Sprintf("F%d (%s) has no code on this screen; bind it with --fkeys %d=CODE", n, label, n)
		} else {
			s.msgType, s.msg = 'W', fmt.Sprintf("F%d is not bound on this screen; --fkeys %d=CODE forces one", n, n)
		}
		return
	}
	fmt.Fprintf(os.Stderr, "tui: F%d -> OK-code %q\n", n, code)
	if err := s.sendPAI(code, -1); err != nil {
		s.msgType, s.msg = 'E', "send: "+err.Error()
	}
}

// sendPAI answers the screen on show with the user's edits and either an
// OK-code or a fired function number (uiEvent, -1 for none — the two are
// mutually exclusive). The logon screen is answered at most once per run,
// whoever fills it in: a second wrong password would count towards the user's
// lock.
func (s *session) sendPAI(okcode string, uiEvent int) error {
	if s.pending {
		return fmt.Errorf("previous answer still pending")
	}
	if s.logonSeen {
		if s.loggedOn {
			return fmt.Errorf("one logon attempt per run; restart to try again")
		}
		s.loggedOn = true
		// A logon submitted by hand goes through the proven logon-PAI builder
		// with the values the user typed, not the general builder — same frame
		// shape a real GUI sends (compress, only-changed fields, echoed counter).
		if s.template == nil {
			return fmt.Errorf("interactive logon needs --hello for the PAI env block (capture-free logon not built yet); reconnect with --hello captures/probe.jsonl")
		}
		out, err := buildLogonPAI(s.template, s.items, s.typedLogonCreds(), s.counter, s.compress)
		if err != nil {
			return err
		}
		if err := s.send(out); err != nil {
			return err
		}
		s.pending = true
		s.msgType, s.msg = 0, ""
		fmt.Fprintf(os.Stderr, "tui: logon PAI sent (typed, %d bytes)\n", len(out))
		return nil
	}
	if s.scr == nil {
		return s.sendOKCode(okcode)
	}
	if s.env == nil {
		return fmt.Errorf("no captured client frame to shape the PAI from")
	}
	out, err := buildPAI(s.env, paiInput{
		OKCode:  okcode,
		Changed: s.scr.changed(),
		Cursor:  s.scr.cursor(),
		UIEvent: uiEvent,
		SES:     s.ses,
		DYNN:    s.dynn,
		Counter: s.counter, // the counter is server-owned and pure-echoed by the client
		Stat:    s.stat,
	}, s.compress)
	if err != nil {
		return err
	}
	if err := s.send(out); err != nil {
		return err
	}
	s.pending = true
	s.msgType, s.msg = 0, ""
	what := "Enter"
	if uiEvent >= 0 {
		what = fmt.Sprintf("fn#%d", uiEvent)
	} else if okcode != "" {
		what = okcode
	}
	fmt.Fprintf(os.Stderr, "tui: PAI sent (%s, %d changed fields, %d bytes)\n", what, len(s.scr.changed()), len(out))
	return nil
}

// sendOKCode sends a PAI carrying only an OK-code (a function code or a system
// command like /nse38), with no changed fields — the scripting primitive. It
// does not need the interactive screen state, so it works in --logon batch runs
// too. The counter is the server's last value (pure-echoed), SES and DYNN the
// server's last.
func (s *session) sendOKCode(okcode string) error {
	if s.env == nil {
		return fmt.Errorf("no captured client frame to shape the PAI from")
	}
	out, err := buildPAI(s.env, paiInput{
		OKCode:  okcode,
		UIEvent: -1,
		SES:     s.ses,
		DYNN:    s.dynn,
		Counter: s.counter,
		Stat:    s.stat,
	}, s.compress)
	if err != nil {
		return err
	}
	if err := s.send(out); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "tui: OK-code %q sent (%d bytes)\n", okcode, len(out))
	return nil
}

// answerLogon answers the logon screen exactly once, shaping the PAI from the
// captured template and this frame's session id and counter.
func (s *session) answerLogon(items []diag.Item) error {
	if s.loggedOn || !isLogonScreen(items) {
		return nil
	}
	if s.template == nil {
		return fmt.Errorf("no captured logon PAI to shape the answer from")
	}
	// The GUI echoes the logon screen's counter unchanged in its logon PAI
	// (screen counter 1 -> PAI counter 1), it does not increment for the
	// logon submit, so echo counterOf(items) rather than +1.
	out, err := buildLogonPAI(s.template, items, s.creds, counterOf(items), s.compress)
	if err != nil {
		return err
	}
	if err := s.send(out); err != nil {
		return err
	}
	s.loggedOn = true
	s.pending = true
	fmt.Fprintln(os.Stderr, "tui: logon sent")
	return nil
}

// redraw paints the interactive screen: the atoms with the local edits, the
// focused field inverted, the terminal cursor on the caret; or the list.
func (s *session) redraw() {
	if s.scr == nil {
		return
	}
	rows, cols := s.chrome.canvasSize()
	var canvas *tui.Grid
	if s.hasList {
		canvas = tui.RenderList(diag.ParseListItems(s.items), rows, cols)
	} else {
		canvas = tui.Render(s.scr.overlay(), rows, cols)
		s.scr.tabHits = tui.DrawTabBar(canvas, s.scr.tabs, tabBarRow)
	}
	msgType, msg := s.msgType, s.msg
	if s.scr.inCmd {
		msgType, msg = 0, "command field — Enter/✓ sends  Esc cancels  (e.g. =STRT to Execute, /nse38)"
	} else if s.pending {
		msgType, msg = 0, "…"
	}
	note := s.scr.note()
	if s.menuOpen {
		note = "←/→ menus  ↑/↓ items  Enter select  Esc close"
	} else if len(s.statusItems) > 0 {
		note += "  F10 menu"
	}
	s.draw(canvas, msgType, msg, note)
}

// draw renders the frame. With chrome it composes the GUI window round the
// canvas and prints it in colour; plain, it prints the bare grid. Interactive,
// it also marks the focused field and places the terminal cursor on it.
func (s *session) draw(canvas *tui.Grid, msgType byte, msg, note string) {
	if s.interactive && s.scr != nil {
		s.chrome.command, s.chrome.commandActive = s.scr.cmd, s.scr.inCmd
	}
	if s.screen != nil {
		s.drawTcell(canvas, msgType, msg, note)
		return
	}
	rows, cols := terminalSize()
	if s.plain {
		if rows > 1 {
			canvas = canvas.Clip(rows-1, cols)
		}
		printClear(canvas.String() + "\n\x1b[7m " + s.bareStatus(note) + " \x1b[0m")
		return
	}
	if !s.interactive {
		note += "  q quits"
	}
	g := s.chrome.compose(canvas, msgType, msg, note, rows, cols)
	cr, cc := 0, 0
	if s.interactive && s.scr != nil && !s.hasList && !s.scr.inCmd {
		cr, cc = s.scr.markFocus(g, tui.ChromeRows-1)
	}
	if s.interactive && s.menuOpen {
		s.drawMenu(g)
		cr, cc = 0, 0
	}
	if s.interactive && s.palette {
		s.drawPalette(g)
		cr, cc = 0, 0
	}
	printClear(g.ANSI())
	if cr > 0 {
		fmt.Printf("\x1b[%d;%dH\x1b[?25h", cr, cc)
	} else if s.interactive {
		fmt.Print("\x1b[?25l")
	}
}

// drawStatusOnly repaints only the status bar under the current chrome, for a
// frame that carried a message but no new screen.
func (s *session) drawStatusOnly(msgType byte, msg string) {
	if s.plain {
		return
	}
	rows, cols := terminalSize()
	g := s.chrome.compose(tui.NewGrid(1, cols), msgType, msg, "q quits", rows, cols)
	printClear(g.ANSI())
}

func (s *session) bareStatus(note string) string {
	parts := []string{}
	if s.chrome.program != "" {
		parts = append(parts, "prog "+s.chrome.program)
	}
	mode := "read-only  q quits"
	if s.interactive {
		mode = "interactive"
	}
	parts = append(parts, note, mode)
	return strings.Join(parts, "  |  ")
}

// dumper records every frame both ways as tap-style JSON lines, so a live
// session can be decoded afterwards with cmd/lens.
type dumper struct {
	enc   *json.Encoder
	f     *os.File
	index int
}

func newDumper(path string) (*dumper, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &dumper{enc: json.NewEncoder(f), f: f}, nil
}

func (d *dumper) write(dir string, payload []byte) {
	d.enc.Encode(struct {
		At    time.Time `json:"at"`
		Dir   string    `json:"dir"`
		Conn  int       `json:"conn"`
		Index int       `json:"index"`
		Len   int       `json:"len"`
		Hex   string    `json:"hex"`
	}{time.Now(), dir, 1, d.index, len(payload), hex.EncodeToString(payload)})
	d.index++
}

func (d *dumper) close() { d.f.Close() }

// firstPaint tracks whether the screen has been cleared once this run.
var firstPaint = true

// printClear paints one frame without flicker or vertical jitter. It homes the
// cursor (a full \x1b[2J every frame flickers, and a trailing newline scrolls
// the viewport whenever a frame's height changes — that is the up/down jitter),
// clears each line to its end so a shorter line leaves no ghost, and clears
// everything below the last line so a shorter frame leaves no tail. No trailing
// newline is written, so the viewport never scrolls. The screen is fully
// cleared only once, on the first frame.
func printClear(s string) {
	var b strings.Builder
	if firstPaint {
		b.WriteString("\x1b[2J")
		firstPaint = false
	}
	b.WriteString("\x1b[H")
	for i, ln := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(ln)
		b.WriteString("\x1b[K") // clear to end of line
	}
	b.WriteString("\x1b[J") // clear everything below the last line
	fmt.Print(b.String())
}

// writeFrame NI-frames and sends one DIAG payload.
func writeFrame(conn net.Conn, payload []byte) error {
	fr, err := ni.EncodeFrame(payload)
	if err != nil {
		return err
	}
	_, err = conn.Write(fr)
	return err
}

// helloFromCapture reads the first C->S frame of a tap capture and returns its
// bytes, to be sent verbatim as the opening hello.
func helloFromCapture(path string) ([]byte, error) {
	frames, err := clientFrames(path, 1)
	if err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("%s: no C->S frame to use as a hello", path)
	}
	return frames[0], nil
}

// logonTemplate returns the capture's second client frame — the logon PAI a
// real GUI sent (SES echo, ST_USER.26 counter, the changed field atoms, the
// metrics XML) — as the shape our own logon answer is built from.
func logonTemplate(path string) ([]byte, error) {
	frames, err := clientFrames(path, 2)
	if err != nil {
		return nil, err
	}
	if len(frames) < 2 {
		return nil, fmt.Errorf("%s: no second C->S frame for a logon template", path)
	}
	return frames[1], nil
}

// clientFrames reads up to n C->S frames (NI keepalives skipped) from a tap
// capture, decoding the hex of each.
func clientFrames(path string, n int) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var out [][]byte
	for sc.Scan() && len(out) < n {
		var l struct {
			Dir string `json:"dir"`
			Hex string `json:"hex"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Dir != "C->S" {
			continue
		}
		data, err := hex.DecodeString(l.Hex)
		if err != nil {
			return nil, fmt.Errorf("decoding hex: %w", err)
		}
		if len(data) == 0 {
			continue
		}
		if _, ok := diag.NIControl(data); ok {
			continue
		}
		out = append(out, data)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// mustPong is the NI_PONG keepalive answer.
func mustPong() []byte {
	frame, err := ni.EncodeFrame([]byte("NI_PONG\x00"))
	if err != nil {
		panic(err)
	}
	return frame
}

// watchQuit ends the session when q is typed on standard input. It never sends
// anything to SAP; the byte read here only cancels the local context. On end
// of input (stdin is not a terminal, e.g. the demo piped) it stops watching but
// does NOT cancel, so a non-interactive run keeps going until Ctrl-C.
func watchQuit(ctx context.Context, cancel context.CancelFunc) {
	r := bufio.NewReader(os.Stdin)
	for {
		if ctx.Err() != nil {
			return
		}
		b, err := r.ReadByte()
		if err != nil {
			return // end of input: stop watching, leave the session running
		}
		if b == 'q' || b == 'Q' {
			cancel()
			return
		}
	}
}

// terminalSize asks the controlling terminal for its size in character cells,
// returning zeros when standard output is not a terminal.
func terminalSize() (rows, cols int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0, 0
	}
	return h, w
}
