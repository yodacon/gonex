package universe

import (
	"math"
	"testing"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// testPorts is a small three-colour universe with a neutral middle, which is
// the shape triforce.xml actually has: three clusters and contested ground.
func testPorts() []Port {
	return []Port{
		{133, "ConEx", 133, 4_200_000, govt.Red},
		{134, "ConEx Yards", 133, 900_000, govt.Red},
		{135, "ConEx Deep", 133, 600_000, govt.Red},
		{238, "Exeon", 238, 4_000_000, govt.Green},
		{239, "Exeon Yards", 238, 900_000, govt.Green},
		{240, "Exeon Deep", 238, 600_000, govt.Green},
		{235, "Cenron", 235, 4_100_000, govt.Blue},
		{236, "Cenron Yards", 235, 900_000, govt.Blue},
		{237, "Cenron Deep", 235, 600_000, govt.Blue},
		{300, "Midpoint", 133, 1_200_000, govt.None},
		{301, "Kestrel", 133, 1_200_000, govt.None},
	}
}

func newTestUniverse(seed int64) *Universe { return New(seed, testPorts(), 12) }

// THE invariant. Everything else in this package is an opinion; this is the
// fact. Four hundred days of mining, refining, eating, trading and dying, and
// every ton the universe was created with is still somewhere you can point
// at. A leak here means some process is minting matter, and an economy that
// mints matter cannot be balanced by anybody.
func TestMassIsConservedOverALongRun(t *testing.T) {
	for _, seed := range []int64{1, 7, 99, 20260903} {
		u := newTestUniverse(seed)
		for d := 0; d < 400; d++ {
			u.Tick()
			if d%50 == 0 || d == 399 {
				if bad := u.Audit(); len(bad) > 0 {
					t.Fatalf("seed %d, day %d: books do not balance:\n  %v",
						seed, u.Day, bad[0])
				}
			}
		}
		// And prove the run actually did something — a universe where
		// nothing happened would pass the audit trivially.
		if u.Journal.Mined.Total() <= 0 {
			t.Errorf("seed %d: nothing was ever mined", seed)
		}
		if u.Journal.Made.Total() <= 0 {
			t.Errorf("seed %d: nothing was ever manufactured", seed)
		}
		if u.Journal.Voyages == 0 {
			t.Errorf("seed %d: no cargo was ever delivered in 400 days", seed)
		}
	}
}

// Losing a hull with cargo aboard must not lose the cargo's mass — it is
// scattered, not deleted. This is the case a naive implementation always gets
// wrong, so it gets its own test.
func TestDestroyingALadenHullConservesItsCargo(t *testing.T) {
	u := newTestUniverse(4)
	for d := 0; d < 60; d++ {
		u.Tick()
	}
	var laden *traffic.Hull
	for _, h := range u.Fleet.Hulls {
		if h.Laden() > 0 {
			laden = h
			break
		}
	}
	if laden == nil {
		t.Skip("no laden hull after 60 days")
	}
	u.Lose(laden, "test")
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books do not balance after a loss: %v", bad[0])
	}
	if laden.Laden() > 0 {
		t.Error("a destroyed hull is still holding cargo")
	}
}

// The census is fixed for the life of the universe: hulls are never spawned
// to fill a scene, and a destroyed one is still counted forever.
func TestTheShipCensusIsFixed(t *testing.T) {
	u := newTestUniverse(11)
	want := len(u.Fleet.Hulls)
	if want == 0 {
		t.Fatal("no hulls were raised")
	}
	for d := 0; d < 200; d++ {
		u.Tick()
		if got := len(u.Fleet.Hulls); got != want {
			t.Fatalf("day %d: census is %d, was %d", u.Day, got, want)
		}
	}
	c := u.Fleet.Census()
	var total int
	for _, n := range c {
		total += n
	}
	if total != want {
		t.Errorf("statuses account for %d hulls, census is %d", total, want)
	}
}

// A universe must be replayable from its seed, or balance work is folklore.
func TestTheSameSeedGivesTheSameUniverse(t *testing.T) {
	run := func() (float64, float64, int) {
		u := newTestUniverse(2026)
		for d := 0; d < 120; d++ {
			u.Tick()
		}
		var pop int
		for _, id := range u.Order() {
			pop += u.Worlds[id].Pop
		}
		return u.Journal.Mined.Total(), u.Journal.Delivered.Total(), pop
	}
	m1, d1, p1 := run()
	m2, d2, p2 := run()
	if m1 != m2 || d1 != d2 || p1 != p2 {
		t.Errorf("replay diverged: mined %.3f vs %.3f, delivered %.3f vs %.3f, pop %d vs %d",
			m1, m2, d1, d2, p1, p2)
	}
}

