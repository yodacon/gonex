package universe

import (
	"fmt"
	"math"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// Standing orders: Konquest's "retry send" and OpenFront's train, one
// mechanism.
//
// Konquest lets a player set N ships to leave planet A for planet B every
// turn until told otherwise. OpenFront spawns a train from a factory to a
// random connected city and pays at every stop. Both are the same idea — a
// scheduled flow between two named places that runs without further
// attention — and here they are one type: N hulls a day (a garrison
// transfer, or an assault if the far end is hostile) or N tons a day of a
// named material (a convoy). An order lives on the world that gave it, runs
// before free routing every tick, and dies with the government that gave it
// when the world changes hands — exactly as Planet::conquer deletes standing
// orders in Konquest.

// StandingOrder is one scheduled flow.
type StandingOrder struct {
	From, To int // stellar IDs
	Owner    govt.Color

	// Hulls per day, or…
	Hulls int
	// …tons per day of Mat.
	Mat  econ.Material
	Tons float64
}

func (o StandingOrder) String() string {
	if o.Hulls > 0 {
		return fmt.Sprintf("%d hull/day  #%d → #%d", o.Hulls, o.From, o.To)
	}
	return fmt.Sprintf("%.0ft %s/day  #%d → #%d", o.Tons, o.Mat, o.From, o.To)
}

// File validates and files a standing order on its origin world.
//
// The checks are the same-type rule and the relation table: a convoy of
// intermediates needs a shuttle link; any convoy needs a port that will
// trade with the owner; a flight to a friendly world is a transfer and to
// a hostile or neutral one an assault; a flight to a colour at peace is
// refused, because peace means exactly that.
func (u *Universe) File(o StandingOrder) error {
	src, dst := u.Worlds[o.From], u.Worlds[o.To]
	if src == nil || dst == nil {
		return fmt.Errorf("no such port")
	}
	if src == dst {
		return fmt.Errorf("an order needs two different ports")
	}
	if src.Govt != o.Owner {
		return fmt.Errorf("%s is not held by %s", src.Name, o.Owner)
	}
	switch {
	case o.Hulls > 0:
		if u.Relation(o.Owner, dst.Govt) == Peace && dst.Govt != govt.None {
			return fmt.Errorf("%s is at peace with %s; a flight there is refused", o.Owner, dst.Govt)
		}
	case o.Tons > 0:
		if !u.canTrade(o.Owner, dst.Govt) {
			return fmt.Errorf("%s will not trade with %s", dst.Name, o.Owner)
		}
		if shuttleOnly(o.Mat) && !u.shuttleLink(src, dst) {
			return fmt.Errorf("%s moves by shuttle: %s and %s need a chartered lane", o.Mat, src.Name, dst.Name)
		}
		if o.Mat < 0 || o.Mat >= econ.Slag || o.Mat == econ.Compost {
			return fmt.Errorf("%s cannot be shipped", o.Mat)
		}
	default:
		return fmt.Errorf("an order moves hulls or tons")
	}
	// One order per (from, to, kind): filing again replaces it.
	kept := src.Orders[:0]
	for _, x := range src.Orders {
		if x.To == o.To && (x.Hulls > 0) == (o.Hulls > 0) && x.Mat == o.Mat {
			continue
		}
		kept = append(kept, x)
	}
	src.Orders = append(kept, o)
	u.Journal.Logf(u.Day, -1, "%s: standing order — %s", src.Name, u.DescribeOrder(o))
	return nil
}

// Cancel strikes an order.
func (u *Universe) Cancel(from int, i int) {
	w := u.Worlds[from]
	if w == nil || i < 0 || i >= len(w.Orders) {
		return
	}
	u.Journal.Logf(u.Day, -1, "%s: order cancelled — %s", w.Name, u.DescribeOrder(w.Orders[i]))
	w.Orders = append(w.Orders[:i], w.Orders[i+1:]...)
}

// DescribeOrder renders an order with port names.
func (u *Universe) DescribeOrder(o StandingOrder) string {
	name := func(id int) string {
		if w := u.Worlds[id]; w != nil {
			return w.Name
		}
		return fmt.Sprintf("#%d", id)
	}
	if o.Hulls > 0 {
		return fmt.Sprintf("%d hull/day %s → %s", o.Hulls, name(o.From), name(o.To))
	}
	return fmt.Sprintf("%.0ft %s/day %s → %s", o.Tons, o.Mat, name(o.From), name(o.To))
}

// runOrders executes every world's standing orders for the day, before free
// routing gets the idle hulls. It is deterministic in world order and order
// order, like everything else in the tick.
func (u *Universe) runOrders() {
	for _, id := range u.order {
		w := u.Worlds[id]
		for _, o := range w.Orders {
			if w.Govt != o.Owner {
				continue // the world changed hands; the order is dead, swept below
			}
			if o.Hulls > 0 {
				u.sendFlight(w, o)
			} else {
				u.sendConvoy(w, o)
			}
		}
		// Sweep orders whose government is gone.
		kept := w.Orders[:0]
		for _, o := range w.Orders {
			if w.Govt == o.Owner {
				kept = append(kept, o)
			}
		}
		w.Orders = kept
	}
}

// idleAt lists a colour's hulls berthed at a world with nothing to do.
func (u *Universe) idleAt(w *World, c govt.Color) []*traffic.Hull {
	var out []*traffic.Hull
	for _, h := range u.Fleet.Hulls {
		if h.Govt == c && h.Home == w.Stellar && h.Status == traffic.Idle {
			out = append(out, h)
		}
	}
	return out
}

// sendFlight moves up to N idle hulls down the lane, leaving one behind —
// Konquest's rule that a planet is held by the ship that stays. Each hull
// loads a magazine from the world's arsenal on the way out: the attacker's
// kill percentage is the one it LEFT with.
func (u *Universe) sendFlight(w *World, o StandingOrder) {
	dst := u.Worlds[o.To]
	if dst == nil {
		return
	}
	idle := u.idleAt(w, o.Owner)
	n := o.Hulls
	if len(idle)-n < 1 {
		n = len(idle) - 1
	}
	for i := 0; i < n; i++ {
		h := idle[i]
		u.arm(w, h)
		h.Mission = traffic.Flight
		u.Fleet.Depart(h, w.Stellar, dst.Stellar, traffic.Hauling, u.Day)
	}
}

// arm loads a hull's magazine from the world it is leaving: rounds first,
// missiles if there are any. Tons move; the world's own defence is exactly
// that much thinner for it.
func (u *Universe) arm(w *World, h *traffic.Hull) {
	want := math.Min(h.Free()*magazineShare, magazineTons)
	if want > h.Magazine() {
		econ.Transfer(&w.Warehouse, &h.Cargo, econ.Rounds, want-h.Magazine())
	}
	if w.Warehouse[econ.Missiles] > 0 && h.Cargo[econ.Missiles] < missileTons {
		econ.Transfer(&w.Warehouse, &h.Cargo, econ.Missiles, missileTons-h.Cargo[econ.Missiles])
	}
	h.Mass = h.Wet()
}

const (
	magazineShare = 0.25 // a flight gives a quarter of its hold to shot
	magazineTons  = 4.0  // and never more than this per hull
	missileTons   = 1.0
)

// sendConvoy loads one idle hull with the ordered material, bought at the
// origin's price out of the pilot's purse, and sends it. It is the
// dispatcher with the destination and cargo fixed.
func (u *Universe) sendConvoy(w *World, o StandingOrder) {
	dst := u.Worlds[o.To]
	if dst == nil || !u.canTrade(o.Owner, dst.Govt) {
		return
	}
	idle := u.idleAt(w, o.Owner)
	if len(idle) == 0 {
		return
	}
	h := idle[0]
	price := w.Shop[o.Mat]
	if price <= 0 {
		price = 1
	}
	tons := math.Min(math.Min(o.Tons, h.Free()), w.Warehouse[o.Mat])
	tons = math.Min(tons, float64(h.Purse/price))
	if tons < minLoad {
		return
	}
	got := econ.Transfer(&w.Warehouse, &h.Cargo, o.Mat, tons)
	cost := int(got) * price
	econ.Pay(&h.Purse, &w.Credits, cost)
	h.Bought = cost
	h.Mission = traffic.Convoy
	h.Mass = h.Wet()
	u.Journal.Logf(u.Day, h.ID, "%s loads %.0ft %s at %s under standing order for %s",
		h.Name, got, o.Mat, w.Name, dst.Name)
	u.Fleet.Depart(h, w.Stellar, dst.Stellar, traffic.Hauling, u.Day)
}
