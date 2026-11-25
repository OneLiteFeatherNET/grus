package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/state"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

var poolNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// mapErr translates store, state and driver errors into the controller's sentinel errors.
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound), errors.Is(err, driver.ErrNotFound):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case errors.Is(err, state.ErrInvalidTransition):
		return fmt.Errorf("%w: %w", ErrInvalidState, err)
	}
	return err
}

// PutPool validates and stores a pool owned by the api. The spec is stored as sent; defaults apply when it is used.
func (c *Controller) PutPool(ctx context.Context, name string, spec model.PoolSpec) (model.Pool, error) {
	return c.ApplyPool(ctx, model.SourceAPI, name, spec)
}

// ApplyPool is PutPool for a declaration owned by source; see Applier.
func (c *Controller) ApplyPool(ctx context.Context, source model.Source, name string, spec model.PoolSpec) (model.Pool, error) {
	var fields []model.FieldError
	if !poolNameRE.MatchString(name) {
		fields = append(fields, model.FieldError{Field: "name", Message: "must match ^[a-z0-9][a-z0-9-]{0,30}$"})
	}
	fields = append(fields, spec.WithDefaults().Validate()...)
	if len(fields) > 0 {
		return model.Pool{}, &ValidationError{Fields: fields}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, err := c.st.GetPool(ctx, name); err == nil {
		if err := checkOwner(cur.Source, source, "pool", name); err != nil {
			return model.Pool{}, err
		}
	}
	pool, _, err := c.st.PutPoolAs(ctx, name, spec, source)
	if err != nil {
		return model.Pool{}, mapErr(err)
	}
	c.pub.Publish(model.EventPoolUpdated, map[string]any{"pool": name, "generation": pool.Generation})
	c.Trigger()
	return pool, nil
}

func (c *Controller) GetPool(ctx context.Context, name string) (model.Pool, error) {
	p, err := c.st.GetPool(ctx, name)
	return p, mapErr(err)
}

func (c *Controller) ListPools(ctx context.Context) ([]model.Pool, error) {
	pools, err := c.st.ListPools(ctx)
	return pools, mapErr(err)
}

// DeletePool removes the pool. With drain, Ready instances are drained instead of stopped; the reconcile
// cycle stops every other instance of a pool that no longer exists.
func (c *Controller) DeletePool(ctx context.Context, name string, drain bool) error {
	return c.RemovePool(ctx, model.SourceAPI, name, drain)
}

// RemovePool is DeletePool on behalf of source; see Applier.
func (c *Controller) RemovePool(ctx context.Context, source model.Source, name string, drain bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, err := c.st.GetPool(ctx, name)
	if err != nil {
		return mapErr(err)
	}
	if err := checkOwner(cur.Source, source, "pool", name); err != nil {
		return err
	}
	if drain {
		insts, err := c.st.ListInstances(ctx, store.InstanceFilter{Pool: name})
		if err != nil {
			return mapErr(err)
		}
		for _, inst := range insts {
			if inst.State != model.StateReady {
				continue
			}
			if _, err := c.apply(ctx, inst, state.InDrain, "pool deleted"); err != nil {
				return mapErr(err)
			}
		}
	}
	if err := c.st.DeletePool(ctx, name); err != nil {
		return mapErr(err)
	}
	delete(c.belowSince, name)
	c.Trigger()
	return nil
}

func (c *Controller) SetMaintenance(ctx context.Context, name string, enabled bool) (model.Pool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pool, err := c.st.SetMaintenance(ctx, name, enabled)
	if err != nil {
		return model.Pool{}, mapErr(err)
	}
	c.pub.Publish(model.EventPoolUpdated, map[string]any{"pool": name, "generation": pool.Generation, "maintenance": enabled})
	c.Trigger()
	return pool, nil
}

func (c *Controller) ListInstances(ctx context.Context, f store.InstanceFilter) ([]model.Instance, error) {
	insts, err := c.st.ListInstances(ctx, f)
	return insts, mapErr(err)
}

func (c *Controller) GetInstance(ctx context.Context, id string) (model.Instance, error) {
	inst, err := c.st.GetInstance(ctx, id)
	return inst, mapErr(err)
}

// CreateInstance starts one more instance of a pool, subject to MaxInstances.
func (c *Controller) CreateInstance(ctx context.Context, pool string) (model.Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, err := c.st.GetPool(ctx, pool)
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	insts, err := c.listAll(ctx)
	if err != nil {
		return model.Instance{}, err
	}
	if countLive(insts) >= c.cfg.MaxInstances {
		return model.Instance{}, ErrLimit
	}
	inst, err := c.createInstance(ctx, p)
	return inst, mapErr(err)
}

// StopInstance stops an instance. Players block the stop unless drain or force is set.
func (c *Controller) StopInstance(ctx context.Context, id string, opts StopOptions) (model.Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, err := c.st.GetInstance(ctx, id)
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	if inst.State.Terminal() {
		return model.Instance{}, fmt.Errorf("%w: instance is %s", ErrInvalidState, inst.State)
	}
	if inst.Players > 0 && !opts.Drain && !opts.Force {
		return model.Instance{}, fmt.Errorf("%w: %d players", ErrHasPlayers, inst.Players)
	}
	switch {
	case opts.Drain && inst.State == model.StateReady:
		inst, err = c.apply(ctx, inst, state.InDrain, "stop requested with drain")
	case opts.Drain && inst.State == model.StateDraining:
		// already draining
	default:
		inst, err = c.stop(ctx, inst, state.InStop, "stop requested", c.lifecycleOf(ctx, inst.Pool))
	}
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	c.Trigger()
	return inst, nil
}

// driverRef returns the driver reference of an instance; ErrNotFound when it has none.
func (c *Controller) driverRef(ctx context.Context, id string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, err := c.st.GetInstance(ctx, id)
	if err != nil {
		return "", mapErr(err)
	}
	if inst.DriverRef == "" {
		return "", fmt.Errorf("%w: instance %s has no driver reference", ErrNotFound, id)
	}
	return inst.DriverRef, nil
}

// Logs streams the driver logs. The lock is released before streaming starts.
func (c *Controller) Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error) {
	ref, err := c.driverRef(ctx, id)
	if err != nil {
		return nil, err
	}
	rc, err := c.drv.Logs(ctx, ref, tail, follow)
	return rc, mapErr(err)
}

