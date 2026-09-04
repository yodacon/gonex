package app

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/console"
	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
	"yodacon.org/gonex/internal/ui"
	"yodacon.org/gonex/internal/universe"
	"yodacon.org/gonex/internal/world"
)

// The Governor's Desk: one screen for a world that is a market, a factory
// and a fortress. See docs/lab-reports/2026-09-04-governor-desk-screen.md.
//
// Four regions in the Ares grammar the dock already speaks. THE WORLD is a
// rail of tanks — population and whether it is fed, the treasury as days of
// imports, the magazine, Konquest's kill percentage made honest, and the
// garrison. THE CHART is the mission computer's map with five overlays:
// typed lanes, a rating badge on every world, hulls in transit with their
// ETA, wreck fields, and standing orders. THE BOOKS is the full material
// vector with cover and today's price against base — where a courier makes
// money selling to you, and what you should be shipping out. THE DESK is the
// keycaps: build, order, charter, relations, tariff. Every action is a Pay
// from a named purse, and every number on the screen is one the console can
// print from the same seed.
//
// The seat is earned: buy the first building at a world and the keycaps go
// live for it; until then, and everywhere else, the desk is read-only at the
// resolution your relation to the holder allows.

type governState struct {
	neigh []int // stellar IDs in the neighbourhood, this world first
	sel   int   // the cursor on neigh
	menu  bool  // the build menu is open
	msg   string
}

// openGovern builds the neighbourhood list when the desk is opened.
func (a *App) openGovern() {
	d := a.dock
	if d == nil || a.uni == nil || a.gal == nil {
		return
	}
	a.stepUniverse()
	here := a.gal.Stellars[d.stellar]
	if here == nil {
		return
	}
	// Systems within two jumps, one BFS.
	hops := map[int]int{here.System: 0}
	queue := []int{here.System}
	for qi := 0; qi < len(queue); qi++ {
		id := queue[qi]
		if hops[id] >= 2 {
			continue
		}
		if s := a.gal.Systems[id]; s != nil {
			for _, l := range s.Links {
				if _, seen := hops[l]; !seen {
					hops[l] = hops[id] + 1
					queue = append(queue, l)
				}
			}
		}
	}
	var neigh []int
	for _, id := range a.uni.Order() {
		w := a.uni.Worlds[id]
		if _, ok := hops[w.System]; ok {
			neigh = append(neigh, id)
		}
	}
	sort.SliceStable(neigh, func(i, j int) bool {
		wi, wj := a.uni.Worlds[neigh[i]], a.uni.Worlds[neigh[j]]
		if neigh[i] == d.stellar {
			return true
		}
		if neigh[j] == d.stellar {
			return false
		}
		if hops[wi.System] != hops[wj.System] {
			return hops[wi.System] < hops[wj.System]
		}
		return wi.Name < wj.Name
	})
	d.gov = governState{neigh: neigh}
}

// target is the world under the chart cursor.
func (a *App) governTarget() *universe.World {
	d := a.dock
	if d == nil || len(d.gov.neigh) == 0 {
		return nil
	}
	if d.gov.sel < 0 || d.gov.sel >= len(d.gov.neigh) {
		d.gov.sel = 0
	}
	return a.uni.Worlds[d.gov.neigh[d.gov.sel]]
}

func (a *App) updateGovern() {
	d := a.dock
	if d == nil || a.uni == nil {
		if d != nil {
			d.view = dockMain
		}
		return
	}
	here := a.uniWorld(d.stellar)
	if here == nil {
		d.view = dockMain
		return
	}
	g := &d.gov
	n := len(g.neigh)
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyG):
		if g.menu {
			g.menu = false
		} else {
			d.view = dockMain
		}
	case inpututil.IsKeyJustPressed(ebiten.KeyJ):
		d.view = dockJournal
	case n > 0 && (inpututil.IsKeyJustPressed(ebiten.KeyRight) || inpututil.IsKeyJustPressed(ebiten.KeyDown)):
		g.sel = (g.sel + 1) % n
	case n > 0 && (inpututil.IsKeyJustPressed(ebiten.KeyLeft) || inpututil.IsKeyJustPressed(ebiten.KeyUp)):
		g.sel = (g.sel + n - 1) % n
	case inpututil.IsKeyJustPressed(ebiten.KeyB):
		g.menu = !g.menu
	case g.menu:
		for b := universe.Building(0); b < universe.BuildingCount; b++ {
			if inpututil.IsKeyJustPressed(ebiten.KeyDigit1 + ebiten.Key(b)) {
				a.govern(func() error { return a.buildHere(here, b) })
				g.menu = false
			}
		}
	case inpututil.IsKeyJustPressed(ebiten.KeyO):
		a.govern(func() error { return a.orderFlight(here) })
	case inpututil.IsKeyJustPressed(ebiten.KeyC):
		a.govern(func() error { return a.orderConvoy(here) })
	case inpututil.IsKeyJustPressed(ebiten.KeyX):
		a.govern(func() error {
			if !a.seatHeld(here) {
				return fmt.Errorf("you do not hold the seat at %s", here.Name)
			}
			for i := len(here.Orders) - 1; i >= 0; i-- {
				a.uni.Cancel(here.Stellar, i)
			}
			return nil
		})
	case inpututil.IsKeyJustPressed(ebiten.KeyL):
		a.govern(func() error {
			t := a.governTarget()
			if t == nil || t == here {
				return fmt.Errorf("put the cursor on another system's world")
			}
			if !a.seatFor(here) {
				return fmt.Errorf("%s will not sell you a charter", here.Name)
			}
			return a.uni.Charter(here, t, &a.voy.Credits, universe.SeatPlayer)
		})
	case inpututil.IsKeyJustPressed(ebiten.KeyR):
		a.govern(func() error { return a.cycleRelation(here) })
	case inpututil.IsKeyJustPressed(ebiten.KeyT):
		a.govern(func() error {
			if !a.seatHeld(here) {
				return fmt.Errorf("you do not hold the seat at %s", here.Name)
			}
			steps := []float64{0, 0.06, 0.12, 0.18}
			i := 0
			for k, s := range steps {
				if math.Abs(here.Tariff-s) < 0.005 {
					i = (k + 1) % len(steps)
				}
			}
			here.Tariff = steps[i]
			g.msg = fmt.Sprintf("%s now takes %.0f%% on non-allied sales.", here.Name, here.Tariff*100)
			return nil
		})
	}
	a.drainNotices()
}

