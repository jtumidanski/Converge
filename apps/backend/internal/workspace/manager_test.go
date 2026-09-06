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
	runner, err := gitx.NewExecRunner(logger(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
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

// TestNewResolvesSymlinkedRoot proves that passing a symlink as the
// WORKSPACE_ROOT argument to New still gives Create/Cleanup/RemoveDir a
// canonical root, so guard()'s exact-equality comparisons (which run against
// m.root) hold for sessions created through the symlink.
func TestNewResolvesSymlinkedRoot(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real-root")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "root-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	m, err := New(link, &gitx.FakeRunner{}, &gitx.LockMap{}, logger())
	if err != nil {
		t.Fatal(err)
	}
	if m.Root() != real {
		t.Fatalf("Root() = %q, want canonical %q", m.Root(), real)
	}

	// RemoveDir through the canonical manager: a real directory under the
	// symlinked root is created directly (bypassing Create, since Create
	// needs a real git mirror) and must still guard and delete correctly.
	if err := os.MkdirAll(filepath.Join(m.Root(), "01234567", "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveDir("01234567"); err != nil {
		t.Fatalf("RemoveDir through symlinked root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(real, "01234567")); !os.IsNotExist(err) {
		t.Fatal("dir remains under real root")
	}

	// The guard must still refuse an escape reached via the symlinked root:
	// symlink <root>/<id> out to a directory outside the (real) root.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(m.Root(), "deadbeef")); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveDir("deadbeef"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("RemoveDir err = %v, want ErrOutsideRoot", err)
	}

	// End-to-end Create/Cleanup through the symlinked root, against a real
	// git mirror, proves the whole lifecycle works with a symlinked
	// WORKSPACE_ROOT, not just RemoveDir.
	src := testutil.NewRepo(t)
	base := src.Head()
	runner, rerr := gitx.NewExecRunner(logger(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if rerr != nil {
		t.Fatal(rerr)
	}
	defer runner.Close()
	locks := &gitx.LockMap{}
	cache := mirror.New(t.TempDir(), runner, locks, logger())
	repo, _ := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("a/b").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	mirrorPath, merr := cache.Ensure(context.Background(), fake.New("fake", provider.KindGitLab), repo)
	if merr != nil {
		t.Fatal(merr)
	}
	linkedM, err := New(link, runner, locks, logger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repoDir, err := linkedM.Create(ctx, mirrorPath, "cafef00d", base)
	if err != nil {
		t.Fatalf("Create through symlinked root: %v", err)
	}
	if repoDir != filepath.Join(real, "cafef00d", "repo") {
		t.Fatalf("repoDir = %s, want under canonical root %s", repoDir, real)
	}
	if err := linkedM.Cleanup(ctx, mirrorPath, "cafef00d"); err != nil {
		t.Fatalf("Cleanup through symlinked root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(real, "cafef00d")); !os.IsNotExist(err) {
		t.Fatal("session dir remains under real root after Cleanup")
	}
}

// TestCleanupContinuesPastGenuineGitFailure pins FR-9.1's documented
// behaviour: a genuine (non-"already gone") failure from any git cleanup
// step must NOT abort the sequence. All three steps must still be attempted,
// and the session directory must still be removed, even though the runner
// fails every single git call. A future change that turns this into an early
// return breaks idempotent cleanup (FR-9.4) and must fail this test.
func TestCleanupContinuesPastGenuineGitFailure(t *testing.T) {
	root := t.TempDir()
	mirrorPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(mirrorPath, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls []string
	var mu sync.Mutex
	failure := errors.New("permission denied")
	runner := &gitx.FakeRunner{Handler: func(s gitx.Spec) (gitx.Result, error) {
		mu.Lock()
		calls = append(calls, strings.Join(s.Args, " "))
		mu.Unlock()
		return gitx.Result{}, failure
	}}

	logBuf := &syncBuffer{}
	log := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	m, err := New(root, runner, &gitx.LockMap{}, log)
	if err != nil {
		t.Fatal(err)
	}
	// Create a real session directory so guard() reports exists=true, which
	// is the "genuine failure, not a routine repeat" case this test targets.
	if err := os.MkdirAll(filepath.Join(m.Root(), "01234567", "repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	err = m.Cleanup(context.Background(), mirrorPath, "01234567")
	if err != nil {
		t.Fatalf("Cleanup returned an error despite RemoveAll succeeding: %v", err)
	}

	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()
	wantCalls := []string{
		"worktree remove --force " + m.RepoDir("01234567"),
		"worktree prune",
		"branch -D " + m.BranchName("01234567"),
	}
	if len(gotCalls) != len(wantCalls) {
		t.Fatalf("git steps attempted = %v, want all three steps despite failures: %v", gotCalls, wantCalls)
	}
	for i := range wantCalls {
		if gotCalls[i] != wantCalls[i] {
			t.Fatalf("step %d = %q, want %q", i, gotCalls[i], wantCalls[i])
		}
	}

	if _, err := os.Stat(m.SessionDir("01234567")); !os.IsNotExist(err) {
		t.Fatal("session dir still present: Cleanup must still remove it despite git-step failures")
	}

	logOut := logBuf.String()
	if strings.Count(logOut, "cleanup step failed") != 3 {
		t.Fatalf("expected all 3 git-step failures logged, got:\n%s", logOut)
	}
	if !strings.Contains(logOut, "session=01234567") {
		t.Fatalf("log missing session id:\n%s", logOut)
	}
	if !strings.Contains(logOut, "step=\"worktree remove\"") || !strings.Contains(logOut, "step=\"worktree prune\"") || !strings.Contains(logOut, "step=\"branch delete\"") {
		t.Fatalf("log missing step identifiers:\n%s", logOut)
	}
	if strings.Contains(logOut, "level=DEBUG") {
		t.Fatalf("a genuine failure (session dir exists) must log at Warn, not Debug:\n%s", logOut)
	}
	if !strings.Contains(logOut, "level=WARN") {
		t.Fatalf("expected Warn-level log for a genuine git-step failure:\n%s", logOut)
	}
}

// syncBuffer is a minimal concurrency-safe io.Writer for capturing slog
// output in tests without importing bytes.Buffer's non-safe methods
// concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