func (c *Controller) Command(ctx context.Context, id string, line string) error {
	ref, err := c.driverRef(ctx, id)
	if err != nil {
		return err
	}
	return mapErr(c.drv.Command(ctx, ref, line))
}

// AuthenticateInstance maps a plain per-instance token to its live instance.
func (c *Controller) AuthenticateInstance(ctx context.Context, token string) (model.Instance, error) {
	if token == "" {
		return model.Instance{}, ErrNotFound
	}
	sum := sha256.Sum256([]byte(token))
	inst, err := c.st.FindInstanceByTokenHash(ctx, hex.EncodeToString(sum[:]))
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	if inst.State.Terminal() {
		return model.Instance{}, ErrNotFound
	}
	return inst, nil
}

// SelfReady: the instance reports it is ready to receive players.
func (c *Controller) SelfReady(ctx context.Context, id string) (model.Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, err := c.st.GetInstance(ctx, id)
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	now := c.clock.Now()
	inst.LastHeartbeat = &now
	inst, err = c.applyWithFields(ctx, inst, state.InReady, "self ready")
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	c.Trigger()
	return inst, nil
}

// SelfStatus stores the player count and labels and refreshes the heartbeat.
func (c *Controller) SelfStatus(ctx context.Context, id string, u StatusUpdate) (model.Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, err := c.st.GetInstance(ctx, id)
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	now := c.clock.Now()
	changed := u.Players != inst.Players
	inst.Players = u.Players
	if u.MaxPlayers > 0 {
		inst.MaxPlayers = u.MaxPlayers
	}
	if u.Labels != nil {
		inst.Labels = u.Labels
	}
	inst.LastHeartbeat = &now

	if inst.State == model.StateUnhealthy {
		inst, err = c.applyWithFields(ctx, inst, state.InHeartbeat, "heartbeat received")
	} else {
		err = c.st.UpdateInstance(ctx, inst)
	}
	if err != nil {
		return model.Instance{}, mapErr(err)
	}

	last, seen := c.lastPublished[id]
	if changed || !seen || now.Sub(last) >= statusPublishInterval {
		c.pub.Publish(model.EventInstanceUpdated, inst)
		c.lastPublished[id] = now
	}
	if changed {
		c.Trigger()
	}
	if inst.Role == model.RoleProxy {
		c.publishNetwork(ctx)
	}
	return inst, nil
}

// SelfAllocate: the instance was taken by a round (Minigame). Allocated instances are never scaled down.
func (c *Controller) SelfAllocate(ctx context.Context, id string) (model.Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, err := c.st.GetInstance(ctx, id)
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	inst, err = c.apply(ctx, inst, state.InAllocate, "allocated")
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	c.Trigger()
	return inst, nil
}

// SelfShutdown: the instance asks to be stopped (end of a round).
func (c *Controller) SelfShutdown(ctx context.Context, id string) (model.Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, err := c.st.GetInstance(ctx, id)
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	inst, err = c.stop(ctx, inst, state.InShutdown, "self shutdown", c.lifecycleOf(ctx, inst.Pool))
	if err != nil {
		return model.Instance{}, mapErr(err)
	}
	c.Trigger()
	return inst, nil
}
