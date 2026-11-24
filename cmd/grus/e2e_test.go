package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// helperEnv switches TestHelperProcess from a no-op test into the fake instance server.
const helperEnv = "GO_WANT_HELPER"

const (
	e2eTimeout     = 20 * time.Second
	pollInterval   = 20 * time.Millisecond
	helperInterval = 200 * time.Millisecond
	e2eAdminToken  = "e2e-admin-token-0123456789"
)

// TestHelperProcess is the fake game server. The controller starts the test binary with
// -test.run=TestHelperProcess and GO_WANT_HELPER=1. It announces ready and then heartbeats until SIGTERM.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("helper process only, runs when started by the e2e test")
	}
	base := os.Getenv("GRUS_URL")
	token := os.Getenv("GRUS_TOKEN")
	if base == "" || token == "" {
		fmt.Fprintln(os.Stderr, "helper: GRUS_URL and GRUS_TOKEN are required")
		os.Exit(1)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	if err := helperCall(client, base, token, http.MethodPost, "/v1/self/ready", nil); err != nil {
		fmt.Fprintln(os.Stderr, "helper: ready:", err)
		os.Exit(1)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(stop)
	ticker := time.NewTicker(helperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			os.Exit(0)
		case <-ticker.C:
			status := map[string]int{"players": 0, "maxPlayers": 10}
			if err := helperCall(client, base, token, http.MethodPut, "/v1/self/status", status); err != nil {
				fmt.Fprintln(os.Stderr, "helper: status:", err)
			}
		}
	}
}

func helperCall(client *http.Client, base, token, method, path string, body any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(base, "/")+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return nil
}

// TestDynamicPoolReachesTwoReadyInstances runs the full controller stack in process on loopback: a
// dynamic pool with min 1 and buffer 1 must bring up two instances that become Ready, and the event
// stream must report both transitions.
func TestDynamicPoolReachesTwoReadyInstances(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	baseURL := "http://" + ln.Addr().String()
	cfg := config{
		DB:                filepath.Join(dir, "grus.db"),
		TemplatesDir:      filepath.Join(dir, "templates"),
		Driver:            "process",
		HostID:            "local",
		ProcessDir:        filepath.Join(dir, "instances"),
		PublicURL:         baseURL,
		AdminToken:        e2eAdminToken,
		MaxInstances:      10,
		ReconcileInterval: pollInterval,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	s, err := newStack(ctx, cfg, logger)
	if err != nil {
		cancel()
		t.Fatalf("newStack: %v", err)
	}
	srv := &http.Server{Handler: s.handler}
	go func() { _ = srv.Serve(ln) }()
	runDone := make(chan struct{})
	go func() {
		_ = s.ctrl.Run(ctx)
		close(runDone)
	}()
	t.Cleanup(func() {
		// Instances are not owned by the controller lifecycle; the test removes its own processes.
		if obs, err := s.drv.List(context.Background()); err == nil {
			for _, o := range obs {
				_ = s.drv.Kill(context.Background(), o.Ref)
			}
		}
		cancel()
		<-runDone
		_ = srv.Close()
		_ = s.store.Close()
	})

	client := &http.Client{}
	events := subscribeEvents(t, ctx, client, baseURL)

	spec := model.PoolSpec{
		Role:      model.RoleGame,
		Mode:      model.ModeDynamic,
		Driver:    "process",
		Command:   []string{os.Args[0], "-test.run=TestHelperProcess"},
		Env:       map[string]string{helperEnv: "1"},
		Resources: model.Resources{MemoryMiB: 128},
		Scaling:   model.Scaling{Min: 1, Max: 4, Buffer: 1, Capacity: 10},
	}
	putJSON(t, client, baseURL, "/v1/pools/lobby", spec)

	readyIDs := waitForReadyEvents(t, events, 2)
	if len(readyIDs) != 2 {
		t.Fatalf("instance.state Ready events for 2 instances: got %v", readyIDs)
	}

	instances := waitForInstances(t, client, baseURL, 2)
	for _, inst := range instances {
		if inst.State != model.StateReady {
			t.Errorf("instance %s state = %s, want Ready", inst.ID, inst.State)
		}
	}
}

// subscribeEvents opens the SSE stream and forwards every instance.state event payload.
func subscribeEvents(t *testing.T, ctx context.Context, client *http.Client, baseURL string) <-chan model.InstanceStateData {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/events", nil)
	if err != nil {
		t.Fatalf("build events request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e2eAdminToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("event stream: HTTP %d", resp.StatusCode)
	}
	out := make(chan model.InstanceStateData, 64)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var eventType string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				eventType = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && eventType == model.EventInstanceState:
				var data model.InstanceStateData
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data) == nil {
					select {
					case out <- data:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return out
}

// waitForReadyEvents collects the ids of instances that reported To == Ready until n distinct ones arrived.
func waitForReadyEvents(t *testing.T, events <-chan model.InstanceStateData, n int) []string {
	t.Helper()
	deadline := time.NewTimer(e2eTimeout)
	defer deadline.Stop()
	seen := map[string]bool{}
	for len(seen) < n {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("event stream closed after %d Ready events, want %d", len(seen), n)
			}
			if ev.To == model.StateReady {
				seen[ev.Instance] = true
			}
		case <-deadline.C:
			t.Fatalf("timed out after %s with %d Ready events, want %d", e2eTimeout, len(seen), n)
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids
}

// waitForInstances polls GET /v1/instances until n instances are listed.
func waitForInstances(t *testing.T, client *http.Client, baseURL string, n int) []model.Instance {
	t.Helper()
	deadline := time.NewTimer(e2eTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		var list []model.Instance
		getJSON(t, client, baseURL, "/v1/instances", &list)
		if len(list) >= n {
			return list
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("timed out with %d instances listed, want %d", len(list), n)
		}
	}
}

func putJSON(t *testing.T, client *http.Client, baseURL, path string, body any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	req, err := http.NewRequest(http.MethodPut, baseURL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build PUT %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+e2eAdminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT %s: HTTP %d: %s", path, resp.StatusCode, msg)
	}
}

func getJSON(t *testing.T, client *http.Client, baseURL, path string, out any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+e2eAdminToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode GET %s: %v", path, err)
	}
}
