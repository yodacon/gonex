// Package power is the ship's energy economy — the one machine every game
// mode is secretly operating. Flight, battle, warp, entry and the landing
// pad differ only in which loads are screaming and which sinks still work;
// the grid itself never changes, which is what stitches the modes into one
// game. The design argument lives in the yodacon repo at
// docs/lab-reports/2026-08-27-bridge-energy-game-design.md.
package power

import "math"

// Grid is the installed plant plus its live state. Everything is MJ and MW;
// the entry sim's watts divide by 1e6 on the way in.
type Grid struct {
	// installed hardware — what the outfitter sells
	ReactorMW  float64 // sustained generation
	BattCapMJ  float64 // deep store: runs the entry, refills in cruise
	CapCapMJ   float64 // burst store: shield hits and gun shots
	RadiatorMW float64 // heat rejection — panels only work in vacuum
	HeatCapMJ  float64 // structural heat ceiling before things cook
	OutfitKg   float64 // mass bought beyond the stock plant

	// live state
	BattMJ float64
	CapMJ  float64
	HeatMJ float64
}

// Stock is the plant the Yodacon leaves the yard with: enough reactor to
// cruise, a battery that barely covers one entry, and capacitors that
// forgive exactly two mistakes.
func Stock() *Grid {
	g := &Grid{
		ReactorMW: 3.0, BattCapMJ: 2600, CapCapMJ: 60,
		RadiatorMW: 3.0, HeatCapMJ: 900,
	}
	g.BattMJ, g.CapMJ = g.BattCapMJ, g.CapCapMJ
	return g
}

// Load is one frame's demand on the grid, in MW.
type Load struct {
	Engines float64 // drive draw
	Screens float64 // capacitor recharge draw
	Coil    float64 // plasma-shield coil + seed during entry
	Hotel   float64 // avionics and life support, always on
	HeatMW  float64 // heat arriving from outside (reentry flux soak)
	Vacuum  bool    // radiators reject only when this is true
}

// Flow reports how a frame resolved.
type Flow struct {
	Served   float64 // 0..1 — below 1 the ship is in brownout
	DrawMW   float64 // demand actually supplied
	FromBatt float64 // MW of that which came off the battery
	Overheat float64 // fraction past the heat ceiling, 0 when under it
}

const (
	// battDischargeMW caps how hard the deep store can be pulled; past it
	// the grid browns out no matter how much energy is banked.
	battDischargeMW = 10.0
	// wasteFrac of every served megawatt arrives in the heat pool.
	wasteFrac = 0.22
	// atmoReject is the trickle the hull sheds without panels: closed-cycle
	// coolant against a plasma sheath is nearly useless.
	atmoReject = 0.35
)

// Step resolves dt seconds of the grid under a load.
func (g *Grid) Step(dt float64, l Load) Flow {
	demand := l.Engines + l.Screens + l.Coil + l.Hotel

	battRate := math.Min(battDischargeMW, g.BattMJ/math.Max(dt, 1e-9))
	supply := g.ReactorMW + battRate

	f := Flow{Served: 1}
	if demand > supply {
		f.Served = supply / demand
	}
	f.DrawMW = demand * f.Served
	f.FromBatt = math.Max(f.DrawMW-g.ReactorMW, 0)
	g.BattMJ = math.Max(g.BattMJ-f.FromBatt*dt, 0)

	// Reactor surplus refills the caps first (they are what keeps you
	// alive tonight), then the battery (what keeps you alive next week).
	if surplus := g.ReactorMW - f.DrawMW; surplus > 0 {
		toCap := math.Min(surplus*dt, g.CapCapMJ-g.CapMJ)
		g.CapMJ += toCap
		g.BattMJ = math.Min(g.BattMJ+(surplus*dt-toCap), g.BattCapMJ)
	}
	// The screens' own draw is capacitor charge by definition.
	g.CapMJ = math.Min(g.CapMJ+l.Screens*f.Served*dt, g.CapCapMJ)

	// Heat: every served megawatt tithes to the pool, entry flux pays in
	// directly, and only vacuum panels pay much out.
	reject := atmoReject
	if l.Vacuum {
		reject += g.RadiatorMW
	}
	g.HeatMJ = math.Max(g.HeatMJ+(f.DrawMW*wasteFrac+l.HeatMW-reject)*dt, 0)
	f.Overheat = math.Max(g.HeatMJ/g.HeatCapMJ-1, 0)
	return f
}

// SpendCap draws mj from the capacitor bank and reports the fraction it
// could cover. A shield eating a hit or a gun demanding a shot cannot wait
// for the reactor; this is the only path fast enough.
func (g *Grid) SpendCap(mj float64) float64 {
	if mj <= 0 {
		return 1
	}
	got := math.Min(mj, g.CapMJ)
	g.CapMJ -= got
	return got / mj
}

// Gauge fractions for the panels.
func (g *Grid) BattFrac() float64 { return g.BattMJ / math.Max(g.BattCapMJ, 1) }
func (g *Grid) CapFrac() float64  { return g.CapMJ / math.Max(g.CapCapMJ, 1) }
func (g *Grid) HeatFrac() float64 { return g.HeatMJ / math.Max(g.HeatCapMJ, 1) }

// Outfit is one line in the yard's power catalog. Everything adds mass, and
// mass is what the atmosphere charges for on the way back down — the shop
// is a loadout choice, not a power curve.
type Outfit struct {
	Name  string
	Desc  string
	Price int
	Kg    float64
	Apply func(g *Grid)
}

// Catalog is the outfitter's shelf.
func Catalog() []Outfit {
	return []Outfit{
		{"Auxiliary generator", "+1.5 MW sustained", 22000, 9000,
			func(g *Grid) { g.ReactorMW += 1.5 }},
		{"Deep battery bank", "+900 MJ stored", 14000, 5000,
			func(g *Grid) { g.BattCapMJ += 900 }},
		{"Capacitor array", "+40 MJ burst", 11000, 2200,
			func(g *Grid) { g.CapCapMJ += 40 }},
		{"Radiator wing", "+2 MW rejection (vacuum)", 9000, 3500,
			func(g *Grid) { g.RadiatorMW += 2 }},
		{"Thermal mass sink", "+300 MJ heat ceiling", 7000, 4000,
			func(g *Grid) { g.HeatCapMJ += 300 }},
	}
}

// Buy applies an outfit and books its mass.
func (g *Grid) Buy(o Outfit) {
	o.Apply(g)
	g.OutfitKg += o.Kg
}
