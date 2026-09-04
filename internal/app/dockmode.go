package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/assets"
	"yodacon.org/gonex/internal/ai"
	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/market"
	"yodacon.org/gonex/internal/mission"
	"yodacon.org/gonex/internal/power"
	"yodacon.org/gonex/internal/reentry"
	"yodacon.org/gonex/internal/ship"
	"yodacon.org/gonex/internal/ui"
	"yodacon.org/gonex/internal/world"
)

// The landed spaceport screen: everything a docked pilot can do, one
// keypress each. The bar is where the 1997 mission machine surfaces the
// contracts that actually walked in today; the mission computer is the
// terminal that lists everything posted for this stellar and your
// affiliations, dice and all.

type dockView int

const (
	dockMain dockView = iota
	dockBar
	dockMissions
	dockTrade
	dockOutfit
	dockYard
	dockJournal
	dockGovern
)

type dockState struct {
	stellar  int
	view     dockView
	offers   []mission.Def // today's rolled bar offers
	board    []mission.Def // everything eligible, dice not applied
	rolled   bool
	yardSel  int
	tradeSel int // cursor on the commodity board
	gov      governState
}

const (
	cargoCap = 100 // tons of deck cargo the Yodacon spares for trade
	// the clamps will take 20% past the book figure — an overstuffed hold
	// is legal at the dock and charged for by the atmosphere: every ton
	// rides the corridor down as entry mass, and past 100% the LOAD dial
	// sits in the red and the stick goes heavy
	overstuffCap = cargoCap * 6 / 5
	maxEscorts   = 4
)

// shipPrice is the yard's bluebook: a hull appraised off its flight spec.
func shipPrice(s *ship.Ship) int {
	return 12000 + int(s.Mass*45+s.MaxVelocity*25+s.Acceleration*30) +
		s.Damage*700
}

// missionDest resolves a posting's concrete destination stellar, or -1 for
// local / random-govt codes.
func missionDest(d mission.Def) int {
	if d.TravelStel >= 128 && d.TravelStel < 10000 {
		return d.TravelStel
	}
	if d.ReturnStel >= 128 && d.ReturnStel < 10000 {
		return d.ReturnStel
	}
	return -1
}

// chartStarTint colors a system's star and its name label by government,
// the Ares way: your own lane operator glows phosphor, the Confederation
// reads cool cyan-white, everyone else falls on a warm hash — so the map
// answers "whose sky is that" before you can read a single name.
func chartStarTint(govt string) (star, name color.RGBA) {
	switch govt {
	case "Consolidated Express":
		return color.RGBA{160, 255, 160, 255}, color.RGBA{120, 230, 120, 255}
	case "Confederation":
		return color.RGBA{170, 210, 255, 255}, color.RGBA{110, 200, 230, 255}
	case "":
		return color.RGBA{200, 200, 190, 255}, color.RGBA{140, 140, 130, 255}
	}
	h := hash31(len(govt), int(govt[0]))
	pal := [3]struct{ star, name color.RGBA }{
		{color.RGBA{255, 190, 120, 255}, color.RGBA{235, 170, 90, 255}},
		{color.RGBA{255, 140, 130, 255}, color.RGBA{230, 110, 100, 255}},
		{color.RGBA{225, 170, 255, 255}, color.RGBA{195, 140, 230, 255}},
	}
	p := pal[h%3]
	return p.star, p.name
}

