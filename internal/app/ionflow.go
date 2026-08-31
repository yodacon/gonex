package app

import (
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
)

// The ion flow: the oncoming plasma as a PHYSICS SIMULATION, not a drawing.
// Nothing here paints a bow wave or a mach diamond directly — ions spawn at
// the trajectory's vanishing point on the horizon, fly at the ship with
// real momentum, and everything the screen shows is what the forces do to
// them:
//
//   - PERSPECTIVE: each ion accelerates and swells as it closes — a speck
//     at the horizon becoming fire past the camera. Whatever the shield
//     turns away is thrown down past the bottom of the frame.
//
//   - MACH DIAMONDS, emergent: ions ride the trajectory's field-line
//     bundle and gyrate about it — a magnetic-tension restoring force,
//     integrated per ion. They spawn phase-locked (at the turn of their
//     swing, the way a nozzle lip clocks the exhaust), so every ion
//     crosses the axis at the same stations. The crossings ARE the
//     diamonds: knots of pure particle density, spaced by v/ω — and since
//     v grows as the flow closes, the cells stretch toward the ship,
//     perspective for free.
//
//   - DOUBLE BOW WAVES, emergent: the ship flies a magnetic dipole. Each
//     ion carries a pole sign and feels the v-cross-B swirl (curving its
//     track around the hull, handedness by charge) plus the magnetic
//     mirror: an exponential pressure wall at the standoff shell and a
//     softer second wall at the outer magnetosphere. Oncoming momentum
//     against two mirror walls piles the ions into two nested arcs — the
//     bow waves collect, they are not drawn.
//
// The pass renders through its own offscreen at partial alpha, additive
// inside, so density reads as brightness: where trajectories bunch, the
// screen glows — the whole point.

type ion struct {
	x, y, vx, vy float64
	q            float64 // pole sign: the swirl handedness
	amp          float64 // gyration amplitude about the field bundle
	sz           float64 // perspective scale, grown in flight
	age, span    float64
	defl         float64 // 0 oncoming → 1 turned away by the shield
	wide         bool    // ambient rain outside the bundle
	live         bool
}

type ionFlow struct {
	ions     []ion
	img      *ebiten.Image
	rng      *rand.Rand
	t        float64
	surge    float64 // pellet-burst kick, decaying
	spawnAcc float64
}

func newIonFlow(seed int64, capacity int) *ionFlow {
	return &ionFlow{
		ions: make([]ion, capacity),
		rng:  rand.New(rand.NewSource(seed)),
	}
}

// Boost is the pellet burst reaching the flow: a wave of extra ions and a
// brightness kick, decaying over the next half second.
func (f *ionFlow) Boost(a float64) { f.surge = math.Min(f.surge+a, 1) }

// ion gyration rate about the field bundle. Node spacing is v/ω·π, so the
// diamonds stand ~40 px apart at horizon speed and stretch past 300 px as
// the flow accelerates in — the train foreshortens itself.
const ionOmega = 6.2

// step spawns and integrates. The geometry is the frame's: dipole on the
// ship's nose, field bundle running from the vanishing point to it.
func (f *ionFlow) step(dt, heat float64, g bowGeom, vpX, vpY float64) {
	f.t += dt
	f.surge *= math.Pow(0.25, dt)
	dipX, dipY := g.cx, g.nose+6
	axX, axY := dipX-vpX, dipY-vpY
	span := math.Hypot(axX, axY)
	if span < 40 {
		span, axX, axY = 40, 0, 40
	}
	axX, axY = axX/span, axY/span
	perpX, perpY := -axY, axX
	r1 := 38 + g.standPx*1.15
	r2 := r1 * promOuterGeo

	// spawn: the horizon feeds the flow as long as there is heat to
	// carry, pulsing — so the tube carries visible PACKETS of ions, a
	// sequence of arrivals marching at the ship instead of a steady hiss
	pulse := 0.55 + 0.45*math.Sin(f.t*3.4)
	f.spawnAcc += dt * (120 + 430*heat*pulse + 700*f.surge)
	for f.spawnAcc >= 1 {
		f.spawnAcc--
		f.spawn(vpX, vpY, axX, axY, perpX, perpY)
	}

	for i := range f.ions {
		p := &f.ions[i]
		if !p.live {
			continue
		}
		p.age += dt
		v := math.Hypot(p.vx, p.vy)
		if p.age > p.span || v < 1e-3 {
			p.live = false
			continue
		}
		fx, fy := 0.0, 0.0
		// perspective momentum: the flow accelerates as it closes, until
		// the shield owns it — and never past its terminal speed
		if v < 950 {
			acc := 480 * (1 - p.defl*0.8)
			fx += p.vx / v * acc
			fy += p.vy / v * acc
		}
		// magnetic tension about the field bundle — the emergent-diamond
		// force. Only the bundled, still-oncoming ions ride it.
		if !p.wide && p.defl < 0.5 {
			d := (p.x-vpX)*perpX + (p.y-vpY)*perpY
			fx -= perpX * ionOmega * ionOmega * d
			fy -= perpY * ionOmega * ionOmega * d
		}
		// the dipole: v×B swirl by pole sign, and the two mirror walls —
		// the standoff shell and the outer magnetosphere
		rx, ry := p.x-dipX, p.y-dipY
		r := math.Max(math.Hypot(rx, ry), 45)
		swirl := p.q * 6.5e4 / (r * r * r)
		fx += swirl * p.vy
		fy += swirl * -p.vx
		mir := 2400*math.Exp((r1-r)/13) + 1100*math.Exp((r2-r)/24)
		if mir > 2400 {
			mir = 2400
		}
		fx += rx / r * mir
		fy += ry / r * mir
		if mir > 500 {
			p.defl += (1 - p.defl) * math.Min(dt*6, 1)
		}
		// rejected fire is thrown down past the camera
		fy += 40 + 300*p.defl
		p.vx += fx * dt
		p.vy += fy * dt
		// a light drag keeps the mirror bounce from pumping energy in,
		// light enough that rejected ions still slide off around the shell
		drag := 1 - (0.10+0.25*p.defl)*dt
		p.vx *= drag
		p.vy *= drag
		p.x += p.vx * dt
		p.y += p.vy * dt
		// perspective size: distance closed plus the deflection blow-up
		prog := 1 - math.Min(math.Hypot(p.x-dipX, p.y-dipY)/span, 1)
		p.sz = math.Min(0.55+1.7*prog*prog+2.6*p.defl*prog, 3.0)
		if p.wide {
			p.sz = math.Min(p.sz, 1.5) // the rain stays background
		}
		if p.y > gaugeTop+60 || p.x < -80 || p.x > ScreenW+80 ||
			p.y < vpY-140 {
			p.live = false
		}
	}
}

