package world

import (
	"testing"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/ship"
)

// The catalog loads real embedded assets; Ebitengine defers GPU work, so
// image creation and Bounds() are safe without a running game loop.
func testWorld(t *testing.T) *World {
	t.Helper()
	catalog, err := ship.LoadCatalog()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return New(catalog, 1)
}

func TestMissileKillScoresAndRespawns(t *testing.T) {
	w := testWorld(t)
	w.Add(&SpawnPoint{Body: Body{P: gmath.V(100, 100)}, Team: TeamGreen})

	shooter := w.NewShip(1, TeamRed, "Shooter", KindNPC)
	victim := w.NewShip(1, TeamGreen, "Victim", KindNPC)
	victim.P = gmath.V(5000, 5000)
	victim.Health = 1

	var killed string
	w.Notify = func(f string, args ...any) { killed = f }

	w.SpawnMissile(shooter)
	// Place the missile on the victim and let one update run collisions.
	m := w.Entities[len(w.Entities)-1].(*Missile)
	m.P, m.V = victim.P, gmath.Vec2{}
	w.Update(1.0 / 60)

	if shooter.Frags != 1 {
		t.Errorf("shooter frags = %d, want 1", shooter.Frags)
	}
	if w.Scores[TeamRed] != 1 {
		t.Errorf("red score = %d, want 1", w.Scores[TeamRed])
	}
	if victim.Deaths != 1 || victim.Health != 100 {
		t.Errorf("victim deaths=%d health=%d, want 1/100", victim.Deaths, victim.Health)
	}
	if victim.P != gmath.V(100, 100) {
		t.Errorf("victim respawned at %v, want team spawn (100,100)", victim.P)
	}
	if killed == "" {
		t.Error("kill notification not sent")
	}
	if m.Alive() {
		t.Error("missile survived its hit")
	}
}

func TestFriendlyFireIgnored(t *testing.T) {
	w := testWorld(t)
	shooter := w.NewShip(1, TeamRed, "A", KindNPC)
	friend := w.NewShip(1, TeamRed, "B", KindNPC)
	friend.Health = 1

	w.SpawnMissile(shooter)
	m := w.Entities[len(w.Entities)-1].(*Missile)
	m.P, m.V = friend.P, gmath.Vec2{}
	w.Update(1.0 / 60)

	if friend.Deaths != 0 {
		t.Error("friendly fire should not kill")
	}
}

func TestMoneyPickup(t *testing.T) {
	w := testWorld(t)
	s := w.NewShip(1, TeamRed, "A", KindNPC)
	w.Add(&Item{Body: Body{P: s.P}, Type: ItemMoney, Amount: 25, TTL: 10})
	w.Update(1.0 / 60)
	if s.Money != 26 { // starts with 1
		t.Errorf("money = %d, want 26", s.Money)
	}
}

func TestVelocityClamped(t *testing.T) {
	w := testWorld(t)
	s := w.NewShip(1, TeamRed, "A", KindNPC)
	s.V = gmath.V(1e6, 0)
	w.Update(1.0 / 60)
	if max := w.Catalog.Get(1).MaxVelocity; s.V.Len() > max+1e-6 {
		t.Errorf("velocity %v exceeds max %v", s.V.Len(), max)
	}
}

// The vector HUD draws Ship.Project as a promise about the future. This is
// the test that keeps it a promise: fly a real ship, under the real
// simulation, and check it lands where the projection said it would.
//
// Both cases matter and they fail differently. Coasting catches a change to
// the integrator; thrusting catches a change to acceleration, to the
// velocity clamp, or to the ORDER of the two — clamp after the step instead
// of before and the paths part company only at the limit, which is exactly
// where a pilot is reading the instrument hardest.
func TestProjectionMatchesActuallyFlying(t *testing.T) {
	for _, tc := range []struct {
		name      string
		thrusting bool
		v         gmath.Vec2
		heading   float64
	}{
		{"coasting", false, gmath.V(180, -90), 55},
		{"thrust across the vector", true, gmath.V(180, -90), 55},
		{"thrust along the vector", true, gmath.V(0, 200), 0},
		{"thrust from rest", true, gmath.Vec2{}, 210},
		{"thrust at the velocity clamp", true, gmath.V(0, 1e6), 90},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const secs, steps = 4.0, 48
			dt := secs / steps

			w := testWorld(t)
			s := w.NewShip(12, TeamRed, "Pilot", KindLocal)
			s.P, s.V, s.Heading = gmath.V(5000, 5000), tc.v, tc.heading
			path := s.Project(w, secs, steps, tc.thrusting)
			if len(path) != steps+1 {
				t.Fatalf("projection has %d points, want %d", len(path), steps+1)
			}

			// Now actually fly it, one identical step at a time.
			flown := w.NewShip(12, TeamRed, "Flown", KindLocal)
			flown.P, flown.V, flown.Heading = gmath.V(5000, 5000), tc.v, tc.heading
			spec := w.Catalog.Get(flown.ShipID)
			for i := 0; i < steps; i++ {
				if tc.thrusting {
					flown.Thrust(w, dt)
				}
				if v := flown.V.Len(); v > spec.MaxVelocity {
					flown.V = flown.V.Norm().Scale(spec.MaxVelocity)
				}
				flown.P = flown.P.Add(flown.V.Scale(dt))

				if d := flown.P.Sub(path[i+1]).Len(); d > 1e-6 {
					t.Fatalf("step %d: flown to %v, projected %v — %.6f apart",
						i+1, flown.P, path[i+1], d)
				}
			}
		})
	}
}

// A coasting projection must be a straight line — there is no drag in this
// sky, and the HUD labels it COAST on exactly that basis.
func TestCoastingProjectionIsStraight(t *testing.T) {
	w := testWorld(t)
	s := w.NewShip(12, TeamRed, "Pilot", KindLocal)
	s.P, s.V, s.Heading = gmath.V(5000, 5000), gmath.V(140, -70), 33
	path := s.Project(w, 4, 48, false)

	dir := path[1].Sub(path[0]).Norm()
	for i := 1; i+1 < len(path); i++ {
		step := path[i+1].Sub(path[i])
		if step.Len() < 1e-9 {
			continue
		}
		if off := step.Norm().Sub(dir).Len(); off > 1e-9 {
			t.Fatalf("coast bends at step %d by %.9f", i, off)
		}
	}
}

// And a thrusting projection across the velocity vector must NOT be straight,
// or the instrument's whole reason for existing is gone.
func TestThrustingProjectionDivergesFromCoasting(t *testing.T) {
	w := testWorld(t)
	s := w.NewShip(12, TeamRed, "Pilot", KindLocal)
	s.P, s.V, s.Heading = gmath.V(5000, 5000), gmath.V(180, -90), 55

	coast := s.Project(w, 4, 48, false)
	burn := s.Project(w, 4, 48, true)
	gap := burn[len(burn)-1].Sub(coast[len(coast)-1]).Len()
	if gap < 1 {
		t.Errorf("four seconds of thrust moved the ship %.3f units off its coast line", gap)
	}
	// The gap must GROW: authority accumulates, it does not arrive at once.
	prev := 0.0
	for i := range burn {
		d := burn[i].Sub(coast[i]).Len()
		if d < prev-1e-9 {
			t.Fatalf("divergence shrank at step %d: %.6f after %.6f", i, d, prev)
		}
		prev = d
	}
}
