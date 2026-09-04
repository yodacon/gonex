package econ

import "math"

// Genesis is where every ton in the game comes from.
//
// A world's endowment is not rolled at random and it is not authored in a
// table. It is DERIVED from a seed and the world's own identity, by a mixer
// good enough that two worlds one ID apart look nothing alike, and stable
// enough that the same universe seed always produces the same universe. That
// matters more here than anywhere else in the game: an economy you cannot
// replay is an economy you cannot balance, and a bug in a market that only
// happens on Tuesday is a bug nobody ever fixes.
//
// The shape of an endowment is deliberately UNEVEN. Reserves are drawn from
// a heavy-tailed distribution, so most worlds have a little of everything and
// a few have an enormous amount of one thing. Those few are what trade routes
// are for, what fleets are sent to take, and what makes the map worth reading.

// Endowment is what a world was created holding.
type Endowment struct {
	// Reserve is what is still in the crust: finite, and the only source of
	// new material anywhere in the universe.
	Reserve Stock
	// Warehouse is the accumulated stock on the surface at genesis — the
	// centuries of digging that happened before the game started.
	Warehouse Stock
}

// Total is every ton the world was created with, in the ground and on it.
func (e Endowment) Total() Stock { return e.Reserve.Plus(e.Warehouse) }

// mix is splitmix64. It is here rather than in math/rand because the property
// that matters is not statistical quality across a long stream, it is that
// ADJACENT SEEDS PRODUCE UNRELATED OUTPUT — worlds 133 and 134 must not be
// neighbours in wealth just because they are neighbours in the gazetteer.
func mix(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// unit returns a deterministic value in [0,1) for a (seed, world, material,
// salt) tuple. The salt lets one world draw several independent numbers for
// the same material without them correlating.
func unit(seed int64, world int, m Material, salt uint64) float64 {
	h := mix(uint64(seed)*0x2545F4914F6CDD1D + uint64(world)*0x9E3779B97F4A7C15)
	h = mix(h ^ (uint64(m+1) * 0xC2B2AE3D27D4EB4F))
	h = mix(h ^ (salt * 0x165667B19E3779F9))
	return float64(h>>11) / float64(1<<53)
}

// Tuning for the endowment draw. These are the only free parameters in the
// whole seeding step, and they are here together so a balance pass is one
// diff rather than a hunt.
const (
	// baseReserve is the median tonnage of a crust material on a world of
	// one million people. Reserves scale with population because the city
	// that grew there grew there for a reason.
	baseReserve = 90000.0

	// tailPower drives how heavy the tail is. Higher means a starker map:
	// more empty rocks, and the rich worlds richer. At 3.5 the top percentile
	// holds roughly forty times the median.
	tailPower = 3.5

	// barrenChance is how often a world simply has none of a given material.
	// Holes in the map are what force trade: a world that has everything
	// never sends a ship anywhere.
	barrenChance = 0.34

	// warehouseDays is how much accumulated surface stock a world starts
	// with, expressed as days of its own extraction. Enough to trade on from
	// turn one, not enough to live on.
	warehouseDays = 26.0
)

// Endow draws a world's genesis holdings.
//
// seed is the universe seed; world is the stellar ID; pop is the population
// the city on it grew to; mineRate is tons per industrial day per million
// citizens for the government that holds it (govt.MineRate), which sets how
// much of the reserve is already above ground.
func Endow(seed int64, world, pop int, mineRate float64) Endowment {
	var e Endowment
	popM := math.Max(float64(pop), 1) / 1e6

	for _, m := range Crusts() {
		if unit(seed, world, m, 1) < barrenChance {
			continue // this world simply has none of it
		}
		// A heavy tail from a uniform: u^-1/k is Pareto, clamped so a single
		// world cannot own a meaningful fraction of the universe.
		u := unit(seed, world, m, 2)
		if u < 1e-6 {
			u = 1e-6
		}
		heavy := math.Pow(u, -1/tailPower)
		if heavy > 40 {
			heavy = 40
		}
		// A second independent draw stops every material on a world being
		// rich or poor together, which is what gives a world a speciality
		// instead of just a size.
		lean := 0.45 + 1.1*unit(seed, world, m, 3)

		tons := baseReserve * math.Sqrt(popM) * heavy * lean
		e.Reserve.Add(m, math.Round(tons))
	}

	// The centuries before the game started. Whatever the world could dig,
	// it has been digging, so surface stock is proportional to what is in
	// the ground beneath it — a rich seam means a full warehouse.
	dug := mineRate * popM * warehouseDays
	total := e.Reserve.Total()
	if total > 0 {
		for _, m := range Crusts() {
			if e.Reserve[m] <= 0 {
				continue
			}
			share := e.Reserve[m] / total
			take := math.Min(math.Round(dug*share), e.Reserve[m])
			// Moved, not minted: the warehouse comes OUT of the reserve, so
			// Total() is the number the auditor was opened with.
			Transfer(&e.Reserve, &e.Warehouse, m, take)
		}
	}
	return e
}

// Richest names the crust material a world has most of, and how many tons.
// It is what gives a port its character in one line: "Kestrel — a ferrite
// world" is the whole briefing.
func (e Endowment) Richest() (Material, float64) {
	best, most := Slag, 0.0
	for _, m := range Crusts() {
		if e.Reserve[m] > most {
			best, most = m, e.Reserve[m]
		}
	}
	return best, most
}
