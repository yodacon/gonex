package universe

// Finding what the economy is short of, and doing something about it.
//
// Every balance pass on this economy so far has been archaeology: print a
// year of trade, stare at the numbers, and work backwards to the one stage
// that was starved. That works, and it found thirteen faults, but it only
// works because a person is doing it — the simulation itself has never known
// what it was short of.
//
// This file is that knowledge, made first-class and derived rather than
// authored. Nothing here decides what matters. The strain counters record
// what the economy tried to do and could not, the detector reads them back
// and attributes a CAUSE to each shortage, and the capitals act on it.
//
// The four causes are the four faults this project has actually shipped, in
// the order it found them:
//
//	NoPlant  nothing in the galaxy makes it            (the polymer fault)
//	NoTurn   the seam is there; the dig went elsewhere (the silicate fault)
//	NoMoney  it reached the pad and nobody could pay   (the orbital fault)
//	NoLane   there is a heap of it three jumps away    (the copper fault)
//
// A fifth, NoFeed, is the honest "its own upstream is short" — which is not
// a fault, it is the chain working as designed one level up.

import (
	"fmt"
	"math"
	"sort"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/industry"
)

// Strain is what the economy tried to do and could not, over one window.
//
// These are not "how much is there" — the warehouse already says that. They
// are counters of REFUSED WORK: intake a plant asked its warehouse for and
// did not get, tonnage a pit was asked to lift and had no budget for, cargo
// that reached a pad and was turned away. A quantity is only evidence of a
// bottleneck if somebody wanted it and was refused.
type Strain struct {
	// Unmet is plant intake demanded and not drawn, in tons.
	Unmet econ.Stock
	// Undug is dig want the day's budget could not cover.
	Undug econ.Stock
	// Offered and Refused are cargo landed at a pad, and the part of it the
	// port could not pay for. Refused/Offered is the money fault directly.
	Offered, Refused econ.Stock
	// Days is how many days this window covers, so every figure above can be
	// read per day without the caller knowing the window length.
	Days int
}

// perDay reads one counter as a daily rate.
func (s Strain) perDay(v float64) float64 {
	if s.Days <= 0 {
		return 0
	}
	return v / float64(s.Days)
}

// strainWindow is how many days a reading covers. Long enough that one bad
// day does not swing it, short enough that a capital retooling is answering
// this month's shortage rather than the opening position.
const strainWindow = 30

// rollStrain closes the window when it is full. The completed window is what
// everything reads; the accumulating one is never read, so a governor and a
// console command a day apart always see the same numbers.
func (u *Universe) rollStrain() {
	u.strain.Days++
	if u.strain.Days >= strainWindow {
		u.Strain, u.strain = u.strain, Strain{}
	}
}

// Cause is why a material is short. See the file comment: each one of these
// is a fault this project has shipped, and the detector exists so the next
// one is found by the simulation rather than by reading a year of logs.
type Cause int

const (
	NoFeed  Cause = iota // its own upstream is short — the chain working as designed
	NoPlant              // nothing in the galaxy stands up a line for it
	NoTurn               // the seam is in the ground and the dig budget went elsewhere
	NoMoney              // it reached the pad and the port could not pay for it
	NoLane               // there is a surplus of it somewhere and it does not move
)

func (c Cause) String() string {
	switch c {
	case NoPlant:
		return "no plant"
	case NoTurn:
		return "no turn at the pithead"
	case NoMoney:
		return "no money at the pad"
	case NoLane:
		return "no lane"
	}
	return "upstream short"
}

// Constraint is one material the economy is short of, with the evidence.
type Constraint struct {
	Mat econ.Material
	// Unmet is tons a day of plant intake this shortage blocked. It is the
	// ranking key because it measures BLOCKED THROUGHPUT rather than
	// scarcity: a material nobody wants can be absent without being a
	// bottleneck, and a material everybody wants is a bottleneck long
	// before it runs out.
	Unmet float64
	// Made and Nameplate are galaxy-wide production and capacity, t/d.
	Made, Nameplate float64
	// Idle is tonnage sitting in warehouses on worlds with no use for it:
	// the difference between a shortage and a distribution failure.
	Idle float64
	// Reserve is what is left in the crust, for a crust material. A seam has
	// no nameplate — a pit lifts whatever the day's dig budget allows — so
	// reporting one was reporting zero for every material that comes out of
	// the ground, which reads as "nobody makes it" when the truth is the
	// opposite.
	Reserve float64
	Cause   Cause
}

// Describe renders one constraint as a line.
func (c Constraint) Describe() string {
	capacity := fmt.Sprintf("of %7.1f nameplate", c.Nameplate)
	if c.Mat.Crust() {
		capacity = fmt.Sprintf("of %6.0f kt in the ground", c.Reserve/1000)
	}
	return fmt.Sprintf("%-10s %8.1f t/d blocked · made %7.1f %s · %7.0f t idle · %s",
		c.Mat, c.Unmet, c.Made, capacity, c.Idle, c.Cause)
}

