package controller

import (
	"errors"
	"reflect"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

func paperEnv() model.EnvironmentSpec {
	return model.EnvironmentSpec{
		Kind: model.KindJVM, Image: "registry.test/paper:1", Port: 25570,
		JVM: &model.JVM{Jar: "paper.jar"}, Env: map[string]string{"EULA": "true", "TIER": "env"},
		StopCommand: "stop",
	}
}

func v2Spec() model.PoolSpec {
	s := dynamicSpec(1, 0)
	s.Driver, s.Image = "", ""
	s.Placement = model.Placement{Driver: "kubernetes"}
	s.Environment = "paper"
	s.Groups = []string{"base"}
	s.Env = map[string]string{"TIER": "pool"}
	return s
}

func (f *fixture) putEnv(name string, spec model.EnvironmentSpec) {
	f.t.Helper()
	if _, err := f.c.PutEnvironment(f.ctx, name, spec); err != nil {
		f.t.Fatalf("put environment %s: %v", name, err)
	}
}

func (f *fixture) putGroup(name string, spec model.GroupSpec) {
	f.t.Helper()
	if _, err := f.c.PutGroup(f.ctx, name, spec); err != nil {
		f.t.Fatalf("put group %s: %v", name, err)
	}
}

func TestPoolWithEnvironmentAndGroupStartsWithTheEffectiveSpec(t *testing.T) {
	f := newFixture(t, Config{})
	f.putEnv("paper", paperEnv())
	f.putGroup("base", model.GroupSpec{Env: map[string]string{"REGION": "fr"}, Labels: map[string]string{"team": "olf"}})
	f.putPool("lobby", v2Spec())

	f.reconcile()

	started := f.drv.Started()
	if len(started) != 1 {
		t.Fatalf("started = %d, want 1", len(started))
	}
	eff := started[0].Effective
	if eff.Image != "registry.test/paper:1" || eff.Port != 25570 || eff.StopCommand != "stop" {
		t.Errorf("effective = %+v, want image, port and stop command from the environment", eff)
	}
	wantEnv := map[string]string{"EULA": "true", "TIER": "pool", "REGION": "fr"}
	if !reflect.DeepEqual(eff.Env, wantEnv) {
		t.Errorf("effective env = %v, want %v", eff.Env, wantEnv)
	}
	if eff.Labels["team"] != "olf" {
		t.Errorf("effective labels = %v, want team=olf from the group", eff.Labels)
	}
	if got := started[0].Env["GRUS_PORT"]; got != "25570" {
		t.Errorf("GRUS_PORT = %q, want 25570 from the environment", got)
	}
	if got := started[0].Env["REGION"]; got != "fr" {
		t.Errorf("instance env REGION = %q, want fr from the group", got)
	}
}

func TestPoolWithUnknownEnvironmentStartsNothingAndReportsOnce(t *testing.T) {
	f := newFixture(t, Config{})
	f.putGroup("base", model.GroupSpec{})
	f.putPool("lobby", v2Spec())

	f.reconcile()
	f.reconcile()

	if n := len(f.drv.Started()); n != 0 {
		t.Fatalf("started = %d, want 0 while the environment is missing", n)
	}
	if n := f.pub.count(model.EventPoolCondition); n != 1 {
		t.Errorf("pool.condition events = %d, want 1 (reported once per reason)", n)
	}

	f.putEnv("paper", paperEnv())
	f.reconcile()
	if n := len(f.drv.Started()); n != 1 {
		t.Errorf("started = %d after creating the environment, want 1", n)
	}
}

func TestPoolWithUnknownGroupStartsNothing(t *testing.T) {
	f := newFixture(t, Config{})
	f.putEnv("paper", paperEnv())
	f.putPool("lobby", v2Spec())

	f.reconcile()

	if n := len(f.drv.Started()); n != 0 {
		t.Fatalf("started = %d, want 0 while group base is missing", n)
	}
	if n := f.pub.count(model.EventPoolCondition); n != 1 {
		t.Errorf("pool.condition events = %d, want 1", n)
	}
}

func TestV1PoolStillStartsWithItsInlineFields(t *testing.T) {
	f := newFixture(t, Config{})
	spec := dynamicSpec(1, 0)
	spec.Port = 25999
	f.putPool("lobby", spec)

	f.reconcile()

	started := f.drv.Started()
	if len(started) != 1 || started[0].Effective.Image != "registry.test/lobby:1" || started[0].Env["GRUS_PORT"] != "25999" {
		t.Fatalf("v1 pool did not start with its inline fields: %+v", started)
	}
}

func TestAPICannotChangeOrDeleteKubernetesManagedObjects(t *testing.T) {
	f := newFixture(t, Config{})
	if _, err := f.c.ApplyPool(f.ctx, model.SourceKubernetes, "lobby", dynamicSpec(1, 0)); err != nil {
		t.Fatalf("apply from kubernetes: %v", err)
	}
	if _, err := f.c.ApplyEnvironment(f.ctx, model.SourceKubernetes, "paper", paperEnv()); err != nil {
		t.Fatalf("apply environment from kubernetes: %v", err)
	}
	if _, err := f.c.ApplyGroup(f.ctx, model.SourceKubernetes, "base", model.GroupSpec{}); err != nil {
		t.Fatalf("apply group from kubernetes: %v", err)
	}

	checks := map[string]error{
		"put pool":           second(f.c.PutPool(f.ctx, "lobby", dynamicSpec(2, 0))),
		"delete pool":        f.c.DeletePool(f.ctx, "lobby", false),
		"put environment":    second(f.c.PutEnvironment(f.ctx, "paper", paperEnv())),
		"delete environment": f.c.DeleteEnvironment(f.ctx, "paper"),
		"put group":          second(f.c.PutGroup(f.ctx, "base", model.GroupSpec{})),
		"delete group":       f.c.DeleteGroup(f.ctx, "base"),
		"file source":        second(f.c.ApplyPool(f.ctx, model.SourceFile, "lobby", dynamicSpec(2, 0))),
	}
	for name, err := range checks {
		if !errors.Is(err, ErrManagedElsewhere) {
			t.Errorf("%s: error = %v, want ErrManagedElsewhere", name, err)
		}
	}
	if p, _ := f.st.GetPool(f.ctx, "lobby"); p.Spec.Scaling.Min != 1 || p.Source != model.SourceKubernetes {
		t.Errorf("pool changed despite the rejection: %+v", p)
	}

	if _, err := f.c.ApplyPool(f.ctx, model.SourceKubernetes, "lobby", dynamicSpec(2, 0)); err != nil {
		t.Errorf("kubernetes source updating its own pool: %v", err)
	}
	if err := f.c.RemoveEnvironment(f.ctx, model.SourceKubernetes, "paper"); err != nil {
		t.Errorf("kubernetes source removing its own environment: %v", err)
	}
	if err := f.c.RemoveGroup(f.ctx, model.SourceKubernetes, "base"); err != nil {
		t.Errorf("kubernetes source removing its own group: %v", err)
	}
	if err := f.c.RemovePool(f.ctx, model.SourceKubernetes, "lobby", false); err != nil {
		t.Errorf("kubernetes source removing its own pool: %v", err)
	}
}

func TestAPIObjectsAreMutableThroughTheAPI(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 0))
	f.putEnv("paper", paperEnv())

	if _, err := f.c.PutPool(f.ctx, "lobby", dynamicSpec(2, 0)); err != nil {
		t.Errorf("put own pool: %v", err)
	}
	if err := f.c.DeleteEnvironment(f.ctx, "paper"); err != nil {
		t.Errorf("delete own environment: %v", err)
	}
	if _, err := f.c.GetEnvironment(f.ctx, "paper"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get deleted environment = %v, want ErrNotFound", err)
	}
	if err := f.c.DeleteGroup(f.ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing group = %v, want ErrNotFound", err)
	}
}

