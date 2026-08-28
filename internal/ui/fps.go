package ui

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const fpsHistorySize = 128

// FPSGraph draws the rolling frame-rate bar graph konex showed in its
// corner window.
type FPSGraph struct {
	history [fpsHistorySize]float64
}

func (f *FPSGraph) Draw(win *Window, dst *ebiten.Image) {
	fps := ebiten.ActualFPS()
	copy(f.history[:], f.history[1:])
	f.history[fpsHistorySize-1] = fps

	green := color.RGBA{0, 255, 0, 100}
	for i, v := range f.history {
		barH := float32(v / 5)
		vector.DrawFilledRect(dst,
			float32(win.X+i), float32(win.Y+win.H)-20-barH,
			1, barH, green, false)
	}
	DrawText(dst, fmt.Sprintf("%0.2f frames/sec", fps),
		float64(win.X), float64(win.Y+win.H)-16, 1)
}
