// Package market is the commodity exchange under the trader game: a small
// board of goods whose prices move like a stock market — a per-station
// bias (the geography), a slow sinusoidal day walk (the cycle), and world
// events (the news) that spike or crash a commodity locally or everywhere.
// Buy low at one station, haul it across the lanes, sell high at another;
// the cargo hold is the position and the jump fuel is the spread.
package market

import (
	"fmt"
	"math"
	"math/rand"
)

type Commodity struct {
	Name string
	Base int // credits per ton at multiplier 1
}

// Commodities is the board. Deliberately few: a market you can hold in
// your head is a market you can play.
var Commodities = []Commodity{
	{"Lumber", 140},
	{"Ore", 220},
	{"Rations", 90},
	{"Medicine", 480},
	{"Chips", 640},
	{"Fuel cells", 300},
}

// Event is one piece of world news moving a price.
type Event struct {
	Name      string
	Commodity int
	System    int     // -1 = everywhere
	Mult      float64 // price multiplier while active
	DaysLeft  int
}

func hash(n int) uint32 {
	h := uint32(n) * 2654435761
	h = (h ^ (h >> 13)) * 1274126177
	return h ^ (h >> 16)
}

// Price quotes commodity c at a stellar in a system on a given day.
// Deterministic outside the event list: the same day at the same station
// always quotes the same number, which is what makes a route plannable.
func Price(sys, stellar, c, day int, events []Event) int {
	base := float64(Commodities[c].Base)
	// station bias: the geography of the market, fixed per station
	bias := 0.72 + float64(hash(stellar*31+c*7)%1000)/1000*0.66
	// the day walk: a slow cycle, phase-shifted per commodity and system
	phase := float64(hash(c*13+sys*5)%628) / 100
	walk := 1 + 0.22*math.Sin(float64(day)*0.31+phase)
	m := bias * walk
	for _, e := range events {
		if e.Commodity == c && (e.System < 0 || e.System == sys) {
			m *= e.Mult
		}
	}
	p := int(base * m)
	if p < 10 {
		p = 10
	}
	return p
}

// Trend compares today with yesterday at a station: +1 rising, -1
// falling, 0 flat-ish.
func Trend(sys, stellar, c, day int, events []Event) int {
	now := Price(sys, stellar, c, day, events)
	prev := Price(sys, stellar, c, day-1, events)
	switch {
	case now > prev+2:
		return 1
	case now < prev-2:
		return -1
	}
	return 0
}

// the news wire: what can happen to a market
var templates = []struct {
	verb   string
	c      int
	mult   float64
	global bool
}{
	{"Construction boom", 0, 2.0, false},
	{"Mining strike", 1, 2.2, true},
	{"Bumper harvest", 2, 0.45, false},
	{"Plague outbreak", 3, 3.0, false},
	{"Chip shortage", 4, 1.9, true},
	{"Fuel glut", 5, 0.55, true},
	{"Refinery fire", 5, 2.1, false},
	{"Ore market crash", 1, 0.5, true},
}

// Step advances the news by a number of days: active events burn down and
// expire, and each day has a chance of breaking a new story. systems is
// the pool of system IDs an event can strike; sysName renders one for the
// headline. Returns the surviving list and the day's headlines.
func Step(events []Event, days int, systems []int, rng *rand.Rand,
	sysName func(int) string) ([]Event, []string) {
	var news []string
	kept := events[:0]
	for _, e := range events {
		e.DaysLeft -= days
		if e.DaysLeft <= 0 {
			news = append(news, fmt.Sprintf("MARKET: %s over — %s prices settle.",
				e.Name, Commodities[e.Commodity].Name))
			continue
		}
		kept = append(kept, e)
	}
	events = kept
	for d := 0; d < days && len(events) < 4; d++ {
		if rng.Float64() > 0.18 {
			continue
		}
		t := templates[rng.Intn(len(templates))]
		e := Event{Commodity: t.c, Mult: t.mult, System: -1,
			DaysLeft: 6 + rng.Intn(10)}
		where := "across the lanes"
		if !t.global && len(systems) > 0 {
			e.System = systems[rng.Intn(len(systems))]
			where = "on " + sysName(e.System)
		}
		dir := "up"
		if t.mult < 1 {
			dir = "down"
		}
		e.Name = t.verb
		events = append(events, e)
		news = append(news, fmt.Sprintf("MARKET: %s %s — %s %s sharply.",
			t.verb, where, Commodities[t.c].Name, dir))
	}
	return events, news
}
