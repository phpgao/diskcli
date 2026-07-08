// Package logx wraps slog, switching levels via the -v flag.
package logx

import (
	"io"
	"log/slog"
	"os"
)

// Options controls logger behavior.
type Options struct {
	// Verbose enables debug level.
	Verbose bool
	// JSON uses JSON output (default text).
	JSON bool
	// Writer is the output target; defaults to os.Stderr.
	Writer io.Writer
}

// New builds a *slog.Logger from the given options.
//
// Design notes:
//   - logs go to stderr, leaving stdout for data/JSON (pipe-friendly)
//   - default level is Info; Verbose=true switches to Debug
func New(opts Options) *slog.Logger {
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	level := slog.LevelInfo
	if opts.Verbose {
		level = slog.LevelDebug
	}
	var handler slog.Handler
	if opts.JSON {
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	}
	return slog.New(handler)
}
