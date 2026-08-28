package mission

import (
	"math/rand"
	"testing"
)

const conexStation = 133 // spöb 133, the ConEx home station

func mustLoad(t *testing.T) *Table {
	t.Helper()
	tb, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func def(t *testing.T, tb *Table, id int) Def {
	t.Helper()
	for _, d := range tb.Defs {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("mission %d not in table", id)
	return Def{}
}

func TestLoads36With1997Prose(t *testing.T) {
	tb := mustLoad(t)
	if len(tb.Defs) != 36 {
		t.Fatalf("got %d missions; want 36", len(tb.Defs))
	}
	if d := def(t, tb, 285); d.Brief == "" || d.Restoration == "" {
		t.Fatalf("285 = %+v; want the 1997 brief and the restoration flag", d)
	}
}

// The Academy ladder: only Flight Practice at a fresh bar; completing it
// unlocks exactly the next rung.
func TestLadderProgression(t *testing.T) {
	tb := mustLoad(t)
	var bits Bits
	rng := rand.New(rand.NewSource(1))

	offers := tb.OffersAt(conexStation, &bits, nil, rng)
	if len(offers) != 1 || offers[0].ID != 250 {
		t.Fatalf("fresh bar offers %v; want just 250 Flight Practice", ids(offers))
	}
	Complete(offers[0], &bits)
	offers = tb.OffersAt(conexStation, &bits, nil, rng)
	if len(offers) != 1 || offers[0].ID != 251 {
		t.Fatalf("after 250, bar offers %v; want just 251", ids(offers))
	}
	// and 250 must never come back: its own bit now blocks it
	for _, d := range offers {
		if d.ID == 250 {
			t.Fatal("Flight Practice offered again after completion")
		}
	}
}

// The Marks Logging chain end-to-end — including the restored finale that
// the stock 1997 data made unreachable.
func TestMarksLoggingChain(t *testing.T) {
	tb := mustLoad(t)
	var bits Bits
	rng := rand.New(rand.NewSource(2))
	confed := []int{129} // any Confederation stellar for the 10000 code

	// 282: ConEx bar → load at Maxwell's Purchase (138) → deliver Northstar (140)
	a := Accept(def(t, tb, 282), conexStation, confed, rng)
	if a.Dest() != 138 {
		t.Fatalf("282 first leg = %d; want Maxwell's Purchase 138", a.Dest())
	}
	if ev := a.Land(138); ev != PickedUp {
		t.Fatalf("landing at pickup = %v; want PickedUp", ev)
	}
	if ev := a.Land(140); ev != Completed {
		t.Fatalf("landing at Northstar = %v; want Completed", ev)
	}
	Complete(a.Def, &bits)

	// 283 at Northstar → Tabletop (147)
	if o := ids(tb.OffersAt(140, &bits, nil, rng)); len(o) != 1 || o[0] != 283 {
		t.Fatalf("Northstar bar offers %v; want 283", o)
	}
	b := Accept(def(t, tb, 283), 140, confed, rng)
	if ev := b.Land(147); ev != Completed {
		t.Fatalf("escort leg end = %v; want Completed", ev)
	}
	Complete(b.Def, &bits)

	// 284 at Tabletop → New Providence (154)
	c := Accept(def(t, tb, 284), 147, confed, rng)
	if ev := c.Land(154); ev != Completed {
		t.Fatalf("shadow leg end = %v; want Completed", ev)
	}
	Complete(c.Def, &bits)

	// 285 at New Japan (185): reachable only because of the restoration fix
	found := false
	for i := 0; i < 20 && !found; i++ { // 80% roll
		for _, d := range tb.OffersAt(185, &bits, nil, rng) {
			if d.ID == 285 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("restored finale 285 never offered at New Japan")
	}
	d := Accept(def(t, tb, 285), 185, confed, rng)
	if d.Dest() != 129 {
		t.Fatalf("285 destination = %d; want the rolled Confederation stellar", d.Dest())
	}
	if ev := d.PassDays(70); ev != Failed {
		t.Fatalf("blowing the 65-day limit = %v; want Failed", ev)
	}
}

func ids(defs []Def) []int {
	out := make([]int, len(defs))
	for i, d := range defs {
		out[i] = d.ID
	}
	return out
}
