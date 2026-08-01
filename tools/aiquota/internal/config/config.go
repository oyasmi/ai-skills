// Package config reads QuotaList's configuration file. It is deliberately
// read-only: the macOS app owns this file, and two writers would fight over it.
// Unknown keys (display mode, refresh interval, history switch...) are ignored.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultPath is QuotaList's config location. It is intentionally not
// XDG-aware: the app hardcodes ~/.config, and honoring XDG_CONFIG_HOME here
// would silently point the two tools at different files.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "quota-list", "config.json")
}

// Resolve picks the config path: explicit flag, then AIQUOTA_CONFIG, then the
// QuotaList default.
func Resolve(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if env := os.Getenv("AIQUOTA_CONFIG"); env != "" {
		return env
	}
	return DefaultPath()
}

type ProxyType string

const (
	ProxyNone   ProxyType = "none"
	ProxyHTTP   ProxyType = "http"
	ProxySOCKS5 ProxyType = "socks5"
)

// Proxy is the single app-wide proxy; each provider opts in via its own flag.
type Proxy struct {
	Type ProxyType `json:"type"`
	Host string    `json:"host"`
	Port int       `json:"port"`
}

// Usable reports whether the proxy is fully configured.
func (p Proxy) Usable() bool {
	return p.Type != "" && p.Type != ProxyNone && p.Host != "" && p.Port > 0
}

func (p Proxy) String() string {
	if !p.Usable() {
		return "none"
	}
	return fmt.Sprintf("%s://%s:%d", p.Type, p.Host, p.Port)
}

// WindowMapping maps JSON keypaths of a custom endpoint onto one quota window.
type WindowMapping struct {
	Label         string `json:"label"`
	UsedPath      string `json:"usedPath"`      // keypath to a 0–100 percentage
	RemainingPath string `json:"remainingPath"` // balance style: remaining / total
	TotalPath     string `json:"totalPath"`
	ResetPath     string `json:"resetPath"`  // ISO string or unix seconds/millis
	DetailUnit    string `json:"detailUnit"` // e.g. "$"
}

// CustomProvider is a user-configured third-party plan exposed over HTTP.
type CustomProvider struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"displayName"`
	ShortCode   string            `json:"shortCode"`
	ColorHex    string            `json:"colorHex"`
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	Windows     []WindowMapping   `json:"windows"`
	AutoDetect  *bool             `json:"autoDetect"`
	UseProxy    bool              `json:"useProxy"`
}

// DetectEnabled reports whether heuristic field detection should run. It
// defaults to true, matching QuotaList.
func (c CustomProvider) DetectEnabled() bool {
	return c.AutoDetect == nil || *c.AutoDetect
}

// Config is the subset of QuotaList's config this tool cares about.
type Config struct {
	ClaudeEnabled bool
	ClaudeSource  string // auto | web | oauth
	CodexEnabled  bool
	ZaiEnabled    bool
	ZaiToken      string

	Proxy          Proxy
	ClaudeUseProxy bool
	CodexUseProxy  bool
	ZaiUseProxy    bool

	CustomProviders []CustomProvider

	// Path is where the config was read from; Found is false when the file did
	// not exist and defaults were used instead.
	Path  string
	Found bool
}

// Default mirrors QuotaList's fresh-install state: Claude and Codex on, z.ai
// off until a token is configured.
func Default() Config {
	return Config{
		ClaudeEnabled: true,
		ClaudeSource:  "auto",
		CodexEnabled:  true,
		Proxy:         Proxy{Type: ProxyNone},
	}
}

// raw decodes with every key optional so partial or older config files (and
// files written by a newer QuotaList) still load.
type raw struct {
	ClaudeEnabled   *bool            `json:"claudeEnabled"`
	ClaudeSource    *string          `json:"claudeSource"`
	CodexEnabled    *bool            `json:"codexEnabled"`
	ZaiEnabled      *bool            `json:"zaiEnabled"`
	ZaiToken        *string          `json:"zaiToken"`
	Proxy           *Proxy           `json:"proxy"`
	ClaudeUseProxy  *bool            `json:"claudeUseProxy"`
	CodexUseProxy   *bool            `json:"codexUseProxy"`
	ZaiUseProxy     *bool            `json:"zaiUseProxy"`
	CustomProviders []CustomProvider `json:"customProviders"`
}

// Load reads the config at `path`. A missing file is not an error: it yields
// the defaults, so the tool works on a machine that has never run the app.
func Load(path string) (Config, error) {
	cfg := Default()
	cfg.Path = path
	if path == "" {
		return cfg, errors.New("无法确定配置文件路径（HOME 未设置？）")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("读取配置失败 %s: %w", path, err)
	}

	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return cfg, fmt.Errorf("解析配置失败 %s: %w", path, err)
	}
	cfg.Found = true

	if r.ClaudeEnabled != nil {
		cfg.ClaudeEnabled = *r.ClaudeEnabled
	}
	if r.ClaudeSource != nil && *r.ClaudeSource != "" {
		cfg.ClaudeSource = *r.ClaudeSource
	}
	if r.CodexEnabled != nil {
		cfg.CodexEnabled = *r.CodexEnabled
	}
	if r.ZaiEnabled != nil {
		cfg.ZaiEnabled = *r.ZaiEnabled
	}
	if r.ZaiToken != nil {
		cfg.ZaiToken = *r.ZaiToken
	}
	if r.Proxy != nil {
		cfg.Proxy = *r.Proxy
	}
	if r.ClaudeUseProxy != nil {
		cfg.ClaudeUseProxy = *r.ClaudeUseProxy
	}
	if r.CodexUseProxy != nil {
		cfg.CodexUseProxy = *r.CodexUseProxy
	}
	if r.ZaiUseProxy != nil {
		cfg.ZaiUseProxy = *r.ZaiUseProxy
	}
	cfg.CustomProviders = r.CustomProviders
	return cfg, nil
}
