# Combined PR/MR Review MVP — Design

Task: task-001-combined-review-mvp
Status: Proposed
Created: 2026-09-04
Source: `prd.md` (approved), `risks.md`

This document fixes the architecture, resolves the PRD's open questions, and records
the alternatives considered. It does not restate requirements; FR-x references point
at `prd.md`. Provider API facts below were verified against the current GitHub and
GitLab REST docs and the git manual on 2026-09-04; where a doc is silent that is
stated explicitly.

---

## 1. System shape

One Go module produces two binaries that share every internal package. The server
embeds the built React UI. Persistence is the filesystem only. There is no database,
no message queue, and no background service other than an in-process worker pool and
a periodic sweep goroutine.

```
                         ┌──────────────────────────────────────────────┐
  Browser ── /  ───────► │ cmd/converge (HTTP)                          │
          ── /api ─────► │  internal/api  ── jsonapi ── review.Service  │
                         │                              │               │
  converge-cli ────────► │  cmd/converge-cli ───────────┘               │
                         │                                              │
                         │  review.Service                              │
                         │   ├─ provider.Registry ── github / gitlab    │──► provider REST APIs
                         │   ├─ mirror.Cache      ── gitx.Runner        │──► git over HTTPS
                         │   ├─ workspace.Manager ── gitx.Runner        │
                         │   ├─ review/landing, review/apply, diff      │
                         │   └─ session.Store (disk + in-memory index)  │
                         └──────────────────────────────────────────────┘
                                   │                     │
                        REPOSITORY_CACHE_ROOT      WORKSPACE_ROOT
                        <provider>/<ns>/<name>.git <id>/session.json, repo/, combined.diff
```

### 1.1 Approaches considered

| Approach | Verdict |
|---|---|
| **A. Single Go module, layered packages, filesystem state, embedded SPA** (chosen) | Matches PRD FR-1.6, FR-8.7, FR-10.12. One deployable, one volume pair, no migrations. |
| B. Separate API and worker processes with a queue | Rejected. Adds a broker and a second image for a single-user, self-hosted tool. Concurrency needs (≤4 builds) are met by a semaphore. |
| C. Use go-git instead of the `git` binary | Rejected. go-git has no cherry-pick, no worktrees, and weak rename detection. The PRD mandates `git` semantics (`cherry-pick -m 1`, `patch-id`, `worktree`). Shelling out with strict argument validation is the correct tool. |
| D. Compute the combined diff without a worktree (tree-level patch application) | Deferred. The `ChangeApplicator` interface (FR-6.1) leaves the door open; the MVP ships the cherry-pick implementation only. |

---

## 2. Backend module layout

Module `github.com/jtumidanski/converge`, rooted at `apps/backend`. Go `1.24` in
`go.mod` (tool directives are used for golangci-lint), built with the toolchain on
`PATH` (locally 1.27). `CGO_ENABLED=0` always.

```
apps/backend/
├── go.mod, go.sum
├── cmd/
│   ├── converge/main.go            HTTP server entry point
│   └── converge-cli/main.go        CLI entry point (stdlib flag, subcommand "build")
├── internal/
│   ├── buildinfo/                  Version string set via -ldflags
│   ├── config/                     Env parsing, validation, redaction helpers
│   ├── provider/                   GitProvider interface, common model, typed errors, Registry
│   │   ├── github/                 REST v3 client + mapping (fixtures in testdata/)
│   │   ├── gitlab/                 REST v4 client + mapping (fixtures in testdata/)
│   │   └── fake/                   In-memory provider for unit + integration tests
│   ├── gitx/                       Runner (exec, env, timeouts, categories), argument validators,
│   │                               credential injection, per-path LockMap
│   ├── mirror/                     Mirror cache: path layout, clone --mirror, remote update, fetch-by-sha
│   ├── workspace/                  Worktree lifecycle: create, cleanup (path guards), sweep helpers
│   ├── review/                     Domain: Session model + builder, error codes, landing-commit
│   │                               resolution, base selection, ChangeApplicator, Service (orchestrator)
│   ├── diff/                       combined.diff production, file summary, per-file extraction
│   ├── session/                    On-disk store (atomic write), in-memory index, startup recovery, sweep
│   ├── jsonapi/                    Minimal JSON:API document/error encoding and request decoding
│   ├── api/                        net/http handlers per resource, middleware, router, embedded UI
│   └── ui/                         `embed.FS` of the built frontend (dist/, git-ignored except .gitkeep)
└── testdata/, internal/testutil/   Scripted bare repositories for integration tests
```

### 2.1 Dependency direction

`api` → `review` → {`provider`, `mirror`, `workspace`, `diff`, `session`} → `gitx`.
`provider/github` and `provider/gitlab` depend only on `provider` and `net/http`.
Nothing imports `api` or `cmd`. `gitx` imports nothing from the module.

### 2.2 Relationship to `backend-dev-guidelines`

The guideline skill was written for GORM-backed microservices with a shared `server`
package, api2go, logrus, and OpenTelemetry. None of those exist in this repository
and the PRD mandates `log/slog` and filesystem persistence. The design keeps the
principles and adapts the mechanics. The deviations below are intentional and should
be reflected in the guidelines and reviewer checklist as a follow-up.

