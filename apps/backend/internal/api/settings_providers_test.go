package api

import (
	"context"
	"fmt"
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
	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
)

// settingsTestRouter builds the /api/settings/providers surface plus the
// minimum of /api/auth/* and /api/providers this file's tests need, directly
// over the handlers this task adds. Route registration is Task 19's job, so
// this exists only here, matching authTestRouter's rationale in auth_test.go.
func settingsTestRouter(mode config.Mode, svc *auth.Service) http.Handler {
	s := &server{deps: Deps{
		Mode:      mode,
		Auth:      svc,
		Log:       testLogger(),
		Providers: provider.NewStaticResolver(provider.NewRegistry()),
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/providers", s.listProviders)
	if mode == config.ModeHosted {
		mux.HandleFunc("POST /api/auth/register", s.register)
		mux.Handle("GET /api/auth/me", s.authenticate(http.HandlerFunc(s.currentUser)))
		mux.Handle("GET /api/settings/providers", s.authenticate(http.HandlerFunc(s.listUserProviders)))
		mux.Handle("POST /api/settings/providers", s.authenticate(http.HandlerFunc(s.createUserProvider)))
		mux.Handle("PATCH /api/settings/providers/{id}", s.authenticate(http.HandlerFunc(s.updateUserProvider)))
		mux.Handle("DELETE /api/settings/providers/{id}", s.authenticate(http.HandlerFunc(s.deleteUserProvider)))
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

// toggledVerifier lets a test flip between "the provider API accepts this
// credential" and "it rejects it" without needing a real HTTP round trip.
type toggledVerifier struct{ reject bool }

func (v *toggledVerifier) Verify(context.Context, config.Kind, string, config.Secret) error {
	if v.reject {
		return &auth.Error{Code: auth.CodeProviderUnauthorized, Message: "The provider rejected this credential."}
	}
	return nil
}

// toggledUsage lets a test simulate a provider still referenced by a
// non-terminal review session.
type toggledUsage struct{ inUse bool }

func (u *toggledUsage) ProviderInUse(identity.Scope, string) bool { return u.inUse }

// newSettingsTestAuthService returns a real, migrated auth.Service wired to
// verifier and usage, so DeleteProvider/CreateProvider/UpdateProvider run
// their genuine logic against a controllable verifier and usage collaborator.
func newSettingsTestAuthService(t *testing.T, verifier auth.ProviderVerifier, usage auth.ProviderUsage) (*auth.Service, *auth.ProviderResolver) {
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
	svc := auth.NewService(auth.ServiceDeps{
		Store:      store,
		Sealer:     sealer,
		Throttle:   throttle,
		Resolver:   resolver,
		Verifier:   verifier,
		Purger:     stubPurger{},
		Usage:      usage,
		Log:        testLogger(),
		Now:        now,
		SessionTTL: 24 * time.Hour,
		IdleTTL:    2 * time.Hour,
	})
	return svc, resolver
}

// registerAndCookie registers a fresh user and returns the router, the
// session cookie, and the new user's id.
func registerAndCookie(t *testing.T, h http.Handler, username string) (*http.Cookie, string) {
	t.Helper()
	reg := doAuth(t, h, http.MethodPost, "/api/auth/register", credentialsBody(username, "correct horse battery staple"))
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201; body=%s", reg.Code, reg.Body.String())
	}
	return sessionCookie(reg), decodeOne(t, reg)["id"].(string)
}

func createProviderBody(slug, displayName, kind, baseURL, token string, validate *bool) string {
	v := "null"
	if validate != nil {
		v = fmt.Sprintf("%v", *validate)
	}
	return fmt.Sprintf(`{"data":{"type":"userProviders","attributes":{"slug":%q,"displayName":%q,"kind":%q,"baseUrl":%q,"token":%q,"validate":%s}}}`,
		slug, displayName, kind, baseURL, token, v)
}

func TestUserProviderCRUDOverHTTP(t *testing.T) {
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "alice")

	create := doAuth(t, h, http.MethodPost, "/api/settings/providers",
		createProviderBody("github", "GitHub", "github", "", "ghp_0123456789012345678901234567890123456789", nil), cookie)
	if create.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201; body=%s", create.Code, create.Body.String())
	}
	created := decodeOne(t, create)
	if created["type"] != "userProviders" {
		t.Fatalf("create type = %v, want userProviders", created["type"])
	}
	id := created["id"].(string)

	list := doAuth(t, h, http.MethodGet, "/api/settings/providers", "", cookie)
	if list.Code != http.StatusOK || len(decodeList(t, list)) != 1 {
		t.Fatalf("list: status = %d, len = %d; body=%s", list.Code, len(decodeList(t, list)), list.Body.String())
	}

	patch := doAuth(t, h, http.MethodPatch, "/api/settings/providers/"+id,
		`{"data":{"type":"userProviders","attributes":{"displayName":"GitHub (renamed)"}}}`, cookie)
	if patch.Code != http.StatusOK {
		t.Fatalf("patch: status = %d, want 200; body=%s", patch.Code, patch.Body.String())
	}
	if decodeOne(t, patch)["attributes"].(map[string]any)["displayName"] != "GitHub (renamed)" {
		t.Fatalf("patch attributes = %+v", decodeOne(t, patch)["attributes"])
	}

	del := doAuth(t, h, http.MethodDelete, "/api/settings/providers/"+id, "", cookie)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204; body=%s", del.Code, del.Body.String())
	}

	list2 := doAuth(t, h, http.MethodGet, "/api/settings/providers", "", cookie)
	if list2.Code != http.StatusOK || len(decodeList(t, list2)) != 0 {
		t.Fatalf("list after delete: status = %d, len = %d; body=%s", list2.Code, len(decodeList(t, list2)), list2.Body.String())
	}
}

