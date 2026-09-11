package app_test

import (
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/api"
	"github.com/jtumidanski/converge/internal/app"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
	"github.com/jtumidanski/converge/internal/workspace"
)

// secretKey32 returns a valid CONVERGE_SECRET_KEY value: 32 random bytes,
// base64 standard encoding. Duplicated (deliberately -- see comment on the
// call site) from app's own (unexported, package app) fixture helper because
// this file is package app_test and cannot reach an unexported helper in
// package app.
func secretKey32(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := crand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// TestNoSecretReachesTheLog installs the redacting handler over a buffer,
// exercises register -> login -> provider create -> review create, and asserts
// the buffer contains no session token, password, provider token, or master
// key.
//
// This is the strongest available evidence for an NFR that is otherwise a
// claim: the redaction is a handler wrapper, so a single unwrapped logger
// anywhere would silently defeat it, and nothing else in the suite would fail.
//
// app.New has no writer seam -- it always builds its logger via
// NewLogger(os.Stderr, cfg) internally (see app.go) -- so the only way to
// observe the genuine, production-wired logger (the one every collaborator,
// including auth.Service and the request-logging middleware, actually
// receives) is to redirect os.Stderr for the duration of the exercise, the
// same technique app's own TestProviderStartupLogRedactsPassword and
// TestHostedWarnsAboutIgnoredProviderVariables use. This is a deliberate
// deviation from the brief's literal `app.NewLogger(buf, cfg)` construction:
// building a second, standalone logger would only prove that a hand-built
// wrapper redacts, not that the real wiring does.
//
// The review-create step deliberately does NOT go through app.New's hosted
// provider (application.Service / application.Resolver): that resolver is
// wired to a real DB-backed auth.ProviderResolver which only knows the
// github/gitlab kinds, so there is no seam to substitute
// internal/provider/fake's test double into it. Instead this test builds its
// own review.Service backed by a static registry holding a "fake" provider,
// exactly as internal/api's newHostedFixture (see isolation_test.go) does,
// and hands that service to api.NewRouter as Deps.Service/Deps.Providers.
// The register/login/settings-provider/password/logout steps still run
// through application.Auth and the real, production-wired logger, so the
// token-encryption path (kind "gitlab", against a local httptest.Server
// stand-in for the upstream host) is still genuinely exercised. This makes
// review create succeed (202), which is what lets the async build --
// including the git-credential-injection path via gitx.Runner -- actually
// run and be observed by the log assertions below.
func TestNoSecretReachesTheLog(t *testing.T) {
	root := t.TempDir()
	masterKey := secretKey32(t)
	password := "sentinel-password-DO-NOT-LOG"
	providerToken := "sentinel-provider-token-DO-NOT-LOG"

	// A local fake upstream for the gitlab-kind provider created below: fast,
	// deterministic, and never leaves the process. It always answers 404,
	// which is enough to exercise the provider-create/token-encryption code
	// path without needing a working git remote or live credential check
	// ("validate":false on the create request sidesteps live verification).
	fakeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fakeUpstream.Close()

	env := []string{
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=info",
		"CONVERGE_MODE=hosted",
		"CONVERGE_SECRET_KEY=" + masterKey,
		"CONVERGE_DATABASE_PATH=" + filepath.Join(root, "hosted.db"),
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		os.Stderr = origStderr
		_ = w.Close()
	}
	defer restore()
	captured := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		captured <- string(data)
	}()

	application, err := app.New(context.Background(), env)
	if err != nil {
		restore()
		t.Fatal(err)
	}
	defer func() { _ = application.Close() }()

	// A review.Service backed by a static registry holding a "fake"
	// provider, mirroring internal/api's newHostedFixture, so review create
	// below succeeds and the build/clone/log path actually runs. Built with
	// application.Log so its logging goes through the same real,
	// production-wired redacting logger under test.
	src := testutil.NewRepo(t)
	src.Branch("feat/a")
	c := src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	sq := src.Squash("feat/a", "squash a")
	src.Push()

	runner, err := gitx.NewExecRunner(application.Log, gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		restore()
		t.Fatal(err)
	}
	defer func() { _ = runner.Close() }()
	locks := &gitx.LockMap{}
	mirrors := mirror.New(t.TempDir(), runner, locks, application.Log)
	ws, err := workspace.New(t.TempDir(), runner, locks, application.Log)
	if err != nil {
		restore()
		t.Fatal(err)
	}
	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetName("server").
		SetNamespace("atlas").SetDefaultBranch("main").SetWebURL("https://example.test/atlas/server").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		restore()
		t.Fatal(err)
	}
	fp := fake.New("fake", provider.KindGitLab)
	fp.AddRepository(repo)
	commit, _ := provider.NewCommit(c, "a1", time.Now())
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(421).SetTitle("Add a").
		SetAuthor("jsmith").SetWebURL("https://example.test/mr/421").SetSourceBranch("feat/a").SetTargetBranch("main").
		SetState(provider.StateMerged).SetMergedAt(time.Now()).SetSquashCommitSHA(sq).SetHeadSHA(c).SetCommits([]provider.Commit{commit}).Build()
	if err != nil {
		restore()
		t.Fatal(err)
	}
	fp.AddChange(cr)
	registry := provider.NewRegistry()
	if err := registry.Register(fp); err != nil {
		restore()
		t.Fatal(err)
	}
	staticResolver := provider.NewStaticResolver(registry)
	cleaner := review.NewCleaner(mirrors, ws, application.Log)
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, application.Log, time.Now)
	svc := review.NewService(review.Deps{
		Providers: staticResolver, Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: review.NewCherryPickApplicator(runner, application.Log), Runner: runner, Log: application.Log,
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 2, Now: time.Now,
	})

	handler := api.NewRouter(api.Deps{
		// Service/Providers: the "fake"-backed service above, not
		// application.Service/application.Resolver (see doc comment).
		Service:   svc,
		Providers: staticResolver,
		Log:       application.Log,
		Mode:      application.Config.Mode,
		Auth:      application.Auth,
		// SecureCookies, TrustedProxy, AuthSweep, DBPing, Store,
		// CleanupInterval, Background, UI, UIPresent are intentionally left
		// at their zero values: this exercise never runs over TLS or behind
		// a proxy, never restarts (so no session sweep is needed within the
		// test), and never serves the embedded UI. BuildContext is set
		// explicitly below because it now gates a build this test actually
		// waits on.
		BuildContext:    context.Background(),
		LoginSessionTTL: application.Config.LoginSessionTTL,
	})

	do := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
		}
		req.Header.Set("Accept", "application/vnd.api+json")
		req.Header.Set("Content-Type", "application/vnd.api+json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	sessionCookie := func(rec *httptest.ResponseRecorder) *http.Cookie {
		for _, c := range rec.Result().Cookies() {
			if c.Name == "converge_session" {
				return c
			}
		}
		return nil
	}
	decodeID := func(rec *httptest.ResponseRecorder) string {
		t.Helper()
		var doc struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			restore()
			t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
		return doc.Data.ID
	}

	const username = "secrets-test-user"
	reg := do(http.MethodPost, "/api/auth/register",
		`{"data":{"type":"credentials","attributes":{"username":"`+username+`","password":`+strconv.Quote(password)+`}}}`)
	if reg.Code != http.StatusCreated {
		restore()
		t.Fatalf("register: status = %d, want 201; body=%s", reg.Code, reg.Body.String())
	}
	cookie := sessionCookie(reg)
	if cookie == nil {
		restore()
		t.Fatal("no session cookie from register")
	}

	login := do(http.MethodPost, "/api/auth/login",
		`{"data":{"type":"credentials","attributes":{"username":"`+username+`","password":`+strconv.Quote(password)+`}}}`)
	if login.Code != http.StatusOK {
		restore()
		t.Fatalf("login: status = %d, want 200; body=%s", login.Code, login.Body.String())
	}
	cookie = sessionCookie(login)
	if cookie == nil {
		restore()
		t.Fatal("no session cookie from login")
	}
	sessionToken := cookie.Value

	// Exercises the token-encryption path (application.Auth, real DB) with a
	// distinct provider kind and token from the review-create step below.
	create := do(http.MethodPost, "/api/settings/providers",
		`{"data":{"type":"userProviders","attributes":{"slug":"secrets-gitlab","displayName":"Secrets GitLab","kind":"gitlab","baseUrl":"`+fakeUpstream.URL+`","token":`+strconv.Quote(providerToken)+`,"validate":false}}}`,
		cookie)
	if create.Code != http.StatusCreated {
		restore()
		t.Fatalf("create provider: status = %d, want 201; body=%s", create.Code, create.Body.String())
	}

	// Review create against the "fake"-backed service constructed above, so
	// this actually reaches 202 and StartBuild's async clone/apply/log path
	// genuinely runs -- the property design SS10 singles out.
	created := do(http.MethodPost, "/api/reviews",
		`{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","baseBranch":"main","changes":[421]}}}`,
		cookie)
	if created.Code != http.StatusAccepted {
		restore()
		t.Fatalf("create review: status = %d, want 202; body=%s", created.Code, created.Body.String())
	}
	reviewID := decodeID(created)

	deadline := time.Now().Add(30 * time.Second)
	var finalStatus string
	for {
		poll := do(http.MethodGet, "/api/reviews/"+reviewID, "", cookie)
		if poll.Code != http.StatusOK {
			restore()
			t.Fatalf("poll review: status = %d, want 200; body=%s", poll.Code, poll.Body.String())
		}
		var doc struct {
			Data struct {
				Attributes struct {
					Status string `json:"status"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(poll.Body.Bytes(), &doc); err != nil {
			restore()
			t.Fatalf("decode poll body %s: %v", poll.Body.String(), err)
		}
		if doc.Data.Attributes.Status != "CREATING" {
			finalStatus = doc.Data.Attributes.Status
			break
		}
		if time.Now().After(deadline) {
			restore()
			t.Fatal("review did not leave CREATING in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The build must genuinely succeed, not merely leave CREATING: a review
	// that lands in FAILED or CONFLICTED would still (wrongly) look like the
	// async build/clone/log path ran, without the applicator step actually
	// completing.
	if finalStatus != "READY" {
		restore()
		t.Fatalf("review final status = %s, want READY", finalStatus)
	}

	list := do(http.MethodGet, "/api/reviews", "", cookie)
	if list.Code != http.StatusOK {
		restore()
		t.Fatalf("list reviews: status = %d, want 200; body=%s", list.Code, list.Body.String())
	}

	newPassword := "a much longer sentinel replacement phrase"
	change := do(http.MethodPost, "/api/auth/password",
		`{"data":{"type":"passwords","attributes":{"currentPassword":`+strconv.Quote(password)+`,"newPassword":`+strconv.Quote(newPassword)+`}}}`,
		cookie)
	if change.Code != http.StatusNoContent {
		restore()
		t.Fatalf("change password: status = %d, want 204; body=%s", change.Code, change.Body.String())
	}

	logout := do(http.MethodPost, "/api/auth/logout", "", cookie)
	if logout.Code != http.StatusNoContent {
		restore()
		t.Fatalf("logout: status = %d, want 204; body=%s", logout.Code, logout.Body.String())
	}

	restore()
	logged := <-captured

	for _, secret := range []string{password, providerToken, masterKey, sessionToken} {
		if strings.Contains(logged, secret) {
			t.Errorf("a secret reached the log") // deliberately does not echo it
		}
	}
	if logged == "" {
		t.Fatal("nothing was logged; the test is not exercising the logger")
	}
	if !strings.Contains(logged, "user_id=") {
		t.Errorf("log never carried user_id=; observability requirement not met")
	}
}
