package ui

import (
	"image/color"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	menuItemHeight = 40
	menuPadding    = 10
	menuFadeTime   = 0.2
)

// MenuItem is one button. If Input is non-nil the item is a text field bound
// to that string (player name, server address).
type MenuItem struct {
	Caption string
	Action  func()
	Input   *string

	focus     bool
	mouseDown bool
}

// Menu renders a page of buttons inside a window, with konex's fade-in.
type Menu struct {
	items     []*MenuItem
	fadeStart time.Time
	runes     []rune
}

// SetItems replaces the page and restarts the fade.
func (m *Menu) SetItems(items ...*MenuItem) {
	m.items = items
	m.fadeStart = time.Now()
}

func (m *Menu) alpha() float32 {
	elapsed := time.Since(m.fadeStart).Seconds()
	if elapsed >= menuFadeTime {
		return 1
	}
	return float32(elapsed / menuFadeTime)
}

// Draw renders the page and handles its input. The window grows to fit.
func (m *Menu) Draw(win *Window, dst *ebiten.Image) {
	alpha := m.alpha()
	mx, my := ebiten.CursorPosition()
	pressed := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)

	m.handleTyping()

	for i, item := range m.items {
		x := win.X + menuPadding
		y := win.Y + menuPadding + i*(menuItemHeight+menuPadding)
		w := win.W - menuPadding*2

		over := mx > x && mx < x+w && my > y && my < y+menuItemHeight
		c := color.RGBA{48, 48, 48, 255}
		if over {
			c = color.RGBA{64, 64, 64, 255}
			if pressed {
				item.mouseDown = true
			} else if item.mouseDown {
				item.mouseDown = false
				if item.Input == nil {
					if item.Action != nil {
						item.Action()
					}
				} else {
					item.focus = true
				}
			}
		} else {
			item.mouseDown = false
			if pressed {
				item.focus = false
			}
		}
		if item.focus {
			c = color.RGBA{96, 96, 96, 255}
		}
		c.A = uint8(255 * alpha)

		vector.DrawFilledRect(dst, float32(x), float32(y), float32(w), menuItemHeight, c, false)
		DrawText(dst, item.Caption, float64(x)+10, float64(y)+12, alpha)
		if item.Input != nil {
			val := *item.Input
			if item.focus {
				val += "_"
			}
			DrawText(dst, val, float64(x+w/2), float64(y)+12, alpha)
		}

		win.SH = y - win.SY + menuItemHeight + menuPadding + 7
	}
}

// handleTyping feeds keyboard input into whichever item has focus.
func (m *Menu) handleTyping() {
	var focused *MenuItem
	for _, item := range m.items {
		if item.focus && item.Input != nil {
			focused = item
			break
		}
	}
	m.runes = ebiten.AppendInputChars(m.runes[:0])
	if focused == nil {
		return
	}
	for _, r := range m.runes {
		if r >= 32 && len(*focused.Input) < 60 {
			*focused.Input += string(r)
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(*focused.Input) > 0 {
		*focused.Input = (*focused.Input)[:len(*focused.Input)-1]
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		focused.focus = false
	}
}
