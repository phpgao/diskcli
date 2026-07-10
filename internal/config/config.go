// Package config loads diskcli runtime configuration from a TOML file.
//
// Config file location: ~/.diskcli/config
//
// Format:
//
//	# Default account (top-level keys)
//	qk_cookie = "xxx"
//	upload_threads = 4
//
//	[account.work]
//	qk_cookie = "yyy"
//
//	[account.personal]
//	qk_cookie = "zzz"
//
// Account selection priority:
//  1. --qk-cookie CLI flag (always wins, bypass account selection)
//  2. DISK_ACCOUNT env var → select [account.<name>] section
//  3. QK_COOKIE env var → overrides default section's cookie
//  4. Top-level keys = [default] account
//
// The same env var overrides work inside named accounts: e.g. DISK_ACCOUNT=work
// makes QK_COOKIE override [account.work].qk_cookie.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/naoina/toml"
)

// Default config file location: $HOME/.diskcli/config
const defaultConfigName = ".diskcli/config"

const defaultConfigContent = `# diskcli configuration
# 
# Fill in your Quark cookie below:
qk_cookie = ""
# Number of concurrent upload threads:
upload_threads = 4
`

// DiskCLIDir returns the ~/.diskcli directory, creating it if needed.
func DiskCLIDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".diskcli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// --- Environment variables ---

// Account env: DISK_ACCOUNT=work selects [account.work].
const EnvAccount = "DISK_ACCOUNT"

// Quark cookie: QK_COOKIE overrides qk_cookie in the active account.
const EnvQuarkCookie = "QK_COOKIE"

// Quark browser header overrides.
const (
	EnvQuarkUserAgent = "QK_USER_AGENT"
	EnvQuarkReferer   = "QK_REFERER"
	EnvQuarkOrigin    = "QK_ORIGIN"
)

// Upload concurrency: DISK_UPLOAD_THREADS overrides upload_threads.
const EnvUploadThreads = "DISK_UPLOAD_THREADS"

// --- Config struct ---

// Config holds configuration for one account.
type Config struct {
	QuarkCookie    string // full cookie string (may come from --qk-cookie)
	QuarkUserAgent string
	QuarkReferer   string
	QuarkOrigin    string
	UploadThreads  int
	AccountName    string // "default" or the name from [account.xxx]
	ConfigPath     string
	CookieSource   string // "flag" | "env" | "config" | "account" | "none"
}

// --- AccountMap is the parsed TOML config file ---

// AccountMap holds the config for the default account plus any named accounts.
type AccountMap struct {
	// Default account fields (top-level keys in TOML).
	Default *Config
	// Named accounts keyed by their section name.
	Accounts map[string]*Config
	// Raw path to the config file.
	Path string
}

// LoadOptions controls which account is loaded.
type LoadOptions struct {
	// QuarkCookieFlag comes from --qk-cookie; empty means not provided.
	// When set, it always wins regardless of account selection.
	QuarkCookieFlag string
	// ConfigPath overrides the default config file location.
	ConfigPath string
}

