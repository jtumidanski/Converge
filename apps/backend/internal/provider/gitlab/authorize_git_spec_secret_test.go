package gitlab

import (
	"strings"
	"testing"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

// TestAuthorizeGitDeclaresBasicAuthBlobAsSpecSecret pins the mechanism a
// hosted per-user token relies on for stderr redaction: AuthorizeGit must
// declare both the raw token and its base64 Basic-auth blob as spec secrets,
// mirroring the equivalent GitHub test. A hosted per-user token is unknown to
// the shared runner's Options.Secrets, and a bare blob with no
// "authorization:" prefix in stderr text is redacted by neither list
// otherwise.
func TestAuthorizeGitDeclaresBasicAuthBlobAsSpecSecret(t *testing.T) {
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

	// The real invariant: the raw token never appears verbatim in spec.Env
	// (it is base64'd as part of an Authorization: Basic header value), but
	// that base64 blob DOES appear there and must itself be declared as a
	// spec secret so stderr redaction can catch it too.
	blob := gitx.BasicAuthBlob(provider.GitUser(provider.KindGitLab), hostedToken)
	blobFound := false
	for _, kv := range spec.Env {
		if strings.Contains(kv, blob) {
			blobFound = true
		}
	}
	if !blobFound {
		t.Fatalf("precondition: base64 Basic blob not present in spec.Env: %v", spec.Env)
	}

	blobDeclared := false
	tokenDeclared := false
	for _, s := range spec.Secrets {
		if s == blob {
			blobDeclared = true
		}
		if s == hostedToken {
			tokenDeclared = true
		}
	}
	if !blobDeclared {
		t.Errorf("AuthorizeGit did not declare the base64 Basic blob as a spec secret: %v", spec.Secrets)
	}
	if !tokenDeclared {
		t.Errorf("AuthorizeGit did not declare the raw token as a spec secret: %v", spec.Secrets)
	}
}
