package app

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/assets"
	"yodacon.org/gonex/internal/city"
	"yodacon.org/gonex/internal/fx"
	"yodacon.org/gonex/internal/power"
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

// The corridor is not flown in real time: a ~10-minute entry plays in
// about half a minute. The sim's RK4 is stable well past this step
// (reentry_test covers the exact game rate).
const entryTimeScale = 18.0

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
	phase        float64 // 0 far upstream → 1 deep wake; drives the color ramp
	bounced      bool    // has it hit the mirror shell yet
}

type groundDot struct {
	lat, ahead float64 // km left/right of track, km ahead of the ship
}

type trailPt struct {
	dr, h, v float64 // m downrange, m altitude, m/s velocity
}

// shockPuff is one pressure ring leaving a hull station — the X-59
// diagram's arcs: circles born along the body, expanding and thinning as
// the shock walks away from the skin.
type shockPuff struct {
	dx, dy  float64 // station offset from the hull center, screen px
	r, life float64
	span    float64
}

// maskEdge is one point on the expanded hull mask: offset from the sprite
// center plus the outward normal — where the envelope fire lives.
type maskEdge struct {
	dx, dy, nx, ny float64
}

type nebBlob struct {
	x, y, r float64
	c       color.RGBA
	al      float64
}

type entryState struct {
	sim        *reentry.Sim
	stellar    int // where we are landing
	feed       float64
	auto       bool
	bank       float64   // smoothed visual bank, radians
	roll       float64   // last roll command — the shell's lobe sits opposite it
	flash      float64   // white-in inherited from the deorbit plasma onset
	pipeCalled bool      // the dropship call, made once at the aero handoff
	dayPhase0  float64   // where the sun's fast clock stood at interface
	boomT      float64   // sonic-boom animation clock; <0 = not triggered
	boomFine   int       // the noise-abatement fine waiting at the pad
	trail      []trailPt // downrange/altitude history for the orbit inset
	parts      []plasmaParticle
	puffs      []shockPuff // pressure rings walking the hull stations
	puffCD     float64

	// the ILS final: once the corridor is flown the computer captures the
	// glideslope and flies the last kilometres like an airliner — the city
	// rides the horizon and closes at exactly the ground speed shown
	finalT    float64 // <0: not started; else seconds on the approach
	finalRun  float64 // km along the runway axis, threshold = 0
	finalH    float64 // km, on the 3° slope
	finalSpd  float64 // km/s ground closure — what VEL reads
	finalDone bool

	// appRange is THE speed reference: the city's distance, anchored to
	// the velocity tape — it reaches the glideslope-capture range exactly
	// as the tape reaches the capture speed, monotonically, so the world's
	// closure and the shown speed can never disagree and never jump.
	appRange float64
	appV0    float64 // tape reading when the port came over the horizon
	ground   []groundDot
	doneWait float64

	// baked from the ship sprite's alpha mask at entry start
	edges     []maskEdge    // shield contour points, fire spawn sites
	glowImg   *ebiten.Image // radial fire falloff hugging the hull
	shieldImg *ebiten.Image // the expanded-mask shield band
	nebula    []nebBlob     // the high sky
	port      *city.Port    // the landing site, grown from the stellar seed

	// the two bow waves, both fire grids bent onto their own arcs: the
	// plasma sheath burning on the standoff shell (hypersonic phase), and
	// the white condensation cloud on the Mach cone (aero phase)
	bowFire   *fx.Fire
	machCloud *fx.Fire

	// expected is the reference profile: the same seed's autoland, flown
	// headless at entry start — the h–V line the corridor monitor plots
	// the live trace against, exactly the console prototype's h–V plane
	expected     []trailPt
	karmanCalled bool // the 100 km callout, made once on the way down

	// the pipe made legible: hullRate is smoothed %-per-sim-second hull
	// loss (drives the BURNING warnings), pred is the dotted green
	// future-position ladder from Sim.Predict, fuelBurn accumulates the
	// jump fuel the damage-control surge eats
	lastHull float64
	hullRate float64
	pred     []reentry.PredPt
	fuelBurn float64
}

// The plasma ramp: the sheath read as layers, front to wake — blue at the
// bow where the flow first ignites, a yellow combustion band at the shell,
// blue again over the shoulders, then pink → white → red as the deflected
// stream wraps aft along the field lines and recombines in the wake.
var plasmaStops = [6]color.RGBA{
	{92, 158, 255, 255},  // bow blue
	{255, 214, 92, 255},  // yellow plasma at the mirror
	{108, 172, 255, 255}, // shoulder blue
	{255, 122, 198, 255}, // flank pink
	{255, 250, 244, 255}, // white-hot midwake
	{255, 72, 58, 255},   // red recombination glow
}

// plasmaRamp lerps the six stops. Straight per-channel float math on the
// render side — no trig, no allocation — so nine hundred particles cost
// nothing.
func plasmaRamp(t float64) color.RGBA {
	if t <= 0 {
		return plasmaStops[0]
	}
	if t >= 1 {
		return plasmaStops[5]
	}
	f := t * 5
	i := int(f)
	f -= float64(i)
	a, b := plasmaStops[i], plasmaStops[i+1]
	return color.RGBA{
		uint8(float64(a.R) + (float64(b.R)-float64(a.R))*f),
		uint8(float64(a.G) + (float64(b.G)-float64(a.G))*f),
		uint8(float64(a.B) + (float64(b.B)-float64(a.B))*f),
		255,
	}
}

// buildShield reads the ship sprite's alpha mask, dilates it outward, and
// bakes the overlays the entry scene wraps around the hull: the fire-glow
// falloff (the bow wave burning on the envelope) and the shield band (the
// expanded mask itself). It also records the contour points with outward
// normals — the envelope fire's spawn sites. One BFS distance transform at
// entry start; nothing per-frame.
func (e *entryState) buildShield(sprite image.Image) {
	const reach = 14
	b := sprite.Bounds()
	w, h := b.Dx(), b.Dy()
	W, H := w+2*reach, h+2*reach
	const inf = 1 << 20
	dist := make([]int, W*H)
	orig := make([]int32, W*H) // nearest hull pixel, packed x<<16|y
	for i := range dist {
		dist[i] = inf
	}
	type qp struct{ x, y int }
	var queue []qp
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, _, al := sprite.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if al > 0x2000 {
				i := (y+reach)*W + x + reach
				dist[i] = 0
				orig[i] = int32(x+reach)<<16 | int32(y+reach)
				queue = append(queue, qp{x + reach, y + reach})
			}
		}
	}
	for qi := 0; qi < len(queue); qi++ {
		p := queue[qi]
		i := p.y*W + p.x
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, ny := p.x+d[0], p.y+d[1]
			if nx < 0 || ny < 0 || nx >= W || ny >= H {
				continue
			}
			j := ny*W + nx
			if dist[j] > dist[i]+1 {
				dist[j] = dist[i] + 1
				orig[j] = orig[i]
				queue = append(queue, qp{nx, ny})
			}
		}
	}
	glow := image.NewRGBA(image.Rect(0, 0, W, H))
	shield := image.NewRGBA(image.Rect(0, 0, W, H))
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			i := y*W + x
			d := dist[i]
			if d == inf || d == 0 {
				continue
			}
			fd := float64(d)
			if fd <= 12 {
				// reentry-photo fire: white-amber core falling off fast
				al := math.Pow(1-fd/12, 1.8)
				glow.SetRGBA(x, y, color.RGBA{
					uint8(255 * al), uint8(212 * al), uint8(146 * al), uint8(255 * al)})
			}
			if d >= 5 && d <= 8 {
				al := 1 - math.Abs(fd-6.5)/2
				shield.SetRGBA(x, y, color.RGBA{
					uint8(110 * al), uint8(225 * al), uint8(255 * al), uint8(210 * al)})
			}
			if d == 7 && (x+y)%2 == 0 {
				o := orig[i]
				nx, ny := float64(x)-float64(o>>16), float64(y)-float64(o&0xffff)
				if l := math.Hypot(nx, ny); l > 0 {
					e.edges = append(e.edges, maskEdge{
						dx: float64(x) - float64(W)/2, dy: float64(y) - float64(H)/2,
						nx: nx / l, ny: ny / l})
				}
			}
		}
	}
	e.glowImg = ebiten.NewImageFromImage(glow)
	e.shieldImg = ebiten.NewImageFromImage(shield)
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
	veh := entryVehicleFor(a.Catalog.Get(a.Cfg.PlayerShipID))
	veh.LiTank = a.voy.Lithium
	veh.RCSTank = a.voy.RCSFuel
	if a.voy.Grid != nil {
		// the outfitter's mass comes home to roost: every generator and
		// battery bought raises the ballistic coefficient right here
		veh.Mass += a.voy.Grid.OutfitKg
	}
	// so does the hold: every ton on the commodity board rides the
	// corridor down. RefMass stays at the design figure, so a full (or
	// overstuffed) deck reads on the LOAD dial and stiffens the stick.
	veh.Mass += float64(a.voy.CargoTotal()) * 1000
	seed := a.voy.Rng.Int63()
	sim := reentry.New(veh, prof, seed)
	sim.Dmg = a.voy.Dmg // damage carries in from the life you've led
	e := &entryState{sim: sim, stellar: stellarID, feed: 0.1, boomT: -1,
		finalT: -1, appRange: -1,
		bowFire:   fx.NewFire(44, 13, seed),
		machCloud: fx.NewFire(36, 9, seed+1),
		expected:  flyExpected(veh, prof, seed),
	}
	e.machCloud.Cooling = 0.988 // condensation lingers; plasma doesn't
	e.lastHull = sim.Dmg.Hull   // carried-in damage is not "burning now"
	for i := 0; i < 110; i++ {
		e.ground = append(e.ground, groundDot{
			lat:   (a.voy.Rng.Float64() - 0.5) * 56,
			ahead: 1.5 + a.voy.Rng.Float64()*46,
		})
	}
	// the shield is the ship: expand this hull's own mask
	if src, err := assets.Decode(
		"data/ships/" + a.Catalog.Get(a.Cfg.PlayerShipID).Folder + "/00.tga"); err == nil {
		e.buildShield(src)
	}
	// the landing site: same stellar, same town
	e.port = city.Generate(int64(stellarID) * 7919)
	// the sun's clock: where in the day this landing begins
	e.dayPhase0 = math.Mod(float64(a.voy.Day)*0.37+float64(stellarID%7)*0.11, 1)
	// the high sky: an even nebula wash, gone once the air thickens
	nebCols := []color.RGBA{{150, 96, 205, 255}, {84, 178, 190, 255},
		{190, 110, 170, 255}, {96, 120, 210, 255}}
	for i := 0; i < 8; i++ {
		e.nebula = append(e.nebula, nebBlob{
			x:  60 + a.voy.Rng.Float64()*(float64(ScreenW)-120),
			y:  30 + a.voy.Rng.Float64()*(horizonBase-130),
			r:  50 + a.voy.Rng.Float64()*85,
			c:  nebCols[i%len(nebCols)],
			al: 0.05 + a.voy.Rng.Float64()*0.06,
		})
	}
	if a.demoStellar > 0 {
		e.auto = true // the scripted pilot trusts the computer
	}
	a.entry = e
	a.mode = modeEntry
	a.miniMapWin.Visible, a.hudWin.Visible, a.targetWin.Visible = false, false, false
	a.fullMapWin.Visible, a.galaxyWin.Visible = false, false
	a.Console.Notifyf("ENTRY INTERFACE — %s. Fly the needle.", st.Name)
}

