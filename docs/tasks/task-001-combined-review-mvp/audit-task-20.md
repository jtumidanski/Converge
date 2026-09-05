# Audit — Task 20: server binary (`apps/backend/cmd/converge/`)

Commit under review: `82daba5` (range `113b55c..82daba5`, 3 files, 268 insertions).
All measurements below were taken in a scratch copy (`/tmp/rev20`, since deleted). The worktree
was never mutated; `git status --porcelain` and `git diff HEAD -- apps/backend` were both empty at
the end of the run.

## Verdicts

1. **Spec compliance: ✅ COMPLIANT**
2. **Task quality: NEEDS-WORK** — 0 Critical, 4 Important, 3 Minor

---

## Gate (cwd `apps/backend`, all with `timeout: 600000`)

| Command | Result |
|---|---|
| `go test -race -count=1 ./...` | **PASS** — 19 packages `ok`, 1 `[no test files]` (`internal/provider/fake`) |
| `go test -race -count=1 -tags integration ./...` | **PASS** — all `ok` |
| `go vet ./...` | **PASS** — no output |
| `go tool golangci-lint run` | **PASS** — `0 issues.` |
| `CGO_ENABLED=0 go build ./...` | **PASS** — no output |

The implementer's reported gate output is accurate.

---

## Centrepiece 1 — the `"GET /"` ServeMux panic

### The panic claim: **PROVEN, exactly as described.**

Reverted `apps/backend/internal/api/router.go:90` in the scratch copy and called `NewRouter` with a
non-nil `Deps.UI`.

Grep proof the mutation landed:

```
90:		mux.Handle("GET /", uiHandler(d.UI, d.UIPresent))
```

Verbatim panic:

```
pattern "GET /" (registered at /tmp/rev20/internal/api/router.go:90) conflicts with
pattern "/api/" (registered at /tmp/rev20/internal/api/router.go:76):
	GET / matches fewer methods than /api/, but has a more general path pattern
```

This is a registration-time panic inside `net/http.(*ServeMux).register`, raised from
`api.NewRouter` on `serve()`'s first call. The implementer's diagnosis — neither pattern dominates,
one being more method-specific and the other more path-specific — is Go's own wording. The fix is
necessary; the binary cannot boot without it. **R35 is correctly applied and the diagnosis is
sound.**

With the committed `mux.Handle("/", ...)`, `NewRouter` with a non-nil `UI` registers and returns
without panicking.

### The blast radius: **CONFIRMED as a real regression.** (Important, I-1)

Measured through the composed router with a `fstest.MapFS` containing `index.html` (i.e. what
production looks like once Phase F builds the frontend):

| Request | Result with committed code |
|---|---|
| `GET /nonexistent` | 200 `text/html` — `index.html` (intended SPA fallback) |
| **`POST /nonexistent`** | **200 `text/html` — `index.html`** |
| **`PUT /nonexistent`** | **200 `text/html` — `index.html`** |
| **`DELETE /nonexistent`** | **200 `text/html` — `index.html`** |
| `POST /` | 200 `text/html` — `index.html` |
| **`POST /healthz`** | **200 `text/html` — `index.html`** |
| `GET /api/bogus` | 404 `application/vnd.api+json`, `"code":"NOT_FOUND"` ✔ |
| `POST /api/bogus` | 404 `application/vnd.api+json`, `"code":"NOT_FOUND"` ✔ |
| `GET /healthz` | 200 `application/json` ✔ |

Pre-Task-20 baseline, measured with `Deps.UI == nil` (which is what every caller and every test did
before this commit):

| Request | Pre-Task-20 |
|---|---|
| `GET /nonexistent` | 404 `404 page not found` |
| `POST /nonexistent` | 404 `404 page not found` |
| `POST /healthz` | **405 `Method Not Allowed`** |

So the change trades a registration panic for a silent loss of method restriction across the whole
non-`/api/` namespace:

- `POST`/`PUT`/`DELETE` to any unknown path now returns **200 with an HTML page**, where it
  previously returned 404.
