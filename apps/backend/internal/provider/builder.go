package provider

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
)

// RepositoryBuilder constructs a Repository.
type RepositoryBuilder struct{ r Repository }

func NewRepositoryBuilder() *RepositoryBuilder { return &RepositoryBuilder{} }

func (b *RepositoryBuilder) SetProviderID(v string) *RepositoryBuilder { b.r.providerID = v; return b }
func (b *RepositoryBuilder) SetFullName(v string) *RepositoryBuilder   { b.r.fullName = v; return b }
func (b *RepositoryBuilder) SetName(v string) *RepositoryBuilder       { b.r.name = v; return b }
func (b *RepositoryBuilder) SetNamespace(v string) *RepositoryBuilder  { b.r.namespace = v; return b }
func (b *RepositoryBuilder) SetDefaultBranch(v string) *RepositoryBuilder {
	b.r.defaultBranch = v
	return b
}
func (b *RepositoryBuilder) SetWebURL(v string) *RepositoryBuilder   { b.r.webURL = v; return b }
func (b *RepositoryBuilder) SetCloneURL(v string) *RepositoryBuilder { b.r.cloneURL = v; return b }

// Build validates invariants; name/namespace derive from the full name when empty.
func (b *RepositoryBuilder) Build() (Repository, error) {
	r := b.r
	if r.providerID == "" {
		return Repository{}, errors.New("repository: provider id is required")
	}
	if err := gitx.ValidateRepoFullName(r.fullName); err != nil {
		return Repository{}, fmt.Errorf("repository: %w", err)
	}
	idx := strings.LastIndex(r.fullName, "/")
	if r.name == "" {
		r.name = r.fullName[idx+1:]
	}
	if r.namespace == "" {
		r.namespace = r.fullName[:idx]
	}
	return r, nil
}

// ChangeRequestBuilder constructs a ChangeRequest.
type ChangeRequestBuilder struct{ c ChangeRequest }

func NewChangeRequestBuilder() *ChangeRequestBuilder { return &ChangeRequestBuilder{} }

func (b *ChangeRequestBuilder) SetProviderID(v string) *ChangeRequestBuilder {
	b.c.providerID = v
	return b
}
func (b *ChangeRequestBuilder) SetRepository(v Repository) *ChangeRequestBuilder {
	b.c.repository = v
	return b
}
func (b *ChangeRequestBuilder) SetNumber(v int) *ChangeRequestBuilder    { b.c.number = v; return b }
func (b *ChangeRequestBuilder) SetTitle(v string) *ChangeRequestBuilder  { b.c.title = v; return b }
func (b *ChangeRequestBuilder) SetAuthor(v string) *ChangeRequestBuilder { b.c.author = v; return b }
func (b *ChangeRequestBuilder) SetWebURL(v string) *ChangeRequestBuilder { b.c.webURL = v; return b }
func (b *ChangeRequestBuilder) SetSourceBranch(v string) *ChangeRequestBuilder {
	b.c.sourceBranch = v
	return b
}
func (b *ChangeRequestBuilder) SetTargetBranch(v string) *ChangeRequestBuilder {
	b.c.targetBranch = v
	return b
}
func (b *ChangeRequestBuilder) SetCreatedAt(v time.Time) *ChangeRequestBuilder {
	b.c.createdAt = v
	return b
}
func (b *ChangeRequestBuilder) SetMergedAt(v time.Time) *ChangeRequestBuilder {
	b.c.mergedAt = v
	return b
}
func (b *ChangeRequestBuilder) SetState(v ChangeState) *ChangeRequestBuilder { b.c.state = v; return b }
func (b *ChangeRequestBuilder) SetCommits(v []Commit) *ChangeRequestBuilder {
	b.c.commits = append([]Commit(nil), v...)
	return b
}
func (b *ChangeRequestBuilder) SetCommitCount(v int) *ChangeRequestBuilder {
	b.c.commitCount = v
	return b
}
func (b *ChangeRequestBuilder) SetMergeCommitSHA(v string) *ChangeRequestBuilder {
	b.c.mergeCommitSHA = v
	return b
}
func (b *ChangeRequestBuilder) SetSquashCommitSHA(v string) *ChangeRequestBuilder {
	b.c.squashCommitSHA = v
	return b
}
func (b *ChangeRequestBuilder) SetHeadSHA(v string) *ChangeRequestBuilder { b.c.headSHA = v; return b }
func (b *ChangeRequestBuilder) SetSquashed(v bool) *ChangeRequestBuilder  { b.c.squashed = v; return b }

// Build validates invariants.
func (b *ChangeRequestBuilder) Build() (ChangeRequest, error) {
	c := b.c
	if c.providerID == "" {
		return ChangeRequest{}, errors.New("change: provider id is required")
	}
	if c.repository.fullName == "" {
		return ChangeRequest{}, errors.New("change: repository is required")
	}
	if c.number <= 0 {
		return ChangeRequest{}, errors.New("change: number must be positive")
	}
	if c.title == "" {
		return ChangeRequest{}, errors.New("change: title is required")
	}
	if c.targetBranch == "" {
		return ChangeRequest{}, errors.New("change: target branch is required")
	}
	if c.state == "" {
		c.state = StateOpen
	}
	for _, sha := range []string{c.mergeCommitSHA, c.squashCommitSHA, c.headSHA} {
		if sha != "" {
			if err := gitx.ValidateSHA(sha); err != nil {
				return ChangeRequest{}, fmt.Errorf("change %d: %w", c.number, err)
			}
		}
	}
	if c.commitCount == 0 {
		c.commitCount = len(c.commits)
	}
	return c, nil
}
