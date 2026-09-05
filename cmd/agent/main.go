package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"nautilus/internal/ai/agent"
	"nautilus/internal/ai/eventlog/pgeventlog"
	"nautilus/internal/ai/llm"
	"nautilus/internal/ai/llm/anthropic"
	"nautilus/internal/aws"
	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/database/pushsubscriptions"
	"nautilus/internal/database/redis"
	"nautilus/internal/enums"
	"nautilus/internal/log"
	"nautilus/internal/notifier/webpush"
	"nautilus/internal/observability/llmtrace/otel"
	"nautilus/internal/queue"
	"nautilus/internal/queue/outbox"
	"nautilus/internal/queue/sqs"
)

func main() {
	config.LoadDotenv()

	logger := log.InferLogger("agent")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = log.WithContext(ctx, logger)

	db, err := postgres.Connect(ctx, config.Get[string]("DATABASE_URL"))
	if err != nil {
		logger.Fatal("error connecting to database", "error", err)
	}
	defer database.Close(ctx, db)()

	rdb, err := redis.Connect(ctx, config.Get[string]("REDIS_URL"))
	if err != nil {
		logger.Fatal("error connecting to redis", "error", err)
	}
	defer redis.Close(ctx, rdb)()

	appTracer, shutdownTracer := newTracer(ctx, logger)
	llmTracer := otel.NewTracer(appTracer)

	awsCfg, err := aws.LoadConfig(ctx)
	if err != nil {
		logger.Fatal("error loading AWS config", "error", err)
	}

	prefix := config.Get[string]("SQS_QUEUE_PREFIX")
	visTimeout := int32(config.Get("SQS_VISIBILITY_TIMEOUT", 300))
	broker := sqs.NewBroker(awsCfg, logger, prefix, visTimeout)

	pubsubClient := redis.NewPubSub(rdb)

	agentEventLog := pgeventlog.New(db)
	agentClients := llm.NewClientRegistry()
	anthropicClient := anthropic.NewClient(nil).
		WithAPIKey(config.Get[string]("ANTHROPIC_API_KEY")).
		WithLLMTracer(llmTracer)
	agentClients.Register(anthropic.ClaudeSonnet45, anthropicClient)

	var agentTools []llm.Tool

	pushStore := pushsubscriptions.NewStore(db)
	pushNotifier := webpush.NewNotifier(
		pushStore,
		config.Get[string]("VAPID_SUBSCRIBER"),
		config.Get[string]("VAPID_PUBLIC_KEY"),
		config.Get[string]("VAPID_PRIVATE_KEY"),
	)

	harness := agent.NewHarness(db, agentEventLog, pubsubClient, agentClients, agentTools, logger, pushNotifier)

	var dispatcherWG sync.WaitGroup
	dispatcherWG.Add(1)
	go func() {
		defer dispatcherWG.Done()
		outbox.NewDispatcher(db, broker).Run(ctx, enums.QueueAgentSignals)
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-shutdown
		logger.Info("received shutdown signal, draining queues...")
		cancel()
	}()

	logger.Info("agent worker starting", "queues", []string{string(enums.QueueAgentSignals)})

	handlers := map[enums.Queue]queue.MessageHandler{
		enums.QueueAgentSignals: harness.HandleQueueMessage,
	}
	if err := queue.Run(ctx, broker, handlers); err != nil {
		logger.Fatal("queue consumer exited with error", "error", err)
	}
	dispatcherWG.Wait()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := harness.Shutdown(shutdownCtx); err != nil {
		logger.Error("error shutting down harness", "error", err)
	}
	if err := shutdownTracer(shutdownCtx); err != nil {
		logger.Error("error shutting down tracer", "error", err)
	}

	logger.Info("agent worker stopped")
}
