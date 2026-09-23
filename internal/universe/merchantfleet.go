package universe

import (
	"fmt"
	"math"
	"sort"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// The merchant fleet is not a constant.
//
// The census was fixed at genesis for a good reason — a hull nobody is
// looking at is still a ship with a position and a manifest, not a number on
// an overlay — and that reason is untouched. What was wrong was the SIZE.
// Sixteen hulls a colour is the right fleet for the eleven-world balance rig
// and an absurd one for the hundred-and-nine-port gazetteer the game
// actually seeds: forty-eight ships against four thousand live routes
// deliver about one and a half cargoes per port per year, which is
// indistinguishable from no trade at all. Every downstream failure in the
// first lithium run traced back to it. The refineries mined ore they could
// not process because nobody flew them acid; the yards pressed no plate
// because nobody flew them chemicals; and the fuel economy, whose nameplate
// capacity covered its appetite 1.2 times over, produced nothing whatsoever
// for two simulated years.
//
// So the fleet is sized by the market instead, on two rules:
//
//   - OPENING. A colour is founded with hulls proportional to the worlds it
//     holds, because a trade network's size is a property of the map and not
//     of a constant somebody typed once.
//
//   - GROWTH. Every day, each colour compares the margin sitting unserved on
//     its board against the hulls it has to serve it. High pressure presses
//     plate; low pressure breaks a ship up. A route that pays better gets
//     more ships pointed at it without anybody deciding to point them,
//     which is the whole of the mechanism and all of its charm.
//
// Nothing here is minted. A commissioned hull is Hull TONS out of a yard's
// warehouse and credits out of an exchequer into that yard's treasury; a
// laid-up hull is the same tons put back on the same shelf. Both books
// balance across the transaction, which is exactly why the fleet is allowed
// to breathe: growth here costs somebody a warehouse.

// berthPressure is the credits of unserved margin per hull a colour has
// afloat. It is the single number the fleet responds to.
//
// Unserved means what it says: FindRoutes lists what the boards are paying
// today, dispatch strikes a parcel off the list as it is loaded, so whatever
// is still on the list at the end of a tick is margin nobody carried.
func berthPressure(routes []Route, hulls int) float64 {
	var v float64
	for _, r := range routes {
		v += r.Value()
	}
	return v / math.Max(float64(hulls), 1)
}

// Fleet-sizing knobs. They live in Tuning so a balance pass is one diff and
// so the console can turn them while a game is running.
const (
	// defaultOpeningHulls is hulls per world held, at genesis.
	defaultOpeningHulls = 5
	// defaultFleetCap is the most hulls a colour may have per world held.
	// The ceiling is per-world rather than absolute so that losing half
	// your territory eventually costs you half your merchant marine.
	defaultFleetCap = 8
	// defaultCommissionAt is the berth pressure above which a yard presses
	// plate, in credits of unserved margin per hull.
	defaultCommissionAt = 90_000
	// defaultLayUpAt is the pressure below which a yard breaks one up.
	// The gap between the two is deliberate and wide: a fleet that
	// commissions and lays up around a single threshold oscillates, and an
	// oscillating merchant marine is worse than a fixed one.
	defaultLayUpAt = 22_000
)

// FleetCap is the most hulls this colour may have afloat at once.
func (u *Universe) FleetCap(c govt.Color) int {
	// Sublinear past the span of control: a stretched government cannot
	// crew, berth or victual eight hulls a world. See Overhead.
	n := int(float64(len(u.worldsOf(c))*u.Tune.FleetCap) * u.Overhead(c))
	if n < minCensus {
		n = minCensus
	}
	return n
}

// minCensus is the smallest merchant marine a colour is allowed, however
// little territory it holds. A colour with no ships cannot trade its way
// back into the game, and a game state nobody can recover from is not a
// difficulty setting, it is an ending.
const minCensus = 6

// sizeFleet is the day's commissioning decision for one colour.
func (u *Universe) sizeFleet(c govt.Color, routes []Route) {
	hulls := u.Fleet.ByGovt(c)
	p := berthPressure(routes, len(hulls))
	switch {
	case p >= float64(u.Tune.CommissionAt) && len(hulls) < u.FleetCap(c):
		u.commission(c, p)
	case p <= float64(u.Tune.LayUpAt) && len(hulls) > minCensus:
		u.layUp(c, hulls, p)
	}
}

// commission puts one more hull on the board.
//
// It prefers to recommission a ship that was laid up — the row is still in
// the census, the name is still on it, and a yard that broke a hull up has
// its plate on the shelf — before pressing a new one. That is why the census
// grows to the cap and then breathes instead of filling with dead rows.
func (u *Universe) commission(c govt.Color, pressure float64) {
	yard := u.bestYard(c)
	if yard == nil {
		return
	}
	// A laid-up hull first.
	var pick *traffic.Hull
	for _, h := range u.Fleet.Hulls {
		if h.Govt == c && h.Status == traffic.LaidUp {
			if pick == nil || h.Dry < pick.Dry {
				pick = h
			}
		}
	}
	dry := 0.0
	if pick != nil {
		dry = pick.Dry
	} else {
		dry = 220.0 + float64(u.Rng.Intn(680))
	}
	if yard.Warehouse[econ.Hull] < dry {
		return
	}
	// What a hull actually COSTS in a zero-sum world is its tons. The
	// credits are a transfer: the yard is the colour's own world, so a
	// state that "buys" a ship there moves money from one of its pockets
	// to another and nothing real happens. Gating growth on that transfer
	// was gating it on an accounting artefact — and, worse, on an exchequer
	// the auto-governor empties into buildings every seven days.
	//
	// So the gate is the plate and the pilot. The yard must have the
	// tonnage on the shelf, and it must be able to stake the pilot who
	// will fly it. Both are real, both are conserved, and both trace
	// straight back through steel and chemicals to somebody's mine — which
	// makes the size of a merchant marine a downstream fact about mining
	// and hauling rather than a budget line.
	if yard.Credits < u.Tune.StartPurse {
		return
	}
	yard.Warehouse.Take(econ.Hull, dry) // read back by Fleet.Structure()
	// The state subsidises the yard when it can afford to. It is not a
	// condition of building, only a transfer that keeps a colour's tariff
	// income circulating back into the ports that earned it.
	subsidy := int(dry) * maxInt(yard.Shop[econ.Hull], 1)
	if u.Exchequer[c] >= subsidy+u.Tune.ExchequerReserve {
		econ.Pay(&u.Exchequer[c], &yard.Credits, subsidy)
	}

	h := pick
	if h == nil {
		h = &traffic.Hull{
			ID:     len(u.Fleet.Hulls),
			Name:   fmt.Sprintf("%s %02d", c, u.nextHullNumber(c)),
			Govt:   c,
			Dry:    dry,
			Thrust: 2200 + float64(u.Rng.Intn(1800)),
		}
		u.Fleet.Add(h)
	}
	h.Status = traffic.Idle
	h.Mission = traffic.Courier
	h.Home, h.From, h.To = yard.Stellar, yard.Stellar, yard.Stellar
	h.Cargo = econ.Stock{}
	h.V, h.S = 0, 0
	h.Mass = h.Wet()
	econ.Pay(&yard.Credits, &h.Purse, u.Tune.StartPurse)
	verb := "commissioned"
	if pick != nil {
		verb = "recommissioned"
	}
	u.Journal.Logf(u.Day, h.ID, "%s %s at %s — %.0ft of plate (board pays %.0fk/hull)",
		h.Name, verb, yard.Name, dry, pressure/1000)
}

// layUp breaks up the least useful hull a colour has and puts its plate back
// on a yard's shelf. The ship it breaks is the one that has carried least —
// the market retiring its worst operator, not a random casualty.
func (u *Universe) layUp(c govt.Color, hulls []*traffic.Hull, pressure float64) {
	var pick *traffic.Hull
	for _, h := range hulls {
		if h.Status != traffic.Idle || h.Laden() > 0 {
			continue
		}
		if w := u.Worlds[h.Home]; w == nil || w.Govt != c {
			continue // a yard of its own colour, or it stays where it is
		}
		if pick == nil || h.Tons < pick.Tons {
			pick = h
		}
	}
	if pick == nil {
		return
	}
	yard := u.Worlds[pick.Home]
	yard.Warehouse.Add(econ.Hull, pick.Dry)
	econ.Pay(&pick.Purse, &yard.Credits, pick.Purse) // the owner cashes out
	pick.Status = traffic.LaidUp
	pick.Mass = 0
	u.Journal.Logf(u.Day, pick.ID, "%s laid up at %s — %.0ft of plate back on the shelf (board pays only %.0fk/hull)",
		pick.Name, yard.Name, pick.Dry, pressure/1000)
}

// bestYard is where this colour would rather build: the world of its own
// holding the most hull plate, ties to the lower stellar so the choice is
// deterministic.
func (u *Universe) bestYard(c govt.Color) *World {
	var best *World
	for _, id := range u.order {
		w := u.Worlds[id]
		if w.Govt != c {
			continue
		}
		if best == nil || w.Warehouse[econ.Hull] > best.Warehouse[econ.Hull] {
			best = w
		}
	}
	if best == nil || best.Warehouse[econ.Hull] <= 0 {
		return nil
	}
	return best
}

// nextHullNumber is the next unused pennant for a colour.
func (u *Universe) nextHullNumber(c govt.Color) int {
	n := 0
	for _, h := range u.Fleet.Hulls {
		if h.Govt == c {
			n++
		}
	}
	return n + 1
}

// FleetReport renders the merchant marine's size and what the board is
// paying it, for the desk and the console.
func (u *Universe) FleetReport() []string {
	out := make([]string, 0, 4)
	for _, c := range govt.Colors() {
		hulls := u.Fleet.ByGovt(c)
		var laid int
		for _, h := range u.Fleet.Hulls {
			if h.Govt == c && h.Status == traffic.LaidUp {
				laid++
			}
		}
		p := berthPressure(u.FindRoutes(c, 0), len(hulls))
		out = append(out, fmt.Sprintf("%-5s %3d afloat of %3d cap · %2d laid up · board pays %.0fk cr/hull (press at %dk, lay up at %dk)",
			c, len(hulls), u.FleetCap(c), laid, p/1000,
			u.Tune.CommissionAt/1000, u.Tune.LayUpAt/1000))
	}
	sort.Strings(out)
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
