# Review Flow Redesign — Implementation Context

Task: task-004-review-flow-redesign
Worktree: `.worktrees/task-004-review-flow-redesign` on branch `task-004-review-flow-redesign`
PRD: `docs/tasks/task-004-review-flow-redesign/prd.md`
Design: `docs/tasks/task-004-review-flow-redesign/design.md`
Plan: `docs/tasks/task-004-review-flow-redesign/plan.md`

---

## 1. What this task is

A frontend rebuild of all three routes plus two backend additions the browser
cannot fake: repository search on the provider listing, and a branch listing
per repository. Everything else the PRD asks for (recent repositories, ticket
grouping, bot filtering, author chips, apply order, viewed state, keyboard
shortcuts, folds, breadcrumbs) is derived or stored in the browser.

One third backend change exists that the PRD did not anticipate: `internal/diff`
gains a wider context window so the review page can fold and expand unchanged
lines from the already-returned patch (design §3.5).

## 2. Key files, as they exist today

### Backend (`apps/backend`)

| File | Why it matters |
|---|---|
| `internal/provider/provider.go` | The `GitProvider` interface. Both signature changes land here. `ListMergedChanges(ctx, repo, target, search string, page Page)` is the precedent for positional `search string, page Page`. |
| `internal/provider/model.go` | Immutable value types with lowercase fields and getter methods. `Repository` is the shape `Branch` copies. |
| `internal/provider/builder.go` | Chainable builders. `RepositoryBuilder.Build()` validates via `gitx.*` and returns `(T, error)`. |
| `internal/provider/page.go` | `Page{Number, Size}` with `Normalize()` (defaults 30, max 100); `Slice[T]{Items, HasNext}`. |
| `internal/provider/fake/fake.go` | In-memory provider used by `api_test.go` and integration tests. `paginate[T]` helper at the bottom. |
| `internal/provider/github/client.go` | `c.get(ctx, path, query, out) (hasNext bool, err error)` — `hasNext` comes from the `Link: rel="next"` header. `providerPage = 100`. |
| `internal/provider/gitlab/client.go` | `c.get(ctx, path, query, out) (nextPage string, err error)` — `hasNext` is `nextPage != ""` from `X-Next-Page`. |
| `internal/provider/{github,gitlab}/mapping.go` | `repoJSON`/`projectJSON` → model. New `branchJSON` types go here. |
| `internal/api/repositories.go` | `pageFrom`, `providerFor`, `repoNameFrom`, `listRepositories`, `getRepository`. |
| `internal/api/changes.go` | The template for a new list handler, including the inline `INVALID_STATE` 400. |
| `internal/api/router.go` | Route registration. `{repo}` is a single wildcard segment on purpose — see the comment there. |
| `internal/api/errors.go` | `classify` maps `provider.ErrNotFound` → `404 NOT_FOUND`, `ErrAuth` → `502 PROVIDER_AUTH`, `ErrUnavailable` → `503 PROVIDER_UNAVAILABLE`. There is no `REPOSITORY_NOT_FOUND` or `PROVIDER_ERROR`. |
| `internal/diff/diff.go` | `FileContent` runs `git diff --find-renames base head -- path`. `MaxFileDiffBytes` truncation is applied after. |
| `internal/gitx/validate.go` | `ValidateSHA` = exactly 40 lowercase hex. `ValidateBranchSyntax` = non-empty, no leading `-`, `len <= 255`, and not matching `branchBad`. |

Test patterns to copy:
- `internal/provider/github/client_test.go` — `httptest.Server` + `fixture(t, name)` reading `testdata/*.json`, recording `[]call{path, query}`.
- `internal/api/api_test.go` — `newAPIFixture(t)` builds a real `review.Service` over a `fake.Provider` seeded with `atlas/server` and change `#421`; helpers `do`, `decodeList`, `decodeOne`, `assertErrorCode`.
- `internal/diff/diff_test.go` — `gitx.FakeRunner{Handler: func(gitx.Spec) (gitx.Result, error)}` lets a test assert the exact argument slice.

### Frontend (`apps/frontend`)

