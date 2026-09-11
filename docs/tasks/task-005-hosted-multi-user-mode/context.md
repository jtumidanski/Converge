# Hosted Multi-User Mode — Implementation Context

Companion to `plan.md`. Written for someone picking this branch up cold: what already
exists, what the plan assumes about it, and which decisions are already settled so they do
not get re-litigated mid-implementation.

Inputs: `prd.md` (approved), `design.md` (approved), `api-contracts.md`, `plan.md`.

---

## 1. The one-paragraph version

Converge is a single-tenant Go + React tool. `CONVERGE_MODE=hosted` turns it into a
multi-user one without the default `standalone` shape paying for it. The mechanism is three
seams: an `identity.Scope` passed explicitly from `api` down to `session.Store` and
`mirror.Cache`; a `provider.Resolver` interface so the provider registry becomes a function
of the caller; and a bounded SQLite store for accounts, login sessions, per-user provider
configuration, and lockout counters. Review sessions stay `session.json` files on disk.

---

## 2. Key existing files the plan touches

Read these before starting. Line counts are as of branch point `bef7217`.

| File | Lines | Why it matters |
| --- | --- | --- |
| `apps/backend/internal/config/config.go` | 261 | `Load(env []string)` parses everything from a slice, not `os.Environ` — which is what makes mode parsing testable. `loadProviders` becomes conditional. `Secret` (in `secret.go`) already redacts on `String`, `MarshalJSON`, and `LogValue`. |
| `apps/backend/internal/session/store.go` | 398 | The in-memory `index` plus `session.json` on disk. Note the careful ordering in `Finish`/`expire`: the terminal status is written to the index *before* `Cleanup` runs, and `Cleanup` runs outside the lock. `Store.Purge` must copy that shape. |
| `apps/backend/internal/session/model.go` | 285 | Immutable `Session` with unexported fields and `With*` transitions. `owner` joins them. |
| `apps/backend/internal/session/record.go` | 150 | `SchemaVersion = 1`, and `FromRecord` **rejects** a mismatched version. This is why the plan keeps the version at 1: bumping it would reject every existing record. |
| `apps/backend/internal/mirror/cache.go` | 112 | `Path` validates `providerID` against `providerIDRe` and the repo name via `gitx.ValidateRepoFullName`. `Ensure`'s refspec includes `gitx.ExcludeReviewRefspec` (commit `b8a8391`) — do not touch that. |
| `apps/backend/internal/review/service.go` | 596 | Owns the build semaphore, the `WaitGroup`, and `StartBuild`'s deliberate outliving of the request. That last property is why `Build` reads the owner off the session instead of taking a scope. |
| `apps/backend/internal/review/resolve.go` | ~340 | `Resolver.Resolve` is the long pipeline; it needs the namespace threaded to two `mirrors` calls. |
| `apps/backend/internal/api/router.go` | 106 | Read the comment on `mux.Handle("/")` before touching registration: `"/api/"` vs `"GET /"` is an ambiguous-pattern panic waiting to happen. |
| `apps/backend/internal/api/errors.go` | ~70 | `classify` is the single error-mapping function. There is deliberately **no** `INTERNAL` code; unclassified failures report `GIT_FAILURE`. |
| `apps/backend/internal/api/middleware.go` | ~85 | `withMiddleware` does request id, recovery, logging, and `Accept` negotiation. The two new middlewares wrap *inside* it. |
| `apps/backend/internal/app/app.go` | 286 | `New` is the whole object graph. `redactingHandler` scrubs `cfg.Secrets()` from every log line — this is what makes the master key safe to add to that list. |
| `apps/backend/cmd/converge-cli/main.go` | ~210 | Goes through `app.New` too, so `app.New` must stay standalone-safe. The CLI never gains accounts. |
| `apps/frontend/src/lib/api/client.ts` | ~55 | A thin fetch wrapper with no React or router imports. Keeping it that way is why the 401 hook is a module-level callback. |
| `apps/frontend/src/App.tsx` | ~35 | `QueryClientProvider > ThemeProvider > BrowserRouter > AppShell > AppRoutes`. `ModeGate` slots in between the router and the shell. |
| `apps/backend/.golangci.yml` | 32 | Only ten linters are on. `gosec`, `errorlint`, and `errcheck` are the ones that will actually bite. `_test.go` is exempt from `gosec` and `errcheck`. |

