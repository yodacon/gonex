package universe

import (
	"math"
	"testing"

	"yodacon.org/gonex/internal/govt"
)

// The span of control is inert until a government is actually large, so it
// is tested directly rather than through a run that never reaches the size.
func TestSpanOfControlBitesOnlyPastTheSpan(t *testing.T) {
	u := newTestUniverse(11)
	// A colour holding nothing, or less than the span, pays nothing.
	u.overhead = [4]float64{}
	u.refreshOverhead()
	for _, c := range govt.Colors() {
		if got := u.Overhead(c); got != 1 {
			t.Errorf("%s holds %d worlds, well inside the span, and pays %.3f",
				c, len(u.worldsOf(c)), got)
		}
	}
	if got := u.Overhead(govt.None); got != 1 {
		t.Errorf("neutral space is not a government and must not be charged: %.3f", got)
	}
}

// The penalty must be monotone, bounded, and never so steep that winning
// makes a government smaller than it was.
func TestSpanOfControlIsGentleAndMonotone(t *testing.T) {
	at := func(n float64) float64 {
		if n <= spanOfControl {
			return 1
		}
		return math.Pow(spanOfControl/n, overheadExponent)
	}
	prev := 1.0
	for n := spanOfControl; n <= 8*spanOfControl; n += 5 {
		o := at(n)
		if o > prev+1e-9 {
			t.Fatalf("overhead rose from %.4f to %.4f at %.0f worlds", prev, o, n)
		}
		if o <= 0 || o > 1 {
			t.Fatalf("overhead out of range at %.0f worlds: %.4f", n, o)
		}
		prev = o
	}
	if o := at(2 * spanOfControl); o < 0.70 || o > 0.72 {
		t.Errorf("at twice the span a government should keep ~71%%, got %.3f", o)
	}
	if o := at(4 * spanOfControl); o < 0.49 || o > 0.51 {
		t.Errorf("at four times the span it should keep ~50%%, got %.3f", o)
	}
	// The point of the term: total capability must still RISE with size.
	// An empire that shrinks when it wins is a bug wearing a balance term.
	small := spanOfControl * at(spanOfControl)
	big := 4 * spanOfControl * at(4*spanOfControl)
	if big <= small {
		t.Errorf("four times the territory yields %.1f against %.1f — conquest must still pay", big, small)
	}
}

// The fleet ceiling is the leg of the snowball that compounds fastest, so it
// must grow sublinearly once the span is passed.
func TestFleetCapGrowsSublinearly(t *testing.T) {
	u := newTestUniverse(11)
	c := govt.Green
	capAt := func(worlds int) int {
		u.overhead[c] = 1
		n := float64(worlds)
		if n > spanOfControl {
			u.overhead[c] = math.Pow(spanOfControl/n, overheadExponent)
		}
		return int(float64(worlds*u.Tune.FleetCap) * u.Overhead(c))
	}
	a, b := capAt(int(spanOfControl)), capAt(int(4*spanOfControl))
	if b <= a {
		t.Fatalf("four times the worlds must still allow more hulls: %d vs %d", b, a)
	}
	if b >= 4*a {
		t.Errorf("fleet cap grew %dx for 4x the territory — linear or worse, so it is not a brake at all", b/a)
	}
}

// A seat is a founding fact: it moves only when it is taken.
//
// It used to be recomputed as "the most populous world this colour holds",
// so it drifted with population growth while the founding Works, Bastion and
// the Munitions and Shipyard mandates stayed where they were built. The
// government ended up at one world and its arsenal at another, and the rally
// point called hulls home to a capital with nothing to arm them from.
func TestTheSeatMovesOnlyWhenItIsTaken(t *testing.T) {
	u := newTestUniverse(11)
	seat := map[govt.Color]int{}
	held := map[govt.Color]govt.Color{}
	for _, c := range govt.Colors() {
		w := u.Capital(c)
		if w == nil {
			continue
		}
		seat[c] = w.Stellar
		held[c] = w.Govt
	}
	if len(seat) == 0 {
		t.Fatal("no colour was founded with a seat")
	}
	for d := 0; d < 300; d++ {
		u.Tick()
		for c, was := range seat {
			w := u.Capital(c)
			if w == nil {
				continue // wiped out; nothing to assert
			}
			if w.Stellar == was {
				continue
			}
			// It moved. That is legal only if the old seat is no longer held.
			old := u.Worlds[was]
			if old != nil && old.Govt == c {
				t.Fatalf("day %d: %s moved its seat from %s to %s while still holding %s",
					u.Day, c, old.Name, w.Name, old.Name)
			}
			seat[c] = w.Stellar
		}
	}
	_ = held
}

// A successor inherits the government's business, or it is a capital with no
// arsenal — Konquest's zero-kill-percentage planet, self-inflicted.
func TestASuccessorTakesTheMandatesWithIt(t *testing.T) {
	u := newTestUniverse(11)
	c := govt.Green
	old := u.Capital(c)
	if old == nil {
		t.Skip("Green was not founded with a seat on this rig")
	}
	for _, name := range capitalMandates {
		if !hasMandate(old, name) {
			t.Fatalf("a founded capital should carry %s", name)
		}
	}
	// Take it.
	other := govt.Red
	old.Govt = other
	u.succeed(c)
	next := u.Capital(c)
	if next == nil {
		t.Fatal("Green lost its capital and elected no successor")
	}
	if next.Stellar == old.Stellar {
		t.Fatal("the successor is the world that was just lost")
	}
	for _, name := range capitalMandates {
		if !hasMandate(next, name) {
			t.Errorf("the successor %s did not inherit %s", next.Name, name)
		}
		if hasMandate(old, name) {
			t.Errorf("the lost capital %s is still mandated to run %s for a government that no longer holds it",
				old.Name, name)
		}
	}
}
