# Frontend Audit — task-001-combined-review-mvp (final whole-branch, Task 30 of 30)

- **Audit Scope:** 81 changed `.ts`/`.tsx` files under `apps/frontend` (`git diff e46f93c...HEAD`)
- **Guidelines Source:** `frontend-dev-guidelines` skill (FE-01 … FE-17), plus the three
  agreed deviations recorded in `CLAUDE.md` (Vitest not Jest; thin `fetch` wrapper not a
  caching API client; plain service objects not a `BaseService` class)
- **Date:** 2026-09-06
- **Build:** PASS (controller's `make clean lint test test-integration build docker-build`, six exit 0)
- **Tests:** PASS (same gate). Reviewer additionally ran four scratch probe tests (removed after use; tree verified clean)
- **Overall:** NEEDS-WORK — 0 Critical, 3 Important, 4 Minor

Prior per-task audits cleared tasks 21–26 in isolation. This sweep deliberately targets the
seams those audits could not see: how the components compose at the page level, and how
React Query's *disabled* state propagates into leaf components' empty-state branches.

---

## Method note — evidence, not inference

Findings F-1 and F-2 are behavioural claims about React Query's `enabled: false` state. Rather
than assert them from memory, four probe tests were written against the real MSW harness
(`src/test/server.ts`, `src/test/render.tsx`) and executed:

```
$ npx vitest run src/pages/__tests__/ZZAudit.probe.test.tsx
 Test Files  1 passed (1)
      Tests  4 passed (4)
```

- PROBE A — `SelectRepositoryPage`, `/api/providers` gated open: asserted
  `findByText(/this token cannot see any repositories/i)` resolves *before* providers land. PASSED.
- PROBE B — `/api/providers` returns `{data: []}`: asserted the same text is present **and**
  `queryByRole("alert")` is absent (no error banner anywhere). PASSED.
- PROBE C — `SelectChangesPage`, repository-detail request gated open: asserted
  `findByText(/nothing matches this base branch and search/i)` resolves while the lookup is pending. PASSED.
- PROBE D — `/api/providers` returns 502 `PROVIDER_AUTH`: asserted the provider `ErrorBanner`
  ("The token was rejected.") and the "this token cannot see any repositories" empty state are
  **both** in the document simultaneously. PASSED.

The probe file was deleted afterwards; `git status --short` returns empty.

---

## File Inventory

| Classification | Files |
|---|---|
| Page | `pages/SelectRepositoryPage.tsx`, `pages/SelectChangesPage.tsx`, `pages/ReviewPage.tsx` |
| Root / routing | `App.tsx`, `main.tsx`, `routes.tsx` |
| Component — common | `components/common/{EmptyState,ErrorBanner,PageHeader,Pagination}.tsx` |
| Component — features | `components/features/changes/{ChangeSearch,ChangeTable,SelectionBar}.tsx`, `components/features/providers/ProviderPicker.tsx`, `components/features/repositories/{ManualRepositoryForm,RepositoryList}.tsx`, `components/features/review/{FileDiff,FileTree,ReviewErrorPanel,ReviewHeader,ReviewStatus}.tsx` |
| Component — ui (vendored shadcn) | `components/ui/{badge,button,card,checkbox,collapsible,dialog,input,scroll-area,select,separator,skeleton,table,tooltip}.tsx` (13) |
| Hook | `lib/hooks/api/{useChanges,useProviders,useRepositories,useReviews}.ts`, `lib/hooks/useSelection.ts` |
| Service | `services/api/{changes,index,providers,repositories,reviews}.ts` |
| Schema | `lib/schemas/repository.ts` |
| Type | `types/api/jsonapi.ts`, `types/models/{change,provider,repository,review,reviewFile}.ts`, `vite-env.d.ts` |
| Other | `lib/api/{client,errors}.ts`, `lib/{query-client,strings,utils}.ts`, `test/{render,server,setup}.ts(x)`, `vite.config.ts`, `vitest.config.ts`, 15 `__tests__/` files |

---

## Anti-Pattern Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-01 | No `any` type | **PASS** | `grep -rn ': any\|as any\|<any>\|any\[\]' src --include='*.ts*'` → zero matches. Every `as` cast in non-test source is either a namespace import alias, a type-guard narrowing inside the guard itself (`types/api/jsonapi.ts:39-40`, `lib/hooks/useSelection.ts:12`), a `Promise<T>` response cast at the single trust boundary (`lib/api/client.ts:35,49`), or an `enabled`-gated non-null assertion in a `queryFn` (`useRepositories.ts:17`, `useChanges.ts:19`, `useReviews.ts:19,36,45`). |
| FE-02 | No manual class concatenation | **PASS** | `grep -rn 'className={"\|className={`' src --include='*.tsx'` → zero matches. Conditional classes go through `cn()`: `FileTree.tsx:54-57`, `lib/utils.ts`. |
| FE-03 | No direct API client calls in components | **PASS** | `grep -rn 'lib/api/client' src/components src/pages` → zero matches. Only `services/api/*.ts` import it (`services/api/repositories.ts:1`). See F-6 for the related-but-distinct service-layer-in-component issue. |
| FE-04 | No inline Zod schemas in components | **PASS** | `grep -rn 'z\.object(\|z\.string(' src/components src/pages` → zero matches. The one schema lives at `lib/schemas/repository.ts:4`. |
| FE-05 | No spinners for content loading | **PASS** | All four `animate-spin` sites are submit-button affordances: `SelectionBar.tsx:21`, `ReviewErrorPanel.tsx:87`, `ReviewHeader.tsx:26`, `ManualRepositoryForm.tsx:58`. Content loading uses `Skeleton` (`RepositoryList.tsx:24-26`, `ChangeTable.tsx:29-31`, `ProviderPicker.tsx:26`, `ReviewPage.tsx:80,146,170`, `ReviewStatus.tsx:29-30`). |
| FE-06 | No hardcoded colors | **FAIL (Minor)** | One match app-wide: `components/ui/dialog.tsx:42` — `bg-black/10` on the overlay instead of a theme token. See F-4. Every other surface uses semantic tokens (`ErrorBanner.tsx:13`, `SelectionBar.tsx:14`, `FileTree.tsx:56,64-65`). |
| FE-07 | No state mutation | **PASS** | Three `.push`/`.sort` matches, all on locally-owned arrays: `FileTree.tsx:29` pushes into a bucket created at line 31 inside a `useMemo`; `FileTree.tsx:34` and `useSelection.ts:75` sort fresh spreads (`[...map.entries()]`, `[...selected.keys()]`). `useSelection.ts:61-67` copies the Map before mutating. |
| FE-08 | No default exports for components | **PASS** | `grep -rn 'export default' src` → zero matches. |
| FE-09 | Error handling surfaces to the user | **PASS** | Project uses `messageFor()` (`lib/api/errors.ts:21`) in place of the guideline's `createErrorFromUnknown` — a rename of the same contract, consistent with the documented thin-client deviation. Every async boundary handles rejection and surfaces it: `SelectChangesPage.tsx:90-94` (error state **and** `toast.error`), `ReviewPage.tsx:60-62` (toast), `ManualRepositoryForm.tsx:38-39` (inline error). Query failures render `ErrorBanner` with a working `onRetry`: `SelectRepositoryPage.tsx:37-43,54-59`, `SelectChangesPage.tsx:124-129`, `ReviewPage.tsx:65-75,137-142,163-168`. `lib/api/client.ts:6-20` degrades safely on a non-JSON error body. |

## Architecture Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-10 | JSON:API model shape | **PASS** | Every model is `Resource<TType, TAttributes>` = `{type, id, attributes}` — `types/api/jsonapi.ts:1-6`; `change.ts:15`, `provider.ts:11`, `repository.ts:10`, `review.ts:56`, `reviewFile.ts:19,26`. `unwrapList`/`unwrapOne` (`jsonapi.ts:50-65`) deliberately **throw** on a malformed 2xx body rather than coercing to `[]`/`undefined` — a good defence that makes F-1/F-2 the only remaining "silent empty" path. |
| FE-11 | Service layer | **PASS (documented deviation)** | Plain frozen-object services, not `BaseService`. Explicitly sanctioned by `CLAUDE.md` → Architecture Notes. `services/api/repositories.ts:33-48`, `index.ts:1-13`. |
| FE-12 | Query key factory uses `as const` | **PASS** | `useProviders.ts:5-7`, `useRepositories.ts:4-12`, `useChanges.ts:4-9`, `useReviews.ts:6-13` — every key literal is `as const`, and keys are hierarchical (`reviewKeys.file` extends `.files` extends `.detail`), so `useFinishReview`'s `invalidateQueries({queryKey: reviewKeys.detail(id)})` (`useReviews.ts:66`) correctly cascades to that review's files and file-diffs. |
| FE-13 | Forms use `react-hook-form` + `zodResolver` | **PASS** | Only one form on the branch: `ManualRepositoryForm.tsx:24-27` — `useForm<RepositoryFormData>({resolver: zodResolver(repositorySchema)})`, with `noValidate` at line 46 so Zod owns validation. |
| FE-14 | Schema in `lib/schemas/` with inferred type | **PASS** | `lib/schemas/repository.ts:4-16` + `export type RepositoryFormData = z.infer<typeof repositorySchema>` at line 18. Covered by `lib/schemas/__tests__/repository.test.ts`. |
| — | React Query invalidation correctness | **PASS** | `useCreateReview` invalidates `reviewKeys.lists()` `onSettled` (`useReviews.ts:55-57`); `useFinishReview` invalidates both `detail(id)` and `lists()` (`useReviews.ts:65-68`). `useReview`'s `refetchInterval` stops polling on any terminal status (`useReviews.ts:22-25` + `review.ts:66-68`), so a `FINISHED`/`EXPIRED`/`FAILED` review does not poll forever. |
| — | Server state owned by React Query | **FAIL (Minor)** | `ManualRepositoryForm.tsx:37` fetches a `Repository` through the service layer directly, outside the cache. See F-6. |

## Styling Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-15 | Interactive elements show `cursor-pointer` | **FAIL (Important)** | `grep -rn 'cursor-' src --include='*.tsx'` returns only `cursor-not-allowed` (disabled) and `cursor-default` (`select.tsx:114,150,169`). **No `cursor-pointer` exists anywhere in the codebase.** See F-3. |

## Testing Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-16 | Tests exist for changed components | **PASS with a noted gap** | 20 test files. Components without a dedicated test (`ChangeTable`, `RepositoryList`, `ProviderPicker`, `ChangeSearch`, `Pagination`, `ErrorBanner`, `EmptyState`, `ReviewHeader`) are all exercised through the three page tests (`SelectRepositoryPage.test.tsx`, `SelectChangesPage.test.tsx`, `ReviewPage.test.tsx`) — acceptable for presentational leaves. The gap is not *which* components are tested but *which states*: no test renders any of them in the query-disabled state, which is exactly how F-1/F-2 survived 26 prior audits. |
| FE-17 | Mocks updated when services changed | **N/A → PASS** | No `__mocks__/` directory; the branch uses MSW request interception (`src/test/server.ts`) so there is no service-mock interface to drift. `services/api/__tests__/reviews.test.ts` tests the service against MSW directly. |

---

## Deferred-minor ledger triage

### Ledger item 1 — the four components with no `error` prop: **combined ruling**

The ledger's premise is that all four are "safe only by caller discipline (exhaustive
ternaries)". **That premise is wrong on two counts, and one of the two is a real defect.**

