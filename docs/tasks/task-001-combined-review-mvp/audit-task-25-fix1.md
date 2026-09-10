# Task 25 Re-Review — Fix Round 1

Fix commit reviewed: `2597bbf` (parent `4a399a8`; 6 files, 130 insertions, 2 deletions — matches `review-4a399a8..2597bbf.diff`'s stat).

## Gate re-run

- `npm test -- --run` (vitest): **14 test files, 75 tests, all passed** (up from 12 files / 66 tests before the fix — +9 new tests: 4 in `SelectChangesPage.test.tsx`, 2 in `routes.test.tsx`, 3 in `SelectionBar.test.tsx`, matching the diff exactly).
- `npm run lint` (eslint): clean, zero output.
- `npm run build` (`tsc -b && vite build`): succeeded, 2179 modules transformed, no errors.

## Finding 1 (Important) — discarded first request → **ADDRESSED**

`apps/frontend/src/lib/hooks/api/useChanges.ts:11-21` adds an `enabled = true` parameter, ANDed into the existing gate:

```
enabled: enabled && Boolean(providerId) && Boolean(repository),
```

`apps/frontend/src/pages/SelectChangesPage.tsx:54` passes `!repositoryQuery.isPending`, with an explanatory comment at `SelectChangesPage.tsx:49-53` stating the settle-not-succeed rationale verbatim.

### R50 (settle-not-succeed) — verified, not just read

`repositoryQuery.isPending` is TanStack Query v5's "no data yet and not disabled" flag; it is `true` only while a query is in-flight for the first time (or genuinely has no cached data), and becomes `false` on **both** success and error settlement, which is exactly the semantics the ruling demands. Confirmed behaviorally by the shipped test at `SelectChangesPage.test.tsx:139-151` ("still fetches and renders changes untargeted when the repository lookup fails"): the repository endpoint returns a 404, and the test asserts (a) the changes endpoint is still called with `target` absent (`:144`), and (b) the row still renders (`:150`). This ran green against the real code.

### Mutation 1 re-run from scratch (scratch copy at `/tmp/scratch-audit25fix1/`, outside the worktree, deleted after use)

Dropped the `enabled` parameter from the `enabled` expression:
```diff
-    enabled: enabled && Boolean(providerId) && Boolean(repository),
+    enabled: Boolean(providerId) && Boolean(repository),
```
Grep-confirmed landed at `useChanges.ts:20` in the mutant (the now-unused `enabled` param itself produces `tsc` noise — `TS6133: 'enabled' is declared but its value is never read` — a pre-existing `noUnusedParameters` diagnostic unrelated to test behavior). Ran `npx vitest run src/pages/__tests__/SelectChangesPage.test.tsx`:
```
✗ waits for the repository lookup to settle before requesting changes, and targets the default branch
AssertionError: expected [ null, 'main' ] to have a length of 1 but got 2
```
**Matches the reported failure exactly.** (A second, collateral test — "wires Pagination…" — also failed on this mutant because the extra untargeted fetch leaves `isFetching` transiently true for longer; this is a bonus kill, not a contradiction.)

### Mutation 2 re-run from scratch — **the one that matters**

Reset `useChanges.ts` to the real committed version, then changed the page's gate:
```diff
-  const changes = useChanges(providerId, repository, changeParams, !repositoryQuery.isPending);
+  const changes = useChanges(providerId, repository, changeParams, repositoryQuery.isSuccess);
```
Grep-confirmed landed at `SelectChangesPage.tsx:54`. `npx tsc -b --noEmit`: clean, no errors — the mutant typechecks. Ran the suite:
```
✗ still fetches and renders changes untargeted when the repository lookup fails
TestingLibraryElementError: Unable to find an element with the text: Add field-state endpoint.
```
**Matches the reported failure exactly, verbatim.** This is the load-bearing kill: an `isSuccess`-gated version would leave `useChanges` permanently disabled after a repository-lookup failure (since `isSuccess` never becomes true), reproducing exactly the "failure renders as nothing here" defect class the ruling was written to prevent, and the shipped test catches it. **No further action needed — the guard against regressing R50 exists and works.**

### No existing `useChanges` caller changed behavior

`grep -rln "useChanges(" apps/frontend/src` finds only `useChanges.ts` itself, `useChanges.test.tsx`, and `SelectChangesPage.tsx`. `useChanges.test.tsx`'s three call sites (`useChanges(undefined, "atlas/server", {})`, `useChanges("gh", undefined, {})`, `useChanges("gh", "atlas/server", {})`, `useChanges("gh", "atlas/server", params)`) all omit the fourth argument, so `enabled` defaults to `true` and their gating behavior (`Boolean(providerId) && Boolean(repository)`) is byte-for-byte unchanged. Confirmed by reading `useChanges.test.tsx:1-73` directly — all pass unmodified in the full suite run above.

**Test aim (Ask 2):** the new test at `SelectChangesPage.test.tsx:125-136` asserts on `changeRequests` — an array populated from the msw handler's actual received `target` query-param per request — and checks both **count** (`toHaveLength(1)`) and **which value** was sent (`toBe("main")`). This is aimed at the branch, not just "something happened": a regression that fired the wrong number of requests, or fired the right number but untargeted, or targeted but with the wrong branch, would each independently fail a different assertion in this test. Well-aimed, confirmed by mutation kill above.

## Finding 2 (Important) — unreached surfaces

### SelectionBar's `onClear` and disabled gating — **ADDRESSED**

`apps/frontend/src/components/features/changes/__tests__/SelectionBar.test.tsx` (new file, 3 tests):
- `:6-12` — count 0: both Clear and Build Review disabled.
- `:14-22` — count 2, not building: Clear enabled; clicking it calls `onClear` exactly once **and asserts `onBuild` was not called** (`:21`) — this is aimed at "which handler fired," not merely "a handler fired," directly answering the concern that a mis-wired `onClick={onBuild}` on the Clear button would go undetected.
- `:24-27` — count 3, `building=true`: both disabled, covering the second disabling condition independently of count.

All three tests mount `SelectionBar` directly with controlled props/mocks — each genuinely enters the branch it names, not incidentally via the page.

### Pagination wiring — **PARTIALLY ADDRESSED**

`SelectChangesPage.test.tsx:157-179` ("wires Pagination to page state and gates Next on hasNext") mounts the full page, seeds `hasNext: true`, and:
- Confirms `Next` is enabled (`:170`), clicks it, and confirms the request layer received `page=2` (`:174`) and that the page-2 view rendered (`:175`).
- Clicks `Previous` and confirms it returns to page 1 and that `Previous` becomes disabled at page 1 (`:178`).

This genuinely enters `Pagination`'s `onChange={setPage}` wiring and its own `page <= 1` disable logic (`Pagination.tsx:15`) — the finding's core complaint ("no test clicks Next/Previous or asserts page state changes") is resolved.

**Residual gap, not closed by this round:** no test in the diff ever asserts `Next` is *disabled* when `hasNext: false` (`Pagination.tsx:22`), despite most other tests in the file seeding `hasNext: false`. A mutant that inverted or dropped `hasNext={changes.data?.page?.hasNext ?? false}` at `SelectChangesPage.tsx:120` would not be caught — `Next` would simply stay enabled and no test asserts otherwise. Likewise, `disabled={changes.isFetching}` (`SelectChangesPage.tsx:122`) is exercised transiently during the click-Next flow but never asserted on directly; a mutant that dropped this prop entirely would not be caught by anything in the diff. Both are narrower than the original finding's "unreached" — they are now **reached but unprotected**, which is a real improvement but not full closure. Grade: **Minor** (the wiring that matters — `onChange`, `page` reflection, and the `page<=1` disable — is now protected; only the `hasNext=false`/`isFetching` disable assertions remain thin).

### `!providerId || !repository` guard — **ADDRESSED**

`SelectChangesPage.test.tsx:153-156` renders with `route: "/select?provider=gh"` (repo param omitted) and asserts the "Missing selection" banner renders and no `table` role appears. This genuinely enters the guard at `SelectChangesPage.tsx:57-63` via the `!repository` half of the OR. The `!providerId` half (e.g. `route: "/select?repo=atlas%2Fserver"`) is not separately tested, but since both operands render identical code, this is not a meaningfully different branch for mutation-testing purposes — a single well-aimed test suffices here, consistent with the original finding's own framing of this as one branch.

### Not-found route — **ADDRESSED**

`apps/frontend/src/__tests__/routes.test.tsx` (new file) imports `AppRoutes` directly (not `SelectChangesPage` in isolation, unlike the rest of the suite) and:
- `:15-18` renders at `/this-does-not-exist` and asserts "Page not found" appears — genuinely enters `routes.tsx`'s `*` route.
- `:20-23` renders at `/reviews/7f14b2c8` and asserts "Page not found" does **not** appear and the review placeholder text does — a good negative-space assertion that also guards against a wildcard route swallowing a valid path (e.g. an ordering regression in the `<Routes>` list).

This closes the "no test imports `AppRoutes` at all" gap named in the original finding. `App.tsx` itself (the `QueryClientProvider > BrowserRouter > AppRoutes` + `<Toaster/>` wrapper) is still never rendered by any test, but `App.tsx` was not one of the four specifically-named unreached branches in this fix-round's scope — deferred below, not blocking.

## Ask 3 — `ChangeTable`/`ChangeSearch` dedicated tests: still absent; ruling on whether page-level coverage reaches their branches

`ChangeTable.tsx` and `ChangeSearch.tsx` are **untouched by this fix diff** (confirmed: neither file appears in `review-4a399a8..2597bbf.diff`'s file list). Per scope, this doesn't reopen a blocking loop item, but the ask requires a plain ruling:

- **`ChangeTable`'s `loading` skeleton branch (`ChangeTable.tsx:18-25`)** — **reached, not asserted.** Every page test's `useChanges` call starts in `isLoading: true` on first render (TanStack Query v5 semantics) before the msw response resolves, so `ChangeTable` is passed `loading={true}` and renders the `<Skeleton>` branch on the very first render of every single test in `SelectChangesPage.test.tsx`. No test asserts on the skeleton's presence, but the branch is genuinely executed by the existing suite, not dead code from a coverage standpoint. A mutant that permanently broke the skeleton's rendering (e.g. wrong key, crashed on render) would surface as an error boundary/render failure in every test, not go unnoticed — so it's "unprotected by name" but not "silently unreached."
- **`ChangeTable`'s empty-state branch (`ChangeTable.tsx:26-28`, `changes.length === 0`)** — **genuinely unreached.** No test in the diff (before or after this fix round) ever seeds a changes response that resolves to zero rows for a *successful, non-error* fetch. The closest test (search for "typo") filters down to one remaining row, never zero. A mutant that broke the empty-state message, or one that made `ChangeTable` render the empty state when it shouldn't (e.g. flipped `changes.length === 0` to `!== 0`), would not be caught by anything in this suite. **This is a real, standing gap** — not closed by this fix round, but also not part of the two findings this round was scoped to answer, so it does not extend this loop. Recorded as deferred below.
- **`ChangeSearch`'s 300 ms debounce boundary** — **reached loosely, boundary not asserted.** `SelectChangesPage.test.tsx:75-84` types into the search box using real timers (no `vi.useFakeTimers()`) and `waitFor`s the filtered result, which necessarily lets the real 300 ms `setTimeout` in `ChangeSearch.tsx:19-24` fire and call `onChange`. So the debounce path is genuinely exercised end-to-end. What is **not** verified: that a keystroke *before* 300 ms elapses does **not** trigger a request (i.e., that rapid typing doesn't fire N requests), or the exact boundary value. This is an existing, pre-fix-round gap (also untouched by this diff), carried forward as deferred.

## Ask 4 — `ReviewPage.tsx` comment — **ADDRESSED**

`apps/frontend/src/pages/ReviewPage.tsx:1`:
```
// Placeholder — Task 26 replaces this with the real combined review page.
```
Accurate and matches Ruling R51's intent exactly; the function body below it is unchanged (still inert, no state/effects).

## Ask 5 — ternary and `ChangeTable`'s `error` prop — **CONFIRMED INTACT, NOT REGRESSED**

`SelectChangesPage.tsx:106-119`:
```tsx
{changes.isError ? (
  <ErrorBanner ... onRetry={() => void changes.refetch()} />
) : (
  <ChangeTable
    changes={changes.data?.items ?? []}
    loading={changes.isLoading}
    isSelected={selection.isSelected}
    onToggle={selection.toggle}
  />
)}
```
The exhaustive `isError` ternary is unchanged from the pre-fix version and still the only thing standing between a fetch failure and a false empty state. `ChangeTable.tsx:8-12`'s prop interface (`{changes, loading, isSelected, onToggle}`) still has no `error` prop — confirmed unchanged (file untouched by this diff). Deliberately deferred to Task 30 per the original audit; nothing in this fix diff touches it. **Not regressed.**

## Deferred (out of scope for this fix round — observations only, do not extend the loop)

- `ChangeTable.tsx`'s empty-state branch (`:26-28`) is genuinely unreached by any test (pre-existing gap, `ChangeTable.tsx` untouched by this diff).
- `ChangeSearch.tsx`'s debounce boundary (rapid-typing / pre-300ms non-firing) is not precisely asserted (pre-existing gap, `ChangeSearch.tsx` untouched by this diff).
- `Pagination`'s `hasNext=false → Next disabled` and `disabled={changes.isFetching}` branches are now *reached* by the page test but not directly *asserted* — see Finding 2 grading above (graded Minor, not blocking).
- `App.tsx` (the `QueryClientProvider > BrowserRouter > AppRoutes` + `<Toaster/>` wrapper) is still never rendered by any test — not one of the four branches named in this fix round's scope.
- `ChangeTable`/`ChangeSearch` still have zero dedicated unit-test files (only reached transitively through `SelectChangesPage.test.tsx`), same as originally noted; this fix round did not add any, and was not asked to.

## Summary

### Blocking — none

Both open findings are resolved at the level this fix round was scoped to close:
- **Finding 1**: ADDRESSED. R50's settle-not-succeed semantics are correctly implemented at `SelectChangesPage.tsx:54`, independently reproduced via both mandated mutations (exact reported failures), and no existing `useChanges` caller's behavior changed.
- **Finding 2**: ADDRESSED for all four named branches (`SelectionBar.onClear`+disabled gating, the `!providerId||!repository` guard, and the not-found route fully; `Pagination` wiring substantially, with a narrow residual gap noted below).

### Non-Blocking (should fix, does not require another round)
- **Minor** — `Pagination`'s `hasNext=false → Next disabled` and `disabled={changes.isFetching}` are executed by the new page test but never asserted; a mutant touching either would go uncaught (`SelectChangesPage.tsx:120,122`).
- **Minor** (deferred, pre-existing) — `ChangeTable`'s empty-state branch (`ChangeTable.tsx:26-28`) remains genuinely unreached by any test in the suite.
- **Minor** (deferred, pre-existing) — `ChangeSearch`'s debounce boundary is exercised but not precisely asserted.
