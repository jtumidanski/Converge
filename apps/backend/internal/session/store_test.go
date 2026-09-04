package session

import (
	"context"
	"errors"
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

func writeRecord(t *testing.T, st *Store, s Session) {
	t.Helper()
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	delete(st.index, s.ID()) // simulate a fresh process
	st.mu.Unlock()
}
