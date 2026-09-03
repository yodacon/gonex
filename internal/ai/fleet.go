package ai

import (
	"fmt"
	"math"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/world"
)

// Ships fly together. A pilot coming off the pad does not set out alone — it
// holds over its own port, defends it, and waits for enough company to make a
// flight. When the flight is big enough it picks an objective, forms up behind
// a commander, and goes looking for whatever is flying between home and the
// enemy's holding. Attrited or dry, it turns for home and dissolves. That
// cycle — muster, advance, strike, withdraw — is the rotation between defence
// and offence, and it is the fleet that rotates, not the individual pilot.
//
// The commander is the only pilot that THINKS. It picks the target, and the
// rest of the flight relays off it: one target search per flight per tick
// instead of one per ship per frame, which is what makes seventy-odd ships
// affordable. Command passes down the roster when the commander dies, docks,
// or turns for home, so the flight outlives any of its members.

// The tuning knobs. All of the fleet's behaviour is these numbers.
var (
	// MinFleet is how much company a pilot waits for before setting out.
	MinFleet = 3
	// MaxFleet caps a flight; past this, joiners start a new one.
	MaxFleet = 32

	// fleetTick is how often a commander re-thinks. Everything expensive
	// the AI does happens on this clock and nowhere else.
	fleetTick = 0.40

	// musterRadius is how close to the port a forming flight stays.
	musterRadius = 1400.0
	// formSpacing is the gap between wingmen in the V.
	formSpacing = 260.0
	// formSlack is how near its slot a wingman has to be to stop chasing it.
	formSlack = 180.0
	// strikeRange is when an advance becomes a strike.
	strikeRange = 1800.0
	// strayRange drops a member that has lost the flight entirely.
	strayRange = 6000.0

	// sortieMax is how long a flight stays out before it goes home on
	// principle, however well it is doing.
	sortieMax = 240.0
	// corridorBonus favours targets sitting between home and the objective —
	// the patrol line, which is what a strike is actually hunting.
	corridorBonus = 2500.0
)

// Phase is what a flight is doing. The full transition table lives in
// docs/fleet-state-machine.md.
type Phase int

const (
	// Mustering: holding over the home port, taking joiners, defending it.
	Mustering Phase = iota
	// Advancing: formed up and moving on the objective.
	Advancing
	// Striking: the called target is in reach; weapons free.
	Striking
	// Withdrawing: spent. Everyone home, and the flight dissolves.
	Withdrawing
)

func (p Phase) String() string {
	switch p {
	case Advancing:
		return "ADVANCE"
	case Striking:
		return "STRIKE"
	case Withdrawing:
		return "WITHDRAW"
	}
	return "MUSTER"
}

// Fleet is a flight of ships under one commander.
type Fleet struct {
	Team   world.Team
	Quarry world.Team    // the colour this flight was raised against
	Home   *world.Planet // where it musters and where it goes back to
	Guard  bool          // a standing defensive flight: it never leaves home

	Objective *world.Planet // the enemy holding being pushed toward
	Target    *world.Ship   // the called target — every member shoots this

	members []*world.Ship // members[0] commands
	phase   Phase
	thinkCD float64
	sortieT float64
	said    Phase
}

// rally is the muster board a port carries: one flight forming to go out and
// one standing watch over the port itself. They are separate because they are
// different jobs — the watch never leaves.
type rally struct {
	strike *Fleet
	watch  *Fleet
}

// rallyAt finds the flight forming up at a port, or raises one. This is the
// only way a Fleet is ever created, which is what keeps the muster points
// countable and makes joining a pointer read instead of a search.
//
// Pilots rally at their colour's CAPITAL, not at whichever port happens to be
// nearest. Rallying near-est splits a squadron across every rock it holds, and
// a dozen flights of two never reach the strength to set out — the whole
// colour sits at home mustering forever.
func rallyAt(home *world.Planet, team world.Team, quarry world.Team, guard bool) *Fleet {
	if home == nil {
		return nil
	}
	r, ok := home.Rally.(*rally)
	if !ok || r == nil {
		r = &rally{}
		home.Rally = r
	}
	slot := &r.strike
	if guard {
		slot = &r.watch
	}
	if f := *slot; f != nil && f.Team == team &&
		(f.phase == Mustering || f.Guard) && len(f.members) < MaxFleet {
		return f
	}
	f := &Fleet{Team: team, Quarry: quarry, Home: home, Guard: guard, phase: Mustering}
	*slot = f
	return f
}

