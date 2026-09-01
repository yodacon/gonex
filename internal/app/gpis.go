package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/reentry"
	"yodacon.org/gonex/internal/ui"
)

// The GPIS layer: the reentry trajectory taught as six distinct stages —
// deorbit, entry, peak g, skip, descent, landing — made live cockpit
// furniture. The stage is classified off the physics every frame, never
// scripted: the same numbers that fly the ship decide which gauges matter
// right now, which band of the profile chart is lit, and what the pilot
// should be doing with the stick. The instrument dressing borrows the
// classic trainer's grammar: green wireframe MFD cards, an attitude ball,
// and the lift-vector-as-steering idea drawn straight onto the hull.

type entryStage int

const (
	stageDeorbit entryStage = iota // the coast from the burn to sensible air
	stageEntry
	stagePeakG
	stageSkip
	stageDescent
	stageLanding
)

type stageInfo struct {
	name, short string
	col         color.RGBA
	hint        string   // what the stick is for right now
	priority    []string // dial labels the stage brings forward
}

var stageTable = [...]stageInfo{
	stageDeorbit: {"DEORBIT COAST", "DEORBIT", color.RGBA{200, 206, 214, 255},
		"hold trim - the air is still theory down there",
		[]string{"MACH", "BATTERY", "LI TANK"}},
	stageEntry: {"ENTRY", "ENTRY", color.RGBA{160, 168, 178, 255},
		"pitch ^v flies AoA - <> banks the lift vector",
		[]string{"MACH", "STANDOFF", "LI TANK"}},
	stagePeakG: {"PEAK G & HEATING", "PEAK G", color.RGBA{255, 59, 78, 255},
		"wings level - ride the pulse - [ ] feed the shield",
		[]string{"G-LOAD", "FLUX/LIM", "WALL K"}},
	stageSkip: {"SKIP", "SKIP", color.RGBA{63, 216, 224, 255},
		"push down - don't surf out of the corridor",
		[]string{"MACH", "STANDOFF", "BATTERY"}},
	stageDescent: {"DESCENT", "DESCENT", color.RGBA{94, 230, 138, 255},
		"<> bank onto the pad line - mind the RCS bottles",
		[]string{"LOAD", "RCS", "HULL SOAK"}},
	stageLanding: {"LANDING", "LANDING", color.RGBA{96, 168, 255, 255},
		"glideslope capture ahead - bleed speed, stay lined up",
		[]string{"RCS", "HULL SOAK", "BATTERY"}},
}

// classifyStage reads the six-stage taxonomy off the live state. The
// order matters: the terminal stages own their altitudes outright, the
// skip is a geometry condition (flat or climbing while hypersonic), and
// peak g is a loads condition — whichever the physics says is happening.
func classifyStage(s *reentry.Sim) entryStage {
	hkm := s.H / 1000
	switch {
	case hkm < 20 || s.V < 700:
		return stageLanding
	case hkm < 42 || s.V < 2400:
		return stageDescent
	case hkm > 100:
		return stageDeorbit
	case s.Gamma > -0.0035 && s.V > 4500 && hkm > 55:
		return stageSkip
	case s.Pt.GLoad > 0.35*s.Veh.GLimit ||
		s.Pt.QShielded > 0.45*s.Veh.TPSLimit:
		return stagePeakG
	default:
		return stageEntry
	}
}

// updateStage advances the stage machine with a short hold so the label
// cannot flicker on a boundary, and calls each transition on the console.
func (a *App) updateStage() {
	e := a.entry
	want := classifyStage(e.sim)
	if want != e.stage {
		e.stageHold += dt
		if e.stageHold > 0.4 {
			e.stage, e.stageHold, e.stageT = want, 0, 0
			a.Console.Notifyf("STAGE — %s.", stageTable[want].name)
		}
	} else {
		e.stageHold = 0
	}
	e.stageT += dt
}

// aoaDeg is the displayed trim angle of attack: the commanded vertical
// L/D mapped onto the airframe's trim table (full commandable lift is
// flown at about 34 degrees, the envelope's design trim).
func aoaDeg(s *reentry.Sim) float64 {
	ldMax := s.Veh.LDMax + (s.Veh.GlideLD-s.Veh.LDMax)*math.Min(s.AeroAuth*1.2, 1)
	f := s.LastLD / math.Max(ldMax, 0.05)
	return 34 * math.Min(math.Max(f, -1), 1)
}

