package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/identity"
)

// mustProviderOfKind is mustProvider generalised to a caller-supplied kind
// and base URL, needed here because mustProvider (store_test.go) always
// seeds a github config.
func mustProviderOfKind(t *testing.T, s *auth.Store, userID, slug, token string, kind config.Kind, baseURL string) auth.UserProvider {
	t.Helper()
	sealer, err := auth.NewSealer(config.NewSecret(key32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	id, err := auth.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	ciphertext, nonce, err := sealer.Seal(token, userID, id)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	p, err := auth.NewUserProvider(id, userID, slug, "Display "+slug, kind, baseURL,
		ciphertext, nonce, auth.Last4(token), time.Unix(2000, 0).UTC())
	if err != nil {
		t.Fatalf("NewUserProvider: %v", err)
	}
	if err := s.CreateUserProvider(context.Background(), p); err != nil {
		t.Fatalf("CreateUserProvider: %v", err)
	}
	return p
}

func newTestSealer(t *testing.T) *auth.Sealer {
	t.Helper()
	sealer, err := auth.NewSealer(config.NewSecret(key32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return sealer
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestResolveBuildsARegistryFromTheUsersConfigurations(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "alice")
	gh := mustProviderOfKind(t, s, u.ID(), "gh-work", "tok-gh", config.KindGitHub, "https://api.github.com")
	gl := mustProviderOfKind(t, s, u.ID(), "gl-work", "tok-gl", config.KindGitLab, "https://gitlab.example.com")

	r := auth.NewProviderResolver(s, newTestSealer(t), http.DefaultClient, fixedClock(time.Unix(1000, 0)))
	registry, err := r.Resolve(context.Background(), identity.ForUser(u.ID()))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	all := registry.All()
	if len(all) != 2 {
		t.Fatalf("All() = %d entries, want 2", len(all))
	}
	byID := map[string]struct {
		kind        config.Kind
		displayName string
		baseURL     string
	}{
		gh.Slug(): {gh.Kind(), gh.DisplayName(), gh.BaseURL()},
		gl.Slug(): {gl.Kind(), gl.DisplayName(), gl.BaseURL()},
	}
	for _, p := range all {
		want, ok := byID[p.ID()]
		if !ok {
			t.Fatalf("unexpected provider id %q", p.ID())
		}
		if string(p.Kind()) != string(want.kind) {
			t.Errorf("provider %q Kind() = %q, want %q", p.ID(), p.Kind(), want.kind)
		}
		if p.DisplayName() != want.displayName {
			t.Errorf("provider %q DisplayName() = %q, want %q", p.ID(), p.DisplayName(), want.displayName)
		}
		if p.BaseURL() != want.baseURL {
			t.Errorf("provider %q BaseURL() = %q, want %q", p.ID(), p.BaseURL(), want.baseURL)
		}
	}
}

func TestResolveIsolatesUsers(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	alice := mustUser(t, s, "alice")
	bob := mustUser(t, s, "bob")
	mustProvider(t, s, alice.ID(), "alice-gh", "tok-a")
	mustProvider(t, s, bob.ID(), "bob-gh", "tok-b")

	r := auth.NewProviderResolver(s, newTestSealer(t), http.DefaultClient, fixedClock(time.Unix(1000, 0)))

	aliceRegistry, err := r.Resolve(context.Background(), identity.ForUser(alice.ID()))
	if err != nil {
		t.Fatalf("Resolve(alice): %v", err)
	}
	bobRegistry, err := r.Resolve(context.Background(), identity.ForUser(bob.ID()))
	if err != nil {
		t.Fatalf("Resolve(bob): %v", err)
	}

	if len(aliceRegistry.All()) != 1 || aliceRegistry.All()[0].ID() != "alice-gh" {
		t.Fatalf("alice registry = %+v, want exactly [alice-gh]", aliceRegistry.All())
	}
	if len(bobRegistry.All()) != 1 || bobRegistry.All()[0].ID() != "bob-gh" {
		t.Fatalf("bob registry = %+v, want exactly [bob-gh]", bobRegistry.All())
	}
	if _, ok := aliceRegistry.Get("bob-gh"); ok {
		t.Fatal("alice's registry contains bob's provider")
	}
	if _, ok := bobRegistry.Get("alice-gh"); ok {
		t.Fatal("bob's registry contains alice's provider")
	}
}

func TestResolveReturnsAnEmptyRegistryForAUserWithNoProviders(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "alice")

	r := auth.NewProviderResolver(s, newTestSealer(t), http.DefaultClient, fixedClock(time.Unix(1000, 0)))
	registry, err := r.Resolve(context.Background(), identity.ForUser(u.ID()))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(registry.All()) != 0 {
		t.Fatalf("All() = %d entries, want 0", len(registry.All()))
	}
}

func TestResolveRejectsAnUnscopedScope(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	r := auth.NewProviderResolver(s, newTestSealer(t), http.DefaultClient, fixedClock(time.Unix(1000, 0)))
	if _, err := r.Resolve(context.Background(), identity.Standalone()); err == nil {
		t.Fatal("Resolve(Standalone()) = nil error, want error")
	}
}

func TestResolveCachesAndInvalidate(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "alice")
	mustProvider(t, s, u.ID(), "one", "tok-1")
	mustProvider(t, s, u.ID(), "two", "tok-2")

	r := auth.NewProviderResolver(s, newTestSealer(t), http.DefaultClient, fixedClock(time.Unix(1000, 0)))
	first, err := r.Resolve(context.Background(), identity.ForUser(u.ID()))
	if err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}
	if len(first.All()) != 2 {
		t.Fatalf("first.All() = %d, want 2", len(first.All()))
	}

	mustProvider(t, s, u.ID(), "three", "tok-3")

	second, err := r.Resolve(context.Background(), identity.ForUser(u.ID()))
	if err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if len(second.All()) != 2 {
		t.Fatalf("second.All() = %d, want the stale 2", len(second.All()))
	}

	r.Invalidate(u.ID())

	third, err := r.Resolve(context.Background(), identity.ForUser(u.ID()))
	if err != nil {
		t.Fatalf("Resolve #3: %v", err)
	}
	if len(third.All()) != 3 {
		t.Fatalf("third.All() = %d, want 3 after Invalidate", len(third.All()))
	}
}

func TestCacheExpiresAfterTheTTL(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "alice")
	mustProvider(t, s, u.ID(), "one", "tok-1")

	now := time.Unix(1000, 0)
	clock := &now
	r := auth.NewProviderResolver(s, newTestSealer(t), http.DefaultClient, func() time.Time { return *clock })

	first, err := r.Resolve(context.Background(), identity.ForUser(u.ID()))
	if err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}
	if len(first.All()) != 1 {
		t.Fatalf("first.All() = %d, want 1", len(first.All()))
	}

	advanced := now.Add(16 * time.Minute)
	clock = &advanced

	mustProvider(t, s, u.ID(), "two", "tok-2")

	second, err := r.Resolve(context.Background(), identity.ForUser(u.ID()))
	if err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if len(second.All()) != 2 {
		t.Fatalf("second.All() = %d, want 2 after TTL expiry", len(second.All()))
	}
}

