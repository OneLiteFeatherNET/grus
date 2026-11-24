// Package model holds the domain types shared by every Grus package: pools, instances, states and
// events. It has no dependencies besides the standard library, so every other package may import it.
package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// Role says what an instance is for the network. Proxies register game instances.
type Role string

const (
	RoleProxy Role = "proxy"
	RoleGame  Role = "game"
	RoleOther Role = "other"
)

// Mode decides whether instances of a pool are interchangeable (dynamic) or keep a fixed identity (static).
type Mode string

const (
	ModeDynamic Mode = "dynamic"
	ModeStatic  Mode = "static"
)

// State is the lifecycle state of an instance. Transitions happen only in package state.
type State string

const (
	StateRequested State = "Requested"
	StateScheduled State = "Scheduled"
	StatePreparing State = "Preparing"
	StateStarting  State = "Starting"
	StateReady     State = "Ready"
	StateAllocated State = "Allocated"
	StateUnhealthy State = "Unhealthy"
	StateDraining  State = "Draining"
	StateStopping  State = "Stopping"
	StateStopped   State = "Stopped"
	StateFailed    State = "Failed"
	StateLost      State = "Lost"
)

// Terminal reports whether no process belongs to the instance any more.
func (s State) Terminal() bool {
	return s == StateStopped || s == StateFailed || s == StateLost
}

// Duration is a time.Duration that reads and writes Go duration strings such as "2m" or "30s" in JSON.
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"30s\": %w", err)
	}
	if s == "" {
		*d = 0
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// SecretRef points at a key of a Kubernetes Secret in the controller namespace; it is injected as env var Name.
type SecretRef struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
	Key    string `json:"key"`
}

// TemplateRef selects a template version by tag or digest. Order in PoolSpec.Templates is overlay order.
type TemplateRef struct {
	Name   string `json:"name"`
	Tag    string `json:"tag,omitempty"`
	Digest string `json:"digest,omitempty"`
}

type Resources struct {
	MemoryMiB int    `json:"memoryMiB"`
	CPU       string `json:"cpu,omitempty"`
}

type Scaling struct {
	Min               int      `json:"min"`
	Max               int      `json:"max"`
	Buffer            int      `json:"buffer,omitempty"`
	Capacity          int      `json:"capacity,omitempty"`
	TargetUtilization float64  `json:"targetUtilization,omitempty"`
	ScaleDownDelay    Duration `json:"scaleDownDelay,omitempty"`
	DrainTimeout      Duration `json:"drainTimeout,omitempty"`
	MaxSurge          int      `json:"maxSurge,omitempty"`
}

type Lifecycle struct {
	StopCommand      string   `json:"stopCommand,omitempty"`
	GracePeriod      Duration `json:"gracePeriod,omitempty"`
	HeartbeatTimeout Duration `json:"heartbeatTimeout,omitempty"`
	StartTimeout     Duration `json:"startTimeout,omitempty"`
}

// PoolSpec is the declarative template for the instances of a pool.
type PoolSpec struct {
	Role              Role              `json:"role"`
	Mode              Mode              `json:"mode"`
	Driver            string            `json:"driver"`
	Image             string            `json:"image,omitempty"`
	Command           []string          `json:"command,omitempty"`
	Args              []string          `json:"args,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	SecretEnv         []SecretRef       `json:"secretEnv,omitempty"`
	Resources         Resources         `json:"resources"`
	Port              int               `json:"port,omitempty"`
	Templates         []TemplateRef     `json:"templates,omitempty"`
	Scaling           Scaling           `json:"scaling"`
	Lifecycle         Lifecycle         `json:"lifecycle,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	PriorityClassName string            `json:"priorityClassName,omitempty"`
}

// Defaults used when a spec leaves a field empty.
const (
	DefaultPort              = 25565
	DefaultMaxSurge          = 2
	DefaultTargetUtilization = 0.8
	DefaultScaleDownDelay    = 5 * time.Minute
	DefaultDrainTimeout      = 60 * time.Second
	DefaultGracePeriod       = 30 * time.Second
	DefaultHeartbeatTimeout  = 15 * time.Second
	DefaultStartTimeout      = 5 * time.Minute
)

// WithDefaults returns a copy with every empty optional field set to its default.
func (s PoolSpec) WithDefaults() PoolSpec {
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	if s.Scaling.MaxSurge == 0 {
		s.Scaling.MaxSurge = DefaultMaxSurge
	}
	if s.Scaling.TargetUtilization == 0 {
		s.Scaling.TargetUtilization = DefaultTargetUtilization
	}
	if s.Scaling.ScaleDownDelay == 0 {
		s.Scaling.ScaleDownDelay = Duration(DefaultScaleDownDelay)
	}
	if s.Scaling.DrainTimeout == 0 {
		s.Scaling.DrainTimeout = Duration(DefaultDrainTimeout)
	}
	if s.Lifecycle.GracePeriod == 0 {
		s.Lifecycle.GracePeriod = Duration(DefaultGracePeriod)
	}
	if s.Lifecycle.HeartbeatTimeout == 0 {
		s.Lifecycle.HeartbeatTimeout = Duration(DefaultHeartbeatTimeout)
	}
	if s.Lifecycle.StartTimeout == 0 {
		s.Lifecycle.StartTimeout = Duration(DefaultStartTimeout)
	}
	return s
}

