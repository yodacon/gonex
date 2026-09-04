package app

import (
	"math"

	"yodacon.org/gonex/internal/ai"
	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
	"yodacon.org/gonex/internal/world"
)

// The handover: the seam between the universe that is simulated and the
// sector that is drawn.
//
// A hull in internal/traffic is a row in a census — a mass, a thrust, a
// cargo, a lane, and a distance along it. A ship in internal/world is a
// sprite with a gun. They are the same vessel seen from two distances, and
// this file is where one becomes the other and back again.
//
// The rule that makes it honest: A HULL IS IN EXACTLY ONE PLACE. While it is
// Resident the registry stops integrating it — the ship on screen IS the
// simulation of that vessel, and its cargo lives in the world's manifest, not
// in the census row. On the way out the manifest goes back. Nothing is copied
// and left behind in both places, because two copies of a cargo is two copies
// of its mass, and the auditor would find it within a day.

// maxResidents bounds how many census hulls are made flesh at once. The point
// of the handover is that the traffic in your sky is REAL traffic, not that
// the sky is full.
const maxResidents = 8

// residentsIn materialises census hulls berthed at, or bound for, ports in
// the system the player just entered.
//
// They arrive as haulers with their actual manifest aboard, flying the escort
// doctrine — they are traders, not fighters, and they will defend themselves
// and run for a pad rather than go looking. A player who shoots one is
// robbing a specific ship of a specific cargo somebody was expecting, and the
// journal will say so.
func (a *App) residentsIn(sysID int) {
	if a.uni == nil || a.World == nil {
		return
	}
	here := map[int]bool{}
	for _, e := range a.World.Entities {
		if pl, ok := e.(*world.Planet); ok && pl.StellarID > 0 {
			here[pl.StellarID] = true
		}
	}
	if len(here) == 0 {
		return
	}

	n := 0
	for _, h := range a.uni.Fleet.Hulls {
		if n >= maxResidents {
			break
		}
		if h.Status == traffic.Lost || h.Status == traffic.Resident {
			continue
		}
		// A hull belongs in this sky if the port it is sitting at, or the
		// port it is bound for, is standing in it.
		anchor := h.Home
		if h.Status.UnderWay() {
			anchor = h.To
		}
		if !here[anchor] {
			continue
		}
		a.materialise(h, anchor)
		n++
	}
	a.syncPlanetStock()
}

// materialise turns one census row into a ship in the sky.
func (a *App) materialise(h *traffic.Hull, anchor int) {
	var at gmath.Vec2
	found := false
	for _, e := range a.World.Entities {
		if pl, ok := e.(*world.Planet); ok && pl.StellarID == anchor {
			at, found = pl.Pos(), true
			break
		}
	}
	if !found {
		return
	}
	// Off the pad, not on it: a trader arriving is in the circuit, and a ship
	// that pops into existence sitting on a berth looks like a bug even when
	// it is not.
	ang := a.World.Rand.Float64() * 2 * math.Pi
	r := world.CollisionRange * (6 + 4*a.World.Rand.Float64())

	s := a.World.NewShip(traderHull(h), teamOf(h.Govt), h.Name, world.KindNPC)
	s.HullID = h.ID
	s.P = at.Add(gmath.V(math.Cos(ang)*r, math.Sin(ang)*r))
	s.V = gmath.Vec2{}
	s.Role = world.RoleHauler
	s.Outfit(a.World) // the role sets the deck, so re-size the manifest
	s.Controller = ai.Parse("escort", a.World.Rand)

	// The manifest comes ACROSS, it is not copied: the census row is emptied
	// as the world's hold is filled, so the tonnage exists once.
	for m := econ.Material(0); m < econ.Material(econ.BoardWidth); m++ {
		if h.Cargo[m] <= 0 {
			continue
		}
		tons := h.Cargo.Take(m, h.Cargo[m])
		if int(m) < len(s.Hold) {
			s.Hold[m] += int(tons)
			// Whole tons only on this side of the fence; the fraction stays
			// in the census row rather than being rounded into existence.
			h.Cargo.Add(m, tons-math.Trunc(tons))
		} else {
			h.Cargo.Add(m, tons)
		}
	}
	h.Status = traffic.Resident
	a.uni.Journal.Logf(a.uni.Day, h.ID, "%s enters the sector at %s",
		h.Name, a.uni.Worlds[anchor].Name)
}

