package scene

import (
	"fmt"
	"testing"

	"yodacon.org/gonex/internal/ai"
	"yodacon.org/gonex/internal/world"
)

// The war has to keep running. Both ways it has failed so far were invisible
// to every unit test and obvious within a minute of simulated battle: a
// capacitor livelock that left the whole fleet gun-cold, and a state machine
// that latched every pilot into a permanent hold over its own pad. Neither
// crashed, neither logged, both simply stopped the game. So the fleet gets
// flown, and what is asserted is that it is still fighting at the end.
func TestTheWarSustainsItself(t *testing.T) {
	for _, seed := range []int64{1, 7, 99, 12345} {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) { warRuns(t, seed) })
	}
}

func warRuns(t *testing.T, seed int64) {
	w := loadSeed(t, "deathmatch.xml", seed)
	w.Notify = func(string, ...any) {}

	const (
		dt   = 1.0 / 60
		half = 450 // seconds
	)
	step := func(secs int) {
		for i := 0; i < secs*60; i++ {
			w.Update(dt)
		}
	}
	kills := func() int {
		n := 0
		for _, s := range w.Scores {
			n += s
		}
		return n
	}

	step(half)
	early := kills()
	if early == 0 {
		t.Fatal("nobody died in the first half — the fleet never engaged")
	}
	step(half)
	if late := kills() - early; late == 0 {
		t.Errorf("%d kills in the first %ds and none in the second: the war stalled",
			early, half)
	}

	// Nobody is stranded: a pilot in a holding pattern with a full magazine
	// and a whole hull is the signature of a latched state machine.
	var flying, resupplied int
	for _, e := range w.Entities {
		s, ok := e.(*world.Ship)
		if !ok {
			continue
		}
		if !s.Docked() {
			flying++
		}
		if s.Rounds > s.RoundsMax/2 {
			resupplied++
		}
		// Being in RTB proves nothing on its own — a pilot with a full
		// magazine and a flat battery is correctly on its way home. What
		// must never happen is a pilot that has been trying to get home for
		// minutes on end, which is what a latched state machine looks like
		// from outside.
		if d := s.Controller.(*ai.Doctrine); d.Held() > 180 {
			t.Errorf("%s has been trying to get home for %.0fs", s.Name, d.Held())
		}
	}
	if flying == 0 {
		t.Error("the entire fleet is on the ground")
	}
	if resupplied == 0 {
		t.Error("no pilot is carrying a working magazine — resupply never happened")
	}

	// The war costs the planets something. If industry never draws down,
	// nothing is being paid for and territory can never be starved.
	var ip, ipMax float64
	for _, e := range w.Entities {
		if p, ok := e.(*world.Planet); ok && p.Team != world.TeamNone {
			ip += p.IP
			ipMax += p.IPMax()
		}
	}
	if ip >= ipMax*0.98 {
		t.Errorf("industry sits at %.0f/%.0f — the war is being fought for free", ip, ipMax)
	}
}

// A planet with nothing left cannot put its squadron back in the air. This
// is the mechanism territory is meant to change hands by.
func TestAStarvedHoldingGroundsItsSquadron(t *testing.T) {
	w := load(t, "deathmatch.xml")
	w.Notify = func(string, ...any) {}

	for _, e := range w.Entities {
		if p, ok := e.(*world.Planet); ok && p.Team == world.TeamRed {
			p.IP, p.Credits = 0, 0
		}
	}
	for _, e := range w.Entities {
		if s, ok := e.(*world.Ship); ok && s.Team == world.TeamRed {
			s.Rounds = 0 // the squadron comes home dry
		}
	}
	for i := 0; i < 60*120; i++ {
		w.Update(1.0 / 60)
		for _, e := range w.Entities {
			if p, ok := e.(*world.Planet); ok && p.Team == world.TeamRed {
				p.IP, p.Credits = 0, 0 // hold the siege
			}
		}
	}

	// A replacement hull still arrives with its starter quarter-magazine —
	// that comes out of the yard, not the pad. What a bankrupt holding must
	// not be able to do is put a round beyond that into anybody.
	armed, deaths := 0, 0
	for _, e := range w.Entities {
		s, ok := e.(*world.Ship)
		if !ok || s.Team != world.TeamRed {
			continue
		}
		deaths += s.Deaths
		if s.Rounds > s.RoundsMax/4 {
			armed++
			t.Errorf("%s carries %d/%d rounds off a holding with no capacity and no treasury",
				s.Name, s.Rounds, s.RoundsMax)
		}
	}
	if armed == 0 && deaths == 0 {
		t.Log("note: no Red losses in this window, so no respawn magazines either")
	}
}
