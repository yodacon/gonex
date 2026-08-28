package app

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"yodacon.org/gonex/internal/city"
)

// The landing-site renderer: one procedural spaceport metropolis per
// stellar (internal/city, the ascitty seed formula), drawn in scale
// through whatever projection the caller is flying. The projection is a
// closure:
//
//	proj(latKm, aheadKm) → screen x, y, distance, ok
//
// so this file knows geometry and paint, and nothing about altitude.
//
// Everything here draws through the fastdraw batch path — one white texel,
// DrawTriangles, no per-call locking — and the building loop culls on one
// cheap projection before doing any more work. That is what holds the
// frame rate with thousands of lots in the slice.

// district palette — narrow on purpose; a wider one reads as confetti
var portHues = [4]color.RGBA{
	{82, 118, 168, 255}, // glass blue
	{70, 140, 150, 255}, // glass cyan
	{140, 92, 66, 255},  // brick
	{150, 122, 80, 255}, // ochre
}

var (
	colTarmac   = color.RGBA{34, 36, 40, 255}
	colRoadLine = color.RGBA{235, 235, 225, 255}
	colWindow   = color.RGBA{255, 214, 140, 255}
	colLampGlow = color.RGBA{255, 236, 190, 255}
	colHeadl    = color.RGBA{240, 240, 255, 255}
	colTaill    = color.RGBA{255, 80, 60, 255}
	colHuman    = color.RGBA{220, 225, 235, 255}
	colLand     = color.RGBA{88, 70, 50, 255} // brown earth
	colSand     = color.RGBA{152, 134, 98, 255}
	colWater    = color.RGBA{38, 72, 96, 255}
	colRoof     = color.RGBA{200, 210, 220, 255}
)

type portProj func(latKm, aheadKm float64) (x, y, dist float64, ok bool)

func shade(c color.RGBA, f float64) color.RGBA {
	cl := func(v float64) uint8 {
		if v > 255 {
			return 255
		}
		return uint8(v)
	}
	return color.RGBA{cl(float64(c.R) * f), cl(float64(c.G) * f), cl(float64(c.B) * f), 255}
}

