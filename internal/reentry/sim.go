package reentry

import (
	"math"
	"math/rand"
)

// Vehicle is the airframe flying the corridor. The Yodacon's numbers come
// from the recovered shïp 174 record (350 t, 80 m) mapped onto the envelope
// model's vehicle knobs.
type Vehicle struct {
	Mass       float64 // kg, as flown — cargo and outfits included
	RefMass    float64 // kg, the design entry mass the trim tables assume
	Diameter   float64 // m
	NoseRadius float64 // m
	LDMax      float64 // maximum commandable lift-to-drag, plasma phase
	GlideLD    float64 // wing-shape coefficient: the aero-phase glide ratio
	TPSLimit   float64 // W/m2 before damage accrues
	GLimit     float64 // structural deceleration limit
	CoilField  float64 // B0 at the nose, T
	LiTank     float64 // kg of lithium aboard at interface
	RCSTank    float64 // kg of attitude propellant at interface
	PowerCap   float64 // W the ship can supply to the shield
}

// Yodacon is the ship the whole game flies. Diameter and nose radius are
// the *inflated envelope*, not the 80 m hull hiding behind it — without the
// pillow the Yodacon's ballistic coefficient is a falling anvil's; the
// plasma aeroshell is what makes a 350 t freighter flyable at all.
func Yodacon() Vehicle {
	return Vehicle{
		Mass: 350e3, RefMass: 350e3, Diameter: 40, NoseRadius: 12, LDMax: 0.35,
		GlideLD:  1.0, // five by five: the wing shape glides 5:5
		TPSLimit: 60e4, GLimit: 6, CoilField: 1.2, LiTank: 60,
		RCSTank: 130, PowerCap: 4e6,
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

// KarmanLine is the edge of space, in metres. The entry interface sits
// above it (122 km); the corridor's work happens below it. The scene and
// the HUD both key the world's wake-up to this altitude.
const KarmanLine = 100000.0

// WeightFactor is how far over (or under) the design entry mass the ship
// is flying, ≥ ~1 at full load. Everything trimmed for RefMass — thruster
// budgets, response — degrades by this ratio: an overstuffed freighter is
// physically harder to hold on the needle.
func (v Vehicle) WeightFactor() float64 {
	if v.RefMass <= 0 {
		return 1
	}
	return v.Mass / v.RefMass
}

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

	// resources — everything consumable depletes
	Li         float64 // kg of lithium seed remaining
	RCS        float64 // kg of attitude propellant remaining
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
	Pt       Point
	RefG     float64 // reference gamma this frame, deg
	Width    float64 // corridor half-width this frame, deg
	PadDist  float64 // km of downrange remaining to the pad
	LastLD   float64 // vertical L/D actually applied this frame — the predictor's input

	// steering authority split, 0..1 each — the handoff the HUD narrates.
	// PlasmaAuth is the MHD cone's grip (dies as the sheath cools and the
	// coil browns out); AeroAuth is dumb air on control surfaces (grows as
	// the air thickens and the speed falls off hypersonic).
	PlasmaAuth float64
	AeroAuth   float64

	// corridor discipline: OffCorridor is how far outside the pipe the
	// needle is, 0 inside → 1 a full band out; the airframe heats and
	// wears while it is nonzero. GuardianOn reports the protective system
	// dumping lithium feed to save the hull.
	OffCorridor float64
	GuardianOn  bool

	// the flight's conduct, remembered for the debrief: the hardest hit,
	// the hottest moment, the seconds spent outside the pipe, and how
	// many times the reflex had to save you.
	MaxG       float64
	PeakQFrac  float64
	OffPipeT   float64
	Recoveries int

	// the damage-control reflex: hull burning off the corridor trips the
	// flight computer into RECOVERY — it takes the stick and flies back
	// toward the centerline for a few seconds or until the correction
	// holds, surging the coil and the seed (battery and lithium pay) and
	// venting RCS through the emergency thrusters. A cooked computer
	// (>60%) has no reflex left.
	recoveryT  float64 // seconds of override remaining
	recoveryCD float64 // lockout before the reflex can trip again

	auto autoland
	rng  *rand.Rand
}

// New starts an entry at interface: 122 km, slightly super-circular — a
// freight deorbit carries excess energy, so early in the entry the
// atmosphere is trying to throw the ship back out and the pilot must hold
// it down; the balance flips once drag bleeds the speed off.
func New(veh Vehicle, prof Profile, seed int64) *Sim {
	if veh.GlideLD == 0 {
		veh.GlideLD = veh.LDMax
	}
	if veh.RCSTank == 0 {
		veh.RCSTank = 130
	}
	s := &Sim{
		Veh: veh, Prof: prof,
		H: 122000, V: 8350 * math.Sqrt(prof.GravityScale),
		Li: veh.LiTank, RCS: veh.RCSTank, BoostLeft: 2, Supply: 1,
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

// Boosting reports whether the coil overdrive is currently firing.
func (s *Sim) Boosting() bool { return s.boostTimer > 0 }

// Step advances the entry by dt seconds under the given controls.
func (s *Sim) Step(dt float64, c Controls) {
	if s.status != Flying {
		return
	}
	if c.Auto {
		c = s.auto.fly(s, c, dt)
	}

	// the damage-control reflex (manual flight only — the autoland IS the
	// computer). Burning off the corridor trips it; it holds the stick
	// until the needle is most of the way home or its few seconds run out.
	s.recoveryCD = math.Max(s.recoveryCD-dt, 0)
	recovering := false
	if !c.Auto && s.Dmg.Computer < 60 {
		// steep side only: the reflex pulls you out of a burning dive.
		// The shallow side stays the pilot's problem — the skip meter is
		// the warning there, and skipping out remains possible.
		if s.recoveryT <= 0 && s.recoveryCD <= 0 && s.OffCorridor > 0.25 &&
			s.GammaError() < 0 {
			s.recoveryT = 5
			s.Recoveries++
		}
		if s.recoveryT > 0 {
			s.recoveryT -= dt
			err := s.GammaError()
			if math.Abs(err) < 0.4*s.Width {
				s.recoveryT = 0
				s.recoveryCD = 6
			} else {
				recovering = true
				c.Pitch = math.Min(math.Max(-err*1.4, -1), 1)
				// the emergency thrusters vent hard to swing a freighter
				s.RCS = math.Max(s.RCS-0.5*s.Veh.WeightFactor()*dt, 0)
			}
			if s.recoveryT <= 0 {
				s.recoveryCD = 6
			}
		}
	} else {
		s.recoveryT = 0
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
	// the guardian: drifting toward the corridor's edge with the sheath
	// hot, the flight computer overrides and dumps seed to save the hull.
	// Protection is not free — it is paid straight out of the reserves.
	s.GuardianOn = false
	if math.Abs(s.GammaError()) > 0.75*s.Width &&
		s.Pt.QShielded > 0.3*s.Veh.TPSLimit && s.Li > 0 {
		s.GuardianOn = true
		if feed < maxFeed {
			feed = maxFeed
		}
	}
	if recovering && s.Li > 0 && feed < maxFeed {
		feed = maxFeed // the reflex floods the shield while it flies you home
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
	if recovering {
		b *= 1.3 // shield surge: the extra draw comes off the battery
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
	s.PlasmaAuth = authority

	// aero authority: once the plasma lets go the ship is an 350 t lifting
	// body — control surfaces need thick air and sub-hypersonic speed.
	rhoNow, _ := Atm(s.H, s.Prof.AtmosScale)
	qd := 0.5 * rhoNow * s.V * s.V
	s.AeroAuth = math.Min(qd/4000, 1) *
		math.Min(math.Max((3200-s.V)/2400, 0), 1)

	// commanded lift: pitch flies the needle, roll spends some of it
	// sideways. Whichever medium grips harder carries the command.
	pitch := math.Min(math.Max(c.Pitch, -1), 1)
	roll := math.Min(math.Max(c.Roll, -1), 1)

	// RCS: every commanded degree costs propellant, triple when diving
	// steep against thick air, and scaled by the weight factor — the
	// thrusters were sized for the design mass, so a full hold pays for
	// every correction. An empty tank is a mushy stick — come in steep
	// and the bottles are critical by the flare.
	spend := (0.55*math.Abs(pitch) + 0.45*math.Abs(roll)) *
		(0.5 + math.Min(qd/9000, 2.5)) * 0.085 * s.Veh.WeightFactor() *
		(0.3 + 0.7*math.Min(s.V/3000, 1)) // aero trim takes over low and slow
	if s.GammaError() < -s.Width {
		spend *= 2.2
	}
	s.RCS = math.Max(s.RCS-spend*dt, 0)
	// the last kilos fade — but the floor is higher than it was: the
	// terminal sink is where a pilot needs the stick most
	rcsAuth := 0.45 + 0.55*math.Min(s.RCS/5, 1)

	// the wing-shape coefficient: as the airframe takes over, the
	// commandable L/D opens up from the cone's 0.35 to the glide ratio
	grip := math.Max(authority, 0.9*s.AeroAuth)
	ldMax := s.Veh.LDMax + (s.Veh.GlideLD-s.Veh.LDMax)*math.Min(s.AeroAuth*1.2, 1)
	ld := pitch * ldMax * (0.55 + 0.45*grip) * rcsAuth
	ldVert := ld * (1 - 0.4*math.Abs(roll))
	s.LastLD = ldVert

	// crossrange walk: the cone steers where the plasma grips, the airframe
	// steers where the air does — both through the thrusters' authority
	s.Crossrange += -roll * dt * rcsAuth *
		(6.5*authority*(s.V/7800) + 4.5*s.AeroAuth)

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

	// the conduct ledger
	s.MaxG = math.Max(s.MaxG, s.Pt.GLoad)
	s.PeakQFrac = math.Max(s.PeakQFrac, s.Pt.QShielded/s.Veh.TPSLimit)
	if s.OffCorridor > 0.02 {
		s.OffPipeT += dt
	}

	s.accrueDamage(dt)
	s.resolve(dt)
}

// accrueDamage: grazing the limits stings, diving past them ruins.
func (s *Sim) accrueDamage(dt float64) {
	if over := s.Pt.QShielded/s.Veh.TPSLimit - 1; over > 0 {
		s.Dmg.Hull += 6 * over * over * dt
	}
	// the pipe is not advisory: while the corridor is live — hypersonic,
	// the sheath doing the flying — leaving the band puts the flow onto
	// the airframe at the wrong angle and the hull pays, faster the
	// further out and the hotter the sheath. Below hypersonic the
	// corridor has done its job and the terminal sink is free to steepen.
	// a grace margin past the painted band, then it costs; and only while
	// the sheath is actually hot — a cold shallow graze is the skip
	// meter's business, not the hull's
	if off := math.Abs(s.GammaError()) - 1.3*s.Width; off > 0 && s.V > 2500 &&
		s.Pt.QShielded > 0.15*s.Veh.TPSLimit {
		s.OffCorridor = math.Min(off/math.Max(s.Width, 0.01), 1)
		s.Dmg.Hull += (0.5 + 2.2*s.OffCorridor) * dt *
			math.Min(s.Pt.QShielded/s.Veh.TPSLimit+0.2, 1.2)
	} else {
		s.OffCorridor = math.Max(s.OffCorridor-1.5*dt, 0)
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

// Recovering reports the damage-control reflex holding the stick.
func (s *Sim) Recovering() bool { return s.recoveryT > 0 }

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
	case s.H <= 2000, s.V < 300 && s.H < 20000:
		// the deck, or parked: once the ship is under 300 m/s in the
		// terminal sink the landing is decided — no need to crawl the
		// last kilometres in real time. Arriving hot is survivable but
		// expensive.
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

// PredPt is one sample of a projected trajectory.
type PredPt struct {
	T, V, Gamma, H, Downrange float64
}

// Predict flies the ship forward from its current state holding the
// current vertical L/D — "where does this stick position put me" — with
// the same equations of motion as Step, no damage, no resources. The HUD
// draws it as the dotted future-position line; the pilot reads it as the
// answer to whether the needle is about to leave the pipe.
func (s *Sim) Predict(horizon, sample float64) []PredPt {
	sArea := math.Pi * s.Veh.Diameter * s.Veh.Diameter / 4
	mu := earthMu * s.Prof.GravityScale
	dragF := s.Pt.DragFactor
	if dragF <= 0 {
		dragF = 1
	}
	v, gam, h, dr := s.V, s.Gamma, s.H, s.Downrange
	out := []PredPt{}
	const dt = 0.5
	for t := 0.0; t <= horizon && h > 2500 && v > 320; t += dt {
		rho, _ := Atm(h, s.Prof.AtmosScale)
		r := earthR + h
		g := mu / (r * r)
		d := 0.5 * rho * v * v * sArea * cd0 * dragF
		l := 0.5 * rho * v * v * sArea * cd0 * s.LastLD
		v += dt * (-d/s.Veh.Mass - g*math.Sin(gam))
		gam += dt * (l/(s.Veh.Mass*math.Max(v, 1)) +
			(v/r-g/math.Max(v, 1))*math.Cos(gam))
		h += dt * v * math.Sin(gam)
		dr += dt * v * math.Cos(gam) * earthR / r
		if math.Mod(t+dt, sample) < dt {
			out = append(out, PredPt{T: t + dt, V: v, Gamma: gam, H: h, Downrange: dr})
		}
	}
	return out
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