func TestResolveSurfacesADecryptFailure(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "alice")
	p := mustProvider(t, s, u.ID(), "one", "tok-1")

	// Corrupt the stored ciphertext directly, bypassing the store's typed
	// API since Store exposes no update-ciphertext method for a test to
	// misuse.
	corrupted := append([]byte(nil), p.TokenCiphertext()...)
	corrupted[0] ^= 0xFF
	updated, err := auth.NewUserProvider(p.ID(), p.UserID(), p.Slug(), p.DisplayName(), p.Kind(), p.BaseURL(),
		corrupted, p.TokenNonce(), p.TokenLast4(), p.TokenSetAt())
	if err != nil {
		t.Fatalf("NewUserProvider: %v", err)
	}
	if err := s.UpdateUserProvider(context.Background(), updated); err != nil {
		t.Fatalf("UpdateUserProvider: %v", err)
	}

	r := auth.NewProviderResolver(s, newTestSealer(t), http.DefaultClient, fixedClock(time.Unix(1000, 0)))
	_, err = r.Resolve(context.Background(), identity.ForUser(u.ID()))
	if err == nil {
		t.Fatal("Resolve = nil error, want a decrypt failure")
	}
	msg := err.Error()
	if strings.Contains(msg, "tok-1") {
		t.Fatalf("error %q leaks the token", msg)
	}
	if strings.Contains(msg, string(corrupted)) {
		t.Fatalf("error %q leaks the ciphertext", msg)
	}
}

func TestAStaleRegistryPointerStaysUsable(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "alice")
	mustProvider(t, s, u.ID(), "one", "tok-1")

	r := auth.NewProviderResolver(s, newTestSealer(t), http.DefaultClient, fixedClock(time.Unix(1000, 0)))
	stale, err := r.Resolve(context.Background(), identity.ForUser(u.ID()))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	r.Invalidate(u.ID())

	if _, err := r.Resolve(context.Background(), identity.ForUser(u.ID())); err != nil {
		t.Fatalf("Resolve after Invalidate: %v", err)
	}

	if len(stale.All()) != 1 || stale.All()[0].ID() != "one" {
		t.Fatalf("stale pointer All() = %+v, want exactly [one]", stale.All())
	}
}

func TestHTTPVerifierMapsAuthFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	v := auth.NewHTTPVerifier(http.DefaultClient, func() time.Time { return time.Unix(1000, 0) })
	err := v.Verify(context.Background(), config.KindGitHub, srv.URL, config.NewSecret("bad-token"))
	if err == nil {
		t.Fatal("Verify = nil error, want an unauthorized error")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("Verify error = %v (%T), want *auth.Error", err, err)
	}
	if authErr.Code != auth.CodeProviderUnauthorized {
		t.Fatalf("Verify error code = %q, want %q", authErr.Code, auth.CodeProviderUnauthorized)
	}

	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer okSrv.Close()

	if err := v.Verify(context.Background(), config.KindGitHub, okSrv.URL, config.NewSecret("good-token")); err != nil {
		t.Fatalf("Verify(200) = %v, want nil", err)
	}
}
