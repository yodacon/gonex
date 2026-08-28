// Package console is the drop-down developer console and the on-screen
// notification feed, ported from konex's console module. Rendering and key
// handling live in internal/ui; this package keeps the state and the command
// registry so game code can register commands without touching Ebitengine.
package console

import (
	"fmt"
	"strings"
)

const (
	MaxLines     = 255
	DisplayLines = 16
	NotifyTTL    = 5.0
	notifySize   = 16
)

type State int

const (
	Hidden State = iota
	Animating
	Shown
)

type notifyLine struct {
	Text string
	TTL  float64
}

type Handler func(c *Console, args string)

type Console struct {
	State  State
	Bottom float64 // current drop-down height in pixels
	speed  float64

	lines    []string // newest first
	Position int      // scrollback offset
	Cmd      string
	lastCmd  string

	notify [notifySize]notifyLine

	commands map[string]Handler
	fallback Handler
}

func New() *Console {
	return &Console{commands: map[string]Handler{}}
}

// Register binds a command name (and aliases) to a handler.
func (c *Console) Register(h Handler, names ...string) {
	for _, n := range names {
		c.commands[n] = h
	}
}

func (c *Console) Printf(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	fmt.Println(text)
	c.lines = append([]string{text}, c.lines...)
	if len(c.lines) > MaxLines {
		c.lines = c.lines[:MaxLines]
	}
	if c.Position > 0 && c.Position < MaxLines-DisplayLines {
		c.Position++
	}
}

// Notifyf prints to the log and shows a fading on-screen line.
func (c *Console) Notifyf(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	c.Printf("%s", text)
	copy(c.notify[:], c.notify[1:])
	c.notify[notifySize-1] = notifyLine{Text: text, TTL: NotifyTTL}
}

// Lines returns the visible slice of scrollback, newest first.
func (c *Console) Lines() []string {
	if c.Position >= len(c.lines) {
		return nil
	}
	end := c.Position + DisplayLines
	if end > len(c.lines) {
		end = len(c.lines)
	}
	return c.lines[c.Position:end]
}

// ActiveNotifications decays and returns the live notification texts.
func (c *Console) ActiveNotifications(dt float64) []string {
	var out []string
	for i := range c.notify {
		if c.notify[i].TTL > 0 {
			out = append(out, c.notify[i].Text)
			c.notify[i].TTL -= dt
		}
	}
	return out
}

func (c *Console) Toggle() {
	const speed = 500.0
	switch c.State {
	case Hidden, Animating:
		c.State, c.speed = Animating, speed
	case Shown:
		c.State, c.speed = Animating, -speed
	}
}

// Animate advances the drop-down toward open (target height) or closed.
func (c *Console) Animate(dt, target float64) {
	if c.State != Animating {
		if c.State == Shown {
			c.Bottom = target
		}
		return
	}
	c.Bottom += c.speed * dt
	if c.speed > 0 && c.Bottom >= target {
		c.Bottom, c.State = target, Shown
	}
	if c.speed < 0 && c.Bottom <= 0 {
		c.Bottom, c.State = 0, Hidden
	}
}

func (c *Console) ScrollBack() {
	if c.Position < len(c.lines)-DisplayLines {
		c.Position++
	}
}

func (c *Console) ScrollForward() {
	if c.Position > 0 {
		c.Position--
	}
}

func (c *Console) RecallLast() { c.Cmd = c.lastCmd }

func (c *Console) Backspace() {
	if len(c.Cmd) > 0 {
		c.Cmd = c.Cmd[:len(c.Cmd)-1]
	}
}

func (c *Console) Append(chars string) { c.Cmd += chars }

// Execute runs the pending command line.
func (c *Console) Execute() {
	line := strings.TrimSpace(c.Cmd)
	c.lastCmd, c.Cmd = c.Cmd, ""
	if line == "" {
		return
	}
	name, args, _ := strings.Cut(line, " ")
	if h, ok := c.commands[name]; ok {
		h(c, strings.TrimSpace(args))
		return
	}
	c.Printf("Unknown Command (%s)", line)
}
