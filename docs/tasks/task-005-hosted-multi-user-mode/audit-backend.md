# Backend Audit — task-005 hosted multi-user mode

- **Worktree:** `.worktrees/task-005-hosted-multi-user-mode`
- **Range:** `7ae00b9..f916444` (50 commits)
- **Guidelines source:** `.claude/skills/backend-dev-guidelines/resources/`
- **Date:** 2026-09-11
- **Build:** PASS (not re-run; read from captured gate log)
- **Tests:** PASS (not re-run; read from captured gate log)
- **Overall:** NEEDS-WORK (one guideline FAIL; no merge-blocking defect)

Documented project deviations from the guidelines skill (`CLAUDE.md`) — `session.json` as
entity, `log/slog` injected through constructors, pipeline steps as plain functions — are
excluded from the findings below, as instructed.

## Build & Test Results

Gates were **not** executed by this audit (a concurrent reviewer held the worktree).
Read verbatim from `.superpowers/sdd/plan/task-28-gate.log`:

| Gate | Exit code | Evidence |
|---|---|---|
| `make lint` | 0 | task-28-gate.log:15 |
| `make test` | 0 | task-28-gate.log:58 |
| `make test-integration` | 0 | task-28-gate.log:83 |
| `make build` | 0 | task-28-gate.log:445, :469 |
| `make docker-build` | 0 | task-28-gate.log:594 |

All 22 backend packages report `ok`; `internal/provider/fake` is `[no test files]`
(task-28-gate.log:18-39, :61-82, :447-468). Zero failures, zero skips recorded.

The only command this audit executed was the read-only
`go list -deps ./internal/<pkg>` sweep used for the dependency-direction check below.

## Package Classification (Phase 2)

| Package | Classification | Note |
|---|---|---|
| `internal/auth` | Domain (`model.go` present) | full DOM checklist |
| `internal/session` | Domain (`model.go` present, pre-existing) | modified; DOM checked for regressions |
| `internal/identity` | Support (leaf value type) | `scope.go` only |
| `internal/db` | Support (persistence leaf) | connection + migrations |
| `internal/api` | Transport | handler-layer DOM items apply |
| `internal/provider`, `provider/{github,gitlab,fake}` | Support | `resolver.go` added |
| `internal/mirror`, `internal/review`, `internal/gitx`, `internal/config`, `internal/app` | Support | modified |

