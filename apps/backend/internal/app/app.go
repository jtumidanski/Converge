// Package app wires configuration into the collaborators both binaries need.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/github"
	"github.com/jtumidanski/converge/internal/provider/gitlab"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
	"github.com/jtumidanski/converge/internal/workspace"
)

// MinGitVersion is required for `cherry-pick --empty=keep`.
var MinGitVersion = "2.45"

// backgroundDrainTimeout bounds how long Close waits for background work
// (asynchronous builds, the session sweeper) to stop. Those goroutines keep
// running git after the HTTP server has drained, and Close deletes the git
// runner's shared HOME and hooks directories, so removing them first would
// pull the environment out from under a live git process. It is a var only so
// tests can shorten it; production never reassigns it.
var backgroundDrainTimeout = 20 * time.Second

// ErrBackgroundDrainTimeout reports that background work was still running
// when Close gave up waiting. Close then proceeds with cleanup regardless --
// a shutdown must finish -- so this error is the caller's only signal that
// the removal raced live git commands.
var ErrBackgroundDrainTimeout = errors.New("background work did not stop before shutdown")

// App holds every wired collaborator.
type App struct {
	Config     config.Config
	Log        *slog.Logger
	Runner     *gitx.ExecRunner
	Registry   *provider.Registry
	Mirrors    *mirror.Cache
	Workspaces *workspace.Manager
	Store      *session.Store
	Service    *review.Service

	// Background tracks every goroutine that keeps using the git runner after
	// the HTTP server has drained: the asynchronous builds started by
	// review.Service and the session sweeper started by api.NewRouter. Wire it
	// into api.Deps.Background so Close covers the sweeper too.
	Background *sync.WaitGroup
}

// Close waits for background work to stop, then releases the git runner's
// temporary directories. Both failures are reported: a drain timeout is
// joined with any cleanup error rather than replaced by it.
func (a *App) Close() error {
	var errs []error
	if a.Background != nil && !waitTimeout(a.Background, backgroundDrainTimeout) {
		errs = append(errs, ErrBackgroundDrainTimeout)
	}
	if a.Runner != nil {
		if err := a.Runner.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// waitTimeout reports whether wg reached zero within d. On a timeout the
// helper goroutine stays parked in wg.Wait until the background work finally
// stops; that is deliberate, and harmless because Close runs once per process
// as it exits.
func waitTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() { defer close(done); wg.Wait() }()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// redactingHandler scrubs configured secrets from every logged value.
type redactingHandler struct {
	inner   slog.Handler
	secrets []string
}

func (h *redactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, h.scrubString(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(h.scrubAttr(a))
		return true
	})
	return h.inner.Handle(ctx, clean)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	scrubbed := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		scrubbed[i] = h.scrubAttr(a)
	}
	return &redactingHandler{inner: h.inner.WithAttrs(scrubbed), secrets: h.secrets}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: h.inner.WithGroup(name), secrets: h.secrets}
}

func (h *redactingHandler) scrubString(s string) string {
	for _, secret := range h.secrets {
		if secret != "" && strings.Contains(s, secret) {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

// scrubAttr recursively scrubs an attribute's value, descending into groups
// (including groups produced by WithGroup/WithAttrs chains) so a secret
// nested under any depth of grouping is still caught.
func (h *redactingHandler) scrubAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.scrubString(v.String()))
	case slog.KindGroup:
		group := v.Group()
		out := make([]any, 0, len(group))
		for _, g := range group {
			out = append(out, h.scrubAttr(g))
		}
		return slog.Group(a.Key, out...)
	default:
		return slog.Attr{Key: a.Key, Value: slog.StringValue(h.scrubString(v.String()))}
	}
}

// loggableURL strips embedded userinfo (e.g. "https://user:pw@host") from a
// provider base URL before it is logged. config.Config.Secrets() only
// collects provider tokens, so a password embedded in BASE_URL's userinfo is
// never seen by the redacting handler and would otherwise reach the log in
// clear. If the URL does not even parse, nothing is logged rather than
// risking an unparsed credential leaking through verbatim.
func loggableURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	return u.String()
}

// NewLogger builds the configured handler wrapped in redaction.
func NewLogger(w io.Writer, cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var base slog.Handler
	if cfg.LogFormat == "json" {
		base = slog.NewJSONHandler(w, opts)
	} else {
		base = slog.NewTextHandler(w, opts)
	}
	return slog.New(&redactingHandler{inner: base, secrets: cfg.Secrets()})
}

