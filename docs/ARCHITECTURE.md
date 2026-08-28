# Gonex architecture

The port keeps konex's module boundaries where they were good and straightens
them where C forced global state. Three rules organize the code:

1. **The simulation cannot see the engine.** `internal/world` knows nothing
   about Ebitengine, keyboards, or pixels. It takes a fixed `dt`, mutates
   entities, and reports events through a `Notify` callback.
2. **Rendering is a function of world + camera.** `internal/render` walks the
   entity list and draws; it owns the non-ship texture catalogs.
3. **The app layer is the only place that knows everything.** `internal/app`
   maps input to ship controls, builds the window set, registers console
   commands and owns the mode switch (menu ↔ playing).

## Packages

| Package | konex ancestor | Role |
|---|---|---|
| `assets` | `vid_image.cpp` + data dir | `go:embed`ed game data, TGA/PNG decode, texture cache |
| `internal/tga` | `vid_image.cpp` | minimal Targa decoder (type 2, 24/32-bit) |
| `internal/gmath` | `vector.h` | `Vec2`, heading↔vector, angle wrap |
| `internal/config` | `config.cpp` | konex-format `config.xml` load/save |
| `internal/ship` | `ships.cpp` | specs.xml + 36-frame sprite banks, catalog |
| `internal/world` | `entity/player/missile/...` | entities, physics, combat, scoring |
| `internal/ai` | `ai.cpp` | `Rabies` and `Siege` controllers |
| `internal/scene` | `game.cpp` loaders | scene/map XML → world population |
| `internal/camera` | `view.cpp` | world (Y-up) ↔ screen (Y-down) |
| `internal/starfield` | `stars.cpp` | screen-space parallax stars |
| `internal/console` | `console.cpp` | scrollback, notify feed, command registry |
| `internal/save` | `game_Save/Load` | JSON snapshot (replaces pointer-dump save.dat) |
| `internal/render` | `vid_main.cpp` draw calls | entity drawing, map blips, target overlay |
| `internal/ui` | `interface.cpp`, `menu.cpp` | windows, menus, FPS graph, console view |
| `internal/app` | `konex.cpp`, `sys_main.cpp` | ebiten.Game, wiring, input, commands |

## Entity model

konex used one 400-slot struct array with a type tag, function pointers for
control, and `used` flags. Gonex keeps the *shape* of that design — one flat
entity list, uniform movement, per-kind behavior — but expresses it with a
small interface:

```go
type Entity interface {
    Update(w *World, dt float64)
    Pos() gmath.Vec2
    Alive() bool
    body() *Body // unexported: only world moves things
}
```

`Ship`, `Missile`, `Explosion`, `Item`, `Planet` and `SpawnPoint` embed
`Body` (position + velocity). `World.Update` moves everything, runs per-kind
updates, then sweeps the dead — the same three phases as
`entities_ProcessMovement`, without the fixed array or the ID arithmetic.

Ship *control* stays data-driven like the original's function pointers:
a `Controller` interface with AI implementations in `internal/ai`, while the
human player's keyboard mapping lives in `internal/app` and calls the same
control surface (`TurnLeft`, `Thrust`, `Fire`, `FaceToward`...).

## Faithful constants

Collision range 64, missile speed 2000 / TTL 2 s, fire cooldown 0.2 s,
health recharge 1 hp / 3 s, item TTL 15 s and 1-in-5 drop chance, explosion
1 s across 17 frames with four ghost copies, minimap/HUD/window geometry,
menu tree, and the console's 270 px drop-down all carry konex's values.
