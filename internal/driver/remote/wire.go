// Package remote implements driver.Driver for a host that runs instances through a grus-agent. The agent
// pulls its desired state from the controller over HTTP and reports what it observes; this package keeps
// both sides of that exchange in memory. Nothing is persisted: after a controller restart the agent
// re-reports its running instances and the controller adopts them.
package remote

import (
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// Capacity is what a host offers.
type Capacity struct {
	MemoryMiB int `json:"memoryMiB,omitempty" doc:"Memory available for instances"`
}

// RegisterRequest is the body of PUT /v1/agents/{host}; it doubles as heartbeat.
type RegisterRequest struct {
	Version   string            `json:"version,omitempty" doc:"Agent version"`
	Labels    map[string]string `json:"labels,omitempty" doc:"Free host labels"`
	Capacity  Capacity          `json:"capacity,omitempty"`
	Address   string            `json:"address,omitempty" doc:"IP address proxies use to reach instances on this host"`
	PortRange string            `json:"portRange,omitempty" doc:"Instance port range, e.g. 30000-30100"`
}

// TemplateVersion is a template version the agent fetches before it starts an instance.
type TemplateVersion struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// StopRequest asks the agent to stop an instance gracefully.
type StopRequest struct {
	Requested    bool   `json:"requested"`
	Command      string `json:"command,omitempty" doc:"Console line that triggers a clean shutdown"`
	GraceSeconds int    `json:"graceSeconds,omitempty" doc:"Time before the agent sends SIGTERM"`
}

// DesiredInstance is one instance the agent must run. Entries without an effective command are known
// instances the controller adopted from the agent: they are never started, only stopped or killed.
type DesiredInstance struct {
	ID         string              `json:"id"`
	Pool       string              `json:"pool"`
	Role       model.Role          `json:"role"`
	Mode       model.Mode          `json:"mode"`
	Generation int64               `json:"generation"`
	Token      string              `json:"token,omitempty" doc:"Plain per-instance token for GRUS_TOKEN"`
	TokenHash  string              `json:"tokenHash,omitempty"`
	Effective  model.EffectiveSpec `json:"effective"`
	Env        map[string]string   `json:"env,omitempty" doc:"Environment of the instance as the controller built it; the agent overrides GRUS_URL"`
	Templates  []TemplateVersion   `json:"templates,omitempty"`
	MemoryMiB  int                 `json:"memoryMiB,omitempty"`
	Stop       StopRequest         `json:"stop"`
	Kill       bool                `json:"kill,omitempty" doc:"Kill the instance and forget it"`
}

// Command is one console line for an instance. Seq grows per host; the agent acknowledges the highest
// sequence number it has applied in its observed report.
type Command struct {
	Seq      int64  `json:"seq"`
	Instance string `json:"instance"`
	Line     string `json:"line"`
}

// Desired is the response of the long poll.
type Desired struct {
	Revision  int64             `json:"revision" doc:"Changes whenever the desired state changes"`
	Instances []DesiredInstance `json:"instances"`
	Commands  []Command         `json:"commands"`
}

// ObservedInstance is what the agent sees of one instance.
type ObservedInstance struct {
	ID         string       `json:"id"`
	Pool       string       `json:"pool,omitempty"`
	Role       model.Role   `json:"role,omitempty"`
	Mode       model.Mode   `json:"mode,omitempty"`
	Generation int64        `json:"generation,omitempty"`
	TokenHash  string       `json:"tokenHash,omitempty"`
	Address    string       `json:"address,omitempty"`
	Phase      driver.Phase `json:"phase"`
	Reason     string       `json:"reason,omitempty"`
	StartedAt  time.Time    `json:"startedAt,omitempty"`
}

// ObservedReport is the body of PUT /v1/agents/{host}/observed.
type ObservedReport struct {
	Revision   int64               `json:"revision" doc:"Desired revision the agent has applied before this report"`
	CommandSeq int64               `json:"commandSeq,omitempty" doc:"Highest console command sequence number the agent has applied"`
	Instances  []ObservedInstance  `json:"instances"`
	Logs       map[string][]string `json:"logs,omitempty" doc:"Last console lines per instance id"`
}

// HostInfo describes one registered host.
type HostInfo struct {
	Name      string            `json:"name"`
	Version   string            `json:"version,omitempty"`
	Address   string            `json:"address,omitempty"`
	PortRange string            `json:"portRange,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Capacity  Capacity          `json:"capacity,omitempty"`
	LastSeen  *time.Time        `json:"lastSeen,omitempty" doc:"Last contact of the agent; absent when it never connected"`
	Online    bool              `json:"online"`
	Instances int               `json:"instances" doc:"Instances the agent reported"`
}
