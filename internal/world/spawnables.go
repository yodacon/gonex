package world

import "yodacon.org/gonex/internal/gmath"

const (
	missileSpeed = 2000.0
	missileTTL   = 2.0
	itemTTL      = 15.0
	explosionTTL = 1.0
)

// --- Missile ---

type Missile struct {
	Body
	Heading float64
	Owner   *Ship
	Damage  int
	TTL     float64
	dead    bool
}

func (w *World) SpawnMissile(owner *Ship) {
	m := &Missile{
		Body: Body{
			P: owner.P,
			V: owner.V.Add(gmath.HeadingVec(owner.Heading).Scale(missileSpeed)),
		},
		Heading: owner.Heading,
		Owner:   owner,
		Damage:  w.Catalog.Get(owner.ShipID).Damage,
		TTL:     missileTTL,
	}
	w.Add(m)
}

func (m *Missile) Alive() bool { return !m.dead }

func (m *Missile) Update(w *World, dt float64) {
	w.ForEachNear(m, func(e Entity) {
		if m.dead {
			return
		}
		s, ok := e.(*Ship)
		if !ok || s == m.Owner || s.Team == m.Owner.Team || s.Docked() {
			return // a ship on the pad is out of the world, not a target
		}
		s.HitByMissile(w, m)
		m.dead = true
	})
	m.TTL -= dt
	if m.TTL < 0 {
		m.dead = true
	}
}

// --- Explosion ---

type Explosion struct {
	Body
	Size float64 // matches the dying ship's sprite size
	TTL  float64
}

func (w *World) SpawnExplosionFrom(s *Ship) {
	size := float64(w.Catalog.Get(s.ShipID).Sprites[0].Bounds().Dx())
	w.Add(&Explosion{Body: Body{P: s.P, V: s.V}, Size: size, TTL: explosionTTL})
}

func (e *Explosion) Alive() bool { return e.TTL > 0 }

func (e *Explosion) Update(_ *World, dt float64) { e.TTL -= dt }

// --- Item (health or money drop) ---

type ItemType int

const (
	ItemHealth ItemType = 1
	ItemMoney  ItemType = 2
)

type Item struct {
	Body
	Type   ItemType
	Amount int
	TTL    float64
	dead   bool
}

// MaybeDropItem gives a 1-in-5 chance of a pickup where a ship died.
func (w *World) MaybeDropItem(s *Ship) {
	if w.Rand.Intn(5) != 0 {
		return
	}
	w.Add(&Item{
		Body:   Body{P: s.P, V: s.V},
		Type:   ItemType(1 + w.Rand.Intn(2)),
		Amount: 1 + w.Rand.Intn(25),
		TTL:    itemTTL,
	})
}

func (it *Item) Alive() bool { return !it.dead }

func (it *Item) Update(w *World, dt float64) {
	w.ForEachNear(it, func(e Entity) {
		if it.dead {
			return
		}
		s, ok := e.(*Ship)
		if !ok {
			return
		}
		switch it.Type {
		case ItemHealth:
			if s.Health < maxHealth {
				s.Health++
			}
		case ItemMoney:
			s.Money += it.Amount
		}
		it.dead = true
	})
	it.TTL -= dt
	if it.TTL <= 0 {
		it.dead = true
	}
}

// --- Static scenery ---

// CommodityCount is the width of every cargo manifest in the game. It mirrors
// len(market.Commodities); world cannot import market without dragging the
// trading UI into the simulation, so the width is pinned here and asserted
// against the market board in the app's tests.
const CommodityCount = 6

type Planet struct {
	Body
	SpriteID  int // 1-based index into the planet picture catalog
	StellarID int // gazetteer spöb ID; 0 for scenery with no dock
	Name      string
	Team      Team // who holds it; TeamNone is neutral ground

	// The industrial ledger. Pop comes from the city that grows on the
	// surface, and everything the fleet needs is drawn against it.
	Pop     int
	IP      float64 // industrial points banked
	Credits int
	Stock   []int   // tons per commodity in the warehouse
	Scrap   float64 // tons of salvage in the yard
	Pad     []*Ship // ships turning around right now

	creditAcc float64 // sub-credit revenue carried between ticks
}

func (p *Planet) Alive() bool { return true }

func (p *Planet) Update(_ *World, dt float64) { p.Tick(dt) }

type SpawnPoint struct {
	Body
	Team Team
}

func (sp *SpawnPoint) Alive() bool            { return true }
func (sp *SpawnPoint) Update(*World, float64) {}
