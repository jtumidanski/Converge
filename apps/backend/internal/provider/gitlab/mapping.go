package gitlab

import (
	"fmt"
	"sort"
	"time"

	"github.com/jtumidanski/converge/internal/provider"
)

type projectJSON struct {
	Name              string `json:"name"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"path_with_namespace"`
	Namespace         struct {
		FullPath string `json:"full_path"`
	} `json:"namespace"`
	DefaultBranch string `json:"default_branch"`
	WebURL        string `json:"web_url"`
	HTTPURLToRepo string `json:"http_url_to_repo"`
}

func (p projectJSON) toModel(providerID string) (provider.Repository, error) {
	return provider.NewRepositoryBuilder().SetProviderID(providerID).SetFullName(p.PathWithNamespace).SetName(p.Path).
		SetNamespace(p.Namespace.FullPath).SetDefaultBranch(p.DefaultBranch).SetWebURL(p.WebURL).SetCloneURL(p.HTTPURLToRepo).Build()
}

type mrJSON struct {
	IID    int    `json:"iid"`
	Title  string `json:"title"`
	State  string `json:"state"`
	WebURL string `json:"web_url"`
	Author struct {
		Username string `json:"username"`
	} `json:"author"`
	CreatedAt       time.Time  `json:"created_at"`
	MergedAt        *time.Time `json:"merged_at"`
	SourceBranch    string     `json:"source_branch"`
	TargetBranch    string     `json:"target_branch"`
	SHA             string     `json:"sha"`
	MergeCommitSHA  *string    `json:"merge_commit_sha"`
	SquashCommitSHA *string    `json:"squash_commit_sha"`
	Squash          bool       `json:"squash"`
}

func (m mrJSON) state() provider.ChangeState {
	switch m.State {
	case "merged":
		return provider.StateMerged
	case "opened", "locked":
		return provider.StateOpen
	}
	return provider.StateClosed
}

func (m mrJSON) toModel(providerID string, repo provider.Repository) (provider.ChangeRequest, error) {
	b := provider.NewChangeRequestBuilder().SetProviderID(providerID).SetRepository(repo).SetNumber(m.IID).SetTitle(m.Title).
		SetAuthor(m.Author.Username).SetWebURL(m.WebURL).SetSourceBranch(m.SourceBranch).SetTargetBranch(m.TargetBranch).
		SetCreatedAt(m.CreatedAt).SetState(m.state()).SetHeadSHA(m.SHA).SetSquashed(m.Squash)
	if m.MergedAt != nil {
		b.SetMergedAt(*m.MergedAt)
	}
	if m.MergeCommitSHA != nil && *m.MergeCommitSHA != "" {
		b.SetMergeCommitSHA(*m.MergeCommitSHA)
	}
	if m.SquashCommitSHA != nil && *m.SquashCommitSHA != "" {
		b.SetSquashCommitSHA(*m.SquashCommitSHA)
	}
	cr, err := b.Build()
	if err != nil {
		return provider.ChangeRequest{}, fmt.Errorf("gitlab mr %d: %w", m.IID, err)
	}
	return cr, nil
}

type commitJSON struct {
	ID           string    `json:"id"`
	Message      string    `json:"message"`
	AuthoredDate time.Time `json:"authored_date"`
}

// toCommits maps GitLab's newest-first list into oldest-first order.
func toCommits(raw []commitJSON) ([]provider.Commit, error) {
	out := make([]provider.Commit, 0, len(raw))
	for _, r := range raw {
		c, err := provider.NewCommit(r.ID, r.Message, r.AuthoredDate)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// sortMergedDesc orders by merged_at desc then iid desc.
func sortMergedDesc(items []provider.ChangeRequest) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].MergedAt().Equal(items[j].MergedAt()) {
			return items[i].MergedAt().After(items[j].MergedAt())
		}
		return items[i].Number() > items[j].Number()
	})
}
