package api

import (
	"net/http"
	"strconv"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
)

type repositoryAttributes struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace"`
	DefaultBranch string `json:"defaultBranch"`
	WebURL        string `json:"webUrl"`
}

func repositoryResource(r provider.Repository) jsonapi.Resource {
	return jsonapi.Resource{Type: "repositories", ID: r.FullName(), Attributes: repositoryAttributes{
		Name: r.Name(), Namespace: r.Namespace(), DefaultBranch: r.DefaultBranch(), WebURL: r.WebURL(),
	}}
}

// pageFrom reads ?page= and ?pageSize=.
func pageFrom(r *http.Request) provider.Page {
	p := provider.Page{}
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil {
		p.Number = v
	}
	size := r.URL.Query().Get("pageSize")
	if size == "" {
		size = r.URL.Query().Get("size")
	}
	if v, err := strconv.Atoi(size); err == nil {
		p.Size = v
	}
	return p.Normalize()
}

func (s *server) providerFor(w http.ResponseWriter, r *http.Request) (provider.GitProvider, bool) {
	registry, err := s.deps.Providers.Resolve(r.Context(), scopeFrom(r))
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return nil, false
	}
	id := r.PathValue("provider")
	p, ok := registry.Get(id)
	if !ok {
		// 404 rather than 403 even when the slug belongs to another user:
		// resource existence is not disclosed (FR-4.2).
		_ = jsonapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", jsonapi.StatusTitle(http.StatusNotFound), "No provider is configured with that id.")
		return nil, false
	}
	return p, true
}

// repoNameFrom reads {repo} (already percent-decoded by ServeMux) or ?repo=.
func repoNameFrom(r *http.Request) (string, error) {
	name := r.PathValue("repo")
	if name == "" {
		name = r.URL.Query().Get("repo")
	}
	if err := gitx.ValidateRepoFullName(name); err != nil {
		return "", err
	}
	return name, nil
}

func (s *server) listRepositories(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	page := pageFrom(r)
	res, err := p.ListRepositories(r.Context(), page)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	out := make([]jsonapi.Resource, 0, len(res.Items))
	for _, repo := range res.Items {
		out = append(out, repositoryResource(repo))
	}
	meta := &jsonapi.Meta{Page: &jsonapi.PageMeta{Number: page.Number, Size: page.Size, HasNext: res.HasNext}}
	if err := jsonapi.WriteList(w, http.StatusOK, out, meta); err != nil {
		s.deps.Log.Error("write repositories failed", "error", err)
	}
}

func (s *server) getRepository(w http.ResponseWriter, r *http.Request) {
	p, ok := s.providerFor(w, r)
	if !ok {
		return
	}
	name, err := repoNameFrom(r)
	if err != nil {
		_ = jsonapi.WriteError(w, http.StatusBadRequest, "INVALID_REPOSITORY", jsonapi.StatusTitle(http.StatusBadRequest), "The repository must look like owner/name.")
		return
	}
	repo, err := p.GetRepository(r.Context(), name)
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	if err := jsonapi.WriteOne(w, http.StatusOK, repositoryResource(repo)); err != nil {
		s.deps.Log.Error("write repository failed", "error", err)
	}
}