func TestPutEnvironmentAndGroupRejectInvalidSpecs(t *testing.T) {
	f := newFixture(t, Config{})
	var ve *ValidationError
	if _, err := f.c.PutEnvironment(f.ctx, "paper", model.EnvironmentSpec{Kind: "wasm"}); !errors.As(err, &ve) || ve.Kind != "environment" {
		t.Errorf("invalid environment error = %v, want ValidationError of kind environment", err)
	}
	bad := model.GroupSpec{Templates: []model.TemplateRef{{Name: "x"}}}
	if _, err := f.c.PutGroup(f.ctx, "Bad_Name", bad); !errors.As(err, &ve) || len(ve.Fields) != 2 {
		t.Errorf("invalid group error = %v, want name and template field errors", err)
	}
}

func proxySpec() model.PoolSpec {
	s := dynamicSpec(2, 0)
	s.Role = model.RoleProxy
	return s
}

func (f *fixture) networkEvents() []model.NetworkStatus {
	var out []model.NetworkStatus
	for i, typ := range f.pub.types {
		if typ == model.EventNetworkPlayers {
			out = append(out, f.pub.data[i].(model.NetworkStatus))
		}
	}
	return out
}

func TestNetworkSumsPlayersOfReadyAndAllocatedProxies(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("proxy", proxySpec())
	f.putPool("lobby", dynamicSpec(1, 0))
	f.reconcile()
	for _, s := range f.drv.Started() {
		f.bringUp(s.Instance.ID, "10.0.0.1:25565")
	}
	var proxies []string
	for _, inst := range f.instancesOf("proxy") {
		proxies = append(proxies, inst.ID)
	}
	f.c.SelfStatus(f.ctx, proxies[0], StatusUpdate{Players: 7, MaxPlayers: 100})
	f.c.SelfStatus(f.ctx, proxies[1], StatusUpdate{Players: 5, MaxPlayers: 100})
	for _, inst := range f.instancesOf("lobby") {
		f.c.SelfStatus(f.ctx, inst.ID, StatusUpdate{Players: 99})
	}

	got, err := f.c.Network(f.ctx)
	if err != nil {
		t.Fatalf("Network: %v", err)
	}
	if want := (model.NetworkStatus{Players: 12, MaxPlayers: 200, Proxies: 2}); got != want {
		t.Errorf("network = %+v, want %+v (game instances do not count)", got, want)
	}
}

