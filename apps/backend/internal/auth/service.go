package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jtumidanski/converge/internal/identity"
)

// hashConcurrency bounds simultaneous Argon2id operations.
//
// Each costs 64 MiB, so five concurrent registrations is 320 MiB transient.
// The per-IP throttle (FR-7.5) is a counter, not a concurrency limiter, so it
// cannot bound that. This semaphore turns a memory spike into latency: callers
// beyond it queue (design §5).
const hashConcurrency = 4

// lastSeenInterval rate-limits last_seen_at writes, so read-heavy traffic does
// not generate a write per request (FR-3.5).
const lastSeenInterval = 5 * time.Minute

// tokenBytes is the login session token length (FR-3.1).
const tokenBytes = 32

// Purger removes a user's filesystem-resident state. Implemented in
// internal/review (which already owns Cleaner) and wired in internal/app.
//
// This indirection exists because the account-deletion cascade spans three
// stores and no existing package may own all three: auth must not import
// review (wrong direction) and review must not import auth (that would drag
// SQLite into the review tier).
type Purger interface {
	PurgeUser(ctx context.Context, userID string) error
}

// ProviderUsage reports whether a user has a non-terminal review session
// referencing a provider slug (FR-5.7). Implemented in internal/review for the
// same reason as Purger.
type ProviderUsage interface {
	ProviderInUse(scope identity.Scope, providerSlug string) bool
}

// invalidCredentialsMessage is byte-identical for an unknown username and a
// wrong password (FR-2.5).
const invalidCredentialsMessage = "The username or password is incorrect."

// ServiceDeps are Service's collaborators.
type ServiceDeps struct {
	Store      *Store
	Sealer     *Sealer
	Throttle   *Throttle
	Resolver   *ProviderResolver
	Verifier   ProviderVerifier
	Purger     Purger
	Usage      ProviderUsage
	Log        *slog.Logger
	Now        func() time.Time
	SessionTTL time.Duration // absolute expiry (LOGIN_SESSION_TTL_HOURS)
	IdleTTL    time.Duration // idle expiry (LOGIN_SESSION_IDLE_HOURS)
}

// Credentials is a username/password pair, used by both Register and Login.
type Credentials struct {
	Username string
	Password string
}

// Service is the account lifecycle: register/login/logout/authenticate,
// change-password, delete-account, and the small set of read-only
// pass-throughs the API and startup log need.
type Service struct {
	deps    ServiceDeps
	hashSem chan struct{}
}

// NewService builds a Service over deps.
func NewService(d ServiceDeps) *Service {
	return &Service{deps: d, hashSem: make(chan struct{}, hashConcurrency)}
}

// hash and verify are the only two places Argon2id runs.
//
// ORDERING DISCIPLINE, do not break: neither may be called while a database
// transaction is open. The pool has exactly one connection, so hashing inside
// a transaction blocks every other query for ~50 ms per call. Every caller
// below hashes or verifies first and touches the database afterwards.
func (s *Service) hash(password string) (string, error) {
	s.hashSem <- struct{}{}
	defer func() { <-s.hashSem }()
	return HashPassword(password)
}

func (s *Service) verify(encoded, password string) error {
	s.hashSem <- struct{}{}
	defer func() { <-s.hashSem }()
	return VerifyPassword(encoded, password)
}

