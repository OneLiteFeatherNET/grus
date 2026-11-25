package remote

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/driver/router"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fixture struct {
	clock *fakeClock
	d     *Driver
	timer chan time.Time // fires the long poll timeout
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{clock: &fakeClock{now: time.Unix(1_700_000_000, 0)}, timer: make(chan time.Time, 1)}
	f.d = New(Config{Host: "mc01", HeartbeatTimeout: 30 * time.Second, Clock: f.clock,
		After: func(time.Duration) <-chan time.Time { return f.timer }})
	return f
}

func startSpec(id string) driver.StartSpec {
	return driver.StartSpec{
		Instance:  model.Instance{ID: id, Pool: "lobby", Role: model.RoleGame, PoolGeneration: 3, TokenHash: "h-" + id},
		Pool:      model.Pool{Name: "lobby", Spec: model.PoolSpec{Mode: model.ModeDynamic, Resources: model.Resources{MemoryMiB: 512}}},
		Effective: model.EffectiveSpec{Driver: "agent", Command: []string{"./server"}},
		Token:     "plain-" + id,
		Env:       map[string]string{"GRUS_INSTANCE": id},
		Templates: []driver.ResolvedTemplate{{Name: "base", Digest: "sha256:aa"}},
	}
}

func (f *fixture) online(t *testing.T) {
	t.Helper()
	f.d.Register(RegisterRequest{Version: "1"})
	f.d.Observe(ObservedReport{Revision: f.d.Desired(context.Background(), -1, 0).Revision})
}

func TestListFailsUntilTheAgentHasReported(t *testing.T) {
	f := newFixture(t)
	if _, err := f.d.List(context.Background()); err == nil {
		t.Fatal("List of a host that never connected returned nil error")
	}
	f.d.Register(RegisterRequest{})
	if _, err := f.d.List(context.Background()); err == nil {
		t.Error("List before the first report returned nil error")
	}
	f.d.Observe(ObservedReport{})
	if _, err := f.d.List(context.Background()); err != nil {
		t.Errorf("List after the first report: %v", err)
	}
}

func TestListFailsOnceTheHeartbeatTimeoutPassed(t *testing.T) {
	f := newFixture(t)
	f.online(t)
	f.clock.Advance(31 * time.Second)

	_, err := f.d.List(context.Background())
	if !errors.Is(err, ErrOffline) {
		t.Errorf("List after the heartbeat timeout: err = %v, want ErrOffline", err)
	}
}

