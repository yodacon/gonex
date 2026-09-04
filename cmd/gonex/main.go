// Gonex — a Go port of konex (Joshua Bussdieker's 2005 remake of Paul
// Richeson's 1997 Escape Velocity plugin ConEx).
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hajimehoshi/ebiten/v2"

	"yodacon.org/gonex/internal/app"
)

// singleThread says whether Ebitengine should keep Update, Draw and the GL
// calls on the main thread instead of its own render thread.
//
// On musl (Alpine, and so every Copal machine) a thread the Go runtime
// creates through cgo gets libc's default stack, which is 128 KB, and Mesa's
// llvmpipe compiles shaders on whichever thread holds the context — with a
// good deal more stack than that. The first glDrawElements dies with SIGSEGV
// inside libgallium, after "Game initialization complete". Measured on
// 4 September 2026 in the Copal VM (aarch64, Mesa 26.1.6): multi-threaded,
// dead on the first frame; single-threaded, fine. The main thread carries the
// process's full stack, so on musl the render loop stays there.
//
// GONEX_SINGLE_THREAD=1 forces it on anywhere; =0 turns it off on musl too,
// for comparing the two.
func singleThread() bool {
	switch os.Getenv("GONEX_SINGLE_THREAD") {
	case "1":
		return true
	case "0":
		return false
	}
	if runtime.GOOS != "linux" {
		return false
	}
	musl, _ := filepath.Glob("/lib/ld-musl-*.so.1")
	return len(musl) > 0
}

func main() {
	game, err := app.New()
	if err != nil {
		log.Fatalf("gonex: %v", err)
	}
	ebiten.SetWindowSize(app.ScreenW, app.ScreenH)
	ebiten.SetWindowTitle(app.AppName + " " + app.AppVersion)
	opts := &ebiten.RunGameOptions{SingleThread: singleThread()}
	if err := ebiten.RunGameWithOptions(game, opts); err != nil {
		log.Fatal(err)
	}
}
