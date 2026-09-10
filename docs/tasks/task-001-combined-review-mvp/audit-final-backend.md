# Final whole-branch backend audit — task-001-combined-review-mvp

Scope: the 104 changed Go files under `apps/backend`, audited as a whole against
the `backend-dev-guidelines` skill (DOM-* / SUB-* / SEC-*), with the priority
order given in the Task 30 brief: security, concurrency/lifecycle, layering,
error/context handling, provider seam.

Verdict: **FAIL** — 0 Critical, 3 Important, 8 Minor.
No security-critical defect was found; the three Important findings are a
production hardening regression, an unbounded attacker-influenced cache, and an
unsynchronised shutdown. None is a release-stopper on its own; together they are
enough that this does not get a clean PASS.

## Checklist applicability

`CLAUDE.md` (worktree, "Architecture Notes") states the backend deviates from
`backend-dev-guidelines` where those rules assume GORM, api2go and logrus:
`session.json` plays the entity role, `log/slog` is injected through
constructors, pipeline steps are plain functions. Verified: no package under
`internal/` has a `model.go`+`entity.go`+`resource.go` GORM/api2go domain shape,
so the mechanical DOM-01..DOM-19 and SUB-01..SUB-04 rows do not bind literally.
The intent behind them was checked against the actual shapes and is recorded
under "Guideline-intent checks" below. SEC-* was applied in full.

## Objective gate (run by me, in `apps/backend`)

