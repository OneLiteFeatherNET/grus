package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// harness runs commands in process against an httptest server with a private config dir.
type harness struct {
	t      *testing.T
	env    *Env
	vars   map[string]string
	clock  *fakeClock
	cfgDir string
}

func newHarness(t *testing.T, h http.Handler) *harness {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	hn := &harness{t: t, vars: map[string]string{"GRUS_URL": srv.URL, "GRUS_TOKEN": "tok"}, cfgDir: t.TempDir(),
		clock: &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
	hn.env = &Env{
		Getenv:     func(k string) string { return hn.vars[k] },
		ConfigDir:  func() (string, error) { return hn.cfgDir, nil },
		HTTPClient: srv.Client(),
		Clock:      hn.clock,
		Exit:       func(int) {},
	}
	return hn
}

func (h *harness) run(stdin string, args ...string) (string, string, error) {
	h.t.Helper()
	root := NewRoot(h.env)
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errb.String(), err
}

func (h *harness) ok(stdin string, args ...string) string {
	h.t.Helper()
	out, _, err := h.run(stdin, args...)
	if err != nil {
		h.t.Fatalf("grus %s: %v", strings.Join(args, " "), err)
	}
	return out
}

const poolsJSON = `[{"name":"lobby","spec":{"role":"game","mode":"static","scaling":{"min":1,"max":4}},"generation":3,"maintenance":false},
{"name":"proxy","spec":{"role":"proxy","mode":"static","scaling":{"min":2,"max":2}},"generation":1,"maintenance":false}]`

const instancesJSON = `[{"id":"lobby-abc","pool":"lobby","state":"Ready","players":3,"maxPlayers":20,"host":"k8s","address":"10.0.0.1:25565"}]`

func apiMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/pools", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, poolsJSON) })
	mux.HandleFunc("GET /v1/instances", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pool") == "none" {
			io.WriteString(w, `[]`)
			return
		}
		io.WriteString(w, instancesJSON)
	})
	mux.HandleFunc("GET /v1/templates", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"paper":{"stable":"sha256:0123456789abcdef0123"}}`)
	})
	return mux
}

func TestGetPoolsTable(t *testing.T) {
	h := newHarness(t, apiMux(t))
	out := h.ok("", "get", "pools")
	for _, want := range []string{"NAME", "lobby", "proxy", "GENERATION"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestGetPoolsJSONIsAnArray(t *testing.T) {
	h := newHarness(t, apiMux(t))
	var got []map[string]any
	if err := json.Unmarshal([]byte(h.ok("", "get", "pools", "-o", "json")), &got); err != nil || len(got) != 2 {
		t.Fatalf("json output: %v (%d items)", err, len(got))
	}
}

func TestGetPoolsYAML(t *testing.T) {
	h := newHarness(t, apiMux(t))
	if out := h.ok("", "get", "pools", "-o", "yaml"); !strings.Contains(out, "  name: lobby") {
		t.Errorf("yaml output:\n%s", out)
	}
}

func TestGetInstancesPassesFilters(t *testing.T) {
	var q string
	mux := apiMux(t)
	mux.HandleFunc("GET /v1/instances/{id}", func(w http.ResponseWriter, r *http.Request) {})
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.RawQuery
		mux.ServeHTTP(w, r)
	}))
	out := h.ok("", "get", "instances", "--pool", "lobby", "--state", "Ready")
	if !strings.Contains(q, "pool=lobby") || !strings.Contains(q, "state=Ready") {
		t.Errorf("query = %q", q)
	}
	if !strings.Contains(out, "3/20") {
		t.Errorf("players column missing:\n%s", out)
	}
}

func TestGetTemplatesShortensDigests(t *testing.T) {
	h := newHarness(t, apiMux(t))
	out := h.ok("", "get", "templates")
	if !strings.Contains(out, "stable=sha256:0123456789ab") || strings.Contains(out, "cdef0123") {
		t.Errorf("templates:\n%s", out)
	}
}

func TestGetUnknownResourceListsChoices(t *testing.T) {
	h := newHarness(t, apiMux(t))
	_, _, err := h.run("", "get", "widgets")
	if err == nil || !strings.Contains(err.Error(), "pools, instances") {
		t.Errorf("err = %v", err)
	}
}

func TestGetGroupsOnOldControllerSaysNotSupported(t *testing.T) {
	h := newHarness(t, apiMux(t))
	_, _, err := h.run("", "get", "groups")
	if err == nil || !strings.Contains(err.Error(), "not supported by this controller") {
		t.Errorf("err = %v", err)
	}
}

func TestGetHostsOnNewerController(t *testing.T) {
	mux := apiMux(t)
	mux.HandleFunc("GET /v1/hosts", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"name":"mc01","version":"2.0.0","capacity":{"memoryMiB":16384},"addresses":["10.1.1.1"]}]`)
	})
	h := newHarness(t, mux)
	out := h.ok("", "get", "hosts")
	if !strings.Contains(out, "mc01") || !strings.Contains(out, "16384") {
		t.Errorf("hosts:\n%s", out)
	}
}

