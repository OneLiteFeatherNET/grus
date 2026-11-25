package controller

import (
	"errors"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

func privateSpec(quota int, idle time.Duration) model.PoolSpec {
	s := dynamicSpec(0, 0)
	s.Scaling.Capacity = 0
	s.Private = &model.Private{QuotaPerOwner: quota, IdleTimeout: model.Duration(idle)}
	return s
}

func TestPrivatePoolIsNotAutoscaled(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, time.Minute))

	f.reconcile()

	if n := len(f.drv.Started()); n != 0 {
		t.Fatalf("started instances = %d, want 0 for a private pool without requests", n)
	}
}

func TestCreatePrivateServerStartsOwnedInstance(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, time.Minute))

	inst, created, err := f.c.CreatePrivateServer(f.ctx, "home", "alex")
	if err != nil || !created {
		t.Fatalf("create = (%v, created %v), want a new instance", err, created)
	}
	if got := inst.Labels[model.LabelOwner]; got != "alex" {
		t.Errorf("owner label = %q, want alex", got)
	}
	if n := len(f.drv.Started()); n != 1 {
		t.Errorf("started instances = %d, want 1", n)
	}
}

func TestCreatePrivateServerReturnsTheExistingInstance(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, time.Minute))
	first, _, _ := f.c.CreatePrivateServer(f.ctx, "home", "alex")

	again, created, err := f.c.CreatePrivateServer(f.ctx, "home", "alex")

	if err != nil || created || again.ID != first.ID {
		t.Fatalf("second create = (%s, created %v, %v), want the first instance %s", again.ID, created, err, first.ID)
	}
	if n := len(f.drv.Started()); n != 1 {
		t.Errorf("started instances = %d, want 1", n)
	}
}

func TestCreatePrivateServerRejectsOwnerAtQuotaWhileStopping(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, time.Minute))
	first, _, _ := f.c.CreatePrivateServer(f.ctx, "home", "alex")
	f.bringUp(first.ID, "10.0.0.1:25565")
	if _, err := f.c.StopPrivateServer(f.ctx, first.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}

	_, _, err := f.c.CreatePrivateServer(f.ctx, "home", "alex")

	if !errors.Is(err, ErrQuota) {
		t.Fatalf("create while the old instance drains = %v, want ErrQuota", err)
	}
}

func TestCreatePrivateServerQuotasAreOwnerSpecific(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, time.Minute))
	_, _, _ = f.c.CreatePrivateServer(f.ctx, "home", "alex")

	if _, created, err := f.c.CreatePrivateServer(f.ctx, "home", "sam"); err != nil || !created {
		t.Fatalf("other owner = (created %v, %v), want a new instance", created, err)
	}
}

func TestCreatePrivateServerNeedsAPrivatePool(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(0, 0))

	_, _, err := f.c.CreatePrivateServer(f.ctx, "lobby", "alex")

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("create in a normal pool = %v, want a validation error", err)
	}
}

func TestCreatePrivateServerUnknownPoolAndBadOwner(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, time.Minute))

	if _, _, err := f.c.CreatePrivateServer(f.ctx, "nope", "alex"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown pool = %v, want ErrNotFound", err)
	}
	var ve *ValidationError
	if _, _, err := f.c.CreatePrivateServer(f.ctx, "home", "a b/c"); !errors.As(err, &ve) {
		t.Errorf("bad owner = %v, want a validation error", err)
	}
}

func TestCreatePrivateServerRespectsPoolMax(t *testing.T) {
	f := newFixture(t, Config{})
	spec := privateSpec(1, time.Minute)
	spec.Scaling.Max = 1
	f.putPool("home", spec)
	_, _, _ = f.c.CreatePrivateServer(f.ctx, "home", "alex")

	if _, _, err := f.c.CreatePrivateServer(f.ctx, "home", "sam"); !errors.Is(err, ErrLimit) {
		t.Fatalf("create above scaling.max = %v, want ErrLimit", err)
	}
}

