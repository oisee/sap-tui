// Package demo is the animation engine shared by the DIAG server (cmd/server,
// which pushes the scenes to a real SAP GUI) and the terminal (cmd/tui, which
// draws them locally). Keeping the scenes here means there is one set of acts,
// not a copy per front end. A scene draws either a dynpro (a frame.Screen of
// widgets, the DIAG dynpro channel) or a list (positioned coloured runs, the
// classic-list channel); exactly one of Dynpro and List is set. Every scene is
// keyed to ts, the seconds elapsed inside it, so the motion runs at a fixed
// wall-clock speed whatever the frame cadence and a dropped frame never stutters.
package demo

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/oisee/sap-tui/internal/diag"
	"github.com/oisee/sap-tui/internal/frame"
)

// Scene is one act of the demo.
type Scene struct {
	Name, Approach string
	// Dur is the scene's length; 0 means use the caller's default. The login
	// opener needs longer than a beat, so it sets its own.
	Dur time.Duration
	// DurMul, when > 0, makes this scene run that multiple of the default
	// length (2 = twice as long), tracking the -scene-ms flag instead of a
	// fixed Dur. Ignored when Dur is set.
	DurMul float64
	// Bare drops the caption/footer for this scene, so the login opener can
	// look like a real logon screen and nothing else.
	Bare bool
	// Dynpro draws the scene as a dynpro; nil for a list scene.
	Dynpro func(ts float64, scr *frame.Screen)
	// List draws the scene as a classic-list frame; nil for a dynpro scene.
	List func(ts float64) []diag.ListSegment
	// Sound, when set, drives event-based audio: given the time span of one
	// frame (prevTs..ts within this scene), it returns the status-message type
	// whose GUI beep should sound (0 for silence) — a boom on a firework burst,
	// say. Natural and sparse: the beep marks a thing that happened, not a
	// metronome.
	Sound func(prevTs, ts float64) byte
}

// crossed reports whether an event that recurs every period seconds at the
// given offset landed inside (prevTs, ts].
func crossed(prevTs, ts, period, at float64) bool {
	return math.Floor((ts-at)/period) > math.Floor((prevTs-at)/period)
}

// fireworksSound sounds each rocket exactly on three visible moments, using the
// three distinct GUI beeps: a launch (S) at phase 0 as it leaves the ground, a
// boom (E) at phase 0.5 as it bursts, and a crackle (W) at phase 0.75 as the
// sparks spread and fall — for every one of the four rockets. No delay: the
// beep lands on the drawn frame, so it reads as tight sync. Rocket i's phase is
// mod(ts + i*0.9, period)/period.
func fireworksSound(prevTs, ts float64) byte {
	for i := 0; i < 4; i++ {
		period := 2.2 + float64(i)*0.5
		off := float64(i) * 0.9
		if crossed(prevTs, ts, period, -off) {
			return 'S' // launch, phase 0
		}
		if crossed(prevTs, ts, period, period/2-off) {
			return 'E' // burst, phase 0.5
		}
		if crossed(prevTs, ts, period, period*0.75-off) {
			return 'W' // crackle, phase 0.75
		}
	}
	return 0
}

// Scenes are the acts, ordered from the lightest frame to the heaviest so the
// contrast in bytes-per-frame is easy to feel. Nine draw dynpros; the three LED
// scenes draw in the list channel.
func Scenes() []Scene {
	led := func(eff int) func(float64) []diag.ListSegment {
		return func(ts float64) []diag.ListSegment { return LEDSegmentsEff(eff, ts) }
	}
	return []Scene{
		{Name: "login", Approach: "a login form that sits, drifts a square, orbits, then multiplies", Dur: 26 * time.Second, Bare: true, Dynpro: sceneLogin},
		{Name: "orbit", Approach: "3 widgets moved by coordinate, sized by depth", DurMul: 2, Dynpro: sceneOrbit},
		{Name: "tetra", Approach: "a wireframe tetrahedron tumbling fast", Dynpro: sceneTetra},
		{Name: "octa", Approach: "a wireframe octahedron spinning fast", Dynpro: sceneOcta},
		{Name: "solid", Approach: "a spinning cube whose edges are z-sorted BUTTONs — solid filled rectangles", Dynpro: sceneSolid},
		{Name: "equalizer", Approach: "a row of buttons whose Height is the graphics — bars", DurMul: 2, Dynpro: sceneEqualizer},
		{Name: "snake", Approach: "a label snake on a Lissajous path, with a fading trail", Dynpro: sceneSnake},
		{Name: "matrix", Approach: "sparse falling columns — the grid used lightly", Dynpro: sceneMatrix},
		{Name: "fireworks", Approach: "rockets that rise and burst into gravity-fed sparks", Dur: 9 * time.Second, Dynpro: sceneFireworks, Sound: fireworksSound},
		{Name: "helix", Approach: "a double helix twisting in place, strands and rungs", Dynpro: sceneHelix},
		{Name: "plasma", Approach: "LED plasma in the list channel — colour + letters", List: led(0)},
		{Name: "rings", Approach: "LED rings in the list channel", List: led(1)},
		{Name: "ball", Approach: "a bright ball bouncing on the LED field", List: led(2)},
		{Name: "starfield", Approach: "the whole character grid redrawn every frame", Dynpro: sceneStars},
		{Name: "icons", Approach: "a grid of real SAP icons, drawn via output fields", Dynpro: sceneIcons},
		{Name: "greetz", Approach: "the tornado made of greets — buttons grow taller, input fields spread wider, all spinning", Dynpro: sceneGreetz},
		{Name: "greetstorm", Approach: "the looping finale — a tornado of junk eases into a slow cylinder of greetings and back, forever", Dur: 80 * time.Second, Dynpro: sceneGreetstorm},
	}
}

