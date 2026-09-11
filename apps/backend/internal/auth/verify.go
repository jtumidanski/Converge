package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/github"
	"github.com/jtumidanski/converge/internal/provider/gitlab"
)

// ProviderVerifier checks a credential against the provider API before a
// configuration is written (FR-5.6). Injected so a test can assert the
// validate=true path without reaching the network.
type ProviderVerifier interface {
	Verify(ctx context.Context, kind config.Kind, baseURL string, token config.Secret) error
}

type httpVerifier struct {
	client *http.Client
	now    func() time.Time
}

// NewHTTPVerifier verifies by listing one repository: the cheapest call that
// exercises authentication on both providers.
func NewHTTPVerifier(client *http.Client, now func() time.Time) ProviderVerifier {
	return &httpVerifier{client: client, now: now}
}

func (v *httpVerifier) Verify(ctx context.Context, kind config.Kind, baseURL string, token config.Secret) error {
	p, err := buildClient("verify", "verify", kind, baseURL, token, v.client, v.now)
	if err != nil {
		return err
	}
	if _, err := p.ListRepositories(ctx, provider.Page{Number: 1, Size: 1}.Normalize()); err != nil {
		if errors.Is(err, provider.ErrAuth) {
			return &Error{
				Code:    CodeProviderUnauthorized,
				Message: "The provider rejected that access token.",
			}
		}
		// A network or availability failure is not an authorisation verdict,
		// so it is reported as itself rather than as a bad credential.
		return fmt.Errorf("auth: verify provider credential: %w", err)
	}
	return nil
}

// buildClient is the single place a provider client is constructed from a
// kind, so the resolver and the verifier cannot drift.
func buildClient(id, displayName string, kind config.Kind, baseURL string, token config.Secret, client *http.Client, now func() time.Time) (provider.GitProvider, error) {
	switch kind {
	case config.KindGitHub:
		return github.New(id, displayName, baseURL, token, client, now), nil
	case config.KindGitLab:
		return gitlab.New(id, displayName, baseURL, token, client), nil
	default:
		return nil, fmt.Errorf("auth: unknown provider kind %q", kind)
	}
}
