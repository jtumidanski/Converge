package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/diff"
)

func TestRecordRoundTrip(t *testing.T) {
	s := newSession(t)
	rc, _ := NewResolvedChange(ResolvedChangeParams{Number: 421, Title: "t", Author: "a", WebURL: "u", MergedAt: t0, Strategy: StrategyMerge, LandingSHAs: []string{strings.Repeat("a", 40)}, SourceSHA: strings.Repeat("a", 40)})
	s = s.WithResolved([]ResolvedChange{rc}, t0)
	s, _ = s.WithBase(strings.Repeat("b", 40), t0)
	s, _ = s.Ready(strings.Repeat("c", 40), []diff.FileSummary{{Path: "x", Status: diff.StatusAdded, Additions: 2}}, diff.Totals{Files: 1, Additions: 2}, t0.Add(time.Minute))
	b, err := json.Marshal(ToRecord(s))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"schemaVersion":1`, `"id":"0123abcd"`, `"providerId":"gh"`, `"status":"READY"`, `"resolvedChanges"`, `"landingShas"`, `"totals"`, `"files"`, `"expiresAt"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("json lacks %s: %s", key, b)
		}
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	back, err := FromRecord(r)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID() != s.ID() || back.Status() != StatusReady || back.BaseSHA() != s.BaseSHA() || back.HeadSHA() != s.HeadSHA() || len(back.ResolvedChanges()) != 1 || back.ResolvedChanges()[0].Strategy() != StrategyMerge || back.Totals().Additions != 2 || len(back.Files()) != 1 || !back.ExpiresAt().Equal(s.ExpiresAt()) {
		t.Errorf("round trip mismatch: %+v", back)
	}
	// unknown fields are ignored
	var r2 Record
	if err := json.Unmarshal([]byte(`{"schemaVersion":1,"id":"0123abcd","providerId":"gh","repository":"a/b","baseBranch":"main","requestedChanges":[1],"status":"CREATING","createdAt":"2026-09-01T12:00:00Z","updatedAt":"2026-09-01T12:00:00Z","expiresAt":"2026-09-02T12:00:00Z","futureField":true}`), &r2); err != nil {
		t.Fatal(err)
	}
	if _, err := FromRecord(r2); err != nil {
		t.Fatal(err)
	}
	r2.Status = "WEIRD"
	if _, err := FromRecord(r2); err == nil {
		t.Error("bad status accepted")
	}
}
