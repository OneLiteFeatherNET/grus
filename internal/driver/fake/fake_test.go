package fake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

func spec(id string) driver.StartSpec {
	return driver.StartSpec{
		Instance: model.Instance{ID: id, Pool: "lobby", Role: model.RoleGame, PoolGeneration: 3, TokenHash: "hash-" + id},
		Pool:     model.Pool{Name: "lobby", Spec: model.PoolSpec{Mode: model.ModeDynamic}},
		Token:    "plain-token",
		Env:      map[string]string{"GRUS_POOL": "lobby"},
	}
}

func observed(t *testing.T, d *Driver, ref string) driver.Observed {
	t.Helper()
	list, err := d.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, o := range list {
		if o.Ref == ref {
			return o
		}
	}
	t.Fatalf("instance %q not listed", ref)
	return driver.Observed{}
}

func TestStartReturnsInstanceIDAsRef(t *testing.T) {
	d := New("local")

	ref, err := d.Start(context.Background(), spec("inst-1"))
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ref != "inst-1" {
		t.Errorf("ref = %q, want the instance id inst-1", ref)
	}
}

func TestStartCreatesPendingInstanceWithoutAddress(t *testing.T) {
	d := New("local")
	ref, _ := d.Start(context.Background(), spec("inst-1"))

	o := observed(t, d, ref)
	if o.Phase != driver.PhasePending {
		t.Errorf("phase = %q, want %q", o.Phase, driver.PhasePending)
	}
	if o.Address != "" {
		t.Errorf("address = %q, want empty until the runtime reports one", o.Address)
	}
	if o.Pool != "lobby" || o.PoolGeneration != 3 || o.TokenHash != "hash-inst-1" || o.Mode != model.ModeDynamic {
		t.Errorf("observed fields not taken from spec: %+v", o)
	}
}

func TestStartGeneratesRefWhenSpecHasNoID(t *testing.T) {
	d := New("local")

	ref, err := d.Start(context.Background(), driver.StartSpec{})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if ref == "" {
		t.Error("ref is empty, want a generated id")
	}
}

func TestStartFailsWithStartErrAndRecordsNothing(t *testing.T) {
	d := New("local")
	want := errors.New("image pull failed")
	d.StartErr = want

	_, err := d.Start(context.Background(), spec("inst-1"))
	if !errors.Is(err, want) {
		t.Fatalf("Start error = %v, want %v", err, want)
	}
	if got := d.Started(); len(got) != 0 {
		t.Errorf("Started() has %d entries after failed start, want 0", len(got))
	}
	if list, _ := d.List(context.Background()); len(list) != 0 {
		t.Errorf("List() has %d entries after failed start, want 0", len(list))
	}
}

func TestSetPhaseUpdatesPhaseAndAddress(t *testing.T) {
	d := New("local")
	ref, _ := d.Start(context.Background(), spec("inst-1"))

	d.SetPhase(ref, driver.PhaseRunning, "10.0.0.5:25565")

	o := observed(t, d, ref)
	if o.Phase != driver.PhaseRunning || o.Address != "10.0.0.5:25565" {
		t.Errorf("after SetPhase: phase=%q address=%q, want Running and 10.0.0.5:25565", o.Phase, o.Address)
	}
}

func TestRemoveDropsInstanceFromList(t *testing.T) {
	d := New("local")
	ref, _ := d.Start(context.Background(), spec("inst-1"))

	d.Remove(ref)

	if list, _ := d.List(context.Background()); len(list) != 0 {
		t.Errorf("List() has %d entries after Remove, want 0", len(list))
	}
}

func TestAddMakesPreExistingInstanceListable(t *testing.T) {
	d := New("local")

	d.Add(driver.Observed{InstanceID: "adopted-1", Phase: driver.PhaseRunning, Address: "10.0.0.7:25565"})

	o := observed(t, d, "adopted-1")
	if o.Phase != driver.PhaseRunning || o.Address != "10.0.0.7:25565" {
		t.Errorf("adopted instance = %+v, want Running at 10.0.0.7:25565", o)
	}
}

func TestStartedReturnsCopyOfSpec(t *testing.T) {
	d := New("local")
	_, _ = d.Start(context.Background(), spec("inst-1"))

	got := d.Started()
	got[0].Env["GRUS_POOL"] = "changed"

	if again := d.Started(); again[0].Env["GRUS_POOL"] != "lobby" {
		t.Errorf("mutating the returned spec changed the driver's record to %q", again[0].Env["GRUS_POOL"])
	}
}

func TestStopOnlyRecordsRequestAndKeepsPhase(t *testing.T) {
	d := New("local")
	ref, _ := d.Start(context.Background(), spec("inst-1"))
	d.SetPhase(ref, driver.PhaseRunning, "10.0.0.5:25565")

	if err := d.Stop(context.Background(), ref, "stop", 0); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if got := d.Stopped(); len(got) != 1 || got[0] != ref {
		t.Errorf("Stopped() = %v, want [%s]", got, ref)
	}
	if o := observed(t, d, ref); o.Phase != driver.PhaseRunning {
		t.Errorf("phase after Stop = %q, want Running (tests change phases explicitly)", o.Phase)
	}
}

func TestKillOnlyRecordsRequest(t *testing.T) {
	d := New("local")
	ref, _ := d.Start(context.Background(), spec("inst-1"))

	if err := d.Kill(context.Background(), ref); err != nil {
		t.Fatalf("Kill returned error: %v", err)
	}

	if got := d.Killed(); len(got) != 1 || got[0] != ref {
		t.Errorf("Killed() = %v, want [%s]", got, ref)
	}
}

func TestStopUnknownRefReturnsNotFound(t *testing.T) {
	d := New("local")

	err := d.Stop(context.Background(), "missing", "stop", 0)
	if !errors.Is(err, driver.ErrNotFound) {
		t.Errorf("Stop error = %v, want driver.ErrNotFound", err)
	}
}

func TestCommandsAreRecordedPerInstance(t *testing.T) {
	d := New("local")
	ref, _ := d.Start(context.Background(), spec("inst-1"))

	_ = d.Command(context.Background(), ref, "say hello")
	_ = d.Command(context.Background(), ref, "list")

	got := d.Commands()[ref]
	if len(got) != 2 || got[0] != "say hello" || got[1] != "list" {
		t.Errorf("Commands()[%s] = %v, want [say hello list]", ref, got)
	}
}

func TestLogsReturnLastLinesOfSetLogs(t *testing.T) {
	d := New("local")
	ref, _ := d.Start(context.Background(), spec("inst-1"))
	d.SetLogs(ref, []string{"one", "two", "three"})

	rc, err := d.Logs(context.Background(), ref, 2, false)
	if err != nil {
		t.Fatalf("Logs returned error: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading logs: %v", err)
	}
	if want := "two\nthree\n"; string(body) != want {
		t.Errorf("tail 2 = %q, want %q", body, want)
	}
}

func TestConcurrentStartsAreAllRecorded(t *testing.T) {
	d := New("local")
	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := d.Start(context.Background(), spec(fmt.Sprintf("inst-%d", i))); err != nil {
				t.Errorf("Start %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	if got := len(d.Started()); got != n {
		t.Errorf("Started() has %d entries, want %d", got, n)
	}
}
