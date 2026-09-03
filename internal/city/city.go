// Package city grows the landing site: a procedural spaceport metropolis
// seeded per stellar, after ascitty's seed-world construction
// (~/code/ascitty, docs/city.md) with the placement arranged for a port.
// The formula kept from ascitty: a street axis GENERATED as classed roads
// with gap-after-depends-on-class (hierarchy, not arithmetic), blocks
// subdivided into lots, building height = falloff-from-core + value noise
// drawn through a skewed four-band distribution, a narrow district palette
// instead of per-building confetti, and windows that are a hash of
// (lot, floor, bay) — sampled, never stored.
//
// The port layout: TWO parallel landing roads, each the best part of a
// kilometre kerb to kerb, with districts on both banks of each — a ship on
// final drops into a canyon of buildings with its street between them.
// Runway A's centreline is the pad line the corridor measures crossrange
// from: you land on it and take off down it. Neither road carries
// buildings or traffic.
//
// The whole port stands on a CIRCLE OF LAND: the city is on land, never on
// the shore, and the water starts a LandRadius out — drawn not as a fill
// but as the shoreline repeated in parallel contour tracings running out
// to the ocean.
//
// Units are kilometres, in scale with the landing world: lat 0 is runway
// A's centreline, ahead 0 the touchdown threshold, + along the landing
// direction.
package city

import (
	"math"
	"sort"
)

// The numbers (ascitty's, rescaled ×10 for the metropolis).
const (
	CellKm    = 0.006 // a cell is ~6 m
	FloorKm   = 0.003 // a floor is 3 m
	RunwayWKm = 0.84  // a landing road: 840 m kerb to kerb

	RunwayFrom = -4.0 // km, runway extent around the threshold
	RunwayTo   = 10.0
	Runway2Lat = 2.2 // runway B's centreline, right of A

	TownFrom = -5.0 // km, city extent along track
	TownTo   = 8.0

	// the circle of land: water never nearer than this to the city —
	// island or continent, the port is always on land, never on shore
	LandRadius = 13.0
	ShoreLat   = LandRadius // legacy alias: where land ends, far out
)

// district strips, lat-wise: both banks of both runways
var strips = [3][2]float64{
	{-9.0, -RunwayWKm/2 - 0.12},                           // the deep left bank
	{RunwayWKm/2 + 0.12, Runway2Lat - RunwayWKm/2 - 0.12}, // the median
	{Runway2Lat + RunwayWKm/2 + 0.12, 9.5},                // the right bank
}

type Building struct {
	Lat, Ahead float64 // km, near corner (max lat, min ahead)
	W, D       float64 // km footprint (lat span, ahead span)
	H          float64 // km height
	Hue        int     // district palette index
	Bright     float64 // 0.5..1.3
	Occ        float64 // fraction of windows lit
	Seed       uint32
}

type Lamp struct {
	Lat, Ahead float64
}

// Street is a town road parallel to the runways (traffic runs along it).
type Street struct {
	Lat  float64 // centreline
	W    float64
	Cars int
}

type Port struct {
	Seed      int64
	Buildings []Building // ascending Ahead: walk backwards for far→near
	Lamps     []Lamp
	Streets   []Street
	CoreLat   float64 // town core, for the skyline falloff
}

// --- ascitty-style integer hashing: transcribable, no floats needed ------

func hash2(seed int64, i, j int) uint32 {
	h := uint32(seed) ^ uint32(i)*374761393 ^ uint32(j)*668265263
	h = (h ^ (h >> 13)) * 1274126177
	return h ^ (h >> 16)
}

func unit(h uint32) float64 { return float64(h%1024) / 1024 }

