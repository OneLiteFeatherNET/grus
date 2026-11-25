package crdsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

// Group and Version of the custom resources.
const (
	Group   = "grus.onelitefeather.net"
	Version = "v1alpha1"
)

const defaultStatusInterval = 10 * time.Second

// Backend is what the syncer needs from the controller: the Applier to declare objects and read access to
// find the objects it owns and to count instances for the pool status.
type Backend interface {
	controller.Applier
	ListPools(ctx context.Context) ([]model.Pool, error)
	ListEnvironments(ctx context.Context) ([]model.Environment, error)
	ListGroups(ctx context.Context) ([]model.Group, error)
	ListInstances(ctx context.Context, f store.InstanceFilter) ([]model.Instance, error)
}

// Config holds the syncer settings.
type Config struct {
	Namespace string       // the only namespace that is watched (required)
	Clock     model.Clock  // default model.SystemClock
	Logger    *slog.Logger // default slog.Default()
	// StatusInterval is how often Tick runs in Run, and the minimum gap between two status writes of one
	// pool that only differ in replica counts. Default 10s.
	StatusInterval time.Duration
}

// kindDef describes one custom resource kind and how it maps onto the Applier.
type kindDef struct {
	kind   string
	gvr    schema.GroupVersionResource
	apply  func(ctx context.Context, b Backend, name string, obj *unstructured.Unstructured) error
	remove func(ctx context.Context, b Backend, name string) error
	// owned lists the names of stored objects with source kubernetes.
	owned func(ctx context.Context, b Backend) ([]string, error)
}

func gvr(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: Group, Version: Version, Resource: resource}
}

var kinds = []kindDef{
	{
		kind: "Environment", gvr: gvr("environments"),
		apply: func(ctx context.Context, b Backend, name string, obj *unstructured.Unstructured) error {
			var spec model.EnvironmentSpec
			if err := decodeSpec(obj, &spec); err != nil {
				return &decodeError{err}
			}
			_, err := b.ApplyEnvironment(ctx, model.SourceKubernetes, name, spec)
			return err
		},
		remove: func(ctx context.Context, b Backend, name string) error {
			return b.RemoveEnvironment(ctx, model.SourceKubernetes, name)
		},
		owned: func(ctx context.Context, b Backend) ([]string, error) {
			items, err := b.ListEnvironments(ctx)
			var names []string
			for _, i := range items {
				if i.Source == model.SourceKubernetes {
					names = append(names, i.Name)
				}
			}
			return names, err
		},
	},
	{
		kind: "Group", gvr: gvr("groups"),
		apply: func(ctx context.Context, b Backend, name string, obj *unstructured.Unstructured) error {
			var spec model.GroupSpec
			if err := decodeSpec(obj, &spec); err != nil {
				return &decodeError{err}
			}
			_, err := b.ApplyGroup(ctx, model.SourceKubernetes, name, spec)
			return err
		},
		remove: func(ctx context.Context, b Backend, name string) error {
			return b.RemoveGroup(ctx, model.SourceKubernetes, name)
		},
		owned: func(ctx context.Context, b Backend) ([]string, error) {
			items, err := b.ListGroups(ctx)
			var names []string
			for _, i := range items {
				if i.Source == model.SourceKubernetes {
					names = append(names, i.Name)
				}
			}
			return names, err
		},
	},
	{
		kind: "Pool", gvr: gvr("pools"),
		apply: func(ctx context.Context, b Backend, name string, obj *unstructured.Unstructured) error {
			var spec model.PoolSpec
			if err := decodeSpec(obj, &spec); err != nil {
				return &decodeError{err}
			}
			_, err := b.ApplyPool(ctx, model.SourceKubernetes, name, spec)
			return err
		},
		remove: func(ctx context.Context, b Backend, name string) error {
			// Deleting the custom resource drains the pool like DELETE /v1/pools does.
			return b.RemovePool(ctx, model.SourceKubernetes, name, true)
		},
		owned: func(ctx context.Context, b Backend) ([]string, error) {
			items, err := b.ListPools(ctx)
			var names []string
			for _, i := range items {
				if i.Source == model.SourceKubernetes {
					names = append(names, i.Name)
				}
			}
			return names, err
		},
	},
}

// decodeError is a spec that cannot be read into the domain type (wrong field type).
type decodeError struct{ err error }

func (e *decodeError) Error() string { return e.err.Error() }

// Syncer watches the custom resources of one namespace; see the package documentation.
type Syncer struct {
	client dynamic.Interface
	be     Backend
	cfg    Config
	clock  model.Clock
	log    *slog.Logger

	mu        sync.Mutex // serialises reconciles and status writes
	ctx       context.Context
	informers map[string]cache.SharedIndexInformer // by kind
	results   map[string]result                    // by kind/name: outcome of the last apply
	lastWrite map[string]time.Time                 // by kind/name: time of the last status write
	written   map[string]map[string]any            // by kind/name: the last status written
}

