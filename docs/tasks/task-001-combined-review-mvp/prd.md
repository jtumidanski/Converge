# Combined PR/MR Review MVP — Product Requirements Document

Version: v1
Status: Draft
Created: 2026-09-04
---

## 1. Overview

Converge is a small self-hosted application that lets a developer select several
merged GitHub Pull Requests or GitLab Merge Requests from one repository and review
their **combined net effect** as a single synthetic diff. It exists for the common
situation where one logical piece of work landed as a sequence of PRs/MRs, interleaved
with unrelated commits from other people. Reviewing each PR/MR individually shows
intermediate churn; diffing an old `main` against the current `main` shows everyone
else's work too. Converge answers exactly one question: *what is the final net code
change produced by this selected collection of PRs/MRs?*

To answer it, the backend resolves each selected change request into the commit(s)
that represent it on the target branch, computes a reconstruction base equal to the
target branch state immediately before the earliest selected change merged, creates an
isolated git worktree at that base from a local mirror cache, applies the selected
changes in chronological order, and diffs base against the synthetic head. Conflicts
and hidden dependencies on unselected work are reported, never guessed around.

This task delivers the whole MVP in one branch: monorepo scaffolding, a Go backend
(reconstruction core, provider layer, HTTP API, CLI proof of concept), a
React/TypeScript UI, Docker packaging, and GitHub Actions plus GitLab CI pipelines
that validate every PR/MR and publish versioned artifacts and container images from
mainline. It is a review aid, not a replacement for GitHub or GitLab review.

## 2. Goals

Primary goals:

- Support GitHub.com and GitLab (gitlab.com and self-hosted) through one provider
  abstraction; the reconstruction layer is provider-agnostic.
- Correctly reconstruct the net effect of a selected set of merged PRs/MRs for the
  merge-commit, squash, and rebase/fast-forward merge strategies.
- Never silently produce an incorrect diff: fail clearly on undeterminable base,
  incompatible target branches, conflicts, and missing dependencies.
- Isolate every review session in its own worktree and local branch; never mutate the
  source repository or the shared mirror cache beyond fetching.
- Present a familiar code-review UI: file tree, per-file stats, syntax-highlighted
  unified diff, binary detection.
- Deploy with `docker compose up` using externalized, secret-safe configuration.
- Ship CI for both GitHub Actions and GitLab CI that wraps repository-local commands,
  validates PRs/MRs, and publishes versioned artifacts and images (GHCR, GitLab
  Container Registry) from mainline.
- Prove reconstruction with a CLI before wiring the web UI; keep the CLI as a
  supported diagnostic tool.

Non-goals:

- Inline review comments, approvals, or any write access to providers.
- OAuth or per-user authentication; static server-side tokens only.
- Automatic conflict resolution, dependency suggestion, or automatic grouping of
  PRs/MRs into tasks.
- Pushing synthetic branches to a remote; persistent or shareable review artifacts.
- Reviewing open (unmerged) PRs/MRs.
- Multi-user collaboration inside a session.
- Jira/Azure DevOps integration, AI summaries, VS Code launch, provenance per line.
- Side-by-side diff unless the chosen rendering library provides it at no extra cost.
- GitHub Enterprise Server beyond a configurable API base URL (no GHES-specific
  behaviour is implemented or tested).

## 3. User Stories

