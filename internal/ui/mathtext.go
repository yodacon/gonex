package ui

import (
	"bytes"
	"image/color"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/goregular"
)

// Math typesetting: a tiny TeX-ish layout over the Go face, for the
// cockpit's live-algebra panel. The 7x13 bitmap face can spell "rho";
// an equation wants ρ, a raised ², a lowered subscript and a vinculum
// over the radicand — so equations get a real vector font and a
// three-marker markup:
//
//	_{...}  subscript      ^{...}  superscript      √{...}  radical
//
// Everything else renders as written (the Go face carries Greek, the
// mid-dot and the math punctuation the envelope model needs). y is the
// TOP of the line, like DrawText; the return value is the end x.

var mathSrc *text.GoTextFaceSource
var mathFaces = map[float64]*text.GoTextFace{}

func init() {
	src, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		log.Fatalf("mathtext: %v", err)
	}
	mathSrc = src
}

func mathFace(size float64) *text.GoTextFace {
	if f, ok := mathFaces[size]; ok {
		return f
	}
	f := &text.GoTextFace{Source: mathSrc, Size: size}
	mathFaces[size] = f
	return f
}

// mathRun lays out spec at (x, y top) and size; dst nil measures only.
func mathRun(dst *ebiten.Image, spec string, x, y, size float64,
	c color.Color, alpha float32) float64 {
	face := mathFace(size)
	small := size * 0.66
	rs := []rune(spec)
	flush := func(runes []rune, fx, fy float64, f *text.GoTextFace) float64 {
		if len(runes) == 0 {
			return 0
		}
		s := string(runes)
		if dst != nil {
			op := &text.DrawOptions{}
			op.GeoM.Translate(fx, fy)
			op.ColorScale.ScaleWithColor(c)
			op.ColorScale.ScaleAlpha(alpha)
			text.Draw(dst, s, f, op)
		}
		return text.Advance(s, f)
	}
	var plain []rune
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		// a marker rune followed by '{' opens a group
		if (r == '_' || r == '^' || r == '√') && i+1 < len(rs) && rs[i+1] == '{' {
			x += flush(plain, x, y, face)
			plain = plain[:0]
			depth, j := 1, i+2
			for ; j < len(rs) && depth > 0; j++ {
				if rs[j] == '{' {
					depth++
				} else if rs[j] == '}' {
					depth--
				}
			}
			inner := string(rs[i+2 : j-1])
			switch r {
			case '_':
				x = mathRun(dst, inner, x, y+size*0.38, small, c, alpha)
			case '^':
				x = mathRun(dst, inner, x, y-size*0.26, small, c, alpha)
			case '√':
				// the radical: a drawn surd, the radicand, the vinculum
				w := mathRun(nil, inner, 0, 0, size, c, alpha)
				if dst != nil {
					lw := float32(size * 0.07)
					px := func(f float64) float32 { return float32(x + f*size) }
					py := func(f float64) float32 { return float32(y + f*size) }
					vector.StrokeLine(dst, px(0.02), py(0.72), px(0.16), py(1.06), lw, mcol(c, alpha), true)
					vector.StrokeLine(dst, px(0.16), py(1.06), px(0.34), py(0.10), lw, mcol(c, alpha), true)
					vector.StrokeLine(dst, px(0.34), py(0.10), float32(x+0.42*size+w), py(0.10), lw, mcol(c, alpha), true)
				}
				mathRun(dst, inner, x+0.42*size, y+size*0.12, size, c, alpha)
				x += 0.42*size + w + size*0.12
			}
			i = j - 1
			continue
		}
		plain = append(plain, r)
	}
	x += flush(plain, x, y, face)
	return x
}

func mcol(c color.Color, alpha float32) color.RGBA {
	r, g, b, _ := c.RGBA()
	return color.RGBA{
		uint8(float32(r>>8) * alpha), uint8(float32(g>>8) * alpha),
		uint8(float32(b>>8) * alpha), uint8(255 * alpha),
	}
}

// DrawMath renders the marked-up equation with its top-left at (x, y)
// and returns the end x.
func DrawMath(dst *ebiten.Image, spec string, x, y, size float64,
	c color.Color, alpha float32) float64 {
	return mathRun(dst, spec, x, y, size, c, alpha)
}

// MathWidth measures the marked-up equation without drawing.
func MathWidth(spec string, size float64) float64 {
	return mathRun(nil, spec, 0, 0, size, color.White, 1)
}
