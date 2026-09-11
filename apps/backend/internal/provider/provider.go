package provider

import (
	"context"

	"github.com/jtumidanski/converge/internal/gitx"
)

// GitProvider isolates provider behaviour behind one interface.
type GitProvider interface {
	ID() string
	Kind() Kind
	DisplayName() string
	BaseURL() string
	// ListRepositories lists the token's repositories. search is an
	// already-trimmed, already-length-checked substring filter; empty means
	// no filter. Validation belongs to the API layer, not here.
	ListRepositories(ctx context.Context, search string, page Page) (Slice[Repository], error)
	GetRepository(ctx context.Context, fullName string) (Repository, error)
	ListMergedChanges(ctx context.Context, repo Repository, targetBranch, search string, page Page) (Slice[ChangeRequest], error)
	GetChange(ctx context.Context, repo Repository, number int) (ChangeRequest, error)
	GetChangeCommits(ctx context.Context, repo Repository, number int) ([]Commit, error)
	CloneURL(repo Repository) string
	// AuthorizeGit attaches credentials to spec via environment only.
	AuthorizeGit(repo Repository, spec *gitx.Spec) error
}

// GitUser is the Basic-auth username each provider accepts for tokens.
func GitUser(kind Kind) string {
	if kind == KindGitHub {
		return "x-access-token"
	}
	return "oauth2"
}
