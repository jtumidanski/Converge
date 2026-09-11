package app

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/config"
	"github.com/jtumidanski/converge/internal/identity"
	"github.com/jtumidanski/converge/internal/mirror"
	"github.com/jtumidanski/converge/internal/session"
)

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it. app.New always logs to os.Stderr (see NewLogger's
// call site in New), so this is the only way to observe its startup log lines
// from outside the package without adding a test-only seam to production
// code.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	os.Stderr = orig
	_ = w.Close()
	out := <-done
	return out
}

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
	if len(a.Store.List(identity.Standalone())) != 0 {
		t.Error("unexpected sessions")
	}
	if _, err := New(context.Background(), []string{"APP_PORT=1"}); err == nil {
		t.Error("missing providers must fail startup")
	}
}

// TestRedactingHandlerScrubsDefaultKindAttrs exercises scrubAttr's default
// branch, which every slog.Any value that isn't a string or a group lands
// in (errors, structs, pointers, maps, slices...). A plain KindString attr
// never touches this branch, so it needs its own coverage.
func TestRedactingHandlerScrubsDefaultKindAttrs(t *testing.T) {
	var buf bytes.Buffer
	secret := "ghp_defaultkindsecret"
	cfg := config.Config{LogFormat: "text", LogLevel: slog.LevelInfo, Providers: []config.ProviderConfig{{ID: "gh", Token: config.NewSecret(secret)}}}
	log := NewLogger(&buf, cfg)
	log.Info("hello", "err", fmt.Errorf("upstream failed: %s", secret))
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("secret leaked through default-kind attr: %s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("no redaction marker: %s", out)
	}
}

// TestRedactingHandlerScrubsMessage exercises message scrubbing in
// isolation: the secret appears only in r.Message, no attribute carries it.
func TestRedactingHandlerScrubsMessage(t *testing.T) {
	var buf bytes.Buffer
	secret := "ghp_messagesecret"
	cfg := config.Config{LogFormat: "text", LogLevel: slog.LevelInfo, Providers: []config.ProviderConfig{{ID: "gh", Token: config.NewSecret(secret)}}}
	log := NewLogger(&buf, cfg)
	log.Info("request failed for " + secret)
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("secret leaked through message: %s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("no redaction marker: %s", out)
	}
}

// TestRedactingHandlerWithAttrsScrubs exercises the WithAttrs path (the
// slog.Logger.With(...) chain), in isolation from Handle's own per-record
// attribute scrubbing: the secret is attached via With, not via Info's
// variadic args.
func TestRedactingHandlerWithAttrsScrubs(t *testing.T) {
	var buf bytes.Buffer
	secret := "ghp_withattrssecret"
	cfg := config.Config{LogFormat: "text", LogLevel: slog.LevelInfo, Providers: []config.ProviderConfig{{ID: "gh", Token: config.NewSecret(secret)}}}
	log := NewLogger(&buf, cfg).With("token", secret)
	log.Info("hello")
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("secret leaked through WithAttrs: %s", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("no redaction marker: %s", out)
	}
}

// TestRedactingHandlerWithGroupScrubsAndPreservesGrouping exercises
// WithGroup. It asserts two things at once: the secret attached inside the
// group is scrubbed, AND the group prefix itself survived (proving
// WithGroup actually delegates to h.inner.WithGroup rather than, say,
// dropping the call and returning itself unchanged -- a mutation that would
// not be caught by checking for the secret's absence alone, since scrubbing
// happens in WithAttrs/Handle regardless of whether grouping is applied).
func TestRedactingHandlerWithGroupScrubsAndPreservesGrouping(t *testing.T) {
	var buf bytes.Buffer
	secret := "ghp_withgroupsecret"
	cfg := config.Config{LogFormat: "text", LogLevel: slog.LevelInfo, Providers: []config.ProviderConfig{{ID: "gh", Token: config.NewSecret(secret)}}}
	log := NewLogger(&buf, cfg).WithGroup("upstream").With("token", secret)
	log.Info("hello")
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("secret leaked through WithGroup chain: %s", out)
	}
	if !strings.Contains(out, "upstream.token=[redacted]") {
		t.Fatalf("group prefix lost or attribute not scrubbed: %s", out)
	}
}

// TestNewLoggerJSONFormat proves LOG_FORMAT=json actually selects the JSON
// handler rather than silently falling back to text.
func TestNewLoggerJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.Config{LogFormat: "json", LogLevel: slog.LevelInfo}
	log := NewLogger(&buf, cfg)
	log.Info("hello")
	out := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(out, "{") || !strings.Contains(out, `"msg":"hello"`) {
		t.Fatalf("LOG_FORMAT=json did not produce JSON output: %s", out)
	}
}

