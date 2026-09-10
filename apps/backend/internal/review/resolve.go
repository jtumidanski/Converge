package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/session"
)

const resolveConcurrency = 4

// Resolved is the outcome of the resolve pipeline.
type Resolved struct {
	Changes    []session.ResolvedChange
	BaseSHA    string
	MirrorPath string
}

// Resolver turns selected change numbers into landing commits and a base SHA.
type Resolver struct {
	mirrors *mirror.Cache
	log     *slog.Logger
}

// NewResolver builds a Resolver.
func NewResolver(m *mirror.Cache, log *slog.Logger) *Resolver { return &Resolver{mirrors: m, log: log} }

// MapProviderError converts a provider error into a ReviewError, or nil if err
// is not one of the classified provider sentinels.
func MapProviderError(providerID, repo string, err error) *session.ReviewError {
	switch {
	case errors.Is(err, provider.ErrAuth):
		return &session.ReviewError{Code: session.CodeProviderAuth, Message: MsgProviderAuth(providerID)}
	case errors.Is(err, provider.ErrNotFound):
		return &session.ReviewError{Code: session.CodeRepositoryUnavailable, Message: MsgRepositoryUnavailable(repo)}
	case errors.Is(err, provider.ErrUnavailable):
		return &session.ReviewError{Code: session.CodeProviderUnavailable, Message: MsgProviderUnavailable(providerID)}
	default:
		return nil
	}
}

func asReviewError(err error) *session.ReviewError {
	var re *session.ReviewError
	if errors.As(err, &re) {
		return re
	}
	return nil
}

