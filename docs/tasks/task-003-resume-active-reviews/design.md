# Resume Active Reviews — Design

Version: v1
Status: Approved for planning
Created: 2026-09-10
Inputs: `prd.md` (v1, approved), `ux-flow.md` (approved)

---

## 1. Scope of this document

The PRD fixes *what* is built and the UX flow fixes *how it looks*. This document
fixes *how it is assembled*: the module boundaries, where state lives, which
existing code is extracted rather than duplicated, and the five open questions the
PRD deferred (§9 of the PRD, resolved here in §7).

The feature is frontend-only. Every backend behaviour it depends on was read in
source during this design pass; §6 records what was verified and where.

---

## 2. Architecture

### 2.1 Module map

Seven modules, five of them new. The split follows the existing
`SelectRepositoryPage` / `RepositoryList` division: the page owns data and
mutations, the feature component is presentational and takes everything as props.

| Module | Status | Responsibility |
|---|---|---|
| `src/lib/relativeTime.ts` | new | Pure formatting of ISO timestamps into "started 12m ago" / "expires in 5h" / "expired", plus the near-expiry urgency flag. No React, no I/O. |
| `src/lib/stageLabel.ts` | new (extracted) | The single `stageLabel(stage)` mapping, moved out of `ReviewStatus.tsx`. |
| `src/lib/strings.ts` | modified | New user-facing copy. |
| `src/lib/hooks/api/useReviews.ts` | modified | `useReviews()` gains a conditional `refetchInterval`. |
| `src/components/features/reviews/ResumeReviewList.tsx` | new | The section: heading, count, and one of {skeletons, error banner, empty state, rows}. Presentational. |
| `src/components/features/reviews/ResumeReviewRow.tsx` | new | One row: badge, identity line, detail line, time line, and the two-step action area. Owns only its own confirm-open boolean. |
| `src/pages/SelectRepositoryPage.tsx` | modified | Owns `useReviews()` and `useFinishReview()`, the toast, and navigation. |

`ReviewStatus.tsx` is modified only to import `stageLabel` instead of defining it —
no behaviour change, and its existing test continues to cover the mapping through
that component.

### 2.2 Data flow

```
SelectRepositoryPage
  ├─ useReviews()            → { data, isLoading, isError, error, refetch }
  ├─ useFinishReview()       → { mutateAsync, isPending, variables }
  │
  └─ <ResumeReviewList
        reviews    = data ?? []
        loading    = isLoading            // initial load only, never isFetching
        error      = isError ? error : undefined
        onRetry    = () => void refetch()
        pendingId  = isPending ? variables : undefined
        onResume   = (id) => navigate(`/reviews/${id}`)
        onDiscard  = (id) => discard(id)  // page-owned async handler
      />
         └─ <ResumeReviewRow ... />       // one per review, keyed by review.id
```

The page's `discard` handler mirrors `ReviewPage.closeReview()` exactly:

```ts
async function discardReview(id: string) {
  try {
    await finishOrDiscard.mutateAsync(id);
  } catch (error: unknown) {
    toast.error(messageFor(error, strings.reviewDiscardFailed));
  }
}
```

No navigation on success — the reviewer stays on `/`; the mutation's existing
`onSettled` invalidation of `reviewKeys.lists()` removes the row (FR-4.5). This is
the one deliberate divergence from `closeReview()`, which navigates because it runs
from inside the review being closed.

### 2.3 Why the page owns the mutation

`pendingId` is derived from the mutation itself (`isPending ? variables : undefined`)
rather than tracked in component state. TanStack Query already stores the variables
of the in-flight mutation, so a second source of truth would only be a way to get
out of sync. Because there is exactly one `useFinishReview()` instance for the whole
list, at most one discard can be in flight; `pendingId` is therefore sufficient to
satisfy FR-4.4's "only that row's controls are disabled" without a per-row map.

Alternative considered: one `useFinishReview()` per row, letting each row own its own
pending state. Rejected — it multiplies hook instances by row count for no gain, and
it makes concurrent discards possible, which the confirmation step makes pointless
and which complicates the empty-state transition.

### 2.4 Polling

`useReviews()` gains a `refetchInterval` shaped exactly like `useReview()`'s:

```ts
export function useReviews() {
  return useQuery({
    queryKey: reviewKeys.lists(),
    queryFn: () => reviewsService.list(),
    refetchInterval: (query) => {
      const data = query.state.data as Review[] | undefined;
      return data?.some((r) => !isTerminal(r.attributes.status)) ? 2000 : false;
    },
  });
}
```