func TestNoResponseEverContainsAToken(t *testing.T) {
	const sentinel = "glpat-SENTINELTOKEN9f2c"
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "bob")

	checkNoToken := func(t *testing.T, label string, w *httptest.ResponseRecorder) {
		t.Helper()
		body := w.Body.String()
		if strings.Contains(body, "SENTINEL") {
			t.Fatalf("%s: response body leaked the token: %s", label, body)
		}
		if strings.Contains(body, `"token"`) {
			t.Fatalf("%s: response body has a token key: %s", label, body)
		}
	}

	create := doAuth(t, h, http.MethodPost, "/api/settings/providers",
		createProviderBody("gitlab-internal", "Internal GitLab", "gitlab", "https://gitlab.example.com", sentinel, nil), cookie)
	if create.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201; body=%s", create.Code, create.Body.String())
	}
	checkNoToken(t, "create", create)
	id := decodeOne(t, create)["id"].(string)
	if decodeOne(t, create)["attributes"].(map[string]any)["tokenLast4"] != "9f2c" {
		t.Fatalf("tokenLast4 = %v, want 9f2c", decodeOne(t, create)["attributes"].(map[string]any)["tokenLast4"])
	}

	list := doAuth(t, h, http.MethodGet, "/api/settings/providers", "", cookie)
	checkNoToken(t, "list", list)

	patch := doAuth(t, h, http.MethodPatch, "/api/settings/providers/"+id,
		`{"data":{"type":"userProviders","attributes":{"displayName":"renamed"}}}`, cookie)
	checkNoToken(t, "patch", patch)

	providers := doAuth(t, h, http.MethodGet, "/api/providers", "", cookie)
	checkNoToken(t, "GET /api/providers", providers)

	me := doAuth(t, h, http.MethodGet, "/api/auth/me", "", cookie)
	checkNoToken(t, "GET /api/auth/me", me)

	del := doAuth(t, h, http.MethodDelete, "/api/settings/providers/"+id, "", cookie)
	checkNoToken(t, "delete", del)
}

func TestCreateValidatesTheRequestShape(t *testing.T) {
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "carol")

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"bad slug", createProviderBody("Bad_Slug", "x", "github", "", "ghp_0123456789012345678901234567890123456789", nil), http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{"unknown kind", createProviderBody("valid-slug", "x", "bitbucket", "", "ghp_0123456789012345678901234567890123456789", nil), http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{"relative base url", createProviderBody("valid-slug", "x", "github", "/not/absolute", "ghp_0123456789012345678901234567890123456789", nil), http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{"missing token", createProviderBody("valid-slug", "x", "github", "", "", nil), http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{
			"unknown attribute", `{"data":{"type":"userProviders","attributes":{"slug":"valid-slug","kind":"github","token":"ghp_0123456789012345678901234567890123456789","unknownField":true}}}`,
			http.StatusBadRequest, "INVALID_REQUEST",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doAuth(t, h, http.MethodPost, "/api/settings/providers", tc.body, cookie)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			assertErrorCode(t, w, tc.wantCode)
		})
	}
}

