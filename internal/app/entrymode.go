package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/reentry"
	"yodacon.org/gonex/internal/ui"
)

// premul converts a straight-alpha tint into the premultiplied color the
// vector package expects.
func premul(c color.RGBA, a float64) color.RGBA {
	return color.RGBA{
		uint8(float64(c.R) * a), uint8(float64(c.G) * a),
		uint8(float64(c.B) * a), uint8(255 * a),
	}
}

// The entry cockpit, chase-camera edition. The camera rides behind and
// above the ship, looking down the velocity vector:
//
//   - the planet limb is a curved arc that flattens as altitude falls,
//     rises as γ steepens, and tilts when the envelope rolls;
//   - the ground is a perspective dot-flow streaming toward the camera,
//     with the landing pad resolving out of it on final;
//   - the shock shell stands off *ahead* of the nose (up-screen), and the
//     wake streams back past the camera, growing as it flies by;
//   - the pilot flies the corridor needle like a glideslope.
//
// Particle colors are the emission lines of the species in the shield
// model; the reflective "one-way mirror" shell is the bright arc.

const entryTimeScale = 6.0 // a ~10-minute entry plays in ~100 s

// Emission-line palette (Li 670.8 nm, N2 first positive, O I 557.7 nm).
var (
	colLi     = color.RGBA{255, 59, 78, 255}
	colN2     = color.RGBA{139, 108, 255, 255}
	colOI     = color.RGBA{94, 230, 138, 255}
	colHeat   = color.RGBA{255, 157, 63, 255}
	colEM     = color.RGBA{63, 216, 224, 255}
	colChrome = color.RGBA{223, 228, 230, 255}
	colDim    = color.RGBA{88, 112, 137, 255}
	colBad    = color.RGBA{255, 59, 78, 255}
	colPhos   = color.RGBA{92, 255, 92, 255}
	colPanel  = color.RGBA{10, 17, 25, 255}
	colRule   = color.RGBA{29, 58, 80, 255}
)

// scene geometry
const (
	shipX, shipY = 512.0, 470.0 // ship anchor on screen
	horizonBase  = 296.0        // limb height at reference γ
	gaugeTop     = 578.0        // instrument panel line
)

type plasmaParticle struct {
	x, y, vx, vy float64
	life, span   float64
	scale        float64 // grows as the wake flies past the camera
	kind         int     // 0 Li streamer, 1 N2 sheath, 2 OI afterglow
}

type groundDot struct {
	lat, ahead float64 // km left/right of track, km ahead of the ship
}

type entryState struct {
	sim     *reentry.Sim
	stellar int // where we are landing
	feed    float64
	auto    bool
	bank    float64 // smoothed visual bank, radians
	flash   float64 // white-in inherited from the deorbit plasma onset
	parts   []plasmaParticle
	ground  []groundDot
	doneWait float64
}

// startEntry converts the view: flight → the landing simulator.
func (a *App) startEntry(stellarID int) {
	st := a.gal.Stellars[stellarID]
	if st == nil {
		return
	}
	prof := reentry.Profile{
		AtmosScale:        st.Landing.AtmosScale,
		GravityScale:      st.Landing.GravityScale,
		CorridorHalfWidth: st.Landing.CorridorHalfWidth,
		PadBonus:          st.Landing.PadBonus,
	}
	veh := reentry.Yodacon()
	veh.LiTank = a.voy.Lithium
	sim := reentry.New(veh, prof, a.voy.Rng.Int63())
	sim.Dmg = a.voy.Dmg // damage carries in from the life you've led
	e := &entryState{sim: sim, stellar: stellarID, feed: 0.1}
	for i := 0; i < 110; i++ {
		e.ground = append(e.ground, groundDot{
			lat:   (a.voy.Rng.Float64() - 0.5) * 56,
			ahead: 1.5 + a.voy.Rng.Float64()*46,
		})
	}
	a.entry = e
	a.mode = modeEntry
	a.miniMapWin.Visible, a.hudWin.Visible, a.targetWin.Visible = false, false, false
	a.fullMapWin.Visible, a.galaxyWin.Visible = false, false
	a.Console.Notifyf("ENTRY INTERFACE — %s. Fly the needle.", st.Name)
}

