# Task 4 Audit — `internal/provider`

## Job A — Fix verification (diff `875b73f..dae9a4e`)

**Finding 1 — Missing `GitUser(kind Kind) string`: ADDRESSED**
`apps/backend/internal/provider/provider.go:118-123` adds `GitUser`, returning `"x-access-token"` for `KindGitHub` and `"oauth2"` for everything else, matching the finding exactly. Test coverage: `apps/backend/internal/provider/provider_test.go:5-11` (`TestGitUser`) exercises both `KindGitHub` and `KindGitLab` branches and asserts the exact literal strings.

**Finding 2 — Over-broad gosec G704 suppression: ADDRESSED**
`apps/backend/.golangci.yml:30` (old) `G704` module-wide exclusion line is deleted; the diff hunk at lines 27-30 shows only `G204` remaining under `gosec.settings.excludes`. The G204 exclusion is untouched (still present, correctly out of scope). The suppression is now scoped: `apps/backend/internal/provider/httpjson.go:12-13` carries `//nolint:gosec // G704: req is built by callers from the configured provider BaseURL and fixed API paths, not from user-supplied URLs` directly above the `client.Do(req.WithContext(ctx))` call. This is a real per-line directive, not a config-level exclusion, so it will not silently blanket-suppress G704 in future files.

**Finding 3 — Vacuous assertion in `errors_test.go`: ADDRESSED**
`apps/backend/internal/provider/errors_test.go:64` (new) replaces the always-true `strings.Contains(err.Error(), "418"[:0]+"")` with `strings.Contains(err.Error(), strconv.Itoa(status))`, inside the `for status, want := range cases` loop (line 58). This is not vacuous: `strconv.Itoa(status)` varies per iteration and `errors.go:213` (`Error()` = `fmt.Sprintf("%s %s returned %d", ...)`) does embed the numeric status in the message, so the assertion has real, per-case failure conditions. The implementer's stated reasoning — that a hardcoded `"418"` literal would spuriously fail on the other six status codes in the same loop — is correct: the loop asserts on `err.Error()` for every one of the seven cases (401/403/404/429/500/503/418), and only the per-iteration status code is guaranteed to appear in each message. The fix is the right generalization of the original (broken) intent, not a weaker substitute.

**New breakage introduced by the fix diff itself: none found.** The three changed non-test files (`provider.go`, `httpjson.go`, `.golangci.yml`) are additive/narrowing only; `errors_test.go`'s change is a strict tightening of an existing assertion. No other logic in the fix diff was touched.

## Job B — Backend guidelines audit (full diff `e372974..dae9a4e`)

`internal/provider` has no `resource.go`, `processor.go`, or `administrator.go` in this task — by design (Task 4 brief only produces model/interface/errors/registry/fake; REST/processor layers land in later tasks) and consistent with the project's documented deviations (no GORM/entity types, no lazy `Provider[T]`). Accordingly, DOM-02/03/04/05/06/07/08/09/10/12/13/14/15/16/17/18 are **N/A** for this diff — there is no REST resource file, processor, or administrator to check, and no entity/`ToEntity`/`Make` pattern applies since there is no persistence layer here. Checks below are the ones that do apply to a model+builder+registry+fake package, plus the security review the task calls out explicitly.

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| DOM-01 | builder.go exists, fluent setters, `Build()` validates | PASS | `apps/backend/internal/provider/builder.go:41-74` (`RepositoryBuilder`, validates via `gitx.ValidateRepoFullName` at line 63, requires non-empty provider id at line 60-62) and `builder.go:76-161` (`ChangeRequestBuilder`, validates number/title/target branch/SHAs at lines 132-156) |
| DOM-19 | Table-driven tests (`tests := []struct{...}` + `t.Run`) | FAIL | No `t.Run` or `[]struct{...}` table found in any of `errors_test.go`, `model_test.go`, `registry_test.go`, `httpjson_test.go`, `provider_test.go` (grep confirms zero matches for `t.Run` across the package). `errors_test.go:12` uses a `map[int]error` loop instead of a struct slice, and other tests are single-scenario functions. Guideline (`testing-guide.md`: "Prefer table-driven tests") is a preference, not a hard mandate — classified Minor below. |

