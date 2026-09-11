package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
	"github.com/jtumidanski/converge/internal/workspace"
)

var fortyHex = regexp.MustCompile(`^[0-9a-f]{40}$`)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

type apiFixture struct {
	handler http.Handler
	svc     *review.Service
	prov    *fake.Provider
	src     *testutil.Repo
	store   *session.Store
	// base is main's HEAD before the squash merge landed, i.e. the commit the
	// review's baseSha must resolve to. sq is the squash commit's SHA, i.e.
	// the change's landingSha.
	base string
	sq   string
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Branch("feat/a")
	c := src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	base := src.Head()
	sq := src.Squash("feat/a", "squash a")
	src.Push()

	log := testLogger()
	runner, err := gitx.NewExecRunner(log, gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	locks := &gitx.LockMap{}
	mirrors := mirror.New(t.TempDir(), runner, locks, log)
	ws, err := workspace.New(t.TempDir(), runner, locks, log)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetName("server").
		SetNamespace("atlas").SetDefaultBranch("main").SetWebURL("https://example.test/atlas/server").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)
	commit, _ := provider.NewCommit(c, "a1", time.Now())
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(421).SetTitle("Add a").
		SetAuthor("jsmith").SetWebURL("https://example.test/mr/421").SetSourceBranch("feat/a").SetTargetBranch("main").
		SetState(provider.StateMerged).SetMergedAt(time.Now()).SetSquashCommitSHA(sq).SetHeadSHA(c).SetCommits([]provider.Commit{commit}).Build()
	if err != nil {
		t.Fatal(err)
	}
	p.AddChange(cr)
	registry := provider.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	cleaner := review.NewCleaner(mirrors, ws, log)
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, log, time.Now)
	svc := review.NewService(review.Deps{
		Providers: registry, Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: review.NewCherryPickApplicator(runner, log), Runner: runner, Log: log,
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 2, Now: time.Now,
	})
	h := NewRouter(Deps{Service: svc, Providers: registry, Log: log})
	return &apiFixture{handler: h, svc: svc, prov: p, src: src, store: store, base: base, sq: sq}
}

func do(t *testing.T, h http.Handler, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var doc struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return doc.Data
}

// assertErrorCode decodes a JSON:API error document and fails the test
// unless it carries exactly one error with the given code.
func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	var doc struct {
		Errors []struct{ Code string } `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode error doc %s: %v", w.Body.String(), err)
	}
	if len(doc.Errors) != 1 || doc.Errors[0].Code != code {
		t.Errorf("errors = %s, want code %s", w.Body.String(), code)
	}
}

func decodeOne(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var doc struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return doc.Data
}

func TestHealthz(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, http.MethodGet, "/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["version"] == "" {
		t.Errorf("body = %v", body)
	}
}

func TestProvidersEndpoint(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, http.MethodGet, "/api/providers", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/vnd.api+json" {
		t.Fatalf("code=%d ct=%q", w.Code, w.Header().Get("Content-Type"))
	}
	data := decodeList(t, w)
	if len(data) != 1 || data[0]["id"] != "fake" || data[0]["type"] != "providers" {
		t.Fatalf("data = %v", data)
	}
	attrs := data[0]["attributes"].(map[string]any)
	if attrs["kind"] != "gitlab" || attrs["displayName"] == "" {
		t.Errorf("attributes = %v", attrs)
	}
	if strings.Contains(w.Body.String(), "token") || strings.Contains(w.Body.String(), "Token") {
		t.Error("token key present in provider response")
	}
}

func TestRepositoriesAndChanges(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories", "")
	if w.Code != http.StatusOK || len(decodeList(t, w)) != 1 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	// %2F-encoded repository id resolves to a single path segment
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/server"), "")
	if w.Code != http.StatusOK {
		t.Fatalf("get repo: %d %s", w.Code, w.Body.String())
	}
	one := decodeOne(t, w)
	repoAttrs := one["attributes"].(map[string]any)
	if one["id"] != "atlas/server" || repoAttrs["defaultBranch"] != "main" || repoAttrs["name"] != "server" ||
		repoAttrs["namespace"] != "atlas" || repoAttrs["webUrl"] != "https://example.test/atlas/server" {
		t.Errorf("repo = %v", one)
	}
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/missing"), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("missing repo: %d", w.Code)
	}
	w = do(t, f.handler, http.MethodGet, "/api/providers/nope/repositories", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown provider: %d", w.Code)
	}
	assertErrorCode(t, w, "NOT_FOUND")
	// changes
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/server")+"/changes?state=merged&target=main", "")
	if w.Code != http.StatusOK {
		t.Fatalf("changes: %d %s", w.Code, w.Body.String())
	}
	data := decodeList(t, w)
	if len(data) != 1 || data[0]["id"] != "421" {
		t.Fatalf("changes = %v", data)
	}
	attrs := data[0]["attributes"].(map[string]any)
	if attrs["number"] != float64(421) || attrs["title"] != "Add a" || attrs["author"] != "jsmith" ||
		attrs["sourceBranch"] != "feat/a" || attrs["targetBranch"] != "main" ||
		attrs["webUrl"] != "https://example.test/mr/421" || attrs["landingSha"] != f.sq {
		t.Errorf("attributes = %v (want landingSha=%s)", attrs, f.sq)
	}
	if v, ok := attrs["mergedAt"]; !ok || v == nil || v == "" {
		t.Errorf("mergedAt missing or empty: %v", attrs)
	}
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/server")+"/changes?state=open", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("state=open: %d", w.Code)
	}
	assertErrorCode(t, w, "INVALID_STATE")
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories/"+url.PathEscape("atlas/server")+"/changes?target=main&pageSize=500&page=2", "")
	if w.Code != http.StatusOK {
		t.Errorf("pageSize clamp: %d %s", w.Code, w.Body.String())
	}
	var pageDoc struct {
		Meta struct {
			Page struct {
				Number int `json:"number"`
				Size   int `json:"size"`
			} `json:"page"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pageDoc); err != nil {
		t.Fatal(err)
	}
	if pageDoc.Meta.Page.Size != 100 || pageDoc.Meta.Page.Number != 2 {
		t.Errorf("meta.page = %+v, want size=100 (clamped from 500) number=2", pageDoc.Meta.Page)
	}
}

