package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.xml")
	cfg := Default()
	cfg.PlayerName = "Ecco"
	cfg.StarCount = 1234
	cfg.GodMode = true
	cfg.PlayerShipID = 7
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if *got != *cfg {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, cfg)
	}
}

func TestLoadMissingGivesDefaults(t *testing.T) {
	got := Load(filepath.Join(t.TempDir(), "nope.xml"))
	if *got != *Default() {
		t.Errorf("missing file should load defaults, got %+v", got)
	}
}

// TestLoadKonexFormat pins compatibility with an original konex config.xml.
func TestLoadKonexFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.xml")
	konex := `<Config>
    <PlayerName Value="Ecco" />
    <StarCount Value="1000" />
    <ShowFPS Value="1" />
    <GodMode Value="1" />
    <PlayerShipID Value="12" />
    <Team Value="1" />
</Config>`
	if err := os.WriteFile(path, []byte(konex), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got.PlayerName != "Ecco" || !got.GodMode || got.PlayerShipID != 12 {
		t.Errorf("konex config parsed wrong: %+v", got)
	}
}