// govern runs one desk action and puts its outcome on the desk.
func (a *App) govern(f func() error) {
	if err := f(); err != nil {
		a.dock.gov.msg = err.Error()
		return
	}
	if a.dock.gov.msg == "" {
		a.dock.gov.msg = "Done."
	}
	a.syncPlanetStock()
}

// seatHeld reports whether the player actually holds this world's seat.
func (a *App) seatHeld(w *universe.World) bool {
	return w != nil && w.Seat == universe.SeatPlayer
}

// buildHere buys the next level of b at w out of the player's purse. The
// first building is the charter: the seat follows it.
func (a *App) buildHere(w *universe.World, b universe.Building) error {
	if !a.seatFor(w) {
		return fmt.Errorf("%s answers to %s; it will not sell you a charter", w.Name, w.Govt.Name())
	}
	if b == universe.Lane {
		t := a.governTarget()
		if t == nil || t == w {
			return fmt.Errorf("put the cursor on another system's world to charter a lane")
		}
		return a.uni.Charter(w, t, &a.voy.Credits, universe.SeatPlayer)
	}
	if err := a.uni.Build(w, b, &a.voy.Credits, universe.SeatPlayer); err != nil {
		return err
	}
	a.dock.gov.msg = fmt.Sprintf("%s level %d stands at %s. Seat: %s.", b, w.Built[b], w.Name, w.Seat)
	return nil
}

// orderFlight files a standing order of a flight's worth of hulls a day at
// the world under the cursor.
func (a *App) orderFlight(w *universe.World) error {
	if !a.seatHeld(w) {
		return fmt.Errorf("you do not hold the seat at %s", w.Name)
	}
	t := a.governTarget()
	if t == nil || t == w {
		return fmt.Errorf("put the cursor on the destination")
	}
	if w.Govt == govt.None {
		return fmt.Errorf("%s has no fleet to send", w.Name)
	}
	o := universe.StandingOrder{From: w.Stellar, To: t.Stellar, Owner: w.Govt, Hulls: govt.MinFleet(w.Govt)}
	if err := a.uni.File(o); err != nil {
		return err
	}
	kind := "reinforce"
	if t.Govt != w.Govt {
		kind = "take"
	}
	a.dock.gov.msg = fmt.Sprintf("%d hulls a day will leave %s to %s %s (rated %.2f).",
		o.Hulls, w.Name, kind, t.Name, a.uni.Rating(t))
	return nil
}

// orderConvoy files a convoy of the good this world makes that the target
// pays most for.
func (a *App) orderConvoy(w *universe.World) error {
	if !a.seatHeld(w) {
		return fmt.Errorf("you do not hold the seat at %s", w.Name)
	}
	t := a.governTarget()
	if t == nil || t == w {
		return fmt.Errorf("put the cursor on the destination")
	}
	if w.Govt == govt.None {
		return fmt.Errorf("%s has no fleet to send", w.Name)
	}
	best, bestP := econ.Slag, 0
	for m := econ.Material(0); m < econ.Slag; m++ {
		if !w.Makes(m) || w.Warehouse[m] < 25 {
			continue
		}
		if m.Refined() && !a.uni.Chartered(w.System, t.System) && w.System != t.System {
			continue
		}
		if p := t.Shop[m]; p > bestP {
			best, bestP = m, p
		}
	}
	if best == econ.Slag {
		return fmt.Errorf("%s makes nothing %s will take by the lane you have", w.Name, t.Name)
	}
	o := universe.StandingOrder{From: w.Stellar, To: t.Stellar, Owner: w.Govt, Mat: best, Tons: 40}
	if err := a.uni.File(o); err != nil {
		return err
	}
	a.dock.gov.msg = fmt.Sprintf("40 t of %s a day will run %s → %s.", best, w.Name, t.Name)
	return nil
}

