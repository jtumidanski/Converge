// Package config parses Converge's environment-only configuration.
package config

import (
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
	out := make([]string, 0, len(c.Providers))
	for _, p := range c.Providers {
		if !p.Token.IsZero() {
			out = append(out, p.Token.Reveal())
		}
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
	if cfg.Providers, err = loadProviders(env); err != nil {
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

func loadProviders(env []string) ([]ProviderConfig, error) {
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
			return nil, &Error{Variable: k, Reason: "expected PROVIDERS__<NAME>__<KEY>"}
		}
		name, key := rest[:idx], rest[idx+len(providerSep):]
		if !providerNameRe.MatchString(name) {
			return nil, &Error{Variable: k, Reason: "provider name must match [A-Z0-9_]+"}
		}
		if grouped[name] == nil {
			grouped[name] = map[string]string{}
			names = append(names, name)
		}
		grouped[name][key] = strings.TrimSpace(v)
	}
	if len(names) == 0 {
		return nil, &Error{Variable: "PROVIDERS__*", Reason: "at least one provider must be configured"}
	}
	sort.Strings(names)
	out := make([]ProviderConfig, 0, len(names))
	for _, name := range names {
		p, err := buildProvider(name, grouped[name])
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
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
