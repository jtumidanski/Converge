# Execution notes — Tasks 19–21

Phase 4 (`/execute-task task-005`, subagent-driven development), session 4. Sessions 1–3 ran
Tasks 1–18 (see `execution-notes.md`, `-2.md`, `-3.md`). Session 4 ran Tasks 19–21 and handed off
at the context threshold, on the boundary where the backend finishes and the frontend screens begin.

The live ledger is `.superpowers/sdd/plan/progress.md` (git-ignored scratch). This file is the
committed summary.

## What landed

| Task | Commit | Result |
| --- | --- | --- |
| 19 — `api` scoped handlers, mode-conditional routes, healthz | `79e62cf` | Review clean, 2 minors |
| 20 — `app`/`cmd` wiring | `b769da1`, fix `fc657d4` | **1 Important**, clean after 1 fix round |
| 21 — frontend client, types, schemas, services, hooks | `ea428f8`, fix `a872dea` | **1 Important**, clean after 1 fix round |

**The entire backend is complete and reviewed (Tasks 1–20), and the frontend data layer is done.**
Standalone equivalence is now proven end to end rather than assumed: `TestStandaloneCreatesNoDatabaseFile`
asserts the database file's *absence* via `os.Stat` returning `os.ErrNotExist`, even with
`CONVERGE_DATABASE_PATH` explicitly set, and checks the default `/data/converge.db` was never touched.

Every commit carries the required `Co-Authored-By` trailer.

## Two Important findings, and what each teaches

### `converge-cli` could be dragged into hosted mode by inherited environment

Task 20 made `app.New` open a database whenever `CONVERGE_MODE=hosted`. But `cmd/converge-cli/main.go:74`
calls `newApp(ctx, os.Environ())` — so a hosted value merely *present* in the CLI's environment dragged
the CLI into hosted mode, opening a database it never uses (its own call sites are permanently
`identity.Standalone()`). Reachable by exec'ing the CLI inside the hosted container.

Corruption was never the risk: `internal/db` uses WAL with `busy_timeout(5000)` and `MaxOpenConns(1)`
per process, and WAL is built for multi-process single-file access. The real harms were that the CLI
would require a `CONVERGE_SECRET_KEY` and DB write access it never needed, and that with
`CONVERGE_DATABASE_PATH` unset it would open the **production** database at the default path.

Fixed in one line — `append(os.Environ(), "CONVERGE_MODE=standalone")` — resting on `config.Load`
building its `vars` map by iterating `env` in order (`config.go:122-127`), so a later duplicate key
wins. The re-reviewer verified that ordering against the source rather than trusting it, because an
inert fix would have looked identical to a correct one.

**The test-design detail worth carrying:** the covering test deliberately supplies a valid
`CONVERGE_SECRET_KEY` so `auth.NewSealer` succeeds and execution actually *reaches* `db.Open`.
Without it the test would pass whether or not the fix were present.

### The global 401 handler could not tell a dead session from a typo

`client.ts` fired the global `onUnauthorized` handler on **any** 401. The reviewer flagged it but
judged the blast radius low, reasoning that a "wrong credentials" 401 only happens on the login page.

**That premise was wrong, and checking it is what changed the severity.** `CodeInvalidCredentials` is
returned from three sites in `internal/auth/service.go`, not one: `:191` (login), `:282`
(`ChangePassword` — "The current password is incorrect."), and `:316` (`DeleteAccount`'s password
confirmation). The latter two are reachable by a fully logged-in user with a valid session who simply
mistypes their current password on the Account Settings screen — who would then be spuriously logged
out with their query cache cleared.

Fixed by discriminating on the error **code** rather than an endpoint allowlist, which would rot as
routes are added: fire on `UNAUTHENTICATED`, never on `INVALID_CREDENTIALS`, and — the fail-safe —
fire on any absent or unrecognised code, since a dead session must never go unnoticed. A non-JSON 401
(an HTML error page from a proxy) yields code `"UNKNOWN"` and correctly fires.

**The lesson, and it is the same one as session 3's:** both findings turned on a *stated assumption*
rather than obviously wrong code, and both were settled by reading the actual error paths instead of
accepting the summary of them.

## Rulings made on the user's behalf

Each is a decision taken so execution could continue. Rework any that are wrong.

1. **A binding carry-forward from Task 16 was built on a false premise, and is corrected.** Sessions
   3's notes asserted that a zero `LoginSessionTTL` emits `MaxAge: 0` meaning "delete immediately" per
   RFC 6265. Verified against the Go stdlib and it is wrong: `$GOROOT/src/net/http/cookie.go:36-38`
   documents `MaxAge=0` as *no* `Max-Age` attribute — an ordinary browser-session cookie — while
   `MaxAge<0` is the delete sentinel. So the real failure mode is a UX regression (users logged out
   when the browser closes), not a silent security or availability failure. Task 16's `Max-Age=0`
   clear-cookie test is unaffected; it asserts the wire output Go produces from `MaxAge=-1`.
   *Cost if wrong:* effort was calibrated down on a hazard that, if it had been real, would have
   deserved a guard. Task 20 wires the value from config regardless.

2. **Task 19's logout requirement is satisfied behaviourally, not structurally.** The standing ruling
   said logout must sit *outside* `authenticate`. It is instead registered on the same mux and
   exempted via `publicRoute` (`authmw.go:24`), so `authenticate` short-circuits without calling
   `Auth.Authenticate`. Accepted: the ruling's intent was the observable behaviour — a 401 must never
   block a logout — and `TestLogoutIsPublicUnderTheRealRouter` fails with a 401 when the exemption is
   mutated away. *Cost if wrong:* nothing shipped; the residual risk is that a future edit to
   `authenticate`'s body could regress logout in a way a structurally-outside wiring could not. The
   mutation-backed test is the guard.

