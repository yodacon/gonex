// Package traffic is every hull in the universe, including the ones nobody
// is looking at.
//
// The census is FIXED. A universe is created with N hulls and that is how
// many there are; a hull is never spawned to fill a scene and never deleted
// when it leaves one. When the player is in the ConEx system, the forty
// hulls flying somewhere else do not stop existing and do not freeze — they
// are still crossing their lanes, still carrying the cargo they loaded,
// still due somewhere on a day you could look up. That is the difference
// between a universe and a set of levels.
//
// Off-sector hulls are stepped with a one-dimensional momentum integrator
// along the lane they are flying: mass, thrust, drag, velocity, distance.
// It is not the full flight model — that belongs to internal/world, and to
// the ships you can actually see — but it is real physics with real inertia,
// so a heavy hauler takes longer to get up to speed and longer to shed it,
// and a light courier does the same run faster because it is lighter, not
// because a table says couriers are fast.
//
// Everything that happens to a hull is written to the JOURNAL, which is the
// game's audit trail and its news feed at once. It is also how the mass
// conservation story stays legible: every ton that moves, moves in an entry
// somebody can read.
package traffic

import (
	"fmt"
	"math"
	"sort"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
)

// Status is what a hull is doing. A hull is in exactly one of these at any
// moment, and every transition is journalled.
type Status int

const (
	// Idle: parked at its berth with nothing to do.
	Idle Status = iota
	// Loading: on a pad, taking cargo aboard.
	Loading
	// Hauling: under way with cargo, outbound to a buyer.
	Hauling
	// Returning: under way empty or part-laden, heading home.
	Returning
	// Fighting: committed to an engagement, not making progress on a lane.
	Fighting
	// Resident: inside the sector the player is flying, and therefore owned
	// by internal/world rather than by this integrator. The registry still
	// knows the hull exists; it simply stops moving it.
	Resident
	// Lost: destroyed. The census counts it forever, because "how many hulls
	// has Red lost this war" is the question the whole economy is about.
	Lost

	StatusCount
)

func (s Status) String() string {
	switch s {
	case Loading:
		return "LOADING"
	case Hauling:
		return "HAULING"
	case Returning:
		return "RETURNING"
	case Fighting:
		return "FIGHTING"
	case Resident:
		return "IN SECTOR"
	case Lost:
		return "LOST"
	}
	return "IDLE"
}

// UnderWay reports the statuses that are actually crossing a lane.
func (s Status) UnderWay() bool { return s == Hauling || s == Returning }

// Hull is one ship in the universe.
type Hull struct {
	ID   int
	Name string
	Govt govt.Color

	Status Status
	Home   int // stellar it berths at
	From   int // lane origin
	To     int // lane destination

	// The physics. Distance is along the lane in megametres; the lane's
	// length lives in the Lane record, not here, so a hull does not have to
	// be told where it is going twice.
	Mass   float64 // tonnes, hull plus cargo
	Dry    float64 // tonnes, hull alone
	Thrust float64 // meganewtons
	V      float64 // Mm/day
	S      float64 // Mm travelled along the current lane

	// Cargo is what it is carrying. Mass follows it: a laden hull is heavier
	// and therefore slower, with no special case anywhere.
	Cargo econ.Stock

	// Purse is the pilot's own money. Loading debits it, delivery credits
	// it, and a pilot who cannot afford the parcel does not load — a
	// colour's courier fleet is a set of small businesses with a visible
	// net worth, not a conveyor belt. Registered with the credit ledger.
	Purse int

	// Mission is why the hull is on its lane. A courier is trading; a
	// flight is under a standing order — a garrison transfer if the far end
	// is friendly, an assault if it is not. The rules of arrival differ,
	// nothing else does: a flight is a hull like any other, with its
	// magazine in its cargo.
	Mission Mission

	// Ledger figures, for the journal and the trade report.
	Bought  int // credits spent on this run's cargo
	Voyages int
	Tons    float64 // lifetime tons delivered
}