func (a *App) updateEntry() {
	e := a.entry
	s := e.sim

	if s.Status() == reentry.Flying {
		if inpututil.IsKeyJustPressed(ebiten.KeyA) {
			e.auto = !e.auto
		}
		if ebiten.IsKeyPressed(ebiten.KeyBracketLeft) {
			e.feed = math.Max(e.feed-0.8*dt, 0)
		}
		if ebiten.IsKeyPressed(ebiten.KeyBracketRight) {
			e.feed = math.Min(e.feed+0.8*dt, 1)
		}
		c := reentry.Controls{Feed: e.feed, Auto: e.auto}
		if ebiten.IsKeyPressed(ebiten.KeyArrowUp) {
			c.Pitch, e.auto = 1, false
		}
		if ebiten.IsKeyPressed(ebiten.KeyArrowDown) {
			c.Pitch, e.auto = -1, false
		}
		if ebiten.IsKeyPressed(ebiten.KeyArrowLeft) {
			c.Roll = -1
		}
		if ebiten.IsKeyPressed(ebiten.KeyArrowRight) {
			c.Roll = 1
		}
		c.Boost = inpututil.IsKeyJustPressed(ebiten.KeyB)
		c.Burst = inpututil.IsKeyJustPressed(ebiten.KeySpace)
		c.Auto = e.auto
		s.Step(dt*entryTimeScale, c)
		a.voy.Lithium = s.Li

		// visual bank chases the roll command (autoland's roll shows too)
		roll := c.Roll
		if e.auto {
			roll = math.Min(math.Max(s.Crossrange*0.12, -0.6), 0.6)
		}
		e.bank += (roll*0.48 - e.bank) * math.Min(3.5*dt, 1)
		e.flash = math.Max(e.flash-1.4*dt, 0)

		a.updatePlasma(c)
		a.updateGround()

		if s.Status() != reentry.Flying {
			a.finishEntry()
		}
		return
	}

	// outcome card: give it a beat, then any key continues
	e.doneWait += dt
	if e.doneWait > 1.2 && len(inpututil.AppendJustPressedKeys(nil)) > 0 {
		switch s.Status() {
		case reentry.Landed:
			a.dock = &dockState{stellar: e.stellar}
			a.mode = modeLanded
			a.voy.LandAt(e.stellar, a.gal)
			a.drainNotices()
		case reentry.Destroyed:
			a.entry = nil
			a.endGame()
			return
		default:
			a.mode = modeFlight
			a.setGameStatus(true)
		}
		a.entry = nil
	}
}

func (a *App) finishEntry() {
	e, s := a.entry, a.entry.sim
	a.voy.Dmg = s.Dmg
	switch s.Status() {
	case reentry.Landed:
		sc := s.Score()
		a.voy.Credits += sc.PadBonus
		if sc.PadBonus > 0 {
			a.Console.Notifyf("Pad bonus: %d cr (%.1f km off the line)", sc.PadBonus, sc.CrossKm)
		}
	case reentry.SkippedOut:
		a.voy.Fuel -= 50 // re-circularising costs
		a.voy.passDays(1)
		a.Console.Notifyf("CORRIDOR ABORT — skipped out. Re-entry costs fuel and a day.")
	case reentry.Destroyed:
		a.Console.Notifyf("The %s broke up in the plasma over %s.",
			a.Catalog.Get(a.Cfg.PlayerShipID).Name, a.gal.Stellars[e.stellar].Name)
	}
	a.drainNotices()
}

// --- scene state ------------------------------------------------------

// horizonY is where the limb sits: γ steeper → camera looks further down
// → the horizon rides higher on screen. The chase camera levels off past
// −9°: in the terminal sink (γ → −80°) it keeps the world in frame while
// the instruments keep reading the true angle.
func (e *entryState) horizonY() float64 {
	gam := math.Max(e.sim.Gamma*180/math.Pi, -9)
	return horizonBase + (gam+2.4)*22
}

// perspK is the ground-projection constant: it grows as the deck nears, so
// the surface visibly swells up at the camera on final.
func (e *entryState) perspK() float64 {
	hkm := math.Max(e.sim.H/1000, 1.5)
	return 640 * (1 + 9/hkm)
}

