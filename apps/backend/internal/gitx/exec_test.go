package gitx

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func newRunner(t *testing.T) *ExecRunner {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	r, err := NewExecRunner(slog.New(slog.NewTextHandler(os.Stderr, nil)), Options{CloneTimeout: time.Minute, CommandTimeout: 10 * time.Second, Secrets: []string{"s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestExecRunnerRunsGitWithIsolatedEnv(t *testing.T) {
	r := newRunner(t)
	res, err := r.Run(context.Background(), Spec{Args: []string{"config", "--show-origin", "--get", "core.hooksPath"}, Category: CategoryQuery})
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, res.Stderr)
	}
	if !strings.Contains(string(res.Stdout), r.hooksDir) {
		t.Errorf("hooksPath not applied: %s", res.Stdout)
	}
	res, err = r.Run(context.Background(), Spec{Args: []string{"var", "GIT_COMMITTER_IDENT"}, Category: CategoryQuery})
	if err != nil || !strings.HasPrefix(string(res.Stdout), "Converge Review <converge@localhost>") {
		t.Errorf("ident: err=%v out=%s", err, res.Stdout)
	}
}

func TestExecRunnerVersion(t *testing.T) {
	r := newRunner(t)
	v, err := r.Version(context.Background())
	if err != nil || !strings.Contains(v, ".") {
		t.Fatalf("v=%q err=%v", v, err)
	}
}

func TestExecRunnerExitErrorAndStdin(t *testing.T) {
	r := newRunner(t)
	_, err := r.Run(context.Background(), Spec{Args: []string{"rev-parse", "--verify", "definitely-not-a-ref"}, Dir: t.TempDir(), Category: CategoryQuery})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Result.ExitCode == 0 {
		t.Fatalf("want ExitError, got %v", err)
	}
	res, err := r.Run(context.Background(), Spec{Args: []string{"hash-object", "--stdin"}, Stdin: strings.NewReader("hello\n"), Category: CategoryQuery})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatalf("stdin not wired: %v %s", err, res.Stdout)
	}
}

func TestExecRunnerTimeout(t *testing.T) {
	r := newRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := r.Run(ctx, Spec{Args: []string{"cat-file", "--batch"}, Stdin: blockingReader{}, Category: CategoryQuery, Timeout: time.Minute})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline error, got %v", err)
	}
}

type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }
