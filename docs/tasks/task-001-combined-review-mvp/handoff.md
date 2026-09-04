# Task 001 — Execution Handoff

Written at a deliberate pause during Phase 4 (`/execute-task`). This document is
the resumption point: what is done, what is next, what was decided along the way,
and what a future session must not forget.

- **Branch:** `task-001-combined-review-mvp`
- **Worktree:** `.worktrees/task-001-combined-review-mvp`
- **Plan:** `docs/tasks/task-001-combined-review-mvp/plan.md` (30 tasks, phases A–G)
- **Spec:** `design.md` (binding), argued from `prd.md`. Where the two conflict,
  **the PRD wins** — this came up for real (see Ruling R21).
- **Ledger (authoritative progress record):** `.superpowers/sdd/plan/progress.md`
  — git-ignored scratch. **If it is lost, recover from `git log` plus this file.**

---

## 1. Status

**13 of 30 tasks complete, and Task 14 implemented but not yet reviewed.** Phases A (foundations), B (providers) and C (git
layers) are finished; Phase D (review domain) is over half done.

| Phase | Tasks | State |
|---|---|---|
| A — Foundations | 1–3 | complete |
| B — Providers | 4–6 | complete |
| C — Git layers | 7–9 | complete |
| D — Review domain | 10–17 | 10–13 complete; **14 committed, not reviewed**; 15–17 not started |
| E — HTTP API | 18–20 | not started |
| F — Frontend | 21–26 | not started |
| G — Packaging, CI, docs | 27–30 | not started |

Commit range so far: `75f52df..e95bab5`.

### Completed tasks and their commit ranges

| Task | Subject | Range | Outcome |
|---|---|---|---|
| 1 | Module scaffold, buildinfo, lint config, Makefile | `75f52df..eba2a60` | clean |
| 2 | Configuration package | `eba2a60..9e8137d` | clean |
| 3 | `gitx` — runner, validators, credentials, locks, redaction | `9e8137d..e372974` | 1 parked |
| 4 | Provider model, interface, errors, registry, fake | `e372974..dae9a4e` | clean (1 fix round) |
| 5 | GitHub provider | `dae9a4e..17b24ee` | 1 parked (1 fix round) |
| 6 | GitLab provider | `17b24ee..9b03c0c` | clean (1 fix round) |
| 7 | Scripted-repository test harness | `9b03c0c..20e7a7f` | clean (1 fix round) |
| 8 | Mirror cache and object reader | `20e7a7f..4ed960d` | clean (1 fix round) |
| 9 | Workspace manager | `4ed960d..87cee9f` | clean (1 fix round) |
| 10 | Diff production and parsing | `87cee9f..b05f695` | clean (1 fix round) |
| 11 | Session model, store, recovery, sweep | `b05f695..144d237` | clean (**3 fix rounds**) |
| 12 | Input validation, messages, landing resolution | `144d237..ffb360f` | clean (1 fix round) |
| 13 | Resolve pipeline (base selection) | `ffb360f..e18e8fe` | clean (1 fix round) |
| 14 | Cherry-pick applicator | `e18e8fe..e95bab5` | **committed, review pending** |

### Exactly where to resume

**Task 14 (cherry-pick applicator) is implemented and committed at `e95bab5`, but
has NOT been reviewed.** The gate is clean (13 packages, vet, lint, build).

**The next action is to dispatch Task 14's task review** —
`backend-guidelines-reviewer`, base `e18e8fe`, head `e95bab5` — then run the
normal fix loop (§3).

Make **this** the review's priority question. The implementer declared one
load-bearing deviation from the brief:

> The empty-outcome check compares **tree state** (`git diff --quiet <before> HEAD`)
> rather than the brief's `after == before` HEAD-SHA equality, because
> `--empty=keep` always advances `HEAD` even for a no-op pick — so the brief's
> check could never fire.

If that reasoning is right, the brief's version would have misclassified **every**
empty pick as applied, which is exactly this plan's recurring defect class (§2).
It needs verifying against real git rather than accepting — the implementer says
they did so by hand, and their transcript is in
`.superpowers/sdd/plan/task-14-report.md`. The implementer also amended two of the
brief's test setups (empty-pick and conflict) that failed spuriously when squash
branches were built sequentially through `main` instead of independently off
`base`; that also wants checking.

