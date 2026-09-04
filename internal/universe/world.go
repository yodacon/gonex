// Package universe runs the economy: worlds that dig and make, shops that
// price, routes that pay, hulls that fly them, and a war that all of it
// feeds. It is the layer where econ, industry, govt and traffic meet.
//
// It is pure simulation. Nothing here imports Ebitengine, nothing here draws,
// and every number it produces is a function of the universe seed — so a
// battle can be replayed, a market can be regression-tested, and a balance
// change can be measured instead of argued about.
//
// The invariant that governs the whole package: MASS IS CONSERVED. Every
// tick moves tons between pools and never invents any. `Audit` proves it
// after every step in the tests, over hundreds of simulated days.
package universe

import (
	"fmt"
	"math"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/industry"
)

// World is one port: what is under it, what is stacked on it, what it can
// make of that, and who holds it.
type World struct {
	Stellar int
	Name    string
	Govt    govt.Color
	System  int
	Pop     int

	// The three pools that hold this world's mass. Nothing else on a world
	// stores material, so these three plus every hold plus the sink are the
	// complete set the auditor is given.
	Reserve   econ.Stock // still in the crust
	Warehouse econ.Stock // dug, refined or landed, and for sale

	// Plant is the industry standing here: one composed supermodule per
	// chain the world found worth running. Each was assembled from the
	// primitives in internal/industry and can be inspected, scaled or
	// re-plugged without this package knowing what a smelter is.
	Plant []*industry.Module

	// Civic is the return path: a composter and a breaker's yard, on every
	// inhabited world, sized to what it can expect to recycle. They run in
	// produce like any plant but are invisible to Makes and Wants, because
	// they are not what the world is FOR — nobody is a composting world.
	Civic []*industry.Module

	// Shop is what the port will pay per ton, by material. It is recomputed
	// each day from scarcity, so a world that has run out of something is
	// visibly worth selling to.
	Shop [econ.Count]int

	// Credits is the treasury. It is a purse on the ledger: it pays couriers
	// for imports, it is paid by the counter and the pad, and when it runs
	// dry the world stops buying — a real event with a real cause.
	Credits int

	// Built is the level of each building standing here; see buildings.go.
	// Endowed is the part of it that was there at genesis — the minimal
	// infrastructure every inhabited world starts with — and does not count
	// against the cost ladder: nobody bought it.
	Built   [BuildingCount]int
	Endowed [BuildingCount]int

	// Tariff is the rate this port takes on sales by couriers it does not
	// consider allied. Neutrals open at 6% into their own treasury, the
	// colours at 12% into their exchequer; allies pay nothing.
	Tariff float64

	// Seat is who governs: the colour's AI, or the player who bought the
	// first building here. The first building is the charter.
	Seat Seat

	// Orders are this world's standing orders: N hulls or N tons, A → B,
	// every day until cancelled — or until the world changes hands, when
	// they die with the government that gave them.
	Orders []StandingOrder

	// shortfall counts consecutive days the world could not feed itself,
	// and fed is yesterday's ration: what the population ate against what
	// it wanted. Growth is made of the second; see grow().
	shortfall int
	fed       float64
}

// Seat is who makes this world's decisions.
type Seat int

const (
	SeatAI     Seat = iota // the colour's government
	SeatPlayer             // the player holds the charter
)

func (s Seat) String() string {
	if s == SeatPlayer {
		return "yours"
	}
	return "the government"
}