func TestReviewLifecycleEndpoints(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, http.MethodPost, "/api/reviews", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","baseBranch":"main","changes":[421]}}}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("post: %d %s", w.Code, w.Body.String())
	}
	created := decodeOne(t, w)
	id := created["id"].(string)
	if created["attributes"].(map[string]any)["status"] != "CREATING" {
		t.Errorf("attributes = %v", created["attributes"])
	}
	// wait for the async build
	deadline := time.Now().Add(60 * time.Second)
	var final map[string]any
	for {
		w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id, "")
		if w.Code != http.StatusOK {
			t.Fatalf("get: %d %s", w.Code, w.Body.String())
		}
		final = decodeOne(t, w)
		status := final["attributes"].(map[string]any)["status"]
		if status != "CREATING" {
			if status != "READY" {
				t.Fatalf("status = %v attrs=%v", status, final["attributes"])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("build did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	attrs := final["attributes"].(map[string]any)
	if attrs["baseSha"] != f.base {
		t.Errorf("baseSha = %v, want %s", attrs["baseSha"], f.base)
	}
	if headSHA, ok := attrs["headSha"].(string); !ok || !fortyHex.MatchString(headSHA) {
		t.Errorf("headSha = %v, want a 40-hex sha", attrs["headSha"])
	}
	if attrs["baseDescription"] != "Immediately before #421" {
		t.Errorf("baseDescription = %v", attrs["baseDescription"])
	}
	totals, ok := attrs["totals"].(map[string]any)
	if !ok || totals["files"] != float64(1) || totals["additions"] != float64(1) || totals["deletions"] != float64(0) {
		t.Errorf("totals = %v", attrs["totals"])
	}
	included, ok := attrs["included"].([]any)
	if !ok || len(included) != 1 {
		t.Fatalf("included = %v", attrs["included"])
	}
	inc := included[0].(map[string]any)
	if inc["number"] != float64(421) || inc["title"] != "Add a" || inc["author"] != "jsmith" ||
		inc["strategy"] != "squash" || inc["webUrl"] != "https://example.test/mr/421" {
		t.Errorf("included[0] = %v", inc)
	}
	rel, ok := final["relationships"].(map[string]any)
	if !ok {
		t.Fatalf("relationships = %v", final["relationships"])
	}
	filesRel, ok := rel["files"].(map[string]any)
	if !ok {
		t.Fatalf("relationships.files = %v", rel["files"])
	}
	links, ok := filesRel["links"].(map[string]any)
	if !ok || links["related"] != "/api/reviews/"+id+"/files" {
		t.Errorf("relationships.files.links = %v", filesRel["links"])
	}
	// list
	w = do(t, f.handler, http.MethodGet, "/api/reviews", "")
	if w.Code != http.StatusOK || len(decodeList(t, w)) != 1 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	// files
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files", "")
	if w.Code != http.StatusOK {
		t.Fatalf("files: %d %s", w.Code, w.Body.String())
	}
	files := decodeList(t, w)
	if len(files) != 1 || files[0]["type"] != "review-files" || files[0]["id"] != "a.txt" {
		t.Fatalf("files = %v", files)
	}
	fileAttrs := files[0]["attributes"].(map[string]any)
	if fileAttrs["status"] != "added" || fileAttrs["additions"] != float64(1) || fileAttrs["deletions"] != float64(0) || fileAttrs["binary"] != false {
		t.Errorf("file attributes = %v", fileAttrs)
	}
	// file diff by path segment and by query
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files/a.txt", "")
	if w.Code != http.StatusOK {
		t.Fatalf("file diff: %d %s", w.Code, w.Body.String())
	}
	one := decodeOne(t, w)
	if one["type"] != "review-file-diffs" || !strings.Contains(one["attributes"].(map[string]any)["diff"].(string), "+a") {
		t.Errorf("diff = %v", one)
	}
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files?path=a.txt", "")
	if w.Code != http.StatusOK {
		t.Errorf("query path form: %d %s", w.Code, w.Body.String())
	}
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files/nope.txt", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown file: %d", w.Code)
	}
	// raw combined diff
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/diff", "")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") || !strings.Contains(w.Body.String(), "a.txt") {
		t.Errorf("raw diff: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	// delete is idempotent and always 204
	for i := 0; i < 2; i++ {
		w = do(t, f.handler, http.MethodDelete, "/api/reviews/"+id, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	w = do(t, f.handler, http.MethodDelete, "/api/reviews/ffffffff", "")
	if w.Code != http.StatusNoContent {
		t.Errorf("delete unknown: %d", w.Code)
	}
	w = do(t, f.handler, http.MethodGet, "/api/reviews/"+id+"/files", "")
	if w.Code != http.StatusConflict {
		t.Errorf("files after finish: %d", w.Code)
	}
	var errDoc struct {
		Errors []struct{ Code string } `json:"errors"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &errDoc)
	if len(errDoc.Errors) != 1 || errDoc.Errors[0].Code != "REVIEW_NOT_READY" {
		t.Errorf("error doc = %s", w.Body.String())
	}
}

func TestReviewValidationErrors(t *testing.T) {
	f := newAPIFixture(t)
	cases := []struct {
		name string
		body string
		code string
	}{
		{"no provider", `{"data":{"type":"reviews","attributes":{"repository":"atlas/server","changes":[1]}}}`, "INVALID_PROVIDER"},
		{"bad repo", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"../x","changes":[1]}}}`, "INVALID_REPOSITORY"},
		{"bad branch", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","baseBranch":"-x","changes":[1]}}}`, "INVALID_BRANCH"},
		{"no changes", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","changes":[]}}}`, "INVALID_CHANGES"},
		{"duplicate changes", `{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","changes":[1,1]}}}`, "INVALID_CHANGES"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, f.handler, http.MethodPost, "/api/reviews", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
			}
			var doc struct {
				Errors []struct{ Code string } `json:"errors"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &doc)
			if len(doc.Errors) != 1 || doc.Errors[0].Code != tc.code {
				t.Errorf("errors = %s want %s", w.Body.String(), tc.code)
			}
		})
	}
	w := do(t, f.handler, http.MethodPost, "/api/reviews", `{"data":{"type":"widgets","attributes":{}}}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("wrong type: %d", w.Code)
	}
	assertErrorCode(t, w, "INVALID_REQUEST")
	w = do(t, f.handler, http.MethodGet, "/api/reviews/not-an-id", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("bad id: %d", w.Code)
	}
	w = do(t, f.handler, http.MethodPut, "/api/reviews", "")
	if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
		t.Errorf("PUT: %d", w.Code)
	}
}

func TestProviderErrorsMapToStatuses(t *testing.T) {
	f := newAPIFixture(t)
	f.prov.FailWith(provider.ErrAuth)
	w := do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories", "")
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "PROVIDER_AUTH") {
		t.Errorf("auth: %d %s", w.Code, w.Body.String())
	}
	f.prov.FailWith(provider.ErrUnavailable)
	w = do(t, f.handler, http.MethodGet, "/api/providers/fake/repositories", "")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "PROVIDER_UNAVAILABLE") {
		t.Errorf("unavailable: %d %s", w.Code, w.Body.String())
	}
	_ = context.Background()
}

// TestNewRouterStartsAndStopsSweeper proves NewRouter, given a Store and a
// positive CleanupInterval, actually runs Store.RunSweeper against
// Deps.BuildContext: nothing else in this process ever calls RunSweeper (the
// one-shot CLI never needs to), so this is the only path that expires a
// session past its TTL without a test calling Store.Sweep directly. It also
// proves cancelling BuildContext stops the goroutine rather than leaking it.
func TestNewRouterStartsAndStopsSweeper(t *testing.T) {
	log := testLogger()
	registry := provider.NewRegistry()
	runner, err := gitx.NewExecRunner(log, gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	locks := &gitx.LockMap{}
	mirrors := mirror.New(t.TempDir(), runner, locks, log)
	ws, err := workspace.New(t.TempDir(), runner, locks, log)
	if err != nil {
		t.Fatal(err)
	}
	cleaner := review.NewCleaner(mirrors, ws, log)
	store := session.NewStore(ws.Root(), time.Hour, cleaner, log, time.Now)
	svc := review.NewService(review.Deps{Providers: registry, Mirrors: mirrors, Workspaces: ws, Store: store, Log: log, Now: time.Now})

	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	sess, err := session.NewBuilder().SetID(id).SetProviderID("fake").SetRepository("atlas/server").
		SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(time.Now().Add(-time.Hour)).SetTTL(time.Millisecond).Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}

	buildCtx, cancel := context.WithCancel(context.Background())
	_ = NewRouter(Deps{
		Service: svc, Providers: registry, Log: log,
		BuildContext: buildCtx, Store: store, CleanupInterval: 10 * time.Millisecond,
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, ok := store.Get(id); ok && got.Status() == session.StatusExpired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sweeper never expired the session; NewRouter did not start it")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Cancelling BuildContext must stop the sweeper: save a second expired
	// session afterward and confirm it is never swept.
	cancel()
	time.Sleep(50 * time.Millisecond) // let RunSweeper observe ctx.Done and return
	id2, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	sess2, err := session.NewBuilder().SetID(id2).SetProviderID("fake").SetRepository("atlas/server").
		SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(time.Now().Add(-time.Hour)).SetTTL(time.Millisecond).Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(sess2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if got, ok := store.Get(id2); !ok || got.Status() != session.StatusCreating {
		t.Errorf("session swept after BuildContext was cancelled: %+v", got)
	}
}

// TestUIRouteServesRealIndexHTMLAndRejectsNonGET composes NewRouter with a
// non-nil Deps.UI containing a real index.html -- the branch no test
// anywhere else in this suite reaches (grep 'UI:\|UIPresent' across
// *_test.go finds nothing else). This is the same hole that let the
// "GET /" vs "/api/" registration panic ship through Task 19's review: the
// composed "router serves the UI" path was never executed. It also pins
// R37: with the UI mounted at "/", every method other than GET/HEAD must
// get the same 404 NOT_FOUND JSON:API response "/api/" already returns for
// unknown endpoints, not the SPA's index.html and not a silent 405.
func TestUIRouteServesRealIndexHTMLAndRejectsNonGET(t *testing.T) {
	log := testLogger()
	registry := provider.NewRegistry()
	uiFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>real ui</html>")},
	}

	handler := NewRouter(Deps{Providers: registry, Log: log, UI: uiFS, UIPresent: true})

	w := do(t, handler, http.MethodGet, "/", "")
	if w.Code != http.StatusOK || w.Body.String() != "<html>real ui</html>" {
		t.Fatalf("GET / = %d %q, want 200 index.html", w.Code, w.Body.String())
	}

	w = do(t, handler, http.MethodGet, "/nonexistent", "")
	if w.Code != http.StatusOK || w.Body.String() != "<html>real ui</html>" {
		t.Fatalf("GET /nonexistent = %d %q, want 200 index.html (SPA fallback)", w.Code, w.Body.String())
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/nonexistent"},
		{http.MethodPut, "/nonexistent"},
		{http.MethodDelete, "/nonexistent"},
		{http.MethodPost, "/"},
		{http.MethodPost, "/healthz"},
	} {
		w := do(t, handler, tc.method, tc.path, "")
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d %q, want 404 NOT_FOUND", tc.method, tc.path, w.Code, w.Body.String())
			continue
		}
		assertErrorCode(t, w, "NOT_FOUND")
	}

	// /api/ must be unaffected by the UI mount: unknown API endpoints still
	// get the JSON:API NOT_FOUND contract regardless of method.
	w = do(t, handler, http.MethodPost, "/api/bogus", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("POST /api/bogus = %d, want 404", w.Code)
	}
	assertErrorCode(t, w, "NOT_FOUND")
}

// TestChangeResourceLandingSHAPrefersMergeOverSquash pins changeResource's
// precedence: when both a merge commit SHA and a squash commit SHA are
// present, landingSha must be the merge commit, not the squash commit. A
// change resolved with only one of the two (as in the fixture used by
// TestRepositoriesAndChanges) cannot distinguish which order the candidates
// are tried in, so this needs its own case with both set.
func TestChangeResourceLandingSHAPrefersMergeOverSquash(t *testing.T) {
	mergeSHA := strings.Repeat("a", 40)
	squashSHA := strings.Repeat("b", 40)
	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").
		SetName("server").SetNamespace("atlas").SetDefaultBranch("main").SetWebURL("https://example.test/atlas/server").
		SetCloneURL("file:///dev/null").Build()
	if err != nil {
		t.Fatal(err)
	}
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(1).
		SetTitle("t").SetTargetBranch("main").SetMergeCommitSHA(mergeSHA).SetSquashCommitSHA(squashSHA).Build()
	if err != nil {
		t.Fatal(err)
	}
	res := changeResource(cr)
	attrs, ok := res.Attributes.(changeAttributes)
	if !ok {
		t.Fatalf("attributes = %#v", res.Attributes)
	}
	if attrs.LandingSHA == nil || *attrs.LandingSHA != mergeSHA {
		t.Errorf("landingSha = %v, want merge SHA %s (not squash SHA %s)", attrs.LandingSHA, mergeSHA, squashSHA)
	}
}

// TestAcceptNegotiation pins middleware.go's content negotiation: an
// acceptable Accept header reaches the handler, an unacceptable one is
// rejected before the handler ever runs, with the exact 406 code.
func TestAcceptNegotiation(t *testing.T) {
	f := newAPIFixture(t)

	r := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	r.Header.Set("Accept", jsonapi.MediaType)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("acceptable Accept header: code = %d body = %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	r.Header.Set("Accept", "text/html")
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusNotAcceptable {
		t.Fatalf("unacceptable Accept header: code = %d body = %s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w, "NOT_ACCEPTABLE")
}

// TestValidateRepoFullNameAtHTTPLayer pins repoNameFrom's use of
// gitx.ValidateRepoFullName: every rejection case from the spec (no `..`
// segment, no leading /, - or ., and the owner/name shape) must reach the
// HTTP layer as 400 INVALID_REPOSITORY.
func TestValidateRepoFullNameAtHTTPLayer(t *testing.T) {
	f := newAPIFixture(t)
	cases := []struct {
		name string
		repo string
	}{
		{"no slash at all", "atlasserver"},
		{"leading slash", "/atlas/server"},
		{"leading dash", "-atlas/server"},
		{"leading dot", ".atlas/server"},
		{"dot-dot segment", "atlas/../server"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "/api/providers/fake/repositories/" + url.PathEscape(tc.repo)
			w := do(t, f.handler, http.MethodGet, target, "")
			if w.Code != http.StatusBadRequest {
				t.Fatalf("repo %q: code = %d body = %s", tc.repo, w.Code, w.Body.String())
			}
			assertErrorCode(t, w, "INVALID_REPOSITORY")
		})
	}
}

// TestSessionForDistinguishesCorruptFromAbsent proves I3's wiring of
// session.Store.Corrupted: a review whose session.json is unreadable answers
// 500 GIT_FAILURE (not the innocuous-looking 404 it produced before this
// fix), while an id that genuinely never existed still answers 404.
func TestSessionForDistinguishesCorruptFromAbsent(t *testing.T) {
	f := newAPIFixture(t)

	corruptID := "eeeeeeee"
	dir := filepath.Join(f.store.Root(), corruptID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.store.LoadAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	w := do(t, f.handler, http.MethodGet, "/api/reviews/"+corruptID, "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt session: code = %d body = %s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w, "GIT_FAILURE")

	// A genuinely absent id (well-formed, never seen) must still be 404.
	w = do(t, f.handler, http.MethodGet, "/api/reviews/ffffffff", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("absent session: code = %d body = %s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w, "NOT_FOUND")
}

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
