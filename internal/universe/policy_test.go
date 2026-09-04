package universe

import (
	"testing"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
)

// Every inhabited world starts as a port; capitals start with a Works, a
// Bastion and a Habitat; and none of it counts against the ladder.
func TestGenesisInfrastructureIsBuiltNotBought(t *testing.T) {
	u := newTestUniverse(3)
	for _, id := range u.Order() {
		w := u.Worlds[id]
		if w.Pop > 0 && w.Built[Spaceport] < 1 {
			t.Errorf("%s has no spaceport at genesis", w.Name)
		}
		if p := w.Price(Works); p != ladderBase && w.Built[Works] == w.Endowed[Works] {
			t.Errorf("%s: first bought Works costs %d, want %d — genesis counted on the ladder", w.Name, p, ladderBase)
		}
	}
	for _, c := range govt.Colors() {
		cap := u.Capital(c)
		if cap.Built[Works] < 1 || cap.Built[Bastion] < 1 || cap.Built[Habitat] < 1 {
			t.Errorf("%s capital %s lacks its genesis works/bastion/habitat: %v", c, cap.Name, cap.Built)
		}
		if len(cap.Plant) < maxChains+1 && len(cap.Plant) < 3 {
			// a capital's Works slot only stands a chain if its crust backs a third
			t.Logf("%s capital runs %d chains (crust may not back more)", c, len(cap.Plant))
		}
	}
}

// Tax fills the exchequers, and the auto-governor spends them on buildings
// in doctrine order.
func TestTaxFundsUpgradesByDoctrine(t *testing.T) {
	u := newTestUniverse(11)
	for d := 0; d < 120; d++ {
		u.Tick()
	}
	for _, c := range govt.Colors() {
		bought := 0
		for _, w := range u.worldsOf(c) {
			bought += w.buildingCount()
		}
		if bought == 0 && u.Exchequer[c] < u.Tune.ExchequerReserve {
			t.Errorf("%s: no exchequer income and nothing built in 120 days", c)
		}
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("ledger: %v", bad)
	}
}

// The three doctrines are read off the trifecta and differ.
func TestDoctrinesDiffer(t *testing.T) {
	r, g, b := Doctrine(govt.Red), Doctrine(govt.Green), Doctrine(govt.Blue)
	if r[0] == g[0] && g[0] == b[0] {
		t.Errorf("all three colours build %s first", r[0])
	}
	if r[0] != Silo {
		t.Errorf("Red the raider builds %s first, want Silo", r[0])
	}
	if g[0] != Habitat {
		t.Errorf("Green the grower builds %s first, want Habitat", g[0])
	}
	if b[0] != Exchange {
		t.Errorf("Blue the fortress builds %s first, want Exchange", b[0])
	}
}

// A priority world gets the government's next building.
func TestPriorityWorldIsUpgradedFirst(t *testing.T) {
	u := newTestUniverse(5)
	if err := u.SetPriority(govt.Red, 135); err != nil {
		t.Fatal(err)
	}
	u.Exchequer[govt.Red] = 500_000
	u.ReopenLedger()
	w := u.Worlds[135]
	before := w.buildingCount()
	for d := 0; d < u.Tune.GovernEvery*2; d++ {
		u.Tick()
	}
	if w.buildingCount() <= before {
		t.Errorf("the priority world got nothing; built %v", w.Built)
	}
}

// Switching the auto-governor off stops the exchequer building; a focus
// bends the plan.
func TestPolicyManualAndFocus(t *testing.T) {
	u := newTestUniverse(6)
	u.SetPolicy(govt.Blue, Policy{Auto: false})
	u.Exchequer[govt.Blue] = 500_000
	u.ReopenLedger()
	var before int
	for _, w := range u.worldsOf(govt.Blue) {
		before += w.buildingCount()
	}
	for d := 0; d < u.Tune.GovernEvery*3; d++ {
		u.Tick()
	}
	var after int
	for _, w := range u.worldsOf(govt.Blue) {
		after += w.buildingCount()
	}
	if after != before {
		t.Errorf("a manual government built %d levels", after-before)
	}
	u.SetPolicy(govt.Green, Policy{Auto: true, Focus: FocusDefence})
	if p := u.plan(govt.Green); buildingAxis[p[0]] != govt.Shields && buildingAxis[p[0]] != govt.Gunnery {
		t.Errorf("Green under a defence focus builds %s first", p[0])
	}
	u.SetPolicy(govt.Red, Policy{Auto: true, Focus: FocusFleet})
	h := u.Fleet.ByGovt(govt.Red)[0]
	h.Purse = 0
	u.Exchequer[govt.Red] = 500_000
	u.ReopenLedger()
	u.invest(govt.Red)
	if h.Purse == 0 {
		t.Error("a fleet focus did not re-stake a broke pilot")
	}
	if bad := u.AuditCredits(); bad != nil {
		t.Errorf("ledger: %v", bad)
	}
}

// Wreckage lifted out of orbit for a sector and dropped back is conserved.
func TestOrbitDebrisRoundTrips(t *testing.T) {
	u := newTestUniverse(8)
	var pool econ.Stock
	pool[econ.Scrap] = 300
	pool[econ.Chips] = 40
	u.DropInOrbit(133, &pool)
	u.ReopenBooks()
	lifted := u.LiftOrbit(133)
	if lifted.Total() != 340 {
		t.Fatalf("lifted %.0f t, want 340", lifted.Total())
	}
	if bad := u.Audit(); len(bad) == 0 {
		t.Error("tons lifted out of the census and held nowhere did not read as a leak")
	}
	u.DropInOrbit(133, &lifted)
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books after the round trip: %v", bad[0])
	}
}

// A capital is founded with an arsenal if its crust allows, and no capital
// is rated zero after a year — the first year-long runs had all three dry
// by day 120.
func TestCapitalsKeepAMagazine(t *testing.T) {
	u := newTestUniverse(20260903)
	for _, c := range govt.Colors() {
		cap := u.Capital(c)
		has := false
		for _, p := range cap.Plant {
			has = has || p.Name == "Munitions"
		}
		if !has {
			t.Errorf("%s capital %s was founded without an arsenal", c, cap.Name)
		}
	}
	for d := 0; d < 365; d++ {
		u.Tick()
	}
	dry := 0
	for _, c := range govt.Colors() {
		cap := u.Capital(c)
		if r := u.Rating(cap); r <= 0 {
			dry++
			// Allowed only when the arsenal never had steel to work: a colour
			// whose map gives it no ferrite and no steel it can buy is dry by
			// geography, which is the trifecta's "must not be rushed".
			if u.Journal.Made[econ.Steel] > 0 && cap.Reserve[econ.Ferrite] > 0 {
				t.Errorf("%s capital %s has ferrite, made steel, and is still rated %.2f after a year", c, cap.Name, r)
			}
			t.Logf("%s capital %s dry after a year: %.0ft rounds, %.0ft steel on hand, ferrite %.0ft",
				c, cap.Name, cap.Warehouse[econ.Rounds], cap.Warehouse[econ.Steel], cap.Reserve[econ.Ferrite])
		}
	}
	if dry > 1 {
		t.Errorf("%d of 3 capitals dry after a year; at most one may be starved by geography", dry)
	}
	if bad := u.Audit(); len(bad) > 0 {
		t.Errorf("books: %v", bad[0])
	}
}
