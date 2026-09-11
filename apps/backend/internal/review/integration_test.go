//go:build integration

package review

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/identity"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/session"
)

func pathsOf(s session.Session) map[string]bool {
	out := map[string]bool{}
	for _, f := range s.Files() {
		out[f.Path] = true
	}
	return out
}

func TestIntegrationSquashMerge(t *testing.T) {
	h := newHarness(t)
	base := h.src.Head()
	h.src.Branch("feat/a")
	c1 := h.src.Commit("a.txt", "a\n", "a1")
	h.src.Checkout("main")
	squash := h.src.Squash("feat/a", "squash a")
	h.src.Push()
	h.addChange(t, 1, "", squash, c1, []string{c1})

	got := h.build(t, 1)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	if got.BaseSHA() != base {
		t.Errorf("base = %s want %s", got.BaseSHA(), base)
	}
	if got.ResolvedChanges()[0].Strategy() != session.StrategySquash {
		t.Errorf("strategy = %s", got.ResolvedChanges()[0].Strategy())
	}
	if !pathsOf(got)["a.txt"] || len(got.Files()) != 1 {
		t.Errorf("files = %+v", got.Files())
	}
}

func TestIntegrationMergeCommitWithMultipleCommits(t *testing.T) {
	h := newHarness(t)
	base := h.src.Head()
	h.src.Branch("feat/a")
	h.src.Commit("a.txt", "a1\n", "a1")
	h.src.Commit("a.txt", "a1\na2\n", "a2")
	h.src.Checkout("main")
	branchCommits := h.src.BranchCommits("feat/a")
	merge := h.src.MergeNoFF("feat/a", "merge a")
	h.src.Push()
	h.addChange(t, 1, merge, "", branchCommits[len(branchCommits)-1], branchCommits)

	got := h.build(t, 1)
	if got.Status() != session.StatusReady || got.BaseSHA() != base {
		t.Fatalf("status=%s base=%s err=%+v", got.Status(), got.BaseSHA(), got.Error())
	}
	if got.ResolvedChanges()[0].Strategy() != session.StrategyMerge {
		t.Errorf("strategy = %s", got.ResolvedChanges()[0].Strategy())
	}
	fd, err := h.svc.FileDiff(context.Background(), got.ID(), "a.txt", identity.Standalone())
	if err != nil || !strings.Contains(fd.Diff, "+a1") || !strings.Contains(fd.Diff, "+a2") {
		t.Errorf("diff = %q err=%v", fd.Diff, err)
	}
}

func TestIntegrationRebaseFastForward(t *testing.T) {
	h := newHarness(t)
	base := h.src.Head()
	h.src.Branch("feat/a")
	orig := []string{h.src.Commit("a.txt", "a1\n", "a1"), h.src.Commit("b.txt", "b1\n", "b1")}
	h.src.Checkout("main")
	rebased := h.src.Rebase("feat/a")
	h.src.Push()
	// the provider reports the ORIGINAL commit SHAs and the rewritten head
	h.addChange(t, 1, "", "", rebased[len(rebased)-1], orig)

	got := h.build(t, 1)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	if got.ResolvedChanges()[0].Strategy() != session.StrategyRebase || len(got.ResolvedChanges()[0].LandingSHAs()) != 2 {
		t.Errorf("resolved = %+v", got.ResolvedChanges()[0])
	}
	if got.BaseSHA() != base {
		t.Errorf("base = %s want %s", got.BaseSHA(), base)
	}
	if !pathsOf(got)["a.txt"] || !pathsOf(got)["b.txt"] || len(got.Files()) != 2 {
		t.Errorf("files = %+v", got.Files())
	}
}