`isTerminal` is reused unchanged (`status !== "CREATING"`), so "still building" has
one definition across the detail and list hooks. When the last `CREATING` session
settles, the next successful refetch returns `false` and polling stops on its own —
no effect, no timer to clean up (FR-5.4, NFR-1).

`staleTime` is deliberately left at the client default rather than set to `0` as
`useReview()` does: the list is not driving a build view, and a mount-time refetch is
already guaranteed by React Query's default `refetchOnMount` behaviour for stale
data. Setting `refetchInterval` does not require `staleTime: 0`.

### 2.5 Loading, error, and empty precedence

`ResumeReviewList` renders exactly one of four states, in this order:

1. `error` present → `ErrorBanner` with `onRetry`. The banner is scoped to the
   section; the provider picker and repository list below are untouched (FR-5.3).
2. `loading` → three `Skeleton` rows. `loading` is wired to `isLoading`, which
   TanStack Query reports only for a first load with no data — a background refetch
   has `isFetching: true` but `isLoading: false`, so rendered rows are never
   replaced by skeletons (FR-5.2, FR-5.5).
3. `reviews.length === 0` → shared `EmptyState`. Unreachable before the query
   resolves because case 2 catches that.
4. otherwise → the rows.

The heading renders above all four; the count renders only in case 4 (FR-1.5).

---

## 3. Row rendering

### 3.1 Structure

Rows are a `<ul>`/`<li>` list, not a `<Table>`. `RepositoryList` uses a table
because its data is genuinely columnar; a resume row is a three-line block with a
variable detail line, and forcing it into a table would either need column spans or
would break the responsive requirement (FR-6.3). A list of bordered cards truncates
long repository names cleanly at narrow widths with `truncate` and no horizontal
scroll.

Line 1: status badge · provider · repository
Line 2 (detail, status-dependent): change numbers, then totals or stage or error
Line 3: `started {rel} · {expiry}`
Action area: `[Resume] [Discard]`, or the inline confirmation when armed.

### 3.2 Status → presentation

A single `statusPresentation(status)` helper in `ResumeReviewRow.tsx` maps status to
`{ label, variant }`:

| Status | Label | Badge variant |
|---|---|---|
| `READY` | "Ready" | `default` |
| `CREATING` | "Building" | `secondary` |
| `CONFLICTED` | "Conflict" (via `strings.conflict`) | `destructive` |
| `FAILED` | "Failed" | `destructive` |
| anything else | the raw string, rendered as text | `outline` |

The unknown-status branch is a `default:` case over `string`, not an exhaustive
switch with `assertUnreachable`. This is a deliberate departure from
`ReviewPage`'s pattern and the reason is specific: `ReviewPage` switches over the
full `ReviewStatus` union to choose a *layout*, and a new status arriving without a
layout is a real bug worth failing the build over. This list must degrade rather
than throw on a status the server introduces (`FINISHED`/`EXPIRED` are already in
the union and must not crash a stale list either) — NFR-5 and FR-2.4 are explicit
about this. The row therefore treats status as data.

Note the consequence: `FINISHED` and `EXPIRED` are in the `ReviewStatus` union and
would fall into the neutral branch if a stale cached list ever held one. That is the
correct degraded behaviour; the client still synthesises nothing and filters nothing
(FR-2.3).

### 3.3 Detail line by status

- `READY`: change numbers, then `{n} files · +{a} −{d}` when `totals` is non-null,
  using the same `+`/`−` glyphs as `ReviewHeader` (U+2212, not a hyphen). When
  `totals` is null the totals fragment is omitted entirely — no zeros (FR-3.4).
- `CREATING`: change numbers, then `stageLabel(stage)`. The stage text sits in a
  `<span aria-live="polite">` so a 2 s poll update is announced once, matching
  `ReviewStatus` (NFR-4).
- `CONFLICTED` / `FAILED`: change numbers, then `errorSummary(error)` — see §3.5.
- unknown: change numbers only.

### 3.4 Change numbers — resolved rule

**Always render from `attributes.changes`. Never from `included`.** (Resolves PRD
§9 Q4.)

`changes` is written at session creation and never mutated afterwards; `included` is
empty until resolution completes and then populated. Reading `included` when
non-empty and falling back to `changes` otherwise — which FR-3.3 permits — means a
`CREATING` row's change list is re-derived from a different source mid-poll. Even
when the two sets agree, this is a needless second code path whose only observable
effect is the risk of a flicker if they ever disagree (a change requested but not
resolvable, for instance). One source, stable from the first frame:

```ts
const changeLabel = changes.map((n) => `#${n}`).join(", ");
```

`included` is not read by this feature at all. Its titles and `webUrl` links belong
to `ReviewHeader` on the detail page, and adding external links to a list row would
also pull in the `target="_blank" rel="noreferrer"` obligation (NFR-3) for no stated
requirement.

### 3.5 Error summary

`errorSummary(error)` returns one line and nothing more:

```
CONFLICT on #14        // error.change present
GIT_FAILURE            // error.change absent
```

The code is rendered verbatim as text — it is server-controlled and short. The
`message`, `conflictingFiles`, `appliedChanges`, and `diagnostics` fields are *not*
rendered here (FR-3.6); they stay in `ReviewErrorPanel` on `ReviewPage`, which is
one click away via Resume. Rendering `message` in the row was considered and
rejected: it is a full sentence sized for a panel, and it would either wrap the row
to an unpredictable height or need truncation that hides the part that matters.

### 3.6 Relative time

`src/lib/relativeTime.ts` exports two functions over a shared internal core:

```ts
export function relativeTime(iso: string, now?: Date): string;
export function expiryLabel(iso: string, now?: Date): { text: string; nearExpiry: boolean };
```

- `relativeTime` picks the largest unit whose magnitude is ≥ 1 from
  {day, hour, minute, second} and formats it with a module-level
  `new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })`. Constructing the
  formatter once at module scope matters: `Intl` constructors are expensive and a
  polling list re-renders every 2 s.
- `expiryLabel` returns `{ text: strings.expired, nearExpiry: true }` when the
  timestamp is at or before `now` (FR-3.10 — never a negative duration), otherwise
  `{ text: "expires in …", nearExpiry: msRemaining < 3_600_000 }`.
- An unparseable timestamp (`Number.isNaN` on the parsed value) returns an empty
  string / `{ text: "", nearExpiry: false }` rather than "Invalid Date" (NFR-5).

`now` is an injected optional parameter defaulting to `new Date()`. This is what
makes the boundary cases (59 minutes vs 61 minutes, already-past) testable without
fake timers.

The near-expiry treatment is a warning text class *plus* a `title`-free textual
signal: the string itself reads "expires in 47m", so urgency is conveyed by content
and colour together, never colour alone (NFR-4). The row carries the expiry text
inside the same `aria-live="polite"` region is *not* done — expiry drifts on every
poll and announcing it repeatedly would be noise. Instead the near-expiry row's
expiry element is marked with `role="status"` only when `nearExpiry` flips true, so
the transition into urgency is announced once.

### 3.7 Time freshness between polls

Relative times are computed during render from `Date.now()`. When nothing is
`CREATING` there is no poll, so a row left on screen keeps showing "started 12m ago"
until something re-renders it. This is accepted for v1: no ticking timer is added.
The alternative (a 30 s interval purely to refresh text) contradicts NFR-1's
"issues no periodic requests once none is building" in spirit — it would keep a
render loop alive on an idle page — and the staleness is cosmetic. Worth stating
explicitly so it is not later mistaken for a bug.

---

## 4. Discard interaction

### 4.1 Mechanism — resolved

**Inline two-step confirmation inside the row.** (Resolves PRD §9 Q2, and matches
the approved mockup in `ux-flow.md`.)

The row holds `const [confirming, setConfirming] = useState(false)`. When
`confirming` is false the action area is `[Resume] [Discard]`. When true it is
replaced by a confirmation line naming the session plus `[Cancel] [Discard]`:

```
Discard this review of atlas/server (#12, #14, #15)? This cannot be undone.
                                              [Cancel] [Discard]
```

The confirming variant's Discard button is `variant="destructive"` and receives
focus when the confirmation opens, so a keyboard user is not left hunting for it.
Cancel restores the default action area and issues no request (FR-4.3).

Rejected: shadcn `AlertDialog`. It is the more conventional pattern and the
underlying primitive is already available (`radix-ui` is a direct dependency, so it
would add a component file rather than a package). It was still rejected because a
modal for a single-row action costs a focus trap, an overlay, and a portal to
express what one line of row-local state expresses, and because the approved mockup
specifies inline. Nothing here forecloses the swap later — the confirmation lives
entirely inside `ResumeReviewRow` behind a boolean.

One consequence to handle: when a discard succeeds the row unmounts, taking
`confirming` with it. When a discard *fails*, the row survives and must not stay
armed — the handler resets `confirming` to false in a `finally`, so the reviewer
sees the toast against a row in its resting state rather than a stuck confirmation.

### 4.2 Pending and disabled

While `pendingId === review.id`: both Resume and Discard in that row are
`disabled`, and Discard shows the `Loader2` spinner exactly as `ReviewHeader`'s
finish button does. Every other row stays fully interactive because `pendingId`
matches only one id (FR-4.4).

### 4.3 Accessible names

Both buttons carry a row-identifying accessible name via `aria-label`, because the
visible text ("Resume", "Discard") repeats across every row and a screen-reader
button list would be unusable:

```
aria-label={`Resume review of ${repository} (${changeLabel})`}
aria-label={`Discard review of ${repository} (${changeLabel})`}
```

Both are real `<Button>` elements. There is no whole-row click handler — the row is
not a link and does not pretend to be one (FR-4.7).

### 4.4 Resume on every status — confirmed

**Resume is enabled for all four active statuses, including `CREATING`.**
(Resolves PRD §9 Q1 in favour of FR-4.1 over the mockup's narrower row.)

Verified against `ReviewPage.tsx`: its status switch renders `ReviewStatus` (the
live progress view, polling at 2 s) for `CREATING` and `ReviewErrorPanel` for
`CONFLICTED`/`FAILED`. Every listed session therefore has a real destination, and
"leave the page while it builds, come back to live progress" is a stated user story.
The mockup's `CREATING` row is treated as illustrative, per its own preamble.

### 4.5 Discarding a `CREATING` session — verified safe

(Resolves PRD §9 Q5 by reading source, not assumption.)

`DELETE /api/reviews/{id}` → `review.Service.Finish` → `session.Store.Finish`
(`apps/backend/internal/session/store.go:216`). `Store.Finish` writes the terminal
status into the index *before* running `Cleanup`, and the in-flight build cannot
resurrect the session: `Store.SaveActive` (`store.go:115`) refuses any write whose
stored session is no longer active, returning `ErrTerminal`. The build path handles
that refusal explicitly rather than treating it as a failure —
`apps/backend/internal/review/service.go:328`, `:370`, and `:522` all branch on
`errors.Is(err, session.ErrTerminal)`, and the comment at `service.go:319` states
that a refused write never fails the build.

Conclusion: discarding a `CREATING` session from the list is the same supported
operation as finishing one from `ReviewPage`, and the concurrent-build case is
already designed for. No frontend guard, no disabled Discard, no special-casing.

---

## 5. Copy

All new strings go in `src/lib/strings.ts` alongside the existing entries
(FR-6.1). Existing `strings.conflict` and `strings.discardReview` are reused rather
than duplicated.

| Key | Value |
|---|---|
| `resumeReview` | "Resume a review" |
| `resume` | "Resume" |
| `discard` | "Discard" |
| `cancel` | "Cancel" |
| `noReviewsInProgressTitle` | "No reviews in progress." |
| `noReviewsInProgressDescription` | "Start one by choosing a repository below." |
| `reviewsUnavailableTitle` | "Could not load reviews in progress" |
| `reviewDiscardFailed` | "The review could not be discarded." |
| `expired` | "expired" |
| `statusReady` / `statusBuilding` / `statusFailed` | "Ready" / "Building" / "Failed" |

Composed strings that interpolate data (the confirmation sentence, the aria-labels,
the "started … · expires in …" line) are built in the component from these parts.
The vocabulary rule holds throughout: "PRs/MRs", never commits or SHAs — no SHA,
base branch, or git term appears anywhere in this section.

---

## 6. Backend contract — verified, unchanged

No Go file is added or modified (FR-7). Each guarantee the frontend leans on was
read in source during this design:

| Guarantee | Evidence |
|---|---|
| Only active sessions are listed | `Store.List()` filters on `Session.IsActive()`, a whitelist of `CREATING`/`READY`/`CONFLICTED`/`FAILED` — `session/store.go:200`, `session/model.go:138` |
| Newest-first ordering | `sort.Slice` on `CreatedAt().After(...)` — `session/store.go:209` |
| Empty result is `{"data": []}`, never `null` | `listReviews` allocates `make([]jsonapi.Resource, 0, …)` — `api/reviews.go:104` |
| No pagination meta | `jsonapi.WriteList(w, 200, out, nil)` — `api/reviews.go:109` |
| `DELETE` is idempotent | `Store.Finish` returns nil for unknown or already-finished ids — `session/store.go:216` |
| Discard races the build safely | §4.5 above |

The client re-sorts nothing and filters nothing (FR-2.1). If the list ever needs a
`?status=` filter or a limit, that is a new PRD, not a widening of this one.

---

## 7. Open questions from the PRD — resolutions

| # | Question | Resolution | Where |
|---|---|---|---|
| Q1 | Resume on a `CREATING` session | Enabled, for all four active statuses | §4.4 |
| Q2 | Confirmation mechanism | Inline two-step in the row; no `AlertDialog` | §4.1 |
| Q3 | Row density at scale | No cap in v1 | below |
| Q4 | `included` vs `changes` | Always `changes`; `included` unread | §3.4 |
| Q5 | Discard of a `CREATING` session | Verified safe in backend source | §4.5 |

**Q3 (row density).** No client-side cap, no "show all" toggle. Sessions are
short-lived by construction — every one carries an `expiresAt` and the server sweeps
past it — so the list is self-limiting at the count a single reviewer creates within
one expiry window. A cap would add a second empty-ish state, a toggle, and a
question about what the count in the heading means, to solve a problem no observed
usage has produced. Revisit if a real deployment shows double-digit concurrent
sessions; the heading's count makes that condition visible when it happens.

---

## 8. Testing

Vitest with the existing MSW server and `listDoc`/`errorDoc` helpers from
`src/test/server.ts`. Four test files:

**`src/lib/__tests__/relativeTime.test.ts`** — pure, no MSW, `now` injected:
- past timestamps at second/minute/hour/day magnitudes
- `expiryLabel` at 59 min (nearExpiry true), 61 min (false), exactly 60 min
  (boundary stated explicitly: `< 3_600_000` means 60 min is *not* near-expiry)
- `expiresAt` in the past → "expired", `nearExpiry: true`, never a negative number
- unparseable input → empty string, no throw

**`src/components/features/reviews/__tests__/ResumeReviewList.test.tsx`** — the
component in isolation with props, no network:
- a row per status: `READY` with and without `totals`, `CREATING` with a stage,
  `CONFLICTED` with `error.change`, `FAILED` without, and an unknown status string
  cast in — each asserted to render and not throw
- totals omitted (not zeroed) when `totals` is null
- loading → skeletons and no empty state; empty → `EmptyState`; error → `ErrorBanner`
  and `onRetry` fires `refetch`
- confirm/cancel: Cancel calls no `onDiscard`; confirming calls it once with the id
- `pendingId` disables only the matching row's buttons

**`src/pages/__tests__/SelectRepositoryPage.test.tsx`** (extended) — through MSW:
- section renders above the provider picker with rows in server order
- Resume navigates to `/reviews/{id}`
- discard success removes the row after invalidation; discard failure
  (`errorDoc`) leaves the row and shows the toast
- `GET /api/reviews` failing still leaves the provider picker and repository list
  working — the explicit FR-5.3 assertion

**`src/lib/hooks/api/__tests__/useReviews.test.tsx`** (extended):
- `refetchInterval` resolves to `2000` when any item is `CREATING` and to `false`
  when none is. Asserted by invoking the option against a stubbed query state
  rather than by waiting on real timers — the existing file's style, and it keeps
  the suite off wall-clock waits.

`ReviewStatus.test.tsx` is left as-is; it exercises `stageLabel` through the
component and so proves the extraction did not change behaviour.

---

## 9. Risks

- **Extraction touches a tested component.** Moving `stageLabel` out of
  `ReviewStatus.tsx` is a pure move; the existing test is the guard. If it fails,
  the move was not pure.
- **Polling plus inline confirm.** A 2 s refetch re-renders rows while a
  confirmation is open. Because `confirming` is row-local state and rows are keyed
  by `review.id`, a refetch that returns the same ids preserves it. A refetch that
  *removes* the row (server-side expiry mid-confirmation) unmounts it, which is the
  right outcome — the session is gone.
- **`isLoading` semantics.** FR-5.5 depends on `isLoading` being false during
  background refetches. This is TanStack Query v5 behaviour (`isLoading` is
  `isPending && isFetching` for a first load); the page test asserting "rows survive
  a refetch" is what keeps that assumption honest rather than assumed.

---

## 10. Definition of done

The PRD's §10 acceptance criteria, unchanged, plus:

- `git diff main --stat` shows no path under `apps/backend/`.
- `apps/frontend/package.json` `dependencies` is byte-identical to `main`.
- `stageLabel` is defined once, in `src/lib/stageLabel.ts`, imported by both
  `ReviewStatus.tsx` and `ResumeReviewRow.tsx`.
- Full verification per CLAUDE.md: frontend `npm run lint`, `npm run format:check`,
  `npm test`, `npm run build`; backend suite run to prove it is unaffected; root
  `make lint`, `make test`, `make test-integration`, `make build`,
  `make docker-build`.
- `superpowers:requesting-code-review` run including `frontend-guidelines-reviewer`,
  findings in `audit.md`, addressed before the PR.
