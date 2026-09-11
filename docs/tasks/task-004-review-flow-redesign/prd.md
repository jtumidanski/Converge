# Review Flow Redesign — Product Requirements Document

Version: v1
Status: Draft
Created: 2026-09-10
---

## 1. Overview

Converge's three screens work, but each makes the reviewer do more scanning than
necessary. The root page asks for a provider, then an `owner/name`, then shows a
thousand-row repository table when the reviewer almost always wants one of the same three
repositories. The change-selection page lists every merged MR flat, so renovate noise
buries the human work, and the reviewer cannot see what they have selected or in what
order it will be applied until they press "Build Review". The review page uses the full
viewport while the other pages are centred, truncates long Java paths in a flat file
list, has no notion of which files have already been read, and forces a trip back to the
sidebar after every file.

This task implements the agreed direction captured in `docs/mockups/d-mix.html` (with
`index.html`, `a-dashboard.html`, `b-stepper.html`, and `c-dense.html` kept as the
explored alternatives). It is mostly frontend work on the existing three routes, plus two
small backend additions that the frontend cannot fake: repository search on the provider
listing, and a branch listing per repository. Everything else (ticket grouping, bot
filtering, author chips, viewed state, recent repositories) is computed or stored in the
browser.

The result should feel like one product with one page width, one header pattern, and
one primary action per screen.

## 2. Goals

Primary goals:
- Reach any recently used repository from the root page in two interactions (open drawer, pick row).
- Find a repository by substring across all of a provider's repositories, not just the loaded page.
- Make the composition of a review (base, changes, apply order) visible before it is built.
- Let a reviewer group and filter merged changes by ticket, author, and bot-ness without leaving the page.
- Give the review page a persistent sense of progress (viewed files) and forward motion (next file).
- Use one centred page container and one header layout across all three routes.

Non-goals:
- Split (side-by-side) diff view.
- Per-line attribution of changes to the MR that introduced them (mockup C).
- Keyboard shortcuts beyond next/previous file and mark-viewed.
- Server-side persistence of viewed state or recent repositories.
- Any change to review building, session lifecycle, sweeping, or the applicator.
- Configurable ticket-key patterns or bot author lists.

## 3. User Stories

- As a reviewer, I want the root page to list my open reviews with progress so that I can pick up where I left off.
- As a reviewer, I want to start a new review from the same table so that the entry point is obvious and does not push the resume list down the page.
- As a reviewer, I want to type part of a repository name and see matches from the whole provider so that I never page through unrelated projects.
- As a reviewer, I want my recently used repositories listed first so that the common case needs no typing.
- As a reviewer, I want to hide dependency-bot MRs so that human changes are not buried.
- As a reviewer, I want to group merged changes by ticket key and select a whole group so that reviewing one feature is one click.
- As a reviewer, I want to filter the loaded changes by author so that I can focus on one person's work.
- As a reviewer, I want to pick the base branch from a list so that I cannot mistype it.
- As a reviewer, I want to see the selected changes in apply order before building so that I know exactly what the combined diff will contain.
- As a reviewer, I want a directory tree of changed files with status and line counts so that long paths are readable.
- As a reviewer, I want to mark files as viewed and see a progress indicator so that I know what is left.
- As a reviewer, I want a "next file" control at the bottom of each diff so that I can read straight through.
- As a reviewer, I want the same page width and header on every screen so that navigation feels consistent.

## 4. Functional Requirements

### 4.1 App shell and header

- FR-1. The top bar has a brand block on the left containing a square logo mark and the word "Converge", visually separated from the rest of the bar by a right border and a distinct background. Clicking the brand navigates to `/`.
- FR-2. To the right of the brand block is a shadcn `Breadcrumb`. Segments per route:
  - `/`: `Reviews`
  - `/select?provider=P&repo=R`: `Reviews / <provider displayName> / <R>`; the first segment links to `/`.
  - `/reviews/:id`: `Reviews / <repository> / <#n · #m · …>`; the first segment links to `/`. The last segment lists the included change numbers separated by ` · `, truncated with an ellipsis after five.
- FR-3. The theme toggle remains at the far right of the top bar.
- FR-4. All three routes render inside the same centred container with a single `max-width` (design to pick a value in the 1200–1320 px range) and identical horizontal padding. No route uses full viewport width.

### 4.2 Root page (`/`)

