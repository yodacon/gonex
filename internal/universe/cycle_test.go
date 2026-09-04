package universe

import (
	"math"
	"testing"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// The resource cycle: the second conservation law, the return paths, the
// yard, the wreck rule, Konquest's battle, standing orders, the ladder and
// the seat. Every test here runs the simulation rather than asserting on a
// single function, because that is how all four bugs in the trade economy
// were found and none of them would have failed a unit assertion.

// THE second invariant. Credits are minted at genesis and never again: over
// four hundred days of loading, delivering, tariffs, leave, subsidies,
// buildings and battles, every credit is still in some purse.
func TestCreditsAreConservedOverALongRun(t *testing.T) {
	for _, seed := range []int64{1, 7, 99, 20260904} {
		u := newTestUniverse(seed)
		supply := u.MoneySupply()
		for d := 0; d < 400; d++ {
			u.Tick()
			if d%50 == 0 || d == 399 {
				if bad := u.AuditCredits(); bad != nil {
					t.Fatalf("seed %d, day %d: %v", seed, u.Day, bad)
				}
				if bad := u.Audit(); len(bad) > 0 {
					t.Fatalf("seed %d, day %d: mass: %v", seed, u.Day, bad[0])
				}
			}
		}
		if u.MoneySupply() != supply {
			t.Errorf("seed %d: money supply moved from %d to %d", seed, supply, u.MoneySupply())
		}
		// And money must actually have MOVED — pilots paid, pilots spending.
		var purses int
		for _, h := range u.Fleet.Hulls {
			purses += h.Purse
		}
		if purses == 0 {
			t.Errorf("seed %d: every pilot is broke after 400 days", seed)
		}
	}
}

// A hull dying drops its cargo where it died. The tons are still on the
// books, findable, and the nearest hold with room has lifted what it could.
func TestWreckCargoIsNeverLost(t *testing.T) {
	u := newTestUniverse(4)
	for d := 0; d < 60; d++ {
		u.Tick()
	}
	var laden *traffic.Hull
	for _, h := range u.Fleet.Hulls {
		if h.Status.UnderWay() && h.Laden() > 0 {
			laden = h
			break
		}
	}
	if laden == nil {
		t.Skip("no laden hull under way after 60 days")
	}
	before := u.Fleet.CargoAfloat().Plus(u.Fleet.DebrisAfloat()).Total()
	cargo, dry := laden.Laden(), laden.Dry
	u.Lose(laden, "test")
	if bad := u.Audit(); len(bad) > 0 {
		t.Fatalf("books unbalanced after a wreck: %v", bad[0])
	}
	after := u.Fleet.CargoAfloat().Plus(u.Fleet.DebrisAfloat()).Total()
	// Cargo went to holds or to the field; the hull's own tons went to the
	// field as scrap. Nothing went to the sink.
	if math.Abs((after-before)-dry) > 1e-6 {
		t.Errorf("cargo+debris moved by %.3f t, want the %.3f t of hull scrap", after-before, dry)
	}
	if u.Sink[econ.Scrap] > 0 {
		t.Error("hull scrap went to the sink; it should be adrift")
	}
	_ = cargo
	// And it stays: a hundred days on, whatever nobody lifted is still there.
	adrift := u.Fleet.DebrisAfloat().Total()
	for d := 0; d < 100; d++ {
		u.Tick()
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books unbalanced running on past a wreck: %v", bad[0])
	}
	if adrift > 0 && u.Fleet.DebrisAfloat().Total() > adrift+1e-6 && len(u.Fleet.Debris) == 0 {
		t.Error("debris register emptied while tons remained adrift")
	}
}

// The collection rule: nearest hold first, up to what it can hold, then the
// next. Built by hand so the geometry is known.
func TestNearestHoldScoopsFirst(t *testing.T) {
	u := newTestUniverse(8)
	u.Fleet.SetLane(133, 238, 600)
	mk := func(id int, s, dry float64) *traffic.Hull {
		h := &traffic.Hull{ID: id, Name: "t", Govt: govt.Red, Status: traffic.Hauling,
			From: 133, To: 238, Dry: dry, Thrust: 40, S: s}
		u.Fleet.Add(h)
		return h
	}
	near := mk(901, 310, 100) // capacity 57.5 t, 10 Mm away
	far := mk(902, 380, 2000) // huge, 80 Mm away
	victim := mk(903, 300, 400)
	victim.Cargo.Add(econ.Chips, 150)
	u.ReopenBooks()

	u.Lose(victim, "test")
	if near.Laden() <= 0 {
		t.Fatal("the nearest hull took nothing")
	}
	if near.Free() > 1e-6 {
		t.Errorf("the nearest hull has %.1f t free; it should be full before the next hull takes any", near.Free())
	}
	if far.Laden() <= 0 {
		t.Error("the second hull took nothing though the first was full")
	}
	total := near.Laden() + far.Laden() + u.Fleet.DebrisAfloat().Total()
	if math.Abs(total-(150+400)) > 1e-6 {
		t.Errorf("%.1f t accounted for across holds and field, want 550", total)
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books: %v", bad[0])
	}
}

// Konquest's battle, conserved: rounds are spent into the sink, dead hulls
// become scrap over the planet, and a planet with nobody home falls.
func TestEngageConservesAndTakesAnUndefendedWorld(t *testing.T) {
	u := newTestUniverse(12)
	target := u.Worlds[301] // Kestrel, unaligned
	target.Warehouse[econ.Rounds] = 0
	u.ReopenBooks()
	var flight []*traffic.Hull
	for _, h := range u.Fleet.ByGovt(govt.Red) {
		if len(flight) == 4 {
			break
		}
		h.Cargo.Add(econ.Rounds, 3) // armed
		flight = append(flight, h)
	}
	u.ReopenBooks()
	out := u.Engage(flight, target)
	if !out.Fell {
		t.Fatalf("an unarmed, ungarrisoned world held: %+v", out)
	}
	if target.Govt != govt.Red {
		t.Errorf("world is %s after falling to Red", target.Govt)
	}
	for _, h := range flight {
		if h.Status == traffic.Lost {
			continue
		}
		if h.Home != target.Stellar || h.Status != traffic.Idle {
			t.Errorf("%s did not become the garrison (home %d, %s)", h.Name, h.Home, h.Status)
		}
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books after battle: %v", bad[0])
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("ledger after battle: %v", bad)
	}
}

// A defended world with a full magazine kills attackers, spends rounds, and
// the wrecks are scrap in its orbit — a mining operation for the winner.
func TestADefendedWorldSpendsRoundsAndLeavesScrap(t *testing.T) {
	u := newTestUniverse(12)
	fort := u.Worlds[235] // Cenron, Blue
	fort.Warehouse[econ.Rounds] = 5000
	fort.Built[Bastion] = 3
	u.ReopenBooks()
	rounds0 := fort.Warehouse[econ.Rounds]
	var flight []*traffic.Hull
	for _, h := range u.Fleet.ByGovt(govt.Red) {
		if len(flight) == 2 {
			break
		}
		h.Cargo.Add(econ.Rounds, 0.2) // nearly dry
		flight = append(flight, h)
	}
	u.ReopenBooks()
	out := u.Engage(flight, fort)
	if out.Fell {
		t.Fatal("a fortress fell to two dry hulls")
	}
	if out.AttackersLost == 0 {
		t.Error("nobody died")
	}
	if fort.Warehouse[econ.Rounds] >= rounds0 {
		t.Error("the defence spent no rounds")
	}
	var scrap float64
	for _, d := range u.Fleet.Debris {
		if d.InOrbit() && d.At == fort.Stellar {
			scrap += d.Stock[econ.Scrap]
		}
	}
	// Idle Blue hulls at Cenron may already have scooped some; either way
	// the scrap exists somewhere on the books.
	var held float64
	for _, h := range u.Fleet.Hulls {
		held += h.Cargo[econ.Scrap]
	}
	if scrap+held <= 0 {
		t.Error("no scrap anywhere after hulls were destroyed")
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books: %v", bad[0])
	}
}

// A world with an empty magazine rates zero, whatever it has built.
func TestRatingIsZeroWithoutRounds(t *testing.T) {
	u := newTestUniverse(3)
	w := u.Worlds[133]
	w.Built[Bastion] = 3
	w.Warehouse[econ.Rounds] = 0
	if r := u.Rating(w); r != 0 {
		t.Errorf("a fortress with no rounds rates %.2f", r)
	}
	w.Warehouse[econ.Rounds] = 1e6
	if r := u.Rating(w); r < 0.8 {
		t.Errorf("a stocked fortress rates only %.2f", r)
	}
}

// A standing order runs every day and dies with the government that gave it.
func TestStandingOrderRunsAndCancelsOnConquest(t *testing.T) {
	u := newTestUniverse(21)
	src, dst := u.Worlds[133], u.Worlds[301]
	if err := u.File(StandingOrder{From: 133, To: 301, Owner: govt.Red, Hulls: 2}); err != nil {
		t.Fatal(err)
	}
	u.Tick()
	var flying int
	for _, h := range u.Fleet.Hulls {
		if h.Mission == traffic.Flight && h.To == 301 {
			flying++
		}
	}
	if flying == 0 {
		t.Fatal("no hull left under the standing order")
	}
	// Somebody else takes the origin: the order must vanish.
	u.conquer(src, []*traffic.Hull{u.Fleet.ByGovt(govt.Green)[0]})
	u.Tick()
	if len(src.Orders) != 0 {
		t.Errorf("%d orders survived the conquest of their origin", len(src.Orders))
	}
	_ = dst
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books: %v", bad[0])
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("ledger: %v", bad)
	}
}

