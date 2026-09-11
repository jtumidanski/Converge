package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
)

func newExecRunnerForTest(t *testing.T) *gitx.ExecRunner {
	t.Helper()
	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	return runner
}

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// fixture builds a repository with three merged changes on main plus unrelated commits.
type resolveFixture struct {
	repo     provider.Repository
	prov     *fake.Provider
	resolver *Resolver
	runner   *gitx.ExecRunner
	src      *testutil.Repo
	mergeSHA string
	baseSHA  string
}

func newResolveFixture(t *testing.T) *resolveFixture {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Commit("unrelated.txt", "u1\n", "unrelated 1")
	baseSHA := src.Head() // parent of the first landing commit

	src.Branch("feat/a")
	src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	mergeSHA := src.MergeNoFF("feat/a", "Merge #1")

	src.Commit("unrelated.txt", "u2\n", "unrelated 2")

	src.Branch("feat/b")
	src.Commit("b.txt", "b\n", "b1")
	src.Checkout("main")
	squashSHA := src.Squash("feat/b", "Squash #2")
	src.Push()

	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	cache := mirror.New(t.TempDir(), runner, &gitx.LockMap{}, testLog())

	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)
	t0 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	add := func(number int, merge, squash string, mergedAt time.Time, commits []provider.Commit) {
		b := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(number).SetTitle("change").
			SetAuthor("dev").SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(mergedAt).SetCommits(commits)
		if merge != "" {
			b.SetMergeCommitSHA(merge)
		}
		if squash != "" {
			b.SetSquashCommitSHA(squash)
		}
		cr, err := b.Build()
		if err != nil {
			t.Fatal(err)
		}
		p.AddChange(cr)
	}
	c1, _ := provider.NewCommit(src.RevParse("feat/a"), "a1", t0)
	c2, _ := provider.NewCommit(src.RevParse("feat/b"), "b1", t0)
	add(1, mergeSHA, "", t0.Add(time.Hour), []provider.Commit{c1})
	add(2, "", squashSHA, t0.Add(2*time.Hour), []provider.Commit{c2})

	return &resolveFixture{repo: repo, prov: p, resolver: NewResolver(cache, testLog()), runner: runner, src: src, mergeSHA: mergeSHA, baseSHA: baseSHA}
}

