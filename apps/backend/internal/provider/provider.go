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
	ListRepositories(ctx context.Context, page Page) (Slice[Repository], error)
	GetRepository(ctx context.Context, fullName string) (Repository, error)
	ListMergedChanges(ctx context.Context, repo Repository, targetBranch, search string, page Page) (Slice[ChangeRequest], error)
	GetChange(ctx context.Context, repo Repository, number int) (ChangeRequest, error)
	GetChangeCommits(ctx context.Context, repo Repository, number int) ([]Commit, error)
	CloneURL(repo Repository) string
	// AuthorizeGit attaches credentials to spec via environment only.
	AuthorizeGit(repo Repository, spec *gitx.Spec) error
}
