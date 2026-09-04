package universe

import (
	"fmt"
	"math"
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
	var out []Route
	for _, fromID := range u.order {
		src := u.Worlds[fromID]
		if !u.canTrade(c, src.Govt) {
			continue
		}
		for _, toID := range u.order {
			if toID == fromID {
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
				// copper, silicon, polymer, grain, steel — ride in-system
				// shuttles, and cross a jump only on a chartered lane. That
				// is OpenFront's port-to-port and factory-to-factory, in a
				// map whose roads are jump links.
				if m.Refined() && !u.shuttleLink(src, dst) {
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
				lane := u.Fleet.Lane(fromID, toID)
				out = append(out, Route{
					Mat: m, From: fromID, To: toID,
					Buy: buy, Sell: sell, Margin: sell - buy,
					Tons:   math.Min(spare, need),
					Length: lane.Length,
				})
			}
		}
	}
	weight := func(r Route) float64 {
		// Margin per megametre: a fat spread across the galaxy is worth less
		// than a decent one next door, because the hull could have run the
		// short one three times. Then OpenFront's two biases: twice the
		// weight for a port next door, twice for an ally — a near ally is
		// four times as likely to get the parcel as a distant stranger.
		v := r.Value() / math.Max(r.Length, 1)
		src, dst := u.Worlds[r.From], u.Worlds[r.To]
		if src.System == dst.System {
			v *= nearBias
		}
		if dst.Govt != c && u.Relation(c, dst.Govt) == Ally {
			v *= allyBias
		}
		return v
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := weight(out[i]), weight(out[j])
		if a != b {
			return a > b
		}
		return out[i].Mat < out[j].Mat
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
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
		routes[c] = u.FindRoutes(c, 24)
	}

	for _, h := range u.Fleet.Hulls {
		switch h.Status {
		case traffic.Lost, traffic.Resident, traffic.Fighting:
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
}

// dispatch finds a hull a job and loads it. The cargo leaves the origin
// warehouse the moment it is assigned — it is on the pad, it is bought and
// paid for, and it is no longer the seller's.
func (u *Universe) dispatch(h *traffic.Hull, routes map[govt.Color][]Route) {
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
	// Nothing to lift here. A trader does not sit on its hands at an empty
	// port — it deadheads to where the cargo is. Flying empty costs a few
	// days and earns nothing, which is exactly the pressure that makes a
	// well-placed berth worth having.
	for _, r := range list {
		if r.From != h.Home && affordable(r) >= minLoad {
			u.Fleet.Depart(h, h.Home, r.From, traffic.Returning, u.Day)
			return
		}
	}
}

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
	if h.Mission == traffic.Flight {
		u.arriveFlight(h, dst)
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
			tons = math.Min(tons, float64((dst.Credits-paid)/price))
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
	budget := int(float64(h.Purse) * leaveShare)
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

// leaveShare is how much of the purse a crew on leave will spend.
const leaveShare = 0.3

// Lose destroys a hull. Its cargo is NOT scattered into the sink: it drops
// where the hull died — on the lane, or in orbit over its port — as a wreck
// field that persists, on the books, and the nearest hulls with room take
// what they can of it, nearest first. Whatever nobody can lift stays there.
// The census records the loss forever.
func (u *Universe) Lose(h *traffic.Hull, why string) { u.wreck(h, why) }
