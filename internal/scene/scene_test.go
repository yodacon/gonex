package scene

import (
	"math/rand"
	"testing"

	"yodacon.org/gonex/internal/ai"
	"yodacon.org/gonex/internal/galaxy"
	"yodacon.org/gonex/internal/market"
	"yodacon.org/gonex/internal/ship"
	"yodacon.org/gonex/internal/world"
)

func load(t *testing.T, path string) *world.World { return loadSeed(t, path, 1) }

func loadSeed(t *testing.T, path string, seed int64) *world.World {
	t.Helper()
	cat, err := ship.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	w := world.New(cat, seed)
	if err := Load(w, path); err != nil {
		t.Fatal(err)
	}
	return w
}

// CommodityCount is pinned in package world, which cannot import market
// without dragging the trading UI into the simulation. This is the assert
// that keeps the two honest.
func TestManifestWidthMatchesTheMarket(t *testing.T) {
	if world.CommodityCount != len(market.Commodities) {
		t.Fatalf("world.CommodityCount = %d, market board has %d",
			world.CommodityCount, len(market.Commodities))
	}
}

// The bug this whole milestone starts from: the map has always declared
// teamed planets and the loader threw the attribute away.
func TestPlanetsCarryTheirTeamAndIndustry(t *testing.T) {
	w := load(t, "deathmatch.xml")

	byTeam := map[world.Team]int{}
	for _, e := range w.Entities {
		p, ok := e.(*world.Planet)
		if !ok {
			continue
		}
		byTeam[p.Team]++
		if p.Team == world.TeamNone {
			continue
		}
		if p.Name == "" {
			t.Error("a held planet has no name")
		}
		if p.Pop <= 0 {
			t.Errorf("%s holds no population", p.Name)
		}
		if p.IP != p.IPMax() || p.IPMax() <= 0 {
			t.Errorf("%s did not start with a full buffer: %.1f/%.1f",
				p.Name, p.IP, p.IPMax())
		}
		if p.Starving() {
			t.Errorf("%s starts starving", p.Name)
		}
		if len(p.Stock) != world.CommodityCount {
			t.Errorf("%s warehouse is %d wide", p.Name, len(p.Stock))
		}
	}
	for _, team := range []world.Team{world.TeamRed, world.TeamGreen, world.TeamBlue} {
		if byTeam[team] != 4 {
			t.Errorf("team %s holds %d planets, want 4", team, byTeam[team])
		}
	}
	if byTeam[world.TeamNone] != 4 {
		t.Errorf("%d neutral planets, want 4", byTeam[world.TeamNone])
	}
}

// Every stellar a map names has to be one the gazetteer actually knows.
// Getting this wrong is not a cosmetic error: the flight banner looks the
// stellar up by ID the moment the player is in range of the planet, and a
// map that cites a landing-picture ID instead of a stellar ID crashes the
// game the instant you fly near your own capital.
func TestSceneStellarsExistInTheGazetteer(t *testing.T) {
	gal, err := galaxy.Load()
	if err != nil {
		t.Fatal(err)
	}
	w := load(t, "deathmatch.xml")
	named := 0
	for _, e := range w.Entities {
		p, ok := e.(*world.Planet)
		if !ok || p.StellarID == 0 {
			continue
		}
		named++
		st := gal.Stellars[p.StellarID]
		if st == nil {
			t.Errorf("%s cites stellar %d, which is not in the gazetteer",
				p.Label(), p.StellarID)
			continue
		}
		if st.Name != p.Label() {
			t.Errorf("%s cites stellar %d, which the gazetteer calls %q",
				p.Label(), p.StellarID, st.Name)
		}
	}
	if named != 3 {
		t.Errorf("%d planets cite a stellar, want the 3 capitals", named)
	}
}