func TestCreateDefaultsValidateToTrue(t *testing.T) {
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{reject: true}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "dave")

	w := doAuth(t, h, http.MethodPost, "/api/settings/providers",
		createProviderBody("github", "x", "github", "", "ghp_0123456789012345678901234567890123456789", nil), cookie)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w, "PROVIDER_UNAUTHORIZED")

	list := doAuth(t, h, http.MethodGet, "/api/settings/providers", "", cookie)
	if len(decodeList(t, list)) != 0 {
		t.Fatalf("a rejected create must write nothing, got %d rows", len(decodeList(t, list)))
	}
}

func TestCreateWithValidateFalseSkipsVerification(t *testing.T) {
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{reject: true}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "erin")

	no := false
	w := doAuth(t, h, http.MethodPost, "/api/settings/providers",
		createProviderBody("github", "x", "github", "", "ghp_0123456789012345678901234567890123456789", &no), cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
}

func TestDuplicateSlugIs409(t *testing.T) {
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "frank")

	body := createProviderBody("github", "x", "github", "", "ghp_0123456789012345678901234567890123456789", nil)
	first := doAuth(t, h, http.MethodPost, "/api/settings/providers", body, cookie)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d; body=%s", first.Code, first.Body.String())
	}
	second := doAuth(t, h, http.MethodPost, "/api/settings/providers", body, cookie)
	if second.Code != http.StatusConflict {
		t.Fatalf("second create: status = %d, want 409; body=%s", second.Code, second.Body.String())
	}
	assertErrorCode(t, second, "PROVIDER_SLUG_TAKEN")
}

func TestPatchWithAnOmittedTokenKeepsTheStoredOne(t *testing.T) {
	svc, resolver := newSettingsTestAuthService(t, &toggledVerifier{}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "grace")

	create := doAuth(t, h, http.MethodPost, "/api/settings/providers",
		createProviderBody("github", "x", "github", "", "ghp_0123456789012345678901234567890123456789", nil), cookie)
	if create.Code != http.StatusCreated {
		t.Fatalf("create: status = %d; body=%s", create.Code, create.Body.String())
	}
	created := decodeOne(t, create)
	id := created["id"].(string)
	attrsBefore := created["attributes"].(map[string]any)

	patch := doAuth(t, h, http.MethodPatch, "/api/settings/providers/"+id,
		`{"data":{"type":"userProviders","attributes":{"displayName":"renamed"}}}`, cookie)
	if patch.Code != http.StatusOK {
		t.Fatalf("patch: status = %d; body=%s", patch.Code, patch.Body.String())
	}

	list := doAuth(t, h, http.MethodGet, "/api/settings/providers", "", cookie)
	rows := decodeList(t, list)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	after := rows[0]["attributes"].(map[string]any)
	if after["tokenLast4"] != attrsBefore["tokenLast4"] {
		t.Fatalf("tokenLast4 changed: before=%v after=%v", attrsBefore["tokenLast4"], after["tokenLast4"])
	}
	if after["tokenSetAt"] != attrsBefore["tokenSetAt"] {
		t.Fatalf("tokenSetAt changed: before=%v after=%v", attrsBefore["tokenSetAt"], after["tokenSetAt"])
	}

	// Prove the token still decrypts: the resolver reads through the same
	// ciphertext/nonce this patch left untouched.
	registry, err := resolver.Resolve(context.Background(), identity.ForUser(mustUserID(t, h, cookie)))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := registry.Get("github"); !ok {
		t.Fatalf("provider %q not resolvable after patch", "github")
	}
}

// mustUserID recovers the calling cookie's user id via /api/auth/me, since
// registerAndCookie's caller may have discarded it.
func mustUserID(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
	me := doAuth(t, h, http.MethodGet, "/api/auth/me", "", cookie)
	if me.Code != http.StatusOK {
		t.Fatalf("me: status = %d; body=%s", me.Code, me.Body.String())
	}
	return decodeOne(t, me)["id"].(string)
}

