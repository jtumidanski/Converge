package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Password length bounds (FR-2.2). There are deliberately no composition
// rules — no required symbol, digit, or mixed case. Length is the only
// requirement, and that is a decision, not an oversight.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 1024
)

// Argon2id parameters (FR-2.3). They live here, in one place, so they can be
// raised later; VerifyPassword deliberately reads m/t/p from the stored
// string rather than from these constants, so raising them does not
// invalidate existing hashes.
const (
	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024 // KiB, i.e. 64 MiB
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
	argonVersion        = argon2.Version // 19
)

// Ceilings on the Argon2 cost parameters decodePHC will accept from a stored
// hash. These exist to fail closed on a corrupt or tampered row — not to
// constrain legitimate hashes, which are always written at the constants
// above. Without a ceiling, a stored m= large enough drives a multi-terabyte
// allocation (an unrecoverable Go runtime OOM) and a large t= hangs the
// verifying goroutine indefinitely.
const (
	maxArgonMemory  uint32 = 1 << 20 // KiB (1 GiB); 16x the production 65536 KiB
	maxArgonTime    uint32 = 16      // 16x the production value of 1
	maxArgonThreads uint8  = 16      // 4x the production value of 4
)

// ErrPasswordMismatch reports a correct encoding that does not match the
// supplied password. A malformed encoding returns a different error.
var ErrPasswordMismatch = errors.New("auth: password does not match")

// ValidatePassword enforces FR-2.2.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLen || len(password) > MaxPasswordLen {
		return &Error{
			Code:    CodeWeakPassword,
			Message: fmt.Sprintf("A password must be between %d and %d characters.", MinPasswordLen, MaxPasswordLen),
		}
	}
	return nil
}

// HashPassword returns a PHC-encoded Argon2id hash at the current parameters.
//
// This is deliberately expensive (64 MiB, ~50 ms). Callers must not hold a
// database transaction across it: the pool has exactly one connection, so
// hashing inside a transaction would block every other query for the
// duration. auth.Service enforces the ordering (hash, then open a
// transaction) and bounds concurrency with a semaphore.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return encodePHC(phcParams{memory: argonMemory, time: argonTime, threads: argonThreads}, salt, key), nil
}

// VerifyPassword recomputes the hash using the parameters recorded in
// encoded and compares in constant time.
func VerifyPassword(encoded, password string) error {
	p, salt, want, err := decodePHC(encoded)
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(want))) //nolint:gosec // G115: want is a base64-decoded PHC key field, always a few dozen bytes; it cannot approach uint32 overflow
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

type phcParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func encodePHC(p phcParams, salt, key []byte) string {
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersion, p.memory, p.time, p.threads,
		enc.EncodeToString(salt), enc.EncodeToString(key))
}

func decodePHC(encoded string) (phcParams, []byte, []byte, error) {
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return phcParams{}, nil, nil, errors.New("auth: malformed password hash")
	}
	if parts[1] != "argon2id" {
		return phcParams{}, nil, nil, fmt.Errorf("auth: unsupported password hash variant %q", parts[1])
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return phcParams{}, nil, nil, fmt.Errorf("auth: malformed password hash version: %w", err)
	}
	if version != argonVersion {
		return phcParams{}, nil, nil, fmt.Errorf("auth: unsupported argon2 version %d", version)
	}
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return phcParams{}, nil, nil, errors.New("auth: malformed password hash parameters")
	}
	memory, err := parsePHCUint(fields[0], "m")
	if err != nil {
		return phcParams{}, nil, nil, err
	}
	timeCost, err := parsePHCUint(fields[1], "t")
	if err != nil {
		return phcParams{}, nil, nil, err
	}
	threadsVal, err := parsePHCUint(fields[2], "p")
	if err != nil {
		return phcParams{}, nil, nil, err
	}
	if memory == 0 || timeCost == 0 || threadsVal == 0 {
		return phcParams{}, nil, nil, errors.New("auth: password hash parameters must be positive")
	}
	if memory > uint64(maxArgonMemory) || timeCost > uint64(maxArgonTime) || threadsVal > uint64(maxArgonThreads) {
		return phcParams{}, nil, nil, errors.New("auth: malformed password hash parameters")
	}
	threads := uint8(threadsVal) //nolint:gosec // G115: threadsVal is bounds-checked against maxArgonThreads (16) above
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return phcParams{}, nil, nil, fmt.Errorf("auth: malformed password hash salt: %w", err)
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil {
		return phcParams{}, nil, nil, fmt.Errorf("auth: malformed password hash key: %w", err)
	}
	if len(salt) != argonSaltLen {
		return phcParams{}, nil, nil, errors.New("auth: malformed password hash salt")
	}
	if len(key) != int(argonKeyLen) {
		return phcParams{}, nil, nil, errors.New("auth: malformed password hash key")
	}
	//nolint:gosec // G115: memory and timeCost are bounds-checked against maxArgonMemory/maxArgonTime above
	return phcParams{memory: uint32(memory), time: uint32(timeCost), threads: threads}, salt, key, nil
}

// parsePHCUint parses a single "prefix=NNN" PHC parameter field. It rejects
// anything strconv.ParseUint would not accept as a whole string, so trailing
// garbage after the digits (e.g. "p=4XYZ") is a malformed encoding rather
// than a silently truncated value.
func parsePHCUint(field, prefix string) (uint64, error) {
	rest, ok := strings.CutPrefix(field, prefix+"=")
	if !ok {
		return 0, fmt.Errorf("auth: malformed password hash parameters")
	}
	v, err := strconv.ParseUint(rest, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("auth: malformed password hash parameters: %w", err)
	}
	return v, nil
}

// dummyHash equalises Login's cost between an unknown username and a wrong
// password (FR-2.5). It is generated once, at init, from a random password,
// so nothing can ever verify against it. Login verifies against this when the
// username is unknown, which makes the dominant term — one Argon2id verify —
// identical on both paths; the residual difference is one index lookup,
// nanoseconds against ~50 ms of hashing.
var dummyHash = mustDummyHash()

func mustDummyHash() string {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		// A CSPRNG failure at init is unrecoverable and would otherwise
		// leave the timing defence silently disabled.
		panic("auth: generate dummy hash secret: " + err.Error())
	}
	encoded, err := HashPassword(string(secret))
	if err != nil {
		panic("auth: generate dummy hash: " + err.Error())
	}
	return encoded
}
