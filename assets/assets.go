// Package assets embeds the game data (ported from konex/dist) and serves
// decoded, cached textures. File paths mirror the original layout:
// "data/ships/<name>/00.tga", "deathmatch.xml", and so on. Images may be
// TGA (the original art) or PNG (art recovered from the 1997 ConEx plugin).
package assets

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	_ "image/png"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"yodacon.org/gonex/internal/tga"
)

//go:embed all:data deathmatch.xml sundaydrive.xml
var FS embed.FS

var cache = map[string]*ebiten.Image{}

// Image loads and caches a texture by embedded path. If the exact path is
// missing, the sibling extension (.tga <-> .png) is tried, so ship folders can
// mix original TGA art with recovered PNG art.
func Image(path string) (*ebiten.Image, error) {
	if img, ok := cache[path]; ok {
		return img, nil
	}
	resolved := path
	raw, err := FS.ReadFile(path)
	if err != nil {
		alt := siblingExt(path)
		if alt == "" {
			return nil, err
		}
		if raw, err = FS.ReadFile(alt); err != nil {
			return nil, fmt.Errorf("assets: %s (and %s) not found", path, alt)
		}
		resolved = alt
	}

	var decoded image.Image
	if strings.HasSuffix(strings.ToLower(resolved), ".tga") {
		decoded, err = tga.Decode(bytes.NewReader(raw))
	} else {
		decoded, _, err = image.Decode(bytes.NewReader(raw))
	}
	if err != nil {
		return nil, fmt.Errorf("assets: decode %s: %w", path, err)
	}
	img := ebiten.NewImageFromImage(decoded)
	cache[path] = img
	return img, nil
}

func siblingExt(path string) string {
	switch {
	case strings.HasSuffix(path, ".tga"):
		return strings.TrimSuffix(path, ".tga") + ".png"
	case strings.HasSuffix(path, ".png"):
		return strings.TrimSuffix(path, ".png") + ".tga"
	}
	return ""
}
