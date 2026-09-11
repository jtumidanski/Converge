package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/identity"
)

type fakeCleaner struct {
	mu      sync.Mutex
	cleaned []string
	removed []string
	fail    error
}

func (f *fakeCleaner) Cleanup(_ context.Context, s Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleaned = append(f.cleaned, s.ID())
	if f.fail != nil {
		return f.fail
	}
	return nil
}

func (f *fakeCleaner) RemoveDir(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	return nil
}

func newStore(t *testing.T, now *time.Time) (*Store, *fakeCleaner) {
	t.Helper()
	fc := &fakeCleaner{}
	st := NewStore(t.TempDir(), 24*time.Hour, fc, slog.New(slog.NewTextHandler(os.Stderr, nil)), func() time.Time { return *now })
	return st, fc
}

func TestSaveGetListAtomic(t *testing.T) {
	now := t0
	st, _ := newStore(t, &now)
	s := newSession(t)
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(st.Dir("0123abcd"), "session.json")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(st.Dir("0123abcd"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatal("temp file left behind")
		}
	}
	got, ok := st.Get("0123abcd", identity.Standalone())
	if !ok || got.Repository() != "atlas/server" {
		t.Fatal("Get")
	}
	if _, ok := st.Get("ffffffff", identity.Standalone()); ok {
		t.Fatal("unknown found")
	}
	other, _ := NewBuilder().SetID("aaaaaaaa").SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(t0.Add(time.Hour)).SetTTL(time.Hour).Build()
	_ = st.Save(other)
	list := st.List(identity.Standalone())
	if len(list) != 2 || list[0].ID() != "aaaaaaaa" {
		t.Fatalf("List = %v", list)
	}
}

func TestFinishIsIdempotentAndCleans(t *testing.T) {
	now := t0
	st, fc := newStore(t, &now)
	_ = st.Save(newSession(t))
	ctx := context.Background()
	if err := st.Finish(ctx, "0123abcd", identity.Standalone()); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get("0123abcd", identity.Standalone()); got.Status() != StatusFinished {
		t.Fatalf("status = %s", got.Status())
	}
	if err := st.Finish(ctx, "0123abcd", identity.Standalone()); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(ctx, "ffffffff", identity.Standalone()); err != nil {
		t.Fatal("unknown must be a no-op")
	}
	if len(fc.cleaned) != 1 {
		t.Fatalf("cleanups = %v", fc.cleaned)
	}
	if len(st.List(identity.Standalone())) != 0 {
		t.Fatal("finished session still listed")
	}
	fc.fail = errors.New("boom")
	_ = st.Save(newSession(t))
	if err := st.Finish(ctx, "0123abcd", identity.Standalone()); err == nil {
		t.Fatal("cleanup failure must surface")
	}
}

func TestLoadAllRecovery(t *testing.T) {
	now := t0.Add(48 * time.Hour)
	st, fc := newStore(t, &now)
	// creating → interrupted
	creating := newSession(t) // created t0, expires t0+24h → also expired at now; recovery marks INTERRUPTED first, then expiry sweep cleans it
	writeRecord(t, st, creating)
	fresh, _ := NewBuilder().SetID("bbbbbbbb").SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(now.Add(-time.Hour)).SetTTL(24 * time.Hour).Build()
	writeRecord(t, st, fresh)
	// invalid directory older than TTL
	old := filepath.Join(st.Root(), "cccccccc")
	_ = os.MkdirAll(old, 0o755)
	oldTime := now.Add(-48 * time.Hour)
	_ = os.Chtimes(old, oldTime, oldTime)
	// invalid directory newer than TTL is left alone
	_ = os.MkdirAll(filepath.Join(st.Root(), "dddddddd"), 0o755)
	// junk file
	_ = os.WriteFile(filepath.Join(st.Root(), "junk.txt"), []byte("x"), 0o644)

	if err := st.LoadAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := st.Get("bbbbbbbb", identity.Standalone())
	if b.Status() != StatusFailed || b.Error() == nil || b.Error().Code != CodeInterrupted {
		t.Errorf("fresh creating session: %+v", b)
	}
	a, _ := st.Get("0123abcd", identity.Standalone())
	if a.Status() != StatusExpired {
		t.Errorf("old session status = %s", a.Status())
	}
	if len(fc.cleaned) != 1 || fc.cleaned[0] != "0123abcd" {
		t.Errorf("cleaned = %v", fc.cleaned)
	}
	if len(fc.removed) != 1 || fc.removed[0] != "cccccccc" {
		t.Errorf("removed = %v", fc.removed)
	}
	// on-disk record for bbbbbbbb was rewritten
	raw, _ := os.ReadFile(filepath.Join(st.Dir("bbbbbbbb"), "session.json"))
	if !strings.Contains(string(raw), `"INTERRUPTED"`) {
		t.Errorf("record not rewritten: %s", raw)
	}
}

