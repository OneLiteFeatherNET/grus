package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// TestMain turns the test binary into a tiny fake game server when GRUS_TEST_HELPER=1, so the driver
// is tested against a real child process without any external program.
func TestMain(m *testing.M) {
	if os.Getenv("GRUS_TEST_HELPER") == "1" {
		runHelperServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runHelperServer prints "started on <port>", echoes console lines, answers "env NAME" with the value
// of that variable and exits 0 on "stop". With GRUS_TEST_IGNORE_TERM=1 it ignores SIGTERM.
func runHelperServer() {
	if os.Getenv("GRUS_TEST_IGNORE_TERM") == "1" {
		signal.Ignore(syscall.SIGTERM)
	}
	fmt.Printf("started on %s\n", os.Getenv("GRUS_PORT"))
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "stop":
			return
		case strings.HasPrefix(line, "env "):
			fmt.Printf("env %s\n", os.Getenv(strings.TrimPrefix(line, "env ")))
		default:
			fmt.Printf("cmd: %s\n", line)
		}
	}
}

// harness owns one driver with a temp base dir. Timers are injected: every stop timer request is
// queued on timers and fires only when the test sends on the request's channel.
type harness struct {
	t      *testing.T
	ctx    context.Context
	d      *Driver
	base   string
	timers chan timerRequest
}

type timerRequest struct {
	d    time.Duration
	fire chan time.Time
}

func newHarness(t *testing.T, fetch func(context.Context, []driver.ResolvedTemplate, string) error) *harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	base := t.TempDir()
	d := New(Config{
		HostID:         "local",
		BaseDir:        base,
		FetchTemplates: fetch,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	h := &harness{t: t, ctx: ctx, d: d, base: base, timers: make(chan timerRequest, 8)}
	d.after = func(dur time.Duration) <-chan time.Time {
		fire := make(chan time.Time, 1)
		h.timers <- timerRequest{d: dur, fire: fire}
		return fire
	}
	d.termWait = 9 * time.Second

	t.Cleanup(func() {
		list, _ := d.List(context.Background())
		for _, o := range list {
			if o.Phase == driver.PhaseRunning {
				_ = d.Kill(context.Background(), o.Ref)
			}
		}
	})
	return h
}

func (h *harness) spec(id string, env map[string]string) driver.StartSpec {
	merged := map[string]string{"GRUS_TEST_HELPER": "1"}
	for k, v := range env {
		merged[k] = v
	}
	return driver.StartSpec{
		Instance: model.Instance{ID: id, Pool: "lobby", Role: model.RoleGame, PoolGeneration: 1, TokenHash: "hash"},
		Pool: model.Pool{Name: "lobby", Spec: model.PoolSpec{
			Role:    model.RoleGame,
			Mode:    model.ModeDynamic,
			Driver:  "process",
			Command: []string{os.Args[0]},
		}},
		Token: "tok-123",
		Env:   merged,
	}
}

// waitPhase blocks until the instance reaches want. It wakes on the driver's change signal, not on a
// timer, and fails when the harness context ends.
func (h *harness) waitPhase(ref string, want driver.Phase) driver.Observed {
	h.t.Helper()
	for {
		h.d.mu.Lock()
		p := h.d.procs[ref]
		var changed chan struct{}
		if p != nil {
			if p.obs.Phase == want {
				o := p.obs
				h.d.mu.Unlock()
				return o
			}
			changed = p.changed
		}
		h.d.mu.Unlock()

		select {
		case <-h.ctx.Done():
			h.t.Fatalf("timed out waiting for instance %q to reach phase %s", ref, want)
		case <-changed:
		}
	}
}

func (h *harness) nextTimer() timerRequest {
	h.t.Helper()
	select {
	case req := <-h.timers:
		return req
	case <-h.ctx.Done():
		h.t.Fatal("timed out waiting for a stop timer request")
		return timerRequest{}
	}
}

// lineWaiter reads a log stream in one goroutine and matches lines in order.
type lineWaiter struct {
	h     *harness
	rc    io.ReadCloser
	lines chan string
}

func (h *harness) follow(ref string) *lineWaiter {
	h.t.Helper()
	rc, err := h.d.Logs(h.ctx, ref, 0, true)
	if err != nil {
		h.t.Fatalf("Logs(%q): %v", ref, err)
	}
	h.t.Cleanup(func() { rc.Close() })
	w := &lineWaiter{h: h, rc: rc, lines: make(chan string)}
	go func() {
		defer close(w.lines)
		sc := bufio.NewScanner(rc)
		for sc.Scan() {
			select {
			case w.lines <- sc.Text():
			case <-h.ctx.Done():
				return
			}
		}
	}()
	return w
}

// wait consumes lines until one equals want.
func (w *lineWaiter) wait(want string) {
	w.h.t.Helper()
	for {
		select {
		case line, ok := <-w.lines:
			if !ok {
				w.h.t.Fatalf("log stream ended before line %q", want)
			}
			if line == want {
				return
			}
		case <-w.h.ctx.Done():
			w.h.t.Fatalf("timed out waiting for log line %q", want)
		}
	}
}

func portOf(t *testing.T, address string) string {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("address %q is not host:port: %v", address, err)
	}
	if host != "127.0.0.1" {
		t.Errorf("host = %q, want 127.0.0.1", host)
	}
	return port
}

