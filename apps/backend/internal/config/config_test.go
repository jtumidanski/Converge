package config

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func baseEnv() []string {
	return []string{
		"PROVIDERS__GITLAB_WORK__TYPE=gitlab",
		"PROVIDERS__GITLAB_WORK__BASE_URL=https://gitlab.company.com/",
		"PROVIDERS__GITLAB_WORK__TOKEN=glpat-x",
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"PROVIDERS__GH__DISPLAY_NAME=GitHub.com",
		"UNRELATED=1",
	}
}

func TestLoadDefaultsAndProviders(t *testing.T) {
	cfg, err := Load(baseEnv())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8080 || cfg.WorkspaceRoot != "/data/workspaces" || cfg.RepositoryCacheRoot != "/data/repositories" {
		t.Errorf("defaults wrong: %+v", cfg)
	}
	if cfg.SessionTTL != 24*time.Hour || cfg.CleanupInterval != 30*time.Minute || cfg.LogLevel != slog.LevelInfo || cfg.LogFormat != "text" {
		t.Errorf("duration/log defaults wrong: %+v", cfg)
	}
	if cfg.MaxConcurrentBuilds != 4 || cfg.GitCloneTimeout != 10*time.Minute || cfg.GitCommandTimeout != 2*time.Minute || cfg.ProviderTimeout != 30*time.Second {
		t.Errorf("extra defaults wrong: %+v", cfg)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("providers = %d", len(cfg.Providers))
	}
	gh, ok := cfg.Provider("gh")
	if !ok || gh.Kind != KindGitHub || gh.BaseURL != "https://api.github.com" || gh.DisplayName != "GitHub.com" || gh.Token.Reveal() != "ghp_x" {
		t.Errorf("gh = %+v", gh)
	}
	gl, ok := cfg.Provider("gitlab-work")
	if !ok || gl.Kind != KindGitLab || gl.BaseURL != "https://gitlab.company.com" || gl.DisplayName != "Gitlab Work" || gl.Name != "GITLAB_WORK" {
		t.Errorf("gl = %+v", gl)
	}
	// Providers are sorted by ID for deterministic startup logs.
	if cfg.Providers[0].ID != "gh" || cfg.Providers[1].ID != "gitlab-work" {
		t.Errorf("order = %s,%s", cfg.Providers[0].ID, cfg.Providers[1].ID)
	}
	if got := cfg.Secrets(); len(got) != 2 {
		t.Errorf("Secrets = %v", got)
	}
}

func TestLoadOverrides(t *testing.T) {
	env := append(baseEnv(),
		"APP_PORT=9090", "WORKSPACE_ROOT=/tmp/ws", "REPOSITORY_CACHE_ROOT=/tmp/rc",
		"SESSION_TTL_HOURS=1", "CLEANUP_INTERVAL_MINUTES=5", "LOG_LEVEL=debug", "LOG_FORMAT=json",
		"MAX_CONCURRENT_BUILDS=2", "GIT_CLONE_TIMEOUT_MINUTES=3", "GIT_COMMAND_TIMEOUT_MINUTES=1", "PROVIDER_TIMEOUT_SECONDS=5")
	cfg, err := Load(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9090 || cfg.WorkspaceRoot != "/tmp/ws" || cfg.RepositoryCacheRoot != "/tmp/rc" || cfg.SessionTTL != time.Hour ||
		cfg.CleanupInterval != 5*time.Minute || cfg.LogLevel != slog.LevelDebug || cfg.LogFormat != "json" || cfg.MaxConcurrentBuilds != 2 ||
		cfg.GitCloneTimeout != 3*time.Minute || cfg.GitCommandTimeout != time.Minute || cfg.ProviderTimeout != 5*time.Second {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name     string
		env      []string
		variable string
	}{
		{"no providers", []string{"APP_PORT=1"}, "PROVIDERS__*"},
		{"unknown type", []string{"PROVIDERS__A__TYPE=bitbucket", "PROVIDERS__A__TOKEN=t"}, "PROVIDERS__A__TYPE"},
		{"missing token", []string{"PROVIDERS__A__TYPE=github"}, "PROVIDERS__A__TOKEN"},
		{"gitlab missing base url", []string{"PROVIDERS__A__TYPE=gitlab", "PROVIDERS__A__TOKEN=t"}, "PROVIDERS__A__BASE_URL"},
		{"bad base url", []string{"PROVIDERS__A__TYPE=gitlab", "PROVIDERS__A__TOKEN=t", "PROVIDERS__A__BASE_URL=not a url"}, "PROVIDERS__A__BASE_URL"},
		{"unknown key", []string{"PROVIDERS__A__TYPE=github", "PROVIDERS__A__TOKEN=t", "PROVIDERS__A__COLOR=red"}, "PROVIDERS__A__COLOR"},
		{"bad name", []string{"PROVIDERS__a-b__TYPE=github", "PROVIDERS__a-b__TOKEN=t"}, "PROVIDERS__a-b__TYPE"},
		{"bad port", append(baseEnv(), "APP_PORT=http"), "APP_PORT"},
		{"port range", append(baseEnv(), "APP_PORT=70000"), "APP_PORT"},
		{"bad ttl", append(baseEnv(), "SESSION_TTL_HOURS=0"), "SESSION_TTL_HOURS"},
		{"bad level", append(baseEnv(), "LOG_LEVEL=loud"), "LOG_LEVEL"},
		{"bad format", append(baseEnv(), "LOG_FORMAT=xml"), "LOG_FORMAT"},
		{"bad builds", append(baseEnv(), "MAX_CONCURRENT_BUILDS=0"), "MAX_CONCURRENT_BUILDS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.env)
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if ce.Variable != tc.variable {
				t.Errorf("Variable = %q, want %q (reason %q)", ce.Variable, tc.variable, ce.Reason)
			}
			if tc.name == "missing token" && (ce.Reason == "" || containsAny(ce.Reason, "ghp_", "glpat")) {
				t.Errorf("reason leaks or empty: %q", ce.Reason)
			}
		})
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) && contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// minimalProviderEnv is the minimum standalone environment: one provider,
// which loadProviders requires when the mode is standalone. Named
// separately from the file's existing baseEnv, which loadProviders would
// otherwise redeclare.
func minimalProviderEnv() []string {
	return []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=t",
	}
}

