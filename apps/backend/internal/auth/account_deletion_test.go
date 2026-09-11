package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
)

// TestDeleteUserRemovesTheUsernameThrottleCounter proves the account-deletion
// cascade reaches login_attempts.
//
// login_attempts is keyed by folded username, not by user id, so it has no
// foreign key to users and ON DELETE CASCADE cannot reach it
// (internal/db/migrations/0001_init.sql). Without an explicit delete, a
// lockout outlives the account it belongs to.
func TestDeleteUserRemovesTheUsernameThrottleCounter(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	u := mustUser(t, s, "Frank")

	if _, err := s.UpdateAttempt(ctx, auth.ScopeUser, auth.Fold("Frank"), func(a auth.Attempt) auth.Attempt {
		a.Failures = 5
		a.WindowStart = time.Unix(1000, 0).UTC()
		a.LockedUntil = time.Unix(1_900_000_000, 0).UTC()
		return a
	}); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}

	if err := s.DeleteUser(ctx, u.ID()); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	a, err := s.Attempt(ctx, auth.ScopeUser, auth.Fold("Frank"))
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if a.Failures != 0 || !a.LockedUntil.IsZero() {
		t.Fatalf("the deleted account's throttle counter survived: %+v", a)
	}
}

// TestDeleteUserLeavesOtherUsernamesThrottleCountersAlone keeps the explicit
// delete narrow: it must remove one username's counter, not clear the table.
func TestDeleteUserLeavesOtherUsernamesThrottleCountersAlone(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	doomed := mustUser(t, s, "doomed")
	mustUser(t, s, "bystander")

	for _, name := range []string{"doomed", "bystander"} {
		if _, err := s.UpdateAttempt(ctx, auth.ScopeUser, name, func(a auth.Attempt) auth.Attempt {
			a.Failures = 3
			return a
		}); err != nil {
			t.Fatalf("UpdateAttempt %s: %v", name, err)
		}
	}
	if _, err := s.UpdateAttempt(ctx, auth.ScopeIP, "198.51.100.7", func(a auth.Attempt) auth.Attempt {
		a.Failures = 7
		return a
	}); err != nil {
		t.Fatalf("UpdateAttempt ip: %v", err)
	}

	if err := s.DeleteUser(ctx, doomed.ID()); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	bystander, err := s.Attempt(ctx, auth.ScopeUser, "bystander")
	if err != nil {
		t.Fatalf("Attempt bystander: %v", err)
	}
	if bystander.Failures != 3 {
		t.Fatalf("bystander's counter = %d failures, want 3", bystander.Failures)
	}
	ip, err := s.Attempt(ctx, auth.ScopeIP, "198.51.100.7")
	if err != nil {
		t.Fatalf("Attempt ip: %v", err)
	}
	if ip.Failures != 7 {
		t.Fatalf("IP counter = %d failures, want 7", ip.Failures)
	}
}

// TestReRegisteredUsernameStartsWithoutAnInheritedLockout is the end-to-end
// consequence of the store fix: lock an account out, delete it, re-register
// the same username, and log in. Before the fix the new owner inherited the
// old owner's lockout.
func TestReRegisteredUsernameStartsWithoutAnInheritedLockout(t *testing.T) {
	t.Parallel()
	f := newService(t)
	ctx := context.Background()

	if _, _, err := f.svc.Register(ctx, auth.Credentials{Username: "erin", Password: "correcthorse"}, "10.0.9.1"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, _, err := f.svc.Login(ctx, auth.Credentials{Username: "erin", Password: "wrongpass1"}, "10.0.9.1"); err == nil {
			t.Fatalf("login %d with the wrong password should have failed", i+1)
		}
	}
	if _, _, err := f.svc.Login(ctx, auth.Credentials{Username: "erin", Password: "correcthorse"}, "10.0.9.1"); err == nil {
		t.Fatal("expected the account to be locked after five failures")
	} else {
		assertCode(t, err, auth.CodeAccountLocked)
	}

	user, err := f.store.UserByFold(ctx, auth.Fold("erin"))
	if err != nil {
		t.Fatalf("UserByFold: %v", err)
	}
	if err := f.svc.DeleteAccount(ctx, user.ID(), "correcthorse"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}

	if _, _, err := f.svc.Register(ctx, auth.Credentials{Username: "erin", Password: "newpassword"}, "10.0.9.2"); err != nil {
		t.Fatalf("re-registering the freed username: %v", err)
	}
	if _, _, err := f.svc.Login(ctx, auth.Credentials{Username: "erin", Password: "newpassword"}, "10.0.9.2"); err != nil {
		t.Fatalf("the new owner of a re-registered username inherited a lockout: %v", err)
	}
}
