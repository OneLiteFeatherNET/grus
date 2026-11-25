package remote

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver/router"
)

// Registry owns the drivers of all agent hosts and keeps them registered in a router. A host exists from
// Ensure (at startup for configured hosts, or on first contact of an agent) and never leaves: an agent that
// disappears makes its host offline, not unknown, so its instances keep their state.
type Registry struct {
	router *router.Router
	base   Config

	mu    sync.Mutex
	hosts map[string]*Driver
	onNew func(host string)
}

// NewRegistry returns a registry that adds its hosts to r. base supplies the heartbeat timeout, clock and
// timer source of every host driver; its Host is ignored.
func NewRegistry(r *router.Router, base Config) *Registry {
	return &Registry{router: r, base: base, hosts: make(map[string]*Driver)}
}

// OnNewHost sets a callback that runs for hosts created after startup, i.e. by first contact of an agent.
func (g *Registry) OnNewHost(fn func(host string)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onNew = fn
}

// Preregister adds hosts without any contact, so the router knows them and reports them as unlisted
// instead of unknown while their agent is away.
func (g *Registry) Preregister(hosts ...string) error {
	for _, h := range hosts {
		if _, _, err := g.ensure(h, false); err != nil {
			return err
		}
	}
	return nil
}

// Ensure returns the driver of host and creates it on first use.
func (g *Registry) Ensure(host string) (*Driver, error) {
	d, _, err := g.ensure(host, true)
	return d, err
}

func (g *Registry) ensure(host string, notify bool) (*Driver, bool, error) {
	g.mu.Lock()
	if d, ok := g.hosts[host]; ok {
		g.mu.Unlock()
		return d, false, nil
	}
	cfg := g.base
	cfg.Host = host
	d := New(cfg)
	if err := g.router.Add(d); err != nil {
		g.mu.Unlock()
		return nil, false, err
	}
	g.hosts[host] = d
	onNew := g.onNew
	g.mu.Unlock()
	if notify && onNew != nil {
		onNew(host)
	}
	return d, true, nil
}

// Online reports whether the host exists and its agent is within the heartbeat timeout.
func (g *Registry) Online(host string) bool {
	g.mu.Lock()
	d, ok := g.hosts[host]
	g.mu.Unlock()
	return ok && d.Online()
}

// Register records a registration or heartbeat of the host's agent.
func (g *Registry) Register(_ context.Context, host string, req RegisterRequest) (HostInfo, error) {
	d, err := g.Ensure(host)
	if err != nil {
		return HostInfo{}, err
	}
	d.Register(req)
	return d.Info(), nil
}

// Desired is the long poll of the host's agent.
func (g *Registry) Desired(ctx context.Context, host string, revision int64, wait time.Duration) (Desired, error) {
	d, err := g.Ensure(host)
	if err != nil {
		return Desired{}, err
	}
	return d.Desired(ctx, revision, wait), nil
}

// Observed stores the report of the host's agent.
func (g *Registry) Observed(_ context.Context, host string, rep ObservedReport) error {
	d, err := g.Ensure(host)
	if err != nil {
		return err
	}
	d.Observe(rep)
	return nil
}

// Hosts lists all hosts sorted by name.
func (g *Registry) Hosts() []HostInfo {
	g.mu.Lock()
	drivers := make([]*Driver, 0, len(g.hosts))
	for _, name := range slices.Sorted(maps.Keys(g.hosts)) {
		drivers = append(drivers, g.hosts[name])
	}
	g.mu.Unlock()
	out := make([]HostInfo, len(drivers))
	for i, d := range drivers {
		out[i] = d.Info()
	}
	return out
}

// Host returns one host.
func (g *Registry) Host(name string) (HostInfo, error) {
	g.mu.Lock()
	d, ok := g.hosts[name]
	g.mu.Unlock()
	if !ok {
		return HostInfo{}, fmt.Errorf("%w: host %s", ErrUnknownHost, name)
	}
	return d.Info(), nil
}

// ErrUnknownHost is returned by Host for a host that never registered and is not preregistered.
var ErrUnknownHost = fmt.Errorf("remote: unknown host")
