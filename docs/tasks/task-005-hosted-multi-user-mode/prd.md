# Hosted Multi-User Mode — Product Requirements Document

Version: v1
Status: Draft
Created: 2026-09-10
---

## 1. Overview

Converge today is a single-tenant tool. One process serves one reviewer, providers come
from `PROVIDERS__*` environment variables, and every review session in `WORKSPACE_ROOT`
belongs to whoever can reach the port. That is the right shape for `docker run` on a
laptop and it must keep working untouched.

This task adds a second shape: a **hosted** instance that several reviewers share. In
hosted mode the application gains accounts. A reviewer registers, logs in with a username
and password, configures their own providers and access tokens through the UI instead of
through the environment, and sees only their own reviews. Logging out, changing a
password, and deleting an account are all self-service; there is no administrator role.

The two modes are selected by a single environment variable and are mutually exclusive at
runtime. `standalone` is the default, so an existing deployment that upgrades and changes
nothing gets byte-for-byte today's behaviour — no database file, no login screen, no
change to the on-disk layout of existing review sessions. `hosted` is opt-in and brings
the pieces single-tenant mode never needed: a SQLite store for users, login sessions and
per-user provider configuration; encrypted-at-rest provider tokens; authentication
middleware; ownership on review sessions; and per-user repository mirrors.

## 2. Goals

Primary goals:

- Add `CONVERGE_MODE=standalone|hosted`, defaulting to `standalone`.
- Preserve standalone behaviour exactly: no auth, no database, `PROVIDERS__*` unchanged,
  existing review sessions still resume.
- In hosted mode, support self-service registration, username/password login, logout,
  password change, and account deletion.
- In hosted mode, let each user configure their own provider instances (kind, base URL,
  display name, access token) through the UI, replacing `PROVIDERS__*` entirely.
- Encrypt provider tokens at rest with an operator-supplied master key; never return a
  token to any client once stored.
- Scope review sessions, workspaces, and repository mirrors to their owning user so no
  account can observe, resume, or delete another account's work.
- Throttle and lock out repeated failed logins.

Non-goals:

- OIDC, SAML, OAuth device flow, or any external identity provider.
- Administrator role, user-management screens, or an invite/approval workflow.
- Email delivery of any kind, and therefore self-service password reset.
- Teams, organisations, shared reviews, or any cross-user sharing.
- Per-user disk quotas, billing, or usage metering.
- Migrating an existing standalone deployment's review sessions into a hosted user
  account.
- Changing the review, diff, or reconstruction behaviour in any way.

## 3. User Stories

- As an operator, I want to keep running Converge exactly as I do today so that upgrading
  costs me nothing.
- As an operator, I want to set two environment variables and get a multi-user instance so
  that my team can share one deployment.
- As a reviewer on a hosted instance, I want to register an account with a username and
  password so that I can start using the instance without asking anyone.
- As a reviewer, I want to log in and stay logged in across browser restarts so that I am
  not re-authenticating every session.
- As a reviewer, I want to log out so that I can leave a shared machine safely.
- As a reviewer, I want to change my password so that I can rotate a credential I think is
  exposed.
- As a reviewer, I want to add a GitHub or GitLab provider with my own access token so
  that Converge sees exactly the repositories I have access to.
- As a reviewer, I want to edit a provider's base URL or rotate its token without
  re-entering everything else so that maintenance is cheap.
- As a reviewer, I want to see only my own in-flight and completed reviews so that a
  shared instance does not become a shared inbox.
- As a reviewer, I want my stored token never displayed back to me so that a shoulder-surf
  or a screenshot cannot leak it.
- As a reviewer, I want to delete my account and have my tokens, mirrors, and review
  workspaces removed so that leaving the instance is clean.
- As an operator, I want repeated failed logins to be throttled so that a
  network-reachable instance is not trivially brute-forced.

## 4. Functional Requirements

### 4.1 Mode selection

- **FR-1.1** `CONVERGE_MODE` accepts `standalone` (default when unset or empty) and
  `hosted`. Any other value is a startup error naming the variable, consistent with
  existing `config.Error` behaviour.
- **FR-1.2** In `standalone`, `PROVIDERS__*` is parsed exactly as today and at least one
  provider remains required.
- **FR-1.3** In `hosted`, `PROVIDERS__*` is ignored. If any such variable is present, the
  server logs one `WARN` at startup naming the ignored variables and continues.