// noise is the smooth value-noise field that puts the secondary height
// clusters somewhere different in every city (two thirds falloff, one
// third this).
func noise(seed int64, x, y float64) float64 {
	xi, yi := math.Floor(x), math.Floor(y)
	fx, fy := x-xi, y-yi
	sx, sy := fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	i, j := int(xi), int(yi)
	a := unit(hash2(seed, i, j))
	b := unit(hash2(seed, i+1, j))
	c := unit(hash2(seed, i, j+1))
	d := unit(hash2(seed, i+1, j+1))
	return a + (b-a)*sx + (c+(d-c)*sx-a-(b-a)*sx)*sy
}

// axis generates one street axis the ascitty way: a list of roads with a
// class, a width, and a gap after it that depends on the class — a bigger
// road gets a bigger block after it, which is what makes the hierarchy
// visible from the ground. mul scales the whole rhythm for the metropolis.
func axis(seed int64, salt int, from, to, mul float64) []float64 {
	var cuts []float64
	pos := from
	i := 0
	for pos < to {
		cuts = append(cuts, pos)
		h := hash2(seed, salt, i)
		var gap float64
		switch {
		case h%10 < 1: // boulevard: a long gap after
			gap = (0.16 + unit(h>>4)*0.10) * mul
		case h%10 < 4: // avenue
			gap = (0.11 + unit(h>>4)*0.07) * mul
		default: // street: short gaps, the fabric
			gap = (0.07 + unit(h>>4)*0.05) * mul
		}
		if gap < CellKm*10 { // MIN_BLOCK: below this no building has a back
			gap = CellKm * 10
		}
		pos += gap
		i++
	}
	return cuts
}

// height draws from the skewed four-band distribution — uniform reads as
// noise; real heights are closer to a power law.
func height(h uint32, ceil float64) float64 {
	r := unit(h)
	switch {
	case r < 0.52: // the fabric
		return ceil * (0.08 + unit(h>>7)*0.12)
	case r < 0.84: // mid-rise, the bulk of a skyline
		return ceil * (0.20 + unit(h>>7)*0.30)
	case r < 0.985: // towers
		return ceil * (0.50 + unit(h>>7)*0.50)
	default: // a landmark
		return ceil * (1.0 + unit(h>>7)*0.5)
	}
}

// genBlocks fills one district strip: blocks between the strip's lat cuts
// and the shared cross streets, one in nine left open as a park, the rest
// subdivided into lots — the ascitty block filler, at metropolis scale.
func (p *Port) genBlocks(seed int64, latCuts, aheadCuts []float64) {
	for li := 0; li+1 < len(latCuts); li++ {
		for ai := 0; ai+1 < len(aheadCuts); ai++ {
			bh := hash2(seed, li*97, ai)
			if bh%9 == 0 {
				continue // park
			}
			l0, l1 := latCuts[li]+CellKm*4, latCuts[li+1]-CellKm*4
			a0, a1 := aheadCuts[ai]+CellKm*4, aheadCuts[ai+1]-CellKm*4
			if l1-l0 < CellKm*8 || a1-a0 < CellKm*8 {
				continue
			}
			// the falloff from the core plus the noise field sets the
			// ceiling; the metropolis carries a real skyline
			core := math.Hypot((l0+l1)/2-p.CoreLat, (a0+a1)/2*0.7) / 1.6
			fall := math.Max(1-core*0.24, 0.12)
			ceil := 0.72 * (0.67*fall + 0.33*noise(seed, (l0+l1), (a0+a1)))
			// subdivide the block into lots of 90–170 m frontage
			for a := a0; a < a1; {
				fr := CellKm * 3 * (5 + float64(bh%5))
				if a+fr > a1 {
					fr = a1 - a
				}
				if fr < CellKm*9 {
					break
				}
				lh := hash2(seed, int(a*1000), int(l0*1000))
				hgt := height(lh, ceil)
				if hgt < FloorKm*3 {
					hgt = FloorKm * 3
				}
				// district palette from the same noise lattice as the
				// height, so it drifts instead of changing at every kerb
				hue := int(noise(seed+7, (l0+l1)*0.7, a*0.7) * 3.99)
				p.Buildings = append(p.Buildings, Building{
					Lat: l1, Ahead: a, W: l1 - l0, D: fr, H: hgt,
					Hue:    hue,
					Bright: 0.5 + unit(lh>>9)*0.8,
					Occ:    0.15 + unit(lh>>13)*0.75, // wide spread: the dark
					Seed:   lh,                       // towers make the lit ones read full
				})
				a += fr + CellKm*3
			}
		}
	}
}

