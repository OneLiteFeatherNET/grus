// Package autoscale decides how many instances a pool should have. It is pure: it takes a snapshot of the
// pool and the current time and returns what the caller should do. It performs no I/O and reads no clock.
package autoscale

import (
	"math"
	"sort"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// PoolView is the input snapshot for one pool. Spec must already have WithDefaults applied.
type PoolView struct {
	Name        string
	Spec        model.PoolSpec
	Maintenance bool
	Instances   []model.Instance // all non-terminal instances of the pool
	BelowSince  *time.Time       // when desired < active was first observed; nil if not
}

// Decision is what the caller should do for one pool in this cycle.
type Decision struct {
	Desired, Current int
	Start            int      // number of new instances to request now
	Drain            []string // instance ids to drain now
	Direction        string   // "", "up", "down"
	Reason           string   // "min", "buffer", "load", "idle" or "" when nothing happens
	Inputs           map[string]int
	BelowSince       *time.Time // value the caller stores for the next call
}

// activeStates are the states that count as capacity for scaling decisions.
var activeStates = map[model.State]bool{
	model.StateRequested: true,
	model.StateScheduled: true,
	model.StatePreparing: true,
	model.StateStarting:  true,
	model.StateReady:     true,
	model.StateAllocated: true,
	model.StateUnhealthy: true,
}

// Decide computes the scaling decision for one pool at time now.
func Decide(v PoolView, now time.Time) Decision {
	spec := v.Spec
	static := spec.Mode == model.ModeStatic

	var (
		active  int
		players int
		busy    int
		ready   []model.Instance
	)
	for _, inst := range v.Instances {
		if activeStates[inst.State] {
			active++
		}
		switch inst.State {
		case model.StateReady:
			players += inst.Players
			if inst.Players > 0 {
				busy++
			}
			ready = append(ready, inst)
		case model.StateAllocated:
			players += inst.Players
			busy++
		}
	}

	loadDesired := 0
	if spec.Scaling.Capacity > 0 && spec.Scaling.TargetUtilization > 0 {
		perInstance := float64(spec.Scaling.Capacity) * spec.Scaling.TargetUtilization
		loadDesired = int(math.Ceil(float64(players) / perInstance))
	}
	bufferDesired := max(spec.Scaling.Min, busy) + spec.Scaling.Buffer

	desired := max(loadDesired, bufferDesired)
	if static {
		desired = spec.Scaling.Min
	}
	desired = max(desired, spec.Scaling.Min)
	desired = min(desired, spec.Scaling.Max)

	d := Decision{
		Desired: desired,
		Current: active,
		Inputs: map[string]int{
			"players":       players,
			"busy":          busy,
			"loadDesired":   loadDesired,
			"bufferDesired": bufferDesired,
			"active":        active,
			"min":           spec.Scaling.Min,
			"max":           spec.Scaling.Max,
			"buffer":        spec.Scaling.Buffer,
		},
	}
	if v.Maintenance {
		return d
	}

	switch {
	case desired > active:
		d.Start = min(desired-active, spec.Scaling.MaxSurge)
		d.Direction = "up"
		switch {
		case active < spec.Scaling.Min:
			d.Reason = "min"
		case loadDesired > bufferDesired:
			d.Reason = "load"
		default:
			d.Reason = "buffer"
		}
	case desired == active:
		// Nothing to do; the hysteresis timer is reset below.
	case static:
		// Static pools are never scaled down automatically.
	default:
		// desired < active on a dynamic pool: scale down only after the condition held for ScaleDownDelay.
		if v.BelowSince == nil {
			d.BelowSince = &now
			return d
		}
		if now.Sub(*v.BelowSince) < spec.Scaling.ScaleDownDelay.D() {
			d.BelowSince = v.BelowSince
			return d
		}
		candidates := drainCandidates(ready)
		n := min(active-desired, len(candidates))
		if n == 0 {
			// Only Allocated, Unhealthy or Starting instances remain above desired; keep the timer running.
			d.BelowSince = v.BelowSince
			return d
		}
		for _, inst := range candidates[:n] {
			d.Drain = append(d.Drain, inst.ID)
		}
		d.Direction = "down"
		d.Reason = "idle"
	}
	return d
}

// drainCandidates orders Ready instances for draining: fewest players first, then youngest, then by ID.
func drainCandidates(ready []model.Instance) []model.Instance {
	c := make([]model.Instance, len(ready))
	copy(c, ready)
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].Players != c[j].Players {
			return c[i].Players < c[j].Players
		}
		if !c[i].CreatedAt.Equal(c[j].CreatedAt) {
			return c[i].CreatedAt.After(c[j].CreatedAt)
		}
		return c[i].ID < c[j].ID
	})
	return c
}
