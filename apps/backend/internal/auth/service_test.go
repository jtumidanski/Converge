package auth_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/identity"
)

// serviceSessionTTL and serviceIdleTTL are the fixture's absolute and idle
// expiries: an idle timeout well inside the absolute one so tests can trigger
// each independently.
const (
	serviceSessionTTL = 24 * time.Hour
	serviceIdleTTL    = 2 * time.Hour
)

// stubVerifier always succeeds: Task 14 does not exercise provider
// verification, only that Service wires the collaborator through.
type stubVerifier struct{}

func (stubVerifier) Verify(context.Context, config.Kind, string, config.Secret) error { return nil }

// stubPurger records every PurgeUser call and can be told to fail.
type stubPurger struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (p *stubPurger) PurgeUser(_ context.Context, userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, userID)
	return p.err
}

func (p *stubPurger) callsSnapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

// stubUsage always reports "not in use": Task 14 does not exercise provider
// deletion, only that Service wires the collaborator through.
type stubUsage struct{}

func (stubUsage) ProviderInUse(identity.Scope, string) bool { return false }

// logRecorder is a minimal slog.Handler that records every record so tests
// can assert a level was logged, without depending on log formatting.
type logRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *logRecorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *logRecorder) WithGroup(string) slog.Handler      { return r }

func (r *logRecorder) hasLevel(level slog.Level) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.records {
		if rec.Level == level {
			return true
		}
	}
	return false
}

// serviceFixture bundles a Service with the collaborators tests need direct
// access to: the store (to inspect rows the API does not expose), the clock
// (to advance time deterministically), the sealer (to seed a provider row),
// the resolver (to observe cache invalidation), the stub purger, and the log
// recorder.
type serviceFixture struct {
	svc      *auth.Service
	store    *auth.Store
	clock    *clock
	sealer   *auth.Sealer
	resolver *auth.ProviderResolver
	purger   *stubPurger
	log      *logRecorder
}

func newService(t *testing.T) *serviceFixture {
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
		Usage:      stubUsage{},
		Log:        slog.New(rec),
		Now:        cl.now,
		SessionTTL: serviceSessionTTL,
		IdleTTL:    serviceIdleTTL,
	})
	return &serviceFixture{svc: svc, store: store, clock: cl, sealer: sealer, resolver: resolver, purger: purger, log: rec}
}

// tokenHash reproduces auth.Service's unexported hashToken, so tests can look
// a session up by its store key without a Service accessor for it.
func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// assertCode asserts err is a *auth.Error with the given code and returns it.
func assertCode(t *testing.T, err error, code auth.Code) *auth.Error {
	t.Helper()
	var ae *auth.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *auth.Error, got %v (%T)", err, err)
	}
	if ae.Code != code {
		t.Fatalf("expected code %s, got %s (%s)", code, ae.Code, ae.Message)
	}
	return ae
}