## Domain Checklist — `internal/auth`

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| DOM-01 | `builder.go` exists with `NewBuilder()` + `Build()` validation | **FAIL** | No `builder.go` in `internal/auth/` (directory listing). `model.go` defines two models (`User` at `internal/auth/model.go:94`, `UserProvider` at `internal/auth/model.go:159`); construction goes through `NewUser` (`model.go:104`) and `NewUserProvider` (`model.go:185`). Guideline: `ai-guidance.md:170`, `scaffolding-checklist.md:139`. Contrast `internal/session/builder.go:18`, which does have `NewBuilder()`. |
| DOM-02 | `ToEntity()` on model | N/A | No GORM entity tier (documented `CLAUDE.md` deviation). The persistence boundary is `internal/auth/store.go`, which scans directly into unexported model fields (`store.go:70`, `store.go:214`). |
| DOM-03 | `Make(Entity)` | N/A | Same as DOM-02; `scanUser` (`store.go:70`) and `scanUserProvider` (`store.go:214`) fill the role. |
| DOM-04/05 | `Transform` / `TransformSlice` in `rest.go` | PASS (equivalent) | No api2go tier; the transport mapping lives in `internal/api`: `userResource` (`internal/api/auth.go:56`), `userProviderResource` (`internal/api/settings_providers.go:55`). List handlers loop over the single-resource mapper rather than inlining field access (`settings_providers.go:87-90`, `auth.go` n/a). |
| DOM-06 | Processor takes an interface logger, not a concrete one | PASS | `ServiceDeps.Log *slog.Logger` (`internal/auth/service.go:63`) injected via `NewService` (`service.go:78`); no package-level logger, no `slog.Default()` anywhere in `internal/auth`. |
| DOM-07 | Handlers pass the injected logger | PASS | Every handler uses `s.deps.Log` (`internal/api/auth.go:73,91,109,132,151`; `settings_providers.go:77,84,92,123,153`). No `slog.Default()` in `internal/api`. |
| DOM-08 | POST/PATCH use a typed input handler, not raw body reads | PASS | `jsonapi.Decode[credentialAttributes]` (`auth.go:78`, `auth.go:96`), `[passwordAttributes]` (`auth.go:156`), `[accountDeletionAttributes]` (`auth.go:172`), `[createUserProviderAttributes]` (`settings_providers.go:97`), `[patchUserProviderAttributes]` (`settings_providers.go:132`). |
| DOM-09 | Transform/decode errors handled, never discarded | PASS | Every `jsonapi.Decode` call checks `err` and returns (`auth.go:79-82,97-100,157-160,173-176`; `settings_providers.go:98-101,133-136`). Zero `_, _ :=` or `_ =` on a decode in `internal/api`. |
| DOM-10 | Providers use lazy evaluation | N/A (adapted) | No `database.Query`/`FixedProvider` abstraction in this project. The analogue — lazy per-scope provider resolution — is `provider.Resolver` (`internal/provider/resolver.go:20`) with `auth.ProviderResolver.Resolve` (`internal/auth/resolver.go:63`) building on demand behind a TTL cache. See Important finding 3 on that cache. |
| DOM-11 | No `os.Getenv()` in handlers | PASS | `grep -rn "os.Getenv" internal/api/` → zero matches. (The only `os.Getenv` in the module is `PATH` pass-through at `internal/gitx/exec.go:87`.) |
| DOM-12 | No cross-domain logic in handlers | PASS | Handlers call exactly one service method plus the response writer. The only cross-store orchestration — account deletion cascading into review sessions and mirrors — is inverted behind interfaces in the service tier: `auth.Purger` (`internal/auth/service.go:41`) and `auth.ProviderUsage` (`service.go:47`), implemented at `internal/review/service.go:647` and `:665`. |
| DOM-13 | Handlers don't call stores/providers directly | PASS | `internal/api` imports neither `internal/db` nor `database/sql`: `grep -rn "database/sql\|\*sql\." internal/api/*.go` (non-test) → zero matches. Provider access goes through `s.deps.Providers.Resolve` (`internal/api/providers.go:23`, `repositories.go:42`), never a registry literal. |
| DOM-14 | No direct writes in handlers | PASS | Zero `db.Create`/`db.Save`/`db.Exec` in `internal/api`; every write goes handler → `auth.Service` → `auth.Store` (e.g. `settings_providers.go:110` → `providers.go:39` → `store.go:233`). |
| DOM-15 | Write tier exists | PASS (equivalent) | `internal/auth/store.go` is the administrator-equivalent: `CreateUser:41`, `SetPasswordHash:98`, `DeleteUser:110`, `CreateLoginSession:137`, `CreateUserProvider:233`, `UpdateUserProvider:299`, `DeleteUserProvider:318`. Called only from `service.go`/`providers.go`, never from `internal/api`. |
| DOM-16 | Domain error → HTTP status mapping | PASS | `(*auth.Error).Status()` (`internal/auth/errors.go:63-78`) maps every code; `api.classify` (`internal/api/errors.go:32-71`) recognises it with one `errors.As` arm at `errors.go:33-36`. Verbatim against the plan: `UNAUTHENTICATED`/`INVALID_CREDENTIALS` 401 (`errors.go:65`), `FORBIDDEN` 403 (`:67`), `USERNAME_TAKEN`/`PROVIDER_SLUG_TAKEN`/`PROVIDER_IN_USE` 409 (`:69`), `INVALID_USERNAME`/`WEAK_PASSWORD`/`PROVIDER_UNAUTHORIZED` 422 (`:71`), `ACCOUNT_LOCKED` 429 (`:73`), default 500 (`:75`). All ten codes declared at `errors.go:21-31`. |
| DOM-17 | JSON:API identity on REST models | PASS | `jsonapi.Resource{Type:..., ID:...}` is the project's shape; set at `internal/api/auth.go:57` (`typeUsers`), `auth.go:68` (`typeModes`), `settings_providers.go:56` (`typeUserProviders`), `reviews.go:72` (`reviews`). Type constants declared verbatim at `auth.go:16-22` and `settings_providers.go:13`. |
| DOM-18 | Request models are flat | PASS | `credentialAttributes` (`auth.go:32`), `passwordAttributes` (`auth.go:45`), `accountDeletionAttributes` (`auth.go:50`), `createUserProviderAttributes` (`settings_providers.go:30`), `patchUserProviderAttributes` (`settings_providers.go:47`) are all flat attribute structs — the `data/type/attributes` envelope is peeled by `jsonapi.Decode`, not by the models. |
| DOM-19 | Table-driven tests | **WARN** | Present: `internal/auth/crypt_test.go`, `internal/auth/password_test.go`, `internal/api/auth_test.go`, `internal/api/authmw_test.go`, `internal/api/settings_providers_test.go`, `internal/identity/scope_test.go`. Absent (neither `[]struct` table nor `t.Run`): `internal/auth/store_test.go` (17 top-level `Test` funcs), `service_test.go` (15), `providers_test.go` (16), `throttle_test.go` (10), `model_test.go` (7), `resolver_test.go` (9), `internal/db/db_test.go` (6). `testing-guide.md:39` says "*Prefer* table-driven tests" — a preference, so WARN rather than FAIL. |
| DOM-IMM | All mutations via builders returning new instances (`patterns-functional.md:13`) | **FAIL** | `Service.UpdateProvider` mutates model fields in place on a local copy: `row.displayName` (`internal/auth/providers.go:105`), `row.kind` (`:108`), `row.baseURL` (`:113`), `row.tokenCiphertext`/`row.tokenNonce` (`:148`), `row.tokenLast4`/`row.tokenSetAt` (`:149`), `row.updatedAt` (`:151`). The caller's value is unaffected (value semantics), so this is not a correctness bug — but it is field assignment, not builder-mediated construction. Same root cause as DOM-01. |

