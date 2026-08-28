// Package ship loads the ship catalog: per-ship flight specs (specs.xml, in
// konex's format) plus the 36-frame rotation sprite bank and the target /
// yard / comm pictures.
package ship

import (
	"encoding/xml"
	"fmt"
	"math/rand"
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"

	"yodacon.org/gonex/assets"
	"yodacon.org/gonex/internal/gmath"
)

// Folders lists the ship data folders, in catalog order. IDs are 1-based to
// match konex ship IDs (config.PlayerShipID 12 is still the yodacon).
var Folders = []string{
	"advisor", "callisto", "citrus", "dart", "defender", "gryphon",
	"large pin", "necromancer", "small pin", "tomaquad", "trident", "yodacon",
	// recovered byte-exact from the original 1997 ConEx plugin
	"yodacon97", "small-pin97", "medium-pin97", "large-pin97", "dart97",
	"defender97", "gryphon97", "trident97", "necromancer97", "tomaquad97",
}

type Spec struct {
	Name            string
	MaxVelocity     float64 // world units / sec
	TurnSpeed       float64 // degrees / sec
	Acceleration    float64 // world units / sec^2
	Mass            float64
	CollisionRadius float64
	Damage          int
}

type Ship struct {
	Spec
	Folder  string
	Sprites [36]*ebiten.Image
	Target  *ebiten.Image
	Yard    *ebiten.Image
	Comm    *ebiten.Image
}

// SpriteFor returns the rotation frame for a heading in degrees
// (frame 0 faces up, 10 degrees per frame, clockwise).
func (s *Ship) SpriteFor(headingDeg float64) *ebiten.Image {
	idx := int(gmath.WrapDeg(headingDeg)/10) % 36
	return s.Sprites[idx]
}

// Catalog holds all loaded ships, addressed by 1-based ID.
type Catalog struct {
	ships []*Ship // index 0 unused
}

func LoadCatalog() (*Catalog, error) {
	c := &Catalog{ships: make([]*Ship, 1, len(Folders)+1)}
	for _, folder := range Folders {
		s, err := loadShip(folder)
		if err != nil {
			return nil, fmt.Errorf("ship %q: %w", folder, err)
		}
		c.ships = append(c.ships, s)
	}
	return c, nil
}

func (c *Catalog) Get(id int) *Ship {
	if id < 1 || id >= len(c.ships) {
		return c.ships[1]
	}
	return c.ships[id]
}

func (c *Catalog) Count() int { return len(c.ships) - 1 }

func (c *Catalog) PickRandom(r *rand.Rand) int { return 1 + r.Intn(c.Count()) }

func loadShip(folder string) (*Ship, error) {
	s := &Ship{Folder: folder}
	base := "data/ships/" + folder + "/"
	for i := 0; i < 36; i++ {
		img, err := assets.Image(fmt.Sprintf("%s%02d.tga", base, i))
		if err != nil {
			return nil, err
		}
		s.Sprites[i] = img
	}
	for _, p := range []struct {
		dst  **ebiten.Image
		name string
	}{{&s.Target, "target.tga"}, {&s.Yard, "yard.tga"}, {&s.Comm, "comm.tga"}} {
		img, err := assets.Image(base + p.name)
		if err != nil {
			return nil, err
		}
		*p.dst = img
	}

	raw, err := assets.FS.ReadFile(base + "specs.xml")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Settings []struct {
			XMLName xml.Name
			Value   string `xml:"Value,attr"`
		} `xml:",any"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	for _, f := range doc.Settings {
		v := f.Value
		switch f.XMLName.Local {
		case "ShipName":
			s.Name = v
		case "MaxVelocity":
			s.MaxVelocity, _ = strconv.ParseFloat(v, 64)
		case "TurnSpeed":
			s.TurnSpeed, _ = strconv.ParseFloat(v, 64)
		case "Acceleration":
			s.Acceleration, _ = strconv.ParseFloat(v, 64)
		case "Mass":
			s.Mass, _ = strconv.ParseFloat(v, 64)
		case "CollisionRadius":
			s.CollisionRadius, _ = strconv.ParseFloat(v, 64)
		case "Damage":
			s.Damage, _ = strconv.Atoi(v)
		}
	}
	return s, nil
}
