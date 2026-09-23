package universe

import "testing"

// The economy runs on the main thread. stepUniverse catches the simulation
// up to the voyage's day, and a landing burns days — so however long one
// Tick takes, a player who has been away for a while pays for all of them
// inside a single frame.
func benchTick(b *testing.B, ports []Port) {
	u := New(20260922, ports, 16)
	for d := 0; d < 30; d++ {
		u.Tick() // warm: routes only exist once there is stock to move
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.Tick()
	}
	b.StopTimer()
	ms := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / 1e6
	b.ReportMetric(ms, "ms/day")
	b.ReportMetric(ms*400, "ms/400d-catchup")
	b.ReportMetric(float64(len(u.Order())), "worlds")
}

func BenchmarkTick11Worlds(b *testing.B) { benchTick(b, testPorts()) }
// The map the game actually seeds. FindRoutes is O(worlds^2 x materials)
// per colour per day, so this is the number that decides whether passing a
// season is instant or a visible freeze.
func BenchmarkTickGazetteer(b *testing.B) { benchTick(b, Triad(gazetteerPorts(b))) }