---

## 3. New packages and the dependency rule

```
                      api  ──────────────────────────────┐
                       │                                 │
                       ▼                                 ▼
                    review  ──────────────────────▶  auth ──▶ db
                       │                              │
        ┌──────────────┼───────────────┐              ▼
        ▼              ▼               ▼           provider
    provider        mirror          session       github/gitlab
        │              │               │
        └──────────────┴───────────────┴──▶ identity  (leaf, stdlib only)
                       │
                       ▼
                     gitx
```

- `identity` and `db` are leaves. `identity` imports **nothing**; `db` imports only stdlib
  plus the driver.
- `auth` may import `db`, `config`, `identity`, `provider`, `provider/github`,
  `provider/gitlab`. It must never import `review` or `session`.
- `review` must never import `auth`.
- The arrow from `review` to `auth` in the diagram is **interface satisfaction, not an
  import**: `*review.Service` implements `auth.Purger` and `auth.ProviderUsage`, and
  `internal/app` is the only package that knows both sides.

Enforce it mechanically after any task that touches imports:

```bash
cd apps/backend
go list -deps ./internal/auth | grep -E 'converge/internal/(review|session|api)$'   # must be empty
go list -deps ./internal/review | grep -E 'converge/internal/(auth|db)$'            # must be empty
go list -deps ./internal/identity | grep converge/internal                          # must be empty
```

The fourth check from the original plan text, `go list -deps ./cmd/converge-cli | grep -E
'converge/internal/(auth|db)$'` expected empty, is **not achievable** and has been dropped
from the enforced list: `cmd/converge-cli` calls `app.New`, and `internal/app` unconditionally
imports `internal/auth` and `internal/db` as struct field types on `App` regardless of mode.
Go's import graph is static, so this always reports both packages. The property that matters —
`cmd/converge-cli` never opens a database file at runtime — is behavioral and is covered by the
Task 20 smoke test instead (see `docs/tasks/task-005-hosted-multi-user-mode/plan.md` Task 20,
Step 4 note).

---

## 4. Settled decisions — do not re-litigate

| Decision | Why | Where |
| --- | --- | --- |
| Identity travels as an **explicit argument**, not on a context below `api` | A value whose absence is a data leak belongs in signatures. Adding the parameter is a compile error at every call site — a one-time cost for a permanent guarantee. | design §3 |
| `provider.Resolver` interface, hosted implementation in `auth` | The hosted resolver needs the DB and the decryption key; `provider` must gain neither. Satisfying the interface from outside runs the dependency the right way. | design §4 |
| SQLite, `modernc.org/sqlite`, `MaxOpenConns(1)` | JSON files cannot do atomic uniqueness or read-modify-write lockout counters without becoming a small buggy database. `mattn/go-sqlite3` fails the `CGO_ENABLED=0` gate. One connection removes `SQLITE_BUSY` handling from every call site. | design §5 |
| Signatures change; no `ScopedStore` wrapper | A wrapper leaves the unscoped methods reachable, and the first direct call loses isolation with no compile error. | design §6 |
| On-disk workspace layout is **unchanged** | `WORKSPACE_ROOT/<session-id>/` in both modes. Isolation lives in the store and the API, not in directory nesting, so existing sessions keep resuming. | FR-6.2 |
| `SchemaVersion` stays `1` | An added optional field that absent-decodes to zero is not a schema break, and `FromRecord` rejects a version mismatch. | design §6 |
| Per-user mirrors, **no disk cap** | N users on one repository means N mirrors. A quota is a separate feature with its own UX question. | design §6, PRD Q5 |
| Registration stays **open** | Documented deployment note: put a reachable instance behind a VPN or proxy auth. `registrationOpen` in the modes response is the seam a future invite code uses. | design §11, PRD Q3 |
| No password reset, no admin removal, no key-rotation command | Each needs its own thinking about who may run it; all three are documented gaps. | design §11, PRD Q1/Q2/Q4 |
| `converge-cli` stays standalone-only | It wraps everything in `identity.Standalone()` and the static resolver and never opens the database. | design §11, PRD Q6 |
| Two new runtime dependencies is a conscious acceptance | `go.mod` currently has **zero** non-tool runtime deps. Both additions are pure Go; `golang.org/x/crypto` is Go-team-maintained. | design §5 |