// stageBand is one colored altitude band of the profile chart.
type stageBand struct {
	h0, h1 float64 // km
	col    color.RGBA
	label  string
}

var entryBands = []stageBand{
	{100, 125, color.RGBA{200, 206, 214, 255}, "COAST"},
	{70, 100, color.RGBA{150, 156, 166, 255}, "ENTRY"},
	{50, 70, color.RGBA{255, 59, 78, 255}, "MAX G&T"},
	{20, 50, color.RGBA{94, 230, 138, 255}, "DESCENT"},
	{0, 20, color.RGBA{96, 168, 255, 255}, "LANDING"},
}

// drawStageBands is the trainer's profile figure made live: altitude
// against downrange with the stage bands painted behind — the expected
// profile as the reference line, the flown trace burning over it, and
// the predictor's dots running ahead to the pad. The band the ship is
// in right now is the lit one.
func (a *App) drawStageBands(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	x0, y0, w, h := 20.0, 238.0, 196.0, 116.0
	vector.DrawFilledRect(screen, float32(x0), float32(y0), float32(w), float32(h),
		premul(color.RGBA{5, 7, 10, 255}, 0.6), false)
	vector.StrokeRect(screen, float32(x0), float32(y0), float32(w), float32(h), 1,
		premul(colRule, 0.9), false)
	ui.DrawText(screen, "TRAJECTORY STAGES  h-dist", x0+8, y0+5, 0.6)

	px0, px1 := x0+8, x0+w-52
	py0, py1 := y0+20, y0+h-10
	yFor := func(hm float64) float32 {
		f := math.Min(math.Max(hm/125000, 0), 1)
		return float32(py1 - f*(py1-py0))
	}
	hkm := s.H / 1000
	for _, b := range entryBands {
		yt, yb := yFor(b.h1*1000), yFor(b.h0*1000)
		al := 0.10
		if hkm >= b.h0 && hkm < b.h1 {
			al = 0.16 + 0.10*math.Abs(math.Sin(s.T*3))
		}
		vector.DrawFilledRect(screen, float32(px0), yt, float32(px1-px0), yb-yt,
			premul(b.col, al), false)
		lal := float32(0.45)
		if hkm >= b.h0 && hkm < b.h1 {
			lal = 1
		}
		ui.DrawTextScaled(screen, b.label, px1+4, float64(yt), 1, b.col, lal)
	}
	// x: downrange over the whole flight, pad at the right edge
	denom := s.Downrange + s.PadDist*1000
	if n := len(e.expected); n > 0 && e.expected[n-1].dr > denom {
		denom = e.expected[n-1].dr
	}
	denom = math.Max(denom, 1)
	xFor := func(dr float64) float32 {
		f := math.Min(math.Max(dr/denom, 0), 1)
		return float32(px0 + f*(px1-px0))
	}
	var lx, ly float32
	for i, p := range e.expected {
		nx, ny := xFor(p.dr), yFor(p.h)
		if i > 0 {
			fastLine(screen, lx, ly, nx, ny, 1, colDim, 0.8)
		}
		lx, ly = nx, ny
	}
	for i := 1; i < len(e.trail); i++ {
		fastLine(screen, xFor(e.trail[i-1].dr), yFor(e.trail[i-1].h),
			xFor(e.trail[i].dr), yFor(e.trail[i].h), 1.5, colHeat, 0.85)
	}
	for _, p := range e.pred {
		fastDot(screen, xFor(p.Downrange), yFor(p.H), 1.2, hudGreen, 0.7)
	}
	// the pad tick, and the ship as a chrome caret riding its own line
	fastLine(screen, xFor(denom), float32(py1-6), xFor(denom), float32(py1), 2,
		colPhos, 0.9)
	sx, sy := xFor(s.Downrange), yFor(s.H)
	vector.DrawFilledCircle(screen, sx, sy, 2.6, colChrome, false)
}

