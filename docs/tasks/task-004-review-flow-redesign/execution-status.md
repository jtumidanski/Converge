# task-004 execution status

Phase 4 (`/execute-task task-004`) is partially complete. This file exists so a
fresh session can resume without re-deriving anything.

## Resume instructions

1. `cd` into this worktree (`.worktrees/task-004-review-flow-redesign`).
2. `/clear`, then re-run `/execute-task task-004`.
3. The SDD skill will find the ledger at `.superpowers/sdd/plan/progress.md`
   (git-ignored). It is the authority on what is done.
4. **Resume at Task 21. Do not re-dispatch tasks 1-20.**

## Done and reviewed

Tasks 1-16 completed in session 1; see git history and the ledger for their
commits. Tasks 17-20 completed in session 2:

| Task | Commits | Outcome |
|---|---|---|
| 17 — reviews table, rows, progress cell | `9827286`, `4bad526` | clean after 1 fix round |
| 18 — root page + new-review drawer | `611f3a9`, `01e2f52` | clean after 1 fix round |
| 19 — base-branch select | `8351054`, `9873ac0` | clean after 1 fix round |
| 20 — change filter row | `cd1265b`, `b762b57` | clean after 1 fix round |

HEAD at handoff: `b762b57`. Working tree clean.

## Controller full gate (run at `9873ac0`)

- `npx tsc --noEmit` clean
- `npx vitest run --pool=forks` — 42 files / 343 tests pass (346 after Task 19's fix)
- `npm run lint` — 0 errors, 1 pre-existing `react-refresh` warning on
  `RepositoryResults.tsx`
- `npm run build` succeeds
- backend `go vet ./...` and `CGO_ENABLED=0 go build ./...` clean

## Resume exactly here

**Task 21 — not started.** Briefs for all of 1-27 already exist in
`.superpowers/sdd/plan/`. Remaining: create page (21-22), review page (23-26),
verification sweep (27).

Carry into Task 22's dispatch: **Ruling 16** — `BaseBranchSelect` cannot
distinguish a branches-fetch error from an empty result (both degrade to
pinned-default + typed-value). Its brief specifies no `isError` prop, so the
fetch-failure banner belongs to the composing create-review page, not the
component.

## Operational findings — carry these into every dispatch

**Vitest's default `threads` pool hangs indefinitely in this sandbox. Always
pass `--pool=forks`.** Not in the repo's vitest config. Every dispatch must
carry it plus a foreground-only, explicit-timeout instruction.

**Only a performed mutation is evidence.** Reviewers on this plan have been
wrong in both directions — one under-called a real finding as non-blocking,
another wrongly cleared a gap that was genuinely uncovered. Every review and
re-review dispatch must instruct the reviewer to run its own mutations and
quote the real failure output, and every re-review must end with a
`git status --porcelain` cleanliness check so no mutation leaks into the branch.

**The recurring defect shape is a fixture whose two distinct inputs share a
value**, which makes a mix-up invisible. It has now appeared three times
(Task 19's `value`/`defaultBranch` both `"main"`; Task 20's `hideBots` default
`true` hiding a hardcoded `checked`). Brief implementers on it up front.

**The plan's own sample code writes bare user-facing string literals.** Eight
tasks have now had to route them through `src/lib/strings.ts`. Brief every
remaining implementer on this before dispatch rather than catching it in review.

**The plan's test samples omit MSW lifecycle hooks.** This repo has no global
`server.listen()`; every MSW-using test file declares
`beforeAll/afterEach/afterAll` locally. Without them tests silently pass for the
wrong reason.

**`npm run build` deletes the tracked `apps/backend/internal/ui/dist/.gitkeep`.**
Restore it with `git checkout --` afterwards, or avoid the command.

## Incident — stale agents from session 1

Two implementer subagents dispatched in session 1 woke ~2 hours later, mid
session 2, and reported. The first wrote into the worktree while tasks 17-20
implementers were running and **clobbered a committed file**
(`useBreadcrumbs.ts`); it detected and restored this itself. The second did no
damage. Both agents' work was already committed and ledgered; both reports were
duplicates.

Controller verification confirmed no corruption survived: commit ancestry intact
and the full gate above is green. **Any future wake-up reporting on tasks 1-16 is
a duplicate — verify tree cleanliness and commit ancestry, ledger it, and
re-dispatch nothing.**

This incident is also the attributed cause of the "BaseBranchSelect is flaky"
report (Ruling 17): it was observed during exactly the clobbering window, could
not be reproduced across 13 runs afterwards, and the full suite has since run
clean. **Closed — do not reopen without a fresh reproduction.**

## Rulings

Rulings 1-15 are recorded in the ledger with full rationale; 1-11 and 13-14 were
settled in session 1. Session 2 added:

12. **RESOLVED BY THE USER: relax the client repository validator to match the
    backend.** `isValidRepositoryName` was stricter than
    `gitx.ValidateRepoFullName`, making `org/.github` — a real repository the
    backend accepts — unreachable from every input path in the drawer. The user
    chose to relax it and to override the plan-supplied test asserting
    `atlas/.hidden` must reject. Implemented in `01e2f52`; a re-review confirmed
    the client predicate now mirrors `validate.go:32-45` exactly, neither
    stricter nor looser.
15. **The drawer's silent no-op on a rejected name is a defect** — fixed in
    `01e2f52` with a `strings.ts`-routed inline error, independent of Ruling 12.
16. **The `isError`-vs-empty gap is not a defect in `BaseBranchSelect`.** Its
    brief specifies no error affordance and the degrade path is the one the brief
    designed for the empty case. A fetch-failure banner belongs to Task 22.
    Cost if wrong: Task 22 ships without one; additive to fix.
17. **The reported `BaseBranchSelect` flakiness is an artifact of the stale-agent
    incident, not a test defect.** See above. Cost if wrong: a genuinely flaky
    test resurfaces; the evidence is recorded so the next observer starts here.

No findings are parked. No breaker has tripped.
