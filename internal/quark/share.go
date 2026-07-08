package quark

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/phpgao/diskcli/internal/provider"
)

// stokenResponse is the stoken response.
//
// requestJSON unmarshals the envelope's "data" object directly into the
// target, so these structs are flat — they mirror the inner object, not the
// full envelope.
type stokenResponse struct {
	Stoken string `json:"stoken"`
}

// shareDetailResponse is the shared-file list response.
type shareDetailResponse struct {
	List []struct {
		Fid           string `json:"fid"`
		FileName      string `json:"file_name"`
		Size          int64  `json:"size"`
		Dir           bool   `json:"dir"`
		ShareFidToken string `json:"share_fid_token"`
	} `json:"list"`
}

// shareInfoResponse is used to query a share after it is created.
type shareInfoResponse struct {
	ShareID   string  `json:"share_id"`
	Title     string  `json:"title"`
	Passcode  string  `json:"passcode"`
	ShareURL  string  `json:"share_url"`
	ExpiredAt float64 `json:"expired_at"`
}

// myShareItem is an entry in the my-shares list.
// myShareEntry is a single entry in the my-shares list.
//
// It is a named type (not an inline anonymous struct) because the Quark
// response's expired_at/created_at are large millisecond timestamps; decoding
// them reliably requires a stable, reflectable field set. The inline anonymous
// struct form intermittently failed to bind expired_at (see history).
type myShareEntry struct {
	ShareID   string  `json:"share_id"`
	Title     string  `json:"title"`
	ShareURL  string  `json:"share_url"`
	Passcode  string  `json:"passcode"`
	CreatedAt float64 `json:"created_at"`
	ExpiresAt float64 `json:"expired_at"`
}

// myShareItem is the my-shares list envelope.
type myShareItem struct {
	List []myShareEntry `json:"list"`
}

// resolveShareToken exchanges pwd_id + passcode for a share stoken (drive-h.quark.cn).
//
// Two query parameters are mixed in for the risk-control layer: __dt is a
// cryptographically random integer in [100, 999], and __t is the current
// unix millisecond timestamp — together they frustrate replay attempts.
func (c *Client) resolveShareToken(ctx context.Context, pwdID, passcode string) (string, error) {
	dtInt, err := randInt(100, 999)
	if err != nil {
		return "", fmt.Errorf("generate random number failed: %w", err)
	}
	t := time.Now().UnixMilli()

	q := url.Values{}
	q.Set("pr", "ucpro")
	q.Set("fr", "pc")
	q.Set("uc_param_str", "")
	q.Set("__dt", strconv.Itoa(dtInt))
	q.Set("__t", strconv.FormatInt(t, 10))

	body := map[string]any{
		"pwd_id":                            pwdID,
		"passcode":                          passcode,
		"support_visit_limit_private_share": true,
	}
	var resp stokenResponse
	path := apiPaths.share.sharepageToken + "?" + q.Encode()
	if err := c.requestJSON(ctx, http.MethodPost, domains.driveH, path, body, &resp); err != nil {
		return "", err
	}
	return resp.Stoken, nil
}

// getShareDetail fetches the full shared file list (on drive-h.quark.cn).
//
// It paginates through every page — a single _size=50 page is not enough for
// shares with more than 50 entries, which previously caused SaveShare to
// silently transfer only the first page. The query parameters mirror the
// working Quark web client (and quark-auto-save): ver=2, force=0 and the
// _fetch_* switches keep the response in the expected shape.
func (c *Client) getShareDetail(ctx context.Context, pwdID, stoken, pdirFid string) ([]*FileItem, error) {
	items := make([]*FileItem, 0)
	page := 1
	for {
		dtInt, _ := randInt(100, 999)
		t := time.Now().UnixMilli()
		q := url.Values{}
		q.Set("pr", "ucpro")
		q.Set("fr", "pc")
		q.Set("pwd_id", pwdID)
		q.Set("stoken", stoken)
		q.Set("pdir_fid", pdirFid)
		q.Set("force", "0")
		q.Set("_page", strconv.Itoa(page))
		q.Set("_size", "50")
		q.Set("_fetch_banner", "0")
		q.Set("_fetch_share", "0")
		q.Set("_fetch_total", "1")
		q.Set("_sort", "file_type:asc,updated_at:desc")
		q.Set("ver", "2")
		q.Set("fetch_share_full_path", "0")
		q.Set("__dt", strconv.Itoa(dtInt))
		q.Set("__t", strconv.FormatInt(t, 10))

		var resp shareDetailResponse
		path := apiPaths.share.sharepageDetail + "?" + q.Encode()
		if err := c.requestJSON(ctx, http.MethodGet, domains.driveH, path, nil, &resp); err != nil {
			return nil, err
		}
		for _, it := range resp.List {
			items = append(items, &FileItem{
				Fid:           it.Fid,
				Name:          it.FileName,
				Size:          it.Size,
				Dir:           it.Dir,
				ShareFidToken: it.ShareFidToken,
			})
		}
		// Stop once a page returns fewer than a full batch — that is the last page.
		if len(resp.List) < 50 {
			break
		}
		page++
		if page > 1000 {
			break
		}
	}
	return items, nil
}

