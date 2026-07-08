package quark

import (
	"net/http"
	"strings"
	"sync"
)

// CookieStore is a thread-safe cookie store with automatic __puus renewal.
//
// Design: the cookie map is managed manually (not via cookiejar) because:
//  1. The OSS PUT uses an independent client and needs Quark cookies injected
//     into OSS-domain request headers; a jar cannot attach them cross-domain.
//  2. Manual management is easier to unit-test (FakeDoer asserts the Cookie
//     header directly).
//  3. __puus renewal only needs to extract the value from set-cookie and
//     update the map.
//
// __puus auto-renewal: after each API call, extract the latest __puus from
// the response set-cookie header and write it back to keep the session alive.
type CookieStore struct {
	mu  sync.RWMutex
	raw string
	m   map[string]string
}

// NewCookieStore parses a cookie string and creates the store.
func NewCookieStore(raw string) *CookieStore {
	return &CookieStore{
		raw: raw,
		m:   parseCookieValue(raw),
	}
}

// CookieHeader returns the Cookie request header value (k1=v1; k2=v2).
func (cs *CookieStore) CookieHeader() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return formatCookie(cs.m)
}

// CookieValue returns a single cookie value.
func (cs *CookieStore) CookieValue(key string) (string, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	v, ok := cs.m[key]
	return v, ok
}

// RefreshFrom updates the in-memory cookies from the HTTP response's
// set-cookie headers (used for automatic __puus renewal).
//
// Only existing keys are updated to avoid pulling in unrelated cookies;
// __puus is the primary renewal target.
func (cs *CookieStore) RefreshFrom(resp *http.Response) {
	if resp == nil {
		return
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, c := range resp.Cookies() {
		// Only update existing keys to avoid pollution (new cookies take
		// effect the next time they are explicitly configured).
		if _, ok := cs.m[c.Name]; ok {
			cs.m[c.Name] = c.Value
		}
	}
	// Rebuild raw.
	cs.raw = formatCookie(cs.m)
}

// parseCookieValue parses a cookie string into a map.
//
// Parsing rules:
//   - Split on ';' (honoring semicolons nested inside double quotes).
//   - Each segment is split on the first '=' into key/value (the value may
//     itself contain '=').
//   - Trim surrounding whitespace.
//   - When __pus is missing, fall back to the unprefixed form (some cookies
//     copied from browsers lack the __pus prefix).
func parseCookieValue(cookieStr string) map[string]string {
	cookies := make(map[string]string)
	for _, part := range tokenizeCookieValue(cookieStr) {
		if part == "" {
			continue
		}
		// Strip surrounding quotes if present.
		if len(part) >= 2 && part[0] == '"' && part[len(part)-1] == '"' {
			part = part[1 : len(part)-1]
		}
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(part[:eq])
		val := strings.TrimSpace(part[eq+1:])
		if key != "" {
			cookies[key] = val
		}
	}
	// Fall back to the __pus prefix when missing (Quark-specific handling).
	if _, ok := cookies["__pus"]; !ok {
		if v, ok := cookies["pus"]; ok {
			cookies["__pus"] = v
			delete(cookies, "pus")
		}
	}
	return cookies
}

// tokenizeCookieValue splits s on ';' while honoring double-quoted segments.
//
// Implementation walks the bytes with strings.IndexByte rather than ranging
// over runes and accumulating through a strings.Builder: a sliced substring
// is cheaper to allocate, and the byte-oriented walk is correct because both
// the delimiter (';') and the quote character ('"') are ASCII.
func tokenizeCookieValue(s string) []string {
	var parts []string
	start := 0
	inQuotes := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			inQuotes = !inQuotes
		case ';':
			if !inQuotes {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if start < len(s) {
		parts = append(parts, strings.TrimSpace(s[start:]))
	}
	return parts
}
