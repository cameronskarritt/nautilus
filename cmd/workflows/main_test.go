package main

import (
	"testing"

	"nautilus/internal/config"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestExecuteInvalidArguments(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing command", want: "usage:"},
		{name: "empty command", args: []string{""}, want: "unknown workflow command"},
		{name: "unknown command", args: []string{"unknown", "--queue=smoke"}, want: "unknown workflow command"},
		{name: "missing queue", args: []string{"smoke"}, want: "usage:"},
		{name: "empty queue", args: []string{"smoke", "--queue="}, want: "usage:"},
		{name: "blank queue", args: []string{"smoke", "--queue= "}, want: "usage:"},
		{name: "missing value", args: []string{"smoke", "--queue"}, want: "flag needs an argument"},
		{name: "unknown flag", args: []string{"smoke", "--queue=smoke", "--unknown"}, want: "flag provided but not defined"},
		{name: "extra argument", args: []string{"smoke", "--queue=smoke", "extra"}, want: "usage:"},
		{name: "wrong queue", args: []string{"smoke", "--queue=uploads"}, want: "smoke requires --queue=smoke"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, execute(t.Context(), tt.args), tt.want)
		})
	}
}

func TestExecuteHelp(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "global help", args: []string{"--help"}},
		{name: "short global help", args: []string{"-h"}},
		{name: "command help", args: []string{"smoke", "--help"}},
		{name: "short command help", args: []string{"smoke", "-h"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, execute(t.Context(), tt.args))
		})
	}
}

func TestSmokeRejectsQueueBeforeDial(t *testing.T) {
	config.SetProvider(nil)
	t.Cleanup(func() { config.SetProvider(new(config.EnvProvider)) })
	require.ErrorContains(t, runSmoke(t.Context(), enums.QueueUploads), "smoke requires --queue=smoke")
}

func TestExecuteStartupPanic(t *testing.T) {
	config.SetProvider(nil)
	t.Cleanup(func() { config.SetProvider(new(config.EnvProvider)) })
	err := execute(t.Context(), []string{"smoke", "--queue=smoke"})
	require.Error(t, err)
	var stack errors.StackTracer
	require.ErrorAs(t, err, &stack)
	require.NotEmpty(t, stack.StackTrace())
}
