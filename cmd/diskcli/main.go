// Package main is the entry point for the diskcli command-line tool.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/phpgao/diskcli/internal/cli"
)

// Build info injected via ldflags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if info, ok := debug.ReadBuildInfo(); ok && version == "dev" {
		version = info.Main.Version
	}
	if err := cli.NewRootCmd(cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
