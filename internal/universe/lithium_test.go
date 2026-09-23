package universe

import (
	"math"
	"testing"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/industry"
	"yodacon.org/gonex/internal/traffic"
)

// The lithium cycle. Same method as everything else in this package: fly the
// simulation and look at what came out, because every fault found while
// building this layer was invisible to an assertion about a single function
// and obvious within seconds of printing a year of trade.

// hotWorld builds a one-world universe forced hot, for the siting rules.
func hotWorld(t *testing.T, rad float64, stellar int) (*Universe, *World) {
	t.Helper()
	u := New(4242, []Port{{Stellar: stellar, Name: "Hot", System: stellar, Pop: 400_000, Govt: govt.None}}, 0)
	w := u.Worlds[stellar]
	// Rad is set after Seed has run, so re-apply the two things Seed
	// derives from it: the population ceiling and the mandate.
	w.Rad = rad
	if cap := w.PopCeiling(); float64(w.Pop) > cap {
		w.Pop = int(cap)
	}
	w.Reserve = econ.Stock{}
	w.Reserve.Add(econ.Spodumene, 500_000)
	w.Reserve.Add(econ.Volatiles, 200_000)
	w.Mandate = nil
	if line := industry.LineFor(rad, stellar%2 == 1); line != "" {
		w.Mandate = []string{line}
	}
	w.standUpIndustry()
	w.stake()
	w.Reprice()
	u.ReopenBooks()
	u.ReopenLedger()
	return u, w
}

// THE siting rule. A hot cell is not merely expensive on a clean world, it
// is illegal there — so the only fuel in the universe comes off the worlds
// nobody can live on, and that is what makes them worth flying to.
func TestOnlyHotWorldsRefineFuel(t *testing.T) {
	var clean econ.Stock
	clean.Add(econ.Spodumene, 1e6) // every ton of ore in the game, and no dose
	for _, ch := range industry.Rank(clean, 0) {
		if ch.MinRad > 0 {
			t.Errorf("a clean world stood up %q", ch.Name)
		}
	}
	// And the ladder is a ladder: each rung licenses strictly more.
	for _, tc := range []struct {
		rad  float64
		want string
	}{
		{0.00, ""},
		{industry.RadMill, "Lithium milling"},
		{industry.RadSmelt, "Radiant smelting"},
		{industry.RadBreed, "Fuel pellets"},
	} {
		if got := industry.LineFor(tc.rad, false); got != tc.want {
			t.Errorf("dose %.2f licenses %q, want %q", tc.rad, got, tc.want)
		}
	}
	// The two fuel forms both exist on the map: which one a licensed world
	// pours alternates on its stellar ID, so a pilot with a fast loop has
	// to go and find a melt world rather than buying whatever is nearest.
	if industry.LineFor(1, false) == industry.LineFor(1, true) {
		t.Error("every refinery in the universe pours the same fuel")
	}
}

// The seam IS the dose: a clean world has no spodumene at all, however rich
// it is in everything else, and no world large enough to be a capital is
// ever condemned.
func TestTheSeamIsTheDose(t *testing.T) {
	var hot, withOre, bigAndHot int
	for id := 100; id < 600; id++ {
		e := econ.Endow(20260922, id, 900_000, 42)
		if e.Rad > 0 {
			hot++
		}
		if e.Reserve[econ.Spodumene] > 0 {
			withOre++
			if e.Rad <= 0 {
				t.Fatalf("world %d has spodumene with no dose", id)
			}
		}
		if e.Rad > 0 && e.Reserve[econ.Spodumene] <= 0 {
			t.Fatalf("world %d has a dose with no spodumene", id)
		}
		if econ.Dose(20260922, id, 8_000_000) > 0 {
			bigAndHot++
		}
	}
	if hot == 0 || hot == 500 {
		t.Errorf("%d of 500 worlds are hot; want a minority, not none and not all", hot)
	}
	if hot != withOre {
		t.Errorf("%d hot worlds but %d with ore", hot, withOre)
	}
	if bigAndHot != 0 {
		t.Errorf("%d worlds above the habitable line were condemned; a capital must never be", bigAndHot)
	}
}

// A refinery is founded with the working capital to buy what it can never
// make. Without it the hot worlds sit in a trap with no exit from inside:
// no chemicals, so no fuel; no fuel, so no revenue; no revenue, so no
// chemicals — at nine per cent of nameplate for a simulated year.
func TestARefineryIsCapitalised(t *testing.T) {
	_, hot := hotWorld(t, 0.9, 400)
	_, mild := hotWorld(t, 0.2, 400)
	if hot.Credits <= mild.Credits {
		t.Errorf("a breeder world opens with %d cr against a clean one's %d", hot.Credits, mild.Credits)
	}
	// Enough to actually run: at least a fortnight of its own intake.
	var daily float64
	for _, p := range hot.Plant {
		d := p.Demand()
		for m := econ.Material(0); m < econ.Count; m++ {
			if d[m] > 0 && !m.Crust() {
				daily += d[m] * Base(m)
			}
		}
	}
	if daily > 0 && float64(hot.Credits) < daily*14 {
		t.Errorf("a refinery opens with %d cr against %.0f cr/day of intake", hot.Credits, daily)
	}
}

