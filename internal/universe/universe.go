package universe

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/industry"
	"yodacon.org/gonex/internal/traffic"
)

// Universe is the whole running economy.
type Universe struct {
	Seed    int64
	Day     int
	Worlds  map[int]*World
	order   []int // stellar IDs, sorted: iteration must be deterministic
	Fleet   *traffic.Registry
	Journal *traffic.Journal
	Books   *econ.Books

	// Ledger is the second book: credits are conserved exactly as tons are.
	// Exchequer is each colour's central purse — tariffs in, subsidies and
	// buildings out — indexed by govt.Color, None included so a neutral
	// port's tariff has somewhere to go.
	Ledger        *econ.Ledger
	Exchequer     [4]int
	ExternCredits []func() int

	// Relations is how the colours stand to one another; absent means war.
	Relations map[[2]govt.Color]Relation
	// Charters are the bought shuttle lanes between systems.
	Charters map[[2]int]bool
	// Priority is the world each colour upgrades first, by stellar; 0 for
	// none. Set by the governor from the desk, honoured by govern().
	Priority [4]int
	// Policies is each colour's spending rule; see policy.go.
	Policies [4]Policy
	// Tune is the balance knobs; see Tuning.
	Tune Tuning

	// OnConquer fires when a world changes hands, so whoever draws the sky
	// can move the flag. This package does not know what is on the other
	// end of it.
	OnConquer func(*World)

	// overhead is each colour's span-of-control penalty, refreshed daily.
	// See targets.go.
	overhead [4]float64

	// capital is each colour's seat, by stellar id, or -1. It is elected
	// once at genesis and inherited thereafter; see Capital().
	capital [4]int

	// Revivals counts what the core world revive plan actually did, by the
	// cause it was answering. It is a diagnostic, not state the simulation
	// reads: a plan that fires a thousand times and changes nothing is the
	// failure mode worth being able to see.
	Revivals [5]int

	// Strain is the last complete window of what the economy tried to do and
	// could not, and strain the one still filling. See bottleneck.go.
	Strain Strain
	strain Strain

	// Sink is where consumed and wasted mass goes. It is a pool like any
	// other and it is handed to the auditor like any other, which is the
	// whole trick: "used up" is a place, not a disappearance.
	Sink econ.Stock

	// Extern are pools of matter this package does not own but must still
	// account for — the player's own hold, above all. A ton bought at a
	// counter has left a warehouse and is somewhere; if the auditor cannot
	// see where, the player becomes the one hole in an otherwise closed
	// universe, and the first thing anybody would do is stand at a counter
	// and buy their way out of the conservation law.
	Extern []func() econ.Stock

	Rng *rand.Rand

	// Scratch buffers for the route scan, reused between days so that
	// ranking four thousand routes does not allocate four thousand routes'
	// worth of garbage every simulated day.
	sortKeys    []routeKey
	sortScratch []Route

	// laneLen is every lane length, flat, indexed by the two worlds' ord.
	// Rebuilt by ChartLanes; nil until then, in which case the registry's
	// map is asked as before.
	laneLen []float64
}

// lane is the length between two worlds we already hold pointers to. It is
// the hot path of the route scan and it must not touch a map.
func (u *Universe) lane(a, b *World) float64 {
	if u.laneLen != nil {
		return u.laneLen[a.ord*len(u.order)+b.ord]
	}
	return u.Fleet.Lane(a.Stellar, b.Stellar).Length
}

// New seeds a universe from a list of ports.
type Port struct {
	Stellar int
	Name    string
	System  int
	Pop     int
	Govt    govt.Color

	// Kind is planet, station or field; see triad.go. The zero value is a
	// planet, so a caller that knows nothing about the triad keeps working.
	Kind BodyKind
	// Host is the planet a minted body belongs to, 0 for a planet itself.
	Host int
}

