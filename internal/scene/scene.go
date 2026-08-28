// Package scene loads konex's XML scene and map files into a world:
// a scene names a map file and a list of AI actors; the map defines size,
// planets and spawn points.
package scene

import (
	"encoding/xml"
	"fmt"

	"yodacon.org/gonex/assets"
	"yodacon.org/gonex/internal/ai"
	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/world"
)

type sceneXML struct {
	Map struct {
		Filename string  `xml:"filename,attr"`
		Width    float64 `xml:"width,attr"`
		Height   float64 `xml:"height,attr"`
	} `xml:"map"`
	Planets []placement `xml:"planets>planet"`
	Spawns  []placement `xml:"spawns>spawn"`
	Actors  []actorXML  `xml:"actors>actor"`
}

type placement struct {
	X      float64 `xml:"xpos,attr"`
	Y      float64 `xml:"ypos,attr"`
	Sprite int     `xml:"sprite,attr"`
	Team   int     `xml:"team,attr"`
}

type actorXML struct {
	X      float64 `xml:"xpos,attr"`
	Y      float64 `xml:"ypos,attr"`
	ShipID int     `xml:"shipid,attr"`
	Team   int     `xml:"team,attr"`
	Name   string  `xml:"name,attr"`
}

// Load reads a scene file (e.g. "deathmatch.xml") and populates the world.
func Load(w *world.World, path string) error {
	doc, err := read(path)
	if err != nil {
		return err
	}

	// The scene references its map by filename; the map file carries the
	// geometry. (A map file parses with the same schema.)
	if doc.Map.Filename != "" {
		mapDoc, err := read(doc.Map.Filename)
		if err != nil {
			return fmt.Errorf("map %s: %w", doc.Map.Filename, err)
		}
		applyMap(w, mapDoc)
	} else {
		applyMap(w, doc)
	}

	for _, a := range doc.Actors {
		name := a.Name
		if name == "" {
			name = "AI Player"
		}
		s := w.NewShip(a.ShipID, world.Team(a.Team), name, world.KindNPC)
		s.P = gmath.V(a.X, a.Y)
		s.Controller = ai.Random(w.Rand)
	}
	return nil
}

func applyMap(w *world.World, doc *sceneXML) {
	if doc.Map.Width > 0 {
		w.MapW, w.MapH = doc.Map.Width, doc.Map.Height
	}
	for _, p := range doc.Planets {
		w.Add(&world.Planet{
			Body:     world.Body{P: gmath.V(p.X, p.Y)},
			SpriteID: p.Sprite,
		})
	}
	for _, sp := range doc.Spawns {
		w.Add(&world.SpawnPoint{
			Body: world.Body{P: gmath.V(sp.X, sp.Y)},
			Team: world.Team(sp.Team),
		})
	}
}

func read(path string) (*sceneXML, error) {
	raw, err := assets.FS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc sceneXML
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("scene %s: %w", path, err)
	}
	return &doc, nil
}