// updatePlasma runs the three emission populations in the chase frame:
// spawn on the standoff shell ahead of the nose, stream back past the
// camera, growing on the way by.
func (a *App) updatePlasma(c reentry.Controls) {
	e, s := a.entry, a.entry.sim
	nose := shipY - 46
	standPx := 16 + 22*(s.Pt.Standoff-1)
	shellY := nose - standPx
	qFrac := s.Pt.QShielded / s.Veh.TPSLimit
	speed := 90 + 240*(s.V/8350)

	spawn := func(kind, n int) {
		for i := 0; i < n; i++ {
			ang := (a.voy.Rng.Float64() - 0.5) * 2.4 // fan across the shell
			px := shipX + math.Sin(ang)*(38+standPx*0.8) - e.bank*30
			py := shellY + (1-math.Cos(ang))*standPx*0.5
			e.parts = append(e.parts, plasmaParticle{
				x: px, y: py,
				vx:    math.Sin(ang)*speed*0.35 + (a.voy.Rng.Float64()-0.5)*24,
				vy:    speed * (0.55 + 0.45*a.voy.Rng.Float64()),
				span:  0.45 + a.voy.Rng.Float64()*(0.5+float64(kind)*0.5),
				scale: 0.4,
				kind:  kind,
			})
		}
	}
	spawn(0, 1+int(c.Feed*6))
	spawn(1, 1+int(qFrac*9))
	if s.V > 2000 {
		spawn(2, 2)
	}

	live := e.parts[:0]
	for _, p := range e.parts {
		p.life += dt
		// past the ship the wake dives toward the camera: spread and grow
		grow := 1 + 3.2*math.Max(0, (p.y-nose)/(float64(ScreenH)-nose))
		p.scale += 2.4 * dt * grow
		p.x += p.vx * dt * grow
		p.y += p.vy * dt * grow
		p.vx += math.Copysign(30, p.x-shipX) * dt * (grow - 1)
		if p.life < p.span && p.y < gaugeTop+40 {
			live = append(live, p)
		}
	}
	e.parts = live
	if len(e.parts) > 900 {
		e.parts = e.parts[len(e.parts)-900:]
	}
}

// updateGround streams the perspective dot field toward the camera.
func (a *App) updateGround() {
	e, s := a.entry, a.entry.sim
	kps := s.V * math.Cos(s.Gamma) / 1000 * entryTimeScale // km/s over ground
	for i := range e.ground {
		g := &e.ground[i]
		g.ahead -= kps * dt
		if g.ahead < 1.2 {
			g.ahead = 44 + a.voy.Rng.Float64()*6
			g.lat = s.Crossrange + (a.voy.Rng.Float64()-0.5)*56
		}
	}
}

// --- drawing ----------------------------------------------------------

// rot rotates a point about the horizon pivot by the bank angle.
func (e *entryState) rot(x, y float64) (float32, float32) {
	hy := e.horizonY()
	sin, cos := math.Sin(e.bank), math.Cos(e.bank)
	dx, dy := x-shipX, y-hy
	return float32(shipX + dx*cos - dy*sin), float32(hy + dx*sin + dy*cos)
}

