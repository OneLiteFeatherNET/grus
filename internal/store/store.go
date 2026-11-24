// Package store persists pools, instances and the controller meta data. The SQLite implementation
// (modernc.org/sqlite, migrations with goose) lives in sqlite.go.
package store

import (
	"context"
	"errors"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

// InstanceFilter selects instances; empty fields match everything.
type InstanceFilter struct {
	Pool  string
	State model.State
	Role  model.Role
}

// Store is the controller's state. All methods are safe for concurrent use.
type Store interface {
	// PutPool creates or replaces a pool. Generation is incremented when the spec changes
	// (first insert = 1); maintenance and created_at are preserved. Returns the stored pool and
	// whether the spec changed.
	PutPool(ctx context.Context, name string, spec model.PoolSpec) (model.Pool, bool, error)
	GetPool(ctx context.Context, name string) (model.Pool, error)
	ListPools(ctx context.Context) ([]model.Pool, error)
	DeletePool(ctx context.Context, name string) error
	SetMaintenance(ctx context.Context, name string, enabled bool) (model.Pool, error)

	// CreateInstance inserts a new instance; ErrConflict if the id exists.
	CreateInstance(ctx context.Context, inst model.Instance) error
	// UpdateInstance replaces all fields of an existing instance; ErrNotFound if missing.
	UpdateInstance(ctx context.Context, inst model.Instance) error
	GetInstance(ctx context.Context, id string) (model.Instance, error)
	// FindInstanceByTokenHash returns the instance owning a token hash; ErrNotFound if none.
	FindInstanceByTokenHash(ctx context.Context, hash string) (model.Instance, error)
	ListInstances(ctx context.Context, f InstanceFilter) ([]model.Instance, error)
	DeleteInstance(ctx context.Context, id string) error

	// Meta is a small key/value table (e.g. "events.epoch").
	GetMeta(ctx context.Context, key string) (string, error)
	SetMeta(ctx context.Context, key, value string) error

	Close() error
}
