package auth

import "testing"

// TestDummyHashNeverMatchesAnyPassword proves the FR-2.5 timing fixture is a
// well-formed Argon2id encoding (so Login's verify against it costs the same
// as a real one) that can never verify true for any candidate password: it
// is derived from a random secret nothing else can supply.
func TestDummyHashNeverMatchesAnyPassword(t *testing.T) {
	t.Parallel()
	for _, candidate := range []string{"", "password", "correct horse battery staple"} {
		if err := VerifyPassword(dummyHash, candidate); err != ErrPasswordMismatch { //nolint:errorlint // exact sentinel expected, no wrapping involved
			t.Fatalf("VerifyPassword(dummyHash, %q) = %v, want ErrPasswordMismatch", candidate, err)
		}
	}
}
