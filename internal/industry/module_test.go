package industry

import (
	"math"
	"testing"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
)

const eps = 1e-9

// available is every ton a chain can legitimately emit: what it buys from
// outside, plus what its own extractors lift out of the crust. A chain
// containing a mine is entitled to emit mass it never "bought", because that
// mass is debited from a finite reserve by whoever runs the chain.
func available(m *Module) float64 {
	total := m.Demand().Total()
	for _, p := range m.Parts {
		if p.Source() {
			total += p.Supply().Total()
		}
	}
	return total
}

// The rule the whole factory rests on: a module never emits more than it
// eats. Break this and the economy prints matter at every tick, and no
// amount of care further up can put it back.
func TestNoPrimitiveCreatesMass(t *testing.T) {
	for k := Kind(0); k < KindCount; k++ {
		for _, c := range govt.Colors() {
			m := Build(k, 10, c)
			in, out := m.Demand().Total(), m.Supply().Total()
			if k.Extractor() {
				// An extractor's product is debited from the world's reserve
				// by the caller, so it legitimately has no input here.
				if out <= 0 {
					t.Errorf("%s (%s) extracts nothing", k, c)
				}
				continue
			}
			if out > in+eps {
				t.Errorf("%s (%s): %.4ft in, %.4ft out — creates %.4ft",
					k, c, in, out, out-in)
			}
			if math.Abs((in-out)-m.Slag) > 1e-6 {
				t.Errorf("%s (%s): slag %.4f, but in-out is %.4f", k, c, m.Slag, in-out)
			}
		}
	}
}

// A recipe that tries to create mass is a typo, and must degrade rather than
// explode three layers inside a world tick.
func TestAnImpossibleRecipeIsScaledBackNotHonoured(t *testing.T) {
	m := New("perpetual motion",
		[]Port{{econ.Ferrite, 1}},
		[]Port{{econ.Steel, 5}})
	if got := m.Supply().Total(); got > 1+eps {
		t.Errorf("output %.3ft from 1t of input", got)
	}
}

// Composition is closed: plugging two modules together yields a module, and
// that module plugs into a third exactly the same way. That is what makes a
// supermodule an ordinary object rather than a special case.
func TestCompositionIsClosedAndConserves(t *testing.T) {
	mine := Build(MineSilicate, 20, govt.Blue)
	furnace := Build(Furnace, 20, govt.Blue)
	fab := Build(Fab, 20, govt.Blue)

	pair := mine.Then(furnace)
	if out, avail := pair.Supply().Total(), available(pair); out > avail+eps {
		t.Errorf("the pair emits %.4ft against %.4ft available", out, avail)
	}

	// The pair is itself a module; plug it into a third.
	whole := pair.Then(fab)
	if len(whole.Parts) < 3 {
		t.Errorf("composite remembers %d parts, want at least 3", len(whole.Parts))
	}
	// Compose() over the same line must agree with the hand-folded chain.
	folded := Compose("electronics", mine, furnace, fab)
	if math.Abs(folded.Supply().Total()-whole.Supply().Total()) > 1e-6 {
		t.Errorf("Compose gives %.6ft, Then-folding gives %.6ft",
			folded.Supply().Total(), whole.Supply().Total())
	}
}

// The interesting property of plugging things together: the chain runs at its
// narrowest stage. A fabricator behind a furnace that cannot feed it does not
// run at full rate, and the composite has to say so.
func TestTheChainRunsAtItsBottleneck(t *testing.T) {
	// A big mine and furnace behind a tiny fab: the fab is the limit.
	big := Compose("wide", Build(MineSilicate, 100, govt.None), Build(Furnace, 100, govt.None))
	small := Build(Fab, 4, govt.None)
	chain := big.Then(small)

	// Output cannot exceed what the small stage could ever make.
	if got, ceiling := chain.Supply()[econ.Chips], small.Supply()[econ.Chips]; got > ceiling+eps {
		t.Errorf("chips %.4f exceeds the small fab's ceiling %.4f", got, ceiling)
	}

	// And the reverse: a starved downstream stage throttles.
	tiny := Compose("narrow", Build(MineSilicate, 1, govt.None), Build(Furnace, 1, govt.None))
	hungry := Build(Fab, 100, govt.None)
	starved := tiny.Then(hungry)
	if got := starved.Supply()[econ.Chips]; got >= hungry.Supply()[econ.Chips] {
		t.Errorf("a fab fed 1t/day of silicate still makes %.3ft of chips", got)
	}
	mat, ratio := starved.Bottleneck()
	if ratio >= 1 {
		t.Errorf("no bottleneck reported on a visibly starved chain (%s at %.2f)", mat, ratio)
	}
}

