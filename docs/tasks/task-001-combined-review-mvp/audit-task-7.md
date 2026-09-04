# Audit — Task 7: Scripted-repository test harness (`internal/testutil`)

Diff reviewed: `9b03c0c..c9ee4f8` (single commit `c9ee4f8`, files `apps/backend/internal/testutil/repo.go`, `apps/backend/internal/testutil/repo_test.go`).

## Job A — Spec compliance

✅ Spec compliant, with two notes (not deviations, see Job B):

- All 14 documented methods exist with the brief's exact names and signatures:
  `NewRepo` (repo.go:40), `Git` (repo.go:86), `Commit` (repo.go:89), `Branch` (repo.go:104),
  `Checkout` (repo.go:107), `MergeNoFF` (repo.go:110), `Squash` (repo.go:117), `Rebase`
  (repo.go:135), `BranchCommits` (repo.go:125), `Push` (repo.go:146), `RevParse` (repo.go:149),
  `Head` (repo.go:155), `CloneURL` (repo.go:158), `FileContent` (repo.go:161).
- `Repo` struct (repo.go:32-37) carries `T`, `Work`, `Bare` as documented, plus an unexported
  `home` field — an implementation detail, not a public-interface deviation.
- `NewRepo`: bare repo at `<tmp>/origin.git` (repo.go:46), working clone at `<tmp>/work`
  (repo.go:46), one initial commit on `main` (`README.md`) pushed to bare (repo.go:52-54).
  Matches brief exactly.
- `MergeNoFF`/`Squash`: operate on "the current branch" (repo.go:109, 116 doc comments), not
  literally forced onto `main` by the method itself — the brief's own reference implementation
  has the identical property (caller must `Checkout("main")` first, which the self-test does at
  repo_test.go:191, 203). Not a deviation from the brief's own code; flagged only as a
  documentation-clarity note in Job B.
- `Rebase`: rebases `branch` onto `main`, fast-forwards `main`, returns rewritten SHAs
  (repo.go:135-143). Matches brief's documented sequence.
- `BranchCommits`: `main..branch` via `rev-list --reverse` (repo.go:127), oldest-first. Matches.
- `CloneURL`: `"file://" + r.Bare` (repo.go:158). Matches `file://<Bare>` exactly.
- `FileContent`: `git show rev:path`, returns `""` on any error (repo.go:161-169). Matches the
  documented "empty when absent" surface behavior; see Job A finding below for what this hides.
- `RevParse` uses `rev-parse --verify rev^{commit}` (repo.go:149-152) — stricter than the bare
  `rev-parse` the brief's snippet shows, but it's still `RevParse(rev string) string`; the
  narrowing to `^{commit}` makes non-commit or bad revisions fail loudly via `r.T.Fatal` inside
  `run()` rather than silently resolve to a tag/tree object. This strengthens the harness's
  fail-loud contract; not a spec violation.

No missing methods, no extra public surface, no misunderstood semantics in the observable
contract.