---

## 5. Decisions this plan makes that the design left open

Recorded here so the implementer does not have to invent them, and the reviewer knows they
were chosen rather than defaulted. The full list, with reasoning, is in `plan.md`'s
*Deliberate deviations from the design*.

1. `auth.NewID()` is 16 hex characters (8 random bytes), not `session.NewID`'s 8 — these are
   long-lived primary keys and a user id becomes a filesystem path segment.
2. `mirror.Namespace` validates a user id against `^[0-9a-f]{16}$`, the exact shape
   `auth.NewID` produces, and `PurgeNamespace` refuses the root namespace.
3. `auth.ProviderUsage` is added alongside the design's `Purger`, for FR-5.7's
   `PROVIDER_IN_USE` check, on the same dependency-direction argument.
4. `ProviderPatch.Validate` defaults to **false**; `ProviderInput.Validate` defaults to
   **true** (which is why the create handler decodes it as `*bool`).
5. `deleteReview` and `sessionFor` each carry one hosted-vs-standalone branch, both for
   non-disclosure reasons documented at the call site.
6. A four-way semaphore bounds concurrent Argon2id operations; the throttle is a counter and
   cannot bound memory.
7. `schema_migrations` is created by the migration runner, not by `0001_init.sql`.

---

## 6. The traps

Things that will silently do the wrong thing if handled casually.

- **`MaxOpenConns(1)` plus Argon2id.** Hashing must never happen while a transaction is
  open, or every other query blocks for ~50 ms. `auth.Service` hashes first and touches the
  database afterwards, every time. Design §12 names this; `plan.md` Task 14 gives it a
  comment at the call site and a concurrent login/list test.
- **`Build` outlives its request.** `StartBuild` deliberately runs past the HTTP response,
  so the request's scope is gone. `Build` must read the owner off the persisted session
  (`scopeOf`). Passing a request scope here would work in tests and fail in production.
- **`Corrupted` in hosted mode.** A corrupt `session.json` has no readable owner, so
  reporting `500` for one would confirm the id exists. `sessionFor` consults `Corrupted`
  only when unscoped.
- **`validate` must be a `*bool` on create.** Its default is `true`, not Go's zero `false`.
  A plain `bool` silently skips credential verification on every request that omits the
  field.
- **`?next` on the login page.** An unvalidated value is an open redirect that fires the
  instant a user authenticates. `safeNext` accepts only a path starting with a single `/`.
- **The 401 handler must no-op on `/login` and `/register`.** The `401` from `/api/auth/me`
  is the expected answer there; redirecting loops.
- **`mux.Handle("/")` ambiguity.** Registering the UI as `"GET /"` panics against the
  existing `"/api/"` catch-all. The existing comment in `router.go` explains why; do not
  "tidy" it.
- **`gitx.ExcludeReviewRefspec`.** Namespacing changes which directory the refspec applies
  to, not the refspec. Commit `b8a8391` exists because pruning it deleted live review
  branches; Task 27 re-proves it under a namespace.
- **Standalone equivalence.** Do not add assertions to pre-existing tests while changing the
  code under them. New assertions go in new test functions, so a pass of the old suite means
  what it appears to mean (design §12).

---

## 7. Environment variables

| Variable | Default | Modes | Notes |
| --- | --- | --- | --- |
| `CONVERGE_MODE` | `standalone` | both | `standalone` \| `hosted`. Anything else is a startup error. |
| `CONVERGE_DATABASE_PATH` | `/data/converge.db` | hosted | Must be on a persisted volume. Never read in standalone. |
| `CONVERGE_SECRET_KEY` | — | hosted | 32 random bytes, base64 standard. Required; validated at parse time. Losing it makes every stored provider token unreadable. |
| `CONVERGE_SECURE_COOKIES` | `false` | hosted | Set when TLS terminates at a proxy. |
| `CONVERGE_TRUSTED_PROXY` | `false` | hosted | Only with a real proxy in front: otherwise `X-Forwarded-For` lets any client forge a throttle key. |
| `LOGIN_SESSION_TTL_HOURS` | `720` | hosted | Absolute session expiry. |
| `LOGIN_SESSION_IDLE_HOURS` | `168` | hosted | Idle session expiry. |

