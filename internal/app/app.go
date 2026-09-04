// Package app wires everything into an ebiten.Game: game modes, windows,
// menus, input and the console. It is the Go equivalent of konex's
// konex.cpp + interface.cpp + game.cpp glue.
package app

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"yodacon.org/gonex/assets"
	"yodacon.org/gonex/internal/camera"
	"yodacon.org/gonex/internal/config"
	"yodacon.org/gonex/internal/console"
	"yodacon.org/gonex/internal/galaxy"
	"yodacon.org/gonex/internal/gmath"
	"yodacon.org/gonex/internal/mission"
	"yodacon.org/gonex/internal/render"
	"yodacon.org/gonex/internal/save"
	"yodacon.org/gonex/internal/scene"
	"yodacon.org/gonex/internal/ship"
	"yodacon.org/gonex/internal/starfield"
	"yodacon.org/gonex/internal/ui"
	"yodacon.org/gonex/internal/universe"
	"yodacon.org/gonex/internal/world"
)

const (
	ScreenW = 1024
	ScreenH = 768
	dt      = 1.0 / 60 // Ebitengine's fixed tick

	AppName    = "Gonex"
	AppVersion = "v0.1a3"
	ConfigPath = "config.xml"
)

type App struct {
	Cfg      *config.Config
	Catalog  *ship.Catalog
	Renderer *render.Renderer

	Console *console.Console
	cview   ui.ConsoleView

	wm   ui.Manager
	menu ui.Menu

	menuWin, hudWin, miniMapWin, fullMapWin *ui.Window
	targetWin, shipSelectWin, fpsWin        *ui.Window
	galaxyWin                               *ui.Window

	World  *world.World
	cam    *camera.Camera
	stars  *starfield.Field
	paused bool

	// the reentry-trader layer
	mode    appMode
	gal     *galaxy.Galaxy
	msn     *mission.Table
	voy     *Voyage
	uni     *universe.Universe // the economy behind the sky; see economy.go
	uniDay  int                // the day the economy has been advanced to
	entry   *entryState
	dock    *dockState
	deorbit *deorbitState
	warp    *warpState
	docking *dockingRequest
	takeoff *takeoffState
	fxlab   *fxlabState

	// the engineering layer: one power grid under every mode
	engPreset engPreset
	engNoteCD float64

	background *ebiten.Image
	skyImg     *ebiten.Image // offscreen for the banked entry/takeoff sky
	skyKey     uint64        // quantized inputs of the last sky render
	started    time.Time
	quitting   bool

	shotPath  string // GONEX_SHOT: dump a frame here and exit (dev)
	shotFrame int
	shotAt    int

	recDir     string // GONEX_REC: dump every Nth frame here for gif assembly
	recEvery   int
	recN       int
	recCaption string // GONEX_REC_CAPTION: burned into every recorded frame

	demoStellar int // GONEX_BOOT "demo <spob>": a scripted full landing
	demoT       float64
	demoHold    float64

	deadT     float64 // seconds on the DED screen
	deadPlace string  // where the ship was lost
}

