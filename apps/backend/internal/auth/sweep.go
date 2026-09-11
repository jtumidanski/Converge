package auth

import (
	"context"
	"log/slog"
)

// Sweep deletes expired login sessions and elapsed IP lockout counters, and
// evicts provider registries past providerResolverTTL.
//
// Called by the background sweeper loop on the same CLEANUP_INTERVAL_MINUTES
// cadence as the review-session sweeper. For rows, expiry is also enforced on
// read (see Authenticate), so a stale row is never honoured even before this
// runs (FR-3.7); that part is housekeeping, not enforcement. The registry
// eviction is different: it is the only thing that actually bounds how long a
// user's decrypted tokens stay in memory, since the read path alone would
// leave an idle user's entry resident forever.
func (s *Service) Sweep(ctx context.Context) error {
	sessions, err := s.deps.Store.DeleteExpiredLoginSessions(ctx, s.deps.Now(), s.deps.IdleTTL)
	if err != nil {
		return err
	}
	lockouts, err := s.deps.Throttle.Sweep(ctx)
	if err != nil {
		return err
	}
	// Deliberately not logged, and deliberately unconditional: the count of
	// resident provider registries is a count of users holding decrypted
	// tokens in memory, which is not something to publish to a log.
	s.deps.Resolver.EvictElapsed(s.deps.Now())
	if sessions > 0 || lockouts > 0 {
		s.deps.Log.Info("auth sweep",
			slog.Int64("login_sessions_removed", sessions),
			slog.Int64("lockouts_removed", lockouts))
	}
	return nil
}
