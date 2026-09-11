# Execution notes — Tasks 9–13

Phase 4 (`/execute-task task-005`, subagent-driven development), session 2. Session 1 ran
Tasks 1–8 (see `execution-notes.md`). Session 2 ran Tasks 9–13 and handed off at the
context threshold, on the boundary where the plan's early signature churn finishes.

The live ledger is `.superpowers/sdd/plan/progress.md` (git-ignored scratch). This file is
the committed summary.

## What landed

| Task | Commit | Result |
| --- | --- | --- |
| 9 — `provider.Resolver` + mechanical rewire | `461ea8b` | Review clean, 1 minor |
| 10 — `session` owner field and scoped store | `efc8e3d`, fix `799f3a3` | Clean after 1 fix round |
| 11 — `mirror` per-user namespaces | `0494dc6` | Review clean, minors only |
| 12 — `review` scope threading | `e145057`, `89a50a7` | Review clean, minors only |
| 13 — `auth` verifier + per-user provider resolver | `7339b2c` | Review clean, no minors |

**Tasks 9–12 are the signature churn the design sequences early (risk #1, §12). That block
is now complete and `review`'s signatures are stable** — every later task builds on them.
Task 13 then supplied the hosted `provider.Resolver`.

Every commit carries the required `Co-Authored-By` trailer (verified with
`git log --format='%(trailers:key=Co-Authored-By,valueonly)'`).

## Rulings made on the user's behalf

Each is a decision taken so execution could continue. Rework any that are wrong.

1. **Task 10's `Purge` cleanup-failure race was fixed, not accepted.** The reviewer found
   `Store.Purge` deleted `index[id]` *and* `pendingCleanup[id]` up front and, when
   `Cleaner.Cleanup` failed, only appended to `errs` — never re-registering the id. `Sweep`'s
   retry loop needs the id in **both** maps, so a transient cleanup failure during account
   deletion stranded the deleted user's `session.json` on disk until a restart let `LoadAll`
   rediscover it (bears on FR-2.7). The brief's own reference `Purge` had this shape, so this
   overrides the plan. Fixed by routing a failed purge through the existing
   `persistCleanupFailure` path that `Finish`/`expire` already use; `sweep.go` untouched.
   Cost if wrong: one extra branch plus one test, and a failed purge lingers in the in-memory
   index until `Sweep` succeeds rather than vanishing immediately while its files remain.

2. **Correction to that ruling's stated rationale (mechanism, not decision).** I justified
   retaining the index entry by claiming no live `identity.Scope` could match a deleted user
   id. That is **false at the store layer** — `Scope.Matches` is a plain string compare, so
   `Get(id, identity.ForUser(deletedID))` would return it. The decision stands, but the real
   guarantee is one layer up: the auth layer never reissues a deleted user's scope, because
   the user row and its login sessions are gone. **Tasks 14–16 must not rely on a store-level
   ownership check that does not exist.**

3. **Task 11's brief self-conflict resolved in favour of the reference code.** The brief's
   rejection table lists `""` as a user id that must return an error, while the brief's own
   `NamespaceFor` reference code treats an empty id as the standalone scope. The table row is
   the defect: `identity.Scope` holds one unexported `userID`, `Standalone()` returns the zero
   value, so `ForUser("")` is bit-identical to `Standalone()`. Erroring would make
   `NamespaceFor(identity.Standalone())` fail and break standalone equivalence (requirement
   #1, FR-6.5). The six non-empty malformed ids still reject. Cost if wrong: nothing
   security-relevant — an empty id cannot arrive from a verified login session, since
   `ForUser` is only called by the auth middleware with a session's user id.

4. **Task 13's unscoped-scope behaviour stands as implemented.** `ProviderResolver.Resolve`
   returns a plain `errors.New` (not a `Code`-bearing `*auth.Error`) when
   `scope.UserID() == ""`. A plain error is right because the closed `Code` enum has no
   "caller misconfigured itself" value and this is a wiring bug unreachable from an
   authenticated request. It never falls through to another user's cached registry.

## Plan-text error to fix

**`task-12-brief.md` Step 4 (plan.md's Task 12 section) says "Task 14 replaces the API's
placeholder with `scopeFrom(r)`". It is Task 16** ("`api` — scope context, cookies, and the
two middlewares", plan.md:4553) that owns `scopeFrom`. Task 14 is `auth/service.go`, the
account lifecycle. Harmless in shipped code — the committed comments in `reviews.go`,
`review_files.go` and `main.go` cite no task number. **Fold the plan/doc correction into
Task 28.**

## Carry-forward for later tasks

- **Task 14** — declares `auth.Purger` and `auth.ProviderUsage`. Task 12 wrote
  `review.Service.PurgeUser` and `ProviderInUse` to satisfy them *structurally*; `review`
  cannot import `auth` to assert it, so **Task 14's review must confirm the satisfaction
  holds**. Also still open from Task 5: verify callers hash **outside** a transaction and
  under the four-way Argon2id semaphore.
- **Task 16** — owns the `identity.Standalone()` placeholders left in `internal/api/*.go`.
  `cmd/converge-cli/main.go`'s `Standalone()` call is **permanent** (the CLI is standalone by
  nature), not a placeholder.
- **Task 20** — the hosted wiring is where standalone equivalence actually becomes
  provable. Task 13 added three unwired files, so its review could not verify it.
- **Anyone changing the DB pool** — unchanged from session 1: `auth.Store`'s uniqueness
  checks are race-free only because `internal/db` sets `MaxOpenConns(1)`.

### Discharged

Tasks 6 and 7 both left a note that a later task must confirm `Seal`/`Open` are called with
`user_id` **and the provider row id**. **Discharged in Task 13:** `resolver.go:104` calls
`r.sealer.Open(row.TokenCiphertext(), row.TokenNonce(), row.UserID(), row.ID())`, matching
`Sealer.Open(ciphertext, nonce, userID, providerID)` at `crypt.go:78`; `row.ID()` is the
row's own `auth.NewID()`, not the slug. Provider rows also round-trip through the existing
`NewUserProvider` with no second constructor — Task 7's other note discharged too.

## Deferred minors for the final whole-branch review

These are **in addition to** the list in `execution-notes.md`.

- Task 9: `service.go` duplicated the literal `"review: resolve providers: %w"` in two
  near-identical resolve-check-wrap blocks.
- Task 10: `TestPurgeRemovesEveryOwnedSession` covered only the success path (the failure
  path got its own test in the fix round).
- Task 11: `NamespaceRoot` is exported but absent from the brief's Produces list (it *is* in
  the brief's reference code); `NamespaceFor`'s "invalid user id" error omits the rejected id
  unlike `Path`'s `%q` sibling — a defensible anti-log-injection choice that deserves a
  one-line comment so nobody "fixes" the asymmetry.
- Task 12: mutation coverage for five of the seven new tests rests on code inspection rather
  than an executed scratch mutation run. The reviewer independently traced each through
  `Scope.Matches` / `NamespaceFor` / `PurgeNamespace` / `store.List`'s `IsActive` filter and
  judged each would genuinely fail under the named regression, but `PurgeUser` and the
  `build`/`scopeOf` path deserve a spot-check.

## Process note

This harness has `SendMessage` disabled, so a fix round **cannot resume a live implementer**.
Every fix round dispatches a fresh `task-implementer` carrying the brief path, the report
path, and the findings; the report file is the persistent memory. Task 10's fix round worked
this way.

## Resuming

`/clear`, then `/execute-task task-005` from this worktree. Next task is **Task 14**
(`auth/service.go` — the account lifecycle), base `7339b2c`. It is the largest brief so far
(413 lines). Briefs for Tasks 10–15 are already extracted in
`.superpowers/sdd/plan/`.
