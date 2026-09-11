package auth

import (
	"context"
	"time"
)

// Attempt is one throttle counter row: failures against a folded username
// ("user" scope) or a client IP ("ip" scope). Persisted so a restart does not
// clear a lockout in progress (FR-7.1).
type Attempt struct {
	Scope       string
	Key         string
	Failures    int
	WindowStart time.Time
	LockedUntil time.Time
}

// Attempt scopes.
const (
	ScopeUser = "user"
	ScopeIP   = "ip"
)

// Throttle thresholds and schedule (FR-7.2, FR-7.3).
const (
	userFailureThreshold = 5
	ipFailureThreshold   = 20
	ipWindow             = 15 * time.Minute
	baseLockout          = time.Minute
	maxLockout           = 15 * time.Minute
)

// lockedMessage is identical for a username lock and an IP lock. Saying which
// one engaged would disclose whether the username exists (FR-7.4).
const lockedMessage = "Too many failed attempts. Try again later."

// Throttle counts failed attempts and engages a doubling lockout. Counters
// are persisted, so a restart does not clear a lockout in progress (FR-7.1).
type Throttle struct {
	store *Store
	now   func() time.Time
}

// NewThrottle builds a Throttle over store. now is injected so tests can
// advance the clock directly instead of sleeping through the schedule.
func NewThrottle(store *Store, now func() time.Time) *Throttle {
	return &Throttle{store: store, now: now}
}

// Check reports whether either key is currently locked. It runs before any
// password verification, so a locked caller never pays for (or benefits from)
// an Argon2id comparison.
func (t *Throttle) Check(ctx context.Context, userKey, ipKey string) error {
	now := t.now()
	for _, k := range t.keys(userKey, ipKey) {
		a, err := t.store.Attempt(ctx, k.scope, k.key)
		if err != nil {
			return err
		}
		if now.Before(a.LockedUntil) {
			return &Error{
				Code:       CodeAccountLocked,
				Message:    lockedMessage,
				RetryAfter: a.LockedUntil.Sub(now),
			}
		}
	}
	return nil
}

// Fail records one failure against each supplied key and engages or extends
// the lockout when the key's threshold is met.
func (t *Throttle) Fail(ctx context.Context, userKey, ipKey string) error {
	now := t.now()
	for _, k := range t.keys(userKey, ipKey) {
		a, err := t.store.Attempt(ctx, k.scope, k.key)
		if err != nil {
			return err
		}
		// The IP counter is windowed: 20 failures "within 15 minutes"
		// (FR-7.3). The username counter is consecutive-failure based and
		// resets only on success (FR-7.2), so it has no window.
		if k.scope == ScopeIP && !a.WindowStart.IsZero() && now.Sub(a.WindowStart) > ipWindow {
			a.Failures = 0
			a.WindowStart = time.Time{}
		}
		if a.WindowStart.IsZero() {
			a.WindowStart = now
		}
		a.Failures++
		if a.Failures >= k.threshold {
			// Doubling from the threshold, capped. A failure can only be
			// recorded when the key is not locked (Check runs first), so the
			// exponent advances once per elapsed lockout, not once per
			// request.
			a.LockedUntil = now.Add(lockoutFor(a.Failures - k.threshold))
		}
		a.Scope, a.Key = k.scope, k.key
		if err := t.store.SaveAttempt(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

// Succeed clears both counters. A successful login means this username and
// this IP are no longer suspect.
func (t *Throttle) Succeed(ctx context.Context, userKey, ipKey string) error {
	for _, k := range t.keys(userKey, ipKey) {
		if err := t.store.ClearAttempt(ctx, k.scope, k.key); err != nil {
			return err
		}
	}
	return nil
}

// Sweep deletes counters whose lockout has elapsed and whose window is stale.
// Called by the background sweeper alongside expired login sessions (FR-3.7).
func (t *Throttle) Sweep(ctx context.Context) (int64, error) {
	return t.store.DeleteElapsedAttempts(ctx, t.now().Add(-ipWindow))
}

type throttleKey struct {
	scope     string
	key       string
	threshold int
}

// keys returns the counters in play. userKey is empty for registration, which
// is throttled per IP only (FR-7.5).
func (t *Throttle) keys(userKey, ipKey string) []throttleKey {
	out := make([]throttleKey, 0, 2)
	if userKey != "" {
		out = append(out, throttleKey{scope: ScopeUser, key: userKey, threshold: userFailureThreshold})
	}
	if ipKey != "" {
		out = append(out, throttleKey{scope: ScopeIP, key: ipKey, threshold: ipFailureThreshold})
	}
	return out
}

// lockoutFor doubles from baseLockout, capped at maxLockout: 1m, 2m, 4m, 8m,
// then 15m forever.
func lockoutFor(excess int) time.Duration {
	d := baseLockout
	for range excess {
		d *= 2
		if d >= maxLockout {
			return maxLockout
		}
	}
	return d
}
