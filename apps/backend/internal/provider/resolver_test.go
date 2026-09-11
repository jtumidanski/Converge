package provider_test

import (
	"context"
	"testing"

	"github.com/jtumidanski/converge/internal/identity"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/provider/fake"
)

// TestStaticResolverIgnoresTheScope is the standalone contract: one registry,
// the same one for everyone, including converge-cli and every existing test.
func TestStaticResolverIgnoresTheScope(t *testing.T) {
	t.Parallel()
	registry := provider.NewRegistry()
	if err := registry.Register(fake.New("gh", provider.KindGitHub)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r := provider.NewStaticResolver(registry)
	for _, scope := range []identity.Scope{identity.Standalone(), identity.ForUser("anyone")} {
		got, err := r.Resolve(context.Background(), scope)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got != registry {
			t.Fatal("static resolver returned a different registry")
		}
	}
}
