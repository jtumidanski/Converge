# Hosted Multi-User Mode — Design

Status: Draft
Created: 2026-09-10
Inputs: `prd.md` (approved), `api-contracts.md`

---

## 1. What this design has to solve

The PRD is specific about behaviour and nearly silent about structure. The interesting
work is not "add a users table"; it is finding the smallest set of seams that let one
binary serve two mutually exclusive shapes without the standalone shape paying for the
hosted one — in code paths, in on-disk layout, or in the reader's head.

Four problems drive every decision below:

1. **Identity has to reach the bottom of the stack.** `mirror` picks a directory and
   `session.Store` filters a list. Both sit far below `api`, which is where the cookie is
   read. Something has to carry "who is asking" across four package boundaries without
   inverting the dependency direction (`api → review → {provider, mirror, workspace, diff,
   session} → gitx`).
2. **Providers stop being process-global.** Today `*provider.Registry` is built once in
   `app.New` from `cfg.Providers` and handed to `review.Service` and the API. In hosted
   mode the set of providers — and the tokens inside them — is a function of the caller.
3. **A second persistence store enters a codebase whose stated invariant is "filesystem
   only."** That invariant is worth keeping mostly true; the design has to make the
   exception legible and bounded rather than quietly general.
4. **Failure to isolate is a security bug, not a defect.** The enforcement points should
   be few, and the compiler should make it hard to bypass one.

---

## 2. Architecture at a glance

```
                       ┌────────────────────────────────────────────┐
   cookie ────────────▶│ api: origin check → authn → scope in ctx   │
                       └───────────────┬────────────────────────────┘
                                       │ identity.Scope (explicit arg)
                       ┌───────────────▼────────────────────────────┐
                       │ review.Service                             │
                       └───┬───────────────┬──────────────┬─────────┘
                           │               │              │
              provider.Resolver     mirror.Cache    session.Store
              (scope → *Registry)   (namespace)     (owner filter)
                           │
        ┌──────────────────┴───────────────────┐
        │ static (standalone)   dbBacked (hosted)│
        └────────────────────────────┬──────────┘
                                     │
                              internal/auth ──▶ internal/db (SQLite)
```

New packages: `internal/identity` (leaf), `internal/db` (leaf), `internal/auth`
(`→ db, config, identity`). Modified: `config`, `provider`, `mirror`, `session`, `review`,
`api`, `app`. Unchanged: `gitx`, `diff`, `workspace`, `jsonapi`, `cmd/converge-cli`.

The dependency rule in `CLAUDE.md` is preserved: `auth` is a sibling of `provider`/`session`
in the middle tier, `api` orchestrates it, and nothing below `review` learns that accounts
exist — they only learn that requests carry a scope.

---

## 3. Decision 1 — how identity travels

### Options

**A. `context.Context` all the way down.** `api` puts the user ID on the context; `mirror`
and `session` read it with a typed accessor.

**B. An explicit `identity.Scope` value passed as a function argument** from `api` into
`review`, and from `review` into its collaborators. Context carries the scope only across
the HTTP boundary (middleware → handler), per FR-4.5; below `api` it is an argument.

**C. Per-request object graph.** Construct a `review.Service` (and registry, and scoped
store view) per authenticated request.

### Decision: B

Context-carried identity is invisible in signatures, which is exactly wrong for a value
whose absence is a data leak. With B, `Store.List(scope)` cannot be called without the
caller stating a scope; adding the parameter is a compile error at every existing call
site, which is a one-time cost that buys a permanent guarantee. The `fatcontext` /
`containedctx` family of lint rules in this repo's `govet`/`revive` posture also reflects a
house preference against smuggling values through contexts.

C is rejected on cost: `review.Service` owns a build semaphore, a `WaitGroup`, and a
resolver cache. Rebuilding that per request either duplicates the limiter (breaking
`MAX_CONCURRENT_BUILDS`) or requires a shared core anyway, at which point C collapses into
B with extra allocation.

### `internal/identity`

A leaf package with no imports beyond the standard library, so every tier can use it:

