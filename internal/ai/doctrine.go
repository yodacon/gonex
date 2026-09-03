package ai

import (
	"fmt"
	"math/rand"
	"strings"

	"yodacon.org/gonex/internal/world"
)

// A doctrine is a pilot's standing orders, and the point of it is not that
// the AI is clever — it is that the AI is LEGIBLE. A player watching the map
// should be able to say what that ship is doing and why it just turned away.
// So the orders are an explicit state a pilot flies and prints, never an
// emergent behavior nobody can name.
//
// Every pilot also carries its own tunings, jittered at creation: two guards
// off the same planet fight at different ranges and go home at different
// bingo numbers. The squadron reads as a dozen pilots rather than one program
// running twelve times.

// Stance is what a doctrine does when it is not going home.
type Stance int

const (
	// Guard holds station over its home planet and fights what comes near.
	Guard Stance = iota
	// Invade goes looking, usually for one particular colour.
	Invade
	// Escort keeps station on the fleet and is the roaming default.
	Escort
)

func (st Stance) String() string {
	switch st {
	case Guard:
		return "guard"
	case Invade:
		return "invade"
	}
	return "escort"
}

const (
	// The approach: inside this range a returning pilot flies the pattern
	// rather than the transit, and holds it under this speed so it can
	// actually settle onto the pad.
	approachRange = 700.0
	approachSpeed = 140.0

	// The hysteresis. battBingo sends a pilot home; battReady and hullMargin
	// are what it takes to send one back out, and the gap between them is
	// what stops the two decisions arguing.
	battBingo  = 0.12
	battReady  = 0.50
	hullMargin = 15

	// A pilot does not re-announce its orders more often than this, however
	// hard it is being shot at.
	sayEvery = 4.0

	// How long a pilot will keep trying to get home before it gives up and
	// goes back to work. Every berth on the holding can be full, or the port
	// it picked can be across the map behind two enemy squadrons — without a
	// wave-off those pilots orbit their own pad until the game ends.
	rtbPatience = 45.0
)

// state is where a pilot is in its sortie.
type state int

const (
	stFly state = iota
	stEngage
	stReturn
)

func (s state) String() string {
	switch s {
	case stEngage:
		return "ENGAGE"
	case stReturn:
		return "RTB"
	}
	return "PATROL"
}

// Doctrine is one pilot's orders plus its personal tunings.
type Doctrine struct {
	Stance Stance
	Quarry world.Team // Invade: the colour this pilot came for. TeamNone: whoever is nearest.

	// Tunings — jittered per pilot, which is what makes a squadron read as
	// a dozen fliers instead of one behavior twelve times over.
	Leash     float64 // how far from home it will be drawn. 0 = unleashed.
	Aggro     float64 // range at which it commits to a target
	Standoff  float64 // the distance it wants to fight at
	Bingo     float64 // magazine fraction that turns it for home
	HullBingo int     // hull points that turn it for home

	home  *world.Planet
	st    state
	rtbT  float64 // seconds spent trying to get home this sortie
	said  state
	saidT float64 // cooldown on the order tape, so a busy pilot cannot flood it
	relit float64 // re-home cooldown: don't rescan for a base every frame
}

// Name identifies the behavior for saves and the console.
func (d *Doctrine) Name() string {
	if d.Stance == Invade && d.Quarry != world.TeamNone {
		return fmt.Sprintf("invade:%d", int(d.Quarry))
	}
	return d.Stance.String()
}

// Held is how long this pilot has been trying to get home on this sortie.
// A number that keeps climbing is the signature of a stranded ship.
func (d *Doctrine) Held() float64 {
	if d.st != stReturn {
		return 0
	}
	return d.rtbT
}

// Status is the order the pilot is flying right now — PATROL, ENGAGE or RTB.
// The console roster prints it, which is what makes a fleet legible.
func (d *Doctrine) Status() string { return d.st.String() }

// Parse builds a doctrine from an orders string — a scene's doctrine="…"
// attribute, or a saved controller name. Unknown strings, konex's original
// "rabies" and "siege" among them, come back as an escort: the roaming,
// unleashed default those two always were.
//
// The rng is the pilot's own personality draw, so it must be the world's:
// tuning twelve guards off one fixed seed would give you one guard twelve
// times, which is exactly what this is here to avoid.
func Parse(name string, r *rand.Rand) world.Controller {
	d := &Doctrine{Stance: Escort}
	switch {
	case name == "guard":
		d.Stance = Guard
	case strings.HasPrefix(name, "invade:"):
		d.Stance = Invade
		var t int
		fmt.Sscanf(name[len("invade:"):], "%d", &t)
		d.Quarry = world.Team(t)
	case name == "invade":
		d.Stance = Invade
	}
	d.tune(r)
	return d
}

