package controller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/state"
	"github.com/OneLiteFeatherNET/grus/internal/store"
	"github.com/OneLiteFeatherNET/grus/internal/templates"
)

var _ Service = (*Controller)(nil)

const (
	defaultMaxInstances       = 10
	defaultInterval           = 5 * time.Second
	defaultStaticRestartDelay = 10 * time.Second
	statusPublishInterval     = time.Second
	maxIDAttempts             = 5
	idSuffixLen               = 5
	idAlphabet                = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// Config holds the controller settings. Zero values fall back to the defaults noted per field.
type Config struct {
	PublicURL          string        // GRUS_URL given to instances
	MaxInstances       int           // hard limit of non-terminal instances over all pools (default 10)
	Interval           time.Duration // reconcile interval (default 5s)
	StaticRestartDelay time.Duration // Stopped static instance record kept this long before it may be recreated (default 10s)
	Clock              model.Clock   // default model.SystemClock
	Logger             *slog.Logger  // default slog.Default()
}

// Controller owns every state mutation. One mutex serialises API calls and the reconcile loop.
type Controller struct {
	cfg   Config
	st    store.Store
	drv   driver.Driver
	tpl   templates.Store
	pub   model.Publisher
	clock model.Clock
	log   *slog.Logger

	mu            sync.Mutex
	ready         atomic.Bool
	trigger       chan struct{}
	belowSince    map[string]*time.Time // per pool: when desired < active was first seen
	lastPublished map[string]time.Time  // per instance: last instance.updated from SelfStatus
	resolveWarned map[string]string     // per pool: last unresolvable reason already reported
	lastNetwork   model.NetworkStatus   // last network.players payload
	adoptHosts    map[string]bool       // hosts whose unknown instances are adopted once they can be listed
}

// New builds a controller. Call Adopt (or Run, which adopts first) before reconciling.
func New(cfg Config, st store.Store, drv driver.Driver, tpl templates.Store, pub model.Publisher) *Controller {
	if cfg.MaxInstances <= 0 {
		cfg.MaxInstances = defaultMaxInstances
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.StaticRestartDelay <= 0 {
		cfg.StaticRestartDelay = defaultStaticRestartDelay
	}
	if cfg.Clock == nil {
		cfg.Clock = model.SystemClock{}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Controller{
		cfg:           cfg,
		st:            st,
		drv:           drv,
		tpl:           tpl,
		pub:           pub,
		clock:         cfg.Clock,
		log:           cfg.Logger,
		trigger:       make(chan struct{}, 1),
		belowSince:    make(map[string]*time.Time),
		lastPublished: make(map[string]time.Time),
		resolveWarned: make(map[string]string),
		adoptHosts:    make(map[string]bool),
	}
}

// Run adopts the observed instances once, marks the controller ready and then reconciles on every tick and
// every Trigger until ctx ends.
func (c *Controller) Run(ctx context.Context) error {
	if err := c.Adopt(ctx); err != nil {
		return fmt.Errorf("adopt instances: %w", err)
	}
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-c.trigger:
		}
		if err := c.ReconcileOnce(ctx); err != nil {
			c.log.Error("reconcile failed", "error", err)
		}
	}
}

// Trigger wakes the reconcile loop. It never blocks; a pending wake-up is enough.
func (c *Controller) Trigger() {
	select {
	case c.trigger <- struct{}{}:
	default:
	}
}

// Ready reports whether Adopt has succeeded.
func (c *Controller) Ready() bool { return c.ready.Load() }

// Adopt creates store records for driver instances that have none, so a restart takes over running
// instances instead of stopping them. Nothing is stopped here. Hosts that cannot be listed yet (agents that
// have not reconnected) do not block readiness: their instances are adopted by the reconcile loop as soon
// as the host can be listed.
func (c *Controller) Adopt(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	observed, err := c.drv.List(ctx)
	if err != nil {
		var partial unlistedHostsError
		if !errors.As(err, &partial) {
			return fmt.Errorf("list driver instances: %w", err)
		}
		for _, host := range partial.FailedHosts() {
			c.adoptHosts[host] = true
		}
		c.log.Warn("some hosts cannot be listed yet, adopting their instances later", "hosts", partial.FailedHosts())
	}
	adopted, err := c.adoptObserved(ctx, observed)
	if err != nil {
		return err
	}
	c.ready.Store(true)
	c.log.Info("adopted instances", "count", adopted)
	return nil
}

// MarkAdoptHost makes the reconcile loop adopt the unknown instances of host once it can be listed. It is
// meant for hosts that appear at runtime, such as an agent that registers for the first time.
func (c *Controller) MarkAdoptHost(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adoptHosts[host] = true
}

// adoptObserved creates records for the observed instances that have none and returns how many it created.
func (c *Controller) adoptObserved(ctx context.Context, observed []driver.Observed) (int, error) {
	now := c.clock.Now()
	adopted := 0
	for _, o := range observed {
		if o.InstanceID == "" || o.Phase == driver.PhaseNotExists {
			continue
		}
		_, err := c.st.GetInstance(ctx, o.InstanceID)
		if err == nil {
			continue
		}
		if !errors.Is(err, store.ErrNotFound) {
			return adopted, fmt.Errorf("load instance %s: %w", o.InstanceID, err)
		}
		created := o.StartedAt
		if created.IsZero() {
			created = now
		}
		inst := model.Instance{
			ID:             o.InstanceID,
			Pool:           o.Pool,
			Role:           o.Role,
			PoolGeneration: o.PoolGeneration,
			Host:           hostOfRef(o.Ref, c.drv.Host()),
			DriverRef:      o.Ref,
			Address:        o.Address,
			StateSince:     now,
			CreatedAt:      created,
			TokenHash:      o.TokenHash,
		}
		switch o.Phase {
		case driver.PhaseRunning:
			inst.State = model.StateReady
			hb := now
			inst.LastHeartbeat = &hb
		case driver.PhasePending:
			inst.State = model.StateStarting
		default:
			inst.State = model.StateStopped
			if err := c.kill(ctx, o.Ref); err != nil {
				c.log.Error("kill adopted instance", "instance", o.InstanceID, "error", err)
			}
		}
		if err := c.st.CreateInstance(ctx, inst); err != nil {
			return adopted, fmt.Errorf("adopt instance %s: %w", inst.ID, err)
		}
		c.pub.Publish(model.EventInstanceCreated, inst)
		adopted++
	}
	return adopted, nil
}

// adoptLater adopts the unknown instances of the marked hosts that can be listed now.
func (c *Controller) adoptLater(ctx context.Context, observed []driver.Observed, unlisted map[string]bool) error {
	var due []driver.Observed
	ready := map[string]bool{}
	for host := range c.adoptHosts {
		if !unlisted[host] {
			ready[host] = true
		}
	}
	if len(ready) == 0 {
		return nil
	}
	for _, o := range observed {
		if ready[hostOfRef(o.Ref, "")] {
			due = append(due, o)
		}
	}
	n, err := c.adoptObserved(ctx, due)
	if err != nil {
		return err
	}
	for host := range ready {
		delete(c.adoptHosts, host)
	}
	if n > 0 {
		c.log.Info("adopted instances of reconnected hosts", "count", n)
	}
	return nil
}

// hostOfRef returns the host part of a router ref "<host>/<ref>", or fallback for plain refs.
func hostOfRef(ref, fallback string) string {
	if host, _, ok := strings.Cut(ref, "/"); ok {
		return host
	}
	return fallback
}

// apply runs one lifecycle input. When the state changes it persists the instance and publishes
// instance.state. Callers that changed other fields must persist them themselves when the state stays.
func (c *Controller) apply(ctx context.Context, inst model.Instance, in state.Input, reason string) (model.Instance, error) {
	from := inst.State
	next, err := state.Transition(inst, in, reason, c.clock.Now())
	if err != nil {
		return inst, err
	}
	if next.State == from {
		return next, nil
	}
	if err := c.st.UpdateInstance(ctx, next); err != nil {
		return inst, fmt.Errorf("persist instance %s: %w", next.ID, err)
	}
	c.pub.Publish(model.EventInstanceState, model.InstanceStateData{
		Instance: next.ID,
		Pool:     next.Pool,
		Role:     next.Role,
		From:     from,
		To:       next.State,
		Address:  next.Address,
		Reason:   reason,
	})
	if next.Role == model.RoleProxy {
		c.publishNetwork(ctx)
	}
	return next, nil
}

// applyWithFields applies in after the caller changed non-state fields; the record is persisted either way.
func (c *Controller) applyWithFields(ctx context.Context, inst model.Instance, in state.Input, reason string) (model.Instance, error) {
	from := inst.State
	updated, err := c.apply(ctx, inst, in, reason)
	if err != nil {
		return inst, err
	}
	if updated.State == from {
		if err := c.st.UpdateInstance(ctx, updated); err != nil {
			return inst, fmt.Errorf("persist instance %s: %w", updated.ID, err)
		}
	}
	return updated, nil
}

// stop asks the driver to stop the instance and then applies the input (InStop or InShutdown). The driver
// is called first, so a failed stop leaves the state unchanged and the next cycle retries.
func (c *Controller) stop(ctx context.Context, inst model.Instance, in state.Input, reason string, lc model.Lifecycle) (model.Instance, error) {
	if !state.Can(inst.State, in) {
		return inst, fmt.Errorf("%w: %s from %s", state.ErrInvalidTransition, in, inst.State)
	}
	if inst.DriverRef != "" {
		err := c.drv.Stop(ctx, inst.DriverRef, lc.StopCommand, lc.GracePeriod.D())
		if err != nil && !errors.Is(err, driver.ErrNotFound) {
			return inst, fmt.Errorf("stop instance %s: %w", inst.ID, err)
		}
	}
	return c.apply(ctx, inst, in, reason)
}

// kill removes the driver instance; a missing instance counts as removed.
func (c *Controller) kill(ctx context.Context, ref string) error {
	if ref == "" {
		return nil
	}
	if err := c.drv.Kill(ctx, ref); err != nil && !errors.Is(err, driver.ErrNotFound) {
		return fmt.Errorf("kill %s: %w", ref, err)
	}
	return nil
}

// lifecycleOf returns the defaulted lifecycle of a pool, or the defaults when the pool does not exist.
func (c *Controller) lifecycleOf(ctx context.Context, pool string) model.Lifecycle {
	p, err := c.st.GetPool(ctx, pool)
	if err != nil {
		return model.PoolSpec{}.WithDefaults().Lifecycle
	}
	lc := p.Spec.WithDefaults().Lifecycle
	if eff, err := c.effective(ctx, p.Spec); err == nil {
		lc.StopCommand = eff.StopCommand
		lc.GracePeriod = eff.GracePeriod
	}
	return lc
}

// createInstance records a new Requested instance, resolves templates, schedules it and starts it through
// the driver. Template and start failures end in Failed, not in an error; only storage errors are returned.
func (c *Controller) createInstance(ctx context.Context, pool model.Pool) (model.Instance, error) {
	spec := pool.Spec.WithDefaults()
	now := c.clock.Now()

	eff, err := c.effective(ctx, pool.Spec)
	c.noteResolve(pool.Name, err)
	if err != nil {
		return model.Instance{}, err
	}

	token, err := newToken()
	if err != nil {
		return model.Instance{}, err
	}
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])

	inst, err := c.insertRequested(ctx, pool, spec, now, tokenHash)
	if err != nil {
		return model.Instance{}, err
	}

	resolved, err := c.resolveTemplates(ctx, eff.Templates)
	if err != nil {
		return c.apply(ctx, inst, state.InFailed, "template: "+err.Error())
	}

	inst, err = c.apply(ctx, inst, state.InScheduled, "")
	if err != nil {
		return inst, err
	}
	ref, err := c.drv.Start(ctx, driver.StartSpec{
		Instance:  inst,
		Pool:      model.Pool{Name: pool.Name, Spec: spec, Generation: pool.Generation},
		Token:     token,
		Effective: eff,
		Env:       instanceEnv(c.cfg.PublicURL, spec, eff, inst),
		Templates: resolved,
	})
	if err != nil {
		c.log.Error("start instance failed", "instance", inst.ID, "pool", inst.Pool, "error", err)
		return c.apply(ctx, inst, state.InFailed, "start: "+err.Error())
	}
	inst.DriverRef = ref
	inst.Host = hostOfRef(ref, inst.Host)
	return c.apply(ctx, inst, state.InPreparing, "")
}