// flyExpected flies the whole entry headless on the flight computer before
// the player touches the stick: same vehicle, same profile, same seed. The
// result is the expected h–V profile — the corridor monitor's reference
// line, and the honest answer to "what should this descent look like".
func flyExpected(veh reentry.Vehicle, prof reentry.Profile, seed int64) []trailPt {
	s := reentry.New(veh, prof, seed)
	c := reentry.Controls{Auto: true}
	out := []trailPt{{dr: s.Downrange, h: s.H, v: s.V}}
	for i := 0; s.Status() == reentry.Flying && i < 16000; i++ {
		s.Step(0.2, c)
		if i%12 == 0 {
			out = append(out, trailPt{dr: s.Downrange, h: s.H, v: s.V})
		}
	}
	return out
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

		// the grid runs the coil: entry drains the deep store while the
		// radiators fly blind inside the sheath. An overdrive is a
		// capacitor transaction; an empty battery is a bare-body descent.
		if gd := a.voy.Grid; gd != nil {
			if c.Boost && gd.SpendCap(boostCapMJ) < 1 {
				c.Boost = false
				a.engNotify("Coil overdrive refused — capacitors dry.")
			}
			fl := gd.Step(dt*entryTimeScale, power.Load{
				Coil:  s.Pt.PowerDraw / 1e6,
				Hotel: 0.3,
				// off the corridor the flow scrubs the hull directly:
				// visible heat buildup on the gauge, fast
				HeatMW: 1.2*s.Pt.QShielded/s.Veh.TPSLimit + 2.5*s.OffCorridor,
			})
			s.Supply = fl.Served
			if fl.Overheat > 0 {
				s.Dmg.Computer = math.Min(
					s.Dmg.Computer+3*fl.Overheat*dt*entryTimeScale, 100)
			}
			a.engNoteCD = math.Max(a.engNoteCD-dt, 0)
			if fl.FromBatt > 0 && gd.BattMJ <= 0 {
				a.engNotify("BATTERY FLAT — coil authority collapsing.")
			}
		}
		s.Step(dt*entryTimeScale, c)
		a.voy.Lithium = s.Li
		a.voy.RCSFuel = s.RCS

		// the pipe's teaching aids: smoothed hull-loss rate for the
		// BURNING warnings, and the future-position ladder
		inst := math.Max(s.Dmg.Hull-e.lastHull, 0) / (dt * entryTimeScale)
		e.lastHull = s.Dmg.Hull
		e.hullRate += (inst - e.hullRate) * math.Min(5*dt, 1)
		e.pred = s.Predict(60, 4)

		// the damage-control surge runs the plant hot: recovery burns
		// jump fuel on top of the battery and the vented RCS
		if s.Recovering() {
			e.fuelBurn += 0.2 * dt * entryTimeScale
			if e.fuelBurn >= 1 && a.voy.Fuel > 0 {
				e.fuelBurn--
				a.voy.Fuel--
			}
		}

		// the handoff call: plasma lets go, the airframe takes it
		if !e.pipeCalled && s.AeroAuth > s.PlasmaAuth && s.T > 30 {
			e.pipeCalled = true
			a.Console.Notifyf("We're in the pipe, five by five.")
		}

		// too steep and fast over the port: still supersonic under 10 km
		// means a boom carpet across the whole town — and a fine
		if e.boomT < 0 && s.H < 10000 && s.Pt.Mach > 1.1 {
			e.boomT = 0
			e.boomFine = 1500 + int(s.Pt.Mach*s.Pt.Mach*400)
			a.Console.Notifyf("SONIC BOOM over the port — that profile is exploding windows.")
		} else if e.boomT >= 0 {
			e.boomT += dt
		}

		// visual bank chases the roll command (autoland's roll shows too)
		roll := c.Roll
		if e.auto {
			roll = math.Min(math.Max(s.Crossrange*0.12, -0.6), 0.6)
		}
		e.bank += (roll*0.48 - e.bank) * math.Min(3.5*dt, 1)
		e.roll += (roll - e.roll) * math.Min(6*dt, 1)
		e.flash = math.Max(e.flash-1.4*dt, 0)

		a.updatePlasma(c)
		a.updateGround()

		// the approach inset's memory: one fix every few seconds
		if n := len(e.trail); n == 0 || s.T-float64(n)*3 > 0 {
			e.trail = append(e.trail, trailPt{dr: s.Downrange, h: s.H, v: s.V})
		}

		// the edge of space, called once on the way through
		if !e.karmanCalled && s.H < reentry.KarmanLine {
			e.karmanCalled = true
			a.Console.Notifyf("KARMAN LINE — 100 km. The world is coming up to meet you.")
		}

		// the speed reference: once the port is over the horizon its
		// range rides the velocity tape down — 90 km at the tape's first
		// reading, the 4 km capture range exactly as the tape reaches
		// capture speed, monotonic in between
		if e.appRange < 0 && s.H < 30000 {
			e.appRange, e.appV0 = 90, math.Max(s.V, 1500)
		}
		if e.appRange > 0 {
			f := math.Min(math.Max((s.V-300)/(e.appV0-300), 0), 1)
			e.appRange = math.Min(e.appRange, 4+86*f)
		}

		if s.Status() != reentry.Flying {
			a.finishEntry()
		}
		return
	}

	// a survivable corridor exit rolls into the ILS final: the computer
	// captures the glideslope and flies the approach into the city
	if s.Status() == reentry.Landed && !e.finalDone {
		if e.finalT < 0 {
			// the final inherits the seam exactly: same range the city is
			// already at, same speed the tape is already showing
			rng := math.Max(e.appRange, 3.5)
			e.finalT, e.finalRun = 0, -rng
			e.finalH = 0.0524 * rng
			e.finalSpd = math.Max(s.V/1000, 0.24)
			a.Console.Notifyf("DESTINATION SPACEPORT — ILS glideslope captured. Cleared autoland.")
		}
		// like the corridor, the final is not flown in real time: the
		// approach, flare and rollout run at 20x wall clock — the same
		// slope over the same ground, just without the taxi-speed wait.
		// finalT stays on wall time so the HUD wobble keeps its period.
		const finalTimeScale = 20.0
		fdt := dt * finalTimeScale
		e.finalT += dt
		switch {
		case e.finalRun < 0: // the slope: h rides 3° above the ground run
			e.finalRun += e.finalSpd * fdt
			e.finalH = math.Max(-e.finalRun*0.0524, 0.02)
			if e.finalRun > -0.35 {
				e.finalSpd = math.Max(e.finalSpd-0.16*fdt, 0.22) // the flare
			}
		default: // rollout on the spaceport road
			e.finalSpd = math.Max(e.finalSpd-0.22*fdt, 0)
			e.finalRun += e.finalSpd * fdt
			e.finalH = 0.012
			if e.finalSpd <= 0.01 {
				e.finalDone = true
			}
		}
		return
	}

	// outcome card: give it a beat, then any key continues
	// (the scripted pilot presses on after three seconds)
	e.doneWait += dt
	if e.doneWait > 1.2 && (len(inpututil.AppendJustPressedKeys(nil)) > 0 ||
		(a.demoStellar > 0 && e.doneWait > 3)) {
		switch s.Status() {
		case reentry.Landed:
			a.dock = &dockState{stellar: e.stellar}
			a.mode = modeLanded
			a.voy.LandAt(e.stellar, a.gal)
			a.drainNotices()
			a.saveGame() // every touchdown writes the berth save DED resumes from
			a.Console.Notifyf("Berth save written.")
		case reentry.Destroyed:
			if st := a.gal.Stellars[e.stellar]; st != nil {
				a.deadPlace = st.Name
			}
			a.entry = nil
			a.deadT = 0
			a.mode = modeDead
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
		if e.boomFine > 0 {
			a.voy.Credits -= e.boomFine
			if a.voy.Credits < 0 {
				a.voy.Credits = 0
			}
			a.Console.Notifyf("NOISE-ABATEMENT FINE: %d cr — your supersonic profile exploded windows across the port.",
				e.boomFine)
		}
	case reentry.SkippedOut:
		a.voy.Fuel -= 50 // re-circularising costs
		a.voy.passDays(1, a.gal)
		a.Console.Notifyf("CORRIDOR ABORT — skipped out. Re-entry costs fuel and a day.")
	case reentry.Destroyed:
		a.Console.Notifyf("The %s broke up in the plasma over %s.",
			a.Catalog.Get(a.Cfg.PlayerShipID).Name, a.gal.Stellars[e.stellar].Name)
	}
	a.drainNotices()
}

