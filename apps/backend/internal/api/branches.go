package api

import (
	"log/slog"
	"net/http"

	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
)

type branchAttributes struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
	SHA       string `json:"sha"`
}

func branchResource(providerID, repo string, b provider.Branch) jsonapi.Resource {
	return jsonapi.Resource{
		Type: "branches",
		ID:   providerID + ":" + repo + ":" + b.Name(),
		Attributes: branchAttributes{
			Name: b.Name(), IsDefault: b.IsDefault(), SHA: b.SHA(),
		},
	}
}

// listBranches serves GET /api/providers/{provider}/repositories/{repo}/branches.
//
// The repository is resolved first (as listChanges does) so an unknown
// repository is a 404 before any branch call is made, and page 1 puts the
// default branch first when it appears there -- the only ordering guarantee
// the backend makes. The UI independently pins the repository's defaultBranch
// at the top of its select, so that guarantee is sufficient.
func (s *server) listBranches(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	name, err := repoNameFrom(r)
	if err != nil {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_REPOSITORY", jsonapi.StatusTitle(http.StatusBadRequest), "The repository must look like owner/name.")
		return
	}
	search, ok := searchFrom(w, r)
	if !ok {
		return
	}
	repo, err := p.GetRepository(r.Context(), name)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	page := pageFrom(r)
	s.deps.Log.Debug("list branches",
		slog.String("provider", p.ID()), slog.String("repository", name),
		slog.Int("page", page.Number), slog.Int("pageSize", page.Size))
	res, err := p.ListBranches(r.Context(), repo, search, page)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	items := defaultFirst(res.Items, repo.DefaultBranch(), page.Number)
	out := make([]jsonapi.Resource, 0, len(items))
	for _, b := range items {
		out = append(out, branchResource(p.ID(), name, b))
	}
	meta := &jsonapi.Meta{Page: &jsonapi.PageMeta{Number: page.Number, Size: page.Size, HasNext: res.HasNext}}
	if err := jsonapi.WriteList(w, http.StatusOK, out, meta); err != nil {
		s.deps.Log.Error("write branches failed", "error", err)
	}
}

// defaultFirst moves the default branch to the front of page 1 without
// otherwise disturbing provider order. Later pages are returned untouched:
// reordering across pages would need the whole list, which is exactly what
// paging exists to avoid.
func defaultFirst(items []provider.Branch, defaultBranch string, pageNumber int) []provider.Branch {
	if pageNumber != 1 || defaultBranch == "" || len(items) < 2 {
		return items
	}
	for i, b := range items {
		if b.Name() != defaultBranch {
			continue
		}
		if i == 0 {
			return items
		}
		out := make([]provider.Branch, 0, len(items))
		out = append(out, items[i])
		out = append(out, items[:i]...)
		out = append(out, items[i+1:]...)
		return out
	}
	return items
}
