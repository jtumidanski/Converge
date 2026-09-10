package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/app"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/ui"
)

// countSweeperGoroutines dumps all live goroutine stacks and counts how many
// are running session.(*Store).RunSweeper. Used to pin R34 (NewRouter starts
// exactly one sweeper; serve() must not start a second).
func countSweeperGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "session.(*Store).RunSweeper")
}

func TestServerServesHealthAndShutsDown(t *testing.T) {
	root := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"APP_PORT=" + itoa(port),
		"LOG_LEVEL=error",
		"CLEANUP_INTERVAL_MINUTES=1",
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, env) }()

	base := "http://127.0.0.1:" + itoa(port)
	deadline := time.Now().Add(15 * time.Second)
	var resp *http.Response
	for {
		resp, err = http.Get(base + "/healthz")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Errorf("health = %d %v", resp.StatusCode, body)
	}
	// The UI endpoint's exact status depends on whether apps/frontend has been
	// built into internal/ui/dist for this checkout (.gitignore keeps dist/*
	// untracked except .gitkeep, so a built checkout and a fresh clone differ
	// here). Assert on the behaviour buildDeps actually controls -- it must
	// match ui.Present() -- rather than hardcoding one ambient state, so this
	// test does not start failing the moment Phase F builds the frontend.
	uiResp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer uiResp.Body.Close()
	wantUIStatus := http.StatusServiceUnavailable
	if ui.Present() {
		wantUIStatus = http.StatusOK
	}
	if uiResp.StatusCode != wantUIStatus {
		t.Errorf("ui = %d, want %d (ui.Present() = %v)", uiResp.StatusCode, wantUIStatus, ui.Present())
	}
	// R37: regardless of build state, POST must never reach the UI's 200/503
	// GET behaviour -- it must get the same 404 NOT_FOUND JSON:API contract
	// as any other unknown non-/api/ method.
	postResp, err := http.Post(base+"/", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer postResp.Body.Close()
	if postResp.StatusCode != http.StatusNotFound {
		t.Errorf("POST / = %d, want 404", postResp.StatusCode)
	}

	// R34: serve() must not start a second session sweeper alongside the one
	// internal/api.NewRouter already starts -- a second would double-sweep
	// every session against the same store. Poll to a deadline (goroutine
	// scheduling isn't synchronous) for exactly one, not zero and not two.
	deadline = time.Now().Add(5 * time.Second)
	var count int
	for {
		count = countSweeperGoroutines()
		if count > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if count != 1 {
		t.Errorf("sweeper goroutines while serve() is running = %d, want exactly 1", count)
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("server did not shut down")
	}

	// serve() returning nil is not, by itself, proof that the listener
	// actually stopped: a mutant that responds to ctx.Done() by returning
	// nil without ever calling srv.Shutdown would also make the assertions
	// above pass. Confirm the port has actually been released.
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+itoa(port), time.Second); err == nil {
		conn.Close()
		t.Fatal("server is still accepting connections after serve() returned")
	}
}

// nopCleaner satisfies session.Cleaner without touching disk; buildDeps
// wiring doesn't exercise cleanup behaviour, only field plumbing.
type nopCleaner struct{}

func (nopCleaner) Cleanup(context.Context, session.Session) error { return nil }
func (nopCleaner) RemoveDir(context.Context, string) error        { return nil }

