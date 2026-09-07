package temporal_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	temporalenums "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/temporal"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/smoke"
)

func TestActivityPanicIntegration(t *testing.T) {
	t.Parallel()
	c := temporalClient(t, "")
	queue := enums.Queue("activity-panic-" + uuid.NewString())
	w := temporal.NewWorker(c, queue)
	smoke.Register(w)
	var attempts atomic.Int32
	w.RegisterActivityWithOptions(func(context.Context) (string, error) {
		if attempts.Add(1) == 1 {
			panic("first activity attempt")
		}
		return "retried", nil
	}, activity.RegisterOptions{Name: "RetryingActivity"})
	w.RegisterWorkflowWithOptions(func(ctx workflow.Context) (string, error) {
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout:    5 * time.Second,
			ScheduleToCloseTimeout: 15 * time.Second,
		})
		var result string
		err := workflow.ExecuteActivity(ctx, "RetryingActivity").Get(ctx, &result)
		return result, errors.Wrap(err, "run retrying activity")
	}, workflow.RegisterOptions{Name: "RetryingWorkflow"})
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: queue.String(), TaskQueue: queue.String(), WorkflowExecutionTimeout: 25 * time.Second,
	}, "RetryingWorkflow")
	require.NoError(t, err)
	var result string
	require.NoError(t, run.Get(ctx, &result))
	require.Equal(t, "retried", result)
	require.Equal(t, int32(2), attempts.Load())
	require.NoError(t, smoke.Check(ctx, c, queue))
}

func TestWorkflowPanicIntegration(t *testing.T) {
	t.Parallel()
	c := temporalClient(t, "")
	queue := enums.Queue("workflow-panic-" + uuid.NewString())
	w := temporal.NewWorker(c, queue)
	smoke.Register(w)
	w.RegisterWorkflowWithOptions(func(workflow.Context) error {
		panic("workflow bug")
	}, workflow.RegisterOptions{Name: "PanickingWorkflow"})
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: queue.String(), TaskQueue: queue.String(), WorkflowExecutionTimeout: time.Minute,
	}, "PanickingWorkflow")
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer cancel()
		require.NoError(t, c.TerminateWorkflow(ctx, run.GetID(), run.GetRunID(), "test cleanup"))
	})
	history := c.GetWorkflowHistory(ctx, run.GetID(), run.GetRunID(), true, temporalenums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	failed := false
	for history.HasNext() {
		event, err := history.Next()
		require.NoError(t, err)
		if event.GetEventType() == temporalenums.EVENT_TYPE_WORKFLOW_TASK_FAILED {
			failure := event.GetWorkflowTaskFailedEventAttributes().GetFailure()
			require.Contains(t, failure.GetMessage(), "workflow bug")
			require.NotEmpty(t, failure.GetStackTrace())
			failed = true
			break
		}
	}
	require.True(t, failed, "workflow panic must fail a workflow task")
	description, err := c.DescribeWorkflowExecution(ctx, run.GetID(), run.GetRunID())
	require.NoError(t, err)
	require.Equal(t, temporalenums.WORKFLOW_EXECUTION_STATUS_RUNNING, description.WorkflowExecutionInfo.Status)
	require.NoError(t, smoke.Check(ctx, c, queue))
}

func TestRunWorkersPanicIntegration(t *testing.T) {
	t.Parallel()
	c := temporalClient(t, "")
	queue := enums.Queue("worker-panic-" + uuid.NewString())
	stopped := temporal.NewWorker(c, queue+"-stopped")
	smoke.Register(stopped)
	require.NoError(t, stopped.Start())
	stopped.Stop()
	healthy := temporal.NewWorker(c, queue)
	smoke.Register(healthy)
	require.NoError(t, healthy.Start())
	t.Cleanup(healthy.Stop)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, smoke.Check(ctx, c, queue))
	done := make(chan error, 1)
	go func() {
		done <- temporal.RunWorkers(ctx, map[enums.Queue]worker.Worker{
			queue: healthy, queue + "-stopped": stopped,
		})
	}()
	select {
	case err := <-done:
		require.Error(t, err)
		var stack errors.StackTracer
		require.ErrorAs(t, err, &stack)
		require.NotEmpty(t, stack.StackTrace())
	case <-ctx.Done():
		t.Fatal("worker panic did not stop the group")
	}
	require.Panics(t, func() { _ = healthy.Start() }, "healthy sibling must have stopped")
}
