// Package save snapshots a running game to JSON and restores it. This
// replaces konex's game_Save, which wrote raw pointers to disk and could not
// actually round-trip a game.
package save

import (
	"encoding/json"
	"os"

	"yodacon.org/gonex/internal/ai"
	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/market"
	"yodacon.org/gonex/internal/mission"
	"yodacon.org/gonex/internal/power"
	"yodacon.org/gonex/internal/reentry"
	"yodacon.org/gonex/internal/world"
)

const DefaultPath = "save.json"

// PilotState is the voyage half of a save — everything the trader's life
// carries between landings. The world half snapshots the flight scene. A
// berth save written on the pad (DockStellar > 0) is what the DED screen
// resumes from.
type PilotState struct {
	Credits, Day, System int
	Fuel, FuelMax        int
	Lithium, LiMax       float64
	RCSFuel, RCSMax      float64
	Cargo                []int          // tons per market commodity
	Events               []market.Event // the news in flight
	Grid                 *power.Grid
	Dmg                  reentry.Damage
	BitsSet              []int // indices of set mission control bits
	Active               []mission.Active
	Route                []int
	Escorts              []int // hired escort ship IDs
	Crew                 int
	PlayerShipID         int
	DockStellar          int // >0: saved on the pad at this stellar
}

// Exists reports whether a save is on disk at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

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

	Role   int     `json:",omitempty"`
	Rounds int     `json:",omitempty"`
	Hold   []int   `json:",omitempty"`
	Junk   float64 `json:",omitempty"`
	BattMJ float64 `json:",omitempty"`
}

type placement struct {
	Pos     gmath.Vec2
	Sprite  int     `json:",omitempty"`
	Team    int     `json:",omitempty"`
	Stellar int     `json:",omitempty"`
	Name    string  `json:",omitempty"`
	Pop     int     `json:",omitempty"`
	IP      float64 `json:",omitempty"`
	Credits int     `json:",omitempty"`
	Scrap   float64 `json:",omitempty"`
}

type snapshot struct {
	MapW, MapH float64
	Scores     [4]int
	Ships      []shipState
	Planets    []placement
	Spawns     []placement
	MainPlayer int         // index into Ships
	Pilot      *PilotState `json:",omitempty"`
}

func Write(w *world.World, pilot *PilotState, path string) error {
	snap := snapshot{MapW: w.MapW, MapH: w.MapH, Scores: w.Scores,
		MainPlayer: -1, Pilot: pilot}
	for _, e := range w.Entities {
		switch v := e.(type) {
		case *world.Ship:
			st := shipState{
				Pos: v.P, Vel: v.V, Heading: v.Heading, ShipID: v.ShipID,
				Team: int(v.Team), Name: v.Name, Kind: int(v.Kind),
				Health: v.Health, Money: v.Money, Frags: v.Frags,
				Deaths: v.Deaths, Crew: v.Crew,
				Role: int(v.Role), Rounds: v.Rounds, Hold: v.Hold, Junk: v.Junk,
			}
			if v.Grid != nil {
				st.BattMJ = v.Grid.BattMJ
			}
			if v.Controller != nil {
				st.AI = v.Controller.Name()
			}
			if v == w.MainPlayer {
				snap.MainPlayer = len(snap.Ships)
			}
			snap.Ships = append(snap.Ships, st)
		case *world.Planet:
			snap.Planets = append(snap.Planets, placement{
				Pos: v.P, Sprite: v.SpriteID, Team: int(v.Team), Name: v.Name,
				Stellar: v.StellarID, Pop: v.Pop, IP: v.IP,
				Credits: v.Credits, Scrap: v.Scrap,
			})
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

// Read restores a snapshot into a fresh world and returns the pilot state
// (nil for a pre-voyage save).
func Read(w *world.World, path string) (*PilotState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}

	w.MapW, w.MapH, w.Scores = snap.MapW, snap.MapH, snap.Scores
	for _, p := range snap.Planets {
		pl := &world.Planet{
			Body: world.Body{P: p.Pos}, SpriteID: p.Sprite,
			Team: world.Team(p.Team), Name: p.Name, StellarID: p.Stellar,
		}
		pl.Setup(p.Pop)
		// A restored planet keeps the ledger it had, not a fresh buffer.
		if p.Pop > 0 {
			pl.IP, pl.Credits, pl.Scrap = p.IP, p.Credits, p.Scrap
		}
		w.Add(pl)
	}
	for _, sp := range snap.Spawns {
		w.Add(&world.SpawnPoint{Body: world.Body{P: sp.Pos}, Team: world.Team(sp.Team)})
	}
	for i, st := range snap.Ships {
		s := w.NewShip(st.ShipID, world.Team(st.Team), st.Name, world.ShipKind(st.Kind))
		s.P, s.V, s.Heading = st.Pos, st.Vel, st.Heading
		s.Health, s.Money, s.Frags = st.Health, st.Money, st.Frags
		s.Deaths, s.Crew = st.Deaths, st.Crew
		// Crew and role size the manifest, so re-outfit before restoring it.
		s.Role = world.Role(st.Role)
		s.Outfit(w)
		if st.Hold != nil {
			copy(s.Hold, st.Hold)
		}
		s.Junk = st.Junk
		if st.Rounds > 0 {
			s.Rounds = st.Rounds
		}
		if st.BattMJ > 0 && s.Grid != nil {
			s.Grid.BattMJ = st.BattMJ
		}
		if s.Kind == world.KindNPC {
			s.Controller = ai.Parse(st.AI, w.Rand)
		}
		if i == snap.MainPlayer {
			w.MainPlayer, w.ViewShip = s, s
		}
	}
	return snap.Pilot, nil
}
