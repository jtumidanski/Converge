# Hosted Multi-User Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an opt-in `hosted` mode in which several reviewers share one Converge process with their own accounts, their own provider tokens, and their own reviews, while the default `standalone` mode keeps behaving byte-for-byte as it does today.

**Architecture:** A leaf `internal/identity` package carries an `identity.Scope` ("on whose behalf") as an explicit function argument from `api` down through `review` into `session.Store` and `mirror.Cache`. Providers stop being process-global: `review`/`api` depend on a new `provider.Resolver` interface, satisfied by a static wrapper in standalone and by a DB-backed cached resolver in `internal/auth` for hosted. A new leaf `internal/db` package owns a `modernc.org/sqlite` connection holding **only** users, login sessions, per-user provider configuration, and lockout counters; review sessions stay `session.json` files on disk.

**Tech Stack:** Go 1.26 (`log/slog`, `net/http` `ServeMux`, `database/sql`), `modernc.org/sqlite` (pure-Go, CGO-free), `golang.org/x/crypto/argon2`, AES-256-GCM from the standard library; React 19 + TypeScript + Vite + TanStack React Query + react-hook-form + Zod + Tailwind/shadcn on the frontend; Vitest for frontend tests.

**Spec:** `docs/tasks/task-005-hosted-multi-user-mode/design.md` (requirements: `prd.md`; wire formats: `api-contracts.md`). All three travel with this plan — the plan argues from the design and cites FR numbers from the PRD.

## Global Constraints

These apply to **every** task. A task's requirements implicitly include this section.