// Different seeds must give genuinely different maps — otherwise the
// endowment is decorative and every game is the same game.
func TestDifferentSeedsGiveDifferentWorlds(t *testing.T) {
	a := newTestUniverse(1)
	b := newTestUniverse(2)
	same := 0
	for _, id := range a.Order() {
		if a.Worlds[id].Speciality() == b.Worlds[id].Speciality() {
			same++
		}
	}
	if same == len(a.Order()) {
		t.Error("two seeds produced identical specialities on every world")
	}
	// And within one universe, worlds must not all be the same either.
	seen := map[string]bool{}
	for _, id := range a.Order() {
		seen[a.Worlds[id].Speciality()] = true
	}
	if len(seen) < 2 {
		t.Errorf("every world in the universe has the same industry (%d kinds)", len(seen))
	}
}

// A crust reserve can only ever fall. Nothing anywhere puts material back in
// the ground, which is what makes the economy zero-sum in the long run rather
// than merely balanced tick to tick.
func TestReservesOnlyEverFall(t *testing.T) {
	u := newTestUniverse(5)
	prev := map[int]econ.Stock{}
	for _, id := range u.Order() {
		prev[id] = u.Worlds[id].Reserve
	}
	for d := 0; d < 150; d++ {
		u.Tick()
		for _, id := range u.Order() {
			now := u.Worlds[id].Reserve
			for _, m := range econ.Crusts() {
				if now[m] > prev[id][m]+1e-9 {
					t.Fatalf("day %d, %s: %s reserve rose from %.3f to %.3f",
						u.Day, u.Worlds[id].Name, m, prev[id][m], now[m])
				}
			}
			prev[id] = now
		}
	}
}

// Trade has to actually happen, and it has to be trade — cargo moving from a
// port that has a surplus to a port that is short.
func TestGoodsActuallyMoveBetweenWorlds(t *testing.T) {
	u := newTestUniverse(31)
	for d := 0; d < 250; d++ {
		u.Tick()
	}
	if u.Journal.Delivered.Total() <= 0 {
		t.Fatal("nothing was delivered anywhere in 250 days")
	}
	// Somebody has to have flown more than once, or the fleet is stuck.
	var voyagers int
	for _, h := range u.Fleet.Hulls {
		if h.Voyages > 1 {
			voyagers++
		}
	}
	if voyagers == 0 {
		t.Error("no hull completed more than a single voyage")
	}
}

// The physics has to be physics: a laden hull accelerates worse than an empty
// one of the same design, because it is heavier. If this fails, the momentum
// step has become a countdown timer.
func TestALadenHullIsSlowerThanAnEmptyOne(t *testing.T) {
	u := newTestUniverse(3)
	mk := func(cargo float64) *traffic.Hull {
		h := &traffic.Hull{
			ID: 900, Name: "test", Govt: govt.Red, Status: traffic.Hauling,
			From: 133, To: 238, Dry: 400, Thrust: 40,
		}
		h.Cargo.Add(econ.Ore, cargo)
		return h
	}
	u.Fleet.SetLane(133, 238, 400)
	empty, full := mk(0), mk(600)
	for i := 0; i < 6; i++ {
		u.Fleet.Step(empty, 1)
		u.Fleet.Step(full, 1)
	}
	if full.S >= empty.S {
		t.Errorf("after 6 days the laden hull is at %.2f Mm and the empty one at %.2f",
			full.S, empty.S)
	}
	if empty.V <= 0 {
		t.Error("the empty hull never got moving")
	}
}

// Green's growth edge and Red's gunnery edge must show up in the SIMULATION,
// not just in the table. If the trifecta does not change outcomes it is
// decoration.
func TestTheTrifectaChangesOutcomes(t *testing.T) {
	// One world each, identical population and stellar-independent seed, so
	// the only difference is the flag over it.
	grow := func(c govt.Color) int {
		u := New(77, []Port{{500, "Test", 500, 1_000_000, c}}, 0)
		// Stock the larder. A lone world with no trade partners starves, and
		// a starving world does not grow whoever holds it — which would make
		// this a test of famine rather than a test of the growth axis.
		w := u.Worlds[500]
		for _, m := range []econ.Material{econ.Rations, econ.Medicine, econ.FuelCells, econ.Lumber} {
			w.Warehouse.Add(m, 5e6)
		}
		u.ReopenBooks()
		for d := 0; d < 60; d++ {
			u.Tick()
		}
		if bad := u.Audit(); len(bad) > 0 {
			t.Fatalf("%s: books unbalanced: %v", c, bad[0])
		}
		return w.Pop
	}
	green, blue := grow(govt.Green), grow(govt.Blue)
	if green <= blue {
		t.Errorf("after 60 days Green is %d and Blue is %d — Green should out-grow Blue",
			green, blue)
	}
	// The gap should be material, not a rounding artefact.
	if ratio := float64(green) / float64(blue); ratio < 1.10 {
		t.Errorf("Green out-grows Blue by only %.1f%% in 60 days", (ratio-1)*100)
	}
}

