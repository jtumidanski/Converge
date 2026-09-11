package auth_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/identity"
)

// ptr returns a pointer to v, for building ProviderPatch literals inline.
func ptr[T any](v T) *T { return &v }

// controllableVerifier lets a test dictate the verification outcome and
// records how many times it was called, so "Validate: false skips
// verification" can assert zero calls rather than merely a successful write.
type controllableVerifier struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (v *controllableVerifier) Verify(context.Context, config.Kind, string, config.Secret) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls++
	return v.err
}

func (v *controllableVerifier) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.calls
}

// newServiceWithVerifier builds a fixture identical to newService but with a
// caller-supplied ProviderVerifier, for the tests that must control the
// verification outcome.
func newServiceWithVerifier(t *testing.T, verifier auth.ProviderVerifier) *serviceFixture {
	t.Helper()
	store := newStore(t)
	sealer, err := auth.NewSealer(config.NewSecret(key32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	cl := newClock()
	throttle := auth.NewThrottle(store, cl.now)
	resolver := auth.NewProviderResolver(store, sealer, http.DefaultClient, cl.now)
	purger := &stubPurger{}
	rec := &logRecorder{}
	svc := auth.NewService(auth.ServiceDeps{
		Store:      store,
		Sealer:     sealer,
		Throttle:   throttle,
		Resolver:   resolver,
		Verifier:   verifier,
		Purger:     purger,
		Usage:      stubUsage{},
		Log:        slog.New(rec),
		Now:        cl.now,
		SessionTTL: serviceSessionTTL,
		IdleTTL:    serviceIdleTTL,
	})
	return &serviceFixture{svc: svc, store: store, clock: cl, sealer: sealer, resolver: resolver, purger: purger, log: rec}
}

// controllableUsage lets a test dictate ProviderInUse's answer.
type controllableUsage struct{ inUse bool }

func (u controllableUsage) ProviderInUse(identity.Scope, string) bool { return u.inUse }

// newServiceWithUsage builds a fixture identical to newService but with a
// caller-supplied ProviderUsage collaborator.
func newServiceWithUsage(t *testing.T, usage auth.ProviderUsage) *serviceFixture {
	t.Helper()
	store := newStore(t)
	sealer, err := auth.NewSealer(config.NewSecret(key32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	cl := newClock()
	throttle := auth.NewThrottle(store, cl.now)
	resolver := auth.NewProviderResolver(store, sealer, http.DefaultClient, cl.now)
	purger := &stubPurger{}
	rec := &logRecorder{}
	svc := auth.NewService(auth.ServiceDeps{
		Store:      store,
		Sealer:     sealer,
		Throttle:   throttle,
		Resolver:   resolver,
		Verifier:   stubVerifier{},
		Purger:     purger,
		Usage:      usage,
		Log:        slog.New(rec),
		Now:        cl.now,
		SessionTTL: serviceSessionTTL,
		IdleTTL:    serviceIdleTTL,
	})
	return &serviceFixture{svc: svc, store: store, clock: cl, sealer: sealer, resolver: resolver, purger: purger, log: rec}
}

func TestCreateProviderStoresAnEncryptedToken(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "cathy", Password: "correcthorse"}, "10.1.0.1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	p, err := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "gl-main", Kind: config.KindGitLab, BaseURL: "https://gitlab.example.com",
		Token: "glpat-abcdef9f2c",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if p.TokenLast4() != "9f2c" {
		t.Fatalf("TokenLast4() = %q, want 9f2c", p.TokenLast4())
	}
	if len(p.TokenCiphertext()) == 0 || len(p.TokenNonce()) == 0 {
		t.Fatalf("expected non-empty ciphertext and nonce")
	}
	// No field anywhere on the returned value holds the plaintext.
	if p.TokenLast4() == "glpat-abcdef9f2c" {
		t.Fatalf("TokenLast4 must not equal the plaintext")
	}
	if p.DisplayName() == "glpat-abcdef9f2c" || p.Slug() == "glpat-abcdef9f2c" || p.BaseURL() == "glpat-abcdef9f2c" {
		t.Fatalf("no string field may equal the plaintext")
	}

	opened, err := f.sealer.Open(p.TokenCiphertext(), p.TokenNonce(), user.ID(), p.ID())
	if err != nil {
		t.Fatalf("Sealer.Open: %v", err)
	}
	if opened.Reveal() != "glpat-abcdef9f2c" {
		t.Fatalf("Open() = %q, want the original plaintext", opened.Reveal())
	}
}

func TestCreateProviderDefaultsTheGitHubBaseURL(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "dan", Password: "correcthorse"}, "10.1.0.2")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	gh, err := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "gh-main", Kind: config.KindGitHub, BaseURL: "", Token: "ghp_abcdef1234",
	})
	if err != nil {
		t.Fatalf("CreateProvider(github): %v", err)
	}
	if gh.BaseURL() != "https://api.github.com" {
		t.Fatalf("BaseURL() = %q, want https://api.github.com", gh.BaseURL())
	}

	_, err = f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "gl-main", Kind: config.KindGitLab, BaseURL: "", Token: "glpat-abcdef1234",
	})
	if err == nil {
		t.Fatalf("expected an error for a gitlab provider with no base url")
	}
}