func (a *App) drawEntry(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	hkm := s.H / 1000
	hy := e.horizonY()
	qFrac := s.Pt.QShielded / s.Veh.TPSLimit

	// --- sky: airglow band above the limb, thickening as the air does
	airglow := math.Min(math.Max((100-hkm)/100, 0), 1)
	for i := 0; i < 5; i++ {
		bx1, by1 := e.rot(-200, hy-14*float64(i+1))
		bx2, by2 := e.rot(float64(ScreenW)+200, hy-14*float64(i+1))
		al := airglow * 0.16 * float64(5-i) / 5
		vector.StrokeLine(screen, bx1, by1, bx2, by2, 15,
			premul(color.RGBA{34, 74, 96, 255}, al), false)
	}

	// --- the limb: a curved arc, flattening as we descend
	curve := 500 + (hkm/122)*-0 + (1-hkm/122)*11000 // px radius: 500 high → 11500 low
	prev := false
	var lx, ly float32
	for x := -60.0; x <= float64(ScreenW)+60; x += 24 {
		dx := x - shipX
		y := hy + dx*dx/(2*curve)
		px, py := e.rot(x, y)
		if prev {
			vector.StrokeLine(screen, lx, ly, px, py, 2,
				premul(color.RGBA{29, 122, 36, 255}, 0.85), false)
		}
		lx, ly, prev = px, py, true
	}

	// --- ground: opaque bands from the limb down to the panel
	for i := 0; i < 14; i++ {
		d := 20 + 38*float64(i)
		bx1, by1 := e.rot(-320, hy+d)
		bx2, by2 := e.rot(float64(ScreenW)+320, hy+d)
		vector.StrokeLine(screen, bx1, by1, bx2, by2, 40,
			color.RGBA{uint8(11 + i/3), uint8(22 + i/2), uint8(27 + i/2), 255}, false)
	}
	groundVis := math.Min(math.Max((62-hkm)/45, 0), 1)
	if groundVis > 0 {
		pk := e.perspK()
		for _, g := range e.ground {
			if g.ahead < 1.2 {
				continue
			}
			px := shipX + (g.lat-s.Crossrange)/g.ahead*430
			py := hy + pk/g.ahead
			if py > gaugeTop {
				continue
			}
			rx, ry := e.rot(px, py)
			al := groundVis * math.Min(6/g.ahead+0.15, 0.9)
			r := float32(math.Min(0.8+7/g.ahead, 4))
			vector.DrawFilledCircle(screen, rx, ry, r,
				premul(color.RGBA{86, 148, 110, 255}, al), false)
		}
		// the pad resolves out of the haze on final
		if s.PadDist > 1 && s.PadDist < 42 && hkm < 30 {
			px := shipX + (0-s.Crossrange)/s.PadDist*430
			py := hy + e.perspK()/s.PadDist
			if py < gaugeTop {
				rx, ry := e.rot(px, py)
				sz := float32(math.Min(3+90/s.PadDist, 44))
				vector.StrokeRect(screen, rx-sz, ry-sz/2, sz*2, sz, 2, colPhos, false)
				vector.StrokeLine(screen, rx, ry-sz/2, rx, ry+sz/2, 1,
					premul(colPhos, 0.5), false)
				ui.DrawText(screen, fmt.Sprintf("PAD %.0f km", s.PadDist),
					float64(rx+sz)+6, float64(ry)-6, 0.8)
			}
		}
	}

	// --- heat veil: the whole sky reddens as q̇ climbs
	if qFrac > 0.15 {
		vector.DrawFilledRect(screen, 0, 0, ScreenW, gaugeTop,
			premul(color.RGBA{255, 120, 60, 255}, (qFrac-0.15)*0.22), false)
	}

	// --- plasma wake (drawn back-to-front: afterglow, sheath, streamers)
	cols := [3]color.RGBA{colLi, colN2, colOI}
	for _, kind := range [3]int{2, 1, 0} {
		for _, p := range e.parts {
			if p.kind != kind {
				continue
			}
			f := 1 - p.life/p.span
			c := premul(cols[kind], 0.72*f)
			r := float32((1.2 + 2.2*f) * p.scale)
			if kind == 1 {
				r += 2
			}
			vector.DrawFilledCircle(screen, float32(p.x), float32(p.y), r, c, false)
		}
	}

	// --- the mirror shell: the bright arc standing off ahead of the nose
	nose := shipY - 46.0
	standPx := 16 + 22*(s.Pt.Standoff-1)
	auth := math.Min(s.Pt.InteractionQ, 1)
	shell := premul(colEM, 0.35+0.55*auth)
	rx0 := 40 + standPx*0.85
	prev = false
	for i := 0; i <= 22; i++ {
		ang := -1.25 + 2.5*float64(i)/22
		x := shipX + math.Sin(ang)*rx0 - e.bank*30
		y := nose - standPx + (1-math.Cos(ang))*standPx*0.75
		px, py := float32(x), float32(y)
		if prev {
			vector.StrokeLine(screen, lx, ly, px, py, 3, shell, false)
			// the hull-facing side stays dark: a thin void line inside
			vector.StrokeLine(screen, lx, ly+4, px, py+4, 2,
				premul(color.RGBA{5, 7, 10, 255}, 0.5), false)
		}
		lx, ly, prev = px, py, true
	}

	// --- the ship, banked with the envelope
	sprite := a.Catalog.Get(a.Cfg.PlayerShipID).Sprites[0]
	b := sprite.Bounds()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(b.Dx())/2, -float64(b.Dy())/2)
	op.GeoM.Rotate(e.bank * 0.7)
	op.GeoM.Scale(1.35, 1.35)
	op.GeoM.Translate(shipX, shipY-32)
	screen.DrawImage(sprite, op)

	a.drawEntryGauges(screen)

	// deorbit plasma white-in
	if e.flash > 0 {
		vector.DrawFilledRect(screen, 0, 0, ScreenW, ScreenH,
			premul(color.RGBA{255, 236, 220, 255}, e.flash), false)
	}

	if s.Status() != reentry.Flying {
		a.drawEntryOutcome(screen)
	}
}

