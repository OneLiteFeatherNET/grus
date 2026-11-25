// Package router implements driver.Driver over several drivers, one per host. Refs returned by Start are
// "<host>/<ref>", so every later call goes to the driver that started the instance. Hosts can join and leave
// at runtime, for example agents that register themselves.
package router

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
)

// Selector picks the host that runs a new instance.
type Selector func(spec driver.StartSpec) (host string, err error)

// PartialListError is returned by List when some hosts could not be listed. The instances of the other
// hosts are still returned. Callers must not treat instances of the failed hosts as gone.
type PartialListError struct {
	Hosts []string // hosts whose List failed, sorted
	Err   error    // the joined host errors
}

func (e *PartialListError) Error() string {
	return fmt.Sprintf("router: list failed on hosts %s: %v", strings.Join(e.Hosts, ", "), e.Err)
}

func (e *PartialListError) Unwrap() error { return e.Err }

// FailedHosts returns the hosts whose List failed.
func (e *PartialListError) FailedHosts() []string { return slices.Clone(e.Hosts) }

// ErrDuplicateHost is returned by Add when a driver for the host is already registered.
var ErrDuplicateHost = errors.New("router: host already registered")

// ErrNoHost is returned by Start when the selector or the default selector cannot pick a host.
var ErrNoHost = errors.New("router: no host can run the instance")

// Router is a driver.Driver over named sub-drivers keyed by host id. It is safe for concurrent use.
type Router struct {
	selector Selector

	mu      sync.RWMutex
	drivers map[string]driver.Driver
}

var _ driver.Driver = (*Router)(nil)

// New returns an empty router. A nil selector uses the default one: the single host whose driver Name
// matches spec.Pool.Spec.Driver.
func New(selector Selector) *Router {
	r := &Router{drivers: make(map[string]driver.Driver)}
	if selector == nil {
		selector = r.defaultSelector
	}
	r.selector = selector
	return r
}

func (r *Router) Name() string { return "router" }

func (r *Router) Host() string { return "*" }

// Add registers the driver under its host id. It fails for an empty host, a host containing "/" (it would
// break the ref format) and a host that is already registered.
func (r *Router) Add(d driver.Driver) error {
	host := d.Host()
	if host == "" || strings.Contains(host, "/") {
		return fmt.Errorf("router: invalid host id %q", host)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.drivers[host]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicateHost, host)
	}
	r.drivers[host] = d
	return nil
}

// Remove unregisters a host. Its instances are no longer reachable through the router and vanish from List.
// Callers decide when a host is gone; the router does not time hosts out.
func (r *Router) Remove(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.drivers, host)
}

// Start asks the selector for a host and starts the instance there.
func (r *Router) Start(ctx context.Context, spec driver.StartSpec) (string, error) {
	host, err := r.selector(spec)
	if err != nil {
		return "", err
	}
	d, ok := r.lookup(host)
	if !ok {
		return "", fmt.Errorf("%w: unknown host %q", ErrNoHost, host)
	}
	ref, err := d.Start(ctx, spec)
	if err != nil {
		return "", err
	}
	return host + "/" + ref, nil
}

func (r *Router) Stop(ctx context.Context, ref string, stopCommand string, grace time.Duration) error {
	d, sub, err := r.route(ref)
	if err != nil {
		return err
	}
	return d.Stop(ctx, sub, stopCommand, grace)
}

func (r *Router) Kill(ctx context.Context, ref string) error {
	d, sub, err := r.route(ref)
	if err != nil {
		return err
	}
	return d.Kill(ctx, sub)
}

func (r *Router) Logs(ctx context.Context, ref string, tail int, follow bool) (io.ReadCloser, error) {
	d, sub, err := r.route(ref)
	if err != nil {
		return nil, err
	}
	return d.Logs(ctx, sub, tail, follow)
}

func (r *Router) Command(ctx context.Context, ref string, line string) error {
	d, sub, err := r.route(ref)
	if err != nil {
		return err
	}
	return d.Command(ctx, sub, line)
}

// List merges the instances of all hosts, with refs prefixed by the host. A host whose List fails is left out
// and reported in a *PartialListError; the instances of the healthy hosts are still returned.
func (r *Router) List(ctx context.Context) ([]driver.Observed, error) {
	r.mu.RLock()
	hosts := slices.Sorted(maps.Keys(r.drivers))
	snapshot := make([]driver.Driver, len(hosts))
	for i, h := range hosts {
		snapshot[i] = r.drivers[h]
	}
	r.mu.RUnlock()

	var out []driver.Observed
	var failed []string
	var errs []error
	for i, d := range snapshot {
		observed, err := d.List(ctx)
		if err != nil {
			failed = append(failed, hosts[i])
			errs = append(errs, fmt.Errorf("host %s: %w", hosts[i], err))
			continue
		}
		for _, o := range observed {
			if o.Ref != "" {
				o.Ref = hosts[i] + "/" + o.Ref
			}
			out = append(out, o)
		}
	}
	if len(failed) > 0 {
		return out, &PartialListError{Hosts: failed, Err: errors.Join(errs...)}
	}
	return out, nil
}

// defaultSelector picks the only host whose driver Name equals the pool's driver.
func (r *Router) defaultSelector(spec driver.StartSpec) (string, error) {
	want := spec.Pool.Spec.Driver
	if want == "" {
		return "", fmt.Errorf("%w: pool %s names no driver", ErrNoHost, spec.Pool.Name)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matches []string
	for host, d := range r.drivers {
		if d.Name() == want {
			matches = append(matches, host)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: no host with driver %s", ErrNoHost, want)
	case 1:
		return matches[0], nil
	default:
		slices.Sort(matches)
		return "", fmt.Errorf("%w: driver %s is on several hosts (%s)", ErrNoHost, want, strings.Join(matches, ", "))
	}
}

// route splits a ref into the driver of its host and the sub-ref. Unknown hosts and refs without a host
// part yield driver.ErrNotFound.
func (r *Router) route(ref string) (driver.Driver, string, error) {
	host, sub, ok := strings.Cut(ref, "/")
	if !ok {
		return nil, "", driver.ErrNotFound
	}
	d, found := r.lookup(host)
	if !found {
		return nil, "", driver.ErrNotFound
	}
	return d, sub, nil
}

func (r *Router) lookup(host string) (driver.Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.drivers[host]
	return d, ok
}
