package quark

import (
	"errors"
	"fmt"
)

// Protocol-layer sentinel errors. Identify with errors.Is to avoid string matching.
var (
	// ErrNoCookie means no cookie was provided.
	ErrNoCookie = errors.New("cookie not provided")
	// ErrUnauthorized means authentication failed (cookie expired or invalid).
	ErrUnauthorized = errors.New("authentication failed")
	// ErrNotFound means the file or directory does not exist.
	ErrNotFound = errors.New("resource not found")
	// ErrAlreadyExists means a resource with the same name already exists.
	ErrAlreadyExists = errors.New("resource already exists")
	// ErrRateLimited means the request was rate-limited by risk control.
	ErrRateLimited = errors.New("rate limited")
	// ErrAPICall means a Quark API call returned a non-success status.
	ErrAPICall = errors.New("API call failed")
	// ErrTaskFailed means an async task failed.
	ErrTaskFailed = errors.New("async task failed")
)

// APIError carries the Quark API error code and message so callers can
// inspect the specific failure.
type APIError struct {
	// Code is the Quark error code (0 means success).
	Code FlexInt
	// Message is the error message.
	Message string
	// Errno is the error number (some APIs use errno/errmsg instead of
	// code/message).
	Errno FlexInt
	// Errmsg is the error message paired with errno.
	Errmsg string
}

func (e *APIError) Error() string {
	if !e.Errno.IsZero() && e.Errmsg != "" {
		return fmt.Sprintf("API error: errno=%d errmsg=%s", e.Errno, e.Errmsg)
	}
	return fmt.Sprintf("API error: code=%d message=%s", e.Code, e.Message)
}

func (e *APIError) Unwrap() error { return ErrAPICall }
