package taskflow_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"

	"nautilus/internal/taskflow"
	"nautilus/internal/testutil/require"
)

func TestSmoke(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(taskflow.SmokeActivity)
	env.ExecuteWorkflow(taskflow.Smoke)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "Temporal activity completed", result)
}

func TestRunWorkersIntegration(t *testing.T) {
	t.Parallel()
	c := temporalClient(t, "")
	prefix := "temporal-test-" + uuid.NewString()
	queues := []string{prefix + "-uploads", prefix + "-ocr", prefix + "-indexing"}
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = taskflow.RunWorkers(ctx, c, queues)
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
		require.NoError(t, taskflow.RunSmoke(t.Context(), c, queue))
	}
}

func TestRunWorkersStartupFailureIntegration(t *testing.T) {
	t.Parallel()
	c := temporalClient(t, "missing-"+uuid.NewString())
	require.Error(t, taskflow.RunWorkers(t.Context(), c, []string{"uploads", "ocr"}))
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