func hbar(dst *ebiten.Image, x, y, w, frac float64, c color.RGBA, label string) {
	vector.DrawFilledRect(dst, float32(x), float32(y), float32(w), 10, colPanel, false)
	vector.StrokeRect(dst, float32(x), float32(y), float32(w), 10, 1, colRule, false)
	f := math.Min(math.Max(frac, 0), 1)
	vector.DrawFilledRect(dst, float32(x)+1, float32(y)+1, float32((w-2)*f), 8, c, false)
	ui.DrawText(dst, label, x, y-15, 0.8)
}

func (a *App) drawEntryGauges(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	py := float64(ScreenH - 190)
	vector.DrawFilledRect(screen, 0, float32(py), ScreenW, 190, color.RGBA{7, 11, 18, 235}, false)
	vector.StrokeLine(screen, 0, float32(py), ScreenW, float32(py), 1, colRule, false)

	// left: tapes
	x := 20.0
	ui.DrawText(screen, fmt.Sprintf("VEL  %6.2f km/s", s.V/1000), x, py+14, 1)
	ui.DrawText(screen, fmt.Sprintf("ALT  %6.1f km", s.H/1000), x, py+34, 1)
	ui.DrawText(screen, fmt.Sprintf("t   +%5.0f s", s.T), x, py+54, 1)
	ui.DrawText(screen, fmt.Sprintf("g    %5.2f / %.0f", s.Pt.GLoad, s.Veh.GLimit), x, py+74, 1)
	ui.DrawText(screen, fmt.Sprintf("QDOT %5.1f / %.0f W/cm2", s.Pt.QShielded/1e4, s.Veh.TPSLimit/1e4), x, py+94, 1)
	ui.DrawText(screen, fmt.Sprintf("GRIP %3.0f%%  (plasma authority)", s.Pt.Gate*100), x, py+114, 1)

	// center: the corridor needle — γ error against the narrowing band
	nx, nw := 260.0, 320.0
	bandFrac := s.Width / (s.Prof.CorridorHalfWidth * 2.2) // 1 → narrowing
	half := nw / 2 * bandFrac
	mid := nx + nw/2
	vector.DrawFilledRect(screen, float32(nx), float32(py+30), float32(nw), 60, colPanel, false)
	vector.DrawFilledRect(screen, float32(mid-half), float32(py+30), float32(half*2), 60,
		color.RGBA{9, 46, 24, 255}, false)
	vector.StrokeRect(screen, float32(mid-half), float32(py+30), float32(half*2), 60, 1,
		premul(colOI, 0.55), false)
	vector.StrokeLine(screen, float32(mid), float32(py+26), float32(mid), float32(py+94), 1, colRule, false)
	err := s.GammaError() / math.Max(s.Width, 0.01) // -1..1 across the band
	npos := mid + math.Min(math.Max(err, -1.6), 1.6)*half
	needleCol := colHeat
	if math.Abs(err) > 1 {
		needleCol = colBad
	}
	vector.StrokeLine(screen, float32(npos), float32(py+26), float32(npos), float32(py+94), 3, needleCol, false)
	ui.DrawText(screen, "STEEP", nx-4, py+98, 0.6)
	ui.DrawText(screen, "SHALLOW", nx+nw-56, py+98, 0.6)
	ui.DrawText(screen, fmt.Sprintf("CORRIDOR  γ %+.2f°  ref %+.2f°", s.Gamma*180/math.Pi, s.RefG), nx, py+12, 1)

	// crossrange strip
	cs := math.Min(math.Max(s.Crossrange/40, -1), 1)
	vector.DrawFilledRect(screen, float32(nx), float32(py+120), float32(nw), 8, colPanel, false)
	vector.StrokeLine(screen, float32(mid), float32(py+118), float32(mid), float32(py+130), 1, colDim, false)
	vector.DrawFilledCircle(screen, float32(mid+cs*nw/2), float32(py+124), 4, colN2, false)
	ui.DrawText(screen, fmt.Sprintf("CROSSRANGE %+.1f km", s.Crossrange), nx, py+134, 0.8)

	// right: budgets
	bx := 640.0
	feedShow := e.feed
	if e.auto {
		feedShow = s.FeedUsed / 0.2
	}
	hbar(screen, bx, py+30, 180, feedShow, colLi, fmt.Sprintf("LI FEED %3.0f g/s  [ ]", s.FeedUsed*1000))
	hbar(screen, bx, py+66, 180, s.Li/s.Veh.LiTank, colLi, fmt.Sprintf("LI RESERVE %.1f kg", s.Li))
	hbar(screen, bx, py+102, 180, s.Pt.PowerDraw/s.Veh.PowerCap, colOI,
		fmt.Sprintf("POWER %.1f / %.1f MW", s.Pt.PowerDraw/1e6, s.Veh.PowerCap/1e6))
	hbar(screen, bx, py+138, 180, 1-s.Dmg.Hull/100, colPhos, fmt.Sprintf("HULL %3.0f%%", 100-s.Dmg.Hull))

	// far right: auto lamp, boost, skip warning
	ax := 850.0
	autoTxt, autoCol := "AUTO OFF   (A)", colDim
	switch {
	case s.Dmg.Computer >= 60:
		autoTxt, autoCol = "AUTO FAILED", colBad
	case e.auto && s.Dmg.Computer > 30:
		autoTxt, autoCol = "AUTO DEGRADED", colHeat
	case e.auto:
		autoTxt, autoCol = "AUTO ENGAGED", colOI
	}
	vector.DrawFilledCircle(screen, float32(ax+8), float32(py+36), 6, autoCol, false)
	ui.DrawText(screen, autoTxt, ax+22, py+28, 1)
	ui.DrawText(screen, fmt.Sprintf("BOOST x%d (B)", s.BoostLeft), ax, py+60, 1)
	ui.DrawText(screen, "BURST (SPC)", ax, py+80, 1)
	if w := s.SkipWarn(); w > 0.1 {
		blink := float32(0.4 + 0.6*math.Abs(math.Sin(s.T*6)))
		ui.DrawText(screen, "SKIP WARNING", ax, py+110, blink)
		hbar(screen, ax, py+138, 150, w, colEM, "")
	}
}

