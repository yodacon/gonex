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

	// Shop is what the port will pay per ton, by material. It is recomputed
	// each day from scarcity, so a world that has run out of something is
	// visibly worth selling to.
	Shop [econ.Count]int

	Credits int

	// shortfall counts consecutive days the world could not feed itself. A
	// starving world stops growing, which is how a cut supply line reads in
	// the population figures rather than only in a warning line.
	shortfall int
}

// Seed builds a world's genesis state from the universe seed. Everything —
// what is in the ground, what is in the warehouse, which industries stood
// up — follows from (seed, stellar) and the government that holds it.
func Seed(seed int64, stellar int, name string, system int, pop int, c govt.Color) *World {
	w := &World{
		Stellar: stellar, Name: name, System: system, Pop: pop, Govt: c,
		Credits: pop / 4,
	}
	e := econ.Endow(seed, stellar, pop, govt.MineRate(c))
	w.Reserve, w.Warehouse = e.Reserve, e.Warehouse
	w.standUpIndustry()
	w.Reprice()
	return w
}

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
func (w *World) standUpIndustry() {
	ranked := industry.Rank(w.Reserve)
	if len(ranked) > maxChains {
		ranked = ranked[:maxChains]
	}
	// Throughput scales with population: the city is the workforce, exactly
	// as the war economy already assumes for industrial points.
	rate := math.Max(float64(w.Pop), 1) / 1e6 * chainRate
	for _, ch := range ranked {
		w.Plant = append(w.Plant, ch.Assemble(rate, w.Govt))
	}
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
func (w *World) Wants(m econ.Material) float64 {
	var t float64
	for _, p := range w.Plant {
		t += p.Demand()[m]
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
	maxChains = 2
	chainRate = 55.0 // tons/day of throughput per million citizens
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
		f := 1.0
		switch {
		case cover < coverDays:
			f = 1 + 2*(1-cover/coverDays)
		default:
			f = math.Max(0.35, 1-0.45*math.Min(1, (cover-coverDays)/(coverDays*3)))
		}
		// A world that MAKES a thing sells it cheap: that is what a producer
		// is, and it is what gives a route a direction.
		if w.Makes(m) {
			f *= 0.62
		}
		w.Shop[m] = int(math.Max(1, math.Round(base*f)))
	}
}

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
func (w *World) appetite(m econ.Material) float64 {
	popM := float64(w.Pop) / 1e6
	switch m {
	case econ.Rations:
		return popM * 9.0
	case econ.Medicine:
		return popM * 1.4
	case econ.FuelCells:
		return popM * 3.2
	case econ.Lumber:
		return popM * 2.1
	case econ.Ore:
		return popM * 3.5 // construction aggregate
	case econ.Chips:
		return popM * 1.6 // the city's own electronics
	case econ.Steel:
		return popM * 2.4 // structure, and the yards
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