func TestSweepExpires(t *testing.T) {
	now := t0
	st, fc := newStore(t, &now)
	s := newSession(t)
	s, _ = s.WithBase(strings.Repeat("b", 40), t0)
	_ = st.Save(s)
	st.Sweep(context.Background())
	if len(fc.cleaned) != 0 {
		t.Fatal("swept too early")
	}
	now = t0.Add(25 * time.Hour)
	st.Sweep(context.Background())
	if got, _ := st.Get("0123abcd", identity.Standalone()); got.Status() != StatusExpired || len(fc.cleaned) != 1 {
		t.Fatalf("not expired: %s %v", got.Status(), fc.cleaned)
	}
	st.Sweep(context.Background())
	if len(fc.cleaned) != 1 {
		t.Fatal("expired session cleaned twice")
	}
}

// TestStoreConcurrentAccess genuinely contends on the Store: multiple
// goroutines hammer Save/Get/List for distinct sessions while a separate
// goroutine repeatedly Sweeps, all racing on the same *Store under -race.
// A clean -race run here actually means something, unlike the sequential
// tests above (see finding 4: Task 5 shipped a real data race that a
// sequential-only -race run did not catch).
func TestStoreConcurrentAccess(t *testing.T) {
	now := t0
	var nowMu sync.Mutex
	st, _ := newStore(t, &now)
	// newStore captures `now` by pointer via a closure reading *now without a
	// lock; give Sweep's reads of "now" the same protection the writer below
	// uses so -race has nothing legitimate to report on the test's own state.
	st.now = func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}

	const workers = 8
	const perWorker = 25
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("%08x", worker*perWorker+i)
				s, err := NewBuilder().SetID(id).SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").
					SetRequestedChanges([]int{1}).SetCreatedAt(t0).SetTTL(time.Hour).Build()
				if err != nil {
					t.Errorf("build %s: %v", id, err)
					return
				}
				if err := st.Save(s); err != nil {
					t.Errorf("save %s: %v", id, err)
					return
				}
				if _, ok := st.Get(id, identity.Standalone()); !ok {
					t.Errorf("get %s: not found immediately after save", id)
				}
				_ = st.List(identity.Standalone())
				_ = st.Corrupted(id)
			}
		}(w)
	}

	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		for i := 0; i < 100; i++ {
			st.Sweep(context.Background())
		}
	}()

	wg.Wait()
	nowMu.Lock()
	now = now.Add(2 * time.Hour) // past every session's 1h TTL
	nowMu.Unlock()
	st.Sweep(context.Background()) // final sweep, expires everything
	<-sweepDone

	if len(st.List(identity.Standalone())) != 0 {
		t.Fatalf("expected every session expired after the final sweep, got %d active", len(st.List(identity.Standalone())))
	}
}

