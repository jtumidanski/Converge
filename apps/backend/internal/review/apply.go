package review

import (
	"bytes"
	"context"
	"errors"
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
type ChangeApplicator interface {
	Apply(ctx context.Context, repoDir string, rc session.ResolvedChange) (ApplyResult, error)
}

// CherryPickApplicator implements ChangeApplicator with git cherry-pick.
//
// The three landing strategies map to distinct cherry-pick invocations:
//   - merge:  cherry-pick -m 1 --empty=keep <sha>       (replay the merge's
//     diff against parent 1, i.e. the mainline)
//   - squash: cherry-pick --empty=keep <sha>             (single squash commit)
//   - rebase: cherry-pick --empty=keep <sha1> ... <shaN> (the full ordered
//     chain of rebased commits, replayed one by one)
//
// On success, only the tip commit (the last one cherry-picked) has its
// message rewritten with the Converge trailer (FR-6.5); intermediate commits
// in a rebase pick are left as-is, since the trailer only needs to map the
// group back to its PR/MR once.
type CherryPickApplicator struct {
	runner gitx.Runner
	log    *slog.Logger
}

// NewCherryPickApplicator builds the MVP applicator.
func NewCherryPickApplicator(r gitx.Runner, log *slog.Logger) *CherryPickApplicator {
	return &CherryPickApplicator{runner: r, log: log}
}

// Apply cherry-picks the change's landing commits in order.
func (a *CherryPickApplicator) Apply(ctx context.Context, repoDir string, rc session.ResolvedChange) (ApplyResult, error) {
	shas := rc.LandingSHAs()
	if len(shas) == 0 {
		return ApplyResult{}, fmt.Errorf("apply #%d: no landing shas", rc.Number())
	}
	for _, s := range shas {
		if err := gitx.ValidateSHA(s); err != nil {
			return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), err)
		}
	}

	args := []string{"cherry-pick", "--empty=keep"}
	if rc.Strategy() == session.StrategyMerge {
		args = append(args, "-m", "1")
	}
	args = append(args, shas...)

	before, err := a.head(ctx, repoDir)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), err)
	}

	_, runErr := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: args, Category: gitx.CategoryCherryPick})
	if runErr != nil {
		var exitErr *gitx.ExitError
		if !errors.As(runErr, &exitErr) {
			// The process failed to start, timed out, or some other
			// non-exit condition occurred. This is never a conflict or an
			// empty pick: classify it as a genuine error rather than
			// guessing at the outcome.
			return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), runErr)
		}

		paths, listErr := a.conflictingPaths(ctx, repoDir)
		if listErr != nil {
			return ApplyResult{}, fmt.Errorf("apply #%d: list conflicting paths: %w", rc.Number(), listErr)
		}
		if len(paths) == 0 {
			// git exited non-zero but left no unmerged paths behind: this is
			// not the conflict shape we know how to handle (e.g. a bad
			// revision, a dirty worktree precondition failure). Do not
			// guess; surface it as an error.
			return ApplyResult{}, fmt.Errorf("apply #%d: cherry-pick failed without conflicting paths: %w", rc.Number(), runErr)
		}
		return ApplyResult{
			Outcome:          OutcomeConflict,
			ConflictingPaths: paths,
			Commit:           a.currentPickSHA(ctx, repoDir, shas),
		}, nil
	}

	// With --empty=keep, a cherry-pick whose content is already present
	// still creates a new (empty) commit and HEAD still moves, so comparing
	// before/after HEAD SHAs cannot detect "empty". Instead compare the
	// resulting tree against the tree before this Apply call: if they are
	// identical, nothing was actually applied, regardless of how many
	// commit objects were created along the way.
	empty, err := a.treeUnchanged(ctx, repoDir, before)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), err)
	}
	if empty {
		return ApplyResult{Outcome: OutcomeEmpty}, nil
	}

	if err := a.annotate(ctx, repoDir, rc); err != nil {
		return ApplyResult{}, fmt.Errorf("apply #%d: %w", rc.Number(), err)
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
// (FR-6.5): "<original subject>\n\nConverge-Change: #<number>\nConverge-Source: <source sha>".
// The trailer deliberately omits the provider id, since session.ResolvedChange
// does not carry one; the provider is recorded once in session.json.
func (a *CherryPickApplicator) annotate(ctx context.Context, repoDir string, rc session.ResolvedChange) error {
	res, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"log", "-1", "--format=%B"}, Category: gitx.CategoryQuery})
	if err != nil {
		return fmt.Errorf("read commit message: %w", err)
	}
	body := strings.TrimRight(string(res.Stdout), "\n")
	msg := fmt.Sprintf("%s\n\nConverge-Change: #%d\nConverge-Source: %s\n", body, rc.Number(), rc.SourceSHA())
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

// currentPickSHA reports which landing commit git was applying when the
// cherry-pick stopped, falling back to the first requested sha if
// CHERRY_PICK_HEAD cannot be resolved (defensive only; git always leaves it
// set on a real conflict).
func (a *CherryPickApplicator) currentPickSHA(ctx context.Context, repoDir string, shas []string) string {
	res, err := a.runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"rev-parse", "--verify", "--quiet", "CHERRY_PICK_HEAD"}, Category: gitx.CategoryQuery})
	if err == nil {
		if s := strings.TrimSpace(string(res.Stdout)); s != "" {
			return s
		}
	}
	return shas[0]
}
