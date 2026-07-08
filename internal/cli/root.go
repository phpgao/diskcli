// Package cli implements the diskcli cobra command layer.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/phpgao/diskcli/internal/config"
	"github.com/phpgao/diskcli/internal/provider"
	"github.com/phpgao/diskcli/pkg/logx"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// BuildInfo holds version metadata injected at build time via -ldflags
// (e.g. -X main.version / -X main.commit / -X main.date).
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// versionInfo is the machine-readable shape of `diskcli version -o json|yaml`.
type versionInfo struct {
	Version   string `json:"version" yaml:"version"`
	Commit    string `json:"commit" yaml:"commit"`
	Date      string `json:"date" yaml:"date"`
	GoVersion string `json:"go" yaml:"go"`
}

// formatVersion renders the human-readable version string (used by both
// `diskcli --version` and the default `diskcli version` output).
func formatVersion(version, commit, date string) string {
	if commit == "" {
		commit = "unknown"
	}
	if date == "" {
		date = "unknown"
	}
	return fmt.Sprintf("diskcli %s\ncommit: %s\ndate:   %s\ngo:     %s",
		version, commit, date, runtime.Version())
}

// globalFlags holds root command global flag state.
type globalFlags struct {
	verbose     bool
	json        bool
	qkCookie    string
	qkUserAgent string
	qkReferer   string
	qkOrigin    string
	configPath  string
	provider    string
}

// rootState is the runtime state visible to subcommands.
type rootState struct {
	cfg           *config.Config
	logger        *logxLogger
	stdout        io.Writer
	stderr        io.Writer
	prov          provider.Provider
	provErr       error // provider creation error (lazy; surfaced when a command needs it)
	provName      string
	customHeaders map[string]string
}

// providerName returns the provider name (defaults to quark).
func (s *rootState) providerName() string {
	if s.provName == "" {
		return "quark"
	}
	return s.provName
}

// NewRootCmd builds the diskcli root cobra command.
func NewRootCmd(bi BuildInfo) *cobra.Command {
	var g globalFlags

	version := bi.Version
	if version == "" {
		version = "dev"
	}
	verStr := formatVersion(version, bi.Commit, bi.Date)

	root := &cobra.Command{
		Use:     "diskcli",
		Short:   "Multi-cloud-disk command-line tool",
		Version: verStr,
		Long: `diskcli is a multi-cloud-disk CLI. Currently only Quark pan is supported;
baidu/aliyun/onedrive are planned.
`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Load config and logger in PersistentPreRunE; subcommands read via state.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(config.LoadOptions{
				QuarkCookieFlag: g.qkCookie,
				ConfigPath:      g.configPath,
			})
			if err != nil {
				return err
			}
			logger := logx.New(logx.Options{
				Verbose: g.verbose,
				JSON:    g.json,
				Writer:  os.Stderr,
			})
			st := &rootState{
				cfg:           cfg,
				logger:        &logxLogger{inner: logger},
				stdout:        os.Stdout,
				stderr:        os.Stderr,
				provName:      g.provider,
				customHeaders: buildCustomHeaders(g.qkUserAgent, g.qkReferer, g.qkOrigin, cfg),
			}
			// Lazy-load provider: a missing cookie is not fatal here (error surfaces
			// when a command actually needs the provider).
			st.prov, st.provErr = newProvider(st.providerName(), cfg, st.customHeaders)
			setState(cmd, st)
			logger.Debug("config loaded",
				"provider", st.providerName(),
				"cookie_source", cfg.CookieSource,
				"config_path", cfg.ConfigPath,
				"upload_threads", cfg.UploadThreads,
			)
			return nil
		},
		// Show help when no subcommand is given (RunE triggers PersistentPreRunE so -v works).
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	// cobra's default `--version` template prints only `.Version`; render our
	// multi-line string verbatim instead.
	root.SetVersionTemplate("{{.Version}}\n")

	root.PersistentFlags().BoolVarP(&g.verbose, "verbose", "v", false, "enable debug logging")
	root.PersistentFlags().BoolVar(&g.json, "log-json", false, "emit logs as JSON")
	root.PersistentFlags().StringVar(&g.qkCookie, "qk-cookie", "", "Quark cookie string")
	root.PersistentFlags().StringVar(&g.configPath, "config", "", "config file path (default ~/.diskcli/config)")
	root.PersistentFlags().StringVar(&g.provider, "provider", "quark", "cloud-disk provider (only quark is supported currently)")
	root.PersistentFlags().StringVar(&g.qkUserAgent, "qk-user-agent", "", "custom User-Agent (overrides default Chrome UA)")
	root.PersistentFlags().StringVar(&g.qkReferer, "qk-referer", "", "custom Referer header")
	root.PersistentFlags().StringVar(&g.qkOrigin, "qk-origin", "", "custom Origin header")

	// Register subcommands.
	root.AddCommand(newInfoCmd())
	root.AddCommand(newLsCmd())
	root.AddCommand(newTreeCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newMkdirCmd())
	root.AddCommand(newMvCmd())
	root.AddCommand(newCpCmd())
	root.AddCommand(newRmCmd())
	root.AddCommand(newRenameCmd())
	root.AddCommand(newBulkRenameCmd())
	root.AddCommand(newUploadCmd())
	root.AddCommand(newDownloadCmd())
	root.AddCommand(newShareCmd())
	root.AddCommand(newUnarchiveCmd())
	root.AddCommand(newVersionCmd(bi))

	return root
}

// newVersionCmd prints build metadata, optionally as JSON/YAML via -o.
func newVersionCmd(bi BuildInfo) *cobra.Command {
	var output string
	v := bi.Version
	if v == "" {
		v = "dev"
	}
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := versionInfo{
				Version:   v,
				Commit:    bi.Commit,
				Date:      bi.Date,
				GoVersion: runtime.Version(),
			}
			out := cmd.OutOrStdout()
			switch output {
			case "json":
				b, err := json.MarshalIndent(info, "", "  ")
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintln(out, string(b))
			case "yaml":
				b, err := yaml.Marshal(info)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprint(out, string(b))
			default:
				_, _ = fmt.Fprintln(out, formatVersion(v, bi.Commit, bi.Date))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text|json|yaml")
	return cmd
}

// buildCustomHeaders builds a header-override map. Priority (high → low):
// CLI flag → env/QK_USER_AGENT etc. → config file → quark defaults.
func buildCustomHeaders(flagUA, flagRef, flagOrigin string, cfg *config.Config) map[string]string {
	m := make(map[string]string)
	// Each key: CLI flag wins, then env/config value from cfg, else empty (falling to default)
	ua := pickFirst(flagUA, cfg.QuarkUserAgent)
	ref := pickFirst(flagRef, cfg.QuarkReferer)
	origin := pickFirst(flagOrigin, cfg.QuarkOrigin)
	if ua != "" {
		m["User-Agent"] = ua
	}
	if ref != "" {
		m["Referer"] = ref
	}
	if origin != "" {
		m["Origin"] = origin
	}
	return m
}

// pickFirst returns the first non-empty argument.
func pickFirst(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
