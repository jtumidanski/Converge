package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	identityName  = "Converge Review"
	identityEmail = "converge@localhost"
	stderrLogCap  = 2048
)

// Options configures an ExecRunner.
type Options struct {
	CloneTimeout   time.Duration // default for clone/fetch categories
	CommandTimeout time.Duration // default for everything else
	Secrets        []string      // scrubbed from logged stderr

	// AllowFileProtocol adds `-c protocol.file.allow=always` to every git
	// invocation. Git disabled that transport by default for CVE-2022-39253
	// (a malicious repository can make a file-protocol clone execute code from
	// the source tree), so production MUST leave this false: the server only
	// ever talks to http(s) remotes. It exists solely for the test harness,
	// which clones `file://` fixtures through this same runner.
	AllowFileProtocol bool
}

// ExecRunner runs the real git binary with an isolated environment.
type ExecRunner struct {
	log      *slog.Logger
	opts     Options
	homeDir  string
	hooksDir string
	gitPath  string
}

// NewExecRunner locates git and prepares private HOME and hooks directories.
func NewExecRunner(log *slog.Logger, opts Options) (*ExecRunner, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git is not on PATH: %w", err)
	}
	if opts.CloneTimeout <= 0 {
		opts.CloneTimeout = 10 * time.Minute
	}
	if opts.CommandTimeout <= 0 {
		opts.CommandTimeout = 2 * time.Minute
	}
	base, err := os.MkdirTemp("", "converge-git-")
	if err != nil {
		return nil, fmt.Errorf("create git env dir: %w", err)
	}
	hooks := filepath.Join(base, "hooks")
	home := filepath.Join(base, "home")
	for _, d := range []string{hooks, home} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}
	return &ExecRunner{log: log, opts: opts, homeDir: home, hooksDir: hooks, gitPath: gitPath}, nil
}

// Close removes the private directories.
func (r *ExecRunner) Close() error { return os.RemoveAll(filepath.Dir(r.homeDir)) }

// Version returns the output of git --version, e.g. "2.49.0".
func (r *ExecRunner) Version(ctx context.Context) (string, error) {
	res, err := r.Run(ctx, Spec{Args: []string{"--version"}, Category: CategoryQuery})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.TrimPrefix(string(res.Stdout), "git version ")), nil
}

func (r *ExecRunner) baseEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.homeDir,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"LC_ALL=C",
		"GIT_AUTHOR_NAME=" + identityName,
		"GIT_AUTHOR_EMAIL=" + identityEmail,
		"GIT_COMMITTER_NAME=" + identityName,
		"GIT_COMMITTER_EMAIL=" + identityEmail,
	}
}

func (r *ExecRunner) timeoutFor(s Spec) time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	if s.Category == CategoryClone || s.Category == CategoryFetch {
		return r.opts.CloneTimeout
	}
	return r.opts.CommandTimeout
}

// secretsFor is the union of the runner-wide secrets and the ones this
// invocation declared. Both lists must be applied: the runner-wide list is the
// only place environment-configured tokens and the master key appear, and the
// spec-scoped list is the only place a per-user hosted token can appear.
func (r *ExecRunner) secretsFor(s Spec) []string {
	if len(s.Secrets) == 0 {
		return r.opts.Secrets
	}
	out := make([]string, 0, len(r.opts.Secrets)+len(s.Secrets))
	out = append(out, r.opts.Secrets...)
	out = append(out, s.Secrets...)
	return out
}

// Run executes git with the fixed -c prefix and isolated environment.
func (r *ExecRunner) Run(ctx context.Context, s Spec) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeoutFor(s))
	defer cancel()
	prefix := []string{
		"-c", "core.hooksPath=" + r.hooksDir,
		"-c", "commit.gpgsign=false",
	}
	if r.opts.AllowFileProtocol {
		prefix = append(prefix, "-c", "protocol.file.allow=always")
	}
	args := append(prefix, s.Args...)
	cmd := exec.CommandContext(ctx, r.gitPath, args...)
	cmd.Dir = s.Dir
	cmd.Env = append(r.baseEnv(), s.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 2 * time.Second

	// Stdin is wired through an explicit pipe rather than cmd.Stdin so that a
	// caller-supplied Reader that blocks forever cannot wedge Wait(): per the
	// os/exec docs, Wait always blocks on an in-flight Read from cmd.Stdin
	// regardless of WaitDelay. Copying in a detached goroutine lets Wait return
	// as soon as the process exits; the goroutine leaks harmlessly if the
	// Reader never unblocks.
	var stdinPipe io.WriteCloser
	if s.Stdin != nil {
		var err error
		stdinPipe, err = cmd.StdinPipe()
		if err != nil {
			return Result{}, fmt.Errorf("git %s: stdin pipe: %w", s.Category, err)
		}
	}

	start := time.Now()
	if startErr := cmd.Start(); startErr != nil {
		return Result{}, fmt.Errorf("git %s: %w", s.Category, startErr)
	}
	if stdinPipe != nil {
		go func() {
			_, _ = io.Copy(stdinPipe, s.Stdin)
			_ = stdinPipe.Close()
		}()
	}
	err := cmd.Wait()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Duration: time.Since(start)}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	attrs := []any{
		slog.String("git.category", string(s.Category)),
		slog.String("repository", s.Repo),
		slog.String("session", s.Session),
		slog.Int("exit_code", res.ExitCode),
		slog.Int64("duration_ms", res.Duration.Milliseconds()),
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		r.log.Warn("git timed out", append(attrs, slog.String("outcome", "timeout"))...)
		return res, fmt.Errorf("git %s: %w", s.Category, ctxErr)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderrLog := res.Stderr
			if len(stderrLog) > stderrLogCap {
				stderrLog = stderrLog[:stderrLogCap]
			}
			r.log.Debug("git exited non-zero", append(attrs, slog.String("outcome", "exit"), slog.String("stderr", string(Redact(stderrLog, r.secretsFor(s)))))...)
			return res, &ExitError{Category: s.Category, Result: res}
		}
		r.log.Error("git failed to start", append(attrs, slog.String("outcome", "error"))...)
		return res, fmt.Errorf("git %s: %w", s.Category, err)
	}
	r.log.Debug("git ok", append(attrs, slog.String("outcome", "ok"))...)
	return res, nil
}
