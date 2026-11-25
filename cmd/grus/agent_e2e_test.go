package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/agent"
	processdriver "github.com/OneLiteFeatherNET/grus/internal/driver/process"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

const e2eAgentToken = "e2e-agent-token-0123456789"

// TestAgentRunsAnInstanceOfAPlacedPoolFromStartToDrain runs the controller and a grus-agent in one process
// on loopback. A pool placed on the agent host must be started by the agent (the test binary acts as the
// game server), report ready through the self API, become Ready in the controller and be stopped again.
//
// Like the other end-to-end test it polls with a short interval under a hard deadline instead of sleeping:
// the agent, the controller loop and a child process run concurrently, so there is no clock to inject.
func TestAgentRunsAnInstanceOfAPlacedPoolFromStartToDrain(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	baseURL := "http://" + ln.Addr().String()
	sum := sha256.Sum256([]byte(e2eAgentToken))
	cfg := config{
		DB:                filepath.Join(dir, "grus.db"),
		TemplatesDir:      filepath.Join(dir, "templates"),
		Driver:            "process",
		HostID:            "local",
		ProcessDir:        filepath.Join(dir, "controller-instances"),
		PublicURL:         "http://cluster.invalid:8080", // the agent must replace it with the URL it uses
		AdminToken:        e2eAdminToken,
		MaxInstances:      10,
		ReconcileInterval: pollInterval,
		AgentTokens:       map[string]string{"mc01": hex.EncodeToString(sum[:])},
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
	ctrlDone := make(chan struct{})
	go func() { _ = s.ctrl.Run(ctx); close(ctrlDone) }()

	pl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a port: %v", err)
	}
	port := pl.Addr().(*net.TCPAddr).Port
	_ = pl.Close()
	procs := processdriver.New(processdriver.Config{
		HostID: "mc01", BaseDir: filepath.Join(dir, "agent-instances"), Logger: logger,
		PortMin: port, PortMax: port, AdvertiseAddress: "127.0.0.1",
	})
	acfg := agent.Config{URL: baseURL, Token: e2eAgentToken, Host: "mc01", Dir: filepath.Join(dir, "agent-instances"),
		Address: "127.0.0.1", PortMin: port, PortMax: port, ReportInterval: pollInterval}
	a := agent.New(ctx, acfg, agent.NewClient(baseURL, "mc01", e2eAgentToken, http.DefaultClient), procs, logger)
	agentDone := make(chan struct{})
	go func() { a.Run(ctx); close(agentDone) }()

	t.Cleanup(func() {
		if obs, err := procs.List(context.Background()); err == nil {
			for _, o := range obs {
				_ = procs.Kill(context.Background(), o.Ref)
			}
		}
		cancel()
		<-agentDone
		<-ctrlDone
		_ = srv.Close()
		_ = s.store.Close()
	})

	client := &http.Client{}
	putJSON(t, client, baseURL, "/v1/pools/mc01-stub", model.PoolSpec{
		Role:      model.RoleGame,
		Mode:      model.ModeDynamic,
		Placement: model.Placement{Driver: model.DriverAgent, Host: "mc01"},
		Command:   []string{os.Args[0], "-test.run=TestHelperProcess"},
		Env:       map[string]string{helperEnv: "1"},
		Resources: model.Resources{MemoryMiB: 128},
		Scaling:   model.Scaling{Min: 1, Max: 1, Capacity: 10},
		Lifecycle: model.Lifecycle{GracePeriod: model.Duration(100 * time.Millisecond)},
	})

	var inst model.Instance
	waitFor(t, "instance Ready on mc01", func() bool {
		var list []model.Instance
		getJSON(t, client, baseURL, "/v1/instances", &list)
		if len(list) == 1 && list[0].State == model.StateReady {
			inst = list[0]
			return true
		}
		return false
	})
	if inst.Host != "mc01" {
		t.Errorf("instance host = %q, want mc01", inst.Host)
	}
	if want := "127.0.0.1:"; !strings.HasPrefix(inst.Address, want) {
		t.Errorf("instance address = %q, want it to start with %s", inst.Address, want)
	}
	var hosts []map[string]any
	getJSON(t, client, baseURL, "/v1/hosts", &hosts)
	if len(hosts) != 1 || hosts[0]["online"] != true {
		t.Errorf("hosts = %v, want mc01 online", hosts)
	}

	postJSON(t, client, baseURL, "/v1/instances/"+inst.ID+"/stop", map[string]any{})
	waitFor(t, "stopped instance to disappear", func() bool {
		var list []model.Instance
		getJSON(t, client, baseURL, "/v1/instances", &list)
		for _, i := range list {
			if i.ID == inst.ID && i.State != model.StateStopped {
				return false
			}
		}
		return true
	})
}

// waitFor polls cond every pollInterval until it holds or e2eTimeout passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.NewTimer(e2eTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for !cond() {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("timed out after %s waiting for %s", e2eTimeout, what)
		}
	}
}

func postJSON(t *testing.T, client *http.Client, baseURL, path string, body any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("build POST %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+e2eAdminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: HTTP %d: %s", path, resp.StatusCode, msg)
	}
}