// New returns a syncer for cfg.Namespace.
func New(client dynamic.Interface, be Backend, cfg Config) *Syncer {
	if cfg.Clock == nil {
		cfg.Clock = model.SystemClock{}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.StatusInterval <= 0 {
		cfg.StatusInterval = defaultStatusInterval
	}
	return &Syncer{
		client: client, be: be, cfg: cfg, clock: cfg.Clock, log: cfg.Logger,
		informers: map[string]cache.SharedIndexInformer{},
		results:   map[string]result{},
		lastWrite: map[string]time.Time{},
		written:   map[string]map[string]any{},
	}
}

// Run starts the informers, applies every custom resource, removes stored kubernetes objects whose resource
// is gone, and then keeps the store and the pool statuses in sync until ctx ends.
func (s *Syncer) Run(ctx context.Context) error {
	if err := s.Start(ctx); err != nil {
		return err
	}
	t := time.NewTicker(s.cfg.StatusInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// Start runs the informers and the initial full sync and returns once both are done.
func (s *Syncer) Start(ctx context.Context) error {
	s.ctx = ctx
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(s.client, 0, s.cfg.Namespace, nil)
	for _, k := range kinds {
		inf := factory.ForResource(k.gvr).Informer()
		s.informers[k.kind] = inf
		if _, err := inf.AddEventHandler(s.handler(k)); err != nil {
			return fmt.Errorf("watch %s: %w", k.gvr.Resource, err)
		}
	}
	factory.Start(ctx.Done())
	for gvr, ok := range factory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			return fmt.Errorf("cache of %s did not sync", gvr.Resource)
		}
	}
	return s.fullSync(ctx)
}

// handler reconciles the object of every event except the initial list, which fullSync covers in order.
func (s *Syncer) handler(k kindDef) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerDetailedFuncs{
		AddFunc: func(obj any, initial bool) {
			if !initial {
				s.reconcileObj(k, obj)
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldM, err1 := meta.Accessor(oldObj)
			newM, err2 := meta.Accessor(newObj)
			if err1 == nil && err2 == nil && oldM.GetGeneration() == newM.GetGeneration() && !s.failed(k, newM.GetName()) {
				return // status or metadata only; our own status writes land here
			}
			s.reconcileObj(k, newObj)
		},
		DeleteFunc: func(obj any) {
			if t, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = t.Obj
			}
			s.reconcileObj(k, obj)
		},
	}
}

func (s *Syncer) reconcileObj(k kindDef, obj any) {
	m, err := meta.Accessor(obj)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconcile(s.ctx, k, m.GetName())
}

func (s *Syncer) failed(k kindDef, name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.results[key(k, name)]
	return ok && !r.ok
}

func key(k kindDef, name string) string { return k.kind + "/" + name }

// fullSync applies every custom resource (environments and groups before pools) and removes the stored
// objects with source kubernetes that no longer have a custom resource.
func (s *Syncer) fullSync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range kinds {
		present := map[string]bool{}
		for _, o := range s.informers[k.kind].GetStore().List() {
			m, err := meta.Accessor(o)
			if err != nil {
				continue
			}
			present[m.GetName()] = true
			s.reconcile(ctx, k, m.GetName())
		}
		names, err := k.owned(ctx, s.be)
		if err != nil {
			return fmt.Errorf("list stored %s: %w", k.gvr.Resource, err)
		}
		for _, name := range names {
			if !present[name] {
				s.reconcile(ctx, k, name)
			}
		}
	}
	return nil
}

// reconcile brings one object in line with its custom resource; the caller holds s.mu. A missing resource
// removes the stored object, but only when it is owned by kubernetes: an api object of the same name stays.
func (s *Syncer) reconcile(ctx context.Context, k kindDef, name string) {
	raw, exists, err := s.informers[k.kind].GetStore().GetByKey(s.cfg.Namespace + "/" + name)
	if err != nil {
		s.log.Error("crdsync: read cache", "kind", k.kind, "name", name, "error", err)
		return
	}
	id := key(k, name)
	if !exists {
		delete(s.results, id)
		delete(s.lastWrite, id)
		delete(s.written, id)
		names, err := k.owned(ctx, s.be)
		if err != nil {
			s.log.Error("crdsync: list stored objects", "kind", k.kind, "error", err)
			return
		}
		for _, n := range names {
			if n != name {
				continue
			}
			if err := k.remove(ctx, s.be, name); err != nil && !errors.Is(err, controller.ErrNotFound) {
				s.log.Error("crdsync: remove", "kind", k.kind, "name", name, "error", err)
			}
		}
		return
	}
	obj, ok := raw.(*unstructured.Unstructured)
	if !ok {
		return
	}
	res := resultOf(k.apply(ctx, s.be, name, obj))
	if !res.ok {
		s.log.Warn("crdsync: apply rejected", "kind", k.kind, "name", name, "reason", res.reason, "message", res.message)
	}
	s.results[id] = res
	s.writeStatus(ctx, k, obj, true)
}

// Tick retries applies that failed and refreshes the pool statuses. Run calls it every StatusInterval.
func (s *Syncer) Tick(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range kinds {
		for _, o := range s.informers[k.kind].GetStore().List() {
			m, err := meta.Accessor(o)
			if err != nil {
				continue
			}
			if r, ok := s.results[key(k, m.GetName())]; ok && !r.ok {
				s.reconcile(ctx, k, m.GetName())
				continue
			}
			if k.kind == "Pool" {
				if u, ok := o.(*unstructured.Unstructured); ok {
					s.writeStatus(ctx, k, u, false)
				}
			}
		}
	}
}
