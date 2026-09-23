package universe

import (
	"fmt"
	"math"
	"slices"
	"sort"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// A route is not authored. It is FOUND, every time a hull needs something to
// do, by looking at what ports are actually paying today. Because prices move
// with real warehouse levels, a route that paid last week can be gone this
// week — the map of trade is a consequence of the simulation rather than a
// fixture in it.

// Route is one profitable run: buy a material here, sell it there.
type Route struct {
	Mat      econ.Material
	From, To int
	Buy      int     // credits per ton at the origin
	Sell     int     // credits per ton at the destination
	Tons     float64 // how much is actually available and wanted
	Margin   int     // per ton, before fuel
	Length   float64 // megametres

	// rank is the sort key, computed once per route rather than twice per
	// comparison. See rankRoutes.
	rank float64
}

// Value is the whole run's gross margin — what makes a long haul of something
// valuable beat a short hop with a thin spread.
func (r Route) Value() float64 { return float64(r.Margin) * r.Tons }

func (r Route) String() string {
	return fmt.Sprintf("%s %d→%d  %.0ft @ %d→%d (+%d/t, %.0f Mm)",
		r.Mat, r.From, r.To, r.Tons, r.Buy, r.Sell, r.Margin, r.Length)
}

// FindRoutes ranks what is worth carrying right now for one government.
//
// A hull will happily buy from a neutral port, and from its own, but never
// from an enemy: the three colours are at war, and a Red freighter does not
// dock at a Green pad to fill its hold. That single restriction is what makes
// territory economically meaningful — taking a world does not just deny it to
// the enemy, it opens a market to you.
func (u *Universe) FindRoutes(c govt.Color, limit int) []Route {
	// Scan the map ONCE for who has a surplus of what and who is short of
	// what, as two bitmasks per world, then pair the worlds up.
	//
	// The obvious loop — every origin, every destination, every material —
	// tests 109 x 109 x 26 triples and throws away 99% of them on the two
	// cheap tests at the top. Measured on the real gazetteer that is 36.8 ms
	// PER SIMULATED DAY, which is 2.2 frames of a 60 Hz budget for one day,
	// and the catch-up after a restored save is bounded at 400 days: a
	// fourteen-second freeze. The economy runs on the main thread, so this
	// was by a wide margin the most expensive thing in the game.
	//
	// With masks, a pair of worlds that could not possibly trade is rejected
	// by a single AND, and the material loop runs only over the bits they
	// actually have in common. Iteration order is unchanged — origin, then
	// destination, then material ascending — so the ranked result is
	// identical, ties included.
	u.refreshTradeMasks(c)
	var out []Route
	for _, fromID := range u.order {
		src := u.Worlds[fromID]
		if src.surplusMask == 0 {
			continue
		}
		for _, toID := range u.order {
			if toID == fromID {
				continue
			}
			dst := u.Worlds[toID]
			common := src.surplusMask & dst.needMask
			if common == 0 {
				continue
			}
			for m := econ.Material(0); m < econ.Slag; m++ {
				if common&(1<<uint(m)) == 0 {
					continue
				}
				if r, ok := u.route(c, src, dst, m); ok {
					out = append(out, r)
				}
			}
		}
	}
	u.rankRoutes(c, out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// refreshTradeMasks records, for every world this colour may deal with, which
// materials it has spare and which it is short of. One linear pass over the
// map replaces the two tests that used to run inside the innermost loop.
func (u *Universe) refreshTradeMasks(c govt.Color) {
	for _, id := range u.order {
		w := u.Worlds[id]
		w.surplusMask, w.needMask = 0, 0
		if !u.canTrade(c, w.Govt) {
			continue
		}
		w.tradeWants = w.wantsAll()
		for m := econ.Material(0); m < econ.Slag; m++ {
			if w.Shop[m] <= 0 {
				continue
			}
			want := w.tradeWants[m]
			if w.Warehouse[m]-want*reserveDays >= minLoad {
				w.surplusMask |= 1 << uint(m)
			}
			if want*reserveDays+w.appetite(m)*reserveDays-w.Warehouse[m] >= minLoad {
				w.needMask |= 1 << uint(m)
			}
		}
	}
}

// route builds the one run src → dst in m, or reports that there is none.
// It is the body of the old innermost loop, unchanged.
func (u *Universe) route(c govt.Color, src, dst *World, m econ.Material) (Route, bool) {
	buy, sell := src.Shop[m], dst.Shop[m]
	if buy <= 0 || sell <= buy {
		return Route{}, false
	}
	if shuttleOnly(m) && !u.shuttleLink(src, dst) {
		return Route{}, false
	}
	spare := src.Warehouse[m] - src.tradeWants[m]*reserveDays
	need := dst.tradeWants[m]*reserveDays + dst.appetite(m)*reserveDays - dst.Warehouse[m]
	if spare < minLoad || need < minLoad {
		return Route{}, false
	}
	r := Route{
		Mat: m, From: src.Stellar, To: dst.Stellar,
		Buy: buy, Sell: sell, Margin: sell - buy,
		Tons:   math.Min(spare, need),
		Length: u.lane(src, dst),
	}
	r.rank = u.rankOf(c, r, src, dst)
	return r, true
}

// rankOf scores one run. It is called where the two worlds are already
// pointers in hand, because looking them back up by ID afterwards cost two
// map probes per route per day — a seventh of the whole tick once the map
// carried twenty thousand routes a colour.
//
// Margin per megametre: a fat spread across the galaxy is worth less than a
// decent one next door, because the hull could have run the short one three
// times. Then OpenFront's two biases: twice the weight for a port in the
// same system, twice for an ally — a near ally is four times as likely to
// get the parcel as a distant stranger.
func (u *Universe) rankOf(c govt.Color, r Route, src, dst *World) float64 {
	v := r.Value() / math.Max(r.Length, 1)
	if src.System == dst.System {
		v *= nearBias
	}
	if dst.Govt != c && u.Relation(c, dst.Govt) == Ally {
		v *= allyBias
	}
	return v
}

// RoutesFrom is everything worth lifting out of ONE port today, ranked.
//
// It exists because a fleet cannot be dispatched off a global top-N list. A
// hull only loads a parcel that starts where it is standing, and on a
// hundred-and-nine-port map the odds that any of the galaxy's twenty best
// runs happens to begin at this particular berth are negligible — so a hull
// at a port with a full warehouse and a buyer two jumps away would deadhead
// away from both. Scanning one origin costs a hundredth of scanning the map,
// so every idle hull can afford to ask about its own doorstep.
func (u *Universe) RoutesFrom(c govt.Color, from int, limit int) []Route {
	src := u.Worlds[from]
	if src == nil {
		return nil
	}
	out := u.routesFrom(c, src, nil)
	u.rankRoutes(c, out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// routesFrom appends every run out of one port that a courier of colour c
// could take today.
func (u *Universe) routesFrom(c govt.Color, src *World, out []Route) []Route {
	if src == nil || !u.canTrade(c, src.Govt) {
		return out
	}
	for _, toID := range u.order {
		if toID == src.Stellar {
			continue
		}
		dst := u.Worlds[toID]
		if !u.canTrade(c, dst.Govt) {
			continue
		}
		for m := econ.Material(0); m < econ.Slag; m++ {
			buy, sell := src.Shop[m], dst.Shop[m]
			if buy <= 0 || sell <= buy {
				continue
			}
			// The same-type rule. Finished goods ride the interstellar
			// couriers between any two spaceports. Intermediates —
			// copper, silicon, polymer, grain — ride in-system shuttles,
			// and cross a jump only on a chartered lane.
			if shuttleOnly(m) && !u.shuttleLink(src, dst) {
				continue
			}
			// Only the surplus is for sale. A port does not sell the
			// stock its own factories are about to eat.
			spare := src.Warehouse[m] - src.Wants(m)*reserveDays
			if spare < minLoad {
				continue
			}
			// And only genuine demand is worth carrying to.
			need := dst.Wants(m)*reserveDays + dst.appetite(m)*reserveDays - dst.Warehouse[m]
			if need < minLoad {
				continue
			}
			r := Route{
				Mat: m, From: src.Stellar, To: toID,
				Buy: buy, Sell: sell, Margin: sell - buy,
				Tons:   math.Min(spare, need),
				Length: u.lane(src, dst),
			}
			r.rank = u.rankOf(c, r, src, dst)
			out = append(out, r)
		}
	}
	return out
}

// rankRoutes sorts a route list best-first, in place.
//
// Decorate, sort, undecorate. The weight of a route is a fixed property of
// it — margin per megametre, with OpenFront's near and ally biases — and
// computing it inside the comparator meant computing it twice for every
// comparison, which on four thousand routes is about a hundred thousand
// evaluations of a function that does THREE MAP LOOKUPS (both worlds and
// the lane). Profiling the economy tick put the comparator and the sort
// machinery at 55% of the whole day.
//
// slices.SortStableFunc rather than sort.SliceStable for the second half of
// it: the reflective version swaps elements through reflectlite.Swapper and
// typedmemmove, which was another fifth of the time on its own. Both are
// stable, so ties keep insertion order and the result is unchanged.
// rankRoutes sorts a route list best-first, in place. The rank itself was
// computed when each route was built; this is only the ordering.
func (u *Universe) rankRoutes(c govt.Color, out []Route) {
	// Sort a 16-byte key, not the 72-byte Route, and sort it under a TOTAL
	// ORDER — rank, then material, then both endpoints. Two routes can only
	// compare equal now if they are the same route, so the result does not
	// depend on the order the scan happened to produce them in and a stable
	// sort is no longer needed to keep it deterministic.
	//
	// That matters because stability is expensive: a stable merge sort moves
	// its elements a great deal, and with three hundred and twenty-seven
	// bodies the board carries twenty thousand routes a colour. Dropping to
	// an unstable pdqsort over a total order is the same answer, cheaper.
	if cap(u.sortKeys) < len(out) {
		u.sortKeys = make([]routeKey, len(out))
	}
	keys := u.sortKeys[:len(out)]
	for i := range out {
		keys[i] = routeKey{rank: out[i].rank, mat: out[i].Mat,
			from: int32(out[i].From), to: int32(out[i].To), idx: int32(i)}
	}
	slices.SortFunc(keys, func(a, b routeKey) int {
		switch {
		case a.rank != b.rank:
			if a.rank > b.rank {
				return -1
			}
			return 1
		case a.mat != b.mat:
			return int(a.mat) - int(b.mat)
		case a.from != b.from:
			return int(a.from - b.from)
		default:
			return int(a.to - b.to)
		}
	})
	if cap(u.sortScratch) < len(out) {
		u.sortScratch = make([]Route, len(out))
	}
	scratch := u.sortScratch[:len(out)]
	for i, k := range keys {
		scratch[i] = out[k.idx]
	}
	copy(out, scratch)
}

// routeKey is the sort payload: everything the comparator reads, and the
// index of the route it came from.
type routeKey struct {
	rank     float64
	idx      int32
	from, to int32
	mat      econ.Material
}

// canTrade reports whether a hull of colour c will dock at a port held by
// colour p. Neutral ports serve anybody; a colour serves itself and its
// allies; war and peace alike close the counter.
func (u *Universe) canTrade(c, p govt.Color) bool {
	return p == govt.None || p == c || u.Relation(c, p) == Ally
}

const (
	// reserveDays is how much of its own demand a port keeps back rather
	// than selling — nobody sells the week's flour.
	reserveDays = 8.0
	// minLoad is the smallest parcel worth sending a ship for.
	minLoad = 25.0
	// nearBias and allyBias are OpenFront's destination weights.
	nearBias = 2.0
	allyBias = 2.0
)

// --- Flying the fleet ----------------------------------------------------

// flyFleet gives every idle hull something to do and advances everybody who
// is already under way. This is the traffic half of the economy: routes are
// opinions until a hull is actually carrying something down one.
func (u *Universe) flyFleet() {
	// Cache route lists per colour: finding them is the expensive part, and
	// twenty hulls of one colour should not each redo the same scan.
	routes := map[govt.Color][]Route{}
	for _, c := range govt.Colors() {
		// The list has to be at least as long as the fleet that will eat
		// it. A flat cap of 24 meant every hull of a colour chased the same
		// two dozen parcels and the other four thousand on the board went
		// uncarried — on a small map that is invisible, on the real one it
		// is the whole trade network.
		routes[c] = u.FindRoutes(c, 4*len(u.Fleet.ByGovt(c))+24)
	}

	for _, h := range u.Fleet.Hulls {
		switch h.Status {
		case traffic.Lost, traffic.LaidUp, traffic.Resident, traffic.Fighting:
			continue
		case traffic.Idle:
			u.dispatch(h, routes)
		case traffic.Loading:
			// Loading takes a day; the cargo went aboard when the run was
			// assigned, so this is just the pad time.
			u.Fleet.Depart(h, h.From, h.To, traffic.Hauling, u.Day)
		case traffic.Hauling, traffic.Returning:
			if u.Fleet.Step(h, 1) {
				u.arrive(h)
			}
		}
	}
	// Whatever is still on the list was margin nobody carried. That is the
	// signal the yards size the fleet from, and it is measured HERE rather
	// than before dispatch because a board full of parcels that all got
	// loaded is a board that does not need more ships.
	for _, c := range govt.Colors() {
		u.sizeFleet(c, routes[c])
	}
}

// dispatch finds a hull a job and loads it. The cargo leaves the origin
// warehouse the moment it is assigned — it is on the pad, it is bought and
// paid for, and it is no longer the seller's.
func (u *Universe) dispatch(h *traffic.Hull, routes map[govt.Color][]Route) {
	// A hull that landed with cargo nobody could pay for carries it on to
	// the next port that will: a broke world is a real event, but a hold
	// full of chips parked at it forever is a hull out of the game.
	if h.Laden() >= minLoad && u.carryOn(h) {
		return
	}
	// A hull sitting somewhere other than home with nothing to carry goes
	// home; an empty ship in the wrong place is the one thing a trade fleet
	// must never leave lying around.
	list := routes[h.Govt]
	// affordable is how much of a parcel this pilot's purse covers. A broke
	// pilot does not load, and does not fly across the map to a parcel it
	// could not pay for on arrival either.
	affordable := func(r Route) float64 {
		t := math.Min(r.Tons, h.Free())
		if r.Buy > 0 {
			t = math.Min(t, float64(h.Purse/r.Buy))
		}
		return t
	}
	for i, r := range list {
		if r.From != h.Home {
			continue
		}
		src := u.Worlds[r.From]
		tons := math.Min(affordable(r), src.Warehouse[r.Mat])
		if tons < minLoad {
			continue
		}
		// Buy it: mass out of the warehouse and into the hold, credits the
		// other way. Transfer and Pay are the only movers, so this can mint
		// neither tons nor money.
		got := econ.Transfer(&src.Warehouse, &h.Cargo, r.Mat, tons)
		if got < minLoad {
			econ.Transfer(&h.Cargo, &src.Warehouse, r.Mat, got) // put it back
			continue
		}
		cost := int(got) * r.Buy
		econ.Pay(&h.Purse, &src.Credits, cost)
		h.Bought = cost
		h.Mission = traffic.Courier
		h.From, h.To, h.Status = r.From, r.To, traffic.Loading
		h.Mass = h.Wet()
		u.Journal.Logf(u.Day, h.ID, "%s loads %.0ft %s at %s for %s (+%d cr)",
			h.Name, got, r.Mat, src.Name, u.Worlds[r.To].Name, cost)
		// A taken route is taken: strike it so twenty hulls do not all fly
		// the same parcel and arrive to find it already sold.
		list[i].Tons -= got
		if list[i].Tons < minLoad {
			routes[h.Govt] = append(list[:i], list[i+1:]...)
		}
		return
	}
	// Nothing on the shared list starts here — which on the real map means
	// almost nothing, since the list is the galaxy's best runs and this is
	// one berth of a hundred and nine. So ask about this doorstep directly
	// before giving up on it. A hull standing at a port with a full
	// warehouse and a buyer two jumps away must not fly away empty, and
	// before this it did: acid was made at six hundred and seventy tons a
	// day and carried at sixteen.
	if u.loadHere(h) {
		return
	}
	// Genuinely nothing to lift here. A trader does not sit on its hands at
	// an empty port — it deadheads to where the cargo is. Flying empty costs
	// a few days and earns nothing, which is exactly the pressure that makes
	// a well-placed berth worth having.
	// Nearest origin first, not richest: a deadhead earns nothing, so the
	// only thing to minimise is how long it takes.
	var best *Route
	for i := range list {
		r := &list[i]
		if r.From == h.Home || affordable(*r) < minLoad {
			continue
		}
		if best == nil || u.Fleet.Lane(h.Home, r.From).Length < u.Fleet.Lane(h.Home, best.From).Length {
			best = r
		}
	}
	if best != nil {
		u.Fleet.Depart(h, h.Home, best.From, traffic.Returning, u.Day)
	}
}

// loadHere scans this hull's own berth for anything worth lifting, and
// loads the best parcel it can pay for. Returns whether it left.
func (u *Universe) loadHere(h *traffic.Hull) bool {
	src := u.Worlds[h.Home]
	if src == nil {
		return false
	}
	for _, r := range u.RoutesFrom(h.Govt, h.Home, berthScan) {
		tons := math.Min(math.Min(r.Tons, h.Free()), src.Warehouse[r.Mat])
		if r.Buy > 0 {
			tons = math.Min(tons, float64(h.Purse/r.Buy))
		}
		if tons < minLoad {
			continue
		}
		got := econ.Transfer(&src.Warehouse, &h.Cargo, r.Mat, tons)
		if got < minLoad {
			econ.Transfer(&h.Cargo, &src.Warehouse, r.Mat, got)
			continue
		}
		cost := int(got) * r.Buy
		econ.Pay(&h.Purse, &src.Credits, cost)
		h.Bought = cost
		h.Mission = traffic.Courier
		h.From, h.To, h.Status = r.From, r.To, traffic.Loading
		h.Mass = h.Wet()
		u.Journal.Logf(u.Day, h.ID, "%s loads %.0ft %s at %s for %s (+%d cr)",
			h.Name, got, r.Mat, src.Name, u.Worlds[r.To].Name, cost)
		return true
	}
	return false
}

// berthScan is how many of a berth's own runs a pilot will look at before
// concluding there is nothing here. Small: the list is already ranked, and
// a pilot who reads the whole board is a pilot who is not flying.
const berthScan = 8

// ChartLanes registers the true length of every lane in the universe from a
// hop count over the jump map.
//
// Until this existed every lane in the game was the registry's default 260
// megametres, which meant DISTANCE DID NOT EXIST. The route ranking divides
// margin by length to prefer a decent run next door over a fat one across
// the galaxy — and with every length identical that division was a constant,
// so the geography of the map had no effect on trade whatsoever. A hundred
// and nine ports were, economically, all in the same place.
//
// hops reports the number of jumps between two stellars, or -1 if there is
// no route. It is supplied by whoever owns the star map, because this
// package deliberately does not.
func (u *Universe) ChartLanes(hops func(from, to int) int) {
	n := len(u.order)
	u.laneLen = make([]float64, n*n)
	for i, from := range u.order {
		for j := i + 1; j < n; j++ {
			to := u.order[j]
			h := hops(from, to)
			if h < 0 {
				h = unreachableHops // there and back the long way round
			}
			l := inSystemMm + jumpMm*float64(h)
			u.Fleet.SetLane(from, to, l)
			u.laneLen[i*n+j], u.laneLen[j*n+i] = l, l
		}
		u.laneLen[i*n+i] = inSystemMm
	}
}

const (
	// inSystemMm is the run from a jump point to a pad and back — what
	// every voyage costs before it has crossed anything.
	inSystemMm = 60.0
	// jumpMm is one hyperspace link. At a typical cruise it is about seven
	// days, so a five-jump haul is a month and a decision.
	jumpMm = 180.0
	// unreachableHops is what an unroutable pair is charged. Not infinity:
	// the pair should be the worst run on the board, not an error.
	unreachableHops = 12
)

// arrive unloads a hull that has reached the far end of its lane.
//
// A flight is the exception: at a hostile world it fights, at a friendly one
// it berths as garrison, and at a colour it is now at peace with it turns
// round. Everybody else sells.
func (u *Universe) arrive(h *traffic.Hull) {
	dst := u.Worlds[h.To]
	if dst == nil {
		h.Status, h.Home = traffic.Idle, h.To
		return
	}
	switch h.Mission {
	case traffic.Flight:
		u.arriveFlight(h, dst)
		return
	case traffic.Harvester:
		h.Home, h.From, h.Status = h.To, h.To, traffic.Idle
		h.V, h.S = 0, 0
		u.harvest(h, dst)
		return
	case traffic.Survey:
		h.Home, h.From, h.Status = h.To, h.To, traffic.Idle
		h.V, h.S, h.Mission = 0, 0, traffic.Courier
		u.recordSurvey(dst)
		return
	}
	var sold float64
	var paid int
	for m := econ.Material(0); m < econ.Count; m++ {
		if h.Cargo[m] <= 0 || m == econ.Rounds && h.Mission != traffic.Convoy {
			continue // a courier keeps its own magazine
		}
		price := dst.Shop[m]
		tons := h.Cargo[m]
		// The port buys what its treasury can pay for. The rest stays
		// aboard; a broke world is a real event and the hold says so.
		if price > 0 {
			// A port buys what its treasury can pay for, and the part it
			// cannot is the NoMoney evidence — the difference between a
			// galaxy short of a material and a galaxy that cannot afford
			// the material it already has.
			afford := math.Min(tons, float64((dst.Credits-paid)/price))
			u.strain.Offered.Add(m, tons)
			if afford < tons {
				u.strain.Refused.Add(m, tons-afford)
			}
			tons = afford
		}
		if tons <= 0 {
			continue
		}
		tons = econ.Transfer(&h.Cargo, &dst.Warehouse, m, tons)
		if tons <= 0 {
			continue
		}
		paid += int(tons) * price
		sold += tons
		u.Journal.Delivered.Add(m, tons)
	}
	if sold > 0 {
		got := econ.Pay(&dst.Credits, &h.Purse, paid)
		tariff := u.tariff(h, dst, got)
		h.Tons += sold
		h.Voyages++
		u.Journal.Voyages++
		u.Journal.Logf(u.Day, h.ID, "%s delivers %.0ft at %s for %d cr (margin %d, tariff %d)",
			h.Name, sold, dst.Name, got, got-h.Bought-tariff, tariff)
		h.Bought = 0
		u.leave(h, dst)
	}
	// The hull now berths where it landed: a trader's home is wherever it
	// last unloaded, which is what lets trade patterns migrate over a long
	// game rather than every ship commuting from its birthplace forever.
	// From must follow To, or the hull is left believing it is still
	// somewhere it left days ago — and dispatch, seeing From != Home, sends
	// it "home" on a leg it has already flown. That phantom leg is what had
	// twenty-six of thirty-six hulls permanently RETURNING and nothing on
	// the board actually being carried.
	h.Home, h.From, h.Status = h.To, h.To, traffic.Idle
	h.Mission = traffic.Courier
	h.V, h.S = 0, 0
	h.Mass = h.Wet()
	dst.Reprice()
	// Anything adrift in this orbit is the berthed hulls' to lift.
	for _, d := range u.Fleet.Debris {
		if d.InOrbit() && d.At == dst.Stellar {
			u.Fleet.Scoop(h, d, u.Day)
		}
	}
}

// arriveFlight is Konquest's doFleetArrival: merge with a friendly garrison,
// fight a hostile one, and — our addition — respect a peace.
func (u *Universe) arriveFlight(h *traffic.Hull, dst *World) {
	switch {
	case dst.Govt == h.Govt:
		h.Home, h.From, h.Status = dst.Stellar, dst.Stellar, traffic.Idle
		h.Mission = traffic.Courier
		h.V, h.S = 0, 0
		u.Journal.Logf(u.Day, h.ID, "%s reinforces %s", h.Name, dst.Name)
	case u.hostile(h.Govt, dst.Govt):
		// Everyone who arrived on this lane today fights together.
		flight := []*traffic.Hull{h}
		for _, o := range u.Fleet.Hulls {
			if o != h && o.Mission == traffic.Flight && o.Status.UnderWay() &&
				o.Govt == h.Govt && o.To == dst.Stellar && u.Fleet.ETA(o) < 1 {
				o.S = u.Fleet.Lane(o.From, o.To).Length
				flight = append(flight, o)
			}
		}
		u.Engage(flight, dst)
	default:
		home := u.Capital(h.Govt)
		if home == nil {
			home = dst
		}
		h.Mission = traffic.Courier
		u.Journal.Logf(u.Day, h.ID, "%s turns back from %s — %s and %s are at peace", h.Name, dst.Name, h.Govt, dst.Govt)
		u.Fleet.Depart(h, dst.Stellar, home.Stellar, traffic.Returning, u.Day)
	}
}

// tariff is what the port takes on a sale, by relation: nothing from an
// ally, the world's own rate from everybody else — into its treasury if it
// is unaligned, into its colour's exchequer if not. OpenFront pays 10k/25k/
// 35k to encourage trade with strangers and allies; zero-sum cannot pay a
// reward, but it can charge less.
func (u *Universe) tariff(h *traffic.Hull, dst *World, paid int) int {
	if u.Relation(h.Govt, dst.Govt) == Ally && dst.Govt != govt.None {
		return 0
	}
	due := int(float64(paid) * dst.Tariff)
	if dst.Govt == govt.None {
		return econ.Pay(&h.Purse, &dst.Credits, due)
	}
	return econ.Pay(&h.Purse, &u.Exchequer[dst.Govt], due)
}

// leave is the crew on leave: a courier that lands rich somewhere other than
// its own capital spends a share of the purse on the goods this port prices
// highest above base, and eats them. The tons go where consumption goes; the
// credits go to the world's treasury. This is the arrow that closes the
// money: margins do not pool at the producers, they walk outward along the
// lanes as fast as the couriers do.
func (u *Universe) leave(h *traffic.Hull, dst *World) {
	if cap := u.Capital(h.Govt); cap != nil && cap.Stellar == dst.Stellar {
		return
	}
	budget := int(float64(h.Purse) * u.Tune.LeaveShare)
	if budget < 10 {
		return
	}
	type lux struct {
		m     econ.Material
		ratio float64
	}
	var wants []lux
	for m := econ.Material(0); m < econ.Count; m++ {
		if !m.Tradeable() || dst.Warehouse[m] < 1 || dst.Shop[m] <= 0 || baseValue[m] <= 0 {
			continue
		}
		wants = append(wants, lux{m, float64(dst.Shop[m]) / baseValue[m]})
	}
	sort.Slice(wants, func(i, j int) bool {
		if wants[i].ratio != wants[j].ratio {
			return wants[i].ratio > wants[j].ratio
		}
		return wants[i].m < wants[j].m
	})
	spent := 0
	for i, x := range wants {
		if i >= 2 || budget <= 0 {
			break
		}
		price := dst.Shop[x.m]
		tons := math.Min(math.Floor(float64(budget/2)/float64(price)), dst.Warehouse[x.m])
		if tons < 1 {
			continue
		}
		got := u.eat(dst, x.m, tons)
		cost := int(got) * price
		paid := econ.Pay(&h.Purse, &dst.Credits, cost)
		u.Journal.Burned.Add(x.m, got)
		budget -= paid
		spent += paid
	}
	if spent > 0 {
		u.Journal.Logf(u.Day, h.ID, "%s's crew spends %d cr on leave at %s", h.Name, spent, dst.Name)
	}
}

// Lose destroys a hull. Its cargo is NOT scattered into the sink: it drops
// where the hull died — on the lane, or in orbit over its port — as a wreck
// field that persists, on the books, and the nearest hulls with room take
// what they can of it, nearest first. Whatever nobody can lift stays there.
// The census records the loss forever.
func (u *Universe) Lose(h *traffic.Hull, why string) { u.wreck(h, why) }

// shuttleOnly is the same-type rule's list: the intermediates that ride
// in-system shuttles and never an interstellar courier.
//
// The exceptions are named on the material rather than here, because two
// quite different arguments produce them. Steel, acid and hydraulic fluid
// are BULKABLE — a city and a yard eat them, so they ship like goods. Pure
// lithium and heavylith are the opposite case: they are Hot, and would have
// been barred from a courier even if everybody wanted them, because a cask
// that survives a jump costs more than the ton inside it. Either way the
// answer is the same rule, and the finishing plant ends up beside the
// breeder instead of beside the customer.
func shuttleOnly(m econ.Material) bool { return m.Refined() && !m.Bulkable() }

// carryOn sends a laden idle hull to the port that pays most for what it is
// carrying and can afford it. Nothing is bought; the cargo is already the
// pilot's. Returns whether it left.
func (u *Universe) carryOn(h *traffic.Hull) bool {
	m := econ.Slag
	for x := econ.Material(0); x < econ.Count; x++ {
		if h.Cargo[x] > 0 && (m == econ.Slag || h.Cargo[x] > h.Cargo[m]) {
			m = x
		}
	}
	if m == econ.Slag || m == econ.Rounds {
		return false // a magazine is not cargo
	}
	src := u.Worlds[h.Home]
	var best *World
	var bestV float64
	for _, id := range u.order {
		dst := u.Worlds[id]
		if dst.Stellar == h.Home || !u.canTrade(h.Govt, dst.Govt) {
			continue
		}
		if src != nil && shuttleOnly(m) && !u.shuttleLink(src, dst) {
			continue
		}
		price := dst.Shop[m]
		if price <= 0 || float64(dst.Credits) < float64(price)*h.Cargo[m]/2 {
			continue
		}
		v := float64(price) / math.Max(u.Fleet.Lane(h.Home, id).Length, 1)
		if best == nil || v > bestV {
			best, bestV = dst, v
		}
	}
	if best == nil {
		return false
	}
	h.Mission = traffic.Courier
	u.Journal.Logf(u.Day, h.ID, "%s carries %.0ft %s on to %s", h.Name, h.Cargo[m], m, best.Name)
	u.Fleet.Depart(h, h.Home, best.Stellar, traffic.Hauling, u.Day)
	return true
}