// --- scene state ------------------------------------------------------

// seamT is the orbital→glideslope seam: 0 anywhere on the corridor, 1 at
// the parking threshold — where the entry frame must EQUAL the ILS
// final's first frame, so the handoff has no cut at all.
func (e *entryState) seamT() float64 {
	return math.Min(math.Max((650-e.sim.V)/350, 0), 1)
}

// horizonY is where the limb sits: γ steeper → camera looks further down
// → the horizon rides higher on screen. The chase camera levels off past
// −9°: in the terminal sink (γ → −80°) it keeps the world in frame while
// the instruments keep reading the true angle. Across the seam it eases
// to the ILS final's horizon, one continuous slide.
func (e *entryState) horizonY() float64 {
	gam := math.Max(e.sim.Gamma*180/math.Pi, -9)
	hy := horizonBase + (gam+2.4)*22
	tF := e.seamT()
	return hy*(1-tF) + 330*tF
}

// perspK is the ground-projection constant: it grows as the deck nears, so
// the surface visibly swells up at the camera on final.
func (e *entryState) perspK() float64 {
	hkm := math.Max(e.sim.H/1000, 1.5)
	return 640 * (1 + 9/hkm)
}

// fadeInLog fades a surface tier in as altitude falls below atKm — on a
// log10 scale, a third of a decade to full. The landing camera thinks in
// orders of magnitude: interface at 122 km down to a 2 km pad is five
// decades, and each tier of surface detail (limb → shores → city lights →
// terrain → pad) owns roughly one of them.
func fadeInLog(hkm, atKm float64) float64 {
	v := (math.Log10(atKm) - math.Log10(math.Max(hkm, 0.05))) * 3
	return math.Min(math.Max(v, 0), 1)
}

// project maps a surface feature (km off-track, km ahead) into the chase
// frame. The simulation stays in float64 — Go's native number — end to end,
// and collapses to float32 only at the draw call, which is the fast path
// Ebitengine's renderer wants.
func (e *entryState) project(latKm, aheadKm float64) (x, y float64, ok bool) {
	if aheadKm < 1.2 {
		return 0, 0, false
	}
	hy := e.horizonY()
	x = shipX + (latKm-e.sim.Crossrange)/aheadKm*430
	y = hy + e.perspK()/aheadKm
	return x, y, y <= gaugeTop
}

// hash31 is a tiny integer mix for the procedural surface — fast,
// allocation-free, stable per world cell, so the same city is always in
// the same place on the same planet.
func hash31(i, j int) uint32 {
	h := uint32(i)*374761393 + uint32(j)*668265263
	h = (h ^ (h >> 13)) * 1274126177
	return h ^ (h >> 16)
}

// shipDrawY is where the hull center sits; the ship draws at 1.35x.
const (
	shipDrawY = shipY - 32
	shipScale = 1.35
)

// dipoleB is the ship-as-bar-magnet field at offset (dx,dy) from the hull
// center, dipole moment m̂ along the ship's axis (nose = north pole):
//
//	B ∝ [3(m̂·r̂)r̂ − m̂] / r³
//
// the classic closed loops — out of the north pole, around the flanks,
// back into the south. dx/dτ = B(x) is what the ignited plasma rides.
func dipoleB(dx, dy, mx, my float64) (bx, by float64) {
	r2 := dx*dx + dy*dy
	if r2 < 1 {
		r2 = 1
	}
	r := math.Sqrt(r2)
	ux, uy := dx/r, dy/r
	md := mx*ux + my*uy
	inv := 1 / (r2 * r)
	return (3*md*ux - mx) * inv, (3*md*uy - my) * inv
}

// updatePlasma flies the sheath from the horizon's perspective: neutral air
// streams in from the vanishing point ahead, small and slow with distance,
// swelling as it closes. Where it meets the envelope the bow wave sets on
// fire — the streak ignites, and from then on it is ionized and the coil
// owns it: it rides the bar-magnet dipole's field lines (dx/dτ = B) out of
// the bow, around the flanks and into the tail, while ram pressure drags
// the whole burning stream aft past the camera. Envelope fire also boils
// straight off the expanded hull mask, so the ship's own outline burns.
// The steerable cone tilts the dipole: rolling shifts the hardened lobe to
// the side opposite the turn, and the deflected stream pushes back.
func (a *App) updatePlasma(c reentry.Controls) {
	e, s := a.entry, a.entry.sim
	nose := float64(shipY - 46)
	standPx := 16 + 22*(s.Pt.Standoff-1)
	cx := shipX - e.bank*30 // deflection center rides the banked envelope
	cy := float64(shipDrawY)
	arxs := 40 + standPx*0.85 // standoff-cone semi-axes
	arys := math.Max(standPx, 8)
	hrx, hry := 62.0, 48.0 // hull-envelope reach, ≈ the dilated mask
	qFrac := s.Pt.QShielded / s.Veh.TPSLimit
	lobe := -e.roll // shield lobe sits opposite the steering direction
	hy := e.horizonY()

	// the dipole moment: ship's axis, tilted by the steerable cone
	mAng := e.bank*0.7 - e.roll*0.45
	mx, my := math.Sin(mAng), -math.Cos(mAng) // nose = north, up-screen

	// --- the two bow waves, each a fire grid bent onto its own arc.
	// The plasma fire eats sheath heat and lithium feed; its front leans
	// with the steering lobe, so rolling visibly drags the flame around
	// the shell the way the deflected stream actually goes.
	lobeV, rollAbs := lobe, math.Abs(e.roll)
	// FeedUsed is the lithium actually going in this frame — the
	// autoland's and the guardian's feed burn on the bow like the pilot's
	e.bowFire.Fuel = math.Min(qFrac*2.0+s.FeedUsed/0.2*0.45, 1.35) * s.Pt.Gate
	e.bowFire.Sweep = -e.roll * 2.4
	e.bowFire.FuelProfile = func(u float64) float64 {
		base := 1 - 0.45*u*u // stagnation-line weighted
		return base * (1 + 0.9*math.Max(0, lobeV*u)*rollAbs)
	}
	if c.Burst {
		e.bowFire.Boost(0.5)
	}
	if s.Boosting() {
		e.bowFire.Boost(0.06)
	}
	e.bowFire.Step(dt)
	// the Mach-speed bow wave: white condensation, volumetric, owned by
	// the aero phase — it condenses where the air is dense and the ship
	// is supersonic, and takes over exactly as the plasma lets go
	e.machCloud.Fuel = s.AeroAuth *
		math.Min(math.Max((s.Pt.Mach-0.85)/0.4, 0), 1)
	e.machCloud.Sweep = -e.roll * 1.4
	e.machCloud.Step(dt)

	rng := a.voy.Rng
	if s.V > 900 {
		// 1) the major drift: the flame river pouring out of the horizon.
		// Streams are born close to the vanishing point and ride the
		// sightline down onto the ship — the whole sky visibly feeding
		// the bow wave, which routes it around the shield.
		vpx, vpy := shipX, hy+8
		for i, n := 0, 6+int(qFrac*22); i < n; i++ {
			tx := cx + (rng.Float64()-0.5)*2*hrx*1.15
			ty := cy + (rng.Float64()-0.5)*36
			fr := rng.Float64()
			f := 0.03 + fr*fr*0.3 // biased hard toward the horizon
			px := vpx + (tx-vpx)*f + (rng.Float64()-0.5)*18
			py := vpy + (ty-vpy)*f
			dx, dy := tx-px, ty-py
			l := math.Hypot(dx, dy) + 1e-6
			sp := (300 + 500*(s.V/8350)) * (0.25 + f)
			e.parts = append(e.parts, plasmaParticle{
				x: px, y: py, vx: dx / l * sp, vy: dy / l * sp,
				span: 1.0 + rng.Float64()*0.6, scale: 0.12 + f*0.5,
			})
		}
		// 1b) out of the pipe: glowing damage traces tear off the hull —
		// long red streaks, the airframe visibly shedding material
		if s.OffCorridor > 0.05 && len(e.edges) > 0 {
			sinb, cosb := math.Sin(e.bank*0.7), math.Cos(e.bank*0.7)
			for i, n := 0, 1+int(s.OffCorridor*7); i < n; i++ {
				ed := e.edges[rng.Intn(len(e.edges))]
				ex := (ed.dx*cosb - ed.dy*sinb) * shipScale
				ey := (ed.dx*sinb + ed.dy*cosb) * shipScale
				e.parts = append(e.parts, plasmaParticle{
					x: shipX + ex, y: cy + ey,
					vx: ed.nx*90 + (rng.Float64()-0.5)*40, vy: 260 + rng.Float64()*160,
					span: 1.1 + rng.Float64()*0.8, scale: 0.9,
					bounced: true, phase: 0.95, // deep in the red
				})
			}
		}
		// 2) envelope fire: born burning on the expanded hull mask itself
		if len(e.edges) > 0 {
			sinb, cosb := math.Sin(e.bank*0.7), math.Cos(e.bank*0.7)
			for i, n := 0, 3+int(qFrac*16)+int(c.Feed*5); i < n; i++ {
				ed := e.edges[rng.Intn(len(e.edges))]
				if ed.ny > 0 && rng.Float64() < 0.6 {
					continue // the bow burns hardest
				}
				ex := (ed.dx*cosb - ed.dy*sinb) * shipScale
				ey := (ed.dx*sinb + ed.dy*cosb) * shipScale
				enx := ed.nx*cosb - ed.ny*sinb
				eny := ed.nx*sinb + ed.ny*cosb
				e.parts = append(e.parts, plasmaParticle{
					x: shipX + ex, y: cy + ey,
					vx: enx * (30 + rng.Float64()*40), vy: eny*30 + 40,
					span: 0.8 + rng.Float64()*0.7, scale: 0.6,
					bounced: true,
					phase:   0.12 + 0.5*math.Min(math.Max((ed.ny+1)/2, 0), 1),
				})
			}
		}
	}

	// shock puffs: while supersonic, pressure rings leave the hull at
	// stations along the body — nose, shoulders, tail — expanding faster
	// the harder the air is being hit
	if s.Pt.Mach > 1 {
		if e.puffCD -= dt; e.puffCD <= 0 {
			e.puffCD = 0.16 + rng.Float64()*0.14
			e.puffs = append(e.puffs, shockPuff{
				dx: (rng.Float64() - 0.5) * 34,
				dy: -46 + rng.Float64()*92,
				r:  5, span: 0.7 + rng.Float64()*0.5,
			})
		}
	}
	livePuffs := e.puffs[:0]
	for _, pf := range e.puffs {
		pf.life += dt
		pf.r += (55 + 22*math.Min(s.Pt.Mach, 12)) * dt
		if pf.life < pf.span {
			livePuffs = append(livePuffs, pf)
		}
	}
	e.puffs = livePuffs

	live := e.parts[:0]
	for _, p := range e.parts {
		p.life += dt
		switch {
		case !p.bounced:
			// neutral approach: blue bow layer, yellowing as it closes
			d := math.Hypot(p.x-cx, p.y-cy)
			p.phase = 0.2 * math.Min(math.Max(1-d/420, 0), 1)
			p.scale = math.Min(p.scale+1.1*dt, 1.2) // perspective swell
			// ignition surfaces: the standoff cone while the mirror holds,
			// else the bare envelope
			hit := false
			var nx, ny float64
			if s.Pt.Gate > 0.3 && p.y < nose {
				dxn, dyn := (p.x-cx)/arxs, (p.y-nose)/arys
				if dxn*dxn+dyn*dyn < 1 {
					hit, nx, ny = true, dxn, dyn
				}
			}
			if !hit {
				dxn, dyn := (p.x-cx)/hrx, (p.y-cy)/hry
				if dxn*dxn+dyn*dyn < 1 {
					hit, nx, ny = true, dxn, dyn
				}
			}
			if hit {
				nl := math.Hypot(nx, ny)
				if nl < 1e-6 {
					nl = 1e-6
				}
				nx, ny = nx/nl, ny/nl
				dot := p.vx*nx + p.vy*ny
				// the hardened lobe (opposite the turn) bounces harder
				k := 2 * (1 + 0.9*math.Max(0, lobe*math.Copysign(1, nx))*math.Abs(e.roll))
				p.vx -= k * dot * nx
				p.vy -= k * dot * ny
				// the mirror sheds most of the ram energy: keep the
				// deflected stream tight enough to read as flame
				if vl := math.Hypot(p.vx, p.vy); vl > 260 {
					p.vx, p.vy = p.vx/vl*260, p.vy/vl*260
				}
				p.bounced, p.phase = true, 0.24
			}
		default:
			// ionized: lock onto the dipole's field line and slide along
			// it, ram pressure dragging the burning stream aft. The lock
			// is stiff and the downstream drag light, so the fire traces
			// the lobes visibly before the wake claims it.
			bx, by := dipoleB(p.x-cx, p.y-cy, mx, my)
			if bl := math.Hypot(bx, by); bl > 1e-12 {
				sp := 190 + 240*qFrac
				tx, ty := bx/bl*sp, by/bl*sp+230
				p.vx += (tx - p.vx) * math.Min(6.5*dt, 1)
				p.vy += (ty - p.vy) * math.Min(6.5*dt, 1)
			}
			p.phase = math.Min(p.phase+dt*0.85, 1)
		}
		// past the ship the wake dives toward the camera: spread and grow
		grow := 1 + 2.2*math.Max(0, (p.y-nose)/(float64(ScreenH)-nose))
		p.scale += 1.1 * dt * grow * boolf(p.bounced)
		p.x += p.vx * dt * grow
		p.y += p.vy * dt * grow
		if !p.bounced && p.y > cy+80 {
			continue // missed the envelope: slipstream, not fire
		}
		if p.life < p.span && p.y < gaugeTop+40 &&
			p.x > -60 && p.x < float64(ScreenW)+60 {
			live = append(live, p)
		}
	}
	e.parts = live
	if len(e.parts) > 2000 {
		e.parts = e.parts[len(e.parts)-2000:]
	}
}