// spawn seeds one ion at the horizon. Bundle ions start at the TURN of
// their gyration — full offset, zero transverse speed — so the whole flow
// swings in phase and the axis crossings line up into diamonds.
func (f *ionFlow) spawn(vpX, vpY, axX, axY, perpX, perpY float64) {
	var slot *ion
	for i := range f.ions {
		if !f.ions[i].live {
			slot = &f.ions[i]
			break
		}
	}
	if slot == nil {
		return
	}
	r := f.rng
	q := math.Copysign(1, r.Float64()-0.5)
	v0 := 95 + r.Float64()*24
	if r.Float64() < 0.22 {
		// the ambient rain: the wide sky falling at the ship, feeding the
		// bow pileup from every bearing
		*slot = ion{
			x: vpX + (r.Float64()-0.5)*760, y: vpY + (r.Float64()-0.5)*40,
			q: q, span: 5 + r.Float64()*3, wide: true, live: true,
		}
		dx := (f.rngShipX() + (r.Float64()-0.5)*160) - slot.x
		dy := (gaugeTop - 40) - slot.y
		l := math.Hypot(dx, dy)
		slot.vx, slot.vy = dx/l*v0, dy/l*v0
		return
	}
	// phase coherence is the diamonds: barely any jitter along the axis,
	// so every ion turns and crosses at the same stations
	amp := 8 + r.Float64()*30
	side := math.Copysign(1, r.Float64()-0.5)
	along := r.Float64() * 6
	*slot = ion{
		x:    vpX + axX*along + perpX*amp*side,
		y:    vpY + axY*along + perpY*amp*side,
		vx:   axX * v0,
		vy:   axY * v0,
		q:    q,
		amp:  amp,
		span: 5 + r.Float64()*3,
		live: true,
	}
}

// rngShipX is where the ambient rain aims: the ship anchor, every mode.
func (f *ionFlow) rngShipX() float64 { return shipX }

// draw renders the swarm through the translucent offscreen: glow bodies
// first, streaks and kernels after — density is brightness, so the
// diamonds and the double bow arcs appear wherever the sim has bunched
// the trajectories.
func (f *ionFlow) draw(dst *ebiten.Image, g bowGeom, heat float64) {
	heat = math.Min(heat+f.surge*0.5, 1.2)
	if heat <= 0.03 || g.alpha <= 0.02 {
		return
	}
	if f.img == nil {
		f.img = ebiten.NewImage(ScreenW, int(gaugeTop))
	}
	img := f.img
	img.Clear()
	for pass := 0; pass < 2; pass++ {
		for i := range f.ions {
			p := &f.ions[i]
			if !p.live {
				continue
			}
			v := math.Hypot(p.vx, p.vy)
			// color rides the speed: rust-red drift at the horizon,
			// white-gold fire at the shield
			col := promRamp(math.Min(math.Max(1.05-v/780, 0), 1) * (1 - 0.35*p.defl))
			fade := math.Min(p.age*3, 1) * math.Min((p.span-p.age)*1.5, 1)
			al := (0.30 + 0.45*math.Min(v/650, 1)) * fade * heat * g.alpha
			if p.wide {
				al *= 0.55
			}
			if pass == 0 {
				glowDot(img, float32(p.x), float32(p.y),
					float32(5+7*p.sz), col, al*0.40)
			} else {
				// the momentum streak: a tail one frame long, stretched
				// by speed — the fire flying at the camera
				tl := 0.022 + 0.02*p.defl
				fastLine(img, float32(p.x-p.vx*tl), float32(p.y-p.vy*tl),
					float32(p.x), float32(p.y),
					float32(0.8+0.9*p.sz), col, al)
				fastDot(img, float32(p.x), float32(p.y),
					float32(0.6+0.8*math.Min(p.sz, 1.4)), col,
					math.Min(al*1.4, 1))
			}
		}
	}
	op := &ebiten.DrawImageOptions{}
	op.ColorScale.ScaleAlpha(float32(math.Min(0.60+0.30*heat, 0.9)))
	dst.DrawImage(img, op)
}
