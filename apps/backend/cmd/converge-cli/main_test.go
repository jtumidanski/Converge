package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/app"
	"github.com/jtumidanski/converge/internal/buildinfo"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/testutil"
	"github.com/jtumidanski/converge/internal/workspace"
)

func TestParseChanges(t *testing.T) {
	got, err := parseChanges("421,427, 435")
	if err != nil || len(got) != 3 || got[0] != 421 || got[2] != 435 {
		t.Fatalf("got %v err %v", got, err)
	}
	for _, bad := range []string{"", "a,b", "1,,2", "0", "-1"} {
		if _, err := parseChanges(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		s    session.Status
		code session.Code
		want int
	}{
		{"ready", session.StatusReady, "", 0},
		{"conflict", session.StatusConflicted, session.CodeConflict, 2},
		{"not merged", session.StatusFailed, session.CodeNotMerged, 3},
		{"targets", session.StatusFailed, session.CodeIncompatibleTargets, 3},
		{"not on base", session.StatusFailed, session.CodeNotOnBaseBranch, 3},
		{"missing commits", session.StatusFailed, session.CodeMissingCommits, 3},
		{"base undetermined", session.StatusFailed, session.CodeBaseUndetermined, 3},
		{"provider auth", session.StatusFailed, session.CodeProviderAuth, 4},
		{"provider unavailable", session.StatusFailed, session.CodeProviderUnavailable, 4},
		{"repository", session.StatusFailed, session.CodeRepositoryUnavailable, 4},
		{"git", session.StatusFailed, session.CodeGitFailure, 1},
		{"interrupted", session.StatusFailed, session.CodeInterrupted, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.s, tc.code); got != tc.want {
				t.Errorf("got %d want %d", got, tc.want)
			}
		})
	}
	if got := exitCodeForError(&review.InputError{Code: review.CodeInvalidChanges}); got != 3 {
		t.Errorf("input error = %d", got)
	}
	if got := exitCodeForError(&session.ReviewError{Code: session.CodeProviderAuth}); got != 4 {
		t.Errorf("review error = %d", got)
	}
	if got := exitCodeForError(errors.New("boom")); got != 1 {
		t.Errorf("plain error = %d", got)
	}
}

// newTestApp wires a real *app.App against a fake provider ("gh",
// provider.KindGitHub, matching --provider gh) and a real local git
// repository, exactly the way internal/review's own service tests do. This
// lets run()'s "build" path be driven end to end -- --out default,
// metadata.json, the combined diff copy, --cleanup -- without config.Load,
// os.Environ, or a real GitHub/GitLab HTTP call.
func newTestApp(t *testing.T) *app.App {
	t.Helper()
	src := testutil.NewRepo(t)
	src.Commit("base.txt", "base\n", "base commit")
	src.Branch("feat/a")
	src.Commit("feature.txt", "feature\n", "add feature")
	src.Checkout("main")
	merge := src.MergeNoFF("feat/a", "merge feature")
	src.Push()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner, err := gitx.NewExecRunner(log, gitx.Options{AllowFileProtocol: true, CommandTimeout: 30 * time.Second, CloneTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	locks := &gitx.LockMap{}
	mirrors := mirror.New(t.TempDir(), runner, locks, log)
	ws, err := workspace.New(t.TempDir(), runner, locks, log)
	if err != nil {
		t.Fatal(err)
	}

	repo, err := provider.NewRepositoryBuilder().SetProviderID("gh").SetFullName("acme/widgets").
		SetDefaultBranch("main").SetCloneURL(src.CloneURL()).Build()
	if err != nil {
		t.Fatal(err)
	}
	p := fake.New("gh", provider.KindGitHub)
	p.AddRepository(repo)
	commit, err := provider.NewCommit(src.RevParse("feat/a"), "add feature", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cr, err := provider.NewChangeRequestBuilder().SetProviderID("gh").SetRepository(repo).SetNumber(1).
		SetTitle("Add feature").SetAuthor("dev").SetTargetBranch("main").SetState(provider.StateMerged).
		SetMergedAt(time.Now()).SetMergeCommitSHA(merge).SetCommits([]provider.Commit{commit}).Build()
	if err != nil {
		t.Fatal(err)
	}
	p.AddChange(cr)
	registry := provider.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}

	cleaner := review.NewCleaner(mirrors, ws, log)
	store := session.NewStore(ws.Root(), 24*time.Hour, cleaner, log, time.Now)
	svc := review.NewService(review.Deps{
		Providers: provider.NewStaticResolver(registry), Mirrors: mirrors, Workspaces: ws, Store: store,
		Applicator: review.NewCherryPickApplicator(runner, log), Runner: runner, Log: log,
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 2, Now: time.Now,
	})
	return &app.App{Log: log, Runner: runner, Registry: registry, Mirrors: mirrors, Workspaces: ws, Store: store, Service: svc}
}

// withTestApp makes run() use a (test bench so this doesn't leak between
// tests) fixed *app.App instead of calling app.New, and restores the
// production seam on cleanup.
func withTestApp(t *testing.T, a *app.App) {
	t.Helper()
	orig := newApp
	newApp = func(_ context.Context, _ []string) (*app.App, error) { return a, nil }
	t.Cleanup(func() { newApp = orig })
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != buildinfo.Version {
		t.Fatalf("stdout = %q, want %q", got, buildinfo.Version)
	}
}

func TestRunUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"frobnicate"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr missing usage: %s", stderr.String())
	}
}

func TestRunNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr missing usage: %s", stderr.String())
	}
}

// TestRunBuildDefaultOutWritesMetadataAndDiff drives the whole "build"
// pipeline through run() with no --out flag, and checks the three things
// Important 4 flagged as uncommitted: the default output directory
// (./converge-out/<session-id>), metadata.json, and the combined diff copy.
func TestRunBuildDefaultOutWritesMetadataAndDiff(t *testing.T) {
	withTestApp(t, newTestApp(t))
	cwd := t.TempDir()
	t.Chdir(cwd)

	var stdout, stderr bytes.Buffer
	code := run([]string{"build", "--provider", "gh", "--repo", "acme/widgets", "--changes", "1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}

	var record session.Record
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatalf("stdout is not the session record JSON: %v\n%s", err, stdout.String())
	}
	if record.Status != session.StatusReady {
		t.Fatalf("record status = %s, want READY", record.Status)
	}

	dir := filepath.Join(cwd, "converge-out", record.ID)
	metaBytes, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatalf("metadata.json not written at default --out path: %v", err)
	}
	var onDisk session.Record
	if err := json.Unmarshal(metaBytes, &onDisk); err != nil || onDisk.ID != record.ID {
		t.Fatalf("metadata.json content wrong: %v %s", err, metaBytes)
	}
	diffBytes, err := os.ReadFile(filepath.Join(dir, review.CombinedDiffFile))
	if err != nil {
		t.Fatalf("combined.diff not written at default --out path: %v", err)
	}
	if !strings.Contains(string(diffBytes), "feature.txt") {
		t.Fatalf("combined.diff missing expected content: %s", diffBytes)
	}
}

// TestRunBuildCleanupRemovesWorkspace proves --cleanup actually invokes
// Service.Finish: the session's workspace directory is gone and the session
// is FINISHED afterward, without affecting the exit code produced for the
// build itself.
func TestRunBuildCleanupRemovesWorkspace(t *testing.T) {
	a := newTestApp(t)
	withTestApp(t, a)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"build", "--provider", "gh", "--repo", "acme/widgets", "--changes", "1", "--out", outDir, "--cleanup"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	var record session.Record
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatalf("stdout is not the session record JSON: %v", err)
	}
	got, ok := a.Store.Get(record.ID)
	if !ok || got.Status() != session.StatusFinished {
		t.Fatalf("session not finished after --cleanup: ok=%v status=%v", ok, got.Status())
	}
	if _, err := os.Stat(a.Workspaces.SessionDir(record.ID)); !os.IsNotExist(err) {
		t.Fatalf("--cleanup did not remove the session workspace: err=%v", err)
	}
	// The artifacts already written to --out must survive cleanup.
	if _, err := os.Stat(filepath.Join(outDir, "metadata.json")); err != nil {
		t.Fatalf("metadata.json missing after cleanup: %v", err)
	}
}

// TestRunBuildReadySessionMissingCombinedDiffIsFailure drives the exact bug
// this fix round closes: a READY session whose combined.diff has vanished
// (Important 1). The Now hook deletes combined.diff the instant the build
// pipeline writes it -- Service.Build runs synchronously inside run(), so by
// the time run() calls CombinedDiffPath the file is already gone, without
// any sleep or goroutine race.
func TestRunBuildReadySessionMissingCombinedDiffIsFailure(t *testing.T) {
	a := newTestApp(t)
	vanish := func() time.Time {
		matches, _ := filepath.Glob(filepath.Join(a.Workspaces.Root(), "*", review.CombinedDiffFile))
		for _, m := range matches {
			_ = os.Remove(m)
		}
		return time.Now()
	}
	a.Service = review.NewService(review.Deps{
		Providers: provider.NewStaticResolver(a.Registry), Mirrors: a.Mirrors, Workspaces: a.Workspaces, Store: a.Store,
		Applicator: review.NewCherryPickApplicator(a.Runner, a.Log), Runner: a.Runner, Log: a.Log,
		SessionTTL: 24 * time.Hour, MaxConcurrentBuilds: 2, Now: vanish,
	})
	withTestApp(t, a)
	outDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"build", "--provider", "gh", "--repo", "acme/widgets", "--changes", "1", "--out", outDir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "combined diff") {
		t.Fatalf("stderr missing combined-diff failure message: %q", stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("stdout metadata JSON must still be printed even when the diff copy fails")
	}
	var record session.Record
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil || record.Status != session.StatusReady {
		t.Fatalf("stdout record wrong: err=%v record=%+v", err, record)
	}
	if _, err := os.Stat(filepath.Join(outDir, review.CombinedDiffFile)); !os.IsNotExist(err) {
		t.Fatalf("combined.diff should not exist in --out: err=%v", err)
	}
}