func boolf(b bool) float64 {
	if b {
		return 1
	}
	return 0.25
}

// updateGround streams the perspective dot field toward the camera — at
// the same real-time km/s the city closes at, so every moving reference
// on screen agrees with the speed tape.
func (a *App) updateGround() {
	e, s := a.entry, a.entry.sim
	kps := s.V / 1000 // km/s, the tape's number
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
	// the ILS final owns the world once the glideslope is captured — but
	// the cockpit furniture stays: same dials, same inset, one scene
	if e.finalT >= 0 {
		a.drawFinalApproach(screen)
		a.drawEntryDials(screen)
		a.drawOrbitInset(screen)
		if e.finalDone && s.Status() != reentry.Flying {
			a.drawEntryOutcome(screen)
		}
		return
	}
	hkm := s.H / 1000
	hy := e.horizonY()
	qFrac := s.Pt.QShielded / s.Veh.TPSLimit

	// the sun's fast clock: a landing crosses at least a quarter day
	sun := sunAt(e.dayPhase0 + s.T*0.0006)
	a.drawSky(screen, hkm, hy, e.bank, shipX, sun, e.nebula)

	// --- the limb: a curved arc, flattening as we descend. From orbit the
	// horizon itself is BLACK — the line you see is the thin blue
	// atmosphere BENEATH it, the classic edge-on airglow band. The world's
	// green horizon only takes over once the ship is down in the air.
	curve := 500 + (hkm/122)*-0 + (1-hkm/122)*11000 // px radius: 500 high → 11500 low
	wake := fadeInLog(hkm, reentry.KarmanLine/1000) // the world coming into view
	limbCol := color.RGBA{
		uint8(110 + (29-110)*wake), uint8(170 + (122-170)*wake),
		uint8(255 + (36-255)*wake), 255}
	// --- ground: opaque bands from the limb down to the panel — near
	// BLACK above the Kármán line, waking into terrain as the world comes
	// into view, then into daylight as the deck closes
	day := (1 + 2.4*math.Min(math.Max((30-hkm)/30, 0), 1)) * (0.35 + 0.65*sun) *
		(0.12 + 0.88*wake)
	for i := 0; i < 14; i++ {
		d := 20 + 38*float64(i)
		bx1, by1 := e.rot(-320, hy+d)
		bx2, by2 := e.rot(float64(ScreenW)+320, hy+d)
		vector.StrokeLine(screen, bx1, by1, bx2, by2, 40,
			color.RGBA{uint8(float64(11+i/3) * day), uint8(float64(22+i/2) * day),
				uint8(float64(27+i/2) * day), 255}, false)
	}
	// the seam: the orbital ground bands dissolve into the ILS final's
	// terrain as the ship slows — one surface, no cut
	tF := e.seamT()
	if tF > 0.01 {
		dayn := 0.3 + 0.7*sun
		fastRect(screen, 0, float32(hy), ScreenW, float32(float64(ScreenH)-hy),
			color.RGBA{uint8(28 + 40*dayn), uint8(24 + 32*dayn), uint8(18 + 24*dayn), 255}, tF)
	}
	// --- the atmosphere seen edge-on: the blue band BENEATH the black
	// horizon — layered arcs sinking into the dark planet, brightest right
	// under the limb, strongest from orbit, over the ground bands so the
	// planet cannot swallow it
	glowA := 0.6 * math.Min(math.Max((hkm-15)/50, 0.2), 1) * (1 - tF)
	var lx, ly float32
	for li, band := range [4]struct {
		off float64
		c   color.RGBA
		al  float64
	}{
		{3, color.RGBA{132, 190, 255, 255}, 1.0},
		{8, color.RGBA{74, 138, 245, 255}, 0.72},
		{15, color.RGBA{38, 84, 190, 255}, 0.46},
		{24, color.RGBA{18, 42, 120, 255}, 0.26},
	} {
		prevB := false
		var blx, bly float32
		for x := -60.0; x <= float64(ScreenW)+60; x += 24 {
			dx := x - shipX
			y := hy + dx*dx/(2*curve) + band.off
			px, py := e.rot(x, y)
			if prevB {
				vector.StrokeLine(screen, blx, bly, px, py, float32(4+li*3),
					premul(band.c, band.al*glowA), false)
			}
			blx, bly, prevB = px, py, true
		}
	}
	// the limb line itself: blue-white from orbit, the world's green low
	prev := false
	for x := -60.0; x <= float64(ScreenW)+60; x += 24 {
		dx := x - shipX
		y := hy + dx*dx/(2*curve)
		px, py := e.rot(x, y)
		if prev {
			vector.StrokeLine(screen, lx, ly, px, py, 2,
				premul(limbCol, 0.35+0.5*wake), false)
		}
		lx, ly, prev = px, py, true
	}
	groundVis := math.Min(math.Max((105-hkm)/50, 0), 1)
	if groundVis > 0 {
		// shore patterns: a procedural coastline wanders across the track,
		// resolving out of the haze a couple of decades up
		if sv := fadeInLog(hkm, 72) * groundVis * (1 - tF); sv > 0.02 {
			dr := s.Downrange / 1000
			shorePrev := false
			var sx, sy float32
			for ahead := 2.0; ahead <= 46; ahead += 2 {
				t := dr + ahead
				lat := 14*math.Sin(t*0.11) + 9*math.Sin(t*0.043+1.7)
				x, y, ok := e.project(lat, ahead)
				if !ok {
					shorePrev = false
					continue
				}
				rx, ry := e.rot(x, y)
				if shorePrev {
					fastLine(screen, sx, sy, rx, ry, 2,
						color.RGBA{96, 178, 186, 255}, sv*math.Min(8/ahead+0.2, 0.8))
				}
				sx, sy, shorePrev = rx, ry, true
			}
		}
		// city lights: hashed 8 km world cells, each a smear of warm dots
		// that twinkles and brightens as the decades fall away — small
		// towns, CONNECTED: a faint highway runs to the next town in the
		// row, so from altitude the surface reads as a settled network
		// the settled network is the FIRST thing the world shows: city
		// lights read from orbit, so they lead the wake-up at the Kármán
		// line — brightest on the night side, exactly like the real view
		if cv := fadeInLog(hkm, 95) * groundVis * (0.55 + 0.75*(1-sun)) * (1 - tF); cv > 0.02 {
			dr := s.Downrange / 1000
			c0 := int(dr) / 8
			var lastX, lastY [7]float32
			var lastSet [7]bool
			for ci := c0; ci < c0+7; ci++ {
				for cj := -3; cj <= 3; cj++ {
					h := hash31(ci, cj)
					if h%10 >= 3 {
						continue
					}
					ahead := float64(ci*8) + float64(h%53)/53*8 - dr
					lat := float64(cj*8) + float64(h%73)/73*6 - 3
					x, y, ok := e.project(lat, ahead)
					if !ok {
						continue
					}
					rx, ry := e.rot(x, y)
					if lastSet[cj+3] {
						fastLine(screen, lastX[cj+3], lastY[cj+3], rx, ry, 1,
							color.RGBA{230, 190, 130, 255}, cv*0.22)
					}
					lastX[cj+3], lastY[cj+3], lastSet[cj+3] = rx, ry, true
					tw := 0.7 + 0.3*math.Sin(s.T*3+float64(h%97))
					al := cv * tw * math.Min(12/ahead+0.15, 0.95)
					sz := math.Min(1+26/ahead, 7)
					for k := 0; k < 5; k++ {
						hk := hash31(int(h)+k, k)
						ox := (float64(hk%100)/50 - 1) * sz * 1.6
						oy := (float64((hk>>8)%100)/50 - 1) * sz * 0.7
						fastDot(screen, rx+float32(ox), ry+float32(oy),
							float32(sz*0.32+1.0), color.RGBA{255, 208, 132, 255}, al)
					}
				}
			}
		}
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
			al := groundVis * math.Min(6/g.ahead+0.15, 0.9) * (1 - tF)
			r := float32(math.Min(0.8+7/g.ahead, 4))
			fastDot(screen, rx, ry, r, color.RGBA{86, 148, 110, 255}, al)
		}
		// the city NEVER fades in: it appears as a speck ON the horizon,
		// directly down the road ahead, and slides toward the camera at
		// EXACTLY the velocity on the tape — appRange is the one number
		// the world, the HUD and the ILS final all read, so closure rate
		// and shown speed can never disagree.
		if e.port != nil && e.appRange > 0 {
			tG := math.Min(math.Max((20-hkm)/10, 0), 1)
			distBase := e.appRange
			pkPort := (pk*(1-tG) + 760*tG) * (1 - tF)
			pkPort += 116 * tF                  // = the final's eye: 3° slope at 4 km
			crossEff := s.Crossrange * (1 - tF) // the autoland lines it up
			proj := func(lat, ahead float64) (float64, float64, float64, bool) {
				dist := distBase + ahead
				if dist < 0.18 {
					return 0, 0, 0, false
				}
				x := shipX + (lat-crossEff)/dist*430
				y := hy + pkPort/dist
				return x, y, dist, y <= float64(ScreenH)+430
			}
			drawPortScene(screen, e.port, s.T, 1, sun, proj, e.rot)
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
				ui.DrawText(screen, fmt.Sprintf("SPACEPORT %.0f km", s.PadDist),
					float64(rx+sz)+6, float64(ry)-6, 0.8)
			}
		}
	}

	// --- heat veil: the whole sky reddens as q̇ climbs — and floods when
	// the ship is out of the pipe and the flow is scrubbing bare hull
	veil := math.Max(qFrac-0.15, 0)*0.22 + s.OffCorridor*0.20
	if veil > 0.01 {
		vector.DrawFilledRect(screen, 0, 0, ScreenW, gaugeTop,
			premul(color.RGBA{255, 120, 60, 255}, veil), false)
	}

	// --- the plasma stream: color is position in the flow, not species —
	// the ramp walks bow blue → shell yellow → shoulder blue → pink →
	// white → wake red as each particle rides its field line aft
	for _, p := range e.parts {
		f := 1 - p.life/p.span
		al := 0.75 * f * math.Min(p.life*6, 1)
		if !p.bounced {
			// neutral inflow: a long motion streak out of the vanishing
			// point — the river of air falling onto the bow
			fastLine(screen,
				float32(p.x-p.vx*0.09), float32(p.y-p.vy*0.09),
				float32(p.x), float32(p.y),
				float32(0.7+p.scale), plasmaRamp(p.phase), al*0.7)
			continue
		}
		// ignited: a flame lick — a streak along the velocity with a hot
		// core, so the deflected stream reads as fire, not confetti
		r := float32((1.0 + 1.5*f) * p.scale)
		col := plasmaRamp(p.phase)
		fastLine(screen,
			float32(p.x-p.vx*0.045), float32(p.y-p.vy*0.045),
			float32(p.x), float32(p.y), r*1.1, col, al*0.7)
		fastDot(screen, float32(p.x), float32(p.y), r*0.8, col, al)
	}

	// --- the mirror shell: the bright arc standing off ahead of the nose.
	// The steering lobe brightens and swells on the side OPPOSITE the roll
	// command — that hardened patch of mirror is what shoves the ship over.
	nose := shipY - 46.0
	standPx := 16 + 22*(s.Pt.Standoff-1)
	auth := math.Min(s.Pt.InteractionQ, 1)
	rx0 := 40 + standPx*0.85
	lobe := -e.roll
	// the plasma bow wave proper: the fire grid bent along the shell,
	// burning UNDER the mirror line — the sheath the ship pushes ahead
	drawBowFire(screen, e.bowFire, bowGeom{
		cx: shipX - e.bank*30, nose: nose, standPx: standPx,
		roll: e.roll, alpha: 0.5 + 0.5*math.Min(qFrac*2, 1),
	})
	prev = false
	for i := 0; i <= 22; i++ {
		ang := -1.25 + 2.5*float64(i)/22
		side := math.Sin(ang)
		swell := 1 + 0.22*math.Max(0, lobe*side)*math.Abs(e.roll)
		x := shipX + side*rx0*swell - e.bank*30
		y := nose - standPx*swell + (1-math.Cos(ang))*standPx*0.75
		px, py := float32(x), float32(y)
		if prev {
			hard := 0.35 + 0.55*auth + 0.35*math.Max(0, lobe*side)*math.Abs(e.roll)
			w := float32(3 + 2*math.Max(0, lobe*side)*math.Abs(e.roll))
			vector.StrokeLine(screen, lx, ly, px, py, w,
				premul(colEM, math.Min(hard, 1)), false)
			// the hull-facing side stays dark: a thin void line inside
			vector.StrokeLine(screen, lx, ly+4, px, py+4, 2,
				premul(color.RGBA{5, 7, 10, 255}, 0.5), false)
		}
		lx, ly, prev = px, py, true
	}

	// --- the burning envelope, in layers: bar-magnet field lines the
	// plasma rides, the fire glow (the bow wave burning on the expanded
	// mask), the hull itself, then the shield band over it.
	a.drawFieldLines(screen)
	shipImg := a.Catalog.Get(a.Cfg.PlayerShipID).Sprites[0]
	// across the seam the ship glides to the ILS final's anchor and scale;
	// through the descent the sprite foreshortens with the flight-path
	// angle — the hull visibly ALIGNED with the reentry slope, nosing
	// down the corridor and leveling out for the flare
	seamY := float64(shipDrawY) + (560-float64(shipDrawY))*tF
	seamScale := shipScale + (1.5-shipScale)*tF
	pitch := math.Min(math.Max(-s.Gamma/(math.Pi/2), 0), 1)
	slopeSquash := 1 - 0.32*pitch*(1-tF)
	place := func(img *ebiten.Image, extraScale float64) *ebiten.DrawImageOptions {
		bb := img.Bounds()
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(-float64(bb.Dx())/2, -float64(bb.Dy())/2)
		op.GeoM.Rotate(e.bank * 0.7)
		op.GeoM.Scale(seamScale*extraScale, seamScale*extraScale*slopeSquash)
		op.GeoM.Translate(shipX, seamY)
		return op
	}
	if e.glowImg != nil && s.V > 900 && qFrac > 0.02 {
		fire := math.Min(qFrac*2.2, 1)
		flick := 0.8 + 0.2*math.Sin(s.T*37+3*math.Sin(s.T*13))
		op := place(e.glowImg, 1+0.06*flick)
		op.ColorScale.Scale(1, float32(0.82+0.12*fire), float32(0.6+0.25*(1-fire)), 1)
		op.ColorScale.ScaleAlpha(float32(fire * (0.62 + 0.3*flick)))
		screen.DrawImage(e.glowImg, op)
	}
	screen.DrawImage(shipImg, place(shipImg, 1))

	// the shock puffs: thin rings expanding off the hull stations
	if len(e.puffs) > 0 {
		sinb, cosb := math.Sin(e.bank*0.7), math.Cos(e.bank*0.7)
		for _, pf := range e.puffs {
			cxp := shipX + (pf.dx*cosb-pf.dy*sinb)*shipScale
			cyp := float64(shipDrawY) + (pf.dx*sinb+pf.dy*cosb)*shipScale
			al := (1 - pf.life/pf.span) * 0.55
			prevSet := false
			var plx, ply float32
			for i := 0; i <= 18; i++ {
				th := 2 * math.Pi * float64(i) / 18
				px := float32(cxp + math.Cos(th)*pf.r)
				py := float32(cyp + math.Sin(th)*pf.r*0.72)
				if prevSet {
					fastLine(screen, plx, ply, px, py, 1.2,
						color.RGBA{225, 240, 250, 255}, al)
				}
				plx, ply, prevSet = px, py, true
			}
		}
	}

	if e.shieldImg != nil && s.Pt.Gate > 0.03 {
		op := place(e.shieldImg, 1+0.02*math.Sin(s.T*6))
		op.ColorScale.ScaleAlpha(float32(0.25 + 0.6*s.Pt.Gate))
		screen.DrawImage(e.shieldImg, op)
	}

	// --- the Mach-speed bow wave: the white condensation collar, owned by
	// the aero phase. It condenses in over the hull exactly as the plasma
	// fire dies — the visible form of the STEER handoff.
	drawMachCloud(screen, e.machCloud, shipX-e.bank*30, float64(shipDrawY)-14,
		s.Pt.Mach, s.AeroAuth)

	a.drawBoom(screen)
	a.drawEntryGauges(screen)
	a.drawEntryStrip(screen)
	a.drawEntryDials(screen)
	a.drawOrbitInset(screen)
	a.drawCorridorInset(screen)
	a.drawILSSide(screen)
	a.drawEntryHud(screen, hy) // the landing HUD flies the whole sequence
	a.drawTrajProjection(screen, hy)
	a.drawBurnWarnings(screen)

	// deorbit plasma white-in
	if e.flash > 0 {
		vector.DrawFilledRect(screen, 0, 0, ScreenW, ScreenH,
			premul(color.RGBA{255, 236, 220, 255}, e.flash), false)
	}

	if s.Status() != reentry.Flying {
		a.drawEntryOutcome(screen)
	}
}

