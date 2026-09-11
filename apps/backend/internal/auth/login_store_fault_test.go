package auth_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/db"
)

// closedStore returns a Store whose handle is already closed, so every
// statement it issues fails. It is the fault injector for the login path:
// Service holds the user store and the Throttle's store as separate
// collaborators, so a test can keep the throttle healthy and break only the
// user lookup.
func closedStore(t *testing.T) *auth.Store {
	t.Helper()
	handle, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "converge.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return auth.NewStore(handle)
}

// TestLoginReportsAStoreFaultRatherThanWrongCredentials pins the distinction
// between "these credentials did not match" and "we could not check them". A
// database outage answered as 401 INVALID_CREDENTIALS tells the user their
// password is wrong, which is both false and unactionable; it must surface as
// a server fault instead.
func TestLoginReportsAStoreFaultRatherThanWrongCredentials(t *testing.T) {
	t.Parallel()
	healthy := newStore(t)
	cl := newClock()
	svc := auth.NewService(auth.ServiceDeps{
		Store:      closedStore(t),
		Throttle:   auth.NewThrottle(healthy, cl.now),
		Verifier:   stubVerifier{},
		Purger:     &stubPurger{},
		Usage:      stubUsage{},
		Log:        slog.New(&logRecorder{}),
		Now:        cl.now,
		SessionTTL: serviceSessionTTL,
		IdleTTL:    serviceIdleTTL,
	})

	_, _, err := svc.Login(context.Background(), auth.Credentials{Username: "grace", Password: "correcthorse"}, "10.0.8.1")
	if err == nil {
		t.Fatal("expected Login to fail when the user store is unreachable")
	}
	var ae *auth.Error
	if errors.As(err, &ae) {
		t.Fatalf("an unreachable user store was reported to the caller as %s (%q); a database outage is not a wrong password",
			ae.Code, ae.Message)
	}

	// An outage must not spend the innocent user's failure budget either.
	a, err := healthy.Attempt(context.Background(), auth.ScopeUser, auth.Fold("grace"))
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if a.Failures != 0 {
		t.Fatalf("a store fault counted %d failures against the username", a.Failures)
	}
}
