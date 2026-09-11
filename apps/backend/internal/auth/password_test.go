package auth_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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
		"$argon2i$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",                                                            // wrong variant
		"$argon2id$v=18$m=65536,t=1,p=4$c2FsdA$a2V5",                                                           // wrong version
		"$argon2id$v=19$m=x,t=1,p=4$c2FsdA$a2V5",                                                               // non-numeric memory
		"$argon2id$v=19$m=65536,t=1,p=4$!!!$a2V5",                                                              // bad base64 salt
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA",                                                                // too few fields
		"$argon2id$v=19$m=65536,t=1,p=4XYZ$c2FsdHNhbHRzYWx0c2FsdA$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2U", // trailing garbage in parameter field, otherwise well-formed
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdHNhbHRz$a2V5",                                                     // salt not 16 bytes
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdHNhbHRzYWx0c2FsdA$a2V5oth",                                        // key not 32 bytes
	} {
		// Every case here is structurally malformed and must be rejected by
		// decodePHC itself, not merely produce a key/salt that fails the
		// constant-time compare (subtle.ConstantTimeCompare already returns
		// false for mismatched lengths, so a bare "err == nil" check here
		// would not prove decodePHC enforces anything).
		err := auth.VerifyPassword(bad, "whatever")
		if err == nil {
			t.Errorf("VerifyPassword(%q) = nil, want an error", bad)
			continue
		}
		if errors.Is(err, auth.ErrPasswordMismatch) {
			t.Errorf("VerifyPassword(%q) = %v (ErrPasswordMismatch); want decodePHC to reject it as malformed", bad, err)
		}
	}
}

// TestVerifyRejectsOversizedCostParameters proves decodePHC rejects Argon2
// cost parameters above the ceilings before ever calling argon2.IDKey: a
// corrupt or tampered stored hash with an enormous memory or time cost must
// fail fast rather than driving a multi-terabyte allocation or hanging
// indefinitely. Each case here uses a value one above the documented
// ceiling, so if decodePHC ever called argon2.IDKey with it the test would
// hang or OOM instead of returning promptly.
func TestVerifyRejectsOversizedCostParameters(t *testing.T) {
	t.Parallel()
	const salt = "c2FsdHNhbHRzYWx0c2FsdA"                     // 16 raw bytes, base64
	const key = "a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2U" // 32 raw bytes, base64
	for name, bad := range map[string]string{
		"memory above ceiling":  fmt.Sprintf("$argon2id$v=19$m=%d,t=1,p=4$%s$%s", uint64(1<<20)+1, salt, key),
		"time above ceiling":    fmt.Sprintf("$argon2id$v=19$m=65536,t=%d,p=4$%s$%s", 17, salt, key),
		"threads above ceiling": fmt.Sprintf("$argon2id$v=19$m=65536,t=1,p=%d$%s$%s", 17, salt, key),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			done := make(chan error, 1)
			go func() { done <- auth.VerifyPassword(bad, "whatever") }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("VerifyPassword(%q) = nil, want an error", bad)
				}
				// The key bytes above never match anything argon2.IDKey could
				// produce, so if VerifyPassword actually hashed and compared,
				// it would still return ErrPasswordMismatch. Rejection must
				// instead come from decodePHC, which returns a distinct
				// error, before argon2.IDKey is ever called.
				if errors.Is(err, auth.ErrPasswordMismatch) {
					t.Fatalf("VerifyPassword(%q) = %v (ErrPasswordMismatch); want decodePHC to reject before hashing", bad, err)
				}
			// The ErrPasswordMismatch check above is the actual assertion; this
			// deadline only stops a regression from hanging the suite while
			// argon2 tries to allocate a terabyte. It is generous on purpose:
			// decodePHC rejects in microseconds, so a deadline tight enough to
			// be a performance assertion would just be a flake under load.
			case <-time.After(10 * time.Second):
				t.Fatalf("VerifyPassword did not return within 10s; the oversized parameter was not rejected before hashing")
			}
		})
	}
}

// TestVerifyAcceptsHashAtTheCeilings proves the ceilings introduced to fail
// closed on a corrupt row do not reject any hash HashPassword actually
// produces.
func TestVerifyAcceptsHashAtTheCeilings(t *testing.T) {
	t.Parallel()
	const pw = "correct horse battery staple"
	encoded, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := auth.VerifyPassword(encoded, pw); err != nil {
		t.Fatalf("VerifyPassword rejected a hash produced by HashPassword: %v", err)
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
