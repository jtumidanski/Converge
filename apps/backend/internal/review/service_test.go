package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
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

// logWatch lets a test wait for a specific log message instead of sleeping.
// Some branches (a queued build draining on shutdown) produce no observable
// state change at all — the session deliberately stays CREATING — so the log
// record is the only deterministic signal that the branch ran.
type logWatch struct {
	mu      sync.Mutex
	waiters map[string]chan struct{}
}

func newLogWatch() *logWatch { return &logWatch{waiters: map[string]chan struct{}{}} }

// wait registers interest in msg before the code under test runs. The
// returned channel is closed the first time that message is logged.
func (w *logWatch) wait(msg string) <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch := make(chan struct{})
	w.waiters[msg] = ch
	return ch
}

func (w *logWatch) note(msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ch, ok := w.waiters[msg]; ok {
		close(ch)
		delete(w.waiters, msg)
	}
}

// watchHandler reports every record to a logWatch and forwards it to inner
// only if inner wants it. Enabled is always true so an Info record still
// reaches the watch even though the test handler only prints warnings.
type watchHandler struct {
	inner slog.Handler
	watch *logWatch
}

func (h *watchHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *watchHandler) Handle(ctx context.Context, r slog.Record) error {
	h.watch.note(r.Message)
	if h.inner.Enabled(ctx, r.Level) {
		return h.inner.Handle(ctx, r)
	}
	return nil
}

func (h *watchHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &watchHandler{inner: h.inner.WithAttrs(attrs), watch: h.watch}
}

func (h *watchHandler) WithGroup(name string) slog.Handler {
	return &watchHandler{inner: h.inner.WithGroup(name), watch: h.watch}
}

