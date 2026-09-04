package universe

import (
	"math"
	"sort"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/industry"
	"yodacon.org/gonex/internal/traffic"
)

// The second book, and the government that spends from it.

// Purses hands the credit ledger every place money can be: treasuries,
// exchequers, pilots' purses, and whatever the app registers — the player
// above all. A purse left out reads as burned money, which is the correct
// failure.
func (u *Universe) Purses() []int {
	out := make([]int, 0, len(u.Worlds)+len(u.Fleet.Hulls)+8)
	for _, id := range u.order {
		out = append(out, u.Worlds[id].Credits)
	}
	for _, h := range u.Fleet.Hulls {
		out = append(out, h.Purse)
	}
	out = append(out, u.Exchequer[:]...)
	for _, f := range u.ExternCredits {
		out = append(out, f())
	}
	return out
}

// AccountCredits registers a purse held outside this package.
func (u *Universe) AccountCredits(f func() int) { u.ExternCredits = append(u.ExternCredits, f) }

// ReopenLedger re-bases the ledger on the money in the universe right now.
// For restoring a save and for hand-built scenarios, like ReopenBooks.
func (u *Universe) ReopenLedger() {
	total := 0
	for _, p := range u.Purses() {
		total += p
	}
	u.Ledger = econ.NewLedger(total)
}

// AuditCredits proves the money supply is what it was.
func (u *Universe) AuditCredits() *econ.Imbalance { return u.Ledger.Audit(u.Purses()...) }

// MoneySupply is every credit in the universe today.
func (u *Universe) MoneySupply() int {
	t := 0
	for _, p := range u.Purses() {
		t += p
	}
	return t
}

// --- The government ------------------------------------------------------

// govern is the colour's AI spending its exchequer. It faces the same menu
// and the same ladder as the player, once a week, and it makes three kinds
// of decision:
//
//  1. Subsidy: a world whose treasury cannot cover a few days of imports is
//     topped up, so the couriers can still earn there.
//  2. Placement: a Works goes where the rocks and the neighbours already
//     agree — the held world with the richest chain its crust can back and
//     has not yet stood up; a Bastion goes to a capital whose rating has
//     slipped.
//  3. Expansion: Konquest's loop. A colour with hulls to spare at its
//     capital sends a flight at the softest unaligned world it can see.
//
// The trifecta is the only source of colour difference in all of it.
func (u *Universe) govern() {
	if u.Day%governEvery != 0 {
		return
	}
	for _, c := range govt.Colors() {
		exch := &u.Exchequer[c]
		worlds := u.worldsOf(c)
		if len(worlds) == 0 {
			continue
		}
		// 1. Subsidy.
		for _, w := range worlds {
			need := int(u.importBill(w) * subsidyDays)
			if w.Credits < need {
				econ.Pay(exch, &w.Credits, need-w.Credits)
			}
		}
		// 2. Placement.
		if best := u.bestWorksSite(worlds); best != nil && *exch >= best.Price(Works)*2 {
			if err := u.Build(best, Works, exch, SeatAI); err == nil {
				continue
			}
		}
		if cap := u.Capital(c); cap != nil && u.Rating(cap) < weakRating && *exch >= cap.Price(Bastion)*2 {
			_ = u.Build(cap, Bastion, exch, SeatAI)
		}
		// 3. Expansion.
		u.expand(c)
	}
}

const (
	governEvery = 7
	subsidyDays = 3.0
	weakRating  = 0.40
	// expandEvery is how often a government looks for a world to take.
	expandEvery = 21
)

// importBill is roughly what a world pays for a day of the finished goods
// it does not make.
func (u *Universe) importBill(w *World) float64 {
	var bill float64
	for m := econ.Material(0); m < econ.Count; m++ {
		if !m.Finished() || w.Makes(m) {
			continue
		}
		bill += w.appetite(m) * float64(w.Shop[m])
	}
	return bill
}

// bestWorksSite picks the held world with the richest chain it could still
// stand up — the placement rule in one sentence.
func (u *Universe) bestWorksSite(worlds []*World) *World {
	var best *World
	var bestTons float64
	for _, w := range worlds {
		if w.Seat == SeatPlayer || w.CanBuild(Works) != nil {
			continue
		}
		ranked := industry.Rank(w.Reserve)
		if len(ranked) <= len(w.Plant) {
			continue
		}
		next := ranked[len(w.Plant)]
		tons := math.MaxFloat64
		for _, m := range next.Needs() {
			tons = math.Min(tons, w.Reserve[m])
		}
		if best == nil || tons > bestTons {
			best, bestTons = w, tons
		}
	}
	return best
}

// expand is Konquest's loop: attack the lowest-kill planet from a higher-
// kill one. Every few weeks a colour whose capital has a flight's worth of
// idle hulls, and out-rates the softest unaligned world it can see, sends
// that flight. The order is one-off, not standing: a government does not
// besiege, it takes what is soft and moves on. Capture more worlds, produce
// more, capture more.
func (u *Universe) expand(c govt.Color) {
	if u.Day%expandEvery != 0 {
		return
	}
	cap := u.Capital(c)
	if cap == nil {
		return
	}
	n := govt.MinFleet(c)
	idle := u.idleAt(cap, c)
	if len(idle) < n+1 {
		return
	}
	var target *World
	var targetR float64
	for _, id := range u.order {
		w := u.Worlds[id]
		if w.Govt != govt.None || w.Pop <= 0 {
			continue
		}
		r := u.Rating(w)
		if target == nil || r < targetR || (r == targetR && w.Pop > target.Pop) {
			target, targetR = w, r
		}
	}
	if target == nil || targetR >= u.Rating(cap) {
		return
	}
	for i := 0; i < n && i < len(idle)-1; i++ {
		h := idle[i]
		u.arm(cap, h)
		h.Mission = traffic.Flight
		u.Fleet.Depart(h, cap.Stellar, target.Stellar, traffic.Hauling, u.Day)
	}
	u.Journal.Logf(u.Day, -1, "%s sends %d hulls from %s to take %s (rated %.2f)",
		c, n, cap.Name, target.Name, targetR)
}

// Standings is a colour-by-colour summary for the desk and the console.
type Standing struct {
	Color     govt.Color
	Worlds    int
	Pop       int
	Hulls     int
	Treasury  int // every treasury it holds
	Exchequer int
	Rating    float64 // its capital's
}

// Standings ranks the three colours by worlds held, then population.
func (u *Universe) Standings() []Standing {
	var out []Standing
	for _, c := range govt.Colors() {
		s := Standing{Color: c, Exchequer: u.Exchequer[c]}
		for _, w := range u.worldsOf(c) {
			s.Worlds++
			s.Pop += w.Pop
			s.Treasury += w.Credits
		}
		s.Hulls = len(u.Fleet.ByGovt(c))
		if cap := u.Capital(c); cap != nil {
			s.Rating = u.Rating(cap)
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Worlds != out[j].Worlds {
			return out[i].Worlds > out[j].Worlds
		}
		return out[i].Pop > out[j].Pop
	})
	return out
}
