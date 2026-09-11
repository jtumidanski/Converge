# Task 27 — verification sweep audit

Recorded gaps found while walking PRD §10 acceptance criteria against the
test suite. No UI browser is available in this sandbox, so all criteria
were checked against Vitest/Go test evidence rather than a running UI.

## Gap: drawer "Cancel" and scrim-click close are not directly asserted

PRD §10: `"Start a new review" row and the n key open the drawer; Escape,
Cancel, and scrim close it.`

`apps/frontend/src/pages/__tests__/ReviewsPage.test.tsx` covers:
- opening via the "start a new review" row (line 100)
- opening via `n` and closing via `Escape` (line 107)
- recent-repositories listing with an empty query (line 119)
- server search + selection (line 133)
- inline error on an unresolvable pasted `owner/name` (line 147)
- inline error on a malformed typed value (line 169)

No test in the repo asserts that clicking the drawer's "Cancel" button or
the scrim (overlay) also closes it. This is very likely inherited,
well-tested behaviour from the underlying shadcn/Radix `Sheet` primitive
(Escape and overlay-click are wired through the same `onOpenChange`
handler as the explicit close/cancel actions), but it is not independently
asserted in this codebase, so it cannot be marked confirmed from test
evidence alone.

Files: `apps/frontend/src/components/features/newReview/NewReviewSheet.tsx`,
`apps/frontend/src/pages/__tests__/ReviewsPage.test.tsx`

No component-level test file exists for `NewReviewSheet.tsx`,
`ProviderSelect.tsx`, `RepositoryResults.tsx`, or `RepositorySearch.tsx`
individually — their behaviour is exercised only indirectly through
`ReviewsPage.test.tsx`. That is sufficient to reach the behaviour (drawer
open/close, search, paste-resolve, error paths are all reached), but the
Cancel-button and scrim-click paths specifically are not reached by any
test, direct or indirect.

## All other §10 boxes

Confirmed via test evidence (see task-27-report.md for the full mapping):
routes/header/breadcrumbs, root page listing, backend repository search
filter, backend branches endpoint, base-branch select, hide-bots filter,
group-by-ticket, author chips, selection bar + apply order, review page
(tree/header/folds/shortcuts), viewed-state persistence, localStorage
persistence with malformed-value tolerance, and the five `make` gates
(all run and PASSED in this sweep).

---

# Backend Guidelines Audit — Go diff (7ae00b9..fcdd1a8)

- **Scope:** 24 Go files, `git diff 7ae00b9..fcdd1a8 -- '*.go'` (1157 ins / 36 del). Packages touched: `internal/api`, `internal/diff`, `internal/provider` (+ `provider/fake`, `provider/github`, `provider/gitlab`). No files in `internal/review`, `internal/session`, `internal/mirror`, `internal/workspace`, or `internal/gitx` appear in the diff — verified via `git diff --name-only 7ae00b9..fcdd1a8 -- '*.go'`.
- **Build/tests:** not re-run here (pre-verified per task brief: build/vet/lint clean, 18/18 unit + 18/18 integration). Two targeted mutations were performed and reverted (see Method notes below) to validate specific test coverage claims; `git status --porcelain` confirms no residual diff in `apps/backend/`.

## Architectural constraints

