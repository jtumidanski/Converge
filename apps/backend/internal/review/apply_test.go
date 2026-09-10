package review

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
)

func newApplicator(t *testing.T) (*CherryPickApplicator, *gitx.ExecRunner) {
	t.Helper()
	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	return NewCherryPickApplicator(runner, testLog()), runner
}

// testProvider is the provider id threaded into Apply; it must show up in
// every Converge-Change trailer (FR-6.5).
const testProvider = "github"

func resolved(t *testing.T, number int, strategy session.Strategy, shas ...string) session.ResolvedChange {
	t.Helper()
	rc, err := session.NewResolvedChange(session.ResolvedChangeParams{Number: number, Title: "change", Author: "dev", MergedAt: time.Now(), Strategy: strategy, LandingSHAs: shas, SourceSHA: shas[0]})
	if err != nil {
		t.Fatal(err)
	}
	return rc
}

func TestApplyMergeSquashRebase(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()

	src.Branch("feat/a")
	src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	mergeSHA := src.MergeNoFF("feat/a", "merge a")

	src.Branch("feat/b")
	src.Commit("b.txt", "b\n", "b1")
	src.Commit("b.txt", "bb\n", "b2")
	src.Checkout("main")
	squashSHA := src.Squash("feat/b", "squash b")

	src.Branch("feat/c")
	src.Commit("c.txt", "c\n", "c1")
	src.Commit("c2.txt", "c2\n", "c2")
	src.Checkout("main")
	rebased := src.Rebase("feat/c")

	a, runner := newApplicator(t)
	ctx := context.Background()
	// work directly in a detached copy of the repo at base
	src.Git("checkout", "-b", "review/test", base)

	res, err := a.Apply(ctx, src.Work, testProvider, resolved(t, 1, session.StrategyMerge, mergeSHA))
	if err != nil || res.Outcome != OutcomeApplied {
		t.Fatalf("merge: %v %+v", err, res)
	}
	if src.FileContent("HEAD", "a.txt") != "a\n" {
		t.Error("merge content missing")
	}
	msg := src.Git("log", "-1", "--format=%B")
	if !strings.Contains(msg, "Converge-Change: github#1") || !strings.Contains(msg, "Converge-Source: "+mergeSHA) {
		t.Errorf("trailer missing: %q", msg)
	}

	res, err = a.Apply(ctx, src.Work, testProvider, resolved(t, 2, session.StrategySquash, squashSHA))
	if err != nil || res.Outcome != OutcomeApplied || src.FileContent("HEAD", "b.txt") != "bb\n" {
		t.Fatalf("squash: %v %+v", err, res)
	}

	beforeRebase := src.Head()
	res, err = a.Apply(ctx, src.Work, testProvider, resolved(t, 3, session.StrategyRebase, rebased...))
	if err != nil || res.Outcome != OutcomeApplied || src.FileContent("HEAD", "c.txt") != "c\n" || src.FileContent("HEAD", "c2.txt") != "c2\n" {
		t.Fatalf("rebase: %v %+v", err, res)
	}
	if n := len(strings.Split(strings.TrimSpace(src.Git("rev-list", base+"..HEAD")), "\n")); n != 4 {
		t.Errorf("commit count = %d, want 4", n)
	}

	// FR-6.5: *every* synthetic commit of a multi-commit rebase pick carries
	// the trailer, not just the tip. Amending only the tip leaves the first
	// of the two rebase commits unattributed and fails here.
	newCommits := strings.Split(strings.TrimSpace(src.Git("rev-list", beforeRebase+"..HEAD")), "\n")
	if len(newCommits) != 2 {
		t.Fatalf("rebase produced %d commits, want 2", len(newCommits))
	}
	for _, c := range newCommits {
		body := src.Git("log", "-1", "--format=%B", c)
		if !strings.Contains(body, "Converge-Change: github#3") {
			t.Errorf("commit %s missing trailer: %q", c, body)
		}
		if !strings.Contains(body, "Converge-Source: "+rebased[0]) {
			t.Errorf("commit %s missing source trailer: %q", c, body)
		}
	}
	_ = runner
}

func TestApplyEmptyPickIsNotAnError(t *testing.T) {
	src := testutil.NewRepo(t)
	base := src.Head()

	// Two independent PRs, both branched from base, that happen to
	// introduce identical content. Each is squashed on its own staging
	// branch off base (not sequentially onto the same "main"): squashing
	// the second one onto a main that already carries the first PR's
	// content would leave nothing to stage, and testutil.Squash's `git
	// commit -m` (no --allow-empty) would fail before the applicator is
	// even exercised. Building both squash commits independently against
	// base keeps each squash-time diff real, while still producing two
	// distinct commits with identical net content — so the *second*
	// cherry-pick onto the shared review branch is the one that becomes
	// empty, which is what this test is about.
	src.Checkout(base)
	src.Branch("feat/a")
	src.Commit("a.txt", "a\n", "a1")
	src.Checkout(base)
	src.Branch("stage/a")
	first := src.Squash("feat/a", "squash a")

	src.Checkout(base)
	src.Branch("feat/a2")
	src.Commit("a.txt", "a\n", "a again")
	src.Checkout(base)
	src.Branch("stage/a2")
	second := src.Squash("feat/a2", "squash a again")

	a, _ := newApplicator(t)
	src.Git("checkout", "-b", "review/test", base)
	if res, err := a.Apply(context.Background(), src.Work, testProvider, resolved(t, 1, session.StrategySquash, first)); err != nil || res.Outcome != OutcomeApplied {
		t.Fatalf("first: %v %+v", err, res)
	}
	before := src.Head()
	res, err := a.Apply(context.Background(), src.Work, testProvider, resolved(t, 2, session.StrategySquash, second))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	// Exactly OutcomeEmpty. `--empty=keep` still creates a commit and still
	// advances HEAD on a no-op pick, so a HEAD-SHA comparison (before != after)
	// would classify this as OutcomeApplied and fail here; only the tree
	// comparison gets it right.
	if res.Outcome != OutcomeEmpty {
		t.Fatalf("outcome = %q, want %q (%+v)", res.Outcome, OutcomeEmpty, res)
	}
	if src.Head() == before {
		t.Error("expected --empty=keep to create a commit and move HEAD")
	}
	if strings.TrimSpace(src.Git("status", "--porcelain")) != "" {
		t.Error("worktree dirty after empty pick")
	}
	// Even the empty commit is attributable (FR-6.5).
	if msg := src.Git("log", "-1", "--format=%B"); !strings.Contains(msg, "Converge-Change: github#2") {
		t.Errorf("empty commit missing trailer: %q", msg)
	}
}

