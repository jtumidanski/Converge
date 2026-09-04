# Audit — Task 13: Resolve pipeline (base selection)

Scope: `apps/backend/internal/review/resolve.go`, `resolve_test.go` (diff `ffb360f..18aac75`), against design §6.1, PRD FR-5.x, and the task-13 brief.

## Step-ordering deviation — verdict

**A test genuinely requires part of the reversal, but the implementer reordered more than the test required, and the extra reordering has a real, avoidable cost.**

Confirmed from the diff itself: the brief's own illustrative `resolve.go` (task-13-brief.md:251-280) calls `checkMergedAndTargets` **before** `mirrors.Ensure`/`BranchExists` — i.e. the illustrative reference code matches design §6.1's stated order (step 2 before step 4). The implementer's report says they ran this reference code verbatim and it failed `TestResolveRejectsUnmergedAndBadTargets`'s "missing branch" sub-case (task-13-brief.md:147-150): `numbers=[1]`, `baseBranch="nonexistent-branch"`, change #1 targets `"main"`. Under design order, `checkMergedAndTargets` sees `"main" != "nonexistent-branch"` and returns `INCOMPATIBLE_TARGETS` before `BranchExists` is ever consulted — but the test asserts `CodeBaseUndetermined`. This is real: the brief's own test contradicts the brief's own reference implementation and design §6.1's literal order. I verified this by tracing the fixture and test data directly (task-13-brief.md:129-156) — no misreading; case 3 is a single-change scenario, not a combined case, and it unambiguously requires `BranchExists` to run and fail before the target-mismatch comparison is reached.

However, the fix implemented (`resolve.go:96-125` in the diff, i.e. the whole `Ensure`+`BranchExists` block moved ahead of `checkMergedAndTargets`) moved **both** the `NOT_MERGED` check and the `INCOMPATIBLE_TARGETS` check after the mirror-fetch/branch-check, not just the target-mismatch check that the failing test actually exercises. Checking every subtest in `TestResolveRejectsUnmergedAndBadTargets` (diff lines 394-421): subtests 1 and 2 use `baseBranch="main"`, which exists in the fixture, so `BranchExists` trivially succeeds regardless of ordering — nothing in the brief's tests requires `NOT_MERGED` to run after the mirror fetch. A narrower fix — keep the `NOT_MERGED` check (pure provider-data comparison, zero cost) ahead of `Ensure`/`BranchExists`, and move only the target-mismatch check after it — would satisfy all four subtests and preserve the design's apparent intent: reject an unmerged/wrong-state selection using data the provider already returned, without first paying for a mirror clone/fetch (`GIT_CLONE_TIMEOUT_MINUTES` default 10 min per the audit brief). As shipped, every request — including one that trivially names an open PR — now pays for `Ensure` (clone-or-fetch) before `NOT_MERGED` is ever checked.

**Verdict:** the target-check/branch-exists reordering is plan-mandated (test-mandated) and should stand. The `NOT_MERGED`-check reordering is **not** mandated by any brief test and is an avoidable regression against design intent. Rate **Important** (not Critical — no wrong output is produced, only unnecessary latency/cost on the reject-fast path for the common "selected an unmerged PR" case).

## Job A — Spec compliance

