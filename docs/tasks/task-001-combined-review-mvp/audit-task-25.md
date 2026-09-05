# Task 25 Audit — Change selection view and routing

Commit reviewed: `773955a` (parent `dc1fc63`; 8 files, 432 insertions, 1 deletion — matches the diff package stat).

## Verdicts

- **Spec compliance:** ✅ PASS — every produced interface named in `task-25-brief.md`'s Interfaces section exists with matching props: `ChangeTable({changes, loading, isSelected, onToggle})` (`ChangeTable.tsx:8-12`), `ChangeSearch({value, onChange})` (`ChangeSearch.tsx`), `SelectionBar({count, onBuild, onClear, building})` (`SelectionBar.tsx:5-9`), `SelectChangesPage` at `/select` reading `?provider=`/`?repo=` (`SelectChangesPage.tsx:22-24`), `AppRoutes` mapping all four routes (`routes.tsx:8-21`), `App` wrapping `QueryClientProvider > BrowserRouter > AppRoutes` + `<Toaster/>` (`App.tsx:10-16`). `ReviewPage` placeholder exists and compiles the app pending Task 26.
- **Task quality:** **Approved with findings.** Build, lint, and tests are all independently re-run and green. The three mandated mutations were independently reproduced from scratch (not read from the report) and killed exactly as reported, including the 0/4→1/4 gap-and-fix on mutation 3. One **Important** finding: the change-selection page fires a genuinely wasted first API request before the repository default branch resolves (Ask 4). Two **Important** coverage gaps: `SelectionBar`, `ChangeSearch`, and `ChangeTable` have zero dedicated unit tests, and `SelectChangesPage.test.tsx` never exercises `onClear`, `Pagination`, the missing-selection guard, or the not-found route — real un-reached code, not merely "under-asserted" code. One **Minor**: `ReviewPage.tsx` has no in-code marker that Task 26 is meant to replace it.

## Build & test gate (re-run by reviewer)

- `npm run build` (`tsc -b && vite build`): succeeded, 2179 modules transformed, output to `apps/backend/internal/ui/dist/`. No errors.
- `npm run lint` (`eslint .`): clean, zero output (confirms `react-hooks/set-state-in-effect` did not fire on either render-derivation block).
- `npm test` (`vitest run`): **12 test files, 66 tests, all passed.** Matches the report.

## Mandated mutation re-verification (independently reproduced, scratch copy at `/tmp/scratch-audit25`, deleted after use)

All three mutations were re-applied from scratch by the reviewer (not read from the report) in a scratch copy at `/tmp/scratch-audit25/` (outside the worktree; `cp -a src` + `cp -a node_modules` + config files from the worktree, deleted after each run). Grep-proof, actual assertion failure, and ratio for each:

### Mutation 1 — selection storage key includes `search`
```
- const selection = useSelection(`converge:selection:${providerId ?? ""}/${repository ?? ""}`);
+ const selection = useSelection(`converge:selection:${providerId ?? ""}/${repository ?? ""}/${search}`);
```
Landed at `SelectChangesPage.tsx:47` (verified via grep in the mutant). Ran `npx vitest run src/pages/__tests__/SelectChangesPage.test.tsx`: **1/4 failed** — "keeps selections across a search and enables Build Review" — `screen.getByText(/1 selected/i)` threw `TestingLibraryElementError: Unable to find an element with the text: /1 selected/i` after the search term changed the storage key and dropped the selection. **Confirmed: matches the report exactly (1/4).**

### Mutation 2 — `build()`'s catch becomes a no-op
```
- } catch (error: unknown) {
-   const detail = messageFor(error, "The review could not be started.");
-   setCreateError(detail);
-   toast.error(detail);
- }
+ } catch (error: unknown) {
+   void error;
+ }
```
Landed at `SelectChangesPage.tsx:69-71`. Ran the suite: **1/4 failed** — "shows the API detail when creating a review fails" — timed out on `expect(await screen.findByText(/same base branch/i)).toBeInTheDocument()` because `createError` was never set. **Confirmed: matches the report exactly (1/4).**

