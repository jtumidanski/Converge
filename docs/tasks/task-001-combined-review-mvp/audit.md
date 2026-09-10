# Task 001 — Final whole-branch code review

Branch `task-001-combined-review-mvp` · merge base `e46f93c` · review head `072dbe8`
89 commits · 260 files · +58,374 lines.

This is the consolidated audit for Task 30, the final task of the 30-task plan.
It supersedes nothing: the per-task audits (`audit-task-N.md`) remain the record
for tasks 4–29 individually. This document records what only became visible when
the branch was reviewed **as a whole**.

Three reviewers ran in parallel, each writing a full section file:

| Reviewer | Verdict | Critical | Important | Minor | Section |
|---|---|---|---|---|---|
| plan-adherence | PASS | 0 | 0 | 6 | `audit-final-plan.md` |
| backend-guidelines | FAIL | 0 | 3 | 8 | `audit-final-backend.md` |
| frontend-guidelines | FAIL | 0 | 3 | 4 | `audit-final-frontend.md` |

**Zero Critical findings across all three.** All six Important findings entered a
single consolidated fix wave (see "Fix wave" below).

---

## 1. Verification gate

Run by the controller from a clean tree, in order, before any review dispatch:

| Command | Result |
|---|---|
| `make clean` | exit 0 |
| `make lint` | exit 0 |
| `make test` | exit 0 |
| `make test-integration` | exit 0 |
| `make build` | exit 0 |
| `make docker-build` | exit 0 |

All six green on the first run, no retries. The tree remained clean afterwards —
`make build` does not leave the `internal/ui/dist/.gitkeep` deletion behind (that
trap bites a bare `npm run build`, not the gate).

The backend reviewer independently re-ran `go build ./...`, `go vet ./...`,
`go tool golangci-lint run` (0 issues), `go test -race -count=1 ./...` (19
packages) and `go test -race -tags integration ./internal/review/...` — no data
race reported anywhere.

### Acceptance criteria checkable by automation

| Check | Result |
|---|---|
| Token-leak / cleanup / concurrency integration tests, `-race`, `-tags integration` | pass |
| `converge-cli --version` | prints `dev` (correct for an untagged build), exit 0 |
| `converge-cli build` with a missing required flag | **exit 3** |
| `converge-cli frobnicate` (unknown subcommand) | exit 1 (usage) |

**Correction to the plan's own verification method (Ruling R60).** Plan Task 30
Step 2 specifies `go run ./cmd/converge-cli build; echo "exit=$?"` and expects
`exit=3`. That command cannot ever show 3: `go run` does not propagate a child's
exit code — it exits 1 itself and prints `exit status 3` to stderr. Measured
against a compiled binary (`go build -o /tmp/converge-cli && /tmp/converge-cli
build`), the exit code is 3, exactly as specified. **The CLI is correct; the
plan's check is defective.** The plan text is left as the historical record and
corrected here instead. This was found independently twice — once by the
controller, once by the plan-adherence reviewer (its Minor-4) — from sessions
that could not see each other's work.

---

## 2. Plan adherence — PASS

All 29 implementation tasks DONE. 50 named backend symbols resolved to file:line,
all three file-structure tables walked, **zero TODO/FIXME/stub markers** on the
branch.

Cross-task seams — the blind spot per-task review structurally cannot cover —
all came back clean:

- **Go API JSON tags ↔ TypeScript models:** exact match. The single divergence
  (`previousPath: string`, not `| null`) is documented in `reviewFile.ts` against
  the verified Go contract.
- **Service URLs ↔ router patterns:** all 11 paths match, including `{path...}`
  and the `%2F`-encoded `{repo}` (pinned by `api_test.go:192`).
- **Task 23 hooks ↔ Tasks 24/25/26:** 9 of 11 consumed. `useReviews` /
  `useInvalidateReviews` are orphaned, but plan-faithfully so — no task was given
  a surface for them.
- **Task 26 `React.lazy`:** implemented one level up at `ReviewPage.tsx:32` and
  confirmed *effective*, not merely present — `dist/assets/FileDiff-*.js` is a
  separate 508 KB chunk with 326 on-demand Shiki chunks.

Note: the plan file's checkboxes read 192 unchecked / 0 checked. Completion for
this branch lives in the per-task audits, not in the plan's checkboxes.

---

## 3. Security review — all SEC-* PASS

Verified from source rather than from prior audits' claims:

