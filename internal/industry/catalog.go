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
	MineSpodumene

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

	// The lithium line. Five stages, and the map is organised around the
	// fact that three of them will only stand up on a world nobody can
	// live on.
	//
	//	OreMill   spodumene + acid  → lithex     (leach; most of it is tailings)
	//	HotCell   lithex            → lithium    (radiant smelting, in a hot cell)
	//	Breeder   lithium + volatiles → heavylith (the fertile blanket, bred)
	//	Press     heavylith + steel → pellets    (clad solid)
	//	MeltLoop  heavylith + fluid → melt       (stable molten salt)
	//
	// The two finishing steps are the whole reason fuel has two forms: a
	// press clads a solid a thermal reactor can take, a melt loop stabilises
	// a salt only a fast loop can circulate. Nothing converts one to the
	// other, so a hull's reactor decides which half of the market it buys in.
	OreMill      // spodumene + acid   → lithex
	HotCell      // lithex             → lithium
	Breeder      // lithium + volatiles→ heavylith
	Press        // heavylith + steel  → pellets
	MeltLoop     // heavylith + fluid  → melt
	ChemWorks    // volatiles          → acid + fluid + polymer

	// The yard tier: what a fleet is made of. These are what a high-
	// population world with steel and chips on hand turns into ships, and
	// they are the reason a supply line matters — a world that cannot make
	// Rounds needs them delivered, or its garrison fights dry.
	Yard         // steel + chips + fuel cells → hull
	Arsenal      // steel + polymer            → rounds
	MissileWorks // steel + chips + polymer    → missiles

	// The returns: the two processes that send the flow back uphill. A
	// composter is on every inhabited world (a city recycles); a breaker's
	// yard is on every port (a wreck landed is steel next week). Neither
	// is a chain a world CHOOSES — they are civic, see universe.World.Civic.
	Composter // compost → biomass, on the surface
	Breaker   // scrap   → steel

	KindCount
)

var kindNames = [KindCount]string{
	MineFerrite: "Ferrite mine", MineCuprite: "Cuprite mine",
	MineSilicate: "Silicate mine", WellVolatiles: "Volatiles well",
	FarmBiomass: "Biomass farm", MineSpodumene: "Spodumene pit",
	Smelter:     "Smelter", Refinery: "Refinery", Furnace: "Furnace",
	Cracker: "Cracker", Thresher: "Thresher",
	Mill: "Mill", Cannery: "Cannery", Pharma: "Pharma works",
	Fab: "Fabricator", CellPlant: "Cell plant", Crusher: "Ore crusher",
	OreMill: "Ore mill", HotCell: "Hot cell", Breeder: "Breeder reactor",
	Press: "Pellet press", MeltLoop: "Melt loop", ChemWorks: "Chemical works",
	Yard: "Yard", Arsenal: "Arsenal", MissileWorks: "Missile works",
	Composter: "Composter", Breaker: "Breaker's yard",
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
func (k Kind) Extractor() bool { return k <= MineSpodumene }

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
	case MineSpodumene:
		return econ.Spodumene
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
	MineSpodumene: {out: []Port{{econ.Spodumene, 1}}},

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

	// The lithium line. The mill is the brutal step — four fifths of the
	// rock is tailings — and it is deliberately the cheapest to stand up,
	// so the tonnage that has to cross a lane is concentrate rather than
	// ore. Every stage after it is a hot cell somebody has to keep cold.
	OreMill:    {in: []Port{{econ.Spodumene, 0.8}, {econ.Acid, 0.2}}, out: []Port{{econ.Lithex, 0.45}}},
	HotCell:    {in: []Port{{econ.Lithex, 1}}, out: []Port{{econ.Lithium, 0.55}}},
	Breeder:    {in: []Port{{econ.Lithium, 0.7}, {econ.Volatiles, 0.3}}, out: []Port{{econ.Heavylith, 0.62}}},
	// Cladding is a jacket, not a hull. The first cut made the press 40%
	// steel by mass and the solid-fuel market never opened: a pellet line
	// competes for structural steel with every city, every yard and every
	// bastion in the galaxy, while a melt loop competes for hydraulic fluid
	// with shipyards alone. Same nameplate, same number of refineries, and
	// the pellet lines ran at 1% of capacity against the melt lines' 13%.
	Press:      {in: []Port{{econ.Heavylith, 0.78}, {econ.Steel, 0.22}}, out: []Port{{econ.Pellets, 0.86}}},
	MeltLoop:   {in: []Port{{econ.Heavylith, 0.55}, {econ.Fluid, 0.45}}, out: []Port{{econ.Melt, 0.90}}},
	// ONE works, THREE product streams, and that is not a shortcut — it is
	// the fix for a ranking collision, applied twice.
	//
	// Acid and hydraulic fluid both stand on volatiles and nothing else, so
	// as two chains they competed for the same rank slot on the same seam
	// and catalogue order decided which one the universe got. Whichever
	// came first was made in tens of thousands of tons and the other in
	// hundreds, and every yard in the game was throttled by whichever had
	// lost.
	//
	// Merging them moved the collision one seat along. POLYMER has no chain
	// of its own anywhere in this catalogue — it exists only as the middle
	// stage of a powercell line and as that line's surplus — so the moment
	// the chemical works started winning volatiles slots, the galaxy's only
	// polymer source was displaced. Two hundred and twenty-seven tons were
	// made in a year against a single capital's demand of a hundred and
	// eighty-four a DAY, every arsenal in the game fell silent, and two of
	// three capitals were rated zero for want of a material nobody had
	// noticed was a by-product.
	//
	// A cracking column yields all three, which is also what a real one
	// does: they are the same barrel of volatiles cut at different points.
	// A dedicated cracker inside a powercell line still gets more polymer
	// per ton — breadth costs efficiency — so the two remain worth telling
	// apart.
	ChemWorks: {in: []Port{{econ.Volatiles, 1}}, out: []Port{{econ.Acid, 0.30}, {econ.Fluid, 0.24}, {econ.Polymer, 0.22}}},

	// The yard tier. A hull is mostly steel with electronics and a power
	// plant; a round is a steel jacket around a polymer charge; a missile
	// is a little of everything. Every one of these takes N tons and hands
	// back N tons of product and waste, like every recipe above.
	// A hull is pure metal and pure chemical liquid: plate and wiring, the
	// acid that etched them, and the fluid in every actuator. A yard with
	// no chemical works within reach of a lane presses no plate, which is
	// the coupling that makes the tanker trade worth flying.
	Yard:         {in: []Port{{econ.Steel, 0.55}, {econ.Chips, 0.15}, {econ.FuelCells, 0.08}, {econ.Acid, 0.10}, {econ.Fluid, 0.12}}, out: []Port{{econ.Hull, 0.90}}},
	Arsenal:      {in: []Port{{econ.Steel, 0.6}, {econ.Polymer, 0.4}}, out: []Port{{econ.Rounds, 0.90}}},
	MissileWorks: {in: []Port{{econ.Steel, 0.5}, {econ.Chips, 0.2}, {econ.Polymer, 0.3}}, out: []Port{{econ.Missiles, 0.85}}},

	// The returns. Compost goes back to biomass ON THE SURFACE — into the
	// warehouse, never the reserve, because a crust reserve only ever
	// falls and the tests hold it to that. Scrap goes back to steel at a
	// breaker's yard, which is how winning a battle becomes a mining
	// operation: the loser's hulls are next week's plate.
	Composter: {in: []Port{{econ.Compost, 1}}, out: []Port{{econ.Biomass, 0.60}}},
	Breaker:   {in: []Port{{econ.Scrap, 1}}, out: []Port{{econ.Steel, 0.75}}},
}

