package universe

import (
	"testing"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
)

// The triad's invariants.
//
// These are not balance targets — balance is measured by the gazetteer rig
// and argued about with numbers. These are the STRUCTURAL promises the three
// body kinds make, each of which was broken at least once while building
// this and each of which is silent when it breaks.

func triadPorts() []Port {
	return Triad([]Port{
		{Stellar: 133, Name: "ConEx", System: 133, Pop: 4_000_000, Govt: govt.Red},
		{Stellar: 238, Name: "Exeon", System: 238, Pop: 4_000_000, Govt: govt.Green},
		{Stellar: 235, Name: "Cenron", System: 235, Pop: 4_000_000, Govt: govt.Blue},
		{Stellar: 301, Name: "Kestrel", System: 301, Pop: 1_200_000, Govt: govt.None},
	})
}

// THE POINT OF THE EXERCISE. The same-type rule says an intermediate moves
// within a system freely and crosses a jump only on a chartered lane. On the
// recovered gazetteer no two worlds shared a system, so that rule described
// a neighbourhood no world had, and copper could not move anywhere at all.
func TestEveryBodyHasANeighbourhood(t *testing.T) {
	u := New(11, triadPorts(), 4)
	for _, id := range u.Order() {
		w := u.Worlds[id]
		if n := u.LocalReach(w); n < 2 {
			t.Errorf("%s (%s) has %d in-system neighbours; the same-type rule needs some",
				w.Name, w.Kind, n)
		}
	}
	// And the three kinds are actually all present, in equal number.
	pl, st, fl := u.TriadReport()
	if pl != 4 || st != 4 || fl != 4 {
		t.Errorf("triad is %d planets, %d stations, %d fields", pl, st, fl)
	}
}

// Each kind is a different economic role, not three sets of scenery.
func TestTheThreeKindsAreDifferentThings(t *testing.T) {
	u := New(11, triadPorts(), 4)
	for _, id := range u.Order() {
		w := u.Worlds[id]
		switch w.Kind {
		case BodyStation:
			if w.Reserve.Total() > 0 {
				t.Errorf("%s is an orbital with %.0f t of crust under it", w.Name, w.Reserve.Total())
			}
			if w.Pop <= 0 {
				t.Errorf("%s is an orbital with nobody on it", w.Name)
			}
			// A station is a factory with no mine: its industry must be
			// mandated, because Rank has no crust to work from.
			if len(w.Plant) == 0 {
				t.Errorf("%s stood up no industry at all; a station is a factory", w.Name)
			}
			// It may well want CRUST — a smelter in orbit is fed by the
			// rock next door, and that shuttle hop is the trade the triad
			// exists to create. What it must not have is any of its own.
		case BodyField:
			if w.Pop != 0 {
				t.Errorf("%s is a rock with %d people on it", w.Name, w.Pop)
			}
			if w.Reserve.Total() <= 0 {
				t.Errorf("%s is a field with nothing in it", w.Name)
			}
			if w.PopCeiling() != 0 {
				t.Errorf("%s can be settled; nothing lives on a rock", w.Name)
			}
			// A clean field has no industry. A HOT one keeps its mandated
			// refinery — that is the licensed site the fuel trade needs.
			if len(w.Mandate) == 0 && len(w.Plant) > 0 {
				t.Errorf("%s ranked %d chains for itself; a field is a source, not a competitor",
					w.Name, len(w.Plant))
			}
		}
	}
}

// The rule that makes a three-body system worth having: the rock next door
// holds what the planet does not. Without it a system still cannot close a
// chain and the triad is decoration.
func TestTheFieldFillsThePlanetsHoles(t *testing.T) {
	filled, holes := 0, 0
	for _, planet := range []int{133, 238, 235, 301, 128, 140, 155} {
		host := econ.Endow(11, planet, 3_000_000, 42)
		field := econ.Complement(11, FieldOf(planet), planet, 3_000_000, 42)
		for _, m := range econ.Crusts() {
			if m == econ.Spodumene || host.Reserve[m] > 0 {
				continue
			}
			holes++
			if field.Reserve[m] > 0 {
				filled++
			}
		}
	}
	if holes == 0 {
		t.Skip("no barren materials in this sample")
	}
	if filled < holes {
		t.Errorf("the field covered %d of its planet's %d holes; it must cover all of them", filled, holes)
	}
}

// A field digs against a stockpile, not against appetite it does not have.
// Uncapped, the fields buried 2.9 megatonnes of finite reserve in heaps
// nobody had asked for inside one simulated year — and the reserve is the
// only finite thing in this economy.
func TestFieldsDoNotStripMineIntoAHeap(t *testing.T) {
	// A field ALONE, so this measures the digging rule and not what passing
	// couriers chose to unload here.
	u := New(11, []Port{{Stellar: FieldOf(133), Name: "Belt", System: 133,
		Pop: 0, Govt: govt.None, Kind: BodyField, Host: 133}}, 0)
	for d := 0; d < 400; d++ {
		u.Tick()
	}
	dug := false
	for _, id := range u.Order() {
		w := u.Worlds[id]
		if w.Kind != BodyField {
			continue
		}
		dug = dug || w.Warehouse.Total() > 0
		for _, m := range econ.Crusts() {
			if w.Warehouse[m] > fieldStockpile*2 {
				t.Errorf("%s has %.0f t of %s on the pad against a %.0f t stockpile",
					w.Name, w.Warehouse[m], m, fieldStockpile)
			}
		}
	}
	if !dug {
		t.Error("the field never lifted anything; a rock nobody can work is worth nothing")
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books on a triad map: %v", bad[0])
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("ledger on a triad map: %v", bad)
	}
}

// And the books still balance with all three kinds running together.
func TestATriadMapBalances(t *testing.T) {
	u := New(11, triadPorts(), 4)
	for d := 0; d < 400; d++ {
		u.Tick()
		if d%100 == 0 {
			if bad := u.Audit(); len(bad) > 0 {
				t.Fatalf("day %d: %v", u.Day, bad[0])
			}
		}
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("mass: %v", bad[0])
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("credits: %v", bad)
	}
	if u.Journal.Voyages == 0 {
		t.Error("nothing was ever delivered on a triad map")
	}
}

// Triad is idempotent: running it twice must not mint a second station.
func TestTriadIsIdempotent(t *testing.T) {
	once := Triad(triadPorts())
	if len(once) != len(triadPorts()) {
		t.Errorf("a second expansion grew the map from %d to %d ports", len(triadPorts()), len(once))
	}
}

// The capital stays on the ground. Capitals are picked by population, and a
// station carries a quarter of its planet's — if that ever inverted, a
// colour's seat of government would move into orbit and its arsenal with it.
func TestCapitalsStayOnPlanets(t *testing.T) {
	u := New(11, triadPorts(), 4)
	for _, c := range govt.Colors() {
		cap := u.Capital(c)
		if cap == nil {
			t.Errorf("%s has no capital", c)
			continue
		}
		if cap.Kind != BodyPlanet {
			t.Errorf("%s's capital is a %s (%s)", c, cap.Kind, cap.Name)
		}
	}
}
