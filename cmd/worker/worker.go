package main

import (
	"context"

	"go.temporal.io/sdk/worker"

	"nautilus/internal/aws"
	"nautilus/internal/config"
	"nautilus/internal/database/postgres"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/kms/awskms"
	"nautilus/internal/objectstore/s3store"
	"nautilus/internal/ocr/stub"
	"nautilus/internal/temporal"
	"nautilus/internal/workflows/smoke"
	"nautilus/internal/workflows/upload"
)

var registrations = map[enums.Queue]func(context.Context, worker.Registry) (func(), error){
	enums.QueueSmoke:   registerSmoke,
	enums.QueueUploads: registerUpload,
}

func runWorker(ctx context.Context, queue enums.Queue) error {
	register, ok := registrations[queue]
	if !ok {
		return errors.Errorf("no workflows registered for queue %q", queue)
	}
	c, err := temporal.Dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	w := temporal.NewWorker(c, queue)
	close, err := register(ctx, w)
	if err != nil {
		return err
	}
	if err := temporal.RunWorkers(ctx, map[enums.Queue]worker.Worker{queue: w}); err != nil {
		// The command exits on failure; do not block that exit on a stuck connection.
		return err
	}
	close()
	return nil
}

func registerSmoke(_ context.Context, reg worker.Registry) (func(), error) {
	smoke.Register(reg)
	return func() {}, nil
}

func registerUpload(ctx context.Context, reg worker.Registry) (func(), error) {
	bucket := config.Get[string]("DOCUMENTS_BUCKET")
	if bucket == "" {
		return nil, errors.New("DOCUMENTS_BUCKET is required for the uploads worker")
	}
	cfg, err := aws.LoadConfig(ctx)
	if err != nil {
		return nil, err
	}
	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	upload.Register(reg, upload.Activities{DB: db, Store: s3store.New(cfg, bucket, cfg.BaseEndpoint != nil), Keys: awskms.New(cfg, db), OCR: stub.OCR{}})
	return db.Close, nil
}
