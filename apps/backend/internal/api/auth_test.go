package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/jsonapi"
)

// authTestRouter builds the /api/auth/* surface directly over the handlers
// this task adds. It exists here, rather than in router.go, because route
// registration is explicitly out of scope for this task (Task 19).
//
// mode/register/login are unauthenticated by contract. logout is registered
// unauthenticated too and reads the cookie itself via sessionTokenFrom
// (authctx.go: "Only the logout handler and the authenticate middleware use
// it"), which is what lets it stay idempotent for a missing or stale cookie
// (FR-3.6) rather than being rejected by the authenticate middleware before
// it runs. me/password/account-deletion each wrap the authenticate
// middleware directly, since those routes need scopeFrom/tokenHashFrom.
// Every state-changing route runs behind originGuard.
func authTestRouter(mode config.Mode, svc *auth.Service) http.Handler {
	s := &server{deps: Deps{Mode: mode, Auth: svc, Log: testLogger(), LoginSessionTTL: time.Hour}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/mode", s.authMode)
	if mode == config.ModeHosted {
		mux.HandleFunc("POST /api/auth/register", s.register)
		mux.HandleFunc("POST /api/auth/login", s.login)
		mux.HandleFunc("POST /api/auth/logout", s.logout)
		mux.Handle("GET /api/auth/me", s.authenticate(http.HandlerFunc(s.currentUser)))
		mux.Handle("POST /api/auth/password", s.authenticate(http.HandlerFunc(s.changePassword)))
		mux.Handle("DELETE /api/auth/me", s.authenticate(http.HandlerFunc(s.deleteAccount)))
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", jsonapi.StatusTitle(http.StatusNotFound), "No such endpoint.")
	})
	var h http.Handler = mux
	if mode == config.ModeHosted {
		h = s.originGuard(h)
	}
	return withMiddleware(h, s.deps.Log)
}

// newHostedServer stands up a hosted-mode router over a temp database.
func newHostedServer(t *testing.T) (http.Handler, *auth.Service) {
	t.Helper()
	svc := newTestAuthService(t)
	return authTestRouter(config.ModeHosted, svc), svc
}

func newStandaloneServer(t *testing.T) http.Handler {
	t.Helper()
	return authTestRouter(config.ModeStandalone, nil)
}

// doAuth issues a request with the headers the origin guard and content
// negotiation require, plus any cookies supplied.
func doAuth(t *testing.T, h http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.Header.Set("Accept", jsonapi.MediaType)
	r.Header.Set("Content-Type", jsonapi.MediaType)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func credentialsBody(username, password string) string {
	return fmt.Sprintf(`{"data":{"type":"credentials","attributes":{"username":%q,"password":%q}}}`, username, password)
}

func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
}

func TestAuthModeReportsTheMode(t *testing.T) {
	h, _ := newHostedServer(t)
	w := doAuth(t, h, http.MethodGet, "/api/auth/mode", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatalf("unexpected cookie on unauthenticated mode call: %v", w.Result().Cookies())
	}
	res := decodeOne(t, w)
	if res["type"] != "modes" || res["id"] != "current" {
		t.Fatalf("data = %+v, want type=modes id=current", res)
	}
	attrs := res["attributes"].(map[string]any)
	if attrs["mode"] != "hosted" || attrs["registrationOpen"] != true {
		t.Fatalf("attributes = %+v, want mode=hosted registrationOpen=true", attrs)
	}

	sh := newStandaloneServer(t)
	w2 := doAuth(t, sh, http.MethodGet, "/api/auth/mode", "")
	if w2.Code != http.StatusOK {
		t.Fatalf("standalone status = %d, want 200; body=%s", w2.Code, w2.Body.String())
	}
	attrs2 := decodeOne(t, w2)["attributes"].(map[string]any)
	if attrs2["mode"] != "standalone" || attrs2["registrationOpen"] != false {
		t.Fatalf("standalone attributes = %+v, want mode=standalone registrationOpen=false", attrs2)
	}
}

func TestRegisterSetsTheSessionCookieAndReturnsTheUser(t *testing.T) {
	h, _ := newHostedServer(t)
	w := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("alice", "correct horse battery staple"))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	res := decodeOne(t, w)
	if res["type"] != "users" {
		t.Fatalf("type = %v, want users", res["type"])
	}
	attrs := res["attributes"].(map[string]any)
	if attrs["username"] != "alice" {
		t.Fatalf("username = %v, want alice", attrs["username"])
	}
	createdAt, ok := attrs["createdAt"].(string)
	if !ok {
		t.Fatalf("createdAt = %v, want a string", attrs["createdAt"])
	}
	if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
		t.Fatalf("createdAt = %q is not RFC3339: %v", createdAt, err)
	}
	cookie := sessionCookie(w)
	if cookie == nil {
		t.Fatal("no converge_session cookie set")
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "password") {
		t.Fatalf("response body leaked the word password: %s", w.Body.String())
	}
}

