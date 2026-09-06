# Final Whole-Branch Plan Adherence Audit — task-001-combined-review-mvp

**Reviewer:** plan-adherence-reviewer (Task 30 of 30, whole-branch sweep)
**Plan:** `docs/tasks/task-001-combined-review-mvp/plan.md` (529.8K, 30 tasks, Phases A–G)
**PRD:** `docs/tasks/task-001-combined-review-mvp/prd.md`
**Branch:** `task-001-combined-review-mvp` @ `072dbe8` — 89 commits, 81 carrying the `(task-001)` scope
**Merge base:** `e46f93c` (main)
**Audit date:** 2026-09-06

**VERDICT: PASS** — Critical 0, Important 0, Minor 6.

---

## Executive Summary

Every deliverable named in Tasks 1–29's `**Files:**` and `**Interfaces:**` blocks exists at
HEAD. I resolved 50 named backend symbols to `file:line`, walked the full file-structure
tables for backend / frontend / root, and cross-checked the four cross-task seams that
per-task reviews are structurally blind to (Go API attribute names ↔ TypeScript models,
service URL paths ↔ router patterns, Task 23 hooks ↔ Task 24–26 consumers, Task 4
`GitProvider` ↔ Tasks 5/6/fake/13). Nothing was silently skipped, stubbed, or deferred
without a ruling. There are zero TODO/FIXME/XXX/HACK markers anywhere in shipped backend
or frontend code.

