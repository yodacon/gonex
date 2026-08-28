package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/ui"
)

// The galaxy chart: every recovered system and hyperspace link, the planned
// route, and the pulse on any system a mission wants you in. Click a system
// to plot a course; J in flight commits the next leg.

func (a *App) drawGalaxyMap(win *ui.Window, dst *ebiten.Image) {
	if a.voy == nil {
		return
	}
	// world→window transform over the recovered coordinates
	minX, maxX, minY, maxY := 1e9, -1e9, 1e9, -1e9
	for _, s := range a.gal.Systems {
		minX, maxX = math.Min(minX, float64(s.X)), math.Max(maxX, float64(s.X))
		minY, maxY = math.Min(minY, float64(s.Y)), math.Max(maxY, float64(s.Y))
	}
	pad := 26.0
	sx := (float64(win.W) - 2*pad) / (maxX - minX)
	sy := (float64(win.H) - 2*pad) / (maxY - minY)
	at := func(id int) (float64, float64, bool) {
		s := a.gal.Systems[id]
		if s == nil {
			return 0, 0, false
		}
		return float64(win.X) + pad + (float64(s.X)-minX)*sx,
			float64(win.Y) + pad + (float64(s.Y)-minY)*sy, true
	}

	// links
	for id, s := range a.gal.Systems {
		x1, y1, _ := at(id)
		for _, l := range s.Links {
			if l <= id { // draw each edge once
				continue
			}
			if x2, y2, ok := at(l); ok {
				vector.StrokeLine(dst, float32(x1), float32(y1), float32(x2), float32(y2),
					1, color.RGBA{29, 122, 36, 110}, false)
			}
		}
	}
	// planned route
	for i := 0; i+1 < len(a.voy.Route); i++ {
		x1, y1, ok1 := at(a.voy.Route[i])
		x2, y2, ok2 := at(a.voy.Route[i+1])
		if ok1 && ok2 {
			vector.StrokeLine(dst, float32(x1), float32(y1), float32(x2), float32(y2),
				2, colEM, false)
		}
	}

	// mission destinations pulse
	wants := map[int]bool{}
	for _, act := range a.voy.Active {
		if st := a.gal.Stellars[act.Dest()]; st != nil {
			wants[st.System] = true
		}
	}

	mx, my := ebiten.CursorPosition()
	clicked := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	pulse := float32(3 + 2*math.Abs(math.Sin(float64(a.voy.Day)+float64(mx)*0))) // steady beat

	for id, s := range a.gal.Systems {
		x, y, _ := at(id)
		c := color.RGBA{92, 255, 92, 200}
		r := float32(2)
		if s.Source != "base" {
			c, r = color.RGBA{194, 107, 216, 230}, 3
		}
		if wants[id] {
			vector.StrokeCircle(dst, float32(x), float32(y), pulse+3, 1, colLi, false)
		}
		if id == a.voy.System {
			vector.StrokeCircle(dst, float32(x), float32(y), 7, 2, colChrome, false)
		}
		vector.DrawFilledCircle(dst, float32(x), float32(y), r, c, false)

		over := math.Hypot(float64(mx)-x, float64(my)-y) < 9
		if over {
			ui.DrawText(dst, fmt.Sprintf("%s (%s)", s.Name, s.Govt), x+10, y-6, 1)
			if clicked && id != a.voy.System {
				a.voy.Route = a.gal.Route(a.voy.System, id)
				legs := len(a.voy.Route) - 1
				a.Console.Notifyf("Course: %d legs to %s (%d fuel)", legs, s.Name, legs*jumpFuel)
			}
		}
	}

	cur := a.gal.Systems[a.voy.System]
	status := fmt.Sprintf("%s · fuel %d · J jumps", cur.Name, a.voy.Fuel)
	if next := a.voy.NextJump(); next >= 0 {
		status = fmt.Sprintf("%s · next leg → %s · fuel %d · J jumps",
			cur.Name, a.gal.Systems[next].Name, a.voy.Fuel)
	}
	ui.DrawText(dst, status, float64(win.X)+8, float64(win.Y)+float64(win.H)-18, 1)
}
