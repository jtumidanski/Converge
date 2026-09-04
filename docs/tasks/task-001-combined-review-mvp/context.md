# Context — task-001-combined-review-mvp

Companion to `plan.md`. Read `prd.md` for requirements, `design.md` for architecture,
`risks.md` for known hazards. This file records the decisions, environment facts, and
dependencies an implementer needs that the plan's task bodies do not repeat.

## Worktree and branch

| Item | Value |
|---|---|
| Worktree | `.worktrees/task-001-combined-review-mvp` (resolve with `git rev-parse --show-toplevel`) |
| Branch | `task-001-combined-review-mvp` |
| Base branch | `main` |
| Starting state | Only tooling exists: `README.md`, `CLAUDE.md`, `.claude/`, `docs/`, `tools/`, `.editorconfig`, `.gitignore` |

Every command runs from the worktree. Never edit the main checkout.

## Verified environment

Checked on 2026-09-04 on this machine.

| Tool | Version | Note |
|---|---|---|
| Go toolchain | 1.27.0 | `go.mod` declares `go 1.26` (see below) |
| git | 2.55.0 | Design requires ≥ 2.45 for `cherry-pick --empty=keep` |
| Node | 22.22.2 | Load with `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22` |
| npm | 10.9.7 | |
| docker | present at `/usr/bin/docker` | buildx assumed available |
| gh | present at `~/.local/bin/gh` | used only by `make release-github` |
| golangci-lint | not installed | pinned as a Go tool instead |

Docker base images confirmed to exist: `node:22-alpine`, `golang:1.27-alpine`, `alpine:3.22`.

## Decisions made during planning

These refine or correct the design. Reviewers should treat them as intentional.

1. **Go version raised to 1.26.** The design said `go 1.24`. golangci-lint v2.13.2, pinned
   through the `tool` directive, declares `go 1.26.0` in its own `go.mod`, so the module
   must be at least 1.26. Verified by downloading the module and reading its `go.mod`.

2. **golangci-lint is a Go tool, not a binary.** `go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2`,
   invoked as `go tool golangci-lint run`. The upstream docs warn that tool-directive
   installs are not guaranteed; if a future version breaks, fall back to the official
   binary install and update the Makefile and `CLAUDE.md` together. Latest v2 line at
   planning time: 2.13.2.

3. **Session model lives in `internal/session`, not `internal/review`.** The design placed
   `Session`, `ReviewError`, and the error codes in `review` while also making
   `session.Store` depend on them. That is an import cycle. The plan puts the model,
   builder, codes, `ReviewError`, the DTO, and the store together in `session`; `review`
   keeps landing resolution, the resolve pipeline, the applicator, messages, and the
   service. `review` imports `session`, never the reverse.

4. **New `internal/app` package.** Both binaries need identical wiring (config, logger with
   redaction, git version check, storage roots, provider registry, mirror, workspace,
   store, service). The design implied duplicating this in each `main`. `internal/app`
   holds it once; `cmd/converge` and `cmd/converge-cli` are thin.

5. **`@pierre/diffs` API confirmed against the installed package.** Version 1.4.0, exports
   `PatchDiff` from `@pierre/diffs/react` with props `patch: string` and
   `options?: FileDiffOptions`. The options that matter are `diffStyle: "unified" | "split"`,
   `expandUnchanged?: boolean`, `collapsedContextThreshold?: number`, `overflow`, and
   `theme`. React 18.3 and 19 are both accepted peers, both optional. A worker pool is
   available via `WorkerPoolContextProvider` from the same entry point but is not required;
   the plan skips it and code-splits the diff route instead. Fallback if 1.x proves
   unstable: `@git-diff-view/react` with lowlight, accepting no expand of collapsed regions.

6. **GitHub list responses leave `landingSha` null.** Verified from the design's endpoint
   table and reflected in the fixtures: the pulls list endpoint returns `merged_at`,
   `head`, `base`, and `user` but not `merge_commit_sha` or `merged`. Only `GET /pulls/{n}`
   supplies the landing commit. The PRD explicitly allows a null `landingSha` in list
   responses.

7. **GitLab commits arrive newest-first.** `GET /merge_requests/{iid}/commits` returns the
   newest commit first; the mapper reverses to oldest-first because landing resolution and
   the applicator both assume chronological order.