## Domain Checklist — `internal/session` (modified)

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| DOM-01 | `builder.go` | PASS | `internal/session/builder.go:18` `func NewBuilder() *Builder`; `SetOwner` threaded at `internal/review/service.go:162`. |
| DOM-16 | Scoped not-found → 404, never 403 | PASS | `Store.Get` folds "absent" and "owned by someone else" into the same `false` (`internal/session/store.go:186-195`), which flows to the existing `session.ErrNotFound` → 404 arm (`internal/api/errors.go:61-62`). |
| DOM-19 | Table-driven tests | WARN | `internal/session/store_test.go` is one-func-per-case like the `auth` files. Same preference-not-mandate ruling. |

## Sub-Domain Checklist — `internal/api` hosted endpoints

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| SUB-01 | Business logic not in handler | PASS | Handlers are decode → one service call → write. Longest is `currentUser` at 15 lines with two service calls and no branching logic (`internal/api/auth.go:138-153`). |
| SUB-02 | No writes in `resource.go`-equivalents | PASS | Zero `sql`/`db` references in `internal/api` (non-test). |
| SUB-03 | Typed input handler for POST/PATCH | PASS | See DOM-08. |
| SUB-04 | No manual JSON parsing | PASS | `grep -rn "json.NewDecoder\|json.Unmarshal\|io.ReadAll" internal/api/*.go` (non-test) → zero matches. The one `encoding/json` use is response encoding at `internal/api/health.go:33`. |

