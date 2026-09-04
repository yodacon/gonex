package universe

import (
	"fmt"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
)

// Policy: how a government spends, and who decides.
//
// Every colour has a policy. By default it is AUTO with a BALANCED focus:
// the exchequer spends by the colour's doctrine (buildings.go) at the
// governor's priority world or the building's natural home. A player who
// holds a seat in a colour can change that colour's policy from the desk:
// switch the auto-governor off and buy by hand, or elect a FOCUS that pulls
// one part of the economy to the front of the doctrine — the fleet, the
// lanes, industry, growth, defence — or pins everything on the priority
// world. The focus does not add money; it points the same money somewhere.

// Focus is what a government is spending toward.
type Focus int

const (
	// Balanced: the doctrine as written.
	Balanced Focus = iota
	// Fleet: yards and arsenals first, and pilots re-staked so more hulls
	// can afford to trade — "upgrades on the entire fleet".
	FocusFleet
	// Lanes: spaceports, and charters between held systems.
	FocusLanes
	// Industry: works and exchanges.
	FocusIndustry
	// Growth: habitats, and rations bought into hungry worlds.
	FocusGrowth
	// Defence: bastions, pickets and silos.
	FocusDefence
	// Priority: everything at the priority world, whatever it is.
	FocusPriority

	FocusCount
)

var focusNames = [FocusCount]string{
	Balanced: "balanced", FocusFleet: "fleet", FocusLanes: "lanes", FocusIndustry: "industry",
	FocusGrowth: "growth", FocusDefence: "defence", FocusPriority: "priority",
}

func (f Focus) String() string {
	if f < 0 || f >= FocusCount {
		return "?"
	}
	return focusNames[f]
}

// ParseFocus reads a focus for the console.
func ParseFocus(s string) (Focus, bool) {
	for f := Focus(0); f < FocusCount; f++ {
		if focusNames[f] == s {
			return f, true
		}
	}
	return 0, false
}

// Policy is one colour's spending rule.
type Policy struct {
	Auto  bool  // the government spends; off means only the governor buys
	Focus Focus // what it spends toward
}

func (p Policy) String() string {
	if !p.Auto {
		return "manual — the governor buys by hand"
	}
	return fmt.Sprintf("auto, %s", p.Focus)
}

// DefaultPolicy is what every colour starts with.
func DefaultPolicy() Policy { return Policy{Auto: true, Focus: Balanced} }

// SetPolicy changes a colour's policy.
func (u *Universe) SetPolicy(c govt.Color, p Policy) {
	if c == govt.None {
		return
	}
	u.Policies[c] = p
	u.Journal.Logf(u.Day, -1, "%s: policy is now %s", c, p)
}

// focusAxes is what each focus pulls to the front of the doctrine.
var focusAxes = map[Focus][]govt.Axis{
	FocusFleet:    {govt.Gunnery, govt.Logistics},
	FocusLanes:    {govt.Logistics},
	FocusIndustry: {govt.Extraction, govt.Industry},
	FocusGrowth:   {govt.Growth},
	FocusDefence:  {govt.Shields, govt.Gunnery},
}

// plan is the build order a colour actually uses this week: its doctrine
// with the focused axes moved to the front, in doctrine order within them.
func (u *Universe) plan(c govt.Color) []Building {
	d := Doctrine(c)
	axes := focusAxes[u.Policies[c].Focus]
	if len(axes) == 0 {
		return d
	}
	front := func(b Building) bool {
		for _, a := range axes {
			if buildingAxis[b] == a {
				return true
			}
		}
		return false
	}
	var out, rest []Building
	for _, b := range d {
		if front(b) {
			out = append(out, b)
		} else {
			rest = append(rest, b)
		}
	}
	return append(out, rest...)
}

// Plan is the week's build order, for the desk.
func (u *Universe) Plan(c govt.Color) []Building { return u.plan(c) }

