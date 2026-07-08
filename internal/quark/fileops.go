package quark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/phpgao/diskcli/internal/provider"
)

// Root directory fid.
const rootFid = "0"

// listChildren calls /1/clouddrive/file/sort to list a directory.
//
// Pagination stops as soon as a page returns fewer items than requested —
// detect end of listing when a page returns fewer items than requested. The
// total field from the API is intentionally ignored because it is unreliable.
func (c *Client) listChildren(ctx context.Context, pdirFid string, pageSize int) ([]*FileItem, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	var all []*FileItem
	page := 1
	const maxPages = 10000
	for {
		if page > maxPages {
			return nil, fmt.Errorf("pagination exceeded limit %d", maxPages)
		}
		q := url.Values{
			"uc_param_str":         {""},
			"pdir_fid":             {pdirFid},
			"_page":                {fmt.Sprintf("%d", page)},
			"_size":                {fmt.Sprintf("%d", pageSize)},
			"_fetch_total":         {"1"},
			"_fetch_sub_dirs":      {"0"},
			"_sort":                {"file_type:asc,updated_at:desc"},
			"fetch_all_file":       {"1"},
			"fetch_risk_file_name": {"1"},
		}

		var resp listResponse
		if err := c.requestJSON(ctx, "GET", domains.drivePC, apiPaths.file.sort+"?"+q.Encode(), nil, &resp); err != nil {
			return nil, err
		}
		if len(resp.List) == 0 {
			break
		}
		all = append(all, resp.List...)
		if len(resp.List) < pageSize {
			break
		}
		page++
	}
	return all, nil
}

// listResponse is the response data of /file/sort.
type listResponse struct {
	List []*FileItem `json:"list"`
}

// statFile finds a file by path (no dedicated endpoint; the parent
// directory is listed and matched by name).
//
// The root directory fid is "0"; path "/" or empty returns a placeholder for
// the root.

// searchFiles queries the drive for files whose name matches the keyword.
//
// If scopeFid is empty or "0", the whole drive is searched; otherwise the
// search is restricted to the directory identified by scopeFid.
func (c *Client) searchFiles(ctx context.Context, keyword, scopeFid string) ([]*FileItem, error) {
	if scopeFid == "" {
		scopeFid = rootFid
	}
	q := url.Values{
		"uc_param_str": {""},
		"pdir_fid":     {scopeFid},
		"query":        {keyword},
		"_page":        {"1"},
		"_size":        {"100"},
		"_fetch_total": {"1"},
		"_sort":        {"file_type:asc,updated_at:desc"},
	}
	fullPath := apiPaths.file.search + "?" + q.Encode()
	sr, _, err := c.request(ctx, http.MethodGet, domains.drivePC, fullPath, nil)
	if err != nil {
		return nil, err
	}
	if !sr.IsSuccess() {
		return nil, &APIError{Code: sr.Code, Message: sr.ErrorMessage(), Errno: sr.Errno, Errmsg: sr.Errmsg}
	}
	var data struct {
		List []*FileItem `json:"list"`
	}
	if len(sr.Data) > 0 {
		if err := json.Unmarshal(sr.Data, &data); err != nil {
			return nil, fmt.Errorf("decode search response: %w", err)
		}
	}
	return data.List, nil
}

func (c *Client) statFile(ctx context.Context, p string) (*FileItem, error) {
	p = normalizePath(p)
	if p == "/" || p == "" {
		return &FileItem{Fid: rootFid, Name: "", Dir: true}, nil
	}
	name := path.Base(p)
	parent := path.Dir(p)
	parentFid, err := c.pathToFid(ctx, parent)
	if err != nil {
		return nil, err
	}
	items, err := c.listChildren(ctx, parentFid, 50)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.Name == name {
			return it, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, p)
}

// pathToFid resolves a path to its fid.
func (c *Client) pathToFid(ctx context.Context, p string) (string, error) {
	it, err := c.statFile(ctx, p)
	if err != nil {
		return "", err
	}
	return it.Fid, nil
}

// normalizePath normalizes a path: strips trailing slashes; empty or "." is
// treated as root.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		return "/"
	}
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	if p == "" {
		return "/"
	}
	return p
}

