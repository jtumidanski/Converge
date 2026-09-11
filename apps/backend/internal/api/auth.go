package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/jsonapi"
)

// Resource types, verbatim from api-contracts.md.
const (
	typeModes            = "modes"
	typeCredentials      = "credentials"
	typeUsers            = "users"
	typePasswords        = "passwords"
	typeAccountDeletions = "accountDeletions"
)

type modeAttributes struct {
	Mode string `json:"mode"`
	// RegistrationOpen is false in standalone and true in hosted. It exists so
	// a future invite-code or closed-registration feature needs no new
	// endpoint — the seam design §11 names.
	RegistrationOpen bool `json:"registrationOpen"`
}

type credentialAttributes struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userAttributes struct {
	Username  string `json:"username"`
	CreatedAt string `json:"createdAt"`
	// ProviderCount is omitted on the register/login responses and present on
	// GET /api/auth/me.
	ProviderCount *int `json:"providerCount,omitempty"`
}

type passwordAttributes struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type accountDeletionAttributes struct {
	Password string `json:"password"`
}

// userResource maps an account onto the wire. There is deliberately no field
// here that could carry a password hash.
func userResource(u auth.User, providerCount *int) jsonapi.Resource {
	return jsonapi.Resource{Type: typeUsers, ID: u.ID(), Attributes: userAttributes{
		Username:      u.Username(),
		CreatedAt:     u.CreatedAt().UTC().Format(time.RFC3339),
		ProviderCount: providerCount,
	}}
}

// authMode is registered in both modes and needs no session: it is the SPA's
// first call and decides whether a login screen is rendered at all (FR-8.1).
func (s *server) authMode(w http.ResponseWriter, _ *http.Request) {
	hosted := s.deps.Mode == config.ModeHosted
	res := jsonapi.Resource{Type: typeModes, ID: "current", Attributes: modeAttributes{
		Mode:             string(s.deps.Mode),
		RegistrationOpen: hosted,
	}}
	if err := jsonapi.WriteOne(w, http.StatusOK, res); err != nil {
		s.deps.Log.Error("write auth mode failed", "error", err)
	}
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[credentialAttributes](r, typeCredentials)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	user, token, err := s.deps.Auth.Register(r.Context(),
		auth.Credentials{Username: attrs.Username, Password: attrs.Password}, s.clientIP(r))
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	s.setSessionCookie(w, r, token)
	if err := jsonapi.WriteOne(w, http.StatusCreated, userResource(user, nil)); err != nil {
		s.deps.Log.Error("write register response failed", "error", err)
	}
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[credentialAttributes](r, typeCredentials)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	user, token, err := s.deps.Auth.Login(r.Context(),
		auth.Credentials{Username: attrs.Username, Password: attrs.Password}, s.clientIP(r))
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	s.setSessionCookie(w, r, token)
	if err := jsonapi.WriteOne(w, http.StatusOK, userResource(user, nil)); err != nil {
		s.deps.Log.Error("write login response failed", "error", err)
	}
}

// writeAuthError delegates to writeDomainError, first setting Retry-After for
// a lockout.
//
// This is the one local exception to "classify owns the HTTP mapping":
// classify returns a triple and cannot set a header, and ACCOUNT_LOCKED needs
// one (FR-7.4). Documented here rather than by widening classify's contract
// for a single code.
func (s *server) writeAuthError(w http.ResponseWriter, err error) {
	var ae *auth.Error
	if errors.As(err, &ae) && ae.Code == auth.CodeAccountLocked && ae.RetryAfter > 0 {
		seconds := int(math.Ceil(ae.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
	}
	writeDomainError(w, s.deps.Log, err)
}

// logout is idempotent and never reveals session validity (FR-3.6).
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Auth.Logout(r.Context(), sessionTokenFrom(r)); err != nil {
		s.deps.Log.Warn("logout failed", "error", err)
	}
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) currentUser(w http.ResponseWriter, r *http.Request) {
	userID := scopeFrom(r).UserID()
	user, err := s.deps.Auth.User(r.Context(), userID)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	count, err := s.deps.Auth.ProviderCount(r.Context(), userID)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusOK, userResource(user, &count)); err != nil {
		s.deps.Log.Error("write current user failed", "error", err)
	}
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[passwordAttributes](r, typePasswords)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	// tokenHashFrom identifies "this session", the one session
	// ChangePassword must not revoke (FR-2.6).
	if err := s.deps.Auth.ChangePassword(r.Context(), scopeFrom(r).UserID(), tokenHashFrom(r),
		attrs.CurrentPassword, attrs.NewPassword); err != nil {
		s.writeAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[accountDeletionAttributes](r, typeAccountDeletions)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	if err := s.deps.Auth.DeleteAccount(r.Context(), scopeFrom(r).UserID(), attrs.Password); err != nil {
		s.writeAuthError(w, err)
		return
	}
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}
