# Task 24 Audit — Shared components, provider/repository selection

Commit reviewed: `76eccf9` (11 files, 605 insertions, matches `git show --stat 76eccf9` / the diff package stat).

## Verdicts

- **Spec compliance:** ✅ PASS — every produced interface in `task-24-brief.md` ("Interfaces" section) is present with matching signatures (`PageHeader`, `EmptyState`, `ErrorBanner`, `Pagination`, `ProviderPicker`, `RepositoryList`, `repositorySchema`/`RepositoryFormData`, `ManualRepositoryForm`, `SelectRepositoryPage`). The four common components (`PageHeader.tsx`, `EmptyState.tsx`, `ErrorBanner.tsx`, `Pagination.tsx`) are byte-for-byte the brief's Step 2 sample. `RepositoryList.tsx` and `ManualRepositoryForm.tsx` are functionally identical to the brief's Step 3 sample (only Prettier multi-line JSX/import wrapping differs). The two declared deviations (`ProviderPicker`'s conditional `value` spread, `SelectRepositoryPage`'s derive-during-render instead of `useEffect`) are both verified real lint/type-check failures on the brief's literal sample, not invented busywork — see below.
- **Task quality:** **Approved**, with one Important finding (schema/backend validator divergence in the *opposite* direction than the one the report worried about) and two Minor findings (no dedicated schema unit test; `RepositoryList`/`ProviderPicker` have no error-prop guard of their own, relying entirely on caller discipline).

## Build & test gate (re-run by reviewer, not taken from the report)

- `npx vitest run --reporter=verbose` (full suite): **10 test files, 44 tests, all passed.** Matches the report's "44 tests passed, 10 files" claim exactly — verified by direct run, not by trusting the report's own admittedly confused arithmetic paragraph.
- `npm run lint` (`eslint .`): clean, 0 output.
- `npx tsc --noEmit -p tsconfig.app.json`: clean, exit 0.
- `npm run build`: succeeded, wiped `apps/backend/internal/ui/dist/.gitkeep` as expected; restored via `git checkout -- apps/backend/internal/ui/dist/.gitkeep`; `git status --porcelain` on that path is clean after restore.

## The four adjudication items

### 1. `ManualRepositoryForm` calling `repositoriesService.get` directly (ruled ACCEPTED — verifying the ruling's two dependencies)

`apps/frontend/src/components/features/repositories/ManualRepositoryForm.tsx:36-42`:
```ts
try {
  onResolved(await repositoriesService.get(providerId, values.repository));
} catch (error: unknown) {
  setServerError(messageFor(error, "That repository could not be read."));
} finally {
  setChecking(false);
}
```
- The `await` is inside a `try` whose `catch` calls `setServerError(messageFor(...))`, which is rendered at line 62-64 (`{serverError ? <p className="text-sm text-destructive">{serverError}</p> : null}`). Failure is surfaced to the user, not silently swallowed. `finally` unconditionally resets `checking`, so the button never gets stuck in a pending state. Confirmed by test `ManualRepositoryForm.test.tsx:52-74` ("shows the API message when the repository is not found") passing against the real component.
- Nothing downstream assumes the resolved repository is already in the React Query cache: `grep -n "queryClient\|setQueryData\|useRepository(" apps/frontend/src/pages/SelectRepositoryPage.tsx apps/frontend/src/components/features/repositories/ManualRepositoryForm.tsx` returns no matches. `goToChanges` (`SelectRepositoryPage.tsx:25-29`) only reads `repository.id` and calls `navigate`; it never re-queries `useRepository` for the same key. Ruling's dependencies hold.

### 2. Zod schema vs. backend validator (ruled ACCEPTED against FR-3.7 — verifying it isn't *stricter* than the backend)

`apps/frontend/src/lib/schemas/repository.ts:4-11` vs. `apps/backend/internal/gitx/validate.go:19-42` (`ValidateRepoFullName`):
- Both use the identical top-level regex `^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`.
- Both reject a string containing `..` and a string starting with `/`, `-`, or `.`.
- **Divergence found, in the permissive direction, not the strict one:** the backend additionally rejects any *individual path segment* equal to `.` or `..`, or containing an empty segment (`validate.go:38-40`, looping `strings.Split(s, "/")`). The frontend's `.refine` only inspects the *whole string* for a leading `-./` or for `..` anywhere — it does not check interior segments. Example: `"owner/./name"` passes the frontend `repositorySchema` (no leading bad char, no literal `..` substring) but is rejected by `ValidateRepoFullName` (segment `.` is invalid). This means the frontend is **less** strict than the backend for this one edge case, not more — so it does not create the "reject something the backend would accept" capability loss the ruling worried about, but it does mean a user can pass client-side validation and still get a 400 from the API for `owner/./name`-shaped input. **Important, not Critical**: no capability is lost, but the client/server validation contract has a real gap.
- No dedicated test file exists for `repositorySchema` (`find apps/frontend/src/lib/schemas -type f` returns only `repository.ts`, no `__tests__`), so this gap is not caught by any test in the diff — flagged as a **Minor** testing gap in addition to the Important validation-parity finding above, since the audit brief's "test file is the ground truth" instruction has no test file to be the ground truth.

