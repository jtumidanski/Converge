# Task 23 Fix Round 1 Re-Review

Diff reviewed: `.superpowers/sdd/plan/review-e4ff964..5110047.diff` (commit `5110047`,
6 files, 390 insertions, 11 deletions — matches `git diff --stat e4ff964..5110047`
exactly).

## Verdict: fix round accepted, no new blocking findings

## Build & test gate (re-run independently)

- `npx vitest run` (full suite, apps/frontend, Node 22) → **8 test files, 36 tests,
  all passed.** Matches the report's claimed "8 files / 36 tests" exactly — not
  inflated.
- `npm run lint` → clean, 0 output.
- `git status --porcelain apps/frontend` → clean, no dist churn left behind.
- `git diff --stat e4ff964..5110047 -- apps/frontend/src | grep -v test` confirms
  the diff touches only test files plus `apps/frontend/src/test/render.tsx` — no
  production hook file (`useReviews.ts`, `useProviders.ts`, `useRepositories.ts`,
  `useChanges.ts`, `useSelection.ts`) appears in the changed-file list.

## Finding 1 (Important) — seven hooks with zero coverage

**ADDRESSED**, all seven by name, including the one the commit message omits:

- `useProviders` — `apps/frontend/src/lib/hooks/api/__tests__/useProviders.test.tsx:12-44`.
  Asserts the hook calls through `providersService.list()` (via MSW handler on
  `/api/providers`), caches under `providerKeys.lists()`
  (`useProviders.test.tsx:28`, compares `wrapper.client.getQueryData(providerKeys.lists())`
  against the hook's own data), and on a 503 surfaces `ApiError` with code
  `PROVIDER_UNAVAILABLE` and `data` staying `undefined` rather than an empty list
  (`useProviders.test.tsx:38-43`).
- `useRepositories` — `useRepositories.test.tsx:21-63`. Named verbatim in the
  commit subject (`git log -1 --format=%B 5110047` → `test(task-001): cover
  useProviders/useRepositories/useChanges/...`). Confirmed covered: "does not
  fetch when providerId is undefined" (`useRepositories.test.tsx:22-33`),
  fetches with params passed through and caches under
  `repositoryKeys.list(...)` (`:35-50`), and surfaces `ApiError` code
  `REPOSITORY_UNAVAILABLE` instead of empty data on failure (`:52-62`).
- `useRepository` — `useRepositories.test.tsx:65-92`. Not named in the commit
  message (message lists `useRepositories`, not the singular `useRepository`),
  but genuinely covered: "does not fetch when enabled is false" (`:66-76`) and
  "fetches ... when enabled, keyed by repositoryKeys.detail" (`:78-91`).
- `useChanges` — `useChanges.test.tsx:26-71`. Covers the disabled-until-both-args
  case, the fetch-with-params/cache-key case, and the `ApiError` surfacing case
  (code `REPOSITORY_UNAVAILABLE`).
- `useFinishReview` — `useReviews.test.tsx:153-195`. Asserts `reviewsService.remove`
  is called (`deleteCalls` counter) **and** that `onSettled` invalidates both
  `reviewKeys.detail(id)` and `reviewKeys.lists()` via
  `queryClient.getQueryState(...)?.isInvalidated`, not merely "no throw"
  (`:170-175`); a second test asserts a failed delete surfaces `ApiError` code
  `REVIEW_NOT_READY` on the mutation's `error` (`:177-195`).
- `useInvalidateReviews` — `useReviews.test.tsx:196-226`. Asserts
  `invalidateReview(id)` invalidates only that review's detail key — a sibling
  review's detail and the list explicitly stay `isInvalidated: false`
  (`:196-211`) — and `invalidateAll()` invalidates every reviews-scoped entry
  (`:213-226`). This is exactly the "invalidation fires against the right keys"
  constraint the original audit asked for.
- `useReviewFile` — `useReviews.test.tsx:100-151`. Fetches a single file's diff,
  asserts it does not fetch until both `id` and `path` are set (two disabled
  renders, `calls` stays 0), and surfaces `ApiError` code `NOT_FOUND` instead of
  empty data.

The commit subject names six hooks (`useProviders`, `useRepositories`,
`useChanges`, `useReviewFile`, `useFinishReview`, `useInvalidateReviews`) and
genuinely omits the seventh, `useRepository` (singular) — confirmed by
`git log -1 --format=%B 5110047`. But the audit's concern was whether coverage
exists, not whether the commit subject is exhaustive, and `useRepository` is
covered as shown above (`useRepositories.test.tsx:65-92`). No gap here — this
is an omission in the commit message, not in test coverage.

Each of the seven hooks' error-path test also satisfies the "surfaces rather
than becomes empty data" bar the brief demanded: every failure test asserts
`isError`/`error` populated with the specific `ApiError` code **and**
`result.current.data` explicitly `toBeUndefined()` (e.g.
`useProviders.test.tsx:43`, `useRepositories.test.tsx:61-62`,
`useChanges.test.tsx:69-70`, `useReviews.test.tsx:149-150`), not just "no crash."

## Finding 2 (Minor) — render-derivation branch untested

**ADDRESSED.** `apps/frontend/src/lib/hooks/__tests__/useSelection.test.ts:61-77`
adds "re-derives selection when storageKey changes on an already-mounted
instance," using `renderHook`'s `rerender` (not a fresh `renderHook` call) to
change `storageKey` on a *live* instance: toggle under `keyA`, `rerender({key:
keyB})` (pre-seeded via `sessionStorage.setItem(keyB, ...)` before mount) and
assert the numbers list switches to the `keyB` content, then `rerender({key:
keyA})` and assert it switches back to the `keyA` selection. This is the first
test in the whole suite that actually drives a `storageKey` prop change across
renders rather than mounting a fresh hook per key — the exact gap the original
audit called out.

Verified by independent mutation (see below, mutation 1): defeating the
render-derivation branch (`useSelection.ts:38`) fails specifically this new
test with `expected [ 427 ] to deeply equal [ 9 ]` at
`useSelection.test.ts:72` — confirms the test is not vacuous and would catch a
regression in exactly the code path it targets.

## New Critical/Important breakage introduced by this diff

None found. This diff is test-file-only plus a backward-compatible extension
of `src/test/render.tsx`'s `queryWrapper()` (new optional `{gcTime}` param
defaulting to the prior value `0`, and a `.client` property attached to the
returned function — additive, does not change the signature any existing
caller uses). No other test file in the repo calls `queryWrapper()` with
assumptions that would break (`grep -rn "queryWrapper(" src --include="*.test.*"`
shows only the four files in this diff use it).

