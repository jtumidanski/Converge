# Task 24 Fix Round 1 Re-Review — Scoped Audit

**Commit:** `8b71cd3` — fix(task-001): match repository schema interior-segment rejection to backend validator

**Scope:** Changed files only (`.superpowers/sdd/plan/review-9836c9c..8b71cd3.diff`), not full task re-audit.

**Date:** 2026-09-05

**Build & Test:** PASS

## Executive Summary

Finding 1 (Important) and Finding 2 (Minor) from the original audit are both correctly addressed:

1. **Finding 1 — schema more permissive than backend:** The new interior-segment refine mirrors `ValidateRepoFullName`'s per-segment loop exactly. Both directions verified: all 3 backend-accepted cases still pass; all 13 backend-rejected cases still fail. Mutation verification confirms: removing the fix causes exactly 2/18 tests to fail (both targeting the interior-dot gap).

2. **Finding 2 — no schema test file:** Test file added (`repository.test.ts`, 18 tests) with fixtures lifted directly from `validate_test.go:22-32`. Parity is pinned across all backend test cases plus two explicit cases for the fixed gap.

**No new Critical or Important breakages introduced by this fix diff.** All 62 tests pass; build, lint, and typecheck clean.

## Build & Test Results

**Baseline verification (before any mutation or change):**
- `npm test`: **11 test files, 62 tests, all passed** (was 10 files / 44 tests before fix round 1; this fix adds 1 file with 18 tests)
- `npm run lint`: clean (0 output)
- `npx tsc --noEmit -p tsconfig.app.json`: clean
- `npm run build`: succeeded (frontend dist built and restored to `.gitkeep`)
- Backend gate: all passes (`go test`, `go vet`, `golangci-lint`, `go build`)

## Finding 1: Schema ↔ Backend Validator Parity

### Claim: Schema Now Mirrors Backend in Both Directions

**Backend contract** (`apps/backend/internal/gitx/validate.go:32-45`):
```go
func ValidateRepoFullName(s string) error {
  if !repoRe.MatchString(s) { ... }
  if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, ".") { ... }
  for _, seg := range strings.Split(s, "/") {
    if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, "..") { ... }
  }
  return nil
}
```

**Backend test fixtures** (`validate_test.go:23-28`):
- **Accepts (3 cases):** `["owner/repo", "group/sub/project", "a.b/c-d_e"]`
- **Rejects (13 cases):** `["", "repo", "/owner/repo", "-owner/repo", ".owner/repo", "owner/../repo", "owner/./repo", "owner//repo", "owner/repo/", "owner/repo name", "owner/repo\x00", "owner/re\npo", "../x/y"]`

**Frontend schema** (`apps/frontend/src/lib/schemas/repository.ts:4-16`):
- Line 8: Regex `^[A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)+$` — identical to backend
- Line 9: `.refine((value) => !value.includes(".."), ...)` — rejects any ".." substring in the whole string
- Line 10: `.refine((value) => !/^[-./]/.test(value), ...)` — rejects leading `-`, `.`, `/`
- Lines 11-15: **NEW** `.refine((value) => value.split("/").every((segment) => segment !== "" && segment !== "." && segment !== ".."), ...)` — rejects empty segments, single-dot segments, double-dot segments

### Direction 1: Backend-Accepted → Frontend Must Accept

**Test verification:** `repository.test.ts:29-32` uses `it.each(BACKEND_ACCEPTS)` with `expect(result.success).toBe(true)`.

All three backend-accepted cases are in the test and are verified to pass:
- `"owner/repo"` ✓
- `"group/sub/project"` ✓
- `"a.b/c-d_e"` ✓

**No capability loss.** All inputs the backend accepts still pass client-side.

### Direction 2: Backend-Rejected → Frontend Must Reject

**Test verification:** `repository.test.ts:34-37` uses `it.each(BACKEND_REJECTS)` with `expect(result.success).toBe(false)`.