- `POST /healthz` — a route that *is* registered, `GET`-only — now returns **200 HTML instead of
  405**. This is the sharper half of the finding: `"/"` shadows the method-not-allowed response
  that `ServeMux` would otherwise generate for every method-restricted route in the tree.

The `/api/` namespace is unaffected: `/api/` is the longer literal prefix and wins on path
specificity, so the JSON:API `NOT_FOUND` contract still holds for `POST /api/bogus`
(`internal/api/router.go:77`). No `INTERNAL` code appears anywhere in the tree (only three comments
asserting its absence, e.g. `internal/api/errors.go:18`).

**This is latent today and live next phase.** `internal/ui/dist` currently contains only
`.gitkeep`, so `ui.Present()` is false and every method gets the 503 build-hint stub — I confirmed
this against the real binary (`GET/POST/PUT/DELETE /nonexistent` → 503 `text/plain`). The moment
Phase F populates `dist/index.html`, the table above becomes production behaviour.

Remedy (not a new rule, just closing the regression the fix opened): keep the registration at
`"/"`, and have `uiHandler` reject anything other than `GET`/`HEAD` before falling back to
`index.html`. That restores method discipline without reintroducing the pattern conflict.

---

## Centrepiece 2 — reachability before protection

Objective data, `go test -coverprofile` on `./cmd/converge/`:

```
cmd/converge/main.go:34  buildDeps   100.0%
cmd/converge/main.go:47  main          0.0%
cmd/converge/main.go:69  serve        84.8%
```

Uncovered statement blocks in `main.go` (from the raw profile, count `0`):

| Lines | Branch | Reached by any test? |
|---|---|---|
| `main.go:48-53` | **all of `main()`** — `signal.NotifyContext` wiring, the stderr print, `os.Exit(1)` | **NO** |
| `main.go:72-73` | `return err` when `app.New` fails | **NO** |
| `main.go:90-92` | `errc <- err; return` — genuine `ListenAndServe` failure | **NO** |
| `main.go:101` | `return err` in the `case err := <-errc` arm — startup-failure propagation | **NO** |
| `main.go:109-110` | `return fmt.Errorf("shutdown: %w", err)` — failed/timed-out drain | **NO** |

Reached: `buildDeps` (all fields), the happy `ListenAndServe` path, the `ctx.Done()` →
`srv.Shutdown` → `return <-errc` path, and `router.go:90`.

**Residual hole of the same class that hid the panic (Important, I-4):** even after this task,
`grep -rn 'UI:\|UIPresent' --include='*_test.go'` over `apps/backend` returns **zero matches**. No
test anywhere constructs a `Deps` with `UIPresent: true`. `uiHandler` is exercised only in
isolation (`internal/api/ui_test.go:10`, `:39`), never composed with the mux; and `cmd/converge`
exercises the composition only with the real `ui.FS()`, where `Present()` is false, so only the 503
stub branch runs. The "UI actually serves `index.html` through the router" path — the one whose
behaviour I had to measure by hand above — is still unreached by the committed suite. That is
precisely why the `"GET /"` conflict survived Task 19's 39-mutation review.

I verified this asymmetry directly: with the `"GET /"` mutation applied, **`internal/api`'s own
tests still pass**; only `cmd/converge/main_test.go:37` catches it, via the panic.

---

## Centrepiece 3 — sweeper count (R34)

Measured by dumping all goroutine stacks (`runtime.Stack(buf, true)`) and counting
`session.(*Store).RunSweeper` frames, under `-race`, polling to a deadline (no fixed sleeps):

```
SWEEPER GOROUTINES WHILE LIVE      = 1
SWEEPER GOROUTINES AFTER SHUTDOWN  = 0
```

- **Exactly one** sweeper runs. `serve()` does not start a second — confirmed by inspection
  (`main.go:69-115` contains no `RunSweeper` call) and by measurement. **R34 satisfied.**