// drawTrajProjection is the future, dotted in the HUD's green: Sim.Predict
// flies the current stick sixty seconds ahead, and each sample is placed
// at its true depression angle below the horizon (Δh over distance, on the
// same 22 px/deg the horizon itself rides). The corridor is painted with
// it — two dashed rails at the reference band's edges — so "fly the pipe"
// stops being a sentence and becomes geometry: keep the dots between the
// rails.
func (a *App) drawTrajProjection(screen *ebiten.Image, hy float64) {
	e, s := a.entry, a.entry.sim
	if s.Status() != reentry.Flying || len(e.pred) == 0 || e.seamT() > 0.6 {
		return
	}
	const pxPerDeg = 22.0
	// the pipe's rails at the CURRENT reference: where the first dots
	// should sit
	for _, edge := range [2]float64{s.RefG - s.Width, s.RefG + s.Width} {
		y := float32(hy + (-edge)*pxPerDeg)
		for x := shipX - 70; x < shipX+70; x += 14 {
			fastLine(screen, float32(x), y, float32(x+7), y, 1, hudGreen, 0.5)
		}
	}
	ui.DrawText(screen, "PIPE", shipX+80, hy+(-s.RefG)*pxPerDeg-4, 0.6)
	// the dots: the projected positions, red once they leave the band
	for k, p := range e.pred {
		dKm := (p.Downrange - s.Downrange) / 1000
		if dKm < 0.5 {
			continue
		}
		dropDeg := math.Atan2(s.H-p.H, dKm*1000) * 180 / math.Pi
		y := hy + dropDeg*pxPerDeg
		if y > gaugeTop-8 {
			break
		}
		errW := (p.Gamma*180/math.Pi - s.RefGamma(p.H)) /
			math.Max(s.CorridorWidth(p.H), 0.01)
		col := hudGreen
		if math.Abs(errW) > 1 {
			col = colBad
		}
		f := float64(k) / float64(len(e.pred))
		fastDot(screen, float32(shipX), float32(y), 2.4, col, 0.85*(1-0.45*f))
		if k == len(e.pred)-1 {
			vector.StrokeCircle(screen, float32(shipX), float32(y), 6, 1,
				premul(col, 0.8), false)
			ui.DrawText(screen, fmt.Sprintf("+%.0fs", p.T), shipX+12, y-4, 0.6)
		}
	}
}