func TestModeDefaultsToStandalone(t *testing.T) {
	for _, env := range [][]string{minimalProviderEnv(), append(minimalProviderEnv(), "CONVERGE_MODE=")} {
		cfg, err := Load(env)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Mode != ModeStandalone {
			t.Fatalf("Mode = %q, want %q", cfg.Mode, ModeStandalone)
		}
		if !cfg.SecretKey.IsZero() {
			t.Fatal("standalone mode read CONVERGE_SECRET_KEY")
		}
		if cfg.DatabasePath != "" {
			t.Fatalf("standalone DatabasePath = %q, want empty", cfg.DatabasePath)
		}
	}
}

func TestModeRejectsUnknownValue(t *testing.T) {
	_, err := Load(append(minimalProviderEnv(), "CONVERGE_MODE=nonsense"))
	var ce *Error
	if !errors.As(err, &ce) || ce.Variable != "CONVERGE_MODE" {
		t.Fatalf("Load error = %v, want a config.Error naming CONVERGE_MODE", err)
	}
}

func TestStandaloneStillRequiresAProvider(t *testing.T) {
	_, err := Load(nil)
	var ce *Error
	if !errors.As(err, &ce) || ce.Variable != "PROVIDERS__*" {
		t.Fatalf("Load error = %v, want a config.Error naming PROVIDERS__*", err)
	}
}

// testKey is 32 bytes of 0x01, base64 standard encoded. Not a credential:
// it exists only so Load has something well-formed to accept.
const testKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

func TestHostedIgnoresProvidersAndRecordsTheirNames(t *testing.T) {
	cfg, err := Load([]string{
		"CONVERGE_MODE=hosted",
		"CONVERGE_SECRET_KEY=" + testKey,
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=t",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("hosted Providers = %v, want none", cfg.Providers)
	}
	want := []string{"PROVIDERS__GH__TOKEN", "PROVIDERS__GH__TYPE"}
	if strings.Join(cfg.IgnoredProviderVars, ",") != strings.Join(want, ",") {
		t.Fatalf("IgnoredProviderVars = %v, want %v", cfg.IgnoredProviderVars, want)
	}
}

func TestHostedWithNoProvidersIsFine(t *testing.T) {
	cfg, err := Load([]string{"CONVERGE_MODE=hosted", "CONVERGE_SECRET_KEY=" + testKey})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabasePath != "/data/converge.db" {
		t.Fatalf("DatabasePath = %q, want /data/converge.db", cfg.DatabasePath)
	}
	if cfg.LoginSessionTTL != 720*time.Hour {
		t.Fatalf("LoginSessionTTL = %v, want 720h", cfg.LoginSessionTTL)
	}
	if cfg.LoginIdleTTL != 168*time.Hour {
		t.Fatalf("LoginIdleTTL = %v, want 168h", cfg.LoginIdleTTL)
	}
	if cfg.SecureCookies || cfg.TrustedProxy {
		t.Fatal("SecureCookies/TrustedProxy should default to false")
	}
}

func TestHostedRequiresAWellFormedSecretKey(t *testing.T) {
	cases := []struct{ name, value string }{
		{"missing", ""},
		{"not base64", "!!!!not-base64!!!!"},
		{"wrong length", "AQEB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := []string{"CONVERGE_MODE=hosted"}
			if tc.value != "" {
				env = append(env, "CONVERGE_SECRET_KEY="+tc.value)
			}
			_, err := Load(env)
			var ce *Error
			if !errors.As(err, &ce) || ce.Variable != "CONVERGE_SECRET_KEY" {
				t.Fatalf("Load error = %v, want a config.Error naming CONVERGE_SECRET_KEY", err)
			}
		})
	}
}

// TestSecretsIncludeMasterKey proves the base64 key joins the redaction list,
// so a key that leaks into an error string is scrubbed from every log line.
func TestSecretsIncludeMasterKey(t *testing.T) {
	cfg, err := Load([]string{"CONVERGE_MODE=hosted", "CONVERGE_SECRET_KEY=" + testKey})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := false
	for _, s := range cfg.Secrets() {
		if s == testKey {
			found = true
		}
	}
	if !found {
		t.Fatalf("Secrets() = %v, want it to contain the master key", cfg.Secrets())
	}
}

func TestBoolVars(t *testing.T) {
	cfg, err := Load([]string{
		"CONVERGE_MODE=hosted", "CONVERGE_SECRET_KEY=" + testKey,
		"CONVERGE_SECURE_COOKIES=true", "CONVERGE_TRUSTED_PROXY=TRUE",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.SecureCookies || !cfg.TrustedProxy {
		t.Fatalf("SecureCookies=%v TrustedProxy=%v, want both true", cfg.SecureCookies, cfg.TrustedProxy)
	}
	if _, err := Load([]string{"CONVERGE_MODE=hosted", "CONVERGE_SECRET_KEY=" + testKey, "CONVERGE_SECURE_COOKIES=yes"}); err == nil {
		t.Fatal("CONVERGE_SECURE_COOKIES=yes should be rejected")
	}
}
