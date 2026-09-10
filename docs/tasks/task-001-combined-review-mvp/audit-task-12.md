# Audit — Task 12: review input validation, messages, landing-commit resolution

- **Diff:** `144d237..39f7c5f`
- **Build:** PASS (`go build ./...`, clean)
- **Tests:** PASS (`go test -race -count=1 ./internal/review/...` — 21/21 tests, verified directly, not taken from implementer's report)

## Named risk — `patchIDsMatch` swallowing a `PatchID` error

**Verdict: Critical. This is the anti-pattern, not an acceptable exception to it.**

`apps/backend/internal/review/landing.go:288-291`:
```go
for _, c := range commits {
    id, err := o.PatchID(ctx, c.SHA())
    if err != nil {
        return false, nil // the original commit may be gone after a rebase
    }
```

Under what conditions can `PatchID` error here? Read `mirror.objects.PatchID` (`apps/backend/internal/mirror/objects.go:83-96`): it runs `git show --format= -p <sha>` piped into `git patch-id --stable`, and **every** non-nil error from either step — `fmt.Errorf("show %s: %w", ...)` / `fmt.Errorf("patch-id %s: %w", ...)` — is returned identically, with no exit-code triage. Compare `Exists` (`objects.go:47-64`), which was explicitly hardened this same review cycle to distinguish exit 1 ("object not present") from exit 128 ("corrupted mirror, missing git dir, unreadable object database") via `gitx.IsExit(err, 1)`. `PatchID` received no equivalent treatment. So "the object isn't in the mirror" is **not** distinguishable from a fatal git failure (context timeout, killed process, corrupted pack, disk I/O error) at this call site — the comment's stated rationale describes one possible cause among several indistinguishable ones.

**What the wrong branch produces.** `patchIDsMatch` is only invoked when `len(walk) == N && len(commits) == N` (`landing.go:248`) — i.e., a real 1-parent candidate whose ancestry the walk already resolved. If any single `PatchID(c.SHA())` call fails for a transient/fatal reason (not "gone after rebase"), the whole multiset comparison silently returns `(false, nil)`. That flows back to `resolveSingleParent`: for `N > 1` this skips the correct `StrategyRebase` classification and falls to the "squash of several commits" branch (`landing.go:263-271`), which checks only `PatchID(found) != ""` and — if the squash commit born of a genuine rebase land has a non-empty diff, which it always will — returns `Landing{Strategy: StrategySquash, SHAs: []string{found}}` **with no error**. The user sees a confident, wrong result: a rebase-landed PR is reported as `session.StrategySquash` with a single landing SHA instead of `StrategyRebase` with the full N-commit chain (`walk`, oldest-first). Net code diff for the combined review may coincide (same final tree state via `found`'s cumulative diff against its pre-chain parent), but the commit-level provenance Task 13/15/19 build on — which and how many commits actually landed — is wrong, and it is wrong with a green `nil` error, exactly the "confident, incorrect review" this task's brief warns is worse than a normal swallow-error bug elsewhere in the plan.

**Is the brief's rationale sound, or self-grading?** Self-grading. The brief wrote both the code (`landing.go` snippet) and the comment justifying it, and the implementer's report (line 41) repeats the brief's own framing almost verbatim ("kept this exactly as the brief specifies") without independently verifying that `PatchID`'s error is actually classifiable as "object gone." It is not — `PatchID` was never hardened the way `Exists` was. The implementer's claim that "a genuinely broken mirror would already have surfaced via [`Exists`/`Parents` on the candidate]" doesn't hold: `Exists`/`Parents` are called against `found` (the *candidate*), not against `c.SHA()` (the *original, pre-rebase commit*), which is a different, independently-fetched object. A transient failure specific to fetching/reading that original object is not ruled out by the candidate's own health.

**What I would do instead.** Either (a) harden `mirror.objects.PatchID` the same way `Exists` was hardened — have it call `Exists` first (or classify the `git show` exit code) so `patchIDsMatch` can legitimately treat "confirmed absent" as `(false, nil)` while propagating everything else, or (b) have `patchIDsMatch` call `o.Exists(ctx, c.SHA())` before `PatchID` and only swallow when `Exists` cleanly returns `(false, nil)`; any other `PatchID` error should propagate to the caller as a real error (surfacing as `MsgGitFailure`/a genuine failure) rather than being read as "not a rebase."

## Job A — Spec compliance

✅ **Spec compliant.** Every produced symbol matches the brief's interface note exactly, verified by reading the diff directly (not the implementer's report):

- `CreateInput{ProviderID, Repository, BaseBranch string; Changes []int}` — `input.go:50-55`.
- `func (in CreateInput) Validate(ctx context.Context, r gitx.Runner) (CreateInput, error)` — `input.go:59`.
- `InputError{Code session.Code; Field, Message string}` implementing `error` via `func (e *InputError) Error() string` — `input.go:41-47`.
- Codes declared as `session.Code` with exact strings — `input.go:33-38`: `CodeInvalidProvider = "INVALID_PROVIDER"`, `CodeInvalidRepository = "INVALID_REPOSITORY"`, `CodeInvalidBranch = "INVALID_BRANCH"`, `CodeInvalidChanges = "INVALID_CHANGES"`.
- All eleven `Msg*` helpers plus `TargetMismatch{Number int; Target string}` present in `messages.go:797-880`, signatures match the brief verbatim (`MsgNotMerged([]int) string`, `MsgIncompatibleTargets([]TargetMismatch) string`, `MsgNotOnBaseBranch(int, string) string`, `MsgMissingCommits([]string) string`, `MsgBaseUndetermined(string) string`, `MsgConflict(int, bool) string`, `MsgProviderAuth(string) string`, `MsgProviderUnavailable(string) string`, `MsgRepositoryUnavailable(string) string`, `MsgGitFailure() string`, `MsgInterrupted() string`).
- `Landing{Strategy session.Strategy; SHAs []string; SourceSHA string}` — `landing.go:174-179`.
- `func ResolveLanding(ctx context.Context, o mirror.ObjectReader, cr provider.ChangeRequest, commits []provider.Commit) (Landing, error)` — `landing.go:195`.
- `var ErrNoCandidate = errors.New("no landing candidate exists in the mirror")` — `landing.go:172`.
- Failures return `*session.ReviewError` with `session.CodeBaseUndetermined` (`landing.go:189-191, 198-202, 232, 269`) / `session.CodeMissingCommits` (`landing.go:216-220`, wrapped in `*noCandidateError` per the brief's own amended "Note on MISSING_COMMITS" instruction so both `errors.Is(err, ErrNoCandidate)` and `errors.As(err, &*session.ReviewError)` succeed — confirmed by `TestResolveLandingRealMissingCandidate`, `landing_realgit_test.go:461-467`, and `TestResolveLandingErrors`, `landing_test.go:750-756`).

No ⚠️ items — everything needed to verify Job A is present in this diff; no cross-package assumption was required.

## Job B — Audit

### 1. `ResolveLanding` correctness against design §6.2

- Candidate ordering: `cr.LandingCandidates()` used as the sole candidate source (`landing.go:196`), matching the merge→squash→head ordering documented in `provider/model.go:120` and design §6.2. ✓
- 2-parent → merge: `landing.go:227-228`, matches. ✓ Real-git-verified (`TestResolveLandingRealMerge`).
- 1-parent branches delegated to `resolveSingleParent` (`landing.go:230, 236-272`), matches the design pseudocode's structure for match/no-match, N==1/N>1. ✓
- 0 or ≥3 parents → `BASE_UNDETERMINED` — `landing.go:231-233`. ✓ Real-git-verified for both 0-parent and 3-parent (octopus) cases.
- **`cr.Squashed()` is never called anywhere in `landing.go`.** Design §6.2's prose (`design.md:367-369`) states: "If patch-ids do not match and N > 1 **while the provider says `Squashed == false`**, the change is still treated as squash only if the candidate's diff is non-empty; otherwise `BASE_UNDETERMINED`." The implementation applies this non-empty-diff gate unconditionally, regardless of what the provider's `Squashed` flag says (`landing.go:262-271`). The design's own ASCII pseudocode (`design.md:346-358`) omits `Squashed()` entirely, so the two parts of the design document disagree with each other — but the task brief I was given explicitly asks me to verify whether `Squashed()`/`CommitCount()` are "used as intended," and `Squashed()` is provably unused. **Important** — a documented accessor from `provider.ChangeRequest` that the design's prose treats as load-bearing for this exact branch is silently dropped, and no test exercises a `Squashed()==true` change with a non-matching/empty-diff candidate to show the omission is harmless.
- **Untested, undocumented guessing when `commits` is empty:** `resolveSingleParent` (`landing.go:237-243`):
  ```go
  n := len(commits)
  if n == 0 {
      n = cr.CommitCount()
  }
  if n <= 0 {
      n = 1
  }
  ```
  Design §6.2 defines `N := len(originalCommits)` with no fallback. This implementation adds two speculative fallbacks not authorized by the design: falling back to `cr.CommitCount()`, and then — if that is *also* zero or negative — forcing `n = 1` outright. With `commits == nil` and `cr.CommitCount() == 0` (a caller bug upstream, e.g. a truncated commit-list fetch that didn't error), `n` becomes 1, `len(commits)(=0) != n(=1)` so the match branch at `landing.go:248` is skipped entirely, and the function falls straight through to `if n == 1 { return Landing{Strategy: StrategySquash, SHAs: []string{found}, ...}, nil }` at `landing.go:260-261` — **a confident `Landing` with no error, no patch-ID verification of any kind, given literally zero evidence about the original commits.** This is precisely the "plausible-but-wrong `Landing` with no error" the audit brief asks me to hunt for. Neither `landing_test.go` nor `landing_realgit_test.go` exercises `commits == nil` / `cr.CommitCount() == 0`. **Critical** given the calibration rule ("any input that can produce a plausible-but-wrong Landing with no error"), tempered only by the fact that triggering it requires an upstream caller bug (a merged PR should always report ≥1 commit) — but ResolveLanding itself provides no defense-in-depth and no diagnostic if that bug occurs.
- `patchIDsMatch` original-commit error swallow: see Named Risk above — Critical.

### 2. Error-vs-absent discipline — every `Exists`/`Parents`/`PatchID`/`FirstParentWalk`/`IsAncestor`/`RevParse`/`BranchExists` call site in the diff

| Site | File:line | Verdict |
|---|---|---|
| `o.Exists(ctx, c)` in candidate loop | `landing.go:206-209` | PASS — error returned immediately, unmodified. Verified by `TestResolveLandingPropagatesExistsError` (`landing_realgit_test.go:518-533`). |
| `o.Parents(ctx, found)` | `landing.go:222-225` | PASS — error returned immediately, unmodified. Verified by `TestResolveLandingPropagatesParentsError` (`landing_realgit_test.go:543-555`). |
| `o.FirstParentWalk(ctx, found, n)` | `landing.go:244-247` | PASS — error returned immediately. No dedicated test forces this path (only `Exists`/`Parents` got explicit propagation tests), but the code shape is identical to the two verified sites. |
| `o.PatchID(ctx, s)` for walked candidate-ancestry commits | `landing.go:278-281` (inside `patchIDsMatch`) | PASS — error returned immediately. |
| `o.PatchID(ctx, c.SHA())` for provider-reported original commits | `landing.go:288-291` (inside `patchIDsMatch`) | **FAIL — error swallowed as `(false, nil)`.** See Named Risk. |
| `o.PatchID(ctx, found)` (squash-of-many non-empty-diff check) | `landing.go:264-267` | PASS — error returned immediately. |

`IsAncestor`, `RevParse`, `BranchExists` are not called anywhere in this diff (reserved for later tasks per the brief) — no site to audit here.

### 3. Test quality

Confirmed genuinely real via `testutil.Repo` + a real `gitx.ExecRunner` + `mirror.New(...).Objects(...)` (`landing_realgit_test.go:332-341`), not fakes dressed up — spot-checked `testutil.Repo.MergeNoFF`/`Squash`/`Rebase`/`Git` (`apps/backend/internal/testutil/repo.go:94-129`), which shell out to the real `git` binary.

§6.2 branch → coverage:
- 2-parent merge: real (`TestResolveLandingRealMerge`).
- 1-parent, N==1, match → squash: real (`TestResolveLandingRealSquashSingleCommit`).
- 1-parent, N>1, match → rebase: real (`TestResolveLandingRealRebase`).
- 1-parent, N>1, no-match, non-empty diff → squash: real (`TestResolveLandingRealSquashOfManyCommits`).
- Candidate-chain fallthrough to `HeadSHA` (ff): real (`TestResolveLandingRealGitLabFastForward`).
- Missing candidate → `MISSING_COMMITS`: real (`TestResolveLandingRealMissingCandidate`).
- 0-parent (root) → `BASE_UNDETERMINED`: real (`TestResolveLandingRealRootCommit`).
- ≥3-parent (octopus) → `BASE_UNDETERMINED`: real (`TestResolveLandingRealOctopusMerge`).
- **1-parent, N>1, no-match, empty diff → `BASE_UNDETERMINED`: fake only** (`landing_test.go:771-782`, the `o4` case in `TestResolveLandingErrors`). No real-git test constructs this branch. The implementer's self-review (`task-12-report.md:140`) claims "every §6.2 branch ... has a test built on real git," which is not accurate for this branch — the report's own coverage table (`task-12-report.md:47-56`) doesn't list it either. **Important** — an overstated claim of coverage plus an untested branch.
- Candidate-skip-then-fallback-to-next-candidate behavior: fake only (`TestResolveLandingSkipsAbsentCandidate`). Minor — secondary to the top-level classification branches, but no real-git equivalent exists.
- `Exists`/`Parents` error propagation: intentionally fake-only (`explodingExists`/`explodingParents`), which is reasonable — forcing a genuine fatal git error deterministically in real git is impractical. Not a finding.
- The `n<=0`/empty-`commits` guessing path identified in §1 above: **untested by either real or fake tests.**

### 4. Input validation

- Reuses `gitx.ValidateRepoFullName`, `gitx.ValidateBranch`, `gitx.ValidateChangeNumbers` — no regex reimplementation (`input.go:63, 67, 74`). ✓
- Rejects branch names starting with `-`: delegated to `gitx.ValidateBranchSyntax` inside `ValidateBranch`, which checks `strings.HasPrefix(s, "-")` before ever invoking the runner (`apps/backend/internal/gitx/validate.go:48-53`). ✓
- ≤50 / non-empty / positive: delegated to `gitx.ValidateChangeNumbers` (`MaxChanges` bound, `len(in)==0` check, `ValidateChangeNumber` positivity check — `validate.go:75-96`). ✓
- Duplicate changes: rejected via the added `hasDuplicate` helper (`input.go:86-95`) before calling `gitx.ValidateChangeNumbers`, per the plan.md-resolved layering decision (already accepted — not re-flagged here).
- Correct `INVALID_*` code + `Field` returned for each failure path — `input.go:60-77`, verified by `TestCreateInputValidate`'s table (`input_test.go:128-147`), all 5 sub-tests pass.

### 5. Message text as product surface

- `grep -rniE "worktree|cherry-pick|synthetic branch"` against `messages.go`, `landing.go`, `input.go` — zero matches (verified directly, not taken on the implementer's word).
- No message interpolates raw provider/git output. `MsgProviderAuth`/`MsgProviderUnavailable` interpolate only a `providerID` (a configured identifier, e.g. `"gh"`), `MsgRepositoryUnavailable` interpolates the `Repository` string the caller validated via `ValidateRepoFullName`, `MsgMissingCommits`/`undetermined()` interpolate only SHAs/short-SHAs and a caller-constructed reason string — none of these carry raw stdout/stderr from a git or provider call. ✓

### 6. Determinism

- `LandingCandidates()` order is fixed (merge, squash, head) and consumed in that order (`landing.go:196, 205`). ✓
- `Landing.SHAs` is always either `[]string{found}` or `walk` (from `FirstParentWalk`, contractually oldest-first, `objects.go:101-118`) — never built from map iteration. ✓
- `patchIDsMatch`'s `counts` map is used only for a zero-sum multiset check (`landing.go:300-304`); the final iteration order over `counts` doesn't affect the boolean result. ✓ No ordering-flakiness risk found in this diff.

## Strengths

- Every symbol in the brief's interface note is implemented exactly, including the brief's own late-breaking correction (`noCandidateError` wrapper) rather than the earlier, acknowledged-wrong `fmt.Errorf("%w: %s", ...)` snippet.
- `Exists` and `Parents` error propagation is correctly disciplined and is the only place in this diff with dedicated tests proving it (`TestResolveLandingPropagatesExistsError`/`ParentsError`), going beyond what the brief's own `landing_test.go` required.
- Eight genuine real-git scenarios (not fakes) covering most §6.2 branches, including a corrected octopus-merge fixture after catching a fast-forward false-negative during development (per the report) — real diligence, independently verified by re-running the suite.
- Duplicate-change contradiction between the brief's own prose and its own test was caught and resolved transparently rather than silently picking a side.
- Message vocabulary constraint honored; zero `nolint` in the package.

## Issues

#### Critical (Must Fix)
- `patchIDsMatch` swallows any `PatchID` error on the provider-reported original commit as "no match" rather than propagating it — `landing.go:288-291`. A transient/fatal git failure is indistinguishable from "commit gone after rebase" at this call site (`PatchID` was never hardened like `Exists` was), and the failure mode is a silently downgraded `StrategyRebase` → `StrategySquash` with a truncated `SHAs` list, returned with `nil` error. **Plan-mandated** (the brief specifies this exact swallow); report it anyway.
- `resolveSingleParent` guesses `n = 1` when `commits` is empty and `cr.CommitCount() <= 0` (`landing.go:237-243`), skipping all patch-ID verification and returning a confident `StrategySquash` `Landing` with no error and no diagnostic. Not specified by design §6.2 (which defines `N := len(originalCommits)` with no fallback), and untested by either the brief's tests or the implementer's additions.

#### Important (Should Fix)
- `cr.Squashed()` is never consulted in `landing.go`, despite design §6.2's prose making it a condition of the non-empty-diff squash-acceptance gate (`design.md:367-369`). The implementation applies that gate unconditionally instead. No test demonstrates the omission is harmless.
- The empty-diff / no-patch-id-match → `BASE_UNDETERMINED` branch (`landing.go:268-270`) has no real-git test, only the brief's `fakeObjects` case (`landing_test.go:771-782`). The implementer's self-review (`task-12-report.md:140`) overstates coverage, claiming every §6.2 branch has a real-git test.

#### Minor (Nice to Have)
- Candidate-skip-then-fallback-to-next-candidate (`TestResolveLandingSkipsAbsentCandidate`) has no real-git equivalent — only the brief's fake test.
- `FirstParentWalk` error propagation (`landing.go:244-247`) has no dedicated propagation test, unlike `Exists`/`Parents`.

## Assessment

**Task quality:** Needs fixes.

**Reasoning:** Job A interface compliance is perfect and the real-git test investment is genuinely strong work, but Job B surfaces two Critical paths — the plan-mandated `patchIDsMatch` error swallow and an untested implicit `n=1` guess — that can each produce a confident, wrong `Landing` with no error, which is exactly the failure mode this task is weighted to catch above everything else in this plan.

---

## Fix round 1 re-review (39f7c5f..ffb360f)

### Finding verdicts

1. **`patchIDsMatch` error discipline: ADDRESSED.** `landing.go:145-166` now calls `o.Exists(ctx, c.SHA())` before `o.PatchID`. `Exists` error → `return false, err` (`landing.go:150-152`, propagated). `Exists` clean `(false, nil)` → `return false, nil` (`landing.go:153-155`, genuine-absence swallow, unchanged behavior for that legitimate case). `Exists` `(true, nil)` → falls through to `o.PatchID(ctx, c.SHA())`, whose error now propagates unmodified (`landing.go:157-159`, `return false, err`, no longer `return false, nil`). All three `PatchID` call sites checked: (a) `landing.go:127-129` in the `walk` loop — already propagated pre-fix, unchanged; (b) `landing.go:157-159` in the `commits` loop — the fixed site; (c) `landing.go:112-118` in `resolveSingleParent`'s squash-of-many gate — already propagated pre-fix, unchanged, now additionally gated by `!cr.Squashed()`. `internal/mirror` diff is empty (`git diff 39f7c5f..ffb360f -- apps/backend/internal/mirror` produced no output) — confirmed untouched, ruling honored.

2. **`resolveSingleParent` no longer guesses: ADDRESSED.** `landing.go:78-90`: when `n <= 0` (empty `commits` and `cr.CommitCount() <= 0`), returns `undetermined(cr.Number(), "the change's commit information was unavailable")` — a `*session.ReviewError{Code: CodeBaseUndetermined}` — instead of setting `n = 1`. Read the full function (`landing.go:78-127`): every subsequent branch (`walk`/match/no-match/`n==1`/`n>1`) is now only reached with a verified positive `n`; no other branch proceeds on an unverified assumed count. `TestResolveLandingNoCommitInfoUndetermined` exercises this and passes.

3. **`Squashed()` load-bearing: ADDRESSED.** `landing.go:110-118`: the non-empty-diff gate (`o.PatchID(ctx, found)` / empty-diff → `BASE_UNDETERMINED`) is now wrapped in `if !cr.Squashed()`. This matches design §6.2 prose verbatim (`design.md:367-369`: "If patch-ids do not match and N > 1 **while the provider says `Squashed == false`**, the change is still treated as squash only if the candidate's diff is non-empty; otherwise `BASE_UNDETERMINED`") — the design already specified this condition; the pre-fix code simply never implemented the `Squashed == false` qualifier. It changes behaviour (a `Squashed()==true`, empty-diff candidate now succeeds instead of erroring) and both branches are covered: `TestResolveLandingSquashedFlagBypassesEmptyDiffCheck` (Squashed=true, empty diff → succeeds) and `TestResolveLandingUnsquashedEmptyDiffStillUndetermined` (Squashed=false, same empty diff → `CodeBaseUndetermined`), both built through the real `provider.ChangeRequestBuilder`/`RepositoryBuilder` (not a hand-rolled interface stub), so the test drives the actual `Squashed()` accessor path.

4. **Every §6.2 branch real-git covered: ADDRESSED.** `TestResolveLandingRealSquashEmptyDiff` (`landing_realgit_test.go`) uses `testutil.NewRepo(t)` + real `gitx.ExecRunner` (via `realObjects(t, r)`, the same helper used by every other real-git test in the file, not a fake) — builds a feature branch that adds then reverts `x.txt` content, `git merge --squash` + `git commit --allow-empty`, so the squash commit's tree is first-parent-identical and `git patch-id` yields `""`. Ran it directly (`go test -race -count=1 ./internal/review/... -v`): `--- PASS: TestResolveLandingRealSquashEmptyDiff (0.09s)`. Cross-checked the full §6.2 pseudocode branch list against the report's table (`task-12-report.md:192-204`): 2-parent merge, 1-parent N==1 match, 1-parent N>1 match (rebase), 1-parent N>1 no-match non-empty-diff (squash), 1-parent N>1 no-match empty-diff (now covered), fallthrough-to-head, 0-parent, ≥3-parent, missing-candidate (§6.3) — all nine have a `TestResolveLandingReal*` test, verified each exists in `landing_realgit_test.go` and passed in the full-package run. The report's claim is scoped correctly: it explicitly does *not* claim the candidate-skip-fallback and `Exists`/`Parents`-error-propagation tests are real-git (they remain fake, correctly disclosed as such, and those aren't §6.2 strategy branches — they're candidate-selection-loop mechanics ahead of the switch).

### Test quality

- `TestResolveLandingPatchIDErrorOnOriginalCommitPropagates` (`landing_test.go:295-317`): drives the production code path (`ResolveLanding` → `resolveSingleParent` → `patchIDsMatch`) with a real `Exists(sha2)==true` fixture, forcing `PatchID(sha2)` to fail via a thin wrapper (`patchIDErrorOn`) around the real `fakeObjects` walk/exists/patchID logic — not a hand-built `ReviewError` asserted against itself. It asserts both that an error is returned *and* that it is **not** `errors.As`-matchable to `*session.ReviewError` (i.e., it's the raw propagated error, not repackaged) — this would fail on a regression back to the old `return false, nil` swallow, since the old behavior returned `nil` error entirely. Not vacuous.
- `TestResolveLandingPatchIDAbsentOriginalDegradesGracefully` (`landing_test.go:324-339`) is a genuine regression guard for the companion path: `Exists(sha2)==false` cleanly, asserts no error and a valid `Landing` via the squash-fallback — would fail if the fix over-corrected into propagating absence as an error too.
- `TestResolveLandingNoCommitInfoUndetermined` (`landing_test.go:270-278`) asserts `CodeBaseUndetermined`; would fail against the old `n=1` guess (which returned `StrategySquash`, `nil` error). Not vacuous.
- `TestResolveLandingSquashedFlagBypassesEmptyDiffCheck` / `TestResolveLandingUnsquashedEmptyDiffStillUndetermined` (`landing_test.go:345-387`) are a genuine paired control: identical empty-diff fixture, only `Squashed()` differs, opposite outcomes asserted. This is exactly the shape needed to prove the gate is load-bearing rather than decoratively called (dead code would make one of these two fail).
- `TestResolveLandingRealSquashEmptyDiff` is real git, not scripted patch-ids — would fail if the `!cr.Squashed()` empty-diff gate were removed or the walk logic broken.
- No always-true or self-referential assertions found in the new tests (contrast with the prior round's flagged issue — not repeated here).

### Credential-leak check on newly propagated errors

- `gitx.ExitError.Error()` (`apps/backend/internal/gitx/spec.go:50-52`): `fmt.Sprintf("git %s exited with code %d", e.Category, e.Result.ExitCode)` — formats only the `Category` label and exit code, never `Result.Stdout`/`Result.Stderr`. The newly-propagated `PatchID` errors (`objects.go:83-96`, wrapped as `fmt.Errorf("show %s: %w", sha[:7], err)` / `fmt.Errorf("patch-id %s: %w", sha[:7], err)`) therefore carry only a short SHA prefix and the underlying `ExitError`'s category/exit-code string when they reach `patchIDsMatch`'s caller — no raw git stdout/stderr, no credentials (credentials are injected via `GIT_CONFIG_COUNT`/`extraheader`, per design §5.3, never present in `Result.Stdout`/`Stderr` content anyway). `landing.go` itself does not wrap or reformat these errors — they pass through `ResolveLanding`'s return unmodified. Clean.

### New breakage in the fix diff

- None found. Checked the specific scenario flagged in the brief: does the added `Exists` call change behaviour for the genuinely-absent-original case that previously degraded gracefully? No — `TestResolveLandingPatchIDAbsentOriginalDegradesGracefully` proves the `Exists()==(false,nil)` path still degrades to "no match" with `nil` error, identical to pre-fix behavior for that case.
- Considered whether a healthy-but-mirror-incomplete scenario (original commit's parent object not fetched, so `git show` inside `PatchID` fails even though the commit object itself passes `Exists`) could now surface as a new user-visible error where it previously degraded silently. `internal/mirror` has no shallow-clone/depth-limiting logic (`grep -rn "shallow\|depth\b" internal/mirror/*.go` — no matches), so mirrors are full fetches and this scenario is not a realistic "healthy rebase" path; a `PatchID` failure with `Exists==true` in production is a genuine git fatal error (corrupted pack, timeout, killed process), which is exactly what should propagate. Not treated as breakage.
- Pre-existing fake-based tests (`TestResolveLandingSquashSingleCommit`, `TestResolveLandingSquashOfManyCommits`, `TestResolveLandingRebase`, `TestResolveLandingGitLabFastForwardFallsThroughToHead`) had their `exists` maps updated to add the original-commit SHAs (`git diff 39f7c5f..ffb360f -- landing_test.go`, four `-exists:` / `+exists:` pairs) — required by the new `Exists`-first order, not a scope-widening change; confirmed these are fixture corrections, not weakened assertions (each test's final assertion on `Strategy`/`SHAs` is unchanged).
- Build (`go build ./...`) clean; `go test -race -count=1 ./internal/review/...` — 24/24 pass (verified directly, not taken on report's word); `nolint` count in `internal/review`: 0 (`grep -rn "nolint" apps/backend/internal/review/` — no matches); no `os/exec` in the package (`grep -rn "os/exec" apps/backend/internal/review/` — no matches).
- Commit `ffb360f` message: `fix(task-001): harden landing-commit resolution against confident-wrong results` — uses `task-001`, not a package name. Correct.
- Message vocabulary: `messages.go` was **not modified** in this diff (only `landing.go`/`landing_test.go`/`landing_realgit_test.go` changed per `git show ffb360f --stat`), so `MsgBaseUndetermined`'s wording is unchanged from the already-passed prior round. `undetermined()`'s new call site (`landing.go:87`, "the change's commit information was unavailable") passes a caller-constructed reason string into the existing `MsgBaseUndetermined` formatter — no new forbidden vocabulary; re-ran `grep -rniE "worktree|cherry-pick|synthetic branch"` against `messages.go`/`landing.go`/`input.go` — zero matches.

### Deferred minors

- Both Minor items from the original audit (`TestResolveLandingSkipsAbsentCandidate` has no real-git equivalent; `FirstParentWalk` error propagation has no dedicated test) were not in scope for this fix round and remain open, unchanged. Not blocking.

### Assessment

**Fix round verdict:** All findings addressed.

**Reasoning:** Both Criticals are genuinely closed rather than relocated — the `patchIDsMatch` swallow is now correctly gated on a clean `Exists` false (an `Exists` error propagates, verified at the exact line), and the `n<=0` guess is replaced with a visible `CodeBaseUndetermined` return with no other branch left proceeding on an unverified count; both Important items are load-bearing (`Squashed()` changes behaviour, proven by a paired test) and real-git covered (verified by direct test execution, not the report's claim alone), and no new breakage, credential leak, or vocabulary regression was found in the diff.
