# Resume Active Reviews — Execution Context

Companion to `plan.md`. Everything an implementer needs to know about the surrounding
codebase that the plan's tasks assume but do not restate.

---

## 1. Where you are

- Worktree: `.worktrees/task-003-resume-active-reviews/`, branch `task-003-resume-active-reviews`.
- All work is under `apps/frontend/`. **No Go file is touched.**
- Frontend commands run with cwd = `apps/frontend`. If `npm` is not on `PATH`:
  `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22`.

---

## 2. Files you will read (existing, unchanged by this task)

| Path | Why it matters |
|---|---|
| `src/types/models/review.ts` | `Review`, `ReviewAttributes`, `ReviewStatus`, `Totals`, `ReviewErrorPayload`, `isTerminal`. No new field is needed. |
| `src/services/api/reviews.ts` | `reviewsService.list()` → `Review[]` and `.remove(id)` → `void`. Already written; this task is their first consumer for `list()`. |
| `src/lib/hooks/api/useReviews.ts` | `reviewKeys`, `useReview` (the polling shape to copy), `useReviews`, `useFinishReview` (already invalidates `reviewKeys.lists()` in `onSettled`). |
| `src/pages/ReviewPage.tsx` | `closeReview()` is the model for the discard handler. Its status switch is what makes Resume meaningful for every active status. |
| `src/components/features/review/ReviewHeader.tsx` | The totals presentation to match: `{n} files`, `+{a}`, `−{d}` with U+2212. |
| `src/components/features/review/ReviewStatus.tsx` | Source of `stageLabel`, extracted in Task 2. |
| `src/components/common/EmptyState.tsx` | `{ title, description?, icon? }`. |
| `src/components/common/ErrorBanner.tsx` | `{ title, detail?, onRetry? }`; renders `role="alert"` and a "Try again" button. |
| `src/pages/SelectRepositoryPage.tsx` | The page being modified; note its "derive during render, never setState-in-effect" comment style. |
| `src/lib/api/errors.ts` | `messageFor(value, fallback)` — returns `value.detail` for an `ApiError`, else the fallback. |
| `src/lib/strings.ts` | All copy. `conflict` and `discardReview` already exist and are reused. |
| `src/test/server.ts` | `server`, `http`, `HttpResponse`, `oneDoc`, `listDoc`, `errorDoc`. |
| `src/test/render.tsx` | `renderWithProviders(ui, { route })` and `queryWrapper({ gcTime })`. |

---

## 3. Decisions already made — do not relitigate

These were settled in `design.md` §7 after being raised as open questions in the PRD.
Changing them is a design change, not an implementation choice.

| Decision | Resolution |
|---|---|
| Resume on a `CREATING` session | **Enabled**, for all four active statuses. `ReviewPage` renders a live progress view for `CREATING` and an error panel for `CONFLICTED`/`FAILED`. |
| Confirmation mechanism | **Inline two-step inside the row.** No `AlertDialog`, no modal, no portal, no focus trap. |
| Row density at scale | **No cap, no "show all" toggle** in v1. Sessions are self-limiting by `expiresAt`. |
| Change numbers | **Always `attributes.changes`.** `included` is not read by this feature at all. |
| Discard of a `CREATING` session | **Safe, no client guard.** Verified in backend source — see §5. |
| Table vs list | **`<ul>`/`<li>` cards**, not `<Table>`. The row is a three-line block with a variable detail line. |
| Layout element | `<section>` + `<h2>` inside `SelectRepositoryPage`'s existing flex column. |

---

## 4. Patterns this codebase expects

- **Data in the page, presentation in the feature component.** `SelectRepositoryPage` +
  `RepositoryList` is the reference split. Feature components take everything as props
  and own no queries.
- **Derive during render; never `setState` in an effect.** Both `SelectRepositoryPage`
  and `ReviewPage` carry comments explaining this. `pendingId` follows the same rule —
  it is derived from the mutation's own `variables`, not mirrored into state.
- **`isLoading`, not `isFetching`, drives skeletons.** TanStack Query v5 reports
  `isLoading: false` with `isFetching: true` for a background refetch, which is exactly
  what keeps rendered rows on screen during a poll.
