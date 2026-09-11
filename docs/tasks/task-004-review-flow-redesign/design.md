# Review Flow Redesign — Design

Task: task-004-review-flow-redesign
PRD: `docs/tasks/task-004-review-flow-redesign/prd.md` (approved)
Mockup: `docs/mockups/d-mix.html`
Status: Proposed
Created: 2026-09-10

---

## 1. Summary

This design implements the PRD on top of the existing three routes. It is
mostly a frontend rebuild of the page bodies, with the following backend
additions:

- `search` on `GET /api/providers/{p}/repositories`, threaded through
  `GitProvider.ListRepositories`.
- A new `GET /api/providers/{p}/repositories/{r}/branches` endpoint backed by
  a new `GitProvider.ListBranches` and a `provider.Branch` value type.
- One small change in `internal/diff`: the per-file diff is produced with a
  larger context window so the review page can fold and expand unchanged
  lines without a second request (see §5.4 for why this is unavoidable).

Everything else the PRD asks for (recent repositories, ticket grouping, bot
filtering, author chips, apply order, viewed state, keyboard shortcuts,
folds, word-level highlighting, breadcrumbs) is derived or stored in the
browser.

The frontend is organised around four new library modules with no React
dependency (`lib/storage`, `lib/changes`, `lib/review/fileTree`,
`lib/hotkeys`) so the derivations are unit-tested in isolation, and pages
become thin composers of feature components.

## 2. Decisions on the PRD's open questions

| Question | Decision | Why |
|---|---|---|
| GitHub repository search: page-walk `/user/repos` or the search API? | **Page-walk `/user/repos`** with `per_page=100`, capped at 10 pages, substring filter on `full_name`. | The search API is limited to 30 requests/minute for all search endpoints (verified against the GitHub REST docs), which a debounced type-ahead can exhaust. Scoping it to the token's affiliations requires enumerating every org first, and private-repo visibility differs by token type. `/user/repos` already returns exactly the set the unsearched list shows, so results are consistent with the browse view. |
| Container `max-width` | **1280 px** (`max-w-[80rem]`), horizontal padding 24 px. | Matches the mockup. On a 1440 px display the diff pane gets ~950 px, enough for 120-column code at 13 px monospace. Wider hurts the root and create tables more than it helps the diff. |
| "Inspect" for conflicted reviews | **Navigation only.** Same target as Resume; the existing `ReviewErrorPanel` is the inspection surface. | Nothing in the PRD needs more, and the panel already shows conflicting files and diagnostics. |

Two further deviations from the PRD, both forced by verified facts:

- **Error codes.** The PRD names `404 REPOSITORY_NOT_FOUND` and
  `502 PROVIDER_ERROR`. Neither exists in `internal/api/errors.go`. The
  branches handler uses the existing mapping: `provider.ErrNotFound` →
  `404 NOT_FOUND`, `provider.ErrAuth` → `502 PROVIDER_AUTH`,
  `provider.ErrUnavailable` → `503 PROVIDER_UNAVAILABLE`. No new codes.
- **"Open in provider" link target (FR-33).** The `files` endpoint does not
  attribute files to changes, and per-line attribution is a PRD non-goal.
  The link targets the change's web URL when the review includes exactly
  **one** change, otherwise the repository's web URL.

## 3. Backend

### 3.1 Provider interface

```go
// provider.go
ListRepositories(ctx context.Context, search string, page Page) (Slice[Repository], error)
ListBranches(ctx context.Context, repo Repository, search string, page Page) (Slice[Branch], error)
```

`search` is an already-trimmed, already-length-checked string; empty means
"no filter". The API layer owns validation (§3.4); providers never see an
untrimmed or oversized value.

Alternative considered: a `RepositoryQuery{Search string; Page Page}` struct.
Rejected as premature. `ListMergedChanges` already takes `search string,
page Page` positionally; matching that shape keeps the interface uniform.

### 3.2 `provider.Branch`