// New builds and seeds the universe, opens its books, and enrols its fleet.
// hullsPer is how many hulls each government starts with; the census is fixed
// from this moment.
func New(seed int64, ports []Port, hullsPer int) *Universe {
	u := &Universe{
		Seed:      seed,
		Worlds:    map[int]*World{},
		Journal:   traffic.NewJournal(400),
		Rng:       rand.New(rand.NewSource(seed ^ 0x5DEECE66D)),
		Relations: map[[2]govt.Color]Relation{},
		Charters:  map[[2]int]bool{},
		Tune:      DefaultTuning(),
	}
	for _, c := range govt.Colors() {
		u.Policies[c] = DefaultPolicy()
	}
	u.Fleet = traffic.NewRegistry(u.Journal)
	u.Fleet.Name = func(id int) string {
		if w := u.Worlds[id]; w != nil {
			return w.Name
		}
		return ""
	}

	var genesis econ.Stock
	for _, p := range ports {
		w := Seed(seed, p)
		u.Worlds[p.Stellar] = w
		u.order = append(u.order, p.Stellar)
		genesis = genesis.Plus(w.Genesis())
	}
	sort.Ints(u.order)
	for i, id := range u.order {
		u.Worlds[id].ord = i
	}

	u.foundCapitals()
	// Before the first tick nothing has counted anybody's worlds yet, and an
	// uninitialised overhead of zero would read as a government with no
	// capability at all — a fleet cap of nought on day zero.
	u.refreshOverhead()

	// Minimal infrastructure, the capital's share: a Works, a Bastion and a
	// Habitat at each colour's most populous world. Built, not bought — the
	// ladder does not see it — and standing before the books are opened,
	// because a Bastion's steel is genesis mass like the warehouse's.
	for _, c := range govt.Colors() {
		if cap := u.Capital(c); cap != nil {
			cap.endow(Works)
			cap.endow(Works) // two: a capital runs its mandates AND a trade
			cap.endow(Bastion)
			cap.endow(Habitat)
			// The arsenal: a capital is founded making its own rounds, if it
			// has the ferrite. The first year-long runs had every capital
			// dry by day 120, because no colour's crust ranked a Munitions
			// chain in its top two and rounds arrived only when a courier
			// found the price worth it.
			// Appended, not assigned: a capital that is also a licensed
			// refinery keeps both mandates. Overwriting here quietly
			// un-sited every hot capital in the universe.
			//
			// The yard is mandated for the same reason the arsenal is, and
			// it is the same failure a year later. Five chains stand on a
			// ferrite seam and tie exactly — bulk ore, structural steel,
			// the yard, munitions, ordnance — so catalogue order decides,
			// and the yard is third. On the eleven-world rig the governor
			// eventually bought enough Works to reach it. On the real
			// gazetteer it never did: NOT ONE TON OF HULL PLATE was pressed
			// anywhere in the galaxy in two simulated years, so no hull
			// lost in battle could ever be replaced and no merchant fleet
			// could ever grow, however hard the board was paying.
			cap.Mandate = append(cap.Mandate, "Munitions", "Shipyard")
			cap.standUpIndustry()
			cap.Reprice()
		}
	}

	// The fleet is raised BEFORE the books are opened, because a hull's dry
	// tonnage is Hull material on the books from the first day: it was
	// pressed in a yard before the game started, like the warehouse stock.
	u.raiseFleets(hullsPer)
	genesis = genesis.Plus(u.Fleet.Structure())
	u.Books = econ.NewBooks(genesis)
	u.ReopenLedger()
	return u
}

// Order is the deterministic iteration order over worlds. Map iteration in Go
// is randomised, and a simulation that iterates a map is a simulation whose
// results depend on the runtime's mood.
func (u *Universe) Order() []int { return u.order }

