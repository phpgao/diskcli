package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phpgao/diskcli/internal/provider"
	"github.com/spf13/cobra"
)

// newUploadCmd creates the upload subcommand.
func newUploadCmd() *cobra.Command {
	var (
		threads     int
		fileThreads int
		onConflict  string
		yes         bool
		resume      bool
	)
	cmd := &cobra.Command{
		Use:   "upload <local> <remote>",
		Short: "Upload files/folders",
		Long: `Upload local files or folders to the cloud disk.

Examples:
  diskcli upload ./file.txt /backup/file.txt
  diskcli upload ./folder /backup/folder --threads 8
  diskcli upload ./file.txt /backup/ --on-conflict overwrite -y`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			local := args[0]
			remote := args[1]
			st := fromCmd(cmd)

			strategy, err := parseConflictStrategy(onConflict)
			if err != nil {
				return err
			}
			opts := provider.UploadOpts{
				Threads:     threads,
				FileThreads: fileThreads,
				OnConflict:  strategy,
				Yes:         yes,
				Resume:      resume,
			}

			info, err := os.Stat(local)
			if err != nil {
				return fmt.Errorf("invalid local path: %w", err)
			}

			if info.IsDir() {
				return uploadFolder(cmd.Context(), prov, local, remote, opts, st)
			}
			return uploadSingleFile(cmd.Context(), prov, local, remote, opts, st)
		},
	}
	cmd.Flags().IntVar(&threads, "threads", 0, "chunk concurrency (0=config or server-suggested)")
	cmd.Flags().IntVar(&fileThreads, "file-threads", 1, "per-file concurrency inside a folder (default 1, avoid rate limit)")
	cmd.Flags().StringVar(&onConflict, "on-conflict", "skip", "name conflict policy: skip|overwrite|rename")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation before overwrite")
	cmd.Flags().BoolVar(&resume, "resume", false, "enable resumable upload")
	return cmd
}

// uploadSingleFile uploads a single file.
func uploadSingleFile(ctx context.Context, prov provider.Provider, local, remote string, opts provider.UploadOpts, st *rootState) error {
	if strings.HasSuffix(remote, "/") {
		remote = strings.TrimSuffix(remote, "/") + "/" + filepath.Base(local)
	}
	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("open local file failed: %w", err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	src := provider.UploadSource{
		Path:    local,
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
	}
	if st != nil {
		st.logger.Info("uploading", "local", local, "remote", remote, "size", formatBytes(fi.Size()))
	}
	res, err := prov.Upload(ctx, remote, src, opts)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "upload ok: %s -> %s (%s)\n", local, res.Path, formatBytes(res.Size))
	return nil
}

// uploadFolder uploads a folder recursively.
//
// Simplified: sequential per-file upload (--file-threads left for transfer layer).
func uploadFolder(ctx context.Context, prov provider.Provider, local, remote string, opts provider.UploadOpts, st *rootState) error {
	if st != nil {
		st.logger.Info("uploading folder", "local", local, "remote", remote)
	}
	count := 0
	err := filepath.Walk(local, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil // dirs are created implicitly by file upload
		}
		rel, _ := filepath.Rel(local, path)
		rel = filepath.ToSlash(rel)
		dstPath := strings.TrimSuffix(remote, "/") + "/" + rel
		// ensure parent directories exist remotely (ignore "already exists" errors)
		if err := ensureRemoteDir(ctx, prov, filepath.Dir(dstPath)); err != nil {
			if st != nil {
				st.logger.Warn("create remote dir failed (may already exist)", "dir", filepath.Dir(dstPath), "err", err)
			}
		}
		src := provider.UploadSource{Path: path, Size: info.Size(), ModTime: info.ModTime()}
		if st != nil {
			st.logger.Debug("uploading file", "path", path, "dst", dstPath)
		}
		if _, err := prov.Upload(ctx, dstPath, src, opts); err != nil {
			return fmt.Errorf("upload %s failed: %w", path, err)
		}
		count++
		_, _ = fmt.Fprintf(os.Stdout, "uploaded: %s -> %s\n", path, dstPath)
		return nil
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "folder upload complete: %d files\n", count)
	return nil
}

// ensureRemoteDir ensures remote directories exist (creates each segment; ignores "already exists").
func ensureRemoteDir(ctx context.Context, prov provider.Provider, dir string) error {
	if dir == "" || dir == "/" || dir == "." {
		return nil
	}
	parts := strings.Split(strings.Trim(dir, "/"), "/")
	current := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		current = current + "/" + p
		if err := prov.Mkdir(ctx, current); err != nil {
			// "already exists" errors are expected; warn on stderr.
			fmt.Fprintf(os.Stderr, "warn: mkdir %s: %v\n", current, err)
		}
		// Brief delay so the cloud listing can observe the new directory.
		time.Sleep(800 * time.Millisecond)
	}
	return nil
}

// parseConflictStrategy parses the on-conflict flag value.
func parseConflictStrategy(s string) (provider.ConflictStrategy, error) {
	switch s {
	case "skip", "":
		return provider.ConflictSkip, nil
	case "overwrite":
		return provider.ConflictOverwrite, nil
	case "rename":
		return provider.ConflictRename, nil
	default:
		return 0, fmt.Errorf("invalid conflict policy: %s (choose skip/overwrite/rename)", s)
	}
}
