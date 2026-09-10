# Audit — Task 5: GitHub provider (`internal/provider/github`)

Diff range: `dae9a4e..edbeba9` (commit `edbeba9`). Read-only review, no code executed.

## Job A — Spec compliance

✅ Spec compliant, with one accepted deviation (verified) and two ⚠️ items that can't be settled from the diff alone.

- Constructor signature matches exactly: `func New(id, displayName, baseURL string, token config.Secret, client *http.Client, now func() time.Time) *Client` — `apps/backend/internal/provider/github/client.go:49`.
- Constants match brief values exactly: `APIVersion = "2022-11-28"` (client.go:21), `MaxScanPages = 10` (client.go:24), `ScanCacheTTL = 60 * time.Second` (client.go:26), `MaxPRCommits = 250` (client.go:28).
- `*Client` satisfies `provider.GitProvider` in full: compile-time assertion `var _ provider.GitProvider = (*Client)(nil)` at client.go:188.
- Injected `now func() time.Time` is actually used for cache expiry, not `time.Now()` called directly at the call site: `c.now().Sub(entry.fetchedAt) > ScanCacheTTL` and `fetchedAt: c.now()` at `list.go:36-37`. The only direct `time.Now` reference is the constructor's nil-guard default (`client.go:51`), which is correct and necessary — callers that don't inject a clock still get real time, but the test-injected clock (`client_test.go:303-304`) is honored everywhere cache expiry is checked.
- All ten files match the brief's `Files:` list exactly (client.go, mapping.go, list.go, client_test.go, six `testdata/*.json` fixtures) — confirmed against the diff stat.
- Reported deviation (tagged vs. untagged switch for staticcheck QF1002) verified as claimed: `client_test.go:261` (`switch r.URL.Path { case "/user/repos": ... }`) is a tagged switch; every case, fixture name, status code, and query assertion is identical in content and order to the brief's untagged version. No behavioral change — confirmed by direct comparison against the brief's `client_test.go` snippet (brief lines 120-154 vs. diff's `client_test.go:260-295`).
- Fixture content (`testdata/*.json`) is byte-identical to the brief's specified JSON in all six files — confirmed by diff comparison.

⚠️ Cannot verify from diff alone:
- The report claims `go test -race -count=1 ./internal/provider/github/` and the full backend gate (`go vet`, `golangci-lint run ./...`, `CGO_ENABLED=0 go build ./...`) all passed. Per instructions I did not re-run these; the controller should trust the reported gate output only insofar as prior tasks' gates have been reliable, and should re-run the full suite once before merge since this diff introduces a concurrency defect (see Job B) that the existing sequential-only tests cannot catch under `-race`.
- Whether `gitx.ValidateRepoFullName`/`ValidateChangeNumber`/`ValidateBranchSyntax`/`CredentialEnv` behave exactly as this client assumes (e.g., that `CredentialEnv` scopes the header to `http.https://github.com/.extraheader` as asserted in `client_test.go:423`) can't be re-verified here since those files aren't in this diff; this task correctly reuses them without reimplementation (see Job B).

No missing requirements, no gold-plating found — the implementation is a close, unembellished match to the brief's prescribed code.

## Job B — Backend guidelines audit

### Applicability
This package is a provider/infrastructure adapter (implements `provider.GitProvider`), not a JSON:API domain package — there is no `model.go`, `resource.go`, `rest.go`, or `processor.go` in this diff. The DOM-01..DOM-19 / SUB-01..SUB-04 checklist items that assume a REST resource layer (builder.go for a domain model, `Transform`/`TransformSlice`, `RegisterInputHandler`, `resource.go` handlers, error→HTTP status mapping) are **N/A by package type**, not passed-by-absence. What does apply and was checked:

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| Reuse — DoJSON | Uses `provider.DoJSON` instead of reimplementing status/error handling | PASS | `client.go:90` (`h, err := provider.DoJSON(ctx, c.http, req, out)`) |
| Reuse — sentinels | Propagates `provider.ErrNotFound`/`ErrAuth`/`ErrUnavailable` via `DoJSON`, no local reimplementation | PASS | `client_test.go:330,336,339,359` assert `errors.Is(err, provider.Err*)` against errors that only flow through `c.get`→`DoJSON` |
| Reuse — builders | Uses `provider.NewRepositoryBuilder()`/`NewChangeRequestBuilder()`/`NewCommit()`, no manual struct literals for immutable models | PASS | `mapping.go` `toModel()` methods (repoJSON, pullJSON, commitJSON) |
| Reuse — CredentialEnv | `AuthorizeGit` uses `gitx.CredentialEnv` exclusively | PASS | `client.go:180` |
| Reuse — validation | Uses `gitx.ValidateRepoFullName`/`ValidateChangeNumber`/`ValidateBranchSyntax`, no local regex/validation reimplementation | PASS | `client.go:124,136,148`; `list.go:81` |
| No `os.Getenv` in client code | grep | PASS | zero matches in client.go/list.go/mapping.go |
| No direct entity/DB writes | grep for `db.Create`/`db.Save`/`db.Delete` | PASS (N/A — filesystem/no-GORM design, agreed deviation) | zero matches |
| gosec module-wide exclusion not reintroduced | `.golangci.yml` not touched by this diff | PASS | diff file list contains no `.golangci.yml` entry |

### Security review (weighted heavily per instructions)

**Token / header / body never logged or placed in an error string:**
- No `log`/`slog`/`logrus` call exists anywhere in this diff — grepped client.go, list.go, mapping.go: zero log call sites. Nothing to leak through logging.
- Every `fmt.Errorf` in the diff carries only a PR/pull number, never a header, body, or token: `client.go:76` (`"github: build request: %w"`), `client.go:167` (`"pull %d: %w", number, ...`), `mapping.go` `toModel` wrap (`"github pull %d: %w", p.Number, err`). None format `req.Header`, response bodies, or `c.token`.
- `c.token.Reveal()` is called in exactly two places, both required: `client.go:79` (setting the `Authorization` header) and `client.go:180` (passing to `gitx.CredentialEnv`). Neither result is ever concatenated into a URL, error, or log statement. **PASS.**

**`AuthorizeGit` credential handling:**
- Passes credentials only via `gitx.CredentialEnv` environment variables, appended to `spec.Env`: `client.go:179-186`. No token ever touches `spec.Args`.
- `CloneURL` (`client.go:176`) returns `repo.CloneURL()` verbatim — the plain HTTPS clone URL with no embedded credentials, so `git remote get-url origin` after a clone recovers only the bare URL, never the token. **PASS.**

**Scan cache concurrency — CRITICAL FINDING (data race):**
`ListMergedChanges` is documented by the task to be called concurrently. The cache in `list.go` is unsafe under that contract:

- `ensureScanned` (`list.go:31-75`) holds `c.mu` (`list.go:33-34`, `defer c.mu.Unlock()`) for the *entire* fetch-and-mutate loop, including the mutation of `entry.items` (`list.go:66: entry.items = append(entry.items, cr)`) and `entry.nextPage`/`entry.capped` (`list.go:41-43,69,71`). That part is safe in isolation.
- However, the function **returns the bare `*scanEntry` pointer** (`list.go:74: return entry, nil`), and Go's `defer` releases the mutex immediately after the return values are set but before control returns to the caller. So by the time `ListMergedChanges` has the entry back, the lock is already released.
- `ListMergedChanges` then reads `entry.items` unsynchronized at `list.go:106` (`filtered := filterChanges(entry.items, search)`) and reads `entry.nextPage`/`entry.capped` unsynchronized at `list.go:121` (`hasNext := end < len(filtered) || (entry.nextPage != 0 && !entry.capped)`).
- If a second goroutine concurrently calls `ListMergedChanges` for the *same* `(repo, target)` — same cache key, e.g. a scan that needs more items (`need` larger, from a search or a later page) — it will re-enter `ensureScanned`, acquire the now-free `c.mu`, and mutate the same `*scanEntry`'s `items`/`nextPage`/`capped` fields (append can rewrite the backing array in place, or grow it) concurrently with the first goroutine's unguarded reads at lines 106 and 121. This is a genuine unsynchronized concurrent read/write on shared mutable state — a data race by the Go memory model, not merely a logic bug.
- This will **not** be caught by the existing test suite: every test in `client_test.go` calls the client sequentially from a single goroutine (`TestListMergedChangesScansFiltersSortsAndCaches`, `client_test.go:364-413`, makes 8 sequential calls, never concurrent). `go test -race` only flags races that are actually exercised at runtime, so a clean `-race` run (as reported) does not establish this code path is safe — consistent with the audit brief's caution not to trust `-race` cleanliness alone.
- **Fix required:** either copy the needed fields (`items`, `nextPage`, `capped`) out of `entry` while still holding `c.mu` inside `ensureScanned` (returning value copies, not the pointer, to the caller), or have `ListMergedChanges` re-acquire `c.mu` (or a per-entry lock) before reading `entry.items`/`entry.nextPage`/`entry.capped`.

**Lock granularity (secondary, related finding):**
`c.mu` is a single client-wide mutex, and it is held across the network round-trip inside `ensureScanned`'s loop (`list.go:33-34` through `list.go:74`, wrapping the `c.get` call at `list.go:54`). This means a scan for one `(repo, target)` blocks *all* other repos/targets on the same `Client` from starting a scan (cache hit or miss) until the in-flight HTTP call(s) complete. Under real concurrent load (the stated `ListMergedChanges` usage pattern) this serializes what should be independent per-key work. Not a correctness bug like the race above, but a design fragility worth fixing alongside it (e.g., a lock per cache key, or copy-then-fetch-then-swap under a narrower critical section).

**Map iteration nondeterminism:** `c.scans` (`client.go:45`, `map[string]*scanEntry`) is never ranged over anywhere in this diff — only looked up by key (`list.go:35: entry := c.scans[key]`) and written by key (`list.go:38`). No nondeterministic ordering risk found. **PASS.**

**Pagination bounds:**
- `MaxScanPages` is genuinely enforced before each page fetch, not just as a documentation comment: `list.go:41-43` checks `entry.nextPage > MaxScanPages` and breaks (setting `capped = true`) *before* issuing the request for that page, so at most `MaxScanPages` (10) provider pages are ever fetched per `(repo, target)` cache entry. **PASS.**
- `MaxPRCommits` is enforced in `GetChangeCommits`: `client.go:166-168` checks `len(out) > MaxPRCommits` after each page is appended and returns `provider.ErrTooManyCommits` wrapped with the PR number, preventing unbounded accumulation from a hostile/large-commit PR. The loop cannot run away indefinitely — it always terminates via either the bound check, `!hasNext`, or `len(raw) == 0` (`client.go:169`). **PASS.**

**gosec G704:** No `.golangci.yml` changes in this diff; the module-wide exclusion removed in Task 4 was not reintroduced. **PASS** (confirmed by absence in the diff's file list).

### Test quality
- Tests are fixture-driven via `httptest.NewServer` (`client_test.go:250-299`) — zero real network calls; the handler serves canned JSON fixture bytes read from `testdata/`.
- Assertions check real mapped values (branch names, SHAs, states, pagination cursors, cache-hit call counts), not just "no error" — e.g. `client_test.go:352` checks `MergeCommitSHA`/`HeadSHA`/`CommitCount`/`Author`/branches together, and `client_test.go:382-390` explicitly counts HTTP calls to prove the cache served page 2 from memory rather than refetching.
- Fixtures exercise the mapping code paths they claim to: `pulls_closed_p1.json` includes one PR with `merged_at: null` specifically to test the "closed without merge" filter path (`list.go:59`); `pull_421.json` vs. the list fixtures deliberately differ (`merge_commit_sha`/`commits` only on the single-PR fixture) to verify `ListMergedChanges` results correctly leave `MergeCommitSHA` empty (`client_test.go:375-377`) while `GetChange` populates it.
- Not table-driven (`tests := []struct{...}` + `t.Run`) — each scenario is its own `Test*` function. This deviates from the testing-guide's "prefer table-driven tests" guideline, but the test code is verbatim-mandated by the brief (task-5-brief.md lines 76-302) and each function already covers a fairly cohesive scenario. Flagged as Minor, not Important, since it doesn't reduce coverage or hide bugs.

## Strengths
- Constructor, constants, and interface conformance match the brief exactly, byte-for-byte in the values that matter (`client.go:21,24,26,28,49,188`).
- Consistent, disciplined reuse of existing shared infrastructure — `DoJSON`, sentinel errors, immutable builders, `CredentialEnv`, and `gitx.Validate*` — with no reimplementation anywhere in the diff.
- No log statements at all in this package, which by construction eliminates the "token/header leaked into logs" risk class entirely.
- `MaxScanPages` and `MaxPRCommits` bounds are both real, pre-fetch/post-append checks, not decorative constants — genuinely close off the unbounded-work risk the brief calls out.
- Fixtures are deliberately constructed (not copy-pasted filler) to exercise specific mapping branches: unmerged-PR filtering, list-vs-single-PR field asymmetry, cache-hit counting.

## Issues

### Critical (Must Fix)
- **Data race on `scanEntry` fields under concurrent `ListMergedChanges` calls for the same `(repo, target)` key.** `ensureScanned` releases `c.mu` before returning the `*scanEntry` (`list.go:31-75`), and the caller then reads `entry.items` (`list.go:106`) and `entry.nextPage`/`entry.capped` (`list.go:121`) without holding any lock, while a concurrent call for the same key can be mutating those same fields under the lock at that moment. Not caught by the existing tests because none of them issue concurrent calls (`client_test.go:364-413`). This directly contradicts the documented usage contract that `ListMergedChanges` may be called concurrently.

### Important (Should Fix)
- **Client-wide mutex held across network I/O** (`list.go:33-34` wrapping the `c.get` call at `list.go:54`) serializes scan work for *every* repo/target pair on one `Client`, not just the one being fetched — a design fragility that will cause needless request pileups under the concurrent-call pattern the brief anticipates. Related to, but distinct from, the Critical race above; fixing the race (e.g., via a per-key lock) would likely also fix this.

### Minor (Nice to Have)
- Tests are not table-driven per the testing-guide's general preference (`.claude/skills/backend-dev-guidelines/resources/testing-guide.md`, "Guidelines: Prefer table-driven tests"), though this is brief-mandated test code and each function is a coherent scenario rather than a repetitive case list — low-value to restructure.

## Assessment
**Task quality:** Needs fixes
**Reasoning:** Spec compliance is clean and the security-sensitive token/credential handling is correctly reused from shared infrastructure with no leakage paths found, but the merged-PR scan cache has a genuine, brief-relevant data race (unsynchronized reads of `scanEntry` fields after the mutex protecting their writes has been released) that the reported `-race`-clean test run cannot have caught, since no test exercises concurrent `ListMergedChanges` calls.
