// Package mirror manages bare mirror clones under REPOSITORY_CACHE_ROOT.
package mirror

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

var providerIDRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// Cache owns the mirror directory tree.
type Cache struct {
	root   string
	runner gitx.Runner
	locks  *gitx.LockMap
	log    *slog.Logger
}

// New creates a cache rooted at root.
func New(root string, runner gitx.Runner, locks *gitx.LockMap, log *slog.Logger) *Cache {
	return &Cache{root: root, runner: runner, locks: locks, log: log}
}

// Path returns <root>/<providerID>/<fullName>.git after validation.
func (c *Cache) Path(providerID, fullName string) (string, error) {
	if !providerIDRe.MatchString(providerID) {
		return "", fmt.Errorf("mirror: invalid provider id %q", providerID)
	}
	if err := gitx.ValidateRepoFullName(fullName); err != nil {
		return "", fmt.Errorf("mirror: %w", err)
	}
	parts := append([]string{c.root, providerID}, strings.Split(fullName, "/")...)
	parts[len(parts)-1] += ".git"
	return filepath.Join(parts...), nil
}

// Lock takes the per-mirror mutex. The returned func releases it.
//
// The lock is a plain, non-reentrant sync.Mutex (via gitx.LockMap). A caller
// that already holds this lock for mirrorPath — e.g. from inside a Cache
// operation such as Ensure or FetchSHA, which take it internally — must not
// call Lock again for the same mirrorPath on the same goroutine before
// releasing it; doing so self-deadlocks.
func (c *Cache) Lock(mirrorPath string) func() { return c.locks.Lock(mirrorPath) }

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Ensure clones the mirror on first use, otherwise updates it. Holds the mirror lock.
func (c *Cache) Ensure(ctx context.Context, p provider.GitProvider, repo provider.Repository) (string, error) {
	path, err := c.Path(p.ID(), repo.FullName())
	if err != nil {
		return "", err
	}
	unlock := c.Lock(path)
	defer unlock()
	var spec gitx.Spec
	if exists(filepath.Join(path, "HEAD")) {
		// The refspec is spelled out rather than left to `remote update` so
		// the review namespace can be excluded: pruning it would delete the
		// branches live session worktrees have checked out (see
		// gitx.ExcludeReviewRefspec).
		spec = gitx.Spec{Dir: path, Args: []string{"fetch", "--prune", "origin", gitx.MirrorRefspec, gitx.ExcludeReviewRefspec}, Category: gitx.CategoryFetch, Repo: repo.FullName()}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return "", fmt.Errorf("mirror: create parent: %w", err)
		}
		spec = gitx.Spec{Args: []string{"clone", "--mirror", p.CloneURL(repo), path}, Category: gitx.CategoryClone, Repo: repo.FullName()}
	}
	if err := p.AuthorizeGit(repo, &spec); err != nil {
		return "", fmt.Errorf("mirror: authorize: %w", err)
	}
	if _, err := c.runner.Run(ctx, spec); err != nil {
		if spec.Category == gitx.CategoryClone {
			_ = os.RemoveAll(path) // never leave a half clone behind
		}
		return "", fmt.Errorf("mirror: %s: %w", spec.Category, err)
	}
	c.log.Info("mirror ready", slog.String("repository", repo.FullName()), slog.String("git.category", string(spec.Category)))
	return path, nil
}

// FetchSHA tries once to fetch a specific commit. Holds the mirror lock.
func (c *Cache) FetchSHA(ctx context.Context, p provider.GitProvider, repo provider.Repository, sha string) error {
	if err := gitx.ValidateSHA(sha); err != nil {
		return err
	}
	path, err := c.Path(p.ID(), repo.FullName())
	if err != nil {
		return err
	}
	unlock := c.Lock(path)
	defer unlock()
	spec := gitx.Spec{Dir: path, Args: []string{"fetch", "origin", sha}, Category: gitx.CategoryFetch, Repo: repo.FullName()}
	if err := p.AuthorizeGit(repo, &spec); err != nil {
		return err
	}
	if _, err := c.runner.Run(ctx, spec); err != nil {
		return fmt.Errorf("mirror: fetch %s: %w", sha[:7], err)
	}
	return nil
}
