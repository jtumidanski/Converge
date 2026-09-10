# Audit — Task 8: Mirror cache and object reader

Diff: `20e7a7f..6114dd8` (commit `6114dd8`). Files: `apps/backend/internal/mirror/{cache.go,cache_test.go,objects.go,objects_test.go}`.

## Job A — Spec compliance

✅ Spec compliant.

- `func New(root string, runner gitx.Runner, locks *gitx.LockMap, log *slog.Logger) *Cache` — `cache.go:28`, matches verbatim.
- `(*Cache) Path(providerID, fullName string) (string, error)` → `<root>/<providerID>/<fullName>.git` — `cache.go:33-43`. Verified: `New("/cache",...); c.Path("gitlab-work","atlas/sub/server")` builds `filepath.Join("/cache","gitlab-work","atlas","sub","server.git")` (`cache.go:40-42`), matching the format exactly.
- `(*Cache) Ensure(ctx, p provider.GitProvider, repo provider.Repository) (mirrorPath string, err error)` — `cache.go:54`, signature matches (named-return semantics equivalent to unnamed `(string, error)`).
- `(*Cache) FetchSHA(ctx, p provider.GitProvider, repo provider.Repository, sha string) error` — `cache.go:84`, matches.
- `(*Cache) Lock(mirrorPath string) func()` — `cache.go:46`, matches, thin delegate to `gitx.LockMap.Lock`.
- `type ObjectReader interface {...}` — `objects.go:14-22`, all seven methods present with exact signatures (`Exists`, `Parents`, `PatchID`, `FirstParentWalk`, `IsAncestor`, `RevParse`, `BranchExists`), matching the brief character-for-character.
- `(*Cache) Objects(mirrorPath, repoName string) ObjectReader` — `objects.go:31-33`, matches.
- Files created: exactly `cache.go`, `objects.go`, `cache_test.go`, `objects_test.go` — matches the brief's `Files:` list, nothing extra, nothing missing.
- Diff body of `cache.go` and `objects.go` is byte-identical to the brief's Step 3 code blocks (confirmed by side-by-side read).

**Deviation — `TestEnsureSerialisesConcurrentCallsOnSameMirror` (`cache_test.go:218-263`).** Judged genuinely additive and proves what it claims:
- It drives 8 real goroutines (`cache_test.go:233-241`) calling `Ensure` concurrently against the *same* mirror path (`repoFor(t, "fake", src.CloneURL())` is invariant across all goroutines), using the real `ExecRunner` (not `FakeRunner`), so `gitx.LockMap` is genuinely contended, not mocked.
- It asserts all calls return no error and the same `mirrorPath` (`cache_test.go:245-256`), then asserts the resulting mirror is a valid, readable bare repo via `Objects(...).Exists` (`cache_test.go:260-262`) — this is the correct proof that concurrent clone attempts into the same directory did not corrupt it, which a mere "no error" assertion would not catch (a torn/partial clone could still exit 0 in pathological interleavings without the lock).
- This is not concurrency theatre: without the `c.Lock(path)` call at `cache.go:59`, this test would be expected to fail intermittently (first-writer creates `HEAD` while a second concurrent `clone --mirror` targets the same not-yet-existent directory) — it exercises the exact hazard the brief's own "Lessons from earlier tasks" note (Task 5 data race) warns about.

No other deviations found. ⚠️ Cannot verify from this diff alone whether `REPOSITORY_CACHE_ROOT` (mentioned in the `cache.go:1` package doc comment) is actually wired to `New`'s `root` argument anywhere — that wiring, if it exists, is outside this diff's four files.

## Job B — Audit

`internal/mirror` has no `model.go` or `resource.go` — it is infrastructure/support code (filesystem-backed cache + git plumbing reader), so the DOM-*/SUB-* domain checklist does not apply; the task's seven priorities substitute for it, as directed.

**1. Path construction and traversal — PASS.**
`Path` (`cache.go:33-43`) validates `providerID` against `providerIDRe = ^[a-z0-9-]+$` (`cache.go:17`, no `/`, `.`, or control chars possible) and `fullName` via `gitx.ValidateRepoFullName` (`cache.go:37`). Read `internal/gitx/validate.go:32-44`: `ValidateRepoFullName` requires the `owner/name(/...)` shape via `repoRe = ^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$` (an allowlist that excludes `/` inside a segment), rejects a string starting with `/`, `-`, or `.`, and — critically — walks every `/`-split segment rejecting `""`, `"."`, `".."`, or any segment containing `".."`. This closes the traversal vector even for a payload like `atlas/../../etc/passwd` (each segment individually checked) or `../escape` (caught by the segment loop and the string-prefix check). `cache_test.go:167-172` exercises both `c.Path("gh", "../escape")` and `c.Path("../gh", "a/b")` and asserts rejection. No filesystem path is ever built from unvalidated input.