- ✅ `Resolved{Changes []session.ResolvedChange; BaseSHA string; MirrorPath string}` — resolve.go:36-40 (diff), exact match.
- ✅ `Resolver{mirrors *mirror.Cache; log *slog.Logger}` + `NewResolver(m *mirror.Cache, log *slog.Logger) *Resolver` — resolve.go:43-49.
- ✅ `func (r *Resolver) Resolve(ctx, p provider.GitProvider, repo provider.Repository, baseBranch string, numbers []int, progress func(stage string)) (Resolved, error)` — resolve.go:82, exact signature match.
- ✅ `const resolveConcurrency = 4` — resolve.go:33.
- ✅ Progress stages: `session.StageResolving` reported at entry (resolve.go:88), `session.StageUpdatingRepo` reported immediately before `Ensure` (resolve.go:105). `TestResolveOrdersAndComputesBase` asserts `stages[0]==StageResolving`, `stages[1]==StageUpdatingRepo` (diff:389-391) — passes per implementer's report.
- ✅ Step 2 (GetChange fan-out, `NOT_MERGED`/`INCOMPATIBLE_TARGETS`) — `fetchChanges` (resolve.go:217-252) + `checkMergedAndTargets` (resolve.go:254-274), present (see ordering caveat above).
- ✅ Step 3 (sort MergedAt asc, number tie-break) — resolve.go:130-135.
- ✅ Step 4 (mirror current, base branch exists) — resolve.go:106-120.
- ✅ Step 5 (`GetChangeCommits` per change) — resolve.go:143.
- ✅ Step 6 (landing resolution + ancestor check) — resolve.go:153-168.
- ✅ Step 7 (base SHA = first parent of earliest change's first landing commit) — resolve.go:182-186.
- ❌ **Design §6.1 step 1 is absent from this diff**: "Validate the request (`ResolveInput`): de-duplicate numbers, ≤ 50, base branch validated. Default base = repository default branch." `Resolve`'s signature takes a raw `[]int` and a raw `baseBranch string` with no dedup, no max-50 enforcement, and no default-branch fallback anywhere in resolve.go. The brief's own "Produces" section (task-13-brief.md:8-12) does not list a `ResolveInput` type or validation function at all, so this is plausibly out of scope for Task 13 (deferred to the HTTP handler/session layer that calls `Resolve`).
  - ⚠️ Cannot verify from this diff alone whether step 1 is implemented elsewhere (a different task/file) or genuinely missing from the pipeline. Flag for confirmation against whichever task owns the HTTP-facing request validation (likely Tasks 15/16, since the brief states "Tasks 15 and 16 call it verbatim").

## Job B — Audit

### Priority 1: Can any input produce a plausible-but-wrong `BaseSHA` with no error?

Traced every return path in `Resolve` (resolve.go:82-188). No path returns a non-empty `Resolved.BaseSHA` without a successful `objects.RevParse` call (resolve.go:182-186); `NewResolvedChange` rejects empty `LandingSHAs` (`internal/session/model.go:50`, `errors.New("resolved change: landing shas required")`), and `ResolveLanding`'s success paths (landing.go: merge → `SHAs:[found]`, squash → `SHAs:[found]`, rebase → `SHAs: walk` where `walk` is only returned once `len(walk)==n` and `n` is validated `>0`, landing.go:87-93) never produce an empty slice, so `resolved[0].LandingSHAs()[0]` cannot index a nil/empty slice.

- **Root commit (no first parent):** `objects.RevParse(ctx, first+"^1")` → git `rev-parse --verify --quiet <sha>^1^{commit}` exits 1 with no output → `RevParse` (mirror/objects.go:141-146) treats this as a generic error → `Resolve` maps it to `BASE_UNDETERMINED` (resolve.go:183-186). No guess produced. ✅
- **Degenerate `MergedAt` ordering (ties):** `sort.SliceStable` tie-breaks by `Number()` ascending (resolve.go:131-134) — deterministic regardless of tie. ✅
- **Empty landing SHAs list:** structurally unreachable per above — `NewResolvedChange` would reject it, and `ResolveLanding`'s success branches never return an empty slice. ✅
- **`rev-parse` returns something unexpected (e.g. exit 128 — corrupted mirror, disk failure, context deadline):** ❌ **Important.** `RevParse` (mirror/objects.go:137-146) does not distinguish "no such parent" (a real, informative BASE_UNDETERMINED case) from a genuine git/infra failure the way `BranchExists`/`IsAncestor`/`Exists` do (those check `gitx.IsExit(err, 1)` specifically and return a distinct `GIT_FAILURE` for anything else — mirror/objects.go:62-65, 131-134, 156-159). At the final base-SHA step, resolve.go:183-186 catches **any** `RevParse` error and reports `BASE_UNDETERMINED` with the hardcoded message `"commit %s has no first parent"` — even if the actual cause was a corrupted mirror or a canceled context, which is a `GIT_FAILURE`, not a base-ambiguity. The error code stays user-visible-safe (no wrong `BaseSHA`, still an explicit error — not Critical), but the message and code are misdiagnostic. Flag as **Important** under priority 4/1 boundary.

No path found that can silently produce a wrong `BaseSHA`. This is the strongest part of the implementation.

### Priority 2: Error-vs-negative-result discipline — every call site checked

| Call site | File:line | Verdict |
|---|---|---|
| `p.GetChange` (fan-out) | resolve.go:233-234, 238-250 | ✅ Propagated via `errs[i]`; classified through `MapProviderError`/`ErrNotFound`/default, never dropped. |
| `p.GetChangeCommits` | resolve.go:143-152 | ✅ `ErrTooManyCommits` explicit BASE_UNDETERMINED; other errors via `MapProviderError`/default PROVIDER_UNAVAILABLE. |
| `r.mirrors.Ensure` | resolve.go:106-112 | ✅ Mapped via `MapProviderError`, else REPOSITORY_UNAVAILABLE. Never read as a negative result. |
| `objects.BranchExists` | resolve.go:114-120 | ✅ Genuine error → GIT_FAILURE; `ok==false` (real negative, not swallowed error) → BASE_UNDETERMINED. Correctly distinguished (mirror/objects.go:156-159 separates exit-1 from other errors). |
| `objects.IsAncestor` | resolve.go:161-167 | ✅ Genuine error → GIT_FAILURE; `onBase==false` → NOT_ON_BASE_BRANCH. Correctly distinguished (mirror/objects.go:131-134). |
| `objects.RevParse` (final base) | resolve.go:183-186 | ⚠️ Error propagated (not swallowed as a negative result), but **not classified** — see Priority 1 finding above; every error becomes BASE_UNDETERMINED regardless of whether it's "no parent" or a real git failure. |
| `objects.Exists` (landing.go, candidate probe) | landing.go:52-56 | ✅ Error returned directly, not read as "candidate absent." |
| `objects.Parents` | landing.go:65-67 | ✅ Propagated directly. |
| `objects.FirstParentWalk` | landing.go:87-89 | ✅ Propagated directly. |
| `objects.PatchID` (`resolveSingleParent`) | landing.go:120-122 | ✅ Propagated directly. |
| `objects.PatchID`/`Exists` (`patchIDsMatch`) | landing.go:137-157 | ✅ This is Task 12's fixed logic (referenced in the audit brief) — `Exists` is checked first and only a clean `(false, nil)` is read as "genuinely absent"; any `PatchID` error propagates unconditionally (comment at landing.go:143-148 documents exactly why). Verified present and unchanged. |
| `r.mirrors.FetchSHA` (retry-by-sha) | resolve.go:196-200 (`landingWithFetch`) | ⚠️ Error is logged at `Debug` and swallowed (`continue`) rather than propagated as a distinct error; the subsequent `ResolveLanding` retry then fails with the generic `MISSING_COMMITS`, even if the true cause of continued absence was e.g. a provider-auth failure during the fetch attempt. This matches FR-5.8's literal text ("performs one explicit fetch attempt... and then fails with `MISSING_COMMITS`"), so it is **spec-compliant**, not a defect — but it means a `FetchSHA` failure is never surfaced as its own error code. Noting as Minor (see Priority 5 — this path also has zero test coverage). |
| `session.NewResolvedChange` | resolve.go:169-175 | ✅ Error checked and wrapped into BASE_UNDETERMINED, not dropped. |

No call site reads an error as a plain negative result (the Task 12 defect class). One call site (`RevParse`, final step) propagates the error but mislabels its code/message; one call site (`FetchSHA` in the retry loop) deliberately absorbs the error per FR-5.8's literal text.

### Priority 3: Concurrency

- **No shared-state escape:** `fetchChanges` writes to `out[i]`/`errs[i]` (pre-sized slices indexed by loop position), not appended — no lock needed, no shared mutable state touched by more than one goroutine at a given index (resolve.go:218-236). ✅
- **Deterministic output ordering (FR-5.4):** `fetchChanges` output is index-aligned to input `numbers` regardless of completion order (verified by `TestFetchChangesBoundedConcurrencyAndOrder`, diff:472-521, which drives 20 reversed-order numbers through a `countingProvider` with an artificial 20ms delay and asserts `got[i].Number() == reversed[i]`). Final ordering by `MergedAt` asc / number-tie is then applied by an explicit, single-threaded `sort.SliceStable` after the fan-out completes (resolve.go:130-135) — ordering is not dependent on goroutine completion order at any point. ✅
- **Bound respected, genuine contention:** the same test asserts `max > 1` (goroutines actually overlapped) and `max <= resolveConcurrency` (bound respected) using an atomic CAS-based high-water mark — this is a real, contending test, not a `-race`-only proof with no concurrent exercise. ✅ Good test.
- **First error cancels cleanly / no goroutine leak:** ⚠️ **Important, partially unmet.** `fetchChanges` (resolve.go:222-236) has no internal cancellation on first error — every goroutine runs `p.GetChange` to completion via `wg.Wait()` before any error is inspected; there is no `context.WithCancel` used to abort siblings once one fails. The "no leak" half is true: a goroutine blocked acquiring the semaphore exits promptly via `case <-ctx.Done(): errs[i] = ctx.Err(); return` (resolve.go:227-230), so nothing blocks forever if the *caller's* ctx is canceled. But "first error cancels" is not implemented — the pipeline does not self-cancel on an internal error, so a fast failure sits idle while up to `resolveConcurrency` slower siblings finish unnecessary provider calls. Not a correctness bug (results are still discarded/first-error-reported correctly), but it does not fulfill the "cancels cleanly" property as commonly understood, and there is no test exercising this scenario (see Priority 5).

### Priority 4: Error codes and messages

Verified each code (session/codes.go:8-17) against its trigger — all nine required codes are used, and reachable from a real path (table above). One direct violation of the "must use existing `Msg*` helpers" rule:

- ❌ **Important.** `fetchChanges`'s `ErrNotFound` branch does **not** use the existing `MsgRepositoryUnavailable` helper (`messages.go`, "The repository %s could not be read."); instead it builds its own ad hoc string inline: resolve.go:243 — `Message: fmt.Sprintf("#%d was not found in %s.", numbers[i], repo.FullName())`. Every other `ErrNotFound`/provider-error path in this file goes through `MapProviderError`, which does use `MsgRepositoryUnavailable` (resolve.go:57-58 in the diff). This one call site bypasses the helper and duplicates/diverges from the canonical message.
- ⚠️ **Minor, verified low-risk.** The brief's noted concern — `err.Error()` interpolated into `MsgBaseUndetermined` (resolve.go:174, on `NewResolvedChange` failure) — was checked against `internal/session/model.go:37-50`: every validation error there is a static string or a `%q`-formatted internal `Strategy` enum value (never a raw provider/git string, URL, or credential). This specific interpolation is safe in the current code, but it is fragile: if `NewResolvedChange`'s validation is later extended to include a raw SHA, title, or URL in its error text, this call site would silently start leaking it with no additional review trigger. Not a current violation.
- ✅ No occurrence of "worktree", "cherry-pick", or "synthetic branch" in `messages.go` or any `ReviewError.Message` construction in `resolve.go`/`landing.go` — grepped both files, zero matches outside this audit note itself.
- ✅ No raw git/provider error text is ever interpolated into a user-facing `Message` (`MsgGitFailure()` is a static string; `RevParse`'s wrapped git stderr is discarded, only replaced with a hardcoded string — see Priority 1 finding for why that's a *different* problem, but it is not a leak).

### Priority 5: Test coverage

- `ErrTooManyCommits` and the generic provider-error sub-paths inside the per-change loop (step 5/6): undisclosed by implementer as untested, matching the brief's own scope. These paths only ever produce explicit, correctly-coded `ReviewError`s (verified statically above) — they cannot produce a wrong `BaseSHA`. **Acceptable to defer** to Task 16 integration coverage per the calibration guidance ("coverage could be broader" is Minor when the untested path cannot produce a wrong review).
- ❌ **Important, undisclosed gap:** `landingWithFetch`'s entire retry-by-SHA branch (resolve.go:190-204, FR-5.8's core behavior) has **zero** test coverage. All three fixture-based tests use candidates already present in the mirror after `Ensure`, so `ResolveLanding` always succeeds on the first pass and `errors.Is(err, ErrNoCandidate)` is never true in any test run — the `for _, sha := range cr.LandingCandidates()` loop, the `FetchSHA` call, the swallowed-error `Debug` log, and the second `ResolveLanding` call are all dead code from the test suite's perspective. This is more load-bearing than the disclosed gaps: it's a named FR (FR-5.8), it's the one call site with the swallowed-error behavior noted under Priority 2, and it was not flagged by the implementer's report (which only names the loop's `ErrTooManyCommits`/provider-error sub-paths, not this one). Recommend a dedicated unit test — remove a landing SHA from the mirror, assert the retry fires and either recovers or produces `MISSING_COMMITS`.

## Strengths

- Base-SHA derivation (Priority 1, the highest-value check) is genuinely sound: every ambiguous or failed condition returns an explicit, correctly-typed error before touching `BaseSHA`. No guess path exists.
- `fetchChanges`'s concurrency test is a real, contending test (atomic high-water-mark + artificial delay + reversed input), not a `-race`-clean-but-unexercised rubber stamp — this directly addresses the class of defect the audit brief calls out from Task 5.
- Task 12's error-propagation discipline (`patchIDsMatch`'s careful `Exists`-first check before trusting a `PatchID` error as "genuinely absent") is carried through unchanged and correctly reused, not duplicated or weakened.
- The step-ordering deviation is disclosed prominently, with a concrete repro (RED run against the brief's own verbatim code) rather than asserted from memory — good process, even though the fix applied was broader than the evidence justified.
- Interface surface (`Resolved`, `Resolver`, `NewResolver`, `Resolve` signature, `resolveConcurrency`) matches the brief exactly, verified field-for-field.

## Issues

#### Critical (Must Fix)

None found.

#### Important (Should Fix)

- The `NOT_MERGED` check was reordered after `Ensure`/`BranchExists` along with the target-mismatch check, but no brief test requires this half of the move — it makes every resolve pay for a full mirror clone/fetch even when a selected change could be rejected immediately from already-fetched provider data. `resolve.go:96-125` vs. `resolve.go:122-125` (`checkMergedAndTargets`, unsplit).
- `objects.RevParse`'s final-step error (resolve.go:183-186) is unconditionally reported as `BASE_UNDETERMINED` / "has no first parent" even when the underlying git error is a genuine failure (e.g. corrupted mirror, canceled context) rather than a missing parent — unlike `BranchExists`/`IsAncestor`, which correctly distinguish exit-1 from other failures.
- `fetchChanges`'s `ErrNotFound` branch (resolve.go:243) does not use the existing `MsgRepositoryUnavailable` helper; it builds an inline, divergent message instead, violating the "messages must use the existing `Msg*` helpers" rule.
- `fetchChanges` does not self-cancel sibling goroutines on the first internal error — it waits for all `resolveConcurrency`-bounded goroutines to finish (no leak, but no fail-fast either) before reporting the first error. No test exercises this scenario.
- `landingWithFetch`'s entire FR-5.8 retry-by-SHA path (resolve.go:190-204) has zero test coverage — none of the shipped tests ever produce `ErrNoCandidate`, so the fetch-retry loop and its swallowed-`FetchSHA`-error behavior (Priority 2) are unverified by any test in this diff.

#### Minor (Nice to Have)

- `err.Error()` interpolated into `MsgBaseUndetermined` at resolve.go:174 is safe today (verified: only static strings/enum values reach it from `session.NewResolvedChange`) but has no guard against a future change to that validation logic introducing raw data into the message.
- `r.mirrors.FetchSHA` failures inside `landingWithFetch` are absorbed into a generic `MISSING_COMMITS` outcome rather than surfaced with their own code; this matches FR-5.8's literal wording, so it's not a violation, but it does mean an auth/network failure during the fetch-by-sha attempt is indistinguishable from a genuinely-gone commit to the end user.
- Design §6.1 step 1 (request validation: dedup, ≤50, default base branch) is not present anywhere in this diff; likely out of scope for Task 13 per the brief's own interface list, but unconfirmed from this diff alone.

## Assessment

**Task quality:** Needs fixes

**Reasoning:** The core base-selection logic is sound and the concurrency/error-propagation discipline this review series has previously flagged (Task 5's race, Task 12's swallowed error) is correctly avoided here. But the step-ordering fix was broader than the evidence required (an avoidable cost regression), one error path silently reclassifies a real git failure as base-ambiguity, one message bypasses the required helper, and the one FR-5.8-specific code path with an already-identified error-swallowing nuance has no test coverage at all.

---

## Fix round 1 re-review (18aac75..e18e8fe)

Scope: five Important findings from the round-1 audit above. Build/tests re-run in full; only `resolve.go`/`resolve_test.go` changed.

### Finding verdicts

**1. Split step reorder — ADDRESSED.** `checkMergedAndTargets` is now split: `checkMerged(changes)` (resolve.go:302-313) runs immediately after `fetchChanges` and before `Ensure`/`BranchExists` (resolve.go:87-89); `checkTargets(changes, baseBranch)` (resolve.go:316-326) stays after `BranchExists` (resolve.go:120-122), exactly the split the round-1 ruling called for — not a partial move (both new functions are pure, provider-data-only vs. baseBranch-dependent respectively) and not a reintroduction of the brief's illustrative order (the "missing branch" case still needs `BranchExists` to run first, which it does). All three pre-existing brief tests (`TestResolveOrdersAndComputesBase`, `TestResolveRejectsUnmergedAndBadTargets` with all four subcases, `TestResolveRejectsChangeNotOnBaseBranch`) ran unmodified and pass (verified directly, not taken on report: `go test -race -run ... -v`, all PASS). `TestResolveRejectsNotMergedBeforeFetchingMirror` (resolve_test.go, new) genuinely pins reject-fast: it points `CloneURL` at `file:///definitely/does/not/exist.git`, so if `Ensure` were ever reached the pipeline would fail with a git/repository error, not `NOT_MERGED` — the code-only assertion (`re.Code != session.CodeNotMerged`) is already dispositive, and the test additionally asserts the mirror directory does not exist on disk afterward (`os.Stat` / `os.IsNotExist`) as an independent, non-code-path-dependent second signal. This is not a merely-observed-error test; it distinguishes the reject-fast path from every other reachable error path by construction. Ran it directly: PASS.

**2. `RevParse` classification — ADDRESSED.** `classifyRevParseErr` (resolve.go:196-201) follows the identical `gitx.IsExit(err, 1)` discipline as `BranchExists`/`IsAncestor` (mirror/objects.go:131-134, 156-159): exit-1 → `BASE_UNDETERMINED` with "no first parent"; anything else → `GIT_FAILURE` via the static `MsgGitFailure()` (no raw git text). `TestResolveBaseIsRootCommit` (resolve_test.go) is genuine end-to-end coverage using real git via `internal/testutil` (`testutil.NewRepo`, real commits, real push, real `Resolve` call) — not a fake `ObjectReader`. Ran it directly: PASS (0.14s, real git subprocess time, confirming it's not a stub). `TestClassifyRevParseErr` constructs `&gitx.ExitError{Category: gitx.CategoryQuery, Result: gitx.Result{ExitCode: N}}` wrapped exactly as `objects.RevParse` wraps it (`fmt.Errorf("rev-parse %s^1: %w", ...)`, matching mirror/objects.go:143's `fmt.Errorf("rev-parse %s: %w", rev, err)` pattern) — this is the real error shape `RevParse` actually produces (confirmed `gitx.IsExit` uses `errors.As`, so `%w`-wrapping is correctly traversed), not a hand-built value the production path never produces. Both exit-1 and exit-128 branches assert code, message content, and message *non*-content (exit-128 case asserts the message does NOT claim "no first parent"). Ran it directly: PASS.

**3. `MsgRepositoryUnavailable` helper used — ADDRESSED.** `fetchChanges`'s `ErrNotFound` branch now reads `Message: MsgRepositoryUnavailable(repo.FullName())` (resolve.go:269), replacing the prior inline `fmt.Sprintf("#%d was not found in %s.", ...)`. The implementer's report correctly discloses the wording tradeoff (change number now only in `.Change`, not in message prose) — this was flagged as an acceptable, disclosed consequence in the finding, not a new defect.

**4. Fail-fast in `fetchChanges` — ADDRESSED, no ordering/ctx regression found.** `cctx, cancel := context.WithCancel(ctx)` (resolve.go:241) is passed to both the semaphore-wait `select` (resolve.go:250) and `p.GetChange` (resolve.go:255); each goroutine calls `cancel()` on its own error (resolve.go:257-259). The caller's own `ctx` cancellation is still honoured: `firstRealError` (resolve.go:285-297) checks `ctx.Err() != nil` (the *original*, non-derived context) and only then treats every `context.Canceled`/`DeadlineExceeded` entry as genuine; otherwise it skips cascade-cancellation fallout and surfaces the real sibling error. No goroutine leak: the `select`/`cctx.Done()` path still returns promptly, and `defer cancel()` (resolve.go:242) always fires. Ordering: `TestFetchChangesBoundedConcurrencyAndOrder` (unmodified, 20-number reversed-order test with genuine concurrency) still passes with the derived-context change in place — ran it directly: PASS, confirming the fail-fast addition didn't corrupt the pre-sized/indexed-write ordering guarantee (writes are still to `out[i]`/`errs[i]` by loop index, untouched by the `cctx` change). `TestFetchChangesCancelsSiblingsOnFirstError` genuinely proves cancellation, not just error observation: it uses a `cancelProvider` that blocks non-erroring calls on `ctx.Done()` with a 2s fallback timer, asserts `elapsed < 500ms` (impossible without real cancellation propagating through `cctx` into the provider call), `cancelledCount > 0` (a sibling actually observed `ctx.Done()` inside its own `select`), and `completedCount == 0` (nothing fell through to the 2s timer). All three assertions are independently regression-sensitive — removing the `cancel()` call or reverting to a plain `ctx` pass-through would make `elapsed` ≈2s and `completedCount` ≥1. Ran it directly: PASS (fast, consistent with fail-fast).

**5. FR-5.8 retry path — ADDRESSED; the "structurally impossible end-to-end" argument is TRUE, verified independently.** `Ensure` performs `git clone --mirror ...` (mirror/cache.go:74) — a full, non-shallow mirror clone with no `--depth`. A full mirror fetch of a ref transitively downloads that ref's entire ancestry. Therefore any commit that is an ancestor of `baseBranch` (a precondition `Resolve`'s downstream `objects.IsAncestor` check enforces on every `landing.SHA` after `landingWithFetch` returns, resolve.go:158-164) is, by construction, already present after `Ensure` — it cannot simultaneously be "missing until an explicit `FetchSHA`" (the FR-5.8 scenario) and "an ancestor of an already-fully-fetched ref." A full-`Resolve` test exercising "retry recovers a missing candidate, then the pipeline succeeds" is therefore not just hard to build, it is contradictory: the same commit would have to be both dangling (unreachable from any fetched ref) and reachable-as-ancestor of a fetched ref. The implementer's argument is correct on the merits, not a convenience — confirmed independently from `mirror/cache.go`'s clone invocation, not taken on the report's word. Given that, unit-level coverage of `landingWithFetch` is the right call. `TestLandingWithFetchRetriesAndSucceeds` builds a genuinely dangling object (pushed to a throwaway branch, then the branch ref deleted server-side) and includes an explicit precondition assertion (`objects.Exists` returns false before the call) so the test cannot silently pass without exercising the retry; ran it directly: PASS (0.12s, real git operations). `TestLandingWithFetchAbsorbsFetchFailure` uses a fabricated 40-`f` SHA that exists nowhere; since `cr.LandingCandidates()` will include this SHA and no other, and `ResolveLanding`'s first pass finds it absent (`ErrNoCandidate`), `landingWithFetch`'s retry loop is necessarily entered and `FetchSHA` necessarily attempted and necessarily fails (the SHA is fictitious) before falling through to the same `ResolveLanding` call and `MISSING_COMMITS` — the retry path is genuinely exercised, not bypassed. Ran it directly: PASS. The absorbed-`FetchSHA`-error behavior is unchanged from the original (`continue` on `fetchErr`, `Debug`-level log only, no raw git output in the log call — `slog.String("repository", ...)`, `slog.String("sha", shortSHA(sha))`) and is now pinned by this test where it previously had zero coverage.

