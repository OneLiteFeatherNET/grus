package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/driver/remote"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// fakeRunner is an in-memory runner that records what the agent asks for.
type fakeRunner struct {
	mu       sync.Mutex
	procs    map[string]*driver.Observed
	started  []driver.StartSpec
	stopped  []string
	killed   []string
	forgot   []string
	commands []string
	logs     map[string][]string
	startErr error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{procs: map[string]*driver.Observed{}, logs: map[string][]string{}}
}

func (r *fakeRunner) Start(_ context.Context, spec driver.StartSpec) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.startErr != nil {
		return "", r.startErr
	}
	id := spec.Instance.ID
	r.started = append(r.started, spec)
	r.procs[id] = &driver.Observed{Ref: id, InstanceID: id, Pool: spec.Instance.Pool, Role: spec.Instance.Role,
		Mode: spec.Pool.Spec.Mode, TokenHash: spec.Instance.TokenHash, Phase: driver.PhaseRunning, Address: "10.0.0.1:30000"}
	return id, nil
}

func (r *fakeRunner) Stop(_ context.Context, ref, _ string, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = append(r.stopped, ref)
	return nil
}

func (r *fakeRunner) Kill(_ context.Context, ref string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.procs[ref]
	if !ok {
		return driver.ErrNotFound
	}
	p.Phase = driver.PhaseExited
	r.killed = append(r.killed, ref)
	return nil
}

func (r *fakeRunner) List(context.Context) ([]driver.Observed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []driver.Observed
	for _, p := range r.procs {
		out = append(out, *p)
	}
	return out, nil
}

func (r *fakeRunner) Logs(_ context.Context, ref string, _ int, _ bool) (io.ReadCloser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return io.NopCloser(strings.NewReader(strings.Join(r.logs[ref], "\n"))), nil
}

func (r *fakeRunner) Command(_ context.Context, ref, line string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, ref+": "+line)
	return nil
}

func (r *fakeRunner) Forget(ref string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.procs, ref)
	r.forgot = append(r.forgot, ref)
	return nil
}

func (r *fakeRunner) Pid(string) (int, bool) { return 0, false }

// fakeAPI is a controller whose answers the test sets.
type fakeAPI struct {
	desired  remote.Desired
	err      error
	reports  []remote.ObservedReport
	register []remote.RegisterRequest
}

func (f *fakeAPI) Register(_ context.Context, req remote.RegisterRequest) error {
	f.register = append(f.register, req)
	return f.err
}

func (f *fakeAPI) Desired(context.Context, int64, time.Duration) (remote.Desired, error) {
	return f.desired, f.err
}

func (f *fakeAPI) Observed(_ context.Context, rep remote.ObservedReport) error {
	f.reports = append(f.reports, rep)
	return f.err
}