// raiseFleets enrols the fixed census. Hull mass and thrust are drawn from
// the seed, so the same universe always has the same ships.
func (u *Universe) raiseFleets(per int) {
	id := 0
	for _, c := range govt.Colors() {
		homes := u.worldsOf(c)
		if len(homes) == 0 {
			continue
		}
		// `per` is a FLOOR, not the answer. A trade network's size is a
		// property of the map: a colour holding twenty-six worlds needs a
		// merchant marine to match, and handing it the same sixteen hulls
		// as a colour holding three is what left the full gazetteer with
		// no trade in it at all.
		n := per
		if scaled := len(homes) * u.Tune.OpeningHulls; scaled > n {
			n = scaled
		}
		for i := 0; i < n; i++ {
			home := homes[i%len(homes)]
			dry := 220.0 + float64(u.Rng.Intn(680))
			h := &traffic.Hull{
				ID:     id,
				Name:   fmt.Sprintf("%s %02d", c, i+1),
				Govt:   c,
				Status: traffic.Idle,
				Home:   home.Stellar,
				From:   home.Stellar,
				To:     home.Stellar,
				Dry:    dry,
				Thrust: 2200 + float64(u.Rng.Intn(1800)),
			}
			h.Mass = h.Wet()
			// A pilot starts on a stake from the home treasury, not on
			// savings from nowhere: the money exists once.
			econ.Pay(&home.Credits, &h.Purse, u.Tune.StartPurse)
			u.Fleet.Add(h)
			id++
		}
	}
}

// The opening stake (Tuning.StartPurse) has to cover a real parcel at real
// prices — a hold of chips runs to tens of thousands — or the whole fleet
// deadheads from port to port looking for a cargo it can afford, which is
// exactly what the first cut did for a hundred and twenty days without
// landing a ton.

func (u *Universe) worldsOf(c govt.Color) []*World {
	var out []*World
	for _, id := range u.order {
		if w := u.Worlds[id]; w.Govt == c {
			out = append(out, w)
		}
	}
	return out
}

// Pools hands the auditor every place mass can be. Adding a new kind of
// storage anywhere in the game means adding it here, and forgetting to is
// indistinguishable from a leak — which is exactly the pressure that keeps
// the model honest.
func (u *Universe) Pools() []econ.Stock {
	pools := make([]econ.Stock, 0, len(u.Worlds)*2+2)
	for _, id := range u.order {
		w := u.Worlds[id]
		pools = append(pools, w.Reserve, w.Warehouse)
	}
	pools = append(pools, u.Fleet.CargoAfloat(), u.Fleet.Structure(), u.Fleet.DebrisAfloat(), u.Sink)
	for _, f := range u.Extern {
		pools = append(pools, f())
	}
	return pools
}

// Account registers a pool of matter held outside this package. Call it once,
// before the books matter; the closure is read on every audit.
func (u *Universe) Account(f func() econ.Stock) { u.Extern = append(u.Extern, f) }

// ReopenBooks re-bases the accounts on whatever is in the universe right
// now. It is for the two moments where the current state is the starting
// state by definition: restoring a save, and setting up a test scenario by
// hand. Calling it at any other time forgives a leak instead of finding one.
func (u *Universe) ReopenBooks() {
	var genesis econ.Stock
	for _, p := range u.Pools() {
		genesis = genesis.Plus(p)
	}
	u.Books = econ.NewBooks(genesis)
}

// Audit proves the books balance right now.
func (u *Universe) Audit() []econ.Discrepancy { return u.Books.Audit(u.Pools()...) }

// --- The day -------------------------------------------------------------

// Tick advances the universe one industrial day. The order is the causal
// order and it matters: you cannot refine ore you have not dug, sell goods
// you have not made, or route a hull to a shortage that this morning's
// production just filled.
//
// After the worlds: standing orders first (a government's schedule beats a
// pilot's opportunism for the idle hulls), then free routing, then the yards
// commission replacements for what was lost, then the governments spend.
func (u *Universe) Tick() {
	u.Day++
	u.rollStrain()
	u.refreshOverhead()
	for _, id := range u.order {
		w := u.Worlds[id]
		u.mine(w)
		u.produce(w)
		u.consume(w)
		u.grow(w)
	}
	for _, id := range u.order {
		u.Worlds[id].Reprice()
	}
	u.runOrders()
	u.govern()
	u.prospect()
	u.flyFleet()
	u.replaceHulls()
}

