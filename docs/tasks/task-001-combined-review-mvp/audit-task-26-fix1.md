# Task 26 Fix Round 1 — Re-review

**Scope:** re-review of fix round 1 for Task 26 (`ReviewPage`/status handling/file tree/file diff), frontend only.
**Commit range reviewed:** `3c02e80..2776c0b` (fix diff: `.superpowers/sdd/plan/review-3c02e80..2776c0b.diff`). Single commit: `2776c0b` "fix(task-001): test ReviewPage error branches, handle terminal statuses, drop hardcoded colours".
**Date:** 2026-09-05.

This is a scoped re-review: each open finding is verdicted ADDRESSED/NOT ADDRESSED against the fix diff only. New breakage is only in scope if introduced by this diff. Untouched code is noted in the Deferred section and does not extend the loop.

## ReviewHeader.tsx — confirmed untouched; overturned finding not re-raised

`git diff 3c02e80..2776c0b -- apps/frontend/src/components/features/review/ReviewHeader.tsx` is empty, and the fix diff's file list (`FileDiff.tsx`, `FileTree.tsx`, `strings.ts`, `ReviewPage.tsx`, `ReviewPage.test.tsx`) does not include `ReviewHeader.tsx` or any `ReviewHeader` test. No test file for `ReviewHeader` exists in the repo at all (unchanged from before). Per the senior reviewer's ruling R54, the prior Critical finding on `ReviewHeader.tsx:21` (base SHA in the always-visible header) is **not re-raised** here.

One-time, non-blocking commentary (not a finding, does not affect the verdict): the ruling's reasoning is sound — FR-10.8 (`prd.md:370`) explicitly mandates "base branch @ short SHA" in the diff view header, and the manual acceptance checklist (`prd.md:730`) independently confirms "shows base branch @ SHA" as an acceptance criterion. FR-10.11's banned-vocabulary list (`prd.md:381-383`) names "worktree, cherry-pick, or synthetic branch," not "SHA," and FR-10.7 places base SHA in Diagnostics for the *error panel* specifically, a different surface than FR-10.8's header. The ruling is correctly grounded in the PRD text as written.

## Finding 1 (Important) — untested error branches: **ADDRESSED**

Three new tests were added to `ReviewPage.test.tsx`, each discriminating its own branch:

- `apps/frontend/src/pages/__tests__/ReviewPage.test.tsx:207-218` — file-list fetch error. Asserts `findByText(/could not load the file list/i)` present AND `queryByText(/no file changes/i)` absent, ruling out the sibling empty-list state.
- `apps/frontend/src/pages/__tests__/ReviewPage.test.tsx:220-227` — review fetch error. Asserts `findByText(/could not load this review/i)` present AND `queryByRole("button", {name: /finish review/i})` absent, ruling out the diff-layout happy path.
- `apps/frontend/src/pages/__tests__/ReviewPage.test.tsx:229-253` — single file-diff fetch error. Asserts `findByText(/could not load this file's diff/i)` present AND `queryByTestId("file-diff")` absent, ruling out the stale-diff-render state.