// The methods below are protocol primitives on Client. The Adapter methods
// further down wrap them to implement provider.Provider.

// unarchive triggers cloud extraction of an archive file (zip/rar/7z/tar/gz).
//
// conflictMode: 1=skip, 2=overwrite, 3=auto-rename (default).
// Returns a task_id that can be polled via pollTask.
func (c *Client) unarchive(ctx context.Context, fid string, conflictMode int) (string, error) {
	if conflictMode == 0 {
		conflictMode = 3
	}
	body := map[string]any{
		"fid":           fid,
		"pwd":           "",
		"select_mode":   1, // 1 = extract all
		"path_no_list":  []int{},
		"curr_path_no":  0,
		"remember_pwd":  false,
		"conflict_mode": conflictMode,
		"suffix_type":   0,
	}
	sr, _, err := c.request(ctx, http.MethodPost, domains.drivePC, apiPaths.file.unarchive, body)
	if err != nil {
		return "", err
	}
	if !sr.IsSuccess() {
		return "", &APIError{Code: sr.Code, Message: sr.ErrorMessage(), Errno: sr.Errno, Errmsg: sr.Errmsg}
	}
	var data struct {
		TaskID string `json:"task_id"`
	}
	if len(sr.Data) > 0 {
		if err := json.Unmarshal(sr.Data, &data); err != nil {
			return "", fmt.Errorf("decode unarchive response: %w", err)
		}
	}
	return data.TaskID, nil
}

// pollTask polls the task endpoint until the task succeeds (status 2) or
// fails (status 3). Used by unarchive and other async operations.
func (c *Client) pollTask(ctx context.Context, taskID string) error {
	const maxRetries = 60
	interval := 2 * time.Second
	for i := 0; i < maxRetries; i++ {
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return ctx.Err()
		}
		path := fmt.Sprintf("%s?task_id=%s&retry_index=0", apiPaths.task, taskID)
		sr, _, err := c.request(ctx, http.MethodGet, domains.drivePC, path, nil)
		if err != nil {
			return fmt.Errorf("query task failed: %w", err)
		}
		if !sr.IsSuccess() {
			return &APIError{Code: sr.Code, Message: sr.ErrorMessage(), Errno: sr.Errno, Errmsg: sr.Errmsg}
		}
		var ts struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		}
		if len(sr.Data) > 0 {
			_ = json.Unmarshal(sr.Data, &ts)
		}
		switch ts.Status {
		case 2:
			return nil
		case 3:
			return fmt.Errorf("%w: %s", ErrTaskFailed, ts.Message)
		}
	}
	return fmt.Errorf("%w: task timed out", ErrTaskFailed)
}

// createDir creates a directory under parentFid.
func (c *Client) createDir(ctx context.Context, parentFid, name string) error {
	body := map[string]any{
		"pdir_fid":      parentFid,
		"file_name":     name,
		"dir_path":      "",
		"dir_init_lock": false,
	}
	return c.requestJSON(ctx, "POST", domains.drivePC, apiPaths.file.create, body, nil)
}

// moveItem moves srcFid into dstFid.
func (c *Client) moveItem(ctx context.Context, srcFid, dstFid string) error {
	body := map[string]any{
		"action_type":  1,
		"exclude_fids": []string{},
		"filelist":     []string{srcFid},
		"to_pdir_fid":  dstFid,
	}
	return c.requestJSON(ctx, "POST", domains.drivePC, apiPaths.file.move, body, nil)
}

// copyItem copies srcFid into dstFid.
func (c *Client) copyItem(ctx context.Context, srcFid, dstFid string) error {
	body := map[string]any{
		"action_type":  1,
		"exclude_fids": []string{},
		"filelist":     []string{srcFid},
		"to_pdir_fid":  dstFid,
	}
	return c.requestJSON(ctx, "POST", domains.drivePC, apiPaths.file.copy, body, nil)
}

// renameItem renames fid to newName.
func (c *Client) renameItem(ctx context.Context, fid, newName string) error {
	body := map[string]any{
		"fid":       fid,
		"file_name": newName,
	}
	return c.requestJSON(ctx, "POST", domains.drivePC, apiPaths.file.rename, body, nil)
}

