package universe

import (
	"fmt"
	"math"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/industry"
	"yodacon.org/gonex/internal/traffic"
)

// Buildings: what a governor buys at a world, and what it does in an economy
// where nothing is minted.
//
// The shape is OpenFront's — same-type connectivity, one shared cost ladder,
// a first building that changes who the world answers to — and the
// accounting is ours: every purchase is a Pay into the world's treasury, and
// an upgrade buys a CLAIM ON FLOW, never a dividend. A Spaceport you levelled
// has more couriers basing there, so more sales through its board, so a
// bigger treasury paying better prices on what you bring in. That is the
// only return a zero-sum game can honestly offer.

// Building is one thing a governor can stand up at a world.
type Building int

const (
	// Spaceport: berths, and where the colour's couriers choose to base.
	// Talks to other Spaceports by interstellar courier.
	Spaceport Building = iota
	// Works: one more industrial chain from industry.Rank. Talks to Works,
	// Habitats and Spaceports by in-system shuttle — the only way copper
	// moves between two moons.
	Works
	// Habitat: housing, and a deeper luxury market.
	Habitat
	// Exchange: the producer keeps more of the price; the board is open to
	// allies.
	Exchange
	// Lane: a chartered shuttle-grade link across one jump, so Works in two
	// adjacent systems trade intermediates.
	Lane
	// Bastion: planetary batteries — the defence rating's multiplier.
	Bastion
	// Picket: point defence against an attacker's missiles.
	Picket
	// Silo: a planetary missile battery, fed from the Missiles stock.
	Silo

	BuildingCount
)

var buildingNames = [BuildingCount]string{
	Spaceport: "Spaceport", Works: "Works", Habitat: "Habitat", Exchange: "Exchange",
	Lane: "Lane", Bastion: "Bastion", Picket: "Picket", Silo: "Silo",
}

func (b Building) String() string {
	if b < 0 || b >= BuildingCount {
		return "?"
	}
	return buildingNames[b]
}

// ParseBuilding reads a building name, case-insensitively, for the console.
func ParseBuilding(s string) (Building, bool) {
	for b := Building(0); b < BuildingCount; b++ {
		if equalFold(buildingNames[b], s) {
			return b, true
		}
	}
	return 0, false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// Military reports the three buildings that connect to nothing and fight.
func (b Building) Military() bool { return b == Bastion || b == Picket || b == Silo }

// The ladders. Spaceport and Works share one counter — buy a Spaceport
// first and the first Works costs the second rung — and Habitat and Exchange
// share another. OpenFront's rungs are 125k / 250k / 500k / 1M; those are
// treasury-scale numbers here (ConEx opens with about a million credits), so
// the ladder is one tenth of that, against a player who starts on 8,000.
const (
	ladderBase = 12_500
	ladderCap  = 100_000
	lanePerHop = 5_000
	militaryCr = 20_000
	// militarySteel is the tonnage a Bastion, Picket or Silo draws from the
	// warehouse when built: a battery is tons, and it comes out of the
	// steel the world would otherwise have sold.
	militarySteel = 200.0
	maxLevel      = 3
)

// ladder is the shared counter's price for the n-th purchase (0-based).
func ladder(n int) int {
	p := ladderBase
	for i := 0; i < n && p < ladderCap; i++ {
		p *= 2
	}
	if p > ladderCap {
		p = ladderCap
	}
	return p
}

// Price is what the next level of a building costs at this world.
func (w *World) Price(b Building) int {
	switch b {
	case Spaceport, Works:
		return ladder(w.Built[Spaceport] + w.Built[Works])
	case Habitat, Exchange:
		return ladder(w.Built[Habitat] + w.Built[Exchange])
	case Lane:
		return lanePerHop
	default:
		return militaryCr
	}
}

// CanBuild reports whether another level can be stood up, and why not.
func (w *World) CanBuild(b Building) error {
	if b < 0 || b >= BuildingCount {
		return fmt.Errorf("no such building")
	}
	if w.Built[b] >= maxLevel {
		return fmt.Errorf("%s is already at level %d", b, maxLevel)
	}
	if b.Military() && w.Warehouse[econ.Steel] < militarySteel {
		return fmt.Errorf("a %s needs %.0f t of steel on hand; %s has %.0f",
			b, militarySteel, w.Name, w.Warehouse[econ.Steel])
	}
	if b == Works && len(w.Plant) >= len(industry.Rank(w.Reserve)) {
		return fmt.Errorf("%s has no further chain its crust can back", w.Name)
	}
	return nil
}

// Build stands up the next level of b at w, paid from `purse` into the
// world's treasury. The payer is whoever holds the seat's money: the player
// or the colour's exchequer. `seat` says who is buying; the first building
// at a world is its charter, and the seat follows it.
//
// Every effect is a change to a number the simulation already reads:
// Works re-stands industry with one more slot; Habitat raises Housing and
// the luxury exponent; Exchange narrows the producer discount; Lane charters
// a shuttle link; the military three raise Rating and draw steel. Nothing
// here has a second dial.
func (u *Universe) Build(w *World, b Building, purse *int, seat Seat) error {
	if err := w.CanBuild(b); err != nil {
		return err
	}
	price := w.Price(b)
	if purse == nil || *purse < price {
		have := 0
		if purse != nil {
			have = *purse
		}
		return fmt.Errorf("%s costs %d cr; %d on hand", b, price, have)
	}
	if b == Lane {
		return fmt.Errorf("a lane is chartered between two systems; use Charter")
	}
	econ.Pay(purse, &w.Credits, price)
	if b.Military() {
		econ.Consume(&w.Warehouse, &u.Sink, econ.Steel, militarySteel)
	}
	first := w.buildingCount() == 0
	w.Built[b]++
	switch b {
	case Works:
		w.standUpIndustry()
	}
	if first && seat == SeatPlayer {
		w.Seat = SeatPlayer
	}
	w.Reprice()
	u.Journal.Logf(u.Day, -1, "%s: %s level %d stood up for %d cr", w.Name, b, w.Built[b], price)
	return nil
}

// Charter buys a shuttle-grade lane between two systems, so Works on either
// side trade intermediates. It is the "territory expansion" that connects
// the roads, in a game where the roads are jump links.
func (u *Universe) Charter(from, to *World, purse *int, seat Seat) error {
	if from == nil || to == nil || from.System == to.System {
		return fmt.Errorf("a lane joins two different systems")
	}
	if u.Chartered(from.System, to.System) {
		return fmt.Errorf("%s and %s are already joined", from.Name, to.Name)
	}
	price := lanePerHop
	if purse == nil || *purse < price {
		return fmt.Errorf("a lane costs %d cr", price)
	}
	econ.Pay(purse, &from.Credits, price)
	u.Charters[systemPair(from.System, to.System)] = true
	if from.buildingCount() == 0 && seat == SeatPlayer {
		from.Seat = SeatPlayer
	}
	from.Built[Lane]++
	u.Journal.Logf(u.Day, -1, "%s - %s: a lane is chartered for %d cr", from.Name, to.Name, price)
	return nil
}

// Chartered reports whether two systems are joined by a bought lane.
func (u *Universe) Chartered(sysA, sysB int) bool {
	return u.Charters[systemPair(sysA, sysB)]
}

func systemPair(a, b int) [2]int {
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}
}

