# Combined PR/MR Review MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Converge end to end: a Go backend that reconstructs the combined net diff of selected merged GitHub PRs / GitLab MRs, a CLI proof of concept, a React UI, Docker packaging, and CI for GitHub Actions and GitLab CI.

**Architecture:** One Go module (`apps/backend`) with layered packages `api → review → {provider, mirror, workspace, diff, session} → gitx`, two binaries (`converge`, `converge-cli`), filesystem-only persistence, and an embedded Vite/React SPA. Reconstruction shells out to the `git` binary with strict argument validation; providers are hidden behind a `GitProvider` interface with an in-memory fake for tests.

**Tech Stack:** Go 1.26 (module) built with the local 1.27 toolchain, `log/slog`, `net/http` ServeMux, golangci-lint v2 via `go tool`; React 19, TypeScript strict, Vite, Tailwind CSS v4, shadcn/ui, TanStack Query v5, react-router, react-hook-form + Zod, `@pierre/diffs` 1.4, Vitest + Testing Library + MSW; Docker multi-stage on `alpine:3.22`; GitHub Actions + GitLab CI.

**Spec:** `docs/tasks/task-001-combined-review-mvp/design.md` (PRD: `docs/tasks/task-001-combined-review-mvp/prd.md`, risks: `risks.md`). The plan argues from the design; executors read both.

## Global Constraints

Every task's requirements implicitly include this section. Values are copied from the spec.

- Worktree: all work happens in the task worktree (`git rev-parse --show-toplevel` must end with `.worktrees/task-001-combined-review-mvp`, branch `task-001-combined-review-mvp`). Prefix every shell command with `cd <worktree-root> && ...`. Never `git add -A` or `git add .`; add named paths.
- Go module path `github.com/jtumidanski/converge` rooted at `apps/backend`. `go.mod` says `go 1.26` (raised from the design's 1.24 because `golangci-lint` v2.13.2, pinned via the `tool` directive, requires Go 1.26). `CGO_ENABLED=0` always; no CGO dependencies.
- Backend gate (cwd `apps/backend`): `go test -race -count=1 ./...`, `go vet ./...`, `go tool golangci-lint run`, `CGO_ENABLED=0 go build ./...` all clean. Integration tests: `go test -race -count=1 -tags integration ./...`.
- Frontend gate (cwd `apps/frontend`): `npm ci`, `npm run lint`, `npm run format:check`, `npm test`, `npm run build` all clean. Load Node with `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22` if `npm` is missing.
- Git binary: startup requires git ≥ 2.45 (`cherry-pick --empty=keep`). Local git is 2.55; Docker final image `alpine:3.22` ships git 2.49.
- Configuration is environment-only: `APP_PORT=8080`, `WORKSPACE_ROOT=/data/workspaces`, `REPOSITORY_CACHE_ROOT=/data/repositories`, `SESSION_TTL_HOURS=24`, `CLEANUP_INTERVAL_MINUTES=30`, `LOG_LEVEL=info`, `LOG_FORMAT=text`, `MAX_CONCURRENT_BUILDS=4`, `GIT_CLONE_TIMEOUT_MINUTES=10`, `GIT_COMMAND_TIMEOUT_MINUTES=2`, `PROVIDER_TIMEOUT_SECONDS=30`, providers as `PROVIDERS__<NAME>__{TYPE,BASE_URL,TOKEN,DISPLAY_NAME}`.
- Tokens never appear in logs, API responses, `session.json`, git argv, or `git remote get-url origin`. Credentials reach git only through `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_0`/`GIT_CONFIG_VALUE_0` environment variables.
- All git execution uses `exec.CommandContext` with argument slices, never a shell. Every git invocation carries `-c core.hooksPath=<empty dir> -c commit.gpgsign=false -c protocol.file.allow=always`, `GIT_TERMINAL_PROMPT=0`, `GIT_CONFIG_NOSYSTEM=1`, `LC_ALL=C`, an isolated `HOME`, and identity `Converge Review <converge@localhost>`.
- Client input validation: repo full name `^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$` with no `..` segment and no leading `/`, `-`, `.`; SHA `^[0-9a-f]{40}$`; branch names pre-checked by regexp then `git check-ref-format --branch`; change numbers positive ints, ≤ 50 per request, de-duplicated. Anything starting with `-` from a client is rejected.
- Error codes (exact strings): `NOT_MERGED`, `INCOMPATIBLE_TARGETS`, `NOT_ON_BASE_BRANCH`, `MISSING_COMMITS`, `BASE_UNDETERMINED`, `CONFLICT`, `PROVIDER_AUTH`, `PROVIDER_UNAVAILABLE`, `REPOSITORY_UNAVAILABLE`, `GIT_FAILURE`, `INTERRUPTED`, plus request validation `INVALID_PROVIDER`, `INVALID_REPOSITORY`, `INVALID_BRANCH`, `INVALID_CHANGES`, and `REVIEW_NOT_READY`.
- Session states (exact strings): `CREATING`, `READY`, `CONFLICTED`, `FAILED`, `FINISHED`, `EXPIRED`. Stages: `resolving`, `updating-repository`, `creating-workspace`, `applying:<number>`, `diffing`. Session IDs are 8 lowercase hex chars from `crypto/rand`.
- UI vocabulary outside Diagnostics is limited to: Provider, Repository, Base, Included PRs/MRs, Combined Review, Conflict, Finish Review, Discard Review. The words worktree, cherry-pick, synthetic branch appear only inside the Diagnostics section.
- JSON:API: resources `{ "data": { "type", "id", "attributes" } }`, lists carry `meta.page`, errors `{ "errors": [ { "status", "code", "title", "detail", "meta" } ] }`, response content type `application/vnd.api+json`, requests accept that or `application/json`.
- Frontend guideline deviations agreed in the design: Vitest instead of Jest; a thin `fetch` API client (no dedup/retry layer); no `BaseService` class, plain service objects.
- Backend guideline deviations agreed in the design: no GORM/entity/migrations; `session.json` DTO plays the entity role; `*slog.Logger` injected via constructors; plain functions instead of lazy `model.Provider[T]`.
- Package-layout deviation from the design (recorded here so reviewers do not flag it): the `Session` model, builder, `ReviewError` and codes live in `internal/session` (with the store) rather than `internal/review`, because `review` imports `session` and Go forbids the reverse import the design implied. `review` owns landing/resolve/apply/service/messages.
- Commit messages use conventional prefixes and the task id, e.g. `feat(task-001): ...`, `test(task-001): ...`, `chore(task-001): ...`.

---

## File Structure

### Backend (`apps/backend`)

| Path | Responsibility |
|---|---|
| `go.mod`, `go.sum` | Module `github.com/jtumidanski/converge`, `go 1.26`, `tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint` |
| `.golangci.yml` | v2 lint config (`errcheck`, `govet`, `staticcheck`, `unused`, `revive`, `gosec`, `errorlint`, `misspell`) |
| `cmd/converge/main.go` | HTTP server: config, wiring, store recovery, sweep goroutine, graceful shutdown |
| `cmd/converge-cli/main.go` | CLI `build` subcommand and `--version` |
| `internal/buildinfo/buildinfo.go` | `Version` var set by ldflags |
| `internal/config/{config.go,secret.go,errors.go}` + tests | `Load(env []string)`, `Secret`, `*Error{Variable,Reason}` |
| `internal/gitx/{spec.go,exec.go,fake.go,validate.go,credentials.go,lock.go,redact.go}` + tests | Runner, validators, credential env, LockMap, redaction |
| `internal/provider/{provider.go,model.go,builder.go,errors.go,registry.go,page.go}` + tests | Interface, immutable model + builders, sentinel errors, Registry |
| `internal/provider/github/{client.go,mapping.go,list.go}` + `testdata/*.json` + tests | REST v3 client |
| `internal/provider/gitlab/{client.go,mapping.go}` + `testdata/*.json` + tests | REST v4 client |
| `internal/provider/fake/fake.go` | In-memory provider for tests |
| `internal/mirror/{cache.go,objects.go}` + tests | Mirror path layout, clone/update/fetch-sha, `ObjectReader` |
| `internal/workspace/manager.go` + tests | Worktree create/cleanup with path guards |
| `internal/diff/{diff.go,parse.go}` + tests | combined.diff, summary parsing, per-file diff |
| `internal/session/{model.go,builder.go,errors.go,codes.go,id.go,record.go,store.go,sweep.go}` + tests | Session model, ReviewError, DTO, atomic store, recovery, sweep |
| `internal/review/{input.go,messages.go,landing.go,resolve.go,apply.go,cleaner.go,service.go}` + tests | Validation, human messages, landing resolution, resolve pipeline, applicator, cleaner, orchestrator |
| `internal/testutil/repo.go` | Scripted bare git repositories for integration tests |
| `internal/review/integration_test.go` | `//go:build integration` scenarios (FR-12.2) |
| `internal/app/app.go` | Shared wiring for both binaries: preflight, runner, registry, store, service |
| `internal/jsonapi/{document.go,decode.go,errors.go}` + tests | JSON:API encode/decode |
| `internal/api/{router.go,middleware.go,errors.go,providers.go,repositories.go,changes.go,reviews.go,review_files.go,health.go,ui.go}` + tests | HTTP handlers |
| `internal/ui/{embed.go,dist/.gitkeep}` | `embed.FS` of the built SPA |

### Frontend (`apps/frontend`)

| Path | Responsibility |
|---|---|
| `package.json`, `vite.config.ts`, `tsconfig*.json`, `eslint.config.js`, `.prettierrc`, `components.json`, `src/index.css` | Tooling |
| `src/main.tsx`, `src/App.tsx`, `src/routes.tsx` | Providers and routes |
| `src/types/api/jsonapi.ts`, `src/types/models/*.ts` | JSON:API document types and resource models |
| `src/lib/api/{client.ts,errors.ts}` | fetch wrapper and `ApiError` |
| `src/services/api/{providers.ts,repositories.ts,changes.ts,reviews.ts,index.ts}` | Service objects |
| `src/lib/hooks/api/{useProviders.ts,useRepositories.ts,useChanges.ts,useReviews.ts}` | Query hooks with key factories |
| `src/lib/hooks/useSelection.ts` | Selection map persisted in `sessionStorage` |
| `src/lib/schemas/repository.ts` | Zod schema for manual repo entry |
| `src/lib/strings.ts`, `src/lib/utils.ts` | Product vocabulary, `cn()` |
| `src/components/ui/*` | shadcn primitives |
| `src/components/common/{PageHeader,EmptyState,ErrorBanner,Pagination}.tsx` | Shared presentational |
| `src/components/features/providers/ProviderPicker.tsx` | Provider selection |
| `src/components/features/repositories/{RepositoryList,ManualRepositoryForm}.tsx` | Repository selection |
| `src/components/features/changes/{ChangeTable,ChangeSearch,SelectionBar}.tsx` | Change selection |
| `src/components/features/review/{ReviewStatus,ReviewErrorPanel,ReviewHeader,FileTree,FileDiff}.tsx` | Review views |
| `src/pages/{SelectRepositoryPage,SelectChangesPage,ReviewPage}.tsx` | Routes |
| `src/test/{setup.ts,server.ts,render.tsx}` | Vitest setup, MSW server, render helper |

### Root

| Path | Responsibility |
|---|---|
| `Makefile`, `tools/{version.sh,build-backend.sh,docker-push.sh}` | All CI logic |
| `Dockerfile`, `docker-compose.yml`, `.env.example`, `.dockerignore` | Packaging |
| `.github/workflows/ci.yml`, `.gitlab-ci.yml` | Pipelines |
| `README.md`, `CLAUDE.md`, `docs/manual-checklist.md` | Docs |

---

## Phase A — Foundations

### Task 1: Backend module scaffold, buildinfo, lint config, Makefile skeleton

**Files:**
- Create: `apps/backend/go.mod`, `apps/backend/.golangci.yml`, `apps/backend/internal/buildinfo/buildinfo.go`, `apps/backend/internal/buildinfo/buildinfo_test.go`, `apps/backend/internal/ui/embed.go`, `apps/backend/internal/ui/dist/.gitkeep`, `apps/backend/internal/ui/ui_test.go`
- Create: `Makefile`, `tools/version.sh`
- Modify: `.gitignore` (add `apps/backend/internal/ui/dist/*` with `!.gitkeep` exception, `dist/`)

**Interfaces:**
- Produces: `buildinfo.Version string` (default `"dev"`); `ui.FS() fs.FS` returning the embedded `dist` subtree; `ui.Present() bool` true when `index.html` exists; `tools/version.sh` printing `X.Y.Z` or `0.0.0-<short sha>`; make targets `version`, `lint`, `test`, `build` (backend part only for now).

- [ ] **Step 1: Initialise the module and pin golangci-lint as a tool**

```bash
cd <worktree-root> && mkdir -p apps/backend && cd apps/backend && go mod init github.com/jtumidanski/converge
go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
go mod tidy
```

Then edit `go.mod` so the `go` directive reads `go 1.26` (the tool pulls it up to 1.26 automatically; if it wrote `1.26.0` leave it). Confirm `go tool golangci-lint --version` prints `2.13.2`.

- [ ] **Step 2: Write `.golangci.yml`**

```yaml
version: "2"
run:
  timeout: 5m
  build-tags:
    - integration
linters:
  default: none
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    - ineffassign
    - errorlint
    - gosec
    - misspell
    - revive
    - unconvert
  settings:
    gosec:
      excludes:
        - G204 # subprocess launched with variable: every git call is validated argument-slice exec
    revive:
      rules:
        - name: exported
          disabled: true
  exclusions:
    rules:
      - path: _test\.go
        linters:
          - gosec
          - errcheck
formatters:
  enable:
    - gofmt
    - goimports
```

- [ ] **Step 3: Write the failing buildinfo test**

`apps/backend/internal/buildinfo/buildinfo_test.go`:

```go
package buildinfo

import "testing"

func TestVersionDefaultsToDev(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("Version = %q, want dev", Version)
	}
}
```

- [ ] **Step 4: Run it to see it fail**

Run: `cd <worktree-root>/apps/backend && go test ./internal/buildinfo/`
Expected: FAIL (package does not compile: `Version` undefined).

- [ ] **Step 5: Implement buildinfo**

`apps/backend/internal/buildinfo/buildinfo.go`:

```go
// Package buildinfo exposes the version compiled into the binary.
package buildinfo

// Version is overridden at link time with
// -ldflags "-X github.com/jtumidanski/converge/internal/buildinfo.Version=<version>".
var Version = "dev"
```

Run: `go test ./internal/buildinfo/` → PASS.

- [ ] **Step 6: Write the failing embedded UI test**

`apps/backend/internal/ui/ui_test.go`:

```go
package ui

import (
	"io/fs"
	"testing"
)

func TestFSIsReadableAndPresentReflectsIndex(t *testing.T) {
	entries, err := fs.ReadDir(FS(), ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// dist/ only holds .gitkeep in a source checkout, so Present must be false.
	if Present() {
		t.Fatalf("Present() = true with entries %v, want false without index.html", entries)
	}
}
```

- [ ] **Step 7: Implement the embed package**

`apps/backend/internal/ui/dist/.gitkeep`: empty file.

`apps/backend/internal/ui/embed.go`:

```go
// Package ui embeds the built frontend (apps/frontend → internal/ui/dist).
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the dist directory as the root of a filesystem.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("ui: dist directory missing from embed: " + err.Error())
	}
	return sub
}

// Present reports whether a built index.html is embedded.
func Present() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
```

Run: `go test ./internal/ui/` → PASS.

- [ ] **Step 8: Write `tools/version.sh` and the Makefile skeleton**

`tools/version.sh` (mode 755):

```bash
#!/usr/bin/env bash
# Prints X.Y.Z when HEAD is exactly tagged vX.Y.Z, otherwise 0.0.0-<short sha>.
set -euo pipefail
tag="$(git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || true)"
if [[ "$tag" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
  echo "${BASH_REMATCH[1]}"
else
  echo "0.0.0-$(git rev-parse --short=7 HEAD)"
fi
```

`Makefile` (tabs for recipes):

```make
SHELL := /bin/bash
.DEFAULT_GOAL := help

ROOT      := $(shell git rev-parse --show-toplevel)
BACKEND   := $(ROOT)/apps/backend
FRONTEND  := $(ROOT)/apps/frontend
VERSION   ?= $(shell $(ROOT)/tools/version.sh)
GIT_SHA   ?= $(shell git rev-parse HEAD)
LDFLAGS   := -s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=$(VERSION)

.PHONY: help version lint test test-integration build

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

version: ## Print the build version
	@echo $(VERSION)

lint: ## Lint backend (frontend added in Task 21)
	cd $(BACKEND) && go vet ./... && go tool golangci-lint run

test: ## Unit tests
	cd $(BACKEND) && go test -race -count=1 ./...

test-integration: ## Integration tests (local git only, no network)
	cd $(BACKEND) && go test -race -count=1 -tags integration ./...

build: ## Build backend binaries into dist/ (frontend build wired in Task 21)
	cd $(BACKEND) && CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" ./...
```

- [ ] **Step 9: Update `.gitignore`**

Append:

```
# Built frontend embedded into the backend (rebuilt by make build)
apps/backend/internal/ui/dist/*
!apps/backend/internal/ui/dist/.gitkeep

# Release artifacts
/dist/
```

- [ ] **Step 10: Verify the gate**

Run: `cd <worktree-root> && chmod +x tools/version.sh && make version && make lint && make test && make build`
Expected: version prints `0.0.0-<sha>`; lint, test, build succeed.

- [ ] **Step 11: Commit**

```bash
cd <worktree-root> && git add apps/backend/go.mod apps/backend/go.sum apps/backend/.golangci.yml apps/backend/internal/buildinfo apps/backend/internal/ui Makefile tools/version.sh .gitignore
git commit -m "chore(task-001): scaffold Go module, buildinfo, embedded UI stub, Makefile"
```

---

### Task 2: Configuration package

**Files:**
- Create: `apps/backend/internal/config/secret.go`, `errors.go`, `config.go`, `secret_test.go`, `config_test.go`

**Interfaces:**
- Produces:
  - `type Secret struct{ v string }`; `NewSecret(string) Secret`; `(Secret) Reveal() string`; `(Secret) String() string` and `MarshalJSON` return `[redacted]`; `(Secret) LogValue() slog.Value`; `(Secret) IsZero() bool`.
  - `type Error struct{ Variable, Reason string }` implementing `error` as `config: <Variable>: <Reason>`.
  - `type Kind string` with `KindGitHub = "github"`, `KindGitLab = "gitlab"`.
  - `type ProviderConfig struct{ ID, Name, DisplayName, BaseURL string; Kind Kind; Token Secret }`.
  - `type Config struct{ Port int; WorkspaceRoot, RepositoryCacheRoot string; SessionTTL, CleanupInterval, GitCloneTimeout, GitCommandTimeout, ProviderTimeout time.Duration; LogLevel slog.Level; LogFormat string; MaxConcurrentBuilds int; Providers []ProviderConfig }`.
  - `func Load(env []string) (Config, error)`.
  - `func (c Config) Provider(id string) (ProviderConfig, bool)`; `func (c Config) Secrets() []string` (all token values, for log redaction).

- [ ] **Step 1: Write failing Secret tests**

`apps/backend/internal/config/secret_test.go`:

```go
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretNeverLeaks(t *testing.T) {
	s := NewSecret("tok-123")
	if s.Reveal() != "tok-123" {
		t.Fatalf("Reveal = %q", s.Reveal())
	}
	for name, out := range map[string]string{
		"String": s.String(),
		"%v":     fmt.Sprintf("%v", s),
		"%+v":    fmt.Sprintf("%+v", s),
		"%s":     fmt.Sprintf("%s", s),
	} {
		if strings.Contains(out, "tok-123") || out != "[redacted]" {
			t.Errorf("%s = %q, want [redacted]", name, out)
		}
	}
	b, err := json.Marshal(struct{ T Secret }{s})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"T":"[redacted]"}` {
		t.Errorf("json = %s", b)
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "token", s)
	if strings.Contains(buf.String(), "tok-123") {
		t.Errorf("slog leaked: %s", buf.String())
	}
	if !NewSecret("").IsZero() || s.IsZero() {
		t.Error("IsZero wrong")
	}
}
```

- [ ] **Step 2: Run to see failure**

Run: `cd <worktree-root>/apps/backend && go test ./internal/config/`
Expected: FAIL, `NewSecret` undefined.

- [ ] **Step 3: Implement Secret and Error**

`apps/backend/internal/config/secret.go`:

```go
package config

import "log/slog"

const redacted = "[redacted]"

// Secret wraps a credential so it cannot be printed, logged, or marshalled by accident.
type Secret struct{ v string }

// NewSecret wraps v.
func NewSecret(v string) Secret { return Secret{v: v} }

// Reveal returns the raw value. Call sites: provider HTTP headers and gitx credential env only.
func (s Secret) Reveal() string { return s.v }

// IsZero reports whether the secret is empty.
func (s Secret) IsZero() bool { return s.v == "" }

func (s Secret) String() string { return redacted }

// MarshalJSON always emits the redaction marker.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }
```

`apps/backend/internal/config/errors.go`:

```go
package config

import "fmt"

// Error names the offending variable and a reason; it never carries the value.
type Error struct {
	Variable string
	Reason   string
}

func (e *Error) Error() string { return fmt.Sprintf("config: %s: %s", e.Variable, e.Reason) }
```

Run the test → PASS.

- [ ] **Step 4: Write failing Load tests**

`apps/backend/internal/config/config_test.go`:

```go
package config

import (
	"errors"
	"log/slog"
	"testing"
	"time"
)

func baseEnv() []string {
	return []string{
		"PROVIDERS__GITLAB_WORK__TYPE=gitlab",
		"PROVIDERS__GITLAB_WORK__BASE_URL=https://gitlab.company.com/",
		"PROVIDERS__GITLAB_WORK__TOKEN=glpat-x",
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"PROVIDERS__GH__DISPLAY_NAME=GitHub.com",
		"UNRELATED=1",
	}
}

func TestLoadDefaultsAndProviders(t *testing.T) {
	cfg, err := Load(baseEnv())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8080 || cfg.WorkspaceRoot != "/data/workspaces" || cfg.RepositoryCacheRoot != "/data/repositories" {
		t.Errorf("defaults wrong: %+v", cfg)
	}
	if cfg.SessionTTL != 24*time.Hour || cfg.CleanupInterval != 30*time.Minute || cfg.LogLevel != slog.LevelInfo || cfg.LogFormat != "text" {
		t.Errorf("duration/log defaults wrong: %+v", cfg)
	}
	if cfg.MaxConcurrentBuilds != 4 || cfg.GitCloneTimeout != 10*time.Minute || cfg.GitCommandTimeout != 2*time.Minute || cfg.ProviderTimeout != 30*time.Second {
		t.Errorf("extra defaults wrong: %+v", cfg)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("providers = %d", len(cfg.Providers))
	}
	gh, ok := cfg.Provider("gh")
	if !ok || gh.Kind != KindGitHub || gh.BaseURL != "https://api.github.com" || gh.DisplayName != "GitHub.com" || gh.Token.Reveal() != "ghp_x" {
		t.Errorf("gh = %+v", gh)
	}
	gl, ok := cfg.Provider("gitlab-work")
	if !ok || gl.Kind != KindGitLab || gl.BaseURL != "https://gitlab.company.com" || gl.DisplayName != "Gitlab Work" || gl.Name != "GITLAB_WORK" {
		t.Errorf("gl = %+v", gl)
	}
	// Providers are sorted by ID for deterministic startup logs.
	if cfg.Providers[0].ID != "gh" || cfg.Providers[1].ID != "gitlab-work" {
		t.Errorf("order = %s,%s", cfg.Providers[0].ID, cfg.Providers[1].ID)
	}
	if got := cfg.Secrets(); len(got) != 2 {
		t.Errorf("Secrets = %v", got)
	}
}

func TestLoadOverrides(t *testing.T) {
	env := append(baseEnv(),
		"APP_PORT=9090", "WORKSPACE_ROOT=/tmp/ws", "REPOSITORY_CACHE_ROOT=/tmp/rc",
		"SESSION_TTL_HOURS=1", "CLEANUP_INTERVAL_MINUTES=5", "LOG_LEVEL=debug", "LOG_FORMAT=json",
		"MAX_CONCURRENT_BUILDS=2", "GIT_CLONE_TIMEOUT_MINUTES=3", "GIT_COMMAND_TIMEOUT_MINUTES=1", "PROVIDER_TIMEOUT_SECONDS=5")
	cfg, err := Load(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9090 || cfg.WorkspaceRoot != "/tmp/ws" || cfg.RepositoryCacheRoot != "/tmp/rc" || cfg.SessionTTL != time.Hour ||
		cfg.CleanupInterval != 5*time.Minute || cfg.LogLevel != slog.LevelDebug || cfg.LogFormat != "json" || cfg.MaxConcurrentBuilds != 2 ||
		cfg.GitCloneTimeout != 3*time.Minute || cfg.GitCommandTimeout != time.Minute || cfg.ProviderTimeout != 5*time.Second {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name     string
		env      []string
		variable string
	}{
		{"no providers", []string{"APP_PORT=1"}, "PROVIDERS__*"},
		{"unknown type", []string{"PROVIDERS__A__TYPE=bitbucket", "PROVIDERS__A__TOKEN=t"}, "PROVIDERS__A__TYPE"},
		{"missing token", []string{"PROVIDERS__A__TYPE=github"}, "PROVIDERS__A__TOKEN"},
		{"gitlab missing base url", []string{"PROVIDERS__A__TYPE=gitlab", "PROVIDERS__A__TOKEN=t"}, "PROVIDERS__A__BASE_URL"},
		{"bad base url", []string{"PROVIDERS__A__TYPE=gitlab", "PROVIDERS__A__TOKEN=t", "PROVIDERS__A__BASE_URL=not a url"}, "PROVIDERS__A__BASE_URL"},
		{"unknown key", []string{"PROVIDERS__A__TYPE=github", "PROVIDERS__A__TOKEN=t", "PROVIDERS__A__COLOR=red"}, "PROVIDERS__A__COLOR"},
		{"bad name", []string{"PROVIDERS__a-b__TYPE=github", "PROVIDERS__a-b__TOKEN=t"}, "PROVIDERS__a-b__TYPE"},
		{"bad port", append(baseEnv(), "APP_PORT=http"), "APP_PORT"},
		{"port range", append(baseEnv(), "APP_PORT=70000"), "APP_PORT"},
		{"bad ttl", append(baseEnv(), "SESSION_TTL_HOURS=0"), "SESSION_TTL_HOURS"},
		{"bad level", append(baseEnv(), "LOG_LEVEL=loud"), "LOG_LEVEL"},
		{"bad format", append(baseEnv(), "LOG_FORMAT=xml"), "LOG_FORMAT"},
		{"bad builds", append(baseEnv(), "MAX_CONCURRENT_BUILDS=0"), "MAX_CONCURRENT_BUILDS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.env)
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if ce.Variable != tc.variable {
				t.Errorf("Variable = %q, want %q (reason %q)", ce.Variable, tc.variable, ce.Reason)
			}
			if tc.name == "missing token" && (ce.Reason == "" || containsAny(ce.Reason, "ghp_", "glpat")) {
				t.Errorf("reason leaks or empty: %q", ce.Reason)
			}
		})
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) && contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Implement `Load`**

`apps/backend/internal/config/config.go`:

```go
// Package config parses Converge's environment-only configuration.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kind identifies a provider implementation.
type Kind string

const (
	KindGitHub Kind = "github"
	KindGitLab Kind = "gitlab"
)

const (
	defaultGitHubBaseURL = "https://api.github.com"
	providerPrefix       = "PROVIDERS__"
	providerSep          = "__"
)

var providerNameRe = regexp.MustCompile(`^[A-Z0-9_]+$`)

// ProviderConfig is one named provider instance.
type ProviderConfig struct {
	ID          string // NAME lower-cased, "_" -> "-"
	Name        string // raw <NAME> from the environment
	DisplayName string
	Kind        Kind
	BaseURL     string // no trailing slash
	Token       Secret
}

// Config is the full runtime configuration.
type Config struct {
	Port                int
	WorkspaceRoot       string
	RepositoryCacheRoot string
	SessionTTL          time.Duration
	CleanupInterval     time.Duration
	LogLevel            slog.Level
	LogFormat           string
	MaxConcurrentBuilds int
	GitCloneTimeout     time.Duration
	GitCommandTimeout   time.Duration
	ProviderTimeout     time.Duration
	Providers           []ProviderConfig
}

// Provider looks a provider up by ID.
func (c Config) Provider(id string) (ProviderConfig, bool) {
	for _, p := range c.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderConfig{}, false
}

// Secrets returns every token value, for log redaction.
func (c Config) Secrets() []string {
	out := make([]string, 0, len(c.Providers))
	for _, p := range c.Providers {
		if !p.Token.IsZero() {
			out = append(out, p.Token.Reveal())
		}
	}
	return out
}

// Load parses an environment slice ("KEY=value") into a Config.
func Load(env []string) (Config, error) {
	vars := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			vars[k] = v
		}
	}
	cfg := Config{LogFormat: "text"}
	var err error
	if cfg.Port, err = intVar(vars, "APP_PORT", 8080, 1, 65535); err != nil {
		return Config{}, err
	}
	cfg.WorkspaceRoot = stringVar(vars, "WORKSPACE_ROOT", "/data/workspaces")
	cfg.RepositoryCacheRoot = stringVar(vars, "REPOSITORY_CACHE_ROOT", "/data/repositories")
	if cfg.SessionTTL, err = durationVar(vars, "SESSION_TTL_HOURS", 24, time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.CleanupInterval, err = durationVar(vars, "CLEANUP_INTERVAL_MINUTES", 30, time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel, err = levelVar(vars, "LOG_LEVEL"); err != nil {
		return Config{}, err
	}
	cfg.LogFormat = strings.ToLower(stringVar(vars, "LOG_FORMAT", "text"))
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		return Config{}, &Error{Variable: "LOG_FORMAT", Reason: "must be text or json"}
	}
	if cfg.MaxConcurrentBuilds, err = intVar(vars, "MAX_CONCURRENT_BUILDS", 4, 1, 64); err != nil {
		return Config{}, err
	}
	if cfg.GitCloneTimeout, err = durationVar(vars, "GIT_CLONE_TIMEOUT_MINUTES", 10, time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.GitCommandTimeout, err = durationVar(vars, "GIT_COMMAND_TIMEOUT_MINUTES", 2, time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.ProviderTimeout, err = durationVar(vars, "PROVIDER_TIMEOUT_SECONDS", 30, time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Providers, err = loadProviders(vars); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func stringVar(vars map[string]string, key, def string) string {
	if v, ok := vars[key]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func intVar(vars map[string]string, key string, def, min, max int) (int, error) {
	raw, ok := vars[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return def, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, &Error{Variable: key, Reason: "must be an integer"}
	}
	if n < min || n > max {
		return 0, &Error{Variable: key, Reason: fmt.Sprintf("must be between %d and %d", min, max)}
	}
	return n, nil
}

func durationVar(vars map[string]string, key string, def int, unit time.Duration) (time.Duration, error) {
	n, err := intVar(vars, key, def, 1, 1<<20)
	if err != nil {
		return 0, err
	}
	return time.Duration(n) * unit, nil
}

func levelVar(vars map[string]string, key string) (slog.Level, error) {
	switch strings.ToLower(stringVar(vars, key, "info")) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, &Error{Variable: key, Reason: "must be one of debug, info, warn, error"}
}

func loadProviders(vars map[string]string) ([]ProviderConfig, error) {
	grouped := map[string]map[string]string{}
	var names []string
	for k, v := range vars {
		if !strings.HasPrefix(k, providerPrefix) {
			continue
		}
		rest := strings.TrimPrefix(k, providerPrefix)
		idx := strings.LastIndex(rest, providerSep)
		if idx <= 0 {
			return nil, &Error{Variable: k, Reason: "expected PROVIDERS__<NAME>__<KEY>"}
		}
		name, key := rest[:idx], rest[idx+len(providerSep):]
		if !providerNameRe.MatchString(name) {
			return nil, &Error{Variable: k, Reason: "provider name must match [A-Z0-9_]+"}
		}
		if grouped[name] == nil {
			grouped[name] = map[string]string{}
			names = append(names, name)
		}
		grouped[name][key] = strings.TrimSpace(v)
	}
	if len(names) == 0 {
		return nil, &Error{Variable: "PROVIDERS__*", Reason: "at least one provider must be configured"}
	}
	sort.Strings(names)
	out := make([]ProviderConfig, 0, len(names))
	for _, name := range names {
		p, err := buildProvider(name, grouped[name])
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func buildProvider(name string, keys map[string]string) (ProviderConfig, error) {
	varName := func(key string) string { return providerPrefix + name + providerSep + key }
	for key := range keys {
		switch key {
		case "TYPE", "BASE_URL", "TOKEN", "DISPLAY_NAME":
		default:
			return ProviderConfig{}, &Error{Variable: varName(key), Reason: "unknown provider key"}
		}
	}
	p := ProviderConfig{ID: providerID(name), Name: name}
	switch Kind(strings.ToLower(keys["TYPE"])) {
	case KindGitHub:
		p.Kind = KindGitHub
	case KindGitLab:
		p.Kind = KindGitLab
	default:
		return ProviderConfig{}, &Error{Variable: varName("TYPE"), Reason: "must be github or gitlab"}
	}
	if keys["TOKEN"] == "" {
		return ProviderConfig{}, &Error{Variable: varName("TOKEN"), Reason: "is required"}
	}
	p.Token = NewSecret(keys["TOKEN"])
	base := keys["BASE_URL"]
	if base == "" {
		if p.Kind == KindGitLab {
			return ProviderConfig{}, &Error{Variable: varName("BASE_URL"), Reason: "is required for gitlab providers"}
		}
		base = defaultGitHubBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ProviderConfig{}, &Error{Variable: varName("BASE_URL"), Reason: "must be an absolute http(s) URL"}
	}
	p.BaseURL = strings.TrimRight(base, "/")
	p.DisplayName = keys["DISPLAY_NAME"]
	if p.DisplayName == "" {
		p.DisplayName = displayName(name)
	}
	return p, nil
}

func providerID(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "_", "-")
}

func displayName(name string) string {
	words := strings.Split(strings.ToLower(name), "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
```

- [ ] **Step 6: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/config/ && go vet ./... && go tool golangci-lint run ./internal/config/...`
Expected: PASS, no lint findings.

- [ ] **Step 7: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/config && git commit -m "feat(task-001): environment configuration with redacted secrets"
```

---

### Task 3: gitx — runner, validators, credentials, locks, redaction

**Files:**
- Create: `apps/backend/internal/gitx/spec.go`, `exec.go`, `fake.go`, `validate.go`, `credentials.go`, `lock.go`, `redact.go`, `validate_test.go`, `exec_test.go`, `credentials_test.go`, `lock_test.go`, `redact_test.go`

**Interfaces:**
- Produces:
  - `type Category string` constants `CategoryClone="clone"`, `CategoryFetch="fetch"`, `CategoryWorktree="worktree"`, `CategoryCherryPick="cherry-pick"`, `CategoryDiff="diff"`, `CategoryCleanup="cleanup"`, `CategoryQuery="query"`.
  - `type Spec struct{ Dir string; Args []string; Env []string; Stdin io.Reader; Category Category; Timeout time.Duration; Repo, Session string }`.
  - `type Result struct{ Stdout, Stderr []byte; ExitCode int; Duration time.Duration }`.
  - `type ExitError struct{ Category Category; Result Result }` with `Error()`; `func IsExit(err error, code int) bool`.
  - `type Runner interface{ Run(ctx context.Context, s Spec) (Result, error) }` — returns `*ExitError` on non-zero exit, `context` errors on timeout.
  - `type Options struct{ CloneTimeout, CommandTimeout time.Duration; Secrets []string }`; `func NewExecRunner(log *slog.Logger, opts Options) (*ExecRunner, error)`; `(*ExecRunner) Close() error` removes its temp HOME/hooks dirs; `(*ExecRunner) Version(ctx) (string, error)`.
  - `type FakeRunner struct{ Handler func(Spec) (Result, error); mu; Calls []Spec }`; `(*FakeRunner) Run`; `(*FakeRunner) Reset()`.
  - `func ValidateSHA(string) error`, `ValidateRepoFullName(string) error`, `ValidateBranchSyntax(string) error`, `ValidateBranch(ctx, Runner, string) error`, `ValidateChangeNumber(int) error`, `ValidateChangeNumbers([]int) ([]int, error)` (sorted, de-duplicated, ≤ 50), `ValidatePathArg(string) error` (no leading `-`, no NUL).
  - `func CredentialEnv(cloneURL, user, token string) ([]string, error)` → `GIT_CONFIG_COUNT=1`, `GIT_CONFIG_KEY_0=http.<scheme>://<host>/.extraheader`, `GIT_CONFIG_VALUE_0=Authorization: Basic <base64(user:token)>`.
  - `type LockMap struct{...}`; `func (l *LockMap) Lock(key string) func()`.
  - `func Redact(b []byte, secrets []string) []byte`.

- [ ] **Step 1: Write failing validator tests (including a fuzz target)**

`apps/backend/internal/gitx/validate_test.go`:

```go
package gitx

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestValidateSHA(t *testing.T) {
	good := strings.Repeat("a", 40)
	if err := ValidateSHA(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "abc", strings.Repeat("A", 40), strings.Repeat("a", 41), "-" + good[1:]} {
		if ValidateSHA(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestValidateRepoFullName(t *testing.T) {
	for _, good := range []string{"owner/repo", "group/sub/project", "a.b/c-d_e"} {
		if err := ValidateRepoFullName(good); err != nil {
			t.Errorf("%q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"", "repo", "/owner/repo", "-owner/repo", ".owner/repo", "owner/../repo", "owner/./repo", "owner//repo", "owner/repo/", "owner/repo name", "owner/repo\x00", "owner/re\npo", "../x/y"} {
		if ValidateRepoFullName(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func FuzzValidateRepoFullName(f *testing.F) {
	f.Add("owner/repo")
	f.Add("../x/y")
	re := regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`)
	f.Fuzz(func(t *testing.T, s string) {
		err := ValidateRepoFullName(s)
		if err == nil {
			if !re.MatchString(s) || strings.Contains(s, "..") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") {
				t.Fatalf("accepted invalid %q", s)
			}
			for _, seg := range strings.Split(s, "/") {
				if seg == "." || seg == ".." || seg == "" {
					t.Fatalf("accepted bad segment in %q", s)
				}
			}
		}
	})
}

func TestValidateBranchSyntax(t *testing.T) {
	for _, good := range []string{"main", "release/1.2", "feat_x.y-z"} {
		if err := ValidateBranchSyntax(good); err != nil {
			t.Errorf("%q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"", "-x", "a..b", "a b", "a\tb", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", "a\\b", "a//b", "/a", "a/", "a.lock", "@", "a@{b", "a\x00"} {
		if ValidateBranchSyntax(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestValidateBranchUsesGit(t *testing.T) {
	fr := &FakeRunner{Handler: func(s Spec) (Result, error) {
		if s.Args[0] != "check-ref-format" || s.Args[1] != "--branch" || s.Args[2] != "main" {
			t.Errorf("args = %v", s.Args)
		}
		return Result{}, nil
	}}
	if err := ValidateBranch(context.Background(), fr, "main"); err != nil {
		t.Fatal(err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("calls = %d", len(fr.Calls))
	}
	fr.Reset()
	if err := ValidateBranch(context.Background(), fr, "-bad"); err == nil || len(fr.Calls) != 0 {
		t.Fatalf("leading dash must be rejected before git runs; err=%v calls=%d", err, len(fr.Calls))
	}
	fr.Handler = func(Spec) (Result, error) { return Result{ExitCode: 1}, &ExitError{Result: Result{ExitCode: 1}} }
	if err := ValidateBranch(context.Background(), fr, "odd"); err == nil {
		t.Fatal("git rejection must propagate")
	}
}

func TestValidateChangeNumbers(t *testing.T) {
	got, err := ValidateChangeNumbers([]int{5, 3, 5, 1})
	if err != nil || len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Fatalf("got %v err %v", got, err)
	}
	if _, err := ValidateChangeNumbers(nil); err == nil {
		t.Error("empty accepted")
	}
	if _, err := ValidateChangeNumbers([]int{0}); err == nil {
		t.Error("zero accepted")
	}
	many := make([]int, 51)
	for i := range many {
		many[i] = i + 1
	}
	if _, err := ValidateChangeNumbers(many); err == nil {
		t.Error(">50 accepted")
	}
	if ValidatePathArg("-rf") == nil || ValidatePathArg("a\x00b") == nil || ValidatePathArg("src/x.go") != nil {
		t.Error("ValidatePathArg wrong")
	}
}
```

- [ ] **Step 2: Run to see failure**

Run: `cd <worktree-root>/apps/backend && go test ./internal/gitx/`
Expected: FAIL to compile.

- [ ] **Step 3: Implement spec, fake runner, validators**

`apps/backend/internal/gitx/spec.go`:

```go
// Package gitx executes git with strict argument validation and credential isolation.
package gitx

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Category labels a git invocation for logging and default timeouts.
type Category string

const (
	CategoryClone      Category = "clone"
	CategoryFetch      Category = "fetch"
	CategoryWorktree   Category = "worktree"
	CategoryCherryPick Category = "cherry-pick"
	CategoryDiff       Category = "diff"
	CategoryCleanup    Category = "cleanup"
	CategoryQuery      Category = "query"
)

// Spec describes one git invocation. Args never include "git".
type Spec struct {
	Dir      string
	Args     []string
	Env      []string
	Stdin    io.Reader
	Category Category
	Timeout  time.Duration
	Repo     string
	Session  string
}

// Result is the captured outcome of an invocation.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

// ExitError is returned when git exits non-zero.
type ExitError struct {
	Category Category
	Result   Result
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("git %s exited with code %d", e.Category, e.Result.ExitCode)
}

// IsExit reports whether err is an ExitError with the given code.
func IsExit(err error, code int) bool {
	var ee *ExitError
	if !asExit(err, &ee) {
		return false
	}
	return ee.Result.ExitCode == code
}

// Runner executes git.
type Runner interface {
	Run(ctx context.Context, s Spec) (Result, error)
}
```

`apps/backend/internal/gitx/fake.go`:

```go
package gitx

import (
	"context"
	"errors"
	"sync"
)

func asExit(err error, target **ExitError) bool { return errors.As(err, target) }

// FakeRunner records specs and returns canned results. Safe for concurrent use.
type FakeRunner struct {
	Handler func(Spec) (Result, error)
	mu      sync.Mutex
	Calls   []Spec
}

// Run records the spec and delegates to Handler (or returns an empty success).
func (f *FakeRunner) Run(_ context.Context, s Spec) (Result, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, s)
	f.mu.Unlock()
	if f.Handler == nil {
		return Result{}, nil
	}
	return f.Handler(s)
}

// Reset clears recorded calls.
func (f *FakeRunner) Reset() {
	f.mu.Lock()
	f.Calls = nil
	f.mu.Unlock()
}

// Snapshot returns a copy of recorded calls.
func (f *FakeRunner) Snapshot() []Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Spec, len(f.Calls))
	copy(out, f.Calls)
	return out
}
```

`apps/backend/internal/gitx/validate.go`:

```go
package gitx

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// MaxChanges is the maximum number of changes in one review.
const MaxChanges = 50

var (
	shaRe      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repoRe     = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`)
	branchBad  = regexp.MustCompile(`[\x00-\x20\x7f~^:?*\[\\]|\.\.|@\{|//|\.lock$|\.lock/|^/|/$|^\.|/\.|^@$`)
	ErrInvalid = errors.New("invalid argument")
)

// ValidateSHA accepts exactly 40 lowercase hex characters.
func ValidateSHA(s string) error {
	if !shaRe.MatchString(s) {
		return fmt.Errorf("%w: sha must be 40 lowercase hex characters", ErrInvalid)
	}
	return nil
}

// ValidateRepoFullName enforces FR-3.7.
func ValidateRepoFullName(s string) error {
	if !repoRe.MatchString(s) {
		return fmt.Errorf("%w: repository must look like owner/name", ErrInvalid)
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, ".") {
		return fmt.Errorf("%w: repository must not start with /, - or .", ErrInvalid)
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, "..") {
			return fmt.Errorf("%w: repository contains an invalid path segment", ErrInvalid)
		}
	}
	return nil
}

// ValidateBranchSyntax is the pre-check applied before git sees the name.
func ValidateBranchSyntax(s string) error {
	if s == "" || strings.HasPrefix(s, "-") || branchBad.MatchString(s) || len(s) > 255 {
		return fmt.Errorf("%w: invalid branch name", ErrInvalid)
	}
	return nil
}

// ValidateBranch pre-checks the syntax then asks git check-ref-format --branch.
func ValidateBranch(ctx context.Context, r Runner, s string) error {
	if err := ValidateBranchSyntax(s); err != nil {
		return err
	}
	if _, err := r.Run(ctx, Spec{Args: []string{"check-ref-format", "--branch", s}, Category: CategoryQuery}); err != nil {
		return fmt.Errorf("%w: git rejected branch name", ErrInvalid)
	}
	return nil
}

// ValidateChangeNumber accepts positive integers.
func ValidateChangeNumber(n int) error {
	if n <= 0 {
		return fmt.Errorf("%w: change number must be positive", ErrInvalid)
	}
	return nil
}

// ValidateChangeNumbers de-duplicates, sorts, and bounds the list.
func ValidateChangeNumbers(in []int) ([]int, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("%w: at least one change is required", ErrInvalid)
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, n := range in {
		if err := ValidateChangeNumber(n); err != nil {
			return nil, err
		}
		if _, dup := seen[n]; dup {
			return nil, fmt.Errorf("%w: duplicate change number %d", ErrInvalid, n)
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	if len(out) > MaxChanges {
		return nil, fmt.Errorf("%w: at most %d changes per review", ErrInvalid, MaxChanges)
	}
	sort.Ints(out)
	return out, nil
}

// ValidatePathArg rejects values that git could parse as options.
func ValidatePathArg(s string) error {
	if s == "" || strings.HasPrefix(s, "-") || strings.ContainsRune(s, 0) {
		return fmt.Errorf("%w: invalid path argument", ErrInvalid)
	}
	return nil
}
```

Run: `go test ./internal/gitx/ -run 'Validate'` → PASS. Run the fuzzer briefly: `go test ./internal/gitx/ -run '^$' -fuzz FuzzValidateRepoFullName -fuzztime 10s` → PASS.

- [ ] **Step 4: Write failing credential, redact, lock tests**

`apps/backend/internal/gitx/credentials_test.go`:

```go
package gitx

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCredentialEnv(t *testing.T) {
	env, err := CredentialEnv("https://gitlab.example.com/group/proj.git", "oauth2", "glpat-secret")
	if err != nil {
		t.Fatal(err)
	}
	want := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("oauth2:glpat-secret"))
	if len(env) != 3 || env[0] != "GIT_CONFIG_COUNT=1" || env[1] != "GIT_CONFIG_KEY_0=http.https://gitlab.example.com/.extraheader" || env[2] != "GIT_CONFIG_VALUE_0="+want {
		t.Errorf("env = %q", env)
	}
	if _, err := CredentialEnv("file:///tmp/x", "u", "t"); err == nil {
		t.Error("non-http URL must be rejected")
	}
	if _, err := CredentialEnv("https://h/x", "", "t"); err == nil {
		t.Error("empty user must be rejected")
	}
	for _, e := range env {
		if strings.Contains(e, "glpat-secret") {
			t.Errorf("raw token present in %q", e)
		}
	}
}
```

`apps/backend/internal/gitx/redact_test.go`:

```go
package gitx

import "testing"

func TestRedact(t *testing.T) {
	in := []byte("fatal: Authorization: Basic abc123 rejected; PRIVATE-TOKEN: glpat-x; token=glpat-x again")
	out := string(Redact(in, []string{"glpat-x", ""}))
	for _, leak := range []string{"abc123", "glpat-x"} {
		if contains(out, leak) {
			t.Errorf("leak %q in %q", leak, out)
		}
	}
	if !contains(out, "Authorization: [redacted]") || !contains(out, "PRIVATE-TOKEN: [redacted]") {
		t.Errorf("headers not redacted: %q", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

`apps/backend/internal/gitx/lock_test.go`:

```go
package gitx

import (
	"sync"
	"testing"
)

func TestLockMapSerialisesSameKeyOnly(t *testing.T) {
	var lm LockMap
	var mu sync.Mutex
	active := map[string]int{}
	maxActive := map[string]int{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		key := "a"
		if i%2 == 1 {
			key = "b"
		}
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			unlock := lm.Lock(key)
			defer unlock()
			mu.Lock()
			active[key]++
			if active[key] > maxActive[key] {
				maxActive[key] = active[key]
			}
			mu.Unlock()
			mu.Lock()
			active[key]--
			mu.Unlock()
		}(key)
	}
	wg.Wait()
	if maxActive["a"] != 1 || maxActive["b"] != 1 {
		t.Fatalf("lock not exclusive: %v", maxActive)
	}
}
```

- [ ] **Step 5: Implement credentials, redact, lock**

`apps/backend/internal/gitx/credentials.go`:

```go
package gitx

import (
	"encoding/base64"
	"fmt"
	"net/url"
)

// CredentialEnv builds the GIT_CONFIG_* environment that scopes a Basic
// Authorization header to the clone URL's origin. The token never enters argv.
func CredentialEnv(cloneURL, user, token string) ([]string, error) {
	u, err := url.Parse(cloneURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: clone URL must be http(s)", ErrInvalid)
	}
	if user == "" || token == "" {
		return nil, fmt.Errorf("%w: credential user and token are required", ErrInvalid)
	}
	basic := base64.StdEncoding.EncodeToString([]byte(user + ":" + token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		fmt.Sprintf("GIT_CONFIG_KEY_0=http.%s://%s/.extraheader", u.Scheme, u.Host),
		"GIT_CONFIG_VALUE_0=Authorization: Basic " + basic,
	}, nil
}
```

`apps/backend/internal/gitx/redact.go`:

```go
package gitx

import (
	"bytes"
	"regexp"
)

var headerRe = regexp.MustCompile(`(?i)(authorization|private-token)\s*:\s*[^\r\n;]+`)

// Redact scrubs known header values and any configured secret from b.
func Redact(b []byte, secrets []string) []byte {
	out := headerRe.ReplaceAllFunc(b, func(m []byte) []byte {
		i := bytes.IndexByte(m, ':')
		return append(append([]byte{}, m[:i]...), []byte(": [redacted]")...)
	})
	for _, s := range secrets {
		if s != "" {
			out = bytes.ReplaceAll(out, []byte(s), []byte("[redacted]"))
		}
	}
	return out
}
```

`apps/backend/internal/gitx/lock.go`:

```go
package gitx

import "sync"

// LockMap hands out one mutex per key (mirror path).
type LockMap struct{ m sync.Map }

// Lock blocks until the key's mutex is held and returns the unlock func.
func (l *LockMap) Lock(key string) func() {
	v, _ := l.m.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
```

Run: `go test ./internal/gitx/` → PASS.

- [ ] **Step 6: Write failing ExecRunner tests (real git)**

`apps/backend/internal/gitx/exec_test.go`:

```go
package gitx

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func newRunner(t *testing.T) *ExecRunner {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	r, err := NewExecRunner(slog.New(slog.NewTextHandler(os.Stderr, nil)), Options{CloneTimeout: time.Minute, CommandTimeout: 10 * time.Second, Secrets: []string{"s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestExecRunnerRunsGitWithIsolatedEnv(t *testing.T) {
	r := newRunner(t)
	res, err := r.Run(context.Background(), Spec{Args: []string{"config", "--show-origin", "--get", "core.hooksPath"}, Category: CategoryQuery})
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, res.Stderr)
	}
	if !strings.Contains(string(res.Stdout), r.hooksDir) {
		t.Errorf("hooksPath not applied: %s", res.Stdout)
	}
	res, err = r.Run(context.Background(), Spec{Args: []string{"var", "GIT_COMMITTER_IDENT"}, Category: CategoryQuery})
	if err != nil || !strings.HasPrefix(string(res.Stdout), "Converge Review <converge@localhost>") {
		t.Errorf("ident: err=%v out=%s", err, res.Stdout)
	}
}

func TestExecRunnerVersion(t *testing.T) {
	r := newRunner(t)
	v, err := r.Version(context.Background())
	if err != nil || !strings.Contains(v, ".") {
		t.Fatalf("v=%q err=%v", v, err)
	}
}

func TestExecRunnerExitErrorAndStdin(t *testing.T) {
	r := newRunner(t)
	_, err := r.Run(context.Background(), Spec{Args: []string{"rev-parse", "--verify", "definitely-not-a-ref"}, Dir: t.TempDir(), Category: CategoryQuery})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Result.ExitCode == 0 {
		t.Fatalf("want ExitError, got %v", err)
	}
	res, err := r.Run(context.Background(), Spec{Args: []string{"hash-object", "--stdin"}, Stdin: strings.NewReader("hello\n"), Category: CategoryQuery})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatalf("stdin not wired: %v %s", err, res.Stdout)
	}
}

func TestExecRunnerTimeout(t *testing.T) {
	r := newRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := r.Run(ctx, Spec{Args: []string{"cat-file", "--batch"}, Stdin: blockingReader{}, Category: CategoryQuery, Timeout: time.Minute})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline error, got %v", err)
	}
}

type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }
```

- [ ] **Step 7: Implement ExecRunner**

`apps/backend/internal/gitx/exec.go`:

```go
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	identityName  = "Converge Review"
	identityEmail = "converge@localhost"
	stderrLogCap  = 2048
)

// Options configures an ExecRunner.
type Options struct {
	CloneTimeout   time.Duration // default for clone/fetch categories
	CommandTimeout time.Duration // default for everything else
	Secrets        []string      // scrubbed from logged stderr
}

// ExecRunner runs the real git binary with an isolated environment.
type ExecRunner struct {
	log      *slog.Logger
	opts     Options
	homeDir  string
	hooksDir string
	gitPath  string
}

// NewExecRunner locates git and prepares private HOME and hooks directories.
func NewExecRunner(log *slog.Logger, opts Options) (*ExecRunner, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git is not on PATH: %w", err)
	}
	if opts.CloneTimeout <= 0 {
		opts.CloneTimeout = 10 * time.Minute
	}
	if opts.CommandTimeout <= 0 {
		opts.CommandTimeout = 2 * time.Minute
	}
	base, err := os.MkdirTemp("", "converge-git-")
	if err != nil {
		return nil, fmt.Errorf("create git env dir: %w", err)
	}
	hooks := filepath.Join(base, "hooks")
	home := filepath.Join(base, "home")
	for _, d := range []string{hooks, home} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}
	return &ExecRunner{log: log, opts: opts, homeDir: home, hooksDir: hooks, gitPath: gitPath}, nil
}

// Close removes the private directories.
func (r *ExecRunner) Close() error { return os.RemoveAll(filepath.Dir(r.homeDir)) }

// Version returns the output of git --version, e.g. "2.49.0".
func (r *ExecRunner) Version(ctx context.Context) (string, error) {
	res, err := r.Run(ctx, Spec{Args: []string{"--version"}, Category: CategoryQuery})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.TrimPrefix(string(res.Stdout), "git version ")), nil
}

func (r *ExecRunner) baseEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.homeDir,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"LC_ALL=C",
		"GIT_AUTHOR_NAME=" + identityName,
		"GIT_AUTHOR_EMAIL=" + identityEmail,
		"GIT_COMMITTER_NAME=" + identityName,
		"GIT_COMMITTER_EMAIL=" + identityEmail,
	}
}

func (r *ExecRunner) timeoutFor(s Spec) time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	if s.Category == CategoryClone || s.Category == CategoryFetch {
		return r.opts.CloneTimeout
	}
	return r.opts.CommandTimeout
}

// Run executes git with the fixed -c prefix and isolated environment.
func (r *ExecRunner) Run(ctx context.Context, s Spec) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeoutFor(s))
	defer cancel()
	args := append([]string{
		"-c", "core.hooksPath=" + r.hooksDir,
		"-c", "commit.gpgsign=false",
		"-c", "protocol.file.allow=always",
	}, s.Args...)
	cmd := exec.CommandContext(ctx, r.gitPath, args...)
	cmd.Dir = s.Dir
	cmd.Env = append(r.baseEnv(), s.Env...)
	cmd.Stdin = s.Stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 2 * time.Second
	start := time.Now()
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Duration: time.Since(start)}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	attrs := []any{
		slog.String("git.category", string(s.Category)),
		slog.String("repository", s.Repo),
		slog.String("session", s.Session),
		slog.Int("exit_code", res.ExitCode),
		slog.Int64("duration_ms", res.Duration.Milliseconds()),
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		r.log.Warn("git timed out", append(attrs, slog.String("outcome", "timeout"))...)
		return res, fmt.Errorf("git %s: %w", s.Category, ctxErr)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderrLog := res.Stderr
			if len(stderrLog) > stderrLogCap {
				stderrLog = stderrLog[:stderrLogCap]
			}
			r.log.Debug("git exited non-zero", append(attrs, slog.String("outcome", "exit"), slog.String("stderr", string(Redact(stderrLog, r.opts.Secrets))))...)
			return res, &ExitError{Category: s.Category, Result: res}
		}
		r.log.Error("git failed to start", append(attrs, slog.String("outcome", "error"))...)
		return res, fmt.Errorf("git %s: %w", s.Category, err)
	}
	r.log.Debug("git ok", append(attrs, slog.String("outcome", "ok"))...)
	return res, nil
}
```

- [ ] **Step 8: Run the whole package with race and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/gitx/ && go tool golangci-lint run ./internal/gitx/...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/gitx && git commit -m "feat(task-001): gitx runner, validators, credential env, locks, redaction"
```

---

## Phase B — Providers

### Task 4: Provider model, interface, errors, registry, fake provider

**Files:**
- Create: `apps/backend/internal/provider/page.go`, `errors.go`, `model.go`, `builder.go`, `provider.go`, `registry.go`, `httpjson.go`, `model_test.go`, `errors_test.go`, `registry_test.go`, `httpjson_test.go`
- Create: `apps/backend/internal/provider/fake/fake.go`

**Interfaces:**
- Produces (package `provider`):
  - `type Kind string`; `KindGitHub Kind = "github"`, `KindGitLab Kind = "gitlab"`.
  - `type Page struct{ Number, Size int }`; `func (p Page) Normalize() Page` (Number ≥ 1, Size default 30, max 100); `type Slice[T any] struct{ Items []T; HasNext bool }`.
  - `var ErrAuth, ErrNotFound, ErrUnavailable, ErrTooManyCommits error`; `type StatusError struct{ Method, Path string; Status int; RetryAfter string }` whose `Unwrap()` returns the sentinel for its status; `func NewStatusError(method, path string, status int, h http.Header) *StatusError`.
  - `type Repository` (immutable): accessors `ProviderID() Name() Namespace() FullName() DefaultBranch() WebURL() CloneURL() string`; `RepositoryBuilder` with `NewRepositoryBuilder().SetProviderID().SetFullName().SetName().SetNamespace().SetDefaultBranch().SetWebURL().SetCloneURL().Build() (Repository, error)`.
  - `type Commit` immutable: `NewCommit(sha, message string, authoredAt time.Time) (Commit, error)`; accessors `SHA() Message() AuthoredAt()`.
  - `type ChangeState string`: `StateMerged="merged"`, `StateOpen="open"`, `StateClosed="closed"`.
  - `type ChangeRequest` immutable with accessors `ProviderID() Repository() Number() Title() Author() WebURL() SourceBranch() TargetBranch() CreatedAt() MergedAt() State() Commits() MergeCommitSHA() SquashCommitSHA() HeadSHA() Squashed() CommitCount()`; `WithCommits([]Commit) ChangeRequest`; `LandingCandidates() []string` returning the non-empty SHAs in the order merge, squash, head (consumed by Task 12's landing resolution); `ChangeRequestBuilder` with `NewChangeRequestBuilder()` and `Set*` for every field plus `SetCommitCount(int)` and `Build() (ChangeRequest, error)` (requires provider ID, repository, number > 0, title, target branch; SHAs when set must pass `gitx.ValidateSHA`).
  - `type GitProvider interface { ID() string; Kind() Kind; DisplayName() string; BaseURL() string; ListRepositories(ctx, Page) (Slice[Repository], error); GetRepository(ctx, fullName string) (Repository, error); ListMergedChanges(ctx, repo Repository, targetBranch, search string, page Page) (Slice[ChangeRequest], error); GetChange(ctx, repo Repository, number int) (ChangeRequest, error); GetChangeCommits(ctx, repo Repository, number int) ([]Commit, error); CloneURL(repo Repository) string; AuthorizeGit(repo Repository, spec *gitx.Spec) error }`.
  - `func ParseSearchNumber(search string) (int, bool)` strips leading `#`/`!`, returns number when the remainder is all digits.
  - `type Registry`; `NewRegistry()`, `(*Registry) Register(GitProvider) error` (duplicate ID → error), `Get(id) (GitProvider, bool)`, `All() []GitProvider` (sorted by ID).
  - `func DoJSON(ctx, client *http.Client, req *http.Request, out any) (http.Header, error)` — executes, maps non-2xx to `*StatusError`, decodes JSON into `out` when non-nil; never includes headers or bodies in errors.
- Produces (package `fake`): `type Provider struct` implementing `GitProvider` with `New(id string, kind provider.Kind) *Provider`, `AddRepository(repo provider.Repository)`, `AddChange(cr provider.ChangeRequest)`, `SetCommits(fullName string, number int, commits []provider.Commit)`, `FailWith(err error)` (next call returns err). `CloneURL` returns the repository's clone URL (a `file://` path in tests); `AuthorizeGit` is a no-op.

- [ ] **Step 1: Write failing model and builder tests**

`apps/backend/internal/provider/model_test.go`:

```go
package provider

import (
	"strings"
	"testing"
	"time"
)

func testRepo(t *testing.T) Repository {
	t.Helper()
	r, err := NewRepositoryBuilder().SetProviderID("gh").SetFullName("atlas/server").SetName("server").
		SetNamespace("atlas").SetDefaultBranch("main").SetWebURL("https://github.com/atlas/server").
		SetCloneURL("https://github.com/atlas/server.git").Build()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRepositoryBuilder(t *testing.T) {
	r := testRepo(t)
	if r.FullName() != "atlas/server" || r.Name() != "server" || r.Namespace() != "atlas" || r.DefaultBranch() != "main" || r.ProviderID() != "gh" {
		t.Errorf("accessors wrong: %+v", r)
	}
	if _, err := NewRepositoryBuilder().SetProviderID("gh").SetFullName("../x").Build(); err == nil {
		t.Error("invalid full name accepted")
	}
	if _, err := NewRepositoryBuilder().SetProviderID("").SetFullName("a/b").Build(); err == nil {
		t.Error("empty provider accepted")
	}
	// Name and namespace derive from the full name when omitted.
	d, err := NewRepositoryBuilder().SetProviderID("gh").SetFullName("group/sub/proj").SetDefaultBranch("main").Build()
	if err != nil || d.Name() != "proj" || d.Namespace() != "group/sub" {
		t.Errorf("derivation wrong: %v %q %q", err, d.Name(), d.Namespace())
	}
}

func TestChangeRequestBuilder(t *testing.T) {
	sha := strings.Repeat("a", 40)
	merged := time.Date(2026, 8, 21, 14, 2, 11, 0, time.UTC)
	c, err := NewCommit(sha, "feat: x", merged.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	cr, err := NewChangeRequestBuilder().SetProviderID("gh").SetRepository(testRepo(t)).SetNumber(421).SetTitle("Add field-state endpoint").
		SetAuthor("jsmith").SetWebURL("https://github.com/atlas/server/pull/421").SetSourceBranch("feat/field-state").SetTargetBranch("main").
		SetCreatedAt(merged.Add(-24 * time.Hour)).SetMergedAt(merged).SetState(StateMerged).SetMergeCommitSHA(sha).SetHeadSHA(sha).
		SetCommitCount(1).SetCommits([]Commit{c}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if cr.Number() != 421 || cr.State() != StateMerged || !cr.MergedAt().Equal(merged) || cr.MergeCommitSHA() != sha || cr.SquashCommitSHA() != "" || len(cr.Commits()) != 1 || cr.CommitCount() != 1 {
		t.Errorf("accessors wrong")
	}
	cr2 := cr.WithCommits(nil)
	if len(cr.Commits()) != 1 || len(cr2.Commits()) != 0 {
		t.Error("WithCommits must not mutate the receiver")
	}
	if _, err := NewChangeRequestBuilder().SetProviderID("gh").SetRepository(testRepo(t)).SetNumber(0).SetTitle("t").SetTargetBranch("main").Build(); err == nil {
		t.Error("number 0 accepted")
	}
	if _, err := NewChangeRequestBuilder().SetProviderID("gh").SetRepository(testRepo(t)).SetNumber(1).SetTitle("t").SetTargetBranch("main").SetMergeCommitSHA("nothex").Build(); err == nil {
		t.Error("bad sha accepted")
	}
	if _, err := NewCommit("bad", "m", merged); err == nil {
		t.Error("bad commit sha accepted")
	}
}

func TestPageNormalizeAndSearchNumber(t *testing.T) {
	if p := (Page{}).Normalize(); p.Number != 1 || p.Size != 30 {
		t.Errorf("default = %+v", p)
	}
	if p := (Page{Number: 3, Size: 500}).Normalize(); p.Number != 3 || p.Size != 100 {
		t.Errorf("clamp = %+v", p)
	}
	for in, want := range map[string]int{"421": 421, "#421": 421, "!7": 7, " 12 ": 12} {
		if n, ok := ParseSearchNumber(in); !ok || n != want {
			t.Errorf("%q -> %d,%v", in, n, ok)
		}
	}
	for _, in := range []string{"", "abc", "4a", "#", "0"} {
		if _, ok := ParseSearchNumber(in); ok {
			t.Errorf("%q parsed as number", in)
		}
	}
}
```

`apps/backend/internal/provider/errors_test.go`:

```go
package provider

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestStatusErrorMapping(t *testing.T) {
	cases := map[int]error{401: ErrAuth, 403: ErrAuth, 404: ErrNotFound, 429: ErrUnavailable, 500: ErrUnavailable, 503: ErrUnavailable, 418: ErrUnavailable}
	for status, want := range cases {
		err := NewStatusError("GET", "/x", status, http.Header{})
		if !errors.Is(err, want) {
			t.Errorf("%d -> %v, want %v", status, err, want)
		}
		if !strings.Contains(err.Error(), "GET /x") || !strings.Contains(err.Error(), "418"[:0]+"") {
			t.Errorf("message = %q", err.Error())
		}
	}
	h := http.Header{"X-Ratelimit-Remaining": []string{"0"}, "X-Ratelimit-Reset": []string{"1700000000"}}
	err := NewStatusError("GET", "/x", 403, h)
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "1700000000") {
		t.Errorf("rate-limited 403 should be unavailable with reset: %v", err)
	}
	h2 := http.Header{"Retry-After": []string{"30"}}
	if err := NewStatusError("GET", "/x", 429, h2); !strings.Contains(err.Error(), "retry after 30") {
		t.Errorf("retry-after missing: %v", err)
	}
}
```

`apps/backend/internal/provider/registry_test.go`:

```go
package provider

import (
	"context"
	"testing"

	"github.com/jtumidanski/converge/internal/gitx"
)

type stubProvider struct{ id string }

func (s stubProvider) ID() string           { return s.id }
func (s stubProvider) Kind() Kind           { return KindGitHub }
func (s stubProvider) DisplayName() string  { return s.id }
func (s stubProvider) BaseURL() string      { return "" }
func (s stubProvider) ListRepositories(context.Context, Page) (Slice[Repository], error) {
	return Slice[Repository]{}, nil
}
func (s stubProvider) GetRepository(context.Context, string) (Repository, error) {
	return Repository{}, ErrNotFound
}
func (s stubProvider) ListMergedChanges(context.Context, Repository, string, string, Page) (Slice[ChangeRequest], error) {
	return Slice[ChangeRequest]{}, nil
}
func (s stubProvider) GetChange(context.Context, Repository, int) (ChangeRequest, error) {
	return ChangeRequest{}, ErrNotFound
}
func (s stubProvider) GetChangeCommits(context.Context, Repository, int) ([]Commit, error) {
	return nil, nil
}
func (s stubProvider) CloneURL(Repository) string                  { return "" }
func (s stubProvider) AuthorizeGit(Repository, *gitx.Spec) error   { return nil }

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stubProvider{"b"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(stubProvider{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(stubProvider{"a"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, ok := r.Get("a"); !ok {
		t.Fatal("Get a")
	}
	if _, ok := r.Get("zz"); ok {
		t.Fatal("Get zz")
	}
	all := r.All()
	if len(all) != 2 || all[0].ID() != "a" || all[1].ID() != "b" {
		t.Fatalf("All = %v", all)
	}
}
```

`apps/backend/internal/provider/httpjson_test.go`:

```go
package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Link", `<x>; rel="next"`)
			_, _ = w.Write([]byte(`{"a":1}`))
		case "/bad":
			w.WriteHeader(502)
			_, _ = w.Write([]byte(`{"message":"secret-body"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ok", nil)
	var out struct{ A int }
	h, err := DoJSON(context.Background(), srv.Client(), req, &out)
	if err != nil || out.A != 1 || h.Get("Link") == "" {
		t.Fatalf("ok: %v %+v %v", err, out, h)
	}
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/bad", nil)
	_, err = DoJSON(context.Background(), srv.Client(), req, &out)
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret-body") {
		t.Fatalf("bad: %v", err)
	}
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/missing", nil)
	if _, err = DoJSON(context.Background(), srv.Client(), req, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	srv.Close()
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/ok", nil)
	if _, err = DoJSON(context.Background(), srv.Client(), req, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("network: %v", err)
	}
}
```

- [ ] **Step 2: Run to see failures**

Run: `cd <worktree-root>/apps/backend && go test ./internal/provider/`
Expected: compile failure.

- [ ] **Step 3: Implement the provider package**

`apps/backend/internal/provider/page.go`:

```go
// Package provider defines the provider-agnostic model and interface.
package provider

import (
	"strconv"
	"strings"
)

const (
	DefaultPageSize = 30
	MaxPageSize     = 100
)

// Page is a 1-based page request.
type Page struct {
	Number int
	Size   int
}

// Normalize applies defaults and clamps.
func (p Page) Normalize() Page {
	if p.Number < 1 {
		p.Number = 1
	}
	if p.Size < 1 {
		p.Size = DefaultPageSize
	}
	if p.Size > MaxPageSize {
		p.Size = MaxPageSize
	}
	return p
}

// Slice is one page of results.
type Slice[T any] struct {
	Items   []T
	HasNext bool
}

// ParseSearchNumber interprets "#421", "!421" or "421" as a change number.
func ParseSearchNumber(search string) (int, bool) {
	s := strings.TrimSpace(search)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "#"), "!")
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
```

`apps/backend/internal/provider/errors.go`:

```go
package provider

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrAuth           = errors.New("provider authentication failed")
	ErrNotFound       = errors.New("provider resource not found")
	ErrUnavailable    = errors.New("provider unavailable")
	ErrTooManyCommits = errors.New("change has too many commits to verify")
)

// StatusError records an HTTP failure without headers or bodies.
type StatusError struct {
	Method     string
	Path       string
	Status     int
	RetryAfter string
	sentinel   error
}

// NewStatusError classifies status (and rate-limit headers) into a sentinel.
func NewStatusError(method, path string, status int, h http.Header) *StatusError {
	e := &StatusError{Method: method, Path: path, Status: status}
	switch {
	case status == http.StatusForbidden && h.Get("X-Ratelimit-Remaining") == "0":
		e.sentinel = ErrUnavailable
		e.RetryAfter = "reset at " + h.Get("X-Ratelimit-Reset")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		e.sentinel = ErrAuth
	case status == http.StatusNotFound:
		e.sentinel = ErrNotFound
	default:
		e.sentinel = ErrUnavailable
		if ra := h.Get("Retry-After"); ra != "" {
			e.RetryAfter = "retry after " + ra
		}
	}
	return e
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("%s %s returned %d", e.Method, e.Path, e.Status)
	if e.RetryAfter != "" {
		msg += " (" + e.RetryAfter + ")"
	}
	return msg + ": " + e.sentinel.Error()
}

func (e *StatusError) Unwrap() error { return e.sentinel }
```

`apps/backend/internal/provider/model.go`:

```go
package provider

import (
	"fmt"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
)

// Kind identifies the provider implementation.
type Kind string

const (
	KindGitHub Kind = "github"
	KindGitLab Kind = "gitlab"
)

// Repository is an immutable provider repository.
type Repository struct {
	providerID    string
	fullName      string
	name          string
	namespace     string
	defaultBranch string
	webURL        string
	cloneURL      string
}

func (r Repository) ProviderID() string    { return r.providerID }
func (r Repository) FullName() string      { return r.fullName }
func (r Repository) Name() string          { return r.name }
func (r Repository) Namespace() string     { return r.namespace }
func (r Repository) DefaultBranch() string { return r.defaultBranch }
func (r Repository) WebURL() string        { return r.webURL }
func (r Repository) CloneURL() string      { return r.cloneURL }

// Commit is an immutable commit reference.
type Commit struct {
	sha        string
	message    string
	authoredAt time.Time
}

// NewCommit validates the SHA.
func NewCommit(sha, message string, authoredAt time.Time) (Commit, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return Commit{}, fmt.Errorf("commit: %w", err)
	}
	return Commit{sha: sha, message: message, authoredAt: authoredAt}, nil
}

func (c Commit) SHA() string            { return c.sha }
func (c Commit) Message() string        { return c.message }
func (c Commit) AuthoredAt() time.Time  { return c.authoredAt }

// ChangeState is the provider-reported state.
type ChangeState string

const (
	StateMerged ChangeState = "merged"
	StateOpen   ChangeState = "open"
	StateClosed ChangeState = "closed"
)

// ChangeRequest is an immutable PR/MR.
type ChangeRequest struct {
	providerID      string
	repository      Repository
	number          int
	title           string
	author          string
	webURL          string
	sourceBranch    string
	targetBranch    string
	createdAt       time.Time
	mergedAt        time.Time
	state           ChangeState
	commits         []Commit
	commitCount     int
	mergeCommitSHA  string
	squashCommitSHA string
	headSHA         string
	squashed        bool
}

func (c ChangeRequest) ProviderID() string      { return c.providerID }
func (c ChangeRequest) Repository() Repository  { return c.repository }
func (c ChangeRequest) Number() int             { return c.number }
func (c ChangeRequest) Title() string           { return c.title }
func (c ChangeRequest) Author() string          { return c.author }
func (c ChangeRequest) WebURL() string          { return c.webURL }
func (c ChangeRequest) SourceBranch() string    { return c.sourceBranch }
func (c ChangeRequest) TargetBranch() string    { return c.targetBranch }
func (c ChangeRequest) CreatedAt() time.Time    { return c.createdAt }
func (c ChangeRequest) MergedAt() time.Time     { return c.mergedAt }
func (c ChangeRequest) State() ChangeState      { return c.state }
func (c ChangeRequest) CommitCount() int        { return c.commitCount }
func (c ChangeRequest) MergeCommitSHA() string  { return c.mergeCommitSHA }
func (c ChangeRequest) SquashCommitSHA() string { return c.squashCommitSHA }
func (c ChangeRequest) HeadSHA() string         { return c.headSHA }
func (c ChangeRequest) Squashed() bool          { return c.squashed }

// Commits returns a copy of the commit list.
func (c ChangeRequest) Commits() []Commit {
	out := make([]Commit, len(c.commits))
	copy(out, c.commits)
	return out
}

// WithCommits returns a copy carrying the given commits.
func (c ChangeRequest) WithCommits(commits []Commit) ChangeRequest {
	c.commits = append([]Commit(nil), commits...)
	if c.commitCount == 0 {
		c.commitCount = len(commits)
	}
	return c
}

// LandingCandidates lists SHAs to probe, in order (nil entries skipped).
func (c ChangeRequest) LandingCandidates() []string {
	var out []string
	for _, s := range []string{c.mergeCommitSHA, c.squashCommitSHA, c.headSHA} {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
```

`apps/backend/internal/provider/builder.go`:

```go
package provider

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
)

// RepositoryBuilder constructs a Repository.
type RepositoryBuilder struct{ r Repository }

func NewRepositoryBuilder() *RepositoryBuilder { return &RepositoryBuilder{} }

func (b *RepositoryBuilder) SetProviderID(v string) *RepositoryBuilder    { b.r.providerID = v; return b }
func (b *RepositoryBuilder) SetFullName(v string) *RepositoryBuilder      { b.r.fullName = v; return b }
func (b *RepositoryBuilder) SetName(v string) *RepositoryBuilder          { b.r.name = v; return b }
func (b *RepositoryBuilder) SetNamespace(v string) *RepositoryBuilder     { b.r.namespace = v; return b }
func (b *RepositoryBuilder) SetDefaultBranch(v string) *RepositoryBuilder { b.r.defaultBranch = v; return b }
func (b *RepositoryBuilder) SetWebURL(v string) *RepositoryBuilder        { b.r.webURL = v; return b }
func (b *RepositoryBuilder) SetCloneURL(v string) *RepositoryBuilder      { b.r.cloneURL = v; return b }

// Build validates invariants; name/namespace derive from the full name when empty.
func (b *RepositoryBuilder) Build() (Repository, error) {
	r := b.r
	if r.providerID == "" {
		return Repository{}, errors.New("repository: provider id is required")
	}
	if err := gitx.ValidateRepoFullName(r.fullName); err != nil {
		return Repository{}, fmt.Errorf("repository: %w", err)
	}
	idx := strings.LastIndex(r.fullName, "/")
	if r.name == "" {
		r.name = r.fullName[idx+1:]
	}
	if r.namespace == "" {
		r.namespace = r.fullName[:idx]
	}
	return r, nil
}

// ChangeRequestBuilder constructs a ChangeRequest.
type ChangeRequestBuilder struct{ c ChangeRequest }

func NewChangeRequestBuilder() *ChangeRequestBuilder { return &ChangeRequestBuilder{} }

func (b *ChangeRequestBuilder) SetProviderID(v string) *ChangeRequestBuilder      { b.c.providerID = v; return b }
func (b *ChangeRequestBuilder) SetRepository(v Repository) *ChangeRequestBuilder  { b.c.repository = v; return b }
func (b *ChangeRequestBuilder) SetNumber(v int) *ChangeRequestBuilder             { b.c.number = v; return b }
func (b *ChangeRequestBuilder) SetTitle(v string) *ChangeRequestBuilder           { b.c.title = v; return b }
func (b *ChangeRequestBuilder) SetAuthor(v string) *ChangeRequestBuilder          { b.c.author = v; return b }
func (b *ChangeRequestBuilder) SetWebURL(v string) *ChangeRequestBuilder          { b.c.webURL = v; return b }
func (b *ChangeRequestBuilder) SetSourceBranch(v string) *ChangeRequestBuilder    { b.c.sourceBranch = v; return b }
func (b *ChangeRequestBuilder) SetTargetBranch(v string) *ChangeRequestBuilder    { b.c.targetBranch = v; return b }
func (b *ChangeRequestBuilder) SetCreatedAt(v time.Time) *ChangeRequestBuilder    { b.c.createdAt = v; return b }
func (b *ChangeRequestBuilder) SetMergedAt(v time.Time) *ChangeRequestBuilder     { b.c.mergedAt = v; return b }
func (b *ChangeRequestBuilder) SetState(v ChangeState) *ChangeRequestBuilder      { b.c.state = v; return b }
func (b *ChangeRequestBuilder) SetCommits(v []Commit) *ChangeRequestBuilder       { b.c.commits = append([]Commit(nil), v...); return b }
func (b *ChangeRequestBuilder) SetCommitCount(v int) *ChangeRequestBuilder        { b.c.commitCount = v; return b }
func (b *ChangeRequestBuilder) SetMergeCommitSHA(v string) *ChangeRequestBuilder  { b.c.mergeCommitSHA = v; return b }
func (b *ChangeRequestBuilder) SetSquashCommitSHA(v string) *ChangeRequestBuilder { b.c.squashCommitSHA = v; return b }
func (b *ChangeRequestBuilder) SetHeadSHA(v string) *ChangeRequestBuilder         { b.c.headSHA = v; return b }
func (b *ChangeRequestBuilder) SetSquashed(v bool) *ChangeRequestBuilder          { b.c.squashed = v; return b }

// Build validates invariants.
func (b *ChangeRequestBuilder) Build() (ChangeRequest, error) {
	c := b.c
	if c.providerID == "" {
		return ChangeRequest{}, errors.New("change: provider id is required")
	}
	if c.repository.fullName == "" {
		return ChangeRequest{}, errors.New("change: repository is required")
	}
	if c.number <= 0 {
		return ChangeRequest{}, errors.New("change: number must be positive")
	}
	if c.title == "" {
		return ChangeRequest{}, errors.New("change: title is required")
	}
	if c.targetBranch == "" {
		return ChangeRequest{}, errors.New("change: target branch is required")
	}
	if c.state == "" {
		c.state = StateOpen
	}
	for _, sha := range []string{c.mergeCommitSHA, c.squashCommitSHA, c.headSHA} {
		if sha != "" {
			if err := gitx.ValidateSHA(sha); err != nil {
				return ChangeRequest{}, fmt.Errorf("change %d: %w", c.number, err)
			}
		}
	}
	if c.commitCount == 0 {
		c.commitCount = len(c.commits)
	}
	return c, nil
}
```

`apps/backend/internal/provider/provider.go`:

```go
package provider

import (
	"context"

	"github.com/jtumidanski/converge/internal/gitx"
)

// GitProvider isolates provider behaviour behind one interface.
type GitProvider interface {
	ID() string
	Kind() Kind
	DisplayName() string
	BaseURL() string
	ListRepositories(ctx context.Context, page Page) (Slice[Repository], error)
	GetRepository(ctx context.Context, fullName string) (Repository, error)
	ListMergedChanges(ctx context.Context, repo Repository, targetBranch, search string, page Page) (Slice[ChangeRequest], error)
	GetChange(ctx context.Context, repo Repository, number int) (ChangeRequest, error)
	GetChangeCommits(ctx context.Context, repo Repository, number int) ([]Commit, error)
	CloneURL(repo Repository) string
	// AuthorizeGit attaches credentials to spec via environment only.
	AuthorizeGit(repo Repository, spec *gitx.Spec) error
}

// GitUser is the Basic-auth username each provider accepts for tokens.
func GitUser(kind Kind) string {
	if kind == KindGitHub {
		return "x-access-token"
	}
	return "oauth2"
}
```

`apps/backend/internal/provider/registry.go`:

```go
package provider

import (
	"fmt"
	"sort"
)

// Registry maps provider IDs to implementations.
type Registry struct {
	byID map[string]GitProvider
}

func NewRegistry() *Registry { return &Registry{byID: map[string]GitProvider{}} }

// Register adds p; duplicate IDs are rejected.
func (r *Registry) Register(p GitProvider) error {
	if _, dup := r.byID[p.ID()]; dup {
		return fmt.Errorf("provider %q registered twice", p.ID())
	}
	r.byID[p.ID()] = p
	return nil
}

func (r *Registry) Get(id string) (GitProvider, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// All returns providers sorted by ID.
func (r *Registry) All() []GitProvider {
	out := make([]GitProvider, 0, len(r.byID))
	for _, p := range r.byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
```

`apps/backend/internal/provider/httpjson.go`:

```go
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DoJSON performs req, maps non-2xx statuses to *StatusError, and decodes JSON into out.
func DoJSON(ctx context.Context, client *http.Client, req *http.Request, out any) (http.Header, error) {
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, ErrUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return resp.Header, NewStatusError(req.Method, req.URL.Path, resp.StatusCode, resp.Header)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, fmt.Errorf("%s %s: decode response: %w", req.Method, req.URL.Path, ErrUnavailable)
		}
	}
	return resp.Header, nil
}
```

- [ ] **Step 4: Run provider tests**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/provider/`
Expected: PASS.

- [ ] **Step 5: Implement the fake provider**

`apps/backend/internal/provider/fake/fake.go`:

```go
// Package fake is an in-memory GitProvider for unit and integration tests.
package fake

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

// Provider stores repositories and changes in memory.
type Provider struct {
	id      string
	kind    provider.Kind
	mu      sync.Mutex
	repos   map[string]provider.Repository
	changes map[string]map[int]provider.ChangeRequest
	commits map[string]map[int][]provider.Commit
	nextErr error
}

// New creates an empty provider.
func New(id string, kind provider.Kind) *Provider {
	return &Provider{id: id, kind: kind, repos: map[string]provider.Repository{}, changes: map[string]map[int]provider.ChangeRequest{}, commits: map[string]map[int][]provider.Commit{}}
}

func (p *Provider) ID() string           { return p.id }
func (p *Provider) Kind() provider.Kind  { return p.kind }
func (p *Provider) DisplayName() string  { return "Fake " + p.id }
func (p *Provider) BaseURL() string      { return "fake://" + p.id }

// AddRepository registers a repository.
func (p *Provider) AddRepository(r provider.Repository) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.repos[r.FullName()] = r
}

// AddChange registers a change request under its repository.
func (p *Provider) AddChange(cr provider.ChangeRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := cr.Repository().FullName()
	if p.changes[key] == nil {
		p.changes[key] = map[int]provider.ChangeRequest{}
	}
	p.changes[key][cr.Number()] = cr
	if len(cr.Commits()) > 0 {
		p.setCommitsLocked(key, cr.Number(), cr.Commits())
	}
}

// SetCommits sets the commits returned by GetChangeCommits.
func (p *Provider) SetCommits(fullName string, number int, commits []provider.Commit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setCommitsLocked(fullName, number, commits)
}

func (p *Provider) setCommitsLocked(fullName string, number int, commits []provider.Commit) {
	if p.commits[fullName] == nil {
		p.commits[fullName] = map[int][]provider.Commit{}
	}
	p.commits[fullName][number] = commits
}

// FailWith makes the next call return err.
func (p *Provider) FailWith(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextErr = err
}

func (p *Provider) takeErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.nextErr
	p.nextErr = nil
	return err
}

func (p *Provider) ListRepositories(_ context.Context, page provider.Page) (provider.Slice[provider.Repository], error) {
	if err := p.takeErr(); err != nil {
		return provider.Slice[provider.Repository]{}, err
	}
	p.mu.Lock()
	all := make([]provider.Repository, 0, len(p.repos))
	for _, r := range p.repos {
		all = append(all, r)
	}
	p.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].FullName() < all[j].FullName() })
	return paginate(all, page), nil
}

func (p *Provider) GetRepository(_ context.Context, fullName string) (provider.Repository, error) {
	if err := p.takeErr(); err != nil {
		return provider.Repository{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.repos[fullName]
	if !ok {
		return provider.Repository{}, provider.ErrNotFound
	}
	return r, nil
}

func (p *Provider) ListMergedChanges(_ context.Context, repo provider.Repository, target, search string, page provider.Page) (provider.Slice[provider.ChangeRequest], error) {
	if err := p.takeErr(); err != nil {
		return provider.Slice[provider.ChangeRequest]{}, err
	}
	p.mu.Lock()
	var all []provider.ChangeRequest
	for _, cr := range p.changes[repo.FullName()] {
		if cr.State() == provider.StateMerged && cr.TargetBranch() == target && matches(cr, search) {
			all = append(all, cr)
		}
	}
	p.mu.Unlock()
	sort.Slice(all, func(i, j int) bool {
		if !all[i].MergedAt().Equal(all[j].MergedAt()) {
			return all[i].MergedAt().After(all[j].MergedAt())
		}
		return all[i].Number() > all[j].Number()
	})
	return paginate(all, page), nil
}

func matches(cr provider.ChangeRequest, search string) bool {
	if search == "" {
		return true
	}
	if n, ok := provider.ParseSearchNumber(search); ok {
		return cr.Number() == n
	}
	s := strings.ToLower(search)
	return strings.Contains(strings.ToLower(cr.Title()), s) || strings.Contains(strings.ToLower(cr.Author()), s)
}

func (p *Provider) GetChange(_ context.Context, repo provider.Repository, number int) (provider.ChangeRequest, error) {
	if err := p.takeErr(); err != nil {
		return provider.ChangeRequest{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cr, ok := p.changes[repo.FullName()][number]
	if !ok {
		return provider.ChangeRequest{}, provider.ErrNotFound
	}
	return cr.WithCommits(nil), nil
}

func (p *Provider) GetChangeCommits(_ context.Context, repo provider.Repository, number int) ([]provider.Commit, error) {
	if err := p.takeErr(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.changes[repo.FullName()][number]; !ok {
		return nil, provider.ErrNotFound
	}
	return append([]provider.Commit(nil), p.commits[repo.FullName()][number]...), nil
}

func (p *Provider) CloneURL(repo provider.Repository) string { return repo.CloneURL() }

func (p *Provider) AuthorizeGit(provider.Repository, *gitx.Spec) error { return nil }

func paginate[T any](all []T, page provider.Page) provider.Slice[T] {
	page = page.Normalize()
	start := (page.Number - 1) * page.Size
	if start >= len(all) {
		return provider.Slice[T]{Items: []T{}}
	}
	end := start + page.Size
	if end > len(all) {
		end = len(all)
	}
	return provider.Slice[T]{Items: all[start:end], HasNext: end < len(all)}
}

var _ provider.GitProvider = (*Provider)(nil)
```

- [ ] **Step 6: Verify and commit**

Run: `cd <worktree-root>/apps/backend && go build ./... && go test -race -count=1 ./internal/provider/... && go tool golangci-lint run ./internal/provider/...`
Expected: PASS.

```bash
cd <worktree-root> && git add apps/backend/internal/provider && git commit -m "feat(task-001): provider model, registry, errors and fake provider"
```

---

### Task 5: GitHub provider

**Files:**
- Create: `apps/backend/internal/provider/github/client.go`, `mapping.go`, `list.go`, `client_test.go`, `testdata/repos.json`, `testdata/repo.json`, `testdata/pulls_closed_p1.json`, `testdata/pulls_closed_p2.json`, `testdata/pull_421.json`, `testdata/pull_421_commits.json`

**Interfaces:**
- Consumes: `provider.*`, `gitx.Spec`, `gitx.CredentialEnv`, `config.Secret`.
- Produces: `func New(id, displayName, baseURL string, token config.Secret, client *http.Client, now func() time.Time) *Client` implementing `provider.GitProvider`; `const APIVersion = "2022-11-28"`, `const MaxScanPages = 10`, `const ScanCacheTTL = 60 * time.Second`, `const MaxPRCommits = 250`.

- [ ] **Step 1: Write fixtures**

`testdata/repo.json` (fields per `GET /repos/{owner}/{repo}`):

```json
{ "id": 1, "name": "server", "full_name": "atlas/server", "owner": { "login": "atlas" },
  "default_branch": "main", "html_url": "https://github.com/atlas/server",
  "clone_url": "https://github.com/atlas/server.git", "private": true }
```

`testdata/repos.json`:

```json
[
  { "id": 1, "name": "server", "full_name": "atlas/server", "owner": { "login": "atlas" }, "default_branch": "main", "html_url": "https://github.com/atlas/server", "clone_url": "https://github.com/atlas/server.git" },
  { "id": 2, "name": "web", "full_name": "atlas/web", "owner": { "login": "atlas" }, "default_branch": "develop", "html_url": "https://github.com/atlas/web", "clone_url": "https://github.com/atlas/web.git" }
]
```

`testdata/pulls_closed_p1.json` (list items: no `merged`, no `merge_commit_sha` relied upon):

```json
[
  { "number": 435, "title": "Refactor field service", "state": "closed", "html_url": "https://github.com/atlas/server/pull/435",
    "user": { "login": "jsmith" }, "created_at": "2026-08-25T09:00:00Z", "merged_at": "2026-08-26T10:00:00Z",
    "head": { "ref": "refactor/field", "sha": "cccccccccccccccccccccccccccccccccccccccc" }, "base": { "ref": "main" } },
  { "number": 430, "title": "Closed without merge", "state": "closed", "html_url": "https://github.com/atlas/server/pull/430",
    "user": { "login": "nobody" }, "created_at": "2026-08-24T09:00:00Z", "merged_at": null,
    "head": { "ref": "junk", "sha": "dddddddddddddddddddddddddddddddddddddddd" }, "base": { "ref": "main" } },
  { "number": 421, "title": "Add field-state endpoint", "state": "closed", "html_url": "https://github.com/atlas/server/pull/421",
    "user": { "login": "jsmith" }, "created_at": "2026-08-20T09:00:00Z", "merged_at": "2026-08-21T14:02:11Z",
    "head": { "ref": "feat/field-state", "sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }, "base": { "ref": "main" } }
]
```

`testdata/pulls_closed_p2.json`:

```json
[
  { "number": 427, "title": "Fix typo by mkay", "state": "closed", "html_url": "https://github.com/atlas/server/pull/427",
    "user": { "login": "mkay" }, "created_at": "2026-08-22T09:00:00Z", "merged_at": "2026-08-23T08:00:00Z",
    "head": { "ref": "fix/typo", "sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" }, "base": { "ref": "main" } }
]
```

`testdata/pull_421.json` (single PR includes `merged`, `merge_commit_sha`, `commits`):

```json
{ "number": 421, "title": "Add field-state endpoint", "state": "closed", "merged": true,
  "html_url": "https://github.com/atlas/server/pull/421", "user": { "login": "jsmith" },
  "created_at": "2026-08-20T09:00:00Z", "merged_at": "2026-08-21T14:02:11Z",
  "merge_commit_sha": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "commits": 2,
  "head": { "ref": "feat/field-state", "sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }, "base": { "ref": "main" } }
```

`testdata/pull_421_commits.json`:

```json
[
  { "sha": "1111111111111111111111111111111111111111", "commit": { "message": "feat: endpoint", "author": { "date": "2026-08-20T10:00:00Z" } } },
  { "sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "commit": { "message": "test: endpoint", "author": { "date": "2026-08-20T11:00:00Z" } } }
]
```

- [ ] **Step 2: Write failing client tests**

`apps/backend/internal/provider/github/client_test.go`:

```go
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type call struct{ path, query string }

func newServer(t *testing.T) (*httptest.Server, *[]call) {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.URL.Path, r.URL.RawQuery})
		if r.Header.Get("Authorization") != "Bearer ghp_test" || r.Header.Get("X-GitHub-Api-Version") != APIVersion || r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("missing headers on %s: %v", r.URL.Path, r.Header)
			w.WriteHeader(500)
			return
		}
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/user/repos":
			if q.Get("page") == "1" {
				w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?page=2>; rel="next"`, "http://x"))
			}
			_, _ = w.Write(fixture(t, "repos.json"))
		case r.URL.Path == "/repos/atlas/server":
			_, _ = w.Write(fixture(t, "repo.json"))
		case r.URL.Path == "/repos/atlas/missing":
			w.WriteHeader(404)
		case r.URL.Path == "/repos/atlas/server/pulls":
			if q.Get("state") != "closed" || q.Get("base") != "main" || q.Get("per_page") != "100" {
				t.Errorf("bad list query %s", r.URL.RawQuery)
			}
			if q.Get("page") == "1" {
				w.Header().Set("Link", `<http://x/repos/atlas/server/pulls?page=2>; rel="next"`)
				_, _ = w.Write(fixture(t, "pulls_closed_p1.json"))
			} else {
				_, _ = w.Write(fixture(t, "pulls_closed_p2.json"))
			}
		case r.URL.Path == "/repos/atlas/server/pulls/421":
			_, _ = w.Write(fixture(t, "pull_421.json"))
		case r.URL.Path == "/repos/atlas/server/pulls/421/commits":
			_, _ = w.Write(fixture(t, "pull_421_commits.json"))
		case r.URL.Path == "/repos/atlas/server/pulls/999":
			w.WriteHeader(404)
		case r.URL.Path == "/repos/atlas/server/pulls/500":
			w.Header().Set("X-Ratelimit-Remaining", "0")
			w.Header().Set("X-Ratelimit-Reset", "1700000000")
			w.WriteHeader(403)
		case r.URL.Path == "/repos/atlas/server/pulls/401":
			w.WriteHeader(401)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return New("gh", "GitHub", srv.URL, config.NewSecret("ghp_test"), srv.Client(), func() time.Time { return now })
}

func TestListRepositoriesPagination(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv)
	s, err := c.ListRepositories(context.Background(), provider.Page{Number: 1, Size: 2})
	if err != nil || len(s.Items) != 2 || !s.HasNext {
		t.Fatalf("page1: %v %+v", err, s)
	}
	if s.Items[0].FullName() != "atlas/server" || s.Items[0].DefaultBranch() != "main" || s.Items[0].CloneURL() != "https://github.com/atlas/server.git" || s.Items[1].DefaultBranch() != "develop" {
		t.Errorf("mapping: %+v", s.Items)
	}
	s, err = c.ListRepositories(context.Background(), provider.Page{Number: 2, Size: 2})
	if err != nil || s.HasNext {
		t.Fatalf("page2: %v %+v", err, s)
	}
}

func TestGetRepositoryAndErrors(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv)
	r, err := c.GetRepository(context.Background(), "atlas/server")
	if err != nil || r.Namespace() != "atlas" || r.WebURL() != "https://github.com/atlas/server" {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := c.GetRepository(context.Background(), "atlas/missing"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	if _, err := c.GetRepository(context.Background(), "../etc"); err == nil {
		t.Error("invalid name accepted")
	}
	if _, err := c.GetChange(context.Background(), r, 500); !errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("rate limit: %v", err)
	}
	if _, err := c.GetChange(context.Background(), r, 401); !errors.Is(err, provider.ErrAuth) || strings.Contains(err.Error(), "ghp_test") {
		t.Errorf("auth: %v", err)
	}
}

func TestGetChangeAndCommits(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	cr, err := c.GetChange(context.Background(), repo, 421)
	if err != nil {
		t.Fatal(err)
	}
	if cr.State() != provider.StateMerged || cr.MergeCommitSHA() != strings.Repeat("e", 40) || cr.HeadSHA() != strings.Repeat("a", 40) || cr.CommitCount() != 2 || cr.Author() != "jsmith" || cr.SourceBranch() != "feat/field-state" || cr.TargetBranch() != "main" {
		t.Errorf("mapping: %+v", cr)
	}
	commits, err := c.GetChangeCommits(context.Background(), repo, 421)
	if err != nil || len(commits) != 2 || commits[0].SHA() != strings.Repeat("1", 40) || commits[1].Message() != "test: endpoint" {
		t.Errorf("commits: %v %+v", err, commits)
	}
	if _, err := c.GetChange(context.Background(), repo, 999); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("999: %v", err)
	}
}

func TestListMergedChangesScansFiltersSortsAndCaches(t *testing.T) {
	srv, calls := newServer(t)
	c := newClient(t, srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	s, err := c.ListMergedChanges(context.Background(), repo, "main", "", provider.Page{Number: 1, Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Items) != 2 || s.Items[0].Number() != 435 || s.Items[1].Number() != 427 || !s.HasNext {
		t.Fatalf("page1 = %v next=%v", numbers(s.Items), s.HasNext)
	}
	if s.Items[0].MergeCommitSHA() != "" {
		t.Error("list responses must leave MergeCommitSHA empty")
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "", provider.Page{Number: 2, Size: 2})
	if err != nil || len(s.Items) != 1 || s.Items[0].Number() != 421 || s.HasNext {
		t.Fatalf("page2 = %v next=%v err=%v", numbers(s.Items), s.HasNext, err)
	}
	listCalls := 0
	for _, cl := range *calls {
		if cl.path == "/repos/atlas/server/pulls" {
			listCalls++
		}
	}
	if listCalls != 2 {
		t.Errorf("provider pages fetched = %d, want 2 (cache must serve page 2)", listCalls)
	}
	// title / author substring search
	s, _ = c.ListMergedChanges(context.Background(), repo, "main", "MKAY", provider.Page{})
	if len(s.Items) != 1 || s.Items[0].Number() != 427 {
		t.Errorf("author search = %v", numbers(s.Items))
	}
	s, _ = c.ListMergedChanges(context.Background(), repo, "main", "field", provider.Page{})
	if len(s.Items) != 2 {
		t.Errorf("title search = %v", numbers(s.Items))
	}
	// numeric search hits GET /pulls/{n}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "#421", provider.Page{})
	if err != nil || len(s.Items) != 1 || s.Items[0].MergeCommitSHA() == "" {
		t.Errorf("numeric search: %v %v", err, numbers(s.Items))
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "develop", "#421", provider.Page{})
	if err != nil || len(s.Items) != 0 {
		t.Errorf("numeric search wrong base must be empty: %v %v", err, numbers(s.Items))
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "999", provider.Page{})
	if err != nil || len(s.Items) != 0 {
		t.Errorf("numeric search missing must be empty: %v", err)
	}
}

func TestAuthorizeGitUsesEnvOnly(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	spec := gitx.Spec{Args: []string{"fetch"}}
	if err := c.AuthorizeGit(repo, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Env) != 3 || spec.Env[1] != "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader" {
		t.Errorf("env = %v", spec.Env)
	}
	for _, a := range spec.Args {
		if strings.Contains(a, "ghp_test") {
			t.Error("token in argv")
		}
	}
	if c.CloneURL(repo) != "https://github.com/atlas/server.git" {
		t.Error("clone url")
	}
}

func numbers(items []provider.ChangeRequest) []int {
	out := make([]int, len(items))
	for i, it := range items {
		out[i] = it.Number()
	}
	return out
}
```

- [ ] **Step 3: Run to see failures**

Run: `cd <worktree-root>/apps/backend && go test ./internal/provider/github/`
Expected: compile failure.

- [ ] **Step 4: Implement the client**

`apps/backend/internal/provider/github/client.go`:

```go
// Package github implements provider.GitProvider against the GitHub REST API.
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

const (
	APIVersion   = "2022-11-28"
	MaxScanPages = 10
	ScanCacheTTL = 60 * time.Second
	MaxPRCommits = 250
	providerPage = 100
)

var linkNextRe = regexp.MustCompile(`<[^>]+>;\s*rel="next"`)

// Client talks to one GitHub instance.
type Client struct {
	id          string
	displayName string
	baseURL     string
	token       config.Secret
	http        *http.Client
	now         func() time.Time
	mu          sync.Mutex
	scans       map[string]*scanEntry
}

// New builds a client; baseURL has no trailing slash.
func New(id, displayName, baseURL string, token config.Secret, client *http.Client, now func() time.Time) *Client {
	if now == nil {
		now = time.Now
	}
	return &Client{id: id, displayName: displayName, baseURL: baseURL, token: token, http: client, now: now, scans: map[string]*scanEntry{}}
}

func (c *Client) ID() string           { return c.id }
func (c *Client) Kind() provider.Kind  { return provider.KindGitHub }
func (c *Client) DisplayName() string  { return c.displayName }
func (c *Client) BaseURL() string      { return c.baseURL }

func (c *Client) newRequest(ctx context.Context, path string, query url.Values) (*http.Request, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token.Reveal())
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	return req, nil
}

// get performs a GET and reports whether a rel="next" link exists.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (bool, error) {
	req, err := c.newRequest(ctx, path, query)
	if err != nil {
		return false, err
	}
	h, err := provider.DoJSON(ctx, c.http, req, out)
	if err != nil {
		return false, err
	}
	return linkNextRe.MatchString(h.Get("Link")), nil
}

func (c *Client) ListRepositories(ctx context.Context, page provider.Page) (provider.Slice[provider.Repository], error) {
	page = page.Normalize()
	q := url.Values{"affiliation": {"owner,collaborator,organization_member"}, "sort": {"full_name"}, "per_page": {strconv.Itoa(page.Size)}, "page": {strconv.Itoa(page.Number)}}
	var raw []repoJSON
	hasNext, err := c.get(ctx, "/user/repos", q, &raw)
	if err != nil {
		return provider.Slice[provider.Repository]{}, err
	}
	items := make([]provider.Repository, 0, len(raw))
	for _, r := range raw {
		repo, err := r.toModel(c.id)
		if err != nil {
			return provider.Slice[provider.Repository]{}, err
		}
		items = append(items, repo)
	}
	return provider.Slice[provider.Repository]{Items: items, HasNext: hasNext}, nil
}

func (c *Client) GetRepository(ctx context.Context, fullName string) (provider.Repository, error) {
	if err := gitx.ValidateRepoFullName(fullName); err != nil {
		return provider.Repository{}, err
	}
	var raw repoJSON
	if _, err := c.get(ctx, "/repos/"+fullName, nil, &raw); err != nil {
		return provider.Repository{}, err
	}
	return raw.toModel(c.id)
}

func (c *Client) GetChange(ctx context.Context, repo provider.Repository, number int) (provider.ChangeRequest, error) {
	if err := gitx.ValidateChangeNumber(number); err != nil {
		return provider.ChangeRequest{}, err
	}
	var raw pullJSON
	if _, err := c.get(ctx, fmt.Sprintf("/repos/%s/pulls/%d", repo.FullName(), number), nil, &raw); err != nil {
		return provider.ChangeRequest{}, err
	}
	return raw.toModel(c.id, repo)
}

func (c *Client) GetChangeCommits(ctx context.Context, repo provider.Repository, number int) ([]provider.Commit, error) {
	if err := gitx.ValidateChangeNumber(number); err != nil {
		return nil, err
	}
	var out []provider.Commit
	for page := 1; ; page++ {
		var raw []commitJSON
		q := url.Values{"per_page": {strconv.Itoa(providerPage)}, "page": {strconv.Itoa(page)}}
		hasNext, err := c.get(ctx, fmt.Sprintf("/repos/%s/pulls/%d/commits", repo.FullName(), number), q, &raw)
		if err != nil {
			return nil, err
		}
		for _, r := range raw {
			cm, err := r.toModel()
			if err != nil {
				return nil, err
			}
			out = append(out, cm)
		}
		if len(out) > MaxPRCommits {
			return nil, fmt.Errorf("pull %d: %w", number, provider.ErrTooManyCommits)
		}
		if !hasNext || len(raw) == 0 {
			return out, nil
		}
	}
}

func (c *Client) CloneURL(repo provider.Repository) string { return repo.CloneURL() }

// AuthorizeGit injects the token through GIT_CONFIG_* env, scoped to the clone host.
func (c *Client) AuthorizeGit(repo provider.Repository, spec *gitx.Spec) error {
	env, err := gitx.CredentialEnv(repo.CloneURL(), provider.GitUser(provider.KindGitHub), c.token.Reveal())
	if err != nil {
		return err
	}
	spec.Env = append(spec.Env, env...)
	return nil
}

var _ provider.GitProvider = (*Client)(nil)
```

`apps/backend/internal/provider/github/mapping.go`:

```go
package github

import (
	"fmt"
	"time"

	"github.com/jtumidanski/converge/internal/provider"
)

type repoJSON struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Owner         struct{ Login string `json:"login"` } `json:"owner"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
}

func (r repoJSON) toModel(providerID string) (provider.Repository, error) {
	return provider.NewRepositoryBuilder().SetProviderID(providerID).SetFullName(r.FullName).SetName(r.Name).
		SetNamespace(r.Owner.Login).SetDefaultBranch(r.DefaultBranch).SetWebURL(r.HTMLURL).SetCloneURL(r.CloneURL).Build()
}

type refJSON struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type pullJSON struct {
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	Merged         *bool      `json:"merged"`
	HTMLURL        string     `json:"html_url"`
	User           struct{ Login string `json:"login"` } `json:"user"`
	CreatedAt      time.Time  `json:"created_at"`
	MergedAt       *time.Time `json:"merged_at"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	Commits        int        `json:"commits"`
	Head           refJSON    `json:"head"`
	Base           refJSON    `json:"base"`
}

func (p pullJSON) state() provider.ChangeState {
	if p.MergedAt != nil || (p.Merged != nil && *p.Merged) {
		return provider.StateMerged
	}
	if p.State == "open" {
		return provider.StateOpen
	}
	return provider.StateClosed
}

func (p pullJSON) toModel(providerID string, repo provider.Repository) (provider.ChangeRequest, error) {
	b := provider.NewChangeRequestBuilder().SetProviderID(providerID).SetRepository(repo).SetNumber(p.Number).SetTitle(p.Title).
		SetAuthor(p.User.Login).SetWebURL(p.HTMLURL).SetSourceBranch(p.Head.Ref).SetTargetBranch(p.Base.Ref).
		SetCreatedAt(p.CreatedAt).SetState(p.state()).SetHeadSHA(p.Head.SHA).SetCommitCount(p.Commits)
	if p.MergedAt != nil {
		b.SetMergedAt(*p.MergedAt)
	}
	if p.MergeCommitSHA != "" {
		b.SetMergeCommitSHA(p.MergeCommitSHA)
	}
	cr, err := b.Build()
	if err != nil {
		return provider.ChangeRequest{}, fmt.Errorf("github pull %d: %w", p.Number, err)
	}
	return cr, nil
}

type commitJSON struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct{ Date time.Time `json:"date"` } `json:"author"`
	} `json:"commit"`
}

func (c commitJSON) toModel() (provider.Commit, error) {
	return provider.NewCommit(c.SHA, c.Commit.Message, c.Commit.Author.Date)
}
```

`apps/backend/internal/provider/github/list.go`:

```go
package github

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

// scanEntry caches merged PRs collected from the closed-PR listing.
type scanEntry struct {
	items     []provider.ChangeRequest
	nextPage  int // 0 when the provider has no more pages
	capped    bool
	fetchedAt time.Time
}

func (c *Client) scanKey(repo provider.Repository, target string) string {
	return repo.FullName() + "\x00" + target
}

// ensureScanned returns merged PRs for (repo, target), fetching provider pages
// until at least `need` items are collected, the provider runs out, or MaxScanPages is hit.
func (c *Client) ensureScanned(ctx context.Context, repo provider.Repository, target string, need int) (*scanEntry, error) {
	key := c.scanKey(repo, target)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.scans[key]
	if entry == nil || c.now().Sub(entry.fetchedAt) > ScanCacheTTL {
		entry = &scanEntry{nextPage: 1, fetchedAt: c.now()}
		c.scans[key] = entry
	}
	for entry.nextPage != 0 && (need < 0 || len(entry.items) < need) {
		if entry.nextPage > MaxScanPages {
			entry.capped = true
			break
		}
		q := url.Values{"state": {"closed"}, "base": {target}, "sort": {"updated"}, "direction": {"desc"}, "per_page": {strconv.Itoa(providerPage)}, "page": {strconv.Itoa(entry.nextPage)}}
		var raw []pullJSON
		hasNext, err := c.get(ctx, "/repos/"+repo.FullName()+"/pulls", q, &raw)
		if err != nil {
			return nil, err
		}
		for _, p := range raw {
			if p.MergedAt == nil || p.Base.Ref != target {
				continue
			}
			cr, err := p.toModel(c.id, repo)
			if err != nil {
				return nil, err
			}
			entry.items = append(entry.items, cr)
		}
		if hasNext && len(raw) > 0 {
			entry.nextPage++
		} else {
			entry.nextPage = 0
		}
	}
	return entry, nil
}

func (c *Client) ListMergedChanges(ctx context.Context, repo provider.Repository, target, search string, page provider.Page) (provider.Slice[provider.ChangeRequest], error) {
	page = page.Normalize()
	if err := gitx.ValidateBranchSyntax(target); err != nil {
		return provider.Slice[provider.ChangeRequest]{}, err
	}
	empty := provider.Slice[provider.ChangeRequest]{Items: []provider.ChangeRequest{}}
	if n, ok := provider.ParseSearchNumber(search); ok {
		cr, err := c.GetChange(ctx, repo, n)
		if errors.Is(err, provider.ErrNotFound) {
			return empty, nil
		}
		if err != nil {
			return empty, err
		}
		if cr.State() != provider.StateMerged || cr.TargetBranch() != target {
			return empty, nil
		}
		return provider.Slice[provider.ChangeRequest]{Items: []provider.ChangeRequest{cr}}, nil
	}
	need := page.Number*page.Size + 1
	if search != "" {
		need = -1 // filtering shrinks the set; scan to the cap
	}
	entry, err := c.ensureScanned(ctx, repo, target, need)
	if err != nil {
		return empty, err
	}
	filtered := filterChanges(entry.items, search)
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].MergedAt().Equal(filtered[j].MergedAt()) {
			return filtered[i].MergedAt().After(filtered[j].MergedAt())
		}
		return filtered[i].Number() > filtered[j].Number()
	})
	start := (page.Number - 1) * page.Size
	if start >= len(filtered) {
		return provider.Slice[provider.ChangeRequest]{Items: []provider.ChangeRequest{}, HasNext: false}, nil
	}
	end := start + page.Size
	if end > len(filtered) {
		end = len(filtered)
	}
	hasNext := end < len(filtered) || (entry.nextPage != 0 && !entry.capped)
	return provider.Slice[provider.ChangeRequest]{Items: filtered[start:end], HasNext: hasNext}, nil
}

func filterChanges(items []provider.ChangeRequest, search string) []provider.ChangeRequest {
	out := make([]provider.ChangeRequest, 0, len(items))
	s := strings.ToLower(strings.TrimSpace(search))
	for _, cr := range items {
		if s == "" || strings.Contains(strings.ToLower(cr.Title()), s) || strings.Contains(strings.ToLower(cr.Author()), s) {
			out = append(out, cr)
		}
	}
	return out
}
```

- [ ] **Step 5: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/provider/github/ && go tool golangci-lint run ./internal/provider/github/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/provider/github && git commit -m "feat(task-001): GitHub provider with fixture tests"
```

---

### Task 6: GitLab provider

**Files:**
- Create: `apps/backend/internal/provider/gitlab/client.go`, `mapping.go`, `client_test.go`, `testdata/projects.json`, `testdata/project.json`, `testdata/mrs_p1.json`, `testdata/mr_421.json`, `testdata/mr_421_commits.json`

**Interfaces:**
- Produces: `func New(id, displayName, baseURL string, token config.Secret, client *http.Client) *Client` implementing `provider.GitProvider`. API root is `baseURL + "/api/v4"`.

- [ ] **Step 1: Write fixtures**

`testdata/project.json`:

```json
{ "id": 42, "name": "server", "path": "server", "path_with_namespace": "atlas/server",
  "namespace": { "full_path": "atlas" }, "default_branch": "main",
  "web_url": "https://gitlab.company.com/atlas/server", "http_url_to_repo": "https://gitlab.company.com/atlas/server.git",
  "merge_method": "merge", "squash_option": "default_off" }
```

`testdata/projects.json`:

```json
[
  { "id": 42, "name": "server", "path": "server", "path_with_namespace": "atlas/server", "namespace": { "full_path": "atlas" }, "default_branch": "main", "web_url": "https://gitlab.company.com/atlas/server", "http_url_to_repo": "https://gitlab.company.com/atlas/server.git" },
  { "id": 43, "name": "web", "path": "web", "path_with_namespace": "atlas/apps/web", "namespace": { "full_path": "atlas/apps" }, "default_branch": "develop", "web_url": "https://gitlab.company.com/atlas/apps/web", "http_url_to_repo": "https://gitlab.company.com/atlas/apps/web.git" }
]
```

`testdata/mrs_p1.json`:

```json
[
  { "iid": 435, "title": "Refactor field service", "state": "merged", "web_url": "https://gitlab.company.com/atlas/server/-/merge_requests/435",
    "author": { "username": "jsmith" }, "created_at": "2026-08-25T09:00:00Z", "merged_at": "2026-08-26T10:00:00Z",
    "source_branch": "refactor/field", "target_branch": "main", "sha": "cccccccccccccccccccccccccccccccccccccccc",
    "merge_commit_sha": "ffffffffffffffffffffffffffffffffffffffff", "squash_commit_sha": null, "squash": false },
  { "iid": 421, "title": "Add field-state endpoint", "state": "merged", "web_url": "https://gitlab.company.com/atlas/server/-/merge_requests/421",
    "author": { "username": "jsmith" }, "created_at": "2026-08-20T09:00:00Z", "merged_at": "2026-08-21T14:02:11Z",
    "source_branch": "feat/field-state", "target_branch": "main", "sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "merge_commit_sha": null, "squash_commit_sha": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "squash": true }
]
```

`testdata/mr_421.json`: same object as the second item above.

`testdata/mr_421_commits.json`:

```json
[
  { "id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "parent_ids": ["1111111111111111111111111111111111111111"], "message": "test: endpoint", "authored_date": "2026-08-20T11:00:00Z" },
  { "id": "1111111111111111111111111111111111111111", "parent_ids": ["0000000000000000000000000000000000000000"], "message": "feat: endpoint", "authored_date": "2026-08-20T10:00:00Z" }
]
```

- [ ] **Step 2: Write failing tests**

`apps/backend/internal/provider/gitlab/client_test.go`:

```go
package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newServer(t *testing.T, rejectMergedAtOrder bool) (*httptest.Server, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-test" {
			t.Errorf("missing PRIVATE-TOKEN on %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v4/") {
			t.Errorf("path lacks /api/v4: %s", r.URL.Path)
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/v4/projects":
			if q.Get("membership") != "true" || q.Get("order_by") != "path" || q.Get("simple") != "" {
				t.Errorf("bad projects query %s", r.URL.RawQuery)
			}
			if q.Get("page") == "1" {
				w.Header().Set("X-Next-Page", "2")
			} else {
				w.Header().Set("X-Next-Page", "")
			}
			_, _ = w.Write(fixture(t, "projects.json"))
		case "/api/v4/projects/atlas%2Fserver", "/api/v4/projects/atlas/server":
			if r.URL.EscapedPath() != "/api/v4/projects/atlas%2Fserver" {
				t.Errorf("project id not url-encoded: %s", r.URL.EscapedPath())
			}
			_, _ = w.Write(fixture(t, "project.json"))
		case "/api/v4/projects/atlas/missing":
			w.WriteHeader(404)
		case "/api/v4/projects/atlas/server/merge_requests":
			if q.Get("state") != "merged" || q.Get("target_branch") != "main" {
				t.Errorf("bad mr query %s", r.URL.RawQuery)
			}
			if rejectMergedAtOrder && q.Get("order_by") == "merged_at" {
				w.WriteHeader(400)
				return
			}
			w.Header().Set("X-Next-Page", "")
			_, _ = w.Write(fixture(t, "mrs_p1.json"))
		case "/api/v4/projects/atlas/server/merge_requests/421":
			_, _ = w.Write(fixture(t, "mr_421.json"))
		case "/api/v4/projects/atlas/server/merge_requests/421/commits":
			w.Header().Set("X-Next-Page", "")
			_, _ = w.Write(fixture(t, "mr_421_commits.json"))
		case "/api/v4/projects/atlas/server/merge_requests/401":
			w.WriteHeader(401)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &queries
}

func newClient(srv *httptest.Server) *Client {
	return New("gitlab-work", "GitLab Work", srv.URL, config.NewSecret("glpat-test"), srv.Client())
}

func TestListRepositoriesAndGet(t *testing.T) {
	srv, _ := newServer(t, false)
	c := newClient(srv)
	s, err := c.ListRepositories(context.Background(), provider.Page{Number: 1, Size: 2})
	if err != nil || len(s.Items) != 2 || !s.HasNext {
		t.Fatalf("%v %+v", err, s)
	}
	if s.Items[1].FullName() != "atlas/apps/web" || s.Items[1].Namespace() != "atlas/apps" || s.Items[1].Name() != "web" || s.Items[1].DefaultBranch() != "develop" {
		t.Errorf("mapping: %+v", s.Items[1])
	}
	s, _ = c.ListRepositories(context.Background(), provider.Page{Number: 2, Size: 2})
	if s.HasNext {
		t.Error("page 2 must be last")
	}
	r, err := c.GetRepository(context.Background(), "atlas/server")
	if err != nil || r.CloneURL() != "https://gitlab.company.com/atlas/server.git" {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := c.GetRepository(context.Background(), "atlas/missing"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestListMergedChangesAndSearch(t *testing.T) {
	srv, queries := newServer(t, false)
	c := newClient(srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	s, err := c.ListMergedChanges(context.Background(), repo, "main", "", provider.Page{})
	if err != nil || len(s.Items) != 2 || s.Items[0].Number() != 435 || s.HasNext {
		t.Fatalf("%v %+v", err, s)
	}
	it := s.Items[1]
	if it.SquashCommitSHA() != strings.Repeat("e", 40) || it.MergeCommitSHA() != "" || it.HeadSHA() != strings.Repeat("a", 40) || !it.Squashed() || it.Author() != "jsmith" || it.State() != provider.StateMerged {
		t.Errorf("mapping: %+v", it)
	}
	if !strings.Contains((*queries)[len(*queries)-1], "order_by=merged_at") {
		t.Errorf("order_by missing: %s", (*queries)[len(*queries)-1])
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "field", provider.Page{})
	if err != nil || len(s.Items) != 2 {
		t.Fatalf("title search: %v %d", err, len(s.Items))
	}
	last := (*queries)[len(*queries)-1]
	if !strings.Contains(last, "search=field") || !strings.Contains(last, "in=title") {
		t.Errorf("server-side search missing: %s", last)
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "jsmith", provider.Page{})
	if err != nil || len(s.Items) != 2 {
		t.Fatalf("author search: %v %d", err, len(s.Items))
	}
	joined := strings.Join(*queries, "\n")
	if !strings.Contains(joined, "author_username=jsmith") {
		t.Errorf("author_username query missing:\n%s", joined)
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "!421", provider.Page{})
	if err != nil || len(s.Items) != 1 || s.Items[0].Number() != 421 {
		t.Fatalf("numeric: %v %+v", err, s)
	}
}

func TestListMergedChangesFallsBackToCreatedAtOrder(t *testing.T) {
	srv, queries := newServer(t, true)
	c := newClient(srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	s, err := c.ListMergedChanges(context.Background(), repo, "main", "", provider.Page{})
	if err != nil || len(s.Items) != 2 || s.Items[0].Number() != 435 {
		t.Fatalf("%v %+v", err, s)
	}
	if !strings.Contains((*queries)[len(*queries)-1], "order_by=created_at") {
		t.Errorf("fallback not used: %s", (*queries)[len(*queries)-1])
	}
}

func TestGetChangeCommitsAndAuth(t *testing.T) {
	srv, _ := newServer(t, false)
	c := newClient(srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	cr, err := c.GetChange(context.Background(), repo, 421)
	if err != nil || cr.Number() != 421 || cr.TargetBranch() != "main" {
		t.Fatalf("%v %+v", err, cr)
	}
	commits, err := c.GetChangeCommits(context.Background(), repo, 421)
	if err != nil || len(commits) != 2 || commits[0].SHA() != strings.Repeat("1", 40) || commits[1].SHA() != strings.Repeat("a", 40) {
		t.Fatalf("commits must be oldest-first: %v %+v", err, commits)
	}
	if _, err := c.GetChange(context.Background(), repo, 401); !errors.Is(err, provider.ErrAuth) || strings.Contains(err.Error(), "glpat") {
		t.Errorf("auth: %v", err)
	}
	spec := gitx.Spec{}
	if err := c.AuthorizeGit(repo, &spec); err != nil || len(spec.Env) != 3 || spec.Env[1] != "GIT_CONFIG_KEY_0=http.https://gitlab.company.com/.extraheader" {
		t.Errorf("authorize: %v %v", err, spec.Env)
	}
}
```

- [ ] **Step 3: Run to see failures**

Run: `cd <worktree-root>/apps/backend && go test ./internal/provider/gitlab/`
Expected: compile failure.

- [ ] **Step 4: Implement the client**

`apps/backend/internal/provider/gitlab/mapping.go`:

```go
package gitlab

import (
	"fmt"
	"sort"
	"time"

	"github.com/jtumidanski/converge/internal/provider"
)

type projectJSON struct {
	Name              string `json:"name"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"path_with_namespace"`
	Namespace         struct{ FullPath string `json:"full_path"` } `json:"namespace"`
	DefaultBranch     string `json:"default_branch"`
	WebURL            string `json:"web_url"`
	HTTPURLToRepo     string `json:"http_url_to_repo"`
}

func (p projectJSON) toModel(providerID string) (provider.Repository, error) {
	return provider.NewRepositoryBuilder().SetProviderID(providerID).SetFullName(p.PathWithNamespace).SetName(p.Path).
		SetNamespace(p.Namespace.FullPath).SetDefaultBranch(p.DefaultBranch).SetWebURL(p.WebURL).SetCloneURL(p.HTTPURLToRepo).Build()
}

type mrJSON struct {
	IID             int        `json:"iid"`
	Title           string     `json:"title"`
	State           string     `json:"state"`
	WebURL          string     `json:"web_url"`
	Author          struct{ Username string `json:"username"` } `json:"author"`
	CreatedAt       time.Time  `json:"created_at"`
	MergedAt        *time.Time `json:"merged_at"`
	SourceBranch    string     `json:"source_branch"`
	TargetBranch    string     `json:"target_branch"`
	SHA             string     `json:"sha"`
	MergeCommitSHA  *string    `json:"merge_commit_sha"`
	SquashCommitSHA *string    `json:"squash_commit_sha"`
	Squash          bool       `json:"squash"`
}

func (m mrJSON) state() provider.ChangeState {
	switch m.State {
	case "merged":
		return provider.StateMerged
	case "opened", "locked":
		return provider.StateOpen
	}
	return provider.StateClosed
}

func (m mrJSON) toModel(providerID string, repo provider.Repository) (provider.ChangeRequest, error) {
	b := provider.NewChangeRequestBuilder().SetProviderID(providerID).SetRepository(repo).SetNumber(m.IID).SetTitle(m.Title).
		SetAuthor(m.Author.Username).SetWebURL(m.WebURL).SetSourceBranch(m.SourceBranch).SetTargetBranch(m.TargetBranch).
		SetCreatedAt(m.CreatedAt).SetState(m.state()).SetHeadSHA(m.SHA).SetSquashed(m.Squash)
	if m.MergedAt != nil {
		b.SetMergedAt(*m.MergedAt)
	}
	if m.MergeCommitSHA != nil && *m.MergeCommitSHA != "" {
		b.SetMergeCommitSHA(*m.MergeCommitSHA)
	}
	if m.SquashCommitSHA != nil && *m.SquashCommitSHA != "" {
		b.SetSquashCommitSHA(*m.SquashCommitSHA)
	}
	cr, err := b.Build()
	if err != nil {
		return provider.ChangeRequest{}, fmt.Errorf("gitlab mr %d: %w", m.IID, err)
	}
	return cr, nil
}

type commitJSON struct {
	ID           string    `json:"id"`
	Message      string    `json:"message"`
	AuthoredDate time.Time `json:"authored_date"`
}

// toCommits maps GitLab's newest-first list into oldest-first order.
func toCommits(raw []commitJSON) ([]provider.Commit, error) {
	out := make([]provider.Commit, 0, len(raw))
	for _, r := range raw {
		c, err := provider.NewCommit(r.ID, r.Message, r.AuthoredDate)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return i > j }) // reverse
	return out, nil
}

// sortMergedDesc orders by merged_at desc then iid desc.
func sortMergedDesc(items []provider.ChangeRequest) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].MergedAt().Equal(items[j].MergedAt()) {
			return items[i].MergedAt().After(items[j].MergedAt())
		}
		return items[i].Number() > items[j].Number()
	})
}
```

Note: `sort.SliceStable` with `i > j` is a reversal trick that is not guaranteed by the sort contract. Implement `toCommits` reversal explicitly instead:

```go
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
```

Use the explicit loop and delete the `sort.SliceStable` reversal line (keep the `sort` import for `sortMergedDesc`).

`apps/backend/internal/provider/gitlab/client.go`:

```go
// Package gitlab implements provider.GitProvider against the GitLab REST API v4.
package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

const providerPage = 100

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Client talks to one GitLab instance.
type Client struct {
	id          string
	displayName string
	baseURL     string
	token       config.Secret
	http        *http.Client
}

// New builds a client; baseURL is the instance root without trailing slash.
func New(id, displayName, baseURL string, token config.Secret, client *http.Client) *Client {
	return &Client{id: id, displayName: displayName, baseURL: strings.TrimRight(baseURL, "/"), token: token, http: client}
}

func (c *Client) ID() string           { return c.id }
func (c *Client) Kind() provider.Kind  { return provider.KindGitLab }
func (c *Client) DisplayName() string  { return c.displayName }
func (c *Client) BaseURL() string      { return c.baseURL }

func projectPath(fullName string) string { return "/projects/" + url.PathEscape(fullName) }

// get performs a GET under /api/v4 and returns the X-Next-Page value.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (string, error) {
	u := c.baseURL + "/api/v4" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("gitlab: build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", c.token.Reveal())
	h, err := provider.DoJSON(ctx, c.http, req, out)
	if err != nil {
		return "", err
	}
	return h.Get("X-Next-Page"), nil
}

func (c *Client) ListRepositories(ctx context.Context, page provider.Page) (provider.Slice[provider.Repository], error) {
	page = page.Normalize()
	q := url.Values{"membership": {"true"}, "order_by": {"path"}, "sort": {"asc"}, "per_page": {strconv.Itoa(page.Size)}, "page": {strconv.Itoa(page.Number)}}
	var raw []projectJSON
	next, err := c.get(ctx, "/projects", q, &raw)
	if err != nil {
		return provider.Slice[provider.Repository]{}, err
	}
	items := make([]provider.Repository, 0, len(raw))
	for _, p := range raw {
		r, err := p.toModel(c.id)
		if err != nil {
			return provider.Slice[provider.Repository]{}, err
		}
		items = append(items, r)
	}
	return provider.Slice[provider.Repository]{Items: items, HasNext: next != ""}, nil
}

func (c *Client) GetRepository(ctx context.Context, fullName string) (provider.Repository, error) {
	if err := gitx.ValidateRepoFullName(fullName); err != nil {
		return provider.Repository{}, err
	}
	var raw projectJSON
	if _, err := c.get(ctx, projectPath(fullName), nil, &raw); err != nil {
		return provider.Repository{}, err
	}
	return raw.toModel(c.id)
}

func (c *Client) listMRs(ctx context.Context, repo provider.Repository, q url.Values) ([]provider.ChangeRequest, string, error) {
	var raw []mrJSON
	next, err := c.get(ctx, projectPath(repo.FullName())+"/merge_requests", q, &raw)
	if err != nil {
		return nil, "", err
	}
	items := make([]provider.ChangeRequest, 0, len(raw))
	for _, m := range raw {
		cr, err := m.toModel(c.id, repo)
		if err != nil {
			return nil, "", err
		}
		items = append(items, cr)
	}
	return items, next, nil
}

func (c *Client) ListMergedChanges(ctx context.Context, repo provider.Repository, target, search string, page provider.Page) (provider.Slice[provider.ChangeRequest], error) {
	page = page.Normalize()
	if err := gitx.ValidateBranchSyntax(target); err != nil {
		return provider.Slice[provider.ChangeRequest]{}, err
	}
	empty := provider.Slice[provider.ChangeRequest]{Items: []provider.ChangeRequest{}}
	if n, ok := provider.ParseSearchNumber(search); ok {
		cr, err := c.GetChange(ctx, repo, n)
		if errors.Is(err, provider.ErrNotFound) {
			return empty, nil
		}
		if err != nil {
			return empty, err
		}
		if cr.State() != provider.StateMerged || cr.TargetBranch() != target {
			return empty, nil
		}
		return provider.Slice[provider.ChangeRequest]{Items: []provider.ChangeRequest{cr}}, nil
	}
	base := url.Values{"state": {"merged"}, "target_branch": {target}, "sort": {"desc"}, "per_page": {strconv.Itoa(page.Size)}, "page": {strconv.Itoa(page.Number)}}
	search = strings.TrimSpace(search)
	if search == "" {
		items, next, err := c.listOrdered(ctx, repo, base)
		if err != nil {
			return empty, err
		}
		return provider.Slice[provider.ChangeRequest]{Items: items, HasNext: next != ""}, nil
	}
	titleQ := cloneValues(base)
	titleQ.Set("search", search)
	titleQ.Set("in", "title")
	items, next, err := c.listOrdered(ctx, repo, titleQ)
	if err != nil {
		return empty, err
	}
	hasNext := next != ""
	if usernameRe.MatchString(search) {
		authorQ := cloneValues(base)
		authorQ.Set("author_username", search)
		byAuthor, next2, err := c.listOrdered(ctx, repo, authorQ)
		if err != nil {
			return empty, err
		}
		items = mergeUnique(items, byAuthor)
		hasNext = hasNext || next2 != ""
	}
	sortMergedDesc(items)
	return provider.Slice[provider.ChangeRequest]{Items: items, HasNext: hasNext}, nil
}

// listOrdered asks for order_by=merged_at (GitLab ≥ 17.2) and falls back to created_at on 400.
func (c *Client) listOrdered(ctx context.Context, repo provider.Repository, q url.Values) ([]provider.ChangeRequest, string, error) {
	q = cloneValues(q)
	q.Set("order_by", "merged_at")
	items, next, err := c.listMRs(ctx, repo, q)
	var se *provider.StatusError
	if err != nil && errors.As(err, &se) && se.Status == http.StatusBadRequest {
		q.Set("order_by", "created_at")
		items, next, err = c.listMRs(ctx, repo, q)
		if err == nil {
			sortMergedDesc(items)
		}
	}
	return items, next, err
}

func (c *Client) GetChange(ctx context.Context, repo provider.Repository, number int) (provider.ChangeRequest, error) {
	if err := gitx.ValidateChangeNumber(number); err != nil {
		return provider.ChangeRequest{}, err
	}
	var raw mrJSON
	if _, err := c.get(ctx, fmt.Sprintf("%s/merge_requests/%d", projectPath(repo.FullName()), number), nil, &raw); err != nil {
		return provider.ChangeRequest{}, err
	}
	return raw.toModel(c.id, repo)
}

func (c *Client) GetChangeCommits(ctx context.Context, repo provider.Repository, number int) ([]provider.Commit, error) {
	if err := gitx.ValidateChangeNumber(number); err != nil {
		return nil, err
	}
	var all []commitJSON
	for page := 1; ; page++ {
		var raw []commitJSON
		q := url.Values{"per_page": {strconv.Itoa(providerPage)}, "page": {strconv.Itoa(page)}}
		next, err := c.get(ctx, fmt.Sprintf("%s/merge_requests/%d/commits", projectPath(repo.FullName()), number), q, &raw)
		if err != nil {
			return nil, err
		}
		all = append(all, raw...)
		if next == "" || len(raw) == 0 {
			break
		}
	}
	return toCommits(all)
}

func (c *Client) CloneURL(repo provider.Repository) string { return repo.CloneURL() }

func (c *Client) AuthorizeGit(repo provider.Repository, spec *gitx.Spec) error {
	env, err := gitx.CredentialEnv(repo.CloneURL(), provider.GitUser(provider.KindGitLab), c.token.Reveal())
	if err != nil {
		return err
	}
	spec.Env = append(spec.Env, env...)
	return nil
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func mergeUnique(a, b []provider.ChangeRequest) []provider.ChangeRequest {
	seen := map[int]struct{}{}
	out := make([]provider.ChangeRequest, 0, len(a)+len(b))
	for _, list := range [][]provider.ChangeRequest{a, b} {
		for _, cr := range list {
			if _, dup := seen[cr.Number()]; dup {
				continue
			}
			seen[cr.Number()] = struct{}{}
			out = append(out, cr)
		}
	}
	return out
}

var _ provider.GitProvider = (*Client)(nil)
```

- [ ] **Step 5: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/provider/... && go tool golangci-lint run ./internal/provider/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/provider/gitlab && git commit -m "feat(task-001): GitLab provider with fixture tests"
```

---

## Phase C — Git layers

### Task 7: Scripted-repository test harness

**Files:**
- Create: `apps/backend/internal/testutil/repo.go`, `apps/backend/internal/testutil/repo_test.go`

**Interfaces:**
- Produces (package `testutil`, real git, no build tag so unit tests in later tasks can use it):
  - `type Repo struct{ T *testing.T; Work string; Bare string }`.
  - `func NewRepo(t *testing.T) *Repo` — bare repo at `<tmp>/origin.git`, working clone at `<tmp>/work` with one initial commit on `main` (`README.md`) pushed to the bare repo.
  - `func (r *Repo) Git(args ...string) string` — runs git in `Work`, fatal on failure, returns trimmed stdout.
  - `func (r *Repo) Commit(file, content, message string) string` — writes file, `git add`, commits, returns SHA.
  - `func (r *Repo) Branch(name string)` — create + checkout from current HEAD; `func (r *Repo) Checkout(name string)`.
  - `func (r *Repo) MergeNoFF(branch, message string) string` — on `main`; returns merge SHA.
  - `func (r *Repo) Squash(branch, message string) string` — on `main`; returns squash SHA.
  - `func (r *Repo) Rebase(branch string) []string` — rebases `branch` onto `main`, fast-forwards `main`, returns the rewritten SHAs oldest-first.
  - `func (r *Repo) BranchCommits(branch string) []string` — commits on `branch` not on `main`, oldest-first (call before merging).
  - `func (r *Repo) Push()` — `git push --all --force origin`.
  - `func (r *Repo) RevParse(rev string) string`; `func (r *Repo) Head() string`; `func (r *Repo) CloneURL() string` (`file://<Bare>`); `func (r *Repo) FileContent(rev, path string) string` (`git show rev:path`, empty when absent).

- [ ] **Step 1: Write a failing self-test**

`apps/backend/internal/testutil/repo_test.go`:

```go
package testutil

import (
	"strings"
	"testing"
)

func TestHarnessStrategies(t *testing.T) {
	r := NewRepo(t)
	base := r.Head()

	r.Branch("feat/a")
	a1 := r.Commit("a.txt", "a1\n", "a1")
	a2 := r.Commit("a.txt", "a1\na2\n", "a2")
	r.Checkout("main")
	if got := r.BranchCommits("feat/a"); len(got) != 2 || got[0] != a1 || got[1] != a2 {
		t.Fatalf("BranchCommits = %v", got)
	}
	merge := r.MergeNoFF("feat/a", "Merge feat/a")
	if parents := strings.Fields(r.Git("rev-list", "--parents", "-n", "1", merge)); len(parents) != 3 || parents[1] != base {
		t.Fatalf("merge parents = %v", parents)
	}

	r.Branch("feat/b")
	r.Commit("b.txt", "b\n", "b1")
	r.Commit("b.txt", "bb\n", "b2")
	r.Checkout("main")
	squash := r.Squash("feat/b", "Squash feat/b")
	if parents := strings.Fields(r.Git("rev-list", "--parents", "-n", "1", squash)); len(parents) != 2 || parents[1] != merge {
		t.Fatalf("squash parents = %v", parents)
	}
	if r.FileContent("main", "b.txt") != "bb\n" {
		t.Fatal("squash content")
	}

	r.Branch("feat/c")
	c1 := r.Commit("c.txt", "c\n", "c1")
	r.Checkout("main")
	r.Commit("other.txt", "x\n", "unrelated on main")
	rebased := r.Rebase("feat/c")
	if len(rebased) != 1 || rebased[0] == c1 || r.Head() != rebased[0] {
		t.Fatalf("rebase = %v head=%s c1=%s", rebased, r.Head(), c1)
	}
	r.Push()
	if !strings.HasPrefix(r.CloneURL(), "file://") {
		t.Fatal("clone url")
	}
}
```

- [ ] **Step 2: Run to see failure**

Run: `cd <worktree-root>/apps/backend && go test ./internal/testutil/`
Expected: compile failure.

- [ ] **Step 3: Implement the harness**

`apps/backend/internal/testutil/repo.go`:

```go
// Package testutil scripts local git repositories for tests. It uses the real
// git binary directly (not gitx) so harness bugs cannot mask runner bugs.
package testutil

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a bare "origin" plus a working clone.
type Repo struct {
	T    *testing.T
	Work string
	Bare string
	home string
}

// NewRepo creates the pair with one initial commit on main.
func NewRepo(t *testing.T) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	r := &Repo{T: t, Work: filepath.Join(root, "work"), Bare: filepath.Join(root, "origin.git"), home: filepath.Join(root, "home")}
	if err := os.MkdirAll(r.home, 0o700); err != nil {
		t.Fatal(err)
	}
	r.run("", "init", "--bare", "--initial-branch=main", r.Bare)
	r.run("", "clone", r.Bare, r.Work)
	r.Git("checkout", "-b", "main")
	r.Commit("README.md", "# test\n", "initial")
	r.Push()
	return r
}

func (r *Repo) env() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=author@example.com",
		"GIT_COMMITTER_NAME=Test Committer", "GIT_COMMITTER_EMAIL=committer@example.com",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
		"LC_ALL=C",
	}
}

func (r *Repo) run(dir string, args ...string) string {
	r.T.Helper()
	full := append([]string{"-c", "protocol.file.allow=always", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = r.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		r.T.Fatalf("git %v: %v\n%s", args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

// Git runs git in the working clone.
func (r *Repo) Git(args ...string) string { r.T.Helper(); return r.run(r.Work, args...) }

// Commit writes file with content and commits it.
func (r *Repo) Commit(file, content, message string) string {
	r.T.Helper()
	p := filepath.Join(r.Work, file)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.T.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.T.Fatal(err)
	}
	r.Git("add", "--", file)
	r.Git("commit", "--allow-empty", "-m", message)
	return r.Head()
}

// Branch creates and checks out a branch from HEAD.
func (r *Repo) Branch(name string) { r.T.Helper(); r.Git("checkout", "-b", name) }

// Checkout switches branches.
func (r *Repo) Checkout(name string) { r.T.Helper(); r.Git("checkout", name) }

// MergeNoFF merges branch into the current branch with a merge commit.
func (r *Repo) MergeNoFF(branch, message string) string {
	r.T.Helper()
	r.Git("merge", "--no-ff", "-m", message, branch)
	return r.Head()
}

// Squash squash-merges branch into the current branch as one commit.
func (r *Repo) Squash(branch, message string) string {
	r.T.Helper()
	r.Git("merge", "--squash", branch)
	r.Git("commit", "-m", message)
	return r.Head()
}

// BranchCommits lists commits on branch not on main, oldest-first.
func (r *Repo) BranchCommits(branch string) []string {
	r.T.Helper()
	out := r.Git("rev-list", "--reverse", "main.."+branch)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// Rebase rebases branch onto main and fast-forwards main. Returns new SHAs oldest-first.
func (r *Repo) Rebase(branch string) []string {
	r.T.Helper()
	r.Git("checkout", branch)
	r.Git("rebase", "main")
	shas := r.BranchCommits(branch)
	r.Git("checkout", "main")
	r.Git("merge", "--ff-only", branch)
	return shas
}

// Push pushes every branch to the bare origin.
func (r *Repo) Push() { r.T.Helper(); r.Git("push", "--all", "--force", "origin") }

// RevParse resolves a revision.
func (r *Repo) RevParse(rev string) string { r.T.Helper(); return r.Git("rev-parse", "--verify", rev+"^{commit}") }

// Head returns HEAD's SHA.
func (r *Repo) Head() string { r.T.Helper(); return r.RevParse("HEAD") }

// CloneURL returns a file:// URL for the bare repository.
func (r *Repo) CloneURL() string { return "file://" + r.Bare }

// FileContent returns rev:path or "" when absent.
func (r *Repo) FileContent(rev, path string) string {
	cmd := exec.Command("git", "show", rev+":"+path)
	cmd.Dir = r.Work
	cmd.Env = r.env()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}
```

- [ ] **Step 4: Run and commit**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/testutil/ && go tool golangci-lint run ./internal/testutil/...`
Expected: PASS.

```bash
cd <worktree-root> && git add apps/backend/internal/testutil && git commit -m "test(task-001): scripted git repository harness"
```

---

### Task 8: Mirror cache and object reader

**Files:**
- Create: `apps/backend/internal/mirror/cache.go`, `objects.go`, `cache_test.go`, `objects_test.go`

**Interfaces:**
- Consumes: `gitx.Runner`, `gitx.LockMap`, `gitx.Spec`, `provider.GitProvider`, `provider.Repository`, `testutil.Repo`, `fake.Provider`.
- Produces:
  - `func New(root string, runner gitx.Runner, locks *gitx.LockMap, log *slog.Logger) *Cache`.
  - `(*Cache) Path(providerID, fullName string) (string, error)` → `<root>/<providerID>/<fullName>.git`.
  - `(*Cache) Ensure(ctx, p provider.GitProvider, repo provider.Repository) (mirrorPath string, err error)`.
  - `(*Cache) FetchSHA(ctx, p provider.GitProvider, repo provider.Repository, sha string) error`.
  - `(*Cache) Lock(mirrorPath string) func()`.
  - `type ObjectReader interface { Exists(ctx, sha string) (bool, error); Parents(ctx, sha string) ([]string, error); PatchID(ctx, sha string) (string, error); FirstParentWalk(ctx, sha string, n int) ([]string, error); IsAncestor(ctx, sha, branch string) (bool, error); RevParse(ctx, rev string) (string, error); BranchExists(ctx, branch string) (bool, error) }`.
  - `(*Cache) Objects(mirrorPath, repoName string) ObjectReader`.

- [ ] **Step 1: Write failing cache tests (FakeRunner for command shape, real git for behaviour)**

`apps/backend/internal/mirror/cache_test.go`:

```go
package mirror

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/testutil"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})) }

func repoFor(t *testing.T, providerID, cloneURL string) provider.Repository {
	t.Helper()
	r, err := provider.NewRepositoryBuilder().SetProviderID(providerID).SetFullName("atlas/server").SetDefaultBranch("main").SetCloneURL(cloneURL).Build()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPathLayoutAndValidation(t *testing.T) {
	c := New("/cache", &gitx.FakeRunner{}, &gitx.LockMap{}, testLogger())
	p, err := c.Path("gitlab-work", "atlas/sub/server")
	if err != nil || p != filepath.Join("/cache", "gitlab-work", "atlas", "sub", "server.git") {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := c.Path("gh", "../escape"); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := c.Path("../gh", "a/b"); err == nil {
		t.Fatal("bad provider id accepted")
	}
}

func TestEnsureClonesThenUpdates(t *testing.T) {
	fr := &gitx.FakeRunner{}
	root := t.TempDir()
	c := New(root, fr, &gitx.LockMap{}, testLogger())
	p := fake.New("gh", provider.KindGitHub)
	repo := repoFor(t, "gh", "https://github.com/atlas/server.git")
	path, err := c.Ensure(context.Background(), p, repo)
	if err != nil {
		t.Fatal(err)
	}
	calls := fr.Snapshot()
	if len(calls) != 1 || calls[0].Category != gitx.CategoryClone || strings.Join(calls[0].Args, " ") != "clone --mirror https://github.com/atlas/server.git "+path {
		t.Fatalf("clone call = %+v", calls)
	}
	// simulate the clone having created HEAD
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fr.Reset()
	if _, err := c.Ensure(context.Background(), p, repo); err != nil {
		t.Fatal(err)
	}
	calls = fr.Snapshot()
	if len(calls) != 1 || calls[0].Category != gitx.CategoryFetch || calls[0].Dir != path || strings.Join(calls[0].Args, " ") != "remote update --prune" {
		t.Fatalf("update call = %+v", calls)
	}
	fr.Reset()
	sha := strings.Repeat("a", 40)
	if err := c.FetchSHA(context.Background(), p, repo, sha); err != nil {
		t.Fatal(err)
	}
	calls = fr.Snapshot()
	if len(calls) != 1 || strings.Join(calls[0].Args, " ") != "fetch origin "+sha || calls[0].Dir != path {
		t.Fatalf("fetch call = %+v", calls)
	}
	if err := c.FetchSHA(context.Background(), p, repo, "nothex"); err == nil {
		t.Fatal("bad sha accepted")
	}
}

func TestEnsureRealGitNoCredentialInRemote(t *testing.T) {
	src := testutil.NewRepo(t)
	runner, err := gitx.NewExecRunner(testLogger(), gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	c := New(t.TempDir(), runner, &gitx.LockMap{}, testLogger())
	p := fake.New("fake", provider.KindGitLab)
	repo := repoFor(t, "fake", src.CloneURL())
	path, err := c.Ensure(context.Background(), p, repo)
	if err != nil {
		t.Fatal(err)
	}
	res, err := runner.Run(context.Background(), gitx.Spec{Dir: path, Args: []string{"remote", "get-url", "origin"}, Category: gitx.CategoryQuery})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != src.CloneURL() {
		t.Fatalf("remote url = %q err=%v", res.Stdout, err)
	}
	// second call updates and picks up a new commit
	src.Commit("new.txt", "n\n", "new")
	src.Push()
	if _, err := c.Ensure(context.Background(), p, repo); err != nil {
		t.Fatal(err)
	}
	ok, err := c.Objects(path, "atlas/server").Exists(context.Background(), src.Head())
	if err != nil || !ok {
		t.Fatalf("new commit not fetched: %v %v", ok, err)
	}
}
```

`apps/backend/internal/mirror/objects_test.go`:

```go
package mirror

import (
	"context"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
)

func TestObjectReader(t *testing.T) {
	src := testutilRepo(t)
	base := src.Head()
	src.Branch("feat")
	c1 := src.Commit("f.txt", "1\n", "c1")
	c2 := src.Commit("f.txt", "1\n2\n", "c2")
	src.Checkout("main")
	merge := src.MergeNoFF("feat", "merge feat")
	src.Push()

	runner, err := gitx.NewExecRunner(testLogger(), gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	cache := New(t.TempDir(), runner, &gitx.LockMap{}, testLogger())
	path, err := cache.Ensure(context.Background(), fake.New("fake", provider.KindGitLab), repoFor(t, "fake", src.CloneURL()))
	if err != nil {
		t.Fatal(err)
	}
	o := cache.Objects(path, "atlas/server")
	ctx := context.Background()

	if ok, _ := o.Exists(ctx, merge); !ok {
		t.Error("merge should exist")
	}
	if ok, err := o.Exists(ctx, "0000000000000000000000000000000000000000"); ok || err != nil {
		t.Errorf("zero sha: %v %v", ok, err)
	}
	if ps, _ := o.Parents(ctx, merge); len(ps) != 2 || ps[0] != base || ps[1] != c2 {
		t.Errorf("parents = %v", ps)
	}
	if ps, _ := o.Parents(ctx, c1); len(ps) != 1 || ps[0] != base {
		t.Errorf("c1 parents = %v", ps)
	}
	id1, _ := o.PatchID(ctx, c1)
	id2, _ := o.PatchID(ctx, c2)
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Errorf("patch ids %q %q", id1, id2)
	}
	if w, _ := o.FirstParentWalk(ctx, c2, 2); len(w) != 2 || w[0] != c1 || w[1] != c2 {
		t.Errorf("walk = %v", w)
	}
	if ok, _ := o.IsAncestor(ctx, c1, "main"); !ok {
		t.Error("c1 should be ancestor of main")
	}
	if ok, _ := o.IsAncestor(ctx, merge, "feat"); ok {
		t.Error("merge is not ancestor of feat")
	}
	if got, _ := o.RevParse(ctx, merge+"^1"); got != base {
		t.Errorf("rev-parse ^1 = %s", got)
	}
	if ok, _ := o.BranchExists(ctx, "main"); !ok {
		t.Error("main missing")
	}
	if ok, _ := o.BranchExists(ctx, "nope"); ok {
		t.Error("nope exists")
	}
	if _, err := o.Parents(ctx, "bad"); err == nil {
		t.Error("invalid sha accepted")
	}
}
```

Add to `cache_test.go` a helper `func testutilRepo(t *testing.T) *testutil.Repo { return testutil.NewRepo(t) }`.

- [ ] **Step 2: Run to see failure**

Run: `cd <worktree-root>/apps/backend && go test ./internal/mirror/`
Expected: compile failure.

- [ ] **Step 3: Implement cache and object reader**

`apps/backend/internal/mirror/cache.go`:

```go
// Package mirror manages bare mirror clones under REPOSITORY_CACHE_ROOT.
package mirror

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

var providerIDRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// Cache owns the mirror directory tree.
type Cache struct {
	root   string
	runner gitx.Runner
	locks  *gitx.LockMap
	log    *slog.Logger
}

// New creates a cache rooted at root.
func New(root string, runner gitx.Runner, locks *gitx.LockMap, log *slog.Logger) *Cache {
	return &Cache{root: root, runner: runner, locks: locks, log: log}
}

// Path returns <root>/<providerID>/<fullName>.git after validation.
func (c *Cache) Path(providerID, fullName string) (string, error) {
	if !providerIDRe.MatchString(providerID) {
		return "", fmt.Errorf("mirror: invalid provider id %q", providerID)
	}
	if err := gitx.ValidateRepoFullName(fullName); err != nil {
		return "", fmt.Errorf("mirror: %w", err)
	}
	parts := append([]string{c.root, providerID}, strings.Split(fullName, "/")...)
	parts[len(parts)-1] += ".git"
	return filepath.Join(parts...), nil
}

// Lock takes the per-mirror mutex.
func (c *Cache) Lock(mirrorPath string) func() { return c.locks.Lock(mirrorPath) }

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Ensure clones the mirror on first use, otherwise updates it. Holds the mirror lock.
func (c *Cache) Ensure(ctx context.Context, p provider.GitProvider, repo provider.Repository) (string, error) {
	path, err := c.Path(p.ID(), repo.FullName())
	if err != nil {
		return "", err
	}
	unlock := c.Lock(path)
	defer unlock()
	var spec gitx.Spec
	if exists(filepath.Join(path, "HEAD")) {
		spec = gitx.Spec{Dir: path, Args: []string{"remote", "update", "--prune"}, Category: gitx.CategoryFetch, Repo: repo.FullName()}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return "", fmt.Errorf("mirror: create parent: %w", err)
		}
		spec = gitx.Spec{Args: []string{"clone", "--mirror", p.CloneURL(repo), path}, Category: gitx.CategoryClone, Repo: repo.FullName()}
	}
	if err := p.AuthorizeGit(repo, &spec); err != nil {
		return "", fmt.Errorf("mirror: authorize: %w", err)
	}
	if _, err := c.runner.Run(ctx, spec); err != nil {
		if spec.Category == gitx.CategoryClone {
			_ = os.RemoveAll(path) // never leave a half clone behind
		}
		return "", fmt.Errorf("mirror: %s: %w", spec.Category, err)
	}
	c.log.Info("mirror ready", slog.String("repository", repo.FullName()), slog.String("git.category", string(spec.Category)))
	return path, nil
}

// FetchSHA tries once to fetch a specific commit. Holds the mirror lock.
func (c *Cache) FetchSHA(ctx context.Context, p provider.GitProvider, repo provider.Repository, sha string) error {
	if err := gitx.ValidateSHA(sha); err != nil {
		return err
	}
	path, err := c.Path(p.ID(), repo.FullName())
	if err != nil {
		return err
	}
	unlock := c.Lock(path)
	defer unlock()
	spec := gitx.Spec{Dir: path, Args: []string{"fetch", "origin", sha}, Category: gitx.CategoryFetch, Repo: repo.FullName()}
	if err := p.AuthorizeGit(repo, &spec); err != nil {
		return err
	}
	if _, err := c.runner.Run(ctx, spec); err != nil {
		return fmt.Errorf("mirror: fetch %s: %w", sha[:7], err)
	}
	return nil
}
```

`apps/backend/internal/mirror/objects.go`:

```go
package mirror

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jtumidanski/converge/internal/gitx"
)

// ObjectReader answers read-only questions about commits in a mirror.
type ObjectReader interface {
	Exists(ctx context.Context, sha string) (bool, error)
	Parents(ctx context.Context, sha string) ([]string, error)
	PatchID(ctx context.Context, sha string) (string, error)
	FirstParentWalk(ctx context.Context, sha string, n int) ([]string, error)
	IsAncestor(ctx context.Context, sha, branch string) (bool, error)
	RevParse(ctx context.Context, rev string) (string, error)
	BranchExists(ctx context.Context, branch string) (bool, error)
}

type objects struct {
	dir    string
	repo   string
	runner gitx.Runner
}

// Objects returns an ObjectReader for a mirror path (no lock needed for reads).
func (c *Cache) Objects(mirrorPath, repoName string) ObjectReader {
	return &objects{dir: mirrorPath, repo: repoName, runner: c.runner}
}

func (o *objects) run(ctx context.Context, stdin []byte, args ...string) (string, error) {
	spec := gitx.Spec{Dir: o.dir, Args: args, Category: gitx.CategoryQuery, Repo: o.repo}
	if stdin != nil {
		spec.Stdin = bytes.NewReader(stdin)
	}
	res, err := o.runner.Run(ctx, spec)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func (o *objects) Exists(ctx context.Context, sha string) (bool, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return false, err
	}
	_, err := o.run(ctx, nil, "cat-file", "-e", sha+"^{commit}")
	if err == nil {
		return true, nil
	}
	if gitx.IsExit(err, 1) || gitx.IsExit(err, 128) {
		return false, nil
	}
	return false, err
}

func (o *objects) Parents(ctx context.Context, sha string) ([]string, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return nil, err
	}
	out, err := o.run(ctx, nil, "rev-list", "--parents", "-n", "1", sha)
	if err != nil {
		return nil, fmt.Errorf("parents of %s: %w", sha[:7], err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return nil, fmt.Errorf("parents of %s: empty output", sha[:7])
	}
	return fields[1:], nil
}

func (o *objects) PatchID(ctx context.Context, sha string) (string, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return "", err
	}
	res, err := o.runner.Run(ctx, gitx.Spec{Dir: o.dir, Args: []string{"show", "--format=", "--no-color", "-p", sha}, Category: gitx.CategoryQuery, Repo: o.repo})
	if err != nil {
		return "", fmt.Errorf("show %s: %w", sha[:7], err)
	}
	out, err := o.run(ctx, res.Stdout, "patch-id", "--stable")
	if err != nil {
		return "", fmt.Errorf("patch-id %s: %w", sha[:7], err)
	}
	if out == "" {
		return "", nil // empty commit
	}
	return strings.Fields(out)[0], nil
}

func (o *objects) FirstParentWalk(ctx context.Context, sha string, n int) ([]string, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return nil, err
	}
	if n <= 0 {
		return nil, fmt.Errorf("walk: n must be positive")
	}
	out, err := o.run(ctx, nil, "rev-list", "--first-parent", "--max-count="+strconv.Itoa(n), sha)
	if err != nil {
		return nil, fmt.Errorf("first-parent walk from %s: %w", sha[:7], err)
	}
	newestFirst := strings.Fields(out)
	oldestFirst := make([]string, len(newestFirst))
	for i, s := range newestFirst {
		oldestFirst[len(newestFirst)-1-i] = s
	}
	return oldestFirst, nil
}

func (o *objects) IsAncestor(ctx context.Context, sha, branch string) (bool, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return false, err
	}
	if err := gitx.ValidateBranchSyntax(branch); err != nil {
		return false, err
	}
	_, err := o.run(ctx, nil, "merge-base", "--is-ancestor", sha, "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	if gitx.IsExit(err, 1) {
		return false, nil
	}
	return false, err
}

func (o *objects) RevParse(ctx context.Context, rev string) (string, error) {
	if err := gitx.ValidatePathArg(rev); err != nil {
		return "", err
	}
	out, err := o.run(ctx, nil, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("rev-parse %s: %w", rev, err)
	}
	return out, nil
}

func (o *objects) BranchExists(ctx context.Context, branch string) (bool, error) {
	if err := gitx.ValidateBranchSyntax(branch); err != nil {
		return false, err
	}
	_, err := o.run(ctx, nil, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	if gitx.IsExit(err, 1) {
		return false, nil
	}
	return false, err
}
```

- [ ] **Step 4: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/mirror/ && go tool golangci-lint run ./internal/mirror/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/mirror && git commit -m "feat(task-001): mirror cache with per-repo locks and object reader"
```

---

### Task 9: Workspace manager (worktree create/cleanup with path guards)

**Files:**
- Create: `apps/backend/internal/workspace/manager.go`, `manager_test.go`

**Interfaces:**
- Produces:
  - `func New(root string, runner gitx.Runner, locks *gitx.LockMap, log *slog.Logger) (*Manager, error)` — creates root, resolves symlinks.
  - `(*Manager) Root() string`, `SessionDir(id) string`, `RepoDir(id) string`, `BranchName(id) string` (`review/<id>`).
  - `(*Manager) Create(ctx, mirrorPath, sessionID, baseSHA string) (repoDir string, err error)`.
  - `(*Manager) Cleanup(ctx, mirrorPath, sessionID string) error` — FR-9.1 order, idempotent, guarded.
  - `(*Manager) RemoveDir(sessionID string) error` — guarded `os.RemoveAll` only.
  - `var ErrOutsideRoot = errors.New("workspace: path is not a direct child of WORKSPACE_ROOT")`.
  - `func ValidateSessionID(id string) error` (`^[0-9a-f]{8}$`).

- [ ] **Step 1: Write failing tests**

`apps/backend/internal/workspace/manager_test.go`:

```go
package workspace

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/testutil"
)

func logger() *slog.Logger { return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})) }

func TestCreateCleanupRealGit(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()
	runner, err := gitx.NewExecRunner(logger(), gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	locks := &gitx.LockMap{}
	cache := mirror.New(t.TempDir(), runner, locks, logger())
	repo, _ := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("a/b").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	mirrorPath, err := cache.Ensure(context.Background(), fake.New("fake", provider.KindGitLab), repo)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(t.TempDir(), runner, locks, logger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repoDir, err := m.Create(ctx, mirrorPath, "0123abcd", base)
	if err != nil {
		t.Fatal(err)
	}
	if repoDir != filepath.Join(m.Root(), "0123abcd", "repo") {
		t.Fatalf("repoDir = %s", repoDir)
	}
	res, _ := runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"rev-parse", "--abbrev-ref", "HEAD"}, Category: gitx.CategoryQuery})
	if strings.TrimSpace(string(res.Stdout)) != "review/0123abcd" {
		t.Fatalf("branch = %s", res.Stdout)
	}
	if _, err := m.Create(ctx, mirrorPath, "BAD", base); err == nil {
		t.Fatal("bad id accepted")
	}
	if err := m.Cleanup(ctx, mirrorPath, "0123abcd"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.SessionDir("0123abcd")); !os.IsNotExist(err) {
		t.Fatal("session dir still present")
	}
	res, err = runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"branch", "--list", "review/*"}, Category: gitx.CategoryQuery})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "" {
		t.Fatalf("branch not deleted: %q %v", res.Stdout, err)
	}
	res, err = runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"worktree", "list", "--porcelain"}, Category: gitx.CategoryQuery})
	if err != nil || strings.Count(string(res.Stdout), "worktree ") != 1 {
		t.Fatalf("worktree not removed: %q", res.Stdout)
	}
	if _, err := runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"fsck", "--no-progress"}, Category: gitx.CategoryQuery}); err != nil {
		t.Fatalf("mirror damaged: %v", err)
	}
	// idempotent
	if err := m.Cleanup(ctx, mirrorPath, "0123abcd"); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
	if err := m.Cleanup(ctx, "", "0123abcd"); err != nil {
		t.Fatalf("cleanup without mirror: %v", err)
	}
}

func TestCleanupRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	m, err := New(root, &gitx.FakeRunner{}, &gitx.LockMap{}, logger())
	if err != nil {
		t.Fatal(err)
	}
	// symlinked session dir pointing outside the root
	if err := os.Symlink(outside, filepath.Join(m.Root(), "deadbeef")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = m.Cleanup(context.Background(), "", "deadbeef")
	if !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatal("outside file was deleted")
	}
	if err := m.RemoveDir("deadbeef"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("RemoveDir err = %v", err)
	}
	for _, bad := range []string{"..", "../x", "abc", "ABCDEF01", "0123456789"} {
		if err := m.RemoveDir(bad); err == nil {
			t.Errorf("RemoveDir(%q) accepted", bad)
		}
	}
	// a real directory is removed
	if err := os.MkdirAll(filepath.Join(m.Root(), "01234567", "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveDir("01234567"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Root(), "01234567")); !os.IsNotExist(err) {
		t.Fatal("dir remains")
	}
}
```

- [ ] **Step 2: Run to see failure**

Run: `cd <worktree-root>/apps/backend && go test ./internal/workspace/`
Expected: compile failure.

- [ ] **Step 3: Implement the manager**

`apps/backend/internal/workspace/manager.go`:

```go
// Package workspace creates and destroys per-session git worktrees.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jtumidanski/converge/internal/gitx"
)

// ErrOutsideRoot is returned when a session path does not resolve inside the root.
var ErrOutsideRoot = errors.New("workspace: path is not a direct child of WORKSPACE_ROOT")

var sessionIDRe = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ValidateSessionID accepts 8 lowercase hex characters.
func ValidateSessionID(id string) error {
	if !sessionIDRe.MatchString(id) {
		return fmt.Errorf("%w: invalid session id", gitx.ErrInvalid)
	}
	return nil
}

// Manager owns WORKSPACE_ROOT.
type Manager struct {
	root   string
	runner gitx.Runner
	locks  *gitx.LockMap
	log    *slog.Logger
}

// New creates the root if needed and resolves it through symlinks.
func New(root string, runner gitx.Runner, locks *gitx.LockMap, log *slog.Logger) (*Manager, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("workspace: create root: %w", err)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve root: %w", err)
	}
	return &Manager{root: real, runner: runner, locks: locks, log: log}, nil
}

func (m *Manager) Root() string                 { return m.root }
func (m *Manager) SessionDir(id string) string  { return filepath.Join(m.root, id) }
func (m *Manager) RepoDir(id string) string     { return filepath.Join(m.root, id, "repo") }
func (m *Manager) BranchName(id string) string  { return "review/" + id }

// guard validates the id and, when the directory exists, that it resolves to a
// direct child of the root. Returns (exists, error).
func (m *Manager) guard(id string) (bool, error) {
	if err := ValidateSessionID(id); err != nil {
		return false, err
	}
	dir := m.SessionDir(id)
	if _, err := os.Lstat(dir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("workspace: stat: %w", err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, fmt.Errorf("workspace: resolve: %w", err)
	}
	if filepath.Dir(real) != m.root || filepath.Base(real) != id {
		return true, ErrOutsideRoot
	}
	return true, nil
}

// Create adds a worktree on a new review/<id> branch at baseSHA. Holds the mirror lock.
func (m *Manager) Create(ctx context.Context, mirrorPath, id, baseSHA string) (string, error) {
	if err := ValidateSessionID(id); err != nil {
		return "", err
	}
	if err := gitx.ValidateSHA(baseSHA); err != nil {
		return "", err
	}
	if err := os.MkdirAll(m.SessionDir(id), 0o750); err != nil {
		return "", fmt.Errorf("workspace: create session dir: %w", err)
	}
	if _, err := m.guard(id); err != nil {
		return "", err
	}
	unlock := m.locks.Lock(mirrorPath)
	defer unlock()
	repoDir := m.RepoDir(id)
	spec := gitx.Spec{Dir: mirrorPath, Args: []string{"worktree", "add", "-b", m.BranchName(id), repoDir, baseSHA}, Category: gitx.CategoryWorktree, Session: id}
	if _, err := m.runner.Run(ctx, spec); err != nil {
		return "", fmt.Errorf("workspace: worktree add: %w", err)
	}
	return repoDir, nil
}

// Cleanup removes the worktree, prunes, deletes the branch, and removes the
// session directory, continuing past individual failures. Idempotent.
func (m *Manager) Cleanup(ctx context.Context, mirrorPath, id string) error {
	exists, err := m.guard(id)
	if err != nil {
		return err
	}
	if mirrorPath != "" {
		if _, err := os.Stat(filepath.Join(mirrorPath, "HEAD")); err == nil {
			unlock := m.locks.Lock(mirrorPath)
			steps := []struct {
				name string
				args []string
			}{
				{"worktree remove", []string{"worktree", "remove", "--force", m.RepoDir(id)}},
				{"worktree prune", []string{"worktree", "prune"}},
				{"branch delete", []string{"branch", "-D", m.BranchName(id)}},
			}
			for _, st := range steps {
				if _, err := m.runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: st.args, Category: gitx.CategoryCleanup, Session: id}); err != nil {
					m.log.Debug("cleanup step failed", slog.String("session", id), slog.String("step", st.name), slog.String("error", err.Error()))
				}
			}
			unlock()
		}
	}
	if !exists {
		return nil
	}
	if err := os.RemoveAll(m.SessionDir(id)); err != nil {
		m.log.Warn("cleanup remove failed", slog.String("session", id), slog.String("step", "remove dir"), slog.String("error", err.Error()))
		return fmt.Errorf("workspace: remove session dir: %w", err)
	}
	return nil
}

// RemoveDir deletes a session directory without touching any mirror.
func (m *Manager) RemoveDir(id string) error {
	exists, err := m.guard(id)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return os.RemoveAll(m.SessionDir(id))
}
```

- [ ] **Step 4: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/workspace/ && go tool golangci-lint run ./internal/workspace/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/workspace && git commit -m "feat(task-001): workspace manager with guarded cleanup"
```

---

## Phase D — Review domain

### Task 10: Diff production and parsing

**Files:**
- Create: `apps/backend/internal/diff/diff.go`, `parse.go`, `parse_test.go`, `diff_test.go`

**Interfaces:**
- Produces:
  - `type FileStatus string` with `StatusAdded="added"`, `StatusModified="modified"`, `StatusDeleted="deleted"`, `StatusRenamed="renamed"`.
  - `type FileSummary struct{ Path string; PreviousPath string; Status FileStatus; Additions, Deletions int; Binary bool }` (JSON tags `path, previousPath, status, additions, deletions, binary`).
  - `type Totals struct{ Files, Additions, Deletions int }` (JSON `files, additions, deletions`).
  - `type FileDiff struct{ FileSummary; Truncated bool; Diff string }`.
  - `const MaxFileDiffBytes = 1 << 20`.
  - `func WriteCombined(ctx, r gitx.Runner, repoDir, base, head, outPath string) error`.
  - `func Summarize(ctx, r gitx.Runner, repoDir, base, head string) ([]FileSummary, Totals, error)` — sorted by path.
  - `func FileContent(ctx, r gitx.Runner, repoDir, base, head string, f FileSummary) (FileDiff, error)`.
  - `func parseRaw(b []byte) ([]rawEntry, error)`, `func parseNumstat(b []byte) ([]numstatEntry, error)` (internal).

- [ ] **Step 1: Write failing parser tests**

`apps/backend/internal/diff/parse_test.go`:

```go
package diff

import "testing"

func TestParseRaw(t *testing.T) {
	in := []byte(":000000 100644 0000000000000000000000000000000000000000 1111111111111111111111111111111111111111 A\x00new.txt\x00" +
		":100644 100644 2222222222222222222222222222222222222222 3333333333333333333333333333333333333333 M\x00mod.txt\x00" +
		":100644 000000 4444444444444444444444444444444444444444 0000000000000000000000000000000000000000 D\x00gone.txt\x00" +
		":100644 100644 5555555555555555555555555555555555555555 6666666666666666666666666666666666666666 R095\x00old/name.go\x00new/name.go\x00")
	got, err := parseRaw(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []rawEntry{{"new.txt", "", StatusAdded}, {"mod.txt", "", StatusModified}, {"gone.txt", "", StatusDeleted}, {"new/name.go", "old/name.go", StatusRenamed}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, got[i], want[i])
		}
	}
	if _, err := parseRaw([]byte(":100644 100644 a b M\x00")); err == nil {
		t.Error("truncated record accepted")
	}
}

func TestParseNumstat(t *testing.T) {
	in := []byte("3\t1\tmod.txt\x00-\t-\timg.png\x0010\t0\t\x00old/name.go\x00new/name.go\x00")
	got, err := parseNumstat(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != (numstatEntry{"mod.txt", "", 3, 1, false}) || got[1] != (numstatEntry{"img.png", "", 0, 0, true}) || got[2] != (numstatEntry{"new/name.go", "old/name.go", 10, 0, false}) {
		t.Errorf("got %+v", got)
	}
}
```

- [ ] **Step 2: Implement parsers**

`apps/backend/internal/diff/parse.go`:

```go
package diff

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type rawEntry struct {
	Path         string
	PreviousPath string
	Status       FileStatus
}

type numstatEntry struct {
	Path         string
	PreviousPath string
	Additions    int
	Deletions    int
	Binary       bool
}

func splitNul(b []byte) []string {
	parts := strings.Split(string(bytes.TrimSuffix(b, []byte{0})), "\x00")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

// parseRaw reads `git diff --raw -z` output.
func parseRaw(b []byte) ([]rawEntry, error) {
	toks := splitNul(b)
	var out []rawEntry
	for i := 0; i < len(toks); {
		hdr := toks[i]
		if !strings.HasPrefix(hdr, ":") {
			return nil, fmt.Errorf("diff: unexpected raw token %q", hdr)
		}
		fields := strings.Fields(hdr[1:])
		if len(fields) < 5 {
			return nil, fmt.Errorf("diff: malformed raw header %q", hdr)
		}
		code := fields[4][0]
		var e rawEntry
		switch code {
		case 'R', 'C':
			if i+2 >= len(toks) {
				return nil, fmt.Errorf("diff: truncated rename record")
			}
			e = rawEntry{Path: toks[i+2], PreviousPath: toks[i+1], Status: StatusRenamed}
			i += 3
		default:
			if i+1 >= len(toks) {
				return nil, fmt.Errorf("diff: truncated record")
			}
			e = rawEntry{Path: toks[i+1], Status: statusFor(code)}
			i += 2
		}
		out = append(out, e)
	}
	return out, nil
}

func statusFor(code byte) FileStatus {
	switch code {
	case 'A':
		return StatusAdded
	case 'D':
		return StatusDeleted
	default:
		return StatusModified
	}
}

// parseNumstat reads `git diff --numstat -z` output.
func parseNumstat(b []byte) ([]numstatEntry, error) {
	toks := splitNul(b)
	var out []numstatEntry
	for i := 0; i < len(toks); {
		parts := strings.SplitN(toks[i], "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("diff: malformed numstat %q", toks[i])
		}
		e := numstatEntry{}
		if parts[0] == "-" || parts[1] == "-" {
			e.Binary = true
		} else {
			var err error
			if e.Additions, err = strconv.Atoi(parts[0]); err != nil {
				return nil, fmt.Errorf("diff: numstat additions %q", parts[0])
			}
			if e.Deletions, err = strconv.Atoi(parts[1]); err != nil {
				return nil, fmt.Errorf("diff: numstat deletions %q", parts[1])
			}
		}
		if parts[2] == "" {
			if i+2 >= len(toks) {
				return nil, fmt.Errorf("diff: truncated numstat rename")
			}
			e.PreviousPath, e.Path = toks[i+1], toks[i+2]
			i += 3
		} else {
			e.Path = parts[2]
			i++
		}
		out = append(out, e)
	}
	return out, nil
}
```

Run: `cd <worktree-root>/apps/backend && go test ./internal/diff/ -run Parse` → will still fail until `diff.go` declares the types; proceed to Step 3 then run.

- [ ] **Step 3: Write failing real-git diff tests**

`apps/backend/internal/diff/diff_test.go`:

```go
package diff

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/testutil"
)

func TestSummarizeWriteAndFileContent(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()
	src.Commit("mod.txt", "a\nb\n", "add mod")
	base = src.Head()
	src.Commit("mod.txt", "a\nc\nd\n", "change mod")
	src.Commit("new.txt", "hello\n", "add new")
	src.Git("mv", "README.md", "docs/README.md")
	src.Git("commit", "-m", "rename")
	if err := os.WriteFile(filepath.Join(src.Work, "bin.dat"), []byte{0, 1, 2, 3, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	src.Git("add", "bin.dat")
	src.Git("commit", "-m", "binary")
	head := src.Head()

	runner, err := gitx.NewExecRunner(slog.New(slog.NewTextHandler(os.Stderr, nil)), gitx.Options{CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	ctx := context.Background()
	files, totals, err := Summarize(ctx, runner, src.Work, base, head)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]FileSummary{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	if totals.Files != 4 || totals.Additions != 4 || totals.Deletions != 1 {
		t.Errorf("totals = %+v", totals)
	}
	if f := byPath["mod.txt"]; f.Status != StatusModified || f.Additions != 2 || f.Deletions != 1 {
		t.Errorf("mod = %+v", f)
	}
	if f := byPath["new.txt"]; f.Status != StatusAdded || f.Additions != 1 {
		t.Errorf("new = %+v", f)
	}
	if f := byPath["docs/README.md"]; f.Status != StatusRenamed || f.PreviousPath != "README.md" {
		t.Errorf("rename = %+v", f)
	}
	if f := byPath["bin.dat"]; f.Status != StatusAdded || !f.Binary {
		t.Errorf("bin = %+v", f)
	}
	if files[0].Path > files[1].Path {
		t.Error("not sorted")
	}
	out := filepath.Join(t.TempDir(), "combined.diff")
	if err := WriteCombined(ctx, runner, src.Work, base, head, out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "diff --git a/mod.txt b/mod.txt") || !strings.Contains(string(b), "rename from README.md") {
		t.Errorf("combined.diff content:\n%s", b)
	}
	fd, err := FileContent(ctx, runner, src.Work, base, head, byPath["mod.txt"])
	if err != nil || fd.Truncated || !strings.Contains(fd.Diff, "+c") || strings.Contains(fd.Diff, "new.txt") {
		t.Errorf("file diff: %v %+v", err, fd)
	}
	fd, err = FileContent(ctx, runner, src.Work, base, head, byPath["docs/README.md"])
	if err != nil || !strings.Contains(fd.Diff, "rename from README.md") {
		t.Errorf("rename diff: %v %q", err, fd.Diff)
	}
	fd, err = FileContent(ctx, runner, src.Work, base, head, byPath["bin.dat"])
	if err != nil || fd.Diff != "" || !fd.Binary {
		t.Errorf("binary diff: %v %+v", err, fd)
	}
	if _, err := FileContent(ctx, runner, src.Work, base, head, FileSummary{Path: "-bad"}); err == nil {
		t.Error("option-like path accepted")
	}
}

func TestFileContentTruncates(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()
	big := strings.Repeat("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde\n", 20000) // ~1.28 MiB
	src.Commit("big.txt", big, "big")
	runner, _ := gitx.NewExecRunner(slog.New(slog.NewTextHandler(os.Stderr, nil)), gitx.Options{CommandTimeout: 30 * time.Second})
	defer runner.Close()
	fd, err := FileContent(context.Background(), runner, src.Work, base, src.Head(), FileSummary{Path: "big.txt", Status: StatusAdded})
	if err != nil || !fd.Truncated || len(fd.Diff) != MaxFileDiffBytes {
		t.Fatalf("err=%v truncated=%v len=%d", err, fd.Truncated, len(fd.Diff))
	}
}
```

- [ ] **Step 4: Implement diff.go**

`apps/backend/internal/diff/diff.go`:

```go
// Package diff produces combined.diff, the file summary, and per-file diffs.
package diff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jtumidanski/converge/internal/gitx"
)

// FileStatus is the change type of a file.
type FileStatus string

const (
	StatusAdded    FileStatus = "added"
	StatusModified FileStatus = "modified"
	StatusDeleted  FileStatus = "deleted"
	StatusRenamed  FileStatus = "renamed"
)

// MaxFileDiffBytes caps per-file diff text.
const MaxFileDiffBytes = 1 << 20

// FileSummary is one entry of the file tree.
type FileSummary struct {
	Path         string     `json:"path"`
	PreviousPath string     `json:"previousPath,omitempty"`
	Status       FileStatus `json:"status"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	Binary       bool       `json:"binary"`
}

// Totals are session-level counts.
type Totals struct {
	Files     int `json:"files"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// FileDiff is one file's unified diff.
type FileDiff struct {
	FileSummary
	Truncated bool
	Diff      string
}

func validateRange(base, head string) error {
	if err := gitx.ValidateSHA(base); err != nil {
		return err
	}
	return gitx.ValidateSHA(head)
}

// WriteCombined writes `git diff --find-renames base head` atomically to outPath.
func WriteCombined(ctx context.Context, r gitx.Runner, repoDir, base, head, outPath string) error {
	if err := validateRange(base, head); err != nil {
		return err
	}
	res, err := r.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"diff", "--find-renames", base, head}, Category: gitx.CategoryDiff})
	if err != nil {
		return fmt.Errorf("diff: combined: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".combined-*.tmp")
	if err != nil {
		return fmt.Errorf("diff: temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(res.Stdout); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("diff: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("diff: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("diff: close: %w", err)
	}
	return os.Rename(tmp.Name(), outPath)
}

// Summarize joins --raw and --numstat output into FileSummaries sorted by path.
func Summarize(ctx context.Context, r gitx.Runner, repoDir, base, head string) ([]FileSummary, Totals, error) {
	if err := validateRange(base, head); err != nil {
		return nil, Totals{}, err
	}
	rawRes, err := r.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"diff", "--raw", "-z", "--find-renames", base, head}, Category: gitx.CategoryDiff})
	if err != nil {
		return nil, Totals{}, fmt.Errorf("diff: raw: %w", err)
	}
	numRes, err := r.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"diff", "--numstat", "-z", "--find-renames", base, head}, Category: gitx.CategoryDiff})
	if err != nil {
		return nil, Totals{}, fmt.Errorf("diff: numstat: %w", err)
	}
	raws, err := parseRaw(rawRes.Stdout)
	if err != nil {
		return nil, Totals{}, err
	}
	nums, err := parseNumstat(numRes.Stdout)
	if err != nil {
		return nil, Totals{}, err
	}
	counts := make(map[string]numstatEntry, len(nums))
	for _, n := range nums {
		counts[n.Path] = n
	}
	files := make([]FileSummary, 0, len(raws))
	var totals Totals
	for _, e := range raws {
		n := counts[e.Path]
		f := FileSummary{Path: e.Path, PreviousPath: e.PreviousPath, Status: e.Status, Additions: n.Additions, Deletions: n.Deletions, Binary: n.Binary}
		files = append(files, f)
		totals.Files++
		totals.Additions += f.Additions
		totals.Deletions += f.Deletions
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, totals, nil
}

// FileContent returns the unified diff for one file, capped at MaxFileDiffBytes.
func FileContent(ctx context.Context, r gitx.Runner, repoDir, base, head string, f FileSummary) (FileDiff, error) {
	if err := validateRange(base, head); err != nil {
		return FileDiff{}, err
	}
	if err := gitx.ValidatePathArg(f.Path); err != nil {
		return FileDiff{}, err
	}
	args := []string{"diff", "--find-renames", base, head, "--", f.Path}
	if f.PreviousPath != "" {
		if err := gitx.ValidatePathArg(f.PreviousPath); err != nil {
			return FileDiff{}, err
		}
		args = append(args, f.PreviousPath)
	}
	out := FileDiff{FileSummary: f}
	if f.Binary {
		return out, nil
	}
	res, err := r.Run(ctx, gitx.Spec{Dir: repoDir, Args: args, Category: gitx.CategoryDiff})
	if err != nil {
		return FileDiff{}, fmt.Errorf("diff: file %s: %w", f.Path, err)
	}
	text := res.Stdout
	if len(text) > MaxFileDiffBytes {
		text = text[:MaxFileDiffBytes]
		out.Truncated = true
	}
	out.Diff = string(text)
	return out, nil
}
```

- [ ] **Step 5: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/diff/ && go tool golangci-lint run ./internal/diff/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/diff && git commit -m "feat(task-001): combined diff, summary parsing and per-file diffs"
```

---

### Task 11: Session model, errors, on-disk store, recovery, sweep

**Files:**
- Create: `apps/backend/internal/session/codes.go`, `errors.go`, `model.go`, `builder.go`, `id.go`, `record.go`, `store.go`, `sweep.go`, `model_test.go`, `record_test.go`, `store_test.go`

**Interfaces:**
- Consumes: `diff.FileSummary`, `diff.Totals`, `workspace.ValidateSessionID`, `gitx.ValidateRepoFullName`, `gitx.ValidateBranchSyntax`, `gitx.ValidateChangeNumbers`.
- Produces:
  - `type Code string` constants: `CodeNotMerged`, `CodeIncompatibleTargets`, `CodeNotOnBaseBranch`, `CodeMissingCommits`, `CodeBaseUndetermined`, `CodeConflict`, `CodeProviderAuth`, `CodeProviderUnavailable`, `CodeRepositoryUnavailable`, `CodeGitFailure`, `CodeInterrupted` with the exact strings from Global Constraints.
  - `type Diagnostics struct{ WorkspacePath, Branch, Strategy, SourceSHA string }` (JSON `workspacePath, branch, strategy, sourceSha`).
  - `type ReviewError struct{ Code Code; Message string; Change int; Commit string; ConflictingFiles []string; AppliedChanges []int; PossibleDependency bool; Diagnostics *Diagnostics }` implementing `error`; JSON tags `code, message, change, commit, conflictingFiles, appliedChanges, possibleDependency, diagnostics` (`omitempty` on all but code/message).
  - `type Status string` (`StatusCreating`… exact strings); `type Strategy string` (`StrategyMerge="merge"`, `StrategySquash="squash"`, `StrategyRebase="rebase"`).
  - `type ResolvedChangeParams struct{ Number int; Title, Author, WebURL string; MergedAt time.Time; Strategy Strategy; LandingSHAs []string; SourceSHA string }`; `func NewResolvedChange(p ResolvedChangeParams) (ResolvedChange, error)`; accessors `Number() Title() Author() WebURL() MergedAt() Strategy() LandingSHAs() SourceSHA()`.
  - `type Session` immutable; accessors `ID() ProviderID() Repository() BaseBranch() BaseSHA() HeadSHA() RequestedChanges() ResolvedChanges() Status() Stage() Error() *ReviewError Totals() *diff.Totals Files() []diff.FileSummary CreatedAt() UpdatedAt() ExpiresAt()`; `IsExpired(now time.Time) bool`; `IsActive() bool` (not FINISHED/EXPIRED); `EarliestChange() (ResolvedChange, bool)`.
  - Transitions (all return a new `Session`): `WithStage(stage string, now)`, `WithResolved(rcs []ResolvedChange, now)`, `WithBase(sha string, now) (Session, error)`, `Ready(headSHA string, files []diff.FileSummary, totals diff.Totals, now) (Session, error)`, `Conflicted(err *ReviewError, now)`, `Failed(err *ReviewError, now)`, `Finished(now)`, `Expired(now)`.
  - `NewBuilder().SetID().SetProviderID().SetRepository().SetBaseBranch().SetRequestedChanges().SetCreatedAt().SetTTL().Build() (Session, error)`.
  - `func NewID() (string, error)`.
  - `const SchemaVersion = 1`; `type Record struct{...}` with JSON tags matching the PRD §6 schema; `func ToRecord(Session) Record`; `func FromRecord(Record) (Session, error)`.
  - `type Cleaner interface{ Cleanup(ctx, s Session) error; RemoveDir(ctx, id string) error }`.
  - `func NewStore(root string, ttl time.Duration, cleaner Cleaner, log *slog.Logger, now func() time.Time) *Store`; `(*Store) Root() string`, `Dir(id) string`, `Save(Session) error`, `Get(id) (Session, bool)`, `List() []Session` (active, newest first), `Finish(ctx, id) error` (idempotent), `LoadAll(ctx) error`, `Sweep(ctx)`, `RunSweeper(ctx, interval)`.
  - `var ErrNotFound = errors.New("session: not found")`.

- [ ] **Step 1: Write failing model/builder tests**

`apps/backend/internal/session/model_test.go`:

```go
package session

import (
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
)

var t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func newSession(t *testing.T) Session {
	t.Helper()
	s, err := NewBuilder().SetID("0123abcd").SetProviderID("gh").SetRepository("atlas/server").SetBaseBranch("main").
		SetRequestedChanges([]int{427, 421}).SetCreatedAt(t0).SetTTL(24 * time.Hour).Build()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuilderAndAccessors(t *testing.T) {
	s := newSession(t)
	if s.Status() != StatusCreating || s.Stage() != "" || s.BaseSHA() != "" || !s.ExpiresAt().Equal(t0.Add(24*time.Hour)) || !s.UpdatedAt().Equal(t0) {
		t.Errorf("initial state wrong: %+v", s)
	}
	if got := s.RequestedChanges(); len(got) != 2 || got[0] != 421 || got[1] != 427 {
		t.Errorf("requested = %v (want sorted, de-duplicated)", got)
	}
	got := s.RequestedChanges()
	got[0] = 999
	if s.RequestedChanges()[0] != 421 {
		t.Error("RequestedChanges leaked internal slice")
	}
	if s.IsExpired(t0.Add(23 * time.Hour)) || !s.IsExpired(t0.Add(25*time.Hour)) || !s.IsActive() {
		t.Error("expiry wrong")
	}
	bad := []struct {
		name string
		f    func(*Builder) *Builder
	}{
		{"id", func(b *Builder) *Builder { return b.SetID("xyz") }},
		{"provider", func(b *Builder) *Builder { return b.SetProviderID("") }},
		{"repo", func(b *Builder) *Builder { return b.SetRepository("../x") }},
		{"branch", func(b *Builder) *Builder { return b.SetBaseBranch("-x") }},
		{"changes", func(b *Builder) *Builder { return b.SetRequestedChanges(nil) }},
		{"ttl", func(b *Builder) *Builder { return b.SetTTL(0) }},
	}
	for _, tc := range bad {
		b := NewBuilder().SetID("0123abcd").SetProviderID("gh").SetRepository("atlas/server").SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(t0).SetTTL(time.Hour)
		if _, err := tc.f(b).Build(); err == nil {
			t.Errorf("%s: invalid accepted", tc.name)
		}
	}
}

func TestTransitionsAreImmutable(t *testing.T) {
	s := newSession(t)
	t1 := t0.Add(time.Minute)
	s2 := s.WithStage("resolving", t1)
	if s.Stage() != "" || s2.Stage() != "resolving" || !s2.UpdatedAt().Equal(t1) {
		t.Error("WithStage mutated receiver or did not apply")
	}
	rc, err := NewResolvedChange(ResolvedChangeParams{Number: 421, Title: "t", Author: "a", MergedAt: t0, Strategy: StrategySquash, LandingSHAs: []string{strings.Repeat("a", 40)}, SourceSHA: strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	s3 := s2.WithResolved([]ResolvedChange{rc}, t1)
	if e, ok := s3.EarliestChange(); !ok || e.Number() != 421 {
		t.Error("EarliestChange")
	}
	if _, err := s3.WithBase("bad", t1); err == nil {
		t.Error("bad base accepted")
	}
	s4, _ := s3.WithBase(strings.Repeat("b", 40), t1)
	files := []diff.FileSummary{{Path: "a.go", Status: diff.StatusAdded, Additions: 1}}
	s5, err := s4.Ready(strings.Repeat("c", 40), files, diff.Totals{Files: 1, Additions: 1}, t1)
	if err != nil || s5.Status() != StatusReady || s5.Stage() != "" || s5.HeadSHA() != strings.Repeat("c", 40) || s5.Totals().Files != 1 || len(s5.Files()) != 1 {
		t.Errorf("Ready: %v %+v", err, s5)
	}
	if _, err := s.Ready(strings.Repeat("c", 40), nil, diff.Totals{}, t1); err == nil {
		t.Error("Ready without base accepted")
	}
	re := &ReviewError{Code: CodeConflict, Message: "boom", Change: 421, ConflictingFiles: []string{"x"}}
	s6 := s4.Conflicted(re, t1)
	if s6.Status() != StatusConflicted || s6.Error() == nil || s6.Error().Code != CodeConflict {
		t.Error("Conflicted")
	}
	s6.Error().ConflictingFiles[0] = "mutated"
	if s6.Error().ConflictingFiles[0] != "x" {
		t.Error("Error() leaked internal slice")
	}
	s7 := s4.Failed(&ReviewError{Code: CodeNotMerged, Message: "m"}, t1)
	if s7.Status() != StatusFailed || s7.Error().Code != CodeNotMerged {
		t.Error("Failed")
	}
	if f := s5.Finished(t1); f.Status() != StatusFinished || f.IsActive() {
		t.Error("Finished")
	}
	if e := s5.Expired(t1); e.Status() != StatusExpired || e.IsActive() {
		t.Error("Expired")
	}
	if _, err := NewResolvedChange(ResolvedChangeParams{Number: 0}); err == nil {
		t.Error("resolved change number 0 accepted")
	}
	if _, err := NewResolvedChange(ResolvedChangeParams{Number: 1, Title: "t", Strategy: "weird", LandingSHAs: []string{strings.Repeat("a", 40)}}); err == nil {
		t.Error("bad strategy accepted")
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewID()
		if err != nil || len(id) != 8 || strings.ToLower(id) != id || seen[id] {
			t.Fatalf("id %q err %v", id, err)
		}
		for _, c := range id {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Fatalf("non-hex %q", id)
			}
		}
		seen[id] = true
	}
}
```

- [ ] **Step 2: Implement codes, errors, model, builder, id**

`apps/backend/internal/session/codes.go`:

```go
// Package session holds the review session model and its on-disk store.
package session

// Code is a review error code exposed by the API and CLI.
type Code string

const (
	CodeNotMerged             Code = "NOT_MERGED"
	CodeIncompatibleTargets   Code = "INCOMPATIBLE_TARGETS"
	CodeNotOnBaseBranch       Code = "NOT_ON_BASE_BRANCH"
	CodeMissingCommits        Code = "MISSING_COMMITS"
	CodeBaseUndetermined      Code = "BASE_UNDETERMINED"
	CodeConflict              Code = "CONFLICT"
	CodeProviderAuth          Code = "PROVIDER_AUTH"
	CodeProviderUnavailable   Code = "PROVIDER_UNAVAILABLE"
	CodeRepositoryUnavailable Code = "REPOSITORY_UNAVAILABLE"
	CodeGitFailure            Code = "GIT_FAILURE"
	CodeInterrupted           Code = "INTERRUPTED"
)

// Status is the session lifecycle state.
type Status string

const (
	StatusCreating   Status = "CREATING"
	StatusReady      Status = "READY"
	StatusConflicted Status = "CONFLICTED"
	StatusFailed     Status = "FAILED"
	StatusFinished   Status = "FINISHED"
	StatusExpired    Status = "EXPIRED"
)

// Strategy is how a change landed on the target branch.
type Strategy string

const (
	StrategyMerge  Strategy = "merge"
	StrategySquash Strategy = "squash"
	StrategyRebase Strategy = "rebase"
)

// Stage names.
const (
	StageResolving         = "resolving"
	StageUpdatingRepo      = "updating-repository"
	StageCreatingWorkspace = "creating-workspace"
	StageApplyingPrefix    = "applying:"
	StageDiffing           = "diffing"
)
```

`apps/backend/internal/session/errors.go`:

```go
package session

import "errors"

// ErrNotFound is returned for unknown session IDs.
var ErrNotFound = errors.New("session: not found")

// Diagnostics carries operator-facing details; the UI shows them only under "Diagnostics".
type Diagnostics struct {
	WorkspacePath string `json:"workspacePath,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Strategy      string `json:"strategy,omitempty"`
	SourceSHA     string `json:"sourceSha,omitempty"`
}

// ReviewError is both the domain error and the API "error" attribute.
type ReviewError struct {
	Code               Code         `json:"code"`
	Message            string       `json:"message"`
	Change             int          `json:"change,omitempty"`
	Commit             string       `json:"commit,omitempty"`
	ConflictingFiles   []string     `json:"conflictingFiles,omitempty"`
	AppliedChanges     []int        `json:"appliedChanges,omitempty"`
	PossibleDependency bool         `json:"possibleDependency,omitempty"`
	Diagnostics        *Diagnostics `json:"diagnostics,omitempty"`
}

func (e *ReviewError) Error() string { return string(e.Code) + ": " + e.Message }

// clone deep-copies so callers cannot mutate a Session's error.
func (e *ReviewError) clone() *ReviewError {
	if e == nil {
		return nil
	}
	c := *e
	c.ConflictingFiles = append([]string(nil), e.ConflictingFiles...)
	c.AppliedChanges = append([]int(nil), e.AppliedChanges...)
	if e.Diagnostics != nil {
		d := *e.Diagnostics
		c.Diagnostics = &d
	}
	return &c
}
```

`apps/backend/internal/session/model.go`:

```go
package session

import (
	"errors"
	"fmt"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
	"github.com/jtumidanski/converge/internal/gitx"
)

// ResolvedChangeParams is the input to NewResolvedChange.
type ResolvedChangeParams struct {
	Number      int
	Title       string
	Author      string
	WebURL      string
	MergedAt    time.Time
	Strategy    Strategy
	LandingSHAs []string
	SourceSHA   string
}

// ResolvedChange is an immutable record of how a selected change landed.
type ResolvedChange struct {
	number      int
	title       string
	author      string
	webURL      string
	mergedAt    time.Time
	strategy    Strategy
	landingSHAs []string
	sourceSHA   string
}

// NewResolvedChange validates params.
func NewResolvedChange(p ResolvedChangeParams) (ResolvedChange, error) {
	if p.Number <= 0 {
		return ResolvedChange{}, errors.New("resolved change: number must be positive")
	}
	if p.Title == "" {
		return ResolvedChange{}, errors.New("resolved change: title is required")
	}
	switch p.Strategy {
	case StrategyMerge, StrategySquash, StrategyRebase:
	default:
		return ResolvedChange{}, fmt.Errorf("resolved change: unknown strategy %q", p.Strategy)
	}
	if len(p.LandingSHAs) == 0 {
		return ResolvedChange{}, errors.New("resolved change: landing shas required")
	}
	for _, s := range p.LandingSHAs {
		if err := gitx.ValidateSHA(s); err != nil {
			return ResolvedChange{}, err
		}
	}
	if p.SourceSHA != "" {
		if err := gitx.ValidateSHA(p.SourceSHA); err != nil {
			return ResolvedChange{}, err
		}
	}
	return ResolvedChange{number: p.Number, title: p.Title, author: p.Author, webURL: p.WebURL, mergedAt: p.MergedAt, strategy: p.Strategy, landingSHAs: append([]string(nil), p.LandingSHAs...), sourceSHA: p.SourceSHA}, nil
}

func (r ResolvedChange) Number() int          { return r.number }
func (r ResolvedChange) Title() string        { return r.title }
func (r ResolvedChange) Author() string       { return r.author }
func (r ResolvedChange) WebURL() string       { return r.webURL }
func (r ResolvedChange) MergedAt() time.Time  { return r.mergedAt }
func (r ResolvedChange) Strategy() Strategy   { return r.strategy }
func (r ResolvedChange) SourceSHA() string    { return r.sourceSHA }
func (r ResolvedChange) LandingSHAs() []string { return append([]string(nil), r.landingSHAs...) }

// Session is an immutable review session; transitions return new values.
type Session struct {
	id               string
	providerID       string
	repository       string
	baseBranch       string
	baseSHA          string
	headSHA          string
	requestedChanges []int
	resolved         []ResolvedChange
	status           Status
	stage            string
	err              *ReviewError
	totals           *diff.Totals
	files            []diff.FileSummary
	createdAt        time.Time
	updatedAt        time.Time
	expiresAt        time.Time
}

func (s Session) ID() string            { return s.id }
func (s Session) ProviderID() string    { return s.providerID }
func (s Session) Repository() string    { return s.repository }
func (s Session) BaseBranch() string    { return s.baseBranch }
func (s Session) BaseSHA() string       { return s.baseSHA }
func (s Session) HeadSHA() string       { return s.headSHA }
func (s Session) Status() Status        { return s.status }
func (s Session) Stage() string         { return s.stage }
func (s Session) CreatedAt() time.Time  { return s.createdAt }
func (s Session) UpdatedAt() time.Time  { return s.updatedAt }
func (s Session) ExpiresAt() time.Time  { return s.expiresAt }
func (s Session) Error() *ReviewError   { return s.err.clone() }
func (s Session) RequestedChanges() []int { return append([]int(nil), s.requestedChanges...) }
func (s Session) ResolvedChanges() []ResolvedChange {
	return append([]ResolvedChange(nil), s.resolved...)
}
func (s Session) Files() []diff.FileSummary { return append([]diff.FileSummary(nil), s.files...) }
func (s Session) Totals() *diff.Totals {
	if s.totals == nil {
		return nil
	}
	t := *s.totals
	return &t
}

// IsExpired reports whether now is past the expiry.
func (s Session) IsExpired(now time.Time) bool { return !now.Before(s.expiresAt) }

// IsActive is true unless the session is FINISHED or EXPIRED.
func (s Session) IsActive() bool { return s.status != StatusFinished && s.status != StatusExpired }

// EarliestChange returns the first resolved change (sorted by merge time).
func (s Session) EarliestChange() (ResolvedChange, bool) {
	if len(s.resolved) == 0 {
		return ResolvedChange{}, false
	}
	return s.resolved[0], true
}

func (s Session) touch(now time.Time) Session { s.updatedAt = now; return s }

func (s Session) WithStage(stage string, now time.Time) Session {
	s.stage = stage
	return s.touch(now)
}

func (s Session) WithResolved(rcs []ResolvedChange, now time.Time) Session {
	s.resolved = append([]ResolvedChange(nil), rcs...)
	return s.touch(now)
}

func (s Session) WithBase(sha string, now time.Time) (Session, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return Session{}, err
	}
	s.baseSHA = sha
	return s.touch(now), nil
}

// Ready marks the session READY; requires a base SHA.
func (s Session) Ready(headSHA string, files []diff.FileSummary, totals diff.Totals, now time.Time) (Session, error) {
	if s.baseSHA == "" {
		return Session{}, errors.New("session: cannot be ready without a base sha")
	}
	if err := gitx.ValidateSHA(headSHA); err != nil {
		return Session{}, err
	}
	s.headSHA = headSHA
	s.files = append([]diff.FileSummary(nil), files...)
	s.totals = &totals
	s.status = StatusReady
	s.stage = ""
	s.err = nil
	return s.touch(now), nil
}

func (s Session) Conflicted(err *ReviewError, now time.Time) Session {
	s.status = StatusConflicted
	s.stage = ""
	s.err = err.clone()
	return s.touch(now)
}

func (s Session) Failed(err *ReviewError, now time.Time) Session {
	s.status = StatusFailed
	s.stage = ""
	s.err = err.clone()
	return s.touch(now)
}

func (s Session) Finished(now time.Time) Session {
	s.status = StatusFinished
	s.stage = ""
	return s.touch(now)
}

func (s Session) Expired(now time.Time) Session {
	s.status = StatusExpired
	s.stage = ""
	return s.touch(now)
}
```

`apps/backend/internal/session/builder.go`:

```go
package session

import (
	"errors"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/workspace"
)

// Builder constructs a new CREATING session.
type Builder struct {
	s       Session
	changes []int
	ttl     time.Duration
}

func NewBuilder() *Builder { return &Builder{} }

func (b *Builder) SetID(v string) *Builder                { b.s.id = v; return b }
func (b *Builder) SetProviderID(v string) *Builder        { b.s.providerID = v; return b }
func (b *Builder) SetRepository(v string) *Builder        { b.s.repository = v; return b }
func (b *Builder) SetBaseBranch(v string) *Builder        { b.s.baseBranch = v; return b }
func (b *Builder) SetRequestedChanges(v []int) *Builder   { b.changes = v; return b }
func (b *Builder) SetCreatedAt(v time.Time) *Builder      { b.s.createdAt = v; return b }
func (b *Builder) SetTTL(v time.Duration) *Builder        { b.ttl = v; return b }

// Build validates every invariant.
func (b *Builder) Build() (Session, error) {
	s := b.s
	if err := workspace.ValidateSessionID(s.id); err != nil {
		return Session{}, err
	}
	if s.providerID == "" {
		return Session{}, errors.New("session: provider id is required")
	}
	if err := gitx.ValidateRepoFullName(s.repository); err != nil {
		return Session{}, err
	}
	if err := gitx.ValidateBranchSyntax(s.baseBranch); err != nil {
		return Session{}, err
	}
	changes, err := gitx.ValidateChangeNumbers(b.changes)
	if err != nil {
		return Session{}, err
	}
	if b.ttl <= 0 {
		return Session{}, errors.New("session: ttl must be positive")
	}
	if s.createdAt.IsZero() {
		return Session{}, errors.New("session: created at is required")
	}
	s.requestedChanges = changes
	s.status = StatusCreating
	s.updatedAt = s.createdAt
	s.expiresAt = s.createdAt.Add(b.ttl)
	return s, nil
}
```

`apps/backend/internal/session/id.go`:

```go
package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewID returns 8 lowercase hex characters from a CSPRNG.
func NewID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("session: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
```

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/session/ -run 'Builder|Transitions|NewID'` → PASS.

- [ ] **Step 3: Write failing record and store tests**

`apps/backend/internal/session/record_test.go`:

```go
package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
)

func TestRecordRoundTrip(t *testing.T) {
	s := newSession(t)
	rc, _ := NewResolvedChange(ResolvedChangeParams{Number: 421, Title: "t", Author: "a", WebURL: "u", MergedAt: t0, Strategy: StrategyMerge, LandingSHAs: []string{strings.Repeat("a", 40)}, SourceSHA: strings.Repeat("a", 40)})
	s = s.WithResolved([]ResolvedChange{rc}, t0)
	s, _ = s.WithBase(strings.Repeat("b", 40), t0)
	s, _ = s.Ready(strings.Repeat("c", 40), []diff.FileSummary{{Path: "x", Status: diff.StatusAdded, Additions: 2}}, diff.Totals{Files: 1, Additions: 2}, t0.Add(time.Minute))
	b, err := json.Marshal(ToRecord(s))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"schemaVersion":1`, `"id":"0123abcd"`, `"providerId":"gh"`, `"status":"READY"`, `"resolvedChanges"`, `"landingShas"`, `"totals"`, `"files"`, `"expiresAt"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("json lacks %s: %s", key, b)
		}
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	back, err := FromRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID() != s.ID() || back.Status() != StatusReady || back.BaseSHA() != s.BaseSHA() || back.HeadSHA() != s.HeadSHA() || len(back.ResolvedChanges()) != 1 || back.ResolvedChanges()[0].Strategy() != StrategyMerge || back.Totals().Additions != 2 || len(back.Files()) != 1 || !back.ExpiresAt().Equal(s.ExpiresAt()) {
		t.Errorf("round trip mismatch: %+v", back)
	}
	// unknown fields are ignored
	var r2 Record
	if err := json.Unmarshal([]byte(`{"schemaVersion":1,"id":"0123abcd","providerId":"gh","repository":"a/b","baseBranch":"main","requestedChanges":[1],"status":"CREATING","createdAt":"2026-09-01T12:00:00Z","updatedAt":"2026-09-01T12:00:00Z","expiresAt":"2026-09-02T12:00:00Z","futureField":true}`), &r2); err != nil {
		t.Fatal(err)
	}
	if _, err := FromRecord(r2); err != nil {
		t.Fatal(err)
	}
	r2.Status = "WEIRD"
	if _, err := FromRecord(r2); err == nil {
		t.Error("bad status accepted")
	}
}
```

`apps/backend/internal/session/store_test.go`:

```go
package session

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeCleaner struct {
	mu      sync.Mutex
	cleaned []string
	removed []string
	fail    error
}

func (f *fakeCleaner) Cleanup(_ context.Context, s Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleaned = append(f.cleaned, s.ID())
	if f.fail != nil {
		return f.fail
	}
	return nil
}

func (f *fakeCleaner) RemoveDir(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	return nil
}

func newStore(t *testing.T, now *time.Time) (*Store, *fakeCleaner) {
	t.Helper()
	fc := &fakeCleaner{}
	st := NewStore(t.TempDir(), 24*time.Hour, fc, slog.New(slog.NewTextHandler(os.Stderr, nil)), func() time.Time { return *now })
	return st, fc
}

func TestSaveGetListAtomic(t *testing.T) {
	now := t0
	st, _ := newStore(t, &now)
	s := newSession(t)
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(st.Dir("0123abcd"), "session.json")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(st.Dir("0123abcd"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatal("temp file left behind")
		}
	}
	got, ok := st.Get("0123abcd")
	if !ok || got.Repository() != "atlas/server" {
		t.Fatal("Get")
	}
	if _, ok := st.Get("ffffffff"); ok {
		t.Fatal("unknown found")
	}
	other, _ := NewBuilder().SetID("aaaaaaaa").SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(t0.Add(time.Hour)).SetTTL(time.Hour).Build()
	_ = st.Save(other)
	list := st.List()
	if len(list) != 2 || list[0].ID() != "aaaaaaaa" {
		t.Fatalf("List = %v", list)
	}
}

func TestFinishIsIdempotentAndCleans(t *testing.T) {
	now := t0
	st, fc := newStore(t, &now)
	_ = st.Save(newSession(t))
	ctx := context.Background()
	if err := st.Finish(ctx, "0123abcd"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get("0123abcd"); got.Status() != StatusFinished {
		t.Fatalf("status = %s", got.Status())
	}
	if err := st.Finish(ctx, "0123abcd"); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(ctx, "ffffffff"); err != nil {
		t.Fatal("unknown must be a no-op")
	}
	if len(fc.cleaned) != 1 {
		t.Fatalf("cleanups = %v", fc.cleaned)
	}
	if len(st.List()) != 0 {
		t.Fatal("finished session still listed")
	}
	fc.fail = errors.New("boom")
	_ = st.Save(newSession(t))
	if err := st.Finish(ctx, "0123abcd"); err == nil {
		t.Fatal("cleanup failure must surface")
	}
}

func TestLoadAllRecovery(t *testing.T) {
	now := t0.Add(48 * time.Hour)
	st, fc := newStore(t, &now)
	// creating → interrupted
	creating := newSession(t) // created t0, expires t0+24h → also expired at now; recovery marks INTERRUPTED first, then expiry sweep cleans it
	writeRecord(t, st, creating)
	fresh, _ := NewBuilder().SetID("bbbbbbbb").SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(now.Add(-time.Hour)).SetTTL(24 * time.Hour).Build()
	writeRecord(t, st, fresh)
	// invalid directory older than TTL
	old := filepath.Join(st.Root(), "cccccccc")
	_ = os.MkdirAll(old, 0o755)
	oldTime := now.Add(-48 * time.Hour)
	_ = os.Chtimes(old, oldTime, oldTime)
	// invalid directory newer than TTL is left alone
	_ = os.MkdirAll(filepath.Join(st.Root(), "dddddddd"), 0o755)
	// junk file
	_ = os.WriteFile(filepath.Join(st.Root(), "junk.txt"), []byte("x"), 0o644)

	if err := st.LoadAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := st.Get("bbbbbbbb")
	if b.Status() != StatusFailed || b.Error() == nil || b.Error().Code != CodeInterrupted {
		t.Errorf("fresh creating session: %+v", b)
	}
	a, _ := st.Get("0123abcd")
	if a.Status() != StatusExpired {
		t.Errorf("old session status = %s", a.Status())
	}
	if len(fc.cleaned) != 1 || fc.cleaned[0] != "0123abcd" {
		t.Errorf("cleaned = %v", fc.cleaned)
	}
	if len(fc.removed) != 1 || fc.removed[0] != "cccccccc" {
		t.Errorf("removed = %v", fc.removed)
	}
	// on-disk record for bbbbbbbb was rewritten
	raw, _ := os.ReadFile(filepath.Join(st.Dir("bbbbbbbb"), "session.json"))
	if !strings.Contains(string(raw), `"INTERRUPTED"`) {
		t.Errorf("record not rewritten: %s", raw)
	}
}

func TestSweepExpires(t *testing.T) {
	now := t0
	st, fc := newStore(t, &now)
	s := newSession(t)
	s, _ = s.WithBase(strings.Repeat("b", 40), t0)
	_ = st.Save(s)
	st.Sweep(context.Background())
	if len(fc.cleaned) != 0 {
		t.Fatal("swept too early")
	}
	now = t0.Add(25 * time.Hour)
	st.Sweep(context.Background())
	if got, _ := st.Get("0123abcd"); got.Status() != StatusExpired || len(fc.cleaned) != 1 {
		t.Fatalf("not expired: %s %v", got.Status(), fc.cleaned)
	}
	st.Sweep(context.Background())
	if len(fc.cleaned) != 1 {
		t.Fatal("expired session cleaned twice")
	}
}

func writeRecord(t *testing.T, st *Store, s Session) {
	t.Helper()
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	delete(st.index, s.ID()) // simulate a fresh process
	st.mu.Unlock()
}
```

- [ ] **Step 4: Implement record, store, sweep**

`apps/backend/internal/session/record.go`:

```go
package session

import (
	"fmt"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
)

// SchemaVersion of session.json.
const SchemaVersion = 1

// ResolvedChangeRecord is the DTO for ResolvedChange.
type ResolvedChangeRecord struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	WebURL      string    `json:"webUrl"`
	MergedAt    time.Time `json:"mergedAt"`
	Strategy    Strategy  `json:"strategy"`
	LandingSHAs []string  `json:"landingShas"`
	SourceSHA   string    `json:"sourceSha,omitempty"`
}

// Record is the on-disk shape of a Session (PRD §6).
type Record struct {
	SchemaVersion    int                    `json:"schemaVersion"`
	ID               string                 `json:"id"`
	ProviderID       string                 `json:"providerId"`
	Repository       string                 `json:"repository"`
	BaseBranch       string                 `json:"baseBranch"`
	BaseSHA          *string                `json:"baseSha"`
	HeadSHA          *string                `json:"headSha"`
	RequestedChanges []int                  `json:"requestedChanges"`
	ResolvedChanges  []ResolvedChangeRecord `json:"resolvedChanges"`
	Status           Status                 `json:"status"`
	Stage            *string                `json:"stage"`
	Error            *ReviewError           `json:"error"`
	Totals           *diff.Totals           `json:"totals"`
	Files            []diff.FileSummary     `json:"files,omitempty"`
	CreatedAt        time.Time              `json:"createdAt"`
	UpdatedAt        time.Time              `json:"updatedAt"`
	ExpiresAt        time.Time              `json:"expiresAt"`
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ToRecord converts a Session for serialisation.
func ToRecord(s Session) Record {
	r := Record{
		SchemaVersion: SchemaVersion, ID: s.id, ProviderID: s.providerID, Repository: s.repository, BaseBranch: s.baseBranch,
		BaseSHA: optional(s.baseSHA), HeadSHA: optional(s.headSHA), RequestedChanges: s.RequestedChanges(),
		ResolvedChanges: make([]ResolvedChangeRecord, 0, len(s.resolved)), Status: s.status, Stage: optional(s.stage),
		Error: s.Error(), Totals: s.Totals(), Files: s.Files(), CreatedAt: s.createdAt, UpdatedAt: s.updatedAt, ExpiresAt: s.expiresAt,
	}
	for _, rc := range s.resolved {
		r.ResolvedChanges = append(r.ResolvedChanges, ResolvedChangeRecord{Number: rc.number, Title: rc.title, Author: rc.author, WebURL: rc.webURL, MergedAt: rc.mergedAt, Strategy: rc.strategy, LandingSHAs: rc.LandingSHAs(), SourceSHA: rc.sourceSHA})
	}
	return r
}

// FromRecord rebuilds a Session, validating enums and identifiers.
func FromRecord(r Record) (Session, error) {
	if r.SchemaVersion != SchemaVersion {
		return Session{}, fmt.Errorf("session %s: unsupported schema version %d", r.ID, r.SchemaVersion)
	}
	ttl := r.ExpiresAt.Sub(r.CreatedAt)
	s, err := NewBuilder().SetID(r.ID).SetProviderID(r.ProviderID).SetRepository(r.Repository).SetBaseBranch(r.BaseBranch).
		SetRequestedChanges(r.RequestedChanges).SetCreatedAt(r.CreatedAt).SetTTL(ttl).Build()
	if err != nil {
		return Session{}, fmt.Errorf("session %s: %w", r.ID, err)
	}
	switch r.Status {
	case StatusCreating, StatusReady, StatusConflicted, StatusFailed, StatusFinished, StatusExpired:
	default:
		return Session{}, fmt.Errorf("session %s: unknown status %q", r.ID, r.Status)
	}
	for _, rr := range r.ResolvedChanges {
		rc, err := NewResolvedChange(ResolvedChangeParams{Number: rr.Number, Title: rr.Title, Author: rr.Author, WebURL: rr.WebURL, MergedAt: rr.MergedAt, Strategy: rr.Strategy, LandingSHAs: rr.LandingSHAs, SourceSHA: rr.SourceSHA})
		if err != nil {
			return Session{}, fmt.Errorf("session %s: %w", r.ID, err)
		}
		s.resolved = append(s.resolved, rc)
	}
	s.baseSHA = deref(r.BaseSHA)
	s.headSHA = deref(r.HeadSHA)
	s.status = r.Status
	s.stage = deref(r.Stage)
	s.err = r.Error.clone()
	if r.Totals != nil {
		t := *r.Totals
		s.totals = &t
	}
	s.files = append([]diff.FileSummary(nil), r.Files...)
	s.updatedAt = r.UpdatedAt
	s.expiresAt = r.ExpiresAt
	return s, nil
}
```

`apps/backend/internal/session/store.go`:

```go
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jtumidanski/converge/internal/workspace"
)

const recordFile = "session.json"

// Cleaner removes a session's git state and directory.
type Cleaner interface {
	Cleanup(ctx context.Context, s Session) error
	RemoveDir(ctx context.Context, id string) error
}

// Store persists sessions under WORKSPACE_ROOT with an in-memory index.
type Store struct {
	root    string
	ttl     time.Duration
	cleaner Cleaner
	log     *slog.Logger
	now     func() time.Time
	mu      sync.RWMutex
	index   map[string]Session
}

// NewStore creates a store; root must already exist.
func NewStore(root string, ttl time.Duration, cleaner Cleaner, log *slog.Logger, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{root: root, ttl: ttl, cleaner: cleaner, log: log, now: now, index: map[string]Session{}}
}

func (s *Store) Root() string        { return s.root }
func (s *Store) Dir(id string) string { return filepath.Join(s.root, id) }

// Save writes session.json atomically (temp + fsync + rename) and updates the index.
func (s *Store) Save(sess Session) error {
	dir := s.Dir(sess.ID())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("session: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(ToRecord(sess), "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, recordFile+".*.tmp")
	if err != nil {
		return fmt.Errorf("session: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("session: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("session: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("session: close: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, recordFile)); err != nil {
		return fmt.Errorf("session: rename: %w", err)
	}
	s.mu.Lock()
	s.index[sess.ID()] = sess
	s.mu.Unlock()
	return nil
}

// Get returns the indexed session.
func (s *Store) Get(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.index[id]
	return sess, ok
}

// List returns active sessions, newest first.
func (s *Store) List() []Session {
	s.mu.RLock()
	out := make([]Session, 0, len(s.index))
	for _, sess := range s.index {
		if sess.IsActive() {
			out = append(out, sess)
		}
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt().After(out[j].CreatedAt()) })
	return out
}

// Finish cleans up and marks FINISHED. Unknown or already finished sessions are no-ops.
func (s *Store) Finish(ctx context.Context, id string) error {
	sess, ok := s.Get(id)
	if !ok || sess.Status() == StatusFinished {
		return nil
	}
	if err := s.cleaner.Cleanup(ctx, sess); err != nil {
		return fmt.Errorf("session %s: cleanup: %w", id, err)
	}
	s.mu.Lock()
	s.index[id] = sess.Finished(s.now())
	s.mu.Unlock()
	return nil
}

// expire cleans up and marks EXPIRED (in memory only; the directory is gone).
func (s *Store) expire(ctx context.Context, sess Session) {
	if err := s.cleaner.Cleanup(ctx, sess); err != nil {
		s.log.Warn("expire cleanup failed", slog.String("session", sess.ID()), slog.String("error", err.Error()))
	}
	s.mu.Lock()
	s.index[sess.ID()] = sess.Expired(s.now())
	s.mu.Unlock()
}

// LoadAll implements FR-8.6 startup recovery.
func (s *Store) LoadAll(ctx context.Context) error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return fmt.Errorf("session: read root: %w", err)
	}
	now := s.now()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		raw, readErr := os.ReadFile(filepath.Join(s.root, id, recordFile))
		var rec Record
		if readErr == nil {
			readErr = json.Unmarshal(raw, &rec)
		}
		var sess Session
		if readErr == nil {
			sess, readErr = FromRecord(rec)
		}
		if readErr != nil {
			if workspace.ValidateSessionID(id) != nil {
				continue // not ours
			}
			info, statErr := e.Info()
			if statErr == nil && now.Sub(info.ModTime()) > s.ttl {
				s.log.Warn("removing invalid session directory", slog.String("session", id))
				if err := s.cleaner.RemoveDir(ctx, id); err != nil {
					s.log.Warn("remove failed", slog.String("session", id), slog.String("error", err.Error()))
				}
			}
			continue
		}
		if sess.Status() == StatusCreating {
			sess = sess.Failed(&ReviewError{Code: CodeInterrupted, Message: "The review was interrupted by a server restart before it finished building."}, now)
			if err := s.Save(sess); err != nil {
				return err
			}
		}
		s.mu.Lock()
		s.index[id] = sess
		s.mu.Unlock()
	}
	s.Sweep(ctx)
	return nil
}
```

`apps/backend/internal/session/sweep.go`:

```go
package session

import (
	"context"
	"log/slog"
	"time"
)

// Sweep expires and cleans every active session past its TTL.
func (s *Store) Sweep(ctx context.Context) {
	now := s.now()
	s.mu.RLock()
	var due []Session
	for _, sess := range s.index {
		if sess.IsActive() && sess.IsExpired(now) {
			due = append(due, sess)
		}
	}
	s.mu.RUnlock()
	for _, sess := range due {
		s.log.Info("expiring session", slog.String("session", sess.ID()))
		s.expire(ctx, sess)
	}
}

// RunSweeper sweeps every interval until ctx is cancelled.
func (s *Store) RunSweeper(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}
```

- [ ] **Step 5: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/session/ && go tool golangci-lint run ./internal/session/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/session && git commit -m "feat(task-001): session model, review errors, atomic store, recovery and sweep"
```

---

### Task 12: Review input validation, human messages, and landing-commit resolution

**Files:**
- Create: `apps/backend/internal/review/input.go`, `messages.go`, `landing.go`, `landing_test.go`, `input_test.go`

**Interfaces:**
- Consumes: `provider.ChangeRequest`, `provider.Commit`, `mirror.ObjectReader`, `session.Strategy`, `session.ReviewError`, `session.Code`.
- Produces:
  - `type CreateInput struct{ ProviderID, Repository, BaseBranch string; Changes []int }`; `func (in CreateInput) Validate(ctx context.Context, r gitx.Runner) (CreateInput, error)` returning a normalised copy (sorted de-duplicated changes) or a `*InputError`.
  - `type InputError struct{ Code session.Code; Field, Message string }` implementing `error`; codes `INVALID_PROVIDER`, `INVALID_REPOSITORY`, `INVALID_BRANCH`, `INVALID_CHANGES` exposed as `session.Code` values declared here: `CodeInvalidProvider`, `CodeInvalidRepository`, `CodeInvalidBranch`, `CodeInvalidChanges`.
  - Message helpers in `messages.go`: `MsgNotMerged(numbers []int) string`, `MsgIncompatibleTargets(pairs []TargetMismatch) string`, `MsgNotOnBaseBranch(number int, branch string) string`, `MsgMissingCommits(shas []string) string`, `MsgBaseUndetermined(reason string) string`, `MsgConflict(number int, dependency bool) string`, `MsgProviderAuth(providerID string) string`, `MsgProviderUnavailable(providerID string) string`, `MsgRepositoryUnavailable(repo string) string`, `MsgGitFailure() string`, `MsgInterrupted() string`; `type TargetMismatch struct{ Number int; Target string }`.
  - `type Landing struct{ Strategy session.Strategy; SHAs []string; SourceSHA string }`.
  - `func ResolveLanding(ctx context.Context, o mirror.ObjectReader, cr provider.ChangeRequest, commits []provider.Commit) (Landing, error)` implementing design §6.2. Returns `*session.ReviewError` with `CodeBaseUndetermined` or `CodeMissingCommits` on failure.
  - `var ErrNoCandidate = errors.New("no landing candidate exists in the mirror")`.

- [ ] **Step 1: Write the failing input validation test**

`apps/backend/internal/review/input_test.go`:

```go
package review

import (
	"context"
	"errors"
	"testing"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/session"
)

func TestCreateInputValidate(t *testing.T) {
	ctx := context.Background()
	runner := &gitx.FakeRunner{}
	good := CreateInput{ProviderID: "gh", Repository: "atlas/server", BaseBranch: "main", Changes: []int{427, 421, 421}}
	if _, err := good.Validate(ctx, runner); err == nil {
		t.Fatal("duplicate change numbers must be rejected")
	}
	good.Changes = []int{427, 421}
	out, err := good.Validate(ctx, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Changes) != 2 || out.Changes[0] != 421 || out.Changes[1] != 427 {
		t.Errorf("changes not normalised: %v", out.Changes)
	}
	cases := []struct {
		name string
		in   CreateInput
		code session.Code
	}{
		{"provider", CreateInput{ProviderID: "", Repository: "a/b", BaseBranch: "main", Changes: []int{1}}, CodeInvalidProvider},
		{"repository", CreateInput{ProviderID: "gh", Repository: "../x", BaseBranch: "main", Changes: []int{1}}, CodeInvalidRepository},
		{"branch", CreateInput{ProviderID: "gh", Repository: "a/b", BaseBranch: "-x", Changes: []int{1}}, CodeInvalidBranch},
		{"changes empty", CreateInput{ProviderID: "gh", Repository: "a/b", BaseBranch: "main"}, CodeInvalidChanges},
		{"changes zero", CreateInput{ProviderID: "gh", Repository: "a/b", BaseBranch: "main", Changes: []int{0}}, CodeInvalidChanges},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.in.Validate(ctx, runner)
			var ie *InputError
			if !errors.As(err, &ie) || ie.Code != tc.code {
				t.Fatalf("err = %v, want code %s", err, tc.code)
			}
		})
	}
	// an empty base branch is allowed here; the service fills the repository default
	if _, err := (CreateInput{ProviderID: "gh", Repository: "a/b", Changes: []int{1}}).Validate(ctx, runner); err != nil {
		t.Fatalf("empty base branch must be allowed: %v", err)
	}
}
```

- [ ] **Step 2: Write the failing landing resolution test**

`apps/backend/internal/review/landing_test.go`:

```go
package review

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/session"
)

func sha(c string) string { return strings.Repeat(c, 40) }

// fakeObjects scripts an ObjectReader.
type fakeObjects struct {
	exists   map[string]bool
	parents  map[string][]string
	patchIDs map[string]string
	walks    map[string][]string
}

func (f *fakeObjects) Exists(_ context.Context, s string) (bool, error) { return f.exists[s], nil }
func (f *fakeObjects) Parents(_ context.Context, s string) ([]string, error) {
	p, ok := f.parents[s]
	if !ok {
		return nil, errors.New("unknown commit " + s)
	}
	return p, nil
}
func (f *fakeObjects) PatchID(_ context.Context, s string) (string, error) { return f.patchIDs[s], nil }
func (f *fakeObjects) FirstParentWalk(_ context.Context, s string, n int) ([]string, error) {
	w := f.walks[s]
	if len(w) > n {
		w = w[len(w)-n:]
	}
	return w, nil
}
func (f *fakeObjects) IsAncestor(context.Context, string, string) (bool, error) { return true, nil }
func (f *fakeObjects) RevParse(context.Context, string) (string, error)         { return "", nil }
func (f *fakeObjects) BranchExists(context.Context, string) (bool, error)       { return true, nil }

var _ mirror.ObjectReader = (*fakeObjects)(nil)

func change(t *testing.T, merge, squash, head string, count int) provider.ChangeRequest {
	t.Helper()
	repo, err := provider.NewRepositoryBuilder().SetProviderID("p").SetFullName("a/b").SetDefaultBranch("main").Build()
	if err != nil {
		t.Fatal(err)
	}
	b := provider.NewChangeRequestBuilder().SetProviderID("p").SetRepository(repo).SetNumber(7).SetTitle("t").
		SetTargetBranch("main").SetState(provider.StateMerged).SetCommitCount(count)
	if merge != "" {
		b.SetMergeCommitSHA(merge)
	}
	if squash != "" {
		b.SetSquashCommitSHA(squash)
	}
	if head != "" {
		b.SetHeadSHA(head)
	}
	cr, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return cr
}

func commits(t *testing.T, shas ...string) []provider.Commit {
	t.Helper()
	out := make([]provider.Commit, 0, len(shas))
	for _, s := range shas {
		c, err := provider.NewCommit(s, "m", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func TestResolveLandingMergeCommit(t *testing.T) {
	m := sha("e")
	o := &fakeObjects{exists: map[string]bool{m: true}, parents: map[string][]string{m: {sha("0"), sha("a")}}}
	got, err := ResolveLanding(context.Background(), o, change(t, m, "", sha("a"), 2), commits(t, sha("1"), sha("a")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategyMerge || len(got.SHAs) != 1 || got.SHAs[0] != m || got.SourceSHA != m {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingSquashSingleCommit(t *testing.T) {
	s := sha("e")
	o := &fakeObjects{
		exists:   map[string]bool{s: true},
		parents:  map[string][]string{s: {sha("0")}},
		patchIDs: map[string]string{s: "pid-x", sha("1"): "pid-x"},
		walks:    map[string][]string{s: {s}},
	}
	got, err := ResolveLanding(context.Background(), o, change(t, "", s, sha("1"), 1), commits(t, sha("1")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategySquash || len(got.SHAs) != 1 || got.SHAs[0] != s {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingSquashOfManyCommits(t *testing.T) {
	// patch ids do not match the originals and the squash commit is non-empty
	s := sha("e")
	o := &fakeObjects{
		exists:   map[string]bool{s: true},
		parents:  map[string][]string{s: {sha("0")}, sha("d"): {sha("c")}},
		patchIDs: map[string]string{s: "pid-squash", sha("1"): "pid-1", sha("2"): "pid-2", sha("d"): "pid-d"},
		walks:    map[string][]string{s: {sha("d"), s}},
	}
	got, err := ResolveLanding(context.Background(), o, change(t, s, "", sha("2"), 2), commits(t, sha("1"), sha("2")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategySquash || len(got.SHAs) != 1 || got.SHAs[0] != s {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingRebase(t *testing.T) {
	last := sha("d")
	first := sha("c")
	o := &fakeObjects{
		exists:   map[string]bool{last: true},
		parents:  map[string][]string{last: {first}},
		patchIDs: map[string]string{first: "pid-1", last: "pid-2", sha("1"): "pid-1", sha("2"): "pid-2"},
		walks:    map[string][]string{last: {first, last}},
	}
	got, err := ResolveLanding(context.Background(), o, change(t, last, "", sha("2"), 2), commits(t, sha("1"), sha("2")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategyRebase || len(got.SHAs) != 2 || got.SHAs[0] != first || got.SHAs[1] != last {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingGitLabFastForwardFallsThroughToHead(t *testing.T) {
	head := sha("a")
	o := &fakeObjects{
		exists:   map[string]bool{head: true},
		parents:  map[string][]string{head: {sha("0")}},
		patchIDs: map[string]string{head: "pid-1", sha("1"): "pid-1"},
		walks:    map[string][]string{head: {head}},
	}
	// merge_commit_sha and squash_commit_sha are null: only HeadSHA is set
	got, err := ResolveLanding(context.Background(), o, change(t, "", "", head, 1), commits(t, sha("1")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategySquash || got.SHAs[0] != head {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingSkipsAbsentCandidate(t *testing.T) {
	missing, head := sha("f"), sha("a")
	o := &fakeObjects{
		exists:   map[string]bool{head: true},
		parents:  map[string][]string{head: {sha("0")}},
		patchIDs: map[string]string{head: "pid-1", sha("1"): "pid-1"},
		walks:    map[string][]string{head: {head}},
	}
	got, err := ResolveLanding(context.Background(), o, change(t, missing, "", head, 1), commits(t, sha("1")))
	if err != nil {
		t.Fatal(err)
	}
	if got.SHAs[0] != head {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingErrors(t *testing.T) {
	// no candidate present at all -> MISSING_COMMITS naming the shas
	missing := sha("f")
	o := &fakeObjects{exists: map[string]bool{}}
	_, err := ResolveLanding(context.Background(), o, change(t, missing, "", "", 1), commits(t, sha("1")))
	var re *session.ReviewError
	if !errors.As(err, &re) || re.Code != session.CodeMissingCommits || !strings.Contains(re.Message, missing[:7]) {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("missing candidate must wrap ErrNoCandidate: %v", err)
	}
	// root commit (no parents) -> BASE_UNDETERMINED
	root := sha("b")
	o2 := &fakeObjects{exists: map[string]bool{root: true}, parents: map[string][]string{root: {}}}
	_, err = ResolveLanding(context.Background(), o2, change(t, root, "", "", 1), commits(t, sha("1")))
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("root err = %v", err)
	}
	// octopus merge (3 parents) -> BASE_UNDETERMINED
	oct := sha("c")
	o3 := &fakeObjects{exists: map[string]bool{oct: true}, parents: map[string][]string{oct: {sha("0"), sha("1"), sha("2")}}}
	_, err = ResolveLanding(context.Background(), o3, change(t, oct, "", "", 1), commits(t, sha("1")))
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("octopus err = %v", err)
	}
	// multi-commit candidate whose diff is empty and whose patch ids do not match
	empty := sha("d")
	o4 := &fakeObjects{
		exists:   map[string]bool{empty: true},
		parents:  map[string][]string{empty: {sha("0")}},
		patchIDs: map[string]string{empty: "", sha("1"): "pid-1", sha("2"): "pid-2"},
		walks:    map[string][]string{empty: {empty}},
	}
	_, err = ResolveLanding(context.Background(), o4, change(t, empty, "", "", 2), commits(t, sha("1"), sha("2")))
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("empty-diff err = %v", err)
	}
}
```

- [ ] **Step 3: Run to see failures**

Run: `cd <worktree-root>/apps/backend && go test ./internal/review/`
Expected: compile failure (`CreateInput`, `ResolveLanding` undefined).

- [ ] **Step 4: Implement input, messages, landing**

`apps/backend/internal/review/input.go`:

```go
// Package review orchestrates reconstruction: resolve, apply, diff.
package review

import (
	"context"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/session"
)

// Request validation codes (PRD §5.4).
const (
	CodeInvalidProvider   session.Code = "INVALID_PROVIDER"
	CodeInvalidRepository session.Code = "INVALID_REPOSITORY"
	CodeInvalidBranch     session.Code = "INVALID_BRANCH"
	CodeInvalidChanges    session.Code = "INVALID_CHANGES"
)

// InputError is a client-fixable validation failure.
type InputError struct {
	Code    session.Code
	Field   string
	Message string
}

func (e *InputError) Error() string { return string(e.Code) + ": " + e.Message }

// CreateInput is a review build request.
type CreateInput struct {
	ProviderID string
	Repository string
	BaseBranch string
	Changes    []int
}

// Validate normalises and checks the request. An empty BaseBranch is allowed;
// the service substitutes the repository default before use.
func (in CreateInput) Validate(ctx context.Context, r gitx.Runner) (CreateInput, error) {
	if in.ProviderID == "" {
		return CreateInput{}, &InputError{Code: CodeInvalidProvider, Field: "provider", Message: "A provider must be selected."}
	}
	if err := gitx.ValidateRepoFullName(in.Repository); err != nil {
		return CreateInput{}, &InputError{Code: CodeInvalidRepository, Field: "repository", Message: "The repository must look like owner/name."}
	}
	if in.BaseBranch != "" {
		if err := gitx.ValidateBranch(ctx, r, in.BaseBranch); err != nil {
			return CreateInput{}, &InputError{Code: CodeInvalidBranch, Field: "baseBranch", Message: "The base branch name is not a valid git branch name."}
		}
	}
	changes, err := gitx.ValidateChangeNumbers(in.Changes)
	if err != nil {
		return CreateInput{}, &InputError{Code: CodeInvalidChanges, Field: "changes", Message: "Select between 1 and 50 distinct PRs/MRs."}
	}
	in.Changes = changes
	return in, nil
}
```

`apps/backend/internal/review/messages.go`:

```go
package review

import (
	"fmt"
	"strings"
)

// TargetMismatch is one change whose target branch differs from the requested base.
type TargetMismatch struct {
	Number int
	Target string
}

func joinNumbers(numbers []int) string {
	parts := make([]string, len(numbers))
	for i, n := range numbers {
		parts[i] = fmt.Sprintf("#%d", n)
	}
	return strings.Join(parts, ", ")
}

// MsgNotMerged reports unmerged selections.
func MsgNotMerged(numbers []int) string {
	return fmt.Sprintf("%s is not merged yet. Converge can only reconstruct merged PRs/MRs.", joinNumbers(numbers))
}

// MsgIncompatibleTargets reports selections that targeted a different branch.
func MsgIncompatibleTargets(pairs []TargetMismatch) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = fmt.Sprintf("#%d targets %s", p.Number, p.Target)
	}
	return "All selected PRs/MRs must target the same base branch: " + strings.Join(parts, ", ") + "."
}

// MsgNotOnBaseBranch reports a change whose landing commit is not on the base branch.
func MsgNotOnBaseBranch(number int, branch string) string {
	return fmt.Sprintf("#%d does not appear on %s. It may have been reverted or the branch was rewritten.", number, branch)
}

// MsgMissingCommits reports commits absent from the repository.
func MsgMissingCommits(shas []string) string {
	short := make([]string, len(shas))
	for i, s := range shas {
		if len(s) > 7 {
			s = s[:7]
		}
		short[i] = s
	}
	return "Some commits are no longer available in the repository: " + strings.Join(short, ", ") + "."
}

// MsgBaseUndetermined reports that the base could not be computed.
func MsgBaseUndetermined(reason string) string {
	return "Converge could not determine a base for this selection: " + reason + "."
}

// MsgConflict reports a conflict, optionally flagging a likely dependency.
func MsgConflict(number int, dependency bool) string {
	msg := fmt.Sprintf("#%d conflicts while being applied.", number)
	if dependency {
		msg += " It may depend on work that is not part of this review."
	}
	return msg
}

// MsgProviderAuth reports rejected credentials.
func MsgProviderAuth(providerID string) string {
	return fmt.Sprintf("The token configured for provider %q was rejected.", providerID)
}

// MsgProviderUnavailable reports an unreachable provider.
func MsgProviderUnavailable(providerID string) string {
	return fmt.Sprintf("Provider %q is unavailable right now. Try again shortly.", providerID)
}

// MsgRepositoryUnavailable reports a repository that could not be read or cloned.
func MsgRepositoryUnavailable(repo string) string {
	return fmt.Sprintf("The repository %s could not be read.", repo)
}

// MsgGitFailure reports an unexpected git failure.
func MsgGitFailure() string {
	return "A git command failed while building the review. See Diagnostics for details."
}

// MsgInterrupted reports a build cut short by a restart.
func MsgInterrupted() string {
	return "The review was interrupted by a server restart before it finished building."
}
```

`apps/backend/internal/review/landing.go`:

```go
package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/session"
)

// ErrNoCandidate reports that no candidate SHA exists in the mirror.
var ErrNoCandidate = errors.New("no landing candidate exists in the mirror")

// Landing is how one change landed on the target branch.
type Landing struct {
	Strategy  session.Strategy
	SHAs      []string // oldest first
	SourceSHA string   // the provider-reported candidate that was used
}

func undetermined(number int, reason string) error {
	return &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(fmt.Sprintf("#%d: %s", number, reason)), Change: number}
}

// ResolveLanding walks the candidate chain (merge, squash, head) and classifies
// the first candidate present in the mirror. Design §6.2.
func ResolveLanding(ctx context.Context, o mirror.ObjectReader, cr provider.ChangeRequest, commits []provider.Commit) (Landing, error) {
	candidates := cr.LandingCandidates()
	if len(candidates) == 0 {
		return Landing{}, &session.ReviewError{
			Code:    session.CodeBaseUndetermined,
			Message: MsgBaseUndetermined(fmt.Sprintf("#%d: the provider reported no merge, squash or head commit", cr.Number())),
			Change:  cr.Number(),
		}
	}
	var found string
	for _, c := range candidates {
		ok, err := o.Exists(ctx, c)
		if err != nil {
			return Landing{}, err
		}
		if ok {
			found = c
			break
		}
	}
	if found == "" {
		return Landing{}, fmt.Errorf("%w: %s", ErrNoCandidate, &session.ReviewError{
			Code:    session.CodeMissingCommits,
			Message: MsgMissingCommits(candidates),
			Change:  cr.Number(),
		})
	}
	parents, err := o.Parents(ctx, found)
	if err != nil {
		return Landing{}, err
	}
	switch len(parents) {
	case 2:
		return Landing{Strategy: session.StrategyMerge, SHAs: []string{found}, SourceSHA: found}, nil
	case 1:
		return resolveSingleParent(ctx, o, cr, commits, found)
	default:
		return Landing{}, undetermined(cr.Number(), fmt.Sprintf("commit %s has %d parents", found[:7], len(parents)))
	}
}

func resolveSingleParent(ctx context.Context, o mirror.ObjectReader, cr provider.ChangeRequest, commits []provider.Commit, found string) (Landing, error) {
	n := len(commits)
	if n == 0 {
		n = cr.CommitCount()
	}
	if n <= 0 {
		n = 1
	}
	walk, err := o.FirstParentWalk(ctx, found, n)
	if err != nil {
		return Landing{}, err
	}
	if len(walk) == n && len(commits) == n {
		same, err := patchIDsMatch(ctx, o, walk, commits)
		if err != nil {
			return Landing{}, err
		}
		if same {
			if n > 1 {
				return Landing{Strategy: session.StrategyRebase, SHAs: walk, SourceSHA: found}, nil
			}
			return Landing{Strategy: session.StrategySquash, SHAs: []string{found}, SourceSHA: found}, nil
		}
	}
	if n == 1 {
		return Landing{Strategy: session.StrategySquash, SHAs: []string{found}, SourceSHA: found}, nil
	}
	// A squash of several commits: accept only when the candidate has a non-empty diff.
	pid, err := o.PatchID(ctx, found)
	if err != nil {
		return Landing{}, err
	}
	if pid == "" {
		return Landing{}, undetermined(cr.Number(), fmt.Sprintf("commit %s carries no changes and does not match the reported commits", found[:7]))
	}
	return Landing{Strategy: session.StrategySquash, SHAs: []string{found}, SourceSHA: found}, nil
}

// patchIDsMatch compares the walked commits against the provider's commits as multisets.
func patchIDsMatch(ctx context.Context, o mirror.ObjectReader, walk []string, commits []provider.Commit) (bool, error) {
	counts := map[string]int{}
	for _, s := range walk {
		id, err := o.PatchID(ctx, s)
		if err != nil {
			return false, err
		}
		if id == "" {
			return false, nil
		}
		counts[id]++
	}
	for _, c := range commits {
		id, err := o.PatchID(ctx, c.SHA())
		if err != nil {
			return false, nil // the original commit may be gone after a rebase
		}
		if id == "" {
			return false, nil
		}
		counts[id]--
		if counts[id] < 0 {
			return false, nil
		}
	}
	for _, v := range counts {
		if v != 0 {
			return false, nil
		}
	}
	return true, nil
}
```

Note on the `MISSING_COMMITS` error: `ResolveLanding` returns a wrapped error so callers can both `errors.Is(err, ErrNoCandidate)` (to trigger the one-shot `FetchSHA` retry in Task 13) and `errors.As(err, &*session.ReviewError)`. Implement the wrapper as a dedicated type rather than `fmt.Errorf` with `%s`:

```go
// noCandidateError carries the review error and unwraps to both sentinels.
type noCandidateError struct{ re *session.ReviewError }

func (e *noCandidateError) Error() string   { return e.re.Error() }
func (e *noCandidateError) Unwrap() []error { return []error{ErrNoCandidate, e.re} }
```

Replace the `fmt.Errorf("%w: %s", ErrNoCandidate, ...)` return with:

```go
		return Landing{}, &noCandidateError{re: &session.ReviewError{
			Code:    session.CodeMissingCommits,
			Message: MsgMissingCommits(candidates),
			Change:  cr.Number(),
		}}
```

`errors.As(err, &re)` finds the `*session.ReviewError` through `Unwrap() []error` (Go 1.20+ multi-error unwrap), and `errors.Is(err, ErrNoCandidate)` is true.

- [ ] **Step 5: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/review/ && go tool golangci-lint run ./internal/review/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/review && git commit -m "feat(task-001): review input validation, messages and landing-commit resolution"
```

---

### Task 13: Resolve pipeline (base selection)

**Files:**
- Create: `apps/backend/internal/review/resolve.go`, `resolve_test.go`

**Interfaces:**
- Consumes: `provider.GitProvider`, `mirror.Cache`, `mirror.ObjectReader`, `ResolveLanding`, `session.*`.
- Produces:
  - `type Resolved struct{ Changes []session.ResolvedChange; BaseSHA string; MirrorPath string }`.
  - `type Resolver struct{ mirrors *mirror.Cache; log *slog.Logger }`; `func NewResolver(m *mirror.Cache, log *slog.Logger) *Resolver`.
  - `func (r *Resolver) Resolve(ctx context.Context, p provider.GitProvider, repo provider.Repository, baseBranch string, numbers []int, progress func(stage string)) (Resolved, error)` implementing design §6.1 steps 1-7. Errors are `*session.ReviewError`.
  - `const resolveConcurrency = 4`.

- [ ] **Step 1: Write the failing resolve test**

`apps/backend/internal/review/resolve_test.go`:

```go
package review

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
)

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// fixture builds a repository with three merged changes on main plus unrelated commits.
type resolveFixture struct {
	repo     provider.Repository
	prov     *fake.Provider
	resolver *Resolver
	runner   *gitx.ExecRunner
	src      *testutil.Repo
	mergeSHA string
	baseSHA  string
}

func newResolveFixture(t *testing.T) *resolveFixture {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Commit("unrelated.txt", "u1\n", "unrelated 1")
	baseSHA := src.Head() // parent of the first landing commit

	src.Branch("feat/a")
	src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	mergeSHA := src.MergeNoFF("feat/a", "Merge #1")

	src.Commit("unrelated.txt", "u2\n", "unrelated 2")

	src.Branch("feat/b")
	src.Commit("b.txt", "b\n", "b1")
	src.Checkout("main")
	squashSHA := src.Squash("feat/b", "Squash #2")
	src.Push()

	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	cache := mirror.New(t.TempDir(), runner, &gitx.LockMap{}, testLog())

	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)
	t0 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	add := func(number int, merge, squash string, mergedAt time.Time, commits []provider.Commit) {
		b := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(number).SetTitle("change").
			SetAuthor("dev").SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(mergedAt).SetCommits(commits)
		if merge != "" {
			b.SetMergeCommitSHA(merge)
		}
		if squash != "" {
			b.SetSquashCommitSHA(squash)
		}
		cr, err := b.Build()
		if err != nil {
			t.Fatal(err)
		}
		p.AddChange(cr)
	}
	c1, _ := provider.NewCommit(src.RevParse("feat/a"), "a1", t0)
	c2, _ := provider.NewCommit(src.RevParse("feat/b"), "b1", t0)
	add(1, mergeSHA, "", t0.Add(time.Hour), []provider.Commit{c1})
	add(2, "", squashSHA, t0.Add(2*time.Hour), []provider.Commit{c2})

	return &resolveFixture{repo: repo, prov: p, resolver: NewResolver(cache, testLog()), runner: runner, src: src, mergeSHA: mergeSHA, baseSHA: baseSHA}
}

func TestResolveOrdersAndComputesBase(t *testing.T) {
	f := newResolveFixture(t)
	var stages []string
	got, err := f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{2, 1}, func(s string) { stages = append(stages, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Changes) != 2 || got.Changes[0].Number() != 1 || got.Changes[1].Number() != 2 {
		t.Fatalf("order = %+v", got.Changes)
	}
	if got.Changes[0].Strategy() != session.StrategyMerge || got.Changes[1].Strategy() != session.StrategySquash {
		t.Errorf("strategies = %s %s", got.Changes[0].Strategy(), got.Changes[1].Strategy())
	}
	if got.BaseSHA != f.baseSHA {
		t.Errorf("base = %s, want parent of first landing commit %s", got.BaseSHA, f.baseSHA)
	}
	if len(stages) < 2 || stages[0] != session.StageResolving || stages[1] != session.StageUpdatingRepo {
		t.Errorf("stages = %v", stages)
	}
}

func TestResolveRejectsUnmergedAndBadTargets(t *testing.T) {
	f := newResolveFixture(t)
	open, _ := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(f.repo).SetNumber(3).SetTitle("open").
		SetTargetBranch("main").SetState(provider.StateOpen).Build()
	f.prov.AddChange(open)
	other, _ := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(f.repo).SetNumber(4).SetTitle("other").
		SetTargetBranch("develop").SetState(provider.StateMerged).SetMergedAt(time.Now()).SetMergeCommitSHA(f.mergeSHA).Build()
	f.prov.AddChange(other)

	var re *session.ReviewError
	_, err := f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{1, 3}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeNotMerged || re.Change != 3 {
		t.Fatalf("not merged: %v", err)
	}
	_, err = f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{1, 4}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeIncompatibleTargets {
		t.Fatalf("targets: %v", err)
	}
	_, err = f.resolver.Resolve(context.Background(), f.prov, f.repo, "nonexistent-branch", []int{1}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("missing branch: %v", err)
	}
	f.prov.FailWith(provider.ErrAuth)
	_, err = f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{1}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeProviderAuth {
		t.Fatalf("auth: %v", err)
	}
}

func TestResolveRejectsChangeNotOnBaseBranch(t *testing.T) {
	f := newResolveFixture(t)
	// a merged change whose landing commit lives only on a side branch
	f.src.Branch("side")
	side := f.src.Commit("side.txt", "s\n", "side commit")
	f.src.Checkout("main")
	f.src.Push()
	c, _ := provider.NewCommit(side, "side", time.Now())
	cr, _ := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(f.repo).SetNumber(9).SetTitle("side").
		SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(time.Now()).SetMergeCommitSHA(side).SetCommits([]provider.Commit{c}).Build()
	f.prov.AddChange(cr)
	var re *session.ReviewError
	_, err := f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{9}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeNotOnBaseBranch || re.Change != 9 {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run to see failure**

Run: `cd <worktree-root>/apps/backend && go test ./internal/review/ -run Resolve`
Expected: FAIL (`NewResolver` undefined).

- [ ] **Step 3: Implement the resolver**

`apps/backend/internal/review/resolve.go`:

```go
package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/session"
)

const resolveConcurrency = 4

// Resolved is the outcome of the resolve pipeline.
type Resolved struct {
	Changes    []session.ResolvedChange
	BaseSHA    string
	MirrorPath string
}

// Resolver turns selected change numbers into landing commits and a base SHA.
type Resolver struct {
	mirrors *mirror.Cache
	log     *slog.Logger
}

// NewResolver builds a Resolver.
func NewResolver(m *mirror.Cache, log *slog.Logger) *Resolver { return &Resolver{mirrors: m, log: log} }

// MapProviderError converts a provider error into a ReviewError.
func MapProviderError(providerID, repo string, err error) *session.ReviewError {
	switch {
	case errors.Is(err, provider.ErrAuth):
		return &session.ReviewError{Code: session.CodeProviderAuth, Message: MsgProviderAuth(providerID)}
	case errors.Is(err, provider.ErrNotFound):
		return &session.ReviewError{Code: session.CodeRepositoryUnavailable, Message: MsgRepositoryUnavailable(repo)}
	case errors.Is(err, provider.ErrUnavailable):
		return &session.ReviewError{Code: session.CodeProviderUnavailable, Message: MsgProviderUnavailable(providerID)}
	default:
		return nil
	}
}

func asReviewError(err error) *session.ReviewError {
	var re *session.ReviewError
	if errors.As(err, &re) {
		return re
	}
	return nil
}

// Resolve implements design §6.1.
func (r *Resolver) Resolve(ctx context.Context, p provider.GitProvider, repo provider.Repository, baseBranch string, numbers []int, progress func(string)) (Resolved, error) {
	report := func(stage string) {
		if progress != nil {
			progress(stage)
		}
	}
	report(session.StageResolving)

	changes, err := r.fetchChanges(ctx, p, repo, numbers)
	if err != nil {
		return Resolved{}, err
	}
	if err := checkMergedAndTargets(changes, baseBranch); err != nil {
		return Resolved{}, err
	}
	sort.SliceStable(changes, func(i, j int) bool {
		if !changes[i].MergedAt().Equal(changes[j].MergedAt()) {
			return changes[i].MergedAt().Before(changes[j].MergedAt())
		}
		return changes[i].Number() < changes[j].Number()
	})

	report(session.StageUpdatingRepo)
	mirrorPath, err := r.mirrors.Ensure(ctx, p, repo)
	if err != nil {
		if re := MapProviderError(p.ID(), repo.FullName(), err); re != nil {
			return Resolved{}, re
		}
		return Resolved{}, &session.ReviewError{Code: session.CodeRepositoryUnavailable, Message: MsgRepositoryUnavailable(repo.FullName())}
	}
	objects := r.mirrors.Objects(mirrorPath, repo.FullName())
	ok, err := objects.BranchExists(ctx, baseBranch)
	if err != nil {
		return Resolved{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}
	if !ok {
		return Resolved{}, &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(fmt.Sprintf("branch %s is not present in the repository", baseBranch))}
	}

	resolved := make([]session.ResolvedChange, 0, len(changes))
	for _, cr := range changes {
		commits, err := p.GetChangeCommits(ctx, repo, cr.Number())
		if err != nil {
			if errors.Is(err, provider.ErrTooManyCommits) {
				return Resolved{}, &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(fmt.Sprintf("#%d has too many commits to verify", cr.Number())), Change: cr.Number()}
			}
			if re := MapProviderError(p.ID(), repo.FullName(), err); re != nil {
				return Resolved{}, re
			}
			return Resolved{}, &session.ReviewError{Code: session.CodeProviderUnavailable, Message: MsgProviderUnavailable(p.ID())}
		}
		landing, err := r.landingWithFetch(ctx, p, repo, objects, cr, commits)
		if err != nil {
			if re := asReviewError(err); re != nil {
				return Resolved{}, re
			}
			return Resolved{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure(), Change: cr.Number()}
		}
		for _, sha := range landing.SHAs {
			onBase, err := objects.IsAncestor(ctx, sha, baseBranch)
			if err != nil {
				return Resolved{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure(), Change: cr.Number()}
			}
			if !onBase {
				return Resolved{}, &session.ReviewError{Code: session.CodeNotOnBaseBranch, Message: MsgNotOnBaseBranch(cr.Number(), baseBranch), Change: cr.Number(), Commit: sha}
			}
		}
		rc, err := session.NewResolvedChange(session.ResolvedChangeParams{
			Number: cr.Number(), Title: cr.Title(), Author: cr.Author(), WebURL: cr.WebURL(), MergedAt: cr.MergedAt(),
			Strategy: landing.Strategy, LandingSHAs: landing.SHAs, SourceSHA: landing.SourceSHA,
		})
		if err != nil {
			return Resolved{}, &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(err.Error()), Change: cr.Number()}
		}
		resolved = append(resolved, rc)
	}

	first := resolved[0].LandingSHAs()[0]
	baseSHA, err := objects.RevParse(ctx, first+"^1")
	if err != nil {
		return Resolved{}, &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(fmt.Sprintf("commit %s has no first parent", first[:7])), Change: resolved[0].Number()}
	}
	return Resolved{Changes: resolved, BaseSHA: baseSHA, MirrorPath: mirrorPath}, nil
}

// landingWithFetch resolves the landing commits, retrying once after fetching missing SHAs (FR-5.8).
func (r *Resolver) landingWithFetch(ctx context.Context, p provider.GitProvider, repo provider.Repository, o mirror.ObjectReader, cr provider.ChangeRequest, commits []provider.Commit) (Landing, error) {
	landing, err := ResolveLanding(ctx, o, cr, commits)
	if err == nil || !errors.Is(err, ErrNoCandidate) {
		return landing, err
	}
	for _, sha := range cr.LandingCandidates() {
		if fetchErr := r.mirrors.FetchSHA(ctx, p, repo, sha); fetchErr != nil {
			r.log.Debug("fetch-by-sha failed", slog.String("repository", repo.FullName()), slog.String("sha", sha[:7]))
			continue
		}
		break
	}
	return ResolveLanding(ctx, o, cr, commits)
}

// fetchChanges loads every selected change with bounded concurrency.
func (r *Resolver) fetchChanges(ctx context.Context, p provider.GitProvider, repo provider.Repository, numbers []int) ([]provider.ChangeRequest, error) {
	out := make([]provider.ChangeRequest, len(numbers))
	errs := make([]error, len(numbers))
	sem := make(chan struct{}, resolveConcurrency)
	var wg sync.WaitGroup
	for i, n := range numbers {
		wg.Add(1)
		go func(i, n int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cr, err := p.GetChange(ctx, repo, n)
			out[i], errs[i] = cr, err
		}(i, n)
	}
	wg.Wait()
	for i, err := range errs {
		if err == nil {
			continue
		}
		if errors.Is(err, provider.ErrNotFound) {
			return nil, &session.ReviewError{Code: session.CodeRepositoryUnavailable, Message: fmt.Sprintf("#%d was not found in %s.", numbers[i], repo.FullName()), Change: numbers[i]}
		}
		if re := MapProviderError(p.ID(), repo.FullName(), err); re != nil {
			re.Change = numbers[i]
			return nil, re
		}
		return nil, &session.ReviewError{Code: session.CodeProviderUnavailable, Message: MsgProviderUnavailable(p.ID()), Change: numbers[i]}
	}
	return out, nil
}

func checkMergedAndTargets(changes []provider.ChangeRequest, baseBranch string) error {
	var unmerged []int
	for _, cr := range changes {
		if cr.State() != provider.StateMerged {
			unmerged = append(unmerged, cr.Number())
		}
	}
	if len(unmerged) > 0 {
		return &session.ReviewError{Code: session.CodeNotMerged, Message: MsgNotMerged(unmerged), Change: unmerged[0]}
	}
	var mismatches []TargetMismatch
	for _, cr := range changes {
		if cr.TargetBranch() != baseBranch {
			mismatches = append(mismatches, TargetMismatch{Number: cr.Number(), Target: cr.TargetBranch()})
		}
	}
	if len(mismatches) > 0 {
		return &session.ReviewError{Code: session.CodeIncompatibleTargets, Message: MsgIncompatibleTargets(mismatches), Change: mismatches[0].Number}
	}
	return nil
}
```

- [ ] **Step 4: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/review/ && go tool golangci-lint run ./internal/review/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/review && git commit -m "feat(task-001): resolve pipeline computing landing commits and base"
```

---

### Task 14: Cherry-pick applicator

**Files:**
- Create: `apps/backend/internal/review/apply.go`, `apply_test.go`

**Interfaces:**
- Produces:
  - `type Outcome string` with `OutcomeApplied="applied"`, `OutcomeEmpty="empty"`, `OutcomeConflict="conflict"`.
  - `type ApplyResult struct{ Outcome Outcome; ConflictingPaths []string; Commit string }`.
  - `type ChangeApplicator interface{ Apply(ctx context.Context, repoDir string, rc session.ResolvedChange) (ApplyResult, error) }`.
  - `type CherryPickApplicator struct{...}`; `func NewCherryPickApplicator(r gitx.Runner, log *slog.Logger) *CherryPickApplicator`.
  - Behaviour: `merge` → `cherry-pick -m 1 --empty=keep <sha>`; `squash` → `cherry-pick --empty=keep <sha>`; `rebase` → `cherry-pick --empty=keep <sha1> ... <shaN>`. On success the tip commit's message is rewritten to `<original subject>\n\nConverge-Change: #<number>\nConverge-Source: <source sha>` via `git commit --amend --allow-empty --no-verify -F -` (FR-6.5). The trailer omits the provider id because `session.ResolvedChange` does not carry one; the provider is recorded once in `session.json`. For a multi-commit rebase pick only the tip is amended, which is enough to map the group back to its PR/MR. On conflict: run `git diff --name-only --diff-filter=U -z`, record the paths, and leave the worktree conflicted.
  - `func (a *CherryPickApplicator) Abort(ctx context.Context, repoDir string) error` — `git cherry-pick --abort`, used only by cleanup paths.

- [ ] **Step 1: Write the failing applicator test**

`apps/backend/internal/review/apply_test.go`:

```go
package review

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
)

func newApplicator(t *testing.T) (*CherryPickApplicator, *gitx.ExecRunner) {
	t.Helper()
	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	return NewCherryPickApplicator(runner, testLog()), runner
}

func resolved(t *testing.T, number int, strategy session.Strategy, shas ...string) session.ResolvedChange {
	t.Helper()
	rc, err := session.NewResolvedChange(session.ResolvedChangeParams{Number: number, Title: "change", Author: "dev", MergedAt: time.Now(), Strategy: strategy, LandingSHAs: shas, SourceSHA: shas[0]})
	if err != nil {
		t.Fatal(err)
	}
	return rc
}

func TestApplyMergeSquashRebase(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()

	src.Branch("feat/a")
	src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	mergeSHA := src.MergeNoFF("feat/a", "merge a")

	src.Branch("feat/b")
	src.Commit("b.txt", "b\n", "b1")
	src.Commit("b.txt", "bb\n", "b2")
	src.Checkout("main")
	squashSHA := src.Squash("feat/b", "squash b")

	src.Branch("feat/c")
	src.Commit("c.txt", "c\n", "c1")
	src.Commit("c2.txt", "c2\n", "c2")
	src.Checkout("main")
	rebased := src.Rebase("feat/c")

	a, runner := newApplicator(t)
	ctx := context.Background()
	// work directly in a detached copy of the repo at base
	src.Git("checkout", "-b", "review/test", base)

	res, err := a.Apply(ctx, src.Work, resolved(t, 1, session.StrategyMerge, mergeSHA))
	if err != nil || res.Outcome != OutcomeApplied {
		t.Fatalf("merge: %v %+v", err, res)
	}
	if src.FileContent("HEAD", "a.txt") != "a\n" {
		t.Error("merge content missing")
	}
	msg := src.Git("log", "-1", "--format=%B")
	if !strings.Contains(msg, "Converge-Change: p#1"[2:]) || !strings.Contains(msg, "Converge-Source: "+mergeSHA) {
		t.Errorf("trailer missing: %q", msg)
	}

	res, err = a.Apply(ctx, src.Work, resolved(t, 2, session.StrategySquash, squashSHA))
	if err != nil || res.Outcome != OutcomeApplied || src.FileContent("HEAD", "b.txt") != "bb\n" {
		t.Fatalf("squash: %v %+v", err, res)
	}

	res, err = a.Apply(ctx, src.Work, resolved(t, 3, session.StrategyRebase, rebased...))
	if err != nil || res.Outcome != OutcomeApplied || src.FileContent("HEAD", "c.txt") != "c\n" || src.FileContent("HEAD", "c2.txt") != "c2\n" {
		t.Fatalf("rebase: %v %+v", err, res)
	}
	if n := len(strings.Split(strings.TrimSpace(src.Git("rev-list", base+"..HEAD")), "\n")); n != 4 {
		t.Errorf("commit count = %d, want 4", n)
	}
	_ = runner
}

func TestApplyEmptyPickIsNotAnError(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()
	src.Branch("feat/a")
	src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	first := src.Squash("feat/a", "squash a")
	// a second commit with identical content
	src.Branch("feat/a2")
	src.Commit("a.txt", "a\n", "a again")
	src.Checkout("main")
	second := src.Squash("feat/a2", "squash a again")

	a, _ := newApplicator(t)
	src.Git("checkout", "-b", "review/test", base)
	if res, err := a.Apply(context.Background(), src.Work, resolved(t, 1, session.StrategySquash, first)); err != nil || res.Outcome != OutcomeApplied {
		t.Fatalf("first: %v %+v", err, res)
	}
	res, err := a.Apply(context.Background(), src.Work, resolved(t, 2, session.StrategySquash, second))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.Outcome != OutcomeApplied && res.Outcome != OutcomeEmpty {
		t.Fatalf("empty pick must not fail: %+v", res)
	}
	if strings.TrimSpace(src.Git("status", "--porcelain")) != "" {
		t.Error("worktree dirty after empty pick")
	}
}

func TestApplyConflictReportsPaths(t *testing.T) {
	src := testutil.NewRepo(t)
	src.Commit("shared.txt", "original\n", "seed")
	base := src.Head()
	src.Branch("feat/a")
	src.Commit("shared.txt", "from a\n", "a")
	src.Checkout("main")
	aSHA := src.Squash("feat/a", "squash a")
	src.Branch("feat/b")
	src.Commit("shared.txt", "from b\n", "b")
	src.Checkout("main")
	bSHA := src.Squash("feat/b", "squash b")

	a, _ := newApplicator(t)
	src.Git("checkout", "-b", "review/test", base)
	// apply b first, then a: the same line conflicts
	if _, err := a.Apply(context.Background(), src.Work, resolved(t, 2, session.StrategySquash, bSHA)); err != nil {
		t.Fatal(err)
	}
	res, err := a.Apply(context.Background(), src.Work, resolved(t, 1, session.StrategySquash, aSHA))
	if err != nil {
		t.Fatalf("conflict must not be an error: %v", err)
	}
	if res.Outcome != OutcomeConflict || len(res.ConflictingPaths) != 1 || res.ConflictingPaths[0] != "shared.txt" {
		t.Fatalf("res = %+v", res)
	}
	if res.Commit != aSHA {
		t.Errorf("commit = %s, want %s", res.Commit, aSHA)
	}
	// the worktree is left conflicted for diagnostics
	if !strings.Contains(src.Git("status", "--porcelain"), "U") {
		t.Error("worktree should remain conflicted")
	}
	if err := a.Abort(context.Background(), src.Work); err != nil {
		t.Fatalf("abort: %v", err)
	}
}
```

Note: the trailer assertion uses the provider-agnostic substring `onverge-Change: p#1` only if the applicator knows the provider ID. It does not: `session.ResolvedChange` carries no provider. Use the trailer format `Converge-Change: #<number>` and assert exactly that. Fix the test line to:

```go
	if !strings.Contains(msg, "Converge-Change: #1") || !strings.Contains(msg, "Converge-Source: "+mergeSHA) {
```

- [ ] **Step 2: Run to see failure**

Run: `cd <worktree-root>/apps/backend && go test ./internal/review/ -run Apply`
Expected: FAIL (`NewCherryPickApplicator` undefined).

- [ ] **Step 3: Implement the applicator**

`apps/backend/internal/review/apply.go`:

```go
package review

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/session"
)

// Outcome is the result of applying one change.
type Outcome string

const (
	OutcomeApplied  Outcome = "applied"
	OutcomeEmpty    Outcome = "empty"
	OutcomeConflict Outcome = "conflict"
)

// ApplyResult reports what happened while applying a change.
type ApplyResult struct {
	Outcome          Outcome
	ConflictingPaths []string
	Commit           string // the landing SHA being applied when a conflict occurred
}

// ChangeApplicator applies one resolved change to a workspace.
type ChangeApplicator interface {
	Apply(ctx context.Context, repoDir string, rc session.ResolvedChange) (ApplyResult, error)
}

// CherryPickApplicator implements ChangeApplicator with git cherry-pick.
type CherryPickApplicator struct {
	runner gitx.Runner
	log    *slog.Logger
}

// NewCherryPickApplicator builds the MVP applicator.
func NewCherryPickApplicator(r gitx.Runner, log *slog.Logger) *CherryPickApplicator {
	return &CherryPickApplicator{runner: r, log: log}
}

// Apply cherry-picks the change's landing commits in order.
func (a *CherryPickApplicator) Apply(ctx context.Context, repoDir string, rc session.ResolvedChange) (ApplyResult, error) {
	shas := rc.LandingSHAs()
	args := []string{"cherry-pick", "--empty=keep"}
	if rc.Strategy() == session.StrategyMerge {
		args = append(args, "-m", "1")
	}
	for _, s := range shas {
		if err := gitx.ValidateSHA(s); err != nil {
			return ApplyResult{}, err
		}
	}
	args = append(args, shas...)
	before, err := a.head(ctx, repoDir)
	if err != nil {
		return ApplyResult{}, err
	}
	if _, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: args, Category: gitx.CategoryCherryPick}); err != nil {
		var ee *gitx.ExitError
		if !asExitError(err, &ee) {
			return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), err)
		}
		paths, listErr := a.conflictingPaths(ctx, repoDir)
		if listErr != nil {
			return ApplyResult{}, fmt.Errorf("apply #%d: list conflicts: %w", rc.Number(), listErr)
		}
		if len(paths) == 0 {
			return ApplyResult{}, fmt.Errorf("apply #%d: cherry-pick failed without conflicts: %w", rc.Number(), err)
		}
		return ApplyResult{Outcome: OutcomeConflict, ConflictingPaths: paths, Commit: a.currentPickSHA(ctx, repoDir, shas)}, nil
	}
	after, err := a.head(ctx, repoDir)
	if err != nil {
		return ApplyResult{}, err
	}
	if after == before {
		return ApplyResult{Outcome: OutcomeEmpty}, nil
	}
	if err := a.annotate(ctx, repoDir, rc); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Outcome: OutcomeApplied}, nil
}

// Abort clears an in-progress cherry-pick.
func (a *CherryPickApplicator) Abort(ctx context.Context, repoDir string) error {
	if _, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"cherry-pick", "--abort"}, Category: gitx.CategoryCherryPick}); err != nil {
		return fmt.Errorf("cherry-pick abort: %w", err)
	}
	return nil
}

func (a *CherryPickApplicator) head(ctx context.Context, repoDir string) (string, error) {
	res, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"rev-parse", "HEAD"}, Category: gitx.CategoryQuery})
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// annotate rewrites the tip commit message with Converge trailers (FR-6.5).
func (a *CherryPickApplicator) annotate(ctx context.Context, repoDir string, rc session.ResolvedChange) error {
	res, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"log", "-1", "--format=%B"}, Category: gitx.CategoryQuery})
	if err != nil {
		return fmt.Errorf("read commit message: %w", err)
	}
	body := strings.TrimRight(string(res.Stdout), "\n")
	msg := fmt.Sprintf("%s\n\nConverge-Change: #%d\nConverge-Source: %s\n", body, rc.Number(), rc.SourceSHA())
	spec := gitx.Spec{Dir: repoDir, Args: []string{"commit", "--amend", "--allow-empty", "--no-verify", "-F", "-"}, Stdin: bytes.NewReader([]byte(msg)), Category: gitx.CategoryCherryPick}
	if _, err := a.runner.Run(ctx, spec); err != nil {
		return fmt.Errorf("amend commit message: %w", err)
	}
	return nil
}

func (a *CherryPickApplicator) conflictingPaths(ctx context.Context, repoDir string) ([]string, error) {
	res, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"diff", "--name-only", "--diff-filter=U", "-z"}, Category: gitx.CategoryDiff})
	if err != nil {
		return nil, err
	}
	raw := bytes.TrimSuffix(res.Stdout, []byte{0})
	if len(raw) == 0 {
		return nil, nil
	}
	return strings.Split(string(raw), "\x00"), nil
}

// currentPickSHA reports which landing commit git was applying, falling back to the first.
func (a *CherryPickApplicator) currentPickSHA(ctx context.Context, repoDir string, shas []string) string {
	res, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"rev-parse", "--verify", "--quiet", "CHERRY_PICK_HEAD"}, Category: gitx.CategoryQuery})
	if err == nil {
		if s := strings.TrimSpace(string(res.Stdout)); s != "" {
			return s
		}
	}
	return shas[0]
}

func asExitError(err error, target **gitx.ExitError) bool {
	return errorsAs(err, target)
}
```

Add `errorsAs` as a thin wrapper in the same file to keep the import list explicit:

```go
import "errors"

func errorsAs(err error, target any) bool { return errors.As(err, target) }
```

- [ ] **Step 4: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/review/ && go tool golangci-lint run ./internal/review/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/review && git commit -m "feat(task-001): cherry-pick applicator with conflict reporting"
```

---

### Task 15: Review service (orchestration) and workspace cleaner adapter

**Files:**
- Create: `apps/backend/internal/review/cleaner.go`, `service.go`, `service_test.go`

**Interfaces:**
- Produces:
  - `type Cleaner struct{...}`; `func NewCleaner(mirrors *mirror.Cache, workspaces *workspace.Manager, log *slog.Logger) *Cleaner` implementing `session.Cleaner`: `Cleanup(ctx, s session.Session) error` resolves the mirror path from the session's provider and repository (best effort; empty path when unresolvable) and calls `workspace.Cleanup`; `RemoveDir(ctx, id) error` calls `workspace.RemoveDir`.
  - `type Service struct{...}`; `func NewService(deps Deps) *Service` where `type Deps struct{ Providers *provider.Registry; Mirrors *mirror.Cache; Workspaces *workspace.Manager; Store *session.Store; Applicator ChangeApplicator; Runner gitx.Runner; Log *slog.Logger; SessionTTL time.Duration; MaxConcurrentBuilds int; Now func() time.Time }`.
  - `(*Service) Create(ctx, in CreateInput) (session.Session, error)` — validates, resolves the repository (for the default branch), persists `CREATING`, returns immediately. Does **not** start the build.
  - `(*Service) StartBuild(ctx context.Context, id string)` — spawns the goroutine (semaphore-bounded) used by the HTTP API.
  - `(*Service) Build(ctx context.Context, id string) session.Session` — the synchronous pipeline used by the CLI and by `StartBuild`.
  - `(*Service) Get(id) (session.Session, bool)`, `List() []session.Session`, `Files(id) ([]diff.FileSummary, error)`, `FileDiff(ctx, id, path string) (diff.FileDiff, error)`, `CombinedDiffPath(id) (string, error)`, `Finish(ctx, id) error`.
  - `var ErrNotReady = errors.New("review: not ready")`.

- [ ] **Step 1: Write the failing service test**

`apps/backend/internal/review/service_test.go`:

```go
package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
	"github.com/jtumidanski/converge/internal/workspace"
)

type serviceFixture struct {
	svc  *Service
	prov *fake.Provider
	src  *testutil.Repo
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Commit("unrelated.txt", "u\n", "unrelated before")
	src.Branch("feat/a")
	src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	mergeSHA := src.MergeNoFF("feat/a", "merge a")
	src.Commit("unrelated.txt", "u2\n", "unrelated after")
	src.Push()

	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	locks := &gitx.LockMap{}
	mirrors := mirror.New(t.TempDir(), runner, locks, testLog())
	ws, err := workspace.New(t.TempDir(), runner, locks, testLog())
	if err != nil {
		t.Fatal(err)
	}
	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)
	c, _ := provider.NewCommit(src.RevParse("feat/a"), "a1", time.Now())
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(1).SetTitle("Add a").
		SetAuthor("dev").SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(time.Now()).
		SetMergeCommitSHA(mergeSHA).SetCommits([]provider.Commit{c}).Build()
	if err != nil {
		t.Fatal(err)
	}
	p.AddChange(cr)
	registry := provider.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	cleaner := NewCleaner(mirrors, ws, testLog())
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, testLog(), time.Now)
	svc := NewService(Deps{
		Providers: registry, Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: NewCherryPickApplicator(runner, testLog()), Runner: runner, Log: testLog(),
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 2, Now: time.Now,
	})
	return &serviceFixture{svc: svc, prov: p, src: src}
}

func TestServiceBuildHappyPath(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	s, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server", Changes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Status() != session.StatusCreating || s.BaseBranch() != "main" {
		t.Fatalf("created = %+v", s)
	}
	done := f.svc.Build(ctx, s.ID())
	if done.Status() != session.StatusReady {
		t.Fatalf("status = %s err = %+v", done.Status(), done.Error())
	}
	if done.BaseSHA() == "" || done.HeadSHA() == "" || done.Totals() == nil || done.Totals().Files != 1 {
		t.Fatalf("session = %+v totals=%+v", done, done.Totals())
	}
	files, err := f.svc.Files(done.ID())
	if err != nil || len(files) != 1 || files[0].Path != "a.txt" {
		t.Fatalf("files = %v %v", files, err)
	}
	fd, err := f.svc.FileDiff(ctx, done.ID(), "a.txt")
	if err != nil || !strings.Contains(fd.Diff, "+a") {
		t.Fatalf("file diff = %+v %v", fd, err)
	}
	if _, err := f.svc.FileDiff(ctx, done.ID(), "unrelated.txt"); err == nil {
		t.Error("unknown file must fail")
	}
	p, err := f.svc.CombinedDiffPath(done.ID())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "a.txt") || strings.Contains(string(b), "unrelated.txt") {
		t.Errorf("combined diff wrong:\n%s", b)
	}
	// finish removes everything
	if err := f.svc.Finish(ctx, done.ID()); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Get(done.ID()); got.Status() != session.StatusFinished {
		t.Errorf("status = %s", got.Status())
	}
	if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
		t.Error("session dir remains")
	}
	if err := f.svc.Finish(ctx, done.ID()); err != nil {
		t.Errorf("finish must be idempotent: %v", err)
	}
	if _, err := f.svc.Files(done.ID()); !errors.Is(err, ErrNotReady) {
		t.Errorf("files after finish: %v", err)
	}
}

func TestServiceCreateValidationAndUnknownProvider(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	var ie *InputError
	if _, err := f.svc.Create(ctx, CreateInput{ProviderID: "nope", Repository: "atlas/server", Changes: []int{1}}); !errors.As(err, &ie) || ie.Code != CodeInvalidProvider {
		t.Fatalf("unknown provider: %v", err)
	}
	if _, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "../x", Changes: []int{1}}); !errors.As(err, &ie) || ie.Code != CodeInvalidRepository {
		t.Fatalf("bad repo: %v", err)
	}
	if _, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/nope", Changes: []int{1}}); err == nil {
		t.Fatal("unknown repository must fail")
	}
	if _, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server"}); !errors.As(err, &ie) || ie.Code != CodeInvalidChanges {
		t.Fatalf("empty changes: %v", err)
	}
}

func TestServiceBuildFailureIsRecorded(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	open, _ := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(mustRepo(t, f)).SetNumber(2).SetTitle("open").
		SetTargetBranch("main").SetState(provider.StateOpen).Build()
	f.prov.AddChange(open)
	s, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server", Changes: []int{2}})
	if err != nil {
		t.Fatal(err)
	}
	done := f.svc.Build(ctx, s.ID())
	if done.Status() != session.StatusFailed || done.Error() == nil || done.Error().Code != session.CodeNotMerged {
		t.Fatalf("done = %+v err=%+v", done, done.Error())
	}
	if _, err := f.svc.Files(done.ID()); !errors.Is(err, ErrNotReady) {
		t.Errorf("files on failed session: %v", err)
	}
}

func TestServiceStartBuildIsAsynchronous(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	s, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server", Changes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.StartBuild(context.Background(), s.ID())
	deadline := time.Now().Add(60 * time.Second)
	for {
		got, _ := f.svc.Get(s.ID())
		if got.Status() != session.StatusCreating {
			if got.Status() != session.StatusReady {
				t.Fatalf("status = %s err = %+v", got.Status(), got.Error())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("build did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func mustRepo(t *testing.T, f *serviceFixture) provider.Repository {
	t.Helper()
	r, err := f.prov.GetRepository(context.Background(), "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
```

- [ ] **Step 2: Run to see failure**

Run: `cd <worktree-root>/apps/backend && go test ./internal/review/ -run Service`
Expected: FAIL (`NewService` undefined).

- [ ] **Step 3: Implement the cleaner adapter**

`apps/backend/internal/review/cleaner.go`:

```go
package review

import (
	"context"
	"log/slog"

	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/workspace"
)

// Cleaner adapts the workspace manager to session.Cleaner.
type Cleaner struct {
	mirrors    *mirror.Cache
	workspaces *workspace.Manager
	log        *slog.Logger
}

// NewCleaner builds the adapter.
func NewCleaner(m *mirror.Cache, w *workspace.Manager, log *slog.Logger) *Cleaner {
	return &Cleaner{mirrors: m, workspaces: w, log: log}
}

// Cleanup removes the worktree, branch and session directory.
func (c *Cleaner) Cleanup(ctx context.Context, s session.Session) error {
	mirrorPath, err := c.mirrors.Path(s.ProviderID(), s.Repository())
	if err != nil {
		c.log.Debug("cleanup without mirror path", slog.String("session", s.ID()), slog.String("error", err.Error()))
		mirrorPath = ""
	}
	return c.workspaces.Cleanup(ctx, mirrorPath, s.ID())
}

// RemoveDir removes an orphaned session directory.
func (c *Cleaner) RemoveDir(_ context.Context, id string) error { return c.workspaces.RemoveDir(id) }

var _ session.Cleaner = (*Cleaner)(nil)
```

- [ ] **Step 4: Implement the service**

`apps/backend/internal/review/service.go`:

```go
package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/workspace"
)

// ErrNotReady is returned for file endpoints on a session that is not READY.
var ErrNotReady = errors.New("review: not ready")

// CombinedDiffFile is the file name inside a session directory.
const CombinedDiffFile = "combined.diff"

// buildCeiling bounds a single build.
const buildCeiling = 60 * time.Minute

// Deps are the service's collaborators.
type Deps struct {
	Providers           *provider.Registry
	Mirrors             *mirror.Cache
	Workspaces          *workspace.Manager
	Store               *session.Store
	Applicator          ChangeApplicator
	Runner              gitx.Runner
	Log                 *slog.Logger
	SessionTTL          time.Duration
	MaxConcurrentBuilds int
	Now                 func() time.Time
}

// Service orchestrates reconstruction.
type Service struct {
	deps     Deps
	resolver *Resolver
	sem      chan struct{}
}

// NewService wires the orchestrator.
func NewService(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.MaxConcurrentBuilds <= 0 {
		d.MaxConcurrentBuilds = 4
	}
	return &Service{deps: d, resolver: NewResolver(d.Mirrors, d.Log), sem: make(chan struct{}, d.MaxConcurrentBuilds)}
}

// Get returns a session by ID.
func (s *Service) Get(id string) (session.Session, bool) { return s.deps.Store.Get(id) }

// List returns active sessions.
func (s *Service) List() []session.Session { return s.deps.Store.List() }

// Finish cleans up and marks the session FINISHED.
func (s *Service) Finish(ctx context.Context, id string) error { return s.deps.Store.Finish(ctx, id) }

// Create validates the request and persists a CREATING session.
func (s *Service) Create(ctx context.Context, in CreateInput) (session.Session, error) {
	in, err := in.Validate(ctx, s.deps.Runner)
	if err != nil {
		return session.Session{}, err
	}
	p, ok := s.deps.Providers.Get(in.ProviderID)
	if !ok {
		return session.Session{}, &InputError{Code: CodeInvalidProvider, Field: "provider", Message: fmt.Sprintf("Provider %q is not configured.", in.ProviderID)}
	}
	repo, err := p.GetRepository(ctx, in.Repository)
	if err != nil {
		if re := MapProviderError(p.ID(), in.Repository, err); re != nil {
			return session.Session{}, re
		}
		return session.Session{}, &session.ReviewError{Code: session.CodeRepositoryUnavailable, Message: MsgRepositoryUnavailable(in.Repository)}
	}
	if in.BaseBranch == "" {
		in.BaseBranch = repo.DefaultBranch()
		if err := gitx.ValidateBranch(ctx, s.deps.Runner, in.BaseBranch); err != nil {
			return session.Session{}, &InputError{Code: CodeInvalidBranch, Field: "baseBranch", Message: "The repository's default branch name is not usable."}
		}
	}
	id, err := session.NewID()
	if err != nil {
		return session.Session{}, err
	}
	sess, err := session.NewBuilder().SetID(id).SetProviderID(p.ID()).SetRepository(repo.FullName()).SetBaseBranch(in.BaseBranch).
		SetRequestedChanges(in.Changes).SetCreatedAt(s.deps.Now()).SetTTL(s.deps.SessionTTL).Build()
	if err != nil {
		return session.Session{}, err
	}
	if err := s.deps.Store.Save(sess); err != nil {
		return session.Session{}, err
	}
	s.deps.Log.Info("review created", slog.String("session", id), slog.String("provider", p.ID()), slog.String("repository", repo.FullName()), slog.Any("changes", in.Changes))
	return sess, nil
}

// StartBuild runs Build on a background goroutine bounded by the semaphore.
func (s *Service) StartBuild(ctx context.Context, id string) {
	go func() {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		buildCtx, cancel := context.WithTimeout(ctx, buildCeiling)
		defer cancel()
		s.Build(buildCtx, id)
	}()
}

// Build runs the pipeline synchronously and returns the final session.
func (s *Service) Build(ctx context.Context, id string) session.Session {
	sess, ok := s.deps.Store.Get(id)
	if !ok {
		return session.Session{}
	}
	defer func() {
		if r := recover(); r != nil {
			s.deps.Log.Error("build panicked", slog.String("session", id), slog.Any("panic", r))
			s.fail(sess, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()})
		}
	}()
	final, err := s.build(ctx, sess)
	if err != nil {
		re := asReviewError(err)
		if re == nil {
			re = &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
		}
		return s.finishWithError(sess, re)
	}
	return final
}

func (s *Service) save(sess session.Session) session.Session {
	if err := s.deps.Store.Save(sess); err != nil {
		s.deps.Log.Error("persist session failed", slog.String("session", sess.ID()), slog.String("error", err.Error()))
	}
	return sess
}

func (s *Service) fail(sess session.Session, re *session.ReviewError) session.Session {
	return s.save(sess.Failed(re, s.deps.Now()))
}

// finishWithError records CONFLICTED for conflicts and FAILED otherwise.
func (s *Service) finishWithError(sess session.Session, re *session.ReviewError) session.Session {
	if re.Diagnostics == nil {
		re.Diagnostics = &session.Diagnostics{WorkspacePath: s.deps.Workspaces.RepoDir(sess.ID()), Branch: s.deps.Workspaces.BranchName(sess.ID())}
	}
	if re.Code == session.CodeConflict {
		s.deps.Log.Info("review conflicted", slog.String("session", sess.ID()), slog.Int("change", re.Change))
		return s.save(sess.Conflicted(re, s.deps.Now()))
	}
	s.deps.Log.Info("review failed", slog.String("session", sess.ID()), slog.String("code", string(re.Code)))
	return s.fail(sess, re)
}

func (s *Service) build(ctx context.Context, sess session.Session) (session.Session, error) {
	p, ok := s.deps.Providers.Get(sess.ProviderID())
	if !ok {
		return session.Session{}, &session.ReviewError{Code: session.CodeProviderUnavailable, Message: MsgProviderUnavailable(sess.ProviderID())}
	}
	repo, err := p.GetRepository(ctx, sess.Repository())
	if err != nil {
		if re := MapProviderError(p.ID(), sess.Repository(), err); re != nil {
			return session.Session{}, re
		}
		return session.Session{}, &session.ReviewError{Code: session.CodeRepositoryUnavailable, Message: MsgRepositoryUnavailable(sess.Repository())}
	}

	current := sess
	progress := func(stage string) { current = s.save(current.WithStage(stage, s.deps.Now())) }
	resolved, err := s.resolver.Resolve(ctx, p, repo, sess.BaseBranch(), sess.RequestedChanges(), progress)
	if err != nil {
		return session.Session{}, err
	}
	current = s.save(current.WithResolved(resolved.Changes, s.deps.Now()))
	current, err = current.WithBase(resolved.BaseSHA, s.deps.Now())
	if err != nil {
		return session.Session{}, err
	}
	current = s.save(current)

	current = s.save(current.WithStage(session.StageCreatingWorkspace, s.deps.Now()))
	repoDir, err := s.deps.Workspaces.Create(ctx, resolved.MirrorPath, current.ID(), resolved.BaseSHA)
	if err != nil {
		return session.Session{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}

	var applied []int
	for i, rc := range resolved.Changes {
		current = s.save(current.WithStage(fmt.Sprintf("%s%d", session.StageApplyingPrefix, rc.Number()), s.deps.Now()))
		res, err := s.deps.Applicator.Apply(ctx, repoDir, rc)
		if err != nil {
			return session.Session{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure(), Change: rc.Number()}
		}
		if res.Outcome == OutcomeConflict {
			dependency := i > 0
			return session.Session{}, &session.ReviewError{
				Code: session.CodeConflict, Message: MsgConflict(rc.Number(), dependency), Change: rc.Number(),
				Commit: res.Commit, ConflictingFiles: res.ConflictingPaths, AppliedChanges: applied, PossibleDependency: dependency,
				Diagnostics: &session.Diagnostics{WorkspacePath: repoDir, Branch: s.deps.Workspaces.BranchName(current.ID()), Strategy: string(rc.Strategy()), SourceSHA: rc.SourceSHA()},
			}
		}
		applied = append(applied, rc.Number())
	}

	current = s.save(current.WithStage(session.StageDiffing, s.deps.Now()))
	headRes, err := s.deps.Runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"rev-parse", "HEAD"}, Category: gitx.CategoryQuery, Session: current.ID()})
	if err != nil {
		return session.Session{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}
	head := trimSHA(headRes.Stdout)
	outPath := filepath.Join(s.deps.Workspaces.SessionDir(current.ID()), CombinedDiffFile)
	if err := diff.WriteCombined(ctx, s.deps.Runner, repoDir, resolved.BaseSHA, head, outPath); err != nil {
		return session.Session{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}
	files, totals, err := diff.Summarize(ctx, s.deps.Runner, repoDir, resolved.BaseSHA, head)
	if err != nil {
		return session.Session{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}
	ready, err := current.Ready(head, files, totals, s.deps.Now())
	if err != nil {
		return session.Session{}, err
	}
	s.deps.Log.Info("review ready", slog.String("session", ready.ID()), slog.Int("files", totals.Files))
	return s.save(ready), nil
}

// Files returns the stored summary for a READY session.
func (s *Service) Files(id string) ([]diff.FileSummary, error) {
	sess, ok := s.deps.Store.Get(id)
	if !ok {
		return nil, session.ErrNotFound
	}
	if sess.Status() != session.StatusReady {
		return nil, ErrNotReady
	}
	return sess.Files(), nil
}

// FileDiff renders one file's diff on demand.
func (s *Service) FileDiff(ctx context.Context, id, path string) (diff.FileDiff, error) {
	sess, ok := s.deps.Store.Get(id)
	if !ok {
		return diff.FileDiff{}, session.ErrNotFound
	}
	if sess.Status() != session.StatusReady {
		return diff.FileDiff{}, ErrNotReady
	}
	for _, f := range sess.Files() {
		if f.Path == path {
			return diff.FileContent(ctx, s.deps.Runner, s.deps.Workspaces.RepoDir(id), sess.BaseSHA(), sess.HeadSHA(), f)
		}
	}
	return diff.FileDiff{}, fmt.Errorf("%w: file %q is not part of this review", session.ErrNotFound, path)
}

// CombinedDiffPath returns the on-disk combined.diff for a READY session.
func (s *Service) CombinedDiffPath(id string) (string, error) {
	sess, ok := s.deps.Store.Get(id)
	if !ok {
		return "", session.ErrNotFound
	}
	if sess.Status() != session.StatusReady {
		return "", ErrNotReady
	}
	p := filepath.Join(s.deps.Workspaces.SessionDir(id), CombinedDiffFile)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("%w: combined diff missing", session.ErrNotFound)
	}
	return p, nil
}

func trimSHA(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

```

Delete the two lines shown at the end of the sketch in some drafts. `service.go` must **not**
declare `var _ session.Cleaner = (*Cleaner)(nil)` — that assertion belongs in `cleaner.go`
only — and must not import `workspace` unless it uses it. As written above, `service.go`
uses `workspace` only through `s.deps.Workspaces`, whose type comes from `Deps`, so the
`workspace` import is required for the `Deps` field declaration and nothing else.

- [ ] **Step 5: Run tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/... && go tool golangci-lint run`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/review && git commit -m "feat(task-001): review service orchestration and cleaner adapter"
```

---

### Task 16: Integration tests (FR-12.2)

**Files:**
- Create: `apps/backend/internal/review/integration_test.go` (build tag `integration`), `apps/backend/internal/review/harness_test.go` (build tag `integration`)

**Interfaces:**
- Consumes: everything from Tasks 7-15.
- Produces: a shared `buildFixture` helper plus one test function per FR-12.2 bullet.

- [ ] **Step 1: Write the integration harness**

`apps/backend/internal/review/harness_test.go`:

```go
//go:build integration

package review

import (
	"context"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
	"github.com/jtumidanski/converge/internal/workspace"
)

// harness wires a real git repository to a fake provider and a full Service.
type harness struct {
	src        *testutil.Repo
	prov       *fake.Provider
	repo       provider.Repository
	svc        *Service
	runner     *gitx.ExecRunner
	mirrors    *mirror.Cache
	workspaces *workspace.Manager
	mergedAt   time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	src := testutil.NewRepo(t)
	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{CommandTimeout: time.Minute, CloneTimeout: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	locks := &gitx.LockMap{}
	mirrors := mirror.New(t.TempDir(), runner, locks, testLog())
	ws, err := workspace.New(t.TempDir(), runner, locks, testLog())
	if err != nil {
		t.Fatal(err)
	}
	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)
	registry := provider.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	cleaner := NewCleaner(mirrors, ws, testLog())
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, testLog(), time.Now)
	svc := NewService(Deps{
		Providers: registry, Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: NewCherryPickApplicator(runner, testLog()), Runner: runner, Log: testLog(),
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 4, Now: time.Now,
	})
	return &harness{src: src, prov: p, repo: repo, svc: svc, runner: runner, mirrors: mirrors, workspaces: ws, mergedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}
}

// addChange registers a merged change; landing is the SHA the provider reports.
func (h *harness) addChange(t *testing.T, number int, merge, squash, head string, commitSHAs []string) {
	t.Helper()
	h.mergedAt = h.mergedAt.Add(time.Hour)
	commits := make([]provider.Commit, 0, len(commitSHAs))
	for _, s := range commitSHAs {
		c, err := provider.NewCommit(s, "m", h.mergedAt)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, c)
	}
	b := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(h.repo).SetNumber(number).
		SetTitle("change "+itoa(number)).SetAuthor("dev").SetTargetBranch("main").SetState(provider.StateMerged).
		SetMergedAt(h.mergedAt).SetCommits(commits)
	if merge != "" {
		b.SetMergeCommitSHA(merge)
	}
	if squash != "" {
		b.SetSquashCommitSHA(squash)
	}
	if head != "" {
		b.SetHeadSHA(head)
	}
	cr, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	h.prov.AddChange(cr)
}

// build runs Create + Build and returns the final session.
func (h *harness) build(t *testing.T, numbers ...int) session.Session {
	t.Helper()
	ctx := context.Background()
	s, err := h.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server", BaseBranch: "main", Changes: numbers})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return h.svc.Build(ctx, s.ID())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
```

- [ ] **Step 2: Write the integration scenarios**

`apps/backend/internal/review/integration_test.go`:

```go
//go:build integration

package review

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/session"
)

func pathsOf(s session.Session) map[string]bool {
	out := map[string]bool{}
	for _, f := range s.Files() {
		out[f.Path] = true
	}
	return out
}

func TestIntegrationSquashMerge(t *testing.T) {
	h := newHarness(t)
	base := h.src.Head()
	h.src.Branch("feat/a")
	c1 := h.src.Commit("a.txt", "a\n", "a1")
	h.src.Checkout("main")
	squash := h.src.Squash("feat/a", "squash a")
	h.src.Push()
	h.addChange(t, 1, "", squash, c1, []string{c1})

	got := h.build(t, 1)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	if got.BaseSHA() != base {
		t.Errorf("base = %s want %s", got.BaseSHA(), base)
	}
	if got.ResolvedChanges()[0].Strategy() != session.StrategySquash {
		t.Errorf("strategy = %s", got.ResolvedChanges()[0].Strategy())
	}
	if !pathsOf(got)["a.txt"] || len(got.Files()) != 1 {
		t.Errorf("files = %+v", got.Files())
	}
}

func TestIntegrationMergeCommitWithMultipleCommits(t *testing.T) {
	h := newHarness(t)
	base := h.src.Head()
	h.src.Branch("feat/a")
	h.src.Commit("a.txt", "a1\n", "a1")
	h.src.Commit("a.txt", "a1\na2\n", "a2")
	h.src.Checkout("main")
	branchCommits := h.src.BranchCommits("feat/a")
	merge := h.src.MergeNoFF("feat/a", "merge a")
	h.src.Push()
	h.addChange(t, 1, merge, "", branchCommits[len(branchCommits)-1], branchCommits)

	got := h.build(t, 1)
	if got.Status() != session.StatusReady || got.BaseSHA() != base {
		t.Fatalf("status=%s base=%s err=%+v", got.Status(), got.BaseSHA(), got.Error())
	}
	if got.ResolvedChanges()[0].Strategy() != session.StrategyMerge {
		t.Errorf("strategy = %s", got.ResolvedChanges()[0].Strategy())
	}
	fd, err := h.svc.FileDiff(context.Background(), got.ID(), "a.txt")
	if err != nil || !strings.Contains(fd.Diff, "+a1") || !strings.Contains(fd.Diff, "+a2") {
		t.Errorf("diff = %q err=%v", fd.Diff, err)
	}
}

func TestIntegrationRebaseFastForward(t *testing.T) {
	h := newHarness(t)
	base := h.src.Head()
	h.src.Branch("feat/a")
	orig := []string{h.src.Commit("a.txt", "a1\n", "a1"), h.src.Commit("b.txt", "b1\n", "b1")}
	h.src.Checkout("main")
	rebased := h.src.Rebase("feat/a")
	h.src.Push()
	// the provider reports the ORIGINAL commit SHAs and the rewritten head
	h.addChange(t, 1, "", "", rebased[len(rebased)-1], orig)

	got := h.build(t, 1)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	if got.ResolvedChanges()[0].Strategy() != session.StrategyRebase || len(got.ResolvedChanges()[0].LandingSHAs()) != 2 {
		t.Errorf("resolved = %+v", got.ResolvedChanges()[0])
	}
	if got.BaseSHA() != base {
		t.Errorf("base = %s want %s", got.BaseSHA(), base)
	}
	if !pathsOf(got)["a.txt"] || !pathsOf(got)["b.txt"] || len(got.Files()) != 2 {
		t.Errorf("files = %+v", got.Files())
	}
}

func TestIntegrationUnrelatedCommitsAreExcluded(t *testing.T) {
	h := newHarness(t)
	h.src.Commit("unrelated-before.txt", "x\n", "before")
	base := h.src.Head()

	h.src.Branch("feat/a")
	a := h.src.Commit("a.txt", "a\n", "a")
	h.src.Checkout("main")
	sqA := h.src.Squash("feat/a", "squash a")
	h.src.Commit("unrelated-middle.txt", "y\n", "middle")

	h.src.Branch("feat/b")
	b := h.src.Commit("b.txt", "b\n", "b")
	h.src.Checkout("main")
	sqB := h.src.Squash("feat/b", "squash b")
	h.src.Commit("unrelated-after.txt", "z\n", "after")
	h.src.Push()

	h.addChange(t, 1, "", sqA, a, []string{a})
	h.addChange(t, 2, "", sqB, b, []string{b})

	got := h.build(t, 2, 1)
	if got.Status() != session.StatusReady || got.BaseSHA() != base {
		t.Fatalf("status=%s base=%s err=%+v", got.Status(), got.BaseSHA(), got.Error())
	}
	paths := pathsOf(got)
	if !paths["a.txt"] || !paths["b.txt"] || len(paths) != 2 {
		t.Fatalf("files = %v", paths)
	}
	p, err := h.svc.CombinedDiffPath(got.ID())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	for _, unrelated := range []string{"unrelated-before.txt", "unrelated-middle.txt", "unrelated-after.txt"} {
		if strings.Contains(string(raw), unrelated) {
			t.Errorf("combined diff mentions %s", unrelated)
		}
	}
}

func TestIntegrationRepeatedEditsToOneFileCollapse(t *testing.T) {
	h := newHarness(t)
	h.src.Commit("f.txt", "v1\n", "seed")
	h.src.Branch("feat/a")
	a := h.src.Commit("f.txt", "v2\n", "a")
	h.src.Checkout("main")
	sqA := h.src.Squash("feat/a", "squash a")
	h.src.Branch("feat/b")
	b := h.src.Commit("f.txt", "v3\n", "b")
	h.src.Checkout("main")
	sqB := h.src.Squash("feat/b", "squash b")
	h.src.Push()
	h.addChange(t, 1, "", sqA, a, []string{a})
	h.addChange(t, 2, "", sqB, b, []string{b})

	got := h.build(t, 1, 2)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	files := got.Files()
	if len(files) != 1 || files[0].Path != "f.txt" || files[0].Additions != 1 || files[0].Deletions != 1 {
		t.Fatalf("expected one cumulative hunk: %+v", files)
	}
	fd, _ := h.svc.FileDiff(context.Background(), got.ID(), "f.txt")
	if !strings.Contains(fd.Diff, "+v3") || strings.Contains(fd.Diff, "+v2") {
		t.Errorf("diff should show only the net change:\n%s", fd.Diff)
	}
}

func TestIntegrationDependencyOnUnselectedChangeConflicts(t *testing.T) {
	h := newHarness(t)
	h.src.Commit("f.txt", "base\n", "seed")
	// change 1 (NOT selected) rewrites the file
	h.src.Branch("feat/1")
	c1 := h.src.Commit("f.txt", "from one\n", "one")
	h.src.Checkout("main")
	sq1 := h.src.Squash("feat/1", "squash 1")
	// change 2 (selected) builds on change 1's content
	h.src.Branch("feat/2")
	c2 := h.src.Commit("f.txt", "from one and two\n", "two")
	h.src.Checkout("main")
	sq2 := h.src.Squash("feat/2", "squash 2")
	// change 3 (selected, earlier in the set) touches an unrelated file
	h.src.Branch("feat/3")
	c3 := h.src.Commit("g.txt", "g\n", "three")
	h.src.Checkout("main")
	sq3 := h.src.Squash("feat/3", "squash 3")
	h.src.Push()
	h.addChange(t, 1, "", sq1, c1, []string{c1})
	h.addChange(t, 2, "", sq2, c2, []string{c2})
	h.addChange(t, 3, "", sq3, c3, []string{c3})

	got := h.build(t, 2, 3) // 2 merged before 3, so 2 applies first... but 2 depends on 1
	if got.Status() != session.StatusConflicted {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	re := got.Error()
	if re.Code != session.CodeConflict || re.Change != 2 {
		t.Fatalf("error = %+v", re)
	}
	if len(re.ConflictingFiles) != 1 || re.ConflictingFiles[0] != "f.txt" {
		t.Errorf("conflicting files = %v", re.ConflictingFiles)
	}
	for _, n := range re.AppliedChanges {
		if n == 1 {
			t.Fatal("unselected change 1 must never be applied")
		}
	}
	p, _ := h.svc.CombinedDiffPath(got.ID())
	if p != "" {
		t.Error("no combined diff may exist for a conflicted session")
	}
}

func TestIntegrationPlainConflictBetweenSelectedChanges(t *testing.T) {
	h := newHarness(t)
	h.src.Commit("f.txt", "original\n", "seed")
	h.src.Branch("feat/a")
	a := h.src.Commit("f.txt", "from a\n", "a")
	h.src.Checkout("main")
	sqA := h.src.Squash("feat/a", "squash a")
	h.src.Branch("feat/b")
	// branch from the pre-a state so the two conflict
	h.src.Git("reset", "--hard", "HEAD~1")
	b := h.src.Commit("f.txt", "from b\n", "b")
	h.src.Checkout("main")
	sqB := h.src.Squash("feat/b", "squash b")
	h.src.Push()
	h.addChange(t, 1, "", sqA, a, []string{a})
	h.addChange(t, 2, "", sqB, b, []string{b})

	got := h.build(t, 1, 2)
	if got.Status() != session.StatusConflicted {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	re := got.Error()
	if re.Change != 2 || !re.PossibleDependency || len(re.AppliedChanges) != 1 || re.AppliedChanges[0] != 1 {
		t.Fatalf("error = %+v", re)
	}
	if re.Diagnostics == nil || re.Diagnostics.WorkspacePath == "" || re.Diagnostics.Branch != "review/"+got.ID() {
		t.Errorf("diagnostics = %+v", re.Diagnostics)
	}
}

func TestIntegrationCleanupLeavesMirrorUsable(t *testing.T) {
	h := newHarness(t)
	h.src.Branch("feat/a")
	a := h.src.Commit("a.txt", "a\n", "a")
	h.src.Checkout("main")
	sq := h.src.Squash("feat/a", "squash a")
	h.src.Push()
	h.addChange(t, 1, "", sq, a, []string{a})

	got := h.build(t, 1)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	ctx := context.Background()
	if err := h.svc.Finish(ctx, got.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.workspaces.SessionDir(got.ID())); !os.IsNotExist(err) {
		t.Error("session dir remains")
	}
	mirrorPath, err := h.mirrors.Path("fake", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"branch", "--list", "review/*"}, Category: gitx.CategoryQuery})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "" {
		t.Errorf("review branch remains: %q %v", res.Stdout, err)
	}
	if _, err := h.runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"fsck", "--no-progress"}, Category: gitx.CategoryQuery}); err != nil {
		t.Fatalf("mirror damaged: %v", err)
	}
	// the mirror is still usable for a second review
	got2 := h.build(t, 1)
	if got2.Status() != session.StatusReady {
		t.Fatalf("second build: %s %+v", got2.Status(), got2.Error())
	}
	if err := h.svc.Finish(ctx, got2.ID()); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Finish(ctx, got2.ID()); err != nil {
		t.Fatalf("cleanup must be idempotent: %v", err)
	}
}

func TestIntegrationConcurrentBuildsShareOneMirror(t *testing.T) {
	h := newHarness(t)
	var shas []string
	for i := 1; i <= 4; i++ {
		name := "feat/" + itoa(i)
		h.src.Branch(name)
		c := h.src.Commit("f"+itoa(i)+".txt", "c\n", "c"+itoa(i))
		h.src.Checkout("main")
		sq := h.src.Squash(name, "squash "+itoa(i))
		shas = append(shas, sq)
		h.addChange(t, i, "", sq, c, []string{c})
	}
	h.src.Push()
	_ = shas

	var wg sync.WaitGroup
	results := make([]session.Session, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = h.build(t, i+1)
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r.Status() != session.StatusReady {
			t.Errorf("build %d: status=%s err=%+v", i+1, r.Status(), r.Error())
		}
	}
	mirrorPath, _ := h.mirrors.Path("fake", "atlas/server")
	if _, err := h.runner.Run(context.Background(), gitx.Spec{Dir: mirrorPath, Args: []string{"fsck", "--no-progress"}, Category: gitx.CategoryQuery}); err != nil {
		t.Fatalf("mirror corrupted by concurrent builds: %v", err)
	}
}

func TestIntegrationNoTokenLeaksIntoMirrorRemote(t *testing.T) {
	h := newHarness(t)
	h.src.Branch("feat/a")
	a := h.src.Commit("a.txt", "a\n", "a")
	h.src.Checkout("main")
	sq := h.src.Squash("feat/a", "squash a")
	h.src.Push()
	h.addChange(t, 1, "", sq, a, []string{a})
	got := h.build(t, 1)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s", got.Status())
	}
	mirrorPath, _ := h.mirrors.Path("fake", "atlas/server")
	res, err := h.runner.Run(context.Background(), gitx.Spec{Dir: mirrorPath, Args: []string{"remote", "get-url", "origin"}, Category: gitx.CategoryQuery})
	if err != nil {
		t.Fatal(err)
	}
	url := strings.TrimSpace(string(res.Stdout))
	if url != h.src.CloneURL() || strings.Contains(url, "@") {
		t.Errorf("remote url = %q", url)
	}
	raw, err := os.ReadFile(h.workspaces.SessionDir(got.ID()) + "/session.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Authorization", "PRIVATE-TOKEN", "extraheader"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("session.json contains %q", forbidden)
		}
	}
}
```

- [ ] **Step 3: Run the integration suite**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 -tags integration ./internal/review/`
Expected: PASS. If `TestIntegrationDependencyOnUnselectedChangeConflicts` does not conflict (because the squash of change 2 replaces the whole file cleanly), adjust the fixture so change 2 edits a *different line* of the same file than change 1 (add a multi-line seed file and have each change edit a distinct nearby line) — the requirement is a real conflict caused by missing change 1, not the exact text. Document the final fixture in a comment.

- [ ] **Step 4: Run everything and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./... && go test -race -count=1 -tags integration ./... && go vet ./... && go tool golangci-lint run`
Expected: all clean.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/review && git commit -m "test(task-001): reconstruction integration suite covering FR-12.2"
```

---

### Task 17: Application wiring and the CLI

**Files:**
- Create: `apps/backend/internal/app/app.go`, `apps/backend/internal/app/app_test.go`, `apps/backend/cmd/converge-cli/main.go`, `apps/backend/cmd/converge-cli/main_test.go`
- Modify: `Makefile` (add `build-cli` inputs to the existing `build` target if needed)

**Interfaces:**
- Produces (package `app`):
  - `type App struct{ Config config.Config; Log *slog.Logger; Runner *gitx.ExecRunner; Registry *provider.Registry; Mirrors *mirror.Cache; Workspaces *workspace.Manager; Store *session.Store; Service *review.Service }`; `(*App) Close() error`.
  - `func New(ctx context.Context, env []string) (*App, error)` — loads config, builds the logger (text/json by `LOG_FORMAT`, level by `LOG_LEVEL`, wrapped in a redacting handler seeded with `cfg.Secrets()`), creates the runner, checks `git --version` ≥ 2.45, creates both roots, builds providers from config, wires everything, runs `Store.LoadAll`. Startup logs one line per provider with ID, kind and base URL and never the token.
  - `func NewLogger(w io.Writer, cfg config.Config) *slog.Logger`.
  - `type redactingHandler struct{...}` implementing `slog.Handler`, replacing any attribute value containing a configured secret with `[redacted]`.
  - `var MinGitVersion = "2.45"`; `func checkGitVersion(v string) error`.
- Produces (CLI): `converge-cli build --provider <id> --repo <owner/repo> [--base <branch>] --changes 1,2,3 [--out <dir>] [--cleanup]`, `converge-cli --version`. Exit codes: 0 READY, 2 CONFLICTED, 3 validation/base errors, 4 provider errors, 1 anything else. Writes `combined.diff` and `metadata.json` into `--out` (default `./converge-out/<session-id>`) and prints the session record as JSON on stdout; logs go to stderr.

- [ ] **Step 1: Write the failing app tests**

`apps/backend/internal/app/app_test.go`:

```go
package app

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtumidanski/converge/internal/config"
)

func TestCheckGitVersion(t *testing.T) {
	for _, ok := range []string{"2.45.0", "2.49.1", "2.55.0", "3.0.0", "2.45.0.windows.1"} {
		if err := checkGitVersion(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"2.44.0", "2.30.2", "1.9.5", "banana"} {
		if err := checkGitVersion(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestRedactingLoggerScrubsSecrets(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.Config{LogFormat: "text", LogLevel: slog.LevelInfo, Providers: []config.ProviderConfig{{ID: "gh", Token: config.NewSecret("ghp_supersecret")}}}
	log := NewLogger(&buf, cfg)
	log.Info("hello", "url", "https://x/y?token=ghp_supersecret", "nested", slog.GroupValue(slog.String("h", "Bearer ghp_supersecret")))
	out := buf.String()
	if strings.Contains(out, "ghp_supersecret") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("no redaction marker: %s", out)
	}
}

func TestNewWiresEverything(t *testing.T) {
	root := t.TempDir()
	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=error",
	}
	a, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Service == nil || a.Store == nil || a.Registry == nil {
		t.Fatal("wiring incomplete")
	}
	if _, ok := a.Registry.Get("gh"); !ok {
		t.Error("provider not registered")
	}
	if len(a.Store.List()) != 0 {
		t.Error("unexpected sessions")
	}
	if _, err := New(context.Background(), []string{"APP_PORT=1"}); err == nil {
		t.Error("missing providers must fail startup")
	}
}
```

- [ ] **Step 2: Implement `internal/app`**

`apps/backend/internal/app/app.go`:

```go
// Package app wires configuration into the collaborators both binaries need.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/github"
	"github.com/jtumidanski/converge/internal/provider/gitlab"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/workspace"
)

// MinGitVersion is required for `cherry-pick --empty=keep`.
const MinGitVersion = "2.45"

// App holds every wired collaborator.
type App struct {
	Config     config.Config
	Log        *slog.Logger
	Runner     *gitx.ExecRunner
	Registry   *provider.Registry
	Mirrors    *mirror.Cache
	Workspaces *workspace.Manager
	Store      *session.Store
	Service    *review.Service
}

// Close releases the git runner's temporary directories.
func (a *App) Close() error {
	if a.Runner != nil {
		return a.Runner.Close()
	}
	return nil
}

// redactingHandler scrubs configured secrets from every logged value.
type redactingHandler struct {
	inner   slog.Handler
	secrets []string
}

func (h *redactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, h.scrubString(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(h.scrubAttr(a))
		return true
	})
	return h.inner.Handle(ctx, clean)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	scrubbed := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		scrubbed[i] = h.scrubAttr(a)
	}
	return &redactingHandler{inner: h.inner.WithAttrs(scrubbed), secrets: h.secrets}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: h.inner.WithGroup(name), secrets: h.secrets}
}

func (h *redactingHandler) scrubString(s string) string {
	for _, secret := range h.secrets {
		if secret != "" && strings.Contains(s, secret) {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

func (h *redactingHandler) scrubAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.scrubString(v.String()))
	case slog.KindGroup:
		group := v.Group()
		out := make([]any, 0, len(group))
		for _, g := range group {
			out = append(out, h.scrubAttr(g))
		}
		return slog.Group(a.Key, out...)
	default:
		return slog.Attr{Key: a.Key, Value: slog.StringValue(h.scrubString(v.String()))}
	}
}

// NewLogger builds the configured handler wrapped in redaction.
func NewLogger(w io.Writer, cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var base slog.Handler
	if cfg.LogFormat == "json" {
		base = slog.NewJSONHandler(w, opts)
	} else {
		base = slog.NewTextHandler(w, opts)
	}
	return slog.New(&redactingHandler{inner: base, secrets: cfg.Secrets()})
}

// checkGitVersion enforces MinGitVersion.
func checkGitVersion(v string) error {
	parse := func(s string) (int, int, error) {
		parts := strings.Split(strings.TrimSpace(s), ".")
		if len(parts) < 2 {
			return 0, 0, fmt.Errorf("cannot parse git version %q", s)
		}
		major, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("cannot parse git version %q", s)
		}
		minor, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("cannot parse git version %q", s)
		}
		return major, minor, nil
	}
	gotMajor, gotMinor, err := parse(v)
	if err != nil {
		return err
	}
	wantMajor, wantMinor, _ := parse(MinGitVersion)
	if gotMajor > wantMajor || (gotMajor == wantMajor && gotMinor >= wantMinor) {
		return nil
	}
	return fmt.Errorf("git %s is required, found %s", MinGitVersion, v)
}

// New loads configuration and wires every collaborator.
func New(ctx context.Context, env []string) (*App, error) {
	cfg, err := config.Load(env)
	if err != nil {
		return nil, err
	}
	log := NewLogger(os.Stderr, cfg)
	runner, err := gitx.NewExecRunner(log, gitx.Options{CloneTimeout: cfg.GitCloneTimeout, CommandTimeout: cfg.GitCommandTimeout, Secrets: cfg.Secrets()})
	if err != nil {
		return nil, err
	}
	version, err := runner.Version(ctx)
	if err != nil {
		_ = runner.Close()
		return nil, fmt.Errorf("git is not usable: %w", err)
	}
	if err := checkGitVersion(version); err != nil {
		_ = runner.Close()
		return nil, err
	}
	for _, dir := range []string{cfg.WorkspaceRoot, cfg.RepositoryCacheRoot} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			_ = runner.Close()
			return nil, &config.Error{Variable: dirVariable(cfg, dir), Reason: "directory could not be created"}
		}
	}
	httpClient := &http.Client{Timeout: cfg.ProviderTimeout}
	registry := provider.NewRegistry()
	for _, pc := range cfg.Providers {
		var p provider.GitProvider
		switch pc.Kind {
		case config.KindGitHub:
			p = github.New(pc.ID, pc.DisplayName, pc.BaseURL, pc.Token, httpClient, time.Now)
		case config.KindGitLab:
			p = gitlab.New(pc.ID, pc.DisplayName, pc.BaseURL, pc.Token, httpClient)
		}
		if err := registry.Register(p); err != nil {
			_ = runner.Close()
			return nil, err
		}
		log.Info("provider configured", slog.String("provider", pc.ID), slog.String("kind", string(pc.Kind)), slog.String("base_url", pc.BaseURL))
	}
	locks := &gitx.LockMap{}
	mirrors := mirror.New(cfg.RepositoryCacheRoot, runner, locks, log)
	workspaces, err := workspace.New(cfg.WorkspaceRoot, runner, locks, log)
	if err != nil {
		_ = runner.Close()
		return nil, err
	}
	cleaner := review.NewCleaner(mirrors, workspaces, log)
	store := session.NewStore(workspaces.Root(), cfg.SessionTTL, cleaner, log, time.Now)
	service := review.NewService(review.Deps{
		Providers: registry, Mirrors: mirrors, Workspaces: workspaces, Store: store,
		Applicator: review.NewCherryPickApplicator(runner, log), Runner: runner, Log: log,
		SessionTTL: cfg.SessionTTL, MaxConcurrentBuilds: cfg.MaxConcurrentBuilds, Now: time.Now,
	})
	if err := store.LoadAll(ctx); err != nil {
		_ = runner.Close()
		return nil, err
	}
	log.Info("converge starting", slog.String("git_version", version), slog.Int("providers", len(cfg.Providers)))
	return &App{Config: cfg, Log: log, Runner: runner, Registry: registry, Mirrors: mirrors, Workspaces: workspaces, Store: store, Service: service}, nil
}

func dirVariable(cfg config.Config, dir string) string {
	if dir == cfg.WorkspaceRoot {
		return "WORKSPACE_ROOT"
	}
	return "REPOSITORY_CACHE_ROOT"
}
```

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/app/` → PASS.

- [ ] **Step 3: Write the failing CLI test**

`apps/backend/cmd/converge-cli/main_test.go`:

```go
package main

import (
	"errors"
	"testing"

	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
)

func TestParseChanges(t *testing.T) {
	got, err := parseChanges("421,427, 435")
	if err != nil || len(got) != 3 || got[0] != 421 || got[2] != 435 {
		t.Fatalf("got %v err %v", got, err)
	}
	for _, bad := range []string{"", "a,b", "1,,2", "0", "-1"} {
		if _, err := parseChanges(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		s    session.Status
		code session.Code
		want int
	}{
		{"ready", session.StatusReady, "", 0},
		{"conflict", session.StatusConflicted, session.CodeConflict, 2},
		{"not merged", session.StatusFailed, session.CodeNotMerged, 3},
		{"targets", session.StatusFailed, session.CodeIncompatibleTargets, 3},
		{"not on base", session.StatusFailed, session.CodeNotOnBaseBranch, 3},
		{"missing commits", session.StatusFailed, session.CodeMissingCommits, 3},
		{"base undetermined", session.StatusFailed, session.CodeBaseUndetermined, 3},
		{"provider auth", session.StatusFailed, session.CodeProviderAuth, 4},
		{"provider unavailable", session.StatusFailed, session.CodeProviderUnavailable, 4},
		{"repository", session.StatusFailed, session.CodeRepositoryUnavailable, 4},
		{"git", session.StatusFailed, session.CodeGitFailure, 1},
		{"interrupted", session.StatusFailed, session.CodeInterrupted, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.s, tc.code); got != tc.want {
				t.Errorf("got %d want %d", got, tc.want)
			}
		})
	}
	if got := exitCodeForError(&review.InputError{Code: review.CodeInvalidChanges}); got != 3 {
		t.Errorf("input error = %d", got)
	}
	if got := exitCodeForError(&session.ReviewError{Code: session.CodeProviderAuth}); got != 4 {
		t.Errorf("review error = %d", got)
	}
	if got := exitCodeForError(errors.New("boom")); got != 1 {
		t.Errorf("plain error = %d", got)
	}
}
```

- [ ] **Step 4: Implement the CLI**

`apps/backend/cmd/converge-cli/main.go`:

```go
// Command converge-cli reconstructs a combined review without the web UI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jtumidanski/converge/internal/app"
	"github.com/jtumidanski/converge/internal/buildinfo"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func usage(w io.Writer) {
	fmt.Fprint(w, `converge-cli reconstructs the combined net diff of merged PRs/MRs.

Usage:
  converge-cli build --provider <id> --repo <owner/repo> [--base <branch>] --changes 421,427 [--out <dir>] [--cleanup]
  converge-cli --version

Exit codes: 0 ready, 2 conflict, 3 validation or base error, 4 provider error, 1 unexpected error.
`)
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 1
	}
	if args[0] == "--version" || args[0] == "-version" {
		fmt.Fprintln(stdout, buildinfo.Version)
		return 0
	}
	if args[0] != "build" {
		usage(stderr)
		return 1
	}
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	providerID := fs.String("provider", "", "configured provider id")
	repo := fs.String("repo", "", "repository as owner/name")
	base := fs.String("base", "", "base branch (defaults to the repository default)")
	changesRaw := fs.String("changes", "", "comma-separated PR/MR numbers")
	out := fs.String("out", "", "output directory (default ./converge-out/<session-id>)")
	cleanup := fs.Bool("cleanup", false, "remove the workspace after writing output")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	changes, err := parseChanges(*changesRaw)
	if err != nil {
		fmt.Fprintf(stderr, "converge-cli: %v\n", err)
		return 3
	}
	ctx := context.Background()
	application, err := app.New(ctx, os.Environ())
	if err != nil {
		fmt.Fprintf(stderr, "converge-cli: %v\n", err)
		return exitCodeForError(err)
	}
	defer func() { _ = application.Close() }()

	sess, err := application.Service.Create(ctx, review.CreateInput{ProviderID: *providerID, Repository: *repo, BaseBranch: *base, Changes: changes})
	if err != nil {
		fmt.Fprintf(stderr, "converge-cli: %v\n", err)
		return exitCodeForError(err)
	}
	final := application.Service.Build(ctx, sess.ID())

	dir := *out
	if dir == "" {
		dir = filepath.Join("converge-out", final.ID())
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		fmt.Fprintf(stderr, "converge-cli: create output directory: %v\n", err)
		return 1
	}
	record := session.ToRecord(final)
	metadata, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "converge-cli: encode metadata: %v\n", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), append(metadata, '\n'), 0o600); err != nil {
		fmt.Fprintf(stderr, "converge-cli: write metadata: %v\n", err)
		return 1
	}
	if final.Status() == session.StatusReady {
		src, err := application.Service.CombinedDiffPath(final.ID())
		if err == nil {
			if err := copyFile(src, filepath.Join(dir, review.CombinedDiffFile)); err != nil {
				fmt.Fprintf(stderr, "converge-cli: copy diff: %v\n", err)
				return 1
			}
		}
	}
	fmt.Fprintln(stdout, string(metadata))
	code := 0
	if re := final.Error(); re != nil {
		code = exitCodeFor(final.Status(), re.Code)
	} else {
		code = exitCodeFor(final.Status(), "")
	}
	if *cleanup {
		if err := application.Service.Finish(ctx, final.ID()); err != nil {
			fmt.Fprintf(stderr, "converge-cli: cleanup: %v\n", err)
		}
	}
	return code
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// parseChanges splits and validates the --changes value.
func parseChanges(raw string) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("--changes is required")
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid change number %q", strings.TrimSpace(p))
		}
		out = append(out, n)
	}
	return out, nil
}

// exitCodeFor maps a final status and code to a process exit code.
func exitCodeFor(status session.Status, code session.Code) int {
	switch status {
	case session.StatusReady:
		return 0
	case session.StatusConflicted:
		return 2
	}
	switch code {
	case session.CodeNotMerged, session.CodeIncompatibleTargets, session.CodeNotOnBaseBranch, session.CodeMissingCommits, session.CodeBaseUndetermined:
		return 3
	case session.CodeProviderAuth, session.CodeProviderUnavailable, session.CodeRepositoryUnavailable:
		return 4
	}
	return 1
}

// exitCodeForError maps a pre-build error to an exit code.
func exitCodeForError(err error) int {
	var ie *review.InputError
	if errors.As(err, &ie) {
		return 3
	}
	var re *session.ReviewError
	if errors.As(err, &re) {
		return exitCodeFor(session.StatusFailed, re.Code)
	}
	return 1
}
```

- [ ] **Step 5: Verify the CLI end to end against a local repository**

Run:

```bash
cd <worktree-root>/apps/backend && go test -race -count=1 ./cmd/... ./internal/app/ && CGO_ENABLED=0 go build ./... && go tool golangci-lint run
```

Expected: PASS. Then check `--version` prints something and `build` without flags exits 3:

```bash
cd <worktree-root>/apps/backend && go run ./cmd/converge-cli --version; go run ./cmd/converge-cli build; echo "exit=$?"
```

Expected: version printed; the second command prints `--changes is required` on stderr and `exit=3`.

- [ ] **Step 6: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/app apps/backend/cmd/converge-cli && git commit -m "feat(task-001): application wiring and converge-cli proof of concept"
```

---

## Phase E — HTTP API

### Task 18: JSON:API encoding and decoding

**Files:**
- Create: `apps/backend/internal/jsonapi/document.go`, `errors.go`, `decode.go`, `jsonapi_test.go`

**Interfaces:**
- Produces:
  - `const MediaType = "application/vnd.api+json"`.
  - `type Resource struct{ Type string; ID string; Attributes any; Relationships map[string]Relationship }` (JSON `type, id, attributes, relationships`).
  - `type Relationship struct{ Links Links }`; `type Links struct{ Related string }` (JSON `related`).
  - `type PageMeta struct{ Number, Size int; HasNext bool }` (JSON `number, size, hasNext`); `type Meta struct{ Page *PageMeta }` (JSON `page,omitempty`).
  - `func WriteOne(w http.ResponseWriter, status int, r Resource) error`.
  - `func WriteList(w http.ResponseWriter, status int, rs []Resource, meta *Meta) error`.
  - `type Error struct{ Status string; Code string; Title string; Detail string; Meta map[string]any }`.
  - `func WriteErrors(w http.ResponseWriter, status int, errs ...Error) error`.
  - `func WriteError(w http.ResponseWriter, status int, code, title, detail string) error`.
  - `func Decode[T any](r *http.Request, wantType string) (T, error)` — reads at most 1 MiB, requires `data.type == wantType`, unmarshals `data.attributes` into `T` with `DisallowUnknownFields`, returns `*DecodeError` on failure.
  - `type DecodeError struct{ Detail string }` implementing `error`.

- [ ] **Step 1: Write the failing test**

`apps/backend/internal/jsonapi/jsonapi_test.go`:

```go
package jsonapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type attrs struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func TestWriteOne(t *testing.T) {
	w := httptest.NewRecorder()
	err := WriteOne(w, http.StatusCreated, Resource{
		Type: "reviews", ID: "7f14b2c8", Attributes: attrs{Name: "x", N: 1},
		Relationships: map[string]Relationship{"files": {Links: Links{Related: "/api/reviews/7f14b2c8/files"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusCreated || w.Header().Get("Content-Type") != MediaType {
		t.Fatalf("code=%d ct=%q", w.Code, w.Header().Get("Content-Type"))
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	data := got["data"].(map[string]any)
	if data["type"] != "reviews" || data["id"] != "7f14b2c8" {
		t.Errorf("data = %v", data)
	}
	if data["attributes"].(map[string]any)["name"] != "x" {
		t.Errorf("attributes = %v", data["attributes"])
	}
	rel := data["relationships"].(map[string]any)["files"].(map[string]any)["links"].(map[string]any)
	if rel["related"] != "/api/reviews/7f14b2c8/files" {
		t.Errorf("relationships = %v", rel)
	}
}

func TestWriteListAlwaysHasArrayAndMeta(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteList(w, http.StatusOK, nil, &Meta{Page: &PageMeta{Number: 1, Size: 30, HasNext: true}}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"data":[]`) {
		t.Errorf("empty list must serialise as []: %s", body)
	}
	if !strings.Contains(body, `"hasNext":true`) || !strings.Contains(body, `"size":30`) {
		t.Errorf("meta missing: %s", body)
	}
	w2 := httptest.NewRecorder()
	if err := WriteList(w2, http.StatusOK, []Resource{{Type: "providers", ID: "gh", Attributes: attrs{Name: "GitHub"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w2.Body.String(), `"meta"`) {
		t.Errorf("nil meta must be omitted: %s", w2.Body.String())
	}
}

func TestWriteErrors(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteError(w, http.StatusBadRequest, "INVALID_CHANGES", "Invalid request", "Select at least one PR/MR."); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", w.Code)
	}
	var doc struct {
		Errors []struct {
			Status, Code, Title, Detail string
		} `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Errors) != 1 || doc.Errors[0].Status != "400" || doc.Errors[0].Code != "INVALID_CHANGES" || doc.Errors[0].Detail == "" {
		t.Errorf("errors = %+v", doc.Errors)
	}
}

func TestDecode(t *testing.T) {
	body := `{"data":{"type":"reviews","attributes":{"name":"x","n":3}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/reviews", strings.NewReader(body))
	got, err := Decode[attrs](req, "reviews")
	if err != nil || got.Name != "x" || got.N != 3 {
		t.Fatalf("got %+v err %v", got, err)
	}
	cases := map[string]string{
		"wrong type":     `{"data":{"type":"widgets","attributes":{}}}`,
		"missing data":   `{}`,
		"unknown field":  `{"data":{"type":"reviews","attributes":{"name":"x","nope":1}}}`,
		"malformed json": `{`,
		"array data":     `{"data":[]}`,
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/reviews", strings.NewReader(b))
			if _, err := Decode[attrs](req, "reviews"); err == nil {
				t.Fatal("accepted")
			} else {
				var de *DecodeError
				if !asDecodeError(err, &de) {
					t.Fatalf("err = %v, want *DecodeError", err)
				}
			}
		})
	}
}

func asDecodeError(err error, target **DecodeError) bool {
	de, ok := err.(*DecodeError)
	if ok {
		*target = de
	}
	return ok
}
```

- [ ] **Step 2: Implement the package**

`apps/backend/internal/jsonapi/document.go`:

```go
// Package jsonapi encodes and decodes the subset of JSON:API that Converge uses.
package jsonapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// MediaType is the JSON:API content type.
const MediaType = "application/vnd.api+json"

// Links holds a related link.
type Links struct {
	Related string `json:"related,omitempty"`
}

// Relationship is a links-only relationship.
type Relationship struct {
	Links Links `json:"links"`
}

// Resource is one JSON:API resource object.
type Resource struct {
	Type          string                  `json:"type"`
	ID            string                  `json:"id"`
	Attributes    any                     `json:"attributes,omitempty"`
	Relationships map[string]Relationship `json:"relationships,omitempty"`
}

// PageMeta describes a page of results.
type PageMeta struct {
	Number  int  `json:"number"`
	Size    int  `json:"size"`
	HasNext bool `json:"hasNext"`
}

// Meta is the document-level meta member.
type Meta struct {
	Page *PageMeta `json:"page,omitempty"`
}

type singleDocument struct {
	Data Resource `json:"data"`
}

type listDocument struct {
	Data []Resource `json:"data"`
	Meta *Meta      `json:"meta,omitempty"`
}

func write(w http.ResponseWriter, status int, doc any) error {
	w.Header().Set("Content-Type", MediaType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		return fmt.Errorf("jsonapi: encode: %w", err)
	}
	return nil
}

// WriteOne writes a single-resource document.
func WriteOne(w http.ResponseWriter, status int, r Resource) error {
	return write(w, status, singleDocument{Data: r})
}

// WriteList writes a resource-collection document; nil resources serialise as [].
func WriteList(w http.ResponseWriter, status int, rs []Resource, meta *Meta) error {
	if rs == nil {
		rs = []Resource{}
	}
	return write(w, status, listDocument{Data: rs, Meta: meta})
}
```

`apps/backend/internal/jsonapi/errors.go`:

```go
package jsonapi

import (
	"net/http"
	"strconv"
)

// Error is one JSON:API error object.
type Error struct {
	Status string         `json:"status"`
	Code   string         `json:"code"`
	Title  string         `json:"title"`
	Detail string         `json:"detail,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
}

type errorDocument struct {
	Errors []Error `json:"errors"`
}

// WriteErrors writes an error document.
func WriteErrors(w http.ResponseWriter, status int, errs ...Error) error {
	return write(w, status, errorDocument{Errors: errs})
}

// WriteError writes a single error document.
func WriteError(w http.ResponseWriter, status int, code, title, detail string) error {
	return WriteErrors(w, status, Error{Status: strconv.Itoa(status), Code: code, Title: title, Detail: detail})
}

// StatusTitle returns a human title for an HTTP status.
func StatusTitle(status int) string {
	if t := http.StatusText(status); t != "" {
		return t
	}
	return "Error"
}
```

`apps/backend/internal/jsonapi/decode.go`:

```go
package jsonapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// MaxBodyBytes bounds request bodies.
const MaxBodyBytes = 1 << 20

// DecodeError is a client-fixable request body problem.
type DecodeError struct{ Detail string }

func (e *DecodeError) Error() string { return e.Detail }

type requestDocument struct {
	Data struct {
		Type       string          `json:"type"`
		ID         string          `json:"id"`
		Attributes json.RawMessage `json:"attributes"`
	} `json:"data"`
}

// Decode unwraps data.attributes into T and checks data.type.
func Decode[T any](r *http.Request, wantType string) (T, error) {
	var zero T
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes))
	if err != nil {
		return zero, &DecodeError{Detail: "The request body could not be read."}
	}
	var doc requestDocument
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&doc); err != nil {
		return zero, &DecodeError{Detail: "The request body is not a valid JSON:API document."}
	}
	if doc.Data.Type == "" {
		return zero, &DecodeError{Detail: `The request body must contain a "data" object with a "type".`}
	}
	if doc.Data.Type != wantType {
		return zero, &DecodeError{Detail: fmt.Sprintf("Expected resource type %q, got %q.", wantType, doc.Data.Type)}
	}
	if len(doc.Data.Attributes) == 0 {
		return zero, &DecodeError{Detail: `The request body must contain "data.attributes".`}
	}
	attrDec := json.NewDecoder(bytes.NewReader(doc.Data.Attributes))
	attrDec.DisallowUnknownFields()
	var out T
	if err := attrDec.Decode(&out); err != nil {
		return zero, &DecodeError{Detail: "The request attributes are invalid: " + err.Error()}
	}
	return out, nil
}
```

- [ ] **Step 3: Run and commit**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/jsonapi/ && go tool golangci-lint run ./internal/jsonapi/...`
Expected: PASS.

```bash
cd <worktree-root> && git add apps/backend/internal/jsonapi && git commit -m "feat(task-001): JSON:API document encoding and request decoding"
```

---

### Task 19: HTTP handlers, router, middleware, embedded UI serving

**Files:**
- Create: `apps/backend/internal/api/errors.go`, `middleware.go`, `router.go`, `providers.go`, `repositories.go`, `changes.go`, `reviews.go`, `review_files.go`, `health.go`, `ui.go`, `api_test.go`, `ui_test.go`

**Interfaces:**
- Consumes: `review.Service`, `provider.Registry`, `session.*`, `diff.*`, `jsonapi.*`, `ui.FS`, `buildinfo.Version`.
- Produces:
  - `type Deps struct{ Service *review.Service; Providers *provider.Registry; Log *slog.Logger; UI fs.FS; UIPresent bool; BuildContext context.Context }` — `BuildContext` bounds asynchronous builds started by `POST /api/reviews` and defaults to `context.Background()` when nil; Task 20 passes the server's lifetime context.
  - `func NewRouter(d Deps) http.Handler` — the fully wrapped handler (middleware applied).
  - Routes: `GET /healthz`; `GET /api/providers`; `GET /api/providers/{provider}/repositories`; `GET /api/providers/{provider}/repositories/{repo}`; `GET /api/providers/{provider}/repositories/{repo}/changes`; `POST /api/reviews`; `GET /api/reviews`; `GET /api/reviews/{id}`; `DELETE /api/reviews/{id}`; `GET /api/reviews/{id}/files`; `GET /api/reviews/{id}/files/{path...}`; `GET /api/reviews/{id}/diff`; `GET /` (SPA).
  - Resource types: `providers`, `repositories`, `changes`, `reviews`, `review-files`, `review-file-diffs`.
  - `func writeDomainError(w http.ResponseWriter, log *slog.Logger, err error)` mapping per design §9.2.

- [ ] **Step 1: Write the failing handler tests**

`apps/backend/internal/api/api_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
	"github.com/jtumidanski/converge/internal/workspace"
)

type apiFixture struct {
	handler http.Handler
	svc     *review.Service
	prov    *fake.Provider
	src     *testutil.Repo
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Branch("feat/a")
	c := src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	sq := src.Squash("feat/a", "squash a")
	src.Push()

	log := testLogger()
	runner, err := gitx.NewExecRunner(log, gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	locks := &gitx.LockMap{}
	mirrors := mirror.New(t.TempDir(), runner, locks, log)
	ws, err := workspace.New(t.TempDir(), runner, locks, log)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetName("server").
		SetNamespace("atlas").SetDefaultBranch("main").SetWebURL("https://example.test/atlas/server").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)
	commit, _ := provider.NewCommit(c, "a1", time.Now())
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(421).SetTitle("Add a").
		SetAuthor("jsmith").SetWebURL("https://example.test/mr/421").SetSourceBranch("feat/a").SetTargetBranch("main").
		SetState(provider.StateMerged).SetMergedAt(time.Now()).SetSquashCommitSHA(sq).SetHeadSHA(c).SetCommits([]provider.Commit{commit}).Build()
	if err != nil {
		t.Fatal(err)
	}
	p.AddChange(cr)
	registry := provider.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	cleaner := review.NewCleaner(mirrors, ws, log)
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, log, time.Now)
	svc := review.NewService(review.Deps{
		Providers: registry, Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: review.NewCherryPickApplicator(runner, log), Runner: runner, Log: log,
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 2, Now: time.Now,
	})
	h := NewRouter(Deps{Service: svc, Providers: registry, Log: log})
	return &apiFixture{handler: h, svc: svc, prov: p, src: src}
}

func do(t *testing.T, h http.Handler, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var doc struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return doc.Data
}

func decodeOne(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var doc struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return doc.Data
}

func TestHealthz(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, http.MethodGet, "/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["version"] == "" {
		t.Errorf("body = %v", body)
	}
}

func TestProvidersEndpoint(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, http.MethodGet, "/api/providers", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/vnd.api+json" {
		t.Fatalf("code=%d ct=%q", w.Code, w.Header().Get("Content-Type"))
	}
	data := decodeList(t, w)
	if len(data) != 1 || data[0]["id"] != "fake" || data[0]["type"] != "providers" {
		t.Fatalf("data = %v", data)
	}
	attrs := data[0]["attributes"].(map[string]any)
	if attrs["kind"] != "gitlab" || attrs["displayName"] == "" {
		t.Errorf("attributes = %v", attrs)
	}
	if strings.Contains(w.Body.String(), "token") || strings.Contains(w.Body.String(), "Token") {
		t.Error("token key present in provider response")
	}
}

func TestRepositoriesAndChanges(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories", "")
	if w.Code != http.StatusOK || len(decodeList(t, w)) != 1 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	// %2F-encoded repository id resolves to a single path segment
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/server"), "")
	if w.Code != http.StatusOK {
		t.Fatalf("get repo: %d %s", w.Code, w.Body.String())
	}
	one := decodeOne(t, w)
	if one["id"] != "atlas/server" || one["attributes"].(map[string]any)["defaultBranch"] != "main" {
		t.Errorf("repo = %v", one)
	}
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/missing"), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("missing repo: %d", w.Code)
	}
	w = do(t, f.handler, http.MethodGet, "/api/providers/nope/repositories", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown provider: %d", w.Code)
	}
	// changes
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/server")+"/changes?state=merged&target=main", "")
	if w.Code != http.StatusOK {
		t.Fatalf("changes: %d %s", w.Code, w.Body.String())
	}
	data := decodeList(t, w)
	if len(data) != 1 || data[0]["id"] != "421" {
		t.Fatalf("changes = %v", data)
	}
	attrs := data[0]["attributes"].(map[string]any)
	for _, key := range []string{"number", "title", "author", "sourceBranch", "targetBranch", "mergedAt", "webUrl", "landingSha"} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("attribute %s missing: %v", key, attrs)
		}
	}
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/server")+"/changes?state=open", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("state=open: %d", w.Code)
	}
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/server")+"/changes?target=main&pageSize=500", "")
	if w.Code != http.StatusOK {
		t.Errorf("pageSize clamp: %d %s", w.Code, w.Body.String())
	}
}

func TestReviewLifecycleEndpoints(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, http.MethodPost, "/api/reviews", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","baseBranch":"main","changes":[421]}}}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("post: %d %s", w.Code, w.Body.String())
	}
	created := decodeOne(t, w)
	id := created["id"].(string)
	if created["attributes"].(map[string]any)["status"] != "CREATING" {
		t.Errorf("attributes = %v", created["attributes"])
	}
	// wait for the async build
	deadline := time.Now().Add(60 * time.Second)
	var final map[string]any
	for {
		w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id, "")
		if w.Code != http.StatusOK {
			t.Fatalf("get: %d %s", w.Code, w.Body.String())
		}
		final = decodeOne(t, w)
		status := final["attributes"].(map[string]any)["status"]
		if status != "CREATING" {
			if status != "READY" {
				t.Fatalf("status = %v attrs=%v", status, final["attributes"])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("build did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	attrs := final["attributes"].(map[string]any)
	for _, key := range []string{"baseSha", "headSha", "baseDescription", "included", "totals"} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("attribute %s missing: %v", key, attrs)
		}
	}
	if rel, ok := final["relationships"].(map[string]any); !ok || rel["files"] == nil {
		t.Errorf("relationships = %v", final["relationships"])
	}
	// list
	w = do(t, f.handler, http.MethodGet, "/api/reviews", "")
	if w.Code != http.StatusOK || len(decodeList(t, w)) != 1 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	// files
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files", "")
	if w.Code != http.StatusOK {
		t.Fatalf("files: %d %s", w.Code, w.Body.String())
	}
	files := decodeList(t, w)
	if len(files) != 1 || files[0]["type"] != "review-files" || files[0]["id"] != "a.txt" {
		t.Fatalf("files = %v", files)
	}
	// file diff by path segment and by query
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files/a.txt", "")
	if w.Code != http.StatusOK {
		t.Fatalf("file diff: %d %s", w.Code, w.Body.String())
	}
	one := decodeOne(t, w)
	if one["type"] != "review-file-diffs" || !strings.Contains(one["attributes"].(map[string]any)["diff"].(string), "+a") {
		t.Errorf("diff = %v", one)
	}
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files?path=a.txt", "")
	if w.Code != http.StatusOK {
		t.Errorf("query path form: %d %s", w.Code, w.Body.String())
	}
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files/nope.txt", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown file: %d", w.Code)
	}
	// raw combined diff
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/diff", "")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") || !strings.Contains(w.Body.String(), "a.txt") {
		t.Errorf("raw diff: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	// delete is idempotent and always 204
	for i := 0; i < 2; i++ {
		w = do(t, f.handler, http.MethodDelete, "/api/reviews/"+id, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	w = do(t, f.handler, http.MethodDelete, "/api/reviews/ffffffff", "")
	if w.Code != http.StatusNoContent {
		t.Errorf("delete unknown: %d", w.Code)
	}
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files", "")
	if w.Code != http.StatusConflict {
		t.Errorf("files after finish: %d", w.Code)
	}
	var errDoc struct {
		Errors []struct{ Code string } `json:"errors"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &errDoc)
	if len(errDoc.Errors) != 1 || errDoc.Errors[0].Code != "REVIEW_NOT_READY" {
		t.Errorf("error doc = %s", w.Body.String())
	}
}

func TestReviewValidationErrors(t *testing.T) {
	f := newAPIFixture(t)
	cases := []struct {
		name string
		body string
		code string
	}{
		{"no provider", `{"data":{"type":"reviews","attributes":{"repository":"atlas/server","changes":[1]}}}`, "INVALID_PROVIDER"},
		{"bad repo", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"../x","changes":[1]}}}`, "INVALID_REPOSITORY"},
		{"bad branch", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","baseBranch":"-x","changes":[1]}}}`, "INVALID_BRANCH"},
		{"no changes", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","changes":[]}}}`, "INVALID_CHANGES"},
		{"duplicate changes", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","changes":[1,1]}}}`, "INVALID_CHANGES"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, f.handler, http.MethodPost, "/api/reviews", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
			}
			var doc struct {
				Errors []struct{ Code string } `json:"errors"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &doc)
			if len(doc.Errors) != 1 || doc.Errors[0].Code != tc.code {
				t.Errorf("errors = %s want %s", w.Body.String(), tc.code)
			}
		})
	}
	w := do(t, f.handler, http.MethodPost, "/api/reviews", `{"data":{"type":"widgets","attributes":{}}}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("wrong type: %d", w.Code)
	}
	w = do(t, f.handler, http.MethodGet, "/api/reviews/not-an-id", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("bad id: %d", w.Code)
	}
	w = do(t, f.handler, http.MethodPut, "/api/reviews", "")
	if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
		t.Errorf("PUT: %d", w.Code)
	}
}

func TestProviderErrorsMapToStatuses(t *testing.T) {
	f := newAPIFixture(t)
	f.prov.FailWith(provider.ErrAuth)
	w := do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories", "")
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "PROVIDER_AUTH") {
		t.Errorf("auth: %d %s", w.Code, w.Body.String())
	}
	f.prov.FailWith(provider.ErrUnavailable)
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories", "")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "PROVIDER_UNAVAILABLE") {
		t.Errorf("unavailable: %d %s", w.Code, w.Body.String())
	}
	_ = context.Background()
}
```

Add a `testLogger()` helper in the same package (`api_test.go` top) returning a warn-level text logger to stderr.

`apps/backend/internal/api/ui_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestUIHandlerServesSPAWithFallback(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":            &fstest.MapFile{Data: []byte("<html>app</html>")},
		"assets/app-abc123.js":  &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	h := uiHandler(fsys, true)
	for _, path := range []string{"/", "/reviews/7f14b2c8", "/select"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || w.Body.String() != "<html>app</html>" {
			t.Errorf("%s: %d %q", path, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s cache header = %q", path, w.Header().Get("Cache-Control"))
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") == "no-cache" {
		t.Errorf("asset: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	// traversal is refused
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/../secret", nil))
	if w.Code == http.StatusOK && w.Body.String() != "<html>app</html>" {
		t.Errorf("traversal served %q", w.Body.String())
	}
}

func TestUIHandlerWithoutBuild(t *testing.T) {
	h := uiHandler(fstest.MapFS{}, false)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d", w.Code)
	}
	if !contains(w.Body.String(), "make build") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run to see failures**

Run: `cd <worktree-root>/apps/backend && go test ./internal/api/`
Expected: compile failure.

- [ ] **Step 3: Implement error mapping and middleware**

`apps/backend/internal/api/errors.go`:

```go
// Package api exposes the HTTP surface: JSON:API handlers plus the embedded UI.
package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
)

// writeDomainError maps a domain error onto a JSON:API error document (design §9.2).
func writeDomainError(w http.ResponseWriter, log *slog.Logger, err error) {
	status, code, detail := classify(err)
	if status >= 500 {
		log.Error("request failed", slog.String("code", code), slog.String("error", err.Error()))
	}
	if writeErr := jsonapi.WriteError(w, status, code, jsonapi.StatusTitle(status), detail); writeErr != nil {
		log.Error("write error document failed", slog.String("error", writeErr.Error()))
	}
}

func classify(err error) (int, string, string) {
	var ie *review.InputError
	if errors.As(err, &ie) {
		return http.StatusBadRequest, string(ie.Code), ie.Message
	}
	var de *jsonapi.DecodeError
	if errors.As(err, &de) {
		return http.StatusBadRequest, "INVALID_REQUEST", de.Detail
	}
	var re *session.ReviewError
	if errors.As(err, &re) {
		switch re.Code {
		case session.CodeProviderAuth:
			return http.StatusBadGateway, string(re.Code), re.Message
		case session.CodeProviderUnavailable:
			return http.StatusServiceUnavailable, string(re.Code), re.Message
		case session.CodeRepositoryUnavailable:
			return http.StatusNotFound, string(re.Code), re.Message
		default:
			return http.StatusBadRequest, string(re.Code), re.Message
		}
	}
	switch {
	case errors.Is(err, provider.ErrAuth):
		return http.StatusBadGateway, "PROVIDER_AUTH", "The configured provider token was rejected."
	case errors.Is(err, provider.ErrNotFound), errors.Is(err, session.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND", "The requested resource does not exist."
	case errors.Is(err, provider.ErrUnavailable):
		return http.StatusServiceUnavailable, "PROVIDER_UNAVAILABLE", "The provider is unavailable right now."
	case errors.Is(err, review.ErrNotReady):
		return http.StatusConflict, "REVIEW_NOT_READY", "This review is not ready yet."
	}
	return http.StatusInternalServerError, "INTERNAL", "An unexpected error occurred."
}
```

`apps/backend/internal/api/middleware.go`:

```go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/jsonapi"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

// withMiddleware applies request id, logging, recovery, and content negotiation.
func withMiddleware(h http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-Id", requestID)
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic serving request", slog.String("request_id", requestID), slog.String("path", r.URL.Path), slog.Any("panic", rec))
				if sw.status == 0 {
					_ = jsonapi.WriteError(sw, http.StatusInternalServerError, "INTERNAL", "Internal Server Error", "An unexpected error occurred.")
				}
			}
			log.Info("request",
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		}()
		if strings.HasPrefix(r.URL.Path, "/api/") && !acceptable(r.Header.Get("Accept")) {
			_ = jsonapi.WriteError(sw, http.StatusNotAcceptable, "NOT_ACCEPTABLE", "Not Acceptable", "This endpoint returns "+jsonapi.MediaType+".")
			return
		}
		h.ServeHTTP(sw, r)
	})
}

// acceptable allows */*, application/json and the JSON:API media type.
func acceptable(accept string) bool {
	if strings.TrimSpace(accept) == "" {
		return true
	}
	for _, part := range strings.Split(accept, ",") {
		media := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if media == "*/*" || media == "application/*" || media == "application/json" || media == jsonapi.MediaType {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Implement the resource handlers**

`apps/backend/internal/api/providers.go`:

```go
package api

import (
	"net/http"

	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
)

type providerAttributes struct {
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"baseUrl"`
}

func providerResource(p provider.GitProvider) jsonapi.Resource {
	return jsonapi.Resource{Type: "providers", ID: p.ID(), Attributes: providerAttributes{
		DisplayName: p.DisplayName(), Kind: string(p.Kind()), BaseURL: p.BaseURL(),
	}}
}

func (s *server) listProviders(w http.ResponseWriter, _ *http.Request) {
	all := s.deps.Providers.All()
	out := make([]jsonapi.Resource, 0, len(all))
	for _, p := range all {
		out = append(out, providerResource(p))
	}
	if err := jsonapi.WriteList(w, http.StatusOK, out, nil); err != nil {
		s.deps.Log.Error("write providers failed", "error", err)
	}
}
```

`apps/backend/internal/api/repositories.go`:

```go
package api

import (
	"net/http"
	"strconv"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
)

type repositoryAttributes struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace"`
	DefaultBranch string `json:"defaultBranch"`
	WebURL        string `json:"webUrl"`
}

func repositoryResource(r provider.Repository) jsonapi.Resource {
	return jsonapi.Resource{Type: "repositories", ID: r.FullName(), Attributes: repositoryAttributes{
		Name: r.Name(), Namespace: r.Namespace(), DefaultBranch: r.DefaultBranch(), WebURL: r.WebURL(),
	}}
}

// pageFrom reads ?page= and ?pageSize=.
func pageFrom(r *http.Request) provider.Page {
	p := provider.Page{}
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil {
		p.Number = v
	}
	size := r.URL.Query().Get("pageSize")
	if size == "" {
		size = r.URL.Query().Get("size")
	}
	if v, err := strconv.Atoi(size); err == nil {
		p.Size = v
	}
	return p.Normalize()
}

func (s *server) providerFor(w http.ResponseWriter, r *http.Request) (provider.GitProvider, bool) {
	id := r.PathValue("provider")
	p, ok := s.deps.Providers.Get(id)
	if !ok {
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Not Found", "No provider is configured with that id.")
		return nil, false
	}
	return p, true
}

// repoNameFrom reads {repo} (already percent-decoded by ServeMux) or ?repo=.
func repoNameFrom(r *http.Request) (string, error) {
	name := r.PathValue("repo")
	if name == "" {
		name = r.URL.Query().Get("repo")
	}
	if err := gitx.ValidateRepoFullName(name); err != nil {
		return "", err
	}
	return name, nil
}

func (s *server) listRepositories(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	page := pageFrom(r)
	res, err := p.ListRepositories(r.Context(), page)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	out := make([]jsonapi.Resource, 0, len(res.Items))
	for _, repo := range res.Items {
		out = append(out, repositoryResource(repo))
	}
	meta := &jsonapi.Meta{Page: &jsonapi.PageMeta{Number: page.Number, Size: page.Size, HasNext: res.HasNext}}
	if err := jsonapi.WriteList(w, http.StatusOK, out, meta); err != nil {
		s.deps.Log.Error("write repositories failed", "error", err)
	}
}

func (s *server) getRepository(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	name, err := repoNameFrom(r)
	if err != nil {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_REPOSITORY", "Bad Request", "The repository must look like owner/name.")
		return
	}
	repo, err := p.GetRepository(r.Context(), name)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusOK, repositoryResource(repo)); err != nil {
		s.deps.Log.Error("write repository failed", "error", err)
	}
}
```

`apps/backend/internal/api/changes.go`:

```go
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
)

type changeAttributes struct {
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	Author       string     `json:"author"`
	SourceBranch string     `json:"sourceBranch"`
	TargetBranch string     `json:"targetBranch"`
	MergedAt     *time.Time `json:"mergedAt"`
	CreatedAt    *time.Time `json:"createdAt"`
	LandingSHA   *string    `json:"landingSha"`
	WebURL       string     `json:"webUrl"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func changeResource(c provider.ChangeRequest) jsonapi.Resource {
	var landing *string
	for _, candidate := range []string{c.MergeCommitSHA(), c.SquashCommitSHA()} {
		if candidate != "" {
			v := candidate
			landing = &v
			break
		}
	}
	return jsonapi.Resource{Type: "changes", ID: strconv.Itoa(c.Number()), Attributes: changeAttributes{
		Number: c.Number(), Title: c.Title(), Author: c.Author(), SourceBranch: c.SourceBranch(), TargetBranch: c.TargetBranch(),
		MergedAt: timePtr(c.MergedAt()), CreatedAt: timePtr(c.CreatedAt()), LandingSHA: landing, WebURL: c.WebURL(),
	}}
}

func (s *server) listChanges(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	name, err := repoNameFrom(r)
	if err != nil {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_REPOSITORY", "Bad Request", "The repository must look like owner/name.")
		return
	}
	q := r.URL.Query()
	if state := q.Get("state"); state != "" && state != "merged" {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_STATE", "Bad Request", "Only state=merged is supported.")
		return
	}
	repo, err := p.GetRepository(r.Context(), name)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	target := q.Get("target")
	if target == "" {
		target = repo.DefaultBranch()
	}
	page := pageFrom(r)
	res, err := p.ListMergedChanges(r.Context(), repo, target, q.Get("search"), page)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	out := make([]jsonapi.Resource, 0, len(res.Items))
	for _, c := range res.Items {
		out = append(out, changeResource(c))
	}
	meta := &jsonapi.Meta{Page: &jsonapi.PageMeta{Number: page.Number, Size: page.Size, HasNext: res.HasNext}}
	if err := jsonapi.WriteList(w, http.StatusOK, out, meta); err != nil {
		s.deps.Log.Error("write changes failed", "error", err)
	}
}
```

`apps/backend/internal/api/reviews.go`:

```go
package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/workspace"
)

type createReviewAttributes struct {
	Provider   string `json:"provider"`
	Repository string `json:"repository"`
	BaseBranch string `json:"baseBranch"`
	Changes    []int  `json:"changes"`
}

type includedChange struct {
	Number   int        `json:"number"`
	Title    string     `json:"title"`
	Author   string     `json:"author"`
	MergedAt *time.Time `json:"mergedAt"`
	WebURL   string     `json:"webUrl"`
	Strategy string     `json:"strategy"`
}

type reviewAttributes struct {
	Status          string               `json:"status"`
	Stage           *string              `json:"stage"`
	Provider        string               `json:"provider"`
	Repository      string               `json:"repository"`
	BaseBranch      string               `json:"baseBranch"`
	BaseSHA         *string              `json:"baseSha"`
	HeadSHA         *string              `json:"headSha"`
	BaseDescription string               `json:"baseDescription"`
	Changes         []int                `json:"changes"`
	Included        []includedChange     `json:"included"`
	Totals          *diff.Totals         `json:"totals"`
	Error           *session.ReviewError `json:"error"`
	CreatedAt       time.Time            `json:"createdAt"`
	UpdatedAt       time.Time            `json:"updatedAt"`
	ExpiresAt       time.Time            `json:"expiresAt"`
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// baseDescription renders "Immediately before #421".
func baseDescription(s session.Session) string {
	if rc, ok := s.EarliestChange(); ok {
		return fmt.Sprintf("Immediately before #%d", rc.Number())
	}
	return ""
}

func reviewResource(s session.Session) jsonapi.Resource {
	included := make([]includedChange, 0, len(s.ResolvedChanges()))
	for _, rc := range s.ResolvedChanges() {
		included = append(included, includedChange{Number: rc.Number(), Title: rc.Title(), Author: rc.Author(), MergedAt: timePtr(rc.MergedAt()), WebURL: rc.WebURL(), Strategy: string(rc.Strategy())})
	}
	return jsonapi.Resource{
		Type: "reviews", ID: s.ID(),
		Attributes: reviewAttributes{
			Status: string(s.Status()), Stage: stringPtr(s.Stage()), Provider: s.ProviderID(), Repository: s.Repository(),
			BaseBranch: s.BaseBranch(), BaseSHA: stringPtr(s.BaseSHA()), HeadSHA: stringPtr(s.HeadSHA()),
			BaseDescription: baseDescription(s), Changes: s.RequestedChanges(), Included: included, Totals: s.Totals(),
			Error: s.Error(), CreatedAt: s.CreatedAt(), UpdatedAt: s.UpdatedAt(), ExpiresAt: s.ExpiresAt(),
		},
		Relationships: map[string]jsonapi.Relationship{
			"files": {Links: jsonapi.Links{Related: "/api/reviews/" + s.ID() + "/files"}},
		},
	}
}

func (s *server) createReview(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[createReviewAttributes](r, "reviews")
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	sess, err := s.deps.Service.Create(r.Context(), review.CreateInput{
		ProviderID: attrs.Provider, Repository: attrs.Repository, BaseBranch: attrs.BaseBranch, Changes: attrs.Changes,
	})
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	s.deps.Service.StartBuild(s.buildCtx, sess.ID())
	if err := jsonapi.WriteOne(w, http.StatusAccepted, reviewResource(sess)); err != nil {
		s.deps.Log.Error("write review failed", "error", err)
	}
}

func (s *server) listReviews(w http.ResponseWriter, _ *http.Request) {
	sessions := s.deps.Service.List()
	out := make([]jsonapi.Resource, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, reviewResource(sess))
	}
	if err := jsonapi.WriteList(w, http.StatusOK, out, nil); err != nil {
		s.deps.Log.Error("write reviews failed", "error", err)
	}
}

// sessionFor resolves {id} or writes 404.
func (s *server) sessionFor(w http.ResponseWriter, r *http.Request) (session.Session, bool) {
	id := r.PathValue("id")
	if err := workspace.ValidateSessionID(id); err != nil {
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Not Found", "No review exists with that id.")
		return session.Session{}, false
	}
	sess, ok := s.deps.Service.Get(id)
	if !ok {
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Not Found", "No review exists with that id.")
		return session.Session{}, false
	}
	return sess, true
}

func (s *server) getReview(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFor(w, r)
	if !ok {
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusOK, reviewResource(sess)); err != nil {
		s.deps.Log.Error("write review failed", "error", err)
	}
}

// deleteReview is idempotent and always answers 204.
func (s *server) deleteReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := workspace.ValidateSessionID(id); err == nil {
		if err := s.deps.Service.Finish(r.Context(), id); err != nil && !errors.Is(err, session.ErrNotFound) {
			s.deps.Log.Warn("finish failed", "session", id, "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
```

`apps/backend/internal/api/review_files.go`:

```go
package api

import (
	"net/http"
	"os"

	"github.com/jtumidanski/converge/internal/diff"
	"github.com/jtumidanski/converge/internal/jsonapi"
)

type reviewFileAttributes struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
	Status       string `json:"status"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Binary       bool   `json:"binary"`
}

type reviewFileDiffAttributes struct {
	reviewFileAttributes
	Truncated bool   `json:"truncated"`
	Diff      string `json:"diff"`
}

func fileAttrs(f diff.FileSummary) reviewFileAttributes {
	return reviewFileAttributes{Path: f.Path, PreviousPath: f.PreviousPath, Status: string(f.Status), Additions: f.Additions, Deletions: f.Deletions, Binary: f.Binary}
}

func (s *server) listReviewFiles(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFor(w, r)
	if !ok {
		return
	}
	// The query form ?path= renders one file instead of the list.
	if path := r.URL.Query().Get("path"); path != "" {
		s.writeFileDiff(w, r, sess.ID(), path)
		return
	}
	files, err := s.deps.Service.Files(sess.ID())
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	out := make([]jsonapi.Resource, 0, len(files))
	for _, f := range files {
		out = append(out, jsonapi.Resource{Type: "review-files", ID: f.Path, Attributes: fileAttrs(f)})
	}
	if err := jsonapi.WriteList(w, http.StatusOK, out, nil); err != nil {
		s.deps.Log.Error("write review files failed", "error", err)
	}
}

func (s *server) getReviewFile(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFor(w, r)
	if !ok {
		return
	}
	path := r.PathValue("path")
	if path == "" {
		path = r.URL.Query().Get("path")
	}
	s.writeFileDiff(w, r, sess.ID(), path)
}

func (s *server) writeFileDiff(w http.ResponseWriter, r *http.Request, id, path string) {
	fd, err := s.deps.Service.FileDiff(r.Context(), id, path)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	res := jsonapi.Resource{Type: "review-file-diffs", ID: fd.Path, Attributes: reviewFileDiffAttributes{
		reviewFileAttributes: fileAttrs(fd.FileSummary), Truncated: fd.Truncated, Diff: fd.Diff,
	}}
	if err := jsonapi.WriteOne(w, http.StatusOK, res); err != nil {
		s.deps.Log.Error("write file diff failed", "error", err)
	}
}

// getReviewDiff streams combined.diff as text/plain.
func (s *server) getReviewDiff(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFor(w, r)
	if !ok {
		return
	}
	path, err := s.deps.Service.CombinedDiffPath(sess.ID())
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="combined.diff"`)
	http.ServeContent(w, r, "combined.diff", sess.UpdatedAt(), f)
}
```

`apps/backend/internal/api/health.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"os/exec"

	"github.com/jtumidanski/converge/internal/buildinfo"
)

type healthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Checks  map[string]string `json:"checks"`
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	git := "ok"
	if _, err := exec.LookPath("git"); err != nil {
		git = "missing"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: buildinfo.Version, Checks: map[string]string{"git": git}}); err != nil {
		s.deps.Log.Error("write health failed", "error", err)
	}
}
```

`apps/backend/internal/api/ui.go`:

```go
package api

import (
	"io/fs"
	"net/http"
	"strings"
)

const notBuiltMessage = "UI not built; run make build\n"

// uiHandler serves the embedded SPA with index.html fallback.
func uiHandler(fsys fs.FS, present bool) http.Handler {
	if !present {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(notBuiltMessage))
		})
	}
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean != "" {
			if _, err := fs.Stat(fsys, clean); err == nil {
				if strings.HasPrefix(clean, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, notBuiltMessage, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	})
}
```

`apps/backend/internal/api/router.go`:

```go
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/review"
)

// Deps are the router's collaborators.
type Deps struct {
	Service   *review.Service
	Providers *provider.Registry
	Log       *slog.Logger
	UI        fs.FS
	UIPresent bool
	// BuildContext bounds asynchronous builds; defaults to context.Background().
	BuildContext context.Context
}

type server struct {
	deps     Deps
	buildCtx context.Context
}

// NewRouter builds the fully wrapped HTTP handler.
func NewRouter(d Deps) http.Handler {
	buildCtx := d.BuildContext
	if buildCtx == nil {
		buildCtx = context.Background()
	}
	s := &server{deps: d, buildCtx: buildCtx}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/providers", s.listProviders)
	mux.HandleFunc("GET /api/providers/{provider}/repositories", s.listRepositories)
	mux.HandleFunc("GET /api/providers/{provider}/repositories/{repo}", s.getRepository)
	mux.HandleFunc("GET /api/providers/{provider}/repositories/{repo}/changes", s.listChanges)
	mux.HandleFunc("POST /api/reviews", s.createReview)
	mux.HandleFunc("GET /api/reviews", s.listReviews)
	mux.HandleFunc("GET /api/reviews/{id}", s.getReview)
	mux.HandleFunc("DELETE /api/reviews/{id}", s.deleteReview)
	mux.HandleFunc("GET /api/reviews/{id}/files", s.listReviewFiles)
	mux.HandleFunc("GET /api/reviews/{id}/files/{path...}", s.getReviewFile)
	mux.HandleFunc("GET /api/reviews/{id}/diff", s.getReviewDiff)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Not Found", "No such endpoint.")
	})
	if d.UI != nil {
		mux.Handle("GET /", uiHandler(d.UI, d.UIPresent))
	}
	return withMiddleware(mux, d.Log)
}
```

- [ ] **Step 5: Run the API tests and lint**

Run: `cd <worktree-root>/apps/backend && go test -race -count=1 ./internal/api/ && go tool golangci-lint run ./internal/api/...`
Expected: PASS. If `TestRepositoriesAndChanges` fails on the `%2F` route, confirm `r.PathValue("repo")` returns the decoded `atlas/server` on this Go version; if the escaped path is split into segments instead, change the route to `{repo...}` for `GET .../repositories/{repo...}` and keep `changes` on a separate `{repo}/changes` pattern, then re-run. Record whichever form works in a comment above the route.

- [ ] **Step 6: Commit**

```bash
cd <worktree-root> && git add apps/backend/internal/api && git commit -m "feat(task-001): JSON:API HTTP handlers, middleware and embedded UI serving"
```

---

### Task 20: Server binary

**Files:**
- Create: `apps/backend/cmd/converge/main.go`, `apps/backend/cmd/converge/main_test.go`

**Interfaces:**
- Produces: `converge` binary that wires `app.New`, starts the sweeper goroutine, serves `NewRouter` on `APP_PORT`, and shuts down gracefully on SIGINT/SIGTERM with a 15 second drain.

- [ ] **Step 1: Write the failing server test**

`apps/backend/cmd/converge/main_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestServerServesHealthAndShutsDown(t *testing.T) {
	root := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"APP_PORT=" + itoa(port),
		"LOG_LEVEL=error",
		"CLEANUP_INTERVAL_MINUTES=1",
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, env) }()

	base := "http://127.0.0.1:" + itoa(port)
	deadline := time.Now().Add(15 * time.Second)
	var resp *http.Response
	for {
		resp, err = http.Get(base + "/healthz")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Errorf("health = %d %v", resp.StatusCode, body)
	}
	// the UI stub answers 503 with the build hint
	uiResp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer uiResp.Body.Close()
	if uiResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("ui = %d, want 503 before the frontend is built", uiResp.StatusCode)
	}
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
```

- [ ] **Step 2: Implement the server**

`apps/backend/cmd/converge/main.go`:

```go
// Command converge serves the Converge HTTP API and the embedded UI.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jtumidanski/converge/internal/api"
	"github.com/jtumidanski/converge/internal/app"
	"github.com/jtumidanski/converge/internal/buildinfo"
	"github.com/jtumidanski/converge/internal/ui"
)

const shutdownTimeout = 15 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "converge: %v\n", err)
		os.Exit(1)
	}
}

// serve runs the HTTP server until ctx is cancelled.
func serve(ctx context.Context, env []string) error {
	application, err := app.New(ctx, env)
	if err != nil {
		return err
	}
	defer func() { _ = application.Close() }()

	sweepCtx, stopSweeper := context.WithCancel(context.Background())
	defer stopSweeper()
	go application.Store.RunSweeper(sweepCtx, application.Config.CleanupInterval)

	handler := api.NewRouter(api.Deps{
		Service: application.Service, Providers: application.Registry, Log: application.Log,
		UI: ui.FS(), UIPresent: ui.Present(), BuildContext: sweepCtx,
	})
	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(application.Config.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		application.Log.Info("listening", slog.String("addr", addr), slog.String("version", buildinfo.Version))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		application.Log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		stopSweeper()
		return <-errc
	}
}
```

- [ ] **Step 3: Run the whole backend gate**

Run:

```bash
cd <worktree-root>/apps/backend && go test -race -count=1 ./... && go test -race -count=1 -tags integration ./... && go vet ./... && go tool golangci-lint run && CGO_ENABLED=0 go build ./...
```

Expected: everything passes.

- [ ] **Step 4: Commit**

```bash
cd <worktree-root> && git add apps/backend/cmd/converge && git commit -m "feat(task-001): converge HTTP server binary with graceful shutdown"
```

---

## Phase F — Frontend

Every frontend task runs with Node 22 loaded:

```bash
export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22
```

### Task 21: Frontend scaffold, tooling, Tailwind, shadcn, Vitest

**Files:**
- Create: `apps/frontend/package.json`, `vite.config.ts`, `vitest.config.ts`, `tsconfig.json`, `tsconfig.app.json`, `tsconfig.node.json`, `eslint.config.js`, `.prettierrc.json`, `.prettierignore`, `components.json`, `index.html`, `src/main.tsx`, `src/App.tsx`, `src/index.css`, `src/vite-env.d.ts`, `src/lib/utils.ts`, `src/lib/strings.ts`, `src/test/setup.ts`, `src/lib/__tests__/utils.test.ts`
- Modify: `Makefile` (frontend targets), `.gitignore` (already covers `node_modules`, `dist`)

**Interfaces:**
- Produces:
  - npm scripts: `dev`, `build` (runs `tsc -b && vite build`), `preview`, `lint`, `format`, `format:check`, `test` (`vitest run`), `test:watch`.
  - Vite `build.outDir` = `../backend/internal/ui/dist`, `emptyOutDir: true`; dev server proxy `/api` → `http://localhost:8080`; alias `@` → `src`.
  - `cn(...inputs: ClassValue[]): string` from `clsx` + `tailwind-merge`.
  - `strings` object in `src/lib/strings.ts` holding the FR-10.11 vocabulary: `provider`, `repository`, `base`, `includedChanges`, `combinedReview`, `conflict`, `finishReview`, `discardReview`, `buildReview`, `diagnostics`.
- Pinned versions (latest verified 2026-09-04): react 19.2.8, react-dom 19.2.8, vite 8.2.2, @vitejs/plugin-react 6.1.1, typescript 5.9 (see note), tailwindcss 4.3.3, @tailwindcss/vite 4.3.3, @tanstack/react-query 5.102.8, react-router 8.3.1, react-hook-form 7.87.0, zod 4.5.4, @hookform/resolvers 5.9.1, sonner 2.0.8, lucide-react 1.41.0, clsx 2.1.1, tailwind-merge 3.6.0, class-variance-authority 0.7.1, @pierre/diffs 1.4.0, vitest 5.0.0, @testing-library/react 16.3.3, @testing-library/jest-dom 7.0.1, @testing-library/user-event 14.6.7, msw 2.15.0, jsdom 30.0.1, prettier 3.9.6, eslint 10.9.1, typescript-eslint 8.69.0, eslint-plugin-react-hooks 7.1.1, @types/node.

- [ ] **Step 1: Scaffold the app**

```bash
cd <worktree-root>/apps && export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22
npm create vite@latest frontend -- --template react-ts
cd frontend && npm install
```

Then install the rest:

```bash
cd <worktree-root>/apps/frontend
npm install @tanstack/react-query react-router react-hook-form zod @hookform/resolvers sonner lucide-react clsx tailwind-merge class-variance-authority @pierre/diffs tailwindcss @tailwindcss/vite
npm install -D vitest @testing-library/react @testing-library/jest-dom @testing-library/user-event msw jsdom prettier @types/node
```

Check the installed TypeScript major with `npx tsc --version`. TypeScript 7 is published; if `npm ls typescript` shows 7.x and `tsc -b` errors on the Vite template config, pin the last 5.x line instead: `npm install -D typescript@~5.9`. Record the chosen version in `context.md`.

- [ ] **Step 2: Configure Vite, Vitest, TypeScript, Tailwind**

`apps/frontend/vite.config.ts`:

```ts
import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true },
      "/healthz": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
  build: {
    // The Go binary embeds this directory (internal/ui/dist).
    outDir: "../backend/internal/ui/dist",
    emptyOutDir: true,
    sourcemap: false,
  },
});
```

`apps/frontend/vitest.config.ts`:

```ts
import path from "node:path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
```

`apps/frontend/tsconfig.app.json` — add to `compilerOptions`:

```json
{
  "baseUrl": ".",
  "paths": { "@/*": ["./src/*"] },
  "strict": true,
  "noUncheckedIndexedAccess": true,
  "exactOptionalPropertyTypes": true,
  "noImplicitOverride": true,
  "noUnusedLocals": true,
  "noUnusedParameters": true,
  "types": ["vite/client", "vitest/globals", "@testing-library/jest-dom"]
}
```

Mirror `baseUrl` and `paths` in `tsconfig.json`.

`apps/frontend/src/index.css`:

```css
@import "tailwindcss";

:root {
  color-scheme: light dark;
}

html,
body,
#root {
  height: 100%;
}
```

- [ ] **Step 3: Initialise shadcn/ui and add the primitives**

```bash
cd <worktree-root>/apps/frontend
npx shadcn@latest init
npx shadcn@latest add button input checkbox table badge card skeleton collapsible select dialog tooltip separator scroll-area
```

Answer the init prompts with: style `new-york`, base colour `neutral`, CSS file `src/index.css`, CSS variables `yes`, alias `@/components` and `@/lib/utils`. If `init` cannot infer the Tailwind version, confirm Tailwind v4. Verify `components.json` exists and `src/components/ui/button.tsx` was generated.

- [ ] **Step 4: Write the failing utility test**

`apps/frontend/src/lib/__tests__/utils.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { cn } from "@/lib/utils";
import { strings } from "@/lib/strings";

describe("cn", () => {
  it("merges conditional classes and resolves conflicts", () => {
    expect(cn("p-2", "p-4")).toBe("p-4");
    expect(cn("flex", false && "hidden", "items-center")).toBe("flex items-center");
  });
});

describe("strings", () => {
  it("uses only product vocabulary outside diagnostics", () => {
    const productCopy = Object.entries(strings)
      .filter(([key]) => key !== "diagnostics")
      .map(([, value]) => value)
      .join(" ")
      .toLowerCase();
    for (const gitTerm of ["worktree", "cherry-pick", "synthetic branch"]) {
      expect(productCopy).not.toContain(gitTerm);
    }
    expect(strings.finishReview).toBe("Finish Review");
    expect(strings.discardReview).toBe("Discard Review");
    expect(strings.combinedReview).toBe("Combined Review");
  });
});
```

- [ ] **Step 5: Implement the utilities and app shell**

`apps/frontend/src/lib/utils.ts` (generated by shadcn; confirm it matches):

```ts
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
```

`apps/frontend/src/lib/strings.ts`:

```ts
/**
 * Product vocabulary (FR-10.11). Git terms appear only under Diagnostics.
 */
export const strings = {
  provider: "Provider",
  repository: "Repository",
  base: "Base",
  includedChanges: "Included PRs/MRs",
  combinedReview: "Combined Review",
  conflict: "Conflict",
  finishReview: "Finish Review",
  discardReview: "Discard Review",
  buildReview: "Build Review",
  diagnostics: "Diagnostics",
} as const;
```

`apps/frontend/src/test/setup.ts`:

```ts
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => {
  cleanup();
});
```

`apps/frontend/src/App.tsx` (placeholder until Task 25 adds routes):

```tsx
export function App() {
  return <main className="p-8 text-foreground">Converge</main>;
}
```

`apps/frontend/src/main.tsx`:

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "@/App";
import "@/index.css";

const container = document.getElementById("root");
if (!container) {
  throw new Error("Root element #root is missing from index.html");
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
```

- [ ] **Step 6: Configure package scripts, ESLint and Prettier**

`apps/frontend/package.json` scripts:

```json
{
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "lint": "eslint .",
    "format": "prettier --write .",
    "format:check": "prettier --check .",
    "test": "vitest run",
    "test:watch": "vitest"
  }
}
```

`apps/frontend/.prettierrc.json`:

```json
{
  "semi": true,
  "singleQuote": false,
  "trailingComma": "all",
  "printWidth": 100
}
```

`apps/frontend/.prettierignore`:

```
dist
node_modules
src/components/ui
```

Keep the ESLint flat config the Vite template generated; append the React Hooks rules and ignore `dist` and generated shadcn components:

```js
// eslint.config.js — add to the exported array
{
  ignores: ["dist", "src/components/ui/**"],
}
```

- [ ] **Step 7: Wire the frontend into the Makefile**

Replace the `lint`, `test`, and `build` recipes:

```make
NPM := export NVM_DIR="$$HOME/.nvm" && . "$$NVM_DIR/nvm.sh" >/dev/null && nvm use 22 >/dev/null && npm

lint: ## Lint backend and frontend
	cd $(BACKEND) && go vet ./... && go tool golangci-lint run
	cd $(FRONTEND) && $(NPM) run lint && $(NPM) run format:check

test: ## Unit tests, both apps
	cd $(BACKEND) && go test -race -count=1 ./...
	cd $(FRONTEND) && $(NPM) test

build: ## Build the frontend into the backend embed dir, then the binaries
	cd $(FRONTEND) && $(NPM) ci && $(NPM) run build
	touch $(BACKEND)/internal/ui/dist/.gitkeep
	cd $(BACKEND) && CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" ./...
	$(ROOT)/tools/build-backend.sh
```

`tools/build-backend.sh` is written in Task 27; for now create a stub that exits 0 with a message, and replace it there. Create it now so `make build` works:

```bash
#!/usr/bin/env bash
# Placeholder replaced in Task 27 with the cross-compilation build.
set -euo pipefail
echo "build-backend.sh: cross-compilation is wired in Task 27"
```

- [ ] **Step 8: Verify the frontend gate**

Run:

```bash
cd <worktree-root>/apps/frontend && export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22 && npm run lint && npm run format:check && npm test && npm run build
cd <worktree-root>/apps/backend && go build ./... && go test -count=1 ./internal/ui/
```

Expected: all pass. `internal/ui` now embeds a real `index.html`, so `TestFSIsReadableAndPresentReflectsIndex` from Task 1 will fail. Update that test to assert both branches explicitly:

```go
func TestFSIsReadable(t *testing.T) {
	if _, err := fs.ReadDir(FS(), "."); err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// Present() is true only after `make build` has run; both outcomes are valid here.
	_ = Present()
}
```

- [ ] **Step 9: Commit**

```bash
cd <worktree-root> && git add apps/frontend Makefile tools/build-backend.sh apps/backend/internal/ui .gitignore && git commit -m "chore(task-001): frontend scaffold with Vite, Tailwind, shadcn and Vitest"
```

---

### Task 22: API types, client, services

**Files:**
- Create: `apps/frontend/src/types/api/jsonapi.ts`, `src/types/models/{provider,repository,change,review,reviewFile}.ts`, `src/lib/api/client.ts`, `src/lib/api/errors.ts`, `src/services/api/{providers,repositories,changes,reviews,index}.ts`, `src/lib/api/__tests__/client.test.ts`, `src/services/api/__tests__/reviews.test.ts`, `src/test/server.ts`

**Interfaces:**
- Produces:
  - `types/api/jsonapi.ts`: `interface Resource<TType extends string, TAttributes> { type: TType; id: string; attributes: TAttributes; relationships?: Record<string, { links: { related: string } }> }`; `interface Document<T> { data: T }`; `interface ListDocument<T> { data: T[]; meta?: { page?: PageMeta } }`; `interface PageMeta { number: number; size: number; hasNext: boolean }`; `interface ErrorDocument { errors: ApiErrorObject[] }`; `interface ApiErrorObject { status: string; code: string; title: string; detail?: string }`.
  - `types/models/provider.ts`: `interface ProviderAttributes { displayName: string; kind: "github" | "gitlab"; baseUrl: string }`; `type Provider = Resource<"providers", ProviderAttributes>`.
  - `types/models/repository.ts`: `RepositoryAttributes { name; namespace; defaultBranch; webUrl }`; `type Repository = Resource<"repositories", RepositoryAttributes>`.
  - `types/models/change.ts`: `ChangeAttributes { number: number; title: string; author: string; sourceBranch: string; targetBranch: string; mergedAt: string | null; createdAt: string | null; landingSha: string | null; webUrl: string }`; `type Change = Resource<"changes", ChangeAttributes>`.
  - `types/models/review.ts`: `type ReviewStatus = "CREATING" | "READY" | "CONFLICTED" | "FAILED" | "FINISHED" | "EXPIRED"`; `interface ReviewErrorPayload { code: string; message: string; change?: number; commit?: string; conflictingFiles?: string[]; appliedChanges?: number[]; possibleDependency?: boolean; diagnostics?: { workspacePath?: string; branch?: string; strategy?: string; sourceSha?: string } }`; `interface IncludedChange { number; title; author; mergedAt: string | null; webUrl: string; strategy: string }`; `interface Totals { files: number; additions: number; deletions: number }`; `ReviewAttributes {...}` mirroring the API; `type Review = Resource<"reviews", ReviewAttributes>`; `interface CreateReviewRequest { provider: string; repository: string; baseBranch?: string; changes: number[] }`.
  - `types/models/reviewFile.ts`: `type FileStatus = "added" | "modified" | "deleted" | "renamed"`; `ReviewFileAttributes { path; previousPath: string | null; status: FileStatus; additions; deletions; binary }`; `type ReviewFile = Resource<"review-files", ReviewFileAttributes>`; `ReviewFileDiffAttributes extends ReviewFileAttributes { truncated: boolean; diff: string }`; `type ReviewFileDiff = Resource<"review-file-diffs", ReviewFileDiffAttributes>`.
  - `lib/api/errors.ts`: `class ApiError extends Error { readonly status: number; readonly code: string; readonly detail: string }`; `function isApiError(e: unknown): e is ApiError`; `function messageFor(e: unknown, fallback: string): string`.
  - `lib/api/client.ts`: `async function apiGet<T>(path: string, init?: RequestInit): Promise<T>`; `apiPost<T>(path, body: unknown)`; `apiDelete(path)`; `apiGetText(path)`. All send `Accept: application/vnd.api+json`, throw `ApiError` on non-2xx, and parse the error document when present.
  - `services/api/providers.ts`: `providersService.list(): Promise<Provider[]>`.
  - `services/api/repositories.ts`: `repositoriesService.list(providerId, params: { page?: number; pageSize?: number }): Promise<{ items: Repository[]; page?: PageMeta }>`; `.get(providerId, fullName): Promise<Repository>`.
  - `services/api/changes.ts`: `changesService.list(providerId, repo, params: { target?: string; search?: string; page?: number; pageSize?: number }): Promise<{ items: Change[]; page?: PageMeta }>`.
  - `services/api/reviews.ts`: `reviewsService.create(req)`, `.get(id)`, `.list()`, `.files(id)`, `.fileDiff(id, path)`, `.remove(id)`.
  - `services/api/index.ts` re-exports the four services and the model types.
  - `src/test/server.ts`: MSW `setupServer` instance plus `http`/`HttpResponse` re-exports and JSON:API document builders `oneDoc`, `listDoc`, `errorDoc`.

- [ ] **Step 1: Write the failing client and service tests**

`apps/frontend/src/lib/api/__tests__/client.test.ts`:

```ts
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse, server } from "@/test/server";
import { apiDelete, apiGet, apiGetText, apiPost } from "@/lib/api/client";
import { ApiError, isApiError, messageFor } from "@/lib/api/errors";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("api client", () => {
  it("sends JSON:API headers and returns the parsed body", async () => {
    let seenAccept = "";
    server.use(
      http.get("/api/thing", ({ request }) => {
        seenAccept = request.headers.get("Accept") ?? "";
        return HttpResponse.json({ data: { type: "thing", id: "1", attributes: { ok: true } } });
      }),
    );
    const body = await apiGet<{ data: { id: string } }>("/api/thing");
    expect(body.data.id).toBe("1");
    expect(seenAccept).toContain("application/vnd.api+json");
  });

  it("posts a JSON:API document and returns the response", async () => {
    let received: unknown = null;
    server.use(
      http.post("/api/reviews", async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({ data: { type: "reviews", id: "abc" } }, { status: 202 });
      }),
    );
    const out = await apiPost<{ data: { id: string } }>("/api/reviews", {
      data: { type: "reviews", attributes: { provider: "gh" } },
    });
    expect(out.data.id).toBe("abc");
    expect(received).toEqual({ data: { type: "reviews", attributes: { provider: "gh" } } });
  });

  it("throws ApiError carrying status, code and detail", async () => {
    server.use(
      http.get("/api/bad", () =>
        HttpResponse.json(
          { errors: [{ status: "400", code: "INVALID_CHANGES", title: "Bad Request", detail: "Pick one." }] },
          { status: 400 },
        ),
      ),
    );
    const error = await apiGet("/api/bad").catch((e: unknown) => e);
    expect(isApiError(error)).toBe(true);
    const apiError = error as ApiError;
    expect(apiError.status).toBe(400);
    expect(apiError.code).toBe("INVALID_CHANGES");
    expect(apiError.detail).toBe("Pick one.");
    expect(messageFor(apiError, "fallback")).toBe("Pick one.");
    expect(messageFor(new Error("boom"), "fallback")).toBe("fallback");
  });

  it("handles error responses that are not JSON:API documents", async () => {
    server.use(http.get("/api/html", () => new HttpResponse("<html>502</html>", { status: 502 })));
    const error = (await apiGet("/api/html").catch((e: unknown) => e)) as ApiError;
    expect(error.status).toBe(502);
    expect(error.code).toBe("UNKNOWN");
  });

  it("returns nothing for 204 and text for text endpoints", async () => {
    server.use(http.delete("/api/reviews/abc", () => new HttpResponse(null, { status: 204 })));
    await expect(apiDelete("/api/reviews/abc")).resolves.toBeUndefined();
    server.use(http.get("/api/reviews/abc/diff", () => HttpResponse.text("diff --git a/x b/x")));
    await expect(apiGetText("/api/reviews/abc/diff")).resolves.toContain("diff --git");
  });
});
```

`apps/frontend/src/services/api/__tests__/reviews.test.ts`:

```ts
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { changesService, repositoriesService, reviewsService } from "@/services/api";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("services", () => {
  it("lists repositories with page meta", async () => {
    server.use(
      http.get("/api/providers/gh/repositories", ({ request }) => {
        const url = new URL(request.url);
        expect(url.searchParams.get("page")).toBe("2");
        expect(url.searchParams.get("pageSize")).toBe("30");
        return HttpResponse.json(
          listDoc([oneDoc("repositories", "atlas/server", { name: "server", namespace: "atlas", defaultBranch: "main", webUrl: "u" }).data], {
            number: 2,
            size: 30,
            hasNext: false,
          }),
        );
      }),
    );
    const out = await repositoriesService.list("gh", { page: 2, pageSize: 30 });
    expect(out.items).toHaveLength(1);
    expect(out.items[0]?.attributes.defaultBranch).toBe("main");
    expect(out.page?.hasNext).toBe(false);
  });

  it("encodes the repository path segment when listing changes", async () => {
    let seenPath = "";
    server.use(
      http.get("/api/providers/gh/repositories/:repo/changes", ({ params, request }) => {
        seenPath = String(params.repo);
        expect(new URL(request.url).searchParams.get("target")).toBe("main");
        return HttpResponse.json(listDoc([], { number: 1, size: 30, hasNext: false }));
      }),
    );
    await changesService.list("gh", "atlas/server", { target: "main" });
    expect(seenPath).toBe("atlas/server");
  });

  it("creates, reads and deletes a review", async () => {
    server.use(
      http.post("/api/reviews", async ({ request }) => {
        const body = (await request.json()) as { data: { type: string; attributes: Record<string, unknown> } };
        expect(body.data.type).toBe("reviews");
        expect(body.data.attributes.changes).toEqual([421, 427]);
        return HttpResponse.json(oneDoc("reviews", "7f14b2c8", { status: "CREATING" }), { status: 202 });
      }),
      http.get("/api/reviews/7f14b2c8", () => HttpResponse.json(oneDoc("reviews", "7f14b2c8", { status: "READY" }))),
      http.get("/api/reviews/7f14b2c8/files", () =>
        HttpResponse.json(listDoc([oneDoc("review-files", "a.txt", { path: "a.txt", status: "added", additions: 1, deletions: 0, binary: false, previousPath: null }).data])),
      ),
      http.get("/api/reviews/7f14b2c8/files/a.txt", () =>
        HttpResponse.json(oneDoc("review-file-diffs", "a.txt", { path: "a.txt", status: "added", additions: 1, deletions: 0, binary: false, previousPath: null, truncated: false, diff: "+a" })),
      ),
      http.delete("/api/reviews/7f14b2c8", () => new HttpResponse(null, { status: 204 })),
    );
    const created = await reviewsService.create({ provider: "gh", repository: "atlas/server", changes: [421, 427] });
    expect(created.id).toBe("7f14b2c8");
    expect((await reviewsService.get("7f14b2c8")).attributes.status).toBe("READY");
    expect(await reviewsService.files("7f14b2c8")).toHaveLength(1);
    expect((await reviewsService.fileDiff("7f14b2c8", "a.txt")).attributes.diff).toBe("+a");
    await expect(reviewsService.remove("7f14b2c8")).resolves.toBeUndefined();
  });
});
```

- [ ] **Step 2: Implement the MSW test server helper**

`apps/frontend/src/test/server.ts`:

```ts
import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";
import type { ListDocument, PageMeta, Document, Resource } from "@/types/api/jsonapi";

export const server = setupServer();
export { http, HttpResponse };

/** Builds a single-resource JSON:API document for tests. */
export function oneDoc<TType extends string, TAttrs>(
  type: TType,
  id: string,
  attributes: TAttrs,
): Document<Resource<TType, TAttrs>> {
  return { data: { type, id, attributes } };
}

/** Builds a collection JSON:API document for tests. */
export function listDoc<T>(data: T[], page?: PageMeta): ListDocument<T> {
  return page ? { data, meta: { page } } : { data };
}
```

Add to `src/test/setup.ts` nothing further; each test file starts and stops the server itself.

- [ ] **Step 3: Implement types, errors, client, services**

`apps/frontend/src/types/api/jsonapi.ts`:

```ts
export interface Resource<TType extends string, TAttributes> {
  type: TType;
  id: string;
  attributes: TAttributes;
  relationships?: Record<string, { links: { related: string } }>;
}

export interface Document<T> {
  data: T;
}

export interface PageMeta {
  number: number;
  size: number;
  hasNext: boolean;
}

export interface ListDocument<T> {
  data: T[];
  meta?: { page?: PageMeta };
}

export interface ApiErrorObject {
  status: string;
  code: string;
  title: string;
  detail?: string;
}

export interface ErrorDocument {
  errors: ApiErrorObject[];
}

export function isErrorDocument(value: unknown): value is ErrorDocument {
  return (
    typeof value === "object" &&
    value !== null &&
    Array.isArray((value as ErrorDocument).errors) &&
    (value as ErrorDocument).errors.length > 0
  );
}
```

`apps/frontend/src/lib/api/errors.ts`:

```ts
/** ApiError carries the JSON:API error status, code and detail. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly detail: string;

  constructor(status: number, code: string, title: string, detail: string) {
    super(detail || title);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.detail = detail || title;
  }
}

export function isApiError(value: unknown): value is ApiError {
  return value instanceof ApiError;
}

/** messageFor returns a user-facing message, falling back for unknown errors. */
export function messageFor(value: unknown, fallback: string): string {
  return isApiError(value) ? value.detail : fallback;
}
```

`apps/frontend/src/lib/api/client.ts`:

```ts
import { ApiError } from "@/lib/api/errors";
import { isErrorDocument } from "@/types/api/jsonapi";

const JSON_API = "application/vnd.api+json";

async function toApiError(response: Response): Promise<ApiError> {
  let body: unknown = null;
  try {
    body = await response.json();
  } catch {
    body = null;
  }
  if (isErrorDocument(body)) {
    const first = body.errors[0];
    if (first) {
      return new ApiError(response.status, first.code, first.title, first.detail ?? "");
    }
  }
  return new ApiError(response.status, "UNKNOWN", response.statusText || "Request failed", "");
}

async function request(path: string, init: RequestInit): Promise<Response> {
  const response = await fetch(path, {
    ...init,
    headers: { Accept: JSON_API, ...(init.headers ?? {}) },
  });
  if (!response.ok) {
    throw await toApiError(response);
  }
  return response;
}

export async function apiGet<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await request(path, { ...init, method: "GET" });
  return (await response.json()) as T;
}

export async function apiGetText(path: string): Promise<string> {
  const response = await request(path, { method: "GET", headers: { Accept: "text/plain" } });
  return await response.text();
}

export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const response = await request(path, {
    method: "POST",
    headers: { "Content-Type": JSON_API },
    body: JSON.stringify(body),
  });
  return (await response.json()) as T;
}

export async function apiDelete(path: string): Promise<void> {
  await request(path, { method: "DELETE" });
}
```

`apps/frontend/src/types/models/provider.ts`:

```ts
import type { Resource } from "@/types/api/jsonapi";

export type ProviderKind = "github" | "gitlab";

export interface ProviderAttributes {
  displayName: string;
  kind: ProviderKind;
  baseUrl: string;
}

export type Provider = Resource<"providers", ProviderAttributes>;
```

`apps/frontend/src/types/models/repository.ts`:

```ts
import type { Resource } from "@/types/api/jsonapi";

export interface RepositoryAttributes {
  name: string;
  namespace: string;
  defaultBranch: string;
  webUrl: string;
}

export type Repository = Resource<"repositories", RepositoryAttributes>;
```

`apps/frontend/src/types/models/change.ts`:

```ts
import type { Resource } from "@/types/api/jsonapi";

export interface ChangeAttributes {
  number: number;
  title: string;
  author: string;
  sourceBranch: string;
  targetBranch: string;
  mergedAt: string | null;
  createdAt: string | null;
  landingSha: string | null;
  webUrl: string;
}

export type Change = Resource<"changes", ChangeAttributes>;

/** shortSha renders a landing SHA for the table, or an em space when unknown. */
export function shortSha(sha: string | null): string {
  return sha ? sha.slice(0, 7) : "—";
}
```

`apps/frontend/src/types/models/review.ts`:

```ts
import type { Resource } from "@/types/api/jsonapi";

export type ReviewStatus = "CREATING" | "READY" | "CONFLICTED" | "FAILED" | "FINISHED" | "EXPIRED";

export interface ReviewDiagnostics {
  workspacePath?: string;
  branch?: string;
  strategy?: string;
  sourceSha?: string;
}

export interface ReviewErrorPayload {
  code: string;
  message: string;
  change?: number;
  commit?: string;
  conflictingFiles?: string[];
  appliedChanges?: number[];
  possibleDependency?: boolean;
  diagnostics?: ReviewDiagnostics;
}

export interface IncludedChange {
  number: number;
  title: string;
  author: string;
  mergedAt: string | null;
  webUrl: string;
  strategy: string;
}

export interface Totals {
  files: number;
  additions: number;
  deletions: number;
}

export interface ReviewAttributes {
  status: ReviewStatus;
  stage: string | null;
  provider: string;
  repository: string;
  baseBranch: string;
  baseSha: string | null;
  headSha: string | null;
  baseDescription: string;
  changes: number[];
  included: IncludedChange[];
  totals: Totals | null;
  error: ReviewErrorPayload | null;
  createdAt: string;
  updatedAt: string;
  expiresAt: string;
}

export type Review = Resource<"reviews", ReviewAttributes>;

export interface CreateReviewRequest {
  provider: string;
  repository: string;
  baseBranch?: string;
  changes: number[];
}

export function isTerminal(status: ReviewStatus): boolean {
  return status !== "CREATING";
}
```

`apps/frontend/src/types/models/reviewFile.ts`:

```ts
import type { Resource } from "@/types/api/jsonapi";

export type FileStatus = "added" | "modified" | "deleted" | "renamed";

export interface ReviewFileAttributes {
  path: string;
  previousPath: string | null;
  status: FileStatus;
  additions: number;
  deletions: number;
  binary: boolean;
}

export type ReviewFile = Resource<"review-files", ReviewFileAttributes>;

export interface ReviewFileDiffAttributes extends ReviewFileAttributes {
  truncated: boolean;
  diff: string;
}

export type ReviewFileDiff = Resource<"review-file-diffs", ReviewFileDiffAttributes>;
```

`apps/frontend/src/services/api/providers.ts`:

```ts
import { apiGet } from "@/lib/api/client";
import type { ListDocument } from "@/types/api/jsonapi";
import type { Provider } from "@/types/models/provider";

export const providersService = {
  async list(): Promise<Provider[]> {
    const doc = await apiGet<ListDocument<Provider>>("/api/providers");
    return doc.data;
  },
};
```

`apps/frontend/src/services/api/repositories.ts`:

```ts
import { apiGet } from "@/lib/api/client";
import type { Document, ListDocument, PageMeta } from "@/types/api/jsonapi";
import type { Repository } from "@/types/models/repository";

export interface PagedRepositories {
  items: Repository[];
  page?: PageMeta;
}

export interface RepositoryListParams {
  page?: number;
  pageSize?: number;
}

function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  const encoded = search.toString();
  return encoded ? `?${encoded}` : "";
}

export const repositoriesService = {
  async list(providerId: string, params: RepositoryListParams = {}): Promise<PagedRepositories> {
    const doc = await apiGet<ListDocument<Repository>>(
      `/api/providers/${encodeURIComponent(providerId)}/repositories${query({ page: params.page, pageSize: params.pageSize })}`,
    );
    return doc.meta?.page ? { items: doc.data, page: doc.meta.page } : { items: doc.data };
  },

  async get(providerId: string, fullName: string): Promise<Repository> {
    const doc = await apiGet<Document<Repository>>(
      `/api/providers/${encodeURIComponent(providerId)}/repositories/${encodeURIComponent(fullName)}`,
    );
    return doc.data;
  },
};

export { query as buildQuery };
```

`apps/frontend/src/services/api/changes.ts`:

```ts
import { apiGet } from "@/lib/api/client";
import { buildQuery } from "@/services/api/repositories";
import type { ListDocument, PageMeta } from "@/types/api/jsonapi";
import type { Change } from "@/types/models/change";

export interface ChangeListParams {
  target?: string;
  search?: string;
  page?: number;
  pageSize?: number;
}

export interface PagedChanges {
  items: Change[];
  page?: PageMeta;
}

export const changesService = {
  async list(providerId: string, repository: string, params: ChangeListParams = {}): Promise<PagedChanges> {
    const path = `/api/providers/${encodeURIComponent(providerId)}/repositories/${encodeURIComponent(repository)}/changes`;
    const doc = await apiGet<ListDocument<Change>>(
      `${path}${buildQuery({ state: "merged", target: params.target, search: params.search, page: params.page, pageSize: params.pageSize })}`,
    );
    return doc.meta?.page ? { items: doc.data, page: doc.meta.page } : { items: doc.data };
  },
};
```

`apps/frontend/src/services/api/reviews.ts`:

```ts
import { apiDelete, apiGet, apiGetText, apiPost } from "@/lib/api/client";
import type { Document, ListDocument } from "@/types/api/jsonapi";
import type { CreateReviewRequest, Review } from "@/types/models/review";
import type { ReviewFile, ReviewFileDiff } from "@/types/models/reviewFile";

export const reviewsService = {
  async create(request: CreateReviewRequest): Promise<Review> {
    const doc = await apiPost<Document<Review>>("/api/reviews", {
      data: { type: "reviews", attributes: request },
    });
    return doc.data;
  },

  async get(id: string): Promise<Review> {
    const doc = await apiGet<Document<Review>>(`/api/reviews/${encodeURIComponent(id)}`);
    return doc.data;
  },

  async list(): Promise<Review[]> {
    const doc = await apiGet<ListDocument<Review>>("/api/reviews");
    return doc.data;
  },

  async files(id: string): Promise<ReviewFile[]> {
    const doc = await apiGet<ListDocument<ReviewFile>>(`/api/reviews/${encodeURIComponent(id)}/files`);
    return doc.data;
  },

  async fileDiff(id: string, path: string): Promise<ReviewFileDiff> {
    const doc = await apiGet<Document<ReviewFileDiff>>(
      `/api/reviews/${encodeURIComponent(id)}/files/${path.split("/").map(encodeURIComponent).join("/")}`,
    );
    return doc.data;
  },

  async combinedDiff(id: string): Promise<string> {
    return apiGetText(`/api/reviews/${encodeURIComponent(id)}/diff`);
  },

  async remove(id: string): Promise<void> {
    await apiDelete(`/api/reviews/${encodeURIComponent(id)}`);
  },
};
```

`apps/frontend/src/services/api/index.ts`:

```ts
export { providersService } from "@/services/api/providers";
export { repositoriesService, type PagedRepositories } from "@/services/api/repositories";
export { changesService, type ChangeListParams, type PagedChanges } from "@/services/api/changes";
export { reviewsService } from "@/services/api/reviews";
export type { Provider } from "@/types/models/provider";
export type { Repository } from "@/types/models/repository";
export type { Change } from "@/types/models/change";
export type { CreateReviewRequest, Review, ReviewStatus } from "@/types/models/review";
export type { ReviewFile, ReviewFileDiff } from "@/types/models/reviewFile";
```

- [ ] **Step 4: Run the tests**

Run: `cd <worktree-root>/apps/frontend && export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22 && npm test && npm run lint && npx tsc --noEmit -p tsconfig.app.json`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add apps/frontend/src && git commit -m "feat(task-001): frontend API types, fetch client and service layer"
```

---

### Task 23: React Query hooks and selection state

**Files:**
- Create: `apps/frontend/src/lib/query-client.ts`, `src/lib/hooks/api/{useProviders,useRepositories,useChanges,useReviews}.ts`, `src/lib/hooks/useSelection.ts`, `src/lib/hooks/__tests__/useSelection.test.ts`, `src/lib/hooks/api/__tests__/useReviews.test.tsx`, `src/test/render.tsx`

**Interfaces:**
- Produces:
  - `createQueryClient(): QueryClient` with `retry: 1`, `refetchOnWindowFocus: false`, `staleTime: 60_000`.
  - `providerKeys`, `repositoryKeys`, `changeKeys`, `reviewKeys` hierarchical `as const` factories.
  - `useProviders()`, `useRepositories(providerId, params)`, `useRepository(providerId, fullName, enabled)`, `useChanges(providerId, repository, params)`.
  - `useReview(id)` with `refetchInterval: (query) => query.state.data?.attributes.status === "CREATING" ? 2000 : false`.
  - `useReviewFiles(id, enabled)`, `useReviewFile(id, path)`, `useReviews()`, `useCreateReview()`, `useFinishReview()`.
  - `useInvalidateReviews()` exposing `invalidateAll`, `invalidateReview(id)`.
  - `useSelection(storageKey)` returning `{ selected: Map<number, Change>, isSelected(n), toggle(change), clear(), numbers: number[], count: number }`, persisted to `sessionStorage`.
  - `src/test/render.tsx`: `renderWithProviders(ui, { route })` wrapping in `QueryClientProvider` + `MemoryRouter`.

- [ ] **Step 1: Write the failing hook tests**

`apps/frontend/src/lib/hooks/__tests__/useSelection.test.ts`:

```ts
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useSelection } from "@/lib/hooks/useSelection";
import type { Change } from "@/types/models/change";

function change(n: number): Change {
  return {
    type: "changes",
    id: String(n),
    attributes: {
      number: n,
      title: `change ${n}`,
      author: "dev",
      sourceBranch: "feat",
      targetBranch: "main",
      mergedAt: "2026-08-21T14:02:11Z",
      createdAt: "2026-08-20T09:00:00Z",
      landingSha: null,
      webUrl: "https://example.test",
    },
  };
}

describe("useSelection", () => {
  beforeEach(() => sessionStorage.clear());

  it("toggles, reports membership and produces sorted numbers", () => {
    const { result } = renderHook(() => useSelection("converge:selection:gh/atlas/server"));
    expect(result.current.count).toBe(0);
    act(() => result.current.toggle(change(427)));
    act(() => result.current.toggle(change(421)));
    expect(result.current.numbers).toEqual([421, 427]);
    expect(result.current.isSelected(421)).toBe(true);
    act(() => result.current.toggle(change(421)));
    expect(result.current.isSelected(421)).toBe(false);
    expect(result.current.count).toBe(1);
    act(() => result.current.clear());
    expect(result.current.count).toBe(0);
  });

  it("persists to sessionStorage and restores on remount", () => {
    const key = "converge:selection:gh/atlas/server";
    const first = renderHook(() => useSelection(key));
    act(() => first.result.current.toggle(change(435)));
    first.unmount();
    const second = renderHook(() => useSelection(key));
    expect(second.result.current.numbers).toEqual([435]);
    expect(second.result.current.selected.get(435)?.attributes.title).toBe("change 435");
  });

  it("keeps selections separate per storage key and survives corrupt storage", () => {
    sessionStorage.setItem("converge:selection:gh/other", "not json");
    const { result } = renderHook(() => useSelection("converge:selection:gh/other"));
    expect(result.current.count).toBe(0);
    act(() => result.current.toggle(change(1)));
    const other = renderHook(() => useSelection("converge:selection:gh/atlas/server"));
    expect(other.result.current.count).toBe(0);
  });
});
```

`apps/frontend/src/lib/hooks/api/__tests__/useReviews.test.tsx`:

```tsx
import { renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, oneDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import { useCreateReview, useReview, useReviewFiles } from "@/lib/hooks/api/useReviews";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  vi.useRealTimers();
});
afterAll(() => server.close());

function reviewAttrs(status: string) {
  return {
    status,
    stage: status === "CREATING" ? "resolving" : null,
    provider: "gh",
    repository: "atlas/server",
    baseBranch: "main",
    baseSha: status === "READY" ? "a".repeat(40) : null,
    headSha: status === "READY" ? "b".repeat(40) : null,
    baseDescription: "Immediately before #421",
    changes: [421],
    included: [],
    totals: status === "READY" ? { files: 1, additions: 2, deletions: 0 } : null,
    error: null,
    createdAt: "2026-09-01T12:00:00Z",
    updatedAt: "2026-09-01T12:00:05Z",
    expiresAt: "2026-09-02T12:00:00Z",
  };
}

describe("useReview", () => {
  it("polls while CREATING and stops once terminal", async () => {
    let calls = 0;
    server.use(
      http.get("/api/reviews/abc12345", () => {
        calls += 1;
        return HttpResponse.json(oneDoc("reviews", "abc12345", reviewAttrs(calls < 2 ? "CREATING" : "READY")));
      }),
    );
    const { result } = renderHook(() => useReview("abc12345"), { wrapper: queryWrapper() });
    await waitFor(() => expect(result.current.data?.attributes.status).toBe("CREATING"));
    await waitFor(() => expect(result.current.data?.attributes.status).toBe("READY"), { timeout: 6000 });
    const callsAtReady = calls;
    await new Promise((resolve) => setTimeout(resolve, 2500));
    expect(calls).toBe(callsAtReady);
  }, 15000);

  it("does not fetch files until the review is READY", async () => {
    let fileCalls = 0;
    server.use(
      http.get("/api/reviews/abc12345/files", () => {
        fileCalls += 1;
        return HttpResponse.json({ data: [] });
      }),
    );
    const disabled = renderHook(() => useReviewFiles("abc12345", false), { wrapper: queryWrapper() });
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(fileCalls).toBe(0);
    expect(disabled.result.current.data).toBeUndefined();
    renderHook(() => useReviewFiles("abc12345", true), { wrapper: queryWrapper() });
    await waitFor(() => expect(fileCalls).toBe(1));
  });
});

describe("useCreateReview", () => {
  it("posts the request and returns the created review", async () => {
    server.use(
      http.post("/api/reviews", () => HttpResponse.json(oneDoc("reviews", "abc12345", reviewAttrs("CREATING")), { status: 202 })),
    );
    const { result } = renderHook(() => useCreateReview(), { wrapper: queryWrapper() });
    const created = await result.current.mutateAsync({ provider: "gh", repository: "atlas/server", changes: [421] });
    expect(created.id).toBe("abc12345");
  });
});
```

- [ ] **Step 2: Implement the query client, wrapper and hooks**

`apps/frontend/src/lib/query-client.ts`:

```ts
import { QueryClient } from "@tanstack/react-query";

/** createQueryClient builds the app's QueryClient with Converge defaults. */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: 1,
        refetchOnWindowFocus: false,
        staleTime: 60_000,
        gcTime: 5 * 60_000,
      },
      mutations: { retry: 0 },
    },
  });
}
```

`apps/frontend/src/test/render.tsx`:

```tsx
import type { ReactElement, ReactNode } from "react";
import { render, type RenderResult } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

function testClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  });
}

/** queryWrapper wraps hooks under test in a QueryClientProvider. */
export function queryWrapper() {
  const client = testClient();
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

/** renderWithProviders renders a component with router and query providers. */
export function renderWithProviders(ui: ReactElement, options: { route?: string } = {}): RenderResult {
  const client = testClient();
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[options.route ?? "/"]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}
```

`apps/frontend/src/lib/hooks/useSelection.ts`:

```ts
import { useCallback, useEffect, useMemo, useState } from "react";
import type { Change } from "@/types/models/change";

function readStorage(key: string): Map<number, Change> {
  try {
    const raw = sessionStorage.getItem(key);
    if (!raw) return new Map();
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return new Map();
    const entries = parsed.filter(
      (item): item is Change =>
        typeof item === "object" && item !== null && typeof (item as Change).id === "string",
    );
    return new Map(entries.map((item) => [item.attributes.number, item]));
  } catch {
    return new Map();
  }
}

export interface SelectionState {
  selected: Map<number, Change>;
  isSelected: (n: number) => boolean;
  toggle: (change: Change) => void;
  clear: () => void;
  numbers: number[];
  count: number;
}

/** useSelection keeps selected PRs/MRs across pagination and search, backed by sessionStorage. */
export function useSelection(storageKey: string): SelectionState {
  const [selected, setSelected] = useState<Map<number, Change>>(() => readStorage(storageKey));

  useEffect(() => {
    setSelected(readStorage(storageKey));
  }, [storageKey]);

  useEffect(() => {
    try {
      sessionStorage.setItem(storageKey, JSON.stringify([...selected.values()]));
    } catch {
      // Storage is best effort; selection still works in memory.
    }
  }, [storageKey, selected]);

  const toggle = useCallback((change: Change) => {
    setSelected((current) => {
      const next = new Map(current);
      if (next.has(change.attributes.number)) {
        next.delete(change.attributes.number);
      } else {
        next.set(change.attributes.number, change);
      }
      return next;
    });
  }, []);

  const clear = useCallback(() => setSelected(new Map()), []);

  const numbers = useMemo(() => [...selected.keys()].sort((a, b) => a - b), [selected]);

  return {
    selected,
    isSelected: useCallback((n: number) => selected.has(n), [selected]),
    toggle,
    clear,
    numbers,
    count: selected.size,
  };
}
```

`apps/frontend/src/lib/hooks/api/useProviders.ts`:

```ts
import { useQuery } from "@tanstack/react-query";
import { providersService } from "@/services/api";

export const providerKeys = {
  all: ["providers"] as const,
  lists: () => [...providerKeys.all, "list"] as const,
};

export function useProviders() {
  return useQuery({
    queryKey: providerKeys.lists(),
    queryFn: () => providersService.list(),
    staleTime: 5 * 60_000,
  });
}
```

`apps/frontend/src/lib/hooks/api/useRepositories.ts`:

```ts
import { useQuery } from "@tanstack/react-query";
import { repositoriesService } from "@/services/api";

export interface RepositoryQueryParams {
  page?: number;
  pageSize?: number;
}

export const repositoryKeys = {
  all: ["repositories"] as const,
  lists: () => [...repositoryKeys.all, "list"] as const,
  list: (providerId: string, params?: RepositoryQueryParams) =>
    [...repositoryKeys.lists(), providerId, params ?? {}] as const,
  details: () => [...repositoryKeys.all, "detail"] as const,
  detail: (providerId: string, fullName: string) =>
    [...repositoryKeys.details(), providerId, fullName] as const,
};

export function useRepositories(providerId: string | undefined, params: RepositoryQueryParams = {}) {
  return useQuery({
    queryKey: repositoryKeys.list(providerId ?? "", params),
    queryFn: () => repositoriesService.list(providerId as string, params),
    enabled: Boolean(providerId),
    staleTime: 2 * 60_000,
  });
}

export function useRepository(providerId: string | undefined, fullName: string, enabled: boolean) {
  return useQuery({
    queryKey: repositoryKeys.detail(providerId ?? "", fullName),
    queryFn: () => repositoriesService.get(providerId as string, fullName),
    enabled: enabled && Boolean(providerId) && fullName.length > 0,
    retry: false,
  });
}
```

`apps/frontend/src/lib/hooks/api/useChanges.ts`:

```ts
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { changesService, type ChangeListParams } from "@/services/api";

export const changeKeys = {
  all: ["changes"] as const,
  lists: () => [...changeKeys.all, "list"] as const,
  list: (providerId: string, repository: string, params: ChangeListParams) =>
    [...changeKeys.lists(), providerId, repository, params] as const,
};

export function useChanges(
  providerId: string | undefined,
  repository: string | undefined,
  params: ChangeListParams,
) {
  return useQuery({
    queryKey: changeKeys.list(providerId ?? "", repository ?? "", params),
    queryFn: () => changesService.list(providerId as string, repository as string, params),
    enabled: Boolean(providerId) && Boolean(repository),
    placeholderData: keepPreviousData,
    staleTime: 60_000,
  });
}
```

`apps/frontend/src/lib/hooks/api/useReviews.ts`:

```ts
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { reviewsService } from "@/services/api";
import type { CreateReviewRequest, Review } from "@/types/models/review";

export const reviewKeys = {
  all: ["reviews"] as const,
  lists: () => [...reviewKeys.all, "list"] as const,
  details: () => [...reviewKeys.all, "detail"] as const,
  detail: (id: string) => [...reviewKeys.details(), id] as const,
  files: (id: string) => [...reviewKeys.detail(id), "files"] as const,
  file: (id: string, path: string) => [...reviewKeys.files(id), path] as const,
};

/** useReview polls every 2 s while the review is still building. */
export function useReview(id: string | undefined) {
  return useQuery({
    queryKey: reviewKeys.detail(id ?? ""),
    queryFn: () => reviewsService.get(id as string),
    enabled: Boolean(id),
    staleTime: 0,
    refetchInterval: (query) =>
      (query.state.data as Review | undefined)?.attributes.status === "CREATING" ? 2000 : false,
  });
}

export function useReviews() {
  return useQuery({ queryKey: reviewKeys.lists(), queryFn: () => reviewsService.list() });
}

export function useReviewFiles(id: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: reviewKeys.files(id ?? ""),
    queryFn: () => reviewsService.files(id as string),
    enabled: enabled && Boolean(id),
    staleTime: 5 * 60_000,
  });
}

export function useReviewFile(id: string | undefined, path: string | undefined) {
  return useQuery({
    queryKey: reviewKeys.file(id ?? "", path ?? ""),
    queryFn: () => reviewsService.fileDiff(id as string, path as string),
    enabled: Boolean(id) && Boolean(path),
    staleTime: 5 * 60_000,
  });
}

export function useCreateReview() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (request: CreateReviewRequest) => reviewsService.create(request),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: reviewKeys.lists() });
    },
  });
}

export function useFinishReview() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => reviewsService.remove(id),
    onSettled: (_data, _error, id) => {
      void queryClient.invalidateQueries({ queryKey: reviewKeys.detail(id) });
      void queryClient.invalidateQueries({ queryKey: reviewKeys.lists() });
    },
  });
}

export function useInvalidateReviews() {
  const queryClient = useQueryClient();
  return {
    invalidateAll: () => queryClient.invalidateQueries({ queryKey: reviewKeys.all }),
    invalidateReview: (id: string) => queryClient.invalidateQueries({ queryKey: reviewKeys.detail(id) }),
  };
}
```

- [ ] **Step 3: Run the tests**

Run: `cd <worktree-root>/apps/frontend && export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22 && npm test && npm run lint`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd <worktree-root> && git add apps/frontend/src && git commit -m "feat(task-001): React Query hooks and selection state"
```

---

### Task 24: Shared components, provider/repository selection

**Files:**
- Create: `apps/frontend/src/components/common/{PageHeader,EmptyState,ErrorBanner,Pagination}.tsx`, `src/components/features/providers/ProviderPicker.tsx`, `src/components/features/repositories/{RepositoryList,ManualRepositoryForm}.tsx`, `src/lib/schemas/repository.ts`, `src/pages/SelectRepositoryPage.tsx`, tests: `src/components/features/repositories/__tests__/ManualRepositoryForm.test.tsx`, `src/pages/__tests__/SelectRepositoryPage.test.tsx`

**Interfaces:**
- Produces:
  - `PageHeader({ title, description, actions })`, `EmptyState({ title, description, icon })`, `ErrorBanner({ title, detail, onRetry })`, `Pagination({ page, hasNext, onChange, disabled })`.
  - `ProviderPicker({ providers, value, onChange, loading })` — a shadcn `Select` labelled "Provider".
  - `RepositoryList({ repositories, loading, onSelect })` — table of name, namespace, default branch with a Select button per row.
  - `repositorySchema` in `src/lib/schemas/repository.ts`: `z.object({ repository: z.string().regex(/^[A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)+$/, "Enter a repository as owner/name").refine((v) => !v.includes(".."), "Path segments cannot contain ..") })` with `export type RepositoryFormData = z.infer<typeof repositorySchema>`.
  - `ManualRepositoryForm({ providerId, onResolved })` — react-hook-form + zodResolver; validates server-side via `useRepository` before calling `onResolved(repository)`; shows the API detail message inline on failure.
  - `SelectRepositoryPage` — route `/`; picks a provider, lists repositories, offers manual entry, and navigates to `/select?provider=<id>&repo=<fullName>`.

- [ ] **Step 1: Write the failing component tests**

`apps/frontend/src/components/features/repositories/__tests__/ManualRepositoryForm.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { ManualRepositoryForm } from "@/components/features/repositories/ManualRepositoryForm";

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("ManualRepositoryForm", () => {
  it("rejects malformed repository names without calling the API", async () => {
    const onResolved = vi.fn();
    let calls = 0;
    server.use(
      http.get("/api/providers/gh/repositories/:repo", () => {
        calls += 1;
        return HttpResponse.json({ errors: [] }, { status: 404 });
      }),
    );
    renderWithProviders(<ManualRepositoryForm providerId="gh" onResolved={onResolved} />);
    await userEvent.type(screen.getByLabelText(/repository/i), "not-a-repo");
    await userEvent.click(screen.getByRole("button", { name: /use repository/i }));
    expect(await screen.findByText(/owner\/name/i)).toBeInTheDocument();
    expect(calls).toBe(0);
    expect(onResolved).not.toHaveBeenCalled();
  });

  it("validates against the API and reports the resolved repository", async () => {
    const onResolved = vi.fn();
    server.use(
      http.get("/api/providers/gh/repositories/:repo", ({ params }) => {
        expect(params.repo).toBe("atlas/server");
        return HttpResponse.json(
          oneDoc("repositories", "atlas/server", { name: "server", namespace: "atlas", defaultBranch: "main", webUrl: "u" }),
        );
      }),
    );
    renderWithProviders(<ManualRepositoryForm providerId="gh" onResolved={onResolved} />);
    await userEvent.type(screen.getByLabelText(/repository/i), "atlas/server");
    await userEvent.click(screen.getByRole("button", { name: /use repository/i }));
    await waitFor(() => expect(onResolved).toHaveBeenCalledTimes(1));
    expect(onResolved.mock.calls[0]?.[0]?.id).toBe("atlas/server");
  });

  it("shows the API message when the repository is not found", async () => {
    server.use(
      http.get("/api/providers/gh/repositories/:repo", () =>
        HttpResponse.json(
          { errors: [{ status: "404", code: "NOT_FOUND", title: "Not Found", detail: "The requested resource does not exist." }] },
          { status: 404 },
        ),
      ),
    );
    renderWithProviders(<ManualRepositoryForm providerId="gh" onResolved={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/repository/i), "atlas/missing");
    await userEvent.click(screen.getByRole("button", { name: /use repository/i }));
    expect(await screen.findByText(/does not exist/i)).toBeInTheDocument();
  });
});
```

`apps/frontend/src/pages/__tests__/SelectRepositoryPage.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { SelectRepositoryPage } from "@/pages/SelectRepositoryPage";

const navigate = vi.fn();
vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigate };
});

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => {
  server.resetHandlers();
  navigate.mockReset();
});
afterAll(() => server.close());

function seedProviders() {
  server.use(
    http.get("/api/providers", () =>
      HttpResponse.json(
        listDoc([
          oneDoc("providers", "gitlab-work", { displayName: "GitLab Work", kind: "gitlab", baseUrl: "https://gitlab.test" }).data,
        ]),
      ),
    ),
  );
}

describe("SelectRepositoryPage", () => {
  it("shows a skeleton, then repositories for the only provider", async () => {
    seedProviders();
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () =>
        HttpResponse.json(
          listDoc(
            [oneDoc("repositories", "atlas/server", { name: "server", namespace: "atlas", defaultBranch: "main", webUrl: "u" }).data],
            { number: 1, size: 30, hasNext: false },
          ),
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText("atlas/server")).toBeInTheDocument();
    expect(screen.getByText("main")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /select/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalled());
    expect(String(navigate.mock.calls[0]?.[0])).toContain("provider=gitlab-work");
    expect(String(navigate.mock.calls[0]?.[0])).toContain("repo=atlas%2Fserver");
  });

  it("surfaces provider failures in an error banner", async () => {
    server.use(
      http.get("/api/providers", () =>
        HttpResponse.json(
          { errors: [{ status: "502", code: "PROVIDER_AUTH", title: "Bad Gateway", detail: "The token was rejected." }] },
          { status: 502 },
        ),
      ),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/token was rejected/i)).toBeInTheDocument();
  });

  it("renders an empty state when no repositories come back", async () => {
    seedProviders();
    server.use(
      http.get("/api/providers/gitlab-work/repositories", () => HttpResponse.json(listDoc([], { number: 1, size: 30, hasNext: false }))),
    );
    renderWithProviders(<SelectRepositoryPage />);
    expect(await screen.findByText(/no repositories/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Implement the shared components**

`apps/frontend/src/components/common/PageHeader.tsx`:

```tsx
import type { ReactNode } from "react";

interface PageHeaderProps {
  title: string;
  description?: string;
  actions?: ReactNode;
}

export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-4 border-b border-border pb-4">
      <div>
        <h1 className="text-xl font-semibold text-foreground">{title}</h1>
        {description ? <p className="mt-1 text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
    </header>
  );
}
```

`apps/frontend/src/components/common/EmptyState.tsx`:

```tsx
import type { ReactNode } from "react";

interface EmptyStateProps {
  title: string;
  description?: string;
  icon?: ReactNode;
}

export function EmptyState({ title, description, icon }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-md border border-dashed border-border p-10 text-center">
      {icon}
      <p className="text-sm font-medium text-foreground">{title}</p>
      {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
    </div>
  );
}
```

`apps/frontend/src/components/common/ErrorBanner.tsx`:

```tsx
import { Button } from "@/components/ui/button";

interface ErrorBannerProps {
  title: string;
  detail?: string;
  onRetry?: () => void;
}

export function ErrorBanner({ title, detail, onRetry }: ErrorBannerProps) {
  return (
    <div role="alert" className="flex items-start justify-between gap-4 rounded-md border border-destructive/40 bg-destructive/10 p-4">
      <div>
        <p className="text-sm font-medium text-foreground">{title}</p>
        {detail ? <p className="mt-1 text-sm text-muted-foreground">{detail}</p> : null}
      </div>
      {onRetry ? (
        <Button variant="outline" size="sm" onClick={onRetry}>
          Try again
        </Button>
      ) : null}
    </div>
  );
}
```

`apps/frontend/src/components/common/Pagination.tsx`:

```tsx
import { Button } from "@/components/ui/button";

interface PaginationProps {
  page: number;
  hasNext: boolean;
  onChange: (page: number) => void;
  disabled?: boolean;
}

export function Pagination({ page, hasNext, onChange, disabled = false }: PaginationProps) {
  return (
    <nav className="flex items-center justify-end gap-2" aria-label="Pagination">
      <Button variant="outline" size="sm" disabled={disabled || page <= 1} onClick={() => onChange(page - 1)}>
        Previous
      </Button>
      <span className="text-sm text-muted-foreground">Page {page}</span>
      <Button variant="outline" size="sm" disabled={disabled || !hasNext} onClick={() => onChange(page + 1)}>
        Next
      </Button>
    </nav>
  );
}
```

- [ ] **Step 3: Implement provider and repository selection**

`apps/frontend/src/lib/schemas/repository.ts`:

```ts
import { z } from "zod";

/** Mirrors the backend repository rule (FR-3.7). */
export const repositorySchema = z.object({
  repository: z
    .string()
    .min(1, "Enter a repository as owner/name")
    .regex(/^[A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)+$/, "Enter a repository as owner/name")
    .refine((value) => !value.includes(".."), "Path segments cannot contain ..")
    .refine((value) => !/^[-./]/.test(value), "A repository cannot start with -, . or /"),
});

export type RepositoryFormData = z.infer<typeof repositorySchema>;
```

`apps/frontend/src/components/features/providers/ProviderPicker.tsx`:

```tsx
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { strings } from "@/lib/strings";
import type { Provider } from "@/types/models/provider";

interface ProviderPickerProps {
  providers: Provider[];
  value: string | undefined;
  onChange: (providerId: string) => void;
  loading?: boolean;
}

export function ProviderPicker({ providers, value, onChange, loading = false }: ProviderPickerProps) {
  if (loading) {
    return <Skeleton className="h-9 w-64" />;
  }
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor="provider-picker" className="text-sm font-medium text-foreground">
        {strings.provider}
      </label>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger id="provider-picker" className="w-64">
          <SelectValue placeholder={`Select a ${strings.provider.toLowerCase()}`} />
        </SelectTrigger>
        <SelectContent>
          {providers.map((provider) => (
            <SelectItem key={provider.id} value={provider.id}>
              {provider.attributes.displayName} ({provider.attributes.kind})
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
```

`apps/frontend/src/components/features/repositories/RepositoryList.tsx`:

```tsx
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/common/EmptyState";
import type { Repository } from "@/types/models/repository";

interface RepositoryListProps {
  repositories: Repository[];
  loading: boolean;
  onSelect: (repository: Repository) => void;
}

export function RepositoryList({ repositories, loading, onSelect }: RepositoryListProps) {
  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2, 3].map((row) => (
          <Skeleton key={row} className="h-10 w-full" />
        ))}
      </div>
    );
  }
  if (repositories.length === 0) {
    return <EmptyState title="No repositories" description="This token cannot see any repositories on this provider." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repository</TableHead>
          <TableHead>Namespace</TableHead>
          <TableHead>Default branch</TableHead>
          <TableHead className="w-24" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {repositories.map((repository) => (
          <TableRow key={repository.id}>
            <TableCell className="font-medium">{repository.id}</TableCell>
            <TableCell className="text-muted-foreground">{repository.attributes.namespace}</TableCell>
            <TableCell>{repository.attributes.defaultBranch}</TableCell>
            <TableCell>
              <Button size="sm" variant="outline" onClick={() => onSelect(repository)}>
                Select
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
```

`apps/frontend/src/components/features/repositories/ManualRepositoryForm.tsx`:

```tsx
import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { repositoriesService } from "@/services/api";
import { messageFor } from "@/lib/api/errors";
import { repositorySchema, type RepositoryFormData } from "@/lib/schemas/repository";
import type { Repository } from "@/types/models/repository";

interface ManualRepositoryFormProps {
  providerId: string | undefined;
  onResolved: (repository: Repository) => void;
}

export function ManualRepositoryForm({ providerId, onResolved }: ManualRepositoryFormProps) {
  const [serverError, setServerError] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<RepositoryFormData>({ resolver: zodResolver(repositorySchema), defaultValues: { repository: "" } });

  async function onSubmit(values: RepositoryFormData) {
    if (!providerId) {
      setServerError("Select a provider first.");
      return;
    }
    setServerError(null);
    setChecking(true);
    try {
      onResolved(await repositoriesService.get(providerId, values.repository));
    } catch (error: unknown) {
      setServerError(messageFor(error, "That repository could not be read."));
    } finally {
      setChecking(false);
    }
  }

  return (
    <form className="flex flex-col gap-2" onSubmit={handleSubmit(onSubmit)} noValidate>
      <label htmlFor="manual-repository" className="text-sm font-medium text-foreground">
        Repository
      </label>
      <div className="flex items-start gap-2">
        <Input id="manual-repository" placeholder="owner/name" className="w-72" {...register("repository")} />
        <Button type="submit" disabled={checking}>
          {checking ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          Use repository
        </Button>
      </div>
      {errors.repository ? <p className="text-sm text-destructive">{errors.repository.message}</p> : null}
      {serverError ? <p className="text-sm text-destructive">{serverError}</p> : null}
    </form>
  );
}
```

`apps/frontend/src/pages/SelectRepositoryPage.tsx`:

```tsx
import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { PageHeader } from "@/components/common/PageHeader";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Pagination } from "@/components/common/Pagination";
import { ProviderPicker } from "@/components/features/providers/ProviderPicker";
import { RepositoryList } from "@/components/features/repositories/RepositoryList";
import { ManualRepositoryForm } from "@/components/features/repositories/ManualRepositoryForm";
import { useProviders } from "@/lib/hooks/api/useProviders";
import { useRepositories } from "@/lib/hooks/api/useRepositories";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { Repository } from "@/types/models/repository";

export function SelectRepositoryPage() {
  const navigate = useNavigate();
  const [providerId, setProviderId] = useState<string | undefined>(undefined);
  const [page, setPage] = useState(1);
  const providers = useProviders();
  const repositories = useRepositories(providerId, { page });

  useEffect(() => {
    const first = providers.data?.[0];
    if (!providerId && first) {
      setProviderId(first.id);
    }
  }, [providers.data, providerId]);

  function goToChanges(repository: Repository) {
    if (!providerId) return;
    const search = new URLSearchParams({ provider: providerId, repo: repository.id });
    navigate(`/select?${search.toString()}`);
  }

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <PageHeader
        title={strings.combinedReview}
        description={`Choose a ${strings.provider.toLowerCase()} and a ${strings.repository.toLowerCase()} to start.`}
      />
      {providers.isError ? (
        <ErrorBanner
          title={`Could not load ${strings.provider.toLowerCase()}s`}
          detail={messageFor(providers.error, "Try again in a moment.")}
          onRetry={() => void providers.refetch()}
        />
      ) : null}
      <ProviderPicker
        providers={providers.data ?? []}
        value={providerId}
        onChange={(id) => {
          setProviderId(id);
          setPage(1);
        }}
        loading={providers.isLoading}
      />
      <ManualRepositoryForm providerId={providerId} onResolved={goToChanges} />
      {repositories.isError ? (
        <ErrorBanner
          title={`Could not load ${strings.repository.toLowerCase()}s`}
          detail={messageFor(repositories.error, "Try again in a moment.")}
          onRetry={() => void repositories.refetch()}
        />
      ) : (
        <>
          <RepositoryList
            repositories={repositories.data?.items ?? []}
            loading={repositories.isLoading}
            onSelect={goToChanges}
          />
          <Pagination
            page={page}
            hasNext={repositories.data?.page?.hasNext ?? false}
            onChange={setPage}
            disabled={repositories.isFetching}
          />
        </>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Run tests and lint**

Run: `cd <worktree-root>/apps/frontend && export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22 && npm test && npm run lint && npx tsc --noEmit -p tsconfig.app.json`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add apps/frontend/src && git commit -m "feat(task-001): provider and repository selection views"
```

---

### Task 25: Change selection view and routing

**Files:**
- Create: `apps/frontend/src/components/features/changes/{ChangeTable,ChangeSearch,SelectionBar}.tsx`, `src/pages/SelectChangesPage.tsx`, `src/routes.tsx`, tests: `src/pages/__tests__/SelectChangesPage.test.tsx`
- Modify: `apps/frontend/src/App.tsx`

**Interfaces:**
- Produces:
  - `ChangeTable({ changes, loading, isSelected, onToggle })` — checkbox per row plus number, title, author, merged date, `source → target`, short landing SHA.
  - `ChangeSearch({ value, onChange })` — debounced (300 ms) search input.
  - `SelectionBar({ count, onBuild, onClear, building })` — shows the count and the **Build Review** button, disabled at zero.
  - `SelectChangesPage` — route `/select`; reads `?provider=` and `?repo=`; base branch input defaulting to the repository default; search, pagination, selection; creates the review and navigates to `/reviews/:id`.
  - `AppRoutes` in `src/routes.tsx` mapping `/` → `SelectRepositoryPage`, `/select` → `SelectChangesPage`, `/reviews/:id` → `ReviewPage` (added in Task 26), `*` → a not-found panel.
  - `App` wraps `BrowserRouter > QueryClientProvider > AppRoutes` plus `<Toaster />`.

- [ ] **Step 1: Write the failing page test**

`apps/frontend/src/pages/__tests__/SelectChangesPage.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { SelectChangesPage } from "@/pages/SelectChangesPage";

const navigate = vi.fn();
vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigate };
});

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => {
  server.resetHandlers();
  navigate.mockReset();
  sessionStorage.clear();
});
afterAll(() => server.close());

function changeDoc(number: number, title: string, author: string) {
  return oneDoc("changes", String(number), {
    number,
    title,
    author,
    sourceBranch: `feat/${number}`,
    targetBranch: "main",
    mergedAt: "2026-08-21T14:02:11Z",
    createdAt: "2026-08-20T09:00:00Z",
    landingSha: "a".repeat(40),
    webUrl: `https://example.test/${number}`,
  }).data;
}

function seed(changes = [changeDoc(421, "Add field-state endpoint", "jsmith"), changeDoc(427, "Fix typo", "mkay")]) {
  server.use(
    http.get("/api/providers/gh/repositories/:repo", () =>
      HttpResponse.json(oneDoc("repositories", "atlas/server", { name: "server", namespace: "atlas", defaultBranch: "main", webUrl: "u" })),
    ),
    http.get("/api/providers/gh/repositories/:repo/changes", ({ request }) => {
      const search = new URL(request.url).searchParams.get("search") ?? "";
      const filtered = search
        ? changes.filter(
            (c) =>
              c.attributes.title.toLowerCase().includes(search.toLowerCase()) ||
              c.attributes.author.toLowerCase().includes(search.toLowerCase()),
          )
        : changes;
      return HttpResponse.json(listDoc(filtered, { number: 1, size: 30, hasNext: false }));
    }),
  );
}

const route = "/select?provider=gh&repo=atlas%2Fserver";

describe("SelectChangesPage", () => {
  it("lists merged changes with their details", async () => {
    seed();
    renderWithProviders(<SelectChangesPage />, { route });
    expect(await screen.findByText("Add field-state endpoint")).toBeInTheDocument();
    const row = screen.getByText("Add field-state endpoint").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row as HTMLElement).getByText("jsmith")).toBeInTheDocument();
    expect(within(row as HTMLElement).getByText(/feat\/421/)).toBeInTheDocument();
    expect(within(row as HTMLElement).getByText(/aaaaaaa/)).toBeInTheDocument();
    expect(await screen.findByDisplayValue("main")).toBeInTheDocument();
  });

  it("keeps selections across a search and enables Build Review", async () => {
    seed();
    renderWithProviders(<SelectChangesPage />, { route });
    await screen.findByText("Add field-state endpoint");
    const build = screen.getByRole("button", { name: /build review/i });
    expect(build).toBeDisabled();
    await userEvent.click(screen.getAllByRole("checkbox")[0] as HTMLElement);
    expect(build).toBeEnabled();
    expect(screen.getByText(/1 selected/i)).toBeInTheDocument();
    await userEvent.type(screen.getByPlaceholderText(/search/i), "typo");
    await waitFor(() => expect(screen.queryByText("Add field-state endpoint")).not.toBeInTheDocument());
    expect(screen.getByText(/1 selected/i)).toBeInTheDocument();
    expect(build).toBeEnabled();
  });

  it("creates a review and navigates to it", async () => {
    seed();
    server.use(
      http.post("/api/reviews", async ({ request }) => {
        const body = (await request.json()) as { data: { attributes: { changes: number[]; baseBranch: string } } };
        expect(body.data.attributes.changes).toEqual([421]);
        expect(body.data.attributes.baseBranch).toBe("main");
        return HttpResponse.json(oneDoc("reviews", "7f14b2c8", { status: "CREATING" }), { status: 202 });
      }),
    );
    renderWithProviders(<SelectChangesPage />, { route });
    await screen.findByText("Add field-state endpoint");
    await userEvent.click(screen.getAllByRole("checkbox")[0] as HTMLElement);
    await userEvent.click(screen.getByRole("button", { name: /build review/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/reviews/7f14b2c8"));
  });

  it("shows the API detail when creating a review fails", async () => {
    seed();
    server.use(
      http.post("/api/reviews", () =>
        HttpResponse.json(
          { errors: [{ status: "400", code: "INCOMPATIBLE_TARGETS", title: "Bad Request", detail: "All selected PRs/MRs must target the same base branch." }] },
          { status: 400 },
        ),
      ),
    );
    renderWithProviders(<SelectChangesPage />, { route });
    await screen.findByText("Add field-state endpoint");
    await userEvent.click(screen.getAllByRole("checkbox")[0] as HTMLElement);
    await userEvent.click(screen.getByRole("button", { name: /build review/i }));
    expect(await screen.findByText(/same base branch/i)).toBeInTheDocument();
    expect(navigate).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Implement the change components**

`apps/frontend/src/components/features/changes/ChangeSearch.tsx`:

```tsx
import { useEffect, useState } from "react";
import { Input } from "@/components/ui/input";

interface ChangeSearchProps {
  value: string;
  onChange: (value: string) => void;
}

/** ChangeSearch debounces keystrokes by 300 ms before querying. */
export function ChangeSearch({ value, onChange }: ChangeSearchProps) {
  const [draft, setDraft] = useState(value);

  useEffect(() => setDraft(value), [value]);

  useEffect(() => {
    const timer = setTimeout(() => {
      if (draft !== value) {
        onChange(draft);
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [draft, onChange, value]);

  return (
    <Input
      className="w-80"
      placeholder="Search by number, title or author"
      value={draft}
      onChange={(event) => setDraft(event.target.value)}
      aria-label="Search PRs/MRs"
    />
  );
}
```

`apps/frontend/src/components/features/changes/ChangeTable.tsx`:

```tsx
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/common/EmptyState";
import { shortSha, type Change } from "@/types/models/change";

interface ChangeTableProps {
  changes: Change[];
  loading: boolean;
  isSelected: (n: number) => boolean;
  onToggle: (change: Change) => void;
}

function formatDate(value: string | null): string {
  return value ? new Date(value).toLocaleDateString() : "—";
}

export function ChangeTable({ changes, loading, isSelected, onToggle }: ChangeTableProps) {
  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2, 3, 4].map((row) => (
          <Skeleton key={row} className="h-10 w-full" />
        ))}
      </div>
    );
  }
  if (changes.length === 0) {
    return <EmptyState title="No merged PRs/MRs" description="Nothing matches this base branch and search." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-10" />
          <TableHead className="w-20">Number</TableHead>
          <TableHead>Title</TableHead>
          <TableHead>Author</TableHead>
          <TableHead>Merged</TableHead>
          <TableHead>Branches</TableHead>
          <TableHead>Landing</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {changes.map((change) => {
          const { number, title, author, mergedAt, sourceBranch, targetBranch, landingSha } = change.attributes;
          return (
            <TableRow key={change.id}>
              <TableCell>
                <Checkbox
                  checked={isSelected(number)}
                  onCheckedChange={() => onToggle(change)}
                  aria-label={`Select #${number}`}
                />
              </TableCell>
              <TableCell className="font-mono text-sm">#{number}</TableCell>
              <TableCell className="font-medium">{title}</TableCell>
              <TableCell className="text-muted-foreground">{author}</TableCell>
              <TableCell className="text-muted-foreground">{formatDate(mergedAt)}</TableCell>
              <TableCell className="text-muted-foreground">
                {sourceBranch} to {targetBranch}
              </TableCell>
              <TableCell className="font-mono text-xs text-muted-foreground">{shortSha(landingSha)}</TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
```

`apps/frontend/src/components/features/changes/SelectionBar.tsx`:

```tsx
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { strings } from "@/lib/strings";

interface SelectionBarProps {
  count: number;
  building: boolean;
  onBuild: () => void;
  onClear: () => void;
}

export function SelectionBar({ count, building, onBuild, onClear }: SelectionBarProps) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-md border border-border bg-card p-3">
      <p className="text-sm text-muted-foreground">{count} selected</p>
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={onClear} disabled={count === 0 || building}>
          Clear
        </Button>
        <Button onClick={onBuild} disabled={count === 0 || building}>
          {building ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {strings.buildReview}
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Implement the page and routing**

`apps/frontend/src/pages/SelectChangesPage.tsx`:

```tsx
import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Pagination } from "@/components/common/Pagination";
import { ChangeSearch } from "@/components/features/changes/ChangeSearch";
import { ChangeTable } from "@/components/features/changes/ChangeTable";
import { SelectionBar } from "@/components/features/changes/SelectionBar";
import { Input } from "@/components/ui/input";
import { useChanges } from "@/lib/hooks/api/useChanges";
import { useRepository } from "@/lib/hooks/api/useRepositories";
import { useCreateReview } from "@/lib/hooks/api/useReviews";
import { useSelection } from "@/lib/hooks/useSelection";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";

export function SelectChangesPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const providerId = params.get("provider") ?? undefined;
  const repository = params.get("repo") ?? undefined;

  const [baseBranch, setBaseBranch] = useState("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [createError, setCreateError] = useState<string | null>(null);

  const repositoryQuery = useRepository(providerId, repository ?? "", Boolean(repository));
  const defaultBranch = repositoryQuery.data?.attributes.defaultBranch;

  useEffect(() => {
    if (defaultBranch && baseBranch === "") {
      setBaseBranch(defaultBranch);
    }
  }, [defaultBranch, baseBranch]);

  const selection = useSelection(`converge:selection:${providerId ?? ""}/${repository ?? ""}`);
  const changes = useChanges(providerId, repository, { target: baseBranch || undefined, search, page });
  const createReview = useCreateReview();

  if (!providerId || !repository) {
    return (
      <div className="mx-auto max-w-5xl p-6">
        <ErrorBanner title="Missing selection" detail="Go back and choose a provider and repository." />
      </div>
    );
  }

  async function build() {
    setCreateError(null);
    try {
      const review = await createReview.mutateAsync({
        provider: providerId as string,
        repository: repository as string,
        baseBranch: baseBranch || undefined,
        changes: selection.numbers,
      });
      selection.clear();
      navigate(`/reviews/${review.id}`);
    } catch (error: unknown) {
      const detail = messageFor(error, "The review could not be started.");
      setCreateError(detail);
      toast.error(detail);
    }
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4 p-6">
      <PageHeader title={repository} description={`${strings.provider}: ${providerId}`} />
      <div className="flex flex-wrap items-end gap-4">
        <div className="flex flex-col gap-1">
          <label htmlFor="base-branch" className="text-sm font-medium text-foreground">
            {strings.base}
          </label>
          <Input
            id="base-branch"
            className="w-64"
            value={baseBranch}
            onChange={(event) => {
              setBaseBranch(event.target.value);
              setPage(1);
            }}
          />
        </div>
        <ChangeSearch
          value={search}
          onChange={(value) => {
            setSearch(value);
            setPage(1);
          }}
        />
      </div>
      {createError ? <ErrorBanner title="Could not start the review" detail={createError} /> : null}
      {changes.isError ? (
        <ErrorBanner
          title={`Could not load ${strings.includedChanges.toLowerCase()}`}
          detail={messageFor(changes.error, "Try again in a moment.")}
          onRetry={() => void changes.refetch()}
        />
      ) : (
        <ChangeTable
          changes={changes.data?.items ?? []}
          loading={changes.isLoading}
          isSelected={selection.isSelected}
          onToggle={selection.toggle}
        />
      )}
      <Pagination
        page={page}
        hasNext={changes.data?.page?.hasNext ?? false}
        onChange={setPage}
        disabled={changes.isFetching}
      />
      <SelectionBar
        count={selection.count}
        building={createReview.isPending}
        onBuild={() => void build()}
        onClear={selection.clear}
      />
    </div>
  );
}
```

`apps/frontend/src/routes.tsx`:

```tsx
import { Route, Routes } from "react-router";
import { SelectRepositoryPage } from "@/pages/SelectRepositoryPage";
import { SelectChangesPage } from "@/pages/SelectChangesPage";
import { ReviewPage } from "@/pages/ReviewPage";
import { EmptyState } from "@/components/common/EmptyState";

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<SelectRepositoryPage />} />
      <Route path="/select" element={<SelectChangesPage />} />
      <Route path="/reviews/:id" element={<ReviewPage />} />
      <Route
        path="*"
        element={
          <div className="mx-auto max-w-3xl p-10">
            <EmptyState title="Page not found" description="That address does not exist." />
          </div>
        }
      />
    </Routes>
  );
}
```

`apps/frontend/src/App.tsx`:

```tsx
import { QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import { Toaster } from "sonner";
import { AppRoutes } from "@/routes";
import { createQueryClient } from "@/lib/query-client";

const queryClient = createQueryClient();

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppRoutes />
        <Toaster richColors position="top-right" />
      </BrowserRouter>
    </QueryClientProvider>
  );
}
```

`ReviewPage` does not exist yet, so this task's build will fail until Task 26. Create a placeholder now so the app compiles, and replace it in Task 26:

`apps/frontend/src/pages/ReviewPage.tsx` (placeholder):

```tsx
export function ReviewPage() {
  return <div className="p-6">Loading review…</div>;
}
```

- [ ] **Step 4: Run tests, lint and build**

Run: `cd <worktree-root>/apps/frontend && export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22 && npm test && npm run lint && npm run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add apps/frontend/src && git commit -m "feat(task-001): PR/MR selection view and application routing"
```

---

### Task 26: Review page — status, error panel, header, file tree, diff

**Files:**
- Create: `apps/frontend/src/components/features/review/{ReviewStatus,ReviewErrorPanel,ReviewHeader,FileTree,FileDiff}.tsx`, tests: `src/components/features/review/__tests__/{ReviewStatus,ReviewErrorPanel,FileTree}.test.tsx`, `src/pages/__tests__/ReviewPage.test.tsx`
- Modify: `apps/frontend/src/pages/ReviewPage.tsx` (replace the placeholder)

**Interfaces:**
- Produces:
  - `ReviewStatus({ stage })` — skeleton layout plus a human stage label: `resolving` → "Resolving PRs/MRs", `updating-repository` → "Updating repository", `creating-workspace` → "Preparing review", `applying:<n>` → "Applying #<n>", `diffing` → "Computing the combined diff".
  - `ReviewErrorPanel({ review, onDiscard, discarding })` — human message, offending number, conflicting files list, applied changes, a collapsible **Diagnostics** section (error code, base SHA, workspace path, branch, strategy) and a **Discard Review** button.
  - `ReviewHeader({ review, onFinish, finishing })` — repository, `base branch @ shortSha`, `baseDescription`, included changes as provider links, totals, **Finish Review**.
  - `FileTree({ files, selectedPath, onSelect })` — grouped by directory with a status badge and `+a −d`.
  - `FileDiff({ file })` — `React.lazy`-wrapped `@pierre/diffs` `PatchDiff`; renders "Binary file changed" for binaries and a truncation notice when `truncated`.
  - `ReviewPage` — reads `:id`, polls, and switches between status, error and diff layouts; auto-selects the first file.

- [ ] **Step 1: Write the failing component tests**

`apps/frontend/src/components/features/review/__tests__/ReviewStatus.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ReviewStatus } from "@/components/features/review/ReviewStatus";

describe("ReviewStatus", () => {
  it("renders a human label for each stage", () => {
    const cases: Array<[string | null, RegExp]> = [
      ["resolving", /resolving/i],
      ["updating-repository", /updating repository/i],
      ["creating-workspace", /preparing review/i],
      ["applying:435", /applying #435/i],
      ["diffing", /combined diff/i],
      [null, /building/i],
    ];
    for (const [stage, expected] of cases) {
      const { unmount } = render(<ReviewStatus stage={stage} />);
      expect(screen.getByText(expected)).toBeInTheDocument();
      unmount();
    }
  });

  it("never shows git vocabulary", () => {
    const { container } = render(<ReviewStatus stage="creating-workspace" />);
    expect(container.textContent?.toLowerCase()).not.toContain("worktree");
    expect(container.textContent?.toLowerCase()).not.toContain("cherry-pick");
  });
});
```

`apps/frontend/src/components/features/review/__tests__/ReviewErrorPanel.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ReviewErrorPanel } from "@/components/features/review/ReviewErrorPanel";
import type { Review } from "@/types/models/review";

function conflictedReview(): Review {
  return {
    type: "reviews",
    id: "7f14b2c8",
    attributes: {
      status: "CONFLICTED",
      stage: null,
      provider: "gitlab-work",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: "9f21a43".padEnd(40, "0"),
      headSha: null,
      baseDescription: "Immediately before #421",
      changes: [421, 435],
      included: [],
      totals: null,
      error: {
        code: "CONFLICT",
        message: "#435 conflicts while being applied. It may depend on work that is not part of this review.",
        change: 435,
        commit: "c0ffee".padEnd(40, "0"),
        conflictingFiles: ["src/field/FieldService.java"],
        appliedChanges: [421],
        possibleDependency: true,
        diagnostics: {
          workspacePath: "/data/workspaces/7f14b2c8/repo",
          branch: "review/7f14b2c8",
          strategy: "squash",
          sourceSha: "abc".padEnd(40, "0"),
        },
      },
      createdAt: "2026-09-01T12:00:00Z",
      updatedAt: "2026-09-01T12:01:00Z",
      expiresAt: "2026-09-02T12:00:00Z",
    },
  };
}

describe("ReviewErrorPanel", () => {
  it("shows the message, offending change and conflicting files", () => {
    render(<ReviewErrorPanel review={conflictedReview()} onDiscard={vi.fn()} discarding={false} />);
    expect(screen.getByText(/#435 conflicts/i)).toBeInTheDocument();
    expect(screen.getByText("src/field/FieldService.java")).toBeInTheDocument();
    expect(screen.getByText(/#421/)).toBeInTheDocument();
  });

  it("hides git vocabulary until Diagnostics is expanded", async () => {
    render(<ReviewErrorPanel review={conflictedReview()} onDiscard={vi.fn()} discarding={false} />);
    expect(screen.queryByText(/review\/7f14b2c8/)).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /diagnostics/i }));
    expect(await screen.findByText(/review\/7f14b2c8/)).toBeInTheDocument();
    expect(screen.getByText("CONFLICT")).toBeInTheDocument();
    expect(screen.getByText(/\/data\/workspaces\/7f14b2c8\/repo/)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /workspace/i })).not.toBeInTheDocument();
  });

  it("calls onDiscard", async () => {
    const onDiscard = vi.fn();
    render(<ReviewErrorPanel review={conflictedReview()} onDiscard={onDiscard} discarding={false} />);
    await userEvent.click(screen.getByRole("button", { name: /discard review/i }));
    expect(onDiscard).toHaveBeenCalledTimes(1);
  });
});
```

`apps/frontend/src/components/features/review/__tests__/FileTree.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { FileTree } from "@/components/features/review/FileTree";
import type { ReviewFile } from "@/types/models/reviewFile";

function file(path: string, status: ReviewFile["attributes"]["status"], additions: number, deletions: number): ReviewFile {
  return { type: "review-files", id: path, attributes: { path, previousPath: null, status, additions, deletions, binary: false } };
}

describe("FileTree", () => {
  const files = [
    file("src/field/FieldService.java", "modified", 40, 12),
    file("src/field/FieldMapper.java", "added", 10, 0),
    file("README.md", "deleted", 0, 5),
  ];

  it("groups files by directory and shows counts", () => {
    render(<FileTree files={files} selectedPath="README.md" onSelect={vi.fn()} />);
    expect(screen.getByText("src/field")).toBeInTheDocument();
    expect(screen.getByText("FieldService.java")).toBeInTheDocument();
    expect(screen.getByText("+40")).toBeInTheDocument();
    expect(screen.getByText("−12")).toBeInTheDocument();
    expect(screen.getAllByText(/added|modified|deleted/i).length).toBeGreaterThanOrEqual(3);
  });

  it("marks the selected file and reports clicks", async () => {
    const onSelect = vi.fn();
    render(<FileTree files={files} selectedPath="README.md" onSelect={onSelect} />);
    const selected = screen.getByRole("button", { name: /README\.md/ });
    expect(selected).toHaveAttribute("aria-current", "true");
    await userEvent.click(screen.getByRole("button", { name: /FieldMapper\.java/ }));
    expect(onSelect).toHaveBeenCalledWith("src/field/FieldMapper.java");
  });
});
```

`apps/frontend/src/pages/__tests__/ReviewPage.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { http, HttpResponse, listDoc, oneDoc, server } from "@/test/server";
import { renderWithProviders } from "@/test/render";
import { ReviewPage } from "@/pages/ReviewPage";

const navigate = vi.fn();
vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>("react-router");
  return { ...actual, useNavigate: () => navigate, useParams: () => ({ id: "7f14b2c8" }) };
});

// The diff renderer pulls in Shiki; stub it for component tests.
vi.mock("@/components/features/review/FileDiff", () => ({
  FileDiff: ({ file }: { file: { attributes: { path: string; diff: string } } }) => (
    <pre data-testid="file-diff">{`${file.attributes.path}\n${file.attributes.diff}`}</pre>
  ),
}));

beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => {
  server.resetHandlers();
  navigate.mockReset();
});
afterAll(() => server.close());

function reviewDoc(status: string, stage: string | null) {
  return oneDoc("reviews", "7f14b2c8", {
    status,
    stage,
    provider: "gitlab-work",
    repository: "atlas/server",
    baseBranch: "main",
    baseSha: status === "READY" ? "9f21a43".padEnd(40, "0") : null,
    headSha: status === "READY" ? "b".repeat(40) : null,
    baseDescription: "Immediately before #421",
    changes: [421],
    included:
      status === "READY"
        ? [{ number: 421, title: "Add field-state endpoint", author: "jsmith", mergedAt: "2026-08-21T14:02:11Z", webUrl: "https://example.test/421", strategy: "squash" }]
        : [],
    totals: status === "READY" ? { files: 1, additions: 40, deletions: 12 } : null,
    error: null,
    createdAt: "2026-09-01T12:00:00Z",
    updatedAt: "2026-09-01T12:00:30Z",
    expiresAt: "2026-09-02T12:00:00Z",
  });
}

describe("ReviewPage", () => {
  it("shows the building stage then switches to the diff view", async () => {
    let calls = 0;
    server.use(
      http.get("/api/reviews/7f14b2c8", () => {
        calls += 1;
        return HttpResponse.json(calls < 2 ? reviewDoc("CREATING", "applying:421") : reviewDoc("READY", null));
      }),
      http.get("/api/reviews/7f14b2c8/files", () =>
        HttpResponse.json(
          listDoc([
            oneDoc("review-files", "src/field/FieldService.java", {
              path: "src/field/FieldService.java",
              previousPath: null,
              status: "modified",
              additions: 40,
              deletions: 12,
              binary: false,
            }).data,
          ]),
        ),
      ),
      http.get("/api/reviews/7f14b2c8/files/src/field/FieldService.java", () =>
        HttpResponse.json(
          oneDoc("review-file-diffs", "src/field/FieldService.java", {
            path: "src/field/FieldService.java",
            previousPath: null,
            status: "modified",
            additions: 40,
            deletions: 12,
            binary: false,
            truncated: false,
            diff: "@@ -1 +1 @@\n-old\n+new",
          }),
        ),
      ),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/applying #421/i)).toBeInTheDocument();
    expect(await screen.findByText(/Immediately before #421/, {}, { timeout: 6000 })).toBeInTheDocument();
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
    expect(screen.getByText(/main @ 9f21a43/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /#421/ })).toHaveAttribute("href", "https://example.test/421");
    // the first file is selected automatically
    expect(await screen.findByTestId("file-diff")).toHaveTextContent("+new");
  }, 15000);

  it("finishes the review and returns to the start", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => HttpResponse.json(reviewDoc("READY", null))),
      http.get("/api/reviews/7f14b2c8/files", () => HttpResponse.json(listDoc([]))),
      http.delete("/api/reviews/7f14b2c8", () => new HttpResponse(null, { status: 204 })),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    await userEvent.click(await screen.findByRole("button", { name: /finish review/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/"));
  });

  it("renders the error panel for a failed review", async () => {
    server.use(
      http.get("/api/reviews/7f14b2c8", () => {
        const doc = reviewDoc("FAILED", null);
        doc.data.attributes.error = {
          code: "NOT_MERGED",
          message: "#421 is not merged yet. Converge can only reconstruct merged PRs/MRs.",
          change: 421,
        };
        return HttpResponse.json(doc);
      }),
      http.delete("/api/reviews/7f14b2c8", () => new HttpResponse(null, { status: 204 })),
    );
    renderWithProviders(<ReviewPage />, { route: "/reviews/7f14b2c8" });
    expect(await screen.findByText(/is not merged yet/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /discard review/i }));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith("/"));
  });
});
```

- [ ] **Step 2: Implement the review components**

`apps/frontend/src/components/features/review/ReviewStatus.tsx`:

```tsx
import { Skeleton } from "@/components/ui/skeleton";

interface ReviewStatusProps {
  stage: string | null;
}

/** stageLabel converts a backend stage into product vocabulary (FR-10.11). */
export function stageLabel(stage: string | null): string {
  if (!stage) return "Building the review";
  if (stage.startsWith("applying:")) return `Applying #${stage.slice("applying:".length)}`;
  switch (stage) {
    case "resolving":
      return "Resolving PRs/MRs";
    case "updating-repository":
      return "Updating repository";
    case "creating-workspace":
      return "Preparing review";
    case "diffing":
      return "Computing the combined diff";
    default:
      return "Building the review";
  }
}

export function ReviewStatus({ stage }: ReviewStatusProps) {
  return (
    <div className="flex flex-col gap-4" aria-live="polite">
      <p className="text-sm font-medium text-foreground">{stageLabel(stage)}</p>
      <Skeleton className="h-8 w-1/3" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}
```

`apps/frontend/src/components/features/review/ReviewErrorPanel.tsx`:

```tsx
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

interface ReviewErrorPanelProps {
  review: Review;
  onDiscard: () => void;
  discarding: boolean;
}

export function ReviewErrorPanel({ review, onDiscard, discarding }: ReviewErrorPanelProps) {
  const error = review.attributes.error;
  if (!error) return null;
  const diagnostics = error.diagnostics;
  return (
    <section className="flex flex-col gap-4 rounded-md border border-destructive/40 bg-destructive/5 p-6">
      <div>
        <h2 className="text-lg font-semibold text-foreground">
          {error.code === "CONFLICT" ? strings.conflict : "This review could not be built"}
        </h2>
        <p className="mt-1 text-sm text-foreground">{error.message}</p>
      </div>
      {error.conflictingFiles && error.conflictingFiles.length > 0 ? (
        <div>
          <p className="text-sm font-medium text-foreground">Files involved</p>
          <ul className="mt-1 list-inside list-disc text-sm text-muted-foreground">
            {error.conflictingFiles.map((path) => (
              <li key={path} className="font-mono">
                {path}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {error.appliedChanges && error.appliedChanges.length > 0 ? (
        <p className="text-sm text-muted-foreground">
          Applied before the problem: {error.appliedChanges.map((n) => `#${n}`).join(", ")}
        </p>
      ) : null}
      <Collapsible>
        <CollapsibleTrigger asChild>
          <Button variant="outline" size="sm">
            {strings.diagnostics}
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-2 rounded-md border border-border bg-background p-3 font-mono text-xs text-muted-foreground">
          <dl className="grid grid-cols-[10rem_1fr] gap-1">
            <dt>Error code</dt>
            <dd>{error.code}</dd>
            {review.attributes.baseSha ? (
              <>
                <dt>Base SHA</dt>
                <dd>{review.attributes.baseSha}</dd>
              </>
            ) : null}
            {error.commit ? (
              <>
                <dt>Commit</dt>
                <dd>{error.commit}</dd>
              </>
            ) : null}
            {diagnostics?.strategy ? (
              <>
                <dt>Strategy</dt>
                <dd>{diagnostics.strategy}</dd>
              </>
            ) : null}
            {diagnostics?.branch ? (
              <>
                <dt>Branch</dt>
                <dd>{diagnostics.branch}</dd>
              </>
            ) : null}
            {diagnostics?.workspacePath ? (
              <>
                <dt>Workspace path</dt>
                <dd>{diagnostics.workspacePath}</dd>
              </>
            ) : null}
          </dl>
        </CollapsibleContent>
      </Collapsible>
      <div>
        <Button variant="destructive" onClick={onDiscard} disabled={discarding}>
          {discarding ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {strings.discardReview}
        </Button>
      </div>
    </section>
  );
}
```

`apps/frontend/src/components/features/review/ReviewHeader.tsx`:

```tsx
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

interface ReviewHeaderProps {
  review: Review;
  onFinish: () => void;
  finishing: boolean;
}

export function ReviewHeader({ review, onFinish, finishing }: ReviewHeaderProps) {
  const { repository, baseBranch, baseSha, baseDescription, included, totals } = review.attributes;
  return (
    <header className="flex flex-col gap-3 border-b border-border pb-4">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold text-foreground">{repository}</h1>
          <p className="text-sm text-muted-foreground">
            {strings.base}: {baseBranch} @ {baseSha ? baseSha.slice(0, 7) : "unknown"}
            {baseDescription ? ` · ${baseDescription}` : ""}
          </p>
        </div>
        <Button onClick={onFinish} disabled={finishing}>
          {finishing ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {strings.finishReview}
        </Button>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium text-foreground">{strings.includedChanges}:</span>
        {included.map((change) => (
          <a
            key={change.number}
            href={change.webUrl}
            target="_blank"
            rel="noreferrer"
            className="text-sm text-primary underline-offset-2 hover:underline"
          >
            #{change.number} {change.title}
          </a>
        ))}
      </div>
      {totals ? (
        <div className="flex items-center gap-2">
          <Badge variant="secondary">{totals.files} files</Badge>
          <Badge variant="secondary">+{totals.additions}</Badge>
          <Badge variant="secondary">−{totals.deletions}</Badge>
        </div>
      ) : null}
    </header>
  );
}
```

`apps/frontend/src/components/features/review/FileTree.tsx`:

```tsx
import { useMemo } from "react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { ReviewFile } from "@/types/models/reviewFile";

interface FileTreeProps {
  files: ReviewFile[];
  selectedPath: string | undefined;
  onSelect: (path: string) => void;
}

function directoryOf(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? "/" : path.slice(0, index);
}

function baseNameOf(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

export function FileTree({ files, selectedPath, onSelect }: FileTreeProps) {
  const grouped = useMemo(() => {
    const map = new Map<string, ReviewFile[]>();
    for (const file of files) {
      const dir = directoryOf(file.attributes.path);
      const bucket = map.get(dir);
      if (bucket) {
        bucket.push(file);
      } else {
        map.set(dir, [file]);
      }
    }
    return [...map.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [files]);

  return (
    <nav className="flex flex-col gap-3" aria-label="Changed files">
      {grouped.map(([directory, entries]) => (
        <div key={directory}>
          <p className="px-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">{directory}</p>
          <ul>
            {entries.map((file) => {
              const { path, status, additions, deletions } = file.attributes;
              const active = path === selectedPath;
              return (
                <li key={path}>
                  <button
                    type="button"
                    aria-current={active ? "true" : undefined}
                    onClick={() => onSelect(path)}
                    className={cn(
                      "flex w-full items-center justify-between gap-2 rounded px-2 py-1 text-left text-sm",
                      active ? "bg-accent text-accent-foreground" : "hover:bg-muted",
                    )}
                  >
                    <span className="truncate font-mono">{baseNameOf(path)}</span>
                    <span className="flex shrink-0 items-center gap-1">
                      <Badge variant="outline" className="capitalize">
                        {status}
                      </Badge>
                      <span className="text-xs text-emerald-600">+{additions}</span>
                      <span className="text-xs text-destructive">−{deletions}</span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      ))}
    </nav>
  );
}
```

`apps/frontend/src/components/features/review/FileDiff.tsx`:

```tsx
import { PatchDiff } from "@pierre/diffs/react";
import type { ReviewFileDiff } from "@/types/models/reviewFile";

interface FileDiffProps {
  file: ReviewFileDiff;
}

/**
 * FileDiff renders one file's unified diff. @pierre/diffs parses the raw patch
 * text directly, so no full file contents are needed (FR-7.4).
 */
export function FileDiff({ file }: FileDiffProps) {
  const { binary, truncated, diff, path } = file.attributes;
  if (binary) {
    return <p className="rounded-md border border-border p-4 text-sm text-muted-foreground">Binary file changed</p>;
  }
  return (
    <div className="flex flex-col gap-2">
      {truncated ? (
        <p className="rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-sm text-foreground">
          This file is too large to display in full. Showing the first part of the change.
        </p>
      ) : null}
      <PatchDiff
        key={path}
        patch={diff}
        options={{
          diffStyle: "unified",
          expandUnchanged: true,
          collapsedContextThreshold: 8,
          overflow: "scroll",
        }}
      />
    </div>
  );
}
```

- [ ] **Step 3: Implement the review page**

`apps/frontend/src/pages/ReviewPage.tsx` (replaces the placeholder):

```tsx
import { Suspense, lazy, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { EmptyState } from "@/components/common/EmptyState";
import { Skeleton } from "@/components/ui/skeleton";
import { ReviewStatus } from "@/components/features/review/ReviewStatus";
import { ReviewErrorPanel } from "@/components/features/review/ReviewErrorPanel";
import { ReviewHeader } from "@/components/features/review/ReviewHeader";
import { FileTree } from "@/components/features/review/FileTree";
import { useFinishReview, useReview, useReviewFile, useReviewFiles } from "@/lib/hooks/api/useReviews";
import { messageFor } from "@/lib/api/errors";

// Shiki is heavy; keep the diff renderer out of the initial bundle.
const FileDiff = lazy(async () => ({
  default: (await import("@/components/features/review/FileDiff")).FileDiff,
}));

export function ReviewPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const review = useReview(id);
  const status = review.data?.attributes.status;
  const files = useReviewFiles(id, status === "READY");
  const [selectedPath, setSelectedPath] = useState<string | undefined>(undefined);
  const fileDiff = useReviewFile(id, selectedPath);
  const finish = useFinishReview();

  useEffect(() => {
    const first = files.data?.[0];
    if (!selectedPath && first) {
      setSelectedPath(first.attributes.path);
    }
  }, [files.data, selectedPath]);

  async function discard() {
    if (!id) return;
    try {
      await finish.mutateAsync(id);
      navigate("/");
    } catch (error: unknown) {
      toast.error(messageFor(error, "The review could not be closed."));
    }
  }

  if (review.isError) {
    return (
      <div className="mx-auto max-w-3xl p-6">
        <ErrorBanner
          title="Could not load this review"
          detail={messageFor(review.error, "It may have expired.")}
          onRetry={() => void review.refetch()}
        />
      </div>
    );
  }

  if (!review.data) {
    return (
      <div className="mx-auto max-w-5xl p-6">
        <Skeleton className="h-8 w-1/3" />
      </div>
    );
  }

  if (status === "CREATING") {
    return (
      <div className="mx-auto max-w-5xl p-6">
        <ReviewStatus stage={review.data.attributes.stage} />
      </div>
    );
  }

  if (status === "CONFLICTED" || status === "FAILED") {
    return (
      <div className="mx-auto max-w-3xl p-6">
        <ReviewErrorPanel review={review.data} onDiscard={() => void discard()} discarding={finish.isPending} />
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-4 p-6">
      <ReviewHeader review={review.data} onFinish={() => void discard()} finishing={finish.isPending} />
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[18rem_1fr]">
        <aside className="lg:sticky lg:top-4 lg:self-start">
          {files.isLoading ? (
            <div className="space-y-2">
              {[0, 1, 2, 3].map((row) => (
                <Skeleton key={row} className="h-8 w-full" />
              ))}
            </div>
          ) : (
            <FileTree files={files.data ?? []} selectedPath={selectedPath} onSelect={setSelectedPath} />
          )}
        </aside>
        <section>
          {files.data && files.data.length === 0 ? (
            <EmptyState title="No file changes" description="The selected PRs/MRs produce no net change." />
          ) : fileDiff.isLoading || !fileDiff.data ? (
            <Skeleton className="h-96 w-full" />
          ) : (
            <Suspense fallback={<Skeleton className="h-96 w-full" />}>
              <FileDiff file={fileDiff.data} />
            </Suspense>
          )}
        </section>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run the frontend gate**

Run: `cd <worktree-root>/apps/frontend && export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22 && npm test && npm run lint && npm run format:check && npm run build`
Expected: PASS. If `@pierre/diffs` 1.4's `PatchDiff` props differ from `patch` + `options` as typed in `node_modules/@pierre/diffs/dist/react/PatchDiff.d.ts`, correct the call to match the installed types and note the change in `context.md`. If the library cannot be made to work, fall back to `@git-diff-view/react` with lowlight and drop `expandUnchanged`, as the design allows.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add apps/frontend/src && git commit -m "feat(task-001): review status, error panel, file tree and diff view"
```

---

## Phase G — Packaging, CI, documentation

### Task 27: Makefile targets, build scripts, Docker image, compose

**Files:**
- Create: `tools/docker-push.sh`, `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `.env.example`
- Modify: `Makefile`, `tools/build-backend.sh` (replace the Task 21 stub)

**Interfaces:**
- Produces:
  - `tools/build-backend.sh` — cross-compiles `converge` and `converge-cli` for `linux/amd64` and `linux/arm64` into `dist/<os>-<arch>/`, then tars each platform to `dist/converge-<version>-<os>-<arch>.tar.gz`. Reads `VERSION` from the environment or `tools/version.sh`.
  - `tools/docker-push.sh` — pushes `$IMAGE:$VERSION`, `$IMAGE:$GIT_SHA`, and `$IMAGE:latest` when `MAINLINE=1`. `IMAGE` resolves from `IMAGE_REPOSITORY`, else `ghcr.io/$GITHUB_REPOSITORY`, else `$CI_REGISTRY_IMAGE`; it fails with a clear message when none is set.
  - Make targets: `help`, `version`, `lint`, `test`, `test-integration`, `build`, `docker-build`, `docker-push`, `release-github`, `dev`, `clean`.
  - `Dockerfile` — three stages (`frontend` on `node:22-alpine`, `backend` on `golang:1.27-alpine`, final `alpine:3.22` with `git` and `ca-certificates`), non-root user `converge` (uid 10001) owning `/data`, `EXPOSE 8080`, `HEALTHCHECK` on `/healthz`.
  - `docker-compose.yml` — one service, `env_file: .env`, named volumes `converge-repositories` and `converge-workspaces`, healthcheck, port mapping from `APP_PORT`.
  - `.env.example` — every variable from Global Constraints with placeholder values.

- [ ] **Step 1: Write the build scripts**

`tools/build-backend.sh` (mode 755, replacing the stub):

```bash
#!/usr/bin/env bash
# Cross-compiles the Converge binaries and packages release tarballs.
#
#   VERSION   overrides tools/version.sh
#   PLATFORMS space-separated os/arch list (default "linux/amd64 linux/arm64")
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
version="${VERSION:-$("$root/tools/version.sh")}"
platforms="${PLATFORMS:-linux/amd64 linux/arm64}"
dist="$root/dist"
ldflags="-s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=${version}"

rm -rf "$dist"
mkdir -p "$dist"

for platform in $platforms; do
  os="${platform%%/*}"
  arch="${platform##*/}"
  outdir="$dist/${os}-${arch}"
  mkdir -p "$outdir"
  for cmd in converge converge-cli; do
    echo "building $cmd for $os/$arch ($version)"
    (cd "$root/apps/backend" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
      go build -trimpath -ldflags "$ldflags" -o "$outdir/$cmd" "./cmd/$cmd")
  done
  tar -czf "$dist/converge-${version}-${os}-${arch}.tar.gz" -C "$outdir" converge converge-cli
done

echo "artifacts in $dist:"
ls -1 "$dist"/*.tar.gz
```

`tools/docker-push.sh` (mode 755):

```bash
#!/usr/bin/env bash
# Pushes the built image tags. The registry is never hard-coded: it comes from
# IMAGE_REPOSITORY, GITHUB_REPOSITORY (GHCR), or CI_REGISTRY_IMAGE (GitLab).
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
version="${VERSION:-$("$root/tools/version.sh")}"
git_sha="${GIT_SHA:-$(git rev-parse HEAD)}"

if [[ -n "${IMAGE_REPOSITORY:-}" ]]; then
  image="$IMAGE_REPOSITORY"
elif [[ -n "${GITHUB_REPOSITORY:-}" ]]; then
  image="ghcr.io/${GITHUB_REPOSITORY,,}"
elif [[ -n "${CI_REGISTRY_IMAGE:-}" ]]; then
  image="$CI_REGISTRY_IMAGE"
else
  echo "docker-push: set IMAGE_REPOSITORY, GITHUB_REPOSITORY or CI_REGISTRY_IMAGE" >&2
  exit 1
fi

tags=("$image:$version" "$image:$git_sha")
if [[ "${MAINLINE:-0}" == "1" ]]; then
  tags+=("$image:latest")
fi

for tag in "${tags[@]}"; do
  echo "pushing $tag"
  docker push "$tag"
done
```

- [ ] **Step 2: Finish the Makefile**

Replace the whole `Makefile` with:

```make
SHELL := /bin/bash
.DEFAULT_GOAL := help

ROOT      := $(shell git rev-parse --show-toplevel)
BACKEND   := $(ROOT)/apps/backend
FRONTEND  := $(ROOT)/apps/frontend
VERSION   ?= $(shell $(ROOT)/tools/version.sh)
GIT_SHA   ?= $(shell git rev-parse HEAD)
LDFLAGS   := -s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=$(VERSION)
IMAGE     ?= $(if $(IMAGE_REPOSITORY),$(IMAGE_REPOSITORY),$(if $(CI_REGISTRY_IMAGE),$(CI_REGISTRY_IMAGE),converge))
PLATFORM  ?= linux/amd64
NPM       := export NVM_DIR="$$HOME/.nvm" && [ -s "$$NVM_DIR/nvm.sh" ] && . "$$NVM_DIR/nvm.sh" >/dev/null && nvm use 22 >/dev/null; npm

.PHONY: help version lint test test-integration build docker-build docker-push release-github dev clean

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

version: ## Print the build version
	@echo $(VERSION)

lint: ## Lint backend and frontend
	cd $(BACKEND) && go vet ./... && go tool golangci-lint run
	cd $(FRONTEND) && $(NPM) run lint && $(NPM) run format:check

test: ## Unit tests, both apps
	cd $(BACKEND) && go test -race -count=1 ./...
	cd $(FRONTEND) && $(NPM) test

test-integration: ## Reconstruction integration tests (local git only, no network)
	cd $(BACKEND) && go test -race -count=1 -tags integration ./...

build: ## Build the UI into the embed directory, then cross-compile the binaries
	cd $(FRONTEND) && $(NPM) ci && $(NPM) run build
	touch $(BACKEND)/internal/ui/dist/.gitkeep
	VERSION=$(VERSION) $(ROOT)/tools/build-backend.sh

docker-build: ## Build the container image
	docker buildx build \
		--platform $(PLATFORM) \
		--build-arg VERSION=$(VERSION) \
		--tag $(IMAGE):$(VERSION) \
		--tag $(IMAGE):$(GIT_SHA) \
		$(if $(filter 1,$(MAINLINE)),--tag $(IMAGE):latest,) \
		--load \
		$(ROOT)

docker-push: ## Push the image tags (registry from CI variables)
	VERSION=$(VERSION) GIT_SHA=$(GIT_SHA) $(ROOT)/tools/docker-push.sh

release-github: ## Attach dist artifacts to the current tag's GitHub Release
	gh release create $(VERSION) $(ROOT)/dist/*.tar.gz --generate-notes || \
		gh release upload $(VERSION) $(ROOT)/dist/*.tar.gz --clobber

dev: ## Run the backend and the Vite dev server together
	cd $(BACKEND) && go run ./cmd/converge & \
	cd $(FRONTEND) && $(NPM) run dev; \
	wait

clean: ## Remove build artifacts
	rm -rf $(ROOT)/dist $(FRONTEND)/dist
	find $(BACKEND)/internal/ui/dist -mindepth 1 ! -name .gitkeep -delete
```

- [ ] **Step 3: Write the Dockerfile and compose file**

`Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1

FROM node:22-alpine AS frontend
WORKDIR /src/apps/frontend
COPY apps/frontend/package.json apps/frontend/package-lock.json ./
RUN npm ci
COPY apps/frontend/ ./
# Vite writes to ../backend/internal/ui/dist, so the target must exist.
RUN mkdir -p /src/apps/backend/internal/ui/dist && npm run build

FROM golang:1.27-alpine AS backend
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src/apps/backend
RUN apk add --no-cache git
COPY apps/backend/go.mod apps/backend/go.sum ./
RUN go mod download
COPY apps/backend/ ./
COPY --from=frontend /src/apps/backend/internal/ui/dist ./internal/ui/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=${VERSION}" \
      -o /out/converge ./cmd/converge && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/jtumidanski/converge/internal/buildinfo.Version=${VERSION}" \
      -o /out/converge-cli ./cmd/converge-cli

FROM alpine:3.22
RUN apk add --no-cache git ca-certificates tini && \
    adduser -D -u 10001 converge && \
    mkdir -p /data/repositories /data/workspaces && \
    chown -R converge:converge /data
COPY --from=backend /out/converge /usr/local/bin/converge
COPY --from=backend /out/converge-cli /usr/local/bin/converge-cli
USER converge
WORKDIR /data
ENV APP_PORT=8080 \
    WORKSPACE_ROOT=/data/workspaces \
    REPOSITORY_CACHE_ROOT=/data/repositories
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${APP_PORT}/healthz" >/dev/null || exit 1
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/converge"]
```

`.dockerignore`:

```
.git
.worktrees
dist
docs
apps/frontend/node_modules
apps/frontend/dist
apps/backend/internal/ui/dist/*
!apps/backend/internal/ui/dist/.gitkeep
**/*_test.go
.env
*.env
```

`docker-compose.yml`:

```yaml
services:
  converge:
    image: ${IMAGE_REPOSITORY:-converge}:${VERSION:-latest}
    build:
      context: .
      args:
        VERSION: ${VERSION:-dev}
    env_file:
      - .env
    ports:
      - "${APP_PORT:-8080}:${APP_PORT:-8080}"
    volumes:
      - converge-repositories:/data/repositories
      - converge-workspaces:/data/workspaces
    restart: unless-stopped
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:${APP_PORT:-8080}/healthz >/dev/null || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s

volumes:
  converge-repositories:
  converge-workspaces:
```

`.env.example`:

```dotenv
# Converge configuration. Copy to .env and fill in real values.
# Every setting is read from the environment; nothing is hard-coded.

# --- Server ---
APP_PORT=8080
WORKSPACE_ROOT=/data/workspaces
REPOSITORY_CACHE_ROOT=/data/repositories
SESSION_TTL_HOURS=24
CLEANUP_INTERVAL_MINUTES=30
LOG_LEVEL=info
LOG_FORMAT=text
MAX_CONCURRENT_BUILDS=4
GIT_CLONE_TIMEOUT_MINUTES=10
GIT_COMMAND_TIMEOUT_MINUTES=2
PROVIDER_TIMEOUT_SECONDS=30

# --- Providers ---
# Pattern: PROVIDERS__<NAME>__<KEY>, where <NAME> matches [A-Z0-9_]+.
# The API exposes <NAME> lower-cased with "_" replaced by "-" (GITLAB_WORK -> gitlab-work).

# GitHub.com. BASE_URL defaults to https://api.github.com and may be omitted.
PROVIDERS__GITHUB__TYPE=github
PROVIDERS__GITHUB__TOKEN=ghp_replace_me
PROVIDERS__GITHUB__DISPLAY_NAME=GitHub

# Self-hosted GitLab. BASE_URL is the instance root; /api/v4 is appended.
PROVIDERS__GITLAB_WORK__TYPE=gitlab
PROVIDERS__GITLAB_WORK__BASE_URL=https://gitlab.example.com
PROVIDERS__GITLAB_WORK__TOKEN=glpat-replace_me
PROVIDERS__GITLAB_WORK__DISPLAY_NAME=GitLab Work
```

- [ ] **Step 4: Verify the packaging locally**

Run:

```bash
cd <worktree-root> && chmod +x tools/build-backend.sh tools/docker-push.sh
make version
make build
ls dist/*.tar.gz
make docker-build
docker run --rm -e PROVIDERS__GH__TYPE=github -e PROVIDERS__GH__TOKEN=ghp_dummy -p 18080:8080 -d --name converge-smoke $(make -s version | xargs -I{} echo converge:{})
sleep 3 && curl -fsS http://127.0.0.1:18080/healthz && echo
curl -fsS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18080/
docker rm -f converge-smoke
```

Expected: two tarballs in `dist/`; `/healthz` returns `{"status":"ok",...}` with the built version; `/` returns `200` because the image embeds the built UI. If the `sleep`-based smoke test is awkward in your shell, poll `curl` in a loop instead.

Also verify the compose path:

```bash
cd <worktree-root> && cp .env.example .env && sed -i 's/ghp_replace_me/ghp_dummy/; s/glpat-replace_me/glpat-dummy/' .env
VERSION=$(make -s version) IMAGE_REPOSITORY=converge docker compose up -d
sleep 5 && curl -fsS http://127.0.0.1:8080/healthz && echo
docker compose down -v && rm .env
```

Expected: the service reports healthy and `/healthz` answers. `.env` is git-ignored (`*.env` and `.env` patterns already present); confirm `git status --short` shows nothing before removing it.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add Makefile tools/build-backend.sh tools/docker-push.sh Dockerfile .dockerignore docker-compose.yml .env.example
git commit -m "chore(task-001): make targets, build scripts, Docker image and compose"
```

---

### Task 28: GitHub Actions and GitLab CI pipelines

**Files:**
- Create: `.github/workflows/ci.yml`, `.gitlab-ci.yml`

**Interfaces:**
- Produces:
  - GitHub Actions `ci` workflow: job `validate` on `pull_request` and `push` to `main` and `v*` tags, running `make lint test test-integration build docker-build`; job `publish` (needs `validate`, runs on `push` to `main` or a `v*` tag) logging into GHCR with `GITHUB_TOKEN`, running `make docker-build docker-push`, uploading `dist/*.tar.gz`, and running `make release-github` on tags.
  - GitLab CI stages `validate`, `build`, `publish` using the same make targets; `publish` runs only on the default branch and tags, logs into the GitLab registry with `CI_REGISTRY_USER`/`CI_REGISTRY_PASSWORD`, and exposes `dist/` as job artifacts.
  - No registry hostname or credential is committed; images resolve through `GITHUB_REPOSITORY` / `CI_REGISTRY_IMAGE` / `IMAGE_REPOSITORY` (FR-14.3).

- [ ] **Step 1: Write the GitHub Actions workflow**

`.github/workflows/ci.yml`:

```yaml
name: ci

on:
  pull_request:
  push:
    branches: [main]
    tags: ["v*"]

permissions:
  contents: write
  packages: write

concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0 # tools/version.sh needs tags

      - uses: actions/setup-go@v5
        with:
          go-version-file: apps/backend/go.mod
          cache-dependency-path: apps/backend/go.sum

      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: apps/frontend/package-lock.json

      - uses: docker/setup-buildx-action@v3

      - name: Lint
        run: make lint

      - name: Unit tests
        run: make test

      - name: Integration tests
        run: make test-integration

      - name: Build
        run: make build

      - name: Docker build
        run: make docker-build

      - name: Upload artifacts
        uses: actions/upload-artifact@v4
        with:
          name: converge-dist-${{ github.sha }}
          path: dist/*.tar.gz
          if-no-files-found: error

  publish:
    needs: validate
    if: github.event_name == 'push'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version-file: apps/backend/go.mod
          cache-dependency-path: apps/backend/go.sum

      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: apps/frontend/package-lock.json

      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build artifacts and image
        env:
          MAINLINE: "1"
        run: |
          make build
          make docker-build

      - name: Push image
        env:
          MAINLINE: "1"
        run: make docker-push

      - name: Create GitHub Release
        if: startsWith(github.ref, 'refs/tags/v')
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: make release-github
```

Note on `IMAGE`: the Makefile's `IMAGE` default does not know about GHCR, but `tools/docker-push.sh` does. So the `docker-build` step must tag with the same repository the push script uses. Set it explicitly in both publish steps:

```yaml
        env:
          MAINLINE: "1"
          IMAGE_REPOSITORY: ghcr.io/${{ github.repository }}
```

Apply that `env` block to the "Build artifacts and image" and "Push image" steps (replacing the `MAINLINE`-only blocks) so `make docker-build` and `make docker-push` agree on the tags. GHCR requires a lowercase repository path; `${{ github.repository }}` is already lowercase for lowercase owner/repo names, and `docker-push.sh` lowercases the GHCR fallback.

- [ ] **Step 2: Write the GitLab CI configuration**

`.gitlab-ci.yml`:

```yaml
stages:
  - validate
  - build
  - publish

variables:
  GIT_DEPTH: "0" # tools/version.sh needs tags
  DOCKER_DRIVER: overlay2

default:
  interruptible: true

.toolchain: &toolchain
  image: golang:1.27-bookworm
  before_script:
    - curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
    - apt-get install -y --no-install-recommends nodejs make git
    - node --version && npm --version && git --version

validate:
  <<: *toolchain
  stage: validate
  script:
    - make lint
    - make test
    - make test-integration
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_TAG

build:
  <<: *toolchain
  stage: build
  script:
    - make build
  artifacts:
    name: converge-$CI_COMMIT_SHORT_SHA
    paths:
      - dist/
    expire_in: 30 days
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_TAG

docker-build:
  stage: build
  image: docker:28-cli
  services:
    - docker:28-dind
  variables:
    DOCKER_TLS_CERTDIR: "/certs"
  before_script:
    - apk add --no-cache make git bash
  script:
    - make docker-build
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_TAG

publish:
  stage: publish
  image: docker:28-cli
  services:
    - docker:28-dind
  variables:
    DOCKER_TLS_CERTDIR: "/certs"
    MAINLINE: "1"
  before_script:
    - apk add --no-cache make git bash
    - echo "$CI_REGISTRY_PASSWORD" | docker login -u "$CI_REGISTRY_USER" --password-stdin "$CI_REGISTRY"
  script:
    - make docker-build
    - make docker-push
  dependencies:
    - build
  artifacts:
    name: converge-$CI_COMMIT_REF_SLUG
    paths:
      - dist/
    expire_in: 90 days
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_TAG
```

The `publish` job reuses `dist/` from the `build` job via `dependencies`, so no second frontend build is needed. GitLab creates no Release entry, per the design's resolved question.

- [ ] **Step 3: Validate the workflow files locally**

There is no CI runner available here, so validate syntax and the referenced targets:

```bash
cd <worktree-root>
# YAML parses
python3 -c "import yaml,sys; [yaml.safe_load(open(p)) for p in ('.github/workflows/ci.yml','.gitlab-ci.yml')]; print('yaml ok')"
# Every make target the pipelines call exists
for target in lint test test-integration build docker-build docker-push release-github; do
  make -n "$target" >/dev/null 2>&1 && echo "target $target ok" || echo "target $target MISSING"
done
```

Expected: `yaml ok` and every target reported `ok`. `make -n docker-push` will print the script invocation without running it.

If `gh` is authenticated and the repository has a remote, optionally lint the workflow with `gh workflow view` after pushing; do not push here.

- [ ] **Step 4: Commit**

```bash
cd <worktree-root> && git add .github/workflows/ci.yml .gitlab-ci.yml && git commit -m "ci(task-001): GitHub Actions and GitLab CI pipelines"
```

---

### Task 29: Documentation — README, CLAUDE.md, manual checklist

**Files:**
- Create: `docs/manual-checklist.md`
- Modify: `README.md`, `CLAUDE.md`

**Interfaces:**
- Produces: accurate build commands, a full configuration reference, deployment and CLI instructions, the documented GitHub scan bound, and the real-provider manual checklist (FR-12.4).

- [ ] **Step 1: Write `README.md`**

Replace the file's contents with:

````markdown
# Converge

Converge reconstructs the **combined net effect** of several merged GitHub Pull
Requests or GitLab Merge Requests as one synthetic diff. It answers a single
question: what is the final net code change produced by this set of PRs/MRs,
without the intermediate churn and without everyone else's unrelated commits?

It is a review aid. It never writes to your provider and never pushes a branch.

## How it works

For a selected set of merged changes, Converge:

1. resolves each change to the commit(s) that landed it on the target branch,
2. computes a base equal to the target branch immediately before the earliest
   selected change merged,
3. creates an isolated workspace at that base from a local mirror of the
   repository,
4. applies the selected changes in merge order,
5. diffs the base against the result.

Conflicts and dependencies on unselected work are reported, never guessed around.

## Requirements

- git 2.45 or newer on `PATH` (the container image ships 2.49)
- Go 1.26+ and Node 22 to build from source
- Docker to run the packaged image

## Quick start with Docker

```sh
cp .env.example .env      # fill in at least one provider token
docker compose up -d
open http://localhost:8080
```

`GET /healthz` reports the build version and whether git is available.

## Configuration

Everything is read from the environment. No provider URL, token, or path is
compiled in.

### Server settings

| Variable | Default | Meaning |
|---|---|---|
| `APP_PORT` | `8080` | HTTP listen port |
| `WORKSPACE_ROOT` | `/data/workspaces` | Per-session workspaces |
| `REPOSITORY_CACHE_ROOT` | `/data/repositories` | Mirror cache |
| `SESSION_TTL_HOURS` | `24` | Age at which sessions expire and are cleaned |
| `CLEANUP_INTERVAL_MINUTES` | `30` | Period of the stale-session sweep |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `text` | `text` or `json` |
| `MAX_CONCURRENT_BUILDS` | `4` | Concurrent review builds |
| `GIT_CLONE_TIMEOUT_MINUTES` | `10` | Timeout for clone and fetch |
| `GIT_COMMAND_TIMEOUT_MINUTES` | `2` | Timeout for every other git command |
| `PROVIDER_TIMEOUT_SECONDS` | `30` | Provider HTTP timeout |

### Providers

Named instances use `PROVIDERS__<NAME>__<KEY>`, where `<NAME>` matches
`[A-Z0-9_]+`. The API exposes `<NAME>` lower-cased with `_` replaced by `-`, so
`GITLAB_WORK` becomes `gitlab-work`.

| Key | Required | Meaning |
|---|---|---|
| `TYPE` | yes | `github` or `gitlab` |
| `BASE_URL` | GitHub: no (default `https://api.github.com`); GitLab: yes | API base URL. For GitLab this is the instance root; `/api/v4` is appended. |
| `TOKEN` | yes | Personal or project access token |
| `DISPLAY_NAME` | no | Human label; defaults to the title-cased name |

Example:

```dotenv
PROVIDERS__GITHUB__TYPE=github
PROVIDERS__GITHUB__TOKEN=ghp_xxx

PROVIDERS__GITLAB_WORK__TYPE=gitlab
PROVIDERS__GITLAB_WORK__BASE_URL=https://gitlab.example.com
PROVIDERS__GITLAB_WORK__TOKEN=glpat-xxx
PROVIDERS__GITLAB_WORK__DISPLAY_NAME=GitLab Work
```

Startup fails with a message naming the offending variable, never its value,
when a provider is misconfigured, no provider is configured, git is missing, or
a storage root cannot be created.

Tokens never appear in logs, API responses, `session.json`, git command lines,
or the mirror's remote URL.

### Security posture

The server binds to `0.0.0.0:APP_PORT` and adds no authentication. Run it on a
trusted network or behind your own reverse proxy. There is no OAuth and no
per-user login: the configured server tokens define what any visitor can see.

## Command line

`converge-cli` runs the same reconstruction pipeline as the server, with the
same environment configuration.

```sh
converge-cli build \
  --provider gitlab-work \
  --repo atlas/server \
  --base main \
  --changes 421,427,435 \
  --out ./out
```

It writes `combined.diff` and `metadata.json` into `--out` (default
`./converge-out/<session-id>`), prints the session record as JSON on stdout, and
leaves the workspace in place unless `--cleanup` is passed.

Exit codes:

| Code | Meaning |
|---|---|
| 0 | The combined diff was produced |
| 2 | A selected change conflicts |
| 3 | Validation or base-selection failure |
| 4 | Provider authentication or availability failure |
| 1 | Anything else |

## Building from source

```sh
make lint             # go vet, golangci-lint, eslint, prettier --check
make test             # go test -race, vitest
make test-integration # reconstruction tests against local git repositories
make build            # UI into the Go embed dir, then cross-compiled binaries
make docker-build     # container image
make version          # X.Y.Z on a v-tag, else 0.0.0-<short sha>
make dev              # backend plus the Vite dev server
```

Per-app commands:

```sh
cd apps/backend  && go test -race -count=1 ./... && go vet ./... && go tool golangci-lint run && CGO_ENABLED=0 go build ./...
cd apps/frontend && npm ci && npm run lint && npm run format:check && npm test && npm run build
```

Node is not always on `PATH`:

```sh
export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22
```

## Known limits

- **GitHub merged-PR listing.** The GitHub pulls endpoint has no server-side
  merged filter, so Converge pages through closed PRs and filters on
  `merged_at`, scanning at most 1,000 closed PRs per repository. Very old merged
  PRs beyond that window will not appear in the list; searching by number goes
  straight to the PR and always works.
- **Merge commits applied onto a different base.** Reconstructing a merge commit
  whose branch was long-lived can produce a conflict that the original merge did
  not have. Diagnostics name the strategy and source commit so this is
  identifiable.
- Open PRs/MRs, inline comments, approvals, and side-by-side provenance are out
  of scope.

## Manual verification

`docs/manual-checklist.md` lists the real-provider scenarios to run before a
release: GitHub squash, merge-commit, and rebase PRs; GitLab squash, merge, and
fast-forward MRs; plus a token-leak sweep.
````

- [ ] **Step 2: Write `docs/manual-checklist.md`**

```markdown
# Manual verification checklist

Run against real GitHub and GitLab repositories before a release. Automated
tests use local repositories only, so these scenarios cover the provider
contracts that fixtures cannot.

Record the result and the date for each row.

## Setup

- [ ] `.env` configures one GitHub provider and one GitLab provider with tokens
      that can read the target repositories.
- [ ] `docker compose up -d` serves the UI and `/healthz` reports the expected
      version.

## GitHub

- [ ] **Squash PR.** Select one squash-merged PR. The base is the commit before
      it landed, the diff matches the PR's own "Files changed" view.
- [ ] **Merge-commit PR.** Select one PR merged with a merge commit and more
      than one commit. The reconstruction contains the whole PR, not just the
      last commit.
- [ ] **Rebase PR.** Select one PR merged with "Rebase and merge". Diagnostics
      report strategy `rebase`, and every commit of the PR appears in the diff.
- [ ] **Several PRs with unrelated work between them.** Select two or three PRs
      that landed with other people's commits interleaved. No file touched only
      by the unrelated commits appears in the combined diff.
- [ ] **Missing commit.** Select a PR whose landing commit was removed from the
      branch (force-push or branch rewrite). The build fails with
      `MISSING_COMMITS` and names the SHA. GitHub's fetch-by-SHA behaviour is
      undocumented, so confirm the failure is clean rather than a hang.

## GitLab

- [ ] **Squash MR.** Same expectation as the GitHub squash case;
      `squash_commit_sha` is used as the landing commit.
- [ ] **Merge-commit MR.** A project with merge method "merge commit".
- [ ] **Fast-forward MR.** A project with merge method "fast-forward". Confirm
      the landing commit resolves from the candidate chain and the strategy is
      reported as `rebase` or `squash` depending on the commit count.
- [ ] **Self-hosted instance.** Repeat one scenario against a self-hosted GitLab
      to confirm `BASE_URL` handling and `/api/v4` appending.

## Errors and lifecycle

- [ ] **Not merged.** Selecting an open PR/MR fails with `NOT_MERGED` naming the
      number.
- [ ] **Incompatible targets.** Selecting changes that targeted different
      branches fails with `INCOMPATIBLE_TARGETS` listing each target.
- [ ] **Conflict.** Two selected changes that touch the same lines produce a
      `CONFLICTED` session listing the files, and the panel names the change.
- [ ] **Dependency.** A change that depends on an unselected change produces a
      conflict whose message mentions work outside the review, and the
      unselected change is absent from the applied list.
- [ ] **Finish Review.** After finishing, the session directory is gone, the
      mirror still works for a new review, and finishing twice is harmless.
- [ ] **Expiry.** With `SESSION_TTL_HOURS=1`, a session older than the TTL is
      expired by the sweep and its directory is removed.
- [ ] **Restart recovery.** Restarting the server while a build is running marks
      that session `FAILED` with code `INTERRUPTED`.

## Token-leak sweep

- [ ] `docker compose logs converge | grep -iE '<token prefix>|authorization|private-token'` finds
      no token value.
- [ ] Every API response body for a review contains no token and no
      `extraheader` string.
- [ ] Inside the container: `git -C /data/repositories/<provider>/<ns>/<name>.git remote get-url origin`
      prints a URL with no credentials.
- [ ] `cat /data/workspaces/<id>/session.json` contains no token.
```

- [ ] **Step 3: Update `CLAUDE.md`**

In the "Project Overview" section, replace the sentence about the repository being unscaffolded:

```markdown
Converge is a combined PR/MR review tool: one place to review pull requests and merge requests across hosting providers. The system is a **Go** backend plus a **React/TypeScript** web UI, laid out as `apps/backend` and `apps/frontend`. The backend is one Go module (`github.com/jtumidanski/converge`) producing two binaries, `converge` (HTTP server with the embedded UI) and `converge-cli` (reconstruction proof of concept). Persistence is the filesystem only: mirrors under `REPOSITORY_CACHE_ROOT`, review sessions under `WORKSPACE_ROOT`. All CI logic lives in the root `Makefile` and `tools/`.
```

Replace the "Build & Verification" section with:

````markdown
## Build & Verification

A branch is "done" only when all of these are clean.

Repository root (what CI runs):

- `make lint`
- `make test`
- `make test-integration`
- `make build`
- `make docker-build`

**Backend** (cwd = `apps/backend`):
- `go test -race -count=1 ./...`
- `go test -race -count=1 -tags integration ./...`
- `go vet ./...`
- `go tool golangci-lint run` (pinned via the `tool` directive; no separate install)
- `CGO_ENABLED=0 go build ./...`

**Frontend** (cwd = `apps/frontend`):
- `npm ci`
- `npm run lint`
- `npm run format:check` (`npm run format` rewrites)
- `npm test` (Vitest)
- `npm run build` (writes into `apps/backend/internal/ui/dist`)

Node is not always on `PATH` — if `npm` is missing, load it first:

```sh
export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22
```
````

Add a new section after "Code Patterns":

````markdown
## Architecture Notes

- Backend package direction: `api → review → {provider, mirror, workspace, diff, session} → gitx`. Nothing imports `api` or `cmd`; `gitx` imports nothing from the module.
- `internal/session` owns the `Session` model, `ReviewError`, error codes, and the on-disk store. `internal/review` owns landing resolution, the resolve pipeline, the applicator, and the orchestrating service.
- Every git call goes through `gitx.Runner` with an argument slice, never a shell. Client-supplied strings are validated before they reach git.
- Provider credentials reach git only through `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_0`/`GIT_CONFIG_VALUE_0`, never argv and never the stored remote URL.
- The frontend deviates from `frontend-dev-guidelines` in three agreed ways: Vitest instead of Jest, a thin `fetch` wrapper instead of a caching API client, and plain service objects instead of a `BaseService` class.
- The backend deviates from `backend-dev-guidelines` where those rules assume GORM, api2go, and logrus: `session.json` plays the entity role, `log/slog` is injected through constructors, and pipeline steps are plain functions.
````

- [ ] **Step 4: Verify the documented commands actually work**

Run each command the README claims, from a clean state:

```bash
cd <worktree-root>
make version && make lint && make test && make test-integration && make build && make docker-build
```

Expected: all succeed. Fix the docs if any command differs from reality.

- [ ] **Step 5: Commit**

```bash
cd <worktree-root> && git add README.md CLAUDE.md docs/manual-checklist.md && git commit -m "docs(task-001): README, CLAUDE.md build commands and manual checklist"
```

---

### Task 30: Full verification and code review

**Files:**
- Modify: any file needed to address review findings
- Create: `docs/tasks/task-001-combined-review-mvp/audit.md` (written by the reviewer agents)

- [ ] **Step 1: Run the complete gate from a clean tree**

```bash
cd <worktree-root>
git status --short          # must be clean before starting
make clean
make lint
make test
make test-integration
make build
make docker-build
```

Expected: every command exits 0. Capture the output; do not claim success without it.

- [ ] **Step 2: Confirm the acceptance criteria that automation can check**

```bash
cd <worktree-root>/apps/backend
# no token in any test log, session file, or remote URL — covered by tests
go test -race -count=1 -tags integration -run 'TokenLeaks|Cleanup|Concurrent' ./internal/review/
# the CLI reports its version and rejects bad input with code 3
go run ./cmd/converge-cli --version
go run ./cmd/converge-cli build; echo "exit=$?"
```

Expected: tests pass, version prints, `exit=3`.

- [ ] **Step 3: Run the code review**

Invoke `superpowers:requesting-code-review`. It dispatches `plan-adherence-reviewer` (this plan), `backend-guidelines-reviewer` (Go files changed), and `frontend-guidelines-reviewer` (TypeScript files changed) in parallel. Each writes to `docs/tasks/task-001-combined-review-mvp/audit.md`.

- [ ] **Step 4: Address findings**

For each finding rated blocking or important, either fix it or record in `audit.md` why it does not apply. Re-run the affected gate commands after each fix. Do not open a PR with unaddressed blocking findings.

- [ ] **Step 5: Commit the audit and any fixes**

```bash
cd <worktree-root> && git add docs/tasks/task-001-combined-review-mvp/audit.md && git commit -m "docs(task-001): code review audit"
```

- [ ] **Step 6: Verify the branch is ready**

```bash
cd <worktree-root>
git rev-parse --show-toplevel   # must end with .worktrees/task-001-combined-review-mvp
git branch --show-current       # must be task-001-combined-review-mvp
git status --short              # must be clean
git log --oneline main..HEAD    # every task above should appear
```

Then hand off to `superpowers:finishing-a-development-branch`.

---

## Traceability

Every PRD functional requirement maps to at least one task.

| Requirement group | Tasks |
|---|---|
| FR-1.x repository layout and tooling | 1, 21, 27, 29 |
| FR-2.x configuration | 2, 17, 27, 29 |
| FR-3.x provider layer | 4, 5, 6 |
| FR-4.x cache and workspace | 3, 8, 9 |
| FR-5.x base selection | 12, 13, 16 |
| FR-6.x applying changes | 14, 16 |
| FR-7.x combined diff | 10, 15 |
| FR-8.x session lifecycle | 11, 15, 19, 20 |
| FR-9.x cleanup | 9, 11, 15, 16 |
| FR-10.x web UI | 21, 22, 23, 24, 25, 26 |
| FR-11.x CLI | 17 |
| FR-12.x testing | every task's tests; 16 for FR-12.2; 22-26 for FR-12.3; 29 for FR-12.4 |
| FR-13.x Docker | 27 |
| FR-14.x CI/CD | 27, 28 |
| API surface §5 | 18, 19 |
| Data model §6 | 11 |
| Non-functional §8 | 3, 11, 15, 17 (security, concurrency, observability), 27 (portability), 30 (maintainability review) |

