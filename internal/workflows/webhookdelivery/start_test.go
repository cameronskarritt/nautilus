package webhookdelivery

import (
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/testutil/require"
)

func TestStartChildWaitsForStart(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	Register(env, Activities{})
	input := Input{OrganizationID: 42, DeliveryID: uuid.NewV4().String()}
	started := false
	env.SetOnChildWorkflowStartedListener(func(info *workflow.Info, _ workflow.Context, _ converter.EncodedValues) {
		started = true
		require.Equal(t, "webhook-delivery-42-"+input.DeliveryID, info.WorkflowExecution.ID)
		require.Equal(t, queueName, info.TaskQueueName)
	})
	env.OnWorkflow(Name, mock.Anything, input).Return(nil).Once()
	env.ExecuteWorkflow(func(ctx workflow.Context) error {
		upper := input
		upper.DeliveryID = strings.ToUpper(input.DeliveryID)
		return StartChild(ctx, upper)
	})
	require.NoError(t, env.GetWorkflowError())
	require.True(t, started)
	env.AssertExpectations(t)
}
