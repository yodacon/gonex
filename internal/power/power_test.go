package power

import (
	"math"
	"testing"
)

func TestCruiseRechargesStores(t *testing.T) {
	g := Stock()
	g.BattMJ, g.CapMJ = 100, 0
	for i := 0; i < 600; i++ { // ten minutes of quiet vacuum cruise
		g.Step(1, Load{Hotel: 0.3, Vacuum: true})
	}
	if g.CapMJ != g.CapCapMJ {
		t.Errorf("caps should refill first: %.1f / %.1f", g.CapMJ, g.CapCapMJ)
	}
	if g.BattMJ <= 100 {
		t.Errorf("battery should climb on surplus: %.1f", g.BattMJ)
	}
	if g.HeatMJ != 0 {
		t.Errorf("radiators in vacuum should hold heat at zero: %.1f", g.HeatMJ)
	}
}

func TestBrownout(t *testing.T) {
	g := Stock()
	g.BattMJ = 0
	f := g.Step(1, Load{Coil: 5, Hotel: 1}) // 6 MW asked, 3 MW reactor, dry battery
	if f.Served >= 1 {
		t.Fatalf("expected brownout, served=%.2f", f.Served)
	}
	if want := g.ReactorMW / 6; math.Abs(f.Served-want) > 1e-9 {
		t.Errorf("served=%.3f want %.3f", f.Served, want)
	}
}

func TestBatteryCarriesTheEntry(t *testing.T) {
	g := Stock()
	drained := false
	for i := 0; i < 900; i++ { // an entry: 4 MW coil, no radiators
		f := g.Step(1, Load{Coil: 4, Hotel: 0.2, HeatMW: 0.6})
		if f.FromBatt > 0 {
			drained = true
		}
	}
	if !drained {
		t.Error("a 4.2 MW entry on a 3 MW reactor must draw the battery")
	}
	if g.BattMJ >= 2600 {
		t.Errorf("battery should be spent down: %.0f", g.BattMJ)
	}
	if g.HeatMJ == 0 {
		t.Error("heat must build with the panels blind")
	}
}

func TestOverheat(t *testing.T) {
	g := Stock()
	g.HeatMJ = g.HeatCapMJ
	f := g.Step(10, Load{Coil: 4, HeatMW: 5})
	if f.Overheat <= 0 {
		t.Error("expected overheat past the ceiling")
	}
}

func TestSpendCap(t *testing.T) {
	g := Stock()
	g.CapMJ = 10
	if got := g.SpendCap(6); got != 1 {
		t.Errorf("full cover expected, got %.2f", got)
	}
	if got := g.SpendCap(8); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("half cover expected, got %.2f", got)
	}
	if g.CapMJ != 0 {
		t.Errorf("bank should be dry, has %.2f", g.CapMJ)
	}
}

func TestBuyBooksMass(t *testing.T) {
	g := Stock()
	for _, o := range Catalog() {
		g.Buy(o)
	}
	if g.OutfitKg <= 0 {
		t.Error("outfits must add mass")
	}
	if g.ReactorMW <= 3 || g.BattCapMJ <= 2600 || g.CapCapMJ <= 60 {
		t.Error("outfits must improve the plant")
	}
}