// invest is the focus's spending that is not a building: re-staking broke
// pilots under FocusFleet, chartering lanes between held systems under
// FocusLanes, and buying rations into hungry worlds under FocusGrowth. Each
// is a Pay; none is a mint.
func (u *Universe) invest(c govt.Color) {
	exch := &u.Exchequer[c]
	switch u.Policies[c].Focus {
	case FocusFleet:
		// A broke pilot is a hull not trading. Stake the poorest first.
		for _, h := range u.Fleet.ByGovt(c) {
			if h.Purse < u.Tune.StartPurse/3 && *exch-u.Tune.ExchequerReserve >= u.Tune.StartPurse {
				econ.Pay(exch, &h.Purse, u.Tune.StartPurse-h.Purse)
				u.Journal.Logf(u.Day, h.ID, "%s re-staked by the exchequer", h.Name)
			}
		}
	case FocusLanes:
		// Charter the first uncharted pair of held systems that can afford it.
		worlds := u.worldsOf(c)
		for i := 0; i < len(worlds); i++ {
			for j := i + 1; j < len(worlds); j++ {
				a, b := worlds[i], worlds[j]
				if a.System == b.System || u.Chartered(a.System, b.System) {
					continue
				}
				if *exch-lanePerHop < u.Tune.ExchequerReserve {
					return
				}
				if err := u.Charter(a, b, exch, SeatAI); err == nil {
					return
				}
			}
		}
	case FocusGrowth:
		// Buy rations for the hungriest held world from a held world that
		// has them: a convoy order, the government's own standing train.
		var hungry, farm *World
		for _, w := range u.worldsOf(c) {
			if hungry == nil || w.fed < hungry.fed {
				hungry = w
			}
			if w.Makes(econ.Rations) && w.Warehouse[econ.Rations] > minLoad*2 &&
				(farm == nil || w.Warehouse[econ.Rations] > farm.Warehouse[econ.Rations]) {
				farm = w
			}
		}
		if hungry != nil && farm != nil && hungry != farm && hungry.fed < fedHold {
			_ = u.File(StandingOrder{From: farm.Stellar, To: hungry.Stellar, Owner: c, Mat: econ.Rations, Tons: 40})
		}
	}
}

// --- Tuning --------------------------------------------------------------

// Tuning is the set of balance knobs the simulation reads at run time, so a
// test, the console or a save can move one without a rebuild. Everything
// here has a default that is the number the plan was measured at; the seed
// is the other input, and the two together reproduce a universe exactly.
type Tuning struct {
	StartPurse       int     // a courier's opening stake
	TaxRate          float64 // share of a treasury's surplus remitted weekly
	ExchequerReserve int     // what a government keeps back for subsidies
	GovernEvery      int     // days between government decisions
	ExpandEvery      int     // days between looks for a world to take
	LeaveShare       float64 // share of the purse a crew on leave spends
}

// DefaultTuning is the measured baseline.
func DefaultTuning() Tuning {
	return Tuning{
		StartPurse:       15000,
		TaxRate:          0.10,
		ExchequerReserve: 20_000,
		GovernEvery:      7,
		ExpandEvery:      21,
		LeaveShare:       0.3,
	}
}

// Set changes one knob by name, for the console.
func (t *Tuning) Set(name string, v float64) error {
	switch name {
	case "purse":
		t.StartPurse = int(v)
	case "tax":
		t.TaxRate = v
	case "reserve":
		t.ExchequerReserve = int(v)
	case "govern":
		t.GovernEvery = max(1, int(v))
	case "expand":
		t.ExpandEvery = max(1, int(v))
	case "leave":
		t.LeaveShare = v
	default:
		return fmt.Errorf("no knob %q (purse, tax, reserve, govern, expand, leave)", name)
	}
	return nil
}

func (t Tuning) String() string {
	return fmt.Sprintf("purse %d · tax %.2f · reserve %d · govern every %dd · expand every %dd · leave %.2f",
		t.StartPurse, t.TaxRate, t.ExchequerReserve, t.GovernEvery, t.ExpandEvery, t.LeaveShare)
}
