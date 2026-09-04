package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// hookApplicator wraps the real applicator so a test can observe an Apply
// call, or replace its result, without widening Deps: ChangeApplicator is
// already an interface, so this is the injection point the service was built
// with.
type hookApplicator struct {
	mu       sync.Mutex
	delegate ChangeApplicator
	before   func(ctx context.Context, rc session.ResolvedChange)
	override func(ctx context.Context, rc session.ResolvedChange) (ApplyResult, error)
}

func (a *hookApplicator) set(
	before func(context.Context, session.ResolvedChange),
	override func(context.Context, session.ResolvedChange) (ApplyResult, error),
) {
	a.mu.Lock()
	a.before, a.override = before, override
	a.mu.Unlock()
}

func (a *hookApplicator) Apply(ctx context.Context, repoDir, providerID string, rc session.ResolvedChange) (ApplyResult, error) {
	a.mu.Lock()
	before, override := a.before, a.override
	a.mu.Unlock()
	if before != nil {
		before(ctx, rc)
	}
	if override != nil {
		return override(ctx, rc)
	}
	return a.delegate.Apply(ctx, repoDir, providerID, rc)
}

type serviceFixture struct {
	svc  *Service
	prov *fake.Provider
	src  *testutil.Repo
	ws   *workspace.Manager
	app  *hookApplicator

	mu      sync.Mutex
	nowHook func()
}

// setNowHook installs a callback run on every Deps.Now() call. Deps.Now is a
// per-call hook, which makes it the one lever that can interleave another
// operation at an exact point in the pipeline.
func (f *serviceFixture) setNowHook(h func()) {
	f.mu.Lock()
	f.nowHook = h
	f.mu.Unlock()
}

func (f *serviceFixture) now() time.Time {
	f.mu.Lock()
	h := f.nowHook
	f.mu.Unlock()
	if h != nil {
		h()
	}
	return time.Now()
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	return newServiceFixtureWith(t, 2)
}

func newServiceFixtureWith(t *testing.T, maxConcurrentBuilds int) *serviceFixture {
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
	// The store keeps time.Now so a fixture now-hook cannot recurse into it.
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, testLog(), time.Now)
	f := &serviceFixture{prov: p, src: src, ws: ws,
		app: &hookApplicator{delegate: NewCherryPickApplicator(runner, testLog())}}
	f.svc = NewService(Deps{
		Providers: registry, Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: f.app, Runner: runner, Log: testLog(),
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: maxConcurrentBuilds, Now: f.now,
	})
	return f
}

// create makes a CREATING session for change #1.
func (f *serviceFixture) create(t *testing.T) session.Session {
	t.Helper()
	s, err := f.svc.Create(context.Background(), CreateInput{
		ProviderID: "fake", Repository: "atlas/server", Changes: []int{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// awaitTerminal polls until the session leaves CREATING or the deadline passes.
func (f *serviceFixture) awaitTerminal(t *testing.T, id string, within time.Duration) session.Session {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		got, _ := f.svc.Get(id)
		if got.Status() != session.StatusCreating {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %s still CREATING after %s", id, within)
		}
		time.Sleep(20 * time.Millisecond)
	}
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

// TestServiceStartBuildIsAsynchronous gates the build inside Apply and
// requires StartBuild to return while it is still gated. A synchronous
// StartBuild blocks until the watchdog releases the gate, so the elapsed-time
// assertion fails rather than the test hanging.
func TestServiceStartBuildIsAsynchronous(t *testing.T) {
	f := newServiceFixture(t)
	s := f.create(t)

	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	// Watchdog: a synchronous StartBuild must still return, so the failure is
	// an assertion rather than a deadlock.
	watchdog := time.AfterFunc(3*time.Second, unblock)
	defer watchdog.Stop()
	entered := make(chan struct{}, 1)
	f.app.set(func(context.Context, session.ResolvedChange) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	}, nil)

	start := time.Now()
	f.svc.StartBuild(context.Background(), s.ID())
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("StartBuild blocked for %s: it must not run the build on the caller's goroutine", elapsed)
	}

	select {
	case <-entered:
	case <-time.After(60 * time.Second):
		t.Fatal("the background build never reached Apply")
	}
	if got, _ := f.svc.Get(s.ID()); got.Status() != session.StatusCreating {
		t.Fatalf("status while the build is gated = %s, want CREATING", got.Status())
	}

	unblock()
	if got := f.awaitTerminal(t, s.ID(), 60*time.Second); got.Status() != session.StatusReady {
		t.Fatalf("status = %s err = %+v", got.Status(), got.Error())
	}
}

// TestServiceStartBuildRespectsMaxConcurrentBuilds exercises the semaphore
// bound: more builds are started than the limit allows, and every Apply
// records how many are running at once.
func TestServiceStartBuildRespectsMaxConcurrentBuilds(t *testing.T) {
	const limit = 2
	const builds = 4
	f := newServiceFixtureWith(t, limit)

	var mu sync.Mutex
	inflight, peak := 0, 0
	f.app.set(func(context.Context, session.ResolvedChange) {
		mu.Lock()
		inflight++
		if inflight > peak {
			peak = inflight
		}
		mu.Unlock()
		// Hold the slot long enough for a second build to overlap.
		time.Sleep(250 * time.Millisecond)
		mu.Lock()
		inflight--
		mu.Unlock()
	}, nil)

	ids := make([]string, 0, builds)
	for range builds {
		ids = append(ids, f.create(t).ID())
	}
	for _, id := range ids {
		f.svc.StartBuild(context.Background(), id)
	}
	for _, id := range ids {
		if got := f.awaitTerminal(t, id, 120*time.Second); got.Status() != session.StatusReady {
			t.Fatalf("session %s status = %s err = %+v", id, got.Status(), got.Error())
		}
	}
	mu.Lock()
	got := peak
	mu.Unlock()
	if got > limit {
		t.Errorf("peak concurrent builds = %d, want <= MaxConcurrentBuilds (%d)", got, limit)
	}
	if got < 2 {
		t.Errorf("peak concurrent builds = %d: the builds never overlapped, so the bound was not exercised", got)
	}
}

// TestServiceBuildRecordsFailureOnTheLiveSession covers every failure the
// applicator can produce. Each case asserts both the terminal status/code and
// that the record keeps the state the build had already persisted (base SHA
// and resolved changes, FR-8.3) rather than rolling back to the pre-build
// snapshot.
func TestServiceBuildRecordsFailureOnTheLiveSession(t *testing.T) {
	tests := []struct {
		name       string
		override   func(context.Context, session.ResolvedChange) (ApplyResult, error)
		wantStatus session.Status
		wantCode   session.Code
	}{
		{
			name: "conflict",
			override: func(context.Context, session.ResolvedChange) (ApplyResult, error) {
				return ApplyResult{Outcome: OutcomeConflict, ConflictingPaths: []string{"a.txt"}}, nil
			},
			wantStatus: session.StatusConflicted,
			wantCode:   session.CodeConflict,
		},
		{
			name: "apply error",
			override: func(context.Context, session.ResolvedChange) (ApplyResult, error) {
				return ApplyResult{}, errors.New("git exploded")
			},
			wantStatus: session.StatusFailed,
			wantCode:   session.CodeGitFailure,
		},
		{
			name: "unknown outcome",
			override: func(context.Context, session.ResolvedChange) (ApplyResult, error) {
				return ApplyResult{Outcome: Outcome("bewildering")}, nil
			},
			wantStatus: session.StatusFailed,
			wantCode:   session.CodeGitFailure,
		},
		{
			name: "panic",
			override: func(context.Context, session.ResolvedChange) (ApplyResult, error) {
				panic("applicator exploded")
			},
			wantStatus: session.StatusFailed,
			wantCode:   session.CodeGitFailure,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newServiceFixture(t)
			s := f.create(t)
			f.app.set(nil, tt.override)
			done := f.svc.Build(context.Background(), s.ID())
			stored, ok := f.svc.Get(s.ID())
			if !ok {
				t.Fatal("session vanished")
			}
			for _, got := range []session.Session{done, stored} {
				if got.Status() != tt.wantStatus {
					t.Fatalf("status = %s err = %+v, want %s", got.Status(), got.Error(), tt.wantStatus)
				}
				if got.Error() == nil || got.Error().Code != tt.wantCode {
					t.Fatalf("error = %+v, want code %s", got.Error(), tt.wantCode)
				}
				if got.BaseSHA() == "" {
					t.Error("base sha was rolled back to the pre-build snapshot")
				}
				if len(got.ResolvedChanges()) != 1 {
					t.Errorf("resolved changes = %d, want 1", len(got.ResolvedChanges()))
				}
			}
		})
	}
}

// TestServiceBuildDoesNotResurrectATerminalSession is the C1 regression: a
// review discarded while its build is running must stay FINISHED, and the
// build must not re-create the session directory that Cleanup removed.
func TestServiceBuildDoesNotResurrectATerminalSession(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		f := newServiceFixture(t)
		ctx := context.Background()
		s := f.create(t)
		combined := filepath.Join(f.ws.SessionDir(s.ID()), CombinedDiffFile)
		var once sync.Once
		// The first Now() call after the combined diff exists is the one
		// Session.Ready is about to be given, so Finish lands in exactly the
		// window between writing the diff and recording READY.
		f.setNowHook(func() {
			if _, err := os.Stat(combined); err != nil {
				return
			}
			once.Do(func() {
				if err := f.svc.Finish(ctx, s.ID()); err != nil {
					t.Errorf("finish: %v", err)
				}
			})
		})
		done := f.svc.Build(ctx, s.ID())
		stored, _ := f.svc.Get(s.ID())
		if done.Status() != session.StatusFinished {
			t.Errorf("returned status = %s, want FINISHED", done.Status())
		}
		if stored.Status() != session.StatusFinished {
			t.Errorf("stored status = %s, want FINISHED", stored.Status())
		}
		if _, err := os.Stat(f.ws.SessionDir(s.ID())); !os.IsNotExist(err) {
			t.Errorf("session directory was re-created after cleanup: %v", err)
		}
	})

	t.Run("failure", func(t *testing.T) {
		f := newServiceFixture(t)
		ctx := context.Background()
		s := f.create(t)
		f.app.set(func(actx context.Context, _ session.ResolvedChange) {
			if err := f.svc.Finish(actx, s.ID()); err != nil {
				t.Errorf("finish: %v", err)
			}
		}, func(context.Context, session.ResolvedChange) (ApplyResult, error) {
			return ApplyResult{Outcome: OutcomeConflict, ConflictingPaths: []string{"a.txt"}}, nil
		})
		done := f.svc.Build(ctx, s.ID())
		stored, _ := f.svc.Get(s.ID())
		if done.Status() != session.StatusFinished || stored.Status() != session.StatusFinished {
			t.Errorf("returned = %s stored = %s, want FINISHED", done.Status(), stored.Status())
		}
		if _, err := os.Stat(f.ws.SessionDir(s.ID())); !os.IsNotExist(err) {
			t.Errorf("session directory was re-created after cleanup: %v", err)
		}
	})
}

