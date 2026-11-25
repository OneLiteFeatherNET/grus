package autoscale

import (
	"reflect"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

var t0 = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

// dynamicSpec is a dynamic pool with the defaults the controller applies: capacity 20, target 0.8, 5 min delay.
func dynamicSpec(minN, maxN, buffer int) model.PoolSpec {
	return model.PoolSpec{
		Role:   model.RoleGame,
		Mode:   model.ModeDynamic,
		Driver: "kubernetes",
		Scaling: model.Scaling{
			Min:               minN,
			Max:               maxN,
			Buffer:            buffer,
			Capacity:          20,
			TargetUtilization: 0.8,
			ScaleDownDelay:    model.Duration(5 * time.Minute),
			MaxSurge:          2,
		},
	}.WithDefaults()
}

func staticSpec(minN, maxN int) model.PoolSpec {
	return model.PoolSpec{
		Role:    model.RoleGame,
		Mode:    model.ModeStatic,
		Driver:  "kubernetes",
		Scaling: model.Scaling{Min: minN, Max: maxN, Capacity: 20},
	}.WithDefaults()
}

func inst(id string, st model.State, players int, created time.Time) model.Instance {
	return model.Instance{ID: id, State: st, Players: players, CreatedAt: created, Role: model.RoleGame}
}

func at(d time.Duration) *time.Time {
	v := t0.Add(d)
	return &v
}

func TestDecideEmptyPoolStartsMinimumWithMinReason(t *testing.T) {
	d := Decide(PoolView{Name: "lobby", Spec: dynamicSpec(1, 10, 1)}, t0)
	if d.Start != 2 {
		t.Errorf("Start = %d, want 2 (min 1 plus buffer 1)", d.Start)
	}
	if d.Direction != "up" || d.Reason != "min" {
		t.Errorf("Direction/Reason = %q/%q, want up/min", d.Direction, d.Reason)
	}
	if d.Desired != 2 || d.Current != 0 {
		t.Errorf("Desired/Current = %d/%d, want 2/0", d.Desired, d.Current)
	}
	if d.BelowSince != nil {
		t.Errorf("BelowSince = %v, want nil when scaling up", d.BelowSince)
	}
}

func TestDecideBufferAddsInstanceForTwoBusyLobbies(t *testing.T) {
	v := PoolView{
		Name: "lobby",
		Spec: dynamicSpec(1, 10, 1),
		Instances: []model.Instance{
			inst("a", model.StateReady, 18, t0),
			inst("b", model.StateReady, 5, t0),
		},
	}
	d := Decide(v, t0)
	if d.Desired != 3 {
		t.Errorf("Desired = %d, want 3", d.Desired)
	}
	if d.Start != 1 {
		t.Errorf("Start = %d, want 1", d.Start)
	}
	if d.Reason != "buffer" {
		t.Errorf("Reason = %q, want buffer", d.Reason)
	}
	wantInputs := map[string]int{
		"players": 23, "busy": 2, "loadDesired": 2, "bufferDesired": 3,
		"active": 2, "min": 1, "max": 10, "buffer": 1,
	}
	if !reflect.DeepEqual(d.Inputs, wantInputs) {
		t.Errorf("Inputs = %v, want %v", d.Inputs, wantInputs)
	}
}

func TestDecideDoesNothingWhenOneLobbyHasPlayersAndOtherIsEmpty(t *testing.T) {
	v := PoolView{
		Name: "lobby",
		Spec: dynamicSpec(1, 10, 1),
		Instances: []model.Instance{
			inst("a", model.StateReady, 18, t0),
			inst("b", model.StateReady, 0, t0),
		},
	}
	d := Decide(v, t0)
	if d.Desired != 2 || d.Current != 2 {
		t.Errorf("Desired/Current = %d/%d, want 2/2", d.Desired, d.Current)
	}
	if d.Start != 0 || d.Direction != "" || d.Reason != "" {
		t.Errorf("Start/Direction/Reason = %d/%q/%q, want 0/\"\"/\"\"", d.Start, d.Direction, d.Reason)
	}
}

func TestDecideLoadReasonWhenPlayersExceedBusyBuffer(t *testing.T) {
	v := PoolView{
		Name: "lobby",
		Spec: dynamicSpec(1, 10, 0),
		Instances: []model.Instance{
			inst("a", model.StateReady, 20, t0),
			inst("b", model.StateReady, 20, t0),
		},
	}
	d := Decide(v, t0)
	if d.Desired != 3 {
		t.Errorf("Desired = %d, want 3 (40 players / 16 per instance)", d.Desired)
	}
	if d.Start != 1 || d.Reason != "load" {
		t.Errorf("Start/Reason = %d/%q, want 1/load", d.Start, d.Reason)
	}
}

func TestDecideScaleDownStartsTimerOnFirstObservation(t *testing.T) {
	v := PoolView{Name: "lobby", Spec: dynamicSpec(1, 10, 1), Instances: threeEmptyReady()}
	d := Decide(v, t0)
	if d.BelowSince == nil || !d.BelowSince.Equal(t0) {
		t.Fatalf("BelowSince = %v, want %v", d.BelowSince, t0)
	}
	if len(d.Drain) != 0 || d.Direction != "" {
		t.Errorf("Drain/Direction = %v/%q on first observation, want no action", d.Drain, d.Direction)
	}
}

func TestDecideScaleDownKeepsTimerBeforeDelay(t *testing.T) {
	v := PoolView{
		Name:       "lobby",
		Spec:       dynamicSpec(1, 10, 1),
		Instances:  threeEmptyReady(),
		BelowSince: at(0),
	}
	d := Decide(v, t0.Add(4*time.Minute))
	if len(d.Drain) != 0 {
		t.Errorf("Drain = %v before ScaleDownDelay, want none", d.Drain)
	}
	if d.BelowSince == nil || !d.BelowSince.Equal(t0) {
		t.Errorf("BelowSince = %v, want the original %v kept", d.BelowSince, t0)
	}
}

func TestDecideScaleDownDrainsYoungestEmptyReadyAfterDelay(t *testing.T) {
	v := PoolView{
		Name:       "lobby",
		Spec:       dynamicSpec(1, 10, 1),
		Instances:  threeEmptyReady(),
		BelowSince: at(0),
	}
	d := Decide(v, t0.Add(5*time.Minute))
	if !reflect.DeepEqual(d.Drain, []string{"young"}) {
		t.Errorf("Drain = %v, want [young] (the youngest empty Ready instance)", d.Drain)
	}
	if d.Direction != "down" || d.Reason != "idle" {
		t.Errorf("Direction/Reason = %q/%q, want down/idle", d.Direction, d.Reason)
	}
	if d.BelowSince != nil {
		t.Errorf("BelowSince = %v after draining, want nil", d.BelowSince)
	}
}

func TestDecideScaleDownPrefersFewerPlayersOverAge(t *testing.T) {
	instances := []model.Instance{
		inst("old-empty", model.StateReady, 0, t0.Add(-time.Hour)),
		inst("young-busy", model.StateReady, 1, t0),
	}
	v := PoolView{
		Name:       "lobby",
		Spec:       dynamicSpec(0, 10, 0),
		Instances:  instances,
		BelowSince: at(0),
	}
	d := Decide(v, t0.Add(5*time.Minute))
	if !reflect.DeepEqual(d.Drain, []string{"old-empty"}) {
		t.Errorf("Drain = %v, want [old-empty] (fewest players first)", d.Drain)
	}
}

func TestDecideNeverDrainsAllocatedUnhealthyOrStarting(t *testing.T) {
	v := PoolView{
		Name: "lobby",
		Spec: dynamicSpec(0, 10, 0),
		Instances: []model.Instance{
			inst("allocated", model.StateAllocated, 0, t0),
			inst("unhealthy", model.StateUnhealthy, 0, t0),
			inst("starting", model.StateStarting, 0, t0),
		},
		BelowSince: at(0),
	}
	d := Decide(v, t0.Add(10*time.Minute))
	if len(d.Drain) != 0 {
		t.Errorf("Drain = %v, want none: only Ready instances may be drained", d.Drain)
	}
	if d.Direction != "" {
		t.Errorf("Direction = %q, want empty when nothing can be drained", d.Direction)
	}
}

func TestDecideStaticPoolStartsMinimumWithMinReason(t *testing.T) {
	d := Decide(PoolView{Name: "proxy", Spec: staticSpec(1, 3)}, t0)
	if d.Start != 1 || d.Reason != "min" || d.Direction != "up" {
		t.Errorf("Start/Reason/Direction = %d/%q/%q, want 1/min/up", d.Start, d.Reason, d.Direction)
	}
	if d.Desired != 1 {
		t.Errorf("Desired = %d, want 1 (static pools use min)", d.Desired)
	}
}

func TestDecideStaticPoolNeverDrains(t *testing.T) {
	v := PoolView{
		Name: "proxy",
		Spec: staticSpec(1, 3),
		Instances: []model.Instance{
			inst("a", model.StateReady, 0, t0),
			inst("b", model.StateReady, 0, t0),
		},
		BelowSince: at(0),
	}
	d := Decide(v, t0.Add(time.Hour))
	if len(d.Drain) != 0 || d.Direction != "" {
		t.Errorf("Drain/Direction = %v/%q, want none for a static pool", d.Drain, d.Direction)
	}
	if d.BelowSince != nil {
		t.Errorf("BelowSince = %v, want nil for a static pool", d.BelowSince)
	}
}

func TestDecideStartIsCappedByMaxSurge(t *testing.T) {
	d := Decide(PoolView{Name: "lobby", Spec: dynamicSpec(5, 10, 0)}, t0)
	if d.Desired != 5 {
		t.Errorf("Desired = %d, want 5", d.Desired)
	}
	if d.Start != 2 {
		t.Errorf("Start = %d, want 2 (MaxSurge)", d.Start)
	}
}

func TestDecideDesiredIsCappedByMax(t *testing.T) {
	spec := dynamicSpec(0, 3, 5)
	spec.Scaling.MaxSurge = 10
	d := Decide(PoolView{Name: "lobby", Spec: spec}, t0)
	if d.Desired != 3 {
		t.Errorf("Desired = %d, want 3 (max)", d.Desired)
	}
	if d.Start != 3 {
		t.Errorf("Start = %d, want 3", d.Start)
	}
}

func TestDecideMaintenanceDoesNothing(t *testing.T) {
	v := PoolView{
		Name:        "lobby",
		Spec:        dynamicSpec(2, 10, 0),
		Maintenance: true,
		Instances: []model.Instance{
			inst("a", model.StateReady, 0, t0),
			inst("b", model.StateReady, 0, t0),
			inst("c", model.StateReady, 0, t0),
		},
		BelowSince: at(0),
	}
	d := Decide(v, t0.Add(time.Hour))
	if d.Desired != 2 || d.Current != 3 {
		t.Errorf("Desired/Current = %d/%d, want 2/3 (filled even in maintenance)", d.Desired, d.Current)
	}
	if d.Start != 0 || len(d.Drain) != 0 || d.Direction != "" {
		t.Errorf("Start/Drain/Direction = %d/%v/%q, want no action in maintenance", d.Start, d.Drain, d.Direction)
	}
	if d.BelowSince != nil {
		t.Errorf("BelowSince = %v in maintenance, want nil", d.BelowSince)
	}
	if d.Inputs == nil {
		t.Error("Inputs = nil in maintenance, want the computed inputs")
	}
}

func threeEmptyReady() []model.Instance {
	return []model.Instance{
		inst("old", model.StateReady, 0, t0.Add(-2*time.Hour)),
		inst("mid", model.StateReady, 0, t0.Add(-time.Hour)),
		inst("young", model.StateReady, 0, t0.Add(-time.Minute)),
	}
}

func TestPrivatePoolIsNeverScaled(t *testing.T) {
	spec := dynamicSpec(0, 5, 0)
	spec.Private = &model.Private{}
	insts := []model.Instance{inst("home-a", model.StateReady, 0, t0), inst("home-b", model.StateAllocated, 3, t0)}

	d := Decide(PoolView{Name: "home", Spec: spec, Instances: insts}, t0.Add(time.Hour))

	if d.Start != 0 || len(d.Drain) != 0 || d.Direction != "" {
		t.Fatalf("decision for a private pool = %+v, want no start, drain or direction", d)
	}
}