- FR-5. The page heading is "Combined review" with a one-line subtitle. Below it is one card titled "Reviews" containing a table.
- FR-6. Each active session (from `GET /api/reviews`) is one row with: a status dot, repository full name in bold with provider id and base branch muted beside it, a second line listing included change numbers plus file count and `+adds −dels` totals, a progress cell, a time-left cell, and an actions cell.
  - Status dot colours: green for `READY`, amber for `CREATING`, red for `CONFLICTED` and `FAILED`.
  - Progress cell: for `READY`, "N of M files viewed" with a progress bar driven by the browser viewed-state (FR-31). For `CREATING`, the word "Building" and the current `stage` if present, with an indeterminate bar. For `CONFLICTED`/`FAILED`, the error code.
  - Time-left cell: humanised `expiresAt` (for example "22h left", "40m left"); an em dash while `CREATING`.
  - Actions: `Discard` (ghost) and `Resume` for `READY`; `Discard` and `Inspect` for `CONFLICTED`/`FAILED` (both navigate to `/reviews/:id`); a disabled `Open` for `CREATING`.
- FR-7. Clicking anywhere on a `READY`, `CONFLICTED`, or `FAILED` row (other than the action buttons) navigates to `/reviews/:id`.
- FR-8. `Discard` calls `DELETE /api/reviews/{id}` after a confirmation dialog and removes the row on success, with a toast on failure. The existing task-003 behaviour is preserved.
- FR-9. `FINISHED` and `EXPIRED` sessions are not listed.
- FR-10. The last row of the table is a "Start a new review" row rendered with a dashed plus icon and muted text. Clicking it, or pressing `n` when focus is not in an input, opens the new-review drawer (4.3). When there are no active sessions this row is the only row and the empty message reads "No open reviews".

### 4.3 New-review drawer

- FR-11. The drawer is a shadcn `Sheet` anchored to the right, roughly 520 px wide, with a header ("New review" plus subtitle), a scrollable body, and a footer with `Cancel` and a primary `Choose changes →` button.
- FR-12. Body section "Provider": a shadcn `Select` of providers from `GET /api/providers`, showing kind badge and display name. The initially selected provider is the most recently used one from browser storage (FR-16), else the first in the list.
- FR-13. Body section "Repository": a search input and a results list.
  - With an empty search, the list shows up to ten recently used repositories for the selected provider (FR-16), each with full name, default branch, and a relative "opened …" label. If there are none, it shows the first page of `GET /api/providers/{p}/repositories`.
  - With a non-empty search (debounced 250 ms, minimum two characters), the list shows results from `GET /api/providers/{p}/repositories?search=<q>` (5.1). Recent repositories whose full name contains the query are shown first, marked, and deduplicated against server results.
  - If the input looks like a full repository name (`owner/name` with at least one slash and no spaces) or a URL under the provider's base URL, pressing Enter resolves it directly via `GET /api/providers/{p}/repositories/{repo}` and selects it if found; a not-found result shows an inline error under the input.
- FR-14. Selecting a row highlights it and enables `Choose changes →`, which navigates to `/select?provider=P&repo=R`. Pressing Enter on a highlighted row does the same.
- FR-15. Escape, `Cancel`, and clicking the scrim close the drawer without side effects. Drawer state (provider, query, selection) is reset on close.
- FR-16. Recent repositories are stored in browser `localStorage` under one key as a list of `{ provider, repository, defaultBranch, openedAt }`, most recent first, capped at twenty entries. An entry is written or moved to the front when the create page loads for that repository and when a review is built. The most recently used provider is derived from the first entry.

### 4.4 Create page (`/select`)