3. **Task 20's plan-text Step 4 check is defective and was left failing.** It prescribes
   `go list -deps ./cmd/converge-cli | grep auth|db` returning no output, which is unsatisfiable given
   Step 3's own prescribed code: the CLI shares `app.New`, and Go's import graph is static, so those
   packages enter the CLI's dependency set the moment `app.go` imports them. Ruled the wrong
   instrument — the property the plan wants is the runtime one (no database opened), which
   `TestStandaloneCreatesNoDatabaseFile` proves more strongly. The reviewer independently ran the check
   and concurred. *Cost if wrong:* if link-time separation was genuinely intended, the CLI binary
   carries the sqlite driver and auth code it never runs — binary size only, no correctness impact.
   Genuine separation would require splitting `app.New` into distinct constructors, out of scope here.

4. **Task 20's Important finding entered a fix round rather than the reviewer's suggested fast-follow.**
   An Important finding enters the loop by rule, and the seam was cheapest to close while Task 20's
   wiring was the live context. *Cost if wrong:* a one-line change plus one test in a file three prior
   tasks deliberately left alone.

5. **I authorised editing `cmd/converge-cli/main.go`, overriding my own standing instruction.** Tasks
   19 and 20 were both told that file must stay untouched. That instruction existed to protect the
   CLI's standalone nature, and the fix *enforces* it rather than regressing it. *Cost if wrong:* the
   file is no longer pristine across the branch; the change is one line plus a test.

6. **Task 21's Important finding was fixed in the seam rather than carried into Task 22's brief**, as
   the reviewer suggested. The defect is in the seam's own contract, and Tasks 22 and 25 both build on
   it and cannot change it cheaply once they do. *Cost if wrong:* Task 22 inherits a slightly different
   `client.ts` contract than its plan text describes — in the safer direction.

7. **Task 21's flat `UserProvider`/`CurrentUser` types stand.** The brief contradicts itself: its
   Interfaces block types them flat while its prose says "Resource-compatible … following `provider.ts`
   style", which wraps. The flat shape is the exact-value contract Tasks 22–25 consume, and `Resource<>`
   is confined to the service layer before flattening — mirroring the brief's own `authService.toUser`
   and the existing `providersService` pattern. *Cost if wrong:* Tasks 22–25 would need an unwrap step
   the hooks currently perform for them.

## Carry-forward for later tasks

- **Task 22** — read the `setUnauthorizedHandler` doc comment in `client.ts:22-39` before writing
  `AuthProvider`. The contract is "the session is gone", **not** "a request returned 401"; the handler
  deliberately does not fire for `INVALID_CREDENTIALS`. Do not re-add a blanket 401 reaction.
- **Task 25** — the Account Settings screen is where wrong-current-password 401s surface. They must
  render as inline form errors, not as a logout.
- **Tasks 22–25** — in standalone mode the `/api/auth/*` and `/api/settings/providers` routes are not
  registered *at all*, so they 404 rather than 401. The mode gate is what keeps the UI from calling
  them; the hooks do not assume those routes exist.
- **Task 28 — now four documentation corrections**, not three: session 3's two (the Task 12 `scopeFrom`
  plan-text error and the Task 14 forward reference to `Service.ListProviders`), plus
  `api-contracts.md:115`'s logout wording, plus Task 20's unsatisfiable Step 4 check (ruling 3). The
  plan's "logout registered OUTSIDE authenticate" wording should also be reconciled with the implemented
  `publicRoute` exemption (ruling 2).
- **Anyone changing the DB pool** — unchanged from earlier sessions: `auth.Store`'s uniqueness checks
  are race-free only because `internal/db` sets `MaxOpenConns(1)`.

## The flake, now upgraded from folklore to a named obligation

Session 3 recorded an intermittent `internal/auth` timing flake under full-suite load as
**unreproducible** after `go test -race -count=3 ./internal/auth/` and a full green module run.

**It has now been sighted a second time, by a different implementer on an unrelated task (Task 21),
again under full-suite load and again passing on an isolated re-run.** Two independent sightings from
two agents materially raises the odds it is real rather than noise. A third run (Task 21's fix round)
was clean.

This must be **either reproduced and fixed, or positively ruled out, before the PR** — not left as
folklore. It belongs to the final whole-branch review and to Task 27 (the hosted integration test).
*Cost if ignored:* an intermittently red suite lands on main and erodes trust in every later run.

## Process notes

- `SendMessage` remains unavailable in this harness (re-confirmed via `ToolSearch` this session), so a
  fix round cannot resume a live implementer. Both fix rounds dispatched a fresh `task-implementer`
  carrying the brief path, the report path, and the findings; the report file is the persistent memory,
  appended to rather than overwritten.
- Both Important findings this session came from checking a *premise* rather than from reading new
  code — one of them a premise the reviewer had already accepted, one a premise this controller had
  itself propagated across two sessions. Cheap to check, expensive to inherit.

## Resuming

`/clear`, then `/execute-task task-005` from this worktree. Next task is **Task 22** (frontend — mode
gate, auth provider, route guard, account menu), base `a872dea`. Task 22's brief must be extracted with
`scripts/task-brief`; briefs through Task 21 are already in the workspace.