```go
// model.go
type Branch struct {
    name      string
    sha       string
    isDefault bool
}
func (b Branch) Name() string    { return b.name }
func (b Branch) SHA() string     { return b.sha }
func (b Branch) IsDefault() bool { return b.isDefault }

// builder.go
type BranchBuilder struct{ b Branch }
func NewBranchBuilder() *BranchBuilder
func (b *BranchBuilder) SetName(v string) *BranchBuilder
func (b *BranchBuilder) SetSHA(v string) *BranchBuilder
func (b *BranchBuilder) SetDefault(v bool) *BranchBuilder
func (b *BranchBuilder) Build() (Branch, error)   // ValidateBranchSyntax(name); ValidateSHA(sha) when non-empty
```

Same immutable getter + chainable builder shape as `Repository`. `Build`
reuses `gitx.ValidateBranchSyntax` and `gitx.ValidateSHA` so a provider can
never hand the API a branch name that would later be rejected by
`POST /api/reviews`.

### 3.3 Provider implementations

**Shared helper.** A new unexported-by-package but exported-from-`provider`
helper avoids duplicating the page-walk in GitHub twice (repositories and
branches):

```go
// provider/filterwalk.go
// FilterWalk pages through fetch (1-based) collecting items for which keep
// returns true, skipping (page.Number-1)*page.Size matches, and returning
// page.Size matches plus HasNext. It stops after maxPages upstream pages;
// if it stops because of the cap, HasNext is false.
func FilterWalk[T any](ctx context.Context, page Page, maxPages int,
    fetch func(ctx context.Context, upstreamPage int) ([]T, bool, error),
    keep func(T) bool) (Slice[T], error)
```

`HasNext` is true when one more match than needed was seen, or when the
upstream said it had more pages and the cap was not reached. Each call
walks from upstream page 1 again; with `pageSize ≤ 100` and a 10-page cap
that is at most 10 upstream calls per request, which is acceptable for a
type-ahead that fires at most every 250 ms and is cancelled on change.

**GitHub.**
- `ListRepositories`: unchanged when `search == ""`. Otherwise
  `FilterWalk` over `GET /user/repos?affiliation=…&sort=full_name&per_page=100`
  with `keep = strings.Contains(lower(full_name), lower(search))`,
  `maxPages = 10` (`const maxSearchPages`).
- `ListBranches`: `GET /repos/{owner}/{repo}/branches?per_page&page`. The
  endpoint has no name filter (verified), so `search != ""` uses `FilterWalk`
  with `per_page=100` and the same cap; `search == ""` is a direct page.
  `isDefault = name == repo.DefaultBranch()` because GitHub's branch payload
  carries no default flag. Mapping `branchJSON{name, commit{sha}}` →
  `BranchBuilder`.

**GitLab.**
- `ListRepositories`: adds `search=<q>&search_namespaces=true` when
  non-empty. `search_namespaces` (verified in the GitLab projects docs)
  makes `atlas/serv` match `atlas/server`; without it the search is
  restricted to the project's own name and path.
- `ListBranches`: `GET /projects/:id/repository/branches?search=<q>&per_page&page`.
  GitLab's `search` is a substring match with optional `^`/`$` anchors;
  the API layer does not strip those characters, they are harmless.
  Mapping `branchJSON{name, default, commit{id}}` → `BranchBuilder`.

**Default-first ordering.** Provider results are returned in upstream
order. The API handler moves the default branch to the front of **page 1**
when it is present in that page. This is the only ordering guarantee the
backend makes; the frontend independently pins the repository's
`defaultBranch` at the top of the select (§4.6) so the guarantee is
sufficient. Fetching the default branch separately to force it onto page 1
was considered and rejected: one extra upstream call per request for a
branch the client already knows.

**Fake.** `AddBranch(repo string, b Branch)`; `ListBranches` sorts by name,
sets the default first, filters by substring, paginates with the existing
`paginate` helper. `ListRepositories` gains substring filtering. The api
fixture seeds `main`, `develop`, and `release/1.0` for `atlas/server`.

