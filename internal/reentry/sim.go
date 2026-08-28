package reentry

import (
	"math"
	"math/rand"
)

// Vehicle is the airframe flying the corridor. The Yodacon's numbers come
// from the recovered shïp 174 record (350 t, 80 m) mapped onto the envelope
// model's vehicle knobs.
type Vehicle struct {
	Mass       float64 // kg
	Diameter   float64 // m
	NoseRadius float64 // m
	LDMax      float64 // maximum commandable lift-to-drag
	TPSLimit   float64 // W/m2 before damage accrues
	GLimit     float64 // structural deceleration limit
	CoilField  float64 // B0 at the nose, T
	LiTank     float64 // kg of lithium aboard at interface
	PowerCap   float64 // W the ship can supply to the shield
}

// Yodacon is the ship the whole game flies. Diameter and nose radius are
// the *inflated envelope*, not the 80 m hull hiding behind it — without the
// pillow the Yodacon's ballistic coefficient is a falling anvil's; the
// plasma aeroshell is what makes a 350 t freighter flyable at all.
func Yodacon() Vehicle {
	return Vehicle{
		Mass: 350e3, Diameter: 40, NoseRadius: 12, LDMax: 0.35,
		TPSLimit: 60e4, GLimit: 6, CoilField: 1.2, LiTank: 60, PowerCap: 4e6,
	}
}

// Profile is a destination's landing difficulty, exported from the
// gazetteer into galaxy.json.
type Profile struct {
	AtmosScale        float64 `json:"atmosScale"`
	GravityScale      float64 `json:"gravityScale"`
	CorridorHalfWidth float64 `json:"corridorHalfWidthDeg"`
	PadBonus          int     `json:"padBonus"`
}

// EarthProfile is the checkride: dense air, narrow corridor.
func EarthProfile() Profile {
	return Profile{AtmosScale: 1, GravityScale: 1, CorridorHalfWidth: 0.22, PadBonus: 40000}
}

// Controls is what the pilot (or the flight computer) holds this frame.
type Controls struct {
	Pitch float64 // -1..1 → commanded L/D fraction (fly the needle)
	Roll  float64 // -1..1 → envelope rotation, walks crossrange
	Feed  float64 // 0..1 → lithium feed fraction of max (200 g/s)
	Boost bool    // coil overdrive (limited charges)
	Burst bool    // emergency pellet dump
	Auto  bool    // hand the frame to the flight computer
}

const maxFeed = 0.200 // kg/s at Feed=1

// Status is where the entry stands.
type Status int

const (
	Flying Status = iota
	Landed
	SkippedOut
	Destroyed
)

// Damage is what the descent has cost so far, in percent.
type Damage struct {
	Hull     float64 // 0-100; 100 = ship lost
	Computer float64 // flight computer; >30 degraded, >60 failed
	Clamps   float64 // cargo clamps; >50 spoils mission cargo
}

// Sim is one flown entry. Step it at a fixed dt with the frame's Controls.
type Sim struct {
	Veh  Vehicle
	Prof Profile

	// trajectory state
	V, Gamma, H, Downrange, T float64
	Crossrange                float64 // km off the pad line, + right

	// resources
	Li         float64 // kg remaining
	BoostLeft  int
	boostTimer float64
	burstTimer float64

	// Supply is the fraction of the shield's power demand the ship's grid
	// met this frame, 0..1. The app's engineering layer sets it from the
	// battery each step; standalone (tests, tools) it stays at 1. A starved
	// coil keeps some authority — the field does not vanish, the array does.
	Supply float64

	Dmg    Damage
	status Status
	skipT  float64 // seconds spent flat/climbing while hypersonic and high

	// last evaluated point, for gauges
	FeedUsed float64 // kg/s actually injected this frame
	Pt      Point
	RefG    float64 // reference gamma this frame, deg
	Width   float64 // corridor half-width this frame, deg
	PadDist float64 // km of downrange remaining to the pad

	auto autoland
	rng  *rand.Rand
}

// New starts an entry at interface: 122 km, slightly super-circular — a
// freight deorbit carries excess energy, so early in the entry the
// atmosphere is trying to throw the ship back out and the pilot must hold
// it down; the balance flips once drag bleeds the speed off.
func New(veh Vehicle, prof Profile, seed int64) *Sim {
	s := &Sim{
		Veh: veh, Prof: prof,
		H: 122000, V: 8350 * math.Sqrt(prof.GravityScale),
		Li: veh.LiTank, BoostLeft: 2, Supply: 1,
		Crossrange: 8, // you never arrive lined up
		rng:        rand.New(rand.NewSource(seed)),
	}
	s.Gamma = s.RefGamma(s.H) * math.Pi / 180
	s.PadDist = 1600
	s.Pt = stateAt(s.H, s.V, veh, prof, veh.CoilField, 0)
	s.RefG = s.RefGamma(s.H)
	s.Width = s.CorridorWidth(s.H)
	return s
}

// RefGamma is the corridor centerline: the flight-path angle (deg) that
// threads the wedge at altitude h.
// Three regimes: steep at interface to commit a super-circular arrival,
// then flatten to −1.7° through the deceleration pulse (the g-load cap:
// n ≈ V²·sinγ/2eH says 6 g at 8 km/s wants under two degrees), then
// steepen toward the deck once the speed is spent.
func (s *Sim) RefGamma(h float64) float64 {
	switch {
	case h >= 70000:
		f := math.Min((h-70000)/52000, 1)
		return -1.7 - 1.1*f // -2.8 at interface → -1.7 at 70 km
	case h >= 45000:
		return -1.7
	default:
		d := (45000 - math.Max(h, 0)) / 45000
		return -1.7 - 4.3*d*d
	}
}

// CorridorWidth is the half-width (deg) of the safe band, narrowing as the
// entry deepens. The profile sets how forgiving the world is.
func (s *Sim) CorridorWidth(h float64) float64 {
	f := math.Min(math.Max(h/122000, 0), 1)
	return s.Prof.CorridorHalfWidth * (0.45 + 0.55*f) * 2.2
}

// GammaError is the needle: degrees above (+, shallow) or below (-, steep)
// the reference.
func (s *Sim) GammaError() float64 { return s.Gamma*180/math.Pi - s.RefG }

func (s *Sim) Status() Status { return s.status }

// Step advances the entry by dt seconds under the given controls.
func (s *Sim) Step(dt float64, c Controls) {
	if s.status != Flying {
		return
	}
	if c.Auto {
		c = s.auto.fly(s, c, dt)
	}

	// resource bookkeeping
	feed := math.Min(math.Max(c.Feed, 0), 1) * maxFeed
	if c.Burst && s.burstTimer <= 0 && s.Li > 2 {
		s.burstTimer = 1.0
	}
	if s.burstTimer > 0 {
		s.burstTimer -= dt
		feed = maxFeed * 2.5
	}
	if feed*dt > s.Li {
		feed = s.Li / dt
	}
	s.Li -= feed * dt
	s.FeedUsed = feed

	b := s.Veh.CoilField
	if c.Boost && s.boostTimer <= 0 && s.BoostLeft > 0 {
		s.BoostLeft--
		s.boostTimer = 10
	}
	if s.boostTimer > 0 {
		s.boostTimer -= dt
		b *= 1.8
	}

	s.Pt = stateAt(s.H, s.V, s.Veh, s.Prof, b, feed)

	// power brownout: over the vehicle's own bus budget the array loses
	// half its authority, and a starving ship grid costs it again — an
	// entry flown on an empty battery is nearly a bare-body entry.
	authority := s.Pt.Gate
	if s.Pt.PowerDraw > s.Veh.PowerCap {
		authority *= 0.5
	}
	authority *= 0.35 + 0.65*math.Min(math.Max(s.Supply, 0), 1)

	// commanded lift: pitch flies the needle, roll spends some of it sideways
	pitch := math.Min(math.Max(c.Pitch, -1), 1)
	roll := math.Min(math.Max(c.Roll, -1), 1)
	ld := pitch * s.Veh.LDMax * (0.55 + 0.45*authority)
	ldVert := ld * (1 - 0.4*math.Abs(roll))

	// crossrange walk, strongest where the plasma grips
	s.Crossrange += -roll * 6.5 * authority * dt * (s.V / 7800)

	// 3-DOF planar dynamics, RK4
	sArea := math.Pi * s.Veh.Diameter * s.Veh.Diameter / 4
	mu := earthMu * s.Prof.GravityScale
	dragF := s.Pt.DragFactor
	deriv := func(v, gam, h float64) (dv, dgam, dh, ds float64) {
		rho, _ := Atm(h, s.Prof.AtmosScale)
		r := earthR + h
		g := mu / (r * r)
		d := 0.5 * rho * v * v * sArea * cd0 * dragF
		l := 0.5 * rho * v * v * sArea * cd0 * ldVert
		dv = -d/s.Veh.Mass - g*math.Sin(gam)
		dgam = l/(s.Veh.Mass*math.Max(v, 1)) + (v/r-g/math.Max(v, 1))*math.Cos(gam)
		dh = v * math.Sin(gam)
		ds = v * math.Cos(gam) * earthR / r
		return
	}
	v, gam, h := s.V, s.Gamma, s.H
	k1v, k1g, k1h, k1s := deriv(v, gam, h)
	k2v, k2g, k2h, k2s := deriv(v+0.5*dt*k1v, gam+0.5*dt*k1g, h+0.5*dt*k1h)
	k3v, k3g, k3h, k3s := deriv(v+0.5*dt*k2v, gam+0.5*dt*k2g, h+0.5*dt*k2h)
	k4v, k4g, k4h, k4s := deriv(v+dt*k3v, gam+dt*k3g, h+dt*k3h)
	s.V += dt / 6 * (k1v + 2*k2v + 2*k3v + k4v)
	s.Gamma += dt / 6 * (k1g + 2*k2g + 2*k3g + k4g)
	s.H += dt / 6 * (k1h + 2*k2h + 2*k3h + k4h)
	ds := dt / 6 * (k1s + 2*k2s + 2*k3s + k4s)
	s.Downrange += ds
	s.PadDist -= ds / 1000
	s.T += dt

	// g-load for the gauge and the damage model
	rho, _ := Atm(s.H, s.Prof.AtmosScale)
	d := 0.5 * rho * s.V * s.V * sArea * cd0 * dragF
	l := 0.5 * rho * s.V * s.V * sArea * cd0 * ldVert
	s.Pt.GLoad = math.Sqrt(d*d+l*l) / (s.Veh.Mass * g0)

	s.RefG = s.RefGamma(s.H)
	s.Width = s.CorridorWidth(s.H)

	s.accrueDamage(dt)
	s.resolve(dt)
}

