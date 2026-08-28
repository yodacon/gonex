// Package ui provides the interface toolkit: draggable windows, the menu
// component, text drawing, the FPS graph and the console view.
package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/basicfont"
)

// LineHeight matches konex's 15px console/window line spacing.
const LineHeight = 15

var face = text.NewGoXFace(basicfont.Face7x13)

// DrawText draws a single line with its top-left at (x, y).
func DrawText(dst *ebiten.Image, s string, x, y float64, alpha float32) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(x, y)
	op.ColorScale.ScaleWithColor(color.White)
	op.ColorScale.ScaleAlpha(alpha)
	text.Draw(dst, s, face, op)
}
