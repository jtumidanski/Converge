package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jtumidanski/converge/internal/config"
)

// secretKeyLen is the AES-256 key length. config.Load already rejects a
// CONVERGE_SECRET_KEY that does not decode to exactly this many bytes; the
// check is repeated here because NewSealer is also reachable from tests.
const secretKeyLen = 32

// Sealer encrypts and decrypts provider tokens with AES-256-GCM under the
// operator's master key.
//
// Plaintext tokens exist only in memory, for the lifetime of a decrypted
// provider registry entry (bounded by the resolver's cache TTL). They are
// never written to the database, never returned by the API, and never logged.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer derives the AEAD from the base64-encoded master key.
func NewSealer(key config.Secret) (*Sealer, error) {
	raw, err := base64.StdEncoding.DecodeString(key.Reveal())
	if err != nil {
		return nil, fmt.Errorf("auth: master key must be base64 standard encoding: %w", err)
	}
	if len(raw) != secretKeyLen {
		return nil, fmt.Errorf("auth: master key must decode to %d bytes, got %d", secretKeyLen, len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("auth: build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: build gcm: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// aad binds a ciphertext to the exact row that holds it (FR-5.3). Because the
// user id and provider row id are authenticated additional data, a ciphertext
// moved between rows or between users fails to open. The NUL separator stops
// ("ab", "c") and ("a", "bc") from producing the same AAD.
func aad(userID, providerID string) []byte {
	out := make([]byte, 0, len(userID)+1+len(providerID))
	out = append(out, userID...)
	out = append(out, 0)
	out = append(out, providerID...)
	return out
}

// Seal encrypts plaintext under a fresh random nonce, returning the
// ciphertext and that nonce for storage in the row's own columns.
func (s *Sealer) Seal(plaintext, userID, providerID string) ([]byte, []byte, error) {
	if userID == "" || providerID == "" {
		return nil, nil, errors.New("auth: seal requires a user id and a provider id")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("auth: generate nonce: %w", err)
	}
	ciphertext := s.aead.Seal(nil, nonce, []byte(plaintext), aad(userID, providerID))
	return ciphertext, nonce, nil
}

// Open decrypts a stored token, returning it wrapped so it cannot be printed,
// logged, or marshalled by accident.
func (s *Sealer) Open(ciphertext, nonce []byte, userID, providerID string) (config.Secret, error) {
	if len(nonce) != s.aead.NonceSize() {
		return config.Secret{}, fmt.Errorf("auth: nonce must be %d bytes, got %d", s.aead.NonceSize(), len(nonce))
	}
	plaintext, err := s.aead.Open(nil, nonce, ciphertext, aad(userID, providerID))
	if err != nil {
		// Deliberately does not echo the ciphertext or the ids into the
		// error: this string reaches logs.
		return config.Secret{}, fmt.Errorf("auth: decrypt provider token: %w", err)
	}
	return config.NewSecret(string(plaintext)), nil
}
