package app

import (
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
)

// The solar-prominence layer: the H-alpha limb photograph laid over the bow
// wave. The reference frames read as three things — a fan of HAIRLINE
// filaments erupting from one footpoint, an electric white base band where
// the fan meets the limb, and a deep maroon ambient the whole structure
// hangs in. Color along a filament is distance from the base: white-gold at
// the root, prominence gold through the body, rusting to dark red where the
// crown folds over against the black.
//
// Here the limb is the standoff shell: filaments root on the mirror arc,
// erupt INTO the oncoming flow, sway on their own slow clocks, and carry a
// brightness wave streaming outward — plasma riding the line, not a static
// streak. Each filament is a fountain: it erupts, holds, collapses, and
// re-rolls somewhere else on the arc, so the fan breathes the way the
// telescope footage does.
//
// The whole pass renders into its own offscreen and composites back at
// partial alpha — a translucent veil OVER the particle deflection, so the
// filaments read as light seen through the sheath, never paint on top of it.

type promFil struct {
	u      float64 // root along the shell arc, -1..1
	splay  float64 // extra fan-out off the shell normal, radians
	length float64 // full eruption reach, px
	curl   float64 // signed crown-over bend
	swayPh float64
	swayW  float64
	flowPh float64
	flowW  float64 // speed of the outward-streaming brightness wave
	life   float64
	span   float64
}

// promWisp is one streak of the deflected flow: fire sliding along the
// shell and peeling aft past a shoulder — the stream the mirror is actually
// turning away, drawn in the same see-through layer.
type promWisp struct {
	arc  float64 // position along the shell, -1..1
	spd  float64
	side float64 // which shoulder it peels around
	life float64
	span float64
}

type promLayer struct {
	fil   []promFil
	wisps []promWisp
	img   *ebiten.Image
	rng   *rand.Rand
	t     float64
	surge float64 // pellet-burst brightness kick, decaying

	// layer shape — 1/1/0/1 is the inner fan; the outer magnetosphere
	// instance runs the same fountain on a vaster shell
	geo     float64 // shell standoff/radius multiplier
	reach   float64 // filament length multiplier
	stretch float64 // 0 radial fan .. 1 lobes dragged flat to the sides
	veil    float64 // composite alpha multiplier
}

// promOuterGeo is the secondary shell's standoff ratio over the primary —
// shared by the outer prominence layer and the ion flow's mirror shells,
// so the second wave of lobes and the double bow pileup stand together.
const promOuterGeo = 2.15

// The H-alpha ramp, base → crown. Stop 0 is the images' electric limb band.
var promStops = [5]color.RGBA{
	{236, 246, 255, 255}, // white-hot base
	{255, 238, 198, 255}, // white gold
	{255, 184, 88, 255},  // prominence gold
	{222, 108, 36, 255},  // deep orange
	{136, 40, 14, 255},   // rust red crown
}

func promRamp(t float64) color.RGBA {
	if t <= 0 {
		return promStops[0]
	}
	if t >= 1 {
		return promStops[4]
	}
	f := t * 4
	i := int(f)
	f -= float64(i)
	a, b := promStops[i], promStops[i+1]
	return color.RGBA{
		uint8(float64(a.R) + (float64(b.R)-float64(a.R))*f),
		uint8(float64(a.G) + (float64(b.G)-float64(a.G))*f),
		uint8(float64(a.B) + (float64(b.B)-float64(a.B))*f),
		255,
	}
}

func newPromLayer(seed int64, n int) *promLayer {
	p := &promLayer{rng: rand.New(rand.NewSource(seed)),
		geo: 1, reach: 1, stretch: 0, veil: 1}
	p.fil = make([]promFil, n)
	for i := range p.fil {
		p.rollFil(&p.fil[i])
		p.fil[i].life = p.rng.Float64() * p.fil[i].span // desync the fountain
	}
	p.wisps = make([]promWisp, 26)
	for i := range p.wisps {
		p.rollWisp(&p.wisps[i])
		p.wisps[i].life = p.rng.Float64() * p.wisps[i].span
	}
	return p
}

// newOuterPromLayer is the second wave of lobes: the same fountain run on
// the secondary bow shell, twice as far out, with the filaments dragged
// flat to the sides — the vast outer magnetosphere the inner fan sits
// inside. It composites a little fainter, so distance reads as thinness.
func newOuterPromLayer(seed int64, n int) *promLayer {
	p := newPromLayer(seed, n)
	p.geo, p.reach, p.stretch, p.veil = promOuterGeo, 2.1, 0.8, 0.72
	return p
}