| Guideline item | This project |
|---|---|
| Immutable models, private fields, accessors | Kept. `review.Session`, `provider.ChangeRequest`, `provider.Repository` are value types with private fields; state transitions return new values. |
| Fluent builder with `Build()` validation | Kept for `review.Session` and `provider.ChangeRequest`. Simple value types (`diff.FileSummary`) use validated constructors. |
| `entity.go` + GORM + migrations | Not applicable. `session.Store` serialises `Session` to `session.json` via an explicit DTO (`session/record.go`), which plays the "entity" role. |
| Processor layer, thin handlers | Kept. `review.Service` is the processor; `api` handlers validate transport input, call the service, map errors. Handlers never touch `gitx` or `provider` directly. |
| `server.RegisterHandler` / `RegisterInputHandler[T]` | Not available. `internal/jsonapi` provides `Decode[T](r, type)` and `Write*` helpers with the same intent: envelope handling lives in one place, handlers receive flat request structs. |
| logrus `FieldLogger` | `*slog.Logger` injected through constructors. No package-level loggers. |
| Lazy `model.Provider[T]` composition | Not adopted. The pipeline is a linear sequence with side effects; plain functions returning `(T, error)` are clearer. |

---

## 3. Configuration (`internal/config`)

`config.Load(env []string) (Config, error)` takes `os.Environ()` so tests pass
explicit slices. It scans for `PROVIDERS__<NAME>__<KEY>`, groups by `<NAME>`, and
validates each provider (FR-2.3, FR-2.4). Errors are `*config.Error{Variable string,
Reason string}` and never carry values.

Additional variables beyond the PRD table, all optional:

| Variable | Default | Reason |
|---|---|---|
| `MAX_CONCURRENT_BUILDS` | `4` | PRD §8 asks for a configurable worker limit. |
| `LOG_FORMAT` | `text` | `text` or `json` (PRD §8 observability). |
| `GIT_CLONE_TIMEOUT_MINUTES` | `10` | FR-4.7 default, made configurable per `risks.md`. |
| `GIT_COMMAND_TIMEOUT_MINUTES` | `2` | FR-4.7 default for non-clone commands. |
| `PROVIDER_TIMEOUT_SECONDS` | `30` | FR-3.5 default. |

Startup also runs `git --version` and requires ≥ 2.45 (needed for
`cherry-pick --empty=keep`, see §7). The Docker base image (Alpine 3.22, git 2.49)
satisfies this.

Token values are wrapped in `config.Secret` (a struct with a private string field
whose `String()` and `MarshalJSON` return `"[redacted]"`). The only accessor is
`Reveal()`, called in exactly two places: provider HTTP header construction and
`gitx` credential injection.

---

## 4. Provider layer (`internal/provider`)

### 4.1 Interface and model

```go
type GitProvider interface {
    ID() string
    Kind() Kind                                  // github | gitlab
    DisplayName() string
    BaseURL() string
    ListRepositories(ctx, Page) (Slice[Repository], error)
    GetRepository(ctx, fullName string) (Repository, error)
    ListMergedChanges(ctx, repo Repository, targetBranch, search string, Page) (Slice[ChangeRequest], error)
    GetChange(ctx, repo Repository, number int) (ChangeRequest, error)
    GetChangeCommits(ctx, repo Repository, number int) ([]Commit, error)
    CloneURL(repo Repository) string
    AuthorizeGit(spec *gitx.Spec)                // adds credential env, never argv
}
```

`Page{Number, Size}`; `Slice[T]{Items []T, HasNext bool}`. Errors are sentinel
values wrapped with context: `ErrAuth` (401/403), `ErrNotFound` (404),
`ErrUnavailable` (429, 5xx, timeouts, network). The wrapping message contains method,
path, and status, never headers or bodies.

`provider.Registry` maps provider ID to `GitProvider` and is built once from
`config.Config`. A shared `*http.Client` with the configured timeout is injected.

`ChangeRequest` follows the PRD model verbatim. `Commits` is populated lazily by the
review service via `GetChangeCommits` only for selected changes; list responses leave
it empty.

### 4.2 GitHub (`provider/github`)

Verified against the REST docs (2022-11-28 API version). Headers on every request:
`Accept: application/vnd.github+json`, `Authorization: Bearer <token>`,
`X-GitHub-Api-Version: 2022-11-28`. Pagination follows the `Link` header
`rel="next"`; `per_page` is clamped at 100 by GitHub.

| Operation | Endpoint | Notes |
|---|---|---|
| ListRepositories | `GET /user/repos?affiliation=owner,collaborator,organization_member&sort=full_name&per_page=<n>&page=<p>` | Correct for a PAT. `/installation/repositories` is App-only and not used. |
| GetRepository | `GET /repos/{owner}/{repo}` | Maps `full_name`, `name`, `owner.login`, `default_branch`, `html_url`, `clone_url`. |
| ListMergedChanges | `GET /repos/{o}/{r}/pulls?state=closed&base=<branch>&sort=updated&direction=desc&per_page=100&page=<k>` then filter `merged_at != null` | The list endpoint has **no** `merged` filter and does **not** return `merge_commit_sha` or `merged`; it does return `merged_at`, `head.ref`, `head.sha`, `base.ref`, `user.login`. `landingSha` is therefore `null` in GitHub list responses (allowed by PRD §5.3). |
| GetChange | `GET /repos/{o}/{r}/pulls/{n}` | Returns `merged`, `merged_at`, `merge_commit_sha`, `commits` (count), `head.sha`. |
| GetChangeCommits | `GET /repos/{o}/{r}/pulls/{n}/commits?per_page=100` | Capped by GitHub at 250 commits. A PR reporting `commits > 250` fails resolution with `BASE_UNDETERMINED` ("too many commits to verify"). |

**Merged-PR listing decision (PRD §9).** The Search API was rejected: it is limited
to 30 requests/minute, capped at 1,000 results, returns issue objects without
branches, and is mid-transition to "advanced search" semantics. The pulls endpoint is
used with client-side `merged_at` filtering. Because filtering breaks the 1:1 page
mapping, the GitHub provider fills a requested page by scanning provider pages of 100
(sorted by `updated` descending) until `page × pageSize` merged PRs are collected or a
cap of 10 provider pages (1,000 closed PRs) is reached; `HasNext` is true only if more
merged items were seen or the scan cap was hit with more pages available. A
per-provider in-memory cache keyed by `(repo, base, search)` holds the scanned merged
list for 60 seconds so that paging through results does not repeat the scan. Results
are re-sorted by `merged_at` descending before slicing (FR-3.6); the `updated` sort is
only an approximation of merge order and the docs bound is documented in README.

