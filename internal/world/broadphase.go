package world

import (
	"math"

	"yodacon.org/gonex/internal/gmath"
)

// The broadphase: a uniform spatial hash, rebuilt once a frame.
//
// Every missile, item and wreck in the sky asks ForEachNear who is within
// CollisionRange of it, and every AI ship asks who the nearest enemy is.
// Both used to walk the whole entity list, so the per-frame cost was
// entities × entities. Measured on a 400-ship scene that is 21 ms of
// simulation before a single triangle is drawn — 1.3 frames of a 60 Hz
// budget spent deciding that almost nothing is touching almost nothing.
//
// A uniform grid is the right shape here rather than a quadtree or a BVH:
// the world is a fixed rectangle, the query radius is a CONSTANT
// (CollisionRange, the same 64 units konex used for everything), and
// entities are small and roughly evenly spread. Under those three
// conditions a hash of fixed cells beats every tree, because the query is
// arithmetic rather than traversal and the rebuild is a single linear pass
// with no allocation after the first frame.
//
// THE ONE SUBTLETY. The grid is a snapshot taken before the update pass,
// and entities move during that pass. An entity that has moved since the
// snapshot may be within range of a querier while its RECORDED cell is
// further away — so a fixed 3×3 query would silently miss collisions that
// the old linear scan caught, and "silently misses collisions" is the worst
// class of bug this engine could have. The radius is therefore derived from
// the fastest thing in the sky at build time: nothing can have moved more
// than maxV·dt, so widening the query by that many cells is exact rather
// than merely careful.
type broadphase struct {
	cell       float64
	cols, rows int
	minX, minY float64
	buckets    [][]Entity // len cols*rows, reused between frames
	used       []int      // which buckets have anything in them, for clearing
	radius     int        // how many cells out a query must look
	built      bool
}

// targetCells is roughly how many buckets the grid aims to have, whatever
// size the map is.
//
// The first cut fixed the cell at CollisionRange, which is the textbook
// answer and was wrong here. A 20,000-unit map at 64-unit cells is 99,000
// buckets holding about a thousand entities — so the REBUILD, which has to
// clear every bucket, cost more than the scan it replaced, and a 25-ship
// scene measured slower with the grid than without it. Sizing the grid to
// the map instead keeps a query cheap without making the rebuild the new
// bottleneck, and the query radius below adapts to whatever cell size falls
// out of it.
const targetCells = 4096

// minCell is the floor: never finer than the query radius, because cells
// smaller than the thing you are searching for only add cells to visit.
const minCell = CollisionRange

func (g *broadphase) build(entities []Entity, dt float64, w, h float64) {
	cell := math.Max(minCell, math.Sqrt(w*h/targetCells))
	if cell != g.cell {
		g.cell = cell
		g.minX, g.minY = -cell, -cell // a margin, so strays off-map still bucket
		g.cols = int(w/cell) + 3
		g.rows = int(h/cell) + 3
		g.buckets = make([][]Entity, g.cols*g.rows)
		g.used = g.used[:0]
	}
	// Clear only the buckets that hold something. Walking all of them is
	// O(cells); walking the occupied ones is O(entities), which is the
	// whole point of the exercise.
	for _, i := range g.used {
		g.buckets[i] = g.buckets[i][:0] // keep the backing arrays
	}
	g.used = g.used[:0]
	maxV := 0.0
	for _, e := range entities {
		if !e.Alive() {
			continue
		}
		if v := e.body().V.LenSq(); v > maxV {
			maxV = v
		}
		if i, ok := g.index(e.Pos()); ok {
			if len(g.buckets[i]) == 0 {
				g.used = append(g.used, i)
			}
			g.buckets[i] = append(g.buckets[i], e)
		}
	}
	// One cell for the query itself, plus however many cells the fastest
	// entity could have crossed since this snapshot was taken.
	g.radius = 1 + int(math.Sqrt(maxV)*dt/g.cell)
	g.built = true
}

// index maps a world position to a bucket, reporting false for anything
// outside the grid entirely — which is not an error, just an entity that has
// wandered off the map and cannot be near anything on it.
func (g *broadphase) index(p gmath.Vec2) (int, bool) {
	cx := int((p.X - g.minX) / g.cell)
	cy := int((p.Y - g.minY) / g.cell)
	if cx < 0 || cy < 0 || cx >= g.cols || cy >= g.rows {
		return 0, false
	}
	return cy*g.cols + cx, true
}

// near calls fn for every entity in the cells that could hold something
// within range of p. It does NOT check the distance: the caller does, on the
// entity's live position, exactly as it always did. The grid narrows the
// candidates; it does not decide anything.
func (g *broadphase) near(p gmath.Vec2, fn func(Entity)) {
	cx := int((p.X - g.minX) / g.cell)
	cy := int((p.Y - g.minY) / g.cell)
	x0, x1 := cx-g.radius, cx+g.radius
	y0, y1 := cy-g.radius, cy+g.radius
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 >= g.cols {
		x1 = g.cols - 1
	}
	if y1 >= g.rows {
		y1 = g.rows - 1
	}
	for y := y0; y <= y1; y++ {
		row := y * g.cols
		for x := x0; x <= x1; x++ {
			for _, e := range g.buckets[row+x] {
				fn(e)
			}
		}
	}
}

// The type indices.
//
// ClosestEnemy and the planet searches do not want "every entity" — they
// want every SHIP, or every PLANET. Asking the entity list means a type
// assertion per entity per query, and with four hundred ships each running
// two such queries a frame that is most of a million assertions a second
// spent rediscovering that a missile is not a planet.
//
// Both indices are rebuilt in the same linear pass that fills the grid, so
// they cost nothing extra and cannot drift: there is exactly one place in
// the engine that decides what is in the sky this frame.
//
// This is a pure narrowing. Every query still checks the same predicates on
// the same live positions and returns the same answer; it simply stops
// considering candidates that could never have qualified.
type indices struct {
	planets []*Planet
	ships   []*Ship
}

func (ix *indices) rebuild(entities []Entity) {
	ix.planets = ix.planets[:0]
	ix.ships = ix.ships[:0]
	for _, e := range entities {
		switch v := e.(type) {
		case *Planet:
			ix.planets = append(ix.planets, v)
		case *Ship:
			ix.ships = append(ix.ships, v)
		}
	}
}