// TestRunSweeperSweepsOnIntervalAndStopsOnCancel drives RunSweeper
// deterministically: a short interval plus a channel-backed Cleaner lets the
// test wait for the actual sweep to happen instead of guessing a sleep
// duration, and a cancelled context proves the loop actually exits.
func TestRunSweeperSweepsOnIntervalAndStopsOnCancel(t *testing.T) {
	now := t0.Add(2 * time.Hour) // already past the session's TTL below
	swept := make(chan string, 1)
	cleaner := &signalingCleaner{cleaned: swept}
	st := NewStore(t.TempDir(), time.Hour, cleaner, slog.New(slog.NewTextHandler(os.Stderr, nil)), func() time.Time { return now })

	s, err := NewBuilder().SetID("0123abcd").SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").
		SetRequestedChanges([]int{1}).SetCreatedAt(t0).SetTTL(time.Hour).Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		st.RunSweeper(ctx, 5*time.Millisecond)
	}()

	select {
	case id := <-swept:
		if id != "0123abcd" {
			t.Fatalf("swept unexpected id %q", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunSweeper did not sweep the expired session in time")
	}

	if got, ok := st.Get("0123abcd", identity.Standalone()); !ok || got.Status() != StatusExpired {
		t.Fatalf("session not expired after sweep: %+v ok=%v", got, ok)
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("RunSweeper did not stop after context cancellation")
	}
}

// signalingCleaner reports each Cleanup call on a channel so tests can wait
// deterministically for a sweep to actually happen, instead of sleeping.
type signalingCleaner struct {
	cleaned chan string
}

func (c *signalingCleaner) Cleanup(_ context.Context, s Session) error {
	select {
	case c.cleaned <- s.ID():
	default:
	}
	return nil
}

func (c *signalingCleaner) RemoveDir(_ context.Context, _ string) error { return nil }

// TestGetVsCorrupted proves finding 5's fix: after LoadAll encounters a
// corrupt session.json for an id that is younger than the store's TTL (so
// it is deliberately left on disk rather than removed), Get(id) reports
// false exactly as it would for an id that never existed - but
// Corrupted(id) distinguishes the two. Once that id is healthily rewritten
// via Save, Corrupted(id) reverts to false.
func TestGetVsCorrupted(t *testing.T) {
	now := t0
	st, _ := newStore(t, &now)

	corruptID := "eeeeeeee"
	dir := filepath.Join(st.Root(), corruptID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := st.LoadAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, ok := st.Get(corruptID, identity.Standalone()); ok {
		t.Fatal("corrupt id should not be in the live index")
	}
	if !st.Corrupted(corruptID) {
		t.Fatal("Corrupted should report true for a known-corrupt id")
	}
	if st.Corrupted("ffffffff") {
		t.Fatal("Corrupted should report false for an id that was never seen at all")
	}

	// A never-existed id and a corrupt id both fail Get identically...
	_, neverOK := st.Get("ffffffff", identity.Standalone())
	_, corruptOK := st.Get(corruptID, identity.Standalone())
	if neverOK != corruptOK {
		t.Fatal("Get should not distinguish the two by itself (that's what Corrupted is for)")
	}

	// ...but once the id is healthily rewritten, it's no longer corrupt.
	healed, err := NewBuilder().SetID(corruptID).SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").
		SetRequestedChanges([]int{1}).SetCreatedAt(t0).SetTTL(time.Hour).Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(healed); err != nil {
		t.Fatal(err)
	}
	if st.Corrupted(corruptID) {
		t.Fatal("Corrupted should clear once the id is saved successfully")
	}
	if _, ok := st.Get(corruptID, identity.Standalone()); !ok {
		t.Fatal("healed id should now be found")
	}
}

// retryCleaner mirrors the real Cleaner's contract (Cleanup removes the
// session's directory) so tests can observe the directory actually
// disappearing once a retried Cleanup succeeds, unlike fakeCleaner which
// never touches the filesystem. failIDs controls which ids currently fail;
// flip an entry to false to simulate a transient failure clearing up.
type retryCleaner struct {
	mu      sync.Mutex
	root    string
	failIDs map[string]bool
	cleaned []string
}

func (c *retryCleaner) Cleanup(_ context.Context, s Session) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleaned = append(c.cleaned, s.ID())
	if c.failIDs[s.ID()] {
		return errors.New("boom")
	}
	return os.RemoveAll(filepath.Join(c.root, s.ID()))
}

func (c *retryCleaner) RemoveDir(_ context.Context, id string) error {
	return os.RemoveAll(filepath.Join(c.root, id))
}

// TestFinishCleanupFailurePersistsTerminalStatus proves the failed-cleanup
// gap identified in round 3: previously, a Cleanup failure left the terminal
// status durable only in memory, so a restart before the workspace was ever
// cleaned would reload the stale, non-terminal on-disk record (PRD FR-8.7
// violation). Now the terminal record is written to the surviving
// session.json via the existing atomic Save path.
func TestFinishCleanupFailurePersistsTerminalStatus(t *testing.T) {
	now := t0
	st, fc := newStore(t, &now)
	_ = st.Save(newSession(t))
	fc.fail = errors.New("boom")

	if err := st.Finish(context.Background(), "0123abcd", identity.Standalone()); err == nil {
		t.Fatal("expected cleanup failure to surface")
	}

	raw, err := os.ReadFile(filepath.Join(st.Dir("0123abcd"), "session.json"))
	if err != nil {
		t.Fatalf("session.json missing after failed cleanup: %v", err)
	}
	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusFinished {
		t.Fatalf("on-disk status = %s, want FINISHED", rec.Status)
	}
}

// TestExpireCleanupFailurePersistsTerminalStatus is the same proof as above,
// via the expire/Sweep path instead of Finish.
func TestExpireCleanupFailurePersistsTerminalStatus(t *testing.T) {
	now := t0
	st, fc := newStore(t, &now)
	_ = st.Save(newSession(t))
	fc.fail = errors.New("boom")

	now = t0.Add(25 * time.Hour) // past the session's TTL
	st.Sweep(context.Background())

	raw, err := os.ReadFile(filepath.Join(st.Dir("0123abcd"), "session.json"))
	if err != nil {
		t.Fatalf("session.json missing after failed cleanup: %v", err)
	}
	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusExpired {
		t.Fatalf("on-disk status = %s, want EXPIRED", rec.Status)
	}
}

// TestSweepRetriesFailedCleanupUntilSuccess proves the leak from finding A is
// closed: a session whose Cleanup failed is retried on a later Sweep, and
// once the underlying problem clears, the workspace is actually removed.
// Throughout, the session must never become visible as active again.
func TestSweepRetriesFailedCleanupUntilSuccess(t *testing.T) {
	now := t0
	root := t.TempDir()
	rc := &retryCleaner{root: root, failIDs: map[string]bool{"0123abcd": true}}
	st := NewStore(root, 24*time.Hour, rc, slog.New(slog.NewTextHandler(os.Stderr, nil)), func() time.Time { return now })
	_ = st.Save(newSession(t))
	ctx := context.Background()

	if err := st.Finish(ctx, "0123abcd", identity.Standalone()); err == nil {
		t.Fatal("expected first cleanup attempt to fail")
	}
	if got, ok := st.Get("0123abcd", identity.Standalone()); !ok || got.Status() != StatusFinished {
		t.Fatalf("status not durable after failed cleanup: %+v ok=%v", got, ok)
	}
	if len(st.List(identity.Standalone())) != 0 {
		t.Fatal("terminal session must never be visible as active")
	}
	if _, err := os.Stat(st.Dir("0123abcd")); err != nil {
		t.Fatal("directory should still exist after a failed cleanup")
	}

	// The transient failure clears; the next Sweep should retry and succeed.
	rc.mu.Lock()
	rc.failIDs["0123abcd"] = false
	rc.mu.Unlock()
	st.Sweep(ctx)

	if _, err := os.Stat(st.Dir("0123abcd")); !os.IsNotExist(err) {
		t.Fatalf("directory should be gone after the retry succeeds, stat err=%v", err)
	}
	if got, ok := st.Get("0123abcd", identity.Standalone()); !ok || got.Status() != StatusFinished {
		t.Fatalf("status changed across retry: %+v ok=%v", got, ok)
	}
	if len(st.List(identity.Standalone())) != 0 {
		t.Fatal("terminal session must never be visible as active")
	}
	st.mu.RLock()
	_, pending := st.pendingCleanup["0123abcd"]
	st.mu.RUnlock()
	if pending {
		t.Fatal("id should be dropped from pendingCleanup once the retry succeeds")
	}
}

// TestSweepRetryCleanupIsBounded proves a permanently failing Cleanup cannot
// make Sweep spin forever or make pendingCleanup grow without bound: after
// maxCleanupRetries is reached, the id is dropped and further Sweep calls
// stop touching it, while its terminal status remains durable and it never
// becomes visible as active.
func TestSweepRetryCleanupIsBounded(t *testing.T) {
	now := t0
	root := t.TempDir()
	rc := &retryCleaner{root: root, failIDs: map[string]bool{"0123abcd": true}} // never clears
	st := NewStore(root, 24*time.Hour, rc, slog.New(slog.NewTextHandler(os.Stderr, nil)), func() time.Time { return now })
	_ = st.Save(newSession(t))
	ctx := context.Background()

	if err := st.Finish(ctx, "0123abcd", identity.Standalone()); err == nil {
		t.Fatal("expected cleanup to fail")
	}

	for i := 0; i < 20; i++ {
		st.Sweep(ctx)
	}

	rc.mu.Lock()
	calls := len(rc.cleaned)
	rc.mu.Unlock()
	wantCalls := 1 + maxCleanupRetries // the initial attempt plus bounded retries
	if calls != wantCalls {
		t.Fatalf("cleanup called %d times across 20 sweeps, want exactly %d (bounded retries)", calls, wantCalls)
	}

	st.mu.RLock()
	_, stillPending := st.pendingCleanup["0123abcd"]
	pendingSize := len(st.pendingCleanup)
	st.mu.RUnlock()
	if stillPending {
		t.Fatal("id should have been dropped from pendingCleanup once retries were exhausted")
	}
	if pendingSize != 0 {
		t.Fatalf("pendingCleanup must not accumulate state, got %d entries", pendingSize)
	}

	if got, ok := st.Get("0123abcd", identity.Standalone()); !ok || got.Status() != StatusFinished {
		t.Fatalf("terminal status must remain durable even after giving up: %+v ok=%v", got, ok)
	}
	if len(st.List(identity.Standalone())) != 0 {
		t.Fatal("terminal session must never be visible as active, even after giving up on retries")
	}
}

func writeRecord(t *testing.T, st *Store, s Session) {
	t.Helper()
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	delete(st.index, s.ID()) // simulate a fresh process
	st.mu.Unlock()
}

// dirCleaner is a Cleaner that actually removes the session directory, the
// way the real cleaner does. The fake one only records the call, which cannot
// show a resurrected directory.
type dirCleaner struct{ root string }

func (c *dirCleaner) Cleanup(_ context.Context, s Session) error {
	return os.RemoveAll(filepath.Join(c.root, s.ID()))
}

func (c *dirCleaner) RemoveDir(_ context.Context, id string) error {
	return os.RemoveAll(filepath.Join(c.root, id))
}

// TestSaveActiveGuardsTerminalTransitions pins the compare-and-swap contract:
// it writes only for an active session, refuses a terminal one (returning the
// stored session so the caller reports the truth), and refuses an id that is
// not in the index at all rather than re-creating a record just proven absent.
func TestSaveActiveGuardsTerminalTransitions(t *testing.T) {
	now := t0
	st, _ := newStore(t, &now)
	s := newSession(t)
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}

	staged := s.WithStage("applying:1", t0.Add(time.Minute))
	got, err := st.SaveActive(staged)
	if err != nil {
		t.Fatalf("SaveActive on an active session: %v", err)
	}
	if got.Stage() != "applying:1" {
		t.Fatalf("returned stage = %q", got.Stage())
	}
	if indexed, _ := st.Get(s.ID(), identity.Standalone()); indexed.Stage() != "applying:1" {
		t.Fatalf("indexed stage = %q, want the written one", indexed.Stage())
	}
	if stage := readStage(t, st, s.ID()); stage != "applying:1" {
		t.Fatalf("on-disk stage = %q, want the written one", stage)
	}

	absent, err := NewBuilder().SetID("aaaaaaaa").SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").
		SetRequestedChanges([]int{1}).SetCreatedAt(t0).SetTTL(time.Hour).Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveActive(absent); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SaveActive on an unknown id = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(st.Dir("aaaaaaaa")); !os.IsNotExist(err) {
		t.Errorf("SaveActive created a directory for an id that is not in the index: %v", err)
	}

	if err := st.Finish(context.Background(), s.ID(), identity.Standalone()); err != nil {
		t.Fatal(err)
	}
	stored, err := st.SaveActive(staged.WithStage("diffing", t0.Add(2*time.Minute)))
	if !errors.Is(err, ErrTerminal) {
		t.Fatalf("SaveActive on a FINISHED session = %v, want ErrTerminal", err)
	}
	if stored.Status() != StatusFinished {
		t.Fatalf("returned session status = %s, want the stored FINISHED", stored.Status())
	}
	if indexed, _ := st.Get(s.ID(), identity.Standalone()); indexed.Status() != StatusFinished {
		t.Fatalf("indexed status = %s, want FINISHED", indexed.Status())
	}
	if stage := readStage(t, st, s.ID()); stage == "diffing" {
		t.Error("SaveActive wrote a refused transition to disk")
	}
}