// drawMissionChart is the quest computer's map, in the Ares grammar: a
// dotted grid over a speckled void, every system a glowing tinted star,
// names beside the stars you can actually reach soon, the route as a
// dotted trace, and a chart-table crosshair boxed on where you stand.
func (a *App) drawMissionChart(screen *ebiten.Image, curSys, destSys int,
	x0, y0, w, h float64) {
	vector.DrawFilledRect(screen, float32(x0), float32(y0), float32(w), float32(h),
		color.RGBA{3, 5, 7, 250}, false)
	vector.StrokeRect(screen, float32(x0), float32(y0), float32(w), float32(h), 1,
		premul(colRule, 0.9), false)
	// the speckled void
	for i := 0; i < 90; i++ {
		hsh := hash31(i, 77)
		sx := x0 + 4 + float64(hsh%1000)/1000*(w-8)
		sy := y0 + 16 + float64((hsh>>10)%1000)/1000*(h-24)
		fastDot(screen, float32(sx), float32(sy), 0.8,
			color.RGBA{200, 205, 215, 255}, 0.12+float64(hsh%40)/200)
	}
	// the dotted grid
	gridC := color.RGBA{46, 105, 58, 255}
	for gx := x0 + w/8; gx < x0+w-4; gx += w / 8 {
		for gy := y0 + 18; gy < y0+h-4; gy += 7 {
			fastDot(screen, float32(gx), float32(gy), 0.6, gridC, 0.5)
		}
	}
	for gy := y0 + 18 + (h-18)/6; gy < y0+h-4; gy += (h - 18) / 6 {
		for gx := x0 + 4; gx < x0+w-4; gx += 7 {
			fastDot(screen, float32(gx), float32(gy), 0.6, gridC, 0.5)
		}
	}
	ui.DrawText(screen, "MISSION ANALYSIS", x0+8, y0+5, 0.7)

	px := a.chartProjector(x0, y0, w, h)

	// hop distances from here, one BFS — names go only on the near sky
	dist := map[int]int{curSys: 0}
	queue := []int{curSys}
	for qi := 0; qi < len(queue); qi++ {
		id := queue[qi]
		if s := a.gal.Systems[id]; s != nil {
			for _, l := range s.Links {
				if _, seen := dist[l]; !seen {
					dist[l] = dist[id] + 1
					queue = append(queue, l)
				}
			}
		}
	}
	onRoute := map[int]bool{}
	var route []int
	if destSys >= 0 {
		route = a.gal.Route(curSys, destSys)
		for _, id := range route {
			onRoute[id] = true
		}
	}
	// the stars
	for id, s := range a.gal.Systems {
		fx, fy, ok := px(id)
		if !ok {
			continue
		}
		star, nameC := chartStarTint(s.Govt)
		glowDot(screen, fx, fy, 9, star, 0.85)
		fastDot(screen, fx, fy, 2, star, 0.9)
		fastDot(screen, fx, fy, 0.9, color.RGBA{255, 255, 252, 255}, 1)
		if d, near := dist[id]; (near && d <= 2) || onRoute[id] || id == destSys {
			al := float32(0.9)
			if d, ok := dist[id]; ok && d > 2 && !onRoute[id] {
				al = 0.65
			}
			if id == destSys {
				nameC = color.RGBA{255, 157, 63, 255}
			}
			if id == curSys {
				nameC = colChrome
			}
			// stagger labels above/below by hash so the dense core reads
			ly := float64(fy) - 11
			if hash31(id, 3)%2 == 0 {
				ly = float64(fy) + 3
			}
			ui.DrawTextScaled(screen, s.Name, float64(fx)+6, ly, 0.8, nameC, al)
		}
	}
	// the route: a dotted trace, Ares boundary-style
	for i := 0; i+1 < len(route); i++ {
		x1, y1, ok1 := px(route[i])
		x2, y2, ok2 := px(route[i+1])
		if !ok1 || !ok2 {
			continue
		}
		dx, dy := float64(x2-x1), float64(y2-y1)
		l := math.Hypot(dx, dy)
		for t := 4.0; t < l; t += 8 {
			fastDot(screen, x1+float32(dx*t/l), y1+float32(dy*t/l), 1,
				color.RGBA{255, 244, 180, 255}, 0.85)
		}
	}
	if len(route) > 1 {
		ui.DrawText(screen, fmt.Sprintf("route: %d hops · %d fuel · %d days",
			len(route)-1, (len(route)-1)*jumpFuel, (len(route)-1)*jumpDays),
			x0+8, y0+h-18, 0.7)
	}
	if fx, fy, ok := px(destSys); ok && destSys >= 0 {
		glowDot(screen, fx, fy, 10, color.RGBA{255, 157, 63, 255}, 0.8)
	}
	// the crosshair box on the current system
	if fx, fy, ok := px(curSys); ok {
		ch := colChrome
		vector.StrokeRect(screen, fx-7, fy-7, 14, 14, 1, ch, false)
		fastLine(screen, float32(x0+4), fy, fx-10, fy, 1, ch, 0.4)
		fastLine(screen, fx+10, fy, float32(x0+w-4), fy, 1, ch, 0.4)
		fastLine(screen, fx, float32(y0+18), fx, fy-10, 1, ch, 0.4)
		fastLine(screen, fx, fy+10, fx, float32(y0+h-4), 1, ch, 0.4)
	}
}

// escortFor picks the hull the local wing sells — one type per port.
func (a *App) escortFor(stellar int) int {
	return 1 + stellar%a.Catalog.Count()
}

// rollMissions runs the daily dice once per landing, for both the bar and
// the mission computer.
func (d *dockState) rollMissions(a *App) {
	if d.rolled {
		return
	}
	v := a.voy
	d.offers = a.msn.OffersAt(d.stellar, &v.Bits, v.Active, v.Rng)
	d.board = a.msn.BoardAt(d.stellar, &v.Bits, v.Active)
	d.rolled = true
}