func (p *promLayer) rollFil(f *promFil) {
	r := p.rng
	u := r.Float64()*2 - 1
	f.u = u * math.Abs(u) // packed at the footpoint, sparse at the shoulders
	// the fan radiates from the footpoint across a wide crown, not just
	// along the local normal — the images' single-origin spray
	f.splay = (r.Float64() - 0.5) * 1.1
	f.length = 70 + r.Float64()*170
	f.curl = (r.Float64() - 0.5) * 120
	f.swayPh = r.Float64() * math.Pi * 2
	f.swayW = 0.8 + r.Float64()*1.6
	f.flowPh = r.Float64() * math.Pi * 2
	f.flowW = 3 + r.Float64()*4
	f.life, f.span = 0, 2.2+r.Float64()*3.4
}

func (p *promLayer) rollWisp(w *promWisp) {
	r := p.rng
	w.arc = (r.Float64() - 0.5) * 0.5
	w.side = math.Copysign(1, r.Float64()-0.5)
	w.spd = 0.5 + r.Float64()*0.9
	w.life, w.span = 0, 0.9+r.Float64()*1.1
}

// Boost is the pellet burst reaching the fan: a brightness kick that decays
// over the next half second.
func (p *promLayer) Boost(a float64) { p.surge = math.Min(p.surge+a, 1) }

// step advances the fountain. heat gates how eagerly collapsed filaments
// re-erupt, so a cooling sheath visibly thins the fan before it dies.
func (p *promLayer) step(dt, heat float64) {
	p.t += dt
	p.surge *= math.Pow(0.25, dt)
	for i := range p.fil {
		f := &p.fil[i]
		f.life += dt
		if f.life >= f.span && p.rng.Float64() < dt*(0.5+3.5*heat) {
			p.rollFil(f)
		}
	}
	for i := range p.wisps {
		w := &p.wisps[i]
		w.life += dt
		w.arc += w.spd * dt * w.side
		if w.life >= w.span || math.Abs(w.arc) > 1.1 {
			p.rollWisp(w)
		}
	}
}

// promGrow is one filament's eruption envelope: smoothstep up over the
// first third, a slow fall over the last, zero while it waits to re-roll.
func promGrow(life, span float64) float64 {
	if life >= span {
		return 0
	}
	up := math.Min(life/(span*0.3), 1)
	down := math.Min(math.Max((span-life)/(span*0.35), 0), 1)
	return up * up * (3 - 2*up) * down
}

