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

func s3Env() map[string]string {
	env := validEnv()
	env["GRUS_TEMPLATES_BACKEND"] = "s3"
	env["GRUS_S3_ENDPOINT"] = "minio.local:9000"
	env["GRUS_S3_BUCKET"] = "grus-templates-dev"
	env["GRUS_S3_PREFIX"] = "envs/dev"
	env["GRUS_S3_INSECURE"] = "true"
	env["AWS_ACCESS_KEY_ID"] = "access-key-id"
	env["AWS_SECRET_ACCESS_KEY"] = "secret-access-key-value"
	return env
}

func TestLoadConfigTemplatesDefaultToFS(t *testing.T) {
	cfg, err := loadConfig(nil, fakeEnv(validEnv()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.TemplatesBackend != "fs" {
		t.Errorf("TemplatesBackend = %q, want fs", cfg.TemplatesBackend)
	}
}

func TestLoadConfigReadsS3Settings(t *testing.T) {
	cfg, err := loadConfig(nil, fakeEnv(s3Env()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.TemplatesBackend != "s3" || cfg.S3Endpoint != "minio.local:9000" || cfg.S3Bucket != "grus-templates-dev" {
		t.Errorf("s3 settings = %q %q %q", cfg.TemplatesBackend, cfg.S3Endpoint, cfg.S3Bucket)
	}
	if cfg.S3Prefix != "envs/dev" || !cfg.S3Insecure {
		t.Errorf("S3Prefix = %q, S3Insecure = %v, want envs/dev and true", cfg.S3Prefix, cfg.S3Insecure)
	}
	if cfg.S3AccessKey != "access-key-id" || cfg.S3SecretKey != "secret-access-key-value" {
		t.Error("S3 credentials were not read from AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
	}
}

func TestLoadConfigRejectsInvalidTemplateBackends(t *testing.T) {
	cases := map[string]func(env map[string]string){
		"unknown backend":       func(env map[string]string) { env["GRUS_TEMPLATES_BACKEND"] = "gcs" },
		"s3 without endpoint":   func(env map[string]string) { delete(env, "GRUS_S3_ENDPOINT") },
		"s3 without bucket":     func(env map[string]string) { delete(env, "GRUS_S3_BUCKET") },
		"s3 without access key": func(env map[string]string) { delete(env, "AWS_ACCESS_KEY_ID") },
		"s3 without secret key": func(env map[string]string) { delete(env, "AWS_SECRET_ACCESS_KEY") },
		"invalid insecure flag": func(env map[string]string) { env["GRUS_S3_INSECURE"] = "sometimes" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			env := s3Env()
			mutate(env)
			if _, err := loadConfig(nil, fakeEnv(env)); err == nil {
				t.Fatalf("loadConfig accepted %s", name)
			}
		})
	}
}

func TestLoadConfigErrorsNeverContainS3Secret(t *testing.T) {
	env := s3Env()
	delete(env, "GRUS_S3_BUCKET")
	_, err := loadConfig(nil, fakeEnv(env))
	if err == nil {
		t.Fatal("loadConfig accepted a missing bucket")
	}
	if strings.Contains(err.Error(), "secret-access-key-value") {
		t.Fatalf("error leaks the secret key: %v", err)
	}
}