// releaseResidents hands every materialised hull back to the census. Called
// before the sky is re-dressed for another system, and on shutdown: a hull
// left Resident in a sector nobody is drawing is a hull that has stopped
// moving forever.
func (a *App) releaseResidents() {
	if a.uni == nil || a.World == nil {
		return
	}
	byID := map[int]*traffic.Hull{}
	for _, h := range a.uni.Fleet.Hulls {
		byID[h.ID] = h
	}
	live := a.World.Entities[:0]
	for _, e := range a.World.Entities {
		s, ok := e.(*world.Ship)
		if !ok || s.HullID < 0 || s == a.World.MainPlayer {
			live = append(live, e)
			continue
		}
		h := byID[s.HullID]
		if h == nil || h.Status != traffic.Resident {
			live = append(live, e)
			continue
		}
		// The manifest goes back the way it came.
		for i := 0; i < len(s.Hold) && i < econ.BoardWidth; i++ {
			if s.Hold[i] > 0 {
				h.Cargo.Add(econ.Material(i), float64(s.Hold[i]))
				s.Hold[i] = 0
			}
		}
		h.Status = traffic.Idle
		h.From, h.To = h.Home, h.Home
		h.V, h.S = 0, 0
		a.uni.Journal.Logf(a.uni.Day, h.ID, "%s leaves the sector", h.Name)
		// and the ship leaves the sky with it
	}
	a.World.Entities = live
}

// loseResident is the OnKill hook: a death on this map, entered in the books.
func (a *App) loseResident(s *world.Ship) {
	if a.uni == nil {
		return
	}
	if a.World != nil && s == a.World.MainPlayer {
		// The player's own hold is scattered too — it is accounted matter
		// like anybody's, and dying with a full deck has to cost the tonnage
		// or the wreck is a mint.
		if a.voy != nil {
			for i := range a.voy.Cargo {
				if a.voy.Cargo[i] <= 0 {
					continue
				}
				a.uni.Sink.Add(econ.Material(i), float64(a.voy.Cargo[i]))
				a.voy.Cargo[i] = 0
			}
		}
		return
	}
	if s.HullID < 0 {
		return // scene furniture, never in the census
	}
	for _, h := range a.uni.Fleet.Hulls {
		if h.ID != s.HullID {
			continue
		}
		// The ship's hold is the hull's cargo while it is Resident, so hand
		// it back before the loss scatters it — otherwise Lose scatters an
		// empty row and the tonnage on the deck is simply gone.
		for i := 0; i < len(s.Hold) && i < econ.BoardWidth; i++ {
			if s.Hold[i] > 0 {
				h.Cargo.Add(econ.Material(i), float64(s.Hold[i]))
				s.Hold[i] = 0
			}
		}
		a.uni.Lose(h, "destroyed in "+a.sectorName())
		if a.Console != nil {
			a.Console.Notifyf("%s destroyed — a %s freighter, %s aboard.",
				h.Name, h.Govt, manifestOf(h))
		}
		s.HullID = -1 // the respawned hull is a different vessel
		return
	}
}

// sectorName is where a loss happened, defensively: a death must never be
// the thing that dereferences a missing system three frames deep.
func (a *App) sectorName() string {
	if a.gal == nil || a.voy == nil {
		return "an uncharted sector"
	}
	if sys := a.gal.Systems[a.voy.System]; sys != nil {
		return "the " + sys.Name + " sector"
	}
	return "an uncharted sector"
}

// manifestOf renders what went down with a hull, for the console.
func manifestOf(h *traffic.Hull) string {
	if t := h.Cargo.Total(); t > 0 {
		return h.Cargo.String()
	}
	return "no cargo"
}

// traderHull picks a hull class for a census freighter. Deterministic in the
// hull's own ID, so the same ship is the same shape every time it is seen.
func traderHull(h *traffic.Hull) int { return 1 + h.ID%10 }

func teamOf(c govt.Color) world.Team {
	switch c {
	case govt.Red:
		return world.TeamRed
	case govt.Green:
		return world.TeamGreen
	case govt.Blue:
		return world.TeamBlue
	}
	return world.TeamNone
}
