package provider

import (
	"context"
	"testing"

	"github.com/jtumidanski/converge/internal/gitx"
)

type stubProvider struct{ id string }

func (s stubProvider) ID() string          { return s.id }
func (s stubProvider) Kind() Kind          { return KindGitHub }
func (s stubProvider) DisplayName() string { return s.id }
func (s stubProvider) BaseURL() string     { return "" }
func (s stubProvider) ListRepositories(context.Context, string, Page) (Slice[Repository], error) {
	return Slice[Repository]{}, nil
}
func (s stubProvider) ListBranches(context.Context, Repository, string, Page) (Slice[Branch], error) {
	return Slice[Branch]{}, nil
}
func (s stubProvider) GetRepository(context.Context, string) (Repository, error) {
	return Repository{}, ErrNotFound
}
func (s stubProvider) ListMergedChanges(context.Context, Repository, string, string, Page) (Slice[ChangeRequest], error) {
	return Slice[ChangeRequest]{}, nil
}
func (s stubProvider) GetChange(context.Context, Repository, int) (ChangeRequest, error) {
	return ChangeRequest{}, ErrNotFound
}
func (s stubProvider) GetChangeCommits(context.Context, Repository, int) ([]Commit, error) {
	return nil, nil
}
func (s stubProvider) CloneURL(Repository) string                { return "" }
func (s stubProvider) AuthorizeGit(Repository, *gitx.Spec) error { return nil }

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stubProvider{"b"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(stubProvider{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(stubProvider{"a"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, ok := r.Get("a"); !ok {
		t.Fatal("Get a")
	}
	if _, ok := r.Get("zz"); ok {
		t.Fatal("Get zz")
	}
	all := r.All()
	if len(all) != 2 || all[0].ID() != "a" || all[1].ID() != "b" {
		t.Fatalf("All = %v", all)
	}
}
