package auth_test

import (
	"testing"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
)

// key32 is 32 bytes of 0x01, base64 standard encoding. Test fixture only.
const key32 = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

func sealer(t *testing.T) *auth.Sealer {
	t.Helper()
	s, err := auth.NewSealer(config.NewSecret(key32))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return s
}

func TestSealOpenRoundTrip(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	const token = "glpat-xxxxxxxxxxxxxxxxxxxx"
	ct, nonce, err := s.Seal(token, "user1", "prov1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if string(ct) == token {
		t.Fatal("ciphertext equals the plaintext")
	}
	got, err := s.Open(ct, nonce, "user1", "prov1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got.Reveal() != token {
		t.Fatalf("Open = %q, want the original token", got.Reveal())
	}
}

func TestSealUsesAFreshNoncePerCall(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	_, n1, err := s.Seal("t", "user1", "prov1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	_, n2, err := s.Seal("t", "user1", "prov1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if string(n1) == string(n2) {
		t.Fatal("two Seal calls produced the same nonce")
	}
}

// TestCiphertextIsBoundToItsRow is the FR-5.3 guarantee and an explicit
// acceptance criterion: a ciphertext copied into another user's row, or
// another provider row of the same user, must fail to decrypt.
func TestCiphertextIsBoundToItsRow(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	ct, nonce, err := s.Seal("glpat-secret", "userA", "provA")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	for _, tc := range []struct{ name, user, provider string }{
		{"foreign user", "userB", "provA"},
		{"foreign provider row", "userA", "provB"},
		{"both foreign", "userB", "provB"},
	} {
		if _, err := s.Open(ct, nonce, tc.user, tc.provider); err == nil {
			t.Errorf("%s: Open succeeded, want an authentication failure", tc.name)
		}
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	ct, nonce, err := s.Seal("glpat-secret", "userA", "provA")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	ct[0] ^= 0xff
	if _, err := s.Open(ct, nonce, "userA", "provA"); err == nil {
		t.Fatal("Open accepted a tampered ciphertext")
	}
}

func TestNewSealerRejectsBadKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, key string }{
		{"empty", ""},
		{"not base64", "!!!!"},
		{"too short", "AQEB"},
	} {
		if _, err := auth.NewSealer(config.NewSecret(tc.key)); err == nil {
			t.Errorf("NewSealer(%s) = nil error, want one", tc.name)
		}
	}
}

// TestOpenReturnsARedactedSecret proves the decrypted token comes back
// wrapped, so it cannot be printed, logged, or marshalled by accident.
func TestOpenReturnsARedactedSecret(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	ct, nonce, err := s.Seal("glpat-secret", "userA", "provA")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := s.Open(ct, nonce, "userA", "provA")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got.String() == "glpat-secret" {
		t.Fatal("String() revealed the token")
	}
}

// TestOpenRejectsTamperedNonce proves the nonce is part of what the caller
// must supply correctly: flipping a bit in it must not silently decrypt to
// garbage that happens to look valid — GCM must reject it outright.
func TestOpenRejectsTamperedNonce(t *testing.T) {
	t.Parallel()
	s := sealer(t)
	ct, nonce, err := s.Seal("glpat-secret", "userA", "provA")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	nonce[0] ^= 0xff
	if _, err := s.Open(ct, nonce, "userA", "provA"); err == nil {
		t.Fatal("Open accepted a tampered nonce")
	}
}
