# Final whole-branch review — task-005 hosted multi-user mode

Reviewer: broad merge review (correctness, security, architecture, maintainability, coherence).
Range `7ae00b9..f916444`, 50 commits, 140 files, +24,703/−271.
Scope note: per the brief I ran **no** build/test gate. Evidence for the gates is the captured
`.superpowers/sdd/plan/task-28-gate.log` (all five commands, real output, `EXIT_CODE=0` for
`make lint`, `make test`, `make test-integration`, `make build`, a post-frontend-build
`go test -race ./...`, and a genuine `make docker-build` image export). Read-only commands I ran:
`git log/diff/show`, `grep`, `sed`, `go list -deps ./internal/auth`, `go list -deps ./internal/identity`,
`go list -deps ./internal/db`.

## Verdict

**Merge, after the four Important findings below.** None of them is a correctness or security
defect in the hosted path, none is a standalone regression, and I found no cross-tenant read,
write, or delete path. Two of the four are documentation/comment statements that are now false,
one is a duplicated form body, one is a throttle-reset semantic that contradicts its own stated
requirement. All four are small, local edits.

## What this branch gets right

These are verified, not courtesies.

- **Standalone equivalence is evidenced, not asserted.** `config.Load` reaches `loadHosted` only
  under `CONVERGE_MODE=hosted` (`internal/config/config.go:165-169`), so `DatabasePath` and
  `SecretKey` stay zero; `app.New` gates every hosted construction on the same condition
  (`internal/app/app.go:310-345`), and `db.Open` is unreachable otherwise. The test that proves
  it asserts the right things — `App.DB == nil`, `App.Auth == nil`, *and* `os.Stat` on both the
  configured path and the default `/data/converge.db`
  (`internal/app/app_test.go:378-406`). On the HTTP side the auth/settings routes are not
  registered at all in standalone (`internal/api/router.go:130-145`) so the existing `/api/`
  catch-all produces the 404s, with no "if standalone" branch in any handler;
  `TestStandaloneRouterIsUnchanged` (`internal/api/isolation_test.go:377-407`) walks all six
  routes and asserts 404. The two middlewares are constructed only in hosted mode
  (`router.go:163-170`), so standalone's request path genuinely gains zero comparisons.
  The one additive change in standalone is `GET /api/auth/mode`, registered in both modes
  (`router.go:113`) because the SPA needs it before it knows anything — additive, documented,
  and correct.
- **The zero-value `identity.Scope` question is answered everywhere it matters.** The field is
  unexported so a `Scope` cannot be forged outside the package (`internal/identity/scope.go:9-16`).
  Every hosted entry point is wrapped by `authenticate` (`router.go:168`), so `scopeFrom`'s
  standalone fallback (`internal/api/authctx.go:30-35`) is unreachable for a scoped deployment.
  Where a zero scope would be dangerous, it is refused rather than defaulted:
  `auth.ProviderResolver.Resolve` errors on an unscoped identity in hosted mode
  (`internal/auth/resolver.go:64-70`), `session.Store.Purge` refuses an empty user id
  (`internal/session/store.go:286-288`), `mirror.Cache.PurgeNamespace` refuses the root namespace
  (`internal/mirror/cache.go:105-117`), and `Sealer.Seal` refuses empty ids
  (`internal/auth/crypt.go:64-66`). `Service.PurgeUser` crosses two of those guards at once
  (`internal/review/service.go:643-655`).
- **404-vs-403 is decided by the store, not by handlers.** `Store.Get`/`Store.List` filter on
  `scope.Matches(owner)` (`internal/session/store.go:186-222`), which flows into the existing
  `ErrNotFound → 404` mapping. Every SQL read for a provider configuration carries `user_id` in
  the WHERE clause (`internal/auth/store.go:266-331`), so a foreign id and an unknown id are
  indistinguishable. The two deliberate handler-level exceptions are both recorded in the plan
  and both check out: `deleteReview` (`internal/api/reviews.go:160-190`) keeps the pre-hosted
  unconditional 204 in standalone and answers 404 for unknown *and* foreign ids in hosted;
  `sessionFor` consults `Corrupted` only when unscoped (`reviews.go:121-135`), which is right —
  a 500 on a corrupt record would confirm the id exists.