// Load reads the config file, applies env vars, and returns the Config for
// the active account (selected by DISK_ACCOUNT or "default").
func Load(opts LoadOptions) (*Config, error) {
	path := opts.ConfigPath
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	if path == "" {
		path = filepath.Join(home, defaultConfigName)
	}

	// Parse TOML.
	am, err := parseTOML(path)
	if err != nil {
		return nil, err
	}
	am.Path = path

	// Select the active account.
	accountName := strings.TrimSpace(os.Getenv(EnvAccount))
	if accountName == "" {
		accountName = "default"
	}

	var cfg *Config
	if accountName == "default" {
		cfg = am.Default
	} else {
		if acc, ok := am.Accounts[accountName]; ok {
			cfg = acc
			cfg.AccountName = accountName
			cfg.ConfigPath = path
			cfg.CookieSource = "account"
		} else {
			return nil, fmt.Errorf("account %q not found in %s (available: default%s)",
				accountName, path, listAccountNames(am.Accounts))
		}
	}

	if cfg == nil {
		cfg = &Config{AccountName: "default", ConfigPath: path}
	}

	// 1. --qk-cookie flag always wins.
	if opts.QuarkCookieFlag != "" {
		cfg.QuarkCookie = normalizeCookie(opts.QuarkCookieFlag)
		cfg.CookieSource = "flag"
		return cfg, nil
	}

	// 2. Env overrides for cookie and headers.
	if v := os.Getenv(EnvQuarkCookie); v != "" {
		cfg.QuarkCookie = normalizeCookie(v)
		cfg.CookieSource = "env"
	}

	// 3. Env overrides for UA/Referer/Origin.
	resolveEnvString(&cfg.QuarkUserAgent, EnvQuarkUserAgent)
	resolveEnvString(&cfg.QuarkReferer, EnvQuarkReferer)
	resolveEnvString(&cfg.QuarkOrigin, EnvQuarkOrigin)

	// 4. Env override for upload_threads.
	if v := os.Getenv(EnvUploadThreads); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.UploadThreads = n
		}
	}

	if cfg.CookieSource == "" && cfg.QuarkCookie != "" {
		cfg.CookieSource = "config"
	}
	if cfg.QuarkCookie == "" {
		cfg.CookieSource = "none"
	}

	return cfg, nil
}

// --- TOML parsing ---

// tomlAccount mirrors the account.* sub-table in the config file.
type tomlAccount struct {
	QkCookie      string `toml:"qk_cookie"`
	QkUserAgent   string `toml:"qk_user_agent"`
	QkReferer     string `toml:"qk_referer"`
	QkOrigin      string `toml:"qk_origin"`
	UploadThreads int    `toml:"upload_threads"`
}

// tomlDoc is the decoded config file shape. Top-level keys belong to the
// default account; [account.<name>] sub-tables become named accounts.
type tomlDoc struct {
	QkCookie      string                 `toml:"qk_cookie"`
	QkUserAgent   string                 `toml:"qk_user_agent"`
	QkReferer     string                 `toml:"qk_referer"`
	QkOrigin      string                 `toml:"qk_origin"`
	UploadThreads int                    `toml:"upload_threads"`
	Account       map[string]tomlAccount `toml:"account"`
}

// parseTOML reads a TOML config file and returns an AccountMap.
//
// Decoding is delegated to github.com/naoina/toml — no hand-rolled
// parser. Unmapped keys are ignored.
func parseTOML(path string) (*AccountMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Try to create a default config file.
			// Silently ignore any errors — best effort only.
			if dir := filepath.Dir(path); dir != "" {
				_ = os.MkdirAll(dir, 0o700)
			}
			_ = os.WriteFile(path, []byte(defaultConfigContent), 0o600)
			return &AccountMap{
				Default:  &Config{AccountName: "default", ConfigPath: path},
				Accounts: make(map[string]*Config),
			}, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var doc tomlDoc
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	am := &AccountMap{
		Default: &Config{
			AccountName:    "default",
			ConfigPath:     path,
			QuarkCookie:    normalizeCookie(doc.QkCookie),
			QuarkUserAgent: doc.QkUserAgent,
			QuarkReferer:   doc.QkReferer,
			QuarkOrigin:    doc.QkOrigin,
			UploadThreads:  doc.UploadThreads,
		},
		Accounts: make(map[string]*Config),
	}
	for name, a := range doc.Account {
		am.Accounts[name] = &Config{
			AccountName:    name,
			ConfigPath:     path,
			QuarkCookie:    normalizeCookie(a.QkCookie),
			QuarkUserAgent: a.QkUserAgent,
			QuarkReferer:   a.QkReferer,
			QuarkOrigin:    a.QkOrigin,
			UploadThreads:  a.UploadThreads,
		}
	}
	return am, nil
}

// --- helpers ---

func resolveEnvString(field *string, envKey string) {
	if v := os.Getenv(envKey); v != "" {
		*field = v
	}
}

func normalizeCookie(raw string) string {
	if raw == "" {
		return raw
	}
	// Trim leading/trailing whitespace and merge consecutive ';'
	parts := strings.Split(raw, ";")
	var cleaned []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, "; ")
}

func listAccountNames(m map[string]*Config) string {
	var names []string
	for k := range m {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}
