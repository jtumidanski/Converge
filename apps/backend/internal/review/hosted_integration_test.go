//go:build integration

package review

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/identity"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
	"github.com/jtumidanski/converge/internal/workspace"
)

// scopedResolver maps a hosted user id to the provider registry it configured
// through its own account, standing in for auth.ProviderResolver. The real
// resolver (internal/auth) only ever builds github/gitlab clients from stored
// credentials, so it cannot host the local-git fake provider these tests
// need to run without network; a per-user map of registries is the minimal
// thing that satisfies provider.Resolver while still proving two accounts
// see two independent provider configurations.
type scopedResolver struct{ byUser map[string]*provider.Registry }

func (r scopedResolver) Resolve(_ context.Context, scope identity.Scope) (*provider.Registry, error) {
	reg, ok := r.byUser[scope.UserID()]
	if !ok {
		return nil, fmt.Errorf("hosted_integration_test: no registry configured for user %q", scope.UserID())
	}
	return reg, nil
}

// hostedHarness wires one shared mirror cache, workspace root and session
// store (as a real hosted-mode deployment would) behind a Service whose
// provider resolver answers per-user, so two identity.Scope values exercise
// genuine account isolation rather than a single shared provider instance.
type hostedHarness struct {
	src     *testutil.Repo
	repo    provider.Repository
	provs   map[string]*fake.Provider
	svc     *Service
	runner  *gitx.ExecRunner
	mirrors *mirror.Cache
	store   *session.Store
}

// newHostedHarness builds one repository and one merged change, then
// registers a separate fake.Provider (and registry) per user id in users so
// each account has its own provider configuration for the same "fake" slug,
// matching how two real users would each configure their own credentials
// against the same GitLab host.
func newHostedHarness(t *testing.T, users ...string) *hostedHarness {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Branch("feat/a")
	c := src.Commit("a.txt", "a\n", "a1")
	src.Checkout("main")
	sq := src.Squash("feat/a", "squash a")
	src.Push()

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
	commit, err := provider.NewCommit(c, "a1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("fake").SetRepository(repo).SetNumber(1).SetTitle("change 1").
		SetAuthor("dev").SetTargetBranch("main").SetState(provider.StateMerged).SetMergedAt(time.Now()).
		SetSquashCommitSHA(sq).SetHeadSHA(c).SetCommits([]provider.Commit{commit}).Build()
	if err != nil {
		t.Fatal(err)
	}

	provs := make(map[string]*fake.Provider, len(users))
	byUser := make(map[string]*provider.Registry, len(users))
	for _, u := range users {
		p := fake.New("fake", provider.KindGitLab)
		p.AddRepository(repo)
		p.AddChange(cr)
		registry := provider.NewRegistry()
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
		provs[u] = p
		byUser[u] = registry
	}

	cleaner := NewCleaner(mirrors, ws, testLog())
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, testLog(), time.Now)
	svc := NewService(Deps{
		Providers: scopedResolver{byUser: byUser}, Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: NewCherryPickApplicator(runner, testLog()), Runner: runner, Log: testLog(),
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 4, Now: time.Now,
	})

	return &hostedHarness{src: src, repo: repo, provs: provs, svc: svc, runner: runner, mirrors: mirrors, store: store}
}