func readStage(t *testing.T, st *Store, id string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(st.Dir(id), recordFile))
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if rec.Stage == nil {
		return ""
	}
	return *rec.Stage
}

// TestSaveActiveIsAtomicWithConcurrentFinish is the C1 regression. The
// invariant is unconditional: whenever the store reports a session as
// FINISHED, its directory must be gone. A check-then-write with any gap
// between the two lets a Finish land in that gap, so the write re-creates the
// directory Cleanup had just removed and the review reports a live status
// against a deleted worktree.
//
// The mechanism that closes the gap is that the status check and the record
// write happen under a single acquisition of the store's lock, so that is
// what is asserted — from inside the write itself, via onWriteRecord, rather
// than by trying to land a racing Finish in a sub-millisecond fsync window
// with a sleep. A racing Finish is started from inside that same window as
// well, so the end-to-end invariant is exercised too; but the discriminating
// assertion is the lock one, which holds on every run and every machine.
func TestSaveActiveIsAtomicWithConcurrentFinish(t *testing.T) {
	root := t.TempDir()
	now := t0
	st := NewStore(root, 24*time.Hour, &dirCleaner{root: root},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		func() time.Time { return now })
	s := newSession(t)
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	staged := s.WithStage("diffing", t0.Add(time.Minute))

	var (
		wg          sync.WaitGroup
		lockWasFree atomic.Bool
		finishErr   error
	)
	st.onWriteRecord = func(id string) {
		if id != s.ID() {
			return
		}
		st.onWriteRecord = nil // this must fire for the guarded write only
		// The write must run with the store's write lock held: that is the
		// only thing preventing a Finish from landing between the status
		// check and the write. If the lock is free here, the gap is open.
		if st.mu.TryLock() {
			st.mu.Unlock()
			lockWasFree.Store(true)
		}
		// Aim a real Finish at this exact window as well.
		wg.Add(1)
		go func() {
			defer wg.Done()
			finishErr = st.Finish(context.Background(), s.ID(), identity.Standalone())
		}()
	}
	_, saveErr := st.SaveActive(staged)
	wg.Wait()

	if lockWasFree.Load() {
		t.Error("SaveActive wrote session.json without holding the store lock: a concurrent Finish can land between the status check and the write")
	}
	if finishErr != nil {
		t.Fatalf("finish: %v", finishErr)
	}
	if saveErr != nil && !errors.Is(saveErr, ErrTerminal) {
		t.Fatalf("SaveActive = %v, want nil or ErrTerminal", saveErr)
	}
	indexed, ok := st.Get(s.ID(), identity.Standalone())
	if !ok || indexed.Status() != StatusFinished {
		t.Fatalf("indexed = %+v ok=%v, want FINISHED", indexed.Status(), ok)
	}
	if _, err := os.Stat(st.Dir(s.ID())); !os.IsNotExist(err) {
		t.Fatalf("the session directory of a FINISHED session survives (%v): the write landed after Cleanup and resurrected it", err)
	}
}