// shuttleLink reports whether intermediates can move between two worlds:
// the same system, or a chartered lane between their systems. This is
// OpenFront's rail range, in a map where distance is the jump graph.
func (u *Universe) shuttleLink(a, b *World) bool {
	return a.System == b.System || u.Chartered(a.System, b.System)
}

func (w *World) buildingCount() int {
	n := 0
	for _, l := range w.Built {
		n += l
	}
	return n
}

// Berths is how many hulls can turn around here at once. A Spaceport level
// adds one, which is the throughput a port sells.
func (w *World) Berths() int {
	return 1 + w.Pop/2_000_000 + w.Built[Spaceport]
}

// --- The defence rating --------------------------------------------------

// Rating is Konquest's kill percentage, made honest.
//
// Konquest rolls 0.30–0.90 per planet and never changes it. Ours is a
// function of things the cycle already produces: the colour's gunnery, the
// Bastion standing here, and — the term that matters — how many days of
// rounds the magazine holds against what the garrison burns. A fortress with
// an empty magazine rates 0.00 and is meant to. It is computed the same way
// for every world, friend or foe, from one function, so what the desk shows
// is what the battle uses.
func (u *Universe) Rating(w *World) float64 {
	if w == nil || w.Pop <= 0 {
		return 0
	}
	garrison := len(u.Garrison(w))
	burn := w.appetite(econ.Rounds) + roundsPerHullDay*float64(garrison)
	if burn <= 0 {
		burn = roundsPerHullDay
	}
	cover := w.Warehouse[econ.Rounds] / burn
	r := baseRating * govt.GunFactor(w.Govt) *
		(1 + bastionStep*float64(w.Built[Bastion])) *
		math.Min(1, cover/ratingCoverDays)
	if w.Built[Silo] > 0 && w.Warehouse[econ.Missiles] > 0 {
		r += siloBonus
	}
	return math.Min(r, maxRating)
}

const (
	baseRating       = 0.55
	bastionStep      = 0.35
	ratingCoverDays  = 3.0
	siloBonus        = 0.10
	maxRating        = 0.95
	roundsPerHullDay = 0.25 // tons of Rounds a berthed hull's crew fires in drills per day
)

// Garrison is the armed hulls of the holding colour berthed at w: what
// stands between an arriving flight and the pad. Konquest's "one ship must
// remain" is a zero here with an enemy arrow inbound.
func (u *Universe) Garrison(w *World) []*traffic.Hull {
	var out []*traffic.Hull
	if w.Govt == govt.None {
		return out
	}
	for _, h := range u.Fleet.Hulls {
		if h.Govt == w.Govt && h.Home == w.Stellar && !h.Status.UnderWay() &&
			h.Status != traffic.Lost && h.Status != traffic.Resident {
			out = append(out, h)
		}
	}
	return out
}

// Capital is a colour's principal world: the most populous one it holds.
func (u *Universe) Capital(c govt.Color) *World {
	var best *World
	for _, id := range u.order {
		w := u.Worlds[id]
		if w.Govt != c {
			continue
		}
		if best == nil || w.Pop > best.Pop {
			best = w
		}
	}
	return best
}