### Mutation 3 — `ChangeTable`'s `checked={isSelected(number)}` → `checked={false}` (the gap-and-fix claim)
Landed at `ChangeTable.tsx:51` (verified by grep). Two runs performed:
1. **With the added `aria-checked` assertion present** (current committed test file): **1/4 failed**, actual failure:
   ```
   Error: expect(element).toHaveAttribute("aria-checked", "true")
   Received: aria-checked="false"
   at SelectChangesPage.test.tsx:80:27
   ```
   Confirmed — the assertion genuinely kills the mutant.
2. **With that same assertion line deleted (reproducing the brief's original 4 tests)** and the mutant still applied: **4/4 passed** — the mutation is invisible without it. This independently confirms the report's claim that clicking a Radix `Checkbox` fires `onCheckedChange` regardless of the `checked` prop, so the original test suite exercised only `selection.toggle`'s side effects (count, mutation payload, navigation), never whether the rendered `checked` state reflected `isSelected`.

**Verdict on mutation 3: the gap was real, and the fix (`SelectChangesPage.test.tsx:80`, `toHaveAttribute("aria-checked", "true")`) genuinely closes it.** This is the single most load-bearing test in the file — without it, `ChangeTable` could silently never reflect selection state and nothing would fail.

## Ask 2 — Is the mutation ratio thin for a diff this size? Reachability audit

**Yes, thin, and it is isolation more than concealment — but real gaps exist and should be named plainly rather than excused.**

432 insertions across 8 files are covered by exactly one test file (`SelectChangesPage.test.tsx`, 4 tests, 118 lines) exercising the integration surface. Three components (`ChangeTable.tsx`, `ChangeSearch.tsx`, `SelectionBar.tsx`) and two files (`routes.tsx`, `App.tsx`) have **zero dedicated test files** — confirmed via `find src -iname "*ChangeTable*test*" / "*SelectionBar*test*" / "*ChangeSearch*test*" / "*App*test*" / "*routes*test*"`, all empty. Everything is covered, if at all, transitively through `SelectChangesPage`.

Checking reachability before protection, per the ask — grepped `SelectChangesPage.test.tsx` for `Clear`, `Pagination`, `Missing selection`, `Page not found`: **zero matches for all four.** Specifically:

- **`SelectionBar`'s `onClear`** (`SelectionBar.tsx:16`, wired to `selection.clear` at `SelectChangesPage.tsx:126`) — no test ever clicks the "Clear" button. Unreached, not merely unprotected: if `onClear` were wired to the wrong handler (e.g. `onBuild` instead of `selection.clear`), no test would fail.
- **`Pagination`'s wiring** (`SelectChangesPage.tsx:119-124`, `page`/`hasNext`/`onChange`/`disabled` props) — no test clicks "Next"/"Previous" or asserts `page` state changes. A mutant that dropped `setPage(1)` on search-change-elsewhere or broke the `onChange={setPage}` wiring would go undetected.
- **The `!providerId || !repository` guard branch** (`SelectChangesPage.tsx:55-61`, renders "Missing selection" `ErrorBanner`) — every test uses `route = "/select?provider=gh&repo=atlas%2Fserver"`; no test renders the page with a missing param. This branch is entirely unreached by the suite.
- **The not-found route** (`routes.tsx:12-19`) and `App.tsx`/`routes.tsx` as a whole — no test imports `AppRoutes` or `App` at all. Routing itself (that `/select` actually maps to `SelectChangesPage`, that `*` renders the not-found panel) is asserted nowhere; `SelectChangesPage.test.tsx` renders the page component directly inside a bare `MemoryRouter`, bypassing `routes.tsx` entirely.

None of these four are "protected but under-tested" — they are **not entered at all** by any test in the diff. This is consistent with the branch's established pattern of testing the "happy path integration" and leaving peripheral wiring to manual/visual verification (see Task 24's audit noting the same shape for `RepositoryList`/`ProviderPicker`), so it reads as a continuation of an existing, disclosed gap rather than a newly concealed one — but the size of this diff (432 lines, first-ever routing wiring) makes the absence of even one `routes.tsx`/`App.tsx` smoke test a more consequential omission than in prior smaller tasks. Graded **Important**, not Critical, because the four gaps are all inert-on-failure (a broken Clear button or broken not-found route degrades UX but doesn't corrupt data or leak credentials), but they are exactly the kind of gap mutation testing exists to surface, and none of the three mandated mutations happened to touch any of the four.

## Ask 3 — Can `ChangeTable`/`SelectionBar`/`ChangeSearch` be misused to show a false empty state on error, independent of the page?

Checked all three components' own prop contracts, not `SelectChangesPage`'s usage of them:

- **`ChangeTable`** (`ChangeTable.tsx:8-12`): props are `{changes, loading, isSelected, onToggle}` — **no `error` prop.** If `loading` is `false` and `changes` is `[]` (which is exactly what a careless caller would pass on a fetch failure, e.g. `changes.data?.items ?? []`), `ChangeTable.tsx:28` renders `<EmptyState title="No merged PRs/MRs" .../>` — a false "nothing here" message indistinguishable from a genuine empty result set. This is the same defect shape flagged in Task 24 for `RepositoryList`/`ProviderPicker`, and it is **not fixed here** — it is once again avoided only by the caller's discipline. `SelectChangesPage.tsx:97-113`'s `changes.isError ? <ErrorBanner/> : <ChangeTable/>` ternary is exhaustive and correctly prevents `ChangeTable` from ever being called during a fetch failure — confirmed by reading the ternary directly, not by trusting the report's claim — but a second caller of `ChangeTable` (there will be one if a future task reuses it, e.g. a dashboard widget) that forgets this ternary gets a silent false-empty state with zero compiler or test signal. This is a **deferred, disclosed-pattern issue** (same as the two prior "carries as a deferred minor" components) rather than a newly introduced defect, but it is not closed by this task either. Graded **Minor** (consistent with how the same shape was graded for `RepositoryList`/`ProviderPicker`).
- **`SelectionBar`** (`SelectionBar.tsx:5-9`): props are `{count, building, onBuild, onClear}` — no `error`/loading-vs-empty ambiguity is possible here; `count` is an unambiguous number and `building` is a boolean the caller controls. No equivalent defect shape applies. **PASS.**
- **`ChangeSearch`** (`ChangeSearch.tsx`): props are `{value, onChange}` — a plain controlled input with no data-loading state at all. No equivalent defect shape applies. **PASS.**

## Ask 4 — Is the pre-`useRepository`-resolution `useChanges` fetch a wasted request, and is it a defect?

**Yes, it is a real wasted request against the API, and it is worth fixing — not an acceptable cost of the render-derivation choice.** Verified from the actual hook code, not the code's appearance:

- `useChanges.ts:16-20`: `enabled: Boolean(providerId) && Boolean(repository)` — this gate depends **only** on the two route params, not on `repositoryQuery`'s status or on `baseBranch` being non-empty. It fires as soon as `providerId`/`repository` are present, which is on first render of `/select`.
- `SelectChangesPage.tsx:48-49`: `changeParams = baseBranch ? {target: baseBranch, search, page} : {search, page}` — since `baseBranch` is `""` until `defaultBranch` resolves and gets rendered-derived in (`SelectChangesPage.tsx:44-46`), the **first** `useChanges` call has query key `[..., providerId, repository, {search, page}]` (no `target`) and fires a real network request for **all** merged changes with no base-branch filter.
- Once `repositoryQuery` resolves and `defaultBranch` is known, `baseBranch` changes from `""` to the default, `changeParams` changes to include `target`, the query key changes, and — because TanStack Query v5 treats a changed `queryKey` as a distinct cache entry — a **second, different** network request fires. `placeholderData: keepPreviousData` (`useChanges.ts:19`) only smooths the UI transition between the two cache entries (avoids a loading flash); it does not suppress the underlying `queryFn` invocation for the new key, per TanStack Query v5's documented `keepPreviousData`/`placeholderData` semantics (it seeds `data` from the *previous* key's cache while the *new* key's fetch is in flight — it does not skip that fetch).
- Net effect: every landing on `/select` issues one request for the (large, unfiltered) full merged-changes list, then immediately discards it and issues a second, target-filtered request. This is wasted backend load on every page load, not merely a one-time cold-start cost, and it also transiently shows unfiltered results (a UX correctness issue, not just a performance one) for however long `repositoryQuery` takes to resolve.
- **Fix available without reintroducing `useEffect`+`setState`:** gate `useChanges`'s `enabled` on `repositoryQuery.isSuccess` (or on `defaultBranch !== undefined || baseBranchState.edited`) in addition to the existing `providerId`/`repository` checks. This does not require an effect — it is a render-time boolean composed from data already in scope in `SelectChangesPage.tsx`. Not applied in this diff.

Graded **Important** — it is a defect, not an acceptable cost, and the fix does not reintroduce the lint violation the render-derivation pattern was chosen to avoid.

## Ask 5 — `exactOptionalPropertyTypes` build-failure claim (reproduced from scratch)

Reproduced independently, not read from the report. `tsconfig.app.json:24` confirms `"exactOptionalPropertyTypes": true`. Wrote a minimal isolated repro (`interface Params { target?: string }`, `f({ target: baseBranch || undefined })`) and compiled it with `tsc --noEmit` under the same flag:
```
error TS2379: Argument of type '{ target: string | undefined; ... }' is not assignable to parameter of type 'Params' with 'exactOptionalPropertyTypes: true'.
  Types of property 'target' are incompatible.
    Type 'string | undefined' is not assignable to type 'string'.
```
This confirms the brief's literal sample (`{ target: baseBranch || undefined, ... }` against `ChangeListParams.target?: string`, and `{ baseBranch: baseBranch || undefined, ... }` against `CreateReviewRequest.baseBranch?: string` at `types/models/review.ts:53`) cannot compile under this repo's tsconfig. The implementer's fix — `SelectChangesPage.tsx:48-49` and `:64-67`'s two-branch conditionals that omit the key entirely — is real and necessary, not a fabricated excuse.

**Backend contract check (also verified from source, not assumed):** `apps/backend/internal/api/reviews.go:20` declares `BaseBranch string \`json:"baseBranch"\`` with no `omitempty` — a plain, non-pointer string. When the frontend omits the `baseBranch` key from the JSON body entirely, Go's `encoding/json` unmarshals the missing field to its zero value `""`. `apps/backend/internal/review/input.go:36-37`'s own doc comment states: *"An empty BaseBranch is allowed; the service substitutes the repository default before use"* — and `apps/backend/internal/review/service.go:121-123` confirms: `if in.BaseBranch == "" { in.BaseBranch = repo.DefaultBranch(); ... }`. Omitting the key and sending an explicit empty string are therefore **indistinguishable to the backend and both correctly mean "use the repository default."** The frontend's omit-the-key fix is not just a type-checker workaround — it produces the exact wire behavior the backend already expects. **Confirmed correct.**

## Ask 1 (R47) — `baseBranchState.edited` behavior when the user clears the input to empty; `ChangeSearch` debounce/staleness

**`baseBranchState.edited` does NOT get clobbered on clear — verified by tracing the actual state transitions, not by inspection alone:**
- `SelectChangesPage.tsx:56-60`: the base-branch `<Input>`'s `onChange` always sets `{edited: true, value: event.target.value}` — including when the user clears the field to `""`. Once `edited` is `true`, it stays `true` for the life of the component (nothing ever resets it to `false`).
- `SelectChangesPage.tsx:44`: `baseBranch = baseBranchState.edited ? baseBranchState.value : (defaultBranch ?? baseBranchState.value)` — once `edited === true`, this expression **ignores `defaultBranch` entirely**, so `baseBranch` becomes and stays `""` after a clear.
- `SelectChangesPage.tsx:45-46`: the re-seed-from-default guard is `if (!baseBranchState.edited && defaultBranch && ...)` — gated on `!edited`, so it can never fire again once the user has typed (or cleared) anything.
- Conclusion: clearing the input does **not** snap the default back and does **not** clobber the user. This scenario is untested (the brief's tests never clear the field), but tracing the code confirms correct behavior, not merely "looks correct." **PASS**, with a note that this exact scenario has zero test coverage (an extension of the Ask 2 coverage-gap finding, graded there rather than separately here).
- One related, secondary consequence not asked directly but adjacent: if the user clears the base-branch field to `""` and then clicks "Build Review," `SelectChangesPage.tsx:64-67`'s conditional treats `baseBranch` as falsy and **omits** `baseBranch` from the create-review request — which (per Ask 5's backend trace) means "use the repository default," not "no base-branch filter." A user who explicitly cleared the field to signal "don't filter" instead silently gets the repository default reapplied server-side. This is a real behavioral wrinkle worth flagging but is a product/UX question (what should an empty base-branch field mean?) rather than a code defect — the frontend's behavior is internally consistent and matches the backend's documented contract. Graded **Minor**.

**`ChangeSearch` debounce and draft staleness — verified by tracing state transitions, not by inspection:**
- `ChangeSearch.tsx`'s `{source, draft}` pair (mirrors `useSelection.ts:36-41`'s precedent) recomputes `draft = state.source === value ? state.draft : value` during render, and conditionally calls `setState` to resync when the incoming `value` prop changes externally (e.g. after the debounce commits and the parent's `search` state updates, flowing back down as a new `value` prop).
- Traced two scenarios: (1) rapid typing before the 300 ms timer fires — each keystroke's `onChange` handler sets `{source: value, draft: newDraft}`; since `value` (the prop) hasn't changed yet, `state.source === value` holds, `draft` reflects the latest keystroke, and the `useEffect`'s cleanup (`clearTimeout`) correctly restarts the 300 ms window on every keystroke (`ChangeSearch.tsx`'s effect dependency array `[draft, onChange, value]`). (2) After the debounce fires and calls `onChange(draft)`, the parent updates `search`/`value`; on the next render `state.source !== value`, so `draft` re-derives to the new `value` and the render-time `setState` resyncs `{source: value, draft: value}` — no stale draft is possible because the derivation is unconditional on every render where `source !== value`.
- No infinite-loop or stale-draft failure mode found. **PASS**, genuinely verified via manual trace of the transitions, not asserted from appearance.

