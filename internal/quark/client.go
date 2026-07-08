package quark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is the Quark pan protocol-layer client.
//
// HTTP transport is abstracted via the Doer interface so unit tests can
// inject a FakeDoer. The default client uses a 30s timeout for regular APIs;
// upload/download use their own clients with custom timeouts.
type Client struct {
	doer    Doer
	cookies *CookieStore
	// Default 30s timeout; long operations such as upload override it via
	// WithTimeout.
	timeout time.Duration
	// customHeaders overrides default browser spoofing headers for this client.
	customHeaders map[string]string
}

// Option configures a Client.
type Option func(*Client)

// WithDoer injects a custom Doer (FakeDoer for unit tests).
func WithDoer(d Doer) Option {
	return func(c *Client) { c.doer = d }
}

// WithTimeout sets the request timeout (default 30s).
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithCustomHeaders overrides default browser spoofing headers (UA, Referer,
// etc.). Entries whose value is empty are stripped from the request entirely
// (use this to suppress a particular header).
func WithCustomHeaders(h map[string]string) Option {
	return func(c *Client) {
		c.customHeaders = h
	}
}

// NewClient creates a Quark protocol client.
//
// cookie is the full Quark cookie string (required; empty returns ErrNoCookie).
func NewClient(cookie string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(cookie) == "" {
		return nil, ErrNoCookie
	}
	c := &Client{
		doer:    http.DefaultClient,
		cookies: NewCookieStore(cookie),
		timeout: 30 * time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Cookies returns the cookie store (used to read the Cookie header for OSS PUT).
func (c *Client) Cookies() *CookieStore { return c.cookies }

// request performs a Quark API call and decodes the standard envelope.
//
// baseURL and path are joined, then pr=ucpro&fr=pc are merged into the query
// string. A non-nil body is JSON-encoded and gets Content-Type: application/json.
// Browser-shaped headers and the Cookie header are applied on the way out; any
// set-cookie in the response refreshes the cookie store (e.g. __puus rotation).
// HTTP >=400 or a non-zero business code surfaces as *APIError.
func (c *Client) request(ctx context.Context, method, baseURL, path string, body any) (*StandardResponse, []byte, error) {
	var bodyBytes []byte
	var reader io.Reader
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal request body failed: %w", err)
		}
		reader = bytes.NewReader(bodyBytes)
	}

	reqURL, err := buildURL(baseURL, path)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("create request failed: %w", err)
	}

	// Inject spoofing headers (Content-Type: application/json is set when body != nil).
	applyHeaders(req, bodyBytes, c.customHeaders)
	// Inject the Cookie header.
	req.Header.Set("Cookie", c.cookies.CookieHeader())

	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Auto-renew __puus.
	c.cookies.RefreshFrom(resp)

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, respBytes, parseHTTPError(resp.StatusCode, respBytes)
	}

	var sr StandardResponse
	if err := json.Unmarshal(respBytes, &sr); err != nil {
		return nil, respBytes, fmt.Errorf("unmarshal response JSON failed: %w (body: %s)", err, truncate(string(respBytes), 200))
	}
	return &sr, respBytes, nil
}

// requestJSON is a convenience wrapper around request that unmarshals data
// into target.
func (c *Client) requestJSON(ctx context.Context, method, baseURL, path string, body any, target any) error {
	sr, raw, err := c.request(ctx, method, baseURL, path, body)
	if err != nil {
		return err
	}
	if !sr.IsSuccess() {
		return &APIError{Code: sr.Code, Message: sr.ErrorMessage(), Errno: sr.Errno, Errmsg: sr.Errmsg}
	}
	if target != nil && len(sr.Data) > 0 {
		if err := json.Unmarshal(sr.Data, target); err != nil {
			return fmt.Errorf("unmarshal data field failed: %w (raw: %s)", err, truncate(string(raw), 200))
		}
	}
	return nil
}

// buildURL joins baseURL + path and appends the pr=ucpro&fr=pc common query
// parameters.
//
// path must start with "/"; an existing query string is preserved and
// extended.
func buildURL(baseURL, path string) (string, error) {
	u, err := url.Parse(baseURL + path)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	q := u.Query()
	for k, vs := range defaultQueryParams() {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// parseHTTPError extracts a business error message from an HTTP error response.
func parseHTTPError(status int, body []byte) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err == nil {
		msg, _ := m["message"].(string)
		if msg == "" {
			msg, _ = m["msg"].(string)
		}
		if msg == "" {
			msg, _ = m["errmsg"].(string)
		}
		if msg != "" {
			return fmt.Errorf("HTTP %d: %s", status, msg)
		}
	}
	bodyStr := truncate(string(body), 500)
	return fmt.Errorf("HTTP %d: %s", status, bodyStr)
}

// truncate trims s to maxLen and appends an ellipsis when truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
