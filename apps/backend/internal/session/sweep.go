package session

import (
	"context"
	"log/slog"
	"time"
)

// Sweep expires and cleans every active session past its TTL, and retries
// Cleanup for any session that is already terminal but whose prior Cleanup
// attempt failed (see persistCleanupFailure/retryCleanup). Retries are
// bounded by maxCleanupRetries, so a permanently failing Cleanup cannot make
// Sweep spin or make the store's tracked state grow without limit.
func (s *Store) Sweep(ctx context.Context) {
	now := s.now()
	s.mu.RLock()
	var due []Session
	for _, sess := range s.index {
		if sess.IsActive() && sess.IsExpired(now) {
			due = append(due, sess)
		}
	}
	var retry []Session
	for id := range s.pendingCleanup {
		if sess, ok := s.index[id]; ok {
			retry = append(retry, sess)
		}
	}
	s.mu.RUnlock()
	for _, sess := range due {
		s.log.Info("expiring session", slog.String("session", sess.ID()))
		s.expire(ctx, sess)
	}
	for _, sess := range retry {
		s.retryCleanup(ctx, sess)
	}
}

// RunSweeper sweeps every interval until ctx is cancelled.
func (s *Store) RunSweeper(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}