// drawBurnWarnings is how the pilot LEARNS the pipe: the moment the hull
// is actually being spent, the screen says so, says how fast, and says
// which way to push. During the damage-control reflex the computer
// announces that it has the stick.
func (a *App) drawBurnWarnings(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	if s.Status() != reentry.Flying {
		return
	}
	blink := 0.55 + 0.45*math.Abs(math.Sin(s.T*7))
	center := func(txt string, y, scale float64, col color.RGBA, al float64) {
		x := float64(ScreenW)/2 - float64(len(txt))*3.6*scale
		ui.DrawTextScaled(screen, txt, x, y, scale, col, float32(al))
	}
	if s.Recovering() {
		center("EMERGENCY OVERRIDE", 150, 2, colHeat, blink)
		center("PULL UP! FLY THE PIPE!", 176, 2, colBad, blink)
	}
	if e.hullRate > 0.08 {
		// the red edges close in as the burn rate climbs
		edge := math.Min(0.10+e.hullRate*0.10, 0.4) * blink
		vector.DrawFilledRect(screen, 0, 0, 26, gaugeTop, premul(colBad, edge), false)
		vector.DrawFilledRect(screen, ScreenW-26, 0, 26, gaugeTop, premul(colBad, edge), false)
		center(fmt.Sprintf("HULL BURNING  -%.1f%%/s", e.hullRate), 210, 2, colBad, blink)
		err := s.GammaError()
		cue := "FEED THE SHIELD  [ ]"
		switch {
		case err < -s.Width:
			cue = "TOO STEEP — EASE THE NOSE UP"
		case err > s.Width:
			cue = "TOO SHALLOW — PUSH BACK DOWN"
		}
		center(cue, 238, 1, colHeat, blink)
	}
}

// sunAt is the fast clock: phase 0 is dawn, 0.25 noon, 0.5 dusk, then the
// long night. A landing runs at least a quarter of the cycle; a takeoff
// the same, in twelve seconds.
func sunAt(phase float64) float64 {
	return 0.18 + 0.82*math.Max(0, math.Sin(2*math.Pi*phase))
}

// drawBoom is the Concorde diagram come to life: the Mach cone leaves the
// ship as an expanding double ring, and the town answers with BOOOOMs over
// the skyline. Triggered by staying supersonic below 10 km — the fine is
// collected at the pad.
func (a *App) drawBoom(screen *ebiten.Image) {
	e := a.entry
	if e.boomT < 0 || e.boomT > 4 {
		return
	}
	t := e.boomT
	ring := func(r, alpha float64) {
		prev := false
		var lx, ly float32
		for i := 0; i <= 48; i++ {
			th := 2 * math.Pi * float64(i) / 48
			x := shipX + math.Cos(th)*r*0.42
			y := float64(shipDrawY) + math.Sin(th)*r
			fx, fy := float32(x), float32(y)
			if prev {
				fastLine(screen, lx, ly, fx, fy, 2,
					color.RGBA{235, 240, 248, 255}, alpha)
			}
			lx, ly, prev = fx, fy, true
		}
	}
	fade := 1 - t/4
	ring(60+t*520, 0.7*fade)
	ring(45+t*390, 0.35*fade)
	for i := 0; i < 5; i++ {
		h := hash31(i*13+int(e.stellar), i)
		bx := 60 + float64(h%880)
		by := 290 + float64((h>>10)%150)
		di := float64(i) * 0.35
		al := math.Min(math.Max(t-di, 0), 1) * math.Max(1-(t-di)/2.5, 0)
		if al <= 0 {
			continue
		}
		ui.DrawTextScaled(screen, "BOOOOM", bx, by, 2,
			color.RGBA{96, 158, 214, 255}, float32(al))
	}
}

