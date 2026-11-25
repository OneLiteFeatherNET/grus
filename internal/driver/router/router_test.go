package router

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/driver/fake"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// namedDriver overrides the driver name of a fake, so the default selector can tell drivers apart.
type namedDriver struct {
	*fake.Driver
	name string
}

func (d namedDriver) Name() string { return d.name }

// selectTo returns a selector that always picks host.
func selectTo(host string) Selector {
	return func(driver.StartSpec) (string, error) { return host, nil }
}

func spec(id, poolDriver string) driver.StartSpec {
	return driver.StartSpec{
		Instance: model.Instance{ID: id, Pool: "lobby"},
		Pool:     model.Pool{Name: "lobby", Spec: model.PoolSpec{Driver: poolDriver}},
	}
}

func twoHosts(t *testing.T, sel Selector) (*Router, *fake.Driver, *fake.Driver) {
	t.Helper()
	a, b := fake.New("host-a"), fake.New("host-b")
	r := New(sel)
	for _, d := range []driver.Driver{a, b} {
		if err := r.Add(d); err != nil {
			t.Fatalf("add %s: %v", d.Host(), err)
		}
	}
	return r, a, b
}

func TestNameAndHost(t *testing.T) {
	r := New(nil)
	if r.Name() != "router" {
		t.Errorf("Name() = %q, want router", r.Name())
	}
	if r.Host() != "*" {
		t.Errorf("Host() = %q, want *", r.Host())
	}
}

func TestStartRunsOnSelectedHostAndPrefixesRef(t *testing.T) {
	tests := []struct {
		name     string
		selected string
	}{
		{name: "first host", selected: "host-a"},
		{name: "second host", selected: "host-b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, a, b := twoHosts(t, selectTo(tt.selected))

			ref, err := r.Start(context.Background(), spec("lobby-1", "kubernetes"))
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			if want := tt.selected + "/lobby-1"; ref != want {
				t.Errorf("ref = %q, want %q", ref, want)
			}
			started := map[string]int{"host-a": len(a.Started()), "host-b": len(b.Started())}
			if started[tt.selected] != 1 {
				t.Errorf("selected host %s started %d instances, want 1", tt.selected, started[tt.selected])
			}
			for host, n := range started {
				if host != tt.selected && n != 0 {
					t.Errorf("host %s started %d instances, want 0", host, n)
				}
			}
		})
	}
}

func TestStartFailsForSelectorErrorOrUnknownHost(t *testing.T) {
	tests := []struct {
		name     string
		selector Selector
	}{
		{name: "selector error", selector: func(driver.StartSpec) (string, error) { return "", errors.New("no placement") }},
		{name: "unknown host", selector: selectTo("host-z")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, a, b := twoHosts(t, tt.selector)

			if _, err := r.Start(context.Background(), spec("lobby-1", "kubernetes")); err == nil {
				t.Fatal("start succeeded, want error")
			}
			if len(a.Started())+len(b.Started()) != 0 {
				t.Error("an instance was started although the selection failed")
			}
		})
	}
}

func TestDefaultSelectorPicksTheOnlyHostWithMatchingDriverName(t *testing.T) {
	tests := []struct {
		name       string
		poolDriver string
		hosts      map[string]string // host -> driver name
		wantHost   string
		wantErr    bool
	}{
		{
			name:       "single match",
			poolDriver: "kubernetes",
			hosts:      map[string]string{"k8s": "kubernetes", "local": "process"},
			wantHost:   "k8s",
		},
		{
			name:       "no matching driver",
			poolDriver: "kubernetes",
			hosts:      map[string]string{"local": "process"},
			wantErr:    true,
		},
		{
			name:       "two matching hosts are ambiguous",
			poolDriver: "process",
			hosts:      map[string]string{"agent-1": "process", "agent-2": "process"},
			wantErr:    true,
		},
		{
			name:       "pool without driver",
			poolDriver: "",
			hosts:      map[string]string{"k8s": "kubernetes"},
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(nil)
			for host, name := range tt.hosts {
				if err := r.Add(namedDriver{Driver: fake.New(host), name: name}); err != nil {
					t.Fatalf("add %s: %v", host, err)
				}
			}

			ref, err := r.Start(context.Background(), spec("lobby-1", tt.poolDriver))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("start succeeded with ref %q, want error", ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			if want := tt.wantHost + "/lobby-1"; ref != want {
				t.Errorf("ref = %q, want %q", ref, want)
			}
		})
	}
}

