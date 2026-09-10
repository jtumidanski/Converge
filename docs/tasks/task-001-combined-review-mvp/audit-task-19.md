# Audit — Task 19: JSON:API HTTP handlers, router, middleware, embedded UI serving

- **Commit under review:** `03a16e3` (12 files, 1264 insertions, all under `apps/backend/internal/api/`)
- **Brief:** `.superpowers/sdd/plan/task-19-brief.md`
- **Report:** `.superpowers/sdd/plan/task-19-report.md`
- **Method:** read every implementation file; 39 mutations applied to a scratch copy at `/tmp/mut-19`
  (never the worktree), each with grep proof of landing; plus 7 behavioural probes.

## Verdicts

1. **Spec compliance: ✅ COMPLIANT** (two deviations named below — neither is a missing requirement)
2. **Task quality: NEEDS-WORK** — 1 Critical, 5 Important, 6 Minor

## Verification gate (cwd `apps/backend`) — ALL GREEN

| Command | Result |
|---|---|
| `go test -race -count=1 ./...` | PASS — 17 packages ok, 1 `[no test files]` (`provider/fake`) |
| `go vet ./...` | clean (exit 0) |
| `go tool golangci-lint run` | `0 issues.` |
| `CGO_ENABLED=0 go build ./...` | clean (exit 0) |

Worktree left untouched: `git status --short` empty; `diff -rq /tmp/mut-19/internal/api <worktree>` reports
only my scratch probe file. Every mutation was reverted.

---

## Part 1 — Spec compliance against the brief

### Everything the brief asked for is present

| Brief requirement | Evidence |
|---|---|
| 12 files created | `errors.go` `middleware.go` `router.go` `providers.go` `repositories.go` `changes.go` `reviews.go` `review_files.go` `health.go` `ui.go` `api_test.go` `ui_test.go` — all present |
| `Deps{Service,Providers,Log,UI,UIPresent,BuildContext}` | `router.go:17-37` |
| `BuildContext` defaults to `context.Background()` when nil | `router.go:50-53` |
| `func NewRouter(d Deps) http.Handler`, middleware applied | `router.go:49`, returns `withMiddleware(mux, d.Log)` at `router.go:82` |
| All 13 routes | `router.go:59-81` — `/healthz`, `/api/providers`, `.../repositories`, `.../repositories/{repo}`, `.../{repo}/changes`, POST/GET `/api/reviews`, GET/DELETE `/api/reviews/{id}`, `.../files`, `.../files/{path...}`, `.../diff`, `/api/` catch-all, `GET /` SPA |
| 6 resource types | `providers` `providers.go:17`; `repositories` `repositories.go:20`; `changes` `changes.go:40`; `reviews` `reviews.go:71`; `review-files` `review_files.go:47`; `review-file-diffs` `review_files.go:72` |
| `writeDomainError(w, log, err)` per design §9.2 | `errors.go:21` |
| `{repo}` vs `{repo...}` risk resolved and documented | `router.go:62-67` comment; behaviour pinned by `api_test.go:164-171` |

### Controller rulings — verified against code

