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

---
---

# SESSION 2 UPDATE

**Everything above this line still holds except §1 (Status), which this section
supersedes.** §2 (what a successor most needs to know), §3 (the execution loop)
and §5–§7 remain accurate and are still worth reading first. §4's rulings R1–R24
stand; R25–R31 are added below.

## 1a. Status (supersedes §1)

**15 of 30 tasks complete.** Phases A–C finished; Phase D (review domain) is
15/17 done — Tasks 16 and 17 remain.

| Phase | Tasks | State |
|---|---|---|
| A — Foundations | 1–3 | complete |
| B — Providers | 4–6 | complete |
| C — Git layers | 7–9 | complete |
| D — Review domain | 10–17 | 10–15 complete; 16–17 not started |
| E — HTTP API | 18–20 | not started |
| F — Frontend | 21–26 | not started |
| G — Packaging, CI, docs | 27–30 | not started |

| Task | Subject | Range | Outcome |
|---|---|---|---|
| 14 | Cherry-pick applicator | `e18e8fe..88ad937` | clean (1 fix round) |
| 15 | Review service + cleaner adapter | `72549d3..818a716` | **3 fix rounds** — 1 Critical, and a Critical regression introduced by round 2 |

### Exactly where to resume

**Task 15 is at fix round 4 of 5.** Rounds 1–3 are committed and their re-reviews
are done. Round 3's re-review came back NEEDS-WORK on one point, and **fix round 4
was dispatched but its result was never recorded here** — the session ended at its
context limit while it was in flight.

**Round 4 landed (`e6a8fb2`) and DOES NOT WORK. Task 15 needs a round 5.** This
was measured, not inferred — see "Round 4 outcome" immediately below. Do **not**
run a round-4 re-review; go straight to fixing.

What round 4 was trying to fix:

> `TestServiceStartBuildRespectsMaxConcurrentBuilds` had a **dead assertion**.
> Its watchdog (`service_test.go:406`) is armed *before* the deadline it races
> (`:441`) is computed, so the watchdog always opens the barrier first and the
> `t.Errorf` at `:451` is unreachable by construction. A reviewer proved the
> consequence: breaking `queuedOnSemaphore`'s goroutine-dump matcher
> (`const waiting`, `:496`) with the semaphore bound left intact produced
> `ok … 121.487s` — a **silent pass**.
>
> Round 4 was told to make the watchdog record a failure (e.g. an `atomic.Bool`
> checked after the loop), without weakening either peak assertion and without
> reintroducing the flaky timing-based lower bound. It owes **two** mutation
> ratios, each ≥10 runs under `go test -race -count=1`: broken matcher (must now
> fail; currently 0/10) and removed semaphore bound (must stay 10/10).

### Round 4 outcome — measured, and it failed

Two implementers stalled on this round before one finally committed. **The reason
was structural, not negligence: the broken-matcher mutation trips a 120-second
watchdog, so a single run takes ~121s and the required 10 runs is a ~20-minute
job no subagent turn can hold.** If you re-dispatch this, hand over the
measurement as a background script, or reduce the watchdog for the measurement.

**Two hazards this left behind — read before touching anything:**

1. **Commit `e6a8fb2` shipped the mutation.** Its report claimed "both mutations
   fully reverted"; that was false. Line 517 was committed as
   `...StartBuild.func1_MUTATED_NOMATCH(`, i.e. the permanently blind test round 4
   existed to prevent. **Already repaired** by commit `29cde86`, which restores
   the real matcher. HEAD is clean; `e6a8fb2` retains it in history.
2. An implementer believed stalled **resumed on its own and committed mid-run**
   while controller-side verification was mutating the same files (ruling R7, in
   its sharpest form). Never run mutation testing against the worktree while any
   agent may be live — use a scratch copy outside the worktree, as the
   re-reviewers do.

**The clean measurement** (taken with no other agent live, matcher restored, gate
green): baseline 3/3 PASS; **broken matcher, semaphore bound intact: 0/10 failed
— the test still passes blind.** The `watchdogFired atomic.Bool` is correctly
written but is never reached. The silent-pass hole is still open.

**Concrete lead for round 5 — a hypothesis to test, not a conclusion.** The
constants are `limit = 2`, `builds = 4`, so `builds-limit = 2`. If
`queuedOnSemaphore()` really returned 0 under a broken matcher, the loop
condition `got >= limit && queuedOnSemaphore() >= builds-limit` would stay false,
the loop would run to the 120s watchdog, and the post-loop `t.Errorf` would fire —
the fix *would* work. It doesn't, and the runs did not take 120s, so **the barrier
is being opened early**. Most likely `queuedOnSemaphore` does not return 0 with a
broken `waiting` string — e.g. it filters on `"[select"` and treats the frame-name
check loosely enough that other parked selects still satisfy `>= 2`.

**Start by unit-testing `queuedOnSemaphore` itself** against a known goroutine
dump, with and without a matching frame name. A helper that silently over-counts
is the actual defect; the watchdog is downstream of it.

Round 3's re-review independently confirmed 10/10 mutation kills on all three of
its items, and md5-verified the four R18/R19 functions byte-identical for the
third time. Everything else in Task 15 is sound — this is the last open item.

Once Task 15 closes, **the next action is Task 16**. Carry these into Task 16's
dispatch:

- Task 16 must cover the `ErrTooManyCommits` and provider-error sub-paths in Task
  13's per-change loop, which still have no unit coverage (carried from §6).
- **`internal/review/resolve_test.go:426`
  `TestFetchChangesCancelsSiblingsOnFirstError` is FLAKY** — it failed once in ~11
  runs with "no sibling goroutine observed cancellation". Task 13 code, untouched
  by Tasks 14–15. A flaky test in the branch is a real defect; fix it in Task 16
  or hand it to Task 30 explicitly.
