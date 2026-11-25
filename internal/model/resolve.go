package model

import (
	"fmt"
	"strconv"
)

// HeapPercent is the share of a pool's memory given to the JVM heap (-Xmx). The rest is left for metaspace,
// thread stacks, direct buffers and the JVM itself, so the process stays below its memory limit.
const HeapPercent = 85

// EffectiveSpec is what a driver actually runs: the pool merged with its environment and groups.
type EffectiveSpec struct {
	Kind        string            `json:"kind,omitempty"`
	Driver      string            `json:"driver"`
	Host        string            `json:"host,omitempty"`
	Image       string            `json:"image,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Port        int               `json:"port"`
	Readiness   string            `json:"readiness"`
	StopCommand string            `json:"stopCommand,omitempty"`
	GracePeriod Duration          `json:"gracePeriod"`
	Templates   []TemplateRef     `json:"templates,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// ResolvePool merges pool, environment and groups into the effective run spec.
//
// Merge rules: environment defaults, then the groups in order, then the pool; later wins for env and
// labels; templates are concatenated group1..groupN, then the pool's. Pool image, command, args, port and
// lifecycle stop command or grace period override the environment's when set. env may be nil only when the
// pool names no environment (v1 inline fields). groups must be the specs named by pool.Groups, in order.
//
// For kind jvm on the process and agent drivers without an explicit command the command line is
// `java <flags> -Xmx<memoryMiB*85/100>m -jar <jar>`; pool and environment args are appended after it.
func ResolvePool(pool PoolSpec, env *EnvironmentSpec, groups []GroupSpec) (EffectiveSpec, []FieldError) {
	var errs []FieldError
	add := func(f, m string) { errs = append(errs, FieldError{Field: f, Message: m}) }

	pl := pool.EffectivePlacement()
	eff := EffectiveSpec{Driver: pl.Driver, Host: pl.Host}

	var e EnvironmentSpec
	switch {
	case pool.Environment != "" && env == nil:
		add("environment", fmt.Sprintf("environment %q does not exist", pool.Environment))
	case env != nil:
		e = env.WithDefaults()
	}
	if len(groups) != len(pool.Groups) {
		add("groups", "resolved groups do not match the pool's group list")
	}
	if len(errs) > 0 {
		return EffectiveSpec{}, errs
	}

	eff.Kind = e.Kind
	eff.Image = firstNonEmpty(pool.Image, e.Image)
	eff.Command = cloneStrings(pickStrings(pool.Command, e.Command))
	eff.Args = cloneStrings(append(append([]string{}, e.Args...), pool.Args...))
	if len(pool.Command) > 0 {
		eff.Args = cloneStrings(pool.Args) // a pool command replaces the environment's and brings its own args
	}
	eff.Env = mergeMaps(e.Env)
	for _, g := range groups {
		eff.Env = mergeMaps(eff.Env, g.Env)
		eff.Labels = mergeMaps(eff.Labels, g.Labels)
		eff.Templates = append(eff.Templates, g.Templates...)
	}
	eff.Env = mergeMaps(eff.Env, pool.Env)
	eff.Labels = mergeMaps(eff.Labels, pool.Labels)
	eff.Templates = append(eff.Templates, pool.Templates...)

	eff.Port = firstPositive(pool.Port, e.Port, DefaultPort)
	eff.Readiness = firstNonEmpty(e.Readiness, ReadinessSelf)
	eff.StopCommand = firstNonEmpty(pool.Lifecycle.StopCommand, e.StopCommand)
	eff.GracePeriod = Duration(firstPositiveDuration(pool.Lifecycle.GracePeriod, e.GracePeriod, Duration(DefaultGracePeriod)))

	if e.Kind == KindJVM && e.JVM != nil && len(eff.Command) == 0 && (pl.Driver == DriverProcess || pl.Driver == DriverAgent) {
		if e.JVM.Jar == "" {
			add("environment.jvm.jar", "is required to start a jvm environment with the "+pl.Driver+" driver")
		} else {
			eff.Command = javaCommand(*e.JVM, pool.Resources.MemoryMiB)
		}
	}
	switch pl.Driver {
	case DriverKubernetes:
		if eff.Image == "" {
			add("image", "no image: set it on the pool or its environment")
		}
	case DriverProcess, DriverAgent:
		if len(eff.Command) == 0 && len(errs) == 0 {
			add("command", "no command: set it on the pool or its environment")
		}
	}
	if len(errs) > 0 {
		return EffectiveSpec{}, errs
	}
	return eff, nil
}

func javaCommand(j JVM, memoryMiB int) []string {
	cmd := append([]string{"java"}, j.Flags...)
	if memoryMiB > 0 {
		cmd = append(cmd, "-Xmx"+strconv.Itoa(memoryMiB*HeapPercent/100)+"m")
	}
	return append(cmd, "-jar", j.Jar)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstPositive(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func firstPositiveDuration(vals ...Duration) Duration {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func pickStrings(pool, env []string) []string {
	if len(pool) > 0 {
		return pool
	}
	return env
}

func cloneStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return append([]string(nil), s...)
}

// mergeMaps returns a new map with every later map overriding the earlier ones; nil when all are empty.
func mergeMaps(maps ...map[string]string) map[string]string {
	var out map[string]string
	for _, m := range maps {
		for k, v := range m {
			if out == nil {
				out = make(map[string]string)
			}
			out[k] = v
		}
	}
	return out
}
