package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/driver/fake"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

// fakeClock is advanced by the tests; nothing reads the wall clock.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// recorder is a model.Publisher that keeps every event for assertions.
type recorder struct {
	types []string
	data  []any
}

func (r *recorder) Publish(eventType string, data any) {
	r.types = append(r.types, eventType)
	r.data = append(r.data, data)
}

func (r *recorder) count(eventType string) int {
	n := 0
	for _, t := range r.types {
		if t == eventType {
			n++
		}
	}
	return n
}

func (r *recorder) scaled(t *testing.T) []model.PoolScaledData {
	t.Helper()
	var out []model.PoolScaledData
	for i, typ := range r.types {
		if typ == model.EventPoolScaled {
			out = append(out, r.data[i].(model.PoolScaledData))
		}
	}
	return out
}

func (r *recorder) stateEvents(t *testing.T) []model.InstanceStateData {
	t.Helper()
	var out []model.InstanceStateData
	for i, typ := range r.types {
		if typ == model.EventInstanceState {
			out = append(out, r.data[i].(model.InstanceStateData))
		}
	}
	return out
}

type fixture struct {
	t     *testing.T
	ctx   context.Context
	clock *fakeClock
	st    store.Store
	drv   *fake.Driver
	pub   *recorder
	c     *Controller
}

func newFixture(t *testing.T, cfg Config) *fixture {
	t.Helper()
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	st, err := store.OpenSQLite(ctx, filepath.Join(t.TempDir(), "grus.db"), clock)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg.Clock = clock
	cfg.PublicURL = "http://grus.test"
	drv := fake.New("test-host")
	pub := &recorder{}
	return &fixture{t: t, ctx: ctx, clock: clock, st: st, drv: drv, pub: pub,
		c: New(cfg, st, drv, nil, pub)}
}

func dynamicSpec(minInst, buffer int) model.PoolSpec {
	return model.PoolSpec{
		Role:      model.RoleGame,
		Mode:      model.ModeDynamic,
		Driver:    "kubernetes",
		Image:     "registry.test/lobby:1",
		Resources: model.Resources{MemoryMiB: 1024},
		Scaling: model.Scaling{
			Min: minInst, Max: 10, Buffer: buffer, Capacity: 20,
			ScaleDownDelay: model.Duration(time.Minute),
		},
	}
}

func staticSpec(minInst int) model.PoolSpec {
	return model.PoolSpec{
		Role:      model.RoleGame,
		Mode:      model.ModeStatic,
		Driver:    "kubernetes",
		Image:     "registry.test/survival:1",
		Resources: model.Resources{MemoryMiB: 2048},
		Scaling:   model.Scaling{Min: minInst, Max: minInst},
	}
}

func (f *fixture) putPool(name string, spec model.PoolSpec) {
	f.t.Helper()
	if _, err := f.c.PutPool(f.ctx, name, spec); err != nil {
		f.t.Fatalf("put pool %s: %v", name, err)
	}
}

func (f *fixture) reconcile() {
	f.t.Helper()
	if err := f.c.ReconcileOnce(f.ctx); err != nil {
		f.t.Fatalf("reconcile: %v", err)
	}
}

func (f *fixture) get(id string) model.Instance {
	f.t.Helper()
	inst, err := f.st.GetInstance(f.ctx, id)
	if err != nil {
		f.t.Fatalf("get instance %s: %v", id, err)
	}
	return inst
}

func (f *fixture) instancesOf(pool string) []model.Instance {
	f.t.Helper()
	insts, err := f.st.ListInstances(f.ctx, store.InstanceFilter{Pool: pool})
	if err != nil {
		f.t.Fatalf("list instances of %s: %v", pool, err)
	}
	return insts
}

// bringUp simulates the driver reporting Running, one reconcile and the instance calling self/ready.
func (f *fixture) bringUp(id, addr string) {
	f.t.Helper()
	f.drv.SetPhase(id, driver.PhaseRunning, addr)
	f.reconcile()
	if _, err := f.c.SelfReady(f.ctx, id); err != nil {
		f.t.Fatalf("self ready %s: %v", id, err)
	}
}

// heartbeatAll sends a status update for every live instance with its current player count.
func (f *fixture) heartbeatAll() {
	f.t.Helper()
	for _, inst := range f.instancesOf("") {
		if inst.State == model.StateReady || inst.State == model.StateAllocated || inst.State == model.StateUnhealthy {
			if _, err := f.c.SelfStatus(f.ctx, inst.ID, StatusUpdate{Players: inst.Players}); err != nil {
				f.t.Fatalf("heartbeat %s: %v", inst.ID, err)
			}
		}
	}
}