```go
// Scope answers "on whose behalf". The zero value is the standalone scope:
// unowned, unfiltered, and identical to pre-hosted behaviour.
type Scope struct {
    userID string // empty in standalone mode
}

func Standalone() Scope                 { return Scope{} }
func ForUser(id string) Scope           { return Scope{userID: id} }
func (s Scope) UserID() string          { return s.userID }
func (s Scope) IsScoped() bool          { return s.userID != "" }
// Matches reports whether a record owned by owner is visible to s.
// Standalone: always true. Hosted: only an exact match (so "" is invisible).
func (s Scope) Matches(owner string) bool
```

`Matches` is where FR-6.3 and FR-6.4 live, in one function, with one table-driven test.
Everything else — store, mirror, API — calls it rather than re-deriving the rule. Note the
deliberate asymmetry: in standalone the zero scope sees everything including owned records;
in hosted an unowned record is invisible to everyone. Hosted scopes are only ever
constructed by the auth middleware from a verified session.

`Scope` is a value type with an unexported field, so it cannot be forged by struct literal
outside the package, and a zero value is a safe default rather than a dangerous one.

---

## 4. Decision 2 — provider resolution

### Options

**A. Make `*provider.Registry` user-aware** — `registry.Get(scope, id)`.
**B. Introduce `provider.Resolver`**: `Resolve(ctx, scope) (*Registry, error)`, with a
static implementation for standalone and a DB-backed, cached one for hosted.
**C. Build a fresh `*Registry` per request in hosted mode, no cache.**

### Decision: B, with a small cache

A conflates two responsibilities in a type that is currently 37 lines and pleasantly dumb.
C is correct but pays a DB read plus AES-GCM open per provider per request, including on
the hot repository-listing path.

```go
// provider.Resolver yields the registry visible to a scope.
type Resolver interface {
    Resolve(ctx context.Context, scope identity.Scope) (*Registry, error)
}

// Static wraps a prebuilt registry and ignores the scope. Standalone mode,
// the CLI, and every existing test use this.
func NewStaticResolver(r *Registry) Resolver
```

`review.Deps.Providers` changes from `*Registry` to `Resolver`; `api.Deps.Providers` does
the same. Every existing test and `cmd/converge-cli` adapt by wrapping their registry in
`NewStaticResolver`, a mechanical change.

The hosted resolver lives in `internal/auth` — not in `provider` — because it is the piece
that needs the DB and the decryption key, and `provider` must not gain either. It satisfies
`provider.Resolver` from the outside, which is the direction the dependency should run:

```go
// auth.ProviderResolver builds a *provider.Registry from user_providers.
// Imports: internal/db, internal/provider, internal/provider/{github,gitlab}.
type ProviderResolver struct { ... }
func (r *ProviderResolver) Resolve(ctx, scope) (*provider.Registry, error)
func (r *ProviderResolver) Invalidate(userID string)
```

**Cache shape.** `map[string]cacheEntry` under an `RWMutex`, keyed by user ID, holding the
built `*Registry` and the `updated_at` high-water mark it was built from. Every write path
(`POST`/`PATCH`/`DELETE /api/settings/providers`, and account deletion) calls `Invalidate`.
The cache is per-process and the process is the only writer of the database, so
invalidation is complete — there is no second node to miss the message. Entries are evicted
on invalidation and on a TTL (15 minutes) so an idle user's decrypted tokens do not sit in
memory forever. A `*Registry` is immutable after construction, so a reader holding a stale
pointer through an invalidation is safe; it finishes its request against the registry it
resolved.

**Decrypted token lifetime.** Tokens are decrypted at registry-build time and live inside
`config.Secret` values held by the `github`/`gitlab` clients — the same place standalone
mode keeps them. This is a real tradeoff: it keeps plaintext in memory for up to the cache
TTL instead of only for the duration of one call. The alternative (decrypt per call) buys
little, because the plaintext still reaches `GIT_CONFIG_VALUE_0` and an HTTP header, and it
costs a DB read on every provider operation. We take the cache and bound it with the TTL.

---

## 5. Decision 3 — the SQLite store

### Why a database at all

Alternatives weighed: (i) JSON files under a `users/` root, mirroring `session.json`;
(ii) an embedded KV store (bbolt); (iii) SQLite.

