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

- git 2.45 or newer on `PATH` (the packaged container image ships alpine
  3.22's git, 2.49.1)
- Go 1.26+ and Node 22 to build from source
- Docker to run the packaged image
- `make dev` specifically requires bash 5.1+ (it uses `wait -n` on two PIDs).
  macOS ships `/bin/bash` 3.2; install a newer bash (e.g. via Homebrew) and
  make sure it is first on `PATH`, or run the backend and `npm run dev`
  in two terminals instead.

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

## Modes

Converge runs in one of two mutually exclusive modes, fixed for the process
lifetime by `CONVERGE_MODE`.

### `standalone` (default)

No accounts, no database, one set of server-wide providers from
`PROVIDERS__*`. This is everything described above and it is unchanged: an
existing deployment that upgrades and sets no new environment variable
behaves exactly as before, and no database file is ever created or opened.

### `hosted`

Adds per-user accounts, a login session with an HttpOnly cookie, and per-user
provider configuration stored encrypted in a small SQLite database.
`PROVIDERS__*` is ignored in this mode (a single startup `WARN` names the
ignored variables, never their values) — each user configures their own
providers after registering.

The minimum to turn it on is two variables: a mode flag and a master
encryption key.

```sh
CONVERGE_MODE=hosted
CONVERGE_SECRET_KEY="$(head -c 32 /dev/urandom | base64)"
```

`CONVERGE_SECRET_KEY` must be 32 random bytes, base64 standard encoding.
Generate it once and keep it: it encrypts every stored provider token, it is
not recoverable from the database, and losing it makes every stored token
unreadable, requiring every user to re-enter theirs.

| Variable | Default | Meaning |
|---|---|---|
| `CONVERGE_MODE` | `standalone` | `standalone` or `hosted` |
| `CONVERGE_DATABASE_PATH` | `/data/converge.db` | SQLite file holding users, login sessions, per-user provider configuration, and lockout counters |
| `CONVERGE_SECRET_KEY` | *(required in hosted mode)* | 32 random bytes, base64 standard encoding; encrypts stored provider tokens |
| `CONVERGE_SECURE_COOKIES` | `false` | Mark the session cookie `Secure`; set `true` when TLS terminates at a proxy in front of Converge |
| `CONVERGE_TRUSTED_PROXY` | `false` | Honour `X-Forwarded-For` for the per-IP login throttle; set `true` **only** when a proxy genuinely sits in front — otherwise any client can forge a fresh `X-Forwarded-For` value and sidestep the per-IP lockout entirely |
| `LOGIN_SESSION_TTL_HOURS` | `720` | Absolute login session lifetime |
| `LOGIN_SESSION_IDLE_HOURS` | `168` | Login session lifetime since last use |

**Registration is open.** Anyone who can reach `POST /api/auth/register` can
create an account — there is no invite flow, no approval step, and no
administrator role. A hosted instance reachable from the internet should sit
behind a VPN or reverse-proxy authentication; Converge's own login is not a
substitute for controlling who can reach the server at all.

See `docs/hosted-mode.md` for the operational gaps hosted mode deliberately
leaves open (password reset, account removal, key rotation, per-user mirror
disk use) and how to work around each.

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
make build             # UI into the Go embed dir, then cross-compiled binaries
make docker-build      # container image
make version           # X.Y.Z on a v-tag, else 0.0.0-<short sha>
make dev               # backend plus the Vite dev server
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
  `merged_at`, scanning at most 1,000 closed PRs per repository (10 pages of
  100). Very old merged PRs beyond that window will not appear in the list;
  searching by number goes straight to the PR and always works.
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
