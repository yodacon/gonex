# Gonex

A Go port of **konex** — Joshua Bussdieker's 2005 C++/OpenGL remake of
**ConEx**, Paul Richeson's 1997 Escape Velocity plugin. Third generation of
the same little space war:

| Year | Name  | Form |
|------|-------|------|
| 1997 | ConEx | Escape Velocity plugin (Mac resource fork, 538 resources) |
| 2005 | konex | standalone C++ / OpenGL / X11+Win32 game |
| 2026 | Gonex | Go / [Ebitengine](https://ebitengine.org) port |

Top-down team-deathmatch space combat: three teams, twelve ships (plus one
recovered from 1997 — see below), missiles, health/money drops, respawns at
team spawn points, and a draggable in-game window UI with a drop-down
developer console.

## Run

```sh
go run ./cmd/gonex
```

All game data is embedded in the binary. `config.xml` (konex-compatible) and
`save.json` are written to the working directory.

## Controls

| Key | Action |
|-----|--------|
| Arrows | turn / thrust / reverse |
| Space | fire |
| X or Keypad-0 | brake |
| Left Shift | target nearest enemy |
| Left Ctrl | toggle autotarget |
| Tab | full map |
| Escape | menu (pauses) |
| ` (backquote) | developer console |

Console commands: `god`, `spawn`, `stat`, `loc`, `uptime`, `team N`,
`playership N`, `starcount N`, `listships`, `listentities`, `viewentity N`,
`savegame`, `loadgame`, `fps`, `hud`, `minimap`, `target`, `quit`.

## The Yodacon '97

Ship 13 in the shipyard is the **Yodacon** exactly as it shipped in 1997:
its 70×70 sprite rotations were decoded straight out of the ConEx plugin's
resource fork (PICT 20617 + mask 20618) along with its target, shipyard and
comm pictures. The extraction pipeline lives in the yodacon repo
(`tools/rsrc_extract.py`, `tools/pict_decode.py`).

## Project layout

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Assets under `assets/data`
are konex's art (GPL-2, © Joshua Bussdieker) plus art recovered from ConEx.

## Deviations from konex

- Fixed 60 Hz timestep (Ebitengine's tick) instead of variable frame dt.
- Text renders with a portable bitmap face instead of X11/WGL font lists.
- Save/load is a real JSON snapshot; konex's `save.dat` wrote raw pointers.
- Networking and sound are omitted — both were already stubbed out upstream.
- Everything else (speeds, cooldowns, ranges, scoring, UI layout, menu tree,
  console behavior) follows the original values.

## License

GPL-2.0 (see [COPYING](COPYING)), as a derivative of konex,
Copyright (C) 2005 Joshua B. Bussdieker.