## Security Review

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| SEC-01 | All SQL parameterised | PASS | Every statement in `internal/auth/store.go` uses `?` placeholders with args (`:49, :60, :87, :93, :100, :111, :128, :139, :151, :168, :177, :187, :198, :242, :253, :268, :293, :301, :319, :337, :350, :367, :390, :407, :421, :431`). The only string concatenation into a query is two **compile-time constants**: `userColumns` (`store.go:83`) and `userProviderColumns` (`store.go:212`) — no value is ever concatenated. `internal/db/db.go` likewise: the one non-parameterised `Exec` is the embedded migration body (`db.go:172`, with the rationale at `:170-171`); the bookkeeping statements at `:85, :122, :181` are constants/parameterised. |
| SEC-01b | `store.go` is the *only* file in the repository containing SQL | **FAIL (claim)** | `internal/db/db.go:85` (`CREATE TABLE schema_migrations`), `:122` (`SELECT version FROM schema_migrations`), `:181` (`INSERT INTO schema_migrations`), plus the DDL file `internal/db/migrations/0001_init.sql`. The *substance* of the constraint holds (all domain SQL in one file, nothing value-concatenated); the exclusivity sentence asserted at `internal/auth/store.go:13` does not. See Important finding 2. |
| SEC-02 | Every git call goes through `gitx.Runner` with an arg slice, never a shell | PASS | `exec.CommandContext(ctx, r.gitPath, args...)` — single binary path plus a slice, no `sh -c` (`internal/gitx/exec.go:135`). `grep -rn "exec.Command"` finds no shell invocation in production code. Call sites build `gitx.Spec{Args: []string{...}}` (`internal/mirror/cache.go:146, :152, :177`). |
| SEC-02b | Credentials reach git only via `GIT_CONFIG_COUNT`/`KEY_0`/`VALUE_0`, never argv, never a stored remote URL | PASS | `gitx.CredentialEnv` returns exactly those three env entries (`internal/gitx/credentials.go:28-32`); the only callers are `github.Client.AuthorizeGit` (`internal/provider/github/client.go:184`) and `gitlab.Client.AuthorizeGit` (`internal/provider/gitlab/client.go:233`), both appending to `spec.Env` only (`client.go:188` / `:237`). The clone argv carries `p.CloneURL(repo)` — `repo.CloneURL()` verbatim from the provider API (`github/client.go:180`, `gitlab/client.go:230`), with no userinfo injected. Pinned by `internal/provider/github/client_test.go:196-205` and `gitlab/client_test.go:178`. |
| SEC-03 | Argon2id exact parameters | PASS | `argonTime=1` (`internal/auth/password.go:28`), `argonMemory=64*1024` i.e. 65536 KiB (`:29`), `argonThreads=4` (`:30`), `argonKeyLen=32` (`:31`), `argonSaltLen=16` (`:32`), `argon2.Version` 19 (`:33`). PHC encoding at `password.go:100-105`. Verification reads `m`/`t`/`p` back **from the stored string**, not from the constants: `decodePHC` at `password.go:107` feeds `p.time, p.memory, p.threads` into `argon2.IDKey` at `password.go:85`. Bounds-checked against tamper ceilings (`password.go:44-46`, enforced `:141-143`). Constant-time compare at `password.go:86`. |
| SEC-04 | Provider tokens AES-256-GCM, per-record nonce, AAD binding to `user_id` + provider row id | PASS | Key length 32 enforced (`internal/auth/crypt.go:17`, `:34-36`); `aes.NewCipher` + `cipher.NewGCM` (`crypt.go:37-45`). Fresh nonce from `crypto/rand` per `Seal` call (`crypt.go:66-69`). AAD is `userID \x00 providerID` with an explicit NUL separator against prefix ambiguity (`crypt.go:54-60`), applied on both `Seal` (`:71`) and `Open` (`:82`). Sealed under the row's own freshly generated id (`providers.go:59-65`). Ciphertext/nonce stored in the row's own columns (`store.go:253-255`). |
| SEC-05 | Session cookie named `converge_session`, base64url of 32 random bytes, only SHA-256 persisted | PASS | Name constant `"converge_session"` (`internal/api/authctx.go:13`), used at `authctx.go:71` and `:85`. `tokenBytes = 32` (`internal/auth/service.go:33`); `newToken` reads 32 bytes from `crypto/rand` and returns `base64.RawURLEncoding` (`service.go:105-113`). Only the hash is persisted: `startSession` passes `hash` to `NewLoginSession` (`service.go:189-193`), and `CreateLoginSession` inserts `ls.tokenHash` (`store.go:140`). `LoginSession` has no plaintext field (`model.go:151-158`). Cookie flags: `HttpOnly: true`, `SameSite=Lax`, `Secure: s.secure(r)` (`authctx.go:74-79`), with `secure` honouring TLS or `CONVERGE_SECURE_COOKIES` (`authctx.go:65-67`). |
| SEC-06 | No password, token, cookie value, or master key is ever logged | PASS | Adversarial sweep, four layers: (a) `config.Secret` has `String()`, `MarshalJSON()`, and `LogValue()` all returning `[redacted]` (`internal/config/secret.go:19-25`); (b) the `redactingHandler` scrubs `cfg.Secrets()` from every attribute, recursing into groups (`internal/app/app.go:158-172`, installed at `:199`), and `loggableURL` strips userinfo before a base URL is logged (`app.go:181-187`); (c) `gitx.Run` logs only category, repo, session, exit code, duration, outcome, and `Redact`-ed stderr — `Spec.Args` and `Spec.Env` appear in no log call (`internal/gitx/exec.go:172-196`), and the redaction set is the union of runner-wide and per-invocation secrets (`secretsFor`, `exec.go:113-121`); (d) `Secret.Reveal()` has exactly six production call sites, all of them terminal sinks — the GCM key (`auth/crypt.go:31`), two HTTP headers (`github/client.go:83`, `gitlab/client.go:58`), two `CredentialEnv` calls (`github/client.go:184`, `gitlab/client.go:233`), and the `spec.Secrets` declarations (`github/client.go:196`, `gitlab/client.go:243`); the redaction list builder (`config/config.go:109,116`) is the only other. Every log call in `internal/auth` carries `user_id` and metadata only, never a credential: `service.go:155,176,182,210,297,308` and `providers.go:82,156,179`. The failed-login event deliberately omits even the attempted username (`service.go:177-180`). `grep` for a log call whose args mention token/password/secret/cookie/credential returns only `slog.Bool("token_rotated", ...)` (`providers.go:157`) and the redaction plumbing itself. **No path found.** Nearest miss, for the record: `auth.ProviderInput.Token` and `ProviderPatch.Token` are plain `string`/`*string` (`providers.go:19`, `:30`) rather than `config.Secret`, so a future `slog.Any("input", in)` would not be caught by `Secret.LogValue`; no such call exists today. |
| SEC-07 | Dependency direction | PASS | `go list -deps` (the one command this audit ran): `internal/identity` → no module imports (stdlib leaf). `internal/db` → no module imports (stdlib + `modernc.org/sqlite` leaf). `internal/auth` → `config`, `identity`, `gitx`, `provider`, `provider/github`, `provider/gitlab` — **no `review`, no `session`** ✓. `internal/review` → `gitx`, `diff`, `identity`, `provider`, `mirror`, `workspace`, `session` — **no `auth`** ✓. `internal/provider` → `gitx`, `identity`. `internal/gitx` → nothing. The `api → review → {...} → gitx` direction holds. |
| SEC-08 | `CGO_ENABLED=0` builds; `modernc.org/sqlite` mandatory, `mattn/go-sqlite3` forbidden | PASS | `modernc.org/sqlite v1.58.0` as a direct require (`apps/backend/go.mod:9`), imported blank with the rationale at `internal/db/db.go:22-25`. `grep mattn go.mod` finds only `go-colorable`, `go-isatty`, `go-runewidth` (indirect, unrelated) — **no `mattn/go-sqlite3`** in `go.mod` or `go.sum`. `make build` (which runs the `CGO_ENABLED=0` build) exited 0 (task-28-gate.log:445). |
| SEC-09 | Multi-tenant isolation: no path to another user's session, mirror, or provider config | PASS | **Scope is unforgeable:** `identity.Scope.userID` is unexported with no setter, so the only constructors are `Standalone()` and `ForUser()` (`internal/identity/scope.go:14-23`), and `ForUser` is reached from the request path only inside the authenticate middleware (`internal/api/authmw.go:73`). **Sessions:** `Get` (`session/store.go:186-195`) and `List` (`:211-221`) gate on `scope.Matches(sess.Owner())`; `Finish` routes through `Get` (`:239`). Owner is assigned once, from the creating scope (`review/service.go:162`). Background builds re-derive scope from the persisted owner, not a stale request scope (`scopeOf`, `review/service.go:422-428`, used at `:438`). `Files`/`FileDiff`/`CombinedDiffPath` each re-check through the scoped `Get` (`review/service.go:593, 605, 628`). **Provider configs:** every read and write carries `user_id` in the `WHERE` clause (`auth/store.go:242, 268, 293, 301, 319, 337`), and `UpdateUserProvider`/`DeleteUserProvider` return `ErrNotFound` on `RowsAffected()==0` (`:311-313`, `:327-329`). **Mirrors:** `NamespaceFor` yields `users/<user-id>` (`mirror/cache.go:42-50, :56-61`), validated against the exact 16-hex shape `auth.NewID` produces (`cache.go:26`) rather than trusted, and `PurgeNamespace` refuses the root namespace so a zero-value `Namespace` cannot wipe every user's cache (`cache.go:105-116`). **Resolver:** keyed per user, and an unscoped resolve is a hard error rather than "everything" (`auth/resolver.go:63-70`). |
| SEC-10 | 403-vs-404 does not disclose existence | PASS | `auth.ErrNotFound` is documented and used as the single answer for both unknown and foreign rows (`auth/errors.go:41-44`), mapped to 404 at `api/errors.go:61-62`. `session.Store.Get` folds both cases into `false` (`session/store.go:174-195`). The one place where a 500-vs-404 oracle could arise is handled explicitly: `Corrupted(id)` is consulted **only when the scope is unscoped**, so a hosted caller gets 404 either way (`api/reviews.go:128-138`). `deleteReview` preserves the pre-hosted unconditional 204 for standalone while answering 404 for an invisible id in hosted mode (`api/reviews.go:171-191`). |
| SEC-11 | Login does not leak username existence | PASS | Unknown username verifies against a package `dummyHash` built once at init from 32 random bytes, so the dominant Argon2id cost is paid on both paths (`auth/password.go:177-197`, used at `auth/service.go:168-173`). Both paths return the byte-identical `invalidCredentialsMessage` (`service.go:51`, returned at `:183`). The lockout message is identical for a username lock and an IP lock (`auth/throttle.go:34-36`). |
| SEC-12 | CSRF / cross-site defence | PASS | `originGuard` rejects any non-GET/HEAD `/api/` request that is neither `Sec-Fetch-Site: same-origin` nor `Origin`-host-matching, with `FORBIDDEN` 403 (`api/authmw.go:40-61`); it wraps `authenticate` so it runs first (`api/router.go:163-169`). Combined with `SameSite=Lax` (`authctx.go:76`). Fails closed: a request bearing neither header is rejected. |
| SEC-13 | Throttle key cannot be forged | PASS | `X-Forwarded-For` is honoured only when the operator declared a trusted proxy, and then only the last hop; otherwise `RemoteAddr` (`api/authctx.go:98-111`). Counters are persisted so a restart does not clear a lockout in progress (`auth/store.go:365-373`, read at `:348`), and the read-modify-write is transactional (`UpdateAttempt`, `store.go:382-417`). |
| SEC-14 | No hardcoded secrets | PASS | `CONVERGE_SECRET_KEY` is required in hosted mode with no default and is length-validated (`config/config.go:253-266`). The only credential-shaped literals are error-code strings with `#nosec G101` justifications (`auth/errors.go:23`) and test fixtures. No default password, no fallback key. |
| SEC-15 | Expiry enforced on read, not only by the sweeper | PASS | `Authenticate` rejects and deletes a row past absolute or idle expiry before honouring it (`auth/service.go:245-252`); the sweeper is explicitly documented as housekeeping rather than enforcement (`auth/sweep.go:10-13`). |
| SEC-16 | Password change revokes other sessions | PASS | `DeleteOtherLoginSessions(ctx, userID, keep)` (`auth/service.go:292`) → `DELETE ... WHERE user_id = ? AND token_hash != ?` (`auth/store.go:187`), with `keep` sourced from the calling session's hash via `tokenHashFrom` (`api/auth.go:163`, `api/authctx.go:39-44`). |
| SEC-17 | `ExitError` does not carry stderr into an error string | PASS | `(*ExitError).Error()` emits only category and exit code (`internal/gitx/spec.go:58-60`). Repo-wide grep for `.Stderr` outside `internal/gitx` finds **no production caller** — only `os.Stderr` logger wiring and the two `authorize_git_secret_test.go` precondition assertions. |

