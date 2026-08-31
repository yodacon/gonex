package app

// The fx lab: a Go-based development environment for the entry effects.
// GONEX_BOOT="fxlab" boots straight into a black stage with the ship, the
// plasma bow-wave fire, and the Mach condensation cloud — every input the
// real entry feeds them is on a key, so an effect can be iterated in
// seconds without flying a corridor. This is where the fire gets tuned.

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/fx"
	"yodacon.org/gonex/internal/ui"
)

type fxlabState struct {
	bow, cloud *fx.Fire
	prom       *promLayer
	qFrac      float64 // sheath heat fraction — feeds the plasma fire
	mach       float64
	feed       float64 // lithium feed fraction
	standoff   float64 // magnetopause standoff ratio, 1..3
	aero       float64 // aero authority — feeds the cloud
	roll, bank float64
	showBow    bool
	showCloud  bool
	showProm   bool
	t          float64
}

func (a *App) startFxLab() {
	a.fxlab = &fxlabState{
		bow:   fx.NewFire(44, 13, 42),
		cloud: fx.NewFire(36, 9, 43),
		prom:  newPromLayer(44, 46),
		qFrac: 0.6, mach: 12, feed: 0.4, standoff: 1.6, aero: 0.3,
		showBow: true, showCloud: true, showProm: true,
	}
	a.fxlab.cloud.Cooling = 0.988
	a.mode = modeFxLab
	a.hideMenu()
	a.miniMapWin.Visible, a.hudWin.Visible, a.targetWin.Visible = false, false, false
	a.fullMapWin.Visible, a.galaxyWin.Visible = false, false
	a.Console.Notifyf("FX LAB — Q/A heat · W/S mach · E/D feed · R/F standoff · T/G aero · arrows steer · 1/2 toggle layers · SPC burst")
}

// knob nudges a parameter with a key pair, clamped.
func knob(v *float64, up, down ebiten.Key, step, lo, hi float64) {
	if ebiten.IsKeyPressed(up) {
		*v = math.Min(*v+step*dt, hi)
	}
	if ebiten.IsKeyPressed(down) {
		*v = math.Max(*v-step*dt, lo)
	}
}

func (a *App) updateFxLab() {
	l := a.fxlab
	l.t += dt
	knob(&l.qFrac, ebiten.KeyQ, ebiten.KeyA, 0.5, 0, 1.3)
	knob(&l.mach, ebiten.KeyW, ebiten.KeyS, 6, 0, 30)
	knob(&l.feed, ebiten.KeyE, ebiten.KeyD, 0.5, 0, 1)
	knob(&l.standoff, ebiten.KeyR, ebiten.KeyF, 0.8, 1, 3)
	knob(&l.aero, ebiten.KeyT, ebiten.KeyG, 0.5, 0, 1)
	roll := 0.0
	if ebiten.IsKeyPressed(ebiten.KeyArrowLeft) {
		roll = -1
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowRight) {
		roll = 1
	}
	l.roll += (roll - l.roll) * math.Min(6*dt, 1)
	l.bank += (roll*0.48 - l.bank) * math.Min(3.5*dt, 1)
	if inpututil.IsKeyJustPressed(ebiten.KeyDigit1) {
		l.showBow = !l.showBow
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDigit2) {
		l.showCloud = !l.showCloud
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDigit3) {
		l.showProm = !l.showProm
	}
	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		l.bow.Boost(0.5)
		l.prom.Boost(0.5)
	}

	// feed the grids exactly the way updatePlasma does
	lobeV, rollAbs := -l.roll, math.Abs(l.roll)
	l.bow.Fuel = math.Min(l.qFrac*2.0+l.feed*0.45, 1.35)
	l.bow.Sweep = -l.roll * 2.4
	l.bow.FuelProfile = func(u float64) float64 {
		base := 1 - 0.45*u*u
		return base * (1 + 0.9*math.Max(0, lobeV*u)*rollAbs)
	}
	l.bow.Step(dt)
	l.cloud.Fuel = l.aero * math.Min(math.Max((l.mach-0.85)/0.4, 0), 1)
	l.cloud.Sweep = -l.roll * 1.4
	l.cloud.Step(dt)
	l.prom.step(dt, math.Min(l.qFrac*1.5, 1))
}

func (a *App) drawFxLab(screen *ebiten.Image) {
	l := a.fxlab
	nose := shipY - 46.0
	standPx := 16 + 22*(l.standoff-1)
	cx := shipX - l.bank*30

	if l.showBow {
		drawBowFire(screen, l.bow, bowGeom{
			cx: cx, nose: nose, standPx: standPx,
			roll: l.roll, alpha: 0.5 + 0.5*math.Min(l.qFrac*2, 1), t: l.t,
		})
	}
	if l.showProm {
		l.prom.draw(screen, bowGeom{
			cx: cx, nose: nose, standPx: standPx,
			roll: l.roll, alpha: 1, t: l.t,
		}, math.Min(l.qFrac*1.5, 1))
	}
	shipImg := a.Catalog.Get(a.Cfg.PlayerShipID).Sprites[0]
	bb := shipImg.Bounds()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(bb.Dx())/2, -float64(bb.Dy())/2)
	op.GeoM.Rotate(l.bank * 0.7)
	op.GeoM.Scale(shipScale, shipScale)
	op.GeoM.Translate(shipX, shipDrawY)
	screen.DrawImage(shipImg, op)
	if l.showCloud {
		drawMachCloud(screen, l.cloud, cx, float64(shipDrawY)-14, l.mach, l.aero)
	}

	// the bench readout
	rows := []string{
		fmt.Sprintf("Q/A  heat      %.2f", l.qFrac),
		fmt.Sprintf("W/S  mach      %.1f", l.mach),
		fmt.Sprintf("E/D  li feed   %.2f", l.feed),
		fmt.Sprintf("R/F  standoff  %.2f", l.standoff),
		fmt.Sprintf("T/G  aero auth %.2f", l.aero),
		fmt.Sprintf("<->  roll      %+.2f", l.roll),
		fmt.Sprintf("1/2/3 bow %v · cloud %v · prom %v", l.showBow, l.showCloud, l.showProm),
		"SPC  pellet burst",
	}
	vector.DrawFilledRect(screen, 16, 60, 250, float32(18*len(rows)+20),
		premul(color.RGBA{5, 7, 10, 255}, 0.7), false)
	ui.DrawText(screen, "FX LAB — bow waves", 24, 66, 0.8)
	for i, r := range rows {
		ui.DrawText(screen, r, 24, 86+float64(i)*18, 0.8)
	}
}