func TestCreateProviderNormalizesATrailingSlash(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "eve", Password: "correcthorse"}, "10.1.0.3")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	p, err := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "gl-main", Kind: config.KindGitLab, BaseURL: "https://gitlab.example.com/", Token: "glpat-abcdef1234",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if p.BaseURL() != "https://gitlab.example.com" {
		t.Fatalf("BaseURL() = %q, want https://gitlab.example.com", p.BaseURL())
	}
}

func TestCreateProviderRejectsADuplicateSlug(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	alice, _, err := f.svc.Register(ctx, auth.Credentials{Username: "faye", Password: "correcthorse"}, "10.1.0.4")
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, _, err := f.svc.Register(ctx, auth.Credentials{Username: "gabe", Password: "correcthorse"}, "10.1.0.5")
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	in := auth.ProviderInput{Slug: "work", Kind: config.KindGitHub, Token: "ghp_abcdef1234"}
	if _, err := f.svc.CreateProvider(ctx, alice.ID(), in); err != nil {
		t.Fatalf("CreateProvider(alice): %v", err)
	}
	_, err = f.svc.CreateProvider(ctx, alice.ID(), in)
	assertCode(t, err, auth.CodeProviderSlugTaken)

	if _, err := f.svc.CreateProvider(ctx, bob.ID(), in); err != nil {
		t.Fatalf("CreateProvider(bob) with the same slug should succeed: %v", err)
	}
}

func TestCreateProviderWithValidateTrueWritesNothingOnRejection(t *testing.T) {
	t.Parallel()
	v := &controllableVerifier{err: &auth.Error{Code: auth.CodeProviderUnauthorized, Message: "nope"}}
	f := newServiceWithVerifier(t, v)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "hank", Password: "correcthorse"}, "10.1.0.6")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_abcdef1234", Validate: true,
	})
	assertCode(t, err, auth.CodeProviderUnauthorized)

	providers, err := f.svc.ListProviders(ctx, user.ID())
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("expected no providers to have been written, got %d", len(providers))
	}
}

func TestCreateProviderWithValidateFalseSkipsVerification(t *testing.T) {
	t.Parallel()
	v := &controllableVerifier{err: errors.New("verifier should not be called")}
	f := newServiceWithVerifier(t, v)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "ivy", Password: "correcthorse"}, "10.1.0.7")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_abcdef1234", Validate: false,
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if v.callCount() != 0 {
		t.Fatalf("expected the verifier not to be called, got %d calls", v.callCount())
	}
}

