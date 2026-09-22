package reentry

import "testing"

// Each rung of the confinement ladder answers ONE failure mode of the
// magnetopause, and the tests are written that way on purpose: an outfit
// that improved everything a little would be a difficulty slider with a
// physics costume on.

// the point in the corridor where the shield is doing real work
func hotPoint(v Vehicle) Point { return stateAt(61000, 6400, v, EarthProfile(), v.Conf.Field(v.CoilField), 0.024) }

// 1. The field is too weak. Stand-off goes as the CUBE ROOT of field, and
// nothing anybody sells changes that exponent — which is exactly why the
// other three rungs exist.
func TestCoilBoostObeysTheSixthRoot(t *testing.T) {
	base := Yodacon()
	up := Yodacon()
	up.Conf.CoilBoost = base.CoilField // double the field
	a, b := hotPoint(base), hotPoint(up)
	if b.Standoff <= a.Standoff {
		t.Fatalf("doubling the field did not push the pillow out: %.3f → %.3f", a.Standoff, b.Standoff)
	}
	// Twice the field is four times the magnetic pressure is 4^(1/6) ≈ 1.26
	// on the radius above the nose. Generous bounds — the gate moves too —
	// but it must not behave like a linear gain.
	if ratio := (b.Standoff - 1) / (a.Standoff - 1); ratio > 2.2 {
		t.Errorf("stand-off grew %.2fx for 2x the field; the sixth root is gone", ratio)
	}
	if b.QShielded >= a.QShielded {
		t.Error("a bigger pillow did not cut the heat flux")
	}
}

// 2. The bottle leaks at the poles. Sealing the cusps is worth far more in
// what it stops leaking than in what it stands off.
func TestCuspSealIsWorthMoreInDragThanInStandoff(t *testing.T) {
	base := Yodacon()
	up := Yodacon()
	up.Conf.CuspSeal = 1
	a, b := hotPoint(base), hotPoint(up)
	if b.Standoff <= a.Standoff {
		t.Error("plugging the cusps did not raise the usable magnetic pressure")
	}
	if b.DragFactor >= a.DragFactor {
		t.Fatalf("a sealed bottle leaked as much wake as an open one: %.3f → %.3f", a.DragFactor, b.DragFactor)
	}
	standoffGain := (b.Standoff - a.Standoff) / (a.Standoff - 1)
	dragRelief := (a.DragFactor - b.DragFactor) / (a.DragFactor - 1)
	if dragRelief <= standoffGain {
		t.Errorf("cusp seal bought %.0f%% stand-off and only %.0f%% drag relief; the sixth root says it must be the other way round",
			standoffGain*100, dragRelief*100)
	}
}

// 3. The pillow is symmetric. A driven array buys authority where a passive
// coil has none, and pays for it on the power ledger.
func TestSteeringArrayBuysAuthorityAndCostsPower(t *testing.T) {
	base := Yodacon()
	up := Yodacon()
	up.Conf.ArrayGain = 1
	a, b := hotPoint(base), hotPoint(up)
	if b.InteractionQ <= a.InteractionQ || b.Gate <= a.Gate {
		t.Error("driving current did not raise the interaction parameter")
	}
	if b.PowerDraw <= a.PowerDraw {
		t.Error("the array steers for free")
	}
	if up.CommandedLD() <= base.CommandedLD() {
		t.Errorf("leaning the envelope bought no lift: %.3f → %.3f", base.CommandedLD(), up.CommandedLD())
	}
}

// 4. The air is too thin to grip. This is the rung that works where the
// magnetic ones do not, so it is tested HIGH UP, where the continuum gate
// has closed and more field would buy nothing at all.
func TestInjectionRingWorksWhereFieldDoesNot(t *testing.T) {
	const thin = 88000.0 // well above the continuum gate
	speed := 7200.0
	stock := Yodacon()
	morefield := Yodacon()
	morefield.Conf.CoilBoost = 2.4 // triple the coil
	ring := Yodacon()
	ring.Conf.RingFeed = 0.011

	at := func(v Vehicle) Point { return stateAt(thin, speed, v, EarthProfile(), v.Conf.Field(v.CoilField), 0.024) }
	s, f, r := at(stock), at(morefield), at(ring)

	if r.Kn >= s.Kn {
		t.Fatal("the seed ring did not collapse the local Knudsen number")
	}
	if r.Gate <= s.Gate {
		t.Fatalf("the ring bought no continuum: gate %.4g → %.4g", s.Gate, r.Gate)
	}
	// The whole argument for the ring is that it beats brute field up here.
	if r.Gate <= f.Gate {
		t.Errorf("tripling the coil (gate %.4g) beat the injection ring (gate %.4g) in the rarefied phase — "+
			"then the ring has no reason to be on the shelf", f.Gate, r.Gate)
	}
	if r.PowerDraw <= s.PowerDraw {
		t.Error("accelerating mass ahead of the nose costs nothing")
	}
}

// A stock hull is untouched: the zero-value Confinement must change no
// number anywhere, or every landing in every save just moved.
func TestStockHullIsUnchanged(t *testing.T) {
	v := Yodacon()
	if v.Conf.Fitted() {
		t.Fatal("a stock hull reports confinement hardware")
	}
	p := hotPoint(v)
	if v.Conf.PressureGain() != 1 || v.Conf.LeakRelief() != 1 || v.Conf.DrivenQ() != 1 ||
		v.Conf.KnudsenRelief() != 1 || v.Conf.CryoLoad() != 1 || v.Conf.Watts(p.Gate) != 0 {
		t.Error("the zero value is not neutral")
	}
	if v.CommandedLD() != v.LDMax {
		t.Error("a stock airframe commands more lift than its own maximum")
	}
}
