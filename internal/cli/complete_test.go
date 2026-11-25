package cli

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// completionHarness counts the list calls the API receives.
func completionHarness(t *testing.T) (*harness, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := apiMux(t)
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/pools" {
			calls.Add(1)
		}
		mux.ServeHTTP(w, r)
	}))
	return h, &calls
}

func complete(h *harness, args ...string) []string {
	h.t.Helper()
	out := h.ok("", append([]string{"__complete"}, args...)...)
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" && !strings.HasPrefix(l, ":") {
			got = append(got, l)
		}
	}
	return got
}

func TestCompleteKindsThenPoolNames(t *testing.T) {
	h, _ := completionHarness(t)
	if got := complete(h, "get", "po"); strings.Join(got, ",") != "pools" {
		t.Errorf("kinds = %v", got)
	}
	if got := complete(h, "get", "pools", "l"); strings.Join(got, ",") != "lobby" {
		t.Errorf("pool names = %v", got)
	}
	if got := complete(h, "describe", "pool", ""); strings.Join(got, ",") != "lobby,proxy" {
		t.Errorf("describe names = %v", got)
	}
}

func TestCompleteInstancesForLogsStopExec(t *testing.T) {
	h, _ := completionHarness(t)
	for _, c := range []string{"logs", "stop", "exec"} {
		if got := complete(h, c, ""); strings.Join(got, ",") != "lobby-abc" {
			t.Errorf("%s: %v", c, got)
		}
	}
	if got := complete(h, "logs", "lobby-abc", ""); len(got) != 0 {
		t.Errorf("second argument must not complete, got %v", got)
	}
}

func TestCompletePoolFlagAndScale(t *testing.T) {
	h, _ := completionHarness(t)
	if got := complete(h, "get", "instances", "--pool", "p"); strings.Join(got, ",") != "proxy" {
		t.Errorf("--pool = %v", got)
	}
	if got := complete(h, "scale", ""); strings.Join(got, ",") != "lobby,proxy" {
		t.Errorf("scale = %v", got)
	}
}

func TestCompletionCacheExpiresWithTheInjectedClock(t *testing.T) {
	h, calls := completionHarness(t)
	complete(h, "scale", "")
	complete(h, "scale", "l")
	if calls.Load() != 1 {
		t.Fatalf("API calls within the TTL = %d, want 1", calls.Load())
	}
	h.clock.advance(completionTTL + time.Second)
	complete(h, "scale", "")
	if calls.Load() != 2 {
		t.Errorf("API calls after the TTL = %d, want 2", calls.Load())
	}
}

func TestCompletionFailureYieldsNoCandidates(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{}`)
	}))
	if got := complete(h, "scale", ""); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestCompleteOutputFormatsAndContexts(t *testing.T) {
	h, _ := completionHarness(t)
	h.ok("", "login", "--url", "http://x", "--name", "prod")
	if got := complete(h, "get", "pools", "-o", ""); strings.Join(got, ",") != "table,json,yaml" {
		t.Errorf("formats = %v", got)
	}
	if got := complete(h, "get", "pools", "--context", "p"); strings.Join(got, ",") != "prod" {
		t.Errorf("contexts = %v", got)
	}
}

func TestShellCompletionScriptsExist(t *testing.T) {
	h, _ := completionHarness(t)
	for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
		if out := h.ok("", "completion", sh); out == "" {
			t.Errorf("completion %s printed nothing", sh)
		}
	}
}