8. **Trailer format is `Converge-Change: #<number>`.** The design wrote
   `Converge-Change: <provider-id>#<number>`, but `session.ResolvedChange` carries no
   provider ID and the applicator has no access to one. The number alone is unambiguous
   within a session, and `session.json` records the provider.

9. **Conflict `Commit` prefers `CHERRY_PICK_HEAD`.** For a multi-commit rebase pick, the
   conflicting commit is whichever one git was applying, not necessarily the first. The
   applicator reads `CHERRY_PICK_HEAD` and falls back to the first landing SHA.

10. **`ResolveLanding` returns a multi-error.** Callers need both `errors.Is(err, ErrNoCandidate)`
    (to trigger the one-shot fetch-by-SHA retry) and `errors.As(err, &*session.ReviewError)`
    (to record the failure). A small `noCandidateError` type with `Unwrap() []error` provides
    both.

11. **Frontend package versions are pinned to what npm reports today.** React 19.2.8,
    Vite 8.2.2, Tailwind 4.3.3, TanStack Query 5.102.8, react-router 8.3.1, Zod 4.5.4,
    Vitest 5.0.0, MSW 2.15.0, ESLint 10.9.1. TypeScript 7.0.2 is published; if the Vite
    template's config does not compile under it, pin `typescript@~5.9` and record the
    choice here.

12. **`internal/ui/dist` is git-ignored except `.gitkeep`.** The Go `//go:embed all:dist`
    directive needs the directory to exist in a source checkout, so `go build ./...` works
    without a frontend build. `make build` fills it and re-creates `.gitkeep`.

## Key files and their responsibilities

### Backend

| Path | Responsibility |
|---|---|
| `internal/config` | Environment parsing, `Secret` redaction, `*Error{Variable,Reason}` |
| `internal/gitx` | Git execution with an isolated environment, argument validators, credential env, per-path locks, stderr redaction |
| `internal/provider` | Immutable model + builders, `GitProvider` interface, sentinel errors, registry, shared JSON helper |
| `internal/provider/github` | REST v3 client, bounded merged-PR scan with a 60 s cache |
| `internal/provider/gitlab` | REST v4 client, `order_by=merged_at` with a `created_at` fallback |
| `internal/provider/fake` | In-memory provider backed by `file://` repositories for tests |
| `internal/mirror` | Mirror path layout, clone/update/fetch-by-SHA, read-only `ObjectReader` |
| `internal/workspace` | Worktree create and cleanup, `EvalSymlinks` path guard |
| `internal/diff` | `combined.diff`, `--raw`/`--numstat` parsing, per-file diffs with a 1 MiB cap |
| `internal/session` | Session model and transitions, `ReviewError` and codes, DTO, atomic store, startup recovery, sweep |
| `internal/review` | Input validation, human messages, landing resolution, resolve pipeline, cherry-pick applicator, cleaner adapter, orchestrating service |
| `internal/app` | Shared wiring for both binaries |
| `internal/jsonapi` | Document encoding, error documents, generic request decoding |
| `internal/api` | Handlers per resource, middleware, router, embedded SPA serving |
| `internal/testutil` | Scripted bare repositories for integration tests |

### Frontend

| Path | Responsibility |
|---|---|
| `src/types/api`, `src/types/models` | JSON:API envelope types and resource models |
| `src/lib/api` | `fetch` wrapper and `ApiError` |
| `src/services/api` | Plain service objects, one per resource |
| `src/lib/hooks/api` | React Query key factories, queries, mutations |
| `src/lib/hooks/useSelection.ts` | Selection map persisted per repository in `sessionStorage` |
| `src/components/features` | Provider picker, repository list and manual entry, change table, review views |
| `src/pages` | Three routes: repository selection, change selection, review |
| `src/test` | Vitest setup, MSW server, render helpers |

### Root

`Makefile` (all CI logic), `tools/{version,build-backend,docker-push}.sh`, `Dockerfile`,
`docker-compose.yml`, `.env.example`, `.github/workflows/ci.yml`, `.gitlab-ci.yml`,
`README.md`, `CLAUDE.md`, `docs/manual-checklist.md`.

## Task dependency order

Tasks are written to be executed in order. The hard dependencies are:

