// Package power is the ship's energy economy — the one machine every game
// mode is secretly operating. Flight, battle, warp, entry and the landing
// pad differ only in which loads are screaming and which sinks still work;
// the grid itself never changes, which is what stitches the modes into one
// game. The design argument lives in the yodacon repo at
// docs/lab-reports/2026-08-27-bridge-energy-game-design.md.
package power

import (
	"math"

	"yodacon.org/gonex/internal/reentry"
)

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

	// Reactor is which design is bolted in; see reactor.go. It decides how
	// much of a ton of heavy lithium the ship can reach, and WHICH OF THE
	// TWO FUEL FORMS it will accept — so it is a loadout choice that
	// changes which half of the galaxy's refineries you can buy from.
	Reactor Class

	// Shield is the plasma-confinement hardware fitted beyond the stock
	// coil. It lives on the grid rather than on the vehicle because that is
	// where it is BOUGHT and because every rung of it is a load: a phased
	// array drives current against a resistive plasma, an injection ring
	// accelerates mass, and a colder coil is a bigger refrigerator. See
	// internal/reentry/confinement.go for what each one does to the pillow.
	Shield reentry.Confinement

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

// For scales the stock plant to a hull. A yard fits a plant proportional to
// the tonnage it has to move, so a heavy carries more reactor and more
// battery than an interceptor — and spends it faster. The Yodacon (5000 kg)
// is the reference hull, which is what makes Stock and For agree.
func For(massKg float64) *Grid {
	k := massKg / 5000
	if k < 0.4 {
		k = 0.4
	}
	if k > 3 {
		k = 3
	}
	g := Stock()
	g.ReactorMW *= k
	g.BattCapMJ *= k
	g.CapCapMJ *= k
	g.RadiatorMW *= k
	g.HeatCapMJ *= k
	g.BattMJ, g.CapMJ = g.BattCapMJ, g.CapCapMJ
	return g
}

// What combat costs the capacitors. These live here, not in the app, because
// an NPC pulling the trigger and the player pulling the trigger must spend
// the same energy — the moment those two numbers diverge the game stops
// being one economy and becomes two arguing ones.
const (
	ShotMJ         = 6.0 // one gun shot off the capacitors
	ShieldMJPerDmg = 2.0 // capacitor MJ to eat one point of missile damage
)

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

// TrySpendCap draws mj only if the bank can cover it in full, reporting
// whether it did. A gun is all-or-nothing: SpendCap's partial draw is right
// for a shield eating what it can of a hit, but a trigger held down at frame
// rate would otherwise swallow every joule that trickles in and never reach
// the price of a single shot. That livelock leaves a ship permanently cold
// with a charging bank, which is exactly as broken as it sounds.
func (g *Grid) TrySpendCap(mj float64) bool {
	if mj <= 0 {
		return true
	}
	if g.CapMJ < mj {
		return false
	}
	g.CapMJ -= mj
	return true
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

		// The reactor ladder. These do not stack: each Fit replaces what is
		// installed, and the mass and generation delta is whatever the swap
		// actually is, so buying down the ladder refunds the mass. The
		// price is the fuel-economy step, not the megawatts — x3.5 range
		// off the same ton of fuel is worth more than 1.6 MW ever was.
		{"Sodium fast loop", "burns MELT — x3.5 range per ton of fuel, +1.6 MW", 68000, 7000,
			func(g *Grid) { g.Fit(Fast) }},
		{"Shipboard breeder", "burns MELT — x4.5 range, breeds heavylith from a lithium blanket", 145000, 16000,
			func(g *Grid) { g.Fit(Breeder) }},

		// The confinement ladder. Four outfits, four different ways the
		// magnetopause goes wrong, and not one of them is a percentage on
		// the same number — see internal/reentry/confinement.go.
		{"HTS coil rewind", "+0.45 T at the nose — stand-off goes as the cube root of field", 41000, 11000,
			func(g *Grid) { g.Shield.CoilBoost += 0.45 }},
		{"Multipole cusp ring", "plugs the polar cusps: more usable pressure, two thirds less wake drag", 36000, 6500,
			func(g *Grid) { g.Shield.CuspSeal = clamp01(g.Shield.CuspSeal + 0.5) }},
		{"Phased steering array", "leans the envelope — +45% commandable L/D and MHD grip higher up", 52000, 5200,
			func(g *Grid) { g.Shield.ArrayGain = clamp01(g.Shield.ArrayGain + 0.5) }},
		{"Seed injection ring", "puts continuum in front of the nose where the air is too thin to grip", 47000, 8800,
			func(g *Grid) { g.Shield.RingFeed += 0.011 }},
		{"Cryoplant uprate", "-35% refrigeration load: hold the field for the whole corridor", 19000, 3000,
			func(g *Grid) { g.Shield.CryoMargin = clamp01(g.Shield.CryoMargin + 0.5) }},
	}
}

// Buy applies an outfit and books its mass.
//
// A reactor swap books its own mass inside Fit — the delta against whatever
// was installed, which can be negative — so the catalogue's flat Kg must not
// be added on top of it.
func (g *Grid) Buy(o Outfit) {
	before := g.Reactor
	o.Apply(g)
	if g.Reactor == before {
		g.OutfitKg += o.Kg
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