func New() (*App, error) {
	a := &App{
		Cfg:     config.Load(ConfigPath),
		Console: console.New(),
		cam:     camera.New(ScreenW, ScreenH),
		started: time.Now(),
	}
	a.cview.Console = a.Console

	var err error
	if a.Catalog, err = ship.LoadCatalog(); err != nil {
		return nil, err
	}
	if a.Renderer, err = render.New(a.Catalog); err != nil {
		return nil, err
	}
	if a.background, err = assets.Image("data/logos/conex.tga"); err != nil {
		return nil, err
	}
	if a.gal, err = galaxy.Load(); err != nil {
		return nil, err
	}
	if a.msn, err = mission.Load(); err != nil {
		return nil, err
	}

	// A throwaway world provides the starfield's RNG before any game starts.
	seedWorld := world.New(a.Catalog, time.Now().UnixNano())
	a.stars = starfield.New(a.Cfg.StarCount, ScreenW, ScreenH, seedWorld.Rand)

	a.buildWindows()
	a.registerCommands()
	a.showMainMenu()

	a.Console.Printf("**************************************************************************")
	a.Console.Printf("* Welcome to %s %s...", AppName, AppVersion)
	a.Console.Printf("* Game initialization complete...")
	a.Console.Printf("**************************************************************************")

	// GONEX_BOOT skips the menus for development: "flight" starts a game,
	// "war" drops into the three-colour deathmatch, and "entry <stellar>"
	// starts a game and goes straight onto the corridor.
	if boot := os.Getenv("GONEX_BOOT"); boot == "load" {
		a.loadGame() // straight into the last berth save
		a.hideMenu()
	} else if boot == "war" {
		a.newGame("deathmatch.xml") // the three-colour supply war, for a look at it
		a.hideMenu()
	} else if boot != "" {
		a.newGame("sundaydrive.xml")
		if id := 0; a.running() {
			if boot == "vector" {
				// The vector HUD bench: put the player under way with the
				// nose ACROSS the velocity, which is the only attitude where
				// the coast line and the thrust curve visibly disagree — and
				// therefore the only one worth photographing the instrument in.
				if p := a.World.MainPlayer; p != nil {
					p.V = gmath.V(190, -120)
					p.Heading = 55
				}
			} else if boot == "fxlab" {
				// the effects bench: no corridor, every input on a key
				a.startFxLab()
			} else if _, err := fmt.Sscanf(boot, "entry %d", &id); err == nil && id > 0 {
				a.startEntry(id)
				if a.entry != nil && strings.HasSuffix(boot, " auto") {
					a.entry.auto = true
				}
			} else if _, err := fmt.Sscanf(boot, "deorbit %d", &id); err == nil && id > 0 {
				a.startDeorbit(id)
			} else if _, err := fmt.Sscanf(boot, "demo %d", &id); err == nil && id > 0 {
				// the scripted pilot: spawn on approach, then fly the whole
				// handshake -> deorbit -> auto entry -> touchdown -> dock
				a.demoStellar = id
				for _, e := range a.World.Entities {
					if pl, ok := e.(*world.Planet); ok && pl.StellarID == id {
						if p := a.World.MainPlayer; p != nil {
							p.P = pl.Pos().Add(gmath.V(120, -90))
							p.V = gmath.Vec2{}
							p.Heading = 340
						}
					}
				}
			} else if _, err := fmt.Sscanf(boot, "dock %d", &id); err == nil && id > 0 {
				a.dock = &dockState{stellar: id}
				a.mode = modeLanded
				a.miniMapWin.Visible, a.hudWin.Visible, a.targetWin.Visible = false, false, false
				a.fullMapWin.Visible, a.galaxyWin.Visible = false, false
				// dev: land straight in a sub-view for screenshots
				if strings.HasSuffix(boot, " bar") {
					a.dock.view = dockBar
					a.dock.rollMissions(a)
				} else if strings.HasSuffix(boot, " govern") {
					a.dock.view = dockGovern
					a.openGovern()
				} else if strings.HasSuffix(boot, " outfit") {
					a.dock.view = dockOutfit
				} else if strings.HasSuffix(boot, " trade") {
					a.dock.view = dockTrade
				} else if strings.HasSuffix(boot, " yard") {
					a.dock.view = dockYard
					a.dock.yardSel = a.Cfg.PlayerShipID - 1
				} else if strings.HasSuffix(boot, " missions") {
					a.dock.view = dockMissions
					a.dock.rollMissions(a)
				} else if strings.HasSuffix(boot, " journal") {
					a.dock.view = dockJournal
				} else if strings.HasSuffix(boot, " trade") {
					a.dock.view = dockTrade
				}
			} else if _, err := fmt.Sscanf(boot, "takeoff %d", &id); err == nil && id > 0 {
				a.startTakeoff(id)
			} else if _, err := fmt.Sscanf(boot, "route %d", &id); err == nil && id > 0 {
				a.voy.Route = a.gal.Route(a.voy.System, id)
			} else if _, err := fmt.Sscanf(boot, "warp %d", &id); err == nil && id > 0 {
				a.voy.Route = a.gal.Route(a.voy.System, id)
				if beacon, _, ok := a.warpBeacon(); ok && a.World.MainPlayer != nil {
					a.World.MainPlayer.P = beacon
					a.tryJump()
				}
			}
		}
	}
	a.recDir = os.Getenv("GONEX_REC")
	a.recCaption = os.Getenv("GONEX_REC_CAPTION")
	a.recEvery = 15
	if n := 0; os.Getenv("GONEX_REC_EVERY") != "" {
		if _, err := fmt.Sscanf(os.Getenv("GONEX_REC_EVERY"), "%d", &n); err == nil && n > 0 {
			a.recEvery = n
		}
	}
	// GONEX_CMD runs console commands at boot, semicolon-separated. It is how
	// a headless run prints the trade journal, and how a screenshot can show
	// a screen that is normally three keystrokes deep.
	if cmds := os.Getenv("GONEX_CMD"); cmds != "" {
		for _, line := range strings.Split(cmds, ";") {
			a.Console.Do(line)
		}
	}
	a.shotPath = os.Getenv("GONEX_SHOT")
	a.shotAt = 300
	if n := 0; os.Getenv("GONEX_SHOT_FRAME") != "" {
		if _, err := fmt.Sscanf(os.Getenv("GONEX_SHOT_FRAME"), "%d", &n); err == nil {
			a.shotAt = n
		}
	}
	return a, nil
}