(i) is attractive for consistency but wrong for this data: the lockout counters are
read-modify-write under concurrency, username uniqueness needs an atomic check-and-insert,
and "delete a user and cascade their sessions and providers" is a transaction. Building
those on files means building a small, buggy database. (ii) gets transactions but not
uniqueness constraints or cascades, and adds a dependency with no SQL escape hatch for
operators. (iii) gives all of it, and a `.db` file an operator can inspect with `sqlite3`.

`modernc.org/sqlite` is mandatory over `mattn/go-sqlite3`: `CGO_ENABLED=0 go build ./...`
is a required gate and the Docker image is built without a C toolchain.

Worth naming explicitly: `go.mod` currently has **zero non-tool runtime dependencies**.
This task adds two — `modernc.org/sqlite` and `golang.org/x/crypto` (argon2). That is a
meaningful change in the project's supply-chain posture and should be a conscious
acceptance, not a side effect. Both are pure Go; `golang.org/x/crypto` is
Go-team-maintained; `modernc.org/sqlite` is large but widely used and CGO-free.

### `internal/db`

Deliberately thin, imports nothing from the module:

```go
type Options struct { Path string }
func Open(ctx context.Context, o Options) (*sql.DB, error) // pragmas + MaxOpenConns(1)
func Migrate(ctx context.Context, db *sql.DB) error        // embedded, forward-only
```

- Pragmas per PRD §6.1: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`,
  applied via DSN parameters so they cannot be missed on a reconnect.
- `MaxOpenConns(1)`. With WAL this serialises writes *and* reads, which is a real ceiling —
  but the workload is one or two indexed lookups per request and a rare Argon2id hash
  (which happens outside any transaction, on the login path, and must not hold the
  connection). Trading throughput for the removal of `SQLITE_BUSY` retry logic from every
  call site is right for a tool whose concurrency ceiling is already
  `MAX_CONCURRENT_BUILDS=4`. If it ever bites, the fix is a second read pool, not a
  rewrite.
- Migrations are `//go:embed migrations/*.sql`, ordered by numeric prefix, applied inside a
  single transaction that also inserts into `schema_migrations`. A file whose version is
  already recorded is skipped. A recorded version with no file on disk is a fatal error
  (the binary is older than the database).

Schema is exactly as specified in PRD §6.2. Two additions worth flagging during
implementation review: `login_attempts` needs an index on `locked_until` for the sweeper,
and `user_providers` needs `(user_id)` covered by the `UNIQUE (user_id, slug)` index for
the per-user list query (it is — SQLite uses the leftmost prefix).

### `internal/auth`

One package, several files, each with one job:

| File | Responsibility |
| --- | --- |
| `model.go` | `User`, `LoginSession`, `UserProvider` — immutable values with constructors, matching the `session` package's style |
| `password.go` | Argon2id hash/verify, PHC encode/decode, `Params` in one place |
| `crypt.go` | AES-256-GCM seal/open with AAD = `user_id \| provider_id` |
| `store.go` | All SQL. The only file in the repo that writes SQL |
| `throttle.go` | Failure counters and the doubling lockout schedule |
| `service.go` | Register / Login / Logout / ChangePassword / DeleteAccount / provider CRUD |
| `resolver.go` | `ProviderResolver` (§4) |
| `sweep.go` | `Sweep(ctx)` — expired login sessions and elapsed lockouts |

`auth.Service` returns a typed `*auth.Error{Code, Message}` mirroring `session.ReviewError`,
so `api.classify` gains one `errors.As` arm rather than ten `errors.Is` cases.

**Timing equalisation (FR-2.5).** `Login` on an unknown username verifies against a
package-level dummy PHC hash generated once at init from a random password. Costs are then
equal in the dominant term (one Argon2id verify); the residual difference (one extra index
lookup on the hit path) is nanoseconds against ~50 ms of hashing.

**Argon2id memory.** 64 MiB per concurrent hash. Five simultaneous registrations is 320 MiB
transient. The per-IP registration throttle (FR-7.5) is the bound, but it is a counter, not
a concurrency limiter. We add a **semaphore of 4 concurrent Argon2id operations** in
`auth.Service`; callers beyond it queue. This is a cheap, explicit cap that turns a memory
spike into latency.

---

## 6. Decision 4 — ownership enforcement

