// Package provider defines generic cloud-disk interfaces decoupled from specific
// protocols.
//
// Goals:
//   - transfer/cli layers depend only on this interface; adding a new provider
//     requires no changes to orchestration or CLI code.
//   - Paths are used as input (/a/b/c) — no provider-specific IDs leak out.
//   - Authentication diversity (Cookie/OAuth2/RefreshToken) is carried by the
//     generic Credentials type.
//   - Upload/download use io.Reader/io.Writer stream abstractions, compatible
//     with both chunked and resumable models.
//   - Sharing is an optional Sharer interface — not all providers support it.
package provider

import (
	"context"
	"io"
	"time"
)

// Provider is the generic cloud-disk protocol interface.
//
// Implementations handle path-to-internal-ID resolution internally (e.g. Quark
// fid, Baidu fs_id). All methods accept context.Context for cancellation and
// timeouts.
type Provider interface {
	// Name returns the provider identifier (quark/baidu/aliyun/onedrive etc.),
	// used for configuration and logging.
	Name() string

	// Authenticate performs authentication with the given credentials.
	// The credential format is implementation-defined; failures should return
	// sentinel errors.
	Authenticate(ctx context.Context, creds Credentials) error

	// GetUserInfo returns account info and capacity.
	GetUserInfo(ctx context.Context) (*UserInfo, error)

	// List returns the contents of the directory at path.
	// path "/" means root; opts controls paging.
	List(ctx context.Context, path string, opts ListOpts) (*ListResult, error)

	// Stat returns metadata for a single file/directory.
	// Returns a "not found" error when the path does not exist.
	Stat(ctx context.Context, path string) (*FileItem, error)

	// Mkdir creates a directory (parent must already exist).
	Mkdir(ctx context.Context, path string) error

	// Move moves a file/directory to a new location.
	Move(ctx context.Context, srcPath, dstDir string) error

	// Copy copies a file/directory to a target directory.
	Copy(ctx context.Context, srcPath, dstDir string) error

	// Rename renames a file/directory within its parent directory.
	Rename(ctx context.Context, path, newName string) error

	// Delete removes files/directories.
	Delete(ctx context.Context, paths []string) error

	// Upload uploads data to a remote path.
	// src describes the data source; opts controls concurrency and conflict
	// handling.
	Upload(ctx context.Context, dstPath string, src UploadSource, opts UploadOpts) (*UploadResult, error)

	// Download downloads a remote file to the local writer.
	// opts controls resumable transfer etc.
	Download(ctx context.Context, srcPath string, dst io.Writer, opts DownloadOpts) error

	// GetDownloadURL returns a direct download link (supported by some providers
	// for external download tool like aria2). Returns an "unsupported" error if
	// not available.
	GetDownloadURL(ctx context.Context, path string) (string, error)

	// Search looks up files by name within a given scope directory.
	// scope is the directory path to restrict the search; empty means the
	// whole drive. Returns matching FileItems (both files and, when the
	// provider includes them, directories).
	Search(ctx context.Context, query, scope string) ([]*FileItem, error)

	// Unarchive triggers cloud-side extraction of an archive file.
	// The archive is extracted into its own parent directory.
	// conflictMode controls name collision: 1=skip, 2=overwrite, 3=auto-rename.
	// Implementations that do not support extraction return ErrNotSupported.
	Unarchive(ctx context.Context, archivePath string, conflictMode int) error
}

// Sharer is an optional interface for providers that support sharing.
// Sharing models differ greatly across providers (Quark stoken, Baidu
// shareid, OneDrive share links), so it is not forced on every Provider.
type Sharer interface {
	// SaveShare saves (transfers) shared files to a local directory.
	SaveShare(ctx context.Context, shareURL, passcode, dstDir string) error

	// CreateShare creates a share link for the given paths.
	CreateShare(ctx context.Context, paths []string, opts ShareOpts) (*ShareResult, error)

	// ListShares returns the user's shared items.
	ListShares(ctx context.Context, opts ListSharesOpts) ([]*ShareItem, error)

	// DeleteShare deletes a share by its ID.
	DeleteShare(ctx context.Context, shareID string) error
}

// Credentials is a generic credential container supporting various auth methods.
type Credentials struct {
	// Cookie is a full cookie string (Quark/Baidu/Lanzou/Tianyi).
	Cookie string
	// AccessToken is an OAuth2 access token (OneDrive/GoogleDrive).
	AccessToken string
	// RefreshToken is an OAuth2 refresh token (Aliyun/OneDrive).
	RefreshToken string
}

// ListOpts controls list behavior.
type ListOpts struct {
	// PageSize is the page size; 0 means provider default.
	PageSize int
	// All includes hidden files.
	All bool
}