// Seed builds a world's genesis state from the universe seed. Everything —
// what is in the ground, what is in the warehouse, which industries stood
// up — follows from (seed, stellar) and the government that holds it.
func Seed(seed int64, stellar int, name string, system int, pop int, c govt.Color) *World {
	w := &World{
		Stellar: stellar, Name: name, System: system, Pop: pop, Govt: c,
		Credits: pop / 4,
		Tariff:  neutralTariff,
		fed:     1,
	}
	if c != govt.None {
		w.Tariff = colourTariff
	}
	e := econ.Endow(seed, stellar, pop, govt.MineRate(c))
	w.Reserve, w.Warehouse = e.Reserve, e.Warehouse
	// The magazine the world built before the game started: a populated
	// world is never quite defenceless on day one. It is on the books like
	// the warehouse — Genesis() counts it — and once it is spent, it is
	// spent; an arsenal or a courier has to replace it.
	w.Warehouse.Add(econ.Rounds, math.Round(float64(pop)/1e6*genesisRounds))
	// Minimal infrastructure: every inhabited world is a port with a pad.
	// Capitals get more in New, once it is known which worlds they are.
	if pop > 0 {
		w.endow(Spaceport)
	}
	w.standUpIndustry()
	w.Reprice()
	return w
}

// endow stands a building up at genesis: built, but not bought.
func (w *World) endow(b Building) {
	w.Built[b]++
	w.Endowed[b]++
}

const (
	neutralTariff = 0.06
	colourTariff  = 0.12
	genesisRounds = 60.0 // tons of Rounds per million citizens at genesis
)

// Genesis is every ton this world was created holding, for opening the books.
func (w *World) Genesis() econ.Stock { return w.Reserve.Plus(w.Warehouse) }

// standUpIndustry decides what this world makes.
//
// Nobody is assigned a speciality. The world looks at what it actually has in
// the ground, ranks the chains it could run by the tonnage backing them, and
// builds the best two. Because the endowment is heavy-tailed and pocked with
// holes, that produces genuinely different ports out of one rule: a world
// sitting on silicate and copper becomes an electronics world, its neighbour
// with nothing but biomass becomes a farm, and neither was written down.
//
// A Works bought here adds one more slot, so the third-ranked chain stands
// up — which is the only way a world gets an industry its rocks did not
// already make the obvious choice.
func (w *World) standUpIndustry() {
	w.Plant = nil
	ranked := industry.Rank(w.Reserve)
	slots := maxChains + w.Built[Works]
	if len(ranked) > slots {
		ranked = ranked[:slots]
	}
	// Throughput scales with population: the city is the workforce, exactly
	// as the war economy already assumes for industrial points.
	rate := math.Max(float64(w.Pop), 1) / 1e6 * chainRate
	for _, ch := range ranked {
		w.Plant = append(w.Plant, ch.Assemble(rate, w.Govt))
	}
	// The return path, sized to the world: a composter that can keep up
	// with what its people eat, and a breaker that can work a wreck a week.
	popM := math.Max(float64(w.Pop), 1) / 1e6
	garden := 0.0
	if w.Reserve[econ.Biomass] > 0 || w.Warehouse[econ.Biomass] > 0 {
		// Enough biomass through a thresher and a cannery to cover the
		// subsistence share of the ration: appetite / (0.75 · 0.90 · yield).
		garden = w.appetite(econ.Rations) * gardenShare / (0.75 * 0.90)
	}
	w.Civic = industry.Civic(garden, w.organicAppetite()*1.1, popM*breakerRate, w.Govt)
}

// organicAppetite is the tonnage of compost a day's eating leaves.
func (w *World) organicAppetite() float64 {
	var t float64
	for m := econ.Material(0); m < econ.Count; m++ {
		if m.Organic() {
			t += w.appetite(m)
		}
	}
	return t
}

// Housing is the population the world can hold. It is not a constant: a
// Habitat raises it, and a world that keeps growing is a world somebody
// kept building. Food decides whether it grows; housing decides how far.
func (w *World) Housing() float64 {
	return popCeiling * (1 + housingPerHabitat*float64(w.Built[Habitat]))
}

// Makes reports whether this world produces a material at all.
func (w *World) Makes(m econ.Material) bool {
	for _, p := range w.Plant {
		if p.Supply()[m] > 0 {
			return true
		}
	}
	return false
}

