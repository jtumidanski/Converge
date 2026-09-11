package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/identity"
)

// ProviderInput is the shape CreateProvider accepts (FR-5.1, FR-5.2, FR-5.6).
type ProviderInput struct {
	Slug        string
	DisplayName string
	Kind        config.Kind
	BaseURL     string
	Token       string
	Validate    bool
}

// ProviderPatch is the shape UpdateProvider accepts. There is deliberately no
// Slug field: the slug is immutable, and this makes that a compile-time
// property rather than a validation rule.
type ProviderPatch struct {
	DisplayName *string
	Kind        *config.Kind
	BaseURL     *string
	Token       *string // nil or empty => keep the stored token (FR-5.5)
	Validate    bool
}

// CreateProvider stores a new provider configuration for userID.
//
// Order matters: validate the shape, optionally verify the credential against
// the provider API, and only then seal and write. A verification failure must
// leave nothing behind (FR-5.6).
func (s *Service) CreateProvider(ctx context.Context, userID string, in ProviderInput) (UserProvider, error) {
	if err := ValidateSlug(in.Slug); err != nil {
		return UserProvider{}, err
	}
	kind, err := normalizeKind(in.Kind)
	if err != nil {
		return UserProvider{}, err
	}
	baseURL, err := NormalizeBaseURL(kind, in.BaseURL)
	if err != nil {
		return UserProvider{}, err
	}
	if in.Token == "" {
		return UserProvider{}, fmt.Errorf("auth: token is required: %w", ErrInvalidInput)
	}
	if in.Validate {
		if err := s.deps.Verifier.Verify(ctx, kind, baseURL, config.NewSecret(in.Token)); err != nil {
			return UserProvider{}, err
		}
	}
	id, err := NewID()
	if err != nil {
		return UserProvider{}, err
	}
	// Sealed under this row's own id, so the ciphertext cannot be moved to
	// another row or another user and still decrypt (FR-5.3).
	ciphertext, nonce, err := s.deps.Sealer.Seal(in.Token, userID, id)
	if err != nil {
		return UserProvider{}, err
	}
	now := s.deps.Now()
	row := UserProvider{
		id: id, userID: userID, slug: in.Slug,
		displayName: displayNameOr(in.DisplayName, in.Slug),
		kind:        kind, baseURL: baseURL,
		tokenCiphertext: ciphertext, tokenNonce: nonce,
		tokenLast4: Last4(in.Token), tokenSetAt: now,
		createdAt: now, updatedAt: now,
	}
	if err := s.deps.Store.CreateUserProvider(ctx, row); err != nil {
		return UserProvider{}, err
	}
	s.deps.Resolver.Invalidate(userID)
	s.deps.Log.Info("provider configuration created",
		slog.String("user_id", userID), slog.String("provider", in.Slug), slog.String("kind", string(kind)))
	return row, nil
}

// ListProviders returns every provider configuration userID owns.
func (s *Service) ListProviders(ctx context.Context, userID string) ([]UserProvider, error) {
	return s.deps.Store.ListUserProviders(ctx, userID)
}

// Provider returns one provider configuration, scoped to userID.
func (s *Service) Provider(ctx context.Context, userID, id string) (UserProvider, error) {
	return s.deps.Store.UserProviderByID(ctx, userID, id)
}

// UpdateProvider applies a partial update. slug is immutable, which
// ProviderPatch enforces by having no such field.
func (s *Service) UpdateProvider(ctx context.Context, userID, id string, p ProviderPatch) (UserProvider, error) {
	row, err := s.deps.Store.UserProviderByID(ctx, userID, id)
	if err != nil {
		return UserProvider{}, err
	}
	if p.DisplayName != nil {
		row.displayName = displayNameOr(*p.DisplayName, row.slug)
	}
	if p.Kind != nil {
		if row.kind, err = normalizeKind(*p.Kind); err != nil {
			return UserProvider{}, err
		}
	}
	if p.BaseURL != nil {
		if row.baseURL, err = NormalizeBaseURL(row.kind, *p.BaseURL); err != nil {
			return UserProvider{}, err
		}
	}
	now := s.deps.Now()
	// FR-5.5: an omitted or empty token leaves the stored token unchanged.
	// This is what lets the UI render an edit form without ever holding the
	// secret — the same mechanism as FR-5.4, seen from the other end.
	replacing := p.Token != nil && *p.Token != ""
	token := ""
	if replacing {
		token = *p.Token
	}
	if p.Validate {
		if replacing {
			if err := s.deps.Verifier.Verify(ctx, row.kind, row.baseURL, config.NewSecret(token)); err != nil {
				return UserProvider{}, err
			}
		} else {
			// Verifying an unchanged base URL or kind needs the stored token,
			// which means opening it — the only place an update decrypts.
			stored, openErr := s.deps.Sealer.Open(row.tokenCiphertext, row.tokenNonce, userID, id)
			if openErr != nil {
				return UserProvider{}, openErr
			}
			if err := s.deps.Verifier.Verify(ctx, row.kind, row.baseURL, stored); err != nil {
				return UserProvider{}, err
			}
		}
	}
	if replacing {
		ciphertext, nonce, sealErr := s.deps.Sealer.Seal(token, userID, id)
		if sealErr != nil {
			return UserProvider{}, sealErr
		}
		row.tokenCiphertext, row.tokenNonce = ciphertext, nonce
		row.tokenLast4, row.tokenSetAt = Last4(token), now
	}
	row.updatedAt = now
	if err := s.deps.Store.UpdateUserProvider(ctx, row); err != nil {
		return UserProvider{}, err
	}
	s.deps.Resolver.Invalidate(userID)
	s.deps.Log.Info("provider configuration updated",
		slog.String("user_id", userID), slog.String("provider", row.slug), slog.Bool("token_rotated", replacing))
	return row, nil
}

// DeleteProvider removes a configuration, refusing while a non-terminal
// review session of this user references it (FR-5.7). Completed reviews that
// referenced it stay readable.
func (s *Service) DeleteProvider(ctx context.Context, userID, id string) error {
	row, err := s.deps.Store.UserProviderByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if s.deps.Usage.ProviderInUse(identity.ForUser(userID), row.Slug()) {
		return &Error{
			Code:    CodeProviderInUse,
			Message: "This provider is used by a review that is still in progress.",
		}
	}
	if err := s.deps.Store.DeleteUserProvider(ctx, userID, id); err != nil {
		return err
	}
	s.deps.Resolver.Invalidate(userID)
	s.deps.Log.Info("provider configuration deleted",
		slog.String("user_id", userID), slog.String("provider", row.Slug()))
	return nil
}

func normalizeKind(k config.Kind) (config.Kind, error) {
	switch config.Kind(strings.ToLower(string(k))) {
	case config.KindGitHub:
		return config.KindGitHub, nil
	case config.KindGitLab:
		return config.KindGitLab, nil
	default:
		return "", fmt.Errorf("auth: kind must be github or gitlab: %w", ErrInvalidInput)
	}
}

func displayNameOr(name, slug string) string {
	if strings.TrimSpace(name) == "" {
		return slug
	}
	return strings.TrimSpace(name)
}
