package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/phpgao/diskcli/internal/provider"
	"github.com/spf13/cobra"
)

// newBulkRenameCmd creates the bulk-rename subcommand.
//
// Usage:
//
//	diskcli brename <dir> --match "old" --to "new"          # literal replace
//	diskcli brename <dir> --match "(.+)\.jpeg" --to "$1.jpg" --regex  # regex
//	diskcli brename <dir> --upper                            # uppercase
//	diskcli brename <dir> --lower                            # lowercase
//	diskcli brename <dir> --match "old" --to "new" --dry-run # preview only
//	diskcli brename <dir> --match "old" --to "new" --undo    # undoable
func newBulkRenameCmd() *cobra.Command {
	var (
		match    string
		to       string
		useRegex bool
		upper    bool
		lower    bool
		dryRun   bool
		undo     bool
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "brename <dir> --match <pattern> --to <replacement>",
		Short: "Batch rename files in a directory",
		Long: `Batch rename all files in a directory by applying a pattern rule.

Modes (pick one):
  --match + --to        literal string replace (default)
  --match + --to --regex  regex replace with capture groups
  --upper               convert names to UPPERCASE
  --lower               convert names to lowercase

Examples:
  diskcli brename /dir --match ".jpeg" --to ".jpg"
  diskcli brename /dir --match "photo_(.+)" --to "img_$1" --regex  # bash: use single quotes
  diskcli brename /dir --match "photo_(.+)" --to "img_\$1" --regex  # bash: escape $
  diskcli brename /dir --lower
  diskcli brename /dir --match "old" --to "new" --dry-run   # preview only

Flags:
  --dry-run             preview changes without executing
  --undo                record changes for confirmation/rollback
  --yes / -y            skip all confirmation prompts

Note: When using --regex with capture groups ($1, $2), wrap the --to value
in single quotes ('$1') or escape the dollar sign (\$1) to prevent shell
expansion.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			dir := args[0]

			// List files in the directory (top-level only, non-recursive).
			res, err := prov.List(cmd.Context(), dir, provider.ListOpts{PageSize: 200})
			if err != nil {
				return err
			}

			// Build the rename table: oldName -> newName.
			type renamePair struct {
				oldPath string
				newPath string
				oldName string
				newName string
			}
			var pairs []renamePair
			var re *regexp.Regexp
			if useRegex {
				re, err = regexp.Compile(match)
				if err != nil {
					return fmt.Errorf("invalid regex %q: %w", match, err)
				}
			}

			for _, item := range res.Items {
				if item.IsDirectory {
					continue
				}
				var newName string
				switch {
				case upper:
					newName = strings.ToUpper(item.Name)
				case lower:
					newName = strings.ToLower(item.Name)
				case useRegex:
					if re.MatchString(item.Name) {
						newName = re.ReplaceAllString(item.Name, to)
					} else {
						continue // no match, skip
					}
				case match != "":
					newName = strings.ReplaceAll(item.Name, match, to)
				default:
					return fmt.Errorf("requires --match+--to, --upper, or --lower")
				}
				if newName == item.Name {
					continue // unchanged, skip
				}
				base := strings.TrimSuffix(dir, "/")
				pairs = append(pairs, renamePair{
					oldPath: base + "/" + item.Name,
					newPath: base + "/" + newName,
					oldName: item.Name,
					newName: newName,
				})
			}

			if len(pairs) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no files to rename")
				return nil
			}

			// Dry-run: show the plan and exit.
			if dryRun {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "would rename %d files:\n", len(pairs))
				for _, p := range pairs {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s  ->  %s\n", p.oldName, p.newName)
				}
				return nil
			}

			// Show the plan.
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "will rename %d files:\n", len(pairs))
			for _, p := range pairs {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s  ->  %s\n", p.oldName, p.newName)
			}
			// Undo mode skips the "proceed?" prompt — the post-exec confirmation covers it.
			if !undo && !yes {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), "proceed? (y/N) ")
				var resp string
				_, _ = fmt.Fscanln(cmd.InOrStdin(), &resp)
				if strings.ToLower(strings.TrimSpace(resp)) != "y" {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "aborted")
					return nil
				}
			}

			// Collect undo entries.
			var undoEntries []renameEntry

			// Execute renames.
			errCount := 0
			for _, p := range pairs {
				if undo {
					entry := renameEntry{
						OldPath: p.oldPath,
						NewPath: p.newPath,
					}
					if f, e := saveRenameState(entry); e == nil {
						undoEntries = append(undoEntries, entry)
						_ = f // keep file for undo
					}
				}
				if err := prov.Rename(cmd.Context(), p.oldPath, filepath.Base(p.newPath)); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "FAIL: %s -> %s: %v\n", p.oldName, p.newName, err)
					errCount++
					continue
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s  ->  %s\n", p.oldName, p.newName)
			}

			if errCount > 0 {
				return fmt.Errorf("%d/%d renames failed", errCount, len(pairs))
			}

			// Undo mode: prompt to confirm.
			if undo && len(undoEntries) > 0 {
				if !yes {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nrenames recorded (%d files). confirm? (y=keep, n=rollback) ", len(undoEntries))
					var resp string
					_, _ = fmt.Fscanln(cmd.InOrStdin(), &resp)
					if strings.ToLower(strings.TrimSpace(resp)) != "y" {
						// Rollback all.
						for i := len(undoEntries) - 1; i >= 0; i-- {
							e := undoEntries[i]
							if err := prov.Rename(cmd.Context(), e.NewPath, filepath.Base(e.OldPath)); err != nil {
								_, _ = fmt.Fprintf(os.Stderr, "rollback failed for %s: %v\n", e.NewPath, err)
							}
						}
						// Clean temp files.
						for _, e := range undoEntries {
							removeRenameState(saveRenameStatePath(e.OldPath))
						}
						_, _ = fmt.Fprintln(cmd.OutOrStdout(), "all rolled back")
						return nil
					}
				}
				// Confirmed — clean temp files.
				for _, e := range undoEntries {
					removeRenameState(saveRenameStatePath(e.OldPath))
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "confirmed, temp files cleaned")
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&match, "match", "", "string or regex pattern to match")
	cmd.Flags().StringVar(&to, "to", "", "replacement (literal or regex replacement, supports $1/$2 etc.)")
	cmd.Flags().BoolVar(&useRegex, "regex", false, "treat --match as a regex")
	cmd.Flags().BoolVar(&upper, "upper", false, "convert names to UPPERCASE")
	cmd.Flags().BoolVar(&lower, "lower", false, "convert names to lowercase")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only, do not execute")
	cmd.Flags().BoolVar(&undo, "undo", false, "record changes for confirmation/rollback")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

// saveRenameStatePath returns the temp file path for undo tracking,
// using the same naming convention as the single-file rename undo.
func saveRenameStatePath(oldPath string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".diskcli", fmt.Sprintf("rename-%x.json", md5Hash(oldPath)))
}
