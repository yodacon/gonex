// Package config reads and writes the player configuration, keeping konex's
// on-disk format: <Config><PlayerName Value="..."/>...</Config> in config.xml
// next to the executable's working directory.
package config

import (
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	PlayerName    string
	StarCount     int
	ShowFPS       bool
	GodMode       bool
	ShowHUD       bool
	ShowTarget    bool
	ShowMiniMap   bool
	VectorHUD     bool // the flying player's forward and thrust vectors
	PlayerShipID  int
	ServerAddress string
	Team          int
	AICount       int
}

func Default() *Config {
	return &Config{
		PlayerName:    "Pilot",
		StarCount:     1000,
		ShowFPS:       false,
		GodMode:       false,
		ShowHUD:       true,
		ShowTarget:    true,
		ShowMiniMap:   true,
		VectorHUD:     true,
		PlayerShipID:  12,
		ServerAddress: "127.0.0.1",
		Team:          1,
		AICount:       12,
	}
}

// setting mirrors konex's per-field XML shape: <Name Value="..." />.
type setting struct {
	XMLName xml.Name
	Value   string `xml:"Value,attr"`
}

type document struct {
	XMLName  xml.Name  `xml:"Config"`
	Settings []setting `xml:",any"`
}

func Load(path string) *Config {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	var doc document
	if xml.Unmarshal(raw, &doc) != nil {
		return cfg
	}
	for _, s := range doc.Settings {
		v := s.Value
		switch s.XMLName.Local {
		case "PlayerName":
			cfg.PlayerName = v
		case "StarCount":
			cfg.StarCount, _ = strconv.Atoi(v)
		case "ShowFPS":
			cfg.ShowFPS = v == "1"
		case "GodMode":
			cfg.GodMode = v == "1"
		case "ShowHUD":
			cfg.ShowHUD = v == "1"
		case "ShowTarget":
			cfg.ShowTarget = v == "1"
		case "ShowMiniMap":
			cfg.ShowMiniMap = v == "1"
		case "VectorHUD":
			cfg.VectorHUD = v == "1"
		case "PlayerShipID":
			cfg.PlayerShipID, _ = strconv.Atoi(v)
		case "ServerAddress":
			cfg.ServerAddress = v
		case "Team":
			cfg.Team, _ = strconv.Atoi(v)
		case "AICount":
			cfg.AICount, _ = strconv.Atoi(v)
		}
	}
	return cfg
}

func (c *Config) Save(path string) error {
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	doc := document{Settings: []setting{
		{xml.Name{Local: "PlayerName"}, c.PlayerName},
		{xml.Name{Local: "StarCount"}, strconv.Itoa(c.StarCount)},
		{xml.Name{Local: "ShowFPS"}, b(c.ShowFPS)},
		{xml.Name{Local: "GodMode"}, b(c.GodMode)},
		{xml.Name{Local: "ShowHUD"}, b(c.ShowHUD)},
		{xml.Name{Local: "ShowTarget"}, b(c.ShowTarget)},
		{xml.Name{Local: "ShowMiniMap"}, b(c.ShowMiniMap)},
		{xml.Name{Local: "VectorHUD"}, b(c.VectorHUD)},
		{xml.Name{Local: "PlayerShipID"}, strconv.Itoa(c.PlayerShipID)},
		{xml.Name{Local: "ServerAddress"}, c.ServerAddress},
		{xml.Name{Local: "Team"}, strconv.Itoa(c.Team)},
		{xml.Name{Local: "AICount"}, strconv.Itoa(c.AICount)},
	}}
	out, err := xml.MarshalIndent(doc, "", "    ")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return os.WriteFile(path, out, 0o644)
}
