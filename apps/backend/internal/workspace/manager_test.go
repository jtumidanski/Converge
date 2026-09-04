package workspace

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/testutil"
)

func logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestCreateCleanupRealGit(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()
	runner, err := gitx.NewExecRunner(logger(), gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	locks := &gitx.LockMap{}
	cache := mirror.New(t.TempDir(), runner, locks, logger())
	repo, _ := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("a/b").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	mirrorPath, err := cache.Ensure(context.Background(), fake.New("fake", provider.KindGitLab), repo)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(t.TempDir(), runner, locks, logger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repoDir, err := m.Create(ctx, mirrorPath, "0123abcd", base)
	if err != nil {
		t.Fatal(err)
	}
	if repoDir != filepath.Join(m.Root(), "0123abcd", "repo") {
		t.Fatalf("repoDir = %s", repoDir)
	}
	res, _ := runner.Run(ctx, gitx.Spec{Dir: repoDir, Args: []string{"rev-parse", "--abbrev-ref", "HEAD"}, Category: gitx.CategoryQuery})
	if strings.TrimSpace(string(res.Stdout)) != "review/0123abcd" {
		t.Fatalf("branch = %s", res.Stdout)
	}
	if _, err := m.Create(ctx, mirrorPath, "BAD", base); err == nil {
		t.Fatal("bad id accepted")
	}
	if err := m.Cleanup(ctx, mirrorPath, "0123abcd"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.SessionDir("0123abcd")); !os.IsNotExist(err) {
		t.Fatal("session dir still present")
	}
	res, err = runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"branch", "--list", "review/*"}, Category: gitx.CategoryQuery})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "" {
		t.Fatalf("branch not deleted: %q %v", res.Stdout, err)
	}
	res, err = runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"worktree", "list", "--porcelain"}, Category: gitx.CategoryQuery})
	if err != nil || strings.Count(string(res.Stdout), "worktree ") != 1 {
		t.Fatalf("worktree not removed: %q", res.Stdout)
	}
	if _, err := runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"fsck", "--no-progress"}, Category: gitx.CategoryQuery}); err != nil {
		t.Fatalf("mirror damaged: %v", err)
	}
	// idempotent
	if err := m.Cleanup(ctx, mirrorPath, "0123abcd"); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
	if err := m.Cleanup(ctx, "", "0123abcd"); err != nil {
		t.Fatalf("cleanup without mirror: %v", err)
	}
}

// TestCreateSerializesPerMirror proves the mirror lock is actually
// contended: concurrent Create calls against the same mirrorPath must not
// run their git invocations overlapping in time. A fake runner records entry
// and exit under a small artificial delay; if the lock were not held (or a
// pointer escaped it) two invocations would overlap and inFlight would spike
// above 1.
func TestCreateSerializesPerMirror(t *testing.T) {
	var inFlight int32
	var maxInFlight int32
	var mu sync.Mutex
	runner := &gitx.FakeRunner{Handler: func(_ gitx.Spec) (gitx.Result, error) {
		cur := atomic.AddInt32(&inFlight, 1)
		mu.Lock()
		if cur > maxInFlight {
			maxInFlight = cur
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return gitx.Result{}, nil
	}}
	locks := &gitx.LockMap{}
	m, err := New(t.TempDir(), runner, locks, logger())
	if err != nil {
		t.Fatal(err)
	}
	const mirrorPath = "/fake/mirror"
	ids := []string{"aaaaaaaa", "bbbbbbbb", "cccccccc"}
	var wg sync.WaitGroup
	errs := make([]error, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			_, errs[i] = m.Create(context.Background(), mirrorPath, id, strings.Repeat("a", 40))
		}(i, id)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	mu.Lock()
	got := maxInFlight
	mu.Unlock()
	if got != 1 {
		t.Fatalf("maxInFlight = %d, want 1 (lock did not serialize same-mirror creates)", got)
	}
}

func TestCleanupRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	m, err := New(root, &gitx.FakeRunner{}, &gitx.LockMap{}, logger())
	if err != nil {
		t.Fatal(err)
	}
	// symlinked session dir pointing outside the root
	if err := os.Symlink(outside, filepath.Join(m.Root(), "deadbeef")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = m.Cleanup(context.Background(), "", "deadbeef")
	if !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatal("outside file was deleted")
	}
	if err := m.RemoveDir("deadbeef"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("RemoveDir err = %v", err)
	}
	for _, bad := range []string{"..", "../x", "abc", "ABCDEF01", "0123456789"} {
		if err := m.RemoveDir(bad); err == nil {
			t.Errorf("RemoveDir(%q) accepted", bad)
		}
	}
	// a real directory is removed
	if err := os.MkdirAll(filepath.Join(m.Root(), "01234567", "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveDir("01234567"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Root(), "01234567")); !os.IsNotExist(err) {
		t.Fatal("dir remains")
	}
}
