# task-004 execution status

Phase 4 (`/execute-task task-004`) is partially complete. This file exists so a
fresh session can resume without re-deriving anything.

## Resume instructions

1. `cd` into this worktree (`.worktrees/task-004-review-flow-redesign`).
2. `/clear`, then re-run `/execute-task task-004`.
3. The SDD skill will find the ledger at `.superpowers/sdd/plan/progress.md`
   (git-ignored). It is the authority on what is done.
4. **Resume by finishing Task 17 and Task 18 — see "Resume exactly here" below.**
   Do not re-dispatch tasks 1-16.

## Done and reviewed

| Task | Commits | Outcome |
|---|---|---|
| 1 — `provider.Branch` + `BranchBuilder` | `133a643` | review clean |
| 2 — `provider.FilterWalk` | `a7d5688` | review clean |
| 3 — repository search through the provider layer | `82a49fe` | review clean |
| 4 — `ListBranches` across the provider layer | `0ff967e` | review clean |
| 5 — `INVALID_SEARCH` validation | `8cc884a`, `103ed48` | clean after 1 fix round |
| 6 — branches endpoint | `e3780d0`, `103ed48` | clean after 1 fix round |
| 7 — wider per-file diff context (`-U40`) | `72d4e0c` | review clean |
| 8 — shadcn primitives install | `80620fd`, `15bc77c` | clean after 1 fix round |
| 9 — `lib/storage/store.ts` | `3440a7f` | review clean |
| 10 — recents / changeFilters / viewed | `c4b8f99` | review clean |
| 11 — change derivations | `13688e4`, `f81a98b` | clean after 1 fix round |
| 12 — file tree, progress, provider links | `f382410` | review clean |
| 13 — keyboard shortcuts | `8b3249e`, `0603b36` | clean after 1 fix round |
| 14 — repository input, debounce, time-left | `f1cb6d0`, `0603b36` | clean after 1 fix round |
| 15 — branches service + hooks | `162d724` | review clean |
| 16 — app shell, breadcrumbs, container | `dc5b767` | review clean |

Phase A (backend, 1-7) and all frontend foundations (8-16) are complete.
Backend and frontend gates were green at each commit.

## Resume exactly here

**Task 17 — implemented (`9827286`), reviewed, ONE Important finding OPEN.**

Fix round 1 was composed but never dispatched (the controller hit the context
handoff threshold). Nothing is in flight; no partial work exists in the tree.

The finding: `ReviewsTable.tsx` renders `Table > TableBody` with no
`TableHeader`/`TableHead` at all — every cell is a bare `<td>` with no
associated `<th>`, so screen-reader users get no column semantics. Inherited
from the brief's own Step 6 sample.

The fix, per Ruling 14 below: add a `TableHeader` of `TableHead` cells styled
`sr-only` (not `display:none`, not `aria-hidden`). One header per column in the
exact order the body cells already use — read `ReviewRow.tsx`,
`NewReviewRow.tsx`, `ReviewProgressCell.tsx` to get the real order; the count
must match the existing `colSpan={5}`. Every label from `src/lib/strings.ts`.
The actions column needs a header too. Add a test asserting
`getAllByRole("columnheader")` returns the expected count and accessible names —
`sr-only` is in the accessibility tree, which is why the ruling specifies it.
Run `src/pages` tests too; Task 18's tests render this table.

**Task 18 — implemented (`611f3a9`), NOT yet reviewed.**

The review was composed but never dispatched (same threshold). Dispatch it over
`9827286..611f3a9`; the review package is already written to
`.superpowers/sdd/plan/review-9827286..611f3a9.diff`.

Two items are already known and ruled on — tell the reviewer they are queued, so
it does not re-raise them, but ask it to check for other tests of the same shape:

1. **Silent no-op (Ruling 15, fix scheduled).** When the client validator
   rejects a name, `resolveTyped` silently no-ops — no error, no toast, nothing.
   Must be fixed in Task 18's fix round: surface a `strings.ts`-routed inline
   error when `parseRepositoryInput` returns null, matching the existing
   `strings.repositoryNotFound` treatment. Do not change the validator.
2. **Breadcrumb test gap.** The breadcrumb-publish mutation was NOT caught:
   `Breadcrumbs.tsx`'s default renders "Reviews" when nothing is published, so
   the brief's verbatim test cannot distinguish `ReviewsPage` publishing from the
   shell's fallback. The test passes either way. Strengthen it in the fix round.

## Remaining after that

Tasks 19-27: create page (19-22), review page (23-26), verification sweep (27).
Briefs for all of 1-27 already exist in `.superpowers/sdd/plan/`.