// advance moves the clock in 10 second steps, heartbeating and reconciling at each step.
func (f *fixture) advance(d time.Duration) {
	f.t.Helper()
	for d > 0 {
		step := min(d, 10*time.Second)
		f.clock.Advance(step)
		d -= step
		f.heartbeatAll()
		f.reconcile()
	}
}

func TestNewDynamicPoolStartsMinAndBufferInstances(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 1))

	f.reconcile()

	started := f.drv.Started()
	if len(started) != 2 {
		t.Fatalf("started instances = %d, want 2 (min 1 + buffer 1)", len(started))
	}
	for _, s := range started {
		if s.Token == "" {
			t.Errorf("instance %s: start spec has no token", s.Instance.ID)
		}
		want := map[string]string{
			"GRUS_URL":         "http://grus.test",
			"GRUS_INSTANCE":    s.Instance.ID,
			"GRUS_POOL":        "lobby",
			"GRUS_ROLE":        "game",
			"GRUS_PORT":        "25565",
			"GRUS_MAX_PLAYERS": "20",
		}
		for k, v := range want {
			if got := s.Env[k]; got != v {
				t.Errorf("instance %s: env %s = %q, want %q", s.Instance.ID, k, got, v)
			}
		}
	}
	if n := f.pub.count(model.EventInstanceCreated); n != 2 {
		t.Errorf("instance.created events = %d, want 2", n)
	}
	if n := f.pub.count(model.EventInstanceState); n == 0 {
		t.Errorf("no instance.state events published")
	}
	scaled := f.pub.scaled(t)
	if len(scaled) != 1 || scaled[0].Reason != "min" || scaled[0].Direction != "up" {
		t.Errorf("pool.scaled = %+v, want one up event with reason min", scaled)
	}
}

func TestDriverRunningMovesToStartingAndSelfReadyToReady(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 0))
	f.reconcile()
	id := f.drv.Started()[0].Instance.ID

	f.drv.SetPhase(id, driver.PhaseRunning, "10.0.0.7:25565")
	f.reconcile()
	if got := f.get(id).State; got != model.StateStarting {
		t.Fatalf("state after driver Running = %s, want Starting", got)
	}

	inst, err := f.c.SelfReady(f.ctx, id)
	if err != nil {
		t.Fatalf("self ready: %v", err)
	}
	if inst.State != model.StateReady {
		t.Fatalf("state after self ready = %s, want Ready", inst.State)
	}
	if inst.Address != "10.0.0.7:25565" {
		t.Errorf("address = %q, want the observed address", inst.Address)
	}
}

func TestPlayersOnBothLobbiesStartBufferInstance(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 1))
	f.reconcile()
	ids := []string{f.drv.Started()[0].Instance.ID, f.drv.Started()[1].Instance.ID}
	f.bringUp(ids[0], "10.0.0.1:25565")
	f.bringUp(ids[1], "10.0.0.2:25565")

	if _, err := f.c.SelfStatus(f.ctx, ids[0], StatusUpdate{Players: 18}); err != nil {
		t.Fatalf("status 1: %v", err)
	}
	if _, err := f.c.SelfStatus(f.ctx, ids[1], StatusUpdate{Players: 5}); err != nil {
		t.Fatalf("status 2: %v", err)
	}
	f.reconcile()

	if n := len(f.drv.Started()); n != 3 {
		t.Fatalf("started instances = %d, want 3", n)
	}
	scaled := f.pub.scaled(t)
	if last := scaled[len(scaled)-1]; last.Reason != "buffer" {
		t.Errorf("last pool.scaled reason = %q, want buffer", last.Reason)
	}
}