// Mission is what a hull was sent to do.
type Mission int

const (
	// Courier: free trade, routed by what pays today.
	Courier Mission = iota
	// Convoy: a standing order to carry a named material A → B.
	Convoy
	// Flight: a standing order to move hulls A → B. Arriving at a hostile
	// world, it fights; at a friendly one, it berths as garrison.
	Flight
)

func (m Mission) String() string {
	switch m {
	case Convoy:
		return "convoy"
	case Flight:
		return "flight"
	}
	return "courier"
}

// Laden is the cargo tonnage aboard.
func (h *Hull) Laden() float64 { return h.Cargo.Total() }

// Wet is the hull's current mass: structure plus what it is carrying.
func (h *Hull) Wet() float64 { return h.Dry + h.Laden() }

// Capacity is the hold in tons. Half the dry mass, scaled by the colour's
// logistics — the one place that number is computed, so the dispatcher, the
// salvage scoop and the desk all agree on how much a hull can lift.
func (h *Hull) Capacity() float64 { return h.Dry / 2 * govt.HoldFactor(h.Govt) }

// Free is the hold space left.
func (h *Hull) Free() float64 { return math.Max(h.Capacity()-h.Laden(), 0) }

// Magazine is the rounds aboard, in tons. A flight's kill percentage is made
// of this — it is Konquest's "the attacker uses the kill percent of the
// planet it left", because this is what it loaded there.
func (h *Hull) Magazine() float64 { return h.Cargo[econ.Rounds] }

// Structure is the hull's own mass as a material vector: the pool the
// auditor counts it in. A hull is Hull tons that left a warehouse.
func (h *Hull) Structure() econ.Stock {
	var s econ.Stock
	if h.Status != Lost {
		s[econ.Hull] = h.Dry
	}
	return s
}

// Lane is a link between two stellars, with a length. Hulls fly along it.
type Lane struct {
	From, To int
	Length   float64 // megametres
}

// Journal is the append-only record of everything that happened. It is
// bounded — a universe running for a thousand days would otherwise grow
// without limit — but the counters it keeps are not, so the totals survive
// the entries scrolling away.
type Journal struct {
	entries []Entry
	max     int

	// Running totals, kept independently of the entry ring so that trimming
	// old news never changes an accounting answer.
	Delivered econ.Stock
	Mined     econ.Stock
	Made      econ.Stock
	Burned    econ.Stock
	Voyages   int
	LostHulls int
}

// Entry is one journalled event.
type Entry struct {
	Day  int
	Hull int
	Text string
}

func (e Entry) String() string { return fmt.Sprintf("d%-4d %s", e.Day, e.Text) }

// NewJournal opens a journal keeping the most recent `max` entries.
func NewJournal(max int) *Journal {
	if max < 16 {
		max = 16
	}
	return &Journal{max: max}
}

// Log records an event.
func (j *Journal) Logf(day, hull int, format string, args ...any) {
	j.entries = append(j.entries, Entry{Day: day, Hull: hull, Text: fmt.Sprintf(format, args...)})
	if len(j.entries) > j.max {
		j.entries = j.entries[len(j.entries)-j.max:]
	}
}

// Tail returns the last n entries, oldest first.
func (j *Journal) Tail(n int) []Entry {
	if n > len(j.entries) {
		n = len(j.entries)
	}
	return j.entries[len(j.entries)-n:]
}

// Len is how many entries are being kept.
func (j *Journal) Len() int { return len(j.entries) }

// --- Debris --------------------------------------------------------------