// New builds a doctrine and rolls this pilot's personal tunings.
func New(stance Stance, quarry world.Team, r *rand.Rand) *Doctrine {
	d := &Doctrine{Stance: stance, Quarry: quarry}
	d.tune(r)
	return d
}

// tune draws the per-pilot spread. The centres are the old konex constants;
// the jitter is the personality.
func (d *Doctrine) tune(r *rand.Rand) {
	jit := func(mid, spread float64) float64 { return mid * (1 - spread + r.Float64()*2*spread) }
	d.Aggro = jit(fireRange, 0.30)
	d.Standoff = jit(closeRange, 0.35)
	d.Bingo = jit(0.15, 0.6)
	d.HullBingo = 18 + r.Intn(26)
	if d.Stance == Guard {
		d.Leash = jit(2600, 0.35)
	}
}

// Random is the old ai.Random: a pilot for a scene that did not ask for one.
// It rolls a stance too, so even an unscripted spawn has orders.
func Random(r *rand.Rand) world.Controller {
	switch r.Intn(3) {
	case 0:
		return New(Guard, world.TeamNone, r)
	case 1:
		return New(Invade, world.Team(1+r.Intn(3)), r)
	}
	return New(Escort, world.TeamNone, r)
}

// --- flying it -----------------------------------------------------------

func (d *Doctrine) Control(s *world.Ship, w *world.World, dt float64) {
	if s.Docked() {
		return
	}
	d.relit -= dt
	d.saidT -= dt
	if d.home == nil || d.home.Team != s.Team || d.relit <= 0 {
		// A guard's station is simply the nearest friendly world; a pilot
		// heading home wants one with a free berth and something to sell.
		if d.st == stReturn {
			d.home = w.ClosestPort(s.Pos(), s.Team)
		} else {
			d.home = w.ClosestPlanet(s.Pos(), s.Team)
		}
		d.relit = 4
	}

	// Return conditions, and the order of these two cases matters. A pilot
	// already heading home stays committed — it is not talked back into the
	// fight by a target of opportunity — so the RELEASE is what has to be
	// asked first. Ask "must I go home?" first and a committed pilot answers
	// yes forever, which strands the whole squadron in a holding pattern over
	// its own pad with full magazines.
	if d.st == stReturn {
		d.rtbT += dt
		// Released by a turnaround, or waved off after trying long enough —
		// but only if it can still fight. A pilot that is genuinely dry keeps
		// trying, because there is nothing else it can usefully do.
		if d.resupplied(s) || (d.rtbT > rtbPatience && d.armed(s)) {
			d.setState(s, w, stFly)
		}
	} else if d.bingo(s) {
		d.setState(s, w, stReturn)
	}

	if d.st == stReturn {
		d.flyHome(s, w, dt)
		return
	}
	d.fight(s, w, dt)
}

// bingo is the turn-for-home check — magazine, hull, or a flat battery.
func (d *Doctrine) bingo(s *world.Ship) bool {
	if s.RoundsFrac() <= d.Bingo || s.Health <= d.HullBingo {
		return true
	}
	return s.Grid != nil && s.Grid.BattFrac() < battBingo
}

// resupplied is the release condition: what a turnaround has to have bought
// before the pilot will go back out. A planet too poor to clear this bar
// keeps its pilots on the ground, which is what losing a supply war looks
// like from the cockpit.
//
// Every condition bingo tests has to be cleared here with room to spare.
// The two must not disagree about any one gauge — a pilot released on the
// exact threshold that sent it home flips between orders every frame and
// flies nowhere at all.
func (d *Doctrine) resupplied(s *world.Ship) bool {
	return d.armed(s) && (s.Grid == nil || s.Grid.BattFrac() > battReady)
}

// armed is the fighting half of that: magazine and hull, with the margin
// that keeps this from arguing with bingo.
func (d *Doctrine) armed(s *world.Ship) bool {
	return s.RoundsFrac() > d.Bingo*2 && s.Health > d.HullBingo+hullMargin
}

