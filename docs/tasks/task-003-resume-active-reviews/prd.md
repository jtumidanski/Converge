# Resume Active Reviews — Product Requirements Document

Version: v1
Status: Draft
Created: 2026-09-10
---

## 1. Overview

A Converge review session is server-side state: a resolved change set, a git worktree
under `WORKSPACE_ROOT`, and a computed diff, all reachable at `/reviews/{id}` until the
session is finished or swept at `expiresAt`. Today the only way to reach that URL is to
have built the review in this browser tab and never navigated away. Reload the page,
close the tab, follow a link, or restart the browser, and the session becomes
unreachable: the reviewer has no listing of what exists, no way back in, and no way to
release the disk it holds. The work is still on the server, fully intact — the UI simply
does not admit it exists.

This task surfaces those sessions. The root page (`SelectRepositoryPage`, route `/`)
gains a **Resume a review** section listing every active session returned by the
already-existing `GET /api/reviews`, each row showing what the session is, how far along
it is, how much longer it will live, and offering two actions: open it, or discard it.
The reviewer's first screen becomes an honest picture of server state instead of an
implicit assumption that they are starting fresh.

The backend requires no changes. `GET /api/reviews`
(`apps/backend/internal/api/reviews.go:104`) already delegates to `session.Store.List()`
(`apps/backend/internal/session/store.go:201`), which returns every session whose status
is `CREATING`, `READY`, `CONFLICTED`, or `FAILED`, sorted newest-first, and serialises
each through the same `reviewResource` projection the detail endpoint uses — so every
attribute this feature displays is already on the wire. The frontend already has
`reviewsService.list()` (`apps/frontend/src/services/api/reviews.ts:20`) and a
`useReviews()` React Query hook (`apps/frontend/src/lib/hooks/api/useReviews.ts:29`),
both currently unreferenced by any component. This task is the consumer they were
written for.

## 2. Goals

Primary goals:

- Make every active review session discoverable from the root page without prior
  knowledge of its id.
- Let a reviewer return to any active session — including one still building — in one
  click.
- Let a reviewer discard a session (releasing its worktree) from the same place they
  discover it, behind an explicit confirmation.
- Show enough per-session detail to tell two sessions on the same repository apart, and
  to know which one is about to expire.
- Reflect build progress live: a `CREATING` session's stage updates without a manual
  reload, and polling stops on its own once nothing is building.
- Ship as a frontend-only change: no modification to the `GET /api/reviews` contract, the
  session model, or the store.

Non-goals:

- Showing `FINISHED` or `EXPIRED` sessions. `Store.List()` filters them out by design
  (`IsActive()`, `apps/backend/internal/session/model.go:135`) and their workspaces are
  already deleted; a review history feature is out of scope.
- Any backend change: no `?status=` filter, no pagination, no new endpoint, no new
  attribute on the `reviews` resource.
- Search, filtering, sorting, or pagination of the session list. The server's
  newest-first order is the display order.
- Multi-user or per-user session scoping. Converge has no per-user identity; every
  session is visible to every client, as it is today.
- Bulk actions ("discard all expired").
- Any change to `ReviewPage`, the reconstruction pipeline, or the `/select` flow.
- Renaming, annotating, or pinning sessions.

## 3. User Stories

- As a reviewer, I want to see the reviews I have in progress the moment I open Converge,
  so that I do not rebuild a review that already exists.
- As a reviewer who reloaded the page or restarted my browser, I want to click back into
  the review I was reading, so that a lost tab does not cost me a reconstruction.
- As a reviewer whose review is still building, I want to leave the page and come back to
  a live progress indicator, so that I am not forced to sit and watch it.
- As a reviewer, I want to see which PRs/MRs and which repository a listed session covers,
  so that I can tell two sessions on the same repository apart before opening either.
- As a reviewer, I want to see how long a session has left before it expires, and be
  warned when that is soon, so that I can finish or re-run it deliberately rather than
  losing it mid-read.
- As a reviewer, I want a session that failed or conflicted to appear in the list with its
  error, so that I learn it exists, can open it for detail, and can clear it.
