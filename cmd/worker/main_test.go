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

func TestParseArgs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{name: "named queue", args: []string{"--queue=uploads"}, want: "uploads"},
		{name: "arbitrary queue", args: []string{"--queue", "custom"}, want: "custom"},
		{name: "trim queue", args: []string{"--queue= uploads "}, want: "uploads"},
		{name: "smoke queue", args: []string{"--queue=smoke"}, want: "smoke"},
		{name: "removed smoke flag", args: []string{"--queue=smoke", "--smoke"}, wantErr: true},
		{name: "missing queue", wantErr: true},
		{name: "smoke missing queue", args: []string{"--smoke"}, wantErr: true},
		{name: "empty queue", args: []string{"--queue="}, wantErr: true},
		{name: "blank queue", args: []string{"--queue= "}, wantErr: true},
		{name: "missing value", args: []string{"--queue"}, wantErr: true},
		{name: "unknown flag", args: []string{"--queue=uploads", "--unknown"}, wantErr: true},
		{name: "positional command", args: []string{"smoke", "--queue=uploads"}, wantErr: true},
		{name: "extra argument", args: []string{"--queue=uploads", "extra"}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseArgs(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExecuteStartupPanic(t *testing.T) {
	config.SetProvider(nil)
	t.Cleanup(func() { config.SetProvider(new(config.EnvProvider)) })
	err := execute(t.Context(), []string{"--queue=smoke"})
	require.Error(t, err)
	var stack errors.StackTracer
	require.ErrorAs(t, err, &stack)
	require.NotEmpty(t, stack.StackTrace())
}

func TestWorkerRejectsUnregisteredQueues(t *testing.T) {
	t.Parallel()
	for _, queue := range []string{"uploads", "custom"} {
		t.Run(queue, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, runWorker(t.Context(), queue), "no workflows registered")
		})
	}
}
