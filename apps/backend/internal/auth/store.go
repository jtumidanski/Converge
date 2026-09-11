package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jtumidanski/converge/internal/config"
)

// Store is the only file in this repository that contains SQL.
//
// Every statement is parameterised: no value is ever concatenated into a
// query string. Times are persisted as unix seconds (INTEGER), matching the
// schema, and are always returned as UTC.
//
// Uniqueness conflicts are detected by a check-then-insert inside a
// transaction rather than by matching on driver error text. That is exact
// here and nowhere else: the pool has exactly one connection, so a
// transaction holds the only connection for its duration, and this process is
// the only writer of the database. Matching on "UNIQUE constraint failed"
// would couple us to the driver's error strings. The UNIQUE constraints stay
// in the schema as a backstop.
type Store struct {
	db *sql.DB
}

// NewStore wraps an already-opened, already-migrated handle. It never opens
// or migrates the database itself: that is db.Open and db.Migrate's job.
func NewStore(handle *sql.DB) *Store { return &Store{db: handle} }

func unix(t time.Time) int64     { return t.Unix() }
func fromUnix(v int64) time.Time { return time.Unix(v, 0).UTC() }

// ---- users ----------------------------------------------------------------

// CreateUser inserts an account, rejecting a case-insensitive duplicate
// username with *Error{Code: CodeUsernameTaken} (FR-2.4).
func (s *Store) CreateUser(ctx context.Context, u User) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin create user: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE username_fold = ?`, u.usernameFold).Scan(&exists)
	switch {
	case err == nil:
		return &Error{Code: CodeUsernameTaken, Message: "That username is already registered."}
	case errors.Is(err, sql.ErrNoRows):
		// fall through to insert
	default:
		return fmt.Errorf("auth: check username: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users (id, username, username_fold, password_hash, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		u.id, u.username, u.usernameFold, u.passwordHash, unix(u.createdAt), unix(u.updatedAt)); err != nil {
		return fmt.Errorf("auth: insert user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("auth: commit create user: %w", err)
	}
	return nil
}

func scanUser(row interface{ Scan(dest ...any) error }) (User, error) {
	var u User
	var created, updated int64
	if err := row.Scan(&u.id, &u.username, &u.usernameFold, &u.passwordHash, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("auth: scan user: %w", err)
	}
	u.createdAt, u.updatedAt = fromUnix(created), fromUnix(updated)
	return u, nil
}

const userColumns = `id, username, username_fold, password_hash, created_at, updated_at`

// UserByFold looks up an account by its folded (case-insensitive) username.
func (s *Store) UserByFold(ctx context.Context, fold string) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username_fold = ?`, fold)
	return scanUser(row)
}

// UserByID looks up an account by its primary key.
func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// SetPasswordHash replaces the stored Argon2id hash and bumps updated_at.
func (s *Store) SetPasswordHash(ctx context.Context, userID, hash string, now time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		hash, unix(now), userID); err != nil {
		return fmt.Errorf("auth: set password hash: %w", err)
	}
	return nil
}

// DeleteUser removes an account. Login sessions, provider configs, and the
// user's own attempt row cascade via ON DELETE CASCADE / explicit key match
// (FR-2.7).
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("auth: delete user: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: delete user rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountUsers returns the number of accounts.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count users: %w", err)
	}
	return n, nil
}

// ---- login sessions ---------------------------------------------------

// CreateLoginSession inserts a fresh, cookie-backed session row.
func (s *Store) CreateLoginSession(ctx context.Context, ls LoginSession) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO login_sessions (token_hash, user_id, created_at, expires_at, last_seen_at) VALUES (?,?,?,?,?)`,
		ls.tokenHash, ls.userID, unix(ls.createdAt), unix(ls.expiresAt), unix(ls.lastSeenAt)); err != nil {
		return fmt.Errorf("auth: insert login session: %w", err)
	}
	return nil
}

// LoginSession looks a session up by its token hash (a primary-key lookup,
// FR-3.3's "compared by hash lookup, not by scanning and comparing
// plaintext").
func (s *Store) LoginSession(ctx context.Context, tokenHash []byte) (LoginSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, created_at, expires_at, last_seen_at FROM login_sessions WHERE token_hash = ?`,
		tokenHash)
	var ls LoginSession
	var created, expires, lastSeen int64
	if err := row.Scan(&ls.tokenHash, &ls.userID, &created, &expires, &lastSeen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LoginSession{}, ErrNotFound
		}
		return LoginSession{}, fmt.Errorf("auth: scan login session: %w", err)
	}
	ls.createdAt, ls.expiresAt, ls.lastSeenAt = fromUnix(created), fromUnix(expires), fromUnix(lastSeen)
	return ls, nil
}