## R48 / R49 — quick confirmations (not re-litigating the choice, only the mechanics)

- **R48**: `App.tsx:10-11` — `<QueryClientProvider client={queryClient}><BrowserRouter>` — confirmed `QueryClientProvider` is the outer wrapper, matching the brief's Step 3 sample order. **PASS.**
- **R49**: `ChangeTable.tsx:65-67` — `{sourceBranch} to {targetBranch}` — confirmed literal "to" text, not an arrow glyph. **PASS.**

## Ask 6 — Is `ReviewPage` placeholder inert and clearly marked as Task 26's to replace?

`ReviewPage.tsx` (3 lines): `export function ReviewPage() { return <div className="p-6">Loading review…</div>; }`. **Inert: confirmed** — no state, no effects, no data fetching, no navigation side effects; renders static text only. Named export (FE-08 compliant), no `any`, no hardcoded colors beyond the default `p-6` spacing utility.

**Not clearly marked in-code, however.** There is no comment in `ReviewPage.tsx` itself (e.g. `// Placeholder — replaced in Task 26`) indicating it is temporary; the "to be replaced in Task 26" framing exists only in the brief and the implementer's report, not in the source file a future reader (or Task 26's own implementer) would open first. Graded **Minor** — functionally harmless (the placeholder is trivially replaceable and the route wiring in `routes.tsx:15` will simply pick up whatever `ReviewPage` becomes), but a one-line comment would have been free and is the kind of thing this repo's own conventions elsewhere use (e.g. the `useSelection.ts`/`SelectRepositoryPage.tsx` render-derivation comments explaining *why*, not just *what*).

