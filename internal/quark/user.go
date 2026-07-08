package quark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// AccountInfo returns the caller's account profile via /account/info (pan.quark.cn).
//
// Unlike the clouddrive family, this endpoint keys its envelope on
// success/code/msg and expects fr=pc&platform=pc as query parameters.
func (c *Client) AccountInfo(ctx context.Context) (*UserInfo, error) {
	// /account/info uses fr=pc&platform=pc (unlike pr=ucpro&fr=pc elsewhere).
	u, err := url.Parse(domains.pan + apiPaths.account.info)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	q := u.Query()
	q.Set("fr", "pc")
	q.Set("platform", "pc")
	u.RawQuery = q.Encode()

	var ui UserInfo
	if err := c.rawRequest(ctx, http.MethodGet, u.String(), nil, &ui); err != nil {
		return nil, err
	}
	return &ui, nil
}

// MembershipInfo returns the membership tier and storage quota via
// /1/clouddrive/member.
//
// The decoded payload carries used/total capacity, member_type, and the
// super-VIP expiry timestamp (super_vip_exp_at).
func (c *Client) MembershipInfo(ctx context.Context) (*MemberInfo, error) {
	var mi MemberInfo
	if err := c.requestJSON(ctx, http.MethodGet, domains.drivePC, apiPaths.account.member+
		"?fetch_subscribe=true&fetch_identity=true", nil, &mi); err != nil {
		return nil, err
	}
	return &mi, nil
}

// rawRequest is used by endpoints with non-standard query parameters
// (e.g. /account/info uses fr=pc&platform=pc).
//
// Unlike requestJSON it takes a full URL (including query) and does not
// append pr=ucpro&fr=pc.
func (c *Client) rawRequest(ctx context.Context, method, fullURL string, body any, target any) error {
	var bodyBytes []byte
	var reader io.Reader
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body failed: %w", err)
		}
		reader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	applyHeaders(req, bodyBytes, c.customHeaders)
	req.Header.Set("Cookie", c.cookies.CookieHeader())

	resp, err := c.doer.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	c.cookies.RefreshFrom(resp)

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		return parseHTTPError(resp.StatusCode, respBytes)
	}

	var sr StandardResponse
	if err := json.Unmarshal(respBytes, &sr); err != nil {
		return fmt.Errorf("unmarshal response JSON failed: %w (body: %s)", err, truncate(string(respBytes), 200))
	}
	if !sr.IsSuccess() {
		return &APIError{Code: sr.Code, Message: sr.ErrorMessage(), Errno: sr.Errno, Errmsg: sr.Errmsg}
	}
	if target != nil && len(sr.Data) > 0 {
		if err := json.Unmarshal(sr.Data, target); err != nil {
			return fmt.Errorf("unmarshal data field failed: %w (raw: %s)", err, truncate(string(respBytes), 200))
		}
	}
	return nil
}