**Tests.** Extend the existing `httptest` + `testdata` pattern. New
fixtures: `github/testdata/{repos_p1.json, repos_p2.json, branches.json}`,
`gitlab/testdata/{projects_search.json, branches.json}`. GitHub tests assert
the walk stops at the cap and that `HasNext` reflects "one more match seen".
GitLab tests assert the exact query string (`search`, `search_namespaces`).
`FilterWalk` gets table tests of its own in `provider/filterwalk_test.go`.

### 3.4 API layer

```go
// api/search.go
const maxSearchLen = 200
// searchFrom trims ?search and returns INVALID_SEARCH when it exceeds 200 runes.
func searchFrom(r *http.Request) (string, error)
```

- `listRepositories` calls `searchFrom`; on error writes
  `400 INVALID_SEARCH` inline (same style as `INVALID_STATE` in
  `changes.go`). `listChanges` is left as-is per PRD §5.3 "unchanged".
- New `api/branches.go`: `listBranches` handler, `branchAttributes{name,
  isDefault, sha}`, `branchResource` with id `<provider>:<repo>:<name>`.
  Route `GET /api/providers/{provider}/repositories/{repo}/branches` in
  `router.go` beside the changes route. Resolves the repository through
  `p.GetRepository` (as `listChanges` does) so an unknown repo is
  `404 NOT_FOUND` before any branch call. Logs at debug with provider,
  repository, page, matching `listChanges`.
- `api_test.go` gains: repositories search hit/miss, `INVALID_SEARCH` on a
  201-rune value, branches list with default first, branches paging,
  branches on an unknown repository, and `FailWith` → status mapping.

### 3.5 `internal/diff` context window

`FileContent` currently runs `git diff --find-renames base head -- path`,
which emits three lines of context per hunk. FR-34 asks for "Expand N
unmodified lines" rows at the top and bottom of each file computed from the
already returned diff. With three lines of context there is nothing to
expand. The choice is between a second endpoint that returns full file
contents (which the PRD rules out) and a wider context window.

Decision: `FileContent` adds `-U<n>` with `n = 40`
(`const fileDiffContext = 40`). The combined diff written by `WriteCombined`
and the numstat used by `Summarize` are untouched, so totals and the
downloadable diff do not change. The 1 MiB truncation cap still applies.
The diff library folds runs longer than `collapsedContextThreshold` and
lets the user expand them `expansionLineCount` lines at a time, so the
frontend needs no fold code of its own.

The PRD lists `diff` under "no change"; this is a one-argument deviation
and is called out here so the plan does not silently absorb it.

## 4. Frontend architecture

### 4.1 Module map