// importShare saves shared files to a local directory.
//
// The request body key for the per-file tokens is "fid_token_list" — this is
// what the Quark sharepage/save endpoint expects. An earlier version sent
// "share_token_list", which the server silently ignored, so nothing got saved.
func (c *Client) importShare(ctx context.Context, pwdID, stoken string, fids, shareTokens []string, toPdirFid string) error {
	dtInt, _ := randInt(100, 999)
	t := time.Now().UnixMilli()
	q := url.Values{}
	q.Set("pr", "ucpro")
	q.Set("fr", "pc")
	q.Set("uc_param_str", "")
	q.Set("app", "clouddrive")
	q.Set("__dt", strconv.Itoa(dtInt))
	q.Set("__t", strconv.FormatInt(t, 10))

	body := map[string]any{
		"fid_list":       fids,
		"fid_token_list": shareTokens,
		"to_pdir_fid":    toPdirFid,
		"pwd_id":         pwdID,
		"stoken":         stoken,
		"pdir_fid":       "0",
		"scene":          "link",
	}
	path := apiPaths.share.sharepageSave + "?" + q.Encode()
	return c.requestJSON(ctx, http.MethodPost, domains.drivePC, path, body, nil)
}

// newShare wraps fids into a new share and returns its details.
//
// url_type picks the access mode: 1 leaves the link open, 2 gates it behind a
// passcode. expired_type sets the lifetime: 1=permanent, 2=1 day, 3=7 days,
// 4=30 days.
//
// Quark creates shares asynchronously: the create call returns only a task_id,
// never a share_id. We poll the task to completion to learn the real share_id,
// then query the password endpoint for the share link. (The old code read
// share_id straight from the create response — which is always empty — and
// then queried the link with an empty id, yielding HTTP 404 "分享不存在".)
func (c *Client) newShare(ctx context.Context, fids []string, title string, urlType, expiredType int, passcode string) (*provider.ShareResult, error) {
	body := map[string]any{
		"fid_list":     fids,
		"title":        title,
		"url_type":     urlType,
		"expired_type": expiredType,
	}
	if passcode != "" {
		body["passcode"] = passcode
	}
	var createResp struct {
		ShareID string `json:"share_id"`
		TaskID  string `json:"task_id"`
	}
	if err := c.requestJSON(ctx, http.MethodPost, domains.drivePC, apiPaths.share.base, body, &createResp); err != nil {
		return nil, err
	}
	shareID := createResp.ShareID
	if shareID == "" {
		if createResp.TaskID == "" {
			return nil, fmt.Errorf("share create returned neither share_id nor task_id")
		}
		var err error
		shareID, err = c.pollShareTask(ctx, createResp.TaskID)
		if err != nil {
			return nil, err
		}
	}
	out, err := c.getSharePassword(ctx, shareID)
	if err != nil {
		return nil, err
	}
	// Some accounts omit the passcode from the password endpoint response; fall
	// back to the passcode we set at creation time so the caller still gets it.
	if out.Passcode == "" && passcode != "" {
		out.Passcode = passcode
	}
	return out, nil
}

