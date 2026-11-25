package main

import (
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestConfigDefaultsToMinecraftPortAndTwentySlots(t *testing.T) {
	cfg, err := loadConfig(env(nil))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Port != "25565" {
		t.Errorf("port = %q, want 25565", cfg.Port)
	}
	if cfg.MaxPlayers != 20 {
		t.Errorf("max players = %d, want 20", cfg.MaxPlayers)
	}
	if cfg.MOTD != "Grus native stub" {
		t.Errorf("motd = %q", cfg.MOTD)
	}
	if cfg.selfEnabled() {
		t.Error("self API enabled without GRUS_URL and GRUS_TOKEN")
	}
}

func TestConfigPrefersGrusPortOverServerPort(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"GRUS_PORT": "30001", "SERVER_PORT": "30002"}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Port != "30001" {
		t.Errorf("port = %q, want 30001", cfg.Port)
	}
}

func TestConfigFallsBackToServerPort(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"SERVER_PORT": "30002"}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Port != "30002" {
		t.Errorf("port = %q, want 30002", cfg.Port)
	}
}

func TestConfigMotdNamesInstanceWhenGiven(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"GRUS_INSTANCE": "stub-7"}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.MOTD != "Grus native stub stub-7" {
		t.Errorf("motd = %q", cfg.MOTD)
	}
}

func TestConfigExplicitMotdAndMaxWin(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"GRUS_STUB_MOTD": "Hello", "GRUS_STUB_MAX": "64", "GRUS_INSTANCE": "x",
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.MOTD != "Hello" || cfg.MaxPlayers != 64 {
		t.Errorf("motd = %q max = %d", cfg.MOTD, cfg.MaxPlayers)
	}
}

func TestConfigRejectsBadNumbers(t *testing.T) {
	cases := map[string]map[string]string{
		"port not a number": {"GRUS_PORT": "http"},
		"port zero":         {"GRUS_PORT": "0"},
		"port too large":    {"GRUS_PORT": "70000"},
		"max zero":          {"GRUS_STUB_MAX": "0"},
		"max not a number":  {"GRUS_STUB_MAX": "many"},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfig(env(vars)); err == nil {
				t.Fatal("bad value accepted")
			}
		})
	}
}

func TestConfigEnablesSelfWhenURLAndTokenSet(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"GRUS_URL": "http://grus.test", "GRUS_TOKEN": "tok"}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !cfg.selfEnabled() {
		t.Fatal("self API disabled although URL and token are set")
	}
	if !strings.HasPrefix(cfg.URL, "http://grus.test") {
		t.Errorf("url = %q", cfg.URL)
	}
}
