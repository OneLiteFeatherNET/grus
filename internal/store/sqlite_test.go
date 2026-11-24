package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// fakeClock is a settable model.Clock so no test reads the wall clock.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

var startTime = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

// newTestStore opens a fresh database file in the test's temp dir and closes it on cleanup.
func newTestStore(t *testing.T, clock model.Clock) *SQLite {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grus.db")
	s, err := OpenSQLite(context.Background(), path, clock)
	if err != nil {
		t.Fatalf("OpenSQLite(%q) failed: %v", path, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func validSpec() model.PoolSpec {
	return model.PoolSpec{
		Role:      model.RoleGame,
		Mode:      model.ModeDynamic,
		Driver:    "kubernetes",
		Image:     "ghcr.io/example/lobby:1",
		Resources: model.Resources{MemoryMiB: 1024},
		Scaling:   model.Scaling{Min: 1, Max: 3, Buffer: 1},
	}
}

func newInstance(id, pool string, created time.Time) model.Instance {
	return model.Instance{
		ID:             id,
		Pool:           pool,
		Role:           model.RoleGame,
		PoolGeneration: 1,
		State:          model.StateRequested,
		StateSince:     created,
		MaxPlayers:     20,
		CreatedAt:      created,
	}
}

func TestOpenSQLiteRunsMigrationsOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grus.db")
	clock := &fakeClock{now: startTime}
	first, err := OpenSQLite(context.Background(), path, clock)
	if err != nil {
		t.Fatalf("first OpenSQLite failed: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close after first open failed: %v", err)
	}

	second, err := OpenSQLite(context.Background(), path, clock)
	if err != nil {
		t.Fatalf("reopening an already migrated database failed: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := second.ListPools(context.Background()); err != nil {
		t.Errorf("tables missing after reopen: ListPools failed: %v", err)
	}
}

func TestPutPoolInsertStartsAtGenerationOne(t *testing.T) {
	clock := &fakeClock{now: startTime}
	s := newTestStore(t, clock)

	pool, changed, err := s.PutPool(context.Background(), "lobby", validSpec())
	if err != nil {
		t.Fatalf("PutPool failed: %v", err)
	}
	if !changed {
		t.Errorf("PutPool of a new pool reported changed=false, want true")
	}
	if pool.Generation != 1 {
		t.Errorf("generation of new pool = %d, want 1", pool.Generation)
	}
	if !pool.CreatedAt.Equal(startTime) {
		t.Errorf("created_at = %v, want %v", pool.CreatedAt, startTime)
	}
}

func TestPutPoolSameSpecKeepsGenerationAndUpdatedAt(t *testing.T) {
	clock := &fakeClock{now: startTime}
	s := newTestStore(t, clock)
	ctx := context.Background()
	if _, _, err := s.PutPool(ctx, "lobby", validSpec()); err != nil {
		t.Fatalf("first PutPool failed: %v", err)
	}

	clock.advance(time.Hour)
	pool, changed, err := s.PutPool(ctx, "lobby", validSpec())
	if err != nil {
		t.Fatalf("second PutPool failed: %v", err)
	}
	if changed {
		t.Errorf("PutPool with the same spec reported changed=true, want false")
	}
	if pool.Generation != 1 {
		t.Errorf("generation after identical spec = %d, want 1", pool.Generation)
	}
	if !pool.UpdatedAt.Equal(startTime) {
		t.Errorf("updated_at after identical spec = %v, want unchanged %v", pool.UpdatedAt, startTime)
	}
}

func TestPutPoolChangedSpecIncrementsGenerationAndSetsUpdatedAt(t *testing.T) {
	clock := &fakeClock{now: startTime}
	s := newTestStore(t, clock)
	ctx := context.Background()
	if _, _, err := s.PutPool(ctx, "lobby", validSpec()); err != nil {
		t.Fatalf("first PutPool failed: %v", err)
	}

	changedSpec := validSpec()
	changedSpec.Scaling.Max = 10
	clock.advance(time.Hour)
	pool, changed, err := s.PutPool(ctx, "lobby", changedSpec)
	if err != nil {
		t.Fatalf("PutPool with changed spec failed: %v", err)
	}
	if !changed {
		t.Errorf("PutPool with changed spec reported changed=false, want true")
	}
	if pool.Generation != 2 {
		t.Errorf("generation after changed spec = %d, want 2", pool.Generation)
	}
	if want := startTime.Add(time.Hour); !pool.UpdatedAt.Equal(want) {
		t.Errorf("updated_at after changed spec = %v, want %v", pool.UpdatedAt, want)
	}

	stored, err := s.GetPool(ctx, "lobby")
	if err != nil {
		t.Fatalf("GetPool failed: %v", err)
	}
	if stored.Spec.Scaling.Max != 10 {
		t.Errorf("stored scaling.max = %d, want 10", stored.Spec.Scaling.Max)
	}
}

func TestPutPoolPreservesMaintenance(t *testing.T) {
	clock := &fakeClock{now: startTime}
	s := newTestStore(t, clock)
	ctx := context.Background()
	if _, _, err := s.PutPool(ctx, "lobby", validSpec()); err != nil {
		t.Fatalf("PutPool failed: %v", err)
	}
	if _, err := s.SetMaintenance(ctx, "lobby", true); err != nil {
		t.Fatalf("SetMaintenance failed: %v", err)
	}

	changedSpec := validSpec()
	changedSpec.Scaling.Max = 5
	if _, _, err := s.PutPool(ctx, "lobby", changedSpec); err != nil {
		t.Fatalf("PutPool with changed spec failed: %v", err)
	}

	stored, err := s.GetPool(ctx, "lobby")
	if err != nil {
		t.Fatalf("GetPool failed: %v", err)
	}
	if !stored.Maintenance {
		t.Errorf("maintenance flag was reset by PutPool, want it preserved as true")
	}
}

func TestPutPoolPreservesCreatedAt(t *testing.T) {
	clock := &fakeClock{now: startTime}
	s := newTestStore(t, clock)
	ctx := context.Background()
	if _, _, err := s.PutPool(ctx, "lobby", validSpec()); err != nil {
		t.Fatalf("PutPool failed: %v", err)
	}

	clock.advance(24 * time.Hour)
	changedSpec := validSpec()
	changedSpec.Scaling.Max = 5
	if _, _, err := s.PutPool(ctx, "lobby", changedSpec); err != nil {
		t.Fatalf("PutPool with changed spec failed: %v", err)
	}

	stored, err := s.GetPool(ctx, "lobby")
	if err != nil {
		t.Fatalf("GetPool failed: %v", err)
	}
	if !stored.CreatedAt.Equal(startTime) {
		t.Errorf("created_at = %v after update, want original %v", stored.CreatedAt, startTime)
	}
}

func TestPoolSpecRoundTripKeepsMapsAndDurations(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	spec := validSpec()
	spec.Env = map[string]string{"MOTD": "hello"}
	spec.Lifecycle.GracePeriod = model.Duration(45 * time.Second)
	if _, _, err := s.PutPool(ctx, "lobby", spec); err != nil {
		t.Fatalf("PutPool failed: %v", err)
	}

	stored, err := s.GetPool(ctx, "lobby")
	if err != nil {
		t.Fatalf("GetPool failed: %v", err)
	}
	if stored.Spec.Env["MOTD"] != "hello" {
		t.Errorf("stored env = %v, want MOTD=hello", stored.Spec.Env)
	}
	if stored.Spec.Lifecycle.GracePeriod.D() != 45*time.Second {
		t.Errorf("stored gracePeriod = %v, want 45s", stored.Spec.Lifecycle.GracePeriod.D())
	}
}

func TestGetPoolUnknownReturnsNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})

	_, err := s.GetPool(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPool of unknown pool error = %v, want ErrNotFound", err)
	}
}