### Session store

`session.Session` gains an unexported `owner` field, `Owner()` accessor,
`Builder.SetOwner`, and `Record.Owner string \`json:"owner,omitempty"\``. `SchemaVersion`
stays `1`: an added optional field that absent-decodes to the zero value is not a schema
break, and bumping it would force a migration path for records that need none.

The store's read and mutate methods take a scope:

```go
func (s *Store) Get(id string, scope identity.Scope) (Session, bool)
func (s *Store) List(scope identity.Scope) []Session
func (s *Store) Finish(ctx context.Context, id string, scope identity.Scope) error
```

Rejected alternative: a `ScopedStore` wrapper returned by `store.For(scope)`. It reads
nicely but leaves the unscoped methods reachable, and the first time someone calls the
inner store directly the isolation is gone with no compile error. Changing the signatures
makes every existing call site declare itself — `cmd/converge-cli` and tests pass
`identity.Standalone()`, which is both correct and self-documenting.

`Sweep` remains unscoped (it is the system acting, not a user) and keeps its current
behaviour; expiry is not an authorisation decision.

**Unowned sessions in hosted mode.** `Matches` makes them invisible. `LoadAll` counts them
and `app.New` logs the count once at startup (FR-6.3). They are never swept early and never
reassigned — an operator deletes them by hand.

### Mirrors

`mirror.Cache.Path` gains a namespace:

```go
func (c *Cache) Path(ns Namespace, providerID, fullName string) (string, error)
```

where `Namespace` is derived from a scope: empty → today's `<root>/<providerID>/...`;
scoped → `<root>/users/<userID>/<providerID>/...`. The user ID segment is validated against
the same identifier regexp the IDs are generated from, so a crafted ID cannot escape the
root — this is the same defensive posture `providerIDRe` and `ValidateRepoFullName` already
take. `Ensure` and `FetchSHA` take the namespace and pass it to `Path`; the per-mirror lock
key is the resolved path, so two users' mirrors of the same repository lock independently,
which is what we want.

Prune's live-branch protection (`gitx.ExcludeReviewRefspec`, commit `b8a8391`) is expressed
in the refspec of a specific mirror directory. Namespacing changes which directory, not the
refspec, so FR-6.6 holds by construction — but it gets an explicit hosted-mode integration
test because "holds by construction" is a claim, not evidence.

**Cost accepted:** N users reviewing the same repository means N mirrors. The PRD's open
question 5 is answered *here* as: no cap in this task. Existing per-mirror prune still runs;
a quota is a separate feature with its own UX (what happens when a user hits it?) and
inventing one now would be speculative.

### Account deletion cascade

The cascade spans three stores, and no single existing package may own all three:
`auth` must not import `review` (wrong direction), and `review` must not import `auth`
(drags SQLite into the review tier).

Resolution: `auth.Service.DeleteAccount` takes a `Purger` collaborator injected at wiring
time:

```go
// auth.Purger removes a user's filesystem-resident state. Implemented in
// internal/review (which already owns Cleaner) and wired in internal/app.
type Purger interface { PurgeUser(ctx context.Context, userID string) error }
```

Order is deliberate and matches the contract in `api-contracts.md`: verify password →
delete DB rows in one transaction (cascade handles login sessions, providers, lockouts) →
invalidate the provider cache → call `PurgeUser`. A `PurgeUser` failure is logged at `ERROR`
with the orphaned paths and the request still returns `204`. Reversing the order would risk
an account that has lost its data but can still log in, which is strictly worse than an
account that is gone with orphaned bytes on disk.

---

## 7. The API layer

### Middleware order

```
requestID → recover/log → Accept negotiation      (existing, both modes)
  → originGuard                                   (hosted only, non-GET/HEAD)
  → authenticate                                  (hosted only, non-public routes)
  → mux
```

`originGuard` before `authenticate` is required by the contract (a cross-site request must
not reach a handler) and is also cheaper. Both are constructed only in hosted mode, so
standalone's request path gains exactly zero comparisons — an empty-interface check per
request is not "free enough" to justify a single code path when the PRD's first acceptance
criterion is byte-for-byte standalone equivalence.

