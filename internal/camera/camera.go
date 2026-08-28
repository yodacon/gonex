// Package camera translates between world coordinates (Y up) and screen
// coordinates (Y down), following a target like konex's view module.
package camera

import "yodacon.org/gonex/internal/gmath"

type Camera struct {
	// X, Y are the world coordinates of the screen's top-left corner
	// (viewX/viewY in konex).
	X, Y          float64
	Width, Height float64
}

func New(w, h float64) *Camera { return &Camera{Width: w, Height: h} }

// Follow centers the camera on a world position.
func (c *Camera) Follow(p gmath.Vec2) {
	c.X = p.X - c.Width/2
	c.Y = p.Y + c.Height/2
}

// ToScreen converts a world point to screen pixels.
func (c *Camera) ToScreen(wp gmath.Vec2) gmath.Vec2 {
	return gmath.V(wp.X-c.X, -wp.Y+c.Y)
}
