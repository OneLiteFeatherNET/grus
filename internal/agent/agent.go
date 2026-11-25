package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/driver/remote"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

const (
	registerInterval = 10 * time.Second
	reportInterval   = 5 * time.Second
	longPollWait     = 25 * time.Second
	maxBackoff       = 30 * time.Second
	logTail          = 100
	stopWait         = 60 * time.Second
	killTimeout      = 15 * time.Second
	pidFile          = "grus.pid"
)

// API is the controller as the agent sees it.
type API interface {
	Register(ctx context.Context, req remote.RegisterRequest) error
	Desired(ctx context.Context, revision int64, wait time.Duration) (remote.Desired, error)
	Observed(ctx context.Context, rep remote.ObservedReport) error
}

// Runner runs the instances; *process.Driver implements it.
type Runner interface {
	Start(ctx context.Context, spec driver.StartSpec) (string, error)
	Stop(ctx context.Context, ref string, stopCommand string, grace time.Duration) error
	Kill(ctx context.Context, ref string) error
	List(ctx context.Context) ([]driver.Observed, error)
	Logs(ctx context.Context, ref string, tail int, follow bool) (io.ReadCloser, error)
	Command(ctx context.Context, ref string, line string) error
	Forget(ref string) error
	Pid(ref string) (int, bool)
}

type failure struct {
	inst   remote.DesiredInstance
	reason string
}

// Agent applies the desired state of the controller to the local runner and reports what it observes.
// It is level based: every poll re-applies the whole desired state, so a lost message or a controller
// restart heals itself. Instances keep running while the controller is unreachable.
type Agent struct {
	cfg Config
	api API
	run Runner
	log *slog.Logger
	// spawn runs a start in the background; tests run it inline.
	spawn func(func())
	// baseCtx is the lifetime of background starts (template downloads), not of one poll.
	baseCtx context.Context

	wake chan struct{} // asks the reporter for an immediate report

	mu       sync.Mutex
	rev      int64 // desired revision applied last; -1 before the first poll
	cmdSeq   int64 // highest console command applied
	starting map[string]remote.DesiredInstance
	failed   map[string]failure
	stopped  map[string]bool
	known    map[string]remote.DesiredInstance // last desired state per instance
}

func New(ctx context.Context, cfg Config, api API, run Runner, log *slog.Logger) *Agent {
	return &Agent{
		cfg: cfg, api: api, run: run, log: log, baseCtx: ctx,
		spawn:    func(f func()) { go f() },
		wake:     make(chan struct{}, 1),
		rev:      -1,
		starting: map[string]remote.DesiredInstance{},
		failed:   map[string]failure{},
		stopped:  map[string]bool{},
		known:    map[string]remote.DesiredInstance{},
	}
}

func (a *Agent) poke() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// register sends the host description; it doubles as the heartbeat.
func (a *Agent) register(ctx context.Context) error {
	return a.api.Register(ctx, remote.RegisterRequest{
		Version:   a.cfg.Version,
		Address:   a.cfg.Address,
		PortRange: fmt.Sprintf("%d-%d", a.cfg.PortMin, a.cfg.PortMax),
	})
}

// poll fetches the desired state, waiting up to wait for a change, and applies it.
func (a *Agent) poll(ctx context.Context, wait time.Duration) error {
	a.mu.Lock()
	rev := a.rev
	a.mu.Unlock()
	d, err := a.api.Desired(ctx, rev, wait)
	if err != nil {
		return err
	}
	a.apply(ctx, d)
	a.mu.Lock()
	a.rev = d.Revision
	a.mu.Unlock()
	a.poke()
	return nil
}

// apply makes the local state match d: console commands, kills, stops and missing starts.
func (a *Agent) apply(ctx context.Context, d remote.Desired) {
	listed := map[string]driver.Observed{}
	if obs, err := a.run.List(ctx); err == nil {
		for _, o := range obs {
			listed[o.InstanceID] = o
		}
	}

	a.mu.Lock()
	seq := a.cmdSeq
	a.mu.Unlock()
	for _, c := range d.Commands {
		if c.Seq <= seq {
			continue
		}
		if err := a.run.Command(ctx, c.Instance, c.Line); err != nil {
			a.log.Warn("console command not delivered", "instance", c.Instance, "error", err)
		}
		seq = c.Seq
	}
	a.mu.Lock()
	a.cmdSeq = seq
	a.mu.Unlock()

	want := make(map[string]bool, len(d.Instances))
	for _, di := range d.Instances {
		want[di.ID] = true
		a.mu.Lock()
		a.known[di.ID] = di
		_, isStarting := a.starting[di.ID]
		_, isFailed := a.failed[di.ID]
		wasStopped := a.stopped[di.ID]
		a.mu.Unlock()
		o, present := listed[di.ID]

		switch {
		case di.Kill:
			a.kill(ctx, di, present)
		case present || isStarting:
			if di.Stop.Requested && present && o.Phase == driver.PhaseRunning && !wasStopped {
				grace := time.Duration(di.Stop.GraceSeconds) * time.Second
				if err := a.run.Stop(ctx, di.ID, di.Stop.Command, grace); err != nil {
					a.log.Warn("stop failed", "instance", di.ID, "error", err)
				} else {
					a.mu.Lock()
					a.stopped[di.ID] = true
					a.mu.Unlock()
				}
			}
		case isFailed || len(di.Effective.Command) == 0:
			// A failed start stays reported until the controller kills it; entries without a command
			// are instances the controller adopted from an earlier agent run.
		default:
			a.start(di)
		}
	}

	// Exited instances nobody asks for any more are cleaned up. Running ones are left alone: the
	// controller adopts them from the report.
	for id, o := range listed {
		if !want[id] && o.Phase == driver.PhaseExited {
			a.forget(id, o.Mode)
		}
	}
	a.mu.Lock()
	for id := range a.known {
		if !want[id] {
			delete(a.known, id)
			delete(a.failed, id)
			delete(a.stopped, id)
		}
	}
	a.mu.Unlock()
}

