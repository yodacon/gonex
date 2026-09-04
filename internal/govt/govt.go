// Package govt is the trifecta: three colours at war, each better at
// something, each worse at something, and provably no better overall than
// either of the others.
//
// The balance is not a matter of taste here, it is arithmetic. Every colour
// is scored on the same six axes, and the table is DOUBLY BALANCED:
//
//   - every ROW sums to 6.00 — no colour was handed more total ability
//     than another, so there is no strongest side;
//   - every COLUMN sums to 3.00 — no axis was globally inflated, so
//     "shields" means the same amount of game across the whole table, and
//     a buff to one colour is exactly a nerf to the others.
//
// Both are asserted in the tests, so the trifecta cannot drift out of true
// by somebody nudging one number in a balance pass. Change a value and two
// other values must move to pay for it, which is the whole point.
//
// The design each colour expresses:
//
//	RED    the raider    — best guns, best logistics, worst industry.
//	                       Wins early, cannot sustain a long war.
//	GREEN  the grower    — best growth, best extraction, worst guns.
//	                       Out-produces and out-mines; must not be rushed.
//	BLUE   the fortress  — best shields, best industry, worst growth.
//	                       Slow to start, terrible to grind down.
//
// Which is a rock-paper-scissors in tempo rather than in damage type: Red
// beats Green by arriving before the growth compounds, Green beats Blue by
// out-massing a slow starter, Blue beats Red by outlasting a side that runs
// out of factory.
package govt

import "fmt"

// Color is a government. The values match world.Team deliberately — Team is
// the battlefield's word for the same three powers — but this package does
// not import world, so the simulation layers can use the constants without
// dragging the renderer's dependency graph behind them.
type Color int

const (
	None Color = iota
	Red
	Green
	Blue
)

// Colors lists the three powers, in order, for iteration.
func Colors() []Color { return []Color{Red, Green, Blue} }

func (c Color) String() string {
	switch c {
	case Red:
		return "Red"
	case Green:
		return "Green"
	case Blue:
		return "Blue"
	}
	return "Neutral"
}

// Name is the polity's name rather than its colour, for prose.
func (c Color) Name() string {
	switch c {
	case Red:
		return "ConEx"
	case Green:
		return "Exeon"
	case Blue:
		return "Cenron"
	}
	return "Unaligned"
}

// Axis is one dimension of national character.
type Axis int

const (
	// Growth: how fast a homeworld's population compounds. Population is
	// industrial capacity, so this is the economy's tempo.
	Growth Axis = iota
	// Extraction: tons of crust a mine lifts per industrial day.
	Extraction
	// Industry: how much of a factory's input survives as product rather
	// than falling out as slag.
	Industry
	// Shields: the fraction of a hit a screen absorbs.
	Shields
	// Gunnery: damage dealt per round.
	Gunnery
	// Logistics: hold size, transit speed, and how small a flight can be and
	// still be worth sending.
	Logistics

	AxisCount
)

func (a Axis) String() string {
	switch a {
	case Growth:
		return "growth"
	case Extraction:
		return "extraction"
	case Industry:
		return "industry"
	case Shields:
		return "shields"
	case Gunnery:
		return "gunnery"
	case Logistics:
		return "logistics"
	}
	return "?"
}

// RowTotal is what every colour's six axes must sum to.
const RowTotal = 6.00

// ColTotal is what every axis must sum to across the three colours.
const ColTotal = 3.00

// table is the trifecta itself. Read a row for a colour's character; read a
// column to see that no axis favours the table as a whole.
//
//	         growth  extract  industry  shields  gunnery  logistics   Σ
//	Red        0.95     0.90      0.90     0.90     1.20      1.15   6.00
//	Green      1.20     1.15      0.90     0.95     0.85      0.95   6.00
//	Blue       0.85     0.95      1.20     1.15     0.95      0.90   6.00
//	  Σ        3.00     3.00      3.00     3.00     3.00      3.00
var table = map[Color][AxisCount]float64{
	Red:   {Growth: 0.95, Extraction: 0.90, Industry: 0.90, Shields: 0.90, Gunnery: 1.20, Logistics: 1.15},
	Green: {Growth: 1.20, Extraction: 1.15, Industry: 0.90, Shields: 0.95, Gunnery: 0.85, Logistics: 0.95},
	Blue:  {Growth: 0.85, Extraction: 0.95, Industry: 1.20, Shields: 1.15, Gunnery: 0.95, Logistics: 0.90},
}

