// Package industry is the factory block: small modules with material ports,
// and a rule for plugging them into each other that yields another module.
//
// A module is a box with inputs and outputs, both measured in tons per
// industrial day. A smelter eats ferrite and hands back steel. A fabricator
// eats silicon and copper and hands back chips. Neither knows the other
// exists.
//
// The one operation that matters is THEN: plug a module's outputs into the
// next module's inputs and get back a single module whose ports are whatever
// the pair could not satisfy internally. Because the result is itself a
// module, it plugs into a third the same way, and a fourth — composition is
// closed, so a supermodule is not a special kind of thing with its own rules.
// It is a module that remembers what it was built from.
//
// Two properties are enforced everywhere and tested:
//
//  1. MASS BALANCES. A module's outputs never weigh more than its inputs.
//     The difference is slag, reported explicitly, so a chain can be audited
//     against econ.Books like everything else.
//
//  2. THE CHAIN RUNS AT ITS BOTTLENECK. If a smelter makes 10 t/day of steel
//     and the mill downstream wants 15, the composite runs at two thirds
//     rate and says so. This is what makes plugging things together a design
//     decision rather than a formality: a world's industry is only as good
//     as its narrowest stage, and finding that stage is the game.
package industry

import (
	"fmt"
	"sort"
	"strings"

	"yodacon.org/gonex/internal/econ"
)

// Port is a material flow in tons per industrial day.
type Port struct {
	Mat  econ.Material
	Tons float64
}

// Module is one stage of production, or a whole composed chain of them.
type Module struct {
	Name string
	In   []Port
	Out  []Port

	// Slag is the mass this module sheds per day: everything that went in
	// and did not come out as product. It is carried explicitly rather than
	// inferred, so the auditor has a number to move into the sink.
	Slag float64

	// Parts is what this was composed from, innermost first. Empty for a
	// primitive. It exists so a supermodule can explain itself.
	Parts []*Module

	// Choke is the material that throttled this chain when it was composed,
	// and Ratio how hard (1.0 = nothing held it back). They are recorded at
	// composition time because that is the ONLY moment the shortfall is
	// visible: once the downstream stage has been scaled to fit, its ports
	// balance perfectly and the evidence is gone.
	Choke econ.Material
	Ratio float64
}

// Source reports whether this module takes nothing and yields something: an
// extractor, whose feedstock is the world's crust rather than another
// module's output. A source is the one place mass legitimately enters the
// factory graph, and the caller is required to debit an equal tonnage from a
// finite reserve — which is what keeps the universe zero-sum even though
// this box appears to make matter out of nothing.
func (m *Module) Source() bool { return len(m.In) == 0 && len(m.Out) > 0 }

// New builds a primitive module and balances its books. The caller states
// inputs and outputs; whatever the outputs do not account for becomes slag.
// Outputs heavier than inputs are a recipe bug and are scaled back to the
// input mass rather than silently minting matter.
func New(name string, in, out []Port) *Module {
	m := &Module{Name: name, In: clone(in), Out: clone(out), Ratio: 1, Choke: econ.Slag}
	inMass, outMass := mass(m.In), mass(m.Out)
	if m.Source() {
		// An extractor: no inputs by design, and no slag. Its output is
		// matched ton for ton against a crust reserve by whoever runs it.
		return m
	}
	if outMass > inMass && outMass > 0 {
		// A recipe that creates mass is always a typo. Scale it to fit
		// rather than refusing, so a bad table degrades instead of panicking
		// three layers inside a world tick.
		m.Out = scale(m.Out, inMass/outMass)
		outMass = mass(m.Out)
	}
	m.Slag = inMass - outMass
	return m
}

// Demand is what the module wants per day, by material.
func (m *Module) Demand() econ.Stock { return vector(m.In) }

// Supply is what the module produces per day, by material.
func (m *Module) Supply() econ.Stock { return vector(m.Out) }

// Rate is the module's throughput: tons of input consumed per day. It is the
// natural "how big is this factory" number.
func (m *Module) Rate() float64 { return mass(m.In) }

// Scaled returns the same module running at f times the throughput. Scaling
// is how a world sizes a plant to its population without inventing a
// separate capacity concept.
func (m *Module) Scaled(f float64) *Module {
	if f < 0 {
		f = 0
	}
	out := &Module{
		Name:  m.Name,
		In:    scale(m.In, f),
		Out:   scale(m.Out, f),
		Slag:  m.Slag * f,
		Choke: m.Choke,
		Ratio: m.Ratio,
	}
	for _, p := range m.Parts {
		out.Parts = append(out.Parts, p.Scaled(f))
	}
	return out
}