**2. Token containment — PASS.**
`Ensure` and `FetchSHA` never touch tokens directly; both route through `p.AuthorizeGit(repo, &spec)` (`cache.go:70`, `cache.go:95`), which is `provider.GitProvider`'s documented "environment only" credential channel (`internal/provider/provider.go:21-22`, pre-existing, not in this diff). Read `internal/gitx/credentials.go:9-24`: `CredentialEnv` puts the Basic-auth header into `GIT_CONFIG_VALUE_0`/env, never into the clone URL or argv — the clone URL passed to `p.CloneURL(repo)` (`cache.go:68`) stays token-free, so `git remote get-url origin` on the resulting mirror is clean, which `TestEnsureRealGitNoCredentialInRemote` (`cache_test.go:265-293`) verifies against a real git mirror. Error strings never leak credentials either: `ExitError.Error()` (`internal/gitx/spec.go:50-52`) formats only category and exit code, no stderr; stderr that does get logged is passed through `Redact(stderrLog, r.opts.Secrets)` first (`internal/gitx/exec.go:165`, pre-existing). `mirror`'s own error wraps (`cache.go:71,77,99`) only wrap this already-clean error, adding no new token surface.

**3. Locking correctness — PASS, with one forward-looking risk flagged.**
`Ensure` acquires the lock at `cache.go:59` (`unlock := c.Lock(path); defer unlock()`) *before* the `exists(HEAD)` check, the clone/update branch selection, `AuthorizeGit`, and the actual `runner.Run` — i.e., the lock covers the entire clone-or-update decision plus execution, and `defer` makes the unlock unconditional across every return path (validation-error early-returns happen *before* the lock is taken, so no early-return leaves the lock held). Same shape in `FetchSHA` (`cache.go:92-93`). No cached in-memory state is returned from inside the locked region — `Cache` holds no map keyed by mirror path; state lives entirely on disk, so the Task 5 shape (mutex + `defer Unlock()` + a live pointer into cache state read after unlock) does not reproduce here; there is no pointer to leak. `TestEnsureSerialisesConcurrentCallsOnSameMirror` (`cache_test.go:218-263`) proves the lock is load-bearing under real contention, not just present in source.
Flagging per the brief's own instruction to judge it: `Lock` is exported (`cache.go:46`) and backed by a plain non-reentrant `sync.Mutex` via `gitx.LockMap.Lock` (`internal/gitx/lock.go:8-13`). A future caller (Tasks 12/13/15, out of this diff's scope) that calls `Cache.Lock(path)` directly and then, on the same goroutine, calls `Ensure`/`FetchSHA` for that same path will deadlock (self-deadlock on a non-reentrant mutex) — there is no reentrancy guard and no doc comment warning of it (`cache.go:45` says only "Lock takes the per-mirror mutex."). This is inherent to the brief-mandated exported signature, not a defect introduced by this implementation, and no misuse exists within this diff's own files — recorded as a forward risk, not a current bug.

**4. All git execution through `gitx.Runner` — PASS.**
`grep -n "os/exec\|exec\.Command" internal/mirror/*.go` → no matches. Every git invocation in `cache.go` (`cache.go:73`) and `objects.go` (`objects.go:36-44`, `objects.go:80`) goes through `runner.Run`/`o.run`, which itself wraps `runner.Run`.

**5. Bounds — PASS.**
`FirstParentWalk(ctx, sha, n)` (`objects.go:94-111`) rejects `n <= 0` (`objects.go:98-100`) and passes `--max-count=`+n directly to `git rev-list` (`objects.go:101`), so the walk is bounded by git itself, not by client-side iteration. No other method in this package loops over git output at all — every `ObjectReader` method is a single `runner.Run`/`o.run` call followed by string parsing of one bounded response; there is no retry loop, no pagination loop, and no unbounded parent-walk recursion.

