package main

import "testing"

func TestLoadConfigCRDsDefaultToOff(t *testing.T) {
	cfg, err := loadConfig(nil, fakeEnv(validEnv()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.CRDs {
		t.Error("CRDs = true, want false by default")
	}
}

func TestLoadConfigReadsCRDsFlagFromEnvironment(t *testing.T) {
	env := validEnv()
	env["GRUS_CRDS"] = "true"
	env["GRUS_NAMESPACE"] = "grus-dev"
	cfg, err := loadConfig(nil, fakeEnv(env))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !cfg.CRDs {
		t.Error("CRDs = false, want true from GRUS_CRDS")
	}
}

func TestLoadConfigRejectsCRDsWithoutNamespace(t *testing.T) {
	env := validEnv()
	env["GRUS_CRDS"] = "true"
	if _, err := loadConfig(nil, fakeEnv(env)); err == nil {
		t.Fatal("loadConfig accepted GRUS_CRDS without GRUS_NAMESPACE")
	}
}

func TestLoadConfigRejectsInvalidCRDsValue(t *testing.T) {
	env := validEnv()
	env["GRUS_CRDS"] = "maybe"
	if _, err := loadConfig(nil, fakeEnv(env)); err == nil {
		t.Fatal("loadConfig accepted GRUS_CRDS=maybe")
	}
}
