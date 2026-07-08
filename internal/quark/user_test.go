package quark

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestNewClient_EmptyCookie(t *testing.T) {
	if _, err := NewClient(""); !errors.Is(err, ErrNoCookie) {
		t.Errorf("empty cookie should return ErrNoCookie, got %v", err)
	}
	if _, err := NewClient("   "); !errors.Is(err, ErrNoCookie) {
		t.Errorf("whitespace cookie should return ErrNoCookie, got %v", err)
	}
}

func TestParseCookie(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{
			name: "standard",
			in:   "__pus=abc; __puus=def; tfstk=xyz",
			want: map[string]string{"__pus": "abc", "__puus": "def", "tfstk": "xyz"},
		},
		{
			name: "value with equals",
			in:   "k=v=v2; a=b",
			want: map[string]string{"k": "v=v2", "a": "b"},
		},
		{
			name: "pus prefix fallback",
			in:   "pus=abc; __puus=def",
			want: map[string]string{"__pus": "abc", "__puus": "def"},
		},
		{
			name: "quoted",
			in:   `"__pus=abc"; __puus=def`,
			want: map[string]string{"__pus": "abc", "__puus": "def"},
		},
		{
			name: "extra whitespace",
			in:   "  __pus=abc  ;   __puus=def  ",
			want: map[string]string{"__pus": "abc", "__puus": "def"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCookieValue(tt.in)
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseCookieValue[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestCookieStore_UpdateFromResponse(t *testing.T) {
	cs := NewCookieStore("__pus=old; __puus=old")
	// Simulate a set-cookie response that renews __puus.
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Add("Set-Cookie", "__puus=new; Path=/; Domain=.quark.cn")
	cs.RefreshFrom(resp)

	if v, _ := cs.CookieValue("__puus"); v != "new" {
		t.Errorf("__puus should be updated to new, got %q", v)
	}
	if v, _ := cs.CookieValue("__pus"); v != "old" {
		t.Errorf("__pus should stay old, got %q", v)
	}
}

func TestCookieStore_UpdateFromResponse_IgnoreNewKey(t *testing.T) {
	cs := NewCookieStore("__pus=abc")
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Add("Set-Cookie", "new_cookie=xyz; Path=/")
	cs.RefreshFrom(resp)

	if _, ok := cs.CookieValue("new_cookie"); ok {
		t.Error("new cookie keys should not be introduced")
	}
}

func TestGetUserInfo(t *testing.T) {
	f := &FakeDoer{}
	// /account/info success response.
	f.Register("GET", "/account/info", 200, `{
		"success": true,
		"code": 0,
		"msg": "ok",
		"data": {
			"nickname": "testuser",
			"avatar": "https://example.com/a.png",
			"account": "test@example.com",
			"member_id": "12345"
		}
	}`)

	c, err := NewClient("__pus=abc; __puus=def", WithDoer(f))
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	ui, err := c.AccountInfo(context.Background())
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if ui.Nickname != "testuser" {
		t.Errorf("nickname = %q, want testuser", ui.Nickname)
	}
	if ui.MemberID != "12345" {
		t.Errorf("member_id = %q, want 12345", ui.MemberID)
	}

	// Verify the Cookie header was attached.
	if len(f.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(f.Calls))
	}
	cookie := f.Calls[0].Header.Get("Cookie")
	if !strings.Contains(cookie, "__pus=abc") {
		t.Errorf("request should carry __pus cookie, got: %s", cookie)
	}
	if !strings.Contains(cookie, "__puus=def") {
		t.Errorf("request should carry __puus cookie, got: %s", cookie)
	}
	// Verify query parameters.
	if !strings.Contains(f.Calls[0].URL, "fr=pc") {
		t.Errorf("should carry fr=pc, got: %s", f.Calls[0].URL)
	}
	if !strings.Contains(f.Calls[0].URL, "platform=pc") {
		t.Errorf("should carry platform=pc, got: %s", f.Calls[0].URL)
	}
	// Verify spoofing headers.
	if ua := f.Calls[0].Header.Get("User-Agent"); !strings.Contains(ua, "Chrome") {
		t.Errorf("should carry Chrome UA, got: %s", ua)
	}
	if ref := f.Calls[0].Header.Get("Referer"); ref != defaultHeaderValues.referer {
		t.Errorf("should carry Referer=%s, got: %s", defaultHeaderValues.referer, ref)
	}
}

func TestGetUserInfo_APIError(t *testing.T) {
	f := &FakeDoer{}
	f.Register("GET", "/account/info", 200, `{
		"success": false,
		"code": 32001,
		"msg": "account not logged in"
	}`)

	c, _ := NewClient("__pus=abc", WithDoer(f))
	_, err := c.AccountInfo(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code.Int() != 32001 {
		t.Errorf("code = %d, want 32001", apiErr.Code.Int())
	}
	if !errors.Is(err, ErrAPICall) {
		t.Error("should be identifiable via errors.Is(err, ErrAPICall)")
	}
}

func TestGetUserInfo_HTTPError(t *testing.T) {
	f := &FakeDoer{}
	f.Register("GET", "/account/info", 401, `{"msg":"unauthorized"}`)
	c, _ := NewClient("__pus=abc", WithDoer(f))
	_, err := c.AccountInfo(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should contain 401, got: %v", err)
	}
}

func TestGetMemberInfo(t *testing.T) {
	f := &FakeDoer{}
	f.Register("GET", "/1/clouddrive/member", 200, `{
		"code": 0,
		"status": 200,
		"message": "ok",
		"data": {
			"use_capacity": 1073741824,
			"total_capacity": 1099511627776,
			"member_type": "EXP_SVIP",
			"exp_svip_exp_at": 1735689600000
		}
	}`)

	c, _ := NewClient("__pus=abc", WithDoer(f))
	mi, err := c.MembershipInfo(context.Background())
	if err != nil {
		t.Fatalf("GetMemberInfo failed: %v", err)
	}
	if mi.UseCapacity != 1073741824 {
		t.Errorf("use_capacity = %d, want 1073741824", mi.UseCapacity)
	}
	if mi.TotalCapacity != 1099511627776 {
		t.Errorf("total_capacity = %d, want 1099511627776", mi.TotalCapacity)
	}
	if mi.MemberType != "EXP_SVIP" {
		t.Errorf("member_type = %q, want EXP_SVIP", mi.MemberType)
	}
	// Verify query parameters (pr=ucpro&fr=pc + fetch_subscribe/identity).
	url := f.Calls[0].URL
	if !strings.Contains(url, "pr=ucpro") || !strings.Contains(url, "fr=pc") {
		t.Errorf("should carry pr/fr parameters, got: %s", url)
	}
	if !strings.Contains(url, "fetch_subscribe=true") {
		t.Errorf("should carry fetch_subscribe=true, got: %s", url)
	}
	if !strings.Contains(url, "fetch_identity=true") {
		t.Errorf("should carry fetch_identity=true, got: %s", url)
	}
}

func TestCookieAutoRenewal(t *testing.T) {
	f := &FakeDoer{}
	// First response carries set-cookie to renew __puus.
	f.RegisterWithHeader("GET", "/account/info", 200,
		`{"success":true,"code":0,"data":{"nickname":"x"}}`,
		http.Header{"Set-Cookie": {"__puus=renewed; Path=/; Domain=.quark.cn"}})

	c, _ := NewClient("__pus=abc; __puus=old", WithDoer(f))
	_, _ = c.AccountInfo(context.Background())

	// Verify __puus was renewed.
	if v, _ := c.Cookies().CookieValue("__puus"); v != "renewed" {
		t.Errorf("__puus should auto-renew to renewed, got %q", v)
	}
}