- **Package dependency direction is law:** `api → review → {provider, mirror, workspace, diff, session} → gitx`. `internal/identity` and `internal/db` are leaves (stdlib only). `internal/auth` sits in the middle tier and may import `internal/db`, `internal/config`, `internal/identity`, `internal/provider`, `internal/provider/github`, `internal/provider/gitlab` — **and nothing else from the module**. `auth` must never import `review` or `session`; `review` must never import `auth`. Nothing imports `api` or `cmd`.
- **Standalone equivalence is requirement #1.** With no new environment variables set, the server must start in standalone mode, create no database file, and pass every pre-existing test **unchanged**. Do not add new assertions to pre-existing test functions while changing the code under them (design §12) — put new assertions in new test functions so a pass of the old suite means what it appears to mean.
- **Every git call goes through `gitx.Runner` with an argument slice, never a shell.** Provider credentials reach git only through `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_0`/`GIT_CONFIG_VALUE_0` (`provider.GitProvider.AuthorizeGit`), never argv and never a stored remote URL.
- **All SQL is parameterised.** No SQL is built by string concatenation of any value. `internal/auth/store.go` is the only file in the repository that contains SQL.
- **Secrets:** `CGO_ENABLED=0 go build ./...` must succeed, so `modernc.org/sqlite` is mandatory and `mattn/go-sqlite3` is forbidden. Passwords are Argon2id: time=1, memory=65536 KiB (64 MiB), parallelism=4, 16-byte salt, 32-byte key, PHC-encoded, with verification reading parameters from the stored string (FR-2.3). Provider tokens are AES-256-GCM with a per-record nonce and AAD binding ciphertext to `user_id` + provider row id (FR-5.3). Session cookie is named `converge_session`, holds a base64url-encoded 32 random bytes, and only its SHA-256 is persisted (FR-3.1, FR-3.3). No password, token, cookie value, or master key is ever logged.
- **Linters actually enabled** (`apps/backend/.golangci.yml`): `errcheck`, `govet`, `staticcheck`, `unused`, `ineffassign`, `errorlint`, `gosec`, `misspell`, `revive`, `unconvert`, plus `gofmt`/`goimports`. Consequences: wrap errors with `%w` (errorlint), handle or explicitly `_ =` every returned error including `rows.Close()` (errcheck), avoid `math/rand` (gosec G404), and run `gofmt`/`goimports` before committing. `_test.go` files are exempt from `gosec` and `errcheck`.
- **Test commands.** Backend (cwd `apps/backend`): `go test -race -count=1 ./...`, `go test -race -count=1 -tags integration ./...`, `go vet ./...`, `go tool golangci-lint run`, `CGO_ENABLED=0 go build ./...`. Frontend (cwd `apps/frontend`): `npm run lint`, `npm run format:check`, `npm test`, `npm run build`. If `npm` is missing: `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22`.
- **Verification over memory.** For the `modernc.org/sqlite` DSN pragma syntax, the GitHub/GitLab API shapes, and the Argon2id PHC encoding, verify against the dependency's own source/docs rather than from memory, and back each with a test that observes the effect (e.g. `SELECT * FROM pragma_journal_mode`).
- **No absolute home-directory paths** in any file this plan creates or modifies.
- **New error codes** and their HTTP statuses, verbatim from PRD §5.3: `UNAUTHENTICATED` 401, `INVALID_CREDENTIALS` 401, `ACCOUNT_LOCKED` 429, `USERNAME_TAKEN` 409, `INVALID_USERNAME` 422, `WEAK_PASSWORD` 422, `FORBIDDEN` 403, `PROVIDER_SLUG_TAKEN` 409, `PROVIDER_UNAUTHORIZED` 422, `PROVIDER_IN_USE` 409. Existing codes and mappings are untouched.
- **New environment variables:** `CONVERGE_MODE` (`standalone` default | `hosted`), `CONVERGE_DATABASE_PATH` (default `/data/converge.db`), `CONVERGE_SECRET_KEY` (32 random bytes, base64 standard encoding), `CONVERGE_SECURE_COOKIES` (bool, default false), `CONVERGE_TRUSTED_PROXY` (bool, default false), `LOGIN_SESSION_TTL_HOURS` (default 720), `LOGIN_SESSION_IDLE_HOURS` (default 168).
- **Commit after every task.** Use Conventional Commit subjects. End every commit message with:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  ```

---

## File Structure

### New backend packages

| Path | Responsibility |
| --- | --- |
| `internal/identity/scope.go` | `Scope` value type; `Standalone()`, `ForUser()`, `UserID()`, `IsScoped()`, `Matches()`. Stdlib-only leaf. This is where FR-6.3/FR-6.4 live, in one function. |
| `internal/db/db.go` | `Open` (DSN pragmas, `MaxOpenConns(1)`) and `Migrate` (embedded, forward-only). Stdlib + driver only. |
| `internal/db/migrations/0001_init.sql` | The schema from PRD §6.2. |
| `internal/auth/errors.go` | `auth.Code`, `*auth.Error{Code, Message, RetryAfter}`, `Status()`, `ErrNotFound`. |
| `internal/auth/model.go` | `User`, `LoginSession`, `UserProvider` immutable values + constructors; username/slug regexps; `newID`. |
| `internal/auth/password.go` | Argon2id hash/verify, PHC encode/decode, `Params` in one place, `ValidatePassword`. |
| `internal/auth/crypt.go` | `Sealer`: AES-256-GCM seal/open with AAD = `user_id \| provider_id`. |
| `internal/auth/store.go` | All SQL. The only file in the repo that writes SQL. |
| `internal/auth/throttle.go` | `Attempt`, `Throttle` — failure counters and the doubling lockout schedule. |
| `internal/auth/verify.go` | `ProviderVerifier` + `HTTPVerifier` — checks a credential against the provider API (FR-5.6). |
| `internal/auth/service.go` | `Service`: register/login/logout/authenticate/change-password/delete-account, the Argon2id semaphore, `Purger` and `ProviderUsage` collaborator interfaces. |
| `internal/auth/providers.go` | `Service` provider-config CRUD (FR-5.1–FR-5.7). |
| `internal/auth/resolver.go` | `ProviderResolver` — builds a `*provider.Registry` per user, cached with a TTL, `Invalidate`. |
| `internal/auth/sweep.go` | `Sweep(ctx)` — expired login sessions and elapsed lockouts (FR-3.7). |
| `internal/provider/resolver.go` | `provider.Resolver` interface + `NewStaticResolver`. |
| `internal/api/authctx.go` | `scopeFrom`/`tokenHashFrom` typed accessors, cookie helpers, `clientIP`. |
| `internal/api/authmw.go` | `originGuard` and `authenticate` middleware. |
| `internal/api/auth.go` | `/api/auth/*` handlers. |
| `internal/api/settings_providers.go` | `/api/settings/providers` handlers. |

### Modified backend files

| Path | Change |
| --- | --- |
| `internal/config/config.go` | `Mode`, hosted variables, `loadProviders(env, required)`, `IgnoredProviderVars`, `Secrets()` gains the master key. |
| `internal/session/model.go`, `builder.go`, `record.go` | `owner` field, `Owner()`, `SetOwner`, `Record.Owner`. |
| `internal/session/store.go` | `Get`/`List`/`Finish` take `identity.Scope`; new `Purge`, `Unowned`. |
| `internal/mirror/cache.go` | `Namespace`; `Path`/`Ensure`/`FetchSHA` take it; new `PurgeNamespace`. |
| `internal/review/service.go`, `resolve.go`, `cleaner.go` | `Deps.Providers` becomes `provider.Resolver`; scope threading; `PurgeUser`, `ProviderInUse`. |
| `internal/api/router.go` | `Deps` gains mode/auth fields; mode-conditional route registration; auth sweeper goroutine. |
| `internal/api/errors.go` | `classify` gains one `errors.As` arm for `*auth.Error` and one `errors.Is` arm for `auth.ErrNotFound`. |
| `internal/api/reviews.go`, `review_files.go`, `providers.go`, `repositories.go`, `health.go` | Scope threading; resolver instead of registry; DB reachability in `/healthz`. |
| `internal/app/app.go` | Hosted wiring: open DB, migrate, build auth service/resolver/throttle/sealer, startup logs, `Close` the DB. |
| `cmd/converge/main.go` | `buildDeps` passes the new `api.Deps` fields. |

### Frontend

| Path | Responsibility |
| --- | --- |
| `src/lib/api/client.ts` *(modify)* | `credentials: "include"`, `apiPatch`, module-level `setUnauthorizedHandler`. |
| `src/types/models/auth.ts` | `AuthMode`, `CurrentUser`, `UserProvider` model types. |
| `src/lib/schemas/auth.ts` | Zod schemas for login, register, password change, account deletion. |
| `src/lib/schemas/userProvider.ts` | Zod schemas for provider create/edit. |
| `src/services/api/auth.ts` | `authService` plain object. |
| `src/services/api/userProviders.ts` | `userProvidersService` plain object. |
| `src/lib/hooks/api/useAuth.ts` | `useAuthMode`, `useCurrentUser`, login/register/logout/password/delete mutations. |
| `src/lib/hooks/api/useUserProviders.ts` | List + create/update/delete mutations, invalidating `["providers"]`. |
| `src/components/auth/AuthProvider.tsx` | Registers the 401 handler, exposes nothing else. |
| `src/components/auth/RequireAuth.tsx` | Route guard. |
| `src/components/auth/ModeGate.tsx` | Branches the whole tree on `useAuthMode()`. |
| `src/components/layout/AccountMenu.tsx` | Username + menu (hosted only). |
| `src/components/layout/AppShell.tsx` *(modify)* | Optional `right` slot for the account menu. |
| `src/App.tsx`, `src/routes.tsx` *(modify)* | Mount `ModeGate`; hosted-only routes. |
| `src/pages/LoginPage.tsx`, `RegisterPage.tsx`, `ProviderSettingsPage.tsx`, `AccountSettingsPage.tsx` | The four new screens. |

### Docs / ops

`README.md`, `.env.example`, `docker-compose.yml`, and `docs/tasks/task-005-hosted-multi-user-mode/context.md`.

---

## Task 1: `internal/identity` — the Scope value

**Files:**
- Create: `apps/backend/internal/identity/scope.go`
- Test: `apps/backend/internal/identity/scope_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `identity.Scope` (value type, unexported `userID` field), `identity.Standalone() Scope`, `identity.ForUser(id string) Scope`, `(Scope) UserID() string`, `(Scope) IsScoped() bool`, `(Scope) Matches(owner string) bool`. Every later task uses these exact names.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/identity/scope_test.go`:

```go
package identity_test

import (
	"testing"

	"github.com/jtumidanski/converge/internal/identity"
)

// TestMatchesTruthTable pins the asymmetry in design §3: a standalone scope
// sees everything, including owned records (so an existing deployment that
// never sets CONVERGE_MODE keeps listing every session); a hosted scope sees
// only an exact owner match, so an unowned record is invisible to everyone
// (FR-6.3, FR-6.4).
func TestMatchesTruthTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		scope identity.Scope
		owner string
		want  bool
	}{
		{"standalone sees unowned", identity.Standalone(), "", true},
		{"standalone sees owned", identity.Standalone(), "abc123", true},
		{"hosted sees own", identity.ForUser("abc123"), "abc123", true},
		{"hosted cannot see foreign", identity.ForUser("abc123"), "def456", false},
		{"hosted cannot see unowned", identity.ForUser("abc123"), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.scope.Matches(tc.owner); got != tc.want {
				t.Fatalf("Matches(%q) = %v, want %v", tc.owner, got, tc.want)
			}
		})
	}
}

func TestScopeAccessors(t *testing.T) {
	t.Parallel()
	if s := identity.Standalone(); s.IsScoped() || s.UserID() != "" {
		t.Fatalf("standalone scope: IsScoped=%v UserID=%q, want false and empty", s.IsScoped(), s.UserID())
	}
	if s := identity.ForUser("abc123"); !s.IsScoped() || s.UserID() != "abc123" {
		t.Fatalf("hosted scope: IsScoped=%v UserID=%q, want true and abc123", s.IsScoped(), s.UserID())
	}
}

// TestZeroValueIsStandalone proves the safe default: a Scope built by a struct
// literal (the zero value, which is all an outside package can construct
// because userID is unexported) behaves as standalone, not as "some user".
func TestZeroValueIsStandalone(t *testing.T) {
	t.Parallel()
	var s identity.Scope
	if s.IsScoped() {
		t.Fatal("zero Scope reported as scoped")
	}
	if !s.Matches("anything") {
		t.Fatal("zero Scope did not match an owned record")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd apps/backend && go test ./internal/identity/...
```

Expected: FAIL — the package does not exist (`no Go files in .../internal/identity`).

- [ ] **Step 3: Write the implementation**

Create `apps/backend/internal/identity/scope.go`:

```go
// Package identity answers "on whose behalf" for a request. It is a leaf: it
// imports nothing, so every tier from api down to mirror can depend on it
// without inverting the module's dependency direction.
package identity

// Scope names the user a request acts for. The zero value is the standalone
// scope: unowned, unfiltered, and identical to pre-hosted behaviour.
//
// The field is unexported deliberately. A Scope cannot be forged by a struct
// literal outside this package, so the only ways to obtain one are Standalone
// (safe) and ForUser (constructed by the auth middleware from a verified
// login session). A zero value is therefore a safe default rather than a
// dangerous one.
type Scope struct {
	userID string
}

// Standalone returns the unfiltered scope used in standalone mode, by
// converge-cli, and by the system acting on its own behalf (sweepers).
func Standalone() Scope { return Scope{} }

// ForUser returns the scope for the authenticated user id.
func ForUser(id string) Scope { return Scope{userID: id} }

// UserID returns the user id, empty in standalone mode.
func (s Scope) UserID() string { return s.userID }

// IsScoped reports whether this scope names a user.
func (s Scope) IsScoped() bool { return s.userID != "" }

// Matches reports whether a record owned by owner is visible to s.
//
// Standalone (s.userID == "") sees everything, including records that carry an
// owner: an instance downgraded from hosted to standalone must still list its
// sessions (FR-6.4). A scoped s sees only an exact match, which makes an
// unowned record invisible to every user in hosted mode (FR-6.3).
func (s Scope) Matches(owner string) bool {
	if s.userID == "" {
		return true
	}
	return owner == s.userID
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/identity/... && go vet ./internal/identity/...
```

Expected: PASS (3 test functions, 5 subtests).

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/identity
git commit -m "$(cat <<'EOF'
feat(identity): add the Scope value that carries request ownership

Scope is the single place the hosted/standalone visibility rule lives
(FR-6.3, FR-6.4). The unexported field makes the zero value the safe
standalone default and prevents a forged scope outside the package.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `config` — mode selection and the hosted variables

**Files:**
- Modify: `apps/backend/internal/config/config.go`
- Test: `apps/backend/internal/config/config_test.go` (add new test functions; do not edit existing ones)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `config.Mode` (string type) with `config.ModeStandalone Mode = "standalone"` and `config.ModeHosted Mode = "hosted"`; `Config` fields `Mode Mode`, `DatabasePath string`, `SecretKey Secret`, `SecureCookies bool`, `TrustedProxy bool`, `LoginSessionTTL time.Duration`, `LoginIdleTTL time.Duration`, `IgnoredProviderVars []string`; `loadProviders(env []string, required bool) ([]ProviderConfig, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `apps/backend/internal/config/config_test.go` (keep the existing `package` clause and imports; add `"strings"` if absent):

```go
// baseEnv is the minimum standalone environment: one provider, which
// loadProviders requires when the mode is standalone.
func baseEnv() []string {
	return []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=t",
	}
}

func TestModeDefaultsToStandalone(t *testing.T) {
	for _, env := range [][]string{baseEnv(), append(baseEnv(), "CONVERGE_MODE=")} {
		cfg, err := config.Load(env)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Mode != config.ModeStandalone {
			t.Fatalf("Mode = %q, want %q", cfg.Mode, config.ModeStandalone)
		}
		if !cfg.SecretKey.IsZero() {
			t.Fatal("standalone mode read CONVERGE_SECRET_KEY")
		}
		if cfg.DatabasePath != "" {
			t.Fatalf("standalone DatabasePath = %q, want empty", cfg.DatabasePath)
		}
	}
}

func TestModeRejectsUnknownValue(t *testing.T) {
	_, err := config.Load(append(baseEnv(), "CONVERGE_MODE=nonsense"))
	var ce *config.Error
	if !errors.As(err, &ce) || ce.Variable != "CONVERGE_MODE" {
		t.Fatalf("Load error = %v, want a config.Error naming CONVERGE_MODE", err)
	}
}

func TestStandaloneStillRequiresAProvider(t *testing.T) {
	_, err := config.Load(nil)
	var ce *config.Error
	if !errors.As(err, &ce) || ce.Variable != "PROVIDERS__*" {
		t.Fatalf("Load error = %v, want a config.Error naming PROVIDERS__*", err)
	}
}

// testKey is 32 bytes of 0x01, base64 standard encoded. Not a credential:
// it exists only so Load has something well-formed to accept.
const testKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

func TestHostedIgnoresProvidersAndRecordsTheirNames(t *testing.T) {
	cfg, err := config.Load([]string{
		"CONVERGE_MODE=hosted",
		"CONVERGE_SECRET_KEY=" + testKey,
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=t",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("hosted Providers = %v, want none", cfg.Providers)
	}
	want := []string{"PROVIDERS__GH__TOKEN", "PROVIDERS__GH__TYPE"}
	if strings.Join(cfg.IgnoredProviderVars, ",") != strings.Join(want, ",") {
		t.Fatalf("IgnoredProviderVars = %v, want %v", cfg.IgnoredProviderVars, want)
	}
}

func TestHostedWithNoProvidersIsFine(t *testing.T) {
	cfg, err := config.Load([]string{"CONVERGE_MODE=hosted", "CONVERGE_SECRET_KEY=" + testKey})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabasePath != "/data/converge.db" {
		t.Fatalf("DatabasePath = %q, want /data/converge.db", cfg.DatabasePath)
	}
	if cfg.LoginSessionTTL != 720*time.Hour {
		t.Fatalf("LoginSessionTTL = %v, want 720h", cfg.LoginSessionTTL)
	}
	if cfg.LoginIdleTTL != 168*time.Hour {
		t.Fatalf("LoginIdleTTL = %v, want 168h", cfg.LoginIdleTTL)
	}
	if cfg.SecureCookies || cfg.TrustedProxy {
		t.Fatal("SecureCookies/TrustedProxy should default to false")
	}
}

func TestHostedRequiresAWellFormedSecretKey(t *testing.T) {
	cases := []struct{ name, value string }{
		{"missing", ""},
		{"not base64", "!!!!not-base64!!!!"},
		{"wrong length", "AQEB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := []string{"CONVERGE_MODE=hosted"}
			if tc.value != "" {
				env = append(env, "CONVERGE_SECRET_KEY="+tc.value)
			}
			_, err := config.Load(env)
			var ce *config.Error
			if !errors.As(err, &ce) || ce.Variable != "CONVERGE_SECRET_KEY" {
				t.Fatalf("Load error = %v, want a config.Error naming CONVERGE_SECRET_KEY", err)
			}
		})
	}
}

// TestSecretsIncludeMasterKey proves the base64 key joins the redaction list,
// so a key that leaks into an error string is scrubbed from every log line.
func TestSecretsIncludeMasterKey(t *testing.T) {
	cfg, err := config.Load([]string{"CONVERGE_MODE=hosted", "CONVERGE_SECRET_KEY=" + testKey})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := false
	for _, s := range cfg.Secrets() {
		if s == testKey {
			found = true
		}
	}
	if !found {
		t.Fatalf("Secrets() = %v, want it to contain the master key", cfg.Secrets())
	}
}

func TestBoolVars(t *testing.T) {
	cfg, err := config.Load([]string{
		"CONVERGE_MODE=hosted", "CONVERGE_SECRET_KEY=" + testKey,
		"CONVERGE_SECURE_COOKIES=true", "CONVERGE_TRUSTED_PROXY=TRUE",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.SecureCookies || !cfg.TrustedProxy {
		t.Fatalf("SecureCookies=%v TrustedProxy=%v, want both true", cfg.SecureCookies, cfg.TrustedProxy)
	}
	if _, err := config.Load([]string{"CONVERGE_MODE=hosted", "CONVERGE_SECRET_KEY=" + testKey, "CONVERGE_SECURE_COOKIES=yes"}); err == nil {
		t.Fatal("CONVERGE_SECURE_COOKIES=yes should be rejected")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd apps/backend && go test ./internal/config/...
```

Expected: FAIL to compile — `cfg.Mode`, `config.ModeStandalone`, `cfg.IgnoredProviderVars`, etc. are undefined.

- [ ] **Step 3: Implement the config changes**

In `apps/backend/internal/config/config.go`, add the `Mode` type next to the existing `Kind` type:

```go
// Mode selects the shape of the process. The two modes are mutually
// exclusive and fixed for the process lifetime (FR-1.6).
type Mode string

const (
	// ModeStandalone is today's single-tenant shape: no accounts, no
	// database, providers from PROVIDERS__*. It is the default so an existing
	// deployment that upgrades and changes nothing is unaffected (FR-1.1).
	ModeStandalone Mode = "standalone"
	// ModeHosted adds accounts, per-user provider configuration in SQLite,
	// and ownership on review sessions.
	ModeHosted Mode = "hosted"
)

const defaultDatabasePath = "/data/converge.db"

// secretKeyLen is the AES-256 key length CONVERGE_SECRET_KEY must decode to.
const secretKeyLen = 32
```

Extend the `Config` struct with the new fields (keep the existing ones in place):

```go
	// Mode is parsed before anything else in Load: it decides whether
	// PROVIDERS__* is required and whether CONVERGE_SECRET_KEY is read.
	Mode Mode
	// DatabasePath, SecretKey, SecureCookies, TrustedProxy,
	// LoginSessionTTL and LoginIdleTTL are populated only in hosted mode.
	// In standalone mode they stay zero and no database file is ever
	// created or opened (FR-1.5).
	DatabasePath    string
	SecretKey       Secret
	SecureCookies   bool
	TrustedProxy    bool
	LoginSessionTTL time.Duration
	LoginIdleTTL    time.Duration
	// IgnoredProviderVars holds the names (never the values) of PROVIDERS__*
	// variables present in hosted mode, so the caller can emit the single
	// startup WARN that FR-1.3 requires.
	IgnoredProviderVars []string
```

Extend `Secrets()`:

```go
func (c Config) Secrets() []string {
	out := make([]string, 0, len(c.Providers)+1)
	for _, p := range c.Providers {
		if !p.Token.IsZero() {
			out = append(out, p.Token.Reveal())
		}
	}
	// The master key is registered alongside provider tokens so the
	// redacting handler scrubs it too: a base64 key that ends up inside an
	// error string is exactly the accident this list exists for.
	if !c.SecretKey.IsZero() {
		out = append(out, c.SecretKey.Reveal())
	}
	return out
}
```

> **Note for the implementer:** read the existing `Secrets()` body first and preserve whatever it already does for provider tokens; only the master-key append is new.

In `Load`, parse the mode **first** (immediately after `cfg := Config{LogFormat: "text"}` and the `var err error` line), and replace the `loadProviders` call:

```go
	if cfg.Mode, err = modeVar(vars); err != nil {
		return Config{}, err
	}
	// ... existing APP_PORT .. PROVIDER_TIMEOUT_SECONDS parsing is unchanged ...

	if cfg.Mode == ModeHosted {
		if err = loadHosted(vars, &cfg); err != nil {
			return Config{}, err
		}
	}
	if cfg.Providers, cfg.IgnoredProviderVars, err = loadProviders(env, cfg.Mode == ModeStandalone); err != nil {
		return Config{}, err
	}
	return cfg, nil
```

Add the new helpers at the bottom of the file:

```go
func modeVar(vars map[string]string) (Mode, error) {
	switch Mode(strings.ToLower(stringVar(vars, "CONVERGE_MODE", string(ModeStandalone)))) {
	case ModeStandalone:
		return ModeStandalone, nil
	case ModeHosted:
		return ModeHosted, nil
	default:
		return "", &Error{Variable: "CONVERGE_MODE", Reason: "must be standalone or hosted"}
	}
}

func boolVar(vars map[string]string, key string) (bool, error) {
	raw := strings.ToLower(stringVar(vars, key, "false"))
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, &Error{Variable: key, Reason: "must be true or false"}
	}
}

// loadHosted fills the hosted-only fields. It is never called in standalone
// mode, which is what keeps CONVERGE_SECRET_KEY unread and DatabasePath empty
// there (FR-1.5).
func loadHosted(vars map[string]string, cfg *Config) error {
	cfg.DatabasePath = stringVar(vars, "CONVERGE_DATABASE_PATH", defaultDatabasePath)

	raw := stringVar(vars, "CONVERGE_SECRET_KEY", "")
	if raw == "" {
		return &Error{Variable: "CONVERGE_SECRET_KEY", Reason: "is required in hosted mode"}
	}
	// Validated at parse time, not at first encrypt: an operator seeing a
	// clear startup error beats a user seeing a 500.
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return &Error{Variable: "CONVERGE_SECRET_KEY", Reason: "must be base64 standard encoding"}
	}
	if len(key) != secretKeyLen {
		return &Error{Variable: "CONVERGE_SECRET_KEY", Reason: "must decode to exactly 32 bytes"}
	}
	cfg.SecretKey = NewSecret(raw)

	if cfg.SecureCookies, err = boolVar(vars, "CONVERGE_SECURE_COOKIES"); err != nil {
		return err
	}
	if cfg.TrustedProxy, err = boolVar(vars, "CONVERGE_TRUSTED_PROXY"); err != nil {
		return err
	}
	if cfg.LoginSessionTTL, err = durationVar(vars, "LOGIN_SESSION_TTL_HOURS", 720, time.Hour); err != nil {
		return err
	}
	if cfg.LoginIdleTTL, err = durationVar(vars, "LOGIN_SESSION_IDLE_HOURS", 168, time.Hour); err != nil {
		return err
	}
	return nil
}
```

Change `loadProviders` to take `required bool` and also return the ignored names. The parsing body is identical — only the "at least one" check becomes conditional, and hosted mode collects names instead of building configs:

```go
// loadProviders parses PROVIDERS__*. required is true only in standalone mode
// (FR-1.2); in hosted mode the variables are ignored entirely and their names
// are returned so the caller can warn once (FR-1.3). Names only — never
// values, which are tokens.
func loadProviders(env []string, required bool) ([]ProviderConfig, []string, error) {
	if !required {
		var ignored []string
		for _, kv := range env {
			if k, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, providerPrefix) {
				ignored = append(ignored, k)
			}
		}
		sort.Strings(ignored)
		return nil, ignored, nil
	}
	// ... the existing body, unchanged, returning (out, nil, nil) and
	// (nil, nil, err) instead of (out, nil) and (nil, err) ...
}
```

Add `"encoding/base64"` to the import block.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/config/... && go vet ./internal/config/...
```

Expected: PASS, including every pre-existing config test.

- [ ] **Step 5: Fix the one other caller and build**

`loadProviders` is package-private, so only `Load` calls it. Confirm and build:

```bash
cd apps/backend && grep -rn 'loadProviders(' internal/ && CGO_ENABLED=0 go build ./...
```

Expected: one call site (in `Load`); build succeeds.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/config
git commit -m "$(cat <<'EOF'
feat(config): add CONVERGE_MODE and the hosted-mode variables

Mode is parsed first because it decides whether PROVIDERS__* is required
(FR-1.2) and whether CONVERGE_SECRET_KEY is read at all (FR-1.5). The
master key is validated at parse time and joins Secrets() so the redacting
handler scrubs it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `internal/db` — SQLite connection and migration runner

**Files:**
- Create: `apps/backend/internal/db/db.go`
- Create: `apps/backend/internal/db/migrations/0001_init.sql`
- Test: `apps/backend/internal/db/db_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (leaf package, stdlib + driver only).
- Produces: `db.Options{Path string}`, `db.Open(ctx context.Context, o Options) (*sql.DB, error)`, `db.Migrate(ctx context.Context, handle *sql.DB) error`, `db.ErrSchemaAhead error`.

- [ ] **Step 1: Add the dependency**

```bash
cd apps/backend && go get modernc.org/sqlite@latest && go mod tidy
```

Then verify the module still builds without CGO — this is the whole reason for this driver:

```bash
cd apps/backend && CGO_ENABLED=0 go build ./...
```

Expected: success. If it fails, stop: `mattn/go-sqlite3` is not an acceptable substitute.

- [ ] **Step 2: Read the driver's DSN documentation**

```bash
cd apps/backend && go doc modernc.org/sqlite | head -60
```

Confirm the query-parameter spelling for pragmas (expected: `_pragma=journal_mode(WAL)` style). **Do not guess** — Step 4's test asserts the pragmas actually took effect, and that test is the contract.

- [ ] **Step 3: Write the failing test**

Create `apps/backend/internal/db/db_test.go`:

```go
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
```

- [ ] **Step 4: Run the tests to verify they fail**

```bash
cd apps/backend && go test ./internal/db/...
```

Expected: FAIL — the package does not exist.

- [ ] **Step 5: Write the migration**

Create `apps/backend/internal/db/migrations/0001_init.sql`:

```sql
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
```

- [ ] **Step 6: Write the implementation**

Create `apps/backend/internal/db/db.go`:

```go
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
```

- [ ] **Step 7: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/db/... && go vet ./internal/db/... && CGO_ENABLED=0 go build ./...
```

Expected: PASS on all six test functions. If `journal_mode` is not `wal`, the DSN spelling is wrong — fix it against the driver's docs (Step 2), never by dropping the assertion.

- [ ] **Step 8: Check the size cost early (design §12 risk)**

```bash
cd apps/backend && CGO_ENABLED=0 go build -o /tmp/converge-size ./cmd/converge && ls -l /tmp/converge-size && rm /tmp/converge-size
```

Record the number in the commit body. It will grow noticeably; that is expected and accepted.

- [ ] **Step 9: Commit**

```bash
git add apps/backend/internal/db apps/backend/go.mod apps/backend/go.sum
git commit -m "$(cat <<'EOF'
feat(db): add the SQLite connection and embedded migration runner

modernc.org/sqlite is mandatory: CGO_ENABLED=0 go build ./... is a required
gate. Pragmas live in the DSN so a reconnect cannot miss them, and
MaxOpenConns(1) trades throughput for the removal of SQLITE_BUSY handling
from every call site (design §5). Migrations are forward-only; a recorded
version with no embedded file is fatal.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `auth` errors and models

**Files:**
- Create: `apps/backend/internal/auth/errors.go`
- Create: `apps/backend/internal/auth/model.go`
- Test: `apps/backend/internal/auth/model_test.go`

**Interfaces:**
- Consumes: `config.Kind`, `config.KindGitHub`, `config.KindGitLab`.
- Produces:
  - `auth.Code` (string) with constants `CodeUnauthenticated`, `CodeInvalidCredentials`, `CodeAccountLocked`, `CodeUsernameTaken`, `CodeInvalidUsername`, `CodeWeakPassword`, `CodeForbidden`, `CodeProviderSlugTaken`, `CodeProviderUnauthorized`, `CodeProviderInUse`.
  - `*auth.Error{Code Code; Message string; RetryAfter time.Duration}` with `Error() string` and `Status() int`.
  - `auth.ErrNotFound error`.
  - `auth.User` with `ID()`, `Username()`, `CreatedAt()`, `UpdatedAt()`, `PasswordHash()`; `auth.NewUser(id, username, passwordHash string, now time.Time) (User, error)`; `auth.Fold(username string) string`.
  - `auth.LoginSession` with `TokenHash() []byte`, `UserID()`, `CreatedAt()`, `ExpiresAt()`, `LastSeenAt()`.
  - `auth.UserProvider` with `ID()`, `UserID()`, `Slug()`, `DisplayName()`, `Kind() config.Kind`, `BaseURL()`, `TokenCiphertext()`, `TokenNonce()`, `TokenLast4()`, `TokenSetAt()`, `CreatedAt()`, `UpdatedAt()`.
  - `auth.ValidateUsername(string) error`, `auth.ValidateSlug(string) error`, `auth.NormalizeBaseURL(kind config.Kind, raw string) (string, error)`, `auth.NewID() (string, error)`, `auth.Last4(string) string`.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/auth/model_test.go`:

```go
package auth_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
)

func TestErrorStatusMapping(t *testing.T) {
	t.Parallel()
	cases := map[auth.Code]int{
		auth.CodeUnauthenticated:      http.StatusUnauthorized,
		auth.CodeInvalidCredentials:   http.StatusUnauthorized,
		auth.CodeAccountLocked:        http.StatusTooManyRequests,
		auth.CodeUsernameTaken:        http.StatusConflict,
		auth.CodeInvalidUsername:      http.StatusUnprocessableEntity,
		auth.CodeWeakPassword:         http.StatusUnprocessableEntity,
		auth.CodeForbidden:            http.StatusForbidden,
		auth.CodeProviderSlugTaken:    http.StatusConflict,
		auth.CodeProviderUnauthorized: http.StatusUnprocessableEntity,
		auth.CodeProviderInUse:        http.StatusConflict,
	}
	for code, want := range cases {
		e := &auth.Error{Code: code, Message: "m"}
		if got := e.Status(); got != want {
			t.Errorf("%s: Status() = %d, want %d", code, got, want)
		}
	}
}

func TestValidateUsername(t *testing.T) {
	t.Parallel()
	valid := []string{"abc", "Alice", "a_b-c.d", "a1234567890123456789012345678901"} // 3 and 32 chars
	for _, u := range valid {
		if err := auth.ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", u, err)
		}
	}
	invalid := []string{"", "ab", "_abc", ".abc", "-abc", "a b", "a/b", "a12345678901234567890123456789012"} // 2 and 33 chars
	for _, u := range invalid {
		var ae *auth.Error
		err := auth.ValidateUsername(u)
		if !errors.As(err, &ae) || ae.Code != auth.CodeInvalidUsername {
			t.Errorf("ValidateUsername(%q) = %v, want INVALID_USERNAME", u, err)
		}
	}
}

func TestFoldIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	if auth.Fold("Alice") != auth.Fold("ALICE") || auth.Fold("Alice") != "alice" {
		t.Fatalf("Fold is not lower-casing: %q vs %q", auth.Fold("Alice"), auth.Fold("ALICE"))
	}
}

func TestValidateSlug(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"a", "github", "gitlab-internal", "a1"} {
		if err := auth.ValidateSlug(s); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{"", "-a", "A", "a_b", "a b", "a/b"} {
		if auth.ValidateSlug(s) == nil {
			t.Errorf("ValidateSlug(%q) = nil, want an error", s)
		}
	}
}

// TestNormalizeBaseURL mirrors the existing config validation (FR-5.2): an
// absolute http(s) URL, trailing slash stripped, github defaulting.
func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()
	got, err := auth.NormalizeBaseURL(config.KindGitHub, "")
	if err != nil || got != "https://api.github.com" {
		t.Fatalf("github default = %q, %v; want https://api.github.com, nil", got, err)
	}
	got, err = auth.NormalizeBaseURL(config.KindGitLab, "https://gitlab.example.com/")
	if err != nil || got != "https://gitlab.example.com" {
		t.Fatalf("trailing slash = %q, %v; want https://gitlab.example.com, nil", got, err)
	}
	if _, err := auth.NormalizeBaseURL(config.KindGitLab, ""); err == nil {
		t.Fatal("gitlab with no base url should fail")
	}
	for _, bad := range []string{"ftp://x", "not a url", "/relative", "https://"} {
		if _, err := auth.NormalizeBaseURL(config.KindGitLab, bad); err == nil {
			t.Errorf("NormalizeBaseURL(%q) = nil error, want one", bad)
		}
	}
}

func TestNewIDIsSixteenLowercaseHex(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for range 100 {
		id, err := auth.NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if len(id) != 16 {
			t.Fatalf("NewID() = %q, want 16 characters", id)
		}
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Fatalf("NewID() = %q, want lowercase hex only", id)
			}
		}
		if seen[id] {
			t.Fatalf("NewID() returned a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func TestLast4(t *testing.T) {
	t.Parallel()
	if got := auth.Last4("glpat-abcdef9f2c"); got != "9f2c" {
		t.Fatalf("Last4 = %q, want 9f2c", got)
	}
	if got := auth.Last4("ab"); got != "ab" {
		t.Fatalf("Last4 of a short token = %q, want ab", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd apps/backend && go test ./internal/auth/...
```

Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write `errors.go`**

Create `apps/backend/internal/auth/errors.go`:

```go
// Package auth owns hosted-mode accounts: password hashing, login sessions,
// encrypted per-user provider configuration, login throttling, and the
// per-user provider registry resolver.
//
// It imports internal/db, internal/config, internal/identity, and
// internal/provider (plus the github/gitlab clients). It must never import
// internal/review or internal/session: the cascade that spans those stores
// reaches them through the Purger and ProviderUsage collaborators instead
// (design §6).
package auth

import (
	"errors"
	"net/http"
	"time"
)

// Code is the machine-readable error code carried on the wire. Values match
// PRD §5.3 exactly.
type Code string

const (
	CodeUnauthenticated      Code = "UNAUTHENTICATED"
	CodeInvalidCredentials   Code = "INVALID_CREDENTIALS"
	CodeAccountLocked        Code = "ACCOUNT_LOCKED"
	CodeUsernameTaken        Code = "USERNAME_TAKEN"
	CodeInvalidUsername      Code = "INVALID_USERNAME"
	CodeWeakPassword         Code = "WEAK_PASSWORD"
	CodeForbidden            Code = "FORBIDDEN"
	CodeProviderSlugTaken    Code = "PROVIDER_SLUG_TAKEN"
	CodeProviderUnauthorized Code = "PROVIDER_UNAUTHORIZED"
	CodeProviderInUse        Code = "PROVIDER_IN_USE"
)

// ErrNotFound reports an unknown row, or a row belonging to another user.
// The two are deliberately indistinguishable: FR-4.2 requires a cross-user
// request to answer 404, never 403, so resource existence is not disclosed.
var ErrNotFound = errors.New("auth: not found")

// Error is the typed domain error api.classify recognises with a single
// errors.As arm, mirroring session.ReviewError.
type Error struct {
	Code    Code
	Message string
	// RetryAfter is set only for CodeAccountLocked, and only so the login
	// handler can emit a Retry-After header (FR-7.4). classify returns a
	// triple and cannot set headers, so that one handler reads this field
	// directly.
	RetryAfter time.Duration
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// Status maps a code onto its HTTP status (PRD §5.3).
func (e *Error) Status() int {
	switch e.Code {
	case CodeUnauthenticated, CodeInvalidCredentials:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeUsernameTaken, CodeProviderSlugTaken, CodeProviderInUse:
		return http.StatusConflict
	case CodeInvalidUsername, CodeWeakPassword, CodeProviderUnauthorized:
		return http.StatusUnprocessableEntity
	case CodeAccountLocked:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
```

- [ ] **Step 4: Write `model.go`**

Create `apps/backend/internal/auth/model.go`:

```go
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/config"
)

// FR-2.1: 3–32 characters, starting alphanumeric, then alphanumerics, dot,
// underscore, or hyphen. Stored as entered; compared folded.
var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{2,31}$`)

// FR-5.1: URL-safe provider slug, unique per user.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

const defaultGitHubBaseURL = "https://api.github.com"

// idBytes is 8, giving a 16-character hex id.
//
// This is the same scheme as session.NewID (crypto/rand, lowercase hex) at
// double the width. Wider is warranted here because these ids are long-lived
// primary keys and because a user id becomes a directory name under
// REPOSITORY_CACHE_ROOT (FR-6.5), where a collision would merge two users'
// mirrors.
const idBytes = 8

// NewID returns 16 lowercase hex characters from a CSPRNG.
func NewID() (string, error) {
	var b [idBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("auth: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Fold returns the case-insensitive uniqueness key for a username (FR-2.1).
func Fold(username string) string { return strings.ToLower(username) }

// ValidateUsername enforces FR-2.1.
func ValidateUsername(username string) error {
	if !usernameRe.MatchString(username) {
		return &Error{
			Code:    CodeInvalidUsername,
			Message: "A username is 3 to 32 characters, starts with a letter or digit, and may contain letters, digits, dots, underscores, and hyphens.",
		}
	}
	return nil
}

// ValidateSlug enforces FR-5.1.
//
// It returns a plain error, not an *Error: a malformed slug is a
// request-shape problem, which the handler reports as VALIDATION_ERROR (422)
// per api-contracts.md. CodeProviderSlugTaken is returned only by
// CreateProvider, and only on a genuine uniqueness conflict.
func ValidateSlug(slug string) error {
	if !slugRe.MatchString(slug) {
		return errors.New("auth: a slug is 1 to 32 characters of lowercase letters, digits, and hyphens, starting with a letter or digit")
	}
	return nil
}

// Last4 returns the last four characters of a token, for masked display.
// The whole point of storing it is that rendering a mask never requires a
// decrypt (api-contracts.md, GET /api/settings/providers).
func Last4(token string) string {
	if len(token) <= 4 {
		return token
	}
	return token[len(token)-4:]
}

// NormalizeBaseURL validates and normalises a provider base URL exactly as
// config.buildProvider does (FR-5.2): absolute http(s), trailing slash
// stripped, defaulting for github and required for gitlab.
func NormalizeBaseURL(kind config.Kind, raw string) (string, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		if kind == config.KindGitLab {
			return "", fmt.Errorf("auth: base url is required for gitlab providers")
		}
		base = defaultGitHubBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("auth: base url must be an absolute http(s) URL")
	}
	return strings.TrimRight(base, "/"), nil
}

// User is an account. Fields are unexported and set only through NewUser or
// the store's row scanners, matching the immutable-value style of
// internal/session.
type User struct {
	id           string
	username     string
	usernameFold string
	passwordHash string
	createdAt    time.Time
	updatedAt    time.Time
}

// NewUser validates the username and returns the account value. passwordHash
// is an already-computed Argon2id PHC string: NewUser never hashes, so the
// caller controls when the expensive operation happens relative to any open
// transaction.
func NewUser(id, username, passwordHash string, now time.Time) (User, error) {
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if id == "" || passwordHash == "" {
		return User{}, fmt.Errorf("auth: user id and password hash are required")
	}
	return User{
		id:           id,
		username:     username,
		usernameFold: Fold(username),
		passwordHash: passwordHash,
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

func (u User) ID() string           { return u.id }
func (u User) Username() string     { return u.username }
func (u User) UsernameFold() string { return u.usernameFold }
func (u User) PasswordHash() string { return u.passwordHash }
func (u User) CreatedAt() time.Time { return u.createdAt }
func (u User) UpdatedAt() time.Time { return u.updatedAt }

// LoginSession is a cookie-backed session. Only the SHA-256 of the token is
// ever held here or persisted (FR-3.3); the plaintext lives in the response
// that creates it and in the client's cookie jar, nowhere else.
type LoginSession struct {
	tokenHash  []byte
	userID     string
	createdAt  time.Time
	expiresAt  time.Time
	lastSeenAt time.Time
}

// NewLoginSession builds the value for a freshly minted token hash.
func NewLoginSession(tokenHash []byte, userID string, now time.Time, ttl time.Duration) LoginSession {
	return LoginSession{
		tokenHash:  tokenHash,
		userID:     userID,
		createdAt:  now,
		expiresAt:  now.Add(ttl),
		lastSeenAt: now,
	}
}

func (s LoginSession) TokenHash() []byte     { return append([]byte(nil), s.tokenHash...) }
func (s LoginSession) UserID() string        { return s.userID }
func (s LoginSession) CreatedAt() time.Time  { return s.createdAt }
func (s LoginSession) ExpiresAt() time.Time  { return s.expiresAt }
func (s LoginSession) LastSeenAt() time.Time { return s.lastSeenAt }

// UserProvider is one user's provider configuration. The token is present
// only as ciphertext plus nonce plus a four-character tail; there is no field
// anywhere in this type that holds a plaintext token.
type UserProvider struct {
	id              string
	userID          string
	slug            string
	displayName     string
	kind            config.Kind
	baseURL         string
	tokenCiphertext []byte
	tokenNonce      []byte
	tokenLast4      string
	tokenSetAt      time.Time
	createdAt       time.Time
	updatedAt       time.Time
}

func (p UserProvider) ID() string          { return p.id }
func (p UserProvider) UserID() string      { return p.userID }
func (p UserProvider) Slug() string        { return p.slug }
func (p UserProvider) DisplayName() string { return p.displayName }
func (p UserProvider) Kind() config.Kind   { return p.kind }
func (p UserProvider) BaseURL() string     { return p.baseURL }
func (p UserProvider) TokenCiphertext() []byte {
	return append([]byte(nil), p.tokenCiphertext...)
}
func (p UserProvider) TokenNonce() []byte     { return append([]byte(nil), p.tokenNonce...) }
func (p UserProvider) TokenLast4() string     { return p.tokenLast4 }
func (p UserProvider) TokenSetAt() time.Time  { return p.tokenSetAt }
func (p UserProvider) CreatedAt() time.Time   { return p.createdAt }
func (p UserProvider) UpdatedAt() time.Time   { return p.updatedAt }
```

Add `errors` to `model.go`'s imports.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/auth/... && go vet ./internal/auth/...
```

Expected: PASS on all seven test functions.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/auth
git commit -m "$(cat <<'EOF'
feat(auth): add the error vocabulary and immutable account models

auth.Error mirrors session.ReviewError so api.classify gains one errors.As
arm rather than ten errors.Is cases. UserProvider has no field that can hold
a plaintext token, which is how FR-5.4 is enforced by the type rather than by
handler discipline.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `auth/password.go` — Argon2id

**Files:**
- Create: `apps/backend/internal/auth/password.go`
- Test: `apps/backend/internal/auth/password_test.go`

**Interfaces:**
- Consumes: `auth.Error`, `auth.CodeWeakPassword` (Task 4).
- Produces: `auth.HashPassword(password string) (string, error)`, `auth.VerifyPassword(encoded, password string) error`, `auth.ValidatePassword(password string) error`, `auth.ErrPasswordMismatch error`, `auth.MinPasswordLen`/`auth.MaxPasswordLen` constants, and the package-private `dummyHash` used for timing equalisation.

- [ ] **Step 1: Add the dependency**

```bash
cd apps/backend && go get golang.org/x/crypto@latest && go mod tidy && CGO_ENABLED=0 go build ./...
```

- [ ] **Step 2: Write the failing test**

Create `apps/backend/internal/auth/password_test.go`:

```go
package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jtumidanski/converge/internal/auth"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	const pw = "correct horse battery staple"
	encoded, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := auth.VerifyPassword(encoded, pw); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if err := auth.VerifyPassword(encoded, pw+"x"); !errors.Is(err, auth.ErrPasswordMismatch) {
		t.Fatalf("wrong password gave %v, want ErrPasswordMismatch", err)
	}
}

// TestHashIsSaltedPerCall proves two hashes of the same password differ, so
// the stored value is not a lookup key for a rainbow table.
func TestHashIsSaltedPerCall(t *testing.T) {
	t.Parallel()
	a, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical; the salt is not random")
	}
}

// TestEncodedFormIsPHC pins the stored shape at the documented parameters
// (FR-2.3) so a later parameter bump is a visible, deliberate change.
func TestEncodedFormIsPHC(t *testing.T) {
	t.Parallel()
	encoded, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=1,p=4$") {
		t.Fatalf("encoded = %q, want the documented Argon2id PHC prefix", encoded)
	}
	if n := strings.Count(encoded, "$"); n != 5 {
		t.Fatalf("encoded = %q, want 5 '$' separators", encoded)
	}
}

// TestVerifyReadsParamsFromTheString is the FR-2.3 requirement that matters
// most for a future parameter raise: a hash written under parameters other
// than the package constants must still verify, because verification reads
// m/t/p from the stored string.
//
// The fixture is computed here rather than pasted as a literal, so the test
// carries no magic constant and stays correct if the PHC encoding changes.
// The parameters are deliberately far below the production ones (m=8 KiB
// instead of 64 MiB, p=1 instead of 4), so a VerifyPassword that ignored the
// stored parameters and used the constants would produce a different key and
// fail.
func TestVerifyReadsParamsFromTheString(t *testing.T) {
	t.Parallel()
	const pw = "hunter2hunter2"
	salt := []byte("saltsaltsaltsalt")
	var (
		weakTime    uint32 = 1
		weakMemory  uint32 = 8
		weakThreads uint8  = 1
		keyLen      uint32 = 32
	)
	key := argon2.IDKey([]byte(pw), salt, weakTime, weakMemory, weakThreads, keyLen)
	enc := base64.RawStdEncoding
	weak := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		weakMemory, weakTime, weakThreads,
		enc.EncodeToString(salt), enc.EncodeToString(key))

	if err := auth.VerifyPassword(weak, pw); err != nil {
		t.Fatalf("VerifyPassword against weak params: %v", err)
	}
	if err := auth.VerifyPassword(weak, pw+"x"); !errors.Is(err, auth.ErrPasswordMismatch) {
		t.Fatalf("wrong password against weak params gave %v, want ErrPasswordMismatch", err)
	}
}

func TestVerifyRejectsMalformedEncodings(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"",
		"not-a-phc-string",
		"$argon2i$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",   // wrong variant
		"$argon2id$v=18$m=65536,t=1,p=4$c2FsdA$a2V5",  // wrong version
		"$argon2id$v=19$m=x,t=1,p=4$c2FsdA$a2V5",      // non-numeric memory
		"$argon2id$v=19$m=65536,t=1,p=4$!!!$a2V5",     // bad base64 salt
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA",       // too few fields
	} {
		if err := auth.VerifyPassword(bad, "whatever"); err == nil {
			t.Errorf("VerifyPassword(%q) = nil, want an error", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	t.Parallel()
	// FR-2.2: length is the only rule. No composition requirements.
	if err := auth.ValidatePassword("12345678"); err != nil {
		t.Fatalf("8 characters rejected: %v", err)
	}
	if err := auth.ValidatePassword(strings.Repeat("a", 1024)); err != nil {
		t.Fatalf("1024 characters rejected: %v", err)
	}
	for _, bad := range []string{"", "1234567", strings.Repeat("a", 1025)} {
		var ae *auth.Error
		err := auth.ValidatePassword(bad)
		if !errors.As(err, &ae) || ae.Code != auth.CodeWeakPassword {
			t.Errorf("ValidatePassword(len %d) = %v, want WEAK_PASSWORD", len(bad), err)
		}
	}
}
```

> **Implementer note:** this test file imports `golang.org/x/crypto/argon2`, `encoding/base64`, and `fmt` in addition to the usual test imports, because `TestVerifyReadsParamsFromTheString` builds its own weak-parameter fixture. That is deliberate — it is the one test that must not go through `HashPassword`, since `HashPassword` always uses the package constants and would therefore prove nothing about reading parameters back.

- [ ] **Step 3: Run the tests to verify they fail**

```bash
cd apps/backend && go test ./internal/auth/... -run 'Password|PHC|Hash'
```

Expected: FAIL to compile — `auth.HashPassword` is undefined.

- [ ] **Step 4: Write the implementation**

Create `apps/backend/internal/auth/password.go`:

```go
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Password length bounds (FR-2.2). There are deliberately no composition
// rules — no required symbol, digit, or mixed case. Length is the only
// requirement, and that is a decision, not an oversight.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 1024
)

// Argon2id parameters (FR-2.3). They live here, in one place, so they can be
// raised later; VerifyPassword deliberately reads m/t/p from the stored
// string rather than from these constants, so raising them does not
// invalidate existing hashes.
const (
	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024 // KiB, i.e. 64 MiB
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
	argonVersion        = argon2.Version // 19
)

// ErrPasswordMismatch reports a correct encoding that does not match the
// supplied password. A malformed encoding returns a different error.
var ErrPasswordMismatch = errors.New("auth: password does not match")

// ValidatePassword enforces FR-2.2.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLen || len(password) > MaxPasswordLen {
		return &Error{
			Code:    CodeWeakPassword,
			Message: fmt.Sprintf("A password must be between %d and %d characters.", MinPasswordLen, MaxPasswordLen),
		}
	}
	return nil
}

// HashPassword returns a PHC-encoded Argon2id hash at the current parameters.
//
// This is deliberately expensive (64 MiB, ~50 ms). Callers must not hold a
// database transaction across it: the pool has exactly one connection, so
// hashing inside a transaction would block every other query for the
// duration. auth.Service enforces the ordering (hash, then open a
// transaction) and bounds concurrency with a semaphore.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return encodePHC(phcParams{memory: argonMemory, time: argonTime, threads: argonThreads}, salt, key), nil
}

// VerifyPassword recomputes the hash using the parameters recorded in
// encoded and compares in constant time.
func VerifyPassword(encoded, password string) error {
	p, salt, want, err := decodePHC(encoded)
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

type phcParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func encodePHC(p phcParams, salt, key []byte) string {
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersion, p.memory, p.time, p.threads,
		enc.EncodeToString(salt), enc.EncodeToString(key))
}

func decodePHC(encoded string) (phcParams, []byte, []byte, error) {
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return phcParams{}, nil, nil, errors.New("auth: malformed password hash")
	}
	if parts[1] != "argon2id" {
		return phcParams{}, nil, nil, fmt.Errorf("auth: unsupported password hash variant %q", parts[1])
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return phcParams{}, nil, nil, fmt.Errorf("auth: malformed password hash version: %w", err)
	}
	if version != argonVersion {
		return phcParams{}, nil, nil, fmt.Errorf("auth: unsupported argon2 version %d", version)
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return phcParams{}, nil, nil, fmt.Errorf("auth: malformed password hash parameters: %w", err)
	}
	if memory == 0 || timeCost == 0 || threads == 0 {
		return phcParams{}, nil, nil, errors.New("auth: password hash parameters must be positive")
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return phcParams{}, nil, nil, fmt.Errorf("auth: malformed password hash salt: %w", err)
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil {
		return phcParams{}, nil, nil, fmt.Errorf("auth: malformed password hash key: %w", err)
	}
	if len(salt) == 0 || len(key) == 0 {
		return phcParams{}, nil, nil, errors.New("auth: password hash salt and key must be non-empty")
	}
	return phcParams{memory: memory, time: timeCost, threads: threads}, salt, key, nil
}

// dummyHash equalises Login's cost between an unknown username and a wrong
// password (FR-2.5). It is generated once, at init, from a random password,
// so nothing can ever verify against it. Login verifies against this when the
// username is unknown, which makes the dominant term — one Argon2id verify —
// identical on both paths; the residual difference is one index lookup,
// nanoseconds against ~50 ms of hashing.
var dummyHash = mustDummyHash()

func mustDummyHash() string {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		// A CSPRNG failure at init is unrecoverable and would otherwise
		// leave the timing defence silently disabled.
		panic("auth: generate dummy hash secret: " + err.Error())
	}
	encoded, err := HashPassword(string(secret))
	if err != nil {
		panic("auth: generate dummy hash: " + err.Error())
	}
	return encoded
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/auth/... && go vet ./internal/auth/... && go tool golangci-lint run ./internal/auth/...
```

Expected: PASS on all six password test functions plus Task 4's.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/auth apps/backend/go.mod apps/backend/go.sum
git commit -m "$(cat <<'EOF'
feat(auth): add Argon2id hashing with PHC encoding

Verification reads m/t/p from the stored string rather than the package
constants (FR-2.3), so raising the parameters later does not invalidate
existing hashes. The package-level dummy hash, generated once at init from a
random password, is what equalises Login's timing between an unknown
username and a wrong password (FR-2.5).

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `auth/crypt.go` — AES-256-GCM token sealing

**Files:**
- Create: `apps/backend/internal/auth/crypt.go`
- Test: `apps/backend/internal/auth/crypt_test.go`

**Interfaces:**
- Consumes: `config.Secret`, `config.NewSecret`.
- Produces: `auth.Sealer`, `auth.NewSealer(key config.Secret) (*Sealer, error)`, `(*Sealer) Seal(plaintext, userID, providerID string) (ciphertext, nonce []byte, err error)`, `(*Sealer) Open(ciphertext, nonce []byte, userID, providerID string) (config.Secret, error)`.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/auth/crypt_test.go`:

```go
package auth_test

import (
	"testing"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
)

// key32 is 32 bytes of 0x01, base64 standard encoding. Test fixture only.
const key32 = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

func sealer(t *testing.T) *auth.Sealer {
	t.Helper()
	s, err := auth.NewSealer(config.NewSecret(key32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return s
}

func TestSealOpenRoundTrip(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	const token = "glpat-xxxxxxxxxxxxxxxxxxxx"
	ct, nonce, err := s.Seal(token, "user1", "prov1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if string(ct) == token {
		t.Fatal("ciphertext equals the plaintext")
	}
	got, err := s.Open(ct, nonce, "user1", "prov1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got.Reveal() != token {
		t.Fatalf("Open = %q, want the original token", got.Reveal())
	}
}

func TestSealUsesAFreshNoncePerCall(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	_, n1, err := s.Seal("t", "user1", "prov1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	_, n2, err := s.Seal("t", "user1", "prov1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if string(n1) == string(n2) {
		t.Fatal("two Seal calls produced the same nonce")
	}
}

// TestCiphertextIsBoundToItsRow is the FR-5.3 guarantee and an explicit
// acceptance criterion: a ciphertext copied into another user's row, or
// another provider row of the same user, must fail to decrypt.
func TestCiphertextIsBoundToItsRow(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	ct, nonce, err := s.Seal("glpat-secret", "userA", "provA")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	for _, tc := range []struct{ name, user, provider string }{
		{"foreign user", "userB", "provA"},
		{"foreign provider row", "userA", "provB"},
		{"both foreign", "userB", "provB"},
	} {
		if _, err := s.Open(ct, nonce, tc.user, tc.provider); err == nil {
			t.Errorf("%s: Open succeeded, want an authentication failure", tc.name)
		}
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	ct, nonce, err := s.Seal("glpat-secret", "userA", "provA")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	ct[0] ^= 0xff
	if _, err := s.Open(ct, nonce, "userA", "provA"); err == nil {
		t.Fatal("Open accepted a tampered ciphertext")
	}
}

func TestNewSealerRejectsBadKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, key string }{
		{"empty", ""},
		{"not base64", "!!!!"},
		{"too short", "AQEB"},
	} {
		if _, err := auth.NewSealer(config.NewSecret(tc.key)); err == nil {
			t.Errorf("NewSealer(%s) = nil error, want one", tc.name)
		}
	}
}

// TestOpenReturnsARedactedSecret proves the decrypted token comes back
// wrapped, so it cannot be printed, logged, or marshalled by accident.
func TestOpenReturnsARedactedSecret(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	ct, nonce, err := s.Seal("glpat-secret", "userA", "provA")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := s.Open(ct, nonce, "userA", "provA")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got.String() == "glpat-secret" {
		t.Fatal("String() revealed the token")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd apps/backend && go test ./internal/auth/... -run Seal
```

Expected: FAIL to compile — `auth.NewSealer` is undefined.

- [ ] **Step 3: Write the implementation**

Create `apps/backend/internal/auth/crypt.go`:

```go
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jtumidanski/converge/internal/config"
)

// secretKeyLen is the AES-256 key length. config.Load already rejects a
// CONVERGE_SECRET_KEY that does not decode to exactly this many bytes; the
// check is repeated here because NewSealer is also reachable from tests.
const secretKeyLen = 32

// Sealer encrypts and decrypts provider tokens with AES-256-GCM under the
// operator's master key.
//
// Plaintext tokens exist only in memory, for the lifetime of a decrypted
// provider registry entry (bounded by the resolver's cache TTL). They are
// never written to the database, never returned by the API, and never logged.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer derives the AEAD from the base64-encoded master key.
func NewSealer(key config.Secret) (*Sealer, error) {
	raw, err := base64.StdEncoding.DecodeString(key.Reveal())
	if err != nil {
		return nil, fmt.Errorf("auth: master key must be base64 standard encoding: %w", err)
	}
	if len(raw) != secretKeyLen {
		return nil, fmt.Errorf("auth: master key must decode to %d bytes, got %d", secretKeyLen, len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("auth: build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: build gcm: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// aad binds a ciphertext to the exact row that holds it (FR-5.3). Because the
// user id and provider row id are authenticated additional data, a ciphertext
// moved between rows or between users fails to open. The NUL separator stops
// ("ab", "c") and ("a", "bc") from producing the same AAD.
func aad(userID, providerID string) []byte {
	out := make([]byte, 0, len(userID)+1+len(providerID))
	out = append(out, userID...)
	out = append(out, 0)
	out = append(out, providerID...)
	return out
}

// Seal encrypts plaintext under a fresh random nonce, returning the
// ciphertext and that nonce for storage in the row's own columns.
func (s *Sealer) Seal(plaintext, userID, providerID string) ([]byte, []byte, error) {
	if userID == "" || providerID == "" {
		return nil, nil, errors.New("auth: seal requires a user id and a provider id")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("auth: generate nonce: %w", err)
	}
	ciphertext := s.aead.Seal(nil, nonce, []byte(plaintext), aad(userID, providerID))
	return ciphertext, nonce, nil
}

// Open decrypts a stored token, returning it wrapped so it cannot be printed,
// logged, or marshalled by accident.
func (s *Sealer) Open(ciphertext, nonce []byte, userID, providerID string) (config.Secret, error) {
	if len(nonce) != s.aead.NonceSize() {
		return config.Secret{}, fmt.Errorf("auth: nonce must be %d bytes, got %d", s.aead.NonceSize(), len(nonce))
	}
	plaintext, err := s.aead.Open(nil, nonce, ciphertext, aad(userID, providerID))
	if err != nil {
		// Deliberately does not echo the ciphertext or the ids into the
		// error: this string reaches logs.
		return config.Secret{}, fmt.Errorf("auth: decrypt provider token: %w", err)
	}
	return config.NewSecret(string(plaintext)), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/auth/... && go tool golangci-lint run ./internal/auth/...
```

Expected: PASS on all six crypt test functions.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/auth
git commit -m "$(cat <<'EOF'
feat(auth): seal provider tokens with AES-256-GCM bound to their row

The AAD is user_id | provider_id, so a ciphertext copied into another user's
row fails to open (FR-5.3) — proved by TestCiphertextIsBoundToItsRow, which
is a stated acceptance criterion. Open returns a config.Secret so the
plaintext cannot be logged or marshalled by accident.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: `auth/store.go` — all the SQL

**Files:**
- Create: `apps/backend/internal/auth/store.go`
- Test: `apps/backend/internal/auth/store_test.go`

**Interfaces:**
- Consumes: `db.Open`, `db.Migrate` (Task 3); `auth.User`, `auth.LoginSession`, `auth.UserProvider`, `auth.ErrNotFound`, `auth.Error`, `auth.CodeUsernameTaken`, `auth.CodeProviderSlugTaken` (Task 4).
- Produces: `auth.NewStore(handle *sql.DB) *Store` and these methods, all taking `ctx context.Context` first:

```go
// users
func (s *Store) CreateUser(ctx context.Context, u User) error                    // *Error{CodeUsernameTaken} on conflict
func (s *Store) UserByFold(ctx context.Context, fold string) (User, error)        // ErrNotFound
func (s *Store) UserByID(ctx context.Context, id string) (User, error)            // ErrNotFound
func (s *Store) SetPasswordHash(ctx context.Context, userID, hash string, now time.Time) error
func (s *Store) DeleteUser(ctx context.Context, id string) error
func (s *Store) CountUsers(ctx context.Context) (int, error)

// login sessions
func (s *Store) CreateLoginSession(ctx context.Context, ls LoginSession) error
func (s *Store) LoginSession(ctx context.Context, tokenHash []byte) (LoginSession, error) // ErrNotFound
func (s *Store) TouchLoginSession(ctx context.Context, tokenHash []byte, at time.Time) error
func (s *Store) DeleteLoginSession(ctx context.Context, tokenHash []byte) error
func (s *Store) DeleteOtherLoginSessions(ctx context.Context, userID string, keep []byte) error
func (s *Store) DeleteExpiredLoginSessions(ctx context.Context, now time.Time, idle time.Duration) (int64, error)

// provider configurations
func (s *Store) CreateUserProvider(ctx context.Context, p UserProvider) error     // *Error{CodeProviderSlugTaken} on conflict
func (s *Store) ListUserProviders(ctx context.Context, userID string) ([]UserProvider, error) // sorted by slug
func (s *Store) UserProviderByID(ctx context.Context, userID, id string) (UserProvider, error) // ErrNotFound
func (s *Store) UpdateUserProvider(ctx context.Context, p UserProvider) error
func (s *Store) DeleteUserProvider(ctx context.Context, userID, id string) error  // ErrNotFound when no row matched
func (s *Store) CountUserProviders(ctx context.Context, userID string) (int, error)

// throttle counters
func (s *Store) Attempt(ctx context.Context, scope, key string) (Attempt, error)  // zero Attempt, nil error when absent
func (s *Store) SaveAttempt(ctx context.Context, a Attempt) error                  // upsert
func (s *Store) ClearAttempt(ctx context.Context, scope, key string) error
func (s *Store) DeleteElapsedAttempts(ctx context.Context, before time.Time) (int64, error)

// db reachability, for /healthz
func (s *Store) Ping(ctx context.Context) error
```

`Attempt` is declared in Task 8 (`throttle.go`) but referenced here; write `throttle.go`'s `Attempt` struct as part of this task so the package compiles, and leave the `Throttle` logic for Task 8.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/auth/store_test.go`. Cover, one test function each:

```go
package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/db"
)

// newStore returns a migrated, file-backed store. A file rather than
// :memory: because MaxOpenConns(1) plus WAL is the configuration under test
// and an in-memory database does not exercise it.
func newStore(t *testing.T) *auth.Store {
	t.Helper()
	handle, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "converge.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return auth.NewStore(handle)
}

// mustUser inserts an account and returns it.
func mustUser(t *testing.T, s *auth.Store, username string) auth.User {
	t.Helper()
	id, err := auth.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	u, err := auth.NewUser(id, username, "$argon2id$fake", time.Unix(1000, 0).UTC())
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}
```

Then these test functions, each asserting exactly the behaviour named:

1. `TestCreateAndReadUser` — `CreateUser`, then `UserByFold("alice")` and `UserByID(u.ID())` both return the account with `Username() == "Alice"` (stored as entered) and the same `PasswordHash()`; `CreatedAt()` round-trips to the same unix second.
2. `TestUserByFoldIsCaseInsensitive` — create `"Alice"`, then `UserByFold(auth.Fold("ALICE"))` succeeds.
3. `TestCreateUserRejectsACaseInsensitiveDuplicate` — create `"Alice"`, then create `"ALICE"` with a different id; assert `errors.As(err, &ae)` and `ae.Code == auth.CodeUsernameTaken` (FR-2.4).
4. `TestUserLookupsReturnErrNotFound` — `UserByFold("ghost")` and `UserByID("nope")` both `errors.Is(err, auth.ErrNotFound)`.
5. `TestSetPasswordHash` — update, re-read, assert the new hash and a bumped `UpdatedAt()`.
6. `TestCountUsers` — zero, then one, then two.
7. `TestLoginSessionLifecycle` — `CreateLoginSession`, `LoginSession(hash)` returns the same `UserID`/`ExpiresAt`/`LastSeenAt`; `TouchLoginSession` moves `LastSeenAt` only; `DeleteLoginSession` then `LoginSession` returns `ErrNotFound`.
8. `TestDeleteOtherLoginSessions` — three sessions for one user plus one for a second user; `DeleteOtherLoginSessions(userA, keep)` leaves exactly `keep` and user B's session (FR-2.6).
9. `TestDeleteExpiredLoginSessions` — one absolutely expired (`expires_at` in the past), one idle-expired (`last_seen_at` older than `idle`), one live; assert the returned count is 2 and only the live one survives (FR-3.7).
10. `TestDeleteUserCascades` — create a user with a login session and a provider config and a `('user', fold)` attempt row; `DeleteUser`; assert all three are gone (FR-2.7).
11. `TestUserProviderCRUD` — create two configs, `ListUserProviders` returns them **sorted by slug**; `UserProviderByID` round-trips every field including `TokenCiphertext`/`TokenNonce`/`TokenLast4`; `UpdateUserProvider` changes display name and token columns and bumps `UpdatedAt`; `DeleteUserProvider` then `UserProviderByID` returns `ErrNotFound`.
12. `TestUserProviderIsScopedToItsOwner` — user A's config id read with user B's user id returns `ErrNotFound`; `DeleteUserProvider(userB, idOfA)` returns `ErrNotFound` and A's row survives.
13. `TestCreateUserProviderRejectsADuplicateSlug` — same user, same slug → `*auth.Error` with `CodeProviderSlugTaken`; a **different** user with the same slug succeeds (the uniqueness is per user).
14. `TestCountUserProviders` — zero, then two, and a second user's count is unaffected.
15. `TestAttemptUpsertAndClear` — `Attempt` of an absent key returns the zero `Attempt` and a nil error; `SaveAttempt` then `Attempt` round-trips `Failures`/`WindowStart`/`LockedUntil` to the same unix second; `SaveAttempt` again with new values updates in place (one row); `ClearAttempt` removes it.
16. `TestDeleteElapsedAttempts` — two rows, one with `locked_until` in the past and a stale window, one live; assert the count and the survivor.
17. `TestPing` — returns nil on a healthy store.

For a provider fixture, seal a token with a `*auth.Sealer` built from `key32` (Task 6) so the ciphertext columns hold realistic bytes.

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/auth/... -run Store
```

Expected: FAIL to compile — `auth.NewStore` is undefined.

- [ ] **Step 3: Write `store.go`**

Create `apps/backend/internal/auth/store.go`. Rules this file must follow:

```go
package auth

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

func NewStore(handle *sql.DB) *Store { return &Store{db: handle} }
```

Implementation notes, per method group:

- **Time helpers.** `func unix(t time.Time) int64 { return t.Unix() }` and `func fromUnix(v int64) time.Time { return time.Unix(v, 0).UTC() }`. Use them everywhere so the round-trip is uniform.
- **`CreateUser`** — `BeginTx`, `SELECT 1 FROM users WHERE username_fold = ?`; on a row, `return &Error{Code: CodeUsernameTaken, Message: "That username is already registered."}`; otherwise `INSERT INTO users (id, username, username_fold, password_hash, created_at, updated_at) VALUES (?,?,?,?,?,?)`, then `Commit`. `defer func() { _ = tx.Rollback() }()`.
- **Row scanning** — one private helper per table, e.g.:
  ```go
  func scanUser(row interface{ Scan(...any) error }) (User, error) {
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
  ```
  This is why `User`'s fields are unexported but the store lives in the same package: the store is the only thing allowed to build a value from a row.
- **`LoginSession`** — `SELECT token_hash, user_id, created_at, expires_at, last_seen_at FROM login_sessions WHERE token_hash = ?`. A primary-key lookup, which is FR-3.3's "compared by hash lookup, not by scanning and comparing plaintext".
- **`DeleteExpiredLoginSessions`** — one statement: `DELETE FROM login_sessions WHERE expires_at <= ? OR last_seen_at <= ?` with `now.Unix()` and `now.Add(-idle).Unix()`. Return `result.RowsAffected()`.
- **`DeleteOtherLoginSessions`** — `DELETE FROM login_sessions WHERE user_id = ? AND token_hash != ?`.
- **`ListUserProviders`** — `... WHERE user_id = ? ORDER BY slug`. `defer func() { _ = rows.Close() }()` and check `rows.Err()` (errcheck is enabled).
- **`UserProviderByID`** — `... WHERE user_id = ? AND id = ?`. Scoping by `user_id` in the WHERE clause, not by a post-read owner check, is what makes a foreign id indistinguishable from an unknown one (FR-4.2).
- **`UpdateUserProvider`** — `UPDATE user_providers SET display_name = ?, kind = ?, base_url = ?, token_ciphertext = ?, token_nonce = ?, token_last4 = ?, token_set_at = ?, updated_at = ? WHERE user_id = ? AND id = ?`. `slug` is deliberately absent: it is immutable. If `RowsAffected() == 0`, return `ErrNotFound`.
- **`DeleteUserProvider`** / **`DeleteUser`** — check `RowsAffected()`; `DeleteUserProvider` returns `ErrNotFound` on zero, `DeleteUser` returns `ErrNotFound` on zero.
- **`Attempt`** — `SELECT scope, key, failures, window_start, locked_until FROM login_attempts WHERE scope = ? AND key = ?`; on `sql.ErrNoRows` return `Attempt{Scope: scope, Key: key}, nil` (an absent counter is a zero counter, not an error).
- **`SaveAttempt`** — `INSERT INTO login_attempts (scope, key, failures, window_start, locked_until) VALUES (?,?,?,?,?) ON CONFLICT(scope, key) DO UPDATE SET failures = excluded.failures, window_start = excluded.window_start, locked_until = excluded.locked_until`.
- **`DeleteElapsedAttempts`** — `DELETE FROM login_attempts WHERE locked_until <= ? AND window_start <= ?` with `before.Unix()` for both, so a row that is neither locked nor within its window is reaped.
- **`Ping`** — `s.db.PingContext(ctx)`, wrapped.

Add to `throttle.go` (created fully in Task 8) just the value type so this task compiles:

```go
package auth

// Attempt is one throttle counter row: failures against a folded username
// ("user" scope) or a client IP ("ip" scope). Persisted so a restart does not
// clear a lockout in progress (FR-7.1).
type Attempt struct {
	Scope       string
	Key         string
	Failures    int
	WindowStart time.Time
	LockedUntil time.Time
}

// Attempt scopes.
const (
	ScopeUser = "user"
	ScopeIP   = "ip"
)
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/auth/... && go tool golangci-lint run ./internal/auth/...
```

Expected: PASS on all seventeen store test functions plus everything from Tasks 4–6.

- [ ] **Step 5: Prove no SQL escaped the file**

```bash
cd apps/backend && grep -rniE '\b(select|insert into|update .* set|delete from)\b' --include='*.go' internal/ cmd/ | grep -v '_test.go' | grep -v 'internal/auth/store.go' | grep -v 'internal/db/db.go'
```

Expected: no output. (`internal/db/db.go` legitimately holds the `schema_migrations` bootstrap and the migration recorder.)

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/auth
git commit -m "$(cat <<'EOF'
feat(auth): add the SQLite-backed account and provider store

All SQL lives in this one file and every statement is parameterised.
Uniqueness is enforced by check-then-insert inside a transaction rather than
by matching driver error strings — exact because the pool has one connection
and this process is the only writer. Provider reads are scoped by user_id in
the WHERE clause, which is what makes a foreign id indistinguishable from an
unknown one (FR-4.2).

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: `auth/throttle.go` — the doubling lockout

**Files:**
- Modify: `apps/backend/internal/auth/throttle.go` (add `Throttle` to the `Attempt` type added in Task 7)
- Test: `apps/backend/internal/auth/throttle_test.go`

**Interfaces:**
- Consumes: `auth.Store` (Task 7), `auth.Attempt`, `auth.ScopeUser`, `auth.ScopeIP`, `auth.Error`, `auth.CodeAccountLocked`.
- Produces: `auth.NewThrottle(store *Store, now func() time.Time) *Throttle` with:
  ```go
  func (t *Throttle) Check(ctx context.Context, userKey, ipKey string) error
  func (t *Throttle) Fail(ctx context.Context, userKey, ipKey string) error
  func (t *Throttle) Succeed(ctx context.Context, userKey, ipKey string) error
  func (t *Throttle) Sweep(ctx context.Context) (int64, error)
  ```
  `userKey` is a folded username or `""` (registration, which is IP-throttled only per FR-7.5). `Check` returns `*Error{Code: CodeAccountLocked, RetryAfter: d}` when either key is locked.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/auth/throttle_test.go`. Use a settable clock so the schedule is tested without sleeping:

```go
package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_700_000_000, 0).UTC()} }
func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
```

Test functions:

1. **`TestUserLockoutEngagesAtFiveFailures`** — `Fail` five times with `userKey = "alice"`; the first four `Check`s in between return nil; after the fifth, `Check` returns an `*auth.Error` with `CodeAccountLocked` and `RetryAfter` within `(0, time.Minute]` (FR-7.2). Advance one minute; `Check` returns nil again.
2. **`TestUserLockoutDoubles`** — drive failures through the schedule, advancing past each lockout, and assert the `RetryAfter` sequence is `1m, 2m, 4m, 8m, 15m, 15m` — i.e. `min(1m << (failures-5), 15m)` (FR-7.2, with the 15-minute cap).
3. **`TestSuccessClearsTheUserCounter`** — four failures, then `Succeed`, then four more failures; `Check` still returns nil, because the counter restarted (FR-7.2).
4. **`TestIPLockoutAtTwentyFailuresWithinTheWindow`** — 20 `Fail` calls with distinct `userKey`s but one `ipKey`; `Check` with a fresh username is now locked (FR-7.3).
5. **`TestIPWindowResets`** — 19 failures from one IP, advance 16 minutes (past the 15-minute window), 19 more; `Check` returns nil because the window restarted.
6. **`TestRegistrationThrottleUsesTheIPKeyOnly`** — `Fail(ctx, "", ip)` twenty times, then `Check(ctx, "", ip)` is locked, and `Check(ctx, "alice", otherIP)` is not (FR-7.5).
7. **`TestLockoutSurvivesReopeningTheDatabase`** — an explicit acceptance criterion. Build a store over a file path kept in a variable, drive five failures, `Close` the handle, `db.Open` + `db.Migrate` the same path again, build a new `Store`/`Throttle` on the same clock, and assert `Check` is still locked (FR-7.1).
8. **`TestSweepRemovesElapsedRows`** — drive a lockout, advance an hour, `Sweep` returns 1 and `Check` is clean.
9. **`TestLockoutMessageDoesNotDiscloseWhichKeyIsLocked`** — lock the username, then lock a different IP; both `Check` errors carry the identical `Message` (FR-7.4: "The detail does not say whether the lock is on the username or the IP").

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/auth/... -run Throttle
```

Expected: FAIL to compile — `auth.NewThrottle` is undefined.

- [ ] **Step 3: Write the implementation**

Append to `apps/backend/internal/auth/throttle.go`:

```go
// Throttle thresholds and schedule (FR-7.2, FR-7.3).
const (
	userFailureThreshold = 5
	ipFailureThreshold   = 20
	ipWindow             = 15 * time.Minute
	baseLockout          = time.Minute
	maxLockout           = 15 * time.Minute
)

// lockedMessage is identical for a username lock and an IP lock. Saying which
// one engaged would disclose whether the username exists (FR-7.4).
const lockedMessage = "Too many failed attempts. Try again later."

// Throttle counts failed attempts and engages a doubling lockout. Counters
// are persisted, so a restart does not clear a lockout in progress (FR-7.1).
type Throttle struct {
	store *Store
	now   func() time.Time
}

func NewThrottle(store *Store, now func() time.Time) *Throttle {
	return &Throttle{store: store, now: now}
}

// Check reports whether either key is currently locked. It runs before any
// password verification, so a locked caller never pays for (or benefits from)
// an Argon2id comparison.
func (t *Throttle) Check(ctx context.Context, userKey, ipKey string) error {
	now := t.now()
	for _, k := range t.keys(userKey, ipKey) {
		a, err := t.store.Attempt(ctx, k.scope, k.key)
		if err != nil {
			return err
		}
		if now.Before(a.LockedUntil) {
			return &Error{
				Code:       CodeAccountLocked,
				Message:    lockedMessage,
				RetryAfter: a.LockedUntil.Sub(now),
			}
		}
	}
	return nil
}

// Fail records one failure against each supplied key and engages or extends
// the lockout when the key's threshold is met.
func (t *Throttle) Fail(ctx context.Context, userKey, ipKey string) error {
	now := t.now()
	for _, k := range t.keys(userKey, ipKey) {
		a, err := t.store.Attempt(ctx, k.scope, k.key)
		if err != nil {
			return err
		}
		// The IP counter is windowed: 20 failures "within 15 minutes"
		// (FR-7.3). The username counter is consecutive-failure based and
		// resets only on success (FR-7.2), so it has no window.
		if k.scope == ScopeIP && !a.WindowStart.IsZero() && now.Sub(a.WindowStart) > ipWindow {
			a.Failures = 0
			a.WindowStart = time.Time{}
		}
		if a.WindowStart.IsZero() {
			a.WindowStart = now
		}
		a.Failures++
		if a.Failures >= k.threshold {
			// Doubling from the threshold, capped. A failure can only be
			// recorded when the key is not locked (Check runs first), so the
			// exponent advances once per elapsed lockout, not once per
			// request.
			a.LockedUntil = now.Add(lockoutFor(a.Failures - k.threshold))
		}
		a.Scope, a.Key = k.scope, k.key
		if err := t.store.SaveAttempt(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

// Succeed clears both counters. A successful login means this username and
// this IP are no longer suspect.
func (t *Throttle) Succeed(ctx context.Context, userKey, ipKey string) error {
	for _, k := range t.keys(userKey, ipKey) {
		if err := t.store.ClearAttempt(ctx, k.scope, k.key); err != nil {
			return err
		}
	}
	return nil
}

// Sweep deletes counters whose lockout has elapsed and whose window is stale.
// Called by the background sweeper alongside expired login sessions (FR-3.7).
func (t *Throttle) Sweep(ctx context.Context) (int64, error) {
	return t.store.DeleteElapsedAttempts(ctx, t.now().Add(-ipWindow))
}

type throttleKey struct {
	scope     string
	key       string
	threshold int
}

// keys returns the counters in play. userKey is empty for registration, which
// is throttled per IP only (FR-7.5).
func (t *Throttle) keys(userKey, ipKey string) []throttleKey {
	out := make([]throttleKey, 0, 2)
	if userKey != "" {
		out = append(out, throttleKey{scope: ScopeUser, key: userKey, threshold: userFailureThreshold})
	}
	if ipKey != "" {
		out = append(out, throttleKey{scope: ScopeIP, key: ipKey, threshold: ipFailureThreshold})
	}
	return out
}

// lockoutFor doubles from baseLockout, capped at maxLockout: 1m, 2m, 4m, 8m,
// then 15m forever.
func lockoutFor(excess int) time.Duration {
	d := baseLockout
	for range excess {
		d *= 2
		if d >= maxLockout {
			return maxLockout
		}
	}
	return d
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/auth/... && go tool golangci-lint run ./internal/auth/...
```

Expected: PASS on all nine throttle test functions.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/auth
git commit -m "$(cat <<'EOF'
feat(auth): add the persisted doubling login throttle

Counters live in the database so a restart does not clear a lockout in
progress (FR-7.1) — TestLockoutSurvivesReopeningTheDatabase is the stated
acceptance criterion. The username counter is consecutive-failure based; the
IP counter is windowed. Both lockout messages are byte-identical so the
response never discloses which key engaged (FR-7.4).

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `provider.Resolver` and the mechanical rewire

This task is the **signature churn** the design flags as risk #1 (§12). It is sequenced early and kept self-contained so every later task builds on stable signatures. It changes no behaviour.

**Files:**
- Create: `apps/backend/internal/provider/resolver.go`
- Test: `apps/backend/internal/provider/resolver_test.go`
- Modify: `apps/backend/internal/review/service.go` (`Deps.Providers`, `Create`), `apps/backend/internal/api/router.go` (`Deps.Providers`), `apps/backend/internal/api/providers.go`, `apps/backend/internal/api/repositories.go`, `apps/backend/internal/app/app.go`
- Modify (mechanically): every test that constructs `review.Deps` or `api.Deps` with a `*provider.Registry`

**Interfaces:**
- Consumes: `identity.Scope` (Task 1).
- Produces:
  ```go
  // internal/provider
  type Resolver interface {
      Resolve(ctx context.Context, scope identity.Scope) (*Registry, error)
  }
  func NewStaticResolver(r *Registry) Resolver
  ```
  `review.Deps.Providers` and `api.Deps.Providers` both become `provider.Resolver`. `app.App` gains `Resolver provider.Resolver` alongside the existing `Registry *provider.Registry`.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/provider/resolver_test.go`:

```go
package provider_test

import (
	"context"
	"testing"

	"github.com/jtumidanski/converge/internal/identity"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
)

// TestStaticResolverIgnoresTheScope is the standalone contract: one registry,
// the same one for everyone, including converge-cli and every existing test.
func TestStaticResolverIgnoresTheScope(t *testing.T) {
	t.Parallel()
	registry := provider.NewRegistry()
	if err := registry.Register(fake.New("gh")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r := provider.NewStaticResolver(registry)
	for _, scope := range []identity.Scope{identity.Standalone(), identity.ForUser("anyone")} {
		got, err := r.Resolve(context.Background(), scope)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got != registry {
			t.Fatal("static resolver returned a different registry")
		}
	}
}
```

> Check `internal/provider/fake/fake.go` for the actual constructor name and signature before writing this; use whatever it exposes.

- [ ] **Step 2: Run to verify it fails**

```bash
cd apps/backend && go test ./internal/provider/...
```

Expected: FAIL to compile — `provider.NewStaticResolver` is undefined.

- [ ] **Step 3: Write `resolver.go`**

Create `apps/backend/internal/provider/resolver.go`:

```go
package provider

import (
	"context"

	"github.com/jtumidanski/converge/internal/identity"
)

// Resolver yields the provider registry visible to a scope.
//
// This exists because in hosted mode the set of providers — and the tokens
// inside them — is a function of the caller, so a process-global
// *Registry no longer describes the system. Standalone mode, converge-cli,
// and every existing test use NewStaticResolver and behave exactly as before.
//
// The hosted implementation lives in internal/auth, not here: it needs the
// database and the decryption key, and this package must gain neither. It
// satisfies this interface from the outside, which is the direction the
// dependency should run.
type Resolver interface {
	Resolve(ctx context.Context, scope identity.Scope) (*Registry, error)
}

type staticResolver struct{ registry *Registry }

// NewStaticResolver wraps a prebuilt registry and ignores the scope.
func NewStaticResolver(r *Registry) Resolver { return staticResolver{registry: r} }

func (s staticResolver) Resolve(context.Context, identity.Scope) (*Registry, error) {
	return s.registry, nil
}
```

- [ ] **Step 4: Rewire `review`**

In `internal/review/service.go`, change `Deps.Providers` to `provider.Resolver` with a comment, and resolve inside `Create`:

```go
	// Providers resolves the registry for the calling scope. In standalone
	// mode this is a static wrapper around the one env-built registry; in
	// hosted mode it is auth.ProviderResolver, which builds and caches a
	// registry per user from that user's encrypted configurations.
	Providers provider.Resolver
```

`Create` gains the scope (its full signature change lands in Task 12; for now resolve with `identity.Standalone()` so this task stays behaviour-neutral):

```go
	registry, err := s.deps.Providers.Resolve(ctx, identity.Standalone())
	if err != nil {
		return session.Session{}, fmt.Errorf("review: resolve providers: %w", err)
	}
	p, ok := registry.Get(in.ProviderID)
```

Do the same at the registry use inside `build`/`resolve` — find every `s.deps.Providers.Get(` and `s.deps.Providers.All(`:

```bash
cd apps/backend && grep -rn 'deps.Providers\.' internal/
```

- [ ] **Step 5: Rewire `api`**

`internal/api/router.go`: `Providers provider.Resolver`.

`internal/api/providers.go` — `listProviders` resolves first:

```go
func (s *server) listProviders(w http.ResponseWriter, r *http.Request) {
	registry, err := s.deps.Providers.Resolve(r.Context(), scopeFrom(r))
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	all := registry.All()
	out := make([]jsonapi.Resource, 0, len(all))
	for _, p := range all {
		out = append(out, providerResource(p))
	}
	if err := jsonapi.WriteList(w, http.StatusOK, out, nil); err != nil {
		s.deps.Log.Error("write providers failed", "error", err)
	}
}
```

`scopeFrom` does not exist yet (Task 13). For this task, add a temporary package-private stub in `internal/api/authctx.go` returning `identity.Standalone()` — Task 13 replaces its body with the real context read and keeps the signature:

```go
// scopeFrom returns the scope the authenticate middleware attached, or the
// standalone scope when none is present. This is the one place identity
// travels on a context (FR-4.5); below api it is an explicit argument.
func scopeFrom(r *http.Request) identity.Scope { return identity.Standalone() }
```

`internal/api/repositories.go` — `providerFor` resolves:

```go
func (s *server) providerFor(w http.ResponseWriter, r *http.Request) (provider.GitProvider, bool) {
	registry, err := s.deps.Providers.Resolve(r.Context(), scopeFrom(r))
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return nil, false
	}
	id := r.PathValue("provider")
	p, ok := registry.Get(id)
	if !ok {
		// 404 rather than 403 even when the slug belongs to another user:
		// resource existence is not disclosed (FR-4.2).
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", jsonapi.StatusTitle(http.StatusNotFound), "No provider is configured with that id.")
		return nil, false
	}
	return p, true
}
```

- [ ] **Step 6: Rewire `app`**

In `internal/app/app.go`, after the registry loop, build the static resolver and pass it:

```go
	// Standalone supplies a static resolver over the one env-built registry.
	// app.New replaces this with auth.ProviderResolver in hosted mode.
	resolver := provider.NewStaticResolver(registry)
	// ... review.NewService(review.Deps{Providers: resolver, ...})
```

Add `Resolver provider.Resolver` to the `App` struct and set it in the returned value.

- [ ] **Step 7: Fix every test call site mechanically and run the full suite**

```bash
cd apps/backend && go build ./... 2>&1 | head -40
```

For each reported error, wrap the registry: `Providers: provider.NewStaticResolver(registry)`. Then:

```bash
cd apps/backend && go vet ./... && go test -race -count=1 ./... && go test -race -count=1 -tags integration ./... && CGO_ENABLED=0 go build ./...
```

Expected: every pre-existing test passes **with no change other than the `NewStaticResolver` wrap**. Per design §10: if any test needs more than that, stop and report it — it is a signal the seam is in the wrong place, and it must be raised rather than papered over.

- [ ] **Step 8: Commit**

```bash
git add apps/backend
git commit -m "$(cat <<'EOF'
refactor(provider): depend on a Resolver instead of a process-global Registry

In hosted mode the set of providers is a function of the caller, so a
process-global *Registry no longer describes the system. Standalone, the CLI,
and every existing test adapt by wrapping their registry in
NewStaticResolver — a mechanical change with no behaviour difference.

Sequenced early and self-contained so later tasks build on stable
signatures (design §12).

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: `session` — the owner field and scoped store

**Files:**
- Modify: `apps/backend/internal/session/model.go`, `builder.go`, `record.go`, `store.go`
- Test: `apps/backend/internal/session/model_test.go`, `record_test.go`, `store_test.go` (new functions only)

**Interfaces:**
- Consumes: `identity.Scope` (Task 1).
- Produces:
  ```go
  func (s Session) Owner() string
  func (b *Builder) SetOwner(v string) *Builder
  // Record gains: Owner string `json:"owner,omitempty"`
  func (s *Store) Get(id string, scope identity.Scope) (Session, bool)
  func (s *Store) List(scope identity.Scope) []Session
  func (s *Store) Finish(ctx context.Context, id string, scope identity.Scope) error
  func (s *Store) Unowned() int
  func (s *Store) Purge(ctx context.Context, userID string) error
  ```
  `Save`, `SaveActive`, `Corrupted`, `LoadAll`, `RunSweeper`, `Root`, `Dir` keep their current signatures.

- [ ] **Step 1: Write the failing tests**

Add to `apps/backend/internal/session/record_test.go`:

```go
// TestOwnerRoundTripsThroughTheRecord is the FR-6.1 contract.
func TestOwnerRoundTripsThroughTheRecord(t *testing.T) {
	sess := buildSession(t) // reuse the file's existing fixture helper
	owned, err := session.NewBuilder().SetID(sess.ID()).SetProviderID(sess.ProviderID()).
		SetRepository(sess.Repository()).SetBaseBranch(sess.BaseBranch()).
		SetRequestedChanges(sess.RequestedChanges()).SetCreatedAt(sess.CreatedAt()).
		SetTTL(time.Hour).SetOwner("abcdef0123456789").Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	record := session.ToRecord(owned)
	if record.Owner != "abcdef0123456789" {
		t.Fatalf("Record.Owner = %q, want the owner", record.Owner)
	}
	back, err := session.FromRecord(record)
	if err != nil {
		t.Fatalf("FromRecord: %v", err)
	}
	if back.Owner() != "abcdef0123456789" {
		t.Fatalf("round-tripped Owner() = %q", back.Owner())
	}
}

// TestRecordWithoutOwnerDecodesToEmpty is the no-migration guarantee (PRD
// §6.3): a session.json written by an older build must parse unchanged.
func TestRecordWithoutOwnerDecodesToEmpty(t *testing.T) {
	sess := buildSession(t)
	blob, err := json.Marshal(session.ToRecord(sess))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Contains(blob, []byte(`"owner"`)) {
		t.Fatalf("an unowned record must omit the owner field: %s", blob)
	}
	var decoded session.Record
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Owner != "" {
		t.Fatalf("Owner = %q, want empty", decoded.Owner)
	}
	if decoded.SchemaVersion != session.SchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", decoded.SchemaVersion, session.SchemaVersion)
	}
}

// TestSchemaVersionIsStillOne: an added optional field that absent-decodes to
// its zero value is not a schema break, and bumping the version would force a
// migration path for records that need none (design §6).
func TestSchemaVersionIsStillOne(t *testing.T) {
	if session.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", session.SchemaVersion)
	}
}
```

Add to `apps/backend/internal/session/store_test.go`:

4. **`TestListIsScopedByOwner`** — save four active sessions: unowned, owned by `userA`, owned by `userA`, owned by `userB`. Assert `List(identity.Standalone())` returns all four; `List(identity.ForUser("userA"))` returns exactly the two A sessions; `List(identity.ForUser("userB"))` returns one; neither hosted list contains the unowned session (FR-6.3, FR-6.4).
5. **`TestGetIsScopedByOwner`** — `Get(idOfA, identity.ForUser("userB"))` returns `false`; `Get(idOfA, identity.ForUser("userA"))` and `Get(idOfA, identity.Standalone())` both return `true`; `Get(unownedID, identity.ForUser("userA"))` returns `false`.
6. **`TestFinishIsScopedByOwner`** — `Finish(ctx, idOfA, identity.ForUser("userB"))` returns nil and leaves A's session **active and its workspace intact**; `Finish(ctx, idOfA, identity.ForUser("userA"))` finishes it.
7. **`TestUnownedCount`** — two unowned and one owned session; `Unowned()` returns 2.
8. **`TestPurgeRemovesEveryOwnedSession`** — save an active and a finished session for `userA` plus one for `userB`; `Purge(ctx, "userA")`; assert both A directories are gone from disk, A is absent from `List(identity.Standalone())`, and B's session and directory survive (FR-2.7).

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/session/...
```

Expected: FAIL to compile — `SetOwner`, `Owner()`, `Record.Owner`, and the scoped signatures are undefined.

- [ ] **Step 3: Add `owner` to the model, builder, and record**

`model.go`: add `owner string` to the `Session` struct and the accessor next to the others:

```go
// Owner is the id of the user who created this review, empty in standalone
// mode. The on-disk layout does not change — WORKSPACE_ROOT/<session-id>/ in
// both modes — because isolation is enforced by the store and the API, not by
// directory nesting, so existing sessions keep resuming after an upgrade
// (FR-6.2).
func (s Session) Owner() string { return s.owner }
```

`builder.go`: `func (b *Builder) SetOwner(v string) *Builder { b.s.owner = v; return b }`. Do **not** make it required in `Build` — an empty owner is the valid standalone value.

`record.go`: add the field, keeping it last so an older record's field order is irrelevant:

```go
	// Owner is absent in records written by a standalone deployment and by
	// builds older than hosted mode. omitempty plus absent-decodes-to-zero is
	// why no data migration is required (PRD §6.3).
	Owner string `json:"owner,omitempty"`
```

Set `Owner: s.owner` in `ToRecord` and `.SetOwner(r.Owner)` in `FromRecord`'s builder chain.

- [ ] **Step 4: Scope the store**

In `store.go`:

```go
// Get returns the session if it exists and is visible to scope. A session
// owned by another user, or an unowned session in hosted mode, is reported
// exactly as a session that does not exist — which is what makes a cross-user
// request answer 404 rather than 403 (FR-4.2), through the existing
// ErrNotFound -> 404 mapping, with no handler making that decision.
func (s *Store) Get(id string, scope identity.Scope) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.index[id]
	if !ok || !scope.Matches(sess.Owner()) {
		return Session{}, false
	}
	return sess, true
}

// List returns the active sessions visible to scope, newest first.
func (s *Store) List(scope identity.Scope) []Session {
	s.mu.RLock()
	out := make([]Session, 0, len(s.index))
	for _, sess := range s.index {
		if sess.IsActive() && scope.Matches(sess.Owner()) {
			out = append(out, sess)
		}
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt().After(out[j].CreatedAt()) })
	return out
}
```

`Finish` takes the scope and delegates the visibility decision to `Get`, so the existing "unknown or already finished is a no-op" contract extends to "not visible" without a new error path:

```go
func (s *Store) Finish(ctx context.Context, id string, scope identity.Scope) error {
	sess, ok := s.Get(id, scope)
	if !ok || sess.Status() == StatusFinished {
		return nil
	}
	// ... the rest of the existing body, unchanged ...
}
```

`Sweep`, `expire`, and `retryCleanup` stay **unscoped**: that is the system acting, not a user, and expiry is not an authorisation decision.

Add:

```go
// Unowned reports how many indexed sessions carry no owner. In hosted mode
// these are invisible to every user (FR-6.3); app.New logs the count once at
// startup so an operator can clean them up by hand. They are never swept
// early and never reassigned.
func (s *Store) Unowned() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, sess := range s.index {
		if sess.Owner() == "" {
			n++
		}
	}
	return n
}

// Purge removes every session owned by userID — active or terminal — along
// with its workspace. This is the account-deletion path (FR-2.7) and is
// deliberately not scope-checked: the caller has already verified the
// account's password. Errors from individual cleanups are joined rather than
// short-circuited, so one stuck workspace does not strand the rest.
func (s *Store) Purge(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("session: purge requires a user id")
	}
	s.mu.Lock()
	ids := make([]string, 0)
	victims := make([]Session, 0)
	for id, sess := range s.index {
		if sess.Owner() == userID {
			ids = append(ids, id)
			victims = append(victims, sess)
		}
	}
	for _, id := range ids {
		delete(s.index, id)
		delete(s.corrupt, id)
	}
	s.mu.Unlock()

	// Cleanup runs outside the lock: it does filesystem and git work and
	// would otherwise serialise the whole store behind it, matching Finish.
	var errs []error
	for _, sess := range victims {
		if err := s.cleaner.Cleanup(ctx, sess); err != nil {
			errs = append(errs, fmt.Errorf("session %s: %w", sess.ID(), err))
			continue
		}
		if err := s.cleaner.RemoveDir(ctx, sess.ID()); err != nil {
			errs = append(errs, fmt.Errorf("session %s: remove dir: %w", sess.ID(), err))
		}
	}
	return errors.Join(errs...)
}
```

> **Implementer note:** read `review.Cleaner.Cleanup` and `RemoveDir` first (`internal/review/cleaner.go`) and confirm `Cleanup` on an already-terminal session is safe and that `RemoveDir` is idempotent. If `Cleanup` already removes the directory for terminal sessions, drop the `RemoveDir` call rather than calling both. Add the private `retryCleanup` registration removal if the id was pending retry.

Add `identity` and `errors` to the imports.

- [ ] **Step 5: Fix call sites and run**

```bash
cd apps/backend && go build ./... 2>&1 | head -40
```

Every `Store.Get`/`List`/`Finish` call site gets an explicit scope. `cmd/converge-cli`, `internal/review`, `internal/api`, and all tests pass `identity.Standalone()` — which is both correct and self-documenting, and is exactly why the signatures changed rather than a `ScopedStore` wrapper being added (design §6).

```bash
cd apps/backend && go vet ./... && go test -race -count=1 ./... && go test -race -count=1 -tags integration ./...
```

Expected: PASS, including every pre-existing session/store/api test.

- [ ] **Step 6: Commit**

```bash
git add apps/backend
git commit -m "$(cat <<'EOF'
feat(session): add an owner to the model and scope the store by it

Get/List/Finish now require an identity.Scope, so no call site can read the
store without declaring on whose behalf. A ScopedStore wrapper was rejected:
it leaves the unscoped methods reachable, and the first direct call loses
isolation with no compile error (design §6).

SchemaVersion stays 1 — an added optional field that absent-decodes to its
zero value is not a schema break, so no data migration is required.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: `mirror` — per-user namespaces

**Files:**
- Modify: `apps/backend/internal/mirror/cache.go`
- Test: `apps/backend/internal/mirror/cache_test.go` (new functions only)

**Interfaces:**
- Consumes: `identity.Scope` (Task 1).
- Produces:
  ```go
  type Namespace struct{ /* unexported */ }
  func RootNamespace() Namespace
  func NamespaceFor(scope identity.Scope) (Namespace, error)
  func (c *Cache) Path(ns Namespace, providerID, fullName string) (string, error)
  func (c *Cache) Ensure(ctx context.Context, ns Namespace, p provider.GitProvider, repo provider.Repository) (string, error)
  func (c *Cache) FetchSHA(ctx context.Context, ns Namespace, p provider.GitProvider, repo provider.Repository, sha string) error
  func (c *Cache) PurgeNamespace(ns Namespace) error
  ```

- [ ] **Step 1: Write the failing tests**

Add to `apps/backend/internal/mirror/cache_test.go`:

1. **`TestRootNamespacePathIsUnchanged`** — `Path(mirror.RootNamespace(), "gh", "atlas/server")` equals `<root>/gh/atlas/server.git`, byte for byte what it was before this task (FR-6.5's standalone half).
2. **`TestScopedNamespaceNestsUnderUsers`** — build `NamespaceFor(identity.ForUser("abcdef0123456789"))`; `Path` equals `<root>/users/abcdef0123456789/gh/atlas/server.git`.
3. **`TestNamespaceForRejectsAMalformedUserID`** — table of `"..", "../../etc", "abc/def", "ABCDEF0123456789", "abcdef012345678", "abcdef01234567890", ""` with a non-empty scope; every one returns an error. **Then** assert directly that no returned path escapes the root:
   ```go
   ns, err := mirror.NamespaceFor(identity.ForUser("../../etc"))
   if err == nil {
       t.Fatal("a traversal user id must be rejected before it reaches a path")
   }
   _ = ns
   ```
   This is the same defensive posture `providerIDRe` and `gitx.ValidateRepoFullName` already take.
4. **`TestTwoUsersGetIndependentPaths`** — two different user ids produce different paths for the same repository, and neither is a prefix of the other.
5. **`TestPurgeNamespaceRemovesOnlyThatUser`** — create `<root>/users/<a>/gh/x.git/HEAD` and `<root>/users/<b>/gh/x.git/HEAD` and `<root>/gh/x.git/HEAD`; `PurgeNamespace(nsA)`; assert A's tree is gone and both others survive (FR-6.6).
6. **`TestPurgeRootNamespaceIsRefused`** — `PurgeNamespace(mirror.RootNamespace())` returns an error and deletes nothing. Deleting the whole cache root is never what account deletion means, and an accidental zero-value `Namespace` must not be able to do it.

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/mirror/...
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement**

In `apps/backend/internal/mirror/cache.go`:

```go
// userIDRe is what auth.NewID produces: 16 lowercase hex characters. A user
// id becomes a directory name under the cache root, so it is validated
// against the exact shape it is generated in — the same posture providerIDRe
// and gitx.ValidateRepoFullName already take — rather than trusted because it
// came from an authenticated session.
var userIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Namespace isolates one user's mirrors under the cache root.
//
// The zero value is the root namespace: mirrors live directly under the root,
// exactly as they did before hosted mode existed, which is why an existing
// deployment's mirrors are still found after an upgrade (FR-6.5).
type Namespace struct {
	userID string
}

