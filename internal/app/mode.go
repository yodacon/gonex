package app

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/ui"
	"yodacon.org/gonex/internal/world"
)

// appMode is which view owns the screen: the classic top-down flight view,
// the warp tunnel, the reentry cockpit, or the landed spaceport.
type appMode int

const (
	modeFlight appMode = iota
	modeWarp
	modeDeorbit
	modeEntry
	modeLanded
)

func (m appMode) String() string {
	switch m {
	case modeWarp:
		return "WARP"
	case modeDeorbit:
		return "DEORBIT"
	case modeEntry:
		return "ENTRY"
	case modeLanded:
		return "DOCKED"
	}
	return "FLIGHT"
}

// Docking is a handshake, not a keypress: L near a port sends the request,
// control answers after a beat, and a second L inside the clearance window
// commits the deorbit.
type dockingRequest struct {
	stellar int
	wait    float64 // seconds until control answers
	granted float64 // clearance window remaining once granted
}

func (a *App) clearDockingRequest() { a.docking = nil }

// requestDocking is L in flight near a stellar.
func (a *App) requestDocking(id int) {
	st := a.gal.Stellars[id]
	switch {
	case a.docking == nil || a.docking.stellar != id:
		a.docking = &dockingRequest{stellar: id, wait: 1.6}
		a.Console.Notifyf("Hailing %s control — requesting docking…", st.Name)
	case a.docking.wait > 0:
		a.Console.Notifyf("%s control has not answered yet.", st.Name)
	default:
		a.startDeorbit(id)
		a.docking = nil
	}
}

// updateDocking runs the handshake clock while flying.
func (a *App) updateDocking() {
	d := a.docking
	if d == nil {
		return
	}
	if a.nearbyStellar() != d.stellar {
		a.Console.Notifyf("Docking request to %s lapsed — out of approach range.",
			a.gal.Stellars[d.stellar].Name)
		a.docking = nil
		return
	}
	if d.wait > 0 {
		d.wait -= dt
		if d.wait <= 0 {
			d.granted = 30
			a.Console.Notifyf("%s control: pad assigned — cleared to land. Press L to commit deorbit.",
				a.gal.Stellars[d.stellar].Name)
		}
		return
	}
	if d.granted -= dt; d.granted <= 0 {
		a.Console.Notifyf("Clearance window expired — request docking again.")
		a.docking = nil
	}
}

// drawModeBanner is the always-on state line: which mode owns the screen,
// where you are, and what the game is waiting on.
func (a *App) drawModeBanner(screen *ebiten.Image) {
	if a.voy == nil {
		return
	}
	sys := a.gal.Systems[a.voy.System]
	detail := ""
	switch a.mode {
	case modeFlight:
		switch {
		case a.docking != nil && a.docking.wait > 0:
			detail = " · docking: awaiting clearance"
		case a.docking != nil:
			detail = fmt.Sprintf(" · CLEARED TO LAND — L commits (%.0fs)", a.docking.granted)
		default:
			if next := a.voy.NextJump(); next >= 0 {
				detail = fmt.Sprintf(" · course: %s — fly to the beacon, J jumps",
					a.gal.Systems[next].Name)
			} else if id := a.nearbyStellar(); id > 0 {
				detail = fmt.Sprintf(" · in approach range of %s — L requests docking",
					a.gal.Stellars[id].Name)
			}
		}
	case modeEntry:
		if a.entry != nil {
			detail = " · " + a.gal.Stellars[a.entry.stellar].Name
		}
	case modeLanded:
		if a.dock != nil {
			detail = " · " + a.gal.Stellars[a.dock.stellar].Name
		}
	}
	line := fmt.Sprintf("[ %s ] %s · day %d%s", a.mode, sys.Name, a.voy.Day, detail)
	w := float32(7*len([]rune(line)) + 16)
	vector.DrawFilledRect(screen, float32(ScreenW)/2-w/2, 4, w, 20,
		premul(color.RGBA{5, 7, 10, 255}, 0.78), false)
	ui.DrawText(screen, line, float64(ScreenW)/2-float64(w)/2+8, 7, 1)
}

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


// updateDemo is the scripted pilot behind GONEX_BOOT "demo <spob>": it
// flies the real docking handshake, and the entry/dock code advances it
// through touchdown to the spaceport, then quits.
func (a *App) updateDemo() {
	if a.demoStellar <= 0 || a.mode != modeFlight {
		return
	}
	a.demoT += dt
	switch {
	case a.docking == nil && a.demoT > 0.9:
		a.requestDocking(a.demoStellar)
	case a.docking != nil && a.docking.wait <= 0 && a.docking.granted > 0:
		a.requestDocking(a.demoStellar) // clearance in hand: commit
	}
}
