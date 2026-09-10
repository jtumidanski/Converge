package gitx

import "testing"

func TestRedact(t *testing.T) {
	in := []byte("fatal: Authorization: Basic abc123 rejected; PRIVATE-TOKEN: glpat-x; token=glpat-x again")
	out := string(Redact(in, []string{"glpat-x", ""}))
	for _, leak := range []string{"abc123", "glpat-x"} {
		if contains(out, leak) {
			t.Errorf("leak %q in %q", leak, out)
		}
	}
	if !contains(out, "Authorization: [redacted]") || !contains(out, "PRIVATE-TOKEN: [redacted]") {
		t.Errorf("headers not redacted: %q", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