// RootNamespace is the standalone namespace: today's flat layout.
func RootNamespace() Namespace { return Namespace{} }

// NamespaceFor derives the namespace for a scope. A standalone scope yields
// the root namespace; a scoped one yields users/<user-id>.
func NamespaceFor(scope identity.Scope) (Namespace, error) {
	id := scope.UserID()
	if id == "" {
		return Namespace{}, nil
	}
	if !userIDRe.MatchString(id) {
		return Namespace{}, fmt.Errorf("mirror: invalid user id")
	}
	return Namespace{userID: id}, nil
}

// IsRoot reports whether this is the unscoped namespace.
func (n Namespace) IsRoot() bool { return n.userID == "" }

func (n Namespace) segments() []string {
	if n.userID == "" {
		return nil
	}
	return []string{"users", n.userID}
}

// Path returns <root>[/users/<user-id>]/<providerID>/<fullName>.git after
// validation.
//
// Two users reviewing the same repository therefore maintain two mirrors.
// That trades disk for a guarantee that mirror contents cannot cross an
// account boundary. No per-user cap ships in this task (design §6).
func (c *Cache) Path(ns Namespace, providerID, fullName string) (string, error) {
	if !providerIDRe.MatchString(providerID) {
		return "", fmt.Errorf("mirror: invalid provider id %q", providerID)
	}
	if err := gitx.ValidateRepoFullName(fullName); err != nil {
		return "", fmt.Errorf("mirror: %w", err)
	}
	parts := append([]string{c.root}, ns.segments()...)
	parts = append(parts, providerID)
	parts = append(parts, strings.Split(fullName, "/")...)
	parts[len(parts)-1] += ".git"
	return filepath.Join(parts...), nil
}