| Ruling | Status | Evidence |
|---|---|---|
| **R27** — no `INTERNAL`; unclassified → `GIT_FAILURE` | ✅ | `errors.go:65` returns `GIT_FAILURE`; `middleware.go:53` panic fallback also `GIT_FAILURE`. `grep -rn '"INTERNAL"' apps/backend` returns only two *comment* lines (`errors.go:18`, `middleware.go:50`). Deviates from the brief's sample (`errors.go:509`, `middleware.go:565` in the brief both wrote `"INTERNAL"`) — **the implementation is correct and the brief's sample is defective.** |
| **R31 / Task-19 ruling** — `context.DeadlineExceeded` → 503 + `INTERRUPTED` with its own message | ✅ | `errors.go:62-63`: `http.StatusServiceUnavailable, "INTERRUPTED", "The request took too long and was interrupted before it could finish."` — distinct from `MsgInterrupted()`'s server-restart wording, exactly as R31 requires. |
| **R28** — `session.ErrNotFound` mapped to 404 and exercised | ✅ | `errors.go:56`. **Proven exercised**: mutation M11 (deleting `errors.Is(err, session.ErrNotFound)` from that case) fails `api_test.go:275`. |
| Every `jsonapi.Decode` failure → `*jsonapi.DecodeError` → 400 | ✅ | `internal/jsonapi/decode.go:35,38,43,46,49,52,58` — all six failure paths return `*DecodeError`; `errors.go:36-39` maps it to 400. Mutation M12 (400→500) fails `api_test.go:336`. Any other error type at that call site would fall through to `errors.go:65` → 500, as required. |
| Token leakage | ✅ | `grep -rni "token\|secret\|password" internal/api/*.go` → one hit, `errors.go:55`, a fixed English string containing no value. Request log (`middleware.go:56-62`) logs `r.URL.Path` only — the query string (which carries `?search=`) is never logged. `writeDomainError` logs `err.Error()` at `errors.go:24` but returns only fixed detail strings to the client for 5xx (`errors.go:65`), satisfying design §9.2's "no internal detail". |
| Session states / 8-hex ids | ✅ | `reviews.go:117` `workspace.ValidateSessionID`; statuses pass through from `session` unmodified (`reviews.go:73`). |
| JSON:API envelopes + `application/vnd.api+json` | ✅ | `jsonapi/document.go:52` sets the media type on every write; asserted at `api_test.go:141`. Probe: a 404 error doc returns `ct="application/vnd.api+json"`. |
| `meta.page` on lists | ✅ | Present on the two paginated lists (`repositories.go:78`, `changes.go:80`). Absent on `providers`/`reviews`/`files` — **this matches the PRD**, whose examples for §5.1, §5.4 `/files` and `/api/reviews` carry no `meta` (prd.md:479-486, 588-596). Not a finding. |

### Deviation 1 — extra scope beyond the brief (YAGNI)

`Deps` gained two fields the brief's enumerated struct does not contain — `Store *session.Store` and
`CleanupInterval time.Duration` (`router.go:35-36`) — and `NewRouter` starts a goroutine as a
constructor side effect (`router.go:54-56`). The implementer's justification holds up: `RunSweeper`
has no other caller in the repo, and the addition is the only mutation in this change set that the
implementer proved discriminating (M8, below). But it is scope the brief did not request, and a
router factory that spawns a background goroutine is a layering smell — see Minor 4.

### Deviation 2 — four error codes outside the sanctioned exact-string set

The binding set is 16 strings. `internal/api` emits four more:

| Code | Site |
|---|---|
| `INVALID_REQUEST` | `errors.go:38` |
| `NOT_FOUND` | `errors.go:57`, `repositories.go:45`, `reviews.go:118`, `reviews.go:123`, `router.go:77` |
| `NOT_ACCEPTABLE` | `middleware.go:65` |
| `INVALID_STATE` | `changes.go:58` |

These come verbatim from the brief, and the PRD's enumeration (prd.md:583-586) sits inside §5.4's
*ReviewError* discussion, so it plausibly scopes the session-error vocabulary rather than the HTTP
transport vocabulary. I am not calling this non-compliance, but Phase F will consume these strings
and they were never adjudicated. See Important 5.

---

## Part 2 — Mutation testing of `api_test.go` / `ui_test.go`

Baseline: `go test -count=1 ./internal/api/` → `ok ... 0.776s`. Each row: exact edit, grep proof that
the mutant landed in the file that was compiled, and suite result.

### Discriminating (24 mutations — the suite caught these)