// TestServiceBuildCancellationIsRecordedAsInterrupted is the I4 regression: a
// build cut short by shutdown must not be flattened into GIT_FAILURE.
func TestServiceBuildCancellationIsRecordedAsInterrupted(t *testing.T) {
	f := newServiceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := f.create(t)
	f.app.set(nil, func(actx context.Context, _ session.ResolvedChange) (ApplyResult, error) {
		cancel() // the server is shutting down mid-apply
		return ApplyResult{}, actx.Err()
	})
	done := f.svc.Build(ctx, s.ID())
	stored, _ := f.svc.Get(s.ID())
	for _, got := range []session.Session{done, stored} {
		if got.Status() != session.StatusFailed {
			t.Fatalf("status = %s, want FAILED", got.Status())
		}
		if got.Error() == nil || got.Error().Code != session.CodeInterrupted {
			t.Fatalf("error = %+v, want code INTERRUPTED", got.Error())
		}
	}
}

// TestServiceBuildPersistFailureIsRecorded drives the Store.Save failure path
// by making the session directory unwritable just before READY is persisted.
func TestServiceBuildPersistFailureIsRecorded(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newServiceFixture(t)
	ctx := context.Background()
	s := f.create(t)
	dir := f.ws.SessionDir(s.ID())
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
	combined := filepath.Join(dir, CombinedDiffFile)
	var once sync.Once
	f.setNowHook(func() {
		if _, err := os.Stat(combined); err != nil {
			return
		}
		once.Do(func() {
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Errorf("chmod: %v", err)
			}
		})
	})
	done := f.svc.Build(ctx, s.ID())
	if done.Status() != session.StatusFailed {
		t.Fatalf("status = %s, want FAILED (READY must never be reported on a failed write)", done.Status())
	}
	if done.Error() == nil || done.Error().Code != session.CodeGitFailure {
		t.Fatalf("error = %+v, want code GIT_FAILURE", done.Error())
	}
}

// TestServiceBuildUnknownSession pins the one outcome Build cannot report
// through a session: an id that is not in the store. The zero session is
// unambiguous because every real session has a non-empty id.
func TestServiceBuildUnknownSession(t *testing.T) {
	f := newServiceFixture(t)
	got := f.svc.Build(context.Background(), "deadbeef")
	if got.ID() != "" || got.Status() != "" {
		t.Fatalf("build of an unknown id = %+v, want the zero session", got)
	}
}

// TestServiceBuildOnANonCreatingSessionDoesNotRerun pins the guard that keeps
// a second Build from rebuilding a session whose workspace may be gone.
func TestServiceBuildOnANonCreatingSessionDoesNotRerun(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	s := f.create(t)
	done := f.svc.Build(ctx, s.ID())
	if done.Status() != session.StatusReady {
		t.Fatalf("status = %s err = %+v", done.Status(), done.Error())
	}
	again := f.svc.Build(ctx, s.ID())
	if again.Status() != session.StatusReady {
		t.Fatalf("second build status = %s", again.Status())
	}
	if !again.UpdatedAt().Equal(done.UpdatedAt()) {
		t.Errorf("second build re-ran the pipeline: updatedAt %s -> %s", done.UpdatedAt(), again.UpdatedAt())
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