`authenticate` puts an `identity.Scope` on the request context; handlers read it through
`scopeFrom(r)`, which returns `identity.Standalone()` when absent. That is the one place
context carries identity (FR-4.5), and it is a 5-line function with a test proving the
default.

### Route registration

`NewRouter` registers the auth and settings routes only when `d.Mode == config.ModeHosted`.
The existing `/api/` catch-all then produces the `404`s FR-4.3 requires, with no explicit
"return 404 in standalone" branches in any handler — the absence of a route *is* the
behaviour. `GET /api/auth/mode` is registered in both modes.

### Error mapping

`classify` gains one arm:

```go
var ae *auth.Error
if errors.As(err, &ae) { return ae.Status(), string(ae.Code), ae.Message }
```

`ACCOUNT_LOCKED` additionally needs a `Retry-After` header, which `classify` cannot set
because it only returns a triple. The login handler therefore checks for a lockout error
explicitly before delegating — a small, local exception, documented at the call site.

### Cross-user 404 (FR-4.2)

Enforced by the store returning "not found" for a non-matching owner, which flows into the
existing `session.ErrNotFound` → 404 mapping. No handler writes a `403`-vs-`404` decision;
the isolation rule and the HTTP shape are one mechanism, not two that could drift.

---

## 8. Configuration

`config.Config` gains:

```go
Mode            Mode            // ModeStandalone | ModeHosted
DatabasePath    string          // CONVERGE_DATABASE_PATH, default /data/converge.db
SecretKey       Secret          // CONVERGE_SECRET_KEY, 32 bytes base64
SecureCookies   bool
TrustedProxy    bool
LoginSessionTTL time.Duration
LoginIdleTTL    time.Duration
```

`Load` parses `CONVERGE_MODE` **first**, because it changes whether `PROVIDERS__*` is
required and whether `CONVERGE_SECRET_KEY` is read. `loadProviders` gains a `required bool`
parameter rather than a second function: the parsing is identical, only the
"at least one" check is conditional. In hosted mode, present `PROVIDERS__*` variables are
collected and their names emitted in one `WARN` (FR-1.3) — names only, never values.

`Secrets()` appends the master key when set, so the redacting handler scrubs it from every
log line (a base64 key appearing in an error string is exactly the accident this exists for).

The secret key is validated at parse time: base64-decodable and exactly 32 bytes, else a
`config.Error` naming the variable. Failing at startup rather than at first encrypt is the
difference between an operator seeing a clear error and a user seeing a 500.

---

## 9. Frontend

**Bootstrap.** `App` renders a `ModeGate` that suspends on `useAuthMode()`
(`GET /api/auth/mode`, `staleTime: Infinity`). Standalone resolves straight to today's
tree — same `AppShell`, same `AppRoutes`, no auth provider mounted, no new network calls.
Hosted mounts the auth-aware shell. One conditional at the root beats a mode check in every
component.

**Session state.** There is none to hold: the cookie is `HttpOnly` and invisible to JS. The
React Query cache entry for `useCurrentUser()` (`GET /api/auth/me`) *is* the auth state. A
`401` clears it. Nothing is written to `localStorage` (FR-8.8), which falls out of the
design rather than needing a rule.

**The `401` interceptor.** `src/lib/api/client.ts` gains `credentials: "include"` and a
module-level `onUnauthorized` callback invoked by `request()` when a response is `401`.
`AuthProvider` registers a callback that clears the query cache and navigates to
`/login?next=<path>`. A callback rather than a thrown-and-caught event keeps `client.ts`
free of React and router imports, preserving its current shape as a thin fetch wrapper.

**New surface** (mirroring the existing `services/api` + `lib/hooks/api` + `pages` split):

- `services/api/auth.ts`, `services/api/userProviders.ts` — plain service objects
- `lib/hooks/api/useAuth.ts`, `useUserProviders.ts` — React Query hooks, mutations
  invalidating `["auth","me"]` and `["providers"]` (the latter because `GET /api/providers`
  changes when settings change)
- `pages/LoginPage.tsx`, `RegisterPage.tsx`, `AccountSettingsPage.tsx`,
  `ProviderSettingsPage.tsx`
- `components/layout/RequireAuth.tsx` (route guard), account menu in `AppShell`
- `lib/schemas/auth.ts`, `userProvider.ts` — Zod schemas mirroring server validation

