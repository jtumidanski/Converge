package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/jsonapi"
)

const typeUserProviders = "userProviders"

// userProviderAttributes is the response shape. There is no token field, and
// there is no endpoint anywhere in this API that returns one (FR-5.4).
// tokenLast4 is captured at write time and stored beside the ciphertext, so
// rendering a mask never requires a decrypt.
type userProviderAttributes struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"baseUrl"`
	TokenLast4  string `json:"tokenLast4"`
	TokenSetAt  string `json:"tokenSetAt"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type createUserProviderAttributes struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"baseUrl"`
	Token       string `json:"token"`
	// Validate is a pointer because its default is true, not Go's zero false
	// (FR-5.6). An omitted field must verify the credential.
	Validate *bool `json:"validate"`
}

// patchUserProviderAttributes has no Slug field: the slug is immutable, and
// jsonapi.Decode rejects unknown attributes, so an attempt to change it is a
// 400 from the decoder rather than a hand-written validation branch.
//
// Token is a pointer so "absent" and "empty string" are both expressible, and
// both mean "keep the stored token" (FR-5.5).
type patchUserProviderAttributes struct {
	DisplayName *string `json:"displayName"`
	Kind        *string `json:"kind"`
	BaseURL     *string `json:"baseUrl"`
	Token       *string `json:"token"`
	Validate    *bool   `json:"validate"`
}

func userProviderResource(p auth.UserProvider) jsonapi.Resource {
	return jsonapi.Resource{Type: typeUserProviders, ID: p.ID(), Attributes: userProviderAttributes{
		Slug:        p.Slug(),
		DisplayName: p.DisplayName(),
		Kind:        string(p.Kind()),
		BaseURL:     p.BaseURL(),
		TokenLast4:  p.TokenLast4(),
		TokenSetAt:  p.TokenSetAt().UTC().Format(time.RFC3339),
		CreatedAt:   p.CreatedAt().UTC().Format(time.RFC3339),
		UpdatedAt:   p.UpdatedAt().UTC().Format(time.RFC3339),
	}}
}

// validationError reports a request-shape problem with the code
// api-contracts.md assigns that class, VALIDATION_ERROR (422). classify has
// no arm for it: every plain error the auth provider layer returns is a
// request-shape problem, not a domain condition, so there is nothing for
// classify's *auth.Error/ErrNotFound arms to recognise here, and this writes
// the response directly instead.
func (s *server) validationError(w http.ResponseWriter, detail string) {
	if err := jsonapi.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR",
		jsonapi.StatusTitle(http.StatusUnprocessableEntity), detail); err != nil {
		s.deps.Log.Error("write validation error failed", "error", err)
	}
}

func (s *server) listUserProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.deps.Auth.ListProviders(r.Context(), scopeFrom(r).UserID())
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, userProviderResource(row))
	}
	if err := jsonapi.WriteList(w, http.StatusOK, out, nil); err != nil {
		s.deps.Log.Error("write user providers failed", "error", err)
	}
}

func (s *server) createUserProvider(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[createUserProviderAttributes](r, typeUserProviders)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	// auth.CreateProvider rejects an empty token with an untyped error, which
	// classify's fallback would otherwise turn into 500/GIT_FAILURE. The
	// contract requires 422/VALIDATION_ERROR (api-contracts.md), so the shape
	// is checked here, before the service is ever called.
	if attrs.Token == "" {
		s.validationError(w, "A token is required.")
		return
	}
	validate := true
	if attrs.Validate != nil {
		validate = *attrs.Validate
	}
	row, err := s.deps.Auth.CreateProvider(r.Context(), scopeFrom(r).UserID(), auth.ProviderInput{
		Slug:        attrs.Slug,
		DisplayName: attrs.DisplayName,
		Kind:        config.Kind(attrs.Kind),
		BaseURL:     attrs.BaseURL,
		Token:       attrs.Token,
		Validate:    validate,
	})
	if err != nil {
		s.writeProviderSettingsError(w, err)
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusCreated, userProviderResource(row)); err != nil {
		s.deps.Log.Error("write user provider failed", "error", err)
	}
}

// updateUserProvider defaults ProviderPatch.Validate to false: re-verifying
// on every cosmetic edit (a display-name change, for instance) would hit the
// provider API for no reason. PROVIDER_UNAUTHORIZED stays reachable on PATCH
// by sending "validate": true.
func (s *server) updateUserProvider(w http.ResponseWriter, r *http.Request) {
	attrs, err := jsonapi.Decode[patchUserProviderAttributes](r, typeUserProviders)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	patch := auth.ProviderPatch{
		DisplayName: attrs.DisplayName,
		BaseURL:     attrs.BaseURL,
		Token:       attrs.Token,
		Validate:    attrs.Validate != nil && *attrs.Validate,
	}
	if attrs.Kind != nil {
		kind := config.Kind(*attrs.Kind)
		patch.Kind = &kind
	}
	row, err := s.deps.Auth.UpdateProvider(r.Context(), scopeFrom(r).UserID(), r.PathValue("id"), patch)
	if err != nil {
		s.writeProviderSettingsError(w, err)
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusOK, userProviderResource(row)); err != nil {
		s.deps.Log.Error("write user provider failed", "error", err)
	}
}

func (s *server) deleteUserProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Auth.DeleteProvider(r.Context(), scopeFrom(r).UserID(), r.PathValue("id")); err != nil {
		s.writeProviderSettingsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeProviderSettingsError separates the two error families the auth
// provider layer produces: a typed *auth.Error carries its own wire code,
// auth.ErrNotFound is the cross-user/unknown-id case (FR-4.2), and everything
// else is a plain error from slug/kind/base-URL normalisation, which is a
// request-shape problem and reports as VALIDATION_ERROR. Every plain error
// returned by internal/auth's provider layer (auth/model.go, auth/providers.go)
// is a fixed, value-free message: none interpolates a token, a base URL, or a
// database message, so putting err.Error() in a client-visible detail here is
// safe.
func (s *server) writeProviderSettingsError(w http.ResponseWriter, err error) {
	var ae *auth.Error
	if errors.As(err, &ae) || errors.Is(err, auth.ErrNotFound) {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	s.validationError(w, err.Error())
}
