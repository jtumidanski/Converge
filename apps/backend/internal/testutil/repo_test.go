package testutil

import (
	"strings"
	"testing"
)

func TestHarnessStrategies(t *testing.T) {
	r := NewRepo(t)
	base := r.Head()

	r.Branch("feat/a")
	a1 := r.Commit("a.txt", "a1\n", "a1")
	a2 := r.Commit("a.txt", "a1\na2\n", "a2")
	r.Checkout("main")
	if got := r.BranchCommits("feat/a"); len(got) != 2 || got[0] != a1 || got[1] != a2 {
		t.Fatalf("BranchCommits = %v", got)
	}
	merge := r.MergeNoFF("feat/a", "Merge feat/a")
	if parents := strings.Fields(r.Git("rev-list", "--parents", "-n", "1", merge)); len(parents) != 3 || parents[1] != base {
		t.Fatalf("merge parents = %v", parents)
	}

	r.Branch("feat/b")
	r.Commit("b.txt", "b\n", "b1")
	r.Commit("b.txt", "bb\n", "b2")
	r.Checkout("main")
	squash := r.Squash("feat/b", "Squash feat/b")
	if parents := strings.Fields(r.Git("rev-list", "--parents", "-n", "1", squash)); len(parents) != 2 || parents[1] != merge {
		t.Fatalf("squash parents = %v", parents)
	}
	if r.FileContent("main", "b.txt") != "bb\n" {
		t.Fatal("squash content")
	}

	r.Branch("feat/c")
	c1 := r.Commit("c.txt", "c\n", "c1")
	r.Checkout("main")
	r.Commit("other.txt", "x\n", "unrelated on main")
	rebased := r.Rebase("feat/c")
	if len(rebased) != 1 || rebased[0] == c1 || r.Head() != rebased[0] {
		t.Fatalf("rebase = %v head=%s c1=%s", rebased, r.Head(), c1)
	}
	r.Push()
	if !strings.HasPrefix(r.CloneURL(), "file://") {
		t.Fatal("clone url")
	}
}