`PROVIDERS__*` is required in standalone and ignored (with one `WARN` naming the variables)
in hosted.

Generate a key:

```sh
CONVERGE_SECRET_KEY="$(head -c 32 /dev/urandom | base64)"
```

---

## 8. Task dependency order

Tasks 9–12 are the signature churn the design sequences early; everything after assumes
those signatures. Do not reorder.

```
1 identity ──┬─▶ 2 config ─┬─▶ 3 db ──▶ 4 auth errors/model ─▶ 5 password ─▶ 6 crypt ─▶ 7 store ─▶ 8 throttle
             │             │                                                                          │
             ├─▶ 9 provider.Resolver (churn) ──┐                                                      │
             ├─▶ 10 session owner   (churn) ───┤                                                      │
             └─▶ 11 mirror namespace (churn) ──┴─▶ 12 review threading                                │
                                                          │                                           │
                                                          └──────────────┬────────────────────────────┘
                                                                         ▼
                                                      13 resolver/verifier ─▶ 14 service ─▶ 15 provider CRUD + sweep
                                                                                                  │
                                       16 api middleware ─▶ 17 auth handlers ─▶ 18 settings ─▶ 19 routes ─▶ 20 app wiring
                                                                                                                │
                                       21 fe api layer ─▶ 22 fe gate/guard ─▶ 23 login/register ─▶ 24 providers ─▶ 25 account
                                                                                                                │
                                                                        26 security assertions ─▶ 27 integration ─▶ 28 docs ─▶ 29 review
```

**Natural hand-off points** (commit, `/clear`, resume from the committed artifacts):
after Task 8 (auth primitives), Task 12 (seams stable), Task 20 (backend whole),
Task 25 (frontend whole).

---

## 9. Verification commands

Per task, backend (cwd `apps/backend`):

```bash
go test -race -count=1 -timeout 300s ./internal/<pkg>/...
go vet ./...
go tool golangci-lint run
CGO_ENABLED=0 go build ./...
```

Per task, frontend (cwd `apps/frontend`; `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22` first if `npm` is missing):

```bash
npm run lint && npm run format:check && npm test
```

The branch is done only when all five of these, from the repository root, are clean:

```bash
make lint && make test && make test-integration && make build && make docker-build
```

`make docker-build` is the one that proves `CGO_ENABLED=0` plus `modernc.org/sqlite` works
in the image without a C toolchain. Run it early — any time after Task 3 — rather than
discovering the size and build-time cost at the end (design §12).

---

## 10. Acceptance criteria coverage

PRD §10 has **35** checkboxes across seven groups (corrected count: the original placeholder
below said 44, which over-counted). The mapping from each to its proving test is produced in
**Task 28, Step 6** and recorded here. A criterion with no named test or hand-verified command
is unfinished work, not a documentation gap.

Group-to-task index, so the walk starts from somewhere:

| Group | Primary tasks |
| --- | --- |
| Mode and backwards compatibility (7) | 2, 19 (`TestStandaloneRouterIsUnchanged`), 20 |
| Accounts (7) | 5, 7, 8, 14, 17 |
| Sessions and CSRF (4) | 14, 16, 17, 26 |
| Providers (7) | 6, 13, 15, 18, 26 |
| Isolation (4) | 10, 11, 12, 19, 27 |
| UI (4) | 22, 23, 24 |
| Build gates (2) | 28 |

### Acceptance criteria coverage (Task 28, Step 6)

All test functions below live under `apps/backend` unless marked `frontend`; file paths are
relative to that root. Verified by running the exact test named, or (for the two build-gate
rows) by hand as part of this task's Step 5.