| Component | Guard at the call site | Ruling |
|---|---|---|
| `RepositoryList` | `SelectRepositoryPage.tsx:54-74` — genuine `repositories.isError ? ErrorBanner : <RepositoryList/>` ternary | Error path **is** exclusive. But the *disabled* path is not — see F-1. **NOT SAFE.** |
| `ChangeTable` | `SelectChangesPage.tsx:124-137` — genuine `changes.isError ? ErrorBanner : <ChangeTable/>` ternary | Same: error exclusive, disabled path leaks — see F-2. **NOT SAFE.** |
| `ProviderPicker` | `SelectRepositoryPage.tsx:37-52` — **not a ternary at all.** The `ErrorBanner` (37-43) and the `ProviderPicker` (44-52) are *siblings* | The picker renders an empty `<Select>` next to the banner. The error *is* surfaced, and an empty dropdown does not assert anything false. **SAFE, but the ledger's description is inaccurate — record it as a sibling banner, not caller discipline.** See F-7. |
| `FileTree` | `ReviewPage.tsx:137-155` — three-way `files.isError ? ErrorBanner : files.isLoading ? Skeleton : <FileTree/>` | Exhaustive, and `useReviewFiles` is only mounted under `status === "READY"` (`ReviewPage.tsx:41`), where `id` is defined, so the disabled state is unreachable in the branch that renders it. `FileTree` also has **no empty branch** (`FileTree.tsx:37-74` maps an empty list to an empty `<nav>`); the "no file changes" empty state is owned by the page at `ReviewPage.tsx:158-162`. **SAFE.** |