func TestIntegrationUnrelatedCommitsAreExcluded(t *testing.T) {
	h := newHarness(t)
	h.src.Commit("unrelated-before.txt", "x\n", "before")
	base := h.src.Head()

	h.src.Branch("feat/a")
	a := h.src.Commit("a.txt", "a\n", "a")
	h.src.Checkout("main")
	sqA := h.src.Squash("feat/a", "squash a")
	h.src.Commit("unrelated-middle.txt", "y\n", "middle")

	h.src.Branch("feat/b")
	b := h.src.Commit("b.txt", "b\n", "b")
	h.src.Checkout("main")
	sqB := h.src.Squash("feat/b", "squash b")
	h.src.Commit("unrelated-after.txt", "z\n", "after")
	h.src.Push()

	h.addChange(t, 1, "", sqA, a, []string{a})
	h.addChange(t, 2, "", sqB, b, []string{b})

	got := h.build(t, 2, 1)
	if got.Status() != session.StatusReady || got.BaseSHA() != base {
		t.Fatalf("status=%s base=%s err=%+v", got.Status(), got.BaseSHA(), got.Error())
	}
	paths := pathsOf(got)
	if !paths["a.txt"] || !paths["b.txt"] || len(paths) != 2 {
		t.Fatalf("files = %v", paths)
	}
	p, err := h.svc.CombinedDiffPath(got.ID(), identity.Standalone())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	for _, unrelated := range []string{"unrelated-before.txt", "unrelated-middle.txt", "unrelated-after.txt"} {
		if strings.Contains(string(raw), unrelated) {
			t.Errorf("combined diff mentions %s", unrelated)
		}
	}
}

func TestIntegrationRepeatedEditsToOneFileCollapse(t *testing.T) {
	h := newHarness(t)
	h.src.Commit("f.txt", "v1\n", "seed")
	h.src.Branch("feat/a")
	a := h.src.Commit("f.txt", "v2\n", "a")
	h.src.Checkout("main")
	sqA := h.src.Squash("feat/a", "squash a")
	h.src.Branch("feat/b")
	b := h.src.Commit("f.txt", "v3\n", "b")
	h.src.Checkout("main")
	sqB := h.src.Squash("feat/b", "squash b")
	h.src.Push()
	h.addChange(t, 1, "", sqA, a, []string{a})
	h.addChange(t, 2, "", sqB, b, []string{b})

	got := h.build(t, 1, 2)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	files := got.Files()
	if len(files) != 1 || files[0].Path != "f.txt" || files[0].Additions != 1 || files[0].Deletions != 1 {
		t.Fatalf("expected one cumulative hunk: %+v", files)
	}
	fd, _ := h.svc.FileDiff(context.Background(), got.ID(), "f.txt", identity.Standalone())
	if !strings.Contains(fd.Diff, "+v3") || strings.Contains(fd.Diff, "+v2") {
		t.Errorf("diff should show only the net change:\n%s", fd.Diff)
	}
}