### Test quality

All six new/changed tests were run directly (not taken from the report) and pass; per-test regression sensitivity assessed above inline. None found vacuous: each asserts either (a) a specific error code plus a construction that makes any other reachable code/behavior distinguishable (Findings 1, 2, 5's absorb test), (b) exact message content and content-exclusion (Finding 2's classifier test), or (c) timing + counter assertions that only hold under genuine cancellation (Finding 4). `TestFetchChangesCancelsSiblingsOnFirstError`'s 500ms/2s timing margin is generous (4x), low flake risk in CI. `TestLandingWithFetchAbsorbsFetchFailure` does not directly assert `FetchSHA` was invoked (e.g., via a call counter) — it infers the retry path was exercised from the deterministic code structure rather than proving it by instrumentation; this is acceptable given the code path is unconditional (`ErrNoCandidate` always enters the loop) but is a minor gap in directness, not a vacuous assertion.

### Credential-safety check on newly propagated errors

`classifyRevParseErr`'s `GIT_FAILURE` branch (resolve.go:200) returns the static `MsgGitFailure()` — "A git command failed while building the review. See Diagnostics for details." (messages.go:83-85) — no interpolation of `err`, `err.Error()`, or any git stdout/stderr into the user-visible message. Checked every other message helper touched or newly reachable in this diff (`MsgRepositoryUnavailable`, `MsgNotMerged`, `MsgIncompatibleTargets`, `MsgBaseUndetermined`, `MsgProviderUnavailable`, `MsgProviderAuth`) — all are `fmt.Sprintf` over caller-controlled identifiers (repo full name, provider ID, change numbers, branch name) or static strings; none embed a raw error value. No new logging of raw git output either — `landingWithFetch`'s existing `Debug` log (resolve.go:211) logs only repository name and a short SHA, unchanged by this diff.