// NamespaceRoot returns the directory holding every mirror in ns.
func (c *Cache) NamespaceRoot(ns Namespace) (string, error) {
	if ns.IsRoot() {
		return "", errors.New("mirror: the root namespace has no dedicated directory")
	}
	return filepath.Join(append([]string{c.root}, ns.segments()...)...), nil
}

// PurgeNamespace removes a user's entire mirror tree. Called when an account
// is deleted (FR-2.7, FR-6.6). The root namespace is refused: deleting the
// whole cache root is never what account deletion means, and a zero-value
// Namespace reaching here would otherwise wipe every user's mirrors.
func (c *Cache) PurgeNamespace(ns Namespace) error {
	dir, err := c.NamespaceRoot(ns)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("mirror: purge namespace: %w", err)
	}
	return nil
}
```

`Ensure` and `FetchSHA` take `ns Namespace` as their second parameter and pass it to `Path`. Nothing else in either function changes:

- The per-mirror lock key is the resolved path, so two users' mirrors of the same repository lock independently — which is what we want.
- `Ensure`'s fetch refspec, including `gitx.ExcludeReviewRefspec` from commit `b8a8391`, is untouched. Namespacing changes **which directory** the refspec applies to, not the refspec, so the live-review-branch protection holds by construction (FR-6.6). Task 21 adds a hosted-mode integration test anyway, because "holds by construction" is a claim, not evidence.

Add `errors`, `os`, and `identity` to the imports.

- [ ] **Step 4: Fix call sites and run**

```bash
cd apps/backend && go build ./... 2>&1 | head -20
```

Three non-test call sites (`review/cleaner.go:40`, `review/resolve.go:103`, `review/resolve.go:210`) plus `mirror`'s own tests. Task 12 gives them real namespaces; for now pass `mirror.RootNamespace()` so this task stays behaviour-neutral.

```bash
cd apps/backend && go vet ./... && go test -race -count=1 ./... && go test -race -count=1 -tags integration ./...
```

- [ ] **Step 5: Commit**

```bash
git add apps/backend
git commit -m "$(cat <<'EOF'
feat(mirror): namespace mirrors per user in hosted mode

The zero-value Namespace is today's flat layout, so an existing deployment's
mirrors are still found after an upgrade. A user id becomes a directory name,
so it is validated against the exact 16-hex shape auth.NewID produces rather
than trusted because it arrived on an authenticated request.

PurgeNamespace refuses the root namespace: a zero-value Namespace reaching it
would otherwise wipe every user's mirrors.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: `review` — thread the scope through the pipeline

**Files:**
- Modify: `apps/backend/internal/review/service.go`, `resolve.go`, `cleaner.go`
- Test: `apps/backend/internal/review/service_test.go` (new functions only)

**Interfaces:**
- Consumes: `identity.Scope`, `mirror.Namespace`, `provider.Resolver`, scoped `session.Store` (Tasks 1, 9, 10, 11).
- Produces:
  ```go
  func (s *Service) Create(ctx context.Context, scope identity.Scope, in CreateInput) (session.Session, error)
  func (s *Service) Get(id string, scope identity.Scope) (session.Session, bool)
  func (s *Service) List(scope identity.Scope) []session.Session
  func (s *Service) Finish(ctx context.Context, id string, scope identity.Scope) error
  func (s *Service) Files(id string, scope identity.Scope) ([]diff.FileSummary, error)
  func (s *Service) FileDiff(ctx context.Context, id, path string, scope identity.Scope) (diff.FileDiff, error)
  func (s *Service) CombinedDiffPath(id string, scope identity.Scope) (string, error)
  func (s *Service) PurgeUser(ctx context.Context, userID string) error   // implements auth.Purger
  func (s *Service) ProviderInUse(scope identity.Scope, providerSlug string) bool // implements auth.ProviderUsage
  ```
  `StartBuild`, `Build`, and `Corrupted` keep their signatures.
  `(*Resolver) Resolve` gains `ns mirror.Namespace` after `ctx`.

- [ ] **Step 1: Write the failing tests**

Add to `apps/backend/internal/review/service_test.go`:

1. **`TestCreateRecordsTheCallersOwner`** — `Create(ctx, identity.ForUser("abcdef0123456789"), in)`; assert the returned session's `Owner()` is that id and that the persisted `session.json` contains `"owner"`.
2. **`TestCreateInStandaloneLeavesOwnerEmpty`** — `Create(ctx, identity.Standalone(), in)`; `Owner()` is `""` and the written `session.json` has no `owner` key (FR-6.1).
3. **`TestCreateResolvesTheCallersRegistry`** — a fake `provider.Resolver` that returns registry A for `userA` and registry B for `userB`; `Create` with `userB`'s scope and a provider id present only in A returns an `*review.InputError` with `CodeInvalidProvider`. This is the seam that makes another user's slug a 404 rather than a cross-user read.
4. **`TestBuildUsesTheSessionsOwnerNotTheRequestScope`** — the important one. Create an owned session, then `Build(ctx, id)` **with no scope argument at all**, and assert the mirror directory that appeared is under `<cacheRoot>/users/<owner>/`. A build outlives the request that triggered it, so it cannot take the request's scope; it must read the owner back off the persisted session.
5. **`TestListAndGetAreScoped`** — thin wrappers over Task 10's store behaviour; two owners, assert each sees only their own.
6. **`TestProviderInUseSeesOnlyNonTerminalSessions`** — an active session on slug `gh` for `userA` makes `ProviderInUse(identity.ForUser(userA), "gh")` true; after `Finish`, false; another user's active session on `gh` does not make it true for `userA` (FR-5.7).
7. **`TestPurgeUserRemovesSessionsAndTheMirrorNamespace`** — an owned session with a materialised mirror under `users/<id>/`; `PurgeUser(ctx, id)`; assert the session directory and the mirror namespace directory are both gone and another user's are untouched (FR-2.7, FR-6.6).

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/review/...
```

Expected: FAIL to compile.

- [ ] **Step 3: Thread the scope**

`service.go` — the delegating methods gain the scope and pass it straight through:

```go
func (s *Service) Get(id string, scope identity.Scope) (session.Session, bool) {
	return s.deps.Store.Get(id, scope)
}

func (s *Service) List(scope identity.Scope) []session.Session { return s.deps.Store.List(scope) }

func (s *Service) Finish(ctx context.Context, id string, scope identity.Scope) error {
	if err := s.deps.Store.Finish(ctx, id, scope); err != nil {
		return fmt.Errorf("review: finish %s: %w", id, err)
	}
	return nil
}
```

`Files`, `FileDiff`, and `CombinedDiffPath` each start with a `Get` — pass the scope into it, so an invisible session yields the same "not found" they already produce for an unknown id.

`Create` takes the scope, resolves the caller's registry, and stamps the owner:

```go
func (s *Service) Create(ctx context.Context, scope identity.Scope, in CreateInput) (session.Session, error) {
	in, err := in.Validate(ctx, s.deps.Runner)
	if err != nil {
		return session.Session{}, err
	}
	registry, err := s.deps.Providers.Resolve(ctx, scope)
	if err != nil {
		return session.Session{}, fmt.Errorf("review: resolve providers: %w", err)
	}
	p, ok := registry.Get(in.ProviderID)
	if !ok {
		return session.Session{}, &InputError{
			Code:    CodeInvalidProvider,
			Field:   "provider",
			Message: fmt.Sprintf("Provider %q is not configured.", in.ProviderID),
		}
	}
	// ... existing GetRepository / default-branch / NewID logic, unchanged ...
	sess, err := session.NewBuilder().SetID(id).SetProviderID(p.ID()).SetRepository(repo.FullName()).
		SetBaseBranch(in.BaseBranch).SetRequestedChanges(in.Changes).
		SetCreatedAt(s.deps.Now()).SetTTL(s.deps.SessionTTL).
		// Empty in standalone mode; the creating user's id in hosted mode
		// (FR-6.1). This is the only place an owner is ever assigned.
		SetOwner(scope.UserID()).
		Build()
	// ... unchanged ...
}
```

`build` derives the scope **from the session**, not from a caller:

```go
// scopeOf returns the scope a background build acts under. StartBuild
// deliberately outlives the HTTP request that triggered it, so the request's
// scope is gone by the time build runs; the owner persisted on the session is
// the durable answer. An unowned session (standalone, or a record from before
// hosted mode) yields the standalone scope, so its mirrors stay where they
// have always been.
func scopeOf(sess session.Session) identity.Scope {
	if owner := sess.Owner(); owner != "" {
		return identity.ForUser(owner)
	}
	return identity.Standalone()
}
```

and inside `build`:

```go
	scope := scopeOf(*live)
	ns, err := mirror.NamespaceFor(scope)
	if err != nil {
		return session.Session{}, fmt.Errorf("review: build %s: %w", live.ID(), err)
	}
	registry, err := s.deps.Providers.Resolve(ctx, scope)
	if err != nil {
		return session.Session{}, fmt.Errorf("review: build %s: resolve providers: %w", live.ID(), err)
	}
	p, ok := registry.Get(live.ProviderID())
	// ... then pass ns into s.resolver.Resolve(ctx, ns, p, repo, ...)
