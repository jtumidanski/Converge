package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/session"
)

// ErrNoCandidate reports that no candidate SHA exists in the mirror.
var ErrNoCandidate = errors.New("no landing candidate exists in the mirror")

// Landing is how one change landed on the target branch.
type Landing struct {
	Strategy  session.Strategy
	SHAs      []string // oldest first
	SourceSHA string   // the provider-reported candidate that was used
}

// noCandidateError carries the review error and unwraps to both sentinels:
// errors.Is(err, ErrNoCandidate) for the one-shot FetchSHA retry (task 13),
// and errors.As(err, &*session.ReviewError) for the API surface.
type noCandidateError struct{ re *session.ReviewError }

func (e *noCandidateError) Error() string   { return e.re.Error() }
func (e *noCandidateError) Unwrap() []error { return []error{ErrNoCandidate, e.re} }

func undetermined(number int, reason string) error {
	return &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(fmt.Sprintf("#%d: %s", number, reason)), Change: number}
}

// ResolveLanding walks the candidate chain (merge, squash, head) and classifies
// the first candidate present in the mirror. Design §6.2.
func ResolveLanding(ctx context.Context, o mirror.ObjectReader, cr provider.ChangeRequest, commits []provider.Commit) (Landing, error) {
	candidates := cr.LandingCandidates()
	if len(candidates) == 0 {
		return Landing{}, &session.ReviewError{
			Code:    session.CodeBaseUndetermined,
			Message: MsgBaseUndetermined(fmt.Sprintf("#%d: the provider reported no merge, squash or head commit", cr.Number())),
			Change:  cr.Number(),
		}
	}
	var found string
	for _, c := range candidates {
		ok, err := o.Exists(ctx, c)
		if err != nil {
			return Landing{}, err
		}
		if ok {
			found = c
			break
		}
	}
	if found == "" {
		return Landing{}, &noCandidateError{re: &session.ReviewError{
			Code:    session.CodeMissingCommits,
			Message: MsgMissingCommits(candidates),
			Change:  cr.Number(),
		}}
	}
	parents, err := o.Parents(ctx, found)
	if err != nil {
		return Landing{}, err
	}
	switch len(parents) {
	case 2:
		return Landing{Strategy: session.StrategyMerge, SHAs: []string{found}, SourceSHA: found}, nil
	case 1:
		return resolveSingleParent(ctx, o, cr, commits, found)
	default:
		return Landing{}, undetermined(cr.Number(), fmt.Sprintf("commit %s has %d parents", found[:7], len(parents)))
	}
}

func resolveSingleParent(ctx context.Context, o mirror.ObjectReader, cr provider.ChangeRequest, commits []provider.Commit, found string) (Landing, error) {
	n := len(commits)
	if n == 0 {
		n = cr.CommitCount()
	}
	if n <= 0 {
		n = 1
	}
	walk, err := o.FirstParentWalk(ctx, found, n)
	if err != nil {
		return Landing{}, err
	}
	if len(walk) == n && len(commits) == n {
		same, err := patchIDsMatch(ctx, o, walk, commits)
		if err != nil {
			return Landing{}, err
		}
		if same {
			if n > 1 {
				return Landing{Strategy: session.StrategyRebase, SHAs: walk, SourceSHA: found}, nil
			}
			return Landing{Strategy: session.StrategySquash, SHAs: []string{found}, SourceSHA: found}, nil
		}
	}
	if n == 1 {
		return Landing{Strategy: session.StrategySquash, SHAs: []string{found}, SourceSHA: found}, nil
	}
	// A squash of several commits: accept only when the candidate has a non-empty diff.
	pid, err := o.PatchID(ctx, found)
	if err != nil {
		return Landing{}, err
	}
	if pid == "" {
		return Landing{}, undetermined(cr.Number(), fmt.Sprintf("commit %s carries no changes and does not match the reported commits", found[:7]))
	}
	return Landing{Strategy: session.StrategySquash, SHAs: []string{found}, SourceSHA: found}, nil
}

// patchIDsMatch compares the walked commits against the provider's commits as multisets.
func patchIDsMatch(ctx context.Context, o mirror.ObjectReader, walk []string, commits []provider.Commit) (bool, error) {
	counts := map[string]int{}
	for _, s := range walk {
		id, err := o.PatchID(ctx, s)
		if err != nil {
			return false, err
		}
		if id == "" {
			return false, nil
		}
		counts[id]++
	}
	for _, c := range commits {
		id, err := o.PatchID(ctx, c.SHA())
		if err != nil {
			return false, nil // the original commit may be gone after a rebase
		}
		if id == "" {
			return false, nil
		}
		counts[id]--
		if counts[id] < 0 {
			return false, nil
		}
	}
	for _, v := range counts {
		if v != 0 {
			return false, nil
		}
	}
	return true, nil
}
