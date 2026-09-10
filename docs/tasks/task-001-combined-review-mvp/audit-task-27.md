# Task 27 audit — Makefile targets, build scripts, Docker image, compose

## Verdict: PASS

All packaging claims in the implementer's report were independently reproduced. No Critical or
Important findings. Two Minor/informational findings below; neither blocks merge.

---

## Findings

### Minor — `make build` no longer runs a full-repo compile check (`go build ./...`)

`Makefile:33-36` (new `build` target):

```
build: ## Build the UI into the embed directory, then cross-compile the binaries
	cd $(FRONTEND) && $(NPM) ci && $(NPM) run build
	touch $(BACKEND)/internal/ui/dist/.gitkeep
	VERSION=$(VERSION) $(ROOT)/tools/build-backend.sh
```

The old `build` target (`git show 7046173:Makefile`, lines 26-30) additionally ran
`cd $(BACKEND) && CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" ./...` — a compile check of
**every** package in the module, not just `./cmd/converge` and `./cmd/converge-cli`.
`tools/build-backend.sh` only builds the two `cmd` binaries
(`tools/build-backend.sh:42-46`, `for cmd in converge converge-cli`). If a non-`cmd` package
(e.g. an internal package with no importer from either binary) had a compile error, `make build`
alone would no longer catch it — only `make test` would (since `go test ./...` compiles every
package it runs). `make lint` runs `go vet ./...`, which also compiles.

This is not a regression in test coverage overall (both `make test` and `make lint` still compile
every package), and the brief's own Step 2 explicitly specifies this new `build` recipe verbatim
— the implementer's report discloses this exact tradeoff under "Diff review" and "Concerns."
Grading it Minor rather than Important because CLAUDE.md's stated gate (`go vet`, `golangci-lint
run`, `go test`, `CGO_ENABLED=0 go build ./...`) is a separate, still-intact contract — `make
build` was never the thing CLAUDE.md's gate names, and the full-repo build is still exercised by
those other steps in CI per design.md's stated `make lint test test-integration build
docker-build` pipeline.

### Minor — `wait -n` in `dev` requires bash ≥ 5.1; `SHELL := /bin/bash` on macOS defaults to 3.2

`Makefile:55-64`:

```
dev: ## Run the backend and the Vite dev server together
	cd $(BACKEND) && go run ./cmd/converge & \
	backend_pid=$$!; \
	...
	wait -n $$backend_pid $$frontend_pid; \
