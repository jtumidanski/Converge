# Plan Adherence Audit — task-003-resume-active-reviews

**Plan Path:** `docs/tasks/task-003-resume-active-reviews/plan.md`
**Audit Date:** 2026-09-10
**Branch:** `task-003-resume-active-reviews`
**Base:** `main` (merge-base `2fc32e4`), head `5c61c8b`, 12 commits
**Diff package read:** `.superpowers/sdd/plan/review-2fc32e4..5c61c8b.diff`
**Ledger read:** `.superpowers/sdd/plan/progress.md`

## Executive Summary

All seven implementation tasks (1–6 plus the Task 7 verification gate) were faithfully
executed. Every file in the plan's File Structure table exists with the specified
responsibility, every test the plan dictates is present at the dictated count, and all
of the plan's Global Constraints hold under direct re-verification. Task 8 (this review)
is the remaining item. Three controller rulings departed from the plan's literal text;
all three are sound. No merge-blocking defect was found. The only substantive gaps are
two small accessibility shortfalls against NFR-4 and one untested behaviour introduced
by a ruling — all non-blocking.

Task 7's full gate (frontend lint/format/test 134-134/build; backend vet/race-test/
golangci-lint/build; root lint/test/test-integration/build/docker-build) is recorded as
passing in `.superpowers/sdd/plan/task-7-report.md` and was **not** re-run here per the
audit brief. Nothing read during this audit raised a doubt those runs do not answer.

## Task Completion

