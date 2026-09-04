package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
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
