# Audit — Task 22: Frontend API types, fetch client and service layer

Commit reviewed: `18ae128` (`feat(task-001): frontend API types, fetch client and service layer`), 16 files, 643 insertions.

## Verdicts

- **Spec compliance:** ✅ Compliant. All produced interfaces, files, and behaviors from `task-22-brief.md` are present; the two declared deviations are both correct and better than the brief's literal sample code.
- **Task quality:** **Approved.**

## Build & test verification (independent, not taken from the report)

- `npm ci` in `apps/frontend` — clean, 688 packages.
- `npx vitest run` — **3 files, 12 tests, all passing** (2 pre-existing tests in `src/lib/__tests__/utils.test.ts` unrelated to this task, 5 in `client.test.ts`, 5 in `reviews.test.ts`).
- `npm run lint` (`eslint .`) — clean, no output/errors.
- `npx tsc --noEmit -p tsconfig.app.json` — clean.
- `npm run build` (`tsc -b && vite build`) — succeeded, wrote `apps/backend/internal/ui/dist/*`; restored the tracked `.gitkeep` afterward (`git checkout -- apps/backend/internal/ui/dist/.gitkeep`), confirmed `git status --porcelain` clean on that directory again.
- Backend gate re-run per the "build repopulates dist" constraint: `go build ./...` and `go vet ./...` from `apps/backend` — both clean.

**Discrepancy found (Minor):** the report claims "After adding guard tests: 3 files, 14 tests, all passing." Actual, independently measured: **12 tests**, not 14 (report §"Testing"). This is a report-accuracy defect, not a code defect — the test suite itself is correct and exhaustive for what it covers. Evidence: `npx vitest run --reporter=verbose` output enumerating all 12 test names, matching exactly the 5 in `apps/frontend/src/lib/api/__tests__/client.test.ts`, 5 in `apps/frontend/src/services/api/__tests__/reviews.test.ts`, and 2 pre-existing in `apps/frontend/src/lib/__tests__/utils.test.ts`.

## Mutation re-verification (independent, scratch copy at `/tmp/audit22-scratch`, outside the worktree, deleted after)

All three of the implementer's mutations were reproduced from scratch (not accepted from its table) with node_modules symlinked in, each applied, grepped to confirm it landed on the claimed line, run, and reverted before the next:

| # | Mutation | Grep proof (mine) | Test run | Assertion (quoted, mine) | Matches report? |
|---|---|---|---|---|---|
| 1 | `unwrapList`: line 52 `throw ...` → `return [];` | `grep -n "return \[\];" src/types/api/jsonapi.ts` → `52:    return [];` | `reviews.test.ts` → 1 failed / 4 passed | `AssertionError: promise resolved "[]" instead of rejecting` | Yes, exact match |
| 2 | `unwrapOne`: body reduced to `return doc.data;` | body diff shows only `return doc.data;` remaining | `reviews.test.ts` → 1 failed / 4 passed | `AssertionError: promise resolved "undefined" instead of rejecting` | Yes, exact match |
| 3 | `client.ts:19` `"UNKNOWN"` → `""` | `grep -n '""' client.ts` → `19:  return new ApiError(response.status, "", response.statusText \|\| "Request failed", "");` | `client.test.ts` → 1 failed / 4 passed | `AssertionError: expected '' to be 'UNKNOWN'` | Yes, exact match |

Reachability confirmed before crediting protection: `unwrapList` is called from `providers.ts:8`, `repositories.ts:32`, `changes.ts:27`, `reviews.ts:20,26`; `unwrapOne` is called from `repositories.ts:41`, `reviews.ts:10,16,32`. Only `reviewsService.list` and `reviewsService.get` are exercised by a malformed-response test, but the mutated code is the single shared `unwrapList`/`unwrapOne` implementation used by every call site, so the fix is verified for all of them transitively — no service method bypasses the guard (confirmed by reading `apps/frontend/src/services/api/{providers,repositories,changes,reviews}.ts` in the diff: every `doc.data` access outside a raw pass-through goes through one of the two guard functions; there is no bare `return doc.data` or `doc.meta?.page ? { items: doc.data, ...}` left anywhere — `repositories.ts` and `changes.ts` both bind `unwrapList(doc)` to `items` before building the paged result, e.g. `apps/frontend/src/services/api/changes.ts:26-28`).

## Adjudication of the two declared deviations

