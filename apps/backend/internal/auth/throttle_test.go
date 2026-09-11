package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/db"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_700_000_000, 0).UTC()} }
func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// lockedErr asserts err is a *auth.Error{Code: CodeAccountLocked} and returns
// its RetryAfter, failing the test otherwise.
func lockedErr(t *testing.T, err error) time.Duration {
	t.Helper()
	var ae *auth.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *auth.Error, got %v (%T)", err, err)
	}
	if ae.Code != auth.CodeAccountLocked {
		t.Fatalf("expected code %q, got %q", auth.CodeAccountLocked, ae.Code)
	}
	return ae.RetryAfter
}

func TestUserLockoutEngagesAtFiveFailures(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	th := auth.NewThrottle(newStore(t), c.now)

	for i := 0; i < 4; i++ {
		if err := th.Fail(ctx, "alice", ""); err != nil {
			t.Fatalf("Fail #%d: %v", i+1, err)
		}
		if err := th.Check(ctx, "alice", ""); err != nil {
			t.Fatalf("Check after %d failures should be nil, got %v", i+1, err)
		}
	}

	if err := th.Fail(ctx, "alice", ""); err != nil {
		t.Fatalf("Fail #5: %v", err)
	}
	err := th.Check(ctx, "alice", "")
	if err == nil {
		t.Fatal("expected lockout after 5th failure, got nil")
	}
	retry := lockedErr(t, err)
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("RetryAfter = %v, want in (0, 1m]", retry)
	}

	c.advance(time.Minute)
	if err := th.Check(ctx, "alice", ""); err != nil {
		t.Fatalf("Check after the lockout elapsed should be nil, got %v", err)
	}
}

func TestUserLockoutDoubles(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	th := auth.NewThrottle(newStore(t), c.now)

	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}

	// Four failures to reach the threshold without engaging a lockout.
	for i := 0; i < 4; i++ {
		if err := th.Fail(ctx, "alice", ""); err != nil {
			t.Fatalf("warmup Fail #%d: %v", i+1, err)
		}
	}

	for i, w := range want {
		if err := th.Fail(ctx, "alice", ""); err != nil {
			t.Fatalf("Fail (stage %d): %v", i, err)
		}
		err := th.Check(ctx, "alice", "")
		if err == nil {
			t.Fatalf("stage %d: expected lockout, got nil", i)
		}
		got := lockedErr(t, err)
		if got != w {
			t.Fatalf("stage %d: RetryAfter = %v, want %v", i, got, w)
		}
		c.advance(w)
		if err := th.Check(ctx, "alice", ""); err != nil {
			t.Fatalf("stage %d: Check after the lockout elapsed should be nil, got %v", i, err)
		}
	}
}

func TestSuccessClearsTheUserCounter(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	th := auth.NewThrottle(newStore(t), c.now)

	for i := 0; i < 4; i++ {
		if err := th.Fail(ctx, "alice", ""); err != nil {
			t.Fatalf("Fail #%d: %v", i+1, err)
		}
	}
	if err := th.Succeed(ctx, "alice", ""); err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	for i := 0; i < 4; i++ {
		if err := th.Fail(ctx, "alice", ""); err != nil {
			t.Fatalf("Fail after success #%d: %v", i+1, err)
		}
	}
	if err := th.Check(ctx, "alice", ""); err != nil {
		t.Fatalf("Check after 4+4 failures split by a success should be nil (counter restarted), got %v", err)
	}
}

func TestIPLockoutAtTwentyFailuresWithinTheWindow(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	th := auth.NewThrottle(newStore(t), c.now)

	for i := 0; i < 20; i++ {
		userKey := "user" + string(rune('a'+i))
		if err := th.Fail(ctx, userKey, "9.9.9.9"); err != nil {
			t.Fatalf("Fail #%d: %v", i+1, err)
		}
	}

	err := th.Check(ctx, "brand-new-user", "9.9.9.9")
	if err == nil {
		t.Fatal("expected IP lockout after 20 failures, got nil")
	}
	lockedErr(t, err)
}

func TestIPWindowResets(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	th := auth.NewThrottle(newStore(t), c.now)

	for i := 0; i < 19; i++ {
		userKey := "userA" + string(rune('a'+i))
		if err := th.Fail(ctx, userKey, "1.1.1.1"); err != nil {
			t.Fatalf("first batch Fail #%d: %v", i+1, err)
		}
	}

	c.advance(16 * time.Minute)

	for i := 0; i < 19; i++ {
		userKey := "userB" + string(rune('a'+i))
		if err := th.Fail(ctx, userKey, "1.1.1.1"); err != nil {
			t.Fatalf("second batch Fail #%d: %v", i+1, err)
		}
	}

	if err := th.Check(ctx, "brand-new-user", "1.1.1.1"); err != nil {
		t.Fatalf("Check should be nil because the window restarted (19+19 < 20 within any single window), got %v", err)
	}
}

