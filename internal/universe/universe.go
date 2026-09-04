package universe

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// Universe is the whole running economy.
type Universe struct {
	Seed    int64
	Day     int
	Worlds  map[int]*World
	order   []int // stellar IDs, sorted: iteration must be deterministic
	Fleet   *traffic.Registry
	Journal *traffic.Journal
	Books   *econ.Books

	// Sink is where consumed and wasted mass goes. It is a pool like any
	// other and it is handed to the auditor like any other, which is the
	// whole trick: "used up" is a place, not a disappearance.
	Sink econ.Stock

	Rng *rand.Rand
}

// New seeds a universe from a list of ports.
type Port struct {
	Stellar int
	Name    string
	System  int
	Pop     int
	Govt    govt.Color
}

// New builds and seeds the universe, opens its books, and enrols its fleet.
// hullsPer is how many hulls each government starts with; the census is fixed
// from this moment.
func New(seed int64, ports []Port, hullsPer int) *Universe {
	u := &Universe{
		Seed:    seed,
		Worlds:  map[int]*World{},
		Journal: traffic.NewJournal(400),
		Rng:     rand.New(rand.NewSource(seed ^ 0x5DEECE66D)),
	}
	u.Fleet = traffic.NewRegistry(u.Journal)

	var genesis econ.Stock
	for _, p := range ports {
		w := Seed(seed, p.Stellar, p.Name, p.System, p.Pop, p.Govt)
		u.Worlds[p.Stellar] = w
		u.order = append(u.order, p.Stellar)
		genesis = genesis.Plus(w.Genesis())
	}
	sort.Ints(u.order)
	u.Books = econ.NewBooks(genesis)

	u.raiseFleets(hullsPer)
	return u
}

// Order is the deterministic iteration order over worlds. Map iteration in Go
// is randomised, and a simulation that iterates a map is a simulation whose
// results depend on the runtime's mood.
func (u *Universe) Order() []int { return u.order }

// raiseFleets enrols the fixed census. Hull mass and thrust are drawn from
// the seed, so the same universe always has the same ships.
func (u *Universe) raiseFleets(per int) {
	id := 0
	for _, c := range govt.Colors() {
		homes := u.worldsOf(c)
		if len(homes) == 0 {
			continue
		}
		for i := 0; i < per; i++ {
			home := homes[i%len(homes)]
			dry := 220.0 + float64(u.Rng.Intn(680))
			h := &traffic.Hull{
				ID:     id,
				Name:   fmt.Sprintf("%s %02d", c, i+1),
				Govt:   c,
				Status: traffic.Idle,
				Home:   home.Stellar,
				From:   home.Stellar,
				To:     home.Stellar,
				Dry:    dry,
				Thrust: 2200 + float64(u.Rng.Intn(1800)),
			}
			h.Mass = h.Wet()
			u.Fleet.Add(h)
			id++
		}
	}
}

func (u *Universe) worldsOf(c govt.Color) []*World {
	var out []*World
	for _, id := range u.order {
		if w := u.Worlds[id]; w.Govt == c {
			out = append(out, w)
		}
	}
	return out
}

// Pools hands the auditor every place mass can be. Adding a new kind of
// storage anywhere in the game means adding it here, and forgetting to is
// indistinguishable from a leak — which is exactly the pressure that keeps
// the model honest.
func (u *Universe) Pools() []econ.Stock {
	pools := make([]econ.Stock, 0, len(u.Worlds)*2+2)
	for _, id := range u.order {
		w := u.Worlds[id]
		pools = append(pools, w.Reserve, w.Warehouse)
	}
	pools = append(pools, u.Fleet.CargoAfloat(), u.Sink)
	return pools
}

// ReopenBooks re-bases the accounts on whatever is in the universe right
// now. It is for the two moments where the current state is the starting
// state by definition: restoring a save, and setting up a test scenario by
// hand. Calling it at any other time forgives a leak instead of finding one.
func (u *Universe) ReopenBooks() {
	var genesis econ.Stock
	for _, p := range u.Pools() {
		genesis = genesis.Plus(p)
	}
	u.Books = econ.NewBooks(genesis)
}

// Audit proves the books balance right now.
func (u *Universe) Audit() []econ.Discrepancy { return u.Books.Audit(u.Pools()...) }

// --- The day -------------------------------------------------------------

// Tick advances the universe one industrial day. The order is the causal
// order and it matters: you cannot refine ore you have not dug, sell goods
// you have not made, or route a hull to a shortage that this morning's
// production just filled.
func (u *Universe) Tick() {
	u.Day++
	for _, id := range u.order {
		w := u.Worlds[id]
		u.mine(w)
		u.produce(w)
		u.consume(w)
		u.grow(w)
	}
	for _, id := range u.order {
		u.Worlds[id].Reprice()
	}
	u.flyFleet()
}

