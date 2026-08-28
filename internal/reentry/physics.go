package reentry

import "math"

// Physical constants and model correlations, straight from the envelope
// model. None of this is validated design data; all of it is a game.
const (
	g0     = 9.80665
	earthR = 6371000.0
	earthMu = 3.986004418e14

	kBoltz  = 1.380649e-23
	keV     = 8.617333262e-5 // Boltzmann in eV/K
	qe      = 1.602176634e-19
	me      = 9.1093837015e-31
	mu0     = 4 * math.Pi * 1e-7

	kSuttonGraves = 1.7415e-4
	sahaPre       = 2.4146e21
	qEN           = 1.0e-19 // electron-neutral cross-section, m2
	lnLambda      = 10.0
	mAir0         = 4.81e-26
	rhoRatio      = 12.0 // real-gas normal-shock density ratio
	sigAvgFac     = 0.40
	kLeak         = 0.50
	knContinuum   = 0.02
	lIon0         = 1e-3
	rhoIon        = 1e-2

	mLiMolar  = 0.006941
	mAirMolar = 0.029
	chiLi     = 5.392
	chiNO     = 9.264
	chiON     = 13.6
	fNO       = 0.02

	cd0 = 1.35
)

// Tauber–Sutton radiative velocity function, log-interpolated.
var fvV = []float64{9, 9.25, 9.5, 9.75, 10, 10.25, 10.5, 10.75, 11, 11.5, 12,
	12.5, 13, 13.5, 14, 14.5, 15, 15.5, 16}
var fvF = []float64{1.5, 4.3, 9.7, 19.5, 35, 55, 81, 115, 151, 238, 359, 495,
	660, 850, 1065, 1313, 1550, 1780, 2040}

func fV(vk float64) float64 {
	if vk < 9 {
		return 1.5 * math.Pow(vk/9, 34.5)
	}
	if vk >= 16 {
		return 2040 * math.Pow(vk/16, 3)
	}
	i := 0
	for i < len(fvV)-2 && fvV[i+1] < vk {
		i++
	}
	f := (vk - fvV[i]) / (fvV[i+1] - fvV[i])
	return math.Exp(math.Log(fvF[i])*(1-f) + math.Log(fvF[i+1])*f)
}

func qRadiative(rho, v, rn float64) float64 {
	r := math.Max(rho, 1e-13)
	a := 1.072e6 * math.Pow(v, -1.88) * math.Pow(r, -0.325)
	a = math.Min(math.Max(a, 0.3), 1.6)
	qc := 4.736e4 * math.Pow(rn, a) * math.Pow(r, 1.22) * fV(v/1000) // W/cm2
	return qc * 1e4                                                  // W/m2
}

// saha solves the multi-species charge balance (NO, N/O, seeded Li) by
// fixed-point iteration and returns the electron density.
func saha(T, nAir, nLi float64) float64 {
	kT := keV * T
	if kT <= 0 {
		return 0
	}
	pre := sahaPre * math.Pow(T, 1.5)
	sNO := pre * math.Exp(-chiNO/kT)
	sON := pre * math.Exp(-chiON/kT)
	sLi := pre * math.Exp(-chiLi/kT)
	nNO, nON := fNO*nAir, (1-fNO)*nAir
	ne := 1e8
	for i := 0; i < 26; i++ {
		f := nNO*sNO/(sNO+ne) + nON*sON/(sON+ne) + nLi*sLi/(sLi+ne)
		nn := math.Sqrt(math.Max(ne, 1e-6) * math.Max(f, 1e-6))
		if math.Abs(nn-ne) < 1e-4*ne {
			return nn
		}
		ne = nn
	}
	return ne
}

// Point is the full flow/plasma/shield state at one trajectory point — the
// numbers behind every gauge in the cluster.
type Point struct {
	Rho, Mach, Kn, QDyn float64
	QBare, QShielded    float64 // stagnation heat flux, W/m2
	WallTemp            float64 // radiation-equilibrium, K
	T2, Ne, Xe          float64 // shock-layer temperature and ionisation
	FLi                 float64 // lithium mole fraction
	Fpe                 float64 // plasma frequency, Hz (the mirror cutoff)
	InteractionQ        float64 // sigma B^2 R / (rho V) — steering authority
	Gate                float64 // 0..1 usable coupling
	Standoff            float64 // R_eff / R_n — how far the pillow stands off
	DragFactor          float64
	PowerDraw           float64 // W the shield asks of the ship
	GLoad               float64 // filled by the integrator
}

