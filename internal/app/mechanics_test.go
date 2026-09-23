package app

import (
	"sort"
	"testing"

	"yodacon.org/gonex/internal/city"
	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/galaxy"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/market"
	"yodacon.org/gonex/internal/power"
	"yodacon.org/gonex/internal/reentry"
	"yodacon.org/gonex/internal/universe"
	"yodacon.org/gonex/internal/world"
)

// The game-layer seams.
//
// internal/universe proves the economy balances. These prove the GAME is
// wired to that economy: that the three type systems agree on how wide a
// cargo manifest is, that the map the player actually flies is the map the
// simulation is seeded from, and that what the outfitter sells reaches the
// physics that decides whether a landing survives.
//
// Every one of these is a place where two packages have to agree about a
// number, and a place where nothing would fail loudly if they stopped.

// THE WIDTH. econ.BoardWidth, len(market.Commodities) and
// world.CommodityCount are three independent statements of one fact, in
// three packages that cannot import each other. econ.go has claimed since it
// was written that "the app's tests assert all three against each other" —
// and until this test, nothing did. Widening the board from six to eight for
// the lithium fuels is exactly the change that would have silently desynced
// them: a hold one entry short does not crash, it just loses the cargo.
func TestTheThreePackagesAgreeOnTheBoard(t *testing.T) {
	if econ.BoardWidth != len(market.Commodities) {
		t.Errorf("econ.BoardWidth is %d, market lists %d commodities",
			econ.BoardWidth, len(market.Commodities))
	}
	if econ.BoardWidth != world.CommodityCount {
		t.Errorf("econ.BoardWidth is %d, world.CommodityCount is %d",
			econ.BoardWidth, world.CommodityCount)
	}
	// And in the same ORDER, because a planet's Stock and a ship's Hold are
	// a prefix of a material vector and convert by copying, not by lookup.
	for i, c := range market.Commodities {
		if got := econ.Material(i).String(); got != c.Name {
			t.Errorf("board slot %d is %q in econ and %q in market", i, got, c.Name)
		}
	}
	// The two fuels are on the board and are the last two slots, which is
	// what makes a pre-lithium six-wide save still read correctly.
	for _, m := range []econ.Material{econ.Pellets, econ.Melt} {
		if !m.Tradeable() || !m.Fuel() {
			t.Errorf("%s is not a tradeable fuel", m)
		}
	}
	if econ.Pellets != econ.BoardWidth-2 || econ.Melt != econ.BoardWidth-1 {
		t.Error("the fuels were inserted into the board rather than appended; old saves will mis-read")
	}
}

