package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/phpgao/diskcli/internal/provider"
	"github.com/spf13/cobra"
)

// newDownloadCmd creates the download subcommand.
func newDownloadCmd() *cobra.Command {
	var (
		onConflict string
		yes        bool
		resume     bool
		threads    int
	)
	cmd := &cobra.Command{
		Use:   "download <remote> <local>",
		Short: "Download files/folders",
		Long: `Download files or folders from the cloud disk to the local filesystem.

Examples:
  diskcli download /remote/file.txt ./file.txt
  diskcli download /remote/folder ./folder --resume
  diskcli download /remote/file.txt ./ --on-conflict overwrite -y`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			remote := args[0]
			local := args[1]
			st := fromCmd(cmd)

			strategy, err := parseConflictStrategy(onConflict)
			if err != nil {
				return err
			}
			opts := provider.DownloadOpts{
				OnConflict: strategy,
				Yes:        yes,
				Resume:     resume,
				Threads:    threads,
			}

			item, err := prov.Stat(cmd.Context(), remote)
			if err != nil {
				return err
			}
			if item.IsDirectory {
				return downloadFolder(cmd.Context(), prov, remote, local, opts, st)
			}
			return downloadSingleFile(cmd.Context(), prov, remote, local, opts, st)
		},
	}
	cmd.Flags().StringVar(&onConflict, "on-conflict", "skip", "name conflict policy: skip|overwrite|rename")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation before overwrite")
	cmd.Flags().BoolVar(&resume, "resume", false, "resumable download (Range)")
	cmd.Flags().IntVar(&threads, "threads", 1, "concurrent download threads")
	return cmd
}

// downloadSingleFile downloads a single file.
func downloadSingleFile(ctx context.Context, prov provider.Provider, remote, local string, opts provider.DownloadOpts, st *rootState) error {
	// if local is a directory, append the remote filename
	if info, err := os.Stat(local); err == nil && info.IsDir() {
		name := filepath.Base(remote)
		local = filepath.Join(local, name)
	}
	// conflict check
	if _, err := os.Stat(local); err == nil {
		switch opts.OnConflict {
		case provider.ConflictSkip:
			_, _ = fmt.Fprintf(os.Stdout, "local exists, skip: %s\n", local)
			return nil
		case provider.ConflictOverwrite:
			if !opts.Yes {
				if !confirmPrompt(fmt.Sprintf("overwrite %s? (y/N) ", local)) {
					_, _ = fmt.Fprintln(os.Stdout, "cancelled")
					return nil
				}
			}
			_ = os.Remove(local)
		case provider.ConflictRename:
			local = renameLocal(local)
		}
	}
	if st != nil {
		st.logger.Info("downloading", "remote", remote, "local", local)
	}
	f, err := os.Create(local)
	if err != nil {
		return fmt.Errorf("create local file failed: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := prov.Download(ctx, remote, f, opts); err != nil {
		_ = os.Remove(local)
		return err
	}
	fi, _ := f.Stat()
	_, _ = fmt.Fprintf(os.Stdout, "download ok: %s -> %s (%s)\n", remote, local, formatBytes(fi.Size()))
	return nil
}

// downloadFolder downloads a folder recursively.
func downloadFolder(ctx context.Context, prov provider.Provider, remote, local string, opts provider.DownloadOpts, st *rootState) error {
	if st != nil {
		st.logger.Info("downloading folder", "remote", remote, "local", local)
	}
	if err := os.MkdirAll(local, 0o755); err != nil {
		return fmt.Errorf("create local directory failed: %w", err)
	}
	res, err := prov.List(ctx, remote, provider.ListOpts{PageSize: 50})
	if err != nil {
		return err
	}
	count := 0
	for _, item := range res.Items {
		remotePath := strings.TrimSuffix(remote, "/") + "/" + item.Name
		localPath := filepath.Join(local, item.Name)
		if item.IsDirectory {
			if err := downloadFolder(ctx, prov, remotePath, localPath, opts, st); err != nil {
				return err
			}
			continue
		}
		if err := downloadSingleFile(ctx, prov, remotePath, localPath, opts, st); err != nil {
			return err
		}
		count++
	}
	_, _ = fmt.Fprintf(os.Stdout, "folder download complete: %d files\n", count)
	return nil
}

// renameLocal generates a non-conflicting local filename (suffix _1 _2 ...).
func renameLocal(path string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	for i := 1; ; i++ {
		newName := fmt.Sprintf("%s_%d%s", base, i, ext)
		newPath := filepath.Join(dir, newName)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}
}

// confirmPrompt is a simple y/N prompt reading from stdin.
func confirmPrompt(msg string) bool {
	_, _ = fmt.Fprint(os.Stdout, msg)
	var resp string
	_, _ = fmt.Fscanln(os.Stdin, &resp)
	resp = strings.ToLower(strings.TrimSpace(resp))
	return resp == "y" || resp == "yes"
}