type serviceFixture struct {
	svc   *Service
	prov  *fake.Provider
	src   *testutil.Repo
	ws    *workspace.Manager
	app   *hookApplicator
	watch *logWatch

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

	runner, err := gitx.NewExecRunner(testLog(), gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
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
	f := &serviceFixture{prov: p, src: src, ws: ws, watch: newLogWatch(),
		app: &hookApplicator{delegate: NewCherryPickApplicator(runner, testLog())}}
	f.svc = NewService(Deps{
		Providers: provider.NewStaticResolver(registry), Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: f.app, Runner: runner,
		Log:        slog.New(&watchHandler{inner: testLog().Handler(), watch: f.watch}),
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
	inflight, peak, entered := 0, 0, 0
	// Every build blocks inside Apply on this barrier while holding its
	// semaphore slot, and the barrier is only opened once *all* `builds` are
	// accounted for — either inside Apply, or proven parked on the semaphore
	// (see queuedOnSemaphore). Opening it as soon as `limit` builds overlap
	// would let the rest stream through without ever queuing, which is what
	// made this test pass with the bound removed.
	//
	// So the two outcomes are distinguished by construction, with no timing
	// assumption on either side:
	//   - bound present: exactly `limit` builds are in Apply, the other
	//     builds-limit are parked in StartBuild's semaphore select and stay
	//     parked until we open the barrier; peak == limit.
	//   - bound removed: all `builds` reach Apply at once, nothing is ever
	//     parked, the hook below opens the barrier and peak == builds > limit.
	// A watchdog opens the barrier regardless so any other regression fails as
	// an assertion instead of hanging.
	barrier := make(chan struct{})
	var release sync.Once
	open := func() { release.Do(func() { close(barrier) }) }
	opened := func() bool {
		select {
		case <-barrier:
			return true
		default:
			return false
		}
	}
	// watchdogFired records that the watchdog itself opened the barrier,
	// rather than the deadline loop below observing the expected
	// queued/entered condition or the "all builds entered" hook. If the
	// watchdog fires, the test did not observe what it set out to prove and
	// must fail loudly instead of relying on peak == limit to catch it.
	//
	// Both timers below are driven from barrierWait so their ordering is
	// deterministic: the deadline loop always gives up first and the watchdog
	// is strictly later, a pure hang-guard. When they were both 120s the
	// watchdog (armed earlier) won by construction and the deadline branch was
	// dead code; either branch reports a failure now, but only one of them
	// decides.
	const barrierWait = 120 * time.Second
	var watchdogFired atomic.Bool
	watchdog := time.AfterFunc(barrierWait+30*time.Second, func() {
		watchdogFired.Store(true)
		open()
	})
	defer watchdog.Stop()
	f.app.set(func(context.Context, session.ResolvedChange) {
		mu.Lock()
		inflight++
		entered++
		if inflight > peak {
			peak = inflight
		}
		all := entered >= builds
		mu.Unlock()
		if all {
			// Every build got into Apply simultaneously: the bound is gone.
			// Release so the assertion below reports it instead of hanging.
			open()
		}
		<-barrier
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

	// Hold every slot until all `builds` are accounted for. With the bound in
	// place the queued builds are parked forever (nothing else can release the
	// semaphore), so this loop always terminates on the condition rather than
	// on the deadline; the deadline only exists so a regression is an
	// assertion, not a hang.
	deadline := time.Now().Add(barrierWait)
	for !opened() {
		mu.Lock()
		got := entered
		mu.Unlock()
		if got >= limit && queuedOnSemaphore() >= builds-limit {
			open()
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("timed out: %d builds reached Apply and %d are parked on the semaphore, want %d + %d",
				got, queuedOnSemaphore(), limit, builds-limit)
			open()
			break
		}
		time.Sleep(time.Millisecond)
	}
	// If the watchdog opened the barrier, the loop above never observed the
	// expected "queued on semaphore" condition (or the "all builds entered"
	// hook) within the deadline. That is a real failure and must not be
	// allowed to silently pass through the peak assertions below.
	if watchdogFired.Load() {
		mu.Lock()
		got := entered
		mu.Unlock()
		t.Errorf("watchdog opened the barrier: %d builds reached Apply and %d are parked on the semaphore, want %d + %d",
			got, queuedOnSemaphore(), limit, builds-limit)
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
	if got < limit {
		t.Errorf("peak concurrent builds = %d: fewer than %d builds ever overlapped, so the bound was not exercised", got, limit)
	}
}

// queuedOnSemaphore counts goroutines parked in StartBuild's semaphore
// select — i.e. builds the bound is actively holding back. It is what lets
// the test above prove "these builds are blocked" instead of assuming it
// after a sleep: a goroutine whose select is the one in StartBuild's build
// goroutine can only be waiting for a slot (its other case is ctx.Done, and
// the test's context is never cancelled).
//
// The match is deliberately narrow: the goroutine must be in the "select"
// wait state *and* its topmost frame must be that select. A build goroutine
// that has already acquired its slot has StartBuild.func1 deeper on its
// stack (below Build), so it is never miscounted as queued.
func queuedOnSemaphore() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	return countParkedInSelect(string(buf), semaphoreSelectFrame)
}

// semaphoreSelectFrame is the topmost stack frame of a build goroutine that is
// parked on StartBuild's semaphore select. If StartBuild's goroutine is renamed
// or its select moves, this string stops matching — which is why
// TestCountParkedInSelect pins the parser and
// TestQueuedOnSemaphoreFrameNameIsCurrent pins this string against the live
// runtime.
const semaphoreSelectFrame = "internal/review.(*Service).StartBuild.func1("

// countParkedInSelect is the pure parser behind queuedOnSemaphore. It counts
// goroutines in dump (the output of runtime.Stack(buf, true)) that are both in
// the "select" wait state and whose *topmost* frame contains frame.
//
// It is separated from runtime.Stack so it can be tested deterministically: a
// matcher that silently over- or under-counts would make the concurrency test
// blind, and that failure mode is invisible from a green concurrency run.
func countParkedInSelect(dump, frame string) int {
	count := 0
	for _, g := range strings.Split(dump, "\n\n") {
		lines := strings.Split(g, "\n")
		if len(lines) < 2 || !strings.HasPrefix(lines[0], "goroutine ") {
			continue
		}
		if !strings.Contains(lines[0], "[select") {
			continue
		}
		if strings.Contains(lines[1], frame) {
			count++
		}
	}
	return count
}

// dumpGoroutine renders one goroutine block in runtime.Stack's format: a header
// line carrying the wait state, then frame/file line pairs, topmost frame first.
func dumpGoroutine(id int, state string, frames ...string) string {
	b := fmt.Sprintf("goroutine %d [%s]:\n", id, state)
	for i, fr := range frames {
		b += fr + "\n\t/src/converge/internal/review/service.go:" + fmt.Sprint(100+i) + " +0x1c\n"
	}
	return b
}

// TestCountParkedInSelect pins the goroutine-dump parser that
// queuedOnSemaphore depends on. Without it, breaking the matcher only shows up
// as the concurrency test timing out after two minutes.
func TestCountParkedInSelect(t *testing.T) {
	const frame = semaphoreSelectFrame
	parked := dumpGoroutine(11, "select", // queued: the select frame is topmost
		"github.com/jtumidanski/converge/internal/review.(*Service).StartBuild.func1(0xc000123456)")
	parkedTwo := dumpGoroutine(12, "select",
		"github.com/jtumidanski/converge/internal/review.(*Service).StartBuild.func1(0xc000abcdef)")
	running := dumpGoroutine(13, "select", // holds a slot: StartBuild.func1 is below Build
		"github.com/jtumidanski/converge/internal/review.(*Service).Build(0xc000000001, {0x0, 0x0})",
		"github.com/jtumidanski/converge/internal/review.(*Service).StartBuild.func1(0xc000000002)")
	chanRecv := dumpGoroutine(14, "chan receive", // right frame, wrong wait state
		"github.com/jtumidanski/converge/internal/review.(*Service).StartBuild.func1(0xc000000003)")
	unrelated := dumpGoroutine(15, "select",
		"github.com/jtumidanski/converge/internal/session.(*Store).retryCleanup(0xc000000004)")
	header := "goroutine 1 [running]:\nmain.main()\n\t/src/main.go:1 +0x1\n"

	tests := []struct {
		name  string
		dump  string
		frame string
		want  int
	}{
		{"empty dump", "", frame, 0},
		{"no goroutine matches", header + "\n" + unrelated, frame, 0},
		{"one parked", header + "\n" + parked, frame, 1},
		{"two parked among noise", strings.Join([]string{header, parked, running, unrelated, parkedTwo, chanRecv}, "\n"), frame, 2},
		{"a goroutine holding a slot is not counted", header + "\n" + running, frame, 0},
		{"the frame in a non-select wait state is not counted", header + "\n" + chanRecv, frame, 0},
		// The point of the whole helper: a frame name that no longer matches
		// the runtime's must count zero, so the concurrency test's wait
		// condition can never be satisfied and its watchdog must fire.
		{"a stale frame name counts nothing", strings.Join([]string{header, parked, parkedTwo}, "\n"), frame + "_MISMATCH", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := countParkedInSelect(tc.dump, tc.frame); got != tc.want {
				t.Errorf("countParkedInSelect = %d, want %d\ndump:\n%s", got, tc.want, tc.dump)
			}
		})
	}
}

// TestQueuedOnSemaphoreFrameNameIsCurrent pins semaphoreSelectFrame against the
// live runtime rather than against a hand-written dump: it parks a real build
// on a full semaphore and requires queuedOnSemaphore to see it. A rename of
// StartBuild's goroutine fails here, in under a second, instead of silently
// turning the concurrency test into a two-minute no-op.
func TestQueuedOnSemaphoreFrameNameIsCurrent(t *testing.T) {
	f := newServiceFixtureWith(t, 1)

	release := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	defer open()
	entered := make(chan struct{}, 2)
	f.app.set(func(context.Context, session.ResolvedChange) {
		entered <- struct{}{}
		<-release
	}, nil)

	holder := f.create(t).ID()
	queued := f.create(t).ID()
	f.svc.StartBuild(context.Background(), holder)
	select {
	case <-entered:
	case <-time.After(60 * time.Second):
		t.Fatal("the first build never reached Apply")
	}
	f.svc.StartBuild(context.Background(), queued)

	deadline := time.Now().Add(60 * time.Second)
	for queuedOnSemaphore() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("queuedOnSemaphore never saw the parked build: %q no longer matches StartBuild's goroutine frame", semaphoreSelectFrame)
		}
		time.Sleep(time.Millisecond)
	}

	open()
	for _, id := range []string{holder, queued} {
		if got := f.awaitTerminal(t, id, 120*time.Second); got.Status() != session.StatusReady {
			t.Fatalf("session %s status = %s err = %+v", id, got.Status(), got.Error())
		}
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

// TestServiceProgressDoesNotResurrectADiscardedSession covers the stage
// markers a build writes mid-pipeline. They are written from the build's own
// copy of the session, which goes stale the moment a discard or an expiry
// lands, so they need the same compare-and-swap the terminal write uses: an
// unguarded stage write re-creates the session directory Cleanup has just
// removed and puts a CREATING record back in the index over the FINISHED one.
func TestServiceProgressDoesNotResurrectADiscardedSession(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	s := f.create(t)
	if err := f.svc.Finish(ctx, s.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.ws.SessionDir(s.ID())); !os.IsNotExist(err) {
		t.Fatalf("precondition: the discarded session's directory still exists: %v", err)
	}

	// The build only learns about the discard when the store refuses a write,
	// so it keeps marching through stages on its stale copy.
	got := f.svc.progress(s.WithStage(session.StageDiffing, time.Now()))
	if got.Stage() != session.StageDiffing {
		t.Errorf("progress returned stage %q, want %q: the build's own copy must keep advancing", got.Stage(), session.StageDiffing)
	}
	stored, ok := f.svc.Get(s.ID())
	if !ok || stored.Status() != session.StatusFinished {
		t.Errorf("stored status = %s ok = %v, want FINISHED: the stage write resurrected the session", stored.Status(), ok)
	}
	if _, err := os.Stat(f.ws.SessionDir(s.ID())); !os.IsNotExist(err) {
		t.Errorf("the stage write re-created the session directory of a FINISHED session: %v", err)
	}
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

// TestServiceStartBuildDrainsAQueuedBuildOnShutdown covers StartBuild's
// ctx.Done() branch: with every slot taken and the lifetime context already
// cancelled, the queued build must give up instead of acquiring a slot and
// running a pipeline whose context is dead. The session is deliberately left
// CREATING (LoadAll marks it INTERRUPTED on the next start), so the log record
// is the deterministic signal that the branch ran — no sleeping.
func TestServiceStartBuildDrainsAQueuedBuildOnShutdown(t *testing.T) {
	f := newServiceFixtureWith(t, 1)
	gate := make(chan struct{})
	var gateOnce sync.Once
	openGate := func() { gateOnce.Do(func() { close(gate) }) }
	watchdog := time.AfterFunc(60*time.Second, openGate)
	defer watchdog.Stop()
	entered := make(chan struct{}, 1)
	f.app.set(func(context.Context, session.ResolvedChange) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-gate
	}, nil)

	held := f.create(t)
	queued := f.create(t)
	f.svc.StartBuild(context.Background(), held.ID())
	select {
	case <-entered:
	case <-time.After(60 * time.Second):
		t.Fatal("the first build never reached Apply, so the only slot was never taken")
	}

	drained := f.watch.wait("build not started; shutting down")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the server is shutting down
	f.svc.StartBuild(ctx, queued.ID())
	select {
	case <-drained:
	case <-time.After(60 * time.Second):
		t.Fatal("the queued build never drained; it must not wait for a slot on a dead context")
	}
	if got, _ := f.svc.Get(queued.ID()); got.Status() != session.StatusCreating {
		t.Errorf("drained session status = %s, want CREATING so LoadAll can mark it INTERRUPTED", got.Status())
	}

	openGate()
	if got := f.awaitTerminal(t, held.ID(), 60*time.Second); got.Status() != session.StatusReady {
		t.Fatalf("the running build status = %s err = %+v", got.Status(), got.Error())
	}
}

// TestServiceBuildTimeoutIsNotReportedAsARestart pins R31: the build ceiling
// and a shutdown share the INTERRUPTED code (the code set is fixed) but must
// not share the message, which would tell the operator a timed-out build was
// "interrupted by a server restart".
func TestServiceBuildTimeoutIsNotReportedAsARestart(t *testing.T) {
	f := newServiceFixture(t)
	s := f.create(t)
	// The build ceiling firing: the pipeline's context is past its deadline,
	// so whatever the dying git process reports, the outcome is the timeout.
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	f.app.set(nil, func(actx context.Context, _ session.ResolvedChange) (ApplyResult, error) {
		return ApplyResult{}, fmt.Errorf("cherry-pick: %w", actx.Err())
	})
	done := f.svc.Build(expired, s.ID())
	stored, _ := f.svc.Get(s.ID())
	for _, got := range []session.Session{done, stored} {
		if got.Status() != session.StatusFailed {
			t.Fatalf("status = %s, want FAILED", got.Status())
		}
		if got.Error() == nil || got.Error().Code != session.CodeInterrupted {
			t.Fatalf("error = %+v, want code INTERRUPTED", got.Error())
		}
		if got.Error().Message != MsgBuildTimedOut() {
			t.Errorf("message = %q, want the build-timeout message %q", got.Error().Message, MsgBuildTimedOut())
		}
		if got.Error().Message == MsgInterrupted() {
			t.Error("a timed-out build must not claim it was interrupted by a server restart")
		}
	}
}

// TestServiceClassifySeparatesTimeoutFromCancellation covers both shapes each
// cause arrives in: the context's own state and the error chain.
func TestServiceClassifySeparatesTimeoutFromCancellation(t *testing.T) {
	f := newServiceFixture(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want string
	}{
		{"cancelled context", cancelled, errors.New("signal: killed"), MsgInterrupted()},
		{"cancelled in the error chain", context.Background(), context.Canceled, MsgInterrupted()},
		{"expired context", expired, errors.New("signal: killed"), MsgBuildTimedOut()},
		{"deadline in the error chain", context.Background(), context.DeadlineExceeded, MsgBuildTimedOut()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := f.svc.classify(tt.ctx, "0123abcd", tt.err)
			if re.Code != session.CodeInterrupted {
				t.Fatalf("code = %s, want INTERRUPTED", re.Code)
			}
			if re.Message != tt.want {
				t.Errorf("message = %q, want %q", re.Message, tt.want)
			}
		})
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
