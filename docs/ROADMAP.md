# The Road to Feature-Complete Flight

*Written against v0.1.0-rc2. The landing sequence is done and pretty; this
is everything around it. The theme of the whole document is UNIFICATION:
gonex is still two stitched games — konex's 1997 deathmatch and the
reentry-trader — and the seams show exactly where a feature stops being
finished.*

The one-sentence diagnosis: **the corridor is one game and the vacuum is
another.** Below the Kármán line every number is connected — damage costs
credits, weight costs handling, conduct earns a grade. Above it, the
world still keeps deathmatch score. Feature-complete means the vacuum
joins the ledger.

---

## 1. The Player — one pilot, one ledger

Today there are two player models that never touch: `Voyage`
(credits, fuel, cargo, damage, missions — `app/voyage.go`) and the world
`Ship` (Health/Money/Frags/Deaths — `world/ship.go:29`), saved
separately. A firefight in flight costs nothing the campaign can see;
`Ship.die` respawns you with full health and `modeDead` is only
reachable from entry.

- [ ] **Identity merge.** `Voyage` becomes the single source of truth;
      the world Ship reads hull from `100−Dmg.Hull` and writes hits back
      into `Dmg` (scaled — a laser is not reentry plasma). Delete
      Money/Frags/Deaths from the flight save or demote them to an
      arcade scoreboard easter egg.
- [ ] **Death in the vacuum.** Hull to zero in flight → `modeDead` with
      the same gravity as breakup in the plasma: the ded screen, the
      berth-save resume, an insurance/escape-pod economics decision.
- [ ] **The legal record.** The mission data already carries
      `AvailRecord` per govt and the EVO model defines per-govt ledgers;
      persist `Record map[govt]int` on the Voyage, move it on mission
      completion (`CompGovt`/`CompReward` are already parsed), and gate
      bar offers on it. This single field makes governments *matter*.
- [ ] **Combat rating.** The EVO `AvailRating` counterpart: a kill/assist
      tally that gates the roughest postings. Depends on §5 (someone to
      fight).

## 2. The Ship — outfits beyond the power grid

The outfit catalog is five power items (`power.go:134`); the weapon is
one straight missile on Space with no turrets, no tracking, no ammo. The
Yodacon's lore is "mounted rotating turrets on every arc" — the master
ship should feel like it.

- [ ] **Outfit families**, all flowing into the same two sinks —
      `entryVehicleFor` (mass, tanks, TPS, coil) and the flight model:
      cargo pods (raise `cargoCap`, raise entry mass), lithium/RCS tank
      stretches, TPS plating (heavier, higher flux limit), coil upgrades
      (standoff reach), engine tuning (arcade accel/turn). Every tonne
      still charged by the atmosphere on the way down — the LOAD dial
      already reads it.
- [ ] **Turrets.** A rotating-arc weapon that leads the target (the lead
      pip in §7 is the UI half), fed by the capacitor through the
      existing `FireGate`. Turret count per ship from the catalog.
- [ ] **Escorts as wingmen.** Hired escorts already spawn on takeoff
      with `rabies` AI on the player's team; make them persist through
      jumps, take orders (form up / engage / flee via one key), and
      appear in the target window as friendlies.

## 3. The Interfaces — one design system

The flight HUD window is still the 1997 scoreboard: Red/Green/Blue team
scores, Frags, Deaths (`windows.go:118`). Credits, fuel, cargo, the day,
the active mission — the things a courier actually is — appear nowhere
in flight except a console command.

