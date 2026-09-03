package world

import "testing"

// testWorld comes from world_test.go.

func TestOutfitScalesWithCrewAndHull(t *testing.T) {
	w := testWorld(t)
	s := w.NewShip(12, TeamRed, "test", KindNPC) // the Yodacon: 5000 kg

	if s.Rounds != s.RoundsMax {
		t.Errorf("new ship not fully loaded: %d/%d", s.Rounds, s.RoundsMax)
	}
	want := 40 + 6*s.Crew + 50
	if s.RoundsMax != want {
		t.Errorf("magazine = %d, want %d for crew %d", s.RoundsMax, want, s.Crew)
	}
	// The Yodacon's deck in dockmode is 100 t, which is Mass/50 — a warship
	// carries the role fraction of that, and it is never zero.
	if s.HoldMax <= 0 || s.HoldMax >= 100 {
		t.Errorf("warship hold = %.1f t, want 0 < h < 100", s.HoldMax)
	}
	if s.Grid == nil || s.Grid.ReactorMW <= 0 {
		t.Fatal("ship has no power plant")
	}

	s.Role = RoleHauler
	s.Outfit(w)
	if s.HoldMax <= 100 {
		t.Errorf("hauler hold = %.1f t, want > 100", s.HoldMax)
	}
}

func TestFiringEmptiesTheMagazine(t *testing.T) {
	w := testWorld(t)
	s := w.NewShip(1, TeamRed, "test", KindNPC)
	s.Grid.CapCapMJ, s.Grid.CapMJ = 1e6, 1e6 // isolate the magazine from the caps

	shots := 0
	for i := 0; i < s.RoundsMax*2; i++ {
		s.fireCD = 0
		if s.Fire(w) {
			shots++
		}
	}
	if shots != s.RoundsMax {
		t.Errorf("fired %d shots on a %d-round magazine", shots, s.RoundsMax)
	}
	if s.Rounds != 0 {
		t.Errorf("magazine not empty: %d", s.Rounds)
	}
	s.fireCD = 0
	if s.Fire(w) {
		t.Error("a dry ship fired")
	}
}

func TestCapacitorsGateTheGun(t *testing.T) {
	w := testWorld(t)
	s := w.NewShip(1, TeamRed, "test", KindNPC)
	s.Grid.CapMJ = 0 // flat bank, full magazine
	s.fireCD = 0
	if s.Fire(w) {
		t.Error("fired with flat capacitors")
	}
	if s.Rounds != s.RoundsMax {
		t.Error("a refused shot still cost a round")
	}
}

func TestTurnaroundRearmsAndCostsThePlanet(t *testing.T) {
	w := testWorld(t)
	p := &Planet{Body: Body{}, Team: TeamRed, Name: "Cenron"}
	p.Setup(4000000)
	w.Add(p)

	s := w.NewShip(1, TeamRed, "Red 1", KindNPC)
	s.Rounds, s.Health, s.Junk = 0, 40, 8

	ip0, cr0 := p.IP, p.Credits
	if !w.Land(s, p) {
		t.Fatal("could not land on a friendly planet")
	}
	if s.Rounds != s.RoundsMax {
		t.Errorf("rearmed to %d/%d", s.Rounds, s.RoundsMax)
	}
	if s.Health != 100 {
		t.Errorf("repaired to %d", s.Health)
	}
	if p.IP >= ip0 || p.Credits >= cr0 {
		t.Error("the turnaround cost the planet nothing")
	}
	if p.Scrap != 8 || s.Junk != 0 {
		t.Errorf("salvage not landed: yard %.0f, aboard %.0f", p.Scrap, s.Junk)
	}
	if !s.Docked() {
		t.Error("ship is not on the pad")
	}

	// On the pad it is out of the world, then it launches clear.
	for i := 0; i < 2000 && s.Docked(); i++ {
		s.Update(w, 0.016)
	}
	if s.Docked() {
		t.Fatal("never left the pad")
	}
	if len(p.Pad) != 0 {
		t.Error("berth not released")
	}
	if s.P.Sub(p.P).Len() <= CollisionRange {
		t.Error("launched inside the pad's own landing range")
	}
}

func TestStarvedPlanetGivesPowerButNotBullets(t *testing.T) {
	w := testWorld(t)
	p := &Planet{Team: TeamRed, Name: "Cenron"}
	p.Setup(4000000)
	p.IP = 0.4 // a nearly spent buffer
	w.Add(p)

	s := w.NewShip(1, TeamRed, "Red 1", KindNPC)
	s.Rounds, s.Health = 0, 30
	s.Grid.BattMJ = 0

	w.Land(s, p)
	if s.Grid.BattFrac() < 0.99 {
		t.Errorf("power was refused: batt %.2f", s.Grid.BattFrac())
	}
	if s.Rounds == s.RoundsMax {
		t.Error("a starving planet fully rearmed a ship")
	}
	if s.Health == 100 {
		t.Error("a starving planet fully repaired a ship")
	}
	// Repairs go dark before the armoury does.
	if s.Rounds == 0 && s.Health > 30 {
		t.Error("repaired before it armed")
	}
}

