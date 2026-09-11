package provider

import (
	"fmt"
	"time"

	"github.com/jtumidanski/converge/internal/gitx"
)

// Kind identifies the provider implementation.
type Kind string

const (
	KindGitHub Kind = "github"
	KindGitLab Kind = "gitlab"
)

// Repository is an immutable provider repository.
type Repository struct {
	providerID    string
	fullName      string
	name          string
	namespace     string
	defaultBranch string
	webURL        string
	cloneURL      string
}

func (r Repository) ProviderID() string    { return r.providerID }
func (r Repository) FullName() string      { return r.fullName }
func (r Repository) Name() string          { return r.name }
func (r Repository) Namespace() string     { return r.namespace }
func (r Repository) DefaultBranch() string { return r.defaultBranch }
func (r Repository) WebURL() string        { return r.webURL }
func (r Repository) CloneURL() string      { return r.cloneURL }

// Branch is an immutable provider branch reference.
type Branch struct {
	name      string
	sha       string
	isDefault bool
}

func (b Branch) Name() string    { return b.name }
func (b Branch) SHA() string     { return b.sha }
func (b Branch) IsDefault() bool { return b.isDefault }

// Commit is an immutable commit reference.
type Commit struct {
	sha        string
	message    string
	authoredAt time.Time
}

// NewCommit validates the SHA.
func NewCommit(sha, message string, authoredAt time.Time) (Commit, error) {
	if err := gitx.ValidateSHA(sha); err != nil {
		return Commit{}, fmt.Errorf("commit: %w", err)
	}
	return Commit{sha: sha, message: message, authoredAt: authoredAt}, nil
}

func (c Commit) SHA() string           { return c.sha }
func (c Commit) Message() string       { return c.message }
func (c Commit) AuthoredAt() time.Time { return c.authoredAt }

// ChangeState is the provider-reported state.
type ChangeState string

const (
	StateMerged ChangeState = "merged"
	StateOpen   ChangeState = "open"
	StateClosed ChangeState = "closed"
)

// ChangeRequest is an immutable PR/MR.
type ChangeRequest struct {
	providerID      string
	repository      Repository
	number          int
	title           string
	author          string
	webURL          string
	sourceBranch    string
	targetBranch    string
	createdAt       time.Time
	mergedAt        time.Time
	state           ChangeState
	commits         []Commit
	commitCount     int
	mergeCommitSHA  string
	squashCommitSHA string
	headSHA         string
	squashed        bool
}

func (c ChangeRequest) ProviderID() string      { return c.providerID }
func (c ChangeRequest) Repository() Repository  { return c.repository }
func (c ChangeRequest) Number() int             { return c.number }
func (c ChangeRequest) Title() string           { return c.title }
func (c ChangeRequest) Author() string          { return c.author }
func (c ChangeRequest) WebURL() string          { return c.webURL }
func (c ChangeRequest) SourceBranch() string    { return c.sourceBranch }
func (c ChangeRequest) TargetBranch() string    { return c.targetBranch }
func (c ChangeRequest) CreatedAt() time.Time    { return c.createdAt }
func (c ChangeRequest) MergedAt() time.Time     { return c.mergedAt }
func (c ChangeRequest) State() ChangeState      { return c.state }
func (c ChangeRequest) CommitCount() int        { return c.commitCount }
func (c ChangeRequest) MergeCommitSHA() string  { return c.mergeCommitSHA }
func (c ChangeRequest) SquashCommitSHA() string { return c.squashCommitSHA }
func (c ChangeRequest) HeadSHA() string         { return c.headSHA }
func (c ChangeRequest) Squashed() bool          { return c.squashed }

// Commits returns a copy of the commit list.
func (c ChangeRequest) Commits() []Commit {
	out := make([]Commit, len(c.commits))
	copy(out, c.commits)
	return out
}

// WithCommits returns a copy carrying the given commits.
func (c ChangeRequest) WithCommits(commits []Commit) ChangeRequest {
	c.commits = append([]Commit(nil), commits...)
	if c.commitCount == 0 {
		c.commitCount = len(commits)
	}
	return c
}

// LandingCandidates lists SHAs to probe, in order (empty entries skipped).
func (c ChangeRequest) LandingCandidates() []string {
	var out []string
	for _, s := range []string{c.mergeCommitSHA, c.squashCommitSHA, c.headSHA} {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