func TestSetMaintenanceUnknownPoolReturnsNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})

	_, err := s.SetMaintenance(context.Background(), "missing", true)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SetMaintenance of unknown pool error = %v, want ErrNotFound", err)
	}
}

func TestSetMaintenanceReturnsUpdatedPool(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	if _, _, err := s.PutPool(ctx, "lobby", validSpec()); err != nil {
		t.Fatalf("PutPool failed: %v", err)
	}

	pool, err := s.SetMaintenance(ctx, "lobby", true)
	if err != nil {
		t.Fatalf("SetMaintenance failed: %v", err)
	}
	if !pool.Maintenance {
		t.Errorf("returned pool maintenance = false, want true")
	}
}

func TestDeletePoolUnknownReturnsNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})

	err := s.DeletePool(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("DeletePool of unknown pool error = %v, want ErrNotFound", err)
	}
}

func TestDeletePoolKeepsItsInstances(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	if _, _, err := s.PutPool(ctx, "lobby", validSpec()); err != nil {
		t.Fatalf("PutPool failed: %v", err)
	}
	if err := s.CreateInstance(ctx, newInstance("lobby-1", "lobby", startTime)); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	if err := s.DeletePool(ctx, "lobby"); err != nil {
		t.Fatalf("DeletePool failed: %v", err)
	}
	if _, err := s.GetInstance(ctx, "lobby-1"); err != nil {
		t.Errorf("instance disappeared after DeletePool: %v", err)
	}
}