// cycleRelation walks war → peace → ally → war with the cursor world's
// colour. Only from a capital the player holds.
func (a *App) cycleRelation(w *universe.World) error {
	if !a.seatHeld(w) {
		return fmt.Errorf("you do not hold the seat at %s", w.Name)
	}
	cap := a.uni.Capital(w.Govt)
	if cap != w {
		return fmt.Errorf("relations are set from %s's capital", w.Govt)
	}
	t := a.governTarget()
	if t == nil || t.Govt == govt.None || t.Govt == w.Govt {
		return fmt.Errorf("put the cursor on another colour's world")
	}
	next := (a.uni.Relation(w.Govt, t.Govt) + 1) % 3
	a.uni.SetRelation(w.Govt, t.Govt, next)
	a.dock.gov.msg = fmt.Sprintf("%s and %s: %s.", w.Govt, t.Govt, next)
	return nil
}

// armouryHas reports whether the port's shelf holds n rounds' worth of tons.
func (a *App) armouryHas(stellar, n int) bool {
	uw := a.uniWorld(stellar)
	return uw == nil || uw.Warehouse[econ.Rounds] >= float64(n)*world.RoundTons
}

// armouryDraw takes n rounds off the shelf, into the sink — a magazine is
// not a pool the auditor reads, so the tons count as spent when bought.
func (a *App) armouryDraw(stellar, n int) {
	if uw := a.uniWorld(stellar); uw != nil {
		econ.Consume(&uw.Warehouse, &a.uni.Sink, econ.Rounds, float64(n)*world.RoundTons)
	}
}

// --- drawing -------------------------------------------------------------

func (a *App) drawGovern(screen *ebiten.Image, x, y float64) {
	d := a.dock
	if d == nil {
		return
	}
	if a.uni == nil {
		ui.DrawText(screen, "GOVERNOR'S DESK — no economy behind this sky.", x, y+90, 0.9)
		return
	}
	a.stepUniverse()
	here := a.uniWorld(d.stellar)
	if here == nil {
		ui.DrawText(screen, "GOVERNOR'S DESK — this port keeps no books.", x, y+90, 0.9)
		return
	}
	if len(d.gov.neigh) == 0 {
		a.openGovern()
	}
	target := a.governTarget()
	if target == nil {
		target = here
	}

	seat := "read only"
	switch {
	case here.Seat == universe.SeatPlayer:
		seat = "YOURS (charter held)"
	case a.seatFor(here):
		seat = "open (the first building is the charter)"
	}
	ui.DrawText(screen, fmt.Sprintf("GOVERNOR'S DESK - %s / %s / seat: %s", here.Name, here.Govt.Name(), seat), x, y+90, 1)
	ui.DrawText(screen, "arrows: cursor   B build  O flight  C convoy  X cancel  L lane  R relations  T tariff  J journal  G leave",
		x, y+106, 0.6)

	a.drawGovernRail(screen, here, x, y+134)
	a.drawGovernChart(screen, here, target, x+300, y+118, 330, 250)
	a.drawGovernBooks(screen, target, x, y+268)
	a.drawGovernWorks(screen, here, x+300, y+378)
	a.drawGovernDesk(screen, here, x, y+540)
}

// drawGovernRail is THE WORLD: six tanks.
func (a *App) drawGovernRail(screen *ebiten.Image, w *universe.World, x, y float64) {
	u := a.uni
	var maxPop float64 = 1
	for _, id := range u.Order() {
		maxPop = math.Max(maxPop, float64(u.Worlds[id].Pop))
	}
	fedArrow := "+"
	if w.Fed() < 0.85 {
		fedArrow = "="
	}
	if w.Fed() < 0.35 {
		fedArrow = "-"
	}
	bill := importBillOf(w)
	days := 999.0
	if bill > 0 {
		days = float64(w.Credits) / bill
	}
	rounds := w.Warehouse[econ.Rounds]
	roundsCover := w.Cover(econ.Rounds)
	rating := u.Rating(w)
	garrison := len(u.Garrison(w))

	type tank struct {
		frac       float64
		c          color.RGBA
		label, val string
	}
	tanks := []tank{
		{float64(w.Pop) / maxPop, color.RGBA{110, 200, 110, 255}, "POP", fmt.Sprintf("%.1fM%s", float64(w.Pop)/1e6, fedArrow)},
		{math.Min(1, days/60), color.RGBA{206, 196, 122, 255}, "CASH", fmt.Sprintf("%dd", int(math.Min(days, 999)))},
		{math.Min(1, rounds/500), color.RGBA{255, 190, 96, 255}, "RNDS", fmt.Sprintf("%.0ft", rounds)},
		{math.Min(1, w.Warehouse[econ.Missiles]/100), color.RGBA{255, 92, 108, 255}, "MSSL", fmt.Sprintf("%.0ft", w.Warehouse[econ.Missiles])},
		{rating / 0.95, color.RGBA{96, 148, 255, 255}, "RATE", fmt.Sprintf("%.2f", rating)},
		{math.Min(1, float64(garrison)/8), teamRGBA(w.Govt), "GARR", fmt.Sprintf("%d", garrison)},
	}
	for i, t := range tanks {
		ui.VGauge(screen, x+float64(i)*46, y, 22, 70, t.frac, t.c, t.label, t.val)
	}
	ui.DrawText(screen, fmt.Sprintf("fed %.0f%%  treasury %d cr  rounds cover %s  %d berths",
		w.Fed()*100, w.Credits, coverText(roundsCover), w.Berths()), x, y+108, 0.65)
}

