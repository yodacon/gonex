package universe

// The production math: which world to take, and why.
//
// This economy's ancestor is Konquest, and Konquest's whole game is three
// numbers per planet — ship count, production, and kill percent — plus the
// distance between them. Its strongest AI, Becai, is described as the one
// that "uses distance, kill %, and production optimally"; the weak ones each
// ignore one of the three.
//
// gonex inherited the kill percent — World Rating is exactly that — and then
// picked its conquests on kill percent ALONE, tie-broken by population. That
// is the weak AI by Konquest's own taxonomy, and on the full gazetteer it
// showed: all three colours scored the same distant neutral as their best
// target, one of them seven jumps away, because with a hundred and six
// populated neutrals rated within a narrow band the kill percent barely
// separates anything and the tie-break decides everything.
//
// Distance is the term that was missing and it is the one the source game
// calls decisive: a planet three turns away gives the defender three turns
// to reinforce. gonex already knows every lane length — the route board
// divides by it — so the number was there, unused, on the other side of the
// package.

import (
	"math"
	"sort"

	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// Target is one world a colour could take, scored on all three terms.
type Target struct {
	World *World
	// Rating is the world's kill percent: what taking it will cost.
	Rating float64
	// Jumps is how far the flight has to go, which is how long the world
	// has to be reinforced before it arrives.
	Jumps int
	// Produce is what the world is worth once held: the daily tonnage its
	// industry turns out. A rich world two jumps away and a barren one next
	// door are not the same prize, and scoring on cost alone cannot tell
	// them apart.
	Produce float64
	// Score is value over cost over time. Higher is better.
	Score float64
}

// Targets ranks every world this colour could take, best first.
//
// The score is the plainest reading of the three terms that behaves
// correctly at the edges:
//
//	score = produce / ((1 + rating) * (1 + jumps))
//
// A world that produces nothing scores nothing however cheap it is, which
// is what stops a colour walking its whole strike fleet to a barren rock.
// Cost and distance both divide rather than subtract, so a target twice as
// far must be twice as valuable to be worth the same — and neither term can
// drive the score negative, which a subtractive form does as soon as a
// galaxy gets wide.
func (u *Universe) Targets(c govt.Color) []Target {
	capital := u.Capital(c)
	if capital == nil {
		return nil
	}
	var out []Target
	for _, id := range u.order {
		w := u.Worlds[id]
		if w.Govt == c || w.Pop <= 0 {
			continue // its own, or a rock with nobody on it to govern
		}
		if w.Govt != govt.None && !u.hostile(c, w.Govt) {
			continue // an ally's world is not a target
		}
		t := Target{
			World:   w,
			Rating:  u.Rating(w),
			Jumps:   u.jumpsBetween(capital, w),
			Produce: u.produceOf(w),
		}
		t.Score = t.Produce / ((1 + t.Rating) * (1 + float64(t.Jumps)))
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].World.Stellar < out[j].World.Stellar
	})
	return out
}

// jumpsBetween recovers the hop count from the charted lane length. The
// lane table is the only place this package knows about geography, and it
// stores megametres because that is what the route board divides by.
func (u *Universe) jumpsBetween(a, b *World) int {
	l := u.lane(a, b)
	if l <= inSystemMm {
		return 0
	}
	return int(math.Round((l - inSystemMm) / jumpMm))
}

// produceOf is what a world turns out in a day, in tons, across every line
// it runs. It is nameplate rather than actual on purpose: a world starved
// by somebody else's blockade is still worth taking for what it CAN make.
func (u *Universe) produceOf(w *World) float64 {
	var t float64
	for _, p := range w.Plant {
		for _, port := range p.Out {
			t += port.Tons
		}
	}
	return t
}

// --- the rally point -----------------------------------------------------