// Debris is what a dead hull leaves: its cargo, and its structure as Scrap,
// sitting where it died. It is a pile, not a pickup — it does not expire,
// it is on the books, and it belongs to whoever gets a hold under it. On a
// lane it sits at a distance S along the link; at a port (S < 0, Lane
// unset) it is in orbit, and idle hulls berthed there scoop it.
type Debris struct {
	From, To int     // the lane, if any
	At       int     // the stellar it is in orbit over, if not on a lane
	S        float64 // Mm along the lane; -1 in orbit
	Stock    econ.Stock
	Day      int // when it was made, for the journal
}

// InOrbit reports whether the pile sits at a port rather than on a lane.
func (d *Debris) InOrbit() bool { return d.S < 0 }

// --- The registry --------------------------------------------------------

// Registry is the universal ship census.
type Registry struct {
	Hulls   []*Hull
	Lanes   map[[2]int]*Lane
	Journal *Journal

	// Debris is every wreck field in the universe. Nothing removes an
	// entry except a hold lifting the last ton out of it.
	Debris []*Debris

	// Name renders a stellar ID as the port's name for the journal. The
	// registry knows hulls and lanes, not the gazetteer, so whoever owns the
	// map supplies this. Without it the journal reads "1870137 → 1370191",
	// which is a log a machine can follow and a person cannot.
	Name func(stellar int) string
}

// port renders a stellar for the journal, falling back to its number.
func (r *Registry) port(id int) string {
	if r.Name != nil {
		if n := r.Name(id); n != "" {
			return n
		}
	}
	return fmt.Sprintf("#%d", id)
}

// NewRegistry opens an empty census.
func NewRegistry(journal *Journal) *Registry {
	return &Registry{Lanes: map[[2]int]*Lane{}, Journal: journal}
}

// Add enrols a hull. The census only ever grows at creation; after that a
// destroyed hull goes to Lost and stays counted.
func (r *Registry) Add(h *Hull) { r.Hulls = append(r.Hulls, h) }

// Lane returns the link between two stellars, creating it at a default
// length if the caller has not registered one.
func (r *Registry) Lane(from, to int) *Lane {
	key := [2]int{from, to}
	if l, ok := r.Lanes[key]; ok {
		return l
	}
	rev := [2]int{to, from}
	if l, ok := r.Lanes[rev]; ok {
		return l
	}
	l := &Lane{From: from, To: to, Length: defaultLane}
	r.Lanes[key] = l
	return l
}

// SetLane registers a link's true length.
func (r *Registry) SetLane(from, to int, length float64) {
	if length <= 0 {
		length = defaultLane
	}
	r.Lanes[[2]int{from, to}] = &Lane{From: from, To: to, Length: length}
}

// Census counts hulls by status. The sum is invariant for the life of the
// universe, which is the property the tests hold it to.
func (r *Registry) Census() [StatusCount]int {
	var out [StatusCount]int
	for _, h := range r.Hulls {
		out[h.Status]++
	}
	return out
}

// Afloat is every hull that has not been destroyed.
func (r *Registry) Afloat() int {
	n := 0
	for _, h := range r.Hulls {
		if h.Status != Lost {
			n++
		}
	}
	return n
}

// CargoAfloat is every ton currently sitting in a hold, anywhere in the
// universe. The auditor needs it, because cargo in transit is mass that has
// left a warehouse and not yet arrived at one.
func (r *Registry) CargoAfloat() econ.Stock {
	var s econ.Stock
	for _, h := range r.Hulls {
		if h.Status != Lost {
			s = s.Plus(h.Cargo)
		}
	}
	return s
}

// Structure is the dry tonnage of every surviving hull, as Hull material.
// Fleet mass is on the books: build a hull and a warehouse gets lighter by
// exactly what the ship weighs; lose one and the tons are Scrap in a field.
func (r *Registry) Structure() econ.Stock {
	var s econ.Stock
	for _, h := range r.Hulls {
		s = s.Plus(h.Structure())
	}
	return s
}

// DebrisAfloat is every ton sitting in a wreck field anywhere.
func (r *Registry) DebrisAfloat() econ.Stock {
	var s econ.Stock
	for _, d := range r.Debris {
		s = s.Plus(d.Stock)
	}
	return s
}

