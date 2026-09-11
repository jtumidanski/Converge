// Package identity answers "on whose behalf" for a request. It is a leaf: it
// imports nothing, so every tier from api down to mirror can depend on it
// without inverting the module's dependency direction.
package identity

// Scope names the user a request acts for. The zero value is the standalone
// scope: unowned, unfiltered, and identical to pre-hosted behaviour.
//
// The field is unexported deliberately. A Scope cannot be forged by a struct
// literal outside this package, so the only ways to obtain one are Standalone
// (safe) and ForUser (constructed by the auth middleware from a verified
// login session). A zero value is therefore a safe default rather than a
// dangerous one.
type Scope struct {
	userID string
}

// Standalone returns the unfiltered scope used in standalone mode, by
// converge-cli, and by the system acting on its own behalf (sweepers).
func Standalone() Scope { return Scope{} }

// ForUser returns the scope for the authenticated user id.
func ForUser(id string) Scope { return Scope{userID: id} }

// UserID returns the user id, empty in standalone mode.
func (s Scope) UserID() string { return s.userID }

// IsScoped reports whether this scope names a user.
func (s Scope) IsScoped() bool { return s.userID != "" }

// Matches reports whether a record owned by owner is visible to s.
//
// Standalone (s.userID == "") sees everything, including records that carry an
// owner: an instance downgraded from hosted to standalone must still list its
// sessions (FR-6.4). A scoped s sees only an exact match, which makes an
// unowned record invisible to every user in hosted mode (FR-6.3).
func (s Scope) Matches(owner string) bool {
	if s.userID == "" {
		return true
	}
	return owner == s.userID
}