- **FR-1.4** In `hosted`, `CONVERGE_SECRET_KEY` and `CONVERGE_DATABASE_PATH` apply
  (§4.2, §4.6). A missing or malformed `CONVERGE_SECRET_KEY` is a fatal startup error.
- **FR-1.5** In `standalone`, no database file is created, opened, or required, and
  `CONVERGE_SECRET_KEY` is not read.
- **FR-1.6** Mode is fixed for the process lifetime. There is no runtime toggle.

### 4.2 Accounts and credentials

- **FR-2.1** A user is identified by a case-insensitive unique username matching
  `^[a-zA-Z0-9][a-zA-Z0-9._-]{2,31}$` (3–32 characters, starting alphanumeric). The
  username is stored as entered and compared case-insensitively.
- **FR-2.2** Passwords are 8–1024 characters. No composition rules (no required symbol,
  digit, or mixed case) are imposed — this is a deliberate decision, not an oversight;
  length is the only requirement.
- **FR-2.3** Passwords are hashed with Argon2id (`golang.org/x/crypto/argon2`) using
  time=1, memory=64 MiB, parallelism=4, 16-byte random salt, 32-byte key, stored as a PHC
  encoded string. Parameters live in one place so they can be raised later, and
  verification reads them from the stored string rather than from the constants.
- **FR-2.4** Registration is open: any client that can reach the instance may create an
  account. Registration fails with `USERNAME_TAKEN` if the username exists
  case-insensitively.
- **FR-2.5** Login verifies the password and, on success, creates a login session and sets
  the session cookie. On an unknown username the handler still performs an Argon2id
  verification against a fixed dummy hash so that response timing does not distinguish
  "no such user" from "wrong password". Both cases return the same
  `INVALID_CREDENTIALS` error.
- **FR-2.6** Changing a password requires the current password, applies the same rules as
  FR-2.2, and revokes every login session for that user **except** the one making the
  request.
- **FR-2.7** Deleting an account requires the current password. It removes the user row
  and, by cascade, their login sessions, provider configurations, and lockout records; it
  then removes their review sessions (including workspaces) and their mirror namespace.
  Deletion is irreversible and the UI requires a typed confirmation of the username.
- **FR-2.8** There is no password reset. A user who forgets their password loses the
  account. This is stated in §9 as a known gap.

### 4.3 Login sessions

- **FR-3.1** A login session is a 32-byte cryptographically random token, base64url
  encoded, delivered in a cookie named `converge_session` with `HttpOnly`, `Path=/`, and
  `SameSite=Lax`.
- **FR-3.2** `Secure` is set when the request arrived over TLS, or unconditionally when
  `CONVERGE_SECURE_COOKIES=true`. Operators terminating TLS at a proxy set that variable.
- **FR-3.3** Only the SHA-256 hash of the token is persisted. The plaintext token exists
  in the response that creates it and in the client's cookie jar, nowhere else. It is
  never logged.
- **FR-3.4** A login session has an absolute expiry of `LOGIN_SESSION_TTL_HOURS`
  (default 720) from creation and an idle expiry of `LOGIN_SESSION_IDLE_HOURS`
  (default 168) from last use. Whichever comes first ends the session.
- **FR-3.5** `last_seen_at` is refreshed on authenticated requests but written at most
  once per five minutes per session, to keep read-heavy traffic from generating a write
  per request.
- **FR-3.6** Logout deletes the current login session row and clears the cookie. Logout
  with no valid session is a no-op returning the same success status.
- **FR-3.7** Expired login sessions are deleted by the existing background sweeper loop,
  extended to cover the auth tables. Expiry is also enforced on read, so a stale row is
  never honoured even before the sweep runs.

### 4.4 Authentication and authorisation on the API

- **FR-4.1** In hosted mode, every `/api/` route requires a valid login session except
  `GET /api/auth/mode`, `POST /api/auth/register`, and `POST /api/auth/login`. `/healthz`
  and the embedded UI assets are always unauthenticated.
- **FR-4.2** An unauthenticated request to a protected route returns `401` with code
  `UNAUTHENTICATED`. A request whose session is valid but which targets another user's
  resource returns `404` (not `403`), so resource existence is not disclosed.
