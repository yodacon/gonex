package app

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"

	"yodacon.org/gonex/internal/ui"
	"yodacon.org/gonex/internal/world"
)

// buildWindows creates the window set konex assembled in interface_Init.
func (a *App) buildWindows() {
	var fps ui.FPSGraph

	a.miniMapWin = a.wm.Add(&ui.Window{
		Title: "Mini Map", SX: ScreenW - 200, SY: 1, SW: 199, SH: 199,
		DrawFn: a.drawMap,
		OnClose: func() {
			a.Cfg.ShowMiniMap = !a.Cfg.ShowMiniMap
			a.miniMapWin.Visible = a.Cfg.ShowMiniMap
		},
	})
	a.targetWin = a.wm.Add(&ui.Window{
		Title: "Target", SX: ScreenW - 150, SY: ScreenH - 321, SW: 149, SH: 320,
		DrawFn: a.drawTargetWindow,
		OnClose: func() {
			a.Cfg.ShowTarget = !a.Cfg.ShowTarget
			a.targetWin.Visible = a.Cfg.ShowTarget
		},
	})
	a.fullMapWin = a.wm.Add(&ui.Window{
		Title: "Full Map", SX: 50, SY: 50, SW: ScreenW - 248, SH: ScreenH - 100,
		DrawFn:  a.drawMap,
		OnClose: func() { a.fullMapWin.Visible = false },
	})
	a.galaxyWin = a.wm.Add(&ui.Window{
		Title: "Galaxy Chart", SX: 80, SY: 40, SW: ScreenW - 320, SH: ScreenH - 120,
		DrawFn:  a.drawGalaxyMap,
		OnClose: func() { a.galaxyWin.Visible = false },
	})
	a.hudWin = a.wm.Add(&ui.Window{
		Title: "HUD", SX: ScreenW - 150, SY: 210, SW: 149, SH: 180,
		DrawFn: a.drawHUD,
		OnClose: func() {
			a.Cfg.ShowHUD = !a.Cfg.ShowHUD
			a.hudWin.Visible = a.Cfg.ShowHUD
		},
	})
	a.shipSelectWin = a.wm.Add(&ui.Window{
		Title: "Select Ship", SX: 120, SY: 80, SW: ScreenW - 240, SH: ScreenH - 160,
		DrawFn:  a.drawShipSelect,
		OnClose: func() { a.shipSelectWin.Visible = false },
	})
	a.menuWin = a.wm.Add(&ui.Window{
		Title: fmt.Sprintf("Welcome to %s %s", AppName, AppVersion),
		SX:    300, SY: 192, SW: ScreenW - 600, SH: ScreenH - 384,
		Visible: true,
		DrawFn:  a.menu.Draw,
		OnClose: a.toggleMenu,
	})
	a.fpsWin = a.wm.Add(&ui.Window{
		Title: "", SX: 1, SY: ScreenH - 101, SW: 138, SH: 100,
		Visible: a.Cfg.ShowFPS,
		DrawFn:  fps.Draw,
		OnClose: a.toggleFPS,
	})
}

func (a *App) toggleWindow(w *ui.Window) { w.Visible = !w.Visible }

func (a *App) toggleFPS() {
	a.Cfg.ShowFPS = !a.Cfg.ShowFPS
	a.fpsWin.Visible = a.Cfg.ShowFPS
}

func (a *App) hideMenu() {
	a.menuWin.Visible = false
	a.paused = false
}

func (a *App) toggleMenu() {
	a.menuWin.Visible = !a.menuWin.Visible
	a.paused = a.menuWin.Visible
	if a.menuWin.Visible {
		a.showMenuFor(a.running())
	}
}

// setGameStatus flips the interface between menu mode and in-game mode.
func (a *App) setGameStatus(inGame bool) {
	if inGame {
		a.miniMapWin.Visible = a.Cfg.ShowMiniMap
		a.hudWin.Visible = a.Cfg.ShowHUD
		a.targetWin.Visible = a.Cfg.ShowTarget
		a.menuWin.Visible = false
		a.paused = false
	} else {
		for _, w := range []*ui.Window{
			a.shipSelectWin, a.miniMapWin, a.hudWin,
			a.fullMapWin, a.targetWin, a.galaxyWin,
		} {
			w.Visible = false
		}
		a.menuWin.Visible = true
	}
	a.showMenuFor(inGame)
}