// drawSurfaceMFD is the attitude director: the classic two-tone ball —
// sky over ground, rolled with the bank, the horizon sliding with pitch —
// with the pitch ladder marked and the surface-data column beside it.
// PTCH here is the attitude the airframe holds: flight-path angle plus
// the trim angle of attack.
func (a *App) drawSurfaceMFD(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	x0, y0, w, h := 20.0, 362.0, 196.0, 124.0
	vector.DrawFilledRect(screen, float32(x0), float32(y0), float32(w), float32(h),
		premul(color.RGBA{5, 7, 10, 255}, 0.6), false)
	vector.StrokeRect(screen, float32(x0), float32(y0), float32(w), float32(h), 1,
		premul(hudGreen, 0.55), false)
	ui.DrawTextScaled(screen, "SURFACE  ATT", x0+8, y0+4, 1, hudGreen, 0.75)

	gamDeg := s.Gamma * 180 / math.Pi
	aoa := aoaDeg(s)
	pitch := gamDeg + aoa
	bankDeg := e.bank * 180 / math.Pi

	cx, cy, r := x0+62, y0+68, 44.0
	const sc = 1.4 // px per degree on the ball
	off := math.Min(math.Max(pitch*sc, -(r-8)), r-8)
	sinb, cosb := math.Sin(e.bank), math.Cos(e.bank)
	skyCol := color.RGBA{70, 130, 210, 255}
	gndCol := color.RGBA{150, 96, 40, 255}
	for iy := -r + 2; iy <= r-2; iy += 3 {
		hw := math.Sqrt(r*r - iy*iy)
		c := gndCol
		if iy < off { // above the horizon line is sky
			c = skyCol
		}
		fastLine(screen,
			float32(cx-hw*cosb-iy*sinb), float32(cy-hw*sinb+iy*cosb),
			float32(cx+hw*cosb-iy*sinb), float32(cy+hw*sinb+iy*cosb),
			3, c, 0.85)
	}
	// horizon line and pitch ladder, rolled with the ball
	mark := func(pdeg, hw, wdt float64, c color.RGBA, al float64) {
		ly := off - pdeg*sc
		if ly < -(r-6) || ly > r-6 {
			return
		}
		fastLine(screen,
			float32(cx-hw*cosb-ly*sinb), float32(cy-hw*sinb+ly*cosb),
			float32(cx+hw*cosb-ly*sinb), float32(cy+hw*sinb+ly*cosb),
			float32(wdt), c, al)
	}
	mark(0, math.Sqrt(math.Max(r*r-off*off, 16)), 2, color.RGBA{235, 240, 245, 255}, 0.95)
	for _, p := range [4]float64{-30, -15, 15, 30} {
		mark(p, 14, 1, color.RGBA{235, 240, 245, 255}, 0.6)
	}
	vector.StrokeCircle(screen, float32(cx), float32(cy), float32(r), 1.2,
		premul(hudGreen, 0.7), false)
	// the fixed aircraft symbol
	fastLine(screen, float32(cx-16), float32(cy), float32(cx-5), float32(cy), 2, colChrome, 1)
	fastLine(screen, float32(cx+5), float32(cy), float32(cx+16), float32(cy), 2, colChrome, 1)
	fastDot(screen, float32(cx), float32(cy), 1.6, colChrome, 1)

	bside := "L"
	if bankDeg >= 0 {
		bside = "R"
	}
	ui.DrawTextScaled(screen,
		fmt.Sprintf("PTCH %+3.0f  BNK %03.0f%s", pitch, math.Abs(bankDeg), bside),
		x0+8, y0+h-16, 1, hudGreen, 0.85)

	// the surface-data column
	vs := s.V * math.Sin(s.Gamma)
	rows := []struct{ k, v string }{
		{"AOA", fmt.Sprintf("%+5.1f'", aoa)},
		{"VS", fmt.Sprintf("%+5.0f m/s", vs)},
		{"ACC", fmt.Sprintf("%5.2f g", s.Pt.GLoad)},
	}
	rx := x0 + 128
	for i, row := range rows {
		ry := y0 + 20 + float64(i)*27
		ui.DrawTextScaled(screen, row.k, rx, ry, 1, hudGreen, 0.5)
		ui.DrawTextScaled(screen, row.v, rx, ry+11, 1, hudGreen, 0.95)
	}
}