// insertRequested stores a new Requested instance with a fresh id and publishes instance.created.
func (c *Controller) insertRequested(ctx context.Context, pool model.Pool, spec model.PoolSpec, now time.Time, tokenHash string) (model.Instance, error) {
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		id, err := c.newID(ctx, pool.Name, spec.Mode)
		if err != nil {
			return model.Instance{}, err
		}
		inst := model.Instance{
			ID:             id,
			Pool:           pool.Name,
			Role:           spec.Role,
			PoolGeneration: pool.Generation,
			Host:           c.drv.Host(),
			State:          model.StateRequested,
			StateSince:     now,
			CreatedAt:      now,
			MaxPlayers:     spec.Scaling.Capacity,
			TokenHash:      tokenHash,
		}
		err = c.st.CreateInstance(ctx, inst)
		if err == nil {
			c.pub.Publish(model.EventInstanceCreated, inst)
			return inst, nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return model.Instance{}, fmt.Errorf("create instance %s: %w", id, err)
		}
	}
	return model.Instance{}, fmt.Errorf("create instance of pool %s: no free id after %d attempts", pool.Name, maxIDAttempts)
}

// newID returns "<pool>-<5 random chars>" for dynamic pools and the lowest free "<pool>-<n>" for static ones.
func (c *Controller) newID(ctx context.Context, pool string, mode model.Mode) (string, error) {
	if mode == model.ModeStatic {
		for n := 1; ; n++ {
			id := fmt.Sprintf("%s-%d", pool, n)
			_, err := c.st.GetInstance(ctx, id)
			if errors.Is(err, store.ErrNotFound) {
				return id, nil
			}
			if err != nil {
				return "", fmt.Errorf("check instance id %s: %w", id, err)
			}
		}
	}
	suffix := make([]byte, idSuffixLen)
	limit := big.NewInt(int64(len(idAlphabet)))
	for i := range suffix {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("random instance id: %w", err)
		}
		suffix[i] = idAlphabet[n.Int64()]
	}
	return pool + "-" + string(suffix), nil
}

