package temporal_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"nautilus/internal/enums"
	"nautilus/internal/temporal"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/smoke"
)

func TestRunWorkersIntegration(t *testing.T) {
	t.Parallel()
	c := temporalClient(t, "")
	prefix := enums.Queue("temporal-test-" + uuid.NewString())
	queues := []enums.Queue{prefix + "-uploads", prefix + "-ocr", prefix + "-indexing"}
	workers := make(map[enums.Queue]worker.Worker, len(queues))
	for _, queue := range queues {
		w := temporal.NewWorker(c, queue)
		smoke.Register(w)
		workers[queue] = w
	}
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = temporal.RunWorkers(ctx, workers)
		close(done)
	}()
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
			require.NoError(t, runErr)
		case <-time.After(35 * time.Second):
			t.Fatal("workers did not stop after cancellation")
		}
	})
	for _, queue := range queues {
		require.NoError(t, smoke.Check(t.Context(), c, queue))
	}
}

func TestRunWorkersStartupFailureIntegration(t *testing.T) {
	t.Parallel()
	c := temporalClient(t, "missing-"+uuid.NewString())
	workers := make(map[enums.Queue]worker.Worker)
	for _, queue := range []enums.Queue{enums.QueueSmoke, enums.QueueUploads} {
		w := temporal.NewWorker(c, queue)
		smoke.Register(w)
		workers[queue] = w
	}
	require.Error(t, temporal.RunWorkers(t.Context(), workers))
}

func temporalClient(t *testing.T, namespace string) client.Client {
	t.Helper()
	address := os.Getenv("TEMPORAL_TEST_ADDRESS")
	if address == "" {
		t.Skip("set TEMPORAL_TEST_ADDRESS to run against a Temporal server")
	}
	if namespace == "" {
		namespace = os.Getenv("TEMPORAL_NAMESPACE")
		if namespace == "" {
			namespace = "nautilus"
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c, err := client.DialContext(ctx, client.Options{HostPort: address, Namespace: namespace})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}