// drawAeroMFD is the aerobrake computer: the numbers the trainer's path
// MFD keeps — AoA, L/D, bank, the worst g so far, and the predicted
// low point of the path ahead ("Pe Alt": where the current stick bottoms
// out) — beside a little planet with the predicted trajectory bent
// around it. All of it re-flown from the live state a few times a second.
func (a *App) drawAeroMFD(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	x0, y0, w, h := 846.0, 324.0, 168.0, 164.0
	vector.DrawFilledRect(screen, float32(x0), float32(y0), float32(w), float32(h),
		premul(color.RGBA{5, 7, 10, 255}, 0.6), false)
	vector.StrokeRect(screen, float32(x0), float32(y0), float32(w), float32(h), 1,
		premul(hudGreen, 0.55), false)
	ui.DrawTextScaled(screen, "AEROPATH", x0+34, y0+4, 1, hudGreen, 0.85)
	ui.DrawTextScaled(screen, "Tgt PAD", x0+104, y0+4, 1, hudGreen, 0.5)

	// the button rail, for the family resemblance
	for i, b := range [5]string{"TGT", "REF", "PG", "MOD", "PRJ"} {
		by := y0 + 22 + float64(i)*26
		vector.StrokeRect(screen, float32(x0+3), float32(by), 26, 13, 1,
			premul(hudGreen, 0.35), false)
		ui.DrawTextScaled(screen, b, x0+6, by+1, 1, hudGreen, 0.45)
	}
	for i, b := range [3]string{"PWR", "SEL", "MNU"} {
		bx := x0 + 38 + float64(i)*42
		c := hudGreen
		if i == 0 {
			c = color.RGBA{255, 90, 90, 255}
		}
		vector.StrokeRect(screen, float32(bx), float32(y0+h-16), 30, 13, 1,
			premul(c, 0.5), false)
		ui.DrawTextScaled(screen, b, bx+4, y0+h-15, 1, c, 0.6)
	}

	// predicted low point of the path ahead, from the live predictor
	peAlt, peVel := s.H, s.V
	for _, p := range e.pred {
		if p.H < peAlt {
			peAlt, peVel = p.H, p.V
		}
	}
	rows := []struct{ k, v string }{
		{"AoA ", fmt.Sprintf("%+6.1f'", aoaDeg(s))},
		{"L/D ", fmt.Sprintf("%+6.2f", s.LastLD)},
		{"Bnk ", fmt.Sprintf("%+6.0f'", e.bank*180/math.Pi)},
		{"GMax", fmt.Sprintf("%6.2f G", s.MaxG)},
		{"PeAl", fmt.Sprintf("%6.1f k", peAlt/1000)},
		{"''V ", fmt.Sprintf("%6.2f k", peVel/1000)},
		{"Dst ", fmt.Sprintf("%6.0f km", math.Max(s.PadDist, 0))},
		{"XRng", fmt.Sprintf("%+6.1f km", s.Crossrange)},
	}
	for i, row := range rows {
		ry := y0 + 20 + float64(i)*15
		ui.DrawTextScaled(screen, row.k, x0+34, ry, 1, hudGreen, 0.5)
		ui.DrawTextScaled(screen, row.v, x0+64, ry, 1, hudGreen, 0.9)
	}

	// the path plot: the planet, the flown arc, the prediction ahead
	pcx, pcy, pr := x0+w-34, y0+52, 15.0
	const kx = 15.0 / 122000
	vector.StrokeCircle(screen, float32(pcx), float32(pcy), float32(pr), 1,
		premul(colDim, 0.9), false)
	pos := func(dr, hm float64) (float32, float32) {
		ang := -math.Pi/2 + dr/1.4e6
		rr := pr + math.Max(hm, 0)*kx
		return float32(pcx + math.Cos(ang)*rr), float32(pcy + math.Sin(ang)*rr)
	}
	var lx, ly float32
	for i, p := range e.trail {
		nx, ny := pos(p.dr, p.h)
		if i > 0 {
			fastLine(screen, lx, ly, nx, ny, 1, colHeat, 0.6)
		}
		lx, ly = nx, ny
	}
	prev := false
	for _, p := range e.pred {
		nx, ny := pos(p.Downrange, p.H)
		if prev {
			fastLine(screen, lx, ly, nx, ny, 1, hudGreen, 0.85)
		}
		lx, ly, prev = nx, ny, true
	}
	sx, sy := pos(s.Downrange, s.H)
	vector.DrawFilledCircle(screen, sx, sy, 2, colChrome, false)
}

