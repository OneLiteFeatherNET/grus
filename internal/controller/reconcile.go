package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/autoscale"
	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/state"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

// ReconcileOnce runs one reconcile cycle: observe, apply driver facts, clean up terminal records, drop
// instances of deleted pools and scale every pool. Errors of single instances are collected and do not stop
// the cycle; a failing driver List aborts it, so no instance is marked lost on a driver outage.
func (c *Controller) ReconcileOnce(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clock.Now()
	pools, err := c.st.ListPools(ctx)
	if err != nil {
		return fmt.Errorf("list pools: %w", err)
	}
	specs := make(map[string]model.PoolSpec, len(pools))
	for _, p := range pools {
		specs[p.Name] = p.Spec.WithDefaults()
	}
	observed, err := c.drv.List(ctx)
	if err != nil {
		return fmt.Errorf("list driver instances: %w", err)
	}
	obsByID := make(map[string]driver.Observed, len(observed))
	for _, o := range observed {
		if o.InstanceID != "" {
			obsByID[o.InstanceID] = o
		}
	}

	var errs []error
	insts, err := c.listAll(ctx)
	if err != nil {
		return err
	}

	// 1. Driver facts and timeouts.
	for _, inst := range insts {
		if inst.State.Terminal() {
			continue
		}
		var obs *driver.Observed
		if o, ok := obsByID[inst.ID]; ok {
			obs = &o
		}
		if _, err := c.reconcileInstance(ctx, inst, obs, specFor(specs, inst.Pool), now); err != nil {
			errs = append(errs, fmt.Errorf("reconcile instance %s: %w", inst.ID, err))
		}
	}

	// 2. Terminal cleanup.
	if insts, err = c.listAll(ctx); err != nil {
		return err
	}
	for _, inst := range insts {
		if inst.State == model.StateFailed || inst.State == model.StateLost {
			updated, err := c.apply(ctx, inst, state.InStop, "cleanup")
			if err != nil {
				errs = append(errs, fmt.Errorf("stop terminal instance %s: %w", inst.ID, err))
				continue
			}
			inst = updated
		}
		if inst.State != model.StateStopped {
			continue
		}
		if specFor(specs, inst.Pool).Mode == model.ModeStatic && now.Sub(inst.StateSince) <= c.cfg.StaticRestartDelay {
			continue // the static name stays reserved for the restart delay
		}
		if err := c.removeInstance(ctx, inst); err != nil {
			errs = append(errs, fmt.Errorf("remove instance %s: %w", inst.ID, err))
		}
	}

	// 3. Instances whose pool no longer exists. Draining ones finish via step 1.
	if insts, err = c.listAll(ctx); err != nil {
		return err
	}
	for _, inst := range insts {
		if inst.State.Terminal() || inst.State == model.StateStopping || inst.State == model.StateDraining {
			continue
		}
		if _, ok := specs[inst.Pool]; ok {
			continue
		}
		if _, err := c.stop(ctx, inst, state.InStop, "pool removed", model.PoolSpec{}.WithDefaults().Lifecycle); err != nil {
			errs = append(errs, fmt.Errorf("stop orphan instance %s: %w", inst.ID, err))
		}
	}

	// 4. Autoscaling per pool.
	if insts, err = c.listAll(ctx); err != nil {
		return err
	}
	for name := range c.belowSince {
		if _, ok := specs[name]; !ok {
			delete(c.belowSince, name)
		}
	}
	live := countLive(insts)
	limitLogged := false
	for _, p := range pools {
		spec := specs[p.Name]
		d := autoscale.Decide(autoscale.PoolView{
			Name:        p.Name,
			Spec:        spec,
			Maintenance: p.Maintenance,
			Instances:   livePoolInstances(insts, p.Name),
			BelowSince:  c.belowSince[p.Name],
		}, now)
		if d.BelowSince != nil {
			c.belowSince[p.Name] = d.BelowSince
		} else {
			delete(c.belowSince, p.Name)
		}
		if d.Start > 0 && spec.Mode == model.ModeStatic && hasStopped(insts, p.Name) {
			// Restart backoff: the previous instance of this static pool is still recorded as Stopped.
			d.Start = 0
			d.Direction = ""
		}
		for _, id := range d.Drain {
			inst, ok := findInstance(insts, id)
			if !ok {
				continue
			}
			if _, err := c.apply(ctx, inst, state.InDrain, "scale down"); err != nil {
				errs = append(errs, fmt.Errorf("drain instance %s: %w", id, err))
			}
		}
		if d.Direction != "" {
			c.pub.Publish(model.EventPoolScaled, model.PoolScaledData{
				Pool:      p.Name,
				Direction: d.Direction,
				Reason:    d.Reason,
				Desired:   d.Desired,
				Current:   d.Current,
				Inputs:    d.Inputs,
			})
		}
		for i := 0; i < d.Start; i++ {
			if live >= c.cfg.MaxInstances {
				if !limitLogged {
					c.log.Warn("instance limit reached, no more instances are started this cycle", "max", c.cfg.MaxInstances)
					limitLogged = true
				}
				break
			}
			if _, err := c.createInstance(ctx, p); err != nil {
				var ve *ValidationError
				if errors.As(err, &ve) {
					break // unknown environment or group: reported once as pool.condition, retried next cycle
				}
				errs = append(errs, fmt.Errorf("create instance of pool %s: %w", p.Name, err))
				break
			}
			live++
		}
	}
	return errors.Join(errs...)
}