// Bottlenecks ranks what the economy is short of, worst first.
//
// It reads the last complete strain window, so it is a measurement of the
// recent past rather than of the opening position — which matters, because
// every shortage in this economy moves: fix the pithead and the bottleneck
// becomes money, fix the money and it becomes lanes.
func (u *Universe) Bottlenecks() []Constraint {
	s := u.Strain
	if s.Days == 0 {
		s = u.strain // nothing has closed yet; read the open window
	}
	if s.Days == 0 {
		return nil
	}

	// One pass over the map for nameplate, production and idle tonnage.
	var nameplate, idle, reserve econ.Stock
	for _, id := range u.Order() {
		w := u.Worlds[id]
		for m := econ.Material(0); m < econ.Count; m++ {
			reserve[m] += w.Reserve[m]
		}
		// Plant AND civic. A composter and a breaker's yard are the two
		// lines that send the flow back uphill, and leaving them out of the
		// census reported compost and scrap as materials nothing in the
		// galaxy makes — which is exactly backwards, since every inhabited
		// world runs both.
		for _, p := range append(append([]*industry.Module{}, w.Plant...), w.Civic...) {
			sup := p.Supply()
			for m := econ.Material(0); m < econ.Count; m++ {
				nameplate[m] += sup[m]
			}
		}
		for m := econ.Material(0); m < econ.Count; m++ {
			if w.Warehouse[m] <= 0 {
				continue
			}
			// Idle means nobody HERE has a use for it. A refinery sitting on
			// its own output is idle tonnage; a fab holding copper it will
			// draw tomorrow is not.
			if w.Wants(m) <= 0 && w.appetite(m) <= 0 {
				idle[m] += w.Warehouse[m]
			}
		}
	}

	var out []Constraint
	for m := econ.Material(0); m < econ.Count; m++ {
		unmet := s.perDay(s.Unmet[m])
		if unmet <= 0.01 {
			continue
		}
		c := Constraint{
			Mat:       m,
			Unmet:     unmet,
			Made:      u.Journal.Made[m] / math.Max(float64(u.Day), 1),
			Nameplate: nameplate[m],
			Idle:      idle[m],
			Reserve:   reserve[m],
		}
		if m.Crust() {
			c.Made = u.Journal.Mined[m] / math.Max(float64(u.Day), 1)
		}
		c.Cause = u.attribute(m, c, s)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Unmet != out[j].Unmet {
			return out[i].Unmet > out[j].Unmet
		}
		return out[i].Mat < out[j].Mat
	})
	return out
}

// attribute decides WHY a material is short. The order of the tests is the
// order of blame: a thing nobody makes cannot be a lane problem, and a thing
// that never left the pithead cannot be a money problem.
func (u *Universe) attribute(m econ.Material, c Constraint, s Strain) Cause {
	if !m.Crust() && c.Nameplate <= 0 {
		return NoPlant
	}
	if m.Crust() {
		if undug := s.perDay(s.Undug[m]); undug > 0.25*c.Unmet {
			return NoTurn
		}
	}
	if offered := s.Offered[m]; offered > 0 && s.Refused[m] > 0.25*offered {
		return NoMoney
	}
	// Two days of the blocked intake, sitting somewhere nobody wants it, is
	// a distribution failure rather than a shortage. One day is noise: a
	// refinery always holds today's output.
	if c.Idle > 2*c.Unmet && c.Unmet > 0 {
		return NoLane
	}
	return NoFeed
}

// --- the core world revive plan ------------------------------------------