| Command | Result |
|---|---|
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go tool golangci-lint run` | `0 issues.` |
| `go test -race -count=1 ./...` | all 19 packages `ok`, 0 failures |
| `go test -race -count=1 -tags integration ./internal/review/...` | `ok  internal/review  8.902s` |

No data race was reported by the detector on any package, including the
`integration`-tagged review tests.

---

## SEC-* — security review

### SEC-01 Provider tokens never reach argv — PASS

`gitx.CredentialEnv` (`apps/backend/internal/gitx/credentials.go:11-24`) is the
only construction of a git credential. It returns three `GIT_CONFIG_*` strings
and puts the base64 Basic value in `GIT_CONFIG_VALUE_0` only
(`credentials.go:20-24`). Both providers append it to `Spec.Env` and never to
`Spec.Args`: `github/client.go:179-186`, `gitlab/client.go:232-239`. The only
`exec.CommandContext` in the module is `gitx/exec.go:110`, whose argv is
`r.gitPath` plus the fixed `-c` prefix (`exec.go:105-109`) plus `s.Args`; the
env is `r.baseEnv()` + `s.Env` (`exec.go:112`). `grep -rn "exec.Command"` over
`apps/backend` returns that one production site plus `internal/testutil` and
`internal/api/health.go:19` (`exec.LookPath("git")`, no args).

### SEC-02 Tokens never reach a stored remote URL — PASS

`CloneURL` returns `repo.CloneURL()` verbatim on both providers
(`github/client.go:176`, `gitlab/client.go:230`); that value comes from the
provider API response (`github/mapping.go:29`, `gitlab/mapping.go:25`) and is
never rewritten to embed userinfo. `mirror.Cache.Ensure` passes it to
`clone --mirror` unmodified (`mirror/cache.go:74`) and no code path runs
`git remote set-url`. Asserted by `gitlab/client_test.go:310-316`
("CloneURL must never carry credentials") and `review/integration_test.go:390`.

### SEC-03 Tokens never reach logs — PASS

Three independent layers, all verified:
- `config.Secret` has `String()`, `MarshalJSON()` and `LogValue()` all returning
  `[redacted]` (`config/secret.go:19-25`); only `Reveal()` exposes it and
  `grep -rn "\.Reveal()"` shows exactly four production call sites, all of them
  an HTTP header or `CredentialEnv` (`github/client.go:79,180`,
  `gitlab/client.go:58,233`).
- `app.redactingHandler` scrubs every configured token out of the message and,
  recursively, out of every attribute and group (`app/app.go:60-108`), and is
  installed unconditionally in `NewLogger` (`app/app.go:134`).
- `gitx.Redact` scrubs `Authorization`/`Private-Token` header values and the
  configured secrets from the only place stderr is ever logged
  (`gitx/redact.go:8-21`, applied at `gitx/exec.go:165`).
`loggableURL` (`app/app.go:116-123`) additionally strips userinfo from a
provider `BASE_URL` before it is logged at `app/app.go:212`, closing the gap
that `Config.Secrets()` (tokens only, `config/config.go:68-76`) leaves.

### SEC-04 Tokens never reach session files on disk — PASS

`session.Record` (`session/record.go:27-45`) has no credential field, and
`ToRecord` (`record.go:62-95`) copies only those fields. `writeRecord`
marshals `ToRecord(sess)` and nothing else (`session/store.go:145`).

### SEC-05 No token echo in HTTP error responses — PASS

`api.classify` (`api/errors.go:31-65`) returns either a fixed literal string or
a `session.ReviewError.Message` / `review.InputError.Message`, all of which are
built from `internal/review/messages.go` templates over change numbers and
branch names. The default branch is a constant
(`api/errors.go:160`). `provider.StatusError` deliberately records no headers
and no body (`provider/errors.go:16-51`) and `DoJSON` discards a non-2xx body
into `io.Discard` (`provider/httpjson.go:20`). Raw git stderr never leaves
`gitx`: `ExitError.Error()` formats category and exit code only
(`gitx/spec.go:50-52`).

### SEC-06 Client-supplied strings reaching git are validated — PASS

- session id: `workspace.ValidateSessionID` `^[0-9a-f]{8}$` (`workspace/manager.go:19-27`),
  enforced at the HTTP edge (`api/reviews.go:121,151`) and again in
  `Manager.guard` (`manager.go:67-86`) which also `EvalSymlinks`-checks the
  resolved dir is a direct child of the canonical root (`manager.go:82`).
- repository: `gitx.ValidateRepoFullName` rejects leading `/ - .`, empty and
  `..` segments (`gitx/validate.go:32-45`), applied at `api/repositories.go:57`
  and again in both clients (`github/client.go:124`, `gitlab/client.go:86`).
- branch: `ValidateBranchSyntax` plus a real `git check-ref-format --branch`
  round trip (`gitx/validate.go:48-64`), used by `review/input.go:46`.
- SHAs: `^[0-9a-f]{40}$` at every boundary — `mirror/cache.go:91`,
  `workspace/manager.go:95`, `review/apply.go:82`, `diff/diff.go:52-57`,
  `review/service.go:528`, and on the way back in from disk at
  `session/record.go:127,133`.
- file path: `getReviewFile`'s `{path...}` is not passed to git as given — it is
  matched against the stored `sess.Files()` whitelist first
  (`review/service.go:555-564`) and only the matching `FileSummary` is used;
  `diff.FileContent` then re-checks with `ValidatePathArg` and puts it after
  `--` (`diff/diff.go:147-156`).

### SEC-07 Session ids are CSPRNG — PASS

`crypto/rand` (`session/id.go:11-15`).

### SEC-08 No hardcoded secrets — PASS

Every credential arrives via `PROVIDERS__<NAME>__TOKEN` and is required
(`config/config.go:226-229`). No literal token/key in any non-test file.

### Accepted, documented posture (not a finding)

`cmd/converge/main.go:86` binds `0.0.0.0` with no authentication, no CORS and no
CSRF token. This is explicitly the product decision, not an oversight:
`README.md:100-101` ("The server binds to `0.0.0.0:APP_PORT` and adds no
authentication. Run it on a trusted network or behind your own reverse proxy")
and `prd.md:683`. Recorded as verified-accepted.

---

## Important findings

### I-1 — `protocol.file.allow=always` is set on every production git invocation

`apps/backend/internal/gitx/exec.go:105-109`

```go
args := append([]string{
    "-c", "core.hooksPath=" + r.hooksDir,
    "-c", "commit.gpgsign=false",
    "-c", "protocol.file.allow=always",
}, s.Args...)
```

`ExecRunner` is the production runner (wired at `app/app.go:176`). This flag
re-enables the `file://` transport that git disables by default as the fix for
CVE-2022-39253. It is here because the test corpus clones `file://` remotes:
`testutil.Repo.CloneURL()` returns `"file://" + r.Bare`
(`internal/testutil/repo.go:142`) and `mirror/cache_test.go:100`,
`objects_test.go:29`, `workspace/manager_test.go:36`, `review/harness_test.go:45`
and `review/service_test.go:173` all clone through the real `ExecRunner`.
`internal/testutil/repo.go:57` already sets the same flag independently for
testutil's own git calls, so the production runner is carrying a test-only need.

