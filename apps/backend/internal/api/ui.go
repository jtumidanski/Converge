package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/jtumidanski/converge/internal/jsonapi"
)

const notBuiltMessage = "UI not built; run make build\n"

// uiHandler serves the embedded SPA with index.html fallback.
//
// It is registered at "/" (not "GET /"): a method-restricted pattern there
// conflicts with the existing "/api/" registration and panics at startup
// (neither pattern dominates -- one is more method-specific, the other more
// path-specific). Because "/" carries no method restriction of its own,
// uiHandler must enforce GET/HEAD itself; otherwise every other method
// (POST/PUT/DELETE) to any non-/api/ path -- including method-restricted
// routes like "GET /healthz" -- falls through to this handler and gets a
// 200 index.html instead of the expected 404/405. R37: anything other than
// GET/HEAD gets the same 404 NOT_FOUND JSON:API response "/api/" already
// returns for unknown endpoints (405 was rejected: it requires an Allow
// header and would leak which paths are UI-served).
func uiHandler(fsys fs.FS, present bool) http.Handler {
	if !present {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", jsonapi.StatusTitle(http.StatusNotFound), "No such endpoint.")
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(notBuiltMessage))
		})
	}
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", jsonapi.StatusTitle(http.StatusNotFound), "No such endpoint.")
			return
		}
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean != "" {
			if _, err := fs.Stat(fsys, clean); err == nil {
				if strings.HasPrefix(clean, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, notBuiltMessage, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	})
}
