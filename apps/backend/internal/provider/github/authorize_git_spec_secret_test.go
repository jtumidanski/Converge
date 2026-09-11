package github

import (
	"strings"
	"testing"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

// TestAuthorizeGitDeclaresTokenAsSpecSecret pins the mechanism the test above
// relies on, so a refactor that drops the declaration fails loudly rather than
// only through the end-to-end assertion.
func TestAuthorizeGitDeclaresTokenAsSpecSecret(t *testing.T) {
	c := New("gh", "GitHub", "https://api.github.com", config.NewSecret(hostedToken), nil, nil)
	repo, err := provider.NewRepositoryBuilder().
		SetProviderID("gh").SetFullName("atlas/server").
		SetCloneURL("https://github.com/atlas/server.git").Build()
	if err != nil {
		t.Fatalf("build repository: %v", err)
	}

	var spec gitx.Spec
	if err := c.AuthorizeGit(repo, &spec); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	found := false
	for _, s := range spec.Secrets {
		if s == hostedToken {
			found = true
		}
	}
	if !found {
		t.Errorf("AuthorizeGit did not declare the token as a spec secret: %v", spec.Secrets)
	}
	for _, kv := range spec.Env {
		if strings.Contains(kv, hostedToken) {
			t.Errorf("raw token reached git environment verbatim: %q", kv)
		}
	}
}