```

`resolve.go` — `Resolver.Resolve` and `landingWithFetch` take `ns mirror.Namespace` and pass it to `r.mirrors.Ensure(ctx, ns, p, repo)` and `r.mirrors.FetchSHA(ctx, ns, p, repo, sha)`.

`cleaner.go` — `Cleanup` derives the namespace from the session itself, so it needs no new parameter:

```go
func (c *Cleaner) Cleanup(ctx context.Context, s session.Session) error {
	ns, err := mirror.NamespaceFor(scopeOf(s))
	if err != nil {
		return fmt.Errorf("cleaner: %w", err)
	}
	mirrorPath, err := c.mirrors.Path(ns, s.ProviderID(), s.Repository())
	// ... unchanged ...
}
```

Add the two collaborator implementations:

```go
// PurgeUser implements auth.Purger. It removes every review session owned by
// userID (workspaces included) and then that user's whole mirror namespace.
//
// Called by auth.Service.DeleteAccount *after* the database rows are gone, so
// a failure here leaves orphaned bytes on disk rather than an account that has
// lost its data but can still log in (design §6).
func (s *Service) PurgeUser(ctx context.Context, userID string) error {
	var errs []error
	if err := s.deps.Store.Purge(ctx, userID); err != nil {
		errs = append(errs, err)
	}
	ns, err := mirror.NamespaceFor(identity.ForUser(userID))
	if err != nil {
		errs = append(errs, err)
	} else if err := s.deps.Mirrors.PurgeNamespace(ns); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ProviderInUse implements auth.ProviderUsage: it reports whether the scope
// has a review session in a non-terminal state referencing providerSlug
// (FR-5.7). Store.List returns only active sessions, which is exactly the
// question being asked, so a completed review does not block a delete.
func (s *Service) ProviderInUse(scope identity.Scope, providerSlug string) bool {
	for _, sess := range s.deps.Store.List(scope) {
		if sess.ProviderID() == providerSlug {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Fix call sites and run**

```bash
cd apps/backend && go build ./... 2>&1 | head -40
```

`internal/api` and `cmd/converge-cli` pass `identity.Standalone()`; Task 16 replaces the API's with `scopeFrom(r)`.

```bash
cd apps/backend && go vet ./... && go test -race -count=1 ./... && go test -race -count=1 -tags integration ./... && go tool golangci-lint run
```

- [ ] **Step 5: Commit**

```bash
git add apps/backend
git commit -m "$(cat <<'EOF'
feat(review): thread the caller's scope through the resolve pipeline

Create stamps the owner and resolves the caller's own registry. Build
deliberately does not take a scope: it outlives the request that triggered it,
so it reads the owner back off the persisted session — scopeOf is the single
place that conversion happens.

PurgeUser and ProviderInUse implement auth.Purger and auth.ProviderUsage from
this side of the boundary, which is how the account-deletion cascade spans
three stores without auth importing review or review importing auth.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: `auth` — credential verification and the per-user provider resolver

**Files:**
- Create: `apps/backend/internal/auth/verify.go`
- Create: `apps/backend/internal/auth/resolver.go`
- Test: `apps/backend/internal/auth/resolver_test.go`

**Interfaces:**
- Consumes: `auth.Store`, `auth.Sealer`, `auth.UserProvider` (Tasks 4, 6, 7); `provider.Resolver`, `provider.Registry`, `provider.NewRegistry` (Task 9); `github.New`, `gitlab.New`; `identity.Scope`.
- Produces:
  ```go
  type ProviderVerifier interface {
      Verify(ctx context.Context, kind config.Kind, baseURL string, token config.Secret) error
  }
  func NewHTTPVerifier(client *http.Client, now func() time.Time) ProviderVerifier

  type ProviderResolver struct{ /* unexported */ }
  func NewProviderResolver(store *Store, sealer *Sealer, client *http.Client, now func() time.Time) *ProviderResolver
  func (r *ProviderResolver) Resolve(ctx context.Context, scope identity.Scope) (*provider.Registry, error)
  func (r *ProviderResolver) Invalidate(userID string)
  var _ provider.Resolver = (*ProviderResolver)(nil)
  ```
  `providerResolverTTL = 15 * time.Minute`.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/auth/resolver_test.go` with these functions:

1. **`TestResolveBuildsARegistryFromTheUsersConfigurations`** — seed two configs for one user (one `github`, one `gitlab`) via `Store.CreateUserProvider` with tokens sealed by a real `Sealer`; `Resolve(ctx, identity.ForUser(u.ID()))` returns a registry whose `All()` has two entries, with `ID()` equal to the **slugs**, `Kind()`/`DisplayName()`/`BaseURL()` matching the stored rows.
2. **`TestResolveIsolatesUsers`** — two users with one config each; each `Resolve` sees exactly one provider, and neither registry contains the other's slug (the acceptance criterion "a second user's `GET /api/providers` does not include the first user's providers").
3. **`TestResolveReturnsAnEmptyRegistryForAUserWithNoProviders`** — `All()` is empty and no error (FR-5.9).
4. **`TestResolveRejectsAnUnscopedScope`** — `Resolve(ctx, identity.Standalone())` returns a non-nil error. Hosted mode never resolves unscoped; failing loudly beats silently returning either nothing or everything.
5. **`TestResolveCachesAndInvalidate`** — instrument the store by resolving once, adding a third config directly through `Store.CreateUserProvider`, resolving again and asserting the **stale** two-entry registry comes back; then `Invalidate(userID)` and assert the third appears. This pins both halves of the contract.
6. **`TestCacheExpiresAfterTheTTL`** — with a settable clock, resolve, advance 16 minutes, add a config, resolve, and assert the new config appears without an explicit `Invalidate`. Entries expire so an idle user's decrypted tokens do not sit in memory forever.
7. **`TestResolveSurfacesADecryptFailure`** — corrupt one row's `token_ciphertext` via a direct `UPDATE` in the test, then `Resolve` returns an error mentioning neither the ciphertext nor the token.
8. **`TestAStaleRegistryPointerStaysUsable`** — resolve, hold the pointer, `Invalidate`, resolve again, and assert the first pointer's `All()` still works. A `*Registry` is immutable after construction, so a reader holding a stale pointer finishes its request against the registry it resolved.
9. **`TestHTTPVerifierMapsAuthFailure`** — stand up an `httptest.Server` returning `401`; `Verify` returns an `*auth.Error` with `CodeProviderUnauthorized`. A `200` with an empty list returns nil.

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/auth/... -run 'Resolve|Verifier|Cache'
```

Expected: FAIL to compile.

- [ ] **Step 3: Write `verify.go`**

```go
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/github"
	"github.com/jtumidanski/converge/internal/provider/gitlab"
)

// ProviderVerifier checks a credential against the provider API before a
// configuration is written (FR-5.6). Injected so a test can assert the
// validate=true path without reaching the network.
type ProviderVerifier interface {
	Verify(ctx context.Context, kind config.Kind, baseURL string, token config.Secret) error
}

type httpVerifier struct {
	client *http.Client
	now    func() time.Time
}

// NewHTTPVerifier verifies by listing one repository: the cheapest call that
// exercises authentication on both providers.
func NewHTTPVerifier(client *http.Client, now func() time.Time) ProviderVerifier {
	return &httpVerifier{client: client, now: now}
}

func (v *httpVerifier) Verify(ctx context.Context, kind config.Kind, baseURL string, token config.Secret) error {
	p, err := buildClient("verify", "verify", kind, baseURL, token, v.client, v.now)
	if err != nil {
		return err
	}
	if _, err := p.ListRepositories(ctx, provider.Page{Number: 1, Size: 1}.Normalize()); err != nil {
		if errors.Is(err, provider.ErrAuth) {
			return &Error{
				Code:    CodeProviderUnauthorized,
				Message: "The provider rejected that access token.",
			}
		}
		// A network or availability failure is not an authorisation verdict,
		// so it is reported as itself rather than as a bad credential.
		return fmt.Errorf("auth: verify provider credential: %w", err)
	}
	return nil
}

// buildClient is the single place a provider client is constructed from a
// kind, so the resolver and the verifier cannot drift.
func buildClient(id, displayName string, kind config.Kind, baseURL string, token config.Secret, client *http.Client, now func() time.Time) (provider.GitProvider, error) {
	switch kind {
	case config.KindGitHub:
		return github.New(id, displayName, baseURL, token, client, now), nil
	case config.KindGitLab:
		return gitlab.New(id, displayName, baseURL, token, client), nil
	default:
		return nil, fmt.Errorf("auth: unknown provider kind %q", kind)
	}
}
```

> **Implementer note:** confirm the exact `github.New` / `gitlab.New` signatures in `internal/provider/github/client.go` and `internal/provider/gitlab/client.go` before writing this (`app.New` currently calls `github.New(pc.ID, pc.DisplayName, pc.BaseURL, pc.Token, httpClient, time.Now)` and `gitlab.New(pc.ID, pc.DisplayName, pc.BaseURL, pc.Token, httpClient)`), and match them.

- [ ] **Step 4: Write `resolver.go`**

```go
package auth

// providerResolverTTL bounds how long a user's decrypted tokens sit in
// memory. Tokens are decrypted at registry-build time and live inside
// config.Secret values held by the github/gitlab clients — the same place
// standalone mode keeps them. Decrypting per call instead buys little (the
// plaintext still reaches GIT_CONFIG_VALUE_0 and an HTTP header) and costs a
// database read on every provider operation, so we take the cache and bound
// it with this TTL (design §4).
const providerResolverTTL = 15 * time.Minute

// ProviderResolver builds a *provider.Registry from a user's stored
// user_providers rows, decrypting each token on the way.
//
// It lives here rather than in internal/provider because it needs the
// database and the decryption key, and internal/provider must gain neither.
// It satisfies provider.Resolver from the outside.
//
// Cache invalidation is complete because the cache is per-process and this
// process is the only writer of the database — there is no second node to
// miss the message. That assumption would need revisiting for a
// multi-replica deployment (design §12).
type ProviderResolver struct {
	store  *Store
	sealer *Sealer
	client *http.Client
	now    func() time.Time

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	registry *provider.Registry
	builtAt  time.Time
}

func NewProviderResolver(store *Store, sealer *Sealer, client *http.Client, now func() time.Time) *ProviderResolver {
	return &ProviderResolver{
		store:  store,
		sealer: sealer,
		client: client,
		now:    now,
		cache:  map[string]cacheEntry{},
	}
}

var _ provider.Resolver = (*ProviderResolver)(nil)

// Resolve returns the registry visible to scope, building it on a cache miss.
func (r *ProviderResolver) Resolve(ctx context.Context, scope identity.Scope) (*provider.Registry, error) {
	userID := scope.UserID()
	if userID == "" {
		// Hosted mode always resolves on behalf of an authenticated user.
		// An unscoped resolve is a wiring bug; failing loudly beats
		// silently answering with nothing or with everything.
		return nil, errors.New("auth: provider resolver requires a scoped identity")
	}
	now := r.now()
	r.mu.RLock()
	entry, ok := r.cache[userID]
	r.mu.RUnlock()
	if ok && now.Sub(entry.builtAt) < providerResolverTTL {
		return entry.registry, nil
	}
	registry, err := r.build(ctx, userID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cache[userID] = cacheEntry{registry: registry, builtAt: now}
	r.mu.Unlock()
	return registry, nil
}

// Invalidate drops a user's cached registry. Every write path calls it:
// provider create, update, delete, and account deletion.
func (r *ProviderResolver) Invalidate(userID string) {
	r.mu.Lock()
	delete(r.cache, userID)
	r.mu.Unlock()
}

// build decrypts each configuration and registers a client under its slug, so
// the slug is the provider id the rest of the system sees — which is what
// keeps GET /api/providers and /api/providers/{provider}/... unchanged in
// shape (FR-5.8).
func (r *ProviderResolver) build(ctx context.Context, userID string) (*provider.Registry, error) {
	rows, err := r.store.ListUserProviders(ctx, userID)
	if err != nil {
		return nil, err
	}
	registry := provider.NewRegistry()
	for _, row := range rows {
		token, err := r.sealer.Open(row.TokenCiphertext(), row.TokenNonce(), row.UserID(), row.ID())
		if err != nil {
			return nil, fmt.Errorf("auth: provider %s: %w", row.Slug(), err)
		}
		p, err := buildClient(row.Slug(), row.DisplayName(), row.Kind(), row.BaseURL(), token, r.client, r.now)
		if err != nil {
			return nil, err
		}
		if err := registry.Register(p); err != nil {
			return nil, fmt.Errorf("auth: register provider %s: %w", row.Slug(), err)
		}
	}
	return registry, nil
}
```

Imports: `context`, `errors`, `fmt`, `net/http`, `sync`, `time`, `identity`, `provider`.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/auth/... && go tool golangci-lint run ./internal/auth/...
```

Expected: PASS on all nine functions. The `-race` flag matters here: this is the first concurrent-access structure in the package.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/auth
git commit -m "$(cat <<'EOF'
feat(auth): add the per-user provider resolver and credential verifier

The resolver satisfies provider.Resolver from the outside, which keeps the
database and the decryption key out of internal/provider. Registries are
cached per user, invalidated on every write, and expired after 15 minutes so
an idle user's decrypted tokens do not sit in memory indefinitely. A
*Registry is immutable after construction, so a reader holding a stale
pointer through an invalidation finishes its request safely.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: `auth/service.go` — the account lifecycle

**Files:**
- Create: `apps/backend/internal/auth/service.go`
- Test: `apps/backend/internal/auth/service_test.go`

**Interfaces:**
- Consumes: everything from Tasks 4–8 and 13.
- Produces:
  ```go
  type Purger interface { PurgeUser(ctx context.Context, userID string) error }
  type ProviderUsage interface { ProviderInUse(scope identity.Scope, providerSlug string) bool }

  type ServiceDeps struct {
      Store      *Store
      Sealer     *Sealer
      Throttle   *Throttle
      Resolver   *ProviderResolver
      Verifier   ProviderVerifier
      Purger     Purger
      Usage      ProviderUsage
      Log        *slog.Logger
      Now        func() time.Time
      SessionTTL time.Duration // absolute expiry (LOGIN_SESSION_TTL_HOURS)
      IdleTTL    time.Duration // idle expiry (LOGIN_SESSION_IDLE_HOURS)
  }
  func NewService(d ServiceDeps) *Service

  type Credentials struct{ Username, Password string }

  func (s *Service) Register(ctx context.Context, c Credentials, clientIP string) (User, string, error)
  func (s *Service) Login(ctx context.Context, c Credentials, clientIP string) (User, string, error)
  func (s *Service) Logout(ctx context.Context, token string) error
  func (s *Service) Authenticate(ctx context.Context, token string) (identity.Scope, []byte, error)
  func (s *Service) User(ctx context.Context, userID string) (User, error)
  func (s *Service) ProviderCount(ctx context.Context, userID string) (int, error)
  func (s *Service) ChangePassword(ctx context.Context, userID string, keep []byte, current, next string) error
  func (s *Service) DeleteAccount(ctx context.Context, userID, password string) error
  func (s *Service) CountUsers(ctx context.Context) (int, error)
  func (s *Service) Ping(ctx context.Context) error
  ```
  The second return of `Register`/`Login` is the **plaintext** cookie token. The second return of `Authenticate` is the token hash, which the middleware stashes so `ChangePassword` can keep the calling session.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/auth/service_test.go`. A `newService(t)` helper builds a migrated store, a `Sealer` over `key32`, a `Throttle` on a `*clock`, a `ProviderResolver`, a stub `Verifier` that always succeeds, and stub `Purger`/`Usage` that record calls. Test functions:

1. **`TestRegisterThenLoginRoundTrip`** — register `"alice"`; assert a non-empty token, `Authenticate(token)` yields `identity.ForUser(user.ID())`; `Logout(token)`; `Authenticate(token)` now returns `CodeUnauthenticated`; `Login` with the same credentials succeeds with a **different** token.
2. **`TestRegisterRejectsACaseInsensitiveDuplicate`** — `USERNAME_TAKEN` (FR-2.4).
3. **`TestRegisterValidatesInput`** — a 2-character username gives `INVALID_USERNAME`; a 7-character password gives `WEAK_PASSWORD`; an 8-character one succeeds (acceptance criteria).
4. **`TestLoginIsIndistinguishableBetweenUnknownUserAndWrongPassword`** — both return an `*auth.Error` with byte-identical `Code` **and** `Message` (FR-2.5, acceptance criterion). Additionally assert both paths take a comparable amount of time: measure each 3 times and assert neither median is less than half the other. Keep the tolerance loose — this asserts "the dummy hash is actually being computed", not a precise timing bound.
5. **`TestLoginEngagesTheThrottle`** — six wrong-password logins; the sixth returns `CodeAccountLocked` with a positive `RetryAfter`; advance the clock a minute and a correct login succeeds and clears the counter.
6. **`TestRegistrationIsThrottledPerIP`** — 20 failed registrations from one IP (duplicate usernames), then the 21st returns `CodeAccountLocked` (FR-7.5).
7. **`TestAuthenticateEnforcesAbsoluteExpiry`** — register, advance past `SessionTTL`, `Authenticate` returns `CodeUnauthenticated` **and** the row is gone (expiry enforced on read, before any sweep — FR-3.7).
8. **`TestAuthenticateEnforcesIdleExpiry`** — register, advance past `IdleTTL` but not `SessionTTL`; same assertions.
9. **`TestAuthenticateRateLimitsLastSeenWrites`** — register, `Authenticate` twice within five minutes, assert `LastSeenAt` did not move; advance six minutes, `Authenticate`, assert it did (FR-3.5).
10. **`TestChangePasswordRevokesOtherSessionsButNotTheCaller`** — log in three times for the same user; `ChangePassword` with session 1's hash as `keep`; assert `Authenticate` on token 1 still works, tokens 2 and 3 do not, the old password no longer logs in, and the new one does (FR-2.6, acceptance criterion).
11. **`TestChangePasswordRequiresTheCurrentPassword`** — wrong current gives `INVALID_CREDENTIALS`; a 7-character new password gives `WEAK_PASSWORD` and does not change the stored hash.
12. **`TestDeleteAccountRemovesRowsAndCallsThePurger`** — register, add a provider config, log in twice; `DeleteAccount` with the correct password; assert both login sessions no longer authenticate, `UserByID` is `ErrNotFound`, the provider rows are gone, `Invalidate` was called, and the stub `Purger` recorded the user id.
13. **`TestDeleteAccountRequiresThePassword`** — wrong password gives `INVALID_CREDENTIALS` and **nothing** is deleted.
14. **`TestDeleteAccountSucceedsEvenWhenThePurgerFails`** — a `Purger` that returns an error; `DeleteAccount` returns nil, the rows are gone, and the stub log recorded an `ERROR`. Reversing this would risk an account that has lost its data but can still log in, which is strictly worse (design §6).
15. **`TestConcurrentLoginAndProviderListDoNotDeadlock`** — design §12's named risk. `Service` does not yet expose a provider list method (that arrives in Task 15), so alternate 8 goroutines between `Login` (Argon2id) and `Store.ListUserProviders` directly — the same single-connection read Task 15's `Service.ListProviders` will wrap — with a 30s ceiling; assert all complete. This is what proves hashing never happens while the single connection is held by a transaction.

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/auth/... -run 'Register|Login|Authenticate|ChangePassword|DeleteAccount|Concurrent'
```

Expected: FAIL to compile.

- [ ] **Step 3: Write `service.go`**

```go
package auth

// hashConcurrency bounds simultaneous Argon2id operations.
//
// Each costs 64 MiB, so five concurrent registrations is 320 MiB transient.
// The per-IP throttle (FR-7.5) is a counter, not a concurrency limiter, so it
// cannot bound that. This semaphore turns a memory spike into latency: callers
// beyond it queue (design §5).
const hashConcurrency = 4

// lastSeenInterval rate-limits last_seen_at writes, so read-heavy traffic does
// not generate a write per request (FR-3.5).
const lastSeenInterval = 5 * time.Minute

// tokenBytes is the login session token length (FR-3.1).
const tokenBytes = 32

// Purger removes a user's filesystem-resident state. Implemented in
// internal/review (which already owns Cleaner) and wired in internal/app.
//
// This indirection exists because the account-deletion cascade spans three
// stores and no existing package may own all three: auth must not import
// review (wrong direction) and review must not import auth (that would drag
// SQLite into the review tier).
type Purger interface {
	PurgeUser(ctx context.Context, userID string) error
}

// ProviderUsage reports whether a user has a non-terminal review session
// referencing a provider slug (FR-5.7). Implemented in internal/review for the
// same reason as Purger.
type ProviderUsage interface {
	ProviderInUse(scope identity.Scope, providerSlug string) bool
}

// invalidCredentials is byte-identical for an unknown username and a wrong
// password (FR-2.5).
const invalidCredentialsMessage = "The username or password is incorrect."

type ServiceDeps struct { /* as in Interfaces above */ }

type Service struct {
	deps    ServiceDeps
	hashSem chan struct{}
}

func NewService(d ServiceDeps) *Service {
	return &Service{deps: d, hashSem: make(chan struct{}, hashConcurrency)}
}

// hash and verify are the only two places Argon2id runs.
//
// ORDERING DISCIPLINE, do not break: neither may be called while a database
// transaction is open. The pool has exactly one connection, so hashing inside
// a transaction blocks every other query for ~50 ms per call. Every caller
// below hashes or verifies first and touches the database afterwards.
func (s *Service) hash(password string) (string, error) {
	s.hashSem <- struct{}{}
	defer func() { <-s.hashSem }()
	return HashPassword(password)
}

func (s *Service) verify(encoded, password string) error {
	s.hashSem <- struct{}{}
	defer func() { <-s.hashSem }()
	return VerifyPassword(encoded, password)
}

// newToken returns the plaintext cookie value and its SHA-256. Only the hash
// is ever persisted (FR-3.3); the plaintext exists in the response that
// creates it and in the client's cookie jar, nowhere else, and is never
// logged.
func newToken() (string, []byte, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("auth: generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Register creates an account and logs the caller in. Registration is open:
// any client that can reach the instance may create an account (FR-2.4). It
// necessarily discloses username availability; that is accepted and
// documented.
func (s *Service) Register(ctx context.Context, c Credentials, clientIP string) (User, string, error) {
	// Registration is throttled per IP only — there is no username counter to
	// consult before the account exists (FR-7.5).
	if err := s.deps.Throttle.Check(ctx, "", clientIP); err != nil {
		return User{}, "", err
	}
	if err := ValidateUsername(c.Username); err != nil {
		return User{}, "", err
	}
	if err := ValidatePassword(c.Password); err != nil {
		return User{}, "", err
	}
	hashed, err := s.hash(c.Password) // before any transaction
	if err != nil {
		return User{}, "", err
	}
	id, err := NewID()
	if err != nil {
		return User{}, "", err
	}
	now := s.deps.Now()
	user, err := NewUser(id, c.Username, hashed, now)
	if err != nil {
		return User{}, "", err
	}
	if err := s.deps.Store.CreateUser(ctx, user); err != nil {
		// A taken username counts against the IP throttle: it is the shape a
		// flooding attempt takes.
		if failErr := s.deps.Throttle.Fail(ctx, "", clientIP); failErr != nil {
			s.deps.Log.Warn("record registration failure", slog.String("error", failErr.Error()))
		}
		return User{}, "", err
	}
	token, err := s.startSession(ctx, user.ID(), now)
	if err != nil {
		return User{}, "", err
	}
	s.deps.Log.Info("account registered", slog.String("user_id", user.ID()))
	return user, token, nil
}

// Login verifies the password and creates a login session.
func (s *Service) Login(ctx context.Context, c Credentials, clientIP string) (User, string, error) {
	fold := Fold(c.Username)
	if err := s.deps.Throttle.Check(ctx, fold, clientIP); err != nil {
		return User{}, "", err
	}
	user, lookupErr := s.deps.Store.UserByFold(ctx, fold)
	// On an unknown username, verify against the package dummy hash anyway so
	// response timing does not distinguish "no such user" from "wrong
	// password" (FR-2.5). Both paths then return the same error.
	encoded := dummyHash
	if lookupErr == nil {
		encoded = user.PasswordHash()
	}
	verifyErr := s.verify(encoded, c.Password)
	if lookupErr != nil || verifyErr != nil {
		if failErr := s.deps.Throttle.Fail(ctx, fold, clientIP); failErr != nil {
			s.deps.Log.Warn("record login failure", slog.String("error", failErr.Error()))
		}
		// user_id is deliberately absent: on the unknown-username path there
		// is none, and logging the attempted username here would put a
		// near-miss credential in the log.
		s.deps.Log.Info("login failed")
		return User{}, "", &Error{Code: CodeInvalidCredentials, Message: invalidCredentialsMessage}
	}
	if err := s.deps.Throttle.Succeed(ctx, fold, clientIP); err != nil {
		return User{}, "", err
	}
	token, err := s.startSession(ctx, user.ID(), s.deps.Now())
	if err != nil {
		return User{}, "", err
	}
	s.deps.Log.Info("login succeeded", slog.String("user_id", user.ID()))
	return user, token, nil
}

func (s *Service) startSession(ctx context.Context, userID string, now time.Time) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	if err := s.deps.Store.CreateLoginSession(ctx, NewLoginSession(hash, userID, now, s.deps.SessionTTL)); err != nil {
		return "", err
	}
	return token, nil
}

// Logout deletes the current login session. Logout with no valid session is a
// no-op returning the same success status, so it never reveals session
// validity (FR-3.6).
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	hash := hashToken(token)
	// Read the session before deleting it, purely so the observability event
	// can carry user_id — the NFR list requires one for logout, and the token
	// hash is not a user identifier.
	userID := ""
	if ls, err := s.deps.Store.LoginSession(ctx, hash); err == nil {
		userID = ls.UserID()
	}
	if err := s.deps.Store.DeleteLoginSession(ctx, hash); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if userID != "" {
		s.deps.Log.Info("logout", slog.String("user_id", userID))
	}
	return nil
}

// Authenticate resolves a cookie token to a scope, returning the token hash so
// the caller can later identify "this session" (ChangePassword keeps it).
//
// Expiry is enforced here, on read, so a stale row is never honoured even
// before the sweeper runs, and the row is deleted on the spot (FR-3.7).
func (s *Service) Authenticate(ctx context.Context, token string) (identity.Scope, []byte, error) {
	unauthenticated := &Error{Code: CodeUnauthenticated, Message: "Sign in to continue."}
	if token == "" {
		return identity.Standalone(), nil, unauthenticated
	}
	hash := hashToken(token)
	ls, err := s.deps.Store.LoginSession(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return identity.Standalone(), nil, unauthenticated
		}
		return identity.Standalone(), nil, err
	}
	now := s.deps.Now()
	if !now.Before(ls.ExpiresAt()) || now.Sub(ls.LastSeenAt()) >= s.deps.IdleTTL {
		if delErr := s.deps.Store.DeleteLoginSession(ctx, hash); delErr != nil {
			s.deps.Log.Warn("delete expired login session", slog.String("error", delErr.Error()))
		}
		return identity.Standalone(), nil, unauthenticated
	}
	// Rate-limited so read-heavy traffic does not write once per request
	// (FR-3.5).
	if now.Sub(ls.LastSeenAt()) >= lastSeenInterval {
		if err := s.deps.Store.TouchLoginSession(ctx, hash, now); err != nil {
			s.deps.Log.Warn("touch login session", slog.String("error", err.Error()))
		}
	}
	return identity.ForUser(ls.UserID()), hash, nil
}

// ChangePassword replaces the password and revokes every other login session
// for this user; keep is the calling session's token hash (FR-2.6).
func (s *Service) ChangePassword(ctx context.Context, userID string, keep []byte, current, next string) error {
	user, err := s.deps.Store.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.verify(user.PasswordHash(), current); err != nil {
		return &Error{Code: CodeInvalidCredentials, Message: "The current password is incorrect."}
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	hashed, err := s.hash(next) // before any write
	if err != nil {
		return err
	}
	if err := s.deps.Store.SetPasswordHash(ctx, userID, hashed, s.deps.Now()); err != nil {
		return err
	}
	if err := s.deps.Store.DeleteOtherLoginSessions(ctx, userID, keep); err != nil {
		return err
	}
	s.deps.Log.Info("password changed", slog.String("user_id", userID))
	return nil
}

// DeleteAccount removes the account and everything it owns (FR-2.7).
//
// The order is deliberate: verify the password, delete the database rows (the
// cascade takes login sessions, provider configurations, and lockout records),
// invalidate the cached registry, then purge the filesystem. A purge failure
// is logged at ERROR with the user id and the request still succeeds:
// reversing the order would risk an account that has lost its data but can
// still log in, which is strictly worse than an account that is gone with
// orphaned bytes on disk (design §6).
func (s *Service) DeleteAccount(ctx context.Context, userID, password string) error {
	user, err := s.deps.Store.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.verify(user.PasswordHash(), password); err != nil {
		return &Error{Code: CodeInvalidCredentials, Message: invalidCredentialsMessage}
	}
	if err := s.deps.Store.DeleteUser(ctx, userID); err != nil {
		return err
	}
	s.deps.Resolver.Invalidate(userID)
	if err := s.deps.Purger.PurgeUser(ctx, userID); err != nil {
		s.deps.Log.Error("purge user state failed; the account is deleted but bytes remain on disk",
			slog.String("user_id", userID), slog.String("error", err.Error()))
	}
	s.deps.Log.Info("account deleted", slog.String("user_id", userID))
	return nil
}

// User, ProviderCount, CountUsers, and Ping are thin pass-throughs the API and
// the startup log need.
func (s *Service) User(ctx context.Context, userID string) (User, error) {
	return s.deps.Store.UserByID(ctx, userID)
}
func (s *Service) ProviderCount(ctx context.Context, userID string) (int, error) {
	return s.deps.Store.CountUserProviders(ctx, userID)
}
func (s *Service) CountUsers(ctx context.Context) (int, error) { return s.deps.Store.CountUsers(ctx) }
func (s *Service) Ping(ctx context.Context) error              { return s.deps.Store.Ping(ctx) }
```

Imports: `context`, `crypto/rand`, `crypto/sha256`, `encoding/base64`, `errors`, `fmt`, `log/slog`, `time`, `identity`.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 -timeout 300s ./internal/auth/... && go tool golangci-lint run ./internal/auth/...
```

Expected: PASS on all fifteen functions. The suite is slow (real Argon2id at 64 MiB); the explicit `-timeout 300s` is why it is written out.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/auth
git commit -m "$(cat <<'EOF'
feat(auth): add the account lifecycle service

Login verifies against a package dummy hash on an unknown username so timing
does not distinguish it from a wrong password (FR-2.5), and both paths return
the identical error. A four-way semaphore bounds Argon2id memory, and hashing
always happens before any transaction opens — the single-connection pool makes
that ordering load-bearing, so it has both a comment at the call site and a
concurrent login/list test.

DeleteAccount deletes rows before purging the filesystem: an account that is
gone with orphaned bytes beats one that lost its data but can still log in.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: `auth` — provider configuration CRUD and the sweeper

**Files:**
- Create: `apps/backend/internal/auth/providers.go`
- Create: `apps/backend/internal/auth/sweep.go`
- Test: `apps/backend/internal/auth/providers_test.go`

**Interfaces:**
- Consumes: Tasks 4–8, 13, 14.
- Produces:
  ```go
  type ProviderInput struct {
      Slug        string
      DisplayName string
      Kind        config.Kind
      BaseURL     string
      Token       string
      Validate    bool
  }
  type ProviderPatch struct {
      DisplayName *string
      Kind        *config.Kind
      BaseURL     *string
      Token       *string   // nil or empty => keep the stored token (FR-5.5)
      Validate    bool
  }
  func (s *Service) CreateProvider(ctx context.Context, userID string, in ProviderInput) (UserProvider, error)
  func (s *Service) ListProviders(ctx context.Context, userID string) ([]UserProvider, error)
  func (s *Service) Provider(ctx context.Context, userID, id string) (UserProvider, error)
  func (s *Service) UpdateProvider(ctx context.Context, userID, id string, p ProviderPatch) (UserProvider, error)
  func (s *Service) DeleteProvider(ctx context.Context, userID, id string) error
  func (s *Service) Sweep(ctx context.Context) error
  ```

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/auth/providers_test.go`:

1. **`TestCreateProviderStoresAnEncryptedToken`** — create with `Token: "glpat-abcdef9f2c"`; assert the returned `UserProvider` has `TokenLast4() == "9f2c"`, non-empty ciphertext and nonce, and that **no field on it equals the plaintext**; then `Sealer.Open` on the stored columns returns the plaintext, proving it is recoverable.
2. **`TestCreateProviderDefaultsTheGitHubBaseURL`** — `Kind: config.KindGitHub, BaseURL: ""` gives `https://api.github.com`; a GitLab with no base URL errors (FR-5.2).
3. **`TestCreateProviderNormalizesATrailingSlash`** — `https://gitlab.example.com/` stores `https://gitlab.example.com`.
4. **`TestCreateProviderRejectsADuplicateSlug`** — `PROVIDER_SLUG_TAKEN`; a second user reusing the slug succeeds.
5. **`TestCreateProviderWithValidateTrueWritesNothingOnRejection`** — a `Verifier` returning `*Error{CodeProviderUnauthorized}`; assert the error and that `ListProviders` is still empty (acceptance criterion).
6. **`TestCreateProviderWithValidateFalseSkipsVerification`** — a `Verifier` that fails; `Validate: false` still writes, and the stub records zero calls (FR-5.6, "allows saving a configuration for an unreachable host").
7. **`TestCreateProviderInvalidatesTheResolverCache`** — resolve, create, resolve; the new provider appears without an explicit `Invalidate`.
8. **`TestUpdateProviderWithAnEmptyTokenKeepsTheStoredOne`** — create, then patch with `Token: ptr("")` and separately with `Token: nil`; in both cases `Sealer.Open` on the stored columns still yields the original plaintext and `TokenSetAt` did **not** move (FR-5.5, acceptance criterion).
9. **`TestUpdateProviderWithANewTokenReplacesIt`** — `TokenLast4` and `TokenSetAt` both change, and the old plaintext no longer opens.
10. **`TestUpdateProviderReencryptsUnderTheSameRowAAD`** — after an update, `Sealer.Open(ct, nonce, userID, providerID)` succeeds and `Open` with another user id fails (FR-5.3 survives updates).
11. **`TestUpdateProviderIsScopedToItsOwner`** — user B patching A's id gets `ErrNotFound` and A's row is unchanged.
12. **`TestUpdateProviderCannotChangeTheSlug`** — `ProviderPatch` has no `Slug` field, so this is a compile-time guarantee; the test instead asserts the stored slug is unchanged after a full patch of every other field, documenting the intent.
13. **`TestDeleteProviderRefusesWhileInUse`** — a `Usage` stub returning true gives `PROVIDER_IN_USE` and the row survives; returning false deletes it (FR-5.7).
14. **`TestDeleteProviderIsScopedAndInvalidates`** — B deleting A's id gets `ErrNotFound`; a successful delete drops the provider from a subsequent `Resolve`.
15. **`TestSweepRemovesExpiredSessionsAndElapsedLockouts`** — seed one expired login session and one elapsed lockout; `Sweep`; assert both gone and a live session survives (FR-3.7).

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/auth/... -run 'Provider|Sweep'
```

Expected: FAIL to compile.

- [ ] **Step 3: Write `providers.go`**

```go
package auth

// CreateProvider stores a new provider configuration for userID.
//
// Order matters: validate the shape, optionally verify the credential against
// the provider API, and only then seal and write. A verification failure must
// leave nothing behind (FR-5.6).
func (s *Service) CreateProvider(ctx context.Context, userID string, in ProviderInput) (UserProvider, error) {
	if err := ValidateSlug(in.Slug); err != nil {
		return UserProvider{}, err
	}
	kind, err := normalizeKind(in.Kind)
	if err != nil {
		return UserProvider{}, err
	}
	baseURL, err := NormalizeBaseURL(kind, in.BaseURL)
	if err != nil {
		return UserProvider{}, err
	}
	if in.Token == "" {
		return UserProvider{}, errors.New("auth: token is required")
	}
	if in.Validate {
		if err := s.deps.Verifier.Verify(ctx, kind, baseURL, config.NewSecret(in.Token)); err != nil {
			return UserProvider{}, err
		}
	}
	id, err := NewID()
	if err != nil {
		return UserProvider{}, err
	}
	// Sealed under this row's own id, so the ciphertext cannot be moved to
	// another row or another user and still decrypt (FR-5.3).
	ciphertext, nonce, err := s.deps.Sealer.Seal(in.Token, userID, id)
	if err != nil {
		return UserProvider{}, err
	}
	now := s.deps.Now()
	row := UserProvider{
		id: id, userID: userID, slug: in.Slug,
		displayName: displayNameOr(in.DisplayName, in.Slug),
		kind:        kind, baseURL: baseURL,
		tokenCiphertext: ciphertext, tokenNonce: nonce,
		tokenLast4: Last4(in.Token), tokenSetAt: now,
		createdAt: now, updatedAt: now,
	}
	if err := s.deps.Store.CreateUserProvider(ctx, row); err != nil {
		return UserProvider{}, err
	}
	s.deps.Resolver.Invalidate(userID)
	s.deps.Log.Info("provider configuration created",
		slog.String("user_id", userID), slog.String("provider", in.Slug), slog.String("kind", string(kind)))
	return row, nil
}

func (s *Service) ListProviders(ctx context.Context, userID string) ([]UserProvider, error) {
	return s.deps.Store.ListUserProviders(ctx, userID)
}

func (s *Service) Provider(ctx context.Context, userID, id string) (UserProvider, error) {
	return s.deps.Store.UserProviderByID(ctx, userID, id)
}

// UpdateProvider applies a partial update. slug is immutable, which
// ProviderPatch enforces by having no such field.
func (s *Service) UpdateProvider(ctx context.Context, userID, id string, p ProviderPatch) (UserProvider, error) {
	row, err := s.deps.Store.UserProviderByID(ctx, userID, id)
	if err != nil {
		return UserProvider{}, err
	}
	if p.DisplayName != nil {
		row.displayName = displayNameOr(*p.DisplayName, row.slug)
	}
	if p.Kind != nil {
		if row.kind, err = normalizeKind(*p.Kind); err != nil {
			return UserProvider{}, err
		}
	}
	if p.BaseURL != nil {
		if row.baseURL, err = NormalizeBaseURL(row.kind, *p.BaseURL); err != nil {
			return UserProvider{}, err
		}
	}
	now := s.deps.Now()
	// FR-5.5: an omitted or empty token leaves the stored token unchanged.
	// This is what lets the UI render an edit form without ever holding the
	// secret — the same mechanism as FR-5.4, seen from the other end.
	replacing := p.Token != nil && *p.Token != ""
	token := ""
	if replacing {
		token = *p.Token
	}
	if p.Validate {
		if replacing {
			if err := s.deps.Verifier.Verify(ctx, row.kind, row.baseURL, config.NewSecret(token)); err != nil {
				return UserProvider{}, err
			}
		} else {
			// Verifying an unchanged base URL or kind needs the stored token,
			// which means opening it — the only place an update decrypts.
			stored, openErr := s.deps.Sealer.Open(row.tokenCiphertext, row.tokenNonce, userID, id)
			if openErr != nil {
				return UserProvider{}, openErr
			}
			if err := s.deps.Verifier.Verify(ctx, row.kind, row.baseURL, stored); err != nil {
				return UserProvider{}, err
			}
		}
	}
	if replacing {
		ciphertext, nonce, sealErr := s.deps.Sealer.Seal(token, userID, id)
		if sealErr != nil {
			return UserProvider{}, sealErr
		}
		row.tokenCiphertext, row.tokenNonce = ciphertext, nonce
		row.tokenLast4, row.tokenSetAt = Last4(token), now
	}
	row.updatedAt = now
	if err := s.deps.Store.UpdateUserProvider(ctx, row); err != nil {
		return UserProvider{}, err
	}
	s.deps.Resolver.Invalidate(userID)
	s.deps.Log.Info("provider configuration updated",
		slog.String("user_id", userID), slog.String("provider", row.slug), slog.Bool("token_rotated", replacing))
	return row, nil
}

// DeleteProvider removes a configuration, refusing while a non-terminal
// review session of this user references it (FR-5.7). Completed reviews that
// referenced it stay readable.
func (s *Service) DeleteProvider(ctx context.Context, userID, id string) error {
	row, err := s.deps.Store.UserProviderByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if s.deps.Usage.ProviderInUse(identity.ForUser(userID), row.Slug()) {
		return &Error{
			Code:    CodeProviderInUse,
			Message: "This provider is used by a review that is still in progress.",
		}
	}
	if err := s.deps.Store.DeleteUserProvider(ctx, userID, id); err != nil {
		return err
	}
	s.deps.Resolver.Invalidate(userID)
	s.deps.Log.Info("provider configuration deleted",
		slog.String("user_id", userID), slog.String("provider", row.Slug()))
	return nil
}

func normalizeKind(k config.Kind) (config.Kind, error) {
	switch config.Kind(strings.ToLower(string(k))) {
	case config.KindGitHub:
		return config.KindGitHub, nil
	case config.KindGitLab:
		return config.KindGitLab, nil
	default:
		return "", fmt.Errorf("auth: kind must be github or gitlab")
	}
}

func displayNameOr(name, slug string) string {
	if strings.TrimSpace(name) == "" {
		return slug
	}
	return strings.TrimSpace(name)
}
```

- [ ] **Step 4: Write `sweep.go`**

```go
package auth

// Sweep deletes expired login sessions and elapsed lockout counters.
//
// Called by the background sweeper loop on the same CLEANUP_INTERVAL_MINUTES
// cadence as the review-session sweeper. Expiry is also enforced on read (see
// Authenticate), so a stale row is never honoured even before this runs
// (FR-3.7); this is housekeeping, not enforcement.
func (s *Service) Sweep(ctx context.Context) error {
	sessions, err := s.deps.Store.DeleteExpiredLoginSessions(ctx, s.deps.Now(), s.deps.IdleTTL)
	if err != nil {
		return err
	}
	lockouts, err := s.deps.Throttle.Sweep(ctx)
	if err != nil {
		return err
	}
	if sessions > 0 || lockouts > 0 {
		s.deps.Log.Info("auth sweep",
			slog.Int64("login_sessions_removed", sessions),
			slog.Int64("lockouts_removed", lockouts))
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 -timeout 300s ./internal/auth/... && go vet ./internal/auth/... && go tool golangci-lint run ./internal/auth/...
```

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/auth
git commit -m "$(cat <<'EOF'
feat(auth): add per-user provider configuration CRUD and the auth sweeper

Tokens are sealed under their own row id, so a ciphertext moved between rows
or users fails to decrypt — and an update re-seals under the same id rather
than reusing the old nonce. ProviderPatch has no Slug field, which makes the
slug's immutability a compile-time property rather than a validation rule.

Every write path invalidates the resolver cache, so the next request builds a
fresh registry.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: `api` — scope context, cookies, and the two middlewares

**Files:**
- Modify: `apps/backend/internal/api/authctx.go` (created as a stub in Task 9)
- Create: `apps/backend/internal/api/authmw.go`
- Modify: `apps/backend/internal/api/router.go` (`Deps` fields only)
- Test: `apps/backend/internal/api/authmw_test.go`

**Interfaces:**
- Consumes: `auth.Service`, `auth.Error`, `config.Mode`, `identity.Scope`.
- Produces:
  - `api.Deps` gains: `Mode config.Mode`, `Auth *auth.Service`, `SecureCookies bool`, `TrustedProxy bool`, `LoginSessionTTL time.Duration`, `AuthSweep func(context.Context) error`, `DBPing func(context.Context) error`.
  - `func scopeFrom(r *http.Request) identity.Scope`, `func tokenHashFrom(r *http.Request) []byte`, `func sessionTokenFrom(r *http.Request) string`
  - `func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string)`, `func (s *server) clearSessionCookie(w http.ResponseWriter, r *http.Request)`
  - `func (s *server) clientIP(r *http.Request) string`
  - `func (s *server) originGuard(next http.Handler) http.Handler`, `func (s *server) authenticate(next http.Handler) http.Handler`
  - `const sessionCookieName = "converge_session"`

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/api/authmw_test.go`:

1. **`TestScopeFromDefaultsToStandalone`** — a bare `httptest.NewRequest` yields `identity.Standalone()` from `scopeFrom`. The five-line function with a test proving the default (design §7).
2. **`TestOriginGuardAllowsSafeMethods`** — `GET` and `HEAD` with a foreign `Origin` reach the handler.
3. **`TestOriginGuardAllowsMatchingOriginAndSameSiteFetch`** — a `POST` with `Origin: http://example.test` and `Host: example.test` passes; a `POST` with **no** `Origin` but `Sec-Fetch-Site: same-origin` passes.
4. **`TestOriginGuardRejectsAForeignOrigin`** — `POST` with `Origin: http://evil.test`, `Host: example.test`, no `Sec-Fetch-Site` → `403` with code `FORBIDDEN`, and the handler was **not** invoked (a cross-site request must never reach a handler).
5. **`TestOriginGuardRejectsAMissingOriginAndSecFetchSite`** — `POST` with neither header → `403`. Combined with `SameSite=Lax` this is the CSRF defence; no token is issued (FR-4.4).
6. **`TestOriginGuardIgnoresNonAPIPaths`** — `POST /healthz` with a foreign origin is not the guard's business.
7. **`TestAuthenticateAllowsPublicRoutes`** — `GET /api/auth/mode`, `POST /api/auth/register`, `POST /api/auth/login` reach the handler with no cookie (FR-4.1).
8. **`TestAuthenticateRejectsAMissingCookie`** — `GET /api/reviews` with no cookie → `401` with code `UNAUTHENTICATED`.
9. **`TestAuthenticateRejectsAnUnknownToken`** — a syntactically valid but unregistered token → `401`, and the response clears the cookie (`Max-Age=0`).
10. **`TestAuthenticateInjectsTheScopeAndTokenHash`** — register through a real `auth.Service`, present the cookie, and assert the handler sees `scopeFrom(r).UserID() == user.ID()` and a 32-byte `tokenHashFrom(r)`.
11. **`TestAuthenticateLeavesHealthzAndUIAlone`** — `GET /healthz` and `GET /` with no cookie reach the handler (FR-4.1: always unauthenticated).
12. **`TestSessionCookieAttributes`** — `setSessionCookie` over plain HTTP with `SecureCookies: false` emits `HttpOnly`, `Path=/`, `SameSite=Lax`, and **no** `Secure`; with `SecureCookies: true` it emits `Secure`; with `r.TLS != nil` and `SecureCookies: false` it also emits `Secure` (FR-3.1, FR-3.2, acceptance criterion).
13. **`TestClientIP`** — with `TrustedProxy: false`, `X-Forwarded-For: 1.2.3.4` is ignored and `RemoteAddr`'s host is used; with `TrustedProxy: true`, `X-Forwarded-For: 9.9.9.9, 1.2.3.4` yields `1.2.3.4` (the last hop, FR-7.3).

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/api/... -run 'OriginGuard|Authenticate|SessionCookie|ClientIP|ScopeFrom'
```

Expected: FAIL to compile.

- [ ] **Step 3: Write `authctx.go`**

Replace the Task 9 stub:

```go
package api

// sessionCookieName is the login session cookie (FR-3.1).
const sessionCookieName = "converge_session"

// authStateKey is a private, non-string context key, so no other package can
// read or write this value.
type authStateKey struct{}

type authState struct {
	scope     identity.Scope
	tokenHash []byte
}

// scopeFrom returns the scope the authenticate middleware attached, or the
// standalone scope when none is present.
//
// This is the one place identity travels on a context (FR-4.5). Below api it
// is always an explicit function argument, because a value whose absence is a
// data leak should be visible in signatures.
func scopeFrom(r *http.Request) identity.Scope {
	if st, ok := r.Context().Value(authStateKey{}).(authState); ok {
		return st.scope
	}
	return identity.Standalone()
}

// tokenHashFrom returns the calling login session's token hash, so
// ChangePassword can revoke every session except this one.
func tokenHashFrom(r *http.Request) []byte {
	if st, ok := r.Context().Value(authStateKey{}).(authState); ok {
		return st.tokenHash
	}
	return nil
}

// sessionTokenFrom reads the raw cookie. Only the logout handler and the
// authenticate middleware use it; no other handler reads the cookie directly
// (FR-4.5).
func sessionTokenFrom(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func withAuthState(r *http.Request, st authState) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authStateKey{}, st))
}

// secure reports whether the cookie should carry the Secure attribute: when
// the request arrived over TLS, or unconditionally when
// CONVERGE_SECURE_COOKIES=true, which is what an operator terminating TLS at
// a proxy sets (FR-3.2).
func (s *server) secure(r *http.Request) bool {
	return r.TLS != nil || s.deps.SecureCookies
}

func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure(r),
		SameSite: http.SameSiteLaxMode,
		// MaxAge mirrors the session's absolute expiry so a browser discards
		// the cookie at roughly the moment the server stops honouring it.
		MaxAge: int(s.deps.LoginSessionTTL.Seconds()),
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// clientIP is the throttle key. X-Forwarded-For is honoured only when the
// operator has declared a trusted proxy: otherwise any client could forge a
// fresh key per request and sidestep the per-IP lockout entirely (FR-7.3).
func (s *server) clientIP(r *http.Request) string {
	if s.deps.TrustedProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			hops := strings.Split(xff, ",")
			if last := strings.TrimSpace(hops[len(hops)-1]); last != "" {
				return last
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
```

- [ ] **Step 4: Write `authmw.go`**

```go
package api

// publicRoute lists the three routes reachable without a login session
// (FR-4.1). /healthz and the embedded UI are handled by the /api/ prefix
// check in the middleware, not here.
func publicRoute(method, path string) bool {
	switch path {
	case "/api/auth/mode":
		return method == http.MethodGet
	case "/api/auth/register", "/api/auth/login":
		return method == http.MethodPost
	default:
		return false
	}
}

// originGuard rejects a state-changing cross-site request.
//
// It runs before authenticate, because the contract requires that a
// cross-site request never reach a handler — and because it is cheaper. Both
// middlewares are constructed only in hosted mode, so standalone's request
// path gains exactly zero comparisons.
//
// Combined with SameSite=Lax this is the whole CSRF defence; no CSRF token is
// issued (FR-4.4).
func (s *server) originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if u, err := url.Parse(origin); err == nil && u.Host == r.Host {
				next.ServeHTTP(w, r)
				return
			}
		}
		_ = jsonapi.WriteError(w, http.StatusForbidden, string(auth.CodeForbidden),
			jsonapi.StatusTitle(http.StatusForbidden),
			"This request did not come from this application.")
	})
}

// authenticate resolves the session cookie into an identity.Scope on the
// request context.
func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /healthz and the embedded UI assets are always unauthenticated
		// (FR-4.1).
		if !strings.HasPrefix(r.URL.Path, "/api/") || publicRoute(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		scope, tokenHash, err := s.deps.Auth.Authenticate(r.Context(), sessionTokenFrom(r))
		if err != nil {
			// Clearing the cookie on a rejected session stops a browser from
			// re-presenting a token the server has already forgotten.
			s.clearSessionCookie(w, r)
			writeDomainError(w, s.deps.Log, err)
			return
		}
		next.ServeHTTP(w, withAuthState(r, authState{scope: scope, tokenHash: tokenHash}))
	})
}
```

- [ ] **Step 5: Add the `Deps` fields**

In `internal/api/router.go`, extend `Deps` (do not wire the middleware yet — Task 19 does the route registration):

```go
	// Mode decides whether the auth and settings routes are registered at all
	// and whether the auth middlewares are constructed. In standalone mode
	// the absence of a route *is* the 404 behaviour FR-4.3 requires, so no
	// handler contains a "return 404 in standalone" branch.
	Mode config.Mode
	// Auth is non-nil only in hosted mode.
	Auth *auth.Service
	// SecureCookies and TrustedProxy come straight from configuration.
	SecureCookies bool
	TrustedProxy  bool
	// LoginSessionTTL sets the session cookie's Max-Age.
	LoginSessionTTL time.Duration
	// AuthSweep, when set, is run every CleanupInterval alongside the
	// review-session sweeper.
	AuthSweep func(context.Context) error
	// DBPing, when set, makes /healthz report database reachability.
	DBPing func(context.Context) error
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
cd apps/backend && go test -race -count=1 ./internal/api/... && go vet ./... && go tool golangci-lint run
```

Expected: PASS on all thirteen new functions plus every pre-existing api test.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/api
git commit -m "$(cat <<'EOF'
feat(api): add the origin guard, the auth middleware, and the scope accessor

originGuard runs before authenticate so a cross-site request never reaches a
handler, and both are constructed only in hosted mode, so standalone's request
path gains zero comparisons. scopeFrom is the single place identity travels on
a context; below api it is an explicit argument.

X-Forwarded-For is honoured only behind a declared trusted proxy — otherwise
any client could forge a fresh throttle key per request.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 17: `api` — the `/api/auth/*` handlers

**Files:**
- Create: `apps/backend/internal/api/auth.go`
- Modify: `apps/backend/internal/api/errors.go` (`classify` gains two arms)
- Test: `apps/backend/internal/api/auth_test.go`

**Interfaces:**
- Consumes: Task 14's `auth.Service`; Task 16's cookie helpers and accessors.
- Produces handlers `(s *server) authMode`, `register`, `login`, `logout`, `currentUser`, `changePassword`, `deleteAccount`, registered in Task 19.

- [ ] **Step 1: Extend `classify`**

In `internal/api/errors.go`, add one `errors.As` arm and one `errors.Is` case. The `errors.As` arm goes **before** the existing `session.ReviewError` arm (they are distinct types, so order is cosmetic, but keeping the auth arm adjacent to the other typed-error arms reads better):

```go
	// One arm rather than ten errors.Is cases: auth.Error carries its own
	// code and status, mirroring session.ReviewError.
	var ae *auth.Error
	if errors.As(err, &ae) {
		return ae.Status(), string(ae.Code), ae.Message
	}
```

and in the `switch` block, extend the existing not-found case:

```go
	case errors.Is(err, provider.ErrNotFound), errors.Is(err, session.ErrNotFound), errors.Is(err, auth.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND", "The requested resource does not exist."
```

- [ ] **Step 2: Write the failing test**

Create `apps/backend/internal/api/auth_test.go`. Build a helper that stands up a hosted-mode router over a temp database — `newHostedServer(t) (http.Handler, *auth.Service)` — plus a `do(t, handler, method, path, body, cookies...)` helper that sets `Accept: application/vnd.api+json`, `Content-Type: application/vnd.api+json`, and `Sec-Fetch-Site: same-origin` (so the origin guard passes) and returns the `*httptest.ResponseRecorder`.

Test functions:

1. **`TestAuthModeReportsTheMode`** — hosted returns `{"mode":"hosted","registrationOpen":true}` with `data.type == "modes"` and `data.id == "current"`, status 200, **with no cookie**; a standalone router returns `{"mode":"standalone","registrationOpen":false}` (acceptance criterion: correct mode, unauthenticated, in both modes).
2. **`TestRegisterSetsTheSessionCookieAndReturnsTheUser`** — `201`, `data.type == "users"`, `attributes.username == "alice"`, an RFC-3339 `createdAt`, a `Set-Cookie` named `converge_session`, and **no** `password` or token anywhere in the body.
3. **`TestRegisterRejectsAnUnknownAttribute`** — a body carrying `"nickname": "x"` gives `400`/`INVALID_REQUEST` (`jsonapi.Decode` rejects unknown attribute fields).
4. **`TestRegisterErrorCodes`** — `INVALID_USERNAME` 422, `WEAK_PASSWORD` 422, `USERNAME_TAKEN` 409.
5. **`TestLoginAndLogout`** — register, logout (`204`, `Set-Cookie` with `Max-Age=0`), the old cookie now `401`s on `GET /api/auth/me`, login returns `200` with a fresh cookie.
6. **`TestLogoutIsIdempotent`** — `POST /api/auth/logout` with no cookie and with a stale cookie both return `204` (FR-3.6).
7. **`TestLoginReturnsRetryAfterOnLockout`** — six wrong-password logins; the sixth is `429`/`ACCOUNT_LOCKED` with a positive integer `Retry-After` header (acceptance criterion).
8. **`TestCurrentUserIncludesProviderCount`** — register, create two provider configs, `GET /api/auth/me` shows `providerCount: 2` (FR-5.9's one-request affordance).
9. **`TestChangePasswordKeepsTheCallingSession`** — two logins; change the password with cookie 1; cookie 1 still works on `/api/auth/me`, cookie 2 returns `401`, login with the old password `401`s and with the new one succeeds.
10. **`TestDeleteAccountClearsTheCookie`** — `204`, `Set-Cookie` with `Max-Age=0`, and the cookie no longer authenticates (acceptance criterion).
11. **`TestAuthRoutesAre404InStandalone`** — on a standalone router, `POST /api/auth/login`, `POST /api/auth/register`, `GET /api/auth/me`, `POST /api/auth/logout`, `POST /api/auth/password`, and `DELETE /api/auth/me` all return `404` with code `NOT_FOUND` (FR-4.3, acceptance criterion). Only `GET /api/auth/mode` works.
12. **`TestStateChangingAuthRoutesRequireTheOriginCheck`** — `POST /api/auth/login` with `Origin: http://evil.test` and no `Sec-Fetch-Site` is `403`/`FORBIDDEN`.

- [ ] **Step 3: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/api/... -run 'Auth|Register|Login|Logout|CurrentUser|ChangePassword|DeleteAccount'
```

- [ ] **Step 4: Write `auth.go`**

```go
package api

// Resource types, verbatim from api-contracts.md.
const (
	typeModes            = "modes"
	typeCredentials      = "credentials"
	typeUsers            = "users"
	typePasswords        = "passwords"
	typeAccountDeletions = "accountDeletions"
)

type modeAttributes struct {
	Mode string `json:"mode"`
	// RegistrationOpen is false in standalone and true in hosted. It exists so
	// a future invite-code or closed-registration feature needs no new
	// endpoint — the seam design §11 names.
	RegistrationOpen bool `json:"registrationOpen"`
}

type credentialAttributes struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userAttributes struct {
	Username  string `json:"username"`
	CreatedAt string `json:"createdAt"`
	// ProviderCount is omitted on the register/login responses and present on
	// GET /api/auth/me.
	ProviderCount *int `json:"providerCount,omitempty"`
}

type passwordAttributes struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type accountDeletionAttributes struct {
	Password string `json:"password"`
}

// userResource maps an account onto the wire. There is deliberately no field
// here that could carry a password hash.
func userResource(u auth.User, providerCount *int) jsonapi.Resource {
	return jsonapi.Resource{Type: typeUsers, ID: u.ID(), Attributes: userAttributes{
		Username:      u.Username(),
		CreatedAt:     u.CreatedAt().UTC().Format(time.RFC3339),
		ProviderCount: providerCount,
	}}
}

// authMode is registered in both modes and needs no session: it is the SPA's
// first call and decides whether a login screen is rendered at all (FR-8.1).
func (s *server) authMode(w http.ResponseWriter, _ *http.Request) {
	hosted := s.deps.Mode == config.ModeHosted
	res := jsonapi.Resource{Type: typeModes, ID: "current", Attributes: modeAttributes{
		Mode:             string(s.deps.Mode),
		RegistrationOpen: hosted,
	}}
	if err := jsonapi.WriteOne(w, http.StatusOK, res); err != nil {
		s.deps.Log.Error("write auth mode failed", "error", err)
	}
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[credentialAttributes](r, typeCredentials)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	user, token, err := s.deps.Auth.Register(r.Context(),
		auth.Credentials{Username: attrs.Username, Password: attrs.Password}, s.clientIP(r))
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	s.setSessionCookie(w, r, token)
	if err := jsonapi.WriteOne(w, http.StatusCreated, userResource(user, nil)); err != nil {
		s.deps.Log.Error("write register response failed", "error", err)
	}
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[credentialAttributes](r, typeCredentials)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	user, token, err := s.deps.Auth.Login(r.Context(),
		auth.Credentials{Username: attrs.Username, Password: attrs.Password}, s.clientIP(r))
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	s.setSessionCookie(w, r, token)
	if err := jsonapi.WriteOne(w, http.StatusOK, userResource(user, nil)); err != nil {
		s.deps.Log.Error("write login response failed", "error", err)
	}
}

// writeAuthError delegates to writeDomainError, first setting Retry-After for
// a lockout.
//
// This is the one local exception to "classify owns the HTTP mapping":
// classify returns a triple and cannot set a header, and ACCOUNT_LOCKED needs
// one (FR-7.4). Documented here rather than by widening classify's contract
// for a single code.
func (s *server) writeAuthError(w http.ResponseWriter, err error) {
	var ae *auth.Error
	if errors.As(err, &ae) && ae.Code == auth.CodeAccountLocked && ae.RetryAfter > 0 {
		seconds := int(math.Ceil(ae.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
	}
	writeDomainError(w, s.deps.Log, err)
}

// logout is idempotent and never reveals session validity (FR-3.6).
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Auth.Logout(r.Context(), sessionTokenFrom(r)); err != nil {
		s.deps.Log.Warn("logout failed", "error", err)
	}
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) currentUser(w http.ResponseWriter, r *http.Request) {
	userID := scopeFrom(r).UserID()
	user, err := s.deps.Auth.User(r.Context(), userID)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	count, err := s.deps.Auth.ProviderCount(r.Context(), userID)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusOK, userResource(user, &count)); err != nil {
		s.deps.Log.Error("write current user failed", "error", err)
	}
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[passwordAttributes](r, typePasswords)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	// tokenHashFrom identifies "this session", the one session
	// ChangePassword must not revoke (FR-2.6).
	if err := s.deps.Auth.ChangePassword(r.Context(), scopeFrom(r).UserID(), tokenHashFrom(r),
		attrs.CurrentPassword, attrs.NewPassword); err != nil {
		s.writeAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[accountDeletionAttributes](r, typeAccountDeletions)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	if err := s.deps.Auth.DeleteAccount(r.Context(), scopeFrom(r).UserID(), attrs.Password); err != nil {
		s.writeAuthError(w, err)
		return
	}
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Run and commit**

```bash
cd apps/backend && go test -race -count=1 -timeout 300s ./internal/api/... && go vet ./... && go tool golangci-lint run
```

Test 11 (`TestAuthRoutesAre404InStandalone`) will fail until Task 19 registers routes conditionally — that is expected; skip it with `t.Skip("route registration lands in Task 19")` here and **remove the skip in Task 19**, where it becomes the acceptance evidence.

```bash
git add apps/backend/internal/api
git commit -m "$(cat <<'EOF'
feat(api): add the /api/auth/* handlers

classify gains one errors.As arm for *auth.Error rather than ten errors.Is
cases. The lockout's Retry-After header is set by a small local helper,
because classify returns a triple and cannot set headers — documented at the
call site rather than by widening classify's contract for one code.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 18: `api` — the `/api/settings/providers` handlers

**Files:**
- Create: `apps/backend/internal/api/settings_providers.go`
- Test: `apps/backend/internal/api/settings_providers_test.go`

**Interfaces:**
- Consumes: Task 15's `Service.CreateProvider`/`ListProviders`/`Provider`/`UpdateProvider`/`DeleteProvider`; `auth.ProviderInput`, `auth.ProviderPatch`.
- Produces handlers `(s *server) listUserProviders`, `createUserProvider`, `updateUserProvider`, `deleteUserProvider`, registered in Task 19.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/api/settings_providers_test.go`:

1. **`TestUserProviderCRUDOverHTTP`** — `POST` creates (`201`, `data.type == "userProviders"`), `GET` lists it, `PATCH` updates `displayName`, `DELETE` returns `204` and the list is empty (acceptance criterion: create, list, edit, delete).
2. **`TestNoResponseEverContainsAToken`** — the single most important test in this task. Create with `token: "glpat-SENTINELTOKEN9f2c"`, then capture the raw bytes of **every** response in the CRUD cycle plus `GET /api/providers` and `GET /api/auth/me`, and assert none contains `"SENTINEL"` and none contains a `"token"` key. Only `tokenLast4: "9f2c"` appears (FR-5.4, acceptance criterion).
3. **`TestCreateValidatesTheRequestShape`** — a malformed slug (`"Bad_Slug"`), an unknown kind, a relative `baseUrl`, and a missing token each give `422`/`VALIDATION_ERROR`; an unknown attribute gives `400`/`INVALID_REQUEST`.
4. **`TestCreateDefaultsValidateToTrue`** — `validate` omitted entirely; a stub verifier that rejects makes the create fail `422`/`PROVIDER_UNAUTHORIZED` and write nothing, proving the default is `true` rather than Go's zero `false` (FR-5.6). **This is why `validate` must be decoded as a `*bool`.**
5. **`TestCreateWithValidateFalseSkipsVerification`** — `"validate": false` with a rejecting verifier still returns `201`.
6. **`TestDuplicateSlugIs409`** — `PROVIDER_SLUG_TAKEN`.
7. **`TestPatchWithAnOmittedTokenKeepsTheStoredOne`** — create, `PATCH` only `displayName`, then assert via a `GET /api/providers` that the registry still resolves (i.e. the token still decrypts) and `tokenLast4`/`tokenSetAt` are unchanged (acceptance criterion).
8. **`TestPatchRejectsASlugChange`** — a body with `"slug"` gives `400`/`INVALID_REQUEST`, because the patch attribute struct has no `Slug` field and `jsonapi.Decode` rejects unknown attributes. The immutability is enforced by the decoder, not by a hand-written check.
9. **`TestForeignProviderIDIs404`** — user B `GET`/`PATCH`/`DELETE` on user A's provider id all return `404`/`NOT_FOUND`, never `403` (FR-4.2, acceptance criterion).
10. **`TestDeleteRefusesWhileInUse`** — with a `Usage` stub returning true, `409`/`PROVIDER_IN_USE` (acceptance criterion).
11. **`TestSettingsRoutesAre404InStandalone`** — all four routes on a standalone router return `404` (FR-4.3, acceptance criterion). Same `t.Skip` + remove-in-Task-19 arrangement as Task 17.
12. **`TestListIsSortedBySlug`** — create `zeta` then `alpha`; the list comes back `alpha`, `zeta`.

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/api/... -run UserProvider
```

- [ ] **Step 3: Write `settings_providers.go`**

```go
package api

const typeUserProviders = "userProviders"

// userProviderAttributes is the response shape. There is no token field, and
// there is no endpoint anywhere in this API that returns one (FR-5.4).
// tokenLast4 is captured at write time and stored beside the ciphertext, so
// rendering a mask never requires a decrypt.
type userProviderAttributes struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"baseUrl"`
	TokenLast4  string `json:"tokenLast4"`
	TokenSetAt  string `json:"tokenSetAt"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type createUserProviderAttributes struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"baseUrl"`
	Token       string `json:"token"`
	// Validate is a pointer because its default is true, not Go's zero false
	// (FR-5.6). An omitted field must verify the credential.
	Validate *bool `json:"validate"`
}

// patchUserProviderAttributes has no Slug field: the slug is immutable, and
// jsonapi.Decode rejects unknown attributes, so an attempt to change it is a
// 400 from the decoder rather than a hand-written validation branch.
//
// Token is a pointer so "absent" and "empty string" are both expressible, and
// both mean "keep the stored token" (FR-5.5).
type patchUserProviderAttributes struct {
	DisplayName *string `json:"displayName"`
	Kind        *string `json:"kind"`
	BaseURL     *string `json:"baseUrl"`
	Token       *string `json:"token"`
	Validate    *bool   `json:"validate"`
}

func userProviderResource(p auth.UserProvider) jsonapi.Resource {
	return jsonapi.Resource{Type: typeUserProviders, ID: p.ID(), Attributes: userProviderAttributes{
		Slug:        p.Slug(),
		DisplayName: p.DisplayName(),
		Kind:        string(p.Kind()),
		BaseURL:     p.BaseURL(),
		TokenLast4:  p.TokenLast4(),
		TokenSetAt:  p.TokenSetAt().UTC().Format(time.RFC3339),
		CreatedAt:   p.CreatedAt().UTC().Format(time.RFC3339),
		UpdatedAt:   p.UpdatedAt().UTC().Format(time.RFC3339),
	}}
}

// validationError reports a request-shape problem with the existing code for
// that class (api-contracts.md), keeping the auth package free of HTTP
// concerns.
func (s *server) validationError(w http.ResponseWriter, detail string) {
	_ = jsonapi.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR",
		jsonapi.StatusTitle(http.StatusUnprocessableEntity), detail)
}

func (s *server) listUserProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.deps.Auth.ListProviders(r.Context(), scopeFrom(r).UserID())
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, userProviderResource(row))
	}
	if err := jsonapi.WriteList(w, http.StatusOK, out, nil); err != nil {
		s.deps.Log.Error("write user providers failed", "error", err)
	}
}

func (s *server) createUserProvider(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[createUserProviderAttributes](r, typeUserProviders)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	if attrs.Token == "" {
		s.validationError(w, "A token is required.")
		return
	}
	validate := true
	if attrs.Validate != nil {
		validate = *attrs.Validate
	}
	row, err := s.deps.Auth.CreateProvider(r.Context(), scopeFrom(r).UserID(), auth.ProviderInput{
		Slug:        attrs.Slug,
		DisplayName: attrs.DisplayName,
		Kind:        config.Kind(attrs.Kind),
		BaseURL:     attrs.BaseURL,
		Token:       attrs.Token,
		Validate:    validate,
	})
	if err != nil {
		s.writeProviderSettingsError(w, err)
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusCreated, userProviderResource(row)); err != nil {
		s.deps.Log.Error("write user provider failed", "error", err)
	}
}

func (s *server) updateUserProvider(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[patchUserProviderAttributes](r, typeUserProviders)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	patch := auth.ProviderPatch{
		DisplayName: attrs.DisplayName,
		BaseURL:     attrs.BaseURL,
		Token:       attrs.Token,
		Validate:    attrs.Validate != nil && *attrs.Validate,
	}
	if attrs.Kind != nil {
		kind := config.Kind(*attrs.Kind)
		patch.Kind = &kind
	}
	row, err := s.deps.Auth.UpdateProvider(r.Context(), scopeFrom(r).UserID(), r.PathValue("id"), patch)
	if err != nil {
		s.writeProviderSettingsError(w, err)
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusOK, userProviderResource(row)); err != nil {
		s.deps.Log.Error("write user provider failed", "error", err)
	}
}

func (s *server) deleteUserProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Auth.DeleteProvider(r.Context(), scopeFrom(r).UserID(), r.PathValue("id")); err != nil {
		s.writeProviderSettingsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeProviderSettingsError separates the two error families the auth
// provider layer produces: a typed *auth.Error carries its own wire code,
// while a plain error from slug/kind/base-URL normalisation is a
// request-shape problem and reports as VALIDATION_ERROR.
func (s *server) writeProviderSettingsError(w http.ResponseWriter, err error) {
	var ae *auth.Error
	if errors.As(err, &ae) || errors.Is(err, auth.ErrNotFound) {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	s.validationError(w, err.Error())
}
```

> **Implementer note:** `writeProviderSettingsError`'s fallback puts `err.Error()` in a client-visible `detail`. Every plain error returned by `auth`'s provider layer is a deliberately short, value-free message (`"auth: kind must be github or gitlab"`). Before wiring this, re-read `auth/providers.go` and `auth/model.go` and confirm no plain error there interpolates a token, a base URL with credentials, or a database message. If any does, give it a fixed message instead.

`ProviderPatch.Validate` defaults to **false** on a PATCH: re-verifying on every cosmetic edit would hit the provider API for a display-name change. `api-contracts.md` lists `PROVIDER_UNAUTHORIZED` as a possible PATCH error, which stays reachable by sending `"validate": true`. Record this as a deliberate reading of the contract in `context.md`.

- [ ] **Step 4: Run and commit**

```bash
cd apps/backend && go test -race -count=1 -timeout 300s ./internal/api/... && go vet ./... && go tool golangci-lint run
```

```bash
git add apps/backend/internal/api
git commit -m "$(cat <<'EOF'
feat(api): add the /api/settings/providers handlers

The response attribute struct has no token field and the patch struct has no
slug field, so FR-5.4 and the slug's immutability are enforced by the types
and the decoder rather than by handler discipline. validate and token are
pointers because "absent" carries meaning for both: validate defaults to true
on create, and an absent or empty token keeps the stored one.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 19: `api` — scoped existing handlers, mode-conditional routes, healthz

**Files:**
- Modify: `apps/backend/internal/api/router.go`, `reviews.go`, `review_files.go`, `health.go`
- Modify: `apps/backend/internal/api/auth_test.go`, `settings_providers_test.go` (remove the `t.Skip`s)
- Test: `apps/backend/internal/api/isolation_test.go` (new)

**Interfaces:**
- Consumes: everything from Tasks 12, 16, 17, 18.
- Produces: no new exported surface; `NewRouter` now branches on `d.Mode`.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/api/isolation_test.go`:

1. **`TestReviewsAreScopedToTheirOwner`** — register two users, each with a provider config over a fake provider, each creating a review. Assert `GET /api/reviews` for A returns exactly A's and never B's (acceptance criterion).
2. **`TestForeignReviewIDIs404`** — `GET /api/reviews/{bID}`, `/files`, `/files/{path}`, `/diff`, and `DELETE /api/reviews/{bID}` as user A all return `404`/`NOT_FOUND` (acceptance criterion).
3. **`TestUnownedReviewsAreInvisibleInHostedMode`** — write a `session.json` with no `owner` directly into `WORKSPACE_ROOT`, `LoadAll`, and assert it appears in neither user's list and `GET` by its id is `404` (FR-6.3).
4. **`TestUnauthenticatedRequestsToProtectedRoutesAre401`** — every route in the table below, with no cookie, returns `401`/`UNAUTHENTICATED`: `GET /api/providers`, `GET /api/providers/{p}/repositories`, `POST /api/reviews`, `GET /api/reviews`, `GET /api/reviews/{id}`, `DELETE /api/reviews/{id}`, `GET /api/settings/providers`, `GET /api/auth/me` (FR-4.1).
5. **`TestHealthzAndUIStayUnauthenticated`** — `GET /healthz` is `200` with no cookie in both modes; `GET /` serves the UI handler.
6. **`TestCreateReviewWithAForeignOriginIs403`** — `POST /api/reviews` with `Origin: http://evil.test` is `403`/`FORBIDDEN` (acceptance criterion).
7. **`TestHealthzReportsDatabaseReachability`** — hosted `/healthz` body has `checks.database == "ok"`; standalone `/healthz` has **no** `database` key at all, so its response is byte-identical to today's.
8. **`TestStandaloneRouterIsUnchanged`** — on a standalone router with no new env vars: `GET /api/reviews` needs no cookie, `GET /api/auth/mode` reports standalone, and every `/api/auth/*` and `/api/settings/*` route is `404`.

Then delete the `t.Skip` lines from `TestAuthRoutesAre404InStandalone` (Task 17) and `TestSettingsRoutesAre404InStandalone` (Task 18).

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/api/...
```

- [ ] **Step 3: Scope the existing handlers**

`reviews.go`:

```go
func (s *server) createReview(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[createReviewAttributes](r, "reviews")
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	sess, err := s.deps.Service.Create(r.Context(), scopeFrom(r), review.CreateInput{
		ProviderID: attrs.Provider, Repository: attrs.Repository, BaseBranch: attrs.BaseBranch, Changes: attrs.Changes,
	})
	// ... unchanged ...
}

func (s *server) listReviews(w http.ResponseWriter, r *http.Request) {
	sessions := s.deps.Service.List(scopeFrom(r))
	// ... unchanged ...
}
```

`sessionFor` passes the scope into `Get`, and skips the `Corrupted` branch in hosted mode:

```go
func (s *server) sessionFor(w http.ResponseWriter, r *http.Request) (session.Session, bool) {
	id := r.PathValue("id")
	scope := scopeFrom(r)
	if err := workspace.ValidateSessionID(id); err != nil {
		s.writeReviewNotFound(w)
		return session.Session{}, false
	}
	sess, ok := s.deps.Service.Get(id, scope)
	if !ok {
		// Corrupted is consulted only when unscoped. A corrupt session.json
		// has no readable owner, so reporting 500 for one in hosted mode
		// would confirm that *some* review exists with this id — the
		// disclosure FR-4.2 exists to prevent. Hosted answers 404 either way;
		// the operator still sees the error LoadAll logged at startup.
		if !scope.IsScoped() && s.deps.Service.Corrupted(id) {
			s.deps.Log.Error("session record unreadable", slog.String("session", id))
			_ = jsonapi.WriteError(w, http.StatusInternalServerError, "GIT_FAILURE",
				jsonapi.StatusTitle(http.StatusInternalServerError), "This review's stored state could not be read.")
			return session.Session{}, false
		}
		s.writeReviewNotFound(w)
		return session.Session{}, false
	}
	return sess, true
}

func (s *server) writeReviewNotFound(w http.ResponseWriter) {
	_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND",
		jsonapi.StatusTitle(http.StatusNotFound), "No review exists with that id.")
}
```

`deleteReview` is the one handler that makes an explicit visibility decision, which deviates from design §7's "no handler writes a 403-vs-404 decision". The reason is standalone equivalence:

```go
// deleteReview is idempotent.
//
// In standalone mode it answers 204 for any id, unchanged. In hosted mode an
// id the caller cannot see answers 404 — which is the same answer an unknown
// id gets, so nothing is disclosed (FR-4.2).
//
// This is the only handler that inspects visibility itself. Everywhere else
// the scoped store's "not found" flows into the existing ErrNotFound -> 404
// mapping. The exception exists because the pre-hosted contract for DELETE is
// an unconditional 204, and requirement #1 is that standalone behaviour does
// not change.
func (s *server) deleteReview(w http.ResponseWriter, r *http.Request) {
	scope := scopeFrom(r)
	id := r.PathValue("id")
	if err := workspace.ValidateSessionID(id); err != nil {
		if scope.IsScoped() {
			s.writeReviewNotFound(w)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if scope.IsScoped() {
		if _, ok := s.deps.Service.Get(id, scope); !ok {
			s.writeReviewNotFound(w)
			return
		}
	}
	if err := s.deps.Service.Finish(r.Context(), id, scope); err != nil && !errors.Is(err, session.ErrNotFound) {
		s.deps.Log.Warn("finish failed", "session", id, "error", err)
	}
	w.WriteHeader(http.StatusNoContent)
}
```

`review_files.go` — `listReviewFiles`, `writeFileDiff`, and `getReviewDiff` pass `scopeFrom(r)` into `Service.Files`, `FileDiff`, and `CombinedDiffPath`.

- [ ] **Step 4: Register routes by mode**

In `NewRouter`, after the existing `GET /healthz` registration:

```go
	// Registered in both modes and always unauthenticated: this is the SPA's
	// first call.
	mux.HandleFunc("GET /api/auth/mode", s.authMode)
	// ... existing provider/review registrations, unchanged ...

	if d.Mode == config.ModeHosted {
		// Registered only in hosted mode. In standalone the existing "/api/"
		// catch-all below produces the 404s FR-4.3 requires, so no handler
		// contains a "return 404 in standalone" branch — the absence of a
		// route *is* the behaviour.
		mux.HandleFunc("POST /api/auth/register", s.register)
		mux.HandleFunc("POST /api/auth/login", s.login)
		mux.HandleFunc("POST /api/auth/logout", s.logout)
		mux.HandleFunc("GET /api/auth/me", s.currentUser)
		mux.HandleFunc("DELETE /api/auth/me", s.deleteAccount)
		mux.HandleFunc("POST /api/auth/password", s.changePassword)
		mux.HandleFunc("GET /api/settings/providers", s.listUserProviders)
		mux.HandleFunc("POST /api/settings/providers", s.createUserProvider)
		mux.HandleFunc("PATCH /api/settings/providers/{id}", s.updateUserProvider)
		mux.HandleFunc("DELETE /api/settings/providers/{id}", s.deleteUserProvider)
	}
```

and at the end, wrap in the documented order:

```go
	var handler http.Handler = mux
	if d.Mode == config.ModeHosted {
		// Innermost first: authenticate wraps the mux, originGuard wraps
		// authenticate, so the guard runs first. Constructed only in hosted
		// mode, so standalone's request path gains exactly zero comparisons
		// (design §7).
		handler = s.authenticate(handler)
		handler = s.originGuard(handler)
	}
	return withMiddleware(handler, d.Log)
```

Add the auth sweeper goroutine next to the existing session sweeper:

```go
	if d.AuthSweep != nil && d.CleanupInterval > 0 {
		d.Background.Add(1)
		go func() {
			defer d.Background.Done()
			ticker := time.NewTicker(d.CleanupInterval)
			defer ticker.Stop()
			for {
				select {
				case <-buildCtx.Done():
					return
				case <-ticker.C:
					if err := d.AuthSweep(buildCtx); err != nil {
						d.Log.Warn("auth sweep failed", slog.String("error", err.Error()))
					}
				}
			}
		}()
	}
```

- [ ] **Step 5: Extend `/healthz`**

```go
func (s *server) health(w http.ResponseWriter, r *http.Request) {
	git := "ok"
	if _, err := exec.LookPath("git"); err != nil {
		git = "missing"
	}
	checks := map[string]string{"git": git}
	// The key is absent in standalone mode, so the response is byte-identical
	// to the previous release there.
	if s.deps.DBPing != nil {
		checks["database"] = "ok"
		if err := s.deps.DBPing(r.Context()); err != nil {
			checks["database"] = "unreachable"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: buildinfo.Version, Checks: checks}); err != nil {
		s.deps.Log.Error("write health failed", "error", err)
	}
}
```

The `health` handler now takes `r`; update its `mux.HandleFunc` registration if the signature was previously `(w, _)`.

- [ ] **Step 6: Run the full suite**

```bash
cd apps/backend && go vet ./... && go test -race -count=1 -timeout 600s ./... && go test -race -count=1 -tags integration ./... && go tool golangci-lint run && CGO_ENABLED=0 go build ./...
```

Expected: PASS, including every pre-existing api test with no modification.

- [ ] **Step 7: Commit**

```bash
git add apps/backend/internal/api
git commit -m "$(cat <<'EOF'
feat(api): scope the review handlers and register auth routes by mode

In standalone mode the absence of a route *is* the 404 behaviour FR-4.3
requires, so no handler contains a mode branch. deleteReview is the single
exception that inspects visibility itself, because the pre-hosted contract for
DELETE is an unconditional 204 and standalone equivalence outranks tidiness;
the reason is recorded at the call site.

sessionFor consults Corrupted only when unscoped: a corrupt record has no
readable owner, so a 500 in hosted mode would confirm the id exists.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 20: `app` and `cmd` — the wiring

**Files:**
- Modify: `apps/backend/internal/app/app.go`, `apps/backend/cmd/converge/main.go`
- Test: `apps/backend/internal/app/app_test.go` (new functions only)

**Interfaces:**
- Consumes: everything above.
- Produces: `app.App` gains `DB *sql.DB`, `Auth *auth.Service`, `Resolver provider.Resolver`, `ProviderResolver *auth.ProviderResolver`; `App.Close` closes the database.

- [ ] **Step 1: Write the failing test**

Add to `apps/backend/internal/app/app_test.go`:

1. **`TestStandaloneCreatesNoDatabaseFile`** — `app.New` with only the existing standalone env plus a temp `WORKSPACE_ROOT`/`REPOSITORY_CACHE_ROOT`; assert `App.DB` is nil, `App.Auth` is nil, and the default `/data/converge.db` path was never touched. Additionally set `CONVERGE_DATABASE_PATH` to a temp path and assert **no file exists there** — proving standalone does not open a database even when the variable is set (FR-1.5, acceptance criterion).
2. **`TestHostedOpensAndMigratesTheDatabase`** — `CONVERGE_MODE=hosted` + `CONVERGE_SECRET_KEY` + a temp `CONVERGE_DATABASE_PATH`; assert the file exists, `App.Auth` is non-nil, `schema_migrations` has one row, and `App.Close()` returns nil and closes the handle.
3. **`TestHostedWarnsAboutIgnoredProviderVariables`** — capture the logger into a buffer; assert exactly one `WARN` naming `PROVIDERS__GH__TYPE` and `PROVIDERS__GH__TOKEN`, and that the **token value** appears nowhere in the buffer (FR-1.3).
4. **`TestHostedLogsTheStartupSummary`** — assert one line carrying `mode=hosted`, the database path, a user count, and an unowned-session count (NFR observability, FR-6.3).
5. **`TestHostedUsesTheDatabaseBackedResolver`** — assert `App.Resolver` is the `*auth.ProviderResolver` (type-assert) and that `Resolve(ctx, identity.Standalone())` errors, i.e. the static resolver is genuinely not in play.
6. **`TestMissingSecretKeyIsAStartupError`** — `CONVERGE_MODE=hosted` alone; `app.New` returns a `*config.Error` naming `CONVERGE_SECRET_KEY` and leaves no database file (acceptance criterion).

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/backend && go test ./internal/app/...
```

- [ ] **Step 3: Wire `app.New`**

In `internal/app/app.go`, extend the `App` struct:

```go
	// DB, Auth, and ProviderResolver are non-nil only in hosted mode. In
	// standalone mode no database file is created, opened, or required
	// (FR-1.5).
	DB               *sql.DB
	Auth             *auth.Service
	ProviderResolver *auth.ProviderResolver
	// Resolver is the seam review.Service and api use. It is a static wrapper
	// around Registry in standalone mode and ProviderResolver in hosted mode.
	Resolver provider.Resolver
```

`Close` closes the database last, after the background drain:

```go
func (a *App) Close() error {
	// ... existing runner/background shutdown, unchanged ...
	if a.DB != nil {
		if err := a.DB.Close(); err != nil {
			// Joined rather than returned early so a database close failure
			// does not mask a runner or drain failure.
			errs = append(errs, fmt.Errorf("close database: %w", err))
		}
	}
	return errors.Join(errs...)
}
```

> **Implementer note:** read the existing `Close` body first and fold the database close into whatever error-collection shape it already uses.

In `New`, after the registry loop and before `review.NewService`:

```go
	// Standalone: one static registry for everyone, exactly as before.
	var resolver provider.Resolver = provider.NewStaticResolver(registry)
	if len(cfg.IgnoredProviderVars) > 0 {
		// One WARN, names only, never values (FR-1.3).
		log.Warn("ignoring PROVIDERS__* in hosted mode; providers are configured per user",
			slog.Any("variables", cfg.IgnoredProviderVars))
	}
```

and after `store`, `cleaner`, and `service` are built (the auth service needs `service` as its `Purger` and `ProviderUsage`, and the resolver must be installed on `service` — so build the resolver *before* `service`, then the auth service *after*):

```go
	var (
		handle           *sql.DB
		authService      *auth.Service
		providerResolver *auth.ProviderResolver
		// authDeps is filled in here but consumed below, after
		// review.NewService exists: *review.Service is both the Purger and
		// the ProviderUsage.
		authDeps auth.ServiceDeps
	)
	if cfg.Mode == config.ModeHosted {
		sealer, err := auth.NewSealer(cfg.SecretKey)
		if err != nil {
			_ = runner.Close()
			return nil, err
		}
		if handle, err = db.Open(ctx, db.Options{Path: cfg.DatabasePath}); err != nil {
			_ = runner.Close()
			return nil, err
		}
		if err := db.Migrate(ctx, handle); err != nil {
			_ = handle.Close()
			_ = runner.Close()
			return nil, err
		}
		authStore := auth.NewStore(handle)
		providerResolver = auth.NewProviderResolver(authStore, sealer, httpClient, time.Now)
		// The hosted resolver replaces the static one before review.Service
		// is constructed, so nothing ever holds the wrong seam.
		resolver = providerResolver
		authDeps = auth.ServiceDeps{
			Store:      authStore,
			Sealer:     sealer,
			Throttle:   auth.NewThrottle(authStore, time.Now),
			Resolver:   providerResolver,
			Verifier:   auth.NewHTTPVerifier(httpClient, time.Now),
			Log:        log,
			Now:        time.Now,
			SessionTTL: cfg.LoginSessionTTL,
			IdleTTL:    cfg.LoginIdleTTL,
			// Purger and Usage are filled in below: both are *review.Service,
			// which does not exist yet. This is the one place the cascade's
			// three stores meet, and internal/app is the only package that
			// may know about all three.
		}
	}

	// ... existing locks / mirrors / workspaces / background / cleaner / store ...

	service := review.NewService(review.Deps{
		Providers: resolver, // was: registry
		// ... the rest unchanged ...
	})

	if cfg.Mode == config.ModeHosted {
		authDeps.Purger = service
		authDeps.Usage = service
		authService = auth.NewService(authDeps)
	}
```

Replace the final startup log with a mode-aware pair:

```go
	if err := store.LoadAll(ctx); err != nil {
		if handle != nil {
			_ = handle.Close()
		}
		_ = runner.Close()
		return nil, err
	}
	log.Info("converge starting",
		slog.String("git_version", version),
		slog.String("mode", string(cfg.Mode)),
		slog.Int("providers", len(cfg.Providers)))
	if cfg.Mode == config.ModeHosted {
		users, err := authService.CountUsers(ctx)
		if err != nil {
			_ = handle.Close()
			_ = runner.Close()
			return nil, err
		}
		// The unowned count is surfaced once so an operator can clean those
		// sessions up by hand: in hosted mode they are invisible to every
		// user and are never reassigned (FR-6.3).
		log.Info("hosted mode ready",
			slog.String("database", cfg.DatabasePath),
			slog.Int("users", users),
			slog.Int("unowned_sessions", store.Unowned()))
	}
```

Return the new fields on `&App{...}`.

> **Implementer note:** `authDeps` is declared with the `var` block above so it is in scope after `service` is built. If that reads awkwardly against the existing function's shape, extract the hosted setup into a private `openHosted(ctx, cfg, log, httpClient) (*sql.DB, *auth.ProviderResolver, auth.ServiceDeps, error)` helper and call it from `New`. Either is fine; the ordering constraint — resolver before `review.NewService`, auth service after — is what matters.

- [ ] **Step 4: Wire `cmd/converge`**

In `buildDeps`:

```go
	return api.Deps{
		Service:         application.Service,
		Providers:       application.Resolver, // was: application.Registry
		Mode:            application.Config.Mode,
		Auth:            application.Auth,
		SecureCookies:   application.Config.SecureCookies,
		TrustedProxy:    application.Config.TrustedProxy,
		LoginSessionTTL: application.Config.LoginSessionTTL,
		AuthSweep:       authSweep(application),
		DBPing:          dbPing(application),
		// ... the existing Log / UI / UIPresent / BuildContext / Store /
		// CleanupInterval / Background fields, unchanged ...
	}
```

```go
// authSweep and dbPing return nil in standalone mode, which is how the router
// knows not to start the auth sweeper goroutine and not to add a database key
// to /healthz. A nil func is the mode signal; api never checks the mode for
// either.
func authSweep(a *app.App) func(context.Context) error {
	if a.Auth == nil {
		return nil
	}
	return a.Auth.Sweep
}

func dbPing(a *app.App) func(context.Context) error {
	if a.Auth == nil {
		return nil
	}
	return a.Auth.Ping
}
```

`cmd/converge-cli` needs **no change beyond compilation**: it stays standalone-only, wraps everything in `identity.Standalone()` and the static resolver, and never opens the database (design §11).

> **Note (corrected after implementation):** this cannot be verified by grepping the import
> graph. `cmd/converge-cli` calls `app.New`, and `internal/app` unconditionally imports
> `internal/auth` and `internal/db` (both are struct field types on `App` — `DB *sql.DB`,
> `Auth *auth.Service` — regardless of which mode ever runs). Go import graphs are static, so
>
> ```bash
> cd apps/backend && go list -deps ./cmd/converge-cli | grep -E 'converge/internal/(auth|db)$'
> ```
>
> reports both packages and this is unavoidable given Step 3's own code, which the plan also
> specifies verbatim — there is no way to satisfy both simultaneously without splitting hosted
> wiring out of the shared `app.New`, which is a real architectural change outside this task's
> scope. The property that actually matters — `cmd/converge-cli` never opens a database file at
> runtime — is behavioral, not structural, and is what the smoke test in Step 6 verifies.
> `app.New(ctx, os.Environ())` would still honor `CONVERGE_MODE=hosted` if that variable is set
> in the CLI's environment, since the CLI has no guard forcing standalone mode; that residual
> gap is a design question for a follow-up, not something this task's wiring can silently fix.

- [ ] **Step 5: Run everything**

```bash
cd apps/backend && go vet ./... && go test -race -count=1 -timeout 600s ./... && go test -race -count=1 -tags integration ./... && go tool golangci-lint run && CGO_ENABLED=0 go build ./...
```

- [ ] **Step 6: Smoke-test both modes by hand**

```bash
cd apps/backend && CGO_ENABLED=0 go build -o /tmp/converge ./cmd/converge
# Standalone: no database, no auth.
WORKSPACE_ROOT=/tmp/cw REPOSITORY_CACHE_ROOT=/tmp/cr \
  PROVIDERS__GH__TYPE=github PROVIDERS__GH__TOKEN=x \
  timeout 5 /tmp/converge 2>&1 | head -5
# Hosted: database created, mode logged.
WORKSPACE_ROOT=/tmp/hw REPOSITORY_CACHE_ROOT=/tmp/hr \
  CONVERGE_MODE=hosted CONVERGE_DATABASE_PATH=/tmp/hosted.db \
  CONVERGE_SECRET_KEY="$(head -c 32 /dev/urandom | base64)" \
  timeout 5 /tmp/converge 2>&1 | head -5
ls -l /tmp/hosted.db && rm -rf /tmp/converge /tmp/cw /tmp/cr /tmp/hw /tmp/hr /tmp/hosted.db*
```

Expected: the standalone run logs `mode=standalone` and creates no `.db`; the hosted run logs `mode=hosted` plus `hosted mode ready` and creates the file.

- [ ] **Step 7: Commit**

```bash
git add apps/backend
git commit -m "$(cat <<'EOF'
feat(app): wire hosted mode — database, auth service, and per-user resolver

internal/app is the only package that may know about all three stores the
account-deletion cascade spans, so it is where *review.Service is injected as
auth's Purger and ProviderUsage. The hosted resolver replaces the static one
before review.NewService is constructed, so nothing ever holds the wrong seam.

A nil AuthSweep/DBPing is the mode signal the router reads; internal/api never
inspects the mode for either.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 21: frontend — client, types, schemas, services, hooks

**Files:**
- Modify: `apps/frontend/src/lib/api/client.ts`
- Create: `apps/frontend/src/types/models/auth.ts`
- Create: `apps/frontend/src/lib/schemas/auth.ts`, `apps/frontend/src/lib/schemas/userProvider.ts`
- Create: `apps/frontend/src/services/api/auth.ts`, `apps/frontend/src/services/api/userProviders.ts`
- Modify: `apps/frontend/src/services/api/index.ts`
- Create: `apps/frontend/src/lib/hooks/api/useAuth.ts`, `apps/frontend/src/lib/hooks/api/useUserProviders.ts`
- Test: `apps/frontend/src/lib/api/__tests__/client.test.ts` (add), `src/lib/schemas/__tests__/auth.test.ts`, `src/lib/hooks/api/__tests__/useAuth.test.tsx`, `src/lib/hooks/api/__tests__/useUserProviders.test.tsx`

**Interfaces:**
- Produces:
  ```ts
  // client.ts
  export function setUnauthorizedHandler(handler: (() => void) | null): void;
  export async function apiPatch<T>(path: string, body: unknown): Promise<T>;
  export async function apiPostNoContent(path: string, body?: unknown): Promise<void>;
  export async function apiDeleteWithBody(path: string, body: unknown): Promise<void>;

  // types/models/auth.ts
  export type AuthModeName = "standalone" | "hosted";
  export interface AuthMode { mode: AuthModeName; registrationOpen: boolean }
  export interface CurrentUser { id: string; username: string; createdAt: string; providerCount: number }
  export type ProviderKind = "github" | "gitlab";
  export interface UserProvider {
    id: string; slug: string; displayName: string; kind: ProviderKind;
    baseUrl: string; tokenLast4: string; tokenSetAt: string; createdAt: string; updatedAt: string;
  }

  // services/api/auth.ts
  export const authService: {
    mode(): Promise<AuthMode>;
    me(): Promise<CurrentUser>;
    login(username: string, password: string): Promise<CurrentUser>;
    register(username: string, password: string): Promise<CurrentUser>;
    logout(): Promise<void>;
    changePassword(currentPassword: string, newPassword: string): Promise<void>;
    deleteAccount(password: string): Promise<void>;
  };

  // services/api/userProviders.ts
  export interface UserProviderCreate {
    slug: string; displayName: string; kind: ProviderKind; baseUrl: string; token: string; validate: boolean;
  }
  export interface UserProviderPatch {
    displayName?: string; kind?: ProviderKind; baseUrl?: string; token?: string; validate?: boolean;
  }
  export const userProvidersService: {
    list(): Promise<UserProvider[]>;
    create(input: UserProviderCreate): Promise<UserProvider>;
    update(id: string, patch: UserProviderPatch): Promise<UserProvider>;
    remove(id: string): Promise<void>;
  };

  // hooks
  export const authKeys: { mode: readonly ["auth","mode"]; me: readonly ["auth","me"] };
  export function useAuthMode(): UseQueryResult<AuthMode>;
  export function useCurrentUser(): UseQueryResult<CurrentUser>;
  export function useLogin(): UseMutationResult<CurrentUser, unknown, { username: string; password: string }>;
  export function useRegister(): UseMutationResult<CurrentUser, unknown, { username: string; password: string }>;
  export function useLogout(): UseMutationResult<void, unknown, void>;
  export function useChangePassword(): UseMutationResult<void, unknown, { currentPassword: string; newPassword: string }>;
  export function useDeleteAccount(): UseMutationResult<void, unknown, { password: string }>;
  export const userProviderKeys: { all: readonly ["userProviders"] };
  export function useUserProviders(): UseQueryResult<UserProvider[]>;
  export function useCreateUserProvider(): UseMutationResult<UserProvider, unknown, UserProviderCreate>;
  export function useUpdateUserProvider(): UseMutationResult<UserProvider, unknown, { id: string; patch: UserProviderPatch }>;
  export function useDeleteUserProvider(): UseMutationResult<void, unknown, string>;
  ```

- [ ] **Step 1: Write the failing tests**

Add to `src/lib/api/__tests__/client.test.ts`:

1. **`sends credentials on every request`** — spy on `fetch` and assert `credentials: "include"` is present on a `GET` and on a `POST`. Without it the browser never sends the `HttpOnly` cookie on a same-origin fetch configured this way.
2. **`invokes the unauthorized handler on 401 and still throws`** — register a handler via `setUnauthorizedHandler`, make a `401` request, assert the handler ran **once** and an `ApiError` with `status === 401` was thrown.
3. **`does not invoke the unauthorized handler on other failures`** — a `403` and a `500` leave the handler untouched.
4. **`setUnauthorizedHandler(null) detaches`** — after detaching, a `401` does not call the old handler.
5. **`apiPatch sends the JSON:API content type`** — assert `method: "PATCH"` and `Content-Type: application/vnd.api+json`.

Create `src/lib/schemas/__tests__/auth.test.ts` — the schemas mirror the server, so the tests mirror the server's tests:

6. `loginSchema` accepts `{username: "alice", password: "12345678"}`; rejects a 2-character username, a 33-character username, a username starting with `_`, and a 7-character password. Rejects nothing about composition — a password of eight lowercase letters is valid (FR-2.2).
7. `registerSchema` additionally requires `confirmPassword` to equal `password`, with the error attached to the `confirmPassword` path.
8. `changePasswordSchema` requires `currentPassword` non-empty, `newPassword` 8–1024, and `confirmPassword` matching.
9. `deleteAccountSchema` requires `password` non-empty and `confirmUsername` to equal a supplied expected username (implement as a factory `deleteAccountSchema(expectedUsername)`), erroring on the `confirmUsername` path (FR-8.6's typed confirmation).
10. `userProviderCreateSchema` accepts a valid slug/kind/baseUrl/token; rejects `"Bad_Slug"`, an unknown kind, `"/relative"` as a base URL, and an empty token. Accepts an empty `baseUrl` when `kind === "github"`.
11. `userProviderEditSchema` accepts an **empty** token (meaning "keep the current one") and a valid non-empty one.

Create `src/lib/hooks/api/__tests__/useAuth.test.tsx` and `useUserProviders.test.tsx` using the existing MSW `src/test/server.ts` setup and `queryWrapper` from `src/test/render.tsx`:

12. `useAuthMode` returns the parsed mode and does not refetch on a second mount (`staleTime: Infinity`).
13. `useCurrentUser` does **not** retry on a 401 — one request only. Retrying an unauthenticated read would delay the redirect and multiply the 401s.
14. `useLogin` success invalidates `["auth","me"]`; `useLogout` success **clears** the cache (assert a previously-cached `["providers"]` entry is gone, so one user's data is not visible after another logs in on the same browser).
15. `useCreateUserProvider`, `useUpdateUserProvider`, and `useDeleteUserProvider` each invalidate both `["userProviders"]` **and** `["providers"]` — the latter because `GET /api/providers` changes when settings change.

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/frontend && npm test
```

(If `npm` is missing: `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22`.)

- [ ] **Step 3: Modify `client.ts`**

```ts
/**
 * onUnauthorized is invoked when any request answers 401.
 *
 * A module-level callback rather than a thrown-and-caught event keeps this
 * file free of React and router imports, preserving its shape as a thin fetch
 * wrapper. AuthProvider registers the callback that clears the query cache and
 * navigates to /login.
 */
