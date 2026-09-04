package econ

import "fmt"

// The credit ledger: the second conservation law.
//
// Credits were the one quantity in the game that could be minted and burned
// freely — a courier's margin was logged and discarded on arrival, a landing
// bonus appeared from nowhere, and everything the player bought at a counter
// went to no treasury. That is the accounting OpenFront runs on purpose
// (gold is created at both ends of every voyage) and it is the wrong
// accounting for a courier simulator whose promise is that a cargo is
// somebody's. So credits are conserved exactly as tons are: every purse in
// the universe is registered, the sum is opened at genesis, and Audit asks
// whether it is all still here.
//
// The consequence worth wanting: the money supply is fixed, so the only
// variable is VELOCITY, and velocity is set by holds in flight. Money walks
// outward along the lanes exactly as fast as the couriers do.

// Pay moves credits from one purse to another and returns how many actually
// moved. It is the ONLY sanctioned way to move money, and like Transfer it
// cannot create any: it takes what the payer has, and adds exactly that.
// A negative amount is a caller bug and moves nothing.
func Pay(from, to *int, n int) int {
	if n <= 0 || from == nil || to == nil {
		return 0
	}
	if *from < n {
		n = *from
	}
	if n <= 0 {
		return 0
	}
	*from -= n
	*to += n
	return n
}

// Ledger is the universe's credit balance.
type Ledger struct {
	// Genesis is the money supply the universe was opened with. Nothing
	// ever changes it.
	Genesis int
}

// NewLedger opens the ledger on a universe holding `genesis` credits.
func NewLedger(genesis int) *Ledger { return &Ledger{Genesis: genesis} }

// Imbalance is a ledger that does not add up.
type Imbalance struct {
	Genesis, Found int
}

func (i Imbalance) Delta() int { return i.Found - i.Genesis }

func (i Imbalance) String() string {
	verb := "minted"
	d := i.Delta()
	if d < 0 {
		verb, d = "burned", -d
	}
	return fmt.Sprintf("CREDITS: %d cr %s (genesis %d, found %d)", d, verb, i.Genesis, i.Found)
}

// Audit sums every purse and compares with genesis. Pass EVERY purse —
// treasuries, exchequers, pilots, the player. A purse left out reads as
// burned money, which is the correct failure: forgetting to count a purse IS
// losing track of what is in it.
func (l *Ledger) Audit(purses ...int) *Imbalance {
	found := 0
	for _, p := range purses {
		found += p
	}
	if found == l.Genesis {
		return nil
	}
	return &Imbalance{Genesis: l.Genesis, Found: found}
}

// Balanced is Audit as a yes/no.
func (l *Ledger) Balanced(purses ...int) bool { return l.Audit(purses...) == nil }
