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

// govern is the colour's AI spending its exchequer, once a week. It faces
// the same menu and the same ladder as the player, and it makes four kinds
// of decision:
//
//  1. Tax: every held world remits a share of what its treasury holds above
//     a few days of imports. That is where an exchequer's money comes from
//     — tariffs alone left every exchequer at zero for the first hundred
//     and twenty days, because early deliveries all went to neutrals.
//  2. Subsidy: a world whose treasury cannot cover a few days of imports is
//     topped up, so the couriers can still earn there.
//  3. Upgrades, by DOCTRINE and PRIORITY: the first building in the colour's
//     doctrine it can afford, at the governor's priority world if one is set
//     and can take it, otherwise at the building's natural home. One a week.
//  4. Expansion: Konquest's loop; see expand.
//
// The trifecta is the only source of colour difference in all of it.
func (u *Universe) govern() {
	if u.Day%u.Tune.GovernEvery != 0 {
		return
	}
	for _, c := range govt.Colors() {
		exch := &u.Exchequer[c]
		worlds := u.worldsOf(c)
		if len(worlds) == 0 {
			continue
		}
		// 1. Tax.
		for _, w := range worlds {
			floor := int(u.importBill(w) * subsidyDays * 2)
			if surplus := w.Credits - floor; surplus > 0 {
				econ.Pay(&w.Credits, exch, int(float64(surplus)*u.Tune.TaxRate))
			}
		}
		// 2. Subsidy.
		for _, w := range worlds {
			need := int(u.importBill(w) * subsidyDays)
			if w.Credits < need {
				econ.Pay(exch, &w.Credits, need-w.Credits)
			}
		}
		if !u.Policies[c].Auto {
			continue // the governor buys by hand; the exchequer waits
		}
		// 3. Upgrades, by the week's plan: doctrine, bent by focus.
		for _, b := range u.plan(c) {
			site := u.siteFor(c, b)
			if site == nil {
				continue
			}
			if *exch-site.Price(b) < u.Tune.ExchequerReserve {
				continue
			}
			if err := u.Build(site, b, exch, SeatAI); err == nil {
				break
			}
		}
		// 3b. The focus's investments that are not buildings.
		u.invest(c)
		// 3c. Restock: a capital going dry gets a rounds convoy from any
		// held world that has them. Konquest's planet with a zero kill
		// percentage is the one thing no government should let happen to
		// its capital without trying.
		u.restock(c)
		// 4. Expansion.
		u.expand(c)
	}
}

const subsidyDays = 3.0

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
	if u.Day%u.Tune.ExpandEvery != 0 {
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

// restock files a rounds convoy to a colour's capital when its magazine is
// thin, from the held world with the most to spare. It is a standing order
// like any other, replaced each week it is still needed and left to lapse
// (the source runs out, or the capital's cover recovers past the threshold
// and the government cancels it) otherwise.
func (u *Universe) restock(c govt.Color) {
	cap := u.Capital(c)
	if cap == nil {
		return
	}
	cover := cap.Cover(econ.Rounds)
	if cover < 0 || cover >= restockCoverDays {
		// Enough: strike any restock order still running.
		for _, w := range u.worldsOf(c) {
			for i := len(w.Orders) - 1; i >= 0; i-- {
				o := w.Orders[i]
				if o.To == cap.Stellar && o.Mat == econ.Rounds && o.Hulls == 0 {
					u.Cancel(w.Stellar, i)
				}
			}
		}
		return
	}
	var src *World
	for _, w := range u.worldsOf(c) {
		if w == cap || w.Warehouse[econ.Rounds] < minLoad*2 {
			continue
		}
		if src == nil || w.Warehouse[econ.Rounds] > src.Warehouse[econ.Rounds] {
			src = w
		}
	}
	if src == nil {
		return
	}
	_ = u.File(StandingOrder{From: src.Stellar, To: cap.Stellar, Owner: c, Mat: econ.Rounds, Tons: 40})
}

// restockCoverDays is the magazine, in days of burn, below which a capital
// is restocked.
const restockCoverDays = 30.0