// Mass is conserved through the whole five-stage line, and the fuel that
// comes out the far end is genuinely made rather than conjured.
func TestTheLithiumLineRunsAndBalances(t *testing.T) {
	u, w := hotWorld(t, 0.95, 401) // odd stellar: a melt refinery
	// It cannot run on its own dirt: the mill wants acid and the loop wants
	// hydraulic fluid, and a hot world makes neither. That IS the design —
	// the chemicals are the supply line into the hostile worlds.
	for d := 0; d < 40; d++ {
		w.Warehouse.Add(econ.Acid, 60)
		w.Warehouse.Add(econ.Fluid, 60)
		u.ReopenBooks()
		u.Tick()
		if bad := u.Audit(); len(bad) > 0 {
			t.Fatalf("day %d: %v", u.Day, bad[0])
		}
	}
	if u.Journal.Made[econ.Melt] <= 0 {
		t.Fatalf("a fully hot melt refinery with chemicals delivered made no fuel: %s", w.Warehouse)
	}
	// Ore in, fuel out, and the difference is slag — never a gain.
	in := u.Journal.Mined[econ.Spodumene]
	out := u.Journal.Made[econ.Melt]
	if out >= in {
		t.Errorf("%.0ft of fuel from %.0ft of ore: the line is a mint", out, in)
	}
	if w.Reserve[econ.Spodumene] >= 500_000 {
		t.Error("the seam did not fall")
	}
}

// Hot material travels in a cask or not at all, and the concentrate the hot
// worlds IMPORT does travel. Both halves matter: the first pins the
// finishing plant beside the breeder, the second is why hostile worlds are
// a trading zone rather than a closed shop.
func TestCaskCargoStaysLocal(t *testing.T) {
	for _, m := range []econ.Material{econ.Lithium, econ.Heavylith} {
		if !m.Hot() || !shuttleOnly(m) {
			t.Errorf("%s can ride an ordinary courier across the galaxy", m)
		}
	}
	for _, m := range []econ.Material{econ.Lithex, econ.Acid, econ.Fluid, econ.Steel} {
		if shuttleOnly(m) {
			t.Errorf("%s cannot be carried to the world that needs it", m)
		}
	}
	for _, m := range []econ.Material{econ.Pellets, econ.Melt} {
		if !m.Tradeable() || !m.Fuel() {
			t.Errorf("%s is not on the board", m)
		}
	}
}

// A hostile world is worked by machines, not by a workforce — otherwise we
// would have put every fuel seam in the universe under the only worlds with
// nobody to work them.
func TestHostileWorldsAreWorkedUnmanned(t *testing.T) {
	u, w := hotWorld(t, 0.9, 402)
	if w.Pop > 200_000 {
		t.Errorf("a world at dose 0.90 holds %d people", w.Pop)
	}
	before := w.Reserve[econ.Spodumene]
	for d := 0; d < 10; d++ {
		u.Tick()
	}
	if w.Reserve[econ.Spodumene] >= before {
		t.Error("a hot world with a mandated refinery dug nothing")
	}
	// And it grows into a camp, not a city.
	if float64(w.Pop) > w.PopCeiling()+1 {
		t.Errorf("pop %d is over the ceiling %.0f", w.Pop, w.PopCeiling())
	}
}

// A mandated line gets first call on the dig budget. Kestrel stood up as a
// melt refinery wanting 142 t of spodumene a day, and the ordinary copper
// line beside it — wanting 177 t of cuprite — took the whole 135 t budget
// every day for a simulated year. The refinery never smelted a ton and
// nothing anywhere logged a complaint.
func TestAMandatedLineIsDugForFirst(t *testing.T) {
	u, w := hotWorld(t, 0.95, 404)
	w.Reserve.Add(econ.Cuprite, 900_000) // a far richer seam than the ore
	w.standUpIndustry()
	u.ReopenBooks()
	if len(w.Mandated()) == 0 {
		t.Fatal("the refinery was not mandated")
	}
	for d := 0; d < 5; d++ {
		u.Tick()
	}
	if w.Warehouse[econ.Spodumene] <= 0 {
		t.Errorf("the mandated refinery got no ore; cuprite on hand %.0ft", w.Warehouse[econ.Cuprite])
	}
}

