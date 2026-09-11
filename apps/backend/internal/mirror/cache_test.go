package mirror

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/testutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func testutilRepo(t *testing.T) *testutil.Repo { return testutil.NewRepo(t) }

func repoFor(t *testing.T, providerID, cloneURL string) provider.Repository {
	t.Helper()
	r, err := provider.NewRepositoryBuilder().SetProviderID(providerID).SetFullName("atlas/server").SetDefaultBranch("main").SetCloneURL(cloneURL).Build()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPathLayoutAndValidation(t *testing.T) {
	c := New("/cache", &gitx.FakeRunner{}, &gitx.LockMap{}, testLogger())
	p, err := c.Path("gitlab-work", "atlas/sub/server")
	if err != nil || p != filepath.Join("/cache", "gitlab-work", "atlas", "sub", "server.git") {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := c.Path("gh", "../escape"); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := c.Path("../gh", "a/b"); err == nil {
		t.Fatal("bad provider id accepted")
	}
}

func TestEnsureClonesThenUpdates(t *testing.T) {
	fr := &gitx.FakeRunner{}
	root := t.TempDir()
	c := New(root, fr, &gitx.LockMap{}, testLogger())
	p := fake.New("gh", provider.KindGitHub)
	repo := repoFor(t, "gh", "https://github.com/atlas/server.git")
	path, err := c.Ensure(context.Background(), p, repo)
	if err != nil {
		t.Fatal(err)
	}
	calls := fr.Snapshot()
	if len(calls) != 1 || calls[0].Category != gitx.CategoryClone || strings.Join(calls[0].Args, " ") != "clone --mirror https://github.com/atlas/server.git "+path {
		t.Fatalf("clone call = %+v", calls)
	}
	// simulate the clone having created HEAD
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fr.Reset()
	if _, err := c.Ensure(context.Background(), p, repo); err != nil {
		t.Fatal(err)
	}
	calls = fr.Snapshot()
	if len(calls) != 1 || calls[0].Category != gitx.CategoryFetch || calls[0].Dir != path ||
		strings.Join(calls[0].Args, " ") != "fetch --prune origin "+gitx.MirrorRefspec+" "+gitx.ExcludeReviewRefspec {
		t.Fatalf("update call = %+v", calls)
	}
	fr.Reset()
	sha := strings.Repeat("a", 40)
	if err := c.FetchSHA(context.Background(), p, repo, sha); err != nil {
		t.Fatal(err)
	}
	calls = fr.Snapshot()
	if len(calls) != 1 || strings.Join(calls[0].Args, " ") != "fetch origin "+sha || calls[0].Dir != path {
		t.Fatalf("fetch call = %+v", calls)
	}
	if err := c.FetchSHA(context.Background(), p, repo, "nothex"); err == nil {
		t.Fatal("bad sha accepted")
	}
}

func TestEnsureSerialisesConcurrentCallsOnSameMirror(t *testing.T) {
	src := testutil.NewRepo(t)
	runner, err := gitx.NewExecRunner(testLogger(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	c := New(t.TempDir(), runner, &gitx.LockMap{}, testLogger())
	p := fake.New("fake", provider.KindGitLab)
	repo := repoFor(t, "fake", src.CloneURL())

	const n = 8
	errs := make(chan error, n)
	paths := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := c.Ensure(context.Background(), p, repo)
			errs <- err
			paths <- path
		}()
	}
	wg.Wait()
	close(errs)
	close(paths)
	var firstPath string
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Ensure failed: %v", err)
		}
	}
	for path := range paths {
		if firstPath == "" {
			firstPath = path
		} else if path != firstPath {
			t.Fatalf("inconsistent mirror path: %q vs %q", path, firstPath)
		}
	}
	// A racing, unserialised clone into the same directory would corrupt the
	// mirror or fail outright; verify it is a valid, readable bare repo.
	if ok, err := c.Objects(firstPath, "atlas/server").Exists(context.Background(), src.Head()); err != nil || !ok {
		t.Fatalf("mirror not usable after concurrent Ensure: ok=%v err=%v", ok, err)
	}
}

