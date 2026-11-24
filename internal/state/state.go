// Package state holds the instance lifecycle transition table. It is pure: no I/O, no goroutines and no
// wall clock. The caller passes the current time, so every transition is deterministic and testable.
package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// Input is an event that may move an instance from one state to another.
type Input string

const (
	InScheduled       Input = "scheduled"
	InPreparing       Input = "preparing"
	InStarted         Input = "started"
	InReady           Input = "ready"
	InAllocate        Input = "allocate"
	InShutdown        Input = "shutdown"
	InHeartbeatMissed Input = "heartbeat-missed"
	InHeartbeat       Input = "heartbeat"
	InDrain           Input = "drain"
	InStop            Input = "stop"
	InExited          Input = "exited"
	InGone            Input = "gone"
	InFailed          Input = "failed"
	InRestart         Input = "restart"
)

// ErrInvalidTransition is wrapped by every error Transition returns for a pair that is not in the table.
var ErrInvalidTransition = errors.New("invalid state transition")

// table maps the current state and the input to the next state. Every pair not listed is invalid.
var table = map[model.State]map[Input]model.State{
	model.StateRequested: {
		InScheduled: model.StateScheduled,
		InStop:      model.StateStopped,
		InFailed:    model.StateFailed,
	},
	model.StateScheduled: {
		InPreparing: model.StatePreparing,
		InGone:      model.StateLost,
		InStop:      model.StateStopping,
		InFailed:    model.StateFailed,
	},
	model.StatePreparing: {
		InStarted: model.StateStarting,
		InReady:   model.StateReady,
		InFailed:  model.StateFailed,
		InGone:    model.StateLost,
		InStop:    model.StateStopping,
	},
	model.StateStarting: {
		InReady:  model.StateReady,
		InFailed: model.StateFailed,
		InExited: model.StateFailed,
		InGone:   model.StateLost,
		InStop:   model.StateStopping,
	},
	model.StateReady: {
		InReady:           model.StateReady,
		InAllocate:        model.StateAllocated,
		InHeartbeatMissed: model.StateUnhealthy,
		InDrain:           model.StateDraining,
		InStop:            model.StateStopping,
		InShutdown:        model.StateStopping,
		InGone:            model.StateLost,
		InExited:          model.StateLost,
	},
	model.StateAllocated: {
		InReady:           model.StateReady,
		InHeartbeatMissed: model.StateUnhealthy,
		InShutdown:        model.StateStopping,
		InStop:            model.StateStopping,
		InGone:            model.StateLost,
		InExited:          model.StateLost,
	},
	model.StateUnhealthy: {
		InHeartbeat: model.StateReady,
		InReady:     model.StateReady,
		InStop:      model.StateStopping,
		InShutdown:  model.StateStopping,
		InGone:      model.StateLost,
		InExited:    model.StateLost,
	},
	model.StateDraining: {
		InStop:     model.StateStopping,
		InShutdown: model.StateStopping,
		InGone:     model.StateLost,
		InExited:   model.StateStopped,
	},
	model.StateStopping: {
		InExited: model.StateStopped,
		InGone:   model.StateStopped,
	},
	model.StateFailed: {
		InStop:   model.StateStopped,
		InExited: model.StateStopped,
		InGone:   model.StateStopped,
	},
	model.StateLost: {
		InStop:   model.StateStopped,
		InExited: model.StateStopped,
		InGone:   model.StateStopped,
	},
	model.StateStopped: {
		InRestart: model.StateRequested,
	},
}

// next looks up the target state for from + in.
func next(from model.State, in Input) (model.State, bool) {
	to, ok := table[from][in]
	return to, ok
}

// Can reports whether the table has an entry for from + in, including same-state entries such as
// Ready + ready, which Transition treats as a no-op.
func Can(from model.State, in Input) bool {
	_, ok := next(from, in)
	return ok
}

// Transition returns the instance after applying in. It sets State, StateReason=reason and StateSince=now
// when the state changes. Entering Allocated sets AllocatedAt=&now; leaving Allocated clears it.
// A transition to the state the instance is already in is a no-op and returns (inst, nil) unchanged.
// A pair that is not in the table returns the instance unchanged and an error wrapping ErrInvalidTransition.
func Transition(inst model.Instance, in Input, reason string, now time.Time) (model.Instance, error) {
	from := inst.State
	to, ok := next(from, in)
	if !ok {
		return inst, fmt.Errorf("%w: input %q from state %s", ErrInvalidTransition, in, from)
	}
	if to == from {
		return inst, nil
	}
	inst.State = to
	inst.StateReason = reason
	inst.StateSince = now
	if to == model.StateAllocated {
		at := now
		inst.AllocatedAt = &at
	}
	if from == model.StateAllocated && to != model.StateAllocated {
		inst.AllocatedAt = nil
	}
	return inst, nil
}