// drawSky paints the orbital-phase gradient, horizon FORWARD: in space the
// zenith stays transparent black (stars, nebula) with only airglow at the
// limb; as height falls the whole dome fills in toward powder blue — or
// toward night, on the sun's fast clock. The whole sky is rendered flat
// into an offscreen and then banked around the horizon pivot, so the haze
// and the world horizon move as one when steering. Shared by the entry
// chase camera and the takeoff camera.
func (a *App) drawSky(screen *ebiten.Image, hkm, hy, bank, pivotX, sun float64, neb []nebBlob) {
	const skyW, skyH = 1500, 640
	if a.skyImg == nil {
		a.skyImg = ebiten.NewImage(skyW, skyH)
	}
	img := a.skyImg
	img.Clear()

	// The dome stays BLACK through the whole corridor: at interface the
	// horizon is a black line over a blue limb, and only below ~50 km does
	// the air start claiming the sky, filling to powder blue by 15.
	space := math.Min(math.Max((hkm-15)/35, 0), 1)
	lowness := 1 - space
	sunF := 0.30 + 0.70*sun
	hue := color.RGBA{
		uint8((70 + 80*lowness) * sunF),
		uint8((130 + 60*lowness) * sunF),
		uint8((150 + 85*lowness) * math.Min(sunF+0.1, 1)), 255}
	const nbands = 64
	bandH := float64(skyH) / nbands
	for i := 0; i < nbands; i++ {
		f := (float64(i) + 0.5) / nbands // 0 zenith → 1 at the limb ahead
		// in full space the dome itself carries almost nothing — a faint
		// airglow at the limb is all; the air only claims the sky low down
		k := math.Pow(f, 1+2.2*space)
		vector.DrawFilledRect(img, 0, float32(math.Floor(bandH*float64(i))),
			skyW, float32(bandH+1), premul(hue, 0.85*k*(0.18+0.82*lowness)), false)
	}
	// the horizon haze: the palest band, sitting right on the limb so the
	// sky's edge and the world's edge are the same line
	vector.DrawFilledRect(img, 0, skyH-16, skyW, 16,
		premul(color.RGBA{225, 230, 235, 255}, 0.16*lowness*(0.25+0.75*sun)), false)
	// the even nebula, swallowed as the air thickens
	if space > 0.05 {
		for _, nb := range neb {
			ix, iy := float32(nb.x+(skyW-ScreenW)/2), float32(math.Min(nb.y*1.4, skyH-30))
			for k := 3; k >= 1; k-- {
				vector.DrawFilledCircle(img, ix, iy, float32(nb.r*float64(k)/3),
					premul(nb.c, nb.al*space/float64(k)), false)
			}
		}
	}
	// the ionosphere ribbon riding just above the limb — the blackout band
	if hkm > 45 && hkm < 115 {
		ion := math.Min((hkm-45)/20, 1) * math.Min((115-hkm)/25, 1)
		for i, ic := range []color.RGBA{{220, 84, 92, 255}, {140, 92, 220, 255}, {64, 200, 188, 255}} {
			vector.DrawFilledRect(img, 0, float32(skyH-34-9*i), skyW, 7,
				premul(ic, 0.20*ion), false)
		}
	}

	// bank the flat sky around the same pivot the world rotates on
	op := &ebiten.DrawImageOptions{}
	op.Filter = ebiten.FilterLinear
	op.GeoM.Scale(1, hy/skyH)
	op.GeoM.Translate(-skyW/2, -hy)
	op.GeoM.Rotate(bank)
	op.GeoM.Translate(pivotX, hy)
	screen.DrawImage(img, op)
}

// drawFieldLines draws the bar-magnet dipole's nested lobes — r = L·sin²θ
// out of the nose pole, around the flanks, back into the tail: the rails
// the ignited plasma slides on. The steerable cone tilts the whole
// pattern with the roll command, and a coil Boost doubles it into crossed
// quadrupole lobes (the buffer collectors).
func (a *App) drawFieldLines(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	auth := s.PlasmaAuth
	if auth < 0.04 || s.V < 900 {
		return
	}
	cx, cy := shipX-e.bank*30, float64(shipDrawY)
	mAng := e.bank*0.7 - e.roll*0.45
	lobes := func(ang, alpha float64) {
		sinm, cosm := math.Sin(ang), math.Cos(ang)
		for _, L := range [4]float64{58, 82, 112, 146} {
			for _, side := range [2]float64{-1, 1} {
				var lx, ly float32
				prev := false
				for i := 0; i <= 26; i++ {
					th := 0.18 + (math.Pi-0.36)*float64(i)/26
					st := math.Sin(th)
					r := L * st * st
					ax := side * r * st     // across the axis
					ay := -r * math.Cos(th) // along the axis, nose up-screen
					px := cx + ax*cosm - ay*sinm
					py := cy + ax*sinm + ay*cosm
					fx, fy := float32(px), float32(py)
					if prev {
						vector.StrokeLine(screen, lx, ly, fx, fy, 1,
							premul(colEM, alpha), false)
					}
					lx, ly, prev = fx, fy, true
				}
			}
		}
	}
	al := 0.12 + 0.26*auth
	lobes(mAng, al)
	if s.Boosting() {
		lobes(mAng+math.Pi/2, al*0.6)
	}
}

// dial is one cockpit instrument in the reentry-console.html grammar: a
// 270° track from 7:30 round to 4:30, the red zone marked on the rim, the
// value swept inside it in cyan — red when the needle is in the red — and
// a needle tick crossing the rim.
func dial(dst *ebiten.Image, cx, cy, r, v, min, max, red0, red1 float64, label, val string) {
	a0, a1 := math.Pi*0.75, math.Pi*2.25
	ang := func(x float64) float64 {
		f := (x - min) / (max - min)
		return a0 + (a1-a0)*math.Min(math.Max(f, 0), 1)
	}
	arc := func(r, from, to, width float64, c color.RGBA, al float64) {
		if to <= from {
			return
		}
		var p vector.Path
		p.MoveTo(float32(cx+math.Cos(from)*r), float32(cy+math.Sin(from)*r))
		p.Arc(float32(cx), float32(cy), float32(r), float32(from), float32(to), vector.Clockwise)
		op := &vector.DrawPathOptions{}
		op.ColorScale.ScaleWithColor(premul(c, al))
		vector.StrokePath(dst, &p, &vector.StrokeOptions{Width: float32(width)}, op)
	}
	arc(r, a0, a1, 4, color.RGBA{28, 44, 62, 255}, 1) // the track
	arc(r, ang(red0), ang(red1), 4, colBad, 0.5)      // the red zone
	inRed := v >= red0 && v <= red1
	vc := colEM
	if inRed {
		vc = colBad
	}
	arc(r-5, a0, ang(v), 2.6, vc, 0.95) // the value sweep
	an := ang(v)                        // the needle tick
	vector.StrokeLine(dst,
		float32(cx+math.Cos(an)*(r-11)), float32(cy+math.Sin(an)*(r-11)),
		float32(cx+math.Cos(an)*(r+3)), float32(cy+math.Sin(an)*(r+3)),
		1.5, colChrome, false)
	ui.DrawText(dst, val, cx-float64(len(val))*3.5, cy-4, 0.95)
	ui.DrawText(dst, label, cx-float64(len(label))*3.5, cy+r-2, 0.6)
}