```
src/
  components/
    layout/
      AppShell.tsx              brand block, breadcrumb slot, theme toggle, shared container
      Breadcrumbs.tsx           renders segments from BreadcrumbContext
      BrandMark.tsx             the square logo mark
    common/
      Hotkey.tsx                <kbd> chip
      ProgressBar.tsx           thin wrapper on ui/progress with label
      ...existing
    features/
      reviews/                  root page table
        ReviewsTable.tsx
        ReviewRow.tsx
        ReviewProgressCell.tsx
        NewReviewRow.tsx
        DiscardDialog.tsx
      newReview/                the Sheet
        NewReviewSheet.tsx
        ProviderSelect.tsx      (moved from features/providers/ProviderPicker.tsx)
        RepositorySearch.tsx    input + Enter-to-resolve
        RepositoryResults.tsx   list, recents first
      changes/                  create page
        BaseBranchSelect.tsx    Popover + Command over useBranches
        ChangeFilters.tsx       search, author chips, bots switch, group toggle, count
        ChangeTable.tsx         (rewritten: grouped rows, hidden-bots row, header checkbox)
        ChangeRow.tsx
        TicketGroupHeader.tsx
        SelectionBar.tsx        (rewritten: sticky, apply-order chips)
      review/                   review page
        ReviewStatusLine.tsx
        IncludedChangesPopover.tsx
        ReviewWorkspace.tsx     the fixed-height split frame
        FileTree.tsx            (rewritten: real tree, viewed checkboxes, filter)
        FileTreeRow.tsx
        DiffPane.tsx            header + FileDiff + footer, owns scroll-to-top
        FileHeader.tsx
        FileDiff.tsx            (options changed; still lazy)
        FileFooter.tsx
        ReviewErrorPanel.tsx    (unchanged)
        ReviewStatus.tsx        (unchanged)
  lib/
    storage/
      store.ts                  createStore<T>(key, guard, fallback) → {get,set,subscribe}; useStore(store)
      recents.ts                converge.recentRepositories
      changeFilters.ts          converge.changeFilters
      viewed.ts                 converge.viewed.<id>; pruneViewed(activeIds)
    changes/
      ticketKey.ts              ticketKey(title) → string | null
      dependencyBot.ts          isDependencyBot(change)
      applyOrder.ts             applyOrder(changes) → Change[]
      groupByTicket.ts          groupByTicket(changes) → TicketGroup[]
      authors.ts                distinctAuthors(changes)
    review/
      fileTree.ts               buildTree(files) → TreeNode[]; flattenVisible(tree, expanded) → path[]
      progress.ts               viewedProgress(files, viewedSet)
      providerLink.ts           openInProviderHref(review, repository)
    hotkeys/
      useHotkeys.ts             useHotkeys(bindings, {enabled})
      isEditableTarget.ts
    breadcrumbs/
      context.ts, useBreadcrumbs.ts
    repositoryInput.ts          parseRepositoryInput(text, provider) → fullName | null
    hooks/
      useDebouncedValue.ts
      useSelection.ts           (unchanged)
      api/
        useRepositories.ts      (list gains search; adds useResolveRepository)
        useBranches.ts          new
        useReviews.ts           (unchanged)
  pages/
    ReviewsPage.tsx             renamed from SelectRepositoryPage.tsx
    SelectChangesPage.tsx
    ReviewPage.tsx
  services/api/
    repositories.ts             RepositoryListParams.search
    branches.ts                 branchesService.list(provider, repo, params)
  types/models/
    branch.ts
```

Deleted: `features/repositories/RepositoryList.tsx`,
`features/repositories/ManualRepositoryForm.tsx`,
`features/reviews/ResumeReviewList.tsx`, `ResumeReviewRow.tsx`,
`features/review/ReviewHeader.tsx`, `common/PageHeader.tsx` if no longer
used. The `lib/schemas/repository.ts` zod schema moves into
`lib/repositoryInput.ts` and is extended to accept provider URLs.

New shadcn components installed via `npx shadcn add`: `sheet`, `breadcrumb`,
`popover`, `command`, `progress`, `switch`, `separator`, `tooltip`,
`alert-dialog`, `scroll-area`, `kbd`. `command` brings in `cmdk` as a
dependency; no other new runtime dependencies.

### 4.2 App shell, container, and breadcrumbs

`AppShell` owns the container. `<main>` becomes
`mx-auto w-full max-w-[80rem] px-6 py-6`. Pages drop their own
`max-w-*` wrappers. The review page's split frame sizes itself with
`h-[calc(100vh-3.5rem-<status line>-3rem)]` inside that container; nothing
uses full viewport width.

Breadcrumbs are published by pages, not inferred by the shell. Three
options were weighed:

1. **Shell infers from the URL and fetches what it needs.** Rejected: the
   shell would need `useReview(id)` and `useRepository(...)`, duplicating
   the page's data dependencies and coupling the shell to every route's
   query params.
2. **React Router `handle` on each route.** Rejected: `handle` is static
   per route; the review breadcrumb needs loaded data.
3. **A `BreadcrumbContext` with `useBreadcrumbs(segments)`.** Chosen. The
   provider lives in `AppShell`; each page calls
   `useBreadcrumbs(useMemo(() => [...], [deps]))` in an effect that sets on
   mount/update and clears on unmount. The shell renders whatever is
   current, defaulting to `Reviews`.