// LEDEffectIndex is the effect index of an LED scene by name (0 plasma, 1 rings,
// 2 ball), or -1 for a non-LED scene.
func LEDEffectIndex(name string) int {
	switch name {
	case "plasma":
		return 0
	case "rings":
		return 1
	case "ball":
		return 2
	}
	return -1
}

// Centre pads text into width, centred; text at least as wide is returned as is.
func Centre(text string, width int) string {
	if len(text) >= width {
		return text
	}
	left := (width - len(text)) / 2
	right := width - len(text) - left
	return fmt.Sprintf("%*s%s%*s", left, "", text, right, "")
}

// Triangle bounces a value between 0 and span: it rises, hits the wall, and
// comes back, forever.
func Triangle(x float64, span int) int {
	if span <= 0 {
		return 0
	}
	p := math.Mod(x, float64(2*span))
	if p < 0 {
		p += float64(2 * span)
	}
	if p > float64(span) {
		p = float64(2*span) - p
	}
	return int(p)
}

// Clampi clamps v into [lo, hi].
func Clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// LoginFields places the four logon fields as loose elements at (top,left): the
// labels and inputs the way the real screen has them, so a copy that moves or
// multiplies is fields on the canvas. idx keeps each copy's field names distinct.
func LoginFields(scr *frame.Screen, top, left, idx int) {
	if top < 0 {
		top = 0
	}
	if left < 0 {
		left = 0
	}
	scr.Text(top+0, left+0, "Client")
	scr.Input(top+0, left+19, 3, fmt.Sprintf("MANDT%d", idx), "001")
	scr.Text(top+2, left+0, "User")
	scr.Input(top+2, left+19, 12, fmt.Sprintf("BNAME%d", idx), "")
	scr.Text(top+3, left+0, "Password")
	scr.InputHidden(top+3, left+19, 12, fmt.Sprintf("BCODE%d", idx), "")
	scr.Text(top+5, left+0, "Logon Language")
	scr.Input(top+5, left+19, 2, fmt.Sprintf("LANGU%d", idx), "EN")
}

// NativeLogon draws the logon screen the way the capture shows it: the four
// fields at their real rows and columns and the Information box to the right,
// its welcome lines output fields led by the @0S@ info icon. Placeholder values.
func NativeLogon(scr *frame.Screen) {
	LoginFields(scr, 0, 1, 0)
	scr.Frame(0, 35, 56, 19, "Information")
	scr.Output(1, 37, 53, "INFO0", "@0S@ ABAP Cloud Developer Trial 2023 initial shipment", false)
	scr.Output(3, 37, 53, "INFO1", "@0S@ Since ABAP Cloud Developer Trial is a free", false)
	scr.Output(4, 37, 53, "INFO2", "offering for education and demo purposes only,", false)
	scr.Output(5, 37, 53, "INFO3", "we offer it with SAP Community support. That", false)
	scr.Output(6, 37, 53, "INFO4", "means that no primary support is available", false)
	scr.Output(7, 37, 53, "INFO5", "for this product.", false)
}

// OrbitLogins draws count logon forms orbiting a centre at the given angle.
func OrbitLogins(scr *frame.Screen, ang float64, count int) {
	const cx, cy, rx, ry = 38.0, 9.0, 30.0, 6.0
	for i := 0; i < count; i++ {
		a := ang + float64(i)*(2.0*math.Pi/float64(count))
		left := int(cx + rx*math.Cos(a))
		top := int(cy + ry*math.Sin(a))
		LoginFields(scr, top, left, i)
	}
}

func sceneLogin(ts float64, scr *frame.Screen) {
	const homeTop, homeLeft = 4, 10
	const dx, dy = 44.0, 11.0
	switch {
	case ts < 6:
		NativeLogon(scr)
	case ts < 12:
		f := (ts - 6) / 6 * 4
		seg := int(f)
		fr := f - float64(seg)
		top, left := float64(homeTop), float64(homeLeft)
		switch seg {
		case 0:
			left = homeLeft + fr*dx
		case 1:
			left = homeLeft + dx
			top = homeTop + fr*dy
		case 2:
			left = homeLeft + (1-fr)*dx
			top = homeTop + dy
		default:
			top = homeTop + (1-fr)*dy
		}
		LoginFields(scr, int(top), int(left), 0)
	case ts < 18:
		OrbitLogins(scr, (ts-12)*1.4, 1)
	case ts < 22:
		OrbitLogins(scr, (ts-12)*1.4, 2)
	default:
		OrbitLogins(scr, (ts-12)*1.4, 3)
	}
}

