package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jtumidanski/converge/internal/auth"
)

// TestClassifyDefaultBranchIsGitFailure pins classify()'s fallback for any
// unclassified error. Per ruling R27 there is no "INTERNAL" code in this
// project; an unclassified failure must report as GIT_FAILURE. This is the
// branch the controller specifically asked to be protected from INTERNAL
// reappearing silently.
func TestClassifyDefaultBranchIsGitFailure(t *testing.T) {
	status, code, _ := classify(errors.New("some unclassified failure"))
	if status != http.StatusInternalServerError || code != "GIT_FAILURE" {
		t.Fatalf("classify(unclassified) = (%d, %q), want (500, GIT_FAILURE)", status, code)
	}
}

// TestClassifyInterrupted pins the Task-19 ruling: a context.DeadlineExceeded
// (wrapped, as it would be flowing up through the call stack) must report as
// 503 INTERRUPTED, not the generic default and not some other status/code.
func TestClassifyInterrupted(t *testing.T) {
	err := fmt.Errorf("request context: %w", context.DeadlineExceeded)
	status, code, _ := classify(err)
	if status != http.StatusServiceUnavailable || code != "INTERRUPTED" {
		t.Fatalf("classify(DeadlineExceeded) = (%d, %q), want (503, INTERRUPTED)", status, code)
	}
}

// TestClassifyAuthNotFound pins Task 17's extension of the not-found switch
// case: an unknown or cross-user account row must answer 404 NOT_FOUND, the
// same as provider.ErrNotFound and session.ErrNotFound, never disclosing
// which of "no such row" or "not yours" applies.
func TestClassifyAuthNotFound(t *testing.T) {
	status, code, _ := classify(fmt.Errorf("lookup: %w", auth.ErrNotFound))
	if status != http.StatusNotFound || code != "NOT_FOUND" {
		t.Fatalf("classify(auth.ErrNotFound) = (%d, %q), want (404, NOT_FOUND)", status, code)
	}
}

// TestClassifyAuthError pins the *auth.Error arm: its own Code and Status
// travel through classify unchanged, mirroring session.ReviewError.
func TestClassifyAuthError(t *testing.T) {
	status, code, detail := classify(&auth.Error{Code: auth.CodeWeakPassword, Message: "too short"})
	if status != http.StatusUnprocessableEntity || code != "WEAK_PASSWORD" || detail != "too short" {
		t.Fatalf("classify(*auth.Error) = (%d, %q, %q), want (422, WEAK_PASSWORD, \"too short\")", status, code, detail)
	}
}
