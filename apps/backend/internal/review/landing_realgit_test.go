package review

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
)

// realObjects builds an ObjectReader backed by the real git binary against
// the working clone of r, so these tests exercise git's actual merge/squash/
// rebase shapes rather than a hand-scripted fake.
func realObjects(t *testing.T, r *testutil.Repo) mirror.ObjectReader {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner, err := gitx.NewExecRunner(log, gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	c := mirror.New(t.TempDir(), runner, &gitx.LockMap{}, log)
	return c.Objects(r.Work, "a/b")
}

// TestResolveLandingRealMerge covers the design §6.2 two-parent branch on a
// genuine `git merge --no-ff` commit.
func TestResolveLandingRealMerge(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Branch("feature")
	c1 := r.Commit("a.txt", "1\n", "feature commit 1")
	c2 := r.Commit("b.txt", "2\n", "feature commit 2")
	r.Checkout("main")
	mergeSHA := r.MergeNoFF("feature", "Merge feature")

	o := realObjects(t, r)
	got, err := ResolveLanding(context.Background(), o, change(t, mergeSHA, "", c2, 2), commits(t, c1, c2))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategyMerge || len(got.SHAs) != 1 || got.SHAs[0] != mergeSHA || got.SourceSHA != mergeSHA {
		t.Fatalf("got %+v", got)
	}
}

// TestResolveLandingRealSquashSingleCommit covers the one-parent, N==1 branch
// on a genuine `git merge --squash` of a single-commit feature branch.
func TestResolveLandingRealSquashSingleCommit(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Branch("feature")
	c1 := r.Commit("x.txt", "1\n", "feature commit")
	r.Checkout("main")
	squashSHA := r.Squash("feature", "Squash feature")

	o := realObjects(t, r)
	got, err := ResolveLanding(context.Background(), o, change(t, "", squashSHA, c1, 1), commits(t, c1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategySquash || len(got.SHAs) != 1 || got.SHAs[0] != squashSHA {
		t.Fatalf("got %+v", got)
	}
}

// TestResolveLandingRealSquashOfManyCommits covers the one-parent, N>1,
// patch-ids-do-not-match, non-empty-diff branch on a genuine squash of a
// two-commit feature branch.
func TestResolveLandingRealSquashOfManyCommits(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Branch("feature")
	c1 := r.Commit("x.txt", "1\n", "c1")
	c2 := r.Commit("y.txt", "2\n", "c2")
	r.Checkout("main")
	squashSHA := r.Squash("feature", "Squash many")

	o := realObjects(t, r)
	got, err := ResolveLanding(context.Background(), o, change(t, squashSHA, "", c2, 2), commits(t, c1, c2))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategySquash || len(got.SHAs) != 1 || got.SHAs[0] != squashSHA {
		t.Fatalf("got %+v", got)
	}
}

// TestResolveLandingRealSquashEmptyDiff covers the one-parent, N>1,
// patch-ids-do-not-match, empty-diff branch on a genuine squash whose net
// tree change is identical to main (an add followed by a revert on the
// feature branch), so the squash-merge commit itself carries no diff and the
// change must fail as BASE_UNDETERMINED rather than guess.
func TestResolveLandingRealSquashEmptyDiff(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Commit("x.txt", "0\n", "seed")
	r.Branch("feature")
	c1 := r.Commit("x.txt", "1\n", "c1")
	c2 := r.Commit("x.txt", "0\n", "c2 (revert)")
	r.Checkout("main")
	r.Git("merge", "--squash", "feature")
	r.Git("commit", "--allow-empty", "-m", "Squash feature (empty diff)")
	squashSHA := r.Head()

	o := realObjects(t, r)
	_, err := ResolveLanding(context.Background(), o, change(t, squashSHA, "", c2, 2), commits(t, c1, c2))
	var re *session.ReviewError
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("err = %v, want CodeBaseUndetermined", err)
	}
}

// TestResolveLandingRealRebase covers the one-parent, N>1, patch-ids-match
// branch on a genuine `git rebase` that rewrites the feature branch's SHAs.
func TestResolveLandingRealRebase(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Branch("feature")
	c1 := r.Commit("m.txt", "1\n", "c1")
	c2 := r.Commit("n.txt", "2\n", "c2")
	r.Checkout("main")
	r.Commit("main-only.txt", "x\n", "main advances") // gives the rebase something to do
	rewritten := r.Rebase("feature")
	if len(rewritten) != 2 {
		t.Fatalf("expected 2 rewritten commits, got %v", rewritten)
	}
	first, last := rewritten[0], rewritten[1]

	o := realObjects(t, r)
	got, err := ResolveLanding(context.Background(), o, change(t, last, "", last, 2), commits(t, c1, c2))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategyRebase || len(got.SHAs) != 2 || got.SHAs[0] != first || got.SHAs[1] != last || got.SourceSHA != last {
		t.Fatalf("got %+v", got)
	}
}

// TestResolveLandingRealGitLabFastForward covers the candidate-chain fallback
// to HeadSHA (merge_commit_sha and squash_commit_sha both empty) on a genuine
// fast-forward merge.
func TestResolveLandingRealGitLabFastForward(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Branch("feature")
	c1 := r.Commit("z.txt", "1\n", "only commit")
	r.Checkout("main")
	r.Git("merge", "--ff-only", "feature")
	head := r.Head()
	if head != c1 {
		t.Fatalf("expected fast-forward tip to equal feature commit: %s vs %s", head, c1)
	}

	o := realObjects(t, r)
	got, err := ResolveLanding(context.Background(), o, change(t, "", "", head, 1), commits(t, c1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategySquash || got.SHAs[0] != head {
		t.Fatalf("got %+v", got)
	}
}

// TestResolveLandingRealMissingCandidate covers a change whose candidate SHA
// is well-formed but genuinely absent from the mirror.
func TestResolveLandingRealMissingCandidate(t *testing.T) {
	r := testutil.NewRepo(t)
	missing := sha("f")

	o := realObjects(t, r)
	_, err := ResolveLanding(context.Background(), o, change(t, missing, "", "", 1), commits(t, sha("1")))
	var re *session.ReviewError
	if !errors.As(err, &re) || re.Code != session.CodeMissingCommits {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("missing candidate must wrap ErrNoCandidate: %v", err)
	}
}

// TestResolveLandingRealRootCommit covers the zero-parent BASE_UNDETERMINED
// branch on the repository's genuine root commit.
func TestResolveLandingRealRootCommit(t *testing.T) {
	r := testutil.NewRepo(t)
	root := r.Head() // NewRepo's sole commit so far has no parents

	o := realObjects(t, r)
	_, err := ResolveLanding(context.Background(), o, change(t, root, "", "", 1), commits(t, sha("1")))
	var re *session.ReviewError
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("err = %v", err)
	}
}

// TestResolveLandingRealOctopusMerge covers the >2-parent BASE_UNDETERMINED
// branch on a genuine octopus merge (3 parents).
func TestResolveLandingRealOctopusMerge(t *testing.T) {
	r := testutil.NewRepo(t)
	r.Branch("b1")
	r.Commit("f1.txt", "1\n", "c-b1")
	r.Checkout("main")
	r.Branch("b2")
	r.Commit("f2.txt", "2\n", "c-b2")
	r.Checkout("main")
	r.Commit("main-only.txt", "x\n", "main advances") // keep main from fast-forwarding to b1, so the merge has 3 distinct parents
	r.Git("merge", "-m", "octopus merge", "b1", "b2")
	octopus := r.Head()

	o := realObjects(t, r)
	_, err := ResolveLanding(context.Background(), o, change(t, octopus, "", "", 1), commits(t, sha("1")))
	var re *session.ReviewError
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("err = %v", err)
	}
}

// explodingExists wraps fakeObjects but reports a fatal error from Exists,
// modelling a corrupted mirror or a fatal git failure rather than a plain
// "object absent" result.
type explodingExists struct{ *fakeObjects }

func (e *explodingExists) Exists(context.Context, string) (bool, error) {
	return false, errors.New("simulated fatal git failure")
}

// TestResolveLandingPropagatesExistsError asserts that an ObjectReader error
// from Exists is returned as-is, never read as "candidate absent" (which
// would produce a confident MISSING_COMMITS instead of surfacing the failure).
func TestResolveLandingPropagatesExistsError(t *testing.T) {
	m := sha("e")
	base := &fakeObjects{exists: map[string]bool{m: true}, parents: map[string][]string{m: {sha("0"), sha("a")}}}
	o := &explodingExists{base}
	_, err := ResolveLanding(context.Background(), o, change(t, m, "", "", 1), commits(t, sha("1")))
	if err == nil {
		t.Fatal("expected the Exists error to propagate")
	}
	var re *session.ReviewError
	if errors.As(err, &re) {
		t.Fatalf("a raw ObjectReader error must not be repackaged as a ReviewError (would masquerade as MISSING_COMMITS/BASE_UNDETERMINED): %v", err)
	}
	if errors.Is(err, ErrNoCandidate) {
		t.Fatalf("an Exists error must not be treated as ErrNoCandidate: %v", err)
	}
}

// explodingParents wraps fakeObjects but reports a fatal error from Parents.
type explodingParents struct{ *fakeObjects }

func (e *explodingParents) Parents(context.Context, string) ([]string, error) {
	return nil, errors.New("simulated fatal git failure")
}

// TestResolveLandingPropagatesParentsError asserts the same for Parents.
func TestResolveLandingPropagatesParentsError(t *testing.T) {
	m := sha("e")
	base := &fakeObjects{exists: map[string]bool{m: true}}
	o := &explodingParents{base}
	_, err := ResolveLanding(context.Background(), o, change(t, m, "", "", 1), commits(t, sha("1")))
	if err == nil {
		t.Fatal("expected the Parents error to propagate")
	}
	var re *session.ReviewError
	if errors.As(err, &re) {
		t.Fatalf("a raw ObjectReader error must not be repackaged as a ReviewError: %v", err)
	}
}
