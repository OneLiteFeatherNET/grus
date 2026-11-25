package model

import (
	"fmt"
	"time"
)

// Environment kinds.
const (
	KindJVM       = "jvm"
	KindNative    = "native"
	KindContainer = "container"
)

// Readiness modes: self means the instance calls POST /v1/self/ready, tcp means Grus marks it Ready once
// the port accepts connections.
const (
	ReadinessSelf = "self"
	ReadinessTCP  = "tcp"
)

// JVM describes how a jvm environment is started by the process and agent drivers. The heap size is
// derived from the pool's memory (see ResolvePool).
type JVM struct {
	Jar   string   `json:"jar,omitempty" doc:"Jar file inside the instance directory"`
	Flags []string `json:"flags,omitempty" doc:"JVM flags placed before -Xmx and -jar"`
}

// EnvironmentSpec says HOW a service runs, independent of what it serves.
type EnvironmentSpec struct {
	Kind        string            `json:"kind"`
	Image       string            `json:"image,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	JVM         *JVM              `json:"jvm,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Port        int               `json:"port,omitempty"`
	Readiness   string            `json:"readiness,omitempty"`
	StopCommand string            `json:"stopCommand,omitempty"`
	GracePeriod Duration          `json:"gracePeriod,omitempty"`
}

// WithDefaults returns a copy with Port and Readiness defaulted.
func (s EnvironmentSpec) WithDefaults() EnvironmentSpec {
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	if s.Readiness == "" {
		s.Readiness = ReadinessSelf
	}
	return s
}

// Validate checks a spec and returns every problem found.
func (s EnvironmentSpec) Validate() []FieldError {
	var errs []FieldError
	add := func(f, m string) { errs = append(errs, FieldError{Field: f, Message: m}) }
	switch s.Kind {
	case KindJVM, KindNative, KindContainer:
	default:
		add("kind", "must be jvm, native or container")
	}
	if s.JVM != nil && s.Kind != KindJVM {
		add("jvm", "is only allowed for kind jvm")
	}
	if s.Kind == KindContainer && s.Image == "" {
		add("image", "is required for kind container")
	}
	if s.Kind == KindNative && len(s.Command) == 0 && s.Image == "" {
		add("command", "is required for kind native unless an image is set")
	}
	if s.Kind == KindJVM && s.Image == "" && len(s.Command) == 0 && (s.JVM == nil || s.JVM.Jar == "") {
		add("jvm.jar", "is required for kind jvm unless an image or command is set")
	}
	if s.Port < 0 || s.Port > 65535 {
		add("port", "must be in 0..65535")
	}
	switch s.Readiness {
	case "", ReadinessSelf, ReadinessTCP:
	default:
		add("readiness", "must be self or tcp")
	}
	if s.GracePeriod < 0 {
		add("gracePeriod", "must not be negative")
	}
	return errs
}

// GroupSpec bundles templates, env and labels shared by many pools.
type GroupSpec struct {
	Templates []TemplateRef     `json:"templates,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// Validate checks a spec and returns every problem found.
func (s GroupSpec) Validate() []FieldError {
	var errs []FieldError
	for i, t := range s.Templates {
		if t.Name == "" {
			errs = append(errs, FieldError{Field: fmt.Sprintf("templates[%d].name", i), Message: "is required"})
		}
		if (t.Tag == "") == (t.Digest == "") {
			errs = append(errs, FieldError{Field: fmt.Sprintf("templates[%d]", i), Message: "needs exactly one of tag or digest"})
		}
	}
	return errs
}

// Environment is a stored environment; Generation increases on every spec change.
type Environment struct {
	Name       string          `json:"name"`
	Spec       EnvironmentSpec `json:"spec"`
	Source     Source          `json:"source"`
	Generation int64           `json:"generation"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// Group is a stored group; Generation increases on every spec change.
type Group struct {
	Name       string    `json:"name"`
	Spec       GroupSpec `json:"spec"`
	Source     Source    `json:"source"`
	Generation int64     `json:"generation"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
