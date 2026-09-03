package world

import (
	"fmt"
	"math"
	"strings"

	"yodacon.org/gonex/internal/gmath"
)

// The planet is the other half of the war. A fighter that must land to rearm
// is a fighter with a home; a home that has to pay for those rounds out of a
// finite industrial capacity is a home that can be starved. Nothing here
// shoots at anybody — this file is how territory is actually lost.

const (
	// PopPerIP is how many citizens it takes to sustain one industrial point
	// per day. The port's skyline sets the population, so the city the pilot
	// lands in is the number that arms the squadron.
	PopPerIP = 40000
	// DaySec is how many battle seconds make an industrial day.
	DaySec = 120.0

	// RoundCr is what one round costs, at a planet's pad and over the dock
	// counter alike — the player and the AI buy from the same armoury.
	RoundCr = 3

	// What a turnaround costs, in credits and in industrial points.
	//
	// Power is a UTILITY, not manufacturing: it is metered, it is cheap, and
	// it is never refused — a planet with an empty treasury still puts its
	// grid on the pad connector. That is what guarantees a ship can always
	// limp home and get back up, which is the promise the whole economy rests
	// on. Rounds and repairs come out of industrial capacity, and those are
	// the taps that run dry.
	energyCrPerMJ = 0.05
	roundIP       = 0.05 // one point per twenty rounds
	repairCr      = 4
	repairIP      = 0.4
	scrapCrPerTon = 60

	// The pad. A turnaround is short, but it is time not spent shooting, and
	// a queue under fire is a massacre.
	padBase = 2.0
	padMax  = 8.0
)

// Service is what a turnaround actually managed — the receipt, printed to
// the console so a player can read the fleet's logistics as it happens.
type Service struct {
	Rounds   int
	Hull     int
	EnergyMJ float64
	Scrap    float64
	Cost     int
	Refused  string // what the planet could not pay for, if anything
}

// Land puts a ship on a planet's pad if there is a berth free, runs the
// turnaround, and returns whether it got in. A ship that cannot get a berth
// keeps flying and tries again — it does not queue in the air, it orbits.
func (w *World) Land(s *Ship, p *Planet) bool {
	if p == nil || s.Docked() || p.Team != s.Team || len(p.Pad) >= p.Berths() {
		return false
	}
	svc := p.serve(s)
	s.Pad, s.PadCD = p, svc.secs()
	s.V = gmath.Vec2{}
	p.Pad = append(p.Pad, s)

	if s.Kind == KindNPC {
		w.Notify("%s LAND %s — %s", s.Name, p.Label(), svc.line())
	}
	return true
}

// Launch takes a ship off the pad when its turnaround is done.
func (p *Planet) Launch(w *World, s *Ship) {
	for i, o := range p.Pad {
		if o == s {
			p.Pad = append(p.Pad[:i], p.Pad[i+1:]...)
			break
		}
	}
	s.Pad, s.PadCD = nil, 0
	// Push clear of the pad so the ship is not landing again next frame.
	s.Heading = float64(w.Rand.Intn(360))
	s.V = gmath.HeadingVec(s.Heading).Scale(220)
	s.P = p.P.Add(gmath.HeadingVec(s.Heading).Scale(CollisionRange * 1.6))
	if s.Kind == KindNPC {
		w.Notify("%s LAUNCH %s — %d rounds, %d%% hull", s.Name, p.Label(),
			s.Rounds, s.Health)
	}
}

// serve is the transaction. Order matters and it is the whole design: energy
// first, then rounds, then repairs. A planet running out of capacity stops
// fixing hulls before it stops selling bullets, and stops selling bullets
// before it stops giving away power — so a starving world's squadrons fly out
// fully fuelled, dented, and carrying eleven rounds.
func (p *Planet) serve(s *Ship) Service {
	var svc Service

	if s.Grid != nil {
		mj := (s.Grid.BattCapMJ - s.Grid.BattMJ) + (s.Grid.CapCapMJ - s.Grid.CapMJ)
		s.Grid.BattMJ, s.Grid.CapMJ = s.Grid.BattCapMJ, s.Grid.CapCapMJ
		s.Grid.HeatMJ = 0
		svc.EnergyMJ = mj
		// Metered, and billed only as far as the treasury goes. Nobody is
		// ever turned away from the connector.
		if cost := int(mj * energyCrPerMJ); cost > 0 {
			p.Credits -= min(cost, p.Credits)
		}
	}

	// Salvage is sold before the resupply is priced, so a ship that hauled a
	// wreck home helps pay for its own rearm — and the yard keeps the scrap.
	if s.Junk > 0 {
		p.Scrap += s.Junk
		s.Money += int(s.Junk * scrapCrPerTon)
		svc.Scrap, s.Junk = s.Junk, 0
	}

	if want := float64(s.RoundsMax - s.Rounds); want > 0 {
		got := int(p.spend(want, RoundCr, roundIP))
		s.Rounds += got
		svc.Rounds = got
		if float64(got) < want {
			svc.Refused = "rounds"
		}
	}

	if want := float64(maxHealth - s.Health); want > 0 {
		got := int(p.spend(want, repairCr, repairIP))
		s.Health += got
		svc.Hull = got
		if float64(got) < want {
			svc.Refused = "repairs"
		}
	}
	return svc
}