- **FR-4.3** In standalone mode, `/api/auth/*` routes other than `GET /api/auth/mode` and
  all `/api/settings/*` routes return `404`, matching the existing unknown-endpoint
  behaviour. No authentication middleware runs.
- **FR-4.4** In hosted mode, every state-changing request (anything other than `GET` or
  `HEAD`) under `/api/` must carry an `Origin` header matching the request's own host, or
  a `Sec-Fetch-Site` of `same-origin`. Requests failing both checks are rejected with
  `403` / `FORBIDDEN`. Combined with `SameSite=Lax` this is the CSRF defence; no CSRF
  token is issued.
- **FR-4.5** The authenticated user's ID is carried on the request `context.Context` and
  read by handlers through a typed accessor. No handler reads the cookie directly.

### 4.5 Per-user provider configuration

- **FR-5.1** In hosted mode a user owns zero or more provider configurations. Each has a
  slug (URL-safe, unique per user, `^[a-z0-9][a-z0-9-]{0,31}$`), a display name, a kind
  (`github` or `gitlab`), a base URL, and an access token.
- **FR-5.2** Base URL must be an absolute `http`/`https` URL and is normalised by
  stripping a trailing slash, matching the existing `config` validation. When kind is
  `github` and base URL is omitted, it defaults to `https://api.github.com`, as today.
- **FR-5.3** Tokens are encrypted with AES-256-GCM before storage using a key derived
  from `CONVERGE_SECRET_KEY`. Each record carries its own random nonce. The additional
  authenticated data binds the ciphertext to its `user_id` and provider row ID, so a
  ciphertext moved between rows or users fails to decrypt.
- **FR-5.4** A token is write-only across the API. Responses expose only
  `tokenLast4` and `tokenSetAt`. There is no endpoint that returns a token in any form.
- **FR-5.5** On update, an omitted or empty token field leaves the stored token unchanged;
  a non-empty value replaces it.
- **FR-5.6** Creating or updating a provider optionally verifies the credential against
  the provider API before saving (`validate=true`, default true). A verification failure
  returns `PROVIDER_UNAUTHORIZED` and nothing is written. `validate=false` allows saving
  a configuration for an unreachable host.
- **FR-5.7** Deleting a provider configuration fails with `PROVIDER_IN_USE` while the user
  has a review session in a non-terminal state that references it.
- **FR-5.8** `GET /api/providers` (the existing endpoint the review flow uses) returns the
  calling user's configured providers in hosted mode and the env-configured providers in
  standalone mode. Its response shape does not change.
- **FR-5.9** A user with no configured providers gets an empty list and the UI directs
  them to the provider settings screen rather than showing an empty repository picker.

### 4.6 Ownership of review sessions, workspaces, and mirrors

- **FR-6.1** `session.Session` gains an `owner` field persisted in `session.json`. In
  standalone mode it is written empty; in hosted mode it is the creating user's ID.
- **FR-6.2** The on-disk workspace layout does **not** change:
  `WORKSPACE_ROOT/<session-id>/` in both modes. Isolation is enforced by the store and the
  API, not by directory nesting, so existing sessions keep resuming after an upgrade.
- **FR-6.3** In hosted mode, `Store.List` returns only the calling user's sessions, and
  `Get`/`Finish`/`Delete` on a session owned by someone else behave as not-found. Sessions
  with an empty owner are invisible in hosted mode; the server logs their count once at
  startup so an operator can clean them up.
- **FR-6.4** In standalone mode, owner is ignored entirely and every session is visible, as
  today.
- **FR-6.5** Repository mirrors are namespaced per user in hosted mode:
  `REPOSITORY_CACHE_ROOT/users/<user-id>/<mirror-key>`. Standalone keeps today's flat
  layout. Two users reviewing the same repository maintain two mirrors; this trades disk
  for a guarantee that mirror contents cannot cross an account boundary.
- **FR-6.6** Mirror pruning operates within a single user's namespace and must continue to
  honour the live-review-branch protection added in `b8a8391`. Deleting a user removes
  that user's whole mirror namespace.
- **FR-6.7** Provider credentials continue to reach git only through
  `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_0`/`GIT_CONFIG_VALUE_0`, now sourced from the
  requesting user's decrypted token rather than from configuration.

### 4.7 Login throttling

- **FR-7.1** Failed logins are counted per username and per client IP in a persisted
  table, so a restart does not clear a lockout in progress.
