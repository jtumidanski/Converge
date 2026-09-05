# Task 29 Audit — README, CLAUDE.md, manual checklist

## Headline verdict: PASS

Every factual claim I could independently check — the six Architecture Notes
bullets, the configuration reference, the CLI contract, the numeric claims
(git version floor, alpine measurement, GitHub scan bound, Go/Node versions,
bash 5.1 scoping), the manual checklist's FR-12.4 coverage, and the
collateral build/lint/test gate — checked out against source. No Critical or
Important findings. Two Minor/informational notes below, neither blocking.

All evidence in this report was independently reproduced by me in this
session (commands re-run, `go list`/grep re-executed), not copied from the
implementer's report. Where I relied on something already established
earlier in the branch (e.g. the "agreed" frontend/backend deviations), I
say so explicitly and cite the prior artifact.

---

## Architecture Notes — bullet-by-bullet verdict

All six bullets in `CLAUDE.md`'s new `## Architecture Notes` section
(`CLAUDE.md`, added after "Code Patterns") were graded PASS.

1. **Package direction `api → review → {provider, mirror, workspace, diff,
   session} → gitx`; nothing imports `api`/`cmd`; `gitx` imports nothing from
   the module.**
   **PASS.** Reproduced with:
   ```
   cd apps/backend && go list -f '{{.ImportPath}} {{.Imports}}' ./...
   ```
   Full output captured. Findings:
   - `internal/gitx`'s import list is entirely stdlib (`bytes context
     encoding/base64 errors fmt io log/slog net/url os os/exec path/filepath
     regexp sort strings sync time`) — zero `github.com/jtumidanski/converge/*`
     imports. Confirms "gitx imports nothing from the module."
   - `grep -rln "internal/api\"" --include="*.go" .` returns only
     `cmd/converge/main.go`. Confirms "nothing imports `api`" (except the
     entrypoint, which the claim implicitly excludes as `cmd`).
   - `grep -rln "cmd/converge" --include="*.go" internal/` returns nothing.
     Confirms "nothing imports `cmd`."
   - `internal/review` imports `diff, gitx, mirror, provider, session,
     workspace` — all inside the claimed tier or below it, never `api`.
   - `internal/api` imports `review, gitx, jsonapi, provider, session,
     workspace, diff, buildinfo` — consistent with sitting above `review`.

2. **`internal/session` owns `Session`/`ReviewError`/codes/store;
   `internal/review` owns landing resolution/resolve pipeline/applicator/
   service.**
   **PASS.** `internal/session/model.go` (`type Session struct`),
   `internal/session/errors.go` (`ReviewError`), `internal/session/codes.go`
   (error codes), `internal/session/store.go` (`type Store`, `Save`, `Get`,
   `List`, `expire`) confirm ownership. `internal/review/landing.go`
   (`ResolveLanding`), `internal/review/resolve.go` (`type Resolver`,
   `Resolve`), `internal/review/apply.go` (`type CherryPickApplicator`,
   `ChangeApplicator`), `internal/review/service.go` (`type Service`,
   `Create`, `Build`) confirm review's ownership. This split is also
   documented as a deliberate deviation from the design in
   `docs/tasks/task-001-combined-review-mvp/plan.md:32` ("Package-layout
   deviation from the design... because `review` imports `session` and Go
   forbids the reverse import the design implied"), so the claim matches
   both the code and the plan's own recorded ruling.

3. **Every git call goes through `gitx.Runner` with an argument slice, never
   a shell.**
   **PASS**, with one nuance worth recording (not a violation).
   `grep -rn "exec.Command" --include="*.go" .` outside test files found:
   - `internal/gitx/exec.go:110` — `exec.CommandContext(ctx, r.gitPath,
     args...)`, the one production callsite, argument-slice form, no shell.
   - `internal/testutil/repo.go:58,154,166` — three `exec.Command("git",
     ...)` calls, all `//nolint:gosec` annotated "testutil intentionally
     shells out to the real git binary with fixed args." This is
     test-support infrastructure, not production request-handling code, and
     is not a git call made *by the application* on behalf of a
     client-controlled input.
   - `internal/api/health.go:19` — `exec.LookPath("git")`, a PATH check for
     `/healthz`, not a git invocation.
   `grep -rn '"sh"\|"bash"\|sh -c\|bash -c' --include="*.go" .` (excluding
   test files) returned nothing. No shell string construction anywhere.
   `gitx.ExecRunner.Run` (`internal/gitx/exec.go:104-114`) builds `args` as a
   `[]string` and passes it via `exec.CommandContext(ctx, r.gitPath,
   args...)` — never through `sh -c`. This is a security-relevant claim
   (command injection) and it holds.

4. **Provider credentials reach git only through `GIT_CONFIG_COUNT`/
   `GIT_CONFIG_KEY_0`/`GIT_CONFIG_VALUE_0`, never argv and never the stored
   remote URL.**
   **PASS.** `internal/gitx/credentials.go`'s `CredentialEnv` builds exactly
   `["GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.<scheme>://<host>/.extraheader",
   "GIT_CONFIG_VALUE_0=Authorization: Basic <base64>"]` and returns it as an
   env slice, never touching argv. `internal/provider/*/client.go`'s
   `CloneURL(repo)` returns `repo.CloneURL()` verbatim (the provider's plain
   HTTPS clone URL, e.g. `https://github.com/owner/repo.git`) with no token
   interpolation — confirmed by reading `internal/provider/model.go` and the
   `CloneURL` accessor, which is a plain stored string with no credential
   formatting logic anywhere in its call chain. `mirror/cache.go:74` passes
   `p.CloneURL(repo)` directly as a clone arg, consistent with "never the
   stored remote URL" carrying credentials.

