// Package controller is the brain of Grus: it owns state transitions, the reconcile loop
// (scheduler, autoscaler, adoption) and the operations the REST API exposes.
package controller

import (
	"context"
	"errors"
	"io"

	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

var (
	// ErrHasPlayers: stop without drain/force on an instance with players (HTTP 409, E6.3).
	ErrHasPlayers = errors.New("instance has players; use drain or force")
	// ErrInvalidState: the requested operation is not allowed in the current state (HTTP 409).
	ErrInvalidState = errors.New("operation not allowed in current state")
	// ErrNotFound: pool or instance unknown (HTTP 404).
	ErrNotFound = errors.New("not found")
	// ErrLimit: the namespace instance limit would be exceeded (HTTP 409).
	ErrLimit = errors.New("instance limit reached")
)

// ValidationError carries field errors of a pool spec (HTTP 422).
type ValidationError struct{ Fields []model.FieldError }

func (e *ValidationError) Error() string { return "invalid pool spec" }

// StatusUpdate is the body of PUT /v1/self/status (and the admin debug variant).
type StatusUpdate struct {
	Players    int               `json:"players"`
	MaxPlayers int               `json:"maxPlayers"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// StopOptions is the body of POST /v1/instances/{id}/stop.
type StopOptions struct {
	Drain bool `json:"drain,omitempty"`
	Force bool `json:"force,omitempty"`
}

// Service is what the API layer calls. Every mutating call applies state.Transition, persists the
// instance and publishes the matching events; the API layer never changes state itself.
type Service interface {
	PutPool(ctx context.Context, name string, spec model.PoolSpec) (model.Pool, error)
	GetPool(ctx context.Context, name string) (model.Pool, error)
	ListPools(ctx context.Context) ([]model.Pool, error)
	DeletePool(ctx context.Context, name string, drain bool) error
	SetMaintenance(ctx context.Context, name string, enabled bool) (model.Pool, error)

	ListInstances(ctx context.Context, f store.InstanceFilter) ([]model.Instance, error)
	GetInstance(ctx context.Context, id string) (model.Instance, error)
	CreateInstance(ctx context.Context, pool string) (model.Instance, error)
	StopInstance(ctx context.Context, id string, opts StopOptions) (model.Instance, error)
	Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error)
	Command(ctx context.Context, id string, line string) error

	// AuthenticateInstance maps a plain per-instance token to its live (non-terminal) instance.
	AuthenticateInstance(ctx context.Context, token string) (model.Instance, error)
	SelfReady(ctx context.Context, id string) (model.Instance, error)
	SelfStatus(ctx context.Context, id string, u StatusUpdate) (model.Instance, error)
	SelfAllocate(ctx context.Context, id string) (model.Instance, error)
	SelfShutdown(ctx context.Context, id string) (model.Instance, error)
}
