package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretNeverLeaks(t *testing.T) {
	s := NewSecret("tok-123")
	if s.Reveal() != "tok-123" {
		t.Fatalf("Reveal = %q", s.Reveal())
	}
	for name, out := range map[string]string{
		"String": s.String(),
		"%v":     fmt.Sprintf("%v", s),
		"%+v":    fmt.Sprintf("%+v", s),
		"%s":     fmt.Sprintf("%s", s), //nolint:staticcheck // intentionally exercising the %s verb, not just String()
	} {
		if strings.Contains(out, "tok-123") || out != "[redacted]" {
			t.Errorf("%s = %q, want [redacted]", name, out)
		}
	}
	b, err := json.Marshal(struct{ T Secret }{s})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"T":"[redacted]"}` {
		t.Errorf("json = %s", b)
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "token", s)
	if strings.Contains(buf.String(), "tok-123") {
		t.Errorf("slog leaked: %s", buf.String())
	}
	if !NewSecret("").IsZero() || s.IsZero() {
		t.Error("IsZero wrong")
	}
}
