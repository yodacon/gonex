// Package world holds the simulation: entities, movement, combat and scoring.
// It is rendering-free; drawing lives in internal/render and input in
// internal/app, mirroring how konex split game logic from vid_*/in_*.
package world

import (
	"fmt"
	"math/rand"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/ship"
)

// CollisionRange is the fixed proximity konex used for all entity collisions.
const CollisionRange = 64.0

type Team int

const (
	TeamNone Team = iota
	TeamRed
	TeamGreen
	TeamBlue
)

func (t Team) String() string {
	switch t {
	case TeamRed:
		return "Red"
	case TeamGreen:
		return "Green"
	case TeamBlue:
		return "Blue"
	}
	return "None"
}

// Body is the state every entity shares: position and velocity in world
// coordinates (Y up, origin bottom-left, like konex).
type Body struct {
	P gmath.Vec2
	V gmath.Vec2
}

func (b *Body) body() *Body     { return b }
func (b *Body) Pos() gmath.Vec2 { return b.P }

// Entity is anything living in the world. Movement is uniform (pos += vel*dt);
// Update adds per-kind behavior after the move. Embedding Body satisfies the
// body accessor, which is unexported so only this package mutates motion.
type Entity interface {
	Update(w *World, dt float64)
	Pos() gmath.Vec2
	Alive() bool
	body() *Body
}

type World struct {
	Entities []Entity

	// grid is the broadphase, rebuilt at the top of every Update. It is
	// what stops ForEachNear from being a walk over everything in the sky.
	grid broadphase

	// ix indexes the entity list by type, rebuilt in the same pass. It is
	// what stops every nearest-ship and nearest-planet query from type-
	// asserting its way down the whole list.
	ix indices

	MapW     float64
	MapH     float64

	Scores     [4]int // indexed by Team
	MainPlayer *Ship
	ViewShip   *Ship // whose eyes the camera uses (console `viewentity`)
	GodMode    bool

	Catalog *ship.Catalog
	Rand    *rand.Rand
	// Notify surfaces game events (kills, team changes) to the UI layer.
	Notify func(format string, args ...any)

	// OnKill fires when a ship dies, before it respawns. It is how a death
	// on this map reaches the economy above it — a hull with a census row
	// scatters its cargo and is struck off. This package deliberately does
	// not know what is on the other end of it.
	OnKill func(*Ship)

	// ShieldFilter, when set, lets the app's power grid eat a hit before
	// the main player's hull does; it returns the damage that gets through.
	ShieldFilter func(dmg int) int
	// FireGate, when set, is asked before the main player's gun fires — the
	// app spends capacitor charge there, and a dry bank refuses the shot.
	FireGate func() bool

	// OnSalvage fires when the main player's ship is over a wreck pile with
	// something to lift: the voyage's deck is not this ship's hold, so the
	// app does the lifting. OnDebrisMerge fires when the field's cap folds
	// two piles into one, so whoever keeps the identity of a pile's Other
	// tonnage can follow it.
	OnSalvage     func(*Ship, *Debris)
	OnDebrisMerge func(into, from *Debris)
}

func New(catalog *ship.Catalog, seed int64) *World {
	return &World{
		MapW:    10000,
		MapH:    10000,
		Catalog: catalog,
		Rand:    rand.New(rand.NewSource(seed)),
		Notify:  func(string, ...any) {},
	}
}

func (w *World) Add(e Entity) { w.Entities = append(w.Entities, e) }

// Update advances the simulation one fixed step.
func (w *World) Update(dt float64) {
	// The broadphase is a snapshot of where everything is BEFORE anything
	// moves this frame; see broadphase.go for why the query radius is
	// derived from the fastest entity rather than fixed at one cell.
	w.grid.build(w.Entities, dt, w.MapW, w.MapH)
	w.ix.rebuild(w.Entities)

	// Uniform movement first, matching entities_ProcessMovement.
	for _, e := range w.Entities {
		if e.Alive() {
			b := e.body()
			b.P = b.P.Add(b.V.Scale(dt))
		}
	}
	// Entities spawned during update (missiles, explosions) join next frame.
	for _, e := range w.Entities {
		if e.Alive() {
			e.Update(w, dt)
		}
	}
	// Sweep the dead.
	live := w.Entities[:0]
	for _, e := range w.Entities {
		if e.Alive() {
			live = append(live, e)
		}
	}
	w.Entities = live
}

