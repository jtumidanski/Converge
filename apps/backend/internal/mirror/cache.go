// Package mirror manages bare mirror clones under REPOSITORY_CACHE_ROOT.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/identity"
	"github.com/jtumidanski/converge/internal/provider"
)

var providerIDRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// userIDRe is what auth.NewID produces: 16 lowercase hex characters. A user
// id becomes a directory name under the cache root, so it is validated
// against the exact shape it is generated in — the same posture providerIDRe
// and gitx.ValidateRepoFullName already take — rather than trusted because it
// came from an authenticated session.
var userIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Namespace isolates one user's mirrors under the cache root.
//
// The zero value is the root namespace: mirrors live directly under the root,
// exactly as they did before hosted mode existed, which is why an existing
// deployment's mirrors are still found after an upgrade (FR-6.5).
type Namespace struct {
	userID string
}

// RootNamespace is the standalone namespace: today's flat layout.
func RootNamespace() Namespace { return Namespace{} }

// NamespaceFor derives the namespace for a scope. A standalone scope yields
// the root namespace; a scoped one yields users/<user-id>.
func NamespaceFor(scope identity.Scope) (Namespace, error) {
	id := scope.UserID()
	if id == "" {
		return Namespace{}, nil
	}
	if !userIDRe.MatchString(id) {
		return Namespace{}, fmt.Errorf("mirror: invalid user id")
	}
	return Namespace{userID: id}, nil
}

// IsRoot reports whether this is the unscoped namespace.
func (n Namespace) IsRoot() bool { return n.userID == "" }

func (n Namespace) segments() []string {
	if n.userID == "" {
		return nil
	}
	return []string{"users", n.userID}
}

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

// Path returns <root>[/users/<user-id>]/<providerID>/<fullName>.git after
// validation.
//
// Two users reviewing the same repository therefore maintain two mirrors.
// That trades disk for a guarantee that mirror contents cannot cross an
// account boundary. No per-user cap ships in this task (design §6).
func (c *Cache) Path(ns Namespace, providerID, fullName string) (string, error) {
	if !providerIDRe.MatchString(providerID) {
		return "", fmt.Errorf("mirror: invalid provider id %q", providerID)
	}
	if err := gitx.ValidateRepoFullName(fullName); err != nil {
		return "", fmt.Errorf("mirror: %w", err)
	}
	parts := append([]string{c.root}, ns.segments()...)
	parts = append(parts, providerID)
	parts = append(parts, strings.Split(fullName, "/")...)
	parts[len(parts)-1] += ".git"
	return filepath.Join(parts...), nil
}

// NamespaceRoot returns the directory holding every mirror in ns.
func (c *Cache) NamespaceRoot(ns Namespace) (string, error) {
	if ns.IsRoot() {
		return "", errors.New("mirror: the root namespace has no dedicated directory")
	}
	return filepath.Join(append([]string{c.root}, ns.segments()...)...), nil
}

// PurgeNamespace removes a user's entire mirror tree. Called when an account
// is deleted (FR-2.7, FR-6.6). The root namespace is refused: deleting the
// whole cache root is never what account deletion means, and a zero-value
// Namespace reaching here would otherwise wipe every user's mirrors.
func (c *Cache) PurgeNamespace(ns Namespace) error {
	dir, err := c.NamespaceRoot(ns)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("mirror: purge namespace: %w", err)
	}
	return nil
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
func (c *Cache) Ensure(ctx context.Context, ns Namespace, p provider.GitProvider, repo provider.Repository) (string, error) {
	path, err := c.Path(ns, p.ID(), repo.FullName())
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
func (c *Cache) FetchSHA(ctx context.Context, ns Namespace, p provider.GitProvider, repo provider.Repository, sha string) error {
	if err := gitx.ValidateSHA(sha); err != nil {
		return err
	}
	path, err := c.Path(ns, p.ID(), repo.FullName())
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