- As a reviewer, I want to pick a configured provider instance (for example "GitLab
  Work") so that I can browse the repositories my server token can access.
- As a reviewer, I want to select a repository from a list or type `owner/repo`
  manually so that I can start a review even when the list is long.
- As a reviewer, I want to see recently merged PRs/MRs with number, title, author,
  merged date, source and target branch, and filter them by number, title, or
  author so that I can find the ones belonging to my task quickly.
- As a reviewer, I want to select several merged PRs/MRs and click **Build Review**
  so that the tool computes their combined net change for me.
- As a reviewer, I want to see the base the review was built from (branch and SHA)
  and the list of included PRs/MRs so that I trust what I am looking at.
- As a reviewer, I want a file tree with add/delete counts and a unified,
  syntax-highlighted diff per file so that I can review the net change like a normal
  PR.
- As a reviewer, I want a clear message naming the PR/MR and files involved when the
  set cannot be reconstructed so that I know what to do next.
- As a reviewer, I want to click **Finish Review** or **Discard** and have all
  temporary state removed so that the server does not accumulate garbage.
- As an operator, I want to configure providers, tokens, ports, and storage roots via
  environment variables and run `docker compose up` so that deployment is trivial.
- As an operator, I want tokens never to appear in logs, API responses, or the browser
  so that a trusted-network deployment is still safe.
- As a maintainer, I want every PR/MR pipeline to run lint, unit tests, reconstruction
  integration tests, a production build, and a Docker build so that mainline stays
  green, and I want mainline to publish versioned artifacts and images.
- As a maintainer, I want a CLI that reproduces reconstruction for a given provider,
  repo, base, and change list so that I can debug reconstruction problems without the
  UI.

## 4. Functional Requirements

### 4.1 Repository layout and tooling

- FR-1.1 The monorepo contains `apps/backend` (Go module `github.com/jtumidanski/converge`)
  and `apps/frontend` (React + TypeScript + Vite).
- FR-1.2 A root `Makefile` exposes at least: `lint`, `test`, `test-integration`,
  `build`, `docker-build`, `docker-push`, `version`. Each target is runnable locally
  with the same behaviour CI observes. CI workflows call only these targets (plus
  registry login).
- FR-1.3 `apps/backend` passes `go test -race -count=1 ./...`, `go vet ./...`,
  `golangci-lint run`, and `CGO_ENABLED=0 go build ./...`. The backend has no CGO
  dependencies.
- FR-1.4 `apps/frontend` passes `npm ci`, `npm run lint`, `npm run format:check`,
  `npm test`, and `npm run build`. `npm run format` rewrites; `format:check` fails
  on drift.
- FR-1.5 `CLAUDE.md` and `README.md` are updated with the real build commands, service
  paths, configuration reference, and deployment instructions.
- FR-1.6 Two binaries are produced from the backend module: `converge` (HTTP server
  serving API and embedded UI) and `converge-cli` (reconstruction proof of concept).
  Both share the same internal packages.

### 4.2 Configuration

- FR-2.1 Configuration is read from environment variables only. No provider URL,
  token, or path is hard-coded.
- FR-2.2 Global variables and defaults:

  | Variable | Default | Meaning |
  |---|---|---|
  | `APP_PORT` | `8080` | HTTP listen port |
  | `WORKSPACE_ROOT` | `/data/workspaces` | Root for per-session workspaces |
  | `REPOSITORY_CACHE_ROOT` | `/data/repositories` | Root for mirror cache |
  | `SESSION_TTL_HOURS` | `24` | Age after which sessions are expired and cleaned |
  | `CLEANUP_INTERVAL_MINUTES` | `30` | Period of the stale-session sweep |
  | `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

- FR-2.3 Named provider instances use the pattern `PROVIDERS__<NAME>__<KEY>` where
  `<NAME>` is `[A-Z0-9_]+` and keys are:

  | Key | Required | Meaning |
  |---|---|---|
  | `TYPE` | yes | `github` or `gitlab` |
  | `BASE_URL` | github: no (default `https://api.github.com`); gitlab: yes | API base URL. For GitLab this is the instance root, for example `https://gitlab.company.com`; the backend appends `/api/v4`. |
  | `TOKEN` | yes | Personal/project access token |
  | `DISPLAY_NAME` | no | Human label; default is `<NAME>` title-cased with `_` replaced by space (`GITLAB_WORK` -> `Gitlab Work`) |

  The provider ID exposed by the API is `<NAME>` lower-cased with `_` replaced by `-`
  (`GITLAB_WORK` -> `gitlab-work`).
- FR-2.4 Startup fails with a descriptive error (naming the variable, never its value)
  when: no providers are configured; a provider has an unknown `TYPE`; a required key
  is missing; `WORKSPACE_ROOT` or `REPOSITORY_CACHE_ROOT` cannot be created; `git` is
  not on `PATH`.
- FR-2.5 Startup logs the list of configured provider IDs, types, and base URLs. It
  never logs tokens or a dump of the environment.
- FR-2.6 `.env.example` documents every variable above with placeholder values.

### 4.3 Provider layer

- FR-3.1 A `GitProvider` interface isolates provider behaviour. Minimum operations:
  `ListRepositories(ctx, page)`, `GetRepository(ctx, fullName)`,
  `ListMergedChanges(ctx, repo, targetBranch, search, page)`, `GetChange(ctx, repo, number)`,
  `GetChangeCommits(ctx, repo, number)`, `CloneURL(repo)` and
  `AuthorizeGit(cmd)` (attach credentials to a git invocation without writing them
  to disk or the remote URL).
- FR-3.2 The common model is:

  ```text
  Repository   { ProviderID, FullName, Name, Namespace, DefaultBranch, WebURL }
  ChangeRequest{ ProviderID, Repository, Number, Title, Author, WebURL,
                 SourceBranch, TargetBranch, CreatedAt, MergedAt, State,
                 Commits []Commit (SHA, Message, AuthoredAt),
                 MergeCommitSHA, SquashCommitSHA, HeadSHA, Squashed bool }
  ```

  Every field is populated from provider API metadata, never from HTML scraping or
  commit-message parsing.
- FR-3.3 GitHub implementation uses the REST API v3 (`Authorization: Bearer <token>`,
  `X-GitHub-Api-Version` header) against the configured base URL. It supplies:
  accessible repositories (paginated), merged pull requests filtered to a base
  branch, a single PR, PR commits, `merge_commit_sha`, `merged_at`, `merged`,
  `head.sha`, `head.ref`, `base.ref`, `user.login`. Exact endpoints and the
  merged-only query strategy are decided in design and verified against the current
  GitHub REST docs.
- FR-3.4 GitLab implementation uses REST API v4 (`PRIVATE-TOKEN` header) against
  `<BASE_URL>/api/v4`. It supplies: member projects (paginated), merged merge
  requests filtered to `target_branch`, a single MR, MR commits, `merge_commit_sha`,
  `squash_commit_sha`, `squash`, `sha`, `merged_at`, `source_branch`,
  `target_branch`, `author.username`. Project identifiers in URLs are URL-encoded
  `namespace/project` paths. Verified against the current GitLab API docs.
- FR-3.5 Provider HTTP calls have a timeout (default 30 s), honour pagination, and
  map 401/403 to an authentication error, 404 to not-found, and 429/5xx to a
  provider-unavailable error. Error messages never include the token or full request
  headers.
- FR-3.6 Change listing supports `search` matching number (exact or prefix after
  stripping `#`/`!`), title (case-insensitive substring), or author username
  (case-insensitive substring). Server-side provider search is used where the API
  offers it; otherwise filtering is applied to fetched pages. Results are ordered by
  `MergedAt` descending. Default page size 30, maximum 100.
- FR-3.7 Repository identifiers accepted from clients must match
  `^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`, contain no `..` segments, and not start
  with `/`, `-`, or `.`. Anything else is rejected with 400 before any git or provider
  call.

### 4.4 Repository cache and workspace management

- FR-4.1 Mirrors live at `<REPOSITORY_CACHE_ROOT>/<provider-id>/<namespace...>/<name>.git`.
  First use runs `git clone --mirror`; subsequent uses run `git remote update --prune`.
- FR-4.2 Credentials are supplied to git per invocation (for example via a
  `-c http.<url>.extraheader=` config or `GIT_ASKPASS`/credential helper backed by
  the environment). The stored remote URL in the mirror contains no credentials.
  Chosen mechanism is fixed in design and covered by a test asserting
  `git remote get-url origin` contains no token.
- FR-4.3 Fetch/update of one mirror is guarded by a per-repository mutex held in
  process; mirrors of different repositories update concurrently. Worktrees are
  created only after the update completes; independent worktrees operate
  concurrently.
- FR-4.4 A session workspace is `<WORKSPACE_ROOT>/<session-id>/` containing
  `session.json` (session metadata), `repo/` (the git worktree), and on success
  `combined.diff`. Session IDs are 8 lowercase hex characters from a CSPRNG.
- FR-4.5 The worktree is created with `git worktree add <path> <base-sha>` followed
  by creation of local branch `review/<session-id>` in that worktree. The branch is
  never pushed.
- FR-4.6 All git invocations use structured process execution (`exec.Command` with
  an argument slice, no shell). Every argument that originates from a client (repo
  name, branch name, change number) is validated before use; SHAs are validated as
  `^[0-9a-f]{40}$`; branch names pass `git check-ref-format --branch`. Arguments
  beginning with `-` that came from clients are rejected.
- FR-4.7 Every git invocation runs with a context timeout (default 10 min for
  clone/fetch, 2 min for others), captures stdout and stderr separately, and logs
  the command category (clone, fetch, worktree, cherry-pick, diff, cleanup), the
  repository, and session ID, but never the full command line when it could contain
  credentials.
- FR-4.8 Git runs with `GIT_TERMINAL_PROMPT=0`, a fixed committer identity
  (`Converge Review <converge@localhost>`), and `core.hooksPath` pointed at an empty
  directory so repository hooks never execute.

### 4.5 Base selection

- FR-5.1 Inputs: provider, repository, base branch (defaults to repository default
  branch), and a non-empty, de-duplicated list of change numbers (maximum 50).
- FR-5.2 Every selected change must be in merged state; otherwise the build fails
  with error code `NOT_MERGED` naming the offending number(s).
- FR-5.3 Every selected change must have `TargetBranch` equal to the requested base
  branch; otherwise the build fails with `INCOMPATIBLE_TARGETS` listing the
  offending numbers and their targets.
- FR-5.4 Changes are sorted by `MergedAt` ascending. Ties are broken by number
  ascending.
- FR-5.5 For each change the "landing commit" on the target branch is resolved:
  - merge commit strategy: `MergeCommitSHA` has two parents;
  - squash strategy: `SquashCommitSHA` (GitLab) or `MergeCommitSHA` (GitHub) has
    one parent and is not among the change's original commits;
  - rebase/fast-forward strategy: the landing commits are the last N first-parent
    commits ending at the provider-reported head/merge SHA, where N is the change's
    commit count, and their patch-ids match the original commits.
  The detection order and exact provider fields are fixed in design and verified
  against provider docs.
- FR-5.6 The landing commit(s) of every change must be reachable from the tip of the
  base branch in the updated mirror (`git merge-base --is-ancestor`); otherwise the
  build fails with `NOT_ON_BASE_BRANCH`.
- FR-5.7 The reconstruction base is the first parent of the earliest change's landing
  commit (for merge commits: parent 1; for squash: the single parent; for rebase:
  parent of the first landing commit). The base SHA is stored in the session and
  displayed with the branch name and the earliest change number.
- FR-5.8 If any required commit object is absent after the mirror update, the
  backend performs one explicit `git fetch origin <sha>` attempt (where the server
  allows it) and then fails with `MISSING_COMMITS` naming the SHAs.
- FR-5.9 If the base cannot be determined for any reason the build fails with
  `BASE_UNDETERMINED` and a human-readable reason. No diff is produced.

### 4.6 Applying changes

- FR-6.1 A `ChangeApplicator` abstraction applies one `ChangeRequest` to a workspace
  and returns success, conflict (with file list), or error. The MVP implementation is
  cherry-pick based; the interface allows a patch/tree-based implementation later.
- FR-6.2 Merge-commit changes are applied with `git cherry-pick -m 1 --allow-empty
  <merge-sha>`, producing one synthetic commit that carries the change's net effect.
- FR-6.3 Squash changes are applied with `git cherry-pick --allow-empty <squash-sha>`.
- FR-6.4 Rebase/fast-forward changes are applied by cherry-picking each landing
  commit in original order.
- FR-6.5 Changes are applied strictly in the order from FR-5.4. Each synthetic commit
  message records the change number and provider so diagnostics can map commits back
  to PRs/MRs.
- FR-6.6 On conflict the applicator runs no automatic resolution. It records the
  change number, the commit SHA being applied, and the conflicting paths from
  `git diff --name-only --diff-filter=U`, leaves the worktree in its conflicted
  state, and the session becomes `CONFLICTED`.
- FR-6.7 The conflict message distinguishes two cases: when the conflicting change is
  the first in the set it is reported as a plain conflict; when an earlier change in
  the set exists the message additionally states that the change may depend on work
  not included in the review, and lists which selected changes were applied
  successfully before it. No unselected change is ever included.
- FR-6.8 An empty cherry-pick (change already fully represented by earlier
  selections) is not an error.

### 4.7 Combined diff

- FR-7.1 After all changes apply, the backend runs `git diff --find-renames
  <base-sha> <head-sha>` and stores the raw output as `combined.diff` in the
  workspace.
- FR-7.2 A file summary is produced from `git diff --numstat --name-status -M` (or
  equivalent): path, previous path for renames, status in {added, modified, deleted,
  renamed}, additions, deletions, and `binary` (numstat reports `-`).
- FR-7.3 Session-level totals: files changed, total additions, total deletions.
- FR-7.4 Per-file diff content is served on demand from `git diff <base> <head> --
  <path>` (old path included for renames). Binary files return `binary: true` and no
  hunk content. Files larger than 1 MiB of diff text return `truncated: true` with
  the first 1 MiB.
- FR-7.5 The combined diff must contain no hunks attributable solely to unselected
  commits (verified by integration tests per section 4.11).

### 4.8 Session lifecycle

- FR-8.1 States: `CREATING`, `READY`, `CONFLICTED`, `FAILED`, `FINISHED`, `EXPIRED`.
- FR-8.2 `POST /api/reviews` validates inputs synchronously, creates the session
  directory and `session.json` in `CREATING`, returns 202, and runs the build
  asynchronously. Progress stage (`resolving`, `updating-repository`,
  `creating-workspace`, `applying:<number>`, `diffing`) is written to `session.json`
  and exposed via the API so the UI can show status.
- FR-8.3 `session.json` contains: id, provider ID, repository, base branch, base SHA,
  head SHA, requested change numbers, resolved changes (number, title, author,
  merged at, strategy, landing SHAs), status, stage, error (code, message, details),
  totals, created at, updated at, expires at.
- FR-8.4 `DELETE /api/reviews/{id}` performs cleanup (FR-9) and marks the session
  `FINISHED`. It is idempotent: deleting an already finished or unknown session
  returns 204.
- FR-8.5 Sessions in `READY`, `CONFLICTED`, or `FAILED` remain browsable until
  finished, discarded, or expired. Expired sessions (`created at + SESSION_TTL_HOURS`
  in the past) are cleaned by the sweep and reported as `EXPIRED`.
- FR-8.6 On startup the backend reads every `session.json` under `WORKSPACE_ROOT`;
  sessions still in `CREATING` are marked `FAILED` with code `INTERRUPTED`; expired
  sessions are cleaned; directories without a valid `session.json` older than the
  TTL are removed.
- FR-8.7 The server keeps an in-memory index of sessions loaded from disk; disk is
  the source of truth and is rewritten atomically (write temp, rename) on every state
  change.

### 4.9 Cleanup

- FR-9.1 Cleanup of a session runs, in order and continuing on individual failure:
  `git worktree remove --force <path>` from the mirror, `git worktree prune`,
  `git branch -D review/<id>` in the mirror, `os.RemoveAll` of the session
  directory. Each failure is logged with session ID and step.
- FR-9.2 Cleanup refuses to remove any path that does not resolve (after
  `filepath.EvalSymlinks`) to a direct child of `WORKSPACE_ROOT`.
- FR-9.3 Cleanup never deletes anything under `REPOSITORY_CACHE_ROOT` except
  worktree metadata and `review/*` branches.
- FR-9.4 Cleanup is idempotent; running it twice for the same session succeeds.
- FR-9.5 A periodic sweep runs every `CLEANUP_INTERVAL_MINUTES` and on startup.

### 4.10 Web UI

- FR-10.1 Views: provider and repository selection, base branch and PR/MR selection,
  review status (building, conflicted, failed), combined diff.
- FR-10.2 Provider selection lists configured providers by display name and type.
- FR-10.3 Repository selection shows name, namespace, default branch, provider, and
  supports a manual `owner/repo` entry that is validated via `GET repository` before
  proceeding.
- FR-10.4 Base branch defaults to the repository default branch and is editable as
  free text (validated server-side).
- FR-10.5 Change selection lists merged PRs/MRs (number, title, author, merged date,
  source -> target branch, landing SHA short form when known) with a checkbox per
  row, a search box (number, title, author), pagination, and a **Build Review**
  button enabled when at least one row is selected. Selected rows persist across
  pages and searches within the view.
- FR-10.6 Status view polls `GET /api/reviews/{id}` every 2 s while `CREATING`,
  shows the current stage, and transitions automatically to the diff view on
  `READY` or to an error panel on `CONFLICTED`/`FAILED`.
- FR-10.7 Error panel shows the human message, the offending PR/MR(s), conflicting
  files when present, an expandable **Diagnostics** section with the error code,
  base SHA, and workspace path (path shown only, no link), and a **Discard Review**
  button.
- FR-10.8 Diff view header shows repository, base branch @ short SHA, "immediately
  before <number>", the included changes (number, title, link to provider web URL),
  totals, and a **Finish Review** button that calls `DELETE` and returns to the
  selection view.
- FR-10.9 File tree groups files by directory, shows status badge and `+a -d`
  counts, and selecting a file loads its diff. The first file is selected by
  default. Binary files render "Binary file changed". Truncated files show a
  truncation notice.
- FR-10.10 Diff rendering uses an existing library (selected in design) providing a
  unified view with syntax highlighting and collapsed unchanged regions. Side-by-side
  is enabled only if the library supports it without extra work.
- FR-10.11 Product vocabulary in the UI is limited to: Provider, Repository, Base,
  Included PRs/MRs, Combined Review, Conflict, Finish Review, Discard Review. Words
  like worktree, cherry-pick, or synthetic branch appear only inside Diagnostics.
- FR-10.12 The UI is built with Vite and embedded into the Go binary via `embed`;
  the server serves it at `/` with SPA fallback and the API under `/api`.
  During development Vite proxies `/api` to the backend.

### 4.11 CLI proof of concept

- FR-11.1 `converge-cli build --provider <id> --repo <owner/repo> [--base <branch>]
  --changes 421,427,435 [--out <dir>]` runs the same pipeline as the API using the
  same environment configuration and writes `combined.diff` and `metadata.json`
  (the `session.json` content) into `<out>` (default `./converge-out/<session-id>`),
  leaving the workspace in `WORKSPACE_ROOT` for inspection unless `--cleanup` is
  passed.
- FR-11.2 Exit codes: 0 success, 2 conflict, 3 validation/base error, 4 provider
  error, 1 unexpected error. The status and error payload are also printed as JSON
  on stdout.
- FR-11.3 `converge-cli` shares all reconstruction code with the server; there is no
  CLI-only reconstruction path.

### 4.12 Testing

- FR-12.1 Unit tests cover configuration parsing, repository identifier validation,
  provider response mapping (from recorded JSON fixtures for GitHub and GitLab),
  base selection ordering/compatibility rules, session persistence, and cleanup path
  guards.
- FR-12.2 Integration tests (`make test-integration`, Go build tag `integration`)
  script local bare git repositories with a fake provider and cover at least:
  - squash-merged change;
  - merge-commit change with multiple commits;
  - rebase/fast-forward change with multiple commits;
  - several selected changes interleaved with unrelated commits (unrelated changes
    absent from the result);
  - repeated modification of the same file across changes (single cumulative hunk);
  - a selected change depending on an unselected change (conflict reported with
    dependency wording, unselected change not included);
  - a plain conflict between selected changes (session `CONFLICTED`, files listed);
  - base determination equals the parent of the earliest landing commit;
  - cleanup removes worktree and branch and leaves the mirror intact;
  - concurrent builds on the same repository do not corrupt the mirror.
- FR-12.3 Frontend tests (Vitest + Testing Library) cover selection state, status
  polling transitions, and error panel rendering against mocked API responses.
- FR-12.4 A documented manual checklist covers the real-provider scenarios from
  section 34 of the source spec (GitHub squash and merge-commit PRs, GitLab squash
  and merge-commit MRs).

### 4.13 Docker deployment

- FR-13.1 A multi-stage `Dockerfile` builds the frontend, embeds it, builds a static
  Go binary, and produces a final image based on a small Linux base containing `git`
  and CA certificates, running as a non-root user with `/data` owned by that user.
- FR-13.2 `docker-compose.yml` runs the image, maps `APP_PORT`, mounts named
  volumes for `/data/repositories` and `/data/workspaces`, and reads provider
  settings from `.env`.
- FR-13.3 The image exposes `GET /healthz` returning 200 with build version, used as
  the compose healthcheck.
- FR-13.4 `docker compose up` with a valid `.env` yields a working UI on the
  configured port.

### 4.14 CI/CD

- FR-14.1 GitHub Actions: a `ci` workflow on `pull_request` and `push` to `main`
  runs `make lint test test-integration build docker-build`. On `push` to `main`
  and on tags matching `v*` it additionally logs in to GHCR with `GITHUB_TOKEN`,
  pushes the image, and uploads build artifacts (backend binaries for
  linux/amd64 and linux/arm64, frontend `dist` tarball). Tagged builds also create a
  GitHub Release with those artifacts.
- FR-14.2 GitLab CI: `.gitlab-ci.yml` with stages `validate`, `build`, `publish`
  running the same make targets for merge request pipelines and the default branch.
  Default-branch and tag pipelines push to the GitLab Container Registry using
  `CI_REGISTRY_USER`/`CI_REGISTRY_PASSWORD` and publish the same artifacts as job
  artifacts.
- FR-14.3 Registry destinations are computed from CI-provided variables
  (`GITHUB_REPOSITORY`, `CI_REGISTRY_IMAGE`) or overridable via `IMAGE_REPOSITORY`;
  no registry hostname or credential is committed.
- FR-14.4 Versioning: `make version` prints the git tag when HEAD is tagged `vX.Y.Z`,
  otherwise `0.0.0-<short-sha>`. Images are tagged with the version and the full SHA;
  mainline builds also tag `latest`. The version is compiled into the binary and
  exposed at `/healthz` and `converge-cli --version`.
- FR-14.5 All CI logic lives in the Makefile and scripts under `tools/`; workflow
  files contain only checkout, toolchain setup, caching, registry login, and make
  invocations.
- FR-14.6 Integration tests in CI must not require network access to GitHub or
  GitLab.

## 5. API Surface

All endpoints are under `/api`, return JSON, and follow JSON:API conventions:
resources are `{ "data": { "type", "id", "attributes" } }` or `{ "data": [...] }`,
lists carry `meta.page`, and errors are `{ "errors": [ { "status", "code", "title",
"detail", "meta" } ] }`. Content type `application/vnd.api+json` is accepted and
returned; plain `application/json` requests are also accepted.

### 5.1 Providers

`GET /api/providers`

```json
{ "data": [
  { "type": "providers", "id": "gitlab-work",
    "attributes": { "displayName": "GitLab Work", "kind": "gitlab", "baseUrl": "https://gitlab.company.com" } }
] }
```

Never includes tokens.

### 5.2 Repositories

`GET /api/providers/{provider}/repositories?page=1&search=`

```json
{ "data": [
  { "type": "repositories", "id": "atlas/server",
    "attributes": { "name": "server", "namespace": "atlas", "defaultBranch": "main",
                    "webUrl": "https://gitlab.company.com/atlas/server" } } ],
  "meta": { "page": { "number": 1, "size": 30, "hasNext": true } } }
```

`GET /api/providers/{provider}/repositories/{repo}` where `{repo}` is the
URL-encoded full name. Returns one repository resource or 404.

### 5.3 Changes

`GET /api/providers/{provider}/repositories/{repo}/changes?state=merged&target=main&search=&page=1&pageSize=30`

Only `state=merged` is supported; other values return 400.

```json
{ "data": [
  { "type": "changes", "id": "421",
    "attributes": { "number": 421, "title": "Add field-state endpoint", "author": "jsmith",
                    "sourceBranch": "feat/field-state", "targetBranch": "main",
                    "mergedAt": "2026-08-21T14:02:11Z", "createdAt": "2026-08-20T09:00:00Z",
                    "landingSha": "a1b2c3d4...", "webUrl": "https://..." } } ],
  "meta": { "page": { "number": 1, "size": 30, "hasNext": false } } }
```

`landingSha` may be null when the provider does not report it in list responses.

### 5.4 Reviews

`POST /api/reviews`

```json
{ "data": { "type": "reviews",
  "attributes": { "provider": "gitlab-work", "repository": "atlas/server",
                  "baseBranch": "main", "changes": [421, 427, 435, 441] } } }
```

Response `202 Accepted`:

```json
{ "data": { "type": "reviews", "id": "7f14b2c8",
  "attributes": { "status": "CREATING", "stage": "resolving", "provider": "gitlab-work",
                  "repository": "atlas/server", "baseBranch": "main", "changes": [421,427,435,441],
                  "createdAt": "...", "expiresAt": "..." } } }
```

Validation failures return 400 with codes `INVALID_PROVIDER`, `INVALID_REPOSITORY`,
`INVALID_BRANCH`, `INVALID_CHANGES` (empty, duplicates, > 50, non-positive).

`GET /api/reviews/{id}`

```json
{ "data": { "type": "reviews", "id": "7f14b2c8",
  "attributes": {
    "status": "READY", "stage": null,
    "provider": "gitlab-work", "repository": "atlas/server",
    "baseBranch": "main", "baseSha": "9f21a43...", "headSha": "...",
    "baseDescription": "Immediately before !421",
    "included": [ { "number": 421, "title": "...", "author": "jsmith", "mergedAt": "...",
                    "webUrl": "...", "strategy": "squash" } ],
    "totals": { "files": 17, "additions": 641, "deletions": 203 },
    "error": null,
    "createdAt": "...", "updatedAt": "...", "expiresAt": "..."
  },
  "relationships": { "files": { "links": { "related": "/api/reviews/7f14b2c8/files" } } } } }
```

When `status` is `CONFLICTED` or `FAILED`, `error` is:

```json
{ "code": "CONFLICT",
  "message": "MR !435 conflicts while being applied.",
  "change": 435, "commit": "c0ffee...",
  "conflictingFiles": ["src/field/FieldService.java"],
  "appliedChanges": [421, 427],
  "possibleDependency": true,
  "diagnostics": { "workspacePath": "/data/workspaces/7f14b2c8/repo", "branch": "review/7f14b2c8" } }
```

Error codes: `NOT_MERGED`, `INCOMPATIBLE_TARGETS`, `NOT_ON_BASE_BRANCH`,
`MISSING_COMMITS`, `BASE_UNDETERMINED`, `CONFLICT`, `PROVIDER_AUTH`,
`PROVIDER_UNAVAILABLE`, `REPOSITORY_UNAVAILABLE`, `GIT_FAILURE`, `INTERRUPTED`.
Human messages follow the wording in section 30 of the source spec.

`GET /api/reviews/{id}/files`

```json
{ "data": [
  { "type": "review-files", "id": "src/field/FieldService.java",
    "attributes": { "path": "src/field/FieldService.java", "previousPath": null,
                    "status": "modified", "additions": 40, "deletions": 12, "binary": false } } ] }
```

Returns 409 with code `REVIEW_NOT_READY` unless status is `READY`.

`GET /api/reviews/{id}/files/{path}` where `{path}` is URL-encoded (slashes encoded)
or supplied as `?path=`.

```json
{ "data": { "type": "review-file-diffs", "id": "src/field/FieldService.java",
  "attributes": { "path": "...", "previousPath": null, "status": "modified", "binary": false,
                  "truncated": false, "additions": 40, "deletions": 12,
                  "diff": "diff --git a/... b/...\n@@ ... @@\n..." } } }
```

`GET /api/reviews/{id}/diff` returns `combined.diff` as `text/plain` (used by the
CLI and for download).

`DELETE /api/reviews/{id}` returns 204 always (idempotent).

`GET /api/reviews` lists non-finished sessions (id, status, repository, createdAt)
so a reviewer can return to a review after a page reload.

### 5.5 Health

`GET /healthz` -> `{ "status": "ok", "version": "1.2.3" }`.

## 6. Data Model

No database. Persistence is the filesystem.

```text
<REPOSITORY_CACHE_ROOT>/<provider-id>/<namespace>/<name>.git     bare mirror
<WORKSPACE_ROOT>/<session-id>/session.json                        session record
<WORKSPACE_ROOT>/<session-id>/repo/                               git worktree
<WORKSPACE_ROOT>/<session-id>/combined.diff                       raw diff (READY only)
```

`session.json` schema (version field `schemaVersion: 1`):

```text
Session {
  schemaVersion    int
  id               string        8 hex
  providerId       string
  repository       string        owner/name
  baseBranch       string
  baseSha          string|null
  headSha          string|null
  requestedChanges []int
  resolvedChanges  []ResolvedChange { number, title, author, webUrl, mergedAt,
                                      strategy (merge|squash|rebase), landingShas []string }
  status           enum CREATING|READY|CONFLICTED|FAILED|FINISHED|EXPIRED
  stage            string|null
  error            ReviewError|null   (shape as in 5.4)
  totals           { files, additions, deletions } | null
  createdAt, updatedAt, expiresAt   RFC3339
}
```

Constraints: `id` unique by directory; the file is rewritten atomically; unknown
future fields are ignored on read. Secrets are never written to `session.json`.

Internal domain model (Go) is immutable value types with constructor validation per
the backend guidelines; the provider layer maps API JSON into these types.

## 7. Service Impact

All services are created by this task.

- `apps/backend` (new Go module):
  - `cmd/converge` server entry point; `cmd/converge-cli` CLI.
  - `internal/config` environment parsing and validation.
  - `internal/provider` interface + model; `internal/provider/github`,
    `internal/provider/gitlab`, `internal/provider/fake` (tests).
  - `internal/gitx` structured git runner, argument validation, credential
    injection, per-repo locks.
  - `internal/cache` mirror management; `internal/workspace` worktree lifecycle and
    cleanup.
  - `internal/review` base selection, applicator, orchestration, session state
    machine; `internal/diff` summary and per-file extraction.
  - `internal/session` on-disk store and sweep.
  - `internal/api` HTTP handlers, JSON:API encoding, embedded UI serving.
  - Package names are indicative; final layout is a design decision.
- `apps/frontend` (new Vite + React + TypeScript app) following the
  frontend-dev-guidelines skill: React Query for API state, Zod-validated forms for
  manual repository entry, Tailwind + shadcn/ui components, a diff library chosen in
  design.
- Repo root: `Makefile`, `tools/*.sh` build helpers, `Dockerfile`,
  `docker-compose.yml`, `.env.example`, `.github/workflows/ci.yml`,
  `.gitlab-ci.yml`, `.golangci.yml`, updated `README.md` and `CLAUDE.md`.

## 8. Non-Functional Requirements

- Security: tokens never appear in logs, API responses, session files, git remote
  configuration, or the browser. All git execution is argument-slice based; no
  shell. Client-supplied strings are validated before reaching git or the
  filesystem. Workspace and cache roots are the only writable locations; cleanup
  cannot escape them. Git hooks in fetched repositories never run. The server binds
  to `0.0.0.0:APP_PORT` and assumes a trusted network; no authentication is added.
- Concurrency: unlimited concurrent sessions bounded by a configurable worker limit
  (default 4 concurrent builds); per-repository fetch lock; no shared mutable state
  outside the session store and lock map.
- Performance: repository list and change list respond within provider latency plus
  200 ms; file diff endpoint for a 1 MiB diff responds within 500 ms on a local
  disk. Build time is dominated by clone/fetch and is reported via stage.
- Observability: structured `log/slog` logs with fields provider, repository,
  session, changes, stage, git category, duration, and outcome. Human-readable text
  handler by default; `LOG_FORMAT=json` optional.
- Reliability: startup recovery marks interrupted sessions failed; cleanup is
  idempotent; a failed cleanup never touches the mirror objects.
- Portability: single static binary; image runs on linux/amd64 and linux/arm64.
- Maintainability: backend follows `backend-dev-guidelines` (immutable domain
  models, functional composition, JSON:API transport); frontend follows
  `frontend-dev-guidelines`. Reviewer agents audit both before the PR.

## 9. Open Questions

- Diff rendering library: choose in design between `@git-diff-view/react`,
  `react-diff-view`, and `@pierre/diffs`, based on unified view quality, syntax
  highlighting, collapse support, and bundle size.
- GitHub merged-PR listing: whether to use `GET /repos/{owner}/{repo}/pulls?state=closed&base=`
  with client-side `merged_at` filtering or the Search API (`is:pr is:merged base:`),
  which has stricter rate limits. Verify against current GitHub docs in design.
- GitLab fast-forward merges: confirm which field (`merge_commit_sha`, `sha`,
  `diff_refs.head_sha`) identifies the landing commit when `merge_commit_sha` is
  null. Verify against GitLab docs in design.
- Git credential injection: `http.extraheader` via `-c` versus `GIT_ASKPASS`
  helper. Decide in design; both satisfy FR-4.2.
- Whether GitHub artifacts on tagged builds should also be attached to a GitHub
  Release (assumed yes) and whether GitLab should create a Release entry (assumed
  no, job artifacts only).

## 10. Acceptance Criteria

- [ ] `apps/backend` and `apps/frontend` exist; all commands in FR-1.3 and FR-1.4
      pass locally and in CI.
- [ ] `make lint test test-integration build docker-build` succeeds on a clean
      checkout with no network access to GitHub or GitLab.
- [ ] `docker compose up` with a `.env` containing one valid provider serves the UI
      on `APP_PORT` and `/healthz` reports the version.
- [ ] Providers configured as `PROVIDERS__<NAME>__*` appear in the UI by display
      name; misconfiguration fails startup with a message naming the variable.
- [ ] A reviewer can select a provider, pick or type a repository, keep or change the
      base branch, browse and search merged PRs/MRs, select several, and build a
      review.
- [ ] The resulting review shows base branch @ SHA, "immediately before <n>",
      included changes with links, totals, a file tree with statuses and counts, and
      a syntax-highlighted unified diff per file; binary files show "Binary file
      changed".
- [ ] Integration tests prove: unrelated interleaved commits are absent; repeated
      edits to one file yield one cumulative diff; squash, merge-commit, and
      rebase merges reconstruct identically to the provider's merged result;
      dependency on an unselected change and plain conflicts produce `CONFLICTED`
      with the correct change number and files; the base equals the parent of the
      earliest landing commit.
- [ ] Not-merged, incompatible-target, missing-commit, and base-undetermined inputs
      fail with the specified error codes and messages and produce no diff.
- [ ] Finish Review and Discard remove the worktree, the `review/<id>` branch, and
      the session directory; the mirror remains usable; repeated cleanup is a no-op.
- [ ] Startup and periodic sweeps expire sessions older than `SESSION_TTL_HOURS`
      and mark interrupted sessions `FAILED`.
- [ ] `converge-cli build` produces `combined.diff` and `metadata.json` and exits
      with the documented codes.
- [ ] No token appears in logs, any API response, `session.json`, or
      `git remote get-url origin` output (asserted by tests).
- [ ] GitHub Actions PR workflow and GitLab MR pipeline run lint, unit tests,
      integration tests, production build, and Docker build. Mainline pipelines on
      both platforms additionally publish versioned binaries, the frontend bundle,
      and a container image to GHCR / GitLab Container Registry using only CI-provided
      secrets and variables.
- [ ] `README.md` documents configuration, deployment, CLI usage, and the manual
      real-provider checklist; `CLAUDE.md` build commands are corrected.
- [ ] Code review via `superpowers:requesting-code-review` (plan adherence, backend
      and frontend guideline reviewers) has been run and findings addressed before
      the PR is opened.
