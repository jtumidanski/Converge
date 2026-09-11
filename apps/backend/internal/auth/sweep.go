package auth

import (
	"context"
	"log/slog"
)

// Sweep deletes expired login sessions and elapsed lockout counters.
//
// Called by the background sweeper loop on the same CLEANUP_INTERVAL_MINUTES
// cadence as the review-session sweeper. Expiry is also enforced on read (see
// Authenticate), so a stale row is never honoured even before this runs
// (FR-3.7); this is housekeeping, not enforcement.
func (s *Service) Sweep(ctx context.Context) error {
	sessions, err := s.deps.Store.DeleteExpiredLoginSessions(ctx, s.deps.Now(), s.deps.IdleTTL)
	if err != nil {
		return err
	}
	lockouts, err := s.deps.Throttle.Sweep(ctx)
	if err != nil {
		return err
	}
	if sessions > 0 || lockouts > 0 {
		s.deps.Log.Info("auth sweep",
			slog.Int64("login_sessions_removed", sessions),
			slog.Int64("lockouts_removed", lockouts))
	}
	return nil
}
