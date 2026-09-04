package econ

import (
	"fmt"
	"math"
	"sort"
)

// Books is the universe's mass balance. It records what was in the ground at
// genesis and, forever after, answers one question: is it all still here?
//
// This is not a debug aid bolted on afterwards. It is the specification. Any
// process that mines, refines, hauls, sells, eats or blows up has to move
// tons from one pool to another, and if it invents or loses a ton the audit
// says so with the material's name on it. Balance work on an economy whose
// mass leaks is folklore, not balance work.
type Books struct {
	// Genesis is the total tonnage of every material the universe was
	// created with. Nothing ever changes it.
	Genesis Stock

	// Tol is how much drift a single material may show before the audit
	// calls it a leak. Float addition over millions of transactions will not
	// land exactly, so the tolerance is proportional to the genesis mass
	// with a small absolute floor.
	Tol float64
}

// NewBooks opens the books on a universe that starts with `genesis` tons in
// it. Call it once, after seeding, before anything runs.
func NewBooks(genesis Stock) *Books {
	return &Books{Genesis: genesis, Tol: 1e-6}
}

// Discrepancy is one material that does not add up.
type Discrepancy struct {
	Mat     Material
	Genesis float64
	Found   float64
}

func (d Discrepancy) Delta() float64 { return d.Found - d.Genesis }

func (d Discrepancy) String() string {
	verb := "created"
	if d.Delta() < 0 {
		verb = "lost"
	}
	what := d.Mat.String()
	if d.Mat < 0 {
		what = "THE UNIVERSE"
	}
	return fmt.Sprintf("%s: %.6f t %s (genesis %.3f, found %.3f)",
		what, math.Abs(d.Delta()), verb, d.Genesis, d.Found)
}

// Audit rolls every pool in the universe into one column and compares the
// TOTAL TONNAGE with genesis. Pass EVERY pool — crusts, warehouses, holds,
// the consumed sink. A pool left out of the call reads exactly like a leak,
// which is the correct and useful failure: forgetting to count a warehouse
// IS losing track of the mass in it.
//
// The check is on the total and not on each material, and that is deliberate.
// Transformation is the entire point of industry: a smelter is SUPPOSED to
// destroy ferrite and create steel, and a chain that could not change what a
// ton is would not be a factory. What no process may do is change how many
// tons there are. So the grand total is the invariant, and the per-material
// column is reported alongside it as evidence rather than as a rule.
//
// Transformation is confined to one function in the whole game —
// universe.produce — for exactly this reason. Everywhere else, mass moves
// between pools without changing what it is, which Transfer guarantees
// structurally.
func (b *Books) Audit(pools ...Stock) []Discrepancy {
	var found Stock
	for _, p := range pools {
		found = found.Plus(p)
	}
	want, got := b.Genesis.Total(), found.Total()
	if math.Abs(got-want) <= b.Tol+math.Abs(want)*1e-9 {
		return nil
	}
	// The books are out. Lead with the whole-universe figure, then name the
	// materials that moved most, because "3 tons appeared" is not actionable
	// and "3 tons of ferrite appeared" is.
	out := []Discrepancy{{Mat: -1, Genesis: want, Found: got}}
	for m := Material(0); m < Count; m++ {
		if math.Abs(found[m]-b.Genesis[m]) > b.Tol {
			out = append(out, Discrepancy{Mat: m, Genesis: b.Genesis[m], Found: found[m]})
		}
	}
	sort.Slice(out[1:], func(i, j int) bool {
		return math.Abs(out[1+i].Delta()) > math.Abs(out[1+j].Delta())
	})
	return out
}

// Balanced is Audit as a yes/no, for the common case.
func (b *Books) Balanced(pools ...Stock) bool { return len(b.Audit(pools...)) == 0 }

// Transfer moves tons from one pool to another and returns how much actually
// moved. It is the ONLY sanctioned way to move mass, because it cannot
// create any: it takes first, and adds exactly what the take returned.
//
// Every leak found while building this economy was a place that added to the
// destination the amount it MEANT to move rather than the amount it GOT.
func Transfer(from, to *Stock, m Material, tons float64) float64 {
	got := from.Take(m, tons)
	to.Add(m, got)
	return got
}

// Consume moves tons out of a pool and into the sink, which is how anything
// is "used up" without leaving the books. Burning fuel, eating rations and
// losing a cargo to enemy fire all end here.
//
// The mass keeps its identity on the way in: a ton of rations eaten shows up
// in the sink as a ton of rations, not as an anonymous ton of slag. That
// costs nothing and buys two things — the sink doubles as a consumption
// ledger you can read, and per-material drift stays confined to the one
// function that is allowed to transform matter.
func Consume(from, sink *Stock, m Material, tons float64) float64 {
	got := from.Take(m, tons)
	sink.Add(m, got)
	return got
}