- **Token-at-rest handling is sound.** AES-256-GCM with `user_id\0provider_id` as AAD
  (`internal/auth/crypt.go:49-73`) means a ciphertext moved between rows or users fails to open.
  `UserProvider` has no plaintext-token field at all (`internal/auth/model.go:165-181`), the wire
  shape has none either (`internal/api/settings_providers.go:19-28`), and `token_last4` is
  captured at write time so rendering a mask never decrypts. Argon2id reads m/t/p back from the
  stored PHC string with ceilings that fail closed on a tampered row
  (`internal/auth/password.go:36-46, 138-143`) — the OOM/hang vector is closed and tested with a
  deadline rather than by eyeballing.
- **Architecture holds mechanically and substantively.** `go list -deps` confirms production
  `internal/auth` imports only `config`, `identity`, `provider`, `provider/github`,
  `provider/gitlab` (and `gitx` transitively) — no `review`, no `session`, no `db`;
  `internal/identity` and `internal/db` are stdlib-only leaves. The account-deletion cascade
  crosses the boundary through `auth.Purger`/`auth.ProviderUsage` wired in `internal/app`
  (`internal/auth/service.go:31-47`, `internal/app/app.go:354-358`), which is the correct
  direction. More importantly it does not *feel* bolted on: the `provider.Resolver` seam
  (`internal/provider/resolver.go`) is the one real architectural change, and it is the right
  one — "the set of providers is a function of the caller" is exactly what hosted mode means, and
  standalone gets a three-line static wrapper. `internal/auth` is large (≈2,100 production lines
  across 9 files) but it is not doing too much: each file is a distinct concern and the SQL is
  confined to one.
- **`mirror` namespacing is explicit, not implicit.** `Namespace` is a required argument on
  `Path`/`Ensure`/`FetchSHA` (`internal/mirror/cache.go:77-180`), so every call site had to be
  visited by the compiler, and a user id is re-validated against `^[0-9a-f]{16}$` before it
  becomes a directory name (`cache.go:18-22`) rather than trusted because it came from a session.
  `TestTwoUsersReviewTheSameRepositoryInSeparateMirrors`
  (`internal/review/hosted_integration_test.go:141-212`) asserts both mirrors exist, that the
  paths differ, that nothing landed under the bare cache root, that A's list contains only A's
  session, and that A cannot `Get` B's — real evidence, not a smoke test.
- **Secret redaction has a real mechanism and the honest limits are written down.** The
  per-invocation `Spec.Secrets` unioned with the runner-wide list (`internal/gitx/exec.go:109-120`)
  is the only channel a per-user token has, because `Options.Secrets` is fixed at construction;
  both providers declare the raw token *and* the base64 Basic blob derived from the shared
  `gitx.BasicAuthBlob` (`internal/provider/github/client.go:189-196`,
  `internal/provider/gitlab/client.go:238-243`). I tried to find a path through it: the only
  production consumer of unredacted stderr would be `ExitError.Result.Stderr`, and `grep` shows
  **no production caller reads it** (the only `.Stderr` references outside `gitx` are
  `os.Stderr` writers and test helpers), while `exec.go:159-190` logs only category, repo,
  session, exit code, duration, and redacted stderr — never `Args` or `Env`.

## Critical

None.

## Important

### I1. `docs/hosted-mode.md:102-110` describes `converge-cli` behaviour that the code no longer has

The closing section states: "`CONVERGE_MODE=hosted` present in the CLI's own environment would
still cause it to attempt to open the database — operators should not set `CONVERGE_MODE=hosted`
in a shell where `converge-cli` runs."