func TestRegisterRejectsAnUnknownAttribute(t *testing.T) {
	h, _ := newHostedServer(t)
	w := doAuth(t, h, http.MethodPost, "/api/auth/register",
		`{"data":{"type":"credentials","attributes":{"username":"alice","password":"correct horse battery staple","nickname":"x"}}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w, "INVALID_REQUEST")
}

func TestRegisterErrorCodes(t *testing.T) {
	h, _ := newHostedServer(t)
	w := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("a", "correct horse battery staple"))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad username: status = %d, want 422; body=%s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w, "INVALID_USERNAME")

	w2 := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("bob", "short"))
	if w2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("weak password: status = %d, want 422; body=%s", w2.Code, w2.Body.String())
	}
	assertErrorCode(t, w2, "WEAK_PASSWORD")

	w3 := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("carol", "correct horse battery staple"))
	if w3.Code != http.StatusCreated {
		t.Fatalf("first carol registration: status = %d, want 201; body=%s", w3.Code, w3.Body.String())
	}
	w4 := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("carol", "another long enough passphrase"))
	if w4.Code != http.StatusConflict {
		t.Fatalf("duplicate carol: status = %d, want 409; body=%s", w4.Code, w4.Body.String())
	}
	assertErrorCode(t, w4, "USERNAME_TAKEN")
}

func TestLoginAndLogout(t *testing.T) {
	h, _ := newHostedServer(t)
	reg := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("dave", "correct horse battery staple"))
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201; body=%s", reg.Code, reg.Body.String())
	}
	cookie := sessionCookie(reg)
	if cookie == nil {
		t.Fatal("no cookie from register")
	}

	logout := doAuth(t, h, http.MethodPost, "/api/auth/logout", "", cookie)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d, want 204; body=%s", logout.Code, logout.Body.String())
	}
	// Go encodes a negative MaxAge (immediate deletion) on the wire as
	// "Max-Age=0" (RFC 6265 §5.2.2); the parsed http.Cookie.MaxAge round-trips
	// that back to -1, so the header text is the acceptance evidence.
	if set := logout.Header().Get("Set-Cookie"); !strings.Contains(set, sessionCookieName+"=;") || !strings.Contains(set, "Max-Age=0") {
		t.Fatalf("logout Set-Cookie = %q, want it to clear %s with Max-Age=0", set, sessionCookieName)
	}

	me := doAuth(t, h, http.MethodGet, "/api/auth/me", "", cookie)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: status = %d, want 401; body=%s", me.Code, me.Body.String())
	}

	login := doAuth(t, h, http.MethodPost, "/api/auth/login", credentialsBody("dave", "correct horse battery staple"))
	if login.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200; body=%s", login.Code, login.Body.String())
	}
	if sessionCookie(login) == nil {
		t.Fatal("login did not set a fresh cookie")
	}
}

func TestLogoutIsIdempotent(t *testing.T) {
	h, _ := newHostedServer(t)
	w := doAuth(t, h, http.MethodPost, "/api/auth/logout", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout with no cookie: status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	stale := &http.Cookie{Name: sessionCookieName, Value: "not-a-real-token"}
	w2 := doAuth(t, h, http.MethodPost, "/api/auth/logout", "", stale)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("logout with stale cookie: status = %d, want 204; body=%s", w2.Code, w2.Body.String())
	}
}

func TestLoginReturnsRetryAfterOnLockout(t *testing.T) {
	h, _ := newHostedServer(t)
	reg := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("erin", "correct horse battery staple"))
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201; body=%s", reg.Code, reg.Body.String())
	}
	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		last = doAuth(t, h, http.MethodPost, "/api/auth/login", credentialsBody("erin", "wrong password"))
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth attempt: status = %d, want 429; body=%s", last.Code, last.Body.String())
	}
	assertErrorCode(t, last, "ACCOUNT_LOCKED")
	ra, err := strconv.Atoi(last.Header().Get("Retry-After"))
	if err != nil || ra <= 0 {
		t.Fatalf("Retry-After = %q, want a positive integer", last.Header().Get("Retry-After"))
	}
}

func TestCurrentUserIncludesProviderCount(t *testing.T) {
	h, svc := newHostedServer(t)
	reg := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("frank", "correct horse battery staple"))
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201; body=%s", reg.Code, reg.Body.String())
	}
	cookie := sessionCookie(reg)
	userID := decodeOne(t, reg)["id"].(string)

	addTestUserProviders(t, svc, userID, 2)

	me := doAuth(t, h, http.MethodGet, "/api/auth/me", "", cookie)
	if me.Code != http.StatusOK {
		t.Fatalf("me: status = %d, want 200; body=%s", me.Code, me.Body.String())
	}
	attrs := decodeOne(t, me)["attributes"].(map[string]any)
	if attrs["providerCount"] != float64(2) {
		t.Fatalf("providerCount = %v, want 2", attrs["providerCount"])
	}
}

func TestChangePasswordKeepsTheCallingSession(t *testing.T) {
	h, _ := newHostedServer(t)
	reg := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("grace", "correct horse battery staple"))
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201; body=%s", reg.Code, reg.Body.String())
	}
	cookie1 := sessionCookie(reg)

	login2 := doAuth(t, h, http.MethodPost, "/api/auth/login", credentialsBody("grace", "correct horse battery staple"))
	if login2.Code != http.StatusOK {
		t.Fatalf("second login: status = %d, want 200; body=%s", login2.Code, login2.Body.String())
	}
	cookie2 := sessionCookie(login2)

	body := `{"data":{"type":"passwords","attributes":{"currentPassword":"correct horse battery staple","newPassword":"a much longer replacement phrase"}}}`
	change := doAuth(t, h, http.MethodPost, "/api/auth/password", body, cookie1)
	if change.Code != http.StatusNoContent {
		t.Fatalf("change password: status = %d, want 204; body=%s", change.Code, change.Body.String())
	}

	me1 := doAuth(t, h, http.MethodGet, "/api/auth/me", "", cookie1)
	if me1.Code != http.StatusOK {
		t.Fatalf("me with cookie1 after change: status = %d, want 200; body=%s", me1.Code, me1.Body.String())
	}
	me2 := doAuth(t, h, http.MethodGet, "/api/auth/me", "", cookie2)
	if me2.Code != http.StatusUnauthorized {
		t.Fatalf("me with cookie2 after change: status = %d, want 401; body=%s", me2.Code, me2.Body.String())
	}

	oldLogin := doAuth(t, h, http.MethodPost, "/api/auth/login", credentialsBody("grace", "correct horse battery staple"))
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("login with old password: status = %d, want 401; body=%s", oldLogin.Code, oldLogin.Body.String())
	}
	newLogin := doAuth(t, h, http.MethodPost, "/api/auth/login", credentialsBody("grace", "a much longer replacement phrase"))
	if newLogin.Code != http.StatusOK {
		t.Fatalf("login with new password: status = %d, want 200; body=%s", newLogin.Code, newLogin.Body.String())
	}
}

func TestDeleteAccountClearsTheCookie(t *testing.T) {
	h, _ := newHostedServer(t)
	reg := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody("heidi", "correct horse battery staple"))
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201; body=%s", reg.Code, reg.Body.String())
	}
	cookie := sessionCookie(reg)

	body := `{"data":{"type":"accountDeletions","attributes":{"password":"correct horse battery staple"}}}`
	del := doAuth(t, h, http.MethodDelete, "/api/auth/me", body, cookie)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204; body=%s", del.Code, del.Body.String())
	}
	if set := del.Header().Get("Set-Cookie"); !strings.Contains(set, sessionCookieName+"=;") || !strings.Contains(set, "Max-Age=0") {
		t.Fatalf("delete Set-Cookie = %q, want it to clear %s with Max-Age=0", set, sessionCookieName)
	}

	me := doAuth(t, h, http.MethodGet, "/api/auth/me", "", cookie)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("me after delete: status = %d, want 401; body=%s", me.Code, me.Body.String())
	}
}

// TestAuthRoutesAre404InStandalone proves NewRouter's real registration (not
// authTestRouter's scaffolding): with Mode left at ModeStandalone, every
// /api/auth/* route except the always-public "mode" one is unregistered, so
// the "/api/" catch-all answers 404 NOT_FOUND for it.
func TestAuthRoutesAre404InStandalone(t *testing.T) {
	h := NewRouter(Deps{Mode: config.ModeStandalone, Log: testLogger()})
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/auth/register"},
		{http.MethodPost, "/api/auth/login"},
		{http.MethodPost, "/api/auth/logout"},
		{http.MethodGet, "/api/auth/me"},
		{http.MethodDelete, "/api/auth/me"},
		{http.MethodPost, "/api/auth/password"},
	} {
		w := doAuth(t, h, tc.method, tc.path, "")
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404; body=%s", tc.method, tc.path, w.Code, w.Body.String())
			continue
		}
		assertErrorCode(t, w, "NOT_FOUND")
	}
	// /api/auth/mode stays registered and unauthenticated in standalone.
	w := doAuth(t, h, http.MethodGet, "/api/auth/mode", "")
	if w.Code != http.StatusOK {
		t.Fatalf("mode: status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestStateChangingAuthRoutesRequireTheOriginCheck(t *testing.T) {
	h, _ := newHostedServer(t)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(credentialsBody("ivan", "correct horse battery staple")))
	r.Header.Set("Accept", jsonapi.MediaType)
	r.Header.Set("Content-Type", jsonapi.MediaType)
	r.Header.Set("Origin", "http://evil.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w, "FORBIDDEN")
}

// addTestUserProviders creates n distinct provider configurations for userID,
// without contacting a real provider API (Validate: false).
func addTestUserProviders(t *testing.T, svc *auth.Service, userID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := svc.CreateProvider(context.Background(), userID, auth.ProviderInput{
			Slug:    fmt.Sprintf("provider-%d", i),
			Kind:    config.KindGitHub,
			BaseURL: "https://api.github.com",
			Token:   "ghp_0123456789012345678901234567890123456789",
		})
		if err != nil {
			t.Fatalf("CreateProvider %d: %v", i, err)
		}
	}
}
