package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"yodacon.org/gonex/internal/ui"
)

// The ILS final: after the corridor is flown, the computer captures the
// glideslope and the last kilometres play as an airliner autoland into the
// city — the port rides the horizon and closes at exactly the ground speed
// on the tape. The overlay is fighter-HUD green: horizon bar with the
// waterline caret, flight-path marker, speed and altitude boxes, heading
// ticks, localizer/glideslope deviation scales, and the ILS lock boxed on
// the Destination Spaceport threshold.

var hudGreen = color.RGBA{120, 255, 130, 255}

func (a *App) drawFinalApproach(screen *ebiten.Image) {
	e := a.entry
	hkm := e.finalH
	hy := 330.0
	flat := func(x, y float64) (float32, float32) { return float32(x), float32(y) }
	sun := sunAt(e.dayPhase0 + e.sim.T*0.0006)
	a.drawSky(screen, hkm, hy, 0, shipX, sun, e.nebula)

	dayn := 0.3 + 0.7*sun
	g := color.RGBA{uint8(28 + 40*dayn), uint8(24 + 32*dayn), uint8(18 + 24*dayn), 255}
	fastRect(screen, 0, float32(hy), ScreenW, float32(float64(ScreenH)-hy), g, 1)

	// the port, closing at the tape's ground speed — attached to the
	// terrain, attached to the horizon
	eyeK := 430 * math.Max(hkm+0.06, 0.03)
	proj := func(lat, ahead float64) (float64, float64, float64, bool) {
		d := ahead - e.finalRun
		if d < 0.03 {
			return 0, 0, 0, false
		}
		x := shipX + lat/d*430
		y := hy + eyeK/d
		return x, y, d, y <= float64(ScreenH)+430 && y >= hy-1
	}
	if e.port != nil {
		drawPortScene(screen, e.port, e.sim.T, 1, sun, proj, flat)
	}

	// the ship, riding the autoland
	shipImg := a.Catalog.Get(a.Cfg.PlayerShipID).Sprites[0]
	b := shipImg.Bounds()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(b.Dx())/2, -float64(b.Dy())/2)
	op.GeoM.Scale(1.5, 1.5)
	op.GeoM.Translate(shipX, 560)
	screen.DrawImage(shipImg, op)

	a.drawILSHud(screen, hy)
}

// drawILSSide is the persistent ILS status, parked off to the side for the
// WHOLE descent: acquiring on the corridor, locked once the beam is in
// range, with the live range and glideslope deviation — so the lock is
// part of the entire landing, not just the final.
func (a *App) drawILSSide(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	x, y := 852.0, 252.0
	locked := s.PadDist < 60 || s.H < 30000
	al := 0.45 + 0.25*math.Sin(s.T*4)
	label := "ILS ACQUIRING"
	if locked {
		al, label = 1, "ILS LOCK"
	}
	fastRect(screen, float32(x-6), float32(y-6), 172, 66, color.RGBA{4, 10, 5, 255}, 0.55)
	dx, dy := float32(x+8), float32(y+8)
	hudLine(screen, dx, dy-6, dx+6, dy, 1.5, al)
	hudLine(screen, dx+6, dy, dx, dy+6, 1.5, al)
	hudLine(screen, dx, dy+6, dx-6, dy, 1.5, al)
	hudLine(screen, dx-6, dy, dx, dy-6, 1.5, al)
	ui.DrawTextScaled(screen, label, x+22, y, 1, hudGreen, float32(al))
	if st := a.gal.Stellars[e.stellar]; st != nil {
		ui.DrawTextScaled(screen, "DEST "+st.Name, x+22, y+16, 1, hudGreen, 0.7)
	}
	rng := math.Max(s.PadDist, 0)
	if e.appRange > 0 {
		rng = e.appRange
	}
	ui.DrawTextScaled(screen,
		fmt.Sprintf("RNG %5.0f km  G/S %+.1f", rng, s.GammaError()),
		x, y+34, 1, hudGreen, 0.7)
}

// hudLine is a thin phosphor stroke.
func hudLine(dst *ebiten.Image, x0, y0, x1, y1, w float32, al float64) {
	fastLine(dst, x0, y0, x1, y1, w, hudGreen, al)
}

// hudParams is one frame of the landing HUD — the same green suite runs
// from entry interface to rollout, fed by whichever camera owns the frame.
type hudParams struct {
	hy            float64
	rot           func(x, y float64) (float32, float32)
	spd, alt, rng float64 // m/s, m, km
	gsDev, locDev float64 // -1..1, autoland holds them near zero
	fmx, fmy      float64 // flight-path marker aim point
	phase         string
	aoa           float64 // trim angle of attack, deg — live, not painted on
	mode          string  // MAN / AUTO
	showLock      bool
}

