# Execution notes — Tasks 14–18

Phase 4 (`/execute-task task-005`, subagent-driven development), session 3. Session 1 ran
Tasks 1–8, session 2 ran Tasks 9–13 (see `execution-notes.md` and `execution-notes-2.md`).
Session 3 ran Tasks 14–18 and handed off at the context threshold, on the boundary where the
backend domain and handler layers finish and the wiring tasks begin.

The live ledger is `.superpowers/sdd/plan/progress.md` (git-ignored scratch). This file is
the committed summary.

## What landed

| Task | Commit | Result |
| --- | --- | --- |
| 14 — `auth/service.go`, the account lifecycle | `f3c04f2` | Review clean, 3 minors |
| 15 — provider-config CRUD + the sweeper | `ce737be` | Review clean, 2 minors |
| 16 — `api` scope context, cookies, two middlewares | `beb27ff` | Review clean, 2 minors |
| 17 — the `/api/auth/*` handlers | `b5949cc` | Review clean, 2 minors |
| 18 — the `/api/settings/providers` handlers | `789543a`, fix `7637842` | **1 Critical**, clean after 1 fix round |

**The backend is now feature-complete below the router.** Every `internal/auth` file the plan
lists exists and is reviewed, and all `/api/auth/*` and `/api/settings/providers` handlers are
written. Tasks 19–20 wire routes and the app; Task 20 is where standalone equivalence finally
becomes provable end to end.

Every commit carries the required `Co-Authored-By` trailer.

## The one Critical finding, and how it was caught

Task 18's `writeProviderSettingsError` routed `*auth.Error` and `auth.ErrNotFound` to the
domain-error writer and funnelled **everything else** into `VALIDATION_ERROR` 422 with the raw
`err.Error()` as a client-visible detail. Its doc comment justified this by asserting every
remaining plain error from the provider layer was "a fixed, value-free message." That claim was
false for three families, all verified in code:

- **DB faults** — `Store.CreateUserProvider`'s four `%w`-wrapped errors (`store.go:233-256`).
- **Provider outages** — `httpVerifier.Verify` types *only* `provider.ErrAuth`; an unreachable
  host, upstream 500, or timeout stays plain (`verify.go:34-51`).
- **Crypto faults** — `Sealer.Seal` nonce failure and `Sealer.Open` decrypt failure
  (`crypt.go:63-86`).

So a database fault or an upstream outage on a credentials endpoint answered 422 with internal
error text. No token leaked, which is exactly why `TestNoResponseEverContainsAToken` never
caught it — that test was not the guard.

**Fixed by inverting the logic rather than enumerating more bad cases:** a new
`auth.ErrInvalidInput` sentinel (`errors.go:39`, following the existing `ErrNotFound` pattern),
wrapped by the four genuine input-validation failures, with everything unrecognised falling to
`writeDomainError` → `classify`'s fixed 500. The handler's redundant empty-token pre-check was
dropped once the domain-level check was mutation-proven load-bearing. 6/6 mutations killed.

**The lesson worth carrying:** the defect lived in a *comment's claim about invariants*, not in
obviously wrong code. The implementer, its own mutation run, and the token-leak test all passed
over it. It surfaced only because the controller read the committed error-writing helper and
checked the comment's assertion against the actual error paths.

## Rulings made on the user's behalf

Each is a decision taken so execution could continue. Rework any that are wrong.

1. **Task 14's test-15 substitution upheld.** The brief asked for a concurrency test against
   `Service.ListProviders`, which does not exist until Task 15; the implementer used
   `Store.ListUserProviders`. Upheld — it exercises the same single-connection read/write race,
   and the brief's reference was a forward reference, i.e. a plan-text defect. *Cost if wrong:*
   coverage sits at the store layer, so a service-level locking mistake in Task 15 would not be
   caught there. Discharged by requiring Task 15 to test the service method (ruling 2).

2. **Task 15's `ListProviders`-concurrency obligation closed, not escalated.** The reviewer
   showed my Task 14 condition was worded for a risk that does not exist: `Service` has no mutex
   the CRUD path touches (`service.go:77-79` holds only `hashSem`), and `ProviderResolver.mu` is
   taken only around a map delete (`resolver.go:92-95`), never across a DB call. "A lock held
   across a DB call" is structurally absent, not unverified. The new test does cover the real
   risk — `CreateUserProvider`'s `BeginTx` racing `ListUserProviders`' bare query under
   `MaxOpenConns(1)`. *Cost if wrong:* any later task that introduces a lock into that path must
   bring its own deadlock test, because this one will not catch it.

3. **The empty-token 422 was routed to Task 18 rather than fixed in Task 15.** A reviewer Minor,
   which on checking was sharper: `api-contracts.md:268,275` require `VALIDATION_ERROR` 422, but
   the untyped domain error fell through `classify` to 500. The contract puts the check in
   request-body validation, so Task 18 owned it. *Outcome:* Task 18 satisfied it, and fix round 1
   moved the guarantee into the domain sentinel, which is the more durable place.

4. **Task 16's out-of-scope `classify` change accepted.** The implementer added the `*auth.Error`
   arm that plan Task 17 Step 1 nominally owns, because Task 16's tests cannot assert the
   contracted 401/403 codes without it. *Cost if wrong:* nothing in shipped code — both tasks need
   the identical arm. Task 17 was told to verify rather than duplicate it, and did.

5. **Logout is exempt from `authenticate` and always answers 204.** `api-contracts.md:113-118`
   says "Requires a session" but documents only a 204 and no 401 for logout. The plan's explicit
   `TestLogoutIsIdempotent` is the more specific instruction, and gating logout would mean a user
   holding an expired cookie could never clear it — the one thing logout exists for. *Cost if
   wrong:* a sessionless logout answers 204 instead of 401; no security impact, since no row is
   deleted either way and the cookie clear is identical. **Task 19 must wire logout outside
   `authenticate`; Task 28 should clarify `api-contracts.md:115`.**