// importBillOf is roughly what a world pays a day for the finished goods it
// does not make: demand (tons a day, read back from cover) times price.
func importBillOf(w *universe.World) float64 {
	var bill float64
	for m := econ.Material(0); m < econ.Count; m++ {
		if !m.Finished() || w.Makes(m) {
			continue
		}
		bill += w.Demand(m) * float64(w.Shop[m])
	}
	return bill
}

func coverText(days float64) string {
	switch {
	case days < 0:
		return "-"
	case days > 999:
		return ">999d"
	}
	return fmt.Sprintf("%.0fd", days)
}

// chartProjector maps a system ID onto a chart rectangle, the same way the
// mission computer does, so the desk's overlays land on the same stars.
func (a *App) chartProjector(x0, y0, w, h float64) func(id int) (float32, float32, bool) {
	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64
	for _, s := range a.gal.Systems {
		minX, maxX = math.Min(minX, float64(s.X)), math.Max(maxX, float64(s.X))
		minY, maxY = math.Min(minY, float64(s.Y)), math.Max(maxY, float64(s.Y))
	}
	return func(id int) (float32, float32, bool) {
		s := a.gal.Systems[id]
		if s == nil || maxX <= minX || maxY <= minY {
			return 0, 0, false
		}
		return float32(x0 + 16 + (float64(s.X)-minX)/(maxX-minX)*(w-32)),
			float32(y0 + 26 + (float64(s.Y)-minY)/(maxY-minY)*(h-46)), true
	}
}