func (a *App) running() bool { return a.World != nil }

// newGame builds a world for a scene and drops the player in, mirroring
// game_CreateTeamDeathmatch / game_CreateSundayDrive.
func (a *App) newGame(scenePath string) {
	w := world.New(a.Catalog, time.Now().UnixNano())
	w.Notify = a.Console.Notifyf
	w.GodMode = a.Cfg.GodMode
	if err := scene.Load(w, scenePath); err != nil {
		a.Console.Printf("GAME: Failed to load %s: %v", scenePath, err)
		return
	}
	player := w.NewShip(a.Cfg.PlayerShipID, world.Team(a.Cfg.Team), a.Cfg.PlayerName, world.KindLocal)
	// Launch from your own colour's pad, where the squadron is and where you
	// will respawn anyway — not adrift in the middle of the map.
	if sp := w.SpawnPointFor(player.Team); sp != nil {
		player.P = sp.P
	}
	w.MainPlayer, w.ViewShip = player, player

	a.World = w
	a.mode = modeFlight
	a.voy = newVoyage(time.Now().UnixNano())
	a.wireGrid()
	a.enterSystem(a.voy.System)
	a.seedUniverse()
	a.setGameStatus(true)
	a.Console.Printf("GAME: Scene loaded successfully %s", scenePath)
	a.Console.Printf("GAME: Docked traffic control: M chart, J jump, L land near a planet")
}

func (a *App) endGame() {
	a.World = nil
	a.voy, a.entry, a.dock, a.deorbit = nil, nil, nil, nil
	a.uni, a.uniDay = nil, 0
	a.warp, a.docking, a.takeoff = nil, nil, nil
	a.mode = modeFlight
	a.setGameStatus(false)
}

func (a *App) saveGame() {
	if !a.running() {
		a.Console.Printf("- No game to save...")
		return
	}
	dockStellar := 0
	if a.mode == modeLanded && a.dock != nil {
		dockStellar = a.dock.stellar
	}
	var pilot *save.PilotState
	if a.voy != nil {
		pilot = a.voy.pilotState(a.Cfg.PlayerShipID, dockStellar)
		if a.uni != nil {
			a.releaseResidents() // a hull is in exactly one place; on disk, that is the census
			a.stepUniverse()
			if raw, err := json.Marshal(a.uni.Snapshot()); err == nil {
				pilot.Universe = raw
			}
			a.residentsIn(a.voy.System)
		}
	}
	if err := save.Write(a.World, pilot, save.DefaultPath); err != nil {
		a.Console.Printf("- Save failed: %v", err)
		return
	}
	a.hideMenu()
	a.Console.Printf("- Game saved...")
}

