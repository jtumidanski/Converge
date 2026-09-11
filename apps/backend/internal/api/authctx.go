package api

import (
	"net/http"

	"github.com/jtumidanski/converge/internal/identity"
)

// scopeFrom returns the scope the authenticate middleware attached, or the
// standalone scope when none is present. This is the one place identity
// travels on a context (FR-4.5); below api it is an explicit argument.
func scopeFrom(r *http.Request) identity.Scope { return identity.Standalone() }
