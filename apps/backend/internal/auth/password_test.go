package auth_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"

	"github.com/jtumidanski/converge/internal/auth"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	const pw = "correct horse battery staple"
	encoded, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := auth.VerifyPassword(encoded, pw); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if err := auth.VerifyPassword(encoded, pw+"x"); !errors.Is(err, auth.ErrPasswordMismatch) {
		t.Fatalf("wrong password gave %v, want ErrPasswordMismatch", err)
	}
}

// TestHashIsSaltedPerCall proves two hashes of the same password differ, so
// the stored value is not a lookup key for a rainbow table.
func TestHashIsSaltedPerCall(t *testing.T) {
	t.Parallel()
	a, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical; the salt is not random")
	}
}

// TestEncodedFormIsPHC pins the stored shape at the documented parameters
// (FR-2.3) so a later parameter bump is a visible, deliberate change.
func TestEncodedFormIsPHC(t *testing.T) {
	t.Parallel()
	encoded, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=1,p=4$") {
		t.Fatalf("encoded = %q, want the documented Argon2id PHC prefix", encoded)
	}
	if n := strings.Count(encoded, "$"); n != 5 {
		t.Fatalf("encoded = %q, want 5 '$' separators", encoded)
	}
}

// TestVerifyReadsParamsFromTheString is the FR-2.3 requirement that matters
// most for a future parameter raise: a hash written under parameters other
// than the package constants must still verify, because verification reads
// m/t/p from the stored string.
//
// The fixture is computed here rather than pasted as a literal, so the test
// carries no magic constant and stays correct if the PHC encoding changes.
// The parameters are deliberately far below the production ones (m=8 KiB
// instead of 64 MiB, p=1 instead of 4), so a VerifyPassword that ignored the
// stored parameters and used the constants would produce a different key and
// fail.
func TestVerifyReadsParamsFromTheString(t *testing.T) {
	t.Parallel()
	const pw = "hunter2hunter2"
	salt := []byte("saltsaltsaltsalt")
	var (
		weakTime    uint32 = 1
		weakMemory  uint32 = 8
		weakThreads uint8  = 1
		keyLen      uint32 = 32
	)
	key := argon2.IDKey([]byte(pw), salt, weakTime, weakMemory, weakThreads, keyLen)
	enc := base64.RawStdEncoding
	weak := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		weakMemory, weakTime, weakThreads,
		enc.EncodeToString(salt), enc.EncodeToString(key))

	if err := auth.VerifyPassword(weak, pw); err != nil {
		t.Fatalf("VerifyPassword against weak params: %v", err)
	}
	if err := auth.VerifyPassword(weak, pw+"x"); !errors.Is(err, auth.ErrPasswordMismatch) {
		t.Fatalf("wrong password against weak params gave %v, want ErrPasswordMismatch", err)
	}
}

func TestVerifyRejectsMalformedEncodings(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"",
		"not-a-phc-string",
		"$argon2i$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",  // wrong variant
		"$argon2id$v=18$m=65536,t=1,p=4$c2FsdA$a2V5", // wrong version
		"$argon2id$v=19$m=x,t=1,p=4$c2FsdA$a2V5",     // non-numeric memory
		"$argon2id$v=19$m=65536,t=1,p=4$!!!$a2V5",    // bad base64 salt
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA",      // too few fields
	} {
		if err := auth.VerifyPassword(bad, "whatever"); err == nil {
			t.Errorf("VerifyPassword(%q) = nil, want an error", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	t.Parallel()
	// FR-2.2: length is the only rule. No composition requirements.
	if err := auth.ValidatePassword("12345678"); err != nil {
		t.Fatalf("8 characters rejected: %v", err)
	}
	if err := auth.ValidatePassword(strings.Repeat("a", 1024)); err != nil {
		t.Fatalf("1024 characters rejected: %v", err)
	}
	for _, bad := range []string{"", "1234567", strings.Repeat("a", 1025)} {
		var ae *auth.Error
		err := auth.ValidatePassword(bad)
		if !errors.As(err, &ae) || ae.Code != auth.CodeWeakPassword {
			t.Errorf("ValidatePassword(len %d) = %v, want WEAK_PASSWORD", len(bad), err)
		}
	}
}