func TestListPoolsOrdersByName(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	for _, name := range []string{"survival", "lobby"} {
		if _, _, err := s.PutPool(ctx, name, validSpec()); err != nil {
			t.Fatalf("PutPool(%q) failed: %v", name, err)
		}
	}

	pools, err := s.ListPools(ctx)
	if err != nil {
		t.Fatalf("ListPools failed: %v", err)
	}
	if len(pools) != 2 || pools[0].Name != "lobby" || pools[1].Name != "survival" {
		t.Errorf("ListPools names = %v, want [lobby survival]", poolNames(pools))
	}
}

func poolNames(pools []model.Pool) []string {
	names := make([]string, 0, len(pools))
	for _, p := range pools {
		names = append(names, p.Name)
	}
	return names
}

func TestInstanceRoundTripKeepsLabelsAndTimes(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	allocated := startTime.Add(90 * time.Second)
	heartbeat := startTime.Add(120 * time.Second)
	inst := newInstance("lobby-1", "lobby", startTime)
	inst.Labels = map[string]string{"region": "eu", "tier": "gold"}
	inst.AllocatedAt = &allocated
	inst.LastHeartbeat = &heartbeat
	inst.TokenHash = "hash-1"
	inst.Address = "10.0.0.5:25565"
	inst.Players = 4

	if err := s.CreateInstance(ctx, inst); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}
	got, err := s.GetInstance(ctx, "lobby-1")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if got.Labels["region"] != "eu" || got.Labels["tier"] != "gold" || len(got.Labels) != 2 {
		t.Errorf("labels = %v, want region=eu tier=gold", got.Labels)
	}
	if got.AllocatedAt == nil || !got.AllocatedAt.Equal(allocated) {
		t.Errorf("allocated_at = %v, want %v", got.AllocatedAt, allocated)
	}
	if got.LastHeartbeat == nil || !got.LastHeartbeat.Equal(heartbeat) {
		t.Errorf("last_heartbeat = %v, want %v", got.LastHeartbeat, heartbeat)
	}
	if !got.StateSince.Equal(startTime) || !got.CreatedAt.Equal(startTime) {
		t.Errorf("state_since/created_at = %v/%v, want %v", got.StateSince, got.CreatedAt, startTime)
	}
	if got.Address != "10.0.0.5:25565" || got.Players != 4 || got.TokenHash != "hash-1" {
		t.Errorf("address/players/token_hash = %q/%d/%q, want 10.0.0.5:25565/4/hash-1",
			got.Address, got.Players, got.TokenHash)
	}
}

