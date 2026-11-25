package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func host() (string, error) { return "MC01", nil }

func tokenFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseConfigReadsEnvironmentAndAppliesDefaults(t *testing.T) {
	env := map[string]string{
		"GRUS_URL": "https://grus-dev-api.apps.onelite.feather/", "GRUS_AGENT_TOKEN_FILE": tokenFile(t, "secret\n"),
		"GRUS_AGENT_ADDRESS": "10.200.2.35",
	}
	cfg, err := parseConfig(nil, envOf(env), host)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.URL != "https://grus-dev-api.apps.onelite.feather" || cfg.Token != "secret" || cfg.Host != "mc01" ||
		cfg.PortMin != 30000 || cfg.PortMax != 30100 || cfg.Dir != "./instances" || cfg.StopOnExit {
		t.Errorf("config = %+v", cfg)
	}
}

func TestParseConfigFlagsOverrideEnvironment(t *testing.T) {
	env := map[string]string{"GRUS_URL": "https://a", "GRUS_AGENT_TOKEN_FILE": tokenFile(t, "s"), "GRUS_AGENT_ADDRESS": "1.2.3.4"}
	cfg, err := parseConfig([]string{"--host", "mc02", "--port-range", "31000-31010", "--stop-on-exit=true", "--java", "/opt/java"}, envOf(env), host)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Host != "mc02" || cfg.PortMin != 31000 || cfg.PortMax != 31010 || !cfg.StopOnExit || cfg.Java != "/opt/java" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestParseConfigRejectsBadInput(t *testing.T) {
	good := func() map[string]string {
		return map[string]string{"GRUS_URL": "https://a", "GRUS_AGENT_TOKEN_FILE": tokenFile(t, "s"), "GRUS_AGENT_ADDRESS": "1.2.3.4"}
	}
	for name, mod := range map[string]func(map[string]string){
		"no url":            func(m map[string]string) { delete(m, "GRUS_URL") },
		"url without http":  func(m map[string]string) { m["GRUS_URL"] = "grus.example" },
		"no address":        func(m map[string]string) { delete(m, "GRUS_AGENT_ADDRESS") },
		"no token file":     func(m map[string]string) { delete(m, "GRUS_AGENT_TOKEN_FILE") },
		"missing file":      func(m map[string]string) { m["GRUS_AGENT_TOKEN_FILE"] = "/nonexistent/token" },
		"empty token":       func(m map[string]string) { m["GRUS_AGENT_TOKEN_FILE"] = tokenFile(t, " \n") },
		"bad range":         func(m map[string]string) { m["GRUS_AGENT_PORT_RANGE"] = "30100-30000" },
		"range not numbers": func(m map[string]string) { m["GRUS_AGENT_PORT_RANGE"] = "a-b" },
	} {
		env := good()
		mod(env)
		if _, err := parseConfig(nil, envOf(env), host); err == nil {
			t.Errorf("%s: parseConfig returned nil error", name)
		}
	}
	if _, err := parseConfig(nil, envOf(nil), func() (string, error) { return "", errors.New("x") }); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("missing config err = %v", err)
	}
}
