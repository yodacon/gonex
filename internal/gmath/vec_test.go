package gmath

import (
	"math"
	"testing"
)

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestHeadingVec(t *testing.T) {
	cases := []struct {
		deg  float64
		x, y float64
	}{
		{0, 0, 1},  // up
		{90, 1, 0}, // right
		{180, 0, -1},
		{270, -1, 0},
	}
	for _, c := range cases {
		v := HeadingVec(c.deg)
		if !almost(v.X, c.x) || !almost(v.Y, c.y) {
			t.Errorf("HeadingVec(%v) = %v, want (%v, %v)", c.deg, v, c.x, c.y)
		}
	}
}

func TestWrapDeg(t *testing.T) {
	for _, c := range [][2]float64{{-10, 350}, {370, 10}, {0, 0}, {720, 0}} {
		if got := WrapDeg(c[0]); !almost(got, c[1]) {
			t.Errorf("WrapDeg(%v) = %v, want %v", c[0], got, c[1])
		}
	}
}

func TestNormZero(t *testing.T) {
	if v := (Vec2{}).Norm(); v.X != 0 || v.Y != 0 {
		t.Errorf("Norm of zero vector = %v", v)
	}
}
