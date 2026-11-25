package model

import (
	"reflect"
	"testing"
	"time"
)

func tpl(name string) TemplateRef { return TemplateRef{Name: name, Tag: "latest"} }

func TestResolvePool(t *testing.T) {
	jvmEnv := &EnvironmentSpec{
		Kind: KindJVM, Image: "ghcr.io/x/paper:1", Port: 25570,
		JVM:  &JVM{Jar: "paper.jar", Flags: []string{"-XX:+UseG1GC"}},
		Env:  map[string]string{"A": "env", "B": "env"},
		Args: []string{"--nogui"}, StopCommand: "stop", GracePeriod: Duration(45 * time.Second),
	}
	tests := []struct {
		name   string
		pool   PoolSpec
		env    *EnvironmentSpec
		groups []GroupSpec
		want   EffectiveSpec
		errs   []string
	}{
		{
			name: "v1 inline spec without environment",
			pool: PoolSpec{Driver: "kubernetes", Image: "img", Command: []string{"run"}, Args: []string{"-x"},
				Env: map[string]string{"K": "v"}, Port: 1234},
			want: EffectiveSpec{Driver: "kubernetes", Image: "img", Command: []string{"run"}, Args: []string{"-x"},
				Env: map[string]string{"K": "v"}, Port: 1234, Readiness: ReadinessSelf,
				GracePeriod: Duration(DefaultGracePeriod)},
		},
		{
			name: "jvm on process builds the java command with 85 percent heap",
			pool: PoolSpec{Placement: Placement{Driver: "process"}, Environment: "paper", Resources: Resources{MemoryMiB: 2048},
				Args: []string{"--port", "1"}},
			env: jvmEnv,
			want: EffectiveSpec{Kind: KindJVM, Driver: "process", Image: "ghcr.io/x/paper:1",
				Command: []string{"java", "-XX:+UseG1GC", "-Xmx1740m", "-jar", "paper.jar"},
				Args:    []string{"--nogui", "--port", "1"}, Env: map[string]string{"A": "env", "B": "env"},
				Port: 25570, Readiness: ReadinessSelf, StopCommand: "stop", GracePeriod: Duration(45 * time.Second)},
		},
		{
			name: "jvm on agent keeps the host",
			pool: PoolSpec{Placement: Placement{Driver: "agent", Host: "mc01"}, Environment: "paper", Resources: Resources{MemoryMiB: 1000}},
			env:  jvmEnv,
			want: EffectiveSpec{Kind: KindJVM, Driver: "agent", Host: "mc01", Image: "ghcr.io/x/paper:1",
				Command: []string{"java", "-XX:+UseG1GC", "-Xmx850m", "-jar", "paper.jar"}, Args: []string{"--nogui"},
				Env: map[string]string{"A": "env", "B": "env"}, Port: 25570, Readiness: ReadinessSelf,
				StopCommand: "stop", GracePeriod: Duration(45 * time.Second)},
		},
		{
			name: "jvm on kubernetes runs the image and builds no command",
			pool: PoolSpec{Placement: Placement{Driver: "kubernetes"}, Environment: "paper", Resources: Resources{MemoryMiB: 1000}},
			env:  jvmEnv,
			want: EffectiveSpec{Kind: KindJVM, Driver: "kubernetes", Image: "ghcr.io/x/paper:1", Args: []string{"--nogui"},
				Env: map[string]string{"A": "env", "B": "env"}, Port: 25570, Readiness: ReadinessSelf,
				StopCommand: "stop", GracePeriod: Duration(45 * time.Second)},
		},
		{
			name: "env and labels: environment, then groups in order, then pool",
			pool: PoolSpec{Driver: "kubernetes", Environment: "paper", Groups: []string{"g1", "g2"},
				Env: map[string]string{"B": "pool"}, Labels: map[string]string{"l": "pool"}},
			env: jvmEnv,
			groups: []GroupSpec{
				{Env: map[string]string{"A": "g1", "C": "g1"}, Labels: map[string]string{"l": "g1", "m": "g1"}},
				{Env: map[string]string{"C": "g2"}, Labels: map[string]string{"m": "g2"}},
			},
			want: EffectiveSpec{Kind: KindJVM, Driver: "kubernetes", Image: "ghcr.io/x/paper:1", Args: []string{"--nogui"},
				Env:  map[string]string{"A": "g1", "B": "pool", "C": "g2"},
				Port: 25570, Readiness: ReadinessSelf, StopCommand: "stop", GracePeriod: Duration(45 * time.Second),
				Labels: map[string]string{"l": "pool", "m": "g2"}},
		},
		{
			name: "templates: groups in order, then the pool",
			pool: PoolSpec{Driver: "kubernetes", Image: "i", Groups: []string{"g1", "g2"}, Templates: []TemplateRef{tpl("p")}},
			groups: []GroupSpec{
				{Templates: []TemplateRef{tpl("a"), tpl("b")}},
				{Templates: []TemplateRef{tpl("c")}},
			},
			want: EffectiveSpec{Driver: "kubernetes", Image: "i", Port: DefaultPort, Readiness: ReadinessSelf,
				GracePeriod: Duration(DefaultGracePeriod),
				Templates:   []TemplateRef{tpl("a"), tpl("b"), tpl("c"), tpl("p")}},
		},
		{
			name: "pool fields override the environment",
			pool: PoolSpec{Driver: "process", Environment: "paper", Port: 1, Image: "own", Command: []string{"bin"},
				Args:      []string{"-p"},
				Lifecycle: Lifecycle{StopCommand: "end", GracePeriod: Duration(5 * time.Second)}},
			env: jvmEnv,
			want: EffectiveSpec{Kind: KindJVM, Driver: "process", Image: "own", Command: []string{"bin"}, Args: []string{"-p"},
				Env: map[string]string{"A": "env", "B": "env"}, Port: 1, Readiness: ReadinessSelf,
				StopCommand: "end", GracePeriod: Duration(5 * time.Second)},
		},
		{
			name: "tcp readiness comes from the environment",
			pool: PoolSpec{Driver: "kubernetes", Environment: "n"},
			env:  &EnvironmentSpec{Kind: KindNative, Image: "i", Readiness: ReadinessTCP},
			want: EffectiveSpec{Kind: KindNative, Driver: "kubernetes", Image: "i", Port: DefaultPort,
				Readiness: ReadinessTCP, GracePeriod: Duration(DefaultGracePeriod)},
		},
		{
			name: "unknown environment is an error",
			pool: PoolSpec{Driver: "kubernetes", Environment: "missing"},
			errs: []string{"environment"},
		},
		{
			name: "jvm without jar on process is an error",
			pool: PoolSpec{Driver: "process", Environment: "e"},
			env:  &EnvironmentSpec{Kind: KindJVM, JVM: &JVM{}},
			errs: []string{"environment.jvm.jar"},
		},
		{
			name: "kubernetes without any image is an error",
			pool: PoolSpec{Driver: "kubernetes", Environment: "e"},
			env:  &EnvironmentSpec{Kind: KindNative, Command: []string{"x"}},
			errs: []string{"image"},
		},
		{
			name:   "group list mismatch is an error",
			pool:   PoolSpec{Driver: "kubernetes", Image: "i", Groups: []string{"g"}},
			groups: nil,
			errs:   []string{"groups"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, errs := ResolvePool(tc.pool, tc.env, tc.groups)
			var fields []string
			for _, e := range errs {
				fields = append(fields, e.Field)
			}
			if !reflect.DeepEqual(fields, tc.errs) {
				t.Fatalf("error fields = %v, want %v", fields, tc.errs)
			}
			if tc.errs == nil && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("effective spec mismatch\n got: %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

func TestResolvePoolDoesNotAliasInputs(t *testing.T) {
	env := &EnvironmentSpec{Kind: KindNative, Command: []string{"bin"}, Env: map[string]string{"A": "1"}}
	got, errs := ResolvePool(PoolSpec{Driver: "process", Environment: "e"}, env, nil)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	got.Env["A"] = "changed"
	got.Command[0] = "changed"
	if env.Env["A"] != "1" || env.Command[0] != "bin" {
		t.Errorf("mutating the effective spec changed the environment: %+v", env)
	}
}

func TestPoolSpecPlacementValidation(t *testing.T) {
	base := PoolSpec{Role: RoleGame, Mode: ModeDynamic, Image: "i", Command: []string{"c"},
		Resources: Resources{MemoryMiB: 1}, Scaling: Scaling{Min: 0, Max: 1}}
	tests := []struct {
		name  string
		mut   func(*PoolSpec)
		field string
	}{
		{"v1 driver kubernetes is valid", func(s *PoolSpec) { s.Driver = "kubernetes" }, ""},
		{"placement driver process is valid", func(s *PoolSpec) { s.Placement.Driver = "process" }, ""},
		{"agent with host is valid", func(s *PoolSpec) { s.Placement = Placement{Driver: "agent", Host: "mc01"} }, ""},
		{"agent without host is rejected", func(s *PoolSpec) { s.Placement.Driver = "agent" }, "placement.host"},
		{"unknown driver is rejected", func(s *PoolSpec) { s.Placement.Driver = "nomad" }, "placement.driver"},
		{"missing driver is rejected", func(s *PoolSpec) {}, "placement.driver"},
		{"conflicting drivers are rejected", func(s *PoolSpec) { s.Driver = "process"; s.Placement.Driver = "kubernetes" }, "driver"},
		{"environment replaces image requirement", func(s *PoolSpec) { s.Driver = "kubernetes"; s.Image = ""; s.Environment = "e" }, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.mut(&s)
			errs := s.WithDefaults().Validate()
			if tc.field == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			for _, e := range errs {
				if e.Field == tc.field {
					return
				}
			}
			t.Fatalf("no error for field %q in %v", tc.field, errs)
		})
	}
}

func TestEnvironmentSpecValidate(t *testing.T) {
	tests := []struct {
		name  string
		spec  EnvironmentSpec
		field string
	}{
		{"jvm with jar", EnvironmentSpec{Kind: KindJVM, JVM: &JVM{Jar: "a.jar"}}, ""},
		{"jvm without anything", EnvironmentSpec{Kind: KindJVM}, "jvm.jar"},
		{"native with command", EnvironmentSpec{Kind: KindNative, Command: []string{"x"}}, ""},
		{"container needs image", EnvironmentSpec{Kind: KindContainer}, "image"},
		{"unknown kind", EnvironmentSpec{Kind: "wasm"}, "kind"},
		{"jvm block on native", EnvironmentSpec{Kind: KindNative, Command: []string{"x"}, JVM: &JVM{}}, "jvm"},
		{"bad readiness", EnvironmentSpec{Kind: KindNative, Command: []string{"x"}, Readiness: "http"}, "readiness"},
		{"bad port", EnvironmentSpec{Kind: KindNative, Command: []string{"x"}, Port: 70000}, "port"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.spec.Validate()
			if tc.field == "" && len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
			if tc.field != "" && (len(errs) == 0 || errs[0].Field != tc.field) {
				t.Fatalf("errors = %v, want first field %q", errs, tc.field)
			}
		})
	}
}

func TestEnvironmentSpecWithDefaults(t *testing.T) {
	got := EnvironmentSpec{Kind: KindNative}.WithDefaults()
	if got.Port != DefaultPort || got.Readiness != ReadinessSelf {
		t.Errorf("defaults = port %d readiness %q", got.Port, got.Readiness)
	}
}