| Property | Evidence |
|---|---|
| Tokens never reach argv | `gitx/credentials.go:11-24` is the sole credential constructor; both providers append to `Spec.Env` only (`github/client.go:184`, `gitlab/client.go:237`); the only `exec.CommandContext` is `gitx/exec.go:110` |
| Tokens never reach a stored remote URL | `CloneURL` stored verbatim (`github/client.go:176`); no `remote set-url` anywhere |
| Tokens never reach logs | three layers: `config/secret.go:19-25`, `app/app.go:60-108`, `gitx/redact.go:8-21` |
| Tokens never reach `session.json` | `session/record.go:27-45` |
| No token echo in HTTP errors | `api/errors.go:31-65`; `provider/httpjson.go:20` discards non-2xx bodies |

The `0.0.0.0` bind with no authentication is an explicitly documented posture
(`README.md:100-101`, `prd.md:683`), recorded as accepted rather than as a
finding.

---

## 4. Important findings (6) — all entered the fix wave

### Backend

**I-1 — `-c protocol.file.allow=always` on the production git runner.**
`gitx/exec.go:107` places it in the fixed argument prefix of `ExecRunner.Run`,
wired into production at `app/app.go:176`, re-enabling the transport disabled for
CVE-2022-39253 on every git call the server makes. Not a one-line delete: the
integration suite clones `file://` remotes (`testutil/repo.go:141`) *through* the
production runner, which is why it was put there. Exploitability is low today
only because `CredentialEnv` incidentally rejects non-http(s) URLs — an
accidental guard, not a designed one.

**I-2 — unbounded, caller-keyed scan cache.** `provider/github/list.go:41-49`:
`c.scans` has no eviction and no cap and is keyed on the raw `?target=` query
param (`api/changes.go:66-71`), caller-controlled and constrained only by branch
syntax. Unbounded growth plus up to 10 upstream calls per novel key against the
shared token. GitLab has no equivalent cache — the provider seam is bounded on
one side only.

**I-3 — shutdown races against in-flight git work.** `cmd/converge/main.go:82`
drains HTTP handlers only, while `StartBuild` goroutines (`review/service.go:159`)
and the sweeper (`api/router.go:56`) keep calling git after `App.Close()` has
`RemoveAll`d the shared `HOME`/`hooksPath`. No `WaitGroup` in service, app, main
or router.

### Frontend

**F-1 / F-2 — loading/disabled conflation renders a false empty state.**
`useRepositories.ts:18` (`enabled: Boolean(providerId)`) and `useChanges.ts:20`
gate their queries. When a query is *disabled*, React Query returns
`isLoading=false, data=undefined`, so the component renders its empty state
having never issued a request.

- **F-1** (`SelectRepositoryPage.tsx:62-66`): `RepositoryList` shows "This token
  cannot see any repositories on this provider." Transient on every load;
  **permanent, with no error banner, when zero providers are configured**; and
  rendered *simultaneously* with the provider ErrorBanner when `/api/providers`
  fails.
- **F-2** (`SelectChangesPage.tsx:131-136`): `ChangeTable` shows "No merged
  PRs/MRs" on every first paint, before any request is issued.

Proven by four probe tests written against the real MSW harness (4 passed), then
removed. The fix is one expression at each call site, not a new prop.

**F-3 — no `cursor-pointer` anywhere in the codebase.** `grep -rn cursor-pointer
apps/frontend/src` returns zero. FE-15's rule assumes browsers give `<button>` a
pointer cursor — true under Tailwind v3 preflight, **removed in v4**, and this
repo runs `tailwindcss ^4.3.3` whose installed `preflight.css` contains no button
cursor rule (its only `cursor` line is the Safari number-spinner reset). Every
Button, SelectTrigger, Checkbox and FileTree entry fails FE-15's stated hover
acceptance test. A version-bump regression no per-task review could have caught,
because no single task introduced it.

---

## 5. The deferred-minor ledger, and a correction to it (Ruling R61)

Eight minor findings were deferred across Phases F and G *specifically to Task
30*. Both triaging reviewers reached ACCEPT on all eight. **One of those
ACCEPTs was wrong, and the way it was wrong is the most useful result of this
review.**

### R61 — the two reviewers contradicted each other on item 1; the frontend reviewer is correct

- **plan-adherence** triaged item 1 ACCEPT: all four components have exactly one
  caller, and every call site uses an exhaustive `isError ? <ErrorBanner/> :
  <Component/>` ternary, so the empty state is "unreachable by construction".
- **frontend-guidelines** graded it FAIL and demonstrated by execution that it
  *is* reached.

