package industry

import (
	"sort"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
)

// The catalogue is deliberately small and deliberately not a tech tree. Ten
// primitives, each doing one obvious thing, and every interesting industry in
// the game is a COMPOSITION of them rather than an entry in a longer table.
// A world that can smelt and fabricate has a chip industry; nobody wrote
// "chip industry" down anywhere.

// Kind identifies a primitive module.
type Kind int

const (
	MineFerrite Kind = iota
	MineCuprite
	MineSilicate
	WellVolatiles
	FarmBiomass

	Smelter  // ferrite  → steel
	Refinery // cuprite  → copper
	Furnace  // silicate → silicon
	Cracker  // volatiles→ polymer
	Thresher // biomass  → grain

	Mill      // biomass          → lumber
	Cannery   // grain            → rations
	Pharma    // grain + polymer  → medicine
	Fab       // silicon + copper → chips
	CellPlant // polymer + copper → fuel cells
	Crusher   // ferrite          → ore (bulk, sold as dug)

	KindCount
)

var kindNames = [KindCount]string{
	MineFerrite: "Ferrite mine", MineCuprite: "Cuprite mine",
	MineSilicate: "Silicate mine", WellVolatiles: "Volatiles well",
	FarmBiomass: "Biomass farm",
	Smelter:     "Smelter", Refinery: "Refinery", Furnace: "Furnace",
	Cracker: "Cracker", Thresher: "Thresher",
	Mill: "Mill", Cannery: "Cannery", Pharma: "Pharma works",
	Fab: "Fabricator", CellPlant: "Cell plant", Crusher: "Ore crusher",
}

func (k Kind) String() string {
	if k < 0 || k >= KindCount {
		return "?"
	}
	return kindNames[k]
}

// Extractor reports whether a module lifts material out of the crust. These
// are the only modules whose inputs do not come from another module, and the
// only ones that can exhaust.
func (k Kind) Extractor() bool { return k <= FarmBiomass }

// Digs is the crust material an extractor lifts, or Slag for the rest.
func (k Kind) Digs() econ.Material {
	switch k {
	case MineFerrite:
		return econ.Ferrite
	case MineCuprite:
		return econ.Cuprite
	case MineSilicate:
		return econ.Silicate
	case WellVolatiles:
		return econ.Volatiles
	case FarmBiomass:
		return econ.Biomass
	}
	return econ.Slag
}

// recipe is a primitive's port list at unit rate — one ton of throughput per
// industrial day. Everything is scaled from here.
type recipe struct {
	in  []Port
	out []Port
}

var recipes = [KindCount]recipe{
	// Extractors take nothing and yield their crust material; the mass they
	// "produce" is debited from the world's reserve by the caller, never
	// invented here.
	MineFerrite:   {out: []Port{{econ.Ferrite, 1}}},
	MineCuprite:   {out: []Port{{econ.Cuprite, 1}}},
	MineSilicate:  {out: []Port{{econ.Silicate, 1}}},
	WellVolatiles: {out: []Port{{econ.Volatiles, 1}}},
	FarmBiomass:   {out: []Port{{econ.Biomass, 1}}},

	// Refining. Each loses mass to slag, which is where the conserved tons
	// go when a process is not perfectly efficient.
	Smelter:  {in: []Port{{econ.Ferrite, 1}}, out: []Port{{econ.Steel, 0.62}}},
	Refinery: {in: []Port{{econ.Cuprite, 1}}, out: []Port{{econ.Copper, 0.48}}},
	Furnace:  {in: []Port{{econ.Silicate, 1}}, out: []Port{{econ.Silicon, 0.40}}},
	Cracker:  {in: []Port{{econ.Volatiles, 1}}, out: []Port{{econ.Polymer, 0.70}}},
	Thresher: {in: []Port{{econ.Biomass, 1}}, out: []Port{{econ.Grain, 0.75}}},

	// Goods. These are what a spaceport actually posts a price for.
	Mill:      {in: []Port{{econ.Biomass, 1}}, out: []Port{{econ.Lumber, 0.85}}},
	Cannery:   {in: []Port{{econ.Grain, 1}}, out: []Port{{econ.Rations, 0.90}}},
	Pharma:    {in: []Port{{econ.Grain, 0.6}, {econ.Polymer, 0.4}}, out: []Port{{econ.Medicine, 0.55}}},
	Fab:       {in: []Port{{econ.Silicon, 0.5}, {econ.Copper, 0.5}}, out: []Port{{econ.Chips, 0.45}}},
	CellPlant: {in: []Port{{econ.Polymer, 0.6}, {econ.Copper, 0.4}}, out: []Port{{econ.FuelCells, 0.65}}},
	Crusher:   {in: []Port{{econ.Ferrite, 1}}, out: []Port{{econ.Ore, 0.92}}},
}

