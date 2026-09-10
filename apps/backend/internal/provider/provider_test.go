package provider

import "testing"

func TestGitUser(t *testing.T) {
	if got := GitUser(KindGitHub); got != "x-access-token" {
		t.Errorf("GitUser(KindGitHub) = %q, want %q", got, "x-access-token")
	}
	if got := GitUser(KindGitLab); got != "oauth2" {
		t.Errorf("GitUser(KindGitLab) = %q, want %q", got, "oauth2")
	}
}
