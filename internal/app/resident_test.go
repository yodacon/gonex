package app

import (
	"testing"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/ship"
	"yodacon.org/gonex/internal/traffic"
	"yodacon.org/gonex/internal/universe"
	"yodacon.org/gonex/internal/world"
)

// The handover is the one place in the game where a ton of cargo exists in
// two type systems at once, and it is therefore the one place a ton can end
// up existing twice. These tests build the smallest App that can hold the
// seam — a universe, a world, and nothing else — and check the books across
// it. No graphics context is needed because none of this draws.

func handoverFixture(t *testing.T) (*App, *universe.World) {
	t.Helper()
	catalog, err := ship.LoadCatalog()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	w := world.New(catalog, 1)
	w.MapW, w.MapH = 10000, 10000

	a := &App{World: w}
	a.Catalog = catalog
	a.uni = universe.New(4242, []universe.Port{
		{Stellar: 133, Name: "ConEx", System: 133, Pop: 4_000_000, Govt: govt.Red},
		{Stellar: 238, Name: "Exeon", System: 238, Pop: 4_000_000, Govt: govt.Green},
	}, 4)
	a.uni.Account(a.playerHold)
	a.uni.Account(a.residentHolds)
	w.OnKill = a.loseResident

	// A planet for the hull to be materialised beside.
	pl := &world.Planet{StellarID: 133, Name: "ConEx", Team: world.TeamRed}
	pl.P = pl.P.Add(pl.P) // origin is fine; position is not under test
	pl.Setup(4_000_000)
	w.Add(pl)

	// A world's warehouse holds only CRUST at genesis — ore, rations and the
	// rest are made, not dug, and on day zero nothing has been made yet. The
	// scenarios below trade in finished goods, so the shelves are stocked by
	// hand and the books re-based on the result: this state IS the starting
	// state, which is the one case ReopenBooks exists for.
	uw := a.uni.Worlds[133]
	uw.Warehouse.Add(econ.Ore, 1000)
	uw.Reprice()
	a.uni.ReopenBooks()

	if bad := a.uni.Audit(); len(bad) > 0 {
		t.Fatalf("fixture starts unbalanced: %v", bad[0])
	}
	return a, uw
}

// setStock puts a port's shelf at an exact figure and re-bases the books on
// it, so a scenario can say "this world has thirty tons" without the
// adjustment itself reading as thirty tons appearing from nowhere.
func setStock(a *App, uw *universe.World, m econ.Material, tons float64) {
	uw.Warehouse[m] = tons
	uw.Reprice()
	a.uni.ReopenBooks()
}

// A cargo crossing the seam must exist ONCE. The failure this guards against
// is the obvious implementation — copy the manifest into the ship and leave
// it in the census row too — which doubles every ton at the moment a hull
// becomes visible and halves it again on the way out.
func TestHandoverDoesNotDuplicateCargo(t *testing.T) {
	a, uw := handoverFixture(t)
	h := a.uni.Fleet.Hulls[0]
	h.Home, h.From, h.To = 133, 133, 133
	h.Status = traffic.Idle
	econ.Transfer(&uw.Warehouse, &h.Cargo, econ.Ore, 60)

	before := a.uni.Fleet.CargoAfloat().Total()
	if before < 60 {
		t.Fatalf("fixture hull is carrying %.1f t, wanted 60", before)
	}
	if bad := a.uni.Audit(); len(bad) > 0 {
		t.Fatalf("unbalanced after loading: %v", bad[0])
	}

	a.residentsIn(133)
	if h.Status != traffic.Resident {
		t.Fatalf("hull did not become resident (status %v)", h.Status)
	}
	if bad := a.uni.Audit(); len(bad) > 0 {
		t.Errorf("unbalanced after materialising: %v", bad[0])
	}
	// The census row must be EMPTY of what the ship is now carrying.
	if got := h.Cargo[econ.Ore]; got >= 1 {
		t.Errorf("census row still holds %.1f t of ore the ship is carrying", got)
	}
	if a.residentHolds()[econ.Ore] < 59 {
		t.Errorf("the ship is carrying %.1f t, expected ~60", a.residentHolds()[econ.Ore])
	}

	// And back again.
	a.releaseResidents()
	if h.Status != traffic.Idle {
		t.Errorf("hull did not return to the census (status %v)", h.Status)
	}
	if bad := a.uni.Audit(); len(bad) > 0 {
		t.Errorf("unbalanced after release: %v", bad[0])
	}
	if got := h.Cargo[econ.Ore]; got < 59 {
		t.Errorf("only %.1f t came back into the census row", got)
	}
	if got := a.residentHolds().Total(); got > 1e-9 {
		t.Errorf("%.3f t left in a ship's hold after release", got)
	}
}