type rig struct {
	t   *testing.T
	a   *Agent
	run *fakeRunner
	api *fakeAPI
	dir string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{t: t, run: newFakeRunner(), api: &fakeAPI{}, dir: dir}
	cfg := Config{URL: "https://grus.example", Token: "agent-secret", Host: "mc01", Dir: dir, Address: "10.200.2.35", PortMin: 30000, PortMax: 30100}
	r.a = New(context.Background(), cfg, r.api, r.run, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.a.spawn = func(f func()) { f() }
	return r
}

func desiredInstance(id string, mode model.Mode) remote.DesiredInstance {
	return remote.DesiredInstance{
		ID: id, Pool: "lobby", Role: model.RoleGame, Mode: mode, Generation: 2, Token: "tok-" + id, TokenHash: "h-" + id,
		Effective: model.EffectiveSpec{Driver: "agent", Command: []string{"./server"}, Env: map[string]string{"A": "pool"}},
		Env:       map[string]string{"GRUS_URL": "http://cluster.internal:8080", "GRUS_MAX_PLAYERS": "20"},
		Templates: []remote.TemplateVersion{{Name: "base", Digest: "sha256:aa"}},
		MemoryMiB: 512,
	}
}

func (r *rig) poll(d remote.Desired) {
	r.t.Helper()
	r.api.desired = d
	if err := r.a.poll(context.Background(), 0); err != nil {
		r.t.Fatalf("poll: %v", err)
	}
}

func TestPollStartsMissingInstancesWithTheAgentsControllerURL(t *testing.T) {
	r := newRig(t)
	r.poll(remote.Desired{Revision: 7, Instances: []remote.DesiredInstance{desiredInstance("lobby-a", model.ModeDynamic)}})

	if len(r.run.started) != 1 {
		t.Fatalf("started %d instances, want 1", len(r.run.started))
	}
	spec := r.run.started[0]
	if spec.Token != "tok-lobby-a" || spec.Instance.TokenHash != "h-lobby-a" || spec.Templates[0].Digest != "sha256:aa" {
		t.Errorf("spec token/hash/templates = %q %q %v", spec.Token, spec.Instance.TokenHash, spec.Templates)
	}
	want := map[string]string{
		"GRUS_URL": "https://grus.example", "GRUS_INSTANCE": "lobby-a", "GRUS_POOL": "lobby", "GRUS_ROLE": "game",
		"A": "pool", "GRUS_MAX_PLAYERS": "20",
	}
	for k, v := range want {
		if spec.Env[k] != v {
			t.Errorf("env %s = %q, want %q", k, spec.Env[k], v)
		}
	}
	if spec.Pool.Spec.Mode != model.ModeDynamic || spec.Pool.Spec.Resources.MemoryMiB != 512 {
		t.Errorf("pool spec = %+v", spec.Pool.Spec)
	}
}

func TestRepeatedPollsDoNotStartAnInstanceTwice(t *testing.T) {
	r := newRig(t)
	d := remote.Desired{Revision: 1, Instances: []remote.DesiredInstance{desiredInstance("lobby-a", model.ModeDynamic)}}
	r.poll(d)
	r.poll(d)
	if len(r.run.started) != 1 {
		t.Errorf("started %d times, want 1", len(r.run.started))
	}
}

func TestFailedStartIsReportedAsFailedAndNotRetried(t *testing.T) {
	r := newRig(t)
	r.run.startErr = errors.New("fetch templates: boom")
	d := remote.Desired{Revision: 1, Instances: []remote.DesiredInstance{desiredInstance("lobby-a", model.ModeDynamic)}}
	r.poll(d)
	r.run.startErr = nil
	r.poll(d)

	if len(r.run.started) != 0 {
		t.Error("a failed start was retried")
	}
	if err := r.a.report(context.Background()); err != nil {
		t.Fatalf("report: %v", err)
	}
	got := r.api.reports[0].Instances
	if len(got) != 1 || got[0].Phase != driver.PhaseFailed || !strings.Contains(got[0].Reason, "boom") || got[0].TokenHash != "h-lobby-a" {
		t.Errorf("reported instances = %+v, want one Failed with the reason", got)
	}
}

func TestReportCarriesRevisionInstancesAndLogTails(t *testing.T) {
	r := newRig(t)
	r.run.logs["lobby-a"] = []string{"one", "two"}
	r.poll(remote.Desired{Revision: 9, Instances: []remote.DesiredInstance{desiredInstance("lobby-a", model.ModeDynamic)}})

	if err := r.a.report(context.Background()); err != nil {
		t.Fatalf("report: %v", err)
	}
	rep := r.api.reports[0]
	if rep.Revision != 9 {
		t.Errorf("report revision = %d, want the applied 9", rep.Revision)
	}
	if len(rep.Instances) != 1 || rep.Instances[0].Phase != driver.PhaseRunning || rep.Instances[0].Address != "10.0.0.1:30000" {
		t.Errorf("instances = %+v", rep.Instances)
	}
	if got := rep.Logs["lobby-a"]; len(got) != 2 || got[1] != "two" {
		t.Errorf("logs = %v", rep.Logs)
	}
}

func TestStopRequestIsForwardedOnce(t *testing.T) {
	r := newRig(t)
	di := desiredInstance("lobby-a", model.ModeDynamic)
	r.poll(remote.Desired{Revision: 1, Instances: []remote.DesiredInstance{di}})
	di.Stop = remote.StopRequest{Requested: true, Command: "stop", GraceSeconds: 20}
	r.poll(remote.Desired{Revision: 2, Instances: []remote.DesiredInstance{di}})
	r.poll(remote.Desired{Revision: 2, Instances: []remote.DesiredInstance{di}})

	if len(r.run.stopped) != 1 || r.run.stopped[0] != "lobby-a" {
		t.Errorf("stopped = %v, want one stop of lobby-a", r.run.stopped)
	}
}

func TestKillRemovesTheInstanceAndTheDirectoryOfDynamicOnes(t *testing.T) {
	r := newRig(t)
	for _, c := range []struct {
		id   string
		mode model.Mode
		keep bool
	}{{"dyn-1", model.ModeDynamic, false}, {"static-1", model.ModeStatic, true}} {
		di := desiredInstance(c.id, c.mode)
		r.poll(remote.Desired{Revision: 1, Instances: []remote.DesiredInstance{di}})
		if err := os.MkdirAll(filepath.Join(r.dir, c.id), 0o755); err != nil {
			t.Fatal(err)
		}
		di.Kill = true
		r.poll(remote.Desired{Revision: 2, Instances: []remote.DesiredInstance{di}})

		_, err := os.Stat(filepath.Join(r.dir, c.id))
		if kept := err == nil; kept != c.keep {
			t.Errorf("%s: directory kept = %v, want %v", c.id, kept, c.keep)
		}
	}
	if len(r.run.killed) != 2 || len(r.run.forgot) != 2 {
		t.Errorf("killed %v forgot %v, want both instances", r.run.killed, r.run.forgot)
	}
}

func TestCommandsAreDeliveredOncePerSequenceNumber(t *testing.T) {
	r := newRig(t)
	d := remote.Desired{Revision: 1, Instances: []remote.DesiredInstance{desiredInstance("lobby-a", model.ModeDynamic)},
		Commands: []remote.Command{{Seq: 5, Instance: "lobby-a", Line: "say hi"}}}
	r.poll(d)
	r.poll(d)
	if len(r.run.commands) != 1 || r.run.commands[0] != "lobby-a: say hi" {
		t.Errorf("commands = %v, want one delivery", r.run.commands)
	}
	if err := r.a.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.api.reports[0].CommandSeq; got != 5 {
		t.Errorf("acknowledged command seq = %d, want 5", got)
	}
}

func TestAdoptedPlaceholdersWithoutACommandAreNeverStarted(t *testing.T) {
	r := newRig(t)
	r.poll(remote.Desired{Revision: 1, Instances: []remote.DesiredInstance{{ID: "old", Pool: "lobby"}}})
	if len(r.run.started) != 0 {
		t.Errorf("started %d instances from a placeholder, want 0", len(r.run.started))
	}
}

func TestRunningInstancesNobodyAsksForAreLeftAloneButExitedOnesAreForgotten(t *testing.T) {
	r := newRig(t)
	r.run.procs["running"] = &driver.Observed{Ref: "running", InstanceID: "running", Phase: driver.PhaseRunning}
	r.run.procs["dead"] = &driver.Observed{Ref: "dead", InstanceID: "dead", Phase: driver.PhaseExited, Mode: model.ModeStatic}
	r.poll(remote.Desired{Revision: 1})

	if len(r.run.killed) != 0 || len(r.run.stopped) != 0 {
		t.Errorf("agent stopped or killed an instance that is not desired: %v %v", r.run.stopped, r.run.killed)
	}
	if len(r.run.forgot) != 1 || r.run.forgot[0] != "dead" {
		t.Errorf("forgot = %v, want [dead]", r.run.forgot)
	}
}

func TestControllerOutageLeavesInstancesUntouched(t *testing.T) {
	r := newRig(t)
	r.poll(remote.Desired{Revision: 1, Instances: []remote.DesiredInstance{desiredInstance("lobby-a", model.ModeDynamic)}})
	r.api.err = errors.New("connection refused")

	if err := r.a.poll(context.Background(), 0); err == nil {
		t.Fatal("poll during an outage returned nil error")
	}
	if len(r.run.killed)+len(r.run.stopped) != 0 {
		t.Error("the outage touched a running instance")
	}
	r.api.err = nil
	if err := r.a.report(context.Background()); err != nil {
		t.Fatalf("report after the outage: %v", err)
	}
	if got := r.api.reports[0].Instances; len(got) != 1 || got[0].ID != "lobby-a" {
		t.Errorf("re-report after reconnect = %+v, want the running instance", got)
	}
}

func TestRegisterAnnouncesAddressAndPortRange(t *testing.T) {
	r := newRig(t)
	if err := r.a.register(context.Background()); err != nil {
		t.Fatalf("register: %v", err)
	}
	got := r.api.register[0]
	if got.Address != "10.200.2.35" || got.PortRange != "30000-30100" {
		t.Errorf("register = %+v", got)
	}
}

func TestStopAllStopsRunningInstancesWithTheirStopCommand(t *testing.T) {
	r := newRig(t)
	di := desiredInstance("lobby-a", model.ModeDynamic)
	r.poll(remote.Desired{Revision: 1, Instances: []remote.DesiredInstance{di}})
	r.run.procs["lobby-a"].Phase = driver.PhaseExited // the fake exits at once, so stopAll does not wait

	r.a.StopAll(context.Background())
	if len(r.run.stopped) != 0 {
		t.Errorf("stopAll stopped %v, want nothing for an instance that already exited", r.run.stopped)
	}
}