// TestBuildDepsWiresConfiguredCleanupIntervalAndStore proves that main.go's
// wiring, not just internal/api.NewRouter's guard, is what makes the
// production sweeper run: buildDeps must forward application.Store and
// application.Config.CleanupInterval (loaded from CLEANUP_INTERVAL_MINUTES)
// into Deps unchanged, and must use the caller's ctx as BuildContext so the
// sweeper stops when the server shuts down. Without this, NewRouter's
// `d.Store != nil && d.CleanupInterval > 0` guard is never satisfied in
// production and the sweeper silently never starts.
func TestBuildDepsWiresConfiguredCleanupIntervalAndStore(t *testing.T) {
	store := session.NewStore(t.TempDir(), time.Hour, nopCleaner{}, slog.Default(), time.Now)
	wantInterval := 7 * time.Minute
	background := &sync.WaitGroup{}
	application := &app.App{
		Config:     config.Config{CleanupInterval: wantInterval},
		Log:        slog.Default(),
		Store:      store,
		Background: background,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := buildDeps(application, ctx)

	if deps.Store != store {
		t.Errorf("Deps.Store = %v, want the application's store %v", deps.Store, store)
	}
	if deps.CleanupInterval != wantInterval {
		t.Errorf("Deps.CleanupInterval = %v, want %v (from config.CleanupInterval)", deps.CleanupInterval, wantInterval)
	}
	if deps.CleanupInterval <= 0 {
		t.Fatalf("Deps.CleanupInterval must be positive or NewRouter's sweeper guard never fires; got %v", deps.CleanupInterval)
	}
	if deps.BuildContext != ctx {
		t.Errorf("Deps.BuildContext = %v, want the server's lifetime ctx %v", deps.BuildContext, ctx)
	}
	// Without this, NewRouter tracks the sweeper on a WaitGroup nobody waits
	// on and App.Close removes the git directories while it is still running.
	if deps.Background != background {
		t.Errorf("Deps.Background = %v, want the application's WaitGroup %v", deps.Background, background)
	}
}

// TestServeReturnsGenuineListenError proves main.go:90-92 and :101: a real
// ListenAndServe failure (e.g. "address already in use") must reach serve()'s
// caller as a non-nil error, not be swallowed as a clean exit. main() maps a
// non-nil serve() error to os.Exit(1), so a server that fails to bind must
// not exit 0.
//
// The failure is injected via the listenAndServe package var rather than by
// racing a real OS port bind (binding an already-bound port from two
// goroutines/processes in the same test is exactly the kind of timing-shaped
// test this project's guidelines forbid), so this is deterministic.
func TestServeReturnsGenuineListenError(t *testing.T) {
	root := t.TempDir()
	wantErr := errors.New("bind: address already in use")

	original := listenAndServe
	listenAndServe = func(*http.Server) error { return wantErr }
	t.Cleanup(func() { listenAndServe = original })

	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"APP_PORT=" + itoa(freePort(t)),
		"LOG_LEVEL=error",
		"CLEANUP_INTERVAL_MINUTES=1",
	}
	// ctx is never cancelled: serve() must return the listen error on its own,
	// not because shutdown was requested.
	err := serve(context.Background(), env)
	if err == nil {
		t.Fatal("serve() returned nil, want the injected listen error to propagate")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("serve() = %v, want it to wrap %v", err, wantErr)
	}
}

// TestServeTreatsErrServerClosedAsSuccess proves the other direction of the
// same branch: http.ErrServerClosed from a normal graceful shutdown must NOT
// be treated as a failure. Without this, a "genuine failure" fix that also
// starts rejecting ErrServerClosed would silently break every clean shutdown.
func TestServeTreatsErrServerClosedAsSuccess(t *testing.T) {
	root := t.TempDir()

	original := listenAndServe
	listenAndServe = func(*http.Server) error { return http.ErrServerClosed }
	t.Cleanup(func() { listenAndServe = original })

	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"APP_PORT=" + itoa(freePort(t)),
		"LOG_LEVEL=error",
		"CLEANUP_INTERVAL_MINUTES=1",
	}
	// ctx is left un-cancelled: with the injected listenAndServe returning
	// immediately, serve() must take the errc branch and map
	// http.ErrServerClosed to a nil return on its own, not because shutdown
	// was separately requested.
	if err := serve(context.Background(), env); err != nil {
		t.Errorf("serve() = %v, want nil for http.ErrServerClosed", err)
	}
}

// freePort returns a port not currently bound on 127.0.0.1, for tests that
// need a syntactically valid APP_PORT but never actually bind it (the real
// bind is replaced by an injected listenAndServe).
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