// The capital's population comes from the city that grows on it, so it is
// bigger than the hand-set outposts and differs from the other capitals.
func TestCapitalsArePopulatedFromTheirCities(t *testing.T) {
	w := load(t, "deathmatch.xml")
	caps := map[string]int{}
	for _, e := range w.Entities {
		if p, ok := e.(*world.Planet); ok && p.StellarID > 0 {
			caps[p.Name] = p.Pop
		}
	}
	if len(caps) != 3 {
		t.Fatalf("found %d capitals, want 3", len(caps))
	}
	seen := map[int]bool{}
	for name, pop := range caps {
		if pop < 1000000 {
			t.Errorf("%s is a village: %d", name, pop)
		}
		if seen[pop] {
			t.Errorf("%s shares a population with another capital", name)
		}
		seen[pop] = true
	}
}

func TestSquadronsFlyDifferentOrders(t *testing.T) {
	w := load(t, "deathmatch.xml")

	orders := map[string]int{}
	ships, armed := 0, 0
	aggro := map[float64]bool{}
	for _, e := range w.Entities {
		s, ok := e.(*world.Ship)
		if !ok {
			continue
		}
		ships++
		if s.Rounds > 0 && s.Rounds == s.RoundsMax {
			armed++
		}
		d, ok := s.Controller.(*ai.Doctrine)
		if !ok {
			t.Fatalf("%s has no doctrine", s.Name)
		}
		orders[d.Name()]++
		aggro[d.Aggro] = true
	}
	if ships != 72 {
		t.Errorf("%d pilots, want 72", ships)
	}
	if armed != ships {
		t.Errorf("%d of %d pilots launched loaded", armed, ships)
	}
	// Ten guards, six after each of two colours, two on the deck — per team.
	if orders["guard"] != 30 {
		t.Errorf("%d guards, want 30", orders["guard"])
	}
	if orders["escort"] != 6 {
		t.Errorf("%d escorts, want 6", orders["escort"])
	}
	for _, inv := range []string{"invade:1", "invade:2", "invade:3"} {
		if orders[inv] != 12 {
			t.Errorf("%d pilots on %s, want 12", orders[inv], inv)
		}
	}
	// Every pilot draws its own tunings: 72 ships must not share one number.
	if len(aggro) < 60 {
		t.Errorf("only %d distinct engagement ranges across 72 pilots", len(aggro))
	}
}

func TestHaulersCarryMoreThanWarships(t *testing.T) {
	w := load(t, "deathmatch.xml")
	var hauler *world.Ship
	haulers := 0
	for _, e := range w.Entities {
		if s, ok := e.(*world.Ship); ok && s.Role == world.RoleHauler {
			haulers++
			hauler = s
		}
	}
	if haulers != 6 {
		t.Fatalf("%d haulers in the roster, want two a colour", haulers)
	}

	// Same hull, other role: the only difference is the deck.
	warship := w.NewShip(hauler.ShipID, world.TeamRed, "control", world.KindNPC)
	if hauler.HoldMax <= warship.HoldMax {
		t.Errorf("hauler deck %.0f t is no bigger than a warship's %.0f t",
			hauler.HoldMax, warship.HoldMax)
	}
	if warship.HoldMax <= 0 {
		t.Error("a warship has no hold at all — even a fighter carries cargo")
	}
	// Role changes the deck and nothing else: same hull, same magazine rule.
	if want := 40 + 6*hauler.Crew + int(w.Catalog.Get(hauler.ShipID).Mass)/100; hauler.RoundsMax != want {
		t.Errorf("hauler magazine %d, want %d", hauler.RoundsMax, want)
	}
}

func TestUnknownOrdersFallBackToEscort(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for _, name := range []string{"rabies", "siege", "", "nonsense"} {
		if got := ai.Parse(name, r).Name(); got != "escort" {
			t.Errorf("ai.Parse(%q) = %q, want escort", name, got)
		}
	}
	if got := ai.Parse("invade:3", r).Name(); got != "invade:3" {
		t.Errorf("invade:3 round-tripped as %q", got)
	}
}