// bytesAllocatedDuring reports how many bytes fn caused to be allocated.
//
// This is the timing-oracle probe, and it replaces a wall-clock measurement:
// an Argon2id verify at m=65536 KiB must allocate its 64 MiB block array, so
// the allocation counter shows whether the hash ran, and unlike elapsed time
// it does not move when the machine is busy. TotalAlloc is process-wide, so
// callers must not be parallel tests.
func bytesAllocatedDuring(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// argonVerifyFloor is a conservative lower bound on the allocation one
// Argon2id verify at the production parameters must perform: the block array
// alone is argonMemory (64 MiB). Anything at or above this floor cannot have
// skipped the hash.
const argonVerifyFloor = 48 << 20

func TestRegisterThenLoginRoundTrip(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	user, token, err := f.svc.Register(ctx, auth.Credentials{Username: "alice", Password: "correcthorse"}, "10.0.0.1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if token == "" {
		t.Fatalf("expected a non-empty token")
	}

	scope, _, err := f.svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if scope != identity.ForUser(user.ID()) {
		t.Fatalf("expected scope for %s, got %+v", user.ID(), scope)
	}

	if err := f.svc.Logout(ctx, token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, token); assertCode(t, err, auth.CodeUnauthenticated) == nil {
		t.Fatalf("expected an error")
	}

	_, token2, err := f.svc.Login(ctx, auth.Credentials{Username: "alice", Password: "correcthorse"}, "10.0.0.1")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token2 == token {
		t.Fatalf("expected a different token on re-login")
	}
}

func TestRegisterRejectsACaseInsensitiveDuplicate(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	if _, _, err := f.svc.Register(ctx, auth.Credentials{Username: "Bob", Password: "correcthorse"}, "10.0.0.2"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, _, err := f.svc.Register(ctx, auth.Credentials{Username: "bob", Password: "correcthorse2"}, "10.0.0.2")
	assertCode(t, err, auth.CodeUsernameTaken)
}

func TestRegisterValidatesInput(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	_, _, err := f.svc.Register(ctx, auth.Credentials{Username: "ab", Password: "correcthorse"}, "10.0.0.3")
	assertCode(t, err, auth.CodeInvalidUsername)

	_, _, err = f.svc.Register(ctx, auth.Credentials{Username: "validname", Password: "short7c"}, "10.0.0.3")
	assertCode(t, err, auth.CodeWeakPassword)

	if _, _, err := f.svc.Register(ctx, auth.Credentials{Username: "validname", Password: "eightchr"}, "10.0.0.3"); err != nil {
		t.Fatalf("expected an 8-character password to succeed: %v", err)
	}
}

// TestLoginIsIndistinguishableBetweenUnknownUserAndWrongPassword is the
// timing-oracle guard for FR-2.5: an unknown username and a wrong password
// must be indistinguishable to the client, in the response *and* in the cost
// of producing it.
//
// It deliberately does not call t.Parallel(), and it deliberately measures
// allocation rather than elapsed time. The earlier form asserted a 2x ratio of
// wall-clock medians while running in parallel with other tests whose 64 MiB
// Argon2id hashes queue behind the same 4-slot semaphore; that makes the
// measurement a function of machine load, which is how it flaked under full
// suite load and never under a focused run. Allocation is a property of the
// work done, not of the scheduler: one Argon2id verify cannot happen without
// its 64 MiB block array. Within a package a sequential test has the process
// to itself, so the process-wide counter is attributable to this test.
func TestLoginIsIndistinguishableBetweenUnknownUserAndWrongPassword(t *testing.T) {
	f := newService(t)
	ctx := context.Background()

	if _, _, err := f.svc.Register(ctx, auth.Credentials{Username: "carol", Password: "correcthorse"}, "10.0.0.4"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var unknownErr, wrongErr error
	unknownAlloc := bytesAllocatedDuring(func() {
		_, _, unknownErr = f.svc.Login(ctx, auth.Credentials{Username: "nosuchuser", Password: "whatever1"}, "10.0.1.1")
	})
	wrongAlloc := bytesAllocatedDuring(func() {
		_, _, wrongErr = f.svc.Login(ctx, auth.Credentials{Username: "carol", Password: "wrongpass1"}, "10.0.1.2")
	})

	unknownAE := assertCode(t, unknownErr, auth.CodeInvalidCredentials)
	wrongAE := assertCode(t, wrongErr, auth.CodeInvalidCredentials)
	if unknownAE.Message != wrongAE.Message {
		t.Fatalf("messages differ: %q vs %q", unknownAE.Message, wrongAE.Message)
	}

	// The unknown-username path is the one that can cheat, by returning before
	// hashing anything. If it did, its allocation would fall far below one
	// Argon2id block array.
	if unknownAlloc < argonVerifyFloor {
		t.Fatalf("the unknown-username login allocated %d bytes, below the %d-byte floor for one Argon2id verify: it did not hash against the dummy, so response time discloses whether the username exists",
			unknownAlloc, argonVerifyFloor)
	}
	if wrongAlloc < argonVerifyFloor {
		t.Fatalf("the wrong-password login allocated %d bytes, below the %d-byte floor for one Argon2id verify",
			wrongAlloc, argonVerifyFloor)
	}
}

func TestLoginEngagesTheThrottle(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	if _, _, err := f.svc.Register(ctx, auth.Credentials{Username: "dave", Password: "correcthorse"}, "10.0.0.5"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var lastErr error
	for i := 0; i < 6; i++ {
		_, _, lastErr = f.svc.Login(ctx, auth.Credentials{Username: "dave", Password: "wrongpass1"}, "10.0.2.1")
	}
	ae := assertCode(t, lastErr, auth.CodeAccountLocked)
	if ae.RetryAfter <= 0 {
		t.Fatalf("expected a positive RetryAfter, got %v", ae.RetryAfter)
	}

	f.clock.advance(time.Minute)
	if _, _, err := f.svc.Login(ctx, auth.Credentials{Username: "dave", Password: "correcthorse"}, "10.0.2.1"); err != nil {
		t.Fatalf("expected the lockout to have cleared: %v", err)
	}
}

func TestRegistrationIsThrottledPerIP(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	if _, _, err := f.svc.Register(ctx, auth.Credentials{Username: "eve", Password: "correcthorse"}, "10.0.3.1"); err != nil {
		t.Fatalf("seed Register: %v", err)
	}

	for i := 0; i < 20; i++ {
		if _, _, err := f.svc.Register(ctx, auth.Credentials{Username: "eve", Password: "correcthorse"}, "10.0.3.2"); err == nil {
			t.Fatalf("expected duplicate-username failure on attempt %d", i)
		}
	}

	_, _, err := f.svc.Register(ctx, auth.Credentials{Username: "eve", Password: "correcthorse"}, "10.0.3.2")
	assertCode(t, err, auth.CodeAccountLocked)
}

func TestAuthenticateEnforcesAbsoluteExpiry(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	_, token, err := f.svc.Register(ctx, auth.Credentials{Username: "frank", Password: "correcthorse"}, "10.0.4.1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	f.clock.advance(serviceSessionTTL + time.Minute)
	if _, _, err := f.svc.Authenticate(ctx, token); assertCode(t, err, auth.CodeUnauthenticated) == nil {
		t.Fatalf("expected an error")
	}
	if _, err := f.store.LoginSession(ctx, tokenHash(token)); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expected the expired row to be gone, got %v", err)
	}
}

func TestAuthenticateEnforcesIdleExpiry(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	_, token, err := f.svc.Register(ctx, auth.Credentials{Username: "grace", Password: "correcthorse"}, "10.0.4.2")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	f.clock.advance(serviceIdleTTL + time.Minute) // past idle, well inside the absolute expiry
	if _, _, err := f.svc.Authenticate(ctx, token); assertCode(t, err, auth.CodeUnauthenticated) == nil {
		t.Fatalf("expected an error")
	}
	if _, err := f.store.LoginSession(ctx, tokenHash(token)); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expected the idle-expired row to be gone, got %v", err)
	}
}

func TestAuthenticateRateLimitsLastSeenWrites(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	_, token, err := f.svc.Register(ctx, auth.Credentials{Username: "henry", Password: "correcthorse"}, "10.0.4.3")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	hash := tokenHash(token)
	initial, err := f.store.LoginSession(ctx, hash)
	if err != nil {
		t.Fatalf("LoginSession: %v", err)
	}

	f.clock.advance(time.Minute)
	if _, _, err := f.svc.Authenticate(ctx, token); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	f.clock.advance(3 * time.Minute)
	if _, _, err := f.svc.Authenticate(ctx, token); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	within, err := f.store.LoginSession(ctx, hash)
	if err != nil {
		t.Fatalf("LoginSession: %v", err)
	}
	if !within.LastSeenAt().Equal(initial.LastSeenAt()) {
		t.Fatalf("expected LastSeenAt unchanged within the 5-minute window, got %v want %v", within.LastSeenAt(), initial.LastSeenAt())
	}

	f.clock.advance(6 * time.Minute)
	if _, _, err := f.svc.Authenticate(ctx, token); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	after, err := f.store.LoginSession(ctx, hash)
	if err != nil {
		t.Fatalf("LoginSession: %v", err)
	}
	if !after.LastSeenAt().After(initial.LastSeenAt()) {
		t.Fatalf("expected LastSeenAt to advance once past the 5-minute window, got %v", after.LastSeenAt())
	}
}

func TestChangePasswordRevokesOtherSessionsButNotTheCaller(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	user, token1, err := f.svc.Register(ctx, auth.Credentials{Username: "irene", Password: "correcthorse"}, "10.0.5.1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, token2, err := f.svc.Login(ctx, auth.Credentials{Username: "irene", Password: "correcthorse"}, "10.0.5.2")
	if err != nil {
		t.Fatalf("Login (session 2): %v", err)
	}
	_, token3, err := f.svc.Login(ctx, auth.Credentials{Username: "irene", Password: "correcthorse"}, "10.0.5.3")
	if err != nil {
		t.Fatalf("Login (session 3): %v", err)
	}

	if err := f.svc.ChangePassword(ctx, user.ID(), tokenHash(token1), "correcthorse", "newpassword1"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if _, _, err := f.svc.Authenticate(ctx, token1); err != nil {
		t.Fatalf("expected the calling session to survive: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, token2); err == nil {
		t.Fatalf("expected session 2 to be revoked")
	}
	if _, _, err := f.svc.Authenticate(ctx, token3); err == nil {
		t.Fatalf("expected session 3 to be revoked")
	}
	if _, _, err := f.svc.Login(ctx, auth.Credentials{Username: "irene", Password: "correcthorse"}, "10.0.5.9"); err == nil {
		t.Fatalf("expected the old password to be rejected")
	}
	if _, _, err := f.svc.Login(ctx, auth.Credentials{Username: "irene", Password: "newpassword1"}, "10.0.5.9"); err != nil {
		t.Fatalf("expected the new password to succeed: %v", err)
	}
}

func TestChangePasswordRequiresTheCurrentPassword(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	user, token, err := f.svc.Register(ctx, auth.Credentials{Username: "jack", Password: "correcthorse"}, "10.0.6.1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	keep := tokenHash(token)

	err = f.svc.ChangePassword(ctx, user.ID(), keep, "wrongcurrent1", "newpassword1")
	assertCode(t, err, auth.CodeInvalidCredentials)

	before, err := f.store.UserByID(ctx, user.ID())
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}

	err = f.svc.ChangePassword(ctx, user.ID(), keep, "correcthorse", "short7c")
	assertCode(t, err, auth.CodeWeakPassword)

	after, err := f.store.UserByID(ctx, user.ID())
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if before.PasswordHash() != after.PasswordHash() {
		t.Fatalf("expected the stored hash to be unchanged")
	}
}

func TestDeleteAccountRemovesRowsAndCallsThePurger(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	user, token1, err := f.svc.Register(ctx, auth.Credentials{Username: "karen", Password: "correcthorse"}, "10.0.7.1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, token2, err := f.svc.Login(ctx, auth.Credentials{Username: "karen", Password: "correcthorse"}, "10.0.7.2")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	ciphertext, nonce, err := f.sealer.Seal("provider-token-plaintext", user.ID(), "provider-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	up, err := auth.NewUserProvider("provider-1", user.ID(), "myslug", "My GitHub", config.KindGitHub, "https://api.github.com", ciphertext, nonce, "abcd", f.clock.now())
	if err != nil {
		t.Fatalf("NewUserProvider: %v", err)
	}
	if err := f.store.CreateUserProvider(ctx, up); err != nil {
		t.Fatalf("CreateUserProvider: %v", err)
	}

	regBefore, err := f.resolver.Resolve(ctx, identity.ForUser(user.ID()))
	if err != nil {
		t.Fatalf("Resolve before delete: %v", err)
	}
	if _, ok := regBefore.Get("myslug"); !ok {
		t.Fatalf("expected the provider registered before delete")
	}

	if err := f.svc.DeleteAccount(ctx, user.ID(), "correcthorse"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}

	if _, _, err := f.svc.Authenticate(ctx, token1); err == nil {
		t.Fatalf("expected session 1 to no longer authenticate")
	}
	if _, _, err := f.svc.Authenticate(ctx, token2); err == nil {
		t.Fatalf("expected session 2 to no longer authenticate")
	}
	if _, err := f.store.UserByID(ctx, user.ID()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expected the user row to be gone, got %v", err)
	}
	providers, err := f.store.ListUserProviders(ctx, user.ID())
	if err != nil {
		t.Fatalf("ListUserProviders: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("expected the provider rows to be gone, got %d", len(providers))
	}

	regAfter, err := f.resolver.Resolve(ctx, identity.ForUser(user.ID()))
	if err != nil {
		t.Fatalf("Resolve after delete: %v", err)
	}
	if _, ok := regAfter.Get("myslug"); ok {
		t.Fatalf("expected Invalidate to have dropped the cached registry")
	}

	if calls := f.purger.callsSnapshot(); len(calls) != 1 || calls[0] != user.ID() {
		t.Fatalf("expected the purger to be called once with %s, got %v", user.ID(), calls)
	}
}

func TestDeleteAccountRequiresThePassword(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "leo", Password: "correcthorse"}, "10.0.8.1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = f.svc.DeleteAccount(ctx, user.ID(), "wrongpassword1")
	assertCode(t, err, auth.CodeInvalidCredentials)

	if _, err := f.store.UserByID(ctx, user.ID()); err != nil {
		t.Fatalf("expected the user to remain, got %v", err)
	}
	if calls := f.purger.callsSnapshot(); len(calls) != 0 {
		t.Fatalf("expected the purger not to be called, got %v", calls)
	}
}

func TestDeleteAccountSucceedsEvenWhenThePurgerFails(t *testing.T) {
	t.Parallel()
	f := newService(t)
	f.purger.err = errors.New("purge boom")
	ctx := context.Background()

	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "mia", Password: "correcthorse"}, "10.0.9.1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := f.svc.DeleteAccount(ctx, user.ID(), "correcthorse"); err != nil {
		t.Fatalf("expected DeleteAccount to succeed despite the purge failure: %v", err)
	}
	if _, err := f.store.UserByID(ctx, user.ID()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("expected the user row to be gone, got %v", err)
	}
	if !f.log.hasLevel(slog.LevelError) {
		t.Fatalf("expected an ERROR log record for the failed purge")
	}
}

// TestConcurrentLoginAndProviderListDoNotDeadlock is design §12's named risk:
// it proves Argon2id never runs while the single database connection is held
// by a transaction. Service does not yet expose a provider list method (that
// arrives in Task 15), so the alternating operation is Store.ListUserProviders
// directly — the same single-connection read Task 15's ListProviders will
// wrap.
func TestConcurrentLoginAndProviderListDoNotDeadlock(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	user, _, err := f.svc.Register(ctx, auth.Credentials{Username: "nina", Password: "correcthorse"}, "10.0.10.1")
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
				_, _, loginErr := f.svc.Login(ctx, auth.Credentials{Username: "nina", Password: "correcthorse"}, fmt.Sprintf("10.0.11.%d", i))
				errs <- loginErr
				return
			}
			_, listErr := f.store.ListUserProviders(ctx, user.ID())
			errs <- listErr
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent login/list: %v", err)
		}
	}
}
