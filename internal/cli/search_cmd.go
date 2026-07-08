package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newSearchCmd creates the search subcommand.
//
// Examples:
//
//	diskcli search "report"
//	diskcli search "report" /
//	diskcli search "invoice" /work/2026
func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <keyword> [scope-dir]",
		Short: "Search files by name",
		Long: `Search the drive for files whose name matches the keyword.

Without a scope argument the whole drive is searched. When a directory path
is given, the search is restricted to that directory.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			keyword := args[0]
			scope := ""
			if len(args) == 2 {
				scope = args[1]
			}
			items, err := prov.Search(cmd.Context(), keyword, scope)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no results")
				return nil
			}
			printListShort(cmd, items)
			return nil
		},
	}
}