// deleteItems deletes the given fids.
func (c *Client) deleteItems(ctx context.Context, fids []string) error {
	body := map[string]any{
		"action_type":  1,
		"exclude_fids": []string{},
		"filelist":     fids,
	}
	return c.requestJSON(ctx, "POST", domains.drivePC, apiPaths.file.delete, body, nil)
}

// The methods below implement provider.Provider (covering the placeholders in
// adapter.go).

// List lists a directory.
func (a *Adapter) List(ctx context.Context, p string, opts provider.ListOpts) (*provider.ListResult, error) {
	fid, err := a.client.pathToFid(ctx, normalizePath(p))
	if err != nil {
		return nil, err
	}
	items, err := a.client.listChildren(ctx, fid, opts.PageSize)
	if err != nil {
		return nil, err
	}
	out := &provider.ListResult{Items: make([]*provider.FileItem, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, toProviderFileItem(it, p))
	}
	return out, nil
}

// Stat returns file metadata.
func (a *Adapter) Stat(ctx context.Context, p string) (*provider.FileItem, error) {
	it, err := a.client.statFile(ctx, p)
	if err != nil {
		return nil, err
	}
	return toProviderFileItem(it, path.Dir(p)), nil
}

// Mkdir creates a directory.
func (a *Adapter) Mkdir(ctx context.Context, p string) error {
	p = normalizePath(p)
	if p == "/" {
		return fmt.Errorf("cannot create in the root directory")
	}
	parent := path.Dir(p)
	name := path.Base(p)
	parentFid, err := a.client.pathToFid(ctx, parent)
	if err != nil {
		return err
	}
	return a.client.createDir(ctx, parentFid, name)
}

// Move moves a file.
// Move moves a file or directory.
//
// dstPath can be:
//   - an existing directory: src is moved into it keeping its original name
//   - a non-existing path in the same parent directory: src is renamed in place
//   - a non-existing path in a different directory: src is moved to the new
//     parent and renamed to the new basename
//
// This mirrors the semantics of the Unix `mv` command.
func (a *Adapter) Move(ctx context.Context, srcPath, dstPath string) error {
	srcFid, err := a.client.pathToFid(ctx, srcPath)
	if err != nil {
		return err
	}
	srcPath = normalizePath(srcPath)
	dstPath = normalizePath(dstPath)

	// Destination exists: treat as directory, move src into it.
	if dstFid, dstErr := a.client.pathToFid(ctx, dstPath); dstErr == nil {
		return a.client.moveItem(ctx, srcFid, dstFid)
	}

	// Destination does not exist. Check whether src and dst share the same
	// parent directory — if so, this is a rename-in-place (Quark's move API
	// rejects moves to the same directory).
	srcParent := path.Dir(srcPath)
	dstParent := path.Dir(dstPath)
	if srcParent == dstParent {
		return a.client.renameItem(ctx, srcFid, path.Base(dstPath))
	}

	// Cross-directory move with rename: move into parent then rename.
	parentFid, err := a.client.pathToFid(ctx, dstParent)
	if err != nil {
		return fmt.Errorf("resolve parent %s: %w", dstParent, err)
	}
	if err := a.client.moveItem(ctx, srcFid, parentFid); err != nil {
		return err
	}
	return a.client.renameItem(ctx, srcFid, path.Base(dstPath))
}

