package gitx

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runnerWithLog builds a runner whose log output is captured, so a test can
// assert on exactly what was written.
func runnerWithLog(t *testing.T, secrets []string) (*ExecRunner, *bytes.Buffer) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r, err := NewExecRunner(log, Options{CommandTimeout: 30 * time.Second, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, &buf
}

// failing makes git exit non-zero with the given value echoed on its stderr,
// which is the only stderr the runner logs.
func failing(echo string, secrets []string) Spec {
	return Spec{Args: []string{"ls-remote", echo}, Category: CategoryQuery, Secrets: secrets}
}

// A spec-scoped secret is the only channel a per-user hosted credential has:
// Options.Secrets is fixed when the single shared runner is constructed.
func TestRunRedactsSpecScopedSecret(t *testing.T) {
	r, buf := runnerWithLog(t, nil)
	const specSecret = "spec-scoped-token-aaa"
	res, _ := r.Run(context.Background(), failing(specSecret, []string{specSecret}))
	if !strings.Contains(string(res.Stderr), specSecret) {
		t.Fatalf("precondition: git stderr lacks the secret: %q", res.Stderr)
	}
	if strings.Contains(buf.String(), specSecret) {
		t.Errorf("spec-scoped secret logged in clear: %s", buf)
	}
	if !strings.Contains(buf.String(), "[redacted]") {
		t.Errorf("expected a redaction marker in the log: %s", buf)
	}
}

// Both lists apply: adding spec secrets must not displace the runner-wide list
// that holds env-configured tokens and the master key.
func TestRunRedactsRunnerAndSpecSecretsTogether(t *testing.T) {
	const runnerSecret = "runner-wide-token-bbb"
	const specSecret = "spec-scoped-token-ccc"
	r, buf := runnerWithLog(t, []string{runnerSecret})

	if _, err := r.Run(context.Background(), failing(runnerSecret, []string{specSecret})); err == nil {
		t.Fatal("expected git to fail")
	}
	if _, err := r.Run(context.Background(), failing(specSecret, []string{specSecret})); err == nil {
		t.Fatal("expected git to fail")
	}
	if strings.Contains(buf.String(), runnerSecret) {
		t.Errorf("runner-wide secret logged in clear: %s", buf)
	}
	if strings.Contains(buf.String(), specSecret) {
		t.Errorf("spec-scoped secret logged in clear: %s", buf)
	}
}

// A spec that declares nothing still gets the runner-wide redaction, which is
// the behaviour every pre-existing caller depends on.
func TestRunRedactsRunnerSecretWithoutSpecSecrets(t *testing.T) {
	const runnerSecret = "runner-wide-token-ddd"
	r, buf := runnerWithLog(t, []string{runnerSecret})
	if _, err := r.Run(context.Background(), failing(runnerSecret, nil)); err == nil {
		t.Fatal("expected git to fail")
	}
	if strings.Contains(buf.String(), runnerSecret) {
		t.Errorf("runner-wide secret logged in clear: %s", buf)
	}
}