Task briefs for all 30 tasks are pre-generated at
`.superpowers/sdd/plan/task-N-brief.md`. Per-task audits are committed at
`docs/tasks/task-001-combined-review-mvp/audit-task-N.md`.

---

## 2. What a successor most needs to know

### The defect class this plan keeps producing

**Eight separate times**, a task shipped code where a real failure was returned as
innocuous-looking data instead of an error. Review caught every instance, but only
because it was looked for specifically:

- Task 7 — `FileContent` returned `""` for *any* git error, not just an absent path.
- Task 8 — `Exists` treated a corrupted mirror the same as a genuinely absent object.
- Task 10 — the diff summary silently zero-filled a record missing from `--numstat`.
- Task 11 — `FromRecord` accepted a corrupt SHA from `session.json` on restart.
- Task 11 — `Get` reported an unreadable session as simply missing.
- Task 12 — `patchIDsMatch` swallowed a `PatchID` error, silently collapsing a
  rebase into a squash strategy — i.e. a **wrong review, rendered confidently**.
- Task 12 — `resolveSingleParent` assumed a commit count when metadata was missing.
- Task 13 — a `RevParse` failure was reported to users as "has no first parent"
  regardless of the real cause.

**Every future task review must probe for this specifically.** The pattern is
always the same shape: a plausible zero value (`""`, `false`, `0`, an empty slice)
returned with a `nil` error. In this product the consequence is not a crash — it
is an incorrect diff shown to a reviewer with no warning.

### The plan's own text is not authoritative