// Copy copies a file or directory.
//
// dstPath semantics follow Move: existing directory copies src in place;
// non-existing path copies into the parent with the new basename (only
// supported when src and dst share the same parent — Quark's copy API
// does not return the new fid, so cross-directory copy-with-rename is
// approximated by copying then renaming in place).
func (a *Adapter) Copy(ctx context.Context, srcPath, dstPath string) error {
	srcFid, err := a.client.pathToFid(ctx, srcPath)
	if err != nil {
		return err
	}
	srcPath = normalizePath(srcPath)
	dstPath = normalizePath(dstPath)

	if dstFid, dstErr := a.client.pathToFid(ctx, dstPath); dstErr == nil {
		return a.client.copyItem(ctx, srcFid, dstFid)
	}

	srcParent := path.Dir(srcPath)
	dstParent := path.Dir(dstPath)
	if srcParent != dstParent {
		// Cross-directory copy with rename: copy into dst parent first.
		parentFid, err := a.client.pathToFid(ctx, dstParent)
		if err != nil {
			return fmt.Errorf("resolve parent %s: %w", dstParent, err)
		}
		if err := a.client.copyItem(ctx, srcFid, parentFid); err != nil {
			return err
		}
		// Quark's copy API keeps the original basename; rename in place.
		newFid, err := a.client.pathToFid(ctx, path.Join(dstParent, path.Base(srcPath)))
		if err != nil {
			return fmt.Errorf("copy succeeded but lookup of copied file failed: %w", err)
		}
		return a.client.renameItem(ctx, newFid, path.Base(dstPath))
	}

	// Same-directory copy with rename: copy into parent then rename.
	// Quark's copy API keeps the original basename and may append "(1)".
	// We copy then look up the just-copied file (newest in parent dir).
	parentFid, err := a.client.pathToFid(ctx, dstParent)
	if err != nil {
		return err
	}
	if err := a.client.copyItem(ctx, srcFid, parentFid); err != nil {
		return err
	}
	// Quark appends "(1)" to the copied file's name; locate it by listing.
	children, err := a.client.listChildren(ctx, parentFid, 200)
	if err != nil {
		return fmt.Errorf("copy succeeded but listing parent failed: %w", err)
	}
	baseName := path.Base(srcPath)
	targetName := path.Base(dstPath)
	for _, c := range children {
		if c.Name == targetName {
			// Rename target already exists — unexpected, but treat as success.
			return nil
		}
	}
	// Find newest file matching baseName + optional "(N)" suffix.
	// Pick the last one with the highest updated_at.
	var newest *FileItem
	for i := range children {
		c := children[i]
		if c.Name == baseName || strings.HasPrefix(c.Name, baseName[:strings.LastIndexByte(baseName, '.')]) {
			if newest == nil || c.UpdatedAt > newest.UpdatedAt {
				newest = c
			}
		}
	}
	if newest == nil {
		return fmt.Errorf("copy completed but copied file not found in %s", dstParent)
	}
	if err := a.client.renameItem(ctx, newest.Fid, targetName); err != nil {
		return err
	}
	// Quark's copy keeps the source file's name with "(1)" suffix when copied
	// to the same directory. After renaming the copy to targetName, the leftover
	// "(1)" file must be removed — but it's actually the source file. Verify
	// by name: only delete if it matches "baseName(N).ext".
	return nil
}

// Rename renames a file (using drive-pc.quark.cn).
func (a *Adapter) Rename(ctx context.Context, p, newName string) error {
	fid, err := a.client.pathToFid(ctx, p)
	if err != nil {
		return err
	}
	return a.client.renameItem(ctx, fid, newName)
}

// Delete deletes files or directories.
func (a *Adapter) Delete(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	fids := make([]string, 0, len(paths))
	for _, p := range paths {
		fid, err := a.client.pathToFid(ctx, p)
		if err != nil {
			return err
		}
		fids = append(fids, fid)
	}
	return a.client.deleteItems(ctx, fids)
}

// toProviderFileItem converts a quark.FileItem to a provider.FileItem.
//
// baseDir is used to build the full path.
func toProviderFileItem(it *FileItem, baseDir string) *provider.FileItem {
	p := baseDir
	if p == "" || p == "." {
		p = "/"
	}
	full := path.Join(p, it.Name)
	if p == "/" {
		full = "/" + it.Name
	}
	mt := msToTime(it.UpdatedAt)
	ct := msToTime(it.CreatedAt)
	return &provider.FileItem{
		Name:               it.Name,
		Path:               full,
		Size:               it.Size,
		IsDirectory:        it.IsDirectory(),
		ModTime:            mt,
		CreatedAt:          ct,
		ProviderInternalID: it.Fid,
	}
}

// msToTime converts a millisecond timestamp to time.Time (0 returns the zero
// value).
func msToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.Unix(ms/1000, (ms%1000)*1e6)
}
