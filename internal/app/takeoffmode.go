package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/city"
	"yodacon.org/gonex/internal/ui"
)

// Takeoff is the landing run backwards, and quicker: roll down the
// spaceport road like an airport runway, rotate, climb out over the town,
// watch the sky bands run the other way — powder blue, indigo, ionosphere,
// black — and end in orbit, back on the flight map. About twelve seconds
// against the entry's hundred.
//
// The camera here is a true ground camera: a surface point aheadKm away
// sits at y = hy + 430·h/d, which is correct at every altitude — the
// runway converges to the vanishing point during the roll and the whole
// town falls away beneath the climb, with no special cases.

const (
	rollDur    = 3.2  // seconds of runway roll
	takeoffDur = 12.0 // total
)

type takeoffState struct {
	stellar   int
	t         float64
	roll      float64 // km down the spaceport road
	h         float64 // km
	v         float64 // km/s ground speed
	gam       float64 // display climb angle, deg
	dayPhase0 float64 // the sun's clock at throttle-up
	port      *city.Port
	nebula    []nebBlob
	prom      *promLayer // the ascent sheath: the prominence fan off the nose
}

// ascentHeat is the climb's sheath envelope: nothing in the thick air,
// full fire punching through the upper atmosphere, gone by the black sky —
// the entry's plasma phase run backwards in a quarter of the time.
func (ts *takeoffState) ascentHeat() float64 {
	return math.Min(math.Max((ts.h-16)/12, 0), 1) *
		math.Min(math.Max((118-ts.h)/30, 0), 1)
}

func (a *App) startTakeoff(stellarID int) {
	ts := &takeoffState{stellar: stellarID, h: 0.028,
		port:      city.Generate(int64(stellarID) * 7919),
		dayPhase0: math.Mod(float64(a.voy.Day)*0.37+float64(stellarID%7)*0.11+0.05, 1),
		prom:      newPromLayer(a.voy.Rng.Int63(), 36)}
	nebCols := []color.RGBA{{150, 96, 205, 255}, {84, 178, 190, 255},
		{190, 110, 170, 255}, {96, 120, 210, 255}}
	for i := 0; i < 8; i++ {
		ts.nebula = append(ts.nebula, nebBlob{
			x:  60 + a.voy.Rng.Float64()*(float64(ScreenW)-120),
			y:  30 + a.voy.Rng.Float64()*(horizonBase-130),
			r:  50 + a.voy.Rng.Float64()*85,
			c:  nebCols[i%len(nebCols)],
			al: 0.05 + a.voy.Rng.Float64()*0.06,
		})
	}
	a.takeoff = ts
	a.mode = modeTakeoff
	a.miniMapWin.Visible, a.hudWin.Visible, a.targetWin.Visible = false, false, false
	a.Console.Notifyf("Rolling — spaceport road clear for departure.")
}

func (a *App) updateTakeoff() {
	ts := a.takeoff
	ts.t += dt
	switch {
	case ts.t < rollDur:
		// the roll: throttle up, streetlights walking past
		ts.v = 0.11 * ts.t
		ts.roll += ts.v * dt
	default:
		// rotate and climb: altitude runs the entry's decades in reverse
		ct := ts.t - rollDur
		k := math.Log(122/0.028) / (takeoffDur - rollDur - 1.2)
		ts.h = 0.028 * math.Exp(k*ct)
		ts.gam = math.Min(ts.gam+8*dt, 11)
		ts.v = math.Min(0.35+ct*1.1, 8.35)
		ts.roll += ts.v * dt * 0.25 // ground track falls behind
	}
	ts.prom.step(dt, ts.ascentHeat())
	if ts.t >= takeoffDur {
		a.takeoff = nil
		a.mode = modeFlight
		a.launchEscorts()
		a.setGameStatus(true)
		a.Console.Notifyf("Orbit. Spaceflight — the lanes are yours.")
	}
}