- [ ] **The courier HUD.** Replace the scoreboard: credits · fuel/max ·
      cargo t/cap · day · hull% · active mission line ("Parcels →
      Exeon, 3 days") · speed/heading. Same phosphor-on-void tokens as
      the site (Phase 6) and the entry HUD.
- [ ] **One palette package.** `internal/ui/theme.go` holding the Phase
      6 tokens (void/crt/phosphor/chrome/accent) plus the IFF colors
      (§7); entrymode's `colEM/colHeat/...` and `hudGreen` move there so
      the corridor and the vacuum stop disagreeing about green.
- [ ] **Promote the entry components.** `dial()` and `hbar()` from
      entrymode into `internal/ui` and reuse them in the engineering
      panel and dock screens — one gauge language everywhere.
- [ ] **One help surface.** A keys/HELP window that lists the real input
      map (it currently lives in six draw functions and the pilot guide).

## 4. The Minimap — from radar echo to instrument

Mini and full map are pixel-identical renders at two sizes
(`render.go:198`): gray planet dots, white player, team-colored ships.

- [ ] **Layered content:** dockable stellars as ringed dots (scenery
      planets stay plain), the warp beacon as the same pulsing diamond
      it is in-world, a player heading wedge, IFF-colored ship dots
      (§7), the current nav target haloed.
- [ ] **The full map becomes the SYSTEM map:** stellar names drawn,
      docking-range rings, mission-destination highlight, and a
      viewport rectangle showing where the minimap sits — actual zoom,
      not a bigger copy.

## 5. The World — someone out there (prereq for "hostile")

Nothing is ever spawned from the galaxy: the same 36 deathmatch Jimmys
ride through every system (`enterSystem` culls only planets), govt
reaches flight as one console line, and `mission.Def.ShipCount/ShipDude/
ShipGoal` are parsed and never used.

- [ ] **The traffic spawner.** On `enterSystem`, cull NPC ships and roll
      the system's population from its govt: liveried freighters on the
      lanes, govt patrols, and — in the lawless corners — the raiders
      the courier universe defines itself against. Dude tables from the
      1997 data name the ship mixes.
- [ ] **Wire the mission ships.** `ShipCount`+`ShipDude`+`ShipGoal`
      spawn real escorts/ambushes/observables in the destination system
      instead of auto-resolving on arrival; `ShipBehav` maps onto the
      two existing AI brains plus a `flee`.
- [ ] **Disposition, not teams.** Replace team-only hostility
      (`ClosestEnemy`, `world.go:128`) with a govt-relations check
      (gazetteer govts + player record): hostile / wary / neutral /
      friendly. A clean record is a shield; a smuggler's record is a
      target painted on the hull.

## 6. Landing Navigation — pointing at the ground

The only in-world nav aid is the warp beacon. Planets are unlabeled
sprites; finding the mission stellar means flying blind until the 256-
unit hail range trips.

- [ ] **Nav targets.** `N` cycles navigable objects (dockable stellars,
      the warp beacon); the selection gets a cyan NAV diamond in-world,
      a name+distance label, and an edge arrow when off-screen — the
      same grammar as the entry HUD's PAD marker, deliberately.
- [ ] **The approach ring.** Draw the docking range around the selected
      stellar; the handshake countdown ("CLEARED — L commits, 22 s")
      renders on the bracket, not only in the mode banner.
- [ ] **Approach assist.** A held key that arcs the ship toward the nav
      target and bleeds speed into the hail window — the vacuum's
      autoland, using the same "computer takes the stick" language as
      the corridor's reflex.
- [ ] **Auto-select on mission.** Accepting a delivery sets the nav
      target chain: destination system on the galaxy map, destination
      stellar on arrival.

## 7. Navigation Brackets & IFF — the color of intent

Target brackets are four white corners on whatever `ClosestEnemy`
returned; the target window prints "Team: Red" (`render.go:171`,
`windows.go:133`).

- [ ] **One IFF color language, used by every surface** — world
      brackets, minimap dots, target-window header, galaxy-map rings:
      **red** hostile (would fire), **amber** wary (scans you, legal
      record risk), **gray** neutral traffic, **green** friendly/escort,
      **cyan** nav target, **orange, pulsing** mission objective. The
      corridor already taught red=danger and green=guidance; the vacuum
      inherits the same grammar.
- [ ] **Bracket anatomy:** corner style by disposition (double-thick
      pulsing corners for hostile lock), name + govt + distance +
      closure under the bracket, a lead pip when weapons are hot.
- [ ] **Target cycling:** `T` next ship, `R` nearest hostile, shift-
      click keeps working; planets excluded (they belong to `N`, §6).
- [ ] **Scan events.** A wary govt ship closing to scan range rolls the
      EVO smuggle check against the manifest — the mission data's
      `ScanGovt`/`FailIfScanned` fields are already in the defs, unused.

## 8. The Jump Map — the galaxy as an instrument

The galaxy chart already routes by click and shows the plotted line;
it colors dots by data-source (base/ConEx), which is provenance, not
politics.

- [ ] **Govt coloring** with the IFF palette against the player's
      record — the map answers "where am I welcome" at a glance.
- [ ] **Fuel-range shading:** systems reachable on current fuel bright,
      one-tank-away dim, beyond dark; the 100/jump economy made visible.
- [ ] **One chart component:** merge `drawGalaxyMap` and the docked
      `drawMissionChart` (route hops, ETA, fuel) into a single view used
      in both places, keyboard-navigable.
- [ ] **Known space.** Systems unvisited render as charted-but-dim;
      landing somewhere lights it. Cheap flag on the Voyage, big
      exploration feel.

---

## Sequencing

| Milestone | Contents | Size | Unlocks |
| --- | --- | --- | --- |
| **A — One Pilot** | §1 identity merge, courier HUD (§3), nav targets + brackets skeleton (§6, §7 colors) | M | everything below reads one ledger |
| **B — Someone Out There** | §5 traffic + disposition, mission ships, scan events, flight death economics | L | "hostile" means something; combat rating |
| **C — The Instruments** | minimap layers + system map (§4), jump-map govt/fuel/known (§8), approach assist | M | the maps stop being echoes |
| **D — The Yard** | outfit families + turrets + escort orders (§2) | L | the upgrade dream loop (Phase 5 bar feeds it) |

A before B (disposition needs the record), B before combat rating,
C anytime after A, D last — it is the reward loop and wants the economy
whole. Every milestone keeps the corridor gates green and adds its own:
a disposition matrix test, a traffic determinism test (seeded per system
per day), a scan-event test against the 1997 mission defs.