func (c *Controller) resolveTemplates(ctx context.Context, refs []model.TemplateRef) ([]driver.ResolvedTemplate, error) {
	out := make([]driver.ResolvedTemplate, 0, len(refs))
	for _, r := range refs {
		ref := r.Tag
		if r.Digest != "" {
			ref = r.Digest
		}
		digest, err := c.tpl.Resolve(ctx, r.Name, ref)
		if err != nil {
			return nil, fmt.Errorf("resolve template %s@%s: %w", r.Name, ref, err)
		}
		out = append(out, driver.ResolvedTemplate{Name: r.Name, Digest: digest})
	}
	return out, nil
}

// instanceEnv builds the GRUS_* variables on top of the pool env. The token is added by the driver.
func instanceEnv(publicURL string, spec model.PoolSpec, eff model.EffectiveSpec, inst model.Instance) map[string]string {
	env := make(map[string]string, len(eff.Env)+6)
	for k, v := range eff.Env {
		env[k] = v
	}
	env["GRUS_URL"] = publicURL
	env["GRUS_INSTANCE"] = inst.ID
	env["GRUS_POOL"] = inst.Pool
	env["GRUS_ROLE"] = string(inst.Role)
	env["GRUS_PORT"] = fmt.Sprint(eff.Port)
	if spec.Scaling.Capacity > 0 {
		env["GRUS_MAX_PLAYERS"] = fmt.Sprint(spec.Scaling.Capacity)
	}
	return env
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