// Drop makes a wreck field from a pool, where the hull was. It merges into a
// field already at that spot rather than making a second, so a battle over
// one port leaves one pile.
func (r *Registry) Drop(h *Hull, pool *econ.Stock, day int) *Debris {
	var d *Debris
	if h.Status.UnderWay() {
		d = r.debrisAt(h.From, h.To, h.S)
		if d == nil {
			d = &Debris{From: h.From, To: h.To, S: h.S, Day: day}
			r.Debris = append(r.Debris, d)
		}
	} else {
		d = r.debrisAt(0, 0, -1)
		for _, o := range r.Debris {
			if o.InOrbit() && o.At == h.Home {
				d = o
				break
			}
		}
		if d == nil {
			d = &Debris{At: h.Home, S: -1, Day: day}
			r.Debris = append(r.Debris, d)
		}
	}
	for m := econ.Material(0); m < econ.Count; m++ {
		if pool[m] > 0 {
			econ.Transfer(pool, &d.Stock, m, pool[m])
		}
	}
	return d
}

func (r *Registry) debrisAt(from, to int, s float64) *Debris {
	if s < 0 {
		return nil
	}
	for _, d := range r.Debris {
		if d.InOrbit() {
			continue
		}
		same := (d.From == from && d.To == to) || (d.From == to && d.To == from)
		if same && math.Abs(d.S-s) < scoopReach {
			return d
		}
	}
	return nil
}

// Scoop lets a hull lift what it can from a field: every material, up to
// the hold space it has left. Returns the tons taken. The field is struck
// from the register once it is empty, and only then.
func (r *Registry) Scoop(h *Hull, d *Debris, day int) float64 {
	free := h.Free()
	if free <= 0 || d.Stock.Total() <= 0 {
		return 0
	}
	var got float64
	for m := econ.Material(0); m < econ.Count && free > 0; m++ {
		if d.Stock[m] <= 0 {
			continue
		}
		t := econ.Transfer(&d.Stock, &h.Cargo, m, math.Min(d.Stock[m], free))
		free -= t
		got += t
	}
	if got > 0 {
		h.Mass = h.Wet()
		r.Journal.Logf(day, h.ID, "%s scoops %.0ft of wreckage", h.Name, got)
	}
	r.sweep()
	return got
}

// Salvage runs the collection rule over a whole field: the NEAREST hull
// with hold space takes what it can, then the next nearest, until the field
// is empty or nobody nearby has room. Whatever is left stays where it is.
// Any colour may lift; a wreck is nobody's.
func (r *Registry) Salvage(d *Debris, day int) {
	type cand struct {
		h *Hull
		d float64
	}
	var near []cand
	for _, h := range r.Hulls {
		if h.Status == Lost || h.Status == Resident || h.Free() <= 0 {
			continue
		}
		switch {
		case d.InOrbit():
			if !h.Status.UnderWay() && h.Home == d.At {
				near = append(near, cand{h, 0})
			}
		case h.Status.UnderWay():
			same := (h.From == d.From && h.To == d.To)
			rev := (h.From == d.To && h.To == d.From)
			if !same && !rev {
				continue
			}
			pos := h.S
			if rev {
				pos = r.Lane(d.From, d.To).Length - h.S
			}
			if dist := math.Abs(pos - d.S); dist <= salvageRange {
				near = append(near, cand{h, dist})
			}
		}
	}
	sort.SliceStable(near, func(i, j int) bool {
		if near[i].d != near[j].d {
			return near[i].d < near[j].d
		}
		return near[i].h.ID < near[j].h.ID
	})
	for _, c := range near {
		if d.Stock.Total() <= 0 {
			break
		}
		r.Scoop(c.h, d, day)
	}
}

