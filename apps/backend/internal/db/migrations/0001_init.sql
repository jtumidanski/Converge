-- Hosted-mode schema (PRD §6.2). This database holds ONLY accounts, login
-- sessions, per-user provider configuration, and lockout counters. Review
-- sessions remain session.json files under WORKSPACE_ROOT; internal/session
-- keeps its storage responsibility.
--
-- schema_migrations is deliberately absent here: the runner creates it with
-- CREATE TABLE IF NOT EXISTS before reading it, because it must exist before
-- the first migration can be recorded.

CREATE TABLE users (
  id            TEXT PRIMARY KEY,
  username      TEXT NOT NULL,
  username_fold TEXT NOT NULL UNIQUE,   -- lower-cased; the uniqueness key (FR-2.1)
  password_hash TEXT NOT NULL,          -- Argon2id PHC string
  created_at    INTEGER NOT NULL,       -- unix seconds
  updated_at    INTEGER NOT NULL
);

CREATE TABLE login_sessions (
  token_hash   BLOB PRIMARY KEY,        -- SHA-256 of the cookie token; the plaintext is never stored (FR-3.3)
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,        -- absolute expiry (FR-3.4)
  last_seen_at INTEGER NOT NULL         -- drives idle expiry (FR-3.4)
);
CREATE INDEX idx_login_sessions_user ON login_sessions(user_id);
CREATE INDEX idx_login_sessions_expiry ON login_sessions(expires_at);

CREATE TABLE user_providers (
  id               TEXT PRIMARY KEY,
  user_id          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  slug             TEXT NOT NULL,
  display_name     TEXT NOT NULL,
  kind             TEXT NOT NULL,       -- 'github' | 'gitlab'
  base_url         TEXT NOT NULL,
  token_ciphertext BLOB NOT NULL,
  token_nonce      BLOB NOT NULL,
  token_last4      TEXT NOT NULL,
  token_set_at     INTEGER NOT NULL,
  created_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL,
  -- The leftmost prefix of this index also serves the per-user list query,
  -- so no separate (user_id) index is needed.
  UNIQUE (user_id, slug)
);

CREATE TABLE login_attempts (
  scope        TEXT NOT NULL,           -- 'user' | 'ip'
  key          TEXT NOT NULL,           -- folded username, or client IP
  failures     INTEGER NOT NULL,
  window_start INTEGER NOT NULL,
  locked_until INTEGER NOT NULL,
  PRIMARY KEY (scope, key)
);
-- The sweeper deletes rows whose lockout has elapsed; without this it would
-- table-scan every cycle (design §5).
CREATE INDEX idx_login_attempts_locked ON login_attempts(locked_until);
