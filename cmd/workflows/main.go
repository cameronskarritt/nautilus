package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"nautilus/internal/config"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/temporal"
	"nautilus/internal/workflows/smoke"
)

var commands = map[string]func(context.Context, enums.Queue) error{
	"smoke": runSmoke,
}

func main() {
	config.LoadDotenv()
	ctx := log.WithContext(context.Background(), log.InferLogger("workflows"))
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:]); err != nil {
		log.FromContext(ctx).Fatal("workflow command failed", "error", err)
	}
}

func execute(ctx context.Context, args []string) (err error) {
	defer temporal.Recover(ctx, &err)
	if len(args) == 0 {
		return errors.New("usage: workflows <command> --queue=<name>")
	}
	if args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(os.Stderr, "usage: workflows <command> --queue=<name>\ncommands: smoke")
		return nil
	}
	command, ok := commands[args[0]]
	if !ok {
		return errors.Errorf("unknown workflow command %q", args[0])
	}
	var queue string
	flags := flag.NewFlagSet("workflows "+args[0], flag.ContinueOnError)
	flags.StringVar(&queue, "queue", "", "Temporal task queue (required)")
	if err := flags.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return errors.Wrap(err, "parse workflow flags")
	}
	queue = strings.TrimSpace(queue)
	if queue == "" || flags.NArg() != 0 {
		return errors.New("usage: workflows <command> --queue=<name>")
	}
	return command(ctx, enums.Queue(queue))
}

func runSmoke(ctx context.Context, queue enums.Queue) error {
	if queue != enums.QueueSmoke {
		return errors.Errorf("smoke requires --queue=%s", enums.QueueSmoke)
	}
	c, err := temporal.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := smoke.Check(ctx, c, queue); err != nil {
		return err
	}
	log.FromContext(ctx).Info("Temporal smoke workflow and activity completed", "queue", queue)
	return nil
}