// Wants reports the daily tonnage of a material this world's industry needs
// from outside — the input its own chains cannot supply. This is the demand
// side of every trade route in the game, and it is derived, never authored.
//
// The civic modules count only for what they dig: the gardens' biomass is a
// real want the mine must serve, but a composter's want for compost and a
// breaker's for scrap are not import demand and must not price as such.
func (w *World) Wants(m econ.Material) float64 {
	var t float64
	for _, p := range w.Plant {
		t += p.Demand()[m]
	}
	if m.Crust() {
		for _, p := range w.Civic {
			t += p.Demand()[m]
		}
	}
	return t
}

// Speciality names what this world is for, in one phrase.
func (w *World) Speciality() string {
	if len(w.Plant) == 0 {
		return "no industry"
	}
	return w.Plant[0].Name
}

const (
	maxChains          = 2
	chainRate          = 55.0 // tons/day of throughput per million citizens
	breakerRate        = 6.0  // tons/day of scrap a port can break per million citizens
	gardenShare        = 0.65 // the share of its own ration a world with soil grows itself
	popCeiling         = 3.2e7
	housingPerHabitat  = 0.5
	luxuryExponent     = 1.2 // rich worlds want more per head; the outer-world gradient
	garrisonRoundsBurn = 0.3 // tons of Rounds a million citizens' militia fires in drills per day
)

// --- The shop ------------------------------------------------------------

// Reprice recomputes what this port pays per ton.
//
// Price is scarcity, and scarcity here is a real quantity rather than a hash:
// a world with a full warehouse of chips pays little for chips, and a world
// whose fabricators are idle for want of copper pays through the nose for
// copper. Because both numbers move as the simulation runs, a trade route
// that was profitable last week can close, which is the thing that makes
// routes worth re-planning instead of memorising.
func (w *World) Reprice() {
	for m := econ.Material(0); m < econ.Count; m++ {
		base := baseValue[m]
		if base <= 0 {
			w.Shop[m] = 0
			continue
		}
		// Cover: how many days of demand the warehouse holds. Demand is
		// industrial appetite plus what the population eats.
		demand := w.Wants(m) + w.appetite(m)
		cover := coverDays
		if demand > 0.01 {
			cover = w.Warehouse[m] / demand
		} else if w.Warehouse[m] > 0 {
			cover = coverDays * 2 // nobody wants it and there is plenty
		}
		// A short world pays up to 3x; a glutted one as little as 0.35x.
		// Food is the exception: a hungry world pays up to 6x for rations,
		// because people will pay anything for the next meal — and that is
		// what turns a famine into the best-paying route on the board, which
		// is how a famine gets relieved without anybody scripting relief.
		f := 1.0
		steep := 2.0
		if m == econ.Rations {
			steep = 7.0
		}
		switch {
		case cover < coverDays:
			f = 1 + steep*(1-cover/coverDays)
		default:
			f = math.Max(0.35, 1-0.45*math.Min(1, (cover-coverDays)/(coverDays*3)))
		}
		// A world that MAKES a thing sells it cheap: that is what a producer
		// is, and it is what gives a route a direction. An Exchange narrows
		// the spread — the producer keeps more of the price.
		if w.Makes(m) {
			f *= math.Min(0.75, producerDiscount+exchangeStep*float64(w.Built[Exchange]))
		}
		w.Shop[m] = int(math.Max(1, math.Round(base*f)))
	}
}

const (
	producerDiscount = 0.62
	exchangeStep     = 0.065
)

// Base is what a ton of a material is worth before scarcity — the number a
// governor reads today's price against.
func Base(m econ.Material) float64 { return baseValue[m] }

// Demand is the tons a day this world wants of a material, industry and
// people together.
func (w *World) Demand(m econ.Material) float64 { return w.Wants(m) + w.appetite(m) }

// Cover is how many days of demand the warehouse holds for a material, or
// -1 when nobody here wants it.
func (w *World) Cover(m econ.Material) float64 {
	demand := w.Demand(m)
	if demand <= 0.01 {
		return -1
	}
	return w.Warehouse[m] / demand
}