// accrueDamage: grazing the limits stings, diving past them ruins.
func (s *Sim) accrueDamage(dt float64) {
	if over := s.Pt.QShielded/s.Veh.TPSLimit - 1; over > 0 {
		s.Dmg.Hull += 6 * over * over * dt
	}
	if over := s.Pt.GLoad/s.Veh.GLimit - 1; over > 0 {
		s.Dmg.Hull += 4 * over * over * dt
		// over-g shakes a random system each second it persists
		if s.rng.Float64() < dt {
			switch s.rng.Intn(3) {
			case 0:
				s.Dmg.Computer += 10 + 15*over
			case 1:
				s.Dmg.Clamps += 10 + 15*over
			default:
				s.Dmg.Hull += 3
			}
		}
	}
	s.Dmg.Hull = math.Min(s.Dmg.Hull, 100)
	s.Dmg.Computer = math.Min(s.Dmg.Computer, 100)
	s.Dmg.Clamps = math.Min(s.Dmg.Clamps, 100)
}

// SkipWarn reports how close the entry is to skipping out, 0..1 — the HUD
// flashes CORRIDOR ABORT as it fills.
func (s *Sim) SkipWarn() float64 { return math.Min(s.skipT/8, 1) }

func (s *Sim) resolve(dt float64) {
	// The skip meter: flying flat or climbing while still hypersonic and
	// above the thick air means the atmosphere is not keeping you — you are
	// surfing off the top of the corridor, and in a few seconds the site is
	// a continent behind you.
	if s.Gamma > -0.0017 && s.H > 55000 && s.V > 5000 { // > -0.1 deg
		s.skipT += dt
	} else {
		s.skipT = math.Max(s.skipT-2*dt, 0)
	}
	switch {
	case s.Dmg.Hull >= 100:
		s.status = Destroyed
	case s.skipT > 8, s.H > 118000 && s.Gamma > 0.005 && s.V > 5500:
		s.status = SkippedOut
	case s.H <= 2000:
		// chute line: arriving hot is survivable but expensive
		if over := s.V/1200 - 1; over > 0 {
			s.Dmg.Hull = math.Min(s.Dmg.Hull+25*over, 100)
		}
		if s.Dmg.Hull >= 100 {
			s.status = Destroyed
		} else {
			s.status = Landed
		}
	}
}

// Score summarises a landed entry for the pay screen.
type Score struct {
	HullLeft   float64
	LiLeft     float64
	CrossKm    float64
	PadBonus   int // profile bonus, paid in full inside 2 km of the pad line
	RepairCost int
}

func (s *Sim) Score() Score {
	sc := Score{
		HullLeft: 100 - s.Dmg.Hull, LiLeft: s.Li,
		CrossKm:    math.Abs(s.Crossrange),
		RepairCost: int(s.Dmg.Hull*900 + s.Dmg.Computer*300 + s.Dmg.Clamps*200),
	}
	if sc.CrossKm < 2 {
		sc.PadBonus = s.Prof.PadBonus
	} else if sc.CrossKm < 10 {
		sc.PadBonus = s.Prof.PadBonus / 2
	}
	return sc
}
