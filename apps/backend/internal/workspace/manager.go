// Package workspace creates and destroys per-session git worktrees.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jtumidanski/converge/internal/gitx"
)

// ErrOutsideRoot is returned when a session path does not resolve inside the root.
var ErrOutsideRoot = errors.New("workspace: path is not a direct child of WORKSPACE_ROOT")

var sessionIDRe = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ValidateSessionID accepts 8 lowercase hex characters.
func ValidateSessionID(id string) error {
	if !sessionIDRe.MatchString(id) {
		return fmt.Errorf("%w: invalid session id", gitx.ErrInvalid)
	}
	return nil
}

// Manager owns WORKSPACE_ROOT: the parent directory under which every
// session gets its own worktree at <root>/<id>/repo.
type Manager struct {
	root   string
	runner gitx.Runner
	locks  *gitx.LockMap
	log    *slog.Logger
}

// New creates the root if needed and resolves it through symlinks so later
// guard checks compare against the real, canonical path.
func New(root string, runner gitx.Runner, locks *gitx.LockMap, log *slog.Logger) (*Manager, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("workspace: create root: %w", err)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve root: %w", err)
	}
	return &Manager{root: real, runner: runner, locks: locks, log: log}, nil
}

// Root returns the canonical WORKSPACE_ROOT path.
func (m *Manager) Root() string { return m.root }

// SessionDir returns <root>/<id>.
func (m *Manager) SessionDir(id string) string { return filepath.Join(m.root, id) }

// RepoDir returns <root>/<id>/repo.
func (m *Manager) RepoDir(id string) string { return filepath.Join(m.root, id, "repo") }

// BranchName returns the review branch name for a session id.
func (m *Manager) BranchName(id string) string { return "review/" + id }

// guard validates the id and, when the session directory exists, verifies it
// resolves (through any symlinks) to a direct child of the root named
// exactly id. Returns whether the directory exists, and a non-nil error if
// the id is invalid or the path escapes the root.
func (m *Manager) guard(id string) (bool, error) {
	if err := ValidateSessionID(id); err != nil {
		return false, err
	}
	dir := m.SessionDir(id)
	if _, err := os.Lstat(dir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("workspace: stat: %w", err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, fmt.Errorf("workspace: resolve: %w", err)
	}
	if filepath.Dir(real) != m.root || filepath.Base(real) != id {
		return true, ErrOutsideRoot
	}
	return true, nil
}

// Create adds a worktree on a new review/<id> branch at baseSHA, checked out
// from mirrorPath. It holds the mirror's lock for the duration of the git
// call so concurrent sessions against the same mirror serialize correctly.
func (m *Manager) Create(ctx context.Context, mirrorPath, id, baseSHA string) (string, error) {
	if err := ValidateSessionID(id); err != nil {
		return "", err
	}
	if err := gitx.ValidateSHA(baseSHA); err != nil {
		return "", err
	}
	if err := os.MkdirAll(m.SessionDir(id), 0o750); err != nil {
		return "", fmt.Errorf("workspace: create session dir: %w", err)
	}
	if _, err := m.guard(id); err != nil {
		return "", err
	}
	unlock := m.locks.Lock(mirrorPath)
	defer unlock()
	repoDir := m.RepoDir(id)
	spec := gitx.Spec{
		Dir:      mirrorPath,
		Args:     []string{"worktree", "add", "-b", m.BranchName(id), repoDir, baseSHA},
		Category: gitx.CategoryWorktree,
		Session:  id,
	}
	if _, err := m.runner.Run(ctx, spec); err != nil {
		return "", fmt.Errorf("workspace: worktree add: %w", err)
	}
	return repoDir, nil
}

// Cleanup removes the worktree, prunes, deletes the branch (FR-9.1 order),
// then removes the session directory. It is idempotent: calling it again for
// an already-cleaned-up session, or with an empty/absent mirrorPath, is not
// an error. Individual git cleanup steps are best-effort and logged rather
// than fatal, since the mirror may already be gone or the branch already
// deleted; but a failure to remove the session directory itself is a real
// error and is surfaced.
func (m *Manager) Cleanup(ctx context.Context, mirrorPath, id string) error {
	exists, err := m.guard(id)
	if err != nil {
		return err
	}
	if mirrorPath != "" {
		if _, statErr := os.Stat(filepath.Join(mirrorPath, "HEAD")); statErr == nil {
			unlock := m.locks.Lock(mirrorPath)
			steps := []struct {
				name string
				args []string
			}{
				{"worktree remove", []string{"worktree", "remove", "--force", m.RepoDir(id)}},
				{"worktree prune", []string{"worktree", "prune"}},
				{"branch delete", []string{"branch", "-D", m.BranchName(id)}},
			}
			for _, st := range steps {
				if _, runErr := m.runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: st.args, Category: gitx.CategoryCleanup, Session: id}); runErr != nil {
					m.log.Debug("cleanup step failed", slog.String("session", id), slog.String("step", st.name), slog.String("error", runErr.Error()))
				}
			}
			unlock()
		}
	}
	if !exists {
		return nil
	}
	if err := os.RemoveAll(m.SessionDir(id)); err != nil {
		m.log.Warn("cleanup remove failed", slog.String("session", id), slog.String("step", "remove dir"), slog.String("error", err.Error()))
		return fmt.Errorf("workspace: remove session dir: %w", err)
	}
	return nil
}

// RemoveDir deletes a session directory without touching any mirror. It is a
// guarded os.RemoveAll: the id must validate and, if the directory exists,
// must resolve to a direct child of the root.
func (m *Manager) RemoveDir(id string) error {
	exists, err := m.guard(id)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := os.RemoveAll(m.SessionDir(id)); err != nil {
		return fmt.Errorf("workspace: remove dir: %w", err)
	}
	return nil
}