func (a *Agent) kill(ctx context.Context, di remote.DesiredInstance, present bool) {
	if present {
		kctx, cancel := context.WithTimeout(ctx, killTimeout)
		err := a.run.Kill(kctx, di.ID)
		cancel()
		if err != nil && !errors.Is(err, driver.ErrNotFound) {
			a.log.Warn("kill failed, retrying on the next poll", "instance", di.ID, "error", err)
			return
		}
		a.forget(di.ID, di.Mode)
	}
	a.mu.Lock()
	delete(a.failed, di.ID)
	delete(a.stopped, di.ID)
	a.mu.Unlock()
}

// forget drops the runner's record of an exited instance and removes the working directory of dynamic ones.
func (a *Agent) forget(id string, mode model.Mode) {
	if err := a.run.Forget(id); err != nil && !errors.Is(err, driver.ErrNotFound) {
		a.log.Warn("forget failed", "instance", id, "error", err)
		return
	}
	_ = os.Remove(filepath.Join(a.cfg.Dir, id, pidFile))
	if mode == model.ModeDynamic && id != "" && id != "." && id != ".." && filepath.Base(id) == id {
		if err := os.RemoveAll(filepath.Join(a.cfg.Dir, id)); err != nil {
			a.log.Warn("remove instance directory failed", "instance", id, "error", err)
		}
	}
}

func (a *Agent) start(di remote.DesiredInstance) {
	a.mu.Lock()
	a.starting[di.ID] = di
	a.mu.Unlock()
	a.spawn(func() {
		_, err := a.run.Start(a.baseCtx, a.startSpec(di))
		a.mu.Lock()
		delete(a.starting, di.ID)
		if err != nil {
			a.failed[di.ID] = failure{inst: di, reason: err.Error()}
		}
		a.mu.Unlock()
		if err != nil {
			a.log.Error("start failed", "instance", di.ID, "pool", di.Pool, "error", err)
		} else if pid, ok := a.run.Pid(di.ID); ok {
			_ = os.WriteFile(filepath.Join(a.cfg.Dir, di.ID, pidFile), []byte(strconv.Itoa(pid)), 0o600)
		}
		a.poke()
	})
}

// startSpec builds the driver spec: the controller's env, with GRUS_URL pointing at the URL this agent uses.
func (a *Agent) startSpec(di remote.DesiredInstance) driver.StartSpec {
	env := make(map[string]string, len(di.Env)+5)
	for k, v := range di.Effective.Env {
		env[k] = v
	}
	for k, v := range di.Env {
		env[k] = v
	}
	env["GRUS_URL"] = a.cfg.URL
	env["GRUS_INSTANCE"] = di.ID
	env["GRUS_POOL"] = di.Pool
	env["GRUS_ROLE"] = string(di.Role)
	refs := make([]driver.ResolvedTemplate, len(di.Templates))
	for i, t := range di.Templates {
		refs[i] = driver.ResolvedTemplate{Name: t.Name, Digest: t.Digest}
	}
	return driver.StartSpec{
		Instance: model.Instance{
			ID: di.ID, Pool: di.Pool, Role: di.Role, PoolGeneration: di.Generation, TokenHash: di.TokenHash,
		},
		Pool: model.Pool{Name: di.Pool, Spec: model.PoolSpec{
			Role: di.Role, Mode: di.Mode, Resources: model.Resources{MemoryMiB: di.MemoryMiB},
		}},
		Effective: di.Effective,
		Token:     di.Token,
		Env:       env,
		Templates: refs,
	}
}

