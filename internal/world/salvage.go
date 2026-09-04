package world

import (
	"math"

	"yodacon.org/gonex/internal/gmath"
)

// The wreck field you can see. War economy §5, built.
//
// A dead ship is not deleted, it is SCATTERED: its cargo and its structure
// drift where it died as a pile, not a pickup. It does not expire. Any ship
// that passes with room in the hold takes what it can — cargo to the hold,
// hull scrap to the junk bay, sold at the next pad — and what nobody lifts
// stays. The census above (internal/universe) keeps the same rule for the
// sectors nobody is drawing; when the player leaves a sector, the piles here
// fold back into orbit there, and when they arrive, orbit piles become these.
//
// The hard cap is enforced by MERGING, never by deleting: mass is conserved
// across every merge, so the battlefield's wealth is a number a test can
// assert on, and the proximity sweep stays honest.

// Debris is one pile of wreckage.
type Debris struct {
	Body
	// Mix is cargo by board commodity, whole tons — what a hold can take.
	Mix []int
	// Scrap is hull structure, tons — what a junk bay takes and a pad buys.
	Scrap float64
	// Other is tonnage this sector cannot name: refined stock, munitions,
	// fractions of a ton. It rides along, uncollectable here, and the
	// economy above keeps its identity (see the app's debris ledger).
	Other float64
	dead  bool
}

// Tons is everything in the pile.
func (d *Debris) Tons() float64 {
	t := d.Scrap + d.Other
	for _, n := range d.Mix {
		t += float64(n)
	}
	return t
}

// Collectable is what a passing hold could actually lift.
func (d *Debris) Collectable() float64 {
	t := d.Scrap
	for _, n := range d.Mix {
		t += float64(n)
	}
	return t
}

func (d *Debris) Alive() bool { return !d.dead && d.Tons() > 1e-9 }

const (
	// maxDebris is the pile cap; past it the two nearest piles merge.
	maxDebris = 96
	// mergeRange is how close a new drop must be to an existing pile to
	// join it rather than start another.
	mergeRange = 300.0
	// debrisDrag settles a pile: it keeps the dead ship's way, slowly.
	debrisDrag = 0.15
)

// DropDebris puts wreckage in the sky. It merges into a pile within range,
// otherwise starts one, and if the field is at its cap it merges the two
// nearest piles first. It returns the pile the tons ended up in, so whoever
// keeps the identity of the Other tonnage can follow the merge.
func (w *World) DropDebris(p, v gmath.Vec2, mix []int, scrap, other float64) *Debris {
	if len(mix) < CommodityCount {
		fixed := make([]int, CommodityCount)
		copy(fixed, mix)
		mix = fixed
	}
	var near *Debris
	nearD := mergeRange
	n := 0
	for _, e := range w.Entities {
		d, ok := e.(*Debris)
		if !ok || !d.Alive() {
			continue
		}
		n++
		if dist := d.P.Sub(p).Len(); dist < nearD {
			near, nearD = d, dist
		}
	}
	if near != nil {
		for i := range near.Mix {
			near.Mix[i] += mix[i]
		}
		near.Scrap += scrap
		near.Other += other
		return near
	}
	if n >= maxDebris {
		w.mergeNearestDebris()
	}
	d := &Debris{Body: Body{P: p, V: v}, Mix: append([]int(nil), mix...), Scrap: scrap, Other: other}
	w.Add(d)
	return d
}

// mergeNearestDebris folds the two closest piles into one. O(n²) over at
// most 96 piles, once per drop at the cap: cheap.
func (w *World) mergeNearestDebris() {
	var a, b *Debris
	best := math.MaxFloat64
	piles := w.Salvage()
	for i := 0; i < len(piles); i++ {
		for j := i + 1; j < len(piles); j++ {
			if d := piles[i].P.Sub(piles[j].P).Len(); d < best {
				a, b, best = piles[i], piles[j], d
			}
		}
	}
	if a == nil || b == nil {
		return
	}
	for i := range a.Mix {
		a.Mix[i] += b.Mix[i]
	}
	a.Scrap += b.Scrap
	a.Other += b.Other
	b.Mix, b.Scrap, b.Other = nil, 0, 0
	b.dead = true
	if w.OnDebrisMerge != nil {
		w.OnDebrisMerge(a, b)
	}
}

// Salvage lists the live piles.
func (w *World) Salvage() []*Debris {
	var out []*Debris
	for _, e := range w.Entities {
		if d, ok := e.(*Debris); ok && d.Alive() {
			out = append(out, d)
		}
	}
	return out
}

// Update drifts the pile and lets any ship with room lift from it. The
// player's own ship is handed to OnSalvage instead, because its deck is the
// voyage's manifest and not this ship's hold.
func (d *Debris) Update(w *World, dt float64) {
	d.V = d.V.Scale(1 / (1 + debrisDrag*dt))
	if d.Collectable() <= 0 {
		return
	}
	w.ForEachNear(d, func(e Entity) {
		s, ok := e.(*Ship)
		if !ok || s.Docked() || !d.Alive() {
			return
		}
		if s == w.MainPlayer {
			if w.OnSalvage != nil {
				w.OnSalvage(s, d)
			}
			return
		}
		d.scoopInto(s)
	})
}

// scoopInto moves what fits into a ship: cargo by whole tons, then scrap.
func (d *Debris) scoopInto(s *Ship) float64 {
	free := s.HoldFree()
	var got float64
	for i := range d.Mix {
		for d.Mix[i] > 0 && free >= 1 {
			d.Mix[i]--
			if i < len(s.Hold) {
				s.Hold[i]++
			}
			free--
			got++
		}
	}
	if free > 0 && d.Scrap > 0 {
		t := math.Min(free, d.Scrap)
		d.Scrap -= t
		s.Junk += t
		got += t
	}
	return got
}