- As a reviewer, I want to discard a session from the list behind a confirmation, so that
  I can release server disk without navigating into a review I do not want to read, and
  without destroying one by a single misclick.
- As a reviewer with no sessions in progress, I want the section to tell me so plainly,
  so that I know the feature exists and that the absence of rows is a real answer rather
  than a failure to load.

## 4. Functional Requirements

### 4.1 Placement and page composition

- **FR-1.1** `SelectRepositoryPage` (route `/`) renders a **Resume a review** section
  between the `PageHeader` and the `ProviderPicker`, above all repository-selection UI.
- **FR-1.2** The section is always rendered, including when there are no active sessions
  (see FR-5.1). It is not collapsible in v1.
- **FR-1.3** No new route is added. Opening a session navigates to the existing
  `/reviews/{id}` route.
- **FR-1.4** The section is implemented as a new feature component under
  `src/components/features/reviews/`, kept presentational; data fetching and mutation
  live in the page and in `useReviews.ts`, matching how `RepositoryList` and
  `SelectRepositoryPage` are split today.
- **FR-1.5** The section renders a heading and, when one or more sessions are listed, a
  count of them.

### 4.2 Which sessions are listed

- **FR-2.1** The list renders exactly the resources returned by `GET /api/reviews`, in
  the order returned. No client-side filtering by status, and no client-side re-sorting.
- **FR-2.2** All four active statuses — `CREATING`, `READY`, `CONFLICTED`, `FAILED` — are
  displayed in one list. They are visually distinguished by status badge (FR-3.1), not
  separated into groups.
- **FR-2.3** `FINISHED` and `EXPIRED` are not displayed. The endpoint does not return
  them; the client adds no special handling and must not synthesise rows for them.
- **FR-2.4** A status value the client does not recognise must render the row with the
  raw status string in a neutral badge rather than crashing or omitting the row.

### 4.3 Row content

Each row displays, sourced entirely from `ReviewAttributes`
(`src/types/models/review.ts`):

- **FR-3.1** A status badge showing the status in product vocabulary, with a variant that
  distinguishes settled-and-readable (`READY`), in-progress (`CREATING`), and
  errored (`CONFLICTED`, `FAILED`). Error statuses use the `destructive` badge variant.
- **FR-3.2** The repository (`repository`) and the provider (`provider`).
- **FR-3.3** The requested change numbers (`changes`), rendered as `#12, #14, #15`. When
  `included` is populated, the row may use it for change numbers instead, but `changes`
  is the authoritative fallback because it is present from the moment the session is
  created, before resolution completes.
- **FR-3.4** When `totals` is non-null: file count, additions, and deletions, using the
  same `+N` / `−N` presentation as `ReviewHeader`. When `totals` is null the row omits
  these rather than rendering zeros.
- **FR-3.5** When status is `CREATING`: the current stage, translated through the same
  product-vocabulary mapping `ReviewStatus` uses (`resolving` → "Resolving PRs/MRs", and
  so on). The stage-label logic must be shared with `ReviewStatus`, not duplicated —
  extract it to a shared module rather than copying the switch.
- **FR-3.6** When `error` is non-null: a single-line summary naming the error code and,
  when present, the change number it concerns. Full error detail stays on `ReviewPage`;
  the row must not render conflicting-file lists or diagnostics.
- **FR-3.7** A relative creation time derived from `createdAt` (e.g. "started 12m ago").
- **FR-3.8** A relative expiry derived from `expiresAt` (e.g. "expires in 5h"). When the
  session expires in under one hour, the expiry text is rendered in a warning style and
  the row is marked so assistive technology conveys the same urgency.
- **FR-3.9** Relative times are computed with `Intl.RelativeTimeFormat` in a shared,
  unit-tested helper in `src/lib/`. No date library is added to `package.json`.
- **FR-3.10** A row whose `expiresAt` is already in the past renders "expired" in the
  warning style rather than a negative duration. (The server sweeps such sessions, but the
  client may hold a stale list between refetches.)

### 4.4 Actions

