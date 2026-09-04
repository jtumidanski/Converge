package gitlab

import (
	"context"
	"errors"
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
	s, err := c.ListRepositories(context.Background(), provider.Page{Number: 1, Size: 2})
	if err != nil || len(s.Items) != 2 || !s.HasNext {
		t.Fatalf("%v %+v", err, s)
	}
	if s.Items[1].FullName() != "atlas/apps/web" || s.Items[1].Namespace() != "atlas/apps" || s.Items[1].Name() != "web" || s.Items[1].DefaultBranch() != "develop" {
		t.Errorf("mapping: %+v", s.Items[1])
	}
	s, _ = c.ListRepositories(context.Background(), provider.Page{Number: 2, Size: 2})
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