**Combined ruling: the caller-discipline pattern does NOT hold.** It holds for `FileTree`, and
`ProviderPicker` is safe for a different reason than claimed, but for `RepositoryList` and
`ChangeTable` the ternary only covers `isError` — it does not cover `isPending && !isFetching`
(a *disabled* query), in which both components receive `loading={false}` and `data=[]` and
render a confidently-worded empty state that is factually false. Proven by probes A, B, C, D.

The recommended fix is one change, not four: **stop deriving `loading` from `isLoading` alone.**
Pass `loading={repositories.isLoading || !providerId}` (`SelectRepositoryPage.tsx:64`) and
`loading={changes.isLoading || repositoryQuery.isPending}` (`SelectChangesPage.tsx:133`), and
give `SelectRepositoryPage` an explicit "no providers configured" branch for the steady-state
case in probe B. Adding an `error` prop to the components is *not* required and would not have
fixed this — the bug is a loading/disabled conflation, not an error/empty conflation.

**`ChangeTable`'s "unreached empty-state branch":** it is not unreached in production. Probe C
shows `ChangeTable.tsx:35-42` renders on every first paint of `SelectChangesPage`. It is
unreached *by tests*, which is why the defect was invisible. Fixing F-2 should come with a test
that renders the page with the repository lookup pending and asserts a skeleton, not the empty state.

