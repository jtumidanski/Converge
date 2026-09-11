package review

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/workspace"
)

// Cleaner adapts the workspace manager to session.Cleaner so the session
// store can drop a session's git state without ever calling git itself.
type Cleaner struct {
	mirrors    *mirror.Cache
	workspaces *workspace.Manager
	log        *slog.Logger
}

// NewCleaner builds the adapter. A nil logger is replaced with a discarding
// one rather than the package-global handler.
func NewCleaner(m *mirror.Cache, w *workspace.Manager, log *slog.Logger) *Cleaner {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Cleaner{mirrors: m, workspaces: w, log: log}
}

// Cleanup removes the worktree, branch and session directory.
//
// Resolving the mirror path is deliberately best effort: mirror.Cache.Path
// only fails when the session's provider id or repository name is malformed,
// which cannot produce a mirror that holds a worktree registration for this
// session in the first place. An empty mirror path makes workspace.Cleanup
// skip the mirror-side steps and still remove the session directory, which
// is the part that must not be leaked. The failure to *remove the session
// directory* is a real error and is returned as itself.
func (c *Cleaner) Cleanup(ctx context.Context, s session.Session) error {
	ns, err := mirror.NamespaceFor(scopeOf(s))
	if err != nil {
		return fmt.Errorf("cleaner: %w", err)
	}
	mirrorPath, err := c.mirrors.Path(ns, s.ProviderID(), s.Repository())
	if err != nil {
		c.log.Debug("cleanup without mirror path",
			slog.String("session", s.ID()),
			slog.String("error", err.Error()))
		mirrorPath = ""
	}
	if err := c.workspaces.Cleanup(ctx, mirrorPath, s.ID()); err != nil {
		return fmt.Errorf("review: cleanup session %s: %w", s.ID(), err)
	}
	return nil
}

// RemoveDir removes an orphaned session directory without touching a mirror.
func (c *Cleaner) RemoveDir(_ context.Context, id string) error {
	if err := c.workspaces.RemoveDir(id); err != nil {
		return fmt.Errorf("review: remove session dir %s: %w", id, err)
	}
	return nil
}

var _ session.Cleaner = (*Cleaner)(nil)
