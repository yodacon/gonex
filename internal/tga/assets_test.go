package tga_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing"

	"yodacon.org/gonex/assets"
	"yodacon.org/gonex/internal/tga"
)

// Three of the planet discs shipped for years as bright colour noise. The
// cause was not the art: the files had been moved through something in text
// mode, and every 0x0A byte in the pixel data had come out the far side as
// 0x0D 0x0A. Each inserted byte shears the rest of the image one channel to
// the right, which is exactly what a garbage rainbow looks like.
//
// A text-mode round trip cannot hide from arithmetic. An uncompressed Targa
// is a fixed-size header followed by exactly width*height*depth/8 bytes, so
// a file whose length does not land on that number has had bytes put into it
// or taken out of it. This walks every image the game ships and insists on
// the count — the damage is silent in a decoder and loud in a subtraction.
func TestShippedTargasAreNotTextMangled(t *testing.T) {
	var checked int
	err := fs.WalkDir(assets.FS, "data", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(path.Ext(p), ".tga") {
			return err
		}
		raw, err := assets.FS.ReadFile(p)
		if err != nil {
			return err
		}
		if len(raw) < 18 {
			t.Errorf("%s: %d bytes, shorter than a Targa header", p, len(raw))
			return nil
		}
		var (
			idLen  = int(raw[0])
			width  = int(raw[12]) | int(raw[13])<<8
			height = int(raw[14]) | int(raw[15])<<8
			bpp    = int(raw[16]) / 8
		)
		want := 18 + idLen + width*height*bpp
		if len(raw) != want {
			excess := len(raw) - want
			t.Errorf("%s: %d bytes, want %d for %dx%d at %d bpp (%+d; file holds %d CRLF pairs)",
				p, len(raw), want, width, height, bpp*8, excess, bytes.Count(raw, []byte("\r\n")))
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if checked == 0 {
		t.Fatal("no .tga files found under assets/data")
	}
	t.Logf("%d shipped Targas measured", checked)
}

// A repaired disc has to decode to a disc: opaque paint in the middle, clear
// space at the corners. Length alone would still pass a file that had been
// padded back to size with rubbish.
func TestPlanetDiscsDecodeAsDiscs(t *testing.T) {
	for i := 0; i < 18; i++ {
		p := fmt.Sprintf("data/planets/%02d/pic.tga", i)
		raw, err := assets.FS.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		img, err := tga.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		b := img.Bounds()
		w, h := b.Dx(), b.Dy()
		if _, _, _, a := img.At(w/2, h/2).RGBA(); a == 0 {
			t.Errorf("%s: centre of the disc is transparent", p)
		}
		for _, c := range [][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}} {
			if _, _, _, a := img.At(c[0], c[1]).RGBA(); a != 0 {
				t.Errorf("%s: corner %v is opaque — the disc does not fit its frame", p, c)
			}
		}
	}
}