// Build makes one primitive at the given daily throughput, with the owning
// government's industrial yield applied. A Blue works keeps more of what it
// puts in than a Red one; the difference falls out as slag, so the balance
// advantage costs mass rather than creating it.
func Build(k Kind, tonsPerDay float64, c govt.Color) *Module {
	if k < 0 || k >= KindCount || tonsPerDay <= 0 {
		return New("empty", nil, nil)
	}
	r := recipes[k]
	out := r.out
	if !k.Extractor() {
		// Yield bites on the product, never on the intake: a worse factory
		// does not eat less, it wastes more.
		rel := govt.Yield(c) / govt.Yield(govt.None)
		out = scale(out, rel)
	}
	m := New(k.String(), scale(r.in, tonsPerDay), scale(out, tonsPerDay))
	m.Parts = nil
	return m
}

// --- Chains --------------------------------------------------------------

// Chain is a named line of primitives that composes into one supermodule.
// These are the recipes for INDUSTRIES, and they are the only place the game
// says "these things go together" — everything else is emergent from what a
// world happens to have in the ground.
type Chain struct {
	Name  string
	Steps []Kind
	// Good is the market commodity the chain exists to make. Used to decide
	// whether a world's industry is worth anything to anybody.
	Good econ.Material
}

// Chains is the catalogue of industries a world can stand up. Each is a
// straight line from crust to a board commodity, and each composes with Then
// into a single module whose external inputs are exactly what the world must
// buy from somebody else.
var Chains = []Chain{
	{"Bulk ore", []Kind{MineFerrite, Crusher}, econ.Ore},
	{"Timber", []Kind{FarmBiomass, Mill}, econ.Lumber},
	{"Foodstuffs", []Kind{FarmBiomass, Thresher, Cannery}, econ.Rations},
	{"Pharmaceutical", []Kind{FarmBiomass, Thresher, Pharma}, econ.Medicine},
	{"Electronics", []Kind{MineSilicate, Furnace, Fab}, econ.Chips},
	{"Powercell", []Kind{WellVolatiles, Cracker, CellPlant}, econ.FuelCells},
	{"Structural steel", []Kind{MineFerrite, Smelter}, econ.Steel},
	{"Conductor", []Kind{MineCuprite, Refinery}, econ.Copper},
}

// Processing is the chain's steps with the extractors removed.
//
// A chain NAMES its mines so that Needs and Rank know what has to be in the
// ground, but it does not CONTAIN them. Digging is the world's job, not the
// factory's: the world lifts crust into its warehouse against a finite
// reserve, and the plant draws from that warehouse like any other input.
//
// Keeping the mine inside the composed module was the first real bug this
// economy had. The chain netted the mine's output against the mill's intake
// internally, so the plant appeared to need nothing and produce lumber out of
// thin air — while the world's own mining moved the same tons a second time.
// Mass was created at every tick, and the auditor caught it on day one.
func (ch Chain) Processing() []Kind {
	out := make([]Kind, 0, len(ch.Steps))
	for _, k := range ch.Steps {
		if !k.Extractor() {
			out = append(out, k)
		}
	}
	return out
}

// Assemble composes a chain's processing steps into one supermodule at the
// given throughput. The result is a module like any other: it can be scaled,
// inspected for its bottleneck, or plugged into something else again. Its
// external inputs are exactly what the world must dig or buy.
func (ch Chain) Assemble(tonsPerDay float64, c govt.Color) *Module {
	steps := ch.Processing()
	mods := make([]*Module, 0, len(steps))
	for _, k := range steps {
		mods = append(mods, Build(k, tonsPerDay, c))
	}
	return Compose(ch.Name, mods...)
}

// Needs lists the crust materials a chain cannot run without.
func (ch Chain) Needs() []econ.Material {
	seen := map[econ.Material]bool{}
	var out []econ.Material
	for _, k := range ch.Steps {
		if m := k.Digs(); m != econ.Slag && !seen[m] {
			seen[m], out = true, append(out, m)
		}
	}
	return out
}

// Viable reports whether a world with this crust can run the chain at all —
// every extractor in the line needs something left in the ground.
func (ch Chain) Viable(reserve econ.Stock) bool {
	for _, m := range ch.Needs() {
		if reserve[m] <= 0 {
			return false
		}
	}
	return len(ch.Needs()) > 0
}

// Rank orders the chains a world could actually run, richest crust first.
// This is how a world's UNIQUE industry falls out of its seed: nobody is
// assigned a speciality, they just have different rocks, and the chains that
// pay follow from that.
func Rank(reserve econ.Stock) []Chain {
	var out []Chain
	for _, ch := range Chains {
		if ch.Viable(reserve) {
			out = append(out, ch)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return chainWealth(out[i], reserve) > chainWealth(out[j], reserve)
	})
	return out
}

// chainWealth is the tonnage in the ground backing a chain, limited by its
// scarcest requirement — a line is only as rich as the thinnest seam it
// depends on.
func chainWealth(ch Chain, reserve econ.Stock) float64 {
	worst := -1.0
	for _, m := range ch.Needs() {
		if worst < 0 || reserve[m] < worst {
			worst = reserve[m]
		}
	}
	if worst < 0 {
		return 0
	}
	return worst
}