Search handling: a numeric search (after stripping `#`/`!`) hits `GET /pulls/{n}`
directly and returns it if merged into the requested base; otherwise title/author
substring filtering is applied to the scanned pages (GitHub offers no server-side
title filter on this endpoint).

**`merge_commit_sha` semantics (verified, quoted from the docs):** merge commit → SHA
of the merge commit; squash → SHA of the squashed commit on the base branch; rebase →
"the commit that the base branch was updated to". This is what §6 relies on.

Rate limiting: 429 and 403 with `x-ratelimit-remaining: 0` map to `ErrUnavailable`
with the `retry-after` / `x-ratelimit-reset` value in the message. No automatic
retry in the MVP.

### 4.3 GitLab (`provider/gitlab`)

Verified against the v4 docs. Header `PRIVATE-TOKEN: <token>`. Base URL is the
instance root; the client appends `/api/v4`. Pagination reads `x-next-page` (empty
means last page); `x-total*` headers are not relied upon because GitLab omits them
above 10,000 records.

| Operation | Endpoint | Notes |
|---|---|---|
| ListRepositories | `GET /projects?membership=true&simple=true&order_by=path&sort=asc&per_page=<n>&page=<p>[&search=]` | Maps `path_with_namespace`, `path`, `namespace.full_path`, `default_branch`, `web_url`, `http_url_to_repo`. `simple=true` omits `default_branch`; therefore `simple` is **not** used. |
| GetRepository | `GET /projects/{url-encoded path}` | Also reads `merge_method` (`merge`/`rebase_merge`/`ff`) and `squash_option` as diagnostics only. |
| ListMergedChanges | `GET /projects/{id}/merge_requests?state=merged&target_branch=<b>&order_by=merged_at&sort=desc&per_page=<n>&page=<p>[&search=<s>&in=title]` | List items include `merge_commit_sha`, `squash_commit_sha`, `sha`, `squash`, `merged_at`, `author.username`, `source_branch`, `target_branch`, so `landingSha` is populated. `order_by=merged_at` requires GitLab 17.2+; on 400 the client retries once with `order_by=created_at` and re-sorts client-side. |
| GetChange | `GET /projects/{id}/merge_requests/{iid}` | Same fields plus `state`. |
| GetChangeCommits | `GET /projects/{id}/merge_requests/{iid}/commits?per_page=100` | Paginated; `id`, `parent_ids`, `message`, `authored_date`. |

Search: numeric → `GET .../merge_requests/{iid}`; text → server-side `search=&in=title`
plus a second query with `author_username=<s>` when `<s>` is a plausible username
(`^[A-Za-z0-9_.-]+$`), merged and de-duplicated client-side. Author substring matching
beyond exact username is applied to fetched pages only.

**Fast-forward landing commit (PRD §9).** The GitLab docs describe `merge_commit_sha`
only as "SHA of the merge request commit, null until merged" and do not state its
value for `ff` merges. The design therefore does not depend on it: §6 uses a
provider-agnostic candidate chain `MergeCommitSHA → SquashCommitSHA → HeadSHA` and
verifies each candidate against the mirror. The README manual checklist includes an
`ff` project so the behaviour is confirmed empirically.

### 4.4 Fake provider (`provider/fake`)

In-memory `GitProvider` whose repositories point at local bare repositories
(`file://` clone URLs) and whose `ChangeRequest`s are produced by the integration test
harness (§11). `AuthorizeGit` is a no-op. It is also used by API handler tests.

---

## 5. Git execution (`internal/gitx`)

### 5.1 Runner

```go
type Spec struct {
    Dir      string
    Args     []string        // never includes "git"
    Env      []string        // appended to the fixed environment
    Stdin    io.Reader
    Category Category        // clone, fetch, worktree, cherry-pick, diff, cleanup, query
    Timeout  time.Duration   // zero = category default
    Repo     string          // for logging
    Session  string          // for logging
}
type Result struct{ Stdout, Stderr []byte; ExitCode int; Duration time.Duration }
type Runner interface{ Run(ctx context.Context, s Spec) (Result, error) }
```

`ExecRunner` uses `exec.CommandContext("git", args...)` with a minimal environment:
`PATH`, `HOME` (a private, empty directory so user gitconfig is ignored),
`GIT_TERMINAL_PROMPT=0`, `GIT_CONFIG_NOSYSTEM=1`, `LC_ALL=C`,
`GIT_AUTHOR_NAME/EMAIL` and `GIT_COMMITTER_NAME/EMAIL` set to
`Converge Review <converge@localhost>`. Every invocation is prefixed with
`-c core.hooksPath=<empty dir>`, `-c commit.gpgsign=false`, `-c protocol.file.allow=always`
(needed for `file://` mirrors in integration tests; harmless otherwise). Logging emits
category, repo, session, exit code, duration, and the first 2 KiB of stderr with
`Authorization`/`PRIVATE-TOKEN` values redacted, never argv or env.

A `FakeRunner` records specs and replays canned results for unit tests of `mirror`,
`workspace`, and `review/apply`.

### 5.2 Argument validation

`gitx.ValidateSHA` (`^[0-9a-f]{40}$`), `gitx.ValidateRepoFullName` (FR-3.7 regexp
plus `..` and leading `/ - .` checks), `gitx.ValidateBranch` (regexp pre-check then
`git check-ref-format --branch <name>` via the runner; names starting with `-` are
rejected before the call), `gitx.ValidateChangeNumber` (positive int). Handlers call
these before any provider or git call; `review.Service` calls them again as a
defence-in-depth invariant in `Session` builders.

