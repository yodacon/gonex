package galaxy

import "testing"

func testGalaxy() *Galaxy {
	return &Galaxy{
		Systems: map[int]*System{
			1: {Name: "Sol", Govt: "Confederation"},
			2: {Name: "Rim", Govt: "Independent"},
		},
		Stellars: map[int]*Stellar{
			128: {Name: "Earth", System: 1, Source: "base"},
		},
	}
}

// The whole point of chartering is that the ID is derived, not handed out:
// a port has to answer to the same number after a re-entry and after a save
// that stored the pilot standing on its pad.
func TestCharterIsStableByName(t *testing.T) {
	g := testGalaxy()
	id := g.Charter("ConEx Yards", 1, 2, 900000)
	if id < CharterBase {
		t.Fatalf("chartered %d, below CharterBase %d", id, CharterBase)
	}
	if again := g.Charter("ConEx Yards", 1, 2, 900000); again != id {
		t.Errorf("re-charter gave %d, want %d", again, id)
	}
	// A fresh galaxy — what a reload builds — must mint the same number.
	if fresh := testGalaxy().Charter("ConEx Yards", 1, 2, 900000); fresh != id {
		t.Errorf("charter on a fresh galaxy gave %d, want %d", fresh, id)
	}
	if !g.Chartered(id) {
		t.Error("chartered port does not report as chartered")
	}
	if g.Chartered(128) {
		t.Error("Earth reports as chartered")
	}
}

func TestCharterDistinctNamesGetDistinctSlips(t *testing.T) {
	g := testGalaxy()
	seen := map[int]string{}
	for _, n := range []string{"ConEx Yards", "ConEx Deep", "ConEx Reach",
		"Midpoint", "Kestrel", "Lanner", "Merlin"} {
		id := g.Charter(n, 1, 3, 400000)
		if prev, dup := seen[id]; dup {
			t.Errorf("%q and %q both chartered as %d", prev, n, id)
		}
		seen[id] = n
	}
}

// A chartered port must be flyable. A zero atmosphere or a zero gravity
// scale does not read as "a plain little world" to the reentry sim, it reads
// as a corridor with no air and an orbit with no speed.
func TestCharterLandingProfileIsFlyable(t *testing.T) {
	g := testGalaxy()
	for _, pop := range []int{0, 150000, 400000, 900000, 4000000} {
		st := g.Stellars[g.Charter("World", 1, 5, pop)]
		delete(g.Stellars, g.Charter("World", 1, 5, pop)) // next pop re-charters
		switch {
		case st.Landing.AtmosScale < 0.8 || st.Landing.AtmosScale > 1.2:
			t.Errorf("pop %d: atmosScale %v out of the recovered range", pop, st.Landing.AtmosScale)
		case st.Landing.GravityScale < 0.85 || st.Landing.GravityScale > 1.15:
			t.Errorf("pop %d: gravityScale %v out of the recovered range", pop, st.Landing.GravityScale)
		case st.Landing.CorridorHalfWidth < 0.2 || st.Landing.CorridorHalfWidth > 0.6:
			t.Errorf("pop %d: corridor %v out of the recovered range", pop, st.Landing.CorridorHalfWidth)
		case st.Landing.PadBonus <= 0:
			t.Errorf("pop %d: pad bonus %d", pop, st.Landing.PadBonus)
		case st.Govt != "Confederation":
			t.Errorf("pop %d: govt %q, want the system's", pop, st.Govt)
		}
	}
}

// The deorbit cinematic picks its disc with `1 + sprite%18`. A chartered
// world has to fall out of the sky wearing the ball it wore in flight.
func TestCharterSpriteSurvivesTheDeorbitRoundTrip(t *testing.T) {
	g := testGalaxy()
	for spriteID := 1; spriteID <= 18; spriteID++ {
		st := g.Stellars[g.Charter(string(rune('a'+spriteID)), 1, spriteID, 500000)]
		if got := 1 + st.Sprite%18; got != spriteID {
			t.Errorf("sprite %d chartered to %d, deorbits as %d", spriteID, st.Sprite, got)
		}
	}
}

// A chartered port stands where the map put it. Filing it under a system's
// stellar list would make the arrival code grow a second copy of it in the
// ring of scenery.
func TestCharterDoesNotJoinTheSystemStellarList(t *testing.T) {
	g := testGalaxy()
	g.Charter("Midpoint", 1, 14, 1200000)
	if got := g.StellarsIn(1); len(got) != 0 {
		t.Errorf("system 1 lists %v after chartering", got)
	}
}

func TestRestationMovesOnlyCharteredPorts(t *testing.T) {
	g := testGalaxy()
	id := g.Charter("Midpoint", 1, 14, 1200000)
	g.Restation(id, 2)
	if st := g.Stellars[id]; st.System != 2 || st.Govt != "Independent" {
		t.Errorf("restationed to system %d govt %q, want 2/Independent", st.System, st.Govt)
	}
	g.Restation(128, 2)
	if g.Stellars[128].System != 1 {
		t.Error("Restation moved a recovered stellar")
	}
}
