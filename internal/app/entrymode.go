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

// The entry cockpit: the render view converted into a landing simulator.
// The pilot flies the corridor needle like a glideslope; the plasma pillow
// is drawn as three particle populations colored by the emission lines of
// the species in the shield model, with the reflective "one-way mirror"
// shell as a bright arc standing off the nose.

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

type plasmaParticle struct {
	x, y, vx, vy float64
	life, span   float64
	kind         int // 0 Li streamer, 1 N2 sheath, 2 OI afterglow
}

type entryState struct {
	sim      *reentry.Sim
	stellar  int // where we are landing
	feed     float64
	auto     bool
	parts    []plasmaParticle
	doneWait float64 // seconds shown on the outcome card before returning
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
	a.entry = &entryState{sim: sim, stellar: stellarID, feed: 0.1}
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
		a.updatePlasma(c)

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

// updatePlasma runs the three emission populations. Spawn rates track the
// physics: streamers follow the feed, the sheath follows heat flux, the
// afterglow follows speed.
func (a *App) updatePlasma(c reentry.Controls) {
	e, s := a.entry, a.entry.sim
	cx, cy := float64(ScreenW)/2, 300.0
	stand := 40 + 26*(s.Pt.Standoff-1)
	qFrac := s.Pt.QShielded / s.Veh.TPSLimit

	spawn := func(kind int, n int) {
		for i := 0; i < n; i++ {
			ang := (a.voy.Rng.Float64() - 0.5) * 2.2 // fan under the nose
			px := cx + math.Sin(ang)*stand
			py := cy + math.Cos(ang)*stand*0.62
			sp := 60 + a.voy.Rng.Float64()*90
			e.parts = append(e.parts, plasmaParticle{
				x: px, y: py,
				vx: math.Sin(ang)*sp*0.5 + (a.voy.Rng.Float64()-0.5)*20,
				vy: -sp * (0.6 + 0.4*a.voy.Rng.Float64()),
				span: 0.5 + a.voy.Rng.Float64()*float64(kind+1),
				kind: kind,
			})
		}
	}
	feedN := int(c.Feed*6) + 1
	spawn(0, feedN)
	spawn(1, 1+int(qFrac*8))
	if s.V > 2000 {
		spawn(2, 2)
	}

	live := e.parts[:0]
	for _, p := range e.parts {
		p.life += dt
		p.x += p.vx * dt
		p.y += p.vy * dt
		p.vy -= 12 * dt // the slipstream carries everything up-screen
		if p.life < p.span {
			live = append(live, p)
		}
	}
	e.parts = live
	if len(e.parts) > 900 {
		e.parts = e.parts[len(e.parts)-900:]
	}
}

func (a *App) drawEntry(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	cx, cy := float64(ScreenW)/2, 300.0

	// the planet fills the bottom as the limb rises with descent
	limb := 690 - 240*(1-s.H/122000)
	vector.DrawFilledRect(screen, 0, float32(limb), ScreenW, float32(ScreenH)-float32(limb),
		color.RGBA{14, 26, 34, 255}, false)
	vector.StrokeLine(screen, 0, float32(limb), ScreenW, float32(limb), 2,
		color.RGBA{29, 122, 36, 200}, false)

	// afterglow first (behind), then sheath, then streamers
	order := [3]int{2, 1, 0}
	cols := [3]color.RGBA{colLi, colN2, colOI}
	for _, kind := range order {
		for _, p := range e.parts {
			if p.kind != kind {
				continue
			}
			f := 1 - p.life/p.span
			c := premul(cols[kind], 0.78*f)
			r := float32(1.5 + 2.5*f)
			if kind == 1 {
				r += 2
			}
			vector.DrawFilledCircle(screen, float32(p.x), float32(p.y), r, c, false)
		}
	}

	// the mirror shell: a specular arc standing off the nose, dark inside
	stand := 40 + 26*(s.Pt.Standoff-1)
	shellCol := colEM
	if s.Pt.InteractionQ < 1 {
		// authority fading: the mirror thins and lets the violet through
		shellCol = premul(colEM, 0.35+0.4*s.Pt.InteractionQ)
	}
	for i := 0; i < 24; i++ {
		a0 := -1.25 + 2.5*float64(i)/24
		a1 := -1.25 + 2.5*float64(i+1)/24
		vector.StrokeLine(screen,
			float32(cx+math.Sin(a0)*stand), float32(cy+math.Cos(a0)*stand*0.62),
			float32(cx+math.Sin(a1)*stand), float32(cy+math.Cos(a1)*stand*0.62),
			3, shellCol, false)
	}

	// the ship, nose down into the pillow
	sprite := a.Catalog.Get(a.Cfg.PlayerShipID).SpriteFor(180)
	b := sprite.Bounds()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(cx-float64(b.Dx())/2, cy-float64(b.Dy())-8)
	screen.DrawImage(sprite, op)

	a.drawEntryGauges(screen)

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
		color.RGBA{94, 230, 138, 26}, false)
	vector.StrokeRect(screen, float32(mid-half), float32(py+30), float32(half*2), 60, 1,
		color.RGBA{94, 230, 138, 120}, false)
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
	hbar(screen, bx, py+30, 180, e.feed, colLi, fmt.Sprintf("LI FEED %3.0f g/s  [ ]", e.feed*200))
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