| # | Edit | Grep proof (line in mutated file) | Result |
|---|---|---|---|
| M1 | `providerFor` 404 → 500 | `repositories.go:45  http.StatusInternalServerError, "NOT_FOUND", ...` | **FAIL** `api_test.go:178: unknown provider: 500` |
| M2 | `"REVIEW_NOT_READY"` → `"REVIEW_NOT_READYY"` | `errors.go:61  return http.StatusConflict, "REVIEW_NOT_READYY", ...` | **FAIL** `api_test.go:302: ..."code":"REVIEW_NOT_READYY"...` |
| M3 | type `review-files` → `review-file` | `review_files.go:47  Type: "review-file"` | **FAIL** `api_test.go:258` |
| M4 | type `review-file-diffs` → `review-file-diffz` | `review_files.go:72  Type: "review-file-diffz"` | **FAIL** `api_test.go:267` |
| M5 | type `providers` → `provider` | `providers.go:17  Type: "provider"` | **FAIL** `api_test.go:146` |
| M6 | invert validation: accept `state=open` | `changes.go:57  ... && state != "open" {` | **FAIL** `api_test.go:197: state=open: 200` |
| M7 | `InputError` 400 → 500 | `errors.go:34  return http.StatusInternalServerError, string(ie.Code), ie.Message` | **FAIL** all 5 subtests of `api_test.go:323`, each naming its own `INVALID_*` code |
| M8 | delete `go d.Store.RunSweeper(...)` | `router.go:55  _ = buildCtx // MUTANT: sweeper not started` | **FAIL** `api_test.go:412: sweeper never expired the session` |
| M10 | `ErrNotFound` 404 → 500 | `errors.go:57  return http.StatusInternalServerError, "NOT_FOUND", ...` | **FAIL** `api_test.go:174`, `api_test.go:275` |
| M11 | drop `session.ErrNotFound` from 404 case (R28) | `errors.go:56  case errors.Is(err, provider.ErrNotFound):` | **FAIL** `api_test.go:275: unknown file: 500` |
| M12 | `DecodeError` 400 → 500 | `errors.go:38  return http.StatusInternalServerError, "INVALID_REQUEST", de.Detail` | **FAIL** `api_test.go:336: wrong type: 500` |
| M15 | `deleteReview` 204 → 200 | `reviews.go:147  w.WriteHeader(http.StatusOK)` | **FAIL** `api_test.go:286: delete 0: 200` |
| M16 | delete `Service.StartBuild(...)` | `reviews.go:97  _ = s.buildCtx // MUTANT: build never started` | **FAIL** `api_test.go:233: build did not finish` |
| M18 | `/api/` catch-all 404 → 500 | `router.go:77  http.StatusInternalServerError, "NOT_FOUND", ...` | **FAIL** `api_test.go:344: PUT: 500` |
| M21 | create 202 → 200 | `reviews.go:98  jsonapi.WriteOne(w, http.StatusOK, ...)` | **FAIL** `api_test.go:209: post: 200` |
| M22 | health `"ok"` → `"degraded"` | `health.go:24  Status: "degraded"` | **FAIL** `api_test.go:134` |
| M23 | raw diff CT `text/plain` → `application/json` | `review_files.go:97  Set("Content-Type", "application/json")` | **FAIL** `api_test.go:280` |
| M30 | `LandingSHA: landing` → `nil` | *(compile error — `landing` unused; bogus measurement, re-run as M30b)* | n/a |
| M35 | drop `Cache-Control: no-cache` on index | `ui.go:39` (the `Set("Cache-Control","no-cache")` line removed) | **FAIL** `ui_test.go:23` ×3 |
| M36 | assets get `no-cache` not `immutable` | `ui.go:26  Set("Cache-Control", "no-cache")` | **FAIL** `ui_test.go:29` |
| M37 | UI-absent 503 → 200 | `ui.go:16  w.WriteHeader(http.StatusOK)` | **FAIL** `ui_test.go:44: code = 200` |

**The implementer's central claim is substantially true.** The suite does assert error-code strings
and resource-type strings, not just status codes: M2, M3, M4, M5 and M7 all die on the string. Status
mutations M1, M10, M12, M15, M18, M21, M22 all die. The validation-branch inversion M6 dies. The
sweeper mutation M8 reproduces exactly. This is *not* one of the four occasions where that claim
shape was false.

### Non-discriminating (13 mutations — the suite stayed green)