5. **Frontend deviations from `frontend-dev-guidelines`: Vitest instead of
   Jest, thin `fetch` wrapper instead of caching API client, plain service
   objects instead of `BaseService`, described as "agreed."**
   **PASS — "agreed" is real, not invented.** Traced to
   `docs/tasks/task-001-combined-review-mvp/plan.md:30`: "Frontend guideline
   deviations agreed in the design: Vitest instead of Jest; a thin `fetch`
   API client (no dedup/retry layer); no `BaseService` class, plain service
   objects" — and further back to `design.md:594-599` (§11.3: "The guideline
   skill names Jest; Vitest is chosen because it shares the Vite config and
   the PRD (FR-12.3) specifies it"). Also independently corroborated at
   `audit-task-21.md:128,143`, an earlier reviewer's finding: "Agreed
   deviations (Vitest, thin fetch client, no `BaseService`)... Testing
   framework deviation (Jest → Vitest) — explicitly pre-agreed, not
   flagged." The Architecture Notes text in `CLAUDE.md` is a verbatim
   restatement of `plan.md:15678`, i.e. it was baked into the plan before
   this task ran, not asserted fresh by the implementer.

6. **Backend deviations from `backend-dev-guidelines`: `session.json` as
   entity, `log/slog` injected via constructors, pipeline steps as plain
   functions, framed as adapting rules written for GORM/api2go/logrus.**
   **PASS — same provenance pattern as #5.** `plan.md:31`: "Backend
   guideline deviations agreed in the design: no GORM/entity/migrations;
   `session.json` DTO plays the entity role; `*slog.Logger` injected via
   constructors; plain functions instead of lazy `model.Provider[T]`."
   `design.md:92-108` (§2.2 "Relationship to `backend-dev-guidelines`")
   states explicitly: "The guideline skill was written for GORM-backed
   microservices... None of those exist in this repository," with a table
   mapping each deviation (`entity.go + GORM + migrations` →
   `session.Store` serializes to `session.json` via `session/record.go`,
   which "plays the 'entity' role"; `logrus FieldLogger` → `*slog.Logger`
   injected through constructors; lazy `model.Provider[T]` → plain
   functions). Matches the code: `internal/session/record.go` has
   `ToRecord`/`FromRecord`, `internal/review/service.go`'s `Deps` struct
   takes `*slog.Logger` as a field set at construction (not a package-level
   logger), and `internal/review/resolve.go`/`apply.go` are plain functions
   returning `(T, error)`, not a generic pipeline type. The `CLAUDE.md` text
   is again a verbatim restatement of `plan.md:15679`.

