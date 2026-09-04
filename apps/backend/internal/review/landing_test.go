package review

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/session"
)

func sha(c string) string { return strings.Repeat(c, 40) }

// fakeObjects scripts an ObjectReader.
type fakeObjects struct {
	exists   map[string]bool
	parents  map[string][]string
	patchIDs map[string]string
	walks    map[string][]string
}

func (f *fakeObjects) Exists(_ context.Context, s string) (bool, error) { return f.exists[s], nil }
func (f *fakeObjects) Parents(_ context.Context, s string) ([]string, error) {
	p, ok := f.parents[s]
	if !ok {
		return nil, errors.New("unknown commit " + s)
	}
	return p, nil
}
func (f *fakeObjects) PatchID(_ context.Context, s string) (string, error) { return f.patchIDs[s], nil }
func (f *fakeObjects) FirstParentWalk(_ context.Context, s string, n int) ([]string, error) {
	w := f.walks[s]
	if len(w) > n {
		w = w[len(w)-n:]
	}
	return w, nil
}
func (f *fakeObjects) IsAncestor(context.Context, string, string) (bool, error) { return true, nil }
func (f *fakeObjects) RevParse(context.Context, string) (string, error)         { return "", nil }
func (f *fakeObjects) BranchExists(context.Context, string) (bool, error)       { return true, nil }

var _ mirror.ObjectReader = (*fakeObjects)(nil)

func change(t *testing.T, merge, squash, head string, count int) provider.ChangeRequest {
	t.Helper()
	repo, err := provider.NewRepositoryBuilder().SetProviderID("p").SetFullName("a/b").SetDefaultBranch("main").Build()
	if err != nil {
		t.Fatal(err)
	}
	b := provider.NewChangeRequestBuilder().SetProviderID("p").SetRepository(repo).SetNumber(7).SetTitle("t").
		SetTargetBranch("main").SetState(provider.StateMerged).SetCommitCount(count)
	if merge != "" {
		b.SetMergeCommitSHA(merge)
	}
	if squash != "" {
		b.SetSquashCommitSHA(squash)
	}
	if head != "" {
		b.SetHeadSHA(head)
	}
	cr, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return cr
}

