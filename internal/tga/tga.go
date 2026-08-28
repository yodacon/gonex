// Package tga decodes the Targa images konex shipped its art in:
// uncompressed true-color (type 2), 24- or 32-bit, either vertical origin.
package tga

import (
	"fmt"
	"image"
	"image/color"
	"io"
)

func Decode(r io.Reader) (image.Image, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(raw) < 18 {
		return nil, fmt.Errorf("tga: file too short")
	}
	idLen := int(raw[0])
	imageType := raw[2]
	width := int(raw[12]) | int(raw[13])<<8
	height := int(raw[14]) | int(raw[15])<<8
	depth := int(raw[16])
	topOrigin := raw[17]&0x20 != 0

	if imageType != 2 || (depth != 24 && depth != 32) {
		return nil, fmt.Errorf("tga: unsupported variant type=%d depth=%d", imageType, depth)
	}
	bpp := depth / 8
	pixels := raw[18+idLen:]
	if len(pixels) < width*height*bpp {
		return nil, fmt.Errorf("tga: truncated pixel data")
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcY := y
		if !topOrigin {
			srcY = height - 1 - y
		}
		for x := 0; x < width; x++ {
			p := pixels[(srcY*width+x)*bpp:]
			a := uint8(255)
			if bpp == 4 {
				a = p[3]
			}
			img.SetNRGBA(x, y, color.NRGBA{R: p[2], G: p[1], B: p[0], A: a})
		}
	}
	return img, nil
}
