# task-004 execution status

Phase 4 (`/execute-task task-004`) is partially complete. This file exists so a
fresh session can resume without re-deriving anything.

## Resume instructions

1. `cd` into this worktree (`.worktrees/task-004-review-flow-redesign`).
2. `/clear`, then re-run `/execute-task task-004`.
3. The SDD skill will find the ledger at `.superpowers/sdd/plan/progress.md`
   (git-ignored). It is the authority on what is done. Resume at **Task 9**.

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

All of Phase A (backend) is complete. Backend and frontend gates were green at
each commit.

## Remaining

Tasks 9-27: frontend foundations (9-16), root page (17-18), create page (19-22),
review page (23-26), verification sweep (27).

## Rulings made so far

Full text with rationale and cost-if-wrong lives in the ledger. In short:

1. **Task 14 does not delete `lib/schemas/repository.ts`** — Task 18 owns that
   deletion. Task 14's Files header and its Step 7 body contradict each other;
   the body is right, since the file's only consumer survives until Task 18.
2. **`config.NewSecret(...)`, not `config.Secret(...)`** in the plan's Go test
   snippets — `config.Secret` is a struct, not a constructor. The plan's form
   does not compile. Applies to tasks 3-6.
3. **Task 4's GitHub `fetch` closure signature** in the plan cannot be passed to
   `FilterWalk`; the two-closure form matching `ListRepositories` was used
   instead. Behaviour is what the plan's tests assert.
4. **`internal/api/changes.go` was brought under `searchFrom`**, though no task
   lists that file. The Global Constraint that providers never receive an
   untrimmed or oversized `search` is unconditional, and Task 5 exists to enforce
   it. Effect: `?search` over 200 runes on the changes listing is now
   `400 INVALID_SEARCH`.
5. **The `cn@^0.2.6` runtime dependency the shadcn installer added was removed**,
   and the 13 generated components now import `cn` from `@/lib/utils` like the 8
   pre-existing ones. The plan budgets `cmdk` as the only new dependency, and
   `components.json` already names the utils alias. A future `npx shadcn add`
   will regenerate the `cn` import and need the same sweep.

No findings are parked; no breaker has tripped.