let onUnauthorized: (() => void) | null = null;

export function setUnauthorizedHandler(handler: (() => void) | null): void {
  onUnauthorized = handler;
}

async function request(path: string, init: RequestInit): Promise<Response> {
  const response = await fetch(path, {
    ...init,
    // The login session lives in an HttpOnly cookie, so it is invisible to
    // JS and must be sent by the browser. Nothing is read from or written to
    // localStorage or sessionStorage (FR-8.8).
    credentials: "include",
    headers: { Accept: JSON_API, ...(init.headers ?? {}) },
  });
  if (!response.ok) {
    const error = await toApiError(response);
    if (response.status === 401) {
      onUnauthorized?.();
    }
    throw error;
  }
  return response;
}

export async function apiPatch<T>(path: string, body: unknown): Promise<T> {
  const response = await request(path, {
    method: "PATCH",
    headers: { "Content-Type": JSON_API },
    body: JSON.stringify(body),
  });
  return (await response.json()) as T;
}

/** apiPostNoContent posts and discards a 204 response. */
export async function apiPostNoContent(path: string, body?: unknown): Promise<void> {
  await request(path, {
    method: "POST",
    ...(body === undefined
      ? {}
      : { headers: { "Content-Type": JSON_API }, body: JSON.stringify(body) }),
  });
}

