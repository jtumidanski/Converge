# Task 26 Audit — Review page: status, error panel, header, file tree, diff

Commit reviewed: `77cf84c` (parent `56574ad`; 12 files, 862 insertions, 4 deletions — matches the diff package stat).

## Verdicts

- **Spec compliance:** ✅ PASS on interface shape — every produced component named in `task-26-brief.md`'s Interfaces section exists with matching props: `ReviewStatus({stage})` (`ReviewStatus.tsx:3-5`), `ReviewErrorPanel({review, onDiscard, discarding})` (`ReviewErrorPanel.tsx:11-14`), `ReviewHeader({review, onFinish, finishing})` (`ReviewHeader.tsx:8-11`), `FileTree({files, selectedPath, onSelect})` (`FileTree.tsx:6-9`), `FileDiff({file})` (`FileDiff.tsx:5-7`), `ReviewPage` reading `:id`, polling, switching status/error/diff layouts, auto-selecting the first file (`ReviewPage.tsx:24-91`).
- **Task quality: ❌ FAIL.** Build and tests are independently re-run and green (89/89, lint clean, build succeeds), and all four mandated mutations were independently reproduced from scratch and killed exactly as reported (three of four; the fourth has a reporting error — see below). However, this task ships **one Critical FR-10.11 violation**: `ReviewHeader` renders a truncated git commit SHA in the always-visible header, outside the Diagnostics collapsible that FR-10.11 explicitly scopes SHA display to. Two **Important** findings compound it: the "exhaustive branching" defense the report cites for Contract 7 is real in code but is **completely untested** (zero tests hit any of the three `isError` branches added to `ReviewPage.tsx`), and two hardcoded, non-semantic Tailwind colors were introduced (`FE-06`). A further Important finding: `ReviewPage`'s status switch is not exhaustive over `ReviewStatus` (`FINISHED`/`EXPIRED` silently fall into the "ready" diff layout). Minor findings: a factual error in the report's own mutation-4 test count, and `ReviewHeader.tsx` has no dedicated test file at all.

## Build & test gate (re-run by reviewer)

- `npm test` (`vitest run`, real worktree): **19 test files, 89 tests, all passed.** Matches the report.
- `npm run lint` (`eslint .`, whole repo, not just touched files): **`ESLint: No issues found`, exit 0.** This is actually cleaner than the report claims — the report says "3 pre-existing warnings remain in files I did not touch (`ChangeTable.tsx`, `SelectChangesPage.tsx`, `SelectChangesPage.test.tsx`)"; a fresh full-repo `eslint .` run shows zero warnings anywhere. (Possibly resolved between the report being written and now, or the report conflated lint with format:check — see next line.)
- `npm run format:check` (`prettier --check .`, whole repo): **FAILS, exit 1** — `[warn] src/components/features/changes/ChangeTable.tsx`, `[warn] src/pages/__tests__/SelectChangesPage.test.tsx`, `[warn] src/pages/SelectChangesPage.tsx`. None of these three files are touched by this task's diff (confirmed against the diff file list). The report's hedge — "clean on all files I touched (same 3 pre-existing files flagged, untouched by me)" — is accurate and was correctly caveated; this is pre-existing debt from an earlier task on this branch, not something Task 26 introduced. Flagging per Ask 7 as requested, but **not counted against Task 26**.
- `npm run build` (`tsc -b && vite build`): succeeded. `apps/backend/internal/ui/dist/.gitkeep` was deleted by the build as expected and was restored via `git checkout -- apps/backend/internal/ui/dist/.gitkeep` immediately after; `git status --porcelain` confirmed clean before finishing.

## Mandated mutation re-verification (independently reproduced, scratch copy at `/tmp/rev26`, deleted after use)

All four mutations were re-applied from scratch by the reviewer (not read from the report) in `/tmp/rev26/frontend` (a `cp -a` of `apps/frontend` including `node_modules`, outside the worktree, deleted after use). Baseline confirmed green (89/89) before mutating.

### Mutation 1 — `applying:<n>` falls through to the generic label
`ReviewStatus.tsx:10`: `if (stage.startsWith("applying:"))` → `if (stage.startsWith("never-matches:"))`. Grep-confirmed landed on line 10. `npx tsc --noEmit` clean. Ran `ReviewStatus.test.tsx`: **1/2 failed** — `getByText(/applying #435/i)` not found, `"Building the review"` rendered instead. **Confirmed: matches the report exactly (1/2).**

