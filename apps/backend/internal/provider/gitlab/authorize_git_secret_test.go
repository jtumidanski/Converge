package gitlab

import (
	"strings"
	"testing"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

// hostedToken stands in for a per-user provider token in hosted mode: it lives
// encrypted in the database, is decrypted per request, and is therefore never
// part of the shared ExecRunner's Options.Secrets.
const hostedToken = "glpat-hostedusertoken0123456789"

func TestAuthorizeGitDeclaresTokenAsSpecSecret(t *testing.T) {
	c := New("gl", "GitLab", "https://gitlab.com/api/v4", config.NewSecret(hostedToken), nil)
	repo, err := provider.NewRepositoryBuilder().
		SetProviderID("gl").SetFullName("atlas/server").
		SetCloneURL("https://gitlab.com/atlas/server.git").Build()
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