// report sends the full observed state: running instances, starts in progress, failed starts and log tails.
func (a *Agent) report(ctx context.Context) error {
	obs, err := a.run.List(ctx)
	if err != nil {
		return fmt.Errorf("list instances: %w", err)
	}
	rep := remote.ObservedReport{Instances: []remote.ObservedInstance{}, Logs: map[string][]string{}}
	seen := map[string]bool{}
	for _, o := range obs {
		seen[o.InstanceID] = true
		rep.Instances = append(rep.Instances, remote.ObservedInstance{
			ID: o.InstanceID, Pool: o.Pool, Role: o.Role, Mode: o.Mode, Generation: o.PoolGeneration,
			TokenHash: o.TokenHash, Address: o.Address, Phase: o.Phase, Reason: o.Reason, StartedAt: o.StartedAt,
		})
		if lines := a.tail(ctx, o.Ref); len(lines) > 0 {
			rep.Logs[o.InstanceID] = lines
		}
	}
	a.mu.Lock()
	rep.Revision, rep.CommandSeq = a.rev, a.cmdSeq
	for id, di := range a.starting {
		if !seen[id] {
			rep.Instances = append(rep.Instances, observedOf(di, driver.PhasePending, ""))
		}
	}
	for id, f := range a.failed {
		if !seen[id] {
			rep.Instances = append(rep.Instances, observedOf(f.inst, driver.PhaseFailed, f.reason))
		}
	}
	a.mu.Unlock()
	if rep.Revision < 0 {
		rep.Revision = 0
	}
	return a.api.Observed(ctx, rep)
}

func observedOf(di remote.DesiredInstance, phase driver.Phase, reason string) remote.ObservedInstance {
	return remote.ObservedInstance{
		ID: di.ID, Pool: di.Pool, Role: di.Role, Mode: di.Mode, Generation: di.Generation,
		TokenHash: di.TokenHash, Phase: phase, Reason: reason,
	}
}

func (a *Agent) tail(ctx context.Context, ref string) []string {
	rc, err := a.run.Logs(ctx, ref, logTail, false)
	if err != nil {
		return nil
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil || len(raw) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// Run registers, polls and reports until ctx ends. Controller errors only delay the next attempt.
func (a *Agent) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.every(ctx, registerInterval, "register", a.register) }()
	go func() { defer wg.Done(); a.reportLoop(ctx) }()
	a.pollLoop(ctx)
	wg.Wait()
}

func (a *Agent) pollLoop(ctx context.Context) {
	backoff := time.Second
	failing := false
	for ctx.Err() == nil {
		err := a.poll(ctx, longPollWait)
		if err == nil {
			if failing {
				a.log.Info("controller reachable again")
				failing = false
			}
			backoff = time.Second
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if !failing {
			a.log.Warn("controller not reachable, instances keep running", "error", err)
			failing = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (a *Agent) reportLoop(ctx context.Context) {
	every := a.cfg.ReportInterval
	if every <= 0 {
		every = reportInterval
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	failing := false
	for {
		if err := a.report(ctx); err != nil && ctx.Err() == nil {
			if !failing {
				a.log.Warn("report failed", "error", err)
			}
			failing = true
		} else {
			failing = false
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-a.wake:
		}
	}
}

// every runs f now and then on every tick, logging a failure only when it starts.
func (a *Agent) every(ctx context.Context, interval time.Duration, what string, f func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failing := false
	for {
		if err := f(ctx); err != nil && ctx.Err() == nil {
			if !failing {
				a.log.Warn(what+" failed", "error", err)
			}
			failing = true
		} else {
			failing = false
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// StopAll asks every running instance to stop and waits until they are gone or stopWait passed.
func (a *Agent) StopAll(ctx context.Context) {
	obs, err := a.run.List(ctx)
	if err != nil {
		return
	}
	for _, o := range obs {
		if o.Phase != driver.PhaseRunning {
			continue
		}
		a.mu.Lock()
		di := a.known[o.InstanceID]
		a.mu.Unlock()
		grace := di.Effective.GracePeriod.D()
		if err := a.run.Stop(ctx, o.Ref, di.Effective.StopCommand, grace); err != nil {
			a.log.Warn("stop on exit failed", "instance", o.InstanceID, "error", err)
		}
	}
	deadline := time.After(stopWait)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		running := 0
		if obs, err := a.run.List(ctx); err == nil {
			for _, o := range obs {
				if o.Phase == driver.PhaseRunning {
					running++
				}
			}
		}
		if running == 0 {
			return
		}
		select {
		case <-deadline:
			return
		case <-ticker.C:
		}
	}
}

// NewClient returns the controller client of the agent host.
func NewClient(base, host, token string, httpClient *http.Client) *Client {
	return &Client{base: base, host: host, token: token, http: httpClient}
}

// Config is what the agent needs to run.
type Config struct {
	URL        string // controller URL the agent and its instances use
	Token      string // agent token
	Host       string
	Dir        string // instance working directory
	Address    string // IP advertised to proxies
	PortMin    int
	PortMax    int
	Java       string
	CAFile     string
	StopOnExit bool
	Version    string
	// ReportInterval is how often the agent reports without a change; zero means 5s.
	ReportInterval time.Duration
}
