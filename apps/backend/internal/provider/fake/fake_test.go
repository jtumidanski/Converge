package fake

import (
	"context"
	"testing"

	"github.com/jtumidanski/converge/internal/provider"
)

func repo(t *testing.T, fullName string) provider.Repository {
	t.Helper()
	r, err := provider.NewRepositoryBuilder().SetProviderID("fake").SetFullName(fullName).
		SetDefaultBranch("main").SetCloneURL("https://example.test/" + fullName + ".git").Build()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestListRepositoriesFiltersBySubstring(t *testing.T) {
	p := New("fake", provider.KindGitLab)
	p.AddRepository(repo(t, "atlas/server"))
	p.AddRepository(repo(t, "atlas/client"))
	p.AddRepository(repo(t, "other/SERVER-tools"))

	got, err := p.ListRepositories(context.Background(), "serv", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2 (case-insensitive substring)", len(got.Items))
	}
	if got.Items[0].FullName() != "atlas/server" || got.Items[1].FullName() != "other/SERVER-tools" {
		t.Errorf("items = %+v, want sorted by full name", got.Items)
	}
}

func TestListRepositoriesEmptySearchReturnsAll(t *testing.T) {
	p := New("fake", provider.KindGitLab)
	p.AddRepository(repo(t, "atlas/server"))
	p.AddRepository(repo(t, "atlas/client"))
	got, err := p.ListRepositories(context.Background(), "", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(got.Items))
	}
}
