// Package starfield ports konex's screen-space parallax stars: each star has
// a brightness-linked speed, drifts opposite the camera's motion and wraps at
// the screen edges.
package starfield

import (
	"image/color"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

type star struct {
	x, y      float32
	speed     float32
	intensity int
}

type Field struct {
	stars        []star
	w, h         float32
	lastX, lastY float64
	rand         *rand.Rand
}

func New(count int, w, h int, r *rand.Rand) *Field {
	f := &Field{w: float32(w), h: float32(h), rand: r}
	f.SetCount(count)
	return f
}

// SetCount regenerates the field (console `starcount`).
func (f *Field) SetCount(count int) {
	f.stars = make([]star, count)
	for i := range f.stars {
		intensity := 50 + f.rand.Intn(150)
		f.stars[i] = star{
			x:         f.rand.Float32() * f.w,
			y:         f.rand.Float32() * f.h,
			intensity: intensity,
			speed:     float32(intensity) / 50,
		}
	}
}

// Update drifts stars against camera motion; viewX/viewY are camera origin.
func (f *Field) Update(viewX, viewY float64) {
	dx := float32(viewX-f.lastX) / 8
	dy := float32(viewY-f.lastY) / 8
	f.lastX, f.lastY = viewX, viewY

	for i := range f.stars {
		s := &f.stars[i]
		s.x -= dx * s.speed
		s.y += dy * s.speed
		if s.x < 0 {
			s.x += f.w
		}
		if s.x > f.w {
			s.x -= f.w
		}
		if s.y < 0 {
			s.y += f.h
		}
		if s.y > f.h {
			s.y -= f.h
		}
	}
}

func (f *Field) Draw(dst *ebiten.Image) {
	for i := range f.stars {
		s := &f.stars[i]
		v := uint8(s.intensity)
		c := color.RGBA{v, v, v, 255}
		// Brighter stars render slightly larger, like the original's
		// multi-pixel plots.
		size := float32(1)
		if s.intensity > 128 {
			size = 2
		}
		vector.DrawFilledRect(dst, s.x, s.y, size, size, c, false)
	}
}
