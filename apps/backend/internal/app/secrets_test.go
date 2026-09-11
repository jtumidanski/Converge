package app_test

import (
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jtumidanski/converge/internal/api"
	"github.com/jtumidanski/converge/internal/app"
)

// secretKey32 returns a valid CONVERGE_SECRET_KEY value: 32 random bytes,
// base64 standard encoding. Duplicated from app's own (unexported, package
// app) fixture helper because this file is deliberately package app_test.
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
func TestNoSecretReachesTheLog(t *testing.T) {
	root := t.TempDir()
	masterKey := secretKey32(t)
	password := "sentinel-password-DO-NOT-LOG"
	providerToken := "sentinel-provider-token-DO-NOT-LOG"

	// A local fake upstream: fast, deterministic, and never leaves the
	// process, so the review-create step below cannot hang or flake on a
	// real network host. It always answers 404, which is enough to exercise
	// the provider-fetch code path without needing a working git remote.
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

	handler := api.NewRouter(api.Deps{
		Service:         application.Service,
		Providers:       application.Resolver,
		Log:             application.Log,
		Mode:            application.Config.Mode,
		Auth:            application.Auth,
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

	create := do(http.MethodPost, "/api/settings/providers",
		`{"data":{"type":"userProviders","attributes":{"slug":"secrets-gitlab","displayName":"Secrets GitLab","kind":"gitlab","baseUrl":"`+fakeUpstream.URL+`","token":`+strconv.Quote(providerToken)+`,"validate":false}}}`,
		cookie)
	if create.Code != http.StatusCreated {
		restore()
		t.Fatalf("create provider: status = %d, want 201; body=%s", create.Code, create.Body.String())
	}

	// The review-create step is exercised against the just-created provider.
	// The brief asks for this "against a fake provider" -- app.New's hosted
	// resolver always builds a real provider.GitProvider client from the
	// user's stored configuration (internal/provider/fake has no seam into
	// it), so there is no way to substitute the fake.Provider test double
	// used elsewhere in this suite. A local httptest.Server standing in for
	// the upstream host is the closest available equivalent: fast,
	// deterministic, and it still drives the genuine provider-fetch code
	// path. The 404 upstream means this call is expected to fail; that is
	// fine -- what matters is that it logs, and logs safely.
	_ = do(http.MethodPost, "/api/reviews",
		`{"data":{"type":"reviews","attributes":{"provider":"secrets-gitlab","repository":"atlas/server","baseBranch":"main","changes":[1]}}}`,
		cookie)

	_ = do(http.MethodGet, "/api/reviews", "", cookie)

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
