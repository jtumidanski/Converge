package session

import (
	"encoding/json"
	"reflect"
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

// TestRecordRoundTripFullStructure builds a session with every field
// populated - including ReviewError.Diagnostics, AppliedChanges and
// PossibleDependency, which the spot-check TestRecordRoundTrip never
// touches - writes it through ToRecord, marshals and unmarshals the JSON
// (so a wrong or missing json tag would actually show up), rebuilds via
// FromRecord, and compares the *whole* resulting Record against the
// original via reflect.DeepEqual so no field can silently go missing.
func TestRecordRoundTripFullStructure(t *testing.T) {
	s := newSession(t)
	t1 := t0.Add(time.Minute)
	t2 := t0.Add(2 * time.Minute)
	t3 := t0.Add(3 * time.Minute)

	rc1, err := NewResolvedChange(ResolvedChangeParams{
		Number: 421, Title: "Fix the thing", Author: "octocat", WebURL: "https://example.com/421",
		MergedAt: t1, Strategy: StrategySquash,
		LandingSHAs: []string{strings.Repeat("a", 40), strings.Repeat("b", 40)},
		SourceSHA:   strings.Repeat("c", 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	rc2, err := NewResolvedChange(ResolvedChangeParams{
		Number: 417, Title: "Second change", Author: "hubot", WebURL: "https://example.com/417",
		MergedAt: t1.Add(time.Hour), Strategy: StrategyRebase,
		LandingSHAs: []string{strings.Repeat("d", 40)},
		// SourceSHA deliberately empty to also exercise the omitempty path.
	})
	if err != nil {
		t.Fatal(err)
	}

	s = s.WithResolved([]ResolvedChange{rc1, rc2}, t1)
	s, err = s.WithBase(strings.Repeat("e", 40), t2)
	if err != nil {
		t.Fatal(err)
	}
	files := []diff.FileSummary{
		{Path: "a.go", Status: diff.StatusAdded, Additions: 5},
		{Path: "b.go", PreviousPath: "old_b.go", Status: diff.StatusRenamed, Additions: 2, Deletions: 1},
		{Path: "c.bin", Status: diff.StatusModified, Binary: true},
	}
	s, err = s.Ready(strings.Repeat("f", 40), files, diff.Totals{Files: 3, Additions: 7, Deletions: 1}, t3)
	if err != nil {
		t.Fatal(err)
	}
	// Conflicted preserves the totals/files Ready just set, and is the only
	// transition that lets us populate a fully-loaded ReviewError, so use it
	// to get every field - including totals/files AND the error - onto one
	// session.
	re := &ReviewError{
		Code: CodeConflict, Message: "3 files conflict", Change: 421, Commit: strings.Repeat("f", 40),
		ConflictingFiles: []string{"a.go", "b.go"}, AppliedChanges: []int{417}, PossibleDependency: true,
		Diagnostics: &Diagnostics{
			WorkspacePath: "/var/converge/sessions/0123abcd",
			Branch:        "converge/review/0123abcd",
			Strategy:      string(StrategySquash),
			SourceSHA:     strings.Repeat("c", 40),
		},
	}
	s = s.Conflicted(re, t3.Add(time.Minute))

	orig := ToRecord(s)
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"diagnostics"`, `"workspacePath"`, `"appliedChanges"`, `"possibleDependency":true`,
		`"conflictingFiles"`, `"webUrl"`, `"sourceSha"`, `"stage":null`,
	} {
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

	// Full-structure comparison: nothing in Record may differ between the
	// original and the round-tripped session.
	roundTripped := ToRecord(back)
	if !reflect.DeepEqual(orig, roundTripped) {
		t.Fatalf("round trip lost or altered a field:\n original = %+v\nroundTrip = %+v", orig, roundTripped)
	}

	// Belt-and-braces: confirm the risky fields explicitly, not just via
	// DeepEqual, so a future refactor that keeps DeepEqual passing by
	// accident still gets caught here.
	gotErr := back.Error()
	if gotErr == nil {
		t.Fatal("Error() is nil after round trip")
	}
	if gotErr.Diagnostics == nil || *gotErr.Diagnostics != *re.Diagnostics {
		t.Errorf("Diagnostics lost: %+v", gotErr.Diagnostics)
	}
	if len(gotErr.AppliedChanges) != 1 || gotErr.AppliedChanges[0] != 417 {
		t.Errorf("AppliedChanges lost: %v", gotErr.AppliedChanges)
	}
	if !gotErr.PossibleDependency {
		t.Error("PossibleDependency lost")
	}
	if len(gotErr.ConflictingFiles) != 2 {
		t.Errorf("ConflictingFiles lost: %v", gotErr.ConflictingFiles)
	}
	if back.ProviderID() != s.ProviderID() || back.Repository() != s.Repository() || back.BaseBranch() != s.BaseBranch() {
		t.Errorf("identity fields lost: %+v", back)
	}
	if got := back.RequestedChanges(); len(got) != 2 || got[0] != 421 || got[1] != 427 {
		t.Errorf("RequestedChanges lost: %v", got)
	}
	if !back.CreatedAt().Equal(s.CreatedAt()) || !back.UpdatedAt().Equal(s.UpdatedAt()) || !back.ExpiresAt().Equal(s.ExpiresAt()) {
		t.Errorf("timestamps lost: created=%v updated=%v expires=%v", back.CreatedAt(), back.UpdatedAt(), back.ExpiresAt())
	}
	rcs := back.ResolvedChanges()
	if len(rcs) != 2 {
		t.Fatalf("resolved changes lost: %v", rcs)
	}
	if rcs[0].Number() != 421 || rcs[0].Title() != "Fix the thing" || rcs[0].Author() != "octocat" || rcs[0].WebURL() != "https://example.com/421" ||
		!rcs[0].MergedAt().Equal(t1) || rcs[0].Strategy() != StrategySquash || rcs[0].SourceSHA() != strings.Repeat("c", 40) ||
		len(rcs[0].LandingSHAs()) != 2 {
		t.Errorf("resolved change 0 lost fields: %+v", rcs[0])
	}
	if rcs[1].SourceSHA() != "" {
		t.Errorf("resolved change 1 SourceSHA should stay empty: %q", rcs[1].SourceSHA())
	}
	if got := back.Files(); len(got) != 3 || got[1].PreviousPath != "old_b.go" || !got[2].Binary {
		t.Errorf("files lost: %+v", got)
	}
	if tot := back.Totals(); tot == nil || tot.Files != 3 || tot.Additions != 7 || tot.Deletions != 1 {
		t.Errorf("totals lost: %+v", tot)
	}
	if back.Stage() != "" {
		t.Errorf("Conflicted should have cleared stage: %q", back.Stage())
	}

	// Stage is only ever non-empty while a session is still CREATING (every
	// transition that sets an error or reaches READY clears it), so verify
	// its round trip separately with a session that has it set.
	staged := newSession(t).WithStage(StageResolving, t1)
	stagedBack, err := FromRecord(fromJSON(t, ToRecord(staged)))
	if err != nil {
		t.Fatal(err)
	}
	if stagedBack.Stage() != StageResolving {
		t.Errorf("stage lost: %q", stagedBack.Stage())
	}
}

func fromJSON(t *testing.T, r Record) Record {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out Record
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestFromRecordErrorPaths covers the FromRecord validation branches that
// run against on-disk data on every restart: an unsupported SchemaVersion,
// an unknown Status, and a Record that fails Build() validation (a
// malformed base SHA reaching FromRecord via a hand-corrupted or
// version-skewed session.json).
func TestFromRecordErrorPaths(t *testing.T) {
	valid := ToRecord(newSession(t))

	t.Run("unknown schema version", func(t *testing.T) {
		r := valid
		r.SchemaVersion = 99
		if _, err := FromRecord(r); err == nil {
			t.Error("unsupported schema version accepted")
		}
	})

	t.Run("unknown status", func(t *testing.T) {
		r := valid
		r.Status = "WEIRD"
		if _, err := FromRecord(r); err == nil {
			t.Error("unknown status accepted")
		}
	})

	t.Run("malformed base sha", func(t *testing.T) {
		r := valid
		bad := "not-a-sha"
		r.BaseSHA = &bad
		if _, err := FromRecord(r); err == nil {
			t.Error("malformed base sha accepted")
		}
	})

	t.Run("malformed resolved change strategy", func(t *testing.T) {
		r := valid
		r.ResolvedChanges = []ResolvedChangeRecord{{
			Number: 1, Title: "t", Strategy: "weird", LandingSHAs: []string{strings.Repeat("a", 40)},
		}}
		if _, err := FromRecord(r); err == nil {
			t.Error("malformed resolved change strategy accepted")
		}
	})

	t.Run("invalid repository fails builder validation", func(t *testing.T) {
		r := valid
		r.Repository = "../escape"
		if _, err := FromRecord(r); err == nil {
			t.Error("invalid repository accepted")
		}
	})

	t.Run("unparseable timestamp fails at json.Unmarshal, before FromRecord", func(t *testing.T) {
		// time.Time's JSON unmarshaling itself rejects non-RFC3339 content;
		// this is the shape that on-disk corruption of a timestamp actually
		// takes (the record never reaches FromRecord as valid Go values).
		raw := `{"schemaVersion":1,"id":"0123abcd","providerId":"gh","repository":"a/b","baseBranch":"main","requestedChanges":[1],"status":"CREATING","createdAt":"not-a-time","updatedAt":"2026-09-01T12:00:00Z","expiresAt":"2026-09-02T12:00:00Z"}`
		var r2 Record
		if err := json.Unmarshal([]byte(raw), &r2); err == nil {
			t.Error("unparseable createdAt accepted by json.Unmarshal")
		}
	})
}
