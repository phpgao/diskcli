package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/phpgao/diskcli/internal/provider"
	"github.com/spf13/cobra"
)

// newInfoCmd creates the info subcommand (user info + capacity).
//
// Supports -o, --output json|yaml|table (kubectl style).
func newInfoCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show user info and capacity",
		Long: `Show the current account's nickname, member type, used/total capacity, etc.

Output formats (-o, --output):
  table  default, aligned table
  json   JSON format
  yaml   YAML format`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			st := fromCmd(cmd)
			ctx := cmd.Context()
			ui, err := prov.GetUserInfo(ctx)
			if err != nil {
				return err
			}
			if st != nil {
				st.logger.Debug("user info fetched", "provider", prov.Name())
			}
			format, err := parseOutput(output)
			if err != nil {
				return err
			}
			if isEncodedFormat(format) {
				return printEncoded(cmd.OutOrStdout(), ui, format)
			}
			printUserInfo(cmd, ui)
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "table", "output format: table|json|yaml")
	return cmd
}

// printUserInfo prints user info as an aligned table.
func printUserInfo(cmd *cobra.Command, ui *provider.UserInfo) {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	// tabwriter.Write never returns an error; fmt.Fprintf writing into tabwriter
	// does not fail either, so we swallow the return with _.
	_, _ = fmt.Fprintf(w, "Nickname\t%s\n", ui.Nickname)
	if ui.Account != "" {
		_, _ = fmt.Fprintf(w, "Account\t%s\n", ui.Account)
	}
	if ui.Avatar != "" {
		_, _ = fmt.Fprintf(w, "Avatar\t%s\n", ui.Avatar)
	}
	_, _ = fmt.Fprintf(w, "Member\t%s\n", memberTypeText(ui.MemberType))
	_, _ = fmt.Fprintf(w, "Usage\t%s / %s (%.1f%%)\n",
		formatBytes(ui.UseCapacity), formatBytes(ui.TotalCapacity), usagePercent(ui.UseCapacity, ui.TotalCapacity))
	if !ui.ExpiresAt.IsZero() {
		_, _ = fmt.Fprintf(w, "Expires\t%s\n", ui.ExpiresAt.Format("2006-01-02 15:04:05"))
	}
	_ = w.Flush()
}

// memberTypeText converts a provider member type to a human-readable string.
func memberTypeText(t string) string {
	switch t {
	case "", "NORMAL":
		return "free"
	case "EXP_SVIP", "SVIP":
		return "svip"
	case "VIP":
		return "vip"
	default:
		return fmt.Sprintf("member(%s)", t)
	}
}

// formatBytes converts a byte count to a human-readable string (B/KB/MB/GB/TB).
func formatBytes(b int64) string {
	const unit = 1024
	if b < 0 {
		return "-"
	}
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div := float64(unit)
	exp := 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := "KMGTPE"
	if exp >= len(units) {
		return fmt.Sprintf("%.2f PB", float64(b)/div)
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/div, units[exp])
}

// usagePercent returns the usage percentage.
func usagePercent(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}
