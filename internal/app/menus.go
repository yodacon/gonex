package app

import "yodacon.org/gonex/internal/ui"

// The menu tree matches konex's menu.cpp, minus the multiplayer and video
// pages it had already commented out.

func (a *App) showMenuFor(inGame bool) {
	if inGame {
		a.showGameMenu()
	} else {
		a.showMainMenu()
	}
}

func (a *App) showMainMenu() {
	if a.running() {
		a.showGameMenu()
		return
	}
	a.menu.SetItems(
		&ui.MenuItem{Caption: "Single Player", Action: a.showSinglePlayerMenu},
		&ui.MenuItem{Caption: "Options", Action: a.showOptionsMenu},
		&ui.MenuItem{Caption: "Load Game", Action: a.loadGame},
		&ui.MenuItem{Caption: "Quit", Action: a.quit},
	)
}

func (a *App) showSinglePlayerMenu() {
	a.menu.SetItems(
		&ui.MenuItem{Caption: "Team Deathmatch", Action: func() { a.newGame("deathmatch.xml") }},
		&ui.MenuItem{Caption: "Sunday Drive", Action: func() { a.newGame("sundaydrive.xml") }},
		&ui.MenuItem{Caption: "Trader", Action: a.newTraderGame},
		&ui.MenuItem{Caption: "Return to Main Menu", Action: a.showMainMenu},
	)
}

// newTraderGame is the commodity campaign: the deathmatch world's traffic
// is live — so the lanes are dangerous — and the game is the board: buy
// low here, sell high there, and use the chart to leave a fight you
// cannot win.
func (a *App) newTraderGame() {
	a.newGame("deathmatch.xml")
	if !a.running() {
		return
	}
	a.stake(8000) // a trader starts on margin, not on savings — and the margin is the home world's
	a.Console.Notifyf("TRADER — the board is at every port (T when docked).")
	a.Console.Notifyf("Buy low, haul, sell high. The news moves the prices.")
	a.Console.Notifyf("The lanes are armed: Tab maps the fight, M charts the way OUT of it, J jumps.")
}

func (a *App) showGameMenu() {
	a.menu.SetItems(
		&ui.MenuItem{Caption: "End Current Game", Action: a.endGame},
		&ui.MenuItem{Caption: "Options", Action: a.showOptionsMenu},
		&ui.MenuItem{Caption: "Load Game", Action: a.loadGame},
		&ui.MenuItem{Caption: "Save Game", Action: a.saveGame},
		&ui.MenuItem{Caption: "Select Ship", Action: func() {
			a.toggleWindow(a.shipSelectWin)
			if a.shipSelectWin.Visible {
				a.hideMenu()
			}
		}},
		&ui.MenuItem{Caption: "Resume Game", Action: a.toggleMenu},
		&ui.MenuItem{Caption: "Quit", Action: a.quit},
	)
}

func (a *App) showOptionsMenu() {
	a.menu.SetItems(
		&ui.MenuItem{Caption: "Display", Action: a.showDisplayMenu},
		&ui.MenuItem{Caption: "Player", Action: a.showPlayerMenu},
		&ui.MenuItem{Caption: "Return to Main Menu", Action: a.showMainMenu},
	)
}

func (a *App) showDisplayMenu() {
	items := []*ui.MenuItem{
		{Caption: "Toggle FPS", Action: a.toggleFPS},
	}
	if a.running() {
		items = append(items,
			&ui.MenuItem{Caption: "Toggle Mini Map", Action: a.miniMapWin.OnClose},
			&ui.MenuItem{Caption: "Toggle HUD", Action: a.hudWin.OnClose},
			&ui.MenuItem{Caption: "Toggle Target", Action: a.targetWin.OnClose},
		)
	}
	items = append(items, &ui.MenuItem{Caption: "Return to Options", Action: a.showOptionsMenu})
	a.menu.SetItems(items...)
}

func (a *App) showPlayerMenu() {
	a.menu.SetItems(
		&ui.MenuItem{Caption: "Player Name", Input: &a.Cfg.PlayerName},
		&ui.MenuItem{Caption: "Return to Options", Action: a.showOptionsMenu},
	)
}
