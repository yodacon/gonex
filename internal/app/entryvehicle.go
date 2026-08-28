package app

import (
	"math"

	"yodacon.org/gonex/internal/reentry"
	"yodacon.org/gonex/internal/ship"
)

// entryVehicleFor maps a catalog ship's arcade spec onto the envelope
// model's vehicle knobs, anchored so the Yodacon (CollisionRadius 40,
// TurnSpeed 220) lands exactly on reentry.Yodacon(). Size comes from the
// collision radius, agility from the turn rate — the only axes the konex
// specs actually vary — so every hull in the yard flies a different
// corridor and reads different ranges on its own dials.
func entryVehicleFor(sh *ship.Ship) reentry.Vehicle {
	base := reentry.Yodacon()
	size := sh.CollisionRadius / 40 // 1.0 = the Yodacon
	if size <= 0 {
		size = 1
	}
	agility := sh.TurnSpeed / 220
	if agility <= 0 {
		agility = 1
	}
	v := base
	v.Mass = base.Mass * math.Pow(size, 1.6)
	v.RefMass = v.Mass
	v.Diameter = base.Diameter * math.Pow(size, 0.8)
	v.NoseRadius = v.Diameter * 0.3
	v.LDMax = base.LDMax * math.Sqrt(agility)
	v.GlideLD = base.GlideLD * agility
	// a light hull tolerates more g; a heavy one less — clamped so nothing
	// in the yard is unflyable
	v.GLimit = math.Min(math.Max(base.GLimit/math.Pow(size, 0.4), 4), 10)
	v.CoilField = base.CoilField * math.Pow(size, 0.3)
	v.PowerCap = base.PowerCap * math.Pow(size, 1.2)
	v.LiTank = base.LiTank * math.Pow(size, 1.2)
	v.RCSTank = base.RCSTank * math.Pow(size, 1.2)
	return v
}
