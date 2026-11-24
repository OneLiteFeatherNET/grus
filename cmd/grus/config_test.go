package main

import (
	"strings"
	"testing"
	"time"
)

const testAdminToken = "test-admin-token-0123456789"

// fakeEnv returns a getenv backed by a map, so no test reads the real environment.
func fakeEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func validEnv() map[string]string {
	return map[string]string{
		"GRUS_DRIVER":        "process",
		"GRUS_PUBLIC_URL":    "http://127.0.0.1:18080",
		"GRUS_ADMIN_TOKEN":   testAdminToken,
		"GRUS_PROCESS_DIR":   "/tmp/unused-by-parse",
		"GRUS_MAX_INSTANCES": "7",
	}
}

func TestLoadConfigReadsDefaultsFromEnvironment(t *testing.T) {
	cfg, err := loadConfig(nil, fakeEnv(validEnv()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want :8080", cfg.Listen)
	}
	if cfg.MaxInstances != 7 {
		t.Errorf("MaxInstances = %d, want 7 from GRUS_MAX_INSTANCES", cfg.MaxInstances)
	}
	if cfg.HostID != "local" {
		t.Errorf("HostID = %q, want local for the process driver", cfg.HostID)
	}
	if cfg.ReconcileInterval != 5*time.Second {
		t.Errorf("ReconcileInterval = %s, want 5s", cfg.ReconcileInterval)
	}
}

func TestLoadConfigFlagOverridesEnvironment(t *testing.T) {
	cfg, err := loadConfig([]string{"-listen", "127.0.0.1:9999"}, fakeEnv(validEnv()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Listen != "127.0.0.1:9999" {
		t.Errorf("Listen = %q, want the flag value", cfg.Listen)
	}
}

func TestLoadConfigRejectsShortAdminToken(t *testing.T) {
	env := validEnv()
	env["GRUS_ADMIN_TOKEN"] = "short"
	if _, err := loadConfig(nil, fakeEnv(env)); err == nil {
		t.Fatal("loadConfig accepted an admin token shorter than 16 characters")
	}
}

func TestLoadConfigRequiresPublicURL(t *testing.T) {
	env := validEnv()
	delete(env, "GRUS_PUBLIC_URL")
	if _, err := loadConfig(nil, fakeEnv(env)); err == nil {
		t.Fatal("loadConfig accepted an empty GRUS_PUBLIC_URL")
	}
}

func TestLoadConfigRequiresNamespaceForKubernetes(t *testing.T) {
	env := validEnv()
	env["GRUS_DRIVER"] = "kubernetes"
	if _, err := loadConfig(nil, fakeEnv(env)); err == nil {
		t.Fatal("loadConfig accepted the kubernetes driver without GRUS_NAMESPACE")
	}
}

func TestLoadConfigDefaultsKubernetesHostIDToNamespace(t *testing.T) {
	env := validEnv()
	env["GRUS_DRIVER"] = "kubernetes"
	env["GRUS_NAMESPACE"] = "grus-dev"
	cfg, err := loadConfig(nil, fakeEnv(env))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.HostID != "k8s-grus-dev" {
		t.Errorf("HostID = %q, want k8s-grus-dev", cfg.HostID)
	}
}

func TestLoadConfigRejectsUnknownDriver(t *testing.T) {
	env := validEnv()
	env["GRUS_DRIVER"] = "docker"
	if _, err := loadConfig(nil, fakeEnv(env)); err == nil {
		t.Fatal("loadConfig accepted an unknown driver")
	}
}

func TestParseKeyValuesSplitsPairs(t *testing.T) {
	got, err := parseKeyValues("GRUS_POD_LABELS", "team=olf, tier=game")
	if err != nil {
		t.Fatalf("parseKeyValues: %v", err)
	}
	if got["team"] != "olf" || got["tier"] != "game" || len(got) != 2 {
		t.Errorf("parseKeyValues = %v, want team=olf and tier=game", got)
	}
}

func TestParseKeyValuesRejectsEntryWithoutEquals(t *testing.T) {
	_, err := parseKeyValues("GRUS_NODE_SELECTOR", "broken")
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("parseKeyValues error = %v, want one naming the bad entry", err)
	}
}

func TestSplitListDropsEmptyEntries(t *testing.T) {
	got := splitList("a, ,b,")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("splitList = %v, want [a b]", got)
	}
}