func TestInstanceRoundTripKeepsNilTimesAndNilLabels(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	if err := s.CreateInstance(ctx, newInstance("lobby-1", "lobby", startTime)); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	got, err := s.GetInstance(ctx, "lobby-1")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if got.AllocatedAt != nil {
		t.Errorf("allocated_at = %v, want nil", got.AllocatedAt)
	}
	if got.LastHeartbeat != nil {
		t.Errorf("last_heartbeat = %v, want nil", got.LastHeartbeat)
	}
	if got.Labels != nil {
		t.Errorf("labels = %v, want nil map", got.Labels)
	}
}

func TestUpdateInstanceReplacesFields(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	if err := s.CreateInstance(ctx, newInstance("lobby-1", "lobby", startTime)); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	inst, err := s.GetInstance(ctx, "lobby-1")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	inst.State = model.StateReady
	inst.StateReason = "heartbeat ok"
	inst.Labels = map[string]string{"x": "y"}
	heartbeat := startTime.Add(time.Minute)
	inst.LastHeartbeat = &heartbeat
	if err := s.UpdateInstance(ctx, inst); err != nil {
		t.Fatalf("UpdateInstance failed: %v", err)
	}

	got, err := s.GetInstance(ctx, "lobby-1")
	if err != nil {
		t.Fatalf("GetInstance after update failed: %v", err)
	}
	if got.State != model.StateReady || got.StateReason != "heartbeat ok" {
		t.Errorf("state/reason = %q/%q, want Ready/heartbeat ok", got.State, got.StateReason)
	}
	if got.Labels["x"] != "y" {
		t.Errorf("labels after update = %v, want x=y", got.Labels)
	}
	if got.LastHeartbeat == nil || !got.LastHeartbeat.Equal(heartbeat) {
		t.Errorf("last_heartbeat after update = %v, want %v", got.LastHeartbeat, heartbeat)
	}
}

func TestUpdateInstanceUnknownReturnsNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})

	err := s.UpdateInstance(context.Background(), newInstance("missing", "lobby", startTime))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateInstance of unknown instance error = %v, want ErrNotFound", err)
	}
}

func TestCreateInstanceDuplicateIDReturnsConflict(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	if err := s.CreateInstance(ctx, newInstance("lobby-1", "lobby", startTime)); err != nil {
		t.Fatalf("first CreateInstance failed: %v", err)
	}

	err := s.CreateInstance(ctx, newInstance("lobby-1", "lobby", startTime))
	if !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate CreateInstance error = %v, want ErrConflict", err)
	}
}

func TestGetInstanceUnknownReturnsNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})

	_, err := s.GetInstance(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetInstance of unknown id error = %v, want ErrNotFound", err)
	}
}

func TestDeleteInstanceUnknownReturnsNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})

	err := s.DeleteInstance(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteInstance of unknown id error = %v, want ErrNotFound", err)
	}
}

func TestListInstancesFiltersByPool(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	must(t, s.CreateInstance(ctx, newInstance("lobby-1", "lobby", startTime)))
	must(t, s.CreateInstance(ctx, newInstance("survival-1", "survival", startTime)))

	got, err := s.ListInstances(ctx, InstanceFilter{Pool: "lobby"})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(got) != 1 || got[0].ID != "lobby-1" {
		t.Errorf("ListInstances(pool=lobby) ids = %v, want [lobby-1]", instanceIDs(got))
	}
}

func TestListInstancesFiltersByState(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	ready := newInstance("lobby-1", "lobby", startTime)
	ready.State = model.StateReady
	must(t, s.CreateInstance(ctx, ready))
	must(t, s.CreateInstance(ctx, newInstance("lobby-2", "lobby", startTime)))

	got, err := s.ListInstances(ctx, InstanceFilter{State: model.StateReady})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(got) != 1 || got[0].ID != "lobby-1" {
		t.Errorf("ListInstances(state=Ready) ids = %v, want [lobby-1]", instanceIDs(got))
	}
}

