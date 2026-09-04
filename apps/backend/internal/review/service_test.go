package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

type serviceFixture struct {
	svc  *Service
	prov *fake.Provider
	src  *testutil.Repo
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Commit("unrelated.txt", "u\n", "unrelated before")
	src.Branch("feat/a")
	src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	mergeSHA := src.MergeNoFF("feat/a", "merge a")
	src.Commit("unrelated.txt", "u2\n", "unrelated after")
	src.Push()

	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
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
	c, _ := provider.NewCommit(src.RevParse("feat/a"), "a1", time.Now())
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(1).SetTitle("Add a").
		SetAuthor("dev").SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(time.Now()).
		SetMergeCommitSHA(mergeSHA).SetCommits([]provider.Commit{c}).Build()
	if err != nil {
		t.Fatal(err)
	}
	p.AddChange(cr)
	registry := provider.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	cleaner := NewCleaner(mirrors, ws, testLog())
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, testLog(), time.Now)
	svc := NewService(Deps{
		Providers: registry, Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: NewCherryPickApplicator(runner, testLog()), Runner: runner, Log: testLog(),
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 2, Now: time.Now,
	})
	return &serviceFixture{svc: svc, prov: p, src: src}
}

func TestServiceBuildHappyPath(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	s, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server", Changes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Status() != session.StatusCreating || s.BaseBranch() != "main" {
		t.Fatalf("created = %+v", s)
	}
	done := f.svc.Build(ctx, s.ID())
	if done.Status() != session.StatusReady {
		t.Fatalf("status = %s err = %+v", done.Status(), done.Error())
	}
	if done.BaseSHA() == "" || done.HeadSHA() == "" || done.Totals() == nil || done.Totals().Files != 1 {
		t.Fatalf("session = %+v totals=%+v", done, done.Totals())
	}
	files, err := f.svc.Files(done.ID())
	if err != nil || len(files) != 1 || files[0].Path != "a.txt" {
		t.Fatalf("files = %v %v", files, err)
	}
	fd, err := f.svc.FileDiff(ctx, done.ID(), "a.txt")
	if err != nil || !strings.Contains(fd.Diff, "+a") {
		t.Fatalf("file diff = %+v %v", fd, err)
	}
	if _, err := f.svc.FileDiff(ctx, done.ID(), "unrelated.txt"); err == nil {
		t.Error("unknown file must fail")
	}
	p, err := f.svc.CombinedDiffPath(done.ID())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "a.txt") || strings.Contains(string(b), "unrelated.txt") {
		t.Errorf("combined diff wrong:\n%s", b)
	}
	// finish removes everything
	if err := f.svc.Finish(ctx, done.ID()); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Get(done.ID()); got.Status() != session.StatusFinished {
		t.Errorf("status = %s", got.Status())
	}
	if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
		t.Error("session dir remains")
	}
	if err := f.svc.Finish(ctx, done.ID()); err != nil {
		t.Errorf("finish must be idempotent: %v", err)
	}
	if _, err := f.svc.Files(done.ID()); !errors.Is(err, ErrNotReady) {
		t.Errorf("files after finish: %v", err)
	}
}

func TestServiceCreateValidationAndUnknownProvider(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	var ie *InputError
	if _, err := f.svc.Create(ctx, CreateInput{ProviderID: "nope", Repository: "atlas/server", Changes: []int{1}}); !errors.As(err, &ie) || ie.Code != CodeInvalidProvider {
		t.Fatalf("unknown provider: %v", err)
	}
	if _, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "../x", Changes: []int{1}}); !errors.As(err, &ie) || ie.Code != CodeInvalidRepository {
		t.Fatalf("bad repo: %v", err)
	}
	if _, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/nope", Changes: []int{1}}); err == nil {
		t.Fatal("unknown repository must fail")
	}
	if _, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server"}); !errors.As(err, &ie) || ie.Code != CodeInvalidChanges {
		t.Fatalf("empty changes: %v", err)
	}
}

func TestServiceBuildFailureIsRecorded(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	open, _ := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(mustRepo(t, f)).SetNumber(2).SetTitle("open").
		SetTargetBranch("main").SetState(provider.StateOpen).Build()
	f.prov.AddChange(open)
	s, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server", Changes: []int{2}})
	if err != nil {
		t.Fatal(err)
	}
	done := f.svc.Build(ctx, s.ID())
	if done.Status() != session.StatusFailed || done.Error() == nil || done.Error().Code != session.CodeNotMerged {
		t.Fatalf("done = %+v err=%+v", done, done.Error())
	}
	if _, err := f.svc.Files(done.ID()); !errors.Is(err, ErrNotReady) {
		t.Errorf("files on failed session: %v", err)
	}
}

func TestServiceStartBuildIsAsynchronous(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	s, err := f.svc.Create(ctx, CreateInput{ProviderID: "fake", Repository: "atlas/server", Changes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.StartBuild(context.Background(), s.ID())
	deadline := time.Now().Add(60 * time.Second)
	for {
		got, _ := f.svc.Get(s.ID())
		if got.Status() != session.StatusCreating {
			if got.Status() != session.StatusReady {
				t.Fatalf("status = %s err = %+v", got.Status(), got.Error())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("build did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func mustRepo(t *testing.T, f *serviceFixture) provider.Repository {
	t.Helper()
	r, err := f.prov.GetRepository(context.Background(), "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
