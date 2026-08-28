// Package ai ports konex's NPC behaviors. Each behavior is a
// world.Controller; ships get one at random when a scene creates AI actors.
package ai

import (
	"math/rand"

	"yodacon.org/gonex/internal/world"
)

const (
	fireRange  = 1024.0
	closeRange = 512.0
)

// Rabies charges the nearest enemy at full throttle, firing inside range.
// (The original spelled it ai_Rabbies.)
type Rabies struct{}

func (Rabies) Name() string { return "rabies" }

func (Rabies) Control(s *world.Ship, w *world.World, dt float64) {
	target := w.ClosestEnemy(s)
	s.Target = target
	if target == nil {
		return
	}
	prox := target.Pos().Sub(s.Pos()).Len()
	if prox < fireRange {
		s.Fire(w)
	}
	s.FaceToward(w, target.Pos(), dt)
	if prox > closeRange {
		s.Thrust(w, dt)
	}
}

// Siege closes to standoff range and then brakes, firing from position.
// (The original spelled it ai_Seige.)
type Siege struct{}

func (Siege) Name() string { return "siege" }

func (Siege) Control(s *world.Ship, w *world.World, dt float64) {
	target := w.ClosestEnemy(s)
	s.Target = target
	if target == nil {
		return
	}
	prox := target.Pos().Sub(s.Pos()).Len()
	if prox < fireRange {
		s.Fire(w)
	}
	s.FaceToward(w, target.Pos(), dt)
	if prox > closeRange {
		s.Thrust(w, dt)
	} else {
		s.Slow(dt)
	}
}

// ByName returns a behavior for a saved name, defaulting to Rabies.
func ByName(name string) world.Controller {
	if name == "siege" {
		return Siege{}
	}
	return Rabies{}
}

// Random picks one of the behaviors, like player_CreateAI did.
func Random(r *rand.Rand) world.Controller {
	if r.Intn(2) == 0 {
		return Rabies{}
	}
	return Siege{}
}