### New breakage in the fix diff

None found. Full backend gate re-run directly:
- `cd apps/backend && go build ./...` — clean, no output.
- `cd apps/backend && go test -race -count=1 ./...` — all 13 packages `ok`, including `internal/review` (2.974s).
- No exported signature changed (`Resolve`, `NewResolver`, `Resolved`, `Resolver`, `resolveConcurrency` — resolve.go:17, 66 — byte-identical to round 1).
- No `session.Code*` string values changed; grepped `resolve.go`/`messages.go` for `worktree`, `cherry-pick`, `synthetic branch` — zero matches outside this audit file.
- No `os/exec` import, no `//nolint` directive, no gosec exclusion added in `resolve.go`/`resolve_test.go` (grepped directly) — matches the implementer's claim of zero `//nolint`.
- Commit `e18e8fe` message begins `fix(task-001): ...` — uses the task-001 id as required.

### Deferred minors

- `err.Error()` interpolation into `MsgBaseUndetermined` at resolve.go:171 (on `NewResolvedChange` validation failure) — unchanged from round 1, still safe today per the same reasoning (static/enum-only inputs), still no guard against future drift. Not part of this fix round's scope; carrying forward as previously noted, not re-litigated.
- `TestLandingWithFetchAbsorbsFetchFailure` infers rather than directly instruments that `FetchSHA` was attempted (see Test quality above) — cosmetic, does not affect the verdict.

### Assessment

**Fix round verdict:** All findings addressed

**Reasoning:** Each of the five findings was independently re-verified against the actual diff and by running the specific new/pre-existing tests directly (not taken from the implementer's report), the full race-enabled test suite and build are clean, and the one argument offered in place of harder test setup (Finding 5's structural-impossibility claim) checks out against `mirror/cache.go`'s real `clone --mirror` invocation rather than being an unverified convenience.
