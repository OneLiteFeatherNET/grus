package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

func envSpec() model.EnvironmentSpec {
	return model.EnvironmentSpec{Kind: model.KindJVM, JVM: &model.JVM{Jar: "paper.jar"}, Port: 25565}
}

func TestPutEnvironmentGenerationRules(t *testing.T) {
	clock := &fakeClock{now: startTime}
	s := newTestStore(t, clock)
	ctx := context.Background()

	first, changed, err := s.PutEnvironment(ctx, "paper", envSpec(), model.SourceAPI)
	if err != nil || !changed || first.Generation != 1 || first.Source != model.SourceAPI {
		t.Fatalf("insert = %+v changed=%v err=%v, want generation 1, source api, changed", first, changed, err)
	}
	clock.advance(time.Minute)
	same, changed, err := s.PutEnvironment(ctx, "paper", envSpec(), model.SourceAPI)
	if err != nil || changed || same.Generation != 1 || !same.UpdatedAt.Equal(first.UpdatedAt) {
		t.Errorf("same spec = %+v changed=%v err=%v, want unchanged generation and updatedAt", same, changed, err)
	}
	spec := envSpec()
	spec.Port = 25570
	next, changed, err := s.PutEnvironment(ctx, "paper", spec, model.SourceAPI)
	if err != nil || !changed || next.Generation != 2 || !next.UpdatedAt.Equal(clock.now) || !next.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("changed spec = %+v changed=%v err=%v, want generation 2, new updatedAt, kept createdAt", next, changed, err)
	}
	got, err := s.GetEnvironment(ctx, "paper")
	if err != nil || !reflect.DeepEqual(got, next) {
		t.Errorf("GetEnvironment = %+v err=%v, want %+v", got, err, next)
	}
}

func TestPutEnvironmentSourceChangeKeepsGeneration(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	if _, _, err := s.PutEnvironment(ctx, "paper", envSpec(), model.SourceAPI); err != nil {
		t.Fatal(err)
	}
	e, changed, err := s.PutEnvironment(ctx, "paper", envSpec(), model.SourceKubernetes)
	if err != nil || changed || e.Generation != 1 || e.Source != model.SourceKubernetes {
		t.Fatalf("source takeover = %+v changed=%v err=%v", e, changed, err)
	}
	got, _ := s.GetEnvironment(ctx, "paper")
	if got.Source != model.SourceKubernetes {
		t.Errorf("stored source = %q, want kubernetes", got.Source)
	}
}

func TestEnvironmentListDeleteAndNotFound(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	for _, n := range []string{"velocity", "paper"} {
		if _, _, err := s.PutEnvironment(ctx, n, envSpec(), model.SourceAPI); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListEnvironments(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "paper" || list[1].Name != "velocity" {
		t.Fatalf("ListEnvironments = %+v err=%v, want paper, velocity", list, err)
	}
	if err := s.DeleteEnvironment(ctx, "paper"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	if _, err := s.GetEnvironment(ctx, "paper"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetEnvironment after delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteEnvironment(ctx, "paper"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestGroupCRUD(t *testing.T) {
	clock := &fakeClock{now: startTime}
	s := newTestStore(t, clock)
	ctx := context.Background()
	spec := model.GroupSpec{
		Templates: []model.TemplateRef{{Name: "plugins", Tag: "latest"}},
		Env:       map[string]string{"A": "1"},
		Labels:    map[string]string{"team": "olf"},
	}
	g, changed, err := s.PutGroup(ctx, "base", spec, model.SourceFile)
	if err != nil || !changed || g.Generation != 1 || g.Source != model.SourceFile {
		t.Fatalf("PutGroup = %+v changed=%v err=%v", g, changed, err)
	}
	spec.Env["A"] = "2"
	g, changed, err = s.PutGroup(ctx, "base", spec, model.SourceFile)
	if err != nil || !changed || g.Generation != 2 {
		t.Fatalf("changed PutGroup = %+v changed=%v err=%v, want generation 2", g, changed, err)
	}
	got, err := s.GetGroup(ctx, "base")
	if err != nil || !reflect.DeepEqual(got.Spec, spec) {
		t.Errorf("GetGroup = %+v err=%v, want spec %+v", got, err, spec)
	}
	list, err := s.ListGroups(ctx)
	if err != nil || len(list) != 1 {
		t.Errorf("ListGroups = %+v err=%v, want one group", list, err)
	}
	if err := s.DeleteGroup(ctx, "base"); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	if _, err := s.GetGroup(ctx, "base"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetGroup after delete = %v, want ErrNotFound", err)
	}
}

func TestPoolSourceIsStoredAndDefaultsToAPI(t *testing.T) {
	s := newTestStore(t, &fakeClock{now: startTime})
	ctx := context.Background()
	p, _, err := s.PutPool(ctx, "lobby", validSpec())
	if err != nil || p.Source != model.SourceAPI {
		t.Fatalf("PutPool = %+v err=%v, want source api", p, err)
	}
	p, changed, err := s.PutPoolAs(ctx, "lobby", validSpec(), model.SourceKubernetes)
	if err != nil || changed || p.Source != model.SourceKubernetes || p.Generation != 1 {
		t.Fatalf("PutPoolAs same spec = %+v changed=%v err=%v", p, changed, err)
	}
	got, _ := s.GetPool(ctx, "lobby")
	if got.Source != model.SourceKubernetes {
		t.Errorf("stored pool source = %q, want kubernetes", got.Source)
	}
	spec := validSpec()
	spec.Placement = model.Placement{Driver: "agent", Host: "mc01"}
	spec.Environment = "paper"
	spec.Groups = []string{"base"}
	if _, _, err := s.PutPoolAs(ctx, "lobby", spec, model.SourceKubernetes); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetPool(ctx, "lobby")
	if !reflect.DeepEqual(got.Spec, spec) {
		t.Errorf("v2 pool fields did not round-trip: got %+v want %+v", got.Spec, spec)
	}
}