**6. Output parsing — Important finding.**
Most parsing is robust: `Parents` treats empty `rev-list --parents` output as an error rather than an empty parent list (`objects.go:69-72`); `RevParse`/`IsAncestor`/`BranchExists` propagate any error that isn't specifically exit-code-1 (`objects.go:124-127`, `objects.go:149-152`, `objects.go:134-138`).
However, `Exists` (`objects.go:47-59`) collapses **two different git exit codes into the same "not found, no error" result**: `if gitx.IsExit(err, 1) || gitx.IsExit(err, 128) { return false, nil }` (`objects.go:55-56`). Exit code 128 is git's generic *fatal* exit code, used both for "object does not exist" and for unrelated fatal conditions — e.g. a corrupted/missing mirror directory, a `.git` that fails to open, or (per `git-cat-file` output) "Not a valid object name" caused by something other than a genuinely absent commit. Treating every 128 as "commit doesn't exist" means a corrupted mirror, a bad `Dir`, or an environment fault surfaces to the caller as an innocuous `(false, nil)` — indistinguishable from a legitimate cache miss — instead of an error the caller can act on (e.g. re-clone, alert). This is the same shape the brief itself calls out ("Task 7's review caught a method that collapsed every git failure into an innocuous-looking empty value"). Neither test exercises the 128 path narrowly enough to prove it's safe: `TestObjectReader`'s zero-SHA case (`objects_test.go:497-499`) only proves *some* not-found path returns `(false, nil)`, not which exit code was actually hit or that a genuine fatal error is still surfaced as an error.

**7. Test depth — PASS.**
Both halves of the brief's required split are genuinely present, not just one masquerading as the other:
- Command-shape assertions via `gitx.FakeRunner`: `TestPathLayoutAndValidation` (`cache_test.go:161-173`) and `TestEnsureClonesThenUpdates` (`cache_test.go:175-216`) assert exact `Args`/`Category`/`Dir` for clone, update, and fetch-by-SHA, plus SHA validation rejection.
- Real-git behavioural assertions via `testutil.Repo` + `ExecRunner`: `TestEnsureRealGitNoCredentialInRemote` (`cache_test.go:265-293`) and `TestObjectReader` (`objects_test.go`) drive an actual bare mirror through `Ensure`/`Objects` and assert on real git output (parents, patch-id divergence, first-parent order, ancestry, rev-parse, branch existence) — this proves behavior, not just constructed argv.
- The concurrency test (`cache_test.go:218-263`) contends on one real, shared mirror path with 8 real goroutines and real git — analyzed under Job A above; it is not theatre.

## Strengths

- `Path` closes path traversal with a genuine allowlist plus per-segment `..`/empty checks (`cache.go:33-43`, `internal/gitx/validate.go:32-44`), and is unit-tested for both traversal and bad-provider-id cases.
- Credentials never enter argv, the persisted remote URL, or error/log strings; `TestEnsureRealGitNoCredentialInRemote` proves this against a real mirror rather than asserting it structurally only.
- Locking is held for the full clone-or-update decision and release is unconditional via `defer`; a real-git concurrency test proves it under actual contention rather than by code inspection alone.
- No direct `os/exec` usage; every git invocation goes through `gitx.Runner`.
- `FirstParentWalk` and all other reads are single bounded git invocations — no client-side unbounded loop exists anywhere in the package.
- Interface surface matches the brief exactly, byte-for-byte in places, and the one added test is a real proof, not padding.

## Issues

### Critical (Must Fix)

None found.

### Important (Should Fix)

- **`objects.go:55-56`** — `Exists` treats git exit code 128 identically to exit code 1 ("not found"), masking fatal/environment errors (corrupted mirror, unreadable directory, etc.) as an ordinary cache-miss `(false, nil)` instead of surfacing them as an error. This repeats the exact anti-pattern the brief itself calls out from Task 7's review. Recommend distinguishing "object not found" from other fatal conditions (e.g., inspect stderr content, or restrict the "not found" mapping to exit code 1 only and let 128 propagate as an error unless a specific known-benign stderr pattern is matched).

### Minor (Nice to Have)

- **`cache.go:45-46`** — `Lock` is exported and backed by a non-reentrant mutex (`internal/gitx/lock.go:8-13`); nothing in the doc comment warns a future caller not to call it and then call `Ensure`/`FetchSHA` for the same path on the same goroutine (self-deadlock). No misuse exists in this diff; flagging as a doc/API-hygiene gap for future consumers (Tasks 12/13/15), not a current defect.