Both are right about different axes, and only one axis is the bug. The ternary is
exhaustive over **error**; the defect is on the **loading/disabled** axis, which
the ternary does not touch. The controller verified both halves directly:
`SelectRepositoryPage.tsx` passes `loading={repositories.isLoading}` with no
disabled term, and `useRepositories.ts:18` sets `enabled: Boolean(providerId)`.

**Why this matters beyond the two-line fix:** for eight sessions this was carried
in the ledger's own words as *"no `error` prop, safe only by caller discipline"* —
an error/empty framing. That framing was wrong, and it was persuasive enough that
a competent reviewer independently re-derived the same wrong ACCEPT today. Acting
on the ledger's wording would have added four `error` props and left both live
bugs in place. **Only running the code broke the loop.**

Two further corrections to the ledger's own text:

- `ChangeTable`'s empty branch is **not** unreached in production, as recorded —
  it fires on every first paint. It was unreached only *by tests*. The test gap
  and the production bug were one fact seen from two sides, which is precisely
  how it survived 26 per-task audits.
- `ProviderPicker` is safe, but **not** by a ternary: `SelectRepositoryPage.tsx:37-52`
  renders the banner and the picker as siblings. The ledger described the
  mechanism incorrectly even though the conclusion held.

### Triage of all eight

| # | Item | Disposition |
|---|---|---|
| 1 | Four components lack an `error` prop | **Re-framed and FIXED** — see R61. The real defect (F-1/F-2) is loading/disabled, not error/empty. `FileTree` and `ProviderPicker` confirmed genuinely safe; untouched. |
| 2 | Twelve shadcn components may be unused | **Measured: 5 of 13**, not twelve — `card`, `dialog`, `scroll-area`, `separator`, `tooltip` (~403 lines, zero import sites). Both reviewers measured the same five independently. Tree-shaken, so non-blocking; deleted as housekeeping, which also disposes of the hardcoded `bg-black/10` at `dialog.tsx:42`. |
| 3 | `ManualRepositoryForm` does not seed the React Query cache | **ACCEPT.** `ManualRepositoryForm.tsx:37` calls the service directly, so `SelectChangesPage` refetches. Worst case one redundant fresh GET — but it made the manual-entry path hit F-2 100% of the time, so fixing F-2 removes its user-visible sting. |
| 4 | `FileDiff`/`PatchDiff` untestable under jsdom | **ACCEPT as a coverage BOUNDARY**, not a gap to close. Narrower than recorded: line 49's truncated case has `binary: false`, so `<PatchDiff>` *does* mount under jsdom without throwing — only its shadow-DOM output is unassertable. `FileDiff.test.tsx:34-41` documents it accurately. |
| 5 | `make build` no longer runs full-repo `go build ./...` | **ACCEPT.** `make lint` runs `go vet ./...` and `make test` compiles everything; full-module `go build ./...` re-run directly and clean. |
| 6 | Pinned GitHub Actions 2–3 majors stale | **ACCEPT.** All verified to exist live; major-version pinning is conventional and bumping carries its own risk. A currency note, not a defect. |
| 7 | README documents 7 of `make help`'s 11 targets | **ACCEPT.** `.DEFAULT_GOAL := help` plus per-target `## ` docs means bare `make` lists all 11; the omissions are CI-only or self-describing. |
| 8 | `make dev` requires bash ≥ 5.1 | **ACCEPT**, settled by Ruling R57 and not reopened. Documentation confirmed at `README.md:30-33` (names the version, the target, the macOS caveat and two workarounds). Rewriting it bash-3.2-portable would reintroduce the always-returns-0 bug R57 exists to prevent. |

---

## 6. Minor findings accepted without change

From plan adherence:

1. PRD §5.2 documents `?search=` on the repositories endpoint; not implemented
   and silently ignored — but FR-3.1 defines the no-search signature, so **the
   PRD contradicts itself** and the code follows the requirement.
2. FR-3.6's "number exact **or prefix**" search is exact-only — deliberate, per
   the documented scan-bypass optimization.
3. FR-14.1's "frontend dist tarball" artifact is never produced — moot, the UI is
   embedded in the binaries.
4. Plan Task 30 Step 2's `go run` exit-code check is defective — see Ruling R60
   in §1.
5. `useReviews` / `useInvalidateReviews` shipped with no production consumer —
   plan-faithful; no task was given a surface for them.
