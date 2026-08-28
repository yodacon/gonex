package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	titleHeight = 15
	padding     = 5
	closeSize   = 10
)

// Window is a draggable frame with a title bar, close box and a content
// callback, ported from konex's interface module.
type Window struct {
	Title   string
	SX, SY  int // outer position
	SW, SH  int // outer size
	Visible bool

	// Content rect, updated every frame before DrawFn runs.
	X, Y, W, H int

	DrawFn  func(win *Window, dst *ebiten.Image)
	OnClose func()

	moving             bool
	moveOffX, moveOffY int
}

func (w *Window) contains(mx, my int) bool {
	return mx > w.SX && mx < w.SX+w.SW && my > w.SY && my < w.SY+w.SH
}

func (w *Window) overClose(mx, my int) bool {
	rx, ry := mx-w.SX, my-w.SY
	return rx >= w.SW-closeSize-2 && rx <= w.SW-2 && ry >= 2 && ry <= 2+closeSize
}

// Manager owns the window list. Order is draw order (last = topmost); input
// is tested topmost first.
type Manager struct {
	Windows []*Window
	dragged *Window
}

func (m *Manager) Add(w *Window) *Window {
	m.Windows = append(m.Windows, w)
	return w
}

// Update handles dragging and close boxes. Returns true if the mouse press
// was consumed by a window.
func (m *Manager) Update(screenW, screenH int) bool {
	mx, my := ebiten.CursorPosition()
	pressed := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)

	if !pressed {
		m.dragged = nil
	}
	if m.dragged != nil {
		w := m.dragged
		w.SX = clamp(mx-w.moveOffX, 1, screenW-w.SW-1)
		w.SY = clamp(my-w.moveOffY, 1, screenH-w.SH-1)
		return true
	}
	if !pressed {
		return false
	}
	for i := len(m.Windows) - 1; i >= 0; i-- {
		w := m.Windows[i]
		if !w.Visible || !w.contains(mx, my) {
			continue
		}
		if w.overClose(mx, my) && w.OnClose != nil {
			w.OnClose()
			return true
		}
		// Drag only from the title bar so content areas keep their clicks.
		if my-w.SY <= titleHeight {
			w.moving = true
			w.moveOffX, w.moveOffY = mx-w.SX, my-w.SY
			m.dragged = w
		}
		return true
	}
	return false
}

func (m *Manager) Draw(dst *ebiten.Image) {
	frame := color.RGBA{100, 100, 100, 255}
	back := color.RGBA{0, 0, 0, 192}
	mx, my := ebiten.CursorPosition()

	for _, w := range m.Windows {
		if !w.Visible {
			continue
		}
		w.X = w.SX + padding
		w.Y = w.SY + padding + titleHeight
		w.W = w.SW - padding*2
		w.H = w.SH - padding*2 - titleHeight

		x, y := float32(w.SX), float32(w.SY)
		sw, sh := float32(w.SW), float32(w.SH)
		vector.DrawFilledRect(dst, x, y, sw, sh, back, false)
		vector.StrokeRect(dst, x, y, sw, sh, 1, frame, false)
		vector.StrokeLine(dst, x, y+titleHeight, x+sw, y+titleHeight, 1, frame, false)

		if w.DrawFn != nil {
			w.DrawFn(w, dst)
		}

		DrawText(dst, w.Title, float64(w.SX)+4, float64(w.SY)+1, 0.75)

		closeAlpha := uint8(128)
		if w.overClose(mx, my) {
			closeAlpha = 255
		}
		vector.DrawFilledRect(dst, x+sw-closeSize-2, y+2, closeSize, closeSize,
			color.RGBA{100, 100, 100, closeAlpha}, false)
	}
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