### 5.3 Credential injection (PRD §9 decision)

Chosen: **`GIT_CONFIG_COUNT` environment configuration** (git ≥ 2.31, verified)
carrying a URL-scoped extra header:

```
GIT_CONFIG_COUNT=1
GIT_CONFIG_KEY_0=http.<scheme>://<host>/.extraheader
GIT_CONFIG_VALUE_0=Authorization: Basic base64(<user>:<token>)
```

`<user>` is `x-access-token` for GitHub and `oauth2` for GitLab (GitLab accepts any
non-empty username for PATs; GitHub ignores the username for PATs). Rationale over
`-c http.extraheader=`: `-c` places the token in argv, which is world-readable via
`/proc/<pid>/cmdline`; environment is readable only by the same uid. Rationale over
`GIT_ASKPASS`: no helper script to ship, no temp files, and the URL scoping means the
header is only sent to the provider host even if a redirect occurred. The stored
remote URL is the plain `clone_url` / `http_url_to_repo`. Tests: `git remote get-url
origin` contains no token; `FakeRunner` asserts no arg contains the token.

### 5.4 Locks

`gitx.LockMap` provides `Lock(key string) func()` backed by a `sync.Map` of
`*sync.Mutex`. The mirror path is the key. The lock is held for: clone/remote update,
fetch-by-sha, `worktree add`, and cleanup (`worktree remove/prune`, `branch -D`).
It is **not** held during cherry-pick or diff, which run inside the worktree; git
object writes into the shared store are safe concurrently. This satisfies FR-4.3 and
the concurrent-builds integration test.

---

## 6. Base selection and landing commits (`internal/review`)

### 6.1 Resolution pipeline (`review/resolve.go`)

1. Validate the request (`ResolveInput`): de-duplicate numbers, ≤ 50, base branch
   validated. Default base = repository default branch.
2. `GetChange` for each number (bounded concurrency 4). Any `State != merged` →
   `NOT_MERGED`. Any `TargetBranch != base` → `INCOMPATIBLE_TARGETS`.
3. Sort by `MergedAt` asc, then number asc (FR-5.4).
4. Ensure mirror is current (§7.1), then confirm `refs/heads/<base>` exists in the
   mirror (else `BASE_UNDETERMINED`, "branch not found in mirror").
5. `GetChangeCommits` for each change.
6. Resolve landing commits (§6.2) for each change; check each is an ancestor of the
   base tip via `git merge-base --is-ancestor <sha> refs/heads/<base>` →
   `NOT_ON_BASE_BRANCH` if not.
7. Base SHA = first parent of the earliest change's first landing commit
   (`git rev-parse <sha>^1`), FR-5.7.

Each step updates `stage` in the session (`resolving`, `updating-repository`).

### 6.2 Landing-commit resolution (`review/landing.go`)

Provider-agnostic. Input: the change's candidate SHAs in order
`[MergeCommitSHA, SquashCommitSHA, HeadSHA]` (nil entries skipped), the original
commit list, and a `gitx`-backed `ObjectReader` (parents, patch-id, first-parent walk).

```
for each candidate C (first that exists in the mirror; §6.3 handles absence):
  parents := parents(C)
  if len(parents) == 2                      → strategy=merge,  landing=[C]
  if len(parents) == 1:
     N := len(originalCommits)
     walk := firstParentWalk(C, N)          // C and its N-1 first-parent ancestors, oldest first
     if len(walk) == N && patchIDs(walk) == patchIDs(originalCommits) (as multisets):
                                            → strategy=rebase, landing=walk     (N > 1)
                                            → strategy=squash, landing=[C]      (N == 1; identical effect)
     else                                   → strategy=squash, landing=[C]
  if len(parents) == 0 or > 2               → BASE_UNDETERMINED
```

Why this order: a 2-parent commit is unambiguously a merge (GitHub merge, GitLab
`merge`, GitLab `rebase_merge` when a merge commit was created). A 1-parent
`merge_commit_sha` is a squash on GitHub or GitLab; for a GitHub rebase it is the
*last rebased commit*, whose ancestors are the other rebased commits with rewritten
SHAs, so patch-id comparison (`git patch-id --stable` over `git show --format= <sha>`)
is the only reliable test. For GitLab `ff`, `merge_commit_sha` may be null; the chain
falls through to `HeadSHA`, which for a fast-forward is literally the branch tip that
landed, and the same walk applies. If patch-ids do not match and N > 1 while the
provider says `Squashed == false`, the change is still treated as squash only if the
candidate's diff is non-empty; otherwise `BASE_UNDETERMINED` with a reason naming the
change. This keeps FR-5.5's "never guess" promise while covering all three
strategies per provider.

### 6.3 Missing objects (FR-5.8)

If a candidate SHA is not present after the mirror update, `mirror.FetchSHA` runs
`git fetch origin <sha>` once under the mirror lock. GitLab allows arbitrary reachable
SHA fetches by default (`uploadpack.allowAnySHA1InWant`, enabled since 13.2); GitHub
accepts reachable SHAs in practice but does not document it. Failure → `MISSING_COMMITS`
naming the SHAs.

---

## 7. Mirror, workspace, apply, diff

### 7.1 Mirror cache (`internal/mirror`)

Path: `<REPOSITORY_CACHE_ROOT>/<provider-id>/<namespace path>/<name>.git`. Namespace
segments come from the validated full name (FR-3.7 guarantees no traversal).
`Ensure(ctx, provider, repo)`: under the lock, if the directory lacks `HEAD` → `git
clone --mirror <url> <path>`, else `git -C <path> remote update --prune`. Both carry
`AuthorizeGit` env and the clone timeout. `ObjectReader` methods (`Parents`,
`PatchID`, `FirstParentWalk`, `IsAncestor`, `RevParse`) run read-only commands in the
mirror without the lock.

