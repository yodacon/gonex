package universe

import (
	"math"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// The battle: Konquest's roll loop, spending rounds and leaving scrap.
//
// KDE Konquest resolves a fleet's arrival at a hostile planet in a dozen
// lines (game.cpp, doFleetArrival): roll for each side; if the defender's
// roll beats the planet's kill percentage an attacker dies, if the
// attacker's roll beats the kill percentage of the planet it LEFT a defender
// dies; loop until one side has nobody. If both percentages are zero, a coin
// decides each round. The garrison of a fallen planet is the fleet that took
// it. It is fast, fair, and one journal line can say what happened.
//
// It is kept here verbatim for engagements the player is not watching, with
// two changes that make it zero-sum:
//
//  1. EVERY ROLL SPENDS A ROUND. A kill roll draws tons of Rounds from the
//     shooter — the world's warehouse for the defence, the flight's magazine
//     for the attack — into the sink. When a side's rounds are gone its kill
//     percentage is zero for the rest of the fight. A planet out of bullets
//     falls to whoever arrives.
//  2. A KILLED HULL BECOMES SCRAP over the planet, not nothing. Its dry tons
//     and its cargo move into a debris field in that orbit, and whoever holds
//     the orbit afterwards has a breaker's yard worth of steel waiting.
//     Winning a battle is a mining operation.

// Relation is how two colours stand.
type Relation int

const (
	// War: fight on arrival, no trade. The default between colours.
	War Relation = iota
	// Peace: no fight, no trade.
	Peace
	// Ally: trade, double destination weight, no tariff.
	Ally
)

func (r Relation) String() string {
	switch r {
	case Peace:
		return "peace"
	case Ally:
		return "allied"
	}
	return "war"
}

// ParseRelation reads a relation for the console.
func ParseRelation(s string) (Relation, bool) {
	switch s {
	case "war":
		return War, true
	case "peace":
		return Peace, true
	case "ally", "allied", "alliance":
		return Ally, true
	}
	return 0, false
}

func colourPair(a, b govt.Color) [2]govt.Color {
	if a > b {
		a, b = b, a
	}
	return [2]govt.Color{a, b}
}

// Relation reports how colour a stands to colour b. A colour is allied to
// itself; everybody is at peace with the unaligned, who trade with anyone
// and are attacked only by a flight sent to take them.
func (u *Universe) Relation(a, b govt.Color) Relation {
	if a == b {
		return Ally
	}
	if a == govt.None || b == govt.None {
		return Peace
	}
	if r, ok := u.Relations[colourPair(a, b)]; ok {
		return r
	}
	return War
}

// SetRelation changes how two colours stand. Declaring war closes every
// lane between them: hulls under way to a port that will no longer have
// them turn for the nearest friendly one, cargo aboard, unpaid.
func (u *Universe) SetRelation(a, b govt.Color, r Relation) {
	if a == b || a == govt.None || b == govt.None {
		return
	}
	u.Relations[colourPair(a, b)] = r
	u.Journal.Logf(u.Day, -1, "%s and %s: %s", a, b, r)
	if r == Ally {
		return
	}
	for _, h := range u.Fleet.Hulls {
		if !h.Status.UnderWay() || h.Mission == traffic.Flight {
			continue
		}
		dst := u.Worlds[h.To]
		if dst == nil || u.canTrade(h.Govt, dst.Govt) {
			continue
		}
		home := u.Capital(h.Govt)
		if home == nil {
			continue
		}
		u.Journal.Logf(u.Day, h.ID, "%s turns back from %s — the lane is closed", h.Name, dst.Name)
		u.Fleet.Depart(h, h.To, home.Stellar, traffic.Returning, u.Day)
	}
}

// hostile reports whether a flight of colour c arriving at a world held by
// p fights for it. Neutral worlds are taken, not fought over with anybody
// in particular; a colour at peace is left alone.
func (u *Universe) hostile(c, p govt.Color) bool {
	if p == c {
		return false
	}
	if p == govt.None {
		return true
	}
	return u.Relation(c, p) == War
}

// Outcome is what a battle came to, for the journal and the desk.
type Outcome struct {
	Attacker, Defender govt.Color
	AttackersLost      int
	DefendersLost      int
	RoundsSpent        float64
	Fell               bool
}

// Engage fights a flight's arrival at a world. It is called with the hulls
// that arrived together this tick; the garrison is whatever is berthed.
func (u *Universe) Engage(flight []*traffic.Hull, at *World) Outcome {
	if len(flight) == 0 || at == nil {
		return Outcome{}
	}
	out := Outcome{Attacker: flight[0].Govt, Defender: at.Govt}
	defenders := u.Garrison(at)
	pickets := float64(at.Built[Picket])

	attackers := append([]*traffic.Hull(nil), flight...)

	for rolls := 0; rolls < maxRolls; rolls++ {
		defK := u.Rating(at)
		atkK := u.flightRating(attackers, pickets)

		dRoll, aRoll := u.Rng.Float64(), u.Rng.Float64()

		if defK == 0 && atkK == 0 {
			// Konquest's special case: nobody can shoot, so a coin decides
			// who loses a ship this round.
			if aRoll < dRoll {
				if len(defenders) > 0 {
					defenders = u.strike(defenders, at, &out.DefendersLost)
				}
			} else {
				attackers = u.strike(attackers, at, &out.AttackersLost)
			}
		} else {
			if dRoll < defK {
				out.RoundsSpent += u.spendRounds(&at.Warehouse, roundTons)
				attackers = u.strike(attackers, at, &out.AttackersLost)
			}
			if len(attackers) == 0 {
				break
			}
			if aRoll < atkK {
				out.RoundsSpent += u.spendMagazine(attackers, roundTons)
				if len(defenders) > 0 {
					defenders = u.strike(defenders, at, &out.DefendersLost)
				}
			}
		}
		if len(attackers) == 0 {
			break
		}
		if len(defenders) == 0 {
			out.Fell = true
			break
		}
	}

	if out.Fell {
		u.conquer(at, attackers)
		u.Journal.Logf(u.Day, -1, "%s has fallen to %s (%d attackers and %d defenders lost, %.1ft of rounds spent)",
			at.Name, out.Attacker, out.AttackersLost, out.DefendersLost, out.RoundsSpent)
	} else {
		u.Journal.Logf(u.Day, -1, "%s has held against %s (%d attackers and %d defenders lost, %.1ft of rounds spent)",
			at.Name, out.Attacker, out.AttackersLost, out.DefendersLost, out.RoundsSpent)
		// The survivors turn for the nearest world of their own colour.
		if home := u.Capital(out.Attacker); home != nil {
			for _, h := range attackers {
				h.Mission = traffic.Courier
				u.Fleet.Depart(h, at.Stellar, home.Stellar, traffic.Returning, u.Day)
			}
		}
	}
	// The wreck field over the planet is somebody's now: whoever is berthed
	// here with room. The victor's garrison, in practice.
	for _, d := range u.Fleet.Debris {
		if d.InOrbit() && d.At == at.Stellar {
			u.Fleet.Salvage(d, u.Day)
		}
	}
	return out
}

const (
	maxRolls  = 20000
	roundTons = 0.05 // tons of Rounds one kill roll fires
	// flightMagazine is how much shot per hull gives a flight its full kill
	// percentage; below it the rating scales down, and at zero it is zero.
	flightMagazine = 1.0
	missileBonus   = 0.15
	picketStep     = 0.05
)

// flightRating is the attacker's kill percentage: the magazine it loaded at
// the world it left, with a bonus for missiles the pickets cannot stop.
func (u *Universe) flightRating(flight []*traffic.Hull, pickets float64) float64 {
	if len(flight) == 0 {
		return 0
	}
	var mag, missiles float64
	for _, h := range flight {
		mag += h.Magazine()
		missiles += h.Cargo[econ.Missiles]
	}
	if mag <= 0 {
		return 0
	}
	r := baseRating * govt.GunFactor(flight[0].Govt) *
		math.Min(1, mag/(flightMagazine*float64(len(flight))))
	if missiles > 0 {
		r += math.Max(0, missileBonus-picketStep*pickets)
	}
	return math.Min(r, maxRating)
}

// spendRounds fires a roll's worth of shot from a warehouse into the sink.
func (u *Universe) spendRounds(from *econ.Stock, tons float64) float64 {
	return econ.Consume(from, &u.Sink, econ.Rounds, tons)
}

// spendMagazine fires a roll's worth from the flight, missiles first.
func (u *Universe) spendMagazine(flight []*traffic.Hull, tons float64) float64 {
	left := tons
	for _, h := range flight {
		if h.Cargo[econ.Missiles] > 0 {
			left -= econ.Consume(&h.Cargo, &u.Sink, econ.Missiles, math.Min(left, h.Cargo[econ.Missiles]))
		}
		if left <= 0 {
			break
		}
		left -= econ.Consume(&h.Cargo, &u.Sink, econ.Rounds, math.Min(left, h.Cargo[econ.Rounds]))
		h.Mass = h.Wet()
		if left <= 0 {
			break
		}
	}
	return tons - left
}

// strike kills the first hull of a side: it becomes a wreck over the world.
func (u *Universe) strike(side []*traffic.Hull, at *World, lost *int) []*traffic.Hull {
	if len(side) == 0 {
		return side
	}
	h := side[0]
	h.Home = at.Stellar
	h.Status = traffic.Idle // the wreck is in this orbit
	u.wreck(h, "destroyed over "+at.Name)
	*lost++
	return side[1:]
}

// conquer hands a world to the flight that took it. The garrison IS the
// flight; the standing orders die with the old government; the treasury is
// captured, because credits are conserved and somebody has to hold them;
// the plants re-stand under the new colour's yield.
func (u *Universe) conquer(w *World, flight []*traffic.Hull) {
	c := flight[0].Govt
	w.Govt = c
	w.Orders = nil
	w.Seat = SeatAI
	w.Tariff = colourTariff
	w.standUpIndustry()
	w.Reprice()
	for _, h := range flight {
		h.Mission = traffic.Courier
		h.Home, h.From, h.To, h.Status = w.Stellar, w.Stellar, w.Stellar, traffic.Idle
		h.V, h.S = 0, 0
	}
	if u.OnConquer != nil {
		u.OnConquer(w)
	}
}

// wreck strikes a hull off: cargo and structure become a debris field where
// it died, on the books, and the collection rule runs over it at once — the
// nearest hold with room takes what it can, then the next.
func (u *Universe) wreck(h *traffic.Hull, why string) {
	if h.Status == traffic.Lost {
		return
	}
	var pool econ.Stock
	pool[econ.Scrap] = h.Dry
	for m := econ.Material(0); m < econ.Count; m++ {
		if h.Cargo[m] > 0 {
			econ.Transfer(&h.Cargo, &pool, m, h.Cargo[m])
		}
	}
	d := u.Fleet.Drop(h, &pool, u.Day)
	h.Status = traffic.Lost
	h.Mission = traffic.Courier
	h.Mass = 0
	u.Journal.LostHulls++
	u.Journal.Logf(u.Day, h.ID, "%s lost — %s; %.0ft of wreckage adrift", h.Name, why, d.Stock.Total())
	u.Fleet.Salvage(d, u.Day)
}

// replaceHulls is Konquest's production: a colour that has lost hulls and
// has Hull tons in a yard's warehouse commissions replacements from them.
// The census row comes back — the count is fixed — at the yard, with its
// dry tonnage taken out of the warehouse ton for ton. A colour with no yard,
// or a yard with no chips delivered, does not replace what it loses.
func (u *Universe) replaceHulls() {
	for _, h := range u.Fleet.Hulls {
		if h.Status != traffic.Lost {
			continue
		}
		for _, id := range u.order {
			w := u.Worlds[id]
			if w.Govt != h.Govt || w.Warehouse[econ.Hull] < h.Dry {
				continue
			}
			w.Warehouse.Take(econ.Hull, h.Dry) // the pool the auditor reads it back in is Structure()
			h.Status = traffic.Idle
			h.Home, h.From, h.To = w.Stellar, w.Stellar, w.Stellar
			h.Cargo = econ.Stock{}
			h.V, h.S = 0, 0
			h.Mass = h.Wet()
			econ.Pay(&w.Credits, &h.Purse, u.Tune.StartPurse/2)
			u.Journal.Logf(u.Day, h.ID, "%s commissioned at %s — %.0ft of hull out of the yard", h.Name, w.Name, h.Dry)
			break
		}
	}
}

// Strike marks a hull lost whose wreck somebody else is already keeping —
// the sector the player is flying, where the debris is an entity in the sky
// and its tons are counted there. The census row goes to Lost; nothing is
// dropped here, because it was dropped there.
func (u *Universe) Strike(h *traffic.Hull, why string) {
	if h.Status == traffic.Lost {
		return
	}
	h.Status = traffic.Lost
	h.Mission = traffic.Courier
	h.Cargo = econ.Stock{}
	h.Mass = 0
	u.Journal.LostHulls++
	u.Journal.Logf(u.Day, h.ID, "%s lost — %s", h.Name, why)
}

// DropInOrbit puts a pool of wreckage in orbit over a port, merging into a
// field already there. It is how a sector's debris comes back to the census
// when nobody is drawing that sky any more.
func (u *Universe) DropInOrbit(stellar int, pool *econ.Stock) *traffic.Debris {
	var d *traffic.Debris
	for _, o := range u.Fleet.Debris {
		if o.InOrbit() && o.At == stellar {
			d = o
			break
		}
	}
	if d == nil {
		d = &traffic.Debris{At: stellar, S: -1, Day: u.Day}
		u.Fleet.Debris = append(u.Fleet.Debris, d)
	}
	for m := econ.Material(0); m < econ.Count; m++ {
		if pool[m] > 0 {
			econ.Transfer(pool, &d.Stock, m, pool[m])
		}
	}
	return d
}

// LiftOrbit takes every ton adrift over a port out of the census, for a
// sector that is about to draw it as wreckage in the sky.
func (u *Universe) LiftOrbit(stellar int) econ.Stock {
	var out econ.Stock
	live := u.Fleet.Debris[:0]
	for _, d := range u.Fleet.Debris {
		if d.InOrbit() && d.At == stellar {
			out = out.Plus(d.Stock)
			continue
		}
		live = append(live, d)
	}
	u.Fleet.Debris = live
	return out
}