func sceneOrbit(ts float64, scr *frame.Screen) {
	const cx, cy, rx, ry = 39.0, 11.0, 28.0, 8.0
	const angSpeed = 1.1
	labels := []string{"Go", "DIAG", "no ABAP"}
	for i, lab := range labels {
		ang := -ts*angSpeed + float64(i)*(2.0*math.Pi/3.0)
		col := int(cx + rx*math.Cos(ang))
		row := int(cy + ry*math.Sin(ang))
		depth := (math.Sin(ang) + 1.0) / 2.0
		w := 6 + int(depth*12.0)
		h := 1 + int(depth*2.0+0.5)
		scr.ButtonH(row, col, w, h, Centre(lab, w-2), fmt.Sprintf("=B%d", i))
	}
	scr.Text(int(cy), int(cx)-2, "( o )")
}

// spinWireframe rotates a solid's vertices on two axes, projects them in
// perspective, draws the edges as sparse dots and the vertices as glyphs — near
// vertices bright (O), far dim (o). Few objects (like orbit) so the frame stays
// light and the shape can spin fast without overrunning a real GUI. scale sizes
// the solid; sx/sy are the per-axis spin rates.
func spinWireframe(scr *frame.Screen, ts float64, verts [][3]float64, edges [][2]int, scale, sx, sy float64) {
	const cx, cy, d = 59.0, 12.0, 3.2
	rx, ry := ts*sx, ts*sy
	px := make([]int, len(verts))
	py := make([]int, len(verts))
	pz := make([]float64, len(verts))
	for i, v := range verts {
		x, y, z := v[0]*scale, v[1]*scale, v[2]*scale
		x, z = x*math.Cos(ry)-z*math.Sin(ry), x*math.Sin(ry)+z*math.Cos(ry) // yaw
		y, z = y*math.Cos(rx)-z*math.Sin(rx), y*math.Sin(rx)+z*math.Cos(rx) // pitch
		p := d / (z + d)
		px[i] = int(cx + x*p*20)
		py[i] = int(cy + y*p*9)
		pz[i] = z
	}
	for _, e := range edges { // edges as sparse dots (few atoms, keeps it light)
		a, b := e[0], e[1]
		for s := 1; s < 5; s++ {
			f := float64(s) / 5
			scr.Text(py[a]+int(float64(py[b]-py[a])*f), px[a]+int(float64(px[b]-px[a])*f), "·")
		}
	}
	for i := range verts {
		ch := "o"
		if pz[i] < 0 {
			ch = "O"
		}
		scr.Text(py[i], px[i], ch)
	}
}

// The wireframe solids: vertices and the edges that join them.
var (
	cubeVerts = [][3]float64{
		{-1, -1, -1}, {1, -1, -1}, {1, 1, -1}, {-1, 1, -1},
		{-1, -1, 1}, {1, -1, 1}, {1, 1, 1}, {-1, 1, 1},
	}
	cubeEdges = [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0}, {4, 5}, {5, 6}, {6, 7}, {7, 4},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
	}
	tetraVerts = [][3]float64{{1, 1, 1}, {1, -1, -1}, {-1, 1, -1}, {-1, -1, 1}}
	tetraEdges = [][2]int{{0, 1}, {0, 2}, {0, 3}, {1, 2}, {1, 3}, {2, 3}}
	octaVerts  = [][3]float64{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	octaEdges  = [][2]int{{0, 2}, {0, 3}, {0, 4}, {0, 5}, {1, 2}, {1, 3}, {1, 4}, {1, 5}, {2, 4}, {2, 5}, {3, 4}, {3, 5}}
)

func sceneCube(ts float64, scr *frame.Screen)  { spinWireframe(scr, ts, cubeVerts, cubeEdges, 1.0, 0.9, 1.3) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// edgeChar picks the line glyph for an edge by its screen slope: - flat, |
// steep, \ and / for the two diagonals.
func edgeChar(x1, y1, x2, y2 int) string {
	dx, dy := x2-x1, y2-y1
	ax, ay := abs(dx), abs(dy)
	switch {
	case ay*2 < ax:
		return "-"
	case ax*2 < ay:
		return "|"
	case (dx > 0) == (dy > 0):
		return "\\"
	default:
		return "/"
	}
}

// spinSolid rotates a solid and draws its corners as depth-scaled BUTTONs — a
// near corner a big 7x3 button, a far one a 1x1 dot, z-sorted so near sits on
// top — with the edges as directional / | \ - glyphs. Weight and depth without
// the muddy overlap of filled edge-boxes. scale sizes the solid; sx/sy the spin.
func spinSolid(scr *frame.Screen, ts float64, verts [][3]float64, edges [][2]int, scale, sx, sy float64) {
	const cx, cy, d = 59.0, 12.0, 3.2
	rx, ry := ts*sx, ts*sy
	n := len(verts)
	px := make([]int, n)
	py := make([]int, n)
	pz := make([]float64, n)
	for i, v := range verts {
		x, y, z := v[0]*scale, v[1]*scale, v[2]*scale
		x, z = x*math.Cos(ry)-z*math.Sin(ry), x*math.Sin(ry)+z*math.Cos(ry)
		y, z = y*math.Cos(rx)-z*math.Sin(rx), y*math.Sin(rx)+z*math.Cos(rx)
		p := d / (z + d)
		px[i] = int(cx + x*p*20)
		py[i] = int(cy + y*p*9)
		pz[i] = z
	}
	for _, e := range edges { // edges: sparse directional glyphs
		a, b := e[0], e[1]
		ch := edgeChar(px[a], py[a], px[b], py[b])
		for s := 1; s < 6; s++ {
			f := float64(s) / 6
			scr.Text(py[a]+int(float64(py[b]-py[a])*f), px[a]+int(float64(px[b]-px[a])*f), ch)
		}
	}
	order := make([]int, n) // corners: depth-scaled buttons, far-to-near
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return pz[order[i]] > pz[order[j]] })
	for _, i := range order {
		depth := Clampi(int((1-(pz[i]+2)/4)*100), 0, 100)
		w := 1 + depth*6/100 // 1..7
		h := 1 + depth*2/100 // 1..3
		scr.ButtonH(py[i]-h/2, px[i]-w/2, w, h, "", fmt.Sprintf("=S%d", i))
	}
}