// ListResult is the result of a List call.
type ListResult struct {
	Items []*FileItem
	// Total is the provider-claimed total (may be inaccurate; use for reference only).
	Total int
	// HasMore indicates whether more pages exist.
	HasMore bool
}

// FileItem is a provider-neutral file/directory entry.
type FileItem struct {
	Name        string    `json:"name" yaml:"name"`
	Path        string    `json:"path" yaml:"path"` // full path (e.g. /a/b/c.txt)
	Size        int64     `json:"size" yaml:"size"` // in bytes
	IsDirectory bool      `json:"is_directory" yaml:"is_directory"`
	ModTime     time.Time `json:"mod_time" yaml:"mod_time"`
	CreatedAt   time.Time `json:"created_at" yaml:"created_at"`
	// ProviderInternalID is the provider's internal ID (e.g. Quark fid, Baidu
	// fs_id); intended for logging/debugging only.
	ProviderInternalID string `json:"fid,omitempty" yaml:"fid,omitempty"`
}

// UserInfo is provider-neutral user information.
type UserInfo struct {
	Nickname      string    `json:"nickname" yaml:"nickname"`
	Avatar        string    `json:"avatar,omitempty" yaml:"avatar,omitempty"`
	Account       string    `json:"account,omitempty" yaml:"account,omitempty"`
	UseCapacity   int64     `json:"use_capacity" yaml:"use_capacity"`
	TotalCapacity int64     `json:"total_capacity" yaml:"total_capacity"`
	MemberType    string    `json:"member_type" yaml:"member_type"` // "NORMAL"/"EXP_SVIP" etc.
	ExpiresAt     time.Time `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
}

// UploadSource describes the data source for an upload.
type UploadSource struct {
	// Path is the local file path (used for folder uploads).
	Path string
	// Reader is the data source (takes precedence over Path when set).
	Reader io.Reader
	// Size is the total data size (required in Reader mode, used for chunking
	// and progress).
	Size int64
	// Name is the filename (required in Reader mode).
	Name string
	// ModTime is the modification time; 0 means use current time.
	ModTime time.Time
}

// ConflictStrategy specifies how to handle name collisions.
type ConflictStrategy int

const (
	// ConflictSkip skips on conflict (default).
	ConflictSkip ConflictStrategy = iota
	// ConflictOverwrite replaces the existing file.
	ConflictOverwrite
	// ConflictRename generates a new name (suffix _1, _2, ...).
	ConflictRename
)

// UploadOpts controls upload behavior.
type UploadOpts struct {
	// Threads is the chunk concurrency; 0 means use the provider default.
	Threads int
	// FileThreads is the per-file concurrency inside a folder (0 means
	// default, typically 1 to avoid rate limits).
	FileThreads int
	// OnConflict is the name conflict strategy.
	OnConflict ConflictStrategy
	// Yes skips the overwrite confirmation prompt.
	Yes bool
	// Resume enables resumable upload.
	Resume bool
	// PartSize is the chunk size; 0 means follow the server.
	PartSize int64
}

// UploadResult is the result of an upload operation.
type UploadResult struct {
	Path  string
	Size  int64
	FID   string // provider internal ID (e.g. Quark fid)
	IsDir bool
}

// DownloadOpts controls download behavior.
type DownloadOpts struct {
	// Threads is the concurrent download thread count; 0 means default.
	Threads int
	// OnConflict is the name conflict strategy.
	OnConflict ConflictStrategy
	// Yes skips the overwrite confirmation prompt.
	Yes bool
	// Resume enables resumable download (Range).
	Resume bool
}

// ShareOpts controls share creation.
type ShareOpts struct {
	// Passcode is the passcode (empty means no passcode required).
	Passcode string
	// Expire is the expiration type (1=permanent 2=1day 3=7days 4=30days;
	// the meaning may vary per provider).
	Expire int
	// NeedPasscode forces a passcode.
	NeedPasscode bool
}

// ShareResult is the result of creating a share.
type ShareResult struct {
	ShareURL  string
	ShareID   string
	Passcode  string
	ExpiresAt time.Time
}

// ListSharesOpts controls listing shares.
type ListSharesOpts struct {
	// Page is the 1-based page to fetch. 0 or negative means page 1.
	Page int
	// PageSize is the page size; 0 means provider default.
	PageSize int
}

// ShareItem represents a shared item.
type ShareItem struct {
	ShareID   string    `json:"share_id" yaml:"share_id"`
	Title     string    `json:"title" yaml:"title"`
	ShareURL  string    `json:"share_url" yaml:"share_url"`
	Passcode  string    `json:"passcode,omitempty" yaml:"passcode,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty" yaml:"created_at,omitempty"`
}