## FE-* checklist (anti-patterns, mechanical)

| Check | Status | Evidence |
|---|---|---|
| No `any` / `as any` | PASS | Zero matches across all 8 changed files (`grep -nE ': any\|as any'`). |
| No manual class concatenation | PASS | Zero `className={"..."+` matches; all conditional classes (none needed here) would go through `cn()`; no violation present. |
| No direct API client calls in components | PASS | Zero `@/lib/api/client` imports in any component/page; `SelectChangesPage.tsx` uses `useChanges`/`useRepository`/`useCreateReview` hooks exclusively. |
| No inline Zod schemas in components | PASS | Zero `z.object(`/`z.string(` matches — this task added no schemas. |
| No spinners for content loading | PASS | `animate-spin` appears once, at `SelectionBar.tsx:21`, inside the "Build Review" submit `<Button>` — the anti-pattern doc's explicit exception. `ChangeTable.tsx:20-26` uses `<Skeleton>` for content loading. |
| No hardcoded colors | PASS | Zero `bg-white`/`bg-gray-N`/etc. matches; all styling uses semantic classes (`bg-card`, `text-muted-foreground`, `border-border`, etc.). |
| No state mutation | PASS | Zero `.push(`/`.splice(`/`.sort(` matches; `useSelection.ts`'s `Map` copy-and-mutate-copy pattern (pre-existing, not part of this diff) is the only state-touching logic reused here. |
| No default exports for components | PASS | Zero `export default` matches; all components/pages use named exports. |
| Error handling surfaces via toast/error state | PASS (established local pattern, not the guideline doc's literal `createErrorFromUnknown`) | `SelectChangesPage.tsx:69-72`'s catch calls `messageFor(error, ...)`, `setCreateError(detail)`, and `toast.error(detail)`. Note: `createErrorFromUnknown` does not exist anywhere in this codebase (`grep -rn "createErrorFromUnknown" src` → no matches) — the established local convention is `ApiError`/`messageFor` (`lib/api/errors.ts`), used identically in the pre-existing `SelectRepositoryPage.tsx`. Grading against the codebase's actual convention, not the generic doc text. |
| Cursor affordance (FE-15) | PASS / not applicable | No non-`<button>`/`<a>` clickable elements introduced. The `Checkbox` in `ChangeTable.tsx:49-53` renders as a native `<button>` (confirmed in rendered test output: `<button aria-checked=... aria-label="Select #427" ...>`), which gets the browser's default pointer per the styling guideline's own carve-out for native `<button>`/`<a>`. `SelectionBar`/`Pagination` buttons are all shadcn `<Button>` (native `<button>`). No clickable `<div>`/table-row surfaces were added. |
| Tests exist for changed components (FE-16) | **FAIL (see Ask 2)** | `ChangeTable.tsx`, `ChangeSearch.tsx`, `SelectionBar.tsx`, `routes.tsx`, `App.tsx` all have zero dedicated test files. Only `SelectChangesPage.test.tsx` exists, and it does not reach `onClear`, `Pagination`, the missing-selection guard, or the not-found route. |
| Mocks updated when services changed (FE-17) | N/A | No service files changed in this diff; `changesService`/`reviewsService`/`useChanges`/`useCreateReview`/`useRepository` all pre-date this task. |

## Summary

### Blocking (must fix before this counts as fully protected — Important)
- **Ask 4**: `useChanges`'s `enabled` gate in `useChanges.ts:16-20` (used from `SelectChangesPage.tsx:50`) does not wait for `repositoryQuery` to resolve, causing a genuine wasted, unfiltered network request on every `/select` page load before the default base branch arrives. Fix by broadening `enabled` (or an equivalent render-time boolean) — does not require reintroducing `useEffect`+`setState`.
- **Ask 2 / FE-16**: Zero coverage for `SelectionBar.onClear` (`SelectionBar.tsx:16`), `Pagination` wiring (`SelectChangesPage.tsx:119-124`), the `!providerId || !repository` guard (`SelectChangesPage.tsx:55-61`), and the not-found route (`routes.tsx:12-19`). These are unreached, not merely under-asserted — no test enters these branches at all.

### Non-Blocking (should fix — Minor)
- **Ask 3**: `ChangeTable.tsx:8-12` has no `error` prop and will silently render "No merged PRs/MRs" if a future caller passes an empty array during a fetch failure (safe today only because `SelectChangesPage.tsx:97-113`'s exhaustive ternary protects it) — same disclosed shape as `RepositoryList`/`ProviderPicker` from Task 24, not newly introduced, not closed here either.
- **Ask 1 (secondary)**: Clearing the base-branch field to `""` and building silently reapplies the repository default server-side (per the backend's documented "`baseBranch == ""` → use default" contract) rather than meaning "no filter" — an internally-consistent but possibly-surprising product behavior, not a code defect.
- **Ask 6**: `ReviewPage.tsx` has no in-code comment marking it as a Task 26 placeholder, though it is otherwise inert and trivially replaceable.