- **FR-4.1** Each row exposes a primary **Resume** action that navigates to
  `/reviews/{id}`. It is enabled for all four active statuses: `ReviewPage` already
  renders a progress view for `CREATING` and an error panel for `CONFLICTED`/`FAILED`, so
  every listed session has a meaningful destination, and resuming a build in progress is
  a primary use case (see §9, Q1).
- **FR-4.2** Each row exposes a **Discard** action that calls the existing
  `useFinishReview()` mutation (`DELETE /api/reviews/{id}`).
- **FR-4.3** Discard requires an explicit confirmation step before the request is sent.
  The confirmation names what is being discarded (repository and change numbers) and
  states that it cannot be undone. Cancelling makes no request.
- **FR-4.4** While a discard is in flight, that row's actions are disabled and the discard
  control shows a pending state. Other rows remain interactive.
- **FR-4.5** On discard success the list refetches (the mutation already invalidates
  `reviewKeys.lists()`) and the row disappears. On failure the row remains and an error
  toast is shown via `sonner`, using `messageFor()` for the message, matching
  `ReviewPage.closeReview()`.
- **FR-4.6** Discard from this list and "Finish Review" on `ReviewPage` are the same
  backend operation; no new endpoint or semantics are introduced.
- **FR-4.7** Both actions are real, focusable controls (`Button`), each with an accessible
  name that identifies its row — the list must not rely on whole-row click as the only
  affordance.

### 4.5 Loading, refresh, and error states

- **FR-5.1** When the query has resolved and returned zero sessions, the section renders
  the shared `EmptyState` with a title stating no reviews are in progress and a
  description pointing at the repository selection below.
- **FR-5.2** While the query is loading for the first time, the section renders
  `Skeleton` placeholders, not the empty state. An empty state must never be shown for a
  request that has not resolved.
- **FR-5.3** On query error the section renders the shared `ErrorBanner` with a retry
  control wired to `refetch()`, and the rest of the root page (provider picker,
  repository list) continues to function normally. A failure to list sessions must never
  block starting a new review.
- **FR-5.4** `useReviews()` polls every 2000 ms while any listed session is non-terminal
  (`!isTerminal(status)`, i.e. `CREATING`), and stops polling — `refetchInterval` returns
  `false` — once no listed session is building. This mirrors the existing `useReview()`
  hook.
- **FR-5.5** Background refetches must not clear the rendered rows or flash the loading
  skeleton; only the initial load shows skeletons.
- **FR-5.6** Creating a review already invalidates `reviewKeys.lists()`
  (`useCreateReview`), so returning to `/` after a build shows the new session without
  extra wiring. No additional invalidation is required.

### 4.6 Vocabulary and styling

- **FR-6.1** All new user-facing copy is added to `src/lib/strings.ts` and follows the
  established product vocabulary: "PRs/MRs", never "commits" or "SHAs"; git terminology
  remains confined to Diagnostics on `ReviewPage`.
- **FR-6.2** Styling uses existing shadcn/ui primitives (`Badge`, `Button`, `Skeleton`)
  and Tailwind tokens already in use. No new shadcn component is added unless the
  confirmation step requires one (see §9, Q2).
- **FR-6.3** The section is responsive at the same breakpoints as the rest of the root
  page and does not introduce horizontal scrolling at narrow widths; long repository
  names truncate rather than overflow.

## 5. API Surface

No new or modified endpoints. This feature consumes one existing endpoint unchanged:

**`GET /api/reviews`** → `200 OK`, JSON:API collection of `reviews` resources.

```jsonc
{
  "data": [
    {
      "type": "reviews",
      "id": "c2f1a7e0-...",
      "attributes": {
        "status": "READY",              // CREATING | READY | CONFLICTED | FAILED
        "stage": null,                  // e.g. "resolving", "applying:14" while CREATING
        "provider": "gitlab-work",
        "repository": "atlas/server",
        "baseBranch": "main",
        "baseSha": "a1b2c3d...",
        "headSha": "9f8e7d6...",
        "baseDescription": "…",
        "changes": [12, 14, 15],        // requested, present from creation
        "included": [ /* resolved changes; empty until resolution completes */ ],
        "totals": { "files": 5, "additions": 84, "deletions": 12 }, // null until diffed
        "error": null,                  // ReviewErrorPayload when CONFLICTED/FAILED
        "createdAt": "2026-09-10T12:00:00Z",
        "updatedAt": "2026-09-10T12:00:40Z",
        "expiresAt": "2026-09-11T12:00:00Z"
      },
      "relationships": { "files": { "links": { "related": "/api/reviews/…/files" } } }
    }
  ]
}
```