// pollShareTask waits for a share-creation task to finish and returns its
// resulting share_id. status 2 means done; anything else means still running,
// so we retry with an incrementing retry_index until it completes or we give
// up after a bounded number of attempts.
func (c *Client) pollShareTask(ctx context.Context, taskID string) (string, error) {
	retry := 0
	for {
		dtInt, _ := randInt(100, 999)
		q := url.Values{}
		q.Set("pr", "ucpro")
		q.Set("fr", "pc")
		q.Set("uc_param_str", "")
		q.Set("task_id", taskID)
		q.Set("retry_index", strconv.Itoa(retry))
		q.Set("__dt", strconv.Itoa(dtInt))
		q.Set("__t", strconv.FormatInt(time.Now().UnixMilli(), 10))

		var resp struct {
			Status  int    `json:"status"`
			ShareID string `json:"share_id"`
		}
		path := apiPaths.task + "?" + q.Encode()
		if err := c.requestJSON(ctx, http.MethodGet, domains.drivePC, path, nil, &resp); err != nil {
			return "", err
		}
		if resp.Status == 2 {
			if resp.ShareID == "" {
				return "", fmt.Errorf("share task finished but returned no share_id")
			}
			return resp.ShareID, nil
		}
		retry++
		if retry > 30 {
			return "", fmt.Errorf("share task did not finish after %d polls", retry)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// getSharePassword fetches the share link (called after creating a share).
func (c *Client) getSharePassword(ctx context.Context, shareID string) (*provider.ShareResult, error) {
	body := map[string]any{"share_id": shareID}
	var resp shareInfoResponse
	if err := c.requestJSON(ctx, http.MethodPost, domains.drivePC, apiPaths.share.password, body, &resp); err != nil {
		return nil, err
	}
	out := &provider.ShareResult{
		ShareURL: resp.ShareURL,
		ShareID:  resp.ShareID,
		Passcode: resp.Passcode,
	}
	if resp.ExpiredAt > 0 {
		out.ExpiresAt = time.UnixMilli(int64(resp.ExpiredAt))
	}
	return out, nil
}

// myShares lists my shares, one page at a time.
//
// page is 1-based; a value <= 0 is treated as page 1. The caller is
// responsible for paging — there is no automatic walk through every page,
// matching the Quark web client's own pagination model.
func (c *Client) myShares(ctx context.Context, page, pageSize int) ([]*provider.ShareItem, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	q := url.Values{}
	q.Set("pr", "ucpro")
	q.Set("fr", "pc")
	q.Set("_page", strconv.Itoa(page))
	q.Set("_size", strconv.Itoa(pageSize))
	q.Set("_order_field", "created_at")
	q.Set("_order_type", "desc")

	var resp myShareItem
	path := apiPaths.share.mypageDetail + "?" + q.Encode()
	if err := c.requestJSON(ctx, http.MethodGet, domains.drivePC, path, nil, &resp); err != nil {
		return nil, err
	}
	items := make([]*provider.ShareItem, 0, len(resp.List))
	for _, it := range resp.List {
		si := &provider.ShareItem{
			ShareID:  it.ShareID,
			Title:    it.Title,
			ShareURL: it.ShareURL,
			Passcode: it.Passcode,
		}
		if it.CreatedAt > 0 {
			si.CreatedAt = time.UnixMilli(int64(it.CreatedAt))
		}
		if it.ExpiresAt > 0 {
			si.ExpiresAt = time.UnixMilli(int64(it.ExpiresAt))
		}
		items = append(items, si)
	}
	return items, nil
}

// removeShare deletes a share.
func (c *Client) removeShare(ctx context.Context, shareID string) error {
	body := map[string]any{"share_ids": []string{shareID}}
	return c.requestJSON(ctx, http.MethodPost, domains.drivePC, apiPaths.share.delete, body, nil)
}

// ParseShareLink extracts pwd_id and passcode from share text.
//
// Supported format: https://pan.quark.cn/s/<pwd_id>#/list/share/... 提取码: xxxx
// The Chinese "提取码" (passcode) pattern in the regex is intentional — it matches
// the real share links Quark users paste from the web UI.
func ParseShareLink(text string) (pwdID, passcode string, err error) {
	re := regexp.MustCompile(`/s/(\w+)(#/list/share.*/(\w+))?`)
	match := re.FindStringSubmatch(text)
	if len(match) < 2 {
		return "", "", fmt.Errorf("failed to extract share ID from text: %s", text)
	}
	pwdID = match[1]
	reCode := regexp.MustCompile(`提取码[:：]\s*(\S+)`)
	matchCode := reCode.FindStringSubmatch(text)
	if len(matchCode) >= 2 {
		passcode = strings.TrimSpace(matchCode[1])
	}
	return pwdID, passcode, nil
}

// randInt returns a cryptographically secure random integer in
// [min, max].
func randInt(min, max int) (int, error) {
	if min >= max {
		return 0, fmt.Errorf("min >= max")
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return 0, err
	}
	n := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	return min + (n % (max - min + 1)), nil
}

// The methods below implement provider.Sharer.

// SaveShare saves a share.
func (a *Adapter) SaveShare(ctx context.Context, shareURL, passcode, dstDir string) error {
	pwdID, code, err := ParseShareLink(shareURL)
	if err != nil {
		return err
	}
	if passcode != "" {
		code = passcode
	}
	stoken, err := a.client.resolveShareToken(ctx, pwdID, code)
	if err != nil {
		return err
	}
	items, err := a.client.getShareDetail(ctx, pwdID, stoken, "0")
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("share has no files")
	}
	// Resolve the destination directory fid.
	dstFid, err := a.client.pathToFid(ctx, normalizePath(dstDir))
	if err != nil {
		return err
	}
	fids := make([]string, 0, len(items))
	tokens := make([]string, 0, len(items))
	for _, it := range items {
		fids = append(fids, it.Fid)
		tokens = append(tokens, it.ShareFidToken)
	}
	return a.client.importShare(ctx, pwdID, stoken, fids, tokens, dstFid)
}

// CreateShare creates a share.
func (a *Adapter) CreateShare(ctx context.Context, paths []string, opts provider.ShareOpts) (*provider.ShareResult, error) {
	fids := make([]string, 0, len(paths))
	for _, p := range paths {
		fid, err := a.client.pathToFid(ctx, normalizePath(p))
		if err != nil {
			return nil, err
		}
		fids = append(fids, fid)
	}
	urlType := 1
	if opts.NeedPasscode {
		urlType = 2
	}
	expired := opts.Expire
	if expired <= 0 {
		expired = 1
	}
	title := pathBase(paths[0])
	return a.client.newShare(ctx, fids, title, urlType, expired, opts.Passcode)
}

// ListShares lists my shares.
func (a *Adapter) ListShares(ctx context.Context, opts provider.ListSharesOpts) ([]*provider.ShareItem, error) {
	return a.client.myShares(ctx, opts.Page, opts.PageSize)
}

// DeleteShare deletes a share.
func (a *Adapter) DeleteShare(ctx context.Context, shareID string) error {
	return a.client.removeShare(ctx, shareID)
}
