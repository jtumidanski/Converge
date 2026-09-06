//go:build integration

package review

import (
	"context"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
	"github.com/jtumidanski/converge/internal/workspace"
)

// harness wires a real git repository to a fake provider and a full Service.
type harness struct {
	src        *testutil.Repo
	prov       *fake.Provider
	repo       provider.Repository
	svc        *Service
	runner     *gitx.ExecRunner
	mirrors    *mirror.Cache
	workspaces *workspace.Manager
	mergedAt   time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	src := testutil.NewRepo(t)
	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{AllowFileProtocol: true, CommandTimeout: time.Minute, CloneTimeout: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	locks := &gitx.LockMap{}
	mirrors := mirror.New(t.TempDir(), runner, locks, testLog())
	ws, err := workspace.New(t.TempDir(), runner, locks, testLog())
	if err != nil {
		t.Fatal(err)
	}
	repo, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName("atlas/server").SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("fake", provider.KindGitLab)
	p.AddRepository(repo)
	registry := provider.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	cleaner := NewCleaner(mirrors, ws, testLog())
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, testLog(), time.Now)
	svc := NewService(Deps{
		Providers: registry, Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: NewCherryPickApplicator(runner, testLog()), Runner: runner, Log: testLog(),
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 4, Now: time.Now,
	})
	return &harness{src: src, prov: p, repo: repo, svc: svc, runner: runner, mirrors: mirrors, workspaces: ws, mergedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}
}

// addChange registers a merged change; landing is the SHA the provider reports.
func (h *harness) addChange(t *testing.T, number int, merge, squash, head string, commitSHAs []string) {
	t.Helper()
	h.mergedAt = h.mergedAt.Add(time.Hour)
	commits := make([]provider.Commit, 0, len(commitSHAs))
	for _, s := range commitSHAs {
		c, err := provider.NewCommit(s, "m", h.mergedAt)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, c)
	}
	b := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(h.repo).SetNumber(number).
		SetTitle("change " + itoa(number)).SetAuthor("dev").SetTargetBranch("main").SetState(provider.StateMerged).
		SetMergedAt(h.mergedAt).SetCommits(commits)
	if merge != "" {
		b.SetMergeCommitSHA(merge)
	}
	if squash != "" {
		b.SetSquashCommitSHA(squash)
	}
	if head != "" {
		b.SetHeadSHA(head)
	}
	cr, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	h.prov.AddChange(cr)
}

// build runs Create + Build and returns the final session.
func (h *harness) build(t *testing.T, numbers ...int) session.Session {
	t.Helper()
	ctx := context.Background()
	s, err := h.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server", BaseBranch: "main", Changes: numbers})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return h.svc.Build(ctx, s.ID())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
