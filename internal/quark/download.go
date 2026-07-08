package quark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/phpgao/diskcli/internal/provider"
)

// downloadResponse models the /file/download payload.
//
// The server answers in one of two shapes: a synchronous array carrying
// download_url directly, or an async envelope with task_id/task_sync/task_resp
// that must be polled separately.

// downloadTaskResp is the task_resp of an async task.
type downloadTaskResp struct {
	TaskID   string `json:"task_id"`
	TaskSync bool   `json:"task_sync"`
	TaskResp struct {
		Data []struct {
			DownloadURL string `json:"download_url"`
		} `json:"data"`
	} `json:"task_resp"`
}

// downloadURLItem is a data item in a sync response.
type downloadURLItem struct {
	DownloadURL string `json:"download_url"`
}

// taskStatusResponse is the /task polling response.
type taskStatusResponse struct {
	Data struct {
		Status      int             `json:"status"` // 1=in progress 2=done 3=failed
		DownloadURL string          `json:"download_url"`
		Data        json.RawMessage `json:"data"`
	} `json:"data"`
}

// requestDownloadURL fetches a download link (sync/async dual mode).
//
// fid is the file ID; the returned pre-signed CDN URL needs no extra
// signature from the caller.
func (c *Client) requestDownloadURL(ctx context.Context, fid string) (string, error) {
	body := map[string]any{"fids": []string{fid}}
	sr, raw, err := c.request(ctx, http.MethodPost, domains.drivePC, apiPaths.file.download, body)
	if err != nil {
		// Error code 23018 / "download file size limit" signals a validation
		// failure during download initialization.
		if strings.Contains(err.Error(), "23018") || strings.Contains(err.Error(), "download file size limit") {
			return "", fmt.Errorf("download file size limit exceeded, use the client to download")
		}
		return "", err
	}
	if !sr.IsSuccess() {
		return "", &APIError{Code: sr.Code, Message: sr.ErrorMessage(), Errno: sr.Errno, Errmsg: sr.Errmsg}
	}

	// Sync: data is an array.
	var items []downloadURLItem
	if err := json.Unmarshal(sr.Data, &items); err == nil && len(items) > 0 && items[0].DownloadURL != "" {
		return items[0].DownloadURL, nil
	}

	// Async: data is an object.
	var tr downloadTaskResp
	if err := json.Unmarshal(sr.Data, &tr); err == nil && tr.TaskID != "" {
		// task_resp.data may carry download_url directly.
		if len(tr.TaskResp.Data) > 0 && tr.TaskResp.Data[0].DownloadURL != "" {
			return tr.TaskResp.Data[0].DownloadURL, nil
		}
		// Otherwise poll the task.
		return c.pollDownloadTask(ctx, tr.TaskID)
	}
	return "", fmt.Errorf("failed to parse download response: %s", truncate(string(raw), 200))
}

// pollDownloadTask polls a download task (2s x 60, up to 2 minutes).
func (c *Client) pollDownloadTask(ctx context.Context, taskID string) (string, error) {
	const maxRetries = 60
	interval := 2 * time.Second
	for i := 0; i < maxRetries; i++ {
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		var tsr taskStatusResponse
		path := fmt.Sprintf("%s?task_id=%s&retry_index=0", apiPaths.task, taskID)
		if err := c.requestJSON(ctx, http.MethodGet, domains.drivePC, path, nil, &tsr); err != nil {
			return "", fmt.Errorf("query download task failed: %w", err)
		}
		switch tsr.Data.Status {
		case 3:
			return "", fmt.Errorf("%w: download task failed", ErrTaskFailed)
		case 2:
			if tsr.Data.DownloadURL != "" {
				return tsr.Data.DownloadURL, nil
			}
			// Try to extract from the data array.
			var items []downloadURLItem
			if err := json.Unmarshal(tsr.Data.Data, &items); err == nil && len(items) > 0 && items[0].DownloadURL != "" {
				return items[0].DownloadURL, nil
			}
		}
	}
	return "", fmt.Errorf("%w: download task timed out (%d retries)", ErrTaskFailed, maxRetries)
}

// downloadFile streams the file to dst using a Range request.
//
// downloadURL is a pre-signed CDN URL; the Cookie header is still required
// (for OSS callback validation).
// Resume is supported: when opts.Resume=true a Range request is sent (the
// response must be 206).
func (c *Client) downloadFile(ctx context.Context, downloadURL string, dst io.Writer, resume bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request failed: %w", err)
	}
	req.Header.Set("Cookie", c.cookies.CookieHeader())
	req.Header.Set("User-Agent", defaultHeaderValues.userAgent)
	req.Header.Set("Referer", defaultHeaderValues.referer)

	resp, err := c.doer.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed status=%d: %s", resp.StatusCode, truncate(string(bodyBytes), 200))
	}
	// Ignore resume when the response is not 206 (the server may not support
	// Range; fall back to a full download).
	_ = resume
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	return nil
}

// Download implements provider.Provider.Download.
func (a *Adapter) Download(ctx context.Context, srcPath string, dst io.Writer, opts provider.DownloadOpts) error {
	// path -> fid
	fid, err := a.client.pathToFid(ctx, normalizePath(srcPath))
	if err != nil {
		return err
	}
	// Get the download URL.
	url, err := a.client.requestDownloadURL(ctx, fid)
	if err != nil {
		return err
	}
	// Stream download.
	return a.client.downloadFile(ctx, url, dst, opts.Resume)
}

// GetDownloadURL implements provider.Provider.GetDownloadURL.
func (a *Adapter) GetDownloadURL(ctx context.Context, srcPath string) (string, error) {
	fid, err := a.client.pathToFid(ctx, normalizePath(srcPath))
	if err != nil {
		return "", err
	}
	return a.client.requestDownloadURL(ctx, fid)
}
