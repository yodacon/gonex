package ai

import (
	"math/rand"
	"testing"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/ship"
	"yodacon.org/gonex/internal/world"
)

func testWorld(t *testing.T) *world.World {
	t.Helper()
	cat, err := ship.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	w := world.New(cat, 1)
	w.Notify = func(string, ...any) {}
	return w
}

// holding builds a colour with a capital and an outpost, and returns both.
func holding(w *world.World, team world.Team, at gmath.Vec2, pop int) (*world.Planet, *world.Planet) {
	cap := &world.Planet{Body: world.Body{P: at}, Team: team, Name: team.String() + " Prime"}
	cap.Setup(pop)
	out := &world.Planet{Body: world.Body{P: at.Add(gmath.V(600, 0))},
		Team: team, Name: team.String() + " Outpost"}
	out.Setup(pop / 4)
	w.Add(cap)
	w.Add(out)
	return cap, out
}

func pilots(w *world.World, team world.Team, n int, orders string, at gmath.Vec2) []*world.Ship {
	var out []*world.Ship
	r := rand.New(rand.NewSource(int64(team)*31 + 7))
	for i := 0; i < n; i++ {
		s := w.NewShip(1, team, team.String()+string(rune('A'+i)), world.KindNPC)
		s.P = at
		s.Controller = Parse(orders, r)
		out = append(out, s)
	}
	return out
}

func run(w *world.World, secs float64) {
	for i := 0; i < int(secs*60); i++ {
		w.Update(1.0 / 60)
	}
}

func fleetOf(s *world.Ship) *Fleet { return s.Controller.(*Doctrine).fleet }

// Pilots rally at their colour's CAPITAL, not at whichever rock is nearest.
// Rallying near-est splits a squadron across every holding it has and no
// flight ever reaches the strength to set out.
func TestSquadronMustersAtTheCapital(t *testing.T) {
	w := testWorld(t)
	capital, out := holding(w, world.TeamRed, gmath.V(1000, 1000), 4000000)
	// half the pilots start beside the outpost, which is their nearest port
	crew := pilots(w, world.TeamRed, 3, "invade:2", capital.P)
	crew = append(crew, pilots(w, world.TeamRed, 3, "invade:2", out.P)...)
	holding(w, world.TeamGreen, gmath.V(9000, 9000), 4000000)

	run(w, 3)

	f := fleetOf(crew[0])
	if f == nil {
		t.Fatal("nobody mustered")
	}
	for _, s := range crew {
		if got := fleetOf(s); got != f {
			t.Errorf("%s mustered into a different flight than the rest", s.Name)
		}
	}
	if f.Home != capital {
		t.Errorf("the flight formed at %s, want the capital %s", f.Home.Label(), capital.Label())
	}
	if f.Size() != len(crew) {
		t.Errorf("flight has %d of %d pilots", f.Size(), len(crew))
	}
}

// A watch and a strike flight form at the same port without displacing one
// another: they are different jobs and need their own muster slots.
func TestWatchAndStrikeMusterSeparately(t *testing.T) {
	w := testWorld(t)
	capital, _ := holding(w, world.TeamRed, gmath.V(1000, 1000), 4000000)
	holding(w, world.TeamGreen, gmath.V(9000, 9000), 4000000)
	guards := pilots(w, world.TeamRed, 3, "guard", capital.P)
	strike := pilots(w, world.TeamRed, 3, "invade:2", capital.P)

	run(w, 3)

	gf, sf := fleetOf(guards[0]), fleetOf(strike[0])
	if gf == nil || sf == nil {
		t.Fatal("one of the two flights never formed")
	}
	if gf == sf {
		t.Fatal("the watch and the strike flight are the same flight")
	}
	if !gf.Guard || sf.Guard {
		t.Error("the guard flag did not follow the doctrine")
	}
	for _, s := range guards {
		if fleetOf(s) != gf {
			t.Error("a guard joined the strike flight")
		}
	}
}

