// Package mission runs the 1997 ConEx mission machine: 512 control bits,
// the 36 recovered mïsn records (data/conex/missions.json, with their
// original briefs), offer evaluation at a spaceport bar, and the
// accept → travel → return → complete lifecycle.
//
// Special stellar codes, per the EV Bible: -1 none, -4 "the stellar where
// the mission was accepted", 10000+n "a random stellar of govt n".
package mission

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"yodacon.org/gonex/assets"
)

type Def struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	AvailStel   int    `json:"availStel"`
	AvailBitSet int    `json:"availBitSet"`
	AvailBitClr int    `json:"availBitClr"`
	AvailRandom int    `json:"availRandom"`
	TravelStel  int    `json:"travelStel"`
	ReturnStel  int    `json:"returnStel"`
	CargoType   int    `json:"cargoType"`
	CargoName   string `json:"cargoName"`
	CargoQty    int    `json:"cargoQty"`
	Pay         int    `json:"pay"`
	ShipCount   int    `json:"shipCount"`
	CompBitSet  int    `json:"compBitSet"`
	TimeLimit   int    `json:"timeLimit"`
	Brief       string `json:"brief"`
	QuickBrief  string `json:"quickBrief"`
	CompText    string `json:"compText"`
	Restoration string `json:"restoration,omitempty"`
}

// Bits is the pilot's control-bit file.
type Bits [512]bool

func (b *Bits) Set(i int) {
	if i >= 0 && i < len(b) {
		b[i] = true
	}
}

func (b *Bits) Get(i int) bool { return i >= 0 && i < len(b) && b[i] }

// Active is a mission the pilot is flying.
type Active struct {
	Def      Def
	Origin   int  // stellar where it was accepted (resolves ReturnStel -4)
	Travel   int  // resolved travel stellar, -1 if none
	Return   int  // resolved return stellar, -1 if none
	PickedUp bool // cargo aboard (set on landing at Travel for cargo runs)
	DaysLeft int  // -1 = no limit
}

// Dest is where the mission wants the pilot next.
func (a *Active) Dest() int {
	if a.Travel >= 0 && !a.PickedUp {
		return a.Travel
	}
	return a.Return
}

type Table struct {
	Defs []Def
}

// Load reads the embedded mission table.
func Load() (*Table, error) {
	raw, err := assets.FS.ReadFile("data/conex/missions.json")
	if err != nil {
		return nil, err
	}
	var defs []Def
	if err := json.Unmarshal(raw, &defs); err != nil {
		return nil, fmt.Errorf("missions.json: %w", err)
	}
	return &Table{Defs: defs}, nil
}

// OffersAt evaluates every mission's GIVEN clause at a stellar's bar:
// right stellar, availability bit set, blocking bit clear, dice passed,
// and not already flying it.
func (t *Table) OffersAt(stellar int, bits *Bits, active []Active, rng *rand.Rand) []Def {
	flying := map[int]bool{}
	for _, a := range active {
		flying[a.Def.ID] = true
	}
	var out []Def
	for _, d := range t.Defs {
		if d.AvailStel != stellar || flying[d.ID] {
			continue
		}
		if d.AvailBitSet >= 0 && !bits.Get(d.AvailBitSet) {
			continue
		}
		if d.AvailBitClr >= 0 && bits.Get(d.AvailBitClr) {
			continue
		}
		if d.AvailRandom < 100 && rng.Intn(100) >= d.AvailRandom {
			continue
		}
		out = append(out, d)
	}
	return out
}

// resolveStel maps a stellar field to a concrete stellar for this playthrough.
func resolveStel(v, origin int, confed []int, rng *rand.Rand) int {
	switch {
	case v >= 10000 && v < 10128: // random stellar of a specific govt
		if len(confed) > 0 {
			return confed[rng.Intn(len(confed))]
		}
		return -1
	case v == -4:
		return origin
	case v >= 128:
		return v
	}
	return -1
}

// Accept starts a mission taken at `origin`. confed lists the candidate
// stellars for the govt-random codes (index 0 = govt 128, Confederation —
// the only govt ConEx's missions roll against).
func Accept(d Def, origin int, confed []int, rng *rand.Rand) Active {
	a := Active{
		Def: d, Origin: origin,
		Travel:   resolveStel(d.TravelStel, origin, confed, rng),
		Return:   resolveStel(d.ReturnStel, origin, confed, rng),
		DaysLeft: d.TimeLimit,
	}
	if d.TimeLimit < 0 {
		a.DaysLeft = -1
	}
	// A mission with cargo but no travel leg loads at the origin.
	if a.Travel < 0 && d.CargoQty > 0 {
		a.PickedUp = true
	}
	return a
}

// Event is what a landing did to a mission.
type Event int

const (
	Nothing Event = iota
	PickedUp
	Completed
	Failed
)

// Land advances a mission for a landing at `stellar`. On Completed the
// caller pays Def.Pay and sets the completion bit via Complete.
func (a *Active) Land(stellar int) Event {
	switch {
	case a.Travel >= 0 && !a.PickedUp && stellar == a.Travel:
		a.PickedUp = true
		if a.Return < 0 {
			return Completed
		}
		return PickedUp
	case a.PickedUp || a.Travel < 0:
		if dest := a.Return; dest >= 0 && stellar == dest {
			return Completed
		}
	}
	return Nothing
}

// PassDays burns deadline; it returns Failed when the clock runs out.
func (a *Active) PassDays(days int) Event {
	if a.DaysLeft < 0 {
		return Nothing
	}
	a.DaysLeft -= days
	if a.DaysLeft < 0 {
		return Failed
	}
	return Nothing
}

// Complete applies the SETS clause.
func Complete(d Def, bits *Bits) {
	if d.CompBitSet >= 0 {
		bits.Set(d.CompBitSet)
	}
}
