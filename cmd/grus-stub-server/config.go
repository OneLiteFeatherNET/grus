package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// config is read from the environment. GRUS_URL and GRUS_TOKEN switch on the Grus self API; without
// them the stub only answers the server list and never talks to a controller.
type config struct {
	Port       string
	MOTD       string
	MaxPlayers int
	Instance   string
	URL        string
	Token      string
}

const defaultPort = "25565"

// loadConfig reads the stub configuration through getenv, which keeps it testable without touching the
// process environment.
func loadConfig(getenv func(string) string) (config, error) {
	var cfg config

	port := strings.TrimSpace(getenv("GRUS_PORT"))
	if port == "" {
		port = strings.TrimSpace(getenv("SERVER_PORT"))
	}
	if port == "" {
		port = defaultPort
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return cfg, fmt.Errorf("invalid port %q", port)
	}
	cfg.Port = port

	cfg.MaxPlayers = 20
	if raw := strings.TrimSpace(getenv("GRUS_STUB_MAX")); raw != "" {
		max, err := strconv.Atoi(raw)
		if err != nil || max < 1 {
			return cfg, errors.New("GRUS_STUB_MAX must be a positive integer")
		}
		cfg.MaxPlayers = max
	}

	cfg.Instance = strings.TrimSpace(getenv("GRUS_INSTANCE"))
	cfg.MOTD = getenv("GRUS_STUB_MOTD")
	if cfg.MOTD == "" {
		cfg.MOTD = "Grus native stub"
		if cfg.Instance != "" {
			cfg.MOTD += " " + cfg.Instance
		}
	}

	cfg.URL = strings.TrimRight(strings.TrimSpace(getenv("GRUS_URL")), "/")
	cfg.Token = strings.TrimSpace(getenv("GRUS_TOKEN"))
	return cfg, nil
}

func (c config) selfEnabled() bool {
	return c.URL != "" && c.Token != ""
}