// Fed is yesterday's ration: 1.0 means the population ate everything it
// wanted, 0 means nothing. Growth is made of this.
func (w *World) Fed() float64 { return w.fed }

// appetite is what the population itself consumes per day, independent of
// industry: food, medicine, power, and the materials a growing city builds
// itself out of.
//
// EVERY FINISHED GOOD HAS AN APPETITE, and that is not decoration — it is
// what keeps the economy circulating. The first cut gave chips, ore and steel
// no consumer at all, so they piled up in warehouses nobody would ever need
// them from, no destination ever showed demand, and the whole trade network
// went quiet on day 104 with thirty-six hulls idle. A commodity with no sink
// is a commodity that stops being traded the moment the first warehouse
// fills.
//
// The intermediates — copper, silicon, polymer, grain — deliberately have no
// appetite. Nobody eats copper; a fabricator does. They move because industry
// wants them, which is a different and better reason.
//
// The staples are linear in population. The LUXURIES — medicine, chips —
// grow faster than heads, so the biggest worlds become the deepest markets
// and the small specialised outer worlds become the best places to SELL.
// That is the gradient the couriers climb, and it is why a pilot who spends
// the margin at an outer world is behaving rationally and not by script.
// A Habitat raises the luxury appetite a notch: a deeper market is what it
// buys.
func (w *World) appetite(m econ.Material) float64 {
	popM := float64(w.Pop) / 1e6
	lux := math.Pow(popM, luxuryExponent+0.05*float64(w.Built[Habitat]))
	switch m {
	case econ.Rations:
		return popM * 9.0
	case econ.Medicine:
		return lux * 1.4
	case econ.FuelCells:
		return popM * 3.2
	case econ.Lumber:
		return popM * 2.1
	case econ.Ore:
		return popM * 3.5 // construction aggregate
	case econ.Chips:
		return lux * 1.6 // the city's own electronics
	case econ.Steel:
		return popM * 2.4 // structure, and the yards
	case econ.Rounds:
		return popM * garrisonRoundsBurn // the militia's drills
	}
	return 0
}

// baseValue is what a ton is worth before scarcity. The six board
// commodities match internal/market's numbers so the two price systems agree
// on what things are roughly worth; the deeper tiers are priced by how much
// crust and processing went into them.
var baseValue = [econ.Count]float64{
	econ.Lumber: 140, econ.Ore: 220, econ.Rations: 90,
	econ.Medicine: 480, econ.Chips: 640, econ.FuelCells: 300,

	econ.Steel: 210, econ.Copper: 340, econ.Silicon: 380,
	econ.Polymer: 190, econ.Grain: 70,

	// The yard tier is priced by what went into it. Scrap is worth what a
	// breaker can get back out of it; compost is worth nothing to anybody
	// but the soil.
	econ.Hull: 900, econ.Rounds: 700, econ.Missiles: 1400,
	econ.Compost: 0, econ.Scrap: 120,

	econ.Ferrite: 60, econ.Cuprite: 95, econ.Silicate: 80,
	econ.Volatiles: 70, econ.Biomass: 40,

	econ.Slag: 0, // worthless by construction — it is where value goes to die
}

const coverDays = 12.0

// Describe renders a world as a couple of readable lines.
func (w *World) Describe() []string {
	rich, tons := econ.Endowment{Reserve: w.Reserve}.Richest()
	out := []string{fmt.Sprintf("%s (%s) pop %.1fM — %s; richest seam %s %.0fkt",
		w.Name, w.Govt, float64(w.Pop)/1e6, w.Speciality(), rich, tons/1000)}
	for _, p := range w.Plant {
		line := "  " + p.Describe()
		if mat, r := p.Bottleneck(); r < 0.999 {
			line += fmt.Sprintf("  [bottleneck: %s at %.0f%%]", mat, r*100)
		}
		out = append(out, line)
	}
	return out
}
