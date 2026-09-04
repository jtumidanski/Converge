package gitx

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCredentialEnv(t *testing.T) {
	env, err := CredentialEnv("https://gitlab.example.com/group/proj.git", "oauth2", "glpat-secret")
	if err != nil {
		t.Fatal(err)
	}
	want := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("oauth2:glpat-secret"))
	if len(env) != 3 || env[0] != "GIT_CONFIG_COUNT=1" || env[1] != "GIT_CONFIG_KEY_0=http.https://gitlab.example.com/.extraheader" || env[2] != "GIT_CONFIG_VALUE_0="+want {
		t.Errorf("env = %q", env)
	}
	if _, err := CredentialEnv("file:///tmp/x", "u", "t"); err == nil {
		t.Error("non-http URL must be rejected")
	}
	if _, err := CredentialEnv("https://h/x", "", "t"); err == nil {
		t.Error("empty user must be rejected")
	}
	for _, e := range env {
		if strings.Contains(e, "glpat-secret") {
			t.Errorf("raw token present in %q", e)
		}
	}
}
