// Package driver defines how Grus runs instances. Implementations: kubernetes (bare pods),
// process (local OS processes, for development) and fake (tests). Drivers never decide anything;
// the reconciler tells them what to do and reads back what they observe.
package driver

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// ErrNotFound is returned when the referenced instance does not exist in the driver.
var ErrNotFound = errors.New("driver: instance not found")

// Labels and annotations every driver attaches, so the controller can adopt instances after a restart.
const (
	LabelManagedBy        = "app.kubernetes.io/managed-by" // value "grus"
	LabelPool             = "grus.olf/pool"
	LabelInstance         = "grus.olf/instance"
	LabelRole             = "grus.olf/role"
	AnnotationGeneration  = "grus.olf/pool-generation"
	AnnotationTokenSHA256 = "grus.olf/token-sha256"
	AnnotationMode        = "grus.olf/mode"
	ManagedByValue        = "grus"
)

// StartSpec is everything a driver needs to start one instance.
type StartSpec struct {
	Instance model.Instance
	Pool     model.Pool
	// Effective is the pool merged with its environment and groups. Zero for specs built by hand (tests);
	// drivers use EffectiveOrV1 to read it.
	Effective model.EffectiveSpec
	// Token is the plain per-instance token, injected as GRUS_TOKEN. Never logged.
	Token string
	// Env are the GRUS_* variables (GRUS_URL, GRUS_INSTANCE, GRUS_POOL, GRUS_ROLE) plus spec env.
	Env map[string]string
	// Templates are resolved to digests, in overlay order; the driver fetches them before start.
	Templates []ResolvedTemplate
}

type ResolvedTemplate struct {
	Name   string
	Digest string
}

// Observed is the driver's view of one instance, used for status updates and adoption.
type Observed struct {
	Ref            string
	InstanceID     string
	Pool           string
	Role           model.Role
	Mode           model.Mode
	PoolGeneration int64
	TokenHash      string
	Address        string // "ip:port", empty until known
	Phase          Phase
	Reason         string
	StartedAt      time.Time
}

// Phase is the coarse runtime phase as the driver sees it.
type Phase string

const (
	PhasePending   Phase = "Pending"   // created, not running yet (pulling, init containers)
	PhaseRunning   Phase = "Running"   // main process runs
	PhaseExited    Phase = "Exited"    // process ended (any exit code)
	PhaseFailed    Phase = "Failed"    // could not start (image pull, template fetch, crash before run)
	PhaseNotExists Phase = "NotExists" // gone
)

// Driver runs instances. All methods must be safe for concurrent use.
type Driver interface {
	// Name is "kubernetes" or "process".
	Name() string
	// Host is the host id this driver represents, e.g. "k8s-grus-dev" or "local".
	Host() string
	// Start creates the instance and returns its driver reference. It must not block until Ready.
	Start(ctx context.Context, spec StartSpec) (ref string, err error)
	// Stop asks the instance to shut down: stop command if supported, then SIGTERM, SIGKILL after grace.
	// It returns once the request is issued; the reconciler observes the exit via List.
	Stop(ctx context.Context, ref string, stopCommand string, grace time.Duration) error
	// Kill removes the instance immediately.
	Kill(ctx context.Context, ref string) error
	// List returns all instances managed by Grus on this host (for status and adoption).
	List(ctx context.Context) ([]Observed, error)
	// Logs returns the last tail lines; with follow it keeps streaming until ctx ends.
	Logs(ctx context.Context, ref string, tail int, follow bool) (io.ReadCloser, error)
	// Command writes one console line to the instance's stdin.
	Command(ctx context.Context, ref string, line string) error
}

// EffectiveOrV1 returns Effective, or the v1 inline fields of the pool when no effective spec was resolved.
func (s StartSpec) EffectiveOrV1() model.EffectiveSpec {
	if s.Effective.Driver != "" {
		return s.Effective
	}
	p := s.Pool.Spec
	return model.EffectiveSpec{
		Driver:      p.EffectiveDriver(),
		Image:       p.Image,
		Command:     p.Command,
		Args:        p.Args,
		Env:         p.Env,
		Port:        p.Port,
		Readiness:   model.ReadinessSelf,
		StopCommand: p.Lifecycle.StopCommand,
		GracePeriod: p.WithDefaults().Lifecycle.GracePeriod,
		Templates:   p.Templates,
		Labels:      p.Labels,
	}
}