// conflictingRepo builds two independent squash commits that both rewrite the
// same line of shared.txt, and checks out a fresh review branch at base. The
// returned shas conflict with each other when applied in either order.
func conflictingRepo(t *testing.T) (*testutil.Repo, string, string) {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Commit("shared.txt", "original\n", "seed")
	base := src.Head()

	// Both squash commits are branched from base independently (not
	// sequentially through main), and each is squashed on its own staging
	// branch. If feat/b were branched from main *after* squashing feat/a
	// (as one might do sequentially), bSHA's implied parent would be
	// aSHA — so cherry-picking bSHA alone onto a plain `base` checkout
	// would 3-way-merge against a merge-base that already contains "from
	// a\n", producing a spurious conflict on the very first Apply call,
	// before the test ever gets to the conflict it means to exercise.
	// Building both off base keeps each squash commit's diff parented on
	// base, so the first Apply (bSHA) is clean and only the second
	// (aSHA, colliding with bSHA's line) conflicts as intended.
	src.Branch("feat/a")
	src.Commit("shared.txt", "from a\n", "a")
	src.Checkout(base)
	src.Branch("stage/a")
	aSHA := src.Squash("feat/a", "squash a")

	src.Checkout(base)
	src.Branch("feat/b")
	src.Commit("shared.txt", "from b\n", "b")
	src.Checkout(base)
	src.Branch("stage/b")
	bSHA := src.Squash("feat/b", "squash b")

	src.Git("checkout", "-b", "review/test", base)
	return src, aSHA, bSHA
}

func TestApplyConflictReportsPaths(t *testing.T) {
	src, aSHA, bSHA := conflictingRepo(t)

	a, _ := newApplicator(t)
	// apply b first, then a: the same line conflicts
	if _, err := a.Apply(context.Background(), src.Work, testProvider, resolved(t, 2, session.StrategySquash, bSHA)); err != nil {
		t.Fatal(err)
	}
	res, err := a.Apply(context.Background(), src.Work, testProvider, resolved(t, 1, session.StrategySquash, aSHA))
	if err != nil {
		t.Fatalf("conflict must not be an error: %v", err)
	}
	if res.Outcome != OutcomeConflict || len(res.ConflictingPaths) != 1 || res.ConflictingPaths[0] != "shared.txt" {
		t.Fatalf("res = %+v", res)
	}
	if res.Commit != aSHA {
		t.Errorf("commit = %s, want %s", res.Commit, aSHA)
	}
	// the worktree is left conflicted for diagnostics
	if !strings.Contains(src.Git("status", "--porcelain"), "U") {
		t.Error("worktree should remain conflicted")
	}
	if err := a.Abort(context.Background(), src.Work); err != nil {
		t.Fatalf("abort: %v", err)
	}
}

// A cherry-pick that fails with a non-1 exit code is a hard failure, not a
// conflict. git leaves the previous change's unmerged paths and
// CHERRY_PICK_HEAD in place, so classifying on "any non-zero exit" would
// return OutcomeConflict with another change's paths and SHA and a nil error.
func TestApplyHardGitFailureIsAnError(t *testing.T) {
	src, aSHA, bSHA := conflictingRepo(t)

	a, _ := newApplicator(t)
	ctx := context.Background()
	res, err := a.Apply(ctx, src.Work, testProvider, resolved(t, 1, session.StrategySquash, bSHA))
	if err != nil || res.Outcome != OutcomeApplied {
		t.Fatalf("first: %v %+v", err, res)
	}
	res, err = a.Apply(ctx, src.Work, testProvider, resolved(t, 2, session.StrategySquash, aSHA))
	if err != nil || res.Outcome != OutcomeConflict {
		t.Fatalf("second must conflict: %v %+v", err, res)
	}

	// The worktree is now left with unmerged paths. git refuses to start
	// another cherry-pick at all: "fatal: cherry-pick failed", exit 128.
	third := resolved(t, 3, session.StrategySquash, bSHA)
	res, err = a.Apply(ctx, src.Work, testProvider, third)
	if err == nil {
		t.Fatalf("hard git failure must be an error, got %+v", res)
	}
	if res.Outcome != "" {
		t.Errorf("no outcome may be reported on a hard failure, got %+v", res)
	}
	if err := a.Abort(ctx, src.Work); err != nil {
		t.Fatalf("abort: %v", err)
	}
}
