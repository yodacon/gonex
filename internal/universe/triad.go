package universe

import (
	"fmt"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
)

// The system triad.
//
// The recovered gazetteer has ONE STELLAR IN EACH of its hundred and nine
// systems, and that single fact is upstream of every balance failure this
// economy has measured. The same-type rule says an intermediate — copper,
// silicon, polymer, grain — moves within a system freely and crosses a jump
// only on a chartered lane. On a map where no two worlds share a system,
// "within a system" was never true for any pair of worlds in the game, so
// those materials could not move anywhere at all. Widening "local" to one
// jump helped and did not fix it: a world still has a neighbourhood only if
// the jump graph happens to give it one.
//
// So each system gets the three bodies a system needs, and they are given
// three DIFFERENT ECONOMIC ROLES rather than three sets of scenery:
//
//	PLANET   crust and people      — the mixed producer, as today
//	STATION  people, no crust      — a pure SINK, and a factory with no mine
//	FIELD    crust, no people      — a pure SOURCE, worked by machines
//
// Source, mixed and sink, sixty megametres apart instead of two hundred and
// forty. That is what the same-type rule was written for and has never once
// had.
//
// The field's crust is drawn from the INVERSE of its planet's barren mask
// (econ.Complement), so a system can close at least one chain locally while
// still being short of the finished goods that drive the long hauls.

// BodyKind is what sort of place a world is. Every rule that differs between
// the three keys off this, so there is one place to look.
type BodyKind int

const (
	// BodyPlanet is a world with rock under it and a city on it.
	BodyPlanet BodyKind = iota
	// BodyStation is an orbital: population, industry, and no ground at all.
	// It must import everything it processes, which is what makes it the
	// demand the couriers fly toward.
	BodyStation
	// BodyField is an asteroid field or an ice cavern: seams and nobody.
	// Nothing grows here and nothing is eaten here.
	BodyField
)

func (b BodyKind) String() string {
	switch b {
	case BodyStation:
		return "station"
	case BodyField:
		return "field"
	}
	return "planet"
}

// The ID space for minted bodies. The recovered gazetteer tops out in the
// low hundreds, the mission generator's random destinations live at 10000+,
// and galaxy.Charter mints into 20000–120000 — so these sit clear of all of
// them and stay derivable from the planet they belong to, which means a
// station's identity survives a reseed without a counter anywhere.
const (
	StationBase = 200000
	FieldBase   = 300000
)

// StationOf and FieldOf name the other two bodies of a planet's system.
func StationOf(planet int) int { return StationBase + planet }
func FieldOf(planet int) int   { return FieldBase + planet }

// HostOf resolves a minted body back to the planet it orbits, and passes an
// ordinary stellar through unchanged.
//
// Whoever charts the lanes needs this. A station and a field are not in the
// gazetteer, so a hop count looked up by their own ID finds nothing and
// charges them the unreachable rate — which put every minted body 2,220 Mm
// from everything, including the planet it is in orbit around. The route
// ranking divides margin by length, so the in-system trade the triad exists
// to create was ranked last on every board in the galaxy.
func HostOf(stellar int) int {
	switch {
	case stellar >= FieldBase:
		return stellar - FieldBase
	case stellar >= StationBase:
		return stellar - StationBase
	}
	return stellar
}

// stationShare is how much of its planet's population an orbital carries.
// Enough to be a real market; not enough to out-vote the planet for the
// capital, which is picked by population and must stay on the ground.
const stationShare = 4

// Triad expands a one-world-per-system port list into the three bodies each
// system needs. Ports that are already minted bodies are passed through, so
// calling it twice is harmless.
func Triad(ports []Port) []Port {
	// Idempotent by construction: a planet that already has its station in
	// the list is not expanded again. Guarding only on "is this a minted
	// body" was not enough — the PLANETS are below StationBase, so a second
	// call re-expanded every one of them and minted a duplicate orbital and
	// rock for each.
	seen := make(map[int]bool, len(ports))
	for _, p := range ports {
		seen[p.Stellar] = true
	}
	out := make([]Port, 0, len(ports)*3)
	for _, p := range ports {
		if p.Stellar >= StationBase || seen[StationOf(p.Stellar)] {
			out = append(out, p)
			continue
		}
		out = append(out, p)
		out = append(out, Port{
			Stellar: StationOf(p.Stellar), Name: p.Name + " Station",
			System: p.System, Pop: p.Pop / stationShare, Govt: p.Govt,
			Kind: BodyStation, Host: p.Stellar,
		})
		out = append(out, Port{
			Stellar: FieldOf(p.Stellar), Name: fieldName(p.Stellar, p.Name),
			System: p.System, Pop: 0, Govt: govt.None,
			Kind: BodyField, Host: p.Stellar,
		})
	}
	return out
}

// fieldName alternates between the two kinds of source the brief asks for,
// so the map reads as a mix of rock and ice rather than a hundred and nine
// identical asteroid belts. They behave identically; only the name differs,
// until there is art to tell them apart.
func fieldName(stellar int, host string) string {
	if stellar%2 == 1 {
		return fmt.Sprintf("%s Nebula", host)
	}
	return fmt.Sprintf("%s Belt", host)
}

// --- What the three kinds do differently ---------------------------------

// Worked reports whether this world lifts its own crust. A planet is worked
// by the people on it; a FIELD has nobody, and is worked the same way a hot
// world is — by machines on a schedule, for whoever is paying. Without this
// a field is a rock with enormous seams and no way to get anything out of
// them, which is precisely the trap the hostile worlds were in before
// autoCrew existed.
func (w *World) Worked() bool {
	return w.Govt != govt.None || w.Hostile() || w.Kind == BodyField
}

// fieldCrew is the machine workforce on a body with no population at all,
// in millions-of-citizens equivalent. A field is a pit head, a rail line
// nobody rides and a mass driver.
const fieldCrew = 2.4

// stationLines are the chains an orbital may be founded running. Every one
// of them is a FINISHING step whose inputs all arrive by ship — which is
// what a factory with no mine can actually do, and what makes a station the
// destination the trade network has always been short of.
var stationLines = []string{"Electronics", "Powercell", "Pharmaceutical", "Shipyard", "Ordnance"}

// stationLine picks an orbital's industry deterministically from its host,
// so a given map always has the same factories in the same places.
func stationLine(host int) string {
	return stationLines[uint(host*2654435761>>7)%uint(len(stationLines))]
}

// Describe adds the body kind to a world's one-line briefing.
func (w *World) KindLabel() string {
	if w.Kind == BodyPlanet {
		return ""
	}
	return " [" + w.Kind.String() + "]"
}

// TriadReport counts what the map was founded with, for the rig.
func (u *Universe) TriadReport() (planets, stations, fields int) {
	for _, id := range u.order {
		switch u.Worlds[id].Kind {
		case BodyStation:
			stations++
		case BodyField:
			fields++
		default:
			planets++
		}
	}
	return
}

// LocalReach is how many other bodies this world can shuttle to without a
// charter — the neighbourhood the same-type rule depends on. It is the one
// number that says whether the triad worked.
func (u *Universe) LocalReach(w *World) int {
	n := 0
	for _, id := range u.order {
		o := u.Worlds[id]
		if o != w && o.System == w.System {
			n++
		}
	}
	return n
}

var _ = econ.Ferrite
