package universe

import (
	"fmt"
	"math"
	"sort"

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
	bought := func(x Building) int { return w.Built[x] - w.Endowed[x] }
	switch b {
	case Spaceport, Works:
		return ladder(bought(Spaceport) + bought(Works))
	case Habitat, Exchange:
		return ladder(bought(Habitat) + bought(Exchange))
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

// buildingCount is how many levels have been BOUGHT here. Genesis
// infrastructure does not count: the charter is the first thing somebody
// paid for.
func (w *World) buildingCount() int {
	n := 0
	for b, l := range w.Built {
		n += l - w.Endowed[b]
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
	roundsPerHullDay = 0.08 // tons of Rounds a berthed hull's crew fires in drills per day
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

// --- Doctrine ------------------------------------------------------------

// buildingAxis is the trifecta axis each building serves. A colour's
// building DOCTRINE — what it stands up first when the exchequer can afford
// something — is the buildings sorted by the colour's trait on that axis.
// Nothing here is a dial: change the trifecta and the doctrines follow, and
// the balance proof in govt still holds.
var buildingAxis = [BuildingCount]govt.Axis{
	Spaceport: govt.Logistics, Lane: govt.Logistics,
	Works:    govt.Extraction,
	Exchange: govt.Industry,
	Habitat:  govt.Growth,
	Bastion:  govt.Shields, Picket: govt.Shields,
	Silo: govt.Gunnery,
}

// Doctrine is the order a colour builds in. Red the raider leads with silos
// and spaceports, Green the grower with habitats and works, Blue the
// fortress with exchanges and bastions — read straight off the table.
func Doctrine(c govt.Color) []Building {
	out := make([]Building, 0, BuildingCount)
	for b := Building(0); b < BuildingCount; b++ {
		if b == Lane {
			continue // lanes are chartered between two worlds, not built at one
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := govt.Trait(c, buildingAxis[out[i]]), govt.Trait(c, buildingAxis[out[j]])
		if ti != tj {
			return ti > tj
		}
		return out[i] < out[j]
	})
	return out
}

// DoctrineName is the colour's building style, in a phrase.
func DoctrineName(c govt.Color) string {
	d := Doctrine(c)
	if len(d) < 2 {
		return "no doctrine"
	}
	switch c {
	case govt.Red:
		return fmt.Sprintf("the raider — %s first, then %s", d[0], d[1])
	case govt.Green:
		return fmt.Sprintf("the grower — %s first, then %s", d[0], d[1])
	case govt.Blue:
		return fmt.Sprintf("the fortress — %s first, then %s", d[0], d[1])
	}
	return "unaligned — builds nothing"
}

// siteFor is where a colour would stand up b: the governor's priority world
// if one is set and can take it, otherwise the building's natural home — a
// Works where the richest unbuilt chain is, everything else at the most
// populous world that can still take another level.
func (u *Universe) siteFor(c govt.Color, b Building) *World {
	if p := u.Worlds[u.Priority[c]]; p != nil && p.Govt == c && p.CanBuild(b) == nil {
		return p
	}
	worlds := u.worldsOf(c)
	if b == Works {
		return u.bestWorksSite(worlds)
	}
	var best *World
	for _, w := range worlds {
		if w.Seat == SeatPlayer || w.CanBuild(b) != nil {
			continue
		}
		if best == nil || w.Pop > best.Pop {
			best = w
		}
	}
	return best
}

// SetPriority names the world a colour upgrades first. The governor's one
// lever on the government's spending: not what it buys — that is doctrine —
// but where.
func (u *Universe) SetPriority(c govt.Color, stellar int) error {
	w := u.Worlds[stellar]
	if w == nil {
		return fmt.Errorf("no such port")
	}
	if w.Govt != c {
		return fmt.Errorf("%s is not held by %s", w.Name, c)
	}
	u.Priority[c] = stellar
	u.Journal.Logf(u.Day, -1, "%s: %s is the priority for upgrades", c, w.Name)
	return nil
}