- **FR-7.2** After 5 consecutive failures for a username, that username is locked for
  1 minute; each subsequent failure doubles the lockout up to a 15-minute cap. A
  successful login clears the counter.
- **FR-7.3** After 20 failures from one client IP within 15 minutes, that IP is locked on
  the same doubling schedule. The IP is taken from the connection's remote address, or
  from the last hop of `X-Forwarded-For` when `CONVERGE_TRUSTED_PROXY=true`.
- **FR-7.4** A request during a lockout returns `429` with code `ACCOUNT_LOCKED` and a
  `Retry-After` header. The response does not reveal whether the username exists.
- **FR-7.5** Registration is throttled per IP on the same mechanism to blunt trivial
  account-flooding.

### 4.8 Web UI

- **FR-8.1** The SPA fetches `GET /api/auth/mode` at boot and branches on the result. In
  standalone mode no login page, no account menu, and no settings routes are reachable —
  the UI is identical to today.
- **FR-8.2** In hosted mode, unauthenticated visitors to any route are redirected to
  `/login`, which offers a link to `/register`.
- **FR-8.3** Any API response of `401` clears client auth state and redirects to `/login`,
  preserving the attempted path for post-login return.
- **FR-8.4** The app header shows the current username with a menu containing *Provider
  settings*, *Account settings*, and *Log out*.
- **FR-8.5** `/settings/providers` lists the user's providers with kind, display name,
  base URL, and masked token, and supports create, edit, and delete. The token input is
  `type="password"` with placeholder text indicating an existing token is retained if
  left blank.
- **FR-8.6** `/settings/account` supports changing the password (current, new, confirm)
  and deleting the account behind a typed-username confirmation.
- **FR-8.7** Forms use `react-hook-form` with Zod resolvers and mirror the server's
  validation rules, per the frontend guidelines. Server-side errors are surfaced on the
  relevant field where the error code identifies one.
- **FR-8.8** No password, token, or session cookie value is written to `localStorage`,
  `sessionStorage`, or any client-side log.

## 5. API Surface

All request and response bodies are JSON:API (`application/vnd.api+json`) and use the
existing `internal/jsonapi` helpers. Full request/response documents are in
`api-contracts.md`; this section is the index and the error contract.

### 5.1 New endpoints

| Method | Path | Auth | Modes | Purpose |
| --- | --- | --- | --- | --- |
| `GET` | `/api/auth/mode` | none | both | Report `standalone` or `hosted` |
| `POST` | `/api/auth/register` | none | hosted | Create an account, log in |
| `POST` | `/api/auth/login` | none | hosted | Log in |
| `POST` | `/api/auth/logout` | session | hosted | End the current login session |
| `GET` | `/api/auth/me` | session | hosted | Current user |
| `POST` | `/api/auth/password` | session | hosted | Change password |
| `DELETE` | `/api/auth/me` | session | hosted | Delete the account |
| `GET` | `/api/settings/providers` | session | hosted | List own provider configs |
| `POST` | `/api/settings/providers` | session | hosted | Create a provider config |
| `PATCH` | `/api/settings/providers/{id}` | session | hosted | Update a provider config |
| `DELETE` | `/api/settings/providers/{id}` | session | hosted | Delete a provider config |

### 5.2 Modified endpoints

No request or response shape changes. In hosted mode the following gain an implicit
`owner = current user` filter and return `401` when unauthenticated:

- `GET /api/providers`, `GET /api/providers/{provider}/repositories` and its subpaths
- `POST|GET /api/reviews`, `GET|DELETE /api/reviews/{id}` and its subpaths

`GET /healthz` is unchanged and remains unauthenticated in both modes.

### 5.3 Error codes

Added to the existing code vocabulary:

| Code | HTTP | Meaning |
| --- | --- | --- |
| `UNAUTHENTICATED` | 401 | No valid login session |
| `INVALID_CREDENTIALS` | 401 | Username or password wrong |
| `ACCOUNT_LOCKED` | 429 | Too many failed attempts; see `Retry-After` |
| `USERNAME_TAKEN` | 409 | Username already registered |
| `INVALID_USERNAME` | 422 | Username fails FR-2.1 |
| `WEAK_PASSWORD` | 422 | Password fails FR-2.2 |
| `FORBIDDEN` | 403 | Origin check failed |
| `PROVIDER_SLUG_TAKEN` | 409 | Slug already used by this user |
| `PROVIDER_UNAUTHORIZED` | 422 | Credential rejected by the provider API |
| `PROVIDER_IN_USE` | 409 | Referenced by an active review session |