// drawEntryStrip is the console's top telemetry row: the scrubbed-instant
// numbers, label over value, straight out of reentry-console.html.
func (a *App) drawEntryStrip(screen *ebiten.Image) {
	s := a.entry.sim
	cells := []struct{ k, v string }{
		{"T", fmt.Sprintf("%.0f s", s.T)},
		{"ALTITUDE", fmt.Sprintf("%.1f km", s.H/1000)},
		{"VELOCITY", fmt.Sprintf("%.2f km/s", s.V/1000)},
		{"MACH", fmt.Sprintf("%.1f", s.Pt.Mach)},
		{"Q STAG", fmt.Sprintf("%.1f W/cm2", s.Pt.QShielded/1e4)},
		{"WALL TEMP", fmt.Sprintf("%.0f K", s.Pt.WallTemp)},
		{"DECEL", fmt.Sprintf("%.2f g", s.Pt.GLoad)},
		{"INTERACTION Q", fmt.Sprintf("%.2f", s.Pt.InteractionQ)},
		{"Ne", fmt.Sprintf("%.1e", s.Pt.Ne)},
		{"SHIP POWER", fmt.Sprintf("%.1f MW", s.Pt.PowerDraw/1e6)},
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
}

// drawOrbitInset is the "from orbit" card: the planet disc, the parking
// orbit it left, and the entry trajectory spiraling down — altitudes
// exaggerated so the descent is visible at all, like the console's
// ORBIT & APPROACH plot.
func (a *App) drawOrbitInset(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	x0, y0, w, h := 846.0, 76.0, 168.0, 168.0
	vector.DrawFilledRect(screen, float32(x0), float32(y0), float32(w), float32(h),
		premul(color.RGBA{5, 7, 10, 255}, 0.6), false)
	vector.StrokeRect(screen, float32(x0), float32(y0), float32(w), float32(h), 1,
		premul(colRule, 0.9), false)
	ui.DrawText(screen, "ORBIT & APPROACH", x0+8, y0+6, 0.6)

	cx, cy := x0+w/2, y0+h/2+8
	const pr = 30.0          // the planet
	const kx = 34.0 / 122000 // px per metre of altitude, exaggerated
	vector.DrawFilledCircle(screen, float32(cx), float32(cy), pr,
		premul(color.RGBA{30, 66, 48, 255}, 0.9), false)
	vector.StrokeCircle(screen, float32(cx), float32(cy), pr, 1,
		premul(colOI, 0.5), false)
	// the parking orbit we came off
	for i := 0; i < 36; i += 2 {
		a0 := float64(i) / 36 * 2 * math.Pi
		a1 := (float64(i) + 1.2) / 36 * 2 * math.Pi
		r := pr + 122000*kx
		vector.StrokeLine(screen,
			float32(cx+math.Cos(a0)*r), float32(cy+math.Sin(a0)*r),
			float32(cx+math.Cos(a1)*r), float32(cy+math.Sin(a1)*r),
			1, premul(colDim, 0.7), false)
	}
	// the descent, altitude-exaggerated, sweeping around the disc
	pos := func(p trailPt) (float32, float32) {
		ang := -math.Pi/2 + p.dr/1.4e6
		r := pr + math.Max(p.h, 0)*kx
		return float32(cx + math.Cos(ang)*r), float32(cy + math.Sin(ang)*r)
	}
	var lx, ly float32
	for i, p := range e.trail {
		px, py := pos(p)
		if i > 0 {
			f := float64(i) / float64(len(e.trail))
			vector.StrokeLine(screen, lx, ly, px, py, 1.5,
				premul(colHeat, 0.35+0.6*f), false)
		}
		lx, ly = px, py
	}
	nx, ny := pos(trailPt{dr: s.Downrange, h: s.H})
	vector.DrawFilledCircle(screen, nx, ny, 3, colChrome, false)
	ui.DrawText(screen, "alt ×6 exaggerated", x0+8, y0+h-16, 0.5)
}

// drawCorridorInset is the console prototype's "Entry corridor" card made
// live: the classic h–V plane, with the EXPECTED profile (this seed's
// autoland, flown headless at interface) as the reference line, the flown
// trace burning over it, and the Kármán line marked where the world wakes
// up. One glance answers "am I on the profile" in the plot's own terms
// instead of the needle's.
func (a *App) drawCorridorInset(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	x0, y0, w, h := 20.0, 76.0, 196.0, 150.0
	vector.DrawFilledRect(screen, float32(x0), float32(y0), float32(w), float32(h),
		premul(color.RGBA{5, 7, 10, 255}, 0.6), false)
	vector.StrokeRect(screen, float32(x0), float32(y0), float32(w), float32(h), 1,
		premul(colRule, 0.9), false)
	ui.DrawText(screen, "EXPECTED PROFILE  h-V", x0+8, y0+6, 0.6)

	vmax := 1000.0
	for _, p := range e.expected {
		if p.v > vmax {
			vmax = p.v
		}
	}
	if s.V > vmax {
		vmax = s.V
	}
	px := func(v float64) float32 { return float32(x0 + 10 + v/vmax*(w-20)) }
	py := func(hm float64) float32 {
		f := math.Min(math.Max(hm/122000, 0), 1)
		return float32(y0 + h - 12 - f*(h-34))
	}
	// the Kármán line: dashes across the plot at 100 km
	ky := py(reentry.KarmanLine)
	for x := x0 + 10; x < x0+w-14; x += 12 {
		vector.StrokeLine(screen, float32(x), ky, float32(x+6), ky, 1,
			premul(colEM, 0.45), false)
	}
	ui.DrawText(screen, "KARMAN 100", x0+w-78, float64(ky)-11, 0.5)
	// the expected profile: the reference line the computer would fly
	var lx, ly float32
	for i, p := range e.expected {
		nx, ny := px(p.v), py(p.h)
		if i > 0 {
			vector.StrokeLine(screen, lx, ly, nx, ny, 1.5,
				premul(colDim, 0.85), false)
		}
		lx, ly = nx, ny
	}
	// the flown trace, shaded like the prototype's q̇-colored trajectory
	for i := 1; i < len(e.trail); i++ {
		f := float64(i) / float64(len(e.trail))
		vector.StrokeLine(screen,
			px(e.trail[i-1].v), py(e.trail[i-1].h),
			px(e.trail[i].v), py(e.trail[i].h), 1.5,
			premul(colHeat, 0.35+0.6*f), false)
	}
	// the ship now: chrome dot, red when the needle is out of the band
	dc := colChrome
	if math.Abs(s.GammaError()) > s.Width {
		dc = colBad
	}
	vector.DrawFilledCircle(screen, px(s.V), py(s.H), 3, dc, false)
	ui.DrawText(screen, "km/s →", x0+w-52, y0+h-11, 0.5)
}

// drawEntryDials is the cockpit cluster riding above the panel: the main
// screen keeps the reentry visualization, the dials keep the numbers.
func (a *App) drawEntryDials(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	top := float64(ScreenH-190) - 84
	vector.DrawFilledRect(screen, 236, float32(top), ScreenW-236, 84,
		premul(color.RGBA{5, 7, 10, 255}, 0.55), false)
	vector.StrokeLine(screen, 236, float32(top), ScreenW, float32(top), 1,
		premul(colRule, 0.8), false)

	battPct, liPct := 100.0, s.Li/s.Veh.LiTank*100
	if gd := a.voy.Grid; gd != nil {
		battPct = gd.BattFrac() * 100
	}
	// Every scale is TUNED TO THIS SHIP: the ranges fall out of the
	// vehicle the yard sold you, not out of the panel's paint.
	//   - the g dial ends at 2.5× this hull's structural limit;
	//   - the wall dial's red line is the radiative-equilibrium wall
	//     temperature at this hull's own TPS flux limit, T = (q/σε)^¼;
	//   - the Mach dial ends just past this world's entry speed;
	//   - the standoff dial's reach scales with the coil actually fitted;
	//   - the LOAD dial reads the flown mass against the design mass —
	//     past 100% is the overstuffed hold, and the stick knows it.
	gMax := s.Veh.GLimit * 2.5
	wallRed := math.Pow(s.Veh.TPSLimit/(5.67e-8*0.85), 0.25)
	machMax := 8350 * math.Sqrt(s.Prof.GravityScale) / 280
	standMax := 1 + 2*s.Veh.CoilField/1.2
	loadPct := s.Veh.WeightFactor() * 100
	type def struct {
		label, val          string
		v, min, max, r0, r1 float64
	}
	defs := []def{
		{"G-LOAD", fmt.Sprintf("%.1f", s.Pt.GLoad), s.Pt.GLoad, 0, gMax, s.Veh.GLimit, gMax},
		{"FLUX/LIM", fmt.Sprintf("%.0f%%", s.Pt.QShielded/s.Veh.TPSLimit*100),
			s.Pt.QShielded / s.Veh.TPSLimit * 100, 0, 160, 100, 160},
		{"WALL K", fmt.Sprintf("%.0f", s.Pt.WallTemp), s.Pt.WallTemp, 0, wallRed * 1.35, wallRed, wallRed * 1.35},
		{"MACH", fmt.Sprintf("%.1f", s.Pt.Mach), s.Pt.Mach, 0, machMax, machMax * 0.9, machMax},
		{"STANDOFF", fmt.Sprintf("%.2f", s.Pt.Standoff), s.Pt.Standoff, 1, standMax, 1, 1.12},
		{"LOAD", fmt.Sprintf("%.0f%%", loadPct), loadPct, 0, 150, 100, 150},
		{"RCS", fmt.Sprintf("%.0f%%", s.RCS/s.Veh.RCSTank*100),
			s.RCS / math.Max(s.Veh.RCSTank, 1) * 100, 0, 100, 0, 12},
		{"HULL SOAK", fmt.Sprintf("%.0f%%", s.Dmg.Hull), s.Dmg.Hull, 0, 100, 85, 100},
		{"BATTERY", fmt.Sprintf("%.0f%%", battPct), battPct, 0, 100, 0, 15},
		{"LI TANK", fmt.Sprintf("%.0f%%", liPct), liPct, 0, 100, 0, 10},
	}
	cw := (float64(ScreenW) - 252) / float64(len(defs))
	for i, d := range defs {
		dial(screen, 244+cw*float64(i)+cw/2, top+38, math.Min(cw*0.36, 28),
			d.v, d.min, d.max, d.r0, d.r1, d.label, d.val)
	}
	_ = e
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
	steer := "PLASMA"
	if s.AeroAuth > s.PlasmaAuth {
		steer = "AERO"
	}
	ui.DrawText(screen, fmt.Sprintf("STEER %-6s plasma %3.0f%% · aero %3.0f%%",
		steer, s.PlasmaAuth*100, s.AeroAuth*100), x, py+114, 1)

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

	// right: budgets — the grid's ledger rides next to the lithium's
	bx := 640.0
	feedShow := e.feed
	if e.auto {
		feedShow = s.FeedUsed / 0.2
	}
	hbar(screen, bx, py+30, 180, feedShow, colLi, fmt.Sprintf("LI FEED %3.0f g/s  [ ]", s.FeedUsed*1000))
	hbar(screen, bx, py+60, 180, s.Li/s.Veh.LiTank, colLi, fmt.Sprintf("LI RESERVE %.1f kg", s.Li))
	if gd := a.voy.Grid; gd != nil {
		battCol := colOI
		if gd.BattFrac() < 0.15 {
			battCol = colBad
		}
		hbar(screen, bx, py+90, 180, gd.BattFrac(), battCol,
			fmt.Sprintf("BATT %3.0f%% · draw %.1f MW", gd.BattFrac()*100, s.Pt.PowerDraw/1e6))
		heatCol := colHeat
		if gd.HeatFrac() > 1 {
			heatCol = colBad
		}
		hbar(screen, bx, py+120, 180, gd.HeatFrac(), heatCol,
			fmt.Sprintf("HEAT %3.0f%% (radiators blind)", gd.HeatFrac()*100))
	}
	hbar(screen, bx, py+150, 180, 1-s.Dmg.Hull/100, colPhos, fmt.Sprintf("HULL %3.0f%%", 100-s.Dmg.Hull))

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
	if s.OffCorridor > 0.02 {
		blink := float32(0.5 + 0.5*math.Abs(math.Sin(s.T*8)))
		ui.DrawText(screen, "OUT OF THE PIPE — HULL SCRUBBING", ax, py+96, blink)
	} else if s.GuardianOn {
		ui.DrawText(screen, "GUARDIAN — dumping seed reserves", ax, py+96, 0.9)
	} else if s.AeroAuth > s.PlasmaAuth {
		ui.DrawText(screen, "IN THE PIPE — FIVE BY FIVE", ax, py+96, 0.8)
	}
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