## Assessment

**Task quality:** Approved.

**Reasoning:** Spec compliance is exact and the one added test is a genuine, real-contention proof rather than padding; path-traversal and token-containment — the two highest-value security checks for this package — both pass with direct evidence. The single Important finding (`Exists` collapsing exit-128 fatal errors into a benign "not found") is a real correctness gap but narrow in scope and does not block merge on its own; it should be fixed before this method is relied on for anything that distinguishes "commit truly absent" from "mirror is broken."

---

## Fix round 1 re-review (6114dd8..4ed960d)

### Finding verdict

**`Exists` fatal-error classification:** ADDRESSED — `apps/backend/internal/mirror/objects.go:47-66`. The command changed from `cat-file -e <sha>^{commit}` to `rev-parse --verify --quiet <sha>^{commit}` (objects.go:58), and the classification narrowed from `gitx.IsExit(err, 1) || gitx.IsExit(err, 128)` (old, both → `(false, nil)`) to `gitx.IsExit(err, 1)` only (objects.go:62-64); every other error, including all 128s, is now wrapped and returned (objects.go:65: `fmt.Errorf("exists %s: %w", sha[:7], err)`).

This is not the same defect moved elsewhere: the branch does not catch "any `*gitx.ExitError`" — it catches exactly one exit code (1) and lets everything else, including 128, fall through to the `return false, err` (now wrapped) path. Verified by reading the code directly, not taking the implementer's characterization on faith.

Exit-code mapping correctness for the actual command run: `rev-parse --verify --quiet <rev>` is documented git plumbing behavior — exit 1 with no stderr (due to `--quiet`) for "does not exist / not a valid ref," exit 128 with `fatal:` stderr for repository-level failures (bad `--git-dir`, corrupted object database, etc.). This matches the implementer's reported empirical observation and matches the `RevParse` method's pre-existing, already-audited use of the identical command (objects.go:137-146) with the identical `1`-only special case — internal consistency confirmed, not just claimed.

Credential-leak risk in the new error message: none. `fmt.Errorf("exists %s: %w", sha[:7], err)` wraps only a 7-char SHA prefix and the underlying `*gitx.ExitError`, whose `Error()` method (`apps/backend/internal/gitx/spec.go:50-52`) formats only `Category` and `ExitCode` — no stdout/stderr content is ever included. No new surface for a credential leak.

### Sibling-method claim

