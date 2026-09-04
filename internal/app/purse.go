package app

import (
	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/universe"
	"yodacon.org/gonex/internal/world"
)

// The player's purse is a purse on the ledger like anybody's. Everything the
// player pays a port goes INTO that port's treasury, and everything a port
// pays the player comes OUT of it — so a landing bonus is paid by the port
// that gave it, a repair bill funds the yard that did the work, and a world
// the player has been selling into all week can genuinely run out of money
// to buy with. When a stellar has no economic record (a scene can place a
// world the gazetteer never listed) the old behaviour stands: the credits
// simply move, because there is no treasury to move them against.

// playerPurse is the player's credits, for the ledger.
func (a *App) playerPurse() int {
	if a.voy == nil {
		return 0
	}
	return a.voy.Credits
}

// playerColour is the colour the player flies under.
func (a *App) playerColour() govt.Color {
	if a.Cfg == nil {
		return govt.None
	}
	return teamColor(world.Team(a.Cfg.Team))
}

// payPort moves n credits from the player to the port's treasury. It refuses
// — moving nothing — when the player cannot cover it.
func (a *App) payPort(stellar, n int) bool {
	v := a.voy
	if v == nil || n < 0 || v.Credits < n {
		return false
	}
	if uw := a.uniWorld(stellar); uw != nil {
		econ.Pay(&v.Credits, &uw.Credits, n)
	} else {
		v.Credits -= n
	}
	return true
}

// portPays moves up to n credits from the port's treasury to the player and
// returns how many actually came. A broke port pays what it has.
func (a *App) portPays(stellar, n int) int {
	v := a.voy
	if v == nil || n <= 0 {
		return 0
	}
	if uw := a.uniWorld(stellar); uw != nil {
		return econ.Pay(&uw.Credits, &v.Credits, n)
	}
	v.Credits += n
	return n
}

// stake sets the player's opening credits to `want`, paid out of the home
// treasury of the player's colour rather than out of the sky. A trader
// starts on margin, and the margin is somebody's.
func (a *App) stake(want int) {
	v := a.voy
	if v == nil || a.uni == nil {
		if v != nil {
			v.Credits = want
		}
		return
	}
	// Whatever the voyage was created holding goes back first, so the
	// ledger sees one movement and not an invented balance.
	home := a.uni.Capital(a.playerColour())
	if home == nil {
		for _, id := range a.uni.Order() {
			if w := a.uni.Worlds[id]; home == nil || w.Pop > home.Pop {
				home = w
			}
		}
	}
	if home == nil {
		v.Credits = want
		return
	}
	if v.Credits > want {
		econ.Pay(&v.Credits, &home.Credits, v.Credits-want)
	} else {
		econ.Pay(&home.Credits, &v.Credits, want-v.Credits)
	}
}

// seatFor reports whether the player may govern a world: they hold its
// charter, or nobody has built there yet and it will trade with them.
func (a *App) seatFor(w *universe.World) bool {
	if w == nil {
		return false
	}
	if w.Seat == universe.SeatPlayer {
		return true
	}
	c := a.playerColour()
	return w.Govt == c || w.Govt == govt.None
}
