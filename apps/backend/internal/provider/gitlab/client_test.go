package gitlab

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

func newServer(t *testing.T, rejectMergedAtOrder bool) (*httptest.Server, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-test" {
			t.Errorf("missing PRIVATE-TOKEN on %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v4/") {
			t.Errorf("path lacks /api/v4: %s", r.URL.Path)
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/v4/projects":
			if q.Get("membership") != "true" || q.Get("order_by") != "path" || q.Get("simple") != "" {
				t.Errorf("bad projects query %s", r.URL.RawQuery)
			}
			if q.Get("page") == "1" {
				w.Header().Set("X-Next-Page", "2")
			} else {
				w.Header().Set("X-Next-Page", "")
			}
			_, _ = w.Write(fixture(t, "projects.json"))
		case "/api/v4/projects/atlas%2Fserver", "/api/v4/projects/atlas/server":
			if r.URL.EscapedPath() != "/api/v4/projects/atlas%2Fserver" {
				t.Errorf("project id not url-encoded: %s", r.URL.EscapedPath())
			}
			_, _ = w.Write(fixture(t, "project.json"))
		case "/api/v4/projects/atlas/missing":
			w.WriteHeader(404)
		case "/api/v4/projects/atlas/server/merge_requests":
			if q.Get("state") != "merged" || q.Get("target_branch") != "main" {
				t.Errorf("bad mr query %s", r.URL.RawQuery)
			}
			if rejectMergedAtOrder && q.Get("order_by") == "merged_at" {
				w.WriteHeader(400)
				return
			}
			w.Header().Set("X-Next-Page", "")
			_, _ = w.Write(fixture(t, "mrs_p1.json"))
		case "/api/v4/projects/atlas/server/merge_requests/421":
			_, _ = w.Write(fixture(t, "mr_421.json"))
		case "/api/v4/projects/atlas/server/merge_requests/421/commits":
			w.Header().Set("X-Next-Page", "")
			_, _ = w.Write(fixture(t, "mr_421_commits.json"))
		case "/api/v4/projects/atlas/server/merge_requests/401":
			w.WriteHeader(401)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &queries
}

func newClient(srv *httptest.Server) *Client {
	return New("gitlab-work", "GitLab Work", srv.URL, config.NewSecret("glpat-test"), srv.Client())
}

func TestListRepositoriesAndGet(t *testing.T) {
	srv, _ := newServer(t, false)
	c := newClient(srv)
	s, err := c.ListRepositories(context.Background(), "", provider.Page{Number: 1, Size: 2})
	if err != nil || len(s.Items) != 2 || !s.HasNext {
		t.Fatalf("%v %+v", err, s)
	}
	if s.Items[1].FullName() != "atlas/apps/web" || s.Items[1].Namespace() != "atlas/apps" || s.Items[1].Name() != "web" || s.Items[1].DefaultBranch() != "develop" {
		t.Errorf("mapping: %+v", s.Items[1])
	}
	s, _ = c.ListRepositories(context.Background(), "", provider.Page{Number: 2, Size: 2})
	if s.HasNext {
		t.Error("page 2 must be last")
	}
	r, err := c.GetRepository(context.Background(), "atlas/server")
	if err != nil || r.CloneURL() != "https://gitlab.company.com/atlas/server.git" {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := c.GetRepository(context.Background(), "atlas/missing"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestListMergedChangesAndSearch(t *testing.T) {
	srv, queries := newServer(t, false)
	c := newClient(srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	s, err := c.ListMergedChanges(context.Background(), repo, "main", "", provider.Page{})
	if err != nil || len(s.Items) != 2 || s.Items[0].Number() != 435 || s.HasNext {
		t.Fatalf("%v %+v", err, s)
	}
	it := s.Items[1]
	if it.SquashCommitSHA() != strings.Repeat("e", 40) || it.MergeCommitSHA() != "" || it.HeadSHA() != strings.Repeat("a", 40) || !it.Squashed() || it.Author() != "jsmith" || it.State() != provider.StateMerged {
		t.Errorf("mapping: %+v", it)
	}
	if !strings.Contains((*queries)[len(*queries)-1], "order_by=merged_at") {
		t.Errorf("order_by missing: %s", (*queries)[len(*queries)-1])
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "field", provider.Page{})
	if err != nil || len(s.Items) != 2 {
		t.Fatalf("title search: %v %d", err, len(s.Items))
	}
	last := (*queries)[len(*queries)-1]
	if !strings.Contains(last, "search=field") || !strings.Contains(last, "in=title") {
		t.Errorf("server-side search missing: %s", last)
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "jsmith", provider.Page{})
	if err != nil || len(s.Items) != 2 {
		t.Fatalf("author search: %v %d", err, len(s.Items))
	}
	joined := strings.Join(*queries, "\n")
	if !strings.Contains(joined, "author_username=jsmith") {
		t.Errorf("author_username query missing:\n%s", joined)
	}
	s, err = c.ListMergedChanges(context.Background(), repo, "main", "!421", provider.Page{})
	if err != nil || len(s.Items) != 1 || s.Items[0].Number() != 421 {
		t.Fatalf("numeric: %v %+v", err, s)
	}
}

func TestListMergedChangesFallsBackToCreatedAtOrder(t *testing.T) {
	srv, queries := newServer(t, true)
	c := newClient(srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	s, err := c.ListMergedChanges(context.Background(), repo, "main", "", provider.Page{})
	if err != nil || len(s.Items) != 2 || s.Items[0].Number() != 435 {
		t.Fatalf("%v %+v", err, s)
	}
	if !strings.Contains((*queries)[len(*queries)-1], "order_by=created_at") {
		t.Errorf("fallback not used: %s", (*queries)[len(*queries)-1])
	}
}

func TestGetChangeCommitsAndAuth(t *testing.T) {
	srv, _ := newServer(t, false)
	c := newClient(srv)
	repo, _ := c.GetRepository(context.Background(), "atlas/server")
	cr, err := c.GetChange(context.Background(), repo, 421)
	if err != nil || cr.Number() != 421 || cr.TargetBranch() != "main" {
		t.Fatalf("%v %+v", err, cr)
	}
	commits, err := c.GetChangeCommits(context.Background(), repo, 421)
	if err != nil || len(commits) != 2 || commits[0].SHA() != strings.Repeat("1", 40) || commits[1].SHA() != strings.Repeat("a", 40) {
		t.Fatalf("commits must be oldest-first: %v %+v", err, commits)
	}
	if _, err := c.GetChange(context.Background(), repo, 401); !errors.Is(err, provider.ErrAuth) || strings.Contains(err.Error(), "glpat") {
		t.Errorf("auth: %v", err)
	}
	spec := gitx.Spec{}
	if err := c.AuthorizeGit(repo, &spec); err != nil || len(spec.Env) != 3 || spec.Env[1] != "GIT_CONFIG_KEY_0=http.https://gitlab.company.com/.extraheader" {
		t.Errorf("authorize: %v %v", err, spec.Env)
	}
}

// TestGetChangeCommitsTooMany drives a server that always reports another page
// of commits available and asserts GetChangeCommits stops and returns
// provider.ErrTooManyCommits rather than paging forever.
func TestGetChangeCommitsTooMany(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-test" {
			w.WriteHeader(500)
			return
		}
		switch r.URL.Path {
		case "/api/v4/projects/atlas%2Fserver", "/api/v4/projects/atlas/server":
			_, _ = w.Write(fixture(t, "project.json"))
		case "/api/v4/projects/atlas/server/merge_requests/421/commits":
			// Always report another page so an unbounded loop would never terminate.
			w.Header().Set("X-Next-Page", "2")
			var buf strings.Builder
			buf.WriteString("[")
			for i := 0; i < 100; i++ {
				if i > 0 {
					buf.WriteString(",")
				}
				_, _ = fmt.Fprintf(&buf, `{"id":"%040x","parent_ids":[],"message":"c","authored_date":"2026-08-20T10:00:00Z"}`, i+1)
			}
			buf.WriteString("]")
			_, _ = w.Write([]byte(buf.String()))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv)
	repo, err := c.GetRepository(context.Background(), "atlas/server")
	if err != nil {
		t.Fatalf("get repository: %v", err)
	}
	_, err = c.GetChangeCommits(context.Background(), repo, 421)
	if !errors.Is(err, provider.ErrTooManyCommits) {
		t.Fatalf("expected ErrTooManyCommits, got %v", err)
	}
}

// mrJSONFixture builds a minimal valid merge_requests list-item JSON object.
func mrJSONFixture(iid int, mergedAt string) string {
	return fmt.Sprintf(`{"iid":%d,"title":"t%d","state":"merged","web_url":"https://gitlab.company.com/atlas/server/-/merge_requests/%d",
		"author":{"username":"smith"},"created_at":"2026-08-20T09:00:00Z","merged_at":"%s",
		"source_branch":"b%d","target_branch":"main","sha":"%040x",
		"merge_commit_sha":null,"squash_commit_sha":null,"squash":false}`, iid, iid, iid, mergedAt, iid, iid+1)
}

// TestListMergedChangesSearchTruncatesToPageSize covers finding 2: the
// dual-query (author + title) search merge is truncated back to page.Size,
// and HasNext is set true when truncation drops items.
func TestListMergedChangesSearchTruncatesToPageSize(t *testing.T) {
	byAuthor := "[" + strings.Join([]string{
		mrJSONFixture(1, "2026-08-21T10:00:00Z"),
		mrJSONFixture(2, "2026-08-22T10:00:00Z"),
		mrJSONFixture(3, "2026-08-23T10:00:00Z"),
	}, ",") + "]"
	byTitle := "[" + strings.Join([]string{
		mrJSONFixture(3, "2026-08-23T10:00:00Z"),
		mrJSONFixture(4, "2026-08-24T10:00:00Z"),
		mrJSONFixture(5, "2026-08-25T10:00:00Z"),
	}, ",") + "]"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-test" {
			w.WriteHeader(500)
			return
		}
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/v4/projects/atlas%2Fserver" || r.URL.Path == "/api/v4/projects/atlas/server":
			_, _ = w.Write(fixture(t, "project.json"))
		case r.URL.Path == "/api/v4/projects/atlas/server/merge_requests" && q.Get("in") == "title":
			w.Header().Set("X-Next-Page", "")
			_, _ = w.Write([]byte(byTitle))
		case r.URL.Path == "/api/v4/projects/atlas/server/merge_requests" && q.Get("author_username") != "":
			w.Header().Set("X-Next-Page", "")
			_, _ = w.Write([]byte(byAuthor))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv)
	repo, err := c.GetRepository(context.Background(), "atlas/server")
	if err != nil {
		t.Fatalf("get repository: %v", err)
	}
	// 5 unique merged items across the two queries; page.Size caps at 3.
	s, err := c.ListMergedChanges(context.Background(), repo, "main", "smith", provider.Page{Number: 1, Size: 3})
	if err != nil {
		t.Fatalf("list merged changes: %v", err)
	}
	if len(s.Items) != 3 {
		t.Fatalf("expected truncation to page size 3, got %d items: %+v", len(s.Items), s.Items)
	}
	if !s.HasNext {
		t.Error("expected HasNext true after truncation dropped items")
	}
	// Highest merged_at first: iid 5, 4, 3.
	if s.Items[0].Number() != 5 || s.Items[1].Number() != 4 || s.Items[2].Number() != 3 {
		t.Errorf("unexpected order after truncation: %d %d %d", s.Items[0].Number(), s.Items[1].Number(), s.Items[2].Number())
	}
}

// TestTrivialAccessors covers the interface accessor methods that carry no
// branching logic, and asserts Client.CloneURL never carries credentials.
func TestTrivialAccessors(t *testing.T) {
	srv, _ := newServer(t, false)
	c := newClient(srv)
	if c.ID() != "gitlab-work" {
		t.Errorf("ID() = %q", c.ID())
	}
	if c.Kind() != provider.KindGitLab {
		t.Errorf("Kind() = %v", c.Kind())
	}
	if c.DisplayName() != "GitLab Work" {
		t.Errorf("DisplayName() = %q", c.DisplayName())
	}
	if c.BaseURL() != srv.URL {
		t.Errorf("BaseURL() = %q, want %q", c.BaseURL(), srv.URL)
	}
	repo, err := c.GetRepository(context.Background(), "atlas/server")
	if err != nil {
		t.Fatalf("get repository: %v", err)
	}
	clone := c.CloneURL(repo)
	if clone != "https://gitlab.company.com/atlas/server.git" {
		t.Errorf("CloneURL() = %q", clone)
	}
	if strings.Contains(clone, "glpat-test") || strings.Contains(clone, "@") || strings.Contains(clone, "token") {
		t.Errorf("CloneURL() must never carry credentials: %q", clone)
	}
}

// TestMRJSONState covers all three branches of mrJSON.state(), none of which
// are exercised by the merged-state-only fixtures used elsewhere.
func TestMRJSONState(t *testing.T) {
	cases := []struct {
		raw  string
		want provider.ChangeState
	}{
		{"merged", provider.StateMerged},
		{"opened", provider.StateOpen},
		{"locked", provider.StateOpen},
		{"closed", provider.StateClosed},
		{"unknown-value", provider.StateClosed},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			m := mrJSON{State: tc.raw}
			if got := m.state(); got != tc.want {
				t.Errorf("state(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestListRepositoriesSearchQuery(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write(fixture(t, "projects_search.json"))
	}))
	defer srv.Close()
	c := New("gl", "GitLab", srv.URL, config.NewSecret("glpat"), srv.Client())

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
	c := New("gl", "GitLab", srv.URL, config.NewSecret("glpat"), srv.Client())
	if _, err := c.ListRepositories(context.Background(), "", provider.Page{Number: 1, Size: 30}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "search") {
		t.Errorf("query = %s, want no search parameter", got)
	}
}

func TestListBranchesUsesServerSearch(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("X-Next-Page", "")
		_, _ = w.Write(fixture(t, "branches.json"))
	}))
	defer srv.Close()
	c := New("gl", "GitLab", srv.URL, config.NewSecret("glpat"), srv.Client())
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