## Findings

### Critical

None.

### Important

**I-1. `docs/hosted-mode.md` contradicts the shipped code (stale doc).**
`docs/hosted-mode.md:106-110` states: "It shares `internal/app`'s wiring code with the
server binary, so `CONVERGE_MODE=hosted` present in the CLI's own environment would still
cause it to attempt to open the database — operators should not set `CONVERGE_MODE=hosted`
in a shell where `converge-cli` runs."

The code no longer behaves that way. `cmd/converge-cli/main.go:80` appends
`"CONVERGE_MODE=standalone"` to `os.Environ()` so a later duplicate key wins, pinning the
mode regardless of the inherited environment (rationale at `main.go:74-79`), and
`cmd/converge-cli/main_test.go:154-176` tests exactly that with
`t.Setenv("CONVERGE_MODE", "hosted")`. The documented residual is now a documentation
defect: it tells operators to work around a hazard that no longer exists, and it
mis-describes the binary's behaviour. One-paragraph fix.

**I-2. `internal/auth/store.go:13` asserts a repository-wide exclusivity that is false.**
"Store is the only file in this repository that contains SQL." It is not:
`internal/db/db.go:85` creates `schema_migrations`, `:122` selects from it, `:181` inserts
into it, and `internal/db/migrations/0001_init.sql` is pure DDL. The constraint's
*substance* is intact — all value-bearing SQL is parameterised, and all *domain* SQL is in
`store.go` — but the sentence as written is disproved by a one-line grep, which makes it a
trap for the next reader auditing the same invariant. Narrow the claim to "the only file
containing domain SQL; schema and migration bookkeeping live in `internal/db`."