// revive is a capital's answer to the galaxy's worst bottleneck.
//
// The three capitals are the only worlds in the game that can be TOLD what
// to do. Everywhere else industry falls out of the rock: Rank orders what a
// world's seams can back and the best two stand up, which is what makes a
// world's speciality an accident of geology rather than a decision. A
// capital is the exception, and this is what the exception is for.
//
// THE PLAN ANSWERS IN THE CURRENCY OF THE FAULT, and that is the whole
// design. The first cut of this did the obvious thing — read the top
// shortage, mandate the line that makes it — and it made the economy
// measurably worse: rounds fell 62% and rations 26% while chips did not move
// a ton, because the capitals crowded out their own founding lines to build
// capacity against shortages that were never capacity shortages. The
// previous report had already measured that and written it down: more plant
// does not make more chips.
//
// So each cause gets the answer it actually needs:
//
//	NoPlant  stand up the line. The only case where capacity IS the fault.
//	NoFeed   stand up the line one level UPSTREAM — the thing the short
//	         thing is short of — rather than more of the short thing.
//	NoMoney  capitalise the ports that are refusing the cargo. A government
//	         whose industry depends on a port can pay that port's bills; it
//	         is a transfer, so the ledger does not notice.
//	NoLane   file a standing order from the idle heap to the starved plant.
//	NoTurn   nothing. A capital cannot reach another world's pithead, and
//	         the dig ration already fixed it galaxy-wide.
//
// It acts on at most one constraint per look, so a capital tracks the
// bottleneck as it moves instead of firing every lever at once.
func (u *Universe) revive(c govt.Color) {
	capital := u.Capital(c)
	if capital == nil || capital.Seat == SeatPlayer {
		return // the player's capital is the player's business
	}
	for _, b := range u.Bottlenecks() {
		var acted bool
		switch b.Cause {
		case NoPlant:
			acted = u.reviveByPlant(capital, b, b.Mat)
		case NoFeed:
			acted = u.reviveByPlant(capital, b, u.feedstockOf(b.Mat))
		case NoMoney:
			acted = u.reviveByPurse(c, b)
		case NoLane:
			acted = u.reviveByLane(c, b)
		}
		if acted {
			u.Revivals[b.Cause]++
			return
		}
	}
}

// reviveByPlant mandates the line that makes `want` at the capital.
//
// A mandated chain does not need the crust — standUpIndustry stands up its
// processing stages alone and buys every input, which is precisely "convert
// and process". A capital with no cuprite can still refine copper; it just
// has to buy ore, and its people are the workforce that makes that pay.
func (u *Universe) reviveByPlant(capital *World, b Constraint, want econ.Material) bool {
	ch := chainFor(want)
	if ch == nil || hasMandate(capital, ch.Name) {
		return false
	}
	u.mandate(capital, ch.Name)
	u.Journal.Logf(u.Day, -1, "%s retools: %s, against %.0f t/d of %s nobody could draw (%s)",
		capital.Name, ch.Name, b.Unmet, b.Mat, b.Cause)
	return true
}

// reviveByPurse capitalises the ports that are turning this cargo away.
//
// This is the answer to the fault the chip famine report ended on: a port
// prices by scarcity, so the world most desperate for a material posts the
// ceiling price for it and can therefore afford the least of it, and no
// amount of extra production or extra hulls reaches a buyer with an empty
// drawer. The stake fixes it once at genesis and the world falls back into
// it as soon as the stake is spent.
//
// A government pays because it is the one agent in the game with a reason
// to: its own yards, arsenals and refineries are downstream of these ports.
// It pays NEUTRAL ports too, and that is the point rather than an oversight
// — two thirds of the galaxy's finishing plant flies no flag, and a subsidy
// that stopped at the border would miss almost all of it.
func (u *Universe) reviveByPurse(c govt.Color, b Constraint) bool {
	exch := &u.Exchequer[c]
	if *exch <= u.Tune.ExchequerReserve {
		return false
	}
	// The ports that want this material, hold none of it, and cannot pay
	// for a day of what they want. Sorted worst first so a thin exchequer
	// reaches the worst-stuck port rather than the first one in map order.
	type stuck struct {
		w    *World
		bill int
	}
	var list []stuck
	for _, id := range u.Order() {
		w := u.Worlds[id]
		if w.Govt != c && w.Govt != govt.None {
			continue // not paying for an enemy's factories
		}
		want := w.Wants(b.Mat)
		if want <= 0 || w.Warehouse[b.Mat] > want {
			continue
		}
		bill := int(want * float64(w.Shop[b.Mat]) * reviveCoverDays)
		if bill <= w.Credits {
			continue
		}
		list = append(list, stuck{w, bill - w.Credits})
	}
	if len(list) == 0 {
		return false
	}
	sort.Slice(list, func(i, j int) bool { return list[i].bill > list[j].bill })
	var paid int
	for _, s := range list {
		room := *exch - u.Tune.ExchequerReserve
		if room <= 0 {
			break
		}
		pay := s.bill
		if pay > room {
			pay = room
		}
		econ.Pay(exch, &s.w.Credits, pay)
		paid += pay
	}
	if paid <= 0 {
		return false
	}
	u.Journal.Logf(u.Day, -1, "%s capitalises %d ports refusing %s — %d cr against %.0f t/d nobody could pay for",
		c, len(list), b.Mat, paid, b.Unmet)
	return true
}

// reviveCoverDays is how many days of a port's own intake the exchequer will
// put in its drawer. Short: this is working capital to get the next cargo
// off the pad, not an endowment.
const reviveCoverDays = 6.0

