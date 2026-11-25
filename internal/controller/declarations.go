package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

var _ Applier = (*Controller)(nil)

// checkOwner rejects a change by source when the stored object belongs to Kubernetes.
func checkOwner(stored, source model.Source, kind, name string) error {
	if stored == model.SourceKubernetes && source != model.SourceKubernetes {
		return &managedError{kind: kind, name: name}
	}
	return nil
}

// managedError is ErrManagedElsewhere with the custom resource the caller has to edit instead.
type managedError struct{ kind, name string }

func (e *managedError) Error() string {
	kind := strings.ToUpper(e.kind[:1]) + e.kind[1:]
	return fmt.Sprintf("managed by Kubernetes custom resource %s/%s; edit it with kubectl", kind, e.name)
}

func (e *managedError) Is(target error) bool { return target == ErrManagedElsewhere }

func validateName(name string) []model.FieldError {
	if poolNameRE.MatchString(name) {
		return nil
	}
	return []model.FieldError{{Field: "name", Message: "must match ^[a-z0-9][a-z0-9-]{0,30}$"}}
}

// PutEnvironment validates and stores an environment owned by the api.
func (c *Controller) PutEnvironment(ctx context.Context, name string, spec model.EnvironmentSpec) (model.Environment, error) {
	return c.ApplyEnvironment(ctx, model.SourceAPI, name, spec)
}