**I-3. `providerResolverTTL` bounds token *use*, not token *residency* — contradicting its
own doc comment.**
`internal/auth/resolver.go:15-16` says the constant "bounds how long a user's decrypted
tokens sit in memory." It does not. `Resolve` checks the TTL before *returning* a cached
entry (`resolver.go:75-77`) and overwrites the entry on a miss (`:82-84`), but nothing
evicts on elapse: the only deletion is `Invalidate` (`resolver.go:90-94`), called on
provider create/update/delete (`providers.go:81, 155, 178`) and account deletion
(`service.go:327`). A user who authenticates once and never returns leaves a
`*provider.Registry` holding plaintext `config.Secret` tokens resident in the `cache` map
for the remaining lifetime of the process. Two consequences: an unbounded map keyed by
user id (memory growth proportional to every user ever seen since boot), and a plaintext
token residency window of "forever" rather than 15 minutes — which is the property the
comment advertises and which a memory-disclosure or core-dump threat model would rely on.
Either add TTL eviction (a sweep pass over `cache`, or evict-on-read-miss) or correct the
comment to say the TTL bounds staleness, not residency.

**I-4. `internal/auth` has no `builder.go` (DOM-01 FAIL).**
`ai-guidance.md:170` and `scaffolding-checklist.md:139` require "every domain with a model"
to have a fluent builder with `Build()` validation. `internal/auth/model.go` declares two
models (`User:94`, `UserProvider:159`) and the package has no `builder.go`; construction is
via `NewUser` (`model.go:104`) and `NewUserProvider` (`model.go:185`). The consequence shows
up at `providers.go:105-151`, where `UpdateProvider` assigns six model fields directly
because there is no `row.Builder()` to go through — a direct violation of
`patterns-functional.md:13` ("all mutations occur via builders returning new instances").
`internal/session/builder.go:18` shows the pattern the codebase already follows elsewhere,
so this is an inconsistency within the project, not only against the skill.

Mitigating: the validating constructors do enforce invariants (`model.go:105-107` rejects
a bad username and empty id/hash; `model.go:186-189` rejects a bad slug and a missing
sealed token), slices are defensively copied on both ingress and egress
(`model.go:199-201`, `:216-224`), and `UpdateProvider` mutates a *value copy* so no caller
observes a partially-updated model. No correctness or security impact — this is a
structural conformance FAIL.

### Minor

**M-1. 422 responses echo internal error text.**
`internal/api/settings_providers.go:184` passes `err.Error()` straight into the
client-facing detail, so a malformed slug returns `"auth: a slug is 1 to 32 characters of
lowercase letters, digits, and hyphens, starting with a letter or digit: auth: invalid
input"` — package prefix, duplicated prefix, and the sentinel's text. The messages are
deliberately value-free (`auth/errors.go:36-38`), so nothing sensitive escapes; it is
contract cosmetics. Contrast the adjacent paths, which return curated messages
(`auth/errors.go:52`, `model.go:52-54`).

**M-2. A store fault during login is reported as `INVALID_CREDENTIALS` (401).**
`internal/auth/service.go:165-184` branches on `lookupErr != nil` without distinguishing
`ErrNotFound` from a genuine database failure, so a `UserByFold` error of any kind yields
401 plus a consumed throttle slot rather than 500. Fails closed and preserves the
timing-equalisation property, but mislabels a server fault and lets a transient DB problem
burn a user toward lockout.

