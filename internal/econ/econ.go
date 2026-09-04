// Package econ is the matter of the universe: what there is, where it is,
// and the promise that none of it is ever created or destroyed by accident.
//
// The economy is ZERO-SUM IN MASS. Every ton of ferrite in the game was put
// in the ground at genesis, and from then on it can only MOVE — out of the
// crust into a warehouse, out of a warehouse into a hold, across a lane,
// into a factory that turns it into something heavier-value and lighter, and
// finally into consumption or into slag. It is never minted. A world that
// grows rich did so by taking mass off somebody, and a world that is mined
// out is mined out forever.
//
// That single invariant is what makes the rest of this testable. Prices,
// routes, industry and war are all opinions; the mass balance is a fact, and
// `Audit` will not let an opinion quietly print money.
package econ

import "fmt"

// Material is one substance. The ordering is load-bearing in one place: the
// first BoardWidth entries are exactly market.Commodities, in order, so a
// planet's 6-wide Stock and a ship's 6-wide Hold are a PREFIX of a full
// material vector and convert by copying rather than by a lookup table.
type Material int

const (
	// The trade board — what has a price posted at a spaceport. These six
	// are the only materials the existing market, holds and warehouses know
	// about, and their order is pinned to market.Commodities.
	Lumber Material = iota
	Ore
	Rations
	Medicine
	Chips
	FuelCells

	// Refined stock. Nobody posts a price on these; they are what a smelter
	// hands to a fabricator, and they exist so that industry is a CHAIN
	// rather than a single magic step from dirt to microchips.
	Steel
	Copper
	Silicon
	Polymer
	Grain

	// The yard tier. What a fleet is made of, and the reason a high-
	// population world with steel and chips on hand is dangerous: a hull's
	// dry tonnage is Hull that left a warehouse, and a magazine is Rounds
	// that a courier carried. Nothing here is abstract — a planet out of
	// Rounds has Konquest's zero kill percentage, and can be taken by
	// whoever arrives.
	Hull
	Rounds
	Missiles

	// The returns. Two sinks that are not graves: organic consumption
	// lands in Compost, which a composter turns back into biomass on the
	// surface, and a dead hull's structure lands in Scrap, which a
	// breaker's yard turns back into steel. These are what make the
	// economy a CYCLE rather than a drain — the only two places the flow
	// goes back uphill.
	Compost
	Scrap

	// The crust. This is the only tier mining can produce, and the only tier
	// that is finite: there is a fixed tonnage of each in each world's rock
	// at genesis and no process anywhere puts any back.
	Ferrite
	Cuprite
	Silicate
	Volatiles
	Biomass

	// The sink. Every ton industry cannot turn into product lands here, and
	// so does everything consumption burns. Slag is not waste in the sense
	// of "gone" — it is waste in the sense of "counted, and worthless",
	// which is precisely what makes the books balance to zero.
	Slag

	// Count is the width of a material vector.
	Count
)

// BoardWidth is how many materials have a price on a spaceport's board. It
// must equal len(market.Commodities) and world.CommodityCount; the app's
// tests assert all three against each other.
const BoardWidth = 6

// The tier boundaries.
const (
	FirstRefined = Steel
	FirstYard    = Hull
	FirstReturn  = Compost
	FirstCrust   = Ferrite
)

var names = [Count]string{
	Lumber: "Lumber", Ore: "Ore", Rations: "Rations",
	Medicine: "Medicine", Chips: "Chips", FuelCells: "Fuel cells",
	Steel: "Steel", Copper: "Copper", Silicon: "Silicon",
	Polymer: "Polymer", Grain: "Grain",
	Hull: "Hull", Rounds: "Rounds", Missiles: "Missiles",
	Compost: "Compost", Scrap: "Scrap",
	Ferrite: "Ferrite", Cuprite: "Cuprite", Silicate: "Silicate",
	Volatiles: "Volatiles", Biomass: "Biomass",
	Slag: "Slag",
}

func (m Material) String() string {
	if m < 0 || m >= Count {
		return fmt.Sprintf("Material(%d)", int(m))
	}
	return names[m]
}

// Tradeable reports whether a spaceport posts a price for this material.
func (m Material) Tradeable() bool { return m >= 0 && m < BoardWidth }

// Crust reports whether this material comes out of the ground. Only these
// are finite; everything else is made from them.
func (m Material) Crust() bool { return m >= FirstCrust && m <= Biomass }

