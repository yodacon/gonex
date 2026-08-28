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
