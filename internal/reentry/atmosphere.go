// Package reentry simulates a guided atmospheric entry behind a steerable
// plasma shield: the MHD Reentry Console envelope model (yodacon
// vendor/docs/reentry-console.html) restructured for interactive flight.
// Everything is SI unless a name says otherwise. The package is
// rendering-free and deterministic so the corridor gate can run headless.
package reentry

import "math"

// US Standard Atmosphere 1976, log-interpolated in density.
var atmAlt = []float64{0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65,
	70, 75, 80, 85, 90, 95, 100, 105, 110, 115, 120, 130, 140, 150}

var atmRho = []float64{1.225, 7.364e-1, 4.135e-1, 1.948e-1, 8.891e-2,
	4.008e-2, 1.841e-2, 8.463e-3, 3.996e-3, 1.966e-3, 1.027e-3, 5.681e-4,
	3.097e-4, 1.632e-4, 8.283e-5, 3.992e-5, 1.846e-5, 8.220e-6, 3.416e-6,
	1.393e-6, 5.604e-7, 2.325e-7, 9.708e-8, 4.289e-8, 2.222e-8, 8.152e-9,
	3.831e-9, 2.076e-9}

var atmT = []float64{288.15, 255.7, 223.3, 216.65, 216.65, 221.6, 226.5,
	236.5, 250.4, 264.2, 270.65, 260.8, 247.0, 233.3, 219.6, 208.4, 198.6,
	188.9, 186.87, 188.4, 195.1, 208.8, 240.0, 300.0, 360.0, 469.3, 559.6,
	634.4}

// Atm returns ambient density (kg/m3) and temperature (K) at altitude hm
// meters, scaled by the landing profile's atmosphere factor.
func Atm(hm, scale float64) (rho, T float64) {
	h := hm / 1000
	switch {
	case h <= 0:
		return 1.225 * scale, 288.15
	case h >= 150:
		return 2.076e-9 * math.Exp(-(h-150)/60) * scale, 634.4 + (h-150)*1.5
	}
	i := 0
	for i < len(atmAlt)-2 && atmAlt[i+1] < h {
		i++
	}
	f := (h - atmAlt[i]) / (atmAlt[i+1] - atmAlt[i])
	rho = math.Exp(math.Log(atmRho[i])*(1-f)+math.Log(atmRho[i+1])*f) * scale
	T = atmT[i]*(1-f) + atmT[i+1]*f
	return rho, T
}
