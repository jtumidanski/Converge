# Review Flow Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild Converge's three routes around one page width, one header pattern, and one primary action per screen, adding repository search and branch listing to the backend so the new drawer and base-branch picker have real data.

**Architecture:** Two small backend additions (`search` on the repository listing, a new branches endpoint) behind the existing `GitProvider` interface, plus a wider per-file diff context window. The frontend is reorganised around React-free library modules (`lib/storage`, `lib/changes`, `lib/review`, `lib/hotkeys`) that carry every derivation — ticket keys, bot detection, apply order, file trees, viewed progress — so pages become thin composers of feature components.

**Tech Stack:** Go 1.x (stdlib `net/http`, `log/slog`, JSON:API via `internal/jsonapi`); React 19 + TypeScript + Vite, TanStack React Query, shadcn/ui over radix-ui, Tailwind 4, `@pierre/diffs`, Vitest + Testing Library + MSW.

**Spec:** `docs/tasks/task-004-review-flow-redesign/design.md` (PRD: `docs/tasks/task-004-review-flow-redesign/prd.md`)

## Global Constraints

- Backend package direction is `api → review → {provider, mirror, workspace, diff, session} → gitx`. Nothing imports `api` or `cmd`. `internal/review`, `session`, `mirror`, `workspace`, and `gitx` are **not touched** by this task.
- Every git call goes through `gitx.Runner` with an argument slice, never a shell. Client-supplied strings are validated before they reach git.
- Provider models are immutable: lowercase fields, getter methods, chainable builder with a validating `Build() (T, error)`.
- `search` is trimmed and length-checked in the API layer (`maxSearchLen = 200`); providers never receive an untrimmed or oversized value. Over-length is `400 INVALID_SEARCH`.
- No new API error codes other than `INVALID_SEARCH`. `provider.ErrNotFound` → `404 NOT_FOUND`, `provider.ErrAuth` → `502 PROVIDER_AUTH`, `provider.ErrUnavailable` → `503 PROVIDER_UNAVAILABLE`, via the existing `classify`.
- No new server-side persistence. `session.json`, sweeping, expiry, review building, and the applicator are unchanged.
- Shared container is exactly `mx-auto w-full max-w-[80rem] px-6 py-6`. No route uses full viewport width.
- localStorage keys are exactly `converge.recentRepositories`, `converge.changeFilters`, `converge.viewed.<reviewId>`. Every read is total: missing, malformed, wrong-shape, and throwing storage all collapse to the documented default.
- Ticket key regex is exactly `/\b[A-Z][A-Z0-9]+-\d+\b/`, first match in the title.
- Dependency-bot rule is exactly: lower-cased `sourceBranch` starts with `renovate/` or `dependabot/`.
- Apply order is ascending `mergedAt`, then ascending `number`; a null `mergedAt` sorts last.
- Keyboard shortcuts are single-key, never fire with ctrl/meta/alt held, never fire while focus is in an input, textarea, select, or `contentEditable`, and are additionally disabled while a sheet, dialog, or popover is open.
- Recent repositories are capped at 20 entries, most recent first; the drawer shows at most 10 for the selected provider.
- GitHub page-walk cap is 10 upstream pages at `per_page=100` (`maxSearchPages`).
- Per-file diff context is `-U40` (`fileDiffContext = 40`).
- Frontend user-facing copy goes through `src/lib/strings.ts`; git vocabulary appears only under Diagnostics.
- Node is not always on `PATH`. Before any `npm`/`npx` command: `export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22`.
- Commit after every task. Never edit files in the main repo — all work happens in `.worktrees/task-004-review-flow-redesign`.

---

## File Structure

### Backend, created

| File | Responsibility |
|---|---|
| `apps/backend/internal/provider/filterwalk.go` | `FilterWalk` — generic paged client-side filter with skip/take and cap |
| `apps/backend/internal/provider/filterwalk_test.go` | Table tests for skip/take arithmetic, cap, `HasNext`, error propagation |
| `apps/backend/internal/api/search.go` | `maxSearchLen`, `searchFrom(r)` |
| `apps/backend/internal/api/branches.go` | `branchAttributes`, `branchResource`, `listBranches` handler |
| `apps/backend/internal/provider/github/testdata/repos_p1.json`, `repos_p2.json`, `branches.json` | GitHub fixtures |
| `apps/backend/internal/provider/gitlab/testdata/projects_search.json`, `branches.json` | GitLab fixtures |

### Backend, modified

| File | Change |
|---|---|
| `internal/provider/provider.go` | `ListRepositories` gains `search`; `ListBranches` added |
| `internal/provider/model.go` | `Branch` value type |
| `internal/provider/builder.go` | `BranchBuilder` |
| `internal/provider/fake/fake.go` | substring filter on repositories; `AddBranch` + `ListBranches` |
| `internal/provider/github/{client,mapping}.go` | search page-walk; `branchJSON`; `ListBranches` |
| `internal/provider/gitlab/{client,mapping}.go` | `search`/`search_namespaces`; `branchJSON`; `ListBranches` |
| `internal/api/repositories.go` | `searchFrom` wiring |
| `internal/api/router.go` | branches route |
| `internal/diff/diff.go` | `fileDiffContext`, `-U40` |

### Frontend, created

```
src/lib/storage/{store.ts,recents.ts,changeFilters.ts,viewed.ts}
src/lib/changes/{ticketKey.ts,dependencyBot.ts,applyOrder.ts,groupByTicket.ts,authors.ts}
src/lib/review/{fileTree.ts,progress.ts,providerLink.ts}
src/lib/hotkeys/{useHotkeys.ts,isEditableTarget.ts}
src/lib/breadcrumbs/{context.ts,useBreadcrumbs.ts}
src/lib/repositoryInput.ts
src/lib/timeLeft.ts
src/lib/hooks/useDebouncedValue.ts
src/lib/hooks/api/useBranches.ts
src/services/api/branches.ts
src/types/models/branch.ts
src/components/layout/{Breadcrumbs.tsx,BrandMark.tsx}
src/components/common/{Hotkey.tsx,ProgressBar.tsx}
src/components/features/reviews/{ReviewsTable.tsx,ReviewRow.tsx,ReviewProgressCell.tsx,NewReviewRow.tsx,DiscardDialog.tsx}
src/components/features/newReview/{NewReviewSheet.tsx,ProviderSelect.tsx,RepositorySearch.tsx,RepositoryResults.tsx}
src/components/features/changes/{BaseBranchSelect.tsx,ChangeFilters.tsx,ChangeRow.tsx,TicketGroupHeader.tsx}
src/components/features/review/{ReviewStatusLine.tsx,IncludedChangesPopover.tsx,ReviewWorkspace.tsx,FileTreeRow.tsx,DiffPane.tsx,FileHeader.tsx,FileFooter.tsx}
src/pages/ReviewsPage.tsx
```

### Frontend, deleted

```
src/components/features/repositories/RepositoryList.tsx
src/components/features/repositories/ManualRepositoryForm.tsx
src/components/features/repositories/__tests__/ManualRepositoryForm.test.tsx
src/components/features/reviews/ResumeReviewList.tsx
src/components/features/reviews/ResumeReviewRow.tsx
src/components/features/reviews/__tests__/ResumeReviewList.test.tsx
src/components/features/providers/ProviderPicker.tsx
src/components/features/review/ReviewHeader.tsx
src/lib/schemas/repository.ts
src/lib/schemas/__tests__/repository.test.ts
src/pages/SelectRepositoryPage.tsx
src/pages/__tests__/SelectRepositoryPage.test.tsx
```

---

# Phase A — Backend

### Task 1: `provider.Branch` and `BranchBuilder`

**Files:**
- Modify: `apps/backend/internal/provider/model.go`
- Modify: `apps/backend/internal/provider/builder.go`
- Test: `apps/backend/internal/provider/builder_test.go` (create if absent; otherwise append)

**Interfaces:**
- Consumes: `gitx.ValidateBranchSyntax`, `gitx.ValidateSHA`.
- Produces: `provider.Branch` with `Name() string`, `SHA() string`, `IsDefault() bool`; `provider.NewBranchBuilder() *BranchBuilder` with `SetName`, `SetSHA`, `SetDefault`, `Build() (Branch, error)`.

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/provider/builder_test.go` (create the file with `package provider` and the imports shown if it does not exist):

```go
package provider

import (
	"errors"
	"testing"

	"github.com/jtumidanski/converge/internal/gitx"
)

func TestBranchBuilder(t *testing.T) {
	const sha = "6140736dbb1a0a0f1f0e2a1b3c4d5e6f70819a2b"
	cases := []struct {
		name    string
		build   func() (Branch, error)
		wantErr bool
	}{
		{"valid with sha", func() (Branch, error) {
			return NewBranchBuilder().SetName("main").SetSHA(sha).SetDefault(true).Build()
		}, false},
		{"valid without sha", func() (Branch, error) {
			return NewBranchBuilder().SetName("release/1.0").Build()
		}, false},
		{"empty name", func() (Branch, error) {
			return NewBranchBuilder().SetName("").Build()
		}, true},
		{"option-like name", func() (Branch, error) {
			return NewBranchBuilder().SetName("--upload-pack=evil").Build()
		}, true},
		{"traversal in name", func() (Branch, error) {
			return NewBranchBuilder().SetName("feat/../x").Build()
		}, true},
		{"short sha", func() (Branch, error) {
			return NewBranchBuilder().SetName("main").SetSHA("614073").Build()
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.build()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				if !errors.Is(err, gitx.ErrInvalid) {
					t.Errorf("error = %v, want gitx.ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBranchGetters(t *testing.T) {
	const sha = "6140736dbb1a0a0f1f0e2a1b3c4d5e6f70819a2b"
	b, err := NewBranchBuilder().SetName("main").SetSHA(sha).SetDefault(true).Build()
	if err != nil {
		t.Fatal(err)
	}
	if b.Name() != "main" || b.SHA() != sha || !b.IsDefault() {
		t.Errorf("branch = %+v", b)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/backend && go test ./internal/provider/ -run TestBranch -v`
Expected: FAIL — `undefined: Branch`, `undefined: NewBranchBuilder`.

- [ ] **Step 3: Add the `Branch` value type**

Append to `internal/provider/model.go`, immediately after the `Repository` getters:

```go
// Branch is an immutable provider branch reference.
type Branch struct {
	name      string
	sha       string
	isDefault bool
}

func (b Branch) Name() string    { return b.name }
func (b Branch) SHA() string     { return b.sha }
func (b Branch) IsDefault() bool { return b.isDefault }
```

- [ ] **Step 4: Add the builder**

Append to `internal/provider/builder.go`, after `RepositoryBuilder.Build`:

```go
// BranchBuilder constructs a Branch.
type BranchBuilder struct{ b Branch }

func NewBranchBuilder() *BranchBuilder { return &BranchBuilder{} }

func (b *BranchBuilder) SetName(v string) *BranchBuilder  { b.b.name = v; return b }
func (b *BranchBuilder) SetSHA(v string) *BranchBuilder   { b.b.sha = v; return b }
func (b *BranchBuilder) SetDefault(v bool) *BranchBuilder { b.b.isDefault = v; return b }

// Build validates the branch name and, when present, the tip SHA. Validating
// here means a provider can never hand the API a ref that POST /api/reviews
// would later reject: the same gitx rules guard both doors.
func (b *BranchBuilder) Build() (Branch, error) {
	br := b.b
	if err := gitx.ValidateBranchSyntax(br.name); err != nil {
		return Branch{}, fmt.Errorf("branch: %w", err)
	}
	if br.sha != "" {
		if err := gitx.ValidateSHA(br.sha); err != nil {
			return Branch{}, fmt.Errorf("branch: %w", err)
		}
	}
	return br, nil
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd apps/backend && go test ./internal/provider/ -run TestBranch -v`
Expected: PASS (8 subtests).

- [ ] **Step 6: Vet and commit**

```bash
cd apps/backend && go vet ./internal/provider/
cd "$(git rev-parse --show-toplevel)"
git add apps/backend/internal/provider/model.go apps/backend/internal/provider/builder.go apps/backend/internal/provider/builder_test.go
git commit -m "feat(provider): add the Branch value type and its builder"
```

---

### Task 2: `provider.FilterWalk`

**Files:**
- Create: `apps/backend/internal/provider/filterwalk.go`
- Test: `apps/backend/internal/provider/filterwalk_test.go`

**Interfaces:**
- Consumes: `Page`, `Slice[T]` from `page.go`.
- Produces: `provider.FilterWalk[T any](ctx context.Context, page Page, maxPages int, fetch func(context.Context, int) ([]T, bool, error), keep func(T) bool) (Slice[T], error)`.

**Semantics (binding):** `HasNext` is **observational** — true only when a kept
item beyond the requested page was actually seen. A walk that stops because it
hit `maxPages` reports `HasNext=false`. This is stricter than the design's
prose and is what the tests assert.

- [ ] **Step 1: Write the failing test**

Create `apps/backend/internal/provider/filterwalk_test.go`:

```go
package provider

import (
	"context"
	"errors"
	"testing"
)

// pages builds a fetch func over fixed upstream pages of ints.
func pages(src [][]int, calls *int) func(context.Context, int) ([]int, bool, error) {
	return func(_ context.Context, n int) ([]int, bool, error) {
		*calls++
		if n < 1 || n > len(src) {
			return nil, false, nil
		}
		return src[n-1], n < len(src), nil
	}
}

func even(n int) bool { return n%2 == 0 }

func TestFilterWalk(t *testing.T) {
	src := [][]int{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}}
	cases := []struct {
		name        string
		page        Page
		maxPages    int
		wantItems   []int
		wantHasNext bool
	}{
		{"first page, more to come", Page{Number: 1, Size: 2}, 10, []int{2, 4}, true},
		{"second page", Page{Number: 2, Size: 2}, 10, []int{6, 8}, true},
		{"last page exhausts upstream", Page{Number: 3, Size: 2}, 10, []int{10, 12}, false},
		{"page past the end", Page{Number: 4, Size: 2}, 10, []int{}, false},
		{"page larger than all matches", Page{Number: 1, Size: 50}, 10, []int{2, 4, 6, 8, 10, 12}, false},
		{"cap hides remaining matches", Page{Number: 1, Size: 2}, 1, []int{2, 4}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := FilterWalk(context.Background(), tc.page, tc.maxPages, pages(src, &calls), even)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Items) != len(tc.wantItems) {
				t.Fatalf("items = %v, want %v", got.Items, tc.wantItems)
			}
			for i := range tc.wantItems {
				if got.Items[i] != tc.wantItems[i] {
					t.Fatalf("items = %v, want %v", got.Items, tc.wantItems)
				}
			}
			if got.HasNext != tc.wantHasNext {
				t.Errorf("hasNext = %v, want %v", got.HasNext, tc.wantHasNext)
			}
		})
	}
}

func TestFilterWalkStopsAtCap(t *testing.T) {
	src := [][]int{{1}, {2}, {3}, {4}, {5}}
	calls := 0
	if _, err := FilterWalk(context.Background(), Page{Number: 1, Size: 10}, 2, pages(src, &calls), even); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("upstream calls = %d, want 2", calls)
	}
}

func TestFilterWalkPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	fetch := func(context.Context, int) ([]int, bool, error) { return nil, false, boom }
	_, err := FilterWalk(context.Background(), Page{Number: 1, Size: 2}, 10, fetch, even)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom unwrapped", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/backend && go test ./internal/provider/ -run TestFilterWalk -v`
Expected: FAIL — `undefined: FilterWalk`.

- [ ] **Step 3: Implement `FilterWalk`**

Create `apps/backend/internal/provider/filterwalk.go`:

```go
package provider

import "context"

// FilterWalk paginates a provider listing that cannot filter server-side.
//
// fetch returns one upstream page (1-based) and whether the upstream has more
// pages. Items for which keep returns true are counted; the first
// (page.Number-1)*page.Size of them are skipped and the next page.Size are
// returned.
//
// HasNext is observational: it is true only when a kept item beyond the
// requested page was actually seen. A walk that stops because it reached
// maxPages reports HasNext=false, so a capped result reads as "that is all we
// can offer" rather than dangling a page the caller could never fill. The
// upstream error is returned unwrapped so the provider sentinel mapping in
// api.classify still matches.
func FilterWalk[T any](
	ctx context.Context,
	page Page,
	maxPages int,
	fetch func(ctx context.Context, upstreamPage int) ([]T, bool, error),
	keep func(T) bool,
) (Slice[T], error) {
	page = page.Normalize()
	skip := (page.Number - 1) * page.Size
	out := make([]T, 0, page.Size)
	for p := 1; p <= maxPages; p++ {
		items, hasNext, err := fetch(ctx, p)
		if err != nil {
			return Slice[T]{}, err
		}
		for _, item := range items {
			if !keep(item) {
				continue
			}
			if skip > 0 {
				skip--
				continue
			}
			if len(out) == page.Size {
				return Slice[T]{Items: out, HasNext: true}, nil
			}
			out = append(out, item)
		}
		if !hasNext {
			return Slice[T]{Items: out, HasNext: false}, nil
		}
	}
	return Slice[T]{Items: out, HasNext: false}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd apps/backend && go test ./internal/provider/ -run TestFilterWalk -v`
Expected: PASS (8 subtests plus the two standalone tests).

- [ ] **Step 5: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/backend/internal/provider/filterwalk.go apps/backend/internal/provider/filterwalk_test.go
git commit -m "feat(provider): add FilterWalk for client-side paged filtering"
```

---

### Task 3: Repository search through the provider layer

Changing `GitProvider.ListRepositories` breaks every implementation and caller
at once, so interface, fake, GitHub, GitLab, and the API call site move in one
commit. The API-layer *validation* is Task 5; here the handler simply forwards
the raw `?search` value.

**Files:**
- Modify: `apps/backend/internal/provider/provider.go`
- Modify: `apps/backend/internal/provider/fake/fake.go`
- Modify: `apps/backend/internal/provider/github/client.go`
- Modify: `apps/backend/internal/provider/gitlab/client.go`
- Modify: `apps/backend/internal/api/repositories.go:62-79`
- Create: `apps/backend/internal/provider/github/testdata/repos_p1.json`, `repos_p2.json`
- Create: `apps/backend/internal/provider/gitlab/testdata/projects_search.json`
- Test: `apps/backend/internal/provider/github/client_test.go`, `apps/backend/internal/provider/gitlab/client_test.go`, `apps/backend/internal/provider/fake/fake_test.go` (create if absent)

**Interfaces:**
- Consumes: `provider.FilterWalk` (Task 2).
- Produces: `GitProvider.ListRepositories(ctx context.Context, search string, page Page) (Slice[Repository], error)` on every implementation; `github.maxSearchPages = 10`.

- [ ] **Step 1: Write the failing tests**

Append to `apps/backend/internal/provider/github/client_test.go`:

```go
func TestListRepositoriesSearchWalksPages(t *testing.T) {
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.URL.Path, r.URL.RawQuery})
		if r.URL.Path != "/user/repos" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("per_page = %s, want 100", r.URL.Query().Get("per_page"))
		}
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", `<http://x/user/repos?page=2>; rel="next"`)
			_, _ = w.Write(fixture(t, "repos_p1.json"))
			return
		}
		_, _ = w.Write(fixture(t, "repos_p2.json"))
	}))
	defer srv.Close()
	c := New("gh", "GitHub", srv.URL, config.Secret("ghp_test"), srv.Client(), nil)

	got, err := c.ListRepositories(context.Background(), "serv", provider.Page{Number: 1, Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(got.Items))
	for _, r := range got.Items {
		names = append(names, r.FullName())
	}
	if len(names) != 2 || names[0] != "atlas/server" || names[1] != "atlas/server-tools" {
		t.Fatalf("names = %v, want [atlas/server atlas/server-tools]", names)
	}
	if got.HasNext {
		t.Errorf("hasNext = true, want false (only two matches exist)")
	}
	if len(calls) != 2 {
		t.Errorf("upstream calls = %d, want 2", len(calls))
	}
}

func TestListRepositoriesWithoutSearchIsUnchanged(t *testing.T) {
	srv, calls := newServer(t)
	defer srv.Close()
	c := New("gh", "GitHub", srv.URL, config.Secret("ghp_test"), srv.Client(), nil)
	if _, err := c.ListRepositories(context.Background(), "", provider.Page{Number: 1, Size: 30}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v, want exactly one", *calls)
	}
	if !strings.Contains((*calls)[0].query, "per_page=30") {
		t.Errorf("query = %s, want per_page=30 (the caller's page size, not the walk size)", (*calls)[0].query)
	}
}
```

Append to `apps/backend/internal/provider/gitlab/client_test.go`:

```go
func TestListRepositoriesSearchQuery(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write(fixture(t, "projects_search.json"))
	}))
	defer srv.Close()
	c := New("gl", "GitLab", srv.URL, config.Secret("glpat"), srv.Client())

	res, err := c.ListRepositories(context.Background(), "serv", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].FullName() != "atlas/server" {
		t.Fatalf("items = %+v", res.Items)
	}
	for _, want := range []string{"search=serv", "search_namespaces=true", "membership=true", "per_page=30"} {
		if !strings.Contains(got, want) {
			t.Errorf("query %q missing %q", got, want)
		}
	}
}

func TestListRepositoriesWithoutSearchSendsNoSearchParam(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	c := New("gl", "GitLab", srv.URL, config.Secret("glpat"), srv.Client())
	if _, err := c.ListRepositories(context.Background(), "", provider.Page{Number: 1, Size: 30}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "search") {
		t.Errorf("query = %s, want no search parameter", got)
	}
}
```

Create `apps/backend/internal/provider/fake/fake_test.go`:

```go
package fake

import (
	"context"
	"testing"

	"github.com/jtumidanski/converge/internal/provider"
)

func repo(t *testing.T, fullName string) provider.Repository {
	t.Helper()
	r, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName(fullName).
		SetDefaultBranch("main").SetCloneURL("https://example.test/" + fullName + ".git").Build()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestListRepositoriesFiltersBySubstring(t *testing.T) {
	p := New("fake", provider.KindGitLab)
	p.AddRepository(repo(t, "atlas/server"))
	p.AddRepository(repo(t, "atlas/client"))
	p.AddRepository(repo(t, "other/SERVER-tools"))

	got, err := p.ListRepositories(context.Background(), "serv", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2 (case-insensitive substring)", len(got.Items))
	}
	if got.Items[0].FullName() != "atlas/server" || got.Items[1].FullName() != "other/SERVER-tools" {
		t.Errorf("items = %+v, want sorted by full name", got.Items)
	}
}

func TestListRepositoriesEmptySearchReturnsAll(t *testing.T) {
	p := New("fake", provider.KindGitLab)
	p.AddRepository(repo(t, "atlas/server"))
	p.AddRepository(repo(t, "atlas/client"))
	got, err := p.ListRepositories(context.Background(), "", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(got.Items))
	}
}
```

Create `apps/backend/internal/provider/github/testdata/repos_p1.json`:

```json
[
  {"name":"server","full_name":"atlas/server","owner":{"login":"atlas"},"default_branch":"main","html_url":"https://github.test/atlas/server","clone_url":"https://github.test/atlas/server.git"},
  {"name":"client","full_name":"atlas/client","owner":{"login":"atlas"},"default_branch":"main","html_url":"https://github.test/atlas/client","clone_url":"https://github.test/atlas/client.git"}
]
```

Create `apps/backend/internal/provider/github/testdata/repos_p2.json`:

```json
[
  {"name":"server-tools","full_name":"atlas/server-tools","owner":{"login":"atlas"},"default_branch":"main","html_url":"https://github.test/atlas/server-tools","clone_url":"https://github.test/atlas/server-tools.git"},
  {"name":"docs","full_name":"atlas/docs","owner":{"login":"atlas"},"default_branch":"main","html_url":"https://github.test/atlas/docs","clone_url":"https://github.test/atlas/docs.git"}
]
```

Create `apps/backend/internal/provider/gitlab/testdata/projects_search.json`:

```json
[
  {"name":"server","path":"server","path_with_namespace":"atlas/server","namespace":{"full_path":"atlas"},"default_branch":"main","web_url":"https://gitlab.test/atlas/server","http_url_to_repo":"https://gitlab.test/atlas/server.git"}
]
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/backend && go test ./internal/provider/... 2>&1 | head -30`
Expected: FAIL — `too many arguments in call to c.ListRepositories` / `p.ListRepositories`.

- [ ] **Step 3: Change the interface**

In `internal/provider/provider.go`, replace the `ListRepositories` line:

```go
	// ListRepositories lists the token's repositories. search is an
	// already-trimmed, already-length-checked substring filter; empty means
	// no filter. Validation belongs to the API layer, not here.
	ListRepositories(ctx context.Context, search string, page Page) (Slice[Repository], error)
```

- [ ] **Step 4: Update the fake**

In `internal/provider/fake/fake.go`, replace `ListRepositories`:

```go
func (p *Provider) ListRepositories(_ context.Context, search string, page provider.Page) (provider.Slice[provider.Repository], error) {
	if err := p.takeErr(); err != nil {
		return provider.Slice[provider.Repository]{}, err
	}
	needle := strings.ToLower(search)
	p.mu.Lock()
	all := make([]provider.Repository, 0, len(p.repos))
	for _, r := range p.repos {
		if needle != "" && !strings.Contains(strings.ToLower(r.FullName()), needle) {
			continue
		}
		all = append(all, r)
	}
	p.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].FullName() < all[j].FullName() })
	return paginate(all, page), nil
}
```

- [ ] **Step 5: Update GitHub**

In `internal/provider/github/client.go`, add to the `const` block:

```go
	// maxSearchPages bounds the /user/repos walk used to answer a repository
	// search. GitHub's search API is rate limited to 30 requests/minute across
	// all search endpoints, which a debounced type-ahead can exhaust, and its
	// visibility rules differ from /user/repos. Walking the same listing the
	// unsearched browse view uses keeps the two consistent.
	maxSearchPages = 10
```

Replace `ListRepositories` with:

```go
// repoListQuery builds the /user/repos query for one upstream page.
func repoListQuery(perPage, page int) url.Values {
	return url.Values{
		"affiliation": {"owner,collaborator,organization_member"},
		"sort":        {"full_name"},
		"per_page":    {strconv.Itoa(perPage)},
		"page":        {strconv.Itoa(page)},
	}
}

// ListRepositories returns the caller's accessible repositories, optionally
// filtered by a case-insensitive substring of the full name.
func (c *Client) ListRepositories(ctx context.Context, search string, page provider.Page) (provider.Slice[provider.Repository], error) {
	page = page.Normalize()
	if search == "" {
		var raw []repoJSON
		hasNext, err := c.get(ctx, "/user/repos", repoListQuery(page.Size, page.Number), &raw)
		if err != nil {
			return provider.Slice[provider.Repository]{}, err
		}
		items, err := toRepositories(raw, c.id)
		if err != nil {
			return provider.Slice[provider.Repository]{}, err
		}
		return provider.Slice[provider.Repository]{Items: items, HasNext: hasNext}, nil
	}
	needle := strings.ToLower(search)
	return provider.FilterWalk(ctx, page, maxSearchPages,
		func(ctx context.Context, upstream int) ([]provider.Repository, bool, error) {
			var raw []repoJSON
			hasNext, err := c.get(ctx, "/user/repos", repoListQuery(providerPage, upstream), &raw)
			if err != nil {
				return nil, false, err
			}
			items, err := toRepositories(raw, c.id)
			if err != nil {
				return nil, false, err
			}
			return items, hasNext, nil
		},
		func(r provider.Repository) bool {
			return strings.Contains(strings.ToLower(r.FullName()), needle)
		})
}

func toRepositories(raw []repoJSON, providerID string) ([]provider.Repository, error) {
	items := make([]provider.Repository, 0, len(raw))
	for _, r := range raw {
		repo, err := r.toModel(providerID)
		if err != nil {
			return nil, err
		}
		items = append(items, repo)
	}
	return items, nil
}
```

Add `"strings"` to the `github/client.go` import block.

- [ ] **Step 6: Update GitLab**

In `internal/provider/gitlab/client.go`, replace `ListRepositories`:

```go
// ListRepositories lists projects the token is a member of, optionally
// filtered by search. search_namespaces widens GitLab's match from the
// project path alone to the full namespace path, so "atlas/serv" finds
// "atlas/server".
func (c *Client) ListRepositories(ctx context.Context, search string, page provider.Page) (provider.Slice[provider.Repository], error) {
	page = page.Normalize()
	q := url.Values{"membership": {"true"}, "order_by": {"path"}, "sort": {"asc"}, "per_page": {strconv.Itoa(page.Size)}, "page": {strconv.Itoa(page.Number)}}
	if search != "" {
		q.Set("search", search)
		q.Set("search_namespaces", "true")
	}
	var raw []projectJSON
	next, err := c.get(ctx, "/projects", q, &raw)
	if err != nil {
		return provider.Slice[provider.Repository]{}, err
	}
	items := make([]provider.Repository, 0, len(raw))
	for _, p := range raw {
		r, err := p.toModel(c.id)
		if err != nil {
			return provider.Slice[provider.Repository]{}, err
		}
		items = append(items, r)
	}
	return provider.Slice[provider.Repository]{Items: items, HasNext: next != ""}, nil
}
```

- [ ] **Step 7: Update the API call site**

In `internal/api/repositories.go`, inside `listRepositories`, replace the call:

```go
	res, err := p.ListRepositories(r.Context(), strings.TrimSpace(r.URL.Query().Get("search")), page)
```

Add `"strings"` to that file's import block. (Task 5 replaces this with the
validating `searchFrom`.)

- [ ] **Step 8: Run the tests to verify they pass**

Run: `cd apps/backend && go build ./... && go test ./internal/provider/... ./internal/api/ -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/backend/internal/provider apps/backend/internal/api/repositories.go
git commit -m "feat(provider): add repository search to ListRepositories"
```

---

### Task 4: `ListBranches` across the provider layer

**Files:**
- Modify: `apps/backend/internal/provider/provider.go`
- Modify: `apps/backend/internal/provider/fake/fake.go`
- Modify: `apps/backend/internal/provider/github/{client.go,mapping.go}`
- Modify: `apps/backend/internal/provider/gitlab/{client.go,mapping.go}`
- Create: `apps/backend/internal/provider/github/testdata/branches.json`
- Create: `apps/backend/internal/provider/gitlab/testdata/branches.json`
- Test: `apps/backend/internal/provider/{github,gitlab}/client_test.go`, `apps/backend/internal/provider/fake/fake_test.go`

**Interfaces:**
- Consumes: `provider.Branch`, `provider.NewBranchBuilder` (Task 1); `provider.FilterWalk`, `github.maxSearchPages` (Tasks 2–3).
- Produces: `GitProvider.ListBranches(ctx context.Context, repo Repository, search string, page Page) (Slice[Branch], error)`; `fake.(*Provider).AddBranch(fullName string, b provider.Branch)`.

- [ ] **Step 1: Write the failing tests**

Create `apps/backend/internal/provider/github/testdata/branches.json`:

```json
[
  {"name":"develop","commit":{"sha":"1111111111111111111111111111111111111111"}},
  {"name":"main","commit":{"sha":"2222222222222222222222222222222222222222"}},
  {"name":"release/1.0","commit":{"sha":"3333333333333333333333333333333333333333"}}
]
```

Create `apps/backend/internal/provider/gitlab/testdata/branches.json`:

```json
[
  {"name":"develop","default":false,"commit":{"id":"1111111111111111111111111111111111111111"}},
  {"name":"main","default":true,"commit":{"id":"2222222222222222222222222222222222222222"}}
]
```

Append to `apps/backend/internal/provider/github/client_test.go`:

```go
func TestListBranchesMapsDefaultFromRepository(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/atlas/server/branches" {
			w.WriteHeader(404)
			return
		}
		query = r.URL.RawQuery
		_, _ = w.Write(fixture(t, "branches.json"))
	}))
	defer srv.Close()
	c := New("gh", "GitHub", srv.URL, config.Secret("ghp_test"), srv.Client(), nil)
	repo, err := provider.NewRepositoryBuilder().SetProviderID("gh").SetFullName("atlas/server").
		SetDefaultBranch("main").SetCloneURL("https://github.test/atlas/server.git").Build()
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.ListBranches(context.Background(), repo, "", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(got.Items))
	}
	// GitHub's branch payload carries no default flag; it is derived from the
	// repository's DefaultBranch.
	var main provider.Branch
	for _, b := range got.Items {
		if b.Name() == "main" {
			main = b
		} else if b.IsDefault() {
			t.Errorf("%s reported as default", b.Name())
		}
	}
	if !main.IsDefault() || main.SHA() != "2222222222222222222222222222222222222222" {
		t.Errorf("main = %+v", main)
	}
	if !strings.Contains(query, "per_page=30") {
		t.Errorf("query = %s, want per_page=30", query)
	}
	if strings.Contains(query, "search") {
		t.Errorf("query = %s, want no search parameter (GitHub has none)", query)
	}
}