// The promise the economy rests on: a ship can always get home and get back
// up again. A world with nothing left still puts its grid on the connector.
func TestBrokePlanetStillGivesPower(t *testing.T) {
	w := testWorld(t)
	p := &Planet{Team: TeamRed}
	p.Setup(4000000)
	p.IP, p.Credits = 0, 0
	w.Add(p)

	s := w.NewShip(1, TeamRed, "Red 1", KindNPC)
	s.Rounds, s.Grid.BattMJ = 0, 0
	w.Land(s, p)

	if s.Grid.BattFrac() < 0.99 {
		t.Errorf("a broke planet refused power: batt %.2f", s.Grid.BattFrac())
	}
	if s.Rounds != 0 {
		t.Errorf("a broke planet found %d rounds", s.Rounds)
	}
	if p.Credits < 0 {
		t.Errorf("the planet went into debt: %d", p.Credits)
	}
	if !p.Starving() {
		t.Error("a planet with nothing left does not read as starving")
	}
}

func TestBerthsAreFinite(t *testing.T) {
	w := testWorld(t)
	p := &Planet{Team: TeamRed}
	p.Setup(1000000) // one berth
	w.Add(p)
	if got := p.Berths(); got != 1 {
		t.Fatalf("berths = %d, want 1", got)
	}
	a := w.NewShip(1, TeamRed, "a", KindNPC)
	b := w.NewShip(1, TeamRed, "b", KindNPC)
	if !w.Land(a, p) {
		t.Fatal("first ship could not land")
	}
	if w.Land(b, p) {
		t.Error("second ship took an occupied berth")
	}
}

func TestNoLandingOnAnEnemyPad(t *testing.T) {
	w := testWorld(t)
	p := &Planet{Team: TeamGreen}
	p.Setup(1000000)
	w.Add(p)
	s := w.NewShip(1, TeamRed, "red", KindNPC)
	if w.Land(s, p) {
		t.Error("landed on an enemy planet")
	}
}

func TestIndustryRegenerates(t *testing.T) {
	p := &Planet{Team: TeamRed}
	p.Setup(4000000)
	p.IP, p.Credits = 0, 0
	p.Tick(DaySec) // one industrial day
	if want := float64(p.Pop) / PopPerIP; p.IP < want*0.99 || p.IP > want*1.01 {
		t.Errorf("a day earned %.1f IP, want %.1f", p.IP, want)
	}
	if p.Credits <= 0 {
		t.Error("a day earned no revenue")
	}
	// The buffer is capped at two days.
	for i := 0; i < 10; i++ {
		p.Tick(DaySec)
	}
	if p.IP > p.IPMax()+0.001 {
		t.Errorf("buffer %.1f over its %.1f ceiling", p.IP, p.IPMax())
	}
}

func TestDeathIsNotResupply(t *testing.T) {
	w := testWorld(t)
	w.Add(&SpawnPoint{Body: Body{}, Team: TeamRed})
	s := w.NewShip(1, TeamRed, "red", KindNPC)
	s.Hold[0], s.Junk = 12, 5
	s.die(w)

	if s.Rounds > s.RoundsMax/4 {
		t.Errorf("respawned with %d rounds — a fresh hull is not a rearm", s.Rounds)
	}
	if s.Junk != 0 || s.Hold[0] != 0 {
		t.Error("the cargo survived the wreck")
	}
	if s.Grid.BattFrac() > 0.5 {
		t.Error("respawned with a full battery")
	}
}

func TestDockedShipsAreNotTargets(t *testing.T) {
	w := testWorld(t)
	p := &Planet{Team: TeamRed}
	p.Setup(4000000)
	w.Add(p)
	victim := w.NewShip(1, TeamRed, "red", KindNPC)
	victim.Rounds = 0
	w.Land(victim, p)

	shooter := w.NewShip(1, TeamGreen, "green", KindNPC)
	shooter.P = victim.P
	m := &Missile{Body: Body{P: victim.P}, Owner: shooter, Damage: 100, TTL: 1}
	w.Add(m)
	m.Update(w, 0.016)

	if !m.Alive() {
		t.Error("a missile struck a ship on the pad")
	}
	if victim.Health != 100 {
		t.Error("a docked ship took damage")
	}
}
