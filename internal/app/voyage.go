package app

import (
	"fmt"
	"math/rand"
	"sort"

	"yodacon.org/gonex/internal/galaxy"
	"yodacon.org/gonex/internal/market"
	"yodacon.org/gonex/internal/mission"
	"yodacon.org/gonex/internal/power"
	"yodacon.org/gonex/internal/reentry"
	"yodacon.org/gonex/internal/save"
)

// Voyage is the trader's life between fights: where you are, what you owe
// the repair yard, which control bits the 1997 mission machine has set,
// and what you're hauling. It persists across systems and landings.
type Voyage struct {
	Credits int
	Day     int

	System  int // current system ID
	Fuel    int // jump fuel units; one leg costs 100
	FuelMax int
	Lithium float64 // kg aboard for the shield
	LiMax   float64
	RCSFuel float64 // kg of attitude propellant aboard
	RCSMax  float64
	Cargo   []int          // tons aboard per market commodity
	Events  []market.Event // the news moving the prices
	Grid    *power.Grid    // the ship's power plant — one grid, every mode
	Dmg     reentry.Damage // carried between entries until repaired
	Bits    mission.Bits
	Active  []mission.Active
	Route   []int // planned jump path, Route[0] == System
	Escorts []int // hired escort ship IDs, wingmen on every launch
	Crew    int   // deckhands on the payroll
	Rng     *rand.Rand
	Notices []string // one-shot messages for the console
}

// CargoTotal is the tons on the deck across the whole board.
func (v *Voyage) CargoTotal() int {
	t := 0
	for _, n := range v.Cargo {
		t += n
	}
	return t
}

const (
	jumpFuel    = 100
	jumpDays    = 3
	landingDays = 1
	fuelPrice   = 1   // credits per unit
	liPrice     = 40  // credits per kg
	homeSystem  = 128 // ConEx (was Levo)

	crewWage   = 20  // credits per crew per day
	escortWage = 60  // credits per escort per day
	crewHire   = 500 // signing bonus at the bar
)

func newVoyage(seed int64) *Voyage {
	veh := reentry.Yodacon()
	return &Voyage{
		Credits: 25000, System: homeSystem,
		Fuel: 400, FuelMax: 400,
		Lithium: veh.LiTank, LiMax: veh.LiTank,
		RCSFuel: veh.RCSTank, RCSMax: veh.RCSTank,
		Cargo: make([]int, len(market.Commodities)),
		Grid:  power.Stock(),
		Rng:   rand.New(rand.NewSource(seed)),
	}
}

func (v *Voyage) notify(format string, args ...any) {
	v.Notices = append(v.Notices, fmt.Sprintf(format, args...))
}

// NextJump is the next system on the planned route, or -1.
func (v *Voyage) NextJump() int {
	if len(v.Route) >= 2 && v.Route[0] == v.System {
		return v.Route[1]
	}
	return -1
}

// Jump commits one leg down the planned route.
func (v *Voyage) Jump(g *galaxy.Galaxy) (int, bool) {
	next := v.NextJump()
	if next < 0 || v.Fuel < jumpFuel {
		return -1, false
	}
	v.Fuel -= jumpFuel
	v.System = next
	v.Route = v.Route[1:]
	v.passDays(jumpDays, g)
	return next, true
}

// passDays burns mission deadlines, pays the payroll, and runs the market
// news wire.
func (v *Voyage) passDays(days int, g *galaxy.Galaxy) {
	v.Day += days
	if wages := days * (crewWage*v.Crew + escortWage*len(v.Escorts)); wages > 0 {
		v.Credits -= wages
		if v.Credits < 0 {
			v.Credits = 0
			v.notify("Payroll bounced — the crew is drinking on your name.")
		}
	}
	if g != nil {
		var systems []int
		for id := range g.Systems {
			systems = append(systems, id)
		}
		sort.Ints(systems)
		var news []string
		v.Events, news = market.Step(v.Events, days, systems, v.Rng, func(id int) string {
			if s := g.Systems[id]; s != nil {
				return s.Name
			}
			return "an outlying system"
		})
		for _, n := range news {
			v.notify("%s", n)
		}
	}
	kept := v.Active[:0]
	for i := range v.Active {
		if v.Active[i].PassDays(days) == mission.Failed {
			v.notify("MISSION FAILED (out of time): %s", v.Active[i].Def.Name)
			continue
		}
		kept = append(kept, v.Active[i])
	}
	v.Active = kept
}

