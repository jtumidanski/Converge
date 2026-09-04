# Audit — Task 9: workspace manager (`internal/workspace`)

Diff reviewed: `4ed960d..ca6a409` (commit `ca6a409`), files:
`apps/backend/internal/workspace/manager.go` (175 lines, new)
`apps/backend/internal/workspace/manager_test.go` (176 lines, new)

## Path guards — can a session id escape WORKSPACE_ROOT?

**Verdict: no bypass found.** Every path that reaches `os.RemoveAll` or a worktree
removal is gated by `guard()` (manager.go:67-86), which runs `ValidateSessionID`
first (line 68) and, if the session directory exists, resolves it through
`filepath.EvalSymlinks` and requires exact equality — `filepath.Dir(real) != m.root
|| filepath.Base(real) != id` (line 82) — not a `strings.HasPrefix`/containment
check. Traced case by case:

- **`..` / `../x` segments**: `sessionIDRe = ^[0-9a-f]{8}$` (manager.go:19) rejects
  any non-hex character, so `..` can never survive `ValidateSessionID`, and
  `SessionDir(id)` (manager.go:55) is only ever constructed from an already-validated
  id in every caller (`guard`:68→71, `Create`:92→98, `RemoveDir`:164, `Cleanup`:127).
  Validated against real test cases (`RemoveDir("..")`, `RemoveDir("../x")`) —
  `manager_test.go:126-130`.
- **Absolute paths / leading `/`, `-`, `.`**: same regex rejects any character
  outside `[0-9a-f]` and any length ≠ 8, so an id can never itself be an absolute
  path or contain a separator.
- **Empty string**: fails the `{8}` length requirement in the regex — rejected at
  `ValidateSessionID`, before any filesystem call.
- **id containing a NUL byte**: NUL is not in `[0-9a-f]`, rejected by the regex.
- **id that's a prefix of a sibling dir name**: not reachable — the boundary check
  is `filepath.Base(real) != id`, an exact string comparison, not
  `strings.HasPrefix`. No such hole exists in this code.
- **Symlinked session dir pointing outward** (`<root>/<id>` → some other tree):
  caught by `guard()`'s `EvalSymlinks` + exact-equality check (manager.go:78-84).
  Verified end-to-end on a real filesystem by `TestCleanupRefusesEscapes`
  (`manager_test.go:335-374`): `Cleanup` and `RemoveDir` both return
  `ErrOutsideRoot`, and the target file outside root is confirmed still present
  afterward (asserts `RemoveAll` was never reached).
- **Symlinked `WORKSPACE_ROOT` itself**: `New` resolves it once via
  `filepath.EvalSymlinks(root)` (manager.go:44-47) and stores only the canonical
  path in `m.root`. All later joins/comparisons use that canonical value, so a
  symlinked root is handled consistently at construction. **Not exercised by any
  test** — no test constructs `New` with a symlinked root argument. Given the
  logic is straightforward (`EvalSymlinks` once, then string-compare everywhere),
  I judge the mechanism sound, but this specific case named in the review brief
  has zero test coverage.
- **`ValidateSessionID` before or after path construction?** In `guard()` it runs
  before `SessionDir(id)` is even called (line 68 validates, line 71 constructs).
  In `Create` it is checked explicitly at line 92, before `os.MkdirAll(m.SessionDir(id))`
  at line 98. In `RemoveDir` and `Cleanup` the only path construction is inside
  `guard()`, which validates first. So on every path, validation precedes path
  construction and any filesystem touch.

