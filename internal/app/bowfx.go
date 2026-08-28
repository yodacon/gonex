package app

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"yodacon.org/gonex/internal/fx"
)

// The bow-wave renderers: both take an fx.Fire — a flat (u, v) grid — and
// lay it onto a curve, so the classic fire propagation reads as a bent
// sheath instead of a wall of flame. Column u walks the arc, row v marches
// outward, and every row drifts aft and outboard like a particle further
// down its streamline: the contour is the flow field, not a rectangle.

// fireBand quantizes intensity into the emission-line contour bands — the
// marching-squares idea reduced to its visible half: hard color steps from
// the white core out through the lithium red to the nitrogen violet rim.
func fireBand(t float64) color.RGBA {
	switch {
	case t > 0.78:
		return color.RGBA{255, 250, 240, 255} // white core
	case t > 0.55:
		return color.RGBA{255, 214, 92, 255} // combustion yellow
	case t > 0.36:
		return colHeat // orange
	case t > 0.19:
		return color.RGBA{255, 59, 78, 255} // Li 670.8 nm
	default:
		return color.RGBA{139, 108, 255, 255} // N2 first-positive rim
	}
}

// bowGeom is where the plasma bow wave stands this frame.
type bowGeom struct {
	cx, nose float64 // deflection center, nose line (screen px)
	standPx  float64 // shell standoff in px
	roll     float64 // the steering command; the lobe sits opposite
	alpha    float64 // overall brightness gate
}

// drawBowFire lays the fire grid along the standoff shell: row 0 burns ON
// the mirror arc, and each row outward drifts aft and outboard, bending
// every column like the streamline it is. The steering lobe both feeds the
// front harder (the grid's FuelProfile) and swells the arc under it, so
// the flame and the shell always agree about where the push is.
func drawBowFire(dst *ebiten.Image, f *fx.Fire, g bowGeom) {
	if g.alpha <= 0.02 {
		return
	}
	rx0 := 40 + g.standPx*0.85
	lobe := -g.roll
	rollAbs := math.Abs(g.roll)
	for i := 0; i < f.Cols; i++ {
		u := f.U(i)
		ang := u * 1.25
		side := math.Sin(ang)
		swell := 1 + 0.22*math.Max(0, lobe*side)*rollAbs
		bx := g.cx + side*rx0*swell
		by := g.nose - g.standPx*swell + (1-math.Cos(ang))*g.standPx*0.75
		// the streamline this column rides: outward at the stagnation
		// point, wrapping aft around the shoulders
		dirX := side * (0.55 + 0.45*math.Abs(side))
		dirY := -math.Cos(ang) * 0.9
		for j := 0; j < f.Rows; j++ {
			c := f.Cell(i, j)
			if c < 0.04 {
				continue
			}
			v := float64(j)
			// aft drag grows quadratically with depth: the tip of every
			// column bends downstream — particle wave, not straight fire
			px := bx + dirX*v*4.5 + side*v*v*0.4
			py := by + dirY*v*5.0 + v*v*0.85
			r := float32(c*(4.6+v*0.7) + 1.1)
			col := fireBand(c)
			glowDot(dst, float32(px), float32(py), r*2.8, col,
				math.Min(c*0.55, 0.55)*g.alpha)
			fastDot(dst, float32(px), float32(py), r, col,
				math.Min(c*1.6, 1)*g.alpha)
		}
	}
}

// drawMachCloud is the other bow wave: the white condensation collar that
// forms where the air is dense and the ship supersonic. Row 0 rides an
// ellipse at the hull's widest station and the rows sweep back along the
// Mach cone — the faster the ship, the tighter the sweep (the cone angle
// is asin(1/M) and the visible cloud hugs it). Rendered as stacked soft
// dots: volume, not line work.
func drawMachCloud(dst *ebiten.Image, f *fx.Fire, cx, cy, mach, alpha float64) {
	if alpha <= 0.02 || mach <= 0.85 {
		return
	}
	// sweep factor: sqrt(M²−1), the cotangent of the cone half-angle
	m := math.Max(mach, 1.02)
	k := math.Min(math.Sqrt(m*m-1), 2.6) / 2.6
	rw := 68.0
	white := color.RGBA{236, 243, 250, 255}
	for i := 0; i < f.Cols; i++ {
		u := f.U(i)
		bx := cx + u*rw
		by := cy - 8 + math.Abs(u)*math.Abs(u)*14
		dirX := u * 0.5
		dirY := 0.42 + 0.75*math.Abs(u)*k
		for j := 0; j < f.Rows; j++ {
			c := f.Cell(i, j)
			if c < 0.05 {
				continue
			}
			v := float64(j)
			px := bx + dirX*v*7 + u*v*v*0.5*k
			py := by + dirY*v*6.5 + v*v*0.5
			al := math.Min(c*1.1, 0.75) * alpha
			r := float32(c*9 + v*1.1 + 2.5)
			// a wide additive glow body under a brighter kernel: volume
			glowDot(dst, float32(px), float32(py), r*2.3, white, al*0.5)
			fastDot(dst, float32(px), float32(py), r, white, al)
		}
	}
}