// Resolve implements design §6.1 steps 1-7: it loads every selected change,
// verifies each is merged and targets baseBranch, orders them by merge time,
// updates the mirror, classifies how each landed, verifies each landing
// commit is actually reachable from baseBranch, and derives the base SHA
// (the first parent of the earliest change's first landing commit).
//
// Every returned error is a *session.ReviewError; any ambiguity in base
// selection fails with CodeBaseUndetermined rather than guessing.
func (r *Resolver) Resolve(ctx context.Context, p provider.GitProvider, repo provider.Repository, baseBranch string, numbers []int, progress func(string)) (Resolved, error) {
	report := func(stage string) {
		if progress != nil {
			progress(stage)
		}
	}
	report(session.StageResolving)

	// Step 1: load every selected change (bounded concurrency).
	changes, err := r.fetchChanges(ctx, p, repo, numbers)
	if err != nil {
		return Resolved{}, err
	}

	// Step 2a: reject anything not merged before doing any git work. This
	// check only needs data the provider already returned, so it must run
	// before the mirror is fetched (which can cost a full clone, up to
	// GIT_CLONE_TIMEOUT_MINUTES): a request naming an unmerged PR/MR should
	// be rejected fast, not after paying for a mirror update it was always
	// going to fail. (Split from the target-mismatch check below, which
	// cannot run this early — see the comment there.)
	if err := checkMerged(changes); err != nil {
		return Resolved{}, err
	}

	// Step 2b (part of): make sure the mirror is up to date and the requested
	// base branch actually exists before judging anything about it. This
	// runs before the target-mismatch check below: a caller-requested base
	// branch that doesn't exist in the repository at all is an undetermined
	// base, not an "incompatible target" report about the selected changes
	// (deviation from the brief's illustrative ordering — its own test,
	// TestResolveRejectsUnmergedAndBadTargets's "missing branch" case,
	// requires CodeBaseUndetermined here, which only reaching BranchExists
	// first can produce). Unlike the NOT_MERGED check above, the target
	// check cannot be pushed earlier than this: it needs to lose to
	// BASE_UNDETERMINED specifically when the base branch itself is absent.
	report(session.StageUpdatingRepo)
	mirrorPath, err := r.mirrors.Ensure(ctx, p, repo)
	if err != nil {
		if re := MapProviderError(p.ID(), repo.FullName(), err); re != nil {
			return Resolved{}, re
		}
		return Resolved{}, &session.ReviewError{Code: session.CodeRepositoryUnavailable, Message: MsgRepositoryUnavailable(repo.FullName())}
	}
	objects := r.mirrors.Objects(mirrorPath, repo.FullName())
	ok, err := objects.BranchExists(ctx, baseBranch)
	if err != nil {
		return Resolved{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure()}
	}
	if !ok {
		return Resolved{}, &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(fmt.Sprintf("branch %s is not present in the repository", baseBranch))}
	}

	// Step 2c: reject changes merged to a different target.
	if err := checkTargets(changes, baseBranch); err != nil {
		return Resolved{}, err
	}

	// Step 3: order by merge time (change number tie-break) so the base is
	// derived from the earliest change deterministically, regardless of the
	// order the caller selected them in or the order goroutines completed.
	sort.SliceStable(changes, func(i, j int) bool {
		if !changes[i].MergedAt().Equal(changes[j].MergedAt()) {
			return changes[i].MergedAt().Before(changes[j].MergedAt())
		}
		return changes[i].Number() < changes[j].Number()
	})

	// Steps 5-6: classify how each change landed and verify it is reachable
	// from baseBranch (a landing commit that isn't an ancestor of the base
	// means the branch was rewritten or the change was reverted; presenting a
	// diff against it would be confidently wrong).
	resolved := make([]session.ResolvedChange, 0, len(changes))
	for _, cr := range changes {
		commits, err := p.GetChangeCommits(ctx, repo, cr.Number())
		if err != nil {
			if errors.Is(err, provider.ErrTooManyCommits) {
				return Resolved{}, &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(fmt.Sprintf("#%d has too many commits to verify", cr.Number())), Change: cr.Number()}
			}
			if re := MapProviderError(p.ID(), repo.FullName(), err); re != nil {
				return Resolved{}, re
			}
			return Resolved{}, &session.ReviewError{Code: session.CodeProviderUnavailable, Message: MsgProviderUnavailable(p.ID())}
		}
		landing, err := r.landingWithFetch(ctx, p, repo, objects, cr, commits)
		if err != nil {
			if re := asReviewError(err); re != nil {
				return Resolved{}, re
			}
			return Resolved{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure(), Change: cr.Number()}
		}
		for _, sha := range landing.SHAs {
			onBase, err := objects.IsAncestor(ctx, sha, baseBranch)
			if err != nil {
				return Resolved{}, &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure(), Change: cr.Number()}
			}
			if !onBase {
				return Resolved{}, &session.ReviewError{Code: session.CodeNotOnBaseBranch, Message: MsgNotOnBaseBranch(cr.Number(), baseBranch), Change: cr.Number(), Commit: sha}
			}
		}
		rc, err := session.NewResolvedChange(session.ResolvedChangeParams{
			Number: cr.Number(), Title: cr.Title(), Author: cr.Author(), WebURL: cr.WebURL(), MergedAt: cr.MergedAt(),
			Strategy: landing.Strategy, LandingSHAs: landing.SHAs, SourceSHA: landing.SourceSHA,
		})
		if err != nil {
			return Resolved{}, &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(err.Error()), Change: cr.Number()}
		}
		resolved = append(resolved, rc)
	}

	// Step 7: the base SHA is the first parent of the earliest change's first
	// (oldest) landing commit. If that commit has no first parent (e.g. a
	// root commit), that is BASE_UNDETERMINED, not a guess.
	first := resolved[0].LandingSHAs()[0]
	baseSHA, err := objects.RevParse(ctx, first+"^1")
	if err != nil {
		return Resolved{}, classifyRevParseErr(err, first, resolved[0].Number())
	}
	return Resolved{Changes: resolved, BaseSHA: baseSHA, MirrorPath: mirrorPath}, nil
}

// classifyRevParseErr maps a failure from the final base-SHA-parent lookup to
// a ReviewError. A clean exit-1 (rev-parse's way of saying "no such
// revision" — here, the commit has no first parent, e.g. a root commit) is a
// genuine BASE_UNDETERMINED. Anything else (a corrupted mirror, a cancelled
// context, or any other infrastructure failure) is not: mislabelling it "no
// first parent" would tell the user something false about their repository,
// so it propagates as the git failure it is. Mirrors the same exit-1
// discipline BranchExists and IsAncestor already apply in
// internal/mirror/objects.go.
func classifyRevParseErr(err error, first string, changeNumber int) *session.ReviewError {
	if gitx.IsExit(err, 1) {
		return &session.ReviewError{Code: session.CodeBaseUndetermined, Message: MsgBaseUndetermined(fmt.Sprintf("commit %s has no first parent", first[:7])), Change: changeNumber}
	}
	return &session.ReviewError{Code: session.CodeGitFailure, Message: MsgGitFailure(), Change: changeNumber}
}

