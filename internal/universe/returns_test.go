package universe

import (
	"math"
	"testing"

	"yodacon.org/gonex/internal/econ"
)

// The two return paths are the only places in this economy where the flow
// goes back uphill, and both of them had been broken since they were
// written. These tests are here so that cannot happen again quietly.

// Wearing out a girder leaves junk on the pad, not nothing.
//
// Before this, the only source of scrap in the game was a hull destroyed in
// battle — and the war did not work, so the galaxy made zero tons of it in
// two simulated years while 218 breaker's yards sat idle.
func TestUsingUpDurablesLeavesScrap(t *testing.T) {
	u := newTestUniverse(11)
	w := u.Worlds[u.order[0]]
	w.Warehouse = econ.Stock{}
	w.Warehouse.Add(econ.Steel, 100)

	before := u.Sink.Total()
	got := u.eat(w, econ.Steel, 100)
	if got != 100 {
		t.Fatalf("the city ate %.1f t of the 100 on the pad", got)
	}
	if w.Warehouse[econ.Scrap] <= 0 {
		t.Fatal("a city wore out 100 t of steel and left no scrap at all")
	}
	// Mass is conserved: what did not become junk went to the sink.
	junk := w.Warehouse[econ.Scrap]
	sunk := u.Sink.Total() - before
	if d := junk + sunk - 100; d > 1e-9 || d < -1e-9 {
		t.Errorf("100 t in, %.3f t of junk and %.3f t sunk — %.3f t unaccounted", junk, sunk, d)
	}
	// And the design rule survives: food is renewable, steel is not. The
	// round trip through a breaker must lose.
	roundTrip := junkShare * 0.75 // breaker yield
	if roundTrip >= 1 {
		t.Errorf("a ton of steel returns %.2f t — steel has become renewable", roundTrip)
	}
}

// Fuel is burnt and leaves nothing. If fuel ever starts leaving scrap, a
// city can run a reactor and mine the ashes.
func TestBurntFuelLeavesNothing(t *testing.T) {
	u := newTestUniverse(11)
	w := u.Worlds[u.order[0]]
	w.Warehouse = econ.Stock{}
	w.Warehouse.Add(econ.Pellets, 50)
	u.eat(w, econ.Pellets, 50)
	if w.Warehouse[econ.Scrap] > 0 {
		t.Errorf("burning fuel left %.1f t of scrap", w.Warehouse[econ.Scrap])
	}
}

// The composter must keep up with the city it serves as that city GROWS.
//
// This is the fault that left 1.17 megatonnes of compost lying on worlds
// that owned a working composter: the return path was sized once at genesis
// against the founding population, and population is the one quantity in
// this game that grows continuously. The plant was not missing. It was the
// right size for a city that no longer existed.
func TestTheComposterGrowsWithItsCity(t *testing.T) {
	u := newTestUniverse(11)
	var w *World
	for _, id := range u.order {
		if x := u.Worlds[id]; x.Pop > 1_000_000 && x.Kind != BodyField {
			w = x
			break
		}
	}
	if w == nil {
		t.Skip("no populated world on this rig")
	}
	capacity := func() float64 {
		var t float64
		for _, p := range w.Civic {
			t += p.Demand()[econ.Compost]
		}
		return t
	}
	was := capacity()
	if was <= 0 {
		t.Fatal("a populated world was founded without a composter")
	}
	// Triple the city.
	w.Pop *= 3
	w.resizeCivic()
	now := capacity()
	if now <= was {
		t.Fatalf("the city tripled and its composter stayed at %.1f t/d", was)
	}
	// And it must be ahead of what the city actually sheds, or the surplus
	// piles up for ever.
	if now < w.organicAppetite() {
		t.Errorf("composter %.1f t/d against %.1f t/d of organic waste — it can never catch up",
			now, w.organicAppetite())
	}
}

// The headroom has to cover the appetite growth a full drift window
// produces, or the inflow outruns the plant for the whole window and the
// surplus never comes back.
func TestComposterHeadroomCoversTheDriftWindow(t *testing.T) {
	// Organic appetite grows faster than population, because medicine is on
	// the luxury exponent. The worst case across one drift window is that
	// growth, and the margin has to clear it.
	worst := math.Pow(1+civicDrift, luxuryExponent)
	if compostMargin <= worst {
		t.Errorf("margin %.2f does not cover %.2f of appetite growth across a %.0f%% drift window",
			compostMargin, worst, civicDrift*100)
	}
}
