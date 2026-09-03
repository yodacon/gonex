package app

import (
	"fmt"
	"strconv"

	"yodacon.org/gonex/internal/ai"
	"yodacon.org/gonex/internal/console"
	"yodacon.org/gonex/internal/world"
)

// registerCommands wires the developer console, keeping konex's command set
// where it still applies.
func (a *App) registerCommands() {
	c := a.Console

	c.Register(func(*console.Console, string) { a.quit() },
		"quit", "exit", "end", "bye", "close", "q")

	c.Register(func(c *console.Console, _ string) {
		a.Cfg.GodMode = !a.Cfg.GodMode
		if a.running() {
			a.World.GodMode = a.Cfg.GodMode
		}
		if a.Cfg.GodMode {
			c.Printf("- God Mode ON")
		} else {
			c.Printf("- God Mode OFF")
		}
	}, "god")

	c.Register(a.withPlayer(func(c *console.Console, _ string, p *world.Ship) {
		c.Printf("- Health: %d Kills: %d", p.Health, p.Frags)
	}), "stat")

	c.Register(a.withPlayer(func(c *console.Console, _ string, p *world.Ship) {
		c.Printf("- Location: %0.2f, %0.2f Heading: %0.2f degrees", p.P.X, p.P.Y, p.Heading)
		c.Printf("- Velocity: %0.2f, %0.2f", p.V.X, p.V.Y)
	}), "loc", "nav", "where", "location", "coord", "coordinates")

	c.Register(func(c *console.Console, _ string) {
		c.Printf("%s", a.uptime())
	}, "uptime")

	c.Register(func(c *console.Console, _ string) {
		if !a.running() {
			c.Printf("- No game running")
			return
		}
		s := a.World.NewShip(a.Catalog.PickRandom(a.World.Rand), world.TeamNone,
			"AI Player", world.KindNPC)
		s.Controller = ai.Random(a.World.Rand)
	}, "spawn")

	c.Register(func(*console.Console, string) { a.toggleFPS() }, "fps")
	c.Register(func(*console.Console, string) { a.hudWin.OnClose() }, "hud")
	c.Register(func(*console.Console, string) { a.miniMapWin.OnClose() }, "minimap")
	c.Register(func(*console.Console, string) { a.targetWin.OnClose() }, "target")

	c.Register(func(*console.Console, string) { a.saveGame() }, "savegame")
	c.Register(func(*console.Console, string) { a.loadGame() }, "loadgame")

	c.Register(func(c *console.Console, _ string) {
		if !a.running() {
			return
		}
		for _, line := range a.World.Describe() {
			c.Printf("%s", line)
		}
	}, "listentities")

	// The war economy readout: who holds what, and how long they can pay
	// for it. This is the screen the supply war is actually fought on.
	c.Register(func(c *console.Console, _ string) {
		if !a.running() {
			return
		}
		for _, e := range a.World.Entities {
			p, ok := e.(*world.Planet)
			if !ok {
				continue
			}
			state := ""
			if p.Starving() {
				state = "  STARVING"
			}
			c.Printf("%-14s %-5s pop %7d  IP %6.1f/%-6.1f  cr %8d  scrap %5.0ft  pad %d/%d%s",
				p.Label(), p.Team, p.Pop, p.IP, p.IPMax(), p.Credits,
				p.Scrap, len(p.Pad), p.Berths(), state)
		}
	}, "planets", "listplanets")

	// A squadron roster: what every pilot is flying with, and under whose
	// orders. "Fleet is dry" is a thing you should be able to look up.
	c.Register(func(c *console.Console, _ string) {
		if !a.running() {
			return
		}
		for _, e := range a.World.Entities {
			s, ok := e.(*world.Ship)
			if !ok || s.Kind != world.KindNPC {
				continue
			}
			orders := "-"
			if s.Controller != nil {
				orders = s.Controller.Name()
			}
			where := "flying"
			if s.Docked() {
				where = fmt.Sprintf("pad %.1fs", s.PadCD)
			}
			c.Printf("%-10s %-5s %-9s %-10s rnd %4d/%-4d hull %3d  batt %3.0f%%  %s",
				s.Name, s.Team, s.Role, orders, s.Rounds, s.RoundsMax,
				s.Health, s.Grid.BattFrac()*100, where)
		}
	}, "fleet", "roster")

	c.Register(func(c *console.Console, _ string) {
		for id := 1; id <= a.Catalog.Count(); id++ {
			s := a.Catalog.Get(id)
			c.Printf("%02d %s Speed(%0.0f) Turn(%0.0f) Accel(%0.0f) Damage(%d)",
				id, s.Name, s.MaxVelocity, s.TurnSpeed, s.Acceleration, s.Damage)
		}
	}, "listships", "shiplist")

	c.Register(func(c *console.Console, args string) {
		if n, err := strconv.Atoi(args); err == nil && n > 0 {
			a.stars.SetCount(n)
			a.Cfg.StarCount = n
			c.Printf("- Star count set to (%d)", n)
		}
	}, "starcount")

	c.Register(a.withPlayer(func(c *console.Console, args string, p *world.Ship) {
		if n, err := strconv.Atoi(args); err == nil && n >= 1 && n <= 3 {
			p.Team = world.Team(n)
			a.Cfg.Team = n
			c.Notifyf("%s is now on team %s", p.Name, p.Team)
		}
	}), "team")

	c.Register(a.withPlayer(func(c *console.Console, args string, p *world.Ship) {
		if n, err := strconv.Atoi(args); err == nil && n >= 1 && n <= a.Catalog.Count() {
			p.ShipID = n
			a.Cfg.PlayerShipID = n
			c.Printf("- Player ship set to ID (%d)", n)
		}
	}), "playership")

	c.Register(func(c *console.Console, args string) {
		if n, err := strconv.Atoi(args); err == nil {
			a.Cfg.AICount = n
			c.Printf("- AI Count Set To (%d)", n)
		}
	}, "aicount")

	c.Register(func(c *console.Console, args string) {
		n, err := strconv.Atoi(args)
		if err != nil || !a.running() || n < 0 || n >= len(a.World.Entities) {
			return
		}
		if s, ok := a.World.Entities[n].(*world.Ship); ok {
			a.World.ViewShip = s
			c.Printf("- Viewing entity (%d)", n)
		}
	}, "viewentity")

	// --- the trader layer ---

	c.Register(func(c *console.Console, args string) {
		if !a.running() || a.voy == nil {
			c.Printf("- No voyage running")
			return
		}
		v := a.voy
		sys := a.gal.Systems[v.System]
		c.Printf("- Day %d in %s (%s)", v.Day, sys.Name, sys.Govt)
		c.Printf("- Credits %d  Fuel %d/%d  Lithium %0.1f kg  Cargo %d t",
			v.Credits, v.Fuel, v.FuelMax, v.Lithium, v.CargoTotal())
		c.Printf("- Hull %0.0f%%  Computer %0.0f%%  Clamps %0.0f%%",
			100-v.Dmg.Hull, 100-v.Dmg.Computer, 100-v.Dmg.Clamps)
		for _, act := range v.Active {
			dest := "?"
			if st := a.gal.Stellars[act.Dest()]; st != nil {
				dest = st.Name
			}
			c.Printf("- MISSION %s -> %s", act.Def.Name, dest)
		}
	}, "voyage", "manifest")

	c.Register(func(c *console.Console, args string) {
		if !a.running() || a.voy == nil {
			c.Printf("- No voyage running")
			return
		}
		id, err := strconv.Atoi(args)
		if err != nil {
			c.Printf("- usage: entry <stellar id>   (e.g. entry 128 for Earth)")
			return
		}
		if a.gal.Stellars[id] == nil {
			c.Printf("- No such stellar (%d)", id)
			return
		}
		a.Console.Toggle()
		a.startEntry(id)
	}, "entry")

	c.Register(func(c *console.Console, args string) {
		if !a.running() || a.voy == nil {
			c.Printf("- No voyage running")
			return
		}
		id, err := strconv.Atoi(args)
		if err != nil || a.gal.Systems[id] == nil {
			c.Printf("- usage: warp <system id>   (e.g. warp 128 for ConEx)")
			return
		}
		a.voy.System = id
		a.enterSystem(id)
	}, "warp")
}

// withPlayer guards commands that need a running game and a main player.
func (a *App) withPlayer(fn func(*console.Console, string, *world.Ship)) console.Handler {
	return func(c *console.Console, args string) {
		if !a.running() || a.World.MainPlayer == nil {
			c.Printf("- No game running")
			return
		}
		fn(c, args, a.World.MainPlayer)
	}
}