Residual exploitability today is low — `CredentialEnv` rejects any clone URL
that is not http(s) (`credentials.go:12-15`) and both providers return its error
(`github/client.go:181`, `gitlab/client.go:234`), so a malicious `clone_url` in a
provider API response cannot reach git; and `clone --mirror` /`worktree add` do
not fetch submodules. But that http(s) restriction is a side effect of building
a credential, not an explicit policy check, and the flag removes the defence in
depth behind it. The flag belongs on the test runner, not on the runner
`app.New` hands to the service.

### I-2 — Unbounded, never-evicted GitHub scan cache keyed on a client-supplied string

`apps/backend/internal/provider/github/list.go:41-49`

```go
key := c.scanKey(repo, target)
c.mu.Lock()
defer c.mu.Unlock()
entry := c.scans[key]
if entry == nil || c.now().Sub(entry.fetchedAt) > ScanCacheTTL {
    entry = &scanEntry{nextPage: 1, fetchedAt: c.now()}
    c.scans[key] = entry
}
```

`c.scans` is written here and read at `list.go:45`; nothing ever deletes from it
(`grep -n "delete(c.scans"` → no match) and there is no size cap. The key is
`repo.FullName() + "\x00" + target` (`list.go:24-26`), and `target` is the raw
`?target=` query parameter (`api/changes.go:66-71`), constrained only by
`ValidateBranchSyntax` — up to 255 characters of essentially free-form text
(`gitx/validate.go:48-53`). The branch need not exist. A caller iterating
`?target=` therefore grows the map without bound for the process lifetime, and
each distinct key also costs up to `MaxScanPages`=10 upstream GitHub API calls
against the operator's shared token (`list.go:50-83`), i.e. rate-limit
amplification. `ScanCacheTTL` refreshes an entry's contents but never removes
the key.

Secondary point for the provider-seam priority: the GitLab client has no
equivalent cache at all — it paginates server-side
(`gitlab/client.go:132-178`). The `HasNext` semantics consequently differ
(`list.go:133` derives it from the local scan state and cap; `gitlab/client.go:139`
from the `X-Next-Page` header). That divergence is inherent to the two upstream
APIs and is contained behind `provider.Slice`, so it is not itself a finding —
but it does mean the memory characteristic of the seam is provider-specific and
only one side is bounded.

### I-3 — Shutdown does not drain background goroutines before the git runner's shared directories are deleted

`apps/backend/cmd/converge/main.go:77-122` and `apps/backend/internal/gitx/exec.go:66`

`serve` defers `application.Close()` (`main.go:82`), which is
`ExecRunner.Close()` → `os.RemoveAll(filepath.Dir(r.homeDir))`
(`exec.go:66`), removing the private `HOME` and `core.hooksPath` directories
that every git invocation is configured against (`exec.go:80,106`).

`srv.Shutdown` (`main.go:114`) drains in-flight *HTTP handlers* only. Two classes
of goroutine outlive it and keep calling `runner.Run`:
- build goroutines from `Service.StartBuild` (`review/service.go:159-176`) — the
  HTTP handler returns 202 immediately (`api/reviews.go:98-99`) so the build is
  by construction still running after Shutdown returns;
- the session sweeper started at `api/router.go:56`, whose `Sweep` →
  `expire`/`retryCleanup` → `Cleaner.Cleanup` → `workspace.Manager.Cleanup`
  path shells out to git (`workspace/manager.go:143-166`).

Both are cancelled by the shared context, but cancellation is never *awaited*:
`grep -rn "WaitGroup\|Wait()"` over `internal/review/service.go`,
`internal/app/app.go`, `cmd/converge/main.go` and `internal/api/router.go`
returns nothing. `ExecRunner` also sets `cmd.WaitDelay = 2 * time.Second`
(`exec.go:115`), so a git child can still be alive for up to two seconds after
its context dies — during which `Close()` may already have deleted its `HOME`
and hooks path. The practical blast radius is small (git tolerates a missing
`HOME`/`hooksPath`, and the recovery path records such sessions as
`INTERRUPTED` at `session/store.go:376-381`), but the lifecycle is genuinely
unsynchronised: the owner of the directories tears them down while known
users are still running.

