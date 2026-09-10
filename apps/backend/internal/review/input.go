// Package review orchestrates reconstruction: resolve, apply, diff.
package review

import (
	"context"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/session"
)

// Request validation codes (PRD §5.4).
const (
	CodeInvalidProvider   session.Code = "INVALID_PROVIDER"
	CodeInvalidRepository session.Code = "INVALID_REPOSITORY"
	CodeInvalidBranch     session.Code = "INVALID_BRANCH"
	CodeInvalidChanges    session.Code = "INVALID_CHANGES"
)

// InputError is a client-fixable validation failure.
type InputError struct {
	Code    session.Code
	Field   string
	Message string
}

func (e *InputError) Error() string { return string(e.Code) + ": " + e.Message }

// CreateInput is a review build request.
type CreateInput struct {
	ProviderID string
	Repository string
	BaseBranch string
	Changes    []int
}

// Validate normalises and checks the request. An empty BaseBranch is allowed;
// the service substitutes the repository default before use.
func (in CreateInput) Validate(ctx context.Context, r gitx.Runner) (CreateInput, error) {
	if in.ProviderID == "" {
		return CreateInput{}, &InputError{Code: CodeInvalidProvider, Field: "provider", Message: "A provider must be selected."}
	}
	if err := gitx.ValidateRepoFullName(in.Repository); err != nil {
		return CreateInput{}, &InputError{Code: CodeInvalidRepository, Field: "repository", Message: "The repository must look like owner/name."}
	}
	if in.BaseBranch != "" {
		if err := gitx.ValidateBranch(ctx, r, in.BaseBranch); err != nil {
			return CreateInput{}, &InputError{Code: CodeInvalidBranch, Field: "baseBranch", Message: "The base branch name is not a valid git branch name."}
		}
	}
	if hasDuplicate(in.Changes) {
		return CreateInput{}, &InputError{Code: CodeInvalidChanges, Field: "changes", Message: "Each PR/MR may only be selected once."}
	}
	changes, err := gitx.ValidateChangeNumbers(in.Changes)
	if err != nil {
		return CreateInput{}, &InputError{Code: CodeInvalidChanges, Field: "changes", Message: "Select between 1 and 50 distinct PRs/MRs."}
	}
	in.Changes = changes
	return in, nil
}

// hasDuplicate reports whether numbers contains a repeated value. Unlike
// gitx.ValidateChangeNumbers (which silently de-duplicates for internal
// normalisation elsewhere), a client-supplied duplicate here is treated as a
// rejected request rather than silently collapsed, per TestCreateInputValidate.
func hasDuplicate(numbers []int) bool {
	seen := make(map[int]struct{}, len(numbers))
	for _, n := range numbers {
		if _, ok := seen[n]; ok {
			return true
		}
		seen[n] = struct{}{}
	}
	return false
}