// newToken returns the plaintext cookie value and its SHA-256. Only the hash
// is ever persisted (FR-3.3); the plaintext exists in the response that
// creates it and in the client's cookie jar, nowhere else, and is never
// logged.
func newToken() (string, []byte, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("auth: generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Register creates an account and logs the caller in. Registration is open:
// any client that can reach the instance may create an account (FR-2.4). It
// necessarily discloses username availability; that is accepted and
// documented.
func (s *Service) Register(ctx context.Context, c Credentials, clientIP string) (User, string, error) {
	// Registration is throttled per IP only — there is no username counter to
	// consult before the account exists (FR-7.5).
	if err := s.deps.Throttle.Check(ctx, "", clientIP); err != nil {
		return User{}, "", err
	}
	if err := ValidateUsername(c.Username); err != nil {
		return User{}, "", err
	}
	if err := ValidatePassword(c.Password); err != nil {
		return User{}, "", err
	}
	hashed, err := s.hash(c.Password) // before any transaction
	if err != nil {
		return User{}, "", err
	}
	id, err := NewID()
	if err != nil {
		return User{}, "", err
	}
	now := s.deps.Now()
	user, err := NewUser(id, c.Username, hashed, now)
	if err != nil {
		return User{}, "", err
	}
	if err := s.deps.Store.CreateUser(ctx, user); err != nil {
		// A taken username counts against the IP throttle: it is the shape a
		// flooding attempt takes.
		if failErr := s.deps.Throttle.Fail(ctx, "", clientIP); failErr != nil {
			s.deps.Log.Warn("record registration failure", slog.String("error", failErr.Error()))
		}
		return User{}, "", err
	}
	token, err := s.startSession(ctx, user.ID(), now)
	if err != nil {
		return User{}, "", err
	}
	s.deps.Log.Info("account registered", slog.String("user_id", user.ID()))
	return user, token, nil
}

// Login verifies the password and creates a login session.
func (s *Service) Login(ctx context.Context, c Credentials, clientIP string) (User, string, error) {
	fold := Fold(c.Username)
	if err := s.deps.Throttle.Check(ctx, fold, clientIP); err != nil {
		return User{}, "", err
	}
	user, lookupErr := s.deps.Store.UserByFold(ctx, fold)
	// On an unknown username, verify against the package dummy hash anyway so
	// response timing does not distinguish "no such user" from "wrong
	// password" (FR-2.5). Both paths then return the same error.
	encoded := dummyHash
	if lookupErr == nil {
		encoded = user.PasswordHash()
	}
	verifyErr := s.verify(encoded, c.Password)
	// "We could not check" is not "they did not match". Only ErrNotFound means
	// the username is unknown; any other lookup error is a store fault, and
	// answering a database outage with 401 INVALID_CREDENTIALS would tell the
	// user their password is wrong when it may well be right. It propagates
	// unclassified, which the API layer answers 500 with a generic message,
	// and it is not counted against the throttle — an outage must not spend an
	// innocent user's failure budget. The dummy verification above has already
	// run, so this path's timing profile is unchanged.
	if lookupErr != nil && !errors.Is(lookupErr, ErrNotFound) {
		s.deps.Log.Error("login could not read the user store", slog.String("error", lookupErr.Error()))
		return User{}, "", lookupErr
	}
	if lookupErr != nil || verifyErr != nil {
		if failErr := s.deps.Throttle.Fail(ctx, fold, clientIP); failErr != nil {
			s.deps.Log.Warn("record login failure", slog.String("error", failErr.Error()))
		}
		// user_id is deliberately absent: on the unknown-username path there
		// is none, and logging the attempted username here would put a
		// near-miss credential in the log.
		s.deps.Log.Info("login failed")
		return User{}, "", &Error{Code: CodeInvalidCredentials, Message: invalidCredentialsMessage}
	}
	if err := s.deps.Throttle.Succeed(ctx, fold, clientIP); err != nil {
		return User{}, "", err
	}
	token, err := s.startSession(ctx, user.ID(), s.deps.Now())
	if err != nil {
		return User{}, "", err
	}
	s.deps.Log.Info("login succeeded", slog.String("user_id", user.ID()))
	return user, token, nil
}

func (s *Service) startSession(ctx context.Context, userID string, now time.Time) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	if err := s.deps.Store.CreateLoginSession(ctx, NewLoginSession(hash, userID, now, s.deps.SessionTTL)); err != nil {
		return "", err
	}
	return token, nil
}

// Logout deletes the current login session. Logout with no valid session is a
// no-op returning the same success status, so it never reveals session
// validity (FR-3.6).
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	hash := hashToken(token)
	// Read the session before deleting it, purely so the observability event
	// can carry user_id — the NFR list requires one for logout, and the token
	// hash is not a user identifier.
	userID := ""
	if ls, err := s.deps.Store.LoginSession(ctx, hash); err == nil {
		userID = ls.UserID()
	}
	if err := s.deps.Store.DeleteLoginSession(ctx, hash); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if userID != "" {
		s.deps.Log.Info("logout", slog.String("user_id", userID))
	}
	return nil
}