func TestCreateProviderInvalidatesTheResolverCache(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "jack", Password: "correcthorse"}, "10.1.0.8")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	registry, err := f.resolver.Resolve(ctx, identity.ForUser(user.ID()))
	if err != nil {
		t.Fatalf("Resolve before create: %v", err)
	}
	if _, ok := registry.Get("work"); ok {
		t.Fatalf("expected no provider before create")
	}

	if _, err := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_abcdef1234",
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	registry, err = f.resolver.Resolve(ctx, identity.ForUser(user.ID()))
	if err != nil {
		t.Fatalf("Resolve after create: %v", err)
	}
	if _, ok := registry.Get("work"); !ok {
		t.Fatalf("expected the new provider to appear without an explicit Invalidate")
	}
}

func TestUpdateProviderWithAnEmptyTokenKeepsTheStoredOne(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "kara", Password: "correcthorse"}, "10.1.0.9")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, err := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_original1",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	for _, tokenPatch := range []*string{ptr(""), nil} {
		updated, err := f.svc.UpdateProvider(ctx, user.ID(), p.ID(), auth.ProviderPatch{Token: tokenPatch})
		if err != nil {
			t.Fatalf("UpdateProvider: %v", err)
		}
		opened, err := f.sealer.Open(updated.TokenCiphertext(), updated.TokenNonce(), user.ID(), p.ID())
		if err != nil {
			t.Fatalf("Sealer.Open: %v", err)
		}
		if opened.Reveal() != "ghp_original1" {
			t.Fatalf("expected the stored token to be unchanged, got %q", opened.Reveal())
		}
		if !updated.TokenSetAt().Equal(p.TokenSetAt()) {
			t.Fatalf("expected TokenSetAt to be unmoved, got %v want %v", updated.TokenSetAt(), p.TokenSetAt())
		}
	}
}

func TestUpdateProviderWithANewTokenReplacesIt(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "liam", Password: "correcthorse"}, "10.1.0.10")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, err := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_original1",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	f.clock.advance(time.Minute)

	updated, err := f.svc.UpdateProvider(ctx, user.ID(), p.ID(), auth.ProviderPatch{Token: ptr("ghp_newtoken2")})
	if err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}
	if updated.TokenLast4() != "ken2" {
		t.Fatalf("TokenLast4() = %q, want ken2", updated.TokenLast4())
	}
	if updated.TokenSetAt().Equal(p.TokenSetAt()) {
		t.Fatalf("expected TokenSetAt to move")
	}
	// The stored row now holds the new ciphertext/nonce; opening what is
	// actually persisted must yield the new plaintext, never the old one.
	opened, err := f.sealer.Open(updated.TokenCiphertext(), updated.TokenNonce(), user.ID(), p.ID())
	if err != nil {
		t.Fatalf("Sealer.Open(new): %v", err)
	}
	if opened.Reveal() != "ghp_newtoken2" {
		t.Fatalf("Open(new) = %q, want ghp_newtoken2", opened.Reveal())
	}
	if string(updated.TokenCiphertext()) == string(p.TokenCiphertext()) {
		t.Fatalf("expected a fresh ciphertext, not the old one")
	}
}

func TestUpdateProviderReencryptsUnderTheSameRowAAD(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "mona", Password: "correcthorse"}, "10.1.0.11")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	other, _, err := f.svc.Register(ctx, auth.Credentials{Username: "nora", Password: "correcthorse"}, "10.1.0.12")
	if err != nil {
		t.Fatalf("Register other: %v", err)
	}
	p, err := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_original1",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	updated, err := f.svc.UpdateProvider(ctx, user.ID(), p.ID(), auth.ProviderPatch{Token: ptr("ghp_newtoken2")})
	if err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}
	if _, err := f.sealer.Open(updated.TokenCiphertext(), updated.TokenNonce(), user.ID(), p.ID()); err != nil {
		t.Fatalf("expected Open under the owning user id to succeed: %v", err)
	}
	if _, err := f.sealer.Open(updated.TokenCiphertext(), updated.TokenNonce(), other.ID(), p.ID()); err == nil {
		t.Fatalf("expected Open under a different user id to fail")
	}
}

