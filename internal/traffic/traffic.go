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

	// Ledger figures, for the journal and the trade report.
	Bought  int // credits spent on this run's cargo
	Voyages int
	Tons    float64 // lifetime tons delivered
}

// Laden is the cargo tonnage aboard.
func (h *Hull) Laden() float64 { return h.Cargo.Total() }

// Wet is the hull's current mass: structure plus what it is carrying.
func (h *Hull) Wet() float64 { return h.Dry + h.Laden() }

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

// --- The registry --------------------------------------------------------

// Registry is the universal ship census.
type Registry struct {
	Hulls   []*Hull
	Lanes   map[[2]int]*Lane
	Journal *Journal
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
		r.Journal.Logf(day, h.ID, "%s %s %d→%d, %.0ft aboard", h.Name, verb, from, to, laden)
	} else {
		r.Journal.Logf(day, h.ID, "%s %s %d→%d, empty", h.Name, verb, from, to)
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
		return fmt.Sprintf("%-12s %-5s %-9s %d→%d  %5.0ft  eta %4.1fd  %5.1f Mm/d",
			h.Name, h.Govt, h.Status, h.From, h.To, h.Laden(), eta, h.V)
	default:
		return fmt.Sprintf("%-12s %-5s %-9s at %d   %5.0ft",
			h.Name, h.Govt, h.Status, h.Home, h.Laden())
	}
}

// Report renders the whole census as a short block, busiest colour first.
func (r *Registry) Report() []string {
	out := []string{fmt.Sprintf("%d hulls in the universe, %d afloat, %.0ft in transit",
		len(r.Hulls), r.Afloat(), r.CargoAfloat().Total())}
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
