package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/jtumidanski/converge/internal/identity"
	"github.com/jtumidanski/converge/internal/provider"
)

// providerResolverTTL bounds how long a user's decrypted tokens sit in
// memory. Two things enforce that bound together: Resolve refuses to serve an
// entry older than the TTL, and EvictElapsed — called from the background
// sweeper — deletes it from the map, so an idle user's tokens do not stay
// resident for the lifetime of the process. Tokens are decrypted at
// registry-build time and live inside
// config.Secret values held by the github/gitlab clients — the same place
// standalone mode keeps them. Decrypting per call instead buys little (the
// plaintext still reaches GIT_CONFIG_VALUE_0 and an HTTP header) and costs a
// database read on every provider operation, so we take the cache and bound
// it with this TTL (design §4).
const providerResolverTTL = 15 * time.Minute

// ProviderResolver builds a *provider.Registry from a user's stored
// user_providers rows, decrypting each token on the way.
//
// It lives here rather than in internal/provider because it needs the
// database and the decryption key, and internal/provider must gain neither.
// It satisfies provider.Resolver from the outside.
//
// Cache invalidation is complete because the cache is per-process and this
// process is the only writer of the database — there is no second node to
// miss the message. That assumption would need revisiting for a
// multi-replica deployment (design §12).
type ProviderResolver struct {
	store  *Store
	sealer *Sealer
	client *http.Client
	now    func() time.Time

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	registry *provider.Registry
	builtAt  time.Time
}

func NewProviderResolver(store *Store, sealer *Sealer, client *http.Client, now func() time.Time) *ProviderResolver {
	return &ProviderResolver{
		store:  store,
		sealer: sealer,
		client: client,
		now:    now,
		cache:  map[string]cacheEntry{},
	}
}

var _ provider.Resolver = (*ProviderResolver)(nil)

// Resolve returns the registry visible to scope, building it on a cache miss.
func (r *ProviderResolver) Resolve(ctx context.Context, scope identity.Scope) (*provider.Registry, error) {
	userID := scope.UserID()
	if userID == "" {
		// Hosted mode always resolves on behalf of an authenticated user.
		// An unscoped resolve is a wiring bug; failing loudly beats
		// silently answering with nothing or with everything.
		return nil, errors.New("auth: provider resolver requires a scoped identity")
	}
	now := r.now()
	r.mu.RLock()
	entry, ok := r.cache[userID]
	r.mu.RUnlock()
	if ok && now.Sub(entry.builtAt) < providerResolverTTL {
		return entry.registry, nil
	}
	registry, err := r.build(ctx, userID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cache[userID] = cacheEntry{registry: registry, builtAt: now}
	r.mu.Unlock()
	return registry, nil
}

// Invalidate drops a user's cached registry. Every write path calls it:
// provider create, update, delete, and account deletion.
func (r *ProviderResolver) Invalidate(userID string) {
	r.mu.Lock()
	delete(r.cache, userID)
	r.mu.Unlock()
}

// EvictElapsed deletes every cache entry built more than providerResolverTTL
// ago. This is what makes the TTL a bound on secret residency rather than only
// a freshness check on the read path: a user who logs in once and goes away
// has their decrypted tokens dropped on the next sweep instead of held until
// the process exits.
//
// Dropping an entry is never incorrect, only wasteful — Resolve rebuilds from
// the database on a miss — so there is no coordination with in-flight
// callers: a caller that already holds the *provider.Registry keeps using it,
// which is the same lifetime an entry returned just before its TTL elapsed
// already had.
func (r *ProviderResolver) EvictElapsed(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for userID, entry := range r.cache {
		if now.Sub(entry.builtAt) >= providerResolverTTL {
			delete(r.cache, userID)
		}
	}
}

// build decrypts each configuration and registers a client under its slug, so
// the slug is the provider id the rest of the system sees — which is what
// keeps GET /api/providers and /api/providers/{provider}/... unchanged in
// shape (FR-5.8).
func (r *ProviderResolver) build(ctx context.Context, userID string) (*provider.Registry, error) {
	rows, err := r.store.ListUserProviders(ctx, userID)
	if err != nil {
		return nil, err
	}
	registry := provider.NewRegistry()
	for _, row := range rows {
		token, err := r.sealer.Open(row.TokenCiphertext(), row.TokenNonce(), row.UserID(), row.ID())
		if err != nil {
			return nil, fmt.Errorf("auth: provider %s: %w", row.Slug(), err)
		}
		p, err := buildClient(row.Slug(), row.DisplayName(), row.Kind(), row.BaseURL(), token, r.client, r.now)
		if err != nil {
			return nil, err
		}
		if err := registry.Register(p); err != nil {
			return nil, fmt.Errorf("auth: register provider %s: %w", row.Slug(), err)
		}
	}
	return registry, nil
}