### 7.2 Workspace (`internal/workspace`)

`Create(ctx, mirrorPath, sessionDir, baseSHA, sessionID)` runs, under the mirror lock,
`git -C <mirror> worktree add -b review/<id> <sessionDir>/repo <baseSHA>`. Using `-b`
creates the branch atomically with the worktree, which is equivalent to FR-4.5's two
steps but cannot leave a detached worktree behind on failure. The worktree gets
`-c core.hooksPath` from the runner on every command, so no hooks execute.

`Cleanup(ctx, mirrorPath, sessionDir, sessionID)` implements FR-9.1 in order,
continuing on failure, and refuses to run unless `filepath.EvalSymlinks(sessionDir)`
resolves to a direct child of the evaluated `WORKSPACE_ROOT` (FR-9.2). It only ever
touches the mirror through `worktree remove/prune` and `branch -D review/<id>`
(FR-9.3).

### 7.3 Applicator (`review/apply.go`)

```go
type ChangeApplicator interface {
    Apply(ctx, ws Workspace, rc ResolvedChange) (ApplyResult, error)
}
type ApplyResult struct{ Outcome Outcome /* applied | empty | conflict */; ConflictingPaths []string; Commit string }
```

`CherryPickApplicator` per strategy:

| Strategy | Command |
|---|---|
| merge | `git cherry-pick -m 1 --empty=keep <merge-sha>` |
| squash | `git cherry-pick --empty=keep <squash-sha>` |
| rebase | `git cherry-pick --empty=keep <sha_1> … <sha_N>` (one invocation, original order) |

`--empty=keep` (git ≥ 2.45) implies `--allow-empty` and additionally keeps commits
that *become* empty because an earlier selected change already carried their content
(FR-6.8); plain `--allow-empty` would stop on those. After a successful pick the
applicator rewrites the message of each new commit with `git commit --amend
--allow-empty -F -` to `<original subject>\n\nConverge-Change: <provider-id>#<number>\nConverge-Source: <sha>`
(FR-6.5). On a non-zero exit, `git diff --name-only --diff-filter=U -z` lists the
conflicting paths; the worktree is left as is and the session becomes `CONFLICTED`
with `appliedChanges` (the selected changes applied before it) and
`possibleDependency = true` exactly when the conflicting change is not the first in
the sorted set, per FR-6.7.

### 7.4 Diff (`internal/diff`)

After the last change, `head = git rev-parse HEAD`. `combined.diff` is written from
`git diff --find-renames <base> <head>` streamed to a temp file then renamed. The
summary is assembled from two `-z` outputs joined by path: `git diff --raw -z
--find-renames <base> <head>` for status letters (`A M D R`) and old/new paths, and
`git diff --numstat -z --find-renames <base> <head>` for counts (`-` → `binary`).
Per-file content: `git diff --find-renames <base> <head> -- <path> [<oldPath>]` with
the `--` separator guaranteeing paths are never parsed as options; output is capped at
1 MiB with `truncated: true`. The summary is computed once and stored in
`session.json` (`files` array) so the files endpoint does not re-run git; per-file
diffs are generated on demand.

---

## 8. Session lifecycle and orchestration

### 8.1 Model (`review/session.go`, `review/errors.go`)

`Session` is immutable; transitions are methods returning a new `Session`
(`WithStage`, `WithResolved`, `WithBase`, `Ready`, `Conflicted`, `Failed`,
`Finished`, `Expired`). `SessionBuilder` validates ID format, provider, repo, branch,
change list. `ReviewError{Code, Message, Change, Commit, ConflictingFiles,
AppliedChanges, PossibleDependency, Diagnostics}` is both the domain error and the
API `error` attribute; codes are the PRD's constant set. Human messages are
centralised in `review/messages.go`.

### 8.2 Store (`internal/session`)

`Store` owns `WORKSPACE_ROOT`, an in-memory `map[id]Session` guarded by `sync.RWMutex`,
and `Save(Session)` which marshals `record.v1`, writes `session.json.tmp`, fsyncs,
renames (FR-8.7). `LoadAll()` at startup applies FR-8.6: `CREATING` → `FAILED/INTERRUPTED`,
expired → cleanup, invalid directories older than TTL → remove. `Sweep()` runs at
startup and every `CLEANUP_INTERVAL_MINUTES`. The store never calls git; it delegates
cleanup to a `Cleaner` interface implemented by `workspace`.

### 8.3 Service (`review/service.go`)

```go
type Service struct {
    providers *provider.Registry; mirrors *mirror.Cache; workspaces *workspace.Manager
    applicator ChangeApplicator; store *session.Store; sem chan struct{}; log *slog.Logger
}
func (s *Service) Create(ctx, CreateInput) (Session, error)   // sync validation, persists CREATING, enqueues build
func (s *Service) Get(id) (Session, bool)
func (s *Service) List() []Session
func (s *Service) Files(id) ([]diff.FileSummary, error)
func (s *Service) FileDiff(ctx, id, path) (diff.FileDiff, error)
func (s *Service) CombinedDiff(id) (io.ReadCloser, error)
func (s *Service) Finish(ctx, id) error                        // idempotent cleanup + FINISHED
func (s *Service) Build(ctx, id) Session                       // the pipeline; used by CLI synchronously
```

`Create` acquires nothing; it spawns `go s.runBuild(id)` which takes a slot from
`sem` (size `MAX_CONCURRENT_BUILDS`), derives a context from the server's lifetime
context with a 60-minute ceiling, and runs `Build`. The CLI calls `Build` directly on
the calling goroutine. Every stage boundary persists the session; a panic in the
build goroutine is recovered and recorded as `GIT_FAILURE`.

Server shutdown cancels the lifetime context; in-flight builds fail fast, and the
next startup's `LoadAll` marks anything still `CREATING` as `INTERRUPTED`.

---