Existing codes and their HTTP mappings are untouched.

## 6. Data Model

### 6.1 Store choice

Hosted mode introduces SQLite via `modernc.org/sqlite`, the pure-Go driver. `mattn/go-sqlite3`
is not viable: `CGO_ENABLED=0 go build ./...` is a required check. This is a deliberate
departure from the "persistence is the filesystem only" invariant in `CLAUDE.md`, and the
departure is bounded: SQLite holds **only** users, login sessions, per-user provider
configuration, and lockout counters. Review sessions remain `session.json` files on disk
and `internal/session` keeps its current storage responsibility.

The database lives at `CONVERGE_DATABASE_PATH` (default `/data/converge.db`) and is opened
with `journal_mode=WAL`, `busy_timeout=5000`, and `foreign_keys=ON`. `MaxOpenConns` is 1:
the workload is a handful of small queries per request, and serialising them removes
`SQLITE_BUSY` handling from every call site. Every statement is parameterised; no SQL is
built by string concatenation.

Schema is applied by a small embedded migration runner (ordered `.sql` files in
`internal/db/migrations`, tracked in `schema_migrations`), run at startup inside a
transaction. Migrations are forward-only.

### 6.2 Tables

```sql
CREATE TABLE users (
  id            TEXT PRIMARY KEY,
  username      TEXT NOT NULL,
  username_fold TEXT NOT NULL UNIQUE,   -- lower-cased, the uniqueness key
  password_hash TEXT NOT NULL,          -- Argon2id PHC string
  created_at    INTEGER NOT NULL,       -- unix seconds
  updated_at    INTEGER NOT NULL
);

CREATE TABLE login_sessions (
  token_hash   BLOB PRIMARY KEY,        -- SHA-256 of the cookie token
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,        -- absolute expiry
  last_seen_at INTEGER NOT NULL         -- drives idle expiry
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

CREATE TABLE schema_migrations (
  version    INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
);
```

IDs use the same generator as `session.NewID` so identifier handling stays uniform.

### 6.3 On-disk changes

`session.json` gains one field:

```json
{ "owner": "<user-id>" }
```

Absent or empty means unowned. Records written by an older build parse unchanged (absent
field decodes to empty), so no data migration is required; §4.6 defines how unowned
records behave in each mode.

### 6.4 Encryption

`CONVERGE_SECRET_KEY` is 32 random bytes, base64 standard encoding. It is wrapped in the
existing `config.Secret` type and registered with the log-redacting handler. It is used as
an AES-256-GCM key. Rotation is not automated in this task; the procedure (decrypt with
old, re-encrypt with new) is noted in §9.

## 7. Service Impact

Backend, in dependency order (`api → review → {provider, mirror, workspace, diff,
session} → gitx`):

- **`internal/config`** — parse `CONVERGE_MODE`, `CONVERGE_DATABASE_PATH`,
  `CONVERGE_SECRET_KEY`, `CONVERGE_SECURE_COOKIES`, `CONVERGE_TRUSTED_PROXY`,
  `LOGIN_SESSION_TTL_HOURS`, `LOGIN_SESSION_IDLE_HOURS`. Make `PROVIDERS__*` required only
  in standalone. Extend `Secrets()` to include the master key.
- **`internal/db`** *(new)* — driver setup, pragmas, embedded migrations, migration runner.
  Imports nothing from the module.
- **`internal/auth`** *(new)* — `User`, `LoginSession`, Argon2id hashing/verification,
  AES-GCM token sealing, the SQLite-backed store, the throttle, and a `Service` exposing
  register/login/logout/change-password/delete-account. Owns its error codes. Imports
  `internal/db` and `internal/config` only.
- **`internal/provider`** — introduce a `Resolver` that yields a `*Registry` for a given
  user. Standalone supplies a static resolver wrapping today's env-built registry; hosted
  supplies one that builds a registry from `user_providers`, cached per user and
  invalidated on write. `review.Service` takes the resolver.
- **`internal/session`** — add `Owner` to the model, builder, and record; scope `List`,
  `Get`, `Finish`, and delete by owner when a scope is supplied.
