// Package fx holds the small screen-effect simulators the cockpit scenes
// share. The centerpiece is Fire: the classic intensity-propagation fire
// (the PSX-Doom grid), decoupled from the screen. The grid burns in its own
// (u, v) space — u along an ignition front, v marching outward through the
// flame — and the caller maps that space onto any curve it likes: a bow
// shock standing off a hull, a Mach cone, a straight wall of flame. The
// same simulation that runs linearly up a screen bends like the flow field
// once its rows are laid along an arc.
package fx

import "math/rand"

// tick is the fixed simulation rate. The classic effect is tuned per-tick,
// not per-second; a fixed accumulator keeps it frame-rate independent.
const tick = 1.0 / 30

// Fire is one burning grid. Rows*Cols float cells, row 0 the ignition
// front. Each tick every cell re-averages the cells one row nearer the
// front (left, center, right, and two rows down for reach — the Doom
// kernel), sheds heat by Cooling, and flickers by a random factor; the
// front row itself re-rolls from Fuel through the FuelProfile. Sweep skews
// which neighbors are sampled, so the whole flame leans sideways the way a
// flow field drags it.
type Fire struct {
	Cols, Rows int
	Cooling    float64 // per-row heat retention, ~0.96 tight … 0.995 lazy
	Sweep      float64 // -1..1: sideways advection along the front
	Fuel       float64 // 0..1: ignition-front intensity

	// FuelProfile shapes the front: intensity multiplier per u in -1..1.
	// nil burns evenly.
	FuelProfile func(u float64) float64

	cells []float64
	boost float64
	acc   float64
	rng   *rand.Rand
}

// NewFire builds a cold grid. Seed it deterministically if the caller
// cares about replayable flames.
func NewFire(cols, rows int, seed int64) *Fire {
	return &Fire{
		Cols: cols, Rows: rows, Cooling: 0.97, Fuel: 1,
		cells: make([]float64, cols*rows),
		rng:   rand.New(rand.NewSource(seed)),
	}
}

// Boost dumps a burst of extra fuel on the front — a pellet burst, a coil
// overdrive — that decays over the next second or so.
func (f *Fire) Boost(amount float64) { f.boost += amount }

// Cell reads the intensity at column i, row j (0 = the front), 0..1.
func (f *Fire) Cell(i, j int) float64 {
	if i < 0 || j < 0 || i >= f.Cols || j >= f.Rows {
		return 0
	}
	return f.cells[j*f.Cols+i]
}

// U maps a column index to the front coordinate -1..1.
func (f *Fire) U(i int) float64 {
	return 2*float64(i)/float64(f.Cols-1) - 1
}

// Step advances the flame by dt seconds at the fixed internal tick.
func (f *Fire) Step(dt float64) {
	for f.acc += dt; f.acc >= tick; f.acc -= tick {
		f.stepTick()
	}
}

func (f *Fire) stepTick() {
	c, rows := f.Cols, f.Rows
	// the front row: fresh fuel, shaped and flickering
	fuel := f.Fuel + f.boost
	if fuel > 1.6 {
		fuel = 1.6
	}
	for i := 0; i < c; i++ {
		p := 1.0
		if f.FuelProfile != nil {
			p = f.FuelProfile(f.U(i))
		}
		f.cells[i] = fuel * p * (0.55 + 0.45*f.rng.Float64())
	}
	// the body: every row re-averages the row beneath, skewed by Sweep.
	// Deeper rows sweep harder — the tip of a flame lags the base exactly
	// like a particle further down a streamline.
	for j := 1; j < rows; j++ {
		base := (j - 1) * c
		base2 := base - c // two rows toward the front, for kernel reach
		if base2 < 0 {
			base2 = base
		}
		depth := float64(j) / float64(rows-1)
		skew := f.Sweep * (0.6 + 1.8*depth)
		si := int(skew)
		frac := skew - float64(si)
		row := j * c
		for i := 0; i < c; i++ {
			s := i - si // sample window shifted against the sweep
			l, m, r := clampi(s-1, c), clampi(s, c), clampi(s+1, c)
			sum := f.cells[base+l] + f.cells[base+m] + f.cells[base+r] +
				f.cells[base2+m]
			// the fractional part of the skew blends one more neighbor,
			// so the lean is continuous rather than stair-stepped
			if frac > 0 {
				sum += frac * f.cells[base+clampi(s-2, c)]
			} else if frac < 0 {
				sum -= frac * f.cells[base+clampi(s+2, c)]
			}
			v := sum / (4 + abs(frac)) * f.Cooling * (0.72 + 0.28*f.rng.Float64())
			if v < 0.004 {
				v = 0
			}
			f.cells[row+i] = v
		}
	}
	f.boost *= 0.86
}

func clampi(i, n int) int {
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