The plan's illustrative code repeatedly contradicted the plan's own prose and
tests. Confirmed instances: Task 2 (map-order flake), Task 3 (two — a
`ValidateChangeNumbers` snippet contradicting its own test, and a
`cmd.Stdin` form that deadlocks its own test), Task 10 (two wrong test
expectations), Task 12 (duplicate-handling), Task 13 (reference code that
*cannot* satisfy the brief's own test).

Treat the brief as requirements, not as correct code. When they conflict, follow
the prose and the test, and record a ruling.

### Reviewer configuration (user decision, not a controller ruling)

Per-task review uses the **project's own agents** — `backend-guidelines-reviewer`
for Go, `frontend-guidelines-reviewer` for TS/React — not the SDD skill's generic
task reviewer. The user chose this explicitly, having been told it drops the
generic reviewer's spec-compliance-against-the-brief check.

Two mitigations are in force and should continue:
- Each guidelines reviewer is handed the **task brief** alongside the diff and
  report, and asked for a spec-compliance verdict *in addition to* its checklist.
- Reviewers write to **`audit-task-N.md`**, not `audit.md`. `audit.md` is reserved
  for the consolidated end-of-branch review at Task 30, which still runs the full
  trio via `superpowers:requesting-code-review`.

### Environment traps that cost real time

- **The Bash tool's 120 s default timeout silently backgrounds long commands.** A
  *cold* `go test -race` build exceeds it. Two agents stalled indefinitely waiting
  on their own auto-backgrounded jobs before this was understood. Every dispatch
  now instructs implementers to pass an explicit `timeout: 600000` on Go commands.
- **`SendMessage` is disabled in this session.** The SDD skill's "resume the
  original implementer for fix rounds 1–3" is therefore unavailable; fresh
  implementers carry the brief path and report path instead (Ruling R4).
- **A `completed` task notification does not mean the agent is dead.** It can mean
  the agent yielded its turn and is still resumable. Treating one as terminal
  caused two agents to run concurrently in the same worktree (Ruling R7).

---

## 3. The execution loop

Per task: dispatch implementer → generate review package → dispatch reviewer →
adjudicate findings → fix round(s) → scoped re-review → ledger + next task.

```sh
SK=~/.claude/plugins/cache/claude-plugins-official/superpowers/6.3.0/skills/subagent-driven-development
# brief for task N (all 30 already generated)
$SK/scripts/task-brief docs/tasks/task-001-combined-review-mvp/plan.md N
# diff package for a review (BASE = commit before the task/fix round)
$SK/scripts/review-package docs/tasks/task-001-combined-review-mvp/plan.md BASE HEAD
```

**Rules that have earned their keep:**

- Findings rated Critical or Important enter the fix loop **regardless of the
  reviewer's headline verdict** — several tasks came back "Approved" with real
  Important findings attached.
- Minor findings are deferred to the ledger, never fixed in-loop.
- A **plan-mandated** defect is still a defect. Weigh it against the spec and rule;
  do not dismiss it because the plan authored it.
- Never fix findings in the controller session.
- Verify the load-bearing claims yourself rather than trusting a report. Reports in
  this plan have falsely claimed byte-for-byte fidelity, omitted a helper as
  "unused" that two later tasks needed, undercounted lint suppressions, and
  overstated real-git coverage — all caught.

Every dispatch must carry: worktree discipline (`cd <worktree> && …` on every Bash
call), the git-safety rules (named paths only, no `git add -A`, no destructive
ops, no push, verify branch after commit), the backend gate, and the standing
constraint that **tokens never appear in logs, API responses, `session.json`, git
argv, or error strings**.

---

## 4. Rulings I made on the user's behalf

These are decisions a human would reasonably want to revisit. Each is recorded in
the ledger with its cost-if-wrong.

**Pre-flight**

- **R1** — Tasks 21 and 25 create files their **Files** headers omit
  (`tools/build-backend.sh` stub, `ReviewPage.tsx` placeholder); the step bodies
  govern. *Cost: one extra file each, overwritten later anyway.*

**Process**

- **R4** — `SendMessage` unavailable → fix rounds use a fresh implementer carrying
  the brief and report paths. *Cost: loses the original's in-context memory.*
- **R5** — Escalated Task 3 to a more capable model after two stalls.
- **R6** — Dispatches instruct an explicit 600 s timeout rather than forbidding
  backgrounding. *Cost: none material.*
- **R7** — Before replacing an agent that reported without a status contract,
  check the tree **and assume the original may still be live**. *Cost: a genuinely
  dead agent stalls until noticed, instead of two agents racing.*

**Accepted deviations from the plan's code**

- **R2/R3** — Task 2's `loadProviders` iterates `env` in declaration order (the
  plan's map-ranging version flaked ~2/8 runs); a scoped `//nolint:staticcheck` on
  a deliberately-redundant test format string.
- **R8** — `ValidateChangeNumbers` de-duplicates (plan prose + its own test) rather
  than erroring as the plan's snippet did.
- **R9** — `ExecRunner.Run` uses `cmd.StdinPipe()` + a detached copy goroutine; the
  plan's literal `cmd.Stdin = s.Stdin` deadlocks its own test, per `os/exec` docs.
  *Cost: a goroutine leaks if a caller passes a never-unblocking reader.*
- **R14** — Accepted Task 10's two corrections to the plan's test expectations,
  both verified against real git. *Cost: assertions match observed git, not the
  plan's arithmetic.*
- **R17** — Accepted an unrequested Debug-vs-Warn downgrade when a cleanup repeat
  finds the session dir already gone. Logging-only.

**Parked — real, deliberately not fixed**

- **R10** — `gitx.LockMap` grows without bound (one mutex per repo, no eviction).
  `design.md` §5.4 specifies exactly that structure; refcounted eviction would put
  an acquire/release race into the core locking primitive to reclaim kilobytes.
  *Symptom if wrong: slow memory creep on a long-lived instance.*
- **R12** — Task 5's GitHub scan cache holds one mutex across network I/O,
  serialising scans across repos. Throughput, not correctness; per-key locking is
  a larger redesign. *Symptom: latency under concurrent load.*

**Substantive design decisions**

- **R11** — Fixed Task 5's scan-cache data race even though the design came from
  the plan. A data race is a defect regardless of authorship.
- **R13** — GitLab's merged-search truncates to `page.Size` with `HasNext`, rather
  than redesigning dual-query pagination. *Cost: approximate page boundaries in a
  search convenience path.*
- **R15/R16** — Bounded GitLab commits at 250 (`ErrTooManyCommits`), mirroring
  GitHub; hardened `Exists` to error on fatal git failures because Task 12's
  landing resolution depends on it.
- **R18** — **Task 11, ordering:** write the terminal status into the index
  *before* running `Cleanup` in `Finish`/`expire`. Closes a window where a client
  could be served a `READY` review whose workspace was being deleted.
- **R19** — **Task 11, consequence of R18:** that reorder made a failed cleanup
  invisible to `Sweep`, leaking the workspace. Fixed with a bounded retry
  (`pendingCleanup`, give-up after 5 attempts, seeded by `LoadAll` on restart)
  plus persisting the terminal status **only on the failure path** — on success
  the directory including `session.json` is deleted, so index-only is correct
  there. *This chain is worth understanding before touching `store.go`.*
- **R20** — Session transitions get **terminal-state protection only** (no-ops on
  `FINISHED`/`EXPIRED`; the two error-returning transitions reject), not a full
  state machine — six of eight transitions cannot return an error without
  signature changes Tasks 13/15/19 depend on.
- **R21** — **Duplicate change numbers:** the spec contradicts itself.
  `prd.md:541` lists duplicates as an `INVALID_CHANGES` condition;
  `design.md:324` says de-duplicate. **The PRD wins.** `review.CreateInput.Validate`
  rejects; `gitx.ValidateChangeNumbers` keeps de-duplication as a defensive lower
  layer. Both stand as layering, not conflict.
- **R22** — Fixed `patchIDsMatch` by gating on `Exists` first, rather than
  hardening `mirror.PatchID` (which would reach into Task 8's package from Task
  12's fix round). *Cost: one extra round-trip per rebase candidate.*
- **R23** — `resolveSingleParent` returns `BASE_UNDETERMINED` instead of assuming
  a commit count. Failing visibly beats reconstructing from an unverified guess.
- **R24** — **Task 13, split reorder:** the plan's brief genuinely contradicts
  itself, but only for the target-mismatch check. `NOT_MERGED` runs first on
  provider data (avoiding a full mirror clone before rejecting), target-mismatch
  stays after the branch check.

---

## 5. Deferred minors (26) and open follow-ups

All are recorded in the ledger as `minor (deferred)` lines and must be handed to
the **Task 30 whole-branch review** for triage. The ones with the most substance:

- **`.golangci.yml` carries a module-wide `G204` exclusion and a blanket
  gosec/errcheck exclusion for test files**, both from Task 1. This predates the
  rule established in Task 4 that module-wide suppressions are unwelcome. Not a
  regression, but the final review should decide whether they stay.
- **Task 10's sort-order test does not discriminate** — the fixture's paths come
  out of git already in lexical order, so the test would still pass if
  `sort.Slice` were deleted. Needs paths where emission and lexical order diverge.
- **Task 11:** cleanup give-up is a single `Error` log with no metric or alert;
  `LoadAll`'s `pendingCleanup` seeding path has no test.
- **Task 3:** `NewExecRunner` does not nil-check its logger; `fake.go`'s
  `Snapshot()` is unrequested; production `IsExit` depends on a helper defined in
  the fake-runner file.
- **Task 8/13:** two error wraps are leak-free only because
  `gitx.ExitError.Error()` formats category and exit code and never stderr — if
  that ever changes, both need re-review together.
- One commit (`144d237`) used the prefix `fix(session):` instead of the plan's
  `fix(task-001):` convention.

---

## 6. Carried acceptance points for later tasks

Findings deliberately deferred to the task that owns them:

- **Task 17** must enforce **git ≥ 2.45 at startup** (`MinGitVersion`,
  `checkGitVersion`) — `gitx` deliberately does not, and Task 3's review flagged it.
- **Task 17** must wire `REPOSITORY_CACHE_ROOT` into `mirror.New`'s `root`.
- **Task 19** must wire `session.ErrNotFound`, which is currently declared but
  unused.
- **Task 16** should cover the `ErrTooManyCommits` and provider-error sub-paths in
  Task 13's per-change loop, which have no unit coverage.

---

## 7. Verification

Backend gate (cwd `apps/backend`), all four must be clean:

```sh
go test -race -count=1 ./...
go vet ./...
go tool golangci-lint run
CGO_ENABLED=0 go build ./...
```

Integration: `go test -race -count=1 -tags integration ./...` (Task 16 onward).
Frontend gate (cwd `apps/frontend`, from Task 21): `npm ci`, `npm run lint`,
`npm run format:check`, `npm test`, `npm run build`. Node may need
`export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22`.

**Before opening a PR**, `CLAUDE.md` requires the code-review step
(`/audit-plan` or `superpowers:requesting-code-review`); Task 30 is that step, and
it must be pointed at the deferred-minor and parked lists in the ledger.
