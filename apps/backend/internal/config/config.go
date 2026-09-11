// Package config parses Converge's environment-only configuration.
package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kind identifies a provider implementation.
type Kind string

const (
	KindGitHub Kind = "github"
	KindGitLab Kind = "gitlab"
)

// Mode selects the shape of the process. The two modes are mutually
// exclusive and fixed for the process lifetime (FR-1.6).
type Mode string

const (
	// ModeStandalone is today's single-tenant shape: no accounts, no
	// database, providers from PROVIDERS__*. It is the default so an existing
	// deployment that upgrades and changes nothing is unaffected (FR-1.1).
	ModeStandalone Mode = "standalone"
	// ModeHosted adds accounts, per-user provider configuration in SQLite,
	// and ownership on review sessions.
	ModeHosted Mode = "hosted"
)

const defaultDatabasePath = "/data/converge.db"

// secretKeyLen is the AES-256 key length CONVERGE_SECRET_KEY must decode to.
const secretKeyLen = 32

const (
	defaultGitHubBaseURL = "https://api.github.com"
	providerPrefix       = "PROVIDERS__"
	providerSep          = "__"
)

var providerNameRe = regexp.MustCompile(`^[A-Z0-9_]+$`)

// ProviderConfig is one named provider instance.
type ProviderConfig struct {
	ID          string // NAME lower-cased, "_" -> "-"
	Name        string // raw <NAME> from the environment
	DisplayName string
	Kind        Kind
	BaseURL     string // no trailing slash
	Token       Secret
}

// Config is the full runtime configuration.
type Config struct {
	Port                int
	WorkspaceRoot       string
	RepositoryCacheRoot string
	SessionTTL          time.Duration
	CleanupInterval     time.Duration
	LogLevel            slog.Level
	LogFormat           string
	MaxConcurrentBuilds int
	GitCloneTimeout     time.Duration
	GitCommandTimeout   time.Duration
	ProviderTimeout     time.Duration
	Providers           []ProviderConfig
	// Mode is parsed before anything else in Load: it decides whether
	// PROVIDERS__* is required and whether CONVERGE_SECRET_KEY is read.
	Mode Mode
	// DatabasePath, SecretKey, SecureCookies, TrustedProxy,
	// LoginSessionTTL and LoginIdleTTL are populated only in hosted mode.
	// In standalone mode they stay zero and no database file is ever
	// created or opened (FR-1.5).
	DatabasePath    string
	SecretKey       Secret
	SecureCookies   bool
	TrustedProxy    bool
	LoginSessionTTL time.Duration
	LoginIdleTTL    time.Duration
	// IgnoredProviderVars holds the names (never the values) of PROVIDERS__*
	// variables present in hosted mode, so the caller can emit the single
	// startup WARN that FR-1.3 requires.
	IgnoredProviderVars []string
}

// Provider looks a provider up by ID.
func (c Config) Provider(id string) (ProviderConfig, bool) {
	for _, p := range c.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderConfig{}, false
}

// Secrets returns every token value, for log redaction.
func (c Config) Secrets() []string {
	out := make([]string, 0, len(c.Providers)+1)
	for _, p := range c.Providers {
		if !p.Token.IsZero() {
			out = append(out, p.Token.Reveal())
		}
	}
	// The master key is registered alongside provider tokens so the
	// redacting handler scrubs it too: a base64 key that ends up inside an
	// error string is exactly the accident this list exists for.
	if !c.SecretKey.IsZero() {
		out = append(out, c.SecretKey.Reveal())
	}
	return out
}

