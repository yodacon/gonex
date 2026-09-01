package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/ui"
	"yodacon.org/gonex/internal/world"
)

// The deorbit sequence bridges the top-down flight view into the entry
// cockpit: the ship commits a retro-burn, the planet swells toward the
// camera while the checklist walks by, and the first plasma washes the
// screen white — which the cockpit then fades in from. About three
// seconds; the mode swap happens inside the flash so the change of camera
// never shows as a cut.

const deorbitDur = 3.2

type deorbitState struct {
	stellar  int
	t        float64
	sprite   int     // planet texture index
	fromX    float64 // planet's screen position when the burn began
	fromY    float64
	shipHdgn float64 // ship heading at commit, for the retro-flip
}

// startDeorbit begins the sequence for a stellar the player is near.
func (a *App) startDeorbit(stellarID int) {
	st := a.gal.Stellars[stellarID]
	if st == nil {
		return
	}
	d := &deorbitState{stellar: stellarID, sprite: 1 + st.Sprite%18}
	// find the planet on screen so the zoom starts from where it truly is
	d.fromX, d.fromY = float64(ScreenW)/2, float64(ScreenH)/2-120
	for _, e := range a.World.Entities {
		if pl, ok := e.(*world.Planet); ok && pl.StellarID == stellarID {
			sp := a.cam.ToScreen(pl.Pos())
			// clamp: even if triggered from far away (console, dev boot),
			// the world starts swelling from inside the frame
			d.fromX = math.Min(math.Max(sp.X, 60), float64(ScreenW)-60)
			d.fromY = math.Min(math.Max(sp.Y, 60), float64(ScreenH)-60)
			d.sprite = pl.SpriteID
		}
	}
	if p := a.World.MainPlayer; p != nil {
		d.shipHdgn = p.Heading
	}
	a.deorbit = d
	a.mode = modeDeorbit
	a.miniMapWin.Visible, a.hudWin.Visible, a.targetWin.Visible = false, false, false
	a.fullMapWin.Visible, a.galaxyWin.Visible = false, false
	a.Console.Notifyf("DEORBIT — %s. Burn committed.", st.Name)
}

func (a *App) updateDeorbit() {
	d := a.deorbit
	d.t += dt
	if d.t >= deorbitDur {
		stellar := d.stellar
		a.deorbit = nil
		a.startEntry(stellar)
		if a.entry != nil {
			a.entry.flash = 1 // the cockpit inherits the plasma white-in
		}
	}
}

func ease(t float64) float64 { return t * t * (3 - 2*t) }

func (a *App) drawDeorbit(screen *ebiten.Image) {
	d := a.deorbit
	prog := math.Min(d.t/deorbitDur, 1)
	cx, cy := float64(ScreenW)/2, float64(ScreenH)/2

	// the planet swells from its true screen position toward filling the view
	g := ease(math.Min(prog/0.85, 1))
	px := d.fromX + (cx-d.fromX)*g
	py := d.fromY + (float64(ScreenH)+180-d.fromY)*g
	planet := a.Renderer.Planet(d.sprite)
	b := planet.Bounds()
	scale := 1 + 11*g*g
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(b.Dx())/2, -float64(b.Dy())/2)
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(px, py)
	screen.DrawImage(planet, op)

	// the ship holds center screen, flipped retrograde, burn plume flaring
	if p := a.World.MainPlayer; p != nil {
		sprite := a.Catalog.Get(p.ShipID).SpriteFor(d.shipHdgn + 180)
		sb := sprite.Bounds()
		sop := &ebiten.DrawImageOptions{}
		sop.GeoM.Translate(cx-float64(sb.Dx())/2, cy-float64(sb.Dy())/2)
		screen.DrawImage(sprite, sop)
		if prog < 0.42 { // the burn itself
			flare := float32(1 - prog/0.42)
			vector.DrawFilledCircle(screen, float32(cx), float32(cy-30),
				8+14*flare, premul(colHeat, float64(flare)*0.7), false)
		}
	}

	// checklist
	line := "DEORBIT BURN — ΔV −212 m/s RETROGRADE"
	if prog > 0.42 {
		line = "COAST TO ENTRY INTERFACE — 122.0 KM"
	}
	if prog > 0.75 {
		line = "PLASMA ONSET — COMMS BLACKOUT"
	}
	ui.DrawText(screen, line, cx-160, 92, 1)
	st := a.gal.Stellars[d.stellar]
	ui.DrawText(screen, "TARGET: "+st.Name, cx-160, 112, 0.7)

	a.drawDeorbitMFD(screen, d, prog)

	// the white-in: first plasma washes the cut away
	if prog > 0.72 {
		w := ease((prog - 0.72) / 0.28)
		vector.DrawFilledRect(screen, 0, 0, ScreenW, ScreenH,
			premul(color.RGBA{255, 236, 220, 255}, w), false)
	}
}

// drawDeorbitMFD is the orbit computer riding the burn: the parking
// circle deforming as the retro engines drag the periapsis down into the
// atmosphere, the osculating elements ticking live beside it, and the
// one number the whole maneuver is about — PeA, driven to −20 km and
// ringed when it gets there. The deorbit stage's own gauge.
func (a *App) drawDeorbitMFD(screen *ebiten.Image, d *deorbitState, prog float64) {
	x0, y0, w, h := 28.0, 168.0, 204.0, 216.0
	vector.DrawFilledRect(screen, float32(x0), float32(y0), float32(w), float32(h),
		premul(color.RGBA{4, 10, 5, 255}, 0.72), false)
	vector.StrokeRect(screen, float32(x0), float32(y0), float32(w), float32(h), 1,
		premul(hudGreen, 0.6), false)
	st := a.gal.Stellars[d.stellar]
	title := "Orbit"
	if st != nil {
		title = "Orbit: " + st.Name
	}
	ui.DrawTextScaled(screen, title, x0+8, y0+4, 1, hudGreen, 0.9)
	ui.DrawTextScaled(screen, "Prj SHP", x0+w-56, y0+4, 1, hudGreen, 0.5)

	// the burn: f runs 0 → 1 over the retro-fire, then holds
	f := ease(math.Min(prog/0.42, 1))
	peA := 362.0 - 382.0*f // thousand metres: +362.0k → −20.0k

	// the orbit plot: the circle pinched toward the planet at periapsis
	ocx, ocy, or0 := x0+w/2+22, y0+112.0, 52.0
	vector.StrokeCircle(screen, float32(ocx), float32(ocy), float32(or0*0.46), 1,
		premul(colDim, 0.9), false)
	var lx, ly float32
	prev := false
	for i := 0; i <= 48; i++ {
		th := 2 * math.Pi * float64(i) / 48
		rr := or0 * (1 - 0.30*f*(1+math.Cos(th-math.Pi/2))/2)
		px := float32(ocx + math.Cos(th)*rr)
		py := float32(ocy + math.Sin(th)*rr)
		if prev {
			fastLine(screen, lx, ly, px, py, 1.2, hudGreen, 0.85)
		}
		lx, ly, prev = px, py, true
	}
	// the ship on the orbit, opposite the periapsis
	vector.DrawFilledCircle(screen, float32(ocx), float32(ocy-or0), 2.5,
		colChrome, false)

	// the osculating elements, live
	rows := []struct{ k, v string }{
		{"SMa", fmt.Sprintf("6.%03dM", 728-int(190*f))},
		{"PeA", fmt.Sprintf("%+6.1fk", peA)},
		{"ApA", " 400.1k"},
		{"Vel", fmt.Sprintf("%6.3fk", 7.694-0.212*f)},
		{"Ecc", fmt.Sprintf("%6.4f", 0.0008+0.0292*f)},
		{"Inc", " 74.52°"},
	}
	for i, row := range rows {
		ry := y0 + 24 + float64(i)*15
		c, al := hudGreen, float32(0.85)
		if row.k == "PeA" {
			al = 1
			if peA <= -19 {
				c = color.RGBA{255, 120, 120, 255}
				// the red ring around the number that says "burn done"
				vector.StrokeRect(screen, float32(x0+6), float32(ry-2), 92, 14, 1,
					premul(c, 0.9), false)
			}
		}
		ui.DrawTextScaled(screen, row.k, x0+8, ry, 1, c, float32(0.55))
		ui.DrawTextScaled(screen, row.v, x0+38, ry, 1, c, al)
	}
	// the PRJ/DST rail, and the target line the maneuver is flown to
	for i, b := range [3]string{"PRJ", "DST", "HUD"} {
		by := y0 + 26 + float64(i)*24
		vector.StrokeRect(screen, float32(x0+w-32), float32(by), 26, 13, 1,
			premul(hudGreen, 0.35), false)
		ui.DrawTextScaled(screen, b, x0+w-29, by+1, 1, hudGreen, 0.45)
	}
	ui.DrawTextScaled(screen, "PeA target -20.0k", x0+8, y0+h-16, 1, hudGreen, 0.6)
}
