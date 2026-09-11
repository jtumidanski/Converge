// Package auth owns hosted-mode accounts: password hashing, login sessions,
// encrypted per-user provider configuration, login throttling, and the
// per-user provider registry resolver.
//
// It imports internal/db, internal/config, internal/identity, and
// internal/provider (plus the github/gitlab clients). It must never import
// internal/review or internal/session: the cascade that spans those stores
// reaches them through the Purger and ProviderUsage collaborators instead
// (design §6).
package auth

import (
	"errors"
	"net/http"
	"time"
)

// Code is the machine-readable error code carried on the wire. Values match
// PRD §5.3 exactly.
type Code string

const (
	CodeUnauthenticated      Code = "UNAUTHENTICATED"
	CodeInvalidCredentials   Code = "INVALID_CREDENTIALS" // #nosec G101 -- an error code string, not a credential
	CodeAccountLocked        Code = "ACCOUNT_LOCKED"
	CodeUsernameTaken        Code = "USERNAME_TAKEN"
	CodeInvalidUsername      Code = "INVALID_USERNAME"
	CodeWeakPassword         Code = "WEAK_PASSWORD"
	CodeForbidden            Code = "FORBIDDEN"
	CodeProviderSlugTaken    Code = "PROVIDER_SLUG_TAKEN"
	CodeProviderUnauthorized Code = "PROVIDER_UNAUTHORIZED"
	CodeProviderInUse        Code = "PROVIDER_IN_USE"
)

// ErrNotFound reports an unknown row, or a row belonging to another user.
// The two are deliberately indistinguishable: FR-4.2 requires a cross-user
// request to answer 404, never 403, so resource existence is not disclosed.
var ErrNotFound = errors.New("auth: not found")

// Error is the typed domain error api.classify recognises with a single
// errors.As arm, mirroring session.ReviewError.
type Error struct {
	Code    Code
	Message string
	// RetryAfter is set only for CodeAccountLocked, and only so the login
	// handler can emit a Retry-After header (FR-7.4). classify returns a
	// triple and cannot set headers, so that one handler reads this field
	// directly.
	RetryAfter time.Duration
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// Status maps a code onto its HTTP status (PRD §5.3).
func (e *Error) Status() int {
	switch e.Code {
	case CodeUnauthenticated, CodeInvalidCredentials:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeUsernameTaken, CodeProviderSlugTaken, CodeProviderInUse:
		return http.StatusConflict
	case CodeInvalidUsername, CodeWeakPassword, CodeProviderUnauthorized:
		return http.StatusUnprocessableEntity
	case CodeAccountLocked:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