func sceneSolid(ts float64, scr *frame.Screen) {
	spinSolid(scr, ts, cubeVerts, cubeEdges, 1.0, 0.9, 1.3)
}
func sceneTetra(ts float64, scr *frame.Screen) { spinSolid(scr, ts, tetraVerts, tetraEdges, 1.3, 1.4, 1.8) }
func sceneOcta(ts float64, scr *frame.Screen)  { spinSolid(scr, ts, octaVerts, octaEdges, 1.6, 1.7, 1.1) }

// sceneTornado swirls a funnel of mixed SAP widgets — buttons, icons, input
// fields and labels — around a vertical axis: the radius is wide at the top and
// narrows toward the bottom, each row is twisted a little more than the one
// below and the whole column sways, so the widgets spiral like a tornado. Items
// are drawn back-to-front and the near ones are drawn wider.
func sceneTornado(ts float64, scr *frame.Screen) {
	const (
		cx, centerRow = 59.0, 13.0
		rows, perRing = 17, 2
		vAspect       = 0.40 // a character cell is ~2.5x taller than wide
		camDist       = 120.0
		tiltMax       = 1.15 // ~66°: the highest the camera pitches up
	)
	icons := []string{"@0S@", "@0Y@", "@0Z@", "@10@", "@08@", "@09@", "@0A@"}
	labels := []string{"Go", "DIAG", "SAP", "no ABAP", "odgp", "R/3"}

	// The camera pitches from a pure side view (tilt 0) up to a high top-side
	// view and back, period ~20s. So the swirl reads first as widgets running
	// left-right, then — as the camera rises — as them travelling on perspective
	// ellipses around the axis, the way you'd see a real vortex from above.
	tilt := tiltMax * 0.5 * (1 - math.Cos(ts*0.32))
	sinP, cosP := math.Sin(tilt), math.Cos(tilt)

	type item struct {
		row, col, kind, k int
		depth             float64
	}
	var items []item
	for r := 0; r < rows; r++ {
		hf := float64(r) / float64(rows)            // 0 top .. 1 bottom
		radius := 6.0 + (1-hf)*30.0                 // wide at top, tight at the base
		sway := math.Sin(ts*2.6+float64(r)*0.4) * 6 // the column leans and whips
		// Increasing angle spins counter-clockwise seen from above — the way a
		// northern-hemisphere cyclone turns.
		base := ts*4.8 + float64(r)*0.75
		py := (8.0 - float64(r)) * 2.4 // ring height on the axis: top +, base -
		for k := 0; k < perRing; k++ {
			a := base + float64(k)*math.Pi
			px := radius * math.Cos(a) // across the ring
			pz := radius * math.Sin(a) // depth within the ring's own plane
			// Pitch the whole ring about the horizontal axis by the camera tilt.
			yUp := py*cosP + pz*sinP
			zDepth := pz*cosP - py*sinP        // + is toward the viewer
			p := camDist / (camDist - zDepth)  // perspective: near is bigger
			col := Clampi(int(cx+sway+px*p), 1, 116)
			row := Clampi(int(centerRow-yUp*p*vAspect), 1, 25)
			dN := zDepth / 45.0
			if dN > 1 {
				dN = 1
			} else if dN < -1 {
				dN = -1
			}
			items = append(items, item{row, col, (r + k) % 4, r*perRing + k, dN})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].depth < items[j].depth }) // back first
	for _, it := range items {
		switch it.kind {
		case 0: // button, wider up close
			w := 4
			if it.depth > 0 {
				w = 8
			}
			scr.ButtonH(it.row, Clampi(it.col-w/2, 1, 116), w, 1, "", fmt.Sprintf("=T%d", it.k))
		case 1: // an icon
			scr.Output(it.row, it.col, 4, fmt.Sprintf("TI%d", it.k), icons[it.k%len(icons)], false)
		case 2: // an input field — a white bar whose length grows up close and
			// shrinks into the distance, so the field width is the graphics. A
			// sharp specular term flashes it wider right as it swings to the
			// front (depth -> 1): the white fields become the glints.
			w := 3 + int((it.depth+1)*3.5) // depth -1..1 -> width 3..10
			if it.depth > 0 {
				w += int(math.Pow(it.depth, 6) * 6) // a glint at the near face only
			}
			scr.Input(it.row, Clampi(it.col-w/2, 1, 116), w, fmt.Sprintf("TF%d", it.k), "")
		default: // a label
			scr.Text(it.row, it.col, labels[it.k%len(labels)])
		}
	}
}