// prospect is the day's decision about the ground rather than the board:
// who is gathering, and who is going to go and look. See prospect.go.
func (u *Universe) prospect() {
	for _, c := range govt.Colors() {
		u.sendHarvesters(c)
		if u.Day%surveyEvery == int(c)%surveyEvery {
			u.sendSurvey(c)
		}
	}
}

// mine moves crust into the warehouse. It is the ONLY process in the game
// that increases the amount of usable material anywhere, and it does so by
// draining a finite reserve — which is why the economy is zero-sum in the
// long run rather than merely balanced in the short.
func (u *Universe) mine(w *World) {
	// Konquest's neutrals do not produce — with one exception, and it is
	// the exception the whole fuel trade is built on. A hostile world is
	// not a polity with a workforce that can be on strike; it is a licensed
	// site with machines on it, and the machines run for whoever is paying.
	// That is what makes the hot worlds the free ports of this economy:
	// nobody holds them, everybody buys from them.
	if !w.Worked() {
		return
	}
	popM := float64(w.Pop)/1e6 + w.autoCrew()
	budget := govt.MineRate(w.Govt) * popM * w.SurveyLift(u.Day)

	// Dig what the world's own industry actually wants, richest seam first.
	// A world does not mine silicate it has no furnace for.
	type want struct {
		m    econ.Material
		tons float64
	}
	// The gardens eat first. Subsistence is what keeps the population on
	// its feet; a timber mill that out-bids the cannery for biomass is a
	// world that exports lumber while it starves, and the first cut did
	// exactly that at the Red capital.
	for _, p := range w.Civic {
		for _, m := range econ.Crusts() {
			need := w.MineNeed(m, p.Demand()[m])
			if need <= 0 || w.Reserve[m] <= 0 || budget <= 0 {
				continue
			}
			got := econ.Transfer(&w.Reserve, &w.Warehouse, m, math.Min(need, budget))
			budget -= got
			if got > 0 {
				u.Journal.Mined.Add(m, got)
			}
		}
	}
	// Then the mandated lines. A world that was SITED for something — a
	// capital's arsenal, a hot world's refinery — digs for it before it
	// digs for whatever its richest seam happens to be, and for exactly the
	// same reason the gardens ate first: the dig budget is allocated
	// greedily by the size of the want, so the largest chain on the world
	// takes the lot.
	//
	// Without this the siting rule is decorative. Kestrel stood up as a
	// melt refinery wanting 142 t of spodumene a day, and its second chain
	// — an ordinary copper line wanting 177 t of cuprite — took the whole
	// 135 t budget every day for a year. The refinery never smelted a ton,
	// the fuel trade never started, and nothing anywhere logged a complaint.
	for _, p := range w.Mandated() {
		for _, m := range econ.Crusts() {
			need := w.MineNeed(m, p.Demand()[m])
			if need <= 0 || w.Reserve[m] <= 0 || budget <= 0 {
				continue
			}
			got := econ.Transfer(&w.Reserve, &w.Warehouse, m, math.Min(need, budget))
			budget -= got
			if got > 0 {
				u.Journal.Mined.Add(m, got)
			}
		}
	}
	var wants []want
	for _, m := range econ.Crusts() {
		if w.Reserve[m] <= 0 {
			continue
		}
		need := w.MineNeed(m, w.Wants(m))
		if w.Wants(m) <= 0 {
			// Nothing here eats this. A populated world still lifts a
			// trickle for trade; a FIELD, which has no industry at all and
			// would otherwise dig every material forever, digs against a
			// stockpile instead — enough on the pad for the hull that comes
			// for it, and no more.
			//
			// Without the cap the fields buried 2.8 MEGATONNES of finite
			// reserve in heaps nobody had asked for inside one simulated
			// year. The reserve is the only finite thing in this economy
			// and digging it into a pile is the one irreversible mistake a
			// world can make.
			if w.Kind == BodyField {
				need = math.Max(fieldStockpile-w.Warehouse[m], 0)
			} else {
				need = 0.15 * budget / 5
			}
		}
		if need <= 0 {
			continue
		}
		wants = append(wants, want{m, need})
	}
	sort.Slice(wants, func(i, j int) bool {
		if wants[i].tons != wants[j].tons {
			return wants[i].tons > wants[j].tons
		}
		return wants[i].m < wants[j].m
	})

	// A CONTESTED SEAM IS RATIONED, exactly as a contested warehouse is on
	// the factory floor. Serving the biggest want first and letting it take
	// the lot is the same fault produce() was fixed for, one stage upstream:
	// a world wanting 245 t/d of ferrite and 245 t/d of silicate against a
	// 273 t/d budget dug ferrite in full and silicate at a ninth, for ever,
	// because ferrite sorts first. Galaxy-wide that is what held silicate
	// at 9% of what the fabricators wanted while cuprite ran at 50%: the
	// electronics chain was not short of rock, it was short of its TURN.
	//
	// Two passes. The first gives every seam its proportional share, so
	// nobody is served last; the second spends whatever the thin seams and
	// the satisfied wants left behind, largest first, so a rationed budget
	// is never an idle one.
	var asked float64
	for _, x := range wants {
		asked += x.tons
	}
	var served econ.Stock
	budgetAtStart := budget
	if asked > budget {
		share := budget / asked
		for _, x := range wants {
			if budget <= 0 {
				break
			}
			take := math.Min(x.tons*share, budget)
			got := econ.Transfer(&w.Reserve, &w.Warehouse, x.m, take)
			budget -= got
			served[x.m] += got
			if got > 0 {
				u.Journal.Mined.Add(x.m, got)
			}
		}
	}

	for _, x := range wants {
		if budget <= 0 {
			break
		}
		take := math.Min(x.tons-served[x.m], budget)
		if take <= 0 {
			continue
		}
		got := econ.Transfer(&w.Reserve, &w.Warehouse, x.m, take)
		budget -= got
		if got > 0 {
			u.Journal.Mined.Add(x.m, got)
		}
		if w.Reserve[x.m] <= 0 {
			u.Journal.Logf(u.Day, -1, "%s: the %s is worked out", w.Name, x.m)
		}
	}

	// What the pit was asked for and had no budget to lift. This is the
	// NoTurn evidence: a seam in the ground, a plant waiting on it, and a
	// dig budget that went to the world's other chain.
	if asked > budgetAtStart {
		short := (asked - budgetAtStart) / asked
		for _, x := range wants {
			u.strain.Undug.Add(x.m, x.tons*short)
		}
	}
}

