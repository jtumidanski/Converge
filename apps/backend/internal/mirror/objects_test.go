package mirror

import (
	"context"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
)

func TestObjectReader(t *testing.T) {
	src := testutilRepo(t)
	base := src.Head()
	src.Branch("feat")
	c1 := src.Commit("f.txt", "1\n", "c1")
	c2 := src.Commit("f.txt", "1\n2\n", "c2")
	src.Checkout("main")
	merge := src.MergeNoFF("feat", "merge feat")
	src.Push()

	runner, err := gitx.NewExecRunner(testLogger(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	cache := New(t.TempDir(), runner, &gitx.LockMap{}, testLogger())
	path, err := cache.Ensure(context.Background(), RootNamespace(), fake.New("fake", provider.KindGitLab), repoFor(t, "fake", src.CloneURL()))
	if err != nil {
		t.Fatal(err)
	}
	o := cache.Objects(path, "atlas/server")
	ctx := context.Background()

	if ok, _ := o.Exists(ctx, merge); !ok {
		t.Error("merge should exist")
	}
	if ok, err := o.Exists(ctx, "0000000000000000000000000000000000000000"); ok || err != nil {
		t.Errorf("zero sha: %v %v", ok, err)
	}
	if ps, _ := o.Parents(ctx, merge); len(ps) != 2 || ps[0] != base || ps[1] != c2 {
		t.Errorf("parents = %v", ps)
	}
	if ps, _ := o.Parents(ctx, c1); len(ps) != 1 || ps[0] != base {
		t.Errorf("c1 parents = %v", ps)
	}
	id1, _ := o.PatchID(ctx, c1)
	id2, _ := o.PatchID(ctx, c2)
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Errorf("patch ids %q %q", id1, id2)
	}
	if w, _ := o.FirstParentWalk(ctx, c2, 2); len(w) != 2 || w[0] != c1 || w[1] != c2 {
		t.Errorf("walk = %v", w)
	}
	if ok, _ := o.IsAncestor(ctx, c1, "main"); !ok {
		t.Error("c1 should be ancestor of main")
	}
	if ok, _ := o.IsAncestor(ctx, merge, "feat"); ok {
		t.Error("merge is not ancestor of feat")
	}
	if got, _ := o.RevParse(ctx, merge+"^1"); got != base {
		t.Errorf("rev-parse ^1 = %s", got)
	}
	if ok, _ := o.BranchExists(ctx, "main"); !ok {
		t.Error("main missing")
	}
	if ok, _ := o.BranchExists(ctx, "nope"); ok {
		t.Error("nope exists")
	}
	if _, err := o.Parents(ctx, "bad"); err == nil {
		t.Error("invalid sha accepted")
	}
}

// TestExistsAbsentObjectReturnsFalseNoError proves the ordinary "commit not
// present in this mirror" case (real git, real mirror) is reported as
// (false, nil), not an error — this is the benign case Exists must preserve.
func TestExistsAbsentObjectReturnsFalseNoError(t *testing.T) {
	src := testutilRepo(t)
	src.Push()

	runner, err := gitx.NewExecRunner(testLogger(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	cache := New(t.TempDir(), runner, &gitx.LockMap{}, testLogger())
	path, err := cache.Ensure(context.Background(), RootNamespace(), fake.New("fake", provider.KindGitLab), repoFor(t, "fake", src.CloneURL()))
	if err != nil {
		t.Fatal(err)
	}
	o := cache.Objects(path, "atlas/server")

	// A well-formed SHA that is not present in the mirror at all.
	ok, err := o.Exists(context.Background(), "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1")
	if err != nil {
		t.Fatalf("absent object: unexpected error %v", err)
	}
	if ok {
		t.Error("absent object should not exist")
	}
}

// TestExistsFatalGitErrorReturnsError proves that a fatal/broken-repository
// git failure (exit code 128 that is NOT the clean "object absent" signal)
// is surfaced as an error rather than masked as (false, nil). Regression
// test for the finding that Exists previously collapsed exit 1 and exit 128
// into the same benign result.
func TestExistsFatalGitErrorReturnsError(t *testing.T) {
	fr := &gitx.FakeRunner{Handler: func(gitx.Spec) (gitx.Result, error) {
		res := gitx.Result{ExitCode: 128, Stderr: []byte("fatal: not a git repository: 'broken'")}
		return res, &gitx.ExitError{Category: gitx.CategoryQuery, Result: res}
	}}
	o := &objects{dir: "broken", repo: "atlas/server", runner: fr}

	ok, err := o.Exists(context.Background(), "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1")
	if err == nil {
		t.Fatal("broken repository should return an error, got nil")
	}
	if ok {
		t.Error("broken repository should not report the object as existing")
	}
}

// TestExistsGenuineNotFoundExitCodeReturnsFalseNoError proves the exit-1
// "not found, verified" signal (as produced by rev-parse --verify --quiet)
// still maps to (false, nil) when driven through a FakeRunner, isolating
// the exit-code classification from real git behaviour.
func TestExistsGenuineNotFoundExitCodeReturnsFalseNoError(t *testing.T) {
	fr := &gitx.FakeRunner{Handler: func(gitx.Spec) (gitx.Result, error) {
		res := gitx.Result{ExitCode: 1}
		return res, &gitx.ExitError{Category: gitx.CategoryQuery, Result: res}
	}}
	o := &objects{dir: "somewhere", repo: "atlas/server", runner: fr}

	ok, err := o.Exists(context.Background(), "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1")
	if err != nil {
		t.Fatalf("exit-1 not-found should not be an error, got %v", err)
	}
	if ok {
		t.Error("exit-1 not-found should report false")
	}
}
