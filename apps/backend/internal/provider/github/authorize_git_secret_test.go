package github

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

// hostedToken stands in for a per-user provider token in hosted mode. Such a
// token lives encrypted in the database and is decrypted per request, so it is
// never a member of config.Config.Secrets() and can never be part of the one
// shared ExecRunner's Options.Secrets, which is fixed at construction.
const hostedToken = "ghp-hostedusertoken0123456789"

// TestAuthorizeGitRedactsHostedTokenFromGitStderr is the defence-in-depth
// guard for per-user tokens in git's own diagnostics. There is no known
// production path by which a *raw* hosted token reaches git stderr — it
// reaches git only through GIT_CONFIG_*, where it exists as base64 inside an
// Authorization: Basic blob, never as the token string itself — so this is not
// a reproduction of an observed leak. What it pins is that a hosted token is
// covered by redaction at all: the runner is built the way app.New builds it
// for a hosted deployment (Options.Secrets empty, because no provider token is
// configured through the environment), so the only declaration of the secret
// is the one AuthorizeGit makes on the Spec it authorises. If that declaration
// were dropped, nothing else would cover the value.
//
// The token is placed in argv by this test's own Spec.Args purely to force git
// to echo it on stderr. Production never puts a token in argv; this
// manufactures the general case of a token value surfacing in git's
// diagnostics, which is what the redaction exists for.
func TestAuthorizeGitRedactsHostedTokenFromGitStderr(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	runner, err := gitx.NewExecRunner(log, gitx.Options{CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	c := New("gh", "GitHub", "https://api.github.com", config.NewSecret(hostedToken), nil, nil)
	repo, err := provider.NewRepositoryBuilder().
		SetProviderID("gh").SetFullName("atlas/server").
		SetCloneURL("https://github.com/atlas/server.git").Build()
	if err != nil {
		t.Fatalf("build repository: %v", err)
	}

	spec := gitx.Spec{
		Args:     []string{"ls-remote", hostedToken},
		Dir:      t.TempDir(),
		Category: gitx.CategoryQuery,
		Repo:     repo.FullName(),
	}
	if err := c.AuthorizeGit(repo, &spec); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	res, _ := runner.Run(context.Background(), spec)
	if !strings.Contains(string(res.Stderr), hostedToken) {
		t.Fatalf("test precondition broken: git stderr does not echo the token: %q", res.Stderr)
	}
	if strings.Contains(logBuf.String(), hostedToken) {
		t.Errorf("hosted per-user token written to the log in clear: %s", logBuf.String())
	}
}

// TestAuthorizeGitRedactsBareBasicBlobFromGitStderr proves that the base64
// "Basic" blob itself — not just the raw token — is redacted from git stderr
// even when it appears with no "authorization:"/"private-token:" prefix
// alongside it. Redact's headerRe only fires when that prefix is present in
// the same text; a bare blob is covered only if AuthorizeGit declares the
// blob as a spec secret in its own right.
func TestAuthorizeGitRedactsBareBasicBlobFromGitStderr(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	runner, err := gitx.NewExecRunner(log, gitx.Options{CommandTimeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	c := New("gh", "GitHub", "https://api.github.com", config.NewSecret(hostedToken), nil, nil)
	repo, err := provider.NewRepositoryBuilder().
		SetProviderID("gh").SetFullName("atlas/server").
		SetCloneURL("https://github.com/atlas/server.git").Build()
	if err != nil {
		t.Fatalf("build repository: %v", err)
	}

	// The exact blob CredentialEnv would embed in the Authorization header.
	blob := gitx.BasicAuthBlob(provider.GitUser(provider.KindGitHub), hostedToken)

	// Place the bare blob in argv, with no "Authorization:" prefix anywhere in
	// the command, so git echoes it on stderr with no header text around it.
	spec := gitx.Spec{
		Args:     []string{"ls-remote", blob},
		Dir:      t.TempDir(),
		Category: gitx.CategoryQuery,
		Repo:     repo.FullName(),
	}
	if err := c.AuthorizeGit(repo, &spec); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	res, _ := runner.Run(context.Background(), spec)
	if !strings.Contains(string(res.Stderr), blob) {
		t.Fatalf("test precondition broken: git stderr does not echo the bare blob: %q", res.Stderr)
	}
	if strings.Contains(logBuf.String(), blob) {
		t.Errorf("bare base64 Basic blob written to the log in clear: %s", logBuf.String())
	}
}
