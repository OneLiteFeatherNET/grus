package crdsync

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

const ns = "grus"

var epoch = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeBackend is an in-memory Backend. It rejects a pool without role like the real validation does and keeps
// the ownership rule of the controller: a kubernetes object cannot be changed by another source.
type fakeBackend struct {
	mu        sync.Mutex
	pools     map[string]model.Pool
	envs      map[string]model.Environment
	groups    map[string]model.Group
	instances []model.Instance
	removed   []string
	drained   map[string]bool
	changed   chan struct{}
}

func newBackend() *fakeBackend {
	return &fakeBackend{
		pools: map[string]model.Pool{}, envs: map[string]model.Environment{}, groups: map[string]model.Group{},
		drained: map[string]bool{}, changed: make(chan struct{}, 100),
	}
}

func (b *fakeBackend) signal() {
	select {
	case b.changed <- struct{}{}:
	default:
	}
}

func (b *fakeBackend) ApplyPool(_ context.Context, src model.Source, name string, spec model.PoolSpec) (model.Pool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.signal()
	if spec.Role == "" {
		return model.Pool{}, &controller.ValidationError{Fields: []model.FieldError{{Field: "role", Message: "must be proxy, game or other"}, {Field: "mode", Message: "must be dynamic or static"}}}
	}
	if cur, ok := b.pools[name]; ok && cur.Source == model.SourceKubernetes && src != model.SourceKubernetes {
		return model.Pool{}, fmt.Errorf("%w", controller.ErrManagedElsewhere)
	}
	p := model.Pool{Name: name, Spec: spec, Source: src}
	b.pools[name] = p
	return p, nil
}

func (b *fakeBackend) RemovePool(_ context.Context, _ model.Source, name string, drain bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.signal()
	delete(b.pools, name)
	b.removed = append(b.removed, "pool/"+name)
	b.drained[name] = drain
	return nil
}

func (b *fakeBackend) ApplyEnvironment(_ context.Context, src model.Source, name string, spec model.EnvironmentSpec) (model.Environment, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.signal()
	e := model.Environment{Name: name, Spec: spec, Source: src}
	b.envs[name] = e
	return e, nil
}

func (b *fakeBackend) RemoveEnvironment(_ context.Context, _ model.Source, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.signal()
	delete(b.envs, name)
	b.removed = append(b.removed, "environment/"+name)
	return nil
}

func (b *fakeBackend) ApplyGroup(_ context.Context, src model.Source, name string, spec model.GroupSpec) (model.Group, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.signal()
	g := model.Group{Name: name, Spec: spec, Source: src}
	b.groups[name] = g
	return g, nil
}

func (b *fakeBackend) RemoveGroup(_ context.Context, _ model.Source, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer b.signal()
	delete(b.groups, name)
	b.removed = append(b.removed, "group/"+name)
	return nil
}

func (b *fakeBackend) ListPools(context.Context) ([]model.Pool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []model.Pool
	for _, p := range b.pools {
		out = append(out, p)
	}
	return out, nil
}

func (b *fakeBackend) ListEnvironments(context.Context) ([]model.Environment, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []model.Environment
	for _, e := range b.envs {
		out = append(out, e)
	}
	return out, nil
}

func (b *fakeBackend) ListGroups(context.Context) ([]model.Group, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []model.Group
	for _, g := range b.groups {
		out = append(out, g)
	}
	return out, nil
}

func (b *fakeBackend) ListInstances(_ context.Context, f store.InstanceFilter) ([]model.Instance, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []model.Instance
	for _, i := range b.instances {
		if f.Pool == "" || i.Pool == f.Pool {
			out = append(out, i)
		}
	}
	return out, nil
}

func (b *fakeBackend) setInstances(i ...model.Instance) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.instances = i
}

func (b *fakeBackend) pool(name string) (model.Pool, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.pools[name]
	return p, ok
}