func TestStartReportsRunningAddressAndWritesOutput(t *testing.T) {
	h := newHarness(t, nil)
	ref, err := h.d.Start(h.ctx, h.spec("inst-1", nil))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if ref != "inst-1" {
		t.Errorf("ref = %q, want inst-1", ref)
	}

	o := h.waitPhase(ref, driver.PhaseRunning)
	port := portOf(t, o.Address)
	h.follow(ref).wait("started on " + port)

	console, err := os.ReadFile(filepath.Join(h.base, "inst-1", "console.log"))
	if err != nil {
		t.Fatalf("console.log missing: %v", err)
	}
	if !strings.Contains(string(console), "started on "+port) {
		t.Errorf("console.log does not contain the startup line: %q", console)
	}
}

func TestCommandIsEchoedInLogs(t *testing.T) {
	h := newHarness(t, nil)
	ref, _ := h.d.Start(h.ctx, h.spec("inst-1", nil))
	h.waitPhase(ref, driver.PhaseRunning)
	w := h.follow(ref)

	if err := h.d.Command(h.ctx, ref, "say hi"); err != nil {
		t.Fatalf("Command: %v", err)
	}

	w.wait("cmd: say hi")
}

func TestCommandRejectsLineBreaks(t *testing.T) {
	h := newHarness(t, nil)
	ref, _ := h.d.Start(h.ctx, h.spec("inst-1", nil))
	h.waitPhase(ref, driver.PhaseRunning)

	if err := h.d.Command(h.ctx, ref, "one\ntwo"); err == nil {
		t.Error("Command with a line break returned nil, want an error")
	}
}