func TestResolveOrdersAndComputesBase(t *testing.T) {
	f := newResolveFixture(t)
	var stages []string
	got, err := f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{2, 1}, func(s string) { stages = append(stages, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Changes) != 2 || got.Changes[0].Number() != 1 || got.Changes[1].Number() != 2 {
		t.Fatalf("order = %+v", got.Changes)
	}
	if got.Changes[0].Strategy() != session.StrategyMerge || got.Changes[1].Strategy() != session.StrategySquash {
		t.Errorf("strategies = %s %s", got.Changes[0].Strategy(), got.Changes[1].Strategy())
	}
	if got.BaseSHA != f.baseSHA {
		t.Errorf("base = %s, want parent of first landing commit %s", got.BaseSHA, f.baseSHA)
	}
	if len(stages) < 2 || stages[0] != session.StageResolving || stages[1] != session.StageUpdatingRepo {
		t.Errorf("stages = %v", stages)
	}
}

func TestResolveRejectsUnmergedAndBadTargets(t *testing.T) {
	f := newResolveFixture(t)
	open, _ := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(f.repo).SetNumber(3).SetTitle("open").
		SetTargetBranch("main").SetState(provider.StateOpen).Build()
	f.prov.AddChange(open)
	other, _ := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(f.repo).SetNumber(4).SetTitle("other").
		SetTargetBranch("develop").SetState(provider.StateMerged).SetMergedAt(time.Now()).SetMergeCommitSHA(f.mergeSHA).Build()
	f.prov.AddChange(other)

	var re *session.ReviewError
	_, err := f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{1, 3}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeNotMerged || re.Change != 3 {
		t.Fatalf("not merged: %v", err)
	}
	_, err = f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{1, 4}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeIncompatibleTargets {
		t.Fatalf("targets: %v", err)
	}
	_, err = f.resolver.Resolve(context.Background(), f.prov, f.repo, "nonexistent-branch", []int{1}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("missing branch: %v", err)
	}
	f.prov.FailWith(provider.ErrAuth)
	_, err = f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{1}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeProviderAuth {
		t.Fatalf("auth: %v", err)
	}
}

func TestResolveRejectsChangeNotOnBaseBranch(t *testing.T) {
	f := newResolveFixture(t)
	// a merged change whose landing commit lives only on a side branch
	f.src.Branch("side")
	side := f.src.Commit("side.txt", "s\n", "side commit")
	f.src.Checkout("main")
	f.src.Push()
	c, _ := provider.NewCommit(side, "side", time.Now())
	cr, _ := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(f.repo).SetNumber(9).SetTitle("side").
		SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(time.Now()).SetMergeCommitSHA(side).SetCommits([]provider.Commit{c}).Build()
	f.prov.AddChange(cr)
	var re *session.ReviewError
	_, err := f.resolver.Resolve(context.Background(), f.prov, f.repo, "main", []int{9}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeNotOnBaseBranch || re.Change != 9 {
		t.Fatalf("err = %v", err)
	}
}

// countingProvider wraps fake.Provider's GetChange with a small delay and
// tracks the maximum number of concurrent in-flight calls, so a test can
// prove the resolver's fan-out is both genuinely concurrent (goroutines
// actually overlap) and bounded (never exceeds resolveConcurrency).
type countingProvider struct {
	*fake.Provider
	cur, max int32
}

func (c *countingProvider) GetChange(ctx context.Context, repo provider.Repository, number int) (provider.ChangeRequest, error) {
	n := atomic.AddInt32(&c.cur, 1)
	defer atomic.AddInt32(&c.cur, -1)
	for {
		old := atomic.LoadInt32(&c.max)
		if n <= old {
			break
		}
		if atomic.CompareAndSwapInt32(&c.max, old, n) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond) // widen the overlap window so goroutines genuinely contend
	return c.Provider.GetChange(ctx, repo, number)
}

// TestFetchChangesBoundedConcurrencyAndOrder drives fetchChanges (the fan-out
// step behind Resolve's step 1) with more change numbers than
// resolveConcurrency, using countingProvider to prove goroutines genuinely
// overlap (max > 1) while never exceeding the bound (max <= 4), and that the
// result is ordered to match the input regardless of which goroutine
// finished first (each result is written to out[i], not appended).
func TestFetchChangesBoundedConcurrencyAndOrder(t *testing.T) {
	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetDefaultBranch("main").SetCloneURL("file:///dev/null").Build()
	if err != nil {
		t.Fatal(err)
	}
	base := fake.New("fake", provider.KindGitLab)
	base.AddRepository(repo)
	sha := strings.Repeat("a", 40)
	numbers := make([]int, 0, 20)
	for i := 1; i <= 20; i++ {
		cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(i).SetTitle(fmt.Sprintf("c%d", i)).
			SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(time.Now()).SetMergeCommitSHA(sha).Build()
		if err != nil {
			t.Fatal(err)
		}
		base.AddChange(cr)
		numbers = append(numbers, i)
	}
	// Shuffle input order (descending) so a naive "append on completion"
	// implementation would very likely disagree with input order; out[i]
	// indexing must still line the result up with numbers[i].
	reversed := make([]int, len(numbers))
	for i, n := range numbers {
		reversed[len(numbers)-1-i] = n
	}

	cp := &countingProvider{Provider: base}
	cache := mirror.New(t.TempDir(), nil, &gitx.LockMap{}, testLog())
	r := NewResolver(cache, testLog())

	got, err := r.fetchChanges(context.Background(), cp, repo, reversed)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(reversed) {
		t.Fatalf("len = %d, want %d", len(got), len(reversed))
	}
	for i, cr := range got {
		if cr.Number() != reversed[i] {
			t.Fatalf("out[%d].Number() = %d, want %d (ordering not deterministic)", i, cr.Number(), reversed[i])
		}
	}
	max := atomic.LoadInt32(&cp.max)
	if max < 2 {
		t.Fatalf("max concurrent calls = %d, want > 1 (test did not genuinely contend)", max)
	}
	if max > resolveConcurrency {
		t.Fatalf("max concurrent calls = %d, want <= resolveConcurrency (%d)", max, resolveConcurrency)
	}
}

// TestResolveRejectsNotMergedBeforeFetchingMirror pins Finding 1's
// reject-fast property: NOT_MERGED must be returned using only provider data,
// before the mirror is ever fetched. The repository's clone URL points
// nowhere, so if Resolve reached the mirror-fetch step at all it would fail
// with a different error (repository unavailable / git failure), not
// NOT_MERGED; the mirror directory is also asserted absent afterward as a
// second, independent signal.
func TestResolveRejectsNotMergedBeforeFetchingMirror(t *testing.T) {
	runner := newExecRunnerForTest(t)
	root := t.TempDir()
	cache := mirror.New(root, runner, &gitx.LockMap{}, testLog())

	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/unreachable").SetDefaultBranch("main").
		SetCloneURL("file:///definitely/does/not/exist.git").Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)
	open, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(1).SetTitle("open").
		SetTargetBranch("main").SetState(provider.StateOpen).Build()
	if err != nil {
		t.Fatal(err)
	}
	p.AddChange(open)

	r := NewResolver(cache, testLog())
	var re *session.ReviewError
	_, err = r.Resolve(context.Background(), p, repo, "main", []int{1}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeNotMerged {
		t.Fatalf("err = %v, want NOT_MERGED", err)
	}

	mirrorPath, err := cache.Path(mirror.RootNamespace(), p.ID(), repo.FullName())
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(mirrorPath); !os.IsNotExist(statErr) {
		t.Fatalf("mirror directory exists at %s; the mirror was fetched despite the reject-fast NOT_MERGED path", mirrorPath)
	}
}

// TestClassifyRevParseErr pins Finding 2: a clean exit-1 from the final
// base-SHA RevParse (root commit, no first parent) is BASE_UNDETERMINED;
// anything else (a corrupted mirror, a cancelled context, or any other
// infrastructure failure) is GIT_FAILURE instead of being mislabelled as a
// root commit.
func TestClassifyRevParseErr(t *testing.T) {
	first := strings.Repeat("a", 40)

	exit1 := &gitx.ExitError{Category: gitx.CategoryQuery, Result: gitx.Result{ExitCode: 1}}
	wrapped1 := fmt.Errorf("rev-parse %s^1: %w", first, exit1)
	re1 := classifyRevParseErr(wrapped1, first, 7)
	if re1.Code != session.CodeBaseUndetermined || re1.Change != 7 {
		t.Fatalf("exit-1: code = %s, change = %d", re1.Code, re1.Change)
	}
	if !strings.Contains(re1.Message, "no first parent") {
		t.Fatalf("exit-1: message = %q, want mention of first parent", re1.Message)
	}

	exit128 := &gitx.ExitError{Category: gitx.CategoryQuery, Result: gitx.Result{ExitCode: 128}}
	wrapped128 := fmt.Errorf("rev-parse %s^1: %w", first, exit128)
	re2 := classifyRevParseErr(wrapped128, first, 7)
	if re2.Code != session.CodeGitFailure || re2.Change != 7 {
		t.Fatalf("exit-128: code = %s, change = %d", re2.Code, re2.Change)
	}
	if strings.Contains(re2.Message, "no first parent") {
		t.Fatalf("exit-128: message = %q, must not claim the commit has no first parent", re2.Message)
	}
}

// TestResolveBaseIsRootCommit drives Finding 2's root-commit branch
// end-to-end through Resolve. The single change is landed via the rebase
// (multi-commit, single-parent) path with n set so the walked history
// reaches all the way back to the repository's actual root commit (0
// parents); that root commit becomes the earliest landing SHA, so the final
// RevParse(first^1) genuinely fails with exit 1 and must surface as
// BASE_UNDETERMINED.
func TestResolveBaseIsRootCommit(t *testing.T) {
	src := testutil.NewRepo(t)
	root := src.Head() // the repository's true root commit: 0 parents

	src.Branch("feat/x")
	c1 := src.Commit("x1.txt", "1\n", "x1")
	c2 := src.Commit("x2.txt", "2\n", "x2")
	src.Checkout("main")
	src.Git("merge", "--ff-only", "feat/x")
	src.Push()

	runner := newExecRunnerForTest(t)
	cache := mirror.New(t.TempDir(), runner, &gitx.LockMap{}, testLog())

	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/root").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)

	rootCommit, _ := provider.NewCommit(root, "initial", time.Now())
	c1Commit, _ := provider.NewCommit(c1, "x1", time.Now())
	c2Commit, _ := provider.NewCommit(c2, "x2", time.Now())
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(1).SetTitle("root").
		SetAuthor("dev").SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(time.Now()).
		SetHeadSHA(c2).SetCommits([]provider.Commit{rootCommit, c1Commit, c2Commit}).Build()
	if err != nil {
		t.Fatal(err)
	}
	p.AddChange(cr)

	resolver := NewResolver(cache, testLog())
	var re *session.ReviewError
	_, err = resolver.Resolve(context.Background(), p, repo, "main", []int{1}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("err = %v, want BASE_UNDETERMINED", err)
	}
	if !strings.Contains(re.Message, "no first parent") {
		t.Fatalf("message = %q, want mention of first parent", re.Message)
	}
}

// cancelProvider errors for a chosen change number and blocks every other
// GetChange on ctx.Done() (with a long fallback timer), so a test can prove
// fetchChanges cancels siblings on the first error instead of letting them
// run to completion.
//
// The errAt call does not return its error until every sibling has entered
// GetChange (armed.Wait() below): fetchChanges' outer per-goroutine select —
// `select { case sem <- struct{}{}: ...; case <-cctx.Done(): ... }` — has a
// capacity-4 semaphore and exactly 4 goroutines, so with no barrier all four
// normally acquire a slot immediately; but if the errAt goroutine happens to
// return (and cancel cctx) before a sibling's goroutine has even reached that
// select, Go's select picks pseudo-randomly between the now-ready sem-acquire
// and ctx.Done() cases, and a sibling can take the cctx.Done() branch and
// return *without ever calling GetChange* — never touching cancelledCount.
// That is the flake this barrier removes (reproduced and confirmed below):
// waiting for every sibling to have entered GetChange first guarantees each
// one already won its semaphore slot deterministically (cctx cannot yet be
// cancelled), so cancellation can only ever be observed inside the inner
// select here, exactly what the assertions below check.
type cancelProvider struct {
	*fake.Provider
	errAt          int
	armed          sync.WaitGroup
	cancelledCount int32
	completedCount int32
}

func (c *cancelProvider) GetChange(ctx context.Context, repo provider.Repository, number int) (provider.ChangeRequest, error) {
	if number == c.errAt {
		c.armed.Wait()
		return provider.ChangeRequest{}, provider.ErrNotFound
	}
	c.armed.Done()
	select {
	case <-ctx.Done():
		atomic.AddInt32(&c.cancelledCount, 1)
		return provider.ChangeRequest{}, ctx.Err()
	case <-time.After(2 * time.Second):
		atomic.AddInt32(&c.completedCount, 1)
		return c.Provider.GetChange(ctx, repo, number)
	}
}

// TestFetchChangesCancelsSiblingsOnFirstError pins Finding 4: once one
// goroutine reports an error, the others must observe cancellation (via the
// ctx handed to the provider) rather than sleeping out their full duration.
// numbers is sized to exactly resolveConcurrency so all goroutines start
// in the same wave with no semaphore queueing to complicate the timing.
func TestFetchChangesCancelsSiblingsOnFirstError(t *testing.T) {
	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetDefaultBranch("main").SetCloneURL("file:///dev/null").Build()
	if err != nil {
		t.Fatal(err)
	}
	base := fake.New("fake", provider.KindGitLab)
	base.AddRepository(repo)
	cp := &cancelProvider{Provider: base, errAt: 1}
	cache := mirror.New(t.TempDir(), nil, &gitx.LockMap{}, testLog())
	r := NewResolver(cache, testLog())

	numbers := []int{1, 2, 3, 4}
	cp.armed.Add(len(numbers) - 1) // every sibling except errAt itself
	start := time.Now()
	_, err = r.fetchChanges(context.Background(), cp, repo, numbers)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected an error")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("fetchChanges took %s, want fail-fast (<500ms); siblings were not cancelled promptly", elapsed)
	}
	if atomic.LoadInt32(&cp.cancelledCount) == 0 {
		t.Fatal("no sibling goroutine observed cancellation")
	}
	if got := atomic.LoadInt32(&cp.completedCount); got != 0 {
		t.Fatalf("%d sibling(s) ran to completion instead of being cancelled", got)
	}
}

// TestLandingWithFetchRetriesAndSucceeds drives FR-5.8's fetch-retry path
// (Finding 5) genuinely: the candidate commit is pushed to origin on a
// throwaway branch and that branch's ref is then deleted, so the object is
// absent from any ref origin exposes and is not picked up by the mirror's
// normal clone/update. The first ResolveLanding attempt inside
// landingWithFetch must therefore miss it (ErrNoCandidate), triggering a
// direct `git fetch origin <sha>` that recovers the dangling object, after
// which the retried ResolveLanding succeeds.
func TestLandingWithFetchRetriesAndSucceeds(t *testing.T) {
	src := testutil.NewRepo(t)
	src.Branch("feat/dangling")
	cand := src.Commit("d.txt", "d\n", "d1")
	src.Checkout("main")
	src.Push()                                             // cand reachable on origin via feat/dangling
	src.Git("push", "origin", "--delete", "feat/dangling") // drop the ref; the object itself stays, now dangling

	runner := newExecRunnerForTest(t)
	cache := mirror.New(t.TempDir(), runner, &gitx.LockMap{}, testLog())

	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/dangling").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)

	commit, _ := provider.NewCommit(cand, "d1", time.Now())
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(1).SetTitle("dangling").
		SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(time.Now()).SetHeadSHA(cand).SetCommits([]provider.Commit{commit}).Build()
	if err != nil {
		t.Fatal(err)
	}
	p.AddChange(cr)

	r := NewResolver(cache, testLog())
	mirrorPath, err := r.mirrors.Ensure(context.Background(), mirror.RootNamespace(), p, repo)
	if err != nil {
		t.Fatal(err)
	}
	objects := r.mirrors.Objects(mirrorPath, repo.FullName())

	// Precondition: the candidate is genuinely absent before the retry, or
	// this test would not be exercising the fetch-retry path at all.
	if ok, existErr := objects.Exists(context.Background(), cand); existErr != nil || ok {
		t.Fatalf("precondition: candidate already present in the mirror (ok=%v err=%v)", ok, existErr)
	}

	landing, err := r.landingWithFetch(context.Background(), p, repo, objects, cr, []provider.Commit{commit})
	if err != nil {
		t.Fatalf("landingWithFetch: %v", err)
	}
	if landing.Strategy != session.StrategySquash || len(landing.SHAs) != 1 || landing.SHAs[0] != cand {
		t.Fatalf("landing = %+v", landing)
	}
}

// TestLandingWithFetchAbsorbsFetchFailure pins FR-5.8's documented behavior
// (Finding 5): when the one-shot fetch-by-SHA retry itself fails (the SHA
// doesn't exist anywhere the provider can reach), that failure is absorbed
// into the same generic MISSING_COMMITS outcome ResolveLanding already
// returns for "no candidate present" — it is not surfaced as a distinct git
// or provider error. This is a documented decision, not an accident: pinned
// here so a future change to that behavior is deliberate.
func TestLandingWithFetchAbsorbsFetchFailure(t *testing.T) {
	f := newResolveFixture(t)
	fakeSHA := strings.Repeat("f", 40)
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(f.repo).SetNumber(99).SetTitle("ghost").
		SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(time.Now()).SetHeadSHA(fakeSHA).Build()
	if err != nil {
		t.Fatal(err)
	}

	mirrorPath, err := f.resolver.mirrors.Ensure(context.Background(), mirror.RootNamespace(), f.prov, f.repo)
	if err != nil {
		t.Fatal(err)
	}
	objects := f.resolver.mirrors.Objects(mirrorPath, f.repo.FullName())

	_, err = f.resolver.landingWithFetch(context.Background(), f.prov, f.repo, objects, cr, nil)
	var re *session.ReviewError
	if !errors.As(err, &re) || re.Code != session.CodeMissingCommits {
		t.Fatalf("err = %v, want MISSING_COMMITS (FetchSHA's failure absorbed into the standard no-candidate outcome)", err)
	}
}

// commitsErrProvider wraps a provider.GitProvider and forces GetChangeCommits
// to fail for one specific change number, delegating every other call
// (including GetChangeCommits for any other number) to the inner provider.
// fake.Provider's FailWith is a one-shot error consumed by the *next* call to
// any method, including the concurrent GetChange calls fetchChanges issues
// before the per-change GetChangeCommits loop runs — it cannot target
// GetChangeCommits alone. This wrapper is the only way to exercise the
// GetChangeCommits error branches of Resolve's per-change loop (resolve.go
// lines ~140-148), which Task 13 left with no unit coverage at all.
type commitsErrProvider struct {
	provider.GitProvider
	number int
	err    error
}

func (p *commitsErrProvider) GetChangeCommits(ctx context.Context, repo provider.Repository, number int) ([]provider.Commit, error) {
	if number == p.number {
		return nil, p.err
	}
	return p.GitProvider.GetChangeCommits(ctx, repo, number)
}

// TestResolveGetChangeCommitsTooManyCommits pins the ErrTooManyCommits branch
// of Resolve's per-change loop: a provider that cannot enumerate a change's
// commits (GitLab/GitHub return this when a change has more commits than the
// API will list) must be reported as BASE_UNDETERMINED, not a generic
// provider failure, so the operator sees why the change couldn't be verified.
func TestResolveGetChangeCommitsTooManyCommits(t *testing.T) {
	f := newResolveFixture(t)
	wrapped := &commitsErrProvider{GitProvider: f.prov, number: 1, err: provider.ErrTooManyCommits}
	var re *session.ReviewError
	_, err := f.resolver.Resolve(context.Background(), wrapped, f.repo, "main", []int{1}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined || re.Change != 1 {
		t.Fatalf("err = %v, want BASE_UNDETERMINED for change 1", err)
	}
}

// TestResolveGetChangeCommitsProviderError pins the two remaining branches of
// the same loop: a classified provider sentinel (ErrAuth) must map through
// MapProviderError to its specific code, while an error MapProviderError does
// not recognise falls back to PROVIDER_UNAVAILABLE rather than being dropped
// or misreported as a git failure.
func TestResolveGetChangeCommitsProviderError(t *testing.T) {
	f := newResolveFixture(t)

	authWrapped := &commitsErrProvider{GitProvider: f.prov, number: 1, err: provider.ErrAuth}
	var re *session.ReviewError
	_, err := f.resolver.Resolve(context.Background(), authWrapped, f.repo, "main", []int{1}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeProviderAuth {
		t.Fatalf("err = %v, want PROVIDER_AUTH", err)
	}

	unclassified := errors.New("commits endpoint exploded")
	unclassifiedWrapped := &commitsErrProvider{GitProvider: f.prov, number: 1, err: unclassified}
	re = nil
	_, err = f.resolver.Resolve(context.Background(), unclassifiedWrapped, f.repo, "main", []int{1}, nil)
	if !errors.As(err, &re) || re.Code != session.CodeProviderUnavailable {
		t.Fatalf("err = %v, want PROVIDER_UNAVAILABLE for an unclassified error", err)
	}
}
