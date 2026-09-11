package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type call struct{ path, query string }

func newServer(t *testing.T) (*httptest.Server, *[]call) {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.URL.Path, r.URL.RawQuery})
		if r.Header.Get("Authorization") != "Bearer ghp_test" || r.Header.Get("X-GitHub-Api-Version") != APIVersion || r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("missing headers on %s: %v", r.URL.Path, r.Header)
			w.WriteHeader(500)
			return
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/user/repos":
			if q.Get("page") == "1" {
				w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?page=2>; rel="next"`, "http://x"))
			}
			_, _ = w.Write(fixture(t, "repos.json"))
		case "/repos/atlas/server":
			_, _ = w.Write(fixture(t, "repo.json"))
		case "/repos/atlas/missing":
			w.WriteHeader(404)
		case "/repos/atlas/server/pulls":
			if q.Get("state") != "closed" || q.Get("base") != "main" || q.Get("per_page") != "100" {
				t.Errorf("bad list query %s", r.URL.RawQuery)
			}
			if q.Get("page") == "1" {
				w.Header().Set("Link", `<http://x/repos/atlas/server/pulls?page=2>; rel="next"`)
				_, _ = w.Write(fixture(t, "pulls_closed_p1.json"))
			} else {
				_, _ = w.Write(fixture(t, "pulls_closed_p2.json"))
			}
		case "/repos/atlas/server/pulls/421":
			_, _ = w.Write(fixture(t, "pull_421.json"))
		case "/repos/atlas/server/pulls/421/commits":
			_, _ = w.Write(fixture(t, "pull_421_commits.json"))
		case "/repos/atlas/server/pulls/999":
			w.WriteHeader(404)
		case "/repos/atlas/server/pulls/500":
			w.Header().Set("X-Ratelimit-Remaining", "0")
			w.Header().Set("X-Ratelimit-Reset", "1700000000")
			w.WriteHeader(403)
		case "/repos/atlas/server/pulls/401":
			w.WriteHeader(401)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return New("gh", "GitHub", srv.URL, config.NewSecret("ghp_test"), srv.Client(), func() time.Time { return now })
}

func TestListRepositoriesPagination(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv)
	s, err := c.ListRepositories(context.Background(), "", provider.Page{Number: 1, Size: 2})
	if err != nil || len(s.Items) != 2 || !s.HasNext {
		t.Fatalf("page1: %v %+v", err, s)
	}
	if s.Items[0].FullName() != "atlas/server" || s.Items[0].DefaultBranch() != "main" || s.Items[0].CloneURL() != "https://github.com/atlas/server.git" || s.Items[1].DefaultBranch() != "develop" {
		t.Errorf("mapping: %+v", s.Items)
	}
	s, err = c.ListRepositories(context.Background(), "", provider.Page{Number: 2, Size: 2})
	if err != nil || s.HasNext {
		t.Fatalf("page2: %v %+v", err, s)
	}
}

func TestGetRepositoryAndErrors(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv)
	r, err := c.GetRepository(context.Background(), "atlas/server")
	if err != nil || r.Namespace() != "atlas" || r.WebURL() != "https://github.com/atlas/server" {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := c.GetRepository(context.Background(), "atlas/missing"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	if _, err := c.GetRepository(context.Background(), "../etc"); err == nil {
		t.Error("invalid name accepted")
	}
	if _, err := c.GetChange(context.Background(), r, 500); !errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("rate limit: %v", err)
	}
	if _, err := c.GetChange(context.Background(), r, 401); !errors.Is(err, provider.ErrAuth) || strings.Contains(err.Error(), "ghp_test") {
		t.Errorf("auth: %v", err)
	}
}

