# task-004 execution status

Phase 4 (`/execute-task task-004`) is **COMPLETE**. All 27 plan tasks are
implemented, individually reviewed, and the final whole-branch review plus its
single fix wave have closed. The branch is ready for
`superpowers:finishing-a-development-branch`.

## Final state

- Branch `task-004-review-flow-redesign`, head `e30a7e8`, 47 commits from `main`
  (merge-base `7ae00b9`).
- **All 27 plan tasks complete and individually reviewed.** Session 1: tasks 1-16.
  Session 2: 17-20. Session 3: 21-27. Session 4: final fix wave.
- **All CI gates pass**: `make lint`, `make test`, `make test-integration`,
  `make build`, `make docker-build`.
  - Frontend: 45 files / **407 tests**; lint 0 errors + 2 known pre-existing
    `react-refresh` warnings (`RepositoryResults.tsx`, `ChangeTable.tsx`);
    `format:check` clean; `npx tsc -b --force` clean.
  - Backend: `go vet` clean, `golangci-lint` 0 issues, race 18/18,
    integration 18/18.
- **All 16 PRD §10 acceptance criteria** now confirmed by test evidence.

## Final whole-branch review — CLOSED

Three reviewers ran in parallel over `7ae00b9..fcdd1a8` (findings in `audit.md`):
plan-adherence **27/27 FULL**, backend-guidelines **PASS**, frontend-guidelines
**NEEDS-WORK** (1 Critical, 4 Important, 6 Minor, FE-06 FAIL).

One fix wave (commit `e30a7e8`) closed all 7 triaged findings. The scoped
re-review over `e8f47f1..e30a7e8` returned **PASS** — 7/7 ADDRESSED, 0 new
Critical/Important breakage, working tree verified clean. Full report:
`final-fix-wave-re-review.md`.

What the fix wave changed:

1. **FR-36** — the "Next file" button rendered a full path; now the bare
   filename, via a `baseName` helper shared from `lib/review/fileTree.ts`.
   Pinned by a new regression test using a nested file (path != name).
2. **FE-06** — raw Tailwind palette colours replaced with design tokens. Three
   new tokens (`--success`/`--warning`/`--info`) were added to `src/index.css`
   in both `:root` and `.dark`, registered in `@theme inline` beside
   `--color-destructive`, following the `--destructive` 600-light/400-dark
   convention. The re-reviewer verified they land in the shipped CSS.
3. Corrected a comment that asserted a `useBranches` "closed-state query" which
   does not exist (comment only; the eager request is deliberately retained).
4. Copy bug: "Could not load included prs/mrs" → "Included PRs/MRs".
5. Duplicated `STATUS_LETTER`/`STATUS_COLOR` maps unified into one module.
6. `ReviewPage` discard-error copy now uses `strings.reviewDiscardFailed`.
7. Added tests pinning `NewReviewSheet`'s Cancel-button and scrim close paths.

## Deferred — triaged, deliberately not fixed

- **File tree ARIA**: `role="tree"` at `FileTree.tsx:128` with plain-button
  directory rows and no `role="group"` at `:111` orphans the one correct
  `role="treeitem"` at `FileTreeRow.tsx:46`. Inherited from the plan's sample
  code. The most substantive deferral — worth a follow-up task.
- **`useBranches` duplication** in `SelectChangesPage.tsx` — an unconditional
  eager request. Only the false comment was fixed.
- `providerLink.ts:29` — untested defensive guard; worst case an empty `href`.
- `ReviewRow.tsx:100-101` `pending` — verified inert (`setConfirming(false)`
  runs before `onDiscard`). The row-level `disabled={pending}` at `:82` is live.
- Ticket-key fixture blind spot: `ReviewStatusLine`'s fixture never has a second
  included change carrying the ticket key.
- From the re-review: `ReviewRow`'s `bg-success`/`bg-warning` and
  `FileTreeRow`'s `STATUS_COLOR` remain unasserted by any test (pre-existing
  coverage shape, not worsened); and `strings.ts:90` embeds "Included PRs/MRs"
  independently of `strings.includedChanges:9`, so the two can drift.
- **FE-04/13/14 are vacuous**: `lib/schemas/` was deleted and zero `zod` /
  `react-hook-form` imports remain on the branch. Confirm that is intended.

## Rulings

35 rulings were made across the four sessions. They are preserved in
`rulings.md` — the ledger that held them is git-ignored scratch and has been
deleted. The load-bearing ones:

- **Ruling 16/25** — `BaseBranchSelect` has no error affordance, so the
  branches-fetch-failure banner belongs to the create page; and Task 23's
  hardcoded "Ready" badge is correct only because Task 26 gates
  `ReviewStatusLine` behind an exhaustive `switch` with `assertUnreachable`.
  Both verified to hold (Task 26's reviewer proved exhaustiveness by adding a
  seventh `ReviewStatus` member and getting `TS2345`).
- **Ruling 22/23** — the plan's test steps deleted coverage of live behaviour;
  a brief's authority covers what to build, not permission to drop coverage.
  Backfilled.
- **Ruling 29/32** — Important findings enter the fix loop even when a reviewer
  calls them non-blocking.
- **Final-wave ruling** — accepted the fix wave's deviation from its brief on
  FR-36 (a shared `baseName` helper rather than an inlined
  `split("/").pop()`); the re-reviewer confirmed the moved body is
  byte-identical and both callers intact.

## Repo facts discovered during execution — these outlive this task

1. **`npx tsc --noEmit` is a NO-OP in this repo.** `apps/frontend/tsconfig.json`
   has `files: []` and only project references, so it exits 0 without checking
   anything. **The real type check is `npx tsc -b --force`.** Every "tsc clean"
   claim across tasks 1-26 was vacuous; the branch does pass the real check.
   Worth fixing in the repo's own docs and CI.
2. **Vitest's default `threads` pool can hang indefinitely in this sandbox** —
   pass `--pool=forks`. Not in the repo's vitest config.
3. **`npm run build` deletes the tracked `apps/backend/internal/ui/dist/.gitkeep`.**
   Restore with `git checkout -- apps/backend/internal/ui/dist/.gitkeep`.
4. **`format:check` had dropped out of the implementer and reviewer briefs**, and
   Task 23 shipped an unformatted file that would have failed CI. Keep it in
   every dispatch's gate list.
5. **`messageFor(error, fallback)` prefers the server message**, so a JSON:API
   500 never reaches the fallback copy. Tests asserting fallback copy must use a
   transport-level error (`HttpResponse.error()`).

## Process lessons from this plan

- **Only a performed mutation is evidence.** Reviewers on this plan were wrong in
  *both* directions — one under-called a real finding, another wrongly cleared a
  genuinely uncovered gap. Task 22 lost two fix rounds to a coverage claim made
  without performing the mutation. In the final wave, a first scrim mutation
  *survived* because Radix binds dismissal at document level; only a second,
  better-targeted mutation pinned it.
- **The signature defect of this plan is a fixture whose two distinct inputs
  share a value**, or one too small to exercise an ordering, depth, or index bug.
  It appeared in nine separate tasks, including FR-36 — where the only footer
  tests used root-level files with path == name, leaving the bug unpinned in
  both directions.
- **The plan's briefs repeatedly contradicted themselves**, and on FR-36 the
  plan contradicted the PRD it implements (`plan.md:8440` specifies
  `nextName={nextPath}` verbatim). When that happens the more specific
  expression of intent wins; record a ruling.
- **A brief that rewrites an existing test file may silently delete cases.**
  Always diff old against new and preserve coverage of behaviour that still
  exists.
