package api

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/db"
	"github.com/jtumidanski/converge/internal/identity"
)

// authTestKey32 is 32 bytes of 0x01, base64 standard encoding. Test fixture
// only, mirroring internal/auth's own key32 fixture constant.
const authTestKey32 = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

// stubVerifier always succeeds: this test file does not exercise provider
// verification, only that the middleware wires Authenticate through.
type stubVerifier struct{}

func (stubVerifier) Verify(context.Context, config.Kind, string, config.Secret) error { return nil }

// stubPurger and stubUsage satisfy auth.ServiceDeps without exercising the
// account-deletion cascade, which this test file does not touch.
type stubPurger struct{}

func (stubPurger) PurgeUser(context.Context, string) error { return nil }

type stubUsage struct{}

func (stubUsage) ProviderInUse(identity.Scope, string) bool { return false }

// newTestAuthService returns a real, migrated auth.Service, so the
// authenticate middleware tests exercise the genuine cookie-to-scope path
// rather than a mock of it.
func newTestAuthService(t *testing.T) *auth.Service {
	t.Helper()
	handle, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "converge.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	store := auth.NewStore(handle)
	sealer, err := auth.NewSealer(config.NewSecret(authTestKey32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	now := time.Now
	throttle := auth.NewThrottle(store, now)
	resolver := auth.NewProviderResolver(store, sealer, http.DefaultClient, now)
	return auth.NewService(auth.ServiceDeps{
		Store:      store,
		Sealer:     sealer,
		Throttle:   throttle,
		Resolver:   resolver,
		Verifier:   stubVerifier{},
		Purger:     stubPurger{},
		Usage:      stubUsage{},
		Log:        testLogger(),
		Now:        now,
		SessionTTL: 24 * time.Hour,
		IdleTTL:    2 * time.Hour,
	})
}

// recordingHandler reports whether it was invoked, so tests can assert a
// rejected request never reaches a handler.
type recordingHandler struct {
	invoked bool
	fn      func(w http.ResponseWriter, r *http.Request)
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.invoked = true
	if h.fn != nil {
		h.fn(w, r)
	}
}

func TestScopeFromDefaultsToStandalone(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/reviews", nil)
	if got := scopeFrom(r); got != identity.Standalone() {
		t.Fatalf("scopeFrom(bare request) = %+v, want identity.Standalone()", got)
	}
}

func TestOriginGuardAllowsSafeMethods(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger()}}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := &recordingHandler{}
		r := httptest.NewRequest(method, "/api/reviews", nil)
		r.Header.Set("Origin", "http://evil.test")
		r.Host = "example.test"
		w := httptest.NewRecorder()
		s.originGuard(rec).ServeHTTP(w, r)
		if !rec.invoked {
			t.Fatalf("%s: handler was not invoked", method)
		}
	}
}

func TestOriginGuardAllowsMatchingOriginAndSameSiteFetch(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger()}}

	rec := &recordingHandler{}
	r := httptest.NewRequest(http.MethodPost, "/api/reviews", nil)
	r.Header.Set("Origin", "http://example.test")
	r.Host = "example.test"
	w := httptest.NewRecorder()
	s.originGuard(rec).ServeHTTP(w, r)
	if !rec.invoked {
		t.Fatal("matching origin: handler was not invoked")
	}

	rec2 := &recordingHandler{}
	r2 := httptest.NewRequest(http.MethodPost, "/api/reviews", nil)
	r2.Header.Set("Sec-Fetch-Site", "same-origin")
	r2.Host = "example.test"
	w2 := httptest.NewRecorder()
	s.originGuard(rec2).ServeHTTP(w2, r2)
	if !rec2.invoked {
		t.Fatal("same-origin fetch: handler was not invoked")
	}
}

func TestOriginGuardRejectsAForeignOrigin(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger()}}
	rec := &recordingHandler{}
	r := httptest.NewRequest(http.MethodPost, "/api/reviews", nil)
	r.Header.Set("Origin", "http://evil.test")
	r.Host = "example.test"
	w := httptest.NewRecorder()
	s.originGuard(rec).ServeHTTP(w, r)
	if rec.invoked {
		t.Fatal("handler was invoked for a foreign origin")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	assertErrorCode(t, w, "FORBIDDEN")
}

func TestOriginGuardRejectsAMissingOriginAndSecFetchSite(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger()}}
	rec := &recordingHandler{}
	r := httptest.NewRequest(http.MethodPost, "/api/reviews", nil)
	r.Host = "example.test"
	w := httptest.NewRecorder()
	s.originGuard(rec).ServeHTTP(w, r)
	if rec.invoked {
		t.Fatal("handler was invoked with neither Origin nor Sec-Fetch-Site")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestOriginGuardIgnoresNonAPIPaths(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger()}}
	rec := &recordingHandler{}
	r := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	r.Header.Set("Origin", "http://evil.test")
	r.Host = "example.test"
	w := httptest.NewRecorder()
	s.originGuard(rec).ServeHTTP(w, r)
	if !rec.invoked {
		t.Fatal("non-API path: handler was not invoked")
	}
}

func TestAuthenticateAllowsPublicRoutes(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger(), Auth: newTestAuthService(t)}}
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/auth/mode"},
		{http.MethodPost, "/api/auth/register"},
		{http.MethodPost, "/api/auth/login"},
	}
	for _, c := range cases {
		rec := &recordingHandler{}
		r := httptest.NewRequest(c.method, c.path, nil)
		w := httptest.NewRecorder()
		s.authenticate(rec).ServeHTTP(w, r)
		if !rec.invoked {
			t.Fatalf("%s %s: handler was not invoked", c.method, c.path)
		}
	}
}

