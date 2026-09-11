package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
)

// TestSweepKeepsPacedUsernameFailuresAccumulating is the regression guard for
// a defeated lockout.
//
// FR-7.2 gives the username counter no sliding window: five consecutive
// failures lock the account however far apart they are paced, and the count
// resets only on a successful login. The background sweeper, however, reaps
// rows whose lockout has elapsed and whose window is stale — and before the
// scope predicate was added to Store.DeleteElapsedAttempts it reaped
// user-scope rows too. An attacker pacing guesses further apart than the
// sweep interval therefore never reached the fifth failure, which made the
// branch's primary defence against credential guessing bypassable by waiting.
func TestSweepKeepsPacedUsernameFailuresAccumulating(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	cl := newClock()
	th := auth.NewThrottle(s, cl.now)
	ctx := context.Background()

	// Four failures, each followed by a sweep from a clock well past the
	// 15-minute window the sweeper uses as its cut-off.
	for i := 1; i <= 4; i++ {
		if err := th.Fail(ctx, "mallory", ""); err != nil {
			t.Fatalf("Fail %d: %v", i, err)
		}
		cl.advance(20 * time.Minute)
		if _, err := th.Sweep(ctx); err != nil {
			t.Fatalf("Sweep after failure %d: %v", i, err)
		}
		if err := th.Check(ctx, "mallory", ""); err != nil {
			t.Fatalf("failure %d of 5 must not lock yet: %v", i, err)
		}
	}

	if err := th.Fail(ctx, "mallory", ""); err != nil {
		t.Fatalf("Fail 5: %v", err)
	}
	if err := th.Check(ctx, "mallory", ""); err == nil {
		t.Fatal("five paced failures must engage the username lockout; the sweeper reset the counter between them")
	} else {
		lockedErr(t, err)
	}
}

// TestSweepStillReapsStaleIPCounters pins the other half of the predicate:
// narrowing the sweep to the IP scope must not stop it reaping what it is
// there to reap. The IP counter *is* windowed (FR-7.3), so a row whose
// lockout has elapsed and whose window has closed carries no information and
// is housekeeping the sweeper owns.
func TestSweepStillReapsStaleIPCounters(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	cl := newClock()
	th := auth.NewThrottle(s, cl.now)
	ctx := context.Background()

	if err := th.Fail(ctx, "", "203.0.113.9"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	cl.advance(31 * time.Minute)
	removed, err := th.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("Sweep removed %d rows, want 1 stale IP counter", removed)
	}
	a, err := s.Attempt(ctx, auth.ScopeIP, "203.0.113.9")
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if a.Failures != 0 {
		t.Fatalf("stale IP counter still has %d failures; it should have been reaped", a.Failures)
	}
}
