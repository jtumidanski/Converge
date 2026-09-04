package testutil

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestHarnessStrategies(t *testing.T) {
	r := NewRepo(t)
	base := r.Head()

	r.Branch("feat/a")
	a1 := r.Commit("a.txt", "a1\n", "a1")
	a2 := r.Commit("a.txt", "a1\na2\n", "a2")
	r.Checkout("main")
	if got := r.BranchCommits("feat/a"); len(got) != 2 || got[0] != a1 || got[1] != a2 {
		t.Fatalf("BranchCommits = %v", got)
	}
	merge := r.MergeNoFF("feat/a", "Merge feat/a")
	if parents := strings.Fields(r.Git("rev-list", "--parents", "-n", "1", merge)); len(parents) != 3 || parents[1] != base {
		t.Fatalf("merge parents = %v", parents)
	}

	r.Branch("feat/b")
	r.Commit("b.txt", "b\n", "b1")
	r.Commit("b.txt", "bb\n", "b2")
	r.Checkout("main")
	squash := r.Squash("feat/b", "Squash feat/b")
	if parents := strings.Fields(r.Git("rev-list", "--parents", "-n", "1", squash)); len(parents) != 2 || parents[1] != merge {
		t.Fatalf("squash parents = %v", parents)
	}
	if r.FileContent("main", "b.txt") != "bb\n" {
		t.Fatal("squash content")
	}

	r.Branch("feat/c")
	c1 := r.Commit("c.txt", "c\n", "c1")
	r.Checkout("main")
	r.Commit("other.txt", "x\n", "unrelated on main")
	rebased := r.Rebase("feat/c")
	if len(rebased) != 1 || rebased[0] == c1 || r.Head() != rebased[0] {
		t.Fatalf("rebase = %v head=%s c1=%s", rebased, r.Head(), c1)
	}
	r.Push()
	if !strings.HasPrefix(r.CloneURL(), "file://") {
		t.Fatal("clone url")
	}
}

// TestRebaseMultiCommitOrdering exercises Rebase's "oldest-first" promise
// with a three-commit branch, tied to identifiable per-commit content, so
// the assertion is about real ordering rather than slice length. A
// single-commit branch (as in TestHarnessStrategies) cannot distinguish a
// correct --reverse ordering from any other ordering.
func TestRebaseMultiCommitOrdering(t *testing.T) {
	r := NewRepo(t)

	r.Branch("feat/multi")
	d1 := r.Commit("d.txt", "d1\n", "d1")
	d2 := r.Commit("d.txt", "d1\nd2\n", "d2")
	d3 := r.Commit("d.txt", "d1\nd2\nd3\n", "d3")
	r.Checkout("main")
	r.Commit("other2.txt", "y\n", "unrelated on main")

	rebased := r.Rebase("feat/multi")
	if len(rebased) != 3 {
		t.Fatalf("rebase = %v, want 3 commits", rebased)
	}

	// The SHAs must genuinely be rewritten by the rebase, not just reused.
	if rebased[0] == d1 || rebased[1] == d2 || rebased[2] == d3 {
		t.Fatalf("rebase did not rewrite SHAs: rebased=%v pre-rebase=[%s %s %s]", rebased, d1, d2, d3)
	}

	// Ordering is asserted against real, distinguishable content at each
	// position, not merely against distinct SHAs.
	if got := r.FileContent(rebased[0], "d.txt"); got != "d1\n" {
		t.Fatalf("rebased[0] content = %q, want %q", got, "d1\n")
	}
	if got := r.FileContent(rebased[1], "d.txt"); got != "d1\nd2\n" {
		t.Fatalf("rebased[1] content = %q, want %q", got, "d1\nd2\n")
	}
	if got := r.FileContent(rebased[2], "d.txt"); got != "d1\nd2\nd3\n" {
		t.Fatalf("rebased[2] content = %q, want %q", got, "d1\nd2\nd3\n")
	}

	// main must fast-forward to the last rewritten commit.
	if r.Head() != rebased[2] {
		t.Fatalf("main head = %s, want %s (last rebased commit)", r.Head(), rebased[2])
	}
}