// drawGovernChart is THE CHART: the mission computer's map with the five
// overlays — typed lanes, rating badges, hulls in transit, wreck fields and
// standing orders — and the cursor.
func (a *App) drawGovernChart(screen *ebiten.Image, here, target *universe.World, x0, y0, w, h float64) {
	u := a.uni
	a.drawMissionChart(screen, here.System, -1, x0, y0, w, h)
	px := a.chartProjector(x0, y0, w, h)
	d := a.dock

	// Lanes out of the current system: solid for couriers (every port is a
	// spaceport), dotted where a charter lets intermediates cross, broken
	// where war closes it.
	if s := a.gal.Systems[here.System]; s != nil {
		hx, hy, ok := px(here.System)
		if ok {
			for _, l := range s.Links {
				lx, ly, ok2 := px(l)
				if !ok2 {
					continue
				}
				c := color.RGBA{120, 150, 130, 255}
				al := 0.45
				if u.Chartered(here.System, l) {
					c, al = color.RGBA{255, 244, 180, 255}, 0.8
				}
				fastLine(screen, hx, hy, lx, ly, 1, c, al)
			}
		}
	}

	// Rating badges on every world in the neighbourhood, staggered so two
	// worlds in one system both read.
	count := map[int]int{}
	for _, id := range d.gov.neigh {
		nw := u.Worlds[id]
		fx, fy, ok := px(nw.System)
		if !ok {
			continue
		}
		k := count[nw.System]
		count[nw.System]++
		if k >= maxBadges && nw != target {
			continue // the dense core: four badges a star, and the cursor's
		}
		bx := float64(fx) + 8
		by := float64(fy) + 12 + float64(k)*12
		mark := "o"
		if nw.Govt != govt.None {
			mark = "#"
		}
		c := teamRGBA(nw.Govt)
		if nw.Govt == govt.None {
			c = color.RGBA{190, 190, 180, 255}
		}
		al := float32(0.75)
		if nw == target {
			al = 1
			vector.StrokeRect(screen, float32(bx-3), float32(by-2), 78, 13, 1, colChrome, false)
		}
		ui.DrawTextScaled(screen, fmt.Sprintf("%s %.2f %s", mark, u.Rating(nw), trunc(nw.Name, 8)),
			bx, by, 0.75, c, al)
	}

	// Hulls in transit on lanes touching the neighbourhood: a marker at the
	// fraction flown, with an ETA.
	for _, hh := range u.Fleet.Hulls {
		if !hh.Status.UnderWay() {
			continue
		}
		from, to := u.Worlds[hh.From], u.Worlds[hh.To]
		if from == nil || to == nil {
			continue
		}
		if !a.inNeigh(from.Stellar) && !a.inNeigh(to.Stellar) {
			continue
		}
		fx, fy, ok1 := px(from.System)
		tx, ty, ok2 := px(to.System)
		if !ok1 || !ok2 {
			continue
		}
		lane := u.Fleet.Lane(hh.From, hh.To)
		f := float32(math.Min(1, hh.S/math.Max(lane.Length, 1)))
		if from.System == to.System {
			// in-system: draw beside the star
			f = 0.5
			tx, ty = fx+30, fy-14
		}
		mx, my := fx+(tx-fx)*f, fy+(ty-fy)*f
		c := teamRGBA(hh.Govt)
		fastDot(screen, mx, my, 2.2, c, 0.95)
		if hh.Mission == traffic.Flight {
			vector.StrokeCircle(screen, mx, my, 5, 1, c, false)
		}
		if to.Stellar == here.Stellar || from.Stellar == here.Stellar {
			ui.DrawTextScaled(screen, fmt.Sprintf("%s %.0fd", trunc(hh.Name, 8), u.Fleet.ETA(hh)),
				float64(mx)+5, float64(my)-4, 0.65, c, 0.9)
		}
	}

	// Wreck fields.
	for _, db := range u.Fleet.Debris {
		var mx, my float32
		var ok bool
		if db.InOrbit() {
			at := u.Worlds[db.At]
			if at == nil {
				continue
			}
			mx, my, ok = px(at.System)
			mx += 18
			my -= 6
		} else {
			from, to := u.Worlds[db.From], u.Worlds[db.To]
			if from == nil || to == nil {
				continue
			}
			fx, fy, ok1 := px(from.System)
			tx, ty, ok2 := px(to.System)
			ok = ok1 && ok2
			lane := u.Fleet.Lane(db.From, db.To)
			f := float32(math.Min(1, db.S/math.Max(lane.Length, 1)))
			mx, my = fx+(tx-fx)*f, fy+(ty-fy)*f
		}
		if !ok {
			continue
		}
		ui.DrawTextScaled(screen, fmt.Sprintf("x %.0ft", db.Stock.Total()), float64(mx), float64(my), 0.7,
			color.RGBA{255, 200, 120, 255}, 0.9)
	}

	// Standing orders from here: a doubled line to the destination.
	for _, o := range here.Orders {
		to := u.Worlds[o.To]
		if to == nil {
			continue
		}
		hx, hy, ok1 := px(here.System)
		tx, ty, ok2 := px(to.System)
		if !ok1 || !ok2 {
			continue
		}
		c := teamRGBA(here.Govt)
		fastLine(screen, hx, hy+2, tx, ty+2, 1, c, 0.8)
		fastLine(screen, hx, hy-2, tx, ty-2, 1, c, 0.8)
	}
	ui.DrawTextScaled(screen, "line: lane  bright: chartered  #: held  o: neutral  x: wreck  dot: hull  ring: flight",
		x0+8, y0+h-18, 0.65, color.RGBA{200, 205, 215, 255}, 0.8)
}

// maxBadges is how many rating badges one star carries before the rest are
// left to the cursor.
const maxBadges = 4

func (a *App) inNeigh(stellar int) bool {
	for _, id := range a.dock.gov.neigh {
		if id == stellar {
			return true
		}
	}
	return false
}

// drawGovernBooks is THE BOOKS for the world under the cursor: every
// material with tons, cover and today's price against base.
func (a *App) drawGovernBooks(screen *ebiten.Image, w *universe.World, x, y float64) {
	u := a.uni
	who := "THE BOOKS"
	if w.Stellar != a.dock.stellar {
		who = fmt.Sprintf("THE BOOKS - %s (%s)", w.Name, w.Govt.Name())
	}
	ui.DrawText(screen, who, x, y, 0.9)
	full := a.resolution(w)
	if !full {
		ui.DrawText(screen, fmt.Sprintf("  %s shows a stranger only what a ship in orbit could see:", w.Name), x, y+18, 0.65)
		ui.DrawText(screen, fmt.Sprintf("  pop %.2fM  rated %.2f  %d hulls in orbit", float64(w.Pop)/1e6,
			u.Rating(w), len(u.Garrison(w))), x, y+34, 0.75)
		return
	}
	ui.DrawText(screen, "  material     tons  cover  vs base", x, y+18, 0.6)
	yy := y + 34
	rows := 0
	for m := econ.Material(0); m < econ.Slag && rows < 14; m++ {
		if w.Warehouse[m] < 0.5 && w.Cover(m) < 0 {
			continue
		}
		base := universe.Base(m)
		delta := ""
		tone := color.RGBA{223, 228, 230, 255}
		if base > 0 && w.Shop[m] > 0 {
			pct := (float64(w.Shop[m])/base - 1) * 100
			switch {
			case pct > 25:
				delta, tone = fmt.Sprintf("%+4.0f%% SELL", pct), color.RGBA{212, 116, 110, 255} // they pay over base: sell here
			case pct < -20:
				delta, tone = fmt.Sprintf("%+4.0f%% BUY", pct), color.RGBA{140, 240, 140, 255} // under base: buy here
			default:
				delta = fmt.Sprintf("%+4.0f%%", pct)
			}
		}
		ui.DrawTextScaled(screen, fmt.Sprintf("  %-11s %6.0f  %5s  %s", trunc(m.String(), 11),
			w.Warehouse[m], coverText(w.Cover(m)), delta), x, yy, 1, tone, 0.85)
		yy += 15
		rows++
	}
}