// checkGitVersion enforces MinGitVersion.
func checkGitVersion(v string) error {
	// parse only rejects outright unparseable shapes (no dot-separated
	// major/minor at all). A non-numeric part parses to 0 via strconv.Atoi's
	// zero value, which the accept/reject comparison below already rejects
	// as "too old" -- so there is nothing a separate Atoi-error branch would
	// add here that isn't already covered by that comparison, and duplicating
	// it produced two error returns neither test could actually distinguish.
	parse := func(s string) (int, int, error) {
		parts := strings.Split(strings.TrimSpace(s), ".")
		if len(parts) < 2 {
			return 0, 0, fmt.Errorf("cannot parse git version %q", s)
		}
		major, _ := strconv.Atoi(parts[0])
		minor, _ := strconv.Atoi(parts[1])
		return major, minor, nil
	}
	gotMajor, gotMinor, err := parse(v)
	if err != nil {
		return err
	}
	wantMajor, wantMinor, _ := parse(MinGitVersion)
	if gotMajor > wantMajor || (gotMajor == wantMajor && gotMinor >= wantMinor) {
		return nil
	}
	return fmt.Errorf("git %s is required, found %s", MinGitVersion, v)
}

// New loads configuration and wires every collaborator.
//
// REPOSITORY_CACHE_ROOT is created here and passed directly into mirror.New
// as its root argument, so the configured value -- not some internal
// default -- is what mirror.Cache resolves every mirror path from.
func New(ctx context.Context, env []string) (*App, error) {
	cfg, err := config.Load(env)
	if err != nil {
		return nil, err
	}
	log := NewLogger(os.Stderr, cfg)
	runner, err := gitx.NewExecRunner(log, gitx.Options{CloneTimeout: cfg.GitCloneTimeout, CommandTimeout: cfg.GitCommandTimeout, Secrets: cfg.Secrets()})
	if err != nil {
		return nil, err
	}
	version, err := runner.Version(ctx)
	if err != nil {
		_ = runner.Close()
		return nil, fmt.Errorf("git is not usable: %w", err)
	}
	if err := checkGitVersion(version); err != nil {
		_ = runner.Close()
		return nil, err
	}
	if err := os.MkdirAll(cfg.RepositoryCacheRoot, 0o750); err != nil {
		_ = runner.Close()
		return nil, &config.Error{Variable: "REPOSITORY_CACHE_ROOT", Reason: "directory could not be created"}
	}
	if err := os.MkdirAll(cfg.WorkspaceRoot, 0o750); err != nil {
		_ = runner.Close()
		return nil, &config.Error{Variable: "WORKSPACE_ROOT", Reason: "directory could not be created"}
	}

	httpClient := &http.Client{Timeout: cfg.ProviderTimeout}
	registry := provider.NewRegistry()
	for _, pc := range cfg.Providers {
		var p provider.GitProvider
		switch pc.Kind {
		case config.KindGitHub:
			p = github.New(pc.ID, pc.DisplayName, pc.BaseURL, pc.Token, httpClient, time.Now)
		case config.KindGitLab:
			p = gitlab.New(pc.ID, pc.DisplayName, pc.BaseURL, pc.Token, httpClient)
		}
		if err := registry.Register(p); err != nil {
			_ = runner.Close()
			return nil, err
		}
		log.Info("provider configured", slog.String("provider", pc.ID), slog.String("kind", string(pc.Kind)), slog.String("base_url", loggableURL(pc.BaseURL)))
	}

	locks := &gitx.LockMap{}
	// REPOSITORY_CACHE_ROOT flows straight from config into mirror.New: the
	// cache never derives or defaults its own root.
	mirrors := mirror.New(cfg.RepositoryCacheRoot, runner, locks, log)
	workspaces, err := workspace.New(cfg.WorkspaceRoot, runner, locks, log)
	if err != nil {
		_ = runner.Close()
		return nil, err
	}
	background := &sync.WaitGroup{}
	cleaner := review.NewCleaner(mirrors, workspaces, log)
	store := session.NewStore(workspaces.Root(), cfg.SessionTTL, cleaner, log, time.Now)
	service := review.NewService(review.Deps{
		Providers: registry, Mirrors: mirrors, Workspaces: workspaces, Store: store,
		Applicator: review.NewCherryPickApplicator(runner, log), Runner: runner, Log: log,
		SessionTTL: cfg.SessionTTL, MaxConcurrentBuilds: cfg.MaxConcurrentBuilds, Now: time.Now,
		Background: background,
	})
	if err := store.LoadAll(ctx); err != nil {
		_ = runner.Close()
		return nil, err
	}
	log.Info("converge starting", slog.String("git_version", version), slog.Int("providers", len(cfg.Providers)))
	return &App{Config: cfg, Log: log, Runner: runner, Registry: registry, Mirrors: mirrors, Workspaces: workspaces, Store: store, Service: service, Background: background}, nil
}