```
1 (scaffold)
├─ 2 (config) ──────────────┐
└─ 3 (gitx) ─┬─ 4 (provider model, fake)
             │   ├─ 5 (github)   [needs 2, 4]
             │   └─ 6 (gitlab)   [needs 2, 4]
             ├─ 7 (test harness)
             ├─ 8 (mirror)       [needs 4, 7]
             ├─ 9 (workspace)    [needs 8]
             ├─ 10 (diff)        [needs 7]
             └─ 11 (session)     [needs 9, 10]
                 └─ 12 (input, messages, landing) [needs 8, 11]
                     └─ 13 (resolve)   [needs 12]
                         └─ 14 (apply) [needs 11]
                             └─ 15 (service, cleaner) [needs 13, 14]
                                 ├─ 16 (integration tests)
                                 └─ 17 (app wiring, CLI) [needs 5, 6, 15]
                                     └─ 18 (jsonapi)
                                         └─ 19 (api handlers)
                                             └─ 20 (server binary)
21 (frontend scaffold)
└─ 22 (types, client, services)
    └─ 23 (hooks, selection)
        └─ 24 (provider/repo views)
            └─ 25 (change selection, routing)
                └─ 26 (review page)
27 (make, docker, compose)   [needs 20, 26]
└─ 28 (CI pipelines)
    └─ 29 (docs)
        └─ 30 (verification, code review)
```

Tasks 5 and 6 are independent of each other. Tasks 21-26 are independent of 12-20 until
Task 27 builds both together, so the frontend can proceed in parallel with the backend
domain if two agents are available.

## Gotchas an implementer will hit

1. **Task 21 breaks a Task 1 test.** Once the frontend builds into `internal/ui/dist`,
   `Present()` returns true and the original assertion fails. Task 21 Step 8 replaces that
   test; do not skip it.

2. **Task 25 references `ReviewPage` before Task 26 writes it.** Task 25 creates a
   placeholder so the app compiles; Task 26 replaces the file.

3. **Task 21 creates a stub `tools/build-backend.sh`.** Task 27 replaces it with the real
   cross-compilation script. `make build` works in between but produces no tarballs.

4. **`git worktree add -b` is used instead of the PRD's two steps.** Equivalent outcome,
   but it cannot leave a detached worktree behind when the branch creation fails.

5. **The mirror lock is not held during cherry-pick or diff.** Those run inside the
   worktree. Concurrent object writes into the shared store are safe; the concurrency
   integration test proves it with `git fsck`.

6. **`protocol.file.allow=always` is set on every git call.** Integration tests clone from
   `file://` paths, which git refuses by default since CVE-2022-39253. It is harmless for
   https remotes but worth understanding: it is a deliberate test-support choice, recorded
   here so a reviewer does not read it as an oversight.

7. **The GitHub scan is bounded at 10 provider pages (1,000 closed PRs).** Very old merged
   PRs will not appear in the list. Number search bypasses the scan. This is documented in
   the README under Known limits.

8. **ServeMux path decoding.** `GET /api/providers/{provider}/repositories/{repo}` relies on
   `%2F` staying one segment and `r.PathValue` returning it decoded. Task 19 Step 5 says to
   verify this on the installed Go version and switch to `{repo...}` if it does not hold.

9. **The dependency-conflict integration test may need fixture tuning.** A squash that
   replaces a whole file can apply cleanly even when its predecessor is missing. Task 16
   Step 3 says to adjust the fixture to edit distinct nearby lines of a multi-line file if
   the first shape does not conflict.

10. **`.env` must not be committed.** `.gitignore` covers `*.env` and `.env`; the Task 27
    compose smoke test creates and deletes one. Confirm `git status --short` is clean
    before committing.

## Verification commands

Backend (cwd `apps/backend`):

```sh
go test -race -count=1 ./...
go test -race -count=1 -tags integration ./...
go vet ./...
go tool golangci-lint run
CGO_ENABLED=0 go build ./...
```

Frontend (cwd `apps/frontend`, Node 22 loaded):

```sh
npm ci && npm run lint && npm run format:check && npm test && npm run build
```

Repository root (what CI runs):

```sh
make lint test test-integration build docker-build
```

## Open items carried into implementation

- Confirm the installed TypeScript major and pin it (decision 11).
- Confirm `@pierre/diffs` `PatchDiff` props against `node_modules` types at Task 26 and
  record any deviation here.
- Confirm ServeMux `%2F` handling at Task 19 and record the chosen route form.
- Record the final dependency-conflict fixture shape at Task 16.