// rally orders a colour's loose idle hulls home.
//
// This is Strategic Conquest's "Army Destination" — set a place, and what
// you build walks there — and without it gonex could not fight at all.
//
// expand() needs MinFleet+1 hulls idle AT THE CAPITAL, because a flight
// leaves from somewhere and arms itself from that somewhere's arsenal. But a
// courier berths where it last delivered: "a trader's home is wherever it
// last delivered" is written into arrive(), and it is the right rule for
// trade. The consequence nobody had measured is that in two simulated years
// on the full gazetteer, with sixty-six hulls idle on any given day, the
// number idle at a capital was ZERO — so the first line of expand() returned
// every time it was called, no colour ever attacked anything, and the map at
// day 730 was the map at genesis. Not one conquest. Not one hull lost.
//
// So a government recalls. It does not recall hulls that are working — a
// hull hauling or loading is left alone, and trade is the point of the
// game — only ones standing idle at somebody else's pad, and only enough of
// them to make up a flight. The nearest are called first, because a hull
// twelve jumps out is a month of nothing.
func (u *Universe) rally(c govt.Color) {
	capital := u.Capital(c)
	if capital == nil {
		return
	}
	want := govt.MinFleet(c) + 1 - len(u.idleAt(capital, c))
	if want <= 0 {
		return
	}
	type call struct {
		h     *traffic.Hull
		jumps int
	}
	var loose []call
	for _, h := range u.Fleet.Hulls {
		if h.Govt != c || h.Status != traffic.Idle || h.Home == capital.Stellar {
			continue
		}
		from := u.Worlds[h.Home]
		if from == nil {
			continue
		}
		loose = append(loose, call{h, u.jumpsBetween(from, capital)})
	}
	if len(loose) == 0 {
		return
	}
	sort.SliceStable(loose, func(i, j int) bool {
		if loose[i].jumps != loose[j].jumps {
			return loose[i].jumps < loose[j].jumps
		}
		return loose[i].h.ID < loose[j].h.ID
	})
	var sent int
	for _, l := range loose {
		if sent >= want {
			break
		}
		u.Fleet.Depart(l.h, l.h.Home, capital.Stellar, traffic.Returning, u.Day)
		sent++
	}
	if sent > 0 {
		u.Journal.Logf(u.Day, -1, "%s recalls %d hulls to %s", c, sent, capital.Name)
	}
}

// --- span of control -----------------------------------------------------

// Overhead is the share of its nominal capability a government actually gets
// at its current size: 1.0 up to the span of control, falling away beyond it.
//
// Until the war worked this term could not have mattered, because no colour
// could grow. Now that conquest is possible its absence is live, and the
// shape of the absence is that EVERY term in this game rewards size linearly
// and none of them charges for it. FleetCap is eight hulls per world held,
// so a colour with twice the territory may float twice the merchant marine
// and therefore fly twice the flights and take twice the worlds. There is no
// opposing term anywhere.
//
// Strategic Conquest's answer is the one adopted here: production time rises
// as the empire grows — more cities, longer builds for everything. It is a
// good answer because it is not a cap. A large empire is still larger; it is
// simply worse per world than a compact one, so conquest has a natural
// stopping point that is a decision rather than a wall.
//
// Three places charge it, and they are the three legs of the snowball:
//
//	the factory floor   every plant on the colour's worlds runs slower
//	the fleet ceiling   FleetCap grows sublinearly instead of linearly
//	the expansion clock a stretched government looks for a target less often
//
// It is deliberately calibrated to be INERT at genesis — the largest colour
// holds 52 worlds against a span of 60 — so today's balance numbers are
// unchanged and the term only speaks when somebody actually runs away. A
// dormant term measured against nothing would be indistinguishable from no
// term at all, so it is unit-tested directly instead.
func (u *Universe) Overhead(c govt.Color) float64 {
	if c == govt.None || c < 0 || int(c) >= len(u.overhead) {
		return 1
	}
	if u.overhead[c] <= 0 {
		return 1 // never counted; a government is not penalised for that
	}
	return u.overhead[c]
}

// refreshOverhead recomputes the span penalty once a day. Counting a
// colour's worlds is a scan of the map, and produce() would otherwise do it
// once per plant per world per tick.
func (u *Universe) refreshOverhead() {
	for i := range u.overhead {
		u.overhead[i] = 1
	}
	for _, c := range govt.Colors() {
		n := float64(len(u.worldsOf(c)))
		if n <= spanOfControl {
			continue
		}
		u.overhead[c] = math.Pow(spanOfControl/n, overheadExponent)
	}
}

const (
	// spanOfControl is how many worlds a government runs without friction.
	// Sixty, against a genesis spread of 20/40/52, so the term is silent
	// until a colour has taken a real bite out of somebody.
	spanOfControl = 60.0
	// overheadExponent is how hard the penalty bites past the span. A half
	// power is gentle on purpose: at twice the span a government keeps 71%
	// of its capability, at four times 50%. Conquest should get harder, not
	// self-defeating — an empire that shrinks when it wins is a bug wearing
	// a balance term's clothes.
	overheadExponent = 0.5
)
