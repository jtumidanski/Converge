package api

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/jtumidanski/converge/internal/identity"
)

// sessionCookieName is the login session cookie (FR-3.1).
const sessionCookieName = "converge_session"

// authStateKey is a private, non-string context key, so no other package can
// read or write this value.
type authStateKey struct{}

type authState struct {
	scope     identity.Scope
	tokenHash []byte
}

// scopeFrom returns the scope the authenticate middleware attached, or the
// standalone scope when none is present.
//
// This is the one place identity travels on a context (FR-4.5). Below api it
// is always an explicit function argument, because a value whose absence is a
// data leak should be visible in signatures.
func scopeFrom(r *http.Request) identity.Scope {
	if st, ok := r.Context().Value(authStateKey{}).(authState); ok {
		return st.scope
	}
	return identity.Standalone()
}

// tokenHashFrom returns the calling login session's token hash, so
// ChangePassword can revoke every session except this one.
func tokenHashFrom(r *http.Request) []byte {
	if st, ok := r.Context().Value(authStateKey{}).(authState); ok {
		return st.tokenHash
	}
	return nil
}

// sessionTokenFrom reads the raw cookie. Only the logout handler and the
// authenticate middleware use it; no other handler reads the cookie directly
// (FR-4.5).
func sessionTokenFrom(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func withAuthState(r *http.Request, st authState) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authStateKey{}, st))
}

// secure reports whether the cookie should carry the Secure attribute: when
// the request arrived over TLS, or unconditionally when
// CONVERGE_SECURE_COOKIES=true, which is what an operator terminating TLS at
// a proxy sets (FR-3.2).
func (s *server) secure(r *http.Request) bool {
	return r.TLS != nil || s.deps.SecureCookies
}

func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124 false positive: HttpOnly and SameSite are set below; Secure is s.secure(r), a dynamic (not statically-analysable) true/false, never omitted (FR-3.1, FR-3.2)
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure(r),
		SameSite: http.SameSiteLaxMode,
		// MaxAge mirrors the session's absolute expiry so a browser discards
		// the cookie at roughly the moment the server stops honouring it.
		MaxAge: int(s.deps.LoginSessionTTL.Seconds()),
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124 false positive: HttpOnly and SameSite are set below; Secure is s.secure(r), a dynamic (not statically-analysable) true/false, never omitted (FR-3.1, FR-3.2)
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// clientIP is the throttle key. X-Forwarded-For is honoured only when the
// operator has declared a trusted proxy: otherwise any client could forge a
// fresh key per request and sidestep the per-IP lockout entirely (FR-7.3).
func (s *server) clientIP(r *http.Request) string {
	if s.deps.TrustedProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			hops := strings.Split(xff, ",")
			if last := strings.TrimSpace(hops[len(hops)-1]); last != "" {
				return last
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