// FieldError is one validation problem of a pool spec, reported as RFC 9457 error detail.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Validate checks a spec (after WithDefaults) and returns every problem found.
func (s PoolSpec) Validate() []FieldError {
	var errs []FieldError
	add := func(f, m string) { errs = append(errs, FieldError{Field: f, Message: m}) }
	switch s.Role {
	case RoleProxy, RoleGame, RoleOther:
	default:
		add("role", "must be proxy, game or other")
	}
	switch s.Mode {
	case ModeDynamic, ModeStatic:
	default:
		add("mode", "must be dynamic or static")
	}
	switch s.Driver {
	case "kubernetes":
		if s.Image == "" {
			add("image", "is required for the kubernetes driver")
		}
	case "process":
		if len(s.Command) == 0 {
			add("command", "is required for the process driver")
		}
	default:
		add("driver", "must be kubernetes or process")
	}
	if s.Resources.MemoryMiB <= 0 {
		add("resources.memoryMiB", "must be greater than 0")
	}
	if s.Scaling.Min < 0 {
		add("scaling.min", "must not be negative")
	}
	if s.Scaling.Max < s.Scaling.Min {
		add("scaling.max", "must be at least scaling.min")
	}
	if s.Scaling.Buffer < 0 {
		add("scaling.buffer", "must not be negative")
	}
	if s.Mode == ModeStatic && s.Scaling.Buffer != 0 {
		add("scaling.buffer", "must be 0 for static pools")
	}
	if s.Scaling.TargetUtilization <= 0 || s.Scaling.TargetUtilization > 1 {
		add("scaling.targetUtilization", "must be in (0, 1]")
	}
	for i, t := range s.Templates {
		if t.Name == "" {
			add(fmt.Sprintf("templates[%d].name", i), "is required")
		}
		if (t.Tag == "") == (t.Digest == "") {
			add(fmt.Sprintf("templates[%d]", i), "needs exactly one of tag or digest")
		}
	}
	return errs
}

// Pool is a stored pool with its spec generation; Generation increases on every spec change.
type Pool struct {
	Name        string    `json:"name"`
	Spec        PoolSpec  `json:"spec"`
	Generation  int64     `json:"generation"`
	Maintenance bool      `json:"maintenance"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Instance is one running or to-be-running unit of a pool.
type Instance struct {
	ID             string            `json:"id"`
	Pool           string            `json:"pool"`
	Role           Role              `json:"role"`
	PoolGeneration int64             `json:"poolGeneration"`
	Host           string            `json:"host"`
	DriverRef      string            `json:"driverRef,omitempty"`
	Address        string            `json:"address,omitempty"`
	State          State             `json:"state"`
	StateReason    string            `json:"stateReason,omitempty"`
	StateSince     time.Time         `json:"stateSince"`
	Players        int               `json:"players"`
	MaxPlayers     int               `json:"maxPlayers"`
	Labels         map[string]string `json:"labels,omitempty"`
	AllocatedAt    *time.Time        `json:"allocatedAt,omitempty"`
	LastHeartbeat  *time.Time        `json:"lastHeartbeat,omitempty"`
	TokenHash      string            `json:"-"`
	CreatedAt      time.Time         `json:"createdAt"`
}

// Event types sent on the SSE stream.
const (
	EventInstanceCreated = "instance.created"
	EventInstanceState   = "instance.state"
	EventInstanceUpdated = "instance.updated"
	EventInstanceRemoved = "instance.removed"
	EventPoolUpdated     = "pool.updated"
	EventPoolScaled      = "pool.scaled"
	EventTemplateTagged  = "template.tagged"
	EventResync          = "resync"
)

// Event is one entry of the event stream. ID is assigned by the hub ("<epoch>-<seq>").
type Event struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	At   time.Time       `json:"at"`
}

// InstanceStateData is the payload of instance.state.
type InstanceStateData struct {
	Instance string `json:"instance"`
	Pool     string `json:"pool"`
	Role     Role   `json:"role"`
	From     State  `json:"from"`
	To       State  `json:"to"`
	Address  string `json:"address,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// PoolScaledData is the payload of pool.scaled (E5.5).
type PoolScaledData struct {
	Pool      string         `json:"pool"`
	Direction string         `json:"direction"` // "up" or "down"
	Reason    string         `json:"reason"`    // "min", "buffer", "load"
	Desired   int            `json:"desired"`
	Current   int            `json:"current"`
	Inputs    map[string]int `json:"inputs"`
}

// Publisher sends events to stream subscribers. Implemented by events.Hub.
type Publisher interface {
	Publish(eventType string, data any)
}

// Clock is injected everywhere time matters, so tests never sleep (F.I.R.S.T.).
type Clock interface {
	Now() time.Time
}

// SystemClock is the production clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
