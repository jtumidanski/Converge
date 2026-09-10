// Command converge serves the Converge HTTP API and the embedded UI.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jtumidanski/converge/internal/api"
	"github.com/jtumidanski/converge/internal/app"
	"github.com/jtumidanski/converge/internal/buildinfo"
	"github.com/jtumidanski/converge/internal/ui"
)

const shutdownTimeout = 15 * time.Second

// listenAndServe runs srv until it stops, and exists so tests can inject a
// genuine startup failure (e.g. "address already in use") deterministically,
// without racing a real OS port bind. Production always uses the default,
// which is exactly srv.ListenAndServe().
var listenAndServe = func(srv *http.Server) error {
	return srv.ListenAndServe()
}

// buildDeps assembles the router's Deps from a wired *app.App and the
// server's lifetime context. This is the one place that feeds
// application.Config.CleanupInterval (loaded from CLEANUP_INTERVAL_MINUTES)
// into Deps.CleanupInterval alongside application.Store: internal/api.NewRouter
// only starts its sweeper goroutine when both Deps.Store is set and
// Deps.CleanupInterval is positive, so without this wiring the router's
// guard is never satisfied and the sweeper silently never runs in
// production. Extracted to a pure function so the wiring itself -- not just
// the end-to-end server behaviour -- has a fast, direct unit test.
func buildDeps(application *app.App, ctx context.Context) api.Deps {
	return api.Deps{
		Service:         application.Service,
		Providers:       application.Registry,
		Log:             application.Log,
		UI:              ui.FS(),
		UIPresent:       ui.Present(),
		BuildContext:    ctx,
		Store:           application.Store,
		CleanupInterval: application.Config.CleanupInterval,
		// The sweeper goroutine NewRouter starts keeps calling git after the
		// HTTP server has drained. Registering it on the App's WaitGroup is
		// what makes App.Close wait for it before it removes the git runner's
		// shared HOME and hooks directories.
		Background: application.Background,
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "converge: %v\n", err)
		os.Exit(1)
	}
}

// serve runs the HTTP server until ctx is cancelled, then drains in-flight
// requests for up to shutdownTimeout before returning.
//
// The session sweeper is NOT started here. internal/api.NewRouter already
// starts exactly one sweeper goroutine, tied to Deps.BuildContext, whenever
// Deps.Store is set and Deps.CleanupInterval is positive (see
// internal/api/router.go). Starting a second sweeper in this function would
// double-sweep every session against the same store. What main.go owns is
// wiring application.Config.CleanupInterval into Deps.CleanupInterval and
// passing application.Store and the server's lifetime ctx as BuildContext,
// so the router's existing guard is finally satisfied in production and the
// one sweeper it starts runs at the configured interval and stops when ctx
// is cancelled.
func serve(ctx context.Context, env []string) error {
	application, err := app.New(ctx, env)
	if err != nil {
		return err
	}
	// Close drains in-flight builds and the sweeper before it removes the git
	// runner's temporary directories. A drain timeout or a cleanup failure is
	// logged rather than discarded: it means git work may have raced the
	// removal, which is exactly the condition an operator needs to see.
	defer func() {
		if err := application.Close(); err != nil {
			application.Log.Error("shutdown cleanup", slog.String("error", err.Error()))
		}
	}()

	handler := api.NewRouter(buildDeps(application, ctx))

	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(application.Config.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		application.Log.Info("listening", slog.String("addr", addr), slog.String("version", buildinfo.Version))
		if err := listenAndServe(srv); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		// ListenAndServe returned before shutdown was requested: this is a
		// genuine startup/listen failure (e.g. the port is already in use)
		// and must propagate as a non-zero exit, not a silent nil return.
		return err
	case <-ctx.Done():
		application.Log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			// A timed-out or failed drain must be visible to the caller, not
			// swallowed as a clean exit.
			return fmt.Errorf("shutdown: %w", err)
		}
		// Shutdown succeeded, so ListenAndServe is guaranteed to have
		// returned (with http.ErrServerClosed, mapped to nil above).
		return <-errc
	}
}
