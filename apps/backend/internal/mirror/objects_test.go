package mirror

import (
	"context"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
)

func TestObjectReader(t *testing.T) {
	src := testutilRepo(t)
	base := src.Head()
	src.Branch("feat")
	c1 := src.Commit("f.txt", "1\n", "c1")
	c2 := src.Commit("f.txt", "1\n2\n", "c2")
	src.Checkout("main")
	merge := src.MergeNoFF("feat", "merge feat")
	src.Push()

	runner, err := gitx.NewExecRunner(testLogger(), gitx.Options{CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	cache := New(t.TempDir(), runner, &gitx.LockMap{}, testLogger())
	path, err := cache.Ensure(context.Background(), fake.New("fake", provider.KindGitLab), repoFor(t, "fake", src.CloneURL()))
	if err != nil {
		t.Fatal(err)
	}
	o := cache.Objects(path, "atlas/server")
	ctx := context.Background()

	if ok, _ := o.Exists(ctx, merge); !ok {
		t.Error("merge should exist")
	}
	if ok, err := o.Exists(ctx, "0000000000000000000000000000000000000000"); ok || err != nil {
		t.Errorf("zero sha: %v %v", ok, err)
	}
	if ps, _ := o.Parents(ctx, merge); len(ps) != 2 || ps[0] != base || ps[1] != c2 {
		t.Errorf("parents = %v", ps)
	}
	if ps, _ := o.Parents(ctx, c1); len(ps) != 1 || ps[0] != base {
		t.Errorf("c1 parents = %v", ps)
	}
	id1, _ := o.PatchID(ctx, c1)
	id2, _ := o.PatchID(ctx, c2)
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Errorf("patch ids %q %q", id1, id2)
	}
	if w, _ := o.FirstParentWalk(ctx, c2, 2); len(w) != 2 || w[0] != c1 || w[1] != c2 {
		t.Errorf("walk = %v", w)
	}
	if ok, _ := o.IsAncestor(ctx, c1, "main"); !ok {
		t.Error("c1 should be ancestor of main")
	}
	if ok, _ := o.IsAncestor(ctx, merge, "feat"); ok {
		t.Error("merge is not ancestor of feat")
	}
	if got, _ := o.RevParse(ctx, merge+"^1"); got != base {
		t.Errorf("rev-parse ^1 = %s", got)
	}
	if ok, _ := o.BranchExists(ctx, "main"); !ok {
		t.Error("main missing")
	}
	if ok, _ := o.BranchExists(ctx, "nope"); ok {
		t.Error("nope exists")
	}
	if _, err := o.Parents(ctx, "bad"); err == nil {
		t.Error("invalid sha accepted")
	}
}