// acceptOffer takes offer i off today's list.
func (a *App) acceptOffer(i int) {
	d, v := a.dock, a.voy
	if i < 0 || i >= len(d.offers) {
		return
	}
	def := d.offers[i]
	act := mission.Accept(def, d.stellar, confederationStellars(a.gal), v.Rng)
	v.Active = append(v.Active, act)
	a.Console.Notifyf("Accepted: %s", def.Name)
	d.offers = append(d.offers[:i], d.offers[i+1:]...)
}

// launchEscorts replaces the flight world's escort wing with the payroll's.
func (a *App) launchEscorts() {
	w := a.World
	p := w.MainPlayer
	if p == nil {
		return
	}
	live := w.Entities[:0]
	for _, e := range w.Entities {
		if s, ok := e.(*world.Ship); ok && s.Kind == world.KindNPC && s.Escort {
			continue
		}
		live = append(live, e)
	}
	w.Entities = live
	for i, id := range a.voy.Escorts {
		s := w.NewShip(id, p.Team, fmt.Sprintf("Escort %d", i+1), world.KindNPC)
		s.Escort = true
		s.Controller = ai.Parse("escort", w.Rand) // hired guns roam with the player
		s.P = p.P.Add(gmath.V(float64(90+50*i), float64(-60+40*i)))
	}
}

// dockBack returns from any concourse sub-screen to the concourse. It is
// what Escape does while docked, and Backspace too; it reports whether there
// was anything to go back from, so the caller can fall through to the game
// menu when there was not.
func (a *App) dockBack() bool {
	d := a.dock
	if a.mode != modeLanded || d == nil || d.view == dockMain {
		return false
	}
	if d.view == dockGovern && d.gov.menu {
		d.gov.menu = false // one level at a time: the build menu closes first
		return true
	}
	d.view = dockMain
	return true
}

