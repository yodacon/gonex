// Package render draws the world through a camera. It owns the non-ship
// texture catalogs (planets, explosion frames, item pickups) so the world
// package can stay free of Ebitengine.
package render

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/assets"
	"yodacon.org/gonex/internal/camera"
	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/ship"
	"yodacon.org/gonex/internal/world"
)

const (
	planetCount    = 18
	explosionCount = 17
)

func TeamColor(t world.Team, highlight bool) color.RGBA {
	v := uint8(128)
	if highlight {
		v = 255
	}
	switch t {
	case world.TeamRed:
		return color.RGBA{v, 0, 0, 255}
	case world.TeamGreen:
		return color.RGBA{0, v, 0, 255}
	case world.TeamBlue:
		return color.RGBA{0, 0, v, 255}
	}
	return color.RGBA{96, 96, 96, 255}
}

type Renderer struct {
	Catalog    *ship.Catalog
	planets    [planetCount + 1]*ebiten.Image // 1-based, like konex
	explosions [explosionCount]*ebiten.Image
	health     *ebiten.Image
	money      *ebiten.Image
}

func New(catalog *ship.Catalog) (*Renderer, error) {
	r := &Renderer{Catalog: catalog}
	for i := 1; i <= planetCount; i++ {
		img, err := assets.Image(fmt.Sprintf("data/planets/%02d/pic.tga", i-1))
		if err != nil {
			return nil, err
		}
		r.planets[i] = img
	}
	for i := 0; i < explosionCount; i++ {
		img, err := assets.Image(fmt.Sprintf("data/explosions/%02d.tga", i))
		if err != nil {
			return nil, err
		}
		r.explosions[i] = img
	}
	var err error
	if r.health, err = assets.Image("data/items/health.tga"); err != nil {
		return nil, err
	}
	if r.money, err = assets.Image("data/items/money.tga"); err != nil {
		return nil, err
	}
	return r, nil
}

// Planet returns a planet texture by 1-based sprite index, for scenes that
// draw outside DrawWorld (the deorbit cinematic).
func (r *Renderer) Planet(i int) *ebiten.Image {
	if i < 1 || i > planetCount {
		return r.planets[1]
	}
	return r.planets[i]
}

func onScreen(sp gmath.Vec2, margin, w, h float64) bool {
	return sp.X > -margin && sp.X < w+margin && sp.Y > -margin && sp.Y < h+margin
}

// drawHolding rings a held planet in its team's colour and draws the pad
// queue as ticks beneath it. Territory has to be readable at a glance or the
// supply war is invisible.
func drawHolding(dst *ebiten.Image, p *world.Planet, sp gmath.Vec2) {
	if p.Team == world.TeamNone {
		return
	}
	c := TeamColor(p.Team, true)
	c.A = 150
	if p.Starving() {
		c.A = 60 // a world that can no longer arm its ships dims
	}
	const rad = 46
	vector.StrokeCircle(dst, float32(sp.X), float32(sp.Y), rad, 2,
		c, false)
	for i := range p.Pad {
		vector.DrawFilledRect(dst, float32(sp.X)-14+float32(i)*8,
			float32(sp.Y)+rad+4, 5, 5, c, false)
	}
}

// DrawWorld renders every visible entity. Explosions draw last so they cover
// the ships that produced them.
func (r *Renderer) DrawWorld(dst *ebiten.Image, w *world.World, cam *camera.Camera) {
	var explosions []*world.Explosion
	for _, e := range w.Entities {
		sp := cam.ToScreen(e.Pos())
		if !onScreen(sp, 512, cam.Width, cam.Height) {
			continue
		}
		switch v := e.(type) {
		case *world.Planet:
			if v.SpriteID >= 1 && v.SpriteID <= planetCount {
				drawCentered(dst, r.planets[v.SpriteID], sp, 1)
			}
			drawHolding(dst, v, sp)
		case *world.Item:
			if v.Type == world.ItemHealth {
				drawCentered(dst, r.health, sp, 1)
			} else {
				drawCentered(dst, r.money, sp, 1)
			}
		case *world.Missile:
			vector.DrawFilledRect(dst, float32(sp.X), float32(sp.Y), 4, 4,
				color.RGBA{192, 192, 192, 192}, false)
		case *world.Ship:
			r.drawShip(dst, v, sp)
		case *world.Explosion:
			explosions = append(explosions, v)
		}
	}
	for _, ex := range explosions {
		r.drawExplosion(dst, ex, cam.ToScreen(ex.Pos()))
	}
}

