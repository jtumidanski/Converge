package review

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/session"
)

// Outcome is the result of applying one change.
type Outcome string

const (
	OutcomeApplied  Outcome = "applied"
	OutcomeEmpty    Outcome = "empty"
	OutcomeConflict Outcome = "conflict"
)

// ApplyResult reports what happened while applying a change.
type ApplyResult struct {
	Outcome          Outcome
	ConflictingPaths []string
	Commit           string // the landing SHA being applied when a conflict occurred
}

// ChangeApplicator applies one resolved change to a workspace.
//
// providerID identifies the hosting provider the change came from. It is not
// carried on session.ResolvedChange because a session is scoped to exactly one
// provider (session.Session.ProviderID()), so it is constant for every change
// in a session; it is passed in here only because FR-6.5 requires it in the
// commit trailer.
type ChangeApplicator interface {
	Apply(ctx context.Context, repoDir string, providerID string, rc session.ResolvedChange) (ApplyResult, error)
}

// CherryPickApplicator implements ChangeApplicator with git cherry-pick.
//
// Landing commits are picked one at a time, in original order (FR-6.4), with
// the strategy deciding the flags of each invocation:
//   - merge:  cherry-pick -m 1 --empty=keep <sha>  (replay the merge's diff
//     against parent 1, i.e. the mainline)
//   - squash: cherry-pick --empty=keep <sha>       (single squash commit)
//   - rebase: cherry-pick --empty=keep <sha_i>     (repeated per landing sha)
//
// Picking sequentially rather than handing git the whole list at once lets the
// applicator amend *every* synthetic commit with the Converge trailer
// (FR-6.5) — amending a non-tip commit after a batch pick would require
// rewriting history — and makes the SHA reported on conflict exact rather than
// inferred from CHERRY_PICK_HEAD.
type CherryPickApplicator struct {
	runner gitx.Runner
	log    *slog.Logger
}

// NewCherryPickApplicator builds the MVP applicator.
func NewCherryPickApplicator(r gitx.Runner, log *slog.Logger) *CherryPickApplicator {
	if log == nil {
		// Never reach for the package-global logger; a caller that supplies
		// no logger gets silence, not someone else's handler.
		log = slog.New(slog.DiscardHandler)
	}
	return &CherryPickApplicator{runner: r, log: log}
}

// Apply cherry-picks the change's landing commits in order.
//
// The change-level outcome is OutcomeEmpty only when every landing commit was
// empty; if any commit contributed content the outcome is OutcomeApplied. On a
// conflict the sha being applied at that moment is reported and the remaining
// shas are not attempted.
func (a *CherryPickApplicator) Apply(ctx context.Context, repoDir string, providerID string, rc session.ResolvedChange) (ApplyResult, error) {
	shas := rc.LandingSHAs()
	if len(shas) == 0 {
		return ApplyResult{}, fmt.Errorf("apply #%d: no landing shas", rc.Number())
	}
	for _, s := range shas {
		if err := gitx.ValidateSHA(s); err != nil {
			return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), err)
		}
	}

	applied := false
	for _, sha := range shas {
		res, err := a.pick(ctx, repoDir, providerID, rc, sha)
		if err != nil {
			return ApplyResult{}, err
		}
		if res.Outcome == OutcomeConflict {
			return res, nil
		}
		if res.Outcome == OutcomeApplied {
			applied = true
		}
	}
	if !applied {
		a.log.DebugContext(ctx, "cherry-pick produced no content", "change", rc.Number(), "shas", len(shas))
		return ApplyResult{Outcome: OutcomeEmpty}, nil
	}
	return ApplyResult{Outcome: OutcomeApplied}, nil
}