func TestGetChangeAndCommits(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	cr, err := c.GetChange(context.Background(), repo, 421)
	if err != nil {
		t.Fatal(err)
	}
	if cr.State() != provider.StateMerged || cr.MergeCommitSHA() != strings.Repeat("e", 40) || cr.HeadSHA() != strings.Repeat("a", 40) || cr.CommitCount() != 2 || cr.Author() != "jsmith" || cr.SourceBranch() != "feat/field-state" || cr.TargetBranch() != "main" {
		t.Errorf("mapping: %+v", cr)
	}
	commits, err := c.GetChangeCommits(context.Background(), repo, 421)
	if err != nil || len(commits) != 2 || commits[0].SHA() != strings.Repeat("1", 40) || commits[1].Message() != "test: endpoint" {
		t.Errorf("commits: %v %+v", err, commits)
	}
	if _, err := c.GetChange(context.Background(), repo, 999); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("999: %v", err)
	}
}

func TestListMergedChangesScansFiltersSortsAndCaches(t *testing.T) {
	srv, calls := newServer(t)
	c := newClient(t, srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	s, err := c.ListMergedChanges(context.Background(), repo, "main", "", provider.Page{Number: 1, Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Items) != 2 || s.Items[0].Number() != 435 || s.Items[1].Number() != 427 || !s.HasNext {
		t.Fatalf("page1 = %v next=%v", numbers(s.Items), s.HasNext)
	}
	if s.Items[0].MergeCommitSHA() != "" {
		t.Error("list responses must leave MergeCommitSHA empty")
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "", provider.Page{Number: 2, Size: 2})
	if err != nil || len(s.Items) != 1 || s.Items[0].Number() != 421 || s.HasNext {
		t.Fatalf("page2 = %v next=%v err=%v", numbers(s.Items), s.HasNext, err)
	}
	listCalls := 0
	for _, cl := range *calls {
		if cl.path == "/repos/atlas/server/pulls" {
			listCalls++
		}
	}
	if listCalls != 2 {
		t.Errorf("provider pages fetched = %d, want 2 (cache must serve page 2)", listCalls)
	}
	// title / author substring search
	s, _ = c.ListMergedChanges(context.Background(), repo, "main", "MKAY", provider.Page{})
	if len(s.Items) != 1 || s.Items[0].Number() != 427 {
		t.Errorf("author search = %v", numbers(s.Items))
	}
	s, _ = c.ListMergedChanges(context.Background(), repo, "main", "field", provider.Page{})
	if len(s.Items) != 2 {
		t.Errorf("title search = %v", numbers(s.Items))
	}
	// numeric search hits GET /pulls/{n}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "#421", provider.Page{})
	if err != nil || len(s.Items) != 1 || s.Items[0].MergeCommitSHA() == "" {
		t.Errorf("numeric search: %v %v", err, numbers(s.Items))
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "develop", "#421", provider.Page{})
	if err != nil || len(s.Items) != 0 {
		t.Errorf("numeric search wrong base must be empty: %v %v", err, numbers(s.Items))
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "999", provider.Page{})
	if err != nil || len(s.Items) != 0 {
		t.Errorf("numeric search missing must be empty: %v", err)
	}
}

func TestAuthorizeGitUsesEnvOnly(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	spec := gitx.Spec{Args: []string{"fetch"}}
	if err := c.AuthorizeGit(repo, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Env) != 3 || spec.Env[1] != "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader" {
		t.Errorf("env = %v", spec.Env)
	}
	for _, a := range spec.Args {
		if strings.Contains(a, "ghp_test") {
			t.Error("token in argv")
		}
	}
	if c.CloneURL(repo) != "https://github.com/atlas/server.git" {
		t.Error("clone url")
	}
}

func numbers(items []provider.ChangeRequest) []int {
	out := make([]int, len(items))
	for i, it := range items {
		out[i] = it.Number()
	}
	return out
}

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
	c := New("gh", "GitHub", srv.URL, config.NewSecret("ghp_test"), srv.Client(), nil)

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
	c := New("gh", "GitHub", srv.URL, config.NewSecret("ghp_test"), srv.Client(), nil)
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
	c := New("gh", "GitHub", srv.URL, config.NewSecret("ghp_test"), srv.Client(), nil)
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
	c := New("gh", "GitHub", srv.URL, config.NewSecret("ghp_test"), srv.Client(), nil)
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