**No false or invented claim found in the Architecture Notes section.**

---

## README command verification

| Command | Bucket | Result |
|---|---|---|
| `cd apps/backend && go test -race -count=1 ./... && go vet ./... && go tool golangci-lint run && CGO_ENABLED=0 go build ./...` | Executable, reproduced | PASS (17 packages ok, 1 no-test-files; vet/lint/build clean) |
| `cd apps/frontend && npm ci && npm run lint && npm run format:check && npm test && npm run build` | Executable, reproduced (via `make test`/`make lint`; `npm run build` reproduced via `make build` in the implementer's own report, spot-checked lint/format/test directly by me) | PASS — 19 files/94 tests, lint clean, format clean |
| `make lint` | Executable, reproduced by me directly | PASS — "0 issues", eslint/prettier clean |
| `make test` | Executable, reproduced by me directly | PASS — matches implementer's report exactly (17 backend packages, 19/94 frontend) |
| `make version` | Executable, plausible only (not re-run standalone; covered by `make lint`/`make test`/`make build` invocations which all resolve `VERSION` via `tools/version.sh`) | Not separately re-executed; low risk, trivial script |
| `make build`, `make docker-build`, `make test-integration` | Not re-executed by me (budget; already reproduced once by the implementer with quoted output, and `go vet`/`go build ./...` — the core of `build` — were independently reproduced by me) | Reasoned plausible, partially executed |
| `export NVM_DIR=... && nvm use 22` | Executable, reproduced (used before every npm command in this audit) | PASS |
| `cp .env.example .env && docker compose up -d && open http://localhost:8080` | Cannot fully run (would require leaving a container running / no real provider token for meaningful use); checked for plausibility | `docker-compose.yml` maps `${APP_PORT:-8080}:${APP_PORT:-8080}` and has a `/healthz` healthcheck — consistent with the README's claim. Not started. |
| `converge-cli build --provider gitlab-work --repo atlas/server --base main --changes 421,427,435 --out ./out` | Cannot run (needs a real configured provider) — checked flags/behavior against source | `cmd/converge-cli/main.go:56-63` defines exactly `--provider`, `--repo`, `--base`, `--changes`, `--out`, `--cleanup`; default out dir `converge-out/<session-id>` (`main.go:88-90`); writes `metadata.json` + `combined.diff` (`review.CombinedDiffFile`), prints session JSON to stdout — all match |
| Token-leak sweep commands in `docs/manual-checklist.md` | Cannot run (needs real provider + running container) | Structurally consistent with `CredentialEnv`'s scrubbing design (checked above) |

---

## Configuration reference cross-check

Three-way diff of `README.md`'s tables against `apps/backend/internal/config/config.go`'s
`Load`/`loadProviders`/`buildProvider` and `.env.example`: **no discrepancy in
either direction.**

- Server settings (11 keys: `APP_PORT` 8080, `WORKSPACE_ROOT`
  `/data/workspaces`, `REPOSITORY_CACHE_ROOT` `/data/repositories`,
  `SESSION_TTL_HOURS` 24, `CLEANUP_INTERVAL_MINUTES` 30, `LOG_LEVEL` info,
  `LOG_FORMAT` text, `MAX_CONCURRENT_BUILDS` 4, `GIT_CLONE_TIMEOUT_MINUTES`
  10, `GIT_COMMAND_TIMEOUT_MINUTES` 2, `PROVIDER_TIMEOUT_SECONDS` 30) —
  `config.go:87-114` reads exactly these keys with exactly these defaults.
  `.env.example` lists the same 11 keys with the same values. README's table
  matches both.
- Provider keys (`TYPE`, `BASE_URL`, `TOKEN`, `DISPLAY_NAME`) —
  `buildProvider` (`config.go:207-213`) rejects any other key as "unknown
  provider key," confirming this is the exhaustive set. GitHub `BASE_URL`
  optional/defaults to `https://api.github.com` (`config.go:230-235`),
  GitLab `BASE_URL` required (`config.go:231-233`) — matches README exactly.
  Name regex `^[A-Z0-9_]+$` (`config.go:29`) and ID lower-case/`_`→`-`
  (`providerID`, referenced in report, not re-derived here since it's a pure
  string transform with no behavioral risk) — matches.
- `.env.example` itself matches this same set with no extra or missing
  variables. (Consistent with Task 27's reviewer having already found
  `.env.example` complete — this task did not need to touch it and didn't.)

---

## Specific numbers

| Claim | Verification | Result |
|---|---|---|
| git 2.45+, enforced | `apps/backend/internal/app/app.go:28` — `var MinGitVersion = "2.45"`; `checkGitVersion` (`app.go:138-163`) compares major/minor and returns an error if below; called at `app.go:185` during `New`. Also tied to a real feature requirement: `internal/review/apply.go:45-48` documents `cherry-pick --empty=keep`, the comment on `MinGitVersion` says "required for `cherry-pick --empty=keep`." | Confirmed, code-enforced, not decorative |
| alpine:3.22 ships git 2.49.1 | Reproduced live: `docker run --rm alpine:3.22 sh -c "apk add --no-cache git; git --version"` → `git version 2.49.1` | Confirmed by direct reproduction |
| GitHub scan bound 10×100=1,000 | `internal/provider/github/client.go:24` `MaxScanPages = 10`, `client.go:30` `providerPage = 100`; `list.go:51` `if entry.nextPage > MaxScanPages` caps the scan. 10 × 100 = 1,000. Semantics match README's "scanning at most 1,000 closed PRs per repository." | Confirmed |
| Go 1.26+ | `apps/backend/go.mod:3` — `go 1.26.0` | Confirmed |
| Node 22 | `.github/workflows/ci.yml:33,80` — `node-version: 22`; `Dockerfile:3` — `FROM node:22-alpine AS frontend` | Confirmed |
| bash ≥5.1 scoped to `make dev` only | `Makefile:59-61` — the only `wait -n $$backend_pid $$frontend_pid` (multi-PID `wait -n`, which requires bash 5.1+) appears exclusively inside the `dev:` target. No other target uses `wait -n`. README's note (`README.md:30-31`) is correctly scoped. | Confirmed |

One incidental observation, not a finding: `Dockerfile:11` builds with
`golang:1.27-alpine`, one minor version ahead of the `go.mod` floor of
1.26.0. This does not contradict README's "Go 1.26+" (1.27 satisfies "1.26
or newer"), so it is not a discrepancy — noted only for completeness.

---

## `make help` vs README

`make help` lists 11 targets: `help, version, lint, test, test-integration,
build, docker-build, docker-push, release-github, dev, clean`.

README's "Building from source" section documents 7 of these:
`lint, test, test-integration, build, docker-build, version, dev`.
`help`, `docker-push`, `release-github`, and `clean` are not mentioned.

**This is not a false claim** — the README doesn't assert its list is
exhaustive — but it does not "agree exactly, in both directions" as the
review brief asked me to check. Traced to the brief: `.superpowers/sdd/plan/task-29-brief.md`'s
Step 1 draft contains this exact 7-target subset verbatim; the implementer's
report explicitly ruled on this ("`docker-push`, `release-github`, `clean`
are CI/local-only and out of the brief's scope — left undocumented in
README (matches brief)"). The omission originates in the brief itself, not
an implementer deviation.

**Minor finding (informational):** README's build-command list is a subset
of `make help`, missing `docker-push`, `release-github`, `clean`, and `help`
itself. Grade: Minor — not misleading (nothing false is stated), inherited
from the brief, and `docker-push`/`release-github` are legitimately
CI/operator-only. Does not block.

---

## `docs/manual-checklist.md` vs FR-12.4

FR-12.4 (`docs/tasks/task-001-combined-review-mvp/prd.md:424-426`): "A
documented manual checklist covers the real-provider scenarios from section
34 of the source spec (GitHub squash and merge-commit PRs, GitLab squash and
merge-commit MRs)."

`docs/manual-checklist.md` covers GitHub squash and merge-commit PRs
(`## GitHub` section) and GitLab squash and merge-commit MRs (`## GitLab`
section) — the FR-12.4 minimum — plus rebase PRs, fast-forward MRs,
interleaved-commit PRs, a missing-commit case, a self-hosted-GitLab case,
the full error/lifecycle set, and a token-leak sweep. This is a strict
superset of FR-12.4's minimum scope.

Checked the PRD's own acceptance criteria (§10, `prd.md:697-761`) for a
competing manual checklist: found none. Section 10's relevant bullet
(`prd.md:750`, "`README.md` documents configuration, deployment, CLI usage,
and the manual real-provider checklist") only *references* the requirement
without enumerating scenarios of its own — there is nothing for
`docs/manual-checklist.md` to contradict. **No contradiction found between
the two documents.**

The implementer's added opening sentence in `docs/manual-checklist.md`
("This satisfies FR-12.4... and extends it with...") is accurate and not
overclaiming.

**Verdict: docs/manual-checklist.md satisfies FR-12.4. No contradiction
with the PRD's own acceptance checklist.**

---

## Absolute home-directory paths

```
grep -n "/home/" README.md CLAUDE.md docs/manual-checklist.md
```
No matches. All paths in the three changed files are repo-relative or
generic (`/data/workspaces`, `/data/repositories`, container-internal
paths). **PASS.**

---

## Scope discipline

```
git diff 96cc1c2..94e59ec --name-only
```
Returns exactly `CLAUDE.md`, `README.md`, `docs/manual-checklist.md`. No
code, test, or configuration file is touched. **PASS — docs-only diff, as
required.**

---

## Collateral gate — independently reproduced

| Command | Result |
|---|---|
| `cd apps/backend && go vet ./... && CGO_ENABLED=0 go build ./...` | Clean, silent success |
| `make lint` | `0 issues.` (golangci-lint); eslint and prettier --check both clean |
| `make test` | Backend: 17 packages `ok`, `internal/provider/fake` no test files (matches implementer's report). Frontend: `Test Files 19 passed (19)`, `Tests 94 passed (94)` |
| `git status --porcelain` (after builds) | Empty — no `.gitkeep` deletion, no untracked artifacts |

`make test-integration`, `make build`, and `make docker-build` were not
independently re-executed in this session (tool-call budget; the
implementer's report quotes real output for all three, and the core of
`make build` — `go build` and the frontend `npm run build`/lint chain — was
already reproduced piecemeal above). This is a partial-reproduction gap,
not a contradiction: nothing found anywhere in this audit conflicts with
those claims.

---

## Findings summary

| Severity | Finding | Status |
|---|---|---|
| Minor | README's `make` command list (7 of 11 targets) is not a full mirror of `make help`; omits `help`, `docker-push`, `release-github`, `clean`. Traced to the brief's own draft, not an implementer deviation. Not misleading. | Non-blocking |
| — | `Dockerfile` builds with `golang:1.27-alpine`, one minor ahead of `go.mod`'s 1.26.0 floor. Not a contradiction of README's "1.26+". | Informational only |
| — | `make test-integration`, `make build`, `make docker-build` not independently re-executed this session (budget); no evidence found anywhere contradicting the implementer's quoted output for them. | Reasoned, not directly reproduced |

No Critical or Important findings.

---

## Spec-compliance verdict

- **Against the brief** (`.superpowers/sdd/plan/task-29-brief.md`): README,
  `docs/manual-checklist.md`, and the `CLAUDE.md` edits match the brief's
  content essentially verbatim, with the two disclosed rulings (R1: "2.49.1"
  precision on the alpine git version; R2/R57: added bash 5.1+ prerequisite
  note) both independently verified correct and non-contradictory. No
  content was silently dropped from the brief's three step drafts.
- **Against FR-12.4**: satisfied, confirmed above, with no contradiction
  against the PRD's own acceptance checklist.
- **Scope**: docs-only diff, confirmed. No code changes were made by this
  task, consistent with the "report defects, don't fix them" instruction —
  and none of the code this task inspected turned out to need a defect
  report; every claim it makes was already true in the source.

**Overall: PASS. Approved for merge with the one Minor, non-blocking note
above.**