// Name is how the flight signs its traffic.
func (f *Fleet) Name() string {
	kind := "flight"
	if f.Guard {
		kind = "watch"
	}
	if f.Home == nil {
		return f.Team.String() + " " + kind
	}
	return fmt.Sprintf("%s %s", f.Home.Label(), kind)
}

// Size is the flight's strength.
func (f *Fleet) Size() int { return len(f.members) }

// Phase is what it is doing.
func (f *Fleet) PhaseName() string { return f.phase.String() }

// Commander is the pilot calling targets, or nil for an empty flight.
func (f *Fleet) Commander() *world.Ship {
	if len(f.members) == 0 {
		return nil
	}
	return f.members[0]
}

func (f *Fleet) join(s *world.Ship) bool {
	if len(f.members) >= MaxFleet {
		return false
	}
	for _, m := range f.members {
		if m == s {
			return true
		}
	}
	f.members = append(f.members, s)
	return true
}

// leave drops a pilot. Losing the commander promotes the next on the roster;
// a flight is a chain of command, not one pilot with followers.
func (f *Fleet) leave(w *world.World, s *world.Ship) {
	for i, m := range f.members {
		if m != s {
			continue
		}
		f.members = append(f.members[:i], f.members[i+1:]...)
		if i == 0 && len(f.members) > 0 && w != nil {
			w.Notify("%s: %s has the flight (%d ships)",
				f.Name(), f.members[0].Name, len(f.members))
		}
		return
	}
}