// Makes reports whether a primitive yields this material. It exists for the
// bottleneck detector, which has to answer "who could make me some of this"
// for by-products as well as for a chain's headline Good — acid, fluid and
// polymer all come out of one column and only acid is named.
func Makes(k Kind, m econ.Material) bool {
	if k < 0 || k >= KindCount {
		return false
	}
	for _, p := range recipes[k].out {
		if p.Mat == m && p.Tons > 0 {
			return true
		}
	}
	return false
}

// Inputs is a primitive's intake at unit rate. Like Makes, it exists so the
// bottleneck detector can walk a chain backwards and ask what the stage that
// is standing idle is actually waiting for.
func Inputs(k Kind) []Port {
	if k < 0 || k >= KindCount {
		return nil
	}
	return append([]Port(nil), recipes[k].in...)
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

	// MinRad is the dose a world must be sitting in before this line will
	// stand up on it. It is a SITING RULE, not a difficulty modifier: a hot
	// cell is not merely expensive on a clean world, it is illegal there,
	// because the only place anybody will licence radiant smelting is a
	// world with nothing left to contaminate.
	//
	// This is the one thing in the catalogue that is not a function of what
	// is in the ground, and it is what turns a handful of uninhabitable
	// rocks into the busiest industrial addresses on the map.
	MinRad float64
}

