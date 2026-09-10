package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
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