// Authenticate resolves a cookie token to a scope, returning the token hash so
// the caller can later identify "this session" (ChangePassword keeps it).
//
// Expiry is enforced here, on read, so a stale row is never honoured even
// before the sweeper runs, and the row is deleted on the spot (FR-3.7).
func (s *Service) Authenticate(ctx context.Context, token string) (identity.Scope, []byte, error) {
	unauthenticated := &Error{Code: CodeUnauthenticated, Message: "Sign in to continue."}
	if token == "" {
		return identity.Standalone(), nil, unauthenticated
	}
	hash := hashToken(token)
	ls, err := s.deps.Store.LoginSession(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return identity.Standalone(), nil, unauthenticated
		}
		return identity.Standalone(), nil, err
	}
	now := s.deps.Now()
	if !now.Before(ls.ExpiresAt()) || now.Sub(ls.LastSeenAt()) >= s.deps.IdleTTL {
		if delErr := s.deps.Store.DeleteLoginSession(ctx, hash); delErr != nil {
			s.deps.Log.Warn("delete expired login session", slog.String("error", delErr.Error()))
		}
		return identity.Standalone(), nil, unauthenticated
	}
	// Rate-limited so read-heavy traffic does not write once per request
	// (FR-3.5).
	if now.Sub(ls.LastSeenAt()) >= lastSeenInterval {
		if err := s.deps.Store.TouchLoginSession(ctx, hash, now); err != nil {
			s.deps.Log.Warn("touch login session", slog.String("error", err.Error()))
		}
	}
	return identity.ForUser(ls.UserID()), hash, nil
}

// ChangePassword replaces the password and revokes every other login session
// for this user; keep is the calling session's token hash (FR-2.6).
func (s *Service) ChangePassword(ctx context.Context, userID string, keep []byte, current, next string) error {
	user, err := s.deps.Store.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.verify(user.PasswordHash(), current); err != nil {
		return &Error{Code: CodeInvalidCredentials, Message: "The current password is incorrect."}
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	hashed, err := s.hash(next) // before any write
	if err != nil {
		return err
	}
	if err := s.deps.Store.SetPasswordHash(ctx, userID, hashed, s.deps.Now()); err != nil {
		return err
	}
	if err := s.deps.Store.DeleteOtherLoginSessions(ctx, userID, keep); err != nil {
		return err
	}
	s.deps.Log.Info("password changed", slog.String("user_id", userID))
	return nil
}

// DeleteAccount removes the account and everything it owns (FR-2.7).
//
// The order is deliberate: verify the password, delete the database rows (the
// cascade takes login sessions, provider configurations, and lockout records),
// invalidate the cached registry, then purge the filesystem. A purge failure
// is logged at ERROR with the user id and the request still succeeds:
// reversing the order would risk an account that has lost its data but can
// still log in, which is strictly worse than an account that is gone with
// orphaned bytes on disk (design §6).
func (s *Service) DeleteAccount(ctx context.Context, userID, password string) error {
	user, err := s.deps.Store.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.verify(user.PasswordHash(), password); err != nil {
		return &Error{Code: CodeInvalidCredentials, Message: invalidCredentialsMessage}
	}
	if err := s.deps.Store.DeleteUser(ctx, userID); err != nil {
		return err
	}
	s.deps.Resolver.Invalidate(userID)
	if err := s.deps.Purger.PurgeUser(ctx, userID); err != nil {
		s.deps.Log.Error("purge user state failed; the account is deleted but bytes remain on disk",
			slog.String("user_id", userID), slog.String("error", err.Error()))
	}
	s.deps.Log.Info("account deleted", slog.String("user_id", userID))
	return nil
}

// User, ProviderCount, CountUsers, and Ping are thin pass-throughs the API and
// the startup log need.
func (s *Service) User(ctx context.Context, userID string) (User, error) {
	return s.deps.Store.UserByID(ctx, userID)
}

func (s *Service) ProviderCount(ctx context.Context, userID string) (int, error) {
	return s.deps.Store.CountUserProviders(ctx, userID)
}

func (s *Service) CountUsers(ctx context.Context) (int, error) { return s.deps.Store.CountUsers(ctx) }

func (s *Service) Ping(ctx context.Context) error { return s.deps.Store.Ping(ctx) }