// allShips is the uncached fallback for callers that run outside the update
// loop, where no index has been built yet.
func (w *World) allShips() []*Ship {
	out := make([]*Ship, 0, len(w.Entities))
	for _, e := range w.Entities {
		if s, ok := e.(*Ship); ok {
			out = append(out, s)
		}
	}
	return out
}

// allPlanets is the same fallback for the planet searches.
func (w *World) allPlanets() []*Planet {
	out := make([]*Planet, 0, 16)
	for _, e := range w.Entities {
		if p, ok := e.(*Planet); ok {
			out = append(out, p)
		}
	}
	return out
}

// ForEachNear calls fn for every other live entity within CollisionRange.
//
// Compares SQUARED distances. Every missile, item and wreck in the sky runs
// this sweep every frame, so the square root it used to take was the single
// most expensive instruction in the game.
func (w *World) ForEachNear(src Entity, fn func(Entity)) {
	p := src.Pos()
	const r2 = CollisionRange * CollisionRange
	if !w.grid.built {
		// No snapshot yet — a caller outside the update loop, or a test
		// driving entities by hand. Fall back to the honest linear scan.
		for _, e := range w.Entities {
			if e != src && e.Alive() && e.Pos().Sub(p).LenSq() < r2 {
				fn(e)
			}
		}
		return
	}
	w.grid.near(p, func(e Entity) {
		if e != src && e.Alive() && e.Pos().Sub(p).LenSq() < r2 {
			fn(e)
		}
	})
}

// ClosestEnemy finds the nearest live ship on a different team.
func (w *World) ClosestEnemy(s *Ship) *Ship {
	// Squared throughout: the nearest of a set by distance is the nearest of
	// it by distance squared, so the root is never needed.
	nearestSq := w.MapW * w.MapW
	p := s.Pos()
	var nearest *Ship
	ships := w.ix.ships
	if len(ships) == 0 {
		ships = w.allShips() // no snapshot yet: a test driving entities by hand
	}
	for _, o := range ships {
		if o == s || o.Team == s.Team || !o.Alive() || o.Docked() {
			continue // a ship on a pad cannot be hit, so it is not a target
		}
		if d := o.Pos().Sub(p).LenSq(); d < nearestSq {
			nearestSq, nearest = d, o
		}
	}
	return nearest
}

// SpawnPointFor picks a random spawn point belonging to a team, or nil.
func (w *World) SpawnPointFor(team Team) *SpawnPoint {
	var points []*SpawnPoint
	for _, e := range w.Entities {
		if sp, ok := e.(*SpawnPoint); ok && sp.Team == team {
			points = append(points, sp)
		}
	}
	if len(points) == 0 {
		return nil
	}
	return points[w.Rand.Intn(len(points))]
}

// EntityLabel names an entity kind for the console's listentities.
func EntityLabel(e Entity) string {
	switch e.(type) {
	case *Ship:
		return "SHIP"
	case *Missile:
		return "MISSILE"
	case *Explosion:
		return "EXPLOSION"
	case *Item:
		return "ITEM"
	case *Planet:
		return "PLANET"
	case *SpawnPoint:
		return "SPAWN"
	}
	return "UNKNOWN"
}

// Describe renders one console line per entity.
func (w *World) Describe() []string {
	lines := make([]string, 0, len(w.Entities))
	for i, e := range w.Entities {
		p := e.Pos()
		lines = append(lines, fmt.Sprintf(" %3d %-9s (%0.2f, %0.2f)", i, EntityLabel(e), p.X, p.Y))
	}
	return lines
}