func (a *App) drawILSHud(screen *ebiten.Image, hy float64) {
	e := a.entry
	// the final's aim point: the threshold, or the rollout waterline
	fmx, fmy := float64(shipX), hy+34
	if e.finalRun < -0.05 {
		fmy = hy + 430*math.Max(e.finalH+0.06, 0.03)/(-e.finalRun)
	}
	phase := "GLIDESLOPE — AUTOLAND"
	switch {
	case e.finalRun >= 0 && e.finalSpd > 0.02:
		phase = "ROLLOUT"
	case e.finalRun >= 0:
		phase = "PARKED — WELCOME TO THE PORT"
	case e.finalRun > -0.4:
		phase = "FLARE"
	case e.finalH < 0.1:
		phase = "MINIMUMS"
	}
	wob := math.Sin(e.finalT*1.7) * 0.06
	a.drawHudFrame(screen, hudParams{
		hy:  hy,
		rot: func(x, y float64) (float32, float32) { return float32(x), float32(y) },
		spd: e.finalSpd * 1000, alt: e.finalH * 1000, rng: math.Max(-e.finalRun, 0),
		gsDev: wob, locDev: wob * 0.7,
		fmx: fmx, fmy: fmy, phase: phase, aoa: 3.0 + wob*8, mode: "AUTO",
		showLock: true,
	})
}

// drawEntryHud runs the same suite during the corridor: the horizon bar
// banks with the world, the deviation diamonds read the corridor needle
// and crossrange, and the flight-path marker rides the flight-path angle.
func (a *App) drawEntryHud(screen *ebiten.Image, hy float64) {
	e, s := a.entry, a.entry.sim
	pitch := math.Min(math.Max(-s.Gamma/(math.Pi/2), 0), 1)
	rng := math.Max(s.PadDist, 0)
	if e.appRange > 0 {
		rng = e.appRange // the same number the world is flying
	}
	// the ALT box crosses the seam too: trajectory altitude eases into
	// the glideslope altitude the final opens on, no jump
	tF := e.seamT()
	alt := s.H
	if e.appRange > 0 {
		alt = s.H*(1-tF) + 52.4*e.appRange*tF
	}
	a.drawHudFrame(screen, hudParams{
		hy:  hy,
		rot: e.rot,
		spd: s.V, alt: alt, rng: rng,
		gsDev:  math.Min(math.Max(s.GammaError()/math.Max(s.Width, 0.01)/1.6, -1), 1),
		locDev: math.Min(math.Max(s.Crossrange/40, -1), 1),
		fmx:    shipX - math.Min(math.Max(s.Crossrange/40, -1), 1)*90,
		fmy:    hy + pitch*(float64(shipDrawY)-70-hy),
		phase:  "REENTRY — " + map[bool]string{true: "AUTOLAND", false: "MANUAL"}[e.auto],
		aoa:    aoaDeg(s),
		mode:   map[bool]string{true: "AUTO", false: "MAN"}[e.auto],
	})
}

