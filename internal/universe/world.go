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

	// ord is this world's index in Universe.order, so a lane length can be
	// read out of a flat array instead of hashed out of a map. With 327
	// bodies the lane map holds fifty-three thousand entries and the route
	// scan probes it sixty thousand times a day.
	ord int

	// Kind is planet, station or field, and Host is the planet a minted
	// body belongs to. Every rule that differs between the three keys off
	// Kind, so there is exactly one place to look for the differences.
	Kind BodyKind
	Host int

	// Rad is the world's dose rate, 0..1, drawn at genesis from the same
	// number as its spodumene seam. It decides three things and nothing
	// else: which chains may stand up here (industry.Chain.MinRad), how
	// big a city the world can hold, and how fast that city grows. A
	// hostile world is not a special kind of World — it is a World with a
	// high Rad, and every consequence follows from that one field.
	Rad float64

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

	// Mandate names chains this world stands up whatever its rank says —
	// the arsenal every capital is founded with. A mandated chain still
	// needs its crust; a capital with no ferrite has no arsenal, and buys.
	Mandate []string

	// Orders are this world's standing orders: N hulls or N tons, A → B,
	// every day until cancelled — or until the world changes hands, when
	// they die with the government that gave them.
	Orders []StandingOrder

	// SurveyUntil is the day a prospector's brief on this world expires,
	// and surveyBonus is the extra fraction of its dig budget the reading
	// is worth while it lasts. See prospect.go: a survey is the only thing
	// in the game that raises extraction without raising population.
	SurveyUntil int
	surveyBonus float64

	// surplusMask and needMask are which materials this world has spare and
	// which it is short of, one bit per material. Recomputed once per route
	// scan by refreshTradeMasks; see FindRoutes for why they exist.
	surplusMask, needMask uint32

	// tradeWants is this world's whole industrial demand vector, cached for
	// the life of one route scan. Wants(m) walks every plant and takes a
	// 30-wide Demand() vector BY VALUE from each — fine once, ruinous when
	// the route scan asks it thirty times per world and then twice more per
	// candidate pair.
	tradeWants econ.Stock

	// mandated is how many of the leading entries in Plant stand under a
	// Mandate rather than under Rank. The mine reads it: a mandated line
	// gets first call on the dig budget, because a world that was TOLD to
	// run a refinery and then has to out-bid its own copper mine for the
	// ore is not sited, it is merely hopeful.
	mandated int

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
func Seed(seed int64, p Port) *World {
	stellar, pop, c := p.Stellar, p.Pop, p.Govt
	w := &World{
		Stellar: stellar, Name: p.Name, System: p.System, Pop: pop, Govt: c,
		Kind:    p.Kind, Host: p.Host,
		Credits: pop / 4,
		Tariff:  neutralTariff,
		fed:     1,
	}
	if c != govt.None {
		w.Tariff = colourTariff
	}
	e := econ.Endow(seed, stellar, pop, govt.MineRate(c))
	switch p.Kind {
	case BodyField:
		// A rock with nobody on it, holding what its planet does not.
		e = econ.Complement(seed, stellar, p.Host, hostPopFor(p), govt.MineRate(c))
	case BodyStation:
		// An orbital has no ground at all — no reserve, no surface stock,
		// and therefore nothing to sell that it did not buy first.
		e.Reserve, e.Warehouse, e.Rad = econ.Stock{}, econ.Stock{}, 0
	}
	w.Reserve, w.Warehouse, w.Rad = e.Reserve, e.Warehouse, e.Rad
	// A hot world is a camp, not a city. The gazetteer's population is what
	// grew there in a universe with no spodumene under it; this is the
	// correction. Nothing else clamps population down, so this line is the
	// entire reason the richest fuel seams are worked by a few thousand
	// people in hardsuits rather than by a metropolis.
	// The treasury is deliberately NOT re-cut to the smaller population: a
	// camp was capitalised by whoever financed the seam, against the seam,
	// and it lands holding a city's working capital with a hamlet's
	// headcount. That is what lets a hostile world buy the acid, the food
	// and the steel it can never make, from day one.
	if cap := w.PopCeiling(); float64(w.Pop) > cap {
		w.Pop = int(cap)
	}
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
	// The siting rule, applied. A world hot enough to be licensed for the
	// fuel business is FOUNDED as a refinery, exactly as a capital is
	// founded with an arsenal — it does not have to out-bid its own copper
	// for the privilege. Which of the two finishing lines it runs alternates
	// on the stellar ID, so the map carries both forms of fuel and a pilot
	// with a fast loop has to go and find a melt world rather than buying
	// whatever the nearest refinery happens to pour.
	if line := industry.LineFor(w.Rad, stellar%2 == 1); line != "" {
		w.Mandate = append(w.Mandate, line)
	}
	// An orbital is a factory with no mine: Rank has no crust to work from,
	// so its industry has to be MANDATED. standUpIndustry already knows how
	// to stand up a chain's processing steps alone and buy every input —
	// the path a capital with no ferrite uses for its arsenal — and that is
	// exactly what a station is.
	if p.Kind == BodyStation && pop > 0 {
		w.Mandate = append(w.Mandate, stationLine(p.Host))
	}
	w.standUpIndustry()
	w.stake()
	w.Reprice()
	return w
}