// TestSaveActiveRacesFinish is the same invariant driven concurrently rather
// than through the injection point, so the two mutators are exercised
// against each other as they actually run.
func TestSaveActiveRacesFinish(t *testing.T) {
	root := t.TempDir()
	now := t0
	st := NewStore(root, 24*time.Hour, &dirCleaner{root: root},
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		func() time.Time { return now })
	s := newSession(t)
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	staged := s.WithStage("diffing", t0.Add(time.Minute))

	var wg sync.WaitGroup
	wg.Add(2)
	var saveErr, finishErr error
	go func() {
		defer wg.Done()
		_, saveErr = st.SaveActive(staged)
	}()
	go func() {
		defer wg.Done()
		// A small offset so the Finish aims at the middle of the save rather
		// than racing its start; the invariant below must hold for every
		// possible interleaving, this one just makes the interesting one likely.
		time.Sleep(2 * time.Millisecond)
		finishErr = st.Finish(context.Background(), s.ID(), identity.Standalone())
	}()
	wg.Wait()

	if finishErr != nil {
		t.Fatalf("finish: %v", finishErr)
	}
	if saveErr != nil && !errors.Is(saveErr, ErrTerminal) {
		t.Fatalf("SaveActive = %v, want nil or ErrTerminal", saveErr)
	}
	indexed, ok := st.Get(s.ID(), identity.Standalone())
	if !ok || indexed.Status() != StatusFinished {
		t.Fatalf("indexed = %+v ok=%v, want FINISHED", indexed.Status(), ok)
	}
	if _, err := os.Stat(st.Dir(s.ID())); !os.IsNotExist(err) {
		t.Fatalf("the session directory of a FINISHED session survives (%v): the write landed after Cleanup and resurrected it", err)
	}
}

