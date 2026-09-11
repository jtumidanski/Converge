package auth

import "time"

// Attempt is one throttle counter row: failures against a folded username
// ("user" scope) or a client IP ("ip" scope). Persisted so a restart does not
// clear a lockout in progress (FR-7.1).
type Attempt struct {
	Scope       string
	Key         string
	Failures    int
	WindowStart time.Time
	LockedUntil time.Time
}

// Attempt scopes.
const (
	ScopeUser = "user"
	ScopeIP   = "ip"
)