func (a *App) updateDock() {
	d := a.dock
	v := a.voy

	// Every sub-screen has its own letter home (O leaves the outfitter, S
	// the yard, T the board) and none of them was obvious. Backspace is the
	// one key that always means "back"; Escape does the same from Update.
	if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && a.dockBack() {
		a.drainNotices()
		return
	}

	// shore power: on the pad the bus idles, so the reactor's whole surplus
	// walks the caps and battery back up while the pad loop takes the heat.
	if v.Grid != nil {
		v.Grid.Step(dt, power.Load{Vacuum: true})
	}

	// the scripted pilot admires the 1997 view for a beat, then wraps
	if a.demoStellar > 0 {
		if a.demoHold += dt; a.demoHold > 3.2 {
			a.quitting = true
		}
	}

	switch d.view {
	case dockBar:
		d.rollMissions(a)
		for i := range d.offers {
			if i < 9 && inpututil.IsKeyJustPressed(ebiten.KeyDigit1+ebiten.Key(i)) {
				a.acceptOffer(i)
				break
			}
		}
		esc := a.escortFor(d.stellar)
		hirePrice := shipPrice(a.Catalog.Get(esc)) / 8
		switch {
		case inpututil.IsKeyJustPressed(ebiten.KeyH):
			switch {
			case len(v.Escorts) >= maxEscorts:
				a.Console.Notifyf("Your wing is full (%d).", maxEscorts)
			case v.Credits < hirePrice:
				a.Console.Notifyf("The %s pilot wants %d cr up front.",
					a.Catalog.Get(esc).Name, hirePrice)
			default:
				a.payPort(d.stellar, hirePrice)
				v.Escorts = append(v.Escorts, esc)
				a.Console.Notifyf("Hired a %s escort — %d cr/day on the payroll.",
					a.Catalog.Get(esc).Name, escortWage)
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyF):
			if n := len(v.Escorts); n > 0 {
				a.Console.Notifyf("Paid off the %s escort.",
					a.Catalog.Get(v.Escorts[n-1]).Name)
				v.Escorts = v.Escorts[:n-1]
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyC):
			if a.payPort(d.stellar, crewHire) {
				v.Crew++
				a.Console.Notifyf("Signed a deckhand — crew %d, %d cr/day each.",
					v.Crew, crewWage)
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyX):
			if v.Crew > 0 {
				v.Crew--
				a.Console.Notifyf("Dismissed a deckhand — crew %d.", v.Crew)
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyB):
			d.view = dockMain
		}
	case dockMissions:
		d.rollMissions(a)
		for i := range d.offers {
			if i < 9 && inpututil.IsKeyJustPressed(ebiten.KeyDigit1+ebiten.Key(i)) {
				a.acceptOffer(i)
				break
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyM) {
			d.view = dockMain
		}
	case dockTrade:
		n := len(market.Commodities)
		if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
			d.tradeSel = (d.tradeSel + 1) % n
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
			d.tradeSel = (d.tradeSel + n - 1) % n
		}
		qty := 1
		if ebiten.IsKeyPressed(ebiten.KeyShiftLeft) || ebiten.IsKeyPressed(ebiten.KeyShiftRight) {
			qty = 10
		}
		// The counter buys and sells against the port's REAL warehouse, so a
		// world can be sold out and a big sale genuinely supplies it. See
		// counter.go — the tons and the credits move together, or neither
		// moves.
		if inpututil.IsKeyJustPressed(ebiten.KeyEqual) {
			if got := a.buyTons(d.stellar, d.tradeSel, qty); got < qty {
				if a.onHand(d.stellar, d.tradeSel) <= 0 {
					a.Console.Notifyf("%s is sold out of %s.",
						a.gal.Stellars[d.stellar].Name, market.Commodities[d.tradeSel].Name)
				}
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyMinus) {
			a.sellTons(d.stellar, d.tradeSel, qty)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyT) {
			d.view = dockMain
		}
	case dockJournal:
		if inpututil.IsKeyJustPressed(ebiten.KeyJ) || inpututil.IsKeyJustPressed(ebiten.KeyM) {
			d.view = dockMain
		}
	case dockGovern:
		a.updateGovern()
	case dockOutfit:
		// the stated goal of getting rich is really the goal of building a
		// grid that survives richer contracts — but every tonne bought here
		// is charged for by the atmosphere on the way back down.
		for i, o := range power.Catalog() {
			if inpututil.IsKeyJustPressed(ebiten.KeyDigit1 + ebiten.Key(i)) {
				switch {
				case v.Grid == nil:
				case v.Credits < o.Price:
					a.Console.Notifyf("Not enough credits for the %s.", o.Name)
				default:
					a.payPort(d.stellar, o.Price)
					v.Grid.Buy(o)
					a.Console.Notifyf("Fitted: %s (+%.0f t). Entries just got hotter — mind the corridor.",
						o.Name, o.Kg/1000)
				}
				break
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyO) {
			d.view = dockMain
		}
	case dockYard:
		n := a.Catalog.Count()
		if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
			d.yardSel = (d.yardSel + 1) % n
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
			d.yardSel = (d.yardSel + n - 1) % n
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
			id := d.yardSel + 1
			cur := a.Cfg.PlayerShipID
			cost := shipPrice(a.Catalog.Get(id)) - shipPrice(a.Catalog.Get(cur))*6/10
			switch {
			case id == cur:
				a.Console.Notifyf("You already fly the %s.", a.Catalog.Get(id).Name)
			case v.Credits < cost:
				a.Console.Notifyf("The %s runs %d cr after trade-in — you're short.",
					a.Catalog.Get(id).Name, cost)
			default:
				if cost >= 0 {
					a.payPort(d.stellar, cost)
				} else {
					a.portPays(d.stellar, -cost)
				}
				a.Cfg.PlayerShipID = id
				if p := a.World.MainPlayer; p != nil {
					p.ShipID = id
				}
				a.Console.Notifyf("The yard swaps your berth: %s, %d cr after trade-in.",
					a.Catalog.Get(id).Name, cost)
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyS) {
			d.view = dockMain
		}
	default:
		switch {
		case inpututil.IsKeyJustPressed(ebiten.KeyB):
			d.view = dockBar
		case inpututil.IsKeyJustPressed(ebiten.KeyM):
			d.view = dockMissions
		case inpututil.IsKeyJustPressed(ebiten.KeyT):
			d.view = dockTrade
		case inpututil.IsKeyJustPressed(ebiten.KeyO):
			d.view = dockOutfit
		case inpututil.IsKeyJustPressed(ebiten.KeyJ):
			d.view = dockJournal
		case inpututil.IsKeyJustPressed(ebiten.KeyS):
			d.view = dockYard
			d.yardSel = a.Cfg.PlayerShipID - 1
		case ebiten.IsKeyPressed(ebiten.KeyR):
			// hold to refuel: the meters walk — jump fuel, lithium seed,
			// then the RCS bottles
			if v.Fuel < v.FuelMax && a.payPort(d.stellar, fuelPrice) {
				v.Fuel++
			} else if v.Lithium < v.LiMax-0.1 && a.payPort(d.stellar, liPrice/10) {
				v.Lithium += 0.1
			} else if v.RCSFuel < v.RCSMax-0.2 && a.payPort(d.stellar, 3) {
				v.RCSFuel += 0.4
			} else if p := a.World.MainPlayer; p != nil && p.Rounds < p.RoundsMax &&
				v.Credits >= world.RoundCr*4 && a.armouryHas(d.stellar, 4) {
				// the armoury: the last meter on the walk, same price the
				// AI's planets pay at their own pads — and the same shelf
				a.payPort(d.stellar, world.RoundCr*4)
				a.armouryDraw(d.stellar, 4)
				p.Rounds = min(p.Rounds+4, p.RoundsMax)
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyY):
			if cost := v.RepairCost(); cost > 0 && a.payPort(d.stellar, cost) {
				v.Dmg = reentry.Damage{}
				a.Console.Notifyf("Repairs complete: %d cr", cost)
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyV):
			a.saveGame()
			a.Console.Notifyf("Berth save written — DED resumes here.")
		case inpututil.IsKeyJustPressed(ebiten.KeyG):
			d.view = dockGovern
			a.openGovern()
		case inpututil.IsKeyJustPressed(ebiten.KeyL):
			st := d.stellar
			a.dock = nil
			a.startTakeoff(st) // the runway, then the sky, then the lanes
		}
	}
	a.drainNotices()
}

func (a *App) drawDock(screen *ebiten.Image) {
	d := a.dock
	v := a.voy
	st := a.gal.Stellars[d.stellar]
	sys := a.gal.Systems[st.System]

	vector.DrawFilledRect(screen, 0, 0, ScreenW, ScreenH, color.RGBA{5, 7, 10, 255}, false)

	// the 1997 landing view — "their backgrounds will amaze you"
	if st.LandPic > 0 {
		if pic, err := assets.Image(fmt.Sprintf("data/conex/land/%d.png", st.LandPic)); err == nil {
			b := pic.Bounds()
			op := &ebiten.DrawImageOptions{}
			scale := float64(ScreenW) / float64(b.Dx())
			op.GeoM.Scale(scale, scale)
			op.ColorScale.ScaleAlpha(0.5)
			screen.DrawImage(pic, op)
			vector.DrawFilledRect(screen, 0, 0, ScreenW, ScreenH,
				premul(color.RGBA{5, 7, 10, 255}, 0.35), false)
		}
	}

	// the ship on the pad
	yard := a.Catalog.Get(a.Cfg.PlayerShipID).Yard
	b := yard.Bounds()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(2, 2)
	op.GeoM.Translate(float64(ScreenW)-float64(b.Dx())*2-60, 90)
	screen.DrawImage(yard, op)

	x, y := 60.0, 70.0
	ui.DrawText(screen, fmt.Sprintf("%s — %s system", st.Name, sys.Name), x, y, 1)
	ui.DrawText(screen, fmt.Sprintf("%s · tech %d · day %d", st.Govt, st.Tech, v.Day), x, y+20, 0.8)
	ui.DrawText(screen, fmt.Sprintf("credits %d · crew %d · escorts %d",
		v.Credits, v.Crew, len(v.Escorts)), x, y+40, 0.8)
	if over := v.CargoTotal() - cargoCap; over > 0 {
		ui.DrawTextScaled(screen, fmt.Sprintf("OVERSTUFFED %d%% — the corridor charges by the ton", 100*v.CargoTotal()/cargoCap),
			x, y+56, 1, color.RGBA{255, 193, 77, 255}, 0.9)
	}

	// the ship's state as the Ares sidebar rail: bezelled tanks under the
	// pad view, filling from the bottom, one glance for the whole vessel
	battFrac := 1.0
	if v.Grid != nil {
		battFrac = v.Grid.BattFrac()
	}
	cargoC := color.RGBA{206, 196, 122, 255}
	if v.CargoTotal() > cargoCap {
		cargoC = color.RGBA{212, 116, 110, 255}
	}
	gauges := []struct {
		frac       float64
		c          color.RGBA
		label, val string
	}{
		{float64(v.Fuel) / math.Max(float64(v.FuelMax), 1), color.RGBA{96, 148, 255, 255}, "FUEL", fmt.Sprintf("%d", v.Fuel)},
		{(100 - v.Dmg.Hull) / 100, color.RGBA{110, 200, 110, 255}, "HULL", fmt.Sprintf("%.0f%%", 100-v.Dmg.Hull)},
		{v.Lithium / math.Max(v.LiMax, 1), color.RGBA{255, 92, 108, 255}, "LI", fmt.Sprintf("%.0fkg", v.Lithium)},
		{v.RCSFuel / math.Max(v.RCSMax, 1), color.RGBA{255, 190, 96, 255}, "RCS", fmt.Sprintf("%.0fkg", v.RCSFuel)},
		{battFrac, color.RGBA{86, 220, 226, 255}, "BATT", fmt.Sprintf("%.0f%%", battFrac*100)},
		{float64(v.CargoTotal()) / float64(overstuffCap), cargoC, "HOLD", fmt.Sprintf("%dt", v.CargoTotal())},
	}
	gx := float64(ScreenW) - 60 - float64(len(gauges))*44
	for i, g := range gauges {
		ui.VGauge(screen, gx+float64(i)*44, 356, 22, 130, g.frac, g.c, g.label, g.val)
	}

	// The way home, on every sub-screen, in the same place: a green Ares
	// keycap at the foot. The desk carries its own in the keycap grid.
	if d.view != dockMain && d.view != dockGovern {
		ui.Keycap(screen, x, float64(ScreenH)-46, 330, "ESC", "Back to the concourse", ui.ToneGreen, true)
	}

	switch d.view {
	case dockBar:
		ui.DrawText(screen, "SPACEPORT BAR — a keypress takes a contract", x, y+90, 1)
		yy := y + 116
		if len(d.offers) == 0 {
			ui.DrawText(screen, "Nobody here has work for you today. (The mission computer knows the odds — M from the concourse.)", x, yy, 0.8)
			yy += 28
		}
		for i, def := range d.offers {
			pay := fmt.Sprintf("%d cr", def.Pay)
			if def.Pay == 0 {
				pay = "on trust"
			}
			// each open contract is a green Ares keycap, its brief beneath
			ui.Keycap(screen, x, yy, 520, fmt.Sprintf("%d", i+1),
				fmt.Sprintf("%s — %s", def.Name, pay), ui.ToneGreen, false)
			brief := def.QuickBrief
			if brief == "" {
				brief = def.Brief
			}
			if len(brief) > 96 {
				brief = brief[:96] + "…"
			}
			ui.DrawText(screen, brief, x+12, yy+26, 0.65)
			yy += 52
		}
		esc := a.Catalog.Get(a.escortFor(d.stellar))
		yy += 10
		ui.DrawText(screen, fmt.Sprintf("WING TABLE — a %s pilot drinks here: %d cr to hire, %d cr/day after",
			esc.Name, shipPrice(esc)/8, escortWage), x, yy, 0.9)
		ui.DrawText(screen, fmt.Sprintf("crew wages %d cr/day each · payroll walks every jump and landing", crewWage), x, yy+18, 0.7)
		by := yy + 44
		ui.Keycap(screen, x, by, 176, "H", "Hire wing", ui.ToneKhaki, false)
		ui.Keycap(screen, x+186, by, 176, "F", "Fire wing", ui.ToneKhaki, false)
		ui.Keycap(screen, x+372, by, 176, "C", "Crew on", ui.ToneKhaki, false)
		ui.Keycap(screen, x+558, by, 176, "X", "Crew off", ui.ToneKhaki, false)
		ui.Keycap(screen, x+744, by, 176, "B", "Leave", ui.ToneGreen, true)
	case dockMissions:
		ui.DrawText(screen, "MISSION COMPUTER — postings for this port, filtered by your affiliations. M or Esc returns.", x, y+90, 1)
		if len(d.board) == 0 {
			ui.DrawText(screen, "No postings match this port and your record.", x, y+120, 0.8)
		}
		posted := map[int]int{} // def ID -> offer index
		for i, def := range d.offers {
			posted[def.ID] = i
		}
		yy := y + 120
		chartDest := -1
		for _, def := range d.board {
			status := fmt.Sprintf("no slot (%d%%/day)", def.AvailRandom)
			open := false
			if i, ok := posted[def.ID]; ok {
				status = fmt.Sprintf("OPEN — press %d", i+1)
				open = true
			}
			pay := fmt.Sprintf("%d cr", def.Pay)
			if def.Pay == 0 {
				pay = "trust"
			}
			hops := "here"
			if dest := missionDest(def); dest >= 0 {
				if ds := a.gal.Stellars[dest]; ds != nil {
					if r := a.gal.Route(st.System, ds.System); r != nil {
						hops = fmt.Sprintf("%d hops", len(r)-1)
						if _, open := posted[def.ID]; open && chartDest < 0 {
							chartDest = ds.System
						}
					}
				}
			} else if def.TravelStel >= 10000 {
				hops = "sealed"
			}
			rowC := color.RGBA{223, 228, 230, 255}
			if open {
				rowC = color.RGBA{140, 240, 140, 255}
			}
			ui.DrawTextScaled(screen, fmt.Sprintf("%-24s %9s %-8s %s",
				def.Name, pay, hops, status), x, yy, 1, rowC, 1)
			yy += 22
		}
		yy += 12
		ui.DrawText(screen, "The bar rolls each posting's dice once per landing —", x, yy, 0.65)
		ui.DrawText(screen, "an OPEN slot is that percentage come up.", x, yy+16, 0.65)
		a.drawMissionChart(screen, st.System, chartDest, 620, y+120, 344, 330)
	case dockTrade:
		ui.DrawText(screen, "COMMODITY BOARD — Up/Down select · + buys · - sells (Shift ×10) · T or Esc returns", x, y+90, 1)
		ui.DrawText(screen, fmt.Sprintf("hold %d/%d t (clamps take %d) · prices differ by station — haul low to high",
			v.CargoTotal(), cargoCap, overstuffCap), x, y+110, 0.7)
		yy := y + 140
		ui.DrawText(screen, "   COMMODITY        PRICE  TREND        IN PORT  ABOARD", x, yy, 0.7)
		yy += 22
		for i, cm := range market.Commodities {
			price := a.shopPrice(d.stellar, i)
			trend := "  ·"
			switch market.Trend(st.System, d.stellar, i, v.Day, v.Events) {
			case 1:
				trend = "  ^ rising"
			case -1:
				trend = "  v falling"
			}
			sel := float32(0.75)
			if i == d.tradeSel {
				sel = 1
				vector.DrawFilledRect(screen, float32(x-8), float32(yy-2), 620, 18,
					premul(color.RGBA{29, 58, 80, 255}, 0.6), false)
			}
			ui.DrawText(screen, fmt.Sprintf("%s%-14s %6d cr %-11s %s %5d t",
				map[bool]string{true: "> ", false: "  "}[i == d.tradeSel],
				cm.Name, price, trend, a.tradeLine(d.stellar, i), v.Cargo[i]), x, yy, sel)
			yy += 20
		}
		if uw := a.uniWorld(d.stellar); uw != nil {
			yy += 6
			ui.DrawText(screen, fmt.Sprintf("  %s makes %s — its own goods sell cheap here, and what it",
				uw.Name, uw.Speciality()), x, yy, 0.65)
			ui.DrawText(screen, "  cannot make, it pays for. IN PORT is a real warehouse: it runs out.",
				x, yy+15, 0.65)
			yy += 24
		}
		yy += 14
		ui.DrawText(screen, "WORLD EVENTS ON THE WIRE:", x, yy, 0.8)
		yy += 20
		if len(v.Events) == 0 {
			ui.DrawText(screen, "  a quiet market — the spreads are all geography today", x, yy, 0.65)
		}
		for _, ev := range v.Events {
			where := "everywhere"
			if ev.System >= 0 {
				if s := a.gal.Systems[ev.System]; s != nil {
					where = s.Name
				}
			}
			dir := fmt.Sprintf("×%.1f", ev.Mult)
			ui.DrawText(screen, fmt.Sprintf("  %s — %s %s (%s) · %d days left",
				ev.Name, market.Commodities[ev.Commodity].Name, dir, where, ev.DaysLeft), x, yy, 0.7)
			yy += 18
		}
	case dockJournal:
		a.drawDockJournal(screen, x, y)
	case dockGovern:
		a.drawGovern(screen, x, y)
	case dockOutfit:
		ui.DrawText(screen, "OUTFITTER — the power grid is the ship. Number buys; O or Esc returns to the concourse.", x, y+90, 1)
		if g := v.Grid; g != nil {
			ui.DrawText(screen, fmt.Sprintf(
				"plant: reactor %.1f MW · battery %.0f MJ · caps %.0f MJ · radiators %.1f MW · heat ceiling %.0f MJ",
				g.ReactorMW, g.BattCapMJ, g.CapCapMJ, g.RadiatorMW, g.HeatCapMJ), x, y+114, 0.75)
			ui.DrawText(screen, fmt.Sprintf(
				"outfit mass +%.0f t — every tonne raises the ballistic coefficient on entry", g.OutfitKg/1000),
				x, y+132, 0.75)
		}
		yy := y + 162
		for i, o := range power.Catalog() {
			ui.DrawText(screen, fmt.Sprintf("%d. %-22s %-28s %7d cr  +%3.0f t",
				i+1, o.Name, o.Desc, o.Price, o.Kg/1000), x, yy, 1)
			yy += 24
		}
	case dockYard:
		ui.DrawText(screen, "SHIPYARD — Up/Down browse, Enter buys (60% trade-in); S or Esc returns.", x, y+90, 1)
		n := a.Catalog.Count()
		cur := a.Cfg.PlayerShipID
		curVal := shipPrice(a.Catalog.Get(cur)) * 6 / 10
		// a windowed list so all 21 hulls fit
		first := d.yardSel - 6
		if first < 0 {
			first = 0
		}
		if first > n-13 {
			first = n - 13
		}
		if first < 0 {
			first = 0
		}
		yy := y + 120
		for i := first; i < n && i < first+13; i++ {
			id := i + 1
			s := a.Catalog.Get(id)
			mark := "  "
			if id == cur {
				mark = "* "
			}
			sel := float32(0.7)
			if i == d.yardSel {
				sel = 1
				vector.DrawFilledRect(screen, float32(x-8), float32(yy-2), 560, 18,
					premul(color.RGBA{29, 58, 80, 255}, 0.6), false)
			}
			ui.DrawText(screen, fmt.Sprintf("%s%-18s %8d cr   vel %3.0f · turn %3.0f · dmg %d",
				mark, s.Name, shipPrice(s), s.MaxVelocity, s.TurnSpeed, s.Damage), x, yy, sel)
			yy += 20
		}
		selShip := a.Catalog.Get(d.yardSel + 1)
		cost := shipPrice(selShip) - curVal
		ui.DrawText(screen, fmt.Sprintf("your %s trades for %d cr — the %s would cost %d cr net",
			a.Catalog.Get(cur).Name, curVal, selShip.Name, cost), x, yy+10, 0.8)
		if t := selShip.Target; t != nil {
			tb := t.Bounds()
			top := &ebiten.DrawImageOptions{}
			top.GeoM.Scale(2, 2)
			top.GeoM.Translate(float64(ScreenW)-float64(tb.Dx())*2-80, 420)
			screen.DrawImage(t, top)
		}
	default:
		// the concourse, in the Ares button grammar: raised keycap, label
		// bar, tones by intent — khaki for the port services, red for a
		// repair bill outstanding, gray for the paperwork, green to fly
		repTone, repLit := ui.ToneDim, false
		repLabel := "Yard repairs — nothing outstanding"
		if rc := v.RepairCost(); rc > 0 {
			repTone, repLit = ui.ToneRed, true
			repLabel = fmt.Sprintf("Yard repairs — %d cr outstanding", rc)
		}
		type opt struct {
			key, label string
			tone       ui.KeyTone
			lit        bool
		}
		opts := []opt{
			{"B", "Spaceport Bar — contracts, escorts, crew", ui.ToneKhaki, false},
			{"M", "Mission Computer — postings and odds", ui.ToneKhaki, false},
			{"R", "Refuel (hold) — jump fuel, then lithium", ui.ToneKhaki, false},
			{"T", "Trade Center — the commodity board", ui.ToneKhaki, false},
			{"O", "Outfitter — the power grid catalog", ui.ToneKhaki, false},
			{"J", "Trade Journal — lanes, arrivals, rivals", ui.ToneKhaki, false},
			{"S", "Shipyard — change your hull", ui.ToneKhaki, false},
			{"Y", repLabel, repTone, repLit},
			{"V", "Save berth — write the pilot file", ui.ToneGray, false},
			{"G", "Governor's Desk — the world's books, chart and orders", ui.ToneKhaki, false},
			{"L", "Leave — launch to orbit", ui.ToneGreen, true},
		}
		for i, o := range opts {
			ui.Keycap(screen, x, y+100+float64(i)*30, 470, o.key, o.label, o.tone, o.lit)
		}
		if n := len(v.Active); n > 0 {
			ui.DrawText(screen, fmt.Sprintf("ACTIVE MISSIONS (%d):", n), x, y+350, 0.9)
			for i, act := range v.Active {
				dest := "—"
				if s := a.gal.Stellars[act.Dest()]; s != nil {
					dest = fmt.Sprintf("%s (%s)", s.Name, a.gal.Systems[s.System].Name)
				}
				days := ""
				if act.DaysLeft >= 0 {
					days = fmt.Sprintf(" · %d days left", act.DaysLeft)
				}
				ui.DrawText(screen, fmt.Sprintf("  %s → %s%s", act.Def.Name, dest, days), x, y+372+float64(i)*18, 0.8)
			}
		}
	}
}