func TestPlayersLeavingDrainAndRemoveOneReadyInstance(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 1))
	f.reconcile()
	ids := []string{f.drv.Started()[0].Instance.ID, f.drv.Started()[1].Instance.ID}
	f.bringUp(ids[0], "10.0.0.1:25565")
	f.bringUp(ids[1], "10.0.0.2:25565")
	_, _ = f.c.SelfStatus(f.ctx, ids[0], StatusUpdate{Players: 18})
	_, _ = f.c.SelfStatus(f.ctx, ids[1], StatusUpdate{Players: 5})
	f.reconcile()
	third := f.drv.Started()[2].Instance.ID
	f.bringUp(third, "10.0.0.3:25565")

	for _, id := range []string{ids[0], ids[1], third} {
		if _, err := f.c.SelfStatus(f.ctx, id, StatusUpdate{Players: 0}); err != nil {
			t.Fatalf("status %s: %v", id, err)
		}
	}
	f.advance(time.Minute + 10*time.Second) // the first reconcile that sees 0 players starts the timer

	var draining []string
	for _, inst := range f.instancesOf("lobby") {
		if inst.State == model.StateDraining {
			draining = append(draining, inst.ID)
		}
	}
	if len(draining) != 1 {
		t.Fatalf("draining instances = %v, want exactly one after ScaleDownDelay", draining)
	}
	victim := draining[0]

	f.reconcile() // Draining with 0 players is stopped
	if got := f.get(victim).State; got != model.StateStopping {
		t.Fatalf("state of drained instance = %s, want Stopping", got)
	}
	if !slices.Contains(f.drv.Stopped(), victim) {
		t.Fatalf("driver Stop not called for %s; stopped = %v", victim, f.drv.Stopped())
	}

	f.drv.Remove(victim)
	f.reconcile()
	if _, err := f.st.GetInstance(f.ctx, victim); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("drained instance still stored after removal: %v", err)
	}
	if f.pub.count(model.EventInstanceRemoved) != 1 {
		t.Errorf("instance.removed events = %d, want 1", f.pub.count(model.EventInstanceRemoved))
	}
}

func TestVanishedReadyInstanceIsLostAndReplaced(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 0))
	f.reconcile()
	id := f.drv.Started()[0].Instance.ID
	f.bringUp(id, "10.0.0.1:25565")

	f.drv.Remove(id)
	f.reconcile()

	if _, err := f.st.GetInstance(f.ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("vanished instance still stored: %v", err)
	}
	sawLost := false
	for _, ev := range f.pub.stateEvents(t) {
		if ev.Instance == id && ev.To == model.StateLost {
			sawLost = true
		}
	}
	if !sawLost {
		t.Errorf("no instance.state to Lost published for %s", id)
	}
	if len(f.drv.Started()) != 2 {
		t.Fatalf("started = %d, want a replacement (2 total)", len(f.drv.Started()))
	}
	live := f.instancesOf("lobby")
	if len(live) != 1 || live[0].ID == id {
		t.Fatalf("live instances = %+v, want one new instance", live)
	}
}

func TestMissingHeartbeatMarksUnhealthyAndStatusRecovers(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 0))
	f.reconcile()
	id := f.drv.Started()[0].Instance.ID
	f.bringUp(id, "10.0.0.1:25565")

	f.clock.Advance(20 * time.Second) // HeartbeatTimeout default is 15s
	f.reconcile()
	if got := f.get(id).State; got != model.StateUnhealthy {
		t.Fatalf("state after missed heartbeat = %s, want Unhealthy", got)
	}

	if _, err := f.c.SelfStatus(f.ctx, id, StatusUpdate{Players: 0}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := f.get(id).State; got != model.StateReady {
		t.Fatalf("state after status = %s, want Ready", got)
	}
}

func TestAdoptCreatesRecordsWithoutStoppingAnything(t *testing.T) {
	f := newFixture(t, Config{})
	if f.c.Ready() {
		t.Fatalf("Ready() = true before Adopt")
	}
	f.drv.Add(driver.Observed{InstanceID: "lobby-run", Pool: "lobby", Role: model.RoleGame,
		Phase: driver.PhaseRunning, Address: "10.0.0.9:25565"})
	f.drv.Add(driver.Observed{InstanceID: "lobby-pend", Pool: "lobby", Role: model.RoleGame,
		Phase: driver.PhasePending})
	f.drv.Add(driver.Observed{InstanceID: "lobby-dead", Pool: "lobby", Role: model.RoleGame,
		Phase: driver.PhaseExited})

	if err := f.c.Adopt(f.ctx); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if !f.c.Ready() {
		t.Errorf("Ready() = false after successful Adopt")
	}
	if got := f.get("lobby-run").State; got != model.StateReady {
		t.Errorf("running pod adopted as %s, want Ready", got)
	}
	if got := f.get("lobby-pend").State; got != model.StateStarting {
		t.Errorf("pending pod adopted as %s, want Starting", got)
	}
	if got := f.get("lobby-dead").State; got != model.StateStopped {
		t.Errorf("exited pod adopted as %s, want Stopped", got)
	}
	if len(f.drv.Stopped()) != 0 {
		t.Errorf("adoption stopped %v, want nothing", f.drv.Stopped())
	}
	if !slices.Equal(f.drv.Killed(), []string{"lobby-dead"}) {
		t.Errorf("killed = %v, want only the exited pod", f.drv.Killed())
	}
}