Six Minor findings remain, none merge-blocking. Four are PRD-text-vs-shipped-code drift
that the plan's Traceability table papers over by mapping at group granularity; one is a
defect in the plan's *own* Task 30 verification command; one is a correction to a premise
in an already-recorded ruling (the ruling's conclusion still holds).

All eight deferred-minor ledger items triaged: **8 ACCEPT, 0 FIX BEFORE MERGE.**

---

## 1. Task-by-Task Deliverable Verification (Tasks 1–29)

Plan checkbox state: **192 unchecked, 0 checked.** The plan file was never annotated
during execution. This is a bookkeeping artifact, not a completion signal — the per-task
audit files (`audit-task-4.md` … `audit-task-29.md`) and `handoff.md` are the actual
completion record, and they are complete. Do not read `- [ ]` as "not done".

### Phase A — Foundations

| Task | Status | Evidence |
|---|---|---|
| 1 Module scaffold, buildinfo, lint, Makefile | DONE | `apps/backend/go.mod`, `apps/backend/.golangci.yml`, `internal/buildinfo/buildinfo.go`, `internal/ui/embed.go`, `internal/ui/dist/.gitkeep`, `internal/ui/ui_test.go`, `Makefile`, `tools/version.sh`. `.gitignore:45-46` carries the `dist/*` + `!.gitkeep` pair; `.gitignore:49` carries `/dist/`. |
| 2 Configuration | DONE | `internal/config/{config,secret,errors}.go` + 2 test files. `NewSecret` `secret.go:11`; `Config.Secrets()` `config.go:68`; `Load(env)` `config.go:79`. All 16 env keys implemented (see §4). |
| 3 gitx | DONE | All 7 sources + 5 test files present. `NewExecRunner` `exec.go:40`; `CredentialEnv` `credentials.go:11`; `Redact` `redact.go:11`; `ValidateBranch` `validate.go:56`; `ValidateChangeNumbers` `validate.go:75`; `ValidatePathArg` `validate.go:99`. |

### Phase B — Providers

| Task | Status | Evidence |
|---|---|---|
| 4 Provider model/interface/registry/fake | DONE | `page.go`, `errors.go`, `model.go`, `builder.go`, `provider.go`, `registry.go`, `httpjson.go` + 4 tests + `fake/fake.go`. `DoJSON` `httpjson.go:12`; `ParseSearchNumber` `page.go:41`; `NewRegistry` `registry.go:13`. |
| 5 GitHub provider | DONE | `github/{client,mapping,list}.go` + `client_test.go` + `list_test.go` (bonus) + all 6 planned `testdata/*.json`. `MaxScanPages` `list.go:40`; `ScanCacheTTL` `list.go:46`; `MaxPRCommits` `client.go:27`. |
| 6 GitLab provider | DONE | `gitlab/{client,mapping}.go` + `client_test.go` + all 5 planned `testdata/*.json`. |

### Phase C — Git layers

| Task | Status | Evidence |
|---|---|---|
| 7 Scripted-repo harness | DONE | `internal/testutil/repo.go` + `repo_test.go`. `Rebase` `repo.go:119`; `BranchCommits` `repo.go:109`. |
| 8 Mirror cache + object reader | DONE | `mirror/{cache,objects}.go` + 2 tests. `FetchSHA` `cache.go:90`; `Objects` `objects.go:31`. |
| 9 Workspace manager | DONE | `workspace/manager.go` + test. `Cleanup` `manager.go:126`; `ErrOutsideRoot` asserted `manager_test.go:152`. |

### Phase D — Review domain

| Task | Status | Evidence |
|---|---|---|
| 10 Diff production/parsing | DONE | `diff/{diff,parse}.go` + 2 tests. `MaxFileDiffBytes` `diff.go:25`; `WriteCombined` `diff.go:60`; `Summarize` `diff.go:88`; `FileContent` `diff.go:143`. |
| 11 Session model/store/sweep | DONE | All 8 sources + 3 tests. `codes.go` carries all 11 error codes, all 6 statuses, 3 strategies, 5 stage constants **verbatim** against Global Constraints — verified string-by-string. `NewID` `id.go:10`; `ToRecord` `record.go:62`; `FromRecord` `record.go:98`; `NewStore` `store.go:58`. |
| 12 Input validation / messages / landing | DONE | `review/{input,messages,landing}.go` + `landing_test.go` + `input_test.go` (+ bonus `landing_realgit_test.go`). `ResolveLanding` `landing.go:37`. |
| 13 Resolve pipeline | DONE | `review/resolve.go` + test. `resolveConcurrency` `resolve.go:17`; base = first parent of earliest landing commit `resolve.go:176-177`, asserted `resolve_test.go:118`. |
| 14 Cherry-pick applicator | DONE | `review/apply.go` + test. `NewCherryPickApplicator` `apply.go:61`; `Abort` exercised `apply_test.go:258`. |
| 15 Review service + cleaner | DONE | `review/{cleaner,service}.go` + `service_test.go`. `NewCleaner` `cleaner.go:23`; `NewService` `service.go:56`; `StartBuild`/`ErrNotReady` exercised `service_test.go:324,282`. |
| 16 Integration tests (FR-12.2) | DONE | `review/integration_test.go` + `harness_test.go`. 10 `TestIntegration*` functions — see §3 for the FR-12.2 bullet map. `go vet -tags integration ./...` clean. |
| 17 App wiring + CLI | DONE | `internal/app/app.go` + test; `cmd/converge-cli/main.go` + test. `MinGitVersion` `app.go:27`; `redactingHandler` `app.go:50`; `NewLogger` `app.go:126`; `checkGitVersion` `app.go:138`. CLI exit codes verified empirically — see §5. |

### Phase E — HTTP API

| Task | Status | Evidence |
|---|---|---|
| 18 JSON:API encode/decode | DONE | `jsonapi/{document,decode,errors}.go` + test. `Decode[T]` `decode.go:31`; `WriteErrors` `errors.go:22`. |
| 19 Handlers/router/middleware/UI | DONE | All 10 sources + `api_test.go` + `ui_test.go`. All 13 planned routes registered `router.go:59-90` — verified one-for-one against the plan's route list, no additions, no omissions. `writeDomainError` `errors.go:21`. |
| 20 Server binary | DONE | `cmd/converge/main.go` + test. `shutdownTimeout = 15 * time.Second` `main.go:23`; `signal.NotifyContext(SIGINT, SIGTERM)` `main.go:56`; `srv.Shutdown` `main.go:114`. **Placement deviation (accepted):** the sweeper goroutine is started in `internal/api/router.go:55` rather than in `main.go`; the rationale is documented at `router.go:32`. Same runtime effect for the server, and correctly excludes the one-shot CLI. |

### Phase F — Frontend

| Task | Status | Evidence |
|---|---|---|
| 21 Scaffold/tooling | DONE | All 19 planned files present, plus `vitest.config.ts` split out. `apps/backend/internal/ui/dist/` contains a live build (`index.html` + 326 asset chunks) proving `build.outDir` resolves as planned. |
| 22 API types/client/services | DONE | All 5 model files, `types/api/jsonapi.ts`, `lib/api/{client,errors}.ts`, all 5 service files, both planned tests, `src/test/server.ts`. |
| 23 Query hooks + selection | DONE | `lib/query-client.ts`, all 4 hook modules, `useSelection.ts`, `src/test/render.tsx`, planned tests (plus 3 bonus hook test files). |
| 24 Shared components + repo selection | DONE (1 accepted deviation) | All 4 `common/` components, `ProviderPicker`, `RepositoryList`, `ManualRepositoryForm`, `lib/schemas/repository.ts`, `SelectRepositoryPage`, both planned tests. Deviation: `ManualRepositoryForm` calls `repositoriesService.get` imperatively instead of the planned `useRepository` — **already ruled ACCEPTED** at `audit-task-24.md:19`. See Minor-6 for a correction to that ruling's premise. |
| 25 Change selection + routing | DONE | `ChangeTable`, `ChangeSearch`, `SelectionBar`, `SelectChangesPage`, `routes.tsx`, planned test (+ bonus `SelectionBar.test.tsx`). |
| 26 Review page | DONE | All 5 review components, all 4 planned tests (+ bonus `FileDiff.test.tsx`). **`React.lazy` requirement satisfied** — the plan wrote it as a property of `FileDiff`; it is implemented one level up at `pages/ReviewPage.tsx:32` (`const FileDiff = lazy(...)`) with `Suspense` at `ReviewPage.tsx:172`. Confirmed effective, not merely present: `apps/backend/internal/ui/dist/assets/FileDiff-D6oY_v4D.js` is a separate 508 KB chunk and Shiki grammars are 300+ on-demand chunks, exactly the design §12.2 mitigation for the 7.4 MB library. |

### Phase G — Packaging, CI, docs

| Task | Status | Evidence |
|---|---|---|
| 27 Makefile/scripts/Docker/compose | DONE | `tools/docker-push.sh`, `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `.env.example`, rewritten `tools/build-backend.sh`. All 11 planned make targets present and `.PHONY`-declared (`Makefile:14`): help, version, lint, test, test-integration, build, docker-build, docker-push, release-github, dev, clean. Dockerfile is 3-stage (node:22-alpine → golang:1.27-alpine → alpine:3.22), non-root uid 10001, `/data` chowned, tini entrypoint, HEALTHCHECK on `/healthz` — FR-13.1/13.3 satisfied. |
| 28 CI pipelines | DONE | `.github/workflows/ci.yml` (jobs `validate`, `publish`) and `.gitlab-ci.yml` (stages `validate`, `build`, `publish`). No registry hostname or credential committed; `tools/docker-push.sh:10-20` resolves `IMAGE_REPOSITORY` → `GITHUB_REPOSITORY` → `CI_REGISTRY_IMAGE` and fails loudly otherwise — FR-14.3 satisfied. FR-14.5 satisfied: workflow files contain only checkout, toolchain setup, caching, registry login, and `make` invocations. |
| 29 Documentation | DONE | `docs/manual-checklist.md` created; `README.md` and `CLAUDE.md` rewritten. FR-12.4 fully covered: the checklist has GitHub squash/merge-commit/rebase, GitLab squash/merge-commit/fast-forward/self-hosted, error+lifecycle cases, and a 4-point token-leak sweep. The GitHub scan bound is documented at `README.md:161` ("at most 1,000 closed PRs per repository, 10 pages"), matching `MaxScanPages = 10` at `list.go:40`. |

**Skipped without approval: 0. Partial: 0. Deferred without ruling: 0.**

---

## 2. Cross-Task Seam Verification

These are the joints a per-task review cannot see, because each side was reviewed in a
different session against a different brief.

**Seam A — Task 19 API attributes ↔ Task 22 TypeScript models. CLEAN.**
Compared every JSON tag against every TS field:
`providers.go:11-13` ↔ `types/models/provider.ts`;
`repositories.go:12-17` ↔ `repository.ts`;
`changes.go:12-22` ↔ `change.ts`;
`reviews.go:32-48` + `includedChange` `reviews.go:23-30` ↔ `review.ts`;
`review_files.go:12-23` ↔ `reviewFile.ts`.
All names, casings and nullability match. The one intentional divergence is *documented at
the point of divergence*: `reviewFile.ts` types `previousPath` as `string` (not
`string | null`) with a comment citing the non-pointer, non-`omitempty` Go field in
`review_files.go` — the frontend was written against the verified Go contract rather than
the brief. This is the correct outcome and exactly the class of bug this seam check exists
to find.

**Seam B — Task 19 router patterns ↔ Task 22 service URLs. CLEAN.**
All 11 `/api/**` paths built in `services/api/*.ts` match `router.go:59-75` exactly,
including the `{path...}` wildcard (`reviews.ts:33` splits on `/` and encodes per segment,
preserving separators) and the `%2F`-encoded `{repo}` segment (`repositories.ts:44`,
`changes.ts:24`). The Go side of that encoding is deliberately pinned by
`api_test.go:192,202,212` with a `router.go:64-66` comment naming the test.

**Seam C — Task 23 hooks ↔ Tasks 24/25/26 consumers. CLEAN, 2 orphans.**
9 of 11 exported hooks have a production consumer. `useReviews` and `useInvalidateReviews`
have none — Task 23 produced them as planned, but the plan never specified a review-list
page for them to serve. Planned-and-shipped, merely unconsumed. Not a gap. (See Minor-5.)

**Seam D — Task 4 `GitProvider` ↔ Tasks 5/6/fake and Task 13. CLEAN.**
Both real clients and the fake satisfy the interface (compile-verified by
`CGO_ENABLED=0 go build ./...` exit 0), and `internal/review/resolve.go` consumes it
through the interface only.

**Seam E — Task 1 `ui.FS`/`ui.Present` ↔ Task 19 `Deps.UI`/`UIPresent` ↔ Task 20. CLEAN.**
`internal/api/ui.go` handles the not-present case with 503 + "UI not built; run make build"
and enforces GET/HEAD itself, with a 14-line comment (`ui.go:11-25`) explaining why the
pattern is `"/"` and not `"GET /"` — a startup-panic hazard that would only ever surface at
this seam.

**Seam F — Task 16 harness ↔ Tasks 7–15. CLEAN.** `go vet -tags integration ./...` exit 0.

---

## 3. Traceability Verification

I re-derived the plan's Traceability table against shipped code rather than trusting it.
All 14 FR groups, API surface §5, data model §6 and non-functional §8 map to real code.
Spot-checks at the leaf level, where group-granularity mapping hides things:

- **FR-3.6** (change search): title and author case-insensitive substring at
  `github/list.go:135-144`; numeric search bypasses the scan at `list.go:96-107`;
  ordering `MergedAt` desc with number tiebreak at `list.go:115-120`; page defaults
  30/max 100 in `provider.Page.Normalize()`. *Partial:* PRD says number matching is
  "exact **or prefix**"; only exact is implemented (`ParseSearchNumber` → `GetChange`).
  → Minor-2.
- **FR-12.2** (10 required integration scenarios): squash `integration_test.go:24`;
  merge-commit multi-commit `:49`; rebase/FF `:74`; unrelated commits excluded `:100`;
  repeated edits collapse `:141`; dependency-on-unselected conflict `:192`; plain conflict
  `:255`; cleanup leaves mirror usable `:292`; concurrent builds share one mirror `:336`.
  The tenth bullet — "base equals the parent of the earliest landing commit" — has no
  separately named test but is **asserted in every scenario** (`got.BaseSHA() != base` at
  `:38,62,92,122`) and unit-pinned at `resolve_test.go:118`. Covered.
  Bonus 11th: `TestIntegrationNoTokenLeaksIntoMirrorRemote:372`.
- **FR-11.2** (CLI exit codes): `exitCodeFor` `cmd/converge-cli/main.go:175-189` maps
  READY→0, CONFLICTED→2, the five validation/base codes→3, the three provider codes→4,
  else 1. Empirically verified — see §5.
- **§6 data model**: `session.Record` JSON tags match the PRD schema; `SchemaVersion = 1`;
  atomic rewrite in `session/store.go`; no secret fields on the record.
- **§8 non-functional**: security (`gitx` argv-only, `GIT_CONFIG_*` credential injection,
  `redactingHandler` `app.go:50`, `workspace.ErrOutsideRoot` guards), concurrency
  (`MAX_CONCURRENT_BUILDS` semaphore, `gitx.LockMap`), observability (`log/slog`,
  `NewLogger` `app.go:126`), reliability (`Store.LoadAll` recovery + `RunSweeper`),
  portability (`tools/build-backend.sh` cross-compiles linux/amd64 + linux/arm64).

**PRD requirements the Traceability table does not cover:**

1. **PRD §5.2 documents `?search=` on `GET /api/providers/{provider}/repositories`.**
   Not implemented: `api/repositories.go:62-79` reads only `page`/`pageSize`, and the
   Task 4 interface `ListRepositories(ctx, Page)` has no search parameter at all — so the
   param is silently ignored rather than rejected. This is internally inconsistent *within
   the PRD*: FR-3.1 defines `ListRepositories(ctx, page)` with no search, and FR-10.3
   provides manual `owner/repo` entry as the discovery path instead. The table maps
   "API surface §5 → 18, 19" at group granularity, so the omission never surfaced.
   → Minor-1.
2. **PRD §7 Service Impact** and **§10 Acceptance Criteria** are not rows in the table.
   §7 is descriptive (it even says "package names are indicative"); §10 is Task 30's job.
   Both are genuinely covered. No action.
3. **FR-14.1's "frontend `dist` tarball"** artifact is never produced. → Minor-3.

---

## 4. Configuration Consistency (three-way)

`internal/config/config.go` env keys, `README.md` documented keys, and `.env.example`
agree exactly on all 16 names: `APP_PORT`, `WORKSPACE_ROOT`, `REPOSITORY_CACHE_ROOT`,
`SESSION_TTL_HOURS`, `CLEANUP_INTERVAL_MINUTES`, `LOG_LEVEL`, `LOG_FORMAT`,
`MAX_CONCURRENT_BUILDS`, `GIT_CLONE_TIMEOUT_MINUTES`, `GIT_COMMAND_TIMEOUT_MINUTES`,
`PROVIDER_TIMEOUT_SECONDS`, and `PROVIDERS__<NAME>__{TYPE,BASE_URL,TOKEN,DISPLAY_NAME}`.
Matches Global Constraints verbatim. No drift.

---

## 5. Build, Vet & Test (this reviewer's own runs)

The controller owns the full `make` gate concurrently; I ran only the read-only /
non-mutating subset, in `apps/backend`.

| Command | Result |
|---|---|
| `CGO_ENABLED=0 go build ./...` | **PASS** (exit 0) |
| `go vet ./...` | **PASS** (exit 0) |
| `go vet -tags integration ./...` | **PASS** (exit 0) |
| `go test -count=1 ./...` | **PASS** — 18 packages ok, 0 fail (`internal/review` 5.35s, `internal/api` 1.13s; `provider/fake` has no test files, by design) |

I did **not** run `go test -race`, `make test`, `make build`, `npm ci`, `npm run build`,
`npm test`, or any docker target — the controller is running those in this same worktree
and they mutate shared paths. **No claim is made here about `-race` or the frontend suite.**

CLI contract verified against a compiled binary (not `go run`, which swallows exit codes):

```
$ go build -o $B ./cmd/converge-cli
$ $B --version            -> "dev",  exit 0
$ $B build                -> exit 3   (--changes is required; unconfigured env)
$ WORKSPACE_ROOT=… REPOSITORY_CACHE_ROOT=… PROVIDERS__X__{TYPE,TOKEN}=… $B build
                          -> exit 3   ("converge-cli: --changes is required")
$ $B                      -> exit 1   (usage)
```

FR-11.2 holds. But see Minor-4: the plan's own Task 30 Step 2 command cannot observe this.

---

## 6. TODO / FIXME / Stub Sweep

`grep -rn 'TODO|FIXME|XXX|HACK|placeholder|not implemented|unimplemented|panic("'` across
`apps/backend/**/*.go` (non-test), `apps/frontend/src/**`, `Makefile`, `tools/`,
`.github/`, `.gitlab-ci.yml`, `Dockerfile`, `README.md`, `docs/manual-checklist.md`:

**One hit, benign.** `apps/backend/internal/ui/embed.go:16` —
`panic("ui: dist directory missing from embed: " + err.Error())`. This is an
`embed.FS` build-invariant assertion in package init; if it can be reached the binary was
mislinked. Correct use of panic. Everything else is clean: zero TODO/FIXME markers on the
entire branch, in either language.

---

## 7. Deferred-Minor Ledger Triage (all 8)

| # | Item | Triage | Reasoning |
|---|---|---|---|
| 1 | No `error` prop on `RepositoryList`, `ProviderPicker`, `ChangeTable`, `FileTree`; caller ternaries tested only for `ReviewPage`; `ChangeTable` empty-state untested | **ACCEPT** | The plan never specified an `error` prop for any of the four — the shipped prop lists match Tasks 24/25/26 character-for-character. Safety is not "caller discipline" in the loose sense: **each component has exactly one caller in the entire tree** (`ProviderPicker`→`SelectRepositoryPage.tsx:44`, `RepositoryList`→`:62`, `ChangeTable`→`SelectChangesPage.tsx:131`, `FileTree`→`ReviewPage.tsx:150`), and **all four call sites guard with an exhaustive `isError ? <ErrorBanner/> : <Component/>` ternary** (`SelectRepositoryPage.tsx:37,54`; `SelectChangesPage.tsx:124`; `ReviewPage.tsx` error branch at `:101`). An error-state render is unreachable by construction, and the construction is a one-line grep to re-verify. The `ChangeTable` empty-state branch is 4 lines of `<EmptyState>` with no logic; the plan required no `ChangeTable` test at all. |
| 2 | Twelve shadcn `components/ui/*` may be unused | **ACCEPT** | Measured: **eight are used** (button 9, skeleton 5, input 3, badge 2, table 2, checkbox 1, collapsible 1, select 1). **Five are genuinely unused** — `card.tsx` (102 lines), `dialog.tsx` (168), `scroll-area.tsx` (52), `separator.tsx` (27), `tooltip.tsx` (54) = 403 lines with zero importers outside `components/ui/`. They are never imported, so Vite tree-shakes them entirely out of the bundle: zero runtime cost, zero user impact. Vendored-but-unused shadcn primitives are the normal state of a shadcn project. Deleting them is a safe optional tidy-up, not a merge gate. |
| 3 | `ManualRepositoryForm` does not populate the React Query cache | **ACCEPT** | Already ruled ACCEPTED at `audit-task-24.md:19`. Confirmed at `ManualRepositoryForm.tsx:37` (imperative `repositoriesService.get`). Worst case is one redundant GET on navigation; no stale or incorrect data is possible because the fetch is fresh. One correction to the ruling's premise is recorded as Minor-6, but it does not change the ruling. |
| 4 | `FileDiff`'s `PatchDiff` path is untestable under jsdom | **ACCEPT — recorded as coverage BOUNDARY** | Confirmed as a boundary, not a gap. `FileDiff.tsx:1` imports `@pierre/diffs/react`, which needs `ResizeObserver` and renders into shadow DOM — neither exists in jsdom. The team reproduced this twice. The testable surface *is* tested: `__tests__/FileDiff.test.tsx` exists and covers the binary and truncated branches, and its line 34 carries an explicit note about the untestable region. That is the right shape for this — cover what jsdom can reach, document the edge, and rely on the FR-12.4 manual checklist for real rendering. Closing it would require a browser-mode runner, which is disproportionate for one component. |
| 5 | `make build` no longer runs full-repo `go build ./...` | **ACCEPT** | `Makefile:38-41` runs the frontend build then `tools/build-backend.sh`, which compiles only `./cmd/converge` and `./cmd/converge-cli` per platform. Full-tree compilation is not lost: `make lint` runs `go vet ./...` (`Makefile:25`), which type-checks every package including tests, and `make test` compiles and runs all of them. The CI gate therefore still fails on any compile error anywhere. I independently confirmed `CGO_ENABLED=0 go build ./...` is clean at HEAD. |
| 6 | Pinned GitHub Actions are 2–3 majors stale | **ACCEPT** | Pins are `actions/checkout@v4`, `actions/setup-go@v5`, `actions/setup-node@v4`, `docker/setup-buildx-action@v3`, `docker/setup-qemu-action@v3`, `docker/login-action@v3`, `actions/upload-artifact@v4`. All verified live by the Task 28 review. Major-version pinning (not SHA) is the mainstream convention and keeps patch fixes flowing. Bumping majors is a maintenance chore with its own regression risk — correctly out of scope for the branch that introduces the pipeline. Currency note only. |
| 7 | README documents 7 of `make help`'s 11 targets | **ACCEPT** | Confirmed: `README.md:135-141` covers lint, test, test-integration, build, docker-build, version, dev. Omitted: `help`, `docker-push`, `release-github`, `clean`. The omissions are self-describing or CI-only — `help` *is* the discovery mechanism and every target carries a `## ` doc comment consumed by `Makefile:17`, and `docker-push`/`release-github` are only meaningful with CI credentials. A reader running `make` with no args lands on `help` (`.DEFAULT_GOAL := help`, `Makefile:2`) and sees all 11. No documentation gap in practice. |
| 8 | `make dev` requires bash ≥ 5.1 (`wait -n <pids>`) | **ACCEPT — settled by ruling R57, not reopened** | Documentation confirmed present at `README.md:30-33`: names the version, names the target, explains that macOS ships bash 3.2, and gives two workarounds (install newer bash on PATH, or run the two processes separately). `Makefile:1` pins `SHELL := /bin/bash`. As instructed I did not evaluate any bash-3.2-portable rewrite; R57 established that such a rewrite reintroduces an always-returns-0 bug. Documented prerequisite, correctly documented. |

---

## 8. Findings

**Critical: 0. Important: 0. Minor: 6.**

**Minor-1 — PRD §5.2 documents a `search=` query parameter on the repositories list
endpoint that is not implemented, and is silently ignored rather than rejected.**
`api/repositories.go:62-79` reads only `page`/`pageSize`; `provider.GitProvider.ListRepositories(ctx, Page)`
has no search parameter. The PRD contradicts itself here — FR-3.1 specifies the
no-search signature, and FR-10.3 makes manual `owner/repo` entry the discovery path — so
the shipped code follows the *requirement* and diverges only from the *example URL* in the
API-surface section. The Traceability table's group-level "API surface §5 → 18, 19" hid the
discrepancy from every per-task review. Recommend a one-line PRD correction post-merge
(strike `&search=` from the §5.2 example), or add it to the backlog if repository search is
wanted later. No code change needed.

**Minor-2 — FR-3.6's "number (exact **or prefix**)" search is implemented as exact-only.**
`github/list.go:96-107` routes a numeric search straight to `GetChange(n)`; a search of
`42` will not surface PR #421. Text search over title and author is fully correct
(`list.go:135-144`). Prefix-number matching would require scanning and client-side
filtering, which conflicts with the deliberate "number search bypasses the scan"
optimisation recorded in design.md:200 and README.md:161. The design decision is sound;
the PRD sentence is what is stale.

**Minor-3 — FR-14.1's "frontend `dist` tarball" artifact is never produced.**
`tools/build-backend.sh:29` tars only the two binaries per platform. There is no separate
frontend bundle artifact in either pipeline. This is arguably moot by construction: the
frontend is compiled *into* the Go binaries via `internal/ui/dist` embed, so a standalone
frontend tarball would have no consumer and could only ever drift from the binary it was
built alongside. Recommend striking the clause from FR-14.1 rather than adding the artifact.

**Minor-4 — the plan's own Task 30 Step 2 verification command cannot observe the exit
code it asserts.** `plan.md:15728-15730` prescribes
`go run ./cmd/converge-cli build; echo "exit=$?"` with "Expected: … `exit=3`". `go run`
does not propagate a non-zero child exit code — it prints `exit status 3` to stderr and
exits **1**. Following the plan literally produces `exit=1` and looks like a failure. The
CLI is correct; the check is not. I verified this both ways: `go run` → 1 with
`exit status 3` on stderr; compiled binary → 3. Anyone re-running Step 2 should use
`go build -o /tmp/cli ./cmd/converge-cli && /tmp/cli build; echo "exit=$?"`. Worth a plan
footnote so the next reader does not chase a phantom regression.

**Minor-5 — two Task 23 deliverables have no production consumer.** `useReviews` and
`useInvalidateReviews` (`lib/hooks/api/useReviews.ts`) are exported, unit-tested, and
imported nowhere outside their own tests. Both were explicitly named in Task 23's
`**Interfaces:**` block, and no later task was ever given a review-list or
cross-invalidation surface to consume them — so this is plan-faithful, not a skip. Flagged
only because "shipped and unreachable" is the kind of thing that should be a conscious
choice: either a future task uses them, or they can be dropped.

**Minor-6 — correction to a premise in the accepted Task 24 ruling.**
`audit-task-24.md:32` justifies accepting `ManualRepositoryForm`'s cache bypass with
"Nothing downstream assumes the resolved repository is already in the React Query cache",
evidenced by a grep scoped to `SelectRepositoryPage.tsx` and `ManualRepositoryForm.tsx`
only. That scope was too narrow: `SelectChangesPage.tsx:36` calls
`useRepository(providerId, repository ?? "", Boolean(repository))` — the very key the
manual form declined to seed — so navigating from manual entry to `/select` *does* issue a
second GET for the same repository. The ruling's **conclusion still holds** (one extra
request, fresh data, no correctness impact, and it is the same request the non-manual path
makes anyway), but the stated evidence should be corrected so it is not cited later as
proof of something it did not establish. A three-line fix exists if wanted
(`queryClient.setQueryData(repositoryKeys.detail(providerId, fullName), repository)` in
`onSubmit`); not required for merge.

---

## 9. Process Observations (not findings)

- **Plan checkboxes were never ticked** — 192 `- [ ]`, 0 `- [x]`. Completion is recorded in
  the per-task audits and `handoff.md` instead. Fine as executed, but a future reader
  diffing the plan will misread it. Worth a one-time sweep or an explicit note at the top
  of `plan.md`.
- **`audit-task-16.md`, `audit-task-17.md`, `audit-task-18.md` do not exist**, though the
  review-context file states per-task audits exist for 4–29. Those three tasks *were*
  reviewed — `handoff.md:646,649-651,827-828` discusses "the Task 18 miss" and "confirmed
  Task 17's fixes" — the reports just were not persisted under those filenames. No
  coverage gap, but the artifact set is incomplete.
- **Two carry-forward obligations from `handoff.md` are discharged.** `handoff.md:302-304`
  required Task 17 to enforce git ≥ 2.45 at startup and wire `REPOSITORY_CACHE_ROOT` into
  `mirror.New` — both present (`app.go:27,138`). `handoff.md:307,436` required Task 16 to
  cover the `ErrTooManyCommits` sub-path — covered at `resolve_test.go:560-567` (in the
  unit suite rather than the integration suite; equivalent coverage).
- **Branch hygiene is good.** 89 commits, 81 carrying the `(task-001)` conventional scope;
  the remainder are merges/docs. `dist/` at the repo root is untracked and correctly
  ignored (`.gitignore:49`).

---

## 10. Recommendation

**PASS — no plan-adherence blocker to merge.**

All 29 implementation tasks are DONE. Nothing was silently skipped, stubbed, or deferred
without a recorded ruling. All six cross-task seams hold, including the two most likely to
break (Go↔TS attribute contract, service URL↔router pattern) — both of which show evidence
of having been verified against the *other side's source* rather than the brief. Zero
TODO/FIXME debt. Backend build, vet (both tag sets) and unit tests pass under my own runs.
All eight deferred minors are ACCEPT; item 8 confirmed documented per R57 and not reopened.

Final merge readiness is contingent on the controller's concurrent
`make clean && make lint && make test && make test-integration && make build && make docker-build`
gate, plus the backend and frontend guideline reviewers' sections — none of which are in
this reviewer's scope.

**Optional post-merge follow-ups, in priority order:** correct the `audit-task-24.md:32`
premise (Minor-6); footnote the Task 30 Step 2 `go run` defect in `plan.md` (Minor-4);
strike the stale `&search=` from PRD §5.2 and the frontend-tarball clause from FR-14.1
(Minor-1, Minor-3); decide the fate of `useReviews`/`useInvalidateReviews` (Minor-5) and
the five unimported shadcn primitives (ledger item 2).