// pick cherry-picks a single landing sha and annotates the resulting commit.
// It returns OutcomeApplied, OutcomeEmpty (the commit was created but changed
// nothing) or OutcomeConflict for this one sha.
func (a *CherryPickApplicator) pick(ctx context.Context, repoDir, providerID string, rc session.ResolvedChange, sha string) (ApplyResult, error) {
	args := []string{"cherry-pick", "--empty=keep"}
	if rc.Strategy() == session.StrategyMerge {
		args = append(args, "-m", "1")
	}
	args = append(args, sha)

	before, err := a.head(ctx, repoDir)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), err)
	}

	if _, runErr := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: args, Category: gitx.CategoryCherryPick}); runErr != nil {
		// Only exit 1 means "the pick stopped on a conflict". Any other
		// non-zero exit (notably 128: bad revision, unmerged files left over
		// from a previous pick, other precondition failures) is a hard
		// failure, and a non-exit error means git never ran at all. Reporting
		// either as a conflict would attribute stale unmerged paths and a
		// stale CHERRY_PICK_HEAD to this change.
		if !gitx.IsExit(runErr, 1) {
			return ApplyResult{}, fmt.Errorf("apply #%d: cherry-pick %s: %w", rc.Number(), sha, runErr)
		}
		paths, listErr := a.conflictingPaths(ctx, repoDir)
		if listErr != nil {
			return ApplyResult{}, fmt.Errorf("apply #%d: list conflicting paths: %w", rc.Number(), listErr)
		}
		if len(paths) == 0 {
			// Exit 1 without unmerged paths is not the conflict shape we know
			// how to handle. Do not guess; surface it as an error.
			return ApplyResult{}, fmt.Errorf("apply #%d: cherry-pick failed without conflicting paths: %w", rc.Number(), runErr)
		}
		a.log.DebugContext(ctx, "cherry-pick conflicted", "change", rc.Number(), "commit", sha, "paths", len(paths))
		return ApplyResult{Outcome: OutcomeConflict, ConflictingPaths: paths, Commit: sha}, nil
	}

	// With --empty=keep, a cherry-pick whose content is already present still
	// creates a new (empty) commit and HEAD still moves, so comparing
	// before/after HEAD SHAs cannot detect "empty". Compare the resulting tree
	// against the tree before this pick instead.
	empty, err := a.treeUnchanged(ctx, repoDir, before)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), err)
	}

	// The commit exists either way (--empty=keep keeps it), and FR-6.5 wants
	// *each* synthetic commit attributable, so annotate before returning.
	if err := a.annotate(ctx, repoDir, providerID, rc); err != nil {
		return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), err)
	}
	if empty {
		return ApplyResult{Outcome: OutcomeEmpty}, nil
	}
	return ApplyResult{Outcome: OutcomeApplied}, nil
}

// Abort clears an in-progress cherry-pick. Used only by cleanup paths.
func (a *CherryPickApplicator) Abort(ctx context.Context, repoDir string) error {
	if _, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"cherry-pick", "--abort"}, Category: gitx.CategoryCherryPick}); err != nil {
		return fmt.Errorf("cherry-pick abort: %w", err)
	}
	return nil
}

// treeUnchanged reports whether HEAD's tree is identical to the tree at
// before, via `git diff --quiet`. Exit 0 means no difference (empty pick);
// exit 1 means a real difference (content applied); anything else is a
// genuine error, not one of the two outcomes above.
func (a *CherryPickApplicator) treeUnchanged(ctx context.Context, repoDir, before string) (bool, error) {
	_, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"diff", "--quiet", before, "HEAD"}, Category: gitx.CategoryDiff})
	if err == nil {
		return true, nil
	}
	if gitx.IsExit(err, 1) {
		return false, nil
	}
	return false, fmt.Errorf("diff --quiet %s HEAD: %w", before, err)
}

func (a *CherryPickApplicator) head(ctx context.Context, repoDir string) (string, error) {
	res, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"rev-parse", "HEAD"}, Category: gitx.CategoryQuery})
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// annotate rewrites the tip commit message with the Converge trailer
// (FR-6.5): "<original subject>\n\nConverge-Change: <provider-id>#<number>\nConverge-Source: <source sha>".
func (a *CherryPickApplicator) annotate(ctx context.Context, repoDir, providerID string, rc session.ResolvedChange) error {
	res, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"log", "-1", "--format=%B"}, Category: gitx.CategoryQuery})
	if err != nil {
		return fmt.Errorf("read commit message: %w", err)
	}
	body := strings.TrimRight(string(res.Stdout), "\n")
	msg := fmt.Sprintf("%s\n\nConverge-Change: %s#%d\nConverge-Source: %s\n", body, providerID, rc.Number(), rc.SourceSHA())
	spec := gitx.Spec{
		Dir:      repoDir,
		Args:     []string{"commit", "--amend", "--allow-empty", "--no-verify", "-F", "-"},
		Stdin:    bytes.NewReader([]byte(msg)),
		Category: gitx.CategoryCherryPick,
	}
	if _, err := a.runner.Run(ctx, spec); err != nil {
		return fmt.Errorf("amend commit message: %w", err)
	}
	return nil
}

// conflictingPaths parses `git diff --name-only --diff-filter=U -z`, whose
// output is a sequence of NUL-terminated paths (including a trailing NUL
// after the last entry, not a NUL-separated list). Splitting naively on NUL
// without first trimming that trailing terminator would produce a spurious
// empty final element.
func (a *CherryPickApplicator) conflictingPaths(ctx context.Context, repoDir string) ([]string, error) {
	res, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"diff", "--name-only", "--diff-filter=U", "-z"}, Category: gitx.CategoryDiff})
	if err != nil {
		return nil, fmt.Errorf("list conflicting paths: %w", err)
	}
	raw := bytes.TrimSuffix(res.Stdout, []byte{0})
	if len(raw) == 0 {
		return nil, nil
	}
	paths := strings.Split(string(raw), "\x00")
	// git diff --name-only already emits paths in tree order, which is
	// deterministic given a fixed worktree state; no further sort needed.
	return paths, nil
}