// slotFor is a wingman's station in the V: alternating sides, stepping back
// a rank at a time behind the commander. Returns false for the commander
// itself, which flies the flight rather than following it.
func (f *Fleet) slotFor(s *world.Ship) (gmath.Vec2, bool) {
	cmd := f.Commander()
	if cmd == nil || cmd == s {
		return gmath.Vec2{}, false
	}
	idx := -1
	for i, m := range f.members {
		if m == s {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return gmath.Vec2{}, false
	}
	rank := float64((idx + 1) / 2)
	side := 1.0
	if idx%2 == 1 {
		side = -1
	}
	fwd := gmath.HeadingVec(cmd.Heading)
	right := gmath.HeadingVec(cmd.Heading + 90)
	return cmd.Pos().
		Add(fwd.Scale(-rank * formSpacing)).
		Add(right.Scale(side * rank * formSpacing * 0.8)), true
}

// centroid is where the flight is, for range decisions.
func (f *Fleet) centroid() gmath.Vec2 {
	if len(f.members) == 0 {
		return gmath.Vec2{}
	}
	var sum gmath.Vec2
	for _, m := range f.members {
		sum = sum.Add(m.Pos())
	}
	return sum.Scale(1 / float64(len(f.members)))
}

// think is the commander's job, and only the commander calls it. One target
// search per flight per tick is the whole efficiency argument for fleets.
func (f *Fleet) think(w *world.World, dt float64) {
	f.sortieT += dt
	if f.thinkCD -= dt; f.thinkCD > 0 {
		return
	}
	f.thinkCD = fleetTick
	f.prune(w)
	if len(f.members) == 0 {
		return
	}

	switch f.phase {
	case Mustering:
		// Forming up is not idling: the flight is the port's air defence
		// while it waits for the numbers to set out.
		f.Target = f.nearestThreat(w, f.Home.Pos(), musterRadius*2)
		if !f.Guard && len(f.members) >= MinFleet {
			if f.Objective = f.pickObjective(w); f.Objective != nil {
				f.setPhase(w, Advancing)
				f.sortieT = 0
			}
		}

	case Advancing:
		f.Target = f.callTarget(w)
		switch {
		case f.spent():
			f.setPhase(w, Withdrawing)
		case f.Target != nil &&
			f.Target.Pos().Sub(f.centroid()).Len() < strikeRange:
			f.setPhase(w, Striking)
		case f.Objective == nil || f.Objective.Team == f.Team:
			// Taken, or gone: find another or go home.
			if f.Objective = f.pickObjective(w); f.Objective == nil {
				f.setPhase(w, Withdrawing)
			}
		}

	case Striking:
		f.Target = f.callTarget(w)
		switch {
		case f.spent():
			f.setPhase(w, Withdrawing)
		case f.Target == nil ||
			f.Target.Pos().Sub(f.centroid()).Len() > strikeRange*1.6:
			f.setPhase(w, Advancing)
		}

	case Withdrawing:
		f.Target = nil
	}
}

// spent decides when a sortie is over: too few left to fight as a flight,
// too long out, or the flight as a whole is low on ammunition.
func (f *Fleet) spent() bool {
	if len(f.members) < MinFleet || f.sortieT > sortieMax {
		return true
	}
	var frac float64
	for _, m := range f.members {
		frac += m.RoundsFrac()
	}
	return frac/float64(len(f.members)) < 0.25
}

// prune drops members that are no longer flying with the flight.
func (f *Fleet) prune(w *world.World) {
	kept := f.members[:0]
	lostCmd := false
	for i, m := range f.members {
		if m.Docked() || m.Pos().Sub(f.centroid()).Len() > strayRange {
			if i == 0 {
				lostCmd = true
			}
			continue
		}
		kept = append(kept, m)
	}
	f.members = kept
	if lostCmd && len(f.members) > 0 && w != nil {
		w.Notify("%s: %s has the flight (%d ships)",
			f.Name(), f.members[0].Name, len(f.members))
	}
}

// pickObjective is the enemy holding this flight is being sent against:
// the quarry colour's nearest port, or any enemy's if it has no preference.
func (f *Fleet) pickObjective(w *world.World) *world.Planet {
	from := f.centroid()
	var best *world.Planet
	bestScore := math.MaxFloat64
	for _, e := range w.Entities {
		p, ok := e.(*world.Planet)
		if !ok || p.Team == world.TeamNone || p.Team == f.Team {
			continue
		}
		score := p.Pos().Sub(from).Len()
		if f.Quarry != world.TeamNone && p.Team != f.Quarry {
			score += 20000 // wrong colour: only if there is nothing else
		}
		if score < bestScore {
			best, bestScore = p, score
		}
	}
	return best
}

// callTarget is the commander's pick, and the one scan that matters. It
// prefers what is flying the corridor between home and the objective —
// the patrol line a strike is actually meant to break.
func (f *Fleet) callTarget(w *world.World) *world.Ship {
	from := f.centroid()
	var a, b gmath.Vec2
	haveLine := false
	if f.Home != nil && f.Objective != nil {
		a, b, haveLine = f.Home.Pos(), f.Objective.Pos(), true
	}

	var best *world.Ship
	bestScore := math.MaxFloat64
	for _, e := range w.Entities {
		o, ok := e.(*world.Ship)
		if !ok || o.Team == f.Team || o.Team == world.TeamNone || o.Docked() {
			continue
		}
		score := o.Pos().Sub(from).Len()
		if f.Quarry != world.TeamNone && o.Team == f.Quarry {
			score -= corridorBonus * 0.4
		}
		if haveLine && distToSegment(o.Pos(), a, b) < strikeRange {
			score -= corridorBonus
		}
		if score < bestScore {
			best, bestScore = o, score
		}
	}
	return best
}

// nearestThreat is the defensive equivalent: whatever is closest to a point
// we are holding, within a radius.
func (f *Fleet) nearestThreat(w *world.World, at gmath.Vec2, radius float64) *world.Ship {
	var best *world.Ship
	bestD := radius
	for _, e := range w.Entities {
		o, ok := e.(*world.Ship)
		if !ok || o.Team == f.Team || o.Team == world.TeamNone || o.Docked() {
			continue
		}
		if d := o.Pos().Sub(at).Len(); d < bestD {
			best, bestD = o, d
		}
	}
	return best
}

func (f *Fleet) setPhase(w *world.World, p Phase) {
	f.phase = p
	if f.said == p || w == nil {
		return
	}
	f.said = p
	switch p {
	case Advancing:
		obj := "open space"
		if f.Objective != nil {
			obj = f.Objective.Label()
		}
		w.Notify("%s ADVANCE on %s — %d ships, %s leading",
			f.Name(), obj, len(f.members), f.members[0].Name)
	case Striking:
		if f.Target != nil {
			w.Notify("%s STRIKE — target %s, %d ships",
				f.Name(), f.Target.Name, len(f.members))
		}
	case Withdrawing:
		w.Notify("%s WITHDRAW — %d ships left", f.Name(), len(f.members))
	}
}

// distToSegment is the perpendicular distance from p to the segment ab, used
// to decide whether a contact is sitting on the patrol corridor.
func distToSegment(p, a, b gmath.Vec2) float64 {
	ab := b.Sub(a)
	l2 := ab.X*ab.X + ab.Y*ab.Y
	if l2 == 0 {
		return p.Sub(a).Len()
	}
	ap := p.Sub(a)
	t := (ap.X*ab.X + ap.Y*ab.Y) / l2
	t = math.Max(0, math.Min(1, t))
	return p.Sub(a.Add(ab.Scale(t))).Len()
}