// annArrow draws one annotated force arrow — shaft, head, label.
func annArrow(dst *ebiten.Image, x, y, ang, ln float64, c color.RGBA,
	al float64, label string) {
	x1 := x + math.Cos(ang)*ln
	y1 := y + math.Sin(ang)*ln
	fastLine(dst, float32(x), float32(y), float32(x1), float32(y1), 2, c, al)
	for _, da := range [2]float64{2.6, -2.6} {
		fastLine(dst, float32(x1), float32(y1),
			float32(x1+math.Cos(ang+da)*9), float32(y1+math.Sin(ang+da)*9), 2, c, al)
	}
	if label != "" {
		ui.DrawTextScaled(dst, label, x1+7, y1-7, 1, c, float32(al))
	}
}

// drawShipVectors is the trainer's third-person diagram drawn onto the
// live hull: the lift vector rolling with the bank (the steering idea
// itself — point the lift where you want to go), the direction of
// motion, gravity, the angle-of-attack wedge at the nose, and the total
// aerodynamic force in real units. The forces are the sim's own numbers.
func (a *App) drawShipVectors(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	if s.Status() != reentry.Flying || s.V < 700 {
		return
	}
	fade := 1 - e.seamT()*2.5
	if fade <= 0 {
		return
	}
	al := 0.55 * fade
	cx, cy := shipX, float64(shipDrawY)

	// the lift vector, rolled with the bank — the whole chapter in one arrow
	ldv := s.LastLD
	liftAng := -math.Pi/2 + e.bank
	if ldv < 0 {
		liftAng = math.Pi/2 + e.bank
	}
	if liftLen := 24 + 95*math.Min(math.Abs(ldv)/0.6, 1); math.Abs(ldv) > 0.02 {
		annArrow(screen, cx+math.Cos(liftAng)*74, cy+math.Sin(liftAng)*56,
			liftAng, liftLen, colHeat, al, "LIFT")
	}
	// direction of motion, tilted with the flight-path angle
	vAng := -math.Pi/2 + math.Min(-s.Gamma*1.5, 0.9)
	annArrow(screen, cx-160, cy+50, vAng, 64, color.RGBA{255, 96, 96, 255}, al,
		fmt.Sprintf("V %.2f km/s", s.V/1000))
	// gravity, always down
	annArrow(screen, cx+168, cy-16, math.Pi/2, 42, colDim, al*0.9, "g")

	// the angle-of-attack wedge at the nose: hull axis against the flow
	aoa := aoaDeg(s) * math.Pi / 180
	nx, ny := cx, cy-64.0
	axAng := -math.Pi/2 + e.bank*0.7
	fastLine(screen, float32(nx), float32(ny),
		float32(nx+math.Cos(axAng)*36), float32(ny+math.Sin(axAng)*36),
		1.2, colChrome, al)
	fastLine(screen, float32(nx), float32(ny),
		float32(nx+math.Cos(axAng+aoa)*36), float32(ny+math.Sin(axAng+aoa)*36),
		1.2, colEM, al)
	ui.DrawTextScaled(screen, fmt.Sprintf("a %+3.0f'", aoaDeg(s)),
		nx+22, ny-30, 1, colEM, float32(al))

	// the total aero force in the writeup's own annotation style
	fMN := s.Pt.GLoad * s.Veh.Mass * 9.80665 / 1e6
	bx, by := cx+120.0, cy+26.0
	fastLine(screen, float32(bx), float32(by), float32(bx),
		float32(by+18+s.Pt.GLoad*20), 2, color.RGBA{255, 220, 80, 255}, al)
	ui.DrawTextScaled(screen, fmt.Sprintf("G %.2f - F %.1f MN", s.Pt.GLoad, fMN),
		bx+8, by+8, 1, color.RGBA{255, 220, 80, 255}, float32(al*0.9))
}

// drawStageBanner is the stage called out over the reticle, in the
// stage's own band color — blinking while it is fresh.
func (a *App) drawStageBanner(screen *ebiten.Image) {
	e := a.entry
	if e.sim.Status() != reentry.Flying {
		return
	}
	st := stageTable[e.stage]
	al := 0.9
	if e.stageT < 2 {
		al = 0.4 + 0.6*math.Abs(math.Sin(e.stageT*6))
	}
	txt := "- " + st.name + " -"
	ui.DrawTextScaled(screen, txt, float64(ScreenW)/2-float64(len(txt))*3.5, 154,
		1, st.col, float32(al))
}

