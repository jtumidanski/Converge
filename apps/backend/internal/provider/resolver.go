package provider

import (
	"context"

	"github.com/jtumidanski/converge/internal/identity"
)

// Resolver yields the provider registry visible to a scope.
//
// This exists because in hosted mode the set of providers — and the tokens
// inside them — is a function of the caller, so a process-global
// *Registry no longer describes the system. Standalone mode, converge-cli,
// and every existing test use NewStaticResolver and behave exactly as before.
//
// The hosted implementation lives in internal/auth, not here: it needs the
// database and the decryption key, and this package must gain neither. It
// satisfies this interface from the outside, which is the direction the
// dependency should run.
type Resolver interface {
	Resolve(ctx context.Context, scope identity.Scope) (*Registry, error)
}

type staticResolver struct{ registry *Registry }

// NewStaticResolver wraps a prebuilt registry and ignores the scope.
func NewStaticResolver(r *Registry) Resolver { return staticResolver{registry: r} }

func (s staticResolver) Resolve(context.Context, identity.Scope) (*Registry, error) {
	return s.registry, nil
}