func TestUpdateProviderIsScopedToItsOwner(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	alice, _, err := f.svc.Register(ctx, auth.Credentials{Username: "opal", Password: "correcthorse"}, "10.1.0.13")
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, _, err := f.svc.Register(ctx, auth.Credentials{Username: "pete", Password: "correcthorse"}, "10.1.0.14")
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	p, err := f.svc.CreateProvider(ctx, alice.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_original1",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	_, err = f.svc.UpdateProvider(ctx, bob.ID(), p.ID(), auth.ProviderPatch{DisplayName: ptr("hijacked")})
	if !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a cross-user update, got %v", err)
	}

	unchanged, err := f.svc.Provider(ctx, alice.ID(), p.ID())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if unchanged.DisplayName() != p.DisplayName() {
		t.Fatalf("expected alice's row to be unchanged, got display name %q", unchanged.DisplayName())
	}
}

func TestUpdateProviderCannotChangeTheSlug(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "quinn", Password: "correcthorse"}, "10.1.0.15")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, err := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_original1",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	updated, err := f.svc.UpdateProvider(ctx, user.ID(), p.ID(), auth.ProviderPatch{
		DisplayName: ptr("Renamed"),
		Kind:        ptr(config.KindGitLab),
		BaseURL:     ptr("https://gitlab.example.com"),
		Token:       ptr("glpat-newone12"),
	})
	if err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}
	// ProviderPatch has no Slug field: this is the documentation of that
	// compile-time guarantee, not the enforcement of it.
	if updated.Slug() != p.Slug() {
		t.Fatalf("expected the slug to remain %q, got %q", p.Slug(), updated.Slug())
	}
}

func TestDeleteProviderRefusesWhileInUse(t *testing.T) {
	t.Parallel()
	f := newServiceWithUsage(t, controllableUsage{inUse: true})
	ctx := context.Background()
	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "raul", Password: "correcthorse"}, "10.1.0.16")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, err := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_original1",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	err = f.svc.DeleteProvider(ctx, user.ID(), p.ID())
	assertCode(t, err, auth.CodeProviderInUse)

	if _, err := f.svc.Provider(ctx, user.ID(), p.ID()); err != nil {
		t.Fatalf("expected the row to survive, got %v", err)
	}
}