## 9. HTTP API (`internal/jsonapi`, `internal/api`)

### 9.1 Router and middleware

Go 1.22+ `http.ServeMux` with method-qualified patterns; no third-party router.
Path wildcards `{provider}`, `{repo}`, `{id}`, `{path...}`. The ServeMux matches
against the escaped path, so `owner%2Frepo` is a single segment and `r.PathValue`
returns it decoded; a handler test pins this behaviour. `?path=` and `?repo=` query
fallbacks are accepted as the PRD allows.

Middleware chain: request ID → slog request log (method, route, status, duration,
session ID when present) → panic recovery (500 JSON:API error) → content negotiation
(accept `application/vnd.api+json` and `application/json`; respond
`application/vnd.api+json`).

### 9.2 JSON:API helpers

`jsonapi.Resource{Type, ID string; Attributes any; Relationships map[string]Relationship}`,
`jsonapi.WriteOne/WriteList(w, status, res, meta)`, `jsonapi.WriteErrors(w, ...Error)`,
`jsonapi.Decode[T any](r *http.Request, wantType string) (T, error)` which unwraps
`data.attributes` into `T` and rejects mismatched `type`. Handlers own the mapping
from domain errors to `(status, code)`:

| Domain error | HTTP |
|---|---|
| validation (`INVALID_*`) | 400 |
| `provider.ErrAuth` | 502 `PROVIDER_AUTH` |
| `provider.ErrNotFound` | 404 |
| `provider.ErrUnavailable` | 503 `PROVIDER_UNAVAILABLE` |
| `review.ErrNotReady` | 409 `REVIEW_NOT_READY` |
| unknown session | 404 (except DELETE → 204) |
| anything else | 500 `GIT_FAILURE`/`INTERNAL` with no internal detail |

Resource files: `providers.go`, `repositories.go`, `changes.go`, `reviews.go`,
`review_files.go`, `health.go`, `ui.go`. Each REST attribute struct lives beside its
handler with a `transform(domain) attrs` function; none of them expose tokens.

### 9.3 Embedded UI (`internal/ui`, `api/ui.go`)

`//go:embed all:dist` in `internal/ui/embed.go`. `apps/backend/internal/ui/dist/` is
git-ignored except `.gitkeep`, so `go build ./...` compiles without a frontend build.
`ui.Handler()` serves files from the embedded FS with SPA fallback to `index.html`
for non-`/api` GETs; if `index.html` is absent it returns a 503 text page "UI not
built; run make build". `make build` runs the Vite build with `outDir` pointing at that
directory (`emptyOutDir: true`, `.gitkeep` re-created by the Makefile). Cache headers:
hashed assets `immutable`, `index.html` `no-cache`.

---

## 10. CLI (`cmd/converge-cli`)

Stdlib `flag` with a `build` subcommand and `--version`. It loads the same
`config.Load`, builds the same `review.Service` with `sem` size 1, calls `Create`
followed by a synchronous `Build`, then copies `combined.diff` and writes
`metadata.json` (the `session.json` content) to `--out`. Exit codes map from the final
status: `READY` → 0, `CONFLICTED` → 2, `FAILED` with `NOT_MERGED | INCOMPATIBLE_TARGETS
| NOT_ON_BASE_BRANCH | MISSING_COMMITS | BASE_UNDETERMINED | INVALID_*` → 3,
`PROVIDER_*` → 4, else 1. `--cleanup` calls `Finish`. The final session record is
printed as JSON on stdout; logs go to stderr.

---

## 11. Testing strategy

### 11.1 Unit (`go test -race ./...`)

- `config`: table-driven env slices → Config or `*config.Error` naming the variable;
  `Secret` redaction in `%v`, `%+v`, `json.Marshal`, slog.
- `provider/github`, `provider/gitlab`: `httptest.Server` replaying JSON fixtures in
  `testdata/` recorded from the documented response shapes; assert mapping, pagination
  (`Link` / `x-next-page`), error mapping, and that no request lacks the auth header.
- `review/landing`: `ObjectReader` fake with scripted parents/patch-ids covering merge,
  squash, rebase (N=1 and N>1), GitLab ff fallback chain, missing candidate.
- `review/resolve`: ordering, `NOT_MERGED`, `INCOMPATIBLE_TARGETS`, de-dup, >50.
- `gitx`: validators (fuzz for the repo-name regexp), `ExecRunner` env construction
  (no token in argv), redaction of stderr.
- `workspace`: cleanup path guard with symlinked directories.
- `session`: atomic write, `LoadAll` recovery cases, unknown-field tolerance.
- `api`: handler tests with `fake` provider and a `FakeRunner`-backed service:
  status codes, JSON:API envelopes, `%2F` routing, 409 on not-ready.

### 11.2 Integration (`go test -race -tags integration ./...`)

`internal/testutil/repo.go` scripts bare repositories with the real `git` binary:
`NewRepo(t)`, `Commit(file, content)`, `Branch`, `MergeNoFF`, `Squash`, `Rebase`
returning the SHAs a provider would report. Scenarios build `fake` `ChangeRequest`s
from those SHAs and run `review.Service.Build` end to end against temp
`WORKSPACE_ROOT`/`REPOSITORY_CACHE_ROOT`. Every FR-12.2 bullet is one test function;
the "unrelated commits absent" test asserts the combined diff has no hunk touching
files only the unrelated commits changed, and the "identical to provider result" tests
compare `git diff base head` trees to the reference branch. Concurrency test runs 4
builds on one repository and checks `git fsck` on the mirror afterwards. No network.

### 11.3 Frontend

Vitest + Testing Library + MSW (mock service worker) for API responses, covering
FR-12.3. The guideline skill names Jest; Vitest is chosen because it shares the Vite
config and the PRD (FR-12.3) specifies it. The skill doc should be updated after this
task.

