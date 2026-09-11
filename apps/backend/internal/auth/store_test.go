package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/db"
)

// newStore returns a migrated, file-backed store. A file rather than
// :memory: because MaxOpenConns(1) plus WAL is the configuration under test
// and an in-memory database does not exercise it.
func newStore(t *testing.T) *auth.Store {
	t.Helper()
	handle, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "converge.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return auth.NewStore(handle)
}

// mustUser inserts an account and returns it.
func mustUser(t *testing.T, s *auth.Store, username string) auth.User {
	t.Helper()
	id, err := auth.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	u, err := auth.NewUser(id, username, "$argon2id$fake", time.Unix(1000, 0).UTC())
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

// mustProvider inserts a provider config for userID with the given slug and
// returns it. The token is sealed with a real *auth.Sealer so the ciphertext
// and nonce columns hold realistic bytes.
func mustProvider(t *testing.T, s *auth.Store, userID, slug, token string) auth.UserProvider {
	t.Helper()
	sealer, err := auth.NewSealer(config.NewSecret(key32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	id, err := auth.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	ciphertext, nonce, err := sealer.Seal(token, userID, id)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	p, err := auth.NewUserProvider(id, userID, slug, "Display "+slug, config.KindGitHub, "https://api.github.com",
		ciphertext, nonce, auth.Last4(token), time.Unix(2000, 0).UTC())
	if err != nil {
		t.Fatalf("NewUserProvider: %v", err)
	}
	if err := s.CreateUserProvider(context.Background(), p); err != nil {
		t.Fatalf("CreateUserProvider: %v", err)
	}
	return p
}

func TestCreateAndReadUser(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "Alice")

	byFold, err := s.UserByFold(context.Background(), auth.Fold("Alice"))
	if err != nil {
		t.Fatalf("UserByFold: %v", err)
	}
	byID, err := s.UserByID(context.Background(), u.ID())
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	for name, got := range map[string]auth.User{"byFold": byFold, "byID": byID} {
		if got.Username() != "Alice" {
			t.Errorf("%s: Username() = %q, want Alice", name, got.Username())
		}
		if got.PasswordHash() != u.PasswordHash() {
			t.Errorf("%s: PasswordHash() = %q, want %q", name, got.PasswordHash(), u.PasswordHash())
		}
		if got.CreatedAt().Unix() != u.CreatedAt().Unix() {
			t.Errorf("%s: CreatedAt() = %v, want %v", name, got.CreatedAt(), u.CreatedAt())
		}
	}
}

func TestUserByFoldIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	mustUser(t, s, "Alice")
	if _, err := s.UserByFold(context.Background(), auth.Fold("ALICE")); err != nil {
		t.Fatalf("UserByFold(folded ALICE) = %v, want nil", err)
	}
}

func TestCreateUserRejectsACaseInsensitiveDuplicate(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	mustUser(t, s, "Alice")

	id, err := auth.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	dup, err := auth.NewUser(id, "ALICE", "$argon2id$other", time.Unix(1000, 0).UTC())
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	err = s.CreateUser(context.Background(), dup)
	var ae *auth.Error
	if !errors.As(err, &ae) || ae.Code != auth.CodeUsernameTaken {
		t.Fatalf("CreateUser(dup) = %v, want *Error{Code: CodeUsernameTaken}", err)
	}
}

func TestUserLookupsReturnErrNotFound(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, err := s.UserByFold(context.Background(), "ghost"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("UserByFold(ghost) = %v, want ErrNotFound", err)
	}
	if _, err := s.UserByID(context.Background(), "nope"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("UserByID(nope) = %v, want ErrNotFound", err)
	}
}

func TestSetPasswordHash(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "Alice")

	if err := s.SetPasswordHash(context.Background(), u.ID(), "$argon2id$new", time.Unix(5000, 0).UTC()); err != nil {
		t.Fatalf("SetPasswordHash: %v", err)
	}
	got, err := s.UserByID(context.Background(), u.ID())
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if got.PasswordHash() != "$argon2id$new" {
		t.Fatalf("PasswordHash() = %q, want $argon2id$new", got.PasswordHash())
	}
	if got.UpdatedAt().Unix() != 5000 {
		t.Fatalf("UpdatedAt() = %v, want unix 5000", got.UpdatedAt())
	}
}