- Task 17 still owes: git ≥ 2.45 enforcement at startup (`MinGitVersion`,
  `checkGitVersion`), and wiring `REPOSITORY_CACHE_ROOT` into `mirror.New`.
- Task 19 still owes the HTTP mapping for `session.ErrNotFound` (the service now
  *produces* it — see R28).

## 2a. The defect class, updated

§2's list stands at eight instances; Tasks 14 and 15 added five more, so **the
count is thirteen**. Every one was caught only because a reviewer was told to
look for it specifically. Keep doing that in every dispatch.

- Task 14 — cherry-pick classification keyed on *any* non-zero exit, so a hard git
  failure (exit 128) returned `OutcomeConflict` with a `nil` error, carrying the
  **previous** change's conflicting paths.
- Task 15 — the brief's `save` swallowed every `Store.Save` failure; since `Save`
  also updates the index, that returned READY while `Get` reported CREATING forever.
- Task 15 — the brief's panic recovery discarded `s.fail(...)` and returned a
  **zero** session.
- Task 15 — the brief's `trimSHA` could return `""` straight into the diff range.
- Task 15 (C1, Critical) — `Build` held a stale session snapshot, so the terminal
  guard checked the stale copy and **READY overwrote FINISHED**, with `Store.Save`
  re-creating the just-deleted session directory. A discarded review reported READY
  forever against a deleted worktree.

**A newer, second failure mode worth its own attention: the silently
non-discriminating test.** There are now **five** proven cases — Task 10's sort
order, Task 14's empty outcome, Task 15's async test, Task 15's CAS atomicity
test, and Task 15's semaphore watchdog. Two of the five are the *same test* in
successive rounds.

The atomicity case is the sharpest lesson on measurement: its mutation *did*
reproduce, but only at `-count=300`, and passed **40/40** under
`go test -race -count=1`, the command the project actually runs.

The watchdog case is the sharpest lesson on *claims*: the implementer asserted
that a broken goroutine-dump matcher would "fail loudly rather than silently
pass." A reviewer tested the assertion instead of accepting it, and it was
false — the test reported `ok` after 121 seconds. **When an implementer claims a
mechanism fails loudly, make a reviewer break it and watch.**

**Rules that came out of this, and should stay in force:**

- Require a mutation ratio (e.g. 10/10), not a single anecdotal failure, and
  require it **under the project's own test command**.
- Be most suspicious of a *silent pass*: round 2 of Task 15 left an assertion
  textually intact but dead, and the test then passed with an effectively
  unbounded semaphore at 0/10 detection.

## 3a. Process notes

- **The brief's sample code is wrong far more often than §2 implies.** It is now
  **5 of 15 tasks** (2, 3, 10, 12, 13, plus three separate defects inside Task 15
  alone). Every implementer dispatch should carry the instruction: follow the
  prose and the tests, and *report* the conflict rather than silently choosing.
- **`SendMessage` was unavailable again**, so R4 still applies: fix rounds use a
  fresh implementer carrying the brief path, the report path, and the findings.
- **Review packages should skip docs-only commits.** Task 14's range
  `e18e8fe..e95bab5` was 305 KB, of which all but 16 KB was a docs commit. Package
  the code commit only.
- **A controller instruction can cause a regression.** Round 2's Critical came
  from *my* instruction to replace a flaky sleep with a barrier: right intent
  (kill the timing-dependent lower bound), but I did not require the replacement
  to preserve the upper bound's discriminating power. When ordering a test
  rewrite, state which assertion must keep its teeth and demand the mutation
  ratio that proves it.

## 4a. Rulings R25–R31

- **R25** — Task 14's commit trailer must carry the provider id
  (`Converge-Change: <provider-id>#<number>`). Not a PRD-vs-design conflict:
  `prd.md:278-280` and `design.md:430` agree, and the *brief* contradicted both.
  The brief's justification (that `session.ResolvedChange` has no provider field)
  is true but non-dispositive — `Session.ProviderID()` has it and a session is
  scoped to one provider. Widened `Apply` to take `providerID` rather than adding
  a field to `ResolvedChange`, which would have touched Task 11's model, its
  `session.json` persistence, and Task 13's construction sites. *Cost: one extra
  parameter threaded through Task 15.*
- **R26** — Amend **every** synthetic commit, not just the tip (`prd.md:278`
  "Each synthetic commit", `design.md:429` "each new commit"), by cherry-picking
  rebase SHAs **sequentially** with an amend after each. Also satisfies FR-6.4,
  makes FR-6.6's "commit SHA being applied" exact rather than inferred, and
  retired two minors. *Cost: N cherry-pick invocations instead of 1, on a path
  already bounded at 250 commits.*
- **R27** — Persistence and unclassified internal failures report as
  `GIT_FAILURE`; **no `INTERNAL` code**. `design.md:494` already records a
  recovered panic as `GIT_FAILURE`, and `INTERNAL` is absent from the
  exact-string set that Task 19 and the frontend consume. *Cost: `GIT_FAILURE`'s
  meaning widens slightly — a disk failure reads as a git failure — mitigated by
  logging the real cause at Error.*
- **R28** — The service **should** produce `session.ErrNotFound` (wrapped with
  `%w`) in `Files`/`FileDiff`/`CombinedDiffPath`. This **overrode my own earlier
  dispatch note**, which had wrongly told the implementer that Task 19 owned it;
  the carried acceptance point meant Task 19 *maps* it to a 404, not that Task 19
  must produce it. *Cost: none identified.*
- **R29** — Do **not** widen `Deps` to interfaces; require the tests instead. The
  implementer's premise (that concrete types blocked fault injection) was false —
  `ChangeApplicator`, `gitx.Runner`/`FakeRunner` and `Deps.Now` are already
  injectable, and the reviewer had driven those paths during review. Only
  `Store.Save` failure needed anything special (`chmod 0500`). *Cost: if a later
  task needs a fake `Store` or `Registry`, the change lands then, on evidence.*