**One real gap, not a bypass but a TOCTOU window**: `Create` calls
`os.MkdirAll(m.SessionDir(id), 0o750)` (manager.go:98) **before** calling
`guard(id)` (manager.go:101). If a symlink already sits at `<root>/<id>` pointing
at an existing directory, `MkdirAll`'s internal `Stat` follows the symlink, sees
a directory, and no-ops — no write happens through it — and `guard()` immediately
afterward catches the mismatch and aborts before the mirror lock is taken or any
git command runs. So this ordering does not itself allow an escape. Separately,
`guard()` (Lstat + EvalSymlinks) and the later `os.RemoveAll(m.SessionDir(id))`
in `Cleanup`/`RemoveDir` are two distinct filesystem operations with a window
between them (manager.go:127→153, manager.go:164→171): if an attacker with
**local write access to WORKSPACE_ROOT** replaces `<root>/<id>` with a symlink
in that window, the guard's earlier check is stale. This requires an attacker who
already has write access to the workspace root (a different, narrower threat
model than "hostile session id from an HTTP client," which is the threat this
guard is built for) — flagged as Minor/theoretical, not Critical, since the
primary attack surface (client-controlled session id) is fully covered.

## Job A — Spec compliance

- ✅ `func New(root string, runner gitx.Runner, locks *gitx.LockMap, log *slog.Logger) (*Manager, error)` — creates root (`os.MkdirAll`, manager.go:41), resolves symlinks (`filepath.EvalSymlinks`, manager.go:44-47). Signature matches exactly — manager.go:40.
- ✅ `Root() string`, `SessionDir(id) string`, `RepoDir(id) string`, `BranchName(id) string` — manager.go:52, 55, 58, 61. `BranchName` returns `"review/" + id`, matching `review/<id>` — manager.go:61.
- ✅ `Create(ctx, mirrorPath, sessionID, baseSHA string) (repoDir string, err error)` — manager.go:91, signature matches exactly.
- ✅ `Cleanup(ctx, mirrorPath, sessionID string) error` — manager.go:126. Idempotent per `TestCreateCleanupRealGit`'s double-cleanup and empty-mirror assertions (manager_test.go:276-281, matching report lines 94-99) and `TestCleanupRefusesEscapes`'s escape assertions. Guarded (calls `guard()` first, manager.go:127, fails closed before touching the mirror or filesystem). FR-9.1 step order (`worktree remove --force` → `worktree prune` → `branch -D` → remove session dir, manager.go:138-140,153) is internally consistent with git semantics (a branch can't be deleted while checked out in a worktree, so removal must precede branch delete) — the FR-9.1 requirement text itself is not present in the diff or brief snippet supplied, so exact conformance to a numbered ordering spec is ⚠️ not fully verifiable from this diff alone; the implemented order is the only order that would actually work.
- ✅ `RemoveDir(sessionID string) error` — manager.go:163-175. Only calls `guard()` then `os.RemoveAll`; no mirror interaction, no git call — matches "guarded `os.RemoveAll` only."
- ✅ `var ErrOutsideRoot = errors.New("workspace: path is not a direct child of WORKSPACE_ROOT")` — manager.go:17, exact string match.
- ✅ `func ValidateSessionID(id string) error` (`^[0-9a-f]{8}$`) — manager.go:19, 22-27, regex matches exactly.
- ✅ Files: both `manager.go` and `manager_test.go` created exactly where the brief's `Files:` section specifies, no other files touched (diff stat: 2 files, 351 insertions, 0 deletions).
- ⚠️ Extra: `TestCreateSerializesPerMirror` (manager_test.go:290-333) was added beyond the brief's Step 1 snippet. It does not alter any produced interface or behavior — purely additive test coverage for the lock-serialization claim. Not a spec violation.

No missing or misunderstood requirements found; the implementation is essentially the brief's own snippet, re-typed.

## Job B — Audit

### Idempotent cleanup that still reports real failures — **Important, plan-mandated**

`Cleanup`'s three git steps (`worktree remove --force`, `worktree prune`,
`branch -D`) each swallow **any** runner error identically, at `Debug` level,
with no distinction between "already gone" (the intended idempotency case) and
a genuine failure — permission denied, a locked/busy worktree, or a corrupted
mirror:

```
manager.go:142-146
for _, st := range steps {
    if _, runErr := m.runner.Run(ctx, gitx.Spec{...}); runErr != nil {
        m.log.Debug("cleanup step failed", ...)
    }
}
```

