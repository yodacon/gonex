package universe

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/galaxy"
)

// The seed sweep.
//
// One seed is an anecdote. The chip famine reports were written off two of
// them and the two disagreed in sign on the headline number, which is the
// point at which a single-seed measurement stops being evidence.
//
// SEEDS=1 flies the full gazetteer on a spread of seeds and reports the
// distribution rather than a number. SEEDLIST=a,b,c overrides the spread;
// SEEDDAYS shortens the run.
func TestSeedSweep(t *testing.T) {
	if os.Getenv("SEEDS") == "" {
		t.Skip("set SEEDS=1 to fly the gazetteer on a spread of seeds")
	}
	days := 730
	fmt.Sscanf(os.Getenv("SEEDDAYS"), "%d", &days)
	seeds := []int64{20260922, 7, 11, 101, 1997, 31337, 424242, 8675309, 90210, 271828}
	if list := os.Getenv("SEEDLIST"); list != "" {
		seeds = nil
		for _, s := range strings.Split(list, ",") {
			if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				seeds = append(seeds, v)
			}
		}
	}

	track := []econ.Material{econ.Chips, econ.Steel, econ.Rations, econ.Copper,
		econ.Ore, econ.Rounds, econ.Pellets, econ.Melt, econ.Hull}
	series := map[econ.Material][]float64{}
	var pops []float64

	t.Logf("%-10s %8s %8s %8s %8s %8s %7s %7s %7s %6s",
		"seed", "chips", "steel", "rations", "copper", "ore", "rounds", "pellets", "melt", "pop M")
	for _, seed := range seeds {
		u := flyGazetteer(t, seed, days)
		if u == nil {
			t.Skip("no gazetteer")
		}
		pd := func(m econ.Material) float64 { return u.Journal.Made[m] / float64(days) }
		var pop int
		for _, id := range u.Order() {
			pop += u.Worlds[id].Pop
		}
		pops = append(pops, float64(pop)/1e6)
		for _, m := range track {
			series[m] = append(series[m], pd(m))
		}
		t.Logf("%-10d %8.1f %8.1f %8.1f %8.1f %8.1f %7.1f %7.1f %7.1f %6.0f",
			seed, pd(econ.Chips), pd(econ.Steel), pd(econ.Rations), pd(econ.Copper),
			pd(econ.Ore), pd(econ.Rounds), pd(econ.Pellets), pd(econ.Melt), float64(pop)/1e6)
		if bad := u.Audit(); len(bad) > 0 {
			t.Errorf("seed %d: MASS DOES NOT BALANCE: %v", seed, bad[0])
		}
		if bad := u.AuditCredits(); bad != nil {
			t.Errorf("seed %d: LEDGER DOES NOT BALANCE: %v", seed, bad)
		}
	}

	t.Logf("--- distribution over %d seeds ---", len(seeds))
	t.Logf("%-10s %9s %9s %9s %9s %7s", "", "mean", "median", "min", "max", "cv")
	for _, m := range track {
		mean, med, lo, hi, cv := stats(series[m])
		t.Logf("%-10s %9.1f %9.1f %9.1f %9.1f %6.0f%%", m, mean, med, lo, hi, 100*cv)
	}
	mean, med, lo, hi, cv := stats(pops)
	t.Logf("%-10s %9.0f %9.0f %9.0f %9.0f %6.0f%%", "pop M", mean, med, lo, hi, 100*cv)
}

// stats returns mean, median, min, max and the coefficient of variation —
// the spread as a share of the mean, which is the number that says whether a
// single-seed measurement meant anything.
func stats(v []float64) (mean, med, lo, hi, cv float64) {
	if len(v) == 0 {
		return
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	lo, hi = s[0], s[len(s)-1]
	med = s[len(s)/2]
	for _, x := range v {
		mean += x
	}
	mean /= float64(len(v))
	var sd float64
	for _, x := range v {
		sd += (x - mean) * (x - mean)
	}
	sd = math.Sqrt(sd / float64(len(v)))
	if mean != 0 {
		cv = sd / mean
	}
	return
}

// flyGazetteer builds and runs the real map for one seed. It is the body
// every rig in this package repeats, factored out once.
func flyGazetteer(t testing.TB, seed int64, days int) *Universe {
	t.Helper()
	ports := gazetteerPorts(t)
	if os.Getenv("FLAT") == "" {
		ports = Triad(ports)
	}
	u := New(seed, ports, 16)
	g, err := galaxy.Load()
	if err != nil {
		return nil
	}
	u.ChartLanes(func(from, to int) int {
		a, b := g.Stellars[HostOf(from)], g.Stellars[HostOf(to)]
		if a == nil || b == nil {
			return -1
		}
		r := g.Route(a.System, b.System)
		if r == nil {
			return -1
		}
		return len(r) - 1
	})
	for d := 0; d < days; d++ {
		u.Tick()
	}
	return u
}