// produce runs every plant on the world for a day.
//
// This is where the module composition earns its keep: the plant is a single
// supermodule, so running it is one loop over its external ports. Whether it
// is a two-stage mill or a four-stage pharmaceutical chain makes no
// difference here, and adding a new industry means adding a Chain, not a
// case.
func (u *Universe) produce(w *World) {
	// Civic first, for the same reason the mine digs for them first: the
	// gardens take their biomass before the mill does.
	for _, p := range w.Civic {
		u.runPlant(w, p, 1)
	}
	// Then the lines this world was SITED for, in mandate order, each
	// taking what it needs before the next is asked. The mine already
	// serves them first and it would be incoherent for the factory floor
	// not to: a capital told to keep an arsenal, and then made to split its
	// ferrite fifty-fifty with the yard next door, keeps half an arsenal —
	// which over a year is no arsenal, because the garrison burns rounds
	// every day. Two of three capitals rated zero the moment the yard was
	// mandated alongside the munitions line.
	for _, p := range w.Mandated() {
		u.runPlant(w, p, u.Overhead(w.Govt))
	}
	// Then everything else — SHARING whatever they compete for.
	//
	// Running the plants in order and letting each take what it wanted was
	// the obvious way to do this and it was wrong in a way that hid for a
	// year of simulated time. Two chains on a ferrite world draw from one
	// warehouse: the ore crusher stands first in the list, asks for the
	// whole day's ferrite, and gets it, and the steel mill beside it runs
	// at zero for ever. The world's Describe() prints both plants at full
	// rate, the bottleneck report shows nothing wrong, and the only
	// symptom is a galaxy that makes 2,204 tons of ore a day and 35 tons
	// of steel — against an appetite for steel of nine hundred.
	//
	// So a contested input is rationed in proportion to what each plant
	// asked for. Nobody is served in full and nobody is served last, which
	// is both fairer and, more to the point, VISIBLE: every plant reports
	// the same throttle, so the shortage shows up on every line it touches
	// instead of being absorbed silently by whichever chain sorted second.
	rest := w.Plant[len(w.Mandated()):]
	var total econ.Stock
	for _, p := range rest {
		d := p.Demand()
		for m := econ.Material(0); m < econ.Count; m++ {
			total[m] += d[m]
		}
	}
	var share econ.Stock
	for m := econ.Material(0); m < econ.Count; m++ {
		if total[m] <= 0 {
			continue
		}
		share[m] = math.Min(1, w.Warehouse[m]/total[m])
	}
	for _, p := range rest {
		rate := u.Overhead(w.Govt)
		d := p.Demand()
		for m := econ.Material(0); m < econ.Count; m++ {
			if d[m] > 0 && share[m] < rate {
				rate = share[m]
			}
		}
		u.runPlant(w, p, rate)
	}
}