func (a *App) drawHudFrame(screen *ebiten.Image, p hudParams) {
	e := a.entry
	cx := float32(shipX)
	fhy := float32(p.hy)

	// --- the green horizon: full-width bar with a gap and caret center,
	// banked with the world so sky and symbology agree
	x0l, y0l := p.rot(30, p.hy)
	x1l, y1l := p.rot(shipX-90, p.hy)
	x0r, y0r := p.rot(shipX+90, p.hy)
	x1r, y1r := p.rot(float64(ScreenW)-30, p.hy)
	hudLine(screen, x0l, y0l, x1l, y1l, 1.5, 0.9)
	hudLine(screen, x0r, y0r, x1r, y1r, 1.5, 0.9)
	hudLine(screen, cx-14, fhy, cx-4, fhy-8, 1.5, 0.9) // the waterline caret
	hudLine(screen, cx+4, fhy-8, cx+14, fhy, 1.5, 0.9)

	// --- flight-path marker: circle and wings on the aim point
	fmx, fmy := float32(p.fmx), float32(p.fmy)
	const fr = 9
	prev := false
	var lx, ly float32
	for i := 0; i <= 12; i++ {
		th := 2 * math.Pi * float64(i) / 12
		px := fmx + float32(math.Cos(th))*fr
		py := fmy + float32(math.Sin(th))*fr
		if prev {
			hudLine(screen, lx, ly, px, py, 1.5, 0.95)
		}
		lx, ly, prev = px, py, true
	}
	hudLine(screen, fmx-fr-14, fmy, fmx-fr, fmy, 1.5, 0.95) // wings
	hudLine(screen, fmx+fr, fmy, fmx+fr+14, fmy, 1.5, 0.95)
	hudLine(screen, fmx, fmy-fr-8, fmx, fmy-fr, 1.5, 0.95) // tail

	// --- the ILS lock on the Destination Spaceport threshold
	if x, y, d, ok := func() (float64, float64, float64, bool) {
		if !p.showLock {
			return 0, 0, 0, false
		}
		dd := -e.finalRun
		if dd < 0.12 {
			return 0, 0, 0, false
		}
		return shipX, p.hy + 430*math.Max(e.finalH+0.06, 0.03)/dd, dd, true
	}(); ok && y < float64(ScreenH)-120 {
		s := float32(math.Min(10+30/d, 40))
		bx, by := float32(x), float32(y)
		// a lock diamond
		hudLine(screen, bx, by-s, bx+s, by, 1.5, 1)
		hudLine(screen, bx+s, by, bx, by+s, 1.5, 1)
		hudLine(screen, bx, by+s, bx-s, by, 1.5, 1)
		hudLine(screen, bx-s, by, bx, by-s, 1.5, 1)
		ui.DrawTextScaled(screen, "ILS LOCK", float64(bx)+float64(s)+8, float64(by)-14, 1, hudGreen, 0.95)
		st := a.gal.Stellars[e.stellar]
		name := "DESTINATION SPACEPORT"
		if st != nil {
			name = "DESTINATION SPACEPORT — " + st.Name
		}
		ui.DrawTextScaled(screen, name, float64(bx)+float64(s)+8, float64(by)+2, 1, hudGreen, 0.8)
	}

	// --- speed box (left) and altitude box (right), with tick ladders
	spd := p.spd
	alt := p.alt
	boxW, boxH := float32(84), float32(24)
	for _, side := range [2]struct {
		x     float32
		val   float64
		label string
	}{{244, spd, "GS m/s"}, {float32(ScreenW) - 290, alt, "ALT m"}} {
		fastRect(screen, side.x, fhy-boxH/2, boxW, boxH, color.RGBA{4, 10, 5, 255}, 0.7)
		hudLine(screen, side.x, fhy-boxH/2, side.x+boxW, fhy-boxH/2, 1, 0.9)
		hudLine(screen, side.x, fhy+boxH/2, side.x+boxW, fhy+boxH/2, 1, 0.9)
		hudLine(screen, side.x, fhy-boxH/2, side.x, fhy+boxH/2, 1, 0.9)
		hudLine(screen, side.x+boxW, fhy-boxH/2, side.x+boxW, fhy+boxH/2, 1, 0.9)
		ui.DrawTextScaled(screen, fmt.Sprintf("%6.0f", side.val),
			float64(side.x)+10, float64(fhy)-7, 1, hudGreen, 1)
		ui.DrawTextScaled(screen, side.label, float64(side.x)+4, float64(fhy)+18, 1, hudGreen, 0.55)
		for k := -3; k <= 3; k++ { // the ladder ticks
			if k == 0 {
				continue
			}
			ty := fhy + float32(k)*22
			tx := side.x + boxW + 4
			if side.x > float32(ScreenW)/2 {
				tx = side.x - 10
			}
			hudLine(screen, tx, ty, tx+6, ty, 1, 0.5)
		}
	}

	// --- heading ticks along the top
	for k := -5; k <= 5; k++ {
		tx := cx + float32(k)*64
		h := float32(6)
		if k%2 == 0 {
			h = 10
		}
		hudLine(screen, tx, 78, tx, 78+h, 1, 0.7)
		if k%2 == 0 {
			ui.DrawTextScaled(screen, fmt.Sprintf("%02d", (36+k*2)%36),
				float64(tx)-7, 92, 1, hudGreen, 0.6)
		}
	}
	hudLine(screen, cx-6, 72, cx, 78, 1.5, 1)
	hudLine(screen, cx, 78, cx+6, 72, 1.5, 1)

	// --- localizer (bottom) and glideslope (right) deviation scales:
	// the diamonds read the live deviations — corridor error and
	// crossrange on entry, beam deviation on the final
	for k := -2; k <= 2; k++ {
		fastDot(screen, cx+float32(k)*30, 700, 2, hudGreen, 0.5)
		fastDot(screen, float32(ScreenW)-90, fhy+float32(k)*30, 2, hudGreen, 0.5)
	}
	dia := func(x, y float32) {
		hudLine(screen, x, y-6, x+6, y, 1.5, 1)
		hudLine(screen, x+6, y, x, y+6, 1.5, 1)
		hudLine(screen, x, y+6, x-6, y, 1.5, 1)
		hudLine(screen, x-6, y, x, y-6, 1.5, 1)
	}
	dia(cx+float32(p.locDev*60), 700)
	dia(float32(ScreenW)-90, fhy+float32(p.gsDev*60))

	// --- the mode line and callouts
	ui.DrawTextScaled(screen, p.phase, float64(cx)-float64(len(p.phase))*3.5, 116, 1, hudGreen, 0.95)
	ui.DrawTextScaled(screen,
		fmt.Sprintf("RNG %5.1f km   AoA %+5.1f   %s", p.rng, p.aoa, p.mode),
		float64(cx)-100, 136, 1, hudGreen, 0.7)
}
