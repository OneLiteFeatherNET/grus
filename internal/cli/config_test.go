package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginStoresTokenInPrivateFileAndNeverPrintsIt(t *testing.T) {
	h := newHarness(t, http.NotFoundHandler())
	out := h.ok("super-secret-token\n", "login", "--url", "https://grus.example/", "--name", "prod", "--token-stdin")
	if strings.Contains(out, "super-secret-token") {
		t.Fatalf("login printed the token: %q", out)
	}
	tokenFile := filepath.Join(h.cfgDir, "grus", "tokens", "prod")
	info, err := os.Stat(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode = %v, want 0600", info.Mode().Perm())
	}
	cfg, err := os.ReadFile(filepath.Join(h.cfgDir, "grus", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "super-secret-token") || !strings.Contains(string(cfg), "url: https://grus.example") {
		t.Errorf("config:\n%s", cfg)
	}
}

func TestContextSuppliesURLAndTokenFile(t *testing.T) {
	var gotAuth string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	}))
	url := h.vars["GRUS_URL"]
	delete(h.vars, "GRUS_URL")
	delete(h.vars, "GRUS_TOKEN")
	h.ok("filetoken\n", "login", "--url", url, "--token-stdin")
	h.ok("", "get", "pools")
	if gotAuth != "Bearer filetoken" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestEnvTokenBeatsTokenFile(t *testing.T) {
	var gotAuth string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	}))
	h.ok("filetoken", "login", "--url", h.vars["GRUS_URL"], "--token-stdin")
	h.vars["GRUS_TOKEN"] = "envtoken"
	h.ok("", "get", "pools")
	if gotAuth != "Bearer envtoken" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestNoURLGivesActionableError(t *testing.T) {
	h := newHarness(t, http.NotFoundHandler())
	delete(h.vars, "GRUS_URL")
	_, _, err := h.run("", "get", "pools")
	if err == nil || !strings.Contains(err.Error(), "grus login") {
		t.Errorf("err = %v", err)
	}
}

func TestConfigUseSwitchesAndRejectsUnknown(t *testing.T) {
	h := newHarness(t, http.NotFoundHandler())
	h.ok("", "login", "--url", "http://a", "--name", "a")
	h.ok("", "login", "--url", "http://b", "--name", "b")
	if got := strings.TrimSpace(h.ok("", "config", "current")); got != "b" {
		t.Errorf("current = %q", got)
	}
	h.ok("", "config", "use", "a")
	if got := strings.TrimSpace(h.ok("", "config", "current")); got != "a" {
		t.Errorf("current = %q", got)
	}
	if _, _, err := h.run("", "config", "use", "zzz"); err == nil {
		t.Error("unknown context must fail")
	}
	if out := h.ok("", "config", "list"); !strings.Contains(out, "http://b") {
		t.Errorf("list:\n%s", out)
	}
}

func TestPromptShowsContextName(t *testing.T) {
	if got := promptFor(connection{Context: "prod"}); got != "grus(prod)> " {
		t.Errorf("prompt = %q", got)
	}
	if got := promptFor(connection{}); got != "grus(-)> " {
		t.Errorf("prompt without context = %q", got)
	}
}

func TestShellTreeHasExitButNoNestedShell(t *testing.T) {
	env := &Env{Exit: func(int) {}}
	env.ensure()
	root := newRoot(env, opts{output: formatTable}, true)
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	if !names["exit"] || names["shell"] || !names["get"] {
		t.Errorf("commands = %v", names)
	}
}
