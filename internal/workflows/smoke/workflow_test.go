package smoke_test

import (
	"testing"

	"go.temporal.io/sdk/testsuite"

	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/smoke"
)

func TestWorkflow(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	smoke.Register(env)
	env.ExecuteWorkflow("Smoke")
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, "Temporal activity completed", result)
}
