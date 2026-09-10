// Package fake is an in-memory GitProvider for unit and integration tests.
package fake

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/jtumidanski/converge/internal/gitx"
	"github.com/jtumidanski/converge/internal/provider"
)

// Provider stores repositories and changes in memory.
type Provider struct {
	id      string
	kind    provider.Kind
	mu      sync.Mutex
	repos   map[string]provider.Repository
	changes map[string]map[int]provider.ChangeRequest
	commits map[string]map[int][]provider.Commit
	nextErr error
}

// New creates an empty provider.
func New(id string, kind provider.Kind) *Provider {
	return &Provider{
		id:      id,
		kind:    kind,
		repos:   map[string]provider.Repository{},
		changes: map[string]map[int]provider.ChangeRequest{},
		commits: map[string]map[int][]provider.Commit{},
	}
}

func (p *Provider) ID() string          { return p.id }
func (p *Provider) Kind() provider.Kind { return p.kind }
func (p *Provider) DisplayName() string { return "Fake " + p.id }
func (p *Provider) BaseURL() string     { return "fake://" + p.id }

// AddRepository registers a repository.
func (p *Provider) AddRepository(r provider.Repository) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.repos[r.FullName()] = r
}

// AddChange registers a change request under its repository.
func (p *Provider) AddChange(cr provider.ChangeRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := cr.Repository().FullName()
	if p.changes[key] == nil {
		p.changes[key] = map[int]provider.ChangeRequest{}
	}
	p.changes[key][cr.Number()] = cr
	if len(cr.Commits()) > 0 {
		p.setCommitsLocked(key, cr.Number(), cr.Commits())
	}
}

// SetCommits sets the commits returned by GetChangeCommits.
func (p *Provider) SetCommits(fullName string, number int, commits []provider.Commit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setCommitsLocked(fullName, number, commits)
}

func (p *Provider) setCommitsLocked(fullName string, number int, commits []provider.Commit) {
	if p.commits[fullName] == nil {
		p.commits[fullName] = map[int][]provider.Commit{}
	}
	p.commits[fullName][number] = commits
}

// FailWith makes the next call return err.
func (p *Provider) FailWith(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextErr = err
}

func (p *Provider) takeErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.nextErr
	p.nextErr = nil
	return err
}

func (p *Provider) ListRepositories(_ context.Context, page provider.Page) (provider.Slice[provider.Repository], error) {
	if err := p.takeErr(); err != nil {
		return provider.Slice[provider.Repository]{}, err
	}
	p.mu.Lock()
	all := make([]provider.Repository, 0, len(p.repos))
	for _, r := range p.repos {
		all = append(all, r)
	}
	p.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].FullName() < all[j].FullName() })
	return paginate(all, page), nil
}

func (p *Provider) GetRepository(_ context.Context, fullName string) (provider.Repository, error) {
	if err := p.takeErr(); err != nil {
		return provider.Repository{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.repos[fullName]
	if !ok {
		return provider.Repository{}, provider.ErrNotFound
	}
	return r, nil
}

func (p *Provider) ListMergedChanges(_ context.Context, repo provider.Repository, target, search string, page provider.Page) (provider.Slice[provider.ChangeRequest], error) {
	if err := p.takeErr(); err != nil {
		return provider.Slice[provider.ChangeRequest]{}, err
	}
	p.mu.Lock()
	var all []provider.ChangeRequest
	for _, cr := range p.changes[repo.FullName()] {
		if cr.State() == provider.StateMerged && cr.TargetBranch() == target && matches(cr, search) {
			all = append(all, cr)
		}
	}
	p.mu.Unlock()
	sort.Slice(all, func(i, j int) bool {
		if !all[i].MergedAt().Equal(all[j].MergedAt()) {
			return all[i].MergedAt().After(all[j].MergedAt())
		}
		return all[i].Number() > all[j].Number()
	})
	return paginate(all, page), nil
}

func matches(cr provider.ChangeRequest, search string) bool {
	if search == "" {
		return true
	}
	if n, ok := provider.ParseSearchNumber(search); ok {
		return cr.Number() == n
	}
	s := strings.ToLower(search)
	return strings.Contains(strings.ToLower(cr.Title()), s) || strings.Contains(strings.ToLower(cr.Author()), s)
}

func (p *Provider) GetChange(_ context.Context, repo provider.Repository, number int) (provider.ChangeRequest, error) {
	if err := p.takeErr(); err != nil {
		return provider.ChangeRequest{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cr, ok := p.changes[repo.FullName()][number]
	if !ok {
		return provider.ChangeRequest{}, provider.ErrNotFound
	}
	return cr.WithCommits(nil), nil
}

func (p *Provider) GetChangeCommits(_ context.Context, repo provider.Repository, number int) ([]provider.Commit, error) {
	if err := p.takeErr(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.changes[repo.FullName()][number]; !ok {
		return nil, provider.ErrNotFound
	}
	return append([]provider.Commit(nil), p.commits[repo.FullName()][number]...), nil
}

func (p *Provider) CloneURL(repo provider.Repository) string { return repo.CloneURL() }

func (p *Provider) AuthorizeGit(provider.Repository, *gitx.Spec) error { return nil }

func paginate[T any](all []T, page provider.Page) provider.Slice[T] {
	page = page.Normalize()
	start := (page.Number - 1) * page.Size
	if start >= len(all) {
		return provider.Slice[T]{Items: []T{}}
	}
	end := start + page.Size
	if end > len(all) {
		end = len(all)
	}
	return provider.Slice[T]{Items: all[start:end], HasNext: end < len(all)}
}

var _ provider.GitProvider = (*Provider)(nil)