- FR-17. The page header shows the repository full name as the title, the subtitle "Select the merged changes to review together. They are applied in merge order onto the base.", and on the right a labelled `Base` select.
- FR-18. The base select is populated from `GET /api/providers/{p}/repositories/{r}/branches` (5.2), preselects the repository's `defaultBranch`, and is searchable (shadcn `Command` inside a `Popover`) because repositories may have hundreds of branches. Changing the base refetches the change list with `target=<branch>` and clears the selection. A `?base=` query parameter, when present and valid, overrides the default.
- FR-19. A filter row above the table contains, left to right: a search input (server-side `search` as today, debounced), one author chip per distinct author in the currently loaded pages, a vertical separator, a "Hide dependency bots" switch chip, and a "Group by ticket" toggle chip. A muted count on the right reads "X of Y shown" where Y is the number of loaded changes and X the number after client-side filters.
- FR-20. Author chips are multi-select. With none active, all authors are shown. Chips are derived from and filter only the loaded pages; loading another page may add chips.
- FR-21. A change is a dependency bot change when its `sourceBranch` starts with `renovate/` or `dependabot/` (case-insensitive). With "Hide dependency bots" on (the default, remembered in `localStorage`), bot changes are removed from the table and a single muted summary row reads "N changes hidden by 'Hide dependency bots'" with a `Show` button that turns the switch off.
- FR-22. Ticket key extraction: the first match of `/\b[A-Z][A-Z0-9]+-\d+\b/` in the title. Changes with no match belong to a "No ticket" group.
- FR-23. With "Group by ticket" on (default off, remembered in `localStorage`), the table shows a group header row per ticket key, ordered by the newest change in each group, containing the key as a badge, "N changes", and a `Select all N` ghost button (or `Deselect all` when every change in the group is selected). Changes within a group keep server order. The "No ticket" group is last. With grouping off, the table is flat as today with the ticket key shown as a badge before the title.
- FR-24. Table columns: checkbox, number, title, author (avatar initials plus name), merged (absolute date, with time when within the last 48 hours), source branch (monospace, truncated). The header checkbox selects or clears all currently visible, non-hidden changes and shows an indeterminate state when some are selected.
- FR-25. Clicking anywhere on a row toggles its selection, except on links.
- FR-26. Pagination stays server-driven with `Newer` and `Older` buttons and a "Page N" label, using the existing `page` and `pageSize` parameters. Selections persist across pages.
- FR-27. A selection bar is fixed to the bottom of the viewport (sticky within the container) once at least one change is selected. It shows "N selected · applied oldest → newest", a `Clear` ghost button, a primary `Build review` button with an Enter hint, and one chip per selected change numbered in apply order (ascending merge time, then ascending number) with number, truncated title, and a remove control. With nothing selected the bar collapses to a single muted line "Select one or more changes".
- FR-28. `Build review` posts `{ provider, repository, baseBranch, changes }` as today, with `changes` sorted in apply order, then navigates to `/reviews/:id`. Pressing Enter with focus outside an input triggers the same when the selection is non-empty.

### 4.5 Review page (`/reviews/:id`)

