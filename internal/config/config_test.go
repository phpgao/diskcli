package config

import (
	"os"
	"testing"
)

func writeTOML(t *testing.T, content string) string {
	t.Helper()
	p := t.TempDir() + "/config"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_DefaultAccount(t *testing.T) {
	p := writeTOML(t, `qk_cookie = "default-cookie"
upload_threads = 4
`)
	cfg, err := Load(LoadOptions{ConfigPath: p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuarkCookie != "default-cookie" {
		t.Errorf("cookie = %q, want default-cookie", cfg.QuarkCookie)
	}
	if cfg.UploadThreads != 4 {
		t.Errorf("threads = %d, want 4", cfg.UploadThreads)
	}
	if cfg.AccountName != "default" {
		t.Errorf("account = %q, want default", cfg.AccountName)
	}
}

func TestLoad_NamedAccount(t *testing.T) {
	p := writeTOML(t, `qk_cookie = "default-cookie"

[account.work]
qk_cookie = "work-cookie"
`)
	t.Setenv("DISK_ACCOUNT", "work")
	cfg, err := Load(LoadOptions{ConfigPath: p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuarkCookie != "work-cookie" {
		t.Errorf("cookie = %q, want work-cookie", cfg.QuarkCookie)
	}
	if cfg.AccountName != "work" {
		t.Errorf("account = %q, want work", cfg.AccountName)
	}
}

func TestLoad_FlagWins(t *testing.T) {
	p := writeTOML(t, `qk_cookie = "config-cookie"`)
	t.Setenv("QK_COOKIE", "env-cookie")
	cfg, err := Load(LoadOptions{ConfigPath: p, QuarkCookieFlag: "flag-cookie"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuarkCookie != "flag-cookie" {
		t.Errorf("cookie = %q, want flag-cookie", cfg.QuarkCookie)
	}
	if cfg.CookieSource != "flag" {
		t.Errorf("source = %q, want flag", cfg.CookieSource)
	}
}

func TestLoad_EnvCookie(t *testing.T) {
	p := writeTOML(t, `qk_cookie = "config-cookie"`)
	t.Setenv("QK_COOKIE", "env-cookie")
	cfg, err := Load(LoadOptions{ConfigPath: p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuarkCookie != "env-cookie" {
		t.Errorf("cookie = %q, want env-cookie", cfg.QuarkCookie)
	}
	if cfg.CookieSource != "env" {
		t.Errorf("source = %q, want env", cfg.CookieSource)
	}
}

func TestLoad_MissingAccount(t *testing.T) {
	p := writeTOML(t, `qk_cookie = "x"`)
	t.Setenv("DISK_ACCOUNT", "nonexistent")
	_, err := Load(LoadOptions{ConfigPath: p})
	if err == nil {
		t.Fatal("expected error for missing account")
	}
}

func TestLoad_NoConfig(t *testing.T) {
	cfg, err := Load(LoadOptions{ConfigPath: "/nonexistent/path/config"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuarkCookie != "" {
		t.Errorf("expected empty cookie, got %q", cfg.QuarkCookie)
	}
	if cfg.CookieSource != "none" {
		t.Errorf("expected none source, got %q", cfg.CookieSource)
	}
}

func TestLoad_UploadThreads(t *testing.T) {
	p := writeTOML(t, `qk_cookie = "x"
upload_threads = 8
`)
	cfg, err := Load(LoadOptions{ConfigPath: p})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UploadThreads != 8 {
		t.Errorf("threads = %d, want 8", cfg.UploadThreads)
	}
}