// resolution reports whether the player sees a world's books in full: their
// own colour's and their allies' worlds, and the unaligned.
func (a *App) resolution(w *universe.World) bool {
	c := a.playerColour()
	return w.Govt == govt.None || w.Govt == c || a.uni.Relation(c, w.Govt) == universe.Ally
}

// drawGovernWorks is the plant list, the yard line and the standing orders.
func (a *App) drawGovernWorks(screen *ebiten.Image, w *universe.World, x, y float64) {
	ui.DrawText(screen, "WORKS", x, y, 0.9)
	yy := y + 18
	for _, p := range w.Plant {
		line := fmt.Sprintf("  %-16s 100%%", trunc(p.Name, 16))
		tone := float32(0.7)
		if mat, r := p.Bottleneck(); r < 0.999 {
			line = fmt.Sprintf("  %-16s %3.0f%% [%s]", trunc(p.Name, 16), r*100, mat)
			tone = 0.95
		}
		ui.DrawText(screen, line, x, yy, tone)
		yy += 15
	}
	if len(w.Plant) == 0 {
		ui.DrawText(screen, "  no industry", x, yy, 0.6)
		yy += 15
	}
	built := []string{}
	for b := universe.Building(0); b < universe.BuildingCount; b++ {
		if w.Built[b] > 0 {
			built = append(built, fmt.Sprintf("%s %d", b, w.Built[b]))
		}
	}
	if len(built) > 0 {
		ui.DrawText(screen, "  built: "+strings.Join(built, ", "), x, yy, 0.65)
		yy += 15
	}
	yy += 6
	ui.DrawText(screen, "STANDING ORDERS", x, yy, 0.9)
	yy += 18
	if len(w.Orders) == 0 {
		ui.DrawText(screen, "  none", x, yy, 0.6)
	}
	for i, o := range w.Orders {
		if i >= 4 {
			break
		}
		to := fmt.Sprintf("#%d", o.To)
		if t := a.uni.Worlds[o.To]; t != nil {
			to = t.Name
		}
		if o.Hulls > 0 {
			ui.DrawText(screen, fmt.Sprintf("  > %d hulls/day -> %s", o.Hulls, to), x, yy, 0.8)
		} else {
			ui.DrawText(screen, fmt.Sprintf("  > %.0ft %s/day -> %s", o.Tons, o.Mat, to), x, yy, 0.8)
		}
		yy += 15
	}
}

// drawGovernDesk is the keycaps, the build menu when open, the message, and
// the two auditors' verdicts.
func (a *App) drawGovernDesk(screen *ebiten.Image, w *universe.World, x, y float64) {
	d := a.dock
	g := &d.gov
	t := a.governTarget()
	tname := "-"
	if t != nil {
		tname = t.Name
	}
	live := a.seatHeld(w)
	tone := func(need bool) ui.KeyTone {
		if need && !live {
			return ui.ToneDim
		}
		return ui.ToneKhaki
	}
	nextB := fmt.Sprintf("Build: Works %d", w.Price(universe.Works))
	if g.menu {
		nextB = "Build: pick 1-8"
	}
	caps := []struct {
		key, label string
		tone       ui.KeyTone
	}{
		{"B", nextB, tone(!a.seatFor(w))},
		{"O", "Flight > " + trunc(tname, 10), tone(true)},
		{"C", "Convoy > " + trunc(tname, 10), tone(true)},
		{"X", "Cancel orders", tone(true)},
		{"L", "Lane > " + trunc(tname, 10), tone(!a.seatFor(w))},
		{"R", "Rel: " + a.relationLabel(w, t), tone(true)},
		{"T", fmt.Sprintf("Tariff %.0f%%", w.Tariff*100), tone(true)},
		{"G", "Leave", ui.ToneGreen},
	}
	for i, c := range caps {
		col, row := i%4, i/4
		ui.Keycap(screen, x+float64(col)*236, y+float64(row)*30, 226, c.key, c.label, c.tone, c.key == "G")
	}
	yy := y + 66
	if g.menu {
		for b := universe.Building(0); b < universe.BuildingCount; b++ {
			line := fmt.Sprintf("%d. %-10s L%d  %7d cr", int(b)+1, b, w.Built[b], w.Price(b))
			if err := w.CanBuild(b); err != nil && b != universe.Lane {
				line += "  - " + trunc(err.Error(), 40)
			}
			ui.DrawText(screen, line, x+float64(b%2)*470, yy+float64(b/2)*15, 0.8)
		}
		yy += 62
	} else if g.msg != "" {
		ui.DrawTextScaled(screen, g.msg, x, yy, 1, color.RGBA{255, 244, 180, 255}, 0.95)
		yy += 18
	}
	// The verdicts, and the wire.
	mass, cash := "MASS OK", "CREDITS OK"
	if bad := a.uni.Audit(); len(bad) > 0 {
		mass = "MASS BAD " + trunc(bad[0].String(), 40)
	}
	if bad := a.uni.AuditCredits(); bad != nil {
		cash = "CREDITS BAD " + trunc(bad.String(), 40)
	}
	tail := a.uni.Journal.Tail(2)
	wire := ""
	if len(tail) > 0 {
		wire = trunc(tail[len(tail)-1].String(), 70)
	}
	ui.DrawText(screen, fmt.Sprintf("%-72s %s  %s", wire, mass, cash), x, y+126, 0.7)
}

