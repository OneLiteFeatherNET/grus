package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

const (
	// DefaultHeartbeatTimeout is how long an agent may stay silent before its host counts as offline.
	DefaultHeartbeatTimeout = 30 * time.Second
	// DefaultMaxWait caps the long poll.
	DefaultMaxWait = 60 * time.Second
	// maxLogLines is how many lines per instance are kept from an agent report.
	maxLogLines = 500
)

// ErrOffline is returned when an agent host has not been heard of within the heartbeat timeout.
var ErrOffline = errors.New("remote: agent host offline")

// Config configures the driver of one agent host.
type Config struct {
	Host             string
	HeartbeatTimeout time.Duration // default DefaultHeartbeatTimeout
	Clock            model.Clock   // default model.SystemClock
	// After is the timer source of the long poll; tests replace it. Default time.After.
	After func(time.Duration) <-chan time.Time
}

// entry is one desired instance plus the revisions needed to tell when the agent has caught up.
type entry struct {
	DesiredInstance
	startRev int64 // revision that added the instance; 0 for instances adopted from a report
	killRev  int64
}

// Driver is the driver of one agent host. It is safe for concurrent use.
type Driver struct {
	cfg Config

	mu        sync.Mutex
	info      HostInfo
	lastSeen  time.Time
	seen      bool
	revision  int64
	changed   chan struct{} // closed and replaced whenever the desired state changes
	desired   map[string]*entry
	commands  []Command
	cmdSeq    int64
	reported  bool
	reportRev int64
	observed  map[string]ObservedInstance
	logs      map[string][]string
}

var _ driver.Driver = (*Driver)(nil)

// New returns the driver of an agent host. Until the agent reports, List fails.
func New(cfg Config) *Driver {
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = DefaultHeartbeatTimeout
	}
	if cfg.Clock == nil {
		cfg.Clock = model.SystemClock{}
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	return &Driver{
		cfg:      cfg,
		info:     HostInfo{Name: cfg.Host},
		revision: cfg.Clock.Now().UnixMilli(),
		cmdSeq:   cfg.Clock.Now().UnixMilli(), // later than any sequence of a previous controller run
		changed:  make(chan struct{}),
		desired:  make(map[string]*entry),
		observed: make(map[string]ObservedInstance),
		logs:     make(map[string][]string),
	}
}

func (d *Driver) Name() string { return model.DriverAgent }

func (d *Driver) Host() string { return d.cfg.Host }

// Online reports whether the agent has been heard of within the heartbeat timeout.
func (d *Driver) Online() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.onlineLocked()
}

func (d *Driver) onlineLocked() bool {
	return d.seen && d.cfg.Clock.Now().Sub(d.lastSeen) <= d.cfg.HeartbeatTimeout
}

// Info returns the host description.
func (d *Driver) Info() HostInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	info := d.info
	info.Online = d.onlineLocked()
	if d.seen {
		t := d.lastSeen
		info.LastSeen = &t
	}
	info.Instances = len(d.observed)
	return info
}

// Register records the host description and counts as a heartbeat.
func (d *Driver) Register(req RegisterRequest) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.touchLocked()
	d.info.Version = req.Version
	d.info.Labels = maps.Clone(req.Labels)
	d.info.Capacity = req.Capacity
	d.info.Address = req.Address
	d.info.PortRange = req.PortRange
}

func (d *Driver) touchLocked() {
	d.lastSeen = d.cfg.Clock.Now()
	d.seen = true
}

// bumpLocked starts a new desired revision and wakes every long poll.
func (d *Driver) bumpLocked() int64 {
	d.revision++
	close(d.changed)
	d.changed = make(chan struct{})
	return d.revision
}

