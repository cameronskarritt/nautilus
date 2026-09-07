package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nautilus/internal/config"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/temporal"
)

func main() {
	config.LoadDotenv()
	ctx := log.WithContext(context.Background(), log.InferLogger("worker"))
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := run(ctx, os.Args[1:], signals); err != nil {
		log.FromContext(ctx).Fatal("worker command failed", "error", err)
	}
}

func run(ctx context.Context, args []string, signals <-chan os.Signal) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- execute(ctx, args) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	case <-signals:
		cancel()
	}
	const timeout = 40 * time.Second
	logger := log.FromContext(ctx)
	logger.Info("started worker shutdown", "timeout", timeout)
	if err := waitForShutdown(done, signals, timeout); err != nil {
		return err
	}
	logger.Info("worker shut down gracefully")
	return nil
}

func waitForShutdown(done <-chan error, signals <-chan os.Signal, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-signals:
		return errors.New("forced worker shutdown")
	case <-timer.C:
		return errors.New("worker shutdown timed out")
	}
}

func execute(ctx context.Context, args []string) (err error) {
	defer temporal.Recover(ctx, &err)
	switch {
	case len(args) == 0:
		return runWorker(ctx)
	case len(args) == 1 && args[0] == "smoke":
		return runSmoke(ctx)
	default:
		return errors.New("usage: worker [smoke]")
	}
}