### Ledger item 2 — unused shadcn components

Method: `grep -rln "components/ui/<name>\"" src` excluding the component's own file, cross-checked
against JSX symbol usage (`grep -rnE '<(Card|Dialog|ScrollArea|Separator|Tooltip)\b' src | grep -v '^src/components/ui/'` → no matches outside `ui/`).

**Genuinely unused — 5 of 13:**

| File | Import sites outside `ui/` |
|---|---|
| `components/ui/card.tsx` | 0 |
| `components/ui/dialog.tsx` | 0 (its own import of `button` is why `button` still shows 9 sites) |
| `components/ui/scroll-area.tsx` | 0 |
| `components/ui/separator.tsx` | 0 |
| `components/ui/tooltip.tsx` | 0 |

**In use — 8 of 13:** `badge` (2), `button` (9), `checkbox` (1), `collapsible` (1), `input` (3),
`select` (1), `skeleton` (5), `table` (2).

These are vendored shadcn primitives, tree-shaken out of the production bundle by Vite. Deleting
them is optional housekeeping, **not** fix-before-merge. Note that deleting `dialog.tsx` also
disposes of F-4.

### Ledger item 3 — `ManualRepositoryForm` does not populate the cache

See F-6. Real consequence, graded Minor.

### Ledger item 4 — `FileDiff` / `PatchDiff` coverage: **BOUNDARY, confirmed, do not close**