- **R30** — Close C1 **structurally** with a compare-and-swap in `session.Store`,
  rather than accepting round 1's narrowed `Get`→`Save` window. A mitigation is
  not a fix for a Critical. Landed as `Store.SaveActive`, checking indexed status
  and doing the disk write under one acquisition of the existing mutex.
  Constraint: **add a method, reorder nothing** — R18/R19 stay exactly as they
  are (verified byte-identical by md5 twice since). *Cost: the terminal write path
  serialises across sessions behind one small-JSON fsync, bounded by
  `MAX_CONCURRENT_BUILDS`; and the plan's most delicate file was touched at fix
  round 2 rather than deferred to Task 30.*
- **R31** — `context.DeadlineExceeded` keeps the `INTERRUPTED` code but gets its
  own message. `MsgInterrupted()` says "interrupted by a server restart", which is
  false for a build that hit the 60-minute ceiling. Message-only, so the
  exact-string code set is untouched. Rejected inventing a timeout code, per R27.
  *Cost: one more user-facing string to translate later.*

## 5a. New deferred minors (add to §5's list for Task 30)

- `store.go` — `Store.onWriteRecord` is a production struct field that exists
  only so a test can hook the write. Settable only from within the package, but
  it is production surface serving a test.
- `service_test.go` — `queuedOnSemaphore` detects parked goroutines by matching
  **goroutine-dump text** (`[select` plus the top frame name `StartBuild.func1`).
  It needs updating if `StartBuild`'s goroutine is renamed or the `select` moves.
- Two timing-shaped tests remain in `service_test.go` (a 500 ms asynchrony budget
  with a 3 s watchdog). Assessed low-risk, ~20 clean executions including
  `GOMAXPROCS=1` and under concurrent load.
- `classify` orders cancellation ahead of **all** classifications, so a CONFLICT
  landing exactly at shutdown records as INTERRUPTED. Truthful, and the comment
  now says so.

---

## 1b. Status (supersedes §1a)

**18 of 30 tasks complete.** Phases A–D finished; Phase E (HTTP API) is
under way.

| Phase | Tasks | State |
|---|---|---|
| A — Foundations | 1–3 | complete |
| B — Providers | 4–6 | complete |
| C — Git layers | 7–9 | complete |
| D — Review domain | 10–17 | **complete** |
| E — HTTP API | 18–20 | 18 complete; **19 in flight**; 20 not started |
| F — Frontend | 21–26 | not started |
| G — Packaging, CI, docs | 27–30 | not started |

| Task | Subject | Range | Outcome |
|---|---|---|---|
| 15 | Review service + cleaner adapter | `72549d3..4df8efb` | clean (**5 fix rounds**) |
| 16 | Integration tests (FR-12.2) | `4df8efb..f62bdcd` | clean (**0 fix rounds**) |
| 17 | App wiring + `converge-cli` | `f62bdcd..de14397` | clean (1 fix round) |
| 18 | JSON:API encode/decode | `de14397..ec1c195` | clean (1 fix round) |

### Exactly where to resume

**Task 19 (HTTP handlers) was dispatched at base `ec1c195` and its result was
not recorded here.** Check `git log` first: if a Task 19 commit exists, the
next action is its task review (`review-package … ec1c195 <head>`); if not,
re-dispatch from `.superpowers/sdd/plan/task-19-brief.md`.

Then Task 20, then Phase F.

### The Task 15 correction — read this before trusting §1a

§1a said "the round-4 fix DOES NOT WORK … measured, not inferred." **That
verdict was false**, and it cost a wasted round. The measurement had applied
its mutation by line number, and round 4 had shifted the target line, so the
mutation silently never landed and the test passed legitimately. The tell was
in the data all along: the runs did not take ~121 s each, which they must
have if the broken matcher were really in effect.

**Standing rule that came out of it:** any mutation measurement must print
grep proof that the mutation landed, and a ratio that contradicts a clear
mechanical argument is a suspect *measurement* first and a code defect
second. When the failure path is slow, shorten the timers in a scratch copy
and then re-run once at real durations to prove duration-independence — that
is what finally settled it.

## 2b. Carried items for the remaining tasks

- **Task 20 must not re-open what Task 19 wired.** `Store.RunSweeper` was
  never started anywhere before Task 19 (`cfg.CleanupInterval` was parsed and
  ignored — correct for a one-shot CLI, wrong for a server). Task 19 owns
  starting it and stopping it cleanly.
- **Phase F consumes exact strings.** The JSON:API field names, the error
  `code` set, and the resource `type` values are contract. Task 18's review
  proved the wire format is pinned by tests (renaming any contract field
  fails a test), so treat a frontend/back-end mismatch as a frontend bug
  until a test says otherwise.
- **Task 30 minor list** now also holds: `checkGitVersion` accepts
  `"3.garbage"`/`"10.x"`/`"9.a.b"` post-`de14397` and its justifying comment
  is wrong; `config` accepts userinfo in `BASE_URL` (only the *logging* was
  fixed, in Task 17); slog attribute **keys** and group names are never
  scrubbed; secret scrubbing is exact-substring only, so base64 and
  percent-encoded forms pass through; `gitx/exec.go:111` sets `cmd.Dir`
  with no empty-`Dir` guard; `harness_test.go:116` calls `t.Fatalf` from a
  goroutine; commit `ec1c195` uses a `test(jsonapi):` subject where the rest
  of the branch uses `test(task-001):`.

## 3b. What changed about how this plan is run

- **The defect class is now nine instances**, and its subtlest form appeared
  in Task 18: a test that was not blind, merely *aimed slightly off-target* —
  it asserted that some error came back, not which path produced it, and so
  passed under an off-by-one that changed the failure's cause.