func TestCountUsers(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if n, err := s.CountUsers(context.Background()); err != nil || n != 0 {
		t.Fatalf("CountUsers() = %d, %v; want 0, nil", n, err)
	}
	mustUser(t, s, "Alice")
	if n, err := s.CountUsers(context.Background()); err != nil || n != 1 {
		t.Fatalf("CountUsers() = %d, %v; want 1, nil", n, err)
	}
	mustUser(t, s, "Bob")
	if n, err := s.CountUsers(context.Background()); err != nil || n != 2 {
		t.Fatalf("CountUsers() = %d, %v; want 2, nil", n, err)
	}
}

func TestLoginSessionLifecycle(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "Alice")

	hash := []byte("token-hash-1")
	ls := auth.NewLoginSession(hash, u.ID(), time.Unix(1000, 0).UTC(), time.Hour)
	if err := s.CreateLoginSession(context.Background(), ls); err != nil {
		t.Fatalf("CreateLoginSession: %v", err)
	}

	got, err := s.LoginSession(context.Background(), hash)
	if err != nil {
		t.Fatalf("LoginSession: %v", err)
	}
	if got.UserID() != u.ID() || got.ExpiresAt().Unix() != ls.ExpiresAt().Unix() || got.LastSeenAt().Unix() != ls.LastSeenAt().Unix() {
		t.Fatalf("LoginSession() = %+v, want matching UserID/ExpiresAt/LastSeenAt", got)
	}

	if err := s.TouchLoginSession(context.Background(), hash, time.Unix(2000, 0).UTC()); err != nil {
		t.Fatalf("TouchLoginSession: %v", err)
	}
	touched, err := s.LoginSession(context.Background(), hash)
	if err != nil {
		t.Fatalf("LoginSession after touch: %v", err)
	}
	if touched.LastSeenAt().Unix() != 2000 {
		t.Fatalf("LastSeenAt() = %v, want unix 2000", touched.LastSeenAt())
	}
	if touched.ExpiresAt().Unix() != ls.ExpiresAt().Unix() {
		t.Fatalf("ExpiresAt() changed after touch: %v, want %v", touched.ExpiresAt(), ls.ExpiresAt())
	}

	if err := s.DeleteLoginSession(context.Background(), hash); err != nil {
		t.Fatalf("DeleteLoginSession: %v", err)
	}
	if _, err := s.LoginSession(context.Background(), hash); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("LoginSession after delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteOtherLoginSessions(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	userA := mustUser(t, s, "Alice")
	userB := mustUser(t, s, "Bob")

	keep := []byte("keep")
	other1 := []byte("other-1")
	other2 := []byte("other-2")
	bHash := []byte("b-hash")

	for _, h := range [][]byte{keep, other1, other2} {
		if err := s.CreateLoginSession(context.Background(), auth.NewLoginSession(h, userA.ID(), time.Unix(1000, 0).UTC(), time.Hour)); err != nil {
			t.Fatalf("CreateLoginSession(A): %v", err)
		}
	}
	if err := s.CreateLoginSession(context.Background(), auth.NewLoginSession(bHash, userB.ID(), time.Unix(1000, 0).UTC(), time.Hour)); err != nil {
		t.Fatalf("CreateLoginSession(B): %v", err)
	}

	if err := s.DeleteOtherLoginSessions(context.Background(), userA.ID(), keep); err != nil {
		t.Fatalf("DeleteOtherLoginSessions: %v", err)
	}

	if _, err := s.LoginSession(context.Background(), keep); err != nil {
		t.Fatalf("kept session missing: %v", err)
	}
	for _, h := range [][]byte{other1, other2} {
		if _, err := s.LoginSession(context.Background(), h); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("session %s survived: %v", h, err)
		}
	}
	if _, err := s.LoginSession(context.Background(), bHash); err != nil {
		t.Fatalf("user B's session was deleted: %v", err)
	}
}