func TestStopWithPlayersNeedsForce(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 0))
	f.reconcile()
	id := f.drv.Started()[0].Instance.ID
	f.bringUp(id, "10.0.0.1:25565")
	if _, err := f.c.SelfStatus(f.ctx, id, StatusUpdate{Players: 3}); err != nil {
		t.Fatalf("status: %v", err)
	}

	_, err := f.c.StopInstance(f.ctx, id, StopOptions{})
	if !errors.Is(err, ErrHasPlayers) {
		t.Fatalf("stop with players: err = %v, want ErrHasPlayers", err)
	}
	if got := f.get(id).State; got != model.StateReady {
		t.Fatalf("state after refused stop = %s, want Ready", got)
	}

	inst, err := f.c.StopInstance(f.ctx, id, StopOptions{Force: true})
	if err != nil {
		t.Fatalf("forced stop: %v", err)
	}
	if inst.State != model.StateStopping {
		t.Errorf("state after forced stop = %s, want Stopping", inst.State)
	}
	if !slices.Contains(f.drv.Stopped(), id) {
		t.Errorf("driver Stop not called for %s", id)
	}
}

func TestMaxInstancesStopsCreation(t *testing.T) {
	f := newFixture(t, Config{MaxInstances: 1})
	f.putPool("lobby", dynamicSpec(2, 0))

	f.reconcile()

	if n := len(f.drv.Started()); n != 1 {
		t.Fatalf("started = %d, want 1 with MaxInstances 1", n)
	}
	if _, err := f.c.CreateInstance(f.ctx, "lobby"); !errors.Is(err, ErrLimit) {
		t.Fatalf("CreateInstance at limit: err = %v, want ErrLimit", err)
	}
}

func TestStaticInstanceKeepsNameAndWaitsForRestartDelay(t *testing.T) {
	f := newFixture(t, Config{StaticRestartDelay: 10 * time.Second})
	f.putPool("survival", staticSpec(1))
	f.reconcile()
	first := f.drv.Started()[0].Instance.ID
	if first != "survival-1" {
		t.Fatalf("static id = %q, want survival-1", first)
	}
	f.bringUp(first, "10.0.0.5:25565")

	if _, err := f.c.StopInstance(f.ctx, first, StopOptions{}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	f.drv.SetPhase(first, driver.PhaseExited, "")
	f.reconcile()
	if got := f.get(first).State; got != model.StateStopped {
		t.Fatalf("state after exit = %s, want Stopped", got)
	}
	f.drv.Remove(first) // the killed pod is gone; the fake's Kill only records the call

	f.reconcile()
	if n := len(f.drv.Started()); n != 1 {
		t.Fatalf("restarted before StaticRestartDelay: started = %d", n)
	}
	f.clock.Advance(10 * time.Second)
	f.reconcile()
	if n := len(f.drv.Started()); n != 1 {
		t.Fatalf("restarted at exactly StaticRestartDelay: started = %d", n)
	}

	f.clock.Advance(time.Second)
	f.reconcile()
	started := f.drv.Started()
	if len(started) != 2 || started[1].Instance.ID != "survival-1" {
		t.Fatalf("after delay: started ids = %v, want second start with survival-1", startedIDs(started))
	}
}

func TestAuthenticateInstanceMapsPlainTokenOnly(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 0))
	f.reconcile()
	start := f.drv.Started()[0]

	inst, err := f.c.AuthenticateInstance(f.ctx, start.Token)
	if err != nil {
		t.Fatalf("authenticate with issued token: %v", err)
	}
	if inst.ID != start.Instance.ID {
		t.Errorf("authenticated as %s, want %s", inst.ID, start.Instance.ID)
	}
	sum := sha256.Sum256([]byte(start.Token))
	if inst.TokenHash != hex.EncodeToString(sum[:]) {
		t.Errorf("stored token hash does not match sha256 of the issued token")
	}

	if _, err := f.c.AuthenticateInstance(f.ctx, "unknown-token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown token: err = %v, want ErrNotFound", err)
	}
}

func TestPutPoolRejectsInvalidSpec(t *testing.T) {
	f := newFixture(t, Config{})
	bad := dynamicSpec(1, 0)
	bad.Role = "bogus"

	_, err := f.c.PutPool(f.ctx, "lobby", bad)
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("invalid spec: err = %v, want *ValidationError", err)
	}
	if len(ve.Fields) == 0 {
		t.Errorf("ValidationError carries no field errors")
	}

	_, err = f.c.PutPool(f.ctx, "Bad_Name", dynamicSpec(1, 0))
	if !errors.As(err, &ve) {
		t.Fatalf("invalid name: err = %v, want *ValidationError", err)
	}
}

func startedIDs(specs []driver.StartSpec) []string {
	ids := make([]string, 0, len(specs))
	for _, s := range specs {
		ids = append(ids, s.Instance.ID)
	}
	return ids
}
