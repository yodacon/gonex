// Package ai flies the NPCs. It began as a port of konex's two behaviors —
// ai_Rabbies charged the nearest enemy, ai_Seige closed to standoff and
// braked — and those two are now the endpoints of one tuning: a pilot whose
// Standoff is short charges, a pilot whose Standoff is long sieges, and the
// jitter in between is where the rest of the squadron lives.
//
// What the doctrines add on top is a reason to break off. A konex NPC fought
// until something killed it. These have somewhere to be.
package ai

// The konex constants, now the centres the per-pilot jitter is drawn around.
const (
	fireRange  = 1024.0
	closeRange = 512.0
)