`Cleanup` then returns `nil` as long as the final `os.RemoveAll(SessionDir(id))`
succeeds (manager.go:153-157) — regardless of whether the git-level steps
actually succeeded. This is exactly the swallowing pattern flagged in Task 7
(`FileContent`) and Task 8 (`Exists`): a genuine git failure at the worktree/branch
level is indistinguishable, in the return value, from "already cleaned up."
Concrete failure mode: if `branch -D` fails for a real reason (line 140/144) and
is silently logged at Debug, a later `Create` reusing the same session id will
fail at `git worktree add -b review/<id> ...` (manager.go:130) because the
branch still exists — surfacing confusingly, disconnected from the `Cleanup`
call that actually caused it, and only after the caller already believed cleanup
succeeded.

This is the brief's own design (the Step 3 code snippet in the task brief,
lines 254-288, does exactly this) — labeled **plan-mandated** per the review
instructions: the brief's authorship doesn't downgrade the finding. The
implementer's report (lines 129-130) explicitly acknowledges considering and
rejecting surfacing these errors, citing the idempotency contract; that
rationale is a claim, not evidence, and doesn't change that a real failure here
is unobservable to the caller.

The one thing this package does get right on this axis: `os.RemoveAll` failure
on the session directory itself (the operation that actually deletes files) *is*
surfaced as a real error (manager.go:153-157, `Warn` + wrapped error return),
correctly distinguishing "directory already absent" (`!exists` → nil, line
150-152) from an actual `RemoveAll` failure.

### Locking — **Important**

`Create` takes the mirror lock and releases it with `defer` (manager.go:104-105):
panic-safe, released on every path.

`Cleanup` takes the same lock but releases it with a **plain call, not deferred**
(manager.go:133 lock, 147 unlock — the `unlock()` call sits after the steps loop,
inside the same `if` block, with no `defer`). If `m.runner.Run` were to panic
inside the loop (not merely return an error), `unlock()` at line 147 would never
execute, leaking the per-mirror lock permanently and deadlocking every future
`Create`/`Cleanup` call against that mirror. This violates "released
unconditionally on every path including errors and panics" — `Create` meets that
bar, `Cleanup` does not. No test exercises a panicking runner to catch this.

No pointer/state escape found: `unlock` is a local closure in both functions,
never stored on `Manager` or returned — matches the Task 5 lesson about pointers
escaping a mutex.

### All git execution through `gitx.Runner` — PASS

Import block (manager.go:4-14) contains no `os/exec`; every git invocation goes
through `m.runner.Run(...)` (manager.go:113, 143) using `gitx.Spec`. `os` is
imported only for directory operations (`MkdirAll`, `Lstat`, `RemoveAll`, `Stat`),
not process execution.

### Test depth — **Important**

Three test functions for a component whose failure mode is arbitrary filesystem
deletion, per the brief:

- `TestCreateCleanupRealGit` (manager_test.go:224-282): real-git, end-to-end.
  Genuinely proves deletion happened, not just a nil return — checks
  `os.Stat`/`IsNotExist` on the session dir, `git branch --list review/*` is
  empty, `git worktree list --porcelain` count drops to 1, and `git fsck` on the
  mirror succeeds afterward (mirror not damaged). This is solid, not a token
  check.
- `TestCreateSerializesPerMirror` (manager_test.go:290-333): proves the lock
  actually serializes concurrent `Create` calls against the same mirror (max
  in-flight == 1), addressing the Task 5 lesson. Good addition, not required by
  the brief.
- `TestCleanupRefusesEscapes` (manager_test.go:335-374): covers a real hostile
  case (symlinked session dir escaping root, for both `Cleanup` and `RemoveDir`)
  and five malformed-id strings (`"..", "../x", "abc", "ABCDEF01", "0123456789"`),
  plus one positive real-deletion case.

Gaps in the hostile-input space named explicitly in this review's own priority
list, not covered by any test:
- **Symlinked `WORKSPACE_ROOT` itself** — no test constructs `New` with a root
  that is a symlink and then verifies guard comparisons still hold against the
  canonical path.