### Mutation 2 — Diagnostics rendered unconditionally
`ReviewErrorPanel.tsx:42`: `<Collapsible>` → `<Collapsible open={true}>`. Grep-confirmed. Ran `ReviewErrorPanel.test.tsx`: **1/3 failed** — `expect(element).not.toBeInTheDocument()` found `<dd>review/7f14b2c8</dd>` present before the Diagnostics trigger was clicked. **Confirmed verbatim, including the exact assertion text (matches the report's quoted failure character-for-character). This is the FR-10.11-critical mutation and it is genuinely caught by the test suite** — separate from the FR-10.11 violation found elsewhere in this task (see Finding 1 below; the *mechanism* the test protects — Diagnostics gating — is sound, but a *different* component leaks a SHA outside any gate).

### Mutation 3 — auto-select always wins over explicit pick
`ReviewPage.tsx:34`: `const selectedPath = explicitPath ?? files.data?.[0]?.attributes.path;` → `const selectedPath = files.data?.[0]?.attributes.path;` (with `void explicitPath;` to satisfy `noUnusedLocals`). Grep-confirmed. `npx tsc --noEmit` clean. Ran `ReviewPage.test.tsx`: **1/5 failed** — "selects a file the user clicks instead of always the first one" timed out inside `waitFor` waiting for `"new readme"` to appear. **Confirmed: matches the report (1/5).**

### Mutation 4 — `CONFLICTED` renders the diff layout
`ReviewPage.tsx:81`: `if (status === "CONFLICTED" || status === "FAILED")` → `if (status === "FAILED")`. Grep-confirmed. `npx tsc --noEmit` clean. Ran `ReviewPage.test.tsx`: **1/5 failed** — "renders the error panel, not the diff layout, for a conflicted review" timed out on `findByText(/#435 conflicts/i)`.

**Discrepancy vs. the report:** the report states "1 of 6 tests... 6 tests total after both additions" for this mutation. `grep -c '  it(' src/pages/__tests__/ReviewPage.test.tsx` in the actual committed file returns **5**, and the reviewer's own scratch run of the full suite under mutation 4 shows "1 failed | 4 passed (**5**)" — not 6. The ratio claimed in the report's summary table (`1/6`) is a factual error; the true ratio is **1/5**. This does not change the substantive conclusion (the coverage gap is real and closed), but it is a measurement-accuracy defect in the report, which matters given the emphasis this task's audit brief places on verifying claims rather than trusting them. **Minor.**

**Mutation ratio table (reviewer-verified):**

| # | Mutation | File:line | Reported ratio | Verified ratio | Gap found? |
|---|---|---|---|---|---|
| 1 | `applying:<n>` falls through | `ReviewStatus.tsx:10` | 1/2 | 1/2 ✅ | No |
| 2 | Diagnostics rendered unconditionally | `ReviewErrorPanel.tsx:42` | 1/3 | 1/3 ✅ | No — FR-10.11 gate mechanism is caught |
| 3 | Auto-select ignores explicit pick | `ReviewPage.tsx:34` | 1/5 | 1/5 ✅ | Yes — closed |
| 4 | CONFLICTED renders diff layout | `ReviewPage.tsx:81` | **1/6** (reported) | **1/5** (actual) | Yes — closed, but reported ratio is wrong |

## FileDiff / `@pierre/diffs` untestability claim — independently reproduced

Built a standalone repro harness against the real `@pierre/diffs@1.4.1` package (not mocked) in the scratch copy:

1. **Bare-hunk patch** (`"@@ -1 +1 @@\n-old\n+new"`) throws synchronously: `Error: FileDiff: Provided patch must contain exactly 1 file diff` at `getSingularPatch.ts:15`. **Reproduced verbatim**, confirms the claim that the naive test fixture (not the production data shape) was the problem.
2. **Full unified patch with headers** renders into `<diffs-container>` as an *empty* light-DOM element (`container.innerHTML` = `<diffs-container></diffs-container>`) — confirmed genuine content exists only inside `host.shadowRoot.innerHTML` (verified: header markup, filename `foo.txt`, `+1`/`-1` counts all present in the shadow root, invisible to `container.textContent` and Testing Library queries on the light DOM). This is standard shadow-DOM semantics, not a Testing Library bug, as the report states.
3. **`ResizeObserver` is genuinely invoked and genuinely undefined in jsdom** — reproduced the exact error asynchronously (it fires from a deferred highlight-render callback, not synchronously at mount, which is why a naive synchronous assertion wouldn't even need to observe it): `ReferenceError: ResizeObserver is not defined` at `node_modules/@pierre/diffs/dist/managers/ResizeManager.js:6`, invoked via `FileDiff.flushManagers` → `FileDiff.render` → `DiffHunksRenderer.applyHighlightResult`. **Matches the report's citation exactly, including the file and line.**
4. Confirmed `@pierre/diffs` was **not** mocked wholesale — `FileDiff.test.tsx` imports the real component and only mocks nothing; the mock in `ReviewPage.test.tsx` is of the local `FileDiff` wrapper component, not the library, and is called out in the brief's own sample as expected isolation at the page level.
5. Confirmed the binary and truncated branches are structurally guaranteed to short-circuit before `PatchDiff` is reached (`FileDiff.tsx:8-11` binary early-return, `:15-19` truncation notice rendered alongside — not instead of — `PatchDiff`, so truncated files still attempt `PatchDiff`, but the *notice* itself, which is what's tested, is a plain conditional reached without touching the library's rendering path for that assertion).

**Verdict: the untestability claim holds. No shim, no wholesale mock, minimal untestable surface. PASS.**

## FR-10.11 sweep — Critical finding

### Finding 1 (Critical): `ReviewHeader` shows a raw git SHA outside Diagnostics

`ReviewHeader.tsx:21`:
```tsx
{strings.base}: {baseBranch} @ {baseSha ? baseSha.slice(0, 7) : "unknown"}
```
This renders e.g. `"Base: main @ 9f21a43"` in the **always-visible page header**, for every review, on every render — not inside the Diagnostics collapsible. The controller's stated global constraint is explicit: *"Git terminology (worktree, cherry-pick, rebase, commit, SHA, branch as a git concept) appears only inside the collapsible Diagnostics section of `ReviewErrorPanel`. Everywhere else the product speaks of PRs/MRs, reviews, and files."* A truncated commit SHA is squarely "SHA" per that list, and `ReviewHeader` is not `ReviewErrorPanel`'s Diagnostics section — it's the page's primary always-on header, reached on every successful review, not just error states.

This exact interface was specified verbatim in the brief (`task-26-brief.md:11`: *"ReviewHeader(...) — repository, `base branch @ shortSha`, ..."*), so the implementer built exactly what was asked. But this is precisely the class of brief/constraint conflict the task's own process (Contract 5) calls for flagging rather than silently implementing — the report's "Brief conflicts found and how I resolved them" section lists two conflicts (both about `previousPath` typing and an untyped test helper) but does not mention this one, despite it colliding with a constraint the report itself explicitly cites and treats carefully for `ReviewErrorPanel`'s Diagnostics gating (Mutation 2, which the implementer clearly understood was FR-10.11-load-bearing). Given the report's care in one place and blindness in an adjacent, more-exposed one, this reads as an oversight rather than a considered trade-off — no comment, deviation note, or brief-conflict entry addresses it anywhere in the report.

**No test would have caught this.** `ReviewStatus.test.tsx` and `ReviewErrorPanel.test.tsx` both assert the absence of git vocabulary; `ReviewHeader` has no dedicated test file at all (see Finding 3), and `ReviewPage.test.tsx` positively asserts `expect(screen.getByText(/main @ 9f21a43/)).toBeInTheDocument()` (`ReviewPage.test.tsx:222` in the diff) — i.e., the one test that exercises this line **encodes the violation as a passing assertion** rather than catching it.

### Sweep results elsewhere: clean
- `aria-label`/`title` attributes across all five new components and `ReviewPage.tsx`: only `"Changed files"` (`FileTree.tsx:38`) and plain product-vocabulary error titles (`"Could not load this review"`, `"Could not load the file list"`, `"No file changes"`, `"Could not load this file's diff"`) — no git terms.
- `ReviewErrorPanel`'s Diagnostics section correctly sources "Base SHA" from `review.attributes.baseSha` (`ReviewErrorPanel.tsx` diff line 29-ish, `review.attributes.baseSha ? ... : null`), **not** from `error.diagnostics.sourceSha` — `ReviewDiagnostics` (`review.ts:5-10`) has no `baseSha` field, only `sourceSha`, and the component correctly does not conflate the two. `sourceSha` itself is read nowhere in the UI (dead but harmless — see Minor note below). **PASS on this specific ask.**
- No `worktree`, `cherry-pick`, `rebase`, or bare `commit`/`branch`/`sha` word leaks found via grep across the five new components and `ReviewPage.tsx`.

## FileTree error-vs-empty (Contract 7 / carried constraint) — Important finding

### Finding 2 (Important): the exhaustive-branching defense is real in code but has zero test coverage

The implementer's documented choice (report, "FileTree error-vs-empty" section) is to leave `FileTree` itself without an error prop and instead make `ReviewPage`'s branching exhaustive around it: `files.isError` → `ErrorBanner` (`ReviewPage.tsx:102-107`), then loading skeletons, then `FileTree`; and separately `fileDiff.isError` → `ErrorBanner` (`ReviewPage.tsx:128-133`), plus `review.isError` → `ErrorBanner` (`ReviewPage.tsx:53-63`). Read in isolation, `FileTree` is indeed never reached during a `files` failure — the code genuinely implements the chosen defense.

However: **`grep -n "500\|isError\|HttpResponse.error\|network\|ErrorResponse" ReviewPage.test.tsx` returns nothing.** No test in `ReviewPage.test.tsx` ever triggers `review.isError`, `files.isError`, or `fileDiff.isError`. All three `ErrorBanner` branches that exist specifically to prevent this branch's recurring defect class (RepositoryList/ProviderPicker/ChangeTable all share the "can't distinguish error from empty" shape per the audit brief) are **unreached by any test** in this diff. Per the audit brief's own framing: "a branch no test enters is not protected by anything." The mechanism is correctly designed and present, but its correctness is currently taken on faith, not verified — a future refactor could silently break any of these three branches (e.g., reorder the ternary so `isError` is checked after `isLoading`/`!data`, reintroducing exactly the false-empty bug this construction exists to prevent) and no test in the suite would fail.

This is also the one part of the report that should have been flagged as a known gap under "Concerns" (which says "None outstanding") but wasn't — the report is transparent about the `FileDiff` gap but silent about this one, despite Contract 7 being called out by name in the report's own section header.

## Additional finding: `ReviewPage`'s status switch is not exhaustive over `ReviewStatus`

### Finding 3 (Important)

`ReviewStatus` (the type, `review.ts:3`) has six members: `CREATING | READY | CONFLICTED | FAILED | FINISHED | EXPIRED`. `ReviewPage.tsx` branches explicitly on `CREATING` (line 73) and `CONFLICTED`/`FAILED` (line 81); everything else — including `READY`, but also `FINISHED` and `EXPIRED` — falls through to the diff-layout return (line 93+). A `FINISHED` or `EXPIRED` review (e.g., a stale tab left open after finishing elsewhere, or navigating directly to a review URL after its TTL lapsed) would render the full diff layout with a live "Finish Review" button, `useReviewFiles`/`useReviewFile` queries against a review that may no longer have backing data, and no messaging that the review is over. The model even ships an `isTerminal()` helper (`review.ts:66`) that is used only for polling cadence (`useReviews.ts:24`) and never consulted for page-level branching. This is not one of the four mandated mutations, but it is exactly the same shape of defect (non-exhaustive status handling silently defaulting to the "happy path" render) that this audit was asked to scrutinize, and it is untested — no test in `ReviewPage.test.tsx` constructs a `FINISHED` or `EXPIRED` review doc.

## Styling / anti-pattern checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-01 | No `any` | PASS | grep across all six new/changed files: zero matches |
| FE-02 | No manual class concatenation | PASS | zero `className={"..."+` matches; `cn()` used in `FileTree.tsx:31` |
| FE-05 | No spinners for content loading | PASS | `animate-spin` only on `Loader2` inside submit buttons: `ReviewErrorPanel.tsx:87`, `ReviewHeader.tsx:26` |
| FE-06 | No hardcoded colors | **FAIL** | `FileTree.tsx:64`: `text-emerald-600` (raw Tailwind color, no semantic token); `FileDiff.tsx:24`: `border-amber-500/40 bg-amber-500/10`. Neither color appears anywhere else in the codebase (`grep -rl "emerald\|amber-500" src` outside these two files returns nothing) — these are new, not inherited debt. The adjacent deletions count in the same `FileTree.tsx` line 65 correctly uses the semantic `text-destructive`, making the asymmetry (semantic token for deletions, raw color for additions) look like an oversight rather than a deliberate choice. |
| FE-08 | No default exports | PASS | zero `export default` in any of the six files |
| FE-15 | `cursor-pointer` on non-native interactive elements | PASS (N/A) | All interactive elements in this diff are native `<button>` (`FileTree.tsx`'s file rows, `ReviewErrorPanel`/`ReviewHeader`'s `Button` components) or the shadcn `CollapsibleTrigger asChild` wrapping a `Button` — no `<div onClick>` or similar found, so the "non-`<button>`/`<a>`" carve-out applies and no `cursor-pointer` class is required. |

## Testing checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-16 | Tests exist for changed components | PARTIAL | `ReviewStatus`, `ReviewErrorPanel`, `FileTree`, `FileDiff` all have dedicated test files. **`ReviewHeader.tsx` has no test file at all** (`find . -iname "*ReviewHeader*"` returns only the source file). It is exercised incidentally through `ReviewPage.test.tsx`'s happy-path assertions (repository name, `base @ sha` text, one provider link), but several of its own branches are never exercised by any test: `baseSha` null → `"unknown"` fallback, `baseDescription` falsy, `totals` null (no badges), more than one `included` entry, and the `finishing` spinner state. |
| — | Reachability of Contract-7 error branches | **FAIL** | See Finding 2 — `review.isError`, `files.isError`, `fileDiff.isError` in `ReviewPage.tsx` are all unreached by any test. |

## Other confirmations (per the audit brief's specific asks)

- **R52 (Finish/Discard → single mutation):** confirmed both paths converge on `finishOrDiscard.mutateAsync(id)` via the shared `closeReview()` function (`ReviewPage.tsx:43-51`), with an in-code comment explaining the one-endpoint reality so a future reader doesn't "fix" it into two. Both the "finishes the review" test (READY status, `onFinish`) and the "renders the error panel for a failed review" test (FAILED status, `onDiscard`) independently reach the mutation and assert `navigate("/")`. **PASS.**
- **R53 (no `useDiscardReview` hook):** `grep -rn "useDiscardReview" src` returns nothing. **PASS.**
- **`previousPath` fixture fix:** confirmed correct — `ReviewFileAttributes.previousPath` is `string` (never `null`) per `reviewFile.ts:12` with an explanatory comment citing the backend contract; every fixture across `FileTree.test.tsx`, `FileDiff.test.tsx`, `ReviewPage.test.tsx`, and pre-existing `services/api/__tests__/reviews.test.ts` / `useReviews.test.tsx` uses `previousPath: ""`, and `grep -rn "previousPath"` finds no `=== null` or `?? ` handling anywhere. **PASS.** One **Minor** observation not asked for but found in the course of checking: `previousPath` is never actually rendered anywhere in `FileTree.tsx` or `FileDiff.tsx` — there is no "renamed from X" display at all, so the ask's question ("confirm rename display still works when `previousPath` is `""`") is moot; there's no rename display to break. This isn't a violation (the brief doesn't require one), just worth noting since a future task adding rename display will need to handle the empty-string convention correctly from scratch.
- **Auto-select render-derived, not `useEffect`+`setState`:** confirmed — `ReviewPage.tsx:34`: `const selectedPath = explicitPath ?? files.data?.[0]?.attributes.path;`, no `useEffect` in the file (`grep -n useEffect ReviewPage.tsx` → no matches), so `react-hooks/set-state-in-effect` cannot fire. Explicit selection surviving a refetch is guaranteed by construction (`explicitPath` state is never written except by `onSelect`, and `files.data` refetching cannot touch it) rather than by an explicit "select then refetch" test — no test literally re-fetches the file list after a selection, but the derivation makes the R47-shaped failure structurally unreachable. **PASS.**

## Summary

### Critical (must fix)
- **Finding 1** — `ReviewHeader.tsx:21` renders a raw git SHA (`baseSha.slice(0, 7)`) in the always-visible header, violating the explicit constraint that SHA/git vocabulary is confined to `ReviewErrorPanel`'s Diagnostics collapsible. `ReviewPage.test.tsx`'s only assertion touching this line (`getByText(/main @ 9f21a43/)`) encodes the violation as a passing test rather than catching it. Not flagged as a brief conflict despite the report demonstrating elsewhere (Mutation 2) that it understood this exact constraint.

### Important (should fix before merge)
- **Finding 2** — The three `isError` branches in `ReviewPage.tsx` (review, files, fileDiff) that implement the documented Contract-7 defense are entirely untested; the defense is real but unverified.
- **Finding 3** — `ReviewPage`'s status switch is non-exhaustive over `ReviewStatus`; `FINISHED`/`EXPIRED` silently render the diff layout with a live Finish Review button, untested.
- FE-06 — two new hardcoded, non-semantic colors (`text-emerald-600` in `FileTree.tsx:64`, `amber-500` in `FileDiff.tsx:24`), asymmetric with the semantic `text-destructive` used one line away.
- Mutation-4 ratio in the report (`1/6`) is factually wrong; the actual, reviewer-verified ratio is `1/5`. Substance of the claim (coverage gap found and closed) still holds.

### Minor (non-blocking)
- `ReviewHeader.tsx` has no dedicated test file; several of its own branches (baseSha-null fallback, no baseDescription, no totals, multiple included changes, finishing spinner) are untested.
- `previousPath` is carried through fixtures correctly but is never rendered anywhere in the UI — no rename display exists to verify.
- `npm run format:check` fails repo-wide on three files this task did not touch (pre-existing debt); correctly caveated in the report, not counted against this task.