// flyHome runs for the pad, and lands when it gets there. A pilot on the way
// home still shoots at whatever is directly in front of it — it is not a
// pacifist, it is just out of options.
func (d *Doctrine) flyHome(s *world.Ship, w *world.World, dt float64) {
	if d.home == nil {
		d.fight(s, w, dt) // nowhere to go: fight where you stand
		return
	}
	dist := d.home.Pos().Sub(s.Pos()).Len()
	if dist < world.CollisionRange && w.Land(s, d.home) {
		return
	}
	s.FaceToward(w, d.home.Pos(), dt)
	// Run for the port, then fly an approach. Without the braking a fast
	// ship simply slingshots past its own pad, over and over, and never
	// gets close enough to land — a hold that looks convincing and never
	// ends.
	switch {
	case dist > approachRange:
		s.Thrust(w, dt)
	case s.Vel().Len() > approachSpeed:
		s.Slow(dt * 4)
	default:
		s.Thrust(w, dt)
	}
	if t := w.ClosestEnemy(s); t != nil && s.Rounds > 0 &&
		t.Pos().Sub(s.Pos()).Len() < d.Standoff {
		s.Fire(w)
	}
}

// fight picks a target under the doctrine's orders and works it.
func (d *Doctrine) fight(s *world.Ship, w *world.World, dt float64) {
	target := d.pick(s, w)
	s.Target = target

	// Leashed and nothing to fight: go back and sit over the home planet.
	if target == nil {
		d.setState(s, w, stFly)
		if d.Leash > 0 && d.home != nil {
			if to := d.home.Pos().Sub(s.Pos()); to.Len() > d.Leash*0.4 {
				s.FaceToward(w, d.home.Pos(), dt)
				s.Thrust(w, dt)
				return
			}
		}
		s.Slow(dt)
		return
	}
	d.setState(s, w, stEngage)

	prox := target.Pos().Sub(s.Pos()).Len()
	if prox < d.Aggro {
		s.Fire(w)
	}
	s.FaceToward(w, target.Pos(), dt)
	if prox > d.Standoff {
		s.Thrust(w, dt)
	} else {
		s.Slow(dt)
	}
}

// pick applies the orders: a guard only takes what comes inside its leash, an
// invader prefers the colour it was sent for, everyone else takes the nearest.
func (d *Doctrine) pick(s *world.Ship, w *world.World) *world.Ship {
	if d.Stance == Invade && d.Quarry != world.TeamNone && d.Quarry != s.Team {
		if t := closestOfTeam(w, s, d.Quarry); t != nil {
			return t
		}
	}
	t := w.ClosestEnemy(s)
	if t == nil {
		return nil
	}
	if d.Stance == Invade {
		return t
	}
	// A guard will not be led away from its planet. This is the behavior
	// that makes a defended world actually feel defended.
	if d.Leash > 0 && d.home != nil {
		if t.Pos().Sub(d.home.Pos()).Len() > d.Leash {
			return nil
		}
	}
	if t.Pos().Sub(s.Pos()).Len() > d.Aggro*3 {
		return nil
	}
	return t
}

func closestOfTeam(w *world.World, s *world.Ship, team world.Team) *world.Ship {
	var best *world.Ship
	bestD := w.MapW * 2
	for _, e := range w.Entities {
		o, ok := e.(*world.Ship)
		if !ok || o == s || o.Team != team || o.Docked() {
			continue
		}
		if dist := o.Pos().Sub(s.Pos()).Len(); dist < bestD {
			best, bestD = o, dist
		}
	}
	return best
}

// setState prints the order the pilot is now flying, once per transition.
// This console tape is the feature: a battle you can read is a battle you
// can form intentions about.
func (d *Doctrine) setState(s *world.Ship, w *world.World, st state) {
	if st == stReturn && d.st != stReturn {
		d.rtbT = 0
	}
	d.st = st
	if d.said == st || s.Kind != world.KindNPC || d.saidT > 0 {
		return
	}
	d.said, d.saidT = st, sayEvery
	switch st {
	case stReturn:
		where := "no port"
		if d.home != nil {
			where = d.home.Label()
		}
		w.Notify("%s RTB %s — %d rounds, %d%% hull", s.Name, where, s.Rounds, s.Health)
	case stEngage:
		if s.Target != nil {
			w.Notify("%s ENGAGE %s (%s)", s.Name, s.Target.Name, d.Name())
		}
	}
}