Guarantees relied upon (verified in source, not assumed):

- Only active sessions are returned; `FINISHED`/`EXPIRED` are filtered by
  `Session.IsActive()`.
- Ordering is `createdAt` descending (`Store.List()`).
- The collection is unpaginated and carries no `meta.page`; `unwrapList` handles this.
- An empty result is `{"data": []}`, never `null` data — `listReviews` allocates a
  zero-length slice.

**`DELETE /api/reviews/{id}`** → `204 No Content`, idempotent, already consumed by
`useFinishReview()`. Unchanged.

## 6. Data Model

No backend data-model change. No new persisted state, no migration, no change to
`session.json`.

Frontend types are already sufficient: `Review`, `ReviewAttributes`, `ReviewStatus`,
`Totals`, `ReviewErrorPayload`, and `IncludedChange` in `src/types/models/review.ts` need
no new fields. New view-model types, if any, are derived locally in the feature component
and are not persisted.

No client-side persistence (no `localStorage` of a "last review id"): the server list is
the single source of truth, which is precisely why a browser-local record is unnecessary.

## 7. Service Impact

**`apps/backend`** — no changes. No Go file is added or modified by this task. If a
change to the backend appears necessary during design or execution, that is a signal to
stop and revisit this PRD rather than to quietly widen scope.

**`apps/frontend`** — all changes:

| Path | Change |
|---|---|
| `src/pages/SelectRepositoryPage.tsx` | Render the new section; own the list query and discard mutation wiring |
| `src/components/features/reviews/` | New: list component, row component, confirmation affordance |
| `src/lib/hooks/api/useReviews.ts` | Add conditional `refetchInterval` to `useReviews()` (FR-5.4) |
| `src/components/features/review/ReviewStatus.tsx` | Extract `stageLabel` to a shared module for reuse (FR-3.5) |
| `src/lib/` | New relative-time helper (FR-3.9) |
| `src/lib/strings.ts` | New copy (FR-6.1) |
| `src/pages/__tests__/SelectRepositoryPage.test.tsx` | Extend for the new section |
| new `__tests__` files | Cover the list component and the time helper |

## 8. Non-Functional Requirements

- **NFR-1 (Performance)** The list adds exactly one `GET /api/reviews` per root-page
  mount. Polling occurs only while a session is `CREATING`, at 2 s, and stops
  automatically — a root page left open with only settled sessions issues no periodic
  requests.
- **NFR-2 (Bundle)** No new runtime dependency. Relative-time formatting uses the
  platform `Intl` API.
- **NFR-3 (Security)** The list renders only fields already exposed by the existing
  endpoint. Provider tokens are never in this payload and must not become so. Repository
  names, change titles, error messages, and stage strings are rendered as text, never as
  HTML, and any `webUrl` link keeps `target="_blank" rel="noreferrer"` as `ReviewHeader`
  does.
- **NFR-4 (Accessibility)** The section has an accessible heading. Status is conveyed by
  text, not colour alone. The near-expiry warning is announced, not purely visual. Both
  row actions are keyboard reachable with row-identifying accessible names. The
  live-updating stage region uses `aria-live="polite"`, consistent with `ReviewStatus`.
- **NFR-5 (Resilience)** A malformed or partially-populated resource (null `totals`,
  empty `included`, unknown status, absent `stage`) must render a degraded row, never
  throw. `ReviewPage`'s exhaustive-switch pattern is the model for handling status
  values.
- **NFR-6 (Observability)** No new logging or metrics. Discard failures surface to the
  user as a toast.

## 9. Open Questions

1. **Resume on a `CREATING` session.** FR-4.1 enables Resume for all four statuses,
   including `CREATING`, on the grounds that `ReviewPage` already renders a live progress
   view and returning to a build in progress is a primary motivation for this feature.
   This is a deliberate resolution of an ambiguity in the approved mockup, which showed a
   `CREATING` row with only a Discard action. Confirm during design.