## Operational finding — carry this into every dispatch

**Vitest's default `threads` pool hangs indefinitely in this sandbox.
Always pass `--pool=forks`.** This is not in the repo's vitest config. It is
almost certainly what stalled the first Tasks 15-16 implementer, which ended its
turn waiting on a backgrounded command and lost its Task 16 work-in-progress
(recovered from the tree by a continuation). Every implementer dispatch must
carry `--pool=forks` plus a foreground-only, explicit-timeout instruction.

Consider setting `pool: 'forks'` in the frontend vitest config — a project-level
change outside this task's scope, so it was not made.

## Rulings made so far

Full text with rationale and cost-if-wrong lives in the ledger. In short:

1. **Task 14 does not delete `lib/schemas/repository.ts`** — Task 18 owns that
   deletion. (Task 18 has now done it, in `611f3a9`.)
2. **`config.NewSecret(...)`, not `config.Secret(...)`** in the plan's Go test
   snippets — `config.Secret` is a struct, not a constructor.
3. **Task 4's GitHub `fetch` closure signature** in the plan cannot be passed to
   `FilterWalk`; the two-closure form matching `ListRepositories` was used.
4. **`internal/api/changes.go` was brought under `searchFrom`**, though no task
   lists that file, because the Global Constraint is unconditional.
5. **The `cn@^0.2.6` dependency the shadcn installer added was removed**; the 13
   generated components import `cn` from `@/lib/utils` like the 8 pre-existing.
6. **`createStore` re-reads the raw stored string on each `get()`** instead of
   caching forever. The plan's Task 9 sample fails the plan's own Task 10 tests
   (4 of 29 assertions, measured) because Task 10 uses singleton stores and
   clears `localStorage` directly. Public interface unchanged.
7. **`applyOrder` compares merge instants for inequality first.** The plan's
   sample fails its own "sorts a null merge time last" test: it gated a delta on
   `Number.isFinite`, so a null (mapped to Infinity) fell through to the number
   tie-break, and two nulls produced `NaN`. Verified consistent and transitive.
8. **`NO_TICKET_LABEL` sources its value from `strings.ts`.** It is rendered UI
   copy; the constraint routing copy through `strings.ts` is unconditional.
9. **`useHotkeys` syncs its handler ref in an effect, not during render.** The
   plan's sample violates the repo's `react-hooks/refs` lint rule and so cannot
   pass a required gate.
10. **`isValidRepositoryName` rejects any segment starting with `.` or `-`.**
    The plan's sample fails its own `atlas/.hidden` test.
11. **`timeLeft`'s three formats moved into `strings.ts`** — same as Ruling 8.
12. **The stricter-than-backend repository rule stands; the comment was
    corrected.** See "Decision you should make" below — this one is parked for
    you, not settled.
13. **The breadcrumb context was split** into a stable setter context plus a
    segments context. The plan's single-context design causes an infinite
    re-render loop reachable by the brief's own `Publisher` test — measured: hung
    indefinitely before, 6/6 passing in 1.4s after. Public `useBreadcrumbs` /
    `BreadcrumbSegment` contract unchanged, so Tasks 22 and 26 need no changes.
14. **`ReviewsTable` gets `sr-only` column headers.** `design.md` §4.4 specifies
    no header row either way, so this is a genuine gap, not a headerless design.
15. **The drawer's silent no-op on a rejected name is a defect and will be
    fixed**, independently of Ruling 12. Queued for Task 18's fix round.

## Decision you should make (Ruling 12 — parked for the user)

`isValidRepositoryName` is **stricter than the backend it serves**. The backend
(`gitx.ValidateRepoFullName`, `validate.go:36-42`) rejects only a leading `/`,
`-`, or `.` on the whole string plus segments that are exactly `.`/`..` or
contain `..`. The client rejects **any** segment starting with `.` or `-`.

Consequence: **`org/.github` — a real and common GitHub repository the backend
accepts — cannot be entered by any input path in the new drawer.** Both the
bare-name branch (`repositoryInput.ts:25`) and the URL-paste branch (`:41`) call
the same validator, so pasting the full URL fails too.

The behaviour stands for now because a plan-supplied test asserts
`atlas/.hidden` must reject, and overriding a plan-supplied test mid-execution
is your call, not the controller's. Reversing it is cheap: one predicate in
`repositoryInput.ts` and one test line.

Ruling 15 fixes the *silent* half regardless — after that fix, a rejected name
at least produces a visible error instead of nothing.

No other findings are parked; no breaker has tripped.
