package config

import "log/slog"

const redacted = "[redacted]"

// Secret wraps a credential so it cannot be printed, logged, or marshalled by accident.
type Secret struct{ v string }

// NewSecret wraps v.
func NewSecret(v string) Secret { return Secret{v: v} }

// Reveal returns the raw value. Call sites: provider HTTP headers and gitx credential env only.
func (s Secret) Reveal() string { return s.v }

// IsZero reports whether the secret is empty.
func (s Secret) IsZero() bool { return s.v == "" }

func (s Secret) String() string { return redacted }

// MarshalJSON always emits the redaction marker.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }
