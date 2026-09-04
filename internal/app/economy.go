package app

import (
	"fmt"

	"yodacon.org/gonex/internal/city"
	"yodacon.org/gonex/internal/console"
	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/universe"
	"yodacon.org/gonex/internal/world"
)

// The economy behind the sky. internal/universe runs a zero-sum material
// simulation for every port the game knows about — what is in its crust, what
// its factories make of that, what its people eat, and which hulls are
// carrying the difference between them right now. This file is the seam
// between that simulation and the running game: it seeds the universe from
// the same gazetteer the flight view uses, advances it on the same clock as
// the voyage, and puts it on the console where it can be read.

// teamColor maps the battlefield's word for a power onto the economy's.
func teamColor(t world.Team) govt.Color {
	switch t {
	case world.TeamRed:
		return govt.Red
	case world.TeamGreen:
		return govt.Green
	case world.TeamBlue:
		return govt.Blue
	}
	return govt.None
}

// seedUniverse builds the economic model of everywhere from the planets
// actually standing in the world plus every stellar the gazetteer knows.
//
// The seed is the voyage's own seed, so a saved game reopens onto the same
// universe: the same seams under the same worlds, the same industries stood
// up on them, the same hulls with the same names. An economy you cannot
// replay is an economy nobody can balance.
func (a *App) seedUniverse() {
	if a.gal == nil || a.voy == nil {
		return
	}
	seen := map[int]bool{}
	var ports []universe.Port

	// The planets in front of the player first: these carry a team, and the
	// flag over a world is what decides how well it digs and makes.
	if a.World != nil {
		for _, e := range a.World.Entities {
			pl, ok := e.(*world.Planet)
			if !ok || pl.StellarID <= 0 || seen[pl.StellarID] {
				continue
			}
			st := a.gal.Stellars[pl.StellarID]
			sys := a.voy.System
			if st != nil {
				sys = st.System
			}
			seen[pl.StellarID] = true
			ports = append(ports, universe.Port{
				Stellar: pl.StellarID, Name: pl.Label(), System: sys,
				Pop: pl.Pop, Govt: teamColor(pl.Team),
			})
		}
	}
	// Then the rest of the recovered galaxy, so trade has somewhere to go
	// that is not on this map. These are the ports the player never sees and
	// the journal still tracks.
	for id, st := range a.gal.Stellars {
		if seen[id] || st == nil {
			continue
		}
		seen[id] = true
		ports = append(ports, universe.Port{
			Stellar: id, Name: st.Name, System: st.System,
			Pop: city.PopulationOf(id), Govt: govt.None,
		})
	}
	a.uni = universe.New(a.voy.Seed, ports, hullsPerColour)
	a.uniDay = a.voy.Day
}

// hullsPerColour is the fixed census each power starts with. It is small on
// purpose: every one of these is a ship whose position, cargo and status you
// can look up, not a number on a strategic overlay.
const hullsPerColour = 16

// stepUniverse advances the economy to the voyage's day. Landing burns days,
// so a player who flies a long delivery comes back to a market that has moved
// — which is the whole point of simulating the parts of the map nobody is
// looking at.
func (a *App) stepUniverse() {
	if a.uni == nil || a.voy == nil {
		return
	}
	for a.uniDay < a.voy.Day && a.uni.Day < a.uniDay+maxCatchUp {
		a.uni.Tick()
		a.uniDay++
	}
	a.uniDay = a.voy.Day
}

// maxCatchUp bounds a single catch-up so a save restored after a very long
// gap cannot stall the frame while a decade of trade is simulated.
const maxCatchUp = 400

// registerEconomyCommands wires the trade journal onto the developer console.
func (a *App) registerEconomyCommands(c *console.Console) {
	c.Register(func(c *console.Console, _ string) {
		if a.uni == nil {
			c.Printf("- No universe: start a game first.")
			return
		}
		a.stepUniverse()
		c.Printf("- THE UNIVERSE, day %d (seed %d)", a.uni.Day, a.uni.Seed)
		for _, line := range a.uni.Fleet.Report() {
			c.Printf("  %s", line)
		}
		var reserve, warehouse econ.Stock
		for _, id := range a.uni.Order() {
			reserve = reserve.Plus(a.uni.Worlds[id].Reserve)
			warehouse = warehouse.Plus(a.uni.Worlds[id].Warehouse)
		}
		c.Printf("  crust %.0fkt · warehouses %.0fkt · in transit %.0ft · consumed %.0fkt",
			reserve.Total()/1000, warehouse.Total()/1000,
			a.uni.Fleet.CargoAfloat().Total(), a.uni.Sink.Total()/1000)
		c.Printf("  mined %.0fkt · made %.0fkt · delivered %.0fkt over %d voyages",
			a.uni.Journal.Mined.Total()/1000, a.uni.Journal.Made.Total()/1000,
			a.uni.Journal.Delivered.Total()/1000, a.uni.Journal.Voyages)
		if bad := a.uni.Audit(); len(bad) > 0 {
			c.Printf("  BOOKS DO NOT BALANCE: %v", bad[0])
		} else {
			c.Printf("  books balance — every ton accounted for")
		}
	}, "economy", "econ", "trade")

	c.Register(func(c *console.Console, _ string) {
		for _, g := range govt.Colors() {
			c.Printf("- %s", govt.Describe(g))
		}
		c.Printf("- every row sums to %.2f and every column to %.2f: no colour",
			govt.RowTotal, govt.ColTotal)
		c.Printf("  is stronger overall, and no axis is worth more than another.")
	}, "trifecta", "govts", "powers")

	c.Register(func(c *console.Console, arg string) {
		if a.uni == nil {
			c.Printf("- No universe: start a game first.")
			return
		}
		a.stepUniverse()
		n := 14
		fmt.Sscanf(arg, "%d", &n)
		for _, e := range a.uni.Journal.Tail(n) {
			c.Printf("  %s", e)
		}
		if a.uni.Journal.Len() == 0 {
			c.Printf("- The journal is empty; no cargo has moved yet.")
		}
	}, "journal", "log")

	c.Register(func(c *console.Console, arg string) {
		if a.uni == nil {
			c.Printf("- No universe: start a game first.")
			return
		}
		a.stepUniverse()
		for _, g := range govt.Colors() {
			rs := a.uni.FindRoutes(g, 4)
			if len(rs) == 0 {
				c.Printf("- %s: nothing worth carrying today.", g)
				continue
			}
			for _, r := range rs {
				c.Printf("- %-5s %s → %s: %s", g,
					a.uni.Worlds[r.From].Name, a.uni.Worlds[r.To].Name, r)
			}
		}
		_ = arg
	}, "routes", "market")

	c.Register(func(c *console.Console, arg string) {
		if a.uni == nil {
			c.Printf("- No universe: start a game first.")
			return
		}
		a.stepUniverse()
		id := a.nearbyStellar()
		fmt.Sscanf(arg, "%d", &id)
		w := a.uni.Worlds[id]
		if w == nil {
			c.Printf("- usage: world <stellar id> (or fly near one)")
			return
		}
		for _, line := range w.Describe() {
			c.Printf("  %s", line)
		}
		c.Printf("  warehouse: %s", w.Warehouse)
		c.Printf("  crust:     %s", w.Reserve)
	}, "world", "port", "industry")
}