func TestListPrivateServersFiltersByOwner(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, time.Minute))
	f.putPool("lobby", dynamicSpec(1, 0))
	f.reconcile()
	_, _, _ = f.c.CreatePrivateServer(f.ctx, "home", "alex")
	_, _, _ = f.c.CreatePrivateServer(f.ctx, "home", "sam")

	all, _ := f.c.ListPrivateServers(f.ctx, "")
	mine, _ := f.c.ListPrivateServers(f.ctx, "alex")

	if len(all) != 2 || len(mine) != 1 || mine[0].Labels[model.LabelOwner] != "alex" {
		t.Fatalf("list = %d all, %d of alex; want 2 and 1 (lobby instances are not private)", len(all), len(mine))
	}
}

func TestStopPrivateServerDrainsAndRejectsNormalInstances(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, time.Minute))
	f.putPool("lobby", dynamicSpec(1, 0))
	f.reconcile()
	lobbyID := f.instancesOf("lobby")[0].ID
	priv, _, _ := f.c.CreatePrivateServer(f.ctx, "home", "alex")
	f.bringUp(priv.ID, "10.0.0.1:25565")

	got, err := f.c.StopPrivateServer(f.ctx, priv.ID)
	if err != nil || got.State != model.StateDraining {
		t.Fatalf("stop = (%s, %v), want Draining", got.State, err)
	}
	if _, err := f.c.StopPrivateServer(f.ctx, lobbyID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stop of a non-private instance = %v, want ErrNotFound", err)
	}
}

func TestIdlePrivateInstanceStopsAfterTimeout(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, 5*time.Minute))
	inst, _, _ := f.c.CreatePrivateServer(f.ctx, "home", "alex")
	f.bringUp(inst.ID, "10.0.0.1:25565")

	f.advance(4 * time.Minute)
	if got := f.get(inst.ID).State; got != model.StateReady {
		t.Fatalf("state before the timeout = %s, want Ready", got)
	}
	f.advance(2 * time.Minute)

	if got := f.get(inst.ID).State; got != model.StateStopping {
		t.Fatalf("state after the timeout = %s, want Stopping", got)
	}
	if n := f.pub.count(model.EventPrivateIdleStop); n != 1 {
		t.Errorf("private.idle-stop events = %d, want 1", n)
	}
	var found bool
	for i, typ := range f.pub.types {
		if typ == model.EventPrivateIdleStop {
			d := f.pub.data[i].(model.PrivateIdleStopData)
			found = d.Owner == "alex" && d.Instance == inst.ID && d.Pool == "home"
		}
	}
	if !found {
		t.Errorf("private.idle-stop payload does not name pool, instance and owner")
	}
}

func TestPlayersResetTheIdleTimer(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, 5*time.Minute))
	inst, _, _ := f.c.CreatePrivateServer(f.ctx, "home", "alex")
	f.bringUp(inst.ID, "10.0.0.1:25565")

	f.advance(4 * time.Minute)
	_, _ = f.c.SelfStatus(f.ctx, inst.ID, StatusUpdate{Players: 1})
	f.advance(10 * time.Second)
	_, _ = f.c.SelfStatus(f.ctx, inst.ID, StatusUpdate{Players: 0})
	f.advance(4 * time.Minute)

	if got := f.get(inst.ID).State; got != model.StateReady {
		t.Fatalf("state = %s, want Ready: the idle period restarted when the player left", got)
	}
	f.advance(2 * time.Minute)
	if got := f.get(inst.ID).State; got != model.StateStopping {
		t.Fatalf("state = %s, want Stopping after a full idle period", got)
	}
}

func TestInstanceLabelsFromStatusKeepTheOwner(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("home", privateSpec(1, time.Minute))
	inst, _, _ := f.c.CreatePrivateServer(f.ctx, "home", "alex")
	f.drv.SetPhase(inst.ID, driver.PhaseRunning, "10.0.0.1:25565")
	f.reconcile()
	_, _ = f.c.SelfReady(f.ctx, inst.ID)

	got, err := f.c.SelfStatus(f.ctx, inst.ID, StatusUpdate{Labels: map[string]string{"map": "x", model.LabelOwner: "mallory"}})

	if err != nil || got.Labels[model.LabelOwner] != "alex" || got.Labels["map"] != "x" {
		t.Fatalf("labels = %v (%v), want owner alex kept and map added", got.Labels, err)
	}
}