func (a *App) relationLabel(w, t *universe.World) string {
	if t == nil || t.Govt == govt.None || t.Govt == w.Govt {
		return "-"
	}
	return fmt.Sprintf("%s %s", t.Govt, a.uni.Relation(w.Govt, t.Govt))
}

// --- the console form ------------------------------------------------------

// registerGovernCommands is the headless desk: every quantity on the screen,
// printable from the same seed, plus the actions.
func (a *App) registerGovernCommands(c *console.Console) {
	need := func(c *console.Console) bool {
		if a.uni == nil {
			c.Printf("- No universe: start a game first.")
			return false
		}
		a.stepUniverse()
		return true
	}
	worldArg := func(arg string) *universe.World {
		id := 0
		if a.dock != nil {
			id = a.dock.stellar
		} else if a.World != nil {
			id = a.nearbyStellar()
		}
		fmt.Sscanf(arg, "%d", &id)
		return a.uni.Worlds[id]
	}

	c.Register(func(c *console.Console, arg string) {
		if !need(c) {
			return
		}
		w := worldArg(arg)
		if w == nil {
			c.Printf("- usage: govern <stellar id> (or dock at one)")
			return
		}
		for _, line := range a.deskText(w) {
			c.Printf("%s", line)
		}
	}, "govern", "desk")

	c.Register(func(c *console.Console, arg string) {
		if !need(c) {
			return
		}
		w := worldArg(arg)
		if w == nil {
			c.Printf("- usage: rating <stellar id>")
			return
		}
		c.Printf("- %s rates %.2f — %d hulls in orbit, %.0ft of rounds, Bastion %d", w.Name,
			a.uni.Rating(w), len(a.uni.Garrison(w)), w.Warehouse[econ.Rounds], w.Built[universe.Bastion])
	}, "rating")

	c.Register(func(c *console.Console, _ string) {
		if !need(c) {
			return
		}
		for _, st := range a.uni.Standings() {
			c.Printf("- %-5s %d worlds · pop %.1fM · %d hulls · treasuries %d cr · exchequer %d cr · capital rated %.2f",
				st.Color, st.Worlds, float64(st.Pop)/1e6, st.Hulls, st.Treasury, st.Exchequer, st.Rating)
		}
		for _, x := range govt.Colors() {
			for _, y := range govt.Colors() {
				if x < y {
					c.Printf("  %s–%s: %s", x, y, a.uni.Relation(x, y))
				}
			}
		}
		if bad := a.uni.AuditCredits(); bad != nil {
			c.Printf("  LEDGER: %v", bad)
		} else {
			c.Printf("  ledger balanced: %d cr in circulation", a.uni.MoneySupply())
		}
	}, "standings", "ledger")

	// order <from> <to> hulls N | tons <material> N
	c.Register(func(c *console.Console, arg string) {
		if !need(c) {
			return
		}
		f := strings.Fields(arg)
		if len(f) < 4 {
			c.Printf("- usage: order <from> <to> hulls N | order <from> <to> tons <material> N")
			return
		}
		var from, to int
		fmt.Sscanf(f[0], "%d", &from)
		fmt.Sscanf(f[1], "%d", &to)
		src := a.uni.Worlds[from]
		if src == nil {
			c.Printf("- no such port %d", from)
			return
		}
		o := universe.StandingOrder{From: from, To: to, Owner: src.Govt}
		switch f[2] {
		case "hulls":
			fmt.Sscanf(f[3], "%d", &o.Hulls)
		case "tons":
			if len(f) < 5 {
				c.Printf("- order <from> <to> tons <material> N")
				return
			}
			m, ok := econ.Parse(f[3])
			if !ok {
				c.Printf("- no such material %q", f[3])
				return
			}
			o.Mat = m
			fmt.Sscanf(f[4], "%g", &o.Tons)
		default:
			c.Printf("- hulls or tons")
			return
		}
		if err := a.uni.File(o); err != nil {
			c.Printf("- refused: %v", err)
			return
		}
		c.Printf("- filed: %s", o)
	}, "order")

	c.Register(func(c *console.Console, arg string) {
		if !need(c) {
			return
		}
		var from, i int
		if n, _ := fmt.Sscanf(arg, "%d %d", &from, &i); n < 2 {
			c.Printf("- usage: cancel <from> <index>")
			return
		}
		a.uni.Cancel(from, i)
		c.Printf("- cancelled.")
	}, "cancel")

	// build <building> [stellar]
	c.Register(func(c *console.Console, arg string) {
		if !need(c) || a.voy == nil {
			return
		}
		f := strings.Fields(arg)
		if len(f) < 1 {
			c.Printf("- usage: build <spaceport|works|habitat|exchange|bastion|picket|silo> [stellar]")
			return
		}
		b, ok := universe.ParseBuilding(f[0])
		if !ok {
			c.Printf("- no such building %q", f[0])
			return
		}
		rest := ""
		if len(f) > 1 {
			rest = f[1]
		}
		w := worldArg(rest)
		if w == nil {
			c.Printf("- no such port")
			return
		}
		if !a.seatFor(w) {
			c.Printf("- %s will not sell you a charter", w.Name)
			return
		}
		if err := a.uni.Build(w, b, &a.voy.Credits, universe.SeatPlayer); err != nil {
			c.Printf("- refused: %v", err)
			return
		}
		a.syncPlanetStock()
		c.Printf("- %s level %d at %s; %d cr left; seat: %s", b, w.Built[b], w.Name, a.voy.Credits, w.Seat)
	}, "build")

	c.Register(func(c *console.Console, arg string) {
		if !need(c) || a.voy == nil {
			return
		}
		var from, to int
		if n, _ := fmt.Sscanf(arg, "%d %d", &from, &to); n < 2 {
			c.Printf("- usage: charter <from> <to>")
			return
		}
		if err := a.uni.Charter(a.uni.Worlds[from], a.uni.Worlds[to], &a.voy.Credits, universe.SeatPlayer); err != nil {
			c.Printf("- refused: %v", err)
			return
		}
		c.Printf("- chartered.")
	}, "charter")

	c.Register(func(c *console.Console, arg string) {
		if !need(c) {
			return
		}
		f := strings.Fields(arg)
		if len(f) < 2 {
			c.Printf("- usage: relate <colour> war|peace|ally")
			return
		}
		other, ok := govt.Parse(f[0])
		r, ok2 := universe.ParseRelation(f[1])
		if !ok || !ok2 || other == govt.None {
			c.Printf("- relate <red|green|blue> war|peace|ally")
			return
		}
		a.uni.SetRelation(a.playerColour(), other, r)
		c.Printf("- %s and %s: %s", a.playerColour(), other, r)
	}, "relate")
}