// Then plugs m's outputs into next's inputs and returns the composite.
//
// The composite runs at the bottleneck. For every material next wants that m
// makes, the ratio of supply to demand is computed; the smallest such ratio
// below 1 throttles the WHOLE downstream stage, because a fabricator starved
// of copper does not run at full rate on silicon alone — it runs slow and
// wastes the silicon it cannot pair. Materials next wants that m does not
// make at all are not a bottleneck; they are an external input the world is
// expected to buy, and they surface on the composite's In list.
func (m *Module) Then(next *Module) *Module {
	supply := m.Supply()
	throttle, choke := 1.0, econ.Slag
	for _, p := range next.In {
		if p.Tons <= 0 {
			continue
		}
		if have := supply[p.Mat]; have > 0 {
			// Only materials m actually produces can throttle the stage.
			if r := have / p.Tons; r < throttle {
				throttle, choke = r, p.Mat
			}
		}
	}
	down := next.Scaled(throttle)

	// Net the internal flows: what m makes and down eats never leaves the
	// building, and must not appear on either external list.
	leftover := m.Supply()
	var extIn []Port
	for _, p := range down.In {
		used := p.Tons
		if have := leftover[p.Mat]; have > 0 {
			if have < used {
				used = have
			}
			leftover[p.Mat] -= used
		} else {
			used = 0
		}
		if rem := p.Tons - used; rem > 1e-12 {
			extIn = append(extIn, Port{Mat: p.Mat, Tons: rem})
		}
	}

	// The composite's inputs are m's own, plus whatever down still needs
	// from outside. Its outputs are down's product plus anything m made that
	// down did not want.
	in := merge(append(clone(m.In), extIn...))
	out := merge(append(clone(down.Out), fromVector(leftover)...))

	comp := &Module{
		Name:  m.Name + " → " + next.Name,
		In:    in,
		Out:   out,
		Parts: append(append([]*Module{}, flatten(m)...), flatten(down)...),
		Choke: choke,
		Ratio: throttle,
	}
	// A chain is as throttled as its worst stage, not its last one, so an
	// upstream pinch survives being composed with something downstream.
	if m.Ratio > 0 && m.Ratio < comp.Ratio {
		comp.Choke, comp.Ratio = m.Choke, m.Ratio
	}
	// Slag is whatever the composite eats and does not emit. Deriving it
	// from the external ports rather than summing the stages' own figures
	// means it stays correct through every throttle and every netting.
	comp.Slag = mass(comp.In) - mass(comp.Out)
	if comp.Slag < 0 {
		comp.Slag = 0
	}
	return comp
}

// Compose is Then folded over a whole line, left to right. This is the
// "plug modules into each other" operation the factory screen offers.
func Compose(name string, mods ...*Module) *Module {
	if len(mods) == 0 {
		return New(name, nil, nil)
	}
	acc := mods[0]
	for _, m := range mods[1:] {
		acc = acc.Then(m)
	}
	out := &Module{Name: name, In: acc.In, Out: acc.Out, Slag: acc.Slag,
		Parts: acc.Parts, Choke: acc.Choke, Ratio: acc.Ratio}
	if name == "" {
		out.Name = acc.Name
	}
	return out
}

// Bottleneck names the stage that is holding the chain back, and by how
// much: the material most starved relative to what the line wants of it.
// It returns Slag and 0 when nothing is short, since slag is never an input.
func (m *Module) Bottleneck() (econ.Material, float64) {
	if m.Ratio <= 0 || m.Ratio >= 1 {
		return econ.Slag, 1.0
	}
	return m.Choke, m.Ratio
}

// Describe renders the module as one readable line.
func (m *Module) Describe() string {
	var b strings.Builder
	b.WriteString(m.Name)
	b.WriteString(": ")
	b.WriteString(portList(m.In))
	b.WriteString(" ⇒ ")
	b.WriteString(portList(m.Out))
	if m.Slag > 0.01 {
		fmt.Fprintf(&b, " (+%.1ft slag)", m.Slag)
	}
	return b.String()
}

// --- helpers -------------------------------------------------------------

func clone(p []Port) []Port {
	if len(p) == 0 {
		return nil
	}
	return append([]Port(nil), p...)
}

func mass(p []Port) float64 {
	var t float64
	for _, x := range p {
		t += x.Tons
	}
	return t
}

func scale(p []Port, f float64) []Port {
	if len(p) == 0 {
		return nil
	}
	out := make([]Port, len(p))
	for i, x := range p {
		out[i] = Port{Mat: x.Mat, Tons: x.Tons * f}
	}
	return out
}

func vector(p []Port) econ.Stock {
	var s econ.Stock
	for _, x := range p {
		s[x.Mat] += x.Tons
	}
	return s
}

func fromVector(s econ.Stock) []Port {
	var out []Port
	for m := econ.Material(0); m < econ.Count; m++ {
		if s[m] > 1e-12 {
			out = append(out, Port{Mat: m, Tons: s[m]})
		}
	}
	return out
}

// merge folds duplicate materials together and sorts, so two chains that
// describe the same industry compare equal and print the same.
func merge(p []Port) []Port {
	out := fromVector(vector(p))
	sort.Slice(out, func(i, j int) bool { return out[i].Mat < out[j].Mat })
	return out
}

// flatten returns a module's primitive stages, or itself if it is one.
func flatten(m *Module) []*Module {
	if len(m.Parts) == 0 {
		return []*Module{m}
	}
	return m.Parts
}

func portList(p []Port) string {
	if len(p) == 0 {
		return "nothing"
	}
	parts := make([]string, 0, len(p))
	for _, x := range p {
		parts = append(parts, fmt.Sprintf("%.1ft %s", x.Tons, x.Mat))
	}
	return strings.Join(parts, " + ")
}