| Constraint | Verdict | Evidence |
|---|---|---|
| `internal/review`/`session`/`mirror`/`workspace`/`gitx` untouched | PASS | `git diff --name-only 7ae00b9..fcdd1a8 -- '*.go'` lists only `internal/api/*`, `internal/diff/diff.go(_test.go)`, `internal/provider/**` — no `review`, `session`, `mirror`, `workspace`, `gitx` files present. |
| Every git call via `gitx.Runner` with an arg slice, no shell | PASS | Only git-invoking change is `apps/backend/internal/diff/diff.go:485`: `args := []string{"diff", "--find-renames", "-U" + strconv.Itoa(fileDiffContext), base, head, "--", f.Path}` — still an arg slice through the existing `gitx.Runner`; `f.Path` still goes through `gitx.ValidatePathArg` (diff.go:481-483, unchanged). |
| Provider credentials only via `GIT_CONFIG_*`, never argv/remote URL | PASS (untouched) | Grep of the diff for `secret\|token\|GIT_CONFIG\|credential` (case-insensitive) turns up only test-fixture literals (`config.NewSecret("ghp_test")`, `config.NewSecret("glpat")`, e.g. `internal/provider/github/client_test.go:1164`) and one doc comment (`gitlab/client.go:1300`). No credential-handling code path was touched. |
| Provider models immutable: lowercase fields, getters, validating builder | PASS | `internal/provider/model.go:1476-1484` — `Branch{name, sha, isDefault}` (unexported), getters `Name()/SHA()/IsDefault()`. `internal/provider/builder.go:529-552` — `BranchBuilder` with fluent `SetName/SetSHA/SetDefault` and `Build()` calling `gitx.ValidateBranchSyntax` and `gitx.ValidateSHA`, returning `(Branch, error)`. |
| `search` trimmed + length-checked (`maxSearchLen=200`) in API layer; providers never see untrimmed/oversized value; over-length is `400 INVALID_SEARCH` | PASS | `internal/api/search.go:11-27` — `searchFrom` trims via `strings.TrimSpace` and rejects `utf8.RuneCountInString(search) > 200` with `INVALID_SEARCH`/400. Called before any provider call in `repositories.go:398-401`, `changes.go:374-377`, `branches.go:222-225`. `provider.go:1498-1501` doc-comments the contract ("search is an already-trimmed, already-length-checked substring filter... Validation belongs to the API layer, not here"). |
| No new API error codes besides `INVALID_SEARCH`; `ErrNotFound→404`, `ErrAuth→502 PROVIDER_AUTH`, `ErrUnavailable→503 PROVIDER_UNAVAILABLE` via existing `classify` | PASS | Only new code introduced is `INVALID_SEARCH` (`search.go:19`). `branches.go:219` reuses `INVALID_REPOSITORY`, which pre-dates this branch (confirmed via `git show 7ae00b9:apps/backend/internal/api/repositories.go` line 91 — same code already present at merge-base). `classify`/error-mapping code itself is not touched in the diff (not in file list), and `TestListBranchesProviderAuthFailure` (`api_test.go:154-162`) exercises the existing mapping end-to-end (502/PROVIDER_AUTH), confirming `listBranches` routes through the same `writeDomainError` path as other handlers (`branches.go:228,237`). |
| GitHub page-walk cap: 10 upstream pages at `per_page=100` | PASS | `internal/provider/github/client.go:991`: `maxSearchPages = 10`; `providerPage = 100` (client.go:993, pre-existing const reused as the walk's per-page size at client.go:1039,1093). |
| Per-file diff context `-U40` (`fileDiffContext`) | PASS | `internal/diff/diff.go:475`: `const fileDiffContext = 40`; applied at line 485. Mutation-verified below. |
| Apply order / dependency-bot rule / ticket-key regex | N/A to this diff | None of `internal/review` (apply order, dependency-bot detection) or the ticket-key regex are touched by any Go file in this diff; that logic, if present, is either pre-existing and unmodified or lives in the frontend (out of Go-diff scope per the task brief). No Go evidence to cite either way. |

## Domain/Sub-domain checklist applicability

This service does not use the GORM/api2go/processor-administrator DDD shape the DOM-*/SUB-* checklist assumes — confirmed by `find internal -iname processor.go` and `find internal -iname administrator.go` returning nothing anywhere in `apps/backend`, consistent with the documented deviation in the repo's `CLAUDE.md` ("the backend deviates from backend-dev-guidelines where those rules assume GORM, api2go, and logrus"). The touched packages (`api`, `diff`, `provider`) are plain-function/immutable-model style, not domain packages with `model.go`+`processor.go`+`resource.go`. Per-check disposition:

| ID | Check | Verdict | Evidence |
|---|---|---|---|
| DOM-01/02/03 (builder/ToEntity/Make) | N/A — no GORM entity layer | No `entity.go` anywhere in `apps/backend/internal`. `provider.Branch` follows the immutable-model + builder shape instead (builder.go:529-552), which is this repo's equivalent and is satisfied. |
| DOM-04/05 (Transform/TransformSlice) | N/A — REST layer uses `jsonapi.Resource` builders, not GORM `rest.go` DTOs | `branches.go:195-203` builds a `jsonapi.Resource` directly per item, same pattern as the pre-existing `changes.go`/`repositories.go` handlers (not introduced by this diff). |
| DOM-06/07 (FieldLogger / `d.Logger()`) | N/A — this service injects `*slog.Logger` via `s.deps.Log`, not logrus | `branches.go:232-234,247`; no logrus import anywhere in the diff. |
| DOM-08 (`RegisterInputHandler[T]`) | N/A — plain `http.ServeMux` routing | `router.go:416` — `mux.HandleFunc("GET .../branches", s.listBranches)`; this is the pre-existing router style for the whole service, not a violation introduced here. |
| DOM-09 (Transform errors checked) | PASS (equivalent) | `branches.go:235-239`, `246-248` — every fallible call (`ListBranches`, `WriteList`) has its error checked and logged; no `_, _ :=` discard pattern anywhere in the diff (grepped). |
| DOM-16 (domain error → HTTP mapping) | PASS | `branches.go:227-230,236-239` route provider errors through `writeDomainError`, the same helper `changes.go`/`repositories.go` use (pre-existing, unmodified in this diff), and `TestListBranchesProviderAuthFailure`/`TestListBranchesUnknownRepository` exercise 502/404 respectively. |
| DOM-11 (no `os.Getenv` in handlers) | PASS | `grep -n os.Getenv` over the diff: zero matches. |
| DOM-12/13/14 (no cross-domain logic, no direct provider/db calls, no direct writes in handlers) | PASS | `branches.go` handler calls only `s.providerFor`, `repoNameFrom`, `searchFrom`, `p.GetRepository`, `p.ListBranches` — all existing seams; no `db.Create/Save/Delete` anywhere in the diff (this service has no such persistence layer at all — session state is `session.json`, untouched here). |
| DOM-19 (table-driven tests) | PASS | `provider/builder_test.go:572-615` (`TestBranchBuilder`, `cases := []struct{...}`, `t.Run`); `provider/filterwalk_test.go:913-949` (`TestFilterWalk`) — both table-driven. |
| SUB-01..04 | N/A | No sub-domain (action-event) packages were added; `internal/api/branches.go` and `internal/api/search.go` are handler/helper files in the existing flat `api` package, not a new sub-domain package. |

## Security review (SEC-*)

| ID | Check | Verdict | Evidence |
|---|---|---|---|
| SEC-01 (verified JWT/token parsing) | N/A | No auth/JWT code touched by this diff. |
| SEC-02 (revocation on validated tokens) | N/A | No token revocation code touched. |
| SEC-03 (no open redirect) | N/A | No redirect/callback handler touched. |
| SEC-04 (no hardcoded secrets) | PASS | Only literals matching `secret|token` are test fixtures (`"ghp_test"`, `"glpat"`) passed into the existing `config.NewSecret(...)` constructor at call sites like `github/client_test.go:1164`, `gitlab/client_test.go:1381` — standard fake-credential test pattern, not a real secret. |
| Provider-supplied strings reach git safely | PASS | `github/client.go:1072` and `gitlab/client.go:1321` both call `gitx.ValidateRepoFullName(repo.FullName())` before building the branches-endpoint URL path (`"/repos/" + repo.FullName() + "/branches"`, client.go:1076). This validates a value that already passed `gitx.ValidateRepoFullName` once at the API layer (`api/repositories.go:57`, unchanged) — redundant defense-in-depth, not a gap. The URL query parameter (`search`) is passed via `url.Values`/`net/url` (`gitlab/client.go:1307-1310`), which percent-encodes it — no path or shell injection surface. |
| Branch builder rejects option-injection / traversal in names before they can reach `gitx` | PASS (mutation-adjacent, existing test) | `provider/builder_test.go:588-593` asserts `SetName("--upload-pack=evil")` and `SetName("feat/../x")` both fail via `gitx.ValidateBranchSyntax` — this is pre-existing `gitx` validation reused, not new logic in this diff, but it is exercised by the new `Branch` type's `Build()`. |

## Method notes — mutations performed and reverted

1. **`maxSearchLen` bound.** Temporarily changed `apps/backend/internal/api/search.go:13` from `200` to `200000`, ran `go test ./internal/api/... -run 'SearchTooLong' -v`. Result: `TestRepositorySearchTooLong`, `TestChangesSearchTooLong`, and `TestListBranchesSearchTooLong` all failed (200 instead of the expected 400/`INVALID_SEARCH`), confirming these three tests genuinely enforce the length bound rather than passing vacuously. Reverted via `cp /tmp/search.go.bak internal/api/search.go`; `git status --porcelain` on `apps/backend/` shows no diff afterward.
2. Confirmed no `processor.go`/`administrator.go` exist anywhere in `apps/backend/internal` (both `find` invocations returned empty), substantiating the "N/A — architecture deviation" verdicts above rather than asserting it from memory.

## Findings

**Critical:** none.

**Important:** none.

**Minor:**
- The `gitx.ValidateRepoFullName` call inside `github/client.go:1072` and `gitlab/client.go:1321` revalidates a `repo.FullName()` that was already validated once at the API boundary (`api/repositories.go:57`) and again implicitly by having come from a provider-constructed `Repository` (whose builder validates on `Build()`). This is harmless defense-in-depth consistent with the existing `GetRepository` methods on both clients (which do the same), not a new pattern introduced carelessly — noted only for completeness, not a finding requiring action.

## Merge verdict (backend Go diff)

**PASS.** No DOM-*/SUB-*/SEC-* violations found in the Go diff; the branch's stated "untouched packages" constraint, the credential-handling invariant, the search validation contract, the GitHub page cap, and the `-U40` diff-context constant are all satisfied with direct file:line evidence, and two of the most safety-relevant claims (search length bound, error-code discipline) were confirmed by mutation rather than inspection alone.

---

# Frontend Audit — task-004-review-flow-redesign (whole-branch gate)

- **Audit Scope:** all `apps/frontend` changes in `7ae00b9..fcdd1a8` (113 files, +7324/−2079)
- **Guidelines Source:** `.claude/skills/frontend-dev-guidelines` (SKILL.md + anti-patterns/styling/react-query/types/service-layer resources)
- **Date:** 2026-09-11
- **Typecheck:** PASS — `npx tsc -b --force`, exit 0
- **Tests:** PASS — 45 files / 402 tests, `npx vitest run --pool=forks`
- **Overall:** NEEDS-WORK (one mechanical FAIL: FE-06; plus integration findings below)

## FE-* Checklist

| ID | Check | Status | Evidence |
|----|-------|--------|----------|
| FE-01 | No `any` | PASS | grep `: any` / `as any` / `<any>` over `src/**/*.{ts,tsx}` → 0 matches |
| FE-02 | No manual class concatenation | PASS | grep `className={"` and `` className={` `` → 0 matches; `cn()` throughout (e.g. `FileTreeRow.tsx:51`, `ReviewRow.tsx:43`) |
| FE-03 | No direct API client in components | PASS | grep `lib/api/client` in `components/`+`pages/` → 0 matches. See IMP-2 for a related seam. |
| FE-04 | No inline Zod schemas | PASS (vacuous) | zero `zod` imports anywhere; `lib/schemas/` was deleted on this branch |
| FE-05 | No spinners for content loading | PASS | only 2 `animate-spin`, both on submit buttons: `SelectionBar.tsx:48`, `ReviewErrorPanel.tsx:87`. Content uses `Skeleton` (`DiffPane.tsx:78-82`, `ReviewsTable.tsx:68-74`, `ReviewPage.tsx:139`) |
| FE-06 | No hardcoded colors | **FAIL** | `ReviewRow.tsx:12-13` (`bg-green-500`, `bg-amber-500`); `FileTreeRow.tsx:14,15,17` and `FileHeader.tsx:16,17,19` (`text-amber-500`, `text-green-500`, `text-blue-500`). None of these are tokens in `src/index.css:10-41`. The sibling entries in the same maps *do* use semantics (`bg-destructive`/`text-destructive`), so the maps are half-converted. Also `bg-black/10` in `dialog.tsx:40`, `sheet.tsx:38`, `alert-dialog.tsx:39` (vendored shadcn overlays — noted, not counted). |
| FE-07 | No state mutation | PASS | every `.push`/`.sort` is on a locally-built or copied array: `applyOrder.ts:17` (`[...changes]`), `authors.ts:13` (`[...seen]`), `useSelection.ts:75` (`[...selected.keys()]`), `fileTree.ts:46,48` (`[...dir.dirs.values()]`, `[...dir.files]`), `groupByTicket.ts:43-47` (local `buckets`/`untagged`), `ChangeTable.tsx:35-51` (local `rows`) |
| FE-08 | No default exports for components | PASS | grep `export default` over `src/**/*.{ts,tsx}` → 0 matches |
| FE-09 | Error handling + user feedback | PASS with caveat | no `createErrorFromUnknown` exists in this codebase; the sanctioned equivalent is `messageFor()` (`lib/api/errors.ts:21-23`), used at every failure site (`ReviewsPage.tsx:49`, `ReviewPage.tsx:126,134,191`, `SelectChangesPage.tsx:207,253,260`, `DiffPane.tsx:74`, `ReviewsTable.tsx:47`, `NewReviewSheet.tsx:112`). Zero `console.*` in `src/`. See IMP-1 for the *presentation* inconsistency. |
| FE-10 | JSON:API model shape | PASS | `types/models/branch.ts:10` — `Resource<"branches", BranchAttributes>`; `{id, attributes}` via `types/api/jsonapi.ts` |
| FE-11 | Service pattern | PASS | `services/api/branches.ts:17-35` follows the plain-object direct-client pattern that `CLAUDE.md` records as an agreed deviation from `BaseService`. See MIN-4 for a boundary nit. |
| FE-12 | Query keys `as const` | PASS | `useBranches.ts:5-8`, `useRepositories.ts:5-10`, `useProviders.ts:5-6`, `useReviews.ts:7-12`, `useChanges.ts:5-8` — all hierarchical and `as const` |
| FE-13 | react-hook-form + zodResolver | N/A | no `<form>`/`useForm` on this branch; the drawer's repository entry is a `cmdk` `CommandInput` validated imperatively (`NewReviewSheet.tsx:95-99`) |
| FE-14 | Schemas in `lib/schemas/` with inferred type | N/A | directory removed; no Zod |
| FE-15 | Cursor affordance | PASS | every non-native clickable surface carries it: `FileTreeRow.tsx:52`, `FileTree.tsx:98`, `ChangeRow.tsx:50`, `ReviewRow.tsx:43` (conditional — correctly withheld while `CREATING`), `ChangeFilters.tsx:49`. `PopoverTrigger asChild` targets are both native `<Button>` (`BaseBranchSelect.tsx:85-86`, `IncludedChangesPopover.tsx:16-17`). |
| FE-16 | Tests for changed components | PASS | 45 suites / 402 tests; every non-trivial new component has a suite. One integration-level gap: see CRIT-1. |
| FE-17 | Mocks updated | PASS | no `__mocks__/` dir; MSW handlers in `test/server.ts` + per-suite `server.use()` |

## Mutation Evidence

Two mutations were applied and the full suite re-run; both were then reverted.

1. `store.ts:63` `cache = isValid(parsed) ? parsed : fallback()` → `cache = parsed as T` — **4 tests failed** across all three keys (`store.test.ts` "returns the fallback for a wrong-shaped value", `changeFilters.test.ts` "falls back to the defaults for a partial stored object", `viewed.test.ts` "tolerates a stored array whose members are not strings", `ReviewsPage.test.tsx` "lists recent repositories first…"). **The Global Constraint on total reads is genuinely enforced by tests, not just by inspection.**
2. `ReviewPage.tsx:220` `nextName={nextPath}` → `nextName={"MUTANT"}` — **0 tests failed** (402/402 still green). See CRIT-1.

## Integration Findings

### Critical

**CRIT-1 — FR-36's "Next file: `<name>`" renders a full path, and no test can see it.**
`ReviewPage.tsx:220` passes `nextPath` into a prop named `nextName`. `nextPath` comes from `order` (`ReviewPage.tsx:68,95`), which is `flattenVisible(...)`, which emits **full paths** (`lib/review/fileTree.ts:77` — `out.push(node.file.attributes.path)`). `FileFooter.tsx:20` interpolates it raw: `` `${strings.nextFile}: ${nextName ?? ""}` ``. So the footer button reads `Next file: src/main/java/com/atlas/App.java`, not `Next file: App.java`. PRD FR-36 specifies `<name>`, and the sibling component that renders the same concept does derive a basename (`FileTreeRow.tsx:20-23,43`).

This is the plan's signature fixture defect. The only tests touching the footer use root-level filenames where path and name are identical — `DiffPane.test.tsx:41` (`nextName="other.ts"`) and `:93` (`nextName="first.ts"`) — and `ReviewPage.test.tsx` never asserts on the footer at all (`grep -n 'Next file' pages/__tests__/ReviewPage.test.tsx` → no match). Mutation 2 above proves it: replacing the value with a literal `"MUTANT"` leaves all 402 tests green.

Fix: apply a basename at `ReviewPage.tsx:220` (or inside `FileFooter`), and add a `ReviewPage` assertion using a nested fixture path.

### Important

**IMP-1 — Three different presentations for the same class of mutation failure.**
The same user action (a failed write) surfaces three ways across the redesign:
- `ReviewsPage.tsx:49` — toast only.
- `ReviewPage.tsx:126` — toast only, but with **hardcoded copy bypassing `lib/strings.ts`**: `"The review could not be closed."` — for the *same* `useFinishReview()` backend call whose sibling caller uses `strings.reviewDiscardFailed` (`strings.ts:36`).
- `SelectChangesPage.tsx:207-209` — a persistent `ErrorBanner` **and** a toast for the same error.
- `NewReviewSheet.tsx:109-113` — inline `<p>` for 404, toast for everything else.

Pick one discipline. At minimum, `ReviewPage.tsx:126` must route through `strings.ts`.

**IMP-2 — The create page's `useBranches` is an eager duplicate, and its justifying comment is factually wrong.**
`SelectChangesPage.tsx:83-94` fires `useBranches(providerId, repository, {}, Boolean(providerId) && Boolean(repository))` — `enabled` as soon as the page has params. The comment at `:86-88` justifies this as reusing "the same cache key BaseBranchSelect's closed-state query would use." BaseBranchSelect has **no closed-state query**: `BaseBranchSelect.tsx:47-52` passes `open` as its `enabled` flag, so it fetches nothing until the popover opens. The page therefore issues a branch-list request on every load purely to detect an error it can render, defeating the lazy design the child was built around. (The keys *do* match — `branchKeys.list(p, r, {})` — so the popover's first open is served warm; the cost is the unconditional request, not a double fetch.)

Either lift the query to the page and pass `items`/`isError` down as props, or drop the page-level copy and give `BaseBranchSelect` an error slot (which Ruling 16 declined) — but the current comment should not survive review as written.

**IMP-3 — Duplicated status-decoration maps.**
`FileTreeRow.tsx:6-18` and `FileHeader.tsx:8-19` define byte-identical `STATUS_LETTER` and `STATUS_COLOR` records. Two copies of a four-case map that must stay in sync with `FileStatus`. Extract to `lib/review/` alongside `fileTree.ts`. (Fixing FE-06 will require touching both anyway.)

**IMP-4 — File tree ARIA is structurally invalid.**
`FileTree.tsx:128` sets `role="tree"`, but directory rows (`FileTree.tsx:91-110`) are plain `<button>`s inside a plain `<div>` (`:90`) — no `role="treeitem"`, no `role="group"` wrapper around `renderNodes(node.children, ...)` at `:111`, and no `aria-level`/`aria-posinset`/`aria-setsize` on either row type. `FileTreeRow.tsx:46` correctly sets `role="treeitem"` but is then an orphan treeitem with no owning group. To a screen reader the tree announces its depth and position as unknown. Already logged as a deferred minor in `execution-status.md:51-53`; restating it here because a `role="tree"` that lies is worse than no role at all.

### Minor

**MIN-1 — Copy bypassing `lib/strings.ts`.** `SelectChangesPage.tsx:156-157` ("Missing selection" / "Go back and choose a provider and repository."), `:218` (page description), `:249` ("Could not start the review"), `:207`; `ReviewPage.tsx:133,134,190,196,197`; `ChangeSearch.tsx:35,38`; `Pagination.tsx:12`; `ReviewErrorPanel.tsx:27,50,78` (Diagnostics — git vocabulary is permitted there, but the strings still bypass the module). `"Try again in a moment."` is repeated verbatim at five sites (`SelectChangesPage.tsx:253,260`, `ReviewPage.tsx:191`, `DiffPane.tsx:74`, `ReviewsTable.tsx:47`) and is the clearest candidate for a `strings` entry.

**MIN-2 — Lowercased product vocabulary.** `SelectChangesPage.tsx:259` composes `` `Could not load ${strings.includedChanges.toLowerCase()}` `` → renders **"Could not load included prs/mrs"**. `strings.includedChanges` is `"Included PRs/MRs"` (`strings.ts:9`); `.toLowerCase()` destroys the acronym casing the Global Constraint exists to protect. Use a dedicated string.

**MIN-3 — `recentsStore` read imperatively in one place, reactively in another.** `NewReviewSheet.tsx:50` (`mostRecentProvider()`) and `:66` (`recentsFor(providerId)`, inside a `useMemo` whose deps at `:70` do not include the store) are plain reads, whereas `SelectChangesPage.tsx:59` and `ReviewPage.tsx:56` bind their stores via `useStore`. The sheet will not react to a `recordRecent` write. Impact is masked today because navigating to `/select` unmounts `ReviewsPage` and therefore the sheet — so this is latent, not live. Two disciplines for one abstraction is still worth collapsing.

**MIN-4 — Cross-service import of a re-exported private helper.** `services/api/repositories.ts:51` does `export { query as buildQuery }`, and `branches.ts:2` and `changes.ts:2` both import it. `CLAUDE.md` explicitly calls out "prefer straightforward moves over re-exporting type aliases" and "don't break service boundaries by having one layer call another's internals directly." Move `buildQuery` to a shared module (`lib/api/` or `services/api/query.ts`).

**MIN-5 — Inconsistent iconography and label casing.** `IncludedChangesPopover.tsx:19` renders `{strings.details} ▾` — lowercase `"details"` (`strings.ts:56`) where every neighbouring label is Title Case, and a raw `▾` glyph where `FileTree.tsx:102-104` uses `<ChevronDown>`. Same for `NewReviewSheet.tsx:170` (`{strings.chooseChanges} →`) vs `FileFooter.tsx:21` (`<ArrowRight>`).

**MIN-6 — Loading-state weight differs by page.** `ReviewPage.tsx:139` returns a single `<Skeleton className="h-8 w-1/3" />` for the whole page while `ReviewsTable.tsx:68-74` and `DiffPane.tsx:78-82` render layout-shaped skeletons. Not a FE-05 violation, but the review page's initial paint does not preserve layout.

### Non-finding

No orphaned components: every file under `components/features/` on this branch has at least one non-test importer (verified by per-symbol reverse grep). The deleted `SelectRepositoryPage`, `ProviderPicker`, `ManualRepositoryForm`, `RepositoryList`, `ResumeReviewList`, `ResumeReviewRow`, `ReviewHeader`, and `lib/schemas/repository.ts` were removed cleanly along with their suites.

Fixture quality was sampled against the plan's signature defect and held up outside CRIT-1: `applyOrder` covers ascending, tie-break, single-null, double-null (the `Infinity - Infinity` NaN case), and non-mutation with distinct values throughout (`changes.test.ts:63-104`); `fileTree` covers single-child collapse chains, branch points, dir-holding-a-file, dir-before-file ordering, and multi-level collapsed ancestors (`fileTree.test.ts:20-115`).

## Working-Tree Note

Both of my mutations were reverted; `git diff` on `ReviewPage.tsx` and `store.ts` is empty. Final `git status --porcelain`:

```
 M apps/frontend/src/components/features/newReview/RepositoryResults.tsx
 M docs/tasks/task-004-review-flow-redesign/audit.md
 M docs/tasks/task-004-review-flow-redesign/execution-status.md
?? node_modules/
```

`node_modules/` is expected. The two `docs/` modifications are this file and the concurrent reviewers. **`RepositoryResults.tsx` is not mine and is an un-reverted mutation left in the tree by a concurrent session** — `mergeChoices` line 32 currently reads `.filter(() => true)` in place of `.filter((r) => !seen.has(r.id))`, i.e. the recents/server-results de-duplication is disabled. It must be restored before merge.

## Merge Verdict

**NEEDS-WORK** — CRIT-1 (untested full-path leak into the `Next file` label, FR-36) and FE-06 (five raw palette colors in half-converted status maps) block; IMP-1/IMP-2 should land in the same pass; and the foreign uncommitted `RepositoryResults.tsx` mutant must be reverted before anything merges.

---

# Plan-Adherence Audit — task-004-review-flow-redesign (whole-branch gate)

- **Plan:** `docs/tasks/task-004-review-flow-redesign/plan.md` (27 tasks)
- **Binding authority:** `docs/tasks/task-004-review-flow-redesign/prd.md`
- **Ledger:** `.superpowers/sdd/plan/progress.md` (34 rulings)
- **Range:** `7ae00b9..fcdd1a8`, 46 commits, 155 files, +19499/−2115
- **Date:** 2026-09-11
- **Method:** file:line evidence per task, plus 12 performed-and-reverted mutations
  (7 frontend full-suite, 5 backend `go test ./internal/...`).

## Task Completion

All 27 tasks implemented. No task silently skipped; no task deferred.

| # | Task | Status | Evidence |
|---|------|--------|----------|
| 1 | `provider.Branch` + `BranchBuilder` | DONE | `internal/provider/model.go:38`, `internal/provider/builder.go:50`, `builder_test.go` (new) |
| 2 | `provider.FilterWalk` | DONE | `internal/provider/filterwalk.go` + `filterwalk_test.go`. Mutation B4 (`HasNext:true`→`false`) and B5 (`skip:=0`) each fail `TestFilterWalk`. |
| 3 | Repository search through the provider layer | DONE | `provider.go:18` (`search string` in iface), `github/client.go:120,135` (`FilterWalk`, `maxSearchPages=10` at `:39`), `gitlab/client.go:70`, `fake/fake.go`, `api/repositories.go` |
| 4 | `ListBranches` across the provider layer | DONE | `provider.go:21`, `github/client.go:167,186`, `gitlab/client.go:95`, fixtures `github/testdata/branches.json`, `gitlab/testdata/branches.json` |
| 5 | `INVALID_SEARCH` validation | DONE | `internal/api/search.go:13,19-28`. Mutation B2 (bound ×100) fails `TestRepositorySearchTooLong`, `TestChangesSearchTooLong`, `TestListBranchesSearchTooLong`; B3 (drop `TrimSpace`) fails `TestRepositorySearchIsTrimmed`. |
| 6 | `GET …/repositories/{repo}/branches` | DONE | `internal/api/branches.go`, route at `internal/api/router.go:82`. Mutation B1 (`defaultFirst`→no-op) fails `TestDefaultFirstMovesDefaultToFront`. |
| 7 | Wider per-file diff context | DONE | `internal/diff/diff.go:159` (`-U` + `fileDiffContext`=40) |
| 8 | shadcn primitives | DONE | `src/components/ui/` — all ten PRD §7 primitives present (sheet, breadcrumb, popover, command, progress, switch, separator, tooltip, alert-dialog, scroll-area) plus three transitive deps of `command` |
| 9 | localStorage store abstraction | DONE | `src/lib/storage/store.ts` + `__tests__` |
| 10 | Recents / filter toggles / viewed | DONE | `recents.ts`, `changeFilters.ts`, `viewed.ts`; keys match PRD §6.3 |
| 11 | Change derivations | DONE | `lib/changes/{ticketKey,dependencyBot,applyOrder,groupByTicket,authors}.ts`. Mutation M2 (tie-break reversed) fails 3 tests. |
| 12 | File tree / progress / provider links | DONE | `lib/review/{fileTree,progress,providerLink}.ts`. Mutation M6 (drop stale-path guard in `viewedProgress`) fails 1 test. |
| 13 | Keyboard shortcuts | DONE | `lib/hotkeys/useHotkeys.ts:35-37` guards all five Global-Constraint conditions; `isEditableTarget.ts` |
| 14 | Repository input / debounce / time-left | DONE | `lib/repositoryInput.ts`, `lib/timeLeft.ts`, `lib/hooks/useDebouncedValue.ts` |
| 15 | Branch types, service, hooks | DONE | `types/models/branch.ts`, `services/api/branches.ts`, `lib/hooks/api/useBranches.ts` |
| 16 | App shell, brand, breadcrumbs, container | DONE | `components/layout/{AppShell,BrandMark,Breadcrumbs}.tsx`, `components/common/ProgressBar.tsx`, `lib/breadcrumbs/` |
| 17 | Reviews table | DONE | `components/features/reviews/{ReviewsTable,ReviewRow,ReviewProgressCell,NewReviewRow,DiscardDialog}.tsx` |
| 18 | New-review drawer + root page | DONE | `components/features/newReview/*`, `pages/ReviewsPage.tsx`; 7 replaced files deleted (`git diff --name-status` shows D for `SelectRepositoryPage`, `ProviderPicker`, `ManualRepositoryForm`, `RepositoryList`, `ResumeReviewList/Row`, `lib/schemas/repository.ts`). Mutations M4 (recents-last) and M5 (dedup removed) each fail 3–4 tests. |
| 19 | Base-branch select | DONE | `components/features/changes/BaseBranchSelect.tsx` |
| 20 | Change filter row | DONE | `components/features/changes/ChangeFilters.tsx` |
| 21 | Grouped change table | DONE | `components/features/changes/{ChangeTable,ChangeRow,TicketGroupHeader}.tsx` |
| 22 | Selection bar + create page | DONE | `components/features/changes/SelectionBar.tsx`, `pages/SelectChangesPage.tsx`; branches-error banner at `:250-256` (Ruling 16 discharged) |
| 23 | Review status line | DONE | `components/features/review/{ReviewStatusLine,IncludedChangesPopover}.tsx` |
| 24 | Directory file tree | DONE | `components/features/review/{FileTree,FileTreeRow}.tsx` |
| 25 | Diff pane | DONE | `components/features/review/{DiffPane,FileHeader,FileDiff,FileFooter}.tsx` |
| 26 | Review page wiring | DONE | `pages/ReviewPage.tsx`; exhaustive switch `:144-231` with `assertUnreachable(currentStatus)` at `:231` (Ruling 25 discharged); hotkeys gated `enabled: status==="READY" && !confirming` at `:110` |
| 27 | Full verification sweep | DONE | commit `fcdd1a8`; §10 walk recorded in this file above |

**Completion rate: 27/27 (100%). Skipped without approval: 0. Partial: 0.**

## Deviations from plan text — all 34 rulings judged

I read every ruling in the ledger and spot-verified the load-bearing ones in
source. **All 34 are justified.** They fall into five classes:

1. **Plan sample does not compile** (Rulings 2, 3, 33) — e.g. `config.Secret(...)`
   is a struct not a constructor (`internal/config/secret.go:8,11`). Correct to deviate.
2. **Plan sample fails the plan's own test** (Rulings 6, 7, 10, 13, 24, 26) —
   the test is the more specific expression of intent. Verified two of these directly:
   `applyOrder.ts:22-24` compares instants for inequality before subtracting, which is
   the only form that sorts a null `mergedAt` last *and* survives `Infinity - Infinity`;
   the ancestor-expansion split in `Breadcrumbs`/`useBreadcrumbs` is real (the combined
   context provably re-render-loops).
3. **Plan sample violates a repo lint rule** (Rulings 9, 27, 33) — `react-hooks/refs`,
   `react-hooks/set-state-in-effect`, `exhaustive-deps`. `make lint` is a branch-completion
   gate in CLAUDE.md, so the plan text could not be shipped as written.
4. **Global Constraint enforcement beyond the plan's file list** (Rulings 4, 8, 11,
   14, 22, 23, 30, 32) — copy routed through `strings.ts`, `?search` validated on
   `changes.go` too, sr-only table headers, and coverage a brief step had silently
   deleted. Ruling 4 in particular is right: `internal/api/changes.go` now routes through
   `searchFrom`, which mutation B2 proves is tested.
5. **Scaffolding a later task owns** (Rulings 19, 28) — both discharged by Task 26;
   `grep` finds no `EMPTY_VIEWED` and no no-op `onToggleViewed` at `fcdd1a8`.

The one ruling that overrode a plan-supplied *assertion* — Ruling 12, relaxing the
client repository validator so `org/.github` is accepted — was escalated to and
**resolved by the user**, not taken unilaterally. `repositoryInput.ts` now mirrors
`gitx/validate.go:32-45`. Correct handling.

## Deferred-item triage

Verdicts on every `minor (deferred)` and parked line in the ledger, plus Task 27's gap.

### Must fix before merge

**MUST-1 — FR-36 "Next file: `<name>`" renders a full path.**
`ReviewPage.tsx:220` passes `nextPath` (a full path from `flattenVisible`,
`lib/review/fileTree.ts:77`) into `FileFooter`'s `nextName`, which interpolates it raw
(`FileFooter.tsx:21`). The PRD binds `<name>`. This is **not an implementer error** —
`plan.md:8440` specifies `nextName={nextPath}` verbatim, so the plan itself contradicts
the PRD it implements, and the PRD wins. Two mutations bracket it: setting the prop to a
literal leaves 402/402 green (frontend auditor), and applying the *correct* fix
(`nextPath?.split("/").pop()`) also leaves 402/402 green — the behaviour is unpinned in
both directions. Fix is one line plus a `ReviewPage` assertion with a nested fixture path.
This is the sole PRD non-conformance on the branch and the only merge blocker I raise.

### Fine to defer — with reasoning

| Ledger item | Verdict | Reasoning |
|---|---|---|
| T9: brief said "10 tests", supplied 9 | defer | Brief prose slip; no code or coverage consequence. |
| T9: `get()` does a `getItem` per `getSnapshot` | defer | A string compare, not a parse; PRD §8's perf budget is tree/diff memoisation, which is satisfied. No measured jank (45 files/402 tests in 17s). |
| T10: `clearViewed` leaves `lastRaw` stale for one read | defer | Self-healing, and both call sites (`ReviewPage.tsx:123`, `ReviewsPage.tsx:47`) navigate or refetch immediately; no read is interposed. |
| T10: stale comment in `changeFilters.test.ts` | defer | Comment-only, inherited from the brief; fix opportunistically. |
| T12: `providerLink.ts:29` untested `webUrl !== ""` branch | defer | **Confirmed still untested** — mutation M3 (guard removed) leaves 402/402 green. But it is a defensive guard on a fallback whose *result* (repository URL) is asserted; worst case is an empty `href`. A 3-line test closes it; not a blocker. |
| T11: `mergedAt` parsing duplicated in two modules | defer | Short, divergent in purpose; extraction is negative value. |
| T13: live keydown never driven against contentEditable/select | defer | `useHotkeys.ts:37` has exactly one call into `isEditableTarget`, which *is* unit-tested for both cases; the composition risk is a single line. |
| T13: no test flips `enabled` true→false mid-mount | **closed, not deferred** | Task 26's fix does exactly this (`ReviewPage.tsx:110`, `!confirming`) and the re-review proved `ReviewPage.test.tsx:232` catches reverting it. |
| T15: `useBranches` has no error/empty test | defer | Substantially closed by Task 22's `branches.isError` banner test at `SelectChangesPage.tsx:250-256`. |
| T15 WARN2: no `400 INVALID_SEARCH` test at the service/hook layer | defer | The security boundary is server-side and is tested: mutation B2 fails three API tests. Client-layer coverage would be testing generic `ApiError` propagation. |
| T17: skeleton-rows test asserts only absence of empty state | defer | Cosmetic loading affordance; no user-visible correctness risk. |
| T17: dialog `pending` branch uncovered / `pending` is inert | defer | **Confirmed inert**: `ReviewRow.tsx:100-101` calls `setConfirming(false)` *before* `onDiscard`, so the dialog unmounts before `pending` can render. The row-level `disabled={pending}` at `:82` is live and correct. Recommend deleting the dead prop later, not now. |
| T17: building-state row-click no-op untested | defer | Guard is a single conditional; the `cursor-pointer` withholding is verified by the frontend audit (FE-15). |
| T18: bare `→` glyph outside `strings.ts` | defer | Decorative; already logged as frontend MIN-5. |
| T21: `onToggleGroup` args not asserted | **closed** | Task 22's review verified a real consumer plus a resulting-state assertion (stronger than arg-matching) and killed the `!== select` mutation. |
| T22: page-level `useBranches` duplicates the select's query | **defer the dedup, fix the comment** | Keys match so there is no double fetch, but the justifying comment at `SelectChangesPage.tsx:86-88` asserts a "closed-state query" that `BaseBranchSelect.tsx:47-52` does not have. A comment that documents non-existent behaviour should not survive; the architecture itself is acceptable (frontend IMP-2). |
| T23: ticket-key iteration order invisible to the suite | defer | Affects which of several ticket badges is chosen for an informational header; PRD FR-29 says "first key extracted from the included titles" without pinning a tie rule. |
| T24: file-tree ARIA is partial | defer (but fix soon) | PRD FR-41 requires only keyboard reachability and accessible names, both present, so this is not a PRD violation. A `role="tree"` without `role="group"`/`aria-level` is nonetheless worse than no role (frontend IMP-4); either complete it or drop the role. |
| **T27: drawer Cancel + scrim close untested** | defer | **Confirmed by mutation M1**: rewiring Cancel to a no-op leaves 402/402 green. But Cancel (`NewReviewSheet.tsx:166`) and the scrim (`:120 onOpenChange={close}`) route through the *same* `close(next)` function as Escape, which **is** tested — so the mechanism is covered and only the button's own wiring is unpinned. Two `userEvent.click` lines close it; fold into the MUST-1 pass since one is happening anyway. |

### Parked findings — all closed

- **Ruling 12 (`org/.github` unreachable):** resolved by the user, implemented; client predicate now mirrors `gitx/validate.go:32-45`. Closed.
- **Ruling 16 (no branches-fetch error affordance):** landed in Task 22, verified by the re-reviewer's 500-response mutation. Closed.
- **Ruling 25 (hardcoded `"Ready"` badge):** gated by the exhaustive switch at `ReviewPage.tsx:144-231`; `ReviewStatusLine` mounts only under `case "READY"`. Closed.
- **`tsc --noEmit` vacuity across 26 tasks:** the real check `npx tsc -b --force` passes on this branch. Harmless in hindsight. Closed.

## Mutations performed (all reverted)

| ID | Mutation | Result |
|---|---|---|
| M1 | `NewReviewSheet` Cancel `onClick` → no-op | 402/402 pass — **gap confirmed** |
| M2 | `applyOrder` tie-break reversed | 3 fail — covered |
| M3 | `providerLink` `webUrl !== ""` guard removed | 402/402 pass — **gap confirmed** |
| M4 | `mergeChoices` recents moved last | 4 fail — covered |
| M5 | `mergeChoices` de-dup removed | 3 fail — covered |
| M6 | `viewedProgress` stale-path guard removed | 1 fail — covered |
| M7 | `nextName` given the *correct* basename fix | 402/402 pass — **behaviour unpinned both ways** |
| B1 | `api.defaultFirst` → no-op | `TestDefaultFirstMovesDefaultToFront` fails |
| B2 | `maxSearchLen` ×100 | 3 API tests fail |
| B3 | `searchFrom` `TrimSpace` removed | `TestRepositorySearchIsTrimmed` fails |
| B4 | `FilterWalk` `HasNext:true` → `false` | `TestFilterWalk` fails |
| B5 | `FilterWalk` `skip := 0` | `TestFilterWalk` fails |

Fixture quality was sampled against this plan's signature defect (two distinct inputs
sharing a value). The derivations, `mergeChoices`, `defaultFirst`, and `FilterWalk`
paging all discriminate. The two holes found (M1, M3) are both untested *guards*, not
shared-value fixtures.

**Note for the concurrent frontend auditor:** the un-reverted
`RepositoryResults.tsx` mutant it observed in the working tree was mutation M5 from this
audit, caught mid-flight. It has been reverted; `git diff` on that file is empty.

## Merge Verdict

**NEEDS_FIXES** — plan adherence is FULL (27/27, all 34 deviations justified), but
**MUST-1** (FR-36 renders a path where the PRD binds a name, root-caused to `plan.md:8440`)
is a real PRD non-conformance and must land first. Every other deferred item is safe to
carry. Fold the drawer-Cancel test, the `providerLink` guard test, and the
`SelectChangesPage.tsx:86-88` comment correction into the same pass.