// stake capitalises a licensed refinery.
//
// A hot world is founded with working capital sized to its OWN LINE: enough
// to buy the acid, the hydraulic fluid and the food it can never make, for
// long enough to get the first fuel out of the door and paid for.
//
// Without it the hot worlds land in a trap that is easy to miss and
// impossible to escape from the inside. No chemicals, so no fuel; no fuel,
// so no revenue; no revenue, so no chemicals. A port buys only what its
// treasury covers, so a broke world does not send a distress signal — it
// quietly declines the cargo and the courier flies on. Kestrel ran a 63 t a
// day melt line at nine per cent of capacity for a simulated year and
// nothing anywhere said why.
//
// This is the only place in the game that mints money outside the ordinary
// pop/4 rule, and it mints it at genesis, where minting is what genesis is
// for. After this the ledger is closed and every credit is conserved.
func (w *World) stake() {
	if !w.Hostile() {
		return
	}
	var daily float64
	for _, p := range w.Plant {
		d := p.Demand()
		for m := econ.Material(0); m < econ.Count; m++ {
			// Only what must be BOUGHT. Crust the world lifts itself is
			// free at the pithead and is not working capital.
			if d[m] > 0 && !m.Crust() {
				daily += d[m] * baseValue[m]
			}
		}
	}
	// A hot world buys at a scarce world's prices — it is at the end of
	// every supply line in the game — so the stake is sized at the markup
	// it will actually be charged, not at base.
	if stake := int(daily * stakeMarkup * stakeDays); stake > w.Credits {
		w.Credits = stake
	}
}

const (
	stakeMarkup = 2.2  // what a port at the end of the line actually pays
	stakeDays   = 45.0 // long enough to sell the first fuel and be paid
)

// hostPopFor recovers the population of the planet a minted body belongs
// to, which is what its seams are scaled against — a field beside a
// metropolis is a bigger rock than one beside an outpost.
func hostPopFor(p Port) int {
	if p.Kind == BodyStation {
		return p.Pop * stationShare
	}
	// A field carries no population of its own, so the port list cannot
	// tell us; scale it against a median world instead of nothing at all.
	return fieldHostPop
}

const fieldHostPop = 3_000_000

// endow stands a building up at genesis: built, but not bought.
func (w *World) endow(b Building) {
	w.Built[b]++
	w.Endowed[b]++
}