6. **My own Task 18 dispatch was wrong, and the implementer was right to deviate.** I told it to
   reuse "an existing input-error type that `classify` maps to `VALIDATION_ERROR`." No such
   mechanism exists in this repo. It used the brief's `validationError` helper instead, matching
   the `execution-notes.md` precedent. Accepted with no rework.

## Carry-forward for later tasks

- **Task 19 — three assumptions Task 16's correctness rests on, all binding, and its review must
  verify each:**
  1. `originGuard`/`authenticate` may be constructed **only in hosted mode** — a nil `Deps.Auth`
     panics at `authmw.go:210` if `authenticate` is wired unconditionally, which would also break
     standalone equivalence.
  2. `originGuard` must be chained **before** `authenticate`, as the code comments assert.
  3. `Deps.LoginSessionTTL` must be wired **non-zero**. `setSessionCookie` (`authctx.go:103`)
     computes `MaxAge` as `int(TTL.Seconds())`, and a zero TTL emits `MaxAge: 0`, which per
     RFC 6265 means "delete immediately" rather than "session cookie" — a wiring slip that fails
     silently in the wrong direction.
  4. Logout must sit **outside** `authenticate` (ruling 5).
- **Task 20** — the hosted wiring is still where standalone equivalence becomes provable; Tasks 13
  and 16–18 all added code whose standalone behaviour no single task's review could verify.
- **Task 28** — three documentation corrections now, not one: the Task 12 plan-text error
  (`scopeFrom` is Task 16's, not Task 14's), the Task 14 forward reference to
  `Service.ListProviders`, and `api-contracts.md:115`'s logout wording (ruling 5).
- **Anyone changing the DB pool** — unchanged from sessions 1–2: `auth.Store`'s uniqueness checks
  are race-free only because `internal/db` sets `MaxOpenConns(1)` (`db.go:71-72`, verified).

### Discharged

- **Task 5's Argon2id carry-forward.** `hash`/`verify` (`service.go:92-100`) are the package's only
  `HashPassword`/`VerifyPassword` callers, both acquire the four-way semaphore and release via
  `defer` so the slot survives an early error return. All five call sites hash strictly before
  their store write, and none of `store.go`'s three `BeginTx` sites spans a hash.
- **Task 12's `Purger`/`ProviderUsage` structural satisfaction.** Verified against
  `review/service.go:647` and `:665` — identical signatures, and `review`'s import block carries no
  `auth` import, so the direction holds both ways.
- **Tasks 9–12's `identity.Standalone()` placeholders in `internal/api`.** All nine production sites
  now use `scopeFrom(r)` (`providers.go:23`, `repositories.go:42`, `reviews.go:91,105,125,152`,
  `review_files.go:40,67,86`). The only remaining one in the package is `authctx.go:34`, which *is*
  the standalone fallback. `cmd/converge-cli/main.go` is untouched and permanent.

## Deferred minors for the final whole-branch review

These are **in addition to** the lists in `execution-notes.md` and `execution-notes-2.md`.

- Task 14: `s.hashSem <- struct{}{}` (`service.go:92-100`) is an unconditional blocking send with
  no `ctx.Done()` select, so a cancelled caller queued behind four hashes still waits for a slot.
  No test covers throttle lockout on a *nonexistent* username, so oracle-freedom rests on code
  inspection of `Throttle.keys` rather than a regression test. `Logout` (`service.go:238-244`) does
  an extra `Store.LoginSession` read purely to recover `user_id` for its log line.
- Task 15: `sweep.go:12-15` returns early if `DeleteExpiredLoginSessions` errors, so a transient
  session-store error also delays that cycle's lockout cleanup. Consider `errors.Join` to make the
  two housekeeping jobs failure-independent.
- Task 16: `clientIP` reads `X-Forwarded-For` with `Header.Get`, which returns only the first field
  line and does not join values split across repeated header lines — irrelevant under the documented
  single-proxy model, worth a comment if a multi-proxy chain ever appears.
- Task 17: the test-only router wraps `me`/`password`/`DELETE me` in `authenticate` individually
  rather than a shared protected sub-mux (Task 19 replaces it anyway). The two new `classify` tests
  exercise `classify` directly rather than over HTTP, because the delete-account cascade removes the
  row before an HTTP caller could observe `auth.ErrNotFound`.

## A test-suite watch item

Task 18's first implementer reported a "pre-existing `internal/auth` timing flake" in the full
suite. I could **not reproduce it**: `go test -race -count=3 ./internal/auth/` passed (14.6s), and
a full `go test -race -count=1 ./...` across the module was entirely green, as was the fix round's
own run. Recorded as unreproducible rather than pre-existing. **The final whole-branch review should
watch for it** — an intermittent timing test that only fails under full-suite load is still a real
risk even when it passes on demand.

## Process notes

- `SendMessage` remains disabled in this harness, so a fix round **cannot resume a live
  implementer**. Task 18's fix round dispatched a fresh `task-implementer` carrying the brief path,
  the report path, and the findings; the report file is the persistent memory, appended to rather
  than overwritten.
- Briefs for Tasks 16–20 are already extracted in `.superpowers/sdd/plan/`.

## Resuming

`/clear`, then `/execute-task task-005` from this worktree. Next task is **Task 19** (`api` —
scoped existing handlers, mode-conditional routes, healthz), base `7637842`. Task 19's dispatch
must carry the four binding items under "Task 19" above.