func (a *App) drawTakeoff(screen *ebiten.Image) {
	ts := a.takeoff
	hkm := ts.h
	hy := 300 + ts.gam*8 // nose-up: the horizon settles down the screen
	flat := func(x, y float64) (float32, float32) { return float32(x), float32(y) }

	// a takeoff also runs a quarter of the sun's fast clock
	sun := sunAt(ts.dayPhase0 + ts.t*(0.25/takeoffDur))
	a.drawSky(screen, hkm, hy, 0, shipX, sun, ts.nebula)

	// the ground plane: brown earth, its daylight fading with altitude
	dayn := math.Min(math.Max((30-hkm)/30, 0), 1) * (0.3 + 0.7*sun)
	g := color.RGBA{uint8(28 + 40*dayn), uint8(24 + 32*dayn), uint8(18 + 24*dayn), 255}
	vector.DrawFilledRect(screen, 0, float32(hy), ScreenW, float32(float64(ScreenH)-hy), g, false)

	// the spaceport, from a camera riding a little above the ship, so the
	// road never leaves the bottom of the frame
	eyeK := 430 * math.Max(hkm+0.12, 0.05)
	proj := func(lat, ahead float64) (float64, float64, float64, bool) {
		d := ahead - ts.roll
		if d < 0.03 {
			return 0, 0, 0, false
		}
		x := shipX + lat/d*430
		y := hy + eyeK/d
		// bases may sit below the frame while the tower tops are in it —
		// keep them and let the renderer clip
		return x, y, d, y <= float64(ScreenH)+430 && y >= hy-1
	}
	vis := math.Min(math.Max((16-hkm)/14, 0), 1)
	if vis > 0.02 {
		drawPortScene(screen, ts.port, ts.t, vis, sun, proj, flat)
	}

	// the ship climbing out, plume opening up with the throttle
	shipImg := a.Catalog.Get(a.Cfg.PlayerShipID).Sprites[0]
	b := shipImg.Bounds()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(b.Dx())/2, -float64(b.Dy())/2)
	op.GeoM.Scale(1.5, 1.5)
	op.GeoM.Translate(shipX, 540)
	screen.DrawImage(shipImg, op)
	throttle := math.Min(ts.t/1.2, 1)
	for i := 0; i < 3; i++ {
		fl := 0.6 + 0.4*math.Sin(ts.t*31+float64(i)*2)
		vector.DrawFilledCircle(screen, shipX, float32(596+i*14),
			float32((9-float64(i)*2.4)*throttle*fl),
			premul(color.RGBA{255, 170, 70, 255}, 0.55*throttle*fl), false)
	}

	// the ascent sheath: the same prominence fan the entry flies, run
	// backwards — igniting on the nose as the ship punches the upper
	// atmosphere, then dying into the black sky as orbit takes over
	if heat := ts.ascentHeat(); heat > 0.03 {
		nose := 494.0
		standPx := 9 + 13*heat
		glowDot(screen, shipX, float32(nose-standPx*0.5), float32(50+standPx),
			color.RGBA{255, 200, 130, 255}, 0.28*heat)
		ts.prom.draw(screen, bowGeom{
			cx: shipX, nose: nose, standPx: standPx,
			roll: 0, alpha: 1, t: ts.t,
		}, heat)
	}

	// the callouts, in phase order
	line := "SPACEPORT DEPARTURE — THROTTLE UP"
	switch {
	case ts.t >= rollDur && hkm < 30:
		line = "ROTATE — CLIMBING OUT"
	case hkm >= 30 && hkm < 90:
		line = "ASCENT — SKY GOING TO BLACK"
	case hkm >= 90:
		line = "ORBITAL INSERTION"
	}
	ui.DrawText(screen, line, float64(ScreenW)/2-160, 92, 1)
	st := a.gal.Stellars[ts.stellar]
	if st != nil {
		ui.DrawText(screen, "DEPARTING: "+st.Name, float64(ScreenW)/2-160, 112, 0.7)
	}
	ui.DrawText(screen,
		fmt.Sprintf("ALT %6.1f km   VEL %5.2f km/s", hkm, ts.v),
		float64(ScreenW)/2-160, 132, 0.7)
}
