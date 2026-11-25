package controller

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/state"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

// ownerRE limits owners to what fits a label value (player uuids and names).
var ownerRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// ownerOf returns the owner label of an instance.
func ownerOf(inst model.Instance) string { return inst.Labels[model.LabelOwner] }

// usable reports whether a private instance can still take its owner: it is on its way up or running.
func usable(inst model.Instance) bool {
	switch inst.State {
	case model.StateRequested, model.StateScheduled, model.StatePreparing, model.StateStarting,
		model.StateReady, model.StateAllocated:
		return true
	}
	return false
}

// CreatePrivateServer returns the live instance of owner in pool, or starts one. An owner with an instance
// that is draining or stopping has to wait until it is gone once the quota is used up.
func (c *Controller) CreatePrivateServer(ctx context.Context, pool, owner string) (model.Instance, bool, error) {
	if !ownerRE.MatchString(owner) {
		return model.Instance{}, false, &ValidationError{Kind: "private server", Fields: []model.FieldError{
			{Field: "owner", Message: "must match " + ownerRE.String()}}}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p, err := c.st.GetPool(ctx, pool)
	if err != nil {
		return model.Instance{}, false, mapErr(err)
	}
	spec := p.Spec.WithDefaults()
	if spec.Private == nil {
		return model.Instance{}, false, &ValidationError{Kind: "private server", Fields: []model.FieldError{
			{Field: "pool", Message: "pool " + pool + " has no private section"}}}
	}
	insts, err := c.listAll(ctx)
	if err != nil {
		return model.Instance{}, false, err
	}
	owned, live := 0, 0
	for _, inst := range insts {
		if inst.State.Terminal() || inst.Pool != pool {
			continue
		}
		live++
		if ownerOf(inst) != owner {
			continue
		}
		if usable(inst) {
			return inst, false, nil
		}
		owned++
	}
	if owned >= spec.Private.QuotaPerOwner {
		return model.Instance{}, false, fmt.Errorf("%w: %s still has %d instance(s) in %s that are stopping", ErrQuota, owner, owned, pool)
	}
	if p.Maintenance {
		return model.Instance{}, false, fmt.Errorf("%w: pool %s is in maintenance", ErrInvalidState, pool)
	}
	if live >= spec.Scaling.Max || countLive(insts) >= c.cfg.MaxInstances {
		return model.Instance{}, false, ErrLimit
	}
	inst, err := c.createInstanceWithLabels(ctx, p, map[string]string{model.LabelOwner: owner})
	if err != nil {
		return model.Instance{}, false, mapErr(err)
	}
	c.Trigger()
	return inst, true, nil
}

// ListPrivateServers lists the non-terminal instances that have an owner, optionally of one owner.
func (c *Controller) ListPrivateServers(ctx context.Context, owner string) ([]model.Instance, error) {
	insts, err := c.st.ListInstances(ctx, store.InstanceFilter{})
	if err != nil {
		return nil, mapErr(err)
	}
	var out []model.Instance
	for _, inst := range insts {
		if inst.State.Terminal() || ownerOf(inst) == "" || (owner != "" && ownerOf(inst) != owner) {
			continue
		}
		out = append(out, inst)
	}
	return out, nil
}

// StopPrivateServer drains the private instance id. An instance that is not private is reported as unknown.
func (c *Controller) StopPrivateServer(ctx context.Context, id string) (model.Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, err := c.st.GetInstance(ctx, id)
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	if ownerOf(inst) == "" {
		return model.Instance{}, fmt.Errorf("%w: %s is not a private server", ErrNotFound, id)
	}
	if inst.State.Terminal() {
		return model.Instance{}, fmt.Errorf("%w: instance is %s", ErrInvalidState, inst.State)
	}
	switch {
	case inst.State == model.StateReady:
		inst, err = c.apply(ctx, inst, state.InDrain, "private server stop requested")
	case inst.State == model.StateDraining || inst.State == model.StateStopping:
		// already on its way out
	default:
		inst, err = c.stop(ctx, inst, state.InStop, "private server stop requested", c.lifecycleOf(ctx, inst.Pool))
	}
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	c.Trigger()
	return inst, nil
}

// stopIdlePrivate stops the Ready and Allocated instances of private pools that have had no players for the
// pool's idle timeout. The time an instance became empty is tracked in memory; after a controller restart the
// idle period starts over, which only delays a stop. Callers hold c.mu.
func (c *Controller) stopIdlePrivate(ctx context.Context, insts []model.Instance, specs map[string]model.PoolSpec, unlisted map[string]bool, now time.Time) []error {
	var errs []error
	seen := make(map[string]bool)
	for _, inst := range insts {
		spec, ok := specs[inst.Pool]
		if !ok || spec.Private == nil || ownerOf(inst) == "" || unlisted[hostOf(inst)] {
			continue
		}
		if inst.State != model.StateReady && inst.State != model.StateAllocated {
			continue
		}
		seen[inst.ID] = true
		if inst.Players > 0 {
			delete(c.idleSince, inst.ID)
			continue
		}
		since, ok := c.idleSince[inst.ID]
		if !ok {
			c.idleSince[inst.ID] = now
			continue
		}
		if now.Sub(since) < spec.Private.IdleTimeout.D() {
			continue
		}
		if _, err := c.stop(ctx, inst, state.InStop, "idle", c.lifecycleOf(ctx, inst.Pool)); err != nil {
			errs = append(errs, fmt.Errorf("stop idle private instance %s: %w", inst.ID, err))
			continue
		}
		delete(c.idleSince, inst.ID)
		c.pub.Publish(model.EventPrivateIdleStop, model.PrivateIdleStopData{Pool: inst.Pool, Instance: inst.ID, Owner: ownerOf(inst)})
	}
	for id := range c.idleSince {
		if !seen[id] {
			delete(c.idleSince, id)
		}
	}
	return errs
}
