package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/ui"
	"yodacon.org/gonex/internal/world"
)

// The vector HUD: where you are going, and where you would go instead.
//
// It is drawn for the FLYING PLAYER ONLY. Nothing else on the map gets one,
// because the whole value of the instrument is that it answers a question only
// the person holding the stick is asking — not "where is that ship", which the
// map already shows, but "if I hold this, what happens?"
//
// Two instruments, and the difference between them is the entire point:
//
//	THE FORWARD VECTOR   a ring around the hull with an arc lit on the
//	                     bearing you are actually travelling. Momentum has
//	                     nothing to do with where the nose is pointing, and
//	                     in a Newtonian ship those two disagree constantly.
//	                     The arc is the sector your mass is committed to.
//
//	THE THRUST VECTOR    two projections drawn forward from the hull:
//	                     a STRAIGHT line — coast, touch nothing, this is
//	                     where you end up — and a CURVING one that integrates
//	                     the drive at the orientation you are holding right
//	                     now, with ghosts of the hull along it.
//
// The gap between the straight line and the curve IS your authority: how much
// of the future you can still change from here. Nose on the velocity vector
// and they lie on top of each other. Nose across it and the curve peels away,
// and you can see the turn before you commit to it.

const (
	// The ring sits a fixed distance out in SCREEN space, not world space: it
	// is an instrument painted on the glass, not an object in the sky.
	ringRadius = 58.0

	// How far ahead the projections look. Long enough to be a plan, short
	// enough that the far end is still about this manoeuvre.
	projectSeconds = 4.0
	projectSteps   = 48

	// Ghost hulls along the thrust curve, one per this many seconds.
	ghostEvery = 1.0

	// Below this speed the forward vector has no meaningful bearing and the
	// ring shows a dashed idle state rather than lying about a direction.
	driftSpeed = 6.0
)

// drawVectorHUD paints both instruments over the flight view.
func (a *App) drawVectorHUD(screen *ebiten.Image) {
	if a.World == nil || !a.Cfg.VectorHUD {
		return
	}
	p := a.World.MainPlayer
	if p == nil || p.Docked() {
		return
	}
	sp := a.cam.ToScreen(p.Pos())
	if sp.X < -200 || sp.X > ScreenW+200 || sp.Y < -200 || sp.Y > ScreenH+200 {
		return
	}
	a.drawForwardVector(screen, p, sp)
	a.drawThrustVector(screen, p, sp)
}

// drawForwardVector is the ring and the arc: the sector the ship's momentum
// is committed to, independent of where the nose is.
//
// The arc NARROWS as you speed up. That is not decoration — it is the honest
// reading of the instrument. A ship barely moving can be pointed anywhere
// within a breath, so its committed sector is nearly the whole circle; a ship
// at full velocity has a future that is almost entirely already decided, and
// the arc closes down onto the bearing to say so.
func (a *App) drawForwardVector(screen *ebiten.Image, p *world.Ship, sp gmath.Vec2) {
	spec := a.Catalog.Get(p.ShipID)
	speed := p.V.Len()
	x, y := float32(sp.X), float32(sp.Y)

	// The ring itself, always present, very faint: the instrument's bezel.
	vector.StrokeCircle(screen, x, y, ringRadius, 1, premul(colDim, 0.30), false)

	if speed < driftSpeed {
		// Adrift. Say so rather than painting a bearing off numerical noise.
		for i := 0; i < 16; i += 2 {
			a0 := float64(i) / 16 * 2 * math.Pi
			a1 := float64(i+1) / 16 * 2 * math.Pi
			arcSegment(screen, sp, ringRadius, a0, a1, 2, premul(colDim, 0.7))
		}
		ui.DrawText(screen, "DRIFT", sp.X-18, sp.Y-ringRadius-16, 0.7)
		return
	}

	// The bearing the mass is actually going, in screen space. Taking it from
	// two projected points rather than from the world vector means the arc
	// stays correct whatever the camera does with the Y axis.
	ahead := a.cam.ToScreen(p.Pos().Add(p.V))
	bearing := math.Atan2(ahead.Y-sp.Y, ahead.X-sp.X)

	// Committed sector: wide when slow, tight when fast.
	frac := math.Min(speed/math.Max(spec.MaxVelocity, 1), 1)
	half := (1 - 0.78*frac) * math.Pi * 0.55

	// The committed sector, drawn as a BAND rather than a line: three strokes
	// at stepped radii, brightest in the middle. A single stroke reads as
	// part of the bezel; a band reads as an area, which is what a sector is.
	arcSegment(screen, sp, ringRadius-3, bearing-half, bearing+half, 2, premul(colEM, 0.30))
	arcSegment(screen, sp, ringRadius, bearing-half, bearing+half, 4, premul(colEM, 0.85))
	arcSegment(screen, sp, ringRadius+3, bearing-half, bearing+half, 2, premul(colEM, 0.30))
	// End caps, so the sector has edges you can actually locate.
	for _, e := range []float64{bearing - half, bearing + half} {
		vector.StrokeLine(screen,
			x+float32(math.Cos(e))*(ringRadius-6), y+float32(math.Sin(e))*(ringRadius-6),
			x+float32(math.Cos(e))*(ringRadius+6), y+float32(math.Sin(e))*(ringRadius+6),
			1.5, premul(colEM, 0.6), false)
	}
	// A bright pip on the exact bearing, and a spur pointing out of it.
	px := x + float32(math.Cos(bearing))*ringRadius
	py := y + float32(math.Sin(bearing))*ringRadius
	vector.StrokeLine(screen, px, py,
		px+float32(math.Cos(bearing))*9, py+float32(math.Sin(bearing))*9, 2, colEM, false)
	vector.DrawFilledCircle(screen, px, py, 3, colEM, false)

	ui.DrawText(screen, fmt.Sprintf("%.0f u/s", speed),
		float64(px)+11, float64(py)-6, 0.75)
}

