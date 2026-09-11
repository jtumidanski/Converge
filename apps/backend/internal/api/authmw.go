package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/jsonapi"
)

// publicRoute lists the routes reachable without a login session (FR-4.1).
// /healthz and the embedded UI are handled by the /api/ prefix check in the
// middleware, not here.
//
// logout is public too: api-contracts.md documents only a 204 response for
// it, never a 401, and Service.Logout treats a missing or stale token as a
// no-op. Gating it on authenticate would mean a caller holding an expired
// cookie could never clear it.
func publicRoute(method, path string) bool {
	switch path {
	case "/api/auth/mode":
		return method == http.MethodGet
	case "/api/auth/register", "/api/auth/login", "/api/auth/logout":
		return method == http.MethodPost
	default:
		return false
	}
}

// originGuard rejects a state-changing cross-site request.
//
// It runs before authenticate, because the contract requires that a
// cross-site request never reach a handler — and because it is cheaper. Both
// middlewares are constructed only in hosted mode, so standalone's request
// path gains exactly zero comparisons.
//
// Combined with SameSite=Lax this is the whole CSRF defence; no CSRF token is
// issued (FR-4.4).
func (s *server) originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if u, err := url.Parse(origin); err == nil && u.Host == r.Host {
				next.ServeHTTP(w, r)
				return
			}
		}
		_ = jsonapi.WriteError(w, http.StatusForbidden, string(auth.CodeForbidden),
			jsonapi.StatusTitle(http.StatusForbidden),
			"This request did not come from this application.")
	})
}

// authenticate resolves the session cookie into an identity.Scope on the
// request context.
func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /healthz and the embedded UI assets are always unauthenticated
		// (FR-4.1).
		if !strings.HasPrefix(r.URL.Path, "/api/") || publicRoute(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		scope, tokenHash, err := s.deps.Auth.Authenticate(r.Context(), sessionTokenFrom(r))
		if err != nil {
			// Clearing the cookie on a rejected session stops a browser from
			// re-presenting a token the server has already forgotten.
			s.clearSessionCookie(w, r)
			writeDomainError(w, s.deps.Log, err)
			return
		}
		next.ServeHTTP(w, withAuthState(r, authState{scope: scope, tokenHash: tokenHash}))
	})
}
