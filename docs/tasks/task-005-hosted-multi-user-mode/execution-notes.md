# Execution notes — Tasks 1–8

Phase 4 (`/execute-task task-005`, subagent-driven development). Session 1 ran Tasks 1–8
and handed off at the plan's own first natural boundary ("after Task 8 — the auth
primitives"), which is also where the controller hit the context-handoff threshold.

The live ledger is `.superpowers/sdd/plan/progress.md` (git-ignored scratch, per-plan
workspace). This file is the committed summary: what landed, what was decided, and what the
next session must carry forward.

## What landed

| Task | Commit | Result |
| --- | --- | --- |
| 1 — `internal/identity` (Scope) | `8d1cfb0` | Review clean |
| 2 — `config` mode + hosted variables | `7868fe6` | Review clean, minors only |
| 3 — `internal/db` (SQLite + migrations) | `e7d1494` | Review clean, minors only |
| 4 — `auth` errors and models | `23fb4bc` | Review clean, minors only |
| 5 — `auth/password.go` (Argon2id) | `4b1b544`, fix `01cf6e2` | Clean after 1 fix round |
| 6 — `auth/crypt.go` (AES-256-GCM) | `6dd7762` | Review clean, minors only |
| 7 — `auth/store.go` (all the SQL) | `0df1f52` | Review clean, minors only |
| 8 — `auth/throttle.go` (doubling lockout) | `f8dc70b`, fix `32f668e` | Clean after 1 fix round |

`make docker-build` was run early (Task 3) as the design asks: exit 0, image 69.6 MB
(19.2 MB compressed), `cmd/converge` binary 11.6 MiB, `CGO_ENABLED=0`. The
`modernc.org/sqlite` size/build-time risk the design names is cleared.

## Pre-flight scan

A full scan of the plan produced 39 interface-pair rows and 29 per-task self-consistency
rows (`.superpowers/sdd/plan/preflight-scan.md`). **No task violates the plan's Global
Constraints** — dependency direction, standalone equivalence, parameterised SQL,
`CGO_ENABLED=0`, linters, error codes/statuses, and environment variables all check out.
Nine rows needed a ruling; they are below.

## Rulings made on the user's behalf

Each is a decision taken so execution could continue. Rework any that are wrong.

1. **`PATCH /api/settings/providers/{id}` with a `slug` key answers 400/`INVALID_REQUEST`**,
   not `api-contracts.md`'s 422/`VALIDATION_ERROR`. Slug immutability is enforced by the
   patch attribute struct having no `Slug` field, so the existing `jsonapi.Decode`
   unknown-attribute path produces the status, consistent with every other endpoint.
   **Task 28 must amend `api-contracts.md` to match.** Cost if wrong: clients expecting 422
   for that one case see 400; one handler + doc edit to reverse.
