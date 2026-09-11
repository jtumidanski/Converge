package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/config"
)

// FR-2.1: 3–32 characters, starting alphanumeric, then alphanumerics, dot,
// underscore, or hyphen. Stored as entered; compared folded.
var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{2,31}$`)

// FR-5.1: URL-safe provider slug, unique per user.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

const defaultGitHubBaseURL = "https://api.github.com"

// idBytes is 8, giving a 16-character hex id.
//
// This is the same scheme as session.NewID (crypto/rand, lowercase hex) at
// double the width. Wider is warranted here because these ids are long-lived
// primary keys and because a user id becomes a directory name under
// REPOSITORY_CACHE_ROOT (FR-6.5), where a collision would merge two users'
// mirrors.
const idBytes = 8

// NewID returns 16 lowercase hex characters from a CSPRNG.
func NewID() (string, error) {
	var b [idBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("auth: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Fold returns the case-insensitive uniqueness key for a username (FR-2.1).
func Fold(username string) string { return strings.ToLower(username) }

// ValidateUsername enforces FR-2.1.
func ValidateUsername(username string) error {
	if !usernameRe.MatchString(username) {
		return &Error{
			Code:    CodeInvalidUsername,
			Message: "A username is 3 to 32 characters, starts with a letter or digit, and may contain letters, digits, dots, underscores, and hyphens.",
		}
	}
	return nil
}

// ValidateSlug enforces FR-5.1.
//
// It returns a plain error, not an *Error: a malformed slug is a
// request-shape problem, which the handler reports as VALIDATION_ERROR (422)
// per api-contracts.md. CodeProviderSlugTaken is returned only by
// CreateProvider, and only on a genuine uniqueness conflict.
func ValidateSlug(slug string) error {
	if !slugRe.MatchString(slug) {
		return errors.New("auth: a slug is 1 to 32 characters of lowercase letters, digits, and hyphens, starting with a letter or digit")
	}
	return nil
}

// Last4 returns the last four characters of a token, for masked display.
// The whole point of storing it is that rendering a mask never requires a
// decrypt (api-contracts.md, GET /api/settings/providers).
func Last4(token string) string {
	if len(token) <= 4 {
		return token
	}
	return token[len(token)-4:]
}

// NormalizeBaseURL validates and normalises a provider base URL exactly as
// config.buildProvider does (FR-5.2): absolute http(s), trailing slash
// stripped, defaulting for github and required for gitlab.
func NormalizeBaseURL(kind config.Kind, raw string) (string, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		if kind == config.KindGitLab {
			return "", fmt.Errorf("auth: base url is required for gitlab providers")
		}
		base = defaultGitHubBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("auth: base url must be an absolute http(s) URL")
	}
	return strings.TrimRight(base, "/"), nil
}

// User is an account. Fields are unexported and set only through NewUser or
// the store's row scanners, matching the immutable-value style of
// internal/session.
type User struct {
	id           string
	username     string
	usernameFold string
	passwordHash string
	createdAt    time.Time
	updatedAt    time.Time
}

// NewUser validates the username and returns the account value. passwordHash
// is an already-computed Argon2id PHC string: NewUser never hashes, so the
// caller controls when the expensive operation happens relative to any open
// transaction.
func NewUser(id, username, passwordHash string, now time.Time) (User, error) {
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if id == "" || passwordHash == "" {
		return User{}, fmt.Errorf("auth: user id and password hash are required")
	}
	return User{
		id:           id,
		username:     username,
		usernameFold: Fold(username),
		passwordHash: passwordHash,
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

func (u User) ID() string           { return u.id }
func (u User) Username() string     { return u.username }
func (u User) UsernameFold() string { return u.usernameFold }
func (u User) PasswordHash() string { return u.passwordHash }
func (u User) CreatedAt() time.Time { return u.createdAt }
func (u User) UpdatedAt() time.Time { return u.updatedAt }

// LoginSession is a cookie-backed session. Only the SHA-256 of the token is
// ever held here or persisted (FR-3.3); the plaintext lives in the response
// that creates it and in the client's cookie jar, nowhere else.
type LoginSession struct {
	tokenHash  []byte
	userID     string
	createdAt  time.Time
	expiresAt  time.Time
	lastSeenAt time.Time
}

// NewLoginSession builds the value for a freshly minted token hash.
func NewLoginSession(tokenHash []byte, userID string, now time.Time, ttl time.Duration) LoginSession {
	return LoginSession{
		tokenHash:  tokenHash,
		userID:     userID,
		createdAt:  now,
		expiresAt:  now.Add(ttl),
		lastSeenAt: now,
	}
}

func (s LoginSession) TokenHash() []byte     { return append([]byte(nil), s.tokenHash...) }
func (s LoginSession) UserID() string        { return s.userID }
func (s LoginSession) CreatedAt() time.Time  { return s.createdAt }
func (s LoginSession) ExpiresAt() time.Time  { return s.expiresAt }
func (s LoginSession) LastSeenAt() time.Time { return s.lastSeenAt }

// UserProvider is one user's provider configuration. The token is present
// only as ciphertext plus nonce plus a four-character tail; there is no field
// anywhere in this type that holds a plaintext token.
type UserProvider struct {
	id              string
	userID          string
	slug            string
	displayName     string
	kind            config.Kind
	baseURL         string
	tokenCiphertext []byte
	tokenNonce      []byte
	tokenLast4      string
	tokenSetAt      time.Time
	createdAt       time.Time
	updatedAt       time.Time
}

func (p UserProvider) ID() string          { return p.id }
func (p UserProvider) UserID() string      { return p.userID }
func (p UserProvider) Slug() string        { return p.slug }
func (p UserProvider) DisplayName() string { return p.displayName }
func (p UserProvider) Kind() config.Kind   { return p.kind }
func (p UserProvider) BaseURL() string     { return p.baseURL }
func (p UserProvider) TokenCiphertext() []byte {
	return append([]byte(nil), p.tokenCiphertext...)
}
func (p UserProvider) TokenNonce() []byte    { return append([]byte(nil), p.tokenNonce...) }
func (p UserProvider) TokenLast4() string    { return p.tokenLast4 }
func (p UserProvider) TokenSetAt() time.Time { return p.tokenSetAt }
func (p UserProvider) CreatedAt() time.Time  { return p.createdAt }
func (p UserProvider) UpdatedAt() time.Time  { return p.updatedAt }
