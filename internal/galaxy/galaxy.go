// Package galaxy holds the star map: every system and stellar of base
// EV 1.0.4 overlaid with ConEx 1.2, exported from the yodacon gazetteer
// into data/conex/galaxy.json. It answers the questions travel asks —
// what's here, what links where, and how do I route to a destination.
package galaxy

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"yodacon.org/gonex/assets"
)

type System struct {
	Name     string `json:"name"`
	X, Y     int
	Links    []int  `json:"links"`
	Stellars []int  `json:"stellars"`
	Govt     string `json:"govt"`
	Source   string `json:"source"`
}

type Landing struct {
	AtmosScale        float64 `json:"atmosScale"`
	GravityScale      float64 `json:"gravityScale"`
	CorridorHalfWidth float64 `json:"corridorHalfWidthDeg"`
	PadBonus          int     `json:"padBonus"`
}

type Stellar struct {
	Name    string  `json:"name"`
	System  int     `json:"system"`
	Govt    string  `json:"govt"`
	Tech    int     `json:"tech"`
	Sprite  int     `json:"sprite"`
	Landing Landing `json:"landing"`
	LandPic int     `json:"landPic"` // 1997 CustPicID view, 0 = none
	Source  string  `json:"source"`
}

type Galaxy struct {
	Systems  map[int]*System
	Stellars map[int]*Stellar
}

