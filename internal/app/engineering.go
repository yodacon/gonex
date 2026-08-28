package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/power"
	"yodacon.org/gonex/internal/ui"
)

// The engineering console, solo edition. A full bridge crew would seat a
// human at this panel; single-handed, the pilot flips between allocation
// presets (E) and the grid does the arithmetic. One grid runs every mode —
// combat spends the capacitors, cruise refills them, entry drains the deep
// store with the radiators blind, and the outfitter sells the upgrades —
// which is what makes the modes one game instead of five.

type engPreset int

const (
	presetBalanced engPreset = iota
	presetFlank
	presetScreens
	presetCharge
	presetCount
)

func (p engPreset) String() string {
	switch p {
	case presetFlank:
		return "FLANK SPEED"
	case presetScreens:
		return "SCREENS"
	case presetCharge:
		return "CHARGE STORES"
	}
	return "BALANCED"
}

// shares is what the preset gives each client of the bus: engine thrust
// scale, capacitor recharge draw (MW), and the always-on hotel load (MW).
func (p engPreset) shares() (thrust, screens, hotel float64) {
	switch p {
	case presetFlank:
		return 1.3, 0, 0.3 // run: everything to the drive, caps stop filling
	case presetScreens:
		return 0.7, 2.4, 0.3 // tank: caps refill fast, the drive starves
	case presetCharge:
		return 0.7, 0.8, 0.3 // pre-entry: bank the surplus in the deep store
	}
	return 1.0, 0.8, 0.3
}

const (
	shotCapMJ      = 6.0 // one gun shot off the capacitors
	shieldMJPerDmg = 2.0 // capacitor MJ to eat one point of missile damage
	boostCapMJ     = 30  // the entry coil overdrive, paid up front
)

// wireGrid connects the world's combat hooks to the voyage's grid: shots
// and shields are capacitor transactions before they are anything else.
func (a *App) wireGrid() {
	w, v := a.World, a.voy
	if w == nil || v == nil || v.Grid == nil {
		return
	}
	w.FireGate = func() bool {
		if v.Grid.SpendCap(shotCapMJ) < 1 {
			a.engNotify("Capacitors dry — guns cold. (E: SCREENS refills them.)")
			return false
		}
		return true
	}
	w.ShieldFilter = func(dmg int) int {
		covered := v.Grid.SpendCap(float64(dmg) * shieldMJPerDmg)
		if covered > 0 {
			a.engNotify(fmt.Sprintf("Screens ate %d%% of the hit.", int(covered*100)))
		}
		return int(math.Round(float64(dmg) * (1 - covered)))
	}
}

// engNotify is a throttled console line — grid chatter arrives in bursts.
func (a *App) engNotify(msg string) {
	if a.engNoteCD > 0 {
		return
	}
	a.engNoteCD = 1.6
	a.Console.Notifyf("%s", msg)
}

// updateFlightGrid steps the grid for the vacuum modes (flight and warp):
// radiators work, the reactor's surplus banks, and combat is paid for.
func (a *App) updateFlightGrid() {
	v := a.voy
	if v == nil || v.Grid == nil {
		return
	}
	a.engNoteCD = math.Max(a.engNoteCD-dt, 0)
	thrust, screens, hotel := a.engPreset.shares()
	engines := 0.8 * thrust
	if p := a.World.MainPlayer; p != nil {
		p.ThrustScale = thrust
	}
	f := v.Grid.Step(dt, power.Load{
		Engines: engines, Screens: screens, Hotel: hotel, Vacuum: true,
	})
	if f.Served < 1 {
		a.engNotify("BROWNOUT — the bus is over the plant. Shed load or buy a generator.")
	}
	if f.Overheat > 0 {
		v.Dmg.Computer = math.Min(v.Dmg.Computer+2*f.Overheat*dt, 100)
		a.engNotify("HEAT CEILING — electronics cooking. Ease the bus.")
	}
}

// drawEngPanel is the always-on grid strip in the flight view.
func (a *App) drawEngPanel(screen *ebiten.Image) {
	v := a.voy
	if v == nil || v.Grid == nil {
		return
	}
	g := v.Grid
	x, y := 8.0, float64(ScreenH-92)
	w := 176.0
	vector.DrawFilledRect(screen, float32(x-4), float32(y-18), float32(w+8), 88,
		premul(color.RGBA{5, 7, 10, 255}, 0.72), false)
	ui.DrawText(screen, fmt.Sprintf("ENG · %s (E)", a.engPreset), x, y-14, 0.8)
	bar := func(row int, frac float64, c color.RGBA, label string) {
		by := y + 6 + float64(row)*18
		vector.DrawFilledRect(screen, float32(x), float32(by), float32(w), 7, colPanel, false)
		f := math.Min(math.Max(frac, 0), 1)
		vector.DrawFilledRect(screen, float32(x), float32(by), float32(w*f), 7, c, false)
		ui.DrawText(screen, label, x+w+6, by-4, 0.65)
	}
	bar(0, g.CapFrac(), colEM, fmt.Sprintf("CAP %3.0f%%", g.CapFrac()*100))
	bar(1, g.BattFrac(), colOI, fmt.Sprintf("BATT %3.0f%%", g.BattFrac()*100))
	heatCol := colHeat
	if g.HeatFrac() > 1 {
		heatCol = colBad
	}
	bar(2, g.HeatFrac(), heatCol, fmt.Sprintf("HEAT %3.0f%%", g.HeatFrac()*100))
	ui.DrawText(screen, fmt.Sprintf("reactor %.1f MW · plant +%.0f t", g.ReactorMW, g.OutfitKg/1000),
		x, y+62, 0.65)
}