// TouchLoginSession updates last_seen_at only, extending idle expiry.
func (s *Store) TouchLoginSession(ctx context.Context, tokenHash []byte, at time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE login_sessions SET last_seen_at = ? WHERE token_hash = ?`,
		unix(at), tokenHash); err != nil {
		return fmt.Errorf("auth: touch login session: %w", err)
	}
	return nil
}

// DeleteLoginSession removes one session, e.g. on logout.
func (s *Store) DeleteLoginSession(ctx context.Context, tokenHash []byte) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM login_sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("auth: delete login session: %w", err)
	}
	return nil
}

// DeleteOtherLoginSessions removes every session for userID except keep, used
// to invalidate other devices on a password change (FR-2.6).
func (s *Store) DeleteOtherLoginSessions(ctx context.Context, userID string, keep []byte) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM login_sessions WHERE user_id = ? AND token_hash != ?`,
		userID, keep); err != nil {
		return fmt.Errorf("auth: delete other login sessions: %w", err)
	}
	return nil
}

// DeleteExpiredLoginSessions reaps sessions past their absolute expiry or
// past idle timeout (FR-3.7), returning how many were removed.
func (s *Store) DeleteExpiredLoginSessions(ctx context.Context, now time.Time, idle time.Duration) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM login_sessions WHERE expires_at <= ? OR last_seen_at <= ?`,
		unix(now), unix(now.Add(-idle)))
	if err != nil {
		return 0, fmt.Errorf("auth: delete expired login sessions: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("auth: delete expired login sessions rows affected: %w", err)
	}
	return n, nil
}

// ---- provider configurations -------------------------------------------

const userProviderColumns = `id, user_id, slug, display_name, kind, base_url, token_ciphertext, token_nonce, token_last4, token_set_at, created_at, updated_at`

func scanUserProvider(row interface{ Scan(dest ...any) error }) (UserProvider, error) {
	var p UserProvider
	var kind string
	var tokenSetAt, created, updated int64
	if err := row.Scan(&p.id, &p.userID, &p.slug, &p.displayName, &kind, &p.baseURL,
		&p.tokenCiphertext, &p.tokenNonce, &p.tokenLast4, &tokenSetAt, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserProvider{}, ErrNotFound
		}
		return UserProvider{}, fmt.Errorf("auth: scan user provider: %w", err)
	}
	p.kind = config.Kind(kind)
	p.tokenSetAt, p.createdAt, p.updatedAt = fromUnix(tokenSetAt), fromUnix(created), fromUnix(updated)
	return p, nil
}

// CreateUserProvider inserts a provider config, rejecting a duplicate slug
// for the same user with *Error{Code: CodeProviderSlugTaken}. The
// uniqueness is per user: a different user may use the same slug.
func (s *Store) CreateUserProvider(ctx context.Context, p UserProvider) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin create user provider: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM user_providers WHERE user_id = ? AND slug = ?`, p.userID, p.slug).Scan(&exists)
	switch {
	case err == nil:
		return &Error{Code: CodeProviderSlugTaken, Message: "You already have a provider with that slug."}
	case errors.Is(err, sql.ErrNoRows):
		// fall through to insert
	default:
		return fmt.Errorf("auth: check provider slug: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO user_providers (`+userProviderColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.id, p.userID, p.slug, p.displayName, string(p.kind), p.baseURL,
		p.tokenCiphertext, p.tokenNonce, p.tokenLast4, unix(p.tokenSetAt), unix(p.createdAt), unix(p.updatedAt)); err != nil {
		return fmt.Errorf("auth: insert user provider: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("auth: commit create user provider: %w", err)
	}
	return nil
}

// ListUserProviders returns every provider config for userID, sorted by
// slug.
func (s *Store) ListUserProviders(ctx context.Context, userID string) ([]UserProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userProviderColumns+` FROM user_providers WHERE user_id = ? ORDER BY slug`, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: list user providers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []UserProvider
	for rows.Next() {
		p, err := scanUserProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterate user providers: %w", err)
	}
	return out, nil
}

// UserProviderByID looks up one provider config, scoped by user_id in the
// WHERE clause so a foreign id is indistinguishable from an unknown one
// (FR-4.2).
func (s *Store) UserProviderByID(ctx context.Context, userID, id string) (UserProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+userProviderColumns+` FROM user_providers WHERE user_id = ? AND id = ?`, userID, id)
	return scanUserProvider(row)
}