- **No leak.** The goroutine is gone after `serve()` returns, because `buildDeps` passes the
  server's lifetime `ctx` as `BuildContext` (`main.go:41`), which `router.go:55` hands to
  `RunSweeper`, whose `select` returns on `ctx.Done()` (`internal/session/sweep.go:43-47`). Not a
  Critical finding.
- **Configured interval, not a default.** Full literal chain, no default injected anywhere:
  `internal/config/config.go:97` (`CLEANUP_INTERVAL_MINUTES`, default 30) → `Config.CleanupInterval`
  (`config.go:47`) → `main.go:43` → `router.go:55` → `time.NewTicker(interval)`
  (`sweep.go:41`). Both ends of that chain are mutation-protected: M1 (below) and M8 (below).
- `internal/session/store.go` is **untouched** by this commit
  (`git diff 113b55c..82daba5 -- .../session/store.go` is empty).

---

## Mutation battery — re-run independently

All applied to a fresh scratch copy, grep-proofed on the file actually compiled, run with the
project's own `go test -race -count=1 ./...`, then reverted and diffed back to byte-identical.
(`internal/gitx TestExecRunnerTimeout` fails deterministically in any scratch copy outside a git
repo — a pre-existing environmental dependency, not Task 20's code, and it is filtered from the
results below.)

| # | Mutation | File | Grep proof (from the compiled file) | Result |
|---|---|---|---|---|
| M1 | `CleanupInterval: 0` | `cmd/converge/main.go` | `43:  CleanupInterval: 0,` | **CAUGHT** — `TestBuildDepsWires...`: "Deps.CleanupInterval = 0s, want 7m0s" |
| M2 | `Store: nil` | `cmd/converge/main.go` | `42:  Store:           nil,` | **CAUGHT** — "Deps.Store = <nil>, want the application's store" |
| M3 | `BuildContext: context.Background()` | `cmd/converge/main.go` | `41:  BuildContext:    context.Background(),` | **CAUGHT** — "want the server's lifetime ctx …WithCancel" |
| M4 | Revert router fix to `"GET /"` | `internal/api/router.go` | `90:  mux.Handle("GET /", uiHandler(d.UI, d.UIPresent))` | **CAUGHT** — panic in `cmd/converge`. NOT caught by `internal/api`'s own tests. |
| M5 | Swallow **all** `ListenAndServe` errors (`!errors.Is(err, err)`, always false) | `cmd/converge/main.go` | `89:  if err := srv.ListenAndServe(); err != nil && !errors.Is(err, err) {` | **SURVIVED** |
| M6 | Treat `ErrServerClosed` as a failure (`!errors.Is(err, context.Canceled)`) | `cmd/converge/main.go` | `89:  … !errors.Is(err, context.Canceled) {` | **CAUGHT** — "serve returned http: Server closed" |
| M7 | Skip `srv.Shutdown` entirely, `return nil` | `cmd/converge/main.go` | `107:  return nil // MUTANT_M7_SKIP_SHUTDOWN` | **CAUGHT** — "server is still accepting connections after serve() returned" |
| M8 | Router hardcodes `30*time.Minute` instead of `d.CleanupInterval` | `internal/api/router.go` | `55:  go d.Store.RunSweeper(buildCtx, 30*time.Minute) // MUTANT_M8_HARDCODED` | **CAUGHT** — `TestNewRouterStartsAndStopsSweeper` (`internal/api/api_test.go:448`) |
| M10 | Start a **second** sweeper in `serve()` (violate R34) | `cmd/converge/main.go` | `76:  go application.Store.RunSweeper(ctx, …) // MUTANT_M10_SECOND_SWEEPER` | **SURVIVED** |

Note on the implementer's M5/M6: as originally written (`if err != nil {`), both fail to compile
(`"errors" imported and not used`), so a build failure would masquerade as a caught mutation. I
re-formulated both to keep `errors` in use; M6 is genuinely caught, M5 genuinely survives.

