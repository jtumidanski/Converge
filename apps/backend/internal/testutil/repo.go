// Package testutil scripts local git repositories for tests. It uses the real
// git binary directly (not gitx) so harness bugs cannot mask runner bugs.
package testutil

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a bare "origin" plus a working clone.
type Repo struct {
	T    *testing.T
	Work string
	Bare string
	home string
}

// NewRepo creates the pair with one initial commit on main.
func NewRepo(t *testing.T) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	r := &Repo{T: t, Work: filepath.Join(root, "work"), Bare: filepath.Join(root, "origin.git"), home: filepath.Join(root, "home")}
	if err := os.MkdirAll(r.home, 0o700); err != nil {
		t.Fatal(err)
	}
	r.run("", "init", "--bare", "--initial-branch=main", r.Bare)
	r.run("", "clone", r.Bare, r.Work)
	r.Git("checkout", "-b", "main")
	r.Commit("README.md", "# test\n", "initial")
	r.Push()
	return r
}

func (r *Repo) env() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=author@example.com",
		"GIT_COMMITTER_NAME=Test Committer", "GIT_COMMITTER_EMAIL=committer@example.com",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
		"LC_ALL=C",
	}
}

func (r *Repo) run(dir string, args ...string) string {
	r.T.Helper()
	full := append([]string{"-c", "protocol.file.allow=always", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...) //nolint:gosec // testutil intentionally shells out to the real git binary with fixed args
	cmd.Dir = dir
	cmd.Env = r.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		r.T.Fatalf("git %v: %v\n%s", args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

// Git runs git in the working clone.
func (r *Repo) Git(args ...string) string { r.T.Helper(); return r.run(r.Work, args...) }

// Commit writes file with content and commits it.
func (r *Repo) Commit(file, content, message string) string {
	r.T.Helper()
	p := filepath.Join(r.Work, file)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		r.T.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil { //nolint:gosec // test fixture file, not sensitive
		r.T.Fatal(err)
	}
	r.Git("add", "--", file)
	r.Git("commit", "--allow-empty", "-m", message)
	return r.Head()
}

// Branch creates and checks out a branch from HEAD.
func (r *Repo) Branch(name string) { r.T.Helper(); r.Git("checkout", "-b", name) }

// Checkout switches branches.
func (r *Repo) Checkout(name string) { r.T.Helper(); r.Git("checkout", name) }

// MergeNoFF merges branch into the current branch with a merge commit.
func (r *Repo) MergeNoFF(branch, message string) string {
	r.T.Helper()
	r.Git("merge", "--no-ff", "-m", message, branch)
	return r.Head()
}

// Squash squash-merges branch into the current branch as one commit.
func (r *Repo) Squash(branch, message string) string {
	r.T.Helper()
	r.Git("merge", "--squash", branch)
	r.Git("commit", "-m", message)
	return r.Head()
}

// BranchCommits lists commits on branch not on main, oldest-first.
func (r *Repo) BranchCommits(branch string) []string {
	r.T.Helper()
	out := r.Git("rev-list", "--reverse", "main.."+branch)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// Rebase rebases branch onto main and fast-forwards main. Returns new SHAs oldest-first.
func (r *Repo) Rebase(branch string) []string {
	r.T.Helper()
	r.Git("checkout", branch)
	r.Git("rebase", "main")
	shas := r.BranchCommits(branch)
	r.Git("checkout", "main")
	r.Git("merge", "--ff-only", branch)
	return shas
}

// Push pushes every branch to the bare origin.
func (r *Repo) Push() { r.T.Helper(); r.Git("push", "--all", "--force", "origin") }

// RevParse resolves a revision.
func (r *Repo) RevParse(rev string) string {
	r.T.Helper()
	return r.Git("rev-parse", "--verify", rev+"^{commit}")
}

// Head returns HEAD's SHA.
func (r *Repo) Head() string { r.T.Helper(); return r.RevParse("HEAD") }

// CloneURL returns a file:// URL for the bare repository.
func (r *Repo) CloneURL() string { return "file://" + r.Bare }

// FileContent returns rev:path or "" when absent.
func (r *Repo) FileContent(rev, path string) string {
	cmd := exec.Command("git", "show", rev+":"+path) //nolint:gosec // testutil intentionally shells out to the real git binary with fixed args
	cmd.Dir = r.Work
	cmd.Env = r.env()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}