// Refined reports whether this is an intermediate — made from crust, and
// consumed by a further stage rather than sold. Intermediates move by
// in-system shuttle, never by interstellar courier: see universe.FindRoutes.
func (m Material) Refined() bool { return m >= FirstRefined && m < FirstYard }

// Yard reports whether this is fleet material: a hull, or what it shoots.
func (m Material) Yard() bool { return m >= FirstYard && m < FirstReturn }

// Return reports whether this is one of the two recycled sinks.
func (m Material) Return() bool { return m >= FirstReturn && m < FirstCrust }

// Organic reports whether eating this leaves compost rather than slag. It is
// the rule that decides which half of consumption is renewable.
func (m Material) Organic() bool {
	switch m {
	case Rations, Medicine, Lumber, Grain, Biomass:
		return true
	}
	return false
}

// Finished reports whether a spaceport population would buy this for its
// own use: the board goods plus munitions for the garrison. Intermediates
// and returns are not finished; a fabricator wants copper, nobody eats it.
func (m Material) Finished() bool { return m.Tradeable() || m == Rounds || m == Missiles }

// Crusts lists the minable materials, in order. Seeding walks this.
func Crusts() []Material {
	out := make([]Material, 0, 5)
	for m := FirstCrust; m <= Biomass; m++ {
		out = append(out, m)
	}
	return out
}

// --- Stock ---------------------------------------------------------------

// Stock is a quantity of every material, in tons. It is a value type on
// purpose: passing a Stock around copies it, so nothing can accidentally
// hold a reference to somebody else's warehouse and mutate it from a
// distance. Anything that must be mutated is passed as a pointer explicitly.
type Stock [Count]float64

// Add puts tons in. Negative tons are a caller bug and are clamped away
// rather than quietly draining the pool.
func (s *Stock) Add(m Material, tons float64) {
	if tons > 0 {
		s[m] += tons
	}
}

// Take removes up to `tons` and returns how much it actually got. A pool is
// never allowed to go negative, because a negative pool is mass created out
// of nothing and it would balance the books by lying.
func (s *Stock) Take(m Material, tons float64) float64 {
	if tons <= 0 {
		return 0
	}
	got := tons
	if s[m] < got {
		got = s[m]
	}
	s[m] -= got
	if s[m] < 0 {
		s[m] = 0
	}
	return got
}

// Has reports the tonnage on hand.
func (s Stock) Has(m Material) float64 { return s[m] }

// Total is every ton in the pool, of every material.
func (s Stock) Total() float64 {
	var t float64
	for _, v := range s {
		t += v
	}
	return t
}

// Plus returns the sum of two pools. Used by the auditor to roll every pool
// in the universe into one column.
func (s Stock) Plus(o Stock) Stock {
	for i := range s {
		s[i] += o[i]
	}
	return s
}

// Scaled returns the pool multiplied through by f.
func (s Stock) Scaled(f float64) Stock {
	for i := range s {
		s[i] *= f
	}
	return s
}

// Board copies the tradeable prefix into a 6-wide integer slice, which is
// the shape a planet warehouse and a ship hold already speak.
func (s Stock) Board() []int {
	out := make([]int, BoardWidth)
	for i := 0; i < BoardWidth; i++ {
		out[i] = int(s[i])
	}
	return out
}

// FromBoard lifts a 6-wide warehouse or hold into a full material vector.
func FromBoard(board []int) Stock {
	var s Stock
	for i := 0; i < BoardWidth && i < len(board); i++ {
		s[i] = float64(board[i])
	}
	return s
}

func (s Stock) String() string {
	out := ""
	for m := Material(0); m < Count; m++ {
		if s[m] <= 0.01 {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += fmt.Sprintf("%.0ft %s", s[m], m)
	}
	if out == "" {
		return "empty"
	}
	return out
}

// Parse reads a material by name, case-insensitively, for the console and
// the desk. "fuel" and "cells" both find Fuel cells.
func Parse(s string) (Material, bool) {
	fold := func(x string) string {
		b := []byte(x)
		for i, c := range b {
			if c >= 'A' && c <= 'Z' {
				b[i] = c + 'a' - 'A'
			}
		}
		return string(b)
	}
	q := fold(s)
	for m := Material(0); m < Count; m++ {
		if fold(names[m]) == q {
			return m, true
		}
	}
	switch q {
	case "fuel", "cells", "fuelcells":
		return FuelCells, true
	}
	return 0, false
}