func TestListBranchesSearchFiltersClientSide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture(t, "branches.json"))
	}))
	defer srv.Close()
	c := New("gh", "GitHub", srv.URL, config.Secret("ghp_test"), srv.Client(), nil)
	repo, _ := provider.NewRepositoryBuilder().SetProviderID("gh").SetFullName("atlas/server").
		SetDefaultBranch("main").SetCloneURL("https://github.test/atlas/server.git").Build()

	got, err := c.ListBranches(context.Background(), repo, "rel", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Name() != "release/1.0" {
		t.Fatalf("items = %+v, want only release/1.0", got.Items)
	}
}
```

Append to `apps/backend/internal/provider/gitlab/client_test.go`:

```go
func TestListBranchesUsesServerSearch(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("X-Next-Page", "")
		_, _ = w.Write(fixture(t, "branches.json"))
	}))
	defer srv.Close()
	c := New("gl", "GitLab", srv.URL, config.Secret("glpat"), srv.Client())
	repo, err := provider.NewRepositoryBuilder().SetProviderID("gl").SetFullName("atlas/server").
		SetDefaultBranch("main").SetCloneURL("https://gitlab.test/atlas/server.git").Build()
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.ListBranches(context.Background(), repo, "ma", provider.Page{Number: 1, Size: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2 (GitLab filters server-side; the fixture is returned as-is)", len(got.Items))
	}
	if got.Items[1].Name() != "main" || !got.Items[1].IsDefault() {
		t.Errorf("main = %+v, want default true from the payload flag", got.Items[1])
	}
	for _, want := range []string{"search=ma", "per_page=50"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q missing %q", query, want)
		}
	}
}
```

Append to `apps/backend/internal/provider/fake/fake_test.go`:

```go
func branch(t *testing.T, name string, isDefault bool) provider.Branch {
	t.Helper()
	b, err := provider.NewBranchBuilder().SetName(name).SetDefault(isDefault).Build()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestListBranchesOrdersDefaultFirstThenByName(t *testing.T) {
	p := New("fake", provider.KindGitLab)
	r := repo(t, "atlas/server")
	p.AddRepository(r)
	p.AddBranch("atlas/server", branch(t, "release/1.0", false))
	p.AddBranch("atlas/server", branch(t, "develop", false))
	p.AddBranch("atlas/server", branch(t, "main", true))

	got, err := p.ListBranches(context.Background(), r, "", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, b := range got.Items {
		names = append(names, b.Name())
	}
	want := []string{"main", "develop", "release/1.0"}
	for i := range want {
		if i >= len(names) || names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}

func TestListBranchesFiltersBySubstring(t *testing.T) {
	p := New("fake", provider.KindGitLab)
	r := repo(t, "atlas/server")
	p.AddRepository(r)
	p.AddBranch("atlas/server", branch(t, "main", true))
	p.AddBranch("atlas/server", branch(t, "release/1.0", false))

	got, err := p.ListBranches(context.Background(), r, "rel", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Name() != "release/1.0" {
		t.Fatalf("items = %+v", got.Items)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/backend && go test ./internal/provider/... 2>&1 | head -20`
Expected: FAIL — `c.ListBranches undefined`, `p.AddBranch undefined`.

- [ ] **Step 3: Extend the interface**

In `internal/provider/provider.go`, after `ListRepositories`:

```go
	// ListBranches lists repo's branches. search is an already-trimmed,
	// already-length-checked substring filter; empty means no filter.
	ListBranches(ctx context.Context, repo Repository, search string, page Page) (Slice[Branch], error)
```

- [ ] **Step 4: Implement on the fake**

In `internal/provider/fake/fake.go`, add a `branches map[string][]provider.Branch` field to `Provider`, initialise it in `New`, and add:

```go
// AddBranch registers a branch under fullName.
func (p *Provider) AddBranch(fullName string, b provider.Branch) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.branches[fullName] = append(p.branches[fullName], b)
}

func (p *Provider) ListBranches(_ context.Context, repo provider.Repository, search string, page provider.Page) (provider.Slice[provider.Branch], error) {
	if err := p.takeErr(); err != nil {
		return provider.Slice[provider.Branch]{}, err
	}
	needle := strings.ToLower(search)
	p.mu.Lock()
	all := make([]provider.Branch, 0, len(p.branches[repo.FullName()]))
	for _, b := range p.branches[repo.FullName()] {
		if needle != "" && !strings.Contains(strings.ToLower(b.Name()), needle) {
			continue
		}
		all = append(all, b)
	}
	p.mu.Unlock()
	sort.Slice(all, func(i, j int) bool {
		if all[i].IsDefault() != all[j].IsDefault() {
			return all[i].IsDefault()
		}
		return all[i].Name() < all[j].Name()
	})
	return paginate(all, page), nil
}
```

- [ ] **Step 5: Implement on GitHub**

Add to `internal/provider/github/mapping.go`:

```go
type branchJSON struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// toModel derives isDefault from the repository, because GitHub's branch
// payload carries no default flag.
func (b branchJSON) toModel(defaultBranch string) (provider.Branch, error) {
	return provider.NewBranchBuilder().
		SetName(b.Name).
		SetSHA(b.Commit.SHA).
		SetDefault(b.Name == defaultBranch).
		Build()
}
```

Add to `internal/provider/github/client.go`:

```go
// ListBranches lists repo's branches. The GitHub branches endpoint has no name
// filter, so a search walks pages and filters client-side.
func (c *Client) ListBranches(ctx context.Context, repo provider.Repository, search string, page provider.Page) (provider.Slice[provider.Branch], error) {
	if err := gitx.ValidateRepoFullName(repo.FullName()); err != nil {
		return provider.Slice[provider.Branch]{}, err
	}
	page = page.Normalize()
	path := "/repos/" + repo.FullName() + "/branches"
	fetch := func(ctx context.Context, perPage, upstream int) ([]provider.Branch, bool, error) {
		q := url.Values{"per_page": {strconv.Itoa(perPage)}, "page": {strconv.Itoa(upstream)}}
		var raw []branchJSON
		hasNext, err := c.get(ctx, path, q, &raw)
		if err != nil {
			return nil, false, err
		}
		items := make([]provider.Branch, 0, len(raw))
		for _, b := range raw {
			br, err := b.toModel(repo.DefaultBranch())
			if err != nil {
				return nil, false, err
			}
			items = append(items, br)
		}
		return items, hasNext, nil
	}
	if search == "" {
		items, hasNext, err := fetch(ctx, page.Size, page.Number)
		if err != nil {
			return provider.Slice[provider.Branch]{}, err
		}
		return provider.Slice[provider.Branch]{Items: items, HasNext: hasNext}, nil
	}
	needle := strings.ToLower(search)
	return provider.FilterWalk(ctx, page, maxSearchPages,
		func(ctx context.Context, upstream int) ([]provider.Branch, bool, error) {
			return fetch(ctx, providerPage, upstream)
		},
		func(b provider.Branch) bool { return strings.Contains(strings.ToLower(b.Name()), needle) })
}
```

- [ ] **Step 6: Implement on GitLab**

Add to `internal/provider/gitlab/mapping.go`:

```go
type branchJSON struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
	Commit  struct {
		ID string `json:"id"`
	} `json:"commit"`
}

func (b branchJSON) toModel() (provider.Branch, error) {
	return provider.NewBranchBuilder().SetName(b.Name).SetSHA(b.Commit.ID).SetDefault(b.Default).Build()
}
```

Add to `internal/provider/gitlab/client.go`:

```go
// ListBranches lists repo's branches. GitLab's search is a substring match
// with optional ^/$ anchors; both are harmless and are passed through.
func (c *Client) ListBranches(ctx context.Context, repo provider.Repository, search string, page provider.Page) (provider.Slice[provider.Branch], error) {
	if err := gitx.ValidateRepoFullName(repo.FullName()); err != nil {
		return provider.Slice[provider.Branch]{}, err
	}
	page = page.Normalize()
	q := url.Values{"per_page": {strconv.Itoa(page.Size)}, "page": {strconv.Itoa(page.Number)}}
	if search != "" {
		q.Set("search", search)
	}
	var raw []branchJSON
	next, err := c.get(ctx, projectPath(repo.FullName())+"/repository/branches", q, &raw)
	if err != nil {
		return provider.Slice[provider.Branch]{}, err
	}
	items := make([]provider.Branch, 0, len(raw))
	for _, b := range raw {
		br, err := b.toModel()
		if err != nil {
			return provider.Slice[provider.Branch]{}, err
		}
		items = append(items, br)
	}
	return provider.Slice[provider.Branch]{Items: items, HasNext: next != ""}, nil
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd apps/backend && go build ./... && go test ./internal/provider/... -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/backend/internal/provider
git commit -m "feat(provider): add ListBranches to the provider interface"
```

---

### Task 5: `INVALID_SEARCH` validation in the API layer

**Files:**
- Create: `apps/backend/internal/api/search.go`
- Modify: `apps/backend/internal/api/repositories.go`
- Test: `apps/backend/internal/api/api_test.go`

**Interfaces:**
- Produces: `api.maxSearchLen = 200`; `api.searchFrom(w http.ResponseWriter, r *http.Request) (string, bool)` — the bool reports "valid"; on rejection `searchFrom` has already written the 400 document, matching the inline `INVALID_STATE` style in `changes.go`.

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/api/api_test.go`:

```go
func TestRepositorySearchFilters(t *testing.T) {
	f := newAPIFixture(t)

	hit := do(t, f.handler, "GET", "/api/providers/fake/repositories?search=serv", "")
	if hit.Code != 200 {
		t.Fatalf("status = %d, body %s", hit.Code, hit.Body.String())
	}
	if items := decodeList(t, hit); len(items) != 1 || items[0]["id"] != "atlas/server" {
		t.Errorf("items = %v, want only atlas/server", items)
	}

	miss := do(t, f.handler, "GET", "/api/providers/fake/repositories?search=nothing-matches", "")
	if miss.Code != 200 {
		t.Fatalf("status = %d", miss.Code)
	}
	if items := decodeList(t, miss); len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

func TestRepositorySearchTooLong(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, "GET", "/api/providers/fake/repositories?search="+strings.Repeat("a", 201), "")
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	assertErrorCode(t, w, "INVALID_SEARCH")
}

func TestRepositorySearchIsTrimmed(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, "GET", "/api/providers/fake/repositories?search=%20%20serv%20%20", "")
	if w.Code != 200 {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if items := decodeList(t, w); len(items) != 1 {
		t.Errorf("items = %v, want the trimmed search to match atlas/server", items)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/backend && go test ./internal/api/ -run TestRepositorySearch -v`
Expected: FAIL — the 201-rune case returns 200 instead of 400.

- [ ] **Step 3: Add `searchFrom`**

Create `apps/backend/internal/api/search.go`:

```go
package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jtumidanski/converge/internal/jsonapi"
)

// maxSearchLen bounds ?search before it reaches a provider API. Measured in
// runes, not bytes, so a multi-byte query is judged by what the user typed.
const maxSearchLen = 200

// searchFrom trims ?search and reports whether it is acceptable. On rejection
// it has already written the 400 document, matching the inline INVALID_STATE
// style in changes.go.
func searchFrom(w http.ResponseWriter, r *http.Request) (string, bool) {
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if utf8.RuneCountInString(search) > maxSearchLen {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_SEARCH",
			jsonapi.StatusTitle(http.StatusBadRequest),
			"The search text is too long.")
		return "", false
	}
	return search, true
}
```

- [ ] **Step 4: Wire it into `listRepositories`**

In `internal/api/repositories.go`, replace the Task 3 call site:

```go
	search, ok := searchFrom(w, r)
	if !ok {
		return
	}
	page := pageFrom(r)
	res, err := p.ListRepositories(r.Context(), search, page)
```

Remove the now-unused `"strings"` import from `repositories.go`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd apps/backend && go test ./internal/api/ -run TestRepositorySearch -v`
Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/backend/internal/api/search.go apps/backend/internal/api/repositories.go apps/backend/internal/api/api_test.go
git commit -m "feat(api): validate ?search on the repository listing"
```

---

### Task 6: `GET /api/providers/{p}/repositories/{repo}/branches`

**Files:**
- Create: `apps/backend/internal/api/branches.go`
- Modify: `apps/backend/internal/api/router.go`
- Modify: `apps/backend/internal/api/api_test.go` (fixture seeds branches)
- Test: `apps/backend/internal/api/api_test.go`

**Interfaces:**
- Consumes: `provider.GitProvider.ListBranches` (Task 4), `searchFrom` (Task 5), `providerFor`, `repoNameFrom`, `pageFrom`, `writeDomainError`.
- Produces: resource type `branches`, id `<provider>:<repo>:<name>`, attributes `{name, isDefault, sha}`.

- [ ] **Step 1: Seed branches in the API fixture**

In `newAPIFixture` (`internal/api/api_test.go`), immediately after `p.AddRepository(repo)`:

```go
	for _, b := range []struct {
		name      string
		isDefault bool
	}{{"main", true}, {"develop", false}, {"release/1.0", false}} {
		br, err := provider.NewBranchBuilder().SetName(b.name).SetDefault(b.isDefault).Build()
		if err != nil {
			t.Fatal(err)
		}
		p.AddBranch("atlas/server", br)
	}
```

- [ ] **Step 2: Write the failing tests**

Append to `apps/backend/internal/api/api_test.go`:

```go
func TestListBranchesDefaultFirst(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, "GET", "/api/providers/fake/repositories/"+url.PathEscape("atlas/server")+"/branches", "")
	if w.Code != 200 {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	items := decodeList(t, w)
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	if items[0]["type"] != "branches" {
		t.Errorf("type = %v, want branches", items[0]["type"])
	}
	if items[0]["id"] != "fake:atlas/server:main" {
		t.Errorf("id = %v, want fake:atlas/server:main", items[0]["id"])
	}
	attrs, _ := items[0]["attributes"].(map[string]any)
	if attrs["name"] != "main" || attrs["isDefault"] != true {
		t.Errorf("attributes = %v, want main/default", attrs)
	}
}

func TestListBranchesPages(t *testing.T) {
	f := newAPIFixture(t)
	target := "/api/providers/fake/repositories/" + url.PathEscape("atlas/server") + "/branches?page=1&pageSize=2"
	w := do(t, f.handler, "GET", target, "")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if items := decodeList(t, w); len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	var doc struct {
		Meta struct {
			Page struct {
				Number  int  `json:"number"`
				Size    int  `json:"size"`
				HasNext bool `json:"hasNext"`
			} `json:"page"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Meta.Page.Number != 1 || doc.Meta.Page.Size != 2 || !doc.Meta.Page.HasNext {
		t.Errorf("page meta = %+v", doc.Meta.Page)
	}
}

func TestListBranchesSearch(t *testing.T) {
	f := newAPIFixture(t)
	target := "/api/providers/fake/repositories/" + url.PathEscape("atlas/server") + "/branches?search=rel"
	w := do(t, f.handler, "GET", target, "")
	items := decodeList(t, w)
	if len(items) != 1 || items[0]["id"] != "fake:atlas/server:release/1.0" {
		t.Errorf("items = %v, want only release/1.0", items)
	}
}

func TestListBranchesUnknownRepository(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, "GET", "/api/providers/fake/repositories/"+url.PathEscape("atlas/missing")+"/branches", "")
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	assertErrorCode(t, w, "NOT_FOUND")
}

func TestListBranchesInvalidRepository(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, "GET", "/api/providers/fake/repositories/"+url.PathEscape("../etc")+"/branches", "")
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	assertErrorCode(t, w, "INVALID_REPOSITORY")
}

func TestListBranchesProviderAuthFailure(t *testing.T) {
	f := newAPIFixture(t)
	f.prov.FailWith(provider.ErrAuth)
	w := do(t, f.handler, "GET", "/api/providers/fake/repositories/"+url.PathEscape("atlas/server")+"/branches", "")
	if w.Code != 502 {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	assertErrorCode(t, w, "PROVIDER_AUTH")
}

func TestListBranchesSearchTooLong(t *testing.T) {
	f := newAPIFixture(t)
	target := "/api/providers/fake/repositories/" + url.PathEscape("atlas/server") + "/branches?search=" + strings.Repeat("a", 201)
	w := do(t, f.handler, "GET", target, "")
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	assertErrorCode(t, w, "INVALID_SEARCH")
}
```

Note: `TestListBranchesProviderAuthFailure` relies on `FailWith` consuming the
*first* provider call, which is `GetRepository`. Both paths classify to
`PROVIDER_AUTH`, so the assertion holds either way.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd apps/backend && go test ./internal/api/ -run TestListBranches -v`
Expected: FAIL — 404 `NOT_FOUND` "No such endpoint" for every case.

- [ ] **Step 4: Write the handler**

Create `apps/backend/internal/api/branches.go`:

```go
package api

import (
	"log/slog"
	"net/http"

	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
)

type branchAttributes struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
	SHA       string `json:"sha"`
}

func branchResource(providerID, repo string, b provider.Branch) jsonapi.Resource {
	return jsonapi.Resource{
		Type: "branches",
		ID:   providerID + ":" + repo + ":" + b.Name(),
		Attributes: branchAttributes{
			Name: b.Name(), IsDefault: b.IsDefault(), SHA: b.SHA(),
		},
	}
}

// listBranches serves GET /api/providers/{provider}/repositories/{repo}/branches.
//
// The repository is resolved first (as listChanges does) so an unknown
// repository is a 404 before any branch call is made, and page 1 puts the
// default branch first when it appears there -- the only ordering guarantee
// the backend makes. The UI independently pins the repository's defaultBranch
// at the top of its select, so that guarantee is sufficient.
func (s *server) listBranches(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	name, err := repoNameFrom(r)
	if err != nil {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_REPOSITORY", jsonapi.StatusTitle(http.StatusBadRequest), "The repository must look like owner/name.")
		return
	}
	search, ok := searchFrom(w, r)
	if !ok {
		return
	}
	repo, err := p.GetRepository(r.Context(), name)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	page := pageFrom(r)
	s.deps.Log.Debug("list branches",
		slog.String("provider", p.ID()), slog.String("repository", name),
		slog.Int("page", page.Number), slog.Int("pageSize", page.Size))
	res, err := p.ListBranches(r.Context(), repo, search, page)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	items := defaultFirst(res.Items, repo.DefaultBranch(), page.Number)
	out := make([]jsonapi.Resource, 0, len(items))
	for _, b := range items {
		out = append(out, branchResource(p.ID(), name, b))
	}
	meta := &jsonapi.Meta{Page: &jsonapi.PageMeta{Number: page.Number, Size: page.Size, HasNext: res.HasNext}}
	if err := jsonapi.WriteList(w, http.StatusOK, out, meta); err != nil {
		s.deps.Log.Error("write branches failed", "error", err)
	}
}

// defaultFirst moves the default branch to the front of page 1 without
// otherwise disturbing provider order. Later pages are returned untouched:
// reordering across pages would need the whole list, which is exactly what
// paging exists to avoid.
func defaultFirst(items []provider.Branch, defaultBranch string, pageNumber int) []provider.Branch {
	if pageNumber != 1 || defaultBranch == "" || len(items) < 2 {
		return items
	}
	for i, b := range items {
		if b.Name() != defaultBranch {
			continue
		}
		if i == 0 {
			return items
		}
		out := make([]provider.Branch, 0, len(items))
		out = append(out, items[i])
		out = append(out, items[:i]...)
		out = append(out, items[i+1:]...)
		return out
	}
	return items
}
```

- [ ] **Step 5: Register the route**

In `internal/api/router.go`, immediately after the changes route:

```go
	mux.HandleFunc("GET /api/providers/{provider}/repositories/{repo}/branches", s.listBranches)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd apps/backend && go test ./internal/api/ -run TestListBranches -v`
Expected: PASS (7 tests).

- [ ] **Step 7: Run the package suites and commit**

```bash
cd apps/backend && go vet ./... && go test -race -count=1 ./internal/api/ ./internal/provider/...
cd "$(git rev-parse --show-toplevel)"
git add apps/backend/internal/api
git commit -m "feat(api): add the repository branches endpoint"
```

---

### Task 7: Wider per-file diff context

**Files:**
- Modify: `apps/backend/internal/diff/diff.go:150`
- Test: `apps/backend/internal/diff/diff_test.go`

**Interfaces:**
- Produces: `diff.fileDiffContext = 40`; `FileContent` emits `git diff --find-renames -U40 <base> <head> -- <path>`.

**Why:** FR-34 asks for "Expand N unmodified lines" rows computed from the
already-returned diff. Git's default is three lines of context, which leaves
nothing to expand. A second full-file endpoint is ruled out by the PRD, so the
window widens instead. `WriteCombined` and `Summarize` are untouched, so the
downloadable diff and the `+adds −dels` totals do not change.

- [ ] **Step 1: Write the failing test**

Append to `apps/backend/internal/diff/diff_test.go`:

```go
// TestFileContentUsesWideContext pins the -U40 window the review page's fold
// rows depend on: with git's default of three context lines there would be no
// unmodified run long enough to collapse (design 3.5).
func TestFileContentUsesWideContext(t *testing.T) {
	var args []string
	fr := &gitx.FakeRunner{Handler: func(s gitx.Spec) (gitx.Result, error) {
		args = append([]string(nil), s.Args...)
		return gitx.Result{Stdout: []byte("diff --git a/a.txt b/a.txt\n")}, nil
	}}
	f := FileSummary{Path: "a.txt"}
	if _, err := FileContent(context.Background(), fr, t.TempDir(), strings.Repeat("a", 40), strings.Repeat("b", 40), f); err != nil {
		t.Fatal(err)
	}
	want := []string{"diff", "--find-renames", "-U40", strings.Repeat("a", 40), strings.Repeat("b", 40), "--", "a.txt"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %v, want %v", args, want)
		}
	}
}
```

Ensure `diff_test.go` imports `"context"`, `"strings"`, and `"github.com/jtumidanski/converge/internal/gitx"` (the last two are likely already present).

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/backend && go test ./internal/diff/ -run TestFileContentUsesWideContext -v`
Expected: FAIL — `args = [diff --find-renames aaa... bbb... -- a.txt]`, missing `-U40`.

- [ ] **Step 3: Widen the window**

In `internal/diff/diff.go`, add near `MaxFileDiffBytes`:

```go
// fileDiffContext is how many unchanged lines surround each hunk in a
// per-file diff. Git's default of 3 leaves nothing for the UI to fold and
// expand; 40 gives the review page real unmodified runs to collapse without a
// second request for full file contents. The combined diff (WriteCombined) and
// the numstat totals (Summarize) keep git's defaults, so nothing downstream of
// them changes.
const fileDiffContext = 40
```

Replace the `args` line in `FileContent`:

```go
	args := []string{"diff", "--find-renames", "-U" + strconv.Itoa(fileDiffContext), base, head, "--", f.Path}
```

Add `"strconv"` to the imports if it is not already there.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd apps/backend && go test ./internal/diff/ -count=1`
Expected: PASS — including the pre-existing truncation tests, which must stay green.

- [ ] **Step 5: Run the full backend suite and commit**

```bash
cd apps/backend && go test -race -count=1 ./... && go test -race -count=1 -tags integration ./...
cd "$(git rev-parse --show-toplevel)"
git add apps/backend/internal/diff
git commit -m "feat(diff): widen the per-file diff context to 40 lines"
```

---

# Phase B — Frontend foundations

### Task 8: Install the shadcn components this redesign needs

**Files:**
- Create: `src/components/ui/{sheet,breadcrumb,popover,command,progress,switch,separator,tooltip,alert-dialog,scroll-area}.tsx` (generated)
- Modify: `apps/frontend/package.json`, `package-lock.json` (`cmdk` arrives with `command`)

**Interfaces:**
- Produces: the shadcn primitives every later UI task imports from `@/components/ui/*`.

- [ ] **Step 1: Load Node and install**

```bash
export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22
cd apps/frontend
npx shadcn@latest add sheet breadcrumb popover command progress switch separator tooltip alert-dialog scroll-area
```

Accept overwrites only for files that do not already exist in `src/components/ui/`. If the CLI offers to overwrite `button.tsx`, `input.tsx`, `badge.tsx`, `checkbox.tsx`, `select.tsx`, `skeleton.tsx`, `table.tsx`, `collapsible.tsx`, or `dropdown-menu.tsx`, decline — those are already in the repo and in use.

- [ ] **Step 2: Verify the generated files compile and are formatted**

```bash
cd apps/frontend
npx prettier --write src/components/ui
npm run lint
npx tsc -b --noEmit
```

Expected: no errors. If `tsc` complains about a missing `cmdk` type, confirm `cmdk` landed in `package.json` dependencies; if not, `npm install cmdk`.

- [ ] **Step 3: Confirm the existing suite still passes**

Run: `cd apps/frontend && npm test`
Expected: PASS — adding unused components changes no behaviour.

- [ ] **Step 4: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/ui apps/frontend/package.json apps/frontend/package-lock.json
git commit -m "chore(frontend): add the shadcn primitives for the review flow redesign"
```

---

### Task 9: The localStorage store abstraction

**Files:**
- Create: `apps/frontend/src/lib/storage/store.ts`
- Test: `apps/frontend/src/lib/storage/__tests__/store.test.ts`

**Interfaces:**
- Produces:
  - `interface Store<T> { key: string; get(): T; set(next: T | ((prev: T) => T)): void; subscribe(cb: () => void): () => void }`
  - `createStore<T>(key: string, isValid: (v: unknown) => v is T, fallback: () => T): Store<T>`
  - `useStore<T>(store: Store<T>): [T, Store<T>["set"]]`
  - `removeKeys(predicate: (key: string) => boolean): void`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/lib/storage/__tests__/store.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { createStore, removeKeys } from "@/lib/storage/store";

const isNumbers = (v: unknown): v is number[] =>
  Array.isArray(v) && v.every((n) => typeof n === "number");

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("createStore", () => {
  it("returns the fallback when the key is absent", () => {
    const store = createStore("t.absent", isNumbers, () => [1]);
    expect(store.get()).toEqual([1]);
  });

  it("returns the fallback for malformed JSON", () => {
    localStorage.setItem("t.malformed", "{not json");
    const store = createStore("t.malformed", isNumbers, () => []);
    expect(store.get()).toEqual([]);
  });

  it("returns the fallback for a wrong-shaped value", () => {
    localStorage.setItem("t.shape", JSON.stringify(["a", "b"]));
    const store = createStore("t.shape", isNumbers, () => []);
    expect(store.get()).toEqual([]);
  });

  it("returns the fallback when storage throws on read", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("disabled");
    });
    const store = createStore("t.throws", isNumbers, () => [7]);
    expect(store.get()).toEqual([7]);
  });

  it("round-trips a written value", () => {
    const store = createStore("t.write", isNumbers, () => []);
    store.set([1, 2]);
    expect(store.get()).toEqual([1, 2]);
    expect(JSON.parse(localStorage.getItem("t.write") as string)).toEqual([1, 2]);
  });

  it("accepts an updater function", () => {
    const store = createStore("t.update", isNumbers, () => [1]);
    store.set((prev) => [...prev, 2]);
    expect(store.get()).toEqual([1, 2]);
  });

  it("notifies subscribers on write and stops after unsubscribe", () => {
    const store = createStore("t.subscribe", isNumbers, () => []);
    const seen = vi.fn();
    const unsubscribe = store.subscribe(seen);
    store.set([1]);
    expect(seen).toHaveBeenCalledTimes(1);
    unsubscribe();
    store.set([2]);
    expect(seen).toHaveBeenCalledTimes(1);
  });

  it("does not throw when storage rejects a write", () => {
    const store = createStore("t.quota", isNumbers, () => []);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    expect(() => store.set([1])).not.toThrow();
  });
});

describe("removeKeys", () => {
  it("removes only the keys matching the predicate", () => {
    localStorage.setItem("keep.me", "1");
    localStorage.setItem("drop.a", "1");
    localStorage.setItem("drop.b", "1");
    removeKeys((key) => key.startsWith("drop."));
    expect(localStorage.getItem("keep.me")).toBe("1");
    expect(localStorage.getItem("drop.a")).toBeNull();
    expect(localStorage.getItem("drop.b")).toBeNull();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22
cd apps/frontend && npx vitest run src/lib/storage
```
Expected: FAIL — cannot resolve `@/lib/storage/store`.

- [ ] **Step 3: Implement the store**

Create `apps/frontend/src/lib/storage/store.ts`:

```ts
import { useCallback, useSyncExternalStore } from "react";

export interface Store<T> {
  readonly key: string;
  get(): T;
  set(next: T | ((prev: T) => T)): void;
  subscribe(callback: () => void): () => void;
}

/**
 * createStore wraps one localStorage key behind a total reader.
 *
 * Every failure mode -- absent key, malformed JSON, right JSON of the wrong
 * shape, and a storage that throws outright (private browsing, disabled
 * storage, exhausted quota) -- collapses to `fallback()`. This is the same
 * contract lib/theme/storage.ts established; that module keeps its own copy
 * because of its boot-script coupling, which this abstraction must not disturb.
 *
 * The value is cached so `get()` is referentially stable between writes, which
 * useSyncExternalStore requires: re-parsing on every call would hand React a
 * new array identity each render and loop forever.
 */
export function createStore<T>(
  key: string,
  isValid: (value: unknown) => value is T,
  fallback: () => T,
): Store<T> {
  const listeners = new Set<() => void>();
  let cache: T | undefined;
  let loaded = false;

  function read(): T {
    try {
      const raw = localStorage.getItem(key);
      if (raw === null) return fallback();
      const parsed: unknown = JSON.parse(raw);
      return isValid(parsed) ? parsed : fallback();
    } catch {
      return fallback();
    }
  }

  function get(): T {
    if (!loaded) {
      cache = read();
      loaded = true;
    }
    return cache as T;
  }

  function emit(): void {
    for (const listener of listeners) listener();
  }

  function set(next: T | ((prev: T) => T)): void {
    const value = typeof next === "function" ? (next as (prev: T) => T)(get()) : next;
    cache = value;
    loaded = true;
    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch {
      // Best effort: the session stays correct in memory and a failed write
      // must never break the interaction that triggered it.
    }
    emit();
  }

  function subscribe(callback: () => void): () => void {
    listeners.add(callback);
    if (listeners.size === 1) {
      window.addEventListener("storage", onStorage);
    }
    return () => {
      listeners.delete(callback);
      if (listeners.size === 0) {
        window.removeEventListener("storage", onStorage);
      }
    };
  }

  // Another tab wrote this key: drop the cache so the next get() re-reads.
  function onStorage(event: StorageEvent): void {
    if (event.key !== null && event.key !== key) return;
    loaded = false;
    emit();
  }

  return { key, get, set, subscribe };
}

/** useStore binds a Store to React, re-rendering on every write. */
export function useStore<T>(store: Store<T>): [T, Store<T>["set"]] {
  const subscribe = useCallback((cb: () => void) => store.subscribe(cb), [store]);
  const getSnapshot = useCallback(() => store.get(), [store]);
  const value = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  const set = useCallback<Store<T>["set"]>((next) => store.set(next), [store]);
  return [value, set];
}

/** removeKeys deletes every localStorage key matching predicate. Total. */
export function removeKeys(predicate: (key: string) => boolean): void {
  try {
    const doomed: string[] = [];
    for (let i = 0; i < localStorage.length; i += 1) {
      const key = localStorage.key(i);
      if (key !== null && predicate(key)) doomed.push(key);
    }
    for (const key of doomed) localStorage.removeItem(key);
  } catch {
    // Storage unavailable; there is nothing to prune.
  }
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/lib/storage`
Expected: PASS (10 tests).

- [ ] **Step 5: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/lib/storage
git commit -m "feat(frontend): add a total localStorage store abstraction"
```

---

### Task 10: Recents, filter toggles, and viewed state

**Files:**
- Create: `apps/frontend/src/lib/storage/recents.ts`
- Create: `apps/frontend/src/lib/storage/changeFilters.ts`
- Create: `apps/frontend/src/lib/storage/viewed.ts`
- Test: `apps/frontend/src/lib/storage/__tests__/{recents,changeFilters,viewed}.test.ts`

**Interfaces:**
- Consumes: `createStore`, `removeKeys` (Task 9).
- Produces:
  - `RecentRepository { provider: string; repository: string; defaultBranch: string; openedAt: string }`
  - `recordRecent(entry: RecentRepository): void`, `recentsFor(provider: string, limit?: number): RecentRepository[]`, `mostRecentProvider(): string | undefined`, `recentsStore: Store<RecentRepository[]>`
  - `ChangeFilters { hideBots: boolean; groupByTicket: boolean }`, `changeFiltersStore: Store<ChangeFilters>`
  - `viewedStore(reviewId: string): Store<string[]>`, `toggleViewed(reviewId, path): void`, `clearViewed(reviewId: string): void`, `pruneViewed(activeIds: string[]): void`

- [ ] **Step 1: Write the failing tests**

Create `apps/frontend/src/lib/storage/__tests__/recents.test.ts`:

```ts
import { afterEach, describe, expect, it } from "vitest";
import {
  mostRecentProvider,
  recentsFor,
  recordRecent,
  RECENTS_KEY,
  type RecentRepository,
} from "@/lib/storage/recents";

function entry(repository: string, provider = "gl", openedAt = "2026-01-01T00:00:00Z"): RecentRepository {
  return { provider, repository, defaultBranch: "main", openedAt };
}

afterEach(() => localStorage.clear());

describe("recents", () => {
  it("uses the documented key", () => {
    expect(RECENTS_KEY).toBe("converge.recentRepositories");
  });

  it("stores the most recent entry first", () => {
    recordRecent(entry("atlas/a"));
    recordRecent(entry("atlas/b"));
    expect(recentsFor("gl").map((r) => r.repository)).toEqual(["atlas/b", "atlas/a"]);
  });

  it("moves an existing entry to the front instead of duplicating it", () => {
    recordRecent(entry("atlas/a"));
    recordRecent(entry("atlas/b"));
    recordRecent(entry("atlas/a", "gl", "2026-02-02T00:00:00Z"));
    const all = recentsFor("gl");
    expect(all.map((r) => r.repository)).toEqual(["atlas/a", "atlas/b"]);
    expect(all[0]?.openedAt).toBe("2026-02-02T00:00:00Z");
  });

  it("treats the same repository under a different provider as a distinct entry", () => {
    recordRecent(entry("atlas/a", "gl"));
    recordRecent(entry("atlas/a", "gh"));
    expect(recentsFor("gl")).toHaveLength(1);
    expect(recentsFor("gh")).toHaveLength(1);
  });

  it("caps the list at twenty entries", () => {
    for (let i = 0; i < 25; i += 1) recordRecent(entry(`atlas/r${i}`));
    expect(recentsFor("gl", 100)).toHaveLength(20);
    expect(recentsFor("gl", 100).at(-1)?.repository).toBe("atlas/r5");
  });

  it("limits the per-provider view to ten by default", () => {
    for (let i = 0; i < 15; i += 1) recordRecent(entry(`atlas/r${i}`));
    expect(recentsFor("gl")).toHaveLength(10);
  });

  it("derives the most recent provider from the first entry", () => {
    recordRecent(entry("atlas/a", "gl"));
    recordRecent(entry("atlas/b", "gh"));
    expect(mostRecentProvider()).toBe("gh");
  });

  it("reports no provider when there are no recents", () => {
    expect(mostRecentProvider()).toBeUndefined();
  });

  it("tolerates a malformed stored value", () => {
    localStorage.setItem(RECENTS_KEY, JSON.stringify([{ nope: true }]));
    expect(recentsFor("gl")).toEqual([]);
  });
});
```

Create `apps/frontend/src/lib/storage/__tests__/changeFilters.test.ts`:

```ts
import { afterEach, describe, expect, it } from "vitest";
import { CHANGE_FILTERS_KEY, changeFiltersStore } from "@/lib/storage/changeFilters";

afterEach(() => localStorage.clear());

describe("changeFilters", () => {
  it("uses the documented key", () => {
    expect(CHANGE_FILTERS_KEY).toBe("converge.changeFilters");
  });

  it("defaults to hiding bots and not grouping", () => {
    expect(changeFiltersStore.get()).toEqual({ hideBots: true, groupByTicket: false });
  });

  it("persists a toggle", () => {
    changeFiltersStore.set({ hideBots: false, groupByTicket: true });
    expect(JSON.parse(localStorage.getItem(CHANGE_FILTERS_KEY) as string)).toEqual({
      hideBots: false,
      groupByTicket: true,
    });
  });

  it("falls back to the defaults for a partial stored object", () => {
    localStorage.setItem(CHANGE_FILTERS_KEY, JSON.stringify({ hideBots: false }));
    // The store caches its first read, so exercise a fresh read path.
    changeFiltersStore.set(changeFiltersStore.get());
    expect(changeFiltersStore.get().groupByTicket).toBe(false);
  });
});
```

Create `apps/frontend/src/lib/storage/__tests__/viewed.test.ts`:

```ts
import { afterEach, describe, expect, it } from "vitest";
import { clearViewed, pruneViewed, toggleViewed, viewedKey, viewedStore } from "@/lib/storage/viewed";

afterEach(() => localStorage.clear());

describe("viewed", () => {
  it("uses the documented key shape", () => {
    expect(viewedKey("abc")).toBe("converge.viewed.abc");
  });

  it("starts empty and toggles a path on and off", () => {
    expect(viewedStore("r1").get()).toEqual([]);
    toggleViewed("r1", "a/b.ts");
    expect(viewedStore("r1").get()).toEqual(["a/b.ts"]);
    toggleViewed("r1", "a/b.ts");
    expect(viewedStore("r1").get()).toEqual([]);
  });

  it("returns the same store instance for the same review id", () => {
    expect(viewedStore("r1")).toBe(viewedStore("r1"));
  });

  it("keeps reviews independent", () => {
    toggleViewed("r1", "a.ts");
    toggleViewed("r2", "b.ts");
    expect(viewedStore("r1").get()).toEqual(["a.ts"]);
    expect(viewedStore("r2").get()).toEqual(["b.ts"]);
  });

  it("clears one review's state", () => {
    toggleViewed("r1", "a.ts");
    clearViewed("r1");
    expect(viewedStore("r1").get()).toEqual([]);
    expect(localStorage.getItem(viewedKey("r1"))).toBeNull();
  });

  it("prunes keys for reviews that are no longer active", () => {
    toggleViewed("live", "a.ts");
    toggleViewed("dead", "b.ts");
    localStorage.setItem("unrelated", "keep");
    pruneViewed(["live"]);
    expect(localStorage.getItem(viewedKey("live"))).not.toBeNull();
    expect(localStorage.getItem(viewedKey("dead"))).toBeNull();
    expect(localStorage.getItem("unrelated")).toBe("keep");
  });

  it("tolerates a malformed stored value", () => {
    localStorage.setItem(viewedKey("bad"), "{oops");
    expect(viewedStore("bad").get()).toEqual([]);
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/frontend && npx vitest run src/lib/storage`
Expected: FAIL — the three new modules do not resolve.

- [ ] **Step 3: Implement `recents.ts`**

```ts
import { createStore, type Store } from "@/lib/storage/store";

export const RECENTS_KEY = "converge.recentRepositories";

/** MAX_RECENTS caps the stored list; recentsFor caps what the drawer shows. */
const MAX_RECENTS = 20;
const DEFAULT_VISIBLE = 10;

export interface RecentRepository {
  provider: string;
  repository: string;
  defaultBranch: string;
  /** ISO timestamp of the most recent open. */
  openedAt: string;
}

function isRecent(value: unknown): value is RecentRepository {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.provider === "string" &&
    typeof v.repository === "string" &&
    typeof v.defaultBranch === "string" &&
    typeof v.openedAt === "string"
  );
}

function isRecents(value: unknown): value is RecentRepository[] {
  return Array.isArray(value) && value.every(isRecent);
}

export const recentsStore: Store<RecentRepository[]> = createStore(RECENTS_KEY, isRecents, () => []);

/**
 * recordRecent moves an entry to the front of the list, replacing any earlier
 * visit to the same (provider, repository) pair. The same repository under two
 * providers is two entries -- the pair, not the name, is the identity.
 */
export function recordRecent(entry: RecentRepository): void {
  recentsStore.set((previous) => {
    const rest = previous.filter(
      (r) => !(r.provider === entry.provider && r.repository === entry.repository),
    );
    return [entry, ...rest].slice(0, MAX_RECENTS);
  });
}

/** recentsFor returns this provider's recents, most recent first. */
export function recentsFor(provider: string, limit: number = DEFAULT_VISIBLE): RecentRepository[] {
  return recentsStore
    .get()
    .filter((r) => r.provider === provider)
    .slice(0, limit);
}

/** mostRecentProvider is the provider of the most recently opened repository. */
export function mostRecentProvider(): string | undefined {
  return recentsStore.get()[0]?.provider;
}
```

- [ ] **Step 4: Implement `changeFilters.ts`**

```ts
import { createStore, type Store } from "@/lib/storage/store";

export const CHANGE_FILTERS_KEY = "converge.changeFilters";

export interface ChangeFilters {
  /** Hide renovate/ and dependabot/ source branches. On by default (FR-21). */
  hideBots: boolean;
  /** Group the table by ticket key. Off by default (FR-23). */
  groupByTicket: boolean;
}

const DEFAULTS: ChangeFilters = { hideBots: true, groupByTicket: false };

function isChangeFilters(value: unknown): value is ChangeFilters {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return typeof v.hideBots === "boolean" && typeof v.groupByTicket === "boolean";
}

export const changeFiltersStore: Store<ChangeFilters> = createStore(
  CHANGE_FILTERS_KEY,
  isChangeFilters,
  () => ({ ...DEFAULTS }),
);
```

- [ ] **Step 5: Implement `viewed.ts`**

```ts
import { createStore, removeKeys, type Store } from "@/lib/storage/store";

const PREFIX = "converge.viewed.";

export function viewedKey(reviewId: string): string {
  return PREFIX + reviewId;
}

function isPaths(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((v) => typeof v === "string");
}

// One store per review id, cached so useStore always sees a stable identity
// for the same review -- a fresh store per render would resubscribe forever.
const stores = new Map<string, Store<string[]>>();

export function viewedStore(reviewId: string): Store<string[]> {
  const existing = stores.get(reviewId);
  if (existing) return existing;
  const store = createStore(viewedKey(reviewId), isPaths, () => []);
  stores.set(reviewId, store);
  return store;
}

/** toggleViewed adds or removes one file path from a review's viewed set. */
export function toggleViewed(reviewId: string, path: string): void {
  viewedStore(reviewId).set((previous) =>
    previous.includes(path) ? previous.filter((p) => p !== path) : [...previous, path],
  );
}

/** clearViewed drops a finished or discarded review's state entirely (FR-38). */
export function clearViewed(reviewId: string): void {
  const store = viewedStore(reviewId);
  store.set([]);
  try {
    localStorage.removeItem(viewedKey(reviewId));
  } catch {
    // Storage unavailable; the in-memory value is already empty.
  }
}

/**
 * pruneViewed removes viewed state for reviews the server no longer lists.
 * Called once per successful GET /api/reviews so an expired or swept review
 * cannot leave its file set behind forever.
 */
export function pruneViewed(activeIds: string[]): void {
  const active = new Set(activeIds.map(viewedKey));
  removeKeys((key) => key.startsWith(PREFIX) && !active.has(key));
  for (const [id] of stores) {
    if (!activeIds.includes(id)) stores.delete(id);
  }
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/lib/storage`
Expected: PASS (all storage suites).

- [ ] **Step 7: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/lib/storage
git commit -m "feat(frontend): add recents, change filters, and viewed-state storage"
```

---

### Task 11: Change derivations

**Files:**
- Create: `apps/frontend/src/lib/changes/{ticketKey.ts,dependencyBot.ts,applyOrder.ts,groupByTicket.ts,authors.ts}`
- Test: `apps/frontend/src/lib/changes/__tests__/changes.test.ts`

**Interfaces:**
- Consumes: `Change` from `@/types/models/change`.
- Produces:
  - `ticketKey(title: string): string | null`
  - `isDependencyBot(change: Change): boolean`
  - `applyOrder(changes: Iterable<Change>): Change[]`
  - `TicketGroup { key: string | null; label: string; changes: Change[] }`; `groupByTicket(changes: Change[]): TicketGroup[]`
  - `distinctAuthors(changes: Change[]): string[]`
  - `NO_TICKET_LABEL = "No ticket"`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/lib/changes/__tests__/changes.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { ticketKey } from "@/lib/changes/ticketKey";
import { isDependencyBot } from "@/lib/changes/dependencyBot";
import { applyOrder } from "@/lib/changes/applyOrder";
import { groupByTicket, NO_TICKET_LABEL } from "@/lib/changes/groupByTicket";
import { distinctAuthors } from "@/lib/changes/authors";
import type { Change } from "@/types/models/change";

function change(overrides: Partial<Change["attributes"]> & { number: number }): Change {
  return {
    type: "changes",
    id: String(overrides.number),
    attributes: {
      title: "Some change",
      author: "jsmith",
      sourceBranch: "feat/x",
      targetBranch: "main",
      mergedAt: "2026-01-01T00:00:00Z",
      createdAt: "2026-01-01T00:00:00Z",
      landingSha: null,
      webUrl: "https://example.test/1",
      ...overrides,
    },
  };
}

describe("ticketKey", () => {
  it.each([
    ["ATLAS-421 add a thing", "ATLAS-421"],
    ["fix(api): ATLAS-1 tidy up", "ATLAS-1"],
    ["ATLAS-1 and PROJ-2 together", "ATLAS-1"],
    ["AB1-99 mixed alphanumeric", "AB1-99"],
  ])("extracts %s", (title, expected) => {
    expect(ticketKey(title)).toBe(expected);
  });

  it.each([
    ["no ticket here"],
    ["lowercase-42 is not a key"],
    ["A-1 needs two leading letters"],
    ["ATLAS- missing the number"],
    ["ATLAS-x not a number"],
  ])("returns null for %s", (title) => {
    expect(ticketKey(title)).toBeNull();
  });
});

describe("isDependencyBot", () => {
  it.each([
    ["renovate/lodash-4.x", true],
    ["Renovate/Lodash", true],
    ["dependabot/npm_and_yarn/vite", true],
    ["DEPENDABOT/pip/x", true],
    ["feat/renovate-config", false],
    ["chore/deps", false],
    ["", false],
  ])("%s -> %s", (sourceBranch, expected) => {
    expect(isDependencyBot(change({ number: 1, sourceBranch }))).toBe(expected);
  });
});

describe("applyOrder", () => {
  it("sorts by merge time ascending", () => {
    const ordered = applyOrder([
      change({ number: 3, mergedAt: "2026-03-01T00:00:00Z" }),
      change({ number: 1, mergedAt: "2026-01-01T00:00:00Z" }),
      change({ number: 2, mergedAt: "2026-02-01T00:00:00Z" }),
    ]);
    expect(ordered.map((c) => c.attributes.number)).toEqual([1, 2, 3]);
  });

  it("breaks ties on merge time by ascending number", () => {
    const ordered = applyOrder([
      change({ number: 9, mergedAt: "2026-01-01T00:00:00Z" }),
      change({ number: 4, mergedAt: "2026-01-01T00:00:00Z" }),
    ]);
    expect(ordered.map((c) => c.attributes.number)).toEqual([4, 9]);
  });

  it("sorts a null merge time last", () => {
    const ordered = applyOrder([
      change({ number: 1, mergedAt: null }),
      change({ number: 2, mergedAt: "2026-01-01T00:00:00Z" }),
    ]);
    expect(ordered.map((c) => c.attributes.number)).toEqual([2, 1]);
  });

  it("sorts two null merge times by number", () => {
    const ordered = applyOrder([
      change({ number: 5, mergedAt: null }),
      change({ number: 2, mergedAt: null }),
    ]);
    expect(ordered.map((c) => c.attributes.number)).toEqual([2, 5]);
  });

  it("does not mutate its input and accepts any iterable", () => {
    const input = [change({ number: 2 }), change({ number: 1 })];
    const map = new Map(input.map((c) => [c.attributes.number, c]));
    expect(applyOrder(map.values()).map((c) => c.attributes.number)).toEqual([1, 2]);
    expect(input.map((c) => c.attributes.number)).toEqual([2, 1]);
  });
});

describe("groupByTicket", () => {
  it("orders groups by their newest change and puts No ticket last", () => {
    const groups = groupByTicket([
      change({ number: 1, title: "ATLAS-1 old", mergedAt: "2026-01-01T00:00:00Z" }),
      change({ number: 2, title: "untagged", mergedAt: "2026-05-01T00:00:00Z" }),
      change({ number: 3, title: "PROJ-9 newer", mergedAt: "2026-03-01T00:00:00Z" }),
    ]);
    expect(groups.map((g) => g.label)).toEqual(["PROJ-9", "ATLAS-1", NO_TICKET_LABEL]);
    expect(groups.at(-1)?.key).toBeNull();
  });

  it("keeps server order within a group", () => {
    const groups = groupByTicket([
      change({ number: 7, title: "ATLAS-1 b", mergedAt: "2026-01-01T00:00:00Z" }),
      change({ number: 3, title: "ATLAS-1 a", mergedAt: "2026-02-01T00:00:00Z" }),
    ]);
    expect(groups[0]?.changes.map((c) => c.attributes.number)).toEqual([7, 3]);
  });

  it("returns no groups for no changes", () => {
    expect(groupByTicket([])).toEqual([]);
  });

  it("omits the No ticket group when every change has a key", () => {
    const groups = groupByTicket([change({ number: 1, title: "ATLAS-1 x" })]);
    expect(groups).toHaveLength(1);
  });
});

describe("distinctAuthors", () => {
  it("returns each author once, sorted", () => {
    expect(
      distinctAuthors([
        change({ number: 1, author: "zoe" }),
        change({ number: 2, author: "adam" }),
        change({ number: 3, author: "zoe" }),
      ]),
    ).toEqual(["adam", "zoe"]);
  });

  it("skips empty authors", () => {
    expect(distinctAuthors([change({ number: 1, author: "" })])).toEqual([]);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/lib/changes`
Expected: FAIL — none of the five modules resolve.

- [ ] **Step 3: Implement the five modules**

`src/lib/changes/ticketKey.ts`:

```ts
/**
 * TICKET_KEY is the fixed pattern from FR-22: two or more leading uppercase
 * alphanumerics, a hyphen, then digits. It is deliberately not configurable.
 */
const TICKET_KEY = /\b[A-Z][A-Z0-9]+-\d+\b/;

/** ticketKey returns the first ticket key in a title, or null. */
export function ticketKey(title: string): string | null {
  return TICKET_KEY.exec(title)?.[0] ?? null;
}
```

`src/lib/changes/dependencyBot.ts`:

```ts
import type { Change } from "@/types/models/change";

const BOT_PREFIXES = ["renovate/", "dependabot/"];

/**
 * isDependencyBot keys off the source branch rather than the author, because
 * both tools are frequently configured to push under a human's token while
 * always using their own branch prefix (FR-21).
 */
export function isDependencyBot(change: Change): boolean {
  const branch = change.attributes.sourceBranch.toLowerCase();
  return BOT_PREFIXES.some((prefix) => branch.startsWith(prefix));
}
```

`src/lib/changes/applyOrder.ts`:

```ts
import type { Change } from "@/types/models/change";

function mergedAtMs(change: Change): number {
  const raw = change.attributes.mergedAt;
  if (raw === null) return Number.POSITIVE_INFINITY;
  const ms = Date.parse(raw);
  return Number.isNaN(ms) ? Number.POSITIVE_INFINITY : ms;
}

/**
 * applyOrder is the order changes are cherry-picked onto the base: oldest
 * merge first, ties broken by ascending number, unknown merge times last.
 * This is the order the selection bar numbers its chips in and the order sent
 * to POST /api/reviews.
 */
export function applyOrder(changes: Iterable<Change>): Change[] {
  return [...changes].sort((a, b) => {
    const delta = mergedAtMs(a) - mergedAtMs(b);
    if (delta !== 0 && Number.isFinite(delta)) return delta;
    return a.attributes.number - b.attributes.number;
  });
}
```

`src/lib/changes/groupByTicket.ts`:

```ts
import { ticketKey } from "@/lib/changes/ticketKey";
import type { Change } from "@/types/models/change";

export const NO_TICKET_LABEL = "No ticket";

export interface TicketGroup {
  /** null for the catch-all group. */
  key: string | null;
  label: string;
  changes: Change[];
}

function newestMs(changes: Change[]): number {
  let newest = Number.NEGATIVE_INFINITY;
  for (const change of changes) {
    const raw = change.attributes.mergedAt;
    if (raw === null) continue;
    const ms = Date.parse(raw);
    if (!Number.isNaN(ms) && ms > newest) newest = ms;
  }
  return newest;
}

/**
 * groupByTicket buckets changes by their ticket key, ordering groups by the
 * newest change in each and pinning the catch-all group last (FR-23). Within a
 * group, server order is preserved.
 */
export function groupByTicket(changes: Change[]): TicketGroup[] {
  const buckets = new Map<string, Change[]>();
  const untagged: Change[] = [];
  for (const change of changes) {
    const key = ticketKey(change.attributes.title);
    if (key === null) {
      untagged.push(change);
      continue;
    }
    const bucket = buckets.get(key);
    if (bucket) bucket.push(change);
    else buckets.set(key, [change]);
  }
  const groups: TicketGroup[] = [...buckets.entries()]
    .map(([key, items]) => ({ key, label: key, changes: items }))
    .sort((a, b) => newestMs(b.changes) - newestMs(a.changes));
  if (untagged.length > 0) {
    groups.push({ key: null, label: NO_TICKET_LABEL, changes: untagged });
  }
  return groups;
}
```

`src/lib/changes/authors.ts`:

```ts
import type { Change } from "@/types/models/change";

/**
 * distinctAuthors is derived from the currently loaded pages only, so loading
 * another page can add chips (FR-20).
 */
export function distinctAuthors(changes: Change[]): string[] {
  const seen = new Set<string>();
  for (const change of changes) {
    const author = change.attributes.author;
    if (author !== "") seen.add(author);
  }
  return [...seen].sort((a, b) => a.localeCompare(b));
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/lib/changes`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/lib/changes
git commit -m "feat(frontend): add ticket, bot, apply-order, and author derivations"
```

---

### Task 12: File tree, viewed progress, and provider links

**Files:**
- Create: `apps/frontend/src/lib/review/{fileTree.ts,progress.ts,providerLink.ts}`
- Test: `apps/frontend/src/lib/review/__tests__/{fileTree,progress,providerLink}.test.ts`

**Interfaces:**
- Consumes: `ReviewFile` from `@/types/models/reviewFile`, `Review` from `@/types/models/review`, `Repository` from `@/types/models/repository`.
- Produces:
  - `type TreeNode = { kind: "dir"; name: string; path: string; children: TreeNode[] } | { kind: "file"; file: ReviewFile }`
  - `buildTree(files: ReviewFile[]): TreeNode[]`
  - `flattenVisible(nodes: TreeNode[], collapsed: ReadonlySet<string>): string[]`
  - `filterTree(nodes: TreeNode[], query: string): TreeNode[]`
  - `ancestorDirs(path: string): string[]`
  - `viewedProgress(files: ReviewFile[], viewed: ReadonlySet<string>): { viewed: number; total: number; percent: number }`
  - `openInProviderHref(review: Review, repositoryWebUrl: string | undefined): string | undefined`

- [ ] **Step 1: Write the failing tests**

Create `apps/frontend/src/lib/review/__tests__/fileTree.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { ancestorDirs, buildTree, filterTree, flattenVisible } from "@/lib/review/fileTree";
import type { ReviewFile } from "@/types/models/reviewFile";

function file(path: string): ReviewFile {
  return {
    type: "review-files",
    id: path,
    attributes: {
      path,
      previousPath: "",
      status: "modified",
      additions: 1,
      deletions: 1,
      binary: false,
    },
  };
}

describe("buildTree", () => {
  it("collapses a chain of single-child directories into one segment", () => {
    const tree = buildTree([file("src/main/java/com/atlas/App.java")]);
    expect(tree).toHaveLength(1);
    const root = tree[0];
    expect(root?.kind).toBe("dir");
    if (root?.kind !== "dir") throw new Error("expected a directory");
    expect(root.name).toBe("src/main/java/com/atlas");
    expect(root.path).toBe("src/main/java/com/atlas");
    expect(root.children).toHaveLength(1);
    expect(root.children[0]?.kind).toBe("file");
  });

  it("stops collapsing where a directory branches", () => {
    const tree = buildTree([file("src/a/one.ts"), file("src/b/two.ts")]);
    expect(tree).toHaveLength(1);
    const root = tree[0];
    if (root?.kind !== "dir") throw new Error("expected a directory");
    expect(root.name).toBe("src");
    expect(root.children.map((c) => (c.kind === "dir" ? c.name : ""))).toEqual(["a", "b"]);
  });

  it("does not collapse a directory that also holds a file", () => {
    const tree = buildTree([file("src/index.ts"), file("src/lib/util.ts")]);
    const root = tree[0];
    if (root?.kind !== "dir") throw new Error("expected a directory");
    expect(root.name).toBe("src");
    expect(root.children).toHaveLength(2);
  });

  it("sorts directories before files, each by name", () => {
    const tree = buildTree([file("z.ts"), file("a.ts"), file("dir/b.ts")]);
    expect(
      tree.map((n) => (n.kind === "dir" ? `dir:${n.name}` : `file:${n.file.attributes.path}`)),
    ).toEqual(["dir:dir", "file:a.ts", "file:z.ts"]);
  });

  it("puts root-level files at the top level", () => {
    const tree = buildTree([file("README.md")]);
    expect(tree).toHaveLength(1);
    expect(tree[0]?.kind).toBe("file");
  });

  it("returns nothing for no files", () => {
    expect(buildTree([])).toEqual([]);
  });
});

describe("flattenVisible", () => {
  const tree = buildTree([file("src/a/one.ts"), file("src/b/two.ts"), file("root.ts")]);

  it("lists every file path in tree order when nothing is collapsed", () => {
    expect(flattenVisible(tree, new Set())).toEqual(["src/a/one.ts", "src/b/two.ts", "root.ts"]);
  });

  it("drops files under a collapsed directory", () => {
    expect(flattenVisible(tree, new Set(["src/a"]))).toEqual(["src/b/two.ts", "root.ts"]);
  });

  it("drops everything under a collapsed ancestor", () => {
    expect(flattenVisible(tree, new Set(["src"]))).toEqual(["root.ts"]);
  });
});

describe("filterTree", () => {
  const tree = buildTree([file("src/a/one.ts"), file("src/b/two.tsx")]);

  it("keeps only files matching the substring, with their ancestors", () => {
    expect(flattenVisible(filterTree(tree, "one"), new Set())).toEqual(["src/a/one.ts"]);
  });

  it("matches on the full path, not just the file name", () => {
    expect(flattenVisible(filterTree(tree, "src/b"), new Set())).toEqual(["src/b/two.tsx"]);
  });

  it("is case-insensitive", () => {
    expect(flattenVisible(filterTree(tree, "ONE"), new Set())).toEqual(["src/a/one.ts"]);
  });

  it("returns the tree unchanged for an empty query", () => {
    expect(filterTree(tree, "")).toBe(tree);
  });

  it("returns nothing when nothing matches", () => {
    expect(filterTree(tree, "zzz")).toEqual([]);
  });
});

describe("ancestorDirs", () => {
  it("lists every directory prefix of a path", () => {
    expect(ancestorDirs("src/a/b/one.ts")).toEqual(["src", "src/a", "src/a/b"]);
  });

  it("returns nothing for a root-level file", () => {
    expect(ancestorDirs("one.ts")).toEqual([]);
  });
});
```

Create `apps/frontend/src/lib/review/__tests__/progress.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { viewedProgress } from "@/lib/review/progress";
import type { ReviewFile } from "@/types/models/reviewFile";

function file(path: string): ReviewFile {
  return {
    type: "review-files",
    id: path,
    attributes: {
      path,
      previousPath: "",
      status: "modified",
      additions: 0,
      deletions: 0,
      binary: false,
    },
  };
}

describe("viewedProgress", () => {
  it("counts viewed files over the total", () => {
    const files = [file("a.ts"), file("b.ts"), file("c.ts")];
    expect(viewedProgress(files, new Set(["a.ts", "c.ts"]))).toEqual({
      viewed: 2,
      total: 3,
      percent: 67,
    });
  });

  it("ignores viewed paths that are no longer in the file list", () => {
    expect(viewedProgress([file("a.ts")], new Set(["a.ts", "gone.ts"]))).toEqual({
      viewed: 1,
      total: 1,
      percent: 100,
    });
  });

  it("reports zero percent for an empty file list rather than NaN", () => {
    expect(viewedProgress([], new Set())).toEqual({ viewed: 0, total: 0, percent: 0 });
  });
});
```

Create `apps/frontend/src/lib/review/__tests__/providerLink.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { openInProviderHref } from "@/lib/review/providerLink";
import type { IncludedChange, Review } from "@/types/models/review";

function included(number: number): IncludedChange {
  return {
    number,
    title: `#${number}`,
    author: "jsmith",
    mergedAt: "2026-01-01T00:00:00Z",
    webUrl: `https://example.test/mr/${number}`,
    strategy: "squash",
  };
}

function review(changes: IncludedChange[]): Review {
  return {
    type: "reviews",
    id: "r1",
    attributes: {
      status: "READY",
      stage: null,
      provider: "gl",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: null,
      headSha: null,
      baseDescription: "",
      changes: changes.map((c) => c.number),
      included: changes,
      totals: null,
      error: null,
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
      expiresAt: "2026-01-02T00:00:00Z",
    },
  };
}

describe("openInProviderHref", () => {
  it("links to the single included change", () => {
    expect(openInProviderHref(review([included(421)]), "https://example.test/atlas/server")).toBe(
      "https://example.test/mr/421",
    );
  });

  it("falls back to the repository when several changes are included", () => {
    // The files endpoint does not attribute files to changes, and per-line
    // attribution is a PRD non-goal, so there is no honest per-file target.
    expect(
      openInProviderHref(review([included(1), included(2)]), "https://example.test/atlas/server"),
    ).toBe("https://example.test/atlas/server");
  });

  it("returns undefined when neither target is known", () => {
    expect(openInProviderHref(review([]), undefined)).toBeUndefined();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/frontend && npx vitest run src/lib/review`
Expected: FAIL — the three modules do not resolve.

- [ ] **Step 3: Implement `fileTree.ts`**

```ts
import type { ReviewFile } from "@/types/models/reviewFile";

export type TreeNode =
  | { kind: "dir"; name: string; path: string; children: TreeNode[] }
  | { kind: "file"; file: ReviewFile };

interface MutableDir {
  name: string;
  path: string;
  dirs: Map<string, MutableDir>;
  files: ReviewFile[];
}

function emptyDir(name: string, path: string): MutableDir {
  return { name, path, dirs: new Map(), files: [] };
}

/**
 * buildTree turns a flat file list into a directory tree, then collapses any
 * directory whose only child is a directory into a single `a/b/c` row. Long
 * Java-style package paths are the reason this exists: without collapsing,
 * the tree is mostly one-child rungs and the file names are pushed off-screen.
 */
export function buildTree(files: ReviewFile[]): TreeNode[] {
  const root = emptyDir("", "");
  for (const file of files) {
    const segments = file.attributes.path.split("/");
    const fileName = segments.pop();
    if (fileName === undefined) continue;
    let cursor = root;
    for (const segment of segments) {
      const path = cursor.path === "" ? segment : `${cursor.path}/${segment}`;
      let next = cursor.dirs.get(segment);
      if (!next) {
        next = emptyDir(segment, path);
        cursor.dirs.set(segment, next);
      }
      cursor = next;
    }
    cursor.files.push(file);
  }
  return toNodes(root);
}

function toNodes(dir: MutableDir): TreeNode[] {
  const dirs = [...dir.dirs.values()]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map(collapse);
  const files: TreeNode[] = [...dir.files]
    .sort((a, b) => a.attributes.path.localeCompare(b.attributes.path))
    .map((file) => ({ kind: "file", file }) as const);
  return [...dirs, ...files];
}

function collapse(dir: MutableDir): TreeNode {
  let current = dir;
  let name = dir.name;
  // Only a directory holding exactly one directory and no files can fold into
  // its child; anything else would hide a sibling.
  while (current.files.length === 0 && current.dirs.size === 1) {
    const [only] = current.dirs.values();
    if (!only) break;
    name = `${name}/${only.name}`;
    current = only;
  }
  return { kind: "dir", name, path: current.path, children: toNodes(current) };
}

/**
 * flattenVisible is the file order j/k, the footer's Next file, and "File i of
 * n" all use: depth-first tree order, skipping anything under a collapsed
 * directory.
 */
export function flattenVisible(nodes: TreeNode[], collapsed: ReadonlySet<string>): string[] {
  const out: string[] = [];
  const walk = (list: TreeNode[]): void => {
    for (const node of list) {
      if (node.kind === "file") {
        out.push(node.file.attributes.path);
        continue;
      }
      if (collapsed.has(node.path)) continue;
      walk(node.children);
    }
  };
  walk(nodes);
  return out;
}

/**
 * filterTree keeps files whose full path contains query (case-insensitively)
 * along with the directories leading to them. An empty query returns the same
 * array identity so callers can memoise on it.
 */
export function filterTree(nodes: TreeNode[], query: string): TreeNode[] {
  const needle = query.trim().toLowerCase();
  if (needle === "") return nodes;
  const keep = (list: TreeNode[]): TreeNode[] => {
    const out: TreeNode[] = [];
    for (const node of list) {
      if (node.kind === "file") {
        if (node.file.attributes.path.toLowerCase().includes(needle)) out.push(node);
        continue;
      }
      const children = keep(node.children);
      if (children.length > 0) out.push({ ...node, children });
    }
    return out;
  };
  return keep(nodes);
}

/** ancestorDirs lists every directory prefix of a file path, outermost first. */
export function ancestorDirs(path: string): string[] {
  const segments = path.split("/");
  segments.pop();
  const out: string[] = [];
  let prefix = "";
  for (const segment of segments) {
    prefix = prefix === "" ? segment : `${prefix}/${segment}`;
    out.push(prefix);
  }
  return out;
}
```

Note: `collapse` folds onto the deepest single-child chain, so its `path` is
the *final* directory's path while its `name` is the joined label. Collapsed
rows are therefore addressed in `collapsed`/`ancestorDirs` by their deepest
path — which is exactly what `ancestorDirs` yields for the files beneath them.

- [ ] **Step 4: Implement `progress.ts`**

```ts
import type { ReviewFile } from "@/types/models/reviewFile";

export interface ViewedProgress {
  viewed: number;
  total: number;
  /** Rounded whole percent; 0 when there are no files (never NaN). */
  percent: number;
}

/**
 * viewedProgress counts only paths that are still in the file list, so a stale
 * localStorage entry for a file that no longer appears cannot push the counter
 * past the total.
 */
export function viewedProgress(
  files: ReviewFile[],
  viewed: ReadonlySet<string>,
): ViewedProgress {
  const total = files.length;
  let count = 0;
  for (const file of files) {
    if (viewed.has(file.attributes.path)) count += 1;
  }
  return { viewed: count, total, percent: total === 0 ? 0 : Math.round((count / total) * 100) };
}
```

- [ ] **Step 5: Implement `providerLink.ts`**

```ts
import type { Review } from "@/types/models/review";

/**
 * openInProviderHref resolves the "Open in <provider>" target for the file
 * header. The files endpoint does not attribute a file to the change that
 * introduced it, and per-line attribution is a PRD non-goal, so a single
 * included change gets its own web URL and anything else falls back to the
 * repository (design 2, second deviation).
 */
export function openInProviderHref(
  review: Review,
  repositoryWebUrl: string | undefined,
): string | undefined {
  const included = review.attributes.included;
  if (included.length === 1) {
    const only = included[0];
    if (only && only.webUrl !== "") return only.webUrl;
  }
  return repositoryWebUrl !== undefined && repositoryWebUrl !== "" ? repositoryWebUrl : undefined;
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/lib/review`
Expected: PASS (all subtests).

- [ ] **Step 7: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/lib/review
git commit -m "feat(frontend): add file-tree, viewed-progress, and provider-link helpers"
```

---

### Task 13: Keyboard shortcuts

**Files:**
- Create: `apps/frontend/src/lib/hotkeys/isEditableTarget.ts`
- Create: `apps/frontend/src/lib/hotkeys/useHotkeys.ts`
- Create: `apps/frontend/src/components/common/Hotkey.tsx`
- Test: `apps/frontend/src/lib/hotkeys/__tests__/hotkeys.test.tsx`

**Interfaces:**
- Produces:
  - `isEditableTarget(target: EventTarget | null): boolean`
  - `useHotkeys(bindings: Record<string, () => void>, options?: { enabled?: boolean }): void`
  - `<Hotkey>{"j"}</Hotkey>` — a `<kbd>` chip

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/lib/hotkeys/__tests__/hotkeys.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { isEditableTarget } from "@/lib/hotkeys/isEditableTarget";
import { useHotkeys } from "@/lib/hotkeys/useHotkeys";

function Harness({ onJ, enabled = true }: { onJ: () => void; enabled?: boolean }) {
  useHotkeys({ j: onJ }, { enabled });
  return (
    <div>
      <input aria-label="filter" />
      <textarea aria-label="notes" />
      <button type="button">plain</button>
    </div>
  );
}

describe("useHotkeys", () => {
  it("fires on a bare key press", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.keyboard("j");
    expect(onJ).toHaveBeenCalledTimes(1);
  });

  it("ignores unbound keys", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.keyboard("q");
    expect(onJ).not.toHaveBeenCalled();
  });

  it("does not fire while an input has focus", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.click(screen.getByLabelText("filter"));
    await userEvent.keyboard("j");
    expect(onJ).not.toHaveBeenCalled();
  });

  it("does not fire while a textarea has focus", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.click(screen.getByLabelText("notes"));
    await userEvent.keyboard("j");
    expect(onJ).not.toHaveBeenCalled();
  });

  it("still fires when a plain button has focus", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} />);
    await userEvent.click(screen.getByRole("button", { name: "plain" }));
    await userEvent.keyboard("j");
    expect(onJ).toHaveBeenCalledTimes(1);
  });

  it.each(["{Control>}j{/Control}", "{Meta>}j{/Meta}", "{Alt>}j{/Alt}"])(
    "ignores %s so browser shortcuts keep working",
    async (sequence) => {
      const onJ = vi.fn();
      render(<Harness onJ={onJ} />);
      await userEvent.keyboard(sequence);
      expect(onJ).not.toHaveBeenCalled();
    },
  );

  it("does nothing when disabled", async () => {
    const onJ = vi.fn();
    render(<Harness onJ={onJ} enabled={false} />);
    await userEvent.keyboard("j");
    expect(onJ).not.toHaveBeenCalled();
  });

  it("uses the latest handler without resubscribing", async () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = render(<Harness onJ={first} />);
    rerender(<Harness onJ={second} />);
    await userEvent.keyboard("j");
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("detaches its listener on unmount", async () => {
    const onJ = vi.fn();
    const { unmount } = render(<Harness onJ={onJ} />);
    unmount();
    await userEvent.keyboard("j");
    expect(onJ).not.toHaveBeenCalled();
  });
});

describe("isEditableTarget", () => {
  it("is false for null and for a plain div", () => {
    expect(isEditableTarget(null)).toBe(false);
    expect(isEditableTarget(document.createElement("div"))).toBe(false);
  });

  it.each(["input", "textarea", "select"])("is true for %s", (tag) => {
    expect(isEditableTarget(document.createElement(tag))).toBe(true);
  });

  it("is true for a contentEditable element", () => {
    const node = document.createElement("div");
    node.setAttribute("contenteditable", "true");
    document.body.append(node);
    expect(isEditableTarget(node)).toBe(true);
    node.remove();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/lib/hotkeys`
Expected: FAIL — modules do not resolve.

- [ ] **Step 3: Implement `isEditableTarget.ts`**

```ts
const EDITABLE_TAGS = new Set(["INPUT", "TEXTAREA", "SELECT"]);

/**
 * isEditableTarget reports whether a key press belongs to the user's typing
 * rather than to a page shortcut (FR-41). `isContentEditable` is only defined
 * for elements attached to a document, so the attribute is checked too.
 */
export function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (EDITABLE_TAGS.has(target.tagName)) return true;
  return target.isContentEditable || target.getAttribute("contenteditable") === "true";
}
```

- [ ] **Step 4: Implement `useHotkeys.ts`**

```ts
import { useEffect, useRef } from "react";
import { isEditableTarget } from "@/lib/hotkeys/isEditableTarget";

export interface HotkeyOptions {
  /** Pass false while a sheet, dialog, or popover owns the keyboard. */
  enabled?: boolean;
}

/**
 * useHotkeys binds single-key shortcuts on window.
 *
 * A binding is skipped when the hook is disabled, when any of ctrl/meta/alt is
 * held (those belong to the browser), when the event was already handled, or
 * when focus is inside something the user is typing into. Radix dialogs and
 * popovers already trap focus; callers additionally pass `enabled: false`
 * while one is open, which satisfies FR-41 from both directions.
 *
 * Bindings live in a ref so a caller can pass inline closures without the
 * listener resubscribing on every render.
 */
export function useHotkeys(bindings: Record<string, () => void>, options: HotkeyOptions = {}): void {
  const enabled = options.enabled ?? true;
  const bindingsRef = useRef(bindings);
  bindingsRef.current = bindings;

  useEffect(() => {
    if (!enabled) return;
    function onKeyDown(event: KeyboardEvent): void {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      if (event.defaultPrevented) return;
      if (isEditableTarget(event.target)) return;
      const handler = bindingsRef.current[event.key];
      if (!handler) return;
      event.preventDefault();
      handler();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enabled]);
}
```

- [ ] **Step 5: Implement the `Hotkey` chip**

Create `apps/frontend/src/components/common/Hotkey.tsx`:

```tsx
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/** Hotkey renders the <kbd> chip shown beside a shortcut-backed action. */
export function Hotkey({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-flex h-5 min-w-5 items-center justify-center rounded border border-border bg-muted px-1 font-mono text-[0.6875rem] text-muted-foreground",
        className,
      )}
    >
      {children}
    </kbd>
  );
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/lib/hotkeys`
Expected: PASS (all subtests).

- [ ] **Step 7: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/lib/hotkeys apps/frontend/src/components/common/Hotkey.tsx
git commit -m "feat(frontend): add single-key hotkey binding"
```

---

### Task 14: Repository input parsing, debouncing, and time-left

**Files:**
- Create: `apps/frontend/src/lib/repositoryInput.ts`
- Create: `apps/frontend/src/lib/timeLeft.ts`
- Create: `apps/frontend/src/lib/hooks/useDebouncedValue.ts`
- Delete: `apps/frontend/src/lib/schemas/repository.ts`, `apps/frontend/src/lib/schemas/__tests__/repository.test.ts`
- Test: `apps/frontend/src/lib/__tests__/{repositoryInput,timeLeft}.test.ts`, `apps/frontend/src/lib/hooks/__tests__/useDebouncedValue.test.ts`

**Interfaces:**
- Produces:
  - `parseRepositoryInput(text: string, baseUrl: string | undefined): string | null`
  - `isValidRepositoryName(text: string): boolean`
  - `timeLeft(expiresAt: string, now?: Date): string`
  - `useDebouncedValue<T>(value: T, delayMs: number): T`

`ManualRepositoryForm` is the only consumer of `lib/schemas/repository.ts`, and
it is deleted in Task 18; the rule it encoded moves here and grows URL support.

- [ ] **Step 1: Write the failing tests**

Create `apps/frontend/src/lib/__tests__/repositoryInput.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { isValidRepositoryName, parseRepositoryInput } from "@/lib/repositoryInput";

const BASE = "https://gitlab.example.test";

describe("parseRepositoryInput", () => {
  it.each([
    ["atlas/server", "atlas/server"],
    ["  atlas/server  ", "atlas/server"],
    ["group/sub/project", "group/sub/project"],
  ])("accepts the plain name %s", (input, expected) => {
    expect(parseRepositoryInput(input, BASE)).toBe(expected);
  });

  it.each([
    [`${BASE}/atlas/server`, "atlas/server"],
    [`${BASE}/atlas/server/`, "atlas/server"],
    [`${BASE}/atlas/server.git`, "atlas/server"],
    [`${BASE}/group/sub/project`, "group/sub/project"],
  ])("accepts the provider URL %s", (input, expected) => {
    expect(parseRepositoryInput(input, BASE)).toBe(expected);
  });

  it.each([
    ["", "empty"],
    ["server", "no slash"],
    ["atlas server", "contains a space"],
    ["../etc/passwd", "traversal"],
    ["atlas/../etc", "traversal in a segment"],
    ["-atlas/server", "leading dash"],
    ["/atlas/server", "leading slash"],
    ["https://other.test/atlas/server", "a different origin"],
  ])("rejects %s (%s)", (input) => {
    expect(parseRepositoryInput(input, BASE)).toBeNull();
  });

  it("rejects a provider URL with no path", () => {
    expect(parseRepositoryInput(BASE, BASE)).toBeNull();
  });

  it("rejects any URL when the provider base URL is unknown", () => {
    expect(parseRepositoryInput(`${BASE}/atlas/server`, undefined)).toBeNull();
  });
});

describe("isValidRepositoryName", () => {
  it("mirrors the backend owner/name rule", () => {
    expect(isValidRepositoryName("atlas/server")).toBe(true);
    expect(isValidRepositoryName("atlas")).toBe(false);
    expect(isValidRepositoryName("atlas/.hidden")).toBe(false);
  });
});
```

Create `apps/frontend/src/lib/__tests__/timeLeft.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { timeLeft } from "@/lib/timeLeft";

const now = new Date("2026-01-01T00:00:00Z");

describe("timeLeft", () => {
  it.each([
    ["2026-01-01T22:00:00Z", "22h left"],
    ["2026-01-01T01:00:00Z", "1h left"],
    ["2026-01-01T00:40:00Z", "40m left"],
    ["2026-01-01T00:00:30Z", "<1m left"],
  ])("formats %s as %s", (expiresAt, expected) => {
    expect(timeLeft(expiresAt, now)).toBe(expected);
  });

  it("rounds hours down so 90 minutes reads as 1h", () => {
    expect(timeLeft("2026-01-01T01:30:00Z", now)).toBe("1h left");
  });

  it("reports an elapsed expiry as expired, never a negative duration", () => {
    expect(timeLeft("2025-12-31T23:00:00Z", now)).toBe("expired");
  });

  it("returns an em dash for an unparseable timestamp", () => {
    expect(timeLeft("not a date", now)).toBe("—");
  });
});
```

Create `apps/frontend/src/lib/hooks/__tests__/useDebouncedValue.test.ts`:

```ts
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDebouncedValue } from "@/lib/hooks/useDebouncedValue";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("useDebouncedValue", () => {
  it("returns the initial value immediately", () => {
    const { result } = renderHook(() => useDebouncedValue("a", 250));
    expect(result.current).toBe("a");
  });

  it("holds the old value until the delay elapses", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: "a" },
    });
    rerender({ v: "b" });
    expect(result.current).toBe("a");
    act(() => void vi.advanceTimersByTime(249));
    expect(result.current).toBe("a");
    act(() => void vi.advanceTimersByTime(1));
    expect(result.current).toBe("b");
  });

  it("restarts the timer on every change, so only the last value lands", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), {
      initialProps: { v: "a" },
    });
    rerender({ v: "b" });
    act(() => void vi.advanceTimersByTime(200));
    rerender({ v: "c" });
    act(() => void vi.advanceTimersByTime(200));
    expect(result.current).toBe("a");
    act(() => void vi.advanceTimersByTime(50));
    expect(result.current).toBe("c");
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/frontend && npx vitest run src/lib/__tests__/repositoryInput.test.ts src/lib/__tests__/timeLeft.test.ts src/lib/hooks/__tests__/useDebouncedValue.test.ts`
Expected: FAIL — modules do not resolve.

- [ ] **Step 3: Implement `repositoryInput.ts`**

```ts
/** Mirrors gitx.ValidateRepoFullName: owner/name, no traversal, no leading -, . or /. */
const REPO_NAME = /^[A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)+$/;

export function isValidRepositoryName(text: string): boolean {
  if (!REPO_NAME.test(text)) return false;
  if (/^[-./]/.test(text)) return false;
  return text
    .split("/")
    .every((segment) => segment !== "" && segment !== "." && !segment.includes(".."));
}

/**
 * parseRepositoryInput turns what the reviewer typed or pasted into a
 * repository full name, or null when it is neither.
 *
 * Accepted: a bare `owner/name` (any depth, as GitLab subgroups need), and a
 * URL under the selected provider's base URL. A URL on any other origin is
 * rejected rather than guessed at -- resolving it against the wrong provider
 * would produce a confusing 404.
 */
export function parseRepositoryInput(text: string, baseUrl: string | undefined): string | null {
  const trimmed = text.trim();
  if (trimmed === "") return null;
  if (!trimmed.includes("://")) {
    return isValidRepositoryName(trimmed) ? trimmed : null;
  }
  if (baseUrl === undefined || baseUrl === "") return null;
  let url: URL;
  let base: URL;
  try {
    url = new URL(trimmed);
    base = new URL(baseUrl);
  } catch {
    return null;
  }
  if (url.origin !== base.origin) return null;
  const path = url.pathname.replace(/^\/+/, "").replace(/\/+$/, "").replace(/\.git$/, "");
  return path !== "" && isValidRepositoryName(path) ? path : null;
}
```

- [ ] **Step 4: Implement `timeLeft.ts`**

```ts
import { strings } from "@/lib/strings";

const MINUTE_MS = 60_000;
const HOUR_MS = 3_600_000;

/**
 * timeLeft renders an expiry as a compact "22h left" / "40m left" for the
 * reviews table. It never produces a negative duration: an elapsed expiry
 * reads as "expired", and an unparseable timestamp as an em dash, so a bad
 * value can never render "Invalid Date" in a table cell.
 */
export function timeLeft(expiresAt: string, now: Date = new Date()): string {
  const ms = Date.parse(expiresAt);
  if (Number.isNaN(ms)) return "—";
  const remaining = ms - now.getTime();
  if (remaining <= 0) return strings.expired;
  if (remaining >= HOUR_MS) return `${Math.floor(remaining / HOUR_MS)}h left`;
  if (remaining >= MINUTE_MS) return `${Math.floor(remaining / MINUTE_MS)}m left`;
  return "<1m left";
}
```

- [ ] **Step 5: Implement `useDebouncedValue.ts`**

```ts
import { useEffect, useState } from "react";

/**
 * useDebouncedValue trails `value` by `delayMs`, restarting on every change so
 * only the last value in a burst propagates. Type-ahead search uses this to
 * keep the provider page-walk off the critical path of every keystroke.
 */
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);
  return debounced;
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/lib/__tests__/repositoryInput.test.ts src/lib/__tests__/timeLeft.test.ts src/lib/hooks/__tests__/useDebouncedValue.test.ts`
Expected: PASS.

- [ ] **Step 7: Commit**

`lib/schemas/repository.ts` is deleted in Task 18 together with its only
consumer; leave it in place for now so the suite stays green.

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/lib/repositoryInput.ts apps/frontend/src/lib/timeLeft.ts apps/frontend/src/lib/hooks/useDebouncedValue.ts apps/frontend/src/lib/__tests__ apps/frontend/src/lib/hooks/__tests__
git commit -m "feat(frontend): add repository input parsing, time-left, and debouncing"
```

---

### Task 15: Branch types, service, and hooks; repository search plumbing

**Files:**
- Create: `apps/frontend/src/types/models/branch.ts`
- Create: `apps/frontend/src/services/api/branches.ts`
- Create: `apps/frontend/src/lib/hooks/api/useBranches.ts`
- Modify: `apps/frontend/src/services/api/repositories.ts`
- Modify: `apps/frontend/src/services/api/index.ts`
- Test: `apps/frontend/src/lib/hooks/api/__tests__/useBranches.test.tsx`, `apps/frontend/src/lib/hooks/api/__tests__/useRepositories.test.tsx`

**Interfaces:**
- Consumes: `apiGet`, `unwrapList`, `buildQuery`, `queryWrapper`, MSW `server`/`listDoc`.
- Produces:
  - `BranchAttributes { name: string; isDefault: boolean; sha: string }`; `Branch = Resource<"branches", BranchAttributes>`
  - `PagedBranches { items: Branch[]; page?: PageMeta }`; `BranchListParams { search?: string; page?: number; pageSize?: number }`
  - `branchesService.list(providerId, repository, params?): Promise<PagedBranches>`
  - `branchKeys.list(providerId, repository, params?)`; `useBranches(providerId, repository, params?, enabled?)`
  - `RepositoryListParams` gains `search?: string`

- [ ] **Step 1: Write the failing tests**

Create `apps/frontend/src/lib/hooks/api/__tests__/useBranches.test.tsx`:

```tsx
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { HttpResponse, http, listDoc, server } from "@/test/server";
import { queryWrapper } from "@/test/render";
import { useBranches } from "@/lib/hooks/api/useBranches";
import type { Branch } from "@/types/models/branch";

function branch(name: string, isDefault = false): Branch {
  return {
    type: "branches",
    id: `gl:atlas/server:${name}`,
    attributes: { name, isDefault, sha: "2222222222222222222222222222222222222222" },
  };
}

describe("useBranches", () => {
  it("requests the branches endpoint with the repository percent-encoded", async () => {
    let seen = "";
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", ({ request, params }) => {
        seen = `${String(params.repo)}?${new URL(request.url).searchParams.toString()}`;
        return HttpResponse.json(
          listDoc([branch("main", true)], { number: 1, size: 50, hasNext: false }),
        );
      }),
    );
    const { result } = renderHook(() => useBranches("gl", "atlas/server", { search: "ma" }), {
      wrapper: queryWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.items.map((b) => b.attributes.name)).toEqual(["main"]);
    expect(result.current.data?.page?.hasNext).toBe(false);
    expect(seen).toBe("atlas/server?search=ma");
  });

  it("omits an empty search from the query string", async () => {
    let seen = "";
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", ({ request }) => {
        seen = new URL(request.url).search;
        return HttpResponse.json(listDoc([branch("main", true)]));
      }),
    );
    const { result } = renderHook(() => useBranches("gl", "atlas/server", { search: "" }), {
      wrapper: queryWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(seen).toBe("");
  });

  it("stays disabled without a provider or repository", () => {
    const { result } = renderHook(() => useBranches(undefined, "atlas/server"), {
      wrapper: queryWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
  });

  it("can be disabled explicitly", () => {
    const { result } = renderHook(() => useBranches("gl", "atlas/server", {}, false), {
      wrapper: queryWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
  });
});
```

Append to `apps/frontend/src/lib/hooks/api/__tests__/useRepositories.test.tsx`:

```tsx
it("passes search through to the repositories endpoint", async () => {
  let seen = "";
  server.use(
    http.get("/api/providers/gl/repositories", ({ request }) => {
      seen = new URL(request.url).searchParams.toString();
      return HttpResponse.json(listDoc([]));
    }),
  );
  const { result } = renderHook(() => useRepositories("gl", { search: "serv", page: 1 }), {
    wrapper: queryWrapper(),
  });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(seen).toContain("search=serv");
});
```

(Keep whatever imports that file already uses; add `http`, `HttpResponse`, `listDoc`, `server` if they are not imported yet.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/frontend && npx vitest run src/lib/hooks/api`
Expected: FAIL — `@/lib/hooks/api/useBranches` does not resolve; the repositories test sees no `search`.

- [ ] **Step 3: Add the branch model**

Create `apps/frontend/src/types/models/branch.ts`:

```ts
import type { Resource } from "@/types/api/jsonapi";

export interface BranchAttributes {
  name: string;
  isDefault: boolean;
  /** The branch tip. Empty when the provider did not report one. */
  sha: string;
}

export type Branch = Resource<"branches", BranchAttributes>;
```

- [ ] **Step 4: Add the service**

Create `apps/frontend/src/services/api/branches.ts`:

```ts
import { apiGet } from "@/lib/api/client";
import { buildQuery } from "@/services/api/repositories";
import { unwrapList, type ListDocument, type PageMeta } from "@/types/api/jsonapi";
import type { Branch } from "@/types/models/branch";

export interface PagedBranches {
  items: Branch[];
  page?: PageMeta;
}

export interface BranchListParams {
  search?: string;
  page?: number;
  pageSize?: number;
}

export const branchesService = {
  async list(
    providerId: string,
    repository: string,
    params: BranchListParams = {},
    init: RequestInit = {},
  ): Promise<PagedBranches> {
    const query = buildQuery({
      search: params.search,
      page: params.page,
      pageSize: params.pageSize,
    });
    const doc = await apiGet<ListDocument<Branch>>(
      `/api/providers/${encodeURIComponent(providerId)}/repositories/${encodeURIComponent(repository)}/branches${query}`,
      init,
    );
    const items = unwrapList(doc);
    return doc.meta?.page ? { items, page: doc.meta.page } : { items };
  },
};
```

Re-export from `apps/frontend/src/services/api/index.ts`:

```ts
export * from "@/services/api/branches";
```

(Match the existing export style in that file — if it uses named re-exports, add `branchesService`, `type PagedBranches`, and `type BranchListParams` alongside them.)

- [ ] **Step 5: Add `search` to the repository list params**

In `apps/frontend/src/services/api/repositories.ts`:

```ts
export interface RepositoryListParams {
  search?: string;
  page?: number;
  pageSize?: number;
}
```

and in `repositoriesService.list`, extend the query — `buildQuery` already
drops `undefined` and `""`, so an empty search sends nothing:

```ts
      `/api/providers/${encodeURIComponent(providerId)}/repositories${query({ search: params.search, page: params.page, pageSize: params.pageSize })}`,
```

- [ ] **Step 6: Add the hook**

Create `apps/frontend/src/lib/hooks/api/useBranches.ts`:

```ts
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { branchesService, type BranchListParams } from "@/services/api";

export const branchKeys = {
  all: ["branches"] as const,
  lists: () => [...branchKeys.all, "list"] as const,
  list: (providerId: string, repository: string, params?: BranchListParams) =>
    [...branchKeys.lists(), providerId, repository, params ?? {}] as const,
};

/**
 * useBranches backs the base-branch picker. placeholderData keeps the previous
 * result on screen while a new search is in flight, so typing narrows the list
 * instead of flashing it empty; the AbortSignal React Query supplies is passed
 * straight through, which is what cancels a superseded request.
 */
export function useBranches(
  providerId: string | undefined,
  repository: string | undefined,
  params: BranchListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: branchKeys.list(providerId ?? "", repository ?? "", params),
    queryFn: ({ signal }) =>
      branchesService.list(providerId as string, repository as string, params, { signal }),
    enabled: enabled && Boolean(providerId) && Boolean(repository),
    staleTime: 2 * 60_000,
    placeholderData: keepPreviousData,
  });
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/lib/hooks/api src/services`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/types/models/branch.ts apps/frontend/src/services/api apps/frontend/src/lib/hooks/api
git commit -m "feat(frontend): add the branches service and repository search param"
```

---

### Task 16: App shell, brand block, breadcrumbs, and the shared container

**Files:**
- Modify: `apps/frontend/src/components/layout/AppShell.tsx`
- Create: `apps/frontend/src/components/layout/{BrandMark.tsx,Breadcrumbs.tsx}`
- Create: `apps/frontend/src/lib/breadcrumbs/{context.ts,useBreadcrumbs.ts}`
- Create: `apps/frontend/src/components/common/ProgressBar.tsx`
- Modify: `apps/frontend/src/lib/strings.ts`
- Test: `apps/frontend/src/components/layout/__tests__/AppShell.test.tsx`

**Interfaces:**
- Produces:
  - `BreadcrumbSegment { label: string; to?: string }`
  - `BreadcrumbContext` + `BreadcrumbProvider`
  - `useBreadcrumbs(segments: BreadcrumbSegment[]): void` — set on mount/update, clear on unmount
  - `<AppShell>` rendering `<main class="mx-auto w-full max-w-[80rem] px-6 py-6">`
  - `<ProgressBar value={number} label={string} />`
  - `strings.reviews = "Reviews"`

- [ ] **Step 1: Write the failing test**

Replace `apps/frontend/src/components/layout/__tests__/AppShell.test.tsx` with:

```tsx
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { useBreadcrumbs } from "@/lib/breadcrumbs/useBreadcrumbs";

function Publisher({ label }: { label: string }) {
  useBreadcrumbs([{ label: "Reviews", to: "/" }, { label }]);
  return <p>page body</p>;
}

function renderShell(children: React.ReactNode) {
  return render(
    <ThemeProvider>
      <MemoryRouter>
        <AppShell>{children}</AppShell>
      </MemoryRouter>
    </ThemeProvider>,
  );
}

describe("AppShell", () => {
  it("renders the brand block as a link to the root", () => {
    renderShell(<p>body</p>);
    const brand = screen.getByRole("link", { name: /converge/i });
    expect(brand).toHaveAttribute("href", "/");
  });

  it("keeps the theme toggle in the top bar", () => {
    renderShell(<p>body</p>);
    expect(screen.getByRole("button", { name: /theme/i })).toBeInTheDocument();
  });

  it("shows Reviews as the default breadcrumb when no page publishes one", () => {
    renderShell(<p>body</p>);
    expect(screen.getByRole("navigation", { name: /breadcrumb/i })).toHaveTextContent("Reviews");
  });

  it("renders the segments a page publishes", () => {
    renderShell(<Publisher label="atlas/server" />);
    const nav = screen.getByRole("navigation", { name: /breadcrumb/i });
    expect(nav).toHaveTextContent("Reviews");
    expect(nav).toHaveTextContent("atlas/server");
    expect(screen.getAllByRole("link", { name: "Reviews" })[0]).toHaveAttribute("href", "/");
  });

  it("falls back to the default once the publishing page unmounts", () => {
    const { rerender } = renderShell(<Publisher label="atlas/server" />);
    rerender(
      <ThemeProvider>
        <MemoryRouter>
          <AppShell>
            <p>body</p>
          </AppShell>
        </MemoryRouter>
      </ThemeProvider>,
    );
    expect(screen.getByRole("navigation", { name: /breadcrumb/i })).not.toHaveTextContent(
      "atlas/server",
    );
  });

  it("puts page content in the one shared centred container", () => {
    renderShell(<p>body</p>);
    const main = screen.getByRole("main");
    expect(main.className).toContain("mx-auto");
    expect(main.className).toContain("max-w-[80rem]");
    expect(main.className).toContain("px-6");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/components/layout`
Expected: FAIL — no breadcrumb navigation, no `max-w-[80rem]`.

- [ ] **Step 3: Add the breadcrumb context**

Create `apps/frontend/src/lib/breadcrumbs/context.ts`:

```ts
import { createContext } from "react";

export interface BreadcrumbSegment {
  label: string;
  /** When set, the segment renders as a link. The last segment never links. */
  to?: string;
}

export interface BreadcrumbContextValue {
  segments: BreadcrumbSegment[];
  setSegments: (segments: BreadcrumbSegment[]) => void;
}

/**
 * Breadcrumbs are published by pages rather than inferred from the URL: the
 * review breadcrumb needs loaded data (the repository and the included change
 * numbers), which the shell would otherwise have to fetch a second time.
 */
export const BreadcrumbContext = createContext<BreadcrumbContextValue | null>(null);
```

Create `apps/frontend/src/lib/breadcrumbs/useBreadcrumbs.ts`:

```ts
import { useContext, useEffect } from "react";
import { BreadcrumbContext, type BreadcrumbSegment } from "@/lib/breadcrumbs/context";

/**
 * useBreadcrumbs publishes this page's breadcrumb trail and clears it on
 * unmount so a route that publishes nothing falls back to the default.
 *
 * Pass a memoised array: the effect depends on identity, and a fresh array
 * every render would set state on every render.
 */
export function useBreadcrumbs(segments: BreadcrumbSegment[]): void {
  const context = useContext(BreadcrumbContext);
  const setSegments = context?.setSegments;
  useEffect(() => {
    if (!setSegments) return;
    setSegments(segments);
    return () => setSegments([]);
  }, [setSegments, segments]);
}
```

- [ ] **Step 4: Add the brand mark and breadcrumb renderer**

Create `apps/frontend/src/components/layout/BrandMark.tsx`:

```tsx
import { cn } from "@/lib/utils";

/** BrandMark is the square logo tile in the top bar's brand block. */
export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex h-6 w-6 items-center justify-center rounded-[0.3rem] bg-foreground text-[0.7rem] font-bold text-background",
        className,
      )}
    >
      C
    </span>
  );
}
```

Create `apps/frontend/src/components/layout/Breadcrumbs.tsx`:

```tsx
import { Fragment, useContext } from "react";
import { Link } from "react-router";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { BreadcrumbContext } from "@/lib/breadcrumbs/context";
import { strings } from "@/lib/strings";

/** Breadcrumbs renders whatever the current page published, defaulting to Reviews. */
export function Breadcrumbs() {
  const context = useContext(BreadcrumbContext);
  const published = context?.segments ?? [];
  const segments = published.length > 0 ? published : [{ label: strings.reviews }];
  return (
    <Breadcrumb>
      <BreadcrumbList>
        {segments.map((segment, index) => {
          const last = index === segments.length - 1;
          return (
            <Fragment key={`${segment.label}-${index}`}>
              <BreadcrumbItem>
                {segment.to !== undefined && !last ? (
                  <BreadcrumbLink asChild>
                    <Link to={segment.to}>{segment.label}</Link>
                  </BreadcrumbLink>
                ) : (
                  <BreadcrumbPage className="max-w-[28rem] truncate">
                    {segment.label}
                  </BreadcrumbPage>
                )}
              </BreadcrumbItem>
              {last ? null : <BreadcrumbSeparator />}
            </Fragment>
          );
        })}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
```

- [ ] **Step 5: Rewrite `AppShell`**

```tsx
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { ThemeToggle } from "@/components/theme/ThemeToggle";
import { BrandMark } from "@/components/layout/BrandMark";
import { Breadcrumbs } from "@/components/layout/Breadcrumbs";
import { BreadcrumbContext, type BreadcrumbSegment } from "@/lib/breadcrumbs/context";

/**
 * AppShell is the persistent chrome: a bordered brand block, the current
 * page's breadcrumb, and the theme control, above one centred container that
 * every route shares. Pages no longer set their own max width -- one width and
 * one horizontal padding for all three routes is the point of the redesign.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const [segments, setSegments] = useState<BreadcrumbSegment[]>([]);
  const value = useMemo(() => ({ segments, setSegments }), [segments]);
  return (
    <BreadcrumbContext.Provider value={value}>
      <div className="flex min-h-full flex-col">
        <header className="sticky top-0 z-40 flex h-14 items-center border-b border-border bg-background">
          <Link
            to="/"
            className="flex h-14 items-center gap-2 border-r border-border bg-muted px-4 text-sm font-semibold text-foreground"
          >
            <BrandMark />
            Converge
          </Link>
          <div className="flex min-w-0 flex-1 items-center px-4">
            <Breadcrumbs />
          </div>
          <div className="px-4">
            <ThemeToggle />
          </div>
        </header>
        <main className="mx-auto w-full max-w-[80rem] px-6 py-6">{children}</main>
      </div>
    </BreadcrumbContext.Provider>
  );
}
```

- [ ] **Step 6: Add the progress bar and the new strings**

Create `apps/frontend/src/components/common/ProgressBar.tsx`:

```tsx
import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";

interface ProgressBarProps {
  /** 0-100. */
  value: number;
  /** Accessible name and visible caption. */
  label: string;
  indeterminate?: boolean;
  className?: string;
}

/** ProgressBar pairs a thin shadcn Progress with its caption. */
export function ProgressBar({ value, label, indeterminate = false, className }: ProgressBarProps) {
  return (
    <div className={cn("flex min-w-[9rem] flex-col gap-1", className)}>
      <span className="text-xs text-muted-foreground">{label}</span>
      <Progress
        aria-label={label}
        value={indeterminate ? undefined : value}
        className={cn("h-1.5", indeterminate && "animate-pulse")}
      />
    </div>
  );
}
```

Add to `apps/frontend/src/lib/strings.ts`:

```ts
  reviews: "Reviews",
  newReview: "New review",
  startNewReviewRow: "Start a new review",
  chooseChanges: "Choose changes",
  noOpenReviews: "No open reviews",
  hideDependencyBots: "Hide dependency bots",
  groupByTicket: "Group by ticket",
  noTicket: "No ticket",
  selectOneOrMoreChanges: "Select one or more changes",
  appliedOldestToNewest: "applied oldest → newest",
  viewed: "Viewed",
  nextFile: "Next file",
  backToFirstFile: "Back to first file",
  copyPath: "Copy path",
  inspect: "Inspect",
  open: "Open",
  show: "Show",
  clear: "Clear",
  details: "details",
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/components/layout`
Expected: PASS (6 tests).

- [ ] **Step 8: Confirm nothing else broke, then commit**

Run: `cd apps/frontend && npm test`
Expected: existing page tests may now render inside a narrower container but must still pass; if a test asserted on an old wrapper class, update that assertion in this commit.

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/layout apps/frontend/src/lib/breadcrumbs apps/frontend/src/components/common/ProgressBar.tsx apps/frontend/src/lib/strings.ts
git commit -m "feat(frontend): add the brand block, breadcrumbs, and shared container"
```

---

# Phase C — Root page

### Task 17: The reviews table

**Files:**
- Create: `apps/frontend/src/components/features/reviews/{ReviewsTable.tsx,ReviewRow.tsx,ReviewProgressCell.tsx,NewReviewRow.tsx,DiscardDialog.tsx}`
- Test: `apps/frontend/src/components/features/reviews/__tests__/ReviewsTable.test.tsx`

**Interfaces:**
- Consumes: `viewedStore` (Task 10), `useStore` (Task 9), `timeLeft` (Task 14), `ProgressBar` (Task 16), `stageLabel` from `@/lib/stageLabel`. (The root page has no file list, so it cannot use `viewedProgress`; it clamps the stored count against `totals.files` instead.)
- Produces:
  - `<ReviewsTable reviews loading error onRetry onOpen onDiscard onNewReview pendingId />`
  - `<DiscardDialog open onOpenChange onConfirm pending />`
  - `<ReviewProgressCell review />`
  - `<NewReviewRow onClick />`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/components/features/reviews/__tests__/ReviewsTable.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReviewsTable } from "@/components/features/reviews/ReviewsTable";
import { toggleViewed } from "@/lib/storage/viewed";
import type { Review, ReviewStatus } from "@/types/models/review";

function review(id: string, status: ReviewStatus, overrides: Partial<Review["attributes"]> = {}): Review {
  return {
    type: "reviews",
    id,
    attributes: {
      status,
      stage: null,
      provider: "gl",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: null,
      headSha: null,
      baseDescription: "",
      changes: [421, 430],
      included: [],
      totals: { files: 4, additions: 120, deletions: 30 },
      error: null,
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
      expiresAt: "2999-01-01T00:00:00Z",
      ...overrides,
    },
  };
}

function renderTable(props: Partial<React.ComponentProps<typeof ReviewsTable>> = {}) {
  const onOpen = vi.fn();
  const onDiscard = vi.fn();
  const onNewReview = vi.fn();
  render(
    <MemoryRouter>
      <ReviewsTable
        reviews={[review("r1", "READY")]}
        loading={false}
        onOpen={onOpen}
        onDiscard={onDiscard}
        onNewReview={onNewReview}
        {...props}
      />
    </MemoryRouter>,
  );
  return { onOpen, onDiscard, onNewReview };
}

afterEach(() => localStorage.clear());

describe("ReviewsTable", () => {
  it("shows the repository, change numbers, and totals", () => {
    renderTable();
    expect(screen.getByText("atlas/server")).toBeInTheDocument();
    const row = screen.getByRole("row", { name: /atlas\/server/ });
    expect(row).toHaveTextContent("#421");
    expect(row).toHaveTextContent("#430");
    expect(row).toHaveTextContent("4 files");
    expect(row).toHaveTextContent("+120");
    expect(row).toHaveTextContent("−30");
  });

  it("reports viewed progress from browser state", () => {
    toggleViewed("r1", "a.ts");
    toggleViewed("r1", "b.ts");
    renderTable();
    expect(screen.getByText("2 of 4 files viewed")).toBeInTheDocument();
  });

  it("shows Building and the stage while creating, with no time left", () => {
    renderTable({ reviews: [review("r2", "CREATING", { stage: "applying", totals: null })] });
    const row = screen.getByRole("row", { name: /atlas\/server/ });
    expect(row).toHaveTextContent("Building");
    expect(within(row).getByRole("button", { name: "Open" })).toBeDisabled();
  });

  it("offers Inspect for a conflicted review", () => {
    renderTable({
      reviews: [
        review("r3", "CONFLICTED", {
          error: { code: "MERGE_CONFLICT", message: "conflict" },
        }),
      ],
    });
    expect(screen.getByRole("button", { name: "Inspect" })).toBeInTheDocument();
    expect(screen.getByText("MERGE_CONFLICT")).toBeInTheDocument();
  });

  it("opens the review when the row is clicked away from the actions", async () => {
    const { onOpen } = renderTable();
    await userEvent.click(screen.getByText("atlas/server"));
    expect(onOpen).toHaveBeenCalledWith("r1");
  });

  it("does not open the review when an action button is clicked", async () => {
    const { onOpen } = renderTable();
    await userEvent.click(screen.getByRole("button", { name: "Resume" }));
    expect(onOpen).toHaveBeenCalledTimes(1);
    onOpen.mockClear();
    await userEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("confirms before discarding", async () => {
    const { onDiscard } = renderTable();
    await userEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onDiscard).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: /discard review/i }));
    expect(onDiscard).toHaveBeenCalledWith("r1");
  });

  it("always renders the start-a-new-review row", async () => {
    const { onNewReview } = renderTable({ reviews: [] });
    expect(screen.getByText("No open reviews")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /start a new review/i }));
    expect(onNewReview).toHaveBeenCalled();
  });

  it("renders skeleton rows while loading", () => {
    renderTable({ reviews: [], loading: true });
    expect(screen.queryByText("No open reviews")).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/components/features/reviews`
Expected: FAIL — `ReviewsTable` does not resolve.

- [ ] **Step 3: Implement `DiscardDialog`**

```tsx
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { strings } from "@/lib/strings";

interface DiscardDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  pending?: boolean;
}

/** DiscardDialog guards the one irreversible action on the root page. */
export function DiscardDialog({ open, onOpenChange, onConfirm, pending = false }: DiscardDialogProps) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{strings.discardReview}?</AlertDialogTitle>
          <AlertDialogDescription>{strings.discardConfirmSuffix}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{strings.cancel}</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm} disabled={pending}>
            {strings.discardReview}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
```

- [ ] **Step 4: Implement `ReviewProgressCell`**

```tsx
import { useMemo } from "react";
import { ProgressBar } from "@/components/common/ProgressBar";
import { useStore } from "@/lib/storage/store";
import { viewedStore } from "@/lib/storage/viewed";
import { stageLabel } from "@/lib/stageLabel";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

/**
 * ReviewProgressCell reads the same browser viewed-state the review page
 * writes, through useSyncExternalStore, so marking a file viewed updates this
 * cell in another tab without a server round trip.
 */
export function ReviewProgressCell({ review }: { review: Review }) {
  const { status, stage, totals, error } = review.attributes;
  const [viewed] = useStore(viewedStore(review.id));
  const total = totals?.files ?? 0;
  const count = useMemo(() => {
    // Only paths still plausible for this review are counted; the review page
    // prunes against the real file list, and the cell has no file list here.
    return Math.min(viewed.length, total);
  }, [viewed, total]);

  if (status === "CREATING") {
    const label = stage ? `${strings.statusBuilding} · ${stageLabel(stage)}` : strings.statusBuilding;
    return <ProgressBar value={0} label={label} indeterminate />;
  }
  if (status === "CONFLICTED" || status === "FAILED") {
    return <span className="font-mono text-xs text-destructive">{error?.code ?? status}</span>;
  }
  const percent = total === 0 ? 0 : Math.round((count / total) * 100);
  return <ProgressBar value={percent} label={`${count} of ${total} files viewed`} />;
}
```

If `stageLabel` is not exported under that name, use whatever `@/lib/stageLabel`
exports — check the file before writing this component.

- [ ] **Step 5: Implement `ReviewRow`**

```tsx
import { useState, type MouseEvent } from "react";
import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import { DiscardDialog } from "@/components/features/reviews/DiscardDialog";
import { ReviewProgressCell } from "@/components/features/reviews/ReviewProgressCell";
import { timeLeft } from "@/lib/timeLeft";
import { strings } from "@/lib/strings";
import { cn } from "@/lib/utils";
import type { Review } from "@/types/models/review";

const DOT: Record<string, string> = {
  READY: "bg-green-500",
  CREATING: "bg-amber-500",
  CONFLICTED: "bg-destructive",
  FAILED: "bg-destructive",
};

interface ReviewRowProps {
  review: Review;
  onOpen: (id: string) => void;
  onDiscard: (id: string) => void;
  pending: boolean;
}

export function ReviewRow({ review, onOpen, onDiscard, pending }: ReviewRowProps) {
  const [confirming, setConfirming] = useState(false);
  const { status, repository, provider, baseBranch, changes, totals, expiresAt } = review.attributes;
  const building = status === "CREATING";

  // The row is clickable, but the actions cell is not part of that target:
  // closest("[data-actions]") is why a Discard click never also navigates.
  function onRowClick(event: MouseEvent<HTMLTableRowElement>): void {
    if (building) return;
    if ((event.target as HTMLElement).closest("[data-actions]")) return;
    onOpen(review.id);
  }

  return (
    <>
      <TableRow
        onClick={onRowClick}
        className={cn(!building && "cursor-pointer")}
        aria-disabled={building || undefined}
      >
        <TableCell className="w-6">
          <span className={cn("inline-block h-2 w-2 rounded-full", DOT[status] ?? "bg-muted")} />
        </TableCell>
        <TableCell>
          <div className="flex items-baseline gap-2">
            <span className="font-medium text-foreground">{repository}</span>
            <span className="text-xs text-muted-foreground">
              {provider} · {baseBranch}
            </span>
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span className="font-mono">{changes.map((n) => `#${n}`).join(" · ")}</span>
            {totals ? (
              <span>
                {totals.files} files · <span className="text-foreground">+{totals.additions}</span>{" "}
                <span className="text-destructive">−{totals.deletions}</span>
              </span>
            ) : null}
          </div>
        </TableCell>
        <TableCell className="w-56">
          <ReviewProgressCell review={review} />
        </TableCell>
        <TableCell className="w-28 text-xs text-muted-foreground">
          {building ? "—" : timeLeft(expiresAt)}
        </TableCell>
        <TableCell className="w-48 text-right" data-actions="">
          {building ? (
            <Button size="sm" variant="outline" disabled>
              {strings.open}
            </Button>
          ) : (
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="ghost" disabled={pending} onClick={() => setConfirming(true)}>
                {strings.discard}
              </Button>
              <Button size="sm" onClick={() => onOpen(review.id)}>
                {status === "READY" ? strings.resume : strings.inspect}
              </Button>
            </div>
          )}
        </TableCell>
      </TableRow>
      <DiscardDialog
        open={confirming}
        onOpenChange={setConfirming}
        pending={pending}
        onConfirm={() => {
          setConfirming(false);
          onDiscard(review.id);
        }}
      />
    </>
  );
}
```

- [ ] **Step 6: Implement `NewReviewRow` and `ReviewsTable`**

`NewReviewRow.tsx`:

```tsx
import { Plus } from "lucide-react";
import { Hotkey } from "@/components/common/Hotkey";
import { TableCell, TableRow } from "@/components/ui/table";
import { strings } from "@/lib/strings";

/** NewReviewRow is the dashed last row that opens the new-review drawer. */
export function NewReviewRow({ onClick }: { onClick: () => void }) {
  return (
    <TableRow className="border-dashed hover:bg-transparent">
      <TableCell colSpan={5} className="p-0">
        <button
          type="button"
          onClick={onClick}
          className="flex w-full cursor-pointer items-center gap-2 px-4 py-3 text-sm text-muted-foreground hover:text-foreground"
        >
          <Plus className="h-4 w-4" />
          {strings.startNewReviewRow}
          <Hotkey className="ml-auto">n</Hotkey>
        </button>
      </TableCell>
    </TableRow>
  );
}
```

`ReviewsTable.tsx`:

```tsx
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { NewReviewRow } from "@/components/features/reviews/NewReviewRow";
import { ReviewRow } from "@/components/features/reviews/ReviewRow";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";

interface ReviewsTableProps {
  reviews: Review[];
  loading: boolean;
  error?: unknown;
  onRetry?: () => void;
  onOpen: (id: string) => void;
  onDiscard: (id: string) => void;
  onNewReview: () => void;
  pendingId?: string;
}

/**
 * ReviewsTable is the root page's single card. FINISHED and EXPIRED reviews
 * are filtered out by the page before they reach here (FR-9).
 */
export function ReviewsTable({
  reviews,
  loading,
  error,
  onRetry,
  onOpen,
  onDiscard,
  onNewReview,
  pendingId,
}: ReviewsTableProps) {
  if (error) {
    return (
      <ErrorBanner
        title={strings.reviewsUnavailableTitle}
        detail={messageFor(error, "Try again in a moment.")}
        {...(onRetry ? { onRetry } : {})}
      />
    );
  }
  return (
    <div className="rounded-lg border border-border bg-card">
      <div className="border-b border-border px-4 py-3 text-sm font-medium text-foreground">
        {strings.reviews}
      </div>
      <Table>
        <TableBody>
          {loading
            ? [0, 1, 2].map((row) => (
                <TableRow key={row}>
                  <TableCell colSpan={5}>
                    <Skeleton className="h-10 w-full" />
                  </TableCell>
                </TableRow>
              ))
            : null}
          {!loading && reviews.length === 0 ? (
            <TableRow>
              <TableCell colSpan={5} className="py-6 text-center text-sm text-muted-foreground">
                {strings.noOpenReviews}
              </TableCell>
            </TableRow>
          ) : null}
          {!loading
            ? reviews.map((review) => (
                <ReviewRow
                  key={review.id}
                  review={review}
                  onOpen={onOpen}
                  onDiscard={onDiscard}
                  pending={pendingId === review.id}
                />
              ))
            : null}
          <NewReviewRow onClick={onNewReview} />
        </TableBody>
      </Table>
    </div>
  );
}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/components/features/reviews`
Expected: PASS (10 tests).

- [ ] **Step 8: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/features/reviews
git commit -m "feat(frontend): add the reviews table with progress and actions"
```

---

### Task 18: The new-review drawer, and the root page wiring

**Files:**
- Create: `apps/frontend/src/components/features/newReview/{NewReviewSheet.tsx,ProviderSelect.tsx,RepositorySearch.tsx,RepositoryResults.tsx}`
- Create: `apps/frontend/src/pages/ReviewsPage.tsx`
- Modify: `apps/frontend/src/routes.tsx`
- Delete: `src/pages/SelectRepositoryPage.tsx`, `src/pages/__tests__/SelectRepositoryPage.test.tsx`, `src/components/features/repositories/RepositoryList.tsx`, `src/components/features/repositories/ManualRepositoryForm.tsx`, `src/components/features/repositories/__tests__/ManualRepositoryForm.test.tsx`, `src/components/features/reviews/ResumeReviewList.tsx`, `src/components/features/reviews/ResumeReviewRow.tsx`, `src/components/features/reviews/__tests__/ResumeReviewList.test.tsx`, `src/components/features/providers/ProviderPicker.tsx`, `src/lib/schemas/repository.ts`, `src/lib/schemas/__tests__/repository.test.ts`
- Modify: `apps/frontend/src/__tests__/routes.test.tsx`
- Test: `apps/frontend/src/pages/__tests__/ReviewsPage.test.tsx`

**Interfaces:**
- Consumes: `ReviewsTable` (Task 17), `recentsFor`/`mostRecentProvider` (Task 10), `pruneViewed`/`clearViewed` (Task 10), `useHotkeys` (Task 13), `parseRepositoryInput`/`useDebouncedValue` (Task 14), `useRepositories`/`repositoryKeys` (Task 15), `useBreadcrumbs` (Task 16).
- Produces: `<NewReviewSheet open onOpenChange />`, `<ReviewsPage />`.

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/pages/__tests__/ReviewsPage.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { ReviewsPage } from "@/pages/ReviewsPage";
import { HttpResponse, http, listDoc, server } from "@/test/server";
import { recordRecent } from "@/lib/storage/recents";

function repo(fullName: string) {
  const [namespace = "", name = ""] = fullName.split("/");
  return {
    type: "repositories" as const,
    id: fullName,
    attributes: {
      name,
      namespace,
      defaultBranch: "main",
      webUrl: `https://gitlab.test/${fullName}`,
    },
  };
}

function seed() {
  server.use(
    http.get("/api/reviews", () => HttpResponse.json(listDoc([]))),
    http.get("/api/providers", () =>
      HttpResponse.json(
        listDoc([
          {
            type: "providers" as const,
            id: "gl",
            attributes: { kind: "gitlab", displayName: "GitLab", baseUrl: "https://gitlab.test" },
          },
        ]),
      ),
    ),
    http.get("/api/providers/gl/repositories", ({ request }) => {
      const search = new URL(request.url).searchParams.get("search") ?? "";
      const all = [repo("atlas/server"), repo("atlas/client")];
      const items = search === "" ? all : all.filter((r) => r.id.includes(search));
      return HttpResponse.json(listDoc(items, { number: 1, size: 30, hasNext: false }));
    }),
  );
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={["/"]}>
          <AppShell>
            <Routes>
              <Route path="/" element={<ReviewsPage />} />
              <Route path="/select" element={<p>select page</p>} />
            </Routes>
          </AppShell>
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("ReviewsPage", () => {
  it("publishes the Reviews breadcrumb", async () => {
    seed();
    renderPage();
    await waitFor(() =>
      expect(screen.getByRole("navigation", { name: /breadcrumb/i })).toHaveTextContent("Reviews"),
    );
  });

  it("opens the drawer from the start-a-new-review row", async () => {
    seed();
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /start a new review/i }));
    expect(await screen.findByRole("dialog", { name: /new review/i })).toBeInTheDocument();
  });

  it("opens the drawer with the n key and closes it with Escape", async () => {
    seed();
    renderPage();
    await screen.findByRole("button", { name: /start a new review/i });
    await userEvent.keyboard("n");
    expect(await screen.findByRole("dialog", { name: /new review/i })).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: /new review/i })).not.toBeInTheDocument(),
    );
  });

  it("lists recent repositories first when the search is empty", async () => {
    recordRecent({
      provider: "gl",
      repository: "atlas/client",
      defaultBranch: "main",
      openedAt: "2026-01-01T00:00:00Z",
    });
    seed();
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /start a new review/i }));
    const options = await screen.findAllByRole("option");
    expect(options[0]).toHaveTextContent("atlas/client");
  });

  it("searches the provider and enables the primary action on selection", async () => {
    seed();
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /start a new review/i }));
    const input = await screen.findByPlaceholderText(/search repositories/i);
    await userEvent.type(input, "server");
    const option = await screen.findByRole("option", { name: /atlas\/server/ });
    await userEvent.click(option);
    const go = screen.getByRole("button", { name: /choose changes/i });
    expect(go).toBeEnabled();
    await userEvent.click(go);
    expect(await screen.findByText("select page")).toBeInTheDocument();
  });

  it("shows an inline error when a pasted name does not resolve", async () => {
    seed();
    server.use(
      http.get("/api/providers/gl/repositories/:repo", () =>
        HttpResponse.json(
          { errors: [{ status: "404", code: "NOT_FOUND", title: "Not Found", detail: "gone" }] },
          { status: 404 },
        ),
      ),
    );
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /start a new review/i }));
    const input = await screen.findByPlaceholderText(/search repositories/i);
    await userEvent.type(input, "atlas/missing{Enter}");
    expect(await screen.findByText(/could not be found/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/pages/__tests__/ReviewsPage.test.tsx`
Expected: FAIL — `@/pages/ReviewsPage` does not resolve.

- [ ] **Step 3: Implement `ProviderSelect`**

```tsx
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { strings } from "@/lib/strings";
import type { Provider } from "@/types/models/provider";

interface ProviderSelectProps {
  providers: Provider[];
  value: string | undefined;
  onChange: (providerId: string) => void;
  loading?: boolean;
}

/** ProviderSelect is the drawer's provider picker (was features/providers/ProviderPicker). */
export function ProviderSelect({ providers, value, onChange, loading = false }: ProviderSelectProps) {
  if (loading) return <Skeleton className="h-9 w-full" />;
  return (
    <Select {...(value !== undefined ? { value } : {})} onValueChange={onChange}>
      <SelectTrigger id="provider-select" className="w-full">
        <SelectValue placeholder={`Select a ${strings.provider.toLowerCase()}`} />
      </SelectTrigger>
      <SelectContent>
        {providers.map((provider) => (
          <SelectItem key={provider.id} value={provider.id}>
            <span className="flex items-center gap-2">
              <Badge variant="outline">{provider.attributes.kind}</Badge>
              {provider.attributes.displayName}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
```

- [ ] **Step 4: Implement `RepositorySearch` and `RepositoryResults`**

`RepositorySearch.tsx`:

```tsx
import { CommandInput } from "@/components/ui/command";

interface RepositorySearchProps {
  value: string;
  onChange: (value: string) => void;
  onEnter: () => void;
}

/**
 * RepositorySearch is the Command's own input, so arrow keys and Enter reach
 * the results list through cmdk's roving focus rather than a hand-rolled
 * listbox. onEnter only fires when cmdk did not already consume the key.
 */
export function RepositorySearch({ value, onChange, onEnter }: RepositorySearchProps) {
  return (
    <CommandInput
      placeholder="Search repositories, or paste owner/name"
      value={value}
      onValueChange={onChange}
      onKeyDown={(event) => {
        if (event.key === "Enter" && !event.defaultPrevented) onEnter();
      }}
    />
  );
}
```

`RepositoryResults.tsx`:

```tsx
import { Clock } from "lucide-react";
import { CommandEmpty, CommandGroup, CommandItem, CommandList } from "@/components/ui/command";
import { Skeleton } from "@/components/ui/skeleton";
import { relativeTime } from "@/lib/relativeTime";
import type { RecentRepository } from "@/lib/storage/recents";
import type { Repository } from "@/types/models/repository";

export interface RepositoryChoice {
  repository: string;
  defaultBranch: string;
  recent: boolean;
  openedAt?: string;
}

/**
 * mergeChoices puts matching recents first, then server results with the
 * recents removed, so the common case needs no typing and never shows a
 * repository twice (FR-13).
 */
export function mergeChoices(
  recents: RecentRepository[],
  results: Repository[],
  query: string,
): RepositoryChoice[] {
  const needle = query.trim().toLowerCase();
  const matching = recents.filter(
    (r) => needle === "" || r.repository.toLowerCase().includes(needle),
  );
  const seen = new Set(matching.map((r) => r.repository));
  const fromServer = results
    .filter((r) => !seen.has(r.id))
    .map((r) => ({
      repository: r.id,
      defaultBranch: r.attributes.defaultBranch,
      recent: false,
    }));
  return [
    ...matching.map((r) => ({
      repository: r.repository,
      defaultBranch: r.defaultBranch,
      recent: true,
      openedAt: r.openedAt,
    })),
    ...fromServer,
  ];
}

interface RepositoryResultsProps {
  choices: RepositoryChoice[];
  selected: string | undefined;
  loading: boolean;
  onSelect: (choice: RepositoryChoice) => void;
}

export function RepositoryResults({
  choices,
  selected,
  loading,
  onSelect,
}: RepositoryResultsProps) {
  return (
    <CommandList className="max-h-[22rem]">
      {loading ? (
        <div className="space-y-2 p-2">
          {[0, 1, 2].map((row) => (
            <Skeleton key={row} className="h-9 w-full" />
          ))}
        </div>
      ) : (
        <>
          <CommandEmpty>No repositories match that search.</CommandEmpty>
          <CommandGroup>
            {choices.map((choice) => (
              <CommandItem
                key={choice.repository}
                value={choice.repository}
                onSelect={() => onSelect(choice)}
                data-selected-repository={choice.repository === selected ? "" : undefined}
                className={choice.repository === selected ? "bg-accent" : undefined}
              >
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate font-medium">{choice.repository}</span>
                  <span className="truncate text-xs text-muted-foreground">
                    {choice.defaultBranch}
                  </span>
                </span>
                {choice.recent && choice.openedAt ? (
                  <span className="ml-2 flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
                    <Clock className="h-3 w-3" />
                    opened {relativeTime(choice.openedAt)}
                  </span>
                ) : null}
              </CommandItem>
            ))}
          </CommandGroup>
        </>
      )}
    </CommandList>
  );
}
```

- [ ] **Step 5: Implement `NewReviewSheet`**

```tsx
import { useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Command } from "@/components/ui/command";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { ProviderSelect } from "@/components/features/newReview/ProviderSelect";
import { RepositoryResults, mergeChoices } from "@/components/features/newReview/RepositoryResults";
import { RepositorySearch } from "@/components/features/newReview/RepositorySearch";
import { useProviders } from "@/lib/hooks/api/useProviders";
import { repositoryKeys, useRepositories } from "@/lib/hooks/api/useRepositories";
import { useDebouncedValue } from "@/lib/hooks/useDebouncedValue";
import { mostRecentProvider, recentsFor } from "@/lib/storage/recents";
import { parseRepositoryInput } from "@/lib/repositoryInput";
import { repositoriesService } from "@/services/api";
import { ApiError, messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";

const MIN_SEARCH = 2;

interface NewReviewSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * NewReviewSheet is the one entry point for starting a review. It writes no
 * recent entry itself: the create page records one on load (FR-16) so a deep
 * link counts the same as a trip through this drawer.
 */
export function NewReviewSheet({ open, onOpenChange }: NewReviewSheetProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const providers = useProviders();
  const [chosenProvider, setChosenProvider] = useState<string | undefined>(undefined);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const [inlineError, setInlineError] = useState<string | null>(null);
  const [resolving, setResolving] = useState(false);

  const list = providers.data ?? [];
  const preferred = mostRecentProvider();
  const providerId =
    chosenProvider ??
    (preferred !== undefined && list.some((p) => p.id === preferred) ? preferred : list[0]?.id);
  const provider = list.find((p) => p.id === providerId);

  const debounced = useDebouncedValue(query, 250);
  const activeSearch = debounced.trim().length >= MIN_SEARCH ? debounced.trim() : undefined;
  const repositories = useRepositories(
    providerId,
    activeSearch !== undefined ? { search: activeSearch } : {},
  );

  const choices = useMemo(
    () =>
      mergeChoices(
        providerId === undefined ? [] : recentsFor(providerId),
        repositories.data?.items ?? [],
        query,
      ),
    [providerId, repositories.data, query],
  );

  function reset(): void {
    setChosenProvider(undefined);
    setQuery("");
    setSelected(undefined);
    setInlineError(null);
  }

  function close(next: boolean): void {
    if (!next) reset();
    onOpenChange(next);
  }

  function go(repository: string): void {
    const search = new URLSearchParams({ provider: providerId as string, repo: repository });
    close(false);
    navigate(`/select?${search.toString()}`);
  }

  // Enter with nothing highlighted means "resolve what I pasted". cmdk marks
  // the event handled when it selects a row, so this only runs otherwise.
  async function resolveTyped(): Promise<void> {
    if (providerId === undefined) return;
    const fullName = parseRepositoryInput(query, provider?.attributes.baseUrl);
    if (fullName === null) return;
    setInlineError(null);
    setResolving(true);
    try {
      await queryClient.fetchQuery({
        queryKey: repositoryKeys.detail(providerId, fullName),
        queryFn: () => repositoriesService.get(providerId, fullName),
      });
      go(fullName);
    } catch (error: unknown) {
      if (error instanceof ApiError && error.status === 404) {
        setInlineError(`${fullName} could not be found on this provider.`);
      } else {
        toast.error(messageFor(error, "That repository could not be checked."));
      }
    } finally {
      setResolving(false);
    }
  }

  return (
    <Sheet open={open} onOpenChange={close}>
      <SheetContent side="right" className="flex w-full flex-col sm:max-w-[32.5rem]">
        <SheetHeader>
          <SheetTitle>{strings.newReview}</SheetTitle>
          <SheetDescription>
            Pick a {strings.provider.toLowerCase()} and a {strings.repository.toLowerCase()}.
          </SheetDescription>
        </SheetHeader>
        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4">
          <div className="flex flex-col gap-1">
            <label htmlFor="provider-select" className="text-sm font-medium text-foreground">
              {strings.provider}
            </label>
            <ProviderSelect
              providers={list}
              value={providerId}
              loading={providers.isLoading}
              onChange={(id) => {
                setChosenProvider(id);
                setSelected(undefined);
                setInlineError(null);
              }}
            />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium text-foreground">{strings.repository}</span>
            {/* shouldFilter=false: the server already filtered, and cmdk's own
                fuzzy filter would hide server results that do not fuzzy-match. */}
            <Command shouldFilter={false} className="rounded-md border border-border">
              <RepositorySearch
                value={query}
                onChange={(value) => {
                  setQuery(value);
                  setInlineError(null);
                }}
                onEnter={() => void resolveTyped()}
              />
              <RepositoryResults
                choices={choices}
                selected={selected}
                loading={repositories.isLoading || resolving}
                onSelect={(choice) => setSelected(choice.repository)}
              />
            </Command>
            {inlineError ? <p className="text-sm text-destructive">{inlineError}</p> : null}
          </div>
        </div>
        <SheetFooter className="flex-row justify-end gap-2">
          <Button variant="ghost" onClick={() => close(false)}>
            {strings.cancel}
          </Button>
          <Button disabled={selected === undefined} onClick={() => selected && go(selected)}>
            {strings.chooseChanges} →
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
```

- [ ] **Step 6: Implement `ReviewsPage`**

```tsx
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { ReviewsTable } from "@/components/features/reviews/ReviewsTable";
import { NewReviewSheet } from "@/components/features/newReview/NewReviewSheet";
import { useFinishReview, useReviews } from "@/lib/hooks/api/useReviews";
import { useBreadcrumbs } from "@/lib/breadcrumbs/useBreadcrumbs";
import { useHotkeys } from "@/lib/hotkeys/useHotkeys";
import { clearViewed, pruneViewed } from "@/lib/storage/viewed";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";

export function ReviewsPage() {
  const navigate = useNavigate();
  const [sheetOpen, setSheetOpen] = useState(false);
  const reviews = useReviews();
  // Discard here and Finish Review on the review page are the same backend
  // operation (DELETE /api/reviews/{id}); the mutation's invalidation removes
  // the row, so this handler does not navigate.
  const discardReview = useFinishReview();

  useBreadcrumbs(useMemo(() => [{ label: strings.reviews }], []));

  const all = reviews.data;
  const active = useMemo(
    () =>
      (all ?? []).filter(
        (review) =>
          review.attributes.status !== "FINISHED" && review.attributes.status !== "EXPIRED",
      ),
    [all],
  );

  // Viewed state for a review the server no longer lists (swept, expired, or
  // finished in another tab) would otherwise linger in localStorage forever.
  useEffect(() => {
    if (all === undefined) return;
    pruneViewed(all.map((review) => review.id));
  }, [all]);

  useHotkeys({ n: () => setSheetOpen(true) }, { enabled: !sheetOpen });

  async function discard(id: string): Promise<void> {
    try {
      await discardReview.mutateAsync(id);
      clearViewed(id);
    } catch (error: unknown) {
      toast.error(messageFor(error, strings.reviewDiscardFailed));
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={strings.combinedReview}
        description="Resume a review in progress, or start a new one."
      />
      <ReviewsTable
        reviews={active}
        // isLoading, not isFetching: a background poll must not replace
        // rendered rows with skeletons.
        loading={reviews.isLoading}
        {...(reviews.isError ? { error: reviews.error } : {})}
        onRetry={() => void reviews.refetch()}
        onOpen={(id) => navigate(`/reviews/${id}`)}
        onDiscard={(id) => void discard(id)}
        onNewReview={() => setSheetOpen(true)}
        {...(discardReview.isPending && discardReview.variables
          ? { pendingId: discardReview.variables }
          : {})}
      />
      <NewReviewSheet open={sheetOpen} onOpenChange={setSheetOpen} />
    </div>
  );
}
```

- [ ] **Step 7: Repoint the route and delete the replaced components**

In `src/routes.tsx`, replace the line
`import { SelectRepositoryPage } from "@/pages/SelectRepositoryPage";` with:

```tsx
import { ReviewsPage } from "@/pages/ReviewsPage";
```

and replace `<Route path="/" element={<SelectRepositoryPage />} />` with:

```tsx
      <Route path="/" element={<ReviewsPage />} />
```

and change the catch-all wrapper to drop its own max width (the shell owns it):

```tsx
        element={
          <EmptyState title="Page not found" description="That address does not exist." />
        }
```

Then delete the replaced files:

```bash
cd apps/frontend
git rm src/pages/SelectRepositoryPage.tsx src/pages/__tests__/SelectRepositoryPage.test.tsx \
  src/components/features/repositories/RepositoryList.tsx \
  src/components/features/repositories/ManualRepositoryForm.tsx \
  src/components/features/repositories/__tests__/ManualRepositoryForm.test.tsx \
  src/components/features/reviews/ResumeReviewList.tsx \
  src/components/features/reviews/ResumeReviewRow.tsx \
  src/components/features/reviews/__tests__/ResumeReviewList.test.tsx \
  src/components/features/providers/ProviderPicker.tsx \
  src/lib/schemas/repository.ts \
  src/lib/schemas/__tests__/repository.test.ts
```

Update `src/__tests__/routes.test.tsx` so any assertion naming
`SelectRepositoryPage` refers to `ReviewsPage`, and any text assertion matching
the old root page ("Choose a provider…") matches the new subtitle.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/pages src/components/features/newReview src/__tests__/routes.test.tsx`
Expected: PASS.

- [ ] **Step 9: Lint, format, and commit**

```bash
cd apps/frontend && npm run lint && npm run format:check && npm test
cd "$(git rev-parse --show-toplevel)"
git add -A apps/frontend/src
git commit -m "feat(frontend): replace the root page with the reviews table and new-review drawer"
```

---

# Phase D — Create page

### Task 19: The base-branch select

**Files:**
- Create: `apps/frontend/src/components/features/changes/BaseBranchSelect.tsx`
- Test: `apps/frontend/src/components/features/changes/__tests__/BaseBranchSelect.test.tsx`

**Interfaces:**
- Consumes: `useBranches` (Task 15), `useDebouncedValue` (Task 14).
- Produces: `<BaseBranchSelect providerId repository value defaultBranch onChange onOpenChange />`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/components/features/changes/__tests__/BaseBranchSelect.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { BaseBranchSelect } from "@/components/features/changes/BaseBranchSelect";
import { HttpResponse, http, listDoc, server } from "@/test/server";

function branch(name: string, isDefault = false) {
  return {
    type: "branches" as const,
    id: `gl:atlas/server:${name}`,
    attributes: { name, isDefault, sha: "" },
  };
}

function renderSelect(onChange = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  render(
    <QueryClientProvider client={client}>
      <BaseBranchSelect
        providerId="gl"
        repository="atlas/server"
        value="main"
        defaultBranch="main"
        onChange={onChange}
      />
    </QueryClientProvider>,
  );
  return onChange;
}

describe("BaseBranchSelect", () => {
  it("shows the current base on the trigger", () => {
    server.use(http.get("/api/providers/gl/repositories/:repo/branches", () =>
      HttpResponse.json(listDoc([branch("main", true)])),
    ));
    renderSelect();
    expect(screen.getByRole("combobox", { name: /base/i })).toHaveTextContent("main");
  });

  it("pins the repository default at the top even when the server did not", async () => {
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(listDoc([branch("develop"), branch("main", true)])),
      ),
    );
    renderSelect();
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    const options = await screen.findAllByRole("option");
    expect(options[0]).toHaveTextContent("main");
  });

  it("sends a debounced search to the branches endpoint", async () => {
    const seen: string[] = [];
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", ({ request }) => {
        seen.push(new URL(request.url).searchParams.get("search") ?? "");
        return HttpResponse.json(listDoc([branch("release/1.0")]));
      }),
    );
    renderSelect();
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    await userEvent.type(screen.getByPlaceholderText(/filter branches/i), "rel");
    await waitFor(() => expect(seen).toContain("rel"));
  });

  it("selects a branch and reports it", async () => {
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(listDoc([branch("main", true), branch("develop")])),
      ),
    );
    const onChange = renderSelect();
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    await userEvent.click(await screen.findByRole("option", { name: /develop/ }));
    expect(onChange).toHaveBeenCalledWith("develop");
  });

  it("lets a typed branch beyond the loaded page be used as-is", async () => {
    server.use(
      http.get("/api/providers/gl/repositories/:repo/branches", () =>
        HttpResponse.json(listDoc([])),
      ),
    );
    const onChange = renderSelect();
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    await userEvent.type(screen.getByPlaceholderText(/filter branches/i), "feature/x");
    await userEvent.click(await screen.findByRole("option", { name: /use “feature\/x”/i }));
    expect(onChange).toHaveBeenCalledWith("feature/x");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/components/features/changes/__tests__/BaseBranchSelect.test.tsx`
Expected: FAIL — module does not resolve.

- [ ] **Step 3: Implement `BaseBranchSelect`**

```tsx
import { useMemo, useState } from "react";
import { Check, ChevronsUpDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useBranches } from "@/lib/hooks/api/useBranches";
import { useDebouncedValue } from "@/lib/hooks/useDebouncedValue";
import { strings } from "@/lib/strings";
import { cn } from "@/lib/utils";

interface BaseBranchSelectProps {
  providerId: string;
  repository: string;
  value: string;
  defaultBranch: string | undefined;
  onChange: (branch: string) => void;
  onOpenChange?: (open: boolean) => void;
}

/**
 * BaseBranchSelect is a searchable combobox rather than a plain select:
 * repositories routinely have hundreds of branches, and the endpoint pages.
 *
 * The repository's own default is pinned at the top regardless of what the
 * current page contains, and a typed value that matches nothing is still
 * offered -- a branch past the first page has to be reachable, and the backend
 * validates the name on build anyway.
 */
export function BaseBranchSelect({
  providerId,
  repository,
  value,
  defaultBranch,
  onChange,
  onOpenChange,
}: BaseBranchSelectProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const debounced = useDebouncedValue(query, 250);
  const branches = useBranches(
    providerId,
    repository,
    debounced.trim() === "" ? {} : { search: debounced.trim() },
    open,
  );

  const names = useMemo(() => {
    const fromServer = (branches.data?.items ?? []).map((b) => b.attributes.name);
    const pinned =
      defaultBranch !== undefined &&
      defaultBranch !== "" &&
      (debounced.trim() === "" || defaultBranch.includes(debounced.trim()))
        ? [defaultBranch]
        : [];
    return [...pinned, ...fromServer.filter((n) => n !== defaultBranch)];
  }, [branches.data, defaultBranch, debounced]);

  const typed = query.trim();
  const offerTyped = typed !== "" && !names.includes(typed);

  function setOpenState(next: boolean): void {
    setOpen(next);
    onOpenChange?.(next);
    if (!next) setQuery("");
  }

  function choose(branch: string): void {
    onChange(branch);
    setOpenState(false);
  }

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor="base-branch" className="text-sm font-medium text-foreground">
        {strings.base}
      </label>
      <Popover open={open} onOpenChange={setOpenState}>
        <PopoverTrigger asChild>
          <Button
            id="base-branch"
            variant="outline"
            role="combobox"
            aria-expanded={open}
            aria-label={strings.base}
            className="w-64 justify-between font-mono text-sm"
          >
            <span className="truncate">{value || "—"}</span>
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-72 p-0" align="start">
          {/* shouldFilter=false: the server filters, and cmdk's fuzzy filter
              would drop server results that do not fuzzy-match the query. */}
          <Command shouldFilter={false}>
            <CommandInput
              placeholder="Filter branches"
              value={query}
              onValueChange={setQuery}
            />
            <CommandList>
              <CommandEmpty>No branches match that filter.</CommandEmpty>
              <CommandGroup>
                {names.map((name) => (
                  <CommandItem key={name} value={name} onSelect={() => choose(name)}>
                    <Check
                      className={cn("mr-2 h-4 w-4", name === value ? "opacity-100" : "opacity-0")}
                    />
                    <span className="truncate font-mono text-sm">{name}</span>
                    {name === defaultBranch ? (
                      <span className="ml-auto text-xs text-muted-foreground">default</span>
                    ) : null}
                  </CommandItem>
                ))}
                {offerTyped ? (
                  <CommandItem value={`use-${typed}`} onSelect={() => choose(typed)}>
                    <span className="truncate text-sm">
                      Use “<span className="font-mono">{typed}</span>”
                    </span>
                  </CommandItem>
                ) : null}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/components/features/changes/__tests__/BaseBranchSelect.test.tsx`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/features/changes/BaseBranchSelect.tsx apps/frontend/src/components/features/changes/__tests__/BaseBranchSelect.test.tsx
git commit -m "feat(frontend): add the searchable base-branch select"
```

---

### Task 20: The change filter row

**Files:**
- Create: `apps/frontend/src/components/features/changes/ChangeFilters.tsx`
- Test: `apps/frontend/src/components/features/changes/__tests__/ChangeFilters.test.tsx`

**Interfaces:**
- Consumes: `ChangeSearch` (existing), `strings`.
- Produces: `<ChangeFilters search onSearchChange authors activeAuthors onToggleAuthor hideBots onHideBotsChange groupByTicket onGroupByTicketChange shown total />`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/components/features/changes/__tests__/ChangeFilters.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ChangeFilters } from "@/components/features/changes/ChangeFilters";

function renderFilters(overrides: Partial<React.ComponentProps<typeof ChangeFilters>> = {}) {
  const props = {
    search: "",
    onSearchChange: vi.fn(),
    authors: ["adam", "zoe"],
    activeAuthors: new Set<string>(),
    onToggleAuthor: vi.fn(),
    hideBots: true,
    onHideBotsChange: vi.fn(),
    groupByTicket: false,
    onGroupByTicketChange: vi.fn(),
    shown: 3,
    total: 10,
    ...overrides,
  };
  render(<ChangeFilters {...props} />);
  return props;
}

describe("ChangeFilters", () => {
  it("renders one chip per author", () => {
    renderFilters();
    expect(screen.getByRole("button", { name: "adam" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "zoe" })).toBeInTheDocument();
  });

  it("reports an author chip toggle", async () => {
    const props = renderFilters();
    await userEvent.click(screen.getByRole("button", { name: "zoe" }));
    expect(props.onToggleAuthor).toHaveBeenCalledWith("zoe");
  });

  it("marks active author chips as pressed", () => {
    renderFilters({ activeAuthors: new Set(["zoe"]) });
    expect(screen.getByRole("button", { name: "zoe" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "adam" })).toHaveAttribute("aria-pressed", "false");
  });

  it("toggles hide-dependency-bots", async () => {
    const props = renderFilters();
    await userEvent.click(screen.getByRole("switch", { name: /hide dependency bots/i }));
    expect(props.onHideBotsChange).toHaveBeenCalledWith(false);
  });

  it("toggles group-by-ticket", async () => {
    const props = renderFilters();
    await userEvent.click(screen.getByRole("button", { name: /group by ticket/i }));
    expect(props.onGroupByTicketChange).toHaveBeenCalledWith(true);
  });

  it("shows the filtered count", () => {
    renderFilters();
    expect(screen.getByText("3 of 10 shown")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/components/features/changes/__tests__/ChangeFilters.test.tsx`
Expected: FAIL — module does not resolve.

- [ ] **Step 3: Implement `ChangeFilters`**

```tsx
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { ChangeSearch } from "@/components/features/changes/ChangeSearch";
import { strings } from "@/lib/strings";
import { cn } from "@/lib/utils";

interface ChangeFiltersProps {
  search: string;
  onSearchChange: (value: string) => void;
  /** Derived from the loaded pages only; loading another page may add chips. */
  authors: string[];
  activeAuthors: ReadonlySet<string>;
  onToggleAuthor: (author: string) => void;
  hideBots: boolean;
  onHideBotsChange: (next: boolean) => void;
  groupByTicket: boolean;
  onGroupByTicketChange: (next: boolean) => void;
  shown: number;
  total: number;
}

export function ChangeFilters({
  search,
  onSearchChange,
  authors,
  activeAuthors,
  onToggleAuthor,
  hideBots,
  onHideBotsChange,
  groupByTicket,
  onGroupByTicketChange,
  shown,
  total,
}: ChangeFiltersProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <ChangeSearch value={search} onChange={onSearchChange} />
      {authors.map((author) => {
        const active = activeAuthors.has(author);
        return (
          <button
            key={author}
            type="button"
            aria-pressed={active}
            onClick={() => onToggleAuthor(author)}
            className={cn(
              "cursor-pointer rounded-full border px-2.5 py-1 text-xs",
              active
                ? "border-foreground bg-foreground text-background"
                : "border-border text-muted-foreground hover:text-foreground",
            )}
          >
            {author}
          </button>
        );
      })}
      <Separator orientation="vertical" className="h-5" />
      <label className="flex items-center gap-2 rounded-full border border-border px-2.5 py-1 text-xs text-muted-foreground">
        <Switch
          checked={hideBots}
          onCheckedChange={onHideBotsChange}
          aria-label={strings.hideDependencyBots}
        />
        {strings.hideDependencyBots}
      </label>
      <Button
        type="button"
        variant={groupByTicket ? "default" : "outline"}
        size="sm"
        className="rounded-full"
        aria-pressed={groupByTicket}
        onClick={() => onGroupByTicketChange(!groupByTicket)}
      >
        {strings.groupByTicket}
      </Button>
      <span className="ml-auto text-xs text-muted-foreground">
        {shown} of {total} shown
      </span>
    </div>
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/components/features/changes/__tests__/ChangeFilters.test.tsx`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/features/changes/ChangeFilters.tsx apps/frontend/src/components/features/changes/__tests__/ChangeFilters.test.tsx
git commit -m "feat(frontend): add the change filter row"
```

---

### Task 21: The grouped change table

**Files:**
- Rewrite: `apps/frontend/src/components/features/changes/ChangeTable.tsx`
- Create: `apps/frontend/src/components/features/changes/{ChangeRow.tsx,TicketGroupHeader.tsx}`
- Test: `apps/frontend/src/components/features/changes/__tests__/ChangeTable.test.tsx`

**Interfaces:**
- Consumes: `ticketKey`, `groupByTicket`, `TicketGroup` (Task 11).
- Produces:
  - `type ChangeTableRow = { kind: "group"; group: TicketGroup; allSelected: boolean } | { kind: "change"; change: Change; showTicketBadge: boolean } | { kind: "hidden"; count: number }`
  - `buildRows(visible: Change[], groups: TicketGroup[] | null, hiddenBots: number, isSelected: (n: number) => boolean): ChangeTableRow[]`
  - `<ChangeTable rows loading isSelected onToggle onToggleGroup onToggleAll allSelected someSelected onShowBots />`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/components/features/changes/__tests__/ChangeTable.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ChangeTable, buildRows } from "@/components/features/changes/ChangeTable";
import { groupByTicket } from "@/lib/changes/groupByTicket";
import type { Change } from "@/types/models/change";

function change(number: number, title: string, author = "jsmith"): Change {
  return {
    type: "changes",
    id: String(number),
    attributes: {
      number,
      title,
      author,
      sourceBranch: `feat/${number}`,
      targetBranch: "main",
      mergedAt: "2026-01-01T00:00:00Z",
      createdAt: "2026-01-01T00:00:00Z",
      landingSha: null,
      webUrl: `https://example.test/mr/${number}`,
    },
  };
}

const changes = [change(1, "ATLAS-1 first"), change(2, "ATLAS-1 second"), change(3, "untagged")];

function renderTable(props: Partial<React.ComponentProps<typeof ChangeTable>> = {}) {
  const isSelected = props.isSelected ?? (() => false);
  const onToggle = vi.fn();
  const onToggleGroup = vi.fn();
  const onToggleAll = vi.fn();
  const onShowBots = vi.fn();
  render(
    <ChangeTable
      rows={buildRows(changes, null, 0, isSelected)}
      loading={false}
      isSelected={isSelected}
      onToggle={onToggle}
      onToggleGroup={onToggleGroup}
      onToggleAll={onToggleAll}
      allSelected={false}
      someSelected={false}
      onShowBots={onShowBots}
      {...props}
    />,
  );
  return { onToggle, onToggleGroup, onToggleAll, onShowBots };
}

describe("ChangeTable", () => {
  it("renders a flat table with the ticket key as a badge", () => {
    renderTable();
    expect(screen.getByText("#1")).toBeInTheDocument();
    expect(screen.getAllByText("ATLAS-1")).toHaveLength(2);
  });

  it("toggles selection when a row is clicked", async () => {
    const { onToggle } = renderTable();
    await userEvent.click(screen.getByText("ATLAS-1 first"));
    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onToggle.mock.calls[0]?.[0]?.attributes.number).toBe(1);
  });

  it("does not toggle when a link inside the row is clicked", async () => {
    const { onToggle } = renderTable();
    await userEvent.click(screen.getByRole("link", { name: "#1" }));
    expect(onToggle).not.toHaveBeenCalled();
  });

  it("renders group headers with a select-all control when grouping", async () => {
    const { onToggleGroup } = renderTable({
      rows: buildRows(changes, groupByTicket(changes), 0, () => false),
    });
    expect(screen.getByText("ATLAS-1")).toBeInTheDocument();
    expect(screen.getByText("No ticket")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Select all 2" }));
    expect(onToggleGroup).toHaveBeenCalled();
  });

  it("offers Deselect all when the whole group is selected", () => {
    renderTable({
      rows: buildRows(changes, groupByTicket(changes), 0, (n) => n === 1 || n === 2),
      isSelected: (n) => n === 1 || n === 2,
    });
    expect(screen.getByRole("button", { name: "Deselect all" })).toBeInTheDocument();
  });

  it("shows the hidden-bots summary row with a Show button", async () => {
    const { onShowBots } = renderTable({ rows: buildRows(changes, null, 4, () => false) });
    expect(screen.getByText(/4 changes hidden by/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Show" }));
    expect(onShowBots).toHaveBeenCalled();
  });

  it("puts the header checkbox in the indeterminate state when some are selected", () => {
    renderTable({ someSelected: true });
    expect(screen.getByRole("checkbox", { name: /select all visible/i })).toHaveAttribute(
      "data-state",
      "indeterminate",
    );
  });

  it("shows the empty state when nothing matches", () => {
    renderTable({ rows: [] });
    expect(screen.getByText("No merged PRs/MRs")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/components/features/changes/__tests__/ChangeTable.test.tsx`
Expected: FAIL — `buildRows` is not exported and the grouped markup does not exist.

- [ ] **Step 3: Implement `TicketGroupHeader`**

```tsx
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import type { TicketGroup } from "@/lib/changes/groupByTicket";

interface TicketGroupHeaderProps {
  group: TicketGroup;
  allSelected: boolean;
  onToggleGroup: (group: TicketGroup, select: boolean) => void;
}

/** TicketGroupHeader turns "review this one feature" into a single click. */
export function TicketGroupHeader({ group, allSelected, onToggleGroup }: TicketGroupHeaderProps) {
  return (
    <TableRow className="bg-muted/50 hover:bg-muted/50">
      <TableCell colSpan={6}>
        <div className="flex items-center gap-3">
          <Badge variant={group.key === null ? "outline" : "secondary"}>{group.label}</Badge>
          <span className="text-xs text-muted-foreground">{group.changes.length} changes</span>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="ml-auto"
            onClick={() => onToggleGroup(group, !allSelected)}
          >
            {allSelected ? "Deselect all" : `Select all ${group.changes.length}`}
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}
```

- [ ] **Step 4: Implement `ChangeRow`**

```tsx
import type { MouseEvent } from "react";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { TableCell, TableRow } from "@/components/ui/table";
import { ticketKey } from "@/lib/changes/ticketKey";
import type { Change } from "@/types/models/change";

const TWO_DAYS_MS = 172_800_000;

/** mergedLabel shows the time only while it is still useful for ordering. */
function mergedLabel(value: string | null, now: Date = new Date()): string {
  if (value === null) return "—";
  const ms = Date.parse(value);
  if (Number.isNaN(ms)) return "—";
  const date = new Date(ms);
  return now.getTime() - ms < TWO_DAYS_MS
    ? date.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
    : date.toLocaleDateString();
}

function initials(author: string): string {
  return author.slice(0, 2).toUpperCase();
}

interface ChangeRowProps {
  change: Change;
  selected: boolean;
  showTicketBadge: boolean;
  onToggle: (change: Change) => void;
}

export function ChangeRow({ change, selected, showTicketBadge, onToggle }: ChangeRowProps) {
  const { number, title, author, mergedAt, sourceBranch, webUrl } = change.attributes;
  const key = showTicketBadge ? ticketKey(title) : null;

  // Clicking anywhere selects, except on a link: the provider link has to stay
  // usable without also toggling the row underneath it.
  function onRowClick(event: MouseEvent<HTMLTableRowElement>): void {
    if ((event.target as HTMLElement).closest("a")) return;
    onToggle(change);
  }

  return (
    <TableRow onClick={onRowClick} className="cursor-pointer" data-state={selected ? "selected" : undefined}>
      <TableCell className="w-10">
        <Checkbox
          checked={selected}
          onCheckedChange={() => onToggle(change)}
          aria-label={`Select #${number}`}
        />
      </TableCell>
      <TableCell className="w-20 font-mono text-sm">
        <a
          href={webUrl}
          target="_blank"
          rel="noreferrer"
          className="text-muted-foreground hover:text-foreground hover:underline"
        >
          #{number}
        </a>
      </TableCell>
      <TableCell className="font-medium">
        <span className="flex items-center gap-2">
          {key ? <Badge variant="secondary">{key}</Badge> : null}
          <span className="truncate">{title}</span>
        </span>
      </TableCell>
      <TableCell className="w-44 text-muted-foreground">
        <span className="flex items-center gap-2">
          <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-muted text-[0.625rem] font-medium">
            {initials(author)}
          </span>
          <span className="truncate">{author}</span>
        </span>
      </TableCell>
      <TableCell className="w-36 text-xs text-muted-foreground">{mergedLabel(mergedAt)}</TableCell>
      <TableCell className="w-48 truncate font-mono text-xs text-muted-foreground">
        {sourceBranch}
      </TableCell>
    </TableRow>
  );
}
```

- [ ] **Step 5: Rewrite `ChangeTable`**

```tsx
import { Checkbox } from "@/components/ui/checkbox";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/common/EmptyState";
import { ChangeRow } from "@/components/features/changes/ChangeRow";
import { TicketGroupHeader } from "@/components/features/changes/TicketGroupHeader";
import type { TicketGroup } from "@/lib/changes/groupByTicket";
import { strings } from "@/lib/strings";
import type { Change } from "@/types/models/change";

export type ChangeTableRow =
  | { kind: "group"; group: TicketGroup; allSelected: boolean }
  | { kind: "change"; change: Change; showTicketBadge: boolean }
  | { kind: "hidden"; count: number };

/**
 * buildRows flattens grouped and ungrouped modes into one row list, so the
 * table markup is the same in both. Pass groups=null for the flat table, where
 * each change carries its own ticket badge instead.
 */
export function buildRows(
  visible: Change[],
  groups: TicketGroup[] | null,
  hiddenBots: number,
  isSelected: (n: number) => boolean,
): ChangeTableRow[] {
  const rows: ChangeTableRow[] = [];
  if (groups === null) {
    for (const change of visible) rows.push({ kind: "change", change, showTicketBadge: true });
  } else {
    for (const group of groups) {
      rows.push({
        kind: "group",
        group,
        allSelected:
          group.changes.length > 0 &&
          group.changes.every((c) => isSelected(c.attributes.number)),
      });
      for (const change of group.changes) {
        rows.push({ kind: "change", change, showTicketBadge: false });
      }
    }
  }
  if (hiddenBots > 0) rows.push({ kind: "hidden", count: hiddenBots });
  return rows;
}

interface ChangeTableProps {
  rows: ChangeTableRow[];
  loading: boolean;
  isSelected: (n: number) => boolean;
  onToggle: (change: Change) => void;
  onToggleGroup: (group: TicketGroup, select: boolean) => void;
  onToggleAll: (select: boolean) => void;
  allSelected: boolean;
  someSelected: boolean;
  onShowBots: () => void;
}

export function ChangeTable({
  rows,
  loading,
  isSelected,
  onToggle,
  onToggleGroup,
  onToggleAll,
  allSelected,
  someSelected,
  onShowBots,
}: ChangeTableProps) {
  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2, 3, 4].map((row) => (
          <Skeleton key={row} className="h-10 w-full" />
        ))}
      </div>
    );
  }
  if (rows.length === 0) {
    return (
      <EmptyState
        title="No merged PRs/MRs"
        description="Nothing matches this base branch and these filters."
      />
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-10">
            <Checkbox
              aria-label="Select all visible changes"
              checked={allSelected ? true : someSelected ? "indeterminate" : false}
              onCheckedChange={() => onToggleAll(!allSelected)}
            />
          </TableHead>
          <TableHead className="w-20">Number</TableHead>
          <TableHead>Title</TableHead>
          <TableHead className="w-44">Author</TableHead>
          <TableHead className="w-36">Merged</TableHead>
          <TableHead className="w-48">Source branch</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => {
          if (row.kind === "group") {
            return (
              <TicketGroupHeader
                key={`group-${row.group.label}`}
                group={row.group}
                allSelected={row.allSelected}
                onToggleGroup={onToggleGroup}
              />
            );
          }
          if (row.kind === "hidden") {
            return (
              <TableRow key="hidden-bots" className="hover:bg-transparent">
                <TableCell colSpan={6} className="text-xs text-muted-foreground">
                  {row.count} changes hidden by “{strings.hideDependencyBots}”
                  <Button variant="ghost" size="sm" className="ml-2" onClick={onShowBots}>
                    {strings.show}
                  </Button>
                </TableCell>
              </TableRow>
            );
          }
          return (
            <ChangeRow
              key={row.change.id}
              change={row.change}
              selected={isSelected(row.change.attributes.number)}
              showTicketBadge={row.showTicketBadge}
              onToggle={onToggle}
            />
          );
        })}
      </TableBody>
    </Table>
  );
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/components/features/changes`
Expected: PASS (8 tests plus the earlier two suites).

- [ ] **Step 7: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/features/changes
git commit -m "feat(frontend): rewrite the change table with ticket groups and bot hiding"
```

---

### Task 22: The selection bar and the create page wiring

**Files:**
- Rewrite: `apps/frontend/src/components/features/changes/SelectionBar.tsx`
- Rewrite: `apps/frontend/src/pages/SelectChangesPage.tsx`
- Modify: `apps/frontend/src/components/features/changes/__tests__/SelectionBar.test.tsx`
- Test: `apps/frontend/src/pages/__tests__/SelectChangesPage.test.tsx`

**Interfaces:**
- Consumes: `BaseBranchSelect` (19), `ChangeFilters` (20), `ChangeTable`/`buildRows` (21), `applyOrder`/`isDependencyBot`/`groupByTicket`/`distinctAuthors` (11), `changeFiltersStore`+`useStore` (9/10), `recordRecent` (10), `useHotkeys` (13), `useBreadcrumbs` (16).
- Produces: `<SelectionBar selected building onBuild onClear onRemove />`

- [ ] **Step 1: Write the failing tests**

Replace `apps/frontend/src/components/features/changes/__tests__/SelectionBar.test.tsx` with:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SelectionBar } from "@/components/features/changes/SelectionBar";
import type { Change } from "@/types/models/change";

function change(number: number, mergedAt: string | null): Change {
  return {
    type: "changes",
    id: String(number),
    attributes: {
      number,
      title: `Change ${number}`,
      author: "jsmith",
      sourceBranch: "feat/x",
      targetBranch: "main",
      mergedAt,
      createdAt: "2026-01-01T00:00:00Z",
      landingSha: null,
      webUrl: "https://example.test/1",
    },
  };
}

describe("SelectionBar", () => {
  it("collapses to a prompt with nothing selected", () => {
    render(
      <SelectionBar selected={[]} building={false} onBuild={vi.fn()} onClear={vi.fn()} onRemove={vi.fn()} />,
    );
    expect(screen.getByText("Select one or more changes")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /build review/i })).not.toBeInTheDocument();
  });

  it("numbers chips in apply order", () => {
    render(
      <SelectionBar
        selected={[change(9, "2026-01-01T00:00:00Z"), change(4, "2026-02-01T00:00:00Z")]}
        building={false}
        onBuild={vi.fn()}
        onClear={vi.fn()}
        onRemove={vi.fn()}
      />,
    );
    expect(screen.getByText("2 selected · applied oldest → newest")).toBeInTheDocument();
    const chips = screen.getAllByTestId("selection-chip");
    expect(chips[0]).toHaveTextContent("1");
    expect(chips[0]).toHaveTextContent("#9");
    expect(chips[1]).toHaveTextContent("#4");
  });

  it("removes one change from the selection", async () => {
    const onRemove = vi.fn();
    render(
      <SelectionBar
        selected={[change(9, "2026-01-01T00:00:00Z")]}
        building={false}
        onBuild={vi.fn()}
        onClear={vi.fn()}
        onRemove={onRemove}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /remove #9/i }));
    expect(onRemove.mock.calls[0]?.[0]?.attributes.number).toBe(9);
  });

  it("builds and clears", async () => {
    const onBuild = vi.fn();
    const onClear = vi.fn();
    render(
      <SelectionBar
        selected={[change(1, null)]}
        building={false}
        onBuild={onBuild}
        onClear={onClear}
        onRemove={vi.fn()}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: /build review/i }));
    expect(onBuild).toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Clear" }));
    expect(onClear).toHaveBeenCalled();
  });

  it("disables both actions while building", () => {
    render(
      <SelectionBar
        selected={[change(1, null)]}
        building
        onBuild={vi.fn()}
        onClear={vi.fn()}
        onRemove={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: /build review/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Clear" })).toBeDisabled();
  });
});
```

Replace `apps/frontend/src/pages/__tests__/SelectChangesPage.test.tsx` with:

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { SelectChangesPage } from "@/pages/SelectChangesPage";
import { HttpResponse, http, listDoc, oneDoc, server } from "@/test/server";
import { RECENTS_KEY } from "@/lib/storage/recents";

function change(number: number, title: string, sourceBranch: string, author = "jsmith") {
  return {
    type: "changes" as const,
    id: String(number),
    attributes: {
      number,
      title,
      author,
      sourceBranch,
      targetBranch: "main",
      mergedAt: `2026-01-0${number}T00:00:00Z`,
      createdAt: "2026-01-01T00:00:00Z",
      landingSha: null,
      webUrl: `https://example.test/mr/${number}`,
    },
  };
}

let changeQueries: string[] = [];
let createdBody: unknown = null;

function seed() {
  changeQueries = [];
  createdBody = null;
  server.use(
    http.get("/api/providers", () =>
      HttpResponse.json(
        listDoc([
          {
            type: "providers" as const,
            id: "gl",
            attributes: { kind: "gitlab", displayName: "GitLab", baseUrl: "https://gitlab.test" },
          },
        ]),
      ),
    ),
    http.get("/api/providers/gl/repositories/:repo", () =>
      HttpResponse.json(
        oneDoc("repositories", "atlas/server", {
          name: "server",
          namespace: "atlas",
          defaultBranch: "main",
          webUrl: "https://gitlab.test/atlas/server",
        }),
      ),
    ),
    http.get("/api/providers/gl/repositories/:repo/branches", () =>
      HttpResponse.json(
        listDoc([
          { type: "branches" as const, id: "gl:atlas/server:main", attributes: { name: "main", isDefault: true, sha: "" } },
          { type: "branches" as const, id: "gl:atlas/server:develop", attributes: { name: "develop", isDefault: false, sha: "" } },
        ]),
      ),
    ),
    http.get("/api/providers/gl/repositories/:repo/changes", ({ request }) => {
      changeQueries.push(new URL(request.url).searchParams.get("target") ?? "");
      return HttpResponse.json(
        listDoc(
          [
            change(1, "ATLAS-1 first", "feat/a"),
            change(2, "ATLAS-1 second", "feat/b", "zoe"),
            change(3, "chore(deps): bump lodash", "renovate/lodash-4.x"),
          ],
          { number: 1, size: 30, hasNext: false },
        ),
      );
    }),
    http.post("/api/reviews", async ({ request }) => {
      createdBody = await request.json();
      return HttpResponse.json(
        oneDoc("reviews", "rev-1", {
          status: "CREATING",
          stage: null,
          provider: "gl",
          repository: "atlas/server",
          baseBranch: "main",
          baseSha: null,
          headSha: null,
          baseDescription: "",
          changes: [1, 2],
          included: [],
          totals: null,
          error: null,
          createdAt: "2026-01-01T00:00:00Z",
          updatedAt: "2026-01-01T00:00:00Z",
          expiresAt: "2999-01-01T00:00:00Z",
        }),
        { status: 201 },
      );
    }),
  );
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={["/select?provider=gl&repo=atlas%2Fserver"]}>
          <AppShell>
            <Routes>
              <Route path="/select" element={<SelectChangesPage />} />
              <Route path="/reviews/:id" element={<p>review page</p>} />
            </Routes>
          </AppShell>
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  localStorage.clear();
  sessionStorage.clear();
});

describe("SelectChangesPage", () => {
  it("publishes the repository breadcrumb", async () => {
    seed();
    renderPage();
    await waitFor(() =>
      expect(screen.getByRole("navigation", { name: /breadcrumb/i })).toHaveTextContent(
        "atlas/server",
      ),
    );
  });

  it("records the repository as recent on load", async () => {
    seed();
    renderPage();
    await waitFor(() => {
      const stored = JSON.parse(localStorage.getItem(RECENTS_KEY) ?? "[]") as Array<{
        repository: string;
      }>;
      expect(stored[0]?.repository).toBe("atlas/server");
    });
  });

  it("hides dependency-bot changes by default and can show them", async () => {
    seed();
    renderPage();
    expect(await screen.findByText("ATLAS-1 first")).toBeInTheDocument();
    expect(screen.queryByText(/bump lodash/)).not.toBeInTheDocument();
    expect(screen.getByText(/1 changes hidden by/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Show" }));
    expect(await screen.findByText(/bump lodash/)).toBeInTheDocument();
  });

  it("filters by author chip", async () => {
    seed();
    renderPage();
    await screen.findByText("ATLAS-1 first");
    await userEvent.click(screen.getByRole("button", { name: "zoe" }));
    expect(screen.queryByText("ATLAS-1 first")).not.toBeInTheDocument();
    expect(screen.getByText("ATLAS-1 second")).toBeInTheDocument();
  });

  it("groups by ticket and selects a whole group", async () => {
    seed();
    renderPage();
    await screen.findByText("ATLAS-1 first");
    await userEvent.click(screen.getByRole("button", { name: /group by ticket/i }));
    await userEvent.click(await screen.findByRole("button", { name: "Select all 2" }));
    expect(screen.getByText("2 selected · applied oldest → newest")).toBeInTheDocument();
  });

  it("refetches with the new target when the base changes", async () => {
    seed();
    renderPage();
    await screen.findByText("ATLAS-1 first");
    await userEvent.click(screen.getByRole("combobox", { name: /base/i }));
    await userEvent.click(await screen.findByRole("option", { name: /develop/ }));
    await waitFor(() => expect(changeQueries).toContain("develop"));
  });

  it("posts the selected changes in apply order", async () => {
    seed();
    renderPage();
    await screen.findByText("ATLAS-1 second");
    await userEvent.click(screen.getByText("ATLAS-1 second"));
    await userEvent.click(screen.getByText("ATLAS-1 first"));
    await userEvent.click(screen.getByRole("button", { name: /build review/i }));
    await waitFor(() => expect(createdBody).not.toBeNull());
    expect((createdBody as { changes: number[] }).changes).toEqual([1, 2]);
    expect(await screen.findByText("review page")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/frontend && npx vitest run src/pages/__tests__/SelectChangesPage.test.tsx src/components/features/changes/__tests__/SelectionBar.test.tsx`
Expected: FAIL — the old `SelectionBar` takes a `count` prop; the page has no filters or base select.

- [ ] **Step 3: Rewrite `SelectionBar`**

```tsx
import { Loader2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Hotkey } from "@/components/common/Hotkey";
import { applyOrder } from "@/lib/changes/applyOrder";
import { strings } from "@/lib/strings";
import type { Change } from "@/types/models/change";

interface SelectionBarProps {
  selected: Change[];
  building: boolean;
  onBuild: () => void;
  onClear: () => void;
  onRemove: (change: Change) => void;
}

/**
 * SelectionBar makes the composition of a review visible before it is built:
 * the chips are numbered in apply order, which is exactly the order sent to
 * POST /api/reviews. It is sticky rather than fixed so it stays inside the
 * shared container and never overlaps the page gutters.
 */
export function SelectionBar({ selected, building, onBuild, onClear, onRemove }: SelectionBarProps) {
  const ordered = applyOrder(selected);
  if (ordered.length === 0) {
    return (
      <div className="sticky bottom-4 rounded-lg border border-border bg-card px-4 py-3 text-sm text-muted-foreground">
        {strings.selectOneOrMoreChanges}
      </div>
    );
  }
  return (
    <div className="sticky bottom-4 flex flex-col gap-2 rounded-lg border border-border bg-card px-4 py-3 shadow-sm">
      <div className="flex items-center gap-2">
        <p className="text-sm text-foreground">
          {ordered.length} selected · {strings.appliedOldestToNewest}
        </p>
        <div className="ml-auto flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={onClear} disabled={building}>
            {strings.clear}
          </Button>
          <Button onClick={onBuild} disabled={building}>
            {building ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            {strings.buildReview}
            <Hotkey className="ml-2">↵</Hotkey>
          </Button>
        </div>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {ordered.map((change, index) => (
          <span
            key={change.id}
            data-testid="selection-chip"
            className="flex max-w-[18rem] items-center gap-1.5 rounded-full border border-border py-1 pl-1.5 pr-1 text-xs"
          >
            <span className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-muted text-[0.625rem]">
              {index + 1}
            </span>
            <span className="font-mono">#{change.attributes.number}</span>
            <span className="truncate text-muted-foreground">{change.attributes.title}</span>
            <button
              type="button"
              aria-label={`Remove #${change.attributes.number}`}
              onClick={() => onRemove(change)}
              className="cursor-pointer rounded-full p-0.5 hover:bg-muted"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Rewrite `SelectChangesPage`**

```tsx
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Pagination } from "@/components/common/Pagination";
import { BaseBranchSelect } from "@/components/features/changes/BaseBranchSelect";
import { ChangeFilters } from "@/components/features/changes/ChangeFilters";
import { ChangeTable, buildRows } from "@/components/features/changes/ChangeTable";
import { SelectionBar } from "@/components/features/changes/SelectionBar";
import { useChanges } from "@/lib/hooks/api/useChanges";
import { useRepository } from "@/lib/hooks/api/useRepositories";
import { useCreateReview } from "@/lib/hooks/api/useReviews";
import { useSelection } from "@/lib/hooks/useSelection";
import { useBreadcrumbs } from "@/lib/breadcrumbs/useBreadcrumbs";
import { useHotkeys } from "@/lib/hotkeys/useHotkeys";
import { useStore } from "@/lib/storage/store";
import { changeFiltersStore } from "@/lib/storage/changeFilters";
import { recordRecent } from "@/lib/storage/recents";
import { applyOrder } from "@/lib/changes/applyOrder";
import { isDependencyBot } from "@/lib/changes/dependencyBot";
import { groupByTicket, type TicketGroup } from "@/lib/changes/groupByTicket";
import { distinctAuthors } from "@/lib/changes/authors";
import { isValidRepositoryName } from "@/lib/repositoryInput";
import type { ChangeListParams } from "@/services/api";
import type { CreateReviewRequest } from "@/types/models/review";
import { useProviders } from "@/lib/hooks/api/useProviders";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";

/**
 * isPlausibleBranch is the client half of gitx.ValidateBranchSyntax: enough to
 * reject a nonsense ?base= without duplicating git's full ref rules, which the
 * backend applies anyway when the review is built.
 */
function isPlausibleBranch(value: string | null): boolean {
  if (value === null || value === "" || value.length > 255) return false;
  if (value.startsWith("-") || value.startsWith("/") || value.endsWith("/")) return false;
  return !/[\s~^:?*[\\]|\.\.|@\{/.test(value);
}

export function SelectChangesPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const providerId = params.get("provider") ?? undefined;
  const repository = params.get("repo") ?? undefined;
  const baseParam = params.get("base");

  const [baseState, setBaseState] = useState<{ edited: boolean; value: string }>({
    edited: false,
    value: "",
  });
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [authorFilter, setAuthorFilter] = useState<Set<string>>(new Set());
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [filters, setFilters] = useStore(changeFiltersStore);

  const repositoryQuery = useRepository(providerId, repository ?? "", Boolean(repository));
  const defaultBranch = repositoryQuery.data?.attributes.defaultBranch;

  // Seed the base from ?base= when it is syntactically usable, else from the
  // repository default, during render rather than in an effect (the existing
  // precedent in this file and useSelection.ts). An unusable ?base= is ignored
  // rather than surfaced: the backend validates the name again on build.
  const seeded = isPlausibleBranch(baseParam) ? (baseParam as string) : defaultBranch;
  const baseBranch = baseState.edited ? baseState.value : (seeded ?? baseState.value);
  if (!baseState.edited && seeded && seeded !== baseState.value) {
    setBaseState({ edited: false, value: seeded });
  }

  const selection = useSelection(`converge:selection:${providerId ?? ""}/${repository ?? ""}`);
  const changeParams: ChangeListParams = baseBranch
    ? { target: baseBranch, search, page }
    : { search, page };
  // Wait for the repository lookup to settle before firing the changes
  // request, so no unfiltered "all merged changes" request is issued and then
  // discarded once the default branch resolves.
  const changes = useChanges(providerId, repository, changeParams, !repositoryQuery.isPending);
  const createReview = useCreateReview();

  // FR-16: the create page records the recent, not the drawer, so a deep link
  // counts the same as a trip through the drawer.
  useEffect(() => {
    if (providerId === undefined || repository === undefined || defaultBranch === undefined) return;
    recordRecent({
      provider: providerId,
      repository,
      defaultBranch,
      openedAt: new Date().toISOString(),
    });
  }, [providerId, repository, defaultBranch]);

  // FR-2 wants the provider's display name here, not its id.
  const providers = useProviders();
  const providerName =
    providers.data?.find((p) => p.id === providerId)?.attributes.displayName ?? providerId ?? "";
  useBreadcrumbs(
    useMemo(
      () => [
        { label: strings.reviews, to: "/" },
        { label: providerName },
        { label: repository ?? "" },
      ],
      [providerName, repository],
    ),
  );

  const items = useMemo(() => changes.data?.items ?? [], [changes.data]);
  const authors = useMemo(() => distinctAuthors(items), [items]);
  const afterBots = useMemo(
    () => (filters.hideBots ? items.filter((c) => !isDependencyBot(c)) : items),
    [items, filters.hideBots],
  );
  const visible = useMemo(
    () =>
      authorFilter.size === 0
        ? afterBots
        : afterBots.filter((c) => authorFilter.has(c.attributes.author)),
    [afterBots, authorFilter],
  );
  const groups = useMemo(
    () => (filters.groupByTicket ? groupByTicket(visible) : null),
    [filters.groupByTicket, visible],
  );
  const rows = useMemo(
    () => buildRows(visible, groups, items.length - afterBots.length, selection.isSelected),
    [visible, groups, items.length, afterBots.length, selection],
  );

  const allSelected = visible.length > 0 && visible.every((c) => selection.isSelected(c.attributes.number));
  const someSelected = !allSelected && visible.some((c) => selection.isSelected(c.attributes.number));

  const canBuild = selection.count > 0 && !createReview.isPending;
  useHotkeys({ Enter: () => void build() }, { enabled: canBuild && !popoverOpen });

  if (!providerId || !repository || !isValidRepositoryName(repository)) {
    return (
      <ErrorBanner
        title="Missing selection"
        detail="Go back and choose a provider and repository."
      />
    );
  }

  function toggleAuthor(author: string): void {
    setAuthorFilter((current) => {
      const next = new Set(current);
      if (next.has(author)) next.delete(author);
      else next.add(author);
      return next;
    });
  }

  function toggleGroup(group: TicketGroup, select: boolean): void {
    for (const change of group.changes) {
      if (selection.isSelected(change.attributes.number) !== select) selection.toggle(change);
    }
  }

  function toggleAll(select: boolean): void {
    for (const change of visible) {
      if (selection.isSelected(change.attributes.number) !== select) selection.toggle(change);
    }
  }

  async function build(): Promise<void> {
    setCreateError(null);
    const ordered = applyOrder(selection.selected.values()).map((c) => c.attributes.number);
    try {
      const request: CreateReviewRequest = baseBranch
        ? { provider: providerId as string, repository: repository as string, baseBranch, changes: ordered }
        : { provider: providerId as string, repository: repository as string, changes: ordered };
      const review = await createReview.mutateAsync(request);
      if (defaultBranch !== undefined) {
        recordRecent({
          provider: providerId as string,
          repository: repository as string,
          defaultBranch,
          openedAt: new Date().toISOString(),
        });
      }
      selection.clear();
      navigate(`/reviews/${review.id}`);
    } catch (error: unknown) {
      const detail = messageFor(error, "The review could not be started.");
      setCreateError(detail);
      toast.error(detail);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <PageHeader
          title={repository}
          description="Select the merged changes to review together. They are applied in merge order onto the base."
        />
        <BaseBranchSelect
          providerId={providerId}
          repository={repository}
          value={baseBranch}
          defaultBranch={defaultBranch}
          onOpenChange={setPopoverOpen}
          onChange={(branch) => {
            setBaseState({ edited: true, value: branch });
            setPage(1);
            selection.clear();
          }}
        />
      </div>
      <ChangeFilters
        search={search}
        onSearchChange={(value) => {
          setSearch(value);
          setPage(1);
        }}
        authors={authors}
        activeAuthors={authorFilter}
        onToggleAuthor={toggleAuthor}
        hideBots={filters.hideBots}
        onHideBotsChange={(next) => setFilters((prev) => ({ ...prev, hideBots: next }))}
        groupByTicket={filters.groupByTicket}
        onGroupByTicketChange={(next) => setFilters((prev) => ({ ...prev, groupByTicket: next }))}
        shown={visible.length}
        total={items.length}
      />
      {createError ? <ErrorBanner title="Could not start the review" detail={createError} /> : null}
      {changes.isError ? (
        <ErrorBanner
          title={`Could not load ${strings.includedChanges.toLowerCase()}`}
          detail={messageFor(changes.error, "Try again in a moment.")}
          onRetry={() => void changes.refetch()}
        />
      ) : (
        <ChangeTable
          rows={rows}
          loading={changes.isLoading || repositoryQuery.isPending}
          isSelected={selection.isSelected}
          onToggle={selection.toggle}
          onToggleGroup={toggleGroup}
          onToggleAll={toggleAll}
          allSelected={allSelected}
          someSelected={someSelected}
          onShowBots={() => setFilters((prev) => ({ ...prev, hideBots: false }))}
        />
      )}
      <Pagination
        page={page}
        hasNext={changes.data?.page?.hasNext ?? false}
        onChange={setPage}
        disabled={changes.isFetching}
      />
      <SelectionBar
        selected={[...selection.selected.values()]}
        building={createReview.isPending}
        onBuild={() => void build()}
        onClear={selection.clear}
        onRemove={selection.toggle}
      />
    </div>
  );
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/pages/__tests__/SelectChangesPage.test.tsx src/components/features/changes`
Expected: PASS.

- [ ] **Step 6: Lint and commit**

```bash
cd apps/frontend && npm run lint && npm run format:check
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/features/changes apps/frontend/src/pages/SelectChangesPage.tsx apps/frontend/src/pages/__tests__/SelectChangesPage.test.tsx
git commit -m "feat(frontend): rebuild the create page around filters, grouping, and apply order"
```

---

# Phase E — Review page

### Task 23: The review status line

**Files:**
- Create: `apps/frontend/src/components/features/review/{ReviewStatusLine.tsx,IncludedChangesPopover.tsx}`
- Test: `apps/frontend/src/components/features/review/__tests__/ReviewStatusLine.test.tsx`

**Interfaces:**
- Consumes: `ticketKey` (11), `ProgressBar` (16), `DiscardDialog` (17), `viewedProgress` (12), `shortSha` from `@/types/models/change`.
- Produces: `<ReviewStatusLine review files viewed onFinish onDiscard pending />`, `<IncludedChangesPopover included />`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/components/features/review/__tests__/ReviewStatusLine.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ReviewStatusLine } from "@/components/features/review/ReviewStatusLine";
import type { IncludedChange, Review } from "@/types/models/review";
import type { ReviewFile } from "@/types/models/reviewFile";

function included(number: number, title: string): IncludedChange {
  return {
    number,
    title,
    author: "jsmith",
    mergedAt: "2026-01-01T00:00:00Z",
    webUrl: `https://example.test/mr/${number}`,
    strategy: "squash",
  };
}

function review(overrides: Partial<Review["attributes"]> = {}): Review {
  return {
    type: "reviews",
    id: "r1",
    attributes: {
      status: "READY",
      stage: null,
      provider: "gl",
      repository: "atlas/server",
      baseBranch: "main",
      baseSha: "6140736dbb1a0a0f1f0e2a1b3c4d5e6f70819a2b",
      headSha: null,
      baseDescription: "before the squash landed",
      changes: [421, 430],
      included: [included(421, "ATLAS-7 add a thing"), included(430, "tidy up")],
      totals: { files: 4, additions: 120, deletions: 30 },
      error: null,
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
      expiresAt: "2999-01-01T00:00:00Z",
      ...overrides,
    },
  };
}

function file(path: string): ReviewFile {
  return {
    type: "review-files",
    id: path,
    attributes: { path, previousPath: "", status: "modified", additions: 1, deletions: 1, binary: false },
  };
}

function renderLine(props: Partial<React.ComponentProps<typeof ReviewStatusLine>> = {}) {
  const onFinish = vi.fn();
  const onDiscard = vi.fn();
  render(
    <ReviewStatusLine
      review={review()}
      files={[file("a.ts"), file("b.ts"), file("c.ts"), file("d.ts")]}
      viewed={new Set(["a.ts"])}
      onFinish={onFinish}
      onDiscard={onDiscard}
      pending={false}
      {...props}
    />,
  );
  return { onFinish, onDiscard };
}

describe("ReviewStatusLine", () => {
  it("shows the ticket badge and one badge per included change", () => {
    renderLine();
    expect(screen.getByText("ATLAS-7")).toBeInTheDocument();
    expect(screen.getByText("#421")).toBeInTheDocument();
    expect(screen.getByText("#430")).toBeInTheDocument();
  });

  it("shows the base branch, short sha, and description", () => {
    renderLine();
    expect(screen.getByText(/main @ 6140736/)).toBeInTheDocument();
    expect(screen.getByText(/before the squash landed/)).toBeInTheDocument();
  });

  it("shows totals and viewed progress", () => {
    renderLine();
    expect(screen.getByText(/4 files/)).toBeInTheDocument();
    expect(screen.getByText("+120")).toBeInTheDocument();
    expect(screen.getByText("−30")).toBeInTheDocument();
    expect(screen.getByText("1 / 4 viewed")).toBeInTheDocument();
  });

  it("lists the included changes in a popover", async () => {
    renderLine();
    await userEvent.click(screen.getByRole("button", { name: /details/i }));
    expect(await screen.findByText("ATLAS-7 add a thing")).toBeInTheDocument();
    expect(screen.getByText("tidy up")).toBeInTheDocument();
    expect(screen.getAllByText(/squash/)).not.toHaveLength(0);
  });

  it("finishes without a confirmation", async () => {
    const { onFinish } = renderLine();
    await userEvent.click(screen.getByRole("button", { name: /finish review/i }));
    expect(onFinish).toHaveBeenCalled();
  });

  it("confirms before discarding", async () => {
    const { onDiscard } = renderLine();
    await userEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onDiscard).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: /discard review/i }));
    expect(onDiscard).toHaveBeenCalled();
  });

  it("omits the ticket badge when no included title carries a key", () => {
    renderLine({ review: review({ included: [included(1, "tidy up")] }) });
    expect(screen.queryByText(/[A-Z]+-\d+/)).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/components/features/review/__tests__/ReviewStatusLine.test.tsx`
Expected: FAIL — module does not resolve.

- [ ] **Step 3: Implement `IncludedChangesPopover`**

```tsx
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { strings } from "@/lib/strings";
import type { IncludedChange } from "@/types/models/review";

function mergedLabel(value: string | null): string {
  if (value === null) return "—";
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? "—" : new Date(ms).toLocaleDateString();
}

/** IncludedChangesPopover keeps the per-change detail out of the status line. */
export function IncludedChangesPopover({ included }: { included: IncludedChange[] }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm" className="h-6 px-1.5 text-xs">
          {strings.details} ▾
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[26rem] p-0">
        <ul className="divide-y divide-border">
          {included.map((change) => (
            <li key={change.number} className="flex flex-col gap-0.5 px-3 py-2">
              <a
                href={change.webUrl}
                target="_blank"
                rel="noreferrer"
                className="truncate text-sm font-medium hover:underline"
              >
                #{change.number} {change.title}
              </a>
              <span className="text-xs text-muted-foreground">
                {change.author} · {mergedLabel(change.mergedAt)} · {change.strategy}
              </span>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}
```

- [ ] **Step 4: Implement `ReviewStatusLine`**

```tsx
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ProgressBar } from "@/components/common/ProgressBar";
import { DiscardDialog } from "@/components/features/reviews/DiscardDialog";
import { IncludedChangesPopover } from "@/components/features/review/IncludedChangesPopover";
import { ticketKey } from "@/lib/changes/ticketKey";
import { viewedProgress } from "@/lib/review/progress";
import { shortSha } from "@/types/models/change";
import { strings } from "@/lib/strings";
import type { Review } from "@/types/models/review";
import type { ReviewFile } from "@/types/models/reviewFile";

interface ReviewStatusLineProps {
  review: Review;
  files: ReviewFile[];
  viewed: ReadonlySet<string>;
  onFinish: () => void;
  onDiscard: () => void;
  pending: boolean;
}

export function ReviewStatusLine({
  review,
  files,
  viewed,
  onFinish,
  onDiscard,
  pending,
}: ReviewStatusLineProps) {
  const [confirming, setConfirming] = useState(false);
  const { status, included, baseBranch, baseSha, baseDescription, totals } = review.attributes;
  // The first key found across the included titles, so a multi-change review
  // for one ticket still reads as that ticket.
  const key = useMemo(() => {
    for (const change of included) {
      const found = ticketKey(change.title);
      if (found !== null) return found;
    }
    return null;
  }, [included]);
  const progress = viewedProgress(files, viewed);

  return (
    <div className="flex flex-wrap items-center gap-3 border-b border-border pb-3">
      <Badge variant={status === "READY" ? "secondary" : "outline"}>{strings.statusReady}</Badge>
      {key ? <Badge>{key}</Badge> : null}
      <span className="flex items-center gap-1">
        {included.map((change) => (
          <Badge key={change.number} variant="outline" className="font-mono">
            #{change.number}
          </Badge>
        ))}
        <IncludedChangesPopover included={included} />
      </span>
      <Separator orientation="vertical" className="h-5" />
      <span className="truncate text-xs text-muted-foreground">
        {strings.base}{" "}
        <span className="font-mono text-foreground">
          {baseBranch} @ {shortSha(baseSha)}
        </span>
        {baseDescription ? ` · ${baseDescription}` : null}
      </span>
      <span className="ml-auto flex items-center gap-4">
        {totals ? (
          <span className="text-xs text-muted-foreground">
            {totals.files} files · <span className="text-foreground">+{totals.additions}</span>{" "}
            <span className="text-destructive">−{totals.deletions}</span>
          </span>
        ) : null}
        <ProgressBar
          value={progress.percent}
          label={`${progress.viewed} / ${progress.total} viewed`}
        />
        <Button variant="ghost" size="sm" disabled={pending} onClick={() => setConfirming(true)}>
          {strings.discard}
        </Button>
        <Button size="sm" disabled={pending} onClick={onFinish}>
          {strings.finishReview}
        </Button>
      </span>
      <DiscardDialog
        open={confirming}
        onOpenChange={setConfirming}
        pending={pending}
        onConfirm={() => {
          setConfirming(false);
          onDiscard();
        }}
      />
    </div>
  );
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/components/features/review/__tests__/ReviewStatusLine.test.tsx`
Expected: PASS (7 tests).

- [ ] **Step 6: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/features/review/ReviewStatusLine.tsx apps/frontend/src/components/features/review/IncludedChangesPopover.tsx apps/frontend/src/components/features/review/__tests__/ReviewStatusLine.test.tsx
git commit -m "feat(frontend): add the review status line with included-change details"
```

---

### Task 24: The directory file tree

**Files:**
- Rewrite: `apps/frontend/src/components/features/review/FileTree.tsx`
- Create: `apps/frontend/src/components/features/review/FileTreeRow.tsx`
- Rewrite: `apps/frontend/src/components/features/review/__tests__/FileTree.test.tsx`

**Interfaces:**
- Consumes: `buildTree`, `filterTree`, `flattenVisible`, `ancestorDirs`, `TreeNode` (Task 12).
- Produces: `<FileTree files viewed selectedPath onSelect onToggleViewed />` where `onSelect(path: string, source: "pointer" | "keyboard")`.

- [ ] **Step 1: Write the failing test**

Replace `apps/frontend/src/components/features/review/__tests__/FileTree.test.tsx` with:

```tsx
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { FileTree } from "@/components/features/review/FileTree";
import type { FileStatus, ReviewFile } from "@/types/models/reviewFile";

function file(path: string, status: FileStatus = "modified", additions = 3, deletions = 1): ReviewFile {
  return {
    type: "review-files",
    id: path,
    attributes: { path, previousPath: "", status, additions, deletions, binary: false },
  };
}

const files = [
  file("src/main/java/com/atlas/App.java", "modified"),
  file("src/test/AppTest.java", "added"),
  file("README.md", "deleted"),
];

function renderTree(props: Partial<React.ComponentProps<typeof FileTree>> = {}) {
  const onSelect = vi.fn();
  const onToggleViewed = vi.fn();
  render(
    <FileTree
      files={files}
      viewed={new Set<string>()}
      selectedPath="README.md"
      onSelect={onSelect}
      onToggleViewed={onToggleViewed}
      {...props}
    />,
  );
  return { onSelect, onToggleViewed };
}

describe("FileTree", () => {
  it("collapses single-child directory chains into one row", () => {
    renderTree();
    expect(screen.getByText("src/main/java/com/atlas")).toBeInTheDocument();
    expect(screen.getByText("App.java")).toBeInTheDocument();
  });

  it("shows the status letter and line counts per file", () => {
    renderTree();
    const row = screen.getByRole("treeitem", { name: /README\.md/ });
    expect(within(row).getByText("D")).toBeInTheDocument();
    expect(within(row).getByText("+3")).toBeInTheDocument();
    expect(within(row).getByText("−1")).toBeInTheDocument();
  });

  it("marks the selected file", () => {
    renderTree();
    expect(screen.getByRole("treeitem", { name: /README\.md/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("reports a pointer selection", async () => {
    const { onSelect } = renderTree();
    await userEvent.click(screen.getByText("App.java"));
    expect(onSelect).toHaveBeenCalledWith("src/main/java/com/atlas/App.java", "pointer");
  });

  it("toggles viewed from the row checkbox without selecting the file", async () => {
    const { onSelect, onToggleViewed } = renderTree();
    await userEvent.click(screen.getByRole("checkbox", { name: /mark App\.java viewed/i }));
    expect(onToggleViewed).toHaveBeenCalledWith("src/main/java/com/atlas/App.java");
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("renders viewed files muted and checked", () => {
    renderTree({ viewed: new Set(["README.md"]) });
    expect(screen.getByRole("checkbox", { name: /mark README\.md viewed/i })).toBeChecked();
  });

  it("collapses and expands a directory", async () => {
    renderTree();
    await userEvent.click(screen.getByRole("button", { name: /collapse src\/test/i }));
    expect(screen.queryByText("AppTest.java")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /expand src\/test/i }));
    expect(screen.getByText("AppTest.java")).toBeInTheDocument();
  });

  it("filters rows by a substring of the full path", async () => {
    renderTree();
    await userEvent.type(screen.getByPlaceholderText(/filter files/i), "test");
    expect(screen.getByText("AppTest.java")).toBeInTheDocument();
    expect(screen.queryByText("App.java")).not.toBeInTheDocument();
    expect(screen.queryByText("README.md")).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/components/features/review/__tests__/FileTree.test.tsx`
Expected: FAIL — the current tree groups by directory string and has no checkboxes or filter.

- [ ] **Step 3: Implement `FileTreeRow`**

```tsx
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import type { FileStatus, ReviewFile } from "@/types/models/reviewFile";

const STATUS_LETTER: Record<FileStatus, string> = {
  modified: "M",
  added: "A",
  deleted: "D",
  renamed: "R",
};

const STATUS_COLOR: Record<FileStatus, string> = {
  modified: "text-amber-500",
  added: "text-green-500",
  deleted: "text-destructive",
  renamed: "text-blue-500",
};

function baseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

interface FileTreeRowProps {
  file: ReviewFile;
  depth: number;
  selected: boolean;
  viewed: boolean;
  onSelect: (path: string, source: "pointer" | "keyboard") => void;
  onToggleViewed: (path: string) => void;
}

export function FileTreeRow({
  file,
  depth,
  selected,
  viewed,
  onSelect,
  onToggleViewed,
}: FileTreeRowProps) {
  const { path, status, additions, deletions } = file.attributes;
  const name = baseName(path);
  return (
    <div
      role="treeitem"
      aria-selected={selected}
      aria-label={path}
      data-path={path}
      onClick={() => onSelect(path, "pointer")}
      className={cn(
        "flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm",
        // A filled background and brighter, bolder text mark the selection --
        // no left accent bar, per the agreed mockup.
        selected ? "bg-accent font-medium text-accent-foreground" : "hover:bg-muted",
        viewed && !selected && "text-muted-foreground opacity-70",
      )}
      style={{ paddingLeft: `${0.5 + depth * 0.75}rem` }}
    >
      <span onClick={(event) => event.stopPropagation()}>
        <Checkbox
          checked={viewed}
          aria-label={`Mark ${name} viewed`}
          onCheckedChange={() => onToggleViewed(path)}
        />
      </span>
      <span className={cn("w-3 shrink-0 font-mono text-xs", STATUS_COLOR[status])}>
        {STATUS_LETTER[status]}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs">{name}</span>
      <span className="shrink-0 text-[0.6875rem] text-foreground">+{additions}</span>
      <span className="shrink-0 text-[0.6875rem] text-destructive">−{deletions}</span>
    </div>
  );
}
```

- [ ] **Step 4: Rewrite `FileTree`**

```tsx
import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { FileTreeRow } from "@/components/features/review/FileTreeRow";
import { ancestorDirs, buildTree, filterTree, type TreeNode } from "@/lib/review/fileTree";
import type { ReviewFile } from "@/types/models/reviewFile";

interface FileTreeProps {
  files: ReviewFile[];
  viewed: ReadonlySet<string>;
  selectedPath: string | undefined;
  onSelect: (path: string, source: "pointer" | "keyboard") => void;
  onToggleViewed: (path: string) => void;
  /** Set when the selection came from j/k or the footer, so the row scrolls into view. */
  scrollSelectionIntoView?: boolean;
}

export function FileTree({
  files,
  viewed,
  selectedPath,
  onSelect,
  onToggleViewed,
  scrollSelectionIntoView = false,
}: FileTreeProps) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [query, setQuery] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);

  // Memoised per file list and per query: a 200-file tree is rebuilt on every
  // keystroke otherwise, and this component re-renders on every viewed toggle.
  const tree = useMemo(() => buildTree(files), [files]);
  const filtered = useMemo(() => filterTree(tree, query), [tree, query]);
  const filtering = query.trim() !== "";

  // The selected file must always be reachable: expanding its ancestors is
  // what makes j/k across a collapsed directory land somewhere visible.
  useEffect(() => {
    if (selectedPath === undefined) return;
    const ancestors = ancestorDirs(selectedPath);
    setCollapsed((current) => {
      if (!ancestors.some((dir) => current.has(dir))) return current;
      const next = new Set(current);
      for (const dir of ancestors) next.delete(dir);
      return next;
    });
  }, [selectedPath]);

  useEffect(() => {
    if (!scrollSelectionIntoView || selectedPath === undefined) return;
    const row = containerRef.current?.querySelector(`[data-path="${CSS.escape(selectedPath)}"]`);
    row?.scrollIntoView({ block: "nearest" });
  }, [scrollSelectionIntoView, selectedPath]);

  function toggleDir(path: string): void {
    setCollapsed((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }

  function renderNodes(nodes: TreeNode[], depth: number): React.ReactNode {
    return nodes.map((node) => {
      if (node.kind === "file") {
        return (
          <FileTreeRow
            key={node.file.attributes.path}
            file={node.file}
            depth={depth}
            selected={node.file.attributes.path === selectedPath}
            viewed={viewed.has(node.file.attributes.path)}
            onSelect={onSelect}
            onToggleViewed={onToggleViewed}
          />
        );
      }
      // A filter result is always open: hiding a match behind a collapsed
      // parent would make the filter look broken.
      const open = filtering || !collapsed.has(node.path);
      return (
        <div key={node.path}>
          <button
            type="button"
            onClick={() => toggleDir(node.path)}
            aria-label={`${open ? "Collapse" : "Expand"} ${node.name}`}
            aria-expanded={open}
            className="flex w-full cursor-pointer items-center gap-1 rounded px-2 py-1 text-left text-xs text-muted-foreground hover:bg-muted"
            style={{ paddingLeft: `${0.5 + depth * 0.75}rem` }}
          >
            {open ? (
              <ChevronDown className="h-3 w-3 shrink-0" />
            ) : (
              <ChevronRight className="h-3 w-3 shrink-0" />
            )}
            <span className="truncate font-mono">{node.name}</span>
          </button>
          {open ? renderNodes(node.children, depth + 1) : null}
        </div>
      );
    });
  }

  return (
    <div className="flex h-full min-h-0 flex-col border-r border-border">
      <div className="border-b border-border p-2">
        <Input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Filter files"
          className="h-8 text-xs"
        />
      </div>
      <ScrollArea className="min-h-0 flex-1">
        <div ref={containerRef} role="tree" aria-label="Changed files" className="p-1">
          {renderNodes(filtered, 0)}
        </div>
      </ScrollArea>
    </div>
  );
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd apps/frontend && npx vitest run src/components/features/review/__tests__/FileTree.test.tsx`
Expected: PASS (8 tests).

- [ ] **Step 6: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/features/review/FileTree.tsx apps/frontend/src/components/features/review/FileTreeRow.tsx apps/frontend/src/components/features/review/__tests__/FileTree.test.tsx
git commit -m "feat(frontend): rewrite the file tree with directories, viewed state, and a filter"
```

---

### Task 25: The diff pane

**Files:**
- Create: `apps/frontend/src/components/features/review/{DiffPane.tsx,FileHeader.tsx,FileFooter.tsx,ReviewWorkspace.tsx}`
- Modify: `apps/frontend/src/components/features/review/FileDiff.tsx`
- Modify: `apps/frontend/src/components/features/review/__tests__/FileDiff.test.tsx`
- Test: `apps/frontend/src/components/features/review/__tests__/DiffPane.test.tsx`

**Interfaces:**
- Consumes: `openInProviderHref` (Task 12).
- Produces:
  - `<ReviewWorkspace>{tree}{pane}</ReviewWorkspace>`
  - `<FileHeader file href providerName viewed onToggleViewed />`
  - `<FileFooter index total nextName onNext />`
  - `<DiffPane file fileDiff loading error onRetry href providerName viewed onToggleViewed index total nextName onNext />`

- [ ] **Step 1: Write the failing test**

Create `apps/frontend/src/components/features/review/__tests__/DiffPane.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DiffPane } from "@/components/features/review/DiffPane";
import type { ReviewFileDiff } from "@/types/models/reviewFile";

vi.mock("@/components/features/review/FileDiff", () => ({
  FileDiff: () => <div data-testid="file-diff" />,
}));

function diff(path: string): ReviewFileDiff {
  return {
    type: "review-file-diffs",
    id: path,
    attributes: {
      path,
      previousPath: "",
      status: "modified",
      additions: 12,
      deletions: 3,
      binary: false,
      truncated: false,
      diff: "diff --git a/x b/x\n",
    },
  };
}

function renderPane(props: Partial<React.ComponentProps<typeof DiffPane>> = {}) {
  const onToggleViewed = vi.fn();
  const onNext = vi.fn();
  render(
    <DiffPane
      fileDiff={diff("src/app/main.ts")}
      loading={false}
      href="https://example.test/mr/421"
      providerName="GitLab"
      viewed={false}
      onToggleViewed={onToggleViewed}
      index={0}
      total={3}
      nextName="other.ts"
      onNext={onNext}
      {...props}
    />,
  );
  return { onToggleViewed, onNext };
}

describe("DiffPane", () => {
  it("shows the full path with the file name emphasised and the line counts", () => {
    renderPane();
    expect(screen.getByText("src/app/")).toBeInTheDocument();
    expect(screen.getByText("main.ts")).toBeInTheDocument();
    expect(screen.getByText("+12")).toBeInTheDocument();
    expect(screen.getByText("−3")).toBeInTheDocument();
  });

  it("links out to the provider", () => {
    renderPane();
    expect(screen.getByRole("link", { name: /open in gitlab/i })).toHaveAttribute(
      "href",
      "https://example.test/mr/421",
    );
  });

  it("omits the provider link when there is no target", () => {
    renderPane({ href: undefined });
    expect(screen.queryByRole("link", { name: /open in/i })).not.toBeInTheDocument();
  });

  it("copies the path", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    renderPane();
    await userEvent.click(screen.getByRole("button", { name: /copy path/i }));
    expect(writeText).toHaveBeenCalledWith("src/app/main.ts");
  });

  it("toggles viewed from the header", async () => {
    const { onToggleViewed } = renderPane();
    await userEvent.click(screen.getByRole("button", { name: /viewed/i }));
    expect(onToggleViewed).toHaveBeenCalled();
  });

  it("shows File i of n and the next file button", async () => {
    const { onNext } = renderPane();
    expect(screen.getByText("File 1 of 3")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /next file: other\.ts/i }));
    expect(onNext).toHaveBeenCalled();
  });

  it("reads Back to first file on the last file", () => {
    renderPane({ index: 2, nextName: "first.ts", isLast: true });
    expect(screen.getByRole("button", { name: /back to first file/i })).toBeInTheDocument();
  });

  it("renders a skeleton while the diff loads", () => {
    renderPane({ loading: true, fileDiff: undefined });
    expect(screen.queryByTestId("file-diff")).not.toBeInTheDocument();
  });
});
```

Update `apps/frontend/src/components/features/review/__tests__/FileDiff.test.tsx`'s
options assertion to the new object (keep the existing binary and truncated
cases unchanged):

```ts
    expect(options).toMatchObject({
      diffStyle: "unified",
      expandUnchanged: false,
      collapsedContextThreshold: 8,
      expansionLineCount: 20,
      lineDiffType: "word",
      disableFileHeader: true,
      overflow: "scroll",
    });
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd apps/frontend && npx vitest run src/components/features/review`
Expected: FAIL — `DiffPane` does not resolve; `FileDiff` still passes `expandUnchanged: true`.

- [ ] **Step 3: Update `FileDiff`'s options**

In `FileDiff.tsx`, replace the `options` object:

```tsx
        options={{
          diffStyle: "unified",
          // expandUnchanged: false is what produces the fold rows. The backend
          // now sends 40 lines of context per hunk (diff.fileDiffContext), so
          // there are real unmodified runs for the library to collapse and
          // expand -- all client-side, with no second request (FR-34).
          expandUnchanged: false,
          collapsedContextThreshold: 8,
          expansionLineCount: 20,
          // Word-level intra-line highlighting on paired modified lines (FR-35).
          lineDiffType: "word",
          // Our own sticky FileHeader owns the path, counts, and actions.
          disableFileHeader: true,
          overflow: "scroll",
          themeType: resolved,
          theme: { light: "pierre-light", dark: "pierre-dark" },
        }}
```

- [ ] **Step 4: Implement `FileHeader` and `FileFooter`**

`FileHeader.tsx`:

```tsx
import { Check, Copy, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { strings } from "@/lib/strings";
import { cn } from "@/lib/utils";
import type { FileStatus } from "@/types/models/reviewFile";

const STATUS_LETTER: Record<FileStatus, string> = {
  modified: "M",
  added: "A",
  deleted: "D",
  renamed: "R",
};

const STATUS_COLOR: Record<FileStatus, string> = {
  modified: "text-amber-500",
  added: "text-green-500",
  deleted: "text-destructive",
  renamed: "text-blue-500",
};

interface FileHeaderProps {
  path: string;
  status: FileStatus;
  additions: number;
  deletions: number;
  href: string | undefined;
  providerName: string;
  viewed: boolean;
  onToggleViewed: () => void;
}

/** FileHeader is sticky at the top of the diff pane's own scroll container. */
export function FileHeader({
  path,
  status,
  additions,
  deletions,
  href,
  providerName,
  viewed,
  onToggleViewed,
}: FileHeaderProps) {
  const cut = path.lastIndexOf("/");
  const directory = cut === -1 ? "" : path.slice(0, cut + 1);
  const name = cut === -1 ? path : path.slice(cut + 1);

  async function copyPath(): Promise<void> {
    try {
      await navigator.clipboard.writeText(path);
      toast.success("Path copied");
    } catch {
      toast.error("The path could not be copied.");
    }
  }

  return (
    <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-border bg-card px-3 py-2">
      <span className={cn("w-3 shrink-0 font-mono text-xs", STATUS_COLOR[status])}>
        {STATUS_LETTER[status]}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">
        {directory}
        <span className="font-semibold text-foreground">{name}</span>
      </span>
      <span className="shrink-0 text-xs text-foreground">+{additions}</span>
      <span className="shrink-0 text-xs text-destructive">−{deletions}</span>
      <Button variant="ghost" size="sm" onClick={() => void copyPath()}>
        <Copy className="mr-1 h-3 w-3" />
        {strings.copyPath}
      </Button>
      {href !== undefined ? (
        <Button variant="ghost" size="sm" asChild>
          <a href={href} target="_blank" rel="noreferrer">
            <ExternalLink className="mr-1 h-3 w-3" />
            Open in {providerName}
          </a>
        </Button>
      ) : null}
      <Button variant={viewed ? "default" : "outline"} size="sm" onClick={onToggleViewed}>
        <Check className="mr-1 h-3 w-3" />
        {strings.viewed}
      </Button>
    </div>
  );
}
```

`FileFooter.tsx`:

```tsx
import { ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Hotkey } from "@/components/common/Hotkey";
import { strings } from "@/lib/strings";

interface FileFooterProps {
  index: number;
  total: number;
  nextName: string | undefined;
  isLast: boolean;
  onNext: () => void;
}

/** FileFooter gives the reviewer forward motion without a trip to the tree. */
export function FileFooter({ index, total, nextName, isLast, onNext }: FileFooterProps) {
  return (
    <div className="flex items-center gap-2 border-t border-border px-3 py-2">
      <span className="text-xs text-muted-foreground">
        File {index + 1} of {total}
      </span>
      <Button variant="outline" size="sm" className="ml-auto" onClick={onNext}>
        {isLast ? strings.backToFirstFile : `${strings.nextFile}: ${nextName ?? ""}`}
        <ArrowRight className="ml-1 h-3 w-3" />
        <Hotkey className="ml-2">j</Hotkey>
      </Button>
    </div>
  );
}
```

- [ ] **Step 5: Implement `ReviewWorkspace` and `DiffPane`**

`ReviewWorkspace.tsx`:

```tsx
import type { ReactNode } from "react";

/**
 * ReviewWorkspace is the fixed-height split frame: 280 px of tree beside the
 * diff, each side scrolling independently, together filling the viewport below
 * the status line. min-h keeps it usable on a short viewport, where the page
 * scrolls as a whole instead.
 */
export function ReviewWorkspace({ children }: { children: ReactNode }) {
  return (
    <div className="grid h-[calc(100vh-14rem)] min-h-[24rem] grid-cols-[280px_1fr] overflow-hidden rounded-lg border border-border bg-card">
      {children}
    </div>
  );
}
```

`DiffPane.tsx`:

```tsx
import { Suspense, lazy, useLayoutEffect, useRef } from "react";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { Skeleton } from "@/components/ui/skeleton";
import { FileFooter } from "@/components/features/review/FileFooter";
import { FileHeader } from "@/components/features/review/FileHeader";
import { messageFor } from "@/lib/api/errors";
import type { ReviewFileDiff } from "@/types/models/reviewFile";

// Shiki is heavy; keep the diff renderer out of the initial bundle.
const FileDiff = lazy(async () => ({
  default: (await import("@/components/features/review/FileDiff")).FileDiff,
}));

interface DiffPaneProps {
  fileDiff: ReviewFileDiff | undefined;
  loading: boolean;
  error?: unknown;
  onRetry?: () => void;
  href: string | undefined;
  providerName: string;
  viewed: boolean;
  onToggleViewed: () => void;
  index: number;
  total: number;
  nextName: string | undefined;
  isLast?: boolean;
  onNext: () => void;
}

export function DiffPane({
  fileDiff,
  loading,
  error,
  onRetry,
  href,
  providerName,
  viewed,
  onToggleViewed,
  index,
  total,
  nextName,
  isLast = false,
  onNext,
}: DiffPaneProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const path = fileDiff?.attributes.path;

  // Selecting a file must start at the top of that file, not wherever the
  // previous file was scrolled to (FR-37).
  useLayoutEffect(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = 0;
  }, [path]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto">
        {fileDiff ? (
          <FileHeader
            path={fileDiff.attributes.path}
            status={fileDiff.attributes.status}
            additions={fileDiff.attributes.additions}
            deletions={fileDiff.attributes.deletions}
            href={href}
            providerName={providerName}
            viewed={viewed}
            onToggleViewed={onToggleViewed}
          />
        ) : null}
        <div className="p-3">
          {error ? (
            <ErrorBanner
              title="Could not load this file's diff"
              detail={messageFor(error, "Try again in a moment.")}
              {...(onRetry ? { onRetry } : {})}
            />
          ) : loading || !fileDiff ? (
            <div className="space-y-2">
              {Array.from({ length: 12 }, (_, row) => (
                <Skeleton key={row} className="h-4 w-full" />
              ))}
            </div>
          ) : (
            <Suspense fallback={<Skeleton className="h-96 w-full" />}>
              <FileDiff file={fileDiff} />
            </Suspense>
          )}
        </div>
      </div>
      {total > 0 ? (
        <FileFooter
          index={index}
          total={total}
          nextName={nextName}
          isLast={isLast}
          onNext={onNext}
        />
      ) : null}
    </div>
  );
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/components/features/review`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add apps/frontend/src/components/features/review
git commit -m "feat(frontend): add the diff pane with a sticky header, folds, and next-file"
```

---

### Task 26: Wire the review page together

**Files:**
- Rewrite: `apps/frontend/src/pages/ReviewPage.tsx`
- Delete: `apps/frontend/src/components/features/review/ReviewHeader.tsx`
- Test: `apps/frontend/src/pages/__tests__/ReviewPage.test.tsx`

**Interfaces:**
- Consumes: everything from Tasks 23–25, plus `viewedStore`/`toggleViewed`/`clearViewed` (10), `flattenVisible`/`buildTree` (12), `useHotkeys` (13), `openInProviderHref` (12), `useBreadcrumbs` (16), `useRepository` (15).

- [ ] **Step 1: Write the failing test**

Replace `apps/frontend/src/pages/__tests__/ReviewPage.test.tsx` with (keeping any
existing CREATING / CONFLICTED / FINISHED cases that still apply — those panels
are unchanged — and adding these):

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AppShell } from "@/components/layout/AppShell";
import { ThemeProvider } from "@/components/theme/ThemeProvider";
import { ReviewPage } from "@/pages/ReviewPage";
import { HttpResponse, http, listDoc, oneDoc, server } from "@/test/server";
import { viewedKey } from "@/lib/storage/viewed";

vi.mock("@/components/features/review/FileDiff", () => ({
  FileDiff: () => <div data-testid="file-diff" />,
}));

let deleted: string[] = [];

function reviewAttrs() {
  return {
    status: "READY" as const,
    stage: null,
    provider: "gl",
    repository: "atlas/server",
    baseBranch: "main",
    baseSha: "6140736dbb1a0a0f1f0e2a1b3c4d5e6f70819a2b",
    headSha: null,
    baseDescription: "before the squash landed",
    changes: [421],
    included: [
      {
        number: 421,
        title: "ATLAS-7 add a thing",
        author: "jsmith",
        mergedAt: "2026-01-01T00:00:00Z",
        webUrl: "https://gitlab.test/atlas/server/-/merge_requests/421",
        strategy: "squash",
      },
    ],
    totals: { files: 2, additions: 10, deletions: 2 },
    error: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    expiresAt: "2999-01-01T00:00:00Z",
  };
}

function reviewFile(path: string) {
  return {
    type: "review-files" as const,
    id: path,
    attributes: { path, previousPath: "", status: "modified" as const, additions: 5, deletions: 1, binary: false },
  };
}

function seed() {
  deleted = [];
  server.use(
    http.get("/api/reviews/:id", () => HttpResponse.json(oneDoc("reviews", "rev-1", reviewAttrs()))),
    http.get("/api/reviews/:id/files", () =>
      HttpResponse.json(listDoc([reviewFile("src/a.ts"), reviewFile("src/b.ts")])),
    ),
    http.get("/api/reviews/:id/files/*", ({ request }) => {
      const path = new URL(request.url).pathname.split("/files/")[1] ?? "";
      return HttpResponse.json(
        oneDoc("review-file-diffs", path, {
          path: decodeURIComponent(path),
          previousPath: "",
          status: "modified",
          additions: 5,
          deletions: 1,
          binary: false,
          truncated: false,
          diff: "diff --git a/x b/x\n",
        }),
      );
    }),
    http.delete("/api/reviews/:id", ({ params }) => {
      deleted.push(String(params.id));
      return new HttpResponse(null, { status: 204 });
    }),
    http.get("/api/providers/gl/repositories/:repo", () =>
      HttpResponse.json(
        oneDoc("repositories", "atlas/server", {
          name: "server",
          namespace: "atlas",
          defaultBranch: "main",
          webUrl: "https://gitlab.test/atlas/server",
        }),
      ),
    ),
    http.get("/api/providers", () =>
      HttpResponse.json(
        listDoc([
          {
            type: "providers" as const,
            id: "gl",
            attributes: { kind: "gitlab", displayName: "GitLab", baseUrl: "https://gitlab.test" },
          },
        ]),
      ),
    ),
  );
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ThemeProvider>
        <MemoryRouter initialEntries={["/reviews/rev-1"]}>
          <AppShell>
            <Routes>
              <Route path="/reviews/:id" element={<ReviewPage />} />
              <Route path="/" element={<p>reviews page</p>} />
            </Routes>
          </AppShell>
        </MemoryRouter>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => localStorage.clear());

describe("ReviewPage (READY)", () => {
  it("publishes a breadcrumb with the repository and change numbers", async () => {
    seed();
    renderPage();
    await waitFor(() => {
      const nav = screen.getByRole("navigation", { name: /breadcrumb/i });
      expect(nav).toHaveTextContent("atlas/server");
      expect(nav).toHaveTextContent("#421");
    });
  });

  it("selects the first file and shows its diff", async () => {
    seed();
    renderPage();
    expect(await screen.findByTestId("file-diff")).toBeInTheDocument();
    expect(screen.getByText("File 1 of 2")).toBeInTheDocument();
  });

  it("moves between files with j and k", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.keyboard("j");
    expect(await screen.findByText("File 2 of 2")).toBeInTheDocument();
    await userEvent.keyboard("k");
    expect(await screen.findByText("File 1 of 2")).toBeInTheDocument();
  });

  it("wraps to the first file from the last", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.keyboard("j");
    await screen.findByRole("button", { name: /back to first file/i });
    await userEvent.keyboard("j");
    expect(await screen.findByText("File 1 of 2")).toBeInTheDocument();
  });

  it("toggles viewed with v and persists it", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.keyboard("v");
    await waitFor(() =>
      expect(JSON.parse(localStorage.getItem(viewedKey("rev-1")) ?? "[]")).toEqual(["src/a.ts"]),
    );
    expect(screen.getByText("1 / 2 viewed")).toBeInTheDocument();
  });

  it("links the file header to the single included change", async () => {
    seed();
    renderPage();
    expect(await screen.findByRole("link", { name: /open in gitlab/i })).toHaveAttribute(
      "href",
      "https://gitlab.test/atlas/server/-/merge_requests/421",
    );
  });

  it("finishes the review, clears viewed state, and returns to the root", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.keyboard("v");
    await userEvent.click(screen.getByRole("button", { name: /finish review/i }));
    await waitFor(() => expect(deleted).toEqual(["rev-1"]));
    expect(localStorage.getItem(viewedKey("rev-1"))).toBeNull();
    expect(await screen.findByText("reviews page")).toBeInTheDocument();
  });

  it("does not fire shortcuts while the tree filter has focus", async () => {
    seed();
    renderPage();
    await screen.findByTestId("file-diff");
    await userEvent.click(screen.getByPlaceholderText(/filter files/i));
    await userEvent.keyboard("j");
    expect(screen.getByText("File 1 of 2")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/frontend && npx vitest run src/pages/__tests__/ReviewPage.test.tsx`
Expected: FAIL — the page still renders `ReviewHeader` and the old two-column layout.

- [ ] **Step 3: Rewrite the `READY` branch of `ReviewPage`**

```tsx
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { ErrorBanner } from "@/components/common/ErrorBanner";
import { EmptyState } from "@/components/common/EmptyState";
import { Skeleton } from "@/components/ui/skeleton";
import { ReviewStatus } from "@/components/features/review/ReviewStatus";
import { ReviewErrorPanel } from "@/components/features/review/ReviewErrorPanel";
import { ReviewStatusLine } from "@/components/features/review/ReviewStatusLine";
import { ReviewWorkspace } from "@/components/features/review/ReviewWorkspace";
import { FileTree } from "@/components/features/review/FileTree";
import { DiffPane } from "@/components/features/review/DiffPane";
import {
  useFinishReview,
  useReview,
  useReviewFile,
  useReviewFiles,
} from "@/lib/hooks/api/useReviews";
import { useProviders } from "@/lib/hooks/api/useProviders";
import { useRepository } from "@/lib/hooks/api/useRepositories";
import { useBreadcrumbs } from "@/lib/breadcrumbs/useBreadcrumbs";
import { useHotkeys } from "@/lib/hotkeys/useHotkeys";
import { useStore } from "@/lib/storage/store";
import { clearViewed, toggleViewed, viewedStore } from "@/lib/storage/viewed";
import { buildTree, flattenVisible } from "@/lib/review/fileTree";
import { openInProviderHref } from "@/lib/review/providerLink";
import { messageFor } from "@/lib/api/errors";
import { strings } from "@/lib/strings";
import type { ReviewStatus as ReviewStatusValue } from "@/types/models/review";

/**
 * assertUnreachable makes the status switch exhaustive over ReviewStatus:
 * adding a status without handling it is a compile error, not a silent
 * fall-through into the diff layout.
 */
function assertUnreachable(status: never): never {
  throw new Error(`Unhandled review status: ${String(status)}`);
}

const MAX_BREADCRUMB_CHANGES = 5;

export function ReviewPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const review = useReview(id);
  const status = review.data?.attributes.status;
  const files = useReviewFiles(id, status === "READY");
  const [explicitPath, setExplicitPath] = useState<string | undefined>(undefined);
  const [fromKeyboard, setFromKeyboard] = useState(false);
  // Read-only binding: toggleViewed writes through the same store, so the
  // setter half of the tuple would be a second way to do one thing.
  const [viewedPaths] = useStore(viewedStore(id ?? ""));
  const providers = useProviders();
  const repositoryQuery = useRepository(
    review.data?.attributes.provider,
    review.data?.attributes.repository ?? "",
    Boolean(review.data),
  );
  // Finish Review and Discard are the same backend call (DELETE
  // /api/reviews/{id}); only the label and the confirmation differ.
  const finishOrDiscard = useFinishReview();

  const fileList = useMemo(() => files.data ?? [], [files.data]);
  const order = useMemo(() => flattenVisible(buildTree(fileList), new Set()), [fileList]);
  const selectedPath = explicitPath ?? order[0];
  const fileDiff = useReviewFile(id, selectedPath);
  const viewedSet = useMemo(() => new Set(viewedPaths), [viewedPaths]);

  const changeNumbers = review.data?.attributes.changes ?? [];
  useBreadcrumbs(
    useMemo(() => {
      const shown = changeNumbers.slice(0, MAX_BREADCRUMB_CHANGES).map((n) => `#${n}`).join(" · ");
      const label = changeNumbers.length > MAX_BREADCRUMB_CHANGES ? `${shown} · …` : shown;
      return [
        { label: strings.reviews, to: "/" },
        { label: review.data?.attributes.repository ?? "" },
        { label },
      ];
      // changeNumbers is a fresh array each render; key the memo on its text.
    }, [review.data?.attributes.repository, changeNumbers.join(",")]),
  );

  const index = selectedPath === undefined ? -1 : order.indexOf(selectedPath);
  const isLast = index >= 0 && index === order.length - 1;
  const nextPath = order.length === 0 ? undefined : order[(index + 1) % order.length];
  const previousPath =
    order.length === 0 ? undefined : order[(index - 1 + order.length) % order.length];

  function select(path: string | undefined, source: "pointer" | "keyboard"): void {
    if (path === undefined) return;
    setExplicitPath(path);
    setFromKeyboard(source === "keyboard");
  }

  function toggleSelectedViewed(): void {
    if (id === undefined || selectedPath === undefined) return;
    toggleViewed(id, selectedPath);
  }

  useHotkeys(
    {
      j: () => select(nextPath, "keyboard"),
      k: () => select(previousPath, "keyboard"),
      v: toggleSelectedViewed,
    },
    { enabled: status === "READY" },
  );

  async function closeReview(): Promise<void> {
    if (!id) return;
    try {
      await finishOrDiscard.mutateAsync(id);
      clearViewed(id);
      navigate("/");
    } catch (error: unknown) {
      toast.error(messageFor(error, "The review could not be closed."));
    }
  }

  if (review.isError) {
    return (
      <ErrorBanner
        title="Could not load this review"
        detail={messageFor(review.error, "It may have expired.")}
        onRetry={() => void review.refetch()}
      />
    );
  }
  if (!review.data) return <Skeleton className="h-8 w-1/3" />;

  const currentStatus: ReviewStatusValue = review.data.attributes.status;

  switch (currentStatus) {
    case "CREATING":
      return <ReviewStatus stage={review.data.attributes.stage} />;

    case "CONFLICTED":
    case "FAILED":
      return (
        <ReviewErrorPanel
          review={review.data}
          onDiscard={() => void closeReview()}
          discarding={finishOrDiscard.isPending}
        />
      );

    // FINISHED and EXPIRED both mean the workspace is already gone; neither
    // should render the diff layout or offer Finish Review.
    case "FINISHED":
    case "EXPIRED":
      return (
        <div className="flex flex-col items-center gap-4">
          <EmptyState
            title={strings.reviewUnavailableTitle}
            description={strings.reviewUnavailableDescription}
          />
          <Button onClick={() => navigate("/")}>{strings.startNewReview}</Button>
        </div>
      );

    case "READY": {
      const providerName =
        providers.data?.find((p) => p.id === review.data?.attributes.provider)?.attributes
          .displayName ?? review.data.attributes.provider;
      const href = openInProviderHref(
        review.data,
        repositoryQuery.data?.attributes.webUrl,
      );
      return (
        <div className="flex flex-col gap-4">
          <ReviewStatusLine
            review={review.data}
            files={fileList}
            viewed={viewedSet}
            onFinish={() => void closeReview()}
            onDiscard={() => void closeReview()}
            pending={finishOrDiscard.isPending}
          />
          {files.isError ? (
            <ErrorBanner
              title="Could not load the file list"
              detail={messageFor(files.error, "Try again in a moment.")}
              onRetry={() => void files.refetch()}
            />
          ) : !files.isLoading && fileList.length === 0 ? (
            <EmptyState
              title="No file changes"
              description="The selected PRs/MRs produce no net change."
            />
          ) : (
            <ReviewWorkspace>
              <FileTree
                files={fileList}
                viewed={viewedSet}
                selectedPath={selectedPath}
                onSelect={select}
                onToggleViewed={(path) => id && toggleViewed(id, path)}
                scrollSelectionIntoView={fromKeyboard}
              />
              <DiffPane
                fileDiff={fileDiff.data}
                loading={fileDiff.isLoading || files.isLoading}
                {...(fileDiff.isError ? { error: fileDiff.error } : {})}
                onRetry={() => void fileDiff.refetch()}
                href={href}
                providerName={providerName}
                viewed={selectedPath !== undefined && viewedSet.has(selectedPath)}
                onToggleViewed={toggleSelectedViewed}
                index={Math.max(index, 0)}
                total={order.length}
                nextName={nextPath}
                isLast={isLast}
                onNext={() => select(nextPath, "keyboard")}
              />
            </ReviewWorkspace>
          )}
        </div>
      );
    }

    default:
      return assertUnreachable(currentStatus);
  }
}
```

- [ ] **Step 4: Delete the replaced header**

```bash
cd apps/frontend && git rm src/components/features/review/ReviewHeader.tsx
```

If a test references `ReviewHeader`, delete that assertion in this commit.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd apps/frontend && npx vitest run src/pages src/components/features/review`
Expected: PASS.

- [ ] **Step 6: Lint and commit**

```bash
cd apps/frontend && npm run lint && npm run format:check
cd "$(git rev-parse --show-toplevel)"
git add -A apps/frontend/src
git commit -m "feat(frontend): rebuild the review page around the tree, diff pane, and shortcuts"
```

---

# Phase F — Close out

### Task 27: Full verification sweep

**Files:**
- Modify: whatever the sweep turns up (no new features)

**Interfaces:**
- Consumes: everything.
- Produces: a branch where every command in CLAUDE.md's "Build & Verification" section is green.

- [ ] **Step 1: Confirm there are no orphaned files or imports**

```bash
cd apps/frontend
grep -rn "SelectRepositoryPage\|ResumeReviewList\|ResumeReviewRow\|ManualRepositoryForm\|RepositoryList\|ProviderPicker\|ReviewHeader\|lib/schemas/repository" src || echo "no stale references"
```

Expected: `no stale references`. Anything that prints is a leftover import that
must be removed or repointed.

- [ ] **Step 2: Run the frontend suite end to end**

```bash
export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm use 22
cd apps/frontend
npm run lint
npm run format:check
npm test
npm run build
```

Expected: all green. `npm run build` writes into
`apps/backend/internal/ui/dist`, which the Go build embeds.

- [ ] **Step 3: Run the backend suite end to end**

```bash
cd apps/backend
go vet ./...
go tool golangci-lint run
CGO_ENABLED=0 go build ./...
go test -race -count=1 ./...
go test -race -count=1 -tags integration ./...
```

Expected: all green.

- [ ] **Step 4: Run what CI runs, from the repository root**

```bash
cd "$(git rev-parse --show-toplevel)"
make lint
make test
make test-integration
make build
make docker-build
```

Expected: all green. Report any failure with its actual output — do not claim a
command passed without having seen it pass.

- [ ] **Step 5: Walk the PRD acceptance criteria**

Open `docs/tasks/task-004-review-flow-redesign/prd.md` §10 and confirm each box
against a test or a running UI. For any box that cannot be checked, record the
gap in `docs/tasks/task-004-review-flow-redesign/audit.md` rather than quietly
marking it done.

- [ ] **Step 6: Commit any fixes the sweep required**

```bash
cd "$(git rev-parse --show-toplevel)"
git add -A
git commit -m "chore(task-004): verification sweep fixes"
```

- [ ] **Step 7: Run the code review before opening a PR**

Per CLAUDE.md, the review step is not optional:

```
/audit-plan task-004
```

or invoke `superpowers:requesting-code-review`, which dispatches
`plan-adherence-reviewer`, `backend-guidelines-reviewer`, and
`frontend-guidelines-reviewer` in parallel. Findings land in
`docs/tasks/task-004-review-flow-redesign/audit.md`.