Two out-of-scope observations, deferred as minors per instructions (not new
loop items): the `useChanges`/`useRepositories`/`useReviewFile` "does not
fetch" tests use fixed ~20ms sleeps, but each is checking a *disabled* query
that structurally cannot fire regardless of wait length — same non-racy
pattern the original audit accepted for the brief's own "does not fetch files"
test, not a new timing-dependent test.

## Adjudicating the implementer's declared concern (gcTime override)

**Both halves of the claim verified:**

1. **"Test-infrastructure only, no production hook code changed."** Confirmed:
   `git diff --stat e4ff964..5110047 -- apps/frontend/src` lists only
   `src/test/render.tsx` and five `__tests__/*.test.tsx` files — zero production
   hook files. `git status --porcelain apps/frontend` after the full test run
   is also clean.
2. **Whether it weakens the tests it enables.** Checked what production's
   `gcTime` actually is: `apps/frontend/src/lib/query-client.ts` does not set
   `gcTime` in `createQueryClient()`'s `defaultOptions.queries`, so production
   uses TanStack Query v5's built-in default of 5 minutes (300,000ms) for
   inactive queries — **not** `0`. The *original* shared test client's
   `gcTime: 0` (still the default when `queryWrapper()` is called with no
   options) was already unrepresentative of production in the stricter
   direction: it evicts inactive queries almost instantly, something
   production never does. Raising it to `Infinity` for the four
   invalidation-inspecting tests does not create a pass condition production
   couldn't reach — if anything it corrects a mismatch that was biased toward
   false negatives (evicting the cache entry before the assertion could even
   run), not false positives. `isInvalidated` is set synchronously by
   `invalidateQueries()` regardless of `gcTime`; `gcTime` only controls how
   long a cache entry survives *after* becoming inactive, which is orthogonal
   to whether invalidation fired correctly.
   Empirically confirmed via mutation re-run (below): mutations 2 and 3, which
   target exactly the two invalidation-bearing hooks gated by the `gcTime:
   Infinity` tests, both still fail with the correct assertion text. The
   override does not mask a dropped or misdirected invalidation — it was
   necessary scaffolding, not a crutch that makes the tests pass regardless of
   correctness.

## Independent mutation re-run (5/5 reproduced)

All mutations applied in a scratch copy outside the worktree
(`/tmp/fe-scratch-task23-review`, deleted after use), each reverted and
confirmed via `diff -rq` against the worktree (byte-identical, no output)
before deletion. Baseline run before mutating: 5 test files / 24 tests, all
passed.