### 11.4 Manual checklist

`docs/manual-checklist.md` lists real-provider scenarios: GitHub squash PR, GitHub
merge-commit PR, GitHub rebase PR, GitLab squash MR, GitLab merge MR, GitLab `ff`
project MR, plus a token-leak sweep (`grep` of logs, `git remote get-url`, API
responses).

---

## 12. Frontend design (`apps/frontend`)

### 12.1 Stack

Vite 6, React 19, TypeScript strict (`noUncheckedIndexedAccess`,
`exactOptionalPropertyTypes`), Tailwind CSS v4, shadcn/ui (Radix), TanStack Query v5,
react-router v7, react-hook-form + Zod, sonner, lucide-react, ESLint (typescript-eslint
+ react-hooks) and Prettier. Path alias `@/*` → `src/*`. Dev proxy `/api` →
`http://localhost:8080`.

### 12.2 Diff rendering library (PRD §9 decision)

| | `@pierre/diffs` 1.4 | `@git-diff-view/react` 0.1.7 | `react-diff-view` 3.3 |
|---|---|---|---|
| Parses raw git patch text | Yes (`parsePatchFiles`) | Yes (`hunks` strings) | Yes (`parseDiff`) |
| Unified + split | Both, one prop | Both | Both |
| Highlighting | Shiki (TextMate grammars) | lowlight/highlight.js | Prism via refractor |
| Collapsed context with expand | Built in (`collapsedContextThreshold`, `expandUnchanged`) | Needs full file content | Needs full file content |
| Maintenance | Active, 4.6M weekly downloads | Active, 89K | Slower, 280K |
| Cost | 7.4 MB unpacked incl. Shiki | 1.3 MB | 1.5 MB |

**Chosen: `@pierre/diffs`.** It is the only option whose collapse/expand works from
the patch alone, which matters because the API serves per-file diffs without full file
contents (FR-7.4). Split view comes free, satisfying FR-10.10's "only if no extra
work". Shiki quality justifies the size; mitigations: the diff route is code-split
(`React.lazy`), the library's worker entry is used for highlighting, and Shiki
languages load on demand. Fallback if the 1.x API proves unstable during
implementation: `@git-diff-view/react` with lowlight, accepting no expand of collapsed
regions.

### 12.3 Structure

```
apps/frontend/src/
├── main.tsx, App.tsx                Providers: BrowserRouter > QueryClientProvider > Toaster
├── pages/
│   ├── SelectRepositoryPage.tsx     /                       provider + repository selection
│   ├── SelectChangesPage.tsx        /select                 ?provider=&repo=  base + PR/MR selection
│   └── ReviewPage.tsx               /reviews/:id            status | error | diff, driven by session status
├── components/
│   ├── ui/                          shadcn primitives (button, input, checkbox, table, badge, card,
│   │                                skeleton, collapsible, select, dialog, tooltip)
│   ├── common/                      PageHeader, EmptyState, ErrorBanner, Pagination
│   └── features/
│       ├── providers/ProviderPicker.tsx
│       ├── repositories/RepositoryList.tsx, ManualRepositoryForm.tsx (Zod + RHF)
│       ├── changes/ChangeTable.tsx, ChangeSearch.tsx, SelectionBar.tsx
│       └── review/ReviewStatus.tsx, ReviewErrorPanel.tsx, ReviewHeader.tsx,
│                  FileTree.tsx, FileDiff.tsx (lazy; wraps @pierre/diffs)
├── lib/
│   ├── api/client.ts                fetch wrapper: JSON:API headers, error → ApiError{status, code, detail}
│   ├── api/errors.ts                isApiError, messageFor(error)
│   ├── hooks/api/useProviders.ts, useRepositories.ts, useChanges.ts, useReviews.ts
│   ├── hooks/useSelection.ts        Map<number, ChangeSummary> selection persisted per repo in sessionStorage
│   ├── schemas/repository.ts        Zod: manual owner/repo entry mirrors FR-3.7 regexp
│   └── utils.ts                     cn()
├── services/api/                    providersService, repositoriesService, changesService, reviewsService
├── types/models/                    provider.ts, repository.ts, change.ts, review.ts, reviewFile.ts
└── types/api/                       jsonapi.ts (Document<T>, ListDocument<T>, ErrorDocument, PageMeta)
```

The API client is a thin `fetch` wrapper; React Query owns caching and retries, so the
guideline's dedup/cache/retry client is not reproduced (YAGNI).

### 12.4 Key behaviours

- **Selection state** lives in `useSelection` (a `Map` keyed by number) and survives
  pagination and search within the page; it is also mirrored to `sessionStorage` under
  `converge:selection:<provider>/<repo>` so an accidental reload does not lose it.
- **Polling**: `useReview(id)` uses `refetchInterval: (q) => q.state.data?.status ===
  'CREATING' ? 2000 : false`. `ReviewPage` renders `ReviewStatus` while `CREATING`,
  `ReviewErrorPanel` on `CONFLICTED`/`FAILED`, and the diff layout on `READY`.
- **Files**: `useReviewFiles(id)` enabled only when `READY`; `FileTree` groups by
  directory with status badge and `+a −d`; the first file auto-selects.
  `useReviewFile(id, path)` fetches the per-file diff; binary and truncated states
  render notices before the diff component.
- **Finish/Discard**: `useFinishReview` mutation → `DELETE`, invalidates review
  queries, navigates to `/`.
- **Vocabulary**: a single `strings.ts` holds product terms; diagnostics text is the
  only place git terms appear (FR-10.11).
- **Loading**: skeletons in content areas; spinners only on submit buttons.
  Errors surface through `toast.error` plus inline `ErrorBanner` for page-level
  failures.

---

## 13. Build, packaging, CI

### 13.1 Makefile and `tools/`