All 13 backend-rejected cases are in the test. Manual trace:

| Rejection Reason | Examples | Caught by Frontend Refine |
|---|---|---|
| Empty string | `""` | `.min(1, ...)` (line 7) |
| No `/` separator | `"repo"` | Regex (line 8) |
| Leading `/` | `"/owner/repo"` | Refine line 10 |
| Leading `-` | `"-owner/repo"` | Refine line 10 |
| Leading `.` | `".owner/repo"` | Refine line 10 |
| Contains `..` substring | `"owner/../repo"` | Refine line 9 OR refine line 11-15 (segment `".."`) |
| Contains interior `.` segment | `"owner/./repo"` | Refine line 11-15 (segment `"."`) — **THE FIX** |
| Empty segment (double `/`) | `"owner//repo"` | Regex (line 8) and refine line 11-15 |
| Trailing `/` (empty segment) | `"owner/repo/"` | Regex (line 8) and refine line 11-15 |
| Contains space | `"owner/repo name"` | Regex (line 8) |
| Contains null byte | `"owner/repo\x00"` | Regex (line 8) |
| Contains newline | `"owner/re\npo"` | Regex (line 8) |
| Leading `..` (double-dot segment) | `"../x/y"` | Refine line 10 (leading `.`) |

**Every rejection is caught.** No inputs the backend rejects slip through the frontend.

### Status: Finding 1 ✅ ADDRESSED

The fix correctly adds the missing per-segment interior-validation loop, mirroring `ValidateRepoFullName:38-42` exactly. Schema is now strictly equivalent to backend in both directions.

## Finding 2: Missing Schema Test File

### Claim: Test File Now Exists and Pins Backend-Parity

**Test file:** `apps/frontend/src/lib/schemas/__tests__/repository.test.ts` (48 lines)

**Parity fixtures:** Lines 10-26 define `BACKEND_ACCEPTS` and `BACKEND_REJECTS` lifted directly from `apps/backend/internal/gitx/validate_test.go:23-28`:
- Fixtures are byte-for-byte identical to backend test (3 accept + 13 reject cases)
- Not hand-picked examples, but the actual backend-test ground truth

**Test structure:**
- Lines 29-32: `it.each(BACKEND_ACCEPTS)` — 3 tests that each accepted backend case also passes frontend
- Lines 34-37: `it.each(BACKEND_REJECTS)` — 13 tests that each rejected backend case also fails frontend
- Lines 39-42: Explicit case `"owner/./name"` (the primary gap fixed this round)
- Lines 44-47: Explicit case `"owner/../name"` (interior `..`, to confirm both `.` and `..` are caught)

**Total: 18 tests** (3 + 13 + 2 explicit)

### Test Execution

All 18 tests pass in the baseline run:
```
✓ src/lib/schemas/__tests__/repository.test.ts > repositorySchema > 
  accepts owner/repo, matching the backend validator
✓ src/lib/schemas/__tests__/repository.test.ts > repositorySchema > 
  accepts group/sub/project, matching the backend validator
... (3 passes total for BACKEND_ACCEPTS)
✓ src/lib/schemas/__tests__/repository.test.ts > repositorySchema > 
  rejects , matching the backend validator
... (13 passes total for BACKEND_REJECTS)
✓ src/lib/schemas/__tests__/repository.test.ts > repositorySchema > 
  rejects an interior '.' segment (the gap fixed on this round)
✓ src/lib/schemas/__tests__/repository.test.ts > repositorySchema > 
  rejects an interior '..' segment
```

### Status: Finding 2 ✅ ADDRESSED

Test file exists with backend-parity fixtures and explicit gap-coverage cases. No hand-picked token examples—uses the actual backend test ground truth.

## Mutation Verification (Contract 6)

**Mandate:** Remove the new interior-segment refine (lines 11-15) and confirm test reachability.

