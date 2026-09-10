package review

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/workspace"
)

// cleanerFixture wires a Cleaner over empty mirror and workspace roots. No git
// is ever run: every path exercised here either resolves no mirror at all or
// resolves one whose directory does not exist, so workspace.Cleanup skips its
// git steps. The runner is a FakeRunner so an unexpected git call would be
// visible rather than silently executed.
type cleanerFixture struct {
	cleaner *Cleaner
	ws      *workspace.Manager
	runner  *gitx.FakeRunner
}

func newCleanerFixture(t *testing.T, log *slog.Logger) *cleanerFixture {
	t.Helper()
	runner := &gitx.FakeRunner{}
	locks := &gitx.LockMap{}
	mirrors := mirror.New(t.TempDir(), runner, locks, testLog())
	ws, err := workspace.New(t.TempDir(), runner, locks, testLog())
	if err != nil {
		t.Fatal(err)
	}
	return &cleanerFixture{cleaner: NewCleaner(mirrors, ws, log), ws: ws, runner: runner}
}

func mustSession(t *testing.T, id string) session.Session {
	t.Helper()
	s, err := session.NewBuilder().SetID(id).SetProviderID("fake").SetRepository("atlas/server").
		SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(time.Now()).
		SetTTL(time.Hour).Build()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCleanerCleanupRemovesSessionDirectory(t *testing.T) {
	f := newCleanerFixture(t, testLog())
	sess := mustSession(t, "0123abcd")
	dir := f.ws.SessionDir(sess.ID())
	if err := os.MkdirAll(filepath.Join(dir, "repo"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, CombinedDiffFile), []byte("diff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.cleaner.Cleanup(context.Background(), sess); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("session dir remains: %v", err)
	}
	// Idempotent: a second Cleanup for an already-removed session is not an
	// error (the store retries cleanup on a later sweep).
	if err := f.cleaner.Cleanup(context.Background(), sess); err != nil {
		t.Errorf("repeat cleanup: %v", err)
	}
	if calls := f.runner.Snapshot(); len(calls) != 0 {
		t.Errorf("no git expected, got %d calls", len(calls))
	}
}

func TestCleanerCleanupUnresolvableMirrorReportsTheRealFailure(t *testing.T) {
	// A nil logger must be replaced with a discarding one, not the package
	// global: this test drives the Debug log inside the unresolvable branch.
	f := newCleanerFixture(t, nil)
	// The zero session has neither a provider id nor a repository, so
	// mirror.Cache.Path fails and the mirror path degrades to "". The session
	// id is then still invalid, and that failure is surfaced as itself rather
	// than being swallowed alongside the mirror one.
	err := f.cleaner.Cleanup(context.Background(), session.Session{})
	if err == nil {
		t.Fatal("cleanup of a zero session must fail")
	}
	if !errors.Is(err, gitx.ErrInvalid) {
		t.Errorf("err = %v, want wrapped gitx.ErrInvalid", err)
	}
	if calls := f.runner.Snapshot(); len(calls) != 0 {
		t.Errorf("no git expected, got %d calls", len(calls))
	}
}

func TestCleanerRemoveDir(t *testing.T) {
	f := newCleanerFixture(t, testLog())
	present := "0123abcd"
	if err := os.MkdirAll(f.ws.SessionDir(present), 0o750); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "existing directory is removed", id: present},
		{name: "unknown id is a no-op", id: "beefcafe"},
		{name: "invalid id is rejected", id: "../escape", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.cleaner.RemoveDir(context.Background(), tt.id)
			if tt.wantErr {
				if err == nil || !errors.Is(err, gitx.ErrInvalid) {
					t.Fatalf("err = %v, want wrapped gitx.ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("remove dir: %v", err)
			}
		})
	}
	if _, err := os.Stat(f.ws.SessionDir(present)); !os.IsNotExist(err) {
		t.Errorf("session dir remains: %v", err)
	}
}