Segment shapes: `{ label: string; to?: string }`. The review page's last
segment is `included.map(n => '#' + n).slice(0, 5).join(' · ') + (more ? ' · …' : '')`.

### 4.3 Storage layer

One tiny store abstraction, used by all three keys:

```ts
// lib/storage/store.ts
export interface Store<T> { get(): T; set(next: T | ((prev: T) => T)): void; subscribe(cb: () => void): () => void; key: string }
export function createStore<T>(key: string, isValid: (v: unknown) => v is T, fallback: () => T): Store<T>
export function useStore<T>(store: Store<T>): [T, Store<T>['set']]   // useSyncExternalStore
```

- `get` reads `localStorage`, `JSON.parse`s, applies `isValid`, and returns
  `fallback()` on any failure (missing, malformed, wrong shape, storage
  disabled). Every read is total and try/catch-wrapped, matching the theme
  storage precedent.
- `set` writes and notifies subscribers; a `storage` event listener also
  notifies so two tabs stay coherent.
- `useStore` binds with `useSyncExternalStore` so the root page's progress
  cell and the review page's viewed state are the same source of truth
  without a React context.

Key modules:

- `recents.ts`: `RecentRepository {provider, repository, defaultBranch, openedAt}`;
  `recordRecent(entry)` (move-to-front, cap 20), `recentsFor(provider, limit 10)`,
  `mostRecentProvider()`.
- `changeFilters.ts`: `{hideBots: boolean; groupByTicket: boolean}`,
  default `{hideBots: true, groupByTicket: false}`.
- `viewed.ts`: one store per review id created lazily and cached in a
  `Map`, key `converge.viewed.<id>`, value `string[]` exposed as a `Set`.
  `clearViewed(id)`, `pruneViewed(activeIds: string[])` which removes every
  `converge.viewed.*` key whose id is not in the list. The root page calls
  `pruneViewed` once per successful `GET /api/reviews`.

Theme storage (`lib/theme/*`) is left alone; it has a boot-script coupling
that this abstraction must not disturb.

### 4.4 Root page

`ReviewsPage` composes `ReviewsTable` and `NewReviewSheet`:

- `useReviews()` (unchanged polling). Rows filter out `FINISHED`/`EXPIRED`.
- `ReviewRow` receives one `Review` and reads viewed progress via
  `useStore(viewedStore(id))` and `totals.files`. Row click navigates
  unless the click target is inside the actions cell (`data-actions`
  attribute check via `closest`).
- `DiscardDialog` is a shadcn `AlertDialog`; confirm calls
  `useFinishReview().mutate(id)`, which already invalidates the list, then
  `clearViewed(id)`; failure shows a sonner toast.
- `NewReviewRow` is the dashed last row; the page holds `sheetOpen` state
  and `useHotkeys({ n: open }, { enabled: !sheetOpen })`.
- Time-left uses a new `timeLeft(expiresAt, now)` helper beside
  `relativeTime.ts`; "22h left" above one hour, "40m left" below, em dash
  while `CREATING`.

### 4.5 New-review sheet

`NewReviewSheet({open, onOpenChange})` holds `providerId`, `query`,
`selected`. State is reset in `onOpenChange(false)`.

- Initial provider: `mostRecentProvider()` if it is in `useProviders()`,
  else the first provider.
- `RepositorySearch` keeps the raw input; `useDebouncedValue(query, 250)`
  feeds `useRepositories(providerId, { search: debounced.length >= 2 ? debounced : undefined })`.
  React Query cancels superseded requests via the `AbortSignal` the fetch
  wrapper already forwards; `apiGet` gets a `signal` option if it lacks one.
- `RepositoryResults` merges: recents whose `repository` contains the
  query (or all recents when the query is empty), marked with a clock icon
  and "opened 3d ago", followed by server items minus duplicates. With
  no recents and no query, the list is the server's first page.
