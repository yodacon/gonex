package universe

import (
	"math"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// Prospecting: the two jobs that are about the ground rather than the board.
//
// Everything else in this economy moves tons that somebody already dug, and
// digging has exactly one ceiling: a world lifts crust in proportion to the
// people living on it. That is a defensible rule — a mine is a workforce —
// but it has an ugly corollary the trade layer could not address. A rock
// with an enormous seam and nobody on it produces NOTHING, for ever, no
// matter how badly the galaxy wants what is under it, and no amount of
// shipping can help because there is nothing at the pithead to ship.
//
// Two AIs answer it, and between them they are what makes "the bottleneck is
// mining and transport" a statement about one system instead of two.
//
//	HARVESTER  flies empty to a seam, lifts crust straight out of the
//	           ground into its own hold against a royalty paid to whoever
//	           holds the rock, and carries it to the nearest port that
//	           wants it. A hold at a time. It is extraction capacity that
//	           is MOBILE, so it goes where the shortage is.
//
//	SURVEY     flies to a seam, reads it, and leaves. For some weeks
//	           afterwards the world and its immediate neighbours lift more
//	           per day for the same workforce, because they now know where
//	           to dig. It carries nothing and is paid nothing, which makes
//	           it the one thing in the game a government does purely
//	           because the arithmetic says so.
//
// Both conserve mass by construction: a harvester's tons come out of a
// Reserve, which only ever falls, and a survey moves no tons at all.

// --- The survey ----------------------------------------------------------

const (
	// surveyDays is how long a reading is good for.
	surveyDays = 90
	// surveyLift is the extra fraction of its dig budget a surveyed world
	// gets. It is large enough to be worth the voyage and small enough that
	// a surveyed world is still a world and not a cheat.
	surveyLift = 0.45
	// surveyNeighbourLift is what the systems next door get. A survey reads
	// a formation, not a property line, and the brief the prospector files
	// is public — which is why one flight can be worth several worlds.
	surveyNeighbourLift = 0.20
	// surveyEvery is how often a government sends a prospector out.
	surveyEvery = 30
)

// SurveyLift is the multiplier on this world's dig budget today.
func (w *World) SurveyLift(day int) float64 {
	if day > w.SurveyUntil {
		return 1
	}
	return 1 + w.surveyBonus
}

// Surveyed reports whether a live reading covers this world, and until when.
func (w *World) Surveyed(day int) bool { return day <= w.SurveyUntil }

// recordSurvey files a prospector's brief at a world and over the fence.
func (u *Universe) recordSurvey(w *World) {
	w.SurveyUntil, w.surveyBonus = u.Day+surveyDays, surveyLift
	for _, id := range u.order {
		n := u.Worlds[id]
		if n == w || !u.oneJump(w, n) {
			continue
		}
		// Never downgrade a world that has its own live reading.
		if n.Surveyed(u.Day) && n.surveyBonus >= surveyNeighbourLift {
			continue
		}
		n.SurveyUntil, n.surveyBonus = u.Day+surveyDays, surveyNeighbourLift
	}
	u.Journal.Logf(u.Day, -1, "%s surveyed — the seams read %.0f%% richer for %d days",
		w.Name, surveyLift*100, surveyDays)
}

// sendSurvey picks a world worth prospecting and sends one idle hull.
//
// Worth prospecting means the widest gap between what a world's industry
// wants out of the ground each day and what its workforce can actually
// lift — a plant standing idle over a full seam is exactly the thing a
// survey fixes, and exactly the thing nothing else could.
func (u *Universe) sendSurvey(c govt.Color) {
	var best *World
	var bestGap float64
	for _, id := range u.order {
		w := u.Worlds[id]
		if !u.canTrade(c, w.Govt) || w.Surveyed(u.Day) {
			continue
		}
		var want, have float64
		for _, m := range econ.Crusts() {
			if w.Reserve[m] <= 0 {
				continue
			}
			want += w.Wants(m)
			have += w.Reserve[m]
		}
		if want <= 0 || have <= 0 {
			continue
		}
		gap := want - govt.MineRate(w.Govt)*(float64(w.Pop)/1e6+w.autoCrew())
		if gap > bestGap {
			best, bestGap = w, gap
		}
	}
	if best == nil {
		return
	}
	for _, h := range u.Fleet.ByGovt(c) {
		if h.Status != traffic.Idle || h.Laden() > 0 || h.Home == best.Stellar {
			continue
		}
		h.Mission = traffic.Survey
		u.Fleet.Depart(h, h.Home, best.Stellar, traffic.Hauling, u.Day)
		u.Journal.Logf(u.Day, h.ID, "%s files for a survey of %s (%.0f t/d of seam nobody can lift)",
			h.Name, best.Name, bestGap)
		return
	}
}

// --- The harvester -------------------------------------------------------

const (
	// royaltyFrac is the share of a crust material's base value a harvester
	// pays the world it lifted from. It is a real payment into a real
	// treasury: a rock with nobody on it still belongs to somebody, and a
	// neutral world that sells extraction rights is how the outer map earns
	// the credits it buys food with.
	royaltyFrac = 0.55
	// harvestShare is the most of a colour's fleet that may be out
	// gathering at once. A merchant marine that turns entirely into miners
	// stops carrying anything, and the shortage it was sent to fix gets
	// worse.
	harvestShare = 0.25
	// harvestFloor is the least a harvester will bother lifting.
	harvestFloor = 40.0
)

// wantsCrust is the crust material this colour is shortest of, measured as
// tons a day of industrial demand its own worlds cannot dig, and the size of
// that shortfall.
func (u *Universe) wantsCrust(c govt.Color) (econ.Material, float64) {
	var short econ.Stock
	for _, id := range u.order {
		w := u.Worlds[id]
		if w.Govt != c {
			continue
		}
		budget := govt.MineRate(w.Govt) * (float64(w.Pop)/1e6 + w.autoCrew()) * w.SurveyLift(u.Day)
		for _, m := range econ.Crusts() {
			if gap := w.Wants(m) - w.Warehouse[m]/reserveDays; gap > 0 {
				short.Add(m, math.Min(gap, w.Wants(m)))
			}
		}
		// Whatever the world can dig for itself is not a shortage.
		for _, m := range econ.Crusts() {
			if w.Reserve[m] > 0 {
				short[m] = math.Max(short[m]-budget/2, 0)
			}
		}
	}
	best, most := econ.Slag, 0.0
	for _, m := range econ.Crusts() {
		if short[m] > most {
			best, most = m, short[m]
		}
	}
	return best, most
}

// sendHarvesters puts idle hulls onto seams when the colour's own mines
// cannot keep its factories fed.
func (u *Universe) sendHarvesters(c govt.Color) {
	m, short := u.wantsCrust(c)
	if m == econ.Slag || short < harvestFloor {
		return
	}
	hulls := u.Fleet.ByGovt(c)
	out := 0
	for _, h := range hulls {
		if h.Mission == traffic.Harvester {
			out++
		}
	}
	if float64(out) >= harvestShare*float64(len(hulls)) {
		return
	}
	for _, h := range hulls {
		if h.Status != traffic.Idle || h.Laden() > 0 {
			continue
		}
		seam := u.richestSeam(c, m, h.Home)
		if seam == nil {
			return
		}
		h.Mission = traffic.Harvester
		if seam.Stellar == h.Home {
			u.harvest(h, seam) // already standing on it
			return
		}
		u.Fleet.Depart(h, h.Home, seam.Stellar, traffic.Hauling, u.Day)
		u.Journal.Logf(u.Day, h.ID, "%s sets out to work the %s at %s", h.Name, m, seam.Name)
		return
	}
}

// richestSeam is the best rock to go and work: the most tons in the ground
// per megametre of flying, among worlds this colour may deal with.
func (u *Universe) richestSeam(c govt.Color, m econ.Material, from int) *World {
	var best *World
	var bestV float64
	for _, id := range u.order {
		w := u.Worlds[id]
		if w.Reserve[m] <= 0 || !u.canTrade(c, w.Govt) {
			continue
		}
		v := w.Reserve[m] / math.Max(u.Fleet.Lane(from, id).Length, 1)
		if best == nil || v > bestV {
			best, bestV = w, v
		}
	}
	return best
}

// harvest fills a hull's hold straight out of a world's crust, against a
// royalty into that world's treasury, and points it at the nearest port
// that wants the stuff.
//
// The tons come out of Reserve and go into Cargo — the same Transfer any
// other movement uses, so the auditor sees a seam fall by exactly what a
// hold rose by. Nothing here can mint a ton, and the reserve, as everywhere
// else in this game, only ever goes down.
func (u *Universe) harvest(h *traffic.Hull, seam *World) {
	m, _ := u.wantsCrust(h.Govt)
	if m == econ.Slag || seam.Reserve[m] <= 0 {
		// The shortage moved on while the hull was in transit. Take
		// whatever this rock has most of rather than flying home empty.
		best, most := econ.Slag, 0.0
		for _, x := range econ.Crusts() {
			if seam.Reserve[x] > most {
				best, most = x, seam.Reserve[x]
			}
		}
		m = best
	}
	if m == econ.Slag {
		h.Mission = traffic.Courier
		return
	}
	want := math.Min(h.Free(), seam.Reserve[m])
	// A pilot pays for what it lifts, and cannot lift more than it can pay
	// for. A harvester is a small business like every other hull here.
	royalty := int(math.Round(Base(m) * royaltyFrac))
	if royalty > 0 {
		want = math.Min(want, float64(h.Purse/royalty))
	}
	if want < harvestFloor {
		h.Mission = traffic.Courier
		return
	}
	got := econ.Transfer(&seam.Reserve, &h.Cargo, m, want)
	econ.Pay(&h.Purse, &seam.Credits, int(got)*royalty)
	h.Bought = int(got) * royalty
	h.Mass = h.Wet()
	u.Journal.Mined.Add(m, got)
	u.Journal.Logf(u.Day, h.ID, "%s works %.0ft of %s out of %s (royalty %d cr)",
		h.Name, got, m, seam.Name, h.Bought)
	if seam.Reserve[m] <= 0 {
		u.Journal.Logf(u.Day, -1, "%s: the %s is worked out", seam.Name, m)
	}
	// And away: the nearest port that wants it. carryOn is exactly the rule
	// a laden hull with nowhere in particular to be already follows.
	h.Status = traffic.Idle
	if !u.carryOn(h) {
		h.Mission = traffic.Courier
	}
}