// sweep drops empty fields from the register.
func (r *Registry) sweep() {
	live := r.Debris[:0]
	for _, d := range r.Debris {
		if d.Stock.Total() > 1e-9 {
			live = append(live, d)
		}
	}
	r.Debris = live
}

// DebrisNear lists the fields a hull is passing or berthed beside.
func (r *Registry) DebrisNear(h *Hull) []*Debris {
	var out []*Debris
	for _, d := range r.Debris {
		if d.InOrbit() {
			if !h.Status.UnderWay() && h.Home == d.At {
				out = append(out, d)
			}
			continue
		}
		if !h.Status.UnderWay() {
			continue
		}
		same := (h.From == d.From && h.To == d.To)
		rev := (h.From == d.To && h.To == d.From)
		if !same && !rev {
			continue
		}
		pos := h.S
		if rev {
			pos = r.Lane(d.From, d.To).Length - h.S
		}
		if math.Abs(pos-d.S) <= scoopReach {
			out = append(out, d)
		}
	}
	return out
}

// ByGovt lists a colour's surviving hulls.
func (r *Registry) ByGovt(c govt.Color) []*Hull {
	var out []*Hull
	for _, h := range r.Hulls {
		if h.Govt == c && h.Status != Lost {
			out = append(out, h)
		}
	}
	return out
}

// --- The physics ---------------------------------------------------------

const (
	// defaultLane is how far apart two stellars are if nobody said.
	defaultLane = 260.0 // megametres

	// drag is not air resistance — there is none. It is the standing-in
	// number for everything that keeps a hull from accelerating forever:
	// reaction mass budget, cruise limits, traffic control. Proportional to
	// v², so a hull settles at a terminal cruise instead of running away.
	//
	// Scaled against Thrust so that a typical 600 t hull reaches a cruise of
	// roughly 45 Mm/day in about nine days and crosses a standard lane in
	// ten to twelve. Get this wrong in the other direction and the whole
	// universe is becalmed: the first cut had hulls taking eighty-eight days
	// to make a crossing, which is a trade network that never trades.
	dragK = 1.5

	// arrival is how close to the far end counts as arrived.
	arrival = 0.5

	// scoopReach is how close a passing hull must be to a wreck field to
	// take from it — about a day's cruise, since the integrator steps a
	// day at a time and a field must not be skipped over between steps.
	scoopReach = 48.0

	// salvageRange is how far along a lane the collection rule looks when a
	// hull dies: the nearest hulls with room take the cargo, then the next.
	salvageRange = 400.0
)

// Step advances one hull along its lane by dt days, integrating momentum.
//
// F = thrust - drag(v);  a = F/m;  v += a·dt;  s += v·dt
//
// Mass is the hull plus its cargo, so a full hauler genuinely accelerates
// worse than an empty one and the return leg is genuinely faster. That is
// the entire reason this is a physics step and not a countdown timer: the
// economics of a route fall out of the mass it carries.
func (r *Registry) Step(h *Hull, dt float64) (arrived bool) {
	if !h.Status.UnderWay() || dt <= 0 {
		return false
	}
	lane := r.Lane(h.From, h.To)
	m := math.Max(h.Wet(), 1)
	cruise := govt.CruiseFactor(h.Govt)

	f := h.Thrust*cruise - dragK*h.V*math.Abs(h.V)
	h.V += (f / m) * dt
	if h.V < 0 {
		h.V = 0
	}
	h.S += h.V * dt

	// A hull passing a wreck field with room in the hold takes what it can.
	// Any colour: a wreck is nobody's.
	if h.Free() > 0 {
		for _, d := range r.DebrisNear(h) {
			r.Scoop(h, d, -1)
		}
	}

	if h.S >= lane.Length-arrival {
		h.S = lane.Length
		return true
	}
	return false
}

