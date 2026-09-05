package app

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jtumidanski/converge/internal/config"
)

func TestCheckGitVersion(t *testing.T) {
	for _, ok := range []string{"2.45.0", "2.49.1", "2.55.0", "3.0.0", "2.45.0.windows.1"} {
		if err := checkGitVersion(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"2.44.0", "2.30.2", "1.9.5", "banana"} {
		if err := checkGitVersion(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// TestCheckGitVersionMalformedNumericParts covers malformed versions that
// have the right shape (two dot-separated parts) but non-numeric content,
// which TestCheckGitVersion's "banana" case (a single token, rejected before
// any numeric parsing) does not exercise.
func TestCheckGitVersionMalformedNumericParts(t *testing.T) {
	for _, bad := range []string{"a.b", "2.b", "a.5"} {
		if err := checkGitVersion(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRedactingLoggerScrubsSecrets(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.Config{LogFormat: "text", LogLevel: slog.LevelInfo, Providers: []config.ProviderConfig{{ID: "gh", Token: config.NewSecret("ghp_supersecret")}}}
	log := NewLogger(&buf, cfg)
	log.Info("hello", "url", "https://x/y?token=ghp_supersecret", "nested", slog.GroupValue(slog.String("h", "Bearer ghp_supersecret")))
	out := buf.String()
	if strings.Contains(out, "ghp_supersecret") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("no redaction marker: %s", out)
	}
}

func TestNewWiresEverything(t *testing.T) {
	root := t.TempDir()
	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=error",
	}
	a, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Service == nil || a.Store == nil || a.Registry == nil {
		t.Fatal("wiring incomplete")
	}
	if _, ok := a.Registry.Get("gh"); !ok {
		t.Error("provider not registered")
	}
	if len(a.Store.List()) != 0 {
		t.Error("unexpected sessions")
	}
	if _, err := New(context.Background(), []string{"APP_PORT=1"}); err == nil {
		t.Error("missing providers must fail startup")
	}
}

// TestNewWiresRepositoryCacheRoot proves REPOSITORY_CACHE_ROOT actually
// reaches mirror.New rather than the cache falling back to its own default.
// mirror.Cache.root is unexported, so the only observable proof is that
// Cache.Path (which is built from that root) is rooted under the configured
// directory rather than some other location.
func TestNewWiresRepositoryCacheRoot(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "custom-cache-dir")
	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + cacheRoot,
		"LOG_LEVEL=error",
	}
	a, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	got, err := a.Mirrors.Path("gh", "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, cacheRoot) {
		t.Fatalf("mirror path %q is not rooted under configured REPOSITORY_CACHE_ROOT %q", got, cacheRoot)
	}
}