// TestIntegrationDependencyOnUnselectedChangeConflicts deviates from the
// brief's literal fixture: the brief's addChange call order (1, 2, 3) makes
// change 3 the LATEST-merged of the three (mergedAt increases with each
// addChange call), so among the selected set {2, 3} change 2 is earliest and
// the resolver's base becomes parent(sq2) == sq1 — which already contains
// change 1's content as a real git ancestor, so replay never notices change 1
// is unselected and the build goes READY instead of CONFLICTED (confirmed by
// running the brief's fixture verbatim). The base SHA is always the real git
// parent of the earliest-merged SELECTED change, so an unselected change can
// only be "missing" from the replay if the earliest-merged selected change
// was actually merged to main BEFORE the unselected change, in true git
// history. This fixture reorders merges so change 3 (unrelated file, selected)
// lands FIRST, then change 1 (NOT selected) rewrites f.txt, then change 2
// (selected) builds on change 1's content:
//
//	main: seed -> sq3 (g.txt) -> sq1 (f.txt, not selected) -> sq2 (f.txt, selected)
//
// addChange is called in merge order (3, 1, 2) so mergedAt sorts the selected
// set as [3, 2]: base = parent(sq3) = seed, change 3 applies cleanly, then
// change 2's patch (whose real parent is sq1, expecting "from one\n" as
// f.txt's content) hits a workspace that still has "base\n" — a genuine
// conflict caused by the missing change 1, not a rewritten-file coincidence.
func TestIntegrationDependencyOnUnselectedChangeConflicts(t *testing.T) {
	h := newHarness(t)
	h.src.Commit("f.txt", "base\n", "seed")
	// change 3 (selected, merges first) touches an unrelated file
	h.src.Branch("feat/3")
	c3 := h.src.Commit("g.txt", "g\n", "three")
	h.src.Checkout("main")
	sq3 := h.src.Squash("feat/3", "squash 3")
	// change 1 (NOT selected) rewrites f.txt, merging after change 3
	h.src.Branch("feat/1")
	c1 := h.src.Commit("f.txt", "from one\n", "one")
	h.src.Checkout("main")
	sq1 := h.src.Squash("feat/1", "squash 1")
	// change 2 (selected) builds on change 1's content
	h.src.Branch("feat/2")
	c2 := h.src.Commit("f.txt", "from one and two\n", "two")
	h.src.Checkout("main")
	sq2 := h.src.Squash("feat/2", "squash 2")
	h.src.Push()
	// addChange order sets mergedAt order: 3 earliest, 1, then 2 latest.
	h.addChange(t, 3, "", sq3, c3, []string{c3})
	h.addChange(t, 1, "", sq1, c1, []string{c1})
	h.addChange(t, 2, "", sq2, c2, []string{c2})

	got := h.build(t, 2, 3) // 3 merged before 2, so 3 applies first, then 2 depends on unselected 1
	if got.Status() != session.StatusConflicted {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	re := got.Error()
	if re.Code != session.CodeConflict || re.Change != 2 {
		t.Fatalf("error = %+v", re)
	}
	if len(re.ConflictingFiles) != 1 || re.ConflictingFiles[0] != "f.txt" {
		t.Errorf("conflicting files = %v", re.ConflictingFiles)
	}
	for _, n := range re.AppliedChanges {
		if n == 1 {
			t.Fatal("unselected change 1 must never be applied")
		}
	}
	p, _ := h.svc.CombinedDiffPath(got.ID(), identity.Standalone())
	if p != "" {
		t.Error("no combined diff may exist for a conflicted session")
	}
}

// TestIntegrationPlainConflictBetweenSelectedChanges deviates from the
// brief's literal fixture: the brief squashes feat/b onto "main" AFTER
// change a already landed there, but feat/b was branched from the pre-a
// state, so `git merge --squash feat/b` performs a real three-way merge
// (base = pre-a, ours = main-with-a, theirs = feat/b) and that merge itself
// conflicts on f.txt — the fixture setup fails with a git error before the
// review is ever built (confirmed: `git merge --squash feat/b` exits 1).
// Squashing feat/b onto a separate staging branch rooted at the same pre-a
// commit avoids that setup conflict (staging's base IS feat/b's base, so the
// squash is a trivial fast-forward), while still producing a real squash
// commit (sqB) whose diff is expressed against the *pre-a* content. sqB is
// then folded into main via a manually-resolved merge commit so it becomes a
// real ancestor of main (satisfying the base-branch reachability check)
// without changing sqB's own recorded diff. Replaying sqA then sqB from the
// shared pre-a base therefore hits a genuine cherry-pick conflict: sqB's
// patch expects "original" as f.txt's content, but sqA has already changed
// it to "from a".
func TestIntegrationPlainConflictBetweenSelectedChanges(t *testing.T) {
	h := newHarness(t)
	h.src.Commit("f.txt", "original\n", "seed")
	seed := h.src.Head()
	h.src.Branch("feat/a")
	a := h.src.Commit("f.txt", "from a\n", "a")
	h.src.Checkout("main")
	sqA := h.src.Squash("feat/a", "squash a")

	h.src.Git("checkout", "-b", "feat/b", seed)
	b := h.src.Commit("f.txt", "from b\n", "b")
	h.src.Git("checkout", "-b", "squash-b-staging", seed)
	sqB := h.src.Squash("feat/b", "squash b")

	// Fold sqB into main so it becomes a real ancestor of the base branch.
	// The resolution (-X theirs) is arbitrary busywork: only sqB's own diff
	// (against its real parent, seed) is ever replayed by the review, so
	// what this merge commit's tree ends up containing does not matter.
	h.src.Checkout("main")
	h.src.Git("merge", "--no-ff", "-X", "theirs", "-m", "fold in squash-b-staging", "squash-b-staging")
	h.src.Push()
	h.addChange(t, 1, "", sqA, a, []string{a})
	h.addChange(t, 2, "", sqB, b, []string{b})

	got := h.build(t, 1, 2)
	if got.Status() != session.StatusConflicted {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	re := got.Error()
	if re.Change != 2 || !re.PossibleDependency || len(re.AppliedChanges) != 1 || re.AppliedChanges[0] != 1 {
		t.Fatalf("error = %+v", re)
	}
	if re.Diagnostics == nil || re.Diagnostics.WorkspacePath == "" || re.Diagnostics.Branch != "review/"+got.ID() {
		t.Errorf("diagnostics = %+v", re.Diagnostics)
	}
}

func TestIntegrationCleanupLeavesMirrorUsable(t *testing.T) {
	h := newHarness(t)
	h.src.Branch("feat/a")
	a := h.src.Commit("a.txt", "a\n", "a")
	h.src.Checkout("main")
	sq := h.src.Squash("feat/a", "squash a")
	h.src.Push()
	h.addChange(t, 1, "", sq, a, []string{a})

	got := h.build(t, 1)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s err=%+v", got.Status(), got.Error())
	}
	ctx := context.Background()
	if err := h.svc.Finish(ctx, got.ID(), identity.Standalone()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.workspaces.SessionDir(got.ID())); !os.IsNotExist(err) {
		t.Error("session dir remains")
	}
	mirrorPath, err := h.mirrors.Path(mirror.RootNamespace(), "fake", "atlas/server")
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"branch", "--list", "review/*"}, Category: gitx.CategoryQuery})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "" {
		t.Errorf("review branch remains: %q %v", res.Stdout, err)
	}
	if _, err := h.runner.Run(ctx, gitx.Spec{Dir: mirrorPath, Args: []string{"fsck", "--no-progress"}, Category: gitx.CategoryQuery}); err != nil {
		t.Fatalf("mirror damaged: %v", err)
	}
	// the mirror is still usable for a second review
	got2 := h.build(t, 1)
	if got2.Status() != session.StatusReady {
		t.Fatalf("second build: %s %+v", got2.Status(), got2.Error())
	}
	if err := h.svc.Finish(ctx, got2.ID(), identity.Standalone()); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Finish(ctx, got2.ID(), identity.Standalone()); err != nil {
		t.Fatalf("cleanup must be idempotent: %v", err)
	}
}