⚠️ Cannot verify from diff alone: the implementer's report claims a local run against git 2.55
proved `checkout -b main` after `clone` (repo.go:52) succeeds without collision on the bare
repo's already-symref'd unborn `main` branch. The diff/self-test's reported PASS output
(quoted in the task-7 report) is the only evidence for this; I did not re-run it (out of scope
per audit instructions — no doubt here that the existing report doesn't already answer).

## Job B — Audit

### 1. Do the git shapes match the claims?

**Strength — self-test asserts actual git shape, not exit codes:**
- `MergeNoFF` shape: repo_test.go:196-198 checks `rev-list --parents` on the merge SHA has
  exactly 3 fields (commit + 2 parents) and that the first parent equals `base` — proving a real
  two-parent merge commit was created, not a fast-forward. `--no-ff` flag used at repo.go:112.
- `Squash` shape: repo_test.go:205-207 checks the squash commit has exactly 2 fields
  (commit + 1 parent) equal to the prior merge SHA — proving no second parent was recorded
  (i.e., genuinely squashed, not a real merge). `git merge --squash` + separate `commit` at
  repo.go:119-121 is the correct pattern (squash never auto-commits or records MERGE_HEAD).
  Content check at repo_test.go:208-210 confirms the squashed tree matches the branch's latest
  content.
- `Rebase` shape: repo_test.go:217 checks the rewritten SHA differs from the pre-rebase SHA
  (`rebased[0] != c1`) and that `main`'s `Head()` matches it after the `--ff-only` merge
  (repo.go:141) — proving the rebase genuinely rewrote history and `main` was correctly
  fast-forwarded.

These are exactly the shape assertions the task calls for, not "commands exited 0" checks.

### 2. Determinism

- Author/committer name+email pinned via env vars (repo.go:64-65), not ambient git config.
- `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` pinned to a fixed literal (repo.go:66) — no wall-clock
  leakage into SHA-affecting metadata.
- `env()` (repo.go:58-69) **replaces** `cmd.Env` entirely rather than appending to
  `os.Environ()` — so no ambient env var (including `XDG_CONFIG_HOME`) reaches the subprocess
  except the explicit whitelist plus `PATH`.
- `HOME` is redirected to a fresh per-repo temp dir (repo.go:47, 61) and `GIT_CONFIG_NOSYSTEM=1`
  is set (repo.go:62) — isolates from both the developer's `~/.gitconfig` and system config.
- `commit.gpgsign=false` forced via `-c` in `run()` (repo.go:73) — prevents signing
  prompts/config from affecting SHAs or hanging the test.
- `init.defaultBranch=main` is forced explicitly two ways: `--initial-branch=main` on the bare
  `init` (repo.go:50) and `-c init.defaultBranch=main` on every `run()` invocation (repo.go:73)
  — the harness does not depend on the host's ambient default branch name.
- `LC_ALL=C` pinned (repo.go:67) — output formatting stable for the `TrimSpace`-based parsing
  in `run()`.
- `FileContent` (repo.go:161-169) builds its own `exec.Command` directly rather than going
  through `run()`, but does reuse `cmd.Env = r.env()` (repo.go:164) — so the same determinism
  pinning applies there too. It omits the `-c` flags `run()` adds, but `git show` needs none of
  them (no commit, no protocol negotiation). Not a gap.

No unpinned determinism input found.

### 3. Ordering determinism

- `BranchCommits` uses `rev-list --reverse main..branch` (repo.go:127) — correct: `rev-list` is
  newest-first by default, `--reverse` is present and required. The self-test genuinely asserts
  **order**, not just membership: repo_test.go:192 checks `got[0] != a1 || got[1] != a2`
  positionally.
- `Rebase` reuses `BranchCommits` for its return value (repo.go:139), so it shares the same
  `--reverse` mechanism.

**Important gap:** the self-test's `Rebase` case only ever rebases a **single-commit** branch
(`feat/c` has exactly one commit, `c1` — repo_test.go:213). The assertion at repo_test.go:217
(`len(rebased) != 1 || rebased[0] == c1 || ...`) is order-blind by construction: a one-element
slice is trivially "in order" regardless of whether `--reverse` is wired correctly. The task's
own risk callout — "Three earlier tasks in this plan hit real flakes from ordering and
shared-state assumptions" — is exactly the class of bug this test cannot catch for `Rebase`.
`BranchCommits`'s ordering is independently verified (point above), and `Rebase` calls the same
function, so the mechanism is very likely correct — but the self-test as written does not prove
`Rebase`'s "oldest-first" promise for the n>1 case that downstream tasks (8-16) are most likely
to rely on (e.g., asserting rewritten commit order across a multi-commit feature branch).

### 4. Error paths

- Every `git` invocation through `run()` (repo.go:71-83) checks `cmd.Run()`'s error and calls
  `r.T.Fatalf` with captured stderr (repo.go:79-81) on failure — no swallowed non-zero exits in
  the `Git`/`Commit`/`Branch`/`Checkout`/`MergeNoFF`/`Squash`/`BranchCommits`/`Rebase`/`Push`/
  `RevParse`/`Head` path.

**Important gap:** `FileContent` (repo.go:161-169) is the one method that does **not** follow
the harness's fail-loud contract. `cmd.Output()`'s error is checked, but *any* error — a bad
revision, a corrupted repo, `git` itself failing to execute, not just "path absent at this
rev" — collapses to the same `""` return (repo.go:166-168). Every other method in this package
treats a git failure as a harness bug and calls `t.Fatal`; `FileContent` alone treats "some
error occurred" and "the path genuinely doesn't exist at this rev" as indistinguishable. A
downstream test asserting `FileContent(rev, path) == ""` to mean "file absent" would pass
identically if the harness itself were mis-invoking git (e.g., a caller passes a `rev` string
that isn't valid) — exactly the "confusing downstream failure" the audit brief warns about.

### 5. Lint-driven changes

- `os.MkdirAll(filepath.Dir(p), 0o750)` (repo.go:92) — confirmed changed from the brief's
  `0o755` to satisfy gosec G301. `0o750` (owner rwx, group rx, no other access) is sufficient
  for a private `t.TempDir()`-scoped working directory; nothing in the harness needs
  group-other access to these paths. No functional impact.
- `//nolint:gosec` on `run()`'s `exec.Command` call (repo.go:74) — single trailing-line
  comment, scoped to that exact call site, with rationale ("testutil intentionally shells out to
  the real git binary with fixed args"). Not a block or file-level exclusion.
- `//nolint:gosec` on `FileContent`'s `exec.Command` call (repo.go:162) — same pattern, same
  rationale, narrowly scoped.
- No changes to `.golangci.yml` in this diff (confirmed: diff touches only `repo.go` and
  `repo_test.go`) — no module-wide gosec exclusion was reintroduced. Task 4's removal stands.
- **Discrepancy in the implementer's report:** the report states "I also added two
  narrowly-scoped `//nolint:gosec` comments... on the two `exec.Command` call sites." The diff
  actually contains a **third** `//nolint:gosec` at repo.go:95, on `os.WriteFile(p, []byte(content), 0o644)`
  inside `Commit` (`//nolint:gosec // test fixture file, not sensitive`) — this suppresses
  gosec G306 (permissive file write mode), not G204, and isn't on an `exec.Command` call at
  all. The directive itself is fine (single-line, scoped, rationale present), so this is a
  reporting-accuracy gap rather than a code defect — but it means the report's self-review
  undercounts what was actually suppressed and mischaracterizes one of the three.

## Strengths

- Self-test verifies real git shapes (parent counts, parent identity, content, SHA rewriting)
  for every strategy (`MergeNoFF`, `Squash`, `Rebase`), not merely that commands exited 0 —
  this is precisely the bar the audit brief sets as most important.
- Determinism inputs are comprehensively pinned: author/committer identity and dates, locale,
  `HOME` isolation, `GIT_CONFIG_NOSYSTEM`, explicit `init.defaultBranch=main` set two
  independent ways, and `commit.gpgsign=false`. `env()` replaces rather than extends the
  subprocess environment, closing off ambient-env leakage entirely.
- `BranchCommits`'s "oldest-first" claim is directly, positionally asserted by the self-test.
- Package correctly carries no build tag, uses `os/exec` directly rather than `gitx`, and takes
  `*testing.T` with `t.Fatal`-on-failure — all per the brief's explicit intent.
- No module-wide gosec exclusion reintroduced; both/all `//nolint:gosec` directives are
  line-scoped with rationale.

## Issues

#### Critical (Must Fix)
None found.

#### Important (Should Fix)
- **`FileContent` swallows all git errors as `""`, not just "path absent."** repo.go:166-168.
  Every other method in the package fails loud (`t.Fatal`) on a git error; `FileContent` alone
  conflates "file genuinely absent at this rev" with "the git invocation itself failed" —
  risking a silently-wrong downstream assertion in Tasks 8-16 that reads as a false negative
  rather than a clear harness error.
- **`Rebase`'s oldest-first ordering claim is not exercised by a multi-commit case.**
  repo_test.go:212-219 rebases a single-commit branch (`feat/c`, only `c1`), so the length-1
  assertion at repo_test.go:217 cannot distinguish correct `--reverse` ordering from any other
  ordering. This is exactly the ordering-flake risk class the task brief calls out as having
  bitten three earlier tasks in this plan.

#### Minor (Nice to Have)
- Implementer report undercounts/mischaracterizes the lint-driven `//nolint:gosec` annotations:
  states "two... on the two `exec.Command` call sites" but the diff contains three, including
  one on `os.WriteFile` (repo.go:95) suppressing G306, not G204. The code itself is compliant
  (narrow, rationale present); this is a self-review accuracy gap, not a defect.
- `MergeNoFF`/`Squash` doc comments describe operating on "the current branch"
  (repo.go:109, 116) rather than explicitly stating "must be on `main`"; correctness depends on
  callers doing `Checkout("main")` first, which the self-test does but nothing enforces. Same
  property as the brief's own reference code — flagged only for downstream-task clarity, not as
  a functional defect.

## Assessment

**Task quality:** Needs fixes

**Reasoning:** The core git-shape verification (merge/squash/rebase parent structure, content,
SHA rewriting) and determinism pinning are done correctly and are well-evidenced by the
self-test — the hardest and most important part of this task is solid. But two Important gaps
mean the harness cannot yet be fully trusted by Tasks 8-16: `FileContent`'s blanket error
swallowing can mask harness bugs as false "absent" results, and the `Rebase` ordering promise
that three earlier tasks already got burned by is asserted only in a degenerate (n=1) case that
provides no real evidence it holds for the multi-commit rebases downstream tests will actually
perform.

---

## Fix round 1 re-review

Diff reviewed: `c9ee4f8..20e7a7f` (commit `20e7a7f`, files `apps/backend/internal/testutil/repo.go`,
`apps/backend/internal/testutil/repo_test.go`). Scope: verify the two Important findings above
were genuinely fixed, audit the `//nolint:gosec` delta (3→5), and check for new breakage.

### Finding verdicts

**1. `FileContent` error classification: ADDRESSED** — `apps/backend/internal/testutil/repo.go:149-174`.

Evidence: `FileContent` now (a) resolves `rev` via `r.RevParse(rev)` at repo.go:151, which itself
Fatals inside `run()` (repo.go:63-65) on an invalid revision — so a bad `rev` never reaches the
existence check; (b) runs `git cat-file -e <sha>:<path>` at repo.go:154-158 to test existence
explicitly, classifying the result via `gitObjectMissing` (repo.go:185-188) rather than
stderr-pattern-matching; (c) only on confirmed existence runs `git show` (repo.go:166-173) to
fetch content, Fataling on any error there too. This closes the exact gap flagged in the
original audit: a bad revision string can no longer be silently reported as "path absent"
(repo.go:171-fixed via original audit line 118-125), because `RevParse` intercepts it before
`cat-file` ever runs.

Robustness judgment on the classification mechanism: it is **not** stderr string-matching —
`gitObjectMissing` (repo.go:185-188) type-switches on `errors.As(err, &exitErr)` for
`*exec.ExitError`, which is robust against git version/locale wording changes (the concern the
implementer's report itself raises and correctly avoids). Given that `object` is always
`<40-hex-SHA>:<path>` (the SHA pre-validated by `RevParse`), the only realistic way
`git cat-file -e` exits nonzero is "path absent in that tree," so in practice the classifier is
sound for this call site.

However, the mechanism is coarser than it needs to be: `gitObjectMissing` (repo.go:185-188)
treats **any** `*exec.ExitError` as "missing," without checking `exitErr.ExitCode()` or
`exitErr.Exited()`. A subprocess killed by a signal (e.g., OOM-killed, or killed by a `go test`
timeout) also surfaces as a Go `*exec.ExitError`, and would be misclassified as "path absent"
(a false `""`) rather than triggering `r.T.Fatal`. This is a real, narrow gap — a false "absent"
verdict can theoretically still slip through — but it is low-probability in this harness's
actual usage (no per-command timeouts are wired, and TempDir-scoped repos are not
resource-constrained). Verified in `TestGitObjectMissingClassification` (repo_test.go:158-179)
that the test suite itself only exercises "clean nonzero exit" (`exec.Command("false").Run()`,
repo_test.go:164) — it does not exercise the signal-kill case, so this gap is untested as well
as unfixed. Logged below as a deferred minor, not a reopened Important finding, since the
originally-flagged defect (blanket swallow of *all* errors, including a bad `rev`) is fully
closed.

**2. `Rebase` multi-commit ordering test: ADDRESSED** — `apps/backend/internal/testutil/repo_test.go:58-94`.

`TestRebaseMultiCommitOrdering` creates a 3-commit branch (`d1`/`d2`/`d3`, each appending a line
to `d.txt` so content is monotonically distinguishable, repo_test.go:62-64), rebases it onto a
`main` that has moved (repo_test.go:65-66, 68), and asserts: `len(rebased) == 3`
(repo_test.go:69-71); all three SHAs differ from their pre-rebase originals, proving genuine
rewriting rather than reuse (repo_test.go:74-76); positional content at each of the three
indices via `FileContent(rebased[i], "d.txt")` matches the expected cumulative content
(repo_test.go:80-88) — this is a real ordering assertion, not a distinctness-only or
length-only check; and `r.Head() == rebased[2]` (repo_test.go:91-93), proving `main`
fast-forwarded to the last (newest) rewritten commit. This is exactly what was asked for and
closes the n=1 degeneracy the original audit flagged (original audit lines 99-108).

`BranchCommits` positional assertion for a multi-commit branch was already present before this
fix round and untouched by it: `repo_test.go:19` (`got[0] != a1 || got[1] != a2`, a 2-commit
`feat/a` branch) — confirmed still in place, unchanged by the diff. No extension was needed
here per the fix report's own reasoning, and that reasoning holds.

### nolint audit

5 directives total (`grep -n nolint` on both files), all single-line, call-site-scoped, with a
stated rationale:

1. `repo.go:58` — `exec.Command("git", full...)` in `run()`. Pre-existing, unchanged by this diff.
2. `repo.go:79` — `os.WriteFile(..., 0o644)` in `Commit`. Pre-existing, unchanged, suppresses G306 not G204.
3. `repo.go:154` — new: `git cat-file -e` existence check in `FileContent`. Scoped to the exact `exec.Command` call, rationale present ("testutil intentionally shells out to the real git binary with fixed args"). Necessary: this is the new G204-triggering call the classification split introduced.
4. `repo.go:166` — new: `git show` content fetch in `FileContent`. Same rationale, same necessity — this is the same call site as before the fix, just re-annotated because it's now a distinct code path from the existence check.
5. `repo_test.go:143` — new: subprocess re-invocation of `os.Args[0]` in `TestFileContentBadRevisionFails`. Scoped to the exact call, rationale ("test-only subprocess re-invocation of the current test binary, fixed args") is accurate — `os.Args[0]` plus two fixed literal flags, no attacker-controlled input.

No suppression is broader than a single line; none is a blanket "disable this check" comment
divorced from a specific finding. `.golangci.yml` (`apps/backend/.golangci.yml`) is **not** in
the diff's file list and `git status --porcelain` shows it untracked-clean (not modified) —
confirmed no module-wide exclusion was added by this fix round.

Separately (out of this diff's scope, noted only for completeness): `apps/backend/.golangci.yml:22`
already carries a module-wide `G204` exclude, and `:27-31` excludes `gosec` (and `errcheck`)
entirely for all `_test.go` files. Since this file is untouched by `c9ee4f8..20e7a7f`, it predates
this fix round and is not a regression introduced here — it is out of scope for this scoped
re-review per the task instructions, which state the original audit's other conclusions stand.

### Test quality

The new tests assert real behavior, not restated implementation:

- `TestRebaseMultiCommitOrdering` (repo_test.go:58-94) ties positional assertions to actual,
  distinguishable file content per index, not just slice length or SHA distinctness — this is
  precisely what makes it non-vacuous versus the original n=1 test.
- `TestFileContentAbsentPath` (repo_test.go:98-103) asserts `== ""` for a real absent path at a
  valid revision — a direct, non-tautological check of the documented contract.
- `TestFileContentBadRevisionFails` (repo_test.go:139-152) does not merely call the method and
  hope; it uses a genuine subprocess re-invocation and asserts the subprocess **exits nonzero**
  (repo_test.go:146-148) and that the correct helper test actually ran (repo_test.go:149-150,
  checking the output contains the helper test's name) — this guards against the subprocess
  failing for an unrelated reason (e.g., a build error) being mistaken for the expected Fatal.
  Not vacuous.
- `TestGitObjectMissingClassification` (repo_test.go:158-179) exercises the pure classifier
  against four real, distinct error instances (nil, a genuine `*exec.ExitError` from
  `exec.Command("false").Run()`, a generic `errors.New`, and a genuine `*exec.Error` from a
  failed `LookPath`) rather than fabricated/mocked error values — a meaningful unit test of the
  classification logic in isolation.

No vacuous or always-true assertions found in the new tests.

### New breakage in the fix diff

None found at Critical or Important severity.

- `go build ./internal/testutil/...` (from `apps/backend`) is clean — confirmed directly.
- `FileContent`'s content-fetch path (repo.go:166-173) deliberately still uses `showCmd.Output()`
  directly rather than routing through `r.run()`, because `run()` (repo.go:66) does
  `strings.TrimSpace` on stdout, which would strip the trailing newline from file content and
  break the pre-existing `TestHarnessStrategies` assertion `FileContent("main","b.txt") == "bb\n"`
  (repo_test.go:35). This is a deliberate, correctly-reasoned deviation, not an inconsistency.
- Both new `exec.Command` calls in `FileContent` (repo.go:154, 166) pass `cmd.Env = r.env()`
  (repo.go:156, 168) — the pinned, replacing (not extending) environment is preserved on both
  new call sites; no ambient-env leakage introduced.
- `TestRebaseMultiCommitOrdering`'s added `other2.txt` commit on `main` (repo_test.go:66) touches
  a distinct file from `d.txt`, so it cannot spuriously conflict with the rebase — no flake risk
  introduced by file overlap.
- Public surface unchanged: `FileContent(rev, path string) string` signature is identical
  (repo.go:149); no other public method (`NewRepo`, `Git`, `Commit`, `Branch`, `Checkout`,
  `MergeNoFF`, `Squash`, `Rebase`, `BranchCommits`, `Push`, `RevParse`, `Head`, `CloneURL`) or
  `Repo` field (`T`, `Work`, `Bare`) was touched by this diff — confirmed by the diff's hunks,
  which only modify `FileContent`'s body and append new functions/tests.

### Deferred minors

- `gitObjectMissing` (repo.go:185-188) classifies **any** `*exec.ExitError` as "object missing,"
  without checking `ExitCode()`/`Exited()`. A signal-killed `git cat-file -e` subprocess would be
  misclassified as "path absent" (a false `""`) rather than failing loud. Low probability given
  no timeouts are wired into this harness's `exec.Command` calls, and untested by
  `TestGitObjectMissingClassification` (repo_test.go:158-179), which only exercises a clean
  nonzero exit. Not blocking — the finding as originally scoped (blanket swallow of all errors,
  including a bad `rev`) is fixed.
- `apps/backend/.golangci.yml:22,27-31` carries a pre-existing module-wide `G204` exclude and a
  blanket test-file `gosec`/`errcheck` exclusion, predating this fix round and out of this
  scoped re-review's remit; flagged only for awareness in case it's relevant to a broader lint
  audit.

### Assessment

**Fix round verdict:** All findings addressed

**Reasoning:** Both Important findings from the original audit are closed with direct,
non-vacuous evidence — `FileContent` now fails loud on every git error except a confirmed-absent
path (with a pre-validated revision closing the "bad rev misread as absent" case specifically),
and `Rebase`'s oldest-first promise is now proven against a real 3-commit, content-tied ordering
assertion. The one residual gap (coarse `*exec.ExitError` classification not distinguishing
signal-kills from clean nonzero exits) is a narrow, untested edge case rather than a reopening of
either original finding, and is logged as a deferred minor.