// runPlant runs one module for a day at up to `rate` of its nameplate.
//
// This is where the module composition earns its keep: the plant is a single
// supermodule, so running it is one loop over its external ports. Whether it
// is a two-stage mill or a five-stage lithium line makes no difference here,
// and adding a new industry means adding a Chain, not a case.
func (u *Universe) runPlant(w *World, plant *industry.Module, rate float64) {
	demand := plant.Demand()
	// The stage runs at whatever fraction of its inputs it can actually
	// find in the warehouse. Short one ingredient, short the whole run.
	for m := econ.Material(0); m < econ.Count; m++ {
		if demand[m] <= 0 {
			continue
		}
		if r := w.Warehouse[m] / demand[m]; r < rate {
			rate = r
		}
	}
	// Whatever the stage wanted and could not draw is the evidence the
	// bottleneck detector runs on. Recording it here rather than inferring
	// it later is what makes a shortage attributable: this is the exact
	// tonnage of work the galaxy tried to do and could not.
	if rate < 1 {
		for m := econ.Material(0); m < econ.Count; m++ {
			if demand[m] > 0 {
				u.strain.Unmet.Add(m, demand[m]*(1-rate))
			}
		}
	}
	if rate <= 1e-9 {
		return
	}
	// Consume the inputs. Take exactly what the run needs and no more.
	var drawn float64
	for m := econ.Material(0); m < econ.Count; m++ {
		if demand[m] <= 0 {
			continue
		}
		drawn += w.Warehouse.Take(m, demand[m]*rate)
	}
	// Emit the products.
	supply := plant.Supply()
	var made float64
	for m := econ.Material(0); m < econ.Count; m++ {
		if supply[m] <= 0 {
			continue
		}
		t := supply[m] * rate
		w.Warehouse.Add(m, t)
		u.Journal.Made.Add(m, t)
		made += t
	}
	// Whatever went in and did not come out is slag. Deriving it from the
	// two figures we just measured — rather than from the module's declared
	// Slag — is what makes this exact under any throttle.
	if waste := drawn - made; waste > 0 {
		u.Sink.Add(econ.Slag, waste)
	}
}