### 3. Two claimed lint/type-check-forced deviations — reproduced independently, not read-and-trusted

- **`react-hooks/set-state-in-effect` on the brief's `useEffect` sample:** reverted `SelectRepositoryPage.tsx` to the brief's literal Step 3 code in a scratch copy (`/tmp/task24-scratch/frontend`, outside the worktree) and ran `npx eslint`. Reproduced exactly: `error Error: Calling setState synchronously within an effect can trigger cascading renders ... react-hooks/set-state-in-effect` at the `setProviderId(first.id)` line. Claim is real.
- **`TS2375` on `<Select value={value} ...>` with `exactOptionalPropertyTypes`:** confirmed `tsconfig.app.json:24` sets `"exactOptionalPropertyTypes": true`. Reverted `ProviderPicker.tsx` to the brief's literal `<Select value={value} onValueChange={onChange}>` in the same scratch copy and ran `npx tsc --noEmit -p tsconfig.app.json`. Reproduced exactly: `error TS2375: Type '{ ...; value: string | undefined; ... }' is not assignable to type '{ value?: string; ... }' with 'exactOptionalPropertyTypes: true'`. Claim is real.
- **Replacement correctness — the derive-during-render reset:** `apps/frontend/src/pages/SelectRepositoryPage.tsx:16-21`:
  ```ts
  const [selectedProviderId, setSelectedProviderId] = useState<string | undefined>(undefined);
  const providers = useProviders();
  const providerId = selectedProviderId ?? providers.data?.[0]?.id;
  ```
  This is a pure derivation recomputed every render — it has no state to go stale, so there is no "reset at the right time" bug class to check for (unlike a synced-copy pattern, there's nothing to desync). When the user explicitly picks a provider, `ProviderPicker`'s `onChange` (`SelectRepositoryPage.tsx:47-50`) sets `selectedProviderId` and resets `page` to 1, matching the brief's original behavior. Confirmed correct.
- Confirmed via `git status --porcelain apps/frontend/src/pages/SelectRepositoryPage.tsx apps/frontend/src/components/features/providers/ProviderPicker.tsx` (empty) and `diff` against the scratch copy that all mutation/reversion work happened only in `/tmp/task24-scratch`, never in the worktree.

### 4. Two tests added beyond the brief's Step 1

- `"surfaces repository failures in an error banner rather than an empty state"` (`SelectRepositoryPage.test.tsx:99-121`) — asserts both the presence of the error detail text and, critically, `expect(screen.queryByText(/no repositories/i)).not.toBeInTheDocument()`. This is a genuine constraint: it fails if a future change collapses the error and empty branches (exactly the defect class the audit brief calls out). Mutation 1 below proves it actually catches that regression.
- `"retries the repository fetch when Try again is pressed after a failure"` (`SelectRepositoryPage.test.tsx:123-164`) — asserts the endpoint is hit exactly twice (`calls === 2`) and the row renders after retry. Not timing-dependent (uses MSW request counting and `findByText`, no fake timers/`sleep`). Mutation 2 below proves it catches an `onRetry` no-op regression. Both tests genuinely constrain rather than decorate.

## Independent verification of loading/error/empty separation (the defect-class check)

- **`SelectRepositoryPage.tsx:54-74`**: `repositories.isError ? <ErrorBanner .../> : (<><RepositoryList .../><Pagination .../></>)` — an exhaustive if/else. `RepositoryList` (and therefore its own `EmptyState` fallback) is structurally unreachable while `repositories.isError` is true. Confirmed by reading the JSX, not inferred.
- **`ProviderPicker.tsx:25-27`**: has no error prop or branch of its own — loading renders a `Skeleton`, otherwise the `Select`. It does not distinguish "provider list failed to load" from "provider list is empty," but it doesn't need to: `SelectRepositoryPage.tsx:37-43` renders the providers `ErrorBanner` independently, above the picker, whenever `providers.isError`. **Minor**: `ProviderPicker` still renders (with an empty list) underneath that banner when providers fail, rather than being suppressed — it's not mislabeled as "no providers," just an empty, non-actionable `Select`. Low severity since the primary error signal (the banner) is present and correctly triggered, but the component itself provides no defense if a future caller omits the banner.
- **`RepositoryList.tsx:20-37`**: same shape — `loading` → skeleton rows, `repositories.length === 0` → `EmptyState`, otherwise the table. It has **no `error` prop at all**, so in isolation, a caller that (incorrectly) passes `repositories={[]}` during an error condition would get a false "No repositories" — this is exactly the defect class named in the audit brief, and it is only avoided because `SelectRepositoryPage` never calls `RepositoryList` in that state (confirmed above). **Minor finding**: the component's own API surface offers no guard; the correctness is 100% caller-discipline today. This matches the brief's specified interface (`RepositoryList({ repositories, loading, onSelect })`, no `error` param), so it is not a deviation from spec, just an architectural fragility worth naming for future callers of these two components.

## Mutation re-run (Contract 6)

Both mutations applied only in `/tmp/task24-scratch/frontend` (a `cp -a` of `apps/frontend`, outside the worktree). Confirmed worktree untouched after each run (`git status --porcelain` on the two touched files was empty both times).

| # | Mutation | Grep proof | Command | Result | Assertion text (quoted) |
|---|---|---|---|---|---|
| 1 | `SelectRepositoryPage.tsx:54` `{repositories.isError ? (` → `{false && repositories.isError ? (` | `grep -n "repositories.isError" src/pages/SelectRepositoryPage.tsx` → `54:      {false && repositories.isError ? (` | `npx tsc --noEmit -p tsconfig.app.json` (clean, exit 0) then `npx vitest run src/pages/__tests__/SelectRepositoryPage.test.tsx` | **2 of 5 tests FAILED**, 3 passed | `TestingLibraryElementError: Unable to find an element with the text: /repository host did not respond/i.` — reproduced verbatim, matches report |
| 2 | `SelectRepositoryPage.tsx:58` `onRetry={() => void repositories.refetch()}` → `onRetry={() => {}}` | `grep -n "onRetry=" src/pages/SelectRepositoryPage.tsx` → line 41 (providers) unchanged, line 58 (repositories) is `onRetry={() => {}}` | `npx tsc --noEmit` (clean) then `npx vitest run src/pages/__tests__/SelectRepositoryPage.test.tsx` | **1 of 5 tests FAILED**, 4 passed | Assertion at `SelectRepositoryPage.test.tsx:162`, `expect(await screen.findByText("atlas/server"))` — element never appears; test times out on the `findByText` query, matching the report's cited line |

Ratios match the report's claims exactly (2/5 and 1/5 failing respectively, both mutants compiled and ran cleanly, no compile errors — reachability confirmed by the pre-mutation baseline run at 44/44 passing).

**Mutation coverage ratio is thin for an 11-file UI diff.** Both mandated mutations target the same file (`SelectRepositoryPage.tsx`) and the same defect class (repositories-query error handling). Neither the implementer nor this re-run exercised a mutation against: `ProviderPicker.tsx`'s loading/Select branch, `RepositoryList.tsx`'s own empty-vs-loading branch order, `Pagination.tsx`'s `hasNext`/`disabled` gating (e.g., mutating `disabled={disabled || !hasNext}` to `disabled={disabled}` would let a user page past the last page — untested), or `ManualRepositoryForm.tsx`'s `finally { setChecking(false) }` (removing it would leave the submit button permanently disabled after an error — untested). This mirrors the concern raised on Task 23 (seven uncovered hooks): **two mandated mutations is a thin ratio here too**, though the two that were run do hit the specific "worst defect" the brief called out, and they pass with genuine assertion failures.

## Summary

### Blocking (must fix)
- None. Build, lint, typecheck, and all 44 tests pass; the four brief interfaces are implemented as specified; both claimed lint/type-check-forced deviations are real and correctly resolved; the two adjudicated concerns hold up under direct verification.

### Non-Blocking (should fix)
- **Important** — `repository.ts:4-11` accepts `owner/./name`-shaped input that `apps/backend/internal/gitx/validate.go:38-40` rejects (interior `.`/`..` segments aren't checked by the frontend refine, only the leading character and a whole-string `..` substring check). Not a capability loss, but a client/server validation gap that will surface as an unnecessary 400 for a small set of malformed-but-client-passing inputs. Recommend tightening the `refine` to check each `/`-split segment, matching the backend loop exactly.
- **Minor** — No dedicated unit test exists for `repositorySchema` (no file under `apps/frontend/src/lib/schemas/__tests__`); the regex/refine edge cases (including the gap above) are unverified by any test in this diff.
- **Minor** — `RepositoryList` and `ProviderPicker` have no `error` prop of their own; the loading/error/empty distinction for both is enforced entirely by `SelectRepositoryPage`'s exhaustive ternaries, not by the components themselves. Correct today, but fragile for future callers who might reuse these components without replicating the same discipline. Matches the brief's specified interfaces, so not a deviation — just worth naming.
- **Minor** — Mutation coverage (2 mutations, both on the same file/defect-class) is thin relative to the diff's size; `Pagination`'s `hasNext`/`disabled` gating and `ManualRepositoryForm`'s `finally` reset are untested by mutation.
