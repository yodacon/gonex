package world

import (
	"math"

	"yodacon.org/gonex/internal/gmath"
)

type ShipKind int

const (
	KindLocal ShipKind = iota // the human player
	KindNPC                   // AI-controlled
)

// Controller decides what a ship does each frame. The human player's
// controller (keyboard) lives in internal/app; AI behaviors in internal/ai.
type Controller interface {
	Name() string
	Control(s *Ship, w *World, dt float64)
}

const (
	maxHealth      = 100
	fireCooldown   = 0.2 // seconds between shots
	rechargePeriod = 3.0 // seconds per point of self-repair
)

type Ship struct {
	Body
	Heading float64 // degrees, 0 = up, clockwise
	ShipID  int     // catalog ID
	Team    Team
	Name    string
	Kind    ShipKind

	Health int
	Money  int
	Frags  int
	Deaths int
	Crew   int

	Autotarget  bool
	Target      *Ship
	Controller  Controller
	ThrustScale float64 // 0 means stock; the app's power presets set it

	fireCD     float64
	rechargeCD float64
}

// NewShip creates a ship with konex's player_Create defaults, centered on the
// map. Team 0 means "roll a random team".
func (w *World) NewShip(shipID int, team Team, name string, kind ShipKind) *Ship {
	if team == TeamNone {
		team = Team(1 + w.Rand.Intn(3))
	}
	s := &Ship{
		Body:    Body{P: gmath.V(w.MapW/2, w.MapH/2)},
		ShipID:  shipID,
		Team:    team,
		Name:    name,
		Kind:    kind,
		Health:  maxHealth,
		Money:   1,
		Crew:    1 + w.Rand.Intn(50),
		Heading: float64(w.Rand.Intn(360)),
	}
	w.Add(s)
	return s
}

func (s *Ship) Alive() bool { return true } // ships respawn, never despawn

// --- control surface (called by Controllers) ---

func (s *Ship) TurnLeft(w *World, dt float64) {
	s.Heading = gmath.WrapDeg(s.Heading - w.Catalog.Get(s.ShipID).TurnSpeed*dt)
}

func (s *Ship) TurnRight(w *World, dt float64) {
	s.Heading = gmath.WrapDeg(s.Heading + w.Catalog.Get(s.ShipID).TurnSpeed*dt)
}

func (s *Ship) Thrust(w *World, dt float64) {
	accel := w.Catalog.Get(s.ShipID).Acceleration
	if s.ThrustScale > 0 {
		accel *= s.ThrustScale // the engineering preset's engine share
	}
	s.V = s.V.Add(gmath.HeadingVec(s.Heading).Scale(accel * dt))
}

func (s *Ship) Reverse(w *World, dt float64) {
	accel := w.Catalog.Get(s.ShipID).Acceleration
	s.V = s.V.Sub(gmath.HeadingVec(s.Heading).Scale(accel * dt))
}

func (s *Ship) Slow(dt float64) {
	s.V = s.V.Scale(1 / (1 + dt))
}

func (s *Ship) Fire(w *World) bool {
	if s.fireCD > 0 {
		return false
	}
	if s == w.MainPlayer && w.FireGate != nil && !w.FireGate() {
		return false
	}
	s.fireCD = fireCooldown
	w.SpawnMissile(s)
	return true
}

// FaceToward turns the ship toward a point, snapping when a frame's turn would
// overshoot — ported from ai_FaceEntityAtPoint.
func (s *Ship) FaceToward(w *World, target gmath.Vec2, dt float64) {
	d := target.Sub(s.P)
	want := gmath.WrapDeg(math.Atan2(d.X, d.Y) * 180 / math.Pi)

	delta := math.Abs(want - s.Heading)
	if delta < w.Catalog.Get(s.ShipID).TurnSpeed*dt {
		s.Heading = want
		return
	}
	if want > s.Heading {
		if want-s.Heading < 180 {
			s.TurnRight(w, dt)
		} else {
			s.TurnLeft(w, dt)
		}
	} else {
		if s.Heading-want < 180 {
			s.TurnLeft(w, dt)
		} else {
			s.TurnRight(w, dt)
		}
	}
}

// --- simulation ---

func (s *Ship) Update(w *World, dt float64) {
	if s.Kind == KindNPC && s.Controller != nil {
		s.Controller.Control(s, w, dt)
	}

	// Velocity clamp.
	spec := w.Catalog.Get(s.ShipID)
	if v := s.V.Len(); v > spec.MaxVelocity {
		s.V = s.V.Norm().Scale(spec.MaxVelocity)
	}

	// Bounce off map edges.
	if s.P.X < 0 {
		s.P.X, s.V.X = 0, -s.V.X
	}
	if s.P.Y < 0 {
		s.P.Y, s.V.Y = 0, -s.V.Y
	}
	if s.P.X > w.MapW {
		s.P.X, s.V.X = w.MapW, -s.V.X
	}
	if s.P.Y > w.MapH {
		s.P.Y, s.V.Y = w.MapH, -s.V.Y
	}

	s.fireCD -= dt
	s.rechargeCD -= dt
	if s.rechargeCD < 0 {
		if s.Health < maxHealth {
			s.Health++
		}
		s.rechargeCD = rechargePeriod
	}

	if s.Kind == KindLocal && s.Autotarget && s.Target != nil {
		s.FaceToward(w, s.Target.P, dt)
	}
}

// HitByMissile applies damage and handles death, scoring and respawn.
func (s *Ship) HitByMissile(w *World, m *Missile) {
	if s == w.MainPlayer && w.GodMode {
		return
	}
	dmg := m.Damage
	if s == w.MainPlayer && w.ShieldFilter != nil {
		dmg = w.ShieldFilter(dmg)
	}
	s.Health -= dmg
	if s.Health > 0 {
		return
	}

	if owner := m.Owner; owner != nil {
		owner.Frags++
		if s.Team >= TeamRed && s.Team <= TeamBlue {
			// konex credited the killer's team; keep that rule.
			if m.Owner.Team >= TeamRed && m.Owner.Team <= TeamBlue {
				w.Scores[m.Owner.Team]++
			}
		}
		w.Notify("%s killed %s", owner.Name, s.Name)
	}
	s.die(w)
}

func (s *Ship) die(w *World) {
	w.SpawnExplosionFrom(s)
	w.MaybeDropItem(s)
	s.Deaths++

	if sp := w.SpawnPointFor(s.Team); sp != nil {
		s.P = sp.P
	} else {
		s.P = gmath.V(w.MapW/2, w.MapH/2)
	}
	s.V = gmath.Vec2{}
	s.Heading = float64(w.Rand.Intn(360))
	s.Target = nil
	s.Health = maxHealth
}
