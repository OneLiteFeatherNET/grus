// Package fake provides an in-memory driver.Driver for controller tests. It never starts anything:
// tests script the observed world with SetPhase, Remove and Add, and inspect what the controller asked
// for with Started, Stopped, Killed and Commands.
package fake

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
)

// Driver is an in-memory driver. The zero value is not usable; create it with New.
// All methods are safe for concurrent use.
type Driver struct {
	// StartErr, when set, makes Start fail with this error and record nothing.
	// Set it before the code under test runs.
	StartErr error
	// ListErr, when set, makes List fail with this error. Set it before the code under test runs.
	ListErr error

	mu        sync.Mutex
	host      string
	nextID    int
	instances map[string]driver.Observed
	logs      map[string][]string
	started   []driver.StartSpec
	stopped   []string
	killed    []string
	commands  map[string][]string
}

var _ driver.Driver = (*Driver)(nil)

// New returns an empty fake driver that represents the given host id.
func New(host string) *Driver {
	return &Driver{
		host:      host,
		instances: make(map[string]driver.Observed),
		logs:      make(map[string][]string),
		commands:  make(map[string][]string),
	}
}

func (d *Driver) Name() string { return "fake" }

func (d *Driver) Host() string { return d.host }

// Start records the spec and creates an instance in phase Pending without an address. The returned
// ref is the instance id; a spec without id gets a generated one. Starting an id that already exists
// fails, like the real drivers do.
func (d *Driver) Start(_ context.Context, spec driver.StartSpec) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.StartErr != nil {
		return "", d.StartErr
	}
	ref := spec.Instance.ID
	if ref == "" {
		d.nextID++
		ref = fmt.Sprintf("fake-%d", d.nextID)
	}
	if _, exists := d.instances[ref]; exists {
		return "", fmt.Errorf("fake: instance %q already exists", ref)
	}

	d.instances[ref] = driver.Observed{
		Ref:            ref,
		InstanceID:     spec.Instance.ID,
		Pool:           spec.Instance.Pool,
		Role:           spec.Instance.Role,
		Mode:           spec.Pool.Spec.Mode,
		PoolGeneration: spec.Instance.PoolGeneration,
		TokenHash:      spec.Instance.TokenHash,
		Phase:          driver.PhasePending,
	}
	d.started = append(d.started, copySpec(spec))
	return ref, nil
}

// Stop only records the request. Tests change the phase with SetPhase or Remove.
func (d *Driver) Stop(_ context.Context, ref string, stopCommand string, _ time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.instances[ref]; !ok {
		return driver.ErrNotFound
	}
	d.stopped = append(d.stopped, ref)
	return nil
}

// Kill only records the request. Tests change the phase with SetPhase or Remove.
func (d *Driver) Kill(_ context.Context, ref string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.instances[ref]; !ok {
		return driver.ErrNotFound
	}
	d.killed = append(d.killed, ref)
	return nil
}

// List returns all instances sorted by ref.
func (d *Driver) List(_ context.Context) ([]driver.Observed, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ListErr != nil {
		return nil, d.ListErr
	}
	out := make([]driver.Observed, 0, len(d.instances))
	for _, ref := range slices.Sorted(maps.Keys(d.instances)) {
		out = append(out, d.instances[ref])
	}
	return out, nil
}

// Logs returns the lines set with SetLogs. Tail keeps the last n lines when n > 0; follow is ignored
// because the fake produces no new lines.
func (d *Driver) Logs(_ context.Context, ref string, tail int, _ bool) (io.ReadCloser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.instances[ref]; !ok {
		return nil, driver.ErrNotFound
	}
	lines := d.logs[ref]
	if tail > 0 && tail < len(lines) {
		lines = lines[len(lines)-tail:]
	}
	text := strings.Join(lines, "\n")
	if len(lines) > 0 {
		text += "\n"
	}
	return io.NopCloser(strings.NewReader(text)), nil
}

// Command records one console line for the instance.
func (d *Driver) Command(_ context.Context, ref string, line string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.instances[ref]; !ok {
		return driver.ErrNotFound
	}
	d.commands[ref] = append(d.commands[ref], line)
	return nil
}

// SetPhase changes the phase and address of an existing instance, as if the runtime reported it.
// Unknown refs are ignored.
func (d *Driver) SetPhase(ref string, p driver.Phase, address string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	o, ok := d.instances[ref]
	if !ok {
		return
	}
	o.Phase = p
	o.Address = address
	d.instances[ref] = o
}

// Remove drops an instance, as if its pod or process vanished.
func (d *Driver) Remove(ref string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.instances, ref)
}

// Add inserts a pre-existing instance, for adoption tests. An empty Ref falls back to InstanceID.
func (d *Driver) Add(o driver.Observed) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if o.Ref == "" {
		o.Ref = o.InstanceID
	}
	d.instances[o.Ref] = o
}

// SetLogs sets the lines that Logs returns for ref.
func (d *Driver) SetLogs(ref string, lines []string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.logs[ref] = slices.Clone(lines)
}

// Started returns a copy of every successfully started spec, in call order.
func (d *Driver) Started() []driver.StartSpec {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]driver.StartSpec, len(d.started))
	for i, s := range d.started {
		out[i] = copySpec(s)
	}
	return out
}

// Stopped returns the refs passed to Stop, in call order.
func (d *Driver) Stopped() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.stopped)
}

// Killed returns the refs passed to Kill, in call order.
func (d *Driver) Killed() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.killed)
}

// Commands returns a copy of the console lines per ref.
func (d *Driver) Commands() map[string][]string {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make(map[string][]string, len(d.commands))
	for ref, lines := range d.commands {
		out[ref] = slices.Clone(lines)
	}
	return out
}

// copySpec copies the parts of a spec that the fake's callers could mutate.
func copySpec(s driver.StartSpec) driver.StartSpec {
	s.Env = maps.Clone(s.Env)
	s.Templates = slices.Clone(s.Templates)
	return s
}
