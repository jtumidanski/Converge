package provider

import (
	"strings"
	"testing"
	"time"
)

func testRepo(t *testing.T) Repository {
	t.Helper()
	r, err := NewRepositoryBuilder().SetProviderID("gh").SetFullName("atlas/server").SetName("server").
		SetNamespace("atlas").SetDefaultBranch("main").SetWebURL("https://github.com/atlas/server").
		SetCloneURL("https://github.com/atlas/server.git").Build()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRepositoryBuilder(t *testing.T) {
	r := testRepo(t)
	if r.FullName() != "atlas/server" || r.Name() != "server" || r.Namespace() != "atlas" || r.DefaultBranch() != "main" || r.ProviderID() != "gh" {
		t.Errorf("accessors wrong: %+v", r)
	}
	if _, err := NewRepositoryBuilder().SetProviderID("gh").SetFullName("../x").Build(); err == nil {
		t.Error("invalid full name accepted")
	}
	if _, err := NewRepositoryBuilder().SetProviderID("").SetFullName("a/b").Build(); err == nil {
		t.Error("empty provider accepted")
	}
	// Name and namespace derive from the full name when omitted.
	d, err := NewRepositoryBuilder().SetProviderID("gh").SetFullName("group/sub/proj").SetDefaultBranch("main").Build()
	if err != nil || d.Name() != "proj" || d.Namespace() != "group/sub" {
		t.Errorf("derivation wrong: %v %q %q", err, d.Name(), d.Namespace())
	}
}

func TestChangeRequestBuilder(t *testing.T) {
	sha := strings.Repeat("a", 40)
	merged := time.Date(2026, 8, 21, 14, 2, 11, 0, time.UTC)
	c, err := NewCommit(sha, "feat: x", merged.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	cr, err := NewChangeRequestBuilder().SetProviderID("gh").SetRepository(testRepo(t)).SetNumber(421).SetTitle("Add field-state endpoint").
		SetAuthor("jsmith").SetWebURL("https://github.com/atlas/server/pull/421").SetSourceBranch("feat/field-state").SetTargetBranch("main").
		SetCreatedAt(merged.Add(-24 * time.Hour)).SetMergedAt(merged).SetState(StateMerged).SetMergeCommitSHA(sha).SetHeadSHA(sha).
		SetCommitCount(1).SetCommits([]Commit{c}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if cr.Number() != 421 || cr.State() != StateMerged || !cr.MergedAt().Equal(merged) || cr.MergeCommitSHA() != sha || cr.SquashCommitSHA() != "" || len(cr.Commits()) != 1 || cr.CommitCount() != 1 {
		t.Errorf("accessors wrong")
	}
	cr2 := cr.WithCommits(nil)
	if len(cr.Commits()) != 1 || len(cr2.Commits()) != 0 {
		t.Error("WithCommits must not mutate the receiver")
	}
	if _, err := NewChangeRequestBuilder().SetProviderID("gh").SetRepository(testRepo(t)).SetNumber(0).SetTitle("t").SetTargetBranch("main").Build(); err == nil {
		t.Error("number 0 accepted")
	}
	if _, err := NewChangeRequestBuilder().SetProviderID("gh").SetRepository(testRepo(t)).SetNumber(1).SetTitle("t").SetTargetBranch("main").SetMergeCommitSHA("nothex").Build(); err == nil {
		t.Error("bad sha accepted")
	}
	if _, err := NewCommit("bad", "m", merged); err == nil {
		t.Error("bad commit sha accepted")
	}
}

func TestPageNormalizeAndSearchNumber(t *testing.T) {
	if p := (Page{}).Normalize(); p.Number != 1 || p.Size != 30 {
		t.Errorf("default = %+v", p)
	}
	if p := (Page{Number: 3, Size: 500}).Normalize(); p.Number != 3 || p.Size != 100 {
		t.Errorf("clamp = %+v", p)
	}
	for in, want := range map[string]int{"421": 421, "#421": 421, "!7": 7, " 12 ": 12} {
		if n, ok := ParseSearchNumber(in); !ok || n != want {
			t.Errorf("%q -> %d,%v", in, n, ok)
		}
	}
	for _, in := range []string{"", "abc", "4a", "#", "0"} {
		if _, ok := ParseSearchNumber(in); ok {
			t.Errorf("%q parsed as number", in)
		}
	}
}
