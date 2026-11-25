package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCellsShowsBoundsAndOwner(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/pools/world/cells", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"cell":"r0","bounds":{"minX":0,"minZ":0,"maxX":31,"maxZ":31},"instance":"world-a","address":"10.0.0.1:25565"},
{"cell":"r1","bounds":{"minX":32,"minZ":0,"maxX":63,"maxZ":31}}]`)
	})
	h := newHarness(t, mux)

	out := h.ok("", "cells", "world")

	for _, want := range []string{"CELL", "r0", "x 0..31, z 0..31", "world-a", "10.0.0.1:25565", "r1"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestCellsOfAnUnshardedPoolFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/pools/lobby/cells", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"title":"Not Found","detail":"not found"}`)
	})
	h := newHarness(t, mux)

	if _, _, err := h.run("", "cells", "lobby"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want the controller's 404", err)
	}
}
