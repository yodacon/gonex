package reentry

import "math"

// Confinement: how the plasma pillow is actually held up, and what a yard
// can sell you to hold it better.
//
// The envelope the Yodacon flies behind is a MAGNETOPAUSE. The coil's dipole
// field pushes outward with magnetic pressure B²/2mu0; the oncoming flow
// pushes inward with ram pressure rho·V²; and the stand-off radius is where
// the two balance. Because the dipole falls off as r^-3 its pressure falls
// as r^-6, which is why the model solves
//
//	r_mp / R_n  =  (p_mag / p_flow) ^ (1/6)
//
// and why the pillow is so stubborn: to stand a shock off twice as far you
// need sixty-four times the magnetic pressure, or eight times the field.
// Nobody is buying their way out of that exponent. What they CAN buy is a
// fix for one of the four distinct ways the balance goes wrong, and each of
// the four is a different piece of hardware because it is a different piece
// of physics.
//
//	1  THE FIELD IS TOO WEAK.        A bigger, colder coil. Stand-off goes
//	                                 as B^(1/3) and nothing changes that,
//	                                 so this is the honest, expensive,
//	                                 diminishing-returns answer.
//
//	2  THE BOTTLE LEAKS AT THE POLES. A bare dipole is not a closed bottle.
//	                                 It has two cusps on the axis where the
//	                                 field lines run INTO the vehicle, and
//	                                 flow funnels straight down them onto
//	                                 the nose. A ring of higher-order
//	                                 multipole coils plugs them, so more of
//	                                 the magnetic pressure you are paying
//	                                 for actually stands the flow off, and
//	                                 less mass leaks through to load the
//	                                 wake.
//
//	3  THE PILLOW IS SYMMETRIC.      A centred dipole gives you a shield and
//	                                 no steering. Driving current
//	                                 asymmetrically from a phased antenna
//	                                 ring pushes the magnetopause off-axis:
//	                                 the pillow leans, the pressure
//	                                 distribution goes with it, and the
//	                                 vehicle has lift it did not have. This
//	                                 is the outfit that turns the shield
//	                                 into a control surface — and it is
//	                                 bought with power, because the array
//	                                 is driving current against a resistive
//	                                 plasma.
//
//	4  THE AIR IS TOO THIN TO GRIP.  MHD needs a conducting continuum. High
//	                                 up, the mean free path is long, the
//	                                 Knudsen number is large, and there is
//	                                 nothing for the field to push against
//	                                 however hard it pushes — the model's
//	                                 phi term goes to zero and the whole
//	                                 envelope collapses to the bare nose.
//	                                 An injection ring answers this the
//	                                 only way it can be answered: put the
//	                                 mass there yourself. A puff of heavy
//	                                 seed ahead of the nose raises the local
//	                                 density, collapses the local Knudsen
//	                                 number, and buys a continuum to grip.
//	                                 It is INERTIAL confinement — the pillow
//	                                 is held by the momentum of matter you
//	                                 brought with you — and it is the one
//	                                 rung that works where the magnetic
//	                                 rungs do not.
//
// Every one of the four is a multiplier on a term that was already in
// stateAt. Nothing here is a new force; it is four ways of being better at
// the one the model already has.

// Confinement is the shield hardware fitted beyond the stock coil.
type Confinement struct {
	// CoilBoost is extra dipole field at the nose, in tesla, added to the
	// vehicle's CoilField. Stand-off goes as the cube root of it.
	CoilBoost float64
	// CuspSeal is 0..1, how well the polar cusps are plugged. It raises the
	// share of magnetic pressure that does useful work and cuts the wake
	// leakage that shows up as drag.
	CuspSeal float64
	// ArrayGain is 0..1, the phased antenna ring's authority. It drives
	// current in the shock layer, which raises the interaction parameter
	// (so the shield works lower down the conductivity curve) and leans the
	// envelope (so the airframe commands more lift).
	ArrayGain float64
	// RingFeed is kg/s of heavy seed the injection ring can put ahead of the
	// nose. It is the inertial rung: it buys continuum where there is none.
	RingFeed float64
	// CryoMargin is 0..1 of extra refrigeration headroom, which shows up as
	// a smaller cryogenic load on the power ledger for the same field.
	CryoMargin float64
}

