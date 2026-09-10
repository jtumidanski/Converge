# Backend Audit — Task 15 (review service orchestration + workspace cleaner adapter)

- **Scope:** `apps/backend/internal/review/{cleaner.go,service.go,service_test.go}` (706 added lines, 3 new files, nothing else touched)
- **Range:** `72549d3..bc2fd0a`
- **Date:** 2026-09-04
- **Build:** PASS (`CGO_ENABLED=0 go build ./...`)
- **Vet:** PASS (`go vet ./...`)
- **Tests:** PASS (`go test -race -count=1 ./internal/review/ ./internal/session/` → ok 4.057s / 1.184s)
- **Spec-compliance verdict:** ✅ PASS with deviations (all mandated symbols/signatures present; 4 brief tests verbatim; 2 deviations beyond the report's own list)
- **Guidelines verdict (DOM-*/SUB-*/SEC-*):** ❌ NEEDS-WORK — 1 Critical, 4 Important

Counts: **Critical 1, Important 4, Minor 4.**

---

## 1. Spec-compliance verdict against the task brief

| Brief requirement | Status | Evidence |
|---|---|---|
| `type Cleaner struct{...}` | ✅ | `internal/review/cleaner.go:15` |
| `NewCleaner(*mirror.Cache, *workspace.Manager, *slog.Logger) *Cleaner` | ✅ | `cleaner.go:23` |
| `Cleanup(ctx, session.Session) error`, mirror path best-effort/empty when unresolvable | ✅ | `cleaner.go:39-51` (`mirrorPath = ""` at :45) |
| `RemoveDir(ctx, id) error` → `workspace.RemoveDir` | ✅ | `cleaner.go:54-59` |
| `var _ session.Cleaner = (*Cleaner)(nil)` **in cleaner.go only** | ✅ | `cleaner.go:61`; absent from `service.go` (grep: 0 matches) |
| `Deps` struct, exact field set | ✅ | `service.go:35-46` — all 10 fields, exact names/types |
| `NewService(Deps) *Service` | ✅ | `service.go:56` |
| `Create(ctx, CreateInput) (session.Session, error)`, validates, resolves default branch, persists CREATING, **does not build** | ✅ | `service.go:91-143`; no build call in body |
| `StartBuild(ctx, id)` semaphore-bounded goroutine | ✅ | `service.go:152-160` |
| `Build(ctx, id) session.Session` synchronous pipeline | ✅ | `service.go:170` (named return `final`, same external signature) |
| `Get`/`List`/`Files`/`FileDiff`/`CombinedDiffPath`/`Finish` | ✅ | `service.go:76, 79, 400, 412, 433, 82` |
| `var ErrNotReady` | ✅ | `service.go:22` |
| `service.go` must not import `workspace` unless used | ✅ | used for the `Deps.Workspaces` field, `service.go:18/38` |
| `Applicator.Apply` 4-arg form (controller note 1) | ✅ | `service.go:308` matches `apply.go:38` |
| The four brief tests, verbatim | ✅ | `service_test.go:77,131,149,168` — byte-for-byte the brief's bodies |
| No extra exported surface (YAGNI) | ✅ | new exported symbols are exactly `Cleaner`, `NewCleaner`, `Deps`, `Service`, `NewService`, `ErrNotReady`, `CombinedDiffFile` — all named by the brief |
| Error codes stay in the exact-string set | ✅ | new files use only `CodeGitFailure`, `CodeProviderUnavailable`, `CodeRepositoryUnavailable`, `CodeConflict` + the four `INVALID_*`; no `INTERNAL` anywhere (`codes.go` grep: 0) |

**Deviations beyond the 12 the report lists** (both behavioural additions, not omissions):

- D-A: `Build` refuses a non-`CREATING` session (`service.go:176-184`). The report calls this out as #5; the brief does not specify it. Consequence in Minor M2.
- D-B: `defaultMaxConcurrentBuilds = 4` extracted as a named constant (`service.go:32`) where the brief inlined `4`. Cosmetic; no objection.

The report's corrections #1 (`save` swallowing `Store.Save`), #2 (panic recovery discarding the failed session), #8 (`trimSHA` returning `""`) were each **independently verified as real and complete**:

- #1: `persist` returns the error (`service.go:215-223`); the READY write is `persist` and aborts the build (`service.go:375-377`); only stage markers use `progress` (`service.go:230-233`).
- #2: named return `final` is assigned in the recover block (`service.go:170, 188`), routed through `finishWithError`, so the panicking build returns the FAILED session, not a zero one. Recorded as `GIT_FAILURE` per **R27** (`service.go:189`) — correct.
- #3/#8: `trimSHA` is gone; `Service.head` trims **and** runs `gitx.ValidateSHA`, so a blank/short HEAD becomes `GIT_FAILURE` instead of an empty diff endpoint (`service.go:383-397`). Correct.

**R28** is implemented as ruled: `session.ErrNotFound` wrapped with `%w` at `service.go:403, 415, 429, 436, 443`.

---

## 2. PRIORITY 1 — enumeration of every pipeline step and error-return site

Legend: **AS ITSELF** = the distinct failure reaches the session's terminal status with its own code; **FLATTENED** = collapsed into a generic code; **BENIGN** = a plausible zero value reaches the caller with no error.

| # | Step / site | Line | Failure → outcome | Verdict |
|---|---|---|---|---|
| 1 | `Store.Get` unknown id | 171-175 | zero `Session`, status `""`, logged Error, **no error return** | BENIGN (bounded — see M1; signature is brief-mandated) |
| 2 | non-`CREATING` guard | 176-184 | returns the stored session unchanged | AS ITSELF (but see M2) |
| 3 | panic recovery | 185-193 | `GIT_FAILURE` + FAILED, assigned to named return | AS ITSELF (R27) |
| 4 | unclassified `build` error | 196-205 | real cause logged at Error, client gets `GIT_FAILURE` | AS ITSELF (R27) |
| 5 | provider not registered | 265-271 | `PROVIDER_UNAVAILABLE` | AS ITSELF |
| 6 | `GetRepository` | 272-281 | `MapProviderError` → `PROVIDER_AUTH`/`REPOSITORY_UNAVAILABLE`/`PROVIDER_UNAVAILABLE`, else `REPOSITORY_UNAVAILABLE` | AS ITSELF |
| 7 | `Resolver.Resolve` | 285-288 | returned raw; every `Resolve` return is a `*session.ReviewError` (`resolve.go:77-184`); `asReviewError` (`resolve.go:50`) preserves it | AS ITSELF (`NOT_MERGED` proven by `service_test.go:160`) |
| 8 | `WithResolved` persist | 289 | swallowed by `progress` | acceptable (stage-only) — but see I1 |
| 9 | `WithBase` | 290-293 | wrapped plain error → `GIT_FAILURE`, cause logged | AS ITSELF (no code exists for it) |
| 10 | `Workspaces.Create` | 297-303 | `GIT_FAILURE`, cause logged | AS ITSELF |
| 11 | `Applicator.Apply` error | 308-317 | `GIT_FAILURE` + `Change`, cause logged | AS ITSELF |
| 12 | `OutcomeConflict` | 319-335 | `CONFLICT` + commit + conflicting paths + appliedChanges + possibleDependency + diagnostics | AS ITSELF |
| 13 | `OutcomeApplied`/`OutcomeEmpty` | 336-339 | recorded as applied; matches `apply.go:100-104` (`--empty=keep` still creates a commit) | correct |
| 14 | unrecognised outcome | 340-343 | error, not a silent "applied" | AS ITSELF |
| 15 | `rev-parse HEAD` | 348-354, 383-397 | blank/short HEAD rejected by `ValidateSHA` → `GIT_FAILURE` | AS ITSELF (fix #8 verified) |
| 16 | `diff.WriteCombined` | 356-361 | `GIT_FAILURE` | AS ITSELF |
| 17 | `diff.Summarize` | 362-368 | `GIT_FAILURE` | AS ITSELF |
| 18 | `Session.Ready` | 369-372 | wrapped → `GIT_FAILURE` | AS ITSELF |
| 19 | `persist(ready)` | 375-377 | build aborts into the failure path; **no READY returned on a failed write** | AS ITSELF (fix #1 verified) |
| 20 | `persist(terminal)` in `finishWithError` | 257 | swallowed, but the true terminal value is still returned and the failure logged | acceptable |
| 21 | **ctx cancelled by shutdown** | 152-158, anywhere in `build` | git dies → `GIT_FAILURE`/FAILED persisted | **FLATTENED — see I4** |
| 22 | terminal record content | 188, 206, 238 | built from the pre-build snapshot, so `baseSha`/`resolvedChanges`/`stage` are rolled back to zero | **BENIGN — see I1** |
| 23 | READY vs. a concurrent `Finish` | 171 + 375 | stale snapshot lets READY overwrite FINISHED | **BENIGN — see C1** |

Sites 1–20 are correct. Sites 21–23 are the findings below.

---

## 3. Findings

### C1 (Critical) — a build in flight resurrects a FINISHED session as READY, and re-creates its deleted workspace directory

`Build` snapshots the session once (`service.go:171`) and every subsequent transition is computed from that local copy. `Session.Ready`'s terminal guard (`session/model.go:210`) therefore inspects the **stale** copy, which is still `CREATING`, and `Store.Save` writes unconditionally (`session/store.go:92-95`) and re-creates the directory with `MkdirAll` (`store.go:65`).

`DELETE /api/reviews/{id}` is explicitly allowed on a `CREATING` session (prd.md:322 — idempotent, no state restriction; the UI offers "Discard Review"), so this is reachable in normal use.

Reproduced (probe in a throwaway copy at `/tmp/bk15`, using the injectable `Deps.Now` to land `Finish` between `diff.WriteCombined` and `persist(ready)`):

```
fired=true returned=READY stored=READY
session dir RECREATED after Finish: .../417b8e46
CombinedDiffPath => "" err=session: not found: combined diff missing for session 417b8e46
```

Consequences: `GET /api/reviews/{id}` reports **READY** with a full file list for a review whose worktree was deleted; every file endpoint then fails; `List` shows the session as active for the rest of its 24 h TTL; the workspace directory survives a successful cleanup. This also defeats the store's deliberate R18/R19 index-first ordering (`store.go:141-170`) — that ordering only protects readers, not a writer holding a stale value.

An earlier-landing `Finish` produces the same overwrite with FAILED (second probe: `returned=FAILED stored=FAILED`).

**Fix direction:** the service must not write a value derived from a stale snapshot. Either re-read the session from the store immediately before each terminal write and abort if `!IsActive()`, or (correct but larger) add a compare-and-swap `Save` to `session.Store` that rejects a write whose stored predecessor is terminal. Note the narrow re-read is still racy; only the store-level guard closes it.

### I1 (Important) — the terminal failure record rolls back base SHA, resolved changes and stage that were already persisted

`Build` passes the pre-build snapshot `sess` into `finishWithError` (`service.go:188, 206`), while the pipeline's accumulated state lives in `build`'s local `current` (`service.go:283-294`) and is discarded by every `return session.Session{}, err`. The terminal `Save` then overwrites the intermediate `session.json` that resolve already wrote.

Reproduced with a stub `ChangeApplicator` returning `OutcomeConflict`:

```
status=CONFLICTED baseSHA="" resolved=0 stage=""
stored:  status=CONFLICTED baseSHA="" resolved=0
```

This violates FR-8.3 (prd.md:313-317 — `session.json` must contain base SHA and resolved changes) and empties the `baseSha` / `included` attributes of `GET /api/reviews/{id}` (prd.md:545-551) on exactly the CONFLICTED/FAILED path where the UI renders the header alongside `ReviewErrorPanel` (design.md:681). Plausible zero values (`""`, `[]`) on a failure path — the Priority-1 defect class.

**Fix direction:** thread the live session out of `build` (e.g. return `(session.Session, error)` with the latest `current` even on error, or hold `current` on the `Service` call frame) and mark **that** value Failed/Conflicted.

### I2 (Important) — `TestServiceStartBuildIsAsynchronous` does not discriminate the behaviour it names

Proved by mutation: replacing `StartBuild`'s body with a direct, fully synchronous `s.Build(buildCtx, id)` (no goroutine) leaves the test green:

```
=== RUN   TestServiceStartBuildIsAsynchronous
--- PASS: TestServiceStartBuildIsAsynchronous (0.18s)
```

The test only asserts "StartBuild eventually yields READY". Asynchrony is untested, and the semaphore bound is never exercised — `MaxConcurrentBuilds: 2` (`service_test.go:72`) is set but no test ever runs two builds concurrently, so neither the bound nor the release-on-every-path property has coverage. (The test body is the brief's verbatim, so this is inherited, not invented — it still needs fixing.)

**Fix direction:** assert `StartBuild` returns before the session leaves `CREATING` (block the build with a gating `ChangeApplicator`, assert `Get` is still `CREATING` right after the call, then release), and add a bounded-concurrency test that counts concurrent `Apply` entries and asserts the peak never exceeds `MaxConcurrentBuilds`.

### I3 (Important) — the error paths are untested, and the stated blocker (concrete-typed `Deps`) is not the real blocker

Report concern 8 claims panic recovery, `Store.Save` failures, unknown/non-`CREATING` `Build`, and the cleaner's unresolvable-mirror branch cannot be tested without changing `Deps`. That is inaccurate for most of them:

- `Deps.Applicator` is the **interface** `ChangeApplicator` (`apply.go:37`). A stub applicator injects apply errors, `OutcomeConflict`, an unknown `Outcome`, and a `panic()` — all four paths, no signature change. I used exactly this for I1.
- `Deps.Runner` is the **interface** `gitx.Runner` (`gitx/spec.go:64`) and `gitx.FakeRunner` already exists (`gitx/fake.go:12`). Wrapping it covers the `rev-parse HEAD` / `WriteCombined` / `Summarize` `GIT_FAILURE` branches and the blank-HEAD `ValidateSHA` guard.
- `Deps.Now` is a `func() time.Time` (`service.go:45`) — a per-call hook. I used it for C1's interleaving probe.
- `Build` on an unknown id and on a non-`CREATING` id need **no** injection: call `Build(ctx, "deadbeef")`, and call `Build` twice on the same id.
- `Cleaner` needs no `Deps` at all: `NewCleaner(mirrors, ws, log).Cleanup(ctx, session.Session{})` drives the unresolvable-path branch directly (`mirror/cache.go:34` rejects the empty provider id). There is currently **no `cleaner_test.go` at all** — `Cleaner` has zero direct tests.
- Only `Store.Save` failure genuinely needs an OS trick rather than an interface: `chmod 0500` the workspace root (or the session dir) after `Create` and before the READY write. That is 3 lines, not a signature change.

Violates testing-guide.md:249 ("Not Testing Error Paths"). **Recommendation: do not widen `Deps` to interfaces.** Ask for tests using the injection points that already exist, plus a `cleaner_test.go`. If C1/I1 are fixed, their regression tests fall out of the same mechanism.

### I4 (Important) — a build cancelled by server shutdown is recorded as `GIT_FAILURE`, not `INTERRUPTED`

Nothing in `build` distinguishes `ctx.Err() != nil` from a git failure: the killed git process produces an error that lands at `service.go:302/315/353/360/367` as `GIT_FAILURE`, and `finishWithError` persists FAILED. design.md §8.3 states shutdown cancels the lifetime context, "in-flight builds fail fast, and the next startup's `LoadAll` marks anything still `CREATING` as `INTERRUPTED`" — but after this code writes FAILED the session is **no longer `CREATING`**, so `LoadAll` (`session/store.go:303-308`, FR-8.6) never applies `INTERRUPTED`. A distinct failure is flattened into a generic one, and the user is told "A git command failed" (`messages.go:84`) for a routine restart. Note `MsgInterrupted()` (`messages.go:88`) exists and is currently unused outside the store.

**Fix direction:** before classifying, check `ctx.Err()`; on `context.Canceled`/`DeadlineExceeded` emit `session.CodeInterrupted` with `MsgInterrupted()`.

### M1 (Minor) — `Build` on an unknown id returns a zero session with `Status() == ""`

`service.go:172-175`. Forced by the brief-mandated signature and documented at `service.go:162-169`, but `""` is not one of the six session states (plan.md:26) and the CLI exit-code mapping (design.md:556) enumerates only READY/CONFLICTED/FAILED. Task 16/19 must handle `""` explicitly.

### M2 (Minor) — the non-`CREATING` guard silently no-ops

`service.go:176-184` returns the stored session with only a Warn log. A CLI operator re-running `build` on a FAILED session gets the old FAILED session back with no signal that nothing ran. Behaviour is not in the brief (deviation D-A).

### M3 (Minor) — `StartBuild`'s semaphore acquire ignores `ctx.Done()`

`service.go:154` blocks on `s.sem <- struct{}{}` with no `select` on `ctx.Done()`. On shutdown, queued goroutines still wait for a slot and then run a build that immediately fails (compounding I4). No leak — the release is deferred at `:155` and runs on every exit path including an escaping panic — but the queue should drain on cancellation.

### M4 (Minor) — tests are not table-driven

`service_test.go:77-190` uses four separate functions with inline assertions rather than the `tests := []struct{...}` + `t.Run` pattern (DOM-19 / testing-guide.md). Inherited verbatim from the brief.

---

## 4. PRIORITY 2 — concurrency

| Check | Result | Evidence |
|---|---|---|
| Semaphore released on every exit path | PASS | acquire `service.go:154`, `defer func(){ <-s.sem }()` `:155` — runs on normal return, on the recovered panic (recovered inside `Build`, `:186`) and during unwinding of an escaping panic |
| Semaphore acquire bounded/cancellable | WARN | M3 — no `ctx.Done()` arm |
| Race between build goroutine and `Get`/`List`/`Finish` | **FAIL** | C1 — the store's mutex protects the map, but the service holds a stale `Session` value across the whole build and writes it back unconditionally |
| Session values immutable / defensively copied | PASS | `session/model.go:117-130` copy slices; `ReviewError.clone` is a deep copy (`session/errors.go:31-43`) |
| `ReviewError` shared between the stored session and the returned value | PASS | `Failed`/`Conflicted` clone before storing (`model.go:236, 248`) |
| R18/R19 ordering in `session/store.go` disturbed? | **PASS — untouched** | `git diff --stat 72549d3..bc2fd0a` lists only the three new `internal/review` files; `store.go:155-170` (index before `Cleanup`), `:199-208` (`pendingCleanup` + persist only on the failure path), `:217-237` (bounded retry) are byte-identical |
| `-race` clean | PASS | `go test -race -count=1 ./internal/review/ ./internal/session/` → ok |

Caveat on the last row: `-race` is clean because C1 is a **logical** lost-update, not a data race — the store's mutex serialises both writes correctly. The race detector cannot find this class; only the probe test can.

---

## 5. PRIORITY 3 — recommendation

**The concrete-typed `Deps` is not the blocker.** See I3 for the per-path breakdown. Recommended, in order:

1. Fix C1 and I1, with regression tests built on `Deps.Now` + a stub `ChangeApplicator` (both already injectable — the probes in this audit are directly reusable).
2. Add error-path tests for panic recovery, apply error, unknown outcome, and the `GIT_FAILURE` diff branches via a stub `ChangeApplicator` / wrapped `gitx.Runner`.
3. Add `Build(ctx, "deadbeef")` and double-`Build` tests — zero infrastructure.
4. Add `cleaner_test.go` covering `Cleanup` (happy, idempotent-repeat, zero-value session for the unresolvable-mirror branch) and `RemoveDir`.
5. `Store.Save`-failure: `chmod 0500` on the session dir mid-build; no interface needed.
6. Replace `TestServiceStartBuildIsAsynchronous` with a version that actually discriminates (I2), and add a concurrency-bound test.

**Do not widen `Deps` to interfaces.** It would add a mock surface the testing guide then requires be kept in sync (testing-guide.md:158-201) for no coverage that is not already reachable.

---

## 6. DOM-* / SUB-* / SEC-* checklist

`internal/review` has no `model.go` and no `resource.go`; per the documented package-layout deviation (plan.md, Global Constraints) the session model lives in `internal/session` and there is no GORM/JSON:API layer in this package. It classifies as a **support/orchestration package**, so DOM-01…DOM-18 and SUB-01…SUB-04 are N/A. Verified, not assumed:

| ID | Check | Status | Evidence |
|---|---|---|---|
| DOM-01…05 | builder/`ToEntity`/`Make`/`Transform`/`TransformSlice` | N/A | no `model.go`, `entity.go` or `rest.go` in `internal/review` (`ls`) |
| DOM-06/07 | logger injection, no global logger | PASS | `*slog.Logger` via `Deps.Log` (`service.go:42`) and `NewCleaner` (`cleaner.go:23`); nil defaults to `slog.DiscardHandler` (`service.go:63`, `cleaner.go:25`), never `slog.Default()` — grep for `slog.Default`: 0 matches |
| DOM-08/17/18 | transport concerns | N/A | no HTTP in this package (Task 19) |
| DOM-09 | discarded errors | PASS | the only `_ =` on an error are `progress` (`service.go:231`) and the terminal persist (`:257`), both preceded by an Error-level log inside `persist` (`:217`) and both deliberate; no `_, _ :=` |
| DOM-10 | lazy providers | N/A | documented deviation: plain functions, no `model.Provider[T]` |
| DOM-11 | no `os.Getenv` in this layer | PASS | grep over both new files: 0 matches |
| DOM-12/13 | layering | PASS | `Service` orchestrates; `Cleaner` delegates to `workspace.Manager`; no package reaches into another's internals |
| DOM-14/15 | writes go through one owner | PASS | all persistence via `Store.Save` through `persist` (`service.go:215`); no direct file writes except `diff.WriteCombined` into the session dir |
| DOM-16 | domain error → status mapping | N/A here | `ReviewError.Code` carried for Task 19; `ErrNotReady`/`session.ErrNotFound` wrapped with `%w` (`service.go:403-443`) per R28 |
| DOM-19 | table-driven tests | WARN | M4 — `service_test.go:77-190` |
| SUB-01…04 | sub-domain checks | N/A | no `resource.go` in `internal/review` |
| SEC-01/02 | token validation | N/A | no JWT/session-token handling in this task |
| SEC-03 | open redirect | N/A | no redirects |
| SEC-04 | secrets never leaked | PASS | grep for `token|Token|secret|password|CloneURL` in `service.go` + `cleaner.go`: **0 matches**. Logged fields are session id, provider id, repository full name, change number, stage, status, file count. `gitx.ExitError.Error()` is `"git <category> exited with code <n>"` (`gitx/spec.go:50-52`) — never stderr; `gitx/exec.go` wraps only `"git %s: %w"`. `messages.go:23-89` embeds no git output. `Diagnostics` carries workspace path, branch, strategy, source SHA only (`session/errors.go:9-14`). Error codes used are all inside the exact-string set; `INTERNAL` appears nowhere (`session/codes.go`). |

---

## 7. Blocking / non-blocking

### Blocking (Critical + Important — fix loop)
- **C1** `service.go:171` + `:375` — a concurrent `Finish` is overwritten; session resurrects as READY (or FAILED) with a deleted workspace, and `Store.Save` re-creates the directory.
- **I1** `service.go:188, 206` — terminal failure record built from the pre-build snapshot; CONFLICTED/FAILED sessions persist `baseSha=""` and `resolvedChanges=[]`, contradicting FR-8.3.
- **I2** `service_test.go:168` — the asynchrony test passes against a fully synchronous `StartBuild`; the semaphore bound is untested.
- **I3** `service_test.go` (whole file) + missing `cleaner_test.go` — every error path is untested, and the injection points to test them already exist.
- **I4** `service.go:302/315/353/360/367` — shutdown cancellation is recorded as `GIT_FAILURE`, permanently preventing the `INTERRUPTED` path of FR-8.6/design.md §8.3.

### Non-blocking
- **M1** `service.go:172-175` — zero session with `Status() == ""` for an unknown id.
- **M2** `service.go:176-184` — silent no-op on a non-`CREATING` rebuild.
- **M3** `service.go:154` — semaphore acquire ignores `ctx.Done()`.
- **M4** `service_test.go:77-190` — tests not table-driven.

## 8. Reproduction

Probes were run against a throwaway copy of `apps/backend` at `/tmp/bk15` (the worktree was not modified). Three probes: (1) `StartBuild` made synchronous → I2; (2) stub `ChangeApplicator` returning `OutcomeConflict` → I1; (3) `Deps.Now` hook firing `Finish` once `combined.diff` exists → C1.
