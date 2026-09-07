package keys

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"io"
	"time"

	"nautilus/internal/aws"
	"nautilus/internal/config"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database"
	"nautilus/internal/database/kmskeys"
	"nautilus/internal/database/postgres"
	"nautilus/internal/errors"
	"nautilus/internal/kms/awskms"
	"nautilus/internal/log"
)

const usage = "usage: keys {provision-user|import-user|provision-organization} --key-arn ARN [--org-id UUID]"

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
	case "provision-user", "import-user", "provision-organization":
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
		if err := allowNewUserKey(ctx, db); err != nil {
			return err
		}
		return manager.ProvisionUser(ctx, opts.keyARN)
	case "import-user":
		key, err := legacyKey(config.Get[string]("ENCRYPTION_KEY"))
		if err != nil {
			return err
		}
		defer clear(key)
		if err := verifyLegacySecrets(ctx, db, key); err != nil {
			return err
		}
		return manager.ImportUserKey(ctx, opts.keyARN, key)
	default:
		return errors.New(usage)
	}
}

func allowNewUserKey(ctx context.Context, db database.Database) error {
	key, err := kmskeys.GetUser(ctx, db)
	if err != nil || key != nil {
		return err
	}
	var exists bool
	err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE totp_secret IS NOT NULL)").Scan(&exists)
	if err != nil {
		return errors.Wrap(err, "unable to check existing TOTP secrets")
	}
	if exists {
		return errors.New("existing TOTP secrets require keys import-user")
	}
	return nil
}

func legacyKey(encoded string) ([]byte, error) {
	if key, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(key) == 32 {
		return key, nil
	}
	if key, err := hex.DecodeString(encoded); err == nil && len(key) == 32 {
		return key, nil
	}
	return nil, errors.New("ENCRYPTION_KEY must encode a 32-byte key for import-user")
}

func verifyLegacySecrets(ctx context.Context, db database.Database, key []byte) error {
	enc, err := encrypt.New(key)
	if err != nil {
		return err
	}
	rows, err := db.Query(ctx, "SELECT totp_secret FROM users WHERE totp_secret IS NOT NULL")
	if err != nil {
		return errors.Wrap(err, "unable to read existing TOTP secrets")
	}
	return database.ScanRows(rows, func(row database.Row) error {
		var ciphertext []byte
		if err := row.Scan(&ciphertext); err != nil {
			return errors.Wrap(err, "unable to read existing TOTP secret")
		}
		plaintext, err := enc.Decrypt(ctx, ciphertext)
		clear(plaintext)
		if err != nil {
			return errors.New("ENCRYPTION_KEY cannot decrypt all retained TOTP secrets; import aborted")
		}
		return nil
	})
}