func TestStopWithStopCommandLetsProcessExit(t *testing.T) {
	h := newHarness(t, nil)
	ref, _ := h.d.Start(h.ctx, h.spec("inst-1", nil))
	h.waitPhase(ref, driver.PhaseRunning)

	if err := h.d.Stop(h.ctx, ref, "stop", 30*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	o := h.waitPhase(ref, driver.PhaseExited)
	if o.Reason != "exit code 0" {
		t.Errorf("reason = %q, want \"exit code 0\"", o.Reason)
	}
}

func TestStopEscalatesToSigtermWhenStopCommandIsIgnored(t *testing.T) {
	h := newHarness(t, nil)
	ref, _ := h.d.Start(h.ctx, h.spec("inst-1", nil))
	h.waitPhase(ref, driver.PhaseRunning)

	grace := 7 * time.Second
	if err := h.d.Stop(h.ctx, ref, "ignore", grace); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if req := h.nextTimer(); req.d != grace {
		t.Fatalf("first timer = %s, want the grace period %s", req.d, grace)
	} else {
		req.fire <- time.Time{}
	}

	o := h.waitPhase(ref, driver.PhaseExited)
	if o.Reason != "signal: terminated" {
		t.Errorf("reason = %q, want \"signal: terminated\" (SIGTERM without SIGKILL)", o.Reason)
	}
}

func TestStopEscalatesToSigkillWhenSigtermIsIgnored(t *testing.T) {
	h := newHarness(t, nil)
	ref, _ := h.d.Start(h.ctx, h.spec("inst-1", map[string]string{"GRUS_TEST_IGNORE_TERM": "1"}))
	o := h.waitPhase(ref, driver.PhaseRunning)
	// The helper prints its start line only after it ignores SIGTERM, so waiting for it is required.
	h.follow(ref).wait("started on " + portOf(t, o.Address))

	grace := 7 * time.Second
	if err := h.d.Stop(h.ctx, ref, "ignore", grace); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if req := h.nextTimer(); req.d != grace {
		t.Fatalf("first timer = %s, want the grace period %s", req.d, grace)
	} else {
		req.fire <- time.Time{}
	}
	if req := h.nextTimer(); req.d != h.d.termWait {
		t.Fatalf("second timer = %s, want the SIGTERM wait %s", req.d, h.d.termWait)
	} else {
		req.fire <- time.Time{}
	}

	exited := h.waitPhase(ref, driver.PhaseExited)
	if exited.Reason != "signal: killed" {
		t.Errorf("reason = %q, want \"signal: killed\" after SIGTERM was ignored", exited.Reason)
	}
}

func TestKillTerminatesProcessBeforeReturning(t *testing.T) {
	h := newHarness(t, nil)
	ref, _ := h.d.Start(h.ctx, h.spec("inst-1", nil))
	h.waitPhase(ref, driver.PhaseRunning)

	if err := h.d.Kill(h.ctx, ref); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	list, _ := h.d.List(h.ctx)
	if len(list) != 1 || list[0].Phase != driver.PhaseExited || list[0].Reason != "signal: killed" {
		t.Errorf("after Kill returned, List = %+v, want one Exited instance with reason \"signal: killed\"", list)
	}
}

func TestFetchTemplatesReceivesInstanceWorkdir(t *testing.T) {
	var gotDir string
	var gotRefs []driver.ResolvedTemplate
	fetch := func(_ context.Context, refs []driver.ResolvedTemplate, dir string) error {
		gotDir = dir
		gotRefs = refs
		return nil
	}
	h := newHarness(t, fetch)
	spec := h.spec("inst-1", nil)
	spec.Templates = []driver.ResolvedTemplate{{Name: "base", Digest: "sha256:abc"}}

	ref, err := h.d.Start(h.ctx, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.waitPhase(ref, driver.PhaseRunning)

	if want := filepath.Join(h.base, "inst-1"); gotDir != want {
		t.Errorf("FetchTemplates dir = %q, want %q", gotDir, want)
	}
	if len(gotRefs) != 1 || gotRefs[0].Digest != "sha256:abc" {
		t.Errorf("FetchTemplates refs = %+v, want the spec's template", gotRefs)
	}
}

func TestStartFailsAndForgetsInstanceWhenFetchTemplatesFails(t *testing.T) {
	fetch := func(context.Context, []driver.ResolvedTemplate, string) error {
		return errors.New("registry down")
	}
	h := newHarness(t, fetch)
	spec := h.spec("inst-1", nil)
	spec.Templates = []driver.ResolvedTemplate{{Name: "base", Digest: "sha256:abc"}}

	if _, err := h.d.Start(h.ctx, spec); err == nil {
		t.Fatal("Start returned nil error although the template fetch failed")
	}
	if list, _ := h.d.List(h.ctx); len(list) != 0 {
		t.Errorf("List after failed start has %d entries, want 0", len(list))
	}
}

func TestEnvironmentContainsTokenAndPortVariables(t *testing.T) {
	h := newHarness(t, nil)
	ref, _ := h.d.Start(h.ctx, h.spec("inst-1", nil))
	o := h.waitPhase(ref, driver.PhaseRunning)
	port := portOf(t, o.Address)
	w := h.follow(ref)

	if err := h.d.Command(h.ctx, ref, "env GRUS_TOKEN"); err != nil {
		t.Fatalf("Command: %v", err)
	}
	w.wait("env tok-123")

	for _, name := range []string{"GRUS_PORT", "SERVER_PORT"} {
		if err := h.d.Command(h.ctx, ref, "env "+name); err != nil {
			t.Fatalf("Command: %v", err)
		}
		w.wait("env " + port)
	}
}

func TestStartRejectsIDOfRunningInstance(t *testing.T) {
	h := newHarness(t, nil)
	ref, _ := h.d.Start(h.ctx, h.spec("inst-1", nil))
	h.waitPhase(ref, driver.PhaseRunning)

	if _, err := h.d.Start(h.ctx, h.spec("inst-1", nil)); err == nil {
		t.Error("second Start of a running id returned nil error, want an error")
	}
}

func TestStartRejectsIDThatEscapesBaseDir(t *testing.T) {
	h := newHarness(t, nil)

	if _, err := h.d.Start(h.ctx, h.spec("../escape", nil)); err == nil {
		t.Error("Start with an id containing a path element returned nil error, want an error")
	}
}

func TestForgetRefusesRunningInstance(t *testing.T) {
	h := newHarness(t, nil)
	ref, _ := h.d.Start(h.ctx, h.spec("inst-1", nil))
	h.waitPhase(ref, driver.PhaseRunning)

	if err := h.d.Forget(ref); err == nil {
		t.Error("Forget of a running instance returned nil error, want an error")
	}
}
