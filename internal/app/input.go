package app

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// handlePlayerInput drives the main player's ship from the keyboard, matching
// konex's bindings: arrows steer and thrust, space fires, keypad-0 (or X)
// brakes, left shift targets the nearest enemy, left ctrl toggles autotarget.
func (a *App) handlePlayerInput() {
	p := a.World.MainPlayer
	if p == nil {
		return
	}
	w := a.World

	if ebiten.IsKeyPressed(ebiten.KeyArrowLeft) {
		p.TurnLeft(w, dt)
		p.Autotarget = false
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowRight) {
		p.TurnRight(w, dt)
		p.Autotarget = false
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowUp) {
		p.Thrust(w, dt)
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowDown) {
		p.Reverse(w, dt)
	}
	if ebiten.IsKeyPressed(ebiten.KeyKP0) || ebiten.IsKeyPressed(ebiten.KeyX) {
		p.Slow(dt)
	}
	if ebiten.IsKeyPressed(ebiten.KeySpace) {
		p.Fire(w)
	}
	if ebiten.IsKeyPressed(ebiten.KeyShiftLeft) {
		p.Target = w.ClosestEnemy(p)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyControlLeft) {
		p.Autotarget = !p.Autotarget
	}

	// the trader's keys: chart, jump, land
	if inpututil.IsKeyJustPressed(ebiten.KeyM) {
		a.toggleWindow(a.galaxyWin)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyJ) && a.voy != nil {
		a.tryJump()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyL) && a.voy != nil {
		if id := a.nearbyStellar(); id > 0 {
			a.requestDocking(id)
		} else {
			a.Console.Notifyf("No port in approach range — fly closer to a planet.")
		}
	}
}
