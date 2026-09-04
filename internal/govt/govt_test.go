package govt

import (
	"math"
	"testing"
)

const eps = 1e-9

// The trifecta's whole claim is that no colour is stronger than another. That
// is not a feeling about play, it is a row sum, and it is checked here so a
// balance pass cannot quietly hand one side an extra tenth of a point.
func TestEveryColourCostsTheSame(t *testing.T) {
	for _, c := range Colors() {
		row := Row(c)
		var sum float64
		for _, v := range row {
			sum += v
		}
		if math.Abs(sum-RowTotal) > eps {
			t.Errorf("%s sums to %.4f, want %.2f — that colour is %+.2f stronger overall",
				c, sum, RowTotal, sum-RowTotal)
		}
	}
}

// And no axis may be globally inflated: "shields" has to mean the same amount
// of game across the whole table, so that buffing one colour's screens is
// exactly a nerf to the other two rather than a gift to everybody.
func TestEveryAxisIsZeroSumAcrossTheColours(t *testing.T) {
	for a := Axis(0); a < AxisCount; a++ {
		var sum float64
		for _, c := range Colors() {
			sum += Trait(c, a)
		}
		if math.Abs(sum-ColTotal) > eps {
			t.Errorf("axis %s sums to %.4f across the three colours, want %.2f",
				a, sum, ColTotal)
		}
	}
}

// Each colour must actually HAVE a character: lead on something, trail on
// something. A perfectly average colour passes both sum rules and is no fun.
func TestEveryColourLeadsAndTrailsSomething(t *testing.T) {
	for _, c := range Colors() {
		var leads, trails int
		for a := Axis(0); a < AxisCount; a++ {
			best, worst := true, true
			for _, other := range Colors() {
				if other == c {
					continue
				}
				if Trait(other, a) >= Trait(c, a) {
					best = false
				}
				if Trait(other, a) <= Trait(c, a) {
					worst = false
				}
			}
			if best {
				leads++
			}
			if worst {
				trails++
			}
		}
		if leads == 0 {
			t.Errorf("%s is best at nothing", c)
		}
		if trails == 0 {
			t.Errorf("%s is worst at nothing — it is strictly comfortable", c)
		}
	}
}

// The design intent, asserted so it survives a refactor: Red shoots, Green
// grows, Blue endures.
func TestTheColoursAreWhoTheySayTheyAre(t *testing.T) {
	leader := func(a Axis) Color {
		best := Red
		for _, c := range Colors() {
			if Trait(c, a) > Trait(best, a) {
				best = c
			}
		}
		return best
	}
	for _, tc := range []struct {
		axis Axis
		want Color
	}{
		{Gunnery, Red}, {Logistics, Red},
		{Growth, Green}, {Extraction, Green},
		{Industry, Blue}, {Shields, Blue},
	} {
		if got := leader(tc.axis); got != tc.want {
			t.Errorf("%s is led by %s, want %s", tc.axis, got, tc.want)
		}
	}
}

// Derived knobs must follow the table rather than being independent dials —
// otherwise there is a second place to hide a buff and the proof above is
// worthless.
func TestMinFleetFollowsLogistics(t *testing.T) {
	want := map[Color]int{Red: 3, Green: 4, Blue: 5}
	for c, n := range want {
		if got := MinFleet(c); got != n {
			t.Errorf("MinFleet(%s) = %d, want %d", c, got, n)
		}
	}
	// The ordering is the actual contract: better logistics, smaller viable
	// flight. If someone re-tunes Logistics, this catches an inversion.
	for _, a := range Colors() {
		for _, b := range Colors() {
			if Trait(a, Logistics) > Trait(b, Logistics) && MinFleet(a) > MinFleet(b) {
				t.Errorf("%s out-hauls %s but needs a bigger flight (%d vs %d)",
					a, b, MinFleet(a), MinFleet(b))
			}
		}
	}
}

func TestDerivedKnobsStayInRange(t *testing.T) {
	for _, c := range Colors() {
		if y := Yield(c); y <= 0 || y > 0.98 {
			t.Errorf("Yield(%s) = %.3f, out of range", c, y)
		}
		if s := ShieldFrac(c); s <= 0 || s > 0.85 {
			t.Errorf("ShieldFrac(%s) = %.3f, out of range", c, s)
		}
		if g := GrowthPerDay(c); g <= 0 || g > 0.05 {
			t.Errorf("GrowthPerDay(%s) = %.4f, out of range", c, g)
		}
		if m := MineRate(c); m <= 0 {
			t.Errorf("MineRate(%s) = %.2f", c, m)
		}
	}
	// A neutral power is the baseline every multiplier is measured against.
	for a := Axis(0); a < AxisCount; a++ {
		if Trait(None, a) != 1.0 {
			t.Errorf("neutral is not neutral on %s: %.2f", a, Trait(None, a))
		}
	}
}
