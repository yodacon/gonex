// Gonex — a Go port of konex (Joshua Bussdieker's 2005 remake of Paul
// Richeson's 1997 Escape Velocity plugin ConEx).
package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"

	"yodacon.org/gonex/internal/app"
)

func main() {
	game, err := app.New()
	if err != nil {
		log.Fatalf("gonex: %v", err)
	}
	ebiten.SetWindowSize(app.ScreenW, app.ScreenH)
	ebiten.SetWindowTitle(app.AppName + " " + app.AppVersion)
	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