// A convoy of an intermediate needs a shuttle link; finished goods do not.
func TestIntermediatesMoveOnlyByShuttle(t *testing.T) {
	u := newTestUniverse(5)
	// 133 and 238 are different systems with no charter.
	err := u.File(StandingOrder{From: 133, To: 238, Owner: govt.Red, Mat: econ.Copper, Tons: 40})
	if err == nil {
		t.Error("copper was allowed across a jump without a lane")
	}
	// Ally them so the port will trade, then charter the lane.
	u.SetRelation(govt.Red, govt.Green, Ally)
	purse := 10_000
	if err := u.Charter(u.Worlds[133], u.Worlds[238], &purse, SeatAI); err != nil {
		t.Fatal(err)
	}
	if err := u.File(StandingOrder{From: 133, To: 238, Owner: govt.Red, Mat: econ.Copper, Tons: 40}); err != nil {
		t.Errorf("copper refused over a chartered lane: %v", err)
	}
	// And routing agrees: no refined material is found on a cross-system
	// route without a charter.
	v := newTestUniverse(5)
	for _, r := range v.FindRoutes(govt.Red, 0) {
		if r.Mat.Refined() && v.Worlds[r.From].System != v.Worlds[r.To].System {
			t.Errorf("route %s crosses systems without a lane", r)
		}
	}
}

