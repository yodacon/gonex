package app

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/ui"
)

// Warp travel is flown, not teleported: plotting a course on the chart (M)
// lights a warp beacon on the system edge in the direction of the next
// leg. Fly to it, J commits, and a ~2 s streak tunnel carries you through;
// you arrive at the far system's opposite beacon, pointed inward.

const (
	warpDur   = 2.1
	warpRange = 4 * 64.0 // how close to the beacon J works (CollisionRange*4)
)

type warpState struct {
	t    float64
	from int
	to   int
}

// warpBeacon is where the next leg departs from: on the system rim, in the
// true bearing of the destination system on the chart. (Chart y grows
// south; world y grows north — hence the flip.)
func (a *App) warpBeacon() (gmath.Vec2, int, bool) {
	next := -1
	if a.voy != nil {
		next = a.voy.NextJump()
	}
	if next < 0 {
		return gmath.Vec2{}, -1, false
	}
	cur, dst := a.gal.Systems[a.voy.System], a.gal.Systems[next]
	dx, dy := float64(dst.X-cur.X), -float64(dst.Y-cur.Y)
	l := math.Hypot(dx, dy)
	if l == 0 {
		l = 1
	}
	c := gmath.V(a.World.MapW/2, a.World.MapH/2)
	return gmath.V(c.X+dx/l*a.World.MapW*0.40, c.Y+dy/l*a.World.MapH*0.40), next, true
}

// tryJump is J in flight: needs a course, fuel, and the beacon in range.
func (a *App) tryJump() {
	beacon, next, ok := a.warpBeacon()
	switch {
	case !ok:
		a.Console.Notifyf("No course plotted — open the chart (M) and click a system.")
	case a.voy.Fuel < jumpFuel:
		a.Console.Notifyf("Not enough fuel to jump (%d/%d).", a.voy.Fuel, jumpFuel)
	case a.World.MainPlayer == nil ||
		a.World.MainPlayer.Pos().Sub(beacon).Len() > warpRange:
		a.Console.Notifyf("Warp beacon out of range — fly to the marker for %s.",
			a.gal.Systems[next].Name)
	default:
		a.warp = &warpState{from: a.voy.System, to: next}
		a.mode = modeWarp
		a.clearDockingRequest()
		a.Console.Notifyf("WARP — %s.", a.gal.Systems[next].Name)
	}
}

func (a *App) updateWarp() {
	w := a.warp
	w.t += dt
	if w.t < warpDur {
		return
	}
	if _, ok := a.voy.Jump(a.gal); ok {
		a.enterSystem(a.voy.System)
		a.stepUniverse() // three days in the tunnel is three days of trade
		// arrive at the inbound beacon: the rim point facing where you came from
		if p := a.World.MainPlayer; p != nil {
			cur, prev := a.gal.Systems[a.voy.System], a.gal.Systems[w.from]
			dx, dy := float64(prev.X-cur.X), -float64(prev.Y-cur.Y)
			l := math.Max(math.Hypot(dx, dy), 1)
			c := gmath.V(a.World.MapW/2, a.World.MapH/2)
			p.P = gmath.V(c.X+dx/l*a.World.MapW*0.38, c.Y+dy/l*a.World.MapH*0.38)
			p.V = gmath.Vec2{}
			p.Heading = gmath.WrapDeg(math.Atan2(-dx, -dy) * 180 / math.Pi)
		}
		a.drainNotices()
	}
	a.warp = nil
	a.mode = modeFlight
}

func (a *App) drawWarp(screen *ebiten.Image) {
	w := a.warp
	prog := math.Min(w.t/warpDur, 1)
	cx, cy := float64(ScreenW)/2, float64(ScreenH)/2

	// streak tunnel: radial lines accelerating outward
	n := 90
	for i := 0; i < n; i++ {
		ang := float64(i) / float64(n) * 2 * math.Pi
		seed := math.Sin(float64(i)*12.9898) * 43758.5453
		r0 := 40 + 400*math.Mod(math.Abs(seed)+prog*prog*3, 1)
		ln := 30 + 260*prog*prog
		x1, y1 := cx+math.Cos(ang)*r0, cy+math.Sin(ang)*r0*0.75
		x2, y2 := cx+math.Cos(ang)*(r0+ln), cy+math.Sin(ang)*(r0+ln)*0.75
		al := 0.12 + 0.5*prog*math.Mod(math.Abs(seed)*7, 1)
		vector.StrokeLine(screen, float32(x1), float32(y1), float32(x2), float32(y2),
			1.5, premul(colEM, al), false)
	}

	// the ship holds the middle of the tunnel
	if p := a.World.MainPlayer; p != nil {
		sprite := a.Catalog.Get(p.ShipID).Sprites[0]
		b := sprite.Bounds()
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(cx-float64(b.Dx())/2, cy-float64(b.Dy())/2)
		screen.DrawImage(sprite, op)
	}

	ui.DrawText(screen, fmt.Sprintf("HYPERSPACE — %s → %s",
		a.gal.Systems[w.from].Name, a.gal.Systems[w.to].Name), cx-140, 92, 1)

	// arrival white-in
	if prog > 0.82 {
		f := (prog - 0.82) / 0.18
		vector.DrawFilledRect(screen, 0, 0, ScreenW, ScreenH,
			premul(color.RGBA{200, 232, 240, 255}, f*f), false)
	}
}

// drawFlightOverlays marks the warp beacon and the docking state in the
// flight view.
func (a *App) drawFlightOverlays(screen *ebiten.Image) {
	if beacon, next, ok := a.warpBeacon(); ok {
		sp := a.cam.ToScreen(beacon)
		if sp.X > -40 && sp.X < ScreenW+40 && sp.Y > -40 && sp.Y < ScreenH+40 {
			x, y := float32(sp.X), float32(sp.Y)
			pulse := float32(4 + 3*math.Abs(math.Sin(time.Since(a.started).Seconds()*4)))
			for _, d := range [][4]float32{{0, -1, 1, 0}, {1, 0, 0, 1}, {0, 1, -1, 0}, {-1, 0, 0, -1}} {
				vector.StrokeLine(screen, x+d[0]*12, y+d[1]*12, x+d[2]*12, y+d[3]*12, 2, colEM, false)
			}
			vector.StrokeCircle(screen, x, y, 12+pulse, 1, premul(colEM, 0.5), false)
			ui.DrawText(screen, fmt.Sprintf("WARP → %s (J)", a.gal.Systems[next].Name),
				float64(x)+18, float64(y)-6, 0.9)
		} else if p := a.World.MainPlayer; p != nil {
			// off-screen: an edge arrow toward the beacon
			d := beacon.Sub(p.Pos())
			ang := math.Atan2(d.X, d.Y)
			ex := float32(float64(ScreenW)/2 + math.Sin(ang)*330)
			ey := float32(float64(ScreenH)/2 - math.Cos(ang)*250)
			vector.DrawFilledCircle(screen, ex, ey, 4, premul(colEM, 0.8), false)
			ui.DrawText(screen, fmt.Sprintf("beacon %.0f", d.Len()), float64(ex)-24, float64(ey)+10, 0.6)
		}
	}
}