// Under MinFleet a flight holds over its port; at MinFleet it sets out. This
// is the rotation between defence and offence.
func TestFlightWaitsForCompanyThenSetsOut(t *testing.T) {
	w := testWorld(t)
	capital, _ := holding(w, world.TeamRed, gmath.V(1000, 1000), 4000000)
	holding(w, world.TeamGreen, gmath.V(9000, 9000), 4000000)

	two := pilots(w, world.TeamRed, 2, "invade:2", capital.P)
	run(w, 6)

	f := fleetOf(two[0])
	if f == nil {
		t.Fatal("no flight formed")
	}
	if f.phase != Mustering {
		t.Fatalf("a flight of %d set out; MinFleet is %d", f.Size(), MinFleet)
	}
	for _, s := range two {
		if s.Pos().Sub(capital.P).Len() > musterRadius*1.6 {
			t.Errorf("%s wandered %0.f off the muster",
				s.Name, s.Pos().Sub(capital.P).Len())
		}
	}

	// The third pilot is what makes it a flight.
	pilots(w, world.TeamRed, 1, "invade:2", capital.P)
	run(w, 6)

	if f.Size() < MinFleet {
		t.Fatalf("the third pilot never joined: %d", f.Size())
	}
	if f.phase == Mustering {
		t.Error("a flight at strength is still sitting at home")
	}
	if f.Objective == nil || f.Objective.Team != world.TeamGreen {
		t.Error("the flight set out without an enemy objective")
	}
}

// The flight outlives its commander: command passes down the roster.
func TestCommandPassesDown(t *testing.T) {
	w := testWorld(t)
	capital, _ := holding(w, world.TeamRed, gmath.V(1000, 1000), 4000000)
	holding(w, world.TeamGreen, gmath.V(9000, 9000), 4000000)
	crew := pilots(w, world.TeamRed, 4, "invade:2", capital.P)
	run(w, 3)

	f := fleetOf(crew[0])
	if f == nil || f.Size() < 4 {
		t.Fatal("the flight never formed")
	}
	lead := f.Commander()
	if lead == nil {
		t.Fatal("no commander")
	}

	// Kill the commander. It comes back as a fresh hull, and a fresh hull is
	// a new pilot as far as the flight is concerned.
	killer := w.NewShip(1, world.TeamGreen, "killer", world.KindNPC)
	lead.HitByMissile(w, &world.Missile{
		Body: world.Body{P: lead.Pos()}, Owner: killer, Damage: 9999, TTL: 1,
	})
	run(w, 3)

	if f.Commander() == nil {
		t.Fatal("the flight collapsed with its commander")
	}
	if f.Commander() == lead {
		t.Error("a dead commander still has the flight")
	}
	if f.Size() == 0 {
		t.Error("the flight lost every member")
	}
}

// A flight caps out, and the next joiner starts a new one rather than
// swelling past the limit.
func TestFleetSizeIsCapped(t *testing.T) {
	old := MaxFleet
	MaxFleet = 4
	defer func() { MaxFleet = old }()

	w := testWorld(t)
	capital, _ := holding(w, world.TeamRed, gmath.V(1000, 1000), 4000000)
	holding(w, world.TeamGreen, gmath.V(9000, 9000), 4000000)
	crew := pilots(w, world.TeamRed, 9, "guard", capital.P)
	run(w, 6)

	seen := map[*Fleet]int{}
	for _, s := range crew {
		if f := fleetOf(s); f != nil {
			seen[f]++
		}
	}
	if len(seen) < 2 {
		t.Fatalf("9 pilots fitted into %d flight(s) with a cap of %d", len(seen), MaxFleet)
	}
	for f, n := range seen {
		if n > MaxFleet || f.Size() > MaxFleet {
			t.Errorf("a flight holds %d ships, cap is %d", f.Size(), MaxFleet)
		}
	}
}