2. **Confirmation mechanism.** Inline two-step confirmation within the row (no new
   dependency, no focus-trap concerns) versus a shadcn `AlertDialog` (more conventional,
   adds a component). Inline is the lighter default; resolve in design.
3. **Row density at scale.** With no pagination, a long-lived server could accumulate
   enough sessions to dominate the root page. Should the section cap the visible rows
   (e.g. show the newest N with a "show all" toggle) purely client-side? Deferred; not
   required for correctness at expected session counts, and explicitly not to be solved
   with a backend change.
4. **`included` versus `changes` for the row's change numbers.** FR-3.3 permits either
   with `changes` as the fallback; design should pick one rule and state it, so a
   mid-resolution session does not flicker between two renderings of the same set.
5. **Discard of a `CREATING` session.** `DELETE` on a session whose build goroutine is
   still running is already the existing Finish path and is expected to be safe, but the
   interaction has not been exercised from a list context. Design should confirm the
   backend behaviour by reading `review.Service.Finish` rather than assuming.

## 10. Acceptance Criteria

Functional:

- [ ] With at least one active session, loading `/` shows a **Resume a review** section
      above the provider picker listing every session `GET /api/reviews` returns, in the
      returned order.
- [ ] Each row shows status, provider, repository, change numbers, and relative created
      and expiry times; `totals` appear when present and are omitted (not zeroed) when
      null.
- [ ] A `CREATING` row shows a product-vocabulary stage label sourced from the same
      mapping `ReviewStatus` uses.
- [ ] A `CONFLICTED` or `FAILED` row shows a `destructive` status badge and a one-line
      error summary.
- [ ] A session expiring in under an hour shows its expiry in the warning style; an
      already-past `expiresAt` renders "expired", not a negative duration.
- [ ] Clicking **Resume** navigates to `/reviews/{id}` for a session in any of the four
      active statuses.
- [ ] Clicking **Discard** shows a confirmation naming the session; cancelling issues no
      request; confirming issues `DELETE /api/reviews/{id}`, removes the row on success,
      and on failure leaves the row and shows an error toast.
- [ ] While a discard is pending, only that row's controls are disabled.
- [ ] With zero active sessions, the section renders the empty state — and does so only
      after the query resolves, never during the initial load.
- [ ] During the initial load the section renders skeletons; a background refetch does
      not replace rendered rows with skeletons.
- [ ] When `GET /api/reviews` fails, the section shows a retryable error banner and the
      provider picker and repository list still work.
- [ ] The list refetches every 2 s while any row is `CREATING`, and issues no periodic
      requests once none is.
- [ ] Building a new review and navigating back to `/` shows the new session without a
      manual reload.

Code and process:

- [ ] `git diff main --stat` shows no changes under `apps/backend/`.
- [ ] No new entry in `apps/frontend/package.json` `dependencies`.
- [ ] `stageLabel` exists in exactly one place and is imported by both `ReviewStatus` and
      the new row component.
- [ ] All new user-facing strings are in `src/lib/strings.ts`.
- [ ] Vitest coverage for: the list rendering each status, the empty state, the loading
      state, the error-and-retry path, the resume navigation, the discard
      confirm/cancel/success/failure paths, and the relative-time helper including the
      near-expiry and already-expired boundaries. MSW handlers use the existing
      `listDoc`/`errorDoc` helpers in `src/test/server.ts`.

Verification (all clean, per CLAUDE.md):

- [ ] `apps/frontend`: `npm run lint`, `npm run format:check`, `npm test`, `npm run build`
- [ ] `apps/backend`: `go test -race -count=1 ./...`, `go vet ./...`,
      `go tool golangci-lint run`, `CGO_ENABLED=0 go build ./...` (expected unaffected;
      run to prove it)
- [ ] Repository root: `make lint`, `make test`, `make test-integration`, `make build`,
      `make docker-build`
- [ ] `superpowers:requesting-code-review` run — including
      `frontend-guidelines-reviewer` — with findings recorded in `audit.md` and addressed
      before a PR is opened.