// deskText is the desk as lines, for the console and the tests.
func (a *App) deskText(w *universe.World) []string {
	u := a.uni
	out := []string{
		fmt.Sprintf("- GOVERNOR'S DESK — %s · %s · seat: %s · day %d", w.Name, w.Govt.Name(), w.Seat, u.Day),
		fmt.Sprintf("  pop %.2fM fed %.0f%% · treasury %d cr · rounds %.0ft · missiles %.0ft · rating %.2f · garrison %d · berths %d",
			float64(w.Pop)/1e6, w.Fed()*100, w.Credits, w.Warehouse[econ.Rounds], w.Warehouse[econ.Missiles],
			u.Rating(w), len(u.Garrison(w)), w.Berths()),
	}
	for _, p := range w.Plant {
		if mat, r := p.Bottleneck(); r < 0.999 {
			out = append(out, fmt.Sprintf("  works: %s %.0f%% [%s]", p.Name, r*100, mat))
		} else {
			out = append(out, fmt.Sprintf("  works: %s 100%%", p.Name))
		}
	}
	for b := universe.Building(0); b < universe.BuildingCount; b++ {
		if w.Built[b] > 0 {
			out = append(out, fmt.Sprintf("  built: %s level %d", b, w.Built[b]))
		}
	}
	out = append(out, "  books:")
	for m := econ.Material(0); m < econ.Slag; m++ {
		if w.Warehouse[m] < 0.5 && w.Cover(m) < 0 {
			continue
		}
		pct := 0.0
		if base := universe.Base(m); base > 0 {
			pct = (float64(w.Shop[m])/base - 1) * 100
		}
		out = append(out, fmt.Sprintf("    %-11s %8.0ft  cover %5s  %5d cr/t  %+.0f%% vs base",
			m, w.Warehouse[m], coverText(w.Cover(m)), w.Shop[m], pct))
	}
	for i, o := range w.Orders {
		out = append(out, fmt.Sprintf("  order %d: %s", i, u.DescribeOrder(o)))
	}
	for _, d := range u.Fleet.Debris {
		if d.InOrbit() && d.At == w.Stellar {
			out = append(out, fmt.Sprintf("  wreck field in orbit: %.0ft", d.Stock.Total()))
		}
	}
	for b := universe.Building(0); b < universe.BuildingCount; b++ {
		if b == universe.Lane {
			continue
		}
		out = append(out, fmt.Sprintf("  next %-10s %7d cr", b, w.Price(b)))
	}
	return out
}
