package state

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

var fixedNow = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

func instanceIn(s model.State) model.Instance {
	return model.Instance{
		ID:         "inst-1",
		Pool:       "lobby",
		State:      s,
		StateSince: fixedNow.Add(-time.Hour),
		CreatedAt:  fixedNow.Add(-2 * time.Hour),
	}
}

func TestTransitionAppliesEveryAllowedRow(t *testing.T) {
	cases := []struct {
		name string
		from model.State
		in   Input
		want model.State
	}{
		{"requested scheduled moves to scheduled", model.StateRequested, InScheduled, model.StateScheduled},
		{"requested stop moves to stopped", model.StateRequested, InStop, model.StateStopped},
		{"requested failed moves to failed", model.StateRequested, InFailed, model.StateFailed},

		{"scheduled preparing moves to preparing", model.StateScheduled, InPreparing, model.StatePreparing},
		{"scheduled gone moves to lost", model.StateScheduled, InGone, model.StateLost},
		{"scheduled stop moves to stopping", model.StateScheduled, InStop, model.StateStopping},
		{"scheduled failed moves to failed", model.StateScheduled, InFailed, model.StateFailed},

		{"preparing started moves to starting", model.StatePreparing, InStarted, model.StateStarting},
		{"preparing ready moves to ready", model.StatePreparing, InReady, model.StateReady},
		{"preparing failed moves to failed", model.StatePreparing, InFailed, model.StateFailed},
		{"preparing gone moves to lost", model.StatePreparing, InGone, model.StateLost},
		{"preparing stop moves to stopping", model.StatePreparing, InStop, model.StateStopping},

		{"starting ready moves to ready", model.StateStarting, InReady, model.StateReady},
		{"starting failed moves to failed", model.StateStarting, InFailed, model.StateFailed},
		{"starting exited moves to failed", model.StateStarting, InExited, model.StateFailed},
		{"starting gone moves to lost", model.StateStarting, InGone, model.StateLost},
		{"starting stop moves to stopping", model.StateStarting, InStop, model.StateStopping},

		{"ready ready is a no-op on ready", model.StateReady, InReady, model.StateReady},
		{"ready allocate moves to allocated", model.StateReady, InAllocate, model.StateAllocated},
		{"ready heartbeat-missed moves to unhealthy", model.StateReady, InHeartbeatMissed, model.StateUnhealthy},
		{"ready drain moves to draining", model.StateReady, InDrain, model.StateDraining},
		{"ready stop moves to stopping", model.StateReady, InStop, model.StateStopping},
		{"ready shutdown moves to stopping", model.StateReady, InShutdown, model.StateStopping},
		{"ready gone moves to lost", model.StateReady, InGone, model.StateLost},
		{"ready exited moves to lost", model.StateReady, InExited, model.StateLost},

		{"allocated ready moves to ready", model.StateAllocated, InReady, model.StateReady},
		{"allocated heartbeat-missed moves to unhealthy", model.StateAllocated, InHeartbeatMissed, model.StateUnhealthy},
		{"allocated shutdown moves to stopping", model.StateAllocated, InShutdown, model.StateStopping},
		{"allocated stop moves to stopping", model.StateAllocated, InStop, model.StateStopping},
		{"allocated gone moves to lost", model.StateAllocated, InGone, model.StateLost},
		{"allocated exited moves to lost", model.StateAllocated, InExited, model.StateLost},

		{"unhealthy heartbeat moves to ready", model.StateUnhealthy, InHeartbeat, model.StateReady},
		{"unhealthy ready moves to ready", model.StateUnhealthy, InReady, model.StateReady},
		{"unhealthy stop moves to stopping", model.StateUnhealthy, InStop, model.StateStopping},
		{"unhealthy shutdown moves to stopping", model.StateUnhealthy, InShutdown, model.StateStopping},
		{"unhealthy gone moves to lost", model.StateUnhealthy, InGone, model.StateLost},
		{"unhealthy exited moves to lost", model.StateUnhealthy, InExited, model.StateLost},

		{"draining stop moves to stopping", model.StateDraining, InStop, model.StateStopping},
		{"draining shutdown moves to stopping", model.StateDraining, InShutdown, model.StateStopping},
		{"draining gone moves to lost", model.StateDraining, InGone, model.StateLost},
		{"draining exited moves to stopped", model.StateDraining, InExited, model.StateStopped},

		{"stopping exited moves to stopped", model.StateStopping, InExited, model.StateStopped},
		{"stopping gone moves to stopped", model.StateStopping, InGone, model.StateStopped},

		{"failed stop moves to stopped", model.StateFailed, InStop, model.StateStopped},
		{"failed exited moves to stopped", model.StateFailed, InExited, model.StateStopped},
		{"failed gone moves to stopped", model.StateFailed, InGone, model.StateStopped},

		{"lost stop moves to stopped", model.StateLost, InStop, model.StateStopped},
		{"lost exited moves to stopped", model.StateLost, InExited, model.StateStopped},
		{"lost gone moves to stopped", model.StateLost, InGone, model.StateStopped},

		{"stopped restart moves to requested", model.StateStopped, InRestart, model.StateRequested},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !Can(tc.from, tc.in) {
				t.Fatalf("Can(%s, %s) = false, want true for an allowed row", tc.from, tc.in)
			}
			got, err := Transition(instanceIn(tc.from), tc.in, "reason", fixedNow)
			if err != nil {
				t.Fatalf("Transition(%s, %s) returned error %v, want nil", tc.from, tc.in, err)
			}
			if got.State != tc.want {
				t.Errorf("Transition(%s, %s).State = %s, want %s", tc.from, tc.in, got.State, tc.want)
			}
		})
	}
}