func commits(t *testing.T, shas ...string) []provider.Commit {
	t.Helper()
	out := make([]provider.Commit, 0, len(shas))
	for _, s := range shas {
		c, err := provider.NewCommit(s, "m", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func TestResolveLandingMergeCommit(t *testing.T) {
	m := sha("e")
	o := &fakeObjects{exists: map[string]bool{m: true}, parents: map[string][]string{m: {sha("0"), sha("a")}}}
	got, err := ResolveLanding(context.Background(), o, change(t, m, "", sha("a"), 2), commits(t, sha("1"), sha("a")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategyMerge || len(got.SHAs) != 1 || got.SHAs[0] != m || got.SourceSHA != m {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingSquashSingleCommit(t *testing.T) {
	s := sha("e")
	o := &fakeObjects{
		exists:   map[string]bool{s: true},
		parents:  map[string][]string{s: {sha("0")}},
		patchIDs: map[string]string{s: "pid-x", sha("1"): "pid-x"},
		walks:    map[string][]string{s: {s}},
	}
	got, err := ResolveLanding(context.Background(), o, change(t, "", s, sha("1"), 1), commits(t, sha("1")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategySquash || len(got.SHAs) != 1 || got.SHAs[0] != s {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingSquashOfManyCommits(t *testing.T) {
	// patch ids do not match the originals and the squash commit is non-empty
	s := sha("e")
	o := &fakeObjects{
		exists:   map[string]bool{s: true},
		parents:  map[string][]string{s: {sha("0")}, sha("d"): {sha("c")}},
		patchIDs: map[string]string{s: "pid-squash", sha("1"): "pid-1", sha("2"): "pid-2", sha("d"): "pid-d"},
		walks:    map[string][]string{s: {sha("d"), s}},
	}
	got, err := ResolveLanding(context.Background(), o, change(t, s, "", sha("2"), 2), commits(t, sha("1"), sha("2")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategySquash || len(got.SHAs) != 1 || got.SHAs[0] != s {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingRebase(t *testing.T) {
	last := sha("d")
	first := sha("c")
	o := &fakeObjects{
		exists:   map[string]bool{last: true},
		parents:  map[string][]string{last: {first}},
		patchIDs: map[string]string{first: "pid-1", last: "pid-2", sha("1"): "pid-1", sha("2"): "pid-2"},
		walks:    map[string][]string{last: {first, last}},
	}
	got, err := ResolveLanding(context.Background(), o, change(t, last, "", sha("2"), 2), commits(t, sha("1"), sha("2")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategyRebase || len(got.SHAs) != 2 || got.SHAs[0] != first || got.SHAs[1] != last {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingGitLabFastForwardFallsThroughToHead(t *testing.T) {
	head := sha("a")
	o := &fakeObjects{
		exists:   map[string]bool{head: true},
		parents:  map[string][]string{head: {sha("0")}},
		patchIDs: map[string]string{head: "pid-1", sha("1"): "pid-1"},
		walks:    map[string][]string{head: {head}},
	}
	// merge_commit_sha and squash_commit_sha are null: only HeadSHA is set
	got, err := ResolveLanding(context.Background(), o, change(t, "", "", head, 1), commits(t, sha("1")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Strategy != session.StrategySquash || got.SHAs[0] != head {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingSkipsAbsentCandidate(t *testing.T) {
	missing, head := sha("f"), sha("a")
	o := &fakeObjects{
		exists:   map[string]bool{head: true},
		parents:  map[string][]string{head: {sha("0")}},
		patchIDs: map[string]string{head: "pid-1", sha("1"): "pid-1"},
		walks:    map[string][]string{head: {head}},
	}
	got, err := ResolveLanding(context.Background(), o, change(t, missing, "", head, 1), commits(t, sha("1")))
	if err != nil {
		t.Fatal(err)
	}
	if got.SHAs[0] != head {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLandingErrors(t *testing.T) {
	// no candidate present at all -> MISSING_COMMITS naming the shas
	missing := sha("f")
	o := &fakeObjects{exists: map[string]bool{}}
	_, err := ResolveLanding(context.Background(), o, change(t, missing, "", "", 1), commits(t, sha("1")))
	var re *session.ReviewError
	if !errors.As(err, &re) || re.Code != session.CodeMissingCommits || !strings.Contains(re.Message, missing[:7]) {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, ErrNoCandidate) {
		t.Fatalf("missing candidate must wrap ErrNoCandidate: %v", err)
	}
	// root commit (no parents) -> BASE_UNDETERMINED
	root := sha("b")
	o2 := &fakeObjects{exists: map[string]bool{root: true}, parents: map[string][]string{root: {}}}
	_, err = ResolveLanding(context.Background(), o2, change(t, root, "", "", 1), commits(t, sha("1")))
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("root err = %v", err)
	}
	// octopus merge (3 parents) -> BASE_UNDETERMINED
	oct := sha("c")
	o3 := &fakeObjects{exists: map[string]bool{oct: true}, parents: map[string][]string{oct: {sha("0"), sha("1"), sha("2")}}}
	_, err = ResolveLanding(context.Background(), o3, change(t, oct, "", "", 1), commits(t, sha("1")))
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("octopus err = %v", err)
	}
	// multi-commit candidate whose diff is empty and whose patch ids do not match
	empty := sha("d")
	o4 := &fakeObjects{
		exists:   map[string]bool{empty: true},
		parents:  map[string][]string{empty: {sha("0")}},
		patchIDs: map[string]string{empty: "", sha("1"): "pid-1", sha("2"): "pid-2"},
		walks:    map[string][]string{empty: {empty}},
	}
	_, err = ResolveLanding(context.Background(), o4, change(t, empty, "", "", 2), commits(t, sha("1"), sha("2")))
	if !errors.As(err, &re) || re.Code != session.CodeBaseUndetermined {
		t.Fatalf("empty-diff err = %v", err)
	}
}