// Start records the instance as desired. The agent starts it on its next poll.
func (d *Driver) Start(_ context.Context, spec driver.StartSpec) (string, error) {
	id := spec.Instance.ID
	if id == "" || strings.ContainsAny(id, "/\\") {
		return "", fmt.Errorf("remote: invalid instance id %q", id)
	}
	eff := spec.EffectiveOrV1()
	if len(eff.Command) == 0 {
		return "", errors.New("remote: effective spec has no command")
	}
	refs := make([]TemplateVersion, len(spec.Templates))
	for i, t := range spec.Templates {
		refs[i] = TemplateVersion{Name: t.Name, Digest: t.Digest}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.onlineLocked() {
		return "", fmt.Errorf("%w: %s", ErrOffline, d.cfg.Host)
	}
	if _, ok := d.desired[id]; ok {
		return "", fmt.Errorf("remote: instance %q already exists on %s", id, d.cfg.Host)
	}
	e := &entry{DesiredInstance: DesiredInstance{
		ID:         id,
		Pool:       spec.Instance.Pool,
		Role:       spec.Instance.Role,
		Mode:       spec.Pool.Spec.Mode,
		Generation: spec.Instance.PoolGeneration,
		Token:      spec.Token,
		TokenHash:  spec.Instance.TokenHash,
		Effective:  eff,
		Env:        maps.Clone(spec.Env),
		Templates:  refs,
		MemoryMiB:  spec.Pool.Spec.Resources.MemoryMiB,
	}}
	e.startRev = d.bumpLocked()
	d.desired[id] = e
	return id, nil
}

// Stop asks the agent to stop the instance; the agent runs the stop ladder (command, SIGTERM, SIGKILL).
func (d *Driver) Stop(_ context.Context, ref string, stopCommand string, grace time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.desired[ref]
	if !ok {
		return driver.ErrNotFound
	}
	if e.Stop.Requested || e.Kill {
		return nil
	}
	e.Stop = StopRequest{Requested: true, Command: stopCommand, GraceSeconds: int(grace.Seconds())}
	d.bumpLocked()
	return nil
}

// Kill asks the agent to kill the instance and forget it. The entry is dropped once the agent confirms.
func (d *Driver) Kill(_ context.Context, ref string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.desired[ref]
	if !ok {
		return driver.ErrNotFound
	}
	if e.Kill {
		return nil
	}
	e.Kill = true
	e.killRev = d.bumpLocked()
	return nil
}

// List returns the instances the agent last reported, plus instances started after the agent's last
// report. It fails while the agent is offline or has not reported yet, so the reconciler keeps the state.
func (d *Driver) List(_ context.Context) ([]driver.Observed, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.onlineLocked() {
		return nil, fmt.Errorf("%w: %s", ErrOffline, d.cfg.Host)
	}
	if !d.reported {
		return nil, fmt.Errorf("remote: agent host %s has not reported yet", d.cfg.Host)
	}
	out := make([]driver.Observed, 0, len(d.desired))
	for _, id := range slices.Sorted(maps.Keys(d.observed)) {
		o := d.observed[id]
		out = append(out, driver.Observed{
			Ref: id, InstanceID: id, Pool: o.Pool, Role: o.Role, Mode: o.Mode, PoolGeneration: o.Generation,
			TokenHash: o.TokenHash, Address: o.Address, Phase: o.Phase, Reason: o.Reason, StartedAt: o.StartedAt,
		})
	}
	for _, id := range slices.Sorted(maps.Keys(d.desired)) {
		e := d.desired[id]
		if _, reported := d.observed[id]; reported || e.Kill || e.startRev <= d.reportRev {
			continue
		}
		out = append(out, driver.Observed{
			Ref: id, InstanceID: id, Pool: e.Pool, Role: e.Role, Mode: e.Mode, PoolGeneration: e.Generation,
			TokenHash: e.TokenHash, Phase: driver.PhasePending,
		})
	}
	return out, nil
}

// Logs returns the cached tail of the instance's console. Following is not supported: the agent reports
// the tail every few seconds, so follow returns the same snapshot and ends.
func (d *Driver) Logs(_ context.Context, ref string, tail int, _ bool) (io.ReadCloser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.desired[ref]; !ok {
		return nil, driver.ErrNotFound
	}
	lines := d.logs[ref]
	if tail > 0 && tail < len(lines) {
		lines = lines[len(lines)-tail:]
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return io.NopCloser(strings.NewReader(b.String())), nil
}

// Command queues a console line; the agent delivers it on its next poll.
func (d *Driver) Command(_ context.Context, ref string, line string) error {
	if strings.ContainsAny(line, "\r\n") {
		return errors.New("remote: console line must not contain line breaks")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.desired[ref]; !ok {
		return driver.ErrNotFound
	}
	d.cmdSeq++
	d.commands = append(d.commands, Command{Seq: d.cmdSeq, Instance: ref, Line: line})
	d.bumpLocked()
	return nil
}

// Desired returns the desired state. When revision equals the current one it waits until the state
// changes, wait elapses or ctx ends, and then returns the current state either way. Asking counts as a
// heartbeat.
func (d *Driver) Desired(ctx context.Context, revision int64, wait time.Duration) Desired {
	d.mu.Lock()
	d.touchLocked()
	if revision == d.revision && wait > 0 {
		if wait > DefaultMaxWait {
			wait = DefaultMaxWait
		}
		changed := d.changed
		d.mu.Unlock()
		select {
		case <-changed:
		case <-d.cfg.After(wait):
		case <-ctx.Done():
		}
		d.mu.Lock()
		d.touchLocked()
	}
	defer d.mu.Unlock()

	out := Desired{Revision: d.revision, Instances: []DesiredInstance{}, Commands: slices.Clone(d.commands)}
	if out.Commands == nil {
		out.Commands = []Command{}
	}
	for _, id := range slices.Sorted(maps.Keys(d.desired)) {
		out.Instances = append(out.Instances, d.desired[id].DesiredInstance)
	}
	return out
}

// Observe stores the report of the agent. Instances the controller does not know yet are kept as desired
// entries so a restarted controller does not tell the agent to drop them.
func (d *Driver) Observe(rep ObservedReport) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.touchLocked()
	d.reported = true
	if rep.Revision > d.reportRev {
		d.reportRev = rep.Revision
	}
	d.observed = make(map[string]ObservedInstance, len(rep.Instances))
	for _, o := range rep.Instances {
		if o.ID == "" {
			continue
		}
		d.observed[o.ID] = o
		if _, ok := d.desired[o.ID]; !ok {
			d.desired[o.ID] = &entry{DesiredInstance: DesiredInstance{
				ID: o.ID, Pool: o.Pool, Role: o.Role, Mode: o.Mode, Generation: o.Generation, TokenHash: o.TokenHash,
			}}
		}
	}
	for id, e := range d.desired {
		if _, reported := d.observed[id]; !reported && e.Kill && e.killRev <= rep.Revision {
			delete(d.desired, id)
			delete(d.logs, id)
		}
	}
	d.logs = make(map[string][]string, len(rep.Logs))
	for id, lines := range rep.Logs {
		if len(lines) > maxLogLines {
			lines = lines[len(lines)-maxLogLines:]
		}
		d.logs[id] = slices.Clone(lines)
	}
	d.commands = slices.DeleteFunc(d.commands, func(c Command) bool { return c.Seq <= rep.CommandSeq })
}
