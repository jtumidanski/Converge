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
	"github.com/jtumidanski/converge/internal/identity"
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
	p, err := c.Path(RootNamespace(), "gitlab-work", "atlas/sub/server")
	if err != nil || p != filepath.Join("/cache", "gitlab-work", "atlas", "sub", "server.git") {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := c.Path(RootNamespace(), "gh", "../escape"); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := c.Path(RootNamespace(), "../gh", "a/b"); err == nil {
		t.Fatal("bad provider id accepted")
	}
}

func TestEnsureClonesThenUpdates(t *testing.T) {
	fr := &gitx.FakeRunner{}
	root := t.TempDir()
	c := New(root, fr, &gitx.LockMap{}, testLogger())
	p := fake.New("gh", provider.KindGitHub)
	repo := repoFor(t, "gh", "https://github.com/atlas/server.git")
	path, err := c.Ensure(context.Background(), RootNamespace(), p, repo)
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
	if _, err := c.Ensure(context.Background(), RootNamespace(), p, repo); err != nil {
		t.Fatal(err)
	}
	calls = fr.Snapshot()
	if len(calls) != 1 || calls[0].Category != gitx.CategoryFetch || calls[0].Dir != path ||
		strings.Join(calls[0].Args, " ") != "fetch --prune origin "+gitx.MirrorRefspec+" "+gitx.ExcludeReviewRefspec {
		t.Fatalf("update call = %+v", calls)
	}
	fr.Reset()
	sha := strings.Repeat("a", 40)
	if err := c.FetchSHA(context.Background(), RootNamespace(), p, repo, sha); err != nil {
		t.Fatal(err)
	}
	calls = fr.Snapshot()
	if len(calls) != 1 || strings.Join(calls[0].Args, " ") != "fetch origin "+sha || calls[0].Dir != path {
		t.Fatalf("fetch call = %+v", calls)
	}
	if err := c.FetchSHA(context.Background(), RootNamespace(), p, repo, "nothex"); err == nil {
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
			path, err := c.Ensure(context.Background(), RootNamespace(), p, repo)
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
	path, err := c.Ensure(ctx, RootNamespace(), p, repo)
	if err != nil {
		t.Fatal(err)
	}

	branch := gitx.ReviewBranchPrefix + "abcd1234"
	worktree := filepath.Join(t.TempDir(), "repo")
	if _, err := runner.Run(ctx, gitx.Spec{Dir: path, Args: []string{"worktree", "add", "-b", branch, worktree, src.Head()}, Category: gitx.CategoryWorktree}); err != nil {
		t.Fatal(err)
	}

	src.Git("push", "origin", "--delete", "stale")
	if _, err := c.Ensure(ctx, RootNamespace(), p, repo); err != nil {
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
	path, err := c.Ensure(context.Background(), RootNamespace(), p, repo)
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
	if _, err := c.Ensure(context.Background(), RootNamespace(), p, repo); err != nil {
		t.Fatal(err)
	}
	ok, err := c.Objects(path, "atlas/server").Exists(context.Background(), src.Head())
	if err != nil || !ok {
		t.Fatalf("new commit not fetched: %v %v", ok, err)
	}
}

func TestRootNamespacePathIsUnchanged(t *testing.T) {
	c := New("/cache", &gitx.FakeRunner{}, &gitx.LockMap{}, testLogger())
	got, err := c.Path(RootNamespace(), "gh", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/cache", "gh", "atlas", "server.git")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestScopedNamespaceNestsUnderUsers(t *testing.T) {
	c := New("/cache", &gitx.FakeRunner{}, &gitx.LockMap{}, testLogger())
	ns, err := NamespaceFor(identity.ForUser("abcdef0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Path(ns, "gh", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/cache", "users", "abcdef0123456789", "gh", "atlas", "server.git")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNamespaceForRejectsAMalformedUserID(t *testing.T) {
	// The brief's rejection table includes "", but identity.ForUser("")
	// produces the same zero-valued Scope as identity.Standalone() (Scope's
	// field is unexported, so there is no way to construct a "non-empty
	// scope" carrying an empty user id). NamespaceFor's own reference
	// implementation deliberately treats an empty user id as the standalone
	// scope rather than an error, precisely because a real Standalone()
	// scope must still resolve to the root namespace for standalone
	// equivalence to hold. Erroring on "" would make NamespaceFor(scope)
	// fail for every unauthenticated request. Treated here as a brief/code
	// self-contradiction (Contract 5): the code's behaviour is followed and
	// the empty case is asserted to yield the root namespace, not an error.
	// Reported as a brief conflict.
	root := "/cache"
	c := New(root, &gitx.FakeRunner{}, &gitx.LockMap{}, testLogger())

	bad := []string{"..", "../../etc", "abc/def", "ABCDEF0123456789", "abcdef012345678", "abcdef01234567890"}
	for _, id := range bad {
		id := id
		t.Run(id, func(t *testing.T) {
			ns, err := NamespaceFor(identity.ForUser(id))
			if err == nil {
				t.Fatalf("user id %q must be rejected before it reaches a path", id)
			}
			// Direct containment assertion: even if a caller ignored the
			// error and used the returned Namespace anyway, it must be the
			// safe zero value, so no path built from it can escape the
			// cache root.
			if !ns.IsRoot() {
				t.Fatalf("rejected user id %q must not yield a usable namespace, got %+v", id, ns)
			}
			p, err := c.Path(ns, "gh", "atlas/server")
			if err != nil {
				t.Fatal(err)
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(root, abs)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Fatalf("resolved path %q escapes cache root %q for rejected id %q", abs, root, id)
			}
		})
	}

	t.Run("empty string is the standalone scope, not malformed", func(t *testing.T) {
		ns, err := NamespaceFor(identity.Standalone())
		if err != nil {
			t.Fatalf("standalone scope must not error: %v", err)
		}
		if !ns.IsRoot() {
			t.Fatal("standalone scope must yield the root namespace")
		}
	})
}

func TestTwoUsersGetIndependentPaths(t *testing.T) {
	c := New("/cache", &gitx.FakeRunner{}, &gitx.LockMap{}, testLogger())
	nsA, err := NamespaceFor(identity.ForUser("abcdef0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	nsB, err := NamespaceFor(identity.ForUser("fedcba9876543210"))
	if err != nil {
		t.Fatal(err)
	}
	pathA, err := c.Path(nsA, "gh", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	pathB, err := c.Path(nsB, "gh", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	if pathA == pathB {
		t.Fatalf("two users must not share a mirror path: %q", pathA)
	}
	if strings.HasPrefix(pathA, pathB) || strings.HasPrefix(pathB, pathA) {
		t.Fatalf("neither user path may be a prefix of the other: %q vs %q", pathA, pathB)
	}
}

func TestPurgeNamespaceRemovesOnlyThatUser(t *testing.T) {
	root := t.TempDir()
	c := New(root, &gitx.FakeRunner{}, &gitx.LockMap{}, testLogger())
	nsA, err := NamespaceFor(identity.ForUser("abcdef0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	nsB, err := NamespaceFor(identity.ForUser("fedcba9876543210"))
	if err != nil {
		t.Fatal(err)
	}

	headA := filepath.Join(root, "users", "abcdef0123456789", "gh", "x.git", "HEAD")
	headB := filepath.Join(root, "users", "fedcba9876543210", "gh", "x.git", "HEAD")
	headRoot := filepath.Join(root, "gh", "x.git", "HEAD")
	for _, p := range []string{headA, headB, headRoot} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("ref: refs/heads/main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := c.PurgeNamespace(nsA); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(headA); !os.IsNotExist(err) {
		t.Fatalf("namespace A must be gone, stat err = %v", err)
	}
	if _, err := os.Stat(headB); err != nil {
		t.Fatalf("namespace B must survive: %v", err)
	}
	if _, err := os.Stat(headRoot); err != nil {
		t.Fatalf("root namespace must survive: %v", err)
	}

	// nsB's own tree must still be reachable through the namespace itself,
	// not just by the path built by hand above.
	dirB, err := c.NamespaceRoot(nsB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dirB); err != nil {
		t.Fatalf("namespace B directory must survive: %v", err)
	}
}

func TestPurgeRootNamespaceIsRefused(t *testing.T) {
	root := t.TempDir()
	c := New(root, &gitx.FakeRunner{}, &gitx.LockMap{}, testLogger())
	headRoot := filepath.Join(root, "gh", "x.git", "HEAD")
	if err := os.MkdirAll(filepath.Dir(headRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headRoot, []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := c.PurgeNamespace(RootNamespace()); err == nil {
		t.Fatal("purging the root namespace must be refused")
	}

	if _, err := os.Stat(headRoot); err != nil {
		t.Fatalf("root namespace must not be deleted: %v", err)
	}
}
