package app

import (
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

	// the white-in: first plasma washes the cut away
	if prog > 0.72 {
		w := ease((prog - 0.72) / 0.28)
		vector.DrawFilledRect(screen, 0, 0, ScreenW, ScreenH,
			premul(color.RGBA{255, 236, 220, 255}, w), false)
	}
}