- **Comments explain *why*, not *what*.** The existing files use multi-line comments to
  record the reasoning behind a non-obvious choice (the `!providerId` term in
  `RepositoryList`'s `loading`, the `assertUnreachable` in `ReviewPage`). Match that
  density: the plan's inline comments are part of the deliverable, not decoration.
- **Exhaustive switches are for layout choices only.** `ReviewPage` uses
  `assertUnreachable` because a new status without a layout is a real bug. The resume row
  deliberately does the opposite and treats status as data — this is called out in the
  code comment so a reviewer does not "fix" it.
- **Toasts via `sonner`**, message via `messageFor(error, fallback)`. `<Toaster />` is
  already mounted in `App.tsx`; no setup needed.
- **Tests are Vitest + Testing Library + MSW.** Queries by role and accessible name,
  never by test id.

---

## 5. Backend facts, verified in source

No backend change is permitted, and none is needed. These were read during design:

| Guarantee | Evidence |
|---|---|
| Only `CREATING`/`READY`/`CONFLICTED`/`FAILED` are listed | `Store.List()` filters on `Session.IsActive()` — `apps/backend/internal/session/store.go:200`, `model.go:138` |
| Newest-first ordering | `sort.Slice` on `CreatedAt().After(...)` — `session/store.go:209` |
| Empty result is `{"data": []}`, never `null` | `listReviews` allocates `make([]jsonapi.Resource, 0, …)` — `api/reviews.go:104` |
| No pagination meta | `jsonapi.WriteList(w, 200, out, nil)` — `api/reviews.go:109` |
| `DELETE` is idempotent | `Store.Finish` returns nil for an unknown or already-finished id — `session/store.go:216` |
| Discarding a building session is safe | `Store.Finish` writes the terminal status before `Cleanup`; `Store.SaveActive` (`store.go:115`) then refuses the in-flight build's writes with `ErrTerminal`, which `review/service.go:328,:370,:522` handle explicitly — a refused write never fails the build (`service.go:319`) |

---

## 6. Dependency order between tasks

```
Task 1 (relativeTime + strings.expired) ─┐
Task 2 (stageLabel extraction) ──────────┼─→ Task 4 (ResumeReviewRow) ─→ Task 5 (ResumeReviewList) ─┐
                                          │                                                          ├─→ Task 6 (page wiring) ─→ Task 7 (verification) ─→ Task 8 (review)
Task 3 (useReviews polling) ──────────────┴──────────────────────────────────────────────────────────┘
```

Tasks 1, 2 and 3 are mutually independent and can be done in any order. Task 4 needs 1
and 2. Task 5 needs 4. Task 6 needs 3 and 5.

Task 4 adds every new `strings` key at once (including those Tasks 5 and 6 consume) so
`strings.ts` is edited in one place rather than three.

---

## 7. Traps

- **`expiryLabel` returns a fragment, not a sentence.** It gives `"in 5 hours"` for a
  future timestamp but the complete word `"expired"` for a past one. The row must branch
  before prefixing `"expires "`, or a swept session reads "expires expired".
- **Testing Library's `getByText` only matches an element's *direct* text nodes.** That
  is why the plan's assertions on nested spans work and do not hit "found multiple
  elements". If you restructure the row's JSX, re-check those assertions.
- **The existing `SelectRepositoryPage` tests use `onUnhandledRequest: "bypass"`.** An
  unstubbed `GET /api/reviews` will not fail them, it will silently do something. Task 6
  seeds a reviews handler in every test in that file for this reason.
- **`npx tsc -b --noEmit` is rejected in build mode** on this TypeScript version. Use
  `npx tsc -b`, or just `npm run build`.
- **Prettier `printWidth` is 100.** The plan's code blocks are formatted for it, but run
  `npm run format` before `npm run format:check` rather than hand-wrapping.
- **`grep -rn "function stageLabel" src/` must return exactly one hit** after Task 2. Two
  hits means the extraction left a copy behind and the acceptance criterion fails.
- **Do not add a ticking timer to refresh relative text.** Design §3.7 accepts that
  "started 12m ago" goes stale on an idle page; a render loop on an idle page contradicts
  NFR-1.

---

## 8. Definition of done

`prd.md` §10 in full, plus:

- `git diff main --stat -- apps/backend/` is empty.
- `git diff main -- apps/frontend/package.json` is empty.
- `stageLabel` defined once, in `src/lib/stageLabel.ts`.
- Frontend: `npm run lint`, `npm run format:check`, `npm test`, `npm run build` all clean.
- Backend suite run to prove it is unaffected: `go vet ./...`,
  `go test -race -count=1 ./...`, `go tool golangci-lint run`, `CGO_ENABLED=0 go build ./...`.
- Root: `make lint`, `make test`, `make test-integration`, `make build`, `make docker-build`.
- `superpowers:requesting-code-review` run (plan-adherence + frontend guidelines),
  findings in `audit.md`, addressed before a PR is opened.
