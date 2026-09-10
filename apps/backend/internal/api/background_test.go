package api

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/session"
)

// The sweeper keeps touching session directories after the HTTP server has
// drained. NewRouter must register it on Deps.Background so shutdown waits for
// it to return before those directories are removed.
func TestRouterRegistersSweeperOnBackgroundWaitGroup(t *testing.T) {
	store := session.NewStore(t.TempDir(), time.Hour, nil, slog.New(slog.DiscardHandler), time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}

	NewRouter(Deps{
		Log:             slog.New(slog.DiscardHandler),
		BuildContext:    ctx,
		Store:           store,
		CleanupInterval: time.Hour,
		Background:      wg,
	})

	waited := make(chan struct{})
	go func() {
		defer close(waited)
		wg.Wait()
	}()
	select {
	case <-waited:
		t.Fatal("Background.Wait returned while the sweeper was still running: shutdown would not wait for it")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	select {
	case <-waited:
	case <-time.After(30 * time.Second):
		t.Fatal("sweeper did not stop after its context was cancelled")
	}
}

// A router built without a Background WaitGroup must still work: the CLI and
// most tests do not manage shutdown.
func TestRouterWithoutBackgroundWaitGroupStillStartsSweeper(t *testing.T) {
	store := session.NewStore(t.TempDir(), time.Hour, nil, slog.New(slog.DiscardHandler), time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if h := NewRouter(Deps{Log: slog.New(slog.DiscardHandler), BuildContext: ctx, Store: store, CleanupInterval: time.Hour}); h == nil {
		t.Fatal("NewRouter returned nil")
	}
}