const (
	neutralTariff = 0.06
	colourTariff  = 0.12
	genesisRounds = 90.0 // tons of Rounds per million citizens at genesis
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
	// A field is a SOURCE, and nothing else. Rank looks only at what is in
	// the ground, and a field is all ground — so left to itself every rock
	// on the map stood up two factories and became a competitor to the
	// worlds it exists to supply. Worse, having industry gave it Wants,
	// which took it off the stockpile rule in mine() and set it digging
	// against demand its own idle plants would never satisfy: 2.9 megatonnes
	// of finite reserve buried in heaps in one simulated year.
	//
	// No plant, no civic, no appetite. What a field has is seams and a mass
	// driver, and the only things that happen here are a pit working to a
	// stockpile and a hull coming to collect.
	// ...with ONE exception, and it is the one the fuel trade is built on.
	// A field may not RANK a chain, but it may still be MANDATED one: a hot
	// rock is exactly the licensed site the siting rule is looking for, and
	// refusing it a refinery threw away two thirds of the galaxy's breeder
	// capacity the moment fields stopped being factories.
	fieldOnly := w.Kind == BodyField
	if fieldOnly {
		w.Civic = nil
		if len(w.Mandate) == 0 {
			w.mandated = 0
			return
		}
	}
	ranked := industry.Rank(w.Reserve, w.Rad)
	slots := maxChains + w.Built[Works]
	if fieldOnly {
		slots = len(w.Mandate) // a mandate, and nothing it chose for itself
	}
	// Mandated chains take slots first, in mandate order, if the crust can
	// back them; the rank fills what is left.
	var chosen []industry.Chain
	for _, name := range w.Mandate {
		if len(chosen) >= slots {
			break
		}
		found := false
		for _, ch := range ranked {
			if ch.Name == name {
				chosen, found = append(chosen, ch), true
				break
			}
		}
		if found {
			continue
		}
		// No crust for it: stand up the processing stages alone and buy
		// every input. A capital with no ferrite still has an arsenal — one
		// that lives on steel and polymer delivered, which is a supply line
		// somebody can cut. Assemble composes only the processing steps, so
		// this is the same module a mined chain would run minus the mine.
		for _, ch := range industry.Chains {
			if ch.Name == name {
				chosen = append(chosen, ch)
				break
			}
		}
	}
	for _, ch := range ranked {
		if fieldOnly || len(chosen) >= slots {
			break
		}
		dup := false
		for _, c := range chosen {
			dup = dup || c.Name == ch.Name
		}
		if !dup {
			chosen = append(chosen, ch)
		}
	}
	ranked = chosen
	// Count the mandates BEFORE the plants are built: the rate each chain
	// is assembled at depends on whether it is one.
	w.mandated = 0
	for _, ch := range chosen {
		for _, name := range w.Mandate {
			if ch.Name == name {
				w.mandated++
				break
			}
		}
	}
	// Throughput scales with population: the city is the workforce, exactly
	// as the war economy already assumes for industrial points.
	// Throughput scales with the workforce — and is then CAPPED AGAINST THE
	// PITHEAD, which is the correction that matters.
	//
	// A chain is built at chainRate tons a day per million citizens and a
	// mine lifts govt.MineRate tons a day per million citizens, and
	// chainRate is the larger of the two. So a world with two chains was
	// founded with two and a half times more factory than its own ground
	// could ever feed, and a capital with four chains had five times more.
	// Nameplate capacity became a number with no relationship to anything:
	// the galaxy's refineries reported 954 t/d of fuel capacity and made
	// forty-five, and every "utilisation" figure in the report was really
	// measuring how oversized the plant was rather than how short the
	// supply.
	//
	// importFactor is the part of a world's intake that is EXPECTED to
	// arrive by ship rather than come out of its own rock. At 1.8 a world
	// is built to buy nearly half of what it processes, which is enough to
	// keep every port dependent on the lanes — the point of the whole
	// economy — without designing in a permanent four-fifths idle.
	popM := math.Max(float64(w.Pop), 1)/1e6 + w.autoCrew()
	full := popM * chainRate
	// The cap is charged against the chains that were RANKED — the ones a
	// world stood up because its own rocks made them the obvious choice.
	// A mandated line is exempt, and that is not a loophole: a refinery
	// whose whole business model is importing acid and hydraulic fluid to
	// process ore it digs itself is precisely the case the pithead cap
	// gets wrong. Capping it shrank the galaxy's fuel output by a third
	// while fixing steel and chips, which is the wrong trade in a pass
	// whose subject is fuel.
	capped := full
	if n := float64(len(ranked) - w.mandated); n > 0 {
		if c := importFactor * govt.MineRate(w.Govt) * popM / n; c < capped {
			capped = c
		}
	}
	for i, ch := range ranked {
		rate := capped
		if i < w.mandated {
			rate = full
		}
		w.Plant = append(w.Plant, ch.Assemble(rate, w.Govt))
	}
	// The return path, sized to the world: a composter that can keep up
	// with what its people eat, and a breaker that can work a wreck a week.
	civicM := math.Max(float64(w.Pop), 1) / 1e6
	garden := 0.0
	if w.Reserve[econ.Biomass] > 0 || w.Warehouse[econ.Biomass] > 0 {
		// Enough biomass through a thresher and a cannery to cover the
		// subsistence share of the ration: appetite / (0.75 · 0.90 · yield).
		garden = w.appetite(econ.Rations) * gardenShare / (0.75 * 0.90)
	}
	if !fieldOnly {
		w.Civic = industry.Civic(garden, w.organicAppetite()*1.1, civicM*breakerRate, w.Govt)
	}
}

