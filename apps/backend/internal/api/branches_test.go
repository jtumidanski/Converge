package api

import (
	"testing"

	"github.com/jtumidanski/converge/internal/provider"
)

func mustBranch(t *testing.T, name string) provider.Branch {
	t.Helper()
	b, err := provider.NewBranchBuilder().SetName(name).SetSHA("").Build()
	if err != nil {
		t.Fatalf("build branch %q: %v", name, err)
	}
	return b
}

func branchNames(items []provider.Branch) []string {
	out := make([]string, len(items))
	for i, b := range items {
		out[i] = b.Name()
	}
	return out
}

func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDefaultFirstMovesDefaultToFront(t *testing.T) {
	items := []provider.Branch{
		mustBranch(t, "feature/a"),
		mustBranch(t, "release/b"),
		mustBranch(t, "main"),
		mustBranch(t, "feature/c"),
	}
	out := defaultFirst(items, "main", 1)
	want := []string{"main", "feature/a", "release/b", "feature/c"}
	if got := branchNames(out); !equalNames(got, want) {
		t.Errorf("defaultFirst = %v, want %v", got, want)
	}
}

func TestDefaultFirstIgnoresPagesAfterFirst(t *testing.T) {
	items := []provider.Branch{mustBranch(t, "feature/a"), mustBranch(t, "main")}
	out := defaultFirst(items, "main", 2)
	want := []string{"feature/a", "main"}
	if got := branchNames(out); !equalNames(got, want) {
		t.Errorf("defaultFirst (page 2) = %v, want untouched %v", got, want)
	}
}

func TestDefaultFirstIgnoresEmptyDefault(t *testing.T) {
	items := []provider.Branch{mustBranch(t, "feature/a"), mustBranch(t, "main")}
	out := defaultFirst(items, "", 1)
	want := []string{"feature/a", "main"}
	if got := branchNames(out); !equalNames(got, want) {
		t.Errorf("defaultFirst (no default) = %v, want untouched %v", got, want)
	}
}

func TestDefaultFirstIgnoresShortSlices(t *testing.T) {
	items := []provider.Branch{mustBranch(t, "main")}
	out := defaultFirst(items, "main", 1)
	want := []string{"main"}
	if got := branchNames(out); !equalNames(got, want) {
		t.Errorf("defaultFirst (single item) = %v, want untouched %v", got, want)
	}
}

func TestDefaultFirstAlreadyAtFront(t *testing.T) {
	items := []provider.Branch{mustBranch(t, "main"), mustBranch(t, "feature/a")}
	out := defaultFirst(items, "main", 1)
	want := []string{"main", "feature/a"}
	if got := branchNames(out); !equalNames(got, want) {
		t.Errorf("defaultFirst (already first) = %v, want untouched %v", got, want)
	}
}
