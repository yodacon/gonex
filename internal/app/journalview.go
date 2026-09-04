package app

import (
	"fmt"
	"image/color"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/render"
	"yodacon.org/gonex/internal/traffic"
	"yodacon.org/gonex/internal/ui"
	"yodacon.org/gonex/internal/world"
)

// The trade journal, on the wall of the spaceport.
//
// Everything on this screen was already true — the economy has been running
// whether or not anybody could see it. What was missing was the READING of
// it. A market simulation nobody can look at is indistinguishable from a
// random number generator, and the player's only way to tell them apart was
// to memorise prices.
//
// So: what this port pays and holds, what leaves it and for where, which
// hulls are inbound and when, and what the other two colours are moving. It
// is a departures board, a commodity ticker and an intelligence briefing on
// one wall, because in a war economy those are the same document.

func (a *App) drawDockJournal(screen *ebiten.Image, x, y float64) {
	d := a.dock
	if d == nil {
		return
	}
	ui.DrawText(screen, "TRADE JOURNAL — J or M returns to the concourse", x, y+90, 1)

	if a.uni == nil {
		ui.DrawText(screen, "No traffic data at this port.", x, y+120, 0.8)
		return
	}
	a.stepUniverse()
	uw := a.uniWorld(d.stellar)
	if uw == nil {
		ui.DrawText(screen, "This port keeps no books — nothing is tracked here.", x, y+120, 0.8)
		return
	}

	yy := y + 118
	// What this world is, in its own terms.
	rich, tons := econ.Endowment{Reserve: uw.Reserve}.Richest()
	ui.DrawText(screen, fmt.Sprintf("%s · %s · pop %.1fM · treasury %d cr",
		uw.Name, uw.Govt.Name(), float64(uw.Pop)/1e6, uw.Credits), x, yy, 0.95)
	yy += 18
	ui.DrawText(screen, fmt.Sprintf("industry: %s · richest seam %s, %.0f kt still in the ground",
		uw.Speciality(), rich, tons/1000), x, yy, 0.7)
	yy += 26

	// The plant, with its bottleneck named. The most actionable line on the
	// screen: a chain choked on an input is a standing order for whoever
	// turns up with that input in the hold.
	for _, p := range uw.Plant {
		line := "  " + p.Describe()
		tone := float32(0.68)
		if mat, r := p.Bottleneck(); r < 0.999 {
			line = fmt.Sprintf("  %s  ← short of %s, running at %.0f%%", p.Name, mat, r*100)
			tone = 0.9
		}
		ui.DrawText(screen, line, x, yy, tone)
		yy += 17
	}
	// The right third of the concourse belongs to the hull render and the
	// ship's own gauges, and the bottom right to the fuel bars. Everything
	// here lives left of that furniture, in two columns, with the wire
	// running under all of it where the screen is clear.
	a.drawJournalLanes(screen, x, y+236, uw.Stellar)
	a.drawJournalInbound(screen, x+420, y+236, uw.Stellar)
	a.drawJournalRivals(screen, x, y+392)
	a.drawJournalTicker(screen, x, y+530)
	_ = yy
}

// drawJournalLanes is the departures board: what is worth carrying OUT of
// this port right now, for the player's own colour.
func (a *App) drawJournalLanes(screen *ebiten.Image, x, y float64, from int) {
	ui.DrawText(screen, "LANES OUT OF HERE — what pays, today", x, y, 0.9)
	y += 20

	mine := teamColor(world.Team(a.Cfg.Team))
	all := a.uni.FindRoutes(mine, 60)
	var here []int
	for i, r := range all {
		if r.From == from {
			here = append(here, i)
		}
	}
	if len(here) == 0 {
		ui.DrawText(screen, "  this port's surplus is short nowhere", x, y, 0.65)
		ui.DrawText(screen, "  you can reach. A colour trades with", x, y+15, 0.6)
		ui.DrawText(screen, "  its own and with neutrals, never an enemy.", x, y+30, 0.6)
		return
	}
	ui.DrawText(screen, "   CARGO          TO                    BUY   SELL   +/t   TONS", x, y, 0.65)
	y += 18
	for n, i := range here {
		if n >= 5 {
			break
		}
		r := all[i]
		dst := a.uni.Worlds[r.To]
		name := fmt.Sprintf("%d", r.To)
		if dst != nil {
			name = dst.Name
		}
		ui.DrawText(screen, fmt.Sprintf("   %-13s %-15s %5d %5d %+5d",
			r.Mat, trunc(name, 15), r.Buy, r.Sell, r.Margin), x, y, 0.75)
		y += 17
	}
}