| # | Mutation | Grep proof (scratch copy) | Reachability | Typecheck | Test result (quoted) |
|---|---|---|---|---|---|
| 1 | `useSelection` render-derivation branch defeated: `const selected = state.key === storageKey ? state.selected : readStorage(storageKey);` → `const selected = state.selected;` | `grep -n "const selected = state.selected;" src/lib/hooks/useSelection.ts` → `38:  const selected = state.selected;` | Reached on every render, not a dead branch | `npx tsc --noEmit -p .` → exit 0, "No errors found" | FAIL — `AssertionError: expected [ 427 ] to deeply equal [ 9 ]` at `useSelection.test.ts:72` |
| 2 | `useInvalidateReviews.invalidateReview` retargeted to `reviewKeys.all` | `grep -n "invalidateReview: (id: string) =>" -A1 src/lib/hooks/api/useReviews.ts` → `77:      queryClient.invalidateQueries({ queryKey: reviewKeys.all }),` | Reached on every `invalidateReview()` call | `npx tsc --noEmit -p .` → exit 0 | FAIL — `AssertionError: expected true to be false` at `useReviews.test.tsx:209` |
| 3 | `useFinishReview`'s `onSettled` dropped `reviewKeys.lists()` invalidation (line deleted) | `sed -n '60,68p' src/lib/hooks/api/useReviews.ts` shows only `detail(id)` remaining | Reached on every `useFinishReview` settle | `npx tsc --noEmit -p .` → exit 0 | FAIL — `AssertionError: expected false to be true` at `useReviews.test.tsx:175` |
| 4 | `useProviders`'s `queryFn` swallows the service error: `.list()` → `.list().catch(() => [])` | `grep -n "catch" src/lib/hooks/api/useProviders.ts` → `12:    queryFn: () => providersService.list().catch(() => []),` | Reached on every fetch/error | `npx tsc --noEmit -p .` → exit 0 | FAIL — `AssertionError: expected false to be true` at `useProviders.test.tsx:41` |
| 5 | `useRepository`'s `enabled` guard drops the caller's `enabled` arg: `enabled && Boolean(providerId) && fullName.length > 0` → `Boolean(providerId) && fullName.length > 0` | `grep -n "enabled: Boolean(providerId)" src/lib/hooks/api/useRepositories.ts` → `27:    enabled: Boolean(providerId) && fullName.length > 0,` | Reached whenever `useRepository(..., false)` is called | `npx tsc --noEmit -p .` → exit 0 | FAIL — `AssertionError: expected 1 to be +0` at `useRepositories.test.tsx:76` |

Ratio: **5/5**, all quoted failure texts match the report's table exactly. No
fabrication found — the report's mutation table is accurate.

## Standing constraints re-checked against this diff

- Hooks reach backend only via `@/services/api`: N/A, this diff touches no
  hook production code; already `PASS` from the original audit and unchanged.
- Error codes used in new tests — `PROVIDER_UNAVAILABLE`, `REPOSITORY_UNAVAILABLE`
  (`useProviders.test.tsx:39`, `useRepositories.test.tsx:56`,
  `useChanges.test.tsx:66`), `NOT_FOUND` (`useReviews.test.tsx:146`),
  `REVIEW_NOT_READY` (`useReviews.test.tsx:180`) — all confirmed against
  `docs/tasks/task-001-combined-review-mvp/plan.md:26` and
  `apps/backend/internal/api/errors.go`'s code set. No `INTERNAL` code
  appears anywhere in the diff (`grep -n "INTERNAL"` across the six changed
  files → no matches).
- No fixed sleep races a real interval: the new/extended fixed-sleep
  assertions (`useChanges.test.tsx:37`, `useRepositories.test.tsx:31,75`,
  `useReviews.test.tsx:134`) all gate on a *disabled* query, which cannot fire
  regardless of elapsed time — not a timing-dependent test in the sense this
  branch has previously rejected.

## Report accuracy check

- "8 files / 36 tests, all passed" — verified independently, exact match.
- Mutation table (5 entries) — verified independently, all 5 reproduce the
  exact quoted failure text.
- "No production hook code changed in this round" — verified independently
  via `git diff --stat`.
- `npm run lint` clean claim — verified independently.

## Summary

### Blocking (must fix)
- None.

### Non-Blocking (should fix)
- None new. Both open findings from `audit-task-23.md` are closed by this
  fix round with tests independently confirmed to be discriminating (5/5
  mutations caught).