// --- the console prototype's chart row, live -------------------------

// miniChart draws one translucent strip-chart card over the sky: the
// prototype's small-multiple grammar — a title, one or two series, a
// dashed limit line.
type chartSeries struct {
	col color.RGBA
	get func(recSample) float64
}

func drawMiniChart(screen *ebiten.Image, x0, y0, w, h float64, title string,
	rec []recSample, series []chartSeries, ymax, limit float64, limCol color.RGBA) {
	vector.DrawFilledRect(screen, float32(x0), float32(y0), float32(w), float32(h),
		premul(color.RGBA{5, 7, 10, 255}, 0.42), false)
	vector.StrokeRect(screen, float32(x0), float32(y0), float32(w), float32(h), 1,
		premul(colRule, 0.8), false)
	ui.DrawText(screen, title, x0+6, y0+3, 0.55)
	if len(rec) < 2 || ymax <= 0 {
		return
	}
	px0, px1 := x0+4, x0+w-4
	py0, py1 := y0+16, y0+h-4
	yFor := func(v float64) float32 {
		f := math.Min(math.Max(v/ymax, 0), 1)
		return float32(py1 - f*(py1-py0))
	}
	if limit > 0 && limit < ymax {
		ly := yFor(limit)
		for x := px0; x < px1; x += 8 {
			fastLine(screen, float32(x), ly, float32(x+4), ly, 1, limCol, 0.7)
		}
	}
	for _, sr := range series {
		var lx, ly float32
		for i, sm := range rec {
			nx := float32(px0 + float64(i)/float64(len(rec)-1)*(px1-px0))
			ny := yFor(sr.get(sm))
			if i > 0 {
				fastLine(screen, lx, ly, nx, ny, 1.2, sr.col, 0.9)
			}
			lx, ly = nx, ny
		}
	}
}

// drawConsoleCharts is the prototype's chart row riding the top of the
// scene: the stagnation heating profile (bare vs shielded vs the TPS
// limit), the control authority handover (the plasma's grip dying into
// the airframe's), and the electrical power ledger against the bus cap.
// All of it is the flight recorder — the entry's own history, live.
func (a *App) drawConsoleCharts(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	if len(e.rec) < 2 || e.seamT() > 0.5 {
		return
	}
	qmax := s.Veh.TPSLimit * 1.25
	for _, sm := range e.rec {
		qmax = math.Max(qmax, sm.qBare)
	}
	drawMiniChart(screen, 238, 178, 196, 56, "HEATING  q bare/shielded",
		e.rec, []chartSeries{
			{colDim, func(r recSample) float64 { return r.qBare }},
			{colHeat, func(r recSample) float64 { return r.qShield }},
		}, qmax, s.Veh.TPSLimit, colBad)
	drawMiniChart(screen, 444, 178, 196, 56, "AUTHORITY  plasma/aero",
		e.rec, []chartSeries{
			{colEM, func(r recSample) float64 { return r.pAuth }},
			{colOI, func(r recSample) float64 { return r.aAuth }},
		}, 1.05, 0, colBad)
	pmax := s.Veh.PowerCap * 1.4
	for _, sm := range e.rec {
		pmax = math.Max(pmax, sm.power)
	}
	drawMiniChart(screen, 650, 178, 186, 56, "POWER LEDGER  MW",
		e.rec, []chartSeries{
			{colN2, func(r recSample) float64 { return r.power }},
		}, pmax, s.Veh.PowerCap, colBad)
}