```

`wait -n <pid> <pid>` (explicit PID arguments to `-n`) requires bash ≥ 5.1; macOS ships `/bin/bash`
3.2 (GPL licensing) by default, where this would fail with `wait: -n: invalid option`. Checked
`docs/tasks/task-001-combined-review-mvp/design.md` and `prd.md` for a stated dev-workstation OS
scope: neither commits to macOS local-dev support. The only portability clause found is
design.md:695, `"Portability: single static binary; image runs on linux/amd64 and linux/arm64"` —
that describes the *deployed artifact*, not the developer's machine running `make dev`. CI
(design.md:727, :731) runs on GitHub Actions/GitLab CI Linux runners, where bash 5.x is standard.
Given the spec is silent on the dev-machine OS and only commits to Linux for CI/deployment, this
is graded **Minor / informational** rather than a defect: it will bite a macOS developer who
hasn't installed a newer bash via Homebrew, but it is out of the stated scope of this task's
contract. Recommend a one-line comment on the `dev` target noting the bash ≥5.1 requirement, but
not a blocking finding.

Ruling 1 in the implementer's report (fixing the brief's bare `wait`, which always returns 0
regardless of child exit status) is itself a correct and verified fix — the controller separately
confirmed `wait -n $p1 $p2` propagates a failing child's exit status on this host (bash 5.2.37).
That specific correctness claim is not in question; only the macOS bash-version portability of the
`-n`-with-PIDs form is flagged here, as a new (pre-existing-brief) side effect of the fix.

---

## Independent reproduction — claim by claim

All commands below were run from the task worktree root (`.worktrees/task-001-combined-review-mvp`)
on the `50a59ec` commit (`git diff 7046173..50a59ec`, 7 files, 206 insertions, matches controller's
pre-verified commit shape).

| Claim | Reproduced | Evidence |
|---|---|---|
| `make version` prints the build version | Yes | `0.0.0-50a59ec` |
| `make build` produces two tarballs, doesn't delete `.gitkeep` | Yes | `dist/converge-0.0.0-50a59ec-linux-amd64.tar.gz` (8.4M), `dist/converge-0.0.0-50a59ec-linux-arm64.tar.gz` (7.7M); `git status --short apps/backend/internal/ui/dist/.gitkeep` empty (unmodified, still tracked) |
| `make docker-build` succeeds (3-stage buildx) | Yes | full buildx log, `exporting to oci image format ... DONE`, `importing to docker ... DONE` |
| Container runs as uid 10001 | Yes | `docker run --rm --entrypoint id converge:0.0.0-50a59ec` → `uid=10001(converge) gid=10001(converge) groups=10001(converge)`; `docker inspect --format '{{.Config.User}}'` → `converge` |
| `/healthz` returns `{"status":"ok",...}` with built version | Yes | `{"status":"ok","version":"0.0.0-50a59ec","checks":{"git":"ok"}}` |
| `/` returns 200 (proves embedded UI, not empty dist) | Yes | `200` |
| HEALTHCHECK wired and starting | Yes | `docker inspect --format '{{json .State.Health}}'` → `{"Status":"starting","FailingStreak":0,"Log":[]}` |
| Compose path: `env_file`, named volumes, healthcheck, `APP_PORT` mapping, service reachable | Yes | `docker compose up -d`; log lines show both example providers (`github`, `gitlab-work`) loaded from `.env`; polled `/healthz` → `{"status":"ok","version":"0.0.0-50a59ec","checks":{"git":"ok"}}`; `docker compose ps` shows `0.0.0.0:8080->8080/tcp` and `health: starting`; `docker compose down -v` removed container, both named volumes, and the network |
| `.env` is git-ignored | Yes | `git check-ignore -v .env` → `.gitignore:32:*.env	.env` |
| `docker-push.sh` fails loudly with no registry vars set | Yes | `env -u IMAGE_REPOSITORY -u GITHUB_REPOSITORY -u CI_REGISTRY_IMAGE bash tools/docker-push.sh` → stderr `docker-push: set IMAGE_REPOSITORY, GITHUB_REPOSITORY or CI_REGISTRY_IMAGE`, `exit=1` |
| `GITHUB_REPOSITORY` case-folds correctly for GHCR image path | Yes (isolated shell check) | `GITHUB_REPOSITORY="JTumidanski/Converge"` → `ghcr.io/jtumidanski/converge` |
| `tools/build-backend.sh` genuinely aborts (not masked) on a build failure | Yes, reproduced directly, not just asserted | `PLATFORMS="linux/bogusarch" bash tools/build-backend.sh` → `go: unsupported GOOS/GOARCH pair linux/bogusarch`, script exit code 2 (`set -euo pipefail` propagates the subshell's failure); no tarball was produced, `dist/linux-bogusarch/` left empty of the two binaries |
| `Makefile build` target aborts (not masked) if an earlier recipe line fails | Yes, reproduced via isolated scratch Makefile (real `npm run build` failure not induced against the repo, to avoid mutating tracked state) | `make -f scratch.mk build` with `false && echo ...` on the first line → `make: *** [scratch.mk:2: build] Error 1`, exit 2, second line's `touch` never ran. This confirms the general Make semantics the real `build` target relies on: `cd $(FRONTEND) && $(NPM) ci && $(NPM) run build` is one recipe line joined with `&&`, so a failing `npm run build` fails that whole line and Make aborts before the `touch .gitkeep` line runs (no `-` prefix on any recipe line in this target) |
| `release-github`'s `||` fallback: both-fail case fails the target | Not executed (no `gh` invocation, no GitHub Release to target — matches report's own disclosure that this is a static-logic confirmation) | Static read: `gh release create ... || gh release upload ...` — shell exit status of an `A || B` compound is B's exit status when A fails; Make has no `-` prefix on the line, so a non-zero B fails the target. Logic checks out on inspection; not independently executed, consistent with what the report says it did |
| `wait -n $p1 $p2` propagates failing-child status | Already verified by the controller (bash 5.2.37 on this host) — not re-derived | N/A, per instructions |
| `make lint` still works | Yes | `go vet ./... && go tool golangci-lint run` → `0 issues.`; frontend `eslint .` clean; `format:check` fails on the 3 pre-existing, disclosed, out-of-scope files (`ChangeTable.tsx`, `SelectChangesPage.test.tsx`, `SelectChangesPage.tsx`) — matches the known/deferred item, not graded here |
| `make test` still works | Yes | Backend: 18 packages `ok`, 1 `[no test files]` (`internal/provider/fake`); Frontend: `19 test files / 94 tests passed` |
| `make help` lists all 10 named targets | Yes | Output lists `help version lint test test-integration build docker-build docker-push release-github dev clean` — exact match to the brief's Interfaces list |
| `make clean` removes `dist/`, `apps/frontend/dist`, and clears `internal/ui/dist` except `.gitkeep` | Yes | Seeded `apps/backend/internal/ui/dist/testfile.tmp` and `dist/dummy.tar.gz`, ran `make clean`; `ls apps/backend/internal/ui/dist/` → only `.gitkeep`; `dist/` no longer exists |
| No previously-working Makefile target lost behavior | Yes | `git diff 7046173..50a59ec -- Makefile`: `help`, `version`, `lint`, `test` unchanged; `test-integration` help text only changed (cosmetic); `build`'s last line changed per brief intent (see Minor finding above, not a silent drop); all other targets are new additions, none removed |
| `.env.example` completeness vs. actual config parser (not the brief's list) | Yes, read `apps/backend/internal/config/config.go` directly | `Load()` (`config.go:79-123`) reads exactly: `APP_PORT`, `WORKSPACE_ROOT`, `REPOSITORY_CACHE_ROOT`, `SESSION_TTL_HOURS`, `CLEANUP_INTERVAL_MINUTES`, `LOG_LEVEL`, `LOG_FORMAT`, `MAX_CONCURRENT_BUILDS`, `GIT_CLONE_TIMEOUT_MINUTES`, `GIT_COMMAND_TIMEOUT_MINUTES`, `PROVIDER_TIMEOUT_SECONDS`, plus `PROVIDERS__<NAME>__{TYPE,BASE_URL,TOKEN,DISPLAY_NAME}` (`loadProviders`/`buildProvider`, `config.go:169-247`). All 11 scalar vars and both `PROVIDERS__*` blocks are present in `.env.example` (lines 5-30). No var read by the parser is missing from the example; no var in the example is absent from the parser (checked both directions). |
| No secrets/tokens on command lines or in logs | Yes | `grep -n "TOKEN"` in `tools/docker-push.sh` and `Makefile`: no matches — neither script ever references a token value; container logs from both the direct-run and compose smoke tests show only `provider configured` lines with `provider=`/`kind=`/`base_url=` fields, no token values logged |
| Cleanup left no stray state | Yes | Images (`converge:0.0.0-50a59ec`, `converge:<sha>`) removed via `docker rmi`; `dist/` removed; `.env` removed; `git status --porcelain` empty before writing this audit file |

---

## Spec compliance against the brief (`task-27-brief.md`)

- **Files created/modified** — all 7 named files present, matching the diff stat (`tools/docker-push.sh`, `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `.env.example` created; `Makefile`, `tools/build-backend.sh` modified). **PASS**
- **`tools/build-backend.sh`** — cross-compiles `converge`/`converge-cli` for `linux/amd64` and `linux/arm64` into `dist/<os>-<arch>/`, tars to `dist/converge-<version>-<os>-<arch>.tar.gz`, reads `VERSION` from env or `tools/version.sh`. Byte-for-byte match to the brief's code block (`tools/build-backend.sh:1-32` vs. brief lines 20-52). **PASS**
- **`tools/docker-push.sh`** — pushes `$IMAGE:$VERSION`, `$IMAGE:$GIT_SHA`, and `$IMAGE:latest` when `MAINLINE=1`; `IMAGE` resolution order `IMAGE_REPOSITORY` → `GITHUB_REPOSITORY` (as `ghcr.io/...`) → `CI_REGISTRY_IMAGE`; fails clearly when none set. Byte-for-byte match to brief. **PASS**
- **Make targets** — `help version lint test test-integration build docker-build docker-push release-github dev clean` all present and functional; `dev` deviates from the brief's illustrative code (bare `wait` → `wait -n` with tracked PIDs) — a deliberate, disclosed, and correctly-reasoned fix for a real defect class (bare `wait` always returns 0). **PASS**, with the `dev`-target bash-version caveat noted above as Minor.
- **`Dockerfile`** — three stages (`frontend` on `node:22-alpine`, `backend` on `golang:1.27-alpine`, final `alpine:3.22`), non-root `converge` uid 10001 owning `/data` (verified live, not just read), `EXPOSE 8080`, `HEALTHCHECK` on `/healthz`. **PASS**, verified by running the container.
- **`docker-compose.yml`** — one service, `env_file: .env`, named volumes `converge-repositories`/`converge-workspaces`, healthcheck, `APP_PORT` port mapping. **PASS**, verified by running compose end-to-end.
- **`.env.example`** — every variable from Global Constraints (verified against the actual parser, not the brief's list) with placeholder values. **PASS**.

## Overall verdict

**PASS.** Every reproducible claim in the implementer's report was independently verified against
real command output (not simulated), including the two hardest-to-check assertions: the container
actually runs as uid 10001, and `/` returns 200 because the image genuinely embeds the built UI
(not an empty `dist/`). The `tools/build-backend.sh` and `Makefile build` failure-propagation
behavior was proven by deliberately breaking each, not merely inspected. No Critical or Important
findings. Two Minor/informational findings recorded above for the fix-loop's awareness; neither
blocks this task.
