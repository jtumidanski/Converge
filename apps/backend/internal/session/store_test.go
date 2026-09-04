package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	got, ok := st.Get("0123abcd")
	if !ok || got.Repository() != "atlas/server" {
		t.Fatal("Get")
	}
	if _, ok := st.Get("ffffffff"); ok {
		t.Fatal("unknown found")
	}
	other, _ := NewBuilder().SetID("aaaaaaaa").SetProviderID("gh").SetRepository("a/b").SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(t0.Add(time.Hour)).SetTTL(time.Hour).Build()
	_ = st.Save(other)
	list := st.List()
	if len(list) != 2 || list[0].ID() != "aaaaaaaa" {
		t.Fatalf("List = %v", list)
	}
}

func TestFinishIsIdempotentAndCleans(t *testing.T) {
	now := t0
	st, fc := newStore(t, &now)
	_ = st.Save(newSession(t))
	ctx := context.Background()
	if err := st.Finish(ctx, "0123abcd"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get("0123abcd"); got.Status() != StatusFinished {
		t.Fatalf("status = %s", got.Status())
	}
	if err := st.Finish(ctx, "0123abcd"); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(ctx, "ffffffff"); err != nil {
		t.Fatal("unknown must be a no-op")
	}
	if len(fc.cleaned) != 1 {
		t.Fatalf("cleanups = %v", fc.cleaned)
	}
	if len(st.List()) != 0 {
		t.Fatal("finished session still listed")
	}
	fc.fail = errors.New("boom")
	_ = st.Save(newSession(t))
	if err := st.Finish(ctx, "0123abcd"); err == nil {
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
	b, _ := st.Get("bbbbbbbb")
	if b.Status() != StatusFailed || b.Error() == nil || b.Error().Code != CodeInterrupted {
		t.Errorf("fresh creating session: %+v", b)
	}
	a, _ := st.Get("0123abcd")
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
	if got, _ := st.Get("0123abcd"); got.Status() != StatusExpired || len(fc.cleaned) != 1 {
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
				if _, ok := st.Get(id); !ok {
					t.Errorf("get %s: not found immediately after save", id)
				}
				_ = st.List()
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

	if len(st.List()) != 0 {
		t.Fatalf("expected every session expired after the final sweep, got %d active", len(st.List()))
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

	if got, ok := st.Get("0123abcd"); !ok || got.Status() != StatusExpired {
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

	if _, ok := st.Get(corruptID); ok {
		t.Fatal("corrupt id should not be in the live index")
	}
	if !st.Corrupted(corruptID) {
		t.Fatal("Corrupted should report true for a known-corrupt id")
	}
	if st.Corrupted("ffffffff") {
		t.Fatal("Corrupted should report false for an id that was never seen at all")
	}

	// A never-existed id and a corrupt id both fail Get identically...
	_, neverOK := st.Get("ffffffff")
	_, corruptOK := st.Get(corruptID)
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
	if _, ok := st.Get(corruptID); !ok {
		t.Fatal("healed id should now be found")
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
