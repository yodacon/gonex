package app

import (
	"fmt"
	"math/rand"

	"yodacon.org/gonex/internal/galaxy"
	"yodacon.org/gonex/internal/mission"
	"yodacon.org/gonex/internal/reentry"
)

// Voyage is the trader's life between fights: where you are, what you owe
// the repair yard, which control bits the 1997 mission machine has set,
// and what you're hauling. It persists across systems and landings.
type Voyage struct {
	Credits int
	Day     int

	System   int // current system ID
	Fuel     int // jump fuel units; one leg costs 100
	FuelMax  int
	Lithium  float64 // kg aboard for the shield
	LiMax    float64
	Lumber   int // tons of trade cargo on the deck
	Dmg      reentry.Damage // carried between entries until repaired
	Bits     mission.Bits
	Active   []mission.Active
	Route    []int // planned jump path, Route[0] == System
	Rng      *rand.Rand
	Notices  []string // one-shot messages for the console
}

const (
	jumpFuel    = 100
	jumpDays    = 3
	landingDays = 1
	fuelPrice   = 1   // credits per unit
	liPrice     = 40  // credits per kg
	homeSystem  = 128 // ConEx (was Levo)
)

func newVoyage(seed int64) *Voyage {
	veh := reentry.Yodacon()
	return &Voyage{
		Credits: 25000, System: homeSystem,
		Fuel: 400, FuelMax: 400,
		Lithium: veh.LiTank, LiMax: veh.LiTank,
		Rng: rand.New(rand.NewSource(seed)),
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
	v.passDays(jumpDays)
	return next, true
}

// passDays burns mission deadlines.
func (v *Voyage) passDays(days int) {
	v.Day += days
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
	v.passDays(landingDays)
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