// A mirror fetches +refs/*:refs/*, so a naive `--prune` deletes the
// refs/heads/review/<id> branches that live session worktrees are sitting on:
// the worktree's HEAD becomes a dangling symref and its next cherry-pick dies
// with exit 128. The prune must still remove branches that really went away
// upstream.
func TestEnsurePruneKeepsLiveReviewBranchesButPrunesStaleOnes(t *testing.T) {
	src := testutil.NewRepo(t)
	src.Branch("stale")
	src.Commit("s.txt", "s\n", "stale")
	src.Checkout("main")
	src.Push()

	runner, err := gitx.NewExecRunner(testLogger(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	c := New(t.TempDir(), runner, &gitx.LockMap{}, testLogger())
	p := fake.New("fake", provider.KindGitLab)
	repo := repoFor(t, "fake", src.CloneURL())
	ctx := context.Background()
	path, err := c.Ensure(ctx, p, repo)
	if err != nil {
		t.Fatal(err)
	}

	branch := gitx.ReviewBranchPrefix + "abcd1234"
	worktree := filepath.Join(t.TempDir(), "repo")
	if _, err := runner.Run(ctx, gitx.Spec{Dir: path, Args: []string{"worktree", "add", "-b", branch, worktree, src.Head()}, Category: gitx.CategoryWorktree}); err != nil {
		t.Fatal(err)
	}

	src.Git("push", "origin", "--delete", "stale")
	if _, err := c.Ensure(ctx, p, repo); err != nil {
		t.Fatal(err)
	}

	res, err := runner.Run(ctx, gitx.Spec{Dir: path, Args: []string{"branch", "--list", "--format=%(refname:short)"}, Category: gitx.CategoryQuery})
	if err != nil {
		t.Fatal(err)
	}
	branches := strings.Fields(string(res.Stdout))
	var sawReview, sawStale bool
	for _, b := range branches {
		switch b {
		case branch:
			sawReview = true
		case "stale":
			sawStale = true
		}
	}
	if !sawReview {
		t.Errorf("prune deleted the live review branch: %v", branches)
	}
	if sawStale {
		t.Errorf("prune did not remove the branch deleted upstream: %v", branches)
	}
	// The worktree must still be on a real branch, not a dangling symref.
	if _, err := runner.Run(ctx, gitx.Spec{Dir: worktree, Args: []string{"rev-parse", "--verify", "HEAD"}, Category: gitx.CategoryQuery}); err != nil {
		t.Errorf("worktree HEAD unusable after prune: %v", err)
	}
}

func TestEnsureRealGitNoCredentialInRemote(t *testing.T) {
	src := testutil.NewRepo(t)
	runner, err := gitx.NewExecRunner(testLogger(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	c := New(t.TempDir(), runner, &gitx.LockMap{}, testLogger())
	p := fake.New("fake", provider.KindGitLab)
	repo := repoFor(t, "fake", src.CloneURL())
	path, err := c.Ensure(context.Background(), p, repo)
	if err != nil {
		t.Fatal(err)
	}
	res, err := runner.Run(context.Background(), gitx.Spec{Dir: path, Args: []string{"remote", "get-url", "origin"}, Category: gitx.CategoryQuery})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != src.CloneURL() {
		t.Fatalf("remote url = %q err=%v", res.Stdout, err)
	}
	// second call updates and picks up a new commit
	src.Commit("new.txt", "n\n", "new")
	src.Push()
	if _, err := c.Ensure(context.Background(), p, repo); err != nil {
		t.Fatal(err)
	}
	ok, err := c.Objects(path, "atlas/server").Exists(context.Background(), src.Head())
	if err != nil || !ok {
		t.Fatalf("new commit not fetched: %v %v", ok, err)
	}
}
