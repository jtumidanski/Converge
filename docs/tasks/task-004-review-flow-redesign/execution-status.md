# task-004 execution status

Phase 4 (`/execute-task task-004`) is **complete through all 27 plan tasks**. What
remains is the final review triage and branch finishing.

## Resume instructions

1. `cd` into this worktree (`.worktrees/task-004-review-flow-redesign`).
2. `/clear`, then re-run `/execute-task task-004`.
3. The ledger at `.superpowers/sdd/plan/progress.md` (git-ignored) is the authority
   on what is done. Its last lines record the final-review dispatch.
4. **Do not re-dispatch any of tasks 1-27.** They are complete and reviewed.

## State at handoff

- Branch `task-004-review-flow-redesign`, head `fcdd1a8`, 46 commits from `main`
  (merge-base `7ae00b9`). Working tree clean (untracked `node_modules/` expected).
- **All 27 plan tasks complete and individually reviewed.**
  - Session 1: tasks 1-16. Session 2: tasks 17-20. Session 3: tasks 21-27.
- **All CI gates pass** (Task 27 sweep): `make lint`, `make test`,
  `make test-integration`, `make build`, `make docker-build` — Docker was available
  and the image built and imported.
  - Frontend: 45 files / 402 tests; lint 0 errors + 2 known pre-existing
    `react-refresh` warnings (`RepositoryResults.tsx`, `ChangeTable.tsx`);
    `format:check` clean; `npx tsc -b --force` clean.
  - Backend: `go vet` clean, `golangci-lint` 0 issues, build clean, race tests
    18/18, integration 18/18 — confirmed non-vacuous.
- 15 of 16 PRD §10 acceptance criteria confirmed by test evidence.

## Final whole-branch review — COMPLETE. Verdict: NEEDS-WORK.

Three reviewers ran in parallel over `7ae00b9..fcdd1a8`; full findings are in
`audit.md`. Working tree verified clean afterwards — all reviewer mutations
restored, `git diff -- apps/` empty.

- `plan-adherence-reviewer` (opus) — **adherence 27/27, FULL.** Nothing skipped.
  All 34 rulings independently judged **justified**. Verdict NEEDS_FIXES on one
  PRD non-conformance.
- `frontend-guidelines-reviewer` (opus) — **NEEDS-WORK.** 1 Critical, 4 Important,
  6 Minor, FE-06 FAIL.
- `backend-guidelines-reviewer` (sonnet) — **PASS.** 0 Critical, 0 Important.

## THE FIX WAVE — do this next, as ONE dispatch plus one scoped re-review

Per `superpowers:subagent-driven-development`: one fix subagent with the complete
list, then exactly one scoped re-review. **There is no second fix wave.**

### MUST FIX — blocks merge

1. **FR-36 violation: the "Next file" button shows a full path, not a filename.**
   Found independently by *both* opus reviewers (frontend CRIT-1, adherence
   MUST-1). `ReviewPage.tsx:220` passes `nextName={nextPath}` — a full path from
   `flattenVisible` (`lib/review/fileTree.ts:77`) — into `FileFooter.tsx:20-21`,
   which interpolates it raw. The button reads
   `Next file: src/main/java/com/atlas/App.java` instead of the PRD's `<name>`.
   `FileTreeRow.tsx:20-23` already derives a basename for the same concept, so the
   branch contradicts itself.
   - **Root cause is the plan itself**: `plan.md:8440` specifies
     `nextName={nextPath}` verbatim. The plan contradicts the PRD it implements.
   - **The behaviour is unpinned in BOTH directions** — applying the correct fix
     (`nextPath?.split("/").pop()`) *also* leaves 402/402 green. A regression test
     is required alongside the one-line fix, or nothing holds it.
   - Ninth occurrence of this plan's signature fixture defect: the only footer
     tests use root-level files where path == name
     (`DiffPane.test.tsx:41,93`), and `ReviewPage.test.tsx` never asserts the
     footer.

2. **FE-06 FAIL — raw Tailwind palette colours instead of design tokens.**
   `ReviewRow.tsx:12-13` (`bg-green-500`, `bg-amber-500`),
   `FileTreeRow.tsx:14,15,17`, `FileHeader.tsx:16,17,19`
   (`text-amber-500/green-500/blue-500`). None are tokens in `src/index.css:10-41`.
   Sibling entries in the *same* maps already use `bg-destructive`/
   `text-destructive`, so the maps are half-converted. This is a checklist failure
   the reviewers grade against.

### SHOULD FIX — fold into the same pass, all cheap

3. **A code comment asserts behaviour that does not exist.**
   `SelectChangesPage.tsx:86-88` justifies its duplicate `useBranches` query by
   citing `BaseBranchSelect`'s "closed-state query" — but
   `BaseBranchSelect.tsx:47-52` passes `open` as `enabled` and has no such query.
   Keys match, so it is an unconditional eager request, not a double fetch.
   **Defer the dedup; fix the false comment.**
4. **Copy bug:** `SelectChangesPage.tsx:259` renders **"Could not load included
   prs/mrs"** — `.toLowerCase()` applied to `"Included PRs/MRs"`.
5. **Duplicated maps:** byte-identical `STATUS_LETTER`/`STATUS_COLOR` in
   `FileTreeRow.tsx:6-18` and `FileHeader.tsx:8-19`. Share them.
