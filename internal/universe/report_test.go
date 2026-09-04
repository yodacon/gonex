package universe

import (
	"os"
	"testing"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
)

// A simulation this size has to be READ to be believed, not just asserted on.
// The war-economy plan learned that the hard way: four bugs that no unit test
// would have found, all of which stopped the game dead without logging a
// thing. So this flies the universe for a year and prints what happened.
//
//	go test ./internal/universe -run TestUniverseReport -v
func TestUniverseReport(t *testing.T) {
	if os.Getenv("REPORT") == "" {
		t.Skip("set REPORT=1 to print the year-end trade report")
	}
	u := newTestUniverse(20260903)
	for d := 0; d < 365; d++ {
		u.Tick()
	}

	t.Log("=== THE MAP ===")
	for _, id := range u.Order() {
		for _, line := range u.Worlds[id].Describe() {
			t.Log(line)
		}
	}

	t.Log("=== THE TRIFECTA ===")
	for _, c := range govt.Colors() {
		t.Log(govt.Describe(c))
	}

	t.Log("=== THE FLEET ===")
	for _, line := range u.Fleet.Report() {
		t.Log(line)
	}
	t.Log("busiest hulls:")
	for _, h := range u.Fleet.Busiest(6) {
		t.Log("  " + h.Manifest(u.Fleet.ETA(h)))
	}

	t.Log("=== ROUTES ON THE BOARD TODAY ===")
	for _, c := range govt.Colors() {
		rs := u.FindRoutes(c, 3)
		for _, r := range rs {
			t.Logf("  %-5s %s", c, r)
		}
	}

	t.Log("=== THE BOOKS ===")
	var reserve, warehouse econ.Stock
	for _, id := range u.Order() {
		reserve = reserve.Plus(u.Worlds[id].Reserve)
		warehouse = warehouse.Plus(u.Worlds[id].Warehouse)
	}
	t.Logf("  genesis      %14.0f t", u.Books.Genesis.Total())
	t.Logf("  in the crust %14.0f t", reserve.Total())
	t.Logf("  in warehouse %14.0f t", warehouse.Total())
	t.Logf("  in transit   %14.0f t", u.Fleet.CargoAfloat().Total())
	t.Logf("  consumed     %14.0f t", u.Sink.Total())
	t.Logf("  ---")
	t.Logf("  mined        %14.0f t", u.Journal.Mined.Total())
	t.Logf("  manufactured %14.0f t", u.Journal.Made.Total())
	t.Logf("  delivered    %14.0f t over %d voyages",
		u.Journal.Delivered.Total(), u.Journal.Voyages)
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("  BOOKS DO NOT BALANCE: %v", bad[0])
	} else {
		t.Logf("  BALANCED — every ton accounted for")
	}

	t.Log("=== THE TRADE JOURNAL (last 12) ===")
	for _, e := range u.Journal.Tail(12) {
		t.Log("  " + e.String())
	}
}