| # | Edit | Grep proof | Result |
|---|---|---|---|
| **M9** | disable Accept negotiation entirely | `middleware.go:64  if false { // MUTANT: negotiation disabled` | **PASSED** |
| **M13** | `INTERRUPTED` branch → `http.StatusTeapot, "NOT_INTERRUPTED"` | `errors.go:63  return http.StatusTeapot, "NOT_INTERRUPTED", ...` | **PASSED** |
| **M14** | default branch → `http.StatusBadRequest, "INTERNAL"` | `errors.go:65  return http.StatusBadRequest, "INTERNAL", ...` | **PASSED** |
| **M17** | panic-recovery code → `"INTERNAL"` | `middleware.go:53  ..., "INTERNAL", jsonapi.StatusTitle(...)` | **PASSED** |
| **M19** | `sessionFor`: drop `ValidateSessionID` | `reviews.go:117  if false {` | **PASSED** |
| **M20** | `repoNameFrom`: drop `ValidateRepoFullName` | `repositories.go:57  _ = gitx.ValidateRepoFullName` | **PASSED** |
| **M24** | `stringPtr` → always `nil` (`baseSha`/`headSha`/`stage` permanently null) | `reviews.go:50  func stringPtr(s string) *string { return nil }` | **PASSED** |
| **M25** | `baseDescription` → always `""` | `reviews.go:59  if false {` | **PASSED** |
| **M26** | `Totals: s.Totals()` → `Totals: nil` | `reviews.go:75  Included: included, Totals: nil,` | **PASSED** |
| **M27** | `included` always empty | `reviews.go:67  for _, rc := range []session.ResolvedChange(nil) {` | **PASSED** |
| **M28** | `fileAttrs` → `{Path: f.Path}` (status/additions/deletions/binary zeroed) | `review_files.go:27  return reviewFileAttributes{Path: f.Path}` | **PASSED** |
| **M29** | landing-SHA precedence swapped (squash before merge) | `changes.go:33  []string{c.SquashCommitSHA(), c.MergeCommitSHA()}` | **PASSED** |
| **M30b** | `landingSha` always nil (compile-safe) | `changes.go:33  for _, candidate := range []string{} { _ = c` | **PASSED** |
| **M31** | change `Title`/`Author`/`SourceBranch`/`TargetBranch` → `""` | `changes.go:41  Number: c.Number(), Title: "", Author: "", ...` | **PASSED** |
| **M32** | repository `Name`/`Namespace`/`WebURL` → `""` | `repositories.go:21  Name: "", Namespace: "", DefaultBranch: r.DefaultBranch(), WebURL: "",` | **PASSED** |
| **M33** | `relationships.files.links.related` → `"/wrong"` | `reviews.go:79  {Links: jsonapi.Links{Related: "/wrong"}}` | **PASSED** |
| **M34** | `pageFrom` ignores `pageSize` | `repositories.go:35  _ = size` | **PASSED** |
| M38 | `jsonapi` body-size limit disabled | `jsonapi/decode.go:37  if false {` | **PASSED** in `./internal/api` (Task 18 scope — the limit *works*, see probe below; only the api suite is blind to it) |

### Behavioural probes (scratch copy, not mutations)

All confirm the code is **correct** even where the tests do not check it:

```
PROBE accept=text/html      -> 406 ct="application/vnd.api+json" {"code":"NOT_ACCEPTABLE"...}
PROBE content-type=text/plain POST -> 202          <- no request Content-Type validation
PROBE oversize body (2 MiB) -> 400 {"code":"INVALID_REQUEST","detail":"The request body is too large."}
PROBE X-Request-Id          -> "c5218dcd2fa81818"
PROBE 404 error doc         -> ct="application/vnd.api+json"
PROBE repo "-evil/repo"     -> 400 INVALID_REPOSITORY
PROBE repo "a/../b"         -> 400 INVALID_REPOSITORY
```

---

## Part 3 — Findings

### CRITICAL

**C1 — Non-discriminating attribute assertions: every JSON:API resource payload in the service can
be silently zeroed and the suite stays green.** (11 surviving mutations: M24, M25, M26, M27, M28,
M29, M30b, M31, M32, M33, M34.)

`api_test.go` asserts *key presence*, never *value correctness*, for resource attributes:

```go
// api_test.go:238-242
for _, key := range []string{"baseSha", "headSha", "baseDescription", "included", "totals"} {
    if _, ok := attrs[key]; !ok {
        t.Errorf("attribute %s missing: %v", key, attrs)
    }
}
```

A JSON `null` satisfies `_, ok := attrs[key]`. The same pattern is used at `api_test.go:190-194` for
`changes`. Consequently `stringPtr` returning `nil` unconditionally (M24 — `baseSha`, `headSha` and
`stage` permanently null), `Totals` permanently null (M26), `included` permanently empty (M27),
`landingSha` permanently null (M30b), every `changes` string attribute permanently `""` (M31), every
`repositories` string attribute permanently `""` (M32), every file summary count permanently `0`
(M28), and the `files` relationship link pointing at `/wrong` (M33) all pass.