func (a *App) loadGame() {
	w := world.New(a.Catalog, time.Now().UnixNano())
	w.Notify = a.Console.Notifyf
	w.GodMode = a.Cfg.GodMode
	pilot, err := save.Read(w, save.DefaultPath)
	if err != nil {
		a.Console.Printf("- Load failed: %v", err)
		return
	}
	a.World = w
	a.entry, a.deorbit, a.warp, a.docking, a.takeoff = nil, nil, nil, nil, nil
	if pilot != nil {
		a.Cfg.PlayerShipID = pilot.PlayerShipID
		a.voy = voyageFrom(pilot, time.Now().UnixNano())
	} else {
		a.voy = newVoyage(time.Now().UnixNano())
	}
	a.wireGrid()
	a.enterSystem(a.voy.System)
	a.seedUniverse()
	if pilot != nil && len(pilot.Universe) > 0 && a.uni != nil {
		var snap universe.Snapshot
		if err := json.Unmarshal(pilot.Universe, &snap); err == nil && snap.Seed == a.uni.Seed {
			a.releaseResidents()
			a.uni.Restore(&snap)
			a.uniDay = a.uni.Day
			a.syncPlanetStock()
			a.residentsIn(a.voy.System)
		}
	}
	a.stepUniverse()
	a.setGameStatus(true)
	if pilot != nil && pilot.DockStellar > 0 {
		a.dock = &dockState{stellar: pilot.DockStellar}
		a.mode = modeLanded
		a.miniMapWin.Visible, a.hudWin.Visible, a.targetWin.Visible = false, false, false
	} else {
		a.dock = nil
		a.mode = modeFlight
	}
	a.Console.Printf("- Game loaded...")
}

func (a *App) quit() {
	a.Console.Printf("* Game shutdown initiated...")
	if err := a.Cfg.Save(ConfigPath); err != nil {
		a.Console.Printf("CONFIG: save failed: %v", err)
	} else {
		a.Console.Printf("CONFIG: Configuration saved successfully")
	}
	a.quitting = true
}

func (a *App) Update() error {
	if a.quitting {
		return ebiten.Termination
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyBackquote) {
		a.Console.Toggle()
	}
	a.cview.Update(dt)

	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		// On the concourse's sub-screens Escape is "back", not "menu": a
		// player deep in the outfitter wants the concourse, and got the
		// game menu over the top of it instead.
		if !a.dockBack() {
			a.toggleMenu()
		}
	}
	if a.running() && a.Console.State == console.Hidden &&
		a.mode != modeEntry && // on the corridor, TAB is the algebra panel
		inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		a.toggleWindow(a.fullMapWin)
	}

	a.wm.Update(ScreenW, ScreenH)

	if a.running() && !a.paused {
		switch a.mode {
		case modeWarp:
			a.updateWarp()
			a.updateFlightGrid() // the tunnel is still vacuum: stores refill
		case modeDeorbit:
			a.updateDeorbit()
		case modeEntry:
			if a.Console.State == console.Hidden {
				a.updateEntry()
			}
		case modeLanded:
			if a.Console.State == console.Hidden {
				a.updateDock()
			}
		case modeDead:
			a.updateDead()
		case modeTakeoff:
			a.updateTakeoff()
			a.updateFlightGrid() // climbing out: the plant is already banking
		case modeFxLab:
			a.updateFxLab()
		default:
			if a.Console.State == console.Hidden {
				a.handlePlayerInput()
			}
			a.updateDemo()
			a.updateDocking()
			a.updateFlightGrid()
			a.World.Update(dt)
			if a.World.ViewShip != nil {
				a.cam.Follow(a.World.ViewShip.Pos())
			}
			a.stars.Update(a.cam.X, a.cam.Y)
		}
	}
	return nil
}