**Verified: the added assertion is load-bearing.** I stripped `main_test.go:80-87` (the
"port actually released" block) to recover the brief's verbatim test, re-applied M7 with grep proof
(`107: return nil // MUTANT_M7_SKIP_SHUTDOWN`), and the suite reported
`ok github.com/jtumidanski/converge/cmd/converge`. The brief's own test **does** pass a server that
never calls `Shutdown`. The implementer's claim is true and its added assertion is a real
improvement — this is the brief's 10th defective sample.

**Score, reproduced: 7 of 9 mutations caught.** The two survivors are M5 and M10, below.

---

## Findings

### Critical — none.

### Important

**I-1 — Method restriction lost across the whole non-`/api/` namespace.**
`internal/api/router.go:90`. `POST`/`PUT`/`DELETE` to any unknown path returns 200 + `index.html`
(was 404), and `POST /healthz` returns 200 + `index.html` (was 405). Latent only because
`internal/ui/dist` is empty; becomes live the moment Phase F builds the frontend. Measured, table
above. Fix: gate `uiHandler` on `GET`/`HEAD`.

**I-2 — `main_test.go:67` is a time-bomb that Phase F will trip.**
The test asserts the UI endpoint returns `503`. `.gitignore:45-46` ignores
`apps/backend/internal/ui/dist/*` except `.gitkeep`, so any developer or CI job that builds the
frontend before running `go test` populates `dist/index.html`, `ui.Present()` flips to true, and
this assertion fails with "ui = 200, want 503 before the frontend is built". The assertion is
inherited verbatim from the brief, but it encodes a transient property of an unbuilt checkout as a
permanent invariant. It should assert on both branches, or key off `ui.Present()`.

**I-3 — `serve()`'s entire error-propagation surface is unreached, and M5 survives.**
`main.go:90-92` and `main.go:101` are 0% covered, and mutation M5 — swallowing *every*
`ListenAndServe` error, including a genuine bind failure, and returning `nil` — passes the full
suite. This is exactly the recurring defect class (a real failure returned as a plausible nil). The
code is correct by inspection: `errc <- err` at `main.go:90` reaches `return err` at `main.go:101`,
which reaches `os.Exit(1)` at `main.go:52`. But nothing protects it. See "Confirmed gap" below —
the implementer disclosed this accurately and chose it over a flaky test; I am recording it as a
finding because an unreached, unprotected error path is a finding regardless of the honesty of the
disclosure. It is testable without flakiness by holding the listener open and asserting `serve()`
returns a non-nil error.

**I-4 — The Task-19 coverage hole is only half closed.**
Zero test files anywhere set `Deps.UI`/`Deps.UIPresent` (grep returns no matches). `cmd/converge`
now reaches `router.go:90`, but only through `ui.Present() == false`, i.e. the 503 stub. The
composed "router serves a real `index.html`" path — the branch whose behaviour I-1 is about — is
still exercised by no test. Confirmed by running M4 against `internal/api` alone: its own tests
still pass.

### Minor

**M-1 — R34 is unenforced by any test.** Mutation M10 (a second `RunSweeper` in `serve()`) survives
the entire suite. Behaviour today is correct — I measured exactly one goroutine — but the ruling has
no regression guard. A goroutine-count assertion in `cmd/converge` would close it.

**M-2 — `buildDeps(application *app.App, ctx context.Context)` (`main.go:34`) takes the context
second.** Go convention is context-first. Not flagged by `golangci-lint` (0 issues), so this is
style only.

**M-3 — `internal/gitx TestExecRunnerTimeout` fails deterministically outside a git working
directory** (`exec_test.go:69`: "want deadline error, got git query exited with code 128"). Not
Task 20's code and it passes in the worktree; recorded only to account for the scratch-run output.

---

## Standing-constraint checks

