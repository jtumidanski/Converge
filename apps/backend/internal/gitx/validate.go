package gitx

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// MaxChanges is the maximum number of changes in one review.
const MaxChanges = 50

var (
	shaRe      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repoRe     = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`)
	branchBad  = regexp.MustCompile(`[\x00-\x20\x7f~^:?*\[\\]|\.\.|@\{|//|\.lock$|\.lock/|^/|/$|^\.|/\.|^@$`)
	ErrInvalid = errors.New("invalid argument")
)

// ValidateSHA accepts exactly 40 lowercase hex characters.
func ValidateSHA(s string) error {
	if !shaRe.MatchString(s) {
		return fmt.Errorf("%w: sha must be 40 lowercase hex characters", ErrInvalid)
	}
	return nil
}

// ValidateRepoFullName accepts owner/name-style repository identifiers only,
// rejecting anything that could traverse paths or look like a flag.
func ValidateRepoFullName(s string) error {
	if !repoRe.MatchString(s) {
		return fmt.Errorf("%w: repository must look like owner/name", ErrInvalid)
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, ".") {
		return fmt.Errorf("%w: repository must not start with /, - or . characters", ErrInvalid)
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, "..") {
			return fmt.Errorf("%w: repository contains an invalid path segment", ErrInvalid)
		}
	}
	return nil
}

// ValidateBranchSyntax is the pre-check applied before git sees the name.
func ValidateBranchSyntax(s string) error {
	if s == "" || strings.HasPrefix(s, "-") || branchBad.MatchString(s) || len(s) > 255 {
		return fmt.Errorf("%w: invalid branch name", ErrInvalid)
	}
	return nil
}

// ValidateBranch pre-checks the syntax then asks git check-ref-format --branch.
func ValidateBranch(ctx context.Context, r Runner, s string) error {
	if err := ValidateBranchSyntax(s); err != nil {
		return err
	}
	if _, err := r.Run(ctx, Spec{Args: []string{"check-ref-format", "--branch", s}, Category: CategoryQuery}); err != nil {
		return fmt.Errorf("%w: git rejected branch name", ErrInvalid)
	}
	return nil
}

// ValidateChangeNumber accepts positive integers.
func ValidateChangeNumber(n int) error {
	if n <= 0 {
		return fmt.Errorf("%w: change number must be positive", ErrInvalid)
	}
	return nil
}

// ValidateChangeNumbers de-duplicates, sorts, and bounds the list.
func ValidateChangeNumbers(in []int) ([]int, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("%w: at least one change is required", ErrInvalid)
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, n := range in {
		if err := ValidateChangeNumber(n); err != nil {
			return nil, err
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	if len(out) > MaxChanges {
		return nil, fmt.Errorf("%w: at most %d changes per review", ErrInvalid, MaxChanges)
	}
	sort.Ints(out)
	return out, nil
}

// ValidatePathArg rejects values that git could parse as options.
func ValidatePathArg(s string) error {
	if s == "" || strings.HasPrefix(s, "-") || strings.ContainsRune(s, 0) {
		return fmt.Errorf("%w: invalid path argument", ErrInvalid)
	}
	return nil
}