- Enter on the input: if the list has a highlighted row, select it. Else
  `parseRepositoryInput(text, provider)` returns a full name when the text
  is `owner/name` or a URL whose origin matches `provider.baseUrl`; the
  sheet calls `queryClient.fetchQuery(repositoryKeys.detail(...))` and on
  success selects, on `ApiError` 404 shows the inline error. Other errors
  toast.
- Highlight and Enter/arrow behaviour come from `Command`, which already
  implements roving focus, so the results list is a `CommandList` with
  `CommandItem`s rather than a hand-rolled listbox.
- `Choose changes →` navigates to `/select?provider=&repo=` and does not
  write a recent; the create page does on load (FR-16) so a deep link also
  counts.

### 4.6 Create page

State: `baseBranch` (initialised from `?base=` if valid else
`repository.defaultBranch`, seeded during render as today), `search`,
`page`, `authorFilter: Set<string>`, `filters` via `useStore(changeFilters)`,
`selection` via `useSelection(...)` (unchanged, keyed by provider/repo in
`sessionStorage`).

- `BaseBranchSelect` is a `Popover` containing a `Command`. It calls
  `useBranches(provider, repo, { search: debounced })` and renders the
  repository's default branch pinned at the top, then results excluding
  it. A typed name that matches no result is still selectable as a free
  value (the backend validates it on build), so a branch beyond the first
  page can be reached by typing. Changing the base calls
  `selection.clear()` and resets `page` to 1; `useChanges` refetches
  because `target` is in its query key.
- Derivations run in `useMemo` over `changes.items`:
  `visible = applyAuthorFilter(applyBotFilter(items))`,
  `groups = filters.groupByTicket ? groupByTicket(visible) : null`,
  `hiddenBots = items.length - afterBotFilter.length`,
  `authors = distinctAuthors(items)`.
- `ChangeTable` takes `{ rows: ChangeRow[] }` where a row is a
  discriminated union `{kind:'group', key, count, allSelected}` |
  `{kind:'change', change}` | `{kind:'hidden', count}`. Rendering one flat
  row list keeps the table markup simple and makes the grouped and flat
  modes the same component.
- `SelectionBar` is `sticky bottom-4` inside the container, rendered after
  the table so it scrolls with the page yet stays pinned. It receives
  `applyOrder(selection.selected.values())` and renders numbered chips;
  removing a chip calls `selection.toggle(change)`.
- Build: `changes: applyOrder(selected).map(c => c.attributes.number)`,
  then `recordRecent(...)`, `selection.clear()`, navigate. Enter is a
  hotkey enabled when `selection.count > 0 && !popoverOpen`.

`applyOrder` sorts by `mergedAt` ascending then `number` ascending; a null
`mergedAt` sorts last. `ticketKey` uses exactly
`/\b[A-Z][A-Z0-9]+-\d+\b/`. `isDependencyBot` lower-cases `sourceBranch`
and tests the `renovate/` and `dependabot/` prefixes.

### 4.7 Review page

The status switch is unchanged; only the `READY` branch is rebuilt:

```
<ReviewStatusLine review files viewed onFinish onDiscard />
<ReviewWorkspace>                            grid-cols-[280px_1fr], fixed height, overflow-hidden
  <FileTree files viewed selectedPath onSelect onToggleViewed />   own ScrollArea
  <DiffPane review file diffQuery viewed onToggleViewed onNext />   own ScrollArea
</ReviewWorkspace>
```

- **File order** is the tree's visible order (`flattenVisible`), which is
  what `j`/`k`, the footer button, and "File i of n" use. Collapsing a
  directory removes its files from that order; the selected file is
  always kept visible by expanding its ancestors on selection.
- **`buildTree`** produces `{kind:'dir', name, path, children}` |
  `{kind:'file', file}` nodes, sorts directories before files and both by
  name, then collapses any directory whose only child is a directory into
  one node named `a/b/c`. Expanded state is a `Set<string>` of directory
  paths held in `FileTree`, default all expanded. The filter input applies
  a substring test on the full path and shows only matching files plus
  their ancestor directories, forced open.
