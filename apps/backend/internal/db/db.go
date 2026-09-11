// Package db owns the SQLite connection that hosted mode uses for accounts,
// login sessions, per-user provider configuration, and lockout counters.
//
// It is a leaf: it imports nothing from the rest of the module, which is what
// keeps the "persistence is the filesystem only" departure bounded and
// legible (design §5). Review sessions are not stored here.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	// Registers the pure-Go "sqlite" driver. mattn/go-sqlite3 is not an
	// option: CGO_ENABLED=0 go build ./... is a required gate and the Docker
	// image is built without a C toolchain.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrSchemaAhead reports a database recording a migration this binary does
// not carry — i.e. the binary is older than the data. Continuing would run
// unknown-shaped SQL, so it is fatal.
var ErrSchemaAhead = errors.New("db: database schema is newer than this binary")

// Options configures Open.
type Options struct {
	// Path is the database file. Its parent directory is created if absent.
	Path string
}

// Open returns a connection pool with WAL, a 5s busy timeout, foreign-key
// enforcement, and exactly one connection.
//
// MaxOpenConns(1) serialises reads as well as writes. That is a real ceiling,
// accepted deliberately: the workload is one or two indexed lookups per
// request, and serialising removes SQLITE_BUSY retry handling from every call
// site. The process's concurrency ceiling is already MAX_CONCURRENT_BUILDS.
// It also means an Argon2id hash must never be computed while a transaction
// is open — see the ordering discipline in auth.Service.
func Open(ctx context.Context, o Options) (*sql.DB, error) {
	if strings.TrimSpace(o.Path) == "" {
		return nil, errors.New("db: path is required")
	}
	if dir := filepath.Dir(o.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("db: create parent directory: %w", err)
		}
	}
	// Pragmas go in the DSN rather than as post-connect Exec calls so they
	// cannot be missed if the pool ever opens a fresh connection.
	dsn := "file:" + o.Path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)"
	handle, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	if err := handle.PingContext(ctx); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return handle, nil
}

// Migrate applies every embedded migration not yet recorded, in numeric
// order, each inside a transaction that also records its version. Migrations
// are forward-only: there is no down path.
func Migrate(ctx context.Context, handle *sql.DB) error {
	if _, err := handle.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("db: ensure schema_migrations: %w", err)
	}
	applied, err := appliedVersions(ctx, handle)
	if err != nil {
		return err
	}
	pending, known, err := loadMigrations()
	if err != nil {
		return err
	}
	for version := range applied {
		if !known[version] {
			return fmt.Errorf("%w: version %d is recorded but this binary has no such migration", ErrSchemaAhead, version)
		}
	}
	for _, m := range pending {
		if applied[m.version] {
			continue
		}
		if err := apply(ctx, handle, m); err != nil {
			return err
		}
	}
	return nil
}

type migration struct {
	version int
	name    string
	body    string
}

func appliedVersions(ctx context.Context, handle *sql.DB) (map[int]bool, error) {
	rows, err := handle.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("db: read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("db: scan schema_migrations: %w", err)
		}
		out[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: iterate schema_migrations: %w", err)
	}
	return out, nil
}

func loadMigrations() ([]migration, map[int]bool, error) {
	entries, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, nil, fmt.Errorf("db: list migrations: %w", err)
	}
	out := make([]migration, 0, len(entries))
	known := map[int]bool{}
	for _, name := range entries {
		base := filepath.Base(name)
		prefix, _, found := strings.Cut(base, "_")
		if !found {
			return nil, nil, fmt.Errorf("db: migration %q is not named <version>_<description>.sql", base)
		}
		version, convErr := strconv.Atoi(prefix)
		if convErr != nil {
			return nil, nil, fmt.Errorf("db: migration %q has a non-numeric version: %w", base, convErr)
		}
		body, readErr := migrationFS.ReadFile(name)
		if readErr != nil {
			return nil, nil, fmt.Errorf("db: read migration %q: %w", base, readErr)
		}
		out = append(out, migration{version: version, name: base, body: string(body)})
		known[version] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, known, nil
}

func apply(ctx context.Context, handle *sql.DB, m migration) error {
	tx, err := handle.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin migration %s: %w", m.name, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once Commit succeeds
	// The migration body is embedded source, not input: it is the only
	// string in this package that is not a parameterised statement.
	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		return fmt.Errorf("db: apply migration %s: %w", m.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
		m.version, time.Now().Unix()); err != nil {
		return fmt.Errorf("db: record migration %s: %w", m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration %s: %w", m.name, err)
	}
	return nil
}
