// Package process runs instances as plain OS processes on the local host (E2.2). It is meant for
// development and for hosts without a container runtime. Every instance gets its own process group,
// so stop escalation and kill reach the whole process tree. Linux only.
package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

const (
	// logRingSize is how many console lines Logs can return.
	logRingSize = 2000
	// subscriberBuffer is how many lines a follower may fall behind before lines are dropped for it.
	subscriberBuffer = 256
	// defaultTermWait is the time between SIGTERM and SIGKILL.
	defaultTermWait = 5 * time.Second
)

// Config configures the process driver.
type Config struct {
	// HostID is the host id this driver represents, e.g. "local".
	HostID string
	// BaseDir holds one working directory per instance: BaseDir/<instance id>.
	BaseDir string
	// FetchTemplates materialises the templates into dir before the process starts.
	FetchTemplates func(ctx context.Context, refs []driver.ResolvedTemplate, dir string) error
	// Logger receives driver logs. Nil means slog.Default().
	Logger *slog.Logger
	// Clock stamps StartedAt. Nil means model.SystemClock.
	Clock model.Clock
}

// Driver runs instances as local processes. It is safe for concurrent use.
type Driver struct {
	cfg Config
	// after is the timer source for stop escalation; tests replace it.
	after    func(time.Duration) <-chan time.Time
	termWait time.Duration

	mu    sync.Mutex
	procs map[string]*proc
}

var _ driver.Driver = (*Driver)(nil)

// proc is one instance. All fields are guarded by Driver.mu except done, which is closed once after
// Wait returned.
type proc struct {
	obs     driver.Observed
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{}
	changed chan struct{} // closed and replaced on every phase change
	lines   []string
	subs    map[chan string]struct{}
}

// New returns a process driver. It starts nothing until Start is called.
func New(cfg Config) *Driver {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Clock == nil {
		cfg.Clock = model.SystemClock{}
	}
	return &Driver{
		cfg:      cfg,
		after:    time.After,
		termWait: defaultTermWait,
		procs:    make(map[string]*proc),
	}
}

func (d *Driver) Name() string { return "process" }

func (d *Driver) Host() string { return d.cfg.HostID }

// Start prepares the working directory, fetches templates, picks a port and starts the command. The
// returned ref is the instance id. Starting an id that is still running is an error.
func (d *Driver) Start(ctx context.Context, spec driver.StartSpec) (string, error) {
	id := spec.Instance.ID
	if err := checkID(id); err != nil {
		return "", err
	}
	eff := spec.EffectiveOrV1()
	if len(eff.Command) == 0 {
		return "", errors.New("process: pool spec has no command")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	log := d.cfg.Logger.With("instance", id, "pool", spec.Instance.Pool)

	// Reserve the id before the slow work so two concurrent starts cannot both win.
	p := &proc{
		obs: driver.Observed{
			Ref:            id,
			InstanceID:     id,
			Pool:           spec.Instance.Pool,
			Role:           spec.Instance.Role,
			Mode:           spec.Pool.Spec.Mode,
			PoolGeneration: spec.Instance.PoolGeneration,
			TokenHash:      spec.Instance.TokenHash,
			Phase:          driver.PhasePending,
		},
		done:    make(chan struct{}),
		changed: make(chan struct{}),
		subs:    make(map[chan string]struct{}),
	}
	d.mu.Lock()
	if old, ok := d.procs[id]; ok && old.obs.Phase != driver.PhaseExited {
		d.mu.Unlock()
		return "", fmt.Errorf("process: instance %q already exists", id)
	}
	d.procs[id] = p
	d.mu.Unlock()

	fail := func(err error) (string, error) {
		d.mu.Lock()
		if d.procs[id] == p {
			delete(d.procs, id)
		}
		d.mu.Unlock()
		return "", err
	}

	workdir := filepath.Join(d.cfg.BaseDir, id)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return fail(fmt.Errorf("create workdir: %w", err))
	}
	if len(spec.Templates) > 0 {
		if d.cfg.FetchTemplates == nil {
			return fail(errors.New("process: templates requested but no FetchTemplates configured"))
		}
		if err := d.cfg.FetchTemplates(ctx, spec.Templates, workdir); err != nil {
			return fail(fmt.Errorf("fetch templates: %w", err))
		}
	}

	port, err := pickPort(spec)
	if err != nil {
		return fail(err)
	}

	consoleLog, err := os.OpenFile(filepath.Join(workdir, "console.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fail(fmt.Errorf("open console.log: %w", err))
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		consoleLog.Close()
		return fail(fmt.Errorf("create output pipe: %w", err))
	}

	args := append(slices.Clone(eff.Command[1:]), eff.Args...)
	cmd := exec.Command(eff.Command[0], args...)
	cmd.Dir = workdir
	cmd.Env = buildEnv(spec.Env, spec.Token, port)
	cmd.Stdout = outW
	cmd.Stderr = outW
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		outR.Close()
		outW.Close()
		consoleLog.Close()
		return fail(fmt.Errorf("create stdin pipe: %w", err))
	}
	if err := cmd.Start(); err != nil {
		outR.Close()
		outW.Close()
		consoleLog.Close()
		return fail(fmt.Errorf("start process: %w", err))
	}
	// The child holds its own copy of the write end now; closing ours lets the reader see EOF.
	outW.Close()

	d.mu.Lock()
	p.cmd = cmd
	p.stdin = stdin
	p.obs.Address = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	p.obs.StartedAt = d.cfg.Clock.Now()
	d.setPhaseLocked(p, driver.PhaseRunning, "")
	d.mu.Unlock()

	go d.readOutput(p, outR, consoleLog)
	go d.waitExit(p)

	log.Info("process started", "pid", cmd.Process.Pid, "port", port)
	return id, nil
}