// spend buys as many units as the planet's capacity and treasury allow,
// returning how many it could actually cover.
func (p *Planet) spend(want, crPerUnit, ipPerUnit float64) float64 {
	if want <= 0 {
		return 0
	}
	got := want
	if ipPerUnit > 0 {
		got = math.Min(got, p.IP/ipPerUnit)
	}
	if crPerUnit > 0 && p.Credits > 0 {
		got = math.Min(got, float64(p.Credits)/crPerUnit)
	} else if crPerUnit > 0 {
		got = 0
	}
	if got <= 0 {
		return 0
	}
	p.IP = math.Max(p.IP-got*ipPerUnit, 0)
	p.Credits -= int(got * crPerUnit)
	return got
}

func (svc Service) secs() float64 {
	// The turnaround is as long as the work asked for.
	t := padBase + float64(svc.Rounds)/120 + float64(svc.Hull)/25
	return math.Min(t, padMax)
}

func (svc Service) line() string {
	var parts []string
	if svc.Rounds > 0 {
		parts = append(parts, fmt.Sprintf("%d rounds", svc.Rounds))
	}
	if svc.Hull > 0 {
		parts = append(parts, fmt.Sprintf("%d hull", svc.Hull))
	}
	if svc.Scrap > 0 {
		parts = append(parts, fmt.Sprintf("%.0ft scrap", svc.Scrap))
	}
	if len(parts) == 0 {
		parts = append(parts, "nothing to give")
	}
	out := strings.Join(parts, ", ")
	if svc.Refused != "" {
		out += " (no " + svc.Refused + ")"
	}
	return out
}

// --- the planet's own clock ---------------------------------------------

// Tick regenerates industrial capacity and books the day's tax revenue. A
// planet earns at a rate its population sets and spends at a rate the war
// sets; the gap between those two is how long it can hold a front.
func (p *Planet) Tick(dt float64) {
	if p.Team == TeamNone || p.Pop == 0 {
		return
	}
	p.IP = math.Min(p.IP+p.IPRate()*dt, p.IPMax())
	p.creditAcc += float64(p.Pop) / PopPerIP / DaySec * 900 * dt
	if whole := int(p.creditAcc); whole > 0 {
		p.Credits += whole
		p.creditAcc -= float64(whole)
	}
}

// IPRate is industrial points per battle second.
func (p *Planet) IPRate() float64 { return float64(p.Pop) / PopPerIP / DaySec }

// IPMax is two days of capacity in the buffer — a planet can bank a surge,
// but only a small one.
func (p *Planet) IPMax() float64 { return 2 * float64(p.Pop) / PopPerIP }

// Berths is how many ships can turn around at once.
func (p *Planet) Berths() int { return 1 + p.Pop/2000000 }

// Starving reports a planet that can no longer arm the ships it launches.
func (p *Planet) Starving() bool { return p.IP < roundIP*20 || p.Credits < RoundCr*20 }

// Label names a planet for the console.
func (p *Planet) Label() string {
	if p.Name != "" {
		return p.Name
	}
	return "planet"
}

// Setup gives a planet its economy. Population comes from the city that grows
// on it; everything else is derived, and the buffer starts full so the first
// sorties of a battle are properly armed.
func (p *Planet) Setup(pop int) {
	p.Pop = pop
	p.IP = p.IPMax()
	p.Credits = pop / 4
	if p.Stock == nil {
		p.Stock = make([]int, CommodityCount)
	}
}

// ClosestPlanet finds the nearest planet a team can land on, or nil. Passing
// TeamNone finds the nearest planet of any allegiance.
func (w *World) ClosestPlanet(from gmath.Vec2, team Team) *Planet {
	return w.closest(from, team, false)
}

// ClosestPort is where a pilot actually goes home to: the nearest world of
// its colour that can still arm it, falling back to the nearest of any
// condition when the whole holding is starving. Without the preference the
// squadron drains whichever outpost happens to be closest while the capital
// sits on a full buffer — and a fleet that lands where there is nothing to
// buy is a fleet that stops fighting for no reason.
func (w *World) ClosestPort(from gmath.Vec2, team Team) *Planet {
	if p := w.closest(from, team, true); p != nil {
		return p
	}
	return w.closest(from, team, false)
}

func (w *World) closest(from gmath.Vec2, team Team, supplied bool) *Planet {
	var best *Planet
	bestD := math.MaxFloat64
	for _, e := range w.Entities {
		p, ok := e.(*Planet)
		if !ok || (team != TeamNone && p.Team != team) {
			continue
		}
		if supplied && (p.Starving() || len(p.Pad) >= p.Berths()) {
			continue
		}
		if d := p.P.Sub(from).Len(); d < bestD {
			best, bestD = p, d
		}
	}
	return best
}
