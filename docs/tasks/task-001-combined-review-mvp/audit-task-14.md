# Backend Audit — Task 14 (cherry-pick applicator)

- **Range:** `d0c3407..e95bab5` (1 commit)
- **Files:** `apps/backend/internal/review/apply.go` (new, 217 lines), `apps/backend/internal/review/apply_test.go` (new, 184 lines)
- **Date:** 2026-09-04
- **Build:** PASS (implementer evidence; `go test -race -count=1 ./internal/review/` re-run by me → `ok ... 3.354s`)
- **Verdict 1 — Spec compliance vs task brief:** ✅ with caveats (all required symbols/behaviour present; both declared deviations verified correct against real git; one PRD FR-6.5 gap inherited from the brief)
- **Verdict 2 — Go guidelines (DOM/SUB/SEC):** NEEDS-WORK (1 Important defect of the plan's recurring class, 1 Important test-quality defect, 1 Important spec gap)

Findings: **Critical 0 / Important 3 / Minor 3**

---

## PRIORITY QUESTION — is the empty-outcome deviation load-bearing? YES. Implementer is correct.

### Experiment 1 — does `--empty=keep` advance HEAD on a no-op pick?

Real git 2.55.0, scripted repo (two independent squash commits off a shared base with
identical net content, both cherry-picked onto a fresh branch at base):

```
$ git cherry-pick --empty=keep $A          # first, real content
[review 347eaf4] squash a
 1 file changed, 1 insertion(+)
pick1 exit=0
$ BEFORE=$(git rev-parse HEAD)
$ git cherry-pick --empty=keep $A2         # second, identical content -> no-op
[review d23e909] squash a again
pick2 exit=0
$ AFTER=$(git rev-parse HEAD)
BEFORE=347eaf43c18706556c871a9b44435f78cc71d712
AFTER =d23e909d5fae805ad1aa39f0ae9b384d8aa9bc96
HEAD-MOVED: brief check would NEVER fire -> misclassified as applied
$ git diff --quiet $BEFORE HEAD; echo $?
0
$ git log --oneline $BASE..HEAD
d23e909 squash a again        <-- empty commit, created anyway
347eaf4 squash a
```

`--empty=keep` creates an empty commit object and advances `HEAD`. The brief's
`after == before` test therefore **can never be true**, so `OutcomeEmpty` would never
be returned and **every** empty pick would be reported as `OutcomeApplied` — a textbook
instance of the Priority-2 defect class. The tree comparison
(`apply.go:143-152`) is the correct discriminator: `git diff --quiet <before> HEAD`
exits 0 on the no-op pick, as shown above. **Deviation accepted.**

### Experiment 2 — did the brief's empty-pick test setup really fail at setup time?

Sequential-through-`main` setup exactly as the brief writes it:

```
$ git checkout main; git merge --squash feat/a; git commit -m "squash a"   # exit 0
$ git checkout -b feat/a2; echo a > a.txt; git add -A; git commit -m "a again"
On branch feat/a2
nothing to commit, working tree clean
commit a-again exit=1
$ git checkout main; git merge --squash feat/a2; git commit -m "squash a again"
Already up to date. (nothing to squash)
nothing to commit, working tree clean
SECOND SQUASH COMMIT EXIT=1
```

Confirmed — and it fails one step *earlier* than the implementer reported (the
`feat/a2` content commit is itself a no-op, because `main` already carries `a.txt`).
`testutil.Squash` (`internal/testutil/repo.go:101`) has no `--allow-empty`, so setup
aborts. **Amendment justified.**

### Experiment 3 — did the brief's conflict test setup really poison the merge-base?

```
=== BRIEF CONFLICT SETUP (sequential) ===
--- first Apply: cherry-pick B onto base ---
CONFLICT (content): Merge conflict in shared.txt
FIRST PICK EXIT=1
UU shared.txt
--- second Apply: cherry-pick A ---
error: Cherry-picking is not possible because you have unmerged files.
fatal: cherry-pick failed
SECOND PICK EXIT=128
CHERRY_PICK_HEAD=343eeb8(=bSHA)  A=36ffbfa  B=343eeb8
```

Confirmed exactly as reported: the *first* `Apply` conflicts (which the brief's test
does not catch, since conflicts are returned as `(result, nil)`), and the second call
exits 128 with a stale `CHERRY_PICK_HEAD`, so `res.Commit != aSHA`. **Amendment justified.**

### Do the amended tests still pin the intended properties?

- **Conflict test (`apply_test.go:132-184`): yes.** It still asserts `OutcomeConflict`,
  exact `ConflictingPaths == ["shared.txt"]`, `Commit == aSHA`, a worktree left with a
  `U` entry, and a successful `Abort`. Independent staging branches off `base`
  (`apply_test.go:148-159`) make the scenario "two independent colliding PRs", which is
  what the brief's prose described. Not weakened.
- **Empty test (`apply_test.go:86-130`): NO — it was not strengthened to match the
  deviation, and it does not pin the empty outcome at all.** See Important-2.

---

## PRIORITY 2 — enumeration of every error-return and git-invocation site

| # | Site | Git command | Can a real failure be misreported as benign data? |
|---|------|-------------|---|
| 1 | `apply.go:62-64` | — | No. Empty `LandingSHAs` → error (also prevents a `shas[0]` panic in `currentPickSHA`). |
| 2 | `apply.go:65-69` | — | No. `gitx.ValidateSHA` failure → error. |
| 3 | `apply.go:77-80` → `head` (`:154-160`) | `rev-parse HEAD` | No. Error wrapped and returned; no `""` fallback. |
| 4 | `apply.go:82-91` | `cherry-pick [--empty=keep] [-m 1] <shas...>` | No for non-`ExitError` (process start / timeout) — returned as an error at `:90`. |
| 5 | **`apply.go:93-108`** | `diff --name-only --diff-filter=U -z` | **YES — Important-1.** Classification keys on `errors.As(*gitx.ExitError)`, i.e. *any* non-zero exit, and never on the exit code. Exit 128 (a hard precondition failure) with pre-existing unmerged paths is returned as `OutcomeConflict, nil`. |
| 6 | `apply.go:97-103` | — | No. Non-zero exit with an empty unmerged list → error, explicitly refusing to guess. Good pattern. |
| 7 | **`apply.go:107` → `currentPickSHA` (`:209-217`)** | `rev-parse --verify --quiet CHERRY_PICK_HEAD` | **YES (Minor-1).** Error is discarded and `shas[0]` is returned as if authoritative. In experiment 3 this returned a SHA belonging to a *different* change. |
| 8 | `apply.go:117-120` → `treeUnchanged` (`:143-152`) | `diff --quiet <before> HEAD` | No. Exit 0 / exit 1 / other are three distinct branches; `gitx.IsExit(err, 1)` at `:148` and a real error at `:151`. This is the correct shape and the model the other sites should follow. |
| 9 | `apply.go:125-127` → `annotate` (`:166-183`) | `log -1 --format=%B`, `commit --amend --allow-empty --no-verify -F -` | No. Both errors wrapped and returned. |
| 10 | `apply.go:190-203` `conflictingPaths` | `diff --name-only --diff-filter=U -z` | No. Error returned; `nil, nil` on empty output is safe because the sole caller treats an empty list as an error (site 6). |
| 11 | `apply.go:132-137` `Abort` | `cherry-pick --abort` | No. Error wrapped. |

### Important-1 (defect class) — non-zero exit ≠ conflict: `apply.go:83-108`

Proved by running a probe test against the real applicator (temp copy, deleted after):

```
r1 = {Outcome:conflict ConflictingPaths:[shared.txt] Commit:4a27c00(=aSHA)} err=<nil>
r2 = {Outcome:conflict ConflictingPaths:[shared.txt] Commit:4a27c00(=aSHA)} err=<nil>
DEFECT: hard git failure (exit 128) reported as OutcomeConflict,
        Commit=4a27c00 (aSHA=4a27c00 bSHA=762c3a1)
```

`r2` is `Apply(change #3, bSHA)` on a worktree left conflicted by `r1`. git printed
`fatal: cherry-pick failed` and exited 128 without attempting the pick at all; the
applicator nevertheless reported a **successful classification** of `OutcomeConflict`,
carrying the *previous* change's conflicting paths and the *previous* change's
`CHERRY_PICK_HEAD` — i.e. change #3 is recorded as conflicting on a SHA that is not
even one of its landing commits. Downstream this becomes a `CONFLICTED` session
blaming the wrong PR/MR (FR-6.6/FR-6.7 record "the commit SHA being applied").

The fix is one line of the discipline already used at `apply.go:148`: require
`gitx.IsExit(runErr, 1)` before entering the conflict branch (`gitx.IsExit` exists at
`internal/gitx/spec.go:55`), and return an error for any other exit code. Note this
also removes the need for the `len(paths) == 0` heuristic at `:97`.

---

## PRIORITY 3 — test discrimination

| Test | Discriminating? | Evidence |
|---|---|---|
| `TestApplyMergeSquashRebase` (`apply_test.go:33-84`) | **Yes** | Asserts per-strategy content (`a.txt`/`b.txt`/`c.txt`/`c2.txt`), the exact trailer (`:67`), and `rev-list base..HEAD == 4` (`:80`). Deleting `-m 1` (`apply.go:72-74`) makes git refuse the merge pick; deleting `annotate` fails `:67`. |
| `TestApplyEmptyPickIsNotAnError` (`:86-130`) | **NO — Important-2** | See below. |
| `TestApplyConflictReportsPaths` (`:132-184`) | **Mostly** | Asserts outcome, exact path, `Commit`, `U` in status, and `Abort`. Nulling `conflictingPaths` turns `Apply` into an error → test fails. **But** the `CHERRY_PICK_HEAD` lookup is untested (Minor-1). |

### Important-2 — the empty test does not test the empty outcome: `apply_test.go:124`

```go
if res.Outcome != OutcomeApplied && res.Outcome != OutcomeEmpty {
```

The assertion accepts either outcome, so nothing in the suite pins the load-bearing
deviation. Verified by mutation: in a scratch copy I reverted `apply.go:117-123` to the
brief's `after == before` HEAD-SHA check (the logic proved wrong in Experiment 1) and ran:

```
=== RUN   TestApplyEmptyPickIsNotAnError
--- PASS: TestApplyEmptyPickIsNotAnError (0.13s)
```

The test passes with the bug reinstated. This directly contradicts the implementer
report's claim that the deviation "is now covered by a passing real-git test that would
fail loudly if the tree-comparison approach were ever wrong" — and no test anywhere in
the diff ever asserts `res.Outcome == OutcomeEmpty`. Change `:124` to
`if res.Outcome != OutcomeEmpty { t.Fatalf(...) }`.

### Minor-1 — `currentPickSHA`'s `CHERRY_PICK_HEAD` branch is untested: `apply.go:209-217`

Mutation: replacing the whole body with `return shas[0]` leaves
`TestApplyConflictReportsPaths` green (`ok ... 0.135s`), because the conflicting change
in that test is single-SHA, so `shas[0] == aSHA` unconditionally. A multi-SHA rebase
change that conflicts on its *second* commit would discriminate it. Combined with the
swallowed error at `:211`, this is the defect class in miniature: a plausible wrong SHA
returned with no signal.

---

## Verdict 1 detail — spec compliance vs the task brief

| Requirement (brief) | Status | Evidence |
|---|---|---|
| `Outcome` + three constants, exact string values | PASS | `apply.go:16-22` |
| `ApplyResult{Outcome, ConflictingPaths, Commit}` | PASS | `apply.go:25-29` |
| `ChangeApplicator` interface, exact signature | PASS | `apply.go:32-34` |
| `CherryPickApplicator` + `NewCherryPickApplicator(gitx.Runner, *slog.Logger)` | PASS | `apply.go:49-57` |
| merge → `cherry-pick -m 1 --empty=keep <sha>` | PASS | `apply.go:71-75` (flag order `--empty=keep -m 1`; semantically identical) |
| squash → `cherry-pick --empty=keep <sha>` | PASS | `apply.go:71-75` |
| rebase → `cherry-pick --empty=keep <sha1..shaN>`, one invocation, original order | PASS | `apply.go:75`; exercised at `apply_test.go:76` |
| Trailer `<subject>\n\nConverge-Change: #<n>\nConverge-Source: <sha>` via `commit --amend --allow-empty --no-verify -F -` | PASS vs brief | `apply.go:172-178`; asserted `apply_test.go:67`. See Important-3 for the PRD conflict. |
| Conflict path listing via `diff --name-only --diff-filter=U -z`, worktree left conflicted | PASS | `apply.go:191`; `apply_test.go:178` |
| `Abort(ctx, repoDir)` → `cherry-pick --abort` | PASS | `apply.go:132-137`; exercised `apply_test.go:181` |
| Three brief test cases present | PASS | `apply_test.go:33, 86, 132` |
| Nothing extra (YAGNI) | PASS | Only additions beyond the brief are the `len(shas)==0` guard (`:62`) and the dropped `errorsAs` wrapper. Both defensible; no speculative API. |
| Declared deviation #4 (tree comparison) | ACCEPTED | Experiment 1 |
| Declared deviation #5 (test setups) | ACCEPTED | Experiments 2 and 3 |

### Important-3 — trailer does not satisfy PRD FR-6.5 (brief-authorized gap)

PRD FR-6.5 (`prd.md:279-281`): "**Each** synthetic commit message records the change
number **and provider**". design.md:429-430 spells this out as
`Converge-Change: <provider-id>#<number>` written to "the message of **each** new commit".
The shipped code writes `Converge-Change: #%d` with no provider (`apply.go:172`) and
amends only the tip commit (`apply.go:45-48`), so an N-commit rebase pick leaves N-1
synthetic commits untrailered, and an empty pick leaves its `--empty=keep` commit
untrailered as well (`apply.go:121-122` returns before `annotate`).

The implementer followed the brief exactly here — the brief explicitly ordered both
choices, reasoning that `session.ResolvedChange` carries no provider id and that the
provider is recorded once in `session.json`. Since the PRD outranks the design and the
brief, this needs a plan-owner ruling rather than a silent pass: either thread the
provider into `ResolvedChange`/`Apply`, or amend the PRD.

---

## Guidelines checklist (DOM-* / SUB-* / SEC-*)

`internal/review` has no `model.go`, `resource.go`, `provider.go` or `administrator.go`
(`find internal -name model.go -o -name resource.go ...` returns only
`internal/session/model.go`, `internal/provider/{provider,model}.go`). Per the plan's
recorded deviations (`plan.md`, Global Constraints: "no GORM/entity/migrations;
`session.json` DTO plays the entity role; `*slog.Logger` injected via constructors;
plain functions instead of lazy `model.Provider[T]`"), the DDD/JSON:API domain layout is
explicitly out of scope for this package. DOM-01…DOM-19 and SUB-01…SUB-04 are therefore
**N/A** — this is a domain-logic support package, not a REST domain package. Applicable
items:

| ID | Check | Status | Evidence |
|---|---|---|---|
| DOM-06 (adapted) | Constructor takes an injected logger, not a package global | PASS | `apply.go:55-57`; no `slog.Default()` / `logrus.StandardLogger()` anywhere in the file |
| DOM-11 | No `os.Getenv()` | PASS | Zero matches in `apply.go` |
| DOM-19 | Test structure | WARN | Three scenario tests, not table-driven. Acceptable for real-git scenarios (matches the brief verbatim), but see Important-2 for the assertion weakness. |
| SEC-04 | No hardcoded secrets / no credential exposure | PASS | Every git call is an argument slice through `gitx.Runner` (`apply.go:82,133,144,155,167,179,191,210`); no shell, no env manipulation, no URLs, no tokens. |
| SEC (project) | No raw git stderr in error strings | PASS | Errors wrap `*gitx.ExitError`, whose `Error()` is `"git %s exited with code %d"` (`internal/gitx/spec.go:50-52`) — category and exit code only, no stderr bytes. |
| SEC (project) | UI vocabulary constraint ("cherry-pick" only in Diagnostics) | PASS | The words "cherry-pick" appear only in Go error strings (`apply.go:102,134`) and comments; user-facing text comes from `internal/review/messages.go`, whose git path is the generic `MsgGitFailure()` (`messages.go:81-83`). |
| Input validation | SHAs validated before reaching git argv | PASS | `apply.go:65-69` via `gitx.ValidateSHA` (`internal/gitx/validate.go:23`) |

---

## Summary

### Blocking (Critical)
None.

### Important (enter the fix loop)
- **Important-1 — `apply.go:83-108`:** cherry-pick classification keys on "any non-zero exit", so a hard git failure (exit 128, e.g. `fatal: cherry-pick failed` on an already-unmerged worktree) is returned as `OutcomeConflict, nil` carrying the *previous* change's paths and SHA. Proved with a live probe. Gate the conflict branch on `gitx.IsExit(runErr, 1)`.
- **Important-2 — `apply_test.go:124`:** `TestApplyEmptyPickIsNotAnError` accepts `OutcomeApplied` *or* `OutcomeEmpty`, so it passes with the brief's proven-wrong HEAD-SHA logic reinstated (mutation-verified). No test in the diff ever asserts `OutcomeEmpty`; the report's claim of coverage for the load-bearing deviation is unsupported.
- **Important-3 — `apply.go:172` and `apply.go:45-48`:** the Converge trailer omits the provider id and is applied only to the tip commit, against PRD FR-6.5 ("each synthetic commit ... change number and provider"). Brief-authorized; needs a plan-owner ruling.

### Minor
- **Minor-1 — `apply.go:209-217`:** `currentPickSHA` discards the `rev-parse CHERRY_PICK_HEAD` error and returns `shas[0]` as if authoritative; the `CHERRY_PICK_HEAD` branch is untested (mutation to `return shas[0]` leaves the conflict test green).
- **Minor-2 — `apply.go:117-123`:** the empty determination is purely tree-based, so a multi-SHA rebase pick whose commits net to zero tree change is reported `OutcomeEmpty` while real, untrailered commits remain on the branch and `HEAD` has moved. Worth a doc comment or an explicit decision.
- **Minor-3 — `apply.go:51`:** the `log *slog.Logger` field is stored but never used; the applicator emits no logging at any decision point (conflict, empty, amend), which is thin for a `GIT_FAILURE`/Diagnostics story.