// Chains is the catalogue of industries a world can stand up. Each is a
// straight line from crust to a board commodity, and each composes with Then
// into a single module whose external inputs are exactly what the world must
// buy from somebody else.
var Chains = []Chain{
	// The lithium line comes FIRST in the catalogue, deepest chain first,
	// and that ordering is load-bearing. Rank breaks ties on catalogue
	// order, and every lithium chain stands on the same seam — so without
	// this a hot world would stand up two half-lines, mill concentrate it
	// could not smelt, and the fuel trade would never start.
	{Name: "Fuel pellets", Steps: []Kind{MineSpodumene, OreMill, HotCell, Breeder, Press}, Good: econ.Pellets, MinRad: RadBreed},
	{Name: "Fuel melt", Steps: []Kind{MineSpodumene, OreMill, HotCell, Breeder, MeltLoop}, Good: econ.Melt, MinRad: RadBreed},
	{Name: "Breeding", Steps: []Kind{MineSpodumene, OreMill, HotCell, Breeder}, Good: econ.Heavylith, MinRad: RadBreed},
	{Name: "Radiant smelting", Steps: []Kind{MineSpodumene, OreMill, HotCell}, Good: econ.Lithium, MinRad: RadSmelt},
	{Name: "Lithium milling", Steps: []Kind{MineSpodumene, OreMill}, Good: econ.Lithex, MinRad: RadMill},

	// The chemical works. Acid and hydraulic fluid have no glamour and no
	// board price worth mentioning, and every mill, hot cell and shipyard
	// in the game is short of one or the other by the second week.
	//
	// It stands on volatiles ALONE, and that is a correction rather than a
	// simplification. The first cut leached acid from volatiles and cuprite
	// together; Rank scores a line by its thinnest seam, so a two-crust
	// chain almost never made a world's top two, and three hundred tons of
	// acid were made in a year against a demand of forty a day. Every yard
	// and every mill in the universe was throttled by a reagent nobody
	// produced — lesson four of the trade economy, running backwards: a
	// commodity with no SOURCE stops the things that need it, as surely as
	// a commodity with no sink stops itself.
	{Name: "Chemical works", Steps: []Kind{WellVolatiles, ChemWorks}, Good: econ.Acid},

	{Name: "Bulk ore", Steps: []Kind{MineFerrite, Crusher}, Good: econ.Ore},
	{Name: "Timber", Steps: []Kind{FarmBiomass, Mill}, Good: econ.Lumber},
	{Name: "Foodstuffs", Steps: []Kind{FarmBiomass, Thresher, Cannery}, Good: econ.Rations},
	{Name: "Pharmaceutical", Steps: []Kind{FarmBiomass, Thresher, Pharma}, Good: econ.Medicine},
	{Name: "Electronics", Steps: []Kind{MineSilicate, Furnace, Fab}, Good: econ.Chips},
	{Name: "Powercell", Steps: []Kind{WellVolatiles, Cracker, CellPlant}, Good: econ.FuelCells},
	{Name: "Structural steel", Steps: []Kind{MineFerrite, Smelter}, Good: econ.Steel},
	{Name: "Conductor", Steps: []Kind{MineCuprite, Refinery}, Good: econ.Copper},

	// The yard chains. Each stands on a ferrite seam and BUYS the rest —
	// chips, fuel cells, polymer — which is what turns a shipyard world
	// into the busiest port on the map: it is short of something every
	// day, and the couriers know it.
	{Name: "Shipyard", Steps: []Kind{MineFerrite, Smelter, Yard}, Good: econ.Hull},
	{Name: "Munitions", Steps: []Kind{MineFerrite, Smelter, Arsenal}, Good: econ.Rounds},
	{Name: "Ordnance", Steps: []Kind{MineFerrite, Smelter, MissileWorks}, Good: econ.Missiles},
}

// The three rungs of the siting rule. They are spread rather than stacked on
// one threshold so that a merely unpleasant world and a genuinely lethal one
// do different jobs: the first mills concentrate for export, the second
// smelts metal its own system's breeder will take, and only the worst worlds
// in the universe breed and finish fuel.
const (
	RadMill  = 0.10
	RadSmelt = 0.35
	RadBreed = 0.55
)

// The deepest line a world at this dose is licensed to run, and "" for a
// world too clean to be in the fuel business at all. It is exported because
// SITING IS A DECISION, not an accident of ranking: a hot world does not
// happen to end up milling lithium because the seam beat its copper, it is
// founded as a refinery and told to.
func LineFor(rad float64, preferMelt bool) string {
	switch {
	case rad >= RadBreed:
		if preferMelt {
			return "Fuel melt"
		}
		return "Fuel pellets"
	case rad >= RadSmelt:
		return "Radiant smelting"
	case rad >= RadMill:
		return "Lithium milling"
	}
	return ""
}

// Civic builds the two return-path modules every inhabited world runs
// regardless of what it chose to make, sized to the tonnage the world can
// expect to recycle per day. They are kept apart from Chains because they
// are not a speciality — nobody is "a composting world" — and because
// Makes/Wants must not see them, or every port would appear to produce
// steel and the steel trade would lose its direction.
func Civic(gardenPerDay, compostPerDay, scrapPerDay float64, c govt.Color) []*Module {
	var out []*Module
	if gardenPerDay > 0 {
		// Subsistence: every inhabited world grows SOME of its own food out
		// of whatever biomass it can dig. Imports decide whether it grows;
		// the gardens decide whether it starves. A barren rock has no
		// gardens and lives or dies by the lane.
		g := Compose("Gardens", Build(Thresher, gardenPerDay, c), Build(Cannery, gardenPerDay, c))
		out = append(out, g)
	}
	if compostPerDay > 0 {
		out = append(out, Build(Composter, compostPerDay, c))
	}
	if scrapPerDay > 0 {
		out = append(out, Build(Breaker, scrapPerDay, c))
	}
	return out
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
func (ch Chain) Viable(reserve econ.Stock, rad float64) bool {
	if rad < ch.MinRad {
		return false
	}
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
func Rank(reserve econ.Stock, rad float64) []Chain {
	var out []Chain
	for _, ch := range Chains {
		if ch.Viable(reserve, rad) {
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