// TestProviderStartupLogRedactsPassword proves a password embedded in a
// provider's BASE_URL userinfo (config.Load accepts, e.g.,
// "https://user:pw@host" and does not include it in cfg.Secrets()) never
// reaches the startup log line in clear.
func TestProviderStartupLogRedactsPassword(t *testing.T) {
	root := t.TempDir()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		os.Stderr = origStderr
		_ = w.Close()
	}
	defer restore()

	captured := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		captured <- string(data)
	}()

	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"PROVIDERS__GH__BASE_URL=https://svcuser:hunter2pw@example.invalid",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=info",
	}
	a, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	_ = a.Close()

	restore()
	out := <-captured
	if strings.Contains(out, "hunter2pw") {
		t.Fatalf("provider startup log leaked the URL password: %s", out)
	}
	if !strings.Contains(out, "example.invalid") {
		t.Fatalf("provider startup log dropped the base url entirely: %s", out)
	}
}

// TestNewWorkspaceRootUncreatable proves that when WORKSPACE_ROOT cannot be
// created, New attributes the failure to WORKSPACE_ROOT via a config.Error,
// matching how REPOSITORY_CACHE_ROOT is handled, rather than surfacing an
// unattributed workspace-package error.
func TestNewWorkspaceRootUncreatable(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(blocker, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=error",
	}
	_, err := New(context.Background(), env)
	var ce *config.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *config.Error", err)
	}
	if ce.Variable != "WORKSPACE_ROOT" {
		t.Fatalf("err variable = %q, want WORKSPACE_ROOT", ce.Variable)
	}
}

// TestNewLoadAllRestoresExistingSessions proves New actually calls
// Store.LoadAll: a session saved to WORKSPACE_ROOT before New runs must be
// visible through the returned App's Store afterward.
func TestNewLoadAllRestoresExistingSessions(t *testing.T) {
	root := t.TempDir()
	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=error",
	}
	first, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := session.NewBuilder().
		SetID("0123abcd").
		SetProviderID("gh").
		SetRepository("acme/widgets").
		SetBaseBranch("main").
		SetRequestedChanges([]int{1}).
		SetCreatedAt(time.Now()).
		SetTTL(time.Hour).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Store.Save(sess); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	if _, ok := second.Store.Get(sess.ID(), identity.Standalone()); !ok {
		t.Fatal("pre-existing session on disk was not visible after New; Store.LoadAll did not run")
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
	got, err := a.Mirrors.Path(mirror.RootNamespace(), "gh", "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, cacheRoot) {
		t.Fatalf("mirror path %q is not rooted under configured REPOSITORY_CACHE_ROOT %q", got, cacheRoot)
	}
}

// secretKey returns a valid CONVERGE_SECRET_KEY value: 32 random bytes,
// base64 standard encoding.
func secretKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := crand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// TestStandaloneCreatesNoDatabaseFile proves standalone mode never opens or
// creates a database, even when CONVERGE_DATABASE_PATH is set explicitly
// (FR-1.5, acceptance criterion). Asserting a pass of the *pre-existing*
// TestNewWiresEverything would not distinguish "no DB opened" from "DB opened
// somewhere unrelated" -- this test checks the filesystem directly.
func TestStandaloneCreatesNoDatabaseFile(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "would-be.db")
	env := []string{
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_x",
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=error",
		"CONVERGE_DATABASE_PATH=" + dbPath,
	}
	a, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.DB != nil {
		t.Error("App.DB is non-nil in standalone mode")
	}
	if a.Auth != nil {
		t.Error("App.Auth is non-nil in standalone mode")
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("CONVERGE_DATABASE_PATH file exists in standalone mode: stat err = %v", err)
	}
	if _, err := os.Stat("/data/converge.db"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("default database path was touched: stat err = %v", err)
	}
}

// TestHostedOpensAndMigratesTheDatabase proves hosted mode opens the
// configured database file, runs the migration, and that Close closes the
// handle.
func TestHostedOpensAndMigratesTheDatabase(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "hosted.db")
	env := []string{
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=error",
		"CONVERGE_MODE=hosted",
		"CONVERGE_SECRET_KEY=" + secretKey(t),
		"CONVERGE_DATABASE_PATH=" + dbPath,
	}
	a, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database file was not created: %v", err)
	}
	if a.Auth == nil {
		t.Fatal("App.Auth is nil in hosted mode")
	}
	var count int
	if err := a.DB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("schema_migrations has %d rows, want 1", count)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}
	if err := a.DB.Ping(); err == nil {
		t.Error("database handle is still usable after Close")
	}
}

