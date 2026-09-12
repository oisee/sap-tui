package demo

import (
	"fmt"
	"math"
	"strings"

	"github.com/oisee/sap-tui/internal/diag"
)

// The LED display: a small logical grid of coloured cells drawn in the classic
// list channel, each cell a block of characters whose colour and density glyph
// come from an effect field. It is run-length encoded per row so the big grid
// stays a few list runs (the list channel stalls on hundreds).

// ledRamp is the density ramp, light to dark, made of letters (plus a space and
// two dots for the lightest steps): a denser letter darkens the cell.
const ledRamp = " .:iclosnuaewmyqpdbkhOQMWNB"

// ledSpectrum is the vivid list colours ordered as a spectrum.
var ledSpectrum = []byte{diag.ColKey, diag.ColHeading, diag.ColPositive, diag.ColTotal, diag.ColGroup, diag.ColNegative}

// The LED grid is logical: LEDRows x LEDCols cells, each drawn as a bw x bh
// block of character cells, so the display is big and chunky while the run
// count stays tied to this logical resolution.
const LEDRows, LEDCols = 10, 22

// ledEffectNames names the three effects, indexed by effect number.
var ledEffectNames = []string{"plasma", "rings", "ball"}

// triF bounces a float value between 0 and span, like Triangle but continuous,
// so a ball's centre moves at sub-cell precision.
func triF(x, span float64) float64 {
	if span <= 0 {
		return 0
	}
	p := math.Mod(x, 2*span)
	if p < 0 {
		p += 2 * span
	}
	if p > span {
		p = 2*span - p
	}
	return p
}

// ballCell renders one LED of a bouncing ball with sub-pixel anti-aliasing: the
// ball is a circle at a float position and radius; each logical cell samples a
// 3x3 sub-grid and its glyph density is the fraction of samples inside the
// circle, so the edge is a soft ramp and the ball stays round and morphs
// smoothly as it drifts between cells (rather than a blocky hard circle).
func ballCell(lr, lc int, t float64) (byte, byte) {
	bx := triF(t*2.6, float64(LEDCols-1))
	by := triF(t*1.7, float64(LEDRows-1))
	const radius, n = 3.2, 3
	inside := 0
	for sy := 0; sy < n; sy++ {
		for sx := 0; sx < n; sx++ {
			x := float64(lc) + (float64(sx)+0.5)/n - 0.5
			y := float64(lr) + (float64(sy)+0.5)/n - 0.5
			if math.Hypot(x-bx, (y-by)*2) < radius { // *2 corrects the cell aspect
				inside++
			}
		}
	}
	cov := float64(inside) / float64(n*n)
	if cov == 0 {
		return diag.ColKey, ledRamp[0] // dark field
	}
	di := Clampi(int(cov*float64(len(ledRamp)-1)+0.5), 1, len(ledRamp)-1)
	var col byte = diag.ColNegative // bright core
	if cov < 0.55 {
		col = diag.ColTotal // dimmer, anti-aliased rim
	}
	return col, ledRamp[di]
}

// ledCell is the colour and density glyph for one logical LED, for the current
// effect at time t.
func ledCell(lr, lc int, t float64, eff int) (byte, byte) {
	switch eff {
	case 1: // concentric rings breathing out from the centre
		cx, cy := float64(LEDCols)/2, float64(LEDRows)/2
		d := math.Hypot(float64(lc)-cx, (float64(lr)-cy)*2)
		v := (math.Sin(d/2.2-t*2.0) + 1.0) / 2.0
		gi := Clampi(int(v*float64(len(ledSpectrum))), 0, len(ledSpectrum)-1)
		di := Clampi(int(v*float64(len(ledRamp))), 0, len(ledRamp)-1)
		return ledSpectrum[gi], ledRamp[di]
	case 2: // a bright ball bouncing on a dark field, anti-aliased
		return ballCell(lr, lc, t)
	default: // plasma: hue and luminance from two sine fields
		fr, fc := float64(lr), float64(lc)
		hv := (math.Sin(fc/3.0+t) + math.Sin(fr/2.0-t) + math.Sin((fc+fr)/4.0+t*1.3) + 3.0) / 6.0
		lv := (math.Sin(fc/2.5-t*0.7) + math.Cos(fr/3.0+t*0.9) + 2.0) / 4.0
		gi := Clampi(int(hv*float64(len(ledSpectrum))), 0, len(ledSpectrum)-1)
		di := Clampi(int(lv*float64(len(ledRamp))), 0, len(ledRamp)-1)
		return ledSpectrum[gi], ledRamp[di]
	}
}

// LEDSegments is one frame of the LED display cycling through the effects,
// keyed to the frame counter n (the server's push cadence).
func LEDSegments(n int) []diag.ListSegment {
	return LEDSegmentsEff((n/45)%len(ledEffectNames), float64(n)*0.15)
}

// LEDSegmentsEff renders one specific effect at time t, run-length encoded per
// row so the big grid stays a handful of list runs.
func LEDSegmentsEff(eff int, t float64) []diag.ListSegment {
	if eff < 0 || eff >= len(ledEffectNames) {
		eff = 0
	}
	const bw, bh = 4, 2
	segs := []diag.ListSegment{
		diag.ListText(0, 2, diag.ColHeading, "OPEN-DIAG-GO-PRO  --  LED display: colour + letters (RLE)"),
	}
	for lr := 0; lr < LEDRows; lr++ {
		type run struct {
			startLC, wLC int
			col, ch      byte
		}
		var runs []run
		for lc := 0; lc < LEDCols; lc++ {
			col, ch := ledCell(lr, lc, t, eff)
			if k := len(runs) - 1; k >= 0 && runs[k].col == col && runs[k].ch == ch {
				runs[k].wLC++
			} else {
				runs = append(runs, run{lc, 1, col, ch})
			}
		}
		for b := 0; b < bh; b++ {
			sr := 2 + lr*bh + b
			for _, rn := range runs {
				segs = append(segs, diag.ListText(sr, 2+rn.startLC*bw, rn.col,
					strings.Repeat(string(rn.ch), rn.wLC*bw)))
			}
		}
	}
	segs = append(segs, diag.ListText(2+LEDRows*bh+1, 2, diag.ColNormal,
		fmt.Sprintf("%s   %dx%d LEDs   F3/Back stops", ledEffectNames[eff], LEDRows, LEDCols)))
	return segs
}