// drawLiveAlgebra is the console's numerical-flow tab as a cockpit
// ticker: the envelope model's own equations, cycled one at a time and
// computed with this frame's numbers — the proof the gauges are read
// off running physics, printed where the pilot can watch it run.
func (a *App) drawLiveAlgebra(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	if s.Status() != reentry.Flying {
		return
	}
	p := s.Pt
	sband := "CLEAR"
	if p.Fpe > 2.2e9 {
		sband = "BLACKOUT"
	}
	beta := s.Veh.Mass / (1.35 * math.Pi * s.Veh.Diameter * s.Veh.Diameter / 4 *
		p.DragFactor)
	lines := []string{
		fmt.Sprintf("qdot.bare = k sqrt(rho/Rn) V^3 = %.1f W/cm2", p.QBare/1e4),
		fmt.Sprintf("qdot.shld = qdot.bare sqrt(Rn/Reff) = %.1f W/cm2  (standoff %.2f)",
			p.QShielded/1e4, p.Standoff),
		fmt.Sprintf("Q.mhd = sigma B^2 Rn / rho V = %.2f  ->  gate %.2f", p.InteractionQ, p.Gate),
		fmt.Sprintf("f.pe = 8.98 sqrt(ne) = %.2f GHz  --  S-BAND %s", p.Fpe/1e9, sband),
		fmt.Sprintf("P = array + cryo + seed = %.1f MW  /  bus cap %.0f MW",
			p.PowerDraw/1e6, s.Veh.PowerCap/1e6),
		fmt.Sprintf("beta = m / Cd A = %.0f kg/m2  --  the freighter's ballistic number", beta),
	}
	idx := int(s.T/6) % len(lines)
	txt := "LIVE ALGEBRA  " + lines[idx]
	x := float64(ScreenW)/2 - float64(len(txt))*3.5
	fin := math.Min(math.Mod(s.T, 6)*2, 1) // each equation types in
	ui.DrawTextScaled(screen, txt, x, float64(ScreenH)-16, 1,
		color.RGBA{63, 216, 224, 255}, float32(0.35+0.5*fin))
	_ = e
}

// --- the takeoff's instrument suite ----------------------------------

// takeoffClock is the ascent's wall-clock compression: the twelve-second
// cinematic stands in for a ~500 s climb, so the rate gauges (VS, ACCEL)
// read per profile-second, the way the entry's read per sim-second.
const takeoffClock = 40.0

var ascentBands = []stageBand{
	{90, 125, color.RGBA{200, 206, 214, 255}, "INSERT"},
	{50, 90, color.RGBA{255, 59, 78, 255}, "SHEATH"},
	{16, 50, color.RGBA{94, 230, 138, 255}, "MAX Q"},
	{0, 16, color.RGBA{96, 168, 255, 255}, "CLIMB"},
}

func takeoffStageName(ts *takeoffState) string {
	switch {
	case ts.t < rollDur:
		return "ROLL"
	case ts.h < 16:
		return "CLIMB"
	case ts.h < 50:
		return "MAX Q"
	case ts.h < 90:
		return "SHEATH"
	}
	return "INSERTION"
}