// confederationStellars lists candidate stellars for the 10000+n random
// codes: govt 128 is the Confederation in both base EV and ConEx.
func confederationStellars(g *galaxy.Galaxy) []int {
	var out []int
	for id, st := range g.Stellars {
		if st.Govt == "Confederation" {
			out = append(out, id)
		}
	}
	return out
}

// LandAt resolves a successful landing at a stellar: mission pickups and
// completions, the day cost, and pay.
func (v *Voyage) LandAt(stellar int, g *galaxy.Galaxy) {
	v.passDays(landingDays, g)
	kept := v.Active[:0]
	for i := range v.Active {
		a := &v.Active[i]
		switch a.Land(stellar) {
		case mission.PickedUp:
			v.notify("Loaded %d t %s for %s", a.Def.CargoQty, a.Def.CargoName, a.Def.Name)
			kept = append(kept, *a)
		case mission.Completed:
			if v.Dmg.Clamps > 50 && a.Def.CargoQty > 0 {
				v.notify("MISSION FAILED: cargo clamps damaged, the %s spoiled — %s",
					a.Def.CargoName, a.Def.Name)
				continue
			}
			v.Credits += a.Def.Pay
			mission.Complete(a.Def, &v.Bits)
			v.notify("MISSION COMPLETE: %s — %d cr", a.Def.Name, a.Def.Pay)
		default:
			kept = append(kept, *a)
		}
	}
	v.Active = kept
}

// RepairCost prices the outstanding damage.
func (v *Voyage) RepairCost() int {
	return int(v.Dmg.Hull*900 + v.Dmg.Computer*300 + v.Dmg.Clamps*200)
}

// pilotState snapshots the voyage for the save file.
func (v *Voyage) pilotState(playerShipID, dockStellar int) *save.PilotState {
	ps := &save.PilotState{
		Credits: v.Credits, Day: v.Day, System: v.System,
		Fuel: v.Fuel, FuelMax: v.FuelMax,
		Lithium: v.Lithium, LiMax: v.LiMax,
		RCSFuel: v.RCSFuel, RCSMax: v.RCSMax,
		Cargo: v.Cargo, Events: v.Events,
		Grid: v.Grid, Dmg: v.Dmg,
		Active: v.Active, Route: v.Route,
		Escorts: v.Escorts, Crew: v.Crew,
		PlayerShipID: playerShipID, DockStellar: dockStellar,
	}
	for i := range v.Bits {
		if v.Bits[i] {
			ps.BitsSet = append(ps.BitsSet, i)
		}
	}
	return ps
}

// voyageFrom rebuilds a voyage from a saved pilot state.
func voyageFrom(ps *save.PilotState, seed int64) *Voyage {
	v := &Voyage{
		Credits: ps.Credits, Day: ps.Day, System: ps.System,
		Fuel: ps.Fuel, FuelMax: ps.FuelMax,
		Lithium: ps.Lithium, LiMax: ps.LiMax,
		RCSFuel: ps.RCSFuel, RCSMax: ps.RCSMax,
		Cargo: ps.Cargo, Events: ps.Events,
		Grid: ps.Grid, Dmg: ps.Dmg,
		Active: ps.Active, Route: ps.Route,
		Escorts: ps.Escorts, Crew: ps.Crew,
		Rng: rand.New(rand.NewSource(seed)),
	}
	if v.Grid == nil {
		v.Grid = power.Stock()
	}
	if v.RCSMax == 0 {
		v.RCSMax = reentry.Yodacon().RCSTank
		v.RCSFuel = v.RCSMax
	}
	if len(v.Cargo) != len(market.Commodities) {
		fixed := make([]int, len(market.Commodities))
		copy(fixed, v.Cargo)
		v.Cargo = fixed
	}
	for _, i := range ps.BitsSet {
		v.Bits.Set(i)
	}
	return v
}