// A world whose supply line is cut must stop growing rather than starving
// silently — the economic consequence of losing a lane has to be visible.
func TestAStarvedWorldStopsGrowing(t *testing.T) {
	// A world with nothing in the warehouse and no farm cannot eat.
	u := New(9, []Port{{600, "Rock", 600, 2_000_000, govt.Green}}, 0)
	w := u.Worlds[600]
	w.Warehouse = econ.Stock{}
	w.Reserve = econ.Stock{}
	w.Plant = nil
	// The scenario IS the starting state, so the books are re-based on it;
	// otherwise emptying the crust by hand reads to the auditor as the
	// planet's entire mass going missing, which is exactly what it would be
	// if the simulation had done it.
	u.ReopenBooks()
	start := w.Pop
	for d := 0; d < 40; d++ {
		u.Tick()
	}
	if w.Pop > start {
		t.Errorf("a world with no food grew from %d to %d", start, w.Pop)
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books unbalanced after starvation: %v", bad[0])
	}
}

// Prices must respond to real scarcity, since that is what makes routes open
// and close as the simulation runs.
func TestPricesRespondToScarcity(t *testing.T) {
	u := newTestUniverse(13)
	w := u.Worlds[133]
	w.Warehouse[econ.Chips] = 0
	w.Reprice()
	short := w.Shop[econ.Chips]

	w.Warehouse[econ.Chips] = 500000
	w.Reprice()
	glut := w.Shop[econ.Chips]

	if short <= glut {
		t.Errorf("a port pays %d for chips when it has none and %d when glutted",
			short, glut)
	}
	if math.Abs(float64(short-glut)) < 10 {
		t.Errorf("scarcity moved the price by only %d credits", short-glut)
	}
}

// Mass held outside this package still has to be on the books. The player's
// deck is the case that matters: a counter where tons can be bought is a
// counter where tons can be INVENTED, and the player is the one actor in the
// game standing at one all day.
func TestExternallyHeldMassIsAudited(t *testing.T) {
	u := newTestUniverse(17)
	var deck econ.Stock
	u.Account(func() econ.Stock { return deck })

	for d := 0; d < 40; d++ {
		u.Tick()
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Fatalf("unbalanced before trading: %v", bad[0])
	}

	// Buy: the tons leave a warehouse and land on the deck. Books hold.
	w := u.Worlds[133]
	for _, m := range []econ.Material{econ.Ore, econ.Lumber, econ.Chips} {
		econ.Transfer(&w.Warehouse, &deck, m, 40)
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("unbalanced after a purchase: %v", bad[0])
	}
	if deck.Total() <= 0 {
		t.Fatal("nothing was bought")
	}

	// Sell somewhere else: the tons leave the deck and supply that port.
	dst := u.Worlds[300]
	for m := econ.Material(0); m < econ.Count; m++ {
		econ.Transfer(&deck, &dst.Warehouse, m, deck[m])
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("unbalanced after a sale: %v", bad[0])
	}
	if deck.Total() > 1e-9 {
		t.Errorf("%.3f t stuck on the deck after selling everything", deck.Total())
	}

	// And an UNREGISTERED pool must read as a leak, or the safety net is
	// decorative: forgetting to account for a hold IS losing track of it.
	stray := u.Worlds[134]
	var hidden econ.Stock
	// Whatever the warehouse actually holds most of — a fixture that names a
	// material is a fixture that breaks when the industry table changes.
	most := econ.Ferrite
	for m := econ.Material(0); m < econ.Count; m++ {
		if stray.Warehouse[m] > stray.Warehouse[most] {
			most = m
		}
	}
	if stray.Warehouse[most] < 25 {
		t.Fatalf("world 134 holds under 25 t of anything; nothing to leak")
	}
	econ.Transfer(&stray.Warehouse, &hidden, most, 25)
	if bad := u.Audit(); len(bad) == 0 {
		t.Error("moving 25 t into an unaccounted pool did not register as a leak")
	}
}

// A hull dying takes its cargo out of circulation without taking it off the
// books, and the census remembers it forever.
func TestLossesShowUpInTheBooksAndTheCensus(t *testing.T) {
	u := newTestUniverse(23)
	for d := 0; d < 80; d++ {
		u.Tick()
	}
	before := u.Fleet.Afloat()
	killed := 0
	for _, h := range u.Fleet.Hulls {
		if h.Status == traffic.Lost {
			continue
		}
		u.Lose(h, "test")
		killed++
		if killed >= 5 {
			break
		}
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books unbalanced after %d losses: %v", killed, bad[0])
	}
	if got := u.Fleet.Afloat(); got != before-killed {
		t.Errorf("%d afloat after %d losses, want %d", got, killed, before-killed)
	}
	if u.Journal.LostHulls != killed {
		t.Errorf("journal counted %d losses, want %d", u.Journal.LostHulls, killed)
	}
	// The census itself never shrinks: a lost hull is still a row.
	if got := len(u.Fleet.Hulls); got != 3*12 {
		t.Errorf("census is %d rows after losses, want %d", got, 3*12)
	}
	// And the simulation must survive its own casualties.
	for d := 0; d < 40; d++ {
		u.Tick()
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books unbalanced running on after losses: %v", bad[0])
	}
}
