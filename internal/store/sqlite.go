package store

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/pressly/goose/v3"
	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// timeLayout is RFC3339 with a fixed nine-digit fraction. Fixed width keeps the text column sortable
// in chronological order, which ListInstances relies on for ORDER BY created_at.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

const instanceColumns = "id, pool, role, pool_generation, host, driver_ref, address, state, state_reason, " +
	"state_since, players, max_players, labels, allocated_at, last_heartbeat, token_hash, created_at"

// SQLite is the Store implementation on a SQLite database file (modernc.org/sqlite, pure Go).
type SQLite struct {
	db    *sql.DB
	clock model.Clock
}

var _ Store = (*SQLite)(nil)

// OpenSQLite opens (creating if needed) the database file, enables WAL and busy_timeout=5000, foreign keys,
// runs all migrations and returns the store. The pool is limited to one connection to avoid SQLITE_BUSY.
func OpenSQLite(ctx context.Context, path string, clock model.Clock) (*SQLite, error) {
	if clock == nil {
		clock = model.SystemClock{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory for %q: %w", path, err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database %q: %w", path, err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLite{db: db, clock: clock}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	fsys, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("select embedded migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// Close closes the database.
func (s *SQLite) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close sqlite database: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

// PutPool creates or replaces a pool. The spec is compared by its canonical JSON: a new pool gets generation 1,
// an unchanged spec keeps generation and updated_at, a changed spec increments the generation.
// Maintenance and created_at are never changed by PutPool.
func (s *SQLite) PutPool(ctx context.Context, name string, spec model.PoolSpec) (model.Pool, bool, error) {
	canon, err := canonicalJSON(spec)
	if err != nil {
		return model.Pool{}, false, fmt.Errorf("encode spec of pool %q: %w", name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Pool{}, false, fmt.Errorf("begin pool transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	cur, storedSpec, err := scanPool(tx.QueryRowContext(ctx, poolSelect+" WHERE name = ?", name))
	now := s.clock.Now().UTC()
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx,
			`INSERT INTO pools (name, spec, generation, maintenance, created_at, updated_at) VALUES (?, ?, 1, 0, ?, ?)`,
			name, canon, formatTime(now), formatTime(now))
		if err != nil {
			return model.Pool{}, false, fmt.Errorf("insert pool %q: %w", name, err)
		}
		cur = model.Pool{Name: name, Generation: 1, CreatedAt: now}
	case err != nil:
		return model.Pool{}, false, fmt.Errorf("load pool %q: %w", name, err)
	case storedSpec == canon:
		if err := tx.Commit(); err != nil {
			return model.Pool{}, false, fmt.Errorf("commit pool %q: %w", name, err)
		}
		return cur, false, nil
	default:
		_, err = tx.ExecContext(ctx,
			`UPDATE pools SET spec = ?, generation = generation + 1, updated_at = ? WHERE name = ?`,
			canon, formatTime(now), name)
		if err != nil {
			return model.Pool{}, false, fmt.Errorf("update spec of pool %q: %w", name, err)
		}
		cur.Generation++
		cur.UpdatedAt = now
	}
	if err := tx.Commit(); err != nil {
		return model.Pool{}, false, fmt.Errorf("commit pool %q: %w", name, err)
	}
	cur.Spec = spec
	cur.UpdatedAt = now
	return cur, true, nil
}

const poolSelect = `SELECT name, spec, generation, maintenance, created_at, updated_at FROM pools`

// GetPool returns the pool; ErrNotFound if it does not exist.
func (s *SQLite) GetPool(ctx context.Context, name string) (model.Pool, error) {
	p, _, err := scanPool(s.db.QueryRowContext(ctx, poolSelect+" WHERE name = ?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Pool{}, fmt.Errorf("get pool %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return model.Pool{}, fmt.Errorf("get pool %q: %w", name, err)
	}
	return p, nil
}

// ListPools returns all pools ordered by name.
func (s *SQLite) ListPools(ctx context.Context) ([]model.Pool, error) {
	rows, err := s.db.QueryContext(ctx, poolSelect+" ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}
	defer rows.Close()
	var out []model.Pool
	for rows.Next() {
		p, _, err := scanPool(rows)
		if err != nil {
			return nil, fmt.Errorf("list pools: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}
	return out, nil
}

// DeletePool removes the pool row only; its instances stay. ErrNotFound if the pool does not exist.
func (s *SQLite) DeletePool(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM pools WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete pool %q: %w", name, err)
	}
	return requireAffected(res, fmt.Sprintf("delete pool %q", name))
}

// SetMaintenance switches the maintenance flag and returns the updated pool; ErrNotFound if it does not exist.
func (s *SQLite) SetMaintenance(ctx context.Context, name string, enabled bool) (model.Pool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE pools SET maintenance = ? WHERE name = ?`, boolInt(enabled), name)
	if err != nil {
		return model.Pool{}, fmt.Errorf("set maintenance of pool %q: %w", name, err)
	}
	if err := requireAffected(res, fmt.Sprintf("set maintenance of pool %q", name)); err != nil {
		return model.Pool{}, err
	}
	return s.GetPool(ctx, name)
}

// CreateInstance inserts a new instance; ErrConflict if the id or a non-empty token hash already exists.
func (s *SQLite) CreateInstance(ctx context.Context, inst model.Instance) error {
	args, err := instanceArgs(inst)
	if err != nil {
		return fmt.Errorf("create instance %q: %w", inst.ID, err)
	}
	placeholders := "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_, err = s.db.ExecContext(ctx, "INSERT INTO instances ("+instanceColumns+") VALUES ("+placeholders+")", args...)
	if err != nil {
		return wrapWriteErr(err, fmt.Sprintf("create instance %q", inst.ID))
	}
	return nil
}

// UpdateInstance replaces all fields of an existing instance; ErrNotFound if it does not exist.
func (s *SQLite) UpdateInstance(ctx context.Context, inst model.Instance) error {
	args, err := instanceArgs(inst)
	if err != nil {
		return fmt.Errorf("update instance %q: %w", inst.ID, err)
	}
	// args[0] is the id; the SET list covers the remaining columns, the id goes last into WHERE.
	res, err := s.db.ExecContext(ctx,
		`UPDATE instances SET pool = ?, role = ?, pool_generation = ?, host = ?, driver_ref = ?, address = ?, `+
			`state = ?, state_reason = ?, state_since = ?, players = ?, max_players = ?, labels = ?, `+
			`allocated_at = ?, last_heartbeat = ?, token_hash = ?, created_at = ? WHERE id = ?`,
		append(args[1:], args[0])...)
	if err != nil {
		return wrapWriteErr(err, fmt.Sprintf("update instance %q", inst.ID))
	}
	return requireAffected(res, fmt.Sprintf("update instance %q", inst.ID))
}

// GetInstance returns the instance; ErrNotFound if it does not exist.
func (s *SQLite) GetInstance(ctx context.Context, id string) (model.Instance, error) {
	inst, err := scanInstance(s.db.QueryRowContext(ctx, "SELECT "+instanceColumns+" FROM instances WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Instance{}, fmt.Errorf("get instance %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return model.Instance{}, fmt.Errorf("get instance %q: %w", id, err)
	}
	return inst, nil
}

// FindInstanceByTokenHash returns the instance owning the token hash; ErrNotFound for "" or an unknown hash.
func (s *SQLite) FindInstanceByTokenHash(ctx context.Context, hash string) (model.Instance, error) {
	if hash == "" {
		return model.Instance{}, fmt.Errorf("find instance by token hash: %w", ErrNotFound)
	}
	inst, err := scanInstance(s.db.QueryRowContext(ctx,
		"SELECT "+instanceColumns+" FROM instances WHERE token_hash = ?", hash))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Instance{}, fmt.Errorf("find instance by token hash: %w", ErrNotFound)
	}
	if err != nil {
		return model.Instance{}, fmt.Errorf("find instance by token hash: %w", err)
	}
	return inst, nil
}

// ListInstances returns the instances matching every non-empty filter field, ordered by created_at then id.
func (s *SQLite) ListInstances(ctx context.Context, f InstanceFilter) ([]model.Instance, error) {
	q := "SELECT " + instanceColumns + " FROM instances WHERE 1 = 1"
	var args []any
	if f.Pool != "" {
		q += " AND pool = ?"
		args = append(args, f.Pool)
	}
	if f.State != "" {
		q += " AND state = ?"
		args = append(args, string(f.State))
	}
	if f.Role != "" {
		q += " AND role = ?"
		args = append(args, string(f.Role))
	}
	q += " ORDER BY created_at, id"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	defer rows.Close()
	var out []model.Instance
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("list instances: %w", err)
		}
		out = append(out, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	return out, nil
}

// DeleteInstance removes the instance row; ErrNotFound if it does not exist.
func (s *SQLite) DeleteInstance(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM instances WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete instance %q: %w", id, err)
	}
	return requireAffected(res, fmt.Sprintf("delete instance %q", id))
}

// GetMeta returns the value of a meta key; ErrNotFound if the key is missing.
func (s *SQLite) GetMeta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("get meta %q: %w", key, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("get meta %q: %w", key, err)
	}
	return value, nil
}

// SetMeta stores a meta value, replacing an existing one.
func (s *SQLite) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	if err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}
	return nil
}

// scanPool reads one pool row and also returns the stored spec text, which PutPool compares against.
func scanPool(sc rowScanner) (model.Pool, string, error) {
	var p model.Pool
	var specText, createdAt, updatedAt string
	var maintenance int64
	if err := sc.Scan(&p.Name, &specText, &p.Generation, &maintenance, &createdAt, &updatedAt); err != nil {
		return model.Pool{}, "", err
	}
	if err := json.Unmarshal([]byte(specText), &p.Spec); err != nil {
		return model.Pool{}, "", fmt.Errorf("decode spec of pool %q: %w", p.Name, err)
	}
	var err error
	p.Maintenance = maintenance != 0
	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.Pool{}, "", fmt.Errorf("pool %q created_at: %w", p.Name, err)
	}
	if p.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.Pool{}, "", fmt.Errorf("pool %q updated_at: %w", p.Name, err)
	}
	return p, specText, nil
}

func scanInstance(sc rowScanner) (model.Instance, error) {
	var inst model.Instance
	var role, state, stateSince, labelsText, createdAt string
	var allocatedAt, lastHeartbeat sql.NullString
	err := sc.Scan(&inst.ID, &inst.Pool, &role, &inst.PoolGeneration, &inst.Host, &inst.DriverRef,
		&inst.Address, &state, &inst.StateReason, &stateSince, &inst.Players, &inst.MaxPlayers,
		&labelsText, &allocatedAt, &lastHeartbeat, &inst.TokenHash, &createdAt)
	if err != nil {
		return model.Instance{}, err
	}
	inst.Role = model.Role(role)
	inst.State = model.State(state)
	if err := json.Unmarshal([]byte(labelsText), &inst.Labels); err != nil {
		return model.Instance{}, fmt.Errorf("decode labels of instance %q: %w", inst.ID, err)
	}
	if inst.StateSince, err = parseTime(stateSince); err != nil {
		return model.Instance{}, fmt.Errorf("instance %q state_since: %w", inst.ID, err)
	}
	if inst.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.Instance{}, fmt.Errorf("instance %q created_at: %w", inst.ID, err)
	}
	if inst.AllocatedAt, err = parseNullTime(allocatedAt); err != nil {
		return model.Instance{}, fmt.Errorf("instance %q allocated_at: %w", inst.ID, err)
	}
	if inst.LastHeartbeat, err = parseNullTime(lastHeartbeat); err != nil {
		return model.Instance{}, fmt.Errorf("instance %q last_heartbeat: %w", inst.ID, err)
	}
	return inst, nil
}

// instanceArgs returns the column values of instanceColumns in order.
func instanceArgs(inst model.Instance) ([]any, error) {
	labels, err := json.Marshal(inst.Labels)
	if err != nil {
		return nil, fmt.Errorf("encode labels: %w", err)
	}
	return []any{
		inst.ID, inst.Pool, string(inst.Role), inst.PoolGeneration, inst.Host, inst.DriverRef,
		inst.Address, string(inst.State), inst.StateReason, formatTime(inst.StateSince),
		inst.Players, inst.MaxPlayers, string(labels),
		formatNullTime(inst.AllocatedAt), formatNullTime(inst.LastHeartbeat),
		inst.TokenHash, formatTime(inst.CreatedAt),
	}, nil
}

// canonicalJSON encodes v with sorted object keys and no insignificant whitespace.
func canonicalJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return "", err
	}
	out, err := json.Marshal(generic)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func formatNullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

func parseNullTime(n sql.NullString) (*time.Time, error) {
	if !n.Valid {
		return nil, nil
	}
	t, err := parseTime(n.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func requireAffected(res sql.Result, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return nil
}

// wrapWriteErr maps unique-key violations (primary key or token hash) to ErrConflict.
func wrapWriteErr(err error, what string) error {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY, sqlitelib.SQLITE_CONSTRAINT_UNIQUE:
			return fmt.Errorf("%s: %w", what, ErrConflict)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}
