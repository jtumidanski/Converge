package github

import (
	"fmt"
	"time"

	"github.com/jtumidanski/converge/internal/provider"
)

type repoJSON struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
}

func (r repoJSON) toModel(providerID string) (provider.Repository, error) {
	return provider.NewRepositoryBuilder().
		SetProviderID(providerID).
		SetFullName(r.FullName).
		SetName(r.Name).
		SetNamespace(r.Owner.Login).
		SetDefaultBranch(r.DefaultBranch).
		SetWebURL(r.HTMLURL).
		SetCloneURL(r.CloneURL).
		Build()
}

type refJSON struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type pullJSON struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Merged  *bool  `json:"merged"`
	HTMLURL string `json:"html_url"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt      time.Time  `json:"created_at"`
	MergedAt       *time.Time `json:"merged_at"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	Commits        int        `json:"commits"`
	Head           refJSON    `json:"head"`
	Base           refJSON    `json:"base"`
}

func (p pullJSON) state() provider.ChangeState {
	if p.MergedAt != nil || (p.Merged != nil && *p.Merged) {
		return provider.StateMerged
	}
	if p.State == "open" {
		return provider.StateOpen
	}
	return provider.StateClosed
}

func (p pullJSON) toModel(providerID string, repo provider.Repository) (provider.ChangeRequest, error) {
	b := provider.NewChangeRequestBuilder().
		SetProviderID(providerID).
		SetRepository(repo).
		SetNumber(p.Number).
		SetTitle(p.Title).
		SetAuthor(p.User.Login).
		SetWebURL(p.HTMLURL).
		SetSourceBranch(p.Head.Ref).
		SetTargetBranch(p.Base.Ref).
		SetCreatedAt(p.CreatedAt).
		SetState(p.state()).
		SetHeadSHA(p.Head.SHA).
		SetCommitCount(p.Commits)
	if p.MergedAt != nil {
		b.SetMergedAt(*p.MergedAt)
	}
	if p.MergeCommitSHA != "" {
		b.SetMergeCommitSHA(p.MergeCommitSHA)
	}
	cr, err := b.Build()
	if err != nil {
		return provider.ChangeRequest{}, fmt.Errorf("github pull %d: %w", p.Number, err)
	}
	return cr, nil
}

type commitJSON struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

func (c commitJSON) toModel() (provider.Commit, error) {
	return provider.NewCommit(c.SHA, c.Commit.Message, c.Commit.Author.Date)
}
