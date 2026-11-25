package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const privateInstanceJSON = `{"id":"home-abc12","pool":"home","state":"Starting","players":0,"labels":{"grus.olf/owner":"alex"}}`

func TestPrivateCreateStartsAServerForTheOwner(t *testing.T) {
	var got map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/private-servers", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, privateInstanceJSON)
	})
	h := newHarness(t, mux)

	out := h.ok("", "private", "create", "home", "--owner", "alex")

	if got["pool"] != "home" || got["owner"] != "alex" {
		t.Errorf("request body = %v", got)
	}
	if !strings.Contains(out, "home-abc12 started") {
		t.Errorf("output = %q, want the instance reported as started", out)
	}
}

func TestPrivateCreateReportsAnExistingServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/private-servers", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, privateInstanceJSON) })
	h := newHarness(t, mux)

	if out := h.ok("", "private", "create", "home", "--owner", "alex"); !strings.Contains(out, "already running") {
		t.Errorf("output = %q, want already running", out)
	}
}

func TestPrivateCreateNeedsAnOwner(t *testing.T) {
	h := newHarness(t, http.NewServeMux())

	if _, _, err := h.run("", "private", "create", "home"); err == nil {
		t.Fatal("create without --owner succeeded")
	}
}

func TestPrivateListPassesTheOwnerFilter(t *testing.T) {
	var q string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/private-servers", func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.RawQuery
		io.WriteString(w, "["+privateInstanceJSON+"]")
	})
	h := newHarness(t, mux)

	out := h.ok("", "private", "list", "--owner", "alex")

	if q != "owner=alex" {
		t.Errorf("query = %q", q)
	}
	if !strings.Contains(out, "home-abc12") || !strings.Contains(out, "alex") {
		t.Errorf("table lacks the server and its owner:\n%s", out)
	}
}
