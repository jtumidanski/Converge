package session

import (
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
)

var t0 = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func newSession(t *testing.T) Session {
	t.Helper()
	s, err := NewBuilder().SetID("0123abcd").SetProviderID("gh").SetRepository("atlas/server").SetBaseBranch("main").
		SetRequestedChanges([]int{427, 421}).SetCreatedAt(t0).SetTTL(24 * time.Hour).Build()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuilderAndAccessors(t *testing.T) {
	s := newSession(t)
	if s.Status() != StatusCreating || s.Stage() != "" || s.BaseSHA() != "" || !s.ExpiresAt().Equal(t0.Add(24*time.Hour)) || !s.UpdatedAt().Equal(t0) {
		t.Errorf("initial state wrong: %+v", s)
	}
	if got := s.RequestedChanges(); len(got) != 2 || got[0] != 421 || got[1] != 427 {
		t.Errorf("requested = %v (want sorted, de-duplicated)", got)
	}
	got := s.RequestedChanges()
	got[0] = 999
	if s.RequestedChanges()[0] != 421 {
		t.Error("RequestedChanges leaked internal slice")
	}
	if s.IsExpired(t0.Add(23*time.Hour)) || !s.IsExpired(t0.Add(25*time.Hour)) || !s.IsActive() {
		t.Error("expiry wrong")
	}
	bad := []struct {
		name string
		f    func(*Builder) *Builder
	}{
		{"id", func(b *Builder) *Builder { return b.SetID("xyz") }},
		{"provider", func(b *Builder) *Builder { return b.SetProviderID("") }},
		{"repo", func(b *Builder) *Builder { return b.SetRepository("../x") }},
		{"branch", func(b *Builder) *Builder { return b.SetBaseBranch("-x") }},
		{"changes", func(b *Builder) *Builder { return b.SetRequestedChanges(nil) }},
		{"ttl", func(b *Builder) *Builder { return b.SetTTL(0) }},
	}
	for _, tc := range bad {
		b := NewBuilder().SetID("0123abcd").SetProviderID("gh").SetRepository("atlas/server").SetBaseBranch("main").SetRequestedChanges([]int{1}).SetCreatedAt(t0).SetTTL(time.Hour)
		if _, err := tc.f(b).Build(); err == nil {
			t.Errorf("%s: invalid accepted", tc.name)
		}
	}
}

func TestTransitionsAreImmutable(t *testing.T) {
	s := newSession(t)
	t1 := t0.Add(time.Minute)
	s2 := s.WithStage("resolving", t1)
	if s.Stage() != "" || s2.Stage() != "resolving" || !s2.UpdatedAt().Equal(t1) {
		t.Error("WithStage mutated receiver or did not apply")
	}
	rc, err := NewResolvedChange(ResolvedChangeParams{Number: 421, Title: "t", Author: "a", MergedAt: t0, Strategy: StrategySquash, LandingSHAs: []string{strings.Repeat("a", 40)}, SourceSHA: strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	s3 := s2.WithResolved([]ResolvedChange{rc}, t1)
	if e, ok := s3.EarliestChange(); !ok || e.Number() != 421 {
		t.Error("EarliestChange")
	}
	if _, err := s3.WithBase("bad", t1); err == nil {
		t.Error("bad base accepted")
	}
	s4, _ := s3.WithBase(strings.Repeat("b", 40), t1)
	files := []diff.FileSummary{{Path: "a.go", Status: diff.StatusAdded, Additions: 1}}
	s5, err := s4.Ready(strings.Repeat("c", 40), files, diff.Totals{Files: 1, Additions: 1}, t1)
	if err != nil || s5.Status() != StatusReady || s5.Stage() != "" || s5.HeadSHA() != strings.Repeat("c", 40) || s5.Totals().Files != 1 || len(s5.Files()) != 1 {
		t.Errorf("Ready: %v %+v", err, s5)
	}
	if _, err := s.Ready(strings.Repeat("c", 40), nil, diff.Totals{}, t1); err == nil {
		t.Error("Ready without base accepted")
	}
	re := &ReviewError{Code: CodeConflict, Message: "boom", Change: 421, ConflictingFiles: []string{"x"}}
	s6 := s4.Conflicted(re, t1)
	if s6.Status() != StatusConflicted || s6.Error() == nil || s6.Error().Code != CodeConflict {
		t.Error("Conflicted")
	}
	s6.Error().ConflictingFiles[0] = "mutated"
	if s6.Error().ConflictingFiles[0] != "x" {
		t.Error("Error() leaked internal slice")
	}
	s7 := s4.Failed(&ReviewError{Code: CodeNotMerged, Message: "m"}, t1)
	if s7.Status() != StatusFailed || s7.Error().Code != CodeNotMerged {
		t.Error("Failed")
	}
	if f := s5.Finished(t1); f.Status() != StatusFinished || f.IsActive() {
		t.Error("Finished")
	}
	if e := s5.Expired(t1); e.Status() != StatusExpired || e.IsActive() {
		t.Error("Expired")
	}
	if _, err := NewResolvedChange(ResolvedChangeParams{Number: 0}); err == nil {
		t.Error("resolved change number 0 accepted")
	}
	if _, err := NewResolvedChange(ResolvedChangeParams{Number: 1, Title: "t", Strategy: "weird", LandingSHAs: []string{strings.Repeat("a", 40)}}); err == nil {
		t.Error("bad strategy accepted")
	}
}

// terminalSessions returns a FINISHED and an EXPIRED session derived from a
// fresh READY session, for use by TestTerminalTransitionsAreNoOps.
func terminalSessions(t *testing.T) (finished, expired Session) {
	t.Helper()
	s := newSession(t)
	s, err := s.WithBase(strings.Repeat("b", 40), t0)
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.Ready(strings.Repeat("c", 40), nil, diff.Totals{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	finished = s.Finished(t0.Add(time.Minute))
	expired = s.Expired(t0.Add(time.Minute))
	return finished, expired
}

// TestTerminalTransitionsAreNoOps proves finding 1's fix: none of the eight
// transitions can resurrect a FINISHED or EXPIRED session. The six
// no-error transitions must return the receiver completely unchanged; the
// two error-returning transitions (WithBase, Ready) must return an error.
func TestTerminalTransitionsAreNoOps(t *testing.T) {
	t2 := t0.Add(time.Hour)
	re := &ReviewError{Code: CodeConflict, Message: "boom"}

	check := func(name string, terminal Session) {
		wantStatus := terminal.Status()
		wantUpdatedAt := terminal.UpdatedAt()

		if got := terminal.WithStage("resolving", t2); got.Status() != wantStatus || !got.UpdatedAt().Equal(wantUpdatedAt) || got.Stage() != terminal.Stage() {
			t.Errorf("%s: WithStage mutated a terminal session: %+v", name, got)
		}
		rc, err := NewResolvedChange(ResolvedChangeParams{Number: 1, Title: "t", Strategy: StrategyMerge, LandingSHAs: []string{strings.Repeat("a", 40)}})
		if err != nil {
			t.Fatal(err)
		}
		if got := terminal.WithResolved([]ResolvedChange{rc}, t2); got.Status() != wantStatus || !got.UpdatedAt().Equal(wantUpdatedAt) || len(got.ResolvedChanges()) != len(terminal.ResolvedChanges()) {
			t.Errorf("%s: WithResolved mutated a terminal session: %+v", name, got)
		}
		if _, err := terminal.WithBase(strings.Repeat("d", 40), t2); err == nil {
			t.Errorf("%s: WithBase on a terminal session did not error", name)
		}
		if _, err := terminal.Ready(strings.Repeat("e", 40), nil, diff.Totals{}, t2); err == nil {
			t.Errorf("%s: Ready on a terminal session did not error", name)
		}
		if got := terminal.Conflicted(re, t2); got.Status() != wantStatus || !got.UpdatedAt().Equal(wantUpdatedAt) {
			t.Errorf("%s: Conflicted mutated a terminal session: %+v", name, got)
		}
		if got := terminal.Failed(re, t2); got.Status() != wantStatus || !got.UpdatedAt().Equal(wantUpdatedAt) {
			t.Errorf("%s: Failed mutated a terminal session: %+v", name, got)
		}
		if got := terminal.Finished(t2); got.Status() != wantStatus || !got.UpdatedAt().Equal(wantUpdatedAt) {
			t.Errorf("%s: Finished mutated a terminal session: %+v", name, got)
		}
		if got := terminal.Expired(t2); got.Status() != wantStatus || !got.UpdatedAt().Equal(wantUpdatedAt) {
			t.Errorf("%s: Expired mutated a terminal session: %+v", name, got)
		}
	}

	finished, expired := terminalSessions(t)
	if finished.Status() != StatusFinished {
		t.Fatalf("setup: finished status = %s", finished.Status())
	}
	if expired.Status() != StatusExpired {
		t.Fatalf("setup: expired status = %s", expired.Status())
	}
	check("FINISHED", finished)
	check("EXPIRED", expired)
}

// TestNonTerminalTransitionsStillWork is a control for
// TestTerminalTransitionsAreNoOps: it proves the terminal-state guard did
// not also block ordinary transitions between non-terminal states.
func TestNonTerminalTransitionsStillWork(t *testing.T) {
	s := newSession(t)
	t1 := t0.Add(time.Minute)

	s2 := s.WithStage("resolving", t1)
	if s2.Stage() != "resolving" || !s2.UpdatedAt().Equal(t1) {
		t.Fatalf("WithStage: %+v", s2)
	}
	rc, err := NewResolvedChange(ResolvedChangeParams{Number: 1, Title: "t", Strategy: StrategyMerge, LandingSHAs: []string{strings.Repeat("a", 40)}})
	if err != nil {
		t.Fatal(err)
	}
	s3 := s2.WithResolved([]ResolvedChange{rc}, t1)
	if len(s3.ResolvedChanges()) != 1 {
		t.Fatalf("WithResolved: %+v", s3)
	}
	s4, err := s3.WithBase(strings.Repeat("b", 40), t1)
	if err != nil || s4.BaseSHA() != strings.Repeat("b", 40) {
		t.Fatalf("WithBase: %v %+v", err, s4)
	}
	s5, err := s4.Ready(strings.Repeat("c", 40), nil, diff.Totals{}, t1)
	if err != nil || s5.Status() != StatusReady {
		t.Fatalf("Ready: %v %+v", err, s5)
	}
	re := &ReviewError{Code: CodeConflict, Message: "boom"}
	if got := s4.Conflicted(re, t1); got.Status() != StatusConflicted {
		t.Fatalf("Conflicted: %+v", got)
	}
	if got := s4.Failed(re, t1); got.Status() != StatusFailed {
		t.Fatalf("Failed: %+v", got)
	}
	if got := s5.Finished(t1); got.Status() != StatusFinished {
		t.Fatalf("Finished: %+v", got)
	}
	if got := s5.Expired(t1); got.Status() != StatusExpired {
		t.Fatalf("Expired: %+v", got)
	}
}

// TestEarliestChangeSortsByMergeTime proves finding 2's fix: EarliestChange
// picks the change with the smallest MergedAt regardless of slice order,
// breaking ties by ascending change Number for determinism.
func TestEarliestChangeSortsByMergeTime(t *testing.T) {
	mk := func(number int, mergedAt time.Time) ResolvedChange {
		rc, err := NewResolvedChange(ResolvedChangeParams{
			Number: number, Title: "t", Strategy: StrategyMerge,
			MergedAt: mergedAt, LandingSHAs: []string{strings.Repeat("a", 40)},
		})
		if err != nil {
			t.Fatal(err)
		}
		return rc
	}

	// Out-of-order input: the slice order does not match merge-time order.
	s := newSession(t)
	s = s.WithResolved([]ResolvedChange{
		mk(300, t0.Add(3*time.Hour)),
		mk(100, t0.Add(1*time.Hour)),
		mk(200, t0.Add(2*time.Hour)),
	}, t0)
	got, ok := s.EarliestChange()
	if !ok || got.Number() != 100 {
		t.Fatalf("out-of-order: got %+v", got)
	}

	// Tie on MergedAt: broken by ascending Number.
	tie := t0.Add(5 * time.Hour)
	s2 := newSession(t)
	s2 = s2.WithResolved([]ResolvedChange{
		mk(50, tie),
		mk(10, tie),
		mk(30, tie),
	}, t0)
	got2, ok := s2.EarliestChange()
	if !ok || got2.Number() != 10 {
		t.Fatalf("tie: got %+v", got2)
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewID()
		if err != nil || len(id) != 8 || strings.ToLower(id) != id || seen[id] {
			t.Fatalf("id %q err %v", id, err)
		}
		for _, c := range id {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Fatalf("non-hex %q", id)
			}
		}
		seen[id] = true
	}
}
