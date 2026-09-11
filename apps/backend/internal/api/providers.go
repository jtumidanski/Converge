package api

import (
	"net/http"

	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
)

type providerAttributes struct {
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"baseUrl"`
}

func providerResource(p provider.GitProvider) jsonapi.Resource {
	return jsonapi.Resource{Type: "providers", ID: p.ID(), Attributes: providerAttributes{
		DisplayName: p.DisplayName(), Kind: string(p.Kind()), BaseURL: p.BaseURL(),
	}}
}

func (s *server) listProviders(w http.ResponseWriter, r *http.Request) {
	registry, err := s.deps.Providers.Resolve(r.Context(), scopeFrom(r))
	if err != nil {
		writeDomainError(w, s.deps.Log, err)
		return
	}
	all := registry.All()
	out := make([]jsonapi.Resource, 0, len(all))
	for _, p := range all {
		out = append(out, providerResource(p))
	}
	if err := jsonapi.WriteList(w, http.StatusOK, out, nil); err != nil {
		s.deps.Log.Error("write providers failed", "error", err)
	}
}
