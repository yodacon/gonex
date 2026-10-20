//go:build ignore

// feature-sheets draws the two pictures in docs/FEATURE_GUIDE.md that are
// made from the repo's own art rather than from a running game:
//
//	docs/features/02-yodacon97.png   the Yodacon '97 sprite bank: all 36
//	                                 rotations, then its shipyard, comm and
//	                                 target pictures
//	docs/features/09-landviews.png   the three recovered 1997 landing views
//
// Standard library only. Run from the repo root:
//
//	go run scripts/feature-sheets.go
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

var bg = color.RGBA{0x0a, 0x0c, 0x12, 0xff} // the docked screens' near-black

func load(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		log.Fatalf("%s: %v", path, err)
	}
	return img
}

// scaled returns src enlarged k times, nearest neighbour, so the 1997
// pixels stay pixels.
func scaled(src image.Image, k int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*k, b.Dy()*k))
	for y := 0; y < b.Dy()*k; y++ {
		for x := 0; x < b.Dx()*k; x++ {
			dst.Set(x, y, src.At(b.Min.X+x/k, b.Min.Y+y/k))
		}
	}
	return dst
}

func canvas(w, h int) *image.RGBA {
	c := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(c, c.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	return c
}

func put(dst *image.RGBA, src image.Image, x, y int) {
	r := src.Bounds()
	draw.Draw(dst, image.Rect(x, y, x+r.Dx(), y+r.Dy()), src, r.Min, draw.Over)
}

func save(img image.Image, path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	st, _ := os.Stat(path)
	fmt.Printf("%s: %d KB\n", path, st.Size()/1024)
}

func yodaconSheet() {
	const (
		dir    = "assets/data/ships/yodacon97"
		k      = 2 // 70 px frames drawn at 140 px
		cols   = 9 // 36 frames = 9 x 4
		cell   = 70 * k
		gap    = 8
		margin = 16
	)
	w := margin*2 + cols*cell + (cols-1)*gap
	rows := 36 / cols
	h := margin*2 + rows*cell + (rows-1)*gap + gap*3 + 100*k
	c := canvas(w, h)
	for i := 0; i < 36; i++ {
		x := margin + (i%cols)*(cell+gap)
		y := margin + (i/cols)*(cell+gap)
		put(c, scaled(load(filepath.Join(dir, fmt.Sprintf("%02d.png", i))), k), x, y)
	}
	y := margin + rows*(cell+gap) + gap*2
	x := margin
	for _, name := range []string{"yard.png", "comm.png", "target.png"} {
		img := scaled(load(filepath.Join(dir, name)), k)
		put(c, img, x, y)
		x += img.Bounds().Dx() + gap*3
	}
	save(c, "docs/features/02-yodacon97.png")
}

func landViews() {
	const (
		dir    = "assets/data/conex/land"
		gap    = 12
		margin = 16
	)
	names := []string{"13514.png", "13515.png", "13516.png"}
	var imgs []image.Image
	w, h := margin*2+gap*(len(names)-1), 0
	for _, n := range names {
		img := load(filepath.Join(dir, n))
		imgs = append(imgs, img)
		w += img.Bounds().Dx()
		if d := img.Bounds().Dy(); d > h {
			h = d
		}
	}
	c := canvas(w, h+margin*2)
	x := margin
	for _, img := range imgs {
		put(c, img, x, margin)
		x += img.Bounds().Dx() + gap
	}
	save(c, "docs/features/09-landviews.png")
}

func main() {
	yodaconSheet()
	landViews()
}