**Mode and backwards compatibility**

1. No new env vars → standalone, no DB file, existing tests unchanged —
   `internal/app/app_test.go:TestStandaloneCreatesNoDatabaseFile`, plus a clean
   `go test -race -count=1 ./...` run (this task, Step 5).
2. A session from the previous release is still listed/resumable after upgrade —
   `internal/app/app_test.go:TestNewLoadAllRestoresExistingSessions`.
3. `CONVERGE_MODE=hosted` without `CONVERGE_SECRET_KEY` fails to start, naming the variable —
   `internal/app/app_test.go:TestMissingSecretKeyIsAStartupError`.
4. `CONVERGE_MODE=nonsense` fails to start, naming `CONVERGE_MODE` —
   `internal/config/config_test.go:TestModeRejectsUnknownValue`.
5. `CONVERGE_MODE=hosted` with `PROVIDERS__*` set logs one warning and ignores them —
   `internal/app/app_test.go:TestHostedWarnsAboutIgnoredProviderVariables`.
6. Standalone: `POST /api/auth/login` and `GET /api/settings/providers` both `404` —
   `internal/api/auth_test.go:TestAuthRoutesAre404InStandalone` and
   `internal/api/settings_providers_test.go:TestSettingsRoutesAre404InStandalone`.
7. `GET /api/auth/mode` returns the correct mode, unauthenticated, in both modes —
   `internal/api/auth_test.go:TestAuthModeReportsTheMode`.

**Accounts**

1. Register, logout, login round trip succeeds —
   `internal/auth/service_test.go:TestRegisterThenLoginRoundTrip` and
   `internal/api/auth_test.go:TestLoginAndLogout`.
2. A case-insensitive duplicate username returns `USERNAME_TAKEN` —
   `internal/auth/service_test.go:TestRegisterRejectsACaseInsensitiveDuplicate`.
3. A 7-character password is `WEAK_PASSWORD`; 8 characters is accepted —
   `internal/auth/service_test.go:TestRegisterValidatesInput`.
4. Wrong password and unknown username return identical status/code/message —
   `internal/auth/service_test.go:TestLoginIsIndistinguishableBetweenUnknownUserAndWrongPassword`.
5. Changing the password invalidates other sessions but not the caller's; old password fails —
   `internal/auth/service_test.go:TestChangePasswordRevokesOtherSessionsButNotTheCaller` and
   `internal/api/auth_test.go:TestChangePasswordKeepsTheCallingSession`.
6. Deleting an account removes its DB rows, review sessions/workspaces, and mirror namespace;
   its cookie stops authenticating —
   `internal/auth/service_test.go:TestDeleteAccountRemovesRowsAndCallsThePurger`,
   `internal/api/auth_test.go:TestDeleteAccountClearsTheCookie`, and
   `internal/mirror/cache_test.go:TestPurgeNamespaceRemovesOnlyThatUser`.
7. Six consecutive failed logins for one username return `429`/`Retry-After`, surviving a
   restart —
   `internal/auth/service_test.go:TestLoginEngagesTheThrottle`,
   `internal/api/auth_test.go:TestLoginReturnsRetryAfterOnLockout`, and
   `internal/auth/throttle_test.go:TestLockoutSurvivesReopeningTheDatabase`.

**Sessions and CSRF**

1. The session cookie is `HttpOnly`/`SameSite=Lax`, and `Secure` when
   `CONVERGE_SECURE_COOKIES=true` —
   `internal/api/authmw_test.go:TestSessionCookieAttributes`.
2. No log line in any test run contains a session token, password, provider token, or master
   key — `internal/app/secrets_test.go:TestNoSecretReachesTheLog`.
3. `POST /api/reviews` with a foreign `Origin` is `403`/`FORBIDDEN` —
   `internal/api/isolation_test.go:TestCreateReviewWithAForeignOriginIs403`.
4. A login session past absolute or idle expiry is rejected before the sweeper runs, and
   removed by the sweeper —
   `internal/auth/service_test.go:TestAuthenticateEnforcesAbsoluteExpiry`,
   `internal/auth/service_test.go:TestAuthenticateEnforcesIdleExpiry`, and
   `internal/auth/providers_test.go:TestSweepRemovesExpiredSessionsAndElapsedLockouts`.