// drawPortScene paints the spaceport metropolis: the circle of land, the
// shore repeated in contour tracings out to the ocean, the two landing
// roads (bare, lit, and empty — never any traffic), then the districts on
// both banks, far to near, with hashed windows, avenue traffic and, close
// in, people on the frontages. day is the sun's brightness 0..1.
func drawPortScene(screen *ebiten.Image, p *city.Port, T, vis, day float64,
	proj portProj, rot func(x, y float64) (float32, float32)) {

	night := 1 - day
	lights := 0.45 + 0.55*night

	// The ground itself is the caller's base fill — everything inside the
	// shore ring is land by construction, so nothing to paint here.
	// the shore, and the ocean beyond: the coastline repeated in parallel
	// contour tracings — the water is the repetition, never a fill
	cLat, cAhead := city.Runway2Lat/2, (city.TownFrom+city.TownTo)/2
	ph1 := float64(uint32(p.Seed)%628) / 100
	ph2 := float64(uint32(p.Seed>>7)%628) / 100
	for k := 0; k < 6; k++ {
		r0 := city.LandRadius + float64(k)*(1.0+0.5*float64(k))
		c := colWater
		al := vis * (0.55 - 0.07*float64(k)) * (0.35 + 0.65*day)
		if k == 0 {
			c, al = colSand, vis*0.6*(0.4+0.6*day)
		}
		prevOK := false
		var px, py float32
		for i := 0; i <= 72; i++ {
			th := 2 * math.Pi * float64(i) / 72
			r := r0 + 1.3*math.Sin(3*th+ph1) + 0.8*math.Sin(7*th+ph2)
			x, y, d, ok := proj(cLat+r*math.Cos(th), cAhead+r*math.Sin(th))
			if !ok || d > 55 || d < 0.8 {
				prevOK = false
				continue
			}
			fx, fy := rot(x, y)
			if prevOK {
				fastLine(screen, px, py, fx, fy,
					float32(math.Min(0.09/d*430+0.8, 3)), c, al)
			}
			px, py, prevOK = fx, fy, true
		}
	}

	// --- the landing roads: two parallel arterials laid like runways,
	// distance-adaptive segments, and an under-nose extension so the
	// ground under a low camera on the road is ALWAYS road.
	drawRoad := func(center float64) {
		type xsec struct {
			x, y, w float64
			ok      bool
		}
		var near, near2 xsec
		for a := city.RunwayFrom; a < city.RunwayTo; {
			x0, y0, d0, ok0 := proj(center, a)
			step := 0.18
			if ok0 {
				step = math.Max(0.03, d0*0.28)
			}
			x1, y1, d1, ok1 := proj(center, a+step)
			a += step
			if !ok0 || !ok1 {
				continue
			}
			w := city.RunwayWKm / ((d0 + d1) / 2) * 430
			fx0, fy0 := rot(x0, y0)
			fx1, fy1 := rot(x1, y1)
			fastLine(screen, fx0, fy0, fx1, fy1, float32(w), colTarmac, vis*0.9)
			if !near.ok || y0 > near.y {
				near2 = near
				near = xsec{x0, y0, city.RunwayWKm / d0 * 430, true}
				if d0 > 3.5 {
					near.ok = false // camera is not over this road
				}
			}
		}
		// the camera is over the road: extend the nearest cross-section
		// down past the bottom of the frame so the tarmac never cuts out
		if near.ok && near2.ok && near.y < float64(ScreenH) && near.y > near2.y+0.5 {
			t := (float64(ScreenH) + 40 - near.y) / (near.y - near2.y)
			if t > 0 && t < 60 {
				xb := near.x + (near.x-near2.x)*t
				wb := near.w + (near.w-near2.w)*t
				x0l, y0l := rot(near.x-near.w/2, near.y)
				x0r, _ := rot(near.x+near.w/2, near.y)
				xbl, ybl := rot(xb-wb/2, float64(ScreenH)+40)
				xbr, _ := rot(xb+wb/2, float64(ScreenH)+40)
				fastQuad(screen, x0l, y0l, x0r, y0l, xbr, ybl, xbl, ybl,
					colTarmac, vis*0.9)
			}
		}
	}
	drawRoad(0)
	drawRoad(city.Runway2Lat)
	// centreline dashes and threshold bars on runway A — ours
	for a := city.RunwayFrom; a < city.RunwayTo; a += 0.4 {
		x0, y0, d0, ok0 := proj(0, a)
		x1, y1, _, ok1 := proj(0, a+0.17)
		if !ok0 || !ok1 {
			continue
		}
		fx0, fy0 := rot(x0, y0)
		fx1, fy1 := rot(x1, y1)
		fastLine(screen, fx0, fy0, fx1, fy1,
			float32(math.Min(0.05/d0*430, 5)), colRoadLine, vis*0.7)
	}
	for i := 0; i < 4; i++ { // the piano keys at the threshold
		a := -0.2 * float64(i)
		xl, yl, d, okl := proj(-city.RunwayWKm*0.35, a)
		xr, yr, _, okr := proj(city.RunwayWKm*0.35, a)
		if okl && okr {
			fxl, fyl := rot(xl, yl)
			fxr, fyr := rot(xr, yr)
			fastLine(screen, fxl, fyl, fxr, fyr,
				float32(math.Min(0.07/d*430, 6)), colRoadLine, vis*0.8)
		}
	}
	// edge lamps — the brightest thing on the ground
	for _, lp := range p.Lamps {
		x, y, d, ok := proj(lp.Lat, lp.Ahead)
		if !ok {
			continue
		}
		fx, fy := rot(x, y)
		r := float32(math.Min(0.6+2.4/d, 3))
		fastDot(screen, fx, fy, r, colLampGlow, vis*0.6+vis*0.35*night)
	}

	// --- the districts, far to near (the generator sorts by Ahead)
	amb := 0.2 + 0.6*day
	for i := len(p.Buildings) - 1; i >= 0; i-- {
		b := &p.Buildings[i]
		// one cheap projection first; everything else only if it pays.
		// Lots nearer than ~700 m are passing the camera — their walls
		// would smear across the whole frame.
		xRf, yF, dF, okF := proj(b.Lat, b.Ahead)
		if !okF || dF > 40 || dF < 0.7 {
			continue
		}
		xLf, _, _, okL := proj(b.Lat-b.W, b.Ahead)
		if !okL {
			continue
		}
		hF := b.H / dF * 430
		wpx := math.Abs(xRf - xLf)
		if wpx < 1.2 || hF < 1 {
			continue
		}
		hue := portHues[b.Hue]
		base := shade(hue, amb*b.Bright)

		// the box faces only where they read: roof and street-side face
		if dF < 22 && wpx > 3 {
			xRb, yB, dB, okB := proj(b.Lat, b.Ahead+b.D)
			xLb, _, _, okLb := proj(b.Lat-b.W, b.Ahead+b.D)
			if okB && okLb {
				hB := b.H / dB * 430
				rLfX, rLfY := rot(xLf, yF-hF)
				rRfX, rRfY := rot(xRf, yF-hF)
				rRbX, rRbY := rot(xRb, yB-hB)
				rLbX, rLbY := rot(xLb, yB-hB)
				// a roof is only a roof seen from above: when the camera
				// is below the roof plane the quad flips across the sky —
				// skip it and let the wall carry the silhouette
				if yF-hF > yB-hB+0.5 {
					fastQuad(screen, rLfX, rLfY, rRfX, rRfY, rRbX, rRbY, rLbX, rLbY,
						shade(base, 1.25), vis) // roof catches the sky
				}
				if b.Lat-b.W/2 < city.Runway2Lat/2 {
					gRfX, gRfY := rot(xRf, yF)
					gRbX, gRbY := rot(xRb, yB)
					fastQuad(screen, gRfX, gRfY, rRfX, rRfY, rRbX, rRbY, gRbX, gRbY,
						shade(base, 0.55), vis)
				} else {
					gLfX, gLfY := rot(xLf, yF)
					gLbX, gLbY := rot(xLb, yB)
					fastQuad(screen, gLfX, gLfY, rLfX, rLfY, rLbX, rLbY, gLbX, gLbY,
						shade(base, 0.55), vis)
				}
			}
		}
		fx, fy := rot(math.Min(xLf, xRf), yF)
		fastRect(screen, fx, fy-float32(hF), float32(wpx), float32(hF), base, vis)

		// archetype geometry: tall lots carry a setback tier, low ones a
		// warm shopfront strip
		if b.H > 0.3 && b.Seed%3 == 0 {
			inset := wpx * 0.18
			tier := hF * 0.45
			fastRect(screen, fx+float32(inset), fy-float32(hF+tier),
				float32(wpx-2*inset), float32(tier), shade(base, 0.9), vis)
		} else if b.H < 0.06 && dF < 8 {
			fastRect(screen, fx, fy-float32(hF*0.35), float32(wpx),
				float32(hF*0.35), color.RGBA{255, 190, 120, 255}, vis*0.5*lights)
		}
		fastRect(screen, fx, fy-float32(hF), float32(wpx), 1, colRoof, vis*0.35*(0.4+day))

		// windows: a hash of (lot, floor, bay), only where the screen can
		// resolve them — the near canyon, not the whole skyline
		if dF < 5 && hF > 10 {
			floors := int(b.H / city.FloorKm)
			rows := int(hF / 5)
			if rows > floors {
				rows = floors
			}
			if rows > 24 {
				rows = 24
			}
			cols := int(wpx / 5)
			if cols > 10 {
				cols = 10
			}
			for fl := 0; fl < rows; fl++ {
				for bay := 0; bay < cols; bay++ {
					h := (b.Seed ^ uint32(fl)*2654435761 ^ uint32(bay)*40503) >> 3
					if float64(h%256)/256 > b.Occ {
						continue
					}
					wx := fx + float32(wpx*(float64(bay)+0.5)/float64(cols))
					wy := fy - float32(hF*(float64(fl)+0.5)/float64(rows))
					fastRect(screen, wx, wy, 1.5, 1.5, colWindow, vis*0.9*lights)
				}
			}
		}
		// close in, people on the frontage — never on the landing roads
		if dF < 2.5 {
			n := int(b.Seed % 3)
			for k := 0; k < n; k++ {
				wl := b.Lat + 0.004 + 0.004*float64(k)
				wa := b.Ahead + b.D*float64(uint32(int(b.Seed)+k*17)%16)/16 +
					0.002*math.Sin(T*0.7+float64(k)+float64(b.Seed%31))
				px, py, pd, pok := proj(wl, wa)
				if !pok {
					continue
				}
				hx, hy2 := rot(px, py)
				fastRect(screen, hx, hy2-1, 1,
					float32(math.Min(0.0017/pd*430, 3)), colHuman, vis*0.8)
			}
		}
	}

	// --- traffic, town avenues only
	for si, st := range p.Streets {
		for c := 0; c < st.Cars; c++ {
			h := float64((uint32(si)*97 + uint32(c)*3559) % 1024)
			span := city.TownTo - city.TownFrom
			prog := math.Mod(h*0.013*span+T*0.11, span) + city.TownFrom
			dir := 1.0
			if c%2 == 1 {
				dir, prog = -1, -prog
			}
			x, y, d, ok := proj(st.Lat+0.01*dir, prog)
			if !ok || d > 10 {
				continue
			}
			fx, fy := rot(x, y)
			r := float32(math.Min(0.4+1.6/d, 2))
			fastDot(screen, fx, fy, r, colHeadl, vis*0.7)
			fastDot(screen, fx+r*2*float32(dir), fy, r*0.8, colTaill, vis*0.6)
		}
	}
}