6. **`audit-task-24.md:32`'s premise is too narrowly scoped.** It ruled out a
   duplicate fetch, but `SelectChangesPage.tsx:36` *does* re-query the key the
   manual form declined to seed, so the duplicate fetch does occur. The ruling's
   conclusion still holds; only its supporting evidence was wrong. Corrected here
   rather than by rewriting a historical audit file. Corroborated independently
   by the frontend reviewer's F-6.

From the backend review (8 Minor, all accepted): asymmetric `Finish` guard and
its Get/write TOCTOU (`session/store.go:229-236`); two undocumented sibling edges
against CLAUDE.md's stated layering direction — `session→workspace`
(`session/store.go:14`) and `mirror→provider` (`mirror/cache.go:14`); absolute
`WorkspacePath` disclosed to all clients (`api/reviews.go:45`); decoder error
echoed to the client (`jsonapi/decode.go:58`); no `nosniff`/CSP on the SPA
(`api/ui.go:39-65`); a documented-intentional stdin goroutine leak
(`gitx/exec.go:137-141`); unlocked `Registry.Register` (`provider/registry.go:16-22`).

Guideline-intent checks — immutability, builders, DTO round trip, injected
logger, typed decode, thin handlers, error mapping, no `os.Getenv` outside
config, context propagation, resource cleanup — all PASS with citations. The
literal DOM-*/SUB-* rows do not bind here because the documented GORM/api2go/
logrus deviations hold up under inspection.

---

## 7. Process note

`audit-task-16.md`, `audit-task-17.md` and `audit-task-18.md` do not exist,
though those tasks *were* reviewed — `handoff.md:646,649-651,827-828` discusses
"the Task 18 miss" and confirms Task 17's fixes. The reports simply were not
persisted. This is a gap in the branch's paper trail, not in its code, and is
recorded because Task 30's own reviewer brief claimed audits existed for 4–29.

---

## 8. Fix wave

All six Important findings were sent to a **single** consolidated fix dispatch
(not one fixer per finding), together with the housekeeping deletion of the five
unused shadcn components.

Commits: `072dbe8` → `8b836fc` → `2eef8a8` → `d6fc71f` → `f4c1530`.
41 files, +1,773 / −443.

| Finding | Resolution |
|---|---|
| I-1 | `gitx.Options.AllowFileProtocol`, default false. Production sends no `-c protocol.file.allow` at all (`exec.go:110-120`; sole production construction at `app/app.go:223` omits it). All 22 opt-ins are in `_test.go`. `testutil/repo.go:142` still returns `file:// + r.Bare` — the suite was **not** weakened to make this pass. |
| I-2 | `MaxScanCacheEntries = 64` + `makeScanRoomLocked` (`github/list.go:43-63`), expiry-first then oldest-first, tie broken on key so the choice is total and independent of Go map order. No GitLab cache added. |
| I-3 | One `sync.WaitGroup` on `app.App`, threaded to `review.Deps.Background` and `api.Deps.Background`. `App.Close` drains ≤20 s, then returns `ErrBackgroundDrainTimeout` joined with any cleanup error. `main.go` now logs `Close`'s error instead of discarding it. |
| F-1 / F-2 | Call-site `loading=` expressions only — `loading={repositories.isLoading \|\| !providerId}` and `loading={changes.isLoading \|\| repositoryQuery.isPending}`. **No `error` prop added anywhere.** `FileTree` and `ProviderPicker` untouched. |
| F-3 | `cursor-pointer` at the primitive level: `button.tsx` cva base, `SelectTrigger`, `SelectItem`, `Checkbox`, and FileTree's plain `<button>` (the only interactive element outside `ui/`). |
| Housekeeping | `card`, `dialog`, `scroll-area`, `separator`, `tooltip` deleted (410 lines) after re-verifying zero references. Disposes of the hardcoded `bg-black/10`. |

### The fixes were mutation-tested, not asserted

Every fix was verified by breaking it deliberately in a `/tmp` scratch copy and
confirming the tests caught it (`grep -rn MUTANT` in the tree returns nothing):

| Mutation | Result |
|---|---|
| I-1 always-allow | 1/1 FAIL |
| I-2 eviction disabled | 2/2 FAIL |
| I-3 build untracked | 1/1 FAIL |
| I-3 sweeper untracked | 1/1 FAIL |
| I-3 no drain wait | 2/2 FAIL |
| F-1/F-2 call sites reverted | **3 failed / 13 passed**, then 16 passed after |

That last row is the one that mattered. This defect survived 26 per-task audits
precisely because the empty-state branch was never exercised by a test; a "fix"
shipped with tests that pass against the bug would have reproduced the failure
mode exactly. Reverting the call sites and watching the new tests fail is what
distinguishes a regression test from a test.