func clampf(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// sceneGreetstorm is the looping finale: a tornado of flying junk that eases into
// a slow cylinder of greetings and back again, forever. One morph value m
// ping-pongs 0..1 (0 = tornado, 1 = greets) and every parameter rides it — the
// funnel straightens into a cylinder, the spin slows, the camera stops pitching
// and parks near the side, the flying junk thins out and the greeting names grow
// in. Reverse, and the junk floods back and the names fade — a breathing storm.
func sceneGreetstorm(ts float64, scr *frame.Screen) {
	const (
		cx, centerRow      = 59.0, 13.0
		rows, perRing      = 17, 2
		vAspect, camDist   = 0.40, 120.0
		spinFast, spinSlow = 4.5, 1.1
	)
	icons := []string{"@0S@", "@0Y@", "@0Z@", "@10@", "@08@", "@09@", "@0A@"}
	labels := []string{"Go", "DIAG", "SAP", "no ABAP", "odgp", "R/3"}

	// The loop has four phases: a tornado, a ramp into the greetings, a long
	// plateau where the cylinder shows two greets at a time and swaps them one by
	// one, then a ramp back to the tornado — once every greet has passed.
	n := len(greetNames)
	const dTor, dRamp, hold, youBeats = 5.0, 6.0, 1.0, 3
	dPlat := float64(n+youBeats) * hold // the greets, then a few beats of YOU!!
	cycle := dTor + 2*dRamp + dPlat
	tc := math.Mod(ts, cycle)
	cycles := math.Floor(ts / cycle)
	var m float64
	step := 0 // which swap we are on within the plateau
	switch {
	case tc < dTor:
		m = 0
	case tc < dTor+dRamp:
		m = smoothstep((tc - dTor) / dRamp)
	case tc < dTor+dRamp+dPlat:
		m = 1
		step = int((tc - dTor - dRamp) / hold)
	default:
		m = smoothstep((cycle - tc) / dRamp)
		step = n // all greets shown — the finale
	}
	// After every greet has passed, the whole cylinder turns to YOU!! at once.
	finale := step >= n
	// The two greets on show: groups A and B take turns advancing, so one name
	// swaps at a time while the other holds.
	aIdx := (2 * ((step + 1) / 2)) % n
	bIdx := (2*(step/2) + 1) % n
	// Spin angle: fast in the tornado, slow on the plateau, integrated per phase
	// (ramps at the mean rate) so it never jumps, even across the loop seam.
	midRate := (spinFast + spinSlow) / 2
	perCycle := spinFast*dTor + midRate*dRamp + spinSlow*dPlat + midRate*dRamp
	var partial float64
	switch {
	case tc < dTor:
		partial = spinFast * tc
	case tc < dTor+dRamp:
		partial = spinFast*dTor + midRate*(tc-dTor)
	case tc < dTor+dRamp+dPlat:
		partial = spinFast*dTor + midRate*dRamp + spinSlow*(tc-dTor-dRamp)
	default:
		partial = spinFast*dTor + midRate*dRamp + spinSlow*dPlat + midRate*(tc-dTor-dRamp-dPlat)
	}
	base0 := cycles*perCycle + partial
	// Tilt: the tornado's rising-falling pitch, easing to a fixed ~5° side view.
	tTorn := 1.15 * 0.5 * (1 - math.Cos(ts*0.32))
	tilt := tTorn*(1-m) + 0.087*m
	sinP, cosP := math.Sin(tilt), math.Cos(tilt)

	type item struct {
		row, col, kind, idx int
		depth, nf           float64
		greet               bool
	}
	var items []item
	for r := 0; r < rows; r++ {
		hf := float64(r) / float64(rows)
		radius := (6.0+(1-hf)*30.0)*(1-m) + 24.0*m // funnel -> cylinder
		swayAmp := 6.0*(1-m) + 2.0*m
		swayFreq := 2.6*(1-m) + 1.3*m
		sway := math.Sin(ts*swayFreq+float64(r)*0.4) * swayAmp
		// Angle offset per height: tornado's descending ramp (a spiral) eases into
		// a scattered permutation (names populate the cylinder, no vortex).
		angOff := (float64(r)*0.75)*(1-m) + (float64((r*5)%rows)/float64(rows)*2*math.Pi)*m
		py := (8.0 - float64(r)) * 2.4
		for k := 0; k < perRing; k++ {
			a := base0 + angOff + float64(k)*math.Pi
			px := radius * math.Cos(a)
			pz := radius * math.Sin(a)
			yUp := py*cosP + pz*sinP
			zDepth := pz*cosP - py*sinP
			p := camDist / (camDist - zDepth)
			col := Clampi(int(cx+sway+px*p), 1, 116)
			row := Clampi(int(centerRow-yUp*p*vAspect), 1, 25)
			idx := r*perRing + k
			items = append(items, item{row, col, idx % 4, idx, clampf(zDepth/45.0, -1, 1), 0, idx%3 == 0})
		}
	}
	for i := range items {
		items[i].nf = (items[i].depth + 1) / 2
	}
	sort.Slice(items, func(i, j int) bool { return items[i].depth < items[j].depth }) // back first
	for _, it := range items {
		if it.greet {
			// A greeting: the name grows in with m, letter-spaced by depth.
			gap := 0
			if it.nf > 0.55 && m > 0.6 {
				gap = 1
			}
			if it.nf > 0.85 && m > 0.8 {
				gap = 2
			}
			text := ""
			if m > 0.35 {
				if finale {
					// Every plate turns to YOU!! together for the send-off.
					text = spaceOut("YOU!!", gap)
				} else {
					// Half the plates carry greet A, half greet B — each in a
					// rotating case (UPPER / Camel / ScEnE), swapping one at a time.
					name, style := greetNames[aIdx], aIdx%3
					if (it.idx/3)%2 == 1 {
						name, style = greetNames[bIdx], bIdx%3
					}
					text = spaceOut(stylize(name, style), gap)
				}
			}
			if it.idx%2 == 0 {
				h := 1 + int(it.nf*2.2*m)
				w := len(text) + 2
				if w < 4 {
					w = 4
				}
				scr.ButtonH(it.row, Clampi(it.col-w/2, 1, 116), w, h, text, fmt.Sprintf("=G%d", it.idx))
			} else {
				w := len(text) + 1
				if w < 3 {
					w = 3
				}
				scr.Input(it.row, Clampi(it.col-w/2, 1, 116), w, fmt.Sprintf("GF%d", it.idx), text)
			}
			continue
		}
		// Junk: it thins out as m rises past a staggered threshold, floods back as
		// m falls — the swarm dissolving into, and reforming from, the storm.
		thr := 0.15 + 0.6*float64((it.idx*7)%10)/10.0
		if m > thr {
			continue
		}
		switch it.kind {
		case 0:
			w := 4
			if it.depth > 0 {
				w = 8
			}
			scr.ButtonH(it.row, Clampi(it.col-w/2, 1, 116), w, 1, "", fmt.Sprintf("=T%d", it.idx))
		case 1:
			scr.Output(it.row, it.col, 4, fmt.Sprintf("TI%d", it.idx), icons[it.idx%len(icons)], false)
		case 2:
			w := 3 + int((it.depth+1)*3.5)
			scr.Input(it.row, Clampi(it.col-w/2, 1, 116), w, fmt.Sprintf("TF%d", it.idx), "")
		default:
			scr.Text(it.row, it.col, labels[it.idx%len(labels)])
		}
	}
}

// sceneGreetz is the tornado, made of greetings: the same spinning funnel (rings
// around a vertical axis, camera tilt, perspective, CCW), but every widget in the
// vortex carries a greet — buttons that grow TALLER up close and input fields
// whose text spreads WIDER (letter-spacing) up close. Vertical size is the
// button height, horizontal size is the spaces between the letters.
func sceneGreetz(ts float64, scr *frame.Screen) {
	const (
		cx, centerRow = 59.0, 13.0
		rows, perRing = 11, 1
		vAspect       = 0.40
		camDist       = 120.0
		tilt          = 0.087 // fixed ~5° above the side view; no up-down camera motion
	)
	// A straight cylinder seen almost edge-on: the camera is parked at a steady
	// ~5° above the equator, so the depth order never shifts and the z-sort
	// (near widgets drawn last, over the far ones) stays clean.
	sinP, cosP := math.Sin(tilt), math.Cos(tilt)

	type item struct {
		row, col, kind, k int
		depth             float64
	}
	var items []item
	for r := 0; r < rows; r++ {
		radius := 24.0                              // constant radius — a cylinder, not a funnel
		sway := math.Sin(ts*1.3+float64(r)*0.4) * 2 // a gentle breathing lean
		// Spin slowly, and scatter each height's angle around the circle (a
		// permutation, not a ramp) so the names populate the cylinder surface
		// instead of spiralling down it like a vortex.
		base := ts*1.2 + float64((r*5)%rows)/float64(rows)*2*math.Pi
		py := (float64(rows-1)/2 - float64(r)) * 2.6 // height on the axis, centred
		for k := 0; k < perRing; k++ {
			a := base + float64(k)*math.Pi
			px := radius * math.Cos(a)
			pz := radius * math.Sin(a)
			yUp := py*cosP + pz*sinP
			zDepth := pz*cosP - py*sinP
			p := camDist / (camDist - zDepth)
			col := Clampi(int(cx+sway+px*p), 1, 116)
			row := Clampi(int(centerRow-yUp*p*vAspect), 1, 25)
			dN := zDepth / 45.0
			if dN > 1 {
				dN = 1
			} else if dN < -1 {
				dN = -1
			}
			items = append(items, item{row, col, (r + k) % 2, r*perRing + k, dN})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].depth < items[j].depth }) // back first
	for _, it := range items {
		name := greetNames[it.k%len(greetNames)]
		nf := (it.depth + 1) / 2 // 0 far .. 1 near
		gap := 0                 // horizontal growth: spaces between the letters
		if nf > 0.55 {
			gap = 1
		}
		if nf > 0.85 {
			gap = 2
		}
		text := spaceOut(name, gap)
		switch it.kind {
		case 0: // a pushbutton that grows taller up close (vertical size)
			h := 1 + int(nf*2.2) // 1 .. 3 rows
			w := len(text) + 2
			scr.ButtonH(it.row, Clampi(it.col-w/2, 1, 116), w, h, text, fmt.Sprintf("=G%d", it.k))
		default: // an input field carrying the greet, wider up close
			w := len(text) + 1
			scr.Input(it.row, Clampi(it.col-w/2, 1, 116), w, fmt.Sprintf("GF%d", it.k), text)
		}
	}
}

