package fx

import "testing"

func TestFireBurnsAndDecays(t *testing.T) {
	f := NewFire(32, 12, 1)
	f.Step(2)
	front, tip := 0.0, 0.0
	for i := 0; i < f.Cols; i++ {
		front += f.Cell(i, 0)
		tip += f.Cell(i, f.Rows-1)
	}
	if front <= 0 {
		t.Fatal("front row never ignited")
	}
	if tip >= front {
		t.Fatalf("flame must decay outward: front %.2f tip %.2f", front, tip)
	}
	f.Fuel = 0
	f.Step(4)
	for i := 0; i < f.Cols; i++ {
		if f.Cell(i, f.Rows-1) > 0.1 {
			t.Fatal("flame should die once the fuel is cut")
		}
	}
}

func TestSweepLeansTheFlame(t *testing.T) {
	f := NewFire(48, 12, 2)
	f.Sweep = 3
	f.FuelProfile = func(u float64) float64 { // burn only the center
		if u > -0.2 && u < 0.2 {
			return 1
		}
		return 0
	}
	f.Step(2)
	left, right := 0.0, 0.0
	deep := f.Rows - 2
	for i := 0; i < f.Cols/2; i++ {
		left += f.Cell(i, deep)
		right += f.Cell(f.Cols-1-i, deep)
	}
	if right <= left {
		t.Fatalf("positive sweep must lean the deep flame right: L %.3f R %.3f", left, right)
	}
}

func TestDeterministicForSeed(t *testing.T) {
	a, b := NewFire(24, 8, 9), NewFire(24, 8, 9)
	a.Step(1.5)
	b.Step(1.5)
	for i := range a.cells {
		if a.cells[i] != b.cells[i] {
			t.Fatal("same seed must burn the same flame")
		}
	}
}
