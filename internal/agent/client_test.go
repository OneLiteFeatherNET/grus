package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver/remote"
)

func testClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{base: srv.URL, host: "mc01", token: "agent-secret", http: srv.Client()}
}

func TestClientSendsTheTokenAndTheRequestShapes(t *testing.T) {
	var gotAuth, gotPath, gotQuery string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotQuery = r.Header.Get("Authorization"), r.URL.Path, r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(remote.Desired{Revision: 4, Instances: []remote.DesiredInstance{{ID: "a"}}})
	}))

	d, err := c.Desired(context.Background(), 3, 25*time.Second)
	if err != nil {
		t.Fatalf("Desired: %v", err)
	}
	if gotAuth != "Bearer agent-secret" || gotPath != "/v1/agents/mc01/desired" {
		t.Errorf("request = %q %q", gotAuth, gotPath)
	}
	if !strings.Contains(gotQuery, "revision=3") || !strings.Contains(gotQuery, "wait=25s") {
		t.Errorf("query = %q", gotQuery)
	}
	if d.Revision != 4 || len(d.Instances) != 1 {
		t.Errorf("desired = %+v", d)
	}
}

func TestClientErrorsNeverContainTheToken(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	err := c.Register(context.Background(), remote.RegisterRequest{})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("Register err = %v, want an HTTP 403 error", err)
	}
	if strings.Contains(err.Error(), "agent-secret") {
		t.Errorf("error %q leaks the token", err)
	}
}

func TestClientPutsTheObservedReport(t *testing.T) {
	var got remote.ObservedReport
	var method string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	rep := remote.ObservedReport{Revision: 2, Instances: []remote.ObservedInstance{{ID: "a", Phase: "Running"}}}
	if err := c.Observed(context.Background(), rep); err != nil {
		t.Fatalf("Observed: %v", err)
	}
	if method != http.MethodPut || got.Revision != 2 || got.Instances[0].ID != "a" {
		t.Errorf("method %s body %+v", method, got)
	}
}
