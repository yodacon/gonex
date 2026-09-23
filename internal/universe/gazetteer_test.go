package universe

import (
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	"yodacon.org/gonex/internal/city"
	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/galaxy"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/industry"
)

// The balance rig.
//
// TestUniverseReport flies eleven hand-placed worlds, which is the right
// shape for proving the trifecta is symmetric and the wrong shape entirely
// for measuring an economy. Eleven worlds draw nought to three hostile
// bodies, so a run either has no fuel industry at all or has one refinery
// supplying a galaxy — and neither number tells you anything about the
// steady state.
//
// This rig flies the REAL GAZETTEER: every stellar the 1997 record knows,
// under the polity it was filed under, with the population the city
// generator grows on it. It is the map the game actually seeds, so it is
// the only map whose numbers are worth quoting.
//
//	GAZ=1 go test ./internal/universe -run TestGazetteer -v
//	GAZ=1 GAZDAYS=1200 go test ./internal/universe -run TestGazetteer -v

func gazetteerPorts(t testing.TB) []Port {
	t.Helper()
	g, err := galaxy.Load()
	if err != nil {
		t.Skipf("no gazetteer: %v", err)
	}
	ids := make([]int, 0, len(g.Stellars))
	for id := range g.Stellars {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	ports := make([]Port, 0, len(ids))
	for _, id := range ids {
		st := g.Stellars[id]
		ports = append(ports, Port{
			Stellar: id, Name: st.Name, System: st.System,
			Pop: city.PopulationOf(id), Govt: govt.FromGazetteer(st.Govt),
		})
	}
	return ports
}

func TestGazetteer(t *testing.T) {
	if os.Getenv("GAZ") == "" {
		t.Skip("set GAZ=1 to fly the full gazetteer and print the balance report")
	}
	days := 730
	fmt.Sscanf(os.Getenv("GAZDAYS"), "%d", &days)
	seed := int64(20260922)
	fmt.Sscanf(os.Getenv("GAZSEED"), "%d", &seed)

	ports := gazetteerPorts(t)
	if os.Getenv("FLAT") == "" {
		ports = Triad(ports) // three bodies per system; FLAT=1 for the old map
	}
	u := New(seed, ports, 16)
	g, _ := galaxy.Load()
	u.ChartLanes(func(from, to int) int {
		// Minted bodies are not in the gazetteer: chart them from the
		// planet they belong to, or they read as unreachable from their
		// own orbit.
		a, b := g.Stellars[HostOf(from)], g.Stellars[HostOf(to)]
		if a == nil || b == nil {
			return -1
		}
		r := g.Route(a.System, b.System)
		if r == nil {
			return -1
		}
		return len(r) - 1
	})

	// --- the map as founded ---
	var hot, breeders, smelters, mills int
	var pellet, melt int
	byColour := map[govt.Color]int{}
	for _, id := range u.Order() {
		w := u.Worlds[id]
		byColour[w.Govt]++
		if !w.Hostile() {
			continue
		}
		hot++
		switch {
		case w.Rad >= industry.RadBreed:
			breeders++
		case w.Rad >= industry.RadSmelt:
			smelters++
		default:
			mills++
		}
		for _, p := range w.Plant {
			switch p.Name {
			case "Fuel pellets":
				pellet++
			case "Fuel melt":
				melt++
			}
		}
	}
	t.Logf("=== THE MAP AS FOUNDED (seed %d) ===", seed)
	pl, st, fl := u.TriadReport()
	t.Logf("  %d ports — %d planets · %d stations · %d fields",
		len(ports), pl, st, fl)
	t.Logf("  Red %d · Green %d · Blue %d · neutral %d",
		byColour[govt.Red], byColour[govt.Green], byColour[govt.Blue], byColour[govt.None])
	var reach int
	for _, id := range u.Order() {
		reach += u.LocalReach(u.Worlds[id])
	}
	t.Logf("  in-system neighbours per body: %.2f (the same-type rule needs > 0)",
		float64(reach)/float64(len(u.Order())))
	t.Logf("  %d hostile (%.0f%%) — %d breeder-class, %d smelter-class, %d mill-class",
		hot, 100*float64(hot)/float64(len(ports)), breeders, smelters, mills)
	t.Logf("  refineries standing: %d pellet lines, %d melt lines", pellet, melt)

	// --- steady-state projection, before a single day is flown ---
	var capPellets, capMelt, demPellets, demMelt float64
	for _, id := range u.Order() {
		w := u.Worlds[id]
		demPellets += w.appetite(econ.Pellets)
		demMelt += w.appetite(econ.Melt)
		for _, p := range w.Plant {
			capPellets += p.Supply()[econ.Pellets]
			capMelt += p.Supply()[econ.Melt]
		}
	}
	t.Logf("=== STEADY STATE, PROJECTED ===")
	t.Logf("  pellets  nameplate %7.1f t/d   appetite %7.1f t/d   cover %5.2fx",
		capPellets, demPellets, capPellets/math.Max(demPellets, 1e-9))
	t.Logf("  melt     nameplate %7.1f t/d   appetite %7.1f t/d   cover %5.2fx",
		capMelt, demMelt, capMelt/math.Max(demMelt, 1e-9))

	// --- fly it ---
	for d := 0; d < days; d++ {
		u.Tick()
	}

	t.Logf("=== AFTER %d DAYS ===", days)
	made, mined, deliv := u.Journal.Made, u.Journal.Mined, u.Journal.Delivered
	perDay := func(x float64) float64 { return x / float64(days) }
	t.Logf("  %-10s %12s %12s %12s", "", "made t/d", "mined t/d", "hauled t/d")
	for _, m := range []econ.Material{econ.Pellets, econ.Melt, econ.Heavylith, econ.Lithium,
		econ.Lithex, econ.Acid, econ.Fluid, econ.Spodumene, econ.Steel, econ.Chips,
		econ.Ore, econ.Rations, econ.Hull, econ.Rounds} {
		t.Logf("  %-10s %12.1f %12.1f %12.1f", m, perDay(made[m]), perDay(mined[m]), perDay(deliv[m]))
	}
	t.Logf("  utilisation: pellets %.0f%% of nameplate, melt %.0f%%",
		100*perDay(made[econ.Pellets])/math.Max(capPellets, 1e-9),
		100*perDay(made[econ.Melt])/math.Max(capMelt, 1e-9))
	t.Logf("  fuel cover:  pellets %.0f%% of appetite, melt %.0f%%",
		100*perDay(made[econ.Pellets])/math.Max(demPellets, 1e-9),
		100*perDay(made[econ.Melt])/math.Max(demMelt, 1e-9))

	var reserve, warehouse econ.Stock
	for _, id := range u.Order() {
		reserve = reserve.Plus(u.Worlds[id].Reserve)
		warehouse = warehouse.Plus(u.Worlds[id].Warehouse)
	}
	t.Logf("=== THE BOOKS ===")
	t.Logf("  genesis %.0f kt · crust %.0f kt · warehouse %.0f kt · afloat %.0f t · sink %.0f kt",
		u.Books.Genesis.Total()/1000, reserve.Total()/1000, warehouse.Total()/1000,
		u.Fleet.CargoAfloat().Total(), u.Sink.Total()/1000)
	t.Logf("  spodumene left in the crust: %.0f kt of %.0f kt genesis (%.1f%% worked out)",
		reserve[econ.Spodumene]/1000, u.Books.Genesis[econ.Spodumene]/1000,
		100*(1-reserve[econ.Spodumene]/math.Max(u.Books.Genesis[econ.Spodumene], 1)))
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("  MASS DOES NOT BALANCE: %v", bad[0])
	} else {
		t.Logf("  mass BALANCED")
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("  LEDGER DOES NOT BALANCE: %v", bad)
	} else {
		t.Logf("  ledger BALANCED — %d cr in circulation", u.MoneySupply())
	}

	t.Logf("=== THE THREE PRONGS ===")
	for _, st := range u.Standings() {
		t.Logf("  %-5s %3d worlds · pop %6.1f M · %2d hulls · treasuries %10d cr · exchequer %9d cr",
			st.Color, st.Worlds, float64(st.Pop)/1e6, st.Hulls, st.Treasury, st.Exchequer)
	}
	for _, c := range govt.Colors() {
		var tons float64
		for _, h := range u.Fleet.ByGovt(c) {
			tons += h.Tons
		}
		t.Logf("  %-5s delivered %.1f kt lifetime, %d routes on the board today",
			c, tons/1000, len(u.FindRoutes(c, 0)))
	}
	t.Logf("  fleet: %s", u.Fleet.Report()[1])
	for _, line := range u.FleetReport() {
		t.Logf("  %s", line)
	}
}
