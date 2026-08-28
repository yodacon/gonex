package app

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/save"
	"yodacon.org/gonex/internal/ui"
)

// The DED screen. The ship broke up in the plasma; the pilot file did not.
// A beat of silence, then any key resumes from the last berth save — every
// landing writes one — or falls back to the menu when no save exists.

func (a *App) updateDead() {
	a.deadT += dt
	if a.deadT < 1.2 {
		return
	}
	if len(inpututil.AppendJustPressedKeys(nil)) == 0 {
		return
	}
	if save.Exists(save.DefaultPath) {
		a.loadGame()
		a.Console.Notifyf("Resumed from the last berth save. The %s flies again.",
			a.Catalog.Get(a.Cfg.PlayerShipID).Name)
	} else {
		a.endGame()
	}
}

func (a *App) drawDead(screen *ebiten.Image) {
	screen.Fill(color.RGBA{4, 2, 3, 255})

	// a red recombination afterglow, guttering out
	glow := math.Max(0, 0.35-a.deadT*0.06)
	if glow > 0 {
		vector.DrawFilledCircle(screen, ScreenW/2, 300, 260,
			premul(color.RGBA{120, 24, 18, 255}, glow), false)
	}

	// DED: 3 glyphs of the 7px face at 12x
	const scale = 12
	w := 3 * 7 * scale
	fade := float32(math.Min(a.deadT/0.8, 1))
	ui.DrawTextScaled(screen, "DED", float64(ScreenW-w)/2, 220, scale,
		color.RGBA{255, 72, 58, 255}, fade)

	if a.deadT > 1.2 {
		place := a.deadPlace
		if place == "" {
			place = "the corridor"
		}
		ui.DrawText(screen, "the "+a.Catalog.Get(a.Cfg.PlayerShipID).Name+
			" broke up over "+place, float64(ScreenW)/2-180, 420, 0.9)
		hint := "press any key — resume from the last berth save"
		if !save.Exists(save.DefaultPath) {
			hint = "no pilot file on disk — press any key"
		}
		blink := float32(0.5 + 0.5*math.Abs(math.Sin(a.deadT*2)))
		ui.DrawText(screen, hint, float64(ScreenW)/2-180, 448, blink)
	}
}