That was true when the doc was written and stopped being true in commit `fc657d4`.
`cmd/converge-cli/main.go:74-79` now calls
`newApp(ctx, append(os.Environ(), "CONVERGE_MODE=standalone"))`, and `config.Load` builds its
map by iterating the slice in order (`internal/config/config.go:123-129`) so the appended
duplicate wins. The CLI is pinned to standalone and cannot open the database.

This matters more than a stale sentence usually would, because the documented hosted deployment
puts `CONVERGE_MODE=hosted` in the container's environment (`docker-compose.yml:40-42`) — i.e.
the doc tells an operator to avoid the exact configuration the project ships, for a reason that
no longer exists, and simultaneously misdescribes what the binary does.

This is recorded residual #8, whose ruling was "confirm the documented behaviour matches the
code". It does not. Fix the doc (the behaviour is correct).

### I2. `apps/frontend/src/components/features/settings/UserProviderForm.tsx:73-158` and `:196-281` are the same form body twice

`CreateUserProviderForm` and `EditUserProviderForm` are two ~110-line components whose JSX is
identical except for four things: the slug field (editable input vs. read-only text), the token
placeholder, the submit label, and one extra error-code mapping. A mechanical diff of the two
return bodies shows roughly 75 of 86 lines matching verbatim, including all of the display-name,
kind, base-URL, and token field markup and their `aria-invalid`/error-paragraph wiring.

The failure mode is the ordinary one: the next field, validation tweak, or accessibility fix
lands in one form and not the other, and nothing in the test suite notices because each form has
its own test. Extract the shared fields into one component that takes the register function and
errors, keeping the four genuine differences as props.

### I3. `apps/backend/internal/auth/store.go:107-109` claims a cleanup that does not happen, and the lockout outlives the account

`DeleteUser`'s doc comment says "Login sessions, provider configs, and the user's own attempt row
cascade via ON DELETE CASCADE / explicit key match (FR-2.7)". The first two do cascade
(`internal/db/migrations/0001_init.sql:21, 31`). The third does not: `login_attempts` has no
foreign key at all (`0001_init.sql:47-54`) — it is keyed `(scope, key)` where `key` is a folded
username or an IP — and `DeleteUser` executes only `DELETE FROM users`
(`store.go:110-123`). There is no "explicit key match" anywhere; `grep` shows the only other
writers are `ClearAttempt` (on successful login) and `DeleteElapsedAttempts` (the sweeper).

Consequence beyond the wrong comment: a user who deletes their account while holding a
non-zero failure counter leaves that row behind, and because the key is the *folded username*
rather than a user id, a subsequent registration of the same username inherits it — including an
in-force `locked_until`. The window is bounded by the sweeper, but the sweeper's own condition
(see I4) is not obviously tight enough to make that reasoning safe by construction.

Either delete the user's `login_attempts` row in `DeleteUser` (one `ExecContext`, same
transaction-free style as the rest), or correct the comment and accept the inheritance
explicitly. I would do the former.

### I4. The username failure counter is windowed by the sweeper, contradicting `throttle.go:83-84` and FR-7.2

`internal/auth/throttle.go:82-88` documents, and the code implements, a deliberate asymmetry: the
IP counter is windowed at 15 minutes, "the username counter is consecutive-failure based and
resets only on success (FR-7.2), so it has no window."

The sweeper undoes that. `Throttle.Sweep` calls
`DeleteElapsedAttempts(now - ipWindow)` (`throttle.go:123-125`) and the statement is
`DELETE FROM login_attempts WHERE locked_until <= ? AND window_start <= ?`
(`store.go:429-432`) — with no `scope` predicate. For a user-scope row that has not yet reached
the threshold, `locked_until` is the zero time (persisted as a large negative unix value by
`unix(time.Time{})`, `store.go:34`), so the first clause is trivially true, and `window_start` is
the timestamp of the first failure. Any user counter whose first failure is more than 15 minutes
old is therefore deleted on the next sweep — i.e. the username counter *does* reset without a
success, on a schedule of `CLEANUP_INTERVAL_MINUTES` (default 30).