func TestAuthenticateRejectsAMissingCookie(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger(), Auth: newTestAuthService(t)}}
	rec := &recordingHandler{}
	r := httptest.NewRequest(http.MethodGet, "/api/reviews", nil)
	w := httptest.NewRecorder()
	s.authenticate(rec).ServeHTTP(w, r)
	if rec.invoked {
		t.Fatal("handler was invoked with no cookie")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	assertErrorCode(t, w, "UNAUTHENTICATED")
}

func TestAuthenticateRejectsAnUnknownToken(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger(), Auth: newTestAuthService(t)}}
	rec := &recordingHandler{}
	r := httptest.NewRequest(http.MethodGet, "/api/reviews", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-registered-token"})
	w := httptest.NewRecorder()
	s.authenticate(rec).ServeHTTP(w, r)
	if rec.invoked {
		t.Fatal("handler was invoked for an unknown token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	set := w.Header().Get("Set-Cookie")
	if !strings.Contains(set, sessionCookieName+"=") {
		t.Fatalf("Set-Cookie = %q, want it to clear %s", set, sessionCookieName)
	}
	// Go's http.Cookie encodes a negative MaxAge (immediate deletion) on the
	// wire as "Max-Age=0" (RFC 6265 §5.2.2), which is what the brief's
	// acceptance criterion checks for.
	if !strings.Contains(set, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want Max-Age=0", set)
	}
}

func TestAuthenticateInjectsTheScopeAndTokenHash(t *testing.T) {
	svc := newTestAuthService(t)
	s := &server{deps: Deps{Log: testLogger(), Auth: svc}}
	user, token, err := svc.Register(context.Background(), auth.Credentials{Username: "alice", Password: "correct horse battery staple"}, "127.0.0.1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	var sawScope identity.Scope
	var sawHash []byte
	rec := &recordingHandler{fn: func(_ http.ResponseWriter, r *http.Request) {
		sawScope = scopeFrom(r)
		sawHash = tokenHashFrom(r)
	}}
	r := httptest.NewRequest(http.MethodGet, "/api/reviews", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	w := httptest.NewRecorder()
	s.authenticate(rec).ServeHTTP(w, r)
	if !rec.invoked {
		t.Fatal("handler was not invoked for a valid session")
	}
	if sawScope.UserID() != user.ID() {
		t.Fatalf("scopeFrom(r).UserID() = %q, want %q", sawScope.UserID(), user.ID())
	}
	if len(sawHash) != 32 {
		t.Fatalf("tokenHashFrom(r) length = %d, want 32", len(sawHash))
	}
}

func TestAuthenticateLeavesHealthzAndUIAlone(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger(), Auth: newTestAuthService(t)}}
	for _, path := range []string{"/healthz", "/"} {
		rec := &recordingHandler{}
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		s.authenticate(rec).ServeHTTP(w, r)
		if !rec.invoked {
			t.Fatalf("%s: handler was not invoked", path)
		}
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger(), LoginSessionTTL: time.Hour, SecureCookies: false}}
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	w := httptest.NewRecorder()
	s.setSessionCookie(w, r, "token-value")
	set := w.Header().Get("Set-Cookie")
	for _, want := range []string{"HttpOnly", "Path=/", "SameSite=Lax"} {
		if !strings.Contains(set, want) {
			t.Fatalf("Set-Cookie = %q, want it to contain %q", set, want)
		}
	}
	if strings.Contains(set, "Secure") {
		t.Fatalf("Set-Cookie = %q, want no Secure over plain HTTP with SecureCookies=false", set)
	}

	s2 := &server{deps: Deps{Log: testLogger(), LoginSessionTTL: time.Hour, SecureCookies: true}}
	w2 := httptest.NewRecorder()
	s2.setSessionCookie(w2, r, "token-value")
	if !strings.Contains(w2.Header().Get("Set-Cookie"), "Secure") {
		t.Fatal("SecureCookies=true: Set-Cookie missing Secure")
	}

	s3 := &server{deps: Deps{Log: testLogger(), LoginSessionTTL: time.Hour, SecureCookies: false}}
	rTLS := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	rTLS.TLS = &tls.ConnectionState{}
	w3 := httptest.NewRecorder()
	s3.setSessionCookie(w3, rTLS, "token-value")
	if !strings.Contains(w3.Header().Get("Set-Cookie"), "Secure") {
		t.Fatal("TLS request with SecureCookies=false: Set-Cookie missing Secure")
	}
}

func TestClientIP(t *testing.T) {
	s := &server{deps: Deps{Log: testLogger(), TrustedProxy: false}}
	r := httptest.NewRequest(http.MethodGet, "/api/reviews", nil)
	r.RemoteAddr = "203.0.113.5:12345"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := s.clientIP(r); got != "203.0.113.5" {
		t.Fatalf("clientIP (untrusted proxy) = %q, want %q", got, "203.0.113.5")
	}

	s2 := &server{deps: Deps{Log: testLogger(), TrustedProxy: true}}
	r2 := httptest.NewRequest(http.MethodGet, "/api/reviews", nil)
	r2.RemoteAddr = "203.0.113.5:12345"
	r2.Header.Set("X-Forwarded-For", "9.9.9.9, 1.2.3.4")
	if got := s2.clientIP(r2); got != "1.2.3.4" {
		t.Fatalf("clientIP (trusted proxy) = %q, want %q", got, "1.2.3.4")
	}
}