- **`internal/mirror`** — accept a namespace segment so the hosted cache root is
  per-user; keep prune's live-branch protection correct within a namespace.
- **`internal/review`** — thread the caller's identity through the resolve pipeline so the
  right registry, mirror namespace, and session owner are used.
- **`internal/api`** — auth middleware, Origin check, the `/api/auth/*` and
  `/api/settings/providers` handlers, `401`/`403` mapping, and mode-conditional route
  registration.
- **`internal/app`** — open the database and run migrations in hosted mode; construct the
  auth service, resolver, and throttle; close the DB in `Close`; extend the sweeper to
  cover expired login sessions and lockouts.
- **`cmd/converge`** — no structural change beyond wiring.
- **`cmd/converge-cli`** — remains standalone-only; it does not gain accounts.

Frontend (`apps/frontend`):

- `src/services/api` — auth and provider-settings service objects; `credentials: "include"`
  on the fetch wrapper; a `401` interceptor.
- `src/lib/hooks/api` — React Query hooks for mode, current user, and provider settings,
  with cache invalidation on mutation.
- `src/pages` — `LoginPage`, `RegisterPage`, `AccountSettingsPage`, `ProviderSettingsPage`.
- `src/components/layout` — auth-aware header/menu and a route guard.
- `src/types/api`, `src/lib/schemas` — resource types and Zod schemas for the new forms.

Repository root:

- `Makefile`, `docker-compose` examples, and `README`/deployment docs document both modes
  and the new variables.

## 8. Non-Functional Requirements

**Security**

- Argon2id at the stated parameters; no fallback to a weaker hash.
- Provider tokens encrypted at rest; plaintext exists only in memory for the duration of a
  git or provider API call.
- No password, token, cookie value, or master key is ever logged; the master key is
  registered with the redacting handler alongside provider tokens.
- Login responses are timing-equalised between unknown-user and wrong-password (FR-2.5).
  Registration necessarily discloses username availability; that is accepted and documented.
- Cross-user access returns `404`, not `403`, to avoid confirming that a resource exists.
- Session tokens are compared by hash lookup, not by scanning and comparing plaintext.
- All SQL is parameterised. Client-supplied strings continue to be validated before they
  reach git, and git is still invoked via `gitx.Runner` with an argument slice.

**Performance**

- Authentication adds at most one indexed primary-key lookup per request; `last_seen_at`
  writes are rate-limited to once per five minutes per session.
- Argon2id at 64 MiB is deliberately expensive and runs only on login, registration, and
  password change. Concurrent login attempts are bounded by the throttle, capping the
  memory a burst can consume.
- The per-user provider registry is cached in memory and invalidated on write, so a review
  request does not hit the database per provider call.

**Observability**