Practical effect: an attacker pacing guesses so that no counter survives a sweep never reaches
the five-failure username lockout. The residual protection is the per-IP counter (20 per 15
minutes), which a distributed attacker does not pay. The absolute rate this permits is low, so
this is not a break — but it is a documented security control behaving differently from its
documentation and from the FR it cites, which is exactly the class of thing that gets relied on
later.

Two acceptable fixes: add `AND scope = 'ip'` (or `AND failures = 0 OR locked_until > 0`
semantics) to the sweep so user counters are only reaped once their lockout has genuinely
elapsed; or change FR-7.2 and the comment to state that the username counter *is* reaped after an
idle window. Pick one; do not leave the comment and the SQL disagreeing.

## Minor

- **M1 — the "live leak" framing survives in one comment.**
  `apps/backend/internal/provider/github/authorize_git_secret_test.go:23-24` still reads "is the
  end-to-end guard for **the hosted leak**". The body immediately below (`:29-32`) carries the
  corrected framing ("Production never puts a token in argv … this test simulates the general
  case"), and `execution-notes-6.md:33` records the correction properly, so this is the last
  sentence holding the stronger claim. Commit `5fcb4cf`'s message body ("one reaching git's
  stderr was logged in clear") also states it, but commit messages are immutable history and the
  branch carries its own correction; I would not rewrite them. Reword the one test comment to
  "defence in depth for a per-user credential surfacing in git's diagnostics".
- **M2 — misplaced doc comment.** `internal/config/config.go:220-223` is `loadProviders`'
  documentation but sits immediately above `modeVar`. Introduced on this branch by inserting
  `modeVar` between the comment and its function.
- **M3 — production-dead `auth.Store.SaveAttempt`.** `internal/auth/store.go:365-373` is the
  non-atomic upsert that commit `32f668e` superseded with `UpdateAttempt`
  (`store.go:382-417`). Its only remaining caller is `store_test.go:323`, seeding a row. Leaving
  an exported, non-transactional writer beside the transactional one invites a future caller to
  reintroduce the lost-update race the commit fixed. Unexport it, or document it as
  test-seeding-only in one line.
- **M4 — production-dead `auth.NewUserProvider`.** `internal/auth/model.go:188-209` validates the
  slug and the required fields, but the only production writer,
  `Service.CreateProvider` (`internal/auth/providers.go:70-77`), builds the struct literal
  directly (it can, same package). So the constructor's invariants guard the tests and not the
  code. Have `CreateProvider` go through it.
- **M5 — `DeleteOtherLoginSessions` fails open on a nil `keep`.**
  `internal/auth/store.go:185-192` uses `token_hash != ?`; with a nil `[]byte` that parameter
  binds to SQL NULL, `x != NULL` evaluates to NULL, and **no rows are deleted** — the opposite of
  "revoke every other session". Unreachable today: `ChangePassword` is an authenticated route, so
  `tokenHashFrom` (`internal/api/authctx.go:39-44`) always returns the hash `authenticate`
  attached. Worth making fail-closed anyway, since the defensive direction here is "delete
  everything" and the current direction is "delete nothing".
- **M6 — a database fault looks like a bad password.** `Service.Login`
  (`internal/auth/service.go:174-192`) treats any non-nil `lookupErr` — not just `ErrNotFound` —
  as invalid credentials, and records a throttle failure for it. A transient store error during
  a SQLite hiccup therefore both lies to the user and pushes their username and IP toward a
  lockout. Distinguish `ErrNotFound` from everything else and return the real error for the rest.
- **M7 — `validationError` echoes internal error text.**
  `internal/api/settings_providers.go:184` sends `err.Error()` to the client, which includes the
  `auth: ` package prefix and the `: invalid input` wrapper suffix. No value or secret leaks (the
  four `ErrInvalidInput` messages are deliberately value-free), so this is cosmetic contract
  noise only.
- **M8 — the end-to-end log test runs below the level it most wants to observe.**
  `internal/app/secrets_test.go` sets `LOG_LEVEL=info`, but the git-stderr line it is implicitly
  protecting is logged at `Debug` (`internal/gitx/exec.go:190`). The test is still valuable — it
  exercises the real production-wired logger across register/login/provider-create/review-create
  and its own doc comment is unusually honest about what it does and does not prove — but the
  gitx path is covered by `internal/gitx/spec_secrets_test.go` and the two provider tests, not by
  this one. Raising it to `debug` would close the gap for free.
- **M9 — `CLAUDE.md:59-61` grants an import that production does not use.** It says
  `internal/auth` "may import `db`". `go list -deps ./internal/auth` shows it does not:
  `store.go` takes a bare `*sql.DB` and only `_test.go` files reach `internal/db`. This is a
  permission, not a crossed boundary (recorded residual #7), but describing what the code
  actually does is stronger: `auth` takes a `*sql.DB` and never names the `db` package.
- **M10 — native `<select>`.** `UserProviderForm.tsx:103-110` and `:232-239` use a native select
  rather than the shipped Radix `Select`. Recorded residual #4; a UI-consistency call that belongs
  to the frontend reviewer, and my I2 is the larger issue in the same file.

## The `internal/auth` flake — explicit call

**The evidence does not retire it, and I can name a plausible mechanism. It is not a merge
blocker.**

The prime suspect is
`apps/backend/internal/auth/service_test.go:224-257`,
`TestLoginIsIndistinguishableBetweenUnknownUserAndWrongPassword`. It is a **wall-clock timing
assertion**: three rounds of (unknown-username login, wrong-password login), then
`if um < wm/2 || wm < um/2 { t.Fatalf(...) }` on the medians. Everything about its environment
works against it:

- it calls `t.Parallel()` (`:225`), so it runs alongside the rest of the package;
- each `Login` performs a 64 MiB Argon2id verify, and `auth.Service` admits only four at a time
  (`hashConcurrency = 4`, `internal/auth/service.go:22, 93-103`), so a sample's latency includes
  time spent queueing behind *other parallel tests'* hashes;
- the suite runs under `-race`, which inflates and destabilises those timings further;
- a median of three is a single-sample defence — one stalled round shifts it outright.

That profile matches the reported sighting precisely: seen twice under full-suite load by
different agents, never reproduced in a focused `./internal/auth/ -count=3` run, because a
focused run removes exactly the contention that makes it fail. ~20 clean runs do not retire a
load-dependent 2x ratio check; they bound its rate, which is all they can do.

Secondary suspect, same class: `internal/auth/password_test.go:147-167` gives
`VerifyPassword` a 200 ms wall-clock deadline to reject an oversized-parameter hash. The
function should return in microseconds, so the margin is large — but it is still a wall-clock
deadline in a parallel, `-race`, 64-MiB-allocating package.

I found no shared-mutable-state mechanism: each fixture gets its own store and its own temp
database, the stubs guard their state with mutexes (`service_test.go:36, 63`), and `now` is
injected rather than read from the wall clock for anything functional. So this is a test-harness
robustness issue, not a product race — which is the good version of this answer, but it should be
fixed rather than left to re-surface in CI. Recommended follow-up (a ticket, not a merge gate):
make the timing test load-independent — count Argon2id invocations through an injected hook
instead of measuring them, or at minimum drop `t.Parallel()` from it, widen the ratio to 4x, and
take a median of five.

## Residual triage

| # | Item | Verdict |
|---|------|---------|
| 1 | Credential-injection path unexercised (`fake.AuthorizeGit` is a no-op) | **Still fine, do not block.** I verified the code answers it rather than a test: `grep` finds no production reader of `ExitError.Result.Stderr`, and `internal/gitx/exec.go:159-190` logs only category/repo/session/exit/duration/redacted-stderr — never `Spec.Args` or `Spec.Env`. The ruling's stated cost (a future logger of `Args`/`Env` goes uncaught) is real; a ticket for a one-assertion guard is proportionate. |
| 2 | `gitx.ExitError.Result.Stderr` is a raw exported field | **Still fine; ticket the `LogValue()`.** Zero production consumers today. Worth noting the stakes have changed slightly under hosted mode: the app-level `redactingHandler` is built from `config.Config.Secrets()` (`internal/app/app.go:199`), which *cannot* contain a per-user token, so if a caller ever does pass this to `slog`, hosted tokens go through unredacted where env-configured ones would not. Follow-up, not a blocker. |
| 3 | GitLab `spec_secrets_test.go` filename asymmetry | **Still fine.** Naming only. The mechanism is shared in one place (`exec.go:109-120`) and the GitLab declaration has its own guard (`gitlab/authorize_git_secret_test.go:17-43`). |
| 4 | Native `<select>` instead of Radix `Select` | **Frontend reviewer's call** (my M10). I would not block on it. My I2 — the duplicated form body in the same file — is the finding I would fix first. |
| 5 | `renderPageWithClient` duplicates `render.tsx` | **Still fine.** Test-helper duplication, no behavioural risk. |
| 6 | Task 22 mutation coverage non-exhaustive | **Still fine.** Coverage breadth, not a defect. |
| 7 | `CLAUDE.md:60` says `auth` "may import `db`" | **Tighten the wording** (my M9). Confirmed by `go list -deps` that production `auth` does not import `db`. Not a blocker; it is a permission statement, but describing the actual shape is better documentation. |
| 8 | `converge-cli` shares `app.New` and would honour inherited `CONVERGE_MODE=hosted` | **Ruling no longer matches the code — see I1.** The code was fixed in `fc657d4`; the doc was not updated. This is the one residual I am reversing. |

### Narrative correction

Applied in the right places (`execution-notes-6.md:33` states the defence-in-depth framing
plainly), with one survivor: the "the hosted leak" phrasing at
`internal/provider/github/authorize_git_secret_test.go:23-24` (my M1).

## Coherence across the 50 commits

I looked specifically for abandoned half-migrations, two-ways-of-one-thing, silently dropped
patterns, and dead code from superseded approaches.

- **No abandoned migrations.** The `Registry → Resolver` change (`461ea8b`) is complete: no
  production call site reaches `Registry.Get` directly any more; `App.Registry` survives only as
  the thing the static resolver wraps (`internal/app/app.go:264-281`), which is legitimate.
  Scope threading (`efc8e3d`, `0494dc6`, `e145057`) reaches every seam — `session.Store`,
  `mirror.Cache`, `review.Service`, and both binaries — with no "TODO: thread scope here"
  remnants. No `TODO`/`FIXME`/`XXX`/`HACK` markers anywhere in the changed Go or TS (the four
  `grep` hits are `xxx` inside test token fixtures).
- **Two superseded-approach remnants**, both exported and both now test-only: `SaveAttempt`
  (M3) and `NewUserProvider` (M4). Neither is harmful today; both are the shape that decays.
- **One pattern applied consistently**, worth crediting: every new guard fails closed
  (`resolver.go:64-70`, `store.go:286-288`, `cache.go:105-117`, `crypt.go:64-66`,
  `settings_providers.go:182-188`, `password.go:138-143`). That is not an accident of review; it
  is visible as a convention.
- **The fix commits read as genuine fixes, not churn**: `32f668e` (atomic counter), `01cf6e2`
  (Argon2 ceilings), `13bea39` (backslash in `safeNext`), `a872dea`
  (UNAUTHENTICATED vs INVALID_CREDENTIALS on 401), `7637842` (fail closed on unrecognised
  provider-settings errors), `799f3a3` (retry Purge cleanup via Sweep). Each closes something a
  reviewer would otherwise have found here.

## Adversarial attempts that found nothing

Recording these so a reader knows what was actually tried rather than assumed.

- **Cross-tenant session access**: `Get`/`List`/`Files`/`FileDiff`/`CombinedDiffPath`/`Finish` all
  take a scope and funnel through `Store`'s `scope.Matches(owner)` filter
  (`internal/review/service.go:88-105, 588-636`, `internal/session/store.go:186-222`).
  `StartBuild` deliberately uses the standalone scope for its lookup (`service.go:217-221`) but
  then derives the acting scope from the *persisted owner* via `scopeOf`
  (`service.go:413-425`), so a background build cannot be made to act as another user.
- **Cross-tenant provider access**: every read and write is `WHERE user_id = ?`
  (`internal/auth/store.go:266-331`), `DeleteProvider` asks `ProviderInUse` with
  `identity.ForUser(userID)` not the request scope (`internal/auth/providers.go:169`), and the
  resolver caches per user id with invalidation on every write path
  (`internal/auth/resolver.go:88-94`, called from `providers.go:81, 155, 178` and
  `service.go:321`).
- **Mirror crossing**: `PurgeUser("")` is double-guarded (`Store.Purge` errors, and
  `NamespaceFor(ForUser(""))` yields the root namespace which `PurgeNamespace` refuses) —
  `internal/review/service.go:643-655`.
- **CSRF / origin**: `originGuard` (`internal/api/authmw.go:40-61`) runs before `authenticate`,
  exempts only GET/HEAD and non-`/api/` paths, and requires either
  `Sec-Fetch-Site: same-origin` or an `Origin` whose host equals `r.Host`. A request with neither
  header is rejected — fail closed. Combined with `SameSite=Lax` this is the whole defence, as
  FR-4.4 states.
- **Open redirect**: `safeNext` (`apps/frontend/src/lib/api/formErrors.ts:11-17`) rejects
  anything not starting `/`, anything starting `//`, and anything containing a backslash;
  `URLSearchParams` decodes `%5C` before that check, so the percent-encoded bypass is covered.
- **Cookie**: `HttpOnly`, `SameSite=Lax`, `Secure` whenever `r.TLS != nil` or
  `CONVERGE_SECURE_COOKIES=true`, `MaxAge` mirroring absolute expiry
  (`internal/api/authctx.go:69-81`). Only the SHA-256 is persisted
  (`internal/auth/service.go:105-121`, `0001_init.sql:20`).
- **Throttle key forgery**: `X-Forwarded-For` is honoured only under
  `CONVERGE_TRUSTED_PROXY` and then only its rightmost hop
  (`internal/api/authctx.go:98-112`) — the correct choice, since that is the entry the nearest
  trusted proxy appended. The reasoning is also documented for operators
  (`docs/hosted-mode.md:27-33`).
- **Path-prefix evasion of `authenticate`**: `//api/reviews` and `/api/../api/reviews` are
  cleaned-and-redirected by `http.ServeMux` rather than served, and a path that fails the
  `/api/` prefix check is served by the UI handler, not by a review handler. `publicRoute`
  (`internal/api/authmw.go:20-29`) matches exact paths, so any near-miss requires authentication
  — fail closed.

## Could not verify

- **The gates themselves.** I read `task-28-gate.log` rather than running them, per the brief.
  The log is internally consistent (timestamps advance, real tool output, five explicit
  `EXIT_CODE=0` markers, a real multi-stage Docker export), so I treat it as credible evidence —
  but it is evidence about a tree state, and my finding-fix edits will need a re-run.
- **Live provider behaviour.** Every GitHub/GitLab interaction on this branch is exercised against
  `httptest` stand-ins. The credential-injection path in particular is verified structurally
  (env construction, secret declaration) rather than against a real remote; residual #1 is the
  recorded form of that gap and `docs/manual-checklist.md` carries the manual sweep.
- **The flake.** I did not attempt a reproduction run (instructed not to). My call above is a
  code-level mechanism argument, which I believe is the stronger artefact, but it is an argument.