- FR-29. Below the top bar, inside the shared container, a status line shows: status badge; ticket badge (first key extracted from the included titles, if any) followed by one badge per included change number and a `details ▾` popover listing each included change with title, author, merged date, and strategy; a separator; "Base `<branch> @ <sha7>` · <baseDescription>"; then on the right the totals "F files · +A −D", a progress bar with "V / F viewed", a `Discard` ghost button, and a primary `Finish review` button. `CONFLICTED`, `FAILED`, and `CREATING` keep today's dedicated panels in place of the file layout.
- FR-30. The file layout is one bordered card containing a 280 px file tree on the left and the diff pane on the right, together filling the viewport height below the status line. Each side scrolls independently.
- FR-31. Viewed state: a set of file paths per review id stored in browser `localStorage` under one key, removed when the review is finished or discarded. The tree shows a checkbox per file; the diff pane shows a `Viewed` toggle in the file header. Pressing `v` toggles the current file. Progress counts viewed files over total files.
- FR-32. The file tree groups files by directory. Directories with a single child directory are collapsed into one path segment (`a/b/c`). Each directory row shows a chevron and is collapsible; each file row shows the viewed checkbox, a status letter (`M` amber, `A` green, `D` red, `R` blue), the file name, and `+adds −dels`. The selected file has a filled background and bold, brighter text; there is no left accent bar. Viewed files render muted. A filter input above the tree narrows rows by substring on the full path.
- FR-33. The diff pane has a sticky file header with the status letter, full path with the file name in bold, `+adds −dels`, `Copy path`, `Open in <provider> ↗` (links to the change's web URL when the file belongs to exactly one included change, otherwise to the repository web URL), and the `Viewed` toggle.
- FR-34. Unchanged regions between hunks collapse into "Expand N unmodified lines" rows at the top and bottom of the file. Expanding is client-side from the already returned diff; no new request.
- FR-35. Word-level intra-line highlighting is shown for paired modified lines.
- FR-36. The pane ends with a footer: "File i of n" on the left and a `Next file: <name> →` button on the right that selects the next file in tree order; on the last file the button reads `Back to first file`. `j` and `k` select the next and previous file when focus is outside an input.
- FR-37. Selecting a file scrolls the diff pane to the top. The tree scrolls the selected row into view when selection changes by keyboard or footer button.
- FR-38. `Finish review` and `Discard` both call `DELETE /api/reviews/{id}` as today, clear the review's viewed state, and navigate to `/`.

### 4.6 Shared behaviour

- FR-39. Loading states use skeletons shaped like the final layout (table rows, tree rows, diff lines). Submit buttons show an inline spinner while pending.
- FR-40. Errors from list endpoints render the existing `ErrorBanner` with retry; mutation errors use toasts.
- FR-41. All new interactive elements are keyboard reachable and have accessible names. Keyboard shortcuts never fire while focus is in an input, textarea, or select.

## 5. API Surface

All endpoints are JSON:API as elsewhere in the backend. Existing endpoints are unchanged unless listed.

### 5.1 Repository search (modified)

`GET /api/providers/{provider}/repositories?search=<q>&page=&pageSize=`

- New optional `search` query parameter. When absent, behaviour is unchanged.
- When present, the provider implementation forwards it:
  - GitLab: `GET /projects?membership=true&search=<q>&order_by=path&sort=asc&per_page&page`.
  - GitHub: `GET /search/repositories?q=<q> in:name` scoped to the token's affiliations is not reliably filterable, so GitHub uses `GET /user/repos` as today and filters full names by case-insensitive substring across pages until `pageSize` results are collected or pages are exhausted, with the same `hasNext` semantics. The design phase may cap the page walk (for example ten pages).
- `search` is trimmed; values longer than 200 characters return `400 INVALID_SEARCH`.
- Response shape is unchanged: `repositories` resources with `meta.page`.
- `GitProvider.ListRepositories` gains a `search string` parameter. The fake provider filters by substring.

### 5.2 Branch listing (new)

`GET /api/providers/{provider}/repositories/{repo}/branches?search=<q>&page=&pageSize=`

- Returns a paged list of `branches` resources:
  ```json
  { "data": [ { "type": "branches", "id": "<provider>:<repo>:<name>", "attributes": { "name": "main", "isDefault": true, "sha": "6140736…" } } ],
    "meta": { "page": { "number": 1, "size": 50, "hasNext": false } } }
  ```
- Ordering: the default branch first, then by name ascending.
- GitLab: `GET /projects/:id/repository/branches?search=<q>&per_page&page`. GitHub: `GET /repos/:owner/:repo/branches?per_page&page`, with `search` applied as a substring filter client-side in the backend (GitHub has no branch search).
- `search` follows the same trimming and length rule as 5.1.
- Errors: `404 REPOSITORY_NOT_FOUND` for an unknown repository, `502 PROVIDER_ERROR` for upstream failures, matching existing codes.
- `GitProvider` gains `ListBranches(ctx, repo Repository, search string, page Page) (Slice[Branch], error)`. The fake provider returns a fixed list including the default branch.

### 5.3 Unchanged endpoints used by this task

- `GET /api/providers`
- `GET /api/providers/{p}/repositories/{repo}`
- `GET /api/providers/{p}/repositories/{repo}/changes?state=merged&target=&search=&page=&pageSize=`
- `POST /api/reviews`, `GET /api/reviews`, `GET /api/reviews/{id}`, `DELETE /api/reviews/{id}`
- `GET /api/reviews/{id}/files`, `GET /api/reviews/{id}/files/{path}`

## 6. Data Model

### 6.1 Backend

- New value type `provider.Branch` with `Name`, `SHA`, `IsDefault`, built with the same immutable builder pattern as `Repository`.
- No change to `session.json` or the on-disk store.

### 6.2 Frontend types

- `Branch` resource: `{ name: string; isDefault: boolean; sha: string }`.
- `RepositoryListParams` gains `search?: string`.
- Client-side derived, not persisted: `ticketKey(change): string | null`, `isDependencyBot(change): boolean`, `applyOrder(changes): Change[]`.

### 6.3 Browser storage (localStorage)

| Key | Shape | Written when | Cleared when |
|---|---|---|---|
| `converge.recentRepositories` | `Array<{ provider, repository, defaultBranch, openedAt }>` (max 20) | create page loads; review built | never (LRU eviction) |
| `converge.changeFilters` | `{ hideBots: boolean; groupByTicket: boolean }` | toggles change | never |
| `converge.viewed.<reviewId>` | `string[]` of file paths | viewed toggled | review finished or discarded; also pruned on root load for ids not in `GET /api/reviews` |

All reads tolerate missing or malformed values by falling back to defaults.

## 7. Service Impact

### `apps/backend`
- `internal/provider`: add `search` to `ListRepositories`; add `Branch` model and `ListBranches` to the interface; update fake, GitHub, and GitLab implementations with unit tests against recorded fixtures.
- `internal/api`: accept and validate `search` on repositories; add `GET .../branches` handler and route; extend `api_test.go`.
- `internal/review`, `session`, `mirror`, `workspace`, `diff`, `gitx`: no change.

### `apps/frontend`
- New shadcn components added via the CLI: `sheet`, `breadcrumb`, `popover`, `command`, `progress`, `switch`, `separator`, `tooltip`, `alert-dialog` (for discard confirmation), `scroll-area`.
- `components/layout/AppShell.tsx`: brand block, breadcrumb, shared container.
- `pages/SelectRepositoryPage.tsx`: becomes the reviews table plus drawer trigger. `RepositoryList` and `ManualRepositoryForm` are replaced by the drawer's search list; `ProviderPicker` is folded into the drawer.
- `components/features/reviews/*`: table rows with progress and status dot.
- New `components/features/newReview/NewReviewSheet.tsx` and children.
- `pages/SelectChangesPage.tsx`, `components/features/changes/*`: base select, filter row, grouping, selection bar.
- `pages/ReviewPage.tsx`, `components/features/review/*`: status line, tree, sticky header, folds, footer, viewed state, shortcuts.
- `lib/hooks/api/`: `useRepositorySearch`, `useBranches`; `lib/storage/`: recents, filters, viewed.
- `services/api/`: `branchesService`; `repositoriesService.list` gains `search`.
- Tests with Vitest for storage helpers, derivations (ticket key, bot rule, apply order, tree building, fold computation), and each page's main interactions.

## 8. Non-Functional Requirements

- Repository search requests are debounced (250 ms) and cancelled on change; at most one in flight per drawer.
- The review page must render a 200-file tree and a 3,000-line diff without visible jank; tree building and fold computation are memoised per file list and per file diff.
- Keyboard shortcuts are single-key and must not conflict with browser defaults; they are disabled while any input has focus or a dialog is open.
- Search values are validated server-side (length, trimming) before reaching provider APIs; no client-supplied string reaches git.
- Backend logging: the new branches handler logs at debug with provider, repository, and page, matching existing handlers.
- No new persistence on the server; sweeping and expiry behaviour are untouched.
- `make lint`, `make test`, `make test-integration`, `make build`, and `make docker-build` remain green.

## 9. Open Questions

- GitHub repository search: walk `GET /user/repos` pages with a cap, or use the search API and accept that it may miss private repositories on some token types? The design should confirm against the GitHub docs and pick one.
- Exact container `max-width`. The mockup uses 1280 px; the design may adjust after checking a 1440 px display.
- Whether the "Inspect" action for conflicted reviews needs anything beyond navigating to the existing error panel.

## 10. Acceptance Criteria

- [ ] All three routes share one centred container and the brand/breadcrumb header; breadcrumbs match FR-2.
- [ ] Root page lists active sessions as rows with status dot, progress, time left, and actions; `FINISHED`/`EXPIRED` are absent.
- [ ] "Start a new review" row and the `n` key open the drawer; Escape, Cancel, and scrim close it.
- [ ] Drawer shows recent repositories with no query, server search results with a query, and resolves a pasted `owner/name` on Enter.
- [ ] `GET /api/providers/{p}/repositories?search=` filters on GitLab and GitHub and is covered by unit and API tests, including the fake provider.
- [ ] `GET /api/providers/{p}/repositories/{r}/branches` exists, orders the default branch first, pages, and is covered by unit and API tests.
- [ ] Base select lists branches, preselects the default, and refetches changes on change.
- [ ] Hide-bots switch removes `renovate/` and `dependabot/` source-branch changes and shows a hidden-count row with `Show`.
- [ ] Group-by-ticket renders group headers with working `Select all N`; keys are extracted by the fixed regex; "No ticket" is last.
- [ ] Author chips filter the loaded pages and are multi-select.
- [ ] Selection bar shows count, numbered chips in apply order, Clear, and Build; `POST /api/reviews` sends changes in apply order.
- [ ] Review page status line, tree, sticky file header, folds, word-level highlighting, footer next-file, and `j`/`k`/`v` shortcuts behave as specified.
- [ ] Viewed state persists across reloads for the same review and is cleared on finish or discard.
- [ ] Recent repositories and filter toggles persist in localStorage with the documented keys and tolerate malformed values.
- [ ] Vitest covers storage helpers, derivations, and each page's main interactions; Go tests cover the new provider methods and handlers.
- [ ] `make lint`, `make test`, `make test-integration`, `make build`, and `make docker-build` pass.