### Scoped re-review — PASS

One re-review of the fix range `072dbe8..HEAD` (the single scoped re-review the
process allows after a final-review fix wave). All six findings verdicted
**ADDRESSED**; **no new Critical or Important breakage** in the fix diff.

It independently re-ran everything rather than trusting the fix report:
`go build`, `go vet`, `go tool golangci-lint run` (0 issues), `go test -race
-count=1 ./...` (19 packages, 0 failures), `go test -race -tags integration
./internal/review/...` (10.8 s — this is what proves I-1 did not break `file://`
clones), `npm run lint` (clean), `npm test` (19 files / 97 tests passed),
`npm run build` (✓ 844 ms).

The I-3 review is worth recording in detail, since a `WaitGroup` misuse is the
most dangerous thing in this diff: `Add(1)` is outside the goroutine in both
producers (`review/service.go:171`, `api/router.go:60`), `Done` is `defer`red as
each goroutine's first statement so panic paths still decrement exactly once,
nil-substitution in `NewService`/`NewRouter` means no path calls `Done` on a nil
group, `waitTimeout` uses a fresh channel and timer per call, and `App.Close` is
invoked once per process from a single `defer` — no double-close and no
reuse-before-`Wait`-returned.

Two claims from the fix wave were checked rather than accepted:

- **The F-1/F-2 regression tests genuinely fail against the bug.** The reviewer
  reverted both `loading=` expressions in place and reproduced `3 failed / 13
  passed` with the exact `expected document not to contain element, found <p>This
  token cannot see any repositories on this provider.</p>` failure, then restored
  the files.
- **`backgroundDrainTimeout` as a `var` is a test seam, not a weakening.** It is
  package-private, 20 s, and referenced in exactly three places: the declaration,
  the single `Close` read, and `background_test.go:34-36`, which saves and
  restores it via `t.Cleanup`. Production never reassigns it.

### Concerns raised by the fix wave, adjudicated

**Ruling R62 — the "indefinite skeleton with zero providers" concern is moot.**
The fix wave reported that with no providers configured `SelectRepositoryPage`
now shows skeletons forever rather than a false empty state, and proposed a
dedicated empty state. Checked rather than accepted: `config/config.go:193`
**refuses to start the server** with zero providers ("at least one provider must
be configured", pinned by `config_test.go:78`). So `providers.data` can only be
empty via an API failure, which renders the ErrorBanner branch instead. The state
is unreachable in production.

Recorded as a **conditional backlog item** rather than dismissed: if a future
change ever lets the provider list be legitimately empty — filtering, per-user
permissions — this becomes a real dead-end screen and needs the dedicated state.

**`MaxScanCacheEntries = 64` — accepted.** The findings named no number; 64
distinct target branches per repository sits well above realistic interactive use
while bounding both the memory and the 10-upstream-calls-per-novel-key cost. It
is a named exported constant, so it is tunable rather than buried.

### Deviations the fix wave volunteered

Five, all self-reported without being asked: `SelectItem` also received
`cursor-pointer`; `backgroundDrainTimeout` was made a `var` for the test seam
above, with repo precedent cited (`MinGitVersion`, `listenAndServe`); `main.go`
stopped discarding `Close`'s error; the three reviewer section files were
committed; and `f4c1530` is a comment-only follow-up from its own self-review.
All are in-scope improvements or necessary test seams.

### Remaining non-blocking notes

- A `sync.WaitGroup` contract nit: `Add(1)` in `StartBuild` could in principle
  race a `Close`-side `Wait` at counter zero. Unreachable in production — HTTP
  drains first and `buildCtx` is cancelled before `Close` — but it is why the
  pattern would become fragile if a future caller invoked `Create` after
  shutdown.
- `ensureScanned` holds `c.mu` across network I/O. Pre-existing, predates this
  range, out of scope for the fix wave.

---

## 9. Final gate, post-fix

The full gate was re-run from a clean tree after the fix wave and the scoped
re-review, at `f4c1530`:

| Command | Result |
|---|---|
| `make clean` | exit 0 |
| `make lint` | exit 0 |
| `make test` | exit 0 |
| `make test-integration` | exit 0 |
| `make build` | exit 0 |
| `make docker-build` | exit 0 |

All six green, matching the pre-review gate in §1. Tree clean afterwards.

**Branch verdict: ready for integration.** Zero Critical findings; all six
Important findings fixed, mutation-tested and independently re-verified; all
remaining Minor findings triaged ACCEPT with reasons recorded above.