// reviveByLane files a standing order from the biggest idle heap of a
// material to the world most starved of it.
//
// The heap is the evidence. A material with two days of blocked intake
// sitting in a warehouse whose owner has no use for it is not scarce, it is
// misplaced, and the route board will not fix it on its own: the board ranks
// by margin per megametre and a long haul of an intermediate loses to a
// short hop of a board good every time.
func (u *Universe) reviveByLane(c govt.Color, b Constraint) bool {
	var src, dst *World
	for _, id := range u.Order() {
		w := u.Worlds[id]
		if w.Govt != c && w.Govt != govt.None {
			continue
		}
		if w.Wants(b.Mat) <= 0 && w.appetite(b.Mat) <= 0 {
			if w.Warehouse[b.Mat] > minLoad && (src == nil || w.Warehouse[b.Mat] > src.Warehouse[b.Mat]) {
				src = w
			}
			continue
		}
		short := w.Wants(b.Mat) - w.Warehouse[b.Mat]
		if short <= 0 {
			continue
		}
		if dst == nil || short > dst.Wants(b.Mat)-dst.Warehouse[b.Mat] {
			dst = w
		}
	}
	if src == nil || dst == nil || src == dst {
		return false
	}
	for _, o := range src.Orders {
		if o.To == dst.Stellar && o.Mat == b.Mat {
			return false // already running
		}
	}
	if u.File(StandingOrder{From: src.Stellar, To: dst.Stellar, Owner: c, Mat: b.Mat, Tons: minLoad}) != nil {
		return false
	}
	u.Journal.Logf(u.Day, -1, "%s orders %s from %s (%.0f t on the pad) to %s (%.0f t/d short)",
		c, b.Mat, src.Name, src.Warehouse[b.Mat], dst.Name, dst.Wants(b.Mat))
	return true
}

// feedstockOf is the material a short material is itself short of: the
// scarcest input of the line that makes it. Answering NoFeed by building
// more of the short thing is how an economy ends up with idle plant
// everywhere; the thing to build is what the idle plant is waiting for.
func (u *Universe) feedstockOf(m econ.Material) econ.Material {
	ch := chainFor(m)
	if ch == nil {
		return m
	}
	var worst econ.Material = m
	var worstUnmet float64
	s := u.Strain
	if s.Days == 0 {
		s = u.strain
	}
	for _, k := range ch.Processing() {
		for _, port := range industry.Inputs(k) {
			if port.Mat == m {
				continue
			}
			if v := s.Unmet[port.Mat]; v > worstUnmet {
				worst, worstUnmet = port.Mat, v
			}
		}
	}
	return worst
}

// reviveMax is how many lines a capital will carry under the revive plan,
// on top of whatever it was founded with. Two is enough to answer a
// bottleneck and its successor without the capital becoming the galaxy's
// only factory.
const reviveMax = 2

// mandate adds a chain to a capital's standing orders and re-stands its
// industry, retiring the oldest revive mandate if the plan is full.
//
// Mandates founded with the world — the arsenal, the yard, a hot world's
// refinery — are never retired, which is why revived is counted separately
// rather than by looking at the length of Mandate.
func (u *Universe) mandate(w *World, name string) {
	if w.revived >= reviveMax {
		// Drop the oldest revive mandate. It sits at the end of the founding
		// mandates, which are the first len(Mandate)-revived entries.
		i := len(w.Mandate) - w.revived
		if i >= 0 && i < len(w.Mandate) {
			u.Journal.Logf(u.Day, -1, "%s stands down its %s line", w.Name, w.Mandate[i])
			w.Mandate = append(w.Mandate[:i], w.Mandate[i+1:]...)
			w.revived--
		}
	}
	w.Mandate = append(w.Mandate, name)
	w.revived++
	w.standUpIndustry()
	w.Reprice()
}

// hasMandate reports whether this world is already told to run the line.
func hasMandate(w *World, name string) bool {
	for _, n := range w.Mandate {
		if n == name {
			return true
		}
	}
	return false
}

// chainFor is the catalogue line that makes a material, or nil. It prefers
// the SHALLOWEST line that produces it, because a capital answering a
// copper shortage wants a refinery, not a five-stage fuel line that happens
// to pass through copper on the way.
func chainFor(m econ.Material) *industry.Chain {
	var best *industry.Chain
	for i := range industry.Chains {
		ch := &industry.Chains[i]
		if ch.Good != m {
			continue
		}
		if best == nil || len(ch.Steps) < len(best.Steps) {
			best = ch
		}
	}
	if best != nil {
		return best
	}
	// Nothing names it as its Good — it is somebody's by-product. The
	// chemical works is the whole reason this branch exists: acid, fluid
	// and polymer all come out of one column and only acid is its Good.
	for i := range industry.Chains {
		ch := &industry.Chains[i]
		for _, k := range ch.Processing() {
			if industry.Makes(k, m) {
				return ch
			}
		}
	}
	return nil
}