- **`DiffPane`** owns a `ref` to its scroll container and scrolls to top in
  a layout effect keyed on `selectedPath`. `FileTree` calls
  `scrollIntoView({block:'nearest'})` on the selected row when the
  selection source is keyboard or footer (a `selectionSource` value passed
  with `onSelect`).
- **`FileDiff`** options change to
  `{ diffStyle:'unified', expandUnchanged:false, collapsedContextThreshold:8,
  expansionLineCount:20, lineDiffType:'word', disableFileHeader:true,
  overflow:'scroll', theme… }`. The library renders the fold rows and
  intra-line word highlights; with the 40-line context from §3.5 there are
  unmodified regions to fold. `FileHeader` is ours, sticky at the top of
  the pane's scroll container.
- **Hotkeys**: `useHotkeys({ j: next, k: prev, v: toggleViewed }, { enabled: !popoverOpen })`.
- **Finish / Discard** both call `useFinishReview` then `clearViewed(id)`
  and navigate to `/`. Discard goes through `DiscardDialog`; Finish does
  not (it is the primary action and today has no confirmation).
- **Open in provider**: `openInProviderHref(review, repository)` where
  `repository` comes from `useRepository(review.provider, review.repository)`
  (cached, cheap). One included change → that change's `webUrl`; otherwise
  `repository.attributes.webUrl`.

### 4.8 Hotkeys

```ts
useHotkeys(bindings: Record<string, () => void>, opts?: { enabled?: boolean })
```

One `keydown` listener on `window` per hook instance. Ignores the event
when: `opts.enabled === false`; any of `ctrl/meta/alt` is held;
`event.defaultPrevented`; or `isEditableTarget(event.target)` (input,
textarea, select, or `contentEditable`). Radix dialogs and popovers trap
focus inside themselves, and the pages additionally pass `enabled: false`
while a sheet, dialog, or popover is open, which covers FR-41 and NFR
"disabled while a dialog is open" twice over. Matching binding calls
`event.preventDefault()` and the handler. Bindings are stored in a ref so
callers can pass inline closures without re-subscribing.

### 4.9 Services and hooks

- `RepositoryListParams` gains `search?: string`; `buildQuery` already
  drops empty strings.
- `services/api/branches.ts`: `branchesService.list(providerId, repository, { search, page, pageSize })`
  → `GET /api/providers/{p}/repositories/{encoded repo}/branches`,
  returns `PagedBranches`.
- `useBranches(providerId, repository, params, enabled)` with key
  `["branches","list",providerId,repository,params]`, `staleTime` 2 min,
  `placeholderData: keepPreviousData` so typing does not flash the list
  empty.
- `useRepositories` is unchanged in shape; the search param flows through
  its existing key.

## 5. Data flow summaries

### 5.1 Repository search

```
input → useDebouncedValue(250) → useRepositories(provider,{search})
      → GET /api/providers/p/repositories?search=q&page=1
      → api.searchFrom (trim, ≤200) → provider.ListRepositories(ctx, q, page)
      → GitHub: FilterWalk over /user/repos (≤10 pages) | GitLab: /projects?search=q&search_namespaces=true
      ← Slice[Repository] → jsonapi list → merged with recents in RepositoryResults
```

### 5.2 Branch listing

```
BaseBranchSelect open/type → useBranches(p, r, {search})
      → GET /api/providers/p/repositories/r/branches?search=q
      → api: GetRepository (404 if unknown) → ListBranches → default-to-front on page 1
      → GitHub: /repos/o/r/branches (+FilterWalk when searching) | GitLab: /repository/branches?search=q
      ← branches list; UI pins repository.defaultBranch at top regardless
```

### 5.3 Viewed state

```
checkbox / Viewed toggle / v → viewedStore(id).set(toggle path)
      → localStorage converge.viewed.<id> + subscribers
      → FileTree (muted row), ReviewStatusLine (V / F), ReviewsPage row progress
Finish/Discard → clearViewed(id); ReviewsPage load → pruneViewed(active ids)
```