// Load reads the embedded galaxy data.
func Load() (*Galaxy, error) {
	raw, err := assets.FS.ReadFile("data/conex/galaxy.json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Systems  map[string]*System  `json:"systems"`
		Stellars map[string]*Stellar `json:"stellars"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("galaxy.json: %w", err)
	}
	g := &Galaxy{Systems: map[int]*System{}, Stellars: map[int]*Stellar{}}
	for k, v := range doc.Systems {
		var id int
		fmt.Sscanf(k, "%d", &id)
		g.Systems[id] = v
	}
	for k, v := range doc.Stellars {
		var id int
		fmt.Sscanf(k, "%d", &id)
		g.Stellars[id] = v
	}
	return g, nil
}

// StellarsIn lists a system's stellar IDs, sorted.
func (g *Galaxy) StellarsIn(sys int) []int {
	s := g.Systems[sys]
	if s == nil {
		return nil
	}
	out := append([]int(nil), s.Stellars...)
	sort.Ints(out)
	return out
}

// Route returns the shortest link-path from one system to another,
// inclusive of both ends, or nil if unreachable. BFS over the recovered
// Con1–Con16 hyperspace links; ties break toward lower system IDs so the
// route is deterministic.
func (g *Galaxy) Route(from, to int) []int {
	if from == to {
		return []int{from}
	}
	if g.Systems[from] == nil || g.Systems[to] == nil {
		return nil
	}
	prev := map[int]int{from: from}
	queue := []int{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		links := append([]int(nil), g.Systems[cur].Links...)
		sort.Ints(links)
		for _, next := range links {
			if _, seen := prev[next]; seen || g.Systems[next] == nil {
				continue
			}
			prev[next] = cur
			if next == to {
				path := []int{to}
				for at := to; at != from; {
					at = prev[at]
					path = append([]int{at}, path...)
				}
				return path
			}
			queue = append(queue, next)
		}
	}
	return nil
}

// --- Chartering ---

// A scene places planets the gazetteer never listed: the yards and deep
// stations a map hangs off a capital, and the contested ground in the middle
// that nobody's colour flies out of. They are real ports — they have a
// skyline, a pad and a market — but the 1997 record has no spöb number for
// them, and every system downstream of the docking handshake is keyed on one.
//
// So the game charters them. A chartered port is a stellar record minted at
// arrival out of what a pilot could actually see from the approach: the name
// on the beacon, the disc in the window, and the size of the city under it.
// Once chartered it is a port like any other — you can request clearance, fly
// the corridor, trade on its board and take off again.
const (
	// CharterBase is where minted IDs begin. The recovered gazetteer tops out
	// in the low hundreds and the mission generator's random destination
	// codes live at 10000+, so nothing minted here can be mistaken for a
	// number the 1997 data actually used.
	CharterBase = 20000
	charterSpan = 100000

	// CharterSource marks a stellar as minted rather than recovered. It is
	// what tells the arena's own furniture apart from a system's scenery.
	CharterSource = "scene"
)

// Chartered reports whether an ID names a port this run minted rather than
// one the gazetteer recovered.
func (g *Galaxy) Chartered(id int) bool {
	st := g.Stellars[id]
	return st != nil && st.Source == CharterSource
}

// Charter registers a port under a name and returns its stellar ID. It is
// idempotent by name: chartering the same world twice — on re-entry, or
// after a save that stored the pilot docked there — returns the ID it had
// the first time, because the ID is derived from the name rather than handed
// out by a counter. A chartered port is deliberately NOT filed in its
// system's stellar list: it stands where the map put it, and re-dressing the
// sky must not grow a second copy of it in the ring.
func (g *Galaxy) Charter(name string, system, sprite, pop int) int {
	if name == "" {
		name = "Unsurveyed"
	}
	id := CharterBase + int(nameHash(name)%charterSpan)
	for {
		st, taken := g.Stellars[id]
		if !taken {
			break
		}
		if st.Source == CharterSource && st.Name == name {
			return id // already surveyed, on an earlier arrival
		}
		id++ // two names landed on the same slip: take the next one along
	}
	g.Stellars[id] = &Stellar{
		Name:    name,
		System:  system,
		Govt:    govtOf(g.Systems[system]),
		Tech:    charterTech(pop),
		Sprite:  charterSprite(sprite),
		Landing: charterLanding(name, pop),
		Source:  CharterSource,
	}
	return id
}

// Restation moves a chartered port onto the system the arena is currently
// standing in, so its market and its flag read as the sky around it. The
// map's furniture does not travel; the sky over it does.
func (g *Galaxy) Restation(id, system int) {
	if st := g.Stellars[id]; st != nil && st.Source == CharterSource {
		st.System, st.Govt = system, govtOf(g.Systems[system])
	}
}

func govtOf(s *System) string {
	if s == nil || s.Govt == "" {
		return "Independent"
	}
	return s.Govt
}

// charterSprite runs the deorbit cinematic's `1 + sprite%18` backwards, so a
// chartered world falls out of the sky wearing the disc it wore in flight.
func charterSprite(spriteID int) int {
	if spriteID < 1 || spriteID > 18 {
		return 1
	}
	return spriteID - 1
}

// charterTech reads industry off the skyline. The ladder matches the
// recovered data, where a tech-5 world is a capital and a tech-0 world is a
// rock with a strip on it.
func charterTech(pop int) int {
	switch {
	case pop >= 3000000:
		return 5
	case pop >= 1500000:
		return 4
	case pop >= 800000:
		return 3
	case pop >= 400000:
		return 2
	case pop >= 150000:
		return 1
	}
	return 0
}

// charterLanding surveys the approach. The big ports are the hard ones and
// the well-paid ones — Earth is the tightest corridor in the gazetteer and
// the richest pad bonus in it — so the line narrows and the money rises
// together with the city. The jitter is keyed on the name, so a given map
// always flies the same way.
func charterLanding(name string, pop int) Landing {
	h := nameHash(name)
	f := math.Min(1, float64(charterTech(pop))/5)
	return Landing{
		AtmosScale:        0.8 + float64(h%5)*0.1,
		GravityScale:      0.85 + float64((h>>3)%5)*0.075,
		CorridorHalfWidth: 0.58 - 0.30*f + float64((h>>6)%13)*0.001,
		PadBonus:          2000 + int(f*8000),
	}
}

// nameHash is FNV-1a, spelled out so a chartered ID stays the same number
// across builds and platforms.
func nameHash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return h
}
