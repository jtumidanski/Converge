package auth

import (
	"errors"
	"testing"
)

// TestDummyHashCostsExactlyWhatARealVerifyCosts is the other half of the
// FR-2.5 timing guard, and the deterministic one: verifying against the dummy
// can only cost what verifying a real stored hash costs if it carries the same
// Argon2id parameters. Asserting the parameters is exact, where a wall-clock
// comparison is a measurement of the machine.
func TestDummyHashCostsExactlyWhatARealVerifyCosts(t *testing.T) {
	t.Parallel()
	stored, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	realParams, _, _, err := decodePHC(stored)
	if err != nil {
		t.Fatalf("decodePHC(stored): %v", err)
	}
	dummyParams, _, _, err := decodePHC(dummyHash)
	if err != nil {
		t.Fatalf("decodePHC(dummyHash): %v", err)
	}
	if dummyParams != realParams {
		t.Fatalf("dummyHash parameters %+v differ from a real hash's %+v; the unknown-username path would cost a different amount and disclose that the username does not exist",
			dummyParams, realParams)
	}
}

// TestDummyHashNeverMatchesAnyPassword proves the FR-2.5 timing fixture is a
// well-formed Argon2id encoding (so Login's verify against it costs the same
// as a real one) that can never verify true for any candidate password: it
// is derived from a random secret nothing else can supply.
func TestDummyHashNeverMatchesAnyPassword(t *testing.T) {
	t.Parallel()
	for _, candidate := range []string{"", "password", "correct horse battery staple"} {
		if err := VerifyPassword(dummyHash, candidate); !errors.Is(err, ErrPasswordMismatch) {
			t.Fatalf("VerifyPassword(dummyHash, %q) = %v, want ErrPasswordMismatch", candidate, err)
		}
	}
}