**Providers**

1. A user can create, list, edit, and delete their own provider configurations —
   `internal/api/settings_providers_test.go:TestUserProviderCRUDOverHTTP`.
2. No API response contains a stored token value; only `tokenLast4` appears —
   `internal/api/no_token_test.go:TestNoResponseBodyEverContainsAToken`.
3. Editing a provider with an empty token field leaves the stored token working —
   `internal/api/settings_providers_test.go:TestPatchWithAnOmittedTokenKeepsTheStoredOne` and
   `TestPatchWithAnExplicitEmptyTokenKeepsTheStoredOne`.
4. A second user's `GET /api/providers` does not include the first user's providers —
   `internal/auth/resolver_test.go:TestResolveIsolatesUsers`.
5. Creating a provider with an invalid token and `validate=true` returns
   `PROVIDER_UNAUTHORIZED` and writes nothing —
   `internal/auth/providers_test.go:TestCreateProviderWithValidateTrueWritesNothingOnRejection`.
6. Deleting a provider referenced by an active review returns `PROVIDER_IN_USE` —
   `internal/auth/providers_test.go:TestDeleteProviderRefusesWhileInUse` and
   `internal/api/settings_providers_test.go:TestDeleteRefusesWhileInUse`.
7. A token ciphertext copied into another user's row fails to decrypt —
   `internal/auth/crypt_test.go:TestCiphertextIsBoundToItsRow`.

**Isolation**

1. User A's `GET /api/reviews` never includes user B's reviews —
   `internal/api/isolation_test.go:TestReviewsAreScopedToTheirOwner`.
2. `GET /api/reviews/{id}` and `DELETE /api/reviews/{id}` for another user's session return
   `404` — `internal/api/isolation_test.go:TestForeignReviewIDIs404`.
3. Two users reviewing the same repository use two mirror directories under
   `REPOSITORY_CACHE_ROOT/users/<user-id>/` —
   `internal/mirror/cache_test.go:TestTwoUsersGetIndependentPaths` and
   `TestScopedNamespaceNestsUnderUsers`.
4. Mirror prune in hosted mode does not delete a branch belonging to a live review —
   `internal/mirror/cache_test.go:TestEnsurePruneKeepsLiveReviewBranchesButPrunesStaleOnes`.

**UI** (all `frontend`, relative to `apps/frontend`)

1. Standalone UI renders exactly as before, no account menu, no reachable settings routes —
   `src/__tests__/App.test.tsx:"mounts no account menu and requests no /api/auth/me in
   standalone mode"` and `src/__tests__/routes.test.tsx:"excludes the settings and auth
   paths"`.
2. Hosted, unauthenticated visit to `/` redirects to `/login` and returns to `/` after login —
   `src/components/auth/__tests__/RequireAuth.test.tsx:"redirects an unauthenticated visitor
   to /login with the attempted path as next"` and
   `src/pages/__tests__/LoginPage.test.tsx:"submits and returns to next"`.
3. A `401` from any API call redirects to `/login` —
   `src/components/auth/__tests__/AuthProvider.test.tsx:"redirects to /login and clears the
   query cache on a 401 from any call"`.
4. The provider settings form creates, edits, and deletes providers, and never renders a
   stored token —
   `src/components/features/settings/__tests__/UserProviderForm.test.tsx:"creates a
   provider"`/`"edits a provider..."` and
   `src/pages/__tests__/ProviderSettingsPage.test.tsx:"never renders a stored token"`.

**Build gates**

1. `make lint`, `make test`, `make test-integration`, `make build`, and `make docker-build`
   are all clean — hand-verified from the repository root in this task, Step 5; all five
   passed (see the Task 28 report for full output).
2. `CGO_ENABLED=0 go build ./...` succeeds with the SQLite driver linked in — covered by
   `make build`'s backend step and independently by `make docker-build`'s multi-stage image
   build, both of which build with `CGO_ENABLED=0` against `modernc.org/sqlite` (pure Go, no
   C toolchain required in the image).