func TestRegistrationThrottleUsesTheIPKeyOnly(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	th := auth.NewThrottle(newStore(t), c.now)

	for i := 0; i < 20; i++ {
		if err := th.Fail(ctx, "", "2.2.2.2"); err != nil {
			t.Fatalf("Fail #%d: %v", i+1, err)
		}
	}

	err := th.Check(ctx, "", "2.2.2.2")
	if err == nil {
		t.Fatal("expected the registration IP to be locked, got nil")
	}
	lockedErr(t, err)

	if err := th.Check(ctx, "alice", "3.3.3.3"); err != nil {
		t.Fatalf("a different IP and username should not be locked, got %v", err)
	}
}

func TestLockoutSurvivesReopeningTheDatabase(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	path := filepath.Join(t.TempDir(), "converge.db")

	handle, err := db.Open(ctx, db.Options{Path: path})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(ctx, handle); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	store := auth.NewStore(handle)
	th := auth.NewThrottle(store, c.now)

	for i := 0; i < 5; i++ {
		if err := th.Fail(ctx, "alice", ""); err != nil {
			t.Fatalf("Fail #%d: %v", i+1, err)
		}
	}
	if err := th.Check(ctx, "alice", ""); err == nil {
		t.Fatal("expected lockout before reopening, got nil")
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	handle2, err := db.Open(ctx, db.Options{Path: path})
	if err != nil {
		t.Fatalf("db.Open (reopen): %v", err)
	}
	t.Cleanup(func() { _ = handle2.Close() })
	if err := db.Migrate(ctx, handle2); err != nil {
		t.Fatalf("db.Migrate (reopen): %v", err)
	}
	store2 := auth.NewStore(handle2)
	th2 := auth.NewThrottle(store2, c.now)

	err = th2.Check(ctx, "alice", "")
	if err == nil {
		t.Fatal("expected the lockout to survive reopening the database, got nil")
	}
	lockedErr(t, err)
}

func TestSweepRemovesElapsedRows(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	th := auth.NewThrottle(newStore(t), c.now)

	for i := 0; i < 5; i++ {
		if err := th.Fail(ctx, "alice", ""); err != nil {
			t.Fatalf("Fail #%d: %v", i+1, err)
		}
	}
	if err := th.Check(ctx, "alice", ""); err == nil {
		t.Fatal("expected lockout before sweeping, got nil")
	}

	c.advance(time.Hour)

	n, err := th.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("Sweep removed %d rows, want 1", n)
	}
	if err := th.Check(ctx, "alice", ""); err != nil {
		t.Fatalf("Check after Sweep should be nil, got %v", err)
	}
}

// TestFailIsAtomicUnderConcurrency races Fail against itself for a single
// key. If the read-modify-write were not atomic across the two store calls,
// concurrent goroutines could read the same Failures value and each write
// back n+1, losing an increment.
func TestFailIsAtomicUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	store := newStore(t)
	th := auth.NewThrottle(store, c.now)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = th.Fail(ctx, "", "5.5.5.5")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Fail goroutine %d: %v", i, err)
		}
	}

	got, err := store.Attempt(ctx, auth.ScopeIP, "5.5.5.5")
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if got.Failures != n {
		t.Fatalf("Failures = %d, want %d (lost update under concurrency)", got.Failures, n)
	}
}

func TestLockoutMessageDoesNotDiscloseWhichKeyIsLocked(t *testing.T) {
	ctx := context.Background()
	c := newClock()
	th := auth.NewThrottle(newStore(t), c.now)

	for i := 0; i < 5; i++ {
		if err := th.Fail(ctx, "alice", ""); err != nil {
			t.Fatalf("user Fail #%d: %v", i+1, err)
		}
	}
	userErr := th.Check(ctx, "alice", "")
	if userErr == nil {
		t.Fatal("expected the username lockout, got nil")
	}

	for i := 0; i < 20; i++ {
		userKey := "other" + string(rune('a'+i))
		if err := th.Fail(ctx, userKey, "4.4.4.4"); err != nil {
			t.Fatalf("ip Fail #%d: %v", i+1, err)
		}
	}
	ipErr := th.Check(ctx, "brand-new-user", "4.4.4.4")
	if ipErr == nil {
		t.Fatal("expected the IP lockout, got nil")
	}

	var uae, iae *auth.Error
	if !errors.As(userErr, &uae) || !errors.As(ipErr, &iae) {
		t.Fatalf("expected both errors to be *auth.Error, got %v and %v", userErr, ipErr)
	}
	if uae.Message != iae.Message {
		t.Fatalf("username lock message %q differs from IP lock message %q; a caller could infer which key locked", uae.Message, iae.Message)
	}
}