| Check | Result |
|---|---|
| Tokens in logs | **PASS.** This task adds exactly two log lines: `main.go:88` (`"listening"`, `addr` + `buildinfo.Version`) and `main.go:103` (`"shutting down"`). Neither touches config. No startup config dump. |
| Tokens in error strings | **PASS.** `main.go:51` prints `serve()`'s error to stderr; every error it can carry is either a `*config.Error{Variable, Reason}` (names only, no values — `config.go:227`, `app.go:191,195`) or a git/path error. `config.Secret` redacts via `String()`, `MarshalJSON()`, and `LogValue()` (`internal/config/secret.go:19,22,25`). |
| `INTERNAL` error code | **PASS.** Absent; the only three matches are comments asserting its absence. |
| Error-code set | **PASS.** The only code this diff touches is `NOT_FOUND` (`router.go:77`), which is in the transport set. |
| `internal/session/store.go` untouched | **PASS.** Not in the diff. |
| Configuration environment-only | **PASS.** `serve(ctx, env)` takes `os.Environ()` (`main.go:50`); no flags, no file reads. `APP_PORT` via `application.Config.Port` (`main.go:78`), `CLEANUP_INTERVAL_MINUTES` via `main.go:43`. |
| Timing-shaped tests | **PASS.** `main_test.go:51` is a `time.Sleep(20ms)` *inside* a poll-to-deadline loop (correct pattern), and `main_test.go:76` is a 20s select-timeout ceiling. `main_test.go:84`'s `DialTimeout` is not a race: `srv.Shutdown` has already returned, which guarantees the listener is closed. No fixed sleep is raced against a result. |
| No mutation leaked | **PASS.** `git status --porcelain` empty; `git diff HEAD -- apps/backend` empty. |

---

## Confirmed gap (not a new discovery)

The implementer's disclosure that a genuine `ListenAndServe` startup failure is untested is
**accurate and complete**. I verified the code path by inspection (`main.go:89-92` → `:96-101` →
`:50-52`, exit 1) and by mutation (M5 survives). Its choice to report the gap rather than ship a
flaky port-race test is the right call. Recorded as I-3 for the coverage debt, not as a new defect.

---

## Spec compliance detail

| Brief requirement | Status |
|---|---|
| `cmd/converge/main.go` + `main_test.go` created | ✔ both present |
| Wires `app.New` | ✔ `main.go:70` |
| "starts the sweeper goroutine" | **Correctly NOT done in `main.go`.** Per R34 the router owns it; the brief's prose and its sample `main.go` (`go application.Store.RunSweeper(...)`) are both wrong here. The implementer instead fixed the actual gap — nothing had ever populated `Deps.Store`/`Deps.CleanupInterval` — at `main.go:42-43`. Measured result: exactly one sweeper, at the configured interval. This is the brief's sample being defective, and the deviation is correct. |
| Serves `NewRouter` on `APP_PORT` | ✔ `main.go:76,78` |
| SIGINT/SIGTERM graceful shutdown | ✔ `main.go:48`, `main.go:102-110` |
| 15-second drain | ✔ `shutdownTimeout = 15 * time.Second`, `main.go:23`, used at `main.go:104` |
| `http.ErrServerClosed` treated as success | ✔ `main.go:89`; mutation-protected (M6) |
| Step 3 full gate passes | ✔ all five commands clean |

**Additions beyond the brief, all justified, none YAGNI:**
`buildDeps` extraction (accepted under R36; earns its keep — it is the only reason M1/M2/M3 are
caught by a fast unit test), `TestBuildDepsWiresConfiguredCleanupIntervalAndStore`, the
`nopCleaner` fixture it needs, the one-line `router.go` fix (R35), and the port-released assertion
(proven load-bearing above). No speculative abstraction, no unused exports, no extra config knobs.

**Verdict: ✅ COMPLIANT.**

## Quality verdict

**NEEDS-WORK** — driven by I-1 (a measured behavioural regression that ships silently the moment
the frontend is built) and I-2 (a test assertion guaranteed to break in the next phase). I-3 and
I-4 are coverage debt on error and UI-composition paths. Nothing here is Critical: the sweeper is
correct and leak-free, error propagation is correct by inspection, and no secret leaks.
