package quark

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/phpgao/diskcli/internal/provider"
)

// Adapter wraps quark.Client as a provider.Provider.
//
// It does not implement provider.Sharer (Quark share support lives in
// share.go; the CLI layer asserts the type at runtime).
type Adapter struct {
	client *Client
}

// Compile-time check that Adapter implements provider.Provider.
var _ provider.Provider = (*Adapter)(nil)

// NewAdapter creates a Quark provider adapter.
func NewAdapter(client *Client) *Adapter {
	return &Adapter{client: client}
}

// Name implements provider.Provider.
func (a *Adapter) Name() string { return "quark" }

// Authenticate authenticates with a cookie (Quark only needs a cookie; no
// OAuth2/RefreshToken).
//
// Returns ErrNoCookie when Credentials.Cookie is empty. Authentication is
// verified by calling AccountInfo (a wrapped error is returned on failure).
func (a *Adapter) Authenticate(ctx context.Context, creds provider.Credentials) error {
	if creds.Cookie == "" {
		return ErrNoCookie
	}
	// The cookie was already injected in NewClient; verify it once via
	// AccountInfo here.
	if _, err := a.client.AccountInfo(ctx); err != nil {
		return err
	}
	return nil
}

// GetUserInfo merges /account/info with /1/clouddrive/member.
func (a *Adapter) GetUserInfo(ctx context.Context) (*provider.UserInfo, error) {
	ui, err := a.client.AccountInfo(ctx)
	if err != nil {
		return nil, err
	}
	mi, mErr := a.client.MembershipInfo(ctx)
	out := &provider.UserInfo{
		Nickname: ui.Nickname,
		Avatar:   ui.Avatar,
		Account:  ui.Account,
	}
	if mErr == nil && mi != nil {
		out.UseCapacity = mi.UseCapacity
		out.TotalCapacity = mi.TotalCapacity
		out.MemberType = mi.MemberType
		// Expiry: prefer exp_svip_exp_at (ms), fall back to super_vip_exp_at.
		if mi.ExpSvipExpAt > 0 {
			out.ExpiresAt = time.Unix(mi.ExpSvipExpAt/1000, (mi.ExpSvipExpAt%1000)*1e6)
		} else if mi.SuperVipExpAt > 0 {
			out.ExpiresAt = time.Unix(mi.SuperVipExpAt/1000, (mi.SuperVipExpAt%1000)*1e6)
		}
	}
	return out, nil
}

// Search looks up files by keyword across the drive or within a directory.
// This is a Quark-specific extension; callers type-assert to *quark.Adapter.
func (a *Adapter) Search(ctx context.Context, keyword, scopePath string) ([]*provider.FileItem, error) {
	scopeFid := rootFid
	if scopePath != "" && scopePath != "/" {
		fid, err := a.client.pathToFid(ctx, normalizePath(scopePath))
		if err != nil {
			return nil, err
		}
		scopeFid = fid
	}
	items, err := a.client.searchFiles(ctx, keyword, scopeFid)
	if err != nil {
		return nil, err
	}
	out := make([]*provider.FileItem, 0, len(items))
	for _, it := range items {
		out = append(out, toProviderFileItem(it, scopePath))
	}
	return out, nil
}

// Unarchive triggers cloud extraction of an archive (zip/rar/7z/tar/gz).
//
// Requires SVIP membership (checked by fetching member_type from the server).
// Extracted files land in the "夸克云解压" directory at the drive root.
// conflictMode controls name collision: 1=skip, 2=overwrite, 3=auto-rename.
func (a *Adapter) Unarchive(ctx context.Context, archivePath string, conflictMode int) error {
	// The unarchive endpoint requires SVIP membership.
	mi, err := a.client.MembershipInfo(ctx)
	if err != nil {
		return fmt.Errorf("check membership for unarchive: %w", err)
	}
	if !strings.Contains(mi.MemberType, "SVIP") {
		return fmt.Errorf("unarchive requires SVIP membership (current: %s)", mi.MemberType)
	}

	fid, err := a.client.pathToFid(ctx, normalizePath(archivePath))
	if err != nil {
		return err
	}
	taskID, err := a.client.unarchive(ctx, fid, conflictMode)
	if err != nil {
		return err
	}
	return a.client.pollTask(ctx, taskID)
}

// The methods below are implemented in later milestones; placeholders return
// ErrNotSupported.
// List/Stat/Mkdir/Move/Copy/Rename/Delete are implemented in fileops.go.
// Upload is implemented in upload.go.
// Download/GetDownloadURL are implemented in download.go.

// ErrNotSupported indicates an operation that is not yet supported (placeholder
// to be replaced by later milestones).
var ErrNotSupported = errors.New("not implemented yet")
