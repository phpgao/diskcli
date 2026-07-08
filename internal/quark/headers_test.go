package quark

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCustomHeaders(t *testing.T) {
	f := &FakeDoer{}
	f.Register("GET", "/account/info", 200, `{"success":true,"code":0,"data":{"nickname":"test"}}`)

	opts := WithCustomHeaders(map[string]string{
		"User-Agent": "MyCustomUA/1.0",
		"Referer":    "https://custom.example.com",
	})
	c, err := NewClient("__pus=abc", WithDoer(f), opts)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	_, err = c.AccountInfo(context.Background())
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(f.Calls))
	}
	ua := f.Calls[0].Header.Get("User-Agent")
	if ua != "MyCustomUA/1.0" {
		t.Errorf("User-Agent = %q, want MyCustomUA/1.0", ua)
	}
	ref := f.Calls[0].Header.Get("Referer")
	if ref != "https://custom.example.com" {
		t.Errorf("Referer = %q, want https://custom.example.com", ref)
	}
	// Default headers we didn't override should still be present.
	if f.Calls[0].Header.Get("Accept") == "" {
		t.Error("Accept should still be set (default)")
	}
}

func TestCustomHeaders_NoOverrides(t *testing.T) {
	f := &FakeDoer{}
	f.Register("GET", "/account/info", 200, `{"success":true,"code":0,"data":{"nickname":"test"}}`)

	c, _ := NewClient("__pus=abc", WithDoer(f))
	_, _ = c.AccountInfo(context.Background())

	ua := f.Calls[0].Header.Get("User-Agent")
	if !strings.Contains(ua, "Chrome") {
		t.Errorf("without override, User-Agent should be default Chrome, got: %s", ua)
	}
	ref := f.Calls[0].Header.Get("Referer")
	if ref != defaultHeaderValues.referer {
		t.Errorf("Referer should be default, got: %s", ref)
	}
}

func TestCustomHeaders_EmptyOverridesAreIgnored(t *testing.T) {
	f := &FakeDoer{}
	f.Register("GET", "/account/info", 200, `{"success":true,"code":0,"data":{"nickname":"test"}}`)

	opts := WithCustomHeaders(map[string]string{
		"User-Agent": "", // empty = ignored
	})
	c, _ := NewClient("__pus=abc", WithDoer(f), opts)
	_, _ = c.AccountInfo(context.Background())

	ua := f.Calls[0].Header.Get("User-Agent")
	if !strings.Contains(ua, "Chrome") {
		t.Errorf("empty override should fall back to default, got: %s", ua)
	}

	// Verify cookie header is set correctly.
	cookie := f.Calls[0].Header.Get("Cookie")
	if !strings.Contains(cookie, "__pus=abc") {
		t.Errorf("Cookie header should carry __pus, got: %s", cookie)
	}
}

// Verify http.Client implements Doer (compile-time check in doer.go handles this).
var _ = http.DefaultClient
