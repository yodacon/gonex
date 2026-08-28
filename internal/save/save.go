// Package save snapshots a running game to JSON and restores it. This
// replaces konex's game_Save, which wrote raw pointers to disk and could not
// actually round-trip a game.
package save

import (
	"encoding/json"
	"os"

	"yodacon.org/gonex/internal/ai"
	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/world"
)

const DefaultPath = "save.json"

type shipState struct {
	Pos     gmath.Vec2
	Vel     gmath.Vec2
	Heading float64
	ShipID  int
	Team    int
	Name    string
	Kind    int
	Health  int
	Money   int
	Frags   int
	Deaths  int
	Crew    int
	AI      string `json:",omitempty"`
}

type placement struct {
	Pos    gmath.Vec2
	Sprite int `json:",omitempty"`
	Team   int `json:",omitempty"`
}

type snapshot struct {
	MapW, MapH float64
	Scores     [4]int
	Ships      []shipState
	Planets    []placement
	Spawns     []placement
	MainPlayer int // index into Ships
}

func Write(w *world.World, path string) error {
	snap := snapshot{MapW: w.MapW, MapH: w.MapH, Scores: w.Scores, MainPlayer: -1}
	for _, e := range w.Entities {
		switch v := e.(type) {
		case *world.Ship:
			st := shipState{
				Pos: v.P, Vel: v.V, Heading: v.Heading, ShipID: v.ShipID,
				Team: int(v.Team), Name: v.Name, Kind: int(v.Kind),
				Health: v.Health, Money: v.Money, Frags: v.Frags,
				Deaths: v.Deaths, Crew: v.Crew,
			}
			if v.Controller != nil {
				st.AI = v.Controller.Name()
			}
			if v == w.MainPlayer {
				snap.MainPlayer = len(snap.Ships)
			}
			snap.Ships = append(snap.Ships, st)
		case *world.Planet:
			snap.Planets = append(snap.Planets, placement{Pos: v.P, Sprite: v.SpriteID})
		case *world.SpawnPoint:
			snap.Spawns = append(snap.Spawns, placement{Pos: v.P, Team: int(v.Team)})
		}
	}
	raw, err := json.MarshalIndent(snap, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// Read restores a snapshot into a fresh world and returns it.
func Read(w *world.World, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return err
	}

	w.MapW, w.MapH, w.Scores = snap.MapW, snap.MapH, snap.Scores
	for _, p := range snap.Planets {
		w.Add(&world.Planet{Body: world.Body{P: p.Pos}, SpriteID: p.Sprite})
	}
	for _, sp := range snap.Spawns {
		w.Add(&world.SpawnPoint{Body: world.Body{P: sp.Pos}, Team: world.Team(sp.Team)})
	}
	for i, st := range snap.Ships {
		s := w.NewShip(st.ShipID, world.Team(st.Team), st.Name, world.ShipKind(st.Kind))
		s.P, s.V, s.Heading = st.Pos, st.Vel, st.Heading
		s.Health, s.Money, s.Frags = st.Health, st.Money, st.Frags
		s.Deaths, s.Crew = st.Deaths, st.Crew
		if s.Kind == world.KindNPC {
			s.Controller = ai.ByName(st.AI)
		}
		if i == snap.MainPlayer {
			w.MainPlayer, w.ViewShip = s, s
		}
	}
	return nil
}