// gazetteerPorts builds the port list exactly as seedUniverse does, so the
// test is measuring the game's own map rather than a fixture.
func gazetteerPorts(t *testing.T, g *galaxy.Galaxy) []universe.Port {
	t.Helper()
	ids := make([]int, 0, len(g.Stellars))
	for id := range g.Stellars {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	ports := make([]universe.Port, 0, len(ids))
	for _, id := range ids {
		st := g.Stellars[id]
		ports = append(ports, universe.Port{
			Stellar: id, Name: st.Name, System: st.System,
			Pop: city.PopulationOf(id), Govt: govt.FromGazetteer(st.Govt),
		})
	}
	return ports
}

// THE MAP. The simulation is seeded from the recovered gazetteer, under the
// polities the 1997 record filed each world under, with the lanes charted
// off the real jump graph. Before FromGazetteer every world outside three
// hand-placed systems was Neutral, and before ChartLanes every lane in the
// game was the registry's default length — which meant the route ranking's
// margin-per-megametre divided by a constant and geography had no effect on
// trade at all.
func TestTheGameIsSeededFromTheRealMap(t *testing.T) {
	g, err := galaxy.Load()
	if err != nil {
		t.Skipf("no gazetteer: %v", err)
	}
	ports := universe.Triad(gazetteerPorts(t, g))
	if len(ports) < 50 {
		t.Fatalf("only %d ports in the gazetteer", len(ports))
	}

	// Every body must have a neighbourhood — the whole point of the triad.
	bySystem := map[int]int{}
	for _, p := range ports {
		bySystem[p.System]++
	}
	for sys, n := range bySystem {
		if n < 3 {
			t.Errorf("system %d has %d bodies; the same-type rule needs a neighbourhood", sys, n)
			break
		}
	}

	// Three prongs, not a sea of neutrals.
	byColour := map[govt.Color]int{}
	for _, p := range ports {
		byColour[p.Govt]++
	}
	for _, c := range govt.Colors() {
		if byColour[c] < 5 {
			t.Errorf("%s holds %d worlds on the real map; the polity mapping is not reaching it", c, byColour[c])
		}
	}
	if byColour[govt.None] == len(ports) {
		t.Fatal("every world in the galaxy is unaligned")
	}

	u := universe.New(20260922, ports, 16)
	u.ChartLanes(func(from, to int) int {
		a, b := g.Stellars[universe.HostOf(from)], g.Stellars[universe.HostOf(to)]
		if a == nil || b == nil {
			return -1
		}
		if a.System == b.System {
			return 0
		}
		r := g.Route(a.System, b.System)
		if r == nil {
			return -1
		}
		return len(r) - 1
	})

	// Geography exists: lanes must differ, and by a lot.
	var shortest, longest float64 = 1e18, 0
	for i, from := range u.Order() {
		for _, to := range u.Order()[i+1:] {
			l := u.Fleet.Lane(from, to).Length
			if l < shortest {
				shortest = l
			}
			if l > longest {
				longest = l
			}
		}
	}
	if longest <= shortest*1.5 {
		t.Errorf("every lane in the galaxy is about %.0f Mm long; distance does not exist", shortest)
	}

	// The fleet is sized to the map rather than to a constant.
	for _, c := range govt.Colors() {
		if n, want := len(u.Fleet.ByGovt(c)), len(ports)/40; n < want {
			t.Errorf("%s opened with %d hulls on a %d-port map", c, n, len(ports))
		}
	}

	// And it runs: a year of the real map, still balanced on both books.
	for d := 0; d < 365; d++ {
		u.Tick()
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Fatalf("mass on the real map: %v", bad[0])
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Fatalf("credits on the real map: %v", bad)
	}
	if u.Journal.Voyages == 0 {
		t.Error("no cargo was delivered anywhere in a year")
	}
	// The lithium cycle is running somewhere on the real map.
	if u.Journal.Mined[econ.Spodumene] <= 0 {
		t.Error("no spodumene was lifted anywhere in the galaxy")
	}
	if u.Journal.Made[econ.Pellets]+u.Journal.Made[econ.Melt] <= 0 {
		t.Error("no fuel of either form was refined anywhere in the galaxy")
	}
}

// findOutfit locates a shelf item by name, so the test fails loudly if the
// catalogue is renamed rather than quietly testing nothing.
func findOutfit(t *testing.T, name string) power.Outfit {
	t.Helper()
	for _, o := range power.Catalog() {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("no outfit %q on the shelf", name)
	return power.Outfit{}
}

// THE SHELF. What the outfitter sells has to reach the physics. A reactor
// swap changes which fuel the ship can burn and how much of it it can
// reach; a confinement outfit changes whether the pillow holds. Both live on
// the Grid, which is what entrymode copies onto the Vehicle — so this is the
// seam between the shop screen and the landing.
func TestTheOutfitterReachesTheFlightModel(t *testing.T) {
	g := power.Stock()
	if g.Reactor != power.Thermal {
		t.Fatalf("a stock hull leaves the yard with a %s", g.Reactor)
	}
	if r := power.ReactorOf(g.Reactor); r.Takes != econ.Pellets {
		t.Errorf("the stock pile burns %s, not pellets", r.Takes)
	}

	// Buying up the ladder changes the fuel form, not just the megawatts.
	mw, kg := g.ReactorMW, g.OutfitKg
	g.Buy(findOutfit(t, "Sodium fast loop"))
	if g.Reactor != power.Fast {
		t.Fatal("buying the fast loop did not fit it")
	}
	if power.ReactorOf(g.Reactor).Takes != econ.Melt {
		t.Error("the fast loop does not burn melt")
	}
	if g.ReactorMW <= mw || g.OutfitKg <= kg {
		t.Error("the swap booked neither power nor mass")
	}
	if got := power.ReactorOf(g.Reactor).Range(); got < 3 {
		t.Errorf("the fast loop reaches x%.1f the range of a pile; the ladder is flat", got)
	}
	// A swap REPLACES; it must not stack the flat catalogue mass on top of
	// the delta Fit already booked.
	kg = g.OutfitKg
	g.Buy(findOutfit(t, "Shipboard breeder"))
	if g.Reactor != power.Breeder {
		t.Fatal("the breeder did not replace the fast loop")
	}
	if delta := g.OutfitKg - kg; delta > 12000 {
		t.Errorf("the breeder booked %.0f kg over the fast loop; the swap is stacking", delta)
	}
	if power.ReactorOf(power.Breeder).Bred(10, 100) <= 0 {
		t.Error("the shipboard breeder breeds nothing")
	}

	// Confinement reaches the reentry vehicle and changes the pillow.
	g.Buy(findOutfit(t, "Multipole cusp ring"))
	g.Buy(findOutfit(t, "Phased steering array"))
	if !g.Shield.Fitted() {
		t.Fatal("the confinement outfits did not reach the grid")
	}
	stock, fitted := reentry.Yodacon(), reentry.Yodacon()
	fitted.Conf = g.Shield // exactly what entrymode does
	if fitted.CommandedLD() <= stock.CommandedLD() {
		t.Error("the steering array bought no lift on the vehicle")
	}
	if !fitted.Conf.Fitted() || fitted.Conf.LeakRelief() >= 1 {
		t.Error("the cusp ring did not reach the drag term")
	}
}
