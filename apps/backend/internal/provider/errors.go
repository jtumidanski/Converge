package provider

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrAuth           = errors.New("provider authentication failed")
	ErrNotFound       = errors.New("provider resource not found")
	ErrUnavailable    = errors.New("provider unavailable")
	ErrTooManyCommits = errors.New("change has too many commits to verify")
)

// StatusError records an HTTP failure without headers or bodies.
type StatusError struct {
	Method     string
	Path       string
	Status     int
	RetryAfter string
	sentinel   error
}

// NewStatusError classifies status (and rate-limit headers) into a sentinel.
func NewStatusError(method, path string, status int, h http.Header) *StatusError {
	e := &StatusError{Method: method, Path: path, Status: status}
	switch {
	case status == http.StatusForbidden && h.Get("X-Ratelimit-Remaining") == "0":
		e.sentinel = ErrUnavailable
		e.RetryAfter = "reset at " + h.Get("X-Ratelimit-Reset")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		e.sentinel = ErrAuth
	case status == http.StatusNotFound:
		e.sentinel = ErrNotFound
	default:
		e.sentinel = ErrUnavailable
		if ra := h.Get("Retry-After"); ra != "" {
			e.RetryAfter = "retry after " + ra
		}
	}
	return e
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("%s %s returned %d", e.Method, e.Path, e.Status)
	if e.RetryAfter != "" {
		msg += " (" + e.RetryAfter + ")"
	}
	return msg + ": " + e.sentinel.Error()
}

func (e *StatusError) Unwrap() error { return e.sentinel }