// TestHostedWarnsAboutIgnoredProviderVariables proves the single WARN names
// every ignored PROVIDERS__* variable and never the token value (FR-1.3).
func TestHostedWarnsAboutIgnoredProviderVariables(t *testing.T) {
	root := t.TempDir()
	env := []string{
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=warn",
		"CONVERGE_MODE=hosted",
		"CONVERGE_SECRET_KEY=" + secretKey(t),
		"CONVERGE_DATABASE_PATH=" + filepath.Join(root, "hosted.db"),
		"PROVIDERS__GH__TYPE=github",
		"PROVIDERS__GH__TOKEN=ghp_supersecrettoken",
	}
	out := captureStderr(t, func() {
		a, err := New(context.Background(), env)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
	})
	if !strings.Contains(out, "PROVIDERS__GH__TYPE") || !strings.Contains(out, "PROVIDERS__GH__TOKEN") {
		t.Fatalf("warning does not name both ignored variables: %s", out)
	}
	if strings.Contains(out, "ghp_supersecrettoken") {
		t.Fatalf("token value leaked into warning: %s", out)
	}
	warnCount := strings.Count(out, "level=WARN")
	if warnCount != 1 {
		t.Errorf("expected exactly one WARN line, found %d: %s", warnCount, out)
	}
}

// TestHostedLogsTheStartupSummary asserts the "hosted mode ready" line
// carries the mode, database path, user count, and unowned-session count
// (NFR observability, FR-6.3).
func TestHostedLogsTheStartupSummary(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "hosted.db")
	env := []string{
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=info",
		"CONVERGE_MODE=hosted",
		"CONVERGE_SECRET_KEY=" + secretKey(t),
		"CONVERGE_DATABASE_PATH=" + dbPath,
	}
	out := captureStderr(t, func() {
		a, err := New(context.Background(), env)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
	})
	if !strings.Contains(out, "mode=hosted") {
		t.Errorf("startup log missing mode=hosted: %s", out)
	}
	if !strings.Contains(out, dbPath) {
		t.Errorf("startup log missing database path: %s", out)
	}
	if !strings.Contains(out, "users=") {
		t.Errorf("startup log missing user count: %s", out)
	}
	if !strings.Contains(out, "unowned_sessions=") {
		t.Errorf("startup log missing unowned-session count: %s", out)
	}
}

// TestHostedUsesTheDatabaseBackedResolver proves the hosted resolver seam is
// genuinely installed: App.Resolver type-asserts to *auth.ProviderResolver,
// and resolving for the standalone scope (no per-user configuration exists)
// errors, which the static resolver never would.
func TestHostedUsesTheDatabaseBackedResolver(t *testing.T) {
	root := t.TempDir()
	env := []string{
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=error",
		"CONVERGE_MODE=hosted",
		"CONVERGE_SECRET_KEY=" + secretKey(t),
		"CONVERGE_DATABASE_PATH=" + filepath.Join(root, "hosted.db"),
	}
	a, err := New(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	pr, ok := a.Resolver.(*auth.ProviderResolver)
	if !ok {
		t.Fatalf("App.Resolver is %T, want *auth.ProviderResolver", a.Resolver)
	}
	if pr != a.ProviderResolver {
		t.Error("App.Resolver and App.ProviderResolver are not the same instance")
	}
	if _, err := a.Resolver.Resolve(context.Background(), identity.Standalone()); err == nil {
		t.Error("expected the database-backed resolver to error for a scope with no configured providers")
	}
}

// TestMissingSecretKeyIsAStartupError proves a missing CONVERGE_SECRET_KEY
// fails startup before any database file is created (acceptance criterion).
func TestMissingSecretKeyIsAStartupError(t *testing.T) {
	root := t.TempDir()
	env := []string{
		"WORKSPACE_ROOT=" + filepath.Join(root, "ws"),
		"REPOSITORY_CACHE_ROOT=" + filepath.Join(root, "cache"),
		"LOG_LEVEL=error",
		"CONVERGE_MODE=hosted",
	}
	_, err := New(context.Background(), env)
	if err == nil {
		t.Fatal("expected an error")
	}
	var cfgErr *config.Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *config.Error, got %T: %v", err, err)
	}
	if cfgErr.Variable != "CONVERGE_SECRET_KEY" {
		t.Errorf("error names %q, want CONVERGE_SECRET_KEY", cfgErr.Variable)
	}
	if _, statErr := os.Stat("/data/converge.db"); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("default database path was touched despite the missing key: stat err = %v", statErr)
	}
}