func TestListInstancesWithoutFilterReturnsAll(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	must(t, s.CreateInstance(ctx, newInstance("lobby-1", "lobby", startTime)))
	must(t, s.CreateInstance(ctx, newInstance("survival-1", "survival", startTime)))

	got, err := s.ListInstances(ctx, InstanceFilter{})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ListInstances() returned %d instances, want 2", len(got))
	}
}

func TestListInstancesOrdersByCreatedAtThenID(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	// a-1 and b-1 share a timestamp, so id breaks the tie; c-1 is half a second earlier than both.
	must(t, s.CreateInstance(ctx, newInstance("b-1", "lobby", startTime.Add(time.Second))))
	must(t, s.CreateInstance(ctx, newInstance("a-1", "lobby", startTime.Add(time.Second))))
	must(t, s.CreateInstance(ctx, newInstance("c-1", "lobby", startTime.Add(500*time.Millisecond))))

	got, err := s.ListInstances(ctx, InstanceFilter{})
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	want := []string{"c-1", "a-1", "b-1"}
	if ids := instanceIDs(got); !equalStrings(ids, want) {
		t.Errorf("ListInstances order = %v, want %v", ids, want)
	}
}

func TestFindInstanceByTokenHashReturnsOwner(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	inst := newInstance("lobby-1", "lobby", startTime)
	inst.TokenHash = "hash-1"
	must(t, s.CreateInstance(ctx, inst))

	got, err := s.FindInstanceByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("FindInstanceByTokenHash failed: %v", err)
	}
	if got.ID != "lobby-1" {
		t.Errorf("owner of hash-1 = %q, want lobby-1", got.ID)
	}
}

func TestFindInstanceByUnknownTokenHashReturnsNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})

	_, err := s.FindInstanceByTokenHash(context.Background(), "unknown")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindInstanceByTokenHash(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestFindInstanceByEmptyTokenHashReturnsNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	must(t, s.CreateInstance(ctx, newInstance("lobby-1", "lobby", startTime)))

	_, err := s.FindInstanceByTokenHash(ctx, "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindInstanceByTokenHash(\"\") error = %v, want ErrNotFound", err)
	}
}

func TestInstancesWithoutTokenHashMayCoexist(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	must(t, s.CreateInstance(ctx, newInstance("lobby-1", "lobby", startTime)))

	if err := s.CreateInstance(ctx, newInstance("lobby-2", "lobby", startTime)); err != nil {
		t.Errorf("second instance without token hash failed: %v", err)
	}
}

func TestDuplicateTokenHashReturnsConflict(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	first := newInstance("lobby-1", "lobby", startTime)
	first.TokenHash = "same"
	must(t, s.CreateInstance(ctx, first))

	second := newInstance("lobby-2", "lobby", startTime)
	second.TokenHash = "same"
	if err := s.CreateInstance(ctx, second); !errors.Is(err, ErrConflict) {
		t.Errorf("CreateInstance with duplicate token hash error = %v, want ErrConflict", err)
	}
}

func TestGetMetaMissingKeyReturnsNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})

	_, err := s.GetMeta(context.Background(), "events.epoch")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMeta of missing key error = %v, want ErrNotFound", err)
	}
}

func TestSetMetaStoresValue(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	must(t, s.SetMeta(ctx, "events.epoch", "e1"))

	got, err := s.GetMeta(ctx, "events.epoch")
	if err != nil {
		t.Fatalf("GetMeta failed: %v", err)
	}
	if got != "e1" {
		t.Errorf("GetMeta = %q, want e1", got)
	}
}

func TestSetMetaOverwritesValue(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	must(t, s.SetMeta(ctx, "events.epoch", "e1"))
	must(t, s.SetMeta(ctx, "events.epoch", "e2"))

	got, err := s.GetMeta(ctx, "events.epoch")
	if err != nil {
		t.Fatalf("GetMeta failed: %v", err)
	}
	if got != "e2" {
		t.Errorf("GetMeta after overwrite = %q, want e2", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func instanceIDs(insts []model.Instance) []string {
	ids := make([]string, 0, len(insts))
	for _, i := range insts {
		ids = append(ids, i.ID)
	}
	return ids
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