func TestIntegrationConcurrentBuildsShareOneMirror(t *testing.T) {
	h := newHarness(t)
	var shas []string
	for i := 1; i <= 4; i++ {
		name := "feat/" + itoa(i)
		h.src.Branch(name)
		c := h.src.Commit("f"+itoa(i)+".txt", "c\n", "c"+itoa(i))
		h.src.Checkout("main")
		sq := h.src.Squash(name, "squash "+itoa(i))
		shas = append(shas, sq)
		h.addChange(t, i, "", sq, c, []string{c})
	}
	h.src.Push()
	_ = shas

	var wg sync.WaitGroup
	results := make([]session.Session, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = h.build(t, i+1)
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r.Status() != session.StatusReady {
			t.Errorf("build %d: status=%s err=%+v", i+1, r.Status(), r.Error())
		}
	}
	mirrorPath, _ := h.mirrors.Path(mirror.RootNamespace(), "fake", "atlas/server")
	if _, err := h.runner.Run(context.Background(), gitx.Spec{Dir: mirrorPath, Args: []string{"fsck", "--no-progress"}, Category: gitx.CategoryQuery}); err != nil {
		t.Fatalf("mirror corrupted by concurrent builds: %v", err)
	}
}

func TestIntegrationNoTokenLeaksIntoMirrorRemote(t *testing.T) {
	h := newHarness(t)
	h.src.Branch("feat/a")
	a := h.src.Commit("a.txt", "a\n", "a")
	h.src.Checkout("main")
	sq := h.src.Squash("feat/a", "squash a")
	h.src.Push()
	h.addChange(t, 1, "", sq, a, []string{a})
	got := h.build(t, 1)
	if got.Status() != session.StatusReady {
		t.Fatalf("status=%s", got.Status())
	}
	mirrorPath, _ := h.mirrors.Path(mirror.RootNamespace(), "fake", "atlas/server")
	res, err := h.runner.Run(context.Background(), gitx.Spec{Dir: mirrorPath, Args: []string{"remote", "get-url", "origin"}, Category: gitx.CategoryQuery})
	if err != nil {
		t.Fatal(err)
	}
	url := strings.TrimSpace(string(res.Stdout))
	if url != h.src.CloneURL() || strings.Contains(url, "@") {
		t.Errorf("remote url = %q", url)
	}
	raw, err := os.ReadFile(h.workspaces.SessionDir(got.ID()) + "/session.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Authorization", "PRIVATE-TOKEN", "extraheader"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("session.json contains %q", forbidden)
		}
	}
}