// drawJournalInbound is the arrivals board: hulls actually under way to this
// port, with what they are carrying and when they are due.
func (a *App) drawJournalInbound(screen *ebiten.Image, x, y float64, to int) {
	ui.DrawText(screen, "INBOUND", x, y, 0.9)
	y += 20

	type row struct {
		h   *traffic.Hull
		eta float64
	}
	var rows []row
	for _, h := range a.uni.Fleet.Hulls {
		if h.Status.UnderWay() && h.To == to {
			rows = append(rows, row{h, a.uni.Fleet.ETA(h)})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].eta < rows[j].eta })

	if len(rows) == 0 {
		ui.DrawText(screen, "  nothing due.", x, y, 0.65)
		return
	}
	ui.DrawText(screen, "  HULL          CARGO           ETA", x, y, 0.65)
	y += 18
	for i, r := range rows {
		if i >= 5 {
			ui.DrawText(screen, fmt.Sprintf("  …and %d more", len(rows)-i), x, y, 0.6)
			break
		}
		cargo := "empty"
		if t := r.h.Laden(); t > 0 {
			cargo = fmt.Sprintf("%.0ft %s", t, heaviest(r.h.Cargo))
		}
		ui.DrawText(screen, fmt.Sprintf("  %-13s %-15s %4.1f d",
			trunc(r.h.Name, 13), trunc(cargo, 15), r.eta), x, y, 0.75)
		y += 17
	}
}

// drawJournalRivals is the intelligence briefing: how the other two colours
// are actually doing, measured in the only currency that decides a war of
// supply — tonnage landed.
func (a *App) drawJournalRivals(screen *ebiten.Image, x, y float64) {
	ui.DrawText(screen, "THE OTHER TWO", x, y, 0.9)
	y += 20

	var max float64 = 1
	tot := map[govt.Color]float64{}
	for _, g := range govt.Colors() {
		for _, h := range a.uni.Fleet.ByGovt(g) {
			tot[g] += h.Tons
		}
		max = math.Max(max, tot[g])
	}
	for _, g := range govt.Colors() {
		hulls := a.uni.Fleet.ByGovt(g)
		ui.DrawText(screen, fmt.Sprintf("%-6s %2d hulls  %7.0f t landed",
			g, len(hulls), tot[g]), x, y, 0.75)
		// A bar, because three numbers in a column is a table and three bars
		// is a standing.
		w := float32(tot[g] / max * 180)
		c := teamRGBA(g)
		c.A = 190
		vector.DrawFilledRect(screen, float32(x), float32(y+16), w, 5, c, false)
		y += 26
	}
	ui.DrawText(screen, fmt.Sprintf("%d hulls afloat · %.0f t in transit · %d lost",
		a.uni.Fleet.Afloat(), a.uni.Fleet.CargoAfloat().Total(),
		a.uni.Journal.LostHulls), x, y, 0.65)
}

// drawJournalTicker is the wire: the last several things that happened
// anywhere, which is the only place in the game the player can see the
// economy behaving like a world rather than like a price list.
func (a *App) drawJournalTicker(screen *ebiten.Image, x, y float64) {
	ui.DrawText(screen, "ON THE WIRE", x, y, 0.9)
	y += 20
	vector.DrawFilledRect(screen, float32(x-8), float32(y-4), 640, 118,
		premul(color.RGBA{10, 17, 25, 255}, 0.75), false)
	tail := a.uni.Journal.Tail(6)
	if len(tail) == 0 {
		ui.DrawText(screen, "  quiet lanes.", x, y, 0.65)
		return
	}
	for i, e := range tail {
		// Older entries dim, so the eye lands on what just happened.
		tone := float32(0.5 + 0.45*float64(i)/math.Max(1, float64(len(tail)-1)))
		ui.DrawText(screen, "  "+e.String(), x, y, tone)
		y += 17
	}
}

// heaviest names the bulk of a manifest — what the ship is really carrying.
func heaviest(s econ.Stock) econ.Material {
	best, most := econ.Slag, 0.0
	for m := econ.Material(0); m < econ.Count; m++ {
		if s[m] > most {
			best, most = m, s[m]
		}
	}
	return best
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// teamRGBA is the colour a power flies under, for the standings bars.
func teamRGBA(g govt.Color) color.RGBA {
	return render.TeamColor(teamOf(g), true)
}
