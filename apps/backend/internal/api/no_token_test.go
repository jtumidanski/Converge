package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNoResponseBodyEverContainsAToken captures every response body produced
// by a full hosted-mode exercise and asserts none contains the plaintext token
// fixture.
//
// FR-5.4 is a property of the whole API surface, not of one handler, so it is
// asserted over every response rather than route by route. A new endpoint that
// leaks a token fails here even if its own test does not check.
func TestNoResponseBodyEverContainsAToken(t *testing.T) {
	const token = "glpat-SENTINEL-TOKEN-VALUE-9f2c"

	f := newHostedFixture(t)

	// capture records every response body this exercise produces, regardless
	// of status: an error body leaking a token is exactly as bad as a 2xx one.
	var bodies []string
	call := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		w := doAuth(t, f.handler, method, path, body, cookies...)
		bodies = append(bodies, w.Body.String())
		return w
	}

	// /api/auth/mode is unauthenticated.
	call(http.MethodGet, "/api/auth/mode", "")

	username := "walker-no-token"
	password := "correct horse battery staple"
	reg := call(http.MethodPost, "/api/auth/register", credentialsBody(username, password))
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201; body=%s", reg.Code, reg.Body.String())
	}
	cookie := sessionCookie(reg)
	if cookie == nil {
		t.Fatal("no session cookie from register")
	}

	login := call(http.MethodPost, "/api/auth/login", credentialsBody(username, password))
	if login.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200; body=%s", login.Code, login.Body.String())
	}
	cookie = sessionCookie(login)
	if cookie == nil {
		t.Fatal("no session cookie from login")
	}

	call(http.MethodGet, "/api/auth/me", "", cookie)

	// /api/settings/providers: create, list, patch, (delete happens later,
	// after the routes below have had a chance to see it in place).
	create := call(http.MethodPost, "/api/settings/providers",
		createProviderBody("walker-gitlab", "Walker GitLab", "gitlab", "https://gitlab.example.com", token, nil), cookie)
	if create.Code != http.StatusCreated {
		t.Fatalf("create provider: status = %d, want 201; body=%s", create.Code, create.Body.String())
	}
	id := decodeOne(t, create)["id"].(string)

	call(http.MethodGet, "/api/settings/providers", "", cookie)

	patch := call(http.MethodPatch, "/api/settings/providers/"+id,
		`{"data":{"type":"userProviders","attributes":{"displayName":"renamed"}}}`, cookie)
	if patch.Code != http.StatusOK {
		t.Fatalf("patch provider: status = %d, want 200; body=%s", patch.Code, patch.Body.String())
	}

	provList := call(http.MethodGet, "/api/providers", "", cookie)
	if provList.Code != http.StatusOK {
		t.Errorf("GET /api/providers: status = %d; body=%s", provList.Code, provList.Body.String())
	}

	repos := call(http.MethodGet, "/api/providers/fake/repositories", "", cookie)
	if repos.Code != http.StatusOK {
		t.Errorf("GET /api/providers/fake/repositories: status = %d; body=%s", repos.Code, repos.Body.String())
	}

	created := call(http.MethodPost, "/api/reviews",
		`{"data":{"type":"reviews","attributes":{"provider":"fake","repository":"atlas/server","baseBranch":"main","changes":[421]}}}`, cookie)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create review: status = %d, want 202; body=%s", created.Code, created.Body.String())
	}
	reviewID := decodeOne(t, created)["id"].(string)

	deadline := time.Now().Add(30 * time.Second)
	for {
		gw := call(http.MethodGet, "/api/reviews/"+reviewID, "", cookie)
		if gw.Code != http.StatusOK {
			t.Fatalf("poll review: status = %d; body=%s", gw.Code, gw.Body.String())
		}
		status := decodeOne(t, gw)["attributes"].(map[string]any)["status"]
		if status != "CREATING" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("review did not leave CREATING in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	call(http.MethodGet, "/api/reviews", "", cookie)
	call(http.MethodGet, "/api/reviews/"+reviewID+"/files", "", cookie)
	call(http.MethodGet, "/api/reviews/"+reviewID+"/diff", "", cookie)

	del := call(http.MethodDelete, "/api/settings/providers/"+id, "", cookie)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete provider: status = %d, want 204; body=%s", del.Code, del.Body.String())
	}

	newPassword := "a much longer replacement passphrase"
	change := call(http.MethodPost, "/api/auth/password",
		`{"data":{"type":"passwords","attributes":{"currentPassword":`+strconv.Quote(password)+`,"newPassword":`+strconv.Quote(newPassword)+`}}}`, cookie)
	if change.Code != http.StatusNoContent {
		t.Fatalf("change password: status = %d, want 204; body=%s", change.Code, change.Body.String())
	}

	logout := call(http.MethodPost, "/api/auth/logout", "", cookie)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d, want 204; body=%s", logout.Code, logout.Body.String())
	}

	relogin := call(http.MethodPost, "/api/auth/login", credentialsBody(username, newPassword))
	if relogin.Code != http.StatusOK {
		t.Fatalf("relogin: status = %d, want 200; body=%s", relogin.Code, relogin.Body.String())
	}
	cookie = sessionCookie(relogin)
	if cookie == nil {
		t.Fatal("no session cookie from relogin")
	}

	del2 := call(http.MethodDelete, "/api/auth/me",
		`{"data":{"type":"accountDeletions","attributes":{"password":`+strconv.Quote(newPassword)+`}}}`, cookie)
	if del2.Code != http.StatusNoContent {
		t.Fatalf("delete account: status = %d, want 204; body=%s", del2.Code, del2.Body.String())
	}

	for i, body := range bodies {
		if strings.Contains(body, token) {
			t.Errorf("response %d contained the provider token", i)
		}
		// Matched with a trailing colon so a key like "tokenLast4" does not
		// false-positive: the brief's assertion (`strings.Contains(body,
		// `"token"`)`) would also fire on `"tokenLast4":"9f2c"`, which is the
		// mask this test's own vacuity guard below requires be present.
		if strings.Contains(body, `"token":`) {
			t.Errorf("response %d contained a token attribute: %s", i, body)
		}
	}
	// Prove the mask is present, so the test is not passing vacuously.
	if !strings.Contains(strings.Join(bodies, ""), `"tokenLast4":"9f2c"`) {
		t.Fatal("no response carried tokenLast4; the exercise did not reach the provider endpoints")
	}
}
