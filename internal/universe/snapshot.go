package universe

import (
	"yodacon.org/gonex/internal/econ"
	"yodacon.org/gonex/internal/govt"
	"yodacon.org/gonex/internal/traffic"
)

// Snapshot is the universe's state, for the save file.
//
// A universe is a function of its seed until somebody acts on it: a Works
// bought, a lane chartered, a world taken, a wreck left adrift. Those are
// exactly the things a player would be angry to lose on reload, so they are
// what a snapshot carries. Industry is NOT carried — it is re-stood from the
// crust and the Works level on restore, deterministically, which keeps the
// save small and the plants out of JSON.
type Snapshot struct {
	Seed      int64
	Day       int
	Worlds    []WorldState
	Hulls     []HullState
	Debris    []traffic.Debris
	Relations []RelationState
	Charters  [][2]int
	Exchequer [4]int
	Priority  [4]int
	Policies  [4]Policy
	Tune      Tuning
	Counters  Counters
}

// WorldState is one world's mutable state.
type WorldState struct {
	Stellar   int
	Govt      govt.Color
	Pop       int
	Credits   int
	Reserve   econ.Stock
	Warehouse econ.Stock
	Built     [BuildingCount]int
	Endowed   [BuildingCount]int
	Tariff    float64
	Seat      Seat
	Orders    []StandingOrder
	Fed       float64
}

// HullState is one census row.
type HullState struct {
	ID      int
	Status  traffic.Status
	Home    int
	From    int
	To      int
	V, S    float64
	Cargo   econ.Stock
	Purse   int
	Mission traffic.Mission
	Bought  int
	Voyages int
	Tons    float64
}

// RelationState is one colour pair's standing.
type RelationState struct {
	A, B govt.Color
	R    Relation
}

// Counters are the journal's running totals.
type Counters struct {
	Delivered, Mined, Made, Burned econ.Stock
	Voyages, LostHulls             int
	Sink                           econ.Stock
}

// Snapshot captures the universe.
func (u *Universe) Snapshot() *Snapshot {
	s := &Snapshot{Seed: u.Seed, Day: u.Day, Exchequer: u.Exchequer, Priority: u.Priority,
		Policies: u.Policies, Tune: u.Tune}
	for _, id := range u.order {
		w := u.Worlds[id]
		s.Worlds = append(s.Worlds, WorldState{
			Stellar: w.Stellar, Govt: w.Govt, Pop: w.Pop, Credits: w.Credits,
			Reserve: w.Reserve, Warehouse: w.Warehouse, Built: w.Built, Endowed: w.Endowed,
			Tariff: w.Tariff, Seat: w.Seat, Orders: append([]StandingOrder(nil), w.Orders...),
			Fed: w.fed,
		})
	}
	for _, h := range u.Fleet.Hulls {
		s.Hulls = append(s.Hulls, HullState{
			ID: h.ID, Status: h.Status, Home: h.Home, From: h.From, To: h.To,
			V: h.V, S: h.S, Cargo: h.Cargo, Purse: h.Purse, Mission: h.Mission,
			Bought: h.Bought, Voyages: h.Voyages, Tons: h.Tons,
		})
	}
	for _, d := range u.Fleet.Debris {
		s.Debris = append(s.Debris, *d)
	}
	for k, r := range u.Relations {
		s.Relations = append(s.Relations, RelationState{A: k[0], B: k[1], R: r})
	}
	for k := range u.Charters {
		s.Charters = append(s.Charters, k)
	}
	j := u.Journal
	s.Counters = Counters{Delivered: j.Delivered, Mined: j.Mined, Made: j.Made, Burned: j.Burned,
		Voyages: j.Voyages, LostHulls: j.LostHulls, Sink: u.Sink}
	return s
}

// Restore lays a snapshot over a freshly seeded universe. The universe must
// have been built from the same ports and seed — the snapshot carries only
// what the seed does not. Books and ledger are reopened on the result,
// because a restored state is the starting state by definition.
func (u *Universe) Restore(s *Snapshot) {
	if s == nil {
		return
	}
	u.Day = s.Day
	u.Exchequer = s.Exchequer
	u.Priority = s.Priority
	u.Policies = s.Policies
	if s.Tune.StartPurse > 0 {
		u.Tune = s.Tune
	}
	for _, ws := range s.Worlds {
		w := u.Worlds[ws.Stellar]
		if w == nil {
			continue
		}
		w.Govt, w.Pop, w.Credits = ws.Govt, ws.Pop, ws.Credits
		w.Reserve, w.Warehouse = ws.Reserve, ws.Warehouse
		w.Built, w.Endowed, w.Tariff, w.Seat = ws.Built, ws.Endowed, ws.Tariff, ws.Seat
		w.Orders = append([]StandingOrder(nil), ws.Orders...)
		w.fed = ws.Fed
		w.standUpIndustry()
		w.Reprice()
	}
	byID := map[int]*traffic.Hull{}
	for _, h := range u.Fleet.Hulls {
		byID[h.ID] = h
	}
	for _, hs := range s.Hulls {
		h := byID[hs.ID]
		if h == nil {
			continue
		}
		h.Status, h.Home, h.From, h.To = hs.Status, hs.Home, hs.From, hs.To
		h.V, h.S, h.Cargo, h.Purse, h.Mission = hs.V, hs.S, hs.Cargo, hs.Purse, hs.Mission
		h.Bought, h.Voyages, h.Tons = hs.Bought, hs.Voyages, hs.Tons
		if h.Status == traffic.Resident {
			h.Status = traffic.Idle // nobody is drawing the old sector any more
		}
		h.Mass = h.Wet()
	}
	u.Fleet.Debris = nil
	for i := range s.Debris {
		d := s.Debris[i]
		u.Fleet.Debris = append(u.Fleet.Debris, &d)
	}
	u.Relations = map[[2]govt.Color]Relation{}
	for _, r := range s.Relations {
		u.Relations[colourPair(r.A, r.B)] = r.R
	}
	u.Charters = map[[2]int]bool{}
	for _, k := range s.Charters {
		u.Charters[k] = true
	}
	j := u.Journal
	j.Delivered, j.Mined, j.Made, j.Burned = s.Counters.Delivered, s.Counters.Mined, s.Counters.Made, s.Counters.Burned
	j.Voyages, j.LostHulls = s.Counters.Voyages, s.Counters.LostHulls
	u.Sink = s.Counters.Sink
	u.ReopenBooks()
	u.ReopenLedger()
}
