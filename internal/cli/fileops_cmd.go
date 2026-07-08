package cli

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// newMkdirCmd creates the mkdir subcommand.
func newMkdirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mkdir <path>",
		Short: "Create a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			if err := prov.Mkdir(cmd.Context(), args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "created: %s\n", args[0])
			return nil
		},
	}
}

// newMvCmd creates the mv subcommand.
func newMvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mv <src> <dst-dir>",
		Short: "Move a file/directory to a target directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			if err := prov.Move(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "moved: %s -> %s\n", args[0], args[1])
			return nil
		},
	}
}

// newCpCmd creates the cp subcommand.
func newCpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cp <src> <dst-dir>",
		Short: "Copy a file/directory to a target directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			if err := prov.Copy(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "copied: %s -> %s\n", args[0], args[1])
			return nil
		},
	}
}

// newRmCmd creates the rm subcommand.
func newRmCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "rm <path> [path...]",
		Short: "Delete files/directories",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			if !yes {
				msg := fmt.Sprintf("will delete %d items: %v, confirm? (y/N) ", len(args), args)
				if !confirmPrompt(msg) {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "cancelled")
					return nil
				}
			}
			if err := prov.Delete(cmd.Context(), args); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "deleted %d items\n", len(args))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return cmd
}

// newRenameCmd creates the rename subcommand.
//
// With --undo, the operation is recorded to a temp file and the user is
// prompted to confirm. Confirming (y) finalizes the rename and removes the
// temp file; rejecting (n) rolls the rename back (swaps old ↔ new) and
// removes the temp file.
func newRenameCmd() *cobra.Command {
	var undo bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "rename <path> <new-name>",
		Short: "Rename a file/directory",
		Long: `Rename a file or directory.

With --undo, the rename is recorded to a temp file (~/.diskcli/rename-<id>.json)
and you will be asked to confirm the change. Confirming finalizes it; rejecting
rolls it back.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			oldPath := args[0]
			newName := args[1]

			if !undo {
				if err := prov.Rename(cmd.Context(), oldPath, newName); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "renamed: %s -> %s\n", oldPath, newName)
				return nil
			}

			// Undo mode: perform the rename, record state, prompt, confirm.
			if err := prov.Rename(cmd.Context(), oldPath, newName); err != nil {
				return err
			}

			// Derive the new full path.
			dir := filepath.Dir(oldPath)
			newPath := filepath.Join(dir, newName)
			if dir == "/" || dir == "." {
				newPath = "/" + newName
			}

			// Record state to temp file.
			entry := renameEntry{OldPath: oldPath, NewPath: newPath}
			stateFile, err := saveRenameState(entry)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "warning: failed to record rename state: %v\n", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "renamed: %s -> %s\n", oldPath, newPath)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "confirm to keep? (y=keep, n=rollback) ")

			if yes {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "y")
				removeRenameState(stateFile)
				return nil
			}

			var resp string
			_, _ = fmt.Fscanln(cmd.InOrStdin(), &resp)
			resp = strings.ToLower(strings.TrimSpace(resp))
			if resp == "y" || resp == "yes" {
				removeRenameState(stateFile)
				return nil
			}

			// Rollback: rename back.
			if err := prov.Rename(cmd.Context(), newPath, filepath.Base(oldPath)); err != nil {
				return fmt.Errorf("rollback failed (undo temp file kept at %s): %w", stateFile, err)
			}
			removeRenameState(stateFile)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "rolled back: %s restored\n", oldPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "record rename and prompt for confirmation")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "auto-confirm (keep without prompting)")
	return cmd
}

// renameEntry records a rename operation for undo purposes.
type renameEntry struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

// saveRenameState writes the rename entry to a temp file under ~/.diskcli/.
func saveRenameState(e renameEntry) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".diskcli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// Use a unique filename with the old-path hash.
	name := fmt.Sprintf("rename-%x.json", md5Hash(e.OldPath))
	path := filepath.Join(dir, name)
	data, _ := json.Marshal(e)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// removeRenameState deletes the rename temp file.
func removeRenameState(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// md5Hash returns the hex MD5 of a string (for unique temp filenames).
func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h[:])
}