- **Every review since Task 16 has re-run the implementer's mutations
  itself** rather than reading the claim. That found the Task 18 miss and
  confirmed Task 17's fixes. Keep doing it; it is the only check that has
  reliably caught this plan's defect class.
- **Model tier is now scaled to the diff**, not to the task number: opus for
  the concurrency and token-handling reviews, sonnet for ordinary
  implementation, haiku for a three-line test fix and its re-review. No
  regression has been traced to the cheaper tiers.
- **`SendMessage` is still unavailable**, so R4 stands: every fix round is a
  fresh implementer carrying the brief, report, review and findings paths.

### Task 19 landed — resumption point updated

**Task 19 is committed at `03a16e3`** (12 files, 1264 insertions, all under
`apps/backend/internal/api/`). The gate is clean and the commit was verified
by the controller, not merely reported: no `INTERNAL` code string exists
anywhere in `apps/backend`, and `Store.RunSweeper` is genuinely started
(`router.go:55`, tied to `Deps.BuildContext`, covered at `api_test.go:364`).

**The next action is Task 19's task review** — `review-package ec1c195
03a16e3`, dispatched on a capable model.

**Make this the centrepiece of that review:** the implementer
mutation-proved only the sweeper wiring it added itself. For the
brief-supplied handler tests it claims they were "cross-checked against real
interfaces and are structured to assert error codes and resource types". That
is not a measurement — it is the same claim shape that has been false four
times in this plan. The 437-line `api_test.go` is unverified, and the review
must break the handlers and watch the tests fail rather than read them.

Task 19 also leaves one thing explicitly for Task 20: `internal/app.New`
still does not wire `CleanupInterval` into `api.Deps`, so the sweeper is
started by the router but the interval does not yet flow from config.

---

# SESSION 4 UPDATE

**Everything above still holds except the status sections, which this supersedes.**
§2 (the defect class), §3 (the execution loop) and §5–§7 remain accurate and are
still the first thing to read. Rulings R1–R31 stand; R32–R38 are added below.

## 1c. Status (supersedes §1b)

**20 of 30 tasks complete. Phases A–E are closed.**

| Phase | Tasks | State |
|---|---|---|
| A — Foundations | 1–3 | complete |
| B — Providers | 4–6 | complete |
| C — Git layers | 7–9 | complete |
| D — Review domain | 10–17 | complete |
| E — HTTP API | 18–20 | **complete** |
| F — Frontend | 21–26 | **21 in flight**; 22–26 not started |
| G — Packaging, CI, docs | 27–30 | not started |

| Task | Subject | Range | Outcome |
|---|---|---|---|
| 19 | JSON:API HTTP handlers | `ec1c195..9c462fa` | clean (1 fix round) |
| 20 | Server binary | `113b55c..89aa9b8` | clean (1 fix round) |

### Exactly where to resume

**Task 21 (frontend scaffold) was dispatched at base `5ab46a2`.** Check `git log`
first: if a Task 21 commit exists, the next action is its task review
(`review-package … 5ab46a2 <head>`, dispatched on `frontend-guidelines-reviewer`);
if not, re-dispatch from `.superpowers/sdd/plan/task-21-brief.md`.

## 2c. The two lessons this session added

**A mutation sweep is not a coverage report — and I mistook one for the other.**
Task 19's review ran 39 mutations and I approved it. Task 20 then found that
`internal/api/router.go` registered the embedded UI at `mux.Handle("GET /", …)`,
which **panics at registration** against the existing `"/api/"` pattern (verbatim:
`pattern "GET /" … conflicts with pattern "/api/": GET / matches fewer methods
than /api/, but has a more general path pattern`). It survived a 39-mutation
review because **no test anywhere set `Deps.UI` non-nil** — the entire UI branch
was unreachable from tests. Mutation testing measures whether *exercised* code is
protected; it says nothing about code no test reaches. **Every review from here
should check reachability before it checks protection**, and Task 30 should sweep
the branch for unreached branches specifically.

**A mutant that fails to compile looks exactly like a mutant that was caught.**
Task 20's implementer reported mutations M5/M6 as evidence; they did not build
(`"errors" imported and not used`), and a build failure exits non-zero exactly as
a failing assertion does. The reviewer noticed and reformulated them. **Every
mutation instruction now requires the mutant to BUILD and the failure to be an
ASSERTION failure with its text quoted.** A non-zero exit is not evidence. The
rule worked on its first outing in Task 20's re-review.

## 3c. Carried into Phase F — read before Task 21's review

- **The first `npm run build` changes backend behaviour.** Vite's `build.outDir`
  is `../backend/internal/ui/dist`, so the first frontend build flips
  `ui.Present()` from false to true and starts exercising `uiHandler`'s present
  branch. `cmd/converge`'s own suite has never reached that branch — proven, not
  assumed: stripping the method gate left `TestServerServesHealthAndShutsDown`
  passing. Coverage today is real but lives only in `internal/api`, which forces
  `present=true` with an `fstest.MapFS`. **Task 21 or 30 should add a
  `cmd/converge` test that forces a populated UI rather than depending on ambient
  `dist` state.** Run the BACKEND gate after any frontend build.
- **The backend wire format is pinned by backend tests.** Field names, the error
  `code` set and resource `type` values are contract. Treat a frontend/backend
  mismatch as a frontend bug until a test says otherwise.
- **The error-code set the frontend must handle is the spec set PLUS the four
  transport codes** kept by R32: `INVALID_REQUEST`, `NOT_FOUND`,
  `NOT_ACCEPTABLE`, `INVALID_STATE`. There is still no `INTERNAL` code.
- **Task 21's pinned versions are dated** "latest verified 2026-09-04" and may no
  longer resolve. The brief already concedes fragility on the TypeScript 5-vs-7
  case. Drift must be reported, not silently floated.

## 4c. Rulings R32–R38

- **R32** — the four transport codes `INVALID_REQUEST`, `NOT_FOUND`,
  `NOT_ACCEPTABLE`, `INVALID_STATE` **stay**. They cover transport and routing
  failures the spec's domain set never enumerates; `REVIEW_NOT_READY` is still
  correctly used for the domain 409. The alternatives were reusing a domain code
  that lies about the cause — this plan's defect class exactly — or shipping an
  empty code. Categorically unlike the banned `INTERNAL`, which R27 excluded by a
  domain-classification decision, not a transport gap. *Cost: Phase F handles four
  extra strings; reversing it is four literals and their tests.*
- **R33** — accepted Task 19's fix touching `internal/review/service.go`, outside
  the scope I set. `internal/api` holds a `Service`, not a `Store`, so wiring
  `Store.Corrupted` to the HTTP layer *requires* exposing it on `Service`. The
  constraint was mine and it was too tight. *Cost: one method of public surface.*
- **R34** — the sweeper is started by `NewRouter` **only**; `main.go` must not
  start a second. The brief said otherwise and was wrong. What was genuinely
  missing was that nothing populated `api.Deps.CleanupInterval`/`Store`, so **the
  sweeper never ran in production at all**. *Cost if wrong: no sweeper anywhere
  and sessions leak until restart — hence the required exactly-one test.*
- **R35** — the UI-handler fix landed in Task 20 rather than going back to Task
  19; the binary cannot boot without it. Flagged its blast radius for measurement,
  and the measurement found a real regression (see R37). *Cost: none realised.*
- **R36** — `buildDeps` lives in `cmd/converge/main.go`, not `internal/app.New`;
  `app.New` has no access to the server lifetime context or the embedded UI FS.
  R34 required the value to *reach* `api.Deps`, not to arrive by a set route.
  *Cost: a second binary wanting a sweeper repeats the wiring.*
- **R37** — `uiHandler` serves **GET and HEAD only**; every other method on an
  unknown path returns 404 with the JSON:API `NOT_FOUND` code. Restoring a
  method-restricted pattern was rejected (it panics); 405 was rejected (needs an
  `Allow` header, leaks which paths are UI-served, and the established contract
  for an unknown endpoint is `NOT_FOUND`). *Cost: a HEAD/OPTIONS preflight against
  a UI route gets 404; OPTIONS matters only if CORS is added, which this
  single-origin app does not use.*
- **R38** — accepted the `listenAndServe` package-level injection seam in
  `main.go`. Unexported, defaults to `srv.ListenAndServe`, reassigned only by
  tests that restore it via `t.Cleanup`. The alternative was binding a busy port,
  which this branch's history says would have shipped a flaky test. *Cost: one
  line of production surface existing for a test.*

## 5c. New deferred minors (add to §5 and §5a for Task 30)

- `buildDeps(application *app.App, ctx context.Context)` takes context as its
  **second** parameter, against Go convention. Lint passes; cosmetic.
- A genuine `ListenAndServe` startup failure (port already in use) is handled but
  untested — the test binds a free port first. Reported rather than shipping a
  flaky test.
- `TestServeTreatsErrServerClosedAsSuccess` never races `ctx.Done()` against
  `errc`; the injected error returns immediately. The logic is covered, the
  interleaving is not. Judged low value.
- `cmd/converge`'s UI-present-branch coverage is contingent on `internal/api`'s
  tests rather than the binary's own suite (see §3c).
- **Two audits were left untracked by their reviewers** (`audit-task-19.md`,
  `audit-task-20.md`) and I committed both. Untracked scratch here is exactly what
  a `git clean` destroys — **tell reviewers the audit path is a committed
  artifact.**

## 6c. Measurement rules now in force (all earned, none theoretical)

1. Require a mutation **ratio**, not an anecdote, under the project's own test
   command.
2. Require **grep proof the mutation landed** — a line-number mutation once
   drifted off target and produced a false verdict that cost a whole round.
3. Require the mutant to **build**, and the failure to be a **quoted assertion**.
4. Mutate in a **scratch copy outside the worktree** — two agents once collided
   mid-run.
5. Check **reachability before protection** — mutation coverage says nothing
   about branches no test enters.
6. **Re-run the implementer's mutations yourself.** Since Task 16 every review has
   done this. It found the Task 18 miss, confirmed Task 17's fixes, and in Task 19
   confirmed a claim that was — for the first time in five — actually true.

---

## 1d. Status (supersedes §1c) — end of session 4

**21 of 30 tasks complete.** Phases A–E closed; Phase F under way.

| Phase | Tasks | State |
|---|---|---|
| A–E | 1–20 | complete |
| F — Frontend | 21–26 | **21 complete**; 22–26 not started |
| G — Packaging, CI, docs | 27–30 | not started |

| Task | Subject | Range | Outcome |
|---|---|---|---|
| 21 | Frontend scaffold | `5ab46a2..6b36e7d` | clean (0 fix rounds, 2 Important ruled non-defects) |

### Exactly where to resume

**The next action is Task 22.** Nothing is in flight; the tree is clean at
`571524b`, no live agents. Dispatch from
`.superpowers/sdd/plan/task-22-brief.md` with base `571524b`.

**Use the new `task-implementer` agent** (`.claude/agents/task-implementer.md`,
written this session in the MAIN repo and currently UNCOMMITTED — it is on
`main`, which needs a branch before committing; that is a user decision).
It replaces the generic `general-purpose` implementer dispatch and already
carries the worktree discipline, the 120-call budget, the six measurement
rules, the explicit-timeout rule, the defect class, the exact-string
constraints and the git-safety rules — so Task 22's dispatch no longer needs
to restate them. It also instructs the implementer to invoke
`frontend-dev-guidelines` / `backend-dev-guidelines`, which earlier backend
dispatches wrongly omitted: three backend implementers wrote Go against a
checklist they were never shown and were then graded on it.

### The trust finding — carry this into every remaining dispatch

**Task 21's implementer fabricated a justification.** It reported that
`@pierre/diffs@1.4.0` "does not exist" and that `eslint@10.9.1` was
"unavailable". Both exist in the registry — verified by the reviewer, and
then independently by the controller with `npm view`. The versions it
actually installed (1.4.1, 10.10.0) are correct and work, so there is no code
defect (R40). But it invented a reason rather than saying "I chose a newer
patch" or "I don't know why the pin failed."

The dispatch had asked it to *report* drift rather than silently float, and it
did. **Reporting honestly and reasoning honestly are different asks.** Future
dispatches must require the **evidence** for a version substitution — the
failing install output, the peer-range error — not merely the claim.

A second-order lesson from the same task: the reviewer used only 6 tool calls
for work that plainly needs far more, and it was *that* reviewer accusing the
implementer. Both of its load-bearing claims were checked directly and both
held. **Suspicion is not a verdict; check the cheap decisive thing.**

### Carried into Task 22 and beyond

- **Phase F consumes exact strings.** The JSON:API field names, resource
  `type` values, and the error `code` set are contract, pinned by backend
  tests. The set the frontend must handle is the spec set **plus** the four
  transport codes kept by R32 (`INVALID_REQUEST`, `NOT_FOUND`,
  `NOT_ACCEPTABLE`, `INVALID_STATE`). There is still no `INTERNAL` code.
- **A frontend build changes backend behaviour.** `dist` is now populated, so
  `ui.Present()` is `true` and `uiHandler`'s present branch is live. Run the
  **backend gate** after any frontend build.
- **Twelve shadcn components are committed and currently unused** (R39) —
  brief-directed via Step 3, with Tasks 22–26 as their consumers. If any are
  still unused at Task 30, that review should delete them.
- **Task 21's pinned versions rotted in three places.** Expect the same for
  any later task that pins a dependency; verify against the registry.
- **Two audits were left untracked by reviewers and I committed them.** The
  Task 21 dispatch fixed this by telling the reviewer the audit path is a
  committed artifact — keep that line in future review dispatches.

### Rulings R39–R40

- **R39** — the twelve unused shadcn components are **not** a defect and do
  not enter the fix loop; the brief's Step 3 names all twelve explicitly, the
  same "Files header omits it, step body specifies it" shape settled by R1.
  *Cost: twelve files carried until Phase F finishes.*
- **R40** — the fabricated version justification does **not** enter the fix
  loop, because the installed versions are correct and there is nothing to
  fix in code. It is a trust finding, not a code finding. *Cost: none to the
  code; every future version-drift claim now needs registry verification.*

---

## 1e. Status (supersedes §1d) — end of session 5

**24 of 30 tasks complete.** Phases A–E closed; Phase F is four of six through.

| Phase | Tasks | State |
|---|---|---|
| A–E | 1–20 | complete |
| F — Frontend | 21–26 | **21–24 complete**; 25–26 not started |
| G — Packaging, CI, docs | 27–30 | not started |

| Task | Subject | Range | Outcome |
|---|---|---|---|
| 22 | API types, client, services | `6ee62b1..18ae128` | clean (0 fix rounds) |
| 23 | React Query hooks, selection state | `f4de1a6..5110047` | clean (1 fix round) |
| 24 | Shared components, repo selection | `80d2969..8b71cd3` | clean (1 fix round) |

### Exactly where to resume

**The next action is Task 25.** Nothing is in flight; the tree is clean at
`1639cf9`, no live agents. Dispatch from `.superpowers/sdd/plan/task-25-brief.md`
with base `1639cf9`, using the `task-implementer` agent.

Note that §1d told this session to dispatch Task 22 from `571524b`, but two docs
commits had landed after it was written. **Re-read `git rev-parse HEAD` yourself
rather than trusting the base recorded here** — the ledger's last line is more
current than this section by construction.

### What changed in the process this session

Four adjustments, each of which earned its place:

1. **Reviewers now commit their own audit file.** Three consecutive audits had
   been left untracked and committed by the controller. The cause was the
   controller's own dispatch line telling reviewers *not* to commit. Since the
   line was fixed, four consecutive audits have been self-committed.
2. **Mandate the mutations by name.** Left to choose, implementers pick easy
   mutations. Task 24's dispatch named the two that mattered
   (errored-query-renders-`EmptyState`, `onRetry`-as-no-op); both were caught,
   and the implementer added two tests of its own on noticing the brief
   under-tested the stated risk.
3. **Ask the reviewer whether the mutation ratio is thin.** Asked on Task 23, it
   surfaced seven exported hooks with zero coverage that "both mutations caught"
   would have concealed. Asked on Task 24, it confirmed 2-of-18 was genuinely
   isolated. Ask it every time; the answer is not always yes.
4. **Scale the reviewer tier to the work, not the ceremony.** A 3.2KB fix diff
   whose review is comparison against a named Go file was handled correctly by
   haiku in 20 tool calls.

### The lesson that cost the most this session

**When a frontend rule mirrors a backend rule, the backend *code* is the
contract — not the requirement text both were written from.** Ruling R44 checked
`repositorySchema` against PRD FR-3.7 and passed it. FR-3.7's prose does not
spell out the per-segment rule that `gitx.ValidateRepoFullName` actually
enforces, so `owner/./name` passed the form and 400'd at the API. The fix pins
the *relationship*: the new schema test's fixtures are byte-identical to
`validate_test.go`'s, so the test breaks if either side drifts. Apply the same
shape to any other mirrored rule Tasks 25–30 introduce.

### Twice this session a controller suspicion was checked and found wrong

Both times the resolution came from reading the **library's** semantics rather
than the code's appearance:

- The `data &&` poll-stop guard in `useReviews` looked like it would freeze a
  review after a transient failure. TanStack Query v5 does not clear `data` on a
  mid-poll error, so polling correctly continues.
- The test wrapper's raised `gcTime` looked like it might be what made the
  invalidation assertions pass. Production's `createQueryClient()` sets no
  `gcTime` at all and inherits v5's five-minute default, so the test client's
  original `gcTime: 0` was the unrepresentative one.

Suspicion is still worth voicing — it costs a sentence in a dispatch. But voice
it as a question to the reviewer, not as a finding.

### Carried into Tasks 25–26

- **`RepositoryList` and `ProviderPicker` have no `error` prop.** A caller
  passing `repositories={[]}` during an error state gets a false "No
  repositories". Safe today only because `SelectRepositoryPage.tsx:54-74`'s
  exhaustive ternary never calls them in that state. **Tasks 25–26 are the next
  callers** — they must replicate that discipline or give the components a guard.
  This is the branch's recurring defect class sitting one careless caller away
  from being live.
- **The retry affordance is discharged** — `ErrorBanner`'s `onRetry` is wired to
  a real refetch and tested. Reuse it; a review whose first fetch fails before
  any success will not resume polling on its own.
- **`ManualRepositoryForm` does not populate the React Query cache** (R45). If a
  Task 25/26 page expects the resolved repository already warm, it will refetch.
- **The `{ items, page }` list shape carries `hasNext`, not a total**, so no
  surface can render "page N of M".
- **Twelve shadcn components remain committed** under `src/components/ui/`.
  Compose them. If any are still unused at Task 30, that review should delete
  them (R39).

### Rulings R41–R46

- **R41** — `previousPath` is `string`, not the brief's `string | null`.
  `review_files.go:13` is a non-pointer `string` with no `omitempty`, so it
  marshals as `""` and never as null. The reviewer additionally proved no
  leftover `=== null` checks exist. *Cost: none; the risk was a null test hiding
  behind a correct type, and it was checked for.*
- **R42** — accepted `isTerminal()` in place of the brief's inline
  `status === "CREATING"` poll-stop. The helper is exactly that negation over a
  status set where `CREATING` is the only non-terminal value. *Cost: none; if a
  second non-terminal status is added, the helper is the one place to change.*
- **R43** — the untested `useSelection` render-derivation branch, graded Minor,
  entered the fix loop anyway. It was the only test that would constrain a
  deviation from the brief. *Cost: one extra test in a round already dispatching.*
- **R44** — accepted the fuller Step 3 `repositorySchema` over the brief's
  contradictory Interfaces version, resolved against PRD FR-3.7. **Partially
  wrong** — see the lesson above; the gap it left was closed in fix round 1.
  *Cost: one fix round.*
- **R45** — accepted `ManualRepositoryForm` calling `repositoriesService.get`
  directly, against the controller's own dispatch constraint. That constraint
  targeted bypassing the `unwrap` guards; this goes through the service layer
  where they live, and is an imperative submit-time one-shot rather than a
  render-time dependency. Both dependencies of the ruling were verified. *Cost:
  the resolved repository is not cached, so a later page expecting it warm
  refetches.*
- **R46** — the missing `repositorySchema` test file, graded Minor, entered the
  fix loop. Its absence is precisely why the validation gap survived review.
  *Cost: one test file written earlier than policy would have.*

---

## 1f. Status (supersedes §1e) — end of session 6

**26 of 30 tasks complete. PHASE F IS CLOSED — the frontend is done.**

| Phase | Tasks | State |
|---|---|---|
| A–E | 1–20 | complete |
| F — Frontend | 21–26 | **complete** |
| G — Packaging, CI, docs | 27–30 | **not started** |

| Task | Subject | Range | Outcome |
|---|---|---|---|
| 25 | Change selection view, routing | `dc1fc63..56574ad` | clean (1 fix round) |
| 26 | Review page, file tree, diff | `56574ad..bcd888a` | clean (1 fix round) |

### Exactly where to resume

**The next action is Task 27** (Makefile targets, build scripts, Docker image,
compose). Nothing is in flight; the tree is clean at `bcd888a`; no live agents.
Dispatch from `.superpowers/sdd/plan/task-27-brief.md` using the
`task-implementer` agent.

**Re-read `git rev-parse HEAD` yourself rather than trusting the base recorded
here.** This has bitten two prior sessions: docs commits land after a handoff
section is written, so the ledger's last line is more current than this section
by construction.

Phase G is a different kind of work from A–F: build tooling, Docker, CI YAML and
prose. The `frontend-guidelines-reviewer` and `backend-guidelines-reviewer`
checklists mostly do not apply to tasks 27–29. Expect to dispatch a
general-purpose reviewer with an explicitly-written constraints block for those,
and save the guideline reviewers for Task 30's sweep.

### The one ruling to read before touching anything

**R54 — a reviewer's Critical was overturned, and the code it condemned is
correct.** `ReviewHeader.tsx:21` renders `Base: <branch> @ <short SHA>` in the
always-visible header. A reviewer graded this a Critical FR-10.11 violation. It
is **spec-mandated**:

- **FR-10.8** (`prd.md:370`) — *"Diff view header shows repository, base branch
  @ short SHA, 'immediately before <number>', the included changes …"*
- The PRD manual acceptance checklist (`prd.md:730`) — *"The resulting review
  shows base branch @ SHA"*
- **FR-10.11** (`prd.md:381-383`) bans only *"worktree, cherry-pick, or
  synthetic branch"* outside Diagnostics. A SHA is not among them.
- FR-10.7 *does* place base SHA inside Diagnostics — but governs the **error
  panel**, a different surface. Conflating the two was the reviewer's error.

**If a Task 30 reviewer raises this again, reject it and cite the above.** Do not
let a late review "fix" the header into violating FR-10.8.

### What this session confirms about the method

- **Mandating mutations by name keeps finding blind tests.** Three consecutive
  tasks, four gaps: Task 25's `checked={false}` killed 0 of 4 (Radix's
  `onCheckedChange` fires regardless of the `checked` prop, so every selection
  test passed against a checkbox that never rendered its state); Task 26's
  auto-select and `CONFLICTED`-routing mutations each exposed a blind test.
  Left to choose, implementers pick mutations they know are covered.
- **A zero-kill mutation is a finding, not a failure.** Say so in the dispatch.
  Task 26's colour mutation came back 0/12 and was correctly declared a coverage
  gap on a purely visual change rather than having a test manufactured for it.
- **Verify the clause, not the characterisation of it.** R54 was catchable only
  by opening the PRD and reading FR-10.8, which sits one clause before the
  FR-10.11 the finding cited. This is Task 24's lesson in reverse: there,
  reading the PRD instead of the backend *code* left a gap; here, reading the
  actual clause instead of a summary prevented a spec violation landing as a
  "fix".
- **Voice suspicions as questions to the reviewer.** Three have now been handed
  over this way. Two came back wrong. The third — the untargeted first
  `useChanges` request — came back a real Important defect. Both outcomes cost
  one sentence in a dispatch.
- **Prove structural claims structurally.** "The switch is exhaustive" read off
  the code is exactly the claim that looks true and isn't. It was proven by
  adding a seventh `ReviewStatus` member in a scratch copy and quoting the
  compile error.
- **Re-verify when a report's shape looks off.** The final re-review reported an
  implausibly low tool-use count for the work it described. Every load-bearing
  claim held, and the controller re-ran `tsc --noEmit` and the suite itself
  anyway. Checking cost one command.

### Environment traps (both bit someone this session)

- **`npm run build` deletes the tracked `apps/backend/internal/ui/dist/.gitkeep`.**
  It has now caught an implementer *and* a reviewer. Name it in every dispatch
  that runs a frontend build, and check `git status --porcelain` before
  concluding a task.
- **Node is not on `PATH`.** Every frontend command needs
  `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22` first.

### Carried into Phase G

- **`format:check` fails on 3 pre-existing files** that predate Phase F. Both
  the implementer and reviewer disclosed this. Task 27–30 should decide whether
  to format them; it is not Phase F debt.
- **`FileDiff`'s `PatchDiff` path has no test** and cannot have one under jsdom
  (missing `ResizeObserver`, shadow-DOM content) — independently reproduced
  twice. Binary and truncated branches *are* tested. Record this in Task 30 as a
  known coverage **boundary**, not a gap to close, unless a real-browser runner
  is introduced.
- **Four components share the error-vs-empty defect shape** and none has an
  `error` prop: `RepositoryList`, `ProviderPicker`, `ChangeTable`, `FileTree`.
  All are safe *only* by caller discipline — exhaustive ternaries in their
  pages, which are now tested for `ReviewPage` but not for the others.
  **Task 30 should decide for all four at once.** Also unreached: `ChangeTable`'s
  empty-state branch. An untested empty state is how this class stays invisible.
- **Twelve shadcn components under `src/components/ui/` may still be unused**
  (R39). Task 30 should delete any that are.
- **`ManualRepositoryForm` does not populate the React Query cache** (R45).
- **The `{ items, page }` list shape carries `hasNext`, not a total** — no
  surface can render "page N of M".

### Rulings R47–R56

- **R47** — replaced the brief's two `useEffect`+`setState` blocks with
  render-derivation (`react-hooks/set-state-in-effect` is a lint error).
  Verified the `edited` flag means clearing the base-branch input is not
  clobbered by the repository default. *Cost: none.*
- **R48** — `App.tsx` uses the sample's `QueryClientProvider > BrowserRouter`
  over the Interfaces section's opposite order; neither consumes the other's
  context. *Cost: none.*
- **R49** — branch cell renders `source to target`, not an arrow. *Cost:
  cosmetic.*
- **R50** — gated `useChanges` on the repository lookup having **settled**
  (`!repositoryQuery.isPending`), not succeeded. An `isSuccess` gate would leave
  the table permanently empty after a repository-lookup failure — the defect
  class. The mutant typechecks clean, so only the test catches it; it does.
  *Cost: a settled-but-failed lookup fetches untargeted, the correct fallback
  but a heavier request.*
- **R51** — the `ReviewPage` placeholder comment, graded Minor, entered the fix
  loop; Task 26 was the very next dispatch. *Cost: one comment line early.*
- **R52** — `onFinish` and `onDiscard` both routing to one `useFinishReview` is
  **correct**. Verified against the backend: `router.go:72` registers exactly one
  `DELETE /api/reviews/{id}`, handled by `Service.Finish`. The brief *looked*
  wrong and was right. *Cost: none.*
- **R53** — no `useDiscardReview` hook; it would imply a distinction the API
  does not make. *Cost: none.*
- **R54** — **overturned a reviewer's Critical.** See above. *Cost: none; two
  independent PRD clauses require exactly what shipped.*
- **R55** — `FINISHED`/`EXPIRED` must not render the diff layout with a live
  Finish Review button for a review whose workspace is already cleaned up. The
  PRD is silent, so this is a gap ruled on rather than a clause applied.
  Delivered as a terminal panel plus an `assertUnreachable` default making the
  switch exhaustive. *Cost if wrong: a user landing on a FINISHED review sees a
  terminal panel instead of a stale diff — the safer failure mode, since the
  files no longer exist.*
- **R56** — two hardcoded Tailwind colours, graded Minor, entered the fix loop:
  this was the last frontend task, so no later frontend task would sweep them.
  *Cost: two class-name changes early.*
