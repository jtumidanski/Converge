package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/db"
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

// hostedFixture composes a real review.Service and a real auth.Service
// behind a hosted-mode NewRouter, so these tests exercise the genuine
// registration and middleware wiring rather than a scaffolding router.
type hostedFixture struct {
	handler http.Handler
	store   *session.Store
}

func newHostedFixture(t *testing.T) *hostedFixture {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Branch("feat/a")
	c := src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
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
		Providers: provider.NewStaticResolver(registry), Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: review.NewCherryPickApplicator(runner, log), Runner: runner, Log: log,
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 2, Now: time.Now,
	})

	authSvc := newTestAuthService(t)

	h := NewRouter(Deps{
		Service: svc, Providers: provider.NewStaticResolver(registry), Log: log,
		Mode: config.ModeHosted, Auth: authSvc, LoginSessionTTL: time.Hour,
	})
	return &hostedFixture{handler: h, store: store}
}

// createReviewAs POSTs /api/reviews on behalf of the given cookie and waits
// for the async build StartBuild kicks off to leave CREATING, since
// newHostedFixture (unlike newAPIFixture) never sets Deps.BuildContext:
// StartBuild's goroutine runs on context.Background(), so a test that
// returned before the build finished writing into its t.TempDir workspace
// would race that directory's removal.
func createReviewAs(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
	w := doAuth(t, h, http.MethodPost, "/api/reviews",
		`{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","baseBranch":"main","changes":[421]}}}`, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatalf("create review: status = %d, want 202; body=%s", w.Code, w.Body.String())
	}
	id := decodeOne(t, w)["id"].(string)
	deadline := time.Now().Add(30 * time.Second)
	for {
		gw := doAuth(t, h, http.MethodGet, "/api/reviews/"+id, "", cookie)
		if gw.Code != http.StatusOK {
			t.Fatalf("poll review %s: status = %d; body=%s", id, gw.Code, gw.Body.String())
		}
		status := decodeOne(t, gw)["attributes"].(map[string]any)["status"]
		if status != "CREATING" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("review %s did not leave CREATING in time", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return id
}

func TestReviewsAreScopedToTheirOwner(t *testing.T) {
	f := newHostedFixture(t)
	cookieA, _ := registerAndCookie(t, f.handler, "alice-reviews")
	cookieB, _ := registerAndCookie(t, f.handler, "bob-reviews")

	idA := createReviewAs(t, f.handler, cookieA)
	createReviewAs(t, f.handler, cookieB)

	w := doAuth(t, f.handler, http.MethodGet, "/api/reviews", "", cookieA)
	if w.Code != http.StatusOK {
		t.Fatalf("list as A: status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	rows := decodeList(t, w)
	if len(rows) != 1 || rows[0]["id"] != idA {
		t.Fatalf("A's list = %v, want exactly [%s]", rows, idA)
	}
}

func TestForeignReviewIDIs404(t *testing.T) {
	f := newHostedFixture(t)
	cookieA, _ := registerAndCookie(t, f.handler, "alice-foreign")
	cookieB, _ := registerAndCookie(t, f.handler, "bob-foreign")
	idB := createReviewAs(t, f.handler, cookieB)

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/reviews/" + idB},
		{http.MethodGet, "/api/reviews/" + idB + "/files"},
		{http.MethodGet, "/api/reviews/" + idB + "/files/a.txt"},
		{http.MethodGet, "/api/reviews/" + idB + "/diff"},
		{http.MethodDelete, "/api/reviews/" + idB},
	}
	for _, tc := range cases {
		w := doAuth(t, f.handler, tc.method, tc.path, "", cookieA)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s as A: status = %d, want 404; body=%s", tc.method, tc.path, w.Code, w.Body.String())
			continue
		}
		if tc.method != http.MethodDelete {
			assertErrorCode(t, w, "NOT_FOUND")
		}
	}
}

// TestUnownedReviewsAreInvisibleInHostedMode writes a session.json with no
// owner directly to disk (as a pre-hosted-mode review would have been left
// on disk, or a corrupt record with no readable owner) and confirms LoadAll
// picking it up does not make it visible to any user in hosted mode (FR-6.3).
func TestUnownedReviewsAreInvisibleInHostedMode(t *testing.T) {
	f := newHostedFixture(t)
	cookieA, _ := registerAndCookie(t, f.handler, "alice-unowned")

	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	sess, err := session.NewBuilder().SetID(id).SetProviderID("fake").SetRepository("atlas/server").
		SetBaseBranch("main").SetRequestedChanges([]int{421}).SetCreatedAt(time.Now()).SetTTL(24 * time.Hour).Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Save(sess); err != nil {
		t.Fatal(err)
	}
	if err := f.store.LoadAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	cookieB, _ := registerAndCookie(t, f.handler, "bob-unowned")
	for _, cookie := range []*http.Cookie{cookieA, cookieB} {
		w := doAuth(t, f.handler, http.MethodGet, "/api/reviews", "", cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("list: status = %d; body=%s", w.Code, w.Body.String())
		}
		for _, row := range decodeList(t, w) {
			if row["id"] == id {
				t.Fatalf("unowned session %s appeared in a user's list", id)
			}
		}
		w2 := doAuth(t, f.handler, http.MethodGet, "/api/reviews/"+id, "", cookie)
		if w2.Code != http.StatusNotFound {
			t.Errorf("get unowned session: status = %d, want 404; body=%s", w2.Code, w2.Body.String())
		}
	}
}

// TestCorruptSessionAnswers404InHostedMode proves the hosted branch of
// sessionFor's Corrupted check: a corrupt session.json has no readable
// owner, so reporting the pre-hosted 500 GIT_FAILURE for it in hosted mode
// would disclose that *some* review exists with that id (FR-4.2). Contrast
// with TestSessionForDistinguishesCorruptFromAbsent, which pins the
// standalone 500 behaviour unchanged.
func TestCorruptSessionAnswers404InHostedMode(t *testing.T) {
	f := newHostedFixture(t)
	cookie, _ := registerAndCookie(t, f.handler, "carol-corrupt")

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

	w := doAuth(t, f.handler, http.MethodGet, "/api/reviews/"+corruptID, "", cookie)
	if w.Code != http.StatusNotFound {
		t.Fatalf("corrupt session in hosted mode: status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w, "NOT_FOUND")
}

func TestUnauthenticatedRequestsToProtectedRoutesAre401(t *testing.T) {
	f := newHostedFixture(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/providers"},
		{http.MethodGet, "/api/providers/fake/repositories"},
		{http.MethodPost, "/api/reviews"},
		{http.MethodGet, "/api/reviews"},
		{http.MethodGet, "/api/reviews/aaaaaaaa"},
		{http.MethodDelete, "/api/reviews/aaaaaaaa"},
		{http.MethodGet, "/api/settings/providers"},
		{http.MethodGet, "/api/auth/me"},
	} {
		w := doAuth(t, f.handler, tc.method, tc.path, "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401; body=%s", tc.method, tc.path, w.Code, w.Body.String())
			continue
		}
		assertErrorCode(t, w, "UNAUTHENTICATED")
	}
}

// TestLogoutIsPublicUnderTheRealRouter exercises logout through
// NewRouter's actual registration (not auth_test.go's authTestRouter
// scaffolding, which never wraps its mux in authenticate at all). If
// authenticate is not exempting /api/auth/logout, a caller with no cookie
// gets rejected with 401 before the handler runs; logout's contract is an
// unconditional 204.
func TestLogoutIsPublicUnderTheRealRouter(t *testing.T) {
	f := newHostedFixture(t)
	w := doAuth(t, f.handler, http.MethodPost, "/api/auth/logout", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout with no cookie: status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	stale := &http.Cookie{Name: sessionCookieName, Value: "not-a-real-token"}
	w2 := doAuth(t, f.handler, http.MethodPost, "/api/auth/logout", "", stale)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("logout with stale cookie: status = %d, want 204; body=%s", w2.Code, w2.Body.String())
	}
}

func TestHealthzAndUIStayUnauthenticated(t *testing.T) {
	uiFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>ui</html>")}}
	for _, mode := range []config.Mode{config.ModeHosted, config.ModeStandalone} {
		log := testLogger()
		registry := provider.NewRegistry()
		var h http.Handler
		if mode == config.ModeHosted {
			h = newHostedFixture(t).handler
		} else {
			h = NewRouter(Deps{Mode: mode, Log: log, Providers: provider.NewStaticResolver(registry), UI: uiFS, UIPresent: true})
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s /healthz: status = %d, want 200; body=%s", mode, rec.Code, rec.Body.String())
		}
		if mode == config.ModeStandalone {
			uiRec := httptest.NewRecorder()
			h.ServeHTTP(uiRec, httptest.NewRequest(http.MethodGet, "/", nil))
			if uiRec.Code != http.StatusOK || uiRec.Body.String() != "<html>ui</html>" {
				t.Fatalf("%s GET /: status = %d body = %q, want the UI handler's index.html", mode, uiRec.Code, uiRec.Body.String())
			}
		}
	}
}

// TestCreateReviewWithAForeignOriginIs403 proves originGuard runs ahead of
// the review handlers for a state-changing route.
func TestCreateReviewWithAForeignOriginIs403(t *testing.T) {
	f := newHostedFixture(t)
	cookie, _ := registerAndCookie(t, f.handler, "eve-origin")
	r := httptest.NewRequest(http.MethodPost, "/api/reviews",
		strings.NewReader(`{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","baseBranch":"main","changes":[421]}}}`))
	r.Header.Set("Accept", jsonapi.MediaType)
	r.Header.Set("Content-Type", jsonapi.MediaType)
	r.Header.Set("Origin", "http://evil.test")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w, "FORBIDDEN")
}

func TestHealthzReportsDatabaseReachability(t *testing.T) {
	log := testLogger()
	registry := provider.NewRegistry()

	handle, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "converge.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	ping := func(ctx context.Context) error { return handle.PingContext(ctx) }

	hosted := NewRouter(Deps{Mode: config.ModeHosted, Log: log, Providers: provider.NewStaticResolver(registry), DBPing: ping})
	w := httptest.NewRecorder()
	hosted.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("hosted /healthz: status = %d; body=%s", w.Code, w.Body.String())
	}
	body := decodeHealthChecks(t, w)
	if body["database"] != "ok" {
		t.Fatalf("hosted checks = %v, want database=ok", body)
	}

	standalone := NewRouter(Deps{Mode: config.ModeStandalone, Log: log, Providers: provider.NewStaticResolver(registry)})
	w2 := httptest.NewRecorder()
	standalone.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("standalone /healthz: status = %d; body=%s", w2.Code, w2.Body.String())
	}
	body2 := decodeHealthChecks(t, w2)
	if _, ok := body2["database"]; ok {
		t.Fatalf("standalone checks = %v, want no database key", body2)
	}
}