// Stop writes the stop command if one is given and returns. In the background it waits up to grace
// for the process to exit, then sends SIGTERM to the process group, waits termWait and sends SIGKILL.
func (d *Driver) Stop(_ context.Context, ref string, stopCommand string, grace time.Duration) error {
	d.mu.Lock()
	p, ok := d.procs[ref]
	if !ok {
		d.mu.Unlock()
		return driver.ErrNotFound
	}
	exited := p.obs.Phase == driver.PhaseExited
	started := p.cmd != nil
	d.mu.Unlock()

	if exited {
		return nil
	}
	if !started {
		return fmt.Errorf("process: instance %q is not started yet", ref)
	}
	if stopCommand != "" {
		if err := d.writeStdin(p, stopCommand); err != nil {
			d.cfg.Logger.Warn("stop command not delivered", "instance", ref, "err", err)
		}
	}
	go d.escalate(ref, p, grace)
	return nil
}

// Kill sends SIGKILL to the process group and waits until the process has been reaped or ctx ends.
func (d *Driver) Kill(ctx context.Context, ref string) error {
	d.mu.Lock()
	p, ok := d.procs[ref]
	if !ok {
		d.mu.Unlock()
		return driver.ErrNotFound
	}
	exited := p.obs.Phase == driver.PhaseExited
	started := p.cmd != nil
	d.mu.Unlock()

	if exited {
		return nil
	}
	if !started {
		return fmt.Errorf("process: instance %q is not started yet", ref)
	}
	d.signal(ref, p, syscall.SIGKILL)
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// List returns every known process, exited ones included, sorted by ref.
func (d *Driver) List(_ context.Context) ([]driver.Observed, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]driver.Observed, 0, len(d.procs))
	for _, ref := range slices.Sorted(maps.Keys(d.procs)) {
		out = append(out, d.procs[ref].obs)
	}
	return out, nil
}

// Forget drops an exited instance from List. Running instances cannot be forgotten.
func (d *Driver) Forget(ref string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	p, ok := d.procs[ref]
	if !ok {
		return driver.ErrNotFound
	}
	if p.obs.Phase != driver.PhaseExited {
		return fmt.Errorf("process: instance %q is still %s", ref, p.obs.Phase)
	}
	delete(d.procs, ref)
	return nil
}

// Logs returns the last tail lines (all of them when tail <= 0). With follow it also streams new
// lines until ctx ends or the returned reader is closed.
func (d *Driver) Logs(ctx context.Context, ref string, tail int, follow bool) (io.ReadCloser, error) {
	d.mu.Lock()
	p, ok := d.procs[ref]
	if !ok {
		d.mu.Unlock()
		return nil, driver.ErrNotFound
	}
	snapshot := tailOf(p.lines, tail)
	var sub chan string
	if follow {
		// Subscribe under the same lock as the snapshot, so no line is lost or repeated.
		sub = make(chan string, subscriberBuffer)
		p.subs[sub] = struct{}{}
	}
	d.mu.Unlock()

	if !follow {
		return io.NopCloser(strings.NewReader(joinLines(snapshot))), nil
	}

	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer func() {
			d.mu.Lock()
			delete(p.subs, sub)
			d.mu.Unlock()
			pw.Close()
		}()
		if _, err := io.WriteString(pw, joinLines(snapshot)); err != nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case line := <-sub:
				if _, err := io.WriteString(pw, line+"\n"); err != nil {
					return
				}
			}
		}
	}()
	return &followReader{PipeReader: pr, cancel: cancel}, nil
}

// Command writes one console line to the instance's stdin. Line breaks are rejected so that one call
// is exactly one command.
func (d *Driver) Command(_ context.Context, ref string, line string) error {
	if strings.ContainsAny(line, "\r\n") {
		return errors.New("process: console line must not contain line breaks")
	}
	d.mu.Lock()
	p, ok := d.procs[ref]
	if !ok {
		d.mu.Unlock()
		return driver.ErrNotFound
	}
	phase := p.obs.Phase
	d.mu.Unlock()

	if phase != driver.PhaseRunning {
		return fmt.Errorf("process: instance %q is %s", ref, phase)
	}
	return d.writeStdin(p, line)
}