// Load parses an environment slice ("KEY=value") into a Config.
func Load(env []string) (Config, error) {
	vars := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			vars[k] = v
		}
	}
	cfg := Config{LogFormat: "text"}
	var err error
	if cfg.Mode, err = modeVar(vars); err != nil {
		return Config{}, err
	}
	if cfg.Port, err = intVar(vars, "APP_PORT", 8080, 1, 65535); err != nil {
		return Config{}, err
	}
	cfg.WorkspaceRoot = stringVar(vars, "WORKSPACE_ROOT", "/data/workspaces")
	cfg.RepositoryCacheRoot = stringVar(vars, "REPOSITORY_CACHE_ROOT", "/data/repositories")
	if cfg.SessionTTL, err = durationVar(vars, "SESSION_TTL_HOURS", 24, time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.CleanupInterval, err = durationVar(vars, "CLEANUP_INTERVAL_MINUTES", 30, time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel, err = levelVar(vars, "LOG_LEVEL"); err != nil {
		return Config{}, err
	}
	cfg.LogFormat = strings.ToLower(stringVar(vars, "LOG_FORMAT", "text"))
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		return Config{}, &Error{Variable: "LOG_FORMAT", Reason: "must be text or json"}
	}
	if cfg.MaxConcurrentBuilds, err = intVar(vars, "MAX_CONCURRENT_BUILDS", 4, 1, 64); err != nil {
		return Config{}, err
	}
	if cfg.GitCloneTimeout, err = durationVar(vars, "GIT_CLONE_TIMEOUT_MINUTES", 10, time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.GitCommandTimeout, err = durationVar(vars, "GIT_COMMAND_TIMEOUT_MINUTES", 2, time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.ProviderTimeout, err = durationVar(vars, "PROVIDER_TIMEOUT_SECONDS", 30, time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Mode == ModeHosted {
		if err = loadHosted(vars, &cfg); err != nil {
			return Config{}, err
		}
	}
	if cfg.Providers, cfg.IgnoredProviderVars, err = loadProviders(env, cfg.Mode == ModeStandalone); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func stringVar(vars map[string]string, key, def string) string {
	if v, ok := vars[key]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func intVar(vars map[string]string, key string, def, min, max int) (int, error) {
	raw, ok := vars[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return def, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, &Error{Variable: key, Reason: "must be an integer"}
	}
	if n < min || n > max {
		return 0, &Error{Variable: key, Reason: fmt.Sprintf("must be between %d and %d", min, max)}
	}
	return n, nil
}

func durationVar(vars map[string]string, key string, def int, unit time.Duration) (time.Duration, error) {
	n, err := intVar(vars, key, def, 1, 1<<20)
	if err != nil {
		return 0, err
	}
	return time.Duration(n) * unit, nil
}

func levelVar(vars map[string]string, key string) (slog.Level, error) {
	switch strings.ToLower(stringVar(vars, key, "info")) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, &Error{Variable: key, Reason: "must be one of debug, info, warn, error"}
}

// loadProviders parses PROVIDERS__*. required is true only in standalone mode
// (FR-1.2); in hosted mode the variables are ignored entirely and their names
// are returned so the caller can warn once (FR-1.3). Names only — never
// values, which are tokens.
func modeVar(vars map[string]string) (Mode, error) {
	switch Mode(strings.ToLower(stringVar(vars, "CONVERGE_MODE", string(ModeStandalone)))) {
	case ModeStandalone:
		return ModeStandalone, nil
	case ModeHosted:
		return ModeHosted, nil
	default:
		return "", &Error{Variable: "CONVERGE_MODE", Reason: "must be standalone or hosted"}
	}
}

func boolVar(vars map[string]string, key string) (bool, error) {
	raw := strings.ToLower(stringVar(vars, key, "false"))
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, &Error{Variable: key, Reason: "must be true or false"}
	}
}

// loadHosted fills the hosted-only fields. It is never called in standalone
// mode, which is what keeps CONVERGE_SECRET_KEY unread and DatabasePath empty
// there (FR-1.5).
func loadHosted(vars map[string]string, cfg *Config) error {
	cfg.DatabasePath = stringVar(vars, "CONVERGE_DATABASE_PATH", defaultDatabasePath)

	raw := stringVar(vars, "CONVERGE_SECRET_KEY", "")
	if raw == "" {
		return &Error{Variable: "CONVERGE_SECRET_KEY", Reason: "is required in hosted mode"}
	}
	// Validated at parse time, not at first encrypt: an operator seeing a
	// clear startup error beats a user seeing a 500.
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return &Error{Variable: "CONVERGE_SECRET_KEY", Reason: "must be base64 standard encoding"}
	}
	if len(key) != secretKeyLen {
		return &Error{Variable: "CONVERGE_SECRET_KEY", Reason: "must decode to exactly 32 bytes"}
	}
	cfg.SecretKey = NewSecret(raw)

	if cfg.SecureCookies, err = boolVar(vars, "CONVERGE_SECURE_COOKIES"); err != nil {
		return err
	}
	if cfg.TrustedProxy, err = boolVar(vars, "CONVERGE_TRUSTED_PROXY"); err != nil {
		return err
	}
	if cfg.LoginSessionTTL, err = durationVar(vars, "LOGIN_SESSION_TTL_HOURS", 720, time.Hour); err != nil {
		return err
	}
	if cfg.LoginIdleTTL, err = durationVar(vars, "LOGIN_SESSION_IDLE_HOURS", 168, time.Hour); err != nil {
		return err
	}
	return nil
}

func loadProviders(env []string, required bool) ([]ProviderConfig, []string, error) {
	if !required {
		var ignored []string
		for _, kv := range env {
			if k, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, providerPrefix) {
				ignored = append(ignored, k)
			}
		}
		sort.Strings(ignored)
		return nil, ignored, nil
	}
	grouped := map[string]map[string]string{}
	var names []string
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, providerPrefix) {
			continue
		}
		rest := strings.TrimPrefix(k, providerPrefix)
		idx := strings.LastIndex(rest, providerSep)
		if idx <= 0 {
			return nil, nil, &Error{Variable: k, Reason: "expected PROVIDERS__<NAME>__<KEY>"}
		}
		name, key := rest[:idx], rest[idx+len(providerSep):]
		if !providerNameRe.MatchString(name) {
			return nil, nil, &Error{Variable: k, Reason: "provider name must match [A-Z0-9_]+"}
		}
		if grouped[name] == nil {
			grouped[name] = map[string]string{}
			names = append(names, name)
		}
		grouped[name][key] = strings.TrimSpace(v)
	}
	if len(names) == 0 {
		return nil, nil, &Error{Variable: "PROVIDERS__*", Reason: "at least one provider must be configured"}
	}
	sort.Strings(names)
	out := make([]ProviderConfig, 0, len(names))
	for _, name := range names {
		p, err := buildProvider(name, grouped[name])
		if err != nil {
			return nil, nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil, nil
}

func buildProvider(name string, keys map[string]string) (ProviderConfig, error) {
	varName := func(key string) string { return providerPrefix + name + providerSep + key }
	for key := range keys {
		switch key {
		case "TYPE", "BASE_URL", "TOKEN", "DISPLAY_NAME":
		default:
			return ProviderConfig{}, &Error{Variable: varName(key), Reason: "unknown provider key"}
		}
	}
	p := ProviderConfig{ID: providerID(name), Name: name}
	switch Kind(strings.ToLower(keys["TYPE"])) {
	case KindGitHub:
		p.Kind = KindGitHub
	case KindGitLab:
		p.Kind = KindGitLab
	default:
		return ProviderConfig{}, &Error{Variable: varName("TYPE"), Reason: "must be github or gitlab"}
	}
	if keys["TOKEN"] == "" {
		return ProviderConfig{}, &Error{Variable: varName("TOKEN"), Reason: "is required"}
	}
	p.Token = NewSecret(keys["TOKEN"])
	base := keys["BASE_URL"]
	if base == "" {
		if p.Kind == KindGitLab {
			return ProviderConfig{}, &Error{Variable: varName("BASE_URL"), Reason: "is required for gitlab providers"}
		}
		base = defaultGitHubBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ProviderConfig{}, &Error{Variable: varName("BASE_URL"), Reason: "must be an absolute http(s) URL"}
	}
	p.BaseURL = strings.TrimRight(base, "/")
	p.DisplayName = keys["DISPLAY_NAME"]
	if p.DisplayName == "" {
		p.DisplayName = displayName(name)
	}
	return p, nil
}

func providerID(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "_", "-")
}

func displayName(name string) string {
	words := strings.Split(strings.ToLower(name), "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