// Generate grows the port metropolis for a stellar seed. Same seed, same
// city — the whole thing is this call plus the hashes.
func Generate(seed int64) *Port {
	p := &Port{Seed: seed, CoreLat: -4.2}

	aheadCuts := axis(seed, 2, TownFrom, TownTo, 2.8)
	var trafficCuts []float64
	for si, sp := range strips {
		mul := 2.8
		if si == 1 {
			mul = 1.6 // the median between the runways is finer-grained
		}
		cuts := axis(seed+int64(si)*11, 6+si, sp[0], sp[1], mul)
		p.genBlocks(seed+int64(si)*17, cuts, aheadCuts)
		if si != 1 {
			trafficCuts = append(trafficCuts, cuts...)
		}
	}

	// landing-road edge lamps, both kerbs of both runways, every 300 m —
	// and no traffic on either road, ever
	for a := RunwayFrom; a <= RunwayTo; a += 0.3 {
		p.Lamps = append(p.Lamps,
			Lamp{Lat: -RunwayWKm / 2, Ahead: a},
			Lamp{Lat: RunwayWKm / 2, Ahead: a},
			Lamp{Lat: Runway2Lat - RunwayWKm/2, Ahead: a},
			Lamp{Lat: Runway2Lat + RunwayWKm/2, Ahead: a})
	}
	// town avenues carry the traffic
	for li := 0; li < len(trafficCuts); li += 3 {
		p.Streets = append(p.Streets, Street{
			Lat: trafficCuts[li], W: CellKm * 6,
			Cars: 2 + int(hash2(seed, 5, li)%4),
		})
	}
	// painter order: ascending Ahead, so a renderer walking the slice
	// backwards draws far to near and the depth comes out right
	sort.Slice(p.Buildings, func(i, j int) bool {
		return p.Buildings[i].Ahead < p.Buildings[j].Ahead
	})
	return p
}

// --- the port as an economic quantity ------------------------------------

// PeoplePerKm2 turns occupied floor area into a headcount. A port's industry
// is downstream of this number, so the skyline the pilot lands in IS the stat
// that supplies the fleet — no second table to keep in sync with the art.
const PeoplePerKm2 = 2200

// Density is the port's crowding, drawn once per city. Every port is grown
// to the same extents, so footprint alone makes every world the same size;
// real ports differ in how hard they are packed. This is the spread that
// makes one planet worth more than another.
func (p *Port) Density() float64 { return 0.35 + unit(hash2(p.Seed, 3, 3))*1.55 }

// SeedFor is the per-stellar city seed. Every caller must use this: the port
// on the landing screen and the port behind a planet's industrial capacity
// have to be the same city.
func SeedFor(stellarID int) int64 { return int64(stellarID) * 7919 }

// Population estimates the metropolis's headcount from the skyline it grew:
// occupied floor area, at PeoplePerKm2. Same seed, same city, same number.
func (p *Port) Population() int {
	var floorKm2 float64
	for _, b := range p.Buildings {
		floors := math.Max(math.Round(b.H/FloorKm), 1)
		floorKm2 += b.W * b.D * floors * b.Occ
	}
	return int(floorKm2 * PeoplePerKm2 * p.Density())
}

// PopulationOf grows a stellar's port just to count it. Callers that already
// hold a Port should ask it directly.
func PopulationOf(stellarID int) int { return Generate(SeedFor(stellarID)).Population() }