This is precisely the recurring defect class the plan has hit nine times, in its subtlest form: the
tests are not blind, they are *aimed off-target* — they check that the handler produced a field, not
that it produced the right value. `baseSha`, `headSha`, `baseDescription` and `totals` are the
fields that tell a reviewer *what they are looking at*; a regression that nulls them ships an
incorrect (or unlabelled) diff to a reviewer with no warning and a green suite.

The implementation itself is **correct** — I read every transform (`reviews.go:65-82`,
`changes.go:31-44`, `repositories.go:19-23`, `review_files.go:26-28`) and each wires the real
accessor. The defect is entirely in the test layer, and it is inherited from the brief. But the
brief's tests were shipped unverified on a claim, and this is what the measurement shows.

*Required fix:* assert values, not keys — e.g. `attrs["baseSha"] == <the sq/base SHA the fixture
built>`, `attrs["baseDescription"] == "Immediately before #421"`, `totals.files == 1`,
`included[0].number == 421 && included[0].strategy == "squash"`, `files[0].additions == 1 &&
files[0].status == "added"`, `changes[0].title == "Add a"`, `landingSha == sq`,
`relationships.files.links.related == "/api/reviews/"+id+"/files"`. Every one of those values is
already known to the fixture at `api_test.go:38-84`.

### IMPORTANT

**I1 — Content negotiation is entirely untested (M9).** Deleting the whole `/api/*` Accept check
(`middleware.go:64-67`) leaves the suite green. The behaviour is correct (probe: 406 with
`NOT_ACCEPTABLE`), but the design mandates it (§9.1) and nothing pins it. Two lines of test:
`Accept: text/html` → 406, `Accept: application/vnd.api+json` → 200.

**I2 — `classify()`'s fallback and `INTERRUPTED` branches are untested (M13, M14).** `errors.go:65`
can be changed to `return http.StatusBadRequest, "INTERNAL", ...` and the suite passes. This is the
one branch the controller specifically ruled on (R27) and specifically asked to be protected from
`INTERNAL` reappearing — and it is the branch with zero coverage. Likewise `errors.go:62-63` can
become `http.StatusTeapot, "NOT_INTERRUPTED"` with no failure, despite the Task-19 ruling that fixed
503+`INTERRUPTED`. Both are directly unit-testable: `classify(errors.New("x"))` and
`classify(fmt.Errorf("w: %w", context.DeadlineExceeded))`.

**I3 — `sessionFor` reports a corrupt session as 404, and `Store.Corrupted` has zero production
callers.** `reviews.go:121-125` treats `Service.Get`'s `ok == false` as "no such review". But
`session/store.go:171-178` documents exactly the opposite contract:

> *"Its bool return only ever means 'this id is currently in the live index' — it deliberately cannot
> distinguish 'id never existed' from 'id existed but LoadAll found its session.json unreadable or
> invalid'. That distinction is not lost, though: LoadAll records every such id (see Corrupted), so a
> caller building a 404 vs. 500 response can check Corrupted(id) after a failed Get."*

Task 11 built `Store.Corrupted` (`store.go:193`) for this consumer. `grep -rn Corrupted` across
`apps/backend` shows the only references outside `store.go` are in `session/store_test.go` — **no
production caller exists.** A session whose `session.json` is unreadable therefore answers
`404 NOT_FOUND "No review exists with that id."` while its workspace still sits on disk. That is the
recurring class: a real persistence failure returned as an innocuous "not found". Per R27 the
corrupt case should be `500 GIT_FAILURE`. Requires a small `review.Service` passthrough (the api
layer holds no `*session.Store` on the read path today).