6. **Inconsistent error copy:** `ReviewPage.tsx:126` hardcodes
   `"The review could not be closed."` for the same `useFinishReview()` whose
   sibling at `ReviewsPage.tsx:49` uses `strings.reviewDiscardFailed`.
7. **Two-line test:** the drawer's Cancel button (`NewReviewSheet.tsx:166`) and
   scrim (`:120`) — the one unmet PRD §10 criterion. Both route through the same
   `close()` as Escape, which *is* tested, so only the button wiring is unpinned.

### FINE TO DEFER — triaged, do not fix now

- **File tree ARIA (upgraded to Important by the FE reviewer, still deferrable):**
  `role="tree"` at `FileTree.tsx:128` with plain-button directory rows and no
  `role="group"` at `:111` orphans the one correct `role="treeitem"` at
  `FileTreeRow.tsx:46`. Inherited verbatim from the plan's sample code.
- **`useBranches` duplication itself** (see #3 — fix only the comment).
- **`providerLink.ts:29`** — untested defensive guard on a fallback whose result
  is asserted; worst case an empty `href`.
- **`ReviewRow.tsx:100-101` `pending`** — confirmed inert: `setConfirming(false)`
  runs before `onDiscard`, so the dialog unmounts before `pending` renders. The
  row-level `disabled={pending}` at `:82` is live and correct.
- **Ticket-key fixture blind spot** (Task 23): the `ReviewStatusLine` fixture never
  has a *second* included change carrying the ticket key.

### Closed — not deferred, verified resolved

Task 13's `enabled` flip (Task 26's `!confirming` gate does exactly this, pinned at
`ReviewPage.test.tsx:232`), Task 21's `onToggleGroup` args (Task 22 landed a
stronger assertion), and all parked findings (Rulings 12/16/25).

### Worth knowing before touching tests

- **FE-04/13/14 are vacuous**: `lib/schemas/` was deleted and there are zero `zod`
  / `react-hook-form` imports left on the branch. Confirm that is intended.
- **Counter-evidence — these are genuinely solid, do not "improve" them:**
  localStorage total-read discipline is test-enforced (mutating `store.ts:63` to
  `cache = parsed as T` fails 4 tests across all three keys); `applyOrder` and
  `fileTree` fixtures survived targeted sampling for the shared-value defect. No
  orphaned components.

## Repo facts discovered during execution — these outlive this task

1. **`npx tsc --noEmit` is a NO-OP in this repo.** `apps/frontend/tsconfig.json`
   has `files: []` and only project references, so it exits 0 without checking
   anything. **The real type check is `npx tsc -b --force`** from `apps/frontend`.
   Every "tsc clean" claim across tasks 1-26 was vacuous; the branch does pass the
   real check. Worth fixing in the repo's own docs and CI.
2. **Vitest's default `threads` pool can hang indefinitely in this sandbox** —
   pass `--pool=forks`. Not in the repo's vitest config. (Did not reproduce during
   Task 27; recorded as observed, not as fixed.)
3. **`npm run build` deletes the tracked `apps/backend/internal/ui/dist/.gitkeep`.**
   Restore with `git checkout -- apps/backend/internal/ui/dist/.gitkeep`.
4. **`format:check` had dropped out of the implementer and reviewer briefs**, and
   Task 23 shipped an unformatted file that would have failed CI. Keep it in every
   dispatch's gate list.
5. **`SendMessage` was disabled this session**, so fix rounds dispatched a fresh
   implementer reading the report file rather than resuming the original agent.
   The report file is the persistent memory that makes this work.

## Process lessons from this plan

- **Only a performed mutation is evidence.** Reviewers on this plan were wrong in
  *both* directions — one under-called a real finding, another wrongly cleared a
  genuinely uncovered gap. Task 22 lost two extra fix rounds to a coverage claim
  made without performing the mutation. Every review and re-review dispatch must
  require the reviewer to run its own mutations against the **full** suite and
  quote real failure output.
- **The signature defect of this plan is a fixture whose two distinct inputs share
  a value**, or one too small to exercise an ordering, depth, or index bug. It
  appeared in eight separate tasks. Brief implementers on it up front.
- **The plan's briefs repeatedly contradicted themselves** — four tasks shipped
  sample code that could not satisfy the brief's own assertions, or that the
  repo's lint rules reject outright. When that happens the more specific
  expression of intent wins; record a ruling.
- **A brief that rewrites an existing test file may silently delete cases.**
  Always diff old against new and preserve coverage of behavior that still exists.

## Rulings

35 rulings were made during execution, all recorded in
`.superpowers/sdd/plan/progress.md` with rationale and cost-if-wrong. Rulings 1-11
and 13-14 settled in session 1; 12, 15-17 in session 2; 18-34 in session 3. The
load-bearing ones:

- **Ruling 16/25** — `BaseBranchSelect` has no error affordance, so the
  branches-fetch-failure banner belongs to the create page; and Task 23's
  hardcoded "Ready" badge is correct only because Task 26 gates
  `ReviewStatusLine` behind an exhaustive `switch` with `assertUnreachable`.
  **Both were verified to hold** (Task 26's reviewer proved exhaustiveness by
  adding a seventh `ReviewStatus` member and getting `TS2345`).
- **Ruling 22/23** — the plan's test steps deleted coverage of live behavior; the
  brief's authority covers what to build, not permission to drop coverage.
  Backfilled.
- **Ruling 29/32** — Important findings enter the fix loop even when a reviewer
  calls them non-blocking.