// autoCrew is the workforce a hostile world does not have, expressed in
// millions of citizens.
//
// A dose that will not let a city grow will not stop a machine, so a hot
// world is worked remotely: shift crews in hardsuits, rail lines nobody
// rides, and a smelter run from orbit. Without this the siting rule would be
// self-defeating — we would have put the only fuel seams in the universe
// under the only worlds with nobody to work them, and the lithium trade
// would never start. It scales with dose because the hotter the world, the
// more of its industry was built to run unmanned in the first place.
func (w *World) autoCrew() float64 {
	crew := autoCrew * w.Rad
	if w.Kind == BodyField {
		// A field is ALL machine — there is nobody on it at any dose — so
		// its whole workforce is this term. Without it the richest seams on
		// the map would sit under the only bodies with no one to work them,
		// which is the same trap the hostile worlds were in.
		crew += fieldCrew
	}
	return crew
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
	return w.PopCeiling() * (1 + housingPerHabitat*float64(w.Built[Habitat]))
}

// PopCeiling is how many people this world could hold before anybody builds
// anything, and it is where the dose bites hardest.
//
// A clean world gets the full ceiling. A world with a body under it does not
// get a FRACTION of that ceiling — it gets a different ceiling entirely,
// because a hot world is not a smaller version of a city, it is a sealed
// industrial site with a shift roster. At the milling threshold that is
// most of a million people under domes; at the breeder threshold it is a
// hundred and fifty thousand in hardsuits.
//
// Scaling the ordinary ceiling instead was the first cut, and it collapsed
// a four-million world to forty thousand in one step at genesis. The map is
// more legible, and the landing city more honest, with a camp that is a
// tenth of a world rather than a thousandth of one.
func (w *World) PopCeiling() float64 {
	if w.Kind == BodyField {
		return 0 // nothing lives on a rock
	}
	if w.Rad <= 0 {
		return popCeiling
	}
	return math.Max(hostileCeiling*(1-w.Rad), hostileFloor)
}

// Hostile reports whether this world is hot enough that the dose, rather
// than the market, is what decides what happens on it.
func (w *World) Hostile() bool { return w.Rad >= HostileDose }

// HostileDose is where a world stops being a place and starts being a site.
const HostileDose = 0.35

// Makes reports whether this world produces a material at all.
func (w *World) Makes(m econ.Material) bool {
	for _, p := range w.Plant {
		if p.Supply()[m] > 0 {
			return true
		}
	}
	return false
}

// MineNeed is what is worth lifting today, given what is already stacked on
// the pad. A finite reserve must not be strip-mined into a heap the plant
// can never work through, and on a hostile world that is exactly what
// happens without this: the refinery is gated by acid and fluid somebody
// else has to fly in, while its own mine keeps lifting ore against a want
// that is never satisfied. Thirty-eight thousand tons of spodumene came out
// of Kestrel's crust in one year to make eighteen hundred tons of fuel.
//
// The reserve is the only finite thing in the game. Digging it to make a
// pile is not neutral — it is the one irreversible mistake a world can make.
func (w *World) MineNeed(m econ.Material, need float64) float64 {
	if need <= 0 {
		return 0
	}
	if w.Warehouse[m] >= need*mineCover {
		return 0
	}
	return need
}

// mineCover is how many days of its own demand a world will stack on the
// pad before it stops digging.
const mineCover = 20.0