**I4 — HTTP-layer repository-name validation is untested (M20).** Deleting
`gitx.ValidateRepoFullName` from `repoNameFrom` (`repositories.go:57-59`) leaves the suite green.
The binding constraint ("repo full name `^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`, no `..` segment, no
leading `/`/`-`/`.`; anything starting with `-` from a client is rejected") is enforced correctly
(probes confirm 400 `INVALID_REPOSITORY` for `-evil/repo` and `a/../b`) but nothing in the suite
reaches `repositories.go:89-93` or `changes.go:51-55`. This is the constraint whose violation feeds
attacker-controlled strings toward provider calls; it deserves a pinned test.

**I5 — Four error codes ship outside the sanctioned exact-string set.** `INVALID_REQUEST`
(`errors.go:38`), `NOT_FOUND` (`errors.go:57`, `repositories.go:45`, `reviews.go:118`,
`reviews.go:123`, `router.go:77`), `NOT_ACCEPTABLE` (`middleware.go:65`), `INVALID_STATE`
(`changes.go:58`). All are brief-mandated and the PRD's enumeration is arguably scoped to
`ReviewError`; but Phase F consumes these verbatim and they were never adjudicated. Needs a
controller ruling before Phase F, not a code change from Task 19.

### MINOR

**m1 — Panic-recovery branch untested (M17).** `middleware.go:53` can emit `"INTERNAL"` with the
suite green. Same R27 exposure as I2, lower reachability.

**m2 — `sessionFor`'s `ValidateSessionID` is behaviourally redundant and untested (M19).**
Removing `reviews.go:117-120` changes nothing observable, because `Store.Get` misses anyway. It is
correct defence-in-depth; note only that no test distinguishes it. (`deleteReview`'s use at
`reviews.go:142` is *not* redundant — it gates the `Finish` call.)

**m3 — No request `Content-Type` validation.** Probe: `POST /api/reviews` with
`Content-Type: text/plain` → 202. The spec says requests accept `application/vnd.api+json` or
`application/json`; nothing rejects other types. Lenient rather than wrong, and neither brief nor
design requires the check.

**m4 — `NewRouter` starts a background goroutine as a constructor side effect** (`router.go:54-56`),
using two `Deps` fields the brief did not specify. Functionally proven (M8) and genuinely needed, but
a router factory owning a process-lifetime goroutine is misplaced; Task 20's `app`/server layer is
the natural owner. Not blocking — flagged so Task 20 can move it deliberately rather than inherit it.

**m5 — `baseDescription` renders `#421`** (`reviews.go:60`) where the PRD's GitLab example shows
`"Immediately before !421"` (prd.md:548). The brief explicitly specifies `#` (brief line 892), so the
implementation follows its requirement; raising only because the PRD sample and the frontend copy may
expect the provider-appropriate sigil.

**m6 — `pageFrom` silently discards malformed pagination (M34).** `?page=abc` / `?pageSize=abc`
leave the field at 0 and `Normalize()` supplies defaults, with no error to the client. Consistent
with the PRD (which specifies clamping, not rejection); noted because `repositories.go:26-39` is
completely untested — even the `pageSize=500` clamp assertion at `api_test.go:199-202` passes with
the whole `pageSize` read deleted.

---

## Confirmation of the known open item

Confirmed, and it is Task 20's. `grep -rn "api.Deps\|api.NewRouter\|CleanupInterval" internal/app/
internal/config/ cmd/` returns **no** reference to `api.Deps` or `api.NewRouter` anywhere —
`internal/app` does not construct the router at all. `cfg.CleanupInterval` exists
(`internal/config/config.go:47`, default 30 min from `CLEANUP_INTERVAL_MINUTES` at
`config.go:97`) and is currently consumed by nothing outside `config_test.go`. The router's default
is sane: `router.go:54` requires `CleanupInterval > 0`, so an unwired `Deps` starts no goroutine
rather than spinning at zero interval. In production today the sweeper is therefore dormant — Task 20
must set both `Store` and `CleanupInterval` on `api.Deps`.

---

## Summary

### Blocking
- **C1** — `api_test.go` attribute assertions check key presence, not value; 11 mutations that
  permanently null/zero/empty the JSON:API payloads (`baseSha`, `headSha`, `stage`,
  `baseDescription`, `totals`, `included`, `landingSha`, all `changes`/`repositories` string
  attributes, all file-summary counts, the `files` relationship link) all pass.

### Should fix before merge
- **I1** Accept negotiation untested (M9)
- **I2** `classify()` default (`GIT_FAILURE`) and `INTERRUPTED` branches untested — `INTERNAL` can
  reappear silently (M13, M14)
- **I3** `sessionFor` returns 404 for a corrupt `session.json`; `Store.Corrupted` has no production
  caller
- **I4** HTTP-layer `ValidateRepoFullName` untested (M20)
- **I5** four unsanctioned error-code strings — needs controller adjudication before Phase F

### Non-blocking
- m1 panic-recovery code untested; m2 redundant `ValidateSessionID`; m3 no request Content-Type
  check; m4 goroutine started by `NewRouter`; m5 `#` vs `!` in `baseDescription`; m6 `pageFrom`
  untested and silently lenient.