| # | Task | Status | Evidence |
|---|---|---|---|
| 1 | Relative-time helpers | DONE | `apps/frontend/src/lib/relativeTime.ts:1-63` — module-scope `Intl.RelativeTimeFormat` (:7), day→hour→minute→second walk (:16-31), `""` on unparseable (:45, :59), `strings.expired` + `nearExpiry: true` at/past now (:61), strict `< NEAR_EXPIRY_MS` boundary (:14, :62). Tests: `src/lib/__tests__/relativeTime.test.ts` — 11 `it()` blocks, matching the plan's 11 verbatim. `strings.expired` at `src/lib/strings.ts` (diff `+expired: "expired"`). Commits `4ca72c0`, `e4cf63d`. |
| 2 | Extract `stageLabel` | DONE | `apps/frontend/src/lib/stageLabel.ts:1-17` is byte-identical to the plan's block. `src/components/features/review/ReviewStatus.tsx:2` imports it; the local definition is gone and the component body is otherwise unchanged. `grep -rn "function stageLabel" apps/frontend/src/` → exactly 1 hit (`src/lib/stageLabel.ts:2`), re-run during this audit. Existing `ReviewStatus.test.tsx` untouched (not in the branch diffstat), so the purity guard is intact. Commit `3915a5b`. |
| 3 | Conditional polling in `useReviews()` | DONE | `apps/frontend/src/lib/hooks/api/useReviews.ts:29-42` — `refetchInterval` returns `2000` when `data?.some(r => !isTerminal(r.attributes.status))`, else `false`; shape mirrors `useReview()` at :22-25; `staleTime` deliberately left at the default as the plan specifies. Tests: `src/lib/hooks/api/__tests__/useReviews.test.tsx:229-283` — all three plan tests present (`polls while any listed review is CREATING…`, `does not poll a list containing only settled reviews`, `does not poll an empty list`). Commit `dd1a632`. |
| 4 | `ResumeReviewRow` | DONE (with a ratified deviation, see Ruling 1) | `apps/frontend/src/components/features/reviews/ResumeReviewRow.tsx:1-149`. `statusPresentation` takes `string` with a degrading `default:` branch (:26-38), no `assertUnreachable`. Change numbers from `attributes.changes` only (:48, :51). Totals omitted when null (:81-86) and use `+` / U+2212 (:84). One-line `errorSummary` (:41-44). `aria-live="polite"` on the stage text only (:78); `role="status"` on expiry only when `nearExpiry` (:100-102). Row-scoped `aria-label`s (:130, :139). `confirming` cleared synchronously before `onDiscard` (:58-63). Compose guard prevents "expires expired" (:53-56). 14 copy keys added to `strings.ts`. Commits `c01caf3`, `ec6afe1`, `b44441f`. |
| 5 | `ResumeReviewList` + tests | DONE (with a ratified deviation, see Ruling 2) | `apps/frontend/src/components/features/reviews/ResumeReviewList.tsx:1-82`. Precedence error → loading → empty → rows enforced by early returns at :25, :35, :45, with rows last (:54-66). Heading always rendered (:74); count gated on `error === undefined && !loading && length > 0` (:70, :75-77). `<section>`/`<h2>`/`<ul>`/`<li>` structure as specified. Tests: `__tests__/ResumeReviewList.test.tsx` — 16 `it()` blocks, matching the plan's 16 one-for-one. Commit `bf11182`. |
| 6 | Wire into `SelectRepositoryPage` | DONE | `apps/frontend/src/pages/SelectRepositoryPage.tsx:56-68` renders the section immediately after `PageHeader` (:52-55) and above the provider error banner (:69) and `ProviderPicker` (:76) — FR-1.1. `loading={reviews.isLoading}` (:60), not `isFetching`. `pendingId` derived from the mutation (:65), no component state. `discard()` awaits `mutateAsync` and toasts `messageFor(error, strings.reviewDiscardFailed)` on failure without navigating (:36-42). Tests: `src/pages/__tests__/SelectRepositoryPage.test.tsx:218-340` — all seven plan tests present; `seedReviews(` appears 11× in the file, so every pre-existing test seeds a reviews handler and the MSW `bypass` trap is closed. Commit `5c61c8b`. |
| 7 | Full verification | DONE | `.superpowers/sdd/plan/task-7-report.md`; ledger records every gate PASS with no fixes needed and a clean tree (hence no commit, as the plan's Step 5 allows). Constraint checks independently re-run in this audit: `git diff --stat 2fc32e4..HEAD -- apps/backend/` → empty; `git diff 2fc32e4..HEAD -- apps/frontend/package.json` → empty; `stageLabel` → 1 hit; `included` → 1 hit, test fixture only (Ruling 3). |
| 8 | Code review | IN PROGRESS | This document. |

**Completion Rate:** 7/7 implementation tasks (100%); Task 8 in progress.
**Skipped without approval:** 0
**Partial implementations:** 0

### Plan bookkeeping

All 40 `- [ ]` checkboxes in `plan.md` remain unchecked (`grep -c -- "- [x]"` → 0). The
execution ledger, not the plan file, was used to track state. This is cosmetic — every
step is independently evidenced above — but the plan file misrepresents the branch's
state to a reader who has not opened the ledger.

## Traceability Verification

Every row of the plan's Traceability table resolves to real code. Spot-checked
mappings with the least obvious evidence:

| Requirement | Evidence |
|---|---|
| FR-1.2 always rendered | `SelectRepositoryPage.tsx:56` — unconditional; error/empty are internal states of the section, not a reason to omit it. |
| FR-1.3 no new route | No change to any router file in the branch diffstat. |
| FR-2.1 server order, no filtering | `ResumeReviewList.tsx:56` maps `props.reviews` directly; no `.filter`/`.sort` anywhere in `features/reviews/`. |
| FR-2.3 no synthesised FINISHED/EXPIRED | No such literal in either component; they would fall through `statusPresentation`'s `default:`. |
| FR-3.3 / §3.4 changes from `changes` | `ResumeReviewRow.tsx:48,51`; `included` absent from all production source. |
| FR-4.1 Resume on all four statuses | Resume button (`ResumeReviewRow.tsx:127-134`) is outside every status conditional. |
| FR-4.6 same backend operation | `SelectRepositoryPage.tsx:34` uses the same `useFinishReview()` as `ReviewPage`; `useReviews.ts:72-81` is unchanged. |
| FR-5.6 create already invalidates | `useReviews.ts:66-68` `useCreateReview.onSettled` — pre-existing, unchanged, as the plan states. |
| FR-6.3 responsive/truncating | `ResumeReviewRow.tsx:69-70` `truncate`; `:111` `flex-wrap` on the confirm row. |
| NFR-2 no new dependency | `package.json` diff empty. |
| NFR-6 no new logging | No `console.*` added; failure path is `toast.error` only (`SelectRepositoryPage.tsx:40`). |
| Design §6 backend unchanged | `apps/backend/` diff empty. |

## Controller Rulings — Assessment

**Ruling 1 — `autoFocus` moved from Discard-confirm to Cancel (`ResumeReviewRow.tsx:118`): SOUND.**
The binding artefacts (`prd.md`, `design.md`, `ux-flow.md`) mandate only that the
confirmation be inline and two-step; none names the focus target, so the plan's choice
carried no spec authority. Autofocusing the destructive control makes Enter-Enter
destroy a session, which defeats the stated purpose of the two-step gate. Focus still
enters the confirm region on exactly one button, so AT users are still moved there. The
implementation matches the ruling and carries an explanatory comment (:115-117).

*One gap:* the ledger states Task 5 was dispatched so "its tests assert Cancel holds
focus". No such assertion exists — `grep -i focus` over
`__tests__/ResumeReviewList.test.tsx` returns nothing. The ruling is correct but
unprotected against regression, and the ledger overstates the coverage.

**Ruling 2 — `pendingId?: string` widened to `pendingId?: string | undefined`
(`ResumeReviewList.tsx:16`): SOUND.** Under this repo's `exactOptionalPropertyTypes`,
the plan's own page code (`pendingId={isPending ? variables : undefined}`) cannot
typecheck against the narrow form. The alternatives — conditional prop spreading, or
restructuring the page — are more invasive for zero behavioural gain. The change is
type-only, one line, and does not weaken any runtime contract: `pendingId === review.id`
(:60) behaves identically for `undefined`.

**Ruling 3 — accepting one `included` grep hit: SOUND.** The binding constraint is that
change numbers come from `attributes.changes` and that `included` is not read by the
feature. Re-verified in this audit: `grep -rn "included"` over
`apps/frontend/src/components/features/reviews/` and `SelectRepositoryPage.tsx` returns
exactly one hit, `__tests__/ResumeReviewList.test.tsx:21` — `included: []` in a fixture,
where `ReviewAttributes.included` is a required field. The substance of the constraint
holds; the plan's expected grep output simply failed to account for fixtures. Mangling a
required model field to satisfy a proxy check would be strictly worse.

## Deferred Minors — Merge Triage

None is merge-blocking. Ranked by whether it is worth fixing before the PR:

**Worth fixing now (cheap, and they touch a requirement the plan itself tracks):**

1. `ResumeReviewList.tsx:72-74` — the `<section>` has no `aria-labelledby` pointing at
   its `<h2>`, so it exposes no landmark role and screen-reader landmark navigation
   cannot find it. NFR-4 is an explicit traceability row; this is the clearest shortfall
   against it. Two-line fix (`id` on the `h2`, `aria-labelledby` on the `section`).
2. `ResumeReviewList.tsx:76` — the count `<span>` renders a bare number with no
   accessible label; a screen reader announces "2" with no context. Same NFR-4 bucket.
3. No test asserts Cancel holds initial focus (see Ruling 1). A regression to the plan's
   original `autoFocus` placement would pass the whole suite silently.

**Accept as-is:**

4. `apps/frontend/.gitignore` `+.vitest` — one line, correct, and the directory is a real
   Vitest artefact. Out of brief scope, but reverting costs more attention than it saves.
5. `relativeTime.ts:34-37` — `Date.parse` is lenient about non-strict-ISO input. Every
   caller passes a JSON:API timestamp produced by the Go backend; a stricter parser would
   add a regex for no reachable behaviour change.
6. `useReviews.ts:37-40` duplicating `useReview()`'s predicate — two call sites is not a
   pattern. The ledger's own trigger (extract on the third) is the right threshold.
7. `useReviews.test.tsx:229-283` real wall-clock timers — matches the pre-existing
   convention of the file (`useReview`'s polling test at :45 does the same). Converting
   only the new tests to fake timers would make the file inconsistent; converting the
   whole file is a separate change.
8. `ResumeReviewRow.tsx:100-102` `{...(cond ? {role:"status"} : {})}` — readability only.
   `role={cond ? "status" : undefined}` is equivalent and clearer, but under
   `exactOptionalPropertyTypes` the spread form is the safer idiom, so this is a wash.
9. `ResumeReviewRow.tsx:130,139` empty-`changes` aria-label — a review with zero changes
   cannot be created by the backend; guarding it is speculative.
10. `ResumeReviewList.tsx:29` inline `messageFor()` fallback — matches five existing call
    sites including `SelectRepositoryPage.tsx:72,89`. Centralising one of six is worse
    than leaving all six consistent; do it as a sweep or not at all.
11. `ResumeReviewList.test.tsx:132-136` loading test asserts only absences, and
    `:174-181` pending test asserts only Resume is disabled — both are verbatim from the
    plan and both are covered in substance by neighbouring tests.
12. `SelectRepositoryPage.test.tsx:280-301` discard-failure test does not assert a toast
    rendered — verbatim from the plan; the production path (`SelectRepositoryPage.tsx:40`)
    does call `toast.error`. A toast assertion would need the `Toaster` mounted in the
    test harness, which is a harness change beyond this task.

## Build & Test Results

Not re-run in this audit, per the brief. Recorded in
`.superpowers/sdd/plan/task-7-report.md`:

| Area | Lint | Tests | Build | Notes |
|---|---|---|---|---|
| `apps/frontend` | PASS (`npm run lint`, `format:check`) | PASS (134/134) | PASS | — |
| `apps/backend` | PASS (`go vet`, `golangci-lint` 0 issues) | PASS (`-race -count=1`) | PASS (`CGO_ENABLED=0`) | No Go file changed; run to prove it. |
| root | PASS (`make lint`) | PASS (`make test`, `make test-integration`) | PASS (`make build`, `make docker-build`) | — |

Working tree is clean at audit time (`git status --porcelain` → empty).

## Overall Assessment

- **Plan Adherence:** FULL
- **Recommendation:** READY_TO_MERGE (after the two accessibility one-liners, if the
  author wants NFR-4 fully clean before the PR)

## Action Items

1. Add `id` to the `<h2>` and `aria-labelledby` to the `<section>` in
   `ResumeReviewList.tsx:72-74` so the section exposes a landmark role (NFR-4).
2. Give the count `<span>` at `ResumeReviewList.tsx:76` an accessible label (NFR-4).
3. Add one assertion that Cancel receives initial focus when the confirmation opens, so
   Ruling 1 is regression-protected.
4. Tick the 40 checkboxes in `plan.md` (or note in the file that the ledger is
   authoritative) before the PR, so the committed plan does not read as unstarted.
5. Re-run the frontend gate (`npm run lint && npm run format:check && npm test`) after
   items 1–3; no other gate is affected.

---

# Frontend Guidelines Audit

- **Audit Scope:** all TypeScript/React files changed on `task-003-resume-active-reviews` (merge-base `2fc32e4` → head `5c61c8b`)
- **Guidelines Source:** `frontend-dev-guidelines` skill (`.claude/skills/frontend-dev-guidelines/`)
- **Date:** 2026-09-10
- **Build:** PASS (not re-run — the controller's brief records `npm run build`, `npm run lint`, `format:check` and the full root gate as green; no re-verification performed)
- **Tests:** 134 passed, 0 failed (as reported by the controller; not re-run)
- **Overall:** NEEDS-WORK (no FE-* checklist item fails; four accessibility/React-Query findings below are graded Important and one Critical-adjacent keyboard defect is recommended before merge)

## Build & Test Results

Not re-executed per the review brief. Recorded verbatim from the brief: frontend
`npm run lint`, `npm run format:check`, `npm test` (134/134), `npm run build`; backend
`go vet`, `go test -race`, `golangci-lint`, `go build`; root `make lint`, `make test`,
`make test-integration`, `make build`, `make docker-build` — all green. No finding in
this audit depended on a suite run, so no focused test was executed.

## File Inventory

| File | Classification |
|------|----------------|
| `apps/frontend/src/pages/SelectRepositoryPage.tsx` | Page |
| `apps/frontend/src/components/features/reviews/ResumeReviewList.tsx` | Component (feature, presentational) |
| `apps/frontend/src/components/features/reviews/ResumeReviewRow.tsx` | Component (feature, presentational) |
| `apps/frontend/src/components/features/review/ReviewStatus.tsx` | Component (import-only change) |
| `apps/frontend/src/lib/hooks/api/useReviews.ts` | Hook |
| `apps/frontend/src/lib/relativeTime.ts` | Other (pure lib) |
| `apps/frontend/src/lib/stageLabel.ts` | Other (pure lib, extracted verbatim) |
| `apps/frontend/src/lib/strings.ts` | Other (copy catalogue) |
| `apps/frontend/src/lib/__tests__/relativeTime.test.ts` | Test |
| `apps/frontend/src/components/features/reviews/__tests__/ResumeReviewList.test.tsx` | Test |
| `apps/frontend/src/pages/__tests__/SelectRepositoryPage.test.tsx` | Test |
| `apps/frontend/src/lib/hooks/api/__tests__/useReviews.test.tsx` | Test |

No `services/api/`, `lib/schemas/`, or `types/models/` file changed on this branch.

## Anti-Pattern Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-01 | No `any` type | PASS | `grep -n ': any\|as any'` over all 12 in-scope files: zero matches. Nearest constructs are narrowing assertions, not `any`: `useReviews.ts:38` `query.state.data as Review[] \| undefined` and `ResumeReviewList.test.tsx:113` `(review.attributes as { status: string })`. |
| FE-02 | No manual class concatenation | PASS | Zero `className={"…" +` / template-string concatenations. Every `className` in `ResumeReviewRow.tsx` and `ResumeReviewList.tsx` is a static literal (e.g. `ResumeReviewRow.tsx:66`, `:111`, `ResumeReviewList.tsx:72`). The one conditional class is a ternary yielding a whole literal or `undefined` (`ResumeReviewRow.tsx:101`), which is not concatenation. See FE-02n below for the style note. |
| FE-03 | No direct API client calls in components | PASS | No in-scope file imports `@/lib/api/client`. The page reaches the server only through hooks: `SelectRepositoryPage.tsx:13` imports `useFinishReview, useReviews`; the hook reaches the service at `useReviews.ts:2` (`reviewsService`), which owns `list()` (`services/api/reviews.ts:19`) and `remove()` (`:42`). |
| FE-04 | No inline Zod schemas in components | PASS | Zero `z.object(` / `z.string(` matches; this branch adds no form. |
| FE-05 | No spinners for content loading | PASS | Sole `animate-spin` is `ResumeReviewRow.tsx:142`, inside the Discard `<Button>` and gated on `pending` — the sanctioned submit-button case. Content loading uses `<Skeleton>` at `ResumeReviewList.tsx:39`. |
| FE-06 | No hardcoded colors | PASS | `grep -nE '(bg\|text\|border)-(white\|black\|gray\|slate\|red\|green\|blue\|yellow\|zinc\|neutral)'` over the in-scope files: zero matches. All tokens are semantic — `border-border` (`ResumeReviewRow.tsx:66`), `text-muted-foreground` (`:69`), `text-foreground` (`:70`), `text-destructive` (`:101`). |
| FE-07 | No state mutation | PASS | Zero `.push(` / `.splice(` / `.sort(` / `.reverse(`. `ResumeReviewRow.tsx:51` uses `.map(...).join(...)`; the only `useState` writes are scalar (`:61`, `:118`, `:140`). |
| FE-08 | No default exports for components | PASS | Zero `export default`. Named exports at `ResumeReviewList.tsx:69`, `ResumeReviewRow.tsx:46`, `SelectRepositoryPage.tsx:18`, `relativeTime.ts:43`/`:54`, `stageLabel.ts:2`. |
| FE-09 | Error handling with `createErrorFromUnknown` | PASS (by documented equivalence) | `createErrorFromUnknown` does not exist in this codebase; under the CLAUDE.md "thin `fetch` wrapper" deviation its role is played by `messageFor` (`lib/api/errors.ts:21`, on top of `ApiError`/`isApiError` at `:2`/`:16`). The one `catch` on the branch — `SelectRepositoryPage.tsx:39-41` — is `catch (error: unknown)` → `toast.error(messageFor(error, strings.reviewDiscardFailed))`. The query error path surfaces through `ErrorBanner` with `messageFor` at `ResumeReviewList.tsx:29`. No `console.*` anywhere in scope. |

### FE-02n (style note, non-blocking)

`ResumeReviewRow.tsx:101` writes `className={expiry.nearExpiry ? "font-medium text-destructive" : undefined}`.
This is not the FE-02 anti-pattern (nothing is concatenated), but `patterns-styling.md`
("cn() Utility") states conditional classnames go through `cn()`. Prefer
`className={cn(expiry.nearExpiry && "font-medium text-destructive")}`. Cosmetic.

## Architecture Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-10 | JSON:API model shape | PASS | `types/models/review.ts:56` — `export type Review = Resource<"reviews", ReviewAttributes>`; attributes defined at `:38-54`. No model changed on this branch; the new components consume `review.id` (`ResumeReviewRow.tsx:62`) and `review.attributes` (`:48`) without flattening. |
| FE-11 | Service extends `BaseService` (when applicable) | N/A / PASS | No service file changed. The plain-service-object pattern is an agreed CLAUDE.md deviation; consumption is correctly mediated (`useReviews.ts:2`). |
| FE-12 | Query key factory uses `as const` | PASS | `useReviews.ts:6-13` — `all: ["reviews"] as const`, `lists: () => [...reviewKeys.all, "list"] as const`, and four further hierarchical members each `as const`. The new list query uses `reviewKeys.lists()` (`:35`), matching the invalidation targets at `:67` and `:78`. |
| FE-13 | Forms use `react-hook-form` + `zodResolver` | N/A | No form added or changed. |
| FE-14 | Schema in `lib/schemas/` with inferred type | N/A | No Zod schema added or changed. |

### Architecture findings beyond the table

**A-1 (Important). `useReviews` sets no per-resource `staleTime`; it inherits the 60 s global default.**
`useReviews.ts:33-42` declares only `queryKey`, `queryFn` and `refetchInterval`. The
sibling `useReview` deliberately sets `staleTime: 0` (`useReviews.ts:21`) so a
detail view never renders a stale build; the list gets `staleTime: 60_000` from
`lib/query-client.ts:10`. `patterns-react-query.md` ("Stale Time Guidelines")
places polled, high-frequency data at 30 s–1 min and calls for a per-resource
value rather than the ambient default. The practical consequence: `refetchInterval`
is `false` for an all-settled list (`:39`), so on remounting `/` within 60 s of a
previous visit React Query serves the cached list with no refetch — a session that
expired server-side, or one discarded in another tab, stays on screen. Mutation-driven
staleness is already covered by the `onSettled` invalidations at `:67` and `:78`;
this gap is server-side-only change. Recommend `staleTime: 0` on `useReviews` for
symmetry with `useReview`.

**A-2 (Minor). Unchecked assertion inside the `refetchInterval` callback.**
`useReviews.ts:38` — `const data = query.state.data as Review[] | undefined`. Not an
FE-01 violation (no `any`), and it mirrors the pre-existing shape at `:23`, but it is
an unverified widening: if the queryFn's return type ever drifts, the `.some()` at `:39`
fails silently and polling stops. `query.state.data` is already typed by the
`useQuery` generic here, so the assertion can most likely be deleted outright.

**A-3 (PASS, verified). Conditional polling actually stops.**
`useReviews.ts:37-40` returns `2000` only while some listed review is non-terminal
(`isTerminal` at `types/models/review.ts:66`) and `false` otherwise. Regression-covered
three ways in `__tests__/useReviews.test.tsx`: polls-then-stops (`+229`), no poll for an
all-settled list (`+250`), no poll for an empty list (`+266`) — each asserting a call
count after a 2.5 s real-time wait, so each would fail if `refetchInterval` regressed to
a constant.

**A-4 (PASS, verified). `isLoading` vs `isFetching`.**
`SelectRepositoryPage.tsx:60` passes `reviews.isLoading` (with the reasoning in the
comment at `:58-59`), and `ResumeReviewList.tsx:11-12` documents the prop as first-load
only. A background poll therefore cannot replace rendered rows with skeletons.
No test pins this specific distinction — see T-2.

**A-5 (PASS, verified). Derive-during-render.**
Two derived values, both computed in the render body with no `useEffect`/`useState`
mirror: `pendingId` at `SelectRepositoryPage.tsx:65`
(`discardReview.isPending ? discardReview.variables : undefined`, i.e. the mutation is
the single source of truth) and `showCount` at `ResumeReviewList.tsx:70`. The
pre-existing `providerId` derivation at `:25` follows the same rule.

**A-6 (PASS, verified). Component/page separation.**
`ResumeReviewList` and `ResumeReviewRow` call no data hook — grep for `useQuery`/
`useMutation`/`@/services` in both files returns nothing; every input arrives by prop
(`ResumeReviewList.tsx:9-19`, `ResumeReviewRow.tsx:10-16`). Fetching, navigation and
toast all live in the page (`SelectRepositoryPage.tsx:28-42`, `:66-67`). This matches
`patterns-components.md` → "Presentational Components". The `body(props)` helper
(`ResumeReviewList.tsx:22`, invoked at `:79`) is a plain function call rather than a
rendered component; that is safe here specifically because it contains no hooks, and it
correctly renders exactly one of error → loading → empty → rows.

## Styling Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-15 | Interactive elements show `cursor-pointer` | PASS | Every clickable surface added on this branch is a shadcn `<Button>`: `ResumeReviewRow.tsx:118`, `:121`, `:127`, `:135`, plus `ErrorBanner.tsx:20`. The base CVA already carries `cursor-pointer` (`components/ui/button.tsx:7`). No `onClick` on a `div`, `li`, `span` or table row was introduced — `grep -n 'onClick'` in scope resolves only to those `<Button>` elements. `<li>` at `ResumeReviewRow.tsx:66` is a container, not a click target. |

**S-1 (Minor). Title-case rule, `patterns-components.md` → "Text Casing Rules".**
The rule is scoped to *interactive* text; the branch's interactive strings comply —
`resume: "Resume"`, `discard: "Discard"`, `cancel: "Cancel"`, and the badge labels
`statusReady: "Ready"`, `statusBuilding: "Building"`, `statusFailed: "Failed"`,
`conflict: "Conflict"` (`lib/strings.ts:10`, and the block added at `:12+`). The section
heading `resumeReview: "Resume a review"` (rendered at `ResumeReviewList.tsx:74`) is
sentence case, but headings are not enumerated by the rule and the copy matches the
pre-existing `startNewReview: "Start a new review"`. Not a finding; recorded so a future
reader does not re-litigate it.

## Accessibility Findings

The FE-* table has no dedicated accessibility ID; these are graded against
`testing-guide.md` → Testing Rules #8 ("Verify accessibility — use `getByRole`, check
`aria-label`s") and the per-task review's open questions.

**X-1 (Important — recommend fixing before merge). Confirmed discard drops keyboard focus to `<body>`.**
In `ResumeReviewRow.tsx:58-63`, `confirmDiscard` calls `setConfirming(false)` and then
`onDiscard`. The element the user just activated is the confirm Discard `<Button>`
(`:121`), which lives inside the `confirming` branch and is therefore unmounted by that
same state update. React removes the focused node; browsers reset focus to `<body>`.
On the success path the row unmounts anyway, but on the *failure* path (exercised by
`SelectRepositoryPage.test.tsx`, "keeps the row when the discard fails") the row remains
and the keyboard user is returned to the top of the document with a toast they may never
reach. Compounding it, the row's remaining Discard button is `disabled` while pending
(`:138`) and `disabled:pointer-events-none` (`button.tsx:7`), so it is not focusable to
return to. Fix: move focus back to the row's Discard button after the confirm collapses
(a `ref` + `focus()` in an effect keyed on `confirming`), or keep the confirm affordance
mounted-but-disabled instead of swapping branches.

**X-2 (Important). The discard confirmation prompt is never announced.**
`ResumeReviewRow.tsx:112-114` renders "Discard Review: atlas/server (#12, #14, #15)?
This cannot be undone." as a plain `<span>` with no role, no `aria-live`, and no
association to the buttons. `autoFocus` lands on Cancel (`:118` — the controller's
Ruling 1, which is the right call), so a screen-reader user hears "Cancel, button" and
nothing else: the destructive question itself is silent. The two-step confirm therefore
provides no non-visual warning. Fix: give the `<span>` an `id` and put
`aria-describedby` on both buttons, or promote the confirm container to
`role="alertdialog"` with `aria-label`/`aria-describedby`.

**X-3 (Important). `role="status"` on the near-expiry line is added at the same moment as its content, so the crossing is not announced.**
`ResumeReviewRow.tsx:100-105` applies `role="status"` conditionally via
`{...(expiry.nearExpiry ? { role: "status" } : {})}`. ARIA live regions must be present
in the accessibility tree *before* their contents change to be announced; a region that
is created already populated is generally not spoken. The intended event — a review
crossing the one-hour threshold during a poll — is exactly the case this construction
misses. The unit test at `__tests__/ResumeReviewList.test.tsx:118-123` asserts only that
the role exists on first render, so it passes while the behaviour it stands for does not
occur. Fix: render the `role="status"` container unconditionally and vary only its text
and `className`. (Note the same pattern at `:78`, `aria-live="polite"` on the stage
span, is *correct*: that region mounts with the CREATING row and persists across stage
transitions, so subsequent stage changes are announced.)

**X-4 (Important). The pending/disabled discard state has no accessible announcement.**
`ResumeReviewRow.tsx:127-144`: while a discard is in flight the two buttons flip to
`disabled` and a `Loader2` icon appears (`:142`). The icon has no accessible text, there
is no `aria-busy`, and `disabled` state changes are not announced by screen readers.
A non-visual user gets no signal that their action was accepted until the row disappears
or a toast fires. Fix: add `aria-busy={pending}` (or a visually-hidden "Discarding…"
string) to the pending button, and `aria-hidden="true"` on the decorative `Loader2`.

**X-5 (Minor — carried from the per-task review, confirmed).**
`ResumeReviewList.tsx:72` — the `<section>` has no accessible name, so it exposes no
`region` landmark and cannot be reached by landmark navigation. The heading exists at
`:74`; adding `id` there plus `aria-labelledby` on the `<section>` is a two-token fix.
Separately, `:76` renders the count as a bare `<span>{props.reviews.length}</span>` —
announced as a naked "2" with no context. Suggest `aria-label={`${n} reviews in
progress`}` or visually-hidden text. Neither blocks a FE-* check; both are cheap.

## Testing Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-16 | Tests exist for changed components | PASS | `ResumeReviewList` + `ResumeReviewRow`: `components/features/reviews/__tests__/ResumeReviewList.test.tsx` (15 cases; the Row is exercised through the List, which is the correct seam for a presentational child). `SelectRepositoryPage`: `pages/__tests__/SelectRepositoryPage.test.tsx` gains a 7-case `SelectRepositoryPage resume section` block. `relativeTime`: `lib/__tests__/relativeTime.test.ts` (11 cases). `useReviews`: 3 cases appended to `lib/hooks/api/__tests__/useReviews.test.tsx`. `stageLabel.ts` has no direct test but is a verbatim extraction whose behaviour is pinned through `ResumeReviewList.test.tsx:77` and the pre-existing `ReviewStatus` coverage. |
| FE-17 | Mocks updated when services changed | N/A / PASS | No service interface changed; `reviewsService.list`/`remove` predate the branch (`services/api/reviews.ts:19`, `:42`). Tests drive the real service through MSW handlers (`@/test/server`) rather than module mocks, so there is no mock surface to drift. |

**T-1 (PASS, verified). Query-by-role discipline.**
No `data-testid` or `*ByTestId` anywhere in scope. Interactive assertions go through
accessible names: `getByRole("heading", { name: /resume a review/i })`
(`ResumeReviewList.test.tsx:52`), `getByRole("button", { name: /resume review of
atlas\/server/i })` (`:152`), the anchored `/^cancel$/i` and `/^discard$/i` matchers at
`:161`/`:169` that distinguish the confirm buttons from the row buttons, and
`getByRole("alert")` at `:145` (satisfied by `ErrorBanner.tsx:12`). The names those
queries match are real product attributes, set at `ResumeReviewRow.tsx:130` and `:139`.

**T-2 (Minor gap). Two behaviours carry a code comment but no regression test.**
(a) The `isLoading`-not-`isFetching` decision (`SelectRepositoryPage.tsx:58-60`): the
list test covers loading vs. loaded (`ResumeReviewList.test.tsx:132-141`) but nothing
asserts that rows *survive* a background refetch, so swapping the page to `isFetching`
would still ship green. (b) The pending spinner / `aria-busy` surface (X-4) is untested;
`SelectRepositoryPage.test.tsx` asserts only that the button is re-enabled after failure.

**T-3 (Minor). `expect(screen.getByText("2"))` at `ResumeReviewList.test.tsx:53` is
brittle.** It matches a bare text node "2" anywhere in the subtree; it passes today only
because the fixture's change numbers render as "#12, #14, #15". A future fixture with a
single-digit change number would make it ambiguous. If X-5's `aria-label` is added, this
becomes `getByLabelText(/2 reviews/i)` and the brittleness disappears.

**T-4 (Note, per the brief's Ruling 3).** `included: []` at
`ResumeReviewList.test.tsx:21` (and the parallel fixtures in the page and hook tests) is
required by `ReviewAttributes.included` (`types/models/review.ts:48`), not dead weight.
Confirmed no production file on this branch reads `included`.

## Summary

### Blocking (must fix)

None. Every FE-01 … FE-17 check is PASS or N/A with file:line evidence above.

### Recommended before merge

- **X-1** — `ResumeReviewRow.tsx:58-63` / `:121`: confirmed discard unmounts the focused
  button and drops keyboard focus to `<body>`; on the failure path the user is stranded
  at the top of the document.
- **X-2** — `ResumeReviewRow.tsx:112-114`: the destructive confirmation question is
  never announced, so `autoFocus`-on-Cancel is the only non-visual safeguard.
- **X-3** — `ResumeReviewRow.tsx:100-105`: `role="status"` applied conditionally means
  the near-expiry crossing it exists to announce is the one case it will not announce.
- **X-4** — `ResumeReviewRow.tsx:127-144`: in-flight discard has no `aria-busy` and a
  label-less spinner.

### Non-Blocking (should fix)

- **A-1** — `useReviews.ts:33-42`: no per-resource `staleTime`; inherits 60 s while
  `useReview` deliberately sets `0`.
- **A-2** — `useReviews.ts:38`: unnecessary `as Review[] | undefined` assertion inside
  `refetchInterval`.
- **X-5** — `ResumeReviewList.tsx:72`, `:76`: unnamed `<section>` (no landmark) and an
  unlabelled count.
- **FE-02n** — `ResumeReviewRow.tsx:101`: conditional class should route through `cn()`.
- **T-2 / T-3** — `SelectRepositoryPage.tsx:58-60` untested; `ResumeReviewList.test.tsx:53`
  brittle `getByText("2")`.