- **No test exercises a genuine (non-"already gone") failure from the runner
  during `Cleanup`'s git steps** — nothing asserts that a real failure is
  surfaced differently from "already removed," which is precisely the defect
  class flagged above and the one this review was told to scrutinize hardest.
  A `gitx.FakeRunner` returning a distinctive error for `worktree remove` would
  have been trivial to add given the harness already exists in this same file.
  Its absence is a coverage gap, not just a "could be broader" nit, given what
  this code does.
- **NUL byte / non-UTF8 id, id containing `/` directly (not just `..`)** —
  implicitly covered by the regex logic but never explicitly asserted as its own
  case the way `".."` and `"../x"` are.
- No test for a lock-panic scenario (see Locking finding above) — understandably
  hard to test cleanly, but the design gap it would have caught went unnoticed.

This is not "three functions with one assertion each" — the real-git test in
particular is thorough — but the specific hostile-input space this review was
asked to weight most heavily (symlinked root, genuine-failure-during-cleanup) has
concrete, nameable gaps.

## Strengths

- Guard logic (manager.go:67-86) uses exact path-boundary comparison, not a
  prefix/containment check — the classic hole named in the review brief is
  correctly avoided.
- `ValidateSessionID` runs before any path construction on every call path.
- `Create`'s lock is released with `defer`, panic-safe.
- `os.RemoveAll` failure on the session directory (the actual delete op) is
  surfaced as a real, wrapped error rather than swallowed — correctly following
  the Task 7/8 lesson for that one code path.
- Real-git test proves actual deletion (`git fsck`, `branch --list`, `worktree
  list`), not just a nil return.
- No `os/exec` in the package; all git execution goes through `gitx.Runner`.
- No `//nolint` directives added.

## Issues

### Critical (Must Fix)
None found. No confirmed path-guard bypass.

### Important (Should Fix)
- **Cleanup's git-step failures are unconditionally swallowed as Debug logs**
  (manager.go:142-146), indistinguishable from the intended "already gone" case
  — plan-mandated (brief's Step 3 snippet does the same), but a real defect per
  this review's own stated priority. A distinct signal (sentinel error type, or
  checking whether the failure is a genuine "not found"/"already deleted" case
  vs. anything else) is needed before this can be trusted at the reliability bar
  the brief sets ("idempotent cleanup that still reports real failures").
- **`Cleanup`'s lock release is not deferred** (manager.go:133, 147) — a panic
  inside the git-steps loop leaks the per-mirror lock permanently, unlike
  `Create`'s `defer unlock()` (manager.go:104-105).
- **Untested hostile-input cases named explicitly in this review's scope**:
  symlinked `WORKSPACE_ROOT` itself, and a genuine (non-benign) runner failure
  during `Cleanup`'s git steps. Both are directly relevant to the failure mode
  this package's own tests otherwise take seriously.

### Minor (Nice to Have)
- TOCTOU window between `guard()`'s symlink check and the later
  `os.RemoveAll` call in `Cleanup`/`RemoveDir` — only exploitable by an attacker
  who already has local write access to `WORKSPACE_ROOT`, a different threat
  model than the client-controlled-session-id surface this guard is built for.
- `Create` calls `os.MkdirAll(SessionDir(id))` before `guard(id)` runs
  (manager.go:98, 101) — verified harmless (guard catches any symlink mismatch
  before the lock or any git call), but the ordering is easy to misread as
  "write before validate."

## Assessment

**Task quality:** Needs fixes

**Reasoning:** The path guard itself — the thing this review was told to weight
above everything else — holds against every hostile-input case traced, including
the classic prefix-containment hole, and is backed by a real-filesystem test.
But `Cleanup`'s uniform swallowing of git-step errors (plan-mandated) and its
non-deferred lock release are concrete reliability/robustness defects matching
patterns this same review series has caught twice before in adjacent tasks, and
the test suite does not exercise either.
