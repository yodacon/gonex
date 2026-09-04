package app

import (
	"fmt"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/market"
	"yodacon.org/gonex/internal/universe"
	"yodacon.org/gonex/internal/world"
)

// The counter: where the player's hold meets the same warehouse the AI
// freighters are emptying.
//
// Before this, the commodity board was a pure function of (station, day) and
// the player's hold was outside the books entirely — you could stand at a
// counter and buy a thousand tons of chips off a world that had never made
// one, out of a warehouse that never noticed. Two economies running side by
// side and disagreeing.
//
// Now there is one. The price on the board is the price the freighters are
// paying, the tons come out of a real warehouse and go into a hold the
// auditor can see, and a port that has been stripped by a rival's haulers
// has nothing to sell you.

// shopPrice is what this port pays per ton today.
//
// It falls back to the old hash-and-day market when the universe has no
// record of a stellar — a scene can place a world the gazetteer never listed,
// and a missing port must read as "the old prices" rather than as a crash or
// a free lunch.
func (a *App) shopPrice(stellar, commodity int) int {
	if w := a.uniWorld(stellar); w != nil {
		if p := w.Shop[econ.Material(commodity)]; p > 0 {
			return p
		}
	}
	st := a.gal.Stellars[stellar]
	if st == nil || a.voy == nil {
		return market.Commodities[commodity].Base
	}
	return market.Price(st.System, stellar, commodity, a.voy.Day, a.voy.Events)
}

// uniWorld is the economic record for a stellar, or nil.
func (a *App) uniWorld(stellar int) *universe.World {
	if a.uni == nil {
		return nil
	}
	return a.uni.Worlds[stellar]
}

// onHand is how many tons of a commodity this port can actually sell. A
// warehouse is a real quantity now, and "sold out" is a thing that happens.
func (a *App) onHand(stellar, commodity int) int {
	w := a.uniWorld(stellar)
	if w == nil {
		return overstuffCap // no record: the old unlimited counter
	}
	return int(w.Warehouse[econ.Material(commodity)])
}

// buyTons moves tons from the port's warehouse into the player's hold and
// charges for them. It returns how many actually changed hands, which is not
// always what was asked for: the hold has a ceiling, the credits run out, and
// the warehouse can simply be empty.
func (a *App) buyTons(stellar, commodity, want int) int {
	v := a.voy
	if v == nil || want <= 0 {
		return 0
	}
	price := a.shopPrice(stellar, commodity)
	got := 0
	for got < want {
		if v.Credits < price || v.CargoTotal() >= overstuffCap {
			break
		}
		if uw := a.uniWorld(stellar); uw != nil {
			// Take the ton out of the warehouse FIRST and buy only what the
			// take returned. Charging for a ton and then discovering the
			// shelf was empty is exactly how matter gets minted.
			if uw.Warehouse.Take(econ.Material(commodity), 1) < 1 {
				break
			}
		}
		v.Credits -= price
		v.Cargo[commodity]++
		got++
	}
	if got > 0 {
		a.syncPlanetStock()
	}
	return got
}

// sellTons is the same transaction the other way. The tons go back into the
// port's warehouse, where its factories and its rivals' freighters can have
// them — selling into a market genuinely supplies it.
func (a *App) sellTons(stellar, commodity, want int) int {
	v := a.voy
	if v == nil || want <= 0 {
		return 0
	}
	price := a.shopPrice(stellar, commodity)
	got := 0
	for got < want && v.Cargo[commodity] > 0 {
		v.Cargo[commodity]--
		v.Credits += price
		if uw := a.uniWorld(stellar); uw != nil {
			uw.Warehouse.Add(econ.Material(commodity), 1)
		}
		got++
	}
	if got > 0 {
		if uw := a.uniWorld(stellar); uw != nil {
			uw.Reprice() // a big sale moves the price you get for the next one
		}
		a.syncPlanetStock()
	}
	return got
}

// playerHold lifts the six-wide cargo manifest into a material vector, so the
// auditor can count what the player is carrying. Registered with the universe
// at seeding; see universe.Account.
func (a *App) playerHold() econ.Stock {
	if a.voy == nil {
		return econ.Stock{}
	}
	return econ.FromBoard(a.voy.Cargo)
}

// --- T3: the warehouse is the planet's stock -----------------------------

// syncPlanetStock copies each port's board-tier warehouse onto the planet
// standing in the sky, so the war economy and the trade economy are looking
// at ONE number.
//
// The war economy gave every planet a `Stock` slice and then never filled it;
// it has been an empty six-element array since the day it was written. This
// is what it was for. A yard's hull plates come out of the ore in its
// warehouse, so a world whose ore has been hauled away by somebody else's
// freighters cannot patch the squadron flying out of it — the supply line the
// war economy made cuttable is now cuttable by TRADE as well as by guns.
func (a *App) syncPlanetStock() {
	if a.uni == nil || a.World == nil {
		return
	}
	for _, e := range a.World.Entities {
		pl, ok := e.(*world.Planet)
		if !ok || pl.StellarID <= 0 {
			continue
		}
		uw := a.uni.Worlds[pl.StellarID]
		if uw == nil {
			continue
		}
		if pl.Stock == nil {
			pl.Stock = make([]int, world.CommodityCount)
		}
		for i := 0; i < world.CommodityCount && i < len(pl.Stock); i++ {
			pl.Stock[i] = int(uw.Warehouse[econ.Material(i)])
		}
	}
}

// drainPlanetStock pushes the war economy's consumption back the other way:
// tons the pads actually burned patching hulls leave the universe's warehouse
// too. Without this the sync is a one-way display and the yards repair out of
// a warehouse that never goes down.
func (a *App) drainPlanetStock() {
	if a.uni == nil || a.World == nil {
		return
	}
	for _, e := range a.World.Entities {
		pl, ok := e.(*world.Planet)
		if !ok || pl.StellarID <= 0 || pl.PlateDraw <= 0 {
			continue
		}
		uw := a.uni.Worlds[pl.StellarID]
		if uw == nil {
			pl.PlateDraw = 0
			continue
		}
		econ.Consume(&uw.Warehouse, &a.uni.Sink, econ.Ore, pl.PlateDraw)
		pl.PlateDraw = 0
	}
}

// tradeLine renders one row of the commodity board: what it costs, what the
// port has, and what you are carrying.
func (a *App) tradeLine(stellar, commodity int) string {
	have := a.onHand(stellar, commodity)
	stock := fmt.Sprintf("%6d t", have)
	if have <= 0 {
		stock = "  SOLD OUT"
	}
	return stock
}