**M-3. `Register` never clears the throttle on success.**
`Login` calls `Throttle.Succeed` after a successful verify (`service.go:185`), but
`Register` (`service.go:126-161`) does not, so IP failures accrued before a successful
registration stay on the counter. Arguably intentional — registration is throttled per IP
precisely to limit flooding — but it is an asymmetry with no comment explaining it.

**M-4. Commit `5fcb4cf`'s message still carries the overstated leak claim.**
Its body reads "no hosted token was in that list and one reaching git's stderr was logged
in clear." `.superpowers/sdd/plan/residuals.md` records that a later review established
there is no currently reachable production path by which a *raw* token lands on git stderr,
and `docs/tasks/task-005-hosted-multi-user-mode/execution-notes-6.md:33` carries the
corrected "defence in depth, not closure of a live leak" framing. I found no *code comment
or doc* still asserting the stronger claim — `internal/provider/github/client.go:189-195`
and `gitlab/client.go:239-242` are both accurately hedged. Commit messages cannot be
corrected without a history rewrite; the PR description should carry the corrected framing
so the merge commit is the accurate record.

**M-5. DOM-19 WARN.** Seven new test files use neither a `[]struct` table nor `t.Run`
subtests (enumerated in the DOM-19 row above). `testing-guide.md:39` states a preference,
so this is not a FAIL; noted because the branch adds ~2,900 lines of test code in that
style and it will set the local convention.

## Residual Triage