1. **`unwrapList`/`unwrapOne` guards (`apps/frontend/src/types/api/jsonapi.ts:51-64`).** Correct and necessary. Without them, `providersService.list()` on a 2xx body missing `data` would return `[]` (Contract 7's "empty list" defect verbatim), and `reviewsService.get()` on a missing-`data` 2xx would return `undefined` typed as `Review`. The brief's own signatures (`Promise<Provider[]>`, `Promise<Review>`, etc.) are unchanged — only the internal unwrap step changed, which is within the brief's tolerance ("not a deviation from prose/interfaces"). Verified every service file routes through them (see reachability check above), and the two mutation tests exercise the exact failure modes described.

2. **`ReviewFileAttributes.previousPath: string` (not `string | null`) (`apps/frontend/src/types/models/reviewFile.ts:8-12`).** Independently confirmed against `apps/backend/internal/api/review_files.go:13`: `PreviousPath string \`json:"previousPath"\`` — non-pointer, no `omitempty`, so it marshals as `""` and never as `null` or absent. The frontend type is correctly narrowed to match. Handling consistency check: grepped the entire diff for any `previousPath` null-check — there is none; the only places `previousPath` is read are in the test fixtures (`apps/frontend/src/services/api/__tests__/reviews.test.ts`, both instances now `previousPath: ""`, not `null`), which the implementer updated in lockstep with the type change. No code anywhere in this diff writes `=== null` or `?? null` against `previousPath`, so there is no leftover null-check hiding behind the corrected type.

## Contract checks

- **No `INTERNAL` code.** Grepped the full frontend diff and the backend `api` package — zero introductions. `client.ts:19` uses a frontend-local sentinel `"UNKNOWN"` for the case where the server's error body isn't a valid JSON:API error document at all (e.g., an HTML 502 page); this is explicitly required by the brief's own test (`client.test.ts`, "handles error responses that are not JSON:API documents" asserts `error.code` to be `"UNKNOWN"`) and by the backend's own domain-error mapping never emitting a code the client needs to invent — `UNKNOWN` is a client-side "we truly don't know" label, not a claim about a backend contract code, and is not asserted anywhere as if it were one.
- **Domain code set.** `INVALID_REQUEST`, `NOT_FOUND`, `NOT_ACCEPTABLE`, `INVALID_STATE`, `REVIEW_NOT_READY` all confirmed present verbatim in `apps/backend/internal/api/{errors,repositories,changes,middleware,reviews,router}.go`; the frontend layer is code-agnostic (`ApiError.code` just carries whatever string arrives), so no frontend code needed to enumerate or validate the set — correct choice for this layer.
- **Route/shape parity with backend.** Spot-checked `apps/backend/internal/api/router.go:59-75` against every service call site (`providers.ts`, `repositories.ts`, `changes.ts`, `reviews.ts`) — paths, HTTP methods, and the `{path...}` wildcard segment for `getReviewFile` all line up with `reviewsService.fileDiff`'s `path.split("/").map(encodeURIComponent).join("/")` construction.
- One report overstatement, immaterial to correctness (Minor): the report says `state=merged` is "required by the handler ... (`state != "" && state != "merged"` → 400)". Reading `apps/backend/internal/api/changes.go:57-58` shows `state=""` (omitted) is accepted too — the handler rejects only a *present-and-different* value, so `state=merged` is a safe default the frontend chose to always send, not something the backend strictly requires. Doesn't affect behavior since the frontend does send it.

## Anti-pattern / code-quality spot checks

- `grep -rn ": any\|as any"` across `types/api`, `types/models`, `lib/api`, `services/api`, `test/server.ts` — zero matches.
- No default exports in any new file; `providersService`/`repositoriesService`/`changesService`/`reviewsService` are all named `export const`.
- `errorDoc` (`apps/frontend/src/test/server.ts:22-24`) is exported but has zero call sites in this commit — matches the report's own disclosure that it's unused pending Tasks 23–26. Not a defect; flagged for the next task's reviewer to confirm it gets used or removed.

## Summary

### Blocking (must fix)
- None.

### Non-blocking (should fix / note for follow-up)
- Report's test count ("14 tests") does not match the actual suite (12 tests) — cosmetic, but the next report should get this right since counts are being used as evidence in this review chain.
- Report's characterization of `state=merged` as backend-required is slightly stronger than the code actually enforces (omission is also legal); no action needed, just don't cite it as a hard requirement in future briefs.
- `errorDoc` in `src/test/server.ts` is currently dead code — carry a note into Task 23-26 review to confirm it gets exercised.
