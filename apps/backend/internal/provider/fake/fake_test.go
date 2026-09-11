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

func branch(t *testing.T, name string, isDefault bool) provider.Branch {
	t.Helper()
	b, err := provider.NewBranchBuilder().SetName(name).SetDefault(isDefault).Build()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestListBranchesOrdersDefaultFirstThenByName(t *testing.T) {
	p := New("fake", provider.KindGitLab)
	r := repo(t, "atlas/server")
	p.AddRepository(r)
	p.AddBranch("atlas/server", branch(t, "release/1.0", false))
	p.AddBranch("atlas/server", branch(t, "develop", false))
	p.AddBranch("atlas/server", branch(t, "main", true))

	got, err := p.ListBranches(context.Background(), r, "", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, b := range got.Items {
		names = append(names, b.Name())
	}
	want := []string{"main", "develop", "release/1.0"}
	for i := range want {
		if i >= len(names) || names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}

func TestListBranchesFiltersBySubstring(t *testing.T) {
	p := New("fake", provider.KindGitLab)
	r := repo(t, "atlas/server")
	p.AddRepository(r)
	p.AddBranch("atlas/server", branch(t, "main", true))
	p.AddBranch("atlas/server", branch(t, "release/1.0", false))

	got, err := p.ListBranches(context.Background(), r, "rel", provider.Page{Number: 1, Size: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Name() != "release/1.0" {
		t.Fatalf("items = %+v", got.Items)
	}
}
