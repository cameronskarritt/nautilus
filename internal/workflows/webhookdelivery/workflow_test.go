package webhookdelivery

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestWorkflowRetries(t *testing.T) {
	t.Parallel()
	for _, infrastructure := range []bool{false, true} {
		t.Run(map[bool]string{false: "HTTP failure", true: "persistence failure"}[infrastructure], func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			Register(env, Activities{})
			input := Input{OrganizationID: 1, DeliveryID: uuid.NewV4().String()}
			var err error
			if infrastructure {
				err = unavailable()
			}
			env.OnActivity(sendName, mock.Anything, input).Return(enums.DeliveryStatusDelivering, err).Once()
			env.OnActivity(sendName, mock.Anything, input).Return(enums.DeliveryStatusSucceeded, nil).Once()
			env.ExecuteWorkflow(Name, input)
			require.NoError(t, env.GetWorkflowError())
			env.AssertExpectations(t)
		})
	}
}

func TestWorkflowExhaustion(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	Register(env, Activities{})
	input := Input{OrganizationID: 1, DeliveryID: uuid.NewV4().String()}
	calls := 0
	env.OnActivity(sendName, mock.Anything, input).Return(func(context.Context, Input) (enums.DeliveryStatus, error) {
		calls++
		return enums.DeliveryStatusDelivering, nil
	})
	completion := Completion{Input: input, Status: enums.DeliveryStatusFailed}
	env.OnActivity(completeName, mock.Anything, completion).Return(enums.DeliveryStatus(""), errors.New("temporary storage failure")).Once()
	env.OnActivity(completeName, mock.Anything, completion).Return(enums.DeliveryStatusFailed, nil).Once()
	env.ExecuteWorkflow(Name, input)
	require.NoError(t, env.GetWorkflowError())
	require.True(t, calls > 10 && calls < 50)
	env.AssertExpectations(t)
}

func TestWorkflowCancellation(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	Register(env, Activities{})
	input := Input{OrganizationID: 1, DeliveryID: uuid.NewV4().String()}
	env.OnActivity(sendName, mock.Anything, input).Return(enums.DeliveryStatusDelivering, nil).Once()
	env.OnActivity(completeName, mock.Anything, Completion{Input: input, Status: enums.DeliveryStatusCanceled}).Return(enums.DeliveryStatusCanceled, nil).Once()
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)
	env.ExecuteWorkflow(Name, input)
	require.True(t, temporal.IsCanceledError(env.GetWorkflowError()))
	env.AssertExpectations(t)
}

func TestWorkflowInvalidInput(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	Register(env, Activities{})
	env.ExecuteWorkflow(Name, Input{OrganizationID: 1, DeliveryID: "invalid"})
	var failure *temporal.ApplicationError
	require.ErrorAs(t, env.GetWorkflowError(), &failure)
	require.True(t, failure.NonRetryable())
}
