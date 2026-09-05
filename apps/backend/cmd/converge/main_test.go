package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/app"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/session"
)

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
	// the UI stub answers 503 with the build hint
	uiResp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer uiResp.Body.Close()
	if uiResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("ui = %d, want 503 before the frontend is built", uiResp.StatusCode)
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
	application := &app.App{
		Config: config.Config{CleanupInterval: wantInterval},
		Log:    slog.Default(),
		Store:  store,
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
