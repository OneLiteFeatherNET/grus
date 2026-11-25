package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type recorded struct {
	Method, Path, Auth, ContentType string
	Body                            []byte
}

// fakeController records every request and answers with the status code queued for that path,
// defaulting to 200.
type fakeController struct {
	mu       sync.Mutex
	reqs     []recorded
	readyQ   []int
	statusOK int
}

func (f *fakeController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, recorded{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
	code := http.StatusOK
	if r.URL.Path == "/v1/self/ready" && len(f.readyQ) > 0 {
		code = f.readyQ[0]
		f.readyQ = f.readyQ[1:]
	}
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{}`))
}

func (f *fakeController) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.reqs...)
}

func newTestReporter(t *testing.T, f *fakeController) *reporter {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	client := newSelfClient(srv.URL, "tok", srv.Client())
	return &reporter{client: client, maxPlayers: 20, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestTickPostsReadyThenStatusWithBearerToken(t *testing.T) {
	f := &fakeController{}
	rep := newTestReporter(t, f)

	if err := rep.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	reqs := f.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2 (ready, status)", len(reqs))
	}
	if reqs[0].Method != http.MethodPost || reqs[0].Path != "/v1/self/ready" {
		t.Errorf("first request = %s %s, want POST /v1/self/ready", reqs[0].Method, reqs[0].Path)
	}
	if reqs[1].Method != http.MethodPut || reqs[1].Path != "/v1/self/status" {
		t.Errorf("second request = %s %s, want PUT /v1/self/status", reqs[1].Method, reqs[1].Path)
	}
	for _, r := range reqs {
		if r.Auth != "Bearer tok" {
			t.Errorf("%s %s authorization = %q", r.Method, r.Path, r.Auth)
		}
	}
	var body struct {
		Players    int `json:"players"`
		MaxPlayers int `json:"maxPlayers"`
	}
	if err := json.Unmarshal(reqs[1].Body, &body); err != nil {
		t.Fatalf("status body %q: %v", reqs[1].Body, err)
	}
	if body.Players != 0 || body.MaxPlayers != 20 {
		t.Errorf("status body = %+v, want players 0 maxPlayers 20", body)
	}
	if reqs[1].ContentType != "application/json" {
		t.Errorf("status content type = %q", reqs[1].ContentType)
	}
}

func TestTickRetriesReadyUntilControllerAccepts(t *testing.T) {
	f := &fakeController{readyQ: []int{http.StatusServiceUnavailable}}
	rep := newTestReporter(t, f)

	if err := rep.Tick(context.Background()); err == nil {
		t.Fatal("tick succeeded although ready was refused")
	}
	for _, r := range f.requests() {
		if r.Path == "/v1/self/status" {
			t.Fatal("status sent before the instance was ready")
		}
	}

	if err := rep.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	paths := []string{}
	for _, r := range f.requests() {
		paths = append(paths, r.Path)
	}
	want := []string{"/v1/self/ready", "/v1/self/ready", "/v1/self/status"}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths = %v, want %v", paths, want)
		}
	}
}

func TestTickOnlyReadiesOnce(t *testing.T) {
	f := &fakeController{}
	rep := newTestReporter(t, f)
	for range 3 {
		if err := rep.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}
	readies := 0
	for _, r := range f.requests() {
		if r.Path == "/v1/self/ready" {
			readies++
		}
	}
	if readies != 1 {
		t.Errorf("ready posted %d times, want 1", readies)
	}
}

func TestShutdownPostsToSelfEndpoint(t *testing.T) {
	f := &fakeController{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	client := newSelfClient(srv.URL, "tok", srv.Client())

	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	reqs := f.requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != "/v1/self/shutdown" {
		t.Fatalf("requests = %+v, want one POST /v1/self/shutdown", reqs)
	}
}

func TestClientReportsNon2xxAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	client := newSelfClient(srv.URL, "tok", srv.Client())

	if err := client.Shutdown(context.Background()); err == nil {
		t.Fatal("403 treated as success")
	}
}