- Structured `slog` events for registration, login success, login failure, lockout
  engaged, logout, password change, account deletion, and provider config create/update/
  delete — each with `user_id` (never the username's password or any token).
- One startup log line stating the mode, and in hosted mode the database path, the number
  of users, and the count of unowned review sessions.
- `/healthz` reports database reachability in hosted mode.

**Compatibility**

- A deployment that upgrades and sets no new variables must behave identically to the
  previous release: same endpoints, same responses, same on-disk layout, existing review
  sessions still listed and resumable.

**Testing**

- Unit tests for hashing, sealing/opening tokens, the throttle's doubling schedule, the
  migration runner, mode parsing, and owner scoping.
- API tests covering the full auth lifecycle, the standalone-mode `404`s, the `401`/`403`
  paths, the cross-user `404`, and the token-never-returned guarantee.
- Integration tests (build tag `integration`) covering two users with separate providers
  building reviews of the same repository against separate mirrors.
- Frontend Vitest coverage for the route guard, the `401` redirect, and both settings forms.

## 9. Open Questions

1. **No password reset.** With no admin role and no mailer, a forgotten password is
   unrecoverable. Is a documented operator recovery path (a `converge-cli` subcommand that
   resets a password against the database file) acceptable to add, or does that belong in
   a later task?
2. **No way to remove another user.** Self-service deletion is the only removal path, so
   an abandoned account persists forever. Same question: a `converge-cli` escape hatch, or
   a later admin task?
3. **Open registration on a reachable instance.** An invite code was considered and
   deferred. Is an operator expected to put the instance behind a VPN or reverse-proxy
   auth, and should the docs say so explicitly?
4. **Master key rotation.** Re-encrypting every stored token under a new key needs a
   procedure. Document-only for this task, or ship a rotation command?
5. **Mirror disk growth.** Per-user mirrors multiply disk use by the number of users
   reviewing the same repository. Is a per-user mirror cap or an LRU eviction needed now,
   or is the existing prune sufficient?
6. **`converge-cli` in hosted deployments.** The CLI reads the environment directly and has
   no notion of users. Confirm it stays standalone-only rather than gaining a
   `--user` flag.

## 10. Acceptance Criteria

Mode and backwards compatibility:

- [ ] With no new environment variables set, the server starts in standalone mode, creates
      no database file, and every existing API test passes unchanged.
- [ ] A review session created by the previous release is still listed and resumable after
      upgrading to this build in standalone mode.
- [ ] `CONVERGE_MODE=hosted` without `CONVERGE_SECRET_KEY` fails to start with an error
      naming the variable.
- [ ] `CONVERGE_MODE=nonsense` fails to start with an error naming `CONVERGE_MODE`.
- [ ] `CONVERGE_MODE=hosted` with `PROVIDERS__*` set logs one warning and ignores them.
- [ ] In standalone mode, `POST /api/auth/login` and `GET /api/settings/providers` both
      return `404`.
- [ ] `GET /api/auth/mode` returns the correct mode, unauthenticated, in both modes.

Accounts:

- [ ] Registering, logging out, and logging back in with the same credentials succeeds.
- [ ] Registering a username differing only in case returns `USERNAME_TAKEN`.
- [ ] A 7-character password is rejected with `WEAK_PASSWORD`; an 8-character one is
      accepted.
- [ ] Login with a wrong password and login with an unknown username return identical
      status, code, and message.
- [ ] Changing the password invalidates other login sessions but not the calling one; the
      old password no longer works.
- [ ] Deleting an account removes its DB rows, its review sessions and workspaces, and its
      mirror namespace; its cookie no longer authenticates.
- [ ] Six consecutive failed logins for one username return `429` with `Retry-After`, and
      the lockout survives a process restart.

Sessions and CSRF:

- [ ] The session cookie is `HttpOnly` and `SameSite=Lax`, and is `Secure` when
      `CONVERGE_SECURE_COOKIES=true`.
- [ ] No log line in any test run contains a session token, a password, a provider token,
      or the master key.
- [ ] A `POST /api/reviews` with a foreign `Origin` header is rejected `403`/`FORBIDDEN`.
- [ ] A login session past its absolute or idle expiry is rejected even before the sweeper
      runs, and is removed by the sweeper.

Providers:

- [ ] A user can create, list, edit, and delete their own provider configurations.
- [ ] No API response anywhere contains a stored token value; only `tokenLast4` appears.
- [ ] Editing a provider with an empty token field leaves the stored token working.
- [ ] A second user's `GET /api/providers` does not include the first user's providers.
- [ ] Creating a provider with an invalid token and `validate=true` returns
      `PROVIDER_UNAUTHORIZED` and writes nothing.
- [ ] Deleting a provider referenced by an active review returns `PROVIDER_IN_USE`.
- [ ] A token ciphertext copied into another user's row fails to decrypt.

Isolation:

- [ ] User A's `GET /api/reviews` never includes user B's reviews.
- [ ] `GET /api/reviews/{id}` and `DELETE /api/reviews/{id}` for another user's session
      return `404`.
- [ ] Two users reviewing the same repository use two mirror directories under
      `REPOSITORY_CACHE_ROOT/users/<user-id>/`.
- [ ] Mirror prune in hosted mode does not delete a branch belonging to a live review.

UI:

- [ ] In standalone mode the UI renders exactly as before, with no account menu and no
      reachable settings routes.
- [ ] In hosted mode an unauthenticated visit to `/` redirects to `/login` and returns to
      `/` after a successful login.
- [ ] A `401` from any API call redirects to `/login`.
- [ ] The provider settings form creates, edits, and deletes providers, and never renders
      a stored token.

Build gates:

- [ ] `make lint`, `make test`, `make test-integration`, `make build`, and
      `make docker-build` are all clean.
- [ ] `CGO_ENABLED=0 go build ./...` succeeds with the SQLite driver linked in.