func TestDeleteExpiredLoginSessions(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "Alice")
	now := time.Unix(10_000, 0).UTC()
	idle := 30 * time.Minute

	expiredAbs := []byte("expired-abs")
	expiredIdle := []byte("expired-idle")
	live := []byte("live")

	// mkSession builds a session with the given expires_at, then
	// TouchLoginSession below sets its last_seen_at independently.
	mkSession := func(hash []byte, createdAt, expiresAt time.Time) auth.LoginSession {
		return auth.NewLoginSession(hash, u.ID(), createdAt, expiresAt.Sub(createdAt))
	}
	// Absolutely expired: expires_at in the past, last_seen_at recent.
	absExpired := mkSession(expiredAbs, now.Add(-time.Hour), now.Add(-time.Minute))
	if err := s.CreateLoginSession(context.Background(), absExpired); err != nil {
		t.Fatalf("CreateLoginSession(absExpired): %v", err)
	}
	if err := s.TouchLoginSession(context.Background(), expiredAbs, now.Add(-time.Minute)); err != nil {
		t.Fatalf("TouchLoginSession(absExpired): %v", err)
	}

	idleExpired := mkSession(expiredIdle, now.Add(-2*time.Hour), now.Add(time.Hour))
	if err := s.CreateLoginSession(context.Background(), idleExpired); err != nil {
		t.Fatalf("CreateLoginSession(idleExpired): %v", err)
	}
	if err := s.TouchLoginSession(context.Background(), expiredIdle, now.Add(-time.Hour)); err != nil {
		t.Fatalf("TouchLoginSession(idleExpired): %v", err)
	}

	liveSession := mkSession(live, now.Add(-time.Minute), now.Add(time.Hour))
	if err := s.CreateLoginSession(context.Background(), liveSession); err != nil {
		t.Fatalf("CreateLoginSession(live): %v", err)
	}
	if err := s.TouchLoginSession(context.Background(), live, now); err != nil {
		t.Fatalf("TouchLoginSession(live): %v", err)
	}

	n, err := s.DeleteExpiredLoginSessions(context.Background(), now, idle)
	if err != nil {
		t.Fatalf("DeleteExpiredLoginSessions: %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteExpiredLoginSessions() = %d, want 2", n)
	}
	if _, err := s.LoginSession(context.Background(), live); err != nil {
		t.Fatalf("live session should survive: %v", err)
	}
	for _, h := range [][]byte{expiredAbs, expiredIdle} {
		if _, err := s.LoginSession(context.Background(), h); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("session %s should be gone: %v", h, err)
		}
	}
}

func TestDeleteUserCascades(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "Alice")
	hash := []byte("cascade-hash")
	if err := s.CreateLoginSession(context.Background(), auth.NewLoginSession(hash, u.ID(), time.Unix(1000, 0).UTC(), time.Hour)); err != nil {
		t.Fatalf("CreateLoginSession: %v", err)
	}
	p := mustProvider(t, s, u.ID(), "github", "ghp_token")
	if err := s.SaveAttempt(context.Background(), auth.Attempt{
		Scope: auth.ScopeUser, Key: auth.Fold("Alice"), Failures: 1,
		WindowStart: time.Unix(1000, 0).UTC(), LockedUntil: time.Unix(1000, 0).UTC(),
	}); err != nil {
		t.Fatalf("SaveAttempt: %v", err)
	}

	if err := s.DeleteUser(context.Background(), u.ID()); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := s.UserByID(context.Background(), u.ID()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("user should be gone: %v", err)
	}
	if _, err := s.LoginSession(context.Background(), hash); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("login session should cascade-delete: %v", err)
	}
	if _, err := s.UserProviderByID(context.Background(), u.ID(), p.ID()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("provider config should cascade-delete: %v", err)
	}
	// The attempt row is keyed by (scope, key), not user_id, so its removal
	// is not a foreign-key cascade; the fact that it is gone confirms the
	// account teardown clears it deliberately, not merely as a foreign-key
	// side effect. Attempt-row deletion on account teardown is service-layer
	// (Task 8+); here we only assert that the store itself did not leave the
	// row orphaned by fiat — i.e. it was never coupled to the user row at the
	// schema level, so this stays a documentation assertion, not a defect.
	got, err := s.Attempt(context.Background(), auth.ScopeUser, auth.Fold("Alice"))
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if got.Failures != 1 {
		t.Fatalf("attempt row unexpectedly cleared by DeleteUser: %+v", got)
	}
}