// ApplyEnvironment is PutEnvironment for a declaration owned by source; see Applier.
func (c *Controller) ApplyEnvironment(ctx context.Context, source model.Source, name string, spec model.EnvironmentSpec) (model.Environment, error) {
	fields := append(validateName(name), spec.WithDefaults().Validate()...)
	if len(fields) > 0 {
		return model.Environment{}, &ValidationError{Kind: "environment", Fields: fields}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, err := c.st.GetEnvironment(ctx, name); err == nil {
		if err := checkOwner(cur.Source, source, "environment", name); err != nil {
			return model.Environment{}, err
		}
	}
	env, changed, err := c.st.PutEnvironment(ctx, name, spec, source)
	if err != nil {
		return model.Environment{}, mapErr(err)
	}
	if changed {
		c.pub.Publish(model.EventPoolUpdated, map[string]any{"environment": name, "generation": env.Generation})
		c.Trigger()
	}
	return env, nil
}

func (c *Controller) GetEnvironment(ctx context.Context, name string) (model.Environment, error) {
	e, err := c.st.GetEnvironment(ctx, name)
	return e, mapErr(err)
}

func (c *Controller) ListEnvironments(ctx context.Context) ([]model.Environment, error) {
	es, err := c.st.ListEnvironments(ctx)
	return es, mapErr(err)
}

// DeleteEnvironment removes an environment owned by the api. Pools that still name it stop starting instances.
func (c *Controller) DeleteEnvironment(ctx context.Context, name string) error {
	return c.RemoveEnvironment(ctx, model.SourceAPI, name)
}

// RemoveEnvironment is DeleteEnvironment on behalf of source; see Applier.
func (c *Controller) RemoveEnvironment(ctx context.Context, source model.Source, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, err := c.st.GetEnvironment(ctx, name)
	if err != nil {
		return mapErr(err)
	}
	if err := checkOwner(cur.Source, source, "environment", name); err != nil {
		return err
	}
	return mapErr(c.st.DeleteEnvironment(ctx, name))
}

// PutGroup validates and stores a group owned by the api.
func (c *Controller) PutGroup(ctx context.Context, name string, spec model.GroupSpec) (model.Group, error) {
	return c.ApplyGroup(ctx, model.SourceAPI, name, spec)
}

// ApplyGroup is PutGroup for a declaration owned by source; see Applier.
func (c *Controller) ApplyGroup(ctx context.Context, source model.Source, name string, spec model.GroupSpec) (model.Group, error) {
	fields := append(validateName(name), spec.Validate()...)
	if len(fields) > 0 {
		return model.Group{}, &ValidationError{Kind: "group", Fields: fields}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, err := c.st.GetGroup(ctx, name); err == nil {
		if err := checkOwner(cur.Source, source, "group", name); err != nil {
			return model.Group{}, err
		}
	}
	g, changed, err := c.st.PutGroup(ctx, name, spec, source)
	if err != nil {
		return model.Group{}, mapErr(err)
	}
	if changed {
		c.pub.Publish(model.EventPoolUpdated, map[string]any{"group": name, "generation": g.Generation})
		c.Trigger()
	}
	return g, nil
}

func (c *Controller) GetGroup(ctx context.Context, name string) (model.Group, error) {
	g, err := c.st.GetGroup(ctx, name)
	return g, mapErr(err)
}

func (c *Controller) ListGroups(ctx context.Context) ([]model.Group, error) {
	gs, err := c.st.ListGroups(ctx)
	return gs, mapErr(err)
}

// DeleteGroup removes a group owned by the api. Pools that still name it stop starting instances.
func (c *Controller) DeleteGroup(ctx context.Context, name string) error {
	return c.RemoveGroup(ctx, model.SourceAPI, name)
}

// RemoveGroup is DeleteGroup on behalf of source; see Applier.
func (c *Controller) RemoveGroup(ctx context.Context, source model.Source, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, err := c.st.GetGroup(ctx, name)
	if err != nil {
		return mapErr(err)
	}
	if err := checkOwner(cur.Source, source, "group", name); err != nil {
		return err
	}
	return mapErr(c.st.DeleteGroup(ctx, name))
}

// effective resolves a pool spec against its stored environment and groups. Unknown references and specs
// that cannot be run come back as a *ValidationError.
func (c *Controller) effective(ctx context.Context, spec model.PoolSpec) (model.EffectiveSpec, error) {
	var env *model.EnvironmentSpec
	var missing []model.FieldError
	if spec.Environment != "" {
		e, err := c.st.GetEnvironment(ctx, spec.Environment)
		switch {
		case err == nil:
			env = &e.Spec
		case errors.Is(err, store.ErrNotFound):
			missing = append(missing, model.FieldError{Field: "environment", Message: fmt.Sprintf("environment %q does not exist", spec.Environment)})
		default:
			return model.EffectiveSpec{}, err
		}
	}
	groups := make([]model.GroupSpec, 0, len(spec.Groups))
	for i, name := range spec.Groups {
		g, err := c.st.GetGroup(ctx, name)
		switch {
		case err == nil:
			groups = append(groups, g.Spec)
		case errors.Is(err, store.ErrNotFound):
			missing = append(missing, model.FieldError{Field: fmt.Sprintf("groups[%d]", i), Message: fmt.Sprintf("group %q does not exist", name)})
		default:
			return model.EffectiveSpec{}, err
		}
	}
	if len(missing) > 0 {
		return model.EffectiveSpec{}, &ValidationError{Fields: missing}
	}
	eff, errs := model.ResolvePool(spec, env, groups)
	if len(errs) > 0 {
		return model.EffectiveSpec{}, &ValidationError{Fields: errs}
	}
	return eff, nil
}

// noteResolve records why a pool cannot start instances, once per distinct reason, as log line and
// pool.condition event. A successful resolve (empty reason) clears the memory.
func (c *Controller) noteResolve(pool string, err error) {
	if err == nil {
		delete(c.resolveWarned, pool)
		return
	}
	var ve *ValidationError
	msg := err.Error()
	if errors.As(err, &ve) {
		parts := make([]string, 0, len(ve.Fields))
		for _, f := range ve.Fields {
			parts = append(parts, f.Field+": "+f.Message)
		}
		msg = strings.Join(parts, "; ")
	}
	if c.resolveWarned[pool] == msg {
		return
	}
	c.resolveWarned[pool] = msg
	c.log.Warn("pool cannot start instances", "pool", pool, "reason", msg)
	c.pub.Publish(model.EventPoolCondition, model.PoolConditionData{Pool: pool, Type: "Unresolvable", Message: msg})
}

// Network sums the players of all Ready and Allocated proxy instances.
func (c *Controller) Network(ctx context.Context) (model.NetworkStatus, error) {
	insts, err := c.st.ListInstances(ctx, store.InstanceFilter{Role: model.RoleProxy})
	if err != nil {
		return model.NetworkStatus{}, mapErr(err)
	}
	var n model.NetworkStatus
	for _, inst := range insts {
		if inst.State != model.StateReady && inst.State != model.StateAllocated {
			continue
		}
		n.Proxies++
		n.Players += inst.Players
		n.MaxPlayers += inst.MaxPlayers
	}
	return n, nil
}

// publishNetwork emits network.players when the summed count differs from the last published one.
// Callers hold c.mu and call it after a proxy's players or state changed.
func (c *Controller) publishNetwork(ctx context.Context) {
	n, err := c.Network(ctx)
	if err != nil {
		c.log.Error("compute network player count", "error", err)
		return
	}
	if n == c.lastNetwork {
		return
	}
	c.lastNetwork = n
	c.pub.Publish(model.EventNetworkPlayers, n)
}
