package provider

import (
	"fmt"
	"sort"
)

// Registry maps provider IDs to implementations.
type Registry struct {
	byID map[string]GitProvider
}

func NewRegistry() *Registry { return &Registry{byID: map[string]GitProvider{}} }

// Register adds p; duplicate IDs are rejected.
func (r *Registry) Register(p GitProvider) error {
	if _, dup := r.byID[p.ID()]; dup {
		return fmt.Errorf("provider %q registered twice", p.ID())
	}
	r.byID[p.ID()] = p
	return nil
}

func (r *Registry) Get(id string) (GitProvider, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// All returns providers sorted by ID.
func (r *Registry) All() []GitProvider {
	out := make([]GitProvider, 0, len(r.byID))
	for _, p := range r.byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
