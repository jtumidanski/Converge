# Resume Active Reviews — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Resume a review" section to the root page that lists every active review session returned by `GET /api/reviews`, letting a reviewer re-open or discard any of them.

**Architecture:** Frontend-only. Two pure helper modules (`relativeTime.ts`, `stageLabel.ts`) are added under `src/lib/`, the latter extracted verbatim from `ReviewStatus.tsx` so the stage vocabulary lives in one place. Two new presentational components (`ResumeReviewList`, `ResumeReviewRow`) render the section; `SelectRepositoryPage` owns the `useReviews()` query, the `useFinishReview()` mutation, the error toast, and navigation, matching the existing page/`RepositoryList` split. `useReviews()` gains a conditional `refetchInterval` shaped exactly like `useReview()`'s.

**Tech Stack:** React 19 + TypeScript, Vite, TanStack React Query v5, react-router, shadcn/ui (`Badge`, `Button`, `Skeleton`), Tailwind, `sonner` for toasts, Vitest + Testing Library + MSW.

**Spec:** `docs/tasks/task-003-resume-active-reviews/design.md` (requirements in `prd.md`, layout in `ux-flow.md`)

## Global Constraints

- **No backend change.** `git diff main --stat` must show no path under `apps/backend/`. If a backend change appears necessary, stop and revisit the PRD.
- **No new runtime dependency.** `apps/frontend/package.json` `dependencies` must be byte-identical to `main`. Relative time uses the platform `Intl.RelativeTimeFormat`.
- **All user-facing copy lives in `src/lib/strings.ts`.** Composed sentences are assembled in components from those parts.
- **Product vocabulary:** "PRs/MRs", never "commits" or "SHAs". No git terminology anywhere in this section — it is confined to Diagnostics on `ReviewPage`.
- **`stageLabel` is defined exactly once**, in `src/lib/stageLabel.ts`, imported by both `ReviewStatus.tsx` and `ResumeReviewRow.tsx`.
- **The client filters nothing and re-sorts nothing.** Rows render in the order the server returns them.
- **Change numbers always come from `attributes.changes`, never from `included`.** `included` is not read by this feature at all.
- **Totals glyphs are `+` and U+2212 (`−`)**, matching `ReviewHeader.tsx` — not an ASCII hyphen.
- **Unknown status must degrade, never throw.** No `assertUnreachable` in this feature; status is treated as data.
- Prettier: `printWidth` 100, double quotes, semicolons, trailing commas. Run `npm run format` before `format:check` if unsure.
- Node is not always on `PATH`. If `npm` is missing: `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22`.
- All frontend commands run with cwd = `apps/frontend`.

---

## File Structure

| Path | Change | Responsibility |
|---|---|---|
| `src/lib/relativeTime.ts` | create | `relativeTime(iso, now?)` and `expiryLabel(iso, now?)`. Pure, no React. |
| `src/lib/__tests__/relativeTime.test.ts` | create | Boundary coverage for both helpers. |
| `src/lib/stageLabel.ts` | create | `stageLabel(stage)` moved out of `ReviewStatus.tsx`, unchanged. |
| `src/components/features/review/ReviewStatus.tsx` | modify | Import `stageLabel` instead of defining it. |
| `src/lib/strings.ts` | modify | New copy keys. |
| `src/lib/hooks/api/useReviews.ts` | modify | `useReviews()` gains conditional `refetchInterval`. |
| `src/lib/hooks/api/__tests__/useReviews.test.tsx` | modify | Cover the new polling behaviour. |
| `src/components/features/reviews/ResumeReviewRow.tsx` | create | One row: badge, identity line, detail line, time line, two-step action area. |
| `src/components/features/reviews/ResumeReviewList.tsx` | create | The section: heading, count, and one of {error, skeletons, empty, rows}. |
| `src/components/features/reviews/__tests__/ResumeReviewList.test.tsx` | create | Component-level coverage with props, no network. |
| `src/pages/SelectRepositoryPage.tsx` | modify | Own the query, the mutation, the toast, and navigation; render the section. |
| `src/pages/__tests__/SelectRepositoryPage.test.tsx` | modify | Integration coverage through MSW. |

Note the directory: `src/components/features/reviews/` (plural) is new and sits alongside the existing `src/components/features/review/` (singular, the detail-page components). This mirrors `providers/` and `repositories/`, which are also plural list-oriented feature directories.

---

## Task 1: Relative-time helpers

**Files:**
- Create: `apps/frontend/src/lib/relativeTime.ts`
- Create: `apps/frontend/src/lib/__tests__/relativeTime.test.ts`
- Modify: `apps/frontend/src/lib/strings.ts`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `relativeTime(iso: string, now?: Date): string` — e.g. `"12 minutes ago"`; `""` for an unparseable input.
  - `expiryLabel(iso: string, now?: Date): { text: string; nearExpiry: boolean }` — `{ text: "expired", nearExpiry: true }` when at or before `now`; `{ text: "in 5 hours", nearExpiry: msRemaining < 3_600_000 }` otherwise; `{ text: "", nearExpiry: false }` for an unparseable input.
  - `strings.expired` = `"expired"`.

**Design notes for the implementer:**
- The formatter is constructed **once at module scope**, not per call: `Intl` constructors are expensive and the list re-renders every 2 s while polling.
- Unit selection: pick the largest unit whose absolute magnitude is ≥ 1, walking day → hour → minute → second. Below one second, fall back to seconds (which `numeric: "auto"` renders as "now").
- `numeric: "auto"` is what turns `-1 day` into "yesterday" and `0 seconds` into "now". Tests below assert substrings (`/minute/`), not exact ICU output, so they do not break on a locale-data update.
- `expiryLabel` returns only the relative fragment (`"in 5 hours"`); the row composes `"expires {text}"` from `strings`. The already-expired case returns the complete word `"expired"` instead, so the row must not blindly prefix — see Task 4's compose rule.
- The `< 3_600_000` boundary means exactly 60 minutes remaining is **not** near-expiry. This is stated in the test.

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/lib/__tests__/relativeTime.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { expiryLabel, relativeTime } from "@/lib/relativeTime";

const now = new Date("2026-09-10T12:00:00Z");

function offset(ms: number): string {
  return new Date(now.getTime() + ms).toISOString();
}