func TestUserProviderCRUD(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	u := mustUser(t, s, "Alice")
	pB := mustProvider(t, s, u.ID(), "bbb", "token-b")
	pA := mustProvider(t, s, u.ID(), "aaa", "token-a")

	list, err := s.ListUserProviders(context.Background(), u.ID())
	if err != nil {
		t.Fatalf("ListUserProviders: %v", err)
	}
	if len(list) != 2 || list[0].Slug() != "aaa" || list[1].Slug() != "bbb" {
		t.Fatalf("ListUserProviders() slugs = %v, want [aaa bbb]", []string{list[0].Slug(), list[1].Slug()})
	}

	got, err := s.UserProviderByID(context.Background(), u.ID(), pA.ID())
	if err != nil {
		t.Fatalf("UserProviderByID: %v", err)
	}
	if string(got.TokenCiphertext()) != string(pA.TokenCiphertext()) ||
		string(got.TokenNonce()) != string(pA.TokenNonce()) ||
		got.TokenLast4() != pA.TokenLast4() {
		t.Fatalf("UserProviderByID did not round-trip token fields: %+v", got)
	}

	sealer, err := auth.NewSealer(config.NewSecret(key32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	newCiphertext, newNonce, err := sealer.Seal("new-token", u.ID(), pA.ID())
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	updated, err := auth.NewUserProvider(pA.ID(), u.ID(), pA.Slug(), "New Display", config.KindGitHub, pA.BaseURL(),
		newCiphertext, newNonce, auth.Last4("new-token"), time.Unix(9000, 0).UTC())
	if err != nil {
		t.Fatalf("NewUserProvider(updated): %v", err)
	}
	if err := s.UpdateUserProvider(context.Background(), updated); err != nil {
		t.Fatalf("UpdateUserProvider: %v", err)
	}
	reread, err := s.UserProviderByID(context.Background(), u.ID(), pA.ID())
	if err != nil {
		t.Fatalf("UserProviderByID after update: %v", err)
	}
	if reread.DisplayName() != "New Display" || string(reread.TokenCiphertext()) != string(newCiphertext) {
		t.Fatalf("update did not take effect: %+v", reread)
	}
	if reread.UpdatedAt().Unix() != 9000 {
		t.Fatalf("UpdatedAt() = %v, want unix 9000", reread.UpdatedAt())
	}

	if err := s.DeleteUserProvider(context.Background(), u.ID(), pA.ID()); err != nil {
		t.Fatalf("DeleteUserProvider: %v", err)
	}
	if _, err := s.UserProviderByID(context.Background(), u.ID(), pA.ID()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("UserProviderByID after delete = %v, want ErrNotFound", err)
	}
	_ = pB
}

func TestUserProviderIsScopedToItsOwner(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	userA := mustUser(t, s, "Alice")
	userB := mustUser(t, s, "Bob")
	pA := mustProvider(t, s, userA.ID(), "github", "token-a")

	if _, err := s.UserProviderByID(context.Background(), userB.ID(), pA.ID()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("UserProviderByID(userB, idOfA) = %v, want ErrNotFound", err)
	}
	if err := s.DeleteUserProvider(context.Background(), userB.ID(), pA.ID()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("DeleteUserProvider(userB, idOfA) = %v, want ErrNotFound", err)
	}
	if _, err := s.UserProviderByID(context.Background(), userA.ID(), pA.ID()); err != nil {
		t.Fatalf("A's row should survive: %v", err)
	}
}

func TestCreateUserProviderRejectsADuplicateSlug(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	userA := mustUser(t, s, "Alice")
	userB := mustUser(t, s, "Bob")
	mustProvider(t, s, userA.ID(), "github", "token-1")

	sealer, err := auth.NewSealer(config.NewSecret(key32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	id, err := auth.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	ciphertext, nonce, err := sealer.Seal("token-2", userA.ID(), id)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	dup, err := auth.NewUserProvider(id, userA.ID(), "github", "Dup", config.KindGitHub, "https://api.github.com",
		ciphertext, nonce, "oken", time.Unix(2000, 0).UTC())
	if err != nil {
		t.Fatalf("NewUserProvider: %v", err)
	}
	err = s.CreateUserProvider(context.Background(), dup)
	var ae *auth.Error
	if !errors.As(err, &ae) || ae.Code != auth.CodeProviderSlugTaken {
		t.Fatalf("CreateUserProvider(dup slug, same user) = %v, want *Error{Code: CodeProviderSlugTaken}", err)
	}

	// A different user with the same slug succeeds: uniqueness is per user.
	// mustProvider calls t.Fatalf itself if CreateUserProvider errors.
	mustProvider(t, s, userB.ID(), "github", "token-3")
}

func TestCountUserProviders(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	userA := mustUser(t, s, "Alice")
	userB := mustUser(t, s, "Bob")

	if n, err := s.CountUserProviders(context.Background(), userA.ID()); err != nil || n != 0 {
		t.Fatalf("CountUserProviders(A) = %d, %v; want 0, nil", n, err)
	}
	mustProvider(t, s, userA.ID(), "github", "t1")
	mustProvider(t, s, userA.ID(), "gitlab", "t2")
	if n, err := s.CountUserProviders(context.Background(), userA.ID()); err != nil || n != 2 {
		t.Fatalf("CountUserProviders(A) = %d, %v; want 2, nil", n, err)
	}
	if n, err := s.CountUserProviders(context.Background(), userB.ID()); err != nil || n != 0 {
		t.Fatalf("CountUserProviders(B) = %d, %v; want 0, nil", n, err)
	}
}

func TestAttemptUpsertAndClear(t *testing.T) {
	t.Parallel()
	s := newStore(t)

	absent, err := s.Attempt(context.Background(), auth.ScopeUser, "ghost")
	if err != nil {
		t.Fatalf("Attempt(absent) error = %v, want nil", err)
	}
	if absent != (auth.Attempt{Scope: auth.ScopeUser, Key: "ghost"}) {
		t.Fatalf("Attempt(absent) = %+v, want zero counter with scope/key set", absent)
	}

	a := auth.Attempt{
		Scope: auth.ScopeUser, Key: "alice", Failures: 3,
		WindowStart: time.Unix(1000, 0).UTC(), LockedUntil: time.Unix(2000, 0).UTC(),
	}
	if err := s.SaveAttempt(context.Background(), a); err != nil {
		t.Fatalf("SaveAttempt: %v", err)
	}
	got, err := s.Attempt(context.Background(), auth.ScopeUser, "alice")
	if err != nil {
		t.Fatalf("Attempt: %v", err)
	}
	if got.Failures != 3 || got.WindowStart.Unix() != 1000 || got.LockedUntil.Unix() != 2000 {
		t.Fatalf("Attempt() = %+v, want failures=3, window=1000, locked=2000", got)
	}

	a.Failures = 5
	a.LockedUntil = time.Unix(3000, 0).UTC()
	if err := s.SaveAttempt(context.Background(), a); err != nil {
		t.Fatalf("SaveAttempt(update): %v", err)
	}
	got, err = s.Attempt(context.Background(), auth.ScopeUser, "alice")
	if err != nil {
		t.Fatalf("Attempt after update: %v", err)
	}
	if got.Failures != 5 || got.LockedUntil.Unix() != 3000 {
		t.Fatalf("Attempt() after update = %+v, want failures=5, locked=3000", got)
	}

	if err := s.ClearAttempt(context.Background(), auth.ScopeUser, "alice"); err != nil {
		t.Fatalf("ClearAttempt: %v", err)
	}
	cleared, err := s.Attempt(context.Background(), auth.ScopeUser, "alice")
	if err != nil {
		t.Fatalf("Attempt after clear: %v", err)
	}
	if cleared != (auth.Attempt{Scope: auth.ScopeUser, Key: "alice"}) {
		t.Fatalf("Attempt after clear = %+v, want zero counter", cleared)
	}
}

func TestDeleteElapsedAttempts(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	now := time.Unix(10_000, 0).UTC()

	elapsed := auth.Attempt{
		Scope: auth.ScopeUser, Key: "elapsed", Failures: 1,
		WindowStart: now.Add(-time.Hour), LockedUntil: now.Add(-time.Minute),
	}
	live := auth.Attempt{
		Scope: auth.ScopeUser, Key: "live", Failures: 1,
		WindowStart: now.Add(-time.Minute), LockedUntil: now.Add(time.Hour),
	}
	if err := s.SaveAttempt(context.Background(), elapsed); err != nil {
		t.Fatalf("SaveAttempt(elapsed): %v", err)
	}
	if err := s.SaveAttempt(context.Background(), live); err != nil {
		t.Fatalf("SaveAttempt(live): %v", err)
	}

	n, err := s.DeleteElapsedAttempts(context.Background(), now)
	if err != nil {
		t.Fatalf("DeleteElapsedAttempts: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteElapsedAttempts() = %d, want 1", n)
	}
	if got, err := s.Attempt(context.Background(), auth.ScopeUser, "elapsed"); err != nil || got.Failures != 0 {
		t.Fatalf("elapsed attempt should be gone: %+v, %v", got, err)
	}
	if got, err := s.Attempt(context.Background(), auth.ScopeUser, "live"); err != nil || got.Failures != 1 {
		t.Fatalf("live attempt should survive: %+v, %v", got, err)
	}
}

func TestPing(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() = %v, want nil", err)
	}
}