func cr(kind, name string, generation int64, spec map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": Group + "/" + Version,
		"kind":       kind,
		"metadata":   map[string]any{"name": name, "namespace": ns, "generation": generation},
		"spec":       spec,
	}}
}

func poolCR(name string, min int64) *unstructured.Unstructured {
	return cr("Pool", name, 1, map[string]any{
		"role": "game", "mode": "dynamic", "environment": "minestom",
		"placement": map[string]any{"driver": "kubernetes"},
		"resources": map[string]any{"memoryMiB": int64(512)},
		"scaling":   map[string]any{"min": min, "max": int64(4)},
	})
}

func newClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	lists := map[schema.GroupVersionResource]string{}
	for _, k := range kinds {
		lists[k.gvr] = k.kind + "List"
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, lists, objs...)
}

func startSyncer(t *testing.T, client *dynamicfake.FakeDynamicClient, b *fakeBackend, clock model.Clock) *Syncer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := New(client, b, Config{Namespace: ns, Clock: clock, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), StatusInterval: time.Minute})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

func getCR(t *testing.T, client *dynamicfake.FakeDynamicClient, kind, name string) *unstructured.Unstructured {
	t.Helper()
	for _, k := range kinds {
		if k.kind == kind {
			u, err := client.Resource(k.gvr).Namespace(ns).Get(context.Background(), name, metaGet)
			if err != nil {
				t.Fatalf("get %s/%s: %v", kind, name, err)
			}
			return u
		}
	}
	t.Fatalf("unknown kind %s", kind)
	return nil
}

func readyCondition(t *testing.T, u *unstructured.Unstructured) map[string]any {
	t.Helper()
	conds, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	if !found || len(conds) != 1 {
		t.Fatalf("status.conditions = %v, want one Ready condition", conds)
	}
	return conds[0].(map[string]any)
}

func waitFor(t *testing.T, b *fakeBackend, cond func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !cond() {
		select {
		case <-b.changed:
		case <-deadline:
			t.Fatal("timed out waiting for the backend to change")
		}
	}
}

func TestStartAppliesEveryCustomResourceWithSourceKubernetes(t *testing.T) {
	client := newClient(
		cr("Environment", "minestom", 1, map[string]any{"kind": "jvm", "jvm": map[string]any{"jar": "server.jar"}}),
		cr("Group", "lobby", 1, map[string]any{"labels": map[string]any{"network": "olf"}}),
		poolCR("lobby", 1),
	)
	b := newBackend()
	startSyncer(t, client, b, &fakeClock{now: epoch})

	if e := b.envs["minestom"]; e.Source != model.SourceKubernetes || e.Spec.JVM == nil || e.Spec.JVM.Jar != "server.jar" {
		t.Errorf("environment = %+v", e)
	}
	if g := b.groups["lobby"]; g.Source != model.SourceKubernetes || g.Spec.Labels["network"] != "olf" {
		t.Errorf("group = %+v", g)
	}
	p := b.pools["lobby"]
	if p.Source != model.SourceKubernetes || p.Spec.Scaling.Max != 4 || p.Spec.EffectiveDriver() != model.DriverKubernetes {
		t.Errorf("pool = %+v", p)
	}
}

func TestStartRemovesKubernetesObjectsWithoutCustomResourceButKeepsApiObjects(t *testing.T) {
	b := newBackend()
	b.pools["stale"] = model.Pool{Name: "stale", Source: model.SourceKubernetes}
	b.pools["manual"] = model.Pool{Name: "manual", Source: model.SourceAPI}
	b.envs["stale"] = model.Environment{Name: "stale", Source: model.SourceKubernetes}
	b.groups["stale"] = model.Group{Name: "stale", Source: model.SourceKubernetes}
	startSyncer(t, newClient(), b, &fakeClock{now: epoch})

	if _, ok := b.pools["stale"]; ok {
		t.Error("stale kubernetes pool was kept")
	}
	if !b.drained["stale"] {
		t.Error("stale pool was removed without draining")
	}
	if _, ok := b.pools["manual"]; !ok {
		t.Error("api pool was removed")
	}
	if len(b.envs) != 0 || len(b.groups) != 0 {
		t.Errorf("stale environment or group kept: %v %v", b.envs, b.groups)
	}
}

