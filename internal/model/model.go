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

// Source says who owns a declaration. Objects owned by kubernetes are read-only through the REST API.
type Source string

const (
	SourceAPI        Source = "api"
	SourceKubernetes Source = "kubernetes"
	SourceFile       Source = "file"
)

// Valid reports whether s is a known source.
func (s Source) Valid() bool {
	return s == SourceAPI || s == SourceKubernetes || s == SourceFile
}

// Placement drivers.
const (
	DriverKubernetes = "kubernetes"
	DriverAgent      = "agent"
	DriverProcess    = "process"
)

// Placement says where the instances of a pool run. Host names the agent for the agent driver.
type Placement struct {
	Driver string `json:"driver,omitempty"`
	Host   string `json:"host,omitempty"`
}

// Sharding splits a pool's world into cells (spec only; behaviour follows later).
type Sharding struct {
	CellSize int      `json:"cellSize,omitempty" doc:"Cell edge length in chunks"`
	Cells    []string `json:"cells,omitempty" doc:"Explicit cell names"`
}

// Private lets owners run their own instance of a pool. A pool with Private set is never autoscaled: its
// instances are created on demand (POST /v1/private-servers) and stopped after IdleTimeout without players.
//
// World persistence is spec only: a later version snapshots the instance directory to a template when the
// instance is stopped and restores it on the next start, so the owner's world survives the idle stop.
type Private struct {
	QuotaPerOwner int      `json:"quotaPerOwner,omitempty" doc:"Instances one owner may have at once (default 1)"`
	IdleTimeout   Duration `json:"idleTimeout,omitempty" doc:"Stop an instance after this long without players (default 10m)"`
}

// PoolSpec is the declarative template for the instances of a pool. Without Environment the inline
// v1 fields (Image, Command, Args, Env, Port) describe how to run; Driver is the v1 spelling of Placement.Driver.
type PoolSpec struct {
	Role              Role              `json:"role"`
	Mode              Mode              `json:"mode"`
	Driver            string            `json:"driver,omitempty"`
	Placement         Placement         `json:"placement,omitempty"`
	Environment       string            `json:"environment,omitempty"`
	Groups            []string          `json:"groups,omitempty"`
	Sharding          *Sharding         `json:"sharding,omitempty"`
	Private           *Private          `json:"private,omitempty"`
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
	DefaultQuotaPerOwner     = 1
	DefaultPrivateIdle       = 10 * time.Minute
)

// Instance label keys set by Grus. SelfStatus keeps labels with this prefix when an instance replaces its labels.
const (
	LabelPrefix = "grus.olf/"
	LabelOwner  = LabelPrefix + "owner"
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
	if s.Private != nil {
		p := *s.Private
		if p.QuotaPerOwner == 0 {
			p.QuotaPerOwner = DefaultQuotaPerOwner
		}
		if p.IdleTimeout == 0 {
			p.IdleTimeout = Duration(DefaultPrivateIdle)
		}
		s.Private = &p
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
	if s.Driver != "" && s.Placement.Driver != "" && s.Driver != s.Placement.Driver {
		add("driver", "conflicts with placement.driver; set only placement.driver")
	}
	switch s.EffectiveDriver() {
	case DriverKubernetes:
		if s.Environment == "" && s.Image == "" {
			add("image", "is required for the kubernetes driver without an environment")
		}
	case DriverProcess, DriverAgent:
		if s.Environment == "" && len(s.Command) == 0 {
			add("command", "is required for the "+s.EffectiveDriver()+" driver without an environment")
		}
		if s.EffectiveDriver() == DriverAgent && s.EffectivePlacement().Host == "" {
			add("placement.host", "is required for the agent driver")
		}
	default:
		add("placement.driver", "must be kubernetes, agent or process")
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
	if p := s.Private; p != nil {
		if p.QuotaPerOwner < 0 {
			add("private.quotaPerOwner", "must not be negative")
		}
		if p.IdleTimeout < 0 {
			add("private.idleTimeout", "must not be negative")
		}
		if s.Scaling.Min != 0 {
			add("scaling.min", "must be 0 for a private pool; instances are created on demand")
		}
		if s.Scaling.Max < 1 {
			add("scaling.max", "must be at least 1 for a private pool")
		}
		if s.Scaling.Buffer != 0 {
			add("scaling.buffer", "must be 0 for a private pool")
		}
		if s.Mode != ModeDynamic {
			add("mode", "must be dynamic for a private pool")
		}
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

// EffectivePlacement returns the placement with the v1 driver filled in when placement.driver is empty.
func (s PoolSpec) EffectivePlacement() Placement {
	p := s.Placement
	if p.Driver == "" {
		p.Driver = s.Driver
	}
	return p
}

// EffectiveDriver is placement.driver, or the v1 driver when placement is empty.
func (s PoolSpec) EffectiveDriver() string { return s.EffectivePlacement().Driver }

// Pool is a stored pool with its spec generation; Generation increases on every spec change.
type Pool struct {
	Name        string    `json:"name"`
	Spec        PoolSpec  `json:"spec"`
	Source      Source    `json:"source,omitempty"`
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
	EventNetworkPlayers  = "network.players"
	EventPoolCondition   = "pool.condition"
	EventPrivateIdleStop = "private.idle-stop"
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

// NetworkStatus is the player count summed over all Ready and Allocated proxy instances.
type NetworkStatus struct {
	Players    int `json:"players"`
	MaxPlayers int `json:"maxPlayers"`
	Proxies    int `json:"proxies"`
}

// PoolConditionData is the payload of pool.condition: a pool cannot start instances for the given reason.
type PoolConditionData struct {
	Pool    string `json:"pool"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// PrivateIdleStopData is the payload of private.idle-stop: a private instance was stopped after it stayed empty.
type PrivateIdleStopData struct {
	Pool     string `json:"pool"`
	Instance string `json:"instance"`
	Owner    string `json:"owner"`
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