func TestTransitionRejectsInvalidPairsAndLeavesInstanceUnchanged(t *testing.T) {
	cases := []struct {
		name string
		from model.State
		in   Input
	}{
		{"requested ready is invalid", model.StateRequested, InReady},
		{"stopped stop is invalid", model.StateStopped, InStop},
		{"ready scheduled is invalid", model.StateReady, InScheduled},
		{"draining heartbeat is invalid", model.StateDraining, InHeartbeat},
		{"lost drain is invalid", model.StateLost, InDrain},
		{"allocated allocate is invalid", model.StateAllocated, InAllocate},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if Can(tc.from, tc.in) {
				t.Fatalf("Can(%s, %s) = true, want false", tc.from, tc.in)
			}
			before := instanceIn(tc.from)
			snapshot := before
			got, err := Transition(before, tc.in, "reason", fixedNow)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Transition(%s, %s) error = %v, want wrapping ErrInvalidTransition", tc.from, tc.in, err)
			}
			if !reflect.DeepEqual(got, snapshot) {
				t.Errorf("Transition(%s, %s) changed the instance on error: got %+v, want %+v", tc.from, tc.in, got, snapshot)
			}
		})
	}
}

func TestTransitionSameStateNoOpKeepsStateSinceAndReason(t *testing.T) {
	inst := instanceIn(model.StateReady)
	inst.StateReason = "first"
	got, err := Transition(inst, InReady, "second", fixedNow)
	if err != nil {
		t.Fatalf("Transition(Ready, ready) error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, inst) {
		t.Errorf("same-state transition changed the instance: got %+v, want %+v", got, inst)
	}
}

func TestTransitionEnteringAllocatedSetsAllocatedAt(t *testing.T) {
	got, err := Transition(instanceIn(model.StateReady), InAllocate, "round started", fixedNow)
	if err != nil {
		t.Fatalf("Transition(Ready, allocate) error = %v, want nil", err)
	}
	if got.AllocatedAt == nil {
		t.Fatal("AllocatedAt = nil after entering Allocated, want the transition time")
	}
	if !got.AllocatedAt.Equal(fixedNow) {
		t.Errorf("AllocatedAt = %v, want %v", *got.AllocatedAt, fixedNow)
	}
}

func TestTransitionLeavingAllocatedClearsAllocatedAt(t *testing.T) {
	allocatedAt := fixedNow.Add(-time.Minute)
	inst := instanceIn(model.StateAllocated)
	inst.AllocatedAt = &allocatedAt

	for _, in := range []Input{InReady, InShutdown, InStop, InGone, InExited, InHeartbeatMissed} {
		t.Run(string(in), func(t *testing.T) {
			got, err := Transition(inst, in, "reason", fixedNow)
			if err != nil {
				t.Fatalf("Transition(Allocated, %s) error = %v, want nil", in, err)
			}
			if got.State == model.StateAllocated {
				t.Fatalf("Transition(Allocated, %s) stayed Allocated, test needs a leaving transition", in)
			}
			if got.AllocatedAt != nil {
				t.Errorf("AllocatedAt = %v after leaving Allocated via %s, want nil", *got.AllocatedAt, in)
			}
		})
	}
}

func TestTransitionOtherStatesLeaveAllocatedAtUntouched(t *testing.T) {
	allocatedAt := fixedNow.Add(-time.Minute)
	inst := instanceIn(model.StateReady)
	inst.AllocatedAt = nil

	got, err := Transition(inst, InHeartbeatMissed, "reason", fixedNow)
	if err != nil {
		t.Fatalf("Transition(Ready, heartbeat-missed) error = %v, want nil", err)
	}
	if got.AllocatedAt != nil {
		t.Errorf("AllocatedAt = %v, want nil when no Allocated state was involved", allocatedAt)
	}
}

func TestTransitionStateSinceChangesOnlyOnRealChange(t *testing.T) {
	original := instanceIn(model.StateReady)
	later := fixedNow.Add(time.Hour)

	noop, err := Transition(original, InReady, "again", later)
	if err != nil {
		t.Fatalf("Transition(Ready, ready) error = %v, want nil", err)
	}
	if !noop.StateSince.Equal(original.StateSince) {
		t.Errorf("StateSince changed on a no-op: got %v, want %v", noop.StateSince, original.StateSince)
	}

	changed, err := Transition(original, InAllocate, "round started", later)
	if err != nil {
		t.Fatalf("Transition(Ready, allocate) error = %v, want nil", err)
	}
	if !changed.StateSince.Equal(later) {
		t.Errorf("StateSince = %v after a real change, want %v", changed.StateSince, later)
	}
	if changed.StateReason != "round started" {
		t.Errorf("StateReason = %q after a real change, want %q", changed.StateReason, "round started")
	}
}