func TestNetworkPlayersEventIsPublishedOnlyOnChange(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("proxy", proxySpec())
	f.reconcile()
	var ids []string
	for _, s := range f.drv.Started() {
		f.bringUp(s.Instance.ID, "10.0.0.1:25565")
		ids = append(ids, s.Instance.ID)
	}
	base := len(f.networkEvents())

	f.c.SelfStatus(f.ctx, ids[0], StatusUpdate{Players: 3, MaxPlayers: 50})
	f.c.SelfStatus(f.ctx, ids[0], StatusUpdate{Players: 3, MaxPlayers: 50})
	f.c.SelfStatus(f.ctx, ids[1], StatusUpdate{Players: 4, MaxPlayers: 50})

	events := f.networkEvents()
	if len(events)-base != 2 {
		t.Fatalf("network.players events = %d, want 2 (unchanged status must not publish)", len(events)-base)
	}
	if last := events[len(events)-1]; last.Players != 7 || last.Proxies != 2 {
		t.Errorf("last event = %+v, want 7 players on 2 proxies", last)
	}
}

func TestNetworkPlayersEventFollowsProxyStateChanges(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("proxy", proxySpec())
	f.reconcile()
	var ids []string
	for _, s := range f.drv.Started() {
		f.bringUp(s.Instance.ID, "10.0.0.1:25565")
		ids = append(ids, s.Instance.ID)
	}
	f.c.SelfStatus(f.ctx, ids[0], StatusUpdate{Players: 3, MaxPlayers: 50})
	f.c.SelfStatus(f.ctx, ids[1], StatusUpdate{Players: 4, MaxPlayers: 50})

	if _, err := f.c.StopInstance(f.ctx, ids[0], StopOptions{Force: true}); err != nil {
		t.Fatalf("stop: %v", err)
	}

	events := f.networkEvents()
	if last := events[len(events)-1]; last.Players != 4 || last.Proxies != 1 {
		t.Errorf("last event = %+v, want 4 players on 1 proxy after a proxy stopped", last)
	}
}

func second[T any](_ T, err error) error { return err }