// consume is the population living: eating, medicating, burning power and
// building. Everything it takes is still counted — the organic half lands
// in the world's own Compost, which the composter turns back into biomass,
// and the rest goes to the sink. That split is what makes food renewable
// and steel not.
func (u *Universe) consume(w *World) {
	for m := econ.Material(0); m < econ.Count; m++ {
		want := w.appetite(m)
		if want <= 0 {
			continue
		}
		got := u.eat(w, m, want)
		u.Journal.Burned.Add(m, got)
		if m == econ.Rations {
			w.fed = got / want
		}
		if got < want*0.5 && w.Pop > 0 {
			w.shortfall++
		} else if w.shortfall > 0 {
			w.shortfall--
		}
	}
}

// eat takes tons off a world's shelves for its people and puts them where
// consumption of that material ends: compost or slag.
func (u *Universe) eat(w *World, m econ.Material, tons float64) float64 {
	if m.Organic() {
		got := w.Warehouse.Take(m, tons)
		w.Warehouse.Add(econ.Compost, got)
		return got
	}
	return econ.Consume(&w.Warehouse, &u.Sink, m, tons)
}

// grow compounds population — the demand side of the whole economy, and the
// axis Green leads.
//
// Growth is MADE OF RATIONS. A world that ate everything it wanted grows at
// its colour's full rate; one that ate 85% holds; one below that shrinks.
// Appetite is proportional to population, so a bigger world wants more,
// which pulls more couriers, which lets it grow more — and the ceiling is
// the food the lanes can deliver, then the housing (see Housing). Unaligned
// worlds neither grow nor starve: Konquest's neutrals do not produce.
func (u *Universe) grow(w *World) {
	// A field has no population to grow and must never be given one: minPop
	// would otherwise conjure a thousand people onto an airless rock and
	// then feed them.
	if w.Govt == govt.None || w.Pop <= 0 || w.Kind == BodyField {
		return
	}
	// Dose is a straight tax on growth, and at the breeder threshold it is
	// very nearly total. A hot world that is fed does not become a city; it
	// stays the camp it was founded as, which is the point.
	g := govt.GrowthPerDay(w.Govt) * math.Max(1-w.Rad*radGrowthBite, 0)
	var rate float64
	switch {
	case w.fed >= fedHold:
		rate = g * (w.fed - fedHold) / (1 - fedHold) // fed: grow, up to the colour's full rate
	case w.fed < fedFamine:
		rate = -famineRate * g * (fedFamine - w.fed) / fedFamine // starving: shrink, slowly
	default:
		rate = 0 // on gardens alone: hold
	}
	next := float64(w.Pop) * (1 + rate)
	if cap := w.Housing(); next > cap {
		next = math.Max(cap, float64(w.Pop)) // never shrink for want of housing alone
	}
	if next < minPop {
		next = minPop
	}
	w.Pop = int(next)
}

const (
	fedHold    = 0.85 // above this ration a population grows
	fedFamine  = 0.35 // below this it shrinks; between, it holds
	famineRate = 0.10 // a starving world shrinks at most this fraction of its growth rate
	minPop     = 1000.0

	// radGrowthBite is how much of a world's growth the dose takes at full
	// scale. Not 1.0: a camp at the worst address in the universe still
	// creeps upward when it is fed, because somebody keeps signing on.
	radGrowthBite = 0.94

	// fieldStockpile is how many tons of a material a field keeps on the pad
	// against a buyer turning up. A few hull-loads: enough that a courier
	// which diverts for it is not disappointed, little enough that the seam
	// is still in the ground when somebody actually wants it.
	fieldStockpile = 1500.0
)