**Token field semantics in the UI.** The edit form's token input is always empty on load,
`type="password"`, with placeholder "Leave blank to keep the current token". The form never
receives a token to render, so there is no possibility of leaking one — FR-5.4 and FR-8.5
are the same mechanism seen from two ends.

---

## 10. Testing strategy

| Layer | What it proves |
| --- | --- |
| `identity` unit | `Matches` truth table: standalone/hosted × owned/unowned/foreign |
| `auth/password` unit | Round-trip, wrong password, PHC parse, params read from string not constants |
| `auth/crypt` unit | Round-trip; ciphertext from user A's row fails to open under user B's AAD |
| `auth/throttle` unit | Doubling schedule, 15-min cap, clear on success, survives reopen |
| `db` unit | Migration applied once, idempotent re-run, unknown recorded version is fatal |
| `config` unit | Mode parsing, hosted-without-key fatal, `PROVIDERS__*` optional in hosted |
| `session` unit | Owner round-trips through record; old record without `owner` decodes |
| `api` tests | Full auth lifecycle; standalone `404`s; `401`/`403`; cross-user `404`; no token in any response body |
| integration | Two users, two providers, same repository, two mirror paths; prune preserves live review branches under a namespace |
| frontend Vitest | Mode gate both ways, `401` redirect with `next`, both settings forms |

Two cross-cutting assertions get dedicated tests because they are the ones most likely to
regress silently:

1. **No secret in logs.** A test installs the redacting handler over a buffer, exercises
   register → login → provider create → review create, and asserts the buffer contains no
   session token, password, provider token, or master key.
2. **No token in any response.** The API test suite captures every response body and
   asserts none contains the plaintext token fixture.

Existing tests change only mechanically: `NewStaticResolver(registry)` and
`identity.Standalone()` at call sites. Any existing test that needs more than that is a
signal the seam is in the wrong place, and should be raised during implementation rather
than papered over.

---

## 11. Deliberate non-decisions

These are PRD §9's open questions. This design takes a position so the plan does not have
to re-litigate them:

1. **Password reset / operator recovery** — out of scope. A `converge-cli` subcommand that
   opens the database and resets a hash is a coherent follow-up, but it needs its own
   thinking about who can run it and what stops it from being an authentication bypass on a
   shared host. Documented as a known gap.
2. **Removing another user** — out of scope, same reasoning.
3. **Open registration** — kept open, with an explicit deployment note in the README: a
   hosted instance reachable from the internet should sit behind a VPN or proxy auth. The
   `registrationOpen` field already in the `modes` response is the seam a future
   invite-code feature uses.
4. **Master key rotation** — documented procedure only (stop, decrypt-all with old key,
   re-encrypt with new, start). No command ships in this task.
5. **Mirror disk growth** — no cap (see §6).
6. **`converge-cli`** — stays standalone-only. It wraps everything in
   `identity.Standalone()` and `NewStaticResolver`, and never opens the database.

---

## 12. Risks

- **Signature churn.** Adding `identity.Scope` to `session.Store` and `mirror.Cache` touches
  most existing tests. Mechanical, but broad — the plan should sequence it as one early,
  self-contained task so later tasks build on stable signatures.
- **`MaxOpenConns(1)` under Argon2id.** Hashing must never happen while a connection is
  held. This is an ordering discipline in `service.go` (hash, *then* open a transaction),
  easy to get wrong in a later edit; it gets a comment at the call site and a test that
  performs a concurrent login and provider-list without deadlocking.
- **`modernc.org/sqlite` binary size and build time.** It is a large dependency. Expect the
  `converge` binary and the Docker build to grow noticeably; verify `make docker-build`
  early rather than at the end.
- **Standalone equivalence is asserted, not proven.** The strongest evidence available is
  that the existing API test suite passes unchanged with no new environment variables. That
  suite's coverage is the ceiling on this guarantee; the plan should not add new assertions
  to it while changing the code under it, so a pass means what it appears to mean.
- **Cache invalidation assumes a single process.** True today, and the design would need
  revisiting for a multi-replica deployment. Recorded here so a future reader does not
  discover it by debugging.