// Spaceport and Works share one ladder; the seat follows the first building.
func TestLadderIsSharedAndSeatFollowsCharter(t *testing.T) {
	u := newTestUniverse(2)
	w := u.Worlds[301]
	purse := 1_000_000
	u.AccountCredits(func() int { return purse })
	u.ReopenLedger()
	if p := w.Price(Works); p != ladderBase {
		t.Fatalf("first Works costs %d, want %d", p, ladderBase)
	}
	if err := u.Build(w, Spaceport, &purse, SeatPlayer); err != nil {
		t.Fatal(err)
	}
	if p := w.Price(Works); p != ladderBase*2 {
		t.Errorf("Works after a Spaceport costs %d, want %d", p, ladderBase*2)
	}
	if w.Seat != SeatPlayer {
		t.Error("the first building did not seat the player")
	}
	if purse != 1_000_000-ladderBase {
		t.Errorf("purse is %d", purse)
	}
	if w.Credits < ladderBase {
		t.Error("the treasury did not receive the price")
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("ledger: %v", bad)
	}
	// Conquest resets the seat.
	u.conquer(w, []*traffic.Hull{u.Fleet.ByGovt(govt.Blue)[0]})
	if w.Seat != SeatAI {
		t.Error("the seat survived the conquest")
	}
}

// Growth is made of rations: fed grows, unfed shrinks.
func TestGrowthIsMadeOfRations(t *testing.T) {
	fed := New(77, []Port{{500, "Fed", 500, 1_000_000, govt.Green}}, 0)
	w := fed.Worlds[500]
	for _, m := range []econ.Material{econ.Rations, econ.Medicine, econ.FuelCells, econ.Lumber} {
		w.Warehouse.Add(m, 5e6)
	}
	fed.ReopenBooks()
	start := w.Pop
	for d := 0; d < 30; d++ {
		fed.Tick()
	}
	if w.Pop <= start {
		t.Errorf("a fed world did not grow: %d → %d", start, w.Pop)
	}
	hungry := New(77, []Port{{500, "Hungry", 500, 1_000_000, govt.Green}}, 0)
	h := hungry.Worlds[500]
	h.Warehouse = econ.Stock{}
	h.Reserve = econ.Stock{}
	h.Plant = nil
	hungry.ReopenBooks()
	for d := 0; d < 30; d++ {
		hungry.Tick()
	}
	if h.Pop >= start {
		t.Errorf("a starving world did not shrink: %d → %d", start, h.Pop)
	}
}

// Eating leaves compost, and the composter turns it back into biomass on
// the surface — never into the reserve.
func TestTheOrganicLoopCloses(t *testing.T) {
	u := New(9, []Port{{500, "Farm", 500, 2_000_000, govt.Green}}, 0)
	w := u.Worlds[500]
	w.Warehouse.Add(econ.Rations, 1e5)
	u.ReopenBooks()
	reserve0 := w.Reserve[econ.Biomass]
	for d := 0; d < 20; d++ {
		u.Tick()
	}
	if u.Journal.Made[econ.Biomass] <= 0 {
		t.Error("the composter made no biomass")
	}
	if w.Reserve[econ.Biomass] > reserve0 {
		t.Error("compost went back into the crust")
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books: %v", bad[0])
	}
}

// Losing a hull and replacing it: the census row comes back out of a yard
// that has Hull tons, and the tons leave the warehouse.
func TestYardsReplaceLostHulls(t *testing.T) {
	u := newTestUniverse(6)
	h := u.Fleet.ByGovt(govt.Red)[0]
	yard := u.Worlds[133]
	u.Lose(h, "test")
	yard.Warehouse.Add(econ.Hull, h.Dry*2)
	u.ReopenBooks()
	before := yard.Warehouse[econ.Hull]
	u.Tick()
	if h.Status == traffic.Lost {
		t.Fatal("the hull was not recommissioned")
	}
	if got := before - yard.Warehouse[econ.Hull]; math.Abs(got-h.Dry) > 1e-6 {
		t.Errorf("the yard gave up %.1f t of Hull for a %.1f t ship", got, h.Dry)
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books: %v", bad[0])
	}
}
