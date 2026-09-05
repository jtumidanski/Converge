package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
)

// Deps are the router's collaborators.
type Deps struct {
	Service   *review.Service
	Providers *provider.Registry
	Log       *slog.Logger
	UI        fs.FS
	UIPresent bool
	// BuildContext bounds asynchronous builds started by POST /api/reviews
	// and defaults to context.Background() when nil. Task 20 passes the
	// server's lifetime context here so builds (and, see Store/CleanupInterval
	// below, the session sweeper) are cancelled together on shutdown.
	BuildContext context.Context

	// Store, if set, is swept every CleanupInterval for the lifetime of
	// BuildContext. A long-running HTTP server must run the sweeper itself:
	// unlike the one-shot CLI, nothing else in the process ever calls
	// Store.RunSweeper. Tests that build a Deps without a Store (or without a
	// positive CleanupInterval) get no background goroutine at all, so unit
	// tests never leak one.
	Store           *session.Store
	CleanupInterval time.Duration
}

type server struct {
	deps     Deps
	buildCtx context.Context
}

// NewRouter builds the fully wrapped HTTP handler. If Deps.Store and a
// positive Deps.CleanupInterval are supplied, NewRouter also starts the
// session sweeper as a goroutine tied to Deps.BuildContext (the server's
// lifetime context); the goroutine exits when that context is cancelled, so
// callers that cancel BuildContext on shutdown do not leak it.
func NewRouter(d Deps) http.Handler {
	buildCtx := d.BuildContext
	if buildCtx == nil {
		buildCtx = context.Background()
	}
	if d.Store != nil && d.CleanupInterval > 0 {
		go d.Store.RunSweeper(buildCtx, d.CleanupInterval)
	}
	s := &server{deps: d, buildCtx: buildCtx}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/providers", s.listProviders)
	mux.HandleFunc("GET /api/providers/{provider}/repositories", s.listRepositories)
	// {repo} (a single wildcard segment, not {repo...}) is correct here: Go's
	// ServeMux matches wildcard segments against the request's escaped path
	// segments, so a %2F-encoded "owner/name" in the URL still resolves to one
	// segment and r.PathValue("repo") returns it fully decoded. Verified by
	// TestRepositoriesAndChanges, which posts url.PathEscape("atlas/server").
	mux.HandleFunc("GET /api/providers/{provider}/repositories/{repo}", s.getRepository)
	mux.HandleFunc("GET /api/providers/{provider}/repositories/{repo}/changes", s.listChanges)
	mux.HandleFunc("POST /api/reviews", s.createReview)
	mux.HandleFunc("GET /api/reviews", s.listReviews)
	mux.HandleFunc("GET /api/reviews/{id}", s.getReview)
	mux.HandleFunc("DELETE /api/reviews/{id}", s.deleteReview)
	mux.HandleFunc("GET /api/reviews/{id}/files", s.listReviewFiles)
	mux.HandleFunc("GET /api/reviews/{id}/files/{path...}", s.getReviewFile)
	mux.HandleFunc("GET /api/reviews/{id}/diff", s.getReviewDiff)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", jsonapi.StatusTitle(http.StatusNotFound), "No such endpoint.")
	})
	if d.UI != nil {
		mux.Handle("GET /", uiHandler(d.UI, d.UIPresent))
	}
	return withMiddleware(mux, d.Log)
}