| File | Why it matters |
|---|---|
| `src/components/layout/AppShell.tsx` | Today: wordmark + theme toggle, sets no max width. Gains the brand block, breadcrumb, and the single shared container. |
| `src/routes.tsx` | Three routes; `/` currently renders `SelectRepositoryPage`, which is renamed to `ReviewsPage`. |
| `src/lib/api/client.ts` | `apiGet<T>(path, init)` already forwards `init`, so an `AbortSignal` passes through unchanged — no client change is needed for request cancellation. |
| `src/services/api/repositories.ts` | Exports `buildQuery`, which already drops `undefined` and `""`. `RepositoryListParams` gains `search`. |
| `src/lib/hooks/api/useRepositories.ts` | `repositoryKeys.list(providerId, params)` already includes `params`, so `search` flows into the key with no key change. |
| `src/lib/hooks/useSelection.ts` | `sessionStorage`-backed `Map<number, Change>`. Unchanged by this task; the new selection bar reads `selection.selected.values()`. |
| `src/lib/theme/storage.ts` | The precedent for total, try/catch-wrapped storage reads. `lib/storage/store.ts` generalises it; the theme module itself is left alone because of its boot-script coupling. |
| `src/lib/relativeTime.ts` | Module-scope `Intl.RelativeTimeFormat`; `expiryLabel` is the sibling of the new `timeLeft`. |
| `src/test/render.tsx` | `renderWithProviders(ui, {route})` and `queryWrapper({gcTime})`. |
| `src/test/server.ts` | MSW `server`, plus `oneDoc`, `listDoc`, `errorDoc` builders. |
| `src/components/features/review/FileDiff.tsx` | `PatchDiff` from `@pierre/diffs/react`; the options object is what tests assert on. |
| `src/types/models/review.ts` | `ReviewAttributes` already carries `included[]`, `totals`, `baseSha`, `baseDescription`, `expiresAt` — the status line needs no API change. |

## 3. Decisions that bind the implementation

From the design (§2), already settled — do not relitigate:

- **GitHub repository search** page-walks `GET /user/repos` with `per_page=100`, capped at 10 pages. The GitHub search API is not used (30 req/min shared across all search endpoints).
- **Container width** is `max-w-[80rem]` (1280 px) with `px-6`.
- **"Inspect"** on a conflicted review is navigation only.
- **Error codes**: the branches handler introduces no new codes. Only `INVALID_SEARCH` (400) is new, on the repositories listing.
- **"Open in provider"** targets the single included change's `webUrl` when the review includes exactly one change, otherwise the repository's `webUrl`.
- **`FileContent` gains `-U40`**, which the PRD listed as "diff: no change". The combined diff and numstat totals are untouched.

Decided while writing the plan:

- **`FilterWalk.HasNext` is observational**: true only when a match beyond the
  requested page was actually seen. The design's looser phrasing ("or when the
  upstream said it had more pages") would let `HasNext` be true for a page that
  turns out to be empty. The stricter rule is deterministic and testable, and a
  walk stopped by the page cap reports `HasNext=false` either way.
- **`AppShell` renders the container; pages drop their own `max-w-*`**, which
  means every page's outer wrapper changes in the task that rewrites it.
- **Breadcrumbs are published by pages** through `BreadcrumbContext`, not
  inferred from the URL by the shell (design §4.2, option 3).

## 4. Dependencies and ordering

The plan has 27 tasks: 1–7 backend, 8–16 frontend foundations, 17–18 root page,
19–22 create page, 23–26 review page, 27 the closing sweep.

- Backend tasks 1–7 are independent of every frontend task and can land first.
- Task 8 (shadcn install) blocks every UI task; run it before Task 16.
- Library tasks 9–15 have no React dependency and no dependency on each other
  except: 10 (`recents`/`changeFilters`/`viewed`) needs 9 (`store`).
- Task 16 (AppShell + breadcrumbs) blocks 18, 22, and 26 — each page's wiring
  task calls `useBreadcrumbs`.
- Task 15 (`branches` service + `useBranches`) needs backend Task 6's contract
  but not its implementation; MSW fakes the endpoint in frontend tests.
- Task 19 (`BaseBranchSelect`) needs 15; Task 21 (`ChangeTable`) needs 11;
  Tasks 24–25 need 12.
- Task 27 is the final verification sweep and must run last.

New runtime dependency: `cmdk`, pulled in by `npx shadcn add command`. No
others.

## 5. Verification

A branch is done only when all of these are clean from the repository root:

```
make lint
make test
make test-integration
make build
make docker-build
```

Node is not always on `PATH`:

```sh
export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22
```

Per-task verification uses the narrow commands (`go test ./internal/provider/...`,
`npx vitest run src/lib/changes`) so a task's cycle stays fast; the full sweep
is task 28.

## 6. Known risks

- **GitHub page-walk latency.** Ten upstream calls worst case per search. The
  250 ms debounce, request cancellation, React Query caching per query string,
  and `maxSearchPages` bound it. If it proves slow, one constant changes.
- **Wider diff payloads.** 40 context lines each side per hunk. The existing
  1 MiB cap and truncation banner already bound the worst case.
- **Renaming `SelectRepositoryPage` → `ReviewsPage`** touches `routes.tsx`,
  `src/__tests__/routes.test.tsx`, and `src/pages/__tests__/SelectRepositoryPage.test.tsx`.
  A straight move with no alias, per project convention.
- **Fixed-height review split on short viewports.** The frame carries
  `min-h-[24rem]`; below that the page scrolls as a whole.
- **Deleted components** (`RepositoryList`, `ManualRepositoryForm`,
  `ResumeReviewList`, `ResumeReviewRow`, `ReviewHeader`, `ProviderPicker`) each
  have tests that must be deleted or moved in the same commit, or `npm run lint`
  fails on unresolved imports.
