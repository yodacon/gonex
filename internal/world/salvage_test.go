package world

import (
	"math"
	"testing"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/ship"
)

func salvageWorld(t *testing.T) *World {
	t.Helper()
	cat, err := ship.LoadCatalog()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return New(cat, 7)
}

// A drop within range joins the pile; the field's total is the sum of what
// went in, whatever merging happened.
func TestDebrisMergesAndConserves(t *testing.T) {
	w := salvageWorld(t)
	a := w.DropDebris(gmath.V(1000, 1000), gmath.Vec2{}, []int{10, 0, 0, 0, 0, 0}, 50, 3)
	b := w.DropDebris(gmath.V(1100, 1000), gmath.Vec2{}, []int{0, 5, 0, 0, 0, 0}, 20, 0)
	if a != b {
		t.Fatal("a drop 100 units away started a second pile")
	}
	if got := a.Tons(); math.Abs(got-88) > 1e-9 {
		t.Errorf("pile holds %.1f t, want 88", got)
	}
	// Past the cap, the two nearest merge and nothing is lost.
	var total float64 = 88
	for i := 0; i < maxDebris+10; i++ {
		w.DropDebris(gmath.V(float64(i)*500+3000, 5000), gmath.Vec2{}, []int{1, 0, 0, 0, 0, 0}, 1, 0)
		total += 2
	}
	var sum float64
	for _, d := range w.Salvage() {
		sum += d.Tons()
	}
	if math.Abs(sum-total) > 1e-9 {
		t.Errorf("field holds %.1f t after merging, want %.1f", sum, total)
	}
	if n := len(w.Salvage()); n > maxDebris {
		t.Errorf("%d piles, cap is %d", n, maxDebris)
	}
}

// A passing ship with room lifts cargo to the hold and scrap to the junk
// bay, up to its free deck, and the pile keeps the rest.
func TestShipsScoopWreckage(t *testing.T) {
	w := salvageWorld(t)
	s := w.NewShip(1, TeamRed, "scooper", KindNPC)
	s.Role = RoleHauler
	s.Outfit(w)
	s.P = gmath.V(2000, 2000)
	free := s.HoldFree()
	if free < 10 {
		t.Skipf("hull too small to test with (%.0f t free)", free)
	}
	d := w.DropDebris(gmath.V(2010, 2000), gmath.Vec2{}, []int{int(free) + 50, 0, 0, 0, 0, 0}, 30, 4)
	before := d.Tons() + s.HoldTons()
	d.Update(w, 0.1)
	if s.HoldTons() <= 0 {
		t.Fatal("the ship lifted nothing")
	}
	if s.HoldFree() > 1 {
		t.Errorf("%.1f t of deck still free beside a pile it could not empty", s.HoldFree())
	}
	if got := d.Tons() + s.HoldTons(); math.Abs(got-before) > 1e-9 {
		t.Errorf("tons went from %.1f to %.1f across a scoop", before, got)
	}
	if d.Other != 4 {
		t.Errorf("the unnameable %.0f t should stay in the pile; %.0f left", 4.0, d.Other)
	}
}