---

## Minor findings

- **M-1** `session/store.go:228-232` — `Finish` short-circuits on
  `StatusFinished` only, not `StatusExpired`. An already-EXPIRED session runs a
  redundant `Cleanup` (and, on failure, gets re-registered in `pendingCleanup`
  at `store.go:272-281`). Harmless because `Session.Finished` is itself
  terminal-guarded (`session/model.go:267-274`) and `workspace.Cleanup` is
  idempotent (`workspace/manager.go:169-171`), but the guard is asymmetric with
  its own docstring.
- **M-2** `session/store.go:229-236` — `Finish` reads via `s.Get(id)` and then
  takes the lock separately to write the index, so two concurrent
  `DELETE /api/reviews/{id}` calls both pass the check and both invoke
  `Cleanup`. Benign for the same idempotency reason as M-1, and every *write*
  path is correctly CAS-guarded through `SaveActive`
  (`store.go:115-132`) — this is the one mutator that is not.
- **M-3** Sibling-package coupling not in the documented direction. `CLAUDE.md`
  declares `review → {provider, mirror, workspace, diff, session} → gitx`, but
  `internal/session` imports `internal/workspace` (`session/store.go:14`,
  `session/builder.go:8`) solely for `ValidateSessionID`, and
  `internal/mirror` imports `internal/provider` (`mirror/cache.go:14`). Both are
  same-level edges the stated direction does not sanction. `ValidateSessionID`
  is a pure regex (`workspace/manager.go:19-27`) and would sit naturally in
  `gitx` alongside the other validators.
- **M-4** `api/reviews.go:45` serialises `session.ReviewError` — including
  `Diagnostics.WorkspacePath` (`session/errors.go:16`) — to every API client.
  `finishWithError` populates it with the absolute host path
  (`review/service.go:335`), as does the conflict path
  (`review/service.go:454`). Absolute server filesystem paths are disclosed to
  any caller. Deliberate per the "UI shows them only under Diagnostics" comment,
  but worth an explicit ruling given the no-auth posture.
- **M-5** `jsonapi/decode.go:58` echoes the decoder's own `err.Error()` into the
  response detail, leaking Go field and type names for a malformed body. Not
  XSS (the response is `application/vnd.api+json`), but it is the one place a
  client sees an internal error string verbatim; everywhere else uses a fixed
  literal.
- **M-6** `api/ui.go:39-65` sets `Content-Type` and `Cache-Control` on the
  embedded SPA responses but no `X-Content-Type-Options: nosniff` and no CSP.
- **M-7** `gitx/exec.go:137-141` documents an intentional goroutine leak if a
  caller's `Spec.Stdin` never unblocks. The reasoning (avoiding a wedged
  `cmd.Wait`) is sound and the only production caller passes a
  `bytes.NewReader` (`review/apply.go:208`), so it cannot trigger today —
  recorded so it is not rediscovered as a leak.
- **M-8** `provider/registry.go:16-22` mutates `r.byID` without a lock while
  `Get`/`All` read it. Safe as written — the only `Register` caller is
  `app.New`'s startup loop (`app/app.go:208`), which completes before the router
  exists — but the type carries no comment saying registration is startup-only.

## Guideline-intent checks (mapped from DOM-*/SUB-* to the actual architecture)

