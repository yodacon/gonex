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
	a.materialiseOrbitDebris()
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
	a.foldDebris()
}

// loseResident is the OnKill hook: a death on this map, entered in the books
// as a WRECK IN THE SKY. The ship's cargo and its structure become a pile
// where it died (world.Debris), drifting on its way, and any ship with room
// that passes lifts from it. Nothing goes to the sink.
func (a *App) loseResident(s *world.Ship) {
	if a.uni == nil || a.World == nil {
		return
	}
	if s == a.World.MainPlayer {
		// The player's own deck scatters too — it is accounted matter like
		// anybody's, and dying with a full deck has to cost the tonnage or
		// the wreck is a mint. It is a pile now, and the player can go back
		// for it.
		if a.voy != nil {
			mix := append([]int(nil), a.voy.Cargo...)
			for i := range a.voy.Cargo {
				a.voy.Cargo[i] = 0
			}
			if total := sumInts(mix); total > 0 {
				a.World.DropDebris(s.P, s.V, mix, s.Junk, 0)
			}
			s.Junk = 0
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
		// The ship's hold is the board half of the hull's cargo while it is
		// Resident; the census row still holds the fractions and anything
		// off the board (a magazine, refined stock). All of it, and the
		// hull's own tonnage as scrap, goes into one pile.
		mix := make([]int, world.CommodityCount)
		for i := 0; i < len(s.Hold) && i < world.CommodityCount; i++ {
			mix[i], s.Hold[i] = s.Hold[i], 0
		}
		other := h.Cargo // fractions and off-board materials keep their identity here
		h.Cargo = econ.Stock{}
		scrap := h.Dry + s.Junk
		s.Junk = 0
		d := a.World.DropDebris(s.P, s.V, mix, scrap, other.Total())
		a.noteOther(d, other)
		a.uni.Strike(h, "destroyed in "+a.sectorName())
		if a.Console != nil {
			a.Console.Notifyf("%s destroyed — a %s freighter; %.0ft of wreckage adrift.",
				h.Name, h.Govt, d.Tons())
		}
		s.HullID = -1 // the respawned hull is a different vessel
		return
	}
}

func sumInts(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

// --- the wreck field's books --------------------------------------------

// noteOther records the identity of a pile's off-board tonnage. The ledger
// is made on first use so a hand-built fixture without seedUniverse works.
func (a *App) noteOther(d *world.Debris, other econ.Stock) {
	if a.debrisOther == nil {
		a.debrisOther = map[*world.Debris]econ.Stock{}
	}
	a.debrisOther[d] = a.debrisOther[d].Plus(other)
}

// residentDebris is every ton adrift in this sector, as a material vector:
// the board mix, the scrap, and the off-board tonnage whose identity the
// app keeps in debrisOther. Registered with the universe at seeding.
func (a *App) residentDebris() econ.Stock {
	var s econ.Stock
	if a.World == nil {
		return s
	}
	for _, d := range a.World.Salvage() {
		s = s.Plus(econ.FromBoard(d.Mix))
		s.Add(econ.Scrap, d.Scrap)
		s = s.Plus(a.debrisOther[d])
	}
	return s
}

// mergeDebrisLedger follows the field's own merges: the identity of the
// off-board tonnage moves with the tons.
func (a *App) mergeDebrisLedger(into, from *world.Debris) {
	if a.debrisOther == nil {
		return
	}
	a.debrisOther[into] = a.debrisOther[into].Plus(a.debrisOther[from])
	delete(a.debrisOther, from)
}

// salvageForPlayer is OnSalvage: the player's deck lifts cargo by the ton up
// to the clamps, and scrap into the junk bay, sold at the next pad.
func (a *App) salvageForPlayer(s *world.Ship, d *world.Debris) {
	if a.voy == nil {
		return
	}
	free := overstuffCap - a.voy.CargoTotal()
	got := 0
	for i := range d.Mix {
		for d.Mix[i] > 0 && free > 0 && i < len(a.voy.Cargo) {
			d.Mix[i]--
			a.voy.Cargo[i]++
			free--
			got++
		}
	}
	if d.Scrap > 0 && s.Junk < playerJunkMax {
		t := math.Min(d.Scrap, playerJunkMax-s.Junk)
		d.Scrap -= t
		s.Junk += t
		got += int(t)
	}
	if got > 0 && a.Console != nil {
		a.Console.Notifyf("Salvage: %d t lifted from the wreck; %.0f t left adrift.", got, d.Tons())
	}
}

// playerJunkMax is how much hull scrap the Yodacon's bay will take.
const playerJunkMax = 40.0

// materialiseOrbitDebris turns the census's orbit piles over this sector's
// ports into wreckage in the sky, beside the planet they were over.
func (a *App) materialiseOrbitDebris() {
	if a.uni == nil || a.World == nil {
		return
	}
	for _, e := range a.World.Entities {
		pl, ok := e.(*world.Planet)
		if !ok || pl.StellarID <= 0 {
			continue
		}
		stock := a.uni.LiftOrbit(pl.StellarID)
		if stock.Total() <= 0 {
			continue
		}
		mix := make([]int, world.CommodityCount)
		for i := 0; i < world.CommodityCount; i++ {
			mix[i] = int(math.Floor(stock[econ.Material(i)]))
			stock[econ.Material(i)] -= float64(mix[i])
		}
		scrap := stock.Take(econ.Scrap, stock[econ.Scrap])
		ang := a.World.Rand.Float64() * 2 * math.Pi
		r := world.CollisionRange * (3 + 2*a.World.Rand.Float64())
		at := pl.Pos().Add(gmath.V(math.Cos(ang)*r, math.Sin(ang)*r))
		d := a.World.DropDebris(at, gmath.Vec2{}, mix, scrap, stock.Total())
		a.noteOther(d, stock)
	}
}

// foldDebris hands every pile in the sky back to the census, in orbit over
// the nearest port. Called whenever this sky stops being drawn.
func (a *App) foldDebris() {
	if a.uni == nil || a.World == nil {
		return
	}
	live := a.World.Entities[:0]
	for _, e := range a.World.Entities {
		d, ok := e.(*world.Debris)
		if !ok {
			live = append(live, e)
			continue
		}
		if !d.Alive() {
			delete(a.debrisOther, d)
			continue
		}
		pool := econ.FromBoard(d.Mix)
		pool.Add(econ.Scrap, d.Scrap)
		pool = pool.Plus(a.debrisOther[d])
		delete(a.debrisOther, d)
		at := a.nearestPortTo(d.P)
		if at <= 0 {
			at = a.dockOrHome()
		}
		a.uni.DropInOrbit(at, &pool)
	}
	a.World.Entities = live
}

// nearestPortTo is the stellar of the closest port with a census record.
func (a *App) nearestPortTo(p gmath.Vec2) int {
	best, bestD := 0, math.MaxFloat64
	for _, e := range a.World.Entities {
		pl, ok := e.(*world.Planet)
		if !ok || pl.StellarID <= 0 || a.uni.Worlds[pl.StellarID] == nil {
			continue
		}
		if d := pl.Pos().Sub(p).Len(); d < bestD {
			best, bestD = pl.StellarID, d
		}
	}
	return best
}

// dockOrHome is a fallback port for wreckage with no planet in the sky.
func (a *App) dockOrHome() int {
	if a.dock != nil {
		return a.dock.stellar
	}
	if cap := a.uni.Capital(a.playerColour()); cap != nil {
		return cap.Stellar
	}
	return a.uni.Order()[0]
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