// Depart puts a hull on a lane, at rest at the near end.
func (r *Registry) Depart(h *Hull, from, to int, st Status, day int) {
	h.From, h.To, h.Status = from, to, st
	h.S, h.V = 0, 0
	h.Mass = h.Wet()
	verb := "departs"
	if st == Returning {
		verb = "turns for home"
	}
	if laden := h.Laden(); laden > 0 {
		r.Journal.Logf(day, h.ID, "%s %s %s → %s, %.0ft aboard",
			h.Name, verb, r.port(from), r.port(to), laden)
	} else {
		r.Journal.Logf(day, h.ID, "%s %s %s → %s, empty",
			h.Name, verb, r.port(from), r.port(to))
	}
}

// ETA is how many days a hull still has to fly, at its present speed and
// mass. It is an estimate on purpose: it does not re-integrate, it reads the
// gauge, exactly like a real one would.
func (r *Registry) ETA(h *Hull) float64 {
	if !h.Status.UnderWay() {
		return 0
	}
	lane := r.Lane(h.From, h.To)
	left := lane.Length - h.S
	if left <= 0 {
		return 0
	}
	// A hull still spooling up is not going to cover the rest of the lane at
	// its current speed, so reading the speedo and dividing would be wrong by
	// a factor of several. Solve the kinematics instead, at the acceleration
	// it has RIGHT NOW:
	//
	//	left = v·t + ½·a·t²   ⇒   t = (√(v² + 2·a·left) − v) / a
	//
	// which degrades gracefully to left/v when the hull is already at cruise
	// and a has fallen to nothing.
	m := math.Max(h.Wet(), 1)
	a := (h.Thrust*govt.CruiseFactor(h.Govt) - dragK*h.V*h.V) / m
	if a <= 1e-6 {
		return left / math.Max(h.V, 0.01)
	}
	return (math.Sqrt(h.V*h.V+2*a*left) - h.V) / a
}

// Manifest renders one hull as a line of the trade journal — what it is,
// what it is doing, what it is carrying and when it is due.
func (h *Hull) Manifest(eta float64) string {
	switch {
	case h.Status == Lost:
		return fmt.Sprintf("%-12s %-5s LOST", h.Name, h.Govt)
	case h.Status.UnderWay():
		return fmt.Sprintf("%-12s %-5s %-9s %d→%d  %5.0ft  eta %4.1fd  %5.1f Mm/d  %s",
			h.Name, h.Govt, h.Status, h.From, h.To, h.Laden(), eta, h.V, h.Mission)
	default:
		return fmt.Sprintf("%-12s %-5s %-9s at %d   %5.0ft",
			h.Name, h.Govt, h.Status, h.Home, h.Laden())
	}
}

// Report renders the whole census as a short block, busiest colour first.
func (r *Registry) Report() []string {
	out := []string{fmt.Sprintf("%d hulls in the universe, %d afloat, %.0ft in transit, %.0ft in %d wreck fields",
		len(r.Hulls), r.Afloat(), r.CargoAfloat().Total(), r.DebrisAfloat().Total(), len(r.Debris))}
	c := r.Census()
	out = append(out, fmt.Sprintf("  idle %d · loading %d · hauling %d · returning %d · fighting %d · in sector %d · lost %d",
		c[Idle], c[Loading], c[Hauling], c[Returning], c[Fighting], c[Resident], c[Lost]))
	for _, g := range govt.Colors() {
		hulls := r.ByGovt(g)
		var tons float64
		for _, h := range hulls {
			tons += h.Tons
		}
		out = append(out, fmt.Sprintf("  %-5s %2d hulls, %.0ft delivered lifetime", g, len(hulls), tons))
	}
	return out
}

// Busiest lists hulls with the most lifetime tonnage first — the universe's
// merchant princes, which is a thing a trade journal ought to be able to say.
func (r *Registry) Busiest(n int) []*Hull {
	out := append([]*Hull(nil), r.Hulls...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tons > out[j].Tons })
	if n < len(out) {
		out = out[:n]
	}
	return out
}
