// Package galaxy holds the star map: every system and stellar of base
// EV 1.0.4 overlaid with ConEx 1.2, exported from the yodacon gazetteer
// into data/conex/galaxy.json. It answers the questions travel asks —
// what's here, what links where, and how do I route to a destination.
package galaxy

import (
	"encoding/json"
	"fmt"
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
