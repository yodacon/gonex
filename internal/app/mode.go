package app

import (
	"math"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/world"
)

// appMode is which view owns the screen: the classic top-down flight view,
// the reentry cockpit, or the landed spaceport.
type appMode int

const (
	modeFlight appMode = iota
	modeEntry
	modeLanded
)

// enterSystem rebuilds the world's scenery for a system: its stellars from
// the gazetteer become dockable planets, ringed around the map center.
func (a *App) enterSystem(sysID int) {
	sys := a.gal.Systems[sysID]
	if sys == nil || a.World == nil {
		return
	}
	live := a.World.Entities[:0]
	for _, e := range a.World.Entities {
		if _, isPlanet := e.(*world.Planet); !isPlanet {
			live = append(live, e)
		}
	}
	a.World.Entities = live

	stellars := a.gal.StellarsIn(sysID)
	cx, cy := a.World.MapW/2, a.World.MapH/2
	for i, id := range stellars {
		ang := 2 * math.Pi * float64(i) / float64(len(stellars))
		r := a.World.MapW * 0.22
		st := a.gal.Stellars[id]
		a.World.Add(&world.Planet{
			Body:      world.Body{P: gmath.V(cx+math.Cos(ang)*r, cy+math.Sin(ang)*r)},
			SpriteID:  1 + st.Sprite%18,
			StellarID: id,
		})
	}
	a.Console.Notifyf("Arrived: %s system (%s) — %d stellar(s)",
		sys.Name, sys.Govt, len(stellars))
}

// nearbyStellar finds a dockable planet within landing range of the player.
func (a *App) nearbyStellar() int {
	p := a.World.MainPlayer
	if p == nil {
		return 0
	}
	for _, e := range a.World.Entities {
		if pl, ok := e.(*world.Planet); ok && pl.StellarID > 0 {
			if pl.Pos().Sub(p.Pos()).Len() < world.CollisionRange*4 {
				return pl.StellarID
			}
		}
	}
	return 0
}

// drainNotices moves voyage messages onto the console.
func (a *App) drainNotices() {
	if a.voy == nil {
		return
	}
	for _, n := range a.voy.Notices {
		a.Console.Notifyf("%s", n)
	}
	a.voy.Notices = nil
}