func TestStartIsOfferedToTheAgentAndListedAsPendingUntilReported(t *testing.T) {
	f := newFixture(t)
	f.online(t)
	ref, err := f.d.Start(context.Background(), startSpec("lobby-a"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	list, _ := f.d.List(context.Background())
	if len(list) != 1 || list[0].Phase != driver.PhasePending || list[0].Ref != ref {
		t.Fatalf("List = %+v, want one Pending instance %q", list, ref)
	}
	want := f.d.Desired(context.Background(), -1, 0)
	if len(want.Instances) != 1 {
		t.Fatalf("desired instances = %d, want 1", len(want.Instances))
	}
	got := want.Instances[0]
	if got.Token != "plain-lobby-a" || got.TokenHash != "h-lobby-a" || got.MemoryMiB != 512 || got.Templates[0].Digest != "sha256:aa" {
		t.Errorf("desired instance = %+v", got)
	}

	f.d.Observe(ObservedReport{Revision: want.Revision, Instances: []ObservedInstance{
		{ID: "lobby-a", Pool: "lobby", Phase: driver.PhaseRunning, Address: "10.0.0.1:30000"},
	}})
	list, _ = f.d.List(context.Background())
	if len(list) != 1 || list[0].Phase != driver.PhaseRunning || list[0].Address != "10.0.0.1:30000" {
		t.Errorf("List after report = %+v, want the reported Running instance", list)
	}
}

func TestInstanceMissingFromAReportThatAppliedItsStartIsGone(t *testing.T) {
	f := newFixture(t)
	f.online(t)
	_, _ = f.d.Start(context.Background(), startSpec("lobby-a"))
	rev := f.d.Desired(context.Background(), -1, 0).Revision

	f.d.Observe(ObservedReport{Revision: rev})
	list, _ := f.d.List(context.Background())
	if len(list) != 0 {
		t.Errorf("List = %+v, want no instances", list)
	}
}

func TestStartRefusesAnOfflineHostAndADuplicate(t *testing.T) {
	f := newFixture(t)
	if _, err := f.d.Start(context.Background(), startSpec("a")); !errors.Is(err, ErrOffline) {
		t.Errorf("Start on an offline host: err = %v, want ErrOffline", err)
	}
	f.online(t)
	if _, err := f.d.Start(context.Background(), startSpec("a")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := f.d.Start(context.Background(), startSpec("a")); err == nil {
		t.Error("second Start of the same id returned nil error")
	}
}

func TestStopAndKillAreDesiredStateAndKillIsDroppedOnceTheAgentConfirms(t *testing.T) {
	f := newFixture(t)
	f.online(t)
	_, _ = f.d.Start(context.Background(), startSpec("a"))
	if err := f.d.Stop(context.Background(), "a", "stop", 20*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got := f.d.Desired(context.Background(), -1, 0).Instances[0]
	if !got.Stop.Requested || got.Stop.Command != "stop" || got.Stop.GraceSeconds != 20 {
		t.Errorf("stop request = %+v", got.Stop)
	}

	if err := f.d.Kill(context.Background(), "a"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	rev := f.d.Desired(context.Background(), -1, 0)
	if !rev.Instances[0].Kill {
		t.Error("kill flag not set")
	}
	f.d.Observe(ObservedReport{Revision: rev.Revision - 1, Instances: []ObservedInstance{{ID: "a", Phase: driver.PhaseRunning}}})
	f.d.Observe(ObservedReport{Revision: rev.Revision})
	if n := len(f.d.Desired(context.Background(), -1, 0).Instances); n != 0 {
		t.Errorf("desired instances after the agent confirmed the kill = %d, want 0", n)
	}
	if err := f.d.Kill(context.Background(), "a"); !errors.Is(err, driver.ErrNotFound) {
		t.Errorf("Kill of a dropped instance: err = %v, want ErrNotFound", err)
	}
}

func TestLongPollReturnsImmediatelyForANewRevisionAndOtherwiseWaitsForAChange(t *testing.T) {
	f := newFixture(t)
	f.online(t)
	rev := f.d.Desired(context.Background(), -1, 0).Revision

	// The client has the current revision: the poll blocks until the state changes.
	done := make(chan Desired, 1)
	go func() { done <- f.d.Desired(context.Background(), rev, 25*time.Second) }()
	_, _ = f.d.Start(context.Background(), startSpec("a"))
	got := <-done
	if got.Revision == rev || len(got.Instances) != 1 {
		t.Errorf("poll after a change = revision %d (was %d), %d instances", got.Revision, rev, len(got.Instances))
	}

	// A stale client revision returns at once, without waiting for the timer.
	if got := f.d.Desired(context.Background(), rev, 25*time.Second); got.Revision == rev {
		t.Error("poll with a stale revision returned the old revision")
	}
}

func TestLongPollEndsWhenTheWaitTimerFires(t *testing.T) {
	f := newFixture(t)
	f.online(t)
	rev := f.d.Desired(context.Background(), -1, 0).Revision
	f.timer <- time.Time{}

	got := f.d.Desired(context.Background(), rev, 25*time.Second)
	if got.Revision != rev {
		t.Errorf("revision after a timeout = %d, want unchanged %d", got.Revision, rev)
	}
}

func TestConsoleCommandsAreQueuedUntilTheAgentAcknowledgesThem(t *testing.T) {
	f := newFixture(t)
	f.online(t)
	_, _ = f.d.Start(context.Background(), startSpec("a"))
	if err := f.d.Command(context.Background(), "a", "say hi"); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if err := f.d.Command(context.Background(), "nope", "x"); !errors.Is(err, driver.ErrNotFound) {
		t.Errorf("Command to an unknown instance: err = %v, want ErrNotFound", err)
	}
	cmds := f.d.Desired(context.Background(), -1, 0).Commands
	if len(cmds) != 1 || cmds[0].Line != "say hi" || cmds[0].Instance != "a" {
		t.Fatalf("commands = %+v", cmds)
	}
	f.d.Observe(ObservedReport{CommandSeq: cmds[0].Seq, Instances: []ObservedInstance{{ID: "a", Phase: driver.PhaseRunning}}})
	if n := len(f.d.Desired(context.Background(), -1, 0).Commands); n != 0 {
		t.Errorf("commands after the acknowledgement = %d, want 0", n)
	}
}

func TestLogsReturnTheReportedTail(t *testing.T) {
	f := newFixture(t)
	f.online(t)
	_, _ = f.d.Start(context.Background(), startSpec("a"))
	f.d.Observe(ObservedReport{
		Instances: []ObservedInstance{{ID: "a", Phase: driver.PhaseRunning}},
		Logs:      map[string][]string{"a": {"one", "two", "three"}},
	})
	rc, err := f.d.Logs(context.Background(), "a", 2, true)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "two\nthree\n" {
		t.Errorf("logs = %q, want the last two lines", b)
	}
}

func TestInstancesReportedByARestartedAgentStayDesiredSoTheyAreNotDropped(t *testing.T) {
	f := newFixture(t)
	f.d.Register(RegisterRequest{})
	f.d.Observe(ObservedReport{Instances: []ObservedInstance{
		{ID: "old", Pool: "lobby", Role: model.RoleGame, TokenHash: "h", Phase: driver.PhaseRunning},
	}})
	got := f.d.Desired(context.Background(), -1, 0).Instances
	if len(got) != 1 || got[0].ID != "old" || len(got[0].Effective.Command) != 0 {
		t.Fatalf("desired = %+v, want the reported instance without a command", got)
	}
	list, err := f.d.List(context.Background())
	if err != nil || len(list) != 1 || list[0].TokenHash != "h" || list[0].Pool != "lobby" {
		t.Errorf("List = %+v, %v", list, err)
	}
}

func TestRegistryAddsHostsToTheRouterAndNotifiesOnFirstContact(t *testing.T) {
	r := router.New(nil)
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	reg := NewRegistry(r, Config{Clock: clock})
	var notified []string
	reg.OnNewHost(func(h string) { notified = append(notified, h) })
	if err := reg.Preregister("pre"); err != nil {
		t.Fatalf("Preregister: %v", err)
	}

	if _, err := reg.Register(context.Background(), "mc01", RegisterRequest{Version: "2", Address: "10.200.2.35"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := reg.Register(context.Background(), "mc01", RegisterRequest{Version: "2"}); err != nil {
		t.Fatalf("second Register: %v", err)
	}
	if len(notified) != 1 || notified[0] != "mc01" {
		t.Errorf("notified = %v, want [mc01] once", notified)
	}
	if !reg.Online("mc01") || reg.Online("pre") || reg.Online("nope") {
		t.Errorf("Online mc01=%v pre=%v nope=%v, want true false false", reg.Online("mc01"), reg.Online("pre"), reg.Online("nope"))
	}
	if _, err := reg.Host("nope"); err == nil {
		t.Error("Host of an unknown name returned nil error")
	}
	hosts := reg.Hosts()
	if len(hosts) != 2 || hosts[0].Name != "mc01" || hosts[1].Name != "pre" || hosts[1].LastSeen != nil {
		t.Errorf("Hosts = %+v", hosts)
	}
	_, err := r.List(context.Background())
	var partial *router.PartialListError
	if !errors.As(err, &partial) || len(partial.Hosts) != 2 {
		t.Errorf("router List err = %v, want both hosts unlisted before any report", err)
	}
}
