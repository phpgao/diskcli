package quark

import "encoding/json"

// Generic response structures.
//
// Quark endpoints return slightly different field names:
//   - /account/info: { success, code, msg, data }
//   - /1/clouddrive/*: { code, status, message, data } or { errno, errmsg, data }
//   - Some: { status, code, message, data }
//
// A permissive struct with multiple json tags is used here so a single struct
// adapts to every response. Both Code/Errno are kept so callers can decide
// which error-code scheme to consult.

// StandardResponse is the generic response envelope.
type StandardResponse struct {
	// Success flag (used by some endpoints).
	Success bool `json:"success"`
	// Error code (0 means success, non-zero means failure). FlexInt tolerates
	// both int and string.
	Code FlexInt `json:"code"`
	// HTTP status code (some endpoints return it inside the body).
	Status FlexInt `json:"status"`
	// Error message (message / msg dual fallback).
	Message string `json:"message"`
	Msg     string `json:"msg"`
	// errno/errmsg fallback.
	Errno  FlexInt `json:"errno"`
	Errmsg string  `json:"errmsg"`
	// Data is the raw response payload, further unmarshaled by each endpoint.
	Data json.RawMessage `json:"data"`
	// Metadata returned by some endpoints (e.g. pre/upload returns part_size).
	Metadata json.RawMessage `json:"metadata"`
}

// IsSuccess reports whether the response indicates success.
//
// It tolerates three conventions: success==true / code==0 / errno==0.
func (r *StandardResponse) IsSuccess() bool {
	if r.Success {
		return true
	}
	// Both code and errno must be 0 (some endpoints return code=0 but errno!=0).
	// In practice Quark endpoints usually expose only one of them, so OR is
	// used as a fallback.
	if r.Code.IsZero() && r.Errno.IsZero() {
		// No error-code field at all: treat as success.
		return true
	}
	return false
}

// ErrorMessage returns the most appropriate error message (errmsg > message > msg).
func (r *StandardResponse) ErrorMessage() string {
	if r.Errmsg != "" {
		return r.Errmsg
	}
	if r.Message != "" {
		return r.Message
	}
	return r.Msg
}

// UserInfo holds basic user information (/account/info).
type UserInfo struct {
	// Nickname is the user's display name.
	Nickname string `json:"nickname"`
	// Avatar is the avatar URL.
	Avatar string `json:"avatar"`
	// Account is the account identifier.
	Account string `json:"account"`
	// MemberID is the user ID.
	MemberID string `json:"member_id"`
}

// MemberInfo holds membership and capacity information (/1/clouddrive/member).
type MemberInfo struct {
	// UseCapacity is the used capacity in bytes.
	UseCapacity int64 `json:"use_capacity"`
	// TotalCapacity is the total capacity in bytes.
	TotalCapacity int64 `json:"total_capacity"`
	// MemberType is the membership type (a string such as "EXP_SVIP"/"NORMAL",
	// not an int).
	MemberType string `json:"member_type"`
	// ExpSvipExpAt is the super-VIP expiry timestamp in milliseconds.
	ExpSvipExpAt int64 `json:"exp_svip_exp_at"`
	// SuperVipExpAt is a compatible field returned by some accounts.
	SuperVipExpAt int64 `json:"super_vip_exp_at"`
}

// FileItem represents a Quark pan file or directory entry.
type FileItem struct {
	// Fid is the file ID.
	Fid string `json:"fid"`
	// Name is the file name.
	Name string `json:"file_name"`
	// Size is the file size in bytes.
	Size int64 `json:"size"`
	// Dir reports whether the entry is a directory (true = directory).
	Dir bool `json:"dir"`
	// File reports whether the entry is a file (inverse of Dir; some endpoints
	// return only one of them).
	File bool `json:"file"`
	// CreatedAt is the creation timestamp in milliseconds.
	CreatedAt int64 `json:"created_at"`
	// UpdatedAt is the modification timestamp in milliseconds.
	UpdatedAt int64 `json:"updated_at"`
	// LCreatedAt is the local creation timestamp in milliseconds (some endpoints).
	LCreatedAt int64 `json:"l_created_at"`
	// LUpdatedAt is the local modification timestamp in milliseconds.
	LUpdatedAt int64 `json:"l_updated_at"`
	// ShareFidToken is the share file token (returned by share/sharepage/detail,
	// needed when saving a share).
	ShareFidToken string `json:"share_fid_token"`
}

// IsDirectory reports whether the entry is a directory.
//
// Endpoints return inconsistent fields: list returns dir+file, while share
// returns file or only dir. Priority: dir==true -> directory; file==true ->
// file; neither set -> fall back to size==0 (unreliable, rarely reached).
func (f *FileItem) IsDirectory() bool {
	if f.Dir {
		return true
	}
	if f.File {
		return false
	}
	// Neither field set: fall back to size==0 with a non-empty fid (empty
	// files would be misclassified, but this branch is rarely reached).
	return f.Fid != "" && f.Size == 0
}
