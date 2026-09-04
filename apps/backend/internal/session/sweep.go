package session

import (
	"context"
	"log/slog"
	"time"
)

// Sweep expires and cleans every active session past its TTL.
func (s *Store) Sweep(ctx context.Context) {
	now := s.now()
	s.mu.RLock()
	var due []Session
	for _, sess := range s.index {
		if sess.IsActive() && sess.IsExpired(now) {
			due = append(due, sess)
		}
	}
	s.mu.RUnlock()
	for _, sess := range due {
		s.log.Info("expiring session", slog.String("session", sess.ID()))
		s.expire(ctx, sess)
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