| Target | Does |
|---|---|
| `lint` | `go vet`, `go tool golangci-lint run` (pinned via `tool` directive in go.mod), `npm run lint`, `npm run format:check` |
| `test` | `go test -race -count=1 ./...`, `npm test` |
| `test-integration` | `go test -race -count=1 -tags integration ./...` |
| `build` | `npm ci && npm run build` (outDir → `internal/ui/dist`), then `tools/build-backend.sh` producing `dist/<os>-<arch>/converge{,-cli}` for linux/amd64 and linux/arm64 with `-ldflags "-s -w -X …/buildinfo.Version=$(VERSION)"` |
| `docker-build` | `docker buildx build` with `--build-arg VERSION`, tags `$(IMAGE):$(VERSION)`, `$(IMAGE):$(GIT_SHA)`, and `latest` when `MAINLINE=1` |
| `docker-push` | `tools/docker-push.sh` pushes the tags; `IMAGE` defaults from `IMAGE_REPOSITORY`, else `ghcr.io/$GITHUB_REPOSITORY`, else `$CI_REGISTRY_IMAGE` |
| `version` | `tools/version.sh`: exact `v*` tag → `X.Y.Z`, else `0.0.0-<short sha>` |
| `release-github` | `gh release create $(TAG) dist/*.tar.gz` (used only on tag pipelines) |
| `dev` | runs backend with `go run` and Vite dev server concurrently |

`golangci-lint` is not installed on the development machine; `go tool` makes the pinned
version part of the module so `make lint` works identically everywhere.

### 13.2 Dockerfile

Stage `frontend`: `node:22-alpine`, `npm ci`, `npm run build`. Stage `backend`:
`golang:1.24-alpine`, copy `dist` into `internal/ui/dist`, `CGO_ENABLED=0 go build`
for `TARGETARCH`. Final: `alpine:3.22` + `git ca-certificates`, user `converge`
(uid 10001), `/data/repositories` and `/data/workspaces` owned by it, `EXPOSE 8080`,
`HEALTHCHECK` hitting `/healthz`, entrypoint `converge`. `docker-compose.yml` mounts
two named volumes and passes `.env` via `env_file`.

### 13.3 Pipelines

`.github/workflows/ci.yml`: job `validate` (checkout, setup-go with cache, setup-node
22 with npm cache, `make lint test test-integration build docker-build`); job
`publish` (needs validate; `push` to `main` or `v*` tag) logs into GHCR with
`GITHUB_TOKEN`, runs `make docker-push`, uploads `dist/*.tar.gz` as artifacts, and on
tags `make release-github`. `.gitlab-ci.yml`: stages `validate`, `build`, `publish`
with the same make targets; `publish` uses `docker:cli` + dind service, logs in with
`CI_REGISTRY_USER`/`CI_REGISTRY_PASSWORD`, runs `make docker-push`, and exposes
`dist/` as job artifacts. GitLab creates no Release entry (PRD §9 assumption kept).
Integration tests use only local `file://` repositories, so no network egress is
needed (FR-14.6).

---

## 14. Security and observability summary

- Tokens: `config.Secret`, env-only git credentials, redacting slog handler wrapper
  that scrubs any value equal to a configured token from log attributes as a last line
  of defence.
- Input: every client string validated in `api` and again in domain builders; git
  arguments follow `--` where paths are involved; SHAs and branch names are validated
  before use; numbers are ints.
- Filesystem: writes only under the two roots; cleanup guarded by `EvalSymlinks`.
- Hooks: `core.hooksPath` to an empty directory on every git call; `HOME` isolated.
- Logging: `slog` with `component`, `provider`, `repository`, `session`, `stage`,
  `git.category`, `duration_ms`, `outcome`. `LOG_FORMAT=json` swaps the handler.
- Health: `/healthz` returns version; also reports `git` availability in `checks`.

---

## 15. Resolved open questions

| PRD §9 question | Decision |
|---|---|
| Diff library | `@pierre/diffs` (§12.2) |
| GitHub merged-PR listing | `GET /pulls?state=closed&base=` with `merged_at` filtering, bounded scan, 60 s cache; Search API rejected (§4.2) |
| GitLab fast-forward landing commit | Candidate chain `merge_commit_sha → squash_commit_sha → sha`, verified by parents/patch-id (§4.3, §6.2) |
| Credential injection | `GIT_CONFIG_COUNT` env with URL-scoped `http.<url>.extraheader` (§5.3) |
| Releases | GitHub Release on tags via `make release-github`; GitLab job artifacts only (§13.3) |

## 16. Risks carried into planning

- The bounded GitHub scan can miss very old merged PRs on repositories with > 1,000
  closed PRs; number search bypasses the scan. Documented in README.
- `@pierre/diffs` 1.x component props must be confirmed against the installed version
  during implementation; the fallback library is named.
- GitHub fetch-by-SHA is undocumented; the manual checklist includes a squash PR whose
  landing commit was force-removed from the branch to exercise `MISSING_COMMITS`.
- Cherry-picking merge commits with `-m 1` onto a base other than the merge's first
  parent can produce spurious conflicts (risks.md). Diagnostics include the strategy
  and source SHA so users can identify it.

## 17. Suggested implementation order (input to `/plan-task`)

1. Module scaffold, `buildinfo`, `config` (+tests), `gitx` runner and validators.
2. `provider` model, `fake`, `github`, `gitlab` with fixture tests.
3. `mirror`, `workspace`, `testutil/repo` harness.
4. `review` landing/resolve/apply, `diff`, `session`, `Service`; integration tests.
5. `converge-cli` end to end against the harness (proof of concept milestone).
6. `jsonapi`, `api` handlers, embedded UI shell, `converge` server.
7. Frontend scaffold, services/hooks, pages, diff view, Vitest suites.
8. Makefile, Dockerfile, compose, GitHub Actions, GitLab CI, README/CLAUDE.md.