// drawTakeoffGauges is the departure flown on instruments: the same
// telemetry strip, dial cluster and stage tape the entry carries, fed by
// the ascent's own background physics — the standard atmosphere
// evaluated at the climb every frame.
func (a *App) drawTakeoffGauges(screen *ebiten.Image) {
	ts := a.takeoff

	// the telemetry strip
	cells := []struct{ k, v string }{
		{"STAGE", takeoffStageName(ts)},
		{"T", fmt.Sprintf("+%.1f s", ts.t)},
		{"ALTITUDE", fmt.Sprintf("%.1f km", ts.h)},
		{"VELOCITY", fmt.Sprintf("%.2f km/s", ts.v)},
		{"MACH", fmt.Sprintf("%.1f", ts.mach)},
		{"Q DYN", fmt.Sprintf("%.2f MPa", ts.qdyn/1e6)},
		{"ACCEL", fmt.Sprintf("%.2f g", ts.accG/takeoffClock)},
		{"VS", fmt.Sprintf("%.0f m/s", ts.vs/takeoffClock)},
		{"SHEATH", fmt.Sprintf("%.0f%%", ts.ascentHeat()*100)},
	}
	vector.DrawFilledRect(screen, 0, 26, ScreenW, 38,
		premul(color.RGBA{5, 7, 10, 255}, 0.62), false)
	vector.StrokeLine(screen, 0, 64, ScreenW, 64, 1, premul(colRule, 0.7), false)
	cw := float64(ScreenW) / float64(len(cells))
	for i, c := range cells {
		x := cw*float64(i) + 10
		ui.DrawText(screen, c.k, x, 30, 0.5)
		ui.DrawText(screen, c.v, x, 44, 0.9)
	}

	// the dial cluster, scaled to the departure profile
	top := float64(ScreenH - 92)
	vector.DrawFilledRect(screen, 0, float32(top), ScreenW, 92,
		premul(color.RGBA{5, 7, 10, 255}, 0.55), false)
	vector.StrokeLine(screen, 0, float32(top), ScreenW, float32(top), 1,
		premul(colRule, 0.8), false)
	type def struct {
		label, val          string
		v, min, max, r0, r1 float64
	}
	defs := []def{
		{"VEL", fmt.Sprintf("%.2f", ts.v), ts.v, 0, 9, 8.35, 9},
		{"MACH", fmt.Sprintf("%.1f", ts.mach), ts.mach, 0, 30, 27, 30},
		{"Q DYN", fmt.Sprintf("%.2f", ts.qdyn/1e6), ts.qdyn / 1e6, 0, 8, 5.5, 8},
		{"ACCEL", fmt.Sprintf("%.2f", ts.accG/takeoffClock), ts.accG / takeoffClock, 0, 4.5, 3.5, 4.5},
		{"VS", fmt.Sprintf("%.0f", ts.vs/takeoffClock), ts.vs / takeoffClock, 0, 3600, 0, 0},
		{"ALT", fmt.Sprintf("%.0f", ts.h), ts.h, 0, 122, 0, 0},
	}
	dw := (float64(ScreenW) - 40) / float64(len(defs))
	for i, d := range defs {
		dial(screen, 20+dw*float64(i)+dw/2, top+42, 28,
			d.v, d.min, d.max, d.r0, d.r1, d.label, d.val)
	}

	// the ascent stage tape: the entry's altitude bands, climbed
	tx, ty0, ty1 := float64(ScreenW-52), 150.0, 440.0
	yFor := func(hkm float64) float32 {
		f := math.Min(math.Max(hkm/125, 0), 1)
		return float32(ty1 - f*(ty1-ty0))
	}
	for _, b := range ascentBands {
		yt, yb := yFor(b.h1), yFor(b.h0)
		al := 0.14
		if ts.h >= b.h0 && ts.h < b.h1 {
			al = 0.30
		}
		vector.DrawFilledRect(screen, float32(tx), yt, 16, yb-yt,
			premul(b.col, al), false)
		lal := float32(0.45)
		if ts.h >= b.h0 && ts.h < b.h1 {
			lal = 1
		}
		ui.DrawTextScaled(screen, b.label, tx-float64(len(b.label))*7-6,
			float64(yt), 1, b.col, lal)
	}
	vector.StrokeRect(screen, float32(tx), float32(ty0), 16, float32(ty1-ty0), 1,
		premul(colRule, 0.9), false)
	cy := yFor(ts.h)
	fastLine(screen, float32(tx-4), cy, float32(tx+20), cy, 2, colChrome, 1)

	// the green suite's horizon: bar, caret and the boxed tapes
	hy := 300 + ts.gam*8
	fhy := float32(hy)
	cx := float32(shipX)
	hudLine(screen, 30, fhy, cx-90, fhy, 1.5, 0.9)
	hudLine(screen, cx+90, fhy, float32(ScreenW)-70, fhy, 1.5, 0.9)
	hudLine(screen, cx-14, fhy, cx-4, fhy-8, 1.5, 0.9)
	hudLine(screen, cx+4, fhy-8, cx+14, fhy, 1.5, 0.9)
	for _, side := range [2]struct {
		x     float32
		val   float64
		label string
	}{{150, ts.v * 1000, "GS m/s"}, {float32(ScreenW) - 234, ts.h * 1000, "ALT m"}} {
		fastRect(screen, side.x, fhy-12, 84, 24, color.RGBA{4, 10, 5, 255}, 0.7)
		vector.StrokeRect(screen, side.x, fhy-12, 84, 24, 1,
			premul(hudGreen, 0.9), false)
		ui.DrawTextScaled(screen, fmt.Sprintf("%6.0f", side.val),
			float64(side.x)+10, float64(fhy)-7, 1, hudGreen, 1)
		ui.DrawTextScaled(screen, side.label, float64(side.x)+4, float64(fhy)+16,
			1, hudGreen, 0.55)
	}
}