func TestDescribePoolShowsSpecAndInstances(t *testing.T) {
	mux := apiMux(t)
	mux.HandleFunc("GET /v1/pools/lobby", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"name":"lobby","spec":{"role":"game"},"generation":3}`)
	})
	h := newHarness(t, mux)
	out := h.ok("", "describe", "pool", "lobby")
	if !strings.Contains(out, "role: game") || !strings.Contains(out, "lobby-abc") {
		t.Errorf("describe:\n%s", out)
	}
}

func TestApplyOrdersKindsAndPutsSpec(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(b))
		mu.Unlock()
		io.WriteString(w, `{}`)
	}))
	manifest := `apiVersion: grus.onelitefeather.net/v1alpha1
kind: Pool
metadata:
  name: lobby
spec:
  role: game
  scaling: {min: 1, max: 2}
---
apiVersion: grus.onelitefeather.net/v1alpha1
kind: Environment
metadata:
  name: paper
spec:
  kind: jvm
`
	out := h.ok(manifest, "apply", "-f", "-")
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "PUT /v1/environments/paper ") || !strings.HasPrefix(calls[1], "PUT /v1/pools/lobby ") {
		t.Fatalf("calls = %q", calls)
	}
	if !strings.Contains(calls[1], `"scaling":{"max":2,"min":1}`) {
		t.Errorf("pool body = %s", calls[1])
	}
	if !strings.Contains(out, "environment/paper applied") || !strings.Contains(out, "pool/lobby applied") {
		t.Errorf("out = %q", out)
	}
}

func TestApplyAcceptsJSONFile(t *testing.T) {
	var path string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { path = r.URL.Path; io.WriteString(w, `{}`) }))
	f := filepath.Join(t.TempDir(), "g.json")
	if err := os.WriteFile(f, []byte(`{"apiVersion":"grus.onelitefeather.net/v1alpha1","kind":"Group","metadata":{"name":"g"},"spec":{"templates":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.ok("", "apply", "-f", f)
	if path != "/v1/groups/g" {
		t.Errorf("path = %q", path)
	}
}

func TestApplyRejectsBadManifests(t *testing.T) {
	h := newHarness(t, http.NotFoundHandler())
	cases := map[string]string{
		"wrong apiVersion": "apiVersion: v1\nkind: Pool\nmetadata: {name: a}\nspec: {}\n",
		"unknown kind":     "apiVersion: grus.onelitefeather.net/v1alpha1\nkind: Server\nmetadata: {name: a}\nspec: {}\n",
		"no name":          "apiVersion: grus.onelitefeather.net/v1alpha1\nkind: Pool\nmetadata: {}\nspec: {}\n",
		"no spec":          "apiVersion: grus.onelitefeather.net/v1alpha1\nkind: Pool\nmetadata: {name: a}\n",
	}
	for name, doc := range cases {
		if _, _, err := h.run(doc, "apply", "-f", "-"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestApplyDryRunDoesNotCallController(t *testing.T) {
	called := false
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	out := h.ok("apiVersion: grus.onelitefeather.net/v1alpha1\nkind: Pool\nmetadata: {name: a}\nspec: {role: game}\n",
		"apply", "-f", "-", "--dry-run")
	if called || !strings.Contains(out, "valid (dry run)") {
		t.Errorf("called=%v out=%q", called, out)
	}
}

func TestScaleChangesOnlyMinAndKeepsTheRestOfTheSpec(t *testing.T) {
	var put string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/pools/lobby", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"name":"lobby","spec":{"role":"game","scaling":{"min":1,"max":4}}}`)
	})
	mux.HandleFunc("PUT /v1/pools/lobby", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		put = string(b)
		io.WriteString(w, `{}`)
	})
	h := newHarness(t, mux)
	out := h.ok("", "scale", "lobby", "--min", "3")
	if put != `{"role":"game","scaling":{"max":4,"min":3}}` {
		t.Errorf("put body = %s", put)
	}
	if !strings.Contains(out, "min=3 max=4") {
		t.Errorf("out = %q", out)
	}
	if _, _, err := h.run("", "scale", "lobby"); err == nil {
		t.Error("scale without flags must fail")
	}
}

func TestStopSendsDrain(t *testing.T) {
	var body string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		io.WriteString(w, `{"state":"Draining"}`)
	}))
	out := h.ok("", "stop", "lobby-abc", "--drain")
	if !strings.Contains(body, `"drain":true`) || !strings.Contains(out, "Draining") {
		t.Errorf("body=%s out=%q", body, out)
	}
	if _, _, err := h.run("", "stop", "x", "--drain", "--force"); err == nil {
		t.Error("drain and force together must fail")
	}
}

