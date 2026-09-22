package power

import "yodacon.org/gonex/internal/econ"

// The ship reactor: how much of a ton of fuel a hull can actually reach.
//
// Heavy lithium is not a tank of petrol. A ton of it carries the same energy
// wherever it is loaded, and what decides how much of that energy a ship can
// USE is the machine it is loaded into. A thermal pile moderated with water
// burns a few per cent of its charge and dumps the rest as spent fuel; a
// sodium-cooled fast loop runs a much harder spectrum and burns a fifth of
// it; a shipboard breeder does something better than burning, and makes more
// fissile material than it consumes out of a fertile blanket it carries.
//
// That is the ladder the outfitter sells, and it is the reason the fuel
// economy has a player in it at all. Without it heavy lithium would be a
// commodity with a price and no decisions attached. With it, every reactor
// on the shelf is an argument about range, mass, and which half of the
// market you are allowed to buy in — because THE TWO FUEL FORMS ARE NOT
// INTERCHANGEABLE. A pile takes clad solids. A fast loop circulates a
// molten salt. Nothing converts one to the other outside a refinery, so the
// reactor you bolt in decides which refineries in the galaxy are your
// suppliers and which are somebody else's.

// Class is a reactor's design.
type Class int

const (
	// Thermal: a light-water pile. Moderated, forgiving, and cheap, and it
	// reaches almost none of the fuel's potential. Every hull leaves the
	// yard with one.
	Thermal Class = iota
	// Fast: sodium-cooled, unmoderated, running a molten salt. Three times
	// the burnup, no moderator mass, and a coolant that catches fire in
	// air — which is why a fast hull is a refit and not a starter.
	Fast
	// Breeder: a fast loop wrapped in a fertile blanket of plain lithium.
	// It burns its charge like a fast loop AND breeds fresh heavylith out
	// of the blanket, so a ship with one aboard makes some of its own fuel
	// on a long leg. Expensive, heavy, and the only machine in the game
	// that sends the flow uphill inside a hull.
	Breeder

	ClassCount
)

var classNames = [ClassCount]string{
	Thermal: "Thermal pile", Fast: "Sodium fast loop", Breeder: "Shipboard breeder",
}

func (c Class) String() string {
	if c < 0 || c >= ClassCount {
		return "?"
	}
	return classNames[c]
}

// Reactor is one design's whole character.
type Reactor struct {
	Class Class
	// Takes is the fuel form this machine will accept. A pile clads solids;
	// a fast loop circulates a salt. There is no third option and no
	// conversion aboard.
	Takes econ.Material
	// Burnup is the fraction of a ton's energy the design can reach before
	// the charge is spent. It is the number the whole ladder is about.
	Burnup float64
	// Breeding is tons of Heavylith bred per ton of Lithium blanket burnt
	// through, per ton of charge consumed. Zero for everything but the
	// breeder; the breeder is the only machine that returns more fissile
	// material than it took.
	Breeding float64
	// MW is sustained generation and Kg is what the machine weighs, which
	// is what the atmosphere charges for on the way back down.
	MW float64
	Kg float64
}

// Reactors is the ladder, in the order the outfitter shows it.
//
// The burnup numbers are the design's whole argument, so they are spread
// far enough apart to be a decision rather than a percentage: a fast loop
// gets three and a half times a pile's range out of the same ton, and a
// breeder gets four and a half plus a blanket that pays part of it back.
func Reactors() [ClassCount]Reactor {
	return [ClassCount]Reactor{
		Thermal: {Class: Thermal, Takes: econ.Pellets, Burnup: 0.055, MW: 3.0, Kg: 0},
		Fast:    {Class: Fast, Takes: econ.Melt, Burnup: 0.195, MW: 4.6, Kg: 7000},
		Breeder: {Class: Breeder, Takes: econ.Melt, Burnup: 0.250, Breeding: 0.35, MW: 5.8, Kg: 16000},
	}
}

// ReactorOf returns a class's design.
func ReactorOf(c Class) Reactor {
	r := Reactors()
	if c < 0 || c >= ClassCount {
		return r[Thermal]
	}
	return r[c]
}

// Range is how far a charge goes, as a multiple of what a thermal pile gets
// out of the same tonnage. It is Burnup normalised on the starter design, so
// the shop can advertise "×3.5 range" without a second table to disagree
// with the first.
func (r Reactor) Range() float64 { return r.Burnup / Reactors()[Thermal].Burnup }

// Bred is the Heavylith a breeder makes from `blanket` tons of fertile
// lithium while burning `charge` tons of fuel. Mass is conserved by the
// caller: these tons come OUT of the blanket pool and into the fuel pool,
// they are not created. A breeder does not mint fissile material, it
// transmutes fertile material it is carrying — which is precisely the
// distinction the books care about.
func (r Reactor) Bred(charge, blanket float64) float64 {
	if r.Breeding <= 0 || charge <= 0 || blanket <= 0 {
		return 0
	}
	want := charge * r.Breeding
	if want > blanket {
		want = blanket
	}
	return want
}

// Fit installs a reactor class on a grid, replacing whatever was there.
func (g *Grid) Fit(c Class) {
	old := ReactorOf(g.Reactor)
	r := ReactorOf(c)
	g.ReactorMW += r.MW - old.MW
	g.OutfitKg += r.Kg - old.Kg
	g.Reactor = c
}