func decodeHealthChecks(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var doc struct {
		Checks map[string]any `json:"checks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return doc.Checks
}

// TestStandaloneRouterIsUnchanged is the standalone-equivalence acceptance
// criterion: config.LoadConfig defaults CONVERGE_MODE to ModeStandalone when
// no new env vars are set, so this pins that Mode value explicitly (a bare
// Deps{} leaves Mode at Go's "" zero value, which is not the same thing and
// would mask a wiring gap in Task 20's config-to-Deps translation) alongside
// every pre-existing review endpoint and the acceptance criteria proper.
func TestStandaloneRouterIsUnchanged(t *testing.T) {
	f := newAPIFixture(t)
	w := do(t, f.handler, http.MethodGet, "/api/reviews", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/reviews needs no cookie: status = %d; body=%s", w.Code, w.Body.String())
	}

	registry := provider.NewRegistry()
	h := NewRouter(Deps{Mode: config.ModeStandalone, Log: testLogger(), Providers: provider.NewStaticResolver(registry)})
	modeW := do(t, h, http.MethodGet, "/api/auth/mode", "")
	if modeW.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/mode: status = %d; body=%s", modeW.Code, modeW.Body.String())
	}
	attrs := decodeOne(t, modeW)["attributes"].(map[string]any)
	if attrs["mode"] != "standalone" {
		t.Fatalf("mode = %v, want standalone", attrs["mode"])
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/auth/register"},
		{http.MethodPost, "/api/auth/login"},
		{http.MethodPost, "/api/auth/logout"},
		{http.MethodGet, "/api/auth/me"},
		{http.MethodGet, "/api/settings/providers"},
		{http.MethodPost, "/api/settings/providers"},
	} {
		w := do(t, h, tc.method, tc.path, "")
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404; body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}