func (a *App) Draw(screen *ebiten.Image) {
	switch {
	case a.running() && a.mode == modeWarp && a.warp != nil:
		screen.Fill(color.RGBA{2, 4, 8, 255})
		a.drawWarp(screen)
	case a.running() && a.mode == modeDeorbit && a.deorbit != nil:
		screen.Fill(color.RGBA{0, 0, 0, 255})
		a.stars.Draw(screen)
		a.drawDeorbit(screen)
	case a.running() && a.mode == modeEntry && a.entry != nil:
		screen.Fill(color.RGBA{5, 7, 10, 255})
		a.stars.Draw(screen)
		a.drawEntry(screen)
	case a.running() && a.mode == modeLanded && a.dock != nil:
		a.drawDock(screen)
	case a.running() && a.mode == modeDead:
		a.drawDead(screen)
	case a.running() && a.mode == modeTakeoff && a.takeoff != nil:
		screen.Fill(color.RGBA{2, 4, 8, 255})
		a.stars.Draw(screen)
		a.drawTakeoff(screen)
	case a.running() && a.mode == modeFxLab && a.fxlab != nil:
		screen.Fill(color.RGBA{5, 7, 10, 255})
		a.drawFxLab(screen)
	case a.running():
		screen.Fill(color.RGBA{0, 0, 0, 255})
		a.stars.Draw(screen)
		a.Renderer.DrawWorld(screen, a.World, a.cam)
		a.Renderer.DrawTargetOverlay(screen, a.World, a.cam)
		a.drawFlightOverlays(screen)
		a.drawVectorHUD(screen)
		a.drawEngPanel(screen)
	default:
		a.drawSplash(screen)
	}
	if a.running() {
		a.drawModeBanner(screen)
	}
	a.cview.DrawNotify(screen, dt)
	a.wm.Draw(screen)
	a.cview.Draw(screen, ScreenW)

	// dev frame dump: GONEX_SHOT=<path> writes frame 300 and exits.
	if a.shotPath != "" {
		if a.shotFrame++; a.shotFrame == a.shotAt {
			dumpFrame(screen, a.shotPath)
			a.quitting = true
		}
	}
	// dev recording: GONEX_REC=<dir> writes every Nth frame for gif assembly.
	// GONEX_REC_CAPTION burns a flight-recorder caption bar into the frames
	// themselves — version, build date, take — in the game's own font, so a
	// published recording carries its provenance in-band.
	if a.recDir != "" {
		if a.recCaption != "" {
			vector.DrawFilledRect(screen, 0, ScreenH-20, ScreenW, 20,
				color.RGBA{5, 7, 10, 235}, false)
			ui.DrawText(screen, a.recCaption, 10, float64(ScreenH)-16, 0.85)
		}
		if a.recN++; a.recN%a.recEvery == 0 {
			dumpFrame(screen, fmt.Sprintf("%s/f%06d.png", a.recDir, a.recN))
		}
	}
}

func dumpFrame(screen *ebiten.Image, path string) {
	pix := make([]byte, 4*ScreenW*ScreenH)
	screen.ReadPixels(pix)
	for i := 3; i < len(pix); i += 4 {
		pix[i] = 255
	}
	img := &image.RGBA{Pix: pix, Stride: 4 * ScreenW,
		Rect: image.Rect(0, 0, ScreenW, ScreenH)}
	if f, err := os.Create(path); err == nil {
		png.Encode(f, img)
		f.Close()
	}
}

// drawSplash shows the ConEx logo with the half-second fade konex opened with.
func (a *App) drawSplash(screen *ebiten.Image) {
	alpha := float32(time.Since(a.started).Seconds() / 0.5)
	if alpha > 1 {
		alpha = 1
	}
	b := a.background.Bounds()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(ScreenW/float64(b.Dx()), ScreenH/float64(b.Dy()))
	op.ColorScale.ScaleAlpha(alpha)
	screen.DrawImage(a.background, op)
}

func (a *App) Layout(_, _ int) (int, int) { return ScreenW, ScreenH }

// uptime supports the console's uptime command.
func (a *App) uptime() string {
	return fmt.Sprintf("- Running for %0.2f second(s)", time.Since(a.started).Seconds())
}
