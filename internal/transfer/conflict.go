// Package transfer implements the orchestration layer (folder traversal,
// conflict policy, concurrency control).
//
// It depends only on the provider.Provider interface, not on any concrete
// protocol, so it can be unit-tested with a mock provider.
package transfer

import (
	"context"
	"fmt"
	"strings"

	"github.com/phpgao/diskcli/internal/provider"
)

// ResolveConflict applies the conflict policy on the remote side and returns
// the final usable path.
//
// Callers invoke this before upload/download to check whether a same-named
// file exists on the remote.
//   - ConflictSkip: if remote exists, returns ("", true) meaning "skip"
//   - ConflictOverwrite: if remote exists, deletes it and returns the original path
//   - ConflictRename: if remote exists, generates a new name (suffix _1 _2 ...) and returns it
//
// When the remote does not exist, the original path is returned unchanged.
func ResolveConflict(ctx context.Context, prov provider.Provider, remotePath string, strategy provider.ConflictStrategy) (finalPath string, skipped bool, err error) {
	_, statErr := prov.Stat(ctx, remotePath)
	if statErr != nil {
		// not found; no conflict to resolve
		return remotePath, false, nil
	}
	switch strategy {
	case provider.ConflictSkip:
		return "", true, nil
	case provider.ConflictOverwrite:
		if err := prov.Delete(ctx, []string{remotePath}); err != nil {
			return "", false, fmt.Errorf("overwrite delete failed: %w", err)
		}
		return remotePath, false, nil
	case provider.ConflictRename:
		newPath, err := findRenameTarget(ctx, prov, remotePath)
		if err != nil {
			return "", false, err
		}
		return newPath, false, nil
	default:
		return remotePath, false, nil
	}
}

// findRenameTarget generates a non-conflicting remote path (suffix _1 _2 ...).
func findRenameTarget(ctx context.Context, prov provider.Provider, remotePath string) (string, error) {
	dir := pathDir(remotePath)
	name := pathBase(remotePath)
	dot := strings.LastIndex(name, ".")
	var base, ext string
	if dot > 0 {
		base, ext = name[:dot], name[dot:]
	} else {
		base = name
	}
	for i := 1; ; i++ {
		newName := fmt.Sprintf("%s_%d%s", base, i, ext)
		newPath := joinPath(dir, newName)
		if _, err := prov.Stat(ctx, newPath); err != nil {
			// not found; usable
			return newPath, nil
		}
		if i > 1000 {
			return "", fmt.Errorf("rename tried over 1000 times still conflicting: %s", remotePath)
		}
	}
}

// pathDir/pathBase/joinPath use POSIX paths (cloud-disk paths use /).
func pathDir(p string) string  { return pathDirImpl(p) }
func pathBase(p string) string { return pathBaseImpl(p) }
func joinPath(dir, name string) string {
	if dir == "/" || dir == "" {
		return "/" + name
	}
	return dir + "/" + name
}

// pathDirImpl/pathBaseImpl use the stdlib path package.
func pathDirImpl(p string) string {
	p = strings.TrimSuffix(p, "/")
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return "/"
	}
	if idx == 0 {
		return "/"
	}
	return p[:idx]
}

func pathBaseImpl(p string) string {
	p = strings.TrimSuffix(p, "/")
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return p
	}
	return p[idx+1:]
}
