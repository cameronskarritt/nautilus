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
	"nautilus/internal/ocr/lmstudio"
	"nautilus/internal/temporal"
	"nautilus/internal/webhook"
	"nautilus/internal/workflows/smoke"
	"nautilus/internal/workflows/upload"
	"nautilus/internal/workflows/webhookdelivery"
)

var registrations = map[enums.Queue]func(context.Context, worker.Registry) (func(), error){
	enums.QueueSmoke:    registerSmoke,
	enums.QueueUploads:  registerUpload,
	enums.QueueWebhooks: registerWebhooks,
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
	indexer, err := newIndexer(ctx)
	if err != nil {
		return nil, err
	}
	extractor, err := lmstudio.New(lmstudio.Config{
		URL:    config.Get("OCR_URL", "http://localhost:1234/v1"),
		Model:  config.Get("OCR_MODEL", "allenai/olmocr-2-7b"),
		APIKey: config.Get[string]("OCR_API_KEY"),
	})
	if err != nil {
		return nil, err
	}
	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	upload.Register(reg, upload.Activities{DB: db, Store: s3store.New(cfg, bucket, cfg.BaseEndpoint != nil), Keys: awskms.New(cfg, db), OCR: extractor, Indexer: indexer})
	return db.Close, nil
}

func registerWebhooks(ctx context.Context, reg worker.Registry) (func(), error) {
	cfg, err := aws.LoadConfig(ctx)
	if err != nil {
		return nil, err
	}
	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	activities := webhookdelivery.Activities{DB: db, Keys: awskms.New(cfg, db), Sender: webhook.NewSender()}
	webhookdelivery.Register(reg, activities)
	return db.Close, nil
}
