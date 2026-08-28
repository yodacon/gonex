package reentry

import "math"

// autoland is the flight computer: a PD law on the corridor needle, a
// setpoint controller on heat flux, and a slow walk of crossrange to zero.
// It is a repairable, damageable system — the game's difficulty knob is
// diegetic. Damage above 30% flies with lag and noise; above 60% the lamp
// goes dark and the stick is yours.
type autoland struct {
	lastErr float64
	lagged  float64 // low-passed pitch command when degraded
}

func (a *autoland) fly(s *Sim, c Controls, dt float64) Controls {
	if s.Dmg.Computer >= 60 {
		// FAILED: the computer holds nothing; whatever the pilot pressed
		// this frame (minus Auto) is what flies.
		c.Auto = false
		return c
	}

	// PD on gamma error: positive error = shallow = push down.
	err := s.GammaError()
	dErr := (err - a.lastErr) / math.Max(dt, 1e-6)
	a.lastErr = err
	pitch := math.Min(math.Max(-(0.9*err+0.35*dErr), -1), 1)

	// heat setpoint: hold shielded q̇ at 80% of the TPS line with the feed.
	frac := s.Pt.QShielded / s.Veh.TPSLimit
	feed := math.Min(math.Max((frac-0.55)*2.2, 0), 1)
	if s.Li < s.Veh.LiTank*0.15 {
		feed *= 0.4 // preserve the reserve; accept the heat
	}

	// crossrange: roll toward the pad line, gently.
	roll := math.Min(math.Max(s.Crossrange*0.12, -0.6), 0.6)

	if s.Dmg.Computer > 30 {
		// DEGRADED: lag and tremor scale with damage.
		k := (s.Dmg.Computer - 30) / 30 // 0..1
		a.lagged += (pitch - a.lagged) * dt / (0.4 + 1.6*k)
		pitch = a.lagged + (s.rng.Float64()*2-1)*0.35*k
		roll *= 1 - 0.5*k
	}

	c.Pitch, c.Roll, c.Feed = pitch, roll, feed
	c.Boost, c.Burst = false, frac > 1.15 && s.Li > 5
	return c
}