// newOwnedSession builds a valid CREATING session for id, owned by owner (an
// empty owner is the valid standalone value).
func newOwnedSession(t *testing.T, id, owner string, createdAt time.Time) Session {
	t.Helper()
	s, err := NewBuilder().SetID(id).SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").
		SetRequestedChanges([]int{1}).SetCreatedAt(createdAt).SetTTL(24 * time.Hour).SetOwner(owner).Build()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestListIsScopedByOwner is the FR-6.3/FR-6.4 contract on List: standalone
// sees everything including unowned records, while a scoped user sees only
// their own and never an unowned one.
func TestListIsScopedByOwner(t *testing.T) {
	now := t0
	st, _ := newStore(t, &now)
	unowned := newOwnedSession(t, "00000001", "", t0)
	a1 := newOwnedSession(t, "0000000a", "userA", t0.Add(time.Minute))
	a2 := newOwnedSession(t, "0000000b", "userA", t0.Add(2*time.Minute))
	b1 := newOwnedSession(t, "0000000c", "userB", t0.Add(3*time.Minute))
	for _, s := range []Session{unowned, a1, a2, b1} {
		if err := st.Save(s); err != nil {
			t.Fatal(err)
		}
	}

	all := st.List(identity.Standalone())
	if len(all) != 4 {
		t.Fatalf("Standalone List = %d, want 4", len(all))
	}

	aList := st.List(identity.ForUser("userA"))
	if len(aList) != 2 {
		t.Fatalf("userA List = %d, want 2", len(aList))
	}
	for _, s := range aList {
		if s.Owner() != "userA" {
			t.Errorf("userA list contains %s owned by %q", s.ID(), s.Owner())
		}
		if s.ID() == unowned.ID() {
			t.Fatal("unowned session visible to userA")
		}
	}

	bList := st.List(identity.ForUser("userB"))
	if len(bList) != 1 || bList[0].Owner() != "userB" {
		t.Fatalf("userB List = %v", bList)
	}
	if bList[0].ID() == unowned.ID() {
		t.Fatal("unowned session visible to userB")
	}
}

// TestGetIsScopedByOwner is the FR-4.2 contract on Get: a session owned by
// another user or unowned in hosted mode is reported exactly as absent.
func TestGetIsScopedByOwner(t *testing.T) {
	now := t0
	st, _ := newStore(t, &now)
	a := newOwnedSession(t, "0000000a", "userA", t0)
	unowned := newOwnedSession(t, "00000001", "", t0)
	if err := st.Save(a); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(unowned); err != nil {
		t.Fatal(err)
	}

	if _, ok := st.Get(a.ID(), identity.ForUser("userB")); ok {
		t.Fatal("userB should not see userA's session")
	}
	if _, ok := st.Get(a.ID(), identity.ForUser("userA")); !ok {
		t.Fatal("userA should see their own session")
	}
	if _, ok := st.Get(a.ID(), identity.Standalone()); !ok {
		t.Fatal("standalone should see an owned session")
	}
	if _, ok := st.Get(unowned.ID(), identity.ForUser("userA")); ok {
		t.Fatal("userA should not see an unowned session")
	}
}

// TestFinishIsScopedByOwner proves Finish delegates its visibility decision
// to Get: a cross-user Finish is a silent no-op that leaves the session
// active and its workspace untouched.
func TestFinishIsScopedByOwner(t *testing.T) {
	now := t0
	st, fc := newStore(t, &now)
	a := newOwnedSession(t, "0000000a", "userA", t0)
	if err := st.Save(a); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := st.Finish(ctx, a.ID(), identity.ForUser("userB")); err != nil {
		t.Fatalf("cross-user finish should be a no-op, got %v", err)
	}
	if got, ok := st.Get(a.ID(), identity.ForUser("userA")); !ok || got.Status() != StatusCreating {
		t.Fatalf("session should still be active: %+v ok=%v", got, ok)
	}
	if len(fc.cleaned) != 0 {
		t.Fatalf("cleanup ran for a cross-user finish: %v", fc.cleaned)
	}
	if _, err := os.Stat(st.Dir(a.ID())); err != nil {
		t.Fatal("workspace should be intact after a cross-user finish")
	}

	if err := st.Finish(ctx, a.ID(), identity.ForUser("userA")); err != nil {
		t.Fatalf("own finish: %v", err)
	}
	if got, ok := st.Get(a.ID(), identity.Standalone()); !ok || got.Status() != StatusFinished {
		t.Fatalf("session should be finished: %+v ok=%v", got, ok)
	}
}

// TestUnownedCount proves Unowned counts exactly the indexed sessions that
// carry no owner.
func TestUnownedCount(t *testing.T) {
	now := t0
	st, _ := newStore(t, &now)
	u1 := newOwnedSession(t, "00000001", "", t0)
	u2 := newOwnedSession(t, "00000002", "", t0)
	a := newOwnedSession(t, "0000000a", "userA", t0)
	for _, s := range []Session{u1, u2, a} {
		if err := st.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	if got := st.Unowned(); got != 2 {
		t.Fatalf("Unowned = %d, want 2", got)
	}
}

// TestPurgeRemovesEveryOwnedSession is the FR-2.7 account-deletion contract:
// every session owned by the purged user is removed, active or terminal,
// along with its workspace, while another user's session is untouched.
func TestPurgeRemovesEveryOwnedSession(t *testing.T) {
	now := t0
	root := t.TempDir()
	cleaner := &dirCleaner{root: root}
	st := NewStore(root, 24*time.Hour, cleaner, slog.New(slog.NewTextHandler(os.Stderr, nil)), func() time.Time { return now })

	active := newOwnedSession(t, "0000000a", "userA", t0)
	if err := st.Save(active); err != nil {
		t.Fatal(err)
	}
	finished := newOwnedSession(t, "0000000b", "userA", t0)
	finished = finished.Finished(now)
	if err := st.Save(finished); err != nil {
		t.Fatal(err)
	}
	b := newOwnedSession(t, "0000000c", "userB", t0)
	if err := st.Save(b); err != nil {
		t.Fatal(err)
	}

	if err := st.Purge(context.Background(), "userA"); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if _, err := os.Stat(st.Dir(active.ID())); !os.IsNotExist(err) {
		t.Errorf("active A directory survives: %v", err)
	}
	if _, err := os.Stat(st.Dir(finished.ID())); !os.IsNotExist(err) {
		t.Errorf("finished A directory survives: %v", err)
	}
	for _, s := range st.List(identity.Standalone()) {
		if s.Owner() == "userA" {
			t.Errorf("purged owner still listed: %s", s.ID())
		}
	}
	if _, ok := st.Get(b.ID(), identity.Standalone()); !ok {
		t.Fatal("userB's session should survive purge")
	}
	if _, err := os.Stat(st.Dir(b.ID())); err != nil {
		t.Errorf("userB's directory should survive purge: %v", err)
	}
}
