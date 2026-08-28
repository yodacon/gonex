# Gonex backlog

Ideas and deferred work, in rough priority order.

## Resource-fork pipeline (the big idea)

The 1997 ConEx plugin has now been fully cracked open (see
`yodacon/extracted/` and `yodacon/docs/lab-reports/`). Gonex already carries
one recovered ship (`yodacon97`). The follow-on idea: **load EV plugin
resources directly** —

- [ ] Port `rsrc_extract.py` + `pict_decode.py` to Go (`internal/rsrc`,
      `internal/pict`) so Gonex can ingest a raw `.rsrc`/AppleDouble file.
- [ ] Map `shïp`/`spïn`/`wëap` stats into `ship.Spec` automatically
      (EV units → konex units conversion table).
- [ ] StuffIt5 ("Arsenic") and BinHex decoders are the hard 10% — keep using
      `unar` offline for that step, or bind libxad.
- [x] Recover the remaining ConEx ships — all nine 1997 banks now fly
      (`*-97` folders: pins, Dart, Defender, Gryphon, Trident, Necromancer,
      Tomaquad), sliced with specs, target/yard/comm art by
      `yodacon/data/export_ships.py`; plus the CustPicID landing views
      (ConEx / Exeon / Cenron) behind the dock screen.

## Gameplay

- [x] Landing on planets — far beyond the konex stub: landing is now an
      **atmospheric reentry minigame** (`internal/reentry`, the MHD plasma-
      shield envelope model flown as a corridor-following sim), ending on a
      spaceport screen (bar / refuel / trade / repairs). `L` near a planet.
- [ ] Use per-ship `CollisionRadius` instead of the flat 64-unit range.
- [ ] Ship-vs-ship elastic collisions (stubbed in konex `playerCollision`).
- [ ] `aicount` should actually drive AI head-count in new games.
- [x] Missions/trading, first pass — the 36 recovered missions run on the
      real EV bit machine (`internal/mission`, briefs included, with the
      unreachable-285 restoration fix), over the full base-EV + ConEx galaxy
      (`internal/galaxy`, jump routing on the 1997 links). Escort/combat
      objectives still auto-resolve on arrival — spawning the academy dudes
      into the flight world is the next slice.

## The reentry-trader campaign (landed in one long day)

- [x] Full gameplay loop: dock screens (bar / mission computer with chart +
      hops / commodity board / outfitter / shipyard), escorts & crew,
      berth saves, the DED screen, Trader mode with market world events.
- [x] The landing: seed-grown spaceport metropolis (ascitty formula, twin
      840 m landing roads, city on both banks, contour shores), mask-dilated
      plasma shield with dipole field lines, shock puffs, console dial
      cluster, sonic-boom fine, RCS/lithium/battery consumables, corridor
      discipline + guardian, and the seamless orbital → ILS-autoland final.
- [x] fastdraw batch renderer: the whole scene through one white texel and
      DrawTriangles — 60 fps at metropolis scale.
- [x] Approach School: ten new missions (300–309) extend the 1997 academy
      chain past Trading 202 — landing/heat lessons, then the commodity-ship
      doctrine ladder (`docs/FLIGHT-SCHOOL.md`).
- [ ] Spawn mission escort/combat ships into the flight world (objectives
      still auto-resolve on arrival).
- [ ] Outfitter: cargo pods (hold size), RCS bottles, coil upgrades — the
      market wants loadout choices beyond the power grid.
- [ ] Commodity board: per-station price history sparkline; buy/sell
      spreads at high-tech vs frontier ports.
- [ ] Takeoff: supersonic departure boom (symmetry with the landing fine).
- [ ] PilotState save versioning before the format calcifies.

## Engine

- [ ] Sound (konex disabled its OpenAL path; `snd `resources from the plugin
      are extracted as raw Mac `snd ` data awaiting conversion).
- [ ] Networking (konex's net_main was already commented out).
- [ ] Resolution options + fullscreen toggle in the video menu.
- [ ] Bitmap font from `font.bmp` for a more period-correct look.
- [ ] Gamepad support.