// mine moves crust into the warehouse. It is the ONLY process in the game
// that increases the amount of usable material anywhere, and it does so by
// draining a finite reserve — which is why the economy is zero-sum in the
// long run rather than merely balanced in the short.
func (u *Universe) mine(w *World) {
	if w.Govt == govt.None {
		return
	}
	popM := float64(w.Pop) / 1e6
	budget := govt.MineRate(w.Govt) * popM

	// Dig what the world's own industry actually wants, richest seam first.
	// A world does not mine silicate it has no furnace for.
	type want struct {
		m    econ.Material
		tons float64
	}
	var wants []want
	for _, m := range econ.Crusts() {
		if w.Reserve[m] <= 0 {
			continue
		}
		need := w.Wants(m)
		if need <= 0 {
			need = 0.15 * budget / 5 // a trickle for trade, even unwanted
		}
		wants = append(wants, want{m, need})
	}
	sort.Slice(wants, func(i, j int) bool {
		if wants[i].tons != wants[j].tons {
			return wants[i].tons > wants[j].tons
		}
		return wants[i].m < wants[j].m
	})

	for _, x := range wants {
		if budget <= 0 {
			break
		}
		take := math.Min(x.tons, budget)
		got := econ.Transfer(&w.Reserve, &w.Warehouse, x.m, take)
		budget -= got
		if got > 0 {
			u.Journal.Mined.Add(x.m, got)
		}
		if w.Reserve[x.m] <= 0 {
			u.Journal.Logf(u.Day, -1, "%s: the %s is worked out", w.Name, x.m)
		}
	}
}

// produce runs every plant on the world for a day.
//
// This is where the module composition earns its keep: the plant is a single
// supermodule, so running it is one loop over its external ports. Whether it
// is a two-stage mill or a four-stage pharmaceutical chain makes no
// difference here, and adding a new industry means adding a Chain, not a
// case.
func (u *Universe) produce(w *World) {
	for _, plant := range w.Plant {
		demand := plant.Demand()
		// The stage runs at whatever fraction of its inputs it can actually
		// find in the warehouse. Short one ingredient, short the whole run.
		rate := 1.0
		for m := econ.Material(0); m < econ.Count; m++ {
			if demand[m] <= 0 {
				continue
			}
			if r := w.Warehouse[m] / demand[m]; r < rate {
				rate = r
			}
		}
		if rate <= 1e-9 {
			continue
		}
		// Consume the inputs. Take exactly what the run needs and no more.
		var drawn float64
		for m := econ.Material(0); m < econ.Count; m++ {
			if demand[m] <= 0 {
				continue
			}
			drawn += w.Warehouse.Take(m, demand[m]*rate)
		}
		// Emit the products.
		supply := plant.Supply()
		var made float64
		for m := econ.Material(0); m < econ.Count; m++ {
			if supply[m] <= 0 {
				continue
			}
			t := supply[m] * rate
			w.Warehouse.Add(m, t)
			u.Journal.Made.Add(m, t)
			made += t
		}
		// Whatever went in and did not come out is slag. Deriving it from
		// the two figures we just measured — rather than from the module's
		// declared Slag — is what makes this exact under any throttle.
		if waste := drawn - made; waste > 0 {
			u.Sink.Add(econ.Slag, waste)
		}
	}
}

// consume is the population living: eating, medicating, burning power and
// building. Everything it takes goes to the sink, still counted.
func (u *Universe) consume(w *World) {
	for _, m := range []econ.Material{econ.Rations, econ.Medicine, econ.FuelCells,
		econ.Lumber, econ.Ore, econ.Chips, econ.Steel} {
		want := w.appetite(m)
		if want <= 0 {
			continue
		}
		got := econ.Consume(&w.Warehouse, &u.Sink, m, want)
		u.Journal.Burned.Add(m, got)
		if got < want*0.5 && w.Pop > 0 {
			// A world that cannot feed itself stops growing; see grow().
			w.shortfall++
		} else if w.shortfall > 0 {
			w.shortfall--
		}
	}
}

// grow compounds population — the demand side of the whole economy, and the
// axis Green leads. A world that could not feed itself does not grow.
func (u *Universe) grow(w *World) {
	if w.Govt == govt.None || w.shortfall > 3 {
		return
	}
	g := govt.GrowthPerDay(w.Govt)
	next := float64(w.Pop) * (1 + g)
	if cap := popCeiling; next > cap {
		next = cap
	}
	w.Pop = int(next)
}

const popCeiling = 3.2e7
