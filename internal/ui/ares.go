package ui

// The Ares grammar: the late-90s Ambrosia console furniture the dock
// screens are styled after. A Keycap is the classic two-segment button —
// a raised key on the left naming the hotkey, a recessed label bar on the
// right, small end nubs, everything bevelled by 1px light/shadow lines.
// A VGauge is the sidebar tank: a bezelled vertical slot filling from the
// bottom with ticked quarters. Both are drawn with plain rects, no
// textures, so they inherit the game's palette and scale.

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// KeyTone selects the Ares button color family.
type KeyTone int

const (
	ToneKhaki KeyTone = iota // the neutral tan of most actions
	ToneGreen                // confirm / primary / an open contract
	ToneRed                  // quit, danger, money owed
	ToneGray                 // passive (save, info)
	ToneDim                  // disabled / unavailable
)

type keyColors struct {
	cap, bar, text color.RGBA
}

func toneColors(t KeyTone) keyColors {
	switch t {
	case ToneGreen:
		return keyColors{color.RGBA{110, 200, 110, 255}, color.RGBA{52, 96, 52, 255}, color.RGBA{198, 255, 198, 255}}
	case ToneRed:
		return keyColors{color.RGBA{212, 116, 110, 255}, color.RGBA{102, 50, 48, 255}, color.RGBA{255, 200, 196, 255}}
	case ToneGray:
		return keyColors{color.RGBA{168, 170, 168, 255}, color.RGBA{78, 80, 78, 255}, color.RGBA{225, 228, 225, 255}}
	case ToneDim:
		return keyColors{color.RGBA{96, 94, 78, 255}, color.RGBA{44, 43, 36, 255}, color.RGBA{140, 138, 118, 255}}
	default: // khaki
		return keyColors{color.RGBA{206, 196, 122, 255}, color.RGBA{96, 90, 58, 255}, color.RGBA{236, 230, 190, 255}}
	}
}

func bevelRect(dst *ebiten.Image, x, y, w, h float32, fill color.RGBA, raised bool) {
	vector.DrawFilledRect(dst, x, y, w, h, fill, false)
	lite := color.RGBA{
		uint8(min32(int(fill.R)+60, 255)), uint8(min32(int(fill.G)+60, 255)),
		uint8(min32(int(fill.B)+60, 255)), 255}
	dark := color.RGBA{fill.R / 3, fill.G / 3, fill.B / 3, 255}
	if !raised {
		lite, dark = dark, lite
	}
	vector.StrokeLine(dst, x, y, x+w, y, 1, lite, false)
	vector.StrokeLine(dst, x, y, x, y+h, 1, lite, false)
	vector.StrokeLine(dst, x, y+h, x+w, y+h, 1, dark, false)
	vector.StrokeLine(dst, x+w, y, x+w, y+h, 1, dark, false)
}

func min32(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// KeycapH is the standard button height.
const KeycapH = 22

// Keycap draws one Ares button: [nub][ KEY ][ label............ ][nub].
// lit brightens the label the way Ares highlights the active row.
func Keycap(dst *ebiten.Image, x, y, w float64, key, label string, tone KeyTone, lit bool) {
	c := toneColors(tone)
	fx, fy, fw := float32(x), float32(y), float32(w)
	const nub, capW = 8, 64
	// frame
	vector.DrawFilledRect(dst, fx-2, fy-2, fw+4, KeycapH+4, color.RGBA{10, 10, 8, 255}, false)
	// end nubs
	bevelRect(dst, fx, fy+4, nub, KeycapH-8, c.cap, true)
	bevelRect(dst, fx+fw-nub, fy+4, nub, KeycapH-8, c.cap, true)
	// the key cap, raised
	bevelRect(dst, fx+nub+2, fy, capW, KeycapH, c.cap, true)
	keyCol := color.RGBA{c.bar.R / 2, c.bar.G / 2, c.bar.B / 2, 255}
	DrawTextScaled(dst, key, x+nub+2+capW/2-float64(len(key))*3.5, y+4, 1, keyCol, 1)
	// the label bar, recessed
	barX := fx + nub + 2 + capW + 2
	barW := fw - nub*2 - capW - 6
	bevelRect(dst, barX, fy, barW, KeycapH, c.bar, false)
	txt := c.text
	if lit {
		txt = color.RGBA{160, 255, 130, 255}
	}
	DrawTextScaled(dst, label, float64(barX)+10, y+4, 1, txt, 1)
}

// VGauge draws one Ares sidebar tank: a bezelled slot filling from the
// bottom, quarter ticks, a bright cap line on the fill, label beneath.
func VGauge(dst *ebiten.Image, x, y, w, h, frac float64, c color.RGBA, label, val string) {
	fx, fy, fw, fh := float32(x), float32(y), float32(w), float32(h)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	// slot
	vector.DrawFilledRect(dst, fx-2, fy-2, fw+4, fh+4, color.RGBA{10, 10, 8, 255}, false)
	bevelRect(dst, fx, fy, fw, fh, color.RGBA{24, 28, 26, 255}, false)
	// fill
	fillH := float32(frac) * (fh - 4)
	if fillH > 0.5 {
		vector.DrawFilledRect(dst, fx+2, fy+fh-2-fillH, fw-4, fillH, c, false)
		cap := color.RGBA{
			uint8(min32(int(c.R)+80, 255)), uint8(min32(int(c.G)+80, 255)),
			uint8(min32(int(c.B)+80, 255)), 255}
		vector.DrawFilledRect(dst, fx+2, fy+fh-2-fillH, fw-4, 2, cap, false)
	}
	// quarter ticks
	for i := 1; i < 4; i++ {
		ty := fy + fh*float32(i)/4
		vector.StrokeLine(dst, fx, ty, fx+4, ty, 1, color.RGBA{90, 100, 95, 255}, false)
		vector.StrokeLine(dst, fx+fw-4, ty, fx+fw, ty, 1, color.RGBA{90, 100, 95, 255}, false)
	}
	DrawText(dst, label, x+w/2-float64(len(label))*3.5, y+h+6, 0.7)
	DrawText(dst, val, x+w/2-float64(len(val))*3.5, y+h+20, 0.9)
}