func sceneEqualizer(ts float64, scr *frame.Screen) {
	const bars, baseRow = 12, 20
	for i := 0; i < bars; i++ {
		amp := (math.Sin(ts*3.0+float64(i)*0.5) + 1.0) / 2.0
		h := 1 + int(amp*10.0)
		col := 6 + i*6
		scr.ButtonH(baseRow-h, col, 4, h, "", fmt.Sprintf("=EQ%d", i))
	}
	for c := 4; c < 6+bars*6; c++ {
		scr.Text(baseRow, c, "-")
	}
}

func sceneSnake(ts float64, scr *frame.Screen) {
	const cx, cy, rx, ry = 39.0, 10.0, 30.0, 8.0
	const seg = 16
	for k := 0; k < seg; k++ {
		tt := ts - float64(k)*0.05
		col := int(cx + rx*math.Sin(tt*1.7))
		row := int(cy + ry*math.Sin(tt*2.3))
		ch := "O"
		switch {
		case k > 10:
			ch = "."
		case k > 4:
			ch = "o"
		}
		scr.Text(row, col, ch)
	}
}

func sceneMatrix(ts float64, scr *frame.Screen) {
	const w, h = 78, 20
	const glyphs = "01<>[]{}=+*/\\ABCDEF$#@abcdef"
	for x := 0; x < w; x += 3 {
		speed := 6.0 + float64((x*37)%11)
		off := float64((x * 13) % 23)
		head := int(math.Mod(ts*speed+off, float64(h+8)))
		for t := 0; t < 5; t++ {
			y := head - t
			if y < 0 || y >= h {
				continue
			}
			g := glyphs[(x+y*7+int(ts*10.0))%len(glyphs)]
			scr.Text(1+y, 1+x, string(g))
		}
	}
}