func TestInvalidSpecIsReportedAsReadyFalseInvalid(t *testing.T) {
	client := newClient(cr("Pool", "broken", 3, map[string]any{"resources": map[string]any{"memoryMiB": int64(512)}}))
	b := newBackend()
	startSyncer(t, client, b, &fakeClock{now: epoch})

	cond := readyCondition(t, getCR(t, client, "Pool", "broken"))
	if cond["status"] != "False" || cond["reason"] != ReasonInvalid {
		t.Fatalf("condition = %v, want Ready=False Invalid", cond)
	}
	want := "role: must be proxy, game or other; mode: must be dynamic or static"
	if cond["message"] != want {
		t.Errorf("message = %q, want %q", cond["message"], want)
	}
	if cond["observedGeneration"] != int64(3) {
		t.Errorf("observedGeneration = %v, want 3", cond["observedGeneration"])
	}
	if _, ok := b.pool("broken"); ok {
		t.Error("invalid pool was stored")
	}
}

func TestSpecWithWrongFieldTypeIsReportedAsInvalid(t *testing.T) {
	client := newClient(cr("Pool", "typo", 1, map[string]any{"scaling": map[string]any{"min": "many"}}))
	startSyncer(t, client, newBackend(), &fakeClock{now: epoch})

	cond := readyCondition(t, getCR(t, client, "Pool", "typo"))
	if cond["reason"] != ReasonInvalid {
		t.Errorf("reason = %v, want Invalid; condition %v", cond["reason"], cond)
	}
}

func TestPoolStatusCountsInstancesAndBecomesReadyAtMin(t *testing.T) {
	client := newClient(poolCR("lobby", 2))
	b := newBackend()
	b.setInstances(
		model.Instance{Pool: "lobby", State: model.StateReady},
		model.Instance{Pool: "lobby", State: model.StateAllocated},
		model.Instance{Pool: "lobby", State: model.StateStarting},
		model.Instance{Pool: "lobby", State: model.StateStopped},
		model.Instance{Pool: "other", State: model.StateReady},
	)
	startSyncer(t, client, b, &fakeClock{now: epoch})

	u := getCR(t, client, "Pool", "lobby")
	for field, want := range map[string]int64{"replicas": 3, "readyReplicas": 1, "allocatedReplicas": 1, "observedGeneration": 1} {
		if got, _, _ := unstructured.NestedInt64(u.Object, "status", field); got != want {
			t.Errorf("status.%s = %d, want %d", field, got, want)
		}
	}
	cond := readyCondition(t, u)
	if cond["status"] != "True" || cond["reason"] != ReasonApplied {
		t.Errorf("condition = %v, want Ready=True Applied", cond)
	}
}

func TestPoolIsNotReadyBelowMin(t *testing.T) {
	client := newClient(poolCR("lobby", 2))
	startSyncer(t, client, newBackend(), &fakeClock{now: epoch})

	cond := readyCondition(t, getCR(t, client, "Pool", "lobby"))
	if cond["status"] != "False" || cond["reason"] != ReasonProgressing {
		t.Errorf("condition = %v, want Ready=False Progressing", cond)
	}
}

