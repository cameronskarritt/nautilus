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

func TestRunSmokeIntegration(t *testing.T) {
	address := os.Getenv("TEMPORAL_TEST_ADDRESS")
	if address == "" {
		t.Skip("set TEMPORAL_TEST_ADDRESS to run against a Temporal server")
	}
	t.Parallel()
	namespace := os.Getenv("TEMPORAL_NAMESPACE")
	if namespace == "" {
		namespace = "nautilus"
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c, err := client.DialContext(ctx, client.Options{HostPort: address, Namespace: namespace})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	queue := "temporal-test-" + uuid.NewString()
	w := taskflow.NewWorker(c, queue)
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	require.NoError(t, taskflow.RunSmoke(t.Context(), c, queue))
}