// TestFileContentAbsentPath asserts the documented "" surface for a path
// that genuinely does not exist at a valid revision.
func TestFileContentAbsentPath(t *testing.T) {
	r := NewRepo(t)
	if got := r.FileContent("main", "does-not-exist.txt"); got != "" {
		t.Fatalf("FileContent for absent path = %q, want \"\"", got)
	}
}

// testFileContentBadRevisionHelperEnv gates
// TestFileContentBadRevisionFailsHelperProcess so it only ever performs its
// Fatal-triggering call when deliberately invoked as a subprocess by
// TestFileContentBadRevisionFails, never as an ordinary part of the suite.
const testFileContentBadRevisionHelperEnv = "TESTUTIL_RUN_BAD_REVISION_HELPER"

// TestFileContentBadRevisionFailsHelperProcess is not a normal test: it is
// the subprocess target for TestFileContentBadRevisionFails. It performs
// the actual FileContent call with an invalid revision, which is expected
// to call r.T.Fatal and fail (and exit nonzero as `go test` for this
// process). See TestFileContentBadRevisionFails for why this indirection
// is necessary.
func TestFileContentBadRevisionFailsHelperProcess(t *testing.T) {
	if os.Getenv(testFileContentBadRevisionHelperEnv) != "1" {
		t.Skip("only runs as a subprocess helper of TestFileContentBadRevisionFails")
	}
	r := NewRepo(t)
	r.FileContent("not-a-real-revision", "README.md")
	t.Fatal("FileContent did not fail for an invalid revision (unreachable if it had)")
}

// TestFileContentBadRevisionFails asserts that FileContent fails loud
// (t.Fatal), rather than returning "", when the revision itself is invalid
// — the case the prior implementation collapsed into "" indistinguishably
// from a genuinely absent path.
//
// t.Fatal calls runtime.Goexit and kills the calling goroutine, so this
// can't be observed with a direct call + recover, and a plain t.Run subtest
// would make the failure a real (red) failure of this package's test
// suite — which is the opposite of what we want to assert here. Instead
// this re-invokes the test binary in a subprocess targeting
// TestFileContentBadRevisionFailsHelperProcess (the standard
// TestHelperProcess pattern used by, e.g., os/exec's own tests) and asserts
// the *subprocess* exits nonzero, while this test itself passes normally.
func TestFileContentBadRevisionFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFileContentBadRevisionFailsHelperProcess$", "-test.v") //nolint:gosec // test-only subprocess re-invocation of the current test binary, fixed args
	cmd.Env = append(os.Environ(), testFileContentBadRevisionHelperEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the helper subprocess to fail for an invalid revision, but it exited 0\noutput:\n%s", out)
	}
	if !strings.Contains(string(out), "TestFileContentBadRevisionFailsHelperProcess") {
		t.Fatalf("unexpected subprocess output (helper test may not have run):\n%s", out)
	}
}

// TestGitObjectMissingClassification unit-tests the error-classification
// logic FileContent relies on to distinguish "path absent" (a plain
// nonzero git exit) from any other failure (git failing to start, a
// signal, etc.), which should instead be treated as a harness error.
func TestGitObjectMissingClassification(t *testing.T) {
	if gitObjectMissing(nil) {
		t.Fatal("nil error must not be classified as missing")
	}

	// A real *exec.ExitError from a plain nonzero exit.
	exitErr := exec.Command("false").Run()
	if !gitObjectMissing(exitErr) {
		t.Fatalf("nonzero exit error must be classified as missing: %v (%T)", exitErr, exitErr)
	}

	// Any non-ExitError (e.g. a failure to invoke the binary at all) must
	// not be silently treated as "missing" — it's a harness-level failure.
	if gitObjectMissing(errors.New("boom")) {
		t.Fatal("a non-ExitError must not be classified as missing")
	}
	if _, err := exec.LookPath("definitely-not-a-real-binary-xyz"); err == nil {
		t.Fatal("expected LookPath to fail for a nonexistent binary")
	} else if gitObjectMissing(err) {
		t.Fatal("an exec.Error (failed to start) must not be classified as missing")
	}
}