// The coefficients. Each is the strength of one upgrade's hook into the one
// term it modifies, and they are spread so that no single outfit is the
// answer to every corridor.
const (
	// cuspGain is the extra usable magnetic pressure at a perfectly sealed
	// cusp. A factor of two on PRESSURE is only 2^(1/6) = 12% on stand-off,
	// which is exactly the point: sealing the bottle is worth much more in
	// what it stops leaking than in what it stands off.
	cuspGain = 1.00
	// cuspLeak is how much of the wake-loading drag penalty a sealed cusp
	// removes. This is where the multipole ring earns its price.
	cuspLeak = 0.65
	// arrayQ is the driven-current contribution to the interaction
	// parameter. A full array roughly doubles it, which moves the gate
	// open several kilometres higher than the passive coil manages.
	arrayQ = 1.15
	// arrayLD is the commandable lift the leaning envelope adds, as a
	// fraction of the airframe's own LDMax.
	arrayLD = 0.45
	// arrayWatts is what driving the array costs, per unit gain, at full
	// interaction. The steering is not free and the ledger says so.
	arrayWatts = 5.5e5
	// ringDensity is how much the injection ring collapses the local
	// Knudsen number per kg/s of seed. Tuned so that a full ring pulls the
	// continuum gate open roughly ten kilometres higher.
	ringDensity = 46.0
	// ringWatts is the accelerator's draw per kg/s of seed.
	ringWatts = 1.8e6
	// cryoRelief is how much of the refrigeration load a full uprate takes
	// off the ledger.
	cryoRelief = 0.70
)

// Field is the total dipole field at the nose.
func (c Confinement) Field(base float64) float64 { return base + c.CoilBoost }

// PressureGain is the multiplier on magnetic pressure from sealing the
// cusps: the share of B²/2mu0 that is actually standing the flow off rather
// than escorting it onto the nose.
func (c Confinement) PressureGain() float64 { return 1 + cuspGain*clamp01(c.CuspSeal) }

// LeakRelief is the fraction of the envelope's drag penalty a sealed bottle
// removes.
func (c Confinement) LeakRelief() float64 { return 1 - cuspLeak*clamp01(c.CuspSeal) }

// DrivenQ is the multiplier the phased array puts on the magnetic
// interaction parameter. Driving current is how you get MHD authority out of
// a plasma too cold and too thin to give it to you for free.
func (c Confinement) DrivenQ() float64 { return 1 + arrayQ*clamp01(c.ArrayGain) }

// LiftGain is the extra commandable L/D from leaning the envelope, as a
// fraction of the airframe's own maximum.
func (c Confinement) LiftGain() float64 { return arrayLD * clamp01(c.ArrayGain) }

// KnudsenRelief divides the local Knudsen number: the injection ring's seed
// raises the density the field has to grip, which is the only thing that
// helps in the rarefied phase.
func (c Confinement) KnudsenRelief() float64 { return 1 + ringDensity*math.Max(c.RingFeed, 0) }

// CryoLoad is the multiplier on refrigeration draw.
func (c Confinement) CryoLoad() float64 { return 1 - cryoRelief*clamp01(c.CryoMargin) }

// Watts is what the confinement hardware asks of the grid on top of the
// stock shield: the array driving current, and the ring's accelerator.
func (c Confinement) Watts(gate float64) float64 {
	return arrayWatts*clamp01(c.ArrayGain)*clamp01(gate) + ringWatts*math.Max(c.RingFeed, 0)
}

// Fitted reports whether anything at all is installed, so the gauges can
// stay quiet on a stock hull.
func (c Confinement) Fitted() bool {
	return c.CoilBoost > 0 || c.CuspSeal > 0 || c.ArrayGain > 0 || c.RingFeed > 0 || c.CryoMargin > 0
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
