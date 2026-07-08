package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newUnarchiveCmd creates the unarchive subcommand for cloud extraction.
//
// Examples:
//
//	diskcli unarchive /backup/data.zip
//	diskcli unarchive /backup/data.zip --conflict overwrite
func newUnarchiveCmd() *cobra.Command {
	var conflict string
	cmd := &cobra.Command{
		Use:   "unarchive <archive-path>",
		Short: "Extract an archive in the cloud",
		Long: `Trigger cloud extraction of an archive file (zip/rar/7z/tar/gz).

Extracted files appear in the "夸克云解压" folder at the drive root.

Conflict modes:
  skip      skip files that already exist (conflict_mode=1)
  overwrite overwrite existing files (conflict_mode=2)
  rename    auto-rename on collision (conflict_mode=3, default)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			var mode int
			switch strings.ToLower(conflict) {
			case "skip", "1":
				mode = 1
			case "overwrite", "2":
				mode = 2
			case "rename", "3", "":
				mode = 3
			default:
				return fmt.Errorf("invalid conflict mode %q (use skip/overwrite/rename)", conflict)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "extracting %s ...\n", args[0])
			if err := prov.Unarchive(cmd.Context(), args[0], mode); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "extraction complete")
			return nil
		},
	}
	cmd.Flags().StringVar(&conflict, "conflict", "rename", "conflict mode: skip/overwrite/rename")
	return cmd
}
