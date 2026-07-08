package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/phpgao/diskcli/internal/provider"
	"github.com/spf13/cobra"
)

// newShareCmd creates the share parent command and its subcommands.
func newShareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "share",
		Short: "Share management",
		Long:  `Manage shared files: save, create, list, delete.`,
	}
	cmd.AddCommand(newShareSaveCmd())
	cmd.AddCommand(newShareCreateCmd())
	cmd.AddCommand(newShareListCmd())
	cmd.AddCommand(newShareDeleteCmd())
	return cmd
}

// mustSharer extracts the Sharer capability from a Provider; returns an error
// if the provider does not support sharing.
func mustSharer(prov provider.Provider) (provider.Sharer, error) {
	sh, ok := prov.(provider.Sharer)
	if !ok {
		return nil, fmt.Errorf("provider %s does not support sharing", prov.Name())
	}
	return sh, nil
}

func newShareSaveCmd() *cobra.Command {
	var passcode string
	cmd := &cobra.Command{
		Use:   "save <share-url> <dst-dir>",
		Short: "Save (transfer) a shared link to a local directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			sh, err := mustSharer(prov)
			if err != nil {
				return err
			}
			if err := sh.SaveShare(cmd.Context(), args[0], passcode, args[1]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(os.Stdout, "save ok: %s -> %s\n", args[0], args[1])
			return nil
		},
	}
	cmd.Flags().StringVar(&passcode, "passcode", "", "passcode (if not embedded in the URL)")
	return cmd
}

func newShareCreateCmd() *cobra.Command {
	var (
		passcode     string
		expire       int
		needPasscode bool
	)
	cmd := &cobra.Command{
		Use:   "create <path> [path...]",
		Short: "Create a share link",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			sh, err := mustSharer(prov)
			if err != nil {
				return err
			}
			opts := provider.ShareOpts{
				Passcode:     passcode,
				Expire:       expire,
				NeedPasscode: needPasscode,
			}
			res, err := sh.CreateShare(cmd.Context(), args, opts)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(os.Stdout, "share created:\nURL: %s\nPasscode: %s\n", res.ShareURL, res.Passcode)
			return nil
		},
	}
	cmd.Flags().StringVar(&passcode, "passcode", "", "passcode (used when --need-passcode)")
	cmd.Flags().IntVar(&expire, "expire", 1, "expiration: 1=permanent 2=1day 3=7days 4=30days")
	cmd.Flags().BoolVar(&needPasscode, "need-passcode", false, "require a passcode")
	return cmd
}

func newShareListCmd() *cobra.Command {
	var (
		page     int
		pageSize int
		output   string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List my shares",
		RunE: func(cmd *cobra.Command, _ []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			sh, err := mustSharer(prov)
			if err != nil {
				return err
			}
			items, err := sh.ListShares(cmd.Context(), provider.ListSharesOpts{Page: page, PageSize: pageSize})
			if err != nil {
				return err
			}
			format, err := parseOutput(output)
			if err != nil {
				return err
			}
			if isEncodedFormat(format) {
				return printEncoded(cmd.OutOrStdout(), items, format)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintf(w, "ShareID\tTitle\tURL\tPasscode\tExpires\n")
			for _, it := range items {
				exp := "permanent"
				// Quark encodes a permanent share as a far-future magic
				// timestamp (expired_at = 4102416000000 → year 2100), not 0.
				// Treat anything at/after 2100 as permanent in the table view.
				if !it.ExpiresAt.IsZero() && it.ExpiresAt.Year() < 2100 {
					exp = it.ExpiresAt.Format("2006-01-02 15:04:05")
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", it.ShareID, it.Title, it.ShareURL, it.Passcode, exp)
			}
			_ = w.Flush()
			return nil
		},
	}
	cmd.Flags().IntVar(&page, "page", 1, "page number (1-based)")
	cmd.Flags().IntVar(&pageSize, "page-size", 50, "page size")
	cmd.Flags().StringVarP(&output, "output", "o", "table", "output format: table|json|yaml")
	return cmd
}

func newShareDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <share-id>",
		Short: "Delete a share",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			sh, err := mustSharer(prov)
			if err != nil {
				return err
			}
			if !yes {
				if !confirmPrompt(fmt.Sprintf("delete share %s? (y/N) ", args[0])) {
					_, _ = fmt.Fprintln(os.Stdout, "cancelled")
					return nil
				}
			}
			if err := sh.DeleteShare(cmd.Context(), args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(os.Stdout, "deleted: %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return cmd
}
