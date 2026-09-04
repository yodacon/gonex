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
	sort.SliceStable(out, func(i, j int) bool {
		// Margin per megametre: a fat spread across the galaxy is worth less
		// than a decent one next door, because the hull could have run the
		// short one three times.
		a := out[i].Value() / math.Max(out[i].Length, 1)
		b := out[j].Value() / math.Max(out[j].Length, 1)
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
// colour p. Neutral ports serve anybody; the three colours serve only
// themselves.
func (u *Universe) canTrade(c, p govt.Color) bool { return p == govt.None || p == c }

const (
	// reserveDays is how much of its own demand a port keeps back rather
	// than selling — nobody sells the week's flour.
	reserveDays = 8.0
	// minLoad is the smallest parcel worth sending a ship for.
	minLoad = 25.0
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
	for i, r := range list {
		if r.From != h.Home {
			continue
		}
		src := u.Worlds[r.From]
		capacity := h.Dry / 2 * govt.HoldFactor(h.Govt)
		tons := math.Min(math.Min(r.Tons, capacity), src.Warehouse[r.Mat])
		if tons < minLoad {
			continue
		}
		// Buy it: mass out of the warehouse and into the hold, credits the
		// other way. Transfer is the only mover, so this cannot mint tons.
		got := econ.Transfer(&src.Warehouse, &h.Cargo, r.Mat, tons)
		if got < minLoad {
			econ.Transfer(&h.Cargo, &src.Warehouse, r.Mat, got) // put it back
			continue
		}
		cost := int(got) * r.Buy
		src.Credits += cost
		h.Bought = cost
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
		if r.From != h.Home && r.Tons >= minLoad {
			u.Fleet.Depart(h, h.Home, r.From, traffic.Returning, u.Day)
			return
		}
	}
}

// arrive unloads a hull that has reached the far end of its lane.
func (u *Universe) arrive(h *traffic.Hull) {
	dst := u.Worlds[h.To]
	if dst == nil {
		h.Status, h.Home = traffic.Idle, h.To
		return
	}
	var sold float64
	var paid int
	for m := econ.Material(0); m < econ.Count; m++ {
		if h.Cargo[m] <= 0 {
			continue
		}
		tons := econ.Transfer(&h.Cargo, &dst.Warehouse, m, h.Cargo[m])
		if tons <= 0 {
			continue
		}
		price := dst.Shop[m]
		paid += int(tons) * price
		sold += tons
		u.Journal.Delivered.Add(m, tons)
	}
	if sold > 0 {
		dst.Credits -= paid
		h.Tons += sold
		h.Voyages++
		u.Journal.Voyages++
		u.Journal.Logf(u.Day, h.ID, "%s delivers %.0ft at %s for %d cr (margin %d)",
			h.Name, sold, dst.Name, paid, paid-h.Bought)
		h.Bought = 0
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
	h.V, h.S = 0, 0
	h.Mass = h.Wet()
	dst.Reprice()
}

// Lose destroys a hull: its cargo is scattered into the sink (mass conserved,
// value gone) and the census records it forever.
func (u *Universe) Lose(h *traffic.Hull, why string) {
	if h.Status == traffic.Lost {
		return
	}
	for m := econ.Material(0); m < econ.Count; m++ {
		if h.Cargo[m] > 0 {
			econ.Consume(&h.Cargo, &u.Sink, m, h.Cargo[m])
		}
	}
	h.Status = traffic.Lost
	u.Journal.LostHulls++
	u.Journal.Logf(u.Day, h.ID, "%s lost — %s", h.Name, why)
}