2. **`auth.NewID` stays 16 hex characters** (deviation #2). A user id becomes a directory
   name under `REPOSITORY_CACHE_ROOT`; a collision merges two users' mirrors.
3. **Task 19's `deleteReview` and `sessionFor` mode branches stand** (deviations #3/#4) —
   both exist for non-disclosure, and standalone equivalence is requirement #1. Pre-ruled so
   reviewers don't re-litigate them as design §7 violations.
4. **`api-contracts.md`'s missing `INVALID_REQUEST`/400 rows** on the register and
   provider-settings error tables are a documentation gap. Fold into Task 28.
5. **Tasks 17/18 ship two `t.Skip`ped tests, un-skipped in Task 19.** Pre-ruled: do not
   delete them, and **Task 19 must un-skip both**.
6. **Task 5's `TestVerifyReadsParamsFromTheString` calls `argon2.IDKey` directly** on
   purpose — `HashPassword` always uses the package constants, so a round-trip could never
   prove verification reads parameters back out of the stored string.
7. **Task 14's timing-equalisation test must not ship as a 3-sample wall-clock assertion.**
   Assert the structural property (an unknown user still runs a dummy Argon2id
   verification); if a timing assertion is kept, use a median over >= 15 samples with a wide
   tolerance.
8. **`ProviderPatch.Validate` defaults to `false`, `ProviderInput.Validate` to `true`**
   (deviation #5, hence `*bool` on create).
9. **Argon2 cost ceilings in `decodePHC`** (Task 5 fix): `memory <= 1<<20` KiB,
   `timeCost <= 16`, `threads <= 16` — 16x/16x/4x the FR-2.3 production values. Without them
   a corrupt stored hash drives a fatal OOM (uncatchable by `recover`) or an unbounded hang
   inside `argon2.IDKey`. Cost if wrong: a future parameter bump above those ceilings needs
   the constants raised in the same commit.
10. **Task 8's plan-mandated lost-update race was fixed, not accepted.** The brief's
    reference `Throttle.Fail` did a read-modify-write across two store calls; with
    `MaxOpenConns(1)` the connection is released between them, so concurrent failures lose
    increments — defeating the brute-force control exactly when it matters. Fixed with
    `Store.UpdateAttempt(ctx, scope, key, fn)`: SELECT + pure-arithmetic policy callback +
    upsert inside one transaction, plus a 20-goroutine test asserting an exact count.

One scan finding needed no ruling: Task 26's `secrets_test.go` assumes
`app.NewLogger(w io.Writer, cfg config.Config) *slog.Logger`, which exists at
`apps/backend/internal/app/app.go:173`.

## Carry-forward for later tasks

- **Task 14** — verify callers hash **outside** a transaction and under the four-way Argon2id
  semaphore (the contract stated in `HashPassword`'s doc comment). Task 5's reviewer could
  not verify it from that diff.
- **Task 15** — confirm `Seal`/`Open` are called with `user_id` **and the provider row id**
  (not some other identifier pair); and confirm provider CRUD can build a `UserProvider`
  through the `NewUserProvider` signature added in Task 7 (`internal/auth/model.go`) without
  needing a second constructor.
- **Anyone changing the DB pool** — `auth.Store` enforces username and provider-slug
  uniqueness with a check-then-insert inside a transaction, which is race-free *only*
  because `internal/db` sets `MaxOpenConns(1)`. Raising the connection limit breaks
  `USERNAME_TAKEN` / `PROVIDER_SLUG_TAKEN`: the raw constraint violation surfaces unmapped.

## Deferred minors for the final whole-branch review

- Task 2: the `loadProviders` doc comment sits above `modeVar`; `loadProviders` has none.
- Task 3: `db.Open` concatenates `CONVERGE_DATABASE_PATH` into a `file:` DSN unescaped.
- Task 4: `User.UsernameFold()` and `NewLoginSession` are unused surface; the `errors.go`
  package doc states the eventual import boundary as current fact.
- Task 5: `TestVerifyRejectsOversizedCostParameters` uses a fixed 200 ms wall-clock timeout
  (4x margin) — latent flakiness on a loaded runner.
- Task 6: `aad()`'s doc comment states its unambiguity unconditionally rather than noting it
  rests on NUL-free ids (both operands are hex from `auth.NewID`, so it holds today); two
  tamper tests assert only `err != nil`; `Seal` validates empty ids but `Open` does not.
- Task 7: `SetPasswordHash`, `TouchLoginSession`, `DeleteLoginSession` and
  `DeleteOtherLoginSessions` ignore `RowsAffected` and silently no-op on a missing row; no
  test covers `DeleteUser` / `UpdateUserProvider` returning `ErrNotFound`; `store.go` is
  407 lines over four tables and will grow.
- Task 8: `Sweep`'s "just expired" boundary and a backwards-moving clock are untested.

## Resuming

`/clear`, then `/execute-task task-005` from this worktree. Next task is **Task 9**
(`provider.Resolver` and the mechanical rewire), base `32f668e`. Tasks 9–12 are the
signature churn the design sequences early — do not reorder them.