// draw renders the fan into the layer's own offscreen and lays it over the
// scene at partial alpha — the translucent pass: fire the bow wave shows
// through. Geometry is the same shell arc drawBowFire bends the fire grid
// onto, so the fan and the flame always agree about where the limb is.
func (p *promLayer) draw(dst *ebiten.Image, g bowGeom, heat float64) {
	heat = math.Min(heat+p.surge*0.5, 1.2)
	if heat <= 0.03 || g.alpha <= 0.02 {
		return
	}
	if p.img == nil {
		p.img = ebiten.NewImage(ScreenW, int(gaugeTop))
	}
	img := p.img
	img.Clear()

	sp := g.standPx * p.geo
	rx0 := (40 + g.standPx*0.85) * p.geo
	lobe := -g.roll
	rollAbs := math.Abs(g.roll)
	arcPt := func(u float64) (x, y, ang float64) {
		ang = u * 1.25
		side := math.Sin(ang)
		swell := 1 + 0.22*math.Max(0, lobe*side)*rollAbs
		x = g.cx + side*rx0*swell
		y = g.nose - sp*swell + (1-math.Cos(ang))*sp*0.75
		return
	}

	// the deep maroon ambient the filaments hang in — held ABOVE the shell
	// so it reads as the fan's sky, not a shadow dome over the ground
	glowDot(img, float32(g.cx), float32(g.nose-sp-95*p.geo), float32(220*p.geo),
		color.RGBA{120, 26, 8, 255}, 0.26*heat)
	glowDot(img, float32(g.cx), float32(g.nose-sp-45*p.geo), float32(140*p.geo),
		color.RGBA{168, 52, 12, 255}, 0.22*heat)

	// the electric base band along the shell — the limb the fan stands on,
	// brightening under the steering lobe exactly like the fire's front
	for i := 0; i <= 16; i++ {
		u := -1 + 2*float64(i)/16
		x, y, _ := arcPt(u)
		hot := 1 - 0.5*u*u
		hot *= 1 + 0.8*math.Max(0, lobe*math.Sin(u*1.25))*rollAbs
		glowDot(img, float32(x), float32(y), float32(16+10*heat),
			color.RGBA{225, 240, 252, 255}, 0.20*heat*hot)
	}

	// the filament fan, two batched passes: all glow bodies, then all
	// hairlines — same two-batch discipline as the rest of the scene
	for pass := 0; pass < 2; pass++ {
		for fi := range p.fil {
			f := &p.fil[fi]
			grow := promGrow(f.life, f.span)
			if grow <= 0.02 {
				continue
			}
			bx, by, ang := arcPt(f.u)
			// outward off the shell plus the filament's own splay, the
			// whole fan dragging with the sweep
			// stretch drags the direction flat to the sides — the outer
			// layer's lobes pulled outboard off the secondary shell
			fa := ang + f.splay*(1+0.6*p.stretch)
			dx := math.Sin(fa)*(0.9+2.2*p.stretch) - g.roll*0.55*(1+p.stretch)
			dy := -math.Cos(fa)
			il := 1 / math.Hypot(dx, dy)
			dx, dy = dx*il, dy*il
			perpX, perpY := -dy, dx
			L := f.length * p.reach * grow * (0.55 + 0.45*heat)
			const segs = 11
			var lxp, lyp float64
			for si := 0; si <= segs; si++ {
				s := float64(si) / segs
				// crown-over: the tip bends sideways and droops, the way
				// the fan folds against the dark in the reference frames
				cb := s * s
				sway := math.Sin(p.t*f.swayW+f.swayPh+s*2.8) * 7 * s
				curl := f.curl * (1 + 0.9*p.stretch)
				x := bx + dx*L*s + perpX*(curl*cb+sway)
				y := by + dy*L*s + perpY*(curl*cb+sway) + cb*L*0.30
				if si > 0 {
					// the streaming brightness: a wave of plasma riding
					// outward along the line
					flow := 0.6 + 0.4*math.Sin(s*9-p.t*f.flowW+f.flowPh)
					al := (1 - 0.5*s) * flow * heat * g.alpha
					col := promRamp(s)
					if pass == 0 {
						if si%2 == 0 {
							glowDot(img, float32(x), float32(y),
								float32(11-6*s), col, al*0.5)
						}
					} else {
						fastLine(img, float32(lxp), float32(lyp),
							float32(x), float32(y),
							float32(2.6-1.7*s), col, al)
					}
				}
				lxp, lyp = x, y
			}
			// the crown knot: a bright bead where the flow wave breaks at
			// the tip — the images' luminous fan edge
			if pass == 0 {
				crest := math.Sin(9 - p.t*f.flowW + f.flowPh)
				if crest > 0.55 {
					glowDot(img, float32(lxp), float32(lyp), 9,
						color.RGBA{255, 224, 170, 255},
						0.8*(crest-0.55)/0.45*heat*grow)
				}
			}
		}
	}

	// the deflection wisps: the flow the mirror is turning away, sliding
	// along the shell and peeling aft past the shoulders
	for wi := range p.wisps {
		w := &p.wisps[wi]
		if math.Abs(w.arc) > 1.1 {
			continue
		}
		fade := math.Sin(math.Min(w.life/w.span, 1) * math.Pi)
		x0, y0, ang := arcPt(w.arc)
		tx := math.Cos(ang)
		ty := math.Abs(math.Sin(ang)) * 0.9
		il := 1 / math.Hypot(tx, ty)
		tx, ty = tx*il*w.side, ty*il
		ln := (16 + 26*math.Abs(w.arc)) * p.geo
		col := promRamp(0.25 + 0.5*math.Abs(w.arc))
		fastLine(img, float32(x0-tx*ln), float32(y0-ty*ln),
			float32(x0), float32(y0), 2.2, col, 0.30*fade*heat*g.alpha)
		glowDot(img, float32(x0), float32(y0), 10, col, 0.22*fade*heat*g.alpha)
	}

	// composite: the one translucent pass — the alpha layer over the scene
	op := &ebiten.DrawImageOptions{}
	op.ColorScale.ScaleAlpha(float32(math.Min(0.50+0.32*heat, 0.85) * p.veil))
	dst.DrawImage(img, op)
}