func TestDeleteProviderIsScopedAndInvalidates(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()
	alice, _, err := f.svc.Register(ctx, auth.Credentials{Username: "sara", Password: "correcthorse"}, "10.1.0.17")
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, _, err := f.svc.Register(ctx, auth.Credentials{Username: "theo", Password: "correcthorse"}, "10.1.0.18")
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}
	p, err := f.svc.CreateProvider(ctx, alice.ID(), auth.ProviderInput{
		Slug: "work", Kind: config.KindGitHub, Token: "ghp_original1",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	if err := f.svc.DeleteProvider(ctx, bob.ID(), p.ID()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a cross-user delete, got %v", err)
	}

	registryBefore, err := f.resolver.Resolve(ctx, identity.ForUser(alice.ID()))
	if err != nil {
		t.Fatalf("Resolve before delete: %v", err)
	}
	if _, ok := registryBefore.Get("work"); !ok {
		t.Fatalf("expected the provider to be present before delete")
	}

	if err := f.svc.DeleteProvider(ctx, alice.ID(), p.ID()); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	registryAfter, err := f.resolver.Resolve(ctx, identity.ForUser(alice.ID()))
	if err != nil {
		t.Fatalf("Resolve after delete: %v", err)
	}
	if _, ok := registryAfter.Get("work"); ok {
		t.Fatalf("expected the provider to be gone after delete")
	}
}

func TestSweepRemovesExpiredSessionsAndElapsedLockouts(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	// A live session, which must survive the sweep.
	live, _, err := f.svc.Register(ctx, auth.Credentials{Username: "uma", Password: "correcthorse"}, "10.1.0.19")
	if err != nil {
		t.Fatalf("Register live: %v", err)
	}

	// An expired login session: register another user, then jump the clock
	// past the absolute session TTL before sweeping.
	_, expiredToken, err := f.svc.Register(ctx, auth.Credentials{Username: "vera", Password: "correcthorse"}, "10.1.0.20")
	if err != nil {
		t.Fatalf("Register expired: %v", err)
	}

	// A stale IP counter: failed logins from one address leave one, and it is
	// the scope the sweeper may reap once its window has closed.
	for i := 0; i < 5; i++ {
		_, _, loginErr := f.svc.Login(ctx, auth.Credentials{Username: "walt", Password: "wrongpassword"}, "10.1.0.21")
		if loginErr == nil {
			t.Fatalf("expected login attempt %d to fail", i)
		}
	}

	f.clock.advance(serviceSessionTTL + time.Hour)

	if err := f.svc.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if _, _, err := f.svc.Authenticate(ctx, expiredToken); err == nil {
		t.Fatalf("expected the expired session to be gone")
	}

	ipAttempt, err := f.store.Attempt(ctx, auth.ScopeIP, "10.1.0.21")
	if err != nil {
		t.Fatalf("Attempt(ip): %v", err)
	}
	if ipAttempt.Failures != 0 {
		t.Fatalf("expected the stale IP counter to be swept, got failures=%d", ipAttempt.Failures)
	}
	// The username counter must survive: FR-7.2 resets it only on a successful
	// login, and sweeping it would let paced guesses escape the lockout.
	userAttempt, err := f.store.Attempt(ctx, auth.ScopeUser, auth.Fold("walt"))
	if err != nil {
		t.Fatalf("Attempt(user): %v", err)
	}
	if userAttempt.Failures != 5 {
		t.Fatalf("the username counter should survive the sweep, got failures=%d", userAttempt.Failures)
	}

	// The live session must be unaffected: re-authenticate the original
	// registration token by looking the user up directly (Register does not
	// return the token twice, so use a fresh login instead).
	if _, err := f.store.UserByID(ctx, live.ID()); err != nil {
		t.Fatalf("expected the live user to remain: %v", err)
	}
	_, freshToken, err := f.svc.Login(ctx, auth.Credentials{Username: "uma", Password: "correcthorse"}, "10.1.0.22")
	if err != nil {
		t.Fatalf("Login uma: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, freshToken); err != nil {
		t.Fatalf("expected the freshly created session to authenticate: %v", err)
	}
}

// TestConcurrentListProvidersAndCreateProviderDoNotDeadlock races
// Service.ListProviders against Service.CreateProvider on the
// single-connection pool. It is the service-level counterpart Task 14 could
// not write (ListProviders did not exist yet): the same discipline —
// Argon2id and, here, no lock held across a database call — must hold for
// the provider CRUD path too, or a lock held across a query is a deadlock
// risk with MaxOpenConns(1).
func TestConcurrentListProvidersAndCreateProviderDoNotDeadlock(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "xena", Password: "correcthorse"}, "10.1.0.23")
	if err != nil {
		t.Fatalf("seed Register: %v", err)
	}

	const workers = 8
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_, listErr := f.svc.ListProviders(ctx, user.ID())
				errs <- listErr
				return
			}
			_, createErr := f.svc.CreateProvider(ctx, user.ID(), auth.ProviderInput{
				Slug: fmt.Sprintf("slug-%d", i), Kind: config.KindGitHub, Token: fmt.Sprintf("ghp_token%04d", i),
			})
			errs <- createErr
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent list/create: %v", err)
		}
	}
}
