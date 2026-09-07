package main

import (
	"os"
	"testing"
	"time"

	"nautilus/internal/config"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestWaitForShutdown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		complete bool
		failure  error
		signal   os.Signal
		timeout  time.Duration
		want     string
	}{
		{name: "graceful completion", complete: true, timeout: time.Second},
		{name: "worker error", complete: true, failure: errors.New("worker failed"), timeout: time.Second},
		{name: "second signal", signal: os.Interrupt, timeout: time.Second, want: "forced worker shutdown"},
		{name: "timeout", want: "worker shutdown timed out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			done := make(chan error, 1)
			if tt.complete {
				done <- tt.failure
			}
			signals := make(chan os.Signal, 1)
			if tt.signal != nil {
				signals <- tt.signal
			}
			err := waitForShutdown(done, signals, tt.timeout)
			switch {
			case tt.failure != nil:
				require.ErrorIs(t, err, tt.failure)
			case tt.want != "":
				require.ErrorContains(t, err, tt.want)
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestExecuteInvalidArguments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "unknown command", args: []string{"unknown"}},
		{name: "extra smoke argument", args: []string{"smoke", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, execute(t.Context(), tt.args), "usage: worker [smoke]")
		})
	}
}

func TestExecuteStartupPanic(t *testing.T) {
	config.SetProvider(nil)
	t.Cleanup(func() { config.SetProvider(new(config.EnvProvider)) })
	err := execute(t.Context(), nil)
	require.Error(t, err)
	var stack errors.StackTracer
	require.ErrorAs(t, err, &stack)
	require.NotEmpty(t, stack.StackTrace())
}
