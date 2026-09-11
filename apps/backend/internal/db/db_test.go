package db_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jtumidanski/converge/internal/db"
)

func open(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "converge.db")
	handle, err := db.Open(context.Background(), db.Options{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	return handle
}

// TestOpenAppliesPragmas checks the effect, not the DSN spelling: WAL,
// foreign-key enforcement, and a single connection are load-bearing for the
// rest of the design (design §5), so they get observed rather than assumed.
func TestOpenAppliesPragmas(t *testing.T) {
	handle := open(t)
	var journal string
	if err := handle.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journal)
	}
	var fk int
	if err := handle.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d, want 1", fk)
	}
	if got := handle.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	handle := open(t) // path includes a "nested" directory that does not exist
	if err := handle.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := db.Open(context.Background(), db.Options{Path: ""}); err == nil {
		t.Fatal("Open with an empty path should fail")
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	handle := open(t)
	ctx := context.Background()
	for i := range 2 {
		if err := db.Migrate(ctx, handle); err != nil {
			t.Fatalf("Migrate run %d: %v", i+1, err)
		}
	}
	var applied int
	if err := handle.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", applied)
	}
	// Every table the design depends on exists after one Migrate.
	for _, table := range []string{"users", "login_sessions", "user_providers", "login_attempts"} {
		var name string
		err := handle.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

// TestMigrateRejectsASchemaFromTheFuture is the "binary is older than the
// database" guard from design §5: rolling back the image must fail loudly
// rather than silently running against a schema it does not understand.
func TestMigrateRejectsASchemaFromTheFuture(t *testing.T) {
	handle := open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, handle); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := handle.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (9999, 0)"); err != nil {
		t.Fatalf("insert future version: %v", err)
	}
	err := db.Migrate(ctx, handle)
	if !errors.Is(err, db.ErrSchemaAhead) {
		t.Fatalf("Migrate error = %v, want ErrSchemaAhead", err)
	}
}

// TestForeignKeysCascade proves ON DELETE CASCADE is live, which is what
// FR-2.7 leans on to remove a user's login sessions and providers.
func TestForeignKeysCascade(t *testing.T) {
	handle := open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, handle); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	_, err := handle.ExecContext(ctx,
		"INSERT INTO users (id, username, username_fold, password_hash, created_at, updated_at) VALUES (?,?,?,?,?,?)",
		"u1", "Alice", "alice", "hash", 1, 1)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	_, err = handle.ExecContext(ctx,
		"INSERT INTO login_sessions (token_hash, user_id, created_at, expires_at, last_seen_at) VALUES (?,?,?,?,?)",
		[]byte("hash"), "u1", 1, 2, 1)
	if err != nil {
		t.Fatalf("insert login session: %v", err)
	}
	if _, err := handle.ExecContext(ctx, "DELETE FROM users WHERE id = ?", "u1"); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var sessions int
	if err := handle.QueryRow("SELECT count(*) FROM login_sessions").Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("login_sessions after cascade = %d, want 0", sessions)
	}
}
