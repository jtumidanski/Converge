package auth

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/db"
	"github.com/jtumidanski/converge/internal/provider"
)

// This file is in package auth rather than auth_test because the property
// under test is residency in an unexported map: that an elapsed entry is
// *gone*, not merely unused. Asserting it through the exported surface would
// only show that Resolve rebuilds, which it already did before the fix.

func newResolverTestStore(t *testing.T) *Store {
	t.Helper()
	handle, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "converge.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return NewStore(handle)
}

// TestSweepEvictsProviderRegistriesPastTheirTTL is the regression guard for
// the secret-residency bound providerResolverTTL documents.
//
// Before the fix the TTL was consulted only on the read path, and the only
// deletion was Invalidate. An entry whose TTL had elapsed was therefore never
// removed: a user who logged in once left decrypted provider tokens resident
// in the cache map for the lifetime of the process.
func TestSweepEvictsProviderRegistriesPastTheirTTL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newResolverTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time { return now }

	resolver := NewProviderResolver(store, nil, nil, clock)
	resolver.cache["elapsed-user"] = cacheEntry{
		registry: provider.NewRegistry(),
		builtAt:  now.Add(-providerResolverTTL - time.Second),
	}
	resolver.cache["fresh-user"] = cacheEntry{
		registry: provider.NewRegistry(),
		builtAt:  now.Add(-time.Minute),
	}

	svc := NewService(ServiceDeps{
		Store:      store,
		Throttle:   NewThrottle(store, clock),
		Resolver:   resolver,
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:        clock,
		SessionTTL: time.Hour,
		IdleTTL:    time.Hour,
	})
	if err := svc.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	resolver.mu.RLock()
	defer resolver.mu.RUnlock()
	if _, ok := resolver.cache["elapsed-user"]; ok {
		t.Fatal("an entry past providerResolverTTL is still resident in the cache; the documented bound on how long decrypted tokens sit in memory is not enforced")
	}
	if _, ok := resolver.cache["fresh-user"]; !ok {
		t.Fatal("eviction removed an entry still inside its TTL")
	}
}
