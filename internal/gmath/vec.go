// Package gmath provides the small 2D vector math the game needs.
package gmath

import "math"

type Vec2 struct {
	X, Y float64
}

func V(x, y float64) Vec2 { return Vec2{x, y} }

func (v Vec2) Add(o Vec2) Vec2      { return Vec2{v.X + o.X, v.Y + o.Y} }
func (v Vec2) Sub(o Vec2) Vec2      { return Vec2{v.X - o.X, v.Y - o.Y} }
func (v Vec2) Scale(s float64) Vec2 { return Vec2{v.X * s, v.Y * s} }
func (v Vec2) Len() float64         { return math.Hypot(v.X, v.Y) }

// Norm returns the unit vector, or the zero vector for zero input.
func (v Vec2) Norm() Vec2 {
	l := v.Len()
	if l == 0 {
		return Vec2{}
	}
	return Vec2{v.X / l, v.Y / l}
}

// HeadingVec converts a ship heading in degrees (0 = up/north, clockwise,
// world Y axis pointing up) to a unit direction vector.
func HeadingVec(deg float64) Vec2 {
	rad := deg * math.Pi / 180
	return Vec2{math.Sin(rad), math.Cos(rad)}
}

// WrapDeg normalizes an angle to [0, 360).
func WrapDeg(deg float64) float64 {
	deg = math.Mod(deg, 360)
	if deg < 0 {
		deg += 360
	}
	return deg
}
