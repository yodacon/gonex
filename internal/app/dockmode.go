package app

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/assets"
	"yodacon.org/gonex/internal/mission"
	"yodacon.org/gonex/internal/power"
	"yodacon.org/gonex/internal/reentry"
	"yodacon.org/gonex/internal/ui"
)

// The landed spaceport screen: everything a docked pilot can do, one
// keypress each. The bar is where the 1997 mission machine surfaces its
// offers.

type dockView int

const (
	dockMain dockView = iota
	dockBar
	dockTrade
	dockOutfit
)

type dockState struct {
	stellar int
	view    dockView
	offers  []mission.Def
	rolled  bool
}

const lumberCap = 100 // tons of deck cargo the Yodacon spares for trade

func lumberPrice(system int) int { return 100 + (system*37)%160 }

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
		if !d.rolled {
			d.offers = a.msn.OffersAt(d.stellar, &v.Bits, v.Active, v.Rng)
			d.rolled = true
		}
		for i, def := range d.offers {
			if i < 9 && inpututil.IsKeyJustPressed(ebiten.KeyDigit1+ebiten.Key(i)) {
				act := mission.Accept(def, d.stellar, confederationStellars(a.gal), v.Rng)
				v.Active = append(v.Active, act)
				a.Console.Notifyf("Accepted: %s", def.Name)
				d.offers = append(d.offers[:i], d.offers[i+1:]...)
				break
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyB) {
			d.view = dockMain
		}
	case dockTrade:
		price := lumberPrice(a.gal.Stellars[d.stellar].System)
		if inpututil.IsKeyJustPressed(ebiten.KeyEqual) || inpututil.IsKeyJustPressed(ebiten.KeyUp) {
			if v.Credits >= price && v.Lumber < lumberCap {
				v.Credits -= price
				v.Lumber++
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyMinus) || inpututil.IsKeyJustPressed(ebiten.KeyDown) {
			if v.Lumber > 0 {
				v.Credits += price
				v.Lumber--
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
	default:
		switch {
		case inpututil.IsKeyJustPressed(ebiten.KeyB):
			d.view = dockBar
		case inpututil.IsKeyJustPressed(ebiten.KeyT):
			d.view = dockTrade
		case inpututil.IsKeyJustPressed(ebiten.KeyO):
			d.view = dockOutfit
		case ebiten.IsKeyPressed(ebiten.KeyR):
			// hold to refuel: the meters walk
			if v.Fuel < v.FuelMax && v.Credits >= fuelPrice {
				v.Fuel++
				v.Credits -= fuelPrice
			} else if v.Lithium < v.LiMax-0.1 && v.Credits >= liPrice {
				v.Lithium += 0.1
				v.Credits -= liPrice / 10
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyY):
			if cost := v.RepairCost(); cost > 0 && v.Credits >= cost {
				v.Credits -= cost
				v.Dmg = reentry.Damage{}
				a.Console.Notifyf("Repairs complete: %d cr", cost)
			}
		case inpututil.IsKeyJustPressed(ebiten.KeyG):
			a.Console.Notifyf("The gaming annex is packed. (Charisma tables: Phase 5.)")
		case inpututil.IsKeyJustPressed(ebiten.KeyL):
			a.mode = modeFlight
			a.dock = nil
			a.setGameStatus(true)
			a.Console.Notifyf("Cleared for orbit.")
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
	ui.DrawText(screen, fmt.Sprintf("credits %d · fuel %d/%d · lithium %.1f kg · hull %.0f%% · lumber %d t",
		v.Credits, v.Fuel, v.FuelMax, v.Lithium, 100-v.Dmg.Hull, v.Lumber), x, y+40, 0.8)

	switch d.view {
	case dockBar:
		ui.DrawText(screen, "SPACEPORT BAR — press a number to take a contract, B to leave", x, y+90, 1)
		if len(d.offers) == 0 {
			ui.DrawText(screen, "Nobody here has work for you today.", x, y+120, 0.8)
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
	case dockTrade:
		price := lumberPrice(st.System)
		ui.DrawText(screen, "TRADE CENTER — lumber only, for now", x, y+90, 1)
		ui.DrawText(screen, fmt.Sprintf("lumber %d cr/t here · aboard %d/%d t", price, v.Lumber, lumberCap), x, y+120, 0.9)
		ui.DrawText(screen, "+ / - to buy and sell · T to leave", x, y+140, 0.7)
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
	default:
		opts := []string{
			"B  Spaceport Bar    — contracts and rumors",
			"R  Refuel (hold)    — jump fuel, then lithium",
			"T  Trade Center     — the commodity spreadsheet",
			"O  Outfitter        — generators, batteries, capacitors, radiators",
			fmt.Sprintf("Y  Shipyard repairs — %d cr outstanding", v.RepairCost()),
			"G  Gaming Annex     — queue-jumping charisma",
			"L  Leave            — launch to orbit",
		}
		for i, o := range opts {
			ui.DrawText(screen, o, x, y+100+float64(i)*24, 1)
		}
		if n := len(v.Active); n > 0 {
			ui.DrawText(screen, fmt.Sprintf("ACTIVE MISSIONS (%d):", n), x, y+270, 0.9)
			for i, act := range v.Active {
				dest := "—"
				if s := a.gal.Stellars[act.Dest()]; s != nil {
					dest = fmt.Sprintf("%s (%s)", s.Name, a.gal.Systems[s.System].Name)
				}
				days := ""
				if act.DaysLeft >= 0 {
					days = fmt.Sprintf(" · %d days left", act.DaysLeft)
				}
				ui.DrawText(screen, fmt.Sprintf("  %s → %s%s", act.Def.Name, dest, days), x, y+292+float64(i)*18, 0.8)
			}
		}
	}
}