// Trait is a colour's multiplier on one axis. A neutral power sits at 1.0 on
// everything, which is what makes unaligned worlds the honest baseline the
// three are measured against.
func Trait(c Color, a Axis) float64 {
	row, ok := table[c]
	if !ok || a < 0 || a >= AxisCount {
		return 1.0
	}
	return row[a]
}

// Row is a colour's whole character, for printing and for the balance proof.
func Row(c Color) [AxisCount]float64 {
	if row, ok := table[c]; ok {
		return row
	}
	var neutral [AxisCount]float64
	for i := range neutral {
		neutral[i] = 1.0
	}
	return neutral
}

// --- Derived knobs -------------------------------------------------------
//
// Everything below is a consequence of the table, never an independent
// number. That is what stops the trifecta being balanced on paper and
// lopsided in play: there is no second place to hide a buff.

// MinFleet is how much company a pilot waits for before setting out. Better
// logistics means a smaller group can be supplied well enough to be worth
// sending, so the raider sorties in threes while the fortress masses fives.
//
//	Red 3, Green 4, Blue 5.
func MinFleet(c Color) int {
	switch {
	case Trait(c, Logistics) >= 1.10:
		return 3
	case Trait(c, Logistics) >= 0.94:
		return 4
	}
	return 5
}

// GrowthPerDay is the fraction a held world's population compounds by each
// industrial day, before any local modifier.
func GrowthPerDay(c Color) float64 { return baseGrowth * Trait(c, Growth) }

// MineRate is tons of crust lifted per industrial day per million citizens.
func MineRate(c Color) float64 { return baseMine * Trait(c, Extraction) }

// Yield is the fraction of a factory's input mass that survives as product;
// the remainder falls out as slag. Never 1.0 for anybody — industry always
// costs something, and that cost is where the conserved mass goes.
func Yield(c Color) float64 {
	y := baseYield * Trait(c, Industry)
	if y > 0.98 {
		y = 0.98
	}
	return y
}

// ShieldFrac is the share of an incoming hit a screen eats.
func ShieldFrac(c Color) float64 {
	f := baseShield * Trait(c, Shields)
	if f > 0.85 {
		f = 0.85
	}
	return f
}

// GunFactor scales damage dealt per round.
func GunFactor(c Color) float64 { return Trait(c, Gunnery) }

// HoldFactor scales a hull's cargo capacity.
func HoldFactor(c Color) float64 { return Trait(c, Logistics) }

// CruiseFactor scales how fast a hull crosses a lane between systems.
func CruiseFactor(c Color) float64 { return 0.5 + 0.5*Trait(c, Logistics) }

const (
	baseGrowth = 0.0125 // 1.25% a day at neutral: a homeworld doubles in ~56
	baseMine   = 42.0   // tons per industrial day per million citizens
	baseYield  = 0.78   // three quarters of what goes in comes out as product
	baseShield = 0.35   // a neutral screen eats a third of a hit
)

// Describe renders a colour's character as a line of prose, for the console.
func Describe(c Color) string {
	return fmt.Sprintf("%s (%s) — grow x%.2f, mine x%.2f, make x%.2f, "+
		"screen x%.2f, gun x%.2f, haul x%.2f · flights of %d",
		c, c.Name(),
		Trait(c, Growth), Trait(c, Extraction), Trait(c, Industry),
		Trait(c, Shields), Trait(c, Gunnery), Trait(c, Logistics),
		MinFleet(c))
}

// Parse reads a colour by name or polity, case-insensitively.
func Parse(s string) (Color, bool) {
	switch s {
	case "red", "Red", "RED", "ConEx", "conex":
		return Red, true
	case "green", "Green", "GREEN", "Exeon", "exeon":
		return Green, true
	case "blue", "Blue", "BLUE", "Cenron", "cenron":
		return Blue, true
	case "neutral", "Neutral", "none", "None":
		return None, true
	}
	return None, false
}