func TestTickDebouncesCountOnlyChanges(t *testing.T) {
	client := newClient(poolCR("lobby", 0))
	b := newBackend()
	clock := &fakeClock{now: epoch}
	s := startSyncer(t, client, b, clock)
	ctx := context.Background()

	b.setInstances(model.Instance{Pool: "lobby", State: model.StateReady})
	clock.advance(10 * time.Second)
	s.Tick(ctx)
	if got, _, _ := unstructured.NestedInt64(getCR(t, client, "Pool", "lobby").Object, "status", "readyReplicas"); got != 0 {
		t.Fatalf("readyReplicas = %d within the debounce window, want the old 0", got)
	}

	clock.advance(time.Minute)
	s.Tick(ctx)
	if got, _, _ := unstructured.NestedInt64(getCR(t, client, "Pool", "lobby").Object, "status", "readyReplicas"); got != 1 {
		t.Fatalf("readyReplicas = %d after the debounce window, want 1", got)
	}
}

func TestLastTransitionTimeSurvivesCountChanges(t *testing.T) {
	client := newClient(poolCR("lobby", 0))
	b := newBackend()
	clock := &fakeClock{now: epoch}
	s := startSyncer(t, client, b, clock)

	first := readyCondition(t, getCR(t, client, "Pool", "lobby"))["lastTransitionTime"]
	b.setInstances(model.Instance{Pool: "lobby", State: model.StateReady})
	clock.advance(2 * time.Minute)
	s.Tick(context.Background())
	if got := readyCondition(t, getCR(t, client, "Pool", "lobby"))["lastTransitionTime"]; got != first {
		t.Errorf("lastTransitionTime = %v, want unchanged %v", got, first)
	}
}

func TestNewCustomResourceIsAppliedAndDeletionDrainsThePool(t *testing.T) {
	client := newClient()
	b := newBackend()
	startSyncer(t, client, b, &fakeClock{now: epoch})
	pools := client.Resource(kinds[2].gvr).Namespace(ns)

	if _, err := pools.Create(context.Background(), poolCR("lobby", 1), metaCreate); err != nil {
		t.Fatalf("create: %v", err)
	}
	waitFor(t, b, func() bool { _, ok := b.pool("lobby"); return ok })

	if err := pools.Delete(context.Background(), "lobby", metaDelete); err != nil {
		t.Fatalf("delete: %v", err)
	}
	waitFor(t, b, func() bool { _, ok := b.pool("lobby"); return !ok })
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.drained["lobby"] {
		t.Error("pool was removed without drain")
	}
}

func TestDeletingACustomResourceKeepsAnApiObjectOfTheSameName(t *testing.T) {
	client := newClient(poolCR("lobby", 1))
	b := newBackend()
	s := startSyncer(t, client, b, &fakeClock{now: epoch})

	b.mu.Lock()
	b.pools["lobby"] = model.Pool{Name: "lobby", Source: model.SourceAPI}
	b.mu.Unlock()
	// Drop the resource from the informer cache, as the watch does when it is deleted.
	inf := s.informers["Pool"].GetStore()
	cached, _, _ := inf.GetByKey(ns + "/lobby")
	if err := inf.Delete(cached); err != nil {
		t.Fatalf("drop from cache: %v", err)
	}
	s.mu.Lock()
	s.reconcile(context.Background(), kinds[2], "lobby")
	s.mu.Unlock()

	if p, ok := b.pool("lobby"); !ok || p.Source != model.SourceAPI {
		t.Errorf("api pool = %+v, %v; want it kept", p, ok)
	}
}

func TestRejectedApplyIsRetriedOnTick(t *testing.T) {
	client := newClient(poolCR("lobby", 1))
	b := newBackend()
	b.pools["lobby"] = model.Pool{Name: "lobby", Source: model.SourceKubernetes}
	s := startSyncer(t, client, b, &fakeClock{now: epoch})
	// A conflicting change by another source cannot happen with this fake; model a transient failure instead.
	s.mu.Lock()
	s.results["Pool/lobby"] = result{reason: ReasonError, message: "boom"}
	s.mu.Unlock()

	s.Tick(context.Background())

	if cond := readyCondition(t, getCR(t, client, "Pool", "lobby")); cond["reason"] == ReasonError {
		t.Errorf("condition = %v, want the retried apply to clear the error", cond)
	}
}