### 5.4 Folds

The per-file diff arrives as one unified patch with 40 lines of context.
The diff library parses it, collapses unchanged runs longer than 8 lines,
renders "Expand N lines" affordances, and expands from lines already in the
patch. No frontend fold computation and no second request. Lines beyond
the 40-line window are not expandable; a full-file endpoint is the natural
follow-up if that limit proves annoying and is out of scope here.

## 6. Error handling

- Backend: `INVALID_SEARCH` (400) is the only new code. Everything else
  reuses `classify`. `FilterWalk` returns the first upstream error
  unwrapped so the sentinel mapping still works.
- Frontend list queries render `ErrorBanner` with retry, as today.
  Mutations toast via sonner. The sheet's Enter-resolve shows a 404 inline
  and toasts other failures. Storage failures are silent and fall back to
  defaults; they never throw into render.
- A `?base=` that is not a syntactically valid branch is ignored and the
  default branch is used; the backend still validates on build.

## 7. Testing

**Go.**
- `provider/filterwalk_test.go`: skip/take arithmetic, cap, `HasNext`
  cases, error propagation.
- `provider/builder_test.go`: `BranchBuilder` validation.
- `github/client_test.go`, `gitlab/client_test.go`: search query strings,
  branches mapping, `isDefault` derivation, paging headers.
- `fake/fake_test.go`: substring filtering and branch ordering.
- `api/api_test.go`: the six cases in §3.4.
- `diff/diff_test.go`: `FileContent` passes `-U40` (assert on the recorded
  argument slice via the existing runner fake) and folds survive
  truncation.

**Vitest.** Pure modules get table tests: `ticketKey`, `dependencyBot`,
`applyOrder` (null `mergedAt`, ties), `groupByTicket` (ordering by newest,
"No ticket" last), `fileTree` (single-child collapse, sort, filter,
`flattenVisible` with collapsed dirs), `storage/store` (malformed JSON,
wrong shape, storage throwing), `recents` (move-to-front, cap),
`viewed` (prune), `hotkeys` (editable target, modifiers, enabled flag),
`parseRepositoryInput`, `timeLeft`.

Component and page tests use the existing MSW server and
`renderWithProviders`: root table rows and actions, `n` opens the sheet,
Escape closes and resets, sheet search flow with recents-first merge,
Enter-resolve 404 inline error, base select refetch with `target`, bots
hidden row with Show, group headers with Select all, author chips,
selection bar chips in apply order and `POST` body order, review page tree
selection, viewed toggle persisting across remount, `j`/`k`/`v`, footer
wrap-around, Finish clearing viewed state, breadcrumbs per route.

`FileDiff` tests keep the existing approach of mocking `@pierre/diffs/react`
and asserting the options object, plus the binary and truncated branches.

## 8. Risks and mitigations

- **GitHub page walk latency.** Ten calls worst case per keystroke burst.
  Mitigated by the 250 ms debounce, request cancellation, React Query
  caching per query string, and the cap. If it proves slow, the cap is one
  constant.
- **Wider diff context grows payloads.** 40 lines each side per hunk; the
  1 MiB cap and truncation banner already bound the worst case.
- **Fixed-height split on short viewports.** The frame has a
  `min-h-[24rem]`; below that the page scrolls as a whole.
- **Renaming `SelectRepositoryPage`.** Touches `routes.tsx` and two test
  files; a straightforward move with no alias, per project convention.
- **`cmdk` list virtualisation.** Branch lists are paged at 50 and repo
  results at 30, so no virtualisation is needed.

## 9. Out of scope (restated)

Split diff view, per-line change attribution, server-side persistence of
viewed state or recents, configurable ticket or bot patterns, and any change
to review building, sessions, sweeping, or the applicator. The
`review`, `session`, `mirror`, `workspace`, and `gitx` packages are untouched.
