package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/console"
)

const (
	consoleHeight = 270
	consoleAlpha  = 160
)

// ConsoleView renders the drop-down console and feeds it keyboard input.
type ConsoleView struct {
	Console *console.Console
	runes   []rune
}

// Update animates the drop-down and, when open, routes input to the console.
// The backquote toggle is handled by the caller (it is a global key).
func (v *ConsoleView) Update(dt float64) {
	c := v.Console
	c.Animate(dt, consoleHeight)
	if c.State != console.Shown {
		return
	}
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyPageUp):
		c.ScrollBack()
	case inpututil.IsKeyJustPressed(ebiten.KeyPageDown):
		c.ScrollForward()
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowUp):
		c.RecallLast()
	case inpututil.IsKeyJustPressed(ebiten.KeyBackspace):
		c.Backspace()
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter):
		c.Execute()
	}
	v.runes = ebiten.AppendInputChars(v.runes[:0])
	for _, r := range v.runes {
		if r >= 32 && r != '`' && r != '~' {
			c.Append(string(r))
		}
	}
}

// DrawNotify shows the fading notification feed (only while the console is
// closed, like the original).
func (v *ConsoleView) DrawNotify(dst *ebiten.Image, dt float64) {
	if v.Console.State != console.Hidden {
		return
	}
	for i, line := range v.Console.ActiveNotifications(dt) {
		DrawText(dst, line, 32, float64(32+i*20), 1)
	}
}

func (v *ConsoleView) Draw(dst *ebiten.Image, screenW int) {
	c := v.Console
	if c.State == console.Hidden {
		return
	}
	vector.DrawFilledRect(dst, 0, 0, float32(screenW), float32(c.Bottom),
		color.RGBA{128, 128, 128, consoleAlpha}, false)

	offset := c.Bottom - 40
	for i, line := range c.Lines() {
		DrawText(dst, line, 10, offset-float64(i*LineHeight), 1)
	}
	DrawText(dst, "> "+c.Cmd+"_", 10, offset+LineHeight, 1)
}