// mandateNeeds reports whether a material feeds the line this world was
// sited to run. It is the pricing half of the mandate: the mine digs for it
// first, the factory floor runs it first, and the shop bids for it hardest.
func (w *World) mandateNeeds(m econ.Material) bool {
	for _, p := range w.Mandated() {
		if p.Demand()[m] > 0 {
			return true
		}
	}
	return false
}

// Mandated is the leading slice of Plant that stands under a Mandate: the
// arsenal a capital was founded with, and the line a licensed world was
// sited for. These are the plants whose feedstock the mine serves first.
func (w *World) Mandated() []*industry.Module {
	if w.mandated > len(w.Plant) {
		return w.Plant
	}
	return w.Plant[:w.mandated]
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

// wantsAll is the whole demand vector in one pass over the plants, which is
// what Wants(m) would cost thirty times over.
func (w *World) wantsAll() econ.Stock {
	var t econ.Stock
	for _, p := range w.Plant {
		d := p.Demand()
		for m := econ.Material(0); m < econ.Count; m++ {
			t[m] += d[m]
		}
	}
	for _, p := range w.Civic {
		d := p.Demand()
		for m := econ.FirstCrust; m <= econ.LastCrust; m++ {
			t[m] += d[m]
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
	hostileCeiling     = 1.5e6    // a sealed industrial world at the milling threshold
	hostileFloor       = 40_000.0 // nobody is evacuated entirely; somebody works the seam
	autoCrew           = 3.6      // millions-of-citizens equivalent of a fully hot world's machines
	importFactor       = 1.8      // how much factory a world is built with, against its own dig budget
	housingPerHabitat  = 0.5
	luxuryExponent     = 1.2  // rich worlds want more per head; the outer-world gradient
	garrisonRoundsBurn = 0.08 // tons of Rounds a million citizens' militia fires in drills per day
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
		} else if w.mandateNeeds(m) {
			// A world bids like a starving one for the feedstock of the
			// line it was SITED for. A refinery short of acid is not
			// inconvenienced, it is shut: it has no other industry, no
			// population worth the name, and nothing else to sell.
			//
			// At the ordinary curve it could not outbid ordinary trade.
			// Acid tops out at 3x a base of 160 while ore tops out at 3x
			// a base of 220 two jumps nearer, so the couriers went where
			// the margin-per-megametre was — correctly — and fifteen
			// refineries wanting 525 t of reagent a day between them were
			// served sixty. The scarcity curve was right and the STEEPNESS
			// was wrong: desperation is not the same shape for a cargo you
			// can do without as for one you cannot.
			steep = 5.0
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
	case econ.Pellets:
		// A city's ground reactors burn clad solids: steady, unglamorous,
		// linear in heads. This is the sink that keeps pellets moving even
		// when nobody is fuelling a fleet.
		return popM * 1.15
	case econ.Melt:
		// The melt market is thinner and richer — fast loops are refits and
		// capital ships, not municipal heating — so it grows with the
		// luxury exponent like medicine and chips.
		return lux * 0.55
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
	econ.Pellets: 860, econ.Melt: 1020,

	econ.Steel: 210, econ.Copper: 340, econ.Silicon: 380,
	econ.Polymer: 190, econ.Grain: 70,

	// The lithium line, priced by how much rock and how much shielding each
	// ton cost to get. Heavylith is the most valuable ton in the game and
	// the one nobody can carry far: the two facts together are what pin the
	// finishing plants to the hostile worlds.
	econ.Lithex: 185, econ.Lithium: 540, econ.Heavylith: 1650,
	econ.Acid: 160, econ.Fluid: 235,

	// The yard tier is priced by what went into it. Scrap is worth what a
	// breaker can get back out of it; compost is worth nothing to anybody
	// but the soil.
	econ.Hull: 900, econ.Rounds: 700, econ.Missiles: 1400,
	econ.Compost: 0, econ.Scrap: 120,

	econ.Ferrite: 60, econ.Cuprite: 95, econ.Silicate: 80,
	econ.Volatiles: 70, econ.Biomass: 40, econ.Spodumene: 130,

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
