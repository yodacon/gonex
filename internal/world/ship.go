package world

import (
	"math"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/power"
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

	// npcGridTick is how often an NPC's power plant is resolved. The player's
	// grid runs per frame under the engineering panel; thirty-five AI do not
	// need that resolution to brown out convincingly.
	npcGridTick = 0.25

	// engineIdleMW is the bus draw of a ship coasting; engineThrustMW is one
	// under power. Hotel load is always on.
	engineIdleMW   = 0.15
	engineThrustMW = 2.2
	hotelMW        = 0.4
)

// Role biases what a hull is for. It is a bias and never an exclusion: a
// warship's hold is small but never zero, because a fighter with a few tons
// spare is a fighter that will detour for salvage.
type Role int

const (
	RoleWarship Role = iota
	RoleHauler
)

func (r Role) String() string {
	if r == RoleHauler {
		return "hauler"
	}
	return "warship"
}

func (r Role) holdFactor() float64 {
	if r == RoleHauler {
		return 1.5
	}
	return 0.35
}

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
	Escort      bool // on the player's payroll; replaced on every launch
	Target      *Ship
	Controller  Controller
	ThrustScale float64 // 0 means stock; the app's power presets set it

	// The manifest. Energy comes back cheap at any friendly planet; rounds
	// are bought. That asymmetry is the whole economy — a ship can always
	// limp home, and can never fight its way out of an empty magazine.
	Role      Role
	Rounds    int         // bullets aboard
	RoundsMax int         // crew to serve the gun, tonnage to stow the shot
	Hold      []int       // tons per market commodity
	HoldMax   float64     // tons of deck
	Junk      float64     // tons of salvage aboard, sold on landing
	Grid      *power.Grid // generator plus batteries, scaled from the hull

	// Where the ship is in a turnaround, if it is in one.
	Pad   *Planet
	PadCD float64

	fireCD     float64
	rechargeCD float64
	gridCD     float64
	gridAcc    float64
	thrustHold float64 // seconds since the drive was last commanded
	served     float64 // last Flow.Served — below 1 the ship is browning out
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
		served:  1,
	}
	s.Outfit(w)
	w.Add(s)
	return s
}

// Outfit sizes the manifest from the hull and the crew, and fills it. Called
// once at creation and again whenever the ship's class or role changes, so
// the magazine always matches what is actually flying.
func (s *Ship) Outfit(w *World) {
	spec := w.Catalog.Get(s.ShipID)
	s.RoundsMax = 40 + 6*s.Crew + int(spec.Mass)/100
	s.HoldMax = spec.Mass / 50 * s.Role.holdFactor()
	s.Grid = power.For(spec.Mass)
	s.Rounds = s.RoundsMax
	if s.Hold == nil {
		s.Hold = make([]int, CommodityCount)
	}
}

// HoldTons is the tonnage on the deck, cargo and salvage together.
func (s *Ship) HoldTons() float64 {
	t := s.Junk
	for _, n := range s.Hold {
		t += float64(n)
	}
	return t
}

// HoldFree is the deck space left.
func (s *Ship) HoldFree() float64 { return math.Max(s.HoldMax-s.HoldTons(), 0) }

// Docked reports whether the ship is on a pad, out of the fight.
func (s *Ship) Docked() bool { return s.PadCD > 0 }

// RoundsFrac is the magazine as a fraction, for gauges and bingo checks.
func (s *Ship) RoundsFrac() float64 {
	if s.RoundsMax <= 0 {
		return 0
	}
	return float64(s.Rounds) / float64(s.RoundsMax)
}

// Vel is the ship's velocity. Motion is written only inside package world,
// so controllers read it through here.
func (s *Ship) Vel() gmath.Vec2 { return s.V }

// Served is the fraction of the bus the plant is carrying. Below 1 the ship
// is browning out, and it shows in the drive.
func (s *Ship) Served() float64 { return s.served }

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
	if s.Kind == KindNPC {
		accel *= s.served // a browned-out ship is visibly slow
	}
	s.thrustHold = 0.35
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
	if s.fireCD > 0 || s.Docked() {
		return false
	}
	if s.Rounds <= 0 {
		return false // dry. Go home.
	}
	if s == w.MainPlayer {
		if w.FireGate != nil && !w.FireGate() {
			return false
		}
	} else if s.Grid != nil && !s.Grid.TrySpendCap(power.ShotMJ) {
		return false // the caps could not push the shot out of the rail
	}
	s.Rounds--
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
	// A ship on a pad is out of the world: no controller, no drive, no gun.
	// It is buying its next sortie, and the cost is that it is not flying it.
	if s.Docked() {
		s.V = gmath.Vec2{}
		s.P = s.Pad.P
		if s.PadCD -= dt; s.PadCD <= 0 {
			s.Pad.Launch(w, s)
		}
		return
	}

	s.stepGrid(w, dt)
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

// stepGrid resolves an NPC's power plant on a coarse tick. The player's grid
// is the engineering panel's business and is stepped there, per frame.
func (s *Ship) stepGrid(w *World, dt float64) {
	if s.Grid == nil || s.Kind == KindLocal {
		return
	}
	s.thrustHold = math.Max(s.thrustHold-dt, 0)
	s.gridAcc += dt
	if s.gridCD -= dt; s.gridCD > 0 {
		return
	}
	engines := engineIdleMW
	if s.thrustHold > 0 {
		engines = engineThrustMW
	}
	// Screens draw is the capacitor refill: a ship that has been shooting
	// pulls harder, which is what starves the drive in a long engagement.
	screens := (1 - s.Grid.CapFrac()) * 2.0
	f := s.Grid.Step(s.gridAcc, power.Load{
		Engines: engines, Screens: screens, Hotel: hotelMW, Vacuum: true,
	})
	s.served, s.gridAcc, s.gridCD = f.Served, 0, npcGridTick
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

	// A replacement hull comes out of the yard with a starter magazine and a
	// cold battery, not a full load. Respawning is not resupply; the pad is.
	// Whatever the ship was carrying stayed with the wreck.
	s.Rounds = s.RoundsMax / 4
	s.Junk = 0
	for i := range s.Hold {
		s.Hold[i] = 0
	}
	if s.Grid != nil {
		s.Grid.BattMJ = s.Grid.BattCapMJ * 0.35
		s.Grid.CapMJ = s.Grid.CapCapMJ
		s.Grid.HeatMJ = 0
	}
}
