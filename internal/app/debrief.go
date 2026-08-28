package app

// The landing debrief: once the rollout stops, every gauge comes off and
// the screen becomes a single card — what the flight cost, what it earned,
// what was flown well, and a grade. The corridor teaches during the
// descent; the debrief is where the lesson is signed.

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/internal/reentry"
	"yodacon.org/gonex/internal/ui"
)

type debriefLine struct {
	good bool
	text string
}

// gradeEntry scores a landed flight 0-100 and names it in the company's
// own ledger language.
func gradeEntry(s *reentry.Sim, boomFine int) (score int, grade, verdict string) {
	sc := s.Score()
	v := 100.0
	v -= s.Dmg.Hull * 0.6
	v -= s.Dmg.Computer * 0.2
	v -= s.Dmg.Clamps * 0.2
	v -= minf(sc.CrossKm*2, 15)
	v -= minf(s.OffPipeT*0.3, 20)
	v -= float64(s.Recoveries) * 4
	if boomFine > 0 {
		v -= 8
	}
	if sc.CrossKm < 2 {
		v += 5
	}
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	switch {
	case v >= 90:
		return int(v), "A+", "FIVE BY FIVE — the corridor never noticed you"
	case v >= 80:
		return int(v), "A", "PROVEN COURIER — the underwriters smile"
	case v >= 70:
		return int(v), "B", "CLEAN CORRIDOR — freight none the wiser"
	case v >= 55:
		return int(v), "C", "SCORCHED BUT SIGNED — the manifest clears"
	case v >= 40:
		return int(v), "D", "INSURANCE CASE — premiums will remember this"
	default:
		return int(v), "E", "WRITE-OFF — the pad crew is still counting parts"
	}
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// conduct builds the good-behavior ledger: what this flight did right,
// and what it paid for.
func conduct(s *reentry.Sim, veh reentry.Vehicle, boomFine int) []debriefLine {
	sc := s.Score()
	var out []debriefLine
	add := func(good bool, f string, args ...any) {
		out = append(out, debriefLine{good, fmt.Sprintf(f, args...)})
	}
	if s.OffPipeT < 1 {
		add(true, "flew the pipe the whole way down")
	} else {
		add(false, "%.0f s outside the pipe, hull scrubbing", s.OffPipeT)
	}
	if s.Recoveries == 0 {
		add(true, "never needed the computer's reflex")
	} else {
		add(false, "emergency override took the stick %d time(s)", s.Recoveries)
	}
	if s.MaxG <= s.Veh.GLimit {
		add(true, "peak %.1f g, inside the %.0f g airframe limit", s.MaxG, s.Veh.GLimit)
	} else {
		add(false, "peak %.1f g OVER the %.0f g limit — systems shaken", s.MaxG, s.Veh.GLimit)
	}
	if s.PeakQFrac <= 1 {
		add(true, "heat pulse held to %.0f%% of the shield's limit", s.PeakQFrac*100)
	} else {
		add(false, "flux peaked at %.0f%% of limit — the hull ate the excess", s.PeakQFrac*100)
	}
	if sc.CrossKm < 2 {
		add(true, "on the pad line: %.1f km off, full bonus", sc.CrossKm)
	} else if sc.CrossKm < 10 {
		add(false, "%.1f km off the pad line — half bonus", sc.CrossKm)
	} else {
		add(false, "%.1f km off the pad line — no bonus", sc.CrossKm)
	}
	if s.Li > veh.LiTank*0.25 {
		add(true, "lithium thrift: %.1f kg still in the tank", s.Li)
	} else if s.Li <= 1 {
		add(false, "tank dry — landed on the last pellets")
	}
	if boomFine > 0 {
		add(false, "supersonic over the port: %d cr noise fine", boomFine)
	} else {
		add(true, "subsonic over the town, windows intact")
	}
	if wf := veh.WeightFactor(); wf > 1.01 {
		add(false, "flown at %.0f%% design mass — every correction cost double", wf*100)
	}
	return out
}

// drawDebrief is the runway screen: gauges gone, the world idling behind,
// one card that closes the flight.
func (a *App) drawDebrief(screen *ebiten.Image) {
	e, s := a.entry, a.entry.sim
	sc := s.Score()
	stName := ""
	if st := a.gal.Stellars[e.stellar]; st != nil {
		stName = st.Name
	}
	score, grade, verdict := gradeEntry(s, e.boomFine)
	lines := conduct(s, s.Veh, e.boomFine)

	w, h := 660.0, 224.0+float64(len(lines))*18
	x, y := (ScreenW-w)/2, 120.0
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(w), float32(h),
		color.RGBA{5, 7, 10, 242}, false)
	vector.StrokeRect(screen, float32(x), float32(y), float32(w), float32(h), 1, colChrome, false)

	ui.DrawText(screen, fmt.Sprintf("LANDING DEBRIEF — %s", stName), x+24, y+18, 1)
	vector.StrokeLine(screen, float32(x+24), float32(y+40), float32(x+w-24), float32(y+40),
		1, premul(colRule, 0.9), false)

	// the grade, big, on the right
	gcol := colOI
	if score < 70 {
		gcol = colHeat
	}
	if score < 40 {
		gcol = colBad
	}
	ui.DrawTextScaled(screen, grade, x+w-120, y+56, 4, gcol, 1)
	ui.DrawText(screen, fmt.Sprintf("%d/100", score), x+w-118, y+112, 0.8)

	// the ledger: hull, credits, resources
	ly := y + 56.0
	row := func(f string, args ...any) {
		ui.DrawText(screen, fmt.Sprintf(f, args...), x+24, ly, 0.9)
		ly += 20
	}
	row("hull %.0f%%   computer %.0f%%   clamps %.0f%%",
		100-s.Dmg.Hull, 100-s.Dmg.Computer, 100-s.Dmg.Clamps)
	row("repairs due %d cr   pad bonus %d cr", sc.RepairCost, sc.PadBonus)
	row("lithium %.1f kg   rcs %.0f kg   fuel %d", s.Li, s.RCS, a.voy.Fuel)

	// conduct
	ly += 10
	ui.DrawText(screen, "CONDUCT", x+24, ly, 0.7)
	ly += 20
	for _, ln := range lines {
		mark, mcol := "+", colOI
		if !ln.good {
			mark, mcol = "-", colBad
		}
		ui.DrawTextScaled(screen, mark, x+28, ly, 1, mcol, 1)
		ui.DrawText(screen, ln.text, x+44, ly, 0.85)
		ly += 18
	}

	ly += 12
	ui.DrawText(screen, verdict, x+24, ly, 1)
	ui.DrawText(screen, "press any key to dock", x+24, y+h-24, 0.6)
}
