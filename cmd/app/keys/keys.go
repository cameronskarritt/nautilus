package keys

import (
	"context"
	"flag"
	"io"
	"time"

	"nautilus/internal/aws"
	"nautilus/internal/config"
	"nautilus/internal/database/postgres"
	"nautilus/internal/errors"
	"nautilus/internal/kms/awskms"
	"nautilus/internal/log"
)

const usage = "usage: keys {provision-user|provision-organization} --key-arn ARN [--org-id UUID]"

type options struct {
	command string
	keyARN  string
	orgID   string
}

func Run(args []string) {
	logger := log.InferLogger("keys")
	opts, err := parse(args)
	if err != nil {
		logger.Fatal("invalid key management command", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ctx = log.WithContext(ctx, logger)
	if err := run(ctx, opts); err != nil {
		logger.Fatal("unable to provision encryption key", "error", err)
		return
	}
	logger.Info("encryption key provisioned", "scope", opts.command)
}

func parse(args []string) (*options, error) {
	if len(args) == 0 {
		return nil, errors.New(usage)
	}
	opts := &options{command: args[0]}
	switch opts.command {
	case "provision-user", "provision-organization":
	default:
		return nil, errors.New(usage)
	}
	flags := flag.NewFlagSet("keys", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.keyARN, "key-arn", "", "canonical KMS key ARN")
	if opts.command == "provision-organization" {
		flags.StringVar(&opts.orgID, "org-id", "", "organization external UUID")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return nil, errors.New(usage)
	}
	if flags.NArg() != 0 || opts.keyARN == "" || (opts.command == "provision-organization" && opts.orgID == "") {
		return nil, errors.New(usage)
	}
	return opts, nil
}

func run(ctx context.Context, opts *options) error {
	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	cfg, err := aws.LoadConfig(ctx)
	if err != nil {
		return err
	}
	manager := awskms.New(cfg, db)
	switch opts.command {
	case "provision-organization":
		return manager.ProvisionOrganization(ctx, opts.orgID, opts.keyARN)
	case "provision-user":
		return manager.ProvisionUser(ctx, opts.keyARN)
	default:
		return errors.New(usage)
	}
}
