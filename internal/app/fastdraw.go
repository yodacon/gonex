package app

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// The fast path. The vector package's helpers build and tessellate a Path
// under a global lock on EVERY call — fine for a cockpit's worth of
// gauges, fatal for a metropolis: thousands of building faces, windows and
// particles per frame each paying the lock and the triangulation.
//
// Everything the busy scenes draw goes through here instead: one shared
// 1×1 white texel and DrawTriangles with vertex colors. Ebitengine batches
// consecutive DrawTriangles on the same source image into a handful of GPU
// draws, and the scratch vertex buffer never allocates. This is the
// "buffer we modify less": the geometry still streams, but the render
// state is one texture and one pipeline for the whole scene.

var (
	fastWhite   *ebiten.Image
	fastIndices = []uint16{0, 1, 2, 0, 2, 3}
	fastOpts    = &ebiten.DrawTrianglesOptions{}
	fastVs      [4]ebiten.Vertex
)

func fastInit() {
	if fastWhite != nil {
		return
	}
	img := ebiten.NewImage(3, 3)
	img.Fill(color.White)
	fastWhite = img.SubImage(image.Rect(1, 1, 2, 2)).(*ebiten.Image)
}

// fastQuad fills an arbitrary quad, corners in draw order.
func fastQuad(dst *ebiten.Image, x0, y0, x1, y1, x2, y2, x3, y3 float32,
	c color.RGBA, al float64) {
	fastInit()
	p := premul(c, al)
	r := float32(p.R) / 255
	g := float32(p.G) / 255
	b := float32(p.B) / 255
	a := float32(p.A) / 255
	fastVs[0] = ebiten.Vertex{DstX: x0, DstY: y0, SrcX: 1.5, SrcY: 1.5, ColorR: r, ColorG: g, ColorB: b, ColorA: a}
	fastVs[1] = ebiten.Vertex{DstX: x1, DstY: y1, SrcX: 1.5, SrcY: 1.5, ColorR: r, ColorG: g, ColorB: b, ColorA: a}
	fastVs[2] = ebiten.Vertex{DstX: x2, DstY: y2, SrcX: 1.5, SrcY: 1.5, ColorR: r, ColorG: g, ColorB: b, ColorA: a}
	fastVs[3] = ebiten.Vertex{DstX: x3, DstY: y3, SrcX: 1.5, SrcY: 1.5, ColorR: r, ColorG: g, ColorB: b, ColorA: a}
	dst.DrawTriangles(fastVs[:], fastIndices, fastWhite, fastOpts)
}

func fastRect(dst *ebiten.Image, x, y, w, h float32, c color.RGBA, al float64) {
	fastQuad(dst, x, y, x+w, y, x+w, y+h, x, y+h, c, al)
}

// fastLine is a filled rotated rect between two points.
func fastLine(dst *ebiten.Image, x0, y0, x1, y1, w float32, c color.RGBA, al float64) {
	dx, dy := x1-x0, y1-y0
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l < 1e-3 {
		return
	}
	px, py := -dy/l*w/2, dx/l*w/2
	fastQuad(dst, x0+px, y0+py, x1+px, y1+py, x1-px, y1-py, x0-px, y0-py, c, al)
}

// fastDot is a small square — at particle sizes indistinguishable from a
// circle and two orders of magnitude cheaper.
func fastDot(dst *ebiten.Image, x, y, r float32, c color.RGBA, al float64) {
	fastRect(dst, x-r, y-r, 2*r, 2*r, c, al)
}