/**
 * apiDeleteWithBody exists for DELETE /api/auth/me, which takes a body
 * because account deletion is irreversible and must be password-confirmed.
 */
export async function apiDeleteWithBody(path: string, body: unknown): Promise<void> {
  await request(path, {
    method: "DELETE",
    headers: { "Content-Type": JSON_API },
    body: JSON.stringify(body),
  });
}
```

- [ ] **Step 4: Write the types, schemas, services, and hooks**

`src/types/models/auth.ts` — the interfaces from the Interfaces block, each a `Resource<...>`-compatible shape matching `api-contracts.md`, following the existing `src/types/models/provider.ts` style.

`src/lib/schemas/auth.ts`:

```ts
import { z } from "zod";

/** Mirrors the backend username rule (FR-2.1). */
const username = z
  .string()
  .regex(
    /^[a-zA-Z0-9][a-zA-Z0-9._-]{2,31}$/,
    "3 to 32 characters, starting with a letter or digit; letters, digits, dots, underscores and hyphens.",
  );

/**
 * Mirrors the backend password rule (FR-2.2): length is the only requirement.
 * There are deliberately no composition rules here, because there are none on
 * the server — adding one would reject a password the server accepts.
 */
const password = z
  .string()
  .min(8, "At least 8 characters.")
  .max(1024, "At most 1024 characters.");

export const loginSchema = z.object({ username, password });
export type LoginFormData = z.infer<typeof loginSchema>;

export const registerSchema = loginSchema
  .extend({ confirmPassword: z.string() })
  .refine((v) => v.password === v.confirmPassword, {
    message: "The passwords do not match.",
    path: ["confirmPassword"],
  });
export type RegisterFormData = z.infer<typeof registerSchema>;

export const changePasswordSchema = z
  .object({
    currentPassword: z.string().min(1, "Enter your current password."),
    newPassword: password,
    confirmPassword: z.string(),
  })
  .refine((v) => v.newPassword === v.confirmPassword, {
    message: "The passwords do not match.",
    path: ["confirmPassword"],
  });
export type ChangePasswordFormData = z.infer<typeof changePasswordSchema>;

/**
 * deleteAccountSchema is a factory because the confirmation compares against
 * the signed-in username (FR-8.6): deletion is irreversible, so it takes a
 * typed confirmation rather than a checkbox.
 */
export function deleteAccountSchema(expectedUsername: string) {
  return z.object({
    password: z.string().min(1, "Enter your password."),
    confirmUsername: z.literal(expectedUsername, {
      message: `Type ${expectedUsername} to confirm.`,
    }),
  });
}
export type DeleteAccountFormData = { password: string; confirmUsername: string };
```

`src/lib/schemas/userProvider.ts`:

```ts
import { z } from "zod";

/** Mirrors the backend slug rule (FR-5.1). */
const slug = z
  .string()
  .regex(
    /^[a-z0-9][a-z0-9-]{0,31}$/,
    "1 to 32 characters of lowercase letters, digits and hyphens, starting with a letter or digit.",
  );

const kind = z.enum(["github", "gitlab"]);

/** Mirrors the backend base-URL rule (FR-5.2). */
const baseUrl = z
  .string()
  .refine((v) => v === "" || /^https?:\/\/[^\s/]+/.test(v), "Enter an absolute http or https URL.");

const providerFields = {
  slug,
  displayName: z.string().max(128, "At most 128 characters."),
  kind,
  baseUrl,
};

export const userProviderCreateSchema = z
  .object({ ...providerFields, token: z.string().min(1, "A token is required.") })
  .refine((v) => v.kind === "github" || v.baseUrl !== "", {
    message: "A GitLab provider needs a base URL.",
    path: ["baseUrl"],
  });
export type UserProviderCreateFormData = z.infer<typeof userProviderCreateSchema>;

/**
 * The edit form allows an empty token, which means "keep the current one"
 * (FR-5.5). The form never receives a token to render, so there is no
 * possibility of leaking one — FR-5.4 and FR-8.5 are the same mechanism seen
 * from two ends. slug is absent because it is immutable.
 */
export const userProviderEditSchema = z
  .object({
    displayName: providerFields.displayName,
    kind,
    baseUrl,
    token: z.string(),
  })
  .refine((v) => v.kind === "github" || v.baseUrl !== "", {
    message: "A GitLab provider needs a base URL.",
    path: ["baseUrl"],
  });
export type UserProviderEditFormData = z.infer<typeof userProviderEditSchema>;
```

`src/services/api/auth.ts` — a plain service object in the existing style:

```ts
import {
  apiDeleteWithBody,
  apiGet,
  apiPost,
  apiPostNoContent,
} from "@/lib/api/client";
import { unwrapOne, type Document } from "@/types/api/jsonapi";
import type { AuthMode, CurrentUser } from "@/types/models/auth";
import type { Resource } from "@/types/api/jsonapi";

type ModeResource = Resource<"modes", AuthMode>;
type UserResource = Resource<"users", Omit<CurrentUser, "id">>;

function toUser(r: UserResource): CurrentUser {
  return { id: r.id, ...r.attributes, providerCount: r.attributes.providerCount ?? 0 };
}

export const authService = {
  async mode(): Promise<AuthMode> {
    const doc = await apiGet<Document<ModeResource>>("/api/auth/mode");
    return unwrapOne(doc).attributes;
  },
  async me(): Promise<CurrentUser> {
    const doc = await apiGet<Document<UserResource>>("/api/auth/me");
    return toUser(unwrapOne(doc));
  },
  async login(username: string, password: string): Promise<CurrentUser> {
    const doc = await apiPost<Document<UserResource>>("/api/auth/login", {
      data: { type: "credentials", attributes: { username, password } },
    });
    return toUser(unwrapOne(doc));
  },
  async register(username: string, password: string): Promise<CurrentUser> {
    const doc = await apiPost<Document<UserResource>>("/api/auth/register", {
      data: { type: "credentials", attributes: { username, password } },
    });
    return toUser(unwrapOne(doc));
  },
  async logout(): Promise<void> {
    await apiPostNoContent("/api/auth/logout");
  },
  async changePassword(currentPassword: string, newPassword: string): Promise<void> {
    await apiPostNoContent("/api/auth/password", {
      data: { type: "passwords", attributes: { currentPassword, newPassword } },
    });
  },
  async deleteAccount(password: string): Promise<void> {
    await apiDeleteWithBody("/api/auth/me", {
      data: { type: "accountDeletions", attributes: { password } },
    });
  },
};
```

> **Implementer note:** `CurrentUser` needs `providerCount` optional on the wire (register and login omit it) but required in the model. Define the resource attributes type with `providerCount?: number` and default it to `0` in `toUser`, as above.

`src/services/api/userProviders.ts` — same shape, hitting `/api/settings/providers`, with `create`/`update` sending `{data: {type: "userProviders", attributes: {...}}}` and `update` using `apiPatch`. `update` must **omit** `token` from the attributes object entirely when the form's token field is empty, so the server sees an absent field.

`src/lib/hooks/api/useAuth.ts`:

```ts
export const authKeys = {
  mode: ["auth", "mode"] as const,
  me: ["auth", "me"] as const,
};

export function useAuthMode() {
  return useQuery({
    queryKey: authKeys.mode,
    queryFn: () => authService.mode(),
    // The mode is fixed for the process lifetime (FR-1.6), so this is fetched
    // once per page load and never again.
    staleTime: Infinity,
    retry: false,
  });
}

export function useCurrentUser() {
  return useQuery({
    queryKey: authKeys.me,
    queryFn: () => authService.me(),
    // No retry: a 401 here is the normal "not signed in" answer, and retrying
    // would delay the redirect and multiply the 401s.
    retry: false,
    staleTime: 60_000,
  });
}
```

Mutations invalidate `authKeys.me`; `useLogout` calls `queryClient.clear()` on success, so no cached data from the previous account survives in the same tab. The provider hooks invalidate both `userProviderKeys.all` and `providerKeys.all` (imported from `useProviders.ts`).

Re-export the new services and types from `src/services/api/index.ts`.

- [ ] **Step 5: Run and commit**

```bash
cd apps/frontend && npm run lint && npm run format:check && npm test
```

```bash
git add apps/frontend
git commit -m "$(cat <<'EOF'
feat(frontend): add the auth API layer and a 401 interceptor

The fetch wrapper gains credentials: "include" and a module-level
onUnauthorized callback — a callback rather than an event so client.ts stays
free of React and router imports. Nothing is written to localStorage: the
React Query entry for the current user *is* the auth state, which falls out of
the HttpOnly cookie rather than needing a rule (FR-8.8).

Zod schemas mirror the server's rules exactly, including the deliberate
absence of password composition requirements.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 22: frontend — mode gate, auth provider, route guard, account menu

**Files:**
- Create: `apps/frontend/src/components/auth/ModeGate.tsx`, `AuthProvider.tsx`, `RequireAuth.tsx`
- Create: `apps/frontend/src/components/layout/AccountMenu.tsx`
- Modify: `apps/frontend/src/components/layout/AppShell.tsx`, `src/App.tsx`, `src/routes.tsx`
- Test: `src/components/auth/__tests__/ModeGate.test.tsx`, `RequireAuth.test.tsx`, `src/components/layout/__tests__/AccountMenu.test.tsx`, and additions to `src/__tests__/App.test.tsx` / `routes.test.tsx`

**Interfaces:**
- Consumes: Task 21's hooks and `setUnauthorizedHandler`.
- Produces:
  ```tsx
  export function ModeGate({ children }: { children: ReactNode }): ReactElement;
  export function AuthProvider({ children }: { children: ReactNode }): ReactElement;
  export function RequireAuth({ children }: { children: ReactNode }): ReactElement;
  export function AccountMenu(): ReactElement;
  // AppShell gains an optional prop:
  export function AppShell({ children, right }: { children: ReactNode; right?: ReactNode }): ReactElement;
  // routes.tsx gains a prop:
  export function AppRoutes({ hosted }: { hosted: boolean }): ReactElement;
  ```

- [ ] **Step 1: Write the failing tests**

1. **`ModeGate renders the standalone tree with no auth provider`** — MSW returns `standalone`; assert the child renders, `/api/auth/me` was **never** requested, and no account menu is in the document (FR-8.1: the UI is identical to today).
2. **`ModeGate renders the hosted tree`** — MSW returns `hosted`; assert the account menu appears once the user resolves.
3. **`ModeGate shows a loading state then resolves`** — assert a skeleton/placeholder before the mode arrives and no flash of the login page in standalone.
4. **`ModeGate surfaces a mode fetch failure`** — a `500` from `/api/auth/mode` renders an error state, not a blank page. The SPA cannot guess the mode, so it must say so.
5. **`RequireAuth redirects an unauthenticated visitor to /login with next`** — render at `/reviews/abc` with `/api/auth/me` returning `401`; assert the location becomes `/login?next=%2Freviews%2Fabc` (FR-8.2, FR-8.3).
6. **`RequireAuth renders children for an authenticated user`**.
7. **`AuthProvider redirects on a 401 from any call`** — render an authenticated tree, then make an arbitrary API call answer `401`; assert navigation to `/login` and that the query cache was cleared (FR-8.3).
8. **`AuthProvider does not redirect when already on /login`** — otherwise the `401` from `/api/auth/me` on the login page loops.
9. **`AccountMenu shows the username and the three items`** — *Provider settings*, *Account settings*, *Log out* (FR-8.4).
10. **`AccountMenu logout navigates to /login`**.
11. **`standalone routes exclude the settings paths`** — `AppRoutes hosted={false}` renders the not-found state for `/settings/providers`, `/settings/account`, `/login`, and `/register` (FR-8.1: no reachable settings routes).
12. **`hosted routes include them`**.

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/frontend && npm test
```

- [ ] **Step 3: Write the components**

`ModeGate.tsx`:

```tsx
/**
 * ModeGate branches the whole tree on GET /api/auth/mode.
 *
 * Standalone resolves straight to today's tree: the same AppShell, the same
 * AppRoutes, no auth provider mounted, no extra network calls. Hosted mounts
 * the auth-aware shell. One conditional at the root beats a mode check in
 * every component (design §9).
 */
export function ModeGate({ children }: { children: ReactNode }): ReactElement {
  const { data, isPending, isError } = useAuthMode();
  if (isPending) {
    return <div className="p-10" aria-busy="true" />;
  }
  if (isError || !data) {
    return (
      <div className="mx-auto max-w-3xl p-10">
        <ErrorBanner message="Converge could not determine how this instance is configured." />
      </div>
    );
  }
  if (data.mode === "standalone") {
    return <>{children}</>;
  }
  return <AuthProvider>{children}</AuthProvider>;
}
```

`AuthProvider.tsx`:

```tsx
/**
 * AuthProvider registers the 401 handler and nothing else.
 *
 * There is no session state to hold: the cookie is HttpOnly and invisible to
 * JS, so the React Query entry for useCurrentUser *is* the auth state. A 401
 * clears it.
 */