// startedOn starts one instance per host through the router and returns the refs.
func startedOn(t *testing.T, r *Router, host string) string {
	t.Helper()
	r.selector = selectTo(host)
	ref, err := r.Start(context.Background(), spec("lobby-1", "kubernetes"))
	if err != nil {
		t.Fatalf("start on %s: %v", host, err)
	}
	return ref
}

func TestRefOperationsDispatchToTheHostOfTheRef(t *testing.T) {
	ops := []struct {
		name string
		run  func(r *Router, ref string) error
	}{
		{name: "stop", run: func(r *Router, ref string) error {
			return r.Stop(context.Background(), ref, "stop", time.Second)
		}},
		{name: "kill", run: func(r *Router, ref string) error {
			return r.Kill(context.Background(), ref)
		}},
		{name: "command", run: func(r *Router, ref string) error {
			return r.Command(context.Background(), ref, "say hi")
		}},
		{name: "logs", run: func(r *Router, ref string) error {
			rc, err := r.Logs(context.Background(), ref, 10, false)
			if err == nil {
				_ = rc.Close()
			}
			return err
		}},
	}
	tests := []struct {
		name    string
		ref     func(t *testing.T, r *Router) string
		wantErr error
	}{
		{name: "known host and instance", ref: func(t *testing.T, r *Router) string {
			return startedOn(t, r, "host-b")
		}},
		{name: "unknown host", ref: func(*testing.T, *Router) string { return "host-z/lobby-1" }, wantErr: driver.ErrNotFound},
		{name: "ref without host", ref: func(*testing.T, *Router) string { return "lobby-1" }, wantErr: driver.ErrNotFound},
		{name: "empty ref", ref: func(*testing.T, *Router) string { return "" }, wantErr: driver.ErrNotFound},
	}
	for _, op := range ops {
		for _, tt := range tests {
			t.Run(op.name+"/"+tt.name, func(t *testing.T) {
				r, _, b := twoHosts(t, selectTo("host-a"))
				ref := tt.ref(t, r)

				err := op.run(r, ref)
				if tt.wantErr != nil {
					if !errors.Is(err, tt.wantErr) {
						t.Fatalf("error = %v, want %v", err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatalf("%s %q: %v", op.name, ref, err)
				}
				if op.name == "command" && b.Commands()["lobby-1"] == nil {
					t.Errorf("host-b did not receive the command")
				}
			})
		}
	}
}

func TestStopAndKillPassTheSubRefToTheHost(t *testing.T) {
	r, _, b := twoHosts(t, selectTo("host-b"))
	ref := startedOn(t, r, "host-b")

	if err := r.Stop(context.Background(), ref, "stop", time.Second); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := r.Kill(context.Background(), ref); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if got := b.Stopped(); len(got) != 1 || got[0] != "lobby-1" {
		t.Errorf("host-b stopped %v, want [lobby-1]", got)
	}
	if got := b.Killed(); len(got) != 1 || got[0] != "lobby-1" {
		t.Errorf("host-b killed %v, want [lobby-1]", got)
	}
}

func TestListMergesHostsAndPrefixesRefs(t *testing.T) {
	r, a, b := twoHosts(t, selectTo("host-a"))
	a.Add(driver.Observed{InstanceID: "lobby-1", Ref: "lobby-1", Phase: driver.PhaseRunning})
	b.Add(driver.Observed{InstanceID: "survival-1", Ref: "survival-1", Phase: driver.PhaseRunning})

	got, err := r.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	refs := map[string]string{}
	for _, o := range got {
		refs[o.InstanceID] = o.Ref
	}
	want := map[string]string{"lobby-1": "host-a/lobby-1", "survival-1": "host-b/survival-1"}
	if len(refs) != len(want) {
		t.Fatalf("listed %v, want %v", refs, want)
	}
	for id, ref := range want {
		if refs[id] != ref {
			t.Errorf("ref of %s = %q, want %q", id, refs[id], ref)
		}
	}
}

func TestListWithOneFailingHostReturnsHealthyInstancesAndTypedError(t *testing.T) {
	tests := []struct {
		name       string
		failing    []string // hosts whose List fails
		wantIDs    []string // instance ids still listed
		wantFailed []string
	}{
		{name: "host-a fails", failing: []string{"host-a"}, wantIDs: []string{"survival-1"}, wantFailed: []string{"host-a"}},
		{name: "host-b fails", failing: []string{"host-b"}, wantIDs: []string{"lobby-1"}, wantFailed: []string{"host-b"}},
		{name: "all hosts fail", failing: []string{"host-a", "host-b"}, wantIDs: nil, wantFailed: []string{"host-a", "host-b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, a, b := twoHosts(t, selectTo("host-a"))
			a.Add(driver.Observed{InstanceID: "lobby-1", Ref: "lobby-1"})
			b.Add(driver.Observed{InstanceID: "survival-1", Ref: "survival-1"})
			listErr := errors.New("agent offline")
			for _, host := range tt.failing {
				if host == "host-a" {
					a.ListErr = listErr
				} else {
					b.ListErr = listErr
				}
			}

			got, err := r.List(context.Background())

			var partial *PartialListError
			if !errors.As(err, &partial) {
				t.Fatalf("error = %v, want *PartialListError", err)
			}
			if !errors.Is(err, listErr) {
				t.Errorf("PartialListError does not wrap the host error: %v", err)
			}
			if !equalStrings(partial.Hosts, tt.wantFailed) {
				t.Errorf("failed hosts = %v, want %v", partial.Hosts, tt.wantFailed)
			}
			if !equalStrings(partial.FailedHosts(), tt.wantFailed) {
				t.Errorf("FailedHosts() = %v, want %v", partial.FailedHosts(), tt.wantFailed)
			}
			var ids []string
			for _, o := range got {
				ids = append(ids, o.InstanceID)
			}
			if !equalStrings(ids, tt.wantIDs) {
				t.Errorf("listed instances = %v, want %v (healthy hosts only)", ids, tt.wantIDs)
			}
		})
	}
}

func TestAddRejectsInvalidAndDuplicateHosts(t *testing.T) {
	tests := []struct {
		name   string
		second driver.Driver
	}{
		{name: "duplicate host", second: fake.New("host-a")},
		{name: "empty host", second: fake.New("")},
		{name: "host with slash", second: fake.New("a/b")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(nil)
			if err := r.Add(fake.New("host-a")); err != nil {
				t.Fatalf("first add: %v", err)
			}
			if err := r.Add(tt.second); err == nil {
				t.Fatal("add succeeded, want error")
			}
		})
	}
}

func TestRemovedHostIsUnknownAndDropsFromList(t *testing.T) {
	r, _, b := twoHosts(t, selectTo("host-b"))
	ref := startedOn(t, r, "host-b")
	b.Add(driver.Observed{InstanceID: "lobby-1", Ref: "lobby-1"})

	r.Remove("host-b")

	if err := r.Kill(context.Background(), ref); !errors.Is(err, driver.ErrNotFound) {
		t.Errorf("kill after remove = %v, want ErrNotFound", err)
	}
	got, err := r.List(context.Background())
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("list after remove = %v, want no instances", got)
	}
	if _, err := r.Start(context.Background(), spec("lobby-2", "kubernetes")); err == nil {
		t.Fatal("start on removed selector host succeeded, want error")
	}
}

func TestConcurrentAddRemoveAndListAreSafe(t *testing.T) {
	r := New(nil)
	if err := r.Add(fake.New("host-a")); err != nil {
		t.Fatalf("add: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = r.List(context.Background())
				_ = r.Add(fake.New("agent-x"))
				r.Remove("agent-x")
			}
		}()
	}
	wg.Wait()
	if _, err := r.List(context.Background()); err != nil {
		t.Fatalf("list after concurrent use: %v", err)
	}
}

// equalStrings compares two string slices, treating nil and empty as equal.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