// stateAt evaluates the shield model. b is the coil field at the nose (T),
// feed the lithium rate (kg/s).
func stateAt(h, v float64, veh Vehicle, prof Profile, b, feed float64) Point {
	rho, tInf := Atm(h, prof.AtmosScale)
	var p Point
	p.Rho = rho
	a := math.Sqrt(1.4 * 287.05 * tInf)
	p.Mach = v / a
	lam := 8.11e-8 / rho
	p.Kn = lam / (2 * veh.NoseRadius)
	p.QDyn = 0.5 * rho * v * v

	// heating, bare
	qc := kSuttonGraves * math.Sqrt(rho/veh.NoseRadius) * v * v * v
	p.QBare = qc + qRadiative(rho, v, veh.NoseRadius)

	// shock layer
	h0 := 0.5 * v * v
	p.T2 = math.Min(1300*math.Sqrt(h0/1e6), 14000)
	rho2 := rhoRatio * rho
	fdis := 1 / (1 + math.Exp(-(p.T2-4500)/1200))
	mbar := mAir0 / (1 + fdis)
	n2 := rho2 / mbar

	// lithium seeding
	ac := math.Pi * veh.NoseRadius * veh.NoseRadius
	mdotAir := rho * v * ac
	molAir, molLi := mdotAir/mAirMolar, feed/mLiMolar
	fLi := 0.0
	if molAir+molLi > 0 {
		fLi = molLi / (molAir + molLi)
	}
	p.FLi = fLi
	fLi = math.Min(fLi, 0.5)
	nLi, nAir := fLi*n2, (1-fLi)*n2

	// frozen-flow de-rating
	dlt0 := 0.15 * veh.NoseRadius
	lIon := lIon0 * math.Pow(rhoIon/math.Max(rho2, 1e-14), 1.5)
	da := dlt0 / lIon
	fneq := da / (1 + da)

	ne := saha(p.T2, nAir, nLi) * fneq
	p.Ne = ne
	p.Xe = ne / n2
	nn := math.Max(n2-ne, 1)

	// transport
	vth := math.Sqrt(8 * kBoltz * p.T2 / (math.Pi * me))
	teV := keV * p.T2
	nuEN := nn * qEN * vth
	nuEI := 2.91e-12 * ne * lnLambda * math.Pow(math.Max(teV, 1e-3), -1.5)
	nuC := nuEN + nuEI
	sig := ne * qe * qe / (me * math.Max(nuC, 1))
	sigE := sigAvgFac * sig
	p.Fpe = 8.98 * math.Sqrt(math.Max(ne, 0))

	// MHD interaction
	phi := 1 / (1 + math.Pow(p.Kn/knContinuum, 2))
	sigM := sigE * phi
	p.InteractionQ = sigM * b * b * veh.NoseRadius / math.Max(rho*v, 1e-12)
	p.Gate = phi * p.InteractionQ / (1 + p.InteractionQ)

	// magnetopause standoff and its consequences
	pmag := b * b / (2 * mu0)
	pflow := rho * v * v
	rmpR := 1.0
	if b > 0 {
		rmpR = math.Max(math.Pow(pmag/math.Max(pflow, 1e-12), 1.0/6.0), 1)
	}
	reff := veh.NoseRadius * (1 + (rmpR-1)*p.Gate)
	p.Standoff = reff / veh.NoseRadius
	p.QShielded = p.QBare * math.Sqrt(veh.NoseRadius/reff)
	p.WallTemp = math.Pow(p.QShielded/(0.85*5.670374419e-8), 0.25)

	aBody := math.Pi * veh.Diameter * veh.Diameter / 4
	aMag := math.Pi * reff * reff
	p.DragFactor = 1 + kLeak*math.Max(0, aMag/aBody-1)

	// power ledger: phased array + cryo + seed + housekeeping
	fluxEnth := 0.5 * rho * v * v * v * aMag
	pArr := 0.005 * fluxEnth / 0.35
	pCryo := 0.0
	if b > 0 {
		aDew := 4 * math.Pi * veh.NoseRadius * veh.NoseRadius
		pCryo = 3e-4 * 5.670374419e-8 * math.Pow(p.WallTemp, 4) * aDew * 50
	}
	p.PowerDraw = pArr + pCryo + feed*2.5e6 + 3.5e4
	return p
}
