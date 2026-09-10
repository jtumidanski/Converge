package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewID returns 8 lowercase hex characters from a CSPRNG.
func NewID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("session: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