// A composite must not report a material as both an input and an output —
// that is the netting failing, and it would let a world "buy" what it is
// simultaneously selling itself.
func TestInternalFlowsDoNotEscape(t *testing.T) {
	for _, ch := range Chains {
		m := ch.Assemble(30, govt.Green)
		in, out := m.Demand(), m.Supply()
		for mat := econ.Material(0); mat < econ.Count; mat++ {
			if in[mat] > eps && out[mat] > eps {
				t.Errorf("%s lists %s as both input (%.3f) and output (%.3f)",
					ch.Name, mat, in[mat], out[mat])
			}
		}
	}
}

// Every catalogued chain must actually make the good it advertises, and must
// conserve while doing it.
func TestEveryChainMakesItsGoodAndConserves(t *testing.T) {
	for _, ch := range Chains {
		for _, c := range govt.Colors() {
			m := ch.Assemble(50, c)
			if got := m.Supply()[ch.Good]; got <= 0 {
				t.Errorf("%s (%s) makes no %s", ch.Name, c, ch.Good)
			}
			// Everything the line emits, plus its slag, must be covered by
			// what it eats plus what it dug out of the ground.
			dug := 0.0
			for _, k := range ch.Steps {
				if k.Extractor() {
					dug += Build(k, 50, c).Supply().Total()
				}
			}
			if in, out := m.Demand().Total()+dug, m.Supply().Total(); out > in+1e-6 {
				t.Errorf("%s (%s): emits %.4ft against %.4ft available", ch.Name, c, out, in)
			}
		}
	}
}

// Blue's industrial edge has to show up as more product from the same intake
// — and Red's deficit as less. If the trifecta's Industry axis does not move
// this number, the axis is decorative.
func TestIndustryAxisChangesWhatAFactoryYields(t *testing.T) {
	blue := Build(Fab, 10, govt.Blue).Supply()[econ.Chips]
	red := Build(Fab, 10, govt.Red).Supply()[econ.Chips]
	if blue <= red {
		t.Errorf("Blue fab yields %.4f, Red %.4f — Blue should lead industry", blue, red)
	}
	// And the intake is unchanged: a worse factory wastes more, it does not
	// eat less.
	if a, b := Build(Fab, 10, govt.Blue).Demand().Total(), Build(Fab, 10, govt.Red).Demand().Total(); math.Abs(a-b) > eps {
		t.Errorf("intake differs by government: %.4f vs %.4f", a, b)
	}
}

// Rank is what gives a world its speciality, so it must actually respond to
// what is in the ground rather than returning the catalogue order.
func TestRankFollowsTheCrust(t *testing.T) {
	var silicaWorld econ.Stock
	silicaWorld.Add(econ.Silicate, 900000)
	silicaWorld.Add(econ.Biomass, 1000)
	if got := Rank(silicaWorld); len(got) == 0 || got[0].Good != econ.Chips {
		name := "nothing"
		if len(got) > 0 {
			name = got[0].Name
		}
		t.Errorf("a silicate world's best industry is %s, want Electronics", name)
	}

	var farmWorld econ.Stock
	farmWorld.Add(econ.Biomass, 900000)
	if got := Rank(farmWorld); len(got) == 0 || got[0].Needs()[0] != econ.Biomass {
		t.Error("a biomass world did not rank a biomass industry first")
	}

	// A barren world runs nothing at all, and must say so rather than
	// standing up a factory with no feedstock.
	if got := Rank(econ.Stock{}); len(got) != 0 {
		t.Errorf("a barren world ranked %d viable chains", len(got))
	}
}