Each assertion pairs a positive (branch-specific error text) with a negative (the visually-adjacent alternative's affordance), so a mutant that silently swallows the error into a different but still "non-empty" render would still be caught. Verified structurally correct via mutation testing below (Mutation A).

`ReviewPage.tsx:65-75` (review-fetch `isError`), `:137-142` (files-fetch `isError`), `:163-168` (fileDiff `isError`) are the three branches now covered.

## Finding 2 (Important, ruling R55) — non-exhaustive status switch: **ADDRESSED**

`ReviewPage.tsx:88-183` replaces the prior `if`-chain with `switch (currentStatus)` over `currentStatus: ReviewStatusValue = review.data.attributes.status` (non-optional, since `review.isError`/`!review.data` guards at lines 65-83 already returned). Cases: `CREATING` (91-96), `CONFLICTED`|`FAILED` (98-108), `FINISHED`|`EXPIRED` (115-125, new terminal panel), `READY` (127-179, diff layout), `default: return assertUnreachable(currentStatus)` (181-182).

`assertUnreachable(status: never): never` (`ReviewPage.tsx:27-29`) throws at runtime but its real job is the type-level one: the `default` branch only type-checks if every prior case has narrowed `currentStatus` to nothing. No `default: return <DiffLayout/>`, no `as ReviewStatus` cast, and no bare `default:` fallback to the happy path exists — confirmed by reading the code directly.

**Exhaustiveness proven structurally** (not just read): in a scratch copy, added a 7th bogus member `"BOGUS7"` to the `ReviewStatus` union at `apps/frontend/src/types/models/review.ts:3` and ran `npx tsc --noEmit -p tsconfig.app.json`. Result:
```
src/pages/ReviewPage.tsx(182,32): error TS2345: Argument of type '"BOGUS7"' is not assignable to parameter of type 'never'.
TypeScript: 1 errors in 1 files
```
This is a genuine compile error pointing at the `assertUnreachable(currentStatus)` call site — a 7th status is a compile-time failure, exactly as claimed. Reverting the union member restored a clean `tsc --noEmit`.

The terminal panel (`ReviewPage.tsx:117-125`) renders `EmptyState` with `strings.reviewUnavailableTitle`/`reviewUnavailableDescription` and a `Button` that calls `navigate("/")` — no `Finish Review` button in this branch (see below).

## Finding 3 (Minor) — non-semantic colours: **ADDRESSED**

- `FileTree.tsx:64`: `text-emerald-600` → `text-muted-foreground` (semantic token). Deletions span already used `text-destructive`.
- `FileDiff.tsx:24`: `border-amber-500/40 bg-amber-500/10` / `text-foreground` → `border-border bg-muted` / `text-muted-foreground` (semantic tokens matching the binary-file message's existing style one block above).

`grep -n "emerald\|amber" apps/frontend/src/components/features/review/FileTree.tsx apps/frontend/src/components/features/review/FileDiff.tsx` returns nothing. No new theme colors were introduced anywhere in the diff (only pre-existing semantic Tailwind/shadcn tokens used).

## Mutation testing detail (scratch copy at `/tmp/task26-fix1-scratch`, a `cp -r` of `apps/frontend` outside the worktree, deleted after use)

Baseline confirmed green before each mutation: `npx vitest run src/pages/__tests__/ReviewPage.test.tsx` → 10/10 passed.

### Mutation A — bypass the file-list `isError` branch

Applied `sed -i '137s/files.isError ? (/false \&\& files.isError ? (/'` to `src/pages/ReviewPage.tsx`. Grep-confirmed landed: line 137 read `{false && files.isError ? (`. `npx tsc --noEmit -p tsconfig.app.json` produced no output (clean — a boolean short-circuit doesn't change types).

Ran `npx vitest run src/pages/__tests__/ReviewPage.test.tsx`: **1/10 failed**, quoted failure:
```
TestingLibraryElementError: Unable to find an element with the text: /could not load the file list/i.
❯ src/pages/__tests__/ReviewPage.test.tsx:215:25
```
Rendered DOM under the mutation showed an empty `<nav aria-label="Changed files">` (self-closing, no `<li>` rows) and an empty `<section />` — i.e. exactly "fell through to `FileTree` with an empty list," not some unrelated diff. Distinguishing from a legitimate empty-file-list state: a real empty list (`listDoc([])`, used at `ReviewPage.test.tsx:180` in the "finishes the review" test) also renders an empty `FileTree` and empty section — this is expected, since the test's assertion is a direct `findByText` for the *error* copy, not an inference from absence-of-content. The mutation kills the test specifically because the error-banner text (`"Could not load the file list"`) never renders under the mutation, which is the exact behavior the test exists to pin; it does not additionally fail on any unrelated DOM difference (only the one `expect` at line 215 threw; the assertion following it at line 217, checking `/no file changes/i` absence, was never reached because the test throws at line 215 first). Reverted via the inverse sed; grep-confirmed line 137 restored to `files.isError ? (`.

### Mutation B — FINISHED falls through to the diff layout

Moved the `case "FINISHED":` label out of the `FINISHED | EXPIRED` group and into the `READY` group (`case "FINISHED": case "READY": return (...)`), leaving `case "EXPIRED":` alone in the terminal-panel group. Grep-confirmed: `case "EXPIRED":` alone at line 115, `case "FINISHED":` immediately followed by `case "READY":` at lines 126-127. `npx tsc --noEmit -p tsconfig.app.json` clean (moving a case preserves exhaustiveness since every member is still handled somewhere).

Ran the test file: **1/10 failed**, quoted failure:
```
TestingLibraryElementError: Unable to find an element with the text: /no longer available/i.
❯ src/pages/__tests__/ReviewPage.test.tsx:260:25
```
Rendered DOM under the mutation showed the full diff layout — `ReviewHeader` with "Included PRs/MRs", and a `<section>` with a loading skeleton — for a `FINISHED` review, confirming the mutant genuinely reproduces "FINISHED renders the diff layout." Reverted; grep-confirmed original case grouping restored (`case "FINISHED": case "EXPIRED":` together, `case "READY":` separate).

### Mutation C — colour (0/12 coverage-gap claim)

Restored `text-emerald-600` in `FileTree.tsx:64` and `border-amber-500/40 bg-amber-500/10`/`text-foreground` in `FileDiff.tsx:24` (the pre-fix values). Ran `npx vitest run` (full suite, not just the two component test files, to give the claim its best chance of being falsified): **94/94 passed, 0 failures.** Also confirmed directly by reading `FileTree.test.tsx` and `FileDiff.test.tsx`: neither file contains any `className`, `toHaveClass`, colour, or snapshot assertion (`grep -n "emerald|amber|className|toHaveClass|color"` on both files returns no matches). The implementer's claim that this is an honest, undisguised coverage gap on a purely cosmetic change — not a fabricated test — is confirmed. No color test is requested here per the task's explicit instruction that this is out of scope.

## Collateral-damage checklist on the ~179-line ReviewPage.tsx restructure

- **2-second poll while CREATING, stops on terminal status:** unchanged — polling lives in `apps/frontend/src/lib/hooks/api/useReviews.ts:15-27` (`useReview`), not in `ReviewPage.tsx`, and was not touched by this diff. `refetchInterval` returns `2000` while `!isTerminal(...)` else `false` (`useReviews.ts:22-25`); `isTerminal` (`apps/frontend/src/types/models/review.ts:66-68`) returns `status !== "CREATING"`, so `READY`/`CONFLICTED`/`FAILED`/`FINISHED`/`EXPIRED` all stop polling. Confirmed intact via `git diff 3c02e80..2776c0b -- apps/frontend/src/lib/hooks/api/useReviews.ts apps/frontend/src/types/models/review.ts` — empty diff, neither file touched.
- **CONFLICTED/FAILED still route to ReviewErrorPanel:** `ReviewPage.tsx:98-108`, unchanged content moved verbatim into the switch case.
- **Auto-select-first-file is still a render derivation, doesn't clobber explicit selection:** `ReviewPage.tsx:44-46` — `const [explicitPath, setExplicitPath] = useState<string | undefined>(undefined); const selectedPath = explicitPath ?? files.data?.[0]?.attributes.path;` — no `useEffect`, `grep -n useEffect ReviewPage.tsx` returns nothing. Untouched by this diff (same lines existed pre-fix, just renumbered).
- **READY still reaches the diff layout:** `ReviewPage.tsx:127-179`, content moved verbatim (same `ReviewHeader`/file-tree/file-diff grid) into the `case "READY":` branch.

No collateral damage found in the restructure; the 179-line diff is a mechanical `if`-chain → `switch` conversion plus one new case group, not a functional rewrite of the surrounding logic.

## Strings / i18n check

`apps/frontend/src/lib/strings.ts:15-17`:
```ts
reviewUnavailableTitle: "This review is no longer available",
reviewUnavailableDescription: "Start a new review to continue.",
startNewReview: "Start a new review",
```
Exact keys/values match the spec. All three are referenced, not orphaned: `strings.reviewUnavailableTitle` and `strings.reviewUnavailableDescription` at `ReviewPage.tsx:120-121` (passed to `EmptyState`), `strings.startNewReview` at `ReviewPage.tsx:123` (`Button` label).

Grepped the fix diff for new hardcoded JSX text: the only quoted `title=`/`description=` string literals added by this diff (`"Could not load the file list"`, `"No file changes"`, `"...net change."`, `"Could not load this file's diff"`) are pre-existing lines reindented into the new `switch` case (confirmed identical against `git show 3c02e80:apps/frontend/src/pages/ReviewPage.tsx`), not new literals introduced by this round. The one genuinely new user-facing surface (terminal panel + button) correctly routes through `strings.ts`. No new hardcoded string literal was introduced.

## Finish Review button reachability

`grep -n "Finish Review\|ReviewHeader"` on `ReviewPage.tsx` shows `ReviewHeader` (the only component that renders a "Finish Review" button, via `strings.finishReview` at `ReviewHeader.tsx:27`) is invoked exactly once, at `ReviewPage.tsx:130`, inside `case "READY":` (lines 127-179). It is not reachable from `CREATING`, `CONFLICTED`/`FAILED`, or `FINISHED`/`EXPIRED` branches. The `Start a new review` button/route in the unavailable panel (`ReviewPage.tsx:123`) calls `navigate("/")` using react-router's `useNavigate` (`ReviewPage.tsx:2,38`) — the same real-navigation pattern already used by `closeReview()` (`ReviewPage.tsx:59`) elsewhere in this file, not a dead `href="#"`.

## Lint / format / build gate (real worktree, not scratch)

- `npm run lint` (`eslint .`, repo-wide): **clean, no output.**
- `npm run format:check` (`prettier --check .`, repo-wide): **fails, exit 1**, exactly the 3 pre-existing files claimed by the implementer and by the round-0 audit — `src/components/features/changes/ChangeTable.tsx`, `src/pages/__tests__/SelectChangesPage.test.tsx`, `src/pages/SelectChangesPage.tsx`. None of these are touched by the fix diff (`git diff 3c02e80..2776c0b --stat` does not list them). Matches the claim exactly.
- Full gate: `npm test` → **19 test files, 94 tests, all passed.** `npm run lint` → clean (as above). `npm run build` → succeeded (`tsc -b && vite build`), deleted `apps/backend/internal/ui/dist/.gitkeep` as expected; restored via `git checkout -- apps/backend/internal/ui/dist/.gitkeep` immediately after. `git status --porcelain` confirmed clean before finishing (aside from this new audit file being added below).

## Deferred (untouched code, out of scope for this round)

- `ReviewHeader.tsx` still has no dedicated test file; several of its own branches (`baseSha` null fallback, no `baseDescription`, no `totals`, multiple `included` entries, `finishing` spinner) remain untested — carried from the original audit, untouched by this fix round.
- `previousPath` is still never rendered anywhere in `FileTree.tsx`/`FileDiff.tsx` (no rename display exists) — carried from the original audit.
- `ReviewPage.tsx` still has several hardcoded English string literals (`"Could not load the file list"`, `"Could not load this review"`, `"No file changes"`, `"Could not load this file's diff"`, and their `detail`/`description` companions) that bypass `strings.ts`; these predate this fix round (confirmed identical in `3c02e80`) and are not part of the three findings in scope, so not graded here.
- The mutation-4 ratio bookkeeping correction (`1/6` → `1/5`) from round 0 is noted as done in the report's "Bookkeeping correction" section; not independently re-verified here since it's a documentation-only correction with no associated code change in this diff.

## Overall verdict

**Fix round 1 is sufficient to close Task 26.** All three open findings (untested error branches, non-exhaustive status switch, non-semantic colours) are ADDRESSED, each independently verified — not merely re-read from the implementer's report — via direct code inspection, mutation testing in an isolated scratch copy, and a structural exhaustiveness proof (adding a 7th `ReviewStatus` member causes a genuine `tsc` compile error at the `assertUnreachable` call site). The overturned Critical finding on `ReviewHeader.tsx` was correctly left untouched and is not re-raised. No collateral damage was found in the ~179-line restructure across polling, CONFLICTED/FAILED routing, auto-select-derivation, or the READY diff layout. Lint is clean repo-wide, format:check fails only on the same 3 pre-existing untouched files as before, and the full `test`/`lint`/`build` gate passes (94/94 tests, clean lint, successful build). No new breakage — Critical, Important, or Minor — was introduced by this fix diff.