describe("relativeTime", () => {
  it("formats a timestamp seconds in the past", () => {
    expect(relativeTime(offset(-40_000), now)).toMatch(/second/);
  });

  it("formats a timestamp minutes in the past", () => {
    expect(relativeTime(offset(-12 * 60_000), now)).toMatch(/12 minutes ago/);
  });

  it("formats a timestamp hours in the past", () => {
    expect(relativeTime(offset(-5 * 3_600_000), now)).toMatch(/5 hours ago/);
  });

  it("formats a timestamp days in the past", () => {
    expect(relativeTime(offset(-3 * 86_400_000), now)).toMatch(/3 days ago/);
  });

  it("returns an empty string for an unparseable timestamp", () => {
    expect(relativeTime("not-a-date", now)).toBe("");
  });
});

describe("expiryLabel", () => {
  it("flags an expiry 59 minutes away as near", () => {
    const result = expiryLabel(offset(59 * 60_000), now);
    expect(result.nearExpiry).toBe(true);
    expect(result.text).toMatch(/minute/);
  });

  // The boundary is strict: exactly 60 minutes remaining is NOT near-expiry.
  it("does not flag an expiry exactly 60 minutes away as near", () => {
    expect(expiryLabel(offset(60 * 60_000), now).nearExpiry).toBe(false);
  });

  it("does not flag an expiry 61 minutes away as near", () => {
    const result = expiryLabel(offset(61 * 60_000), now);
    expect(result.nearExpiry).toBe(false);
    expect(result.text).toMatch(/hour|minute/);
  });

  it("renders an already-past expiry as expired rather than a negative duration", () => {
    const result = expiryLabel(offset(-5 * 60_000), now);
    expect(result.text).toBe("expired");
    expect(result.nearExpiry).toBe(true);
    expect(result.text).not.toMatch(/-/);
  });

  it("renders an expiry exactly at now as expired", () => {
    expect(expiryLabel(offset(0), now).text).toBe("expired");
  });

  it("returns an empty label for an unparseable timestamp", () => {
    expect(expiryLabel("not-a-date", now)).toEqual({ text: "", nearExpiry: false });
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/lib/__tests__/relativeTime.test.ts`
Expected: FAIL — cannot resolve `@/lib/relativeTime`.

- [ ] **Step 3: Add the `expired` string**

In `apps/frontend/src/lib/strings.ts`, add to the `strings` object (keep the existing entries untouched):

```ts
  expired: "expired",
```

- [ ] **Step 4: Write the implementation**

Create `apps/frontend/src/lib/relativeTime.ts`:

```ts
import { strings } from "@/lib/strings";

/**
 * A single module-scope formatter. Intl constructors are expensive and the
 * resume list re-renders every 2 s while a review is building.
 */
const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

const MINUTE_MS = 60_000;
const HOUR_MS = 3_600_000;
const DAY_MS = 86_400_000;

/** NEAR_EXPIRY_MS is strict: exactly one hour remaining is not near-expiry. */
const NEAR_EXPIRY_MS = HOUR_MS;

const UNITS: ReadonlyArray<[Intl.RelativeTimeFormatUnit, number]> = [
  ["day", DAY_MS],
  ["hour", HOUR_MS],
  ["minute", MINUTE_MS],
  ["second", 1000],
];

/** format renders a signed millisecond delta in the largest unit of magnitude >= 1. */
function format(deltaMs: number): string {
  for (const [unit, size] of UNITS) {
    if (Math.abs(deltaMs) >= size) {
      return formatter.format(Math.round(deltaMs / size), unit);
    }
  }
  return formatter.format(0, "second");
}

/** parse returns the epoch milliseconds of an ISO timestamp, or undefined if unparseable. */
function parse(iso: string): number | undefined {
  const ms = Date.parse(iso);
  return Number.isNaN(ms) ? undefined : ms;
}

/**
 * relativeTime formats a past timestamp, e.g. "12 minutes ago". An unparseable
 * timestamp yields an empty string rather than "Invalid Date" (NFR-5).
 */
export function relativeTime(iso: string, now: Date = new Date()): string {
  const ms = parse(iso);
  if (ms === undefined) return "";
  return format(ms - now.getTime());
}

/**
 * expiryLabel formats a future timestamp as a relative fragment ("in 5 hours")
 * and reports whether it is within the near-expiry window. A timestamp at or
 * before `now` yields the word "expired" -- never a negative duration (FR-3.10).
 */
export function expiryLabel(
  iso: string,
  now: Date = new Date(),
): { text: string; nearExpiry: boolean } {
  const ms = parse(iso);
  if (ms === undefined) return { text: "", nearExpiry: false };
  const remaining = ms - now.getTime();
  if (remaining <= 0) return { text: strings.expired, nearExpiry: true };
  return { text: format(remaining), nearExpiry: remaining < NEAR_EXPIRY_MS };
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/lib/__tests__/relativeTime.test.ts`
Expected: PASS, 11 tests.

- [ ] **Step 6: Commit**

```bash
git add apps/frontend/src/lib/relativeTime.ts apps/frontend/src/lib/__tests__/relativeTime.test.ts apps/frontend/src/lib/strings.ts
git commit -m "feat(frontend): add relative-time helpers for review expiry"
```

---

## Task 2: Extract `stageLabel` into a shared module

**Files:**
- Create: `apps/frontend/src/lib/stageLabel.ts`
- Modify: `apps/frontend/src/components/features/review/ReviewStatus.tsx`
- Test: `apps/frontend/src/components/features/review/__tests__/ReviewStatus.test.tsx` (existing, unmodified — it is the guard that proves the move was pure)

**Interfaces:**
- Consumes: nothing.
- Produces: `stageLabel(stage: string | null): string`, imported by `ReviewStatus.tsx` (Task 2) and `ResumeReviewRow.tsx` (Task 4).

This is a **pure move**. The function body is copied verbatim; the only edits are adding `export` and changing `ReviewStatus.tsx` to import it. Do not "improve" the mapping — the existing `ReviewStatus` test covers it through the component, and any behaviour change would show up there.

- [ ] **Step 1: Run the existing test to establish the baseline**

Run: `cd apps/frontend && npx vitest run src/components/features/review/__tests__/ReviewStatus.test.tsx`
Expected: PASS. (If this file does not exist, run `npx vitest run src/components/features/review` instead and note which tests cover stage labels; the baseline is whatever passes now.)

- [ ] **Step 2: Create the shared module**

Create `apps/frontend/src/lib/stageLabel.ts`:

```ts
/** stageLabel converts a backend stage into product vocabulary (FR-10.11). */
export function stageLabel(stage: string | null): string {
  if (!stage) return "Building the review";
  if (stage.startsWith("applying:")) return `Applying #${stage.slice("applying:".length)}`;
  switch (stage) {
    case "resolving":
      return "Resolving PRs/MRs";
    case "updating-repository":
      return "Updating repository";
    case "creating-workspace":
      return "Preparing review";
    case "diffing":
      return "Computing the combined diff";
    default:
      return "Building the review";
  }
}
```

- [ ] **Step 3: Point `ReviewStatus.tsx` at it**

Rewrite `apps/frontend/src/components/features/review/ReviewStatus.tsx` to:

```tsx
import { Skeleton } from "@/components/ui/skeleton";
import { stageLabel } from "@/lib/stageLabel";

interface ReviewStatusProps {
  stage: string | null;
}

export function ReviewStatus({ stage }: ReviewStatusProps) {
  return (
    <div className="flex flex-col gap-4" aria-live="polite">
      <p className="text-sm font-medium text-foreground">{stageLabel(stage)}</p>
      <Skeleton className="h-8 w-1/3" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}
```

- [ ] **Step 4: Verify the move was pure**

Run: `cd apps/frontend && npx vitest run src/components/features/review && npx tsc -b`
Expected: PASS, with the same test count as Step 1. A failure here means the move was not pure — revert the body edit rather than adjusting the test.

- [ ] **Step 5: Confirm there is exactly one definition**

Run: `cd apps/frontend && grep -rn "function stageLabel" src/`
Expected: exactly one line, in `src/lib/stageLabel.ts`.

- [ ] **Step 6: Commit**

```bash
git add apps/frontend/src/lib/stageLabel.ts apps/frontend/src/components/features/review/ReviewStatus.tsx
git commit -m "refactor(frontend): extract stageLabel into a shared module"
```

---

## Task 3: Conditional polling in `useReviews()`

**Files:**
- Modify: `apps/frontend/src/lib/hooks/api/useReviews.ts`
- Test: `apps/frontend/src/lib/hooks/api/__tests__/useReviews.test.tsx`

**Interfaces:**
- Consumes: `isTerminal(status)` from `@/types/models/review` (already imported in this file), `Review`.
- Produces: `useReviews()` — unchanged call signature, returning the standard React Query result over `Review[]`; now polls at 2000 ms while any listed review is non-terminal.

**Design notes:**
- `isTerminal` is `status !== "CREATING"`, so "still building" has one definition shared with `useReview()`.
- `staleTime` is deliberately left at the client default. Setting `refetchInterval` does not require `staleTime: 0`, and unlike `useReview()` this query is not driving a build view.
- The existing test file already has a `reviewAttrs(status)` factory near the top — reuse it. Note it takes a `string`, so it produces an object that needs no cast for MSW.
- The test asserts the *behaviour* (request count stops growing) rather than reading the option, matching how `useReview`'s polling test in the same file is written. It uses real timers with an explicit `timeout`, as the existing test does.

- [ ] **Step 1: Write the failing tests**

Append to `apps/frontend/src/lib/hooks/api/__tests__/useReviews.test.tsx` (and add `useReviews` to the import list from `@/lib/hooks/api/useReviews` at the top of the file):

```tsx
describe("useReviews", () => {
  it("polls while any listed review is CREATING and stops once none is", async () => {
    let calls = 0;
    server.use(
      http.get("/api/reviews", () => {
        calls += 1;
        return HttpResponse.json(
          listDoc([oneDoc("reviews", "abc12345", reviewAttrs(calls < 2 ? "CREATING" : "READY")).data]),
        );
      }),
    );
    const { result } = renderHook(() => useReviews(), { wrapper: queryWrapper() });
    await waitFor(() => expect(result.current.data?.[0]?.attributes.status).toBe("CREATING"));
    await waitFor(() => expect(result.current.data?.[0]?.attributes.status).toBe("READY"), {
      timeout: 6000,
    });
    const callsAtReady = calls;
    await new Promise((resolve) => setTimeout(resolve, 2500));
    expect(calls).toBe(callsAtReady);
  }, 15000);

  it("does not poll a list containing only settled reviews", async () => {
    let calls = 0;
    server.use(
      http.get("/api/reviews", () => {
        calls += 1;
        return HttpResponse.json(
          listDoc([oneDoc("reviews", "abc12345", reviewAttrs("READY")).data]),
        );
      }),
    );
    const { result } = renderHook(() => useReviews(), { wrapper: queryWrapper() });
    await waitFor(() => expect(result.current.data).toHaveLength(1));
    await new Promise((resolve) => setTimeout(resolve, 2500));
    expect(calls).toBe(1);
  }, 15000);

  it("does not poll an empty list", async () => {
    let calls = 0;
    server.use(
      http.get("/api/reviews", () => {
        calls += 1;
        return HttpResponse.json(listDoc([]));
      }),
    );
    const { result } = renderHook(() => useReviews(), { wrapper: queryWrapper() });
    await waitFor(() => expect(result.current.data).toEqual([]));
    await new Promise((resolve) => setTimeout(resolve, 2500));
    expect(calls).toBe(1);
  }, 15000);
});
```

If `listDoc` is not already imported in that file, add it to the existing `@/test/server` import.

- [ ] **Step 2: Run the tests to verify the first one fails**

Run: `cd apps/frontend && npx vitest run src/lib/hooks/api/__tests__/useReviews.test.tsx -t "useReviews"`
Expected: the polling test FAILS (status stays `CREATING`; the second request is never issued). The two "does not poll" tests pass already — that is correct, they are regression guards.

- [ ] **Step 3: Add the conditional interval**

In `apps/frontend/src/lib/hooks/api/useReviews.ts`, replace:

```ts
export function useReviews() {
  return useQuery({ queryKey: reviewKeys.lists(), queryFn: () => reviewsService.list() });
}
```

with:

```ts
/**
 * useReviews polls every 2 s while any listed review is still building, and
 * stops once none is -- an idle root page issues no periodic requests (NFR-1).
 */
export function useReviews() {
  return useQuery({
    queryKey: reviewKeys.lists(),
    queryFn: () => reviewsService.list(),
    refetchInterval: (query) => {
      const data = query.state.data as Review[] | undefined;
      return data?.some((review) => !isTerminal(review.attributes.status)) ? 2000 : false;
    },
  });
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/lib/hooks/api/__tests__/useReviews.test.tsx`
Expected: PASS, including the pre-existing tests in the file.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/src/lib/hooks/api/useReviews.ts apps/frontend/src/lib/hooks/api/__tests__/useReviews.test.tsx
git commit -m "feat(frontend): poll the review list while any review is building"
```

---

## Task 4: `ResumeReviewRow`

**Files:**
- Create: `apps/frontend/src/components/features/reviews/ResumeReviewRow.tsx`
- Modify: `apps/frontend/src/lib/strings.ts`

**Interfaces:**
- Consumes: `stageLabel` (Task 2), `relativeTime` / `expiryLabel` (Task 1), `Review` and `ReviewErrorPayload` from `@/types/models/review`, `Badge`, `Button`, `Loader2`.
- Produces:
  ```ts
  interface ResumeReviewRowProps {
    review: Review;
    pending: boolean;
    onResume: (id: string) => void;
    onDiscard: (id: string) => void;
  }
  export function ResumeReviewRow(props: ResumeReviewRowProps): ReactElement;
  ```
  Consumed by `ResumeReviewList` (Task 5). The row renders an `<li>`; the list supplies the `<ul>`.

**Design notes — read before writing code:**
- **Status is data, not a union.** `statusPresentation` takes `string` and has a `default:` branch that returns `{ label: status, variant: "outline" }`. There is no `assertUnreachable` here. `FINISHED`/`EXPIRED` are in the `ReviewStatus` union and would land in that branch if a stale cached list ever held one — that is the correct degraded behaviour (FR-2.4, NFR-5).
- **Change numbers come only from `attributes.changes`.** `included` is not read. One source, stable from the first frame (design §3.4).
- **Totals are omitted entirely when `totals` is null** — never rendered as zeros (FR-3.4).
- **The error summary is one line**: the code, plus `on #N` when `error.change` is present. `message`, `conflictingFiles`, `appliedChanges`, and `diagnostics` stay on `ReviewPage` (FR-3.6).
- **`confirming` resets on unmount naturally.** The row is keyed by `review.id` in the list, so a poll that returns the same ids preserves an open confirmation; a poll that drops the row unmounts it, which is correct.
- **`confirming` must also reset when a discard settles.** The row cannot observe the mutation's outcome directly, so it resets on the `pending` → `false` transition is *not* used (that needs an effect). Instead: the row clears `confirming` synchronously in its own click handler, before calling `onDiscard`. On success the row unmounts anyway; on failure the reviewer sees the toast against a row in its resting state (design §4.1).
- **Accessible names** identify the row, because "Resume"/"Discard" repeat on every row.
- Only the `CREATING` stage text sits in `aria-live="polite"`. The expiry text is **not** in a live region — it drifts every poll and announcing it repeatedly would be noise. Instead the expiry element carries `role="status"` only when `nearExpiry` is true, so the transition into urgency is announced once (design §3.6).
- Composing the expiry line: `expiryLabel` returns `"expired"` (a complete word) or a fragment like `"in 5 hours"`. Compose as `text === strings.expired ? strings.expired : \`${strings.expires} ${text}\``, so the past case never reads "expires expired".

- [ ] **Step 1: Add the copy**

In `apps/frontend/src/lib/strings.ts`, add these keys (alongside `expired` from Task 1; `conflict` and `discardReview` already exist and are reused, not duplicated):

```ts
  resumeReview: "Resume a review",
  resume: "Resume",
  discard: "Discard",
  cancel: "Cancel",
  started: "started",
  expires: "expires",
  statusReady: "Ready",
  statusBuilding: "Building",
  statusFailed: "Failed",
  discardConfirmSuffix: "This cannot be undone.",
  noReviewsInProgressTitle: "No reviews in progress.",
  noReviewsInProgressDescription: "Start one by choosing a repository below.",
  reviewsUnavailableTitle: "Could not load reviews in progress",
  reviewDiscardFailed: "The review could not be discarded.",
```

`noReviewsInProgress*` and `reviewsUnavailableTitle` are used by Task 5; `reviewDiscardFailed` by Task 6. Adding them all now keeps `strings.ts` to one edit.

- [ ] **Step 2: Write the component**

Create `apps/frontend/src/components/features/reviews/ResumeReviewRow.tsx`:

```tsx
import { useState } from "react";
import { Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { expiryLabel, relativeTime } from "@/lib/relativeTime";
import { stageLabel } from "@/lib/stageLabel";
import { strings } from "@/lib/strings";
import type { Review, ReviewErrorPayload } from "@/types/models/review";

interface ResumeReviewRowProps {
  review: Review;
  /** pending is true only for the row whose discard is in flight (FR-4.4). */
  pending: boolean;
  onResume: (id: string) => void;
  onDiscard: (id: string) => void;
}

type BadgeVariant = "default" | "secondary" | "destructive" | "outline";

/**
 * statusPresentation deliberately takes a string rather than the ReviewStatus
 * union: this list must degrade on a status it does not recognise rather than
 * fail to render (FR-2.4, NFR-5). ReviewPage's exhaustive switch is the right
 * pattern there because it picks a layout; here status is data.
 */
function statusPresentation(status: string): { label: string; variant: BadgeVariant } {
  switch (status) {
    case "READY":
      return { label: strings.statusReady, variant: "default" };
    case "CREATING":
      return { label: strings.statusBuilding, variant: "secondary" };
    case "CONFLICTED":
      return { label: strings.conflict, variant: "destructive" };
    case "FAILED":
      return { label: strings.statusFailed, variant: "destructive" };
    default:
      return { label: status, variant: "outline" };
  }
}

/** errorSummary renders one line: the code, and the change it concerns when known. */
function errorSummary(error: ReviewErrorPayload): string {
  return error.change === undefined ? error.code : `${error.code} on #${error.change}`;
}

export function ResumeReviewRow({ review, pending, onResume, onDiscard }: ResumeReviewRowProps) {
  const [confirming, setConfirming] = useState(false);
  const { status, stage, provider, repository, changes, totals, error, createdAt, expiresAt } =
    review.attributes;
  const presentation = statusPresentation(status);
  const changeLabel = changes.map((number) => `#${number}`).join(", ");
  const expiry = expiryLabel(expiresAt);
  const expiryText =
    expiry.text === "" || expiry.text === strings.expired
      ? expiry.text
      : `${strings.expires} ${expiry.text}`;

  function confirmDiscard() {
    // Clear the armed state before firing: on success the row unmounts, and on
    // failure the reviewer should see the toast against a resting row.
    setConfirming(false);
    onDiscard(review.id);
  }

  return (
    <li className="flex flex-col gap-2 rounded-md border border-border p-4">
      <div className="flex items-center gap-2">
        <Badge variant={presentation.variant}>{presentation.label}</Badge>
        <span className="truncate text-sm text-muted-foreground">{provider}</span>
        <span className="truncate text-sm font-medium text-foreground">{repository}</span>
      </div>

      <p className="text-sm text-muted-foreground">
        <span>{changeLabel}</span>
        {status === "CREATING" ? (
          <>
            {" · "}
            <span aria-live="polite">{stageLabel(stage)}</span>
          </>
        ) : null}
        {totals ? (
          <span>
            {" · "}
            {totals.files} files · +{totals.additions} −{totals.deletions}
          </span>
        ) : null}
        {error ? <span>{" · "}{errorSummary(error)}</span> : null}
      </p>

      <p className="text-sm text-muted-foreground">
        {strings.started} {relativeTime(createdAt)}
        {expiryText ? (
          <>
            {" · "}
            <span
              className={expiry.nearExpiry ? "font-medium text-destructive" : undefined}
              {...(expiry.nearExpiry ? { role: "status" } : {})}
            >
              {expiryText}
            </span>
          </>
        ) : null}
      </p>

      {confirming ? (
        <div className="flex flex-wrap items-center justify-end gap-2">
          <span className="mr-auto text-sm text-foreground">
            {strings.discardReview}: {repository} ({changeLabel})? {strings.discardConfirmSuffix}
          </span>
          <Button variant="outline" size="sm" onClick={() => setConfirming(false)}>
            {strings.cancel}
          </Button>
          <Button variant="destructive" size="sm" autoFocus onClick={confirmDiscard}>
            {strings.discard}
          </Button>
        </div>
      ) : (
        <div className="flex items-center justify-end gap-2">
          <Button
            size="sm"
            disabled={pending}
            aria-label={`${strings.resume} review of ${repository} (${changeLabel})`}
            onClick={() => onResume(review.id)}
          >
            {strings.resume}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={pending}
            aria-label={`${strings.discard} review of ${repository} (${changeLabel})`}
            onClick={() => setConfirming(true)}
          >
            {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            {strings.discard}
          </Button>
        </div>
      )}
    </li>
  );
}
```

- [ ] **Step 3: Type-check**

Run: `cd apps/frontend && npx tsc -b`
Expected: no errors. (This task has no test file of its own — the row is exercised through `ResumeReviewList` in Task 5, which is how it is rendered in production.)

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/src/components/features/reviews/ResumeReviewRow.tsx apps/frontend/src/lib/strings.ts
git commit -m "feat(frontend): add the resume-review row component"
```

---

## Task 5: `ResumeReviewList` and its tests

**Files:**
- Create: `apps/frontend/src/components/features/reviews/ResumeReviewList.tsx`
- Create: `apps/frontend/src/components/features/reviews/__tests__/ResumeReviewList.test.tsx`

**Interfaces:**
- Consumes: `ResumeReviewRow` (Task 4), `EmptyState`, `ErrorBanner`, `Skeleton`, `messageFor`, `strings`.
- Produces:
  ```ts
  interface ResumeReviewListProps {
    reviews: Review[];
    loading: boolean;
    error?: unknown;
    onRetry: () => void;
    pendingId?: string;
    onResume: (id: string) => void;
    onDiscard: (id: string) => void;
  }
  export function ResumeReviewList(props: ResumeReviewListProps): ReactElement;
  ```
  Consumed by `SelectRepositoryPage` (Task 6).

**Design notes:**
- **State precedence is exactly: error → loading → empty → rows** (design §2.5). The heading renders above all four; the count renders only when there are rows (FR-1.5).
- `loading` is wired by the page to `isLoading`, never `isFetching` — that is what keeps a background refetch from replacing rendered rows with skeletons (FR-5.5). The list itself just honours the prop.
- The section is a `<section>` with an `<h2>`; the rows are a `<ul>` of `<li>`. Not a `<Table>` — the row is a three-line block with a variable detail line, and a table would need column spans or break the responsive requirement (design §3.1).
- The error banner is scoped to this section. The page must keep rendering the provider picker and repository list around it (FR-5.3) — enforced by the page test in Task 6.

- [ ] **Step 1: Write the failing tests**

Create `apps/frontend/src/components/features/reviews/__tests__/ResumeReviewList.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ResumeReviewList } from "@/components/features/reviews/ResumeReviewList";
import type { Review, ReviewAttributes } from "@/types/models/review";

function makeReview(id: string, overrides: Partial<ReviewAttributes> = {}): Review {
  return {
    type: "reviews",
    id,
    attributes: {
      status: "READY",
      stage: null,
      provider: "gitlab-work",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: "a".repeat(40),
      headSha: "b".repeat(40),
      baseDescription: "Immediately before #12",
      changes: [12, 14, 15],
      included: [],
      totals: { files: 5, additions: 84, deletions: 12 },
      error: null,
      createdAt: new Date(Date.now() - 12 * 60_000).toISOString(),
      updatedAt: new Date(Date.now() - 60_000).toISOString(),
      expiresAt: new Date(Date.now() + 5 * 3_600_000).toISOString(),
      ...overrides,
    },
  };
}

function renderList(props: Partial<Parameters<typeof ResumeReviewList>[0]> = {}) {
  const onResume = vi.fn();
  const onDiscard = vi.fn();
  const onRetry = vi.fn();
  render(
    <ResumeReviewList
      reviews={[makeReview("r1")]}
      loading={false}
      onRetry={onRetry}
      onResume={onResume}
      onDiscard={onDiscard}
      {...props}
    />,
  );
  return { onResume, onDiscard, onRetry };
}

describe("ResumeReviewList", () => {
  it("renders a heading and a count of the listed reviews", () => {
    renderList({ reviews: [makeReview("r1"), makeReview("r2")] });
    expect(screen.getByRole("heading", { name: /resume a review/i })).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("renders a READY row with repository, provider, changes and totals", () => {
    renderList();
    expect(screen.getByText("Ready")).toBeInTheDocument();
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
    expect(screen.getByText("gitlab-work")).toBeInTheDocument();
    expect(screen.getByText(/#12, #14, #15/)).toBeInTheDocument();
    expect(screen.getByText(/5 files/)).toBeInTheDocument();
    expect(screen.getByText(/\+84/)).toBeInTheDocument();
  });

  it("omits totals rather than rendering zeros when totals is null", () => {
    renderList({ reviews: [makeReview("r1", { totals: null })] });
    expect(screen.queryByText(/files/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\+0/)).not.toBeInTheDocument();
  });

  it("renders a CREATING row with the shared stage label", () => {
    renderList({ reviews: [makeReview("r1", { status: "CREATING", stage: "resolving", totals: null })] });
    expect(screen.getByText("Building")).toBeInTheDocument();
    expect(screen.getByText("Resolving PRs/MRs")).toBeInTheDocument();
  });

  it("renders a CONFLICTED row with a one-line error summary naming the change", () => {
    renderList({
      reviews: [
        makeReview("r1", {
          status: "CONFLICTED",
          totals: null,
          error: { code: "CONFLICT", message: "A long panel-sized sentence.", change: 14 },
        }),
      ],
    });
    expect(screen.getByText("Conflict")).toBeInTheDocument();
    expect(screen.getByText(/CONFLICT on #14/)).toBeInTheDocument();
    expect(screen.queryByText(/panel-sized/)).not.toBeInTheDocument();
  });

  it("renders a FAILED row without a change number", () => {
    renderList({
      reviews: [
        makeReview("r1", {
          status: "FAILED",
          totals: null,
          error: { code: "GIT_FAILURE", message: "…" },
        }),
      ],
    });
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText(/GIT_FAILURE/)).toBeInTheDocument();
  });

  it("renders an unrecognised status as a neutral badge rather than throwing", () => {
    const review = makeReview("r1", { totals: null });
    // Deliberately outside the union: the server may add a status the client
    // does not know, and a stale cached list may hold FINISHED/EXPIRED.
    (review.attributes as { status: string }).status = "SOMETHING_NEW";
    renderList({ reviews: [review] });
    expect(screen.getByText("SOMETHING_NEW")).toBeInTheDocument();
  });

  it("marks a near expiry with a warning role", () => {
    renderList({
      reviews: [makeReview("r1", { expiresAt: new Date(Date.now() + 47 * 60_000).toISOString() })],
    });
    expect(screen.getByRole("status")).toHaveTextContent(/expires in/i);
  });

  it("renders an already-past expiry as expired, not a negative duration", () => {
    renderList({
      reviews: [makeReview("r1", { expiresAt: new Date(Date.now() - 60_000).toISOString() })],
    });
    expect(screen.getByText(/expired/)).toBeInTheDocument();
  });

  it("renders skeletons and no empty state while loading", () => {
    renderList({ reviews: [], loading: true });
    expect(screen.queryByText(/no reviews in progress/i)).not.toBeInTheDocument();
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });

  it("renders the empty state once the query resolves with no reviews", () => {
    renderList({ reviews: [], loading: false });
    expect(screen.getByText(/no reviews in progress/i)).toBeInTheDocument();
  });

  it("renders a retryable error banner and calls onRetry", async () => {
    const { onRetry } = renderList({ reviews: [], error: new Error("boom") });
    expect(screen.getByRole("alert")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("calls onResume with the review id", async () => {
    const { onResume } = renderList();
    await userEvent.click(screen.getByRole("button", { name: /resume review of atlas\/server/i }));
    expect(onResume).toHaveBeenCalledWith("r1");
  });

  it("does not discard until the confirmation is accepted", async () => {
    const { onDiscard } = renderList();
    await userEvent.click(screen.getByRole("button", { name: /discard review of atlas\/server/i }));
    expect(onDiscard).not.toHaveBeenCalled();
    expect(screen.getByText(/cannot be undone/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /^cancel$/i }));
    expect(onDiscard).not.toHaveBeenCalled();
    expect(screen.queryByText(/cannot be undone/i)).not.toBeInTheDocument();
  });

  it("calls onDiscard once when the confirmation is accepted", async () => {
    const { onDiscard } = renderList();
    await userEvent.click(screen.getByRole("button", { name: /discard review of atlas\/server/i }));
    await userEvent.click(screen.getByRole("button", { name: /^discard$/i }));
    expect(onDiscard).toHaveBeenCalledTimes(1);
    expect(onDiscard).toHaveBeenCalledWith("r1");
  });

  it("disables only the pending row's controls", () => {
    renderList({
      reviews: [makeReview("r1"), makeReview("r2", { repository: "web/ui" })],
      pendingId: "r1",
    });
    expect(screen.getByRole("button", { name: /resume review of atlas\/server/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /resume review of web\/ui/i })).toBeEnabled();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/frontend && npx vitest run src/components/features/reviews`
Expected: FAIL — cannot resolve `@/components/features/reviews/ResumeReviewList`.

- [ ] **Step 3: Write the component**

Create `apps/frontend/src/components/features/reviews/ResumeReviewList.tsx`:

```tsx
import { EmptyState } from "@/components/common/EmptyState";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Skeleton } from "@/components/ui/skeleton";
import { ResumeReviewRow } from "@/components/features/reviews/ResumeReviewRow";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

interface ResumeReviewListProps {
  reviews: Review[];
  /** loading is the query's isLoading -- a first load only, never a background refetch. */
  loading: boolean;
  error?: unknown;
  onRetry: () => void;
  /** pendingId is the id of the review whose discard is in flight, if any. */
  pendingId?: string;
  onResume: (id: string) => void;
  onDiscard: (id: string) => void;
}

/** body renders exactly one of error, loading, empty, or rows -- in that order. */
function body(props: ResumeReviewListProps) {
  const { reviews, loading, error, onRetry, pendingId, onResume, onDiscard } = props;

  if (error !== undefined) {
    return (
      <ErrorBanner
        title={strings.reviewsUnavailableTitle}
        detail={messageFor(error, "Try again in a moment.")}
        onRetry={onRetry}
      />
    );
  }

  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2].map((row) => (
          <Skeleton key={row} className="h-24 w-full" />
        ))}
      </div>
    );
  }

  if (reviews.length === 0) {
    return (
      <EmptyState
        title={strings.noReviewsInProgressTitle}
        description={strings.noReviewsInProgressDescription}
      />
    );
  }

  return (
    <ul className="flex flex-col gap-2">
      {reviews.map((review) => (
        <ResumeReviewRow
          key={review.id}
          review={review}
          pending={pendingId === review.id}
          onResume={onResume}
          onDiscard={onDiscard}
        />
      ))}
    </ul>
  );
}

export function ResumeReviewList(props: ResumeReviewListProps) {
  const showCount = props.error === undefined && !props.loading && props.reviews.length > 0;
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-base font-semibold text-foreground">{strings.resumeReview}</h2>
        {showCount ? (
          <span className="text-sm text-muted-foreground">{props.reviews.length}</span>
        ) : null}
      </div>
      {body(props)}
    </section>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/components/features/reviews`
Expected: PASS, 16 tests.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/src/components/features/reviews/
git commit -m "feat(frontend): add the resume-review list section"
```

---

## Task 6: Wire the section into `SelectRepositoryPage`

**Files:**
- Modify: `apps/frontend/src/pages/SelectRepositoryPage.tsx`
- Test: `apps/frontend/src/pages/__tests__/SelectRepositoryPage.test.tsx`

**Interfaces:**
- Consumes: `ResumeReviewList` (Task 5), `useReviews` (Task 3), `useFinishReview` (existing), `messageFor`, `toast` from `sonner`.
- Produces: nothing consumed by later tasks.

**Design notes:**
- `pendingId` is derived from the mutation (`isPending ? variables : undefined`), **not** from component state. TanStack Query already stores the in-flight mutation's variables; a second source of truth could only get out of sync. One `useFinishReview()` for the whole list means at most one discard in flight, so a single id suffices (design §2.3).
- The discard handler mirrors `ReviewPage.closeReview()` but does **not** navigate: the reviewer stays on `/`, and the mutation's existing `onSettled` invalidation of `reviewKeys.lists()` removes the row (FR-4.5, design §2.2).
- `loading` is wired to `isLoading`, never `isFetching`.
- The section renders between the `PageHeader` and the provider error banner / `ProviderPicker` (FR-1.1).
- The existing tests in this file use `onUnhandledRequest: "bypass"`, so tests that do not stub `GET /api/reviews` will not fail — but they will render whatever the real fetch does. Add a `seedReviews()` helper and call it (or explicitly stub an empty list) in the pre-existing tests that assert on the empty/error states, so a stray resume row cannot collide with their queries.

- [ ] **Step 1: Write the failing tests**

Add to `apps/frontend/src/pages/__tests__/SelectRepositoryPage.test.tsx`. First add a helper next to the existing `seedProviders()`:

```tsx
function reviewResource(id: string, overrides: Record<string, unknown> = {}) {
  return oneDoc("reviews", id, {
    status: "READY",
    stage: null,
    provider: "gitlab-work",
    repository: "atlas/server",
    baseBranch: "main",
    baseSha: "a".repeat(40),
    headSha: "b".repeat(40),
    baseDescription: "Immediately before #12",
    changes: [12, 14, 15],
    included: [],
    totals: { files: 5, additions: 84, deletions: 12 },
    error: null,
    createdAt: new Date(Date.now() - 12 * 60_000).toISOString(),
    updatedAt: new Date(Date.now() - 60_000).toISOString(),
    expiresAt: new Date(Date.now() + 5 * 3_600_000).toISOString(),
    ...overrides,
  }).data;
}

function seedReviews(...resources: ReturnType<typeof reviewResource>[]) {
  server.use(http.get("/api/reviews", () => HttpResponse.json(listDoc(resources))));
}
```

Then add this suite:

```tsx
describe("SelectRepositoryPage resume section", () => {
  it("lists active reviews in the order the server returns them, above the provider picker", async () => {
    seedProviders();
    seedReviews(reviewResource("r1"), reviewResource("r2", { repository: "web/ui" }));
    renderWithProviders(<SelectRepositoryPage />);
    const rows = await screen.findAllByRole("listitem");
    expect(rows[0]).toHaveTextContent("atlas/server");
    expect(rows[1]).toHaveTextContent("web/ui");
    const heading = screen.getByRole("heading", { name: /resume a review/i });
    const picker = screen.getByText("Provider");
    expect(heading.compareDocumentPosition(picker) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("navigates to the review when Resume is pressed", async () => {
    seedProviders();
    seedReviews(reviewResource("r1"));
    renderWithProviders(<SelectRepositoryPage />);
    await userEvent.click(
      await screen.findByRole("button", { name: /resume review of atlas\/server/i }),
    );
    expect(navigate).toHaveBeenCalledWith("/reviews/r1");
  });

  it("removes the row after a confirmed discard succeeds", async () => {
    seedProviders();
    let discarded = false;
    server.use(
      http.get("/api/reviews", () =>
        HttpResponse.json(listDoc(discarded ? [] : [reviewResource("r1")])),
      ),
      http.delete("/api/reviews/r1", () => {
        discarded = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderWithProviders(<SelectRepositoryPage />);
    await userEvent.click(
      await screen.findByRole("button", { name: /discard review of atlas\/server/i }),
    );
    await userEvent.click(screen.getByRole("button", { name: /^discard$/i }));
    expect(await screen.findByText(/no reviews in progress/i)).toBeInTheDocument();
  });

  it("issues no request when the discard confirmation is cancelled", async () => {
    seedProviders();
    let deletes = 0;
    server.use(
      http.get("/api/reviews", () => HttpResponse.json(listDoc([reviewResource("r1")]))),
      http.delete("/api/reviews/r1", () => {
        deletes += 1;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderWithProviders(<SelectRepositoryPage />);
    await userEvent.click(
      await screen.findByRole("button", { name: /discard review of atlas\/server/i }),
    );
    await userEvent.click(screen.getByRole("button", { name: /^cancel$/i }));
    expect(deletes).toBe(0);
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
  });

  it("keeps the row when the discard fails", async () => {
    seedProviders();
    server.use(
      http.get("/api/reviews", () => HttpResponse.json(listDoc([reviewResource("r1")]))),
      http.delete("/api/reviews/r1", () =>
        HttpResponse.json(errorDoc(500, "INTERNAL", "Internal", "The worktree is locked."), {
          status: 500,
        }),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    await userEvent.click(
      await screen.findByRole("button", { name: /discard review of atlas\/server/i }),
    );
    await userEvent.click(screen.getByRole("button", { name: /^discard$/i }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /discard review of atlas\/server/i })).toBeEnabled(),
    );
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
  });

  it("keeps the repository flow working when the review list fails to load", async () => {
    seedProviders();
    server.use(
      http.get("/api/reviews", () =>
        HttpResponse.json(errorDoc(503, "UNAVAILABLE", "Unavailable", "Sessions are unreadable."), {
          status: 503,
        }),
      ),
      http.get("/api/providers/gitlab-work/repositories", () =>
        HttpResponse.json(
          listDoc(
            [
              oneDoc("repositories", "atlas/server", {
                name: "server",
                namespace: "atlas",
                defaultBranch: "main",
                webUrl: "u",
              }).data,
            ],
            { number: 1, size: 30, hasNext: false },
          ),
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/sessions are unreadable/i)).toBeInTheDocument();
    expect(await screen.findByText("atlas/server")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /select/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalled());
  });

  it("renders the empty state when there are no active reviews", async () => {
    seedProviders();
    seedReviews();
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/no reviews in progress/i)).toBeInTheDocument();
  });
});
```

Also add `seedReviews()` to the pre-existing tests in this file so an unstubbed `GET /api/reviews` cannot interfere: insert a `seedReviews();` call immediately after each existing `seedProviders();` call, and add one at the top of the two tests that stub `/api/providers` directly.

Ensure `errorDoc` and `listDoc` are in the file's `@/test/server` import.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/frontend && npx vitest run src/pages/__tests__/SelectRepositoryPage.test.tsx`
Expected: the new suite FAILS — no resume section is rendered. The pre-existing tests still pass.

- [ ] **Step 3: Wire the page**

In `apps/frontend/src/pages/SelectRepositoryPage.tsx`, add these imports:

```tsx
import { toast } from "sonner";
import { ResumeReviewList } from "@/components/features/reviews/ResumeReviewList";
import { useFinishReview, useReviews } from "@/lib/hooks/api/useReviews";
```

Inside the component, after `const repositories = useRepositories(...)`, add:

```tsx
  const reviews = useReviews();
  // Discard here and Finish Review on ReviewPage are the same backend
  // operation (DELETE /api/reviews/{id}); this list simply calls it from
  // outside the review. Unlike ReviewPage's closeReview it does not navigate --
  // the reviewer stays on `/` and the mutation's onSettled invalidation of
  // reviewKeys.lists() removes the row.
  const discardReview = useFinishReview();

  async function discard(id: string) {
    try {
      await discardReview.mutateAsync(id);
    } catch (error: unknown) {
      toast.error(messageFor(error, strings.reviewDiscardFailed));
    }
  }
```

Then render the section as the first child after `<PageHeader ... />`:

```tsx
      <ResumeReviewList
        reviews={reviews.data ?? []}
        // isLoading, not isFetching: a background poll must not replace
        // rendered rows with skeletons (FR-5.5).
        loading={reviews.isLoading}
        error={reviews.isError ? reviews.error : undefined}
        onRetry={() => void reviews.refetch()}
        // The mutation itself holds the in-flight variables, so there is no
        // second source of truth to fall out of sync (design 2.3).
        pendingId={discardReview.isPending ? discardReview.variables : undefined}
        onResume={(id) => navigate(`/reviews/${id}`)}
        onDiscard={(id) => void discard(id)}
      />
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/pages/__tests__/SelectRepositoryPage.test.tsx`
Expected: PASS, both the new suite and every pre-existing test.

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/src/pages/SelectRepositoryPage.tsx apps/frontend/src/pages/__tests__/SelectRepositoryPage.test.tsx
git commit -m "feat(frontend): surface resumable reviews on the root page"
```

---

## Task 7: Full verification

**Files:** none created or modified, unless a check fails.

**Interfaces:**
- Consumes: everything from Tasks 1–6.
- Produces: a clean branch.

Every command below must be run and its output read. Do not report a step as passing without having seen it pass — a command that was not run is not a passing command.

- [ ] **Step 1: Frontend gate**

```bash
cd apps/frontend
npm run lint
npm run format:check
npm test
npm run build
```

Expected: all four clean. If `format:check` fails, run `npm run format`, re-run `format:check`, and commit the reformatting.

- [ ] **Step 2: Backend gate — prove it is unaffected**

```bash
cd apps/backend
go vet ./...
go test -race -count=1 ./...
go tool golangci-lint run
CGO_ENABLED=0 go build ./...
```

Expected: all clean. This task changes no Go file; running the suite is how that claim is proven rather than asserted. Use an explicit generous timeout on the test and lint commands — they are slow on a cold cache.

- [ ] **Step 3: Root gate**

```bash
make lint
make test
make test-integration
make build
make docker-build
```

Expected: all clean.

- [ ] **Step 4: Prove the global constraints hold**

```bash
git diff main --stat -- apps/backend/          # expect: no output
git diff main -- apps/frontend/package.json    # expect: no output
grep -rn "function stageLabel" apps/frontend/src/   # expect: exactly one hit, src/lib/stageLabel.ts
grep -rn "included" apps/frontend/src/components/features/reviews/  # expect: no hits
```

- [ ] **Step 5: Commit any fixes**

```bash
git add -A
git commit -m "chore(frontend): verification fixes for the resume-review section"
```

Skip if nothing changed.

---

## Task 8: Code review

**Files:** `docs/tasks/task-003-resume-active-reviews/audit.md`

- [ ] **Step 1: Request review**

Invoke `superpowers:requesting-code-review`. Because only TypeScript/React files changed, it dispatches `plan-adherence-reviewer` and `frontend-guidelines-reviewer` (not the backend reviewer). Findings go to `docs/tasks/task-003-resume-active-reviews/audit.md`.

- [ ] **Step 2: Address findings**

Fix every Critical and Important finding. Re-run Task 7's frontend gate afterwards.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "docs(task-003): code review findings and fixes"
```

Only after this is a PR appropriate (CLAUDE.md, "Code Review Before PR").

---

## Traceability

| Requirement | Task |
|---|---|
| FR-1.1 placement above the picker | 6 |
| FR-1.2 always rendered | 5 |
| FR-1.3 no new route | 6 |
| FR-1.4 presentational component, data in the page | 4, 5, 6 |
| FR-1.5 heading and count | 5 |
| FR-2.1 server order, no client filtering | 5, 6 |
| FR-2.2 all four statuses in one list | 4, 5 |
| FR-2.3 no synthesised FINISHED/EXPIRED rows | 4, 5 |
| FR-2.4 unknown status degrades | 4, 5 |
| FR-3.1 status badge and variants | 4, 5 |
| FR-3.2 repository and provider | 4, 5 |
| FR-3.3 / §3.4 change numbers from `changes` | 4, 7 |
| FR-3.4 totals omitted when null | 4, 5 |
| FR-3.5 shared stage label | 2, 4, 5 |
| FR-3.6 one-line error summary | 4, 5 |
| FR-3.7 relative creation time | 1, 4 |
| FR-3.8 relative expiry with warning | 1, 4, 5 |
| FR-3.9 `Intl` helper, unit-tested | 1 |
| FR-3.10 past expiry reads "expired" | 1, 4, 5 |
| FR-4.1 Resume on all four statuses | 4, 5, 6 |
| FR-4.2 Discard via `useFinishReview()` | 6 |
| FR-4.3 explicit confirmation | 4, 5, 6 |
| FR-4.4 only the pending row disabled | 4, 5, 6 |
| FR-4.5 refetch on success, toast on failure | 6 |
| FR-4.6 same backend operation | 6 |
| FR-4.7 real focusable controls with row names | 4, 5 |
| FR-5.1 empty state | 5, 6 |
| FR-5.2 skeletons on first load | 5 |
| FR-5.3 error banner, rest of page works | 5, 6 |
| FR-5.4 conditional 2 s polling | 3 |
| FR-5.5 no skeleton flash on refetch | 5, 6 |
| FR-5.6 create already invalidates | — (existing behaviour, no change) |
| FR-6.1 copy in `strings.ts` | 1, 4 |
| FR-6.2 existing shadcn primitives only | 4, 5 |
| FR-6.3 responsive, truncating | 4 |
| NFR-1 one request per mount, polling stops | 3 |
| NFR-2 no new dependency | 1, 7 |
| NFR-3 text rendering, no new links | 4 |
| NFR-4 accessibility | 4, 5 |
| NFR-5 degraded rendering, never throws | 1, 4, 5 |
| NFR-6 no new logging; toast on failure | 6 |
| Design §6 backend unchanged | 7 |