func (a *App) drawEntryOutcome(screen *ebiten.Image) {
	s := a.entry.sim
	var title, detail string
	switch s.Status() {
	case reentry.Landed:
		sc := s.Score()
		title = "TOUCHDOWN — " + a.gal.Stellars[a.entry.stellar].Name
		detail = fmt.Sprintf("hull %.0f%%  ·  %.1f km off the pad line  ·  bonus %d cr  ·  repairs %d cr",
			sc.HullLeft, sc.CrossKm, sc.PadBonus, sc.RepairCost)
	case reentry.SkippedOut:
		title = "CORRIDOR ABORT — SKIPPED OUT"
		detail = "the atmosphere threw you back; re-circularising costs fuel and a day"
	case reentry.Destroyed:
		title = "BREAKUP IN THE PLASMA"
		detail = "the pillow can only forgive so much"
	}
	w, h := 620.0, 110.0
	x, y := (ScreenW-w)/2, 240.0
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(w), float32(h), color.RGBA{5, 7, 10, 245}, false)
	vector.StrokeRect(screen, float32(x), float32(y), float32(w), float32(h), 1, colChrome, false)
	ui.DrawText(screen, title, x+20, y+22, 1)
	ui.DrawText(screen, detail, x+20, y+52, 0.9)
	ui.DrawText(screen, "press any key", x+20, y+80, 0.6)
}
