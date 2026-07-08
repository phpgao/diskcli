package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/phpgao/diskcli/internal/provider"
	"github.com/spf13/cobra"
)

// newLsCmd creates the ls subcommand.
//
// Supports -o, --output json|yaml|table|wide (kubectl style).
func newLsCmd() *cobra.Command {
	var (
		long     bool
		pageSize int
		all      bool
		output   string
	)
	cmd := &cobra.Command{
		Use:   "ls [path]",
		Short: "List directory contents",
		Long: `List files/directories under the given path. Defaults to root /.

Output formats (-o, --output):
  table  default, names only (directories suffixed with /)
  wide   long table (type/size/time/fid/name)
  json   JSON format (pipe-friendly)
  yaml   YAML format`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			p := "/"
			if len(args) > 0 {
				p = args[0]
			}
			res, err := prov.List(cmd.Context(), p, provider.ListOpts{PageSize: pageSize, All: all})
			if err != nil {
				return err
			}
			format, err := parseOutput(output)
			if err != nil {
				return err
			}
			if isEncodedFormat(format) {
				return printEncoded(cmd.OutOrStdout(), res.Items, format)
			}
			if long || format == OutputWide {
				printListLong(cmd, res.Items, p)
			} else {
				printListShort(cmd, res.Items)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&long, "long", "l", false, "show details (equivalent to -o wide)")
	cmd.Flags().IntVar(&pageSize, "page-size", 50, "page size")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include hidden files")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "output format: table|wide|json|yaml")
	return cmd
}

// printListShort prints a short list (names only).
func printListShort(cmd *cobra.Command, items []*provider.FileItem) {
	for _, it := range items {
		name := it.Name
		if it.IsDirectory {
			name += "/"
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), name)
	}
}

// printListLong prints a long-format table (size/mod time/fid/name).
func printListLong(cmd *cobra.Command, items []*provider.FileItem, _ string) {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "Type\tSize\tModTime\tFID\tName\n")
	for _, it := range items {
		typ := "file"
		if it.IsDirectory {
			typ = "dir"
		}
		size := formatBytes(it.Size)
		if it.IsDirectory {
			size = "-"
		}
		mt := "-"
		if !it.ModTime.IsZero() {
			mt = it.ModTime.Format("2006-01-02 15:04:05")
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", typ, size, mt, it.ProviderInternalID, it.Name)
	}
	_ = w.Flush()
}
