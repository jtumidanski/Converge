# Task 23 Audit — React Query hooks, query client, and change-selection state

Commit reviewed: `3f0efa9` (9 files, 427 insertions, matches `git show --stat 3f0efa9`).

## Verdicts

- **Spec compliance:** ✅ PASS — every interface in `task-23-brief.md` (lines 7–15) is present with matching signatures; all three declared deviations are justified and none change observable behavior for callers.
- **Task quality:** **Approved**, with one Important finding (thin test coverage on 7 of the module's exported hooks) and one Minor observation (poll-stop `data &&` guard is inherited from the brief, not introduced by the implementer, but worth flagging for a future task).

## Build & test gate (re-run by reviewer, not taken from the report)

- `npm ci` — clean, 688 packages.
- `npx vitest run` (full suite) — **5 test files, 18 tests, all passed.** Matches the report's claim exactly (report claimed "5 test files, 18 tests, all passed" — confirmed, not inflated).
- `npm run lint` — clean, 0 errors, including on `useSelection.ts` specifically (verified `npx eslint src/lib/hooks/useSelection.ts` produces no output).
- `npm run build` — succeeded; wiped `apps/backend/internal/ui/dist/.gitkeep` as expected; restored via `git checkout -- apps/backend/internal/ui`; `git status --porcelain` is clean after restore.

## The four adjudication items

### 1. `useSelection` render-time derivation deviation

`apps/frontend/src/lib/hooks/useSelection.ts:386-396`:
```ts
const [state, setState] = useState<{ key: string; selected: Map<number, Change> }>(() => ({...}));
const selected = state.key === storageKey ? state.selected : readStorage(storageKey);
if (state.key !== storageKey) {
  setState({ key: storageKey, selected });
}
```
This is the React-endorsed "adjusting state when a prop changes" pattern (compare-during-render, conditional `setState` call in the render body, not in an effect). Verified:
- `eslint-plugin-react-hooks@7.1.1`'s `recommended-latest` config (checked via `node -e "require('eslint-plugin-react-hooks').configs"`) includes `react-hooks/set-state-in-effect: "error"` and, notably, also `react-hooks/set-state-in-render: "error"` — the new React-Compiler-era rule that specifically permits this exact conditional-during-render pattern while still catching unconditional render-time `setState` calls. Running `npx eslint src/lib/hooks/useSelection.ts` produces **zero output** — confirms the file passes both rules, so the implementer's claim that the brief's literal sample trips `set-state-in-effect` and that this rewrite is lint-clean is verified, not asserted.
- Reset correctness: on `storageKey` change, `selected` is recomputed from `readStorage(storageKey)` in the same render, and the `setState` call commits `{key: storageKey, selected}` — so the derived value and the committed state agree, and a second render will see `state.key === storageKey` and take the cheap branch. No stale key/selected pairing is possible.
- No cascading-render risk: the `setState` inside the `if` only fires when `state.key !== storageKey`, so it fires at most once per key change (fires again only if key changes again) — it does not tear across renders on every render like an unconditional call would.
- **Test constraint check (not vacuous):** re-ran `npx vitest run src/lib/hooks/__tests__/useSelection.test.ts` unmodified against this implementation — 3/3 pass. Ran the mutation `if (next.has(...))` → `if (false)` (membership check defeated) at `apps/frontend/src/lib/hooks/useSelection.ts:62` in a scratch copy (`/tmp/fe-scratch-audit23`) — this **is not a render-derivation mutation**, so it doesn't test item 1 directly, but it confirms the suite is not vacuously passing: mutant produced `AssertionError: expected true to be false` at `useSelection.test.ts:35`, typechecked clean (`npx tsc --noEmit` exit 0). I additionally hand-traced the "keeps selections separate per storage key" test (brief lines 72-79): it renders two different keys in sequence within the same test — the second `renderHook` call is a **fresh** hook instance (new `useState` initializer), not a rerender of the first with a new key, so it does not actually exercise the render-time key-change branch at all. The key-change branch is exercised for real only by test 2 remounting with the *same* key (not a key change) — so no test in the brief's suite constrains behavior when a **live, mounted** `useSelection` instance's `storageKey` argument changes across rerenders (e.g., navigating between two repos without unmounting). This is a real gap in the brief's own test design, inherited rather than introduced by the implementer — noted as Minor since the code appears correct on inspection but the "reset at the right times" property for the live-rerender case is unverified by any test in this diff.

**Finding (Minor):** No test exercises `useSelection` with a `storageKey` prop change on an already-mounted instance (the actual trigger for the render-derivation branch at `useSelection.ts:394-396`). All three brief tests use fresh `renderHook` calls per key. Not blocking — the derivation logic is small and inspection-verified correct — but flag for a follow-up test if `useSelection` is later wired to a route param that changes without unmounting (e.g., repo switch within the same page).

### 2. Poll-stop guard at `useReviews.ts:24`

`apps/frontend/src/lib/hooks/api/useReviews.ts:22-25`:
```ts
refetchInterval: (query) => {
  const data = query.state.data as Review | undefined;
  return data && !isTerminal(data.attributes.status) ? 2000 : false;
},
```
`isTerminal` (`apps/frontend/src/types/models/review.ts:65-67`, `status !== "CREATING"`) is semantically identical to the brief's inline check — already confirmed correct by the task brief, not re-litigated here.

On the `data &&` guard specifically: the brief's own sample code (`task-23-brief.md:11`) is `query.state.data?.attributes.status === "CREATING" ? 2000 : false` — optional chaining on `undefined` data yields `undefined`, which is `!== "CREATING"`, so it **also** returns `false` when data is undefined. The implementer's `data && ...` guard is behaviorally identical to the brief's own spec, not a deviation. This is not an implementer-introduced defect; it is inherited from the brief.

Is it correct behavior? Traced two cases:
- **Mid-poll transient failure** (review previously reached CREATING, then one fetch errors): TanStack Query v5 does not clear `query.state.data` on a query error when there was a prior successful fetch — the last successful value is retained. So `data` stays the CREATING snapshot, the guard remains truthy, and polling **continues** correctly through a transient blip. No defect here.
- **First-load failure** (the very first `GET /api/reviews/:id` never succeeds): `data` is `undefined` for the query's whole lifetime until a fetch succeeds, so the guard returns `false` and no interval is scheduled. The query still surfaces `isError`/`error` correctly (no swallowed-error defect per the recurring class — this is a real error, not a plausible empty value), but there is no interval-driven automatic retry after the built-in `retry: 1` is exhausted; recovery requires a manual `refetch()`/`invalidateQueries()` from the consuming component.

**Finding (Minor, informational, not a defect in this diff):** the first-load-failure-then-stuck-until-manual-refetch behavior is real, but it is identical to the brief's specified behavior (verified above), so it is not chargeable to this implementer. Flag for whoever builds the review-detail page (Task 25/26 territory) to confirm the page has a manual retry affordance for a review stuck in error before ever reaching CREATING/READY.

### 3. `RepositoryListParams` reuse vs. brief's `RepositoryQueryParams`

Brief (`task-23-brief.md:321-324`):
```ts
export interface RepositoryQueryParams {
  page?: number;
  pageSize?: number;
}
```
Actual, reused type at `apps/frontend/src/services/api/repositories.ts:15-18`:
```ts
export interface RepositoryListParams {
  page?: number;
  pageSize?: number;
}
```
Structurally identical, field-for-field (`page?: number; pageSize?: number;`) — confirmed by direct read, not inference. `useRepositories.ts:1,10` imports it via `import { repositoriesService, type RepositoryListParams } from "@/services/api";` and uses it as the hook's params type, matching the brief's usage exactly except for the name. No behavior change. **PASS.**

### 4. Async-hook test hygiene

- `useReviews.test.tsx`'s poll-stop test (brief lines 120-134, landed verbatim) uses `waitFor` with real assertions for both the CREATING and READY transitions (`expect(result.current.data?.attributes.status).toBe(...)`), then a **fixed** `setTimeout(..., 2500)` sleep to assert no further calls occurred. This is a real-timer sleep racing the 2000ms poll interval, but with adequate margin (2500 > 2000) and a 15000ms test timeout — re-ran it standalone (`npx vitest run .../useReviews.test.tsx`) and it passed in ~4.6s, consistent with a real (not flaky) margin. This pattern is inherited verbatim from the brief, not introduced by the implementer.
- The "does not fetch files until READY" test uses a fixed 50ms sleep to assert `fileCalls === 0` while the query is `enabled: false`. This is not actually racing anything — a disabled query never issues a fetch regardless of how long the test waits, so the fixed sleep doesn't create the timing-dependency the project has previously rejected (Task 21/22 issues were sleeps racing a real async operation that *could* complete; here the operation structurally cannot fire).
- Confirmed the test genuinely observes the stop rather than a premature snapshot: `callsAtReady` is captured only after `waitFor(... .toBe("READY"))` resolves, and the post-sleep assertion compares against that captured value, not a hardcoded number — a regression that kept polling would fail this assertion (verified directly: mutation 1 below reproduces exactly this failure).

**No blocking finding.** Both async tests are either brief-verbatim (already accepted at the brief-review stage) or structurally non-racy.

## Independent mutation re-run

Both of the report's two mutations were reproduced independently in a scratch copy (`/tmp/fe-scratch-audit23`, outside the worktree, deleted after use), following the six measurement rules:

| # | Mutation | Grep proof (scratch copy) | Reachability | Typecheck | Test result |
|---|---|---|---|---|---|
| 1 | `useReview`'s `refetchInterval` forced to always `return 2000` | `grep -n "return 2000" .../useReviews.ts` → `25:      return 2000;` | Reached by every render of `useReview` (not a dead branch) | `npx tsc --noEmit -p .` → exit 0, no errors | `npx vitest run src/lib/hooks/api/__tests__/useReviews.test.tsx` → **FAIL**, `AssertionError: expected 3 to be 2` at `useReviews.test.tsx:52` (`expect(calls).toBe(callsAtReady)`) |
| 2 | `useSelection`'s `toggle` membership check forced to `if (false)` | `grep -n "if (false)" .../useSelection.ts` → `62:        if (false) {` | Reached on every `toggle()` call | `npx tsc --noEmit -p .` → exit 0, no errors | `npx vitest run src/lib/hooks/__tests__/useSelection.test.ts` → **FAIL**, `AssertionError: expected true to be false` at `useSelection.test.ts:35` (`expect(result.current.isSelected(421)).toBe(false)`) |

Both mutations reproduce the report's claimed failure text exactly. Ratio 2/2 confirmed independently — the report's table is accurate, not fabricated.

**Finding (Important):** Two mutations is a thin ratio for 427 lines across four hook modules (`useProviders.ts`, `useRepositories.ts`, `useChanges.ts`, `useReviews.ts` minus `useReview`) plus `useSelection.ts`. Grep confirms **zero test references** to `useProviders`, `useRepositories`, `useRepository`, `useChanges`, `useFinishReview`, `useInvalidateReviews`, or `useReviewFile` anywhere under `apps/frontend/src` (`grep -rn "useProviders\|useRepositories\|useRepository\b\|useChanges\|useFinishReview\|useInvalidateReviews\|useReviewFile\b" --include="*.test.*"` → no matches). This is not a violation of the brief — Step 1 of the brief explicitly scoped tests to only `useSelection` and three `useReview*`/`useCreateReview` cases — but it means 7 of the module's ~13 exported hooks (including two mutations with cache-invalidation side effects, `useFinishReview` and `useInvalidateReviews`) ship with no regression coverage in this diff. Recommend a follow-up task add at least smoke tests for the mutation hooks' `onSettled` invalidation behavior, since that's exactly the kind of side-effect logic that silently rots.

## Global constraints checklist

| Constraint | Status | Evidence |
|---|---|---|
| Hooks reach backend only via `@/services/api` | ✅ PASS | `grep -rn "apiGet\|apiPost\|apiDelete\|from \"@/lib/api/client\""` across all new hook files → no matches; every hook imports from `@/services/api` (`useProviders.ts:2`, `useRepositories.ts:1`, `useChanges.ts:2`, `useReviews.ts:2`) |
| List services return `{ items, page }`, hooks don't unwrap further | ✅ PASS | `useRepositories`/`useChanges` pass through `repositoriesService.list`/`changesService.list` return values untouched (`useRepositories.ts:14`, `useChanges.ts:15`), which return `PagedRepositories`/`PagedChanges` (`repositories.ts:11-14`, `changes.ts:12-15`) |
| No `INTERNAL` error code introduced | ✅ PASS | `grep -rn "INTERNAL"` across all new files → no matches |
| `ReviewStatus` terminal-state handling correct | ✅ PASS | `isTerminal` at `types/models/review.ts:66` is `status !== "CREATING"`, matching the five terminal states; used correctly at `useReviews.ts:24` (see item 2 above) |
| No `any` | ✅ PASS | `grep -n ": any\|as any"` across all new hook/selection/query-client files → no matches |
| No default exports | ✅ PASS | `grep -n "export default"` across all new files → no matches |
| Query key factories use `as const` | ✅ PASS | `providerKeys`, `repositoryKeys`, `changeKeys`, `reviewKeys` all use `as const` on every array literal (`useProviders.ts:4-6`, `useRepositories.ts:5-12`, `useChanges.ts:5-8`, `useReviews.ts:6-12`) |

## Report accuracy check

- Test count claim ("5 test files, 18 tests, all passed") — **verified independently**, matches exactly.
- Mutation table — **verified independently**, both mutations reproduce the exact quoted failure text.
- `npm run lint` clean claim — **verified independently**.
- `npm run build` / dist `.gitkeep` restore claim — **verified independently**; reviewer's own build wiped `.gitkeep` identically and required the same `git checkout --` restore.
- `RepositoryListParams` structural-identity claim — **verified independently** by reading both interfaces side by side.
- No fabrication found in this report.

## Summary

### Blocking (must fix)
- None.

### Non-Blocking (should fix)
- **Important:** 7 of ~13 exported hooks (`useProviders`, `useRepositories`, `useRepository`, `useChanges`, `useFinishReview`, `useInvalidateReviews`, `useReviewFile`) have zero test coverage in this diff — in scope for the brief as written, but a real gap, especially for the two invalidation-bearing mutation hooks.
- **Minor:** No test exercises `useSelection` with a live `storageKey` change on an already-mounted instance — the actual trigger for the render-time derivation branch at `useSelection.ts:394-396`. Add one when `useSelection` is wired into a page that can change repos without unmounting.
- **Minor:** The `data &&` guard in `useReview`'s `refetchInterval` (identical to the brief's own optional-chaining spec) means a review whose very first fetch fails before ever reaching CREATING/READY will not auto-retry via polling; only the manual-refetch path recovers it. Confirm the consuming page (Task 25/26) has a retry affordance for that state.