func TestExecJoinsTheConsoleLine(t *testing.T) {
	var body string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	h.ok("", "exec", "lobby-abc", "--", "say", "hello", "world")
	if body != `{"line":"say hello world"}` {
		t.Errorf("body = %s", body)
	}
}

func TestLogsTailAndFollow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/instances/i/logs", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "tail="+r.URL.Query().Get("tail")+"\n")
	})
	mux.HandleFunc("GET /v1/instances/i/logs/stream", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "data: one\n\ndata: two\n\n")
	})
	h := newHarness(t, mux)
	if out := h.ok("", "logs", "i", "--tail", "7"); out != "tail=7\n" {
		t.Errorf("tail out = %q", out)
	}
	if out := h.ok("", "logs", "i", "-f"); out != "one\ntwo\n" {
		t.Errorf("follow out = %q", out)
	}
}

func TestEventsFilterByType(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "id: 1\nevent: pool.updated\ndata: {\"a\":1}\n\nid: 2\nevent: instance.state\ndata: {\"b\":2}\n\n")
	}))
	out := h.ok("", "events", "--type", "instance.")
	if strings.Contains(out, "pool.updated") || !strings.Contains(out, "instance.state") {
		t.Errorf("out = %q", out)
	}
	out = h.ok("", "events", "-o", "json")
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 2 || !strings.Contains(lines[0], `"type":"pool.updated"`) {
		t.Errorf("json lines = %q", lines)
	}
}

func TestNetwork(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/network", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"players":42,"maxPlayers":500,"proxies":2}`)
	})
	h := newHarness(t, mux)
	if out := h.ok("", "network"); !strings.Contains(out, "42") || !strings.Contains(out, "PROXIES") {
		t.Errorf("out = %q", out)
	}
	old := newHarness(t, http.NotFoundHandler())
	if _, _, err := old.run("", "network"); err == nil || !strings.Contains(err.Error(), "not supported by this controller") {
		t.Errorf("err = %v", err)
	}
}

func TestInvalidOutputFormatIsRejected(t *testing.T) {
	h := newHarness(t, apiMux(t))
	if _, _, err := h.run("", "get", "pools", "-o", "xml"); err == nil {
		t.Error("want an error for -o xml")
	}
}

func TestAPIErrorIsReportedWithStatus(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"title":"Unauthorized"}`)
	}))
	_, _, err := h.run("", "get", "pools")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v", err)
	}
}
