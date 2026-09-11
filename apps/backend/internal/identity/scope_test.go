package identity_test

import (
	"testing"

	"github.com/jtumidanski/converge/internal/identity"
)

// TestMatchesTruthTable pins the asymmetry in design §3: a standalone scope
// sees everything, including owned records (so an existing deployment that
// never sets CONVERGE_MODE keeps listing every session); a hosted scope sees
// only an exact owner match, so an unowned record is invisible to everyone
// (FR-6.3, FR-6.4).
func TestMatchesTruthTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		scope identity.Scope
		owner string
		want  bool
	}{
		{"standalone sees unowned", identity.Standalone(), "", true},
		{"standalone sees owned", identity.Standalone(), "abc123", true},
		{"hosted sees own", identity.ForUser("abc123"), "abc123", true},
		{"hosted cannot see foreign", identity.ForUser("abc123"), "def456", false},
		{"hosted cannot see unowned", identity.ForUser("abc123"), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.scope.Matches(tc.owner); got != tc.want {
				t.Fatalf("Matches(%q) = %v, want %v", tc.owner, got, tc.want)
			}
		})
	}
}

func TestScopeAccessors(t *testing.T) {
	t.Parallel()
	if s := identity.Standalone(); s.IsScoped() || s.UserID() != "" {
		t.Fatalf("standalone scope: IsScoped=%v UserID=%q, want false and empty", s.IsScoped(), s.UserID())
	}
	if s := identity.ForUser("abc123"); !s.IsScoped() || s.UserID() != "abc123" {
		t.Fatalf("hosted scope: IsScoped=%v UserID=%q, want true and abc123", s.IsScoped(), s.UserID())
	}
}

// TestZeroValueIsStandalone proves the safe default: a Scope built by a struct
// literal (the zero value, which is all an outside package can construct
// because userID is unexported) behaves as standalone, not as "some user".
func TestZeroValueIsStandalone(t *testing.T) {
	t.Parallel()
	var s identity.Scope
	if s.IsScoped() {
		t.Fatal("zero Scope reported as scoped")
	}
	if !s.Matches("anything") {
		t.Fatal("zero Scope did not match an owned record")
	}
}
