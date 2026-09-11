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
	// The real invariant: the raw token never appears verbatim in spec.Env
	// (it is base64'd as part of an Authorization: Basic header value), but
	// that base64 blob DOES appear there and must itself be declared as a
	// spec secret so stderr redaction can catch it too.
	blob := gitx.BasicAuthBlob(provider.GitUser(provider.KindGitHub), hostedToken)
	blobFound := false
	for _, kv := range spec.Env {
		if strings.Contains(kv, hostedToken) {
			t.Errorf("raw token reached git environment verbatim: %q", kv)
		}
		if strings.Contains(kv, blob) {
			blobFound = true
		}
	}
	if !blobFound {
		t.Fatalf("precondition: base64 Basic blob not present in spec.Env: %v", spec.Env)
	}
	blobDeclared := false
	for _, s := range spec.Secrets {
		if s == blob {
			blobDeclared = true
		}
	}
	if !blobDeclared {
		t.Errorf("AuthorizeGit did not declare the base64 Basic blob as a spec secret: %v", spec.Secrets)
	}
}
