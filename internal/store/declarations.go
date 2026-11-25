package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// declaration is the shared row shape of environments and groups.
type declaration struct {
	name       string
	specText   string
	source     model.Source
	generation int64
	createdAt  string
	updatedAt  string
}

// Table names are constants of this package, never user input.
const (
	tableEnvironments = "environments"
	tableGroups       = "groups"
)

func declSelect(table string) string {
	return "SELECT name, spec, source, generation, created_at, updated_at FROM " + table
}

func scanDecl(sc rowScanner) (declaration, error) {
	var d declaration
	var source string
	if err := sc.Scan(&d.name, &d.specText, &source, &d.generation, &d.createdAt, &d.updatedAt); err != nil {
		return declaration{}, err
	}
	d.source = model.Source(source)
	return d, nil
}

// putDecl stores a declaration with the generation rules of PutPool and returns the stored row.
func (s *SQLite) putDecl(ctx context.Context, table, name string, spec any, source model.Source) (declaration, bool, error) {
	canon, err := canonicalJSON(spec)
	if err != nil {
		return declaration{}, false, fmt.Errorf("encode spec of %s %q: %w", table, name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return declaration{}, false, fmt.Errorf("begin %s transaction: %w", table, err)
	}
	defer func() { _ = tx.Rollback() }()

	now := s.clock.Now().UTC()
	cur, err := scanDecl(tx.QueryRowContext(ctx, declSelect(table)+" WHERE name = ?", name))
	changed := true
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx,
			"INSERT INTO "+table+" (name, spec, source, generation, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)",
			name, canon, string(source), formatTime(now), formatTime(now))
		if err != nil {
			return declaration{}, false, fmt.Errorf("insert %s %q: %w", table, name, err)
		}
		cur = declaration{name: name, generation: 1, createdAt: formatTime(now), updatedAt: formatTime(now)}
	case err != nil:
		return declaration{}, false, fmt.Errorf("load %s %q: %w", table, name, err)
	case cur.specText == canon:
		changed = false
		if cur.source != source {
			if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET source = ? WHERE name = ?", string(source), name); err != nil {
				return declaration{}, false, fmt.Errorf("update source of %s %q: %w", table, name, err)
			}
		}
	default:
		_, err = tx.ExecContext(ctx,
			"UPDATE "+table+" SET spec = ?, source = ?, generation = generation + 1, updated_at = ? WHERE name = ?",
			canon, string(source), formatTime(now), name)
		if err != nil {
			return declaration{}, false, fmt.Errorf("update %s %q: %w", table, name, err)
		}
		cur.generation++
		cur.updatedAt = formatTime(now)
	}
	if err := tx.Commit(); err != nil {
		return declaration{}, false, fmt.Errorf("commit %s %q: %w", table, name, err)
	}
	cur.specText, cur.source = canon, source
	return cur, changed, nil
}

func (s *SQLite) getDecl(ctx context.Context, table, name string) (declaration, error) {
	d, err := scanDecl(s.db.QueryRowContext(ctx, declSelect(table)+" WHERE name = ?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return declaration{}, fmt.Errorf("get %s %q: %w", table, name, ErrNotFound)
	}
	if err != nil {
		return declaration{}, fmt.Errorf("get %s %q: %w", table, name, err)
	}
	return d, nil
}

func (s *SQLite) listDecls(ctx context.Context, table string) ([]declaration, error) {
	rows, err := s.db.QueryContext(ctx, declSelect(table)+" ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", table, err)
	}
	defer rows.Close()
	var out []declaration
	for rows.Next() {
		d, err := scanDecl(rows)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", table, err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list %s: %w", table, err)
	}
	return out, nil
}

func (s *SQLite) deleteDecl(ctx context.Context, table, name string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("delete %s %q: %w", table, name, err)
	}
	return requireAffected(res, fmt.Sprintf("delete %s %q", table, name))
}

func (d declaration) decode(spec any) (created, updated time.Time, err error) {
	if err = json.Unmarshal([]byte(d.specText), spec); err != nil {
		return created, updated, fmt.Errorf("decode spec of %q: %w", d.name, err)
	}
	if created, err = parseTime(d.createdAt); err != nil {
		return created, updated, fmt.Errorf("%q created_at: %w", d.name, err)
	}
	if updated, err = parseTime(d.updatedAt); err != nil {
		return created, updated, fmt.Errorf("%q updated_at: %w", d.name, err)
	}
	return created, updated, nil
}

func (d declaration) environment() (model.Environment, error) {
	e := model.Environment{Name: d.name, Source: d.source, Generation: d.generation}
	var err error
	e.CreatedAt, e.UpdatedAt, err = d.decode(&e.Spec)
	return e, err
}

func (d declaration) group() (model.Group, error) {
	g := model.Group{Name: d.name, Source: d.source, Generation: d.generation}
	var err error
	g.CreatedAt, g.UpdatedAt, err = d.decode(&g.Spec)
	return g, err
}

// PutEnvironment creates or replaces an environment; generation rules as in PutPool.
func (s *SQLite) PutEnvironment(ctx context.Context, name string, spec model.EnvironmentSpec, source model.Source) (model.Environment, bool, error) {
	d, changed, err := s.putDecl(ctx, tableEnvironments, name, spec, source)
	if err != nil {
		return model.Environment{}, false, err
	}
	e, err := d.environment()
	return e, changed, err
}

// GetEnvironment returns the environment; ErrNotFound if it does not exist.
func (s *SQLite) GetEnvironment(ctx context.Context, name string) (model.Environment, error) {
	d, err := s.getDecl(ctx, tableEnvironments, name)
	if err != nil {
		return model.Environment{}, err
	}
	return d.environment()
}

// ListEnvironments returns all environments ordered by name.
func (s *SQLite) ListEnvironments(ctx context.Context) ([]model.Environment, error) {
	ds, err := s.listDecls(ctx, tableEnvironments)
	if err != nil {
		return nil, err
	}
	out := make([]model.Environment, 0, len(ds))
	for _, d := range ds {
		e, err := d.environment()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// DeleteEnvironment removes the environment; ErrNotFound if it does not exist.
func (s *SQLite) DeleteEnvironment(ctx context.Context, name string) error {
	return s.deleteDecl(ctx, tableEnvironments, name)
}

// PutGroup creates or replaces a group; generation rules as in PutPool.
func (s *SQLite) PutGroup(ctx context.Context, name string, spec model.GroupSpec, source model.Source) (model.Group, bool, error) {
	d, changed, err := s.putDecl(ctx, tableGroups, name, spec, source)
	if err != nil {
		return model.Group{}, false, err
	}
	g, err := d.group()
	return g, changed, err
}

// GetGroup returns the group; ErrNotFound if it does not exist.
func (s *SQLite) GetGroup(ctx context.Context, name string) (model.Group, error) {
	d, err := s.getDecl(ctx, tableGroups, name)
	if err != nil {
		return model.Group{}, err
	}
	return d.group()
}

// ListGroups returns all groups ordered by name.
func (s *SQLite) ListGroups(ctx context.Context) ([]model.Group, error) {
	ds, err := s.listDecls(ctx, tableGroups)
	if err != nil {
		return nil, err
	}
	out := make([]model.Group, 0, len(ds))
	for _, d := range ds {
		g, err := d.group()
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// DeleteGroup removes the group; ErrNotFound if it does not exist.
func (s *SQLite) DeleteGroup(ctx context.Context, name string) error {
	return s.deleteDecl(ctx, tableGroups, name)
}