func TestPatchRejectsASlugChange(t *testing.T) {
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "heidi")

	create := doAuth(t, h, http.MethodPost, "/api/settings/providers",
		createProviderBody("github", "x", "github", "", "ghp_0123456789012345678901234567890123456789", nil), cookie)
	id := decodeOne(t, create)["id"].(string)

	patch := doAuth(t, h, http.MethodPatch, "/api/settings/providers/"+id,
		`{"data":{"type":"userProviders","attributes":{"slug":"new-slug"}}}`, cookie)
	if patch.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", patch.Code, patch.Body.String())
	}
	assertErrorCode(t, patch, "INVALID_REQUEST")
}

func TestForeignProviderIDIs404(t *testing.T) {
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookieA, _ := registerAndCookie(t, h, "ivan")
	cookieB, _ := registerAndCookie(t, h, "judy")

	create := doAuth(t, h, http.MethodPost, "/api/settings/providers",
		createProviderBody("github", "x", "github", "", "ghp_0123456789012345678901234567890123456789", nil), cookieA)
	id := decodeOne(t, create)["id"].(string)

	// The brief's acceptance criterion names GET/PATCH/DELETE, but this
	// task's handler set (listUserProviders, createUserProvider,
	// updateUserProvider, deleteUserProvider) has no single-resource GET —
	// there is no "getUserProvider" handler to register. PATCH and DELETE
	// cover the cross-user check this test exists to prove.
	patch := doAuth(t, h, http.MethodPatch, "/api/settings/providers/"+id,
		`{"data":{"type":"userProviders","attributes":{"displayName":"mine now"}}}`, cookieB)
	if patch.Code != http.StatusNotFound {
		t.Fatalf("patch: status = %d, want 404; body=%s", patch.Code, patch.Body.String())
	}
	assertErrorCode(t, patch, "NOT_FOUND")

	del := doAuth(t, h, http.MethodDelete, "/api/settings/providers/"+id, "", cookieB)
	if del.Code != http.StatusNotFound {
		t.Fatalf("delete: status = %d, want 404; body=%s", del.Code, del.Body.String())
	}
	assertErrorCode(t, del, "NOT_FOUND")
}

func TestDeleteRefusesWhileInUse(t *testing.T) {
	usage := &toggledUsage{inUse: true}
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{}, usage)
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "kevin")

	create := doAuth(t, h, http.MethodPost, "/api/settings/providers",
		createProviderBody("github", "x", "github", "", "ghp_0123456789012345678901234567890123456789", nil), cookie)
	id := decodeOne(t, create)["id"].(string)

	del := doAuth(t, h, http.MethodDelete, "/api/settings/providers/"+id, "", cookie)
	if del.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", del.Code, del.Body.String())
	}
	assertErrorCode(t, del, "PROVIDER_IN_USE")
}

func TestSettingsRoutesAre404InStandalone(t *testing.T) {
	t.Skip("route registration lands in Task 19")
}

func TestListIsSortedBySlug(t *testing.T) {
	svc, _ := newSettingsTestAuthService(t, &toggledVerifier{}, &toggledUsage{})
	h := settingsTestRouter(config.ModeHosted, svc)
	cookie, _ := registerAndCookie(t, h, "laura")

	for _, slug := range []string{"zeta", "alpha"} {
		w := doAuth(t, h, http.MethodPost, "/api/settings/providers",
			createProviderBody(slug, slug, "github", "", "ghp_0123456789012345678901234567890123456789", nil), cookie)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: status = %d; body=%s", slug, w.Code, w.Body.String())
		}
	}

	list := doAuth(t, h, http.MethodGet, "/api/settings/providers", "", cookie)
	rows := decodeList(t, list)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0]["attributes"].(map[string]any)["slug"] != "alpha" || rows[1]["attributes"].(map[string]any)["slug"] != "zeta" {
		t.Fatalf("order = [%v, %v], want [alpha, zeta]", rows[0]["attributes"].(map[string]any)["slug"], rows[1]["attributes"].(map[string]any)["slug"])
	}
}