// escalate implements the stop ladder after the stop command: grace, SIGTERM, termWait, SIGKILL.
func (d *Driver) escalate(ref string, p *proc, grace time.Duration) {
	select {
	case <-p.done:
		return
	case <-d.after(grace):
	}
	d.cfg.Logger.Info("grace period over, sending SIGTERM", "instance", ref)
	d.signal(ref, p, syscall.SIGTERM)

	select {
	case <-p.done:
		return
	case <-d.after(d.termWait):
	}
	d.cfg.Logger.Warn("process ignored SIGTERM, sending SIGKILL", "instance", ref)
	d.signal(ref, p, syscall.SIGKILL)
}

// signal sends sig to the process group unless the process has already been reaped.
func (d *Driver) signal(ref string, p *proc, sig syscall.Signal) {
	d.mu.Lock()
	exited := p.obs.Phase == driver.PhaseExited
	pid := p.cmd.Process.Pid
	d.mu.Unlock()
	if exited {
		return
	}
	if err := syscall.Kill(-pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		d.cfg.Logger.Warn("signal failed", "instance", ref, "signal", sig.String(), "err", err)
	}
}

// waitExit reaps the process and records how it ended.
func (d *Driver) waitExit(p *proc) {
	err := p.cmd.Wait()
	reason := exitReason(p.cmd, err)

	d.mu.Lock()
	d.setPhaseLocked(p, driver.PhaseExited, reason)
	close(p.done)
	d.mu.Unlock()

	d.cfg.Logger.Info("process exited", "instance", p.obs.Ref, "reason", reason)
}

// readOutput merges stdout and stderr into the ring buffer and the console log file.
func (d *Driver) readOutput(p *proc, r *os.File, file *os.File) {
	defer file.Close()
	defer r.Close()

	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			if _, werr := fmt.Fprintln(file, line); werr != nil {
				d.cfg.Logger.Warn("console log write failed", "instance", p.obs.Ref, "err", werr)
			}
			d.appendLine(p, line)
		}
		if err != nil {
			return
		}
	}
}

func (d *Driver) appendLine(p *proc, line string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	p.lines = append(p.lines, line)
	// Trim in batches so the copy cost is amortised.
	if len(p.lines) >= 2*logRingSize {
		p.lines = slices.Clone(p.lines[len(p.lines)-logRingSize:])
	}
	for sub := range p.subs {
		select {
		case sub <- line:
		default:
			d.cfg.Logger.Warn("follower too slow, dropping line", "instance", p.obs.Ref)
		}
	}
}

// setPhaseLocked changes the phase and wakes everyone waiting for a change. Caller holds d.mu.
func (d *Driver) setPhaseLocked(p *proc, phase driver.Phase, reason string) {
	p.obs.Phase = phase
	p.obs.Reason = reason
	close(p.changed)
	p.changed = make(chan struct{})
}

// writeStdin writes one line to the process stdin. The stdin pointer is read under the lock because
// waitExit closes it.
func (d *Driver) writeStdin(p *proc, line string) error {
	d.mu.Lock()
	w := p.stdin
	d.mu.Unlock()
	if w == nil {
		return errors.New("process: stdin not available")
	}
	_, err := io.WriteString(w, line+"\n")
	return err
}

// followReader closes its subscription when closed.
type followReader struct {
	*io.PipeReader
	cancel context.CancelFunc
}

func (r *followReader) Close() error {
	r.cancel()
	return r.PipeReader.Close()
}

func tailOf(lines []string, tail int) []string {
	if len(lines) > logRingSize {
		lines = lines[len(lines)-logRingSize:]
	}
	if tail > 0 && tail < len(lines) {
		lines = lines[len(lines)-tail:]
	}
	return slices.Clone(lines)
}

func joinLines(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

// buildEnv is the parent environment, then the spec env, then the GRUS_* values. Later entries win.
func buildEnv(extra map[string]string, token string, port int) []string {
	env := os.Environ()
	for _, k := range slices.Sorted(maps.Keys(extra)) {
		env = append(env, k+"="+extra[k])
	}
	p := strconv.Itoa(port)
	return append(env, "GRUS_TOKEN="+token, "GRUS_PORT="+p, "SERVER_PORT="+p)
}

// pickPort returns the fixed port of a static pool, or a free loopback port otherwise.
func pickPort(spec driver.StartSpec) (int, error) {
	if port := spec.EffectiveOrV1().Port; spec.Pool.Spec.Mode == model.ModeStatic && port > 0 {
		return port, nil
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find free port: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// checkID rejects ids that would escape BaseDir or are not usable as a directory name.
func checkID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\\x00") {
		return fmt.Errorf("process: invalid instance id %q", id)
	}
	return nil
}

// exitReason describes how the process ended, e.g. "exit code 0" or "signal: killed".
func exitReason(cmd *exec.Cmd, err error) string {
	st := cmd.ProcessState
	if st == nil {
		if err != nil {
			return err.Error()
		}
		return "exited"
	}
	if ws, ok := st.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return "signal: " + ws.Signal().String()
	}
	return fmt.Sprintf("exit code %d", st.ExitCode())
}