**Applied in scratch copy (`/tmp/task24-fix-check`)**, never in worktree. Mutation:
```diff
- .refine(
-   (value) =>
-     value.split("/").every((segment) => segment !== "" && segment !== "." && segment !== ".."),
-   "Path segments cannot contain ..",
- ),
```

**Result:**
```
Test Files  1 failed (1)
     Tests  2 failed | 16 passed (18)
```

**Failures (by line number in test file):**

| Test | Location | Error |
|---|---|---|
| `rejects owner/./repo, matching the backend validator` | `repository.test.ts:36` | `AssertionError: expected true to be false // Object.is equality` |
| `rejects an interior '.' segment (the gap fixed on this round)` | `repository.test.ts:41` | `AssertionError: expected true to be false // Object.is equality` |

**Analysis:**
- Mutant typechecks (`npx tsc --noEmit` clean, exit 0)
- Mutant runs (`vitest` executed full suite, no compile error)
- Exactly 2 tests fail (both target interior-dot gaps)
- Other 16 tests pass (base regex, leading-char, whole-string `..` substring, interior `..` exact-segment checks all still caught by remaining refines)
- **Mutations are isolated; mutant is not simply broken code**

**Reachability before mutation:** All 18 tests pass in the unmodified worktree, confirming both failed assertions were reachable before mutation — satisfies Contract 6(f).

**Worktree status:** `git status --porcelain` shows clean; no mutations leaked into the worktree.

### Status: Mutation Verification ✅ CONFIRMED

The fix is reachable and necessary. Exactly 2 of 18 tests distinguish the new interior-segment refine, matching the report's claim.

## Message Reuse Assessment

**The new refine uses:** `"Path segments cannot contain .."`

**The new refine catches:** Empty segments (`""`), single-dot segments (`"."`), and double-dot segments (`".."`)

**Issue:** The message says "cannot contain .." but also rejects single-dot segments. User entering `owner/./name` sees "Path segments cannot contain .." but typed a single dot, not `..`.

**Instruction context:** User's prior directive allowed reusing existing FR-10.11 vocabulary but permitted new copy "where the existing messages genuinely cannot cover the case." The implementer chose to reuse the existing message.

**Assessment:** This is technically acceptable reuse within the vocabulary constraint, but it represents a UX imprecision. The message is semantically related (both `.` and `..` are path-traversal hazards) but literally inaccurate for the single-dot case. Future work should consider:
- Adding new vocabulary (`"Path segments cannot be . or .."`) if FR-10.11 scope permits
- Or refactoring the refine to use separate messages for each rejection type

**Verdict:** Not a blocking finding. The fix follows the stated vocabulary constraint, and the message is defensible in context (path-segment validation). But it's suboptimal from a UX clarity standpoint.

## Summary: New Critical/Important Breakages from Fix Diff

**None detected.**

The fix correctly addresses Finding 1 (schema divergence) and Finding 2 (missing test). No new validation failures, no new test breakages, no new type errors. All 62 tests pass; build and lint clean.

The one observation (message imprecision for single-dot segments) is not a blocking issue—it's a UX refinement opportunity deferred to future work.

---

## Audit Checklist

| Item | Status | Evidence |
|---|---|---|
| Build passes | ✅ | `npm run build` succeeded |
| All tests pass | ✅ | 62 tests in 11 files, all passed |
| Lint clean | ✅ | `npm run lint` — 0 output |
| TypeScript clean | ✅ | `npx tsc --noEmit` — no errors |
| Finding 1 addressed | ✅ | Schema mirrors backend in both directions; fixtures verified |
| Finding 2 addressed | ✅ | Test file exists with backend-parity fixtures; 18 tests |
| Mutation reachable | ✅ | 2/18 tests fail when interior-segment refine removed |
| No new breakages | ✅ | All tests pass in current state |

**Overall Verdict: PASS**

The fix round 1 is complete, correct, and ready for merge.