// sceneGreetings is the greets drum: a carousel of names revolving around a
// vertical axis (the tornado's projection), near ones big and letter-spaced,
// swinging in from the left, filling the centre, then shrinking away to the
// right and hiding round the back. Each character's spacing scales with depth,
// so a name zooms as it turns to face the viewer.
// greetNames are the shout-outs — the greets from the vivid-vibes outro credits
// (oisee/vivid-vibes, build_demo_outro). Shuffled at startup (init below) with
// Lars pinned first.
var greetNames = []string{
	"Lars Hvam Petersen",
	"Scott Hanselman", "Paul Modderman", "Jelena Perfiljeva", "Fred Huet",
	"Holger Bruchelt", "Dr. Philip Herzig", "Level 9", "Infocom", "Amit Lal",
	"Prof. Dr. Alexander Zeier", "Marian Zeis", "Anthropic", "Volker Buzek",
	"Camunda", "Filipp G.", "Claude", "Parazite", "Bizhuka", "Sq", "Kq",
	"Thomas Jung", "Enno Wulff", "HallycinoJen", "KiM", "Marcello Urbani",
	"Martin Pankraz", "Emma Qian", "Florian Farr", "S. Novikov", "Megus",
	"SAP", "Devraj Bardhan", "IBM", "Random/CC", "Nora von Thenen", "TSL",
	"Mistral", "Ivan Pirog", "Nik-O", "G_D", "JtN", "CyberJack", "4D",
	"Triebkraft", "Stardust", "Gasman", "BaZe", "Nova", "Aki", "Arwel Owen",
	"Edgar Martinez", "DJ Adams", "Michael Keller", "Dirk Roeckmann", "3SC",
	"K3L", "Robin van het Hof", "Yurii Sychov", "Aλex Nihirash", "Introspec",
}

// Shuffle the greets at startup so the roll differs run to run, but keep Lars
// Hvam Petersen (index 0) at the front.
func init() {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(greetNames)-1, func(i, j int) {
		greetNames[i+1], greetNames[j+1] = greetNames[j+1], greetNames[i+1]
	})
}

// stylize renders a greet in one of three cases, so the same name reappears
// dressed differently: UPPERCASE, CamelCase, and ScEnE-case (irregular studly
// caps).
func stylize(s string, style int) string {
	switch ((style % 3) + 3) % 3 {
	case 1: // CamelCase
		parts := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '-' || r == '_' })
		for i, p := range parts {
			if p != "" {
				parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
			}
		}
		return strings.Join(parts, "")
	case 2: // ScEnE-case: irregular alternating caps
		b := []byte(strings.ToLower(s))
		for i := range b {
			if b[i] >= 'a' && b[i] <= 'z' && (i*7+int(b[i]))%2 == 0 {
				b[i] -= 32
			}
		}
		return string(b)
	default: // UPPERCASE
		return strings.ToUpper(s)
	}
}

// smoothstep eases 0..1 with zero slope at both ends.
func smoothstep(x float64) float64 {
	x = clampf(x, 0, 1)
	return x * x * (3 - 2*x)
}

// spaceOut inserts gap spaces between every character, so letter-spacing lives
// inside a single string (one atom) rather than one atom per character. It walks
// runes, so multi-byte names (e.g. "Aλex") stay intact.
func spaceOut(s string, gap int) string {
	if gap <= 0 {
		return s
	}
	sep := strings.Repeat(" ", gap)
	var b strings.Builder
	first := true
	for _, r := range s {
		if !first {
			b.WriteString(sep)
		}
		first = false
		b.WriteRune(r)
	}
	return b.String()
}