Independently verified against `apps/backend/internal/mirror/objects.go` (not from the report's prose):

- **`Parents`** (objects.go:68-81) — any non-nil error from `o.run` is wrapped and returned unconditionally (line 73-75); there is no boolean collapse in this method at all (it returns `([]string, error)`). Claim true.
- **`PatchID`** (objects.go:83-99) — both `show` and `patch-id` errors propagate unconditionally (lines 88-89, 92-94); the `out == ""` case at line 95-97 is reached only after a successful `patch-id` run with empty stdout, not after a swallowed error. Claim true.
- **`FirstParentWalk`** (objects.go:101-118) — `rev-list` error propagates unconditionally (line 109-111); no exit-code branching present. Claim true.
- **`IsAncestor`** (objects.go:120-135) — only `gitx.IsExit(err, 1)` maps to `(false, nil)` (line 131-133); everything else, including 128, falls to `return false, err` (line 134). Same one-exit-code-only shape as the fixed `Exists`. Claim true — this method was already correct and did **not** need the fix.
- **`RevParse`** (objects.go:137-146) — any error is wrapped and returned unconditionally (line 142-144); no boolean collapse (returns `(string, error)`). Claim true, and this is the pattern `Exists` was aligned to.
- **`BranchExists`** (objects.go:148-160) — only `gitx.IsExit(err, 1)` maps to `(false, nil)` (line 156-158); everything else, including 128, falls to `return false, err` (line 159). Same shape as the fixed `Exists`. Claim true — already correct, did not need the fix.

The "all already correct" claim holds under independent code reading, including for the two the finding asked to spot-check (`IsAncestor`, `BranchExists`): both distinguish their meaningful exit code (1) from everything else, and neither collapses fatal 128s into `false, nil`. Unlike the earlier task in this plan referenced by the prompt, this claim checks out.

### Test quality

Three tests added to `apps/backend/internal/mirror/objects_test.go`, all independently re-run (`go test -race -count=1 -run 'TestExists|TestObjectReader' ./internal/mirror/...` — 4 passed):

- `TestExistsAbsentObjectReturnsFalseNoError` (objects_test.go:108-132) — drives a real mirror via `Cache.Ensure` + real `ExecRunner`, queries a well-formed unknown SHA, asserts `(false, nil)`. Genuine end-to-end proof of the benign path against real git, not a mock.
- `TestExistsFatalGitErrorReturnsError` (objects_test.go:139-153) — uses `gitx.FakeRunner` to return `Result{ExitCode: 128}` wrapped in a real `*gitx.ExitError`, asserts `err != nil` and `ok == false`. This is not a vacuous assertion built on a hand-constructed error type the production code would never see: `objects.run()` (objects.go:35-45) returns whatever `o.runner.Run` returns unmodified, and `gitx.FakeRunner.Run` (gitx/fake.go:19) returns exactly what the test handler produces — so `Exists` receives the identical `*gitx.ExitError` shape it would receive from `gitx.ExecRunner` in production. The test genuinely drives the fatal branch, not a stand-in.
- `TestExistsGenuineNotFoundExitCodeReturnsFalseNoError` (objects_test.go:159-173) — same `FakeRunner` mechanism with `ExitCode: 1`, asserts `(false, nil)`, isolating the exit-1 classification from real git behavior.

Together the fatal (128) and genuine-not-found (1) tests exercise both branches of the narrowed `if gitx.IsExit(err, 1)` at objects.go:62, with the real-git test additionally proving the benign path end-to-end. No vacuous assertions found — each test's expected outcome differs from what the pre-fix code would have produced (the 128 test is precisely the regression case: old code returned `(false, nil)`, new code must return an error, and the test would have failed against the pre-fix `objects.go`).

### New breakage in the fix diff

None found at Critical or Important severity.

- `cache.go` change is a comment-only addition (Lock doc comment, cache.go:29-35) — no code path touched, cannot introduce a regression.
- `objects.go` change is confined to `Exists`; the other six `ObjectReader` methods are byte-identical to the pre-fix version (confirmed by reading the full file above) — no interface signature change, no incidental drift into sibling methods.
- `ObjectReader` interface (objects.go:14-22) unchanged — `Exists(ctx, sha string) (bool, error)` and all six siblings retain their exact pre-fix signatures, so Tasks 12/13/15 (written against this interface) are unaffected.
- Error message `fmt.Errorf("exists %s: %w", sha[:7], err)` (objects.go:65) — `sha` is validated by `gitx.ValidateSHA` (objects.go:48-50) as exactly 40 lowercase hex chars before this line, so `sha[:7]` cannot panic on a short string.
- `gitx.Runner` usage unchanged — no `os/exec` introduced (grep confirms no `os/exec`/`exec.Command` in the diff's changed lines).
- `.golangci.yml` not touched by this diff (not in the file list); implementer's "zero `//nolint` added" claim verified independently: `grep -n "nolint" apps/backend/internal/mirror/*.go` → no matches.

### Deferred minors

- `objects.go:65` error message includes only a SHA prefix and the underlying `ExitError`'s category/exit-code string — acceptable, but if `gitx.ExecRunner` is ever changed to include stderr in a wrapped error's `Error()` string, this and `RevParse`'s equivalent wrap (objects.go:143) would need re-review together. Not a current defect; noted for awareness only, out of scope for this round.
- Lock doc comment (cache.go:29-35) is prose-only and cannot be mechanically verified for deadlock accuracy beyond code reading; it correctly identifies `Ensure`/`FetchSHA` as the internal callers that take the lock (cache.go, unchanged in this diff) — accurate as written, no action needed.

### Assessment

**Fix round verdict:** All findings addressed.

**Reasoning:** The `Exists` classification is narrowed to precisely `gitx.IsExit(err, 1)`, matching the pre-existing, already-audited `RevParse`/`IsAncestor`/`BranchExists` pattern rather than reproducing the old defect under a new name; the sibling-method claim was independently verified against source (not accepted from the report) and holds for all six methods including the two spot-checked; the three new tests genuinely exercise both branches through the real production error path (`FakeRunner` returns the same `*gitx.ExitError` type `ExecRunner` would), with no vacuous assertions; and the diff introduces no new breakage to the unchanged `ObjectReader` interface or its five untouched sibling methods.