| Intent | Status | Evidence |
|---|---|---|
| Immutable model with accessors | PASS | `session/model.go` — every transition returns a new `Session` (`Finished` 267, `Expired` 278, `Ready`, `Failed`, `Conflicted`); `ReviewError.clone()` deep-copies so a caller cannot mutate a session's error (`session/errors.go:37-49`) |
| Fluent builder enforcing invariants | PASS | `session/builder.go`, `provider/builder.go`; `Build()` validated — proven by `FromRecord` going through it (`session/record.go:103-107`) |
| Entity/DTO round trip (`ToEntity`/`Make` equivalent) | PASS | `ToRecord` (`session/record.go:62`) / `FromRecord` (`record.go:98`), with enum and SHA revalidation on the way in (`record.go:108-136`) |
| Transport DTO transform, no inline mapping in handlers | PASS | `reviewResource` (`api/reviews.go:66`), `repositoryResource` (`api/repositories.go:19`), `changeResource` (`api/changes.go:31`), `fileAttrs` (`api/review_files.go:26`); list handlers loop over the single-item transform rather than duplicating it |
| Logger injected, never package-global | PASS | `*slog.Logger` through every constructor; nil is replaced with `slog.DiscardHandler`, never `slog.Default()` — `review/service.go:61-64`, `review/cleaner.go:24-26`, `review/apply.go:62-66` |
| POST body decoded through a typed helper, no manual envelope parsing in handlers | PASS | `jsonapi.Decode[createReviewAttributes]` (`api/reviews.go:86`); `DisallowUnknownFields` and a 1 MiB `LimitReader` (`decode.go:33-55`). `grep` finds no `json.NewDecoder`/`io.ReadAll` in any `api/*.go` handler |
| Flat request attributes, no nested Data/Type/Attributes in the request struct | PASS | `createReviewAttributes` (`api/reviews.go:17-22`) |
| Handlers are thin; orchestration in the service layer | PASS | Every handler resolves inputs then delegates to `review.Service` or `provider.GitProvider`; no handler calls `gitx`, `mirror` or `workspace` for state changes. The one direct filesystem call, `os.Open` at `api/review_files.go:91`, takes a path the service produced and validated (`review/service.go:568-581`) |
| Transform/serialisation errors checked | PASS | Every `jsonapi.Write*` return is checked and logged — `api/reviews.go:99,110,143`, `review_files.go:49,75`, `repositories.go:79,99`, `changes.go:81`, `providers.go:28`. No `_ =` discard on a success-path write |
| Domain error → HTTP status mapping | PASS | `api/errors.go:31-65`: input/decode → 400, not-found → 404, not-ready → 409, provider auth → 502, unavailable/deadline → 503, default → 500 |
| No `os.Getenv` outside config | PASS | `grep -rn "os.Getenv"` → only `gitx/exec.go:79` and `testutil/repo.go:44`, both forwarding `PATH` into a deliberately minimal child env; all real configuration flows through `config.Load(env)` (`config/config.go:79`) |
| Table-driven tests | PASS | e.g. `session/store_test.go`, `config/config_test.go`, `gitx/validate_test.go`, `review/resolve_test.go` |
| Context propagated and honoured | PASS | `ctx` is the first parameter on every I/O function; `gitx.Run` derives a per-category timeout (`exec.go:103`); `StartBuild` waits for its semaphore slot *selectably* so a queued build drains on shutdown instead of starting doomed (`review/service.go:165-170`); `firstRealError` distinguishes caller cancellation from fail-fast fallout (`review/resolve.go:285-297`) |
| Resource cleanup | PASS | Response bodies closed (`provider/httpjson.go:18`); `os.Open` deferred-closed (`api/review_files.go:96`); temp files removed on every path (`session/store.go:154`, `diff/diff.go:72`); half-clones removed (`mirror/cache.go:80-82`). The one gap is I-3 (goroutines, not handles) |

## Deferred-minor ledger — backend triage

Only item 5 is backend. **Not fix-before-merge.** `make build` cross-compiling
the two binaries rather than running full-repo `go build ./...` is adequately
covered: I ran `go build ./...` and `go vet ./...` over the whole module
directly, both clean, and `golangci-lint` reports `0 issues.` across every
package including ones no binary imports. Items 1-4 and 6-8 are
frontend/CI/docs and are outside this reviewer's scope.

## What must change for a PASS

1. I-1 — move `protocol.file.allow=always` off the production `ExecRunner` onto
   a test-only runner option (`gitx/exec.go:108`).
2. I-2 — bound `Client.scans`: cap the entry count or evict on TTL rather than
   only refreshing (`provider/github/list.go:41-49`).
3. I-3 — track in-flight builds and the sweeper on a `sync.WaitGroup` and wait
   for them in `serve` before `application.Close()` runs
   (`cmd/converge/main.go:82`, `review/service.go:159`).

The Minor items are judgement calls; M-3 and M-4 in particular deserve an
explicit ruling rather than a silent fix.