// reconcileInstance applies what the driver observed and the timeouts to one non-terminal instance.
func (c *Controller) reconcileInstance(ctx context.Context, inst model.Instance, obs *driver.Observed, spec model.PoolSpec, now time.Time) (model.Instance, error) {
	if obs == nil {
		if inst.State == model.StateRequested {
			return inst, nil // created later in this cycle, see step 4
		}
		return c.apply(ctx, inst, state.InGone, "instance gone")
	}
	if obs.Phase == driver.PhaseFailed || obs.Phase == driver.PhaseExited {
		return c.onExit(ctx, inst, obs)
	}

	addrChanged := obs.Address != "" && obs.Address != inst.Address
	refChanged := obs.Ref != "" && obs.Ref != inst.DriverRef
	if addrChanged {
		inst.Address = obs.Address
	}
	if refChanged {
		inst.DriverRef = obs.Ref
	}
	if addrChanged || refChanged {
		if err := c.st.UpdateInstance(ctx, inst); err != nil {
			return inst, fmt.Errorf("persist instance %s: %w", inst.ID, err)
		}
	}
	if addrChanged {
		c.pub.Publish(model.EventInstanceUpdated, inst)
	}

	var err error
	if inst.State == model.StateScheduled && (obs.Phase == driver.PhasePending || obs.Phase == driver.PhaseRunning) {
		if inst, err = c.apply(ctx, inst, state.InPreparing, ""); err != nil {
			return inst, err
		}
	}
	if obs.Phase == driver.PhaseRunning && inst.State == model.StatePreparing {
		if inst, err = c.apply(ctx, inst, state.InStarted, "process running"); err != nil {
			return inst, err
		}
	}

	lc := spec.Lifecycle
	switch inst.State {
	case model.StateStarting:
		if now.Sub(inst.StateSince) > lc.StartTimeout.D() {
			if inst, err = c.apply(ctx, inst, state.InFailed, "start timeout"); err != nil {
				return inst, err
			}
			return inst, c.kill(ctx, inst.DriverRef)
		}
	case model.StateReady, model.StateAllocated:
		last := inst.StateSince
		if inst.LastHeartbeat != nil {
			last = *inst.LastHeartbeat
		}
		if now.Sub(last) > lc.HeartbeatTimeout.D() {
			return c.apply(ctx, inst, state.InHeartbeatMissed, "heartbeat missed")
		}
	case model.StateUnhealthy:
		if spec.Mode == model.ModeDynamic && now.Sub(inst.StateSince) > lc.HeartbeatTimeout.D() {
			return c.stop(ctx, inst, state.InStop, "unhealthy", c.lifecycleOf(ctx, inst.Pool))
		}
	case model.StateDraining:
		if inst.Players == 0 || now.Sub(inst.StateSince) > spec.Scaling.DrainTimeout.D() {
			return c.stop(ctx, inst, state.InStop, "drained", c.lifecycleOf(ctx, inst.Pool))
		}
	}
	return inst, nil
}

// onExit handles a driver phase of Failed or Exited. The input is chosen from what the current state accepts.
func (c *Controller) onExit(ctx context.Context, inst model.Instance, obs *driver.Observed) (model.Instance, error) {
	prefs := []state.Input{state.InExited, state.InFailed}
	if obs.Phase == driver.PhaseFailed {
		prefs = []state.Input{state.InFailed, state.InExited}
	}
	in := state.InGone
	for _, p := range prefs {
		if state.Can(inst.State, p) {
			in = p
			break
		}
	}
	reason := fmt.Sprintf("driver phase %s", obs.Phase)
	if obs.Reason != "" {
		reason += ": " + obs.Reason
	}
	updated, err := c.apply(ctx, inst, in, reason)
	if err != nil {
		return inst, err
	}
	return updated, c.kill(ctx, updated.DriverRef)
}

// removeInstance kills the driver instance and deletes the record. Dynamic records go at once, static ones
// only after StaticRestartDelay (see step 2).
func (c *Controller) removeInstance(ctx context.Context, inst model.Instance) error {
	if err := c.kill(ctx, inst.DriverRef); err != nil {
		return err
	}
	if err := c.st.DeleteInstance(ctx, inst.ID); err != nil {
		return fmt.Errorf("delete instance %s: %w", inst.ID, err)
	}
	delete(c.lastPublished, inst.ID)
	c.pub.Publish(model.EventInstanceRemoved, map[string]any{
		"instance": inst.ID,
		"pool":     inst.Pool,
		"role":     inst.Role,
	})
	return nil
}

func (c *Controller) listAll(ctx context.Context) ([]model.Instance, error) {
	insts, err := c.st.ListInstances(ctx, store.InstanceFilter{})
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	return insts, nil
}

func specFor(specs map[string]model.PoolSpec, name string) model.PoolSpec {
	if s, ok := specs[name]; ok {
		return s
	}
	return model.PoolSpec{}.WithDefaults()
}

func countLive(insts []model.Instance) int {
	n := 0
	for _, inst := range insts {
		if !inst.State.Terminal() {
			n++
		}
	}
	return n
}

func livePoolInstances(insts []model.Instance, pool string) []model.Instance {
	var out []model.Instance
	for _, inst := range insts {
		if inst.Pool == pool && !inst.State.Terminal() {
			out = append(out, inst)
		}
	}
	return out
}

func hasStopped(insts []model.Instance, pool string) bool {
	for _, inst := range insts {
		if inst.Pool == pool && inst.State == model.StateStopped {
			return true
		}
	}
	return false
}

func findInstance(insts []model.Instance, id string) (model.Instance, bool) {
	for _, inst := range insts {
		if inst.ID == id {
			return inst, true
		}
	}
	return model.Instance{}, false
}
