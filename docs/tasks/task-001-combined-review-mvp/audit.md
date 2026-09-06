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
unused shadcn components. Scope, exact locations and required verification are
recorded in `.superpowers/sdd/plan/task-30-fix-findings.md`.

Results are appended below.