### Security review (weighted heavily per task instructions)

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| SEC-token-leak | No token/Authorization/query/body in error strings or logs | PASS | `errors.go:212-218` `Error()` builds only from `Method`, `Path`, `Status`, `RetryAfter` — never from headers or body. `httpjson.go:14-15,20` on non-2xx, body is read into `io.Discard` (never captured into a string) before constructing `NewStatusError`; network-error path (`httpjson.go:16-18`) wraps only `ErrUnavailable`, not the underlying `client.Do` error text (which could contain a URL with embedded credentials). Verified by `httpjson_test.go:38-41`: a 502 response body containing `"secret-body"` is asserted absent from `err.Error()`. |
| SEC-immutability | `Repository`/`ChangeRequest`/`Commit` cannot be mutated post-construction via shared backing arrays | PASS | All fields on `Repository` (`model.go:567-575`), `Commit` (`model.go:586-590`), `ChangeRequest` (`model.go:614-632`) are unexported with no setters outside the builder. `Commits()` returns a fresh copy (`model.go:652-656`, `make`+`copy`), `LandingCandidates()` builds a new `[]string` (`model.go:668-676`), `WithCommits` reassigns via `append([]Commit(nil), commits...)` on a value receiver copy, not the original (`model.go:659-665`), confirmed by test `model_test.go:737-739` asserting the receiver's commit count is unchanged after `WithCommits(nil)`. `fake.Provider.GetChangeCommits` also returns a defensive copy (`fake.go:435`, `append([]provider.Commit(nil), ...)`), so no caller can mutate the fake's internal store through a returned slice. |
| SEC-builder-validation | Builders reuse `gitx` validators rather than reimplementing regexes | PASS | `builder.go:63` calls `gitx.ValidateRepoFullName(r.fullName)`; `builder.go:152` calls `gitx.ValidateSHA(sha)`; `model.go:594` (`NewCommit`) also calls `gitx.ValidateSHA`. Confirmed `gitx.ValidateSHA`/`gitx.ValidateRepoFullName` are real regex-backed validators in `apps/backend/internal/gitx/validate.go:23-45` — not duplicated logic in `provider`. |
| SEC-map-nondeterminism | No unsorted map-iteration ordering in listing/error paths | PASS | `registry.go:918-925` (`Registry.All()`) sorts by `ID()` via `sort.Slice` before returning. `fake/fake.go:364` (`ListRepositories`) sorts by `FullName()`; `fake/fake.go:393-398` (`ListMergedChanges`) sorts by `MergedAt()` descending then `Number()` descending. `Get`/`GetChange`/`GetChangeCommits` key directly into maps (no iteration). |

## Issues

### Critical
None.

### Important
None.

### Minor
- DOM-19: no test file in `internal/provider` uses the table-driven `[]struct{...}` + `t.Run` pattern the guideline prefers (`testing-guide.md` "Prefer table-driven tests"). `errors_test.go:12` gets partial credit via a `map[int]error` loop, but the other four test files are single-scenario functions with no sub-test names. Not blocking since the guideline uses "prefer," and every branch/edge case cited in the brief is still covered.

## Assessment

**Fix round verdict:** All findings addressed.
**Task quality:** Approved.
**Reasoning:** All three fix-round findings are genuinely resolved with correct, non-superficial fixes (a real function + test, a real line-scoped nolint replacing a config-wide exclusion, and a per-iteration assertion that actually varies with the loop). The security-sensitive paths (token/body leakage, immutability, validator reuse, deterministic ordering) all check out with concrete evidence; the only gap found is a Minor, non-mandatory test-style preference.