// landingWithFetch resolves the landing commits, retrying once after fetching missing SHAs (FR-5.8).
func (r *Resolver) landingWithFetch(ctx context.Context, p provider.GitProvider, repo provider.Repository, o mirror.ObjectReader, cr provider.ChangeRequest, commits []provider.Commit) (Landing, error) {
	landing, err := ResolveLanding(ctx, o, cr, commits)
	if err == nil || !errors.Is(err, ErrNoCandidate) {
		return landing, err
	}
	for _, sha := range cr.LandingCandidates() {
		if fetchErr := r.mirrors.FetchSHA(ctx, p, repo, sha); fetchErr != nil {
			r.log.Debug("fetch-by-sha failed", slog.String("repository", repo.FullName()), slog.String("sha", shortSHA(sha)))
			continue
		}
		break
	}
	return ResolveLanding(ctx, o, cr, commits)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// fetchChanges loads every selected change with bounded concurrency. Results
// are written to a slice pre-sized to len(numbers) and indexed by the
// goroutine's own position, so output order is deterministic regardless of
// completion order and no shared mutable state needs a lock.
//
// On the first error, the shared context is cancelled so sibling goroutines
// still waiting on a semaphore slot or an in-flight provider call stop
// promptly instead of continuing to make provider calls a request that is
// already doomed. The caller's ctx is still honoured: if it was already
// cancelled/expired, that is reported rather than swallowed as fallout from
// this internal fail-fast cancellation.
func (r *Resolver) fetchChanges(ctx context.Context, p provider.GitProvider, repo provider.Repository, numbers []int) ([]provider.ChangeRequest, error) {
	out := make([]provider.ChangeRequest, len(numbers))
	errs := make([]error, len(numbers))
	sem := make(chan struct{}, resolveConcurrency)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for i, n := range numbers {
		wg.Add(1)
		go func(i, n int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-cctx.Done():
				errs[i] = cctx.Err()
				return
			}
			defer func() { <-sem }()
			cr, err := p.GetChange(cctx, repo, n)
			out[i], errs[i] = cr, err
			if err != nil {
				cancel() // fail fast: stop siblings still in flight
			}
		}(i, n)
	}
	wg.Wait()

	i, err := firstRealError(errs, ctx)
	if err == nil {
		return out, nil
	}
	if errors.Is(err, provider.ErrNotFound) {
		return nil, &session.ReviewError{Code: session.CodeRepositoryUnavailable, Message: MsgRepositoryUnavailable(repo.FullName()), Change: numbers[i]}
	}
	if re := MapProviderError(p.ID(), repo.FullName(), err); re != nil {
		re.Change = numbers[i]
		return nil, re
	}
	return nil, &session.ReviewError{Code: session.CodeProviderUnavailable, Message: MsgProviderUnavailable(p.ID()), Change: numbers[i]}
}

// firstRealError picks the first error in errs that isn't just fallout from
// fetchChanges' own fail-fast cancellation. A goroutine cancelled because a
// sibling failed reports context.Canceled even though nothing is actually
// wrong with the caller's request; surfacing that instead of the sibling's
// real error would be misleading. If the caller's own ctx was the one that
// was cancelled/expired, though, every context.Canceled/DeadlineExceeded is
// genuine and the first one found is returned as-is.
func firstRealError(errs []error, ctx context.Context) (int, error) {
	callerDone := ctx.Err() != nil
	for i, err := range errs {
		if err == nil {
			continue
		}
		if !callerDone && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			continue
		}
		return i, err
	}
	return -1, nil
}

// checkMerged rejects any selected change that isn't merged. Uses only data
// already returned by the provider, so it is deliberately cheap enough to
// run before any mirror work.
func checkMerged(changes []provider.ChangeRequest) error {
	var unmerged []int
	for _, cr := range changes {
		if cr.State() != provider.StateMerged {
			unmerged = append(unmerged, cr.Number())
		}
	}
	if len(unmerged) > 0 {
		return &session.ReviewError{Code: session.CodeNotMerged, Message: MsgNotMerged(unmerged), Change: unmerged[0]}
	}
	return nil
}

// checkTargets rejects changes merged to a target branch other than baseBranch.
func checkTargets(changes []provider.ChangeRequest, baseBranch string) error {
	var mismatches []TargetMismatch
	for _, cr := range changes {
		if cr.TargetBranch() != baseBranch {
			mismatches = append(mismatches, TargetMismatch{Number: cr.Number(), Target: cr.TargetBranch()})
		}
	}
	if len(mismatches) > 0 {
		return &session.ReviewError{Code: session.CodeIncompatibleTargets, Message: MsgIncompatibleTargets(mismatches), Change: mismatches[0].Number}
	}
	return nil
}