// greetGap is the whole-name letter-spacing (spaces between letters) by depth,
// in integer bands so it never jitters: wide at the front, tight round the side.
func greetGap(depth float64) int {
	switch {
	case depth > 0.85:
		return 3
	case depth > 0.55:
		return 2
	case depth > 0.30:
		return 1
	default:
		return 0
	}
}

func sceneGreetings(ts float64, scr *frame.Screen) {
	const cx, midRow, rvert = 59.0, 11.0, 8.0
	n := len(greetNames)
	base := ts * 1.6 // drum rotation — faster vertical roll
	scr.Text(1, 43, "= = =   O D G P   G R E E T S   = = =")
	scr.Text(22, 40, "respect to everyone who bent a SAP GUI")

	type spun struct {
		i     int
		depth float64
	}
	order := make([]spun, n)
	for i := range order {
		a := base + float64(i)*2*math.Pi/float64(n)
		order[i] = spun{i, math.Cos(a)} // depth: front > 0
	}
	sort.Slice(order, func(a, b int) bool { return order[a].depth < order[b].depth }) // back first
	for _, o := range order {
		if o.depth <= 0.05 { // hidden round the back of the drum
			continue
		}
		a := base + float64(o.i)*2*math.Pi/float64(n)
		// Each name is ONE label: it rolls vertically over the cylinder (its own
		// row), letter-spaced by depth, and sways on X by a sine so the drum feels
		// alive. One atom per name keeps the frame light and the GUI fast.
		row := midRow - math.Sin(a)*rvert
		sway := math.Sin(ts*1.5+a) * 5.0 // the column waves side to side
		text := spaceOut(greetNames[o.i], greetGap(o.depth))
		col := cx + sway - float64(len(text))/2
		scr.Text(int(row+0.5), int(col+0.5), text)
	}
}

func sceneIcons(ts float64, scr *frame.Screen) {
	scr.Output(1, 2, 52, "IHDR", "@0S@ SAP icons drawn by Go via output fields", false)
	const cols, count = 12, 48
	sweep := int(ts*8.0) % count
	for code := 0; code < count; code++ {
		r := 3 + (code/cols)*3
		c := 4 + (code%cols)*8
		scr.Output(r, c, 4, fmt.Sprintf("IC%02X", code), fmt.Sprintf("@%02X@", code), false)
		label := fmt.Sprintf("%02X", code)
		if code == sweep {
			label = ">" + label
		}
		scr.Text(r+1, c, label)
	}
}

func sceneStars(ts float64, scr *frame.Screen) {
	const w, h = 78, 18
	banner := "  OPEN-DIAG-GO-PRO  ***  the whole character grid, redrawn  ***  driven by Go  "
	off := int(ts * 12.0)
	line := make([]byte, w)
	for i := 0; i < w; i++ {
		line[i] = banner[(off+i)%len(banner)]
	}
	scr.Text(1, 1, string(line))
	for x := 0; x < w; x++ {
		y := h/2 + int(float64(h/2-1)*math.Sin(float64(x)/6.0+ts*2.0))
		if y >= 0 && y < h {
			scr.Text(3+y, 1+x, "*")
		}
	}
}

func sceneFireworks(ts float64, scr *frame.Screen) {
	const w, h = 116, 21
	sparks := "*+.o"
	for i := 0; i < 4; i++ {
		period := 2.2 + float64(i)*0.5
		phase := math.Mod(ts+float64(i)*0.9, period) / period
		launchX := 16 + i*28
		peakY := 2 + (i%3)*2
		if phase < 0.5 {
			f := phase / 0.5
			y := h - 1 - int(f*float64(h-1-peakY))
			scr.Text(y, launchX, "|")
			if f > 0.6 {
				scr.Text(Clampi(y-1, 0, h-1), launchX, "^")
			}
		} else {
			f := (phase - 0.5) / 0.5
			const n = 14
			for s := 0; s < n; s++ {
				a := float64(s) / float64(n) * 2 * math.Pi
				rad := f * 11
				col := launchX + int(rad*2*math.Cos(a))
				row := peakY + int(rad*math.Sin(a)+f*f*7)
				if row >= 0 && row < h && col >= 0 && col < w {
					scr.Text(row, col, string(sparks[(s+int(ts*5))%len(sparks)]))
				}
			}
		}
	}
}

func sceneHelix(ts float64, scr *frame.Screen) {
	const rows, cx, amp = 21, 59, 34
	for r := 0; r < rows; r++ {
		a := float64(r)*0.5 + ts*2.2
		x1 := cx + int(float64(amp)*math.Sin(a))
		x2 := cx + int(float64(amp)*math.Sin(a+math.Pi))
		if r%2 == 0 {
			lo, hi := x1, x2
			if lo > hi {
				lo, hi = hi, lo
			}
			// One run per rung, not one atom per character: a per-char rung made
			// this the heaviest scene (~9 KB/frame) and overran the real GUI
			// (KNOWLEDGE §9); as a single run it is a few hundred bytes.
			if hi-lo > 1 {
				scr.Text(1+r, lo+1, strings.Repeat("-", hi-lo-1))
			}
		}
		ch1, ch2 := "o", "O"
		if math.Cos(a) >= 0 {
			ch1, ch2 = "O", "o"
		}
		scr.Text(1+r, x1, ch1)
		scr.Text(1+r, x2, ch2)
	}
}
