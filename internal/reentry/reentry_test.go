package reentry

import (
	"math"
	"testing"
)

func runToEnd(s *Sim, c Controls, maxT float64) Status {
	return runToEndDt(s, c, maxT, 0.2)
}

func runToEndDt(s *Sim, c Controls, maxT, dt float64) Status {
	for s.Status() == Flying && s.T < maxT {
		s.Step(dt, c)
	}
	return s.Status()
}

// The corridor gate: the flight computer must land the Yodacon on the
// checkride profile with light damage and lithium to spare.
func TestAutolandNominal(t *testing.T) {
	s := New(Yodacon(), EarthProfile(), 1)
	st := runToEnd(s, Controls{Auto: true}, 2600)
	if st != Landed {
		t.Fatalf("autoland ended %v at h=%.0f v=%.0f t=%.0f hull=%.1f",
			st, s.H, s.V, s.T, s.Dmg.Hull)
	}
	if s.Dmg.Hull > 20 {
		t.Errorf("autoland took %.1f%% hull damage; want <= 20%%", s.Dmg.Hull)
	}
	if s.Li <= 0 {
		t.Errorf("autoland burned the whole lithium tank")
	}
	sc := s.Score()
	if sc.CrossKm > 10 {
		t.Errorf("autoland landed %.1f km off the pad line; want < 10", sc.CrossKm)
	}
}

// Full stick-forward with no computer must punish: either the ship dies or
// the repair bill is ruinous.
func TestSteepDiveBurns(t *testing.T) {
	s := New(Yodacon(), EarthProfile(), 2)
	st := runToEnd(s, Controls{Pitch: -1}, 2600)
	if st == SkippedOut {
		t.Fatalf("full-down entry skipped out; corridor is inverted")
	}
	if st == Landed && s.Dmg.Hull < 30 {
		t.Errorf("diving the corridor cost only %.1f%% hull; the burn boundary is toothless", s.Dmg.Hull)
	}
}

// Full stick-back early must skip off the atmosphere.
func TestShallowSkipsOut(t *testing.T) {
	s := New(Yodacon(), EarthProfile(), 3)
	if st := runToEnd(s, Controls{Pitch: 1}, 2600); st != SkippedOut {
		t.Fatalf("full-up entry ended %v (h=%.0f gam=%.2f); want SkippedOut",
			st, s.H, s.Gamma*180/math.Pi)
	}
}

// The pillow must actually shield: with seed and coil, peak heat flux sits
// well below the bare value at the same point.
func TestShieldReducesHeatFlux(t *testing.T) {
	veh := Yodacon()
	p := stateAt(61000, 6400, veh, EarthProfile(), veh.CoilField, 0.024)
	if p.QShielded >= p.QBare {
		t.Fatalf("shielded flux %.0f >= bare %.0f", p.QShielded, p.QBare)
	}
	if p.QShielded > 0.85*p.QBare {
		t.Errorf("shield only cut flux to %.2f of bare; the pillow is flat",
			p.QShielded/p.QBare)
	}
	if p.InteractionQ <= 0 {
		t.Errorf("no MHD authority at mid-entry: Q=%v", p.InteractionQ)
	}
}

// The corridor narrows on the way down; the game gets harder as you descend.
func TestCorridorNarrows(t *testing.T) {
	s := New(Yodacon(), EarthProfile(), 4)
	if s.CorridorWidth(15000) >= s.CorridorWidth(122000) {
		t.Fatalf("corridor does not narrow: %.3f at 15 km vs %.3f at 122 km",
			s.CorridorWidth(15000), s.CorridorWidth(122000))
	}
}

// Same seed, same controls, same outcome — the sim must be deterministic.
func TestDeterminism(t *testing.T) {
	a, b := New(Yodacon(), EarthProfile(), 7), New(Yodacon(), EarthProfile(), 7)
	c := Controls{Auto: true}
	for i := 0; i < 2000 && a.Status() == Flying; i++ {
		a.Step(0.2, c)
		b.Step(0.2, c)
	}
	if a.H != b.H || a.V != b.V || a.Dmg != b.Dmg {
		t.Fatalf("diverged: %v/%v vs %v/%v", a.H, a.Dmg, b.H, b.Dmg)
	}
}

// A failed computer refuses the frame: Auto must not fly when Dmg.Computer
// is past the failure line.
func TestFailedComputerHandsBack(t *testing.T) {
	s := New(Yodacon(), EarthProfile(), 8)
	s.Dmg.Computer = 80
	out := s.auto.fly(s, Controls{Auto: true, Pitch: 0.5}, 0.2)
	if out.Auto {
		t.Fatalf("failed computer still armed")
	}
	if out.Pitch != 0.5 {
		t.Fatalf("failed computer overrode the pilot's stick")
	}
}

// The exporter hands out profiles across these ranges; the flight computer
// must bring the ship down on all of them without ruinous damage, or the
// galaxy is full of unlandable worlds.
func TestAutolandAcrossProfiles(t *testing.T) {
	for _, atmos := range []float64{0.8, 1.0, 1.2} {
		for _, grav := range []float64{0.85, 1.0, 1.15} {
			prof := Profile{AtmosScale: atmos, GravityScale: grav,
				CorridorHalfWidth: 0.30}
			s := New(Yodacon(), prof, 5)
			st := runToEnd(s, Controls{Auto: true}, 2600)
			if st != Landed || s.Dmg.Hull > 35 {
				t.Errorf("atmos %.2f grav %.2f: %v hull %.1f%% (t=%.0f h=%.0f v=%.0f)",
					atmos, grav, st, s.Dmg.Hull, s.T, s.H, s.V)
			}
		}
	}
}

// The in-game step is dt=0.3 (60 fps at 18x time — the entry is not flown
// in real time): the flight computer must fly the checkride cleanly at
// that rate too, across seeds.
func TestAutolandNominalAtGameRate(t *testing.T) {
	for seed := int64(1); seed <= 6; seed++ {
		s := New(Yodacon(), EarthProfile(), seed)
		st := runToEndDt(s, Controls{Auto: true}, 2600, 0.3)
		if st != Landed || s.Dmg.Hull > 20 {
			t.Errorf("seed %d: %v hull %.1f%% computer %.0f%% (t=%.0f h=%.0f)",
				seed, st, s.Dmg.Hull, s.Dmg.Computer, s.T, s.H)
		}
	}
}

// A hold stuffed to the 120% clamp limit must still be landable on the
// computer — heavier, hotter, harder, but never a death sentence. The
// same flight must also cost more RCS than the empty ship's: the weight
// factor makes every correction dearer, which is the overstuff tradeoff.
func TestOverstuffedStillLandsAndPaysForIt(t *testing.T) {
	heavy := Yodacon()
	heavy.Mass += 120e3 // 120 t of cargo, RefMass stays at design
	hs := New(heavy, EarthProfile(), 1)
	st := runToEnd(hs, Controls{Auto: true}, 2600)
	if st != Landed || hs.Dmg.Hull > 40 {
		t.Fatalf("overstuffed: %v hull %.1f%% (t=%.0f h=%.0f v=%.0f)",
			st, hs.Dmg.Hull, hs.T, hs.H, hs.V)
	}
	ls := New(Yodacon(), EarthProfile(), 1)
	runToEnd(ls, Controls{Auto: true}, 2600)
	if hs.RCS >= ls.RCS {
		t.Errorf("full weight must burn more RCS: heavy %.1f kg left, light %.1f",
			hs.RCS, ls.RCS)
	}
	if heavy.WeightFactor() < 1.3 {
		t.Errorf("weight factor should read the overstuff: %.2f", heavy.WeightFactor())
	}
}