func (r *Renderer) drawShip(dst *ebiten.Image, s *world.Ship, sp gmath.Vec2) {
	sprite := r.Catalog.Get(s.ShipID).SpriteFor(s.Heading)
	drawCentered(dst, sprite, sp, 1)

	// Health bar just under the sprite.
	h := float64(sprite.Bounds().Dy())
	barX, barY := float32(sp.X-25), float32(sp.Y+h/2)
	bg, fg := TeamColor(s.Team, false), TeamColor(s.Team, true)
	bg.A, fg.A = 64, 64
	vector.DrawFilledRect(dst, barX, barY, 50, 5, bg, false)
	vector.DrawFilledRect(dst, barX, barY, float32(s.Health)/2, 5, fg, false)
}

func (r *Renderer) drawExplosion(dst *ebiten.Image, ex *world.Explosion, sp gmath.Vec2) {
	idx := int(-ex.TTL*16) + 16
	if idx < 0 {
		idx = 0
	}
	if idx >= explosionCount {
		idx = explosionCount - 1
	}
	frame := r.explosions[idx]
	off := float64(idx)
	// One bright center draw plus four faint offset ghosts, like konex.
	drawScaledAt(dst, frame, sp.X-ex.Size/2, sp.Y-ex.Size/2, ex.Size, 0.5)
	for _, d := range [][2]float64{{off, off}, {-off, -off}, {off, -off}, {-off, off}} {
		drawScaledAt(dst, frame, sp.X-ex.Size/2+d[0], sp.Y-ex.Size/2+d[1], ex.Size, 0.125)
	}
}

func drawCentered(dst, img *ebiten.Image, sp gmath.Vec2, alpha float32) {
	b := img.Bounds()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(sp.X-float64(b.Dx())/2, sp.Y-float64(b.Dy())/2)
	op.ColorScale.ScaleAlpha(alpha)
	dst.DrawImage(img, op)
}

func drawScaledAt(dst, img *ebiten.Image, x, y, size float64, alpha float32) {
	b := img.Bounds()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(size/float64(b.Dx()), size/float64(b.Dy()))
	op.GeoM.Translate(x, y)
	op.ColorScale.ScaleAlpha(alpha)
	dst.DrawImage(img, op)
}

// DrawTargetOverlay draws the in-world targeting cues: a dim red line from
// screen center to the target plus white corner brackets around it.
func (r *Renderer) DrawTargetOverlay(dst *ebiten.Image, w *world.World, cam *camera.Camera) {
	view := w.ViewShip
	if view == nil || view.Target == nil {
		return
	}
	t := view.Target
	sprite := r.Catalog.Get(t.ShipID).Sprites[0]
	bw, bh := float32(sprite.Bounds().Dx()), float32(sprite.Bounds().Dy())
	sp := cam.ToScreen(t.Pos())
	cx, cy := float32(sp.X), float32(sp.Y)

	vector.StrokeLine(dst, cx, cy, float32(cam.Width)/2, float32(cam.Height)/2,
		1, color.RGBA{128, 0, 0, 255}, false)

	white := color.RGBA{255, 255, 255, 255}
	x0, y0 := cx-bw/2, cy-bh/2
	x1, y1 := cx+bw/2, cy+bh/2
	for _, c := range [][4]float32{
		{x0, y0, 1, 1}, {x1, y0, -1, 1}, {x1, y1, -1, -1}, {x0, y1, 1, -1},
	} {
		vector.StrokeLine(dst, c[0], c[1], c[0]+10*c[2], c[1], 1, white, false)
		vector.StrokeLine(dst, c[0], c[1], c[0], c[1]+10*c[3], 1, white, false)
	}
}

// DrawMap renders the minimap / full map: every entity as a colored blip in
// map-scaled coordinates inside the given content rect.
func (r *Renderer) DrawMap(dst *ebiten.Image, w *world.World, x, y, width, height int) {
	scaleX := w.MapW / float64(width)
	scaleY := w.MapH / float64(height)
	var viewTarget *world.Ship
	if w.ViewShip != nil {
		viewTarget = w.ViewShip.Target
	}
	for _, e := range w.Entities {
		if _, isSpawn := e.(*world.SpawnPoint); isSpawn {
			continue
		}
		p := e.Pos()
		px := float32(float64(x) + p.X/scaleX)
		py := float32(float64(y) - p.Y/scaleY + float64(height))

		var c color.RGBA
		size := float32(2)
		switch v := e.(type) {
		case *world.Planet:
			c, size = TeamColor(v.Team, true), 4
			if v.Team == world.TeamNone {
				c, size = color.RGBA{128, 128, 128, 255}, 3
			}
		case *world.Ship:
			switch {
			case v == viewTarget:
				c = color.RGBA{255, 0, 0, 255}
			case v == w.MainPlayer:
				c, size = color.RGBA{255, 255, 255, 255}, 3
			default:
				c = TeamColor(v.Team, true)
			}
		default:
			c = color.RGBA{96, 96, 96, 255}
		}
		vector.DrawFilledRect(dst, px, py, size, size, c, false)
	}
}
