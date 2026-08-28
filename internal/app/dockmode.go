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
)

type dockState struct {
	stellar  int
	view     dockView
	offers   []mission.Def // today's rolled bar offers
	board    []mission.Def // everything eligible, dice not applied
	rolled   bool
	yardSel  int
	tradeSel int // cursor on the commodity board
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

// drawMissionChart is the quest computer's map: every known system as a
// dot, you in green, and the route to the first open posting drawn with
// its hop legs — how far the work actually is.
func (a *App) drawMissionChart(screen *ebiten.Image, curSys, destSys int,
	x0, y0, w, h float64) {
	vector.DrawFilledRect(screen, float32(x0), float32(y0), float32(w), float32(h),
		premul(color.RGBA{5, 7, 10, 255}, 0.7), false)
	vector.StrokeRect(screen, float32(x0), float32(y0), float32(w), float32(h), 1,
		premul(colRule, 0.9), false)
	ui.DrawText(screen, "CHART", x0+8, y0+6, 0.7)

	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64
	for _, s := range a.gal.Systems {
		minX, maxX = math.Min(minX, float64(s.X)), math.Max(maxX, float64(s.X))
		minY, maxY = math.Min(minY, float64(s.Y)), math.Max(maxY, float64(s.Y))
	}
	px := func(id int) (float32, float32, bool) {
		s := a.gal.Systems[id]
		if s == nil || maxX <= minX || maxY <= minY {
			return 0, 0, false
		}
		return float32(x0 + 14 + (float64(s.X)-minX)/(maxX-minX)*(w-28)),
			float32(y0 + 24 + (float64(s.Y)-minY)/(maxY-minY)*(h-40)), true
	}
	for id := range a.gal.Systems {
		if fx, fy, ok := px(id); ok {
			fastDot(screen, fx, fy, 1, color.RGBA{110, 130, 150, 255}, 0.6)
		}
	}
	if destSys >= 0 {
		if route := a.gal.Route(curSys, destSys); len(route) > 1 {
			for i := 0; i+1 < len(route); i++ {
				x1, y1, ok1 := px(route[i])
				x2, y2, ok2 := px(route[i+1])
				if ok1 && ok2 {
					fastLine(screen, x1, y1, x2, y2, 1.5, hudGreen, 0.8)
				}
			}
			ui.DrawText(screen, fmt.Sprintf("route: %d hops · %d fuel · %d days",
				len(route)-1, (len(route)-1)*jumpFuel, (len(route)-1)*jumpDays),
				x0+8, y0+h-18, 0.7)
		}
		if fx, fy, ok := px(destSys); ok {
			fastDot(screen, fx, fy, 3, color.RGBA{255, 157, 63, 255}, 0.95)
		}
	}
	if fx, fy, ok := px(curSys); ok {
		fastDot(screen, fx, fy, 3, hudGreen, 1)
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
		s.Controller = ai.ByName("rabies")
		s.P = p.P.Add(gmath.V(float64(90+50*i), float64(-60+40*i)))
	}
}

func (a *App) updateDock() {
	d := a.dock
	v := a.voy

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
				v.Credits -= hirePrice
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
			if v.Credits >= crewHire {
				v.Credits -= crewHire
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
		st := a.gal.Stellars[d.stellar]
		price := market.Price(st.System, d.stellar, d.tradeSel, v.Day, v.Events)
		qty := 1
		if ebiten.IsKeyPressed(ebiten.KeyShiftLeft) || ebiten.IsKeyPressed(ebiten.KeyShiftRight) {
			qty = 10
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEqual) {
			for i := 0; i < qty && v.Credits >= price && v.CargoTotal() < overstuffCap; i++ {
				v.Credits -= price
				v.Cargo[d.tradeSel]++
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyMinus) {
			for i := 0; i < qty && v.Cargo[d.tradeSel] > 0; i++ {
				v.Credits += price
				v.Cargo[d.tradeSel]--
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyT) {
			d.view = dockMain
		}
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
					v.Credits -= o.Price
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
				v.Credits -= cost
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
		case inpututil.IsKeyJustPressed(ebiten.KeyS):
			d.view = dockYard
			d.yardSel = a.Cfg.PlayerShipID - 1
		case ebiten.IsKeyPressed(ebiten.KeyR):
			// hold to refuel: the meters walk — jump fuel, lithium seed,
			// then the RCS bottles
			if v.Fuel < v.FuelMax && v.Credits >= fuelPrice {
				v.Fuel++
				v.Credits -= fuelPrice
			} else if v.Lithium < v.LiMax-0.1 && v.Credits >= liPrice {
				v.Lithium += 0.1
				v.Credits -= liPrice / 10
			} else if v.RCSFuel < v.RCSMax-0.2 && v.Credits >= 3 {
				v.RCSFuel += 0.4
				v.Credits -= 3
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyY):
			if cost := v.RepairCost(); cost > 0 && v.Credits >= cost {
				v.Credits -= cost
				v.Dmg = reentry.Damage{}
				a.Console.Notifyf("Repairs complete: %d cr", cost)
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyV):
			a.saveGame()
			a.Console.Notifyf("Berth save written — DED resumes here.")
		case inpututil.IsKeyJustPressed(ebiten.KeyG):
			a.Console.Notifyf("The gaming annex is packed. (Charisma tables: Phase 5.)")
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
	ui.DrawText(screen, fmt.Sprintf("credits %d · fuel %d/%d · li %.1f kg · rcs %.0f kg · hull %.0f%% · cargo %d/%d t · crew %d · escorts %d",
		v.Credits, v.Fuel, v.FuelMax, v.Lithium, v.RCSFuel, 100-v.Dmg.Hull, v.CargoTotal(), cargoCap, v.Crew, len(v.Escorts)), x, y+40, 0.8)
	if over := v.CargoTotal() - cargoCap; over > 0 {
		ui.DrawTextScaled(screen, fmt.Sprintf("OVERSTUFFED %d%% — the corridor charges by the ton", 100*v.CargoTotal()/cargoCap),
			x, y+56, 1, color.RGBA{255, 193, 77, 255}, 0.9)
	}

	switch d.view {
	case dockBar:
		ui.DrawText(screen, "SPACEPORT BAR — number takes a contract · H hire escort · F fire escort · C crew on · X crew off · B leave", x, y+90, 1)
		if len(d.offers) == 0 {
			ui.DrawText(screen, "Nobody here has work for you today. (The mission computer knows the odds — M from the concourse.)", x, y+120, 0.8)
		}
		yy := y + 120
		for i, def := range d.offers {
			pay := fmt.Sprintf("%d cr", def.Pay)
			if def.Pay == 0 {
				pay = "trust"
			}
			ui.DrawText(screen, fmt.Sprintf("%d. %-28s %10s", i+1, def.Name, pay), x, yy, 1)
			brief := def.QuickBrief
			if brief == "" {
				brief = def.Brief
			}
			if len(brief) > 96 {
				brief = brief[:96] + "…"
			}
			ui.DrawText(screen, "   "+brief, x, yy+16, 0.65)
			yy += 44
		}
		esc := a.Catalog.Get(a.escortFor(d.stellar))
		yy += 12
		ui.DrawText(screen, fmt.Sprintf("WING TABLE — a %s pilot drinks here: %d cr to hire, %d cr/day after",
			esc.Name, shipPrice(esc)/8, escortWage), x, yy, 0.9)
		ui.DrawText(screen, fmt.Sprintf("crew wages %d cr/day each · payroll walks every jump and landing", crewWage), x, yy+18, 0.7)
	case dockMissions:
		ui.DrawText(screen, "MISSION COMPUTER — postings for this port, filtered by your affiliations. M leaves.", x, y+90, 1)
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
			if i, ok := posted[def.ID]; ok {
				status = fmt.Sprintf("OPEN — press %d", i+1)
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
			ui.DrawText(screen, fmt.Sprintf("%-24s %9s %-8s %s",
				def.Name, pay, hops, status), x, yy, 1)
			yy += 22
		}
		yy += 12
		ui.DrawText(screen, "The bar rolls each posting's dice once per landing —", x, yy, 0.65)
		ui.DrawText(screen, "an OPEN slot is that percentage come up.", x, yy+16, 0.65)
		a.drawMissionChart(screen, st.System, chartDest, 620, y+120, 344, 330)
	case dockTrade:
		ui.DrawText(screen, "COMMODITY BOARD — Up/Down select · + buys · - sells (Shift ×10) · T leaves", x, y+90, 1)
		ui.DrawText(screen, fmt.Sprintf("hold %d/%d t (clamps take %d) · prices differ by station — haul low to high",
			v.CargoTotal(), cargoCap, overstuffCap), x, y+110, 0.7)
		yy := y + 140
		ui.DrawText(screen, "   COMMODITY        PRICE  TREND   ABOARD", x, yy, 0.7)
		yy += 22
		for i, cm := range market.Commodities {
			price := market.Price(st.System, d.stellar, i, v.Day, v.Events)
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
				vector.DrawFilledRect(screen, float32(x-8), float32(yy-2), 520, 18,
					premul(color.RGBA{29, 58, 80, 255}, 0.6), false)
			}
			ui.DrawText(screen, fmt.Sprintf("%s%-14s %6d cr %-11s %4d t",
				map[bool]string{true: "> ", false: "  "}[i == d.tradeSel],
				cm.Name, price, trend, v.Cargo[i]), x, yy, sel)
			yy += 20
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
	case dockOutfit:
		ui.DrawText(screen, "OUTFITTER — the power grid is the ship. Number buys, O leaves.", x, y+90, 1)
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
		ui.DrawText(screen, "SHIPYARD — Up/Down browse, Enter buys (60% trade-in), S leaves.", x, y+90, 1)
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
		opts := []string{
			"B  Spaceport Bar    — contracts, escorts and crew",
			"M  Mission Computer — every posting, and today's odds",
			"R  Refuel (hold)    — jump fuel, then lithium",
			"T  Trade Center     — the commodity spreadsheet",
			"O  Outfitter        — generators, batteries, capacitors, radiators",
			"S  Shipyard         — change your hull",
			fmt.Sprintf("Y  Yard repairs     — %d cr outstanding", v.RepairCost()),
			"V  Save berth       — write the pilot file",
			"G  Gaming Annex     — queue-jumping charisma",
			"L  Leave            — launch to orbit",
		}
		for i, o := range opts {
			ui.DrawText(screen, o, x, y+100+float64(i)*24, 1)
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
