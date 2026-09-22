package ai_test

import (
	"fmt"
	"testing"

	"yodacon.org/gonex/internal/ai"
	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/ship"
	"yodacon.org/gonex/internal/world"
)

// How does a frame scale with how much is in the sky? The suspicion is that
// the broadphase is O(n^2): every missile, item and wreck asks ForEachNear
// (a linear scan) and every AI ship asks ClosestEnemy (another one), so the
// per-frame cost is entities x entities rather than entities.
func benchWorld(n int) *world.World {
	catalog, err := ship.LoadCatalog()
	if err != nil {
		panic(err)
	}
	w := world.New(catalog, 1)
	w.MapW, w.MapH = 20000, 20000
	for i := 0; i < n; i++ {
		s := w.NewShip(1+i%12, world.Team(1+i%3), fmt.Sprintf("s%d", i), world.KindNPC)
		// Spread them over the map so this is not one dogpile.
		s.P = gmath.V(float64((i*613)%20000), float64((i*971)%20000))
		s.Controller = ai.Parse("escort", w.Rand)
		w.Add(s)
	}
	// A missile per four ships: each one runs its own ForEachNear sweep.
	for i := 0; i < n/4; i++ {
		if sh, ok := w.Entities[i].(*world.Ship); ok {
			w.SpawnMissile(sh)
		}
	}
	return w
}

func benchFrames(b *testing.B, n int) {
	w := benchWorld(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Update(1.0 / 60)
	}
	b.StopTimer()
	// ns per frame, and the budget a frame actually has at 60 Hz is
	// 16,666,667 ns for EVERYTHING including drawing.
	ns := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	b.ReportMetric(ns/1e6, "ms/frame")
	b.ReportMetric(ns/16666667*100, "%of60Hz")
	b.ReportMetric(float64(len(w.Entities)), "entities")
}

func BenchmarkFrame25(b *testing.B)  { benchFrames(b, 25) }
func BenchmarkFrame50(b *testing.B)  { benchFrames(b, 50) }
func BenchmarkFrame100(b *testing.B) { benchFrames(b, 100) }
func BenchmarkFrame200(b *testing.B) { benchFrames(b, 200) }
func BenchmarkFrame400(b *testing.B) { benchFrames(b, 400) }
