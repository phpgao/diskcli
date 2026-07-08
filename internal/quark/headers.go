package quark

import (
	"net/http"
	"strings"
)

// defaultHeaderValues bundles the handful of header values that need to be
// referenced individually outside the default map — e.g. the OSS signing
// path needs the aliyun user-agent, and the download streaming path sets
// the Referer/User-Agent directly without going through applyHeaders.
//
// The remaining defaults (the bulk of the spoofing map, including the
// sec-ch-ua family) live inside defaultHeaders and never need to be
// addressed by name.
var defaultHeaderValues = struct {
	referer      string
	origin       string
	userAgent    string
	ossUserAgent string
}{
	referer:      "https://pan.quark.cn/list",
	origin:       "https://pan.quark.cn",
	userAgent:    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
	ossUserAgent: "aliyun-sdk-js/1.0.0 Chrome 150.0.0.0 on macOS 15.6.0 64-bit",
}

// defaultHeaders returns a fresh map of browser-spoofing headers required
// by the Quark web API.
//
// The gateway runs risk-control heuristics over Referer/Origin/UA and the
// sec-ch-ua family; omitting them causes the call to be flagged as a non-
// browser client and rejected with auth errors or throttling.
//
// A new map is returned on every call so callers never observe mutations
// made by other code holding the same reference (the previous global map
// was a shared mutable singleton — an accident waiting to happen).
func defaultHeaders() map[string]string {
	return map[string]string{
		"User-Agent":                  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
		"Referer":                     "https://pan.quark.cn/list",
		"Origin":                      "https://pan.quark.cn",
		"Accept":                      "application/json, text/plain, */*",
		"Accept-Language":             "zh-CN,zh;q=0.9",
		"Cache-Control":               "no-cache",
		"Pragma":                      "no-cache",
		"Priority":                    "u=1, i",
		"Sec-Ch-Ua":                   `"Chromium";v="150", "Google Chrome";v="150", "Not_A Brand";v="99"`,
		"Sec-Ch-Ua-Arch":              `"arm"`,
		"Sec-Ch-Ua-Bitness":           `"64"`,
		"Sec-Ch-Ua-Full-Version":      `"150.0.0.0"`,
		"Sec-Ch-Ua-Full-Version-List": `"Chromium";v="150.0.0.0", "Google Chrome";v="150.0.0.0", "Not_A Brand";v="99.0.0.0"`,
		"Sec-Ch-Ua-Mobile":            "?0",
		"Sec-Ch-Ua-Model":             `""`,
		"Sec-Ch-Ua-Platform":          `"macOS"`,
		"Sec-Ch-Ua-Platform-Version":  `"15.6.0"`,
		"Sec-Ch-Ua-Wow64":             "?0",
		"Sec-Fetch-Dest":              "empty",
		"Sec-Fetch-Mode":              "cors",
		"Sec-Fetch-Site":              "same-origin",
	}
}

// applyHeaders injects browser-spoofing headers, the caller's overrides,
// and (when body is non-nil) the JSON content type into req.
//
// Precedence: defaults first, then customOverrides; entries in customOverrides
// whose value is empty are skipped so a caller can fall back to the default
// for a key by passing "". The Cookie header is not touched here — the
// caller attaches it from the cookie store.
func applyHeaders(req *http.Request, body []byte, customOverrides map[string]string) {
	for k, v := range defaultHeaders() {
		req.Header.Set(k, v)
	}
	for k, v := range customOverrides {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
}

// formatCookie renders a cookie map into the HTTP Cookie header wire form
// (k1=v1; k2=v2).
//
// Used when bypassing the cookie jar to attach cookies directly — for
// example, when the OSS PUT goes through an independent client that
// cannot reach the jar cross-domain.
func formatCookie(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}
