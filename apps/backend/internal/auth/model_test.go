package auth_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
)

func TestErrorStatusMapping(t *testing.T) {
	t.Parallel()
	cases := map[auth.Code]int{
		auth.CodeUnauthenticated:      http.StatusUnauthorized,
		auth.CodeInvalidCredentials:   http.StatusUnauthorized,
		auth.CodeAccountLocked:        http.StatusTooManyRequests,
		auth.CodeUsernameTaken:        http.StatusConflict,
		auth.CodeInvalidUsername:      http.StatusUnprocessableEntity,
		auth.CodeWeakPassword:         http.StatusUnprocessableEntity,
		auth.CodeForbidden:            http.StatusForbidden,
		auth.CodeProviderSlugTaken:    http.StatusConflict,
		auth.CodeProviderUnauthorized: http.StatusUnprocessableEntity,
		auth.CodeProviderInUse:        http.StatusConflict,
	}
	for code, want := range cases {
		e := &auth.Error{Code: code, Message: "m"}
		if got := e.Status(); got != want {
			t.Errorf("%s: Status() = %d, want %d", code, got, want)
		}
	}
}

func TestValidateUsername(t *testing.T) {
	t.Parallel()
	valid := []string{"abc", "Alice", "a_b-c.d", "a1234567890123456789012345678901"} // 3 and 32 chars
	for _, u := range valid {
		if err := auth.ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", u, err)
		}
	}
	invalid := []string{"", "ab", "_abc", ".abc", "-abc", "a b", "a/b", "a12345678901234567890123456789012"} // 2 and 33 chars
	for _, u := range invalid {
		var ae *auth.Error
		err := auth.ValidateUsername(u)
		if !errors.As(err, &ae) || ae.Code != auth.CodeInvalidUsername {
			t.Errorf("ValidateUsername(%q) = %v, want INVALID_USERNAME", u, err)
		}
	}
}

func TestFoldIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	if auth.Fold("Alice") != auth.Fold("ALICE") || auth.Fold("Alice") != "alice" {
		t.Fatalf("Fold is not lower-casing: %q vs %q", auth.Fold("Alice"), auth.Fold("ALICE"))
	}
}

func TestValidateSlug(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"a", "github", "gitlab-internal", "a1"} {
		if err := auth.ValidateSlug(s); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{"", "-a", "A", "a_b", "a b", "a/b"} {
		if auth.ValidateSlug(s) == nil {
			t.Errorf("ValidateSlug(%q) = nil, want an error", s)
		}
	}
}

// TestNormalizeBaseURL mirrors the existing config validation (FR-5.2): an
// absolute http(s) URL, trailing slash stripped, github defaulting.
func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()
	got, err := auth.NormalizeBaseURL(config.KindGitHub, "")
	if err != nil || got != "https://api.github.com" {
		t.Fatalf("github default = %q, %v; want https://api.github.com, nil", got, err)
	}
	got, err = auth.NormalizeBaseURL(config.KindGitLab, "https://gitlab.example.com/")
	if err != nil || got != "https://gitlab.example.com" {
		t.Fatalf("trailing slash = %q, %v; want https://gitlab.example.com, nil", got, err)
	}
	if _, err := auth.NormalizeBaseURL(config.KindGitLab, ""); err == nil {
		t.Fatal("gitlab with no base url should fail")
	}
	for _, bad := range []string{"ftp://x", "not a url", "/relative", "https://"} {
		if _, err := auth.NormalizeBaseURL(config.KindGitLab, bad); err == nil {
			t.Errorf("NormalizeBaseURL(%q) = nil error, want one", bad)
		}
	}
}

func TestNewIDIsSixteenLowercaseHex(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for range 100 {
		id, err := auth.NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if len(id) != 16 {
			t.Fatalf("NewID() = %q, want 16 characters", id)
		}
		for _, c := range id {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				t.Fatalf("NewID() = %q, want lowercase hex only", id)
			}
		}
		if seen[id] {
			t.Fatalf("NewID() returned a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func TestLast4(t *testing.T) {
	t.Parallel()
	if got := auth.Last4("glpat-abcdef9f2c"); got != "9f2c" {
		t.Fatalf("Last4 = %q, want 9f2c", got)
	}
	if got := auth.Last4("ab"); got != "ab" {
		t.Fatalf("Last4 of a short token = %q, want ab", got)
	}
}