// UpdateUserProvider updates the mutable fields of a provider config. slug
// is deliberately absent: it is immutable.
func (s *Store) UpdateUserProvider(ctx context.Context, p UserProvider) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE user_providers SET display_name = ?, kind = ?, base_url = ?, token_ciphertext = ?, token_nonce = ?, token_last4 = ?, token_set_at = ?, updated_at = ? WHERE user_id = ? AND id = ?`,
		p.displayName, string(p.kind), p.baseURL, p.tokenCiphertext, p.tokenNonce, p.tokenLast4, unix(p.tokenSetAt), unix(p.updatedAt),
		p.userID, p.id)
	if err != nil {
		return fmt.Errorf("auth: update user provider: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: update user provider rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUserProvider removes one provider config, scoped by user_id.
func (s *Store) DeleteUserProvider(ctx context.Context, userID, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM user_providers WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("auth: delete user provider: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: delete user provider rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountUserProviders returns how many provider configs userID has.
func (s *Store) CountUserProviders(ctx context.Context, userID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_providers WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count user providers: %w", err)
	}
	return n, nil
}

// ---- throttle counters -------------------------------------------------

// Attempt returns the throttle counter for scope/key, or a zero Attempt with
// a nil error when absent: an absent counter is a zero counter, not an
// error.
func (s *Store) Attempt(ctx context.Context, scope, key string) (Attempt, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT scope, key, failures, window_start, locked_until FROM login_attempts WHERE scope = ? AND key = ?`,
		scope, key)
	var a Attempt
	var windowStart, lockedUntil int64
	if err := row.Scan(&a.Scope, &a.Key, &a.Failures, &windowStart, &lockedUntil); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Attempt{Scope: scope, Key: key}, nil
		}
		return Attempt{}, fmt.Errorf("auth: scan attempt: %w", err)
	}
	a.WindowStart, a.LockedUntil = fromUnix(windowStart), fromUnix(lockedUntil)
	return a, nil
}

// SaveAttempt upserts a throttle counter.
func (s *Store) SaveAttempt(ctx context.Context, a Attempt) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO login_attempts (scope, key, failures, window_start, locked_until) VALUES (?,?,?,?,?)
		 ON CONFLICT(scope, key) DO UPDATE SET failures = excluded.failures, window_start = excluded.window_start, locked_until = excluded.locked_until`,
		a.Scope, a.Key, a.Failures, unix(a.WindowStart), unix(a.LockedUntil)); err != nil {
		return fmt.Errorf("auth: save attempt: %w", err)
	}
	return nil
}

// UpdateAttempt reads the counter for scope/key, applies fn, and writes the
// result back, all inside one transaction. That makes the whole
// read-modify-write atomic: with a single-connection pool, a concurrent
// UpdateAttempt blocks for the connection until this transaction commits, so
// two callers can never observe the same row and each write back a
// conflicting increment. fn must be pure arithmetic — no I/O, nothing slow —
// since it runs while the sole connection is held.
func (s *Store) UpdateAttempt(ctx context.Context, scope, key string, fn func(Attempt) Attempt) (Attempt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, fmt.Errorf("auth: begin update attempt: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx,
		`SELECT scope, key, failures, window_start, locked_until FROM login_attempts WHERE scope = ? AND key = ?`,
		scope, key)
	var a Attempt
	var windowStart, lockedUntil int64
	switch err := row.Scan(&a.Scope, &a.Key, &a.Failures, &windowStart, &lockedUntil); {
	case err == nil:
		a.WindowStart, a.LockedUntil = fromUnix(windowStart), fromUnix(lockedUntil)
	case errors.Is(err, sql.ErrNoRows):
		a = Attempt{Scope: scope, Key: key}
	default:
		return Attempt{}, fmt.Errorf("auth: scan attempt for update: %w", err)
	}

	a = fn(a)
	a.Scope, a.Key = scope, key

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO login_attempts (scope, key, failures, window_start, locked_until) VALUES (?,?,?,?,?)
		 ON CONFLICT(scope, key) DO UPDATE SET failures = excluded.failures, window_start = excluded.window_start, locked_until = excluded.locked_until`,
		a.Scope, a.Key, a.Failures, unix(a.WindowStart), unix(a.LockedUntil)); err != nil {
		return Attempt{}, fmt.Errorf("auth: save attempt in update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Attempt{}, fmt.Errorf("auth: commit update attempt: %w", err)
	}
	return a, nil
}

// ClearAttempt removes a throttle counter, e.g. after a successful login.
func (s *Store) ClearAttempt(ctx context.Context, scope, key string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM login_attempts WHERE scope = ? AND key = ?`, scope, key); err != nil {
		return fmt.Errorf("auth: clear attempt: %w", err)
	}
	return nil
}

// DeleteElapsedAttempts reaps counters that are neither locked nor within
// their window, returning how many were removed.
func (s *Store) DeleteElapsedAttempts(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM login_attempts WHERE locked_until <= ? AND window_start <= ?`,
		unix(before), unix(before))
	if err != nil {
		return 0, fmt.Errorf("auth: delete elapsed attempts: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("auth: delete elapsed attempts rows affected: %w", err)
	}
	return n, nil
}

// ---- reachability -------------------------------------------------------

// Ping reports whether the database is reachable, for /healthz.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("auth: ping: %w", err)
	}
	return nil
}