`FileDiff.test.tsx:34-41` documents the reason in the source, and it checks out:
`@pierre/diffs`' `PatchDiff` renders into a `<diffs-container>` custom element's shadow DOM
(invisible to Testing Library's light-DOM queries by design) and calls `ResizeObserver`, which
jsdom does not implement. Polyfilling would assert the polyfill, not the render.

Worth recording that the boundary is narrower than "no test": `FileDiff.test.tsx:49` (the
truncated case) has `binary: false`, so it *does* mount `<PatchDiff>` (`FileDiff.tsx:28-37`) and
the test passes — the component mounts under jsdom without throwing. Only the rendered patch
*content* is unassertable. **Recorded as an accepted coverage boundary. No action.**

---

## Findings

### Critical (0)

None.

### Important (3)

**F-1 — `RepositoryList` renders a false, sometimes permanent, empty state whenever the repositories query is disabled.**
`pages/SelectRepositoryPage.tsx:62-66` · `lib/hooks/api/useRepositories.ts:18` · `components/features/repositories/RepositoryList.tsx:30-37`

`useRepositories` is `enabled: Boolean(providerId)` and `providerId` is derived at
`SelectRepositoryPage.tsx:22` as `selectedProviderId ?? providers.data?.[0]?.id`. When the query
is disabled, React Query reports `isPending: true, isFetching: false`, therefore
`isLoading === false` and `data === undefined`. The page passes `loading={false}` and
`repositories={[]}`, and `RepositoryList.tsx:30` renders *"No repositories — This token cannot
see any repositories on this provider."* Three reachable cases:

- **Transient (every page load):** shown for the whole duration of the `/api/providers` fetch. Probe A.
- **Permanent (zero providers configured):** `providers.data` is `[]`, so `providerId` is
  `undefined` forever, `isError` is false, and **no error banner is rendered at all** — the user
  is told their token cannot see repositories on a provider they never selected. Probe B.
- **Contradictory (providers request fails):** the provider `ErrorBanner` ("The token was
  rejected.") and the "cannot see any repositories" empty state are in the DOM *simultaneously*,
  asserting two different causes for one failure. Probe D.

The existing test at `SelectRepositoryPage.test.tsx:120` (`queryByText(/no repositories/i)` is
absent) only covers the case where providers *succeeded* and repositories *errored*, which is
why this survived. Fix: `loading={repositories.isLoading || !providerId}` at line 64, plus an
explicit zero-providers branch.

**F-2 — `ChangeTable` renders a false empty state on every first paint of `SelectChangesPage`.**
`pages/SelectChangesPage.tsx:58,131-136` · `lib/hooks/api/useChanges.ts:20` · `components/features/changes/ChangeTable.tsx:35-42`

Same mechanism. `useChanges` is gated on `!repositoryQuery.isPending` (line 58 — deliberate, and
the reasoning in the comment at lines 53-57 is sound), but while that gate is closed the query is
disabled, so `isLoading` is `false` and `changes.data` is `undefined`. `ChangeTable` receives
`loading={false}, changes={[]}` and renders *"No merged PRs/MRs — Nothing matches this base
branch and search."* — before any search or base branch has been applied, and before any request
was made. `placeholderData: keepPreviousData` (`useChanges.ts:21`) does not help on first mount.
Probe C. Fix: `loading={changes.isLoading || repositoryQuery.isPending}` at line 133.

This is compounded by F-6: arriving from `ManualRepositoryForm` always takes this path, because
the repository detail is not in the cache and must be refetched.

**F-3 — No `cursor-pointer` anywhere; every clickable surface in the app fails FE-15's acceptance test.**
`components/ui/button.tsx:7` (cva base) · `src/index.css:127-133` (no cursor rule) · `node_modules/tailwindcss/preflight.css` (only occurrence of "cursor" is a comment at line 384)

FE-15's rule text assumes "native `<button>` and `<a>` elements get a pointer from the browser."
That assumption is false here. No UA stylesheet sets `cursor: pointer` on `<button>` — Tailwind
v3's preflight added it, and **Tailwind v4 removed it**. This project is on `tailwindcss@4.3.3`
(verified: `node -p "require('./node_modules/tailwindcss/package.json').version"` → `4.3.3`), its
preflight contains no button cursor rule, `src/index.css` adds no replacement, and
`button.tsx:7`'s cva base string does not include `cursor-pointer`.

Consequence: every `Button` (`SelectionBar.tsx:17,20`, `Pagination.tsx:13,22`,
`ErrorBanner.tsx:20`, `ReviewHeader.tsx:25`, `ReviewErrorPanel.tsx:44,86`,
`RepositoryList.tsx:57`, `ManualRepositoryForm.tsx:57`, `ReviewPage.tsx:123`), the
`SelectTrigger` (`ProviderPicker.tsx:34`), every row `Checkbox` (`ChangeTable.tsx:63`), and every
file button in `FileTree.tsx:50` shows the default arrow cursor on hover. Applying the
guideline's stated acceptance test — "hover with a pointing device; the cursor must change to a
pointer" — this fails app-wide.

Fix is one line, either `cursor-pointer` in the `button.tsx:7` cva base (plus `select.tsx:46`'s
trigger and `checkbox.tsx:14`), or a `@layer base` rule in `index.css` restoring the v3 behaviour
for `button:not(:disabled), [role="button"]:not(:disabled)`.

### Minor (4)

**F-4 — Hardcoded color in the dialog overlay.** `components/ui/dialog.tsx:42` uses `bg-black/10`
rather than a theme token, violating FE-06. Moot in practice — `dialog.tsx` has zero import sites
(see ledger item 2) — so deleting the file resolves it.

**F-5 — Five unused vendored shadcn components.** `card`, `dialog`, `scroll-area`, `separator`,
`tooltip` under `components/ui/`. Tree-shaken from the bundle; dead source only.

**F-6 — `ManualRepositoryForm` fetches server state outside React Query.**
`components/features/repositories/ManualRepositoryForm.tsx:37` calls `repositoriesService.get()`
directly in the submit handler. This satisfies FE-03 (it uses the service layer, not the API
client) but breaks the skill's Key Principle 3, "all server data managed through TanStack React
Query hooks." Real user-visible consequence, and it is small but not nil: the fetched
`Repository` is never written to `repositoryKeys.detail(providerId, fullName)`
(`useRepositories.ts:10-11`), so the immediate `navigate("/select?…")` at
`SelectRepositoryPage.tsx:28` lands on `SelectChangesPage`, whose `useRepository`
(`SelectChangesPage.tsx:36`) finds a cold cache and re-requests the resource that was just
successfully fetched. During that second round trip the base-branch input is empty
(`SelectChangesPage.tsx:44,108`) and — because of F-2 — the change list shows "No merged PRs/MRs".
The manual-entry path therefore reproduces F-2 100% of the time. Fix: either use a
`useMutation`/`queryClient.setQueryData(repositoryKeys.detail(...), repo)` on success, or accept
the duplicate fetch once F-2 is fixed and the interim state is a skeleton.

**F-7 — Ledger correction: `ProviderPicker` is not guarded by a ternary.**
`pages/SelectRepositoryPage.tsx:37-52`. The provider `ErrorBanner` and the `ProviderPicker` are
siblings, so on a providers failure the user sees the banner *and* an empty `<Select>` whose
placeholder reads "Select a provider". This is acceptable — the error is surfaced and the empty
dropdown asserts nothing false — but the ledger's characterisation ("safe by exhaustive ternary")
does not describe this call site. Recording the correction so the next reader does not rely on a
guard that isn't there. No code change required.

---

## Summary

### Blocking (fix before merge)
- **F-1** (FE-05/architecture) `pages/SelectRepositoryPage.tsx:62-66` — `RepositoryList` shows
  "This token cannot see any repositories on this provider" while the repositories query is
  disabled; permanent and error-banner-free when zero providers are configured.
- **F-2** (FE-05/architecture) `pages/SelectChangesPage.tsx:131-136` — `ChangeTable` shows
  "No merged PRs/MRs" on every first paint, before any request is issued.
- **F-3** (FE-15) `components/ui/button.tsx:7` + `src/index.css` — no `cursor-pointer` anywhere;
  Tailwind v4 does not restore v3's button pointer cursor, so every clickable surface fails
  FE-15's stated acceptance test.

### Non-Blocking (should fix)
- **F-4** (FE-06) `components/ui/dialog.tsx:42` — `bg-black/10` instead of a theme token.
- **F-5** `components/ui/{card,dialog,scroll-area,separator,tooltip}.tsx` — unused, deletable.
- **F-6** `components/features/repositories/ManualRepositoryForm.tsx:37` — server state fetched
  outside React Query; duplicate round trip on navigate, and deterministically triggers F-2.
- **F-7** `pages/SelectRepositoryPage.tsx:37-52` — ledger correction, no code change.

### Accepted boundaries (no action)
- `FileDiff`'s `PatchDiff` rendered output is unassertable under jsdom (shadow DOM +
  `ResizeObserver`). `FileDiff.test.tsx:34-41` documents it; the component is still *mounted*
  under test at line 49. Confirmed as a coverage boundary, not a gap.
- Plain service objects instead of `BaseService`, Vitest instead of Jest, thin `fetch` wrapper
  instead of a caching client — all three sanctioned by `CLAUDE.md` → Architecture Notes.