export function AuthProvider({ children }: { children: ReactNode }): ReactElement {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();

  // A ref so the effect below does not re-register the handler on every
  // navigation, which would be a new closure per route change.
  const locationRef = useRef(location);
  locationRef.current = location;

  useEffect(() => {
    setUnauthorizedHandler(() => {
      const path = locationRef.current.pathname;
      // Already on an unauthenticated screen: the 401 from /api/auth/me is
      // the expected answer there, and redirecting would loop.
      if (path === "/login" || path === "/register") {
        return;
      }
      queryClient.clear();
      const next = encodeURIComponent(path + locationRef.current.search);
      void navigate(`/login?next=${next}`, { replace: true });
    });
    return () => setUnauthorizedHandler(null);
  }, [navigate, queryClient]);

  return <>{children}</>;
}
```

`RequireAuth.tsx`:

```tsx
/**
 * RequireAuth gates a hosted route. An unauthenticated visitor is redirected
 * to /login with the attempted path preserved, so a successful login returns
 * there (FR-8.2, FR-8.3).
 */
export function RequireAuth({ children }: { children: ReactNode }): ReactElement {
  const { data, isPending, isError } = useCurrentUser();
  const location = useLocation();
  if (isPending) {
    return <div className="p-10" aria-busy="true" />;
  }
  if (isError || !data) {
    const next = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?next=${next}`} replace />;
  }
  return <>{children}</>;
}
```

`AccountMenu.tsx` — a `dropdown-menu` (already in `src/components/ui/`) triggered by the username, with `Link`s to `/settings/providers` and `/settings/account` and a *Log out* item calling `useLogout().mutate()` then navigating to `/login`.

`AppShell.tsx` — add an optional `right` slot rendered beside `ThemeToggle`, so the shell gains no knowledge of auth:

```tsx
export function AppShell({ children, right }: { children: ReactNode; right?: ReactNode }) {
  // ...
      <div className="flex items-center gap-2">
        {right}
        <ThemeToggle />
      </div>
  // ...
}
```

`routes.tsx` — `AppRoutes({ hosted })` adds, only when `hosted`:

```tsx
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
      <Route
        path="/settings/providers"
        element={
          <RequireAuth>
            <ProviderSettingsPage />
          </RequireAuth>
        }
      />
      <Route
        path="/settings/account"
        element={
          <RequireAuth>
            <AccountSettingsPage />
          </RequireAuth>
        }
      />
```

and wraps the three existing routes in `<RequireAuth>` when `hosted` is true. Keep the standalone branch rendering the existing routes **unwrapped**, so standalone mounts no guard at all.

`App.tsx` — mount `ModeGate` outside `AppShell` so the shell itself can differ by mode:

```tsx
function ModeAwareApp() {
  const { data } = useAuthMode();
  const hosted = data?.mode === "hosted";
  return (
    <AppShell right={hosted ? <AccountMenu /> : undefined}>
      <AppRoutes hosted={hosted} />
    </AppShell>
  );
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <BrowserRouter>
          <ModeGate>
            <ModeAwareApp />
          </ModeGate>
          <ThemedToaster />
        </BrowserRouter>
      </ThemeProvider>
    </QueryClientProvider>
  );
}
```

`AccountMenu` calls `useCurrentUser`, which 401s for an unauthenticated hosted visitor — and `AuthProvider`'s handler then redirects to `/login`. On `/login` itself the handler is a no-op, so `AccountMenu` must render nothing when the user is not resolved. Handle that with an early `if (!data) return null;`.

- [ ] **Step 4: Run and commit**

```bash
cd apps/frontend && npm run lint && npm run format:check && npm test
```

```bash
git add apps/frontend
git commit -m "$(cat <<'EOF'
feat(frontend): add the mode gate, 401 interceptor, and route guard

ModeGate is the single root conditional: standalone resolves straight to
today's tree with no auth provider mounted and no extra network calls, which
is what makes "the UI renders exactly as before" checkable rather than hoped
for. AuthProvider holds no session state — the cookie is HttpOnly, so the
React Query entry for the current user *is* the auth state.

The 401 handler is a no-op on /login and /register, because the 401 from
/api/auth/me is the expected answer there and redirecting would loop.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 23: frontend — login and register pages

**Files:**
- Create: `apps/frontend/src/pages/LoginPage.tsx`, `apps/frontend/src/pages/RegisterPage.tsx`
- Test: `src/pages/__tests__/LoginPage.test.tsx`, `RegisterPage.test.tsx`

**Interfaces:**
- Consumes: Task 21's `useLogin`/`useRegister` and `loginSchema`/`registerSchema`; Task 22's routes.
- Produces: `LoginPage`, `RegisterPage`.

- [ ] **Step 1: Write the failing tests**

1. **`LoginPage submits and returns to next`** — render at `/login?next=%2Freviews%2Fabc`; fill and submit; assert the request body is `{data:{type:"credentials",attributes:{username,password}}}` and the location becomes `/reviews/abc` (acceptance criterion: returns to the attempted path).
2. **`LoginPage defaults to / when next is absent`**.
3. **`LoginPage rejects a next that is not a local path`** — `?next=https://evil.test/` navigates to `/`, not to the external URL. An unvalidated `next` is an open-redirect.
4. **`LoginPage shows client-side validation before any request`** — submit empty; assert both field errors render and `fetch` was never called.
5. **`LoginPage surfaces INVALID_CREDENTIALS as a form-level error`** — the message is the server's detail, and it is not attached to either field (the server deliberately does not say which is wrong).
6. **`LoginPage surfaces ACCOUNT_LOCKED`** — a `429` renders the lockout message.
7. **`LoginPage never puts a password in the DOM as plain text`** — assert the password input has `type="password"` and that `document.body.innerHTML` does not contain the typed value outside the input's value.
8. **`LoginPage links to register`** (FR-8.2).
9. **`RegisterPage submits and lands on provider settings`** — a new account has `providerCount: 0`, so a successful register navigates to `/settings/providers` rather than to an empty repository picker (FR-5.9).
10. **`RegisterPage shows a confirm-password mismatch`** — the error is on the `confirmPassword` field.
11. **`RegisterPage maps USERNAME_TAKEN onto the username field`** — FR-8.7: "server-side errors are surfaced on the relevant field where the error code identifies one".
12. **`RegisterPage maps WEAK_PASSWORD onto the password field`** and **`INVALID_USERNAME` onto the username field**.

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/frontend && npm test
```

- [ ] **Step 3: Implement**

Both pages use `react-hook-form` with `zodResolver`, per the frontend guidelines. Shared pieces:

```tsx
/**
 * safeNext returns a path to return to after a successful login, or "/".
 *
 * Only a same-origin absolute path is accepted. An unvalidated ?next is an
 * open redirect: a link to /login?next=https://evil.test would bounce a user
 * off the instance immediately after they authenticate.
 */
export function safeNext(search: string): string {
  const raw = new URLSearchParams(search).get("next");
  if (!raw || !raw.startsWith("/") || raw.startsWith("//")) {
    return "/";
  }
  return raw;
}

/**
 * applyServerError attaches a server error to the field its code identifies,
 * falling back to a form-level error (FR-8.7). INVALID_CREDENTIALS
 * deliberately has no field: the server does not say which of the two is
 * wrong, and guessing here would undo that.
 */
function applyServerError<T extends FieldValues>(
  error: unknown,
  setError: UseFormSetError<T>,
  fieldForCode: Partial<Record<string, Path<T>>>,
): void {
  const code = isApiError(error) ? error.code : "";
  const field = fieldForCode[code];
  const message = messageFor(error, "Something went wrong. Try again.");
  if (field) {
    setError(field, { type: "server", message });
    return;
  }
  setError("root" as Path<T>, { type: "server", message });
}
```

`LoginPage` passes `{}` (no code maps to a field) so every server error is form-level. `RegisterPage` passes
`{ USERNAME_TAKEN: "username", INVALID_USERNAME: "username", WEAK_PASSWORD: "password" }`.

Put `safeNext` and `applyServerError` in `src/lib/api/formErrors.ts` so both pages and Task 25's account page share them, and give `safeNext` its own unit test (`src/lib/api/__tests__/formErrors.test.ts`) covering `/`, `/a/b?c=d`, `//evil.test`, `https://evil.test`, and an absent parameter.

Token and password inputs are `type="password"` throughout. Neither page writes anything to `localStorage` or `sessionStorage`, and neither logs a form value (FR-8.8).

- [ ] **Step 4: Run and commit**

```bash
cd apps/frontend && npm run lint && npm run format:check && npm test
```

```bash
git add apps/frontend
git commit -m "$(cat <<'EOF'
feat(frontend): add the login and register pages

?next is validated to a same-origin absolute path before navigation: an
unvalidated one is an open redirect that fires the moment a user
authenticates. Server errors land on the field their code identifies;
INVALID_CREDENTIALS deliberately stays form-level, because the server does not
say which of the two is wrong and guessing here would undo that.

A fresh account routes to provider settings rather than to an empty repository
picker (FR-5.9).

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 24: frontend — provider settings page

**Files:**
- Create: `apps/frontend/src/pages/ProviderSettingsPage.tsx`
- Create: `apps/frontend/src/components/features/settings/UserProviderForm.tsx`, `UserProviderList.tsx`
- Test: `src/pages/__tests__/ProviderSettingsPage.test.tsx`, `src/components/features/settings/__tests__/UserProviderForm.test.tsx`

**Interfaces:**
- Consumes: Task 21's `useUserProviders` + mutations and `userProviderCreateSchema`/`userProviderEditSchema`.
- Produces: `ProviderSettingsPage`, `UserProviderForm`, `UserProviderList`.

- [ ] **Step 1: Write the failing tests**

1. **`lists providers with kind, display name, base URL and a masked token`** — the row shows `•••• 9f2c` (or equivalent) and **never** a full token (FR-8.5).
2. **`creates a provider`** — fill the form, submit, assert the POST body carries `slug`, `displayName`, `kind`, `baseUrl`, `token`, `validate`, and that the list refetches (acceptance criterion).
3. **`edits a provider`** — open edit; assert the token input is **empty on load** and its placeholder says the current token is kept if left blank (FR-8.5); submit without touching it and assert the PATCH body has **no `token` key at all**.
4. **`edits a provider and rotates the token`** — a non-empty token is sent.
5. **`never renders a stored token`** — the page's HTML never contains a value that looks like a token beyond the four-character tail; assert against a fixture whose `tokenLast4` is `9f2c` and whose full value is never sent by the server anyway (acceptance criterion).
6. **`deletes a provider`** — a confirmation step, then `DELETE`, then the list refetches.
7. **`surfaces PROVIDER_IN_USE on delete`** — the `409` message renders and the row stays.
8. **`surfaces PROVIDER_UNAUTHORIZED on create`** — the `422` message renders on the token field and the list is unchanged.
9. **`surfaces PROVIDER_SLUG_TAKEN on the slug field`**.
10. **`shows an empty state that points at adding a provider`** — a user with no providers sees a call to action, not an empty table (FR-5.9).
11. **`hides the slug field when editing`** — the slug is immutable, so it is displayed as text, not as an input.
12. **`does not require a base URL for github`** — leaving it blank submits `baseUrl: ""`, which the server defaults (FR-5.2).

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/frontend && npm test
```

- [ ] **Step 3: Implement**

`UserProviderForm` takes `mode: "create" | "edit"`, an optional `provider: UserProvider` for edit, and an `onSubmitted` callback. Key details:

```tsx
{/*
  The token input is always empty on load, type="password", and says so.
  The form never receives a token to render, because no API response carries
  one — FR-5.4 and FR-8.5 are the same mechanism seen from two ends.
*/}
<Input
  type="password"
  autoComplete="new-password"
  placeholder={
    mode === "edit" ? "Leave blank to keep the current token" : "Access token"
  }
  {...register("token")}
/>
```

On edit submit, build the patch by omitting the token entirely when blank:

```tsx
const patch: UserProviderPatch = {
  displayName: values.displayName,
  kind: values.kind,
  baseUrl: values.baseUrl,
  // Omitted, not sent as "", so the request body genuinely has no token key.
  // The server treats both the same (FR-5.5), but omitting keeps the wire
  // honest about intent and keeps the empty string out of any proxy log.
  ...(values.token !== "" ? { token: values.token } : {}),
};
```

Server errors map with Task 23's `applyServerError` and
`{ PROVIDER_SLUG_TAKEN: "slug", PROVIDER_UNAUTHORIZED: "token", VALIDATION_ERROR: undefined }`.

`UserProviderList` renders a `table` (the existing `src/components/ui/table.tsx`) with a masked token cell:

```tsx
{/* Only the four-character tail the server sends; there is nothing else to show. */}
<span className="font-mono text-muted-foreground">•••• {provider.tokenLast4}</span>
```

Delete uses a two-step confirm in the row (no new dialog primitive needed), and surfaces the `409` inline.

- [ ] **Step 4: Run and commit**

```bash
cd apps/frontend && npm run lint && npm run format:check && npm test
```

```bash
git add apps/frontend
git commit -m "$(cat <<'EOF'
feat(frontend): add the provider settings page

The edit form's token input loads empty and says the current token is kept if
left blank; a blank value omits the key from the PATCH body entirely. The form
never receives a token to render, because no API response carries one, so
there is nothing to leak.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 25: frontend — account settings page

**Files:**
- Create: `apps/frontend/src/pages/AccountSettingsPage.tsx`
- Test: `src/pages/__tests__/AccountSettingsPage.test.tsx`

**Interfaces:**
- Consumes: Task 21's `useCurrentUser`, `useChangePassword`, `useDeleteAccount`, `changePasswordSchema`, `deleteAccountSchema`.
- Produces: `AccountSettingsPage`.

- [ ] **Step 1: Write the failing tests**

1. **`changes the password`** — fill current/new/confirm, submit, assert the POST body shape and a success toast.
2. **`shows a confirm mismatch before any request`**.
3. **`maps INVALID_CREDENTIALS onto the current-password field`** and **`WEAK_PASSWORD` onto the new-password field**.
4. **`deletes the account behind a typed username confirmation`** — the delete button is disabled until `confirmUsername` matches the signed-in username exactly; a near-miss (`"alic"`, `"ALICE"`) keeps it disabled (FR-8.6).
5. **`deletion navigates to /login`** — after a `204`, the query cache is cleared and the location is `/login`.
6. **`maps INVALID_CREDENTIALS on delete onto the password field`**.
7. **`all three password inputs and the delete password are type="password"`**.
8. **`renders the username and created date`**.

- [ ] **Step 2: Run to verify they fail**

```bash
cd apps/frontend && npm test
```

- [ ] **Step 3: Implement**

Two independent `react-hook-form` forms on one page. The deletion form's resolver is built from the factory, memoised on the username:

```tsx
const { data: user } = useCurrentUser();
// The schema depends on the signed-in username, so it is rebuilt when that
// changes rather than captured once (FR-8.6).
const deleteResolver = useMemo(
  () => zodResolver(deleteAccountSchema(user?.username ?? "")),
  [user?.username],
);
```

Copy for the destructive section states plainly what is removed, matching FR-2.7: the account, its provider configurations, its review sessions and workspaces, and its repository mirrors — and that it cannot be undone, because there is no password reset and no administrator who can restore it.

- [ ] **Step 4: Run the full frontend gate and commit**

```bash
cd apps/frontend && npm run lint && npm run format:check && npm test && npm run build
```

`npm run build` writes into `apps/backend/internal/ui/dist`, so confirm `git status` shows no unexpected churn there beyond the normal build output.

```bash
git add apps/frontend
git commit -m "$(cat <<'EOF'
feat(frontend): add the account settings page

Deletion is gated on typing the exact username, with the Zod schema rebuilt
from the signed-in username rather than captured once. The copy names
everything that goes — configurations, sessions, workspaces, mirrors — and
that it cannot be undone, because there is no password reset and no
administrator who can restore it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 26: the two cross-cutting security assertions

These are the assertions design §10 singles out as most likely to regress silently, so each gets a dedicated test rather than being a property assumed of the other tests.

**Files:**
- Create: `apps/backend/internal/app/secrets_test.go`
- Create: `apps/backend/internal/api/no_token_test.go`

**Interfaces:** consumes everything; produces no new source.

- [ ] **Step 1: Write the no-secret-in-logs test**

Create `apps/backend/internal/app/secrets_test.go`:

```go
package app_test

// TestNoSecretReachesTheLog installs the redacting handler over a buffer,
// exercises register -> login -> provider create -> review create, and asserts
// the buffer contains no session token, password, provider token, or master
// key.
//
// This is the strongest available evidence for an NFR that is otherwise a
// claim: the redaction is a handler wrapper, so a single unwrapped logger
// anywhere would silently defeat it, and nothing else in the suite would fail.
func TestNoSecretReachesTheLog(t *testing.T) {
	// ...
}
```

Structure it as:

1. Build a `bytes.Buffer`, construct the logger through `app.NewLogger(buf, cfg)` so the redacting handler is the real one, and wire a hosted app with distinctive sentinel values:
   - password: `"sentinel-password-DO-NOT-LOG"`
   - provider token: `"sentinel-provider-token-DO-NOT-LOG"`
   - master key: a real 32-byte base64 key captured in a variable
2. Exercise, through the HTTP router so the request-logging middleware runs too: `POST /api/auth/register` → `POST /api/auth/login` → `POST /api/settings/providers` → `POST /api/reviews` (against a fake provider) → `GET /api/reviews` → `POST /api/auth/password` → `POST /api/auth/logout`.
3. Capture the session cookie token from the `Set-Cookie` headers as a fourth sentinel.
4. Assert, over `buf.String()`:
   ```go
   for _, secret := range []string{password, providerToken, masterKey, sessionToken} {
       if strings.Contains(logged, secret) {
           t.Errorf("a secret reached the log")  // deliberately does not echo it
       }
   }
   if logged == "" {
       t.Fatal("nothing was logged; the test is not exercising the logger")
   }
   ```
   The non-empty check matters: a test that passes because nothing was logged proves nothing.
5. Also assert the buffer **does** contain `user_id=` at least once, confirming the observability requirement (structured events carry the user id) is met in the same pass.

- [ ] **Step 2: Write the no-token-in-responses test**

Create `apps/backend/internal/api/no_token_test.go`:

```go
// TestNoResponseBodyEverContainsAToken captures every response body produced
// by a full hosted-mode exercise and asserts none contains the plaintext token
// fixture.
//
// FR-5.4 is a property of the whole API surface, not of one handler, so it is
// asserted over every response rather than route by route. A new endpoint that
// leaks a token fails here even if its own test does not check.
func TestNoResponseBodyEverContainsAToken(t *testing.T) {
	const token = "glpat-SENTINEL-TOKEN-VALUE-9f2c"
	// ...
}
```

Structure it as: a `capture` helper that records every `httptest.ResponseRecorder` body into a slice; a scripted walk over every hosted route (`/api/auth/mode`, register, login, me, `POST|GET|PATCH|DELETE /api/settings/providers`, `GET /api/providers`, `GET /api/providers/{p}/repositories`, `POST|GET /api/reviews`, `GET /api/reviews/{id}`, `/files`, `/diff`, password, logout, delete account); then:

```go
	for i, body := range bodies {
		if strings.Contains(body, token) {
			t.Errorf("response %d contained the provider token", i)
		}
		if strings.Contains(body, `"token"`) {
			t.Errorf("response %d contained a token attribute: %s", i, body)
		}
	}
	// Prove the mask is present, so the test is not passing vacuously.
	if !strings.Contains(strings.Join(bodies, ""), `"tokenLast4":"9f2c"`) {
		t.Fatal("no response carried tokenLast4; the exercise did not reach the provider endpoints")
	}
```

- [ ] **Step 3: Run both**

```bash
cd apps/backend && go test -race -count=1 -timeout 600s ./internal/app/... ./internal/api/... -run 'NoSecret|NoResponseBody' -v
```

Expected: PASS, with the vacuity guards confirming the exercises actually ran.

- [ ] **Step 4: Commit**

```bash
git add apps/backend
git commit -m "$(cat <<'EOF'
test: assert no secret reaches a log and no response carries a token

Both are properties of the whole system rather than of one function, so each
gets a dedicated test over a full hosted-mode exercise. Each carries a
vacuity guard — the log test fails if nothing was logged, the response test
fails if no response carried tokenLast4 — so a pass cannot come from the
exercise silently not running.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 27: the hosted-mode integration test

**Files:**
- Create: `apps/backend/internal/review/hosted_integration_test.go` (build tag `integration`)

**Interfaces:** consumes everything; produces no new source.

Read `apps/backend/internal/review/integration_test.go` first and reuse its local-git harness verbatim — it builds real repositories with `gitx` and no network, which is exactly what this needs.

- [ ] **Step 1: Write the test**

```go
//go:build integration

package review_test

// TestTwoUsersReviewTheSameRepositoryInSeparateMirrors is the isolation
// acceptance criterion at full depth: two users, two provider configurations,
// one repository, two mirror directories, two review sessions, and neither
// visible to the other.
func TestTwoUsersReviewTheSameRepositoryInSeparateMirrors(t *testing.T) {
	// ...
}

// TestHostedPruneDoesNotDeleteALiveReviewBranch is the FR-6.6 evidence.
//
// The design argues this holds by construction: namespacing changes which
// directory the refspec applies to, not the refspec, so
// gitx.ExcludeReviewRefspec (commit b8a8391) still protects the branches a
// live worktree has checked out. "Holds by construction" is a claim, not
// evidence, so it gets a test under a namespace.
func TestHostedPruneDoesNotDeleteALiveReviewBranch(t *testing.T) {
	// ...
}
```

The first test:

1. Stand up a hosted app over a temp database, workspace root, and cache root, with a local-git-backed fake provider registered per user through `POST /api/settings/providers` (or directly through `auth.Service.CreateProvider` if the fake cannot answer the HTTP verifier — pass `Validate: false`).
2. Both users create a review of the same repository and the same merged change, through `review.Service.Create` + `Build`.
3. Assert:
   - Both sessions reach `StatusReady`.
   - `REPOSITORY_CACHE_ROOT/users/<idA>/<slug>/<owner>/<repo>.git` and the equivalent for B **both** exist, and no mirror exists directly under `REPOSITORY_CACHE_ROOT/<slug>/` (acceptance criterion).
   - `Store.List(identity.ForUser(idA))` contains only A's session.
   - `Store.Get(bSessionID, identity.ForUser(idA))` returns `false`.
   - The two reviews produce byte-identical combined diffs — isolation must not change the *result*, only where it is computed.

The second test:

1. Build an owned review to `StatusReady`, so a worktree has a review branch checked out in the user's mirror.
2. Call `mirror.Cache.Ensure(ctx, nsA, provider, repo)` again, which is the prune path (`fetch --prune ... ExcludeReviewRefspec`).
3. Assert the review branch ref still resolves via `gitx` in that user's mirror, and that the session is still `StatusReady` and its files still readable.

- [ ] **Step 2: Run**

```bash
cd apps/backend && go test -race -count=1 -tags integration -timeout 900s ./internal/review/... -run 'TwoUsers|HostedPrune' -v
```

- [ ] **Step 3: Run the whole integration suite**

```bash
cd apps/backend && go test -race -count=1 -tags integration ./...
```

- [ ] **Step 4: Commit**

```bash
git add apps/backend
git commit -m "$(cat <<'EOF'
test(review): add the hosted-mode isolation and prune integration tests

Two users reviewing the same repository must land in two mirror directories
and produce byte-identical diffs: isolation changes where the work happens,
not the result. The prune test exists because "namespacing preserves
ExcludeReviewRefspec by construction" is a claim, and FR-6.6 deserves
evidence.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 28: documentation, ops, and the full build gate

**Files:**
- Modify: `README.md`, `.env.example`, `docker-compose.yml`, `CLAUDE.md`
- Create: `docs/hosted-mode.md`

**Interfaces:** documentation only.

- [ ] **Step 1: Document the two modes in `README.md`**

Add a *Modes* section covering:

- `standalone` (default): what it is, the fact that upgrading and changing nothing is a no-op, `PROVIDERS__*` unchanged.
- `hosted`: what it adds, the minimum two variables, and a generated-key example:
  ```sh
  CONVERGE_MODE=hosted
  CONVERGE_SECRET_KEY="$(head -c 32 /dev/urandom | base64)"
  ```
- A full table of the seven new variables with defaults, taken verbatim from Global Constraints.
- **Registration is open** (design §11, PRD open question 3): state explicitly that a hosted instance reachable from the internet should sit behind a VPN or reverse-proxy authentication, because any client that can reach it may create an account. This is the answer to the open question and it belongs in the docs, not just in the design.
- `CONVERGE_SECURE_COOKIES=true` when TLS terminates at a proxy, and `CONVERGE_TRUSTED_PROXY=true` only when a proxy is genuinely in front — otherwise `X-Forwarded-For` lets any client forge a fresh throttle key.

- [ ] **Step 2: Write `docs/hosted-mode.md`**

The operator guide, covering the four documented gaps this task deliberately leaves open (design §11):

1. **No password reset.** A user who forgets their password loses the account. There is no mailer and no administrator role. A `converge-cli` subcommand that resets a hash against the database file is a coherent follow-up but needs its own thinking about who may run it and what stops it from being an authentication bypass on a shared host.
2. **No way to remove another user.** Self-service deletion is the only removal path, so an abandoned account persists. Same reasoning.
3. **Master key rotation**, as a written procedure: stop the process; for each `user_providers` row, decrypt with the old key (AAD = `user_id \| id`) and re-encrypt with the new; write the new key; start. No command ships in this task. Note that the key is *not* recoverable from the database: losing it means every stored provider token is unreadable and every user must re-enter theirs.
4. **Per-user mirrors multiply disk use.** N users reviewing one repository means N mirrors. The existing per-mirror prune still runs; no cap ships, because a quota is a separate feature with its own UX question (what happens when a user hits it?).

Also document: unowned review sessions are invisible in hosted mode and the startup log reports their count, so an operator can remove those directories by hand; and `converge-cli` stays standalone-only and never opens the database.

- [ ] **Step 3: Update `.env.example` and `docker-compose.yml`**

Add the seven variables to `.env.example`, commented, with `CONVERGE_MODE=standalone` as the active default and the hosted ones commented out. In `docker-compose.yml`, add a commented hosted service block showing `CONVERGE_MODE`, `CONVERGE_SECRET_KEY`, and a volume for `CONVERGE_DATABASE_PATH`'s directory — the database must live on a persisted volume, or every restart loses every account.

- [ ] **Step 4: Record the invariant change in `CLAUDE.md`**

The "Architecture Notes" section states persistence is the filesystem only. Amend that line rather than leaving it false:

```markdown
- Persistence is the filesystem in standalone mode. Hosted mode
  (`CONVERGE_MODE=hosted`) adds a SQLite database at `CONVERGE_DATABASE_PATH`
  holding **only** users, login sessions, per-user provider configuration, and
  lockout counters; review sessions remain `session.json` files and
  `internal/session` keeps its storage responsibility. `internal/auth/store.go`
  is the only file in the repository that contains SQL.
- `internal/identity` and `internal/db` are leaves (stdlib only).
  `internal/auth` sits beside `provider`/`session` and may import
  `db`, `config`, `identity`, and `provider`; it must never import `review` or
  `session`, and `review` must never import `auth`. The account-deletion
  cascade crosses that boundary through the `auth.Purger` and
  `auth.ProviderUsage` interfaces, wired in `internal/app`.
```

- [ ] **Step 5: Run every gate from the repository root**

```bash
cd "$(git rev-parse --show-toplevel)" && make lint && make test && make test-integration && make build && make docker-build
```

All five must be clean. `make docker-build` is the one that proves `CGO_ENABLED=0` plus `modernc.org/sqlite` works in the image without a C toolchain — the design's risk #3.

- [ ] **Step 6: Verify every acceptance criterion**

Walk PRD §10 end to end and confirm each box has a passing test or a hand-verified result. Record the mapping in `docs/tasks/task-005-hosted-multi-user-mode/context.md` under *Acceptance criteria coverage*, one line per criterion naming the test function or the manual command. Any criterion without one is unfinished work, not a documentation gap.

- [ ] **Step 7: Commit**

```bash
git add README.md .env.example docker-compose.yml CLAUDE.md docs/
git commit -m "$(cat <<'EOF'
docs: document standalone and hosted modes

Amends CLAUDE.md's "persistence is the filesystem only" invariant rather than
leaving it false: hosted mode adds a bounded SQLite store and the note says
exactly what it may hold.

README states plainly that registration is open and a reachable hosted
instance belongs behind a VPN or proxy auth — the answer to PRD open question
3. docs/hosted-mode.md records the four gaps this task deliberately leaves
open, including the master-key rotation procedure and the fact that losing the
key makes every stored token unreadable.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 29: code review before the PR

Per `CLAUDE.md`'s "Code Review Before PR", this is not optional and is not skipped because the plan looks complete.

- [ ] **Step 1: Dispatch the reviewers**

Invoke `superpowers:requesting-code-review`, which dispatches the appropriate subset for this branch — all three apply here, since the task changed Go, TypeScript, and the plan itself:

- `plan-adherence-reviewer` — every task in this plan actually implemented, nothing silently skipped or deferred
- `backend-guidelines-reviewer` — DOM-*/SUB-*/SEC-* over the changed Go packages
- `frontend-guidelines-reviewer` — FE-* over the changed TypeScript

Each writes findings to `docs/tasks/task-005-hosted-multi-user-mode/audit.md`.

- [ ] **Step 2: Address Critical and Important findings**

Use `superpowers:receiving-code-review`: verify each finding before implementing it rather than agreeing performatively. A finding that is technically wrong gets a reasoned reply in `audit.md`, not a change.

- [ ] **Step 3: Re-run every gate after the fixes**

```bash
cd "$(git rev-parse --show-toplevel)" && make lint && make test && make test-integration && make build && make docker-build
```

- [ ] **Step 4: Commit the audit**

```bash
git add docs/tasks/task-005-hosted-multi-user-mode/audit.md
git commit -m "$(cat <<'EOF'
docs(task-005): record the code review audit

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Deliberate deviations from the design

Recorded here so a reviewer does not have to rediscover them. Each is a decision, not a drift.

1. **`schema_migrations` is created by the runner, not by `0001_init.sql`** (Task 3). It has to exist before the first migration can be recorded. PRD §6.2 lists it with the other tables; the shape is identical.
2. **`auth.NewID` returns 16 hex characters, not `session.NewID`'s 8** (Task 4). PRD §6.2 says "the same generator". Same scheme, double the width: these are long-lived primary keys, and a user id becomes a directory name under `REPOSITORY_CACHE_ROOT`, where a collision would merge two users' mirrors.
3. **`deleteReview` inspects visibility in the handler** (Task 19). Design §7 says no handler writes a 403-vs-404 decision. The exception exists because the pre-hosted contract for `DELETE /api/reviews/{id}` is an unconditional `204` and standalone equivalence is requirement #1; the hosted branch answers `404` for both unknown and foreign ids, so nothing is disclosed.
4. **`sessionFor` consults `Corrupted` only when unscoped** (Task 19). A corrupt `session.json` has no readable owner, so a `500` in hosted mode would confirm that some review exists with that id.
5. **`ProviderPatch.Validate` defaults to `false`** (Task 18). `api-contracts.md` lists `PROVIDER_UNAUTHORIZED` as a possible `PATCH` error but does not state the default. Re-verifying on every cosmetic edit would hit the provider API for a display-name change; the error stays reachable by sending `"validate": true`.
6. **`auth.Service` gains a `ProviderUsage` collaborator** alongside the `Purger` design §6 specifies (Tasks 14, 15). FR-5.7's `PROVIDER_IN_USE` check needs to ask `review` a question, and the same dependency-direction argument applies, so it takes the same shape.
7. **`mirror.Cache.PurgeNamespace` refuses the root namespace** (Task 11). Not in the design; added because a zero-value `Namespace` reaching it would delete every user's mirrors.

## Notes for the executor

- **Task order is a dependency order, not a preference.** Tasks 9–12 are the signature churn the design sequences early (§12); every later task assumes those signatures. Do not reorder them.
- **Two tasks deliberately leave a failing test behind.** Task 17's `TestAuthRoutesAre404InStandalone` and Task 18's `TestSettingsRoutesAre404InStandalone` are written with `t.Skip` and un-skipped in Task 19, where the conditional route registration lands. Do not delete them instead.
- **One test deliberately bypasses the package API.** Task 5's `TestVerifyReadsParamsFromTheString` calls `argon2.IDKey` directly to build a weak-parameter hash, because `HashPassword` always uses the package constants and so could never prove that verification reads parameters back. Do not "simplify" it into a `HashPassword` round-trip.
- **`make docker-build` early.** The design names `modernc.org/sqlite`'s size and build time as a risk and says to verify the image build early rather than at the end. Task 3 records the binary size; run `make docker-build` any time after Task 3 rather than waiting for Task 28.
- **The backend test suite is slow** once Argon2id at 64 MiB is in play. Pass an explicit `-timeout` (300s for `./internal/auth/...`, 600s for `./...`, 900s for the integration tags) rather than letting the default 10-minute timeout surprise you mid-task.
- **Context budget.** This plan is well beyond one session. Hand off at the natural boundaries — after Task 8 (the `auth` primitives), after Task 12 (the seams), after Task 20 (the backend is whole), after Task 25 (the frontend is whole) — by committing and letting the next session resume from the committed artifacts.