// drawThrustVector projects the next few seconds twice: once coasting, once
// with the drive held down at the orientation the ship has right now.
//
// The two share one integrator with one flag, deliberately. If the coast line
// and the burn curve ever disagreed about the physics — the velocity clamp,
// the map edges, the step size — the instrument would be lying in exactly the
// situation a pilot most needs it, which is at the limit.
func (a *App) drawThrustVector(screen *ebiten.Image, p *world.Ship, sp gmath.Vec2) {
	coast := p.Project(a.World, projectSeconds, projectSteps, false)
	burn := p.Project(a.World, projectSeconds, projectSteps, true)

	// Coast: where you end up if you touch nothing. Straight, by definition —
	// there is no drag in this sky.
	a.drawPath(screen, coast, premul(colChrome, 0.42), 1.5)
	if at, ok := a.lastVisible(coast); ok {
		ui.DrawText(screen, "COAST", at.X+6, at.Y-6, 0.7)
	}

	// Burn: the same seconds with the engine lit at this heading. It curves
	// because thrust is applied along the nose while momentum carries the old
	// direction, which is the one fact about flying this way that a new pilot
	// has to feel rather than be told.
	a.drawPath(screen, burn, premul(colHeat, 0.9), 2)

	// Ghost hulls down the burn: discrete places you will actually be, at one
	// second each, so the curve reads as time rather than as a shape.
	step := projectSeconds / projectSteps
	per := int(math.Max(1, math.Round(ghostEvery/step)))
	for i := per; i < len(burn); i += per {
		g := a.cam.ToScreen(burn[i])
		fade := 1 - float64(i)/float64(len(burn))
		r := float32(2.5 + 2*fade)
		vector.StrokeCircle(screen, float32(g.X), float32(g.Y), r, 1.5,
			premul(colHeat, 0.45+0.5*fade), false)
	}
	if at, ok := a.lastVisible(burn); ok {
		vector.DrawFilledCircle(screen, float32(at.X), float32(at.Y), 3, colHeat, false)
		ui.DrawText(screen, "FULL THRUST", at.X+6, at.Y+4, 0.7)
	}

	// The divergence: how far the burn has taken you off the coast line over
	// the full projection. This is the number that says how much of the next
	// four seconds you can still change, and it is the reason both curves are
	// drawn rather than just the interesting one.
	//
	// It hangs off the HULL, not off the end of a line, because at speed both
	// projections leave the glass entirely and a reading you cannot see is
	// not an instrument.
	if len(burn) > 1 && len(coast) > 1 {
		d := burn[len(burn)-1].Sub(coast[len(coast)-1]).Len()
		ui.DrawText(screen, fmt.Sprintf("AUTHORITY %.0f u", d),
			sp.X-ringRadius, sp.Y+ringRadius+8, 0.7)
	}
}

// drawPath strokes a world-space polyline in screen space, skipping segments
// that are entirely off the glass.
func (a *App) drawPath(screen *ebiten.Image, pts []gmath.Vec2, c color.RGBA, w float32) {
	for i := 0; i+1 < len(pts); i++ {
		p0, p1 := a.cam.ToScreen(pts[i]), a.cam.ToScreen(pts[i+1])
		if offGlass(p0) && offGlass(p1) {
			continue
		}
		vector.StrokeLine(screen, float32(p0.X), float32(p0.Y),
			float32(p1.X), float32(p1.Y), w, c, false)
	}
}

// lastVisible is the furthest point of a projection still on the glass, inset
// so a label anchored there is readable rather than half off the edge. A
// projection that leaves the screen — which at speed both of them do — still
// gets its name printed where the pilot can see it.
func (a *App) lastVisible(pts []gmath.Vec2) (gmath.Vec2, bool) {
	const inset = 96
	for i := len(pts) - 1; i >= 0; i-- {
		p := a.cam.ToScreen(pts[i])
		if p.X > inset && p.X < ScreenW-inset && p.Y > inset && p.Y < ScreenH-inset {
			return p, true
		}
	}
	return gmath.Vec2{}, false
}

func offGlass(p gmath.Vec2) bool {
	const m = 80
	return p.X < -m || p.X > ScreenW+m || p.Y < -m || p.Y > ScreenH+m
}

// arcSegment strokes part of a circle as a short polyline. Ebitengine's
// vector package draws whole circles and straight lines; an arc is neither,
// and sixteen segments is past the point where the eye can find the corners.
func arcSegment(dst *ebiten.Image, c gmath.Vec2, r, from, to float64, w float32, col color.RGBA) {
	const seg = 16
	for i := 0; i < seg; i++ {
		a0 := from + (to-from)*float64(i)/seg
		a1 := from + (to-from)*float64(i+1)/seg
		vector.StrokeLine(dst,
			float32(c.X+math.Cos(a0)*r), float32(c.Y+math.Sin(a0)*r),
			float32(c.X+math.Cos(a1)*r), float32(c.Y+math.Sin(a1)*r),
			w, col, false)
	}
}