// A resident dying enters the books as a loss, with its cargo scattered —
// not silently deleted, and not left floating in a hold nobody owns.
func TestKillingAResidentEntersTheBooks(t *testing.T) {
	a, uw := handoverFixture(t)
	h := a.uni.Fleet.Hulls[0]
	h.Home, h.From, h.To = 133, 133, 133
	h.Status = traffic.Idle
	econ.Transfer(&uw.Warehouse, &h.Cargo, econ.Ore, 50)
	a.residentsIn(133)

	var victim *world.Ship
	for _, e := range a.World.Entities {
		if s, ok := e.(*world.Ship); ok && s.HullID == h.ID {
			victim = s
			break
		}
	}
	if victim == nil {
		t.Fatal("no resident ship was created")
	}
	afloat := a.uni.Fleet.Afloat()

	a.World.OnKill(victim) // what die() calls

	if h.Status != traffic.Lost {
		t.Errorf("hull status is %v after being killed, want LOST", h.Status)
	}
	if got := a.uni.Fleet.Afloat(); got != afloat-1 {
		t.Errorf("%d hulls afloat, want %d", got, afloat-1)
	}
	if a.uni.Journal.LostHulls != 1 {
		t.Errorf("journal counted %d losses", a.uni.Journal.LostHulls)
	}
	if bad := a.uni.Audit(); len(bad) > 0 {
		t.Errorf("books unbalanced after a resident died: %v", bad[0])
	}
	// The wreck's cargo is in the sink, not in a hold and not nowhere.
	if a.uni.Sink[econ.Ore] < 49 {
		t.Errorf("only %.1f t of ore reached the sink", a.uni.Sink[econ.Ore])
	}
}

// The player's counter has to move real tons. Buying takes them out of a
// warehouse; selling puts them back; and a port that has none has none.
func TestTheCounterMovesRealTons(t *testing.T) {
	a, uw := handoverFixture(t)
	a.voy = newVoyage(1)
	a.voy.Credits = 1_000_000

	setStock(a, uw, econ.Ore, 30)

	got := a.buyTons(133, int(econ.Ore), 100)
	if got != 30 {
		t.Errorf("bought %d t from a warehouse holding 30", got)
	}
	if uw.Warehouse[econ.Ore] > 1e-9 {
		t.Errorf("%.3f t left in a warehouse that sold out", uw.Warehouse[econ.Ore])
	}
	if a.voy.Cargo[econ.Ore] != 30 {
		t.Errorf("%d t on the deck, want 30", a.voy.Cargo[econ.Ore])
	}
	if bad := a.uni.Audit(); len(bad) > 0 {
		t.Errorf("unbalanced after buying: %v", bad[0])
	}

	// Nothing more to sell: the counter refuses rather than inventing stock.
	if more := a.buyTons(133, int(econ.Ore), 10); more != 0 {
		t.Errorf("bought %d t from an empty warehouse", more)
	}

	// Selling supplies the port for real.
	sold := a.sellTons(133, int(econ.Ore), 12)
	if sold != 12 {
		t.Errorf("sold %d t, want 12", sold)
	}
	if uw.Warehouse[econ.Ore] < 12 {
		t.Errorf("the port has %.1f t after buying 12 back", uw.Warehouse[econ.Ore])
	}
	if bad := a.uni.Audit(); len(bad) > 0 {
		t.Errorf("unbalanced after selling: %v", bad[0])
	}
}

// T3: the planet standing in the sky and the port in the economy are ONE
// warehouse, and the pad's hull plates come out of it.
func TestPlanetStockMirrorsTheWarehouse(t *testing.T) {
	a, uw := handoverFixture(t)
	setStock(a, uw, econ.Ore, 777)
	a.syncPlanetStock()

	var pl *world.Planet
	for _, e := range a.World.Entities {
		if p, ok := e.(*world.Planet); ok && p.StellarID == 133 {
			pl = p
		}
	}
	if pl == nil {
		t.Fatal("no planet")
	}
	if pl.Stock[world.OreIndex] != 777 {
		t.Errorf("planet stock is %d, warehouse is 777", pl.Stock[world.OreIndex])
	}

	// A pad that burns plates debits the warehouse it drew them from.
	pl.PlateDraw = 100
	a.drainPlanetStock()
	if uw.Warehouse[econ.Ore] > 677.001 {
		t.Errorf("warehouse is %.1f after 100 t of plates were burned", uw.Warehouse[econ.Ore])
	}
	if pl.PlateDraw != 0 {
		t.Errorf("plate draw not cleared: %.1f", pl.PlateDraw)
	}
	if bad := a.uni.Audit(); len(bad) > 0 {
		t.Errorf("unbalanced after the yard burned plates: %v", bad[0])
	}
}