// Two plants drawing on one warehouse share it in proportion to what they
// asked for. Running them in list order let the first take the lot, and the
// galaxy made 2,204 t of ore a day against 35 t of steel — with an appetite
// for steel of nine hundred.
func TestContestedInputsAreRationed(t *testing.T) {
	u := New(77, []Port{{Stellar: 500, Name: "Ferrite", System: 500, Pop: 2_000_000, Govt: govt.Green}}, 0)
	w := u.Worlds[500]
	w.Reserve = econ.Stock{}
	w.Reserve.Add(econ.Ferrite, 400_000)
	w.Mandate = nil
	w.standUpIndustry()
	u.ReopenBooks()
	var ore, steel bool
	for _, p := range w.Plant {
		ore = ore || p.Supply()[econ.Ore] > 0
		steel = steel || p.Supply()[econ.Steel] > 0
	}
	if !ore || !steel {
		t.Skip("this seed did not stand up both ferrite chains")
	}
	for d := 0; d < 30; d++ {
		u.Tick()
	}
	made := u.Journal.Made
	if made[econ.Ore] <= 0 || made[econ.Steel] <= 0 {
		t.Fatalf("ore %.0f, steel %.0f — one chain starved the other out", made[econ.Ore], made[econ.Steel])
	}
	if r := made[econ.Ore] / made[econ.Steel]; r > 6 || r < 1.0/6 {
		t.Errorf("ore:steel is %.1f:1 — one plant is taking the warehouse", r)
	}
}

// The merchant fleet is sized by the map, then by the board.
func TestFleetIsSizedByTheMapAndTheBoard(t *testing.T) {
	u := newTestUniverse(31)
	for _, c := range govt.Colors() {
		n := len(u.Fleet.ByGovt(c))
		if want := len(u.worldsOf(c)) * u.Tune.OpeningHulls; n < want {
			t.Errorf("%s opened with %d hulls over %d worlds, want at least %d",
				c, n, len(u.worldsOf(c)), want)
		}
		if n > u.FleetCap(c) {
			t.Errorf("%s opened over its own cap: %d of %d", c, n, u.FleetCap(c))
		}
	}
	// A yard with plate and a board that is paying presses a hull; the
	// census grows by a row and the warehouse falls by exactly its tonnage.
	w := u.Capital(govt.Red)
	w.Warehouse.Add(econ.Hull, 4000)
	w.Credits += 200_000
	u.ReopenBooks()
	u.ReopenLedger()
	before, plate := len(u.Fleet.ByGovt(govt.Red)), w.Warehouse[econ.Hull]
	u.commission(govt.Red, 1e6)
	after := len(u.Fleet.ByGovt(govt.Red))
	if after != before+1 {
		t.Fatalf("fleet went %d → %d", before, after)
	}
	drawn := plate - w.Warehouse[econ.Hull]
	newest := u.Fleet.Hulls[len(u.Fleet.Hulls)-1]
	if math.Abs(drawn-newest.Dry) > 1e-6 {
		t.Errorf("the yard gave up %.1ft for a %.1ft ship", drawn, newest.Dry)
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books after commissioning: %v", bad[0])
	}
	// And laying one up puts the plate back, with the books still square.
	u.layUp(govt.Red, u.Fleet.ByGovt(govt.Red), 0)
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books after a lay-up: %v", bad[0])
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("ledger after a lay-up: %v", bad)
	}
}

// A harvester lifts crust straight out of a rock nobody lives on. The tons
// come off a reserve, which only ever falls; nothing is minted.
func TestHarvesterLiftsFromTheGroundAndBalances(t *testing.T) {
	u := newTestUniverse(41)
	for d := 0; d < 30; d++ {
		u.Tick()
	}
	seam := u.Worlds[301]
	h := u.Fleet.ByGovt(govt.Red)[0]
	h.Status, h.Home, h.Cargo = traffic.Idle, seam.Stellar, econ.Stock{}
	h.Purse = 400_000
	h.Mass = h.Wet()
	u.ReopenBooks()
	u.ReopenLedger()
	before := seam.Reserve.Total()
	u.harvest(h, seam)
	if seam.Reserve.Total() >= before {
		t.Error("the harvester lifted nothing out of the ground")
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books after a harvest: %v", bad[0])
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("royalty did not balance: %v", bad)
	}
}

// A survey raises the dig budget where it lands and over the fence, and then
// expires. It is the only thing in the game that raises extraction without
// raising population.
func TestASurveyLiftsTheSeamAndExpires(t *testing.T) {
	u := newTestUniverse(43)
	u.ChartLanes(func(from, to int) int {
		if u.Worlds[from].System == u.Worlds[to].System {
			return 0
		}
		return 4
	})
	w := u.Worlds[133]
	var neighbour *World
	for _, id := range u.Order() {
		if n := u.Worlds[id]; n != w && u.oneJump(w, n) {
			neighbour = n
			break
		}
	}
	if neighbour == nil {
		t.Skip("no neighbour in this fixture")
	}
	u.recordSurvey(w)
	if got := w.SurveyLift(u.Day); got <= 1 {
		t.Errorf("the surveyed world lifts x%.2f", got)
	}
	if got := neighbour.SurveyLift(u.Day); got <= 1 {
		t.Errorf("the world next door lifts x%.2f — a survey reads a formation, not a property line", got)
	}
	if w.SurveyLift(u.Day) <= neighbour.SurveyLift(u.Day) {
		t.Error("the neighbour did as well out of the survey as the world it landed on")
	}
	if got := w.SurveyLift(u.Day + surveyDays + 1); got != 1 {
		t.Errorf("the reading never expires: x%.2f", got)
	}
}
