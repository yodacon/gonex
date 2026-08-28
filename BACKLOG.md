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
- [ ] Recover the remaining ConEx ships whose sprites live in the plugin
      (S/M/L Pin, Dart, Gryphon, Trident, Necromancer, Tomaquad already
      composited in `yodacon/extracted/sprites/`).

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

## Engine

- [ ] Sound (konex disabled its OpenAL path; `snd `resources from the plugin
      are extracted as raw Mac `snd ` data awaiting conversion).
- [ ] Networking (konex's net_main was already commented out).
- [ ] Resolution options + fullscreen toggle in the video menu.
- [ ] Bitmap font from `font.bmp` for a more period-correct look.
- [ ] Gamepad support.
