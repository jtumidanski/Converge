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
