package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/phpgao/diskcli/internal/provider"
	"github.com/spf13/cobra"
)

// stateKey is the context key for storing rootState in cobra commands.
type stateKey struct{}

// logxLogger is a thin wrapper around *slog.Logger providing a uniform interface.
//
// Wrapping instead of storing *slog.Logger directly allows the cli package to
// expose simple Debug/Info/Warn/Error sugar while keeping the option to extend
// later.
type logxLogger struct {
	inner *slog.Logger
}

func (l *logxLogger) Debug(msg string, args ...any) { l.inner.Debug(msg, args...) }
func (l *logxLogger) Info(msg string, args ...any)  { l.inner.Info(msg, args...) }
func (l *logxLogger) Warn(msg string, args ...any)  { l.inner.Warn(msg, args...) }
func (l *logxLogger) Error(msg string, args ...any) { l.inner.Error(msg, args...) }

// setState stores rootState in the cobra command's context.
func setState(cmd *cobra.Command, st *rootState) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cmd.SetContext(context.WithValue(ctx, stateKey{}, st))
}

// fromCmd retrieves rootState from the cobra command's context.
func fromCmd(cmd *cobra.Command) *rootState {
	if cmd.Context() == nil {
		return nil
	}
	v, _ := cmd.Context().Value(stateKey{}).(*rootState)
	return v
}

// mustProvider retrieves the provider from rootState; returns an error if
// provider creation failed. Subcommands call this in their RunE.
func mustProvider(cmd *cobra.Command) (provider.Provider, error) {
	st := fromCmd(cmd)
	if st == nil {
		return nil, fmt.Errorf("internal error: state not initialized")
	}
	if st.provErr != nil {
		return nil, st.provErr
	}
	if st.prov == nil {
		return nil, fmt.Errorf("provider not initialized")
	}
	return st.prov, nil
}