// --- window content ---

func (a *App) drawMap(win *ui.Window, dst *ebiten.Image) {
	if a.running() {
		a.Renderer.DrawMap(dst, a.World, win.X, win.Y, win.W, win.H)
	}
}

func (a *App) drawHUD(win *ui.Window, dst *ebiten.Image) {
	if !a.running() || a.World.MainPlayer == nil {
		return
	}
	p := a.World.MainPlayer
	x, y := float64(win.X), float64(win.Y)
	ui.DrawText(dst, fmt.Sprintf("Red: %d", a.World.Scores[world.TeamRed]), x, y, 1)
	ui.DrawText(dst, fmt.Sprintf("Green: %d", a.World.Scores[world.TeamGreen]), x, y+20, 1)
	ui.DrawText(dst, fmt.Sprintf("Blue: %d", a.World.Scores[world.TeamBlue]), x, y+40, 1)
	ui.DrawText(dst, fmt.Sprintf("Frags: %d", p.Frags), x, y+80, 1)
	ui.DrawText(dst, fmt.Sprintf("Deaths: %d", p.Deaths), x, y+100, 1)
	ui.DrawText(dst, fmt.Sprintf("Health: %d", p.Health), x, y+120, 1)
	ui.DrawText(dst, fmt.Sprintf("Money: %d", p.Money), x, y+140, 1)
}

func (a *App) drawTargetWindow(win *ui.Window, dst *ebiten.Image) {
	if !a.running() || a.World.ViewShip == nil || a.World.ViewShip.Target == nil {
		return
	}
	t := a.World.ViewShip.Target
	spec := a.Catalog.Get(t.ShipID)

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(win.X), float64(win.Y))
	dst.DrawImage(spec.Comm, op)

	x, y := float64(win.X), float64(win.Y)
	ui.DrawText(dst, spec.Name, x, y+140, 1)
	ui.DrawText(dst, fmt.Sprintf("Health: %d", t.Health), x, y+160, 1)
	ui.DrawText(dst, fmt.Sprintf("Crew: %d", t.Crew), x, y+180, 1)
	ui.DrawText(dst, fmt.Sprintf("Team: %s", t.Team), x, y+200, 1)

	op = &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(win.X), float64(win.Y)+220)
	dst.DrawImage(spec.Target, op)
}

func (a *App) drawShipSelect(win *ui.Window, dst *ebiten.Image) {
	const cell, padX, padY = 128, 50, 50

	mx, my := ebiten.CursorPosition()
	clicked := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)

	curX, curY := win.X+padX, win.Y
	for id := 1; id <= a.Catalog.Count(); id++ {
		spec := a.Catalog.Get(id)
		over := mx > curX && mx < curX+cell && my > curY && my < curY+cell

		op := &ebiten.DrawImageOptions{}
		b := spec.Yard.Bounds()
		op.GeoM.Scale(cell/float64(b.Dx()), cell/float64(b.Dy()))
		op.GeoM.Translate(float64(curX), float64(curY))
		if !over {
			op.ColorScale.ScaleAlpha(0.5)
		}
		dst.DrawImage(spec.Yard, op)

		ui.DrawText(dst, spec.Name, float64(curX), float64(curY+cell)-10, 1)
		ui.DrawText(dst, fmt.Sprintf("Speed: %0.1f km/sec", spec.MaxVelocity/10),
			float64(curX+8), float64(curY+cell)+14, 1)
		ui.DrawText(dst, fmt.Sprintf("Accel: %0.1f km/sec2", spec.Acceleration/10),
			float64(curX+8), float64(curY+cell)+30, 1)
		ui.DrawText(dst, fmt.Sprintf("Mass: %0.1f kg", spec.Mass),
			float64(curX+8), float64(curY+cell)+46, 1)

		if over && clicked {
			if a.running() && a.World.MainPlayer != nil {
				a.World.MainPlayer.ShipID = id
			}
			a.Cfg.PlayerShipID = id
			a.shipSelectWin.Visible = false
		}

		curX += cell + padX
		if curX > win.X+win.W-cell-padX {
			curX = win.X + padX
			curY += cell + padY
		}
	}
}
