package mirror

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jtumidanski/converge/internal/gitx"
)

// ObjectReader answers read-only questions about commits in a mirror.
type ObjectReader interface {
	Exists(ctx context.Context, sha string) (bool, error)
	Parents(ctx context.Context, sha string) ([]string, error)
	PatchID(ctx context.Context, sha string) (string, error)
	FirstParentWalk(ctx context.Context, sha string, n int) ([]string, error)
	IsAncestor(ctx context.Context, sha, branch string) (bool, error)
	RevParse(ctx context.Context, rev string) (string, error)
	BranchExists(ctx context.Context, branch string) (bool, error)
}

type objects struct {
	dir    string
	repo   string
	runner gitx.Runner
}

// Objects returns an ObjectReader for a mirror path (no lock needed for reads).
func (c *Cache) Objects(mirrorPath, repoName string) ObjectReader {
	return &objects{dir: mirrorPath, repo: repoName, runner: c.runner}
}

func (o *objects) run(ctx context.Context, stdin []byte, args ...string) (string, error) {
	spec := gitx.Spec{Dir: o.dir, Args: args, Category: gitx.CategoryQuery, Repo: o.repo}
	if stdin != nil {
		spec.Stdin = bytes.NewReader(stdin)
	}
	res, err := o.runner.Run(ctx, spec)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func (o *objects) Exists(ctx context.Context, sha string) (bool, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return false, err
	}
	// rev-parse --verify --quiet cleanly separates "object not present" (exit
	// 1, silent) from fatal conditions (exit 128: corrupted mirror, missing
	// git dir, unreadable object database, etc). cat-file -e was tried first
	// but rejected: empirically it reports both "object absent" and "fatal,
	// broken repository" as the same exit code 128, so exit code alone can't
	// distinguish them for that command. Matches the plumbing RevParse already
	// uses below.
	_, err := o.run(ctx, nil, "rev-parse", "--verify", "--quiet", sha+"^{commit}")
	if err == nil {
		return true, nil
	}
	if gitx.IsExit(err, 1) {
		return false, nil
	}
	return false, fmt.Errorf("exists %s: %w", sha[:7], err)
}

func (o *objects) Parents(ctx context.Context, sha string) ([]string, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return nil, err
	}
	out, err := o.run(ctx, nil, "rev-list", "--parents", "-n", "1", sha)
	if err != nil {
		return nil, fmt.Errorf("parents of %s: %w", sha[:7], err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return nil, fmt.Errorf("parents of %s: empty output", sha[:7])
	}
	return fields[1:], nil
}

func (o *objects) PatchID(ctx context.Context, sha string) (string, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return "", err
	}
	res, err := o.runner.Run(ctx, gitx.Spec{Dir: o.dir, Args: []string{"show", "--format=", "--no-color", "-p", sha}, Category: gitx.CategoryQuery, Repo: o.repo})
	if err != nil {
		return "", fmt.Errorf("show %s: %w", sha[:7], err)
	}
	out, err := o.run(ctx, res.Stdout, "patch-id", "--stable")
	if err != nil {
		return "", fmt.Errorf("patch-id %s: %w", sha[:7], err)
	}
	if out == "" {
		return "", nil // empty commit
	}
	return strings.Fields(out)[0], nil
}

func (o *objects) FirstParentWalk(ctx context.Context, sha string, n int) ([]string, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return nil, err
	}
	if n <= 0 {
		return nil, fmt.Errorf("walk: n must be positive")
	}
	out, err := o.run(ctx, nil, "rev-list", "--first-parent", "--max-count="+strconv.Itoa(n), sha)
	if err != nil {
		return nil, fmt.Errorf("first-parent walk from %s: %w", sha[:7], err)
	}
	newestFirst := strings.Fields(out)
	oldestFirst := make([]string, len(newestFirst))
	for i, s := range newestFirst {
		oldestFirst[len(newestFirst)-1-i] = s
	}
	return oldestFirst, nil
}

func (o *objects) IsAncestor(ctx context.Context, sha, branch string) (bool, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return false, err
	}
	if err := gitx.ValidateBranchSyntax(branch); err != nil {
		return false, err
	}
	_, err := o.run(ctx, nil, "merge-base", "--is-ancestor", sha, "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	if gitx.IsExit(err, 1) {
		return false, nil
	}
	return false, err
}

func (o *objects) RevParse(ctx context.Context, rev string) (string, error) {
	if err := gitx.ValidatePathArg(rev); err != nil {
		return "", err
	}
	out, err := o.run(ctx, nil, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("rev-parse %s: %w", rev, err)
	}
	return out, nil
}

func (o *objects) BranchExists(ctx context.Context, branch string) (bool, error) {
	if err := gitx.ValidateBranchSyntax(branch); err != nil {
		return false, err
	}
	_, err := o.run(ctx, nil, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	if gitx.IsExit(err, 1) {
		return false, nil
	}
	return false, err
}