**R-1 — Task 26, the credential-injection path is unexercised by any test. → Still fine.
Non-blocking.**
The ruling's core reasoning verifies: `gitx.Run` logs category, repo, session, exit code,
duration, outcome, and `Redact`-ed stderr, and `Spec.Args`/`Spec.Env` appear in no log call
anywhere in the function (`internal/gitx/exec.go:172-196`). The gap is also narrower than
the residual describes, because guard tests now exist at the declaration layer:
`internal/provider/github/authorize_git_spec_secret_test.go:15-61` asserts the raw token is
absent from `spec.Env` *and* that both the token and the Basic blob are declared in
`spec.Secrets`; `internal/provider/gitlab/authorize_git_spec_secret_test.go:19-62` does the
same for GitLab; and `internal/provider/github/authorize_git_secret_test.go:33-113` drives
real git stderr end to end for both the raw token and the bare blob. What remains
unexercised is only the *composition* — a real review build through a real provider that
actually populates `GIT_CONFIG_*`. The named cost ("if future code logs `Spec.Args` or
`Spec.Env`, no test catches it") is real but is a hypothetical-future-regression guard, not
a present defect. I agree with the parking.

**R-2 — `gitx.ExitError.Result.Stderr` is a raw exported field. → Still fine. Ticket, not
this branch.**
`(*ExitError).Error()` emits only category and exit code (`internal/gitx/spec.go:58-60`),
and a repo-wide grep for `.Stderr` finds no production caller outside `internal/gitx`.
Reaching a log requires someone to write `slog.Any("err", ee)` *and* for `Result` to be
reached by the JSON handler's reflection — two future steps, neither present. A
`LogValue()` on `Result` is cheap and worth a follow-up ticket; it is not merge-blocking.

**R-3 — `spec_secrets_test.go` GitLab-filename asymmetry. → Still fine. Naming only.**
`internal/provider/gitlab/authorize_git_spec_secret_test.go:12-62` asserts both the raw
token and the Basic blob are declared, and the redaction mechanism they feed is shared
(`internal/gitx/exec.go:113-121`, `:190`). The only thing GitHub has that GitLab does not
is the end-to-end real-git-stderr exercise, and that tests `gitx`, not the provider. No
coverage gap.

**R-7 — `CLAUDE.md:60` says `internal/auth` "may import `db`". → Agree with the ruling.
Non-blocking; tightening optional.**
Confirmed independently: `go list -deps ./internal/auth` returns `config`, `identity`,
`gitx`, `provider`, `provider/github`, `provider/gitlab` — no `internal/db`. `NewStore`
takes a bare `*sql.DB` (`internal/auth/store.go:32`), so the coupling is to `database/sql`,
not to the package. The sentence grants a permission that the code declines to exercise,
which is not a crossed boundary, and it mirrors the doc comment at
`internal/auth/errors.go:5-10` (which has the same wording). Tightening both to describe
what the code actually does would be an improvement; leaving them is not a defect.

**R-8 — `cmd/converge-cli` honouring `CONVERGE_MODE`. → I differ from the recorded ruling.
Doc fix needed (see I-1).**
The residual asks me to "confirm the documented behaviour matches the code." It does not.
The residual and `docs/hosted-mode.md:106-110` both describe a CLI that would open the
hosted database if `CONVERGE_MODE=hosted` were inherited; `cmd/converge-cli/main.go:80`
pins `CONVERGE_MODE=standalone` and `main_test.go:154-176` tests it. So the *code* residual
was closed and only the *documentation* was left behind. My verdict: the code is correct
and better than documented; the doc is wrong and should be corrected before merge, since
it actively misinstructs operators.

**R-4, R-5, R-6** are frontend / test-helper / coverage-breadth items outside this audit's
scope. R-4 (native `<select>` vs. Radix `Select`) is explicitly the frontend reviewer's
ruling to make.

## The `internal/auth` Flake — Explicit Call

**The evidence retires it.** I could not identify a shared-state mechanism that would
produce a load-dependent failure in `internal/auth`:

- **No shared clock.** Time is injected per construction (`ServiceDeps.Now`,
  `internal/auth/service.go:65`; `NewThrottle(store, now)`, `throttle.go:47`), so tests
  advance their own clock rather than racing a global.
- **No shared temp dir or database.** Each store test opens its own handle; `db.Open`
  creates the parent directory per path (`internal/db/db.go:56-60`).
- **No unsynchronised counter.** The one read-modify-write is wrapped in a transaction
  (`Store.UpdateAttempt`, `store.go:382-417`), and the pool is pinned to a single
  connection (`db.go:70-71`), which serialises it.
- **Only one piece of package-level mutable state**, `dummyHash` (`password.go:183`), and it
  is written exactly once by `mustDummyHash()` at init and read-only thereafter
  (`service.go:170`).

The one coupling I *can* name, and the most plausible mechanism if it ever resurfaces:
every `internal/auth` test that hashes or verifies pays real Argon2id at 64 MiB
(`password.go:29`), and `hashConcurrency` is 4 (`service.go:29`), so the package can hold
~320 MiB transient while `go test ./...` runs other packages in parallel. Under genuine
memory pressure that surfaces as a timeout or an allocation failure — **not** as a wrong
assertion. So if it reappears, the first datum to capture is whether the failure is a
timeout/OOM or an assertion: the former points at the Argon2 memory budget under parallel
package load and is a harness-capacity question; only the latter would indicate a real
logic race. Given ~20 consecutive clean runs including a dedicated
`go test -race -count=3 ./internal/auth/` hunt and two full CI gates, I do not consider
this a merge risk.

## Summary

### Blocking (must fix)

None. No security, isolation, or correctness defect was found.

### Should fix before merge (cheap, and both are correctness-of-record issues)

- **I-1** `docs/hosted-mode.md:106-110` — delete or rewrite the stale `converge-cli`
  `CONVERGE_MODE` warning; the code pins standalone at `cmd/converge-cli/main.go:80`.
- **I-2** `internal/auth/store.go:13` — narrow "the only file in this repository that
  contains SQL" to exclude `internal/db`'s schema bookkeeping.

### Should fix (guideline conformance / bounded risk)

- **I-3** `internal/auth/resolver.go:15-16` — either evict cache entries on TTL elapse or
  correct the comment; as written the plaintext-token residency claim is wrong.
- **I-4 / DOM-01 + DOM-IMM** `internal/auth` — add `builder.go` with `NewBuilder()`,
  fluent setters, `Build()` validation, and a `Builder()` method on each model, then route
  `UpdateProvider` (`providers.go:105-151`) through it.

### Non-blocking

- **M-1** `settings_providers.go:184` returns raw `err.Error()` as the 422 detail.
- **M-2** `service.go:165-184` maps a store fault to 401 instead of 500.
- **M-3** `Register` (`service.go:126-161`) does not call `Throttle.Succeed`.
- **M-4** commit `5fcb4cf`'s body retains the overstated leak framing; carry the corrected
  framing in the PR description.
- **M-5 / DOM-19** seven new test files are one-func-per-case rather than table-driven.
- **R-2** a `LogValue()` on `gitx.Result` is worth a follow-up ticket.

### Verdict

**Checklist status: NEEDS-WORK** — DOM-01 and DOM-IMM fail for `internal/auth`, and per the
audit contract a single FAIL prevents an overall PASS. There is no curve.

**Merge recommendation: approve once I-1 and I-2 are fixed.** Both are one-paragraph
documentation edits to statements that are *disprovably* false, on a branch whose entire
safety argument rests on documented invariants being trustworthy. The DOM-01 failure is
structural conformance with no correctness or security consequence and can land as a
follow-up. I-3 is the only finding with any runtime weight, and its impact is memory
residency rather than disclosure.

All five CI gates pass. Every binding security constraint in the plan — SQL
parameterisation, shell-free git with env-only credential injection, exact Argon2id and
AES-256-GCM parameters with AAD row-binding, cookie shape and hash-only persistence,
secret redaction, dependency direction, `CGO_ENABLED=0` with the pure-Go driver, the ten
error-code/status pairs verbatim, and multi-tenant isolation across sessions, mirrors, and
provider configs — verified with file:line evidence above. I found no path by which one
user reads, mutates, or deletes another's data, no path by which a secret reaches a log,
and no 403-vs-404 existence oracle.