// buildAs runs Create + Build on behalf of scope and fails the test unless
// the resulting session reaches StatusReady.
func (h *hostedHarness) buildAs(t *testing.T, scope identity.Scope) session.Session {
	t.Helper()
	ctx := context.Background()
	sess, err := h.svc.Create(ctx, scope, CreateInput{ProviderID: "fake", Repository: "atlas/server", BaseBranch: "main", Changes: []int{1}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got := h.svc.Build(ctx, sess.ID())
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	return got
}

// TestTwoUsersReviewTheSameRepositoryInSeparateMirrors is the isolation
// acceptance criterion at full depth: two users, two provider configurations,
// one repository, two mirror directories, two review sessions, and neither
// visible to the other.
func TestTwoUsersReviewTheSameRepositoryInSeparateMirrors(t *testing.T) {
	const idA = "aaaaaaaaaaaaaaaa"
	const idB = "bbbbbbbbbbbbbbbb"
	h := newHostedHarness(t, idA, idB)

	gotA := h.buildAs(t, identity.ForUser(idA))
	gotB := h.buildAs(t, identity.ForUser(idB))

	nsA, err := mirror.NamespaceFor(identity.ForUser(idA))
	if err != nil {
		t.Fatal(err)
	}
	nsB, err := mirror.NamespaceFor(identity.ForUser(idB))
	if err != nil {
		t.Fatal(err)
	}
	pathA, err := h.mirrors.Path(nsA, "fake", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	pathB, err := h.mirrors.Path(nsB, "fake", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pathA, "HEAD")); err != nil {
		t.Errorf("A's mirror missing at %s: %v", pathA, err)
	}
	if _, err := os.Stat(filepath.Join(pathB, "HEAD")); err != nil {
		t.Errorf("B's mirror missing at %s: %v", pathB, err)
	}
	if pathA == pathB {
		t.Fatalf("A and B share one mirror path: %s", pathA)
	}
	rootPath, err := h.mirrors.Path(mirror.RootNamespace(), "fake", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rootPath); !os.IsNotExist(err) {
		t.Errorf("mirror exists directly under the cache root at %s (err=%v)", rootPath, err)
	}

	listA := h.store.List(identity.ForUser(idA))
	if len(listA) != 1 || listA[0].ID() != gotA.ID() {
		t.Fatalf("A's list = %+v, want exactly [%s]", listA, gotA.ID())
	}
	if _, ok := h.store.Get(gotB.ID(), identity.ForUser(idA)); ok {
		t.Fatalf("A can see B's session %s", gotB.ID())
	}

	pDiffA, err := h.svc.CombinedDiffPath(gotA.ID(), identity.ForUser(idA))
	if err != nil {
		t.Fatal(err)
	}
	pDiffB, err := h.svc.CombinedDiffPath(gotB.ID(), identity.ForUser(idB))
	if err != nil {
		t.Fatal(err)
	}
	rawA, err := os.ReadFile(pDiffA)
	if err != nil {
		t.Fatal(err)
	}
	rawB, err := os.ReadFile(pDiffB)
	if err != nil {
		t.Fatal(err)
	}
	if len(rawA) == 0 {
		t.Fatal("A's combined diff is empty")
	}
	if string(rawA) != string(rawB) {
		t.Errorf("combined diffs differ:\nA: %s\nB: %s", rawA, rawB)
	}
}

// TestHostedPruneDoesNotDeleteALiveReviewBranch is the FR-6.6 evidence.
//
// The design argues this holds by construction: namespacing changes which
// directory the refspec applies to, not the refspec, so
// gitx.ExcludeReviewRefspec (commit b8a8391) still protects the branches a
// live worktree has checked out. "Holds by construction" is a claim, not
// evidence, so it gets a test under a namespace.
func TestHostedPruneDoesNotDeleteALiveReviewBranch(t *testing.T) {
	const idA = "cccccccccccccccc"
	h := newHostedHarness(t, idA)
	scope := identity.ForUser(idA)

	got := h.buildAs(t, scope)

	ns, err := mirror.NamespaceFor(scope)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := h.mirrors.Ensure(ctx, ns, h.provs[idA], h.repo); err != nil {
		t.Fatalf("prune fetch: %v", err)
	}

	mirrorPath, err := h.mirrors.Path(ns, "fake", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"branch", "--list", "review/" + got.ID()}, Category: gitx.CategoryQuery})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Stdout), got.ID()) {
		t.Errorf("review branch review/%s was pruned from the namespaced mirror; branch --list = %q", got.ID(), res.Stdout)
	}

	reGot, ok := h.store.Get(got.ID(), scope)
	if !ok || reGot.Status() != session.StatusReady {
		t.Fatalf("session after prune: ok=%v status=%s", ok, reGot.Status())
	}
	files, err := h.svc.Files(got.ID(), scope)
	if err != nil || len(files) == 0 {
		t.Fatalf("files after prune: %+v err=%v", files, err)
	}
}
